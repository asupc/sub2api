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
}
