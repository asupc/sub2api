package admin

import (
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// QoderOAuthHandler 是 qoder 平台的管理端 OAuth 端点（运营任务走通用
// PlatformOpsHandler，不在此重复）。登录流为「设备授权轮询」语义：前端拿到
// 授权 URL 后在浏览器完成登录，周期性调 login-poll 直到 done=true 取回凭证。
type QoderOAuthHandler struct {
	qoderOAuthService *service.QoderOAuthService
}

// NewQoderOAuthHandler 构造 qoder OAuth handler。
func NewQoderOAuthHandler(qoderOAuthService *service.QoderOAuthService) *QoderOAuthHandler {
	return &QoderOAuthHandler{qoderOAuthService: qoderOAuthService}
}

// QoderLoginStartRequest 是 login-start 请求体。
type QoderLoginStartRequest struct {
	// Realm 区域：cn（国内版）| intl（国际版），空按 cn。
	Realm string `json:"realm"`
	// ProxyID 让授权与轮询请求经指定代理发出。
	ProxyID *int64 `json:"proxy_id"`
}

// QoderLoginStartResponse 是 login-start 响应体。
type QoderLoginStartResponse struct {
	URL   string `json:"url"`
	State string `json:"state"`
}

// LoginStart 生成设备授权页 URL。
// POST /admin/qoder/oauth/login-start
func (h *QoderOAuthHandler) LoginStart(c *gin.Context) {
	var req QoderLoginStartRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	authURL, state, err := h.qoderOAuthService.StartLogin(c.Request.Context(), req.Realm, req.ProxyID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, QoderLoginStartResponse{URL: authURL, State: state})
}

// QoderLoginPollRequest 是 login-poll 请求体。
type QoderLoginPollRequest struct {
	// State login-start 返回的状态标识。
	State string `json:"state" binding:"required"`
}

// QoderLoginPollResponse 是 login-poll 响应体。
type QoderLoginPollResponse struct {
	Done        bool           `json:"done"`
	Credentials map[string]any `json:"credentials,omitempty"`
	UserID      string         `json:"user_id,omitempty"`
	Nickname    string         `json:"nickname,omitempty"`
	Message     string         `json:"message,omitempty"`
}

// LoginPoll 轮询设备授权进度；授权完成后返回完整凭证。
// POST /admin/qoder/oauth/login-poll
func (h *QoderOAuthHandler) LoginPoll(c *gin.Context) {
	var req QoderLoginPollRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	result, err := h.qoderOAuthService.PollLogin(c.Request.Context(), req.State)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, QoderLoginPollResponse{
		Done:        result.Done,
		Credentials: result.Credentials,
		UserID:      result.UserID,
		Nickname:    result.Nickname,
		Message:     result.Message,
	})
}

// QoderPATImportRequest 是 create-from-pat 请求体。
type QoderPATImportRequest struct {
	// PAT 个人访问令牌（pt- 前缀）。
	PAT string `json:"pat" binding:"required"`
	// Realm 区域：cn | intl，空按 cn。
	Realm string `json:"realm"`
	// ProxyID 让交换请求经指定代理发出。
	ProxyID *int64 `json:"proxy_id"`
}

// ImportPAT 导入 pt- 个人访问令牌：交换 jobToken 并返回可直接建号的凭证。
// POST /admin/qoder/oauth/create-from-pat
func (h *QoderOAuthHandler) ImportPAT(c *gin.Context) {
	var req QoderPATImportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if !strings.HasPrefix(strings.TrimSpace(req.PAT), "pt-") {
		response.BadRequest(c, "PAT must start with pt-")
		return
	}
	result, err := h.qoderOAuthService.ImportPAT(c.Request.Context(), req.PAT, req.Realm, req.ProxyID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, QoderLoginPollResponse{
		Done:        true,
		Credentials: result.Credentials,
		UserID:      result.UserID,
		Nickname:    result.Nickname,
	})
}

// QueryQuota 手动查询账号剩余积分并落 extra["qd_quota"]。
// GET /admin/qoder/accounts/:id/quota
func (h *QoderOAuthHandler) QueryQuota(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	snapshot, err := h.qoderOAuthService.QueryAccountQuota(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, snapshot)
}
