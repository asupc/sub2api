// Package workbuddy 封装腾讯 CodeBuddy / WorkBuddy 上游（chat / billing / auth /
// growth）的协议客户端与账号动作，移植自 workbuddy2api-panel（MIT）的
// internal/upstream + internal/auth + internal/panel 上游协议层。
//
// 设计约定：
//   - 纯标准库自足；http.Client 由调用方注入（含代理 / TLS 指纹 / 超时策略），
//     本包不做全局单例；所有方法接受 context 与凭据参数，无全局可变状态。
//   - 账号池 / 冷却 / 熔断 / 会话粘性 / 调度不在本包——sub2api 已有调度体系，
//     本包只提供 ClassifyError 等判据供其映射账号状态。
//   - 上游是标准 OpenAI SSE，chat 流由 sub2api 网关既有中继处理（SSE 帧白名单
//     重建规则见 ChatStreamContext 注释，供集成参考）。
package workbuddy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// 上游基址（realm 路由表，来源：workbuddy2api-panel internal/upstream/client.go New()）。
const (
	// chatBaseCN CN chat 域（copilot.tencent.com）：chat completions、模型目录、growth 域。
	chatBaseCN = "https://copilot.tencent.com"
	// billingBaseCN CN billing 域（codebuddy.cn）：余额 / 签到 / 活跃上报。
	billingBaseCN = "https://www.codebuddy.cn"
	// webBaseCN 官网（workbuddy.cn）域：部分「任务领奖」类接口只在此域提供
	// （Web 成长中心用；CLI 域 copilot.tencent.com 的同名路径返回 400）。
	webBaseCN = "https://www.workbuddy.cn"
	// globalBase 国际版（global realm）chat/billing/web 共用基址。
	globalBase = "https://www.workbuddy.ai"
)

// 端点路径常量。
const (
	// chatCompletionsPath chat 出站路径（CN 与 global 共用 /v2 单路径）。
	// 注意：global 早期走 /console/chat/completions，但该路径挂腾讯云 WAF body
	// 内容规则（反引号 printf/whoami 等命令执行特征确定性 403）；/v2 同 base 不挂
	// 该规则，实测等价端点（workbuddy2api-panel issue #119 后固定 /v2）。
	chatCompletionsPath = "/v2/chat/completions"
	// refreshTokenPath access token 刷新端点（chatBase 域）。
	refreshTokenPath = "/v2/plugin/auth/token/refresh"

	// billing 域端点路径（billingBase + path；仅 /billing/meter/* 族按 realm 走
	// 无 /v2 前缀 → 有 /v2 前缀的 fallback，global 上游新旧路径分叉）。
	billingMeterPath   = "/billing/meter/get-user-resource"    // global 首选（国际版无 /v2 前缀）
	dailyCheckinPath   = "/billing/meter/daily-checkin"        // global 首选
	billingMeterPathV2 = "/v2/billing/meter/get-user-resource" // CN 现状 / global fallback
	dailyCheckinPathV2 = "/v2/billing/meter/daily-checkin"
)

// refreshTokenExpiresInMax refresh 响应 expiresIn 的量级上限（10 年，纯防御值：
// 实测响应恒 5184000=60d）。超限视为上游脏数据，不写 ExpiresAt（保留旧值），
// 防止过期判定永假导致 token 永不刷新反而真过期失效。
const refreshTokenExpiresInMax = 10 * 365 * 24 * time.Hour

// Client WorkBuddy 上游客户端。httpClient 由调用方注入；now 用于时间相关判定
// （token 过期 / TTL），测试可注入固定时钟。
type Client struct {
	httpClient *http.Client
	now        func() time.Time
}

// NewClient 构造客户端。httpClient 为 nil 时用缺省 http.Client（生产应注入带
// 超时与连接池策略的实例；流式 chat 建议无总时长上限、由调用方管理取消）。
func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 120 * time.Second}
	}
	return &Client{httpClient: httpClient, now: time.Now}
}

// Now 返回客户端时钟（未设置回落 time.Now）。
func (c *Client) Now() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

// httpc 返回注入的 http client。
func (c *Client) httpc() *http.Client { return c.httpClient }

// chatBase 按 realm 返回 chat/growth 域基址。CN 域支持 WB_CN_CHAT_BASE 环境变量
// 覆盖（移植自 workbuddy2api-panel 7fcc20a cn.chat_base：官方桌面端已改用
// www.workbuddy.cn，copilot.tencent.com 对部分海外出口 TLS 握手复位；空 = 回落
// 内置默认）。service 层 workbuddyChatBaseURL 同口径，两处必须同步改。
func (c *Client) chatBase(cred Credentials) string {
	if cred.IsGlobal() {
		return globalBase
	}
	if v := strings.TrimSpace(os.Getenv("WB_CN_CHAT_BASE")); v != "" {
		return strings.TrimSuffix(v, "/")
	}
	return chatBaseCN
}

// billingBase 按 realm 返回 billing 域基址。
func (c *Client) billingBase(cred Credentials) string {
	if cred.IsGlobal() {
		return globalBase
	}
	return billingBaseCN
}

// webBase 按 realm 返回官网（任务领奖类）域基址。
func (c *Client) webBase(cred Credentials) string {
	if cred.IsGlobal() {
		return globalBase
	}
	return webBaseCN
}

// billingMeterPaths 按 realm 返回 billing/meter 域路径候选序列：
// global → [无 /v2, 有 /v2]（404 时 fallback）；cn → [有 /v2]（现状逐字）。
func billingMeterPaths(cred Credentials) []string {
	if cred.IsGlobal() {
		return []string{billingMeterPath, billingMeterPathV2}
	}
	return []string{billingMeterPathV2}
}

// checkinMeterPaths 同 billingMeterPaths，针对 daily-checkin。
func checkinMeterPaths(cred Credentials) []string {
	if cred.IsGlobal() {
		return []string{dailyCheckinPath, dailyCheckinPathV2}
	}
	return []string{dailyCheckinPathV2}
}

// apiEnvelope 上游统一信封：{code, msg, data}，code != 0 视为业务错误。
type apiEnvelope struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

// truncate 截断长文本（错误信息透出用）。
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// doJSON 发请求并解信封；HTTP 非 2xx 或业务 code != 0 时返回带分类的 *Error。
// body 读失败（连接中断/截断）返回普通错误（非 *Error）——半截 body 不进
// ClassifyError，不参与账号惩罚（传输层故障不该喂熔断误罚号）。
func (c *Client) doJSON(ctx context.Context, req *http.Request) (json.RawMessage, error) {
	resp, err := c.httpc().Do(req.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode >= 400 {
		kind := classifyString(resp.StatusCode, string(raw))
		return nil, &Error{Kind: kind, Status: resp.StatusCode, Msg: truncate(string(raw), 200), RawBody: truncate(string(raw), 512)}
	}
	var env apiEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("parse failed: %w (body: %s)", err, truncate(string(raw), 120))
	}
	if env.Code != 0 {
		kind := classifyString(resp.StatusCode, env.Msg)
		if kind == KindNone {
			kind = KindClient
		}
		return nil, &Error{
			Kind: kind, Status: resp.StatusCode,
			Msg:     fmt.Sprintf("code=%d msg=%s", env.Code, truncate(env.Msg, 160)),
			RawBody: truncate(string(raw), 512),
		}
	}
	return env.Data, nil
}

// RefreshToken 刷新 access token。成功返回更新后的凭证副本（缺省字段保留旧值），
// 由调用方持久化写回；本包不改写传入的 Credentials（值类型，天然并发安全）。
//
// 端点：POST {chatBase}/v2/plugin/auth/token/refresh，头见 RefreshHeaders
// （X-Refresh-Token + X-Auth-Refresh-Source: plugin）。
// 响应缺 accessToken 视为刷新失败（需重新登录）；expiresIn 缺省保留旧过期时间
// （防刷新风暴），超过 10 年按脏值处理同样保留旧值。
func (c *Client) RefreshToken(ctx context.Context, cred Credentials) (Credentials, error) {
	if strings.TrimSpace(cred.RefreshToken) == "" {
		return cred, fmt.Errorf("no refreshToken")
	}
	endpoint := c.chatBase(cred) + refreshTokenPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return cred, err
	}
	RefreshHeaders(req, cred)

	data, err := c.doJSON(ctx, req)
	if err != nil {
		return cred, err
	}
	var tok struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ExpiresIn    int64  `json:"expiresIn"`
		Domain       string `json:"domain"`
	}
	if err := json.Unmarshal(data, &tok); err != nil || tok.AccessToken == "" {
		return cred, fmt.Errorf("refresh_failed: no accessToken in response — re-login required")
	}
	out := cred
	out.AccessToken = tok.AccessToken
	if tok.RefreshToken != "" {
		out.RefreshToken = tok.RefreshToken
	}
	if tok.Domain != "" {
		out.Domain = tok.Domain
	}
	// 缺省保留旧过期时间（实测响应恒带 expiresIn=5184000 即 60d；缺省分支纯防御）。
	if tok.ExpiresIn > 0 && time.Duration(tok.ExpiresIn)*time.Second < refreshTokenExpiresInMax {
		out.ExpiresAt = c.Now().Add(time.Duration(tok.ExpiresIn) * time.Second).Unix()
	}
	return out, nil
}

// ChatStreamContext 发 chat 请求并返回上游原始 *http.Response（标准 OpenAI SSE，
// 调用方负责 Close）。ctx 取消会立即中断在途上游调用（客户端断连联动）。
//
// 出站前对 body 做协议归一化（PrepareChatBody：强制 stream、tool_choice 归一、
// prompt_cache_key 注入等，见其注释）；global realm 额外做兜底 system 注入与
// effort 档位降级（静态表）。
//
// 错误路径：HTTP >= 400 时读尽 body、关闭连接，返回 nil 响应 + 已分类的 *Error
// （Kind 信封 + Retry-After 头解析 + RawBody 原文），调用方一次拿到分类结果，
// 不再对 body 二次分类。
//
// SSE 帧白名单重建规则（源项目 internal/upstream/sse.go normalizeFrame，本包不
// 重建、由 sub2api 中继处理，记录供集成参考）：
//   - 顶层仅保留白名单字段：id / object / created / model / system_fingerprint /
//     service_tier；缺 object 补 "chat.completion.chunk"，缺 id 补占位；
//   - delta 仅保留非空键：role / content / reasoning_content / refusal /
//     tool_calls（空数组剔除）/ function_call（name 与 arguments 全空的占位剔除）；
//   - finish_reason 空串归一为 null（上游噪声 "" 不合规）；
//   - usage 缺失补 null（保证字段恒在）；usage 内做缓存别名归一；
//   - error 帧（上游错误信封）必须绕过白名单透传——白名单会剥掉 error 字段，
//     客户端就看不到上游报错原文；
//   - tool_calls 首片保留 name 键，后续分片删除 name（流式拼接语义）；
//   - 上游强制 stream:true（非流式会被拒），网关靠 stream_options.include_usage
//     让末帧带 usage（PrepareChatBody 已注入）。
func (c *Client) ChatStreamContext(ctx context.Context, cred Credentials, body []byte, meta ChatMeta) (*http.Response, error) {
	prepared := PrepareChatBody(cred, body, meta.ConversationID)
	if cred.IsGlobal() {
		prepared = ensureConsoleSystem(prepared)
	}
	endpoint := c.chatBase(cred) + chatCompletionsPath
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(prepared))
	if err != nil {
		return nil, err
	}
	ChatHeaders(req, cred, meta)
	resp, err := c.httpc().Do(req.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		raw, rerr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		// body 读失败（掐流/截断）→ 传输层错误：半截 raw 不进 ClassifyError，
		// 否则调用方会按误判分类罚号。
		if rerr != nil {
			return nil, fmt.Errorf("read body: %w", rerr)
		}
		kind := classifyString(resp.StatusCode, string(raw))
		if kind == KindNone {
			// 防御分支（>=400 不应产生 none）：返回原文让调用方兜底。
			return nil, fmt.Errorf("upstream %d: %s", resp.StatusCode, truncate(string(raw), 200))
		}
		ue := &Error{Kind: kind, Status: resp.StatusCode, Msg: truncate(string(raw), 200), RawBody: truncate(string(raw), 512)}
		if d, ok := ParseRetryAfter(resp.Header); ok {
			ue.RetryAfter = d
		}
		return nil, ue
	}
	return resp, nil
}

// ---------------------------------------------------------------------------
// 出站请求体协议归一化（源项目 internal/upstream/payload.go + cache_key.go）。
// 移植「让请求通过 / 费用优化」的负载位与指纹脱敏（sanitize.go，防上游
// 渠道校验 11128）；tool 配对清理未移植（sub2api 网关已有自己的出站整理层）。
// ---------------------------------------------------------------------------

// gptMinMaxTokens GPT 系上游接受的 max_tokens 下限：上游 GPT 系对 max_tokens < 16
// 一律 400 code=11133 model_param_invalid（15 拒、16 过）；Claude Code 切模型时发
// 极小探针会被拒，只能在发送前抬到下限（只放宽输出上限、不改语义）。
const gptMinMaxTokens = 16

// PrepareChatBody 组装出站 chat 请求体（纯函数）：
//  1. 强制 stream:true（上游拒绝非流式）；
//  2. max_completion_tokens → max_tokens 翻译（上游只认旧字段，别名透传会被忽略
//     后回落默认输出上限 32000，长流任务被截）；
//  3. GPT 系过小 max_tokens 抬到下限 16；
//  4. stream_options 缺省补 {include_usage:true}（官方 CLI 流式必发，上游据此在
//     末帧返回 usage；显式带则不覆盖）；
//  5. tool_choice 归一化（上游该字段是 string，对象形式 400 code=11101）；
//  6. developer 角色归一为 system（上游 role 白名单不含 developer，命中 400
//     code=11128）；
//  7. image_url 字符串形态转对象形态（上游只认 OpenAI 对象形态，字符串 400
//     code=11101）；
//  8. global realm：reasoning_effort 按静态档位表降级（国际版模型多只认 high，
//     发 low/max 是非法参数 400）；
//  9. 指纹脱敏（sanitize.go：剥离 Claude Code 等客户端的 system prompt 指纹句，
//     防上游渠道校验 400 code 11128）；
//  10. 注入 prompt_cache_key（上游前缀缓存命中可降费 ~17x）。
//
// body 不可解析时原样返回（坏 body 不在这里二次错误化，交给上游报真实参数错）。
func PrepareChatBody(cred Credentials, body []byte, conversationID string) []byte {
	if len(body) == 0 {
		return body
	}
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		return body
	}
	obj["stream"] = true
	translateMaxCompletionTokens(obj)
	clampGPTMinMaxTokens(obj)
	if _, has := obj["stream_options"]; !has {
		obj["stream_options"] = map[string]any{"include_usage": true}
	}
	normalizeToolChoice(obj)
	normalizeRoles(obj)
	normalizeImageURL(obj)
	if cred.IsGlobal() {
		// global 域档位降级：静态表为基（远端目录本包不缓存），非法档位降到支持档。
		efforts, _ := globalEffortMap(nil, nil)
		normalizeReasoningEffort(obj, efforts)
	}
	// 指纹脱敏（源项目 payload.go 同位，无条件启用——源项目生产默认亦开）：
	// 上游渠道校验（400 code 11128）按逐字精确匹配拦截客户端指纹句
	// （Claude Code system prompt 身份句 / x-anthropic-billing-header 归因行 /
	// 裸 11128 反探测串等），见 sanitize.go。放在全部归一化之后、marshal 之前。
	if msgs, ok := obj["messages"].([]any); ok {
		// 思考片段剔除（源项目 payload.go dropReasoningContentParts 同位）：Codex 等
		// 客户端经 Responses→chat 转换后，助手历史里会带 reasoning_text/reasoning 片段，
		// 上游 content 类型白名单不含它们，命中 400 code=11101 "unsupported content
		// type"。思考片段只是模型私有推理，下游续写不需要，剔除无副作用。
		if cleaned, ch := dropReasoningContentParts(msgs); ch {
			obj["messages"] = cleaned
			msgs = cleaned
		}
		// 工具配对清理（源项目 tool_pairing.go 同位，无条件启用）：剔除无配对的
		// tool_call↔tool 结果与空 function.name 脏条目（上游 400 code 11133/11148
		// 会顶死整条会话），见 tool_pairing.go。放在脱敏之前——被删条目无需再净化。
		if cleaned, ch := CleanupOrphanToolCalls(msgs); ch {
			obj["messages"] = cleaned
			msgs = cleaned
		}
		sanitizeMessages(msgs)
	}
	out, err := json.Marshal(obj)
	if err != nil {
		return body
	}
	return injectPromptCacheKey(out, cred.UID, conversationID)
}

// translateMaxCompletionTokens 把 OpenAI 别名 max_completion_tokens 翻译为上游认的
// max_tokens。规则：显式 max_tokens 优先（别名只删）；别名非正数值不翻译；
// 非数值别名不翻译（原样透传由上游报参数错）。
func translateMaxCompletionTokens(obj map[string]any) {
	alias, has := obj["max_completion_tokens"]
	delete(obj, "max_completion_tokens") // 无论翻译与否，别名一律删
	if !has {
		return
	}
	if _, explicit := obj["max_tokens"]; explicit {
		return
	}
	switch v := alias.(type) {
	case float64:
		if v > 0 && v == float64(int64(v)) {
			obj["max_tokens"] = int64(v)
		}
	case int64:
		if v > 0 {
			obj["max_tokens"] = v
		}
	case int:
		if v > 0 {
			obj["max_tokens"] = int64(v)
		}
	}
}

// clampGPTMinMaxTokens 把 GPT 系模型过小的 max_tokens 抬到下限（见 gptMinMaxTokens）。
func clampGPTMinMaxTokens(obj map[string]any) {
	model, _ := obj["model"].(string)
	if !strings.Contains(strings.ToLower(model), "gpt-") {
		return
	}
	var v int64
	switch n := obj["max_tokens"].(type) {
	case float64:
		v = int64(n)
	case int64:
		v = n
	case int:
		v = int64(n)
	default:
		return
	}
	if v < gptMinMaxTokens {
		obj["max_tokens"] = int64(gptMinMaxTokens)
	}
}

// normalizeToolChoice 按上游 Go struct（string 类型）改写 OpenAI tool_choice：
//   - "none" / {"type":"none"} → 删 tool_choice + 删 tools/functions；
//   - {"type":"auto"/"required"} → 字符串 "auto"/"required"；
//   - {"type":"function","function":{"name":"x"}} → 字符串 "x"；
//   - 其他对象/非标量 → 删 tool_choice。
func normalizeToolChoice(obj map[string]any) {
	suppress := func() {
		delete(obj, "tools")
		delete(obj, "functions")
	}
	tc, present := obj["tool_choice"]
	if !present {
		return
	}
	switch v := tc.(type) {
	case string:
		if strings.EqualFold(strings.TrimSpace(v), "none") {
			delete(obj, "tool_choice")
			suppress()
		}
	case map[string]any:
		typ, _ := v["type"].(string)
		typ = strings.ToLower(strings.TrimSpace(typ))
		switch typ {
		case "none":
			delete(obj, "tool_choice")
			suppress()
		case "auto", "required":
			obj["tool_choice"] = typ
		case "function":
			name := ""
			if fn, ok := v["function"].(map[string]any); ok {
				name, _ = fn["name"].(string)
			}
			if name == "" {
				name, _ = v["name"].(string)
			}
			if name = strings.TrimSpace(name); name != "" {
				obj["tool_choice"] = name
			} else {
				obj["tool_choice"] = "auto"
			}
		default:
			delete(obj, "tool_choice")
		}
	default:
		delete(obj, "tool_choice")
	}
}

// normalizeRoles 把 messages 里的 developer 角色归一为 system（OpenAI 新规范里
// system 的别名，改写不丢语义）。只认 developer 这一个值，其余 role 原样保留。
func normalizeRoles(obj map[string]any) {
	msgs, ok := obj["messages"].([]any)
	if !ok {
		return
	}
	for _, m := range msgs {
		msg, ok := m.(map[string]any)
		if !ok {
			continue
		}
		role, ok := msg["role"].(string)
		if !ok {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(role), "developer") {
			msg["role"] = "system"
		}
	}
}

// normalizeImageURL 兼容 image_url 的字符串写法（部分客户端发 "data:..." 字符串）：
// 字符串转 {"url": 原值}；已有对象原样保留；空/缺失不补默认值。
func normalizeImageURL(obj map[string]any) {
	msgs, ok := obj["messages"].([]any)
	if !ok {
		return
	}
	for _, rawMsg := range msgs {
		msg, ok := rawMsg.(map[string]any)
		if !ok {
			continue
		}
		parts, ok := msg["content"].([]any)
		if !ok {
			continue
		}
		for _, rawPart := range parts {
			part, ok := rawPart.(map[string]any)
			if !ok || part["type"] != "image_url" {
				continue
			}
			imageURL, ok := part["image_url"].(string)
			if !ok || imageURL == "" {
				continue
			}
			part["image_url"] = map[string]any{"url": imageURL}
		}
	}
}

// dropReasoningContentParts 剔除 chat content 数组里的 reasoning_text / reasoning 片段。
//
// Codex 只走 /v1/responses：responses→chat 转换会把助手历史里的思考片段原样透传进
// /v1/chat/completions。上游 content 类型白名单不含 reasoning_text，命中即 HTTP 400
// code=11101 "unsupported content type at index 0: reasoning_text"。思考片段只是模型
// 私有推理，下游续写不需要，剔除无副作用：
//   - 删除 type 为 reasoning_text / reasoning 的片段（TrimSpace 后比较）；
//   - 若删除后 content 为空且该消息无 tool_calls，整条消息一并删除。
//
// 移植自 workbuddy2api-panel dea68c4（payload.go dropReasoningContentParts）。
func dropReasoningContentParts(msgs []any) ([]any, bool) {
	changed := false
	out := make([]any, 0, len(msgs))
	for _, rawMsg := range msgs {
		msg, ok := rawMsg.(map[string]any)
		if !ok {
			out = append(out, rawMsg)
			continue
		}
		parts, ok := msg["content"].([]any)
		if !ok {
			out = append(out, rawMsg)
			continue
		}
		msgChanged := false
		kept := make([]any, 0, len(parts))
		for _, rawPart := range parts {
			part, ok := rawPart.(map[string]any)
			if !ok {
				kept = append(kept, rawPart)
				continue
			}
			ptype, _ := part["type"].(string)
			if strings.TrimSpace(ptype) == "reasoning_text" || strings.TrimSpace(ptype) == "reasoning" {
				msgChanged = true
				continue
			}
			kept = append(kept, rawPart)
		}
		if !msgChanged {
			out = append(out, rawMsg)
			continue
		}
		changed = true
		// content 被清空：无 tool_calls 的消息整条删除（留着是一条空 content 消息，
		// 上游同样拒）；有 tool_calls 的保留（tool_calls 才是这轮的语义主体）。
		if len(kept) == 0 {
			if _, hasToolCalls := msg["tool_calls"]; !hasToolCalls {
				continue
			}
		}
		msg["content"] = kept
		out = append(out, rawMsg)
	}
	return out, changed
}

// ensureConsoleSystem global realm 兜底 system 注入（防上游 code 11-128）：
// 首条消息非 system 时在 messages 最前补一条 fallback system。body 不可解析时
// 原样返回。
func ensureConsoleSystem(body []byte) []byte {
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
			return body // 首条已是 system：不注入
		}
	}
	obj["messages"] = append([]any{map[string]any{"role": "system", "content": "You are a helpful assistant."}}, msgs...)
	out, err := json.Marshal(obj)
	if err != nil {
		return body
	}
	return out
}

// injectPromptCacheKey 在出站 body 上注入 prompt_cache_key 字段（P0 费用优化：
// 同段 8k 前缀，带 key 命中 prompt_cache_hit_tokens，费用降 ~17x）。
//
// 优先级：
//  1. body 已带 prompt_cache_key → 原值保留；
//  2. body 已带 conversation_id / conversationId → 用它做会话哈希源；
//  3. 都没有 → 用入站 conversationID 参数。
//
// 安全约束——按账号隔离：生成键格式 `wb2a-<uid8>-<convHex>`，uid8 是账号 UID
// 前 8 字符（跨账号缓存键绝不碰撞，否则会命中错账号的前缀缓存、泄露对方对话）。
// body 不可解析时原样返回。
func injectPromptCacheKey(body []byte, uid, conversationID string) []byte {
	if len(body) == 0 {
		return body
	}
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		return body
	}
	if existing, ok := obj["prompt_cache_key"].(string); ok && existing != "" {
		return body
	}
	conv := strings.TrimSpace(conversationID)
	if v := strField(obj, "conversation_id"); v != "" {
		conv = v
	} else if v := strField(obj, "conversationId"); v != "" {
		conv = v
	}
	obj["prompt_cache_key"] = buildCacheKey(uid, conv)
	out, err := json.Marshal(obj)
	if err != nil {
		return body
	}
	return out
}

// buildCacheKey 生成 `wb2a-<uid8>-<convHex>` 格式的稳定 cache key：
// convHex = sha256(uid + "|" + conversation)[:16] 的 hex（同账号同会话稳定、
// 不同会话不同）；会话源为空时仍由 uid 单独哈希（跨账号绝不碰撞）。
func buildCacheKey(uid, conversation string) string {
	uid8 := uid
	if len(uid8) > 8 {
		uid8 = uid8[:8]
	}
	if uid8 == "" {
		uid8 = "-"
	}
	sum := sha256.Sum256([]byte(uid + "|" + conversation))
	return "wb2a-" + uid8 + "-" + hex.EncodeToString(sum[:16])
}

// strField 从 map 取 string 字段，非 string 或空串返回 ""。
func strField(obj map[string]any, key string) string {
	v, ok := obj[key].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(v)
}

// ---------------------------------------------------------------------------
// billing / growth 域 JSON 请求底座。
// ---------------------------------------------------------------------------

// billingJSON 发 billing 域（billingBase）请求并解信封；body 为 nil 时不带请求体。
func (c *Client) billingJSON(ctx context.Context, cred Credentials, method, path string, body any) (json.RawMessage, error) {
	req, err := c.newJSONRequest(ctx, method, c.billingBase(cred)+path, body)
	if err != nil {
		return nil, err
	}
	BillingHeaders(req, cred)
	return c.doJSON(ctx, req)
}

// billingMeterJSON 仅对 /billing/meter 族端点按 realm 走双路径 fallback：
// global 先无 /v2 前缀，KindNotFound 时二次换有 /v2 前缀；cn 单路径。
func (c *Client) billingMeterJSON(ctx context.Context, cred Credentials, paths []string, method string, body any) (json.RawMessage, error) {
	var lastErr error
	for i, path := range paths {
		data, err := c.billingJSON(ctx, cred, method, path, body)
		if err == nil {
			return data, nil
		}
		lastErr = err
		// 仅 404 换路径（路径不存在才值得 fallback）；其他错误直接返回。
		var ue *Error
		if !errors.As(err, &ue) || ue.Kind != KindNotFound || i == len(paths)-1 {
			return nil, err
		}
	}
	return nil, lastErr
}

// billingRetryDelay 签到/余额等维护类计费调用瞬时错误重试的间隔基数
// （生产固定 2s：第 1 次重试等 2s、第 2 次等 4s；测试可缩短）。
var billingRetryDelay = 2 * time.Second

// isTransientBillingErr 报告 err 是否值得对计费维护类调用做有界重试：
// 上游 5xx（KindServer）或网络层错误（非 *Error 的传输失败）。业务错误
// （已签到/参数错、4xx、限流）不重试——重试只会原样再失败一次。
func isTransientBillingErr(err error) bool {
	if err == nil {
		return false
	}
	var ue *Error
	if errors.As(err, &ue) {
		return ue.Kind == KindServer
	}
	return true
}

// retryBillingTransient 对签到/余额这类低频维护调用做瞬时错误有界重试：
// 最多补打 2 次，首次成功或非瞬时错误立即返回。chat 热路径不用本策略——
// 它有自己的换号轮转语义，重试会放大在途请求。
func (c *Client) retryBillingTransient(ctx context.Context, fn func() error) error {
	err := fn()
	if err == nil || !isTransientBillingErr(err) || ctx.Err() != nil {
		return err
	}
	for i := 1; i <= 2; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(i) * billingRetryDelay):
		}
		if err = fn(); err == nil || !isTransientBillingErr(err) || ctx.Err() != nil {
			return err
		}
	}
	return err
}

// newJSONRequest 构造 JSON 请求（body 为 nil 时不带请求体；json.RawMessage 直发）。
func (c *Client) newJSONRequest(ctx context.Context, method, url string, body any) (*http.Request, error) {
	var rdr io.Reader
	if body != nil {
		switch v := body.(type) {
		case json.RawMessage:
			rdr = bytes.NewReader(v)
		default:
			raw, err := json.Marshal(body)
			if err != nil {
				return nil, err
			}
			rdr = bytes.NewReader(raw)
		}
	}
	return http.NewRequestWithContext(ctx, method, url, rdr)
}
