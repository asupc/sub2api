//go:build unit

package service

// workbuddy_ops_service_test.go 单元测试：任务注册表 / 到期判定（latestDailySlot、
// taskDue 的 Hours 与 Interval 语义、退避 NextRun）/ 任务分派与状态记录 / 企业账号跳过。

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestWorkbuddyOpsService_TaskRegistryMatchesSpec(t *testing.T) {
	repo := &wbRepoRecorder{}
	svc := NewWorkbuddyOpsService(repo, NewWorkbuddyOAuthService(repo, nil), nil, time.Minute)

	require.Equal(t, PlatformWorkbuddy, svc.Platform())
	byID := map[string]PlatformOpsTask{}
	for _, task := range svc.Tasks() {
		byID[task.ID] = task
	}
	require.Len(t, byID, 9)

	require.Equal(t, []int{9, 21}, byID["checkin"].Hours)
	require.True(t, byID["checkin"].IncludeDisabled)
	require.Equal(t, []int{9, 21}, byID["travel"].Hours)
	require.Equal(t, []int{10}, byID["activity"].Hours)
	require.Equal(t, []int{22}, byID["keepalive"].Hours)
	require.True(t, byID["keepalive"].IncludeDisabled)
	require.Equal(t, []int{23}, byID["blackcat"].Hours)
	require.Equal(t, []int{1}, byID["growth"].Hours)
	require.Equal(t, 4*time.Hour, byID["growth_sweep"].Interval, "补领清扫周期幂等（162762f）")
	require.Equal(t, 6*time.Hour, byID["compensation"].Interval, "补偿先查后领（d48b715）")
	require.True(t, byID["compensation"].IncludeDisabled, "补偿是积分恢复入口，error 账号也要领")
	require.Equal(t, 6*time.Hour, byID["balance"].Interval)
	require.True(t, byID["balance"].IncludeDisabled)

	// 注册表模式：构造即注册，通用端点可按平台查到。
	runner, ok := LookupPlatformOpsRunner(PlatformWorkbuddy)
	require.True(t, ok)
	require.Same(t, svc, runner)
}

func TestWorkbuddyOpsService_LatestDailySlot(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	mk := func(day int, h, m int) time.Time {
		return time.Date(2026, 10, day, h, m, 0, 0, loc)
	}
	// 09/21 双点位：各时刻的最近已到点。
	require.Equal(t, mk(12, 9, 0), latestDailySlot(mk(12, 12, 0), []int{9, 21}, loc))
	require.Equal(t, mk(12, 21, 0), latestDailySlot(mk(12, 21, 30), []int{9, 21}, loc))
	require.Equal(t, mk(11, 21, 0), latestDailySlot(mk(12, 3, 0), []int{9, 21}, loc), "凌晨的最近已到点是昨夜 21 点")
	require.Equal(t, mk(12, 9, 0), latestDailySlot(mk(12, 9, 0), []int{9, 21}, loc), "恰好命中 9 点整算已到")
}

func TestWorkbuddyOpsService_TaskDueHoursSemantics(t *testing.T) {
	loc := platformOpsTimezone(nil)
	repo := &wbRepoRecorder{}
	svc := NewWorkbuddyOpsService(repo, NewWorkbuddyOAuthService(repo, nil), nil, time.Minute)
	checkin := svc.taskByID("checkin")

	account := wbWorkbuddyAccount()
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, loc)

	// 从未执行：09:00 已过 → 到期。
	require.True(t, svc.taskDue(account, checkin, now))

	// 今晨 09:30 已执行成功：12:00 不到期（下个点位 21:00）。
	account.Extra = map[string]any{opsStateKey: map[string]any{"checkin": map[string]any{
		"last_run": now.Add(-150 * time.Minute).Format(time.RFC3339),
		"last_ok":  now.Add(-150 * time.Minute).Format(time.RFC3339),
	}}}
	require.False(t, svc.taskDue(account, checkin, now))

	// 21:30：21:00 点位晚于上次执行 → 到期。
	require.True(t, svc.taskDue(account, checkin, now.Add(9*time.Hour+30*time.Minute)))

	// 跨日：昨天 21:30 执行，今晨 09:30 → 09:00 点位已到 → 到期。
	yesterday := time.Date(2026, 10, 11, 21, 30, 0, 0, loc)
	account.Extra = map[string]any{opsStateKey: map[string]any{"checkin": map[string]any{
		"last_run": yesterday.Format(time.RFC3339),
	}}}
	require.True(t, svc.taskDue(account, checkin, time.Date(2026, 10, 12, 9, 30, 0, 0, loc)))

	// 退避 NextRun 未到：即使点位已过也不执行。
	account.Extra = map[string]any{opsStateKey: map[string]any{"checkin": map[string]any{
		"last_run": yesterday.Format(time.RFC3339),
		"next_run": now.Add(5 * time.Minute).Format(time.RFC3339),
	}}}
	require.False(t, svc.taskDue(account, checkin, now))
	require.True(t, svc.taskDue(account, checkin, now.Add(10*time.Minute)))
}

func TestWorkbuddyOpsService_TaskDueIntervalSemantics(t *testing.T) {
	repo := &wbRepoRecorder{}
	svc := NewWorkbuddyOpsService(repo, NewWorkbuddyOAuthService(repo, nil), nil, time.Minute)
	balance := svc.taskByID("balance")
	now := time.Now()

	account := wbWorkbuddyAccount()
	// LastRun 零值 → 首跑到期。
	require.True(t, svc.taskDue(account, balance, now))
	// 3h 前：6h 间隔未到。
	account.Extra = map[string]any{opsStateKey: map[string]any{"balance": map[string]any{
		"last_run": now.Add(-3 * time.Hour).Format(time.RFC3339),
	}}}
	require.False(t, svc.taskDue(account, balance, now))
	// 7h 前：到期。
	account.Extra = map[string]any{opsStateKey: map[string]any{"balance": map[string]any{
		"last_run": now.Add(-7 * time.Hour).Format(time.RFC3339),
	}}}
	require.True(t, svc.taskDue(account, balance, now))
}

func (s *WorkbuddyOpsService) taskByID(id string) PlatformOpsTask {
	for _, task := range s.Tasks() {
		if task.ID == id {
			return task
		}
	}
	panic("unknown task " + id)
}

func TestWorkbuddyOpsService_RunAccountTaskCheckinIdempotentSuccess(t *testing.T) {
	// 上游返回「今日已签到」（业务 code 非 0）→ checkin 幂等成功，状态记成功。
	wbStubClientFactory(t, func(r *http.Request) (*http.Response, error) {
		return wbJSONResponse(200, `{"code":10001,"msg":"今天已签到，请明天再来"}`), nil
	})
	repo := &wbRepoRecorder{account: wbWorkbuddyAccount()}
	oauth := NewWorkbuddyOAuthService(repo, nil)
	svc := NewWorkbuddyOpsService(repo, oauth, nil, time.Minute)

	err := svc.RunAccountTask(context.Background(), wbWorkbuddyAccount(), "checkin")
	require.NoError(t, err)

	// ops_state 记录成功（last_ok 落库，无 last_err）。
	saved := false
	for _, extra := range repo.updateExtras {
		for id, updates := range extra {
			if id != 42 {
				continue
			}
			if states, ok := updates[opsStateKey].(map[string]any); ok {
				if entry, ok := states["checkin"].(map[string]any); ok {
					require.Empty(t, entry["last_err"])
					require.NotEmpty(t, entry["last_ok"])
					saved = true
				}
			}
		}
	}
	require.True(t, saved, "checkin 成功状态应写入 ops_state")

	// 未知任务报错。
	require.Error(t, svc.RunAccountTask(context.Background(), wbWorkbuddyAccount(), "nope"))
}

func TestWorkbuddyOpsService_EnterpriseAccountsSkipGrowthTasks(t *testing.T) {
	called := false
	wbStubClientFactory(t, func(r *http.Request) (*http.Response, error) {
		called = true
		return wbJSONResponse(200, `{"code":0}`), nil
	})
	repo := &wbRepoRecorder{}
	svc := NewWorkbuddyOpsService(repo, NewWorkbuddyOAuthService(repo, nil), nil, time.Minute)

	enterprise := wbWorkbuddyAccount()
	enterprise.Credentials["enterprise_id"] = "ent-1"
	_, err := svc.runCheckin(context.Background(), enterprise)
	require.NoError(t, err)
	_, err = svc.runTravel(context.Background(), enterprise)
	require.NoError(t, err)
	_, err = svc.runActivity(context.Background(), enterprise)
	require.NoError(t, err)
	require.False(t, called, "企业账号 growth 族任务应在发请求前跳过")
}

func TestWorkbuddyOpsService_IncludeDisabledFilter(t *testing.T) {
	// 上游桩：全部请求成功（空 data）——保号任务（keepalive/checkin 等）可跑通。
	wbStubClientFactory(t, func(r *http.Request) (*http.Response, error) {
		return wbJSONResponse(200, `{"code":0,"msg":"","data":{}}`), nil
	})
	inactive := wbWorkbuddyAccount()
	inactive.Status = StatusError
	repo := &wbRepoRecorder{platformList: []Account{*inactive}}
	svc := NewWorkbuddyOpsService(repo, NewWorkbuddyOAuthService(repo, nil), nil, time.Minute)
	svc.runOnce()

	tasksRun := wbCollectOpsTaskIDs(t, repo)
	require.NotContains(t, tasksRun, "activity", "IncludeDisabled=false 的任务不跑 error 账号")
	require.NotContains(t, tasksRun, "growth")
	require.Contains(t, tasksRun, "keepalive", "IncludeDisabled=true 的保号任务照常执行")
}

// wbCollectOpsTaskIDs 汇总桩仓库收到的 ops_state 更新里出现过的任务 ID。
func wbCollectOpsTaskIDs(t *testing.T, repo *wbRepoRecorder) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, extra := range repo.updateExtras {
		for _, updates := range extra {
			states, ok := updates[opsStateKey].(map[string]any)
			if !ok {
				continue
			}
			for taskID := range states {
				out[taskID] = true
			}
		}
	}
	return out
}

// 领域常量兜底断言：ops 注册的平台名与 domain 常量一致（防止字符串漂移）。
func TestWorkbuddyOpsService_PlatformConstant(t *testing.T) {
	require.Equal(t, domain.PlatformWorkbuddy, PlatformWorkbuddy)
}
