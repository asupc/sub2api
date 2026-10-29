package admin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type availableModelsAdminService struct {
	*stubAdminService
	account service.Account
}

func (s *availableModelsAdminService) GetAccount(_ context.Context, id int64) (*service.Account, error) {
	if s.account.ID == id {
		acc := s.account
		return &acc, nil
	}
	return s.stubAdminService.GetAccount(context.Background(), id)
}

func setupAvailableModelsRouter(adminSvc service.AdminService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewAccountHandler(adminSvc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	router.GET("/api/v1/admin/accounts/:id/models", handler.GetAvailableModels)
	return router
}

type syncUpstreamHTTPUpstream struct {
	resp      *http.Response
	responses []*http.Response
	err       error
}

func (u *syncUpstreamHTTPUpstream) Do(req *http.Request, proxyURL string, accountID int64, accountConcurrency int) (*http.Response, error) {
	if u.err != nil {
		return nil, u.err
	}
	if len(u.responses) > 0 {
		resp := u.responses[0]
		u.responses = u.responses[1:]
		return resp, nil
	}
	return u.resp, nil
}

func (u *syncUpstreamHTTPUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

func setupSyncUpstreamModelsRouter(adminSvc service.AdminService, upstream service.HTTPUpstream) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	accountTestSvc := service.NewAccountTestService(
		nil,
		nil,
		nil,
		nil,
		nil,
		upstream,
		&config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}},
		nil,
	)
	handler := NewAccountHandler(adminSvc, nil, nil, nil, nil, nil, nil, nil, accountTestSvc, nil, nil, nil, nil, nil)
	router.POST("/api/v1/admin/accounts/:id/models/sync-upstream", handler.SyncUpstreamModels)
	router.POST("/api/v1/admin/accounts/models/sync-upstream-preview", handler.SyncUpstreamModelsPreview)
	return router
}

func TestAccountHandlerGetAvailableModels_SourceIntegratedPlatformsUseOwnCatalog(t *testing.T) {
	// 二开三平台 OAuth 账号必须返回各自平台的模型清单（静态兜底，accountTestService
	// 缺失时仍可用），而不是落入通用 OAuth 分支误报 Claude 模型。
	cases := []struct {
		platform    string
		realm       string
		mustContain string
	}{
		{platform: service.PlatformWorkbuddy, realm: "cn", mustContain: "deepseek-v4.1-flash"},
		{platform: service.PlatformQoder, realm: "cn", mustContain: "Auto"},
		{platform: service.PlatformTrae, realm: "", mustContain: "deepseek-v4.1-flash"},
	}
	for _, tc := range cases {
		t.Run(tc.platform, func(t *testing.T) {
			credentials := map[string]any{}
			if tc.realm != "" {
				credentials["realm"] = tc.realm
			}
			svc := &availableModelsAdminService{
				stubAdminService: newStubAdminService(),
				account: service.Account{
					ID:          60,
					Name:        tc.platform + "-oauth",
					Platform:    tc.platform,
					Type:        service.AccountTypeOAuth,
					Status:      service.StatusActive,
					Credentials: credentials,
				},
			}
			router := setupAvailableModelsRouter(svc)

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/60/models", nil)
			router.ServeHTTP(rec, req)

			require.Equal(t, http.StatusOK, rec.Code)
			var resp struct {
				Data []struct {
					ID string `json:"id"`
				} `json:"data"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
			require.NotEmpty(t, resp.Data)

			ids := make([]string, 0, len(resp.Data))
			for _, m := range resp.Data {
				ids = append(ids, m.ID)
			}
			require.Contains(t, ids, tc.mustContain)
			// 不得回落通用 OAuth 分支的 Claude 模型清单。
			for _, id := range ids {
				require.False(t, strings.HasPrefix(id, "claude-"), "unexpected claude model %q for %s", id, tc.platform)
			}
		})
	}
}

func TestAccountHandlerGetAvailableModels_SourceIntegratedPlatformsWithMappingUseWhitelist(t *testing.T) {
	// 白名单/映射模式（非空 model_mapping）时测试下拉框只能列出映射请求侧 keys；
	// 调度准入 IsModelSupported 只放行这些模型，展示 live 全集会让管理员选中
	// 账号实际不服务的模型。
	cases := []struct {
		platform string
		realm    string
	}{
		{platform: service.PlatformWorkbuddy, realm: "cn"},
		{platform: service.PlatformQoder, realm: "cn"},
		{platform: service.PlatformTrae, realm: ""},
	}
	for _, tc := range cases {
		t.Run(tc.platform, func(t *testing.T) {
			credentials := map[string]any{
				"model_mapping": map[string]any{
					"glm-5.3":      "glm-5.3",
					"custom-alias": "upstream-model",
					"glm-*":        "glm-*",
					" ":            "blank-key",
				},
			}
			if tc.realm != "" {
				credentials["realm"] = tc.realm
			}
			svc := &availableModelsAdminService{
				stubAdminService: newStubAdminService(),
				account: service.Account{
					ID:          61,
					Name:        tc.platform + "-oauth-whitelist",
					Platform:    tc.platform,
					Type:        service.AccountTypeOAuth,
					Status:      service.StatusActive,
					Credentials: credentials,
				},
			}
			router := setupAvailableModelsRouter(svc)

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/61/models", nil))

			require.Equal(t, http.StatusOK, rec.Code)
			// 通配符与空键无法枚举具体模型，跳过；剩余 keys 即账号可测试集合。
			require.ElementsMatch(t, []string{"glm-5.3", "custom-alias"}, collectModelIDs(t, rec))
		})
	}
}

func TestAccountHandlerGetAvailableModels_SourceIntegratedPlatformsWithWildcardOnlyMappingFallBackToCatalog(t *testing.T) {
	// 仅通配符的映射 = 允许匹配的整族模型，无法枚举，回落平台静态兜底目录。
	svc := &availableModelsAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID:       62,
			Name:     "workbuddy-oauth-wildcard",
			Platform: service.PlatformWorkbuddy,
			Type:     service.AccountTypeOAuth,
			Status:   service.StatusActive,
			Credentials: map[string]any{
				"realm":         "cn",
				"model_mapping": map[string]any{"glm-*": "glm-*"},
			},
		},
	}
	router := setupAvailableModelsRouter(svc)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/62/models", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	ids := collectModelIDs(t, rec)
	require.NotEmpty(t, ids)
	require.Contains(t, ids, "deepseek-v4.1-flash")
	require.NotContains(t, ids, "glm-*")
}

func TestAccountHandlerGetAvailableModels_ClaudeOAuthUsesExplicitModelMapping(t *testing.T) {
	// OAuth 账号经批量编辑配置白名单后，可测试集合就是映射 keys（官方模型
	// 保留 DefaultModels 显示名，自定义别名用 id 兜底）。
	svc := &availableModelsAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID:       63,
			Name:     "claude-oauth-whitelist",
			Platform: service.PlatformAnthropic,
			Type:     service.AccountTypeOAuth,
			Status:   service.StatusActive,
			Credentials: map[string]any{
				"model_mapping": map[string]any{
					"claude-fable-5": "claude-fable-5",
					"my-alias":       "claude-fable-5",
				},
			},
		},
	}
	router := setupAvailableModelsRouter(svc)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/63/models", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Data []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 2)
	byID := make(map[string]string, len(resp.Data))
	for _, m := range resp.Data {
		byID[m.ID] = m.DisplayName
	}
	require.Equal(t, "Claude Fable 5", byID["claude-fable-5"])
	require.Equal(t, "my-alias", byID["my-alias"])
}

func TestAccountHandlerGetAvailableModels_GeminiOAuthUsesExplicitModelMapping(t *testing.T) {
	cases := []struct {
		name        string
		oauthType   string
		mapping     map[string]any
		wantIDs     []string
		wantDisplay map[string]string
	}{
		{
			name:      "plain oauth",
			oauthType: "",
			mapping: map[string]any{
				// 上游已把停用的 2.x 条目移出 DefaultModels（仅 Google One 清单保留），
				// 这里改用现行 DefaultModels 条目验证映射请求侧 keys + 显示名回填。
				"gemini-3-flash": "gemini-3-flash",
				"my-gem":         "gemini-3-flash",
			},
			wantIDs: []string{"gemini-3-flash", "my-gem"},
			wantDisplay: map[string]string{
				"gemini-3-flash": "Gemini 3 Flash",
				"my-gem":         "my-gem",
			},
		},
		{
			name:      "google one oauth",
			oauthType: "google_one",
			mapping: map[string]any{
				"gemini-2.5-flash": "gemini-2.5-flash",
			},
			wantIDs: []string{"gemini-2.5-flash"},
			wantDisplay: map[string]string{
				"gemini-2.5-flash": "Gemini 2.5 Flash",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			credentials := map[string]any{
				"model_mapping": tc.mapping,
			}
			if tc.oauthType != "" {
				credentials["oauth_type"] = tc.oauthType
			}
			svc := &availableModelsAdminService{
				stubAdminService: newStubAdminService(),
				account: service.Account{
					ID:          64,
					Name:        "gemini-oauth-whitelist",
					Platform:    service.PlatformGemini,
					Type:        service.AccountTypeOAuth,
					Status:      service.StatusActive,
					Credentials: credentials,
				},
			}
			router := setupAvailableModelsRouter(svc)

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/64/models", nil))

			require.Equal(t, http.StatusOK, rec.Code)
			var resp struct {
				Data []struct {
					ID          string `json:"id"`
					DisplayName string `json:"display_name"`
				} `json:"data"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
			byID := make(map[string]string, len(resp.Data))
			for _, m := range resp.Data {
				byID[m.ID] = m.DisplayName
			}
			require.ElementsMatch(t, tc.wantIDs, keysOf(byID))
			require.Equal(t, tc.wantDisplay, byID)
		})
	}
}

// collectModelIDs 解析 GetAvailableModels 响应中的模型 ID 列表。
func collectModelIDs(t *testing.T, rec *httptest.ResponseRecorder) []string {
	t.Helper()
	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	ids := make([]string, 0, len(resp.Data))
	for _, m := range resp.Data {
		ids = append(ids, m.ID)
	}
	return ids
}

func keysOf(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func TestAccountHandlerGetAvailableModels_GrokUsesXAIModels(t *testing.T) {
	svc := &availableModelsAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID:       44,
			Name:     "grok-oauth",
			Platform: service.PlatformGrok,
			Type:     service.AccountTypeOAuth,
			Status:   service.StatusActive,
			Credentials: map[string]any{
				"model_mapping": map[string]any{
					"grok-4.3": "grok-4.3",
				},
			},
		},
	}
	router := setupAvailableModelsRouter(svc)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/44/models", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 1)
	require.Equal(t, "grok-4.3", resp.Data[0].ID)
}

func TestAccountHandlerGetAvailableModels_GrokDefaultsToXAIModelsWithoutMapping(t *testing.T) {
	svc := &availableModelsAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID:       45,
			Name:     "grok-oauth-defaults",
			Platform: service.PlatformGrok,
			Type:     service.AccountTypeOAuth,
			Status:   service.StatusActive,
		},
	}
	router := setupAvailableModelsRouter(svc)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/45/models", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.Data)

	var ids []string
	for _, model := range resp.Data {
		id := model.ID
		ids = append(ids, id)
		require.NotContains(t, strings.ToLower(id), "claude")
	}
	require.Contains(t, ids, "grok-4.3")
	require.Contains(t, ids, "grok-build-0.1")
}

func TestAccountHandlerGetAvailableModels_OpenAIOAuthUsesExplicitModelMapping(t *testing.T) {
	svc := &availableModelsAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID:       42,
			Name:     "openai-oauth",
			Platform: service.PlatformOpenAI,
			Type:     service.AccountTypeOAuth,
			Status:   service.StatusActive,
			Credentials: map[string]any{
				"model_mapping": map[string]any{
					"gpt-5": "gpt-5.1",
				},
			},
		},
	}
	router := setupAvailableModelsRouter(svc)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/42/models", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 1)
	require.Equal(t, "gpt-5", resp.Data[0].ID)
}

func TestAccountHandlerGetAvailableModels_OpenAIOAuthPassthroughFallsBackToDefaults(t *testing.T) {
	svc := &availableModelsAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID:       43,
			Name:     "openai-oauth-passthrough",
			Platform: service.PlatformOpenAI,
			Type:     service.AccountTypeOAuth,
			Status:   service.StatusActive,
			Credentials: map[string]any{
				"model_mapping": map[string]any{
					"gpt-5": "gpt-5.1",
				},
			},
			Extra: map[string]any{
				"openai_passthrough": true,
			},
		},
	}
	router := setupAvailableModelsRouter(svc)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/43/models", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.Data)
	require.NotEqual(t, "gpt-5", resp.Data[0].ID)
}

func TestAccountHandlerGetAvailableModels_OpenAIAPIKeyDefaultsToConcreteGPT56Sol(t *testing.T) {
	svc := &availableModelsAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID:       46,
			Name:     "openai-apikey",
			Platform: service.PlatformOpenAI,
			Type:     service.AccountTypeAPIKey,
			Status:   service.StatusActive,
			Credentials: map[string]any{
				"api_key": "test-key",
			},
		},
	}
	router := setupAvailableModelsRouter(svc)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/46/models", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.Data)
	require.Equal(t, "gpt-5.6-sol", resp.Data[0].ID)
}

func TestAccountHandlerGetAvailableModels_OpenAISparkShadowReturnsMappingModels(t *testing.T) {
	parentID := int64(100)
	svc := &availableModelsAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID:              44,
			Name:            "openai-spark-shadow",
			Platform:        service.PlatformOpenAI,
			Type:            service.AccountTypeOAuth,
			Status:          service.StatusActive,
			ParentAccountID: &parentID,
			QuotaDimension:  service.QuotaDimensionSpark,
			Credentials: map[string]any{
				"model_mapping": map[string]any{
					"gpt-5.3-codex-spark": "gpt-5.3-codex-spark",
				},
			},
		},
	}
	router := setupAvailableModelsRouter(svc)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/44/models", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	ids := make([]string, 0, len(resp.Data))
	for _, m := range resp.Data {
		ids = append(ids, m.ID)
	}
	require.ElementsMatch(t, []string{
		"gpt-5.3-codex-spark",
	}, ids, "影子可用模型由 model_mapping 派生（非写死）")
}

func TestAccountHandlerGetAvailableModels_GeminiGoogleOneUsesConservativeCatalog(t *testing.T) {
	svc := &availableModelsAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID:       45,
			Name:     "google-one",
			Platform: service.PlatformGemini,
			Type:     service.AccountTypeOAuth,
			Status:   service.StatusActive,
			Credentials: map[string]any{
				"oauth_type": "google_one",
			},
		},
	}
	router := setupAvailableModelsRouter(svc)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/45/models", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	ids := make([]string, 0, len(resp.Data))
	for _, model := range resp.Data {
		ids = append(ids, model.ID)
	}
	require.ElementsMatch(t, []string{"gemini-2.0-flash", "gemini-2.5-flash", "gemini-2.5-pro"}, ids)
	require.NotContains(t, ids, "gemini-3.5-flash")
	require.NotContains(t, ids, "gemini-2.5-flash-image")
}

func TestAntigravityHasStoredModelMapping(t *testing.T) {
	// 白名单/映射模式（非空 model_mapping）时测试下拉框只能列出映射 keys；
	// 空映射 = 直接模型模式，允许展示上游 live 目录。
	cases := []struct {
		name    string
		account *service.Account
		want    bool
	}{
		{name: "nil account", account: nil, want: false},
		{name: "nil credentials", account: &service.Account{}, want: false},
		{name: "missing mapping", account: &service.Account{Credentials: map[string]any{}}, want: false},
		{name: "empty JSON mapping", account: &service.Account{Credentials: map[string]any{"model_mapping": map[string]any{}}}, want: false},
		{name: "empty string mapping", account: &service.Account{Credentials: map[string]any{"model_mapping": map[string]string{}}}, want: false},
		{name: "blank key only", account: &service.Account{Credentials: map[string]any{"model_mapping": map[string]any{" ": "upstream"}}}, want: false},
		{name: "JSON mapping", account: &service.Account{Credentials: map[string]any{"model_mapping": map[string]any{"gemini-3.8-flash-high": "gemini-3.8-flash-high"}}}, want: true},
		{name: "string mapping", account: &service.Account{Credentials: map[string]any{"model_mapping": map[string]string{"gemini-3.8-flash-high": "gemini-3.8-flash-high"}}}, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, antigravityHasStoredModelMapping(tc.account))
		})
	}
}

func TestAccountHandlerGetAvailableModels_Antigravity(t *testing.T) {
	defaults := antigravity.DefaultModels()
	known := defaults[0]
	custom := antigravity.ClaudeModel{ID: "claude-custom-alias", Type: "model", DisplayName: "claude-custom-alias"}
	cases := []struct {
		name        string
		credentials map[string]any
		want        []antigravity.ClaudeModel
	}{
		{name: "nil credentials", want: defaults},
		{name: "missing mapping", credentials: map[string]any{}, want: defaults},
		{name: "empty JSON mapping", credentials: map[string]any{"model_mapping": map[string]any{}}, want: defaults},
		{name: "empty string mapping", credentials: map[string]any{"model_mapping": map[string]string{}}, want: defaults},
		{name: "blank key", credentials: map[string]any{"model_mapping": map[string]any{" ": "upstream-model"}}, want: defaults},
		{name: "wildcard only falls back to defaults", credentials: map[string]any{"model_mapping": map[string]any{"claude-*": "claude-sonnet-4-6", "gemini-3*": "gemini-3-flash"}}, want: defaults},
		{name: "JSON mapping keys and metadata skips wildcards", credentials: map[string]any{"model_mapping": map[string]any{
			known.ID: known.ID, custom.ID: "upstream-model", "claude-*": "claude-sonnet-4-6",
		}}, want: []antigravity.ClaudeModel{custom, known}},
		{name: "string mapping keys and metadata skips wildcards", credentials: map[string]any{"model_mapping": map[string]string{
			known.ID: known.ID, custom.ID: "upstream-model", "gemini-*": "gemini-3-flash",
		}}, want: []antigravity.ClaudeModel{custom, known}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, accountType := range []string{service.AccountTypeOAuth, service.AccountTypeAPIKey} {
				t.Run(accountType, func(t *testing.T) {
					svc := &availableModelsAdminService{
						stubAdminService: newStubAdminService(),
						account:          service.Account{ID: 51, Platform: service.PlatformAntigravity, Type: accountType, Credentials: tc.credentials},
					}
					rec := httptest.NewRecorder()
					setupAvailableModelsRouter(svc).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/51/models", nil))
					require.Equal(t, http.StatusOK, rec.Code)
					var resp struct {
						Data []antigravity.ClaudeModel `json:"data"`
					}
					require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
					require.Equal(t, tc.want, resp.Data)
				})
			}
		})
	}
}

func TestAccountHandlerSyncUpstreamModels_ConfigErrorReturnsBadRequest(t *testing.T) {
	svc := &availableModelsAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID:       44,
			Name:     "openai-apikey-missing-key",
			Platform: service.PlatformOpenAI,
			Type:     service.AccountTypeAPIKey,
			Status:   service.StatusActive,
			Credentials: map[string]any{
				"base_url": "https://openai.example.com/v1",
			},
		},
	}
	router := setupSyncUpstreamModelsRouter(svc, &syncUpstreamHTTPUpstream{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/44/models/sync-upstream", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "No OpenAI API key is available")
}

func TestAccountHandlerSyncUpstreamModelsReturnsCapabilityMetadata(t *testing.T) {
	svc := &availableModelsAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID: 48, Name: "custom-openai", Platform: service.PlatformOpenAI,
			Type: service.AccountTypeAPIKey, Status: service.StatusActive,
			Credentials: map[string]any{"api_key": "key", "base_url": "https://provider.example/v1"},
		},
	}
	upstream := &syncUpstreamHTTPUpstream{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{"models":[{
			"id":"custom-thinking-model",
			"reasoning":true,
			"default_reasoning_level":"high",
			"supported_reasoning_levels":["low","high"],
			"input_modalities":["text","image"],
			"context_window":256000
		}]}`)),
	}}
	router := setupSyncUpstreamModelsRouter(svc, upstream)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/48/models/sync-upstream", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Data service.UpstreamModelCatalog `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, []string{"custom-thinking-model"}, resp.Data.Models)
	metadata := resp.Data.Metadata["custom-thinking-model"]
	require.NotNil(t, metadata.Reasoning)
	require.True(t, *metadata.Reasoning)
	require.Equal(t, []string{"low", "high"}, metadata.SupportedReasoningLevels)
	require.Equal(t, []string{"text", "image"}, metadata.InputModalities)
}

// Scenario: 创建账号 preview 将具体 mapping 传给 404/405 配置回退。
func TestAccountHandlerSyncUpstreamModelsPreviewUsesProvidedModelMapping(t *testing.T) {
	upstream := &syncUpstreamHTTPUpstream{responses: []*http.Response{
		{
			StatusCode: http.StatusNotFound,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"error":"not found"}`)),
		},
		{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{
				"configured-provider": {
					"api": "https://provider.example/v1",
					"models": {
						"glm-5.3": {
							"id": "glm-5.3",
							"reasoning": true,
							"reasoning_options": [{"type":"effort","values":["low","high"]}],
							"modalities": {"input":["text"],"output":["text"]},
							"limit": {"context":1000000,"output":131072}
						}
					}
				}
			}`)),
		},
	}}
	router := setupSyncUpstreamModelsRouter(newStubAdminService(), upstream)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/admin/accounts/models/sync-upstream-preview",
		strings.NewReader(`{
			"platform":"openai",
			"type":"apikey",
			"base_url":"https://provider.example/v1",
			"api_key":"key",
			"model_mapping":{"public-glm":"glm-5.3"}
		}`),
	)
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Data service.UpstreamModelCatalog `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, []string{"glm-5.3"}, resp.Data.Models)
	require.Equal(t, []string{"low", "high"}, resp.Data.Metadata["glm-5.3"].SupportedReasoningLevels)
}

func TestAccountHandlerSyncUpstreamModels_UpstreamErrorDoesNotExposeBody(t *testing.T) {
	svc := &availableModelsAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID:       45,
			Name:     "openai-apikey-upstream-error",
			Platform: service.PlatformOpenAI,
			Type:     service.AccountTypeAPIKey,
			Status:   service.StatusActive,
			Credentials: map[string]any{
				"api_key":  "openai-key",
				"base_url": "https://openai.example.com/v1",
			},
		},
	}
	upstream := &syncUpstreamHTTPUpstream{resp: &http.Response{
		StatusCode: http.StatusBadGateway,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":"SECRET_TOKEN should not be exposed"}`)),
	}}
	router := setupSyncUpstreamModelsRouter(svc, upstream)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/45/models/sync-upstream", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadGateway, rec.Code)
	require.Contains(t, rec.Body.String(), "Upstream model list request failed with HTTP 502")
	require.NotContains(t, rec.Body.String(), "SECRET_TOKEN")
}

// Scenario: 能力补全失败显示部分成功。
func TestAccountHandlerSyncUpstreamModels_MetadataEnrichmentFailureReturnsWarning(t *testing.T) {
	svc := &availableModelsAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID:       46,
			Name:     "opencode-id-only-model-list",
			Platform: service.PlatformOpenAI,
			Type:     service.AccountTypeAPIKey,
			Status:   service.StatusActive,
			Credentials: map[string]any{
				"api_key":  "opencode-key",
				"base_url": "https://opencode.ai/zen/v1",
			},
		},
	}
	upstream := &syncUpstreamHTTPUpstream{responses: []*http.Response{
		{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"data":[{"id":"x-preview-f-free"}]}`)),
		},
		{
			StatusCode: http.StatusBadGateway,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"error":"registry unavailable"}`)),
		},
	}}
	router := setupSyncUpstreamModelsRouter(svc, upstream)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/46/models/sync-upstream", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Data struct {
			Models   []string `json:"models"`
			Warnings []struct {
				Code string `json:"code"`
			} `json:"warnings"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, []string{"x-preview-f-free"}, resp.Data.Models)
	require.Len(t, resp.Data.Warnings, 1)
	require.Equal(t, "upstream_model_metadata_incomplete", resp.Data.Warnings[0].Code)
}

func TestAccountHandlerGetAvailableModels_TypeSafeOnlyReturnsJev(t *testing.T) {
	for _, credentials := range []map[string]any{
		{"api_key": "ts-secret"},
		{"api_key": "ts-secret", "model_mapping": map[string]any{"jev-latest": "jev-latest"}},
	} {
		svc := &availableModelsAdminService{
			stubAdminService: newStubAdminService(),
			account: service.Account{
				ID:          46,
				Name:        "typesafe",
				Platform:    service.PlatformTypeSafe,
				Type:        service.AccountTypeAPIKey,
				Status:      service.StatusActive,
				Credentials: credentials,
			},
		}
		router := setupAvailableModelsRouter(svc)

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/46/models", nil))
		require.Equal(t, http.StatusOK, rec.Code)

		var resp struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		require.Len(t, resp.Data, 1)
		require.Equal(t, "jev-latest", resp.Data[0].ID)
	}
}
