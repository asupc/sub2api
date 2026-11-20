package handler

// gateway_qoder_search.go 二开：Qoder 官方 Web 搜索端点（与 workbuddy 搜索共用
// POST /v1/search/messages，按分组平台分派）。
//
// 只接入「搜索」这一条：官方 oneSearch 由 Qoder 服务器执行，网关侧做账号轮转、
// 域名过滤与结果裁剪。Responses 托管工具（web_search_call / open_page 事件、
// 调用预算、来源引用）未在本轮移植，见 CHANGELOG-SO.md。
//
// 响应契约与 workbuddy 搜索一致：Anthropic Messages + web_search_tool_result 块，
// 摘要走 text block 的 citations[].cited_text。

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
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

// qoderSearchModelLabel 记账/日志里的「模型」列标识。
const qoderSearchModelLabel = "qoder-web-search"

// QoderSearchMessages 处理 POST /v1/search/messages 的 qoder 分组流量。
func (h *GatewayHandler) QoderSearchMessages(c *gin.Context) {
	requestStart := time.Now()

	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok || apiKey == nil {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "API key required")
		return
	}
	if apiKey.Group == nil || apiKey.Group.Platform != service.PlatformQoder {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error",
			"web search is only supported for qoder groups")
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
	if !workbuddyDeclaresWebSearch(req.Tools) {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error",
			"request does not declare the web_search tool; this endpoint only serves web search, not chat")
		return
	}
	query := extractWorkbuddySearchQuery(req.Messages)
	if len(query) < workbuddy.SearchMinQueryLen {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error",
			"could not read a search query from the request")
		return
	}

	reqLog := requestLogger(c, "handler.gateway.qoder_search",
		zap.Int64("user_id", apiKey.User.ID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("group_id", apiKey.GroupID),
		zap.String("model", qoderSearchModelLabel),
	)
	setOpsRequestContext(c, qoderSearchModelLabel, false)
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

	inflightDone, inflightErr := reserveInflightBalance(c, h.billingCacheService, h.gatewayService, apiKey, subscription, service.InflightEstimateRequest{
		Model:       qoderSearchModelLabel,
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

	auditBody, _ := json.Marshal(map[string]any{
		"messages": []map[string]any{{
			"role": "user", "content": query,
		}},
	})
	if decision := h.checkSecurityAudit(c, reqLog, apiKey, subject, service.ContentModerationProtocolOpenAIChat, qoderSearchModelLabel, auditBody); decision != nil && !decision.AllowNextStage {
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

	results, fail := h.openAIGatewayService.RunQoderWebSearch(c.Request.Context(), c, apiKey.GroupID, query, 0, nil, nil)
	if fail != nil {
		errType := "api_error"
		if fail.Status == http.StatusTooManyRequests {
			errType = "rate_limit_error"
		} else if fail.Status == http.StatusBadRequest {
			errType = "invalid_request_error"
		}
		reqLog.Warn("gateway.qoder_search.failed", zap.Int("status", fail.Status), zap.String("message", fail.Msg))
		h.errorResponse(c, fail.Status, errType, fail.Msg)
		return
	}

	out := &workbuddy.SearchOutcome{Query: query}
	for _, r := range results {
		out.Results = append(out.Results, workbuddy.SearchResult{
			Title:   r.Title,
			URL:     r.URL,
			Snippet: r.Snippet,
		})
	}

	userAgent := c.GetHeader("User-Agent")
	clientIP := ip.GetClientIP(c)
	inboundEndpoint := GetInboundEndpoint(c)
	upstreamEndpoint := "/algo/api/v1/webSearch/oneSearch"
	searchRequestID := "qoder-search:" + uuid.NewString()
	requestPayloadHash := service.HashUsageRequestPayload([]byte(query))
	quotaPlatform := service.QuotaPlatform(c.Request.Context(), apiKey)
	h.submitMandatoryUsageRecordTask(c.Request.Context(), func(ctx context.Context) {
		if err := h.gatewayService.RecordUsage(ctx, &service.RecordUsageInput{
			Result: &service.ForwardResult{
				RequestID:   searchRequestID,
				Model:       qoderSearchModelLabel,
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
				zap.String("component", "handler.gateway.qoder_search"),
				zap.Int64("user_id", apiKey.User.ID),
				zap.Int64("api_key_id", apiKey.ID),
			).Error("gateway.qoder_search.record_usage_failed", zap.Error(err))
		}
	})

	c.JSON(http.StatusOK, buildWorkbuddySearchResponse(query, req.Model, out))
}
