package service

// openai_gateway_qoder.go 二开新增：raw Chat Completions 传输漏斗的 qoder 分叉。
//
// 协议收敛架构：qoder 账号 credentials 强制 api_protocol=adaptive，供应商
// profile 只声明 chat_completions 端点 → resolveUpstreamProtocol 把任意入站
// 协议（Anthropic / Responses / CC）都收敛到 chat_completions → 全部流量进
// forwardAsRawChatCompletions，本文件在该漏斗做平台分叉（上游是私有协议）：
//
//   - URL：realm → GatewayCandidates 主选主机 + agent_chat_generation 端点
//     （qoderChatURL；主选瞬时故障时按候选序换主机重试一次——COSY 签名只覆盖
//     path，换主机签名依旧有效）；
//   - 请求体：pkg/qoder.BuildAgentChatBody 以官方 baseprompt 骨架改写并
//     qoder_encode（applyQoderChatBody）；
//   - 请求头：删净默认 Bearer 头后注入 COSY 签名头族（applyQoderChatTransport，
//     Authorization: Bearer COSY.*；会话由进程级 CosySessionCache 管理，
//     token 刷新后失效重建）；
//   - 响应：上游恒为私有 SSE 信封。流式用 NewChatStream 包装为 OpenAI SSE
//     管道交既有中继；非流式本地 Aggregate 成单个 chat.completion。流内错误
//     经 FriendlyUpstreamError 合成客户端错误响应，并按错误族映射账号状态
//     （classifyQoderUpstreamFailure / applyQoderUpstreamFailurePolicy）。

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/qoder"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// qoderStructuredLogOnce 结构化工具历史首次开启的一次性日志（对齐上游
// "structured tool history: ON" 的验证观测点）。
var qoderStructuredLogOnce sync.Once

// qoderCosySessions 进程级 COSY 签名会话缓存（uid → 会话，token 变更失效重建）。
var qoderCosySessions = qoder.NewCosySessionCache()

// qoderRequestPathRefresher 指向最近构造的 QoderOAuthService，供请求路径
// 401/403 时触发一次强制 token 刷新（注册表模式，NewQoderOAuthService 构造时
// Store；网关与 oauth 服务分属不同 wire 节点，无法经构造参数直达）。
var qoderRequestPathRefresher atomic.Pointer[QoderOAuthService]

// qoderHeartbeatInterval 流式转换的心跳间隔（SSE 注释帧防中间代理空闲断连）。
const qoderHeartbeatInterval = 15 * time.Second

// qoderChatEndpointPath 是用量日志里的出站端点标识（agent_chat_generation）。
const qoderChatEndpointPath = "/algo/api/v2/service/pro/sse/agent_chat_generation"

// qoderChatURL 解析 qoder 账号的对话上游端点：realm → GatewayCandidates 主选
// 主机 + ChatPath。base_url 不参与（端点由产品线决定，运营不可配置错端点）。
func qoderChatURL(account *Account) (string, error) {
	creds, err := qoderAccountCredentials(account)
	if err != nil {
		return "", err
	}
	return qoder.GatewayCandidates(creds.NormalizedRealm())[0] + qoder.ChatPath, nil
}

// qoderCosySession 取（或建）该账号的 COSY 签名会话。
func qoderCosySession(account *Account) (*qoder.CosySession, error) {
	creds, err := qoderAccountCredentials(account)
	if err != nil {
		return nil, err
	}
	return qoderCosySessions.Get(creds.Identity())
}

// qoderAccountRealm 返回账号的规范区域名（凭据缺失时回落 cn）。
func qoderAccountRealm(account *Account) string {
	if creds, err := qoderAccountCredentials(account); err == nil {
		return creds.NormalizedRealm()
	}
	return "cn"
}

// applyQoderChatBody 把标准 OpenAI CC 请求体改写为 agent_chat_generation 请求体
// （BuildAgentChatBody：baseprompt 骨架 + 消息压平/脱敏 + 模型 key 解析 +
// qoder_encode）。返回值是上游的编码载荷（不透明字符串）。
func applyQoderChatBody(account *Account, body []byte) ([]byte, error) {
	var openAIReq map[string]any
	if err := json.Unmarshal(body, &openAIReq); err != nil {
		return nil, fmt.Errorf("parse openai body for qoder: %w", err)
	}
	sess, err := qoderCosySession(account)
	if err != nil {
		return nil, err
	}
	realm := qoderAccountRealm(account)
	encoded, err := qoder.BuildAgentChatBody(openAIReq, sess, realm)
	if err != nil {
		return nil, fmt.Errorf("build qoder agent chat body: %w", err)
	}
	// 一次性观测日志：结构化直传是否对本请求生效（与上游同一验证口径）。
	if qoder.StructuredToolHistoryEnabledForRequest(openAIReq, realm) {
		model, _ := openAIReq["model"].(string)
		qoderStructuredLogOnce.Do(func() {
			logger.L().Info("qoder structured tool history: ON",
				zap.String("model", model),
				zap.String("realm", realm),
				zap.Int64("account_id", account.ID),
			)
		})
	}
	return []byte(encoded), nil
}

// applyQoderChatTransport 在 sendCCUpstreamRequest 构建的出站请求上注入 COSY
// 签名头族：删净默认头（Bearer Authorization / 白名单透传 / 默认 UA）后由
// sess.Headers 统一设置（Authorization: Bearer COSY.*）。签名与 body 绑定，
// 调用点必须位于 body 定稿之后。放在账号级 header 覆写之前，运营覆写仍最后生效。
//
// origBody 为编码改写前的原始 OpenAI 请求体：据此解析模型名并带 realm 归一
// 为上游 key，经 x-model-key / x-model-source 头下发（qoder_sign.py L26：
// "x-model-key 决定上游模型路由"；Python open_upstream L4294 同款）。
func applyQoderChatTransport(req *http.Request, account *Account, body []byte, origBody []byte) error {
	sess, err := qoderCosySession(account)
	if err != nil {
		return err
	}
	modelKey := qoder.ResolveUpstreamKey(qoderRequestModel(origBody), qoderAccountRealm(account))
	headers := sess.Headers(string(body), req.URL.String(), modelKey, true, "")
	for key := range req.Header {
		req.Header.Del(key)
	}
	for key, values := range headers {
		for _, v := range values {
			req.Header.Add(key, v)
		}
	}
	return nil
}

// qoderTransientGatewayStatus 报告网关主机层 HTTP 状态是否属于瞬时故障
// （可换主机重试；对齐 pkg transientHTTPCodes：418 与 5xx）。
func qoderTransientGatewayStatus(code int) bool {
	return code == 418 || code >= 500
}

// sendQoderChat 发送 qoder 对话请求并做网关主机故障切换：主选主机传输失败或
// 瞬时故障（418/5xx）时换下一候选主机重试一次（重试失败回落原结果，交既有
// failover 链统一归一）。签名只覆盖 path，换主机后签名依旧有效。
func (s *OpenAIGatewayService) sendQoderChat(ctx context.Context, account *Account, firstReq *http.Request, body []byte, proxyURL string) (*http.Response, error) {
	resp, err := s.doOpenAIUpstream(firstReq, proxyURL, account)
	if err == nil && !qoderTransientGatewayStatus(resp.StatusCode) {
		return resp, nil
	}
	reason := fmt.Sprintf("transport error: %v", err)
	if err == nil {
		reason = fmt.Sprintf("http %d", resp.StatusCode)
	}
	retryReq, ok := qoderNextGatewayRequest(account, firstReq, body)
	if !ok {
		return resp, err
	}
	if resp != nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 8192))
		_ = resp.Body.Close()
	}
	logger.L().Warn("qoder gateway failover: retrying next candidate host",
		zap.Int64("account_id", account.ID),
		zap.String("first_host", firstReq.URL.Host),
		zap.String("next_host", retryReq.URL.Host),
		zap.String("reason", reason),
	)
	return s.doOpenAIUpstream(retryReq, proxyURL, account)
}

// qoderNextGatewayRequest 构造换主机后的重试请求：按候选序取当前主机的下一个，
// 复制原请求头（COSY 签名只覆盖 path，签名依旧有效）与 body。无下一候选时
// 返回 (nil, false)。
func qoderNextGatewayRequest(account *Account, req *http.Request, body []byte) (*http.Request, bool) {
	creds, err := qoderAccountCredentials(account)
	if err != nil {
		return nil, false
	}
	candidates := qoder.GatewayCandidates(creds.NormalizedRealm())
	next := ""
	for i, host := range candidates {
		parsed, perr := url.Parse(host)
		if perr != nil || parsed.Host != req.URL.Host {
			continue
		}
		if i+1 < len(candidates) {
			next = candidates[i+1]
		}
		break
	}
	if next == "" {
		return nil, false
	}
	nextParsed, perr := url.Parse(next)
	if perr != nil {
		return nil, false
	}
	nextURL := *req.URL
	nextURL.Scheme = nextParsed.Scheme
	nextURL.Host = nextParsed.Host
	retryReq, rerr := http.NewRequestWithContext(req.Context(), http.MethodPost, nextURL.String(), bytes.NewReader(body))
	if rerr != nil {
		return nil, false
	}
	retryReq.Header = req.Header.Clone()
	return retryReq, true
}

// qoderStreamBody 把管道读端包装为可 Close 的响应体：Close 取消读端，
// 让后台转换 goroutine 经写失败退出，不泄漏。
type qoderStreamBody struct {
	reader io.Reader
	closer io.Closer
	ctx    context.Context
}

func (b *qoderStreamBody) Read(p []byte) (int, error) {
	if b.ctx == nil {
		return b.reader.Read(p)
	}
	type readResult struct {
		n   int
		err error
	}
	done := make(chan readResult, 1)
	go func() {
		n, err := b.reader.Read(p)
		done <- readResult{n: n, err: err}
	}()
	select {
	case result := <-done:
		return result.n, result.err
	case <-b.ctx.Done():
		_ = b.closer.Close()
		result := <-done
		_ = result
		return 0, b.ctx.Err()
	}
}
func (b *qoderStreamBody) Close() error { return b.closer.Close() }

// wrapQoderChatResponse 把 qoder 上游响应（恒为私有 SSE 信封）转换为下游期望的
// 形态后替换 resp.Body，使既有 CC 中继（流式透传 / 非流式缓冲）无需感知 qoder：
//
//   - 客户端要流式：Body 替换为 NewChatStream 管道（OpenAI SSE + 心跳 + 必发
//     [DONE]），转换在后台 goroutine 中进行；流内错误合成错误帧 + [DONE] 转发
//     给客户端，同时做账号级分类处置。首帧若即错误信封（10605 排队等），改为
//     合成错误状态交调用方 failover 链换号重试（should_retry_envelope 语义：
//     未向客户端写出任何字节前不放弃换号）；
//   - 客户端要非流式：本地 Aggregate 成单个 chat.completion JSON；上游在流内
//     报错时按 classifyQoderUpstreamFailure 合成 HTTP 错误状态，交由调用方的
//     既有错误链（failover / 账号处置）统一处理。
//
// origBody 为编码改写前的原始 OpenAI 请求体（解析 model 与 tools 声明）。
func (s *OpenAIGatewayService) wrapQoderChatResponse(ctx context.Context, account *Account, resp *http.Response, clientStream bool, origBody []byte) error {
	if s == nil || account == nil || resp == nil || resp.Body == nil || resp.StatusCode >= 400 {
		return nil
	}
	model := strings.TrimSpace(qoderRequestModel(origBody))
	var payload map[string]any
	_ = json.Unmarshal(origBody, &payload)
	allowed := qoder.ToolNamesFromPayload(payload)

	if clientStream {
		// 先捕获上游 body 再启动转换 goroutine：goroutine 内惰性读 resp.Body
		// 会与下方 resp.Body = pipe 赋值构成数据竞争（转换器可能把输出管道当
		// 输入，自环死锁）。
		upstreamBody := resp.Body
		it := qoder.NewChatStream(upstreamBody, qoder.ChatStreamOptions{
			AllowedToolNames:  allowed,
			HeartbeatInterval: qoderHeartbeatInterval,
		})
		// 首帧同步探测（qoder_proxy.py should_retry_envelope L3917-3934：401/403/429
		// 与瞬时错误在**尚未向客户端写出任何字节**前值得重开上游换号重试）。
		// 10605 排队等错误信封恒为上游首帧：取到错误时按非流式分支合成错误状态
		// 与错误体，交调用方既有 failover 链换号（对应 Python 的 reopen），而不是
		// 把 200 + 错误帧写给客户端——首字节写出后 failover 链无法再换号。探测不
		// 改变客户端可见时序：streamRawChatCompletions 的静默拒绝门控本就把客户端
		// 输出压到首帧内容之后；慢上游时探测至多等到首个心跳帧（15s）。
		firstFrame, firstErr := it.Next()
		if firstErr != nil {
			code, detail := qoderLearnStreamError(account, model, firstErr)
			s.applyQoderUpstreamFailurePolicy(ctx, account, code, detail)
			status := classifyQoderUpstreamFailure(code, detail)
			errBody, _ := json.Marshal(qoderClientErrorPayload(code, detail))
			_ = upstreamBody.Close()
			resp.StatusCode = status
			resp.Status = fmt.Sprintf("%d %s", status, http.StatusText(status))
			resp.Body = io.NopCloser(strings.NewReader(string(errBody)))
			resp.Header.Set("Content-Type", "application/json")
			resp.Header.Del("Content-Length")
			resp.ContentLength = int64(len(errBody))
			return nil
		}
		pipeReader, pipeWriter := io.Pipe()
		go func() {
			var streamErr error
			if len(firstFrame) > 0 {
				// 首帧写失败（客户端已断开）不单独收尾：落入循环后首次写出错即
				// 退出，走下方统一收尾（关 body + 排空迭代器）。
				_, _ = pipeWriter.Write(firstFrame)
			}
			for {
				frame, err := it.Next()
				if err == io.EOF {
					break
				}
				if err != nil {
					streamErr = err
					break
				}
				if _, werr := pipeWriter.Write(frame); werr != nil {
					break // 客户端已断开，退出并释放上游
				}
			}
			_ = upstreamBody.Close()
			// 客户端提前断开时必须排空迭代器：WithHeartbeat 的读协程可能阻塞在
			// 16 深度缓冲 channel 的发送上，仅 Close 底层 body 不足以让它退出
			//（它卡在送帧而非读帧）；持续消费至错误/EOF，读协程才能在拿到读错
			// 误后自然收尾，否则 goroutine 泄漏。正常 EOF 路径迭代器已终态，
			// 首次调用即返回，无额外开销。
			for {
				if _, derr := it.Next(); derr != nil {
					break
				}
			}
			if streamErr != nil {
				code, detail := qoderLearnStreamError(account, model, streamErr)
				s.applyQoderUpstreamFailurePolicy(ctx, account, code, detail)
				frame, _ := json.Marshal(qoderClientErrorPayload(code, detail))
				_, _ = pipeWriter.Write([]byte("data: " + string(frame) + "\n\n"))
				_, _ = pipeWriter.Write([]byte("data: [DONE]\n\n"))
			}
			_ = pipeWriter.Close()
		}()
		resp.Body = &qoderStreamBody{reader: pipeReader, closer: pipeReader, ctx: ctx}
		resp.Header.Set("Content-Type", "text/event-stream")
		resp.Header.Del("Content-Length")
		return nil
	}

	usage := &qoder.UsageHolder{}
	agg, aggErr := qoder.AggregateStream(resp.Body, model, usage, allowed)
	_ = resp.Body.Close()
	if aggErr != nil {
		code, detail := qoderLearnStreamError(account, model, aggErr)
		s.applyQoderUpstreamFailurePolicy(ctx, account, code, detail)
		status := classifyQoderUpstreamFailure(code, detail)
		errBody, _ := json.Marshal(qoderClientErrorPayload(code, detail))
		resp.StatusCode = status
		resp.Status = fmt.Sprintf("%d %s", status, http.StatusText(status))
		resp.Body = io.NopCloser(strings.NewReader(string(errBody)))
		resp.Header.Set("Content-Type", "application/json")
		resp.Header.Del("Content-Length")
		resp.ContentLength = int64(len(errBody))
		return nil
	}
	payloadBytes, err := json.Marshal(qoderAggregatedToCompletion(agg, model))
	if err != nil {
		return fmt.Errorf("marshal qoder aggregated response: %w", err)
	}
	resp.Body = io.NopCloser(strings.NewReader(string(payloadBytes)))
	resp.Header.Set("Content-Type", "application/json")
	resp.Header.Del("Content-Length")
	resp.ContentLength = int64(len(payloadBytes))
	return nil
}

// qoderRequestModel 从编码改写前的请求体提取 model 字段。
func qoderRequestModel(body []byte) string {
	var obj struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &obj); err != nil {
		return ""
	}
	return obj.Model
}

// qoderClientErrorPayload 由 FriendlyUpstreamError 合成的 OpenAI 风格错误体。
func qoderClientErrorPayload(code int, detail string) map[string]any {
	message, errType := qoder.FriendlyUpstreamError(code, detail)
	return map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    errType,
			"code":    code,
		},
	}
}

// qoderAggregatedToCompletion 把 pkg 聚合结果转为标准 chat.completion 形态。
func qoderAggregatedToCompletion(agg *qoder.AggregatedChat, fallbackModel string) map[string]any {
	model := agg.Model
	if model == "" {
		model = fallbackModel
	}
	message := map[string]any{"role": "assistant", "content": agg.Message.Content}
	if agg.Message.ReasoningContent != "" {
		message["reasoning_content"] = agg.Message.ReasoningContent
	}
	if len(agg.Message.ToolCalls) > 0 {
		calls := make([]map[string]any, 0, len(agg.Message.ToolCalls))
		for _, call := range agg.Message.ToolCalls {
			typ := call.Type
			if typ == "" {
				typ = "function"
			}
			calls = append(calls, map[string]any{
				"id":   call.ID,
				"type": typ,
				"function": map[string]any{
					"name":      call.Name,
					"arguments": call.Arguments,
				},
			})
		}
		message["tool_calls"] = calls
	}
	out := map[string]any{
		"id":      agg.ID,
		"object":  "chat.completion",
		"created": agg.Created,
		"model":   model,
		"choices": []any{map[string]any{
			"index":         0,
			"message":       message,
			"finish_reason": agg.FinishReason,
		}},
	}
	if agg.Usage != nil {
		out["usage"] = agg.Usage
	}
	return out
}

// qoderStreamErrorInfo 从流内错误提取 (HTTP 状态, 详情)：信封错误取
// UpstreamStatusError 的状态与 body，其余按 502 传输类处理。
func qoderStreamErrorInfo(err error) (int, string) {
	var se *qoder.UpstreamStatusError
	if errors.As(err, &se) {
		return se.IntStatus(), se.Detail
	}
	return http.StatusBadGateway, err.Error()
}

// qoderLearnStreamError 从流内错误文本学 max_tokens 输出上限（issue #101，
// 源 record_error 的学习钩子语义）并返回 (code, detail)。学习失败不影响错误处置。
func qoderLearnStreamError(account *Account, model string, err error) (int, string) {
	code, detail := qoderStreamErrorInfo(err)
	qoder.LearnMaxOutputLimit(detail, model, qoderAccountRealm(account))
	return code, detail
}

// ---------------------------------------------------------------------------
// 错误族 → 账号状态映射。
// ---------------------------------------------------------------------------

// classifyQoderUpstreamFailure 把 qoder 上游错误（信封状态或 HTTP 状态）映射为
// 等价的 HTTP 状态码（合成客户端错误响应用）：
//
//	10605 排队（403 信封）   → 429（免费档服务排队：冷却等待，非鉴权失败）
//	401/403 / SessionDead   → 401（token 失效：强制 refresh + failover）
//	402（积分耗尽）          → 429（冷却 + 罚号，签到成功自动恢复）
//	429 / 风控冷却           → 429（SetRateLimited，尊重信封 resetAt）
//	内容审核 / 客户端参数错   → 400（确定性拒绝，透传不计 strike）
//	其余（瞬时错/未知）       → 502（既有 failover 链）
func classifyQoderUpstreamFailure(code int, detail string) int {
	switch {
	case qoder.SessionDead(detail):
		return http.StatusUnauthorized
	case qoder.IsQueuedReject(detail):
		// 10605 排队：403 信封但语义是"服务暂不可用，N 秒后重试"。
		return http.StatusTooManyRequests
	case code == http.StatusUnauthorized || code == http.StatusForbidden:
		return http.StatusUnauthorized
	case code == http.StatusPaymentRequired:
		return http.StatusTooManyRequests
	case code == http.StatusTooManyRequests:
		return http.StatusTooManyRequests
	case qoder.IsContentPolicy(detail) || qoder.IsClientFault(detail):
		return http.StatusBadRequest
	default:
		return http.StatusBadGateway
	}
}

// qoderSingleAccountPool 报告 qoder 平台当前可调度账号是否不超过 1 个
// （qoder_proxy.py open_upstream/_handle_envelope_account_cooldown 的
// single_account=(total <= 1) 口径）。真机验证：上游 429/排队是**模型级**状态
// （同一账号上 kmodel 可用而 qmodel 排队），账号级冻结会把唯一账号的可用模型
// 一起拖死——单号池对此不接冷却，让每次请求真实撞上游拿真实结果。查询失败按
// 多账号处理（保守：保留冷却语义）。
func (s *OpenAIGatewayService) qoderSingleAccountPool(ctx context.Context) bool {
	if s == nil || s.accountRepo == nil {
		return false
	}
	accounts, err := s.accountRepo.ListSchedulableByPlatform(ctx, PlatformQoder)
	if err != nil || len(accounts) > 1 {
		return false
	}
	return true
}

// qoderCoolAccountForLimit 429/10605 排队类的账号冷却：多账号池按信封
// retryAfterSeconds 冷却（缺省 30s、封顶 SoftRateMax）后换号；单账号池跳过
// （qoder_accounts.py note_error single_account：一次排队不能把唯一账号冷死）。
func (s *OpenAIGatewayService) qoderCoolAccountForLimit(ctx context.Context, account *Account, cooldown float64) {
	if s == nil || s.accountRepo == nil || account == nil {
		return
	}
	if s.qoderSingleAccountPool(ctx) {
		return
	}
	if cooldown <= 0 {
		cooldown = 30
	}
	if cooldown > qoder.SoftRateMax {
		cooldown = qoder.SoftRateMax
	}
	resetAt := time.Now().Add(time.Duration(cooldown) * time.Second)
	if err := s.accountRepo.SetRateLimited(ctx, account.ID, resetAt); err != nil {
		logger.L().Warn("qoder set rate limit failed",
			zap.Int64("account_id", account.ID), zap.Error(err))
	}
}

// applyQoderUpstreamFailurePolicy 流内错误的账号级处置（ctx 经 WithoutCancel
// 使用：客户端可能已断开，处置不能随之取消）：
//
//	token 失效 / SessionDead → 触发一次强制 refresh（失败由刷新侧置 error）；
//	402 积分耗尽             → qd_quota 清零 + status=error（签到成功自动恢复）；
//	429 / 风控冷却           → SetRateLimited（尊重信封 retryAfterSeconds）；
//	客户端错 / 瞬时错        → 不计 strike（客户端错透传；瞬时错交既有 failover 链）。
func (s *OpenAIGatewayService) applyQoderUpstreamFailurePolicy(ctx context.Context, account *Account, code int, detail string) {
	if s == nil || account == nil || s.accountRepo == nil {
		return
	}
	actionCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	switch {
	case qoder.IsQueuedReject(detail):
		// 10605 排队（403 信封，免费档服务暂不可用）：冷却 retryAfterSeconds
		//（缺省 30s，封顶 SoftRateMax），不罚号、不触发 token 刷新；单账号池
		// 跳过冷却（429/排队是模型级状态，账号级冻结会拖死唯一账号的可用模型）。
		s.qoderCoolAccountForLimit(actionCtx, account, float64(qoder.RetryAfterSeconds(detail)))
	case qoder.SessionDead(detail) || code == http.StatusUnauthorized || code == http.StatusForbidden:
		logger.L().Warn("qoder upstream rejected credentials",
			zap.Int64("account_id", account.ID),
			zap.Int("status", code),
			zap.String("detail", detail),
		)
		triggerQoderRequestPathRefresh(ctx, s.accountRepo, account)
	case code == http.StatusPaymentRequired:
		logger.L().Warn("qoder credits exhausted",
			zap.Int64("account_id", account.ID),
			zap.String("detail", detail),
		)
		zeroQoderQuotaExtra(actionCtx, s.accountRepo, account.ID)
		if err := s.accountRepo.SetError(actionCtx, account.ID, QoderNoCreditReason); err != nil {
			logger.L().Warn("qoder set no-credit error failed",
				zap.Int64("account_id", account.ID), zap.Error(err))
		} else {
			account.Status = StatusError
			account.ErrorMessage = QoderNoCreditReason
			account.Schedulable = false
		}
	case code == http.StatusTooManyRequests:
		// 软限流：冷却（尊重信封 retryAfterSeconds，封顶 SoftRateMax），不罚号；
		// 单账号池跳过（同上）。
		cooldown, _ := qoder.EnvelopeCooldown(code, detail, false, true)
		if cooldown <= 0 {
			cooldown = qoder.SoftBackoff(1)
		}
		s.qoderCoolAccountForLimit(actionCtx, account, cooldown)
	}
}

// handleQoderUpstreamError 处理 qoder 平台的 >=400 上游响应（建流前 HTTP 错误，
// pkg errors.go ClassifyDirectError 的专门路径）。返回 (handled, failoverErr)：
// handled=false 表示该类别未接管（5xx / WAF 等），调用方继续走既有通用 failover
// 链；handled=true 且 failoverErr=nil 表示不换号、按上游原文透传给客户端
// （内容审核 / 客户端参数错：确定性拒绝，重试无效，不计 strike）。
func (s *OpenAIGatewayService) handleQoderUpstreamError(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	resp *http.Response,
	respBody []byte,
	upstreamMsg string,
	upstreamModel string,
) (bool, *UpstreamFailoverError) {
	code := resp.StatusCode
	detail := string(respBody)
	// issue #101：从上游 400 文本学输出上限（下一次请求直接 clamp）。接在
	// 错误文本可得的单点上（源 record_error 的学习钩子语义），学到不落盘。
	qoder.LearnMaxOutputLimit(detail, upstreamModel, qoderAccountRealm(account))

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
			Message:            fmt.Sprintf("qoder %s: %s", kindLabel, upstreamMsg),
		})
	}

	switch {
	case qoder.IsQueuedReject(detail):
		// 10605 排队以 403 信封回传（免费档服务暂不可用），语义是"等待 N 秒后
		// 重试"，**不是**鉴权失败。必须先于下方 401/403 判定：否则排队会被当成
		// token 失效触发强制刷新（刷新必失败并置 error 罚号）且账号不冷却、
		// 下次请求继续撞排队。处置对齐流内路径（applyQoderUpstreamFailurePolicy
		// 的 IsQueuedReject 分支）与测试连接路径：冷却 retryAfterSeconds
		//（缺省 30s，封顶 SoftRateMax；单账号池跳过，见 qoderCoolAccountForLimit），
		// 不刷新、不罚号，本次换号重试。
		cooldown, _ := qoder.EnvelopeCooldown(code, detail, false, true)
		s.qoderCoolAccountForLimit(ctx, account, cooldown)
		appendFailover("queued")
		return true, s.newOpenAIAccountFailoverError(account, code, resp.Header, respBody, upstreamMsg, false, false)

	case qoder.SessionDead(detail) || code == http.StatusUnauthorized || code == http.StatusForbidden:
		// token 失效：触发一次强制 refresh（异步；失败由刷新侧置 error），本次换号
		// （新 token 下次请求生效）。
		triggerQoderRequestPathRefresh(ctx, s.accountRepo, account)
		appendFailover("auth_retry")
		return true, s.newOpenAIAccountFailoverError(account, code, resp.Header, respBody, upstreamMsg, false, false)

	case code == http.StatusPaymentRequired:
		// 积分耗尽：快照清零 + status=error（qd_no_credit）+ 不可调度；
		// checkin/balance 成功后自动恢复。
		zeroQoderQuotaExtra(ctx, s.accountRepo, account.ID)
		if s.accountRepo != nil {
			if err := s.accountRepo.SetError(ctx, account.ID, QoderNoCreditReason); err != nil {
				logger.L().Warn("qoder set no-credit error failed",
					zap.Int64("account_id", account.ID), zap.Error(err))
			} else {
				account.Status = StatusError
				account.ErrorMessage = QoderNoCreditReason
				account.Schedulable = false
			}
		}
		appendFailover("no_credit")
		return true, s.newOpenAIAccountFailoverError(account, code, resp.Header, respBody, upstreamMsg, true, false)

	case code == http.StatusTooManyRequests:
		// 软限流：冷却（尊重信封 retryAfterSeconds，封顶 SoftRateMax），不罚号。
		cooldown, _ := qoder.EnvelopeCooldown(code, detail, false, true)
		if cooldown <= 0 {
			cooldown = qoder.SoftBackoff(1)
		}
		s.qoderCoolAccountForLimit(ctx, account, cooldown)
		appendFailover("soft_rate")
		return true, s.newOpenAIAccountFailoverError(account, code, resp.Header, respBody, upstreamMsg, false, false)

	case qoder.IsContentPolicy(detail) || qoder.IsClientFault(detail):
		// 内容审核 / 客户端参数错：确定性拒绝，透传原文不计 strike（不换号）。
		// issue #72 降级记忆：参数类拒绝（400/422）且模型命中结构化白名单时，
		// 记下「该区域+模型的结构化历史被拒」——下一次请求直接文本模式
		// （StructuredToolHistoryEnabled 的运行时记忆），不再每次先撞一次 400。
		// 参数错与账号无关：不冷却、不罚号；同账号文本重发在 CC pipeline 的
		// 换号循环之外，代价高于每 TTL 一次的 400 体验，不做本次重发。
		if code == http.StatusBadRequest || code == http.StatusUnprocessableEntity {
			realm := qoderAccountRealm(account)
			if qoder.StructuredToolHistoryWhitelistHit(realm, upstreamModel) &&
				!qoder.StructuredRejected(realm, upstreamModel) {
				qoder.NoteStructuredRejected(realm, upstreamModel)
				logger.L().Warn("qoder structured tool history rejected by upstream; text mode for this model until TTL",
					zap.Int64("account_id", account.ID),
					zap.String("model", upstreamModel),
					zap.String("realm", realm),
					zap.Int("status", code),
				)
			}
		}
		logger.L().Warn("qoder client-fault (pass-through)",
			zap.Int64("account_id", account.ID),
			zap.Int("status", code),
			zap.String("message", upstreamMsg),
		)
		return true, nil

	default:
		// 5xx / 418 瞬时错及其余未知：交既有通用 failover 链（含账号 strike 治理）。
		return false, nil
	}
}

// triggerQoderRequestPathRefresh 异步触发一次强制 token 刷新（401/403 路径）。
// 刷新失败时把账号置 error（token 已不可自愈，需人工重登）。
func triggerQoderRequestPathRefresh(ctx context.Context, accountRepo AccountRepository, account *Account) {
	svc := qoderRequestPathRefresher.Load()
	if svc == nil || accountRepo == nil {
		return
	}
	accountID := account.ID
	go func() {
		refreshCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if _, err := (*svc).RefreshAccountToken(refreshCtx, account); err != nil {
			logger.L().Warn("qoder request-path refresh failed",
				zap.Int64("account_id", accountID), zap.Error(err))
			// SessionDead 在 RefreshAccountToken 内已置 error；其余刷新失败同样罚号，
			// 防止死 token 账号反复被调度。
			if !strings.Contains(err.Error(), "QODER_OAUTH_SESSION_DEAD") {
				if setErr := accountRepo.SetError(refreshCtx, accountID, "qd_token_refresh_failed: "+err.Error()); setErr != nil {
					logger.L().Warn("qoder set token-refresh error failed",
						zap.Int64("account_id", accountID), zap.Error(setErr))
				}
			}
			return
		}
		logger.L().Info("qoder request-path refresh ok", zap.Int64("account_id", accountID))
	}()
}

// ---------------------------------------------------------------------------
// /v1/models 模型目录。
// ---------------------------------------------------------------------------

// fetchQoderUpstreamModels 拉取 qoder 账号所在 realm 的模型目录（pkg FetchModels：
// 动态 /algo/api/v2/model/list + 静态快照合并，进程级 300s TTL；动态失败回退
// 内嵌目录），返回官方显示名（CatalogEntry.PublicID，动态独有条目缺显示名时
// 回落 key），按官方 UI 顺序保序输出（静态快照序，动态独有追加尾部）。chat
// 过滤由动态路径的 mergeCatalog 承担；静态回退路径依赖内嵌目录全为 chat 条目。
func (s *AccountTestService) fetchQoderUpstreamModels(ctx context.Context, account *Account) ([]string, error) {
	creds, err := qoderAccountCredentials(account)
	if err != nil {
		return nil, newUpstreamModelSyncConfigError("Invalid qoder credentials", err)
	}
	sess, err := qoderCosySessions.Get(creds.Identity())
	if err != nil {
		return nil, newUpstreamModelSyncConfigError("Build qoder cosy session failed", err)
	}
	client := qoder.NewClient(newQoderHTTPClient(upstreamModelsProxyURL(account), 30*time.Second))
	entries, err := client.FetchModels(ctx, creds.NormalizedRealm(), sess)
	if err != nil {
		return nil, newUpstreamModelSyncUpstreamError("Failed to fetch qoder model catalog", err)
	}
	// 保序去重（不用 dedupeAndSortModelIDs 的字母序，对齐 qoder2api /v1/models
	// 的官方 UI 顺序）。双区目录内显示名唯一，去重仅防御动态数据；若未来出现
	// 同名，丢后者与 ResolveUpstreamKey 的 displayIndex（首个 key 优先）语义一致。
	ids := make([]string, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		id := strings.TrimSpace(entry.PublicID())
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids, nil
}

// qoderDefaultModelIDs 是 /v1/models 的 qoder 内嵌目录兜底（按 realm，chat
// 过滤在 key 域，输出官方显示名口径；静态目录序直出）。
func qoderDefaultModelIDs(realm string) []string {
	entries, err := qoder.StaticModels(realm)
	if err != nil {
		return nil
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !qoderIsChatModelKey(entry.Key) {
			continue
		}
		ids = append(ids, entry.PublicID())
	}
	return ids
}

// DefaultQoderModelIDs 是 /v1/models 的 qoder 静态兜底清单（内嵌 cn 目录，
// chat 场景过滤，官方显示名口径；运营侧「同步模型」以账号 realm 的动态目录覆盖）。
func DefaultQoderModelIDs() []string {
	return qoderDefaultModelIDs("cn")
}

// qoderIsChatModelKey 复刻 pkg isChatModel 的过滤规则（未导出）：
// 排除 lite / codewise-* / completion-* / *-image 等非 chat key。
func qoderIsChatModelKey(key string) bool {
	if key == "" || key == "lite" {
		return false
	}
	for _, prefix := range []string{"codewise-", "completion-"} {
		if strings.HasPrefix(key, prefix) {
			return false
		}
	}
	for _, suffix := range []string{"-image-alpha", "-image-alpha-edit", "-taco-completion"} {
		if strings.HasSuffix(key, suffix) {
			return false
		}
	}
	return true
}

// testQoderAccountConnection 经真实转发路径测试 qoder 账号连接：复用
// sendCCUpstreamRequest 的 qoder 分叉（COSY 签名头族 + qoder_encode 体 + 网关主机
// 故障切换；私有 SSE 信封已在分叉内包装为 OpenAI SSE），交既有测试流解析。
func (s *AccountTestService) testQoderAccountConnection(c *gin.Context, account *Account, modelID string, prompt string) error {
	ctx := c.Request.Context()
	testModelID := strings.TrimSpace(modelID)
	if testModelID == "" {
		testModelID = DefaultQoderTestModel
	}
	testModelID = account.GetMappedModel(testModelID)

	creds, err := qoderAccountCredentials(account)
	if err != nil {
		return s.sendErrorAndEnd(c, err.Error())
	}
	targetURL, err := qoderChatURL(account)
	if err != nil {
		return s.sendErrorAndEnd(c, err.Error())
	}

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.Flush()

	payloadBytes, _ := json.Marshal(createOpenAIChatCompletionsTestPayload(testModelID, prompt))
	s.sendEvent(c, TestEvent{Type: "test_start", Model: testModelID})
	s.sendEvent(c, TestEvent{Type: "status", Text: "正在通过 agent_chat_generation 测试连接"})

	// 上游 10605 排队（免费档服务暂不可用，可出现在非 200 状态或 200 流内错误
	// 帧）：等待信封 retryAfterSeconds（封顶 45s）后重试一次，而不是把排队当
	// 失败报给管理员。
	waitIfQueued := func(detail string) bool {
		if !qoder.IsQueuedReject(detail) {
			return false
		}
		wait := qoder.RetryAfterSeconds(detail)
		if wait <= 0 || wait > 45 {
			wait = 45
		}
		s.sendEvent(c, TestEvent{Type: "status", Text: fmt.Sprintf("上游排队中，等待 %d 秒后自动重试", wait)})
		select {
		case <-time.After(time.Duration(wait) * time.Second):
			return true
		case <-ctx.Done():
			return false
		}
	}

	for attempt := 0; attempt < 2; attempt++ {
		resp, sendErr := s.openaiGatewayService.sendCCUpstreamRequest(ctx, c, account, targetURL, payloadBytes, true, creds.AccessToken, "", "")
		if sendErr != nil {
			return s.sendErrorAndEnd(c, fmt.Sprintf("qoder chat API request failed: %s", sendErr.Error()))
		}
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if attempt == 0 && waitIfQueued(string(body)) {
				continue
			}
			if (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) && s.accountRepo != nil {
				_ = s.accountRepo.SetError(ctx, account.ID, fmt.Sprintf("qoder chat authentication failed (%d): %s", resp.StatusCode, string(body)))
			}
			return s.sendErrorAndEnd(c, fmt.Sprintf("qoder chat API returned %d: %s", resp.StatusCode, string(body)))
		}
		// 分叉内私有 SSE 信封已转换为 OpenAI SSE，直接复用既有测试流解析。
		streamErr := s.processOpenAIChatCompletionsStream(c, resp.Body)
		_ = resp.Body.Close()
		if streamErr == nil {
			return nil
		}
		if attempt == 0 && waitIfQueued(streamErr.Error()) {
			continue
		}
		return streamErr
	}
	return s.sendErrorAndEnd(c, "qoder chat API kept reporting upstream queue after retry")
}
