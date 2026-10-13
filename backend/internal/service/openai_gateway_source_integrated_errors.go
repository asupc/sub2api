package service

// openai_gateway_source_integrated_errors.go 二开新增：源码集成平台
//（workbuddy / qoder）的上游错误分派入口。
//
// 三条 raw CC 路径（/v1/chat/completions、/v1/responses 回退、/v1/messages 回退）
// 的 >=400 错误处理必须统一先问平台专属分类，再回落通用 failover 链：
// 通用链按 OpenAI 语义解读 401/403（鉴权失败 / HTML 豁免 + 计数升级），会把
// qoder 10605 排队（403 信封）误判为鉴权失败触发强制刷新、把 workbuddy WAF 403
// 交 handleOpenAI403 计数升级至永久禁号。此前只有 /v1/chat/completions 主漏斗
// 接入了平台分类，两条 CC 回退路径（Claude Code 的 /v1/messages 落点）遗漏——
// 本文件把分派收敛为单点，三条路径共用。
//
// trae 不在此分派：其建流前错误按分类表由既有通用链处理（401/403/429/5xx
// 语义与通用链一致），流内错误在 wrapTraeChatResponse 内处置。

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
)

// handleSourceIntegratedUpstreamError 分派源码集成平台的 >=400 上游响应到
// 平台专属处理。返回 (handled, failoverErr)：
//   - handled=false：该平台/类别未接管，调用方继续走既有通用 failover 链；
//   - handled=true 且 failoverErr 非 nil：平台分类判定需换号（账号状态已按
//     平台语义处置）；
//   - handled=true 且 failoverErr=nil：确定性拒绝按上游原文透传（不换号）。
func (s *OpenAIGatewayService) handleSourceIntegratedUpstreamError(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	resp *http.Response,
	respBody []byte,
	upstreamMsg string,
	upstreamModel string,
) (bool, *UpstreamFailoverError) {
	if account == nil {
		return false, nil
	}
	switch account.Platform {
	case PlatformWorkbuddy:
		return s.handleWorkbuddyUpstreamError(ctx, c, account, resp, respBody, upstreamMsg, upstreamModel)
	case PlatformQoder:
		return s.handleQoderUpstreamError(ctx, c, account, resp, respBody, upstreamMsg, upstreamModel)
	default:
		return false, nil
	}
}
