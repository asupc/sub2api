package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/lib/pq"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
)

// apiKeyUsageStatsSelect 为 API 密钥用量统计的聚合列（按密钥与按密钥×模型两个查询共用）。
const apiKeyUsageStatsSelect = `
  COUNT(*)::bigint AS request_count,
  COALESCE(SUM(u.input_tokens), 0)::bigint AS input_tokens,
  COALESCE(SUM(u.output_tokens), 0)::bigint AS output_tokens,
  COALESCE(SUM(u.cache_creation_tokens), 0)::bigint AS cache_creation_tokens,
  COALESCE(SUM(u.cache_read_tokens), 0)::bigint AS cache_read_tokens,
  COALESCE(SUM(u.input_tokens + u.output_tokens + u.cache_creation_tokens + u.cache_read_tokens), 0)::bigint AS total_tokens,
  ROUND(
    AVG(
      CASE
        WHEN u.duration_ms > 0 AND u.output_tokens > 0
        THEN u.output_tokens * 1000.0 / u.duration_ms
      END
    )::numeric,
    2
  )::float8 AS avg_tokens_per_sec,
  COALESCE(ROUND(AVG(u.duration_ms)::numeric, 0), 0)::bigint AS avg_duration_ms
`

// GetAPIKeyUsageStats 返回指定时间范围内各 API 密钥的用量汇总（按总 Token 降序，含每模型明细）。
func (r *usageLogRepository) GetAPIKeyUsageStats(ctx context.Context, startTime, endTime time.Time, limit int) ([]usagestats.APIKeyUsageStatItem, error) {
	mainQuery := `
SELECT
  u.api_key_id,
  COALESCE(k.name, '') AS name,
  COALESCE(us.username, '') AS username,
` + apiKeyUsageStatsSelect + `
FROM usage_logs u
LEFT JOIN api_keys k ON u.api_key_id = k.id
LEFT JOIN users us ON k.user_id = us.id
WHERE u.created_at >= $1 AND u.created_at < $2
GROUP BY u.api_key_id, k.name, us.username
ORDER BY total_tokens DESC, u.api_key_id ASC
LIMIT $3`

	rows, err := r.sql.QueryContext(ctx, mainQuery, startTime, endTime, limit)
	if err != nil {
		return nil, err
	}
	defer func() {
		// 保持主错误优先；仅在无错误时回传 Close 失败。
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	items := make([]usagestats.APIKeyUsageStatItem, 0)
	keyIDs := make([]int64, 0)
	for rows.Next() {
		var (
			item     usagestats.APIKeyUsageStatItem
			name     sql.NullString
			username sql.NullString
		)
		if err = rows.Scan(
			&item.APIKeyID,
			&name,
			&username,
			&item.RequestCount,
			&item.InputTokens,
			&item.OutputTokens,
			&item.CacheCreationTokens,
			&item.CacheReadTokens,
			&item.TotalTokens,
			&item.AvgTokensPerSec,
			&item.AvgDurationMs,
		); err != nil {
			return nil, err
		}
		item.Name = name.String
		item.Username = username.String
		item.Models = []usagestats.APIKeyModelUsageStat{}
		items = append(items, item)
		keyIDs = append(keyIDs, item.APIKeyID)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return items, nil
	}

	modelQuery := `
SELECT
  u.api_key_id,
  u.model,
` + apiKeyUsageStatsSelect + `
FROM usage_logs u
WHERE u.created_at >= $1 AND u.created_at < $2
  AND u.api_key_id = ANY($3)
GROUP BY u.api_key_id, u.model
ORDER BY u.api_key_id ASC, total_tokens DESC, u.model ASC`

	modelRows, err := r.sql.QueryContext(ctx, modelQuery, startTime, endTime, pq.Array(keyIDs))
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := modelRows.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	byKey := make(map[int64]int, len(items))
	for idx := range items {
		byKey[items[idx].APIKeyID] = idx
	}
	for modelRows.Next() {
		var (
			apiKeyID int64
			stat     usagestats.APIKeyModelUsageStat
		)
		if err = modelRows.Scan(
			&apiKeyID,
			&stat.Model,
			&stat.RequestCount,
			&stat.InputTokens,
			&stat.OutputTokens,
			&stat.CacheCreationTokens,
			&stat.CacheReadTokens,
			&stat.TotalTokens,
			&stat.AvgTokensPerSec,
			&stat.AvgDurationMs,
		); err != nil {
			return nil, err
		}
		if idx, ok := byKey[apiKeyID]; ok {
			items[idx].Models = append(items[idx].Models, stat)
		}
	}
	if err = modelRows.Err(); err != nil {
		return nil, err
	}

	return items, nil
}

// GetAPIKeyUsageStatsByUser 返回某用户名下各 API 密钥在指定时间范围内的用量汇总（按总 Token 降序，含每模型明细）。
// 与 GetAPIKeyUsageStats 同口径，仅限定 u.user_id，供用户端按 API 统计用量。
func (r *usageLogRepository) GetAPIKeyUsageStatsByUser(ctx context.Context, userID int64, startTime, endTime time.Time, limit int) (items []usagestats.APIKeyUsageStatItem, err error) {
	mainQuery := `
SELECT
  u.api_key_id,
  COALESCE(k.name, '') AS name,
  COALESCE(us.username, '') AS username,
` + apiKeyUsageStatsSelect + `
FROM usage_logs u
LEFT JOIN api_keys k ON u.api_key_id = k.id
LEFT JOIN users us ON k.user_id = us.id
WHERE u.created_at >= $1 AND u.created_at < $2
  AND u.user_id = $4
GROUP BY u.api_key_id, k.name, us.username
ORDER BY total_tokens DESC, u.api_key_id ASC
LIMIT $3`

	rows, err := r.sql.QueryContext(ctx, mainQuery, startTime, endTime, limit, userID)
	if err != nil {
		return nil, err
	}
	defer func() {
		// 保持主错误优先；仅在无错误时回传 Close 失败（依赖命名返回值生效）。
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			items, err = nil, closeErr
		}
	}()

	items = make([]usagestats.APIKeyUsageStatItem, 0)
	keyIDs := make([]int64, 0)
	for rows.Next() {
		var (
			item     usagestats.APIKeyUsageStatItem
			name     sql.NullString
			username sql.NullString
		)
		if err = rows.Scan(
			&item.APIKeyID,
			&name,
			&username,
			&item.RequestCount,
			&item.InputTokens,
			&item.OutputTokens,
			&item.CacheCreationTokens,
			&item.CacheReadTokens,
			&item.TotalTokens,
			&item.AvgTokensPerSec,
			&item.AvgDurationMs,
		); err != nil {
			return nil, err
		}
		item.Name = name.String
		item.Username = username.String
		item.Models = []usagestats.APIKeyModelUsageStat{}
		items = append(items, item)
		keyIDs = append(keyIDs, item.APIKeyID)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return items, nil
	}

	// 明细查询同样限定 u.user_id，避免密钥归属与日志 user_id 不一致时串入他人用量。
	modelQuery := `
SELECT
  u.api_key_id,
  u.model,
` + apiKeyUsageStatsSelect + `
FROM usage_logs u
WHERE u.created_at >= $1 AND u.created_at < $2
  AND u.api_key_id = ANY($3)
  AND u.user_id = $4
GROUP BY u.api_key_id, u.model
ORDER BY u.api_key_id ASC, total_tokens DESC, u.model ASC`

	modelRows, err := r.sql.QueryContext(ctx, modelQuery, startTime, endTime, pq.Array(keyIDs), userID)
	if err != nil {
		return nil, err
	}
	defer func() {
		// 保持主错误优先；仅在无错误时回传 Close 失败（依赖命名返回值生效）。
		if closeErr := modelRows.Close(); closeErr != nil && err == nil {
			items, err = nil, closeErr
		}
	}()

	byKey := make(map[int64]int, len(items))
	for idx := range items {
		byKey[items[idx].APIKeyID] = idx
	}
	for modelRows.Next() {
		var (
			apiKeyID int64
			stat     usagestats.APIKeyModelUsageStat
		)
		if err = modelRows.Scan(
			&apiKeyID,
			&stat.Model,
			&stat.RequestCount,
			&stat.InputTokens,
			&stat.OutputTokens,
			&stat.CacheCreationTokens,
			&stat.CacheReadTokens,
			&stat.TotalTokens,
			&stat.AvgTokensPerSec,
			&stat.AvgDurationMs,
		); err != nil {
			return nil, err
		}
		if idx, ok := byKey[apiKeyID]; ok {
			items[idx].Models = append(items[idx].Models, stat)
		}
	}
	if err = modelRows.Err(); err != nil {
		return nil, err
	}

	return items, nil
}
