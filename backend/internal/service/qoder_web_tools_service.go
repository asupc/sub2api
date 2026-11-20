package service

// qoder_web_tools_service.go 二开：Qoder 官方 Web 搜索的账号轮转执行
//（移植自 qoder2api-hub qoder_official_web.py 的账号作用域语义）。
//
// 与 RunWorkbuddySearch 同骨架：同一请求内同一账号只试一次；工具调用与模型推理
// 是分开的上游请求，失败换号时**同一账号**不会再打一次（官方工具有独立用量）。

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/qoder"
	"github.com/gin-gonic/gin"
)

// QoderWebToolError 客户端视角的出口错误。
type QoderWebToolError struct {
	Status int
	Msg    string
}

const qoderWebSearchMaxAttempts = 3

// qoderWebSearchTimeout 官方工具单次请求超时（webFetch 上游 14s，留余量）。
const qoderWebSearchTimeout = 30 * time.Second

// RunQoderWebSearch 在 qoder 账号池上执行一次官方 web 搜索。
// allowed/blocked 为应用级域名过滤（工具声明参数，由调用方解析）。
func (s *OpenAIGatewayService) RunQoderWebSearch(
	ctx context.Context,
	c *gin.Context,
	groupID *int64,
	query string,
	limit int,
	allowed, blocked []string,
) ([]qoder.WebSearchResult, *QoderWebToolError) {
	failed := make(map[int64]struct{}, qoderWebSearchMaxAttempts)
	var lastFail *QoderWebToolError

	for attempt := 0; attempt < qoderWebSearchMaxAttempts; attempt++ {
		selection, _, err := s.SelectAccountWithSchedulerForCapability(
			ctx,
			groupID,
			"", "", "",
			failed,
			OpenAIUpstreamTransportAny,
			"",
			false,
			false,
			false,
			PlatformQoder,
		)
		if err != nil || selection == nil || selection.Account == nil {
			break
		}
		account := selection.Account
		failed[account.ID] = struct{}{}

		release := selection.ReleaseFunc
		if !selection.Acquired || release == nil {
			acq, aerr := s.tryAcquireAccountSlot(ctx, account.ID, account.Concurrency)
			if aerr != nil || acq == nil || !acq.Acquired {
				continue
			}
			release = acq.ReleaseFunc
		}

		creds, cerr := qoderAccountCredentials(account)
		if cerr != nil {
			if release != nil {
				release()
			}
			slog.Warn("qoder web search credentials invalid", "account_id", account.ID, "error", cerr)
			lastFail = &QoderWebToolError{Status: http.StatusServiceUnavailable, Msg: "qoder account credentials are invalid"}
			continue
		}
		client := qoder.NewClient(newQoderHTTPClient(upstreamModelsProxyURL(account), qoderWebSearchTimeout))
		results, serr := client.WebSearch(ctx, creds, query, limit, allowed, blocked)
		if release != nil {
			release()
		}
		if serr == nil {
			return results, nil
		}

		var wte *qoder.WebToolError
		if errors.As(serr, &wte) {
			// 官方工具失败（参数/地址/上游业务错误）与模型账号健康无关：不罚号、
			// 不换号重放同一请求——同一 query 换账号报文一致。
			lastFail = &QoderWebToolError{Status: http.StatusBadGateway, Msg: wte.Error()}
			break
		}
		slog.Warn("qoder web search failed", "account_id", account.ID, "error", serr)
		lastFail = &QoderWebToolError{Status: http.StatusServiceUnavailable, Msg: "qoder web search upstream temporarily unavailable"}
	}

	if lastFail != nil {
		return nil, lastFail
	}
	return nil, &QoderWebToolError{
		Status: http.StatusServiceUnavailable,
		Msg:    "all qoder accounts are temporarily unavailable, please retry later",
	}
}
