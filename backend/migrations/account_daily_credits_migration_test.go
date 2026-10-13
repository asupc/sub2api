package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountDailyCreditsMigration(t *testing.T) {
	content, err := FS.ReadFile("242_so_account_daily_credits.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS account_daily_credits")
	require.Contains(t, sql, "PRIMARY KEY (account_id, day)")
	require.Contains(t, sql, "REFERENCES accounts(id) ON DELETE CASCADE")
	require.Contains(t, sql, "credits DECIMAL(20, 10) NOT NULL DEFAULT 0")
	require.Contains(t, sql, "CREATE INDEX IF NOT EXISTS idx_account_daily_credits_day")
}
