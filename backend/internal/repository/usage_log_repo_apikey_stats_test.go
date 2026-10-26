package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestUsageLogRepositoryGetAPIKeyUsageStats(t *testing.T) {
	start := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)

	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	mainRows := mock.NewRows([]string{
		"api_key_id", "name", "username",
		"request_count", "input_tokens", "output_tokens", "cache_creation_tokens", "cache_read_tokens",
		"total_tokens", "avg_tokens_per_sec", "avg_duration_ms",
	})
	mainRows.AddRow(int64(11), "自动开发", "小菜鸡",
		int64(10), int64(1000), int64(400), int64(50), int64(8000), int64(9450), 37.5, int64(8000))
	mainRows.AddRow(int64(12), "客服机器人", "dev",
		int64(4), int64(200), int64(100), int64(0), int64(0), int64(300), nil, int64(2500))
	mock.ExpectQuery(regexp.QuoteMeta("LEFT JOIN api_keys k ON u.api_key_id = k.id")).WillReturnRows(mainRows)

	modelRows := mock.NewRows([]string{
		"api_key_id", "model",
		"request_count", "input_tokens", "output_tokens", "cache_creation_tokens", "cache_read_tokens",
		"total_tokens", "avg_tokens_per_sec", "avg_duration_ms",
	})
	modelRows.AddRow(int64(11), "glm-5.3",
		int64(6), int64(700), int64(300), int64(50), int64(6000), int64(7050), 40.2, int64(9000))
	modelRows.AddRow(int64(11), "deepseek-flash",
		int64(4), int64(300), int64(100), int64(0), int64(2000), int64(2400), 115.8, int64(3918))
	modelRows.AddRow(int64(12), "gpt-5.6-sol",
		int64(4), int64(200), int64(100), int64(0), int64(0), int64(300), 11.37, int64(2500))
	mock.ExpectQuery(regexp.QuoteMeta("api_key_id = ANY($3)")).WillReturnRows(modelRows)

	items, err := repo.GetAPIKeyUsageStats(context.Background(), start, end, 50)
	require.NoError(t, err)
	require.Len(t, items, 2)

	first := items[0]
	require.Equal(t, int64(11), first.APIKeyID)
	require.Equal(t, "自动开发", first.Name)
	require.Equal(t, "小菜鸡", first.Username)
	require.Equal(t, int64(10), first.RequestCount)
	require.Equal(t, int64(9450), first.TotalTokens)
	require.NotNil(t, first.AvgTokensPerSec)
	require.Equal(t, 37.5, *first.AvgTokensPerSec)
	require.Len(t, first.Models, 2)
	// 模型明细按 total_tokens 降序
	require.Equal(t, "glm-5.3", first.Models[0].Model)
	require.Equal(t, int64(7050), first.Models[0].TotalTokens)
	require.Equal(t, "deepseek-flash", first.Models[1].Model)

	second := items[1]
	require.Equal(t, int64(12), second.APIKeyID)
	require.Nil(t, second.AvgTokensPerSec)
	require.Len(t, second.Models, 1)
}

func TestUsageLogRepositoryGetAPIKeyUsageStats_Empty(t *testing.T) {
	start := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)

	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	emptyRows := mock.NewRows([]string{
		"api_key_id", "name", "username",
		"request_count", "input_tokens", "output_tokens", "cache_creation_tokens", "cache_read_tokens",
		"total_tokens", "avg_tokens_per_sec", "avg_duration_ms",
	})
	mock.ExpectQuery(regexp.QuoteMeta("LEFT JOIN api_keys k ON u.api_key_id = k.id")).WillReturnRows(emptyRows)

	items, err := repo.GetAPIKeyUsageStats(context.Background(), start, end, 50)
	require.NoError(t, err)
	require.NotNil(t, items)
	require.Empty(t, items)
}

func TestUsageLogRepositoryGetAPIKeyUsageStatsByUser(t *testing.T) {
	start := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)

	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}

	mainRows := mock.NewRows([]string{
		"api_key_id", "name", "username",
		"request_count", "input_tokens", "output_tokens", "cache_creation_tokens", "cache_read_tokens",
		"total_tokens", "avg_tokens_per_sec", "avg_duration_ms",
	})
	mainRows.AddRow(int64(11), "自动开发", "小菜鸡",
		int64(10), int64(1000), int64(400), int64(50), int64(8000), int64(9450), 37.5, int64(8000))
	mock.ExpectQuery(regexp.QuoteMeta("AND u.user_id = $4")).
		WithArgs(start, end, 50, int64(42)).
		WillReturnRows(mainRows)

	modelRows := mock.NewRows([]string{
		"api_key_id", "model",
		"request_count", "input_tokens", "output_tokens", "cache_creation_tokens", "cache_read_tokens",
		"total_tokens", "avg_tokens_per_sec", "avg_duration_ms",
	})
	modelRows.AddRow(int64(11), "glm-5.3",
		int64(6), int64(700), int64(300), int64(50), int64(6000), int64(7050), 40.2, int64(9000))
	mock.ExpectQuery(regexp.QuoteMeta("api_key_id = ANY($3)")).
		WithArgs(start, end, sqlmock.AnyArg(), int64(42)).
		WillReturnRows(modelRows)

	items, err := repo.GetAPIKeyUsageStatsByUser(context.Background(), 42, start, end, 50)
	require.NoError(t, err)
	require.Len(t, items, 1)

	first := items[0]
	require.Equal(t, int64(11), first.APIKeyID)
	require.Equal(t, "自动开发", first.Name)
	require.Equal(t, int64(9450), first.TotalTokens)
	require.Len(t, first.Models, 1)
	require.Equal(t, "glm-5.3", first.Models[0].Model)
}
