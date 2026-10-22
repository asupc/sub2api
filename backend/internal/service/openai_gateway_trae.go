package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/trae"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// 本文件是 raw Chat Completions 传输漏斗的 trae 平台分叉（协议实现见
// internal/pkg/trae）。trae 账号 credentials 强制 api_protocol=adaptive，任意
// 入站协议（/v1/chat/completions、/v1/responses、/v1/messages）都经
// resolveUpstreamProtocol 收敛到 chat_completions，再由 sendCCUpstreamRequest
// 里的平台分叉接管真实上游：
//
//   - URL：变体端点表的 AgentHost + /api/agent/v3/llm_utils_chat（traeChatURL）；
//   - 请求体：pkg/trae.PrepareLLMChatBody 改写（content 数组化 / tools 参数字符串化 /
//     固定字段覆写，上游恒为流式）；
//   - 请求头：删净默认 Bearer 头后注入 Cloud-IDE-JWT 头族（applyTraeChatHeaders）；
//   - 响应：流式用 StreamConvert 包装为 OpenAI SSE 透传给既有中继；非流式在本地
//     Aggregate 成单个 chat.completion（上游不支持非流式）。流内 error 事件按
//     classifyTraeUpstreamFailure 映射为合成 HTTP 状态，交既有 failover / 账号
//     状态链处理。

// traeAccountCredentials 从账号 credentials 解析 trae 契约字段并补齐身份标识。
func traeAccountCredentials(account *Account) (*trae.Credentials, error) {
	if account == nil {
		return nil, errors.New("trae account is nil")
	}
	if account.Platform != PlatformTrae {
		return nil, fmt.Errorf("account %d is not a trae account", account.ID)
	}
	creds, err := trae.ParseCredentials(map[string]string{
		"jwt":                account.GetCredential("jwt"),
		"refresh_token":      account.GetCredential("refresh_token"),
		"expires_at":         account.GetCredential("expires_at"),
		"variant":            account.GetCredential("variant"),
		"uid":                account.GetCredential("uid"),
		"device_id":          account.GetCredential("device_id"),
		"machine_id":         account.GetCredential("machine_id"),
		"device_private_key": account.GetCredential("device_private_key"),
	})
	if err != nil {
		return nil, err
	}
	creds.JWT = trae.NormalizeJWT(creds.JWT)
	creds.EnsureIdentity()
	return creds, nil
}

// traeChatURL 解析 trae 账号的对话上游端点：按 variant 的区域端点表取
// AgentHost（mchost 域），拼 llm_utils_chat 路径。base_url 不参与（端点由
// 产品线决定，运营不可配置错端点）。
func traeChatURL(account *Account) (string, error) {
	creds, err := traeAccountCredentials(account)
	if err != nil {
		return "", err
	}
	return creds.VariantOf().Endpoints().AgentHost + trae.LLMChatPath, nil
}

// applyTraeChatBody 把标准 OpenAI CC 请求体改写为 llm_utils_chat 请求体
// （pkg/trae.PrepareLLMChatBody：模型名映射、content/tools 形态、固定字段）。
func applyTraeChatBody(account *Account, body []byte) ([]byte, error) {
	creds, err := traeAccountCredentials(account)
	if err != nil {
		return nil, err
	}
	variant := creds.VariantOf()
	// Max Mode（issue #53 三版）：顺带确保官方模型目录新鲜（24h TTL、单飞、
	// 静默失败）——目录里的 max > dev 决定本次请求是否注入 is_max_mode。
	// 异步进行：目录未同步时本次按默认窗口出站（不猜数字），下批请求生效。
	if jwt := strings.TrimSpace(creds.JWT); jwt != "" {
		go trae.EnsureCatalog(context.Background(), nil, variant.Region(), jwt)
	}
	return trae.PrepareLLMChatBody(body, variant, trae.DefaultModel, creds.UID, creds.DeviceID, creds.MachineID), nil
}

// applyTraeChatHeaders 删净 sendCCUpstreamRequest 已设置的默认头（Bearer
// Authorization、白名单透传头、默认 UA）后注入 Cloud-IDE-JWT 头族。调用点位于
// 账号级 header 覆写之前，运营覆写仍拥有最高优先级。
func applyTraeChatHeaders(header http.Header, account *Account) error {
	creds, err := traeAccountCredentials(account)
	if err != nil {
		return err
	}
	for key := range header {
		header.Del(key)
	}
	header.Set("Content-Type", "application/json")
	// 上游只支持流式；stream:false 由 wrapTraeChatResponse 本地聚合。
	header.Set("Accept", "text/event-stream")
	header.Set("Authorization", trae.AuthorizationHeader(creds.JWT))
	header.Set("User-Agent", trae.UserAgent())
	header.Set("X-Device-Id", creds.DeviceID)
	header.Set("X-Market-User-Id", creds.UID)
	return nil
}

// traeStreamBody 把管道读端包装为可 Close 的响应体：Close 取消读端，
// 让后台转换 goroutine 经写失败退出，不泄漏。
type traeStreamBody struct {
	reader io.Reader
	closer io.Closer
}

func (b *traeStreamBody) Read(p []byte) (int, error) { return b.reader.Read(p) }
func (b *traeStreamBody) Close() error               { return b.closer.Close() }

// wrapTraeChatResponse 把 trae 上游响应（恒为 SOLO 私有 SSE）转换为下游期望的
// 形态后替换 resp.Body，使既有 CC 中继（流式透传 / 非流式缓冲）无需感知 trae：
//
//   - 客户端要流式：Body 替换为 StreamConvert 管道（OpenAI SSE），转换在后台
//     goroutine 中进行；流内 error 事件转发给客户端的同时做账号级分类；
//   - 客户端要非流式：本地 Aggregate 成单个 chat.completion JSON；上游在流内
//     报错时按 classifyTraeUpstreamFailure 合成 HTTP 错误状态，交由调用方的
//     既有错误链（failover / 账号处置）统一处理。
//
// model 为回显进响应体的模型名（取出站请求体的 model 字段）。
func (s *OpenAIGatewayService) wrapTraeChatResponse(ctx context.Context, account *Account, resp *http.Response, clientStream bool, model string) error {
	if s == nil || account == nil || resp == nil || resp.Body == nil || resp.StatusCode >= 400 {
		return nil
	}
	chatID := "chatcmpl-" + trae.RandomHex(16)

	if clientStream {
		// 先取出原始上游 body：goroutine 若按引用读 resp.Body，会拿到下方替换后的
		// 管道自身（自读自写死锁）。
		upstreamBody := resp.Body
		pipeReader, pipeWriter := io.Pipe()
		go func() {
			outcome := trae.StreamConvert(upstreamBody, chatID, model, func(line string) error {
				_, err := pipeWriter.Write([]byte(line))
				return err
			})
			_ = upstreamBody.Close()
			if outcome.Error != nil {
				s.handleTraeUpstreamStreamError(ctx, account, outcome.Error)
			}
			_ = pipeWriter.Close()
		}()
		resp.Body = &traeStreamBody{reader: pipeReader, closer: pipeReader}
		resp.Header.Set("Content-Type", "text/event-stream")
		resp.Header.Del("Content-Length")
		return nil
	}

	response, upstreamErr, _ := trae.Aggregate(resp.Body, chatID, model)
	_ = resp.Body.Close()
	if upstreamErr != nil {
		status := classifyTraeUpstreamFailure(upstreamErr.Code, upstreamErr.Message)
		errBody, _ := json.Marshal(trae.ErrorBody(strconv.FormatInt(upstreamErr.Code, 10), upstreamErr.Message))
		resp.StatusCode = status
		resp.Status = fmt.Sprintf("%d %s", status, http.StatusText(status))
		resp.Body = io.NopCloser(bytes.NewReader(errBody))
		resp.Header.Set("Content-Type", "application/json")
		resp.Header.Del("Content-Length")
		resp.ContentLength = int64(len(errBody))
		return nil
	}
	payload, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("marshal trae aggregated response: %w", err)
	}
	resp.Body = io.NopCloser(bytes.NewReader(payload))
	resp.Header.Set("Content-Type", "application/json")
	resp.Header.Del("Content-Length")
	resp.ContentLength = int64(len(payload))
	return nil
}

// classifyTraeUpstreamFailure 把 trae 上游错误（流内 error 事件的业务码或
// HTTP 状态）映射为等价的 HTTP 状态码：
//
//	401 / token 失效           → 401（既有链：failover + 账号处置）
//	20403 / 040036 设备不匹配   → 403
//	429 / 配额与积分类          → 429（冷却而非禁用）
//	4001 / 4023 参数与模型名    → 400（客户端错误，透传不换号）
//	其余                        → 502（failover）
func classifyTraeUpstreamFailure(code int64, message string) int {
	msg := strings.ToLower(message)
	switch {
	case code == 401 || strings.Contains(msg, "unauthorized") || strings.Contains(msg, "token expired"):
		return http.StatusUnauthorized
	case code == 20403 || code == 40036 || strings.Contains(msg, "device not match"):
		return http.StatusForbidden
	case code == 429 || strings.Contains(msg, "rate limit"):
		return http.StatusTooManyRequests
	case strings.Contains(msg, "quota") || strings.Contains(msg, "credit") || strings.Contains(msg, "insufficient"):
		return http.StatusTooManyRequests
	case code == 4001 || code == 4023 || strings.Contains(msg, "param is invalid") || strings.Contains(msg, "model is unknown"):
		return http.StatusBadRequest
	default:
		return http.StatusBadGateway
	}
}

// handleTraeUpstreamStreamError 处理流式转换中发现的流内错误：仅对凭据失效
// 类（合成 401）做账号级处置，其余记录日志交由观测。ctx 经 WithoutCancel
// 使用（客户端可能已断开，drain 阶段的处置不能随之取消）。
func (s *OpenAIGatewayService) handleTraeUpstreamStreamError(ctx context.Context, account *Account, upstreamErr *trae.UpstreamError) {
	if upstreamErr == nil {
		return
	}
	status := classifyTraeUpstreamFailure(upstreamErr.Code, upstreamErr.Message)
	logger.L().Warn("trae chat stream reported upstream error",
		zap.Int64("account_id", account.ID),
		zap.Int64("upstream_code", upstreamErr.Code),
		zap.Int("mapped_status", status),
		zap.String("message", upstreamErr.Message),
	)
	if status != http.StatusUnauthorized || s.accountRepo == nil {
		return
	}
	actionCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	msg := fmt.Sprintf("trae jwt invalid (code=%d): %s", upstreamErr.Code, upstreamErr.Message)
	if err := s.accountRepo.SetError(actionCtx, account.ID, msg); err != nil {
		logger.L().Warn("trae chat stream: mark account error failed",
			zap.Int64("account_id", account.ID),
			zap.Error(err),
		)
	}
}

// DefaultTraeModelIDs 是 /v1/models 的 trae 静态兜底清单（pkg/trae 的静态
// 20 条，按客户端清单口径保序）。
func DefaultTraeModelIDs() []string {
	return append([]string(nil), trae.ModelNames...)
}

// testTraeAccountConnection 经真实转发路径测试 trae 账号连接：复用
// sendCCUpstreamRequest 的 trae 分叉（体改写 + Cloud-IDE-JWT 头族 + SOLO SSE
// →OpenAI SSE 转换），响应流直接交给既有的 OpenAI 测试流解析器。
func (s *AccountTestService) testTraeAccountConnection(c *gin.Context, account *Account, modelID string, prompt string) error {
	ctx := c.Request.Context()
	testModelID := strings.TrimSpace(modelID)
	if testModelID == "" {
		testModelID = trae.DefaultModel
	}
	testModelID = account.GetMappedModel(testModelID)

	creds, err := traeAccountCredentials(account)
	if err != nil {
		return s.sendErrorAndEnd(c, err.Error())
	}
	targetURL, err := traeChatURL(account)
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
	s.sendEvent(c, TestEvent{Type: "status", Text: "正在通过 llm_utils_chat 测试连接"})

	resp, err := s.openaiGatewayService.sendCCUpstreamRequest(ctx, c, account, targetURL, payloadBytes, true, creds.JWT, "", "")
	if err != nil {
		return s.sendErrorAndEnd(c, fmt.Sprintf("trae chat API request failed: %s", err.Error()))
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode == http.StatusUnauthorized && s.accountRepo != nil {
			_ = s.accountRepo.SetError(ctx, account.ID, fmt.Sprintf("trae chat authentication failed (401): %s", string(body)))
		}
		return s.sendErrorAndEnd(c, fmt.Sprintf("trae chat API returned %d: %s", resp.StatusCode, string(body)))
	}
	// 分叉内已把 SOLO SSE 转换为 OpenAI SSE，直接复用既有测试流解析。
	return s.processOpenAIChatCompletionsStream(c, resp.Body)
}
