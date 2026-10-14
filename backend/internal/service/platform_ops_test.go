//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// nextDailyFire 移植自 workbuddy2api-panel 的 nextFire：返回 now 之后最近的整点时刻。
func TestNextDailyFire(t *testing.T) {
	loc := opsDefaultTimezone
	// 08:30 → 09:00 当天
	now := time.Date(2026, 10, 12, 8, 30, 0, 0, loc)
	got := nextDailyFire(now, []int{9, 21}, loc)
	require.Equal(t, time.Date(2026, 10, 12, 9, 0, 0, 0, loc), got)

	// 09:00 整（该时刻本身不严格晚于 now，不触发）→ 当天 21:00
	now = time.Date(2026, 10, 12, 9, 0, 0, 0, loc)
	got = nextDailyFire(now, []int{9, 21}, loc)
	require.Equal(t, time.Date(2026, 10, 12, 21, 0, 0, 0, loc), got)

	// 10:00 → 21:00 当天
	now = time.Date(2026, 10, 12, 10, 0, 0, 0, loc)
	got = nextDailyFire(now, []int{9, 21}, loc)
	require.Equal(t, time.Date(2026, 10, 12, 21, 0, 0, 0, loc), got)

	// 23:00 → 次日 09:00
	now = time.Date(2026, 10, 12, 23, 0, 0, 0, loc)
	got = nextDailyFire(now, []int{9, 21}, loc)
	require.Equal(t, time.Date(2026, 10, 13, 9, 0, 0, 0, loc), got)

	// 空 hours → 零值
	require.True(t, nextDailyFire(now, nil, loc).IsZero())

	// 跨时区输入：UTC 时刻按 loc 计算
	nowUTC := time.Date(2026, 10, 12, 1, 30, 0, 0, time.UTC) // 北京时间 09:30
	got = nextDailyFire(nowUTC, []int{21}, loc)
	require.Equal(t, time.Date(2026, 10, 12, 21, 0, 0, 0, loc), got)
}

func TestPlatformOpsTaskManualOnly(t *testing.T) {
	require.True(t, PlatformOpsTask{ID: "x"}.ManualOnly())
	require.False(t, PlatformOpsTask{ID: "x", Hours: []int{9}}.ManualOnly())
	require.False(t, PlatformOpsTask{ID: "x", Interval: time.Hour}.ManualOnly())
}

// OpsStateEntry 经 extra（JSONB 动态结构）往返不丢字段。
func TestOpsStateRoundTrip(t *testing.T) {
	account := &Account{ID: 1, Extra: map[string]any{}}
	entry := OpsStateEntry{
		LastRun: time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC),
		LastOK:  time.Date(2026, 10, 12, 9, 0, 1, 0, time.UTC),
		LastErr: "",
		Streak:  2,
		Retries: 1,
	}
	blob := []byte(`{"last_run":"2026-10-12T09:00:00Z","last_ok":"2026-10-12T09:00:01Z","streak":2,"retries":1}`)
	var raw map[string]any
	require.NoError(t, json.Unmarshal(blob, &raw))
	account.Extra[opsStateKey] = map[string]any{"checkin": raw}
	loaded := LoadOpsState(account, "checkin")
	require.Equal(t, entry, loaded)

	// 未登记的任务返回零值
	require.Equal(t, OpsStateEntry{}, LoadOpsState(account, "other"))
	require.Equal(t, OpsStateEntry{}, LoadOpsState(nil, "checkin"))
}

// RunPlatformOpsTask 成功路径：状态落库、清零退避。
func TestRunPlatformOpsTaskSuccess(t *testing.T) {
	repo := &opsStateStubRepo{}
	account := &Account{ID: 7, Platform: PlatformWorkbuddy, Extra: map[string]any{}}
	ran := false
	task := PlatformOpsTask{ID: "checkin", Platform: PlatformWorkbuddy, Name: "每日签到",
		RunOne: func(ctx context.Context, a *Account) (string, error) { ran = true; return "签到成功 +100", nil }}
	require.NoError(t, RunPlatformOpsTask(context.Background(), repo, task, account))
	require.True(t, ran)
	entry := LoadOpsState(account, "checkin")
	require.False(t, entry.LastRun.IsZero())
	require.False(t, entry.LastOK.IsZero())
	require.Empty(t, entry.LastErr)
	require.Equal(t, "签到成功 +100", entry.Note)
	require.Zero(t, entry.Streak)
	require.True(t, entry.NextRun.IsZero())
	require.Equal(t, int64(7), repo.lastID)
}

// 成功路径的 note 随后续失败清空。
func TestRunPlatformOpsTaskNoteClearedOnFailure(t *testing.T) {
	repo := &opsStateStubRepo{}
	account := &Account{ID: 9, Platform: PlatformQoder, Extra: map[string]any{}}
	ok := PlatformOpsTask{ID: "checkin", Platform: PlatformQoder,
		RunOne: func(ctx context.Context, a *Account) (string, error) { return "已领", nil }}
	require.NoError(t, RunPlatformOpsTask(context.Background(), repo, ok, account))
	require.Equal(t, "已领", LoadOpsState(account, "checkin").Note)

	bad := PlatformOpsTask{ID: "checkin", Platform: PlatformQoder,
		RunOne: func(ctx context.Context, a *Account) (string, error) { return "残留", errors.New("boom") }}
	require.Error(t, RunPlatformOpsTask(context.Background(), repo, bad, account))
	require.Empty(t, LoadOpsState(account, "checkin").Note)
}

// note 经 extra JSON 往返持久化并可还原。
func TestOpsStateNoteJSONRoundTrip(t *testing.T) {
	account := &Account{ID: 10, Extra: map[string]any{}}
	entry := OpsStateEntry{Note: "活动领取成功 +100 Credits（每日积分）"}
	blob, err := json.Marshal(entry)
	require.NoError(t, err)
	var raw map[string]any
	require.NoError(t, json.Unmarshal(blob, &raw))
	account.Extra[opsStateKey] = map[string]any{"checkin": raw}
	require.Equal(t, entry, LoadOpsState(account, "checkin"))
}

// 失败路径：记录错误、递增 Streak/Retries、NextRun 落在第一档退避。
func TestRunPlatformOpsTaskFailureBackoff(t *testing.T) {
	repo := &opsStateStubRepo{}
	account := &Account{ID: 8, Platform: PlatformQoder, Extra: map[string]any{}}
	boom := errors.New("upstream 500")
	task := PlatformOpsTask{ID: "keepalive", Platform: PlatformQoder,
		RunOne: func(ctx context.Context, a *Account) (string, error) { return "", boom }}
	err := RunPlatformOpsTask(context.Background(), repo, task, account)
	require.Same(t, boom, err)
	entry := LoadOpsState(account, "keepalive")
	require.Equal(t, "upstream 500", entry.LastErr)
	require.Equal(t, 1, entry.Streak)
	require.Equal(t, 1, entry.Retries)
	require.WithinDuration(t, time.Now().Add(opsStateBackoff[0]), entry.NextRun, 2*time.Second)
}

// ops_state 持久化 stub：仅记录最后写入。
type opsStateStubRepo struct {
	lastID    int64
	lastExtra map[string]any
}

func (r *opsStateStubRepo) UpdateExtra(ctx context.Context, id int64, updates map[string]any) error {
	r.lastID = id
	r.lastExtra = updates
	return nil
}

// opsSettingStoreStub 平台级配置的 settings 存取桩。
type opsSettingStoreStub struct {
	value string
	sets  []string
}

func (s *opsSettingStoreStub) GetValue(_ context.Context, _ string) (string, error) {
	return s.value, nil
}

func (s *opsSettingStoreStub) Set(_ context.Context, _, value string) error {
	s.sets = append(s.sets, value)
	s.value = value
	return nil
}

func TestPlatformOpsTaskConfigRoundTripAndApply(t *testing.T) {
	t.Parallel()

	disabled := false
	sixHours := 6
	store := &opsSettingStoreStub{value: `{"workbuddy":{"checkin":{"enabled":false,"hours":[8,20]}}}`}

	// 读取：JSON 键下按平台取配置表。
	configs := LoadPlatformOpsTaskConfigs(context.Background(), store, PlatformWorkbuddy)
	cfg, ok := configs["checkin"]
	require.True(t, ok)
	require.NotNil(t, cfg.Enabled)
	require.False(t, *cfg.Enabled)
	require.Equal(t, []int{8, 20}, cfg.Hours)

	// 未配置平台返回空表；坏 JSON / 空 store 均安全回落空表。
	require.Empty(t, LoadPlatformOpsTaskConfigs(context.Background(), store, PlatformQoder))
	require.Empty(t, LoadPlatformOpsTaskConfigs(context.Background(), &opsSettingStoreStub{value: "not-json"}, PlatformWorkbuddy))
	require.Empty(t, LoadPlatformOpsTaskConfigs(context.Background(), nil, PlatformWorkbuddy))

	// 自动调度开关：显式 false 停用，缺省启用。
	require.False(t, PlatformOpsTaskAutoEnabled(configs, "checkin"))
	require.True(t, PlatformOpsTaskAutoEnabled(configs, "balance"))
	require.True(t, PlatformOpsTaskAutoEnabled(nil, "checkin"))

	// 应用覆盖：hours 覆盖并清 interval；未配置的任务原样返回。
	base := PlatformOpsTask{ID: "checkin", Hours: []int{9, 21}}
	applied := ApplyPlatformOpsTaskConfig(base, configs)
	require.Equal(t, []int{8, 20}, applied.Hours)
	require.Zero(t, applied.Interval)
	require.Equal(t, base, ApplyPlatformOpsTaskConfig(base, nil))

	// interval 覆盖仅对无 hours 任务生效。
	intervalTask := PlatformOpsTask{ID: "balance", Interval: 6 * time.Hour}
	override12 := 12
	intervalConfigs := map[string]OpsTaskConfig{"balance": {IntervalHours: &override12}}
	require.Equal(t, 12*time.Hour, ApplyPlatformOpsTaskConfig(intervalTask, intervalConfigs).Interval)

	// 平台级保存：读-改-写整键，其它平台子表保留。
	err := SavePlatformOpsTaskConfigs(context.Background(), store, PlatformQoder,
		map[string]OpsTaskConfig{"balance": {Enabled: &disabled, IntervalHours: &sixHours}})
	require.NoError(t, err)
	require.Len(t, store.sets, 1)
	var saved map[string]map[string]OpsTaskConfig
	require.NoError(t, json.Unmarshal([]byte(store.sets[0]), &saved))
	require.Contains(t, saved, "workbuddy")
	require.Contains(t, saved, "qoder")
	require.NotNil(t, saved["qoder"]["balance"].Enabled)
	require.False(t, *saved["qoder"]["balance"].Enabled)
	require.Equal(t, 6, *saved["qoder"]["balance"].IntervalHours)
}
