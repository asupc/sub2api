package service

// qoder_ops_service.go 二开新增：Qoder 平台运营任务服务。
//
// 克隆 CNProviderBalanceCheckService 的 Start/Stop/runOnce + ticker 骨架（tick 1min，
// 按 latestDailySlot / NextRun 判定到期执行），任务注册表全部走 RunPlatformOpsTask
// （账号锁 + extra["ops_state"] 状态记录 + 当日退避）。手动触发经通用端点
// POST /admin/platform-ops/accounts/:id/tasks/:task 分派到本服务的 Runner 实现。
//
// 任务清单（时刻为账号时区，默认 Asia/Shanghai）：
//
//	checkin     每日签到    09,21  Checkin（幂等：已领/未开放按成功）+ 成功后刷新
//	                            额度快照（解冻 qd_no_credit 罚停）
//	pro_welfare Pro 福利包  09,21  eligibility → claim（无资格/已领按成功）
//	campaigns   活动扫描    10     Campaigns 列表，仅 CLAIMABLE 条目领取（幂等）
//	keepalive   token 保活  22     RefreshAccountToken（PAT 账号经 personal_token
//	                            重交换；IncludeDisabled 对 error 账号也要保活）
//	balance     额度刷新    6h     QuotaUsage + UserPlan → extra["qd_quota"]（含
//	                            qd_no_credit 恢复判定）

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/qoder"
)

// qoderOpsDefaultTimeout 单账号单任务的执行预算。
const qoderOpsDefaultTimeout = 2 * time.Minute

// QoderOpsService Qoder 平台运营任务运行器（实现 PlatformOpsRunner）。
type QoderOpsService struct {
	accountRepo  AccountRepository
	oauthService *QoderOAuthService
	// settings 平台级任务调度配置存取（nil = 未配置，全部沿用默认）。
	settings platformOpsSettingStore
	tasks    []PlatformOpsTask
	interval time.Duration
	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

// NewQoderOpsService 构造并自注册到平台运行器注册表；interval <= 0 时 Start()
// 直接返回（不启动），默认 1min。
func NewQoderOpsService(accountRepo AccountRepository, oauthService *QoderOAuthService, settings SettingRepository, interval time.Duration) *QoderOpsService {
	s := &QoderOpsService{
		accountRepo:  accountRepo,
		oauthService: oauthService,
		settings:     settings,
		interval:     interval,
		stopCh:       make(chan struct{}),
	}
	if s.interval <= 0 {
		s.interval = time.Minute
	}
	s.tasks = s.buildTasks()
	RegisterPlatformOpsRunner(s)
	return s
}

// buildTasks 组装任务注册表（RunOne 只做单账号动作，状态记录由
// RunPlatformOpsTask 统一处理）。
func (s *QoderOpsService) buildTasks() []PlatformOpsTask {
	return []PlatformOpsTask{
		{
			ID:              "checkin",
			Platform:        PlatformQoder,
			Name:            "每日签到",
			Hours:           []int{9, 21},
			IncludeDisabled: true, // 签到是积分恢复入口，qd_no_credit 罚停账号也要跑
			RunOne:          s.runCheckin,
		},
		{
			ID:              "pro_welfare",
			Platform:        PlatformQoder,
			Name:            "Pro 福利包",
			Hours:           []int{9, 21},
			IncludeDisabled: true,
			RunOne:          s.runProWelfare,
		},
		{
			ID:       "campaigns",
			Platform: PlatformQoder,
			Name:     "活动扫描",
			Hours:    []int{10},
			RunOne:   s.runCampaigns,
		},
		{
			ID:              "keepalive",
			Platform:        PlatformQoder,
			Name:            "token 保活",
			Hours:           []int{22},
			IncludeDisabled: true, // 保号类：error 账号更要保 token 活性
			RunOne:          s.runKeepalive,
		},
		{
			ID:              "balance",
			Platform:        PlatformQoder,
			Name:            "额度刷新",
			Interval:        6 * time.Hour,
			IncludeDisabled: true, // 额度快照驱动 qd_no_credit 恢复判定
			RunOne:          s.runBalance,
		},
	}
}

// Platform 所属平台。
func (s *QoderOpsService) Platform() string { return PlatformQoder }

// Tasks 任务注册表。
func (s *QoderOpsService) Tasks() []PlatformOpsTask { return s.tasks }

// RunAccountTask 对账号执行指定任务（手动触发入口；内部走 RunPlatformOpsTask）。
func (s *QoderOpsService) RunAccountTask(ctx context.Context, account *Account, taskID string) error {
	for _, task := range s.tasks {
		if task.ID == taskID {
			taskCtx, cancel := context.WithTimeout(ctx, qoderOpsDefaultTimeout)
			defer cancel()
			return RunPlatformOpsTask(taskCtx, s.accountRepo, task, account)
		}
	}
	return fmt.Errorf("unknown qoder ops task %q", taskID)
}

// Start 启动定时循环（ticker 每 interval 评估一次到期任务）。
func (s *QoderOpsService) Start() {
	if s == nil || s.accountRepo == nil || s.oauthService == nil {
		return
	}
	if s.interval <= 0 {
		return
	}
	log.Printf("[QoderOps] started (tick=%s tasks=%d)", s.interval, len(s.tasks))
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

// Stop 停止定时循环（可安全多次调用）。
func (s *QoderOpsService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	s.wg.Wait()
}

// runOnce 单轮评估：列出全部 qoder 账号，对到期任务逐个执行（串行，任务自身
// 有超时预算）。
func (s *QoderOpsService) runOnce() {
	accounts, err := s.accountRepo.ListByPlatform(context.Background(), PlatformQoder)
	if err != nil {
		log.Printf("[QoderOps] list accounts failed: %v", err)
		return
	}
	now := time.Now()
	opsConfigs := LoadPlatformOpsTaskConfigs(context.Background(), s.settings, PlatformQoder)
	for i := range accounts {
		account := &accounts[i]
		for _, task := range s.tasks {
			if !task.IncludeDisabled && !account.IsActive() {
				continue
			}
			// 平台级任务配置：禁用仅停自动调度（手动执行不受影响）；时刻/间隔覆盖默认。
			if !PlatformOpsTaskAutoEnabled(opsConfigs, task.ID) {
				continue
			}
			task = ApplyPlatformOpsTaskConfig(task, opsConfigs)
			if !qoderOpsTaskDue(account, task, now) {
				continue
			}
			taskCtx, cancel := context.WithTimeout(context.Background(), qoderOpsDefaultTimeout)
			runErr := RunPlatformOpsTask(taskCtx, s.accountRepo, task, account)
			cancel()
			if runErr != nil {
				log.Printf("[QoderOps] task %s account %d failed: %v", task.ID, account.ID, runErr)
			}
		}
	}
}

// qoderOpsTaskDue 判定任务是否到期（NextRun 退避顺延优先；Hours 任务按最近一个
// 已到计划触发点晚于 LastRun——当日失败重试由 ops_state 退避 NextRun 驱动；
// Interval 任务距 LastRun 满周期）。
func qoderOpsTaskDue(account *Account, task PlatformOpsTask, now time.Time) bool {
	entry := LoadOpsState(account, task.ID)
	if !entry.NextRun.IsZero() {
		return !now.Before(entry.NextRun)
	}
	if len(task.Hours) > 0 {
		return latestDailySlot(now, task.Hours, platformOpsTimezone(account)).After(entry.LastRun)
	}
	if task.Interval > 0 {
		if entry.LastRun.IsZero() {
			return true
		}
		return now.Sub(entry.LastRun) >= task.Interval
	}
	return false // ManualOnly
}

// ---------------------------------------------------------------------------
// 各任务动作。
// ---------------------------------------------------------------------------

// runCheckin 每日签到（幂等：已签 / 区域未开放 / 活动下线按成功）；成功后刷新
// 额度快照，顺带做 qd_no_credit 恢复判定（签到到账 → error 账号自动回 active）。
func (s *QoderOpsService) runCheckin(ctx context.Context, account *Account) error {
	client, err := s.accountClient(account)
	if err != nil {
		return err
	}
	creds, err := qoderAccountCredentials(account)
	if err != nil {
		return err
	}
	result, err := client.Checkin(ctx, creds)
	if err != nil {
		return fmt.Errorf("qoder checkin: %w", err)
	}
	if !result.OK {
		return fmt.Errorf("qoder checkin failed: %s", result.Message)
	}
	// 签到成功（含幂等）：刷新额度（失败不影响签到结果，只记日志）。
	if snapshot, qerr := s.oauthService.QueryAccountQuota(ctx, account.ID); qerr != nil {
		log.Printf("[QoderOps] checkin quota refresh account %d failed: %v", account.ID, qerr)
	} else if snapshot != nil {
		log.Printf("[QoderOps] checkin ok account %d: %s remain=%d", account.ID, result.Message, snapshot.Remain)
	}
	return nil
}

// runProWelfare Pro 升级福利包：eligibility → claim。无资格 / 端点不存在 /
// 已领取均按成功跳过（上游幂等语义）。
func (s *QoderOpsService) runProWelfare(ctx context.Context, account *Account) error {
	client, err := s.accountClient(account)
	if err != nil {
		return err
	}
	creds, err := qoderAccountCredentials(account)
	if err != nil {
		return err
	}
	eligible, err := client.ProEligibility(ctx, creds)
	if err != nil {
		return fmt.Errorf("qoder pro eligibility: %w", err)
	}
	if !eligible {
		return nil // 无资格 / 活动下线：幂等成功
	}
	result, err := client.ProClaim(ctx, creds)
	if err != nil {
		return fmt.Errorf("qoder pro claim: %w", err)
	}
	if !result.OK {
		return fmt.Errorf("qoder pro claim failed: %s", result.Message)
	}
	log.Printf("[QoderOps] pro welfare account %d: %s", account.ID, result.Message)
	return nil
}

// runCampaigns 活动扫描：拉取活动平台列表，仅对 CLAIMABLE 状态的条目领取
// （幂等：已领回放 BLOCKED/同人去重等确定性结局记日志，不算任务失败）。
func (s *QoderOpsService) runCampaigns(ctx context.Context, account *Account) error {
	client, err := s.accountClient(account)
	if err != nil {
		return err
	}
	creds, err := qoderAccountCredentials(account)
	if err != nil {
		return err
	}
	status, err := client.Campaigns(ctx, creds)
	if err != nil {
		return fmt.Errorf("qoder campaigns: %w", err)
	}
	if !status.Available {
		return nil // 本区域无活动平台：幂等成功
	}
	if status.Error != "" {
		return fmt.Errorf("qoder campaigns unavailable: %s", status.Error)
	}
	var results []string
	for _, campaign := range status.Campaigns {
		if campaign.CampaignID == "" || !strings.EqualFold(strings.TrimSpace(campaign.ClaimStatus), "CLAIMABLE") {
			continue
		}
		claim, cerr := client.ClaimCampaign(ctx, creds, campaign.CampaignID)
		if cerr != nil {
			results = append(results, fmt.Sprintf("%s: %v", campaign.CampaignID, cerr))
			continue
		}
		results = append(results, fmt.Sprintf("%s: %s", campaign.CampaignID, claim.Message))
	}
	if len(results) > 0 {
		log.Printf("[QoderOps] campaigns account %d: %s", account.ID, strings.Join(results, "; "))
	}
	return nil
}

// runKeepalive token 保活：RefreshToken 空跑并写回新凭据（PAT 账号经
// personal_token 重交换，同样适用）。
func (s *QoderOpsService) runKeepalive(ctx context.Context, account *Account) error {
	_, err := s.oauthService.RefreshAccountToken(ctx, account)
	if err != nil {
		return fmt.Errorf("qoder keepalive refresh: %w", err)
	}
	return nil
}

// runBalance 额度刷新：QuotaUsage + UserPlan → extra["qd_quota"] 快照 +
// qd_no_credit 恢复判定。
func (s *QoderOpsService) runBalance(ctx context.Context, account *Account) error {
	snapshot, err := s.oauthService.QueryAccountQuota(ctx, account.ID)
	if err != nil {
		return err
	}
	if snapshot != nil {
		log.Printf("[QoderOps] balance account %d remain=%d/%d plan=%s", account.ID, snapshot.Remain, snapshot.Total, snapshot.Plan)
	}
	return nil
}

// ---------------------------------------------------------------------------
// 账号侧小工具。
// ---------------------------------------------------------------------------

// accountClient 构造挂账号代理的 qoder 客户端。
func (s *QoderOpsService) accountClient(account *Account) (*qoder.Client, error) {
	proxyURL, err := s.oauthService.proxyURL(context.Background(), account.ProxyID)
	if err != nil {
		return nil, err
	}
	return qoder.NewClient(newQoderHTTPClient(proxyURL, 0)), nil
}
