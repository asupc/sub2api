package admin

// workbuddy_oauth_handler.go 二开新增：WorkBuddy 平台的管理端 OAuth 端点。
//
//	POST /admin/workbuddy/oauth/login-start        {realm, proxy_id?} → {url, state}
//	POST /admin/workbuddy/oauth/login-poll         {state} → {done, credentials?}
//	POST /admin/workbuddy/oauth/create-from-oauth  {state|credentials, name, ...} → 账号
//	POST /admin/workbuddy/accounts/:id/refresh     手动刷新 token
//	GET  /admin/workbuddy/accounts/:id/quota       手动查积分余额 → extra["wb_quota"]

import (
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// WorkbuddyOAuthHandler WorkBuddy 管理端 OAuth 处理器。
type WorkbuddyOAuthHandler struct {
	workbuddyOAuthService *service.WorkbuddyOAuthService
	adminService          service.AdminService
}

// NewWorkbuddyOAuthHandler 构造处理器。
func NewWorkbuddyOAuthHandler(workbuddyOAuthService *service.WorkbuddyOAuthService, adminService service.AdminService) *WorkbuddyOAuthHandler {
	return &WorkbuddyOAuthHandler{
		workbuddyOAuthService: workbuddyOAuthService,
		adminService:          adminService,
	}
}

// StartLogin 发起设备授权，返回授权 URL 与 state。
func (h *WorkbuddyOAuthHandler) StartLogin(c *gin.Context) {
	var req struct {
		Realm   string `json:"realm"`
		ProxyID *int64 `json:"proxy_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		req = struct {
			Realm   string `json:"realm"`
			ProxyID *int64 `json:"proxy_id"`
		}{}
	}
	authURL, state, err := h.workbuddyOAuthService.StartLogin(c.Request.Context(), req.Realm, req.ProxyID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"url": authURL, "state": state})
}

// PollLogin 轮询登录态：授权未完成返回 done=false；完成后返回完整凭证
// （前端拿 credentials 调 create-from-oauth 建号）。
func (h *WorkbuddyOAuthHandler) PollLogin(c *gin.Context) {
	var req struct {
		State string `json:"state" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	cred, err := h.workbuddyOAuthService.PollLogin(c.Request.Context(), req.State)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if cred == nil {
		response.Success(c, gin.H{"done": false})
		return
	}
	response.Success(c, gin.H{
		"done":        true,
		"credentials": h.workbuddyOAuthService.BuildAccountCredentials(cred),
		"nickname":    cred.Nickname,
		"realm":       cred.RealmNormalized(),
	})
}

// CreateAccountFromOAuth 用登录凭证（state 二次轮询或直接粘贴 credentials）建号。
func (h *WorkbuddyOAuthHandler) CreateAccountFromOAuth(c *gin.Context) {
	var req struct {
		State       string         `json:"state"`
		Credentials map[string]any `json:"credentials"`
		Name        string         `json:"name"`
		Notes       *string        `json:"notes"`
		ProxyID     *int64         `json:"proxy_id"`
		Concurrency int            `json:"concurrency"`
		Priority    int            `json:"priority"`
		GroupIDs    []int64        `json:"group_ids"`
		// ModelMapping 可选模型白名单（选中模型 {模型: 模型}），落 credentials。
		ModelMapping map[string]any `json:"model_mapping"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	credentials := req.Credentials
	if strings.TrimSpace(req.State) != "" {
		cred, err := h.workbuddyOAuthService.PollLogin(c.Request.Context(), req.State)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		if cred == nil {
			response.BadRequest(c, "WORKBUDDY_OAUTH_LOGIN_PENDING: 授权尚未完成，无法创建账号")
			return
		}
		credentials = h.workbuddyOAuthService.BuildAccountCredentials(cred)
	}
	if len(credentials) == 0 {
		response.BadRequest(c, "state or credentials is required")
		return
	}
	// 归一化：必填校验 + 强制 api_protocol=adaptive（与创建路径同规则）。
	credentials = service.MergeCredentials(credentials, map[string]any{"api_protocol": service.APIProtocolAdaptive})
	if len(req.ModelMapping) > 0 {
		credentials["model_mapping"] = req.ModelMapping
	}
	if err := service.ValidateWorkbuddyCredentials(credentials); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		if nickname, _ := credentials["nickname"].(string); strings.TrimSpace(nickname) != "" {
			name = strings.TrimSpace(nickname)
		}
	}
	if name == "" {
		name = "WorkBuddy OAuth Account"
	}
	account, err := h.adminService.CreateAccount(c.Request.Context(), &service.CreateAccountInput{
		Name:        name,
		Notes:       req.Notes,
		Platform:    service.PlatformWorkbuddy,
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

// RefreshAccountToken 手动刷新账号 token（401/keepalive 兜底）。
func (h *WorkbuddyOAuthHandler) RefreshAccountToken(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	account, err := h.adminService.GetAccount(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if account.Platform != service.PlatformWorkbuddy {
		response.BadRequest(c, "Account platform does not match WorkBuddy OAuth endpoint")
		return
	}
	updated, err := h.workbuddyOAuthService.RefreshAccountToken(c.Request.Context(), account)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.AccountFromService(updated))
}

// QueryQuota 手动查询积分余额（落 extra["wb_quota"] 后返回快照）。
func (h *WorkbuddyOAuthHandler) QueryQuota(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	snapshot, err := h.workbuddyOAuthService.QueryWorkbuddyQuota(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, snapshot)
}
