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
