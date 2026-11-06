package service

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/trae"
)

// TraeOpsService 是 trae 平台的运营任务调度器（克隆 CNProviderBalanceCheckService
// 的 Start/Stop/runOnce + ticker 骨架），任务注册表：
//   - checkin         每日 09 点签到（账号时区）；
//   - credits_monitor 每日 10 点积分到期监控：临期 14 天内写
//     extra["trae_quota"]["expiring_soon"]=true 并告警；
//   - usage_refresh   每 6 小时刷新积分快照到 extra["trae_quota"]。
//
// credits_monitor / usage_refresh 与手动查询共用 storeTraeQuotaSnapshot：快照落库后
// 顺带做 trae_no_credit 恢复判定（积分恢复即解冻），三条路径语义只有一份。
// 三个任务全部 IncludeDisabled：积分耗尽罚停后账号不再是 active，若跳过它就再也
// 没有恢复入口（签到 / 积分刷新正是解冻通道，与 qoder/workbuddy 同款口径）。
//
// 手动触发与状态记录复用 platform_ops.go 的共享基建（账号锁 / ops_state / 退避）；
// 构造时经 RegisterPlatformOpsRunner 自注册，通用 /admin/platform-ops 端点按
// 平台分派到本服务。
type TraeOpsService struct {
	accountRepo  AccountRepository
	oauthService *TraeOAuthService
	// settings 平台级任务调度配置存取（nil = 未配置，全部沿用默认）。
	settings platformOpsSettingStore
	interval time.Duration
	tasks    []PlatformOpsTask

	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

// traeOpsScanInterval 是调度扫描周期（任务自身的触发节奏由 Hours / Interval 决定）。
const traeOpsScanInterval = 10 * time.Minute

// NewTraeOpsService 构造 trae 运营任务服务。interval <= 0 时 Start() 直接返回
// （不启动），便于测试与按需关闭。
func NewTraeOpsService(accountRepo AccountRepository, oauthService *TraeOAuthService, settings SettingRepository, interval time.Duration) *TraeOpsService {
	s := &TraeOpsService{
		accountRepo:  accountRepo,
		oauthService: oauthService,
		settings:     settings,
		interval:     interval,
		stopCh:       make(chan struct{}),
	}
	s.tasks = []PlatformOpsTask{
		{ID: "checkin", Platform: PlatformTrae, Name: "每日签到", Hours: []int{9}, IncludeDisabled: true, RunOne: s.runCheckin},
		{ID: "credits_monitor", Platform: PlatformTrae, Name: "积分到期监控", Hours: []int{10}, IncludeDisabled: true, RunOne: s.runCreditsMonitor},
		{ID: "usage_refresh", Platform: PlatformTrae, Name: "积分额度刷新", Interval: 6 * time.Hour, IncludeDisabled: true, RunOne: s.runUsageRefresh},
	}
	RegisterPlatformOpsRunner(s)
	return s
}

// Platform 实现 PlatformOpsRunner。
func (s *TraeOpsService) Platform() string { return PlatformTrae }

// Tasks 实现 PlatformOpsRunner。
func (s *TraeOpsService) Tasks() []PlatformOpsTask { return s.tasks }

// RunAccountTask 对已加载的账号执行指定任务（含账号锁与 ops_state 记录）。
func (s *TraeOpsService) RunAccountTask(ctx context.Context, account *Account, taskID string) error {
	for _, task := range s.tasks {
		if task.ID == taskID {
			return RunPlatformOpsTask(ctx, s.accountRepo, task, account)
		}
	}
	return fmt.Errorf("unknown trae ops task %q", taskID)
}

// Start 启动周期调度；依赖缺失或 interval <= 0 时不启动。
func (s *TraeOpsService) Start() {
	if s == nil || s.accountRepo == nil || s.interval <= 0 {
		return
	}
	log.Printf("[TraeOps] started (scan_interval=%s)", s.interval)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.runOnce()
			case <-s.stopCh:
				return
			}
		}
	}()
}

// Stop 停止调度（可安全多次调用）。
func (s *TraeOpsService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	s.wg.Wait()
}

// runOnce 扫描全部 trae 账号并执行到期任务。
func (s *TraeOpsService) runOnce() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	accounts, err := s.accountRepo.ListByPlatform(ctx, PlatformTrae)
	if err != nil {
		log.Printf("[TraeOps] list trae accounts failed: %v", err)
		return
	}
	now := time.Now()
	opsConfigs := LoadPlatformOpsTaskConfigs(ctx, s.settings, PlatformTrae)
	for i := range accounts {
		account := &accounts[i]
		loc := platformOpsTimezone(account)
		for _, task := range s.tasks {
			// 默认只跑激活账号；trae 的三个任务均 IncludeDisabled（见任务注册表
			// 注释：它们是 trae_no_credit 罚停的唯一恢复入口）。
			if !task.IncludeDisabled && !account.IsActive() {
				continue
			}
			// 平台级任务配置：禁用仅停自动调度（手动执行不受影响）；时刻/间隔覆盖默认。
			if !PlatformOpsTaskAutoEnabled(opsConfigs, task.ID) {
				continue
			}
			task = ApplyPlatformOpsTaskConfig(task, opsConfigs)
			if !traeOpsTaskDue(task, LoadOpsState(account, task.ID), now, loc) {
				continue
			}
			if runErr := RunPlatformOpsTask(ctx, s.accountRepo, task, account); runErr != nil {
				log.Printf("[TraeOps] account %d task %s failed: %v", account.ID, task.ID, runErr)
			}
		}
	}
}

// traeOpsTaskDue 判定任务是否到期：
//   - Hours 任务：当天触发点已到且上次成功早于该触发点；
//   - Interval 任务：距上次执行满一个周期；
//   - 退避顺延（entry.NextRun）优先于以上两条。
func traeOpsTaskDue(task PlatformOpsTask, entry OpsStateEntry, now time.Time, loc *time.Location) bool {
	if !entry.NextRun.IsZero() && now.Before(entry.NextRun) {
		return false
	}
	if len(task.Hours) > 0 {
		local := now.In(loc)
		var lastScheduled time.Time
		for _, hour := range task.Hours {
			t := time.Date(local.Year(), local.Month(), local.Day(), hour, 0, 0, 0, loc)
			if !t.After(now) && t.After(lastScheduled) {
				lastScheduled = t
			}
		}
		if lastScheduled.IsZero() {
			return false
		}
		return entry.LastOK.Before(lastScheduled)
	}
	if task.Interval > 0 {
		if entry.LastRun.IsZero() {
			return true
		}
		return now.Sub(entry.LastRun) >= task.Interval
	}
	return false
}

// runCheckin 每日签到：预检（已签直接成功）→ claim。业务性失败（重复签到 /
// 套餐限制）按成功记账，其余报错交退避。
func (s *TraeOpsService) runCheckin(ctx context.Context, account *Account) (string, error) {
	creds, client, err := s.traeClient(account)
	if err != nil {
		return "", err
	}
	variant := creds.VariantOf()
	device := trae.DeviceIdentity{DeviceID: creds.DeviceID, MarketUserID: creds.UID}

	if status, statErr := trae.CheckinStatus(ctx, client, variant, creds.JWT, device); statErr == nil &&
		status.OK() && status.CheckedIn {
		return "今日已签到", nil
	}
	claim, err := trae.CheckinClaim(ctx, client, variant, creds.JWT, device)
	if err != nil {
		return "", err
	}
	if claim.OK() || traeCheckinBusinessSuccess(claim) {
		if msg := strings.TrimSpace(claim.Message); msg != "" {
			return msg, nil
		}
		return "签到成功", nil
	}
	return "", fmt.Errorf("签到失败: HTTP %d code=%d has_code=%v: %s", claim.HTTPStatus, claim.Code, claim.HasCode, claim.Message)
}

// traeCheckinBusinessSuccess 判定签到 claim 的业务性「等效成功」（重复签到 /
// 套餐限额等不会因重试改变的结局，不计为故障）。
func traeCheckinBusinessSuccess(claim *trae.ClaimResult) bool {
	if claim == nil || claim.HTTPStatus != 200 {
		return false
	}
	if claim.Code == 1005 {
		return true
	}
	message := strings.ToLower(claim.Message)
	return strings.Contains(message, "already") || strings.Contains(claim.Message, "已签") || strings.Contains(message, "repeat")
}

// runUsageRefresh 刷新积分快照到 extra["trae_quota"]。
func (s *TraeOpsService) runUsageRefresh(ctx context.Context, account *Account) (string, error) {
	_, err := storeTraeQuotaSnapshot(ctx, s.accountRepo, account)
	return "", err
}

// runCreditsMonitor 积分到期监控：刷新快照并对临期（14 天内到期）的账号打
// expiring_soon 标记 + 告警日志。顺带完成 trae_no_credit 的恢复判定——走
// storeTraeQuotaSnapshot 而不是自己再查一遍上游，避免两条路径的恢复语义漂移。
func (s *TraeOpsService) runCreditsMonitor(ctx context.Context, account *Account) (string, error) {
	entitlements, err := storeTraeQuotaSnapshot(ctx, s.accountRepo, account)
	if err != nil {
		return "", err
	}
	expiring := traeEntitlementsExpiring(entitlements)
	if err := writeTraeQuotaExtra(ctx, s.accountRepo, account, entitlements, map[string]any{
		"expiring_soon": expiring,
	}); err != nil {
		return "", err
	}
	if expiring {
		log.Printf("[TraeOps] account %d 积分临期告警：%d 天内到期（expiring_soon=true）", account.ID, traeExpiringSoonDays)
		return fmt.Sprintf("积分临期：%d 天内到期", traeExpiringSoonDays), nil
	}
	return "", nil
}

// traeEntitlementsExpiring 是否存在临期且未过期的积分包（窗口见
// pkg/trae 的 expiringSoonWindow，当前 14 天）。
func traeEntitlementsExpiring(entitlements *trae.Entitlements) bool {
	if entitlements == nil {
		return false
	}
	for _, pack := range entitlements.Packages {
		if pack.ExpiringSoon && !pack.Expired {
			return true
		}
	}
	return false
}

// traeExpiringSoonDays 「临期」窗口的天数（仅用于文案；判据在 pkg/trae）。
const traeExpiringSoonDays = 14

// traeClient 解析账号凭据并构造带账号代理的 HTTP 客户端。
func (s *TraeOpsService) traeClient(account *Account) (*trae.Credentials, *http.Client, error) {
	creds, err := traeAccountCredentials(account)
	if err != nil {
		return nil, nil, err
	}
	client, err := traeHTTPClient(traeAccountProxyURL(account))
	if err != nil {
		return nil, nil, err
	}
	return creds, client, nil
}
