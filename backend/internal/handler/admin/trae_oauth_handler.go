package admin

import (
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// TraeOAuthHandler 是 trae 平台的管理端 OAuth 端点（运营任务走通用
// PlatformOpsHandler，不在此重复）。登录流为「本机回调」语义：前端拿到
// 授权 URL 后在浏览器完成登录，把回跳查询串回传给 login-poll 完成交换。
type TraeOAuthHandler struct {
	traeOAuthService *service.TraeOAuthService
	adminService     service.AdminService
}

// NewTraeOAuthHandler 构造 trae OAuth handler。
func NewTraeOAuthHandler(traeOAuthService *service.TraeOAuthService, adminService service.AdminService) *TraeOAuthHandler {
	return &TraeOAuthHandler{
		traeOAuthService: traeOAuthService,
		adminService:     adminService,
	}
}

// TraeLoginStartRequest 是 login-start 请求体。
type TraeLoginStartRequest struct {
	// Variant 产品线变体：trae_cn | solo_cn | trae_global | solo_global。
	Variant string `json:"variant" binding:"required"`
	// ProxyID 让授权与交换请求经指定代理发出。
	ProxyID *int64 `json:"proxy_id"`
	// DeviceID 可选的设备标识（与 DevicePublicKey 成对提供，见下）。
	DeviceID string `json:"device_id"`
	// DevicePublicKey 可选的设备公钥 PEM 覆盖（上游设备校验严格时使用）；
	// 必须与 DeviceID 成对提供（同一台已知设备的 id+公钥对）。
	DevicePublicKey string `json:"device_public_key"`
}

// LoginStart 生成授权页 URL。
// POST /admin/trae/oauth/login-start
func (h *TraeOAuthHandler) LoginStart(c *gin.Context) {
	var req TraeLoginStartRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	result, err := h.traeOAuthService.GenerateLoginURL(c.Request.Context(), req.Variant, req.ProxyID, req.DeviceID, req.DevicePublicKey)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

// TraeLoginPollRequest 是 login-poll 请求体。
type TraeLoginPollRequest struct {
	// State login-start 返回的状态标识。
	State string `json:"state" binding:"required"`
	// CallbackQuery 授权页回跳 URL 的原始查询串（authCodeInfo/host/loginTraceID…）。
	CallbackQuery string `json:"callback_query"`
	// Error 授权失败的错误说明（前端回传，丢弃会话）。
	Error string `json:"error"`
}

// TraeLoginPollResponse 是 login-poll 响应体。
type TraeLoginPollResponse struct {
	Done        bool           `json:"done"`
	Credentials map[string]any `json:"credentials,omitempty"`
	UserID      string         `json:"user_id,omitempty"`
	DisplayName string         `json:"display_name,omitempty"`
}

// LoginPoll 完成回调交换（或查询会话进度）。
// POST /admin/trae/oauth/login-poll
func (h *TraeOAuthHandler) LoginPoll(c *gin.Context) {
	var req TraeLoginPollRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if msg := strings.TrimSpace(req.Error); msg != "" {
		h.traeOAuthService.AbandonLogin(req.State)
		response.Success(c, TraeLoginPollResponse{Done: false})
		return
	}
	if strings.TrimSpace(req.CallbackQuery) == "" {
		// 前端尚未回传回调参数：会话仍在等待。
		response.Success(c, TraeLoginPollResponse{Done: false})
		return
	}
	result, err := h.traeOAuthService.CompleteLogin(c.Request.Context(), req.State, req.CallbackQuery)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, TraeLoginPollResponse{
		Done:        true,
		Credentials: result.Credentials,
		UserID:      result.UserID,
		DisplayName: result.DisplayName,
	})
}

// CreateAccountFromOAuth 用完成的登录会话直接创建账号。
// POST /admin/trae/oauth/create-from-oauth
func (h *TraeOAuthHandler) CreateAccountFromOAuth(c *gin.Context) {
	var req struct {
		State         string  `json:"state" binding:"required"`
		CallbackQuery string  `json:"callback_query"`
		Name          string  `json:"name"`
		ProxyID       *int64  `json:"proxy_id"`
		Concurrency   int     `json:"concurrency"`
		Priority      int     `json:"priority"`
		GroupIDs      []int64 `json:"group_ids"`
		// ModelMapping 可选模型白名单（选中模型 {模型: 模型}），落 credentials。
		ModelMapping map[string]any `json:"model_mapping"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	result, err := h.traeOAuthService.CompleteLogin(c.Request.Context(), req.State, req.CallbackQuery)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = strings.TrimSpace(result.DisplayName)
	}
	if name == "" && result.UserID != "" {
		name = "Trae " + result.Variant.DisplayName() + " " + result.UserID
	}
	if name == "" {
		name = "Trae " + result.Variant.DisplayName() + " OAuth Account"
	}

	credentials := result.Credentials
	if len(req.ModelMapping) > 0 {
		merged := map[string]any{}
		for key, value := range credentials {
			merged[key] = value
		}
		merged["model_mapping"] = req.ModelMapping
		credentials = merged
	}
	account, err := h.adminService.CreateAccount(c.Request.Context(), &service.CreateAccountInput{
		Name:        name,
		Platform:    service.PlatformTrae,
		Type:        service.AccountTypeOAuth,
		Credentials: credentials,
		ProxyID:     req.ProxyID,
		Concurrency: req.Concurrency,
		Priority:    req.Priority,
		GroupIDs:    req.GroupIDs,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.AccountFromService(account))
}

// QueryQuota 手动查询账号剩余积分并落 extra["trae_quota"]。
// GET /admin/trae/accounts/:id/quota
func (h *TraeOAuthHandler) QueryQuota(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	result, err := h.traeOAuthService.QueryAccountQuota(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}
