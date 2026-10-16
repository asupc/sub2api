package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestModelCapabilityBenchmarkMigrationIsIdempotent(t *testing.T) {
	content, err := FS.ReadFile("243_so_model_capability_benchmark.sql")
	require.NoError(t, err)
	sql := string(content)
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS model_benchmark_plans")
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS model_benchmark_runs")
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS model_benchmark_run_items")
	require.Contains(t, sql, "enabled BOOLEAN NOT NULL DEFAULT FALSE")
	require.Contains(t, sql, "running_status VARCHAR(20) NOT NULL DEFAULT 'idle'")
	require.Contains(t, sql, "claim_expires_at TIMESTAMPTZ")
	require.False(t, strings.Contains(strings.ToLower(sql), "drop table"), "benchmark migration must be forward-only and non-destructive")
}

func TestModelBenchmarkManualTriggerMigrationIsIdempotent(t *testing.T) {
	content, err := FS.ReadFile("244_so_model_benchmark_manual_trigger.sql")
	require.NoError(t, err)
	sql := string(content)
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS manual_trigger_at TIMESTAMPTZ")
	// 手动排队与租约恢复都要求索引不再带 enabled 条件（停用计划同样可执行）。
	require.Contains(t, sql, "DROP INDEX IF EXISTS idx_model_benchmark_plans_due")
	require.Contains(t, sql, "DROP INDEX IF EXISTS idx_model_benchmark_plans_claim_recovery")
	require.Contains(t, sql, "CREATE INDEX IF NOT EXISTS idx_model_benchmark_plans_manual")
	require.False(t, strings.Contains(strings.ToLower(sql), "drop table"), "benchmark migration must be forward-only and non-destructive")
}
