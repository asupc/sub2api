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

// 249 放宽作答明细难度约束：243 写死的 3..5 会拒绝 D6/D7 作答行，
// 导致测评在首个 D6 题中断、其余题按 0 分计入分母。
func TestModelBenchmarkDifficultyRangeMigrationIsIdempotent(t *testing.T) {
	content, err := FS.ReadFile("249_so_model_benchmark_difficulty_range.sql")
	require.NoError(t, err)
	sql := string(content)
	require.Contains(t, sql, "DROP CONSTRAINT IF EXISTS model_benchmark_run_items_difficulty_check")
	require.Contains(t, sql, "CHECK (difficulty BETWEEN 3 AND 7)")
	require.False(t, strings.Contains(strings.ToLower(sql), "drop table"), "benchmark migration must be forward-only and non-destructive")
}

func TestModelBenchmarkComparisonMigrationIsNonDestructive(t *testing.T) {
	raw, err := FS.ReadFile("245_so_model_benchmark_comparison.sql")
	require.NoError(t, err)
	text := string(raw)
	require.Contains(t, text, "DEFAULT 'legacy'")
	require.Contains(t, text, "DEFAULT 'baseline-v2'")
	require.Contains(t, text, "NUMERIC(16,10)")
	require.NotContains(t, strings.ToLower(text), "drop table")
}

// 246 去账号化：plans/runs 的 account_id 列与 plans 账号索引一并移除；
// 只允许 DROP COLUMN/DROP INDEX，禁止 DROP TABLE。
func TestModelBenchmarkGatewayModeMigrationIsIdempotent(t *testing.T) {
	content, err := FS.ReadFile("246_so_model_benchmark_gateway_mode.sql")
	require.NoError(t, err)
	sql := string(content)
	require.Contains(t, sql, "ALTER TABLE model_benchmark_plans DROP COLUMN IF EXISTS account_id")
	require.Contains(t, sql, "DROP INDEX IF EXISTS idx_model_benchmark_plans_account")
	require.Contains(t, sql, "ALTER TABLE model_benchmark_runs DROP COLUMN IF EXISTS account_id")
	require.False(t, strings.Contains(strings.ToLower(sql), "drop table"), "benchmark migration must be forward-only and non-destructive")
}
