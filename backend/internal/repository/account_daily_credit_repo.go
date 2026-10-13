package repository

// account_daily_credit_repo.go 二开新增：qoder / workbuddy 账号每日积分消耗
// 累计的存储实现（表见 migrations/242_so_account_daily_credits.sql）。
//
// 写入是每请求一次的最热路径（RecordUsage 内 best-effort 调用），UPSERT 原子累加；
// 读取走批量接口供账号列表的 today-stats 聚合，禁止 per-account N+1。
// day 由 service 层以 timezone.Today() 生成（yyyy-mm-dd），与读侧同源。

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

// accountDailyCreditsUpsertSQL 是累加写入的单一事实来源（单测断言 UPSERT 语义）。
const accountDailyCreditsUpsertSQL = `
INSERT INTO account_daily_credits (account_id, day, credits, updated_at)
VALUES ($1, $2, $3, NOW())
ON CONFLICT (account_id, day) DO UPDATE SET
    credits = account_daily_credits.credits + EXCLUDED.credits,
    updated_at = NOW()
`

type accountDailyCreditRepository struct {
	db *sql.DB
}

// NewAccountDailyCreditRepository 构造每日积分仓库（纯 database/sql，无 ent 依赖）。
func NewAccountDailyCreditRepository(db *sql.DB) service.AccountDailyCreditRepository {
	return &accountDailyCreditRepository{db: db}
}

// AddCredits 原子累加 (accountID, day) 的积分消耗。
func (r *accountDailyCreditRepository) AddCredits(ctx context.Context, accountID int64, day string, credits float64) error {
	if r == nil || r.db == nil || accountID <= 0 || credits <= 0 {
		return nil
	}
	dayValue, err := parseAccountDailyCreditsDay(day)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, accountDailyCreditsUpsertSQL, accountID, dayValue, credits)
	if err != nil {
		return fmt.Errorf("add account daily credits: %w", err)
	}
	return nil
}

// CreditsByAccounts 批量读取多个账号在 day 的积分消耗（无记录的账号不出现在结果中）。
func (r *accountDailyCreditRepository) CreditsByAccounts(ctx context.Context, accountIDs []int64, day string) (map[int64]float64, error) {
	result := make(map[int64]float64, len(accountIDs))
	if r == nil || r.db == nil || len(accountIDs) == 0 {
		return result, nil
	}
	dayValue, err := parseAccountDailyCreditsDay(day)
	if err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `
SELECT account_id, credits
FROM account_daily_credits
WHERE day = $1 AND account_id = ANY($2)
`, dayValue, pq.Array(accountIDs))
	if err != nil {
		return nil, fmt.Errorf("query account daily credits: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var accountID int64
		var credits float64
		if err := rows.Scan(&accountID, &credits); err != nil {
			return nil, fmt.Errorf("scan account daily credits: %w", err)
		}
		result[accountID] = credits
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate account daily credits: %w", err)
	}
	return result, nil
}

// parseAccountDailyCreditsDay 把 yyyy-mm-dd 统计日解析为 DATE 参数（lib/pq 直传 time.Time）。
func parseAccountDailyCreditsDay(day string) (time.Time, error) {
	parsed, err := time.ParseInLocation("2006-01-02", day, time.UTC)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid account daily credits day %q: %w", day, err)
	}
	return parsed, nil
}
