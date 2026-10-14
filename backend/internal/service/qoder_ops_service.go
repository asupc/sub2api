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
//	checkin     每日签到    10,21  CampaignCheckin（活动平台优先，只领 CREDITS
//	                            类；正常出口不兜底）→ 仅列表查询失败时兜底旧 sash
//	                            Checkin；成功后刷新额度快照（解冻 qd_no_credit 罚停）
//	pro_welfare Pro 福利包  09,21  eligibility → claim（无资格/已领按成功）
//	campaigns   活动扫描    10     Campaigns 列表，仅 CLAIMABLE 条目领取（幂等；
//	                            排除 CREDITS 类——每日积分归 checkin 任务）
//	keepalive   token 保活  22     RefreshAccountToken（PAT 账号经 personal_token
//	                            重交换；IncludeDisabled 对 error 账号也要保活）
//	balance     额度刷新    6h     QuotaUsage + UserPlan → extra["qd_quota"]（含
//	                            qd_no_credit 恢复判定）
//
// checkin 的同人去重冷却（claim 返回 BLOCKED → cid 6h 不发 POST）持久化在
// account.extra["qd_campaign_blocked"]（cid→到期 unix 秒）。

import (
	"context"
	"encoding/json"
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
			Hours:           []int{10, 21}, // 名额 10:00（UTC+8）刷新，09:00 必早
			IncludeDisabled: true,          // 签到是积分恢复入口，qd_no_credit 罚停账号也要跑
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

// qoderCampaignBlockedKey 是 extra 中活动领取冷却的顶层键（cid→到期 unix 秒）。
const qoderCampaignBlockedKey = "qd_campaign_blocked"

// loadQoderCampaignBlocked 读取同人去重冷却表（extra 来自 JSONB，数值经
// JSON 往返为 float64，统一转 map[string]int64）。
func loadQoderCampaignBlocked(account *Account) map[string]int64 {
	if account == nil || account.Extra == nil {
		return nil
	}
	raw, ok := account.Extra[qoderCampaignBlockedKey]
	if !ok {
		return nil
	}
	blob, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var out map[string]int64
	if err := json.Unmarshal(blob, &out); err != nil {
		return nil
	}
	return out
}

// saveQoderCampaignBlocked 合并本轮新增冷却标记、丢弃过期项后写回 extra 并落库
// （仿 SaveOpsState：JSON 往返 + UpdateExtra 浅合并 + 同步内存副本）。
func (s *QoderOpsService) saveQoderCampaignBlocked(ctx context.Context, account *Account, marks map[string]int64) {
	merged := loadQoderCampaignBlocked(account)
	if merged == nil {
		merged = map[string]int64{}
	}
	now := time.Now().Unix()
	for cid, exp := range marks {
		merged[cid] = exp
	}
	for cid, exp := range merged {
		if exp <= now {
			delete(merged, cid)
		}
	}
	if account.Extra == nil {
		account.Extra = map[string]any{}
	}
	account.Extra[qoderCampaignBlockedKey] = merged
	if err := s.accountRepo.UpdateExtra(ctx, account.ID, map[string]any{qoderCampaignBlockedKey: merged}); err != nil {
		log.Printf("[QoderOps] save campaign blocked marks account %d failed: %v", account.ID, err)
	}
}

// runCheckin 每日签到闭环（对齐 qoder_tasks.py run_checkin L693-804）：
// 活动平台优先——CampaignCheckin 只领 CREDITS 类条目，OK=true（claimed/already/
// blocked/暂无活动等任一正常出口）即返回，**不兜底**；仅列表查询失败（OK=false）
// 才兜底旧 sash Checkin。成功后刷新额度快照，顺带做 qd_no_credit 恢复判定
// （签到到账 → error 账号自动回 active）。结论文本经 note 透传前端。
func (s *QoderOpsService) runCheckin(ctx context.Context, account *Account) (string, error) {
	client, err := s.accountClient(account)
	if err != nil {
		return "", err
	}
	creds, err := qoderAccountCredentials(account)
	if err != nil {
		return "", err
	}
	note, err := s.checkinViaCampaigns(ctx, client, account, creds)
	if err == nil {
		s.refreshQuotaAfterCheckin(ctx, account, note)
		return note, nil
	}
	// 兜底旧 sash 接口（仅活动平台列表查询失败时进入）。
	result, cerr := client.Checkin(ctx, creds)
	if cerr != nil {
		return "", fmt.Errorf("qoder checkin: %w (campaigns: %v)", cerr, err)
	}
	if !result.OK {
		return "", fmt.Errorf("qoder checkin failed: %s", result.Message)
	}
	s.refreshQuotaAfterCheckin(ctx, account, result.Message)
	return result.Message, nil
}

// checkinViaCampaigns 活动平台签到主路径。返回 note；error 非 nil 表示列表
// 查询失败（service 侧走旧 sash 兜底）。
func (s *QoderOpsService) checkinViaCampaigns(ctx context.Context, client *qoder.Client, account *Account, creds qoder.Credentials) (string, error) {
	camp, err := client.CampaignCheckin(ctx, qoder.CampaignCheckinInput{
		Creds:        creds,
		OnlyKinds:    []string{"", "CREDITS"}, // 只做每日签到领积分，不动券类（Python only_daily=True）
		BlockedUntil: loadQoderCampaignBlocked(account),
		ClaimGap:     time.Second,
	})
	if err != nil {
		return "", err
	}
	if !camp.OK {
		return "", fmt.Errorf("qoder campaigns unavailable: %s", camp.Error)
	}
	if len(camp.BlockedMarks) > 0 {
		s.saveQoderCampaignBlocked(ctx, account, camp.BlockedMarks)
	}
	note := camp.Message
	if camp.Earned == 0 && camp.NextAvailableNote != "" {
		note += "；下次可领 " + camp.NextAvailableNote // Python 前端契约：没到账才渲染下次窗口
	}
	return note, nil
}

// refreshQuotaAfterCheckin 签到后刷新额度快照（失败不影响签到结果，只记日志）。
func (s *QoderOpsService) refreshQuotaAfterCheckin(ctx context.Context, account *Account, note string) {
	if snapshot, qerr := s.oauthService.QueryAccountQuota(ctx, account.ID); qerr != nil {
		log.Printf("[QoderOps] checkin quota refresh account %d failed: %v", account.ID, qerr)
	} else if snapshot != nil {
		log.Printf("[QoderOps] checkin ok account %d: %s remain=%d", account.ID, note, snapshot.Remain)
	}
}

// runProWelfare Pro 升级福利包：eligibility → claim。无资格 / 端点不存在 /
// 已领取均按成功跳过（上游幂等语义）。
func (s *QoderOpsService) runProWelfare(ctx context.Context, account *Account) (string, error) {
	client, err := s.accountClient(account)
	if err != nil {
		return "", err
	}
	creds, err := qoderAccountCredentials(account)
	if err != nil {
		return "", err
	}
	eligible, err := client.ProEligibility(ctx, creds)
	if err != nil {
		return "", fmt.Errorf("qoder pro eligibility: %w", err)
	}
	if !eligible {
		return "Pro 升级包不可领取（已领或活动未开放）", nil // 幂等成功
	}
	result, err := client.ProClaim(ctx, creds)
	if err != nil {
		return "", fmt.Errorf("qoder pro claim: %w", err)
	}
	if !result.OK {
		return "", fmt.Errorf("qoder pro claim failed: %s", result.Message)
	}
	log.Printf("[QoderOps] pro welfare account %d: %s", account.ID, result.Message)
	return result.Message, nil
}

// runCampaigns 活动扫描：拉取活动平台列表，仅对 CLAIMABLE 状态的**非 CREDITS**
// 条目领取（兑换码/券/福利类；每日积分归 checkin 任务，两任务零重叠。幂等：
// 已领回放 BLOCKED/同人去重等确定性结局记日志，不算任务失败）。
func (s *QoderOpsService) runCampaigns(ctx context.Context, account *Account) (string, error) {
	client, err := s.accountClient(account)
	if err != nil {
		return "", err
	}
	creds, err := qoderAccountCredentials(account)
	if err != nil {
		return "", err
	}
	status, err := client.Campaigns(ctx, creds)
	if err != nil {
		return "", fmt.Errorf("qoder campaigns: %w", err)
	}
	if !status.Available {
		return "本区域无活动平台", nil // 幂等成功
	}
	if status.Error != "" {
		return "", fmt.Errorf("qoder campaigns unavailable: %s", status.Error)
	}
	var results []string
	for _, campaign := range status.Campaigns {
		if campaign.CampaignID == "" || !strings.EqualFold(strings.TrimSpace(campaign.ClaimStatus), "CLAIMABLE") {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(campaign.BenefitKind), "CREDITS") {
			continue // 每日积分归 checkin 任务，避免重复打上游
		}
		claim, cerr := client.ClaimCampaign(ctx, creds, campaign.CampaignID)
		if cerr != nil {
			results = append(results, fmt.Sprintf("%s: %v", campaign.CampaignID, cerr))
			continue
		}
		results = append(results, fmt.Sprintf("%s: %s", campaign.CampaignID, claim.Message))
	}
	note := strings.Join(results, "; ")
	if note != "" {
		log.Printf("[QoderOps] campaigns account %d: %s", account.ID, note)
		return note, nil
	}
	return "无可领取的活动福利", nil
}

// runKeepalive token 保活：RefreshToken 空跑并写回新凭据（PAT 账号经
// personal_token 重交换，同样适用）。
func (s *QoderOpsService) runKeepalive(ctx context.Context, account *Account) (string, error) {
	_, err := s.oauthService.RefreshAccountToken(ctx, account)
	if err != nil {
		return "", fmt.Errorf("qoder keepalive refresh: %w", err)
	}
	return "", nil
}

// runBalance 额度刷新：QuotaUsage + UserPlan → extra["qd_quota"] 快照 +
// qd_no_credit 恢复判定。
func (s *QoderOpsService) runBalance(ctx context.Context, account *Account) (string, error) {
	snapshot, err := s.oauthService.QueryAccountQuota(ctx, account.ID)
	if err != nil {
		return "", err
	}
	if snapshot != nil {
		log.Printf("[QoderOps] balance account %d remain=%d/%d plan=%s", account.ID, snapshot.Remain, snapshot.Total, snapshot.Plan)
	}
	return "", nil
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
