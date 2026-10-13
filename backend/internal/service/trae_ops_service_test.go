//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/trae"
	"github.com/stretchr/testify/require"
)

// traeOpsStubRepo 记录 UpdateExtra 调用的最小账号仓库（未实现方法经内嵌接口
// 保持类型满足，调用即 panic 暴露误用）。
type traeOpsStubRepo struct {
	AccountRepository
	extras map[int64]map[string]any
}

func (r *traeOpsStubRepo) UpdateExtra(_ context.Context, id int64, updates map[string]any) error {
	if r.extras == nil {
		r.extras = make(map[int64]map[string]any)
	}
	base := r.extras[id]
	if base == nil {
		base = make(map[string]any, len(updates))
		r.extras[id] = base
	}
	for key, value := range updates {
		base[key] = value
	}
	return nil
}

func TestTraeOpsService_Registration(t *testing.T) {
	t.Parallel()
	svc := NewTraeOpsService(&traeOpsStubRepo{}, nil, nil, 0)

	require.Equal(t, PlatformTrae, svc.Platform())
	runner, ok := LookupPlatformOpsRunner(PlatformTrae)
	require.True(t, ok)
	require.Same(t, svc, runner)

	ids := make([]string, 0, len(svc.Tasks()))
	for _, task := range svc.Tasks() {
		ids = append(ids, task.ID)
		require.Equal(t, PlatformTrae, task.Platform)
	}
	require.Equal(t, []string{"checkin", "credits_monitor", "usage_refresh"}, ids)
}

func TestTraeOpsService_RunAccountTask(t *testing.T) {
	t.Parallel()
	repo := &traeOpsStubRepo{}
	svc := NewTraeOpsService(repo, nil, nil, 0)

	// 未知任务报错。
	require.Error(t, svc.RunAccountTask(context.Background(), traeTestAccount(), "bogus"))

	// 已知任务（凭据缺失 → 执行失败）仍记录 ops_state。
	account := traeTestAccount()
	delete(account.Credentials, "variant")
	err := svc.RunAccountTask(context.Background(), account, "usage_refresh")
	require.Error(t, err)

	states, ok := repo.extras[account.ID]["ops_state"].(map[string]any)
	require.True(t, ok, "ops_state should be persisted, got %v", repo.extras[account.ID])
	entryRaw, ok := states["usage_refresh"].(map[string]any)
	require.True(t, ok)
	require.Contains(t, entryRaw["last_err"], "variant")
}

func TestTraeOpsTaskDue(t *testing.T) {
	t.Parallel()
	loc := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 10, 12, 11, 0, 0, 0, loc) // 周一 11:00

	hourly := PlatformOpsTask{ID: "checkin", Hours: []int{9}}
	interval := PlatformOpsTask{ID: "usage_refresh", Interval: 6 * time.Hour}
	manual := PlatformOpsTask{ID: "manual"}

	// 从未成功：当天 09:00 触发点已过 → 到期。
	require.True(t, traeOpsTaskDue(hourly, OpsStateEntry{}, now, loc))
	// 今天 09:30 已成功 → 不再到期。
	require.False(t, traeOpsTaskDue(hourly, OpsStateEntry{LastOK: time.Date(2026, 10, 12, 9, 30, 0, 0, loc)}, now, loc))
	// 昨天 09:30 成功 → 今天触发点已过 → 到期。
	require.True(t, traeOpsTaskDue(hourly, OpsStateEntry{LastOK: time.Date(2026, 10, 11, 9, 30, 0, 0, loc)}, now, loc))
	// 触发点未到（现在是 08:00）→ 不到期。
	early := time.Date(2026, 10, 12, 8, 0, 0, 0, loc)
	require.False(t, traeOpsTaskDue(hourly, OpsStateEntry{}, early, loc))

	// 固定间隔：从未执行 → 到期；3 小时前执行 → 不到期；7 小时前执行 → 到期。
	require.True(t, traeOpsTaskDue(interval, OpsStateEntry{}, now, loc))
	require.False(t, traeOpsTaskDue(interval, OpsStateEntry{LastRun: now.Add(-3 * time.Hour)}, now, loc))
	require.True(t, traeOpsTaskDue(interval, OpsStateEntry{LastRun: now.Add(-7 * time.Hour)}, now, loc))

	// 退避顺延优先：NextRun 在未来 → 不到期。
	require.False(t, traeOpsTaskDue(hourly, OpsStateEntry{NextRun: now.Add(10 * time.Minute)}, now, loc))

	// 仅手动任务恒不到期。
	require.False(t, traeOpsTaskDue(manual, OpsStateEntry{}, now, loc))
}

func TestTraeCheckinBusinessSuccess(t *testing.T) {
	t.Parallel()
	require.False(t, traeCheckinBusinessSuccess(nil))
	require.False(t, traeCheckinBusinessSuccess(&trae.ClaimResult{HTTPStatus: 0, Message: "network down"}))
	require.False(t, traeCheckinBusinessSuccess(&trae.ClaimResult{HTTPStatus: 500, Message: "server error"}))
	require.True(t, traeCheckinBusinessSuccess(&trae.ClaimResult{HTTPStatus: 200, Code: 1005, HasCode: true, Message: "套餐额度限制"}))
	require.True(t, traeCheckinBusinessSuccess(&trae.ClaimResult{HTTPStatus: 200, Code: 1001, HasCode: true, Message: "already checked in"}))
	require.True(t, traeCheckinBusinessSuccess(&trae.ClaimResult{HTTPStatus: 200, Message: "今日已签到"}))
}

func TestTraeEntitlementsExpiring(t *testing.T) {
	t.Parallel()
	require.False(t, traeEntitlementsExpiring(nil))
	require.False(t, traeEntitlementsExpiring(&trae.Entitlements{Packages: []trae.CreditPackage{
		{ExpiringSoon: false, ExpireAt: time.Now().Add(30 * 24 * time.Hour).Unix()},
	}}))
	require.True(t, traeEntitlementsExpiring(&trae.Entitlements{Packages: []trae.CreditPackage{
		{ExpiringSoon: true, ExpireAt: time.Now().Add(3 * 24 * time.Hour).Unix()},
	}}))
	// 已过期的包不算临期。
	require.False(t, traeEntitlementsExpiring(&trae.Entitlements{Packages: []trae.CreditPackage{
		{ExpiringSoon: true, Expired: true, ExpireAt: time.Now().Add(-24 * time.Hour).Unix()},
	}}))
}

func TestWriteTraeQuotaExtra(t *testing.T) {
	t.Parallel()
	repo := &traeOpsStubRepo{}
	account := traeTestAccount()

	entitlements := &trae.Entitlements{Total: 42.5, Packages: []trae.CreditPackage{{PackageName: "试用包", Total: 60, Remaining: 42.5}}}
	require.NoError(t, writeTraeQuotaExtra(context.Background(), repo, account, entitlements, map[string]any{"expiring_soon": false}))

	persisted, ok := repo.extras[account.ID]["trae_quota"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, 42.5, persisted["total"])
	require.Equal(t, false, persisted["expiring_soon"])
	// 内存副本同步更新。
	inMemory, ok := account.Extra["trae_quota"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, 42.5, inMemory["total"])
}
