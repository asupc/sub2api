package service

// workbuddy_usage.go 二开新增：WorkBuddy 账号积分余额查询（wb_quota 快照）与
// 积分耗尽后的自动恢复。供 admin quota 端点与 ops 的 balance/checkin 任务复用。
//
// 快照落在 account.extra["wb_quota"]，形状：
//
//	{"remain":100,"total":500,"expiring":20,"expiring_at":"...","updated_at":"...",
//	 "enterprise":false,"unlimited":false}

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// WorkBuddy 账号 extra / 错误原因常量。
const (
	// workbuddyQuotaExtraKey 积分快照在 extra 中的键。
	workbuddyQuotaExtraKey = "wb_quota"
	// workbuddySessionStrikesExtraKey 12153 session dead 的连续容忍计数。
	workbuddySessionStrikesExtraKey = "wb_session_strikes"
	// workbuddySessionStrikeLimit 连续 3 次 session dead 才罚号（首次仅换号）。
	workbuddySessionStrikeLimit = 3
	// WorkbuddyNoCreditReason 积分耗尽罚号的 error reason（恢复逻辑按它识别）。
	WorkbuddyNoCreditReason = "wb_no_credit"
)

// WorkbuddyQuotaSnapshot 积分余额快照（extra["wb_quota"] 的形状）。
type WorkbuddyQuotaSnapshot struct {
	Remain     int64  `json:"remain"`
	Total      int64  `json:"total"`
	Expiring   int64  `json:"expiring,omitempty"`
	ExpiringAt string `json:"expiring_at,omitempty"`
	UpdatedAt  string `json:"updated_at"`
	Enterprise bool   `json:"enterprise,omitempty"`
	Unlimited  bool   `json:"unlimited,omitempty"`
}

// workbuddyExpiringSoon 快过期分桶窗口（48h 内到期的积分单独计数，供调度优先消耗）。
const workbuddyExpiringSoon = 48 * time.Hour

// QueryWorkbuddyQuota 查询单账号积分余额：落 extra["wb_quota"] 快照并做
// wb_no_credit 恢复判定（余额恢复且账号是本逻辑罚停的 → 清错误并恢复调度）。
func (s *WorkbuddyOAuthService) QueryWorkbuddyQuota(ctx context.Context, accountID int64) (*WorkbuddyQuotaSnapshot, error) {
	if s == nil || s.accountRepo == nil {
		return nil, infraerrors.New(http.StatusInternalServerError, "WORKBUDDY_QUOTA_REPO_NOT_CONFIGURED", "account repository is not configured")
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if account.Platform != PlatformWorkbuddy {
		return nil, infraerrors.New(http.StatusBadRequest, "WORKBUDDY_QUOTA_INVALID_ACCOUNT", "account platform does not match WorkBuddy quota endpoint")
	}
	snapshot, err := s.probeWorkbuddyQuota(ctx, account)
	if err != nil {
		return nil, err
	}
	if err := s.accountRepo.UpdateExtra(ctx, account.ID, map[string]any{workbuddyQuotaExtraKey: snapshot}); err != nil {
		slog.Warn("workbuddy quota snapshot persist failed", "account_id", account.ID, "error", err)
	}
	s.recoverWorkbuddyNoCredit(ctx, account, snapshot)
	return snapshot, nil
}

// probeWorkbuddyQuota 调上游余额接口（企业口径自动分流，见 pkg billing.go）。
func (s *WorkbuddyOAuthService) probeWorkbuddyQuota(ctx context.Context, account *Account) (*WorkbuddyQuotaSnapshot, error) {
	cred, err := parseWorkbuddyCredentials(account)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadRequest, "WORKBUDDY_QUOTA_INVALID_CREDENTIALS", "账号凭证不完整: %v", err)
	}
	proxyURL, err := s.proxyURL(ctx, account.ProxyID)
	if err != nil {
		return nil, err
	}
	client := newWorkbuddyUpstreamClient(proxyURL, 30*time.Second)
	balance, err := client.UserResourceDetailedWithExpiry(ctx, cred, workbuddyExpiringSoon)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "WORKBUDDY_QUOTA_UPSTREAM_FAILED", "查询 WorkBuddy 余额失败: %v", err)
	}
	snapshot := &WorkbuddyQuotaSnapshot{
		Remain:     balance.Remain,
		Total:      balance.Total,
		Expiring:   balance.Expiring,
		Enterprise: balance.Enterprise,
		Unlimited:  balance.Unlimited,
		UpdatedAt:  time.Now().Format(time.RFC3339),
	}
	if !balance.EarliestExpiry.IsZero() {
		snapshot.ExpiringAt = balance.EarliestExpiry.Format(time.RFC3339)
	}
	return snapshot, nil
}

// recoverWorkbuddyNoCredit 余额恢复（或企业不限量）且账号是被 wb_no_credit 罚停的，
// 清除错误状态并恢复调度。其他来源的 error 不动（避免误恢复人工停用的账号）。
func (s *WorkbuddyOAuthService) recoverWorkbuddyNoCredit(ctx context.Context, account *Account, snapshot *WorkbuddyQuotaSnapshot) {
	if snapshot == nil {
		return
	}
	healthy := snapshot.Unlimited || snapshot.Remain > 0
	if !healthy || account.Status != StatusError || account.ErrorMessage != WorkbuddyNoCreditReason {
		return
	}
	if err := s.accountRepo.ClearError(ctx, account.ID); err != nil {
		slog.Warn("workbuddy no-credit recovery clear error failed", "account_id", account.ID, "error", err)
		return
	}
	if err := s.accountRepo.SetSchedulable(ctx, account.ID, true); err != nil {
		slog.Warn("workbuddy no-credit recovery re-enable failed", "account_id", account.ID, "error", err)
		return
	}
	account.Status = StatusActive
	account.ErrorMessage = ""
	account.Schedulable = true
	slog.Info("workbuddy account recovered from no-credit", "account_id", account.ID, "remain", snapshot.Remain)
}

// ZeroWorkbuddyQuotaExtra 积分耗尽时把快照清零（402 路径调用，配合罚号）。
func ZeroWorkbuddyQuotaExtra(ctx context.Context, repo AccountRepository, accountID int64) {
	if repo == nil {
		return
	}
	snapshot := WorkbuddyQuotaSnapshot{Remain: 0, Total: 0, UpdatedAt: time.Now().Format(time.RFC3339)}
	if err := repo.UpdateExtra(ctx, accountID, map[string]any{workbuddyQuotaExtraKey: snapshot}); err != nil {
		slog.Warn("workbuddy quota zero-out failed", "account_id", accountID, "error", err)
	}
}
