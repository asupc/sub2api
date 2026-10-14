package service

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"sort"
	"strings"
	"sync"
	"time"
)

// 二开新增：源码集成平台（WorkBuddy / Qoder / Trae）运营任务共享基建。
//
// 三个外部项目（workbuddy2api-panel / qoder2api-hub / trae-workbuddy-switch）都带有
// 每日签到、token 保活、福利领取等定时运营功能；本文件提供它们共用的任务描述、
// 每日定点触发计算与账号级状态存取，各平台在自己的 <platform>_ops_service.go 中
// 用 PlatformOpsTask 注册表驱动具体动作（克隆 CNProviderBalanceCheckService 的
// Start/Stop/runOnce + ticker 骨架）。
//
// 设计约定：
//   - 任务状态存 account.extra["ops_state"][taskID]（OpsStateEntry），与凭据分离；
//   - 同一账号的任务执行互斥（per-account mutex），防止手动触发与定时触发并发；
//   - 失败退避：当日最多重试 2 次（间隔 10min → 30min），跨日重置；
//   - 时区：账号 extra["ops_timezone"]（IANA 名）可覆盖，默认 Asia/Shanghai。

// PlatformOpsTask 描述一个平台运营任务。
type PlatformOpsTask struct {
	// ID 是任务标识（如 "checkin"），同时是 ops_state 的键，平台内唯一。
	ID string
	// Platform 是所属平台（domain 平台常量）。
	Platform string
	// Name 是任务的中文展示名（如「每日签到」）。
	Name string
	// Hours 是每日定点触发的小时列表（0-23，账号时区）；空表示不定期。
	Hours []int
	// Interval 是固定间隔触发周期（与 Hours 互斥，优先 Hours）；<=0 表示仅按 Hours。
	Interval time.Duration
	// RunOne 对单个账号执行任务；返回 error 记入 ops_state（LastErr / Streak），
	// 返回的 note 为本次执行的上游结论文本（成功时写入 ops_state 供前端展示，
	// 如「活动领取成功 +100 Credits（…）」「今日已签到」；失败时忽略）。
	RunOne func(ctx context.Context, account *Account) (string, error)
	// IncludeDisabled 表示是否对非 active 账号执行（保号类任务为 true）。
	IncludeDisabled bool
}

// ManualOnly 报告任务是否仅手动触发（无 Hours 且无 Interval）。
func (t PlatformOpsTask) ManualOnly() bool {
	return len(t.Hours) == 0 && t.Interval <= 0
}

// opsDefaultTimezone 是运营任务的默认时区。
var opsDefaultTimezone = time.FixedZone("Asia/Shanghai", 8*3600)

// platformOpsTimezone 返回账号的运营任务时区：
// extra["ops_timezone"]（IANA 名）合法时覆盖，否则默认 Asia/Shanghai（固定 +8，无 DST）。
func platformOpsTimezone(account *Account) *time.Location {
	if account != nil && account.Extra != nil {
		if name, ok := account.Extra["ops_timezone"].(string); ok {
			name = strings.TrimSpace(name)
			if loc, err := time.LoadLocation(name); err == nil {
				return loc
			}
			log.Printf("[PlatformOps] invalid ops_timezone %q on account %d, fallback to Asia/Shanghai", name, account.ID)
		}
	}
	return opsDefaultTimezone
}

// nextDailyFire 返回 now 之后最近一个 hours 整点触发时刻（loc 时区）。
// 移植自 workbuddy2api-panel internal/scheduler nextFire；hours 为空返回零值。
func nextDailyFire(now time.Time, hours []int, loc *time.Location) time.Time {
	now = now.In(loc)
	var earliest time.Time
	for _, h := range hours {
		t := time.Date(now.Year(), now.Month(), now.Day(), h, 0, 0, 0, loc)
		if !t.After(now) {
			t = t.Add(24 * time.Hour)
		}
		if earliest.IsZero() || t.Before(earliest) {
			earliest = t
		}
	}
	return earliest
}

// opsStateBackoff 是失败退避间隔序列：当日第 1 次重试等 10 分钟，第 2 次等 30 分钟，
// 超过当日重试上限（2 次）则顺延到下一触发点。跨日（LastOK/LastRun 不同日）自动重置。
var opsStateBackoff = []time.Duration{10 * time.Minute, 30 * time.Minute}

// opsStateMaxRetries 是单日最大重试次数。
const opsStateMaxRetries = 2

// OpsStateEntry 是运营任务的单账号状态，存于 account.extra["ops_state"][taskID]。
type OpsStateEntry struct {
	LastRun time.Time `json:"last_run,omitempty"`
	LastOK  time.Time `json:"last_ok,omitempty"`
	LastErr string    `json:"last_err,omitempty"`
	// Streak 是连续失败次数（观测用，>0 表示近期不健康）。
	Streak int `json:"streak,omitempty"`
	// Retries 是当日已重试次数。
	Retries int `json:"retries,omitempty"`
	// NextRun 是下次应执行时刻（含退避顺延；零值表示按任务调度规则计算）。
	NextRun time.Time `json:"next_run,omitempty"`
	// Note 是最近一次成功执行的上游结论文本（到账/幂等跳过/未开放等原因），
	// 供手动触发响应与任务面板展示；失败时清空。
	Note string `json:"note,omitempty"`
}

// opsStateKey 是 extra 中运营状态的顶层键。
const opsStateKey = "ops_state"

// LoadOpsState 读取账号某任务的状态（无记录返回零值条目）。
func LoadOpsState(account *Account, taskID string) OpsStateEntry {
	if account == nil || account.Extra == nil {
		return OpsStateEntry{}
	}
	states, ok := account.Extra[opsStateKey].(map[string]any)
	if !ok {
		return OpsStateEntry{}
	}
	raw, ok := states[taskID].(map[string]any)
	if !ok {
		return OpsStateEntry{}
	}
	// extra 来自 JSONB 反序列化，结构体字段需经 JSON 往返还原。
	blob, err := json.Marshal(raw)
	if err != nil {
		return OpsStateEntry{}
	}
	var entry OpsStateEntry
	if err := json.Unmarshal(blob, &entry); err != nil {
		return OpsStateEntry{}
	}
	return entry
}

// opsStateMutexes 以账号 ID 为粒度串行化运营任务执行（手动触发与定时触发互斥）。
var opsStateMutexes sync.Map // map[int64]*sync.Mutex

func lockOpsAccount(accountID int64) func() {
	v, _ := opsStateMutexes.LoadOrStore(accountID, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// opsStateRepo 是运营状态持久化的最小接口（*accountRepository 天然满足，测试可 stub）。
type opsStateRepo interface {
	UpdateExtra(ctx context.Context, id int64, updates map[string]any) error
}

// SaveOpsState 把任务状态写回账号 extra 并持久化（JSONB 浅合并，嵌套 map 整体覆盖）。
// 调用方须持有该账号的任务锁（lockOpsAccount）。
func SaveOpsState(ctx context.Context, repo opsStateRepo, account *Account, taskID string, entry OpsStateEntry) error {
	if account == nil || repo == nil {
		return nil
	}
	if account.Extra == nil {
		account.Extra = map[string]any{}
	}
	states, ok := account.Extra[opsStateKey].(map[string]any)
	if !ok {
		states = map[string]any{}
		account.Extra[opsStateKey] = states
	}
	// OpsStateEntry 经 JSON 往返转为 map[string]any，保持 extra 的动态结构。
	blob, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	var raw map[string]any
	if err := json.Unmarshal(blob, &raw); err != nil {
		return err
	}
	states[taskID] = raw
	return repo.UpdateExtra(ctx, account.ID, map[string]any{opsStateKey: states})
}

// RunPlatformOpsTask 对单个账号执行一次任务并记录状态（手动触发入口复用同一逻辑）。
// 返回执行错误（已写入 ops_state；err == nil 表示成功）。
func RunPlatformOpsTask(ctx context.Context, repo opsStateRepo, task PlatformOpsTask, account *Account) error {
	unlock := lockOpsAccount(account.ID)
	defer unlock()

	now := time.Now()
	entry := LoadOpsState(account, task.ID)
	note, err := task.RunOne(ctx, account)

	entry.LastRun = now
	if err != nil {
		entry.LastErr = err.Error()
		entry.Note = ""
		entry.Streak++
		// 当日重试退避；跨日（上次成功/执行不在今天）重置计数。
		if !sameLocalDay(entry.LastOK, now, platformOpsTimezone(account)) || entry.Retries == 0 {
			entry.Retries = 1
		} else {
			entry.Retries++
		}
		if entry.Retries <= opsStateMaxRetries && entry.Retries <= len(opsStateBackoff) {
			entry.NextRun = now.Add(opsStateBackoff[entry.Retries-1])
		} else {
			entry.NextRun = time.Time{} // 交还定期调度规则
		}
	} else {
		entry.LastOK = now
		entry.LastErr = ""
		entry.Note = strings.TrimSpace(note)
		entry.Streak = 0
		entry.Retries = 0
		entry.NextRun = time.Time{}
	}
	if saveErr := SaveOpsState(ctx, repo, account, task.ID, entry); saveErr != nil {
		log.Printf("[PlatformOps] save state failed: account=%d task=%s err=%v", account.ID, task.ID, saveErr)
	}
	return err
}

// sameLocalDay 报告两个时刻在 loc 时区是否同一天。
func sameLocalDay(a, b time.Time, loc *time.Location) bool {
	if a.IsZero() {
		return false
	}
	ay, am, ad := a.In(loc).Date()
	by, bm, bd := b.In(loc).Date()
	return ay == by && am == bm && ad == bd
}

// PlatformOpsRunner 由各平台的 OpsService 实现（workbuddy / qoder / trae），
// 构造时调用 RegisterPlatformOpsRunner 自注册，通用 HTTP 端点据此按账号平台分派。
type PlatformOpsRunner interface {
	// Platform 返回所属平台（domain 平台常量）。
	Platform() string
	// Tasks 返回该平台的任务注册表。
	Tasks() []PlatformOpsTask
	// RunAccountTask 对已加载的账号执行指定任务（内部走 RunPlatformOpsTask，
	// 含账号锁与 ops_state 记录）；taskID 不存在时返回错误。
	RunAccountTask(ctx context.Context, account *Account, taskID string) error
}

// platformOpsRunners 是平台运营任务运行器注册表（进程级单例）。
var platformOpsRunners sync.Map // map[string]PlatformOpsRunner

// RegisterPlatformOpsRunner 注册（覆盖式）某平台的运营任务运行器；
// 各平台 OpsService 构造时调用。幂等，便于测试反复构造。
func RegisterPlatformOpsRunner(r PlatformOpsRunner) {
	if r == nil {
		return
	}
	platformOpsRunners.Store(r.Platform(), r)
}

// LookupPlatformOpsRunner 按平台查找运营任务运行器。
func LookupPlatformOpsRunner(platform string) (PlatformOpsRunner, bool) {
	v, ok := platformOpsRunners.Load(platform)
	if !ok {
		return nil, false
	}
	r, ok := v.(PlatformOpsRunner)
	return r, ok
}

// AllPlatformOpsRunners 返回全部已注册运行器（按平台名排序，展示稳定）。
func AllPlatformOpsRunners() []PlatformOpsRunner {
	out := make([]PlatformOpsRunner, 0, 4)
	platformOpsRunners.Range(func(_, v any) bool {
		if r, ok := v.(PlatformOpsRunner); ok {
			out = append(out, r)
		}
		return true
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Platform() < out[j].Platform() })
	return out
}

// PlatformOpsTaskInfo 是任务注册表的对外展示形状（GET ops/tasks 端点用）。
type PlatformOpsTaskInfo struct {
	ID       string `json:"id"`
	Platform string `json:"platform"`
	Name     string `json:"name"`
	Hours    []int  `json:"hours,omitempty"`
	// IntervalSeconds 仅固定间隔任务有值。
	IntervalSeconds int  `json:"interval_seconds,omitempty"`
	ManualOnly      bool `json:"manual_only,omitempty"`
	IncludeDisabled bool `json:"include_disabled,omitempty"`
}

// Describe 汇总任务元信息（不含账号状态）。
func (t PlatformOpsTask) Describe() PlatformOpsTaskInfo {
	info := PlatformOpsTaskInfo{
		ID:              t.ID,
		Platform:        t.Platform,
		Name:            t.Name,
		IncludeDisabled: t.IncludeDisabled,
		ManualOnly:      t.ManualOnly(),
	}
	if len(t.Hours) > 0 {
		info.Hours = append([]int(nil), t.Hours...)
	}
	if t.Interval > 0 {
		info.IntervalSeconds = int(t.Interval / time.Second)
	}
	return info
}

// ---------------------------------------------------------------------------
// 平台级任务配置（settings 表单键 platform_ops_config，按平台统一覆盖默认调度）。
// ---------------------------------------------------------------------------

// SettingKeyPlatformOpsConfig 平台级运营任务调度配置在 settings 表的键。
// 值为 JSON：{"workbuddy":{"checkin":{"enabled":true,"hours":[8,20]}},"qoder":{...}}。
const SettingKeyPlatformOpsConfig = "platform_ops_config"

// OpsTaskConfig 单个任务的平台级配置；字段缺省（nil / 空）表示沿用注册表默认，
// 显式赋值覆盖（对该平台全部账号统一生效）。Enabled 只控制自动调度（禁用后
// 手动执行端点仍可用）。
type OpsTaskConfig struct {
	// Enabled 缺省（nil）= 启用自动调度。
	Enabled *bool `json:"enabled,omitempty"`
	// Hours nil = 沿用默认；非 nil（含空切片）= 覆盖每日定点小时（空 = 不定期）。
	Hours []int `json:"hours,omitempty"`
	// IntervalHours nil = 沿用默认；非 nil = 覆盖固定间隔（小时）。Hours 覆盖优先。
	IntervalHours *int `json:"interval_hours,omitempty"`
}

// platformOpsSettingStore 平台配置持久化最小接口（SettingRepository 天然满足，
// 测试可 stub；nil 视为未配置 = 全部沿用默认）。
type platformOpsSettingStore interface {
	GetValue(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key, value string) error
}

// loadAllPlatformOpsTaskConfigs 读取整键配置（空/坏 JSON 返回空表，不报错——
// 调度循环宁可按默认跑也不因配置损坏停摆）。
func loadAllPlatformOpsTaskConfigs(ctx context.Context, store platformOpsSettingStore) map[string]map[string]OpsTaskConfig {
	out := map[string]map[string]OpsTaskConfig{}
	if store == nil {
		return out
	}
	raw, err := store.GetValue(ctx, SettingKeyPlatformOpsConfig)
	if err != nil || strings.TrimSpace(raw) == "" {
		return out
	}
	_ = json.Unmarshal([]byte(raw), &out)
	return out
}

// LoadPlatformOpsTaskConfigs 读取某平台的任务配置表（未配置返回空表）。
func LoadPlatformOpsTaskConfigs(ctx context.Context, store platformOpsSettingStore, platform string) map[string]OpsTaskConfig {
	return loadAllPlatformOpsTaskConfigs(ctx, store)[platform]
}

// PlatformOpsTaskAutoEnabled 报告任务是否参与自动调度（配置显式 enabled=false 为假）。
func PlatformOpsTaskAutoEnabled(configs map[string]OpsTaskConfig, taskID string) bool {
	cfg, ok := configs[taskID]
	if !ok {
		return true
	}
	return cfg.Enabled == nil || *cfg.Enabled
}

// ApplyPlatformOpsTaskConfig 返回应用平台配置后的任务副本（不改注册表原任务）：
// Hours 非 nil 覆盖定点（清空 Interval，二者互斥）；IntervalHours 覆盖间隔。
func ApplyPlatformOpsTaskConfig(task PlatformOpsTask, configs map[string]OpsTaskConfig) PlatformOpsTask {
	cfg, ok := configs[task.ID]
	if !ok {
		return task
	}
	if cfg.Hours != nil {
		task.Hours = append([]int(nil), cfg.Hours...)
		task.Interval = 0
	}
	if cfg.IntervalHours != nil && *cfg.IntervalHours > 0 && len(task.Hours) == 0 {
		task.Interval = time.Duration(*cfg.IntervalHours) * time.Hour
	}
	return task
}

// SavePlatformOpsTaskConfigs 读-改-写整键保存某平台的任务配置表。
func SavePlatformOpsTaskConfigs(ctx context.Context, store platformOpsSettingStore, platform string, configs map[string]OpsTaskConfig) error {
	if store == nil {
		return errors.New("platform ops setting store is not configured")
	}
	all := loadAllPlatformOpsTaskConfigs(ctx, store)
	all[platform] = configs
	blob, err := json.Marshal(all)
	if err != nil {
		return err
	}
	return store.Set(ctx, SettingKeyPlatformOpsConfig, string(blob))
}
