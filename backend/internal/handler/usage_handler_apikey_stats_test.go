package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
	_ "time/tzdata" // 测试环境不保证系统 tzdata，embeded 时区保证 DST 用例稳定

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type apiKeyStatsRepoStub struct {
	service.UsageLogRepository

	called    bool
	userID    int64
	startTime time.Time
	endTime   time.Time
	limit     int
}

func (s *apiKeyStatsRepoStub) GetAPIKeyUsageStatsByUser(
	ctx context.Context,
	userID int64,
	startTime, endTime time.Time,
	limit int,
) ([]usagestats.APIKeyUsageStatItem, error) {
	s.called = true
	s.userID = userID
	s.startTime = startTime
	s.endTime = endTime
	s.limit = limit
	return []usagestats.APIKeyUsageStatItem{}, nil
}

func newAPIKeyStatsTestRouter(repo *apiKeyStatsRepoStub, userID int64) *gin.Engine {
	gin.SetMode(gin.TestMode)
	usageSvc := service.NewUsageService(repo, nil, nil, nil)
	handler := NewUsageHandler(usageSvc, nil, nil, nil)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: userID})
		c.Next()
	})
	router.GET("/usage/dashboard/api-keys-usage-stats", handler.DashboardAPIKeysUsageStats)
	return router
}

type apiKeyStatsHandlerResponse struct {
	Code int `json:"code"`
	Data struct {
		Items     []usagestats.APIKeyUsageStatItem `json:"items"`
		StartDate string                           `json:"start_date"`
		EndDate   string                           `json:"end_date"`
	} `json:"data"`
}

func TestDashboardAPIKeysUsageStatsEndDateOnlyUsesThatDay(t *testing.T) {
	repo := &apiKeyStatsRepoStub{}
	router := newAPIKeyStatsTestRouter(repo, 42)

	req := httptest.NewRequest(http.MethodGet, "/usage/dashboard/api-keys-usage-stats?end_date=2026-10-01&timezone=UTC", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, repo.called)
	require.Equal(t, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), repo.startTime)
	require.Equal(t, time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC), repo.endTime)
	require.Equal(t, int64(42), repo.userID)

	var got apiKeyStatsHandlerResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, "2026-10-01", got.Data.StartDate)
	require.Equal(t, "2026-10-01", got.Data.EndDate)
}

func TestDashboardAPIKeysUsageStatsStartDateOnlyEndsNextDay(t *testing.T) {
	repo := &apiKeyStatsRepoStub{}
	router := newAPIKeyStatsTestRouter(repo, 42)

	req := httptest.NewRequest(http.MethodGet, "/usage/dashboard/api-keys-usage-stats?start_date=2026-10-01&timezone=UTC", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, repo.called)
	require.Equal(t, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), repo.startTime)
	require.Equal(t, time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC), repo.endTime)
}

func TestDashboardAPIKeysUsageStatsUsesCalendarDaysAcrossDST(t *testing.T) {
	repo := &apiKeyStatsRepoStub{}
	router := newAPIKeyStatsTestRouter(repo, 42)

	// 2026-11-01 为美国夏令时结束日（25 小时），按日历日推进应得到完整一天
	req := httptest.NewRequest(http.MethodGet, "/usage/dashboard/api-keys-usage-stats?start_date=2026-11-01&timezone=America/New_York", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, repo.called)
	require.Equal(t, 25*time.Hour, repo.endTime.Sub(repo.startTime))
	require.Equal(t, "2026-11-01", repo.startTime.Format("2006-01-02"))
	require.Equal(t, "2026-11-02", repo.endTime.Format("2006-01-02"))

	var got apiKeyStatsHandlerResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, "2026-11-01", got.Data.StartDate)
	require.Equal(t, "2026-11-01", got.Data.EndDate)
}

func TestDashboardAPIKeysUsageStatsRejectsInvertedRange(t *testing.T) {
	repo := &apiKeyStatsRepoStub{}
	router := newAPIKeyStatsTestRouter(repo, 42)

	req := httptest.NewRequest(http.MethodGet, "/usage/dashboard/api-keys-usage-stats?start_date=2026-10-02&end_date=2026-10-01&timezone=UTC", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.False(t, repo.called)
}

func TestDashboardAPIKeysUsageStatsRejectsInvalidDate(t *testing.T) {
	for _, query := range []string{"start_date=not-a-date", "end_date=2026-13-01"} {
		t.Run(query, func(t *testing.T) {
			repo := &apiKeyStatsRepoStub{}
			router := newAPIKeyStatsTestRouter(repo, 42)

			req := httptest.NewRequest(http.MethodGet, "/usage/dashboard/api-keys-usage-stats?"+query+"&timezone=UTC", nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.False(t, repo.called)
		})
	}
}

func TestDashboardAPIKeysUsageStatsIgnoresUnsupportedFilters(t *testing.T) {
	repo := &apiKeyStatsRepoStub{}
	router := newAPIKeyStatsTestRouter(repo, 42)

	// api_key_id 等过滤参数不再参与校验或查询：既不查密钥归属，也不应静默生效
	req := httptest.NewRequest(
		http.MethodGet,
		"/usage/dashboard/api-keys-usage-stats?api_key_id=999&model=glm-5.3&billing_mode=invalid&timezone=UTC",
		nil,
	)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, repo.called)
}
