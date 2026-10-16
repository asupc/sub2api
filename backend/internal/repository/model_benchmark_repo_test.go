package repository

import (
	"context"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestModelBenchmarkRepositoryClaimDuePlansUsesAtomicLease(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	now := time.Now().UTC().Truncate(time.Second)
	lease := 25 * time.Hour
	columns := []string{"id", "name", "suite_id", "suite_version", "scoring_version", "model_id", "account_id", "cron_expression", "enabled", "sample_count", "max_output_tokens", "timeout_seconds", "max_results", "running_status", "claim_expires_at", "manual_trigger_at", "last_run_at", "next_run_at", "created_at", "updated_at"}
	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)FROM model_benchmark_plans.*FOR UPDATE SKIP LOCKED`).
		WithArgs(now, 1).
		WillReturnRows(sqlmock.NewRows(columns).AddRow(int64(1), "weekly", "hard-v1", "1.0.0", "1.0.0", "gpt-test", int64(2), "0 4 * * 1", true, 1, 768, 90, 50, "idle", nil, nil, nil, now, now, now))
	mock.ExpectExec(`UPDATE model_benchmark_plans SET running_status='running',claim_expires_at=\$2,manual_trigger_at=NULL`).
		WithArgs(int64(1), now.Add(lease)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	repo := NewModelBenchmarkRepository(db)
	plans, err := repo.ClaimDuePlans(context.Background(), now, lease, 1)
	require.NoError(t, err)
	require.Len(t, plans, 1)
	require.Equal(t, int64(1), plans[0].ID)
	require.Equal(t, "idle", plans[0].RunningStatus)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestModelBenchmarkRepositoryReleaseExpiredClaims(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	now := time.Now().UTC()
	mock.ExpectExec(`UPDATE model_benchmark_plans SET running_status='idle'`).
		WithArgs(now).
		WillReturnResult(sqlmock.NewResult(0, 2))
	repo := NewModelBenchmarkRepository(db)
	recovered, err := repo.ReleaseExpiredClaims(context.Background(), now)
	require.NoError(t, err)
	require.Equal(t, int64(2), recovered)
	require.NoError(t, mock.ExpectationsWereMet())
}
