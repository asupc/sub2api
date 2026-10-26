package handler

// gateway_workbuddy_search.go 二开新增：WorkBuddy 联网搜索端点 POST /v1/search/messages。
//
// 移植自 workbuddy2api-panel internal/server/messages.go + search.go 的入口部分
// （commit 4f31005 / 4f31005 修正见 ee9db99）：
//   - Anthropic Messages 兼容形态：DSH 等客户端的搜索 provider 按 ${baseURL}/messages
//     发请求，网关把 baseURL 指到 <网关>/v1/search 即可零代码接入；
//   - 刻意不占用 /v1/messages：那是 Anthropic 模型对话标准路径（so 已有），搜索挂在
//     /v1/search/messages，互不遮蔽；
//   - 只服务搜索：未声明 web_search 工具的请求直接 400，不把用户消息当搜索词；
//   - 搜索词从最后一条带文本的消息提取并剥掉固定前缀
//     `Perform a web search for the query:`（不拼接历史，见 extractMessagesQuery）；
//   - 响应按官方契约组装：web_search_tool_result 承载 url/title，摘要走 text block 的
//     citations[].cited_text（Anthropic 的 web_search_result 本身不带摘要）。
//
// 鉴权/记账形态对齐 so 网关既有独立搜索端点（gateway_web_search.go 的 grok
// web_search）：API Key 鉴权 + 计费资格 + 在途预留 + 安全审计 + RecordUsage
// （SearchCount=1 按次口径，无 token 消耗）。

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/workbuddy"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// workbuddySearchBodyLimit Messages 请求体上限：只含 query 与工具声明，1MB 富余。
const workbuddySearchBodyLimit = 1 << 20

// workbuddySearchQueryPrefix 辅助搜索模型收到的固定指令前缀（DSH/官方 CLI 的约定）。
// 它是给模型看的指令，不是搜索词的一部分，必须剥掉后再送去搜索。
const workbuddySearchQueryPrefix = "Perform a web search for the query:"

// workbuddySearchModelLabel 记账/日志里「模型」列的标识。搜索不是模型调用，
// 但该列留空会让运维无法区分这类流量与异常请求（对齐源项目 searchToolLabel 语义）。
const workbuddySearchModelLabel = "workbuddy-web-search"

// workbuddySearchMessagesRequest Anthropic Messages 请求里本端点关心的字段。
type workbuddySearchMessagesRequest struct {
	Model    string                        `json:"model"`
	Tools    []workbuddySearchMessagesTool `json:"tools"`
	Messages []workbuddySearchTurnItem     `json:"messages"`
}

// workbuddySearchMessagesTool 工具声明。搜索工具两种形态都认：
// `{type:"web_search_20250305"}` 与 `{name:"web_search"}`（部分客户端的简化形态）。
type workbuddySearchMessagesTool struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

// workbuddySearchTurnItem 单条消息。content 既可能是字符串，也可能是 block 数组，
// 用 RawMessage 保留原始形态交给 extractWorkbuddySearchQuery 处理。
type workbuddySearchTurnItem struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// workbuddySearchContentBlock Messages content 数组里的文本块。
type workbuddySearchContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// workbuddyDeclaresWebSearch 判断请求是否声明了 web_search server tool。
// 判定口径与 DSH 内置 provider 的客户端实现一致：type 以 "web_search" 开头，
// 或 name 恰为 "web_search"。
func workbuddyDeclaresWebSearch(tools []workbuddySearchMessagesTool) bool {
	for _, t := range tools {
		if strings.HasPrefix(t.Type, "web_search") || t.Name == "web_search" {
			return true
		}
	}
	return false
}

// extractWorkbuddySearchQuery 从 Messages 请求里还原真实搜索词。
//
// 约定（DSH / 官方 CLI 相同）：请求里恰好一条 user 消息，文本为
// `Perform a web search for the query: <query>`。前缀必须剥掉，否则会把指令本身
// 当成搜索词送去上游。
//
// **只取最后一条带文本的消息**，不拼接整段历史。拼接会在客户端带多轮上下文时把
// 历史问句一起当搜索词送上游（且前缀因不在开头而剥不掉，最终搜的是一段垃圾）；
// 「最后一次提问」才是此处唯一正确的语义（ee9db99 review 修复项）。
func extractWorkbuddySearchQuery(items []workbuddySearchTurnItem) string {
	joined := ""
	for _, m := range items {
		if len(m.Content) == 0 {
			continue
		}
		// content 为纯字符串（Anthropic 允许的简写形态）。
		var plain string
		if err := json.Unmarshal(m.Content, &plain); err == nil {
			if t := strings.TrimSpace(plain); t != "" {
				joined = t
			}
			continue
		}
		var blocks []workbuddySearchContentBlock
		if err := json.Unmarshal(m.Content, &blocks); err != nil {
			continue
		}
		var text []string
		for _, b := range blocks {
			if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
				text = append(text, b.Text)
			}
		}
		// 同一条消息内的多个 text block 拼接（一条消息被拆块是合法的）。
		if t := strings.TrimSpace(strings.Join(text, "\n")); t != "" {
			joined = t
		}
	}
	// 前缀大小写不敏感地剥离：不同客户端的模板大小写可能有差异。
	if len(joined) >= len(workbuddySearchQueryPrefix) &&
		strings.EqualFold(joined[:len(workbuddySearchQueryPrefix)], workbuddySearchQueryPrefix) {
		joined = strings.TrimSpace(joined[len(workbuddySearchQueryPrefix):])
	}
	return joined
}

// workbuddySearchResultItem 响应里 `web_search_tool_result.content[]` 的单项。
// 注意：Anthropic 原生不带 snippet，摘要走 text 块的 citations，故这里只有 url/title。
type workbuddySearchResultItem struct {
	Type  string `json:"type"`
	URL   string `json:"url"`
	Title string `json:"title,omitempty"`
}

// workbuddySearchToolResultBlock 搜索结果块。
type workbuddySearchToolResultBlock struct {
	Type      string                      `json:"type"`
	ToolUseID string                      `json:"tool_use_id"`
	Content   []workbuddySearchResultItem `json:"content"`
}

// workbuddySearchCitation 引用条目（snippet 的唯一承载处）。
type workbuddySearchCitation struct {
	Type      string `json:"type"`
	URL       string `json:"url"`
	CitedText string `json:"cited_text"`
}

// workbuddySearchTextBlock 承载 citations 的文本块。
type workbuddySearchTextBlock struct {
	Type      string                    `json:"type"`
	Text      string                    `json:"text"`
	Citations []workbuddySearchCitation `json:"citations,omitempty"`
}

// workbuddySearchUsage Anthropic 响应必带的 usage。搜索不消耗模型 token，故为 0。
type workbuddySearchUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// workbuddySearchMessagesResponse Anthropic Messages 响应。
type workbuddySearchMessagesResponse struct {
	ID           string               `json:"id"`
	Type         string               `json:"type"`
	Role         string               `json:"role"`
	Model        string               `json:"model"`
	Content      []any                `json:"content"`
	StopReason   string               `json:"stop_reason"`
	StopSequence any                  `json:"stop_sequence"`
	Usage        workbuddySearchUsage `json:"usage"`
}

// buildWorkbuddySearchResponse 把上游搜索结果渲染成 Anthropic Messages 响应。
//
// 结构契约（DSH 内置 provider 的 mapAnthropicResponse 逐条要求）：
//   - 必须至少有一个 type="web_search_tool_result" 的 block，否则客户端报
//     WEB_PROVIDER_ERROR；
//   - 该 block 的 content[] 每项 type="web_search_result"，url 非空；
//   - snippet 只能从 text block 的 citations[].cited_text 取（web_search_result
//     本身不带摘要），故有摘要时追加一个 text block 承载引用。
func buildWorkbuddySearchResponse(query, model string, out *workbuddy.SearchOutcome) workbuddySearchMessagesResponse {
	items := make([]workbuddySearchResultItem, 0, len(out.Results))
	citations := make([]workbuddySearchCitation, 0, len(out.Results))
	for _, r := range out.Results {
		title := r.Title
		if title == "" {
			title = r.URL // 无标题时用 URL 兜底，客户端渲染 title ?? hostname 才不会空
		}
		items = append(items, workbuddySearchResultItem{
			Type:  "web_search_result",
			URL:   r.URL,
			Title: title,
		})
		if r.Snippet != "" {
			citations = append(citations, workbuddySearchCitation{
				Type:      "web_search_result_location",
				URL:       r.URL,
				CitedText: r.Snippet,
			})
		}
	}
	if model == "" {
		model = "web-search"
	}
	// tool_use_id 与 msg id 每次调用都必须唯一：Anthropic 协议里它们标识的是
	// 「一次工具调用」，不是「一次查询的内容」。用 query 哈希会让同一 query 的
	// 重复搜索返回同一个 id（下游按 id 缓存/去重会互相覆盖），32 位哈希本身也可
	// 能碰撞。随机 16 hex 兼顾唯一性与可读性（ee9db99 review 修复项）。
	callID := workbuddySearchRandomHex(16)
	content := []any{workbuddySearchToolResultBlock{
		Type:      "web_search_tool_result",
		ToolUseID: "srvtoolu_" + callID,
		Content:   items,
	}}
	if len(citations) > 0 {
		content = append(content, workbuddySearchTextBlock{Type: "text", Text: "", Citations: citations})
	}
	return workbuddySearchMessagesResponse{
		ID:           "msg_wb2api_" + callID,
		Type:         "message",
		Role:         "assistant",
		Model:        model,
		Content:      content,
		StopReason:   "end_turn",
		StopSequence: nil,
		Usage:        workbuddySearchUsage{InputTokens: 0, OutputTokens: 0},
	}
}

// workbuddySearchRandomHex 返回 n 字节随机数的 hex 串。熵源失败时回落时间+计数派生值：
// 仍保证非空且每次调用都不同（id 唯一性是这里的硬要求）。
func workbuddySearchRandomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%0*x", n*2, uint64(time.Now().UnixNano())^atomic.AddUint64(&workbuddySearchIDSeq, 1))
	}
	return hex.EncodeToString(b)
}

// workbuddySearchIDSeq 熵源故障时的兜底计数器（正常路径不使用）。
var workbuddySearchIDSeq uint64

// workbuddySearchMetaFromRequest 从入站请求头提取会话头族并补全聚合主键。
//
// 搜索通常由某个会话内的 agent 发起，入站若带 X-Conversation-ID /
// X-Conversation-Request-ID / X-Trace-ID 必须原样透传给上游——上游按会话聚合
// 搜索流量，缺失会被视为请求不完整。客户端没带时才本地生成聚合主键；
// ConversationID 保持空串（不伪造一个假会话，避免污染后台真实会话聚合）。
// 同一次搜索的换号重试属于同一请求，必须复用同一组会话 ID（在轮转循环外构造）。
func workbuddySearchMetaFromRequest(r *http.Request) workbuddy.ChatMeta {
	meta := workbuddy.ChatMeta{
		ConversationID:        strings.TrimSpace(r.Header.Get("X-Conversation-ID")),
		ConversationRequestID: strings.TrimSpace(r.Header.Get("X-Conversation-Request-ID")),
		TraceID:               strings.TrimSpace(r.Header.Get("X-Trace-ID")),
	}
	if meta.ConversationRequestID == "" {
		meta.ConversationRequestID = workbuddy.NewMessageID()
	}
	return meta
}

// WorkbuddySearchMessages 处理 POST /v1/search/messages（Anthropic Messages 兼容搜索端点）。
//
// 鉴权由 /v1 网关中间件链（apiKeyAuth + requireGroupAnthropic）保证；此处再做
// 计费资格/在途预留/安全审计，与 grok web_search 端点同口径。
func (h *GatewayHandler) WorkbuddySearchMessages(c *gin.Context) {
	requestStart := time.Now()

	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok || apiKey == nil {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "API key required")
		return
	}
	if apiKey.Group == nil || apiKey.Group.Platform != service.PlatformWorkbuddy {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error",
			"web search is only supported for workbuddy groups")
		return
	}

	body, err := io.ReadAll(io.LimitReader(c.Request.Body, workbuddySearchBodyLimit))
	if err != nil {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "failed to read request body")
		return
	}
	var req workbuddySearchMessagesRequest
	if err := json.Unmarshal(body, &req); err != nil {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "request body must be a JSON object")
		return
	}
	// 只服务搜索：没有声明 web_search 的 Messages 请求属于模型对话，不该打到本端点
	// （打错端点时给明确错误，而不是静默把用户消息当搜索词）。
	if !workbuddyDeclaresWebSearch(req.Tools) {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error",
			"request does not declare the web_search tool; this endpoint only serves web search, not chat")
		return
	}
	query := extractWorkbuddySearchQuery(req.Messages)
	if len(query) < workbuddy.SearchMinQueryLen {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error",
			fmt.Sprintf("could not read a search query from the request (need at least %d characters)", workbuddy.SearchMinQueryLen))
		return
	}

	reqLog := requestLogger(c, "handler.gateway.workbuddy_search",
		zap.Int64("user_id", apiKey.User.ID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("group_id", apiKey.GroupID),
		zap.String("model", workbuddySearchModelLabel),
	)
	setOpsRequestContext(c, workbuddySearchModelLabel, false)
	setOpsEndpointContext(c, "", int16(service.RequestTypeSync))

	subject, _ := middleware2.GetAuthSubjectFromContext(c)
	subscription, _ := middleware2.GetSubscriptionFromContext(c)

	if err := h.billingCacheService.CheckBillingEligibility(c.Request.Context(), apiKey.User, apiKey, apiKey.Group, subscription, service.QuotaPlatform(c.Request.Context(), apiKey)); err != nil {
		status, code, message, retryAfter := billingErrorDetails(err)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		h.errorResponse(c, status, code, message)
		return
	}

	// 余额模式在途预留（与计费同口径估算；计费任务扣减余额缓存后才释放）。
	// SearchCalls=1：搜索无 token 消耗，按分组 search_price_per_1k 按次预留。
	inflightDone, inflightErr := reserveInflightBalance(c, h.billingCacheService, h.gatewayService, apiKey, subscription, service.InflightEstimateRequest{
		Model:       workbuddySearchModelLabel,
		Kind:        service.InflightEstimatePerRequest,
		SearchCalls: 1,
	})
	if inflightErr != nil {
		status, code, message, retryAfter := billingErrorDetails(inflightErr)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		h.errorResponse(c, status, code, message)
		return
	}
	defer inflightDone()

	// 安全审计：搜索词按 user 消息口径送审（与 grok web_search 同形态）。
	auditBody, _ := json.Marshal(map[string]any{
		"messages": []map[string]any{{
			"role": "user", "content": query,
		}},
	})
	if decision := h.checkSecurityAudit(c, reqLog, apiKey, subject, service.ContentModerationProtocolOpenAIChat, workbuddySearchModelLabel, auditBody); decision != nil && !decision.AllowNextStage {
		status := decision.HTTPStatus
		if status == 0 {
			status = http.StatusForbidden
		}
		code := decision.ErrorCode
		if code == "" {
			code = "content_policy_violation"
		}
		msg := decision.ClientMessage
		if msg == "" {
			msg = "Request blocked by content policy"
		}
		h.errorResponse(c, status, code, msg)
		return
	}

	// 轮转执行（service 层）：选号/占槽/上游调用/错误处置/换号重试。
	meta := workbuddySearchMetaFromRequest(c.Request)
	out, fail := h.openAIGatewayService.RunWorkbuddySearch(c.Request.Context(), c, apiKey.GroupID, query, 0, meta)
	if fail != nil {
		// Anthropic 错误类型映射：限流用 rate_limit_error，其余用 api_error，
		// 与真实 Anthropic 的错误词表保持可识别（对齐源项目 messages 入口）。
		errType := "api_error"
		if fail.Status == http.StatusTooManyRequests {
			errType = "rate_limit_error"
		} else if fail.Status == http.StatusBadRequest {
			errType = "invalid_request_error"
		}
		reqLog.Warn("gateway.workbuddy_search.failed",
			zap.Int("status", fail.Status),
			zap.String("message", fail.Msg),
		)
		h.errorResponse(c, fail.Status, errType, fail.Msg)
		return
	}

	// 记账：搜索无 token 消耗，走既有按次搜索口径（SearchCount=1，与 grok
	// web_search / alpha search 同链路）。请求 ID 每次调用唯一——它同时是计费
	// 幂等键，用 query 哈希会让重复搜索合并成一次扣费。
	userAgent := c.GetHeader("User-Agent")
	clientIP := ip.GetClientIP(c)
	inboundEndpoint := GetInboundEndpoint(c)
	// 上游端点固定为搜索路径（入站是 /v1/search/messages，不能拿它冒充上游端点）。
	upstreamEndpoint := "/agenttool/v1/search"
	searchRequestID := "workbuddy-search:" + uuid.NewString()
	requestPayloadHash := service.HashUsageRequestPayload([]byte(query))
	quotaPlatform := service.QuotaPlatform(c.Request.Context(), apiKey)
	h.submitMandatoryUsageRecordTask(c.Request.Context(), func(ctx context.Context) {
		if err := h.gatewayService.RecordUsage(ctx, &service.RecordUsageInput{
			Result: &service.ForwardResult{
				RequestID:   searchRequestID,
				Model:       workbuddySearchModelLabel,
				SearchCount: 1,
				Duration:    time.Since(requestStart),
			},
			APIKey:             apiKey,
			User:               apiKey.User,
			Subscription:       subscription,
			InboundEndpoint:    inboundEndpoint,
			UpstreamEndpoint:   upstreamEndpoint,
			UserAgent:          userAgent,
			IPAddress:          clientIP,
			RequestPayloadHash: requestPayloadHash,
			APIKeyService:      h.apiKeyService,
			QuotaPlatform:      quotaPlatform,
		}); err != nil {
			logger.L().With(
				zap.String("component", "handler.gateway.workbuddy_search"),
				zap.Int64("user_id", apiKey.User.ID),
				zap.Int64("api_key_id", apiKey.ID),
			).Error("gateway.workbuddy_search.record_usage_failed", zap.Error(err))
		}
	})

	c.JSON(http.StatusOK, buildWorkbuddySearchResponse(query, req.Model, out))
}
