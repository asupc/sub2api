//go:build unit

package service

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestQoderOpsService_Registration(t *testing.T) {
	svc := NewQoderOpsService(newQoderOAuthStubRepo(), NewQoderOAuthService(newQoderOAuthStubRepo(), nil), nil, 0)
	defer svc.Stop()

	require.Equal(t, PlatformQoder, svc.Platform())
	runner, ok := LookupPlatformOpsRunner(PlatformQoder)
	require.True(t, ok)
	require.Same(t, svc, runner)

	ids := make([]string, 0, len(svc.Tasks()))
	for _, task := range svc.Tasks() {
		ids = append(ids, task.ID)
		require.Equal(t, PlatformQoder, task.Platform)
	}
	require.Equal(t, []string{"checkin", "pro_welfare", "campaigns", "keepalive", "balance"}, ids)

	// 签到 / 保活 / 额度刷新对 error 账号也要执行（恢复与保号入口）。
	byID := map[string]PlatformOpsTask{}
	for _, task := range svc.Tasks() {
		byID[task.ID] = task
	}
	require.True(t, byID["checkin"].IncludeDisabled)
	require.True(t, byID["keepalive"].IncludeDisabled)
	require.True(t, byID["balance"].IncludeDisabled)
	require.Equal(t, 6*time.Hour, byID["balance"].Interval)
}

func TestQoderOpsService_RunAccountTask(t *testing.T) {
	repo := newQoderOAuthStubRepo()
	svc := NewQoderOpsService(repo, NewQoderOAuthService(repo, nil), nil, 0)
	defer svc.Stop()

	// 未知任务报错。
	require.Error(t, svc.RunAccountTask(context.Background(), qoderTestAccount(), "bogus"))

	// 已知任务（凭据缺失 → 执行失败）仍记录 ops_state。
	account := qoderTestAccount()
	delete(account.Credentials, "access_token")
	err := svc.RunAccountTask(context.Background(), account, "balance")
	require.Error(t, err)

	states, ok := account.Extra["ops_state"].(map[string]any)
	require.True(t, ok, "ops_state should be persisted, got %v", account.Extra)
	entryRaw, ok := states["balance"].(map[string]any)
	require.True(t, ok)
	require.NotEmpty(t, entryRaw["last_err"])
}

func TestQoderOpsTaskDue(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 10, 12, 11, 0, 0, 0, loc) // 周一 11:00
	account := qoderTestAccount()

	hourly := PlatformOpsTask{ID: "checkin", Hours: []int{9, 21}}
	interval := PlatformOpsTask{ID: "balance", Interval: 6 * time.Hour}
	manual := PlatformOpsTask{ID: "manual"}

	// 从未成功：当天 09:00 触发点已过 → 到期。
	require.True(t, qoderOpsTaskDue(account, hourly, now))
	// 今天 09:30 已执行 → 不到期（Hours 任务按 LastRun 判定，当日失败重试由
	// ops_state 退避 NextRun 驱动）。
	hourlyRan := OpsStateEntry{LastRun: time.Date(2026, 10, 12, 9, 30, 0, 0, loc)}
	require.False(t, qoderOpsTaskDue(withOpsState(account, hourlyRan), hourly, now))
	// 昨天 21:30 执行 → 今天 09:00 触发点已过 → 到期。
	yesterday := OpsStateEntry{LastRun: time.Date(2026, 10, 11, 21, 30, 0, 0, loc)}
	require.True(t, qoderOpsTaskDue(withOpsState(account, yesterday), hourly, now))
	// 触发点未到（现在 08:00）→ 不到期。
	early := time.Date(2026, 10, 12, 8, 0, 0, 0, loc)
	require.False(t, qoderOpsTaskDue(account, hourly, early))

	// 固定间隔：从未执行 → 到期；3 小时前执行 → 不到期；7 小时前执行 → 到期。
	require.True(t, qoderOpsTaskDue(account, interval, now))
	require.False(t, qoderOpsTaskDue(withOpsState(account, OpsStateEntry{LastRun: now.Add(-3 * time.Hour)}), interval, now))
	require.True(t, qoderOpsTaskDue(withOpsState(account, OpsStateEntry{LastRun: now.Add(-7 * time.Hour)}), interval, now))

	// 退避顺延优先：NextRun 在未来 → 不到期。
	require.False(t, qoderOpsTaskDue(withOpsState(account, OpsStateEntry{NextRun: now.Add(10 * time.Minute)}), hourly, now))

	// 仅手动任务恒不到期。
	require.False(t, qoderOpsTaskDue(account, manual, now))
}

// withOpsState 把任务状态预置进账号 extra（模拟历史执行记录；extra 里的条目
// 须是 JSONB 形态的 map，LoadOpsState 经 map 断言还原）。
func withOpsState(account *Account, entry OpsStateEntry) *Account {
	blob, err := json.Marshal(entry)
	if err != nil {
		panic(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(blob, &raw); err != nil {
		panic(err)
	}
	if account.Extra == nil {
		account.Extra = map[string]any{}
	}
	states, ok := account.Extra["ops_state"].(map[string]any)
	if !ok {
		states = map[string]any{}
		account.Extra["ops_state"] = states
	}
	states["checkin"] = raw
	states["balance"] = raw
	states["manual"] = raw
	return account
}

// TestQoderOpsRunCheckin 签到全链路（假 openapi）：CLAIMED 状态判已签 → 幂等成功
// → 刷新额度快照 → qd_no_credit 罚停账号自动恢复。
func TestQoderOpsRunCheckin(t *testing.T) {
	endpoints := newQoderTokenEndpoints()
	defer endpoints.server.Close()
	patchQoderRealm(t, "cn", endpoints.server.URL)
	endpoints.handle("/sash/api/v1/me/daily-check-in/status", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"CLAIMED","lastClaimedAt":` + strconv.FormatInt(time.Now().Unix(), 10) + `,"currentStreakDays":3}`))
	})
	endpoints.handle("/api/v2/quota/usage", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"userQuota":{"remaining":6,"used":4,"total":10},"addOnQuota":{"remaining":0,"used":0,"total":0}}`))
	})
	endpoints.handle("/api/v2/user/plan", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"plan_tier_name":"Max"}`))
	})

	account := qoderTestAccount()
	account.Status = StatusError
	account.ErrorMessage = QoderNoCreditReason
	account.Schedulable = false
	repo := newQoderOAuthStubRepo(account)
	svc := NewQoderOpsService(repo, NewQoderOAuthService(repo, nil), nil, 0)
	defer svc.Stop()

	require.NoError(t, svc.RunAccountTask(context.Background(), account, "checkin"))

	// 额度快照已刷新（签到成功联动 balance）。
	quota, ok := repo.extras[account.ID][qoderQuotaExtraKey].(QoderQuotaSnapshot)
	require.True(t, ok)
	require.Equal(t, 6, quota.Remain)
	require.Equal(t, "Max", quota.Plan)
	// 签到到账 → 罚停恢复。
	require.True(t, repo.cleared[account.ID])
	require.Equal(t, StatusActive, account.Status)
}

// TestQoderOpsRunCampaigns 活动扫描：仅 CLAIMABLE 条目发起领取，BLOCKED /
// 已领取等确定性结局不算任务失败。
func TestQoderOpsRunCampaigns(t *testing.T) {
	endpoints := newQoderTokenEndpoints()
	defer endpoints.server.Close()
	patchQoderRealm(t, "cn", endpoints.server.URL)
	endpoints.handle("/sash/api/v1/me/campaigns", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"showCampaign":true,"claimable":true,"campaigns":[
			{"campaignId":"c-claimed","claimStatus":"CLAIMED"},
			{"campaignId":"c-ready","claimStatus":"CLAIMABLE"},
			{"campaignId":"c-disabled","claimStatus":"DISABLED"}
		]}`))
	})
	endpoints.handle("/sash/api/v1/me/campaigns/c-ready/claim", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"CLAIMED","amount":50,"replayed":false}`))
	})

	repo := newQoderOAuthStubRepo()
	svc := NewQoderOpsService(repo, NewQoderOAuthService(repo, nil), nil, 0)
	defer svc.Stop()

	require.NoError(t, svc.RunAccountTask(context.Background(), qoderTestAccount(), "campaigns"))

	// 只领取了 CLAIMABLE 条目。
	paths := endpoints.called()
	require.Len(t, paths, 2)
	require.Equal(t, "GET /sash/api/v1/me/campaigns", paths[0])
	require.Equal(t, "POST /sash/api/v1/me/campaigns/c-ready/claim", paths[1])
}

// TestQoderOpsRunProWelfare Pro 福利包：无资格按成功跳过（不 claim）。
func TestQoderOpsRunProWelfare(t *testing.T) {
	endpoints := newQoderTokenEndpoints()
	defer endpoints.server.Close()
	patchQoderRealm(t, "cn", endpoints.server.URL)
	endpoints.handle("/sash/api/v1/me/pro-upgrade/eligibility", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"eligible":false}`))
	})

	repo := newQoderOAuthStubRepo()
	svc := NewQoderOpsService(repo, NewQoderOAuthService(repo, nil), nil, 0)
	defer svc.Stop()

	require.NoError(t, svc.RunAccountTask(context.Background(), qoderTestAccount(), "pro_welfare"))
	require.Equal(t, []string{"GET /sash/api/v1/me/pro-upgrade/eligibility"}, endpoints.called())
}
