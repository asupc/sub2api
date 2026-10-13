package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func newAccountDailyCreditRepositoryTestDB(t *testing.T) (*accountDailyCreditRepository, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return &accountDailyCreditRepository{db: db}, mock
}

func TestAccountDailyCreditRepositoryAddCredits(t *testing.T) {
	repo, mock := newAccountDailyCreditRepositoryTestDB(t)

	mock.ExpectExec("INSERT INTO account_daily_credits").
		WithArgs(int64(7), time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC), 1.25).
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, repo.AddCredits(context.Background(), 7, "2026-10-13", 1.25))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAccountDailyCreditRepositoryAddCreditsUpsertSQL(t *testing.T) {
	// UPSERT 语义断言：DO UPDATE 必须用既有累计值 + EXCLUDED（新值）相加，而不是覆盖。
	require.Regexp(t, regexp.MustCompile(`ON CONFLICT \(account_id, day\) DO UPDATE SET\s+credits = account_daily_credits\.credits \+ EXCLUDED\.credits`), accountDailyCreditsUpsertSQL)
}

func TestAccountDailyCreditRepositoryAddCreditsSkipsNonPositive(t *testing.T) {
	repo, mock := newAccountDailyCreditRepositoryTestDB(t)
	// 无 ExpectExec：credits <= 0 与 accountID <= 0 直接短路，不应产生 SQL。
	require.NoError(t, repo.AddCredits(context.Background(), 7, "2026-10-13", 0))
	require.NoError(t, repo.AddCredits(context.Background(), 0, "2026-10-13", 1))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAccountDailyCreditRepositoryAddCreditsRejectsBadDay(t *testing.T) {
	repo, _ := newAccountDailyCreditRepositoryTestDB(t)
	require.Error(t, repo.AddCredits(context.Background(), 7, "not-a-day", 1))
}

func TestAccountDailyCreditRepositoryCreditsByAccounts(t *testing.T) {
	repo, mock := newAccountDailyCreditRepositoryTestDB(t)

	mock.ExpectQuery("SELECT account_id, credits").
		WithArgs(time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"account_id", "credits"}).
			AddRow(int64(7), 12.5).
			AddRow(int64(9), 3.0))

	got, err := repo.CreditsByAccounts(context.Background(), []int64{7, 8, 9}, "2026-10-13")
	require.NoError(t, err)
	require.Equal(t, map[int64]float64{7: 12.5, 9: 3.0}, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAccountDailyCreditRepositoryCreditsByAccountsEmpty(t *testing.T) {
	repo, mock := newAccountDailyCreditRepositoryTestDB(t)
	got, err := repo.CreditsByAccounts(context.Background(), nil, "2026-10-13")
	require.NoError(t, err)
	require.Empty(t, got)
	require.NoError(t, mock.ExpectationsWereMet())
}
