package service

// openai_gateway_workbuddy.go 二开新增：raw Chat Completions 漏斗的 WorkBuddy 分叉。
//
// 协议收敛架构：workbuddy 账号 credentials 强制 api_protocol=adaptive，且供应商
// profile 只声明 chat_completions 端点 → resolveUpstreamProtocol 把任意入站协议
// （Anthropic / Responses / CC）都收敛到 chat_completions → 全部流量进
// forwardAsRawChatCompletions，本文件在该漏斗做平台分叉：
//
//   - URL：realm → {chatBase}/v2/chat/completions（cn=copilot.tencent.com，
//     global=www.workbuddy.ai；不走账号 base_url，上游域由 realm 唯一决定）；
//   - 体：pkg/workbuddy.PrepareChatBody 归一化（强制 stream、max_tokens 翻译、
//     tool_choice/role/image_url 归一、prompt_cache_key 注入；global 补兜底 system
//     与 effort 档位降级）；
//   - 头：pkg/workbuddy.ChatHeaders 头族（UA 三段式 / X-Machine-ID 会话族 /
//     Authorization Bearer），删净默认头再注入；
//   - 响应：上游是标准 OpenAI SSE——流式直接走既有 streamRawChatCompletions 中继；
//     非流式客户端请求同样以流出站（上游拒绝非流式），在本地聚合 SSE 为
//     chat.completion JSON（bufferWorkbuddyChatCompletions）；
//   - 错误：handleWorkbuddyUpstreamError 按 pkg 错误分类（ClassifyError）映射账号
//     状态（限流/积分耗尽/session dead/模型封禁/UA 风控/token 失效）。

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/workbuddy"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/sjson"
)

// workbuddyChatEndpointPath chat 出站路径（CN 与 global 共用 /v2 单路径；
// 不用 /console/chat/completions——该路径挂腾讯云 WAF body 内容规则）。
const workbuddyChatEndpointPath = "/v2/chat/completions"

// workbuddyRateLimitFallbackCooldown 限流无重置线索时的本地冷却兜底；
// workbuddyRateLimitCooldownCap 有线索（resetAt/Retry-After）时的封顶。
const (
	workbuddyRateLimitFallbackCooldown = 10 * time.Minute
	workbuddyRateLimitCooldownCap      = 2 * time.Hour
	// workbuddyWafCooldownBase WAF 403 账号软冷却基数（源项目 wafCooldownBase 同值）。
	workbuddyWafCooldownBase = 60 * time.Second
)

// workbuddyRequestPathRefresher 指向最近构造的 WorkbuddyOAuthService，供请求路径
// 401/403 时触发一次强制 token 刷新（注册表模式，与 RegisterPlatformOpsRunner 同款；
// 网关与 oauth 服务分属不同 wire 节点，无法经构造参数直达）。
var workbuddyRequestPathRefresher atomic.Pointer[WorkbuddyOAuthService]

// workbuddyChatBaseURL realm → chat 域基址。
func workbuddyChatBaseURL(cred workbuddy.Credentials) string {
	if cred.IsGlobal() {
		return DefaultWorkbuddyGlobalBaseURL
	}
	return DefaultWorkbuddyCNChatBaseURL
}

// workbuddyEnsureConsoleSystem global realm 兜底 system 注入（移植自 pkg
// client.go ensureConsoleSystem——防上游 code 11-128，未导出故在此复刻）：
// 首条消息非 system 时在 messages 最前补 fallback system。
func workbuddyEnsureConsoleSystem(body []byte) []byte {
	if len(body) == 0 {
		return body
	}
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		return body
	}
	msgs, ok := obj["messages"].([]any)
	if !ok || len(msgs) == 0 {
		return body
	}
	if first, ok := msgs[0].(map[string]any); ok {
		if role, _ := first["role"].(string); strings.EqualFold(strings.TrimSpace(role), "system") {
			return body
		}
	}
	obj["messages"] = append([]any{map[string]any{"role": "system", "content": "You are a helpful assistant."}}, msgs...)
	out, err := json.Marshal(obj)
	if err != nil {
		return body
	}
	return out
}

// prepareWorkbuddyForwardBody 出站体归一化：PrepareChatBody（强制 stream / 参数
// 归一 / prompt_cache_key）+ global 兜底 system。conversationID 留空：CC 漏斗的
// body 不携带会话标识，cache key 回落按账号 uid 派生（源项目同款回落）。
func prepareWorkbuddyForwardBody(cred workbuddy.Credentials, body []byte) []byte {
	prepared := workbuddy.PrepareChatBody(cred, body, "")
	if cred.IsGlobal() {
		prepared = workbuddyEnsureConsoleSystem(prepared)
	}
	return prepared
}

// applyWorkbuddyChatTransport 在 sendCCUpstreamRequest 构建的出站请求上注入
// WorkBuddy 头族：删净默认 Authorization 后由 ChatHeaders 统一设置（Bearer
// access_token / 缺省 X-No-* 约定）；Content-Type / Accept / UA / Accept-Language
// 均被 Header.Set 覆盖为官方桌面端形态。账号 user_agent 凭证可显式覆盖整串 UA
// （优先级：credentials.user_agent > openai user_agent 覆写 > 默认三段式）。
// 放在账号级 header_overrides 之前应用，保持运营覆写的最高优先级。
func applyWorkbuddyChatTransport(req *http.Request, account *Account) error {
	cred, err := parseWorkbuddyCredentials(account)
	if err != nil {
		return fmt.Errorf("parse workbuddy credentials: %w", err)
	}
	if cred.UserAgent == "" {
		if ua := account.GetOpenAIUserAgent(); ua != "" {
			cred.UserAgent = ua
		}
	}
	// ConversationID 透传优先：CC 白名单不含该头，客户端未给就不伪造；
	// ConversationRequestID 是聚合主键（必发），每条出站消息独立生成。
	req.Header.Del("Authorization")
	workbuddy.ChatHeaders(req, cred, workbuddy.ChatMeta{ConversationRequestID: workbuddy.NewMessageID()})
	return nil
}

// ---------------------------------------------------------------------------
// 非流式聚合：上游强制 stream，客户端非流式请求在本地把 SSE 聚合为 chat.completion。
// ---------------------------------------------------------------------------

// workbuddyChunkAggregator CC SSE → 非流式 chat.completion 的聚合状态机。
// 文本字段按 choice 累积为 string（ChatMessage.Content 是 json.RawMessage，
// 收口时再序列化），tool_calls 按 (choice, tool index) 累积。
type workbuddyChunkAggregator struct {
	resp        apicompat.ChatCompletionsResponse
	choices     map[int]*apicompat.ChatChoice
	contents    map[int]*strings.Builder
	reasonings  map[int]*strings.Builder
	refusals    map[int]*strings.Builder
	toolCalls   map[int]map[int]*apicompat.ChatToolCall // choice → tool index → 累积调用
	maxChoice   int
	sawChoice   bool
	maxToolCall map[int]int
}

func newWorkbuddyChunkAggregator(model string) *workbuddyChunkAggregator {
	return &workbuddyChunkAggregator{
		resp:        apicompat.ChatCompletionsResponse{Object: "chat.completion", Model: model},
		choices:     map[int]*apicompat.ChatChoice{},
		contents:    map[int]*strings.Builder{},
		reasonings:  map[int]*strings.Builder{},
		refusals:    map[int]*strings.Builder{},
		toolCalls:   map[int]map[int]*apicompat.ChatToolCall{},
		maxToolCall: map[int]int{},
	}
}

// choiceOf 取（或创建）choice 的累积槽位（含文本构造器）。
func (a *workbuddyChunkAggregator) choiceOf(index int) *apicompat.ChatChoice {
	if _, ok := a.contents[index]; !ok {
		a.contents[index] = &strings.Builder{}
		a.reasonings[index] = &strings.Builder{}
		a.refusals[index] = &strings.Builder{}
	}
	acc, ok := a.choices[index]
	if !ok {
		acc = &apicompat.ChatChoice{Index: index, Message: apicompat.ChatMessage{Role: "assistant"}}
		a.choices[index] = acc
		if !a.sawChoice || index > a.maxChoice {
			a.maxChoice = index
		}
		a.sawChoice = true
	}
	return acc
}

// Observe 累积单个 chunk。
func (a *workbuddyChunkAggregator) Observe(chunk *apicompat.ChatCompletionsChunk) {
	if chunk == nil {
		return
	}
	if a.resp.ID == "" {
		a.resp.ID = chunk.ID
		a.resp.Created = chunk.Created
	}
	if chunk.Model != "" && a.resp.Model == "" {
		a.resp.Model = chunk.Model
	}
	if chunk.SystemFingerprint != "" {
		a.resp.SystemFingerprint = chunk.SystemFingerprint
	}
	if chunk.ServiceTier != "" {
		a.resp.ServiceTier = chunk.ServiceTier
	}
	if chunk.Usage != nil {
		usage := *chunk.Usage
		a.resp.Usage = &usage
	}
	for _, choice := range chunk.Choices {
		acc := a.choiceOf(choice.Index)
		if role := choice.Delta.Role; role != "" {
			acc.Message.Role = role
		}
		if choice.Delta.Content != nil && *choice.Delta.Content != "" {
			a.contents[choice.Index].WriteString(*choice.Delta.Content)
		}
		if rc := choice.Delta.ReasoningContent; rc != nil && *rc != "" {
			a.reasonings[choice.Index].WriteString(*rc)
		} else if rc := choice.Delta.Reasoning; rc != nil && *rc != "" {
			a.reasonings[choice.Index].WriteString(*rc)
		}
		if choice.Delta.Refusal != nil && *choice.Delta.Refusal != "" {
			a.refusals[choice.Index].WriteString(*choice.Delta.Refusal)
		}
		for _, tc := range choice.Delta.ToolCalls {
			idx := 0
			if tc.Index != nil {
				idx = *tc.Index
			}
			byIndex, ok := a.toolCalls[choice.Index]
			if !ok {
				byIndex = map[int]*apicompat.ChatToolCall{}
				a.toolCalls[choice.Index] = byIndex
			}
			if idx > a.maxToolCall[choice.Index] {
				a.maxToolCall[choice.Index] = idx
			}
			accTC, ok := byIndex[idx]
			if !ok {
				typ := tc.Type
				if typ == "" {
					typ = "function"
				}
				accTC = &apicompat.ChatToolCall{ID: tc.ID, Type: typ}
				byIndex[idx] = accTC
			}
			if tc.ID != "" && accTC.ID == "" {
				accTC.ID = tc.ID
			}
			if tc.Function.Name != "" && accTC.Function.Name == "" {
				accTC.Function.Name = tc.Function.Name
			}
			accTC.Function.Arguments += tc.Function.Arguments
		}
		if choice.FinishReason != nil && *choice.FinishReason != "" {
			acc.FinishReason = *choice.FinishReason
		}
	}
}

// Finish 收口为非流式响应（choices 按下标升序、tool_calls 按流序展开、
// 文本字段序列化回 ChatMessage.Content）。
func (a *workbuddyChunkAggregator) Finish() apicompat.ChatCompletionsResponse {
	encodeText := func(s string) json.RawMessage {
		if s == "" {
			return nil
		}
		raw, err := json.Marshal(s)
		if err != nil {
			return nil
		}
		return raw
	}
	for i := 0; i <= a.maxChoice; i++ {
		choice, ok := a.choices[i]
		if !ok {
			continue
		}
		choice.Message.Content = encodeText(a.contents[i].String())
		if r := a.reasonings[i].String(); r != "" {
			choice.Message.ReasoningContent = r
		}
		if r := a.refusals[i].String(); r != "" {
			choice.Message.Refusal = r
		}
		if byIndex := a.toolCalls[i]; len(byIndex) > 0 {
			// 展开 tool_calls（去 Index，非流式响应不带）。
			calls := make([]apicompat.ChatToolCall, 0, len(byIndex))
			for j := 0; j <= a.maxToolCall[i]; j++ {
				if tc, ok := byIndex[j]; ok {
					calls = append(calls, *tc)
				}
			}
			choice.Message.ToolCalls = calls
		}
		a.resp.Choices = append(a.resp.Choices, *choice)
	}
	if a.resp.ID == "" {
		a.resp.ID = "chatcmpl-workbuddy-" + workbuddy.NewMessageID()
	}
	if a.resp.Created == 0 {
		a.resp.Created = time.Now().Unix()
	}
	if len(a.resp.Choices) == 0 {
		a.resp.Choices = append(a.resp.Choices, apicompat.ChatChoice{
			Index:        0,
			Message:      apicompat.ChatMessage{Role: "assistant"},
			FinishReason: "stop",
		})
	}
	return a.resp
}

// workbuddyUsageFromChat apicompat.ChatUsage → 网关计费用量。
func workbuddyUsageFromChat(u apicompat.ChatUsage) OpenAIUsage {
	out := OpenAIUsage{
		InputTokens:  u.PromptTokens,
		OutputTokens: u.CompletionTokens,
	}
	if u.PromptTokensDetails != nil {
		out.CacheReadInputTokens = u.PromptTokensDetails.CachedTokens
	}
	return out
}

// wrapWorkbuddyChatResponse 上游响应形态归一（sendCCUpstreamRequest 出口调用）：
// workbuddy 上游恒以 SSE 回流（拒绝非流式）。调用方期望流式（stream=true）时本就是
// 标准 OpenAI SSE，直接透传；期望非流式（stream=false）时本地聚合为 chat.completion
// JSON 并替换 resp.Body，使三条 raw CC 路径（CC / Responses / Anthropic 入站）的既有
// buffer 处理器无需平台特判。usage 已含在聚合 JSON（PrepareChatBody 注入的
// include_usage 让末帧携带 usage），调用方按非流式体提取即可。
func (s *OpenAIGatewayService) wrapWorkbuddyChatResponse(resp *http.Response, stream bool, model string) error {
	if stream || resp == nil || resp.Body == nil {
		return nil
	}
	agg := newWorkbuddyChunkAggregator(model)
	scan := s.scanCCStream(nil, resp, "openai chat workbuddy aggregate", resp.Header.Get("x-request-id"), time.Now(), agg.Observe)
	if scan.Err != nil {
		return fmt.Errorf("workbuddy aggregate stream read: %w", scan.Err)
	}
	if !scan.SawDone {
		logCCStreamMissingDoneSentinel("openai chat workbuddy aggregate", resp.Header.Get("x-request-id"))
	}
	raw, err := json.Marshal(agg.Finish())
	if err != nil {
		return fmt.Errorf("workbuddy aggregate marshal: %w", err)
	}
	// 二开：apicompat.ChatUsage 无 credits 字段，聚合 JSON 会丢掉 usage.credit；
	// 用 scanCCStream 原始提取值回填，供 RecordUsage 记录账号今日积分消耗。
	if scan.Usage.Credits > 0 {
		if patched, serr := sjson.SetBytes(raw, "usage.credit", scan.Usage.Credits); serr == nil {
			raw = patched
		}
	}
	_ = resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(raw))
	resp.ContentLength = int64(len(raw))
	resp.Header.Set("Content-Type", "application/json")
	return nil
}

// ---------------------------------------------------------------------------
// 上游错误 → 账号状态映射。
// ---------------------------------------------------------------------------

// handleWorkbuddyUpstreamError 处理 workbuddy 平台的 >=400 上游响应。
// 返回 (handled, failoverErr)：handled=false 表示该类别未接管，调用方继续走
// 既有通用 failover 链（5xx / not_found / WAF / 内容策略 / 参数错误等）；
// handled=true 且 failoverErr=nil 表示不换号、按上游原文透传给客户端
// （11102 模型封禁 / 11140 UA 风控）。
func (s *OpenAIGatewayService) handleWorkbuddyUpstreamError(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	resp *http.Response,
	respBody []byte,
	upstreamMsg string,
	upstreamModel string,
) (bool, *UpstreamFailoverError) {
	kind := workbuddy.ClassifyError(resp.StatusCode, respBody)
	body := string(respBody)

	appendFailover := func(kindLabel string) {
		appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
			ProxyID:            opsUpstreamProxyID(account),
			ProxyName:          opsUpstreamProxyName(account),
			Platform:           account.Platform,
			AccountID:          account.ID,
			AccountName:        account.Name,
			UpstreamStatusCode: resp.StatusCode,
			UpstreamRequestID:  resp.Header.Get("x-request-id"),
			Kind:               "failover",
			Message:            fmt.Sprintf("workbuddy %s: %s", kindLabel, upstreamMsg),
		})
	}

	switch {
	case kind == workbuddy.KindHardCredit:
		// 402 / 14018+429 积分耗尽：快照清零 + status=error（wb_no_credit）+
		// 不可调度；checkin/balance 成功后自动恢复。
		ZeroWorkbuddyQuotaExtra(ctx, s.accountRepo, account.ID)
		if err := s.accountRepo.SetError(ctx, account.ID, WorkbuddyNoCreditReason); err != nil {
			slog.Warn("workbuddy set no-credit error failed", "account_id", account.ID, "error", err)
		} else {
			account.Status = StatusError
			account.ErrorMessage = WorkbuddyNoCreditReason
			account.Schedulable = false
		}
		appendFailover("hard_credit")
		return true, s.newOpenAIAccountFailoverError(account, resp.StatusCode, resp.Header, respBody, upstreamMsg, true, false)

	case kind == workbuddy.KindSoftRate:
		resetAt := workbuddyRateLimitResetAt(resp.Header, body)
		if resp.StatusCode == http.StatusTooManyRequests && workbuddy.IsModelRateLimit(body) {
			// 6004 模型级限流：只冷却 (账号,模型)，账号整体健康。
			if err := s.accountRepo.SetModelRateLimit(ctx, account.ID, upstreamModel, resetAt, "wb model rate limit (6004)"); err != nil {
				slog.Warn("workbuddy set model rate limit failed", "account_id", account.ID, "model", upstreamModel, "error", err)
			}
		} else if err := s.accountRepo.SetRateLimited(ctx, account.ID, resetAt); err != nil {
			slog.Warn("workbuddy set rate limit failed", "account_id", account.ID, "error", err)
		}
		appendFailover("soft_rate")
		return true, s.newOpenAIAccountFailoverError(account, resp.StatusCode, resp.Header, respBody, upstreamMsg, false, false)

	case kind == workbuddy.KindSessionDead:
		// 12153 session dead：连续 3 次容忍计数后才罚号；前两次仅本次 failover 换号。
		strikes := bumpWorkbuddySessionStrikes(ctx, s.accountRepo, account)
		if strikes >= workbuddySessionStrikeLimit {
			if err := s.accountRepo.SetError(ctx, account.ID, "wb_session_dead"); err != nil {
				slog.Warn("workbuddy set session-dead error failed", "account_id", account.ID, "error", err)
			}
			appendFailover("session_dead_persist")
			return true, s.newOpenAIAccountFailoverError(account, resp.StatusCode, resp.Header, respBody, upstreamMsg, true, false)
		}
		slog.Warn("workbuddy session dead tolerated", "account_id", account.ID, "strikes", strikes)
		appendFailover("session_dead")
		return true, s.newOpenAIAccountFailoverError(account, resp.StatusCode, resp.Header, respBody, upstreamMsg, false, false)

	case kind == workbuddy.KindModelBlocked:
		// 11102 该后端无此模型：不罚账号；向客户端返回模型不可用（不换号）。
		return true, nil

	case kind == workbuddy.KindAccountFault:
		// 11140 request illegal（UA 风控）/ 14017 试用未激活：透传原文 + 告警，不计 strike。
		slog.Warn("workbuddy account fault (pass-through)", "account_id", account.ID, "status", resp.StatusCode, "message", upstreamMsg)
		return true, nil

	case kind == workbuddy.KindWafBlock:
		// 403 无业务信封（APISIX WAF 拦截页/空体/纯文本）：链路或出口 IP 的频控
		// 信号，不是账号凭据失效。必须在此接管——回落通用链会走 handleOpenAI403
		// 的计数升级路径，而 WAF 形态多为空体/纯文本（不享 HTML 豁免），三次即
		// SetError 永久禁号；且 403 在 failover 状态集里会逐账号重放，一个坏出口
		// 足以把整组账号打下线。处置对齐源项目 wafCooldownBase：账号软冷却
		//（优先 Retry-After 头，缺失 60s 基数，封顶 2h），本次换号、到期自愈。
		resetAt := workbuddyWafCooldown(resp.Header)
		if err := s.accountRepo.SetRateLimited(ctx, account.ID, resetAt); err != nil {
			slog.Warn("workbuddy set waf cooldown failed", "account_id", account.ID, "error", err)
		}
		appendFailover("waf_block")
		return true, s.newOpenAIAccountFailoverError(account, resp.StatusCode, resp.Header, respBody, upstreamMsg, false, false)

	case kind == workbuddy.KindClient && (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden):
		// 401/403 token 失效：触发一次强制 refresh；刷新失败由刷新侧置 error。
		// 本次仍换号（新 token 下次请求生效）。
		s.triggerWorkbuddyRequestPathRefresh(ctx, account)
		appendFailover("auth_retry")
		return true, s.newOpenAIAccountFailoverError(account, resp.StatusCode, resp.Header, respBody, upstreamMsg, false, false)

	default:
		return false, nil
	}
}

// workbuddyRateLimitResetAt 限流冷却截止时刻：上游明示重置时间优先（body 的
// 「将在 … 重置」/ Retry-After 头），否则本地兜底 10min；有线索时封顶 2h。
func workbuddyRateLimitResetAt(header http.Header, body string) time.Time {
	now := time.Now()
	if reset, ok := workbuddy.ParseRateReset(body); ok && reset.After(now) {
		if reset.Before(now.Add(workbuddyRateLimitCooldownCap)) {
			return reset
		}
		return now.Add(workbuddyRateLimitCooldownCap)
	}
	if d, ok := workbuddy.ParseRetryAfter(header); ok {
		return now.Add(d)
	}
	return now.Add(workbuddyRateLimitFallbackCooldown)
}

// workbuddyWafCooldown WAF 403 的账号软冷却截止时刻：优先 Retry-After 头
//（复用 ParseRetryAfter 的纯数字解析与 2h 上限），缺失按 wafCooldownBase
//（对齐源项目 workbuddy2api-panel 的 60s 基数）。不罚号，到期自愈。
func workbuddyWafCooldown(header http.Header) time.Time {
	if d, ok := workbuddy.ParseRetryAfter(header); ok && d > 0 {
		return time.Now().Add(d)
	}
	return time.Now().Add(workbuddyWafCooldownBase)
}

// bumpWorkbuddySessionStrikes 递增并持久化 session dead 容忍计数。
func bumpWorkbuddySessionStrikes(ctx context.Context, repo AccountRepository, account *Account) int {
	current := 0
	if account != nil && account.Extra != nil {
		switch v := account.Extra[workbuddySessionStrikesExtraKey].(type) {
		case float64:
			current = int(v)
		case int:
			current = v
		case int64:
			current = int(v)
		}
	}
	current++
	if repo != nil && account != nil {
		if err := repo.UpdateExtra(ctx, account.ID, map[string]any{workbuddySessionStrikesExtraKey: current}); err != nil {
			slog.Warn("workbuddy persist session strikes failed", "account_id", account.ID, "error", err)
		}
		if account.Extra == nil {
			account.Extra = map[string]any{}
		}
		account.Extra[workbuddySessionStrikesExtraKey] = current
	}
	return current
}

// triggerWorkbuddyRequestPathRefresh 异步触发一次强制 token 刷新（401/403 路径）。
// 刷新失败时把账号置 error（token 已不可自愈，需人工重登）。
func (s *OpenAIGatewayService) triggerWorkbuddyRequestPathRefresh(ctx context.Context, account *Account) {
	svc := workbuddyRequestPathRefresher.Load()
	if svc == nil {
		return
	}
	accountID := account.ID
	go func() {
		refreshCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if _, err := (*svc).refreshAccountCredentials(refreshCtx, account); err != nil {
			slog.Warn("workbuddy request-path refresh failed", "account_id", accountID, "error", err)
			if s.accountRepo != nil {
				if setErr := s.accountRepo.SetError(refreshCtx, accountID, "wb_token_refresh_failed: "+err.Error()); setErr != nil {
					slog.Warn("workbuddy set token-refresh error failed", "account_id", accountID, "error", setErr)
				}
			}
			return
		}
		slog.Info("workbuddy request-path refresh ok", "account_id", accountID)
	}()
}

// testWorkbuddyAccountConnection 经真实转发路径测试 workbuddy 账号连接：复用
// sendCCUpstreamRequest 的 workbuddy 分叉（realm 端点 /v2/chat/completions + 官方
// 桌面端头族 + 体归一化；上游 SSE 为 OpenAI 形态直接透传），交既有测试流解析。
// （此前的通用 CN 供应商通道会拼 {base}/v1/chat/completions 并要求 api_key，
// 对 workbuddy 私有端点双双失效。）
func (s *AccountTestService) testWorkbuddyAccountConnection(c *gin.Context, account *Account, modelID string, prompt string) error {
	ctx := c.Request.Context()
	testModelID := strings.TrimSpace(modelID)
	if testModelID == "" {
		testModelID = DefaultWorkbuddyTestModel
	}
	testModelID = account.GetMappedModel(testModelID)

	creds, err := parseWorkbuddyCredentials(account)
	if err != nil {
		return s.sendErrorAndEnd(c, err.Error())
	}
	targetURL, err := s.openaiGatewayService.rawChatCompletionsURL(account)
	if err != nil {
		return s.sendErrorAndEnd(c, err.Error())
	}

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.Flush()

	payload := createOpenAIChatCompletionsTestPayload(testModelID, prompt)
	if isBenchmarkAccountTest(c) {
		payload["max_tokens"] = modelBenchmarkOutputLimit(c)
	}
	payloadBytes, _ := json.Marshal(payload)
	s.sendEvent(c, TestEvent{Type: "test_start", Model: testModelID})
	s.sendEvent(c, TestEvent{Type: "status", Text: "正在通过 /v2/chat/completions 测试连接"})

	resp, err := s.openaiGatewayService.sendCCUpstreamRequest(ctx, c, account, targetURL, payloadBytes, true, creds.AccessToken, "", "")
	if err != nil {
		return s.sendErrorAndEnd(c, fmt.Sprintf("workbuddy chat API request failed: %s", err.Error()))
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode == http.StatusUnauthorized && s.accountRepo != nil {
			_ = s.accountRepo.SetError(ctx, account.ID, fmt.Sprintf("workbuddy chat authentication failed (401): %s", string(body)))
		}
		return s.sendErrorAndEnd(c, fmt.Sprintf("workbuddy chat API returned %d: %s", resp.StatusCode, string(body)))
	}
	// 分叉内上游 SSE 即 OpenAI 形态（流式透传），直接复用既有测试流解析。
	return s.processOpenAIChatCompletionsStream(c, resp.Body)
}
