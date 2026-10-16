//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestModelBenchmarkMigrationCreatesCoreTables(t *testing.T) {
	var count int
	err := integrationDB.QueryRowContext(context.Background(), `
		SELECT COUNT(*) FROM information_schema.tables
		WHERE table_schema = 'public'
		  AND table_name IN ('model_benchmark_plans', 'model_benchmark_runs', 'model_benchmark_run_items')
	`).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 3, count, "model benchmark migration must create all core tables")

	var columns int
	err = integrationDB.QueryRowContext(context.Background(), `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'model_benchmark_plans'
		  AND column_name = 'comparison_key'
	`).Scan(&columns)
	require.NoError(t, err)
	require.Equal(t, 1, columns, "comparison key must be applied by migrations")

	err = integrationDB.QueryRowContext(context.Background(), `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'model_benchmark_runs'
		  AND column_name IN ('comparison_key', 'max_output_tokens', 'timeout_seconds', 'sample_count', 'quality')
	`).Scan(&columns)
	require.NoError(t, err)
	require.Equal(t, 5, columns, "comparison and budget evidence columns must be applied by migrations")
}
