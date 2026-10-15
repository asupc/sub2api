package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// fakeManifestAccountSource 模型清单 handler 测试用的窄数据源
type fakeManifestAccountSource struct{}

func (f *fakeManifestAccountSource) ListAllWithFilters(ctx context.Context, platform, accountType, status, search string, groupID int64, privacyMode string) ([]service.Account, error) {
	account := service.Account{
		ID:          1,
		Name:        "acc-1",
		Platform:    "anthropic",
		Type:        "oauth",
		Status:      "active",
		Schedulable: true,
		Concurrency: 5,
	}
	account.Credentials = map[string]any{
		"model_mapping": map[string]any{"claude-alpha": "claude-sonnet-5"},
	}
	return []service.Account{account}, nil
}

func setupModelManifestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewModelManifestHandler(service.NewModelManifestService(&fakeManifestAccountSource{}))
	router.GET("/api/v1/admin/models/manifest", handler.GetManifest)
	return router
}

func TestModelManifestHandler_GetManifest(t *testing.T) {
	router := setupModelManifestRouter()

	// 无过滤：返回映射 keys 的模型与全局 summary
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/models/manifest", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		Code int `json:"code"`
		Data struct {
			Summary service.ModelManifestSummary `json:"summary"`
			Models  []service.ModelManifestItem  `json:"models"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, 1, body.Data.Summary.TotalModels)
	require.Equal(t, 1, body.Data.Summary.HealthyModels)
	require.Equal(t, 1, body.Data.Summary.TotalAccounts)
	require.Len(t, body.Data.Models, 1)
	require.Equal(t, "claude-alpha", body.Data.Models[0].ID)
	require.Equal(t, "claude-sonnet-5", body.Data.Models[0].Accounts[0].Target)
	require.False(t, body.Data.Models[0].HasWarning)

	// 平台过滤：不匹配的平台返回空列表但 summary 保持全局口径
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/models/manifest?platform=openai", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Empty(t, body.Data.Models)
	require.Equal(t, 1, body.Data.Summary.TotalModels)
}
