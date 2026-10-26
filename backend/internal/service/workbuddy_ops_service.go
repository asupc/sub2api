package service

// workbuddy_ops_service.go 二开新增：WorkBuddy 平台运营任务服务。
//
// 克隆 CNProviderBalanceCheckService 的 Start/Stop/runOnce + ticker 骨架（tick 1min，
// 按 nextDailyFire / NextRun 判定到期执行），任务注册表全部走 RunPlatformOpsTask
// （账号锁 + extra["ops_state"] 状态记录 + 当日退避）。手动触发经通用端点
// POST /admin/platform-ops/accounts/:id/tasks/:task 分派到本服务的 Runner 实现。
//
// 任务清单（时刻为账号时区，默认 Asia/Shanghai）：
//
//	checkin   每日签到   09,21  DailyCheckin（幂等）+ 成功后刷新余额快照（含恢复判定）
//	travel    猫猫旅行   09,21  RunTravelRound（领奖/派出/领养单趟闭环）
//	activity  活跃上报   10     ReportChatActivity（点亮 growth 连登 / first_buddy 门槛）
//	keepalive token 保活 22     RefreshToken 空跑（企业账号同样适用）
//	blackcat  夜猫子     23     InNightWindow 才 RunNightChats 补足差额
//	growth    成长任务   01     AcceptPendingGrowthTasks + 内置动作（chat_5/first_buddy/black_cat）
//	balance   余额刷新   6h     UserResourceDetailedWithExpiry → extra["wb_quota"]
//
// 企业账号（enterprise_id 非空）无个人成长体系（签到/旅行/上报/夜猫子/成长上游一律
// 403），growth 族任务直接跳过；keepalive/balance 正常执行。

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/workbuddy"
)

// 任务执行预算（单账号单任务）。
const (
	workbuddyOpsDefaultTimeout = 2 * time.Minute
	workbuddyOpsTravelTimeout  = 3 * time.Minute
	workbuddyOpsGrowthTimeout  = 10 * time.Minute
	workbuddyOpsNightTimeout   = 10 * time.Minute
)

// WorkbuddyOpsService WorkBuddy 平台运营任务运行器（实现 PlatformOpsRunner）。
type WorkbuddyOpsService struct {
	accountRepo  AccountRepository
	oauthService *WorkbuddyOAuthService
	// settings 平台级任务调度配置存取（nil = 未配置，全部沿用默认）。
	settings platformOpsSettingStore
	tasks        []PlatformOpsTask
	interval     time.Duration
	stopCh       chan struct{}
	stopOnce     sync.Once
	wg           sync.WaitGroup

	// now 测试时钟接缝（缺省 time.Now）。
	now func() time.Time
}

// NewWorkbuddyOpsService 构造并自注册到平台运行器注册表；interval <= 0 时
// Start() 直接返回（不启动），默认 1min。
func NewWorkbuddyOpsService(accountRepo AccountRepository, oauthService *WorkbuddyOAuthService, settings SettingRepository, interval time.Duration) *WorkbuddyOpsService {
	s := &WorkbuddyOpsService{
		accountRepo:  accountRepo,
		oauthService: oauthService,
		settings:     settings,
		interval:     interval,
		stopCh:       make(chan struct{}),
		now:          time.Now,
	}
	if s.interval <= 0 {
		s.interval = time.Minute
	}
	s.tasks = s.buildTasks()
	RegisterPlatformOpsRunner(s)
	return s
}

// buildTasks 组装任务注册表（RunOne 只做单账号动作，状态记录由 RunPlatformOpsTask 统一处理）。
func (s *WorkbuddyOpsService) buildTasks() []PlatformOpsTask {
	return []PlatformOpsTask{
		{
			ID:              "checkin",
			Platform:        PlatformWorkbuddy,
			Name:            "每日签到",
			Hours:           []int{9, 21},
			IncludeDisabled: true, // 签到是积分恢复入口，error 账号也要跑
			RunOne:          s.runCheckin,
		},
		{
			ID:              "travel",
			Platform:        PlatformWorkbuddy,
			Name:            "猫猫旅行",
			Hours:           []int{9, 21},
			IncludeDisabled: true,
			RunOne:          s.runTravel,
		},
		{
			ID:       "activity",
			Platform: PlatformWorkbuddy,
			Name:     "活跃上报",
			Hours:    []int{10},
			RunOne:   s.runActivity,
		},
		{
			ID:              "keepalive",
			Platform:        PlatformWorkbuddy,
			Name:            "token 保活",
			Hours:           []int{22},
			IncludeDisabled: true, // 保号类：error 账号更要保 token 活性
			RunOne:          s.runKeepalive,
		},
		{
			ID:       "blackcat",
			Platform: PlatformWorkbuddy,
			Name:     "夜猫子",
			Hours:    []int{23},
			RunOne:   s.runBlackcat,
		},
		{
			ID:       "growth",
			Platform: PlatformWorkbuddy,
			Name:     "成长任务",
			Hours:    []int{1},
			RunOne:   s.runGrowth,
		},
		{
			// 补领清扫（移植自 workbuddy2api-panel 162762f）：周期幂等 accept 未接单
			// + claim 已达标未领的奖励，不跑行为链路。与 growth 互补——growth 只覆盖
			// 内置动作任务，经真实对话点亮的任务由本任务兜底领奖（治积分卡住）。
			ID:       "growth_sweep",
			Platform: PlatformWorkbuddy,
			Name:     "成长任务补领清扫",
			Interval: 4 * time.Hour,
			RunOne:   s.runGrowthSweep,
		},
		{
			// 补偿先查后领（移植自 workbuddy2api-panel d48b715）：盲领会把「没补偿/
			// 已领过/领取失败」混成同一种日志，补偿带截止日会静默过期。先查状态，
			// 待领才领；无活动是正常状态不算错误。
			ID:              "compensation",
			Platform:        PlatformWorkbuddy,
			Name:            "补偿检查领取",
			Interval:        6 * time.Hour,
			IncludeDisabled: true, // 补偿是积分恢复入口，error 账号也要领
			RunOne:          s.runCompensation,
		},
		{
			ID:              "balance",
			Platform:        PlatformWorkbuddy,
			Name:            "余额刷新",
			Interval:        6 * time.Hour,
			IncludeDisabled: true, // 余额快照驱动 wb_no_credit 恢复判定
			RunOne:          s.runBalance,
		},
	}
}

// Platform 所属平台。
func (s *WorkbuddyOpsService) Platform() string { return PlatformWorkbuddy }

// Tasks 任务注册表。
func (s *WorkbuddyOpsService) Tasks() []PlatformOpsTask { return s.tasks }

// RunAccountTask 对账号执行指定任务（手动触发入口；内部走 RunPlatformOpsTask）。
func (s *WorkbuddyOpsService) RunAccountTask(ctx context.Context, account *Account, taskID string) error {
	for _, task := range s.tasks {
		if task.ID == taskID {
			taskCtx, cancel := context.WithTimeout(ctx, s.taskTimeout(taskID))
			defer cancel()
			return RunPlatformOpsTask(taskCtx, s.accountRepo, task, account)
		}
	}
	return fmt.Errorf("unknown workbuddy ops task %q", taskID)
}

// Start 启动定时循环（ticker 每 interval 评估一次到期任务）。
func (s *WorkbuddyOpsService) Start() {
	if s == nil || s.accountRepo == nil || s.oauthService == nil {
		return
	}
	if s.interval <= 0 {
		return
	}
	log.Printf("[WorkbuddyOps] started (tick=%s tasks=%d)", s.interval, len(s.tasks))
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		// 启动后先等待一个周期再首次评估，避免与进程启动峰重叠
		//（balance 首跑在启动一个 tick 后自然发生）。
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
func (s *WorkbuddyOpsService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	s.wg.Wait()
}

// runOnce 单轮评估：列出全部 workbuddy 账号，对到期任务逐个执行（串行，
// 任务自身有超时预算，不会互相放大在途请求）。
func (s *WorkbuddyOpsService) runOnce() {
	accounts, err := s.accountRepo.ListByPlatform(context.Background(), PlatformWorkbuddy)
	if err != nil {
		log.Printf("[WorkbuddyOps] list accounts failed: %v", err)
		return
	}
	now := s.now()
	opsConfigs := LoadPlatformOpsTaskConfigs(context.Background(), s.settings, PlatformWorkbuddy)
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
			if !s.taskDue(account, task, now) {
				continue
			}
			taskCtx, cancel := context.WithTimeout(context.Background(), s.taskTimeout(task.ID))
			runErr := RunPlatformOpsTask(taskCtx, s.accountRepo, task, account)
			cancel()
			if runErr != nil {
				log.Printf("[WorkbuddyOps] task %s account %d failed: %v", task.ID, account.ID, runErr)
			}
			// SaveOpsState 就地更新 account.Extra，同账号后续任务的到期判定
			// 能看到本任务刚写入的 LastRun/NextRun。
		}
	}
}

// taskDue 判定任务是否到期：
//   - NextRun 非零（失败退避顺延）：now >= NextRun 即到期；
//   - Hours 任务：最近一个已到计划触发点晚于 LastRun 即到期（跨日自然重置）；
//   - Interval 任务：距 LastRun 超过间隔（LastRun 零值 = 首跑到期）。
func (s *WorkbuddyOpsService) taskDue(account *Account, task PlatformOpsTask, now time.Time) bool {
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

// latestDailySlot 返回 now 之前（含恰好命中）最近一个 hours 整点时刻。
func latestDailySlot(now time.Time, hours []int, loc *time.Location) time.Time {
	now = now.In(loc)
	var latest time.Time
	for _, h := range hours {
		t := time.Date(now.Year(), now.Month(), now.Day(), h, 0, 0, 0, loc)
		if t.After(now) {
			t = t.Add(-24 * time.Hour)
		}
		if latest.IsZero() || t.After(latest) {
			latest = t
		}
	}
	return latest
}

// taskTimeout 单任务执行预算。
func (s *WorkbuddyOpsService) taskTimeout(taskID string) time.Duration {
	switch taskID {
	case "travel":
		return workbuddyOpsTravelTimeout
	case "growth":
		return workbuddyOpsGrowthTimeout
	case "blackcat":
		return workbuddyOpsNightTimeout
	default:
		return workbuddyOpsDefaultTimeout
	}
}

// ---------------------------------------------------------------------------
// 各任务动作。
// ---------------------------------------------------------------------------

// runCheckin 每日签到（幂等：IsAlreadyCheckin 视为成功）；成功后刷新余额快照，
// 顺带做 wb_no_credit 恢复判定（签到到账 → error 账号自动回 active）。
func (s *WorkbuddyOpsService) runCheckin(ctx context.Context, account *Account) (string, error) {
	cred, skip, err := s.growthEligibleCredentials(account)
	if err != nil {
		return "", err
	}
	if skip {
		return "企业账号无个人成长体系，跳过", nil
	}
	client, err := s.accountClient(account)
	if err != nil {
		return "", err
	}
	if err := client.DailyCheckin(ctx, cred); err != nil {
		if workbuddy.IsAlreadyCheckin(err) {
			return "今日已签到", nil // 幂等成功
		}
		return "", fmt.Errorf("daily checkin: %w", err)
	}
	// 签到成功：刷新余额（失败不影响签到结果，只记日志）。
	if snapshot, qerr := s.oauthService.QueryWorkbuddyQuota(ctx, account.ID); qerr != nil {
		log.Printf("[WorkbuddyOps] checkin quota refresh account %d failed: %v", account.ID, qerr)
	} else if snapshot != nil {
		log.Printf("[WorkbuddyOps] checkin ok account %d remain=%d", account.ID, snapshot.Remain)
	}
	return "签到成功", nil
}

// runTravel 猫猫旅行单趟巡检（领奖 / 派出 / 领养，最多一个动作）。
func (s *WorkbuddyOpsService) runTravel(ctx context.Context, account *Account) (string, error) {
	cred, skip, err := s.behaviorEligibleCredentials(account)
	if err != nil {
		return "", err
	}
	if skip {
		return "", nil
	}
	client, err := s.accountClient(account)
	if err != nil {
		return "", err
	}
	msg, err := client.RunTravelRound(ctx, cred)
	if err != nil {
		return "", fmt.Errorf("travel round: %w", err)
	}
	log.Printf("[WorkbuddyOps] travel account %d: %s", account.ID, msg)
	return msg, nil
}

// runActivity 对话活跃上报（一条 chat_request_send 点亮连登 + first_buddy 门槛）。
func (s *WorkbuddyOpsService) runActivity(ctx context.Context, account *Account) (string, error) {
	cred, skip, err := s.behaviorEligibleCredentials(account)
	if err != nil {
		return "", err
	}
	if skip {
		return "", nil
	}
	client, err := s.accountClient(account)
	if err != nil {
		return "", err
	}
	conv := fmt.Sprintf("wb2api-ops-activity-%d", s.now().UnixMilli())
	if err := client.ReportChatActivity(ctx, cred, conv, ""); err != nil {
		return "", fmt.Errorf("report chat activity: %w", err)
	}
	return "", nil
}

// runKeepalive token 保活：RefreshToken 空跑并写回新凭据（企业账号同样适用）。
func (s *WorkbuddyOpsService) runKeepalive(ctx context.Context, account *Account) (string, error) {
	_, err := s.oauthService.RefreshAccountToken(ctx, account)
	if err != nil {
		return "", fmt.Errorf("keepalive refresh: %w", err)
	}
	return "", nil
}

// runBlackcat 夜猫子：仅在 23:00–08:00 计数窗口内补足差额对话，窗口外视为成功跳过。
func (s *WorkbuddyOpsService) runBlackcat(ctx context.Context, account *Account) (string, error) {
	cred, skip, err := s.behaviorEligibleCredentials(account)
	if err != nil {
		return "", err
	}
	if skip {
		return "", nil
	}
	if !workbuddy.InNightWindow(s.now()) {
		return "非夜窗（23:00–08:00），跳过", nil
	}
	client, err := s.accountClient(account)
	if err != nil {
		return "", err
	}
	need, err := client.BlackcatNeed(ctx, cred)
	if err != nil {
		return "", fmt.Errorf("blackcat need: %w", err)
	}
	if need <= 0 {
		return "夜窗对话数已达标", nil
	}
	done, err := client.RunNightChats(ctx, cred, int(need))
	if err != nil {
		return "", fmt.Errorf("night chats completed=%d: %w", done, err)
	}
	log.Printf("[WorkbuddyOps] blackcat account %d completed %d chats", account.ID, done)
	return fmt.Sprintf("补足夜窗对话 %d 条", done), nil
}

// runGrowth 成长任务：批量接受未接受任务 + 内置动作闭环（chat_5 / first_buddy /
// black_cat；black_cat 动作内部自带夜窗判定）。其余无内置动作的任务仅接受报名，
// 达标领奖交给下一轮（判据事件未移植，见 pkg tasks.go 注释）。
func (s *WorkbuddyOpsService) runGrowth(ctx context.Context, account *Account) (string, error) {
	cred, skip, err := s.behaviorEligibleCredentials(account)
	if err != nil {
		return "", err
	}
	if skip {
		return "", nil
	}
	client, err := s.accountClient(account)
	if err != nil {
		return "", err
	}
	accepted, err := client.AcceptPendingGrowthTasks(ctx, cred)
	if err != nil {
		return "", fmt.Errorf("accept growth tasks: %w", err)
	}
	var results []string
	for _, code := range []string{"chat_5", "first_buddy", "black_cat"} {
		res, err := client.RunGrowthAction(ctx, cred, code, false)
		if err != nil {
			results = append(results, fmt.Sprintf("%s: %v", code, err))
			continue
		}
		if res != nil {
			results = append(results, fmt.Sprintf("%s: %s", code, res.Message))
		}
	}
	note := fmt.Sprintf("accepted=%d %s", accepted, strings.Join(results, "; "))
	log.Printf("[WorkbuddyOps] growth account %d %s", account.ID, note)
	return note, nil
}

// runBalance 余额刷新：查上游积分 → extra["wb_quota"] 快照 + wb_no_credit 恢复判定。
func (s *WorkbuddyOpsService) runBalance(ctx context.Context, account *Account) (string, error) {
	snapshot, err := s.oauthService.QueryWorkbuddyQuota(ctx, account.ID)
	if err != nil {
		return "", err
	}
	if snapshot != nil {
		log.Printf("[WorkbuddyOps] balance account %d remain=%d/%d", account.ID, snapshot.Remain, snapshot.Total)
	}
	return "", nil
}

// runGrowthSweep 补领清扫：只 accept 未接单 + claim 已达标未领（移植自
// workbuddy2api-panel 162762f）。不跑行为链路，与 runGrowth 互补。跳过企业切换
// 会话（行为类判定同源——虽然清扫本身不上报，但该形态下任务进度恒 0/x，清扫也是
// 空转，省上游请求）。
func (s *WorkbuddyOpsService) runGrowthSweep(ctx context.Context, account *Account) (string, error) {
	cred, skip, err := s.behaviorEligibleCredentials(account)
	if err != nil {
		return "", err
	}
	if skip {
		return "", nil
	}
	client, err := s.accountClient(account)
	if err != nil {
		return "", err
	}
	res, err := client.RunTaskSweep(ctx, cred)
	if err != nil {
		return "", fmt.Errorf("task sweep: %w", err)
	}
	if res.Accepted == 0 && res.Claimed == 0 {
		return "无待补领", nil
	}
	note := fmt.Sprintf("accepted=%d claimed=%d credit=%d energy=%d", res.Accepted, res.Claimed, res.Credits, res.Energy)
	log.Printf("[WorkbuddyOps] growth_sweep account %d %s", account.ID, note)
	return note, nil
}

// runCompensation 补偿先查后领（移植自 workbuddy2api-panel d48b715）：
// 查状态 → Pending 才领。无活动/已领完/无额度都是正常状态（不算错误）。
func (s *WorkbuddyOpsService) runCompensation(ctx context.Context, account *Account) (string, error) {
	// 补偿对个人/企业/切换会话都可能发放，不做成长体系门控；只要有凭证就查。
	cred, err := parseWorkbuddyCredentials(account)
	if err != nil {
		return "", fmt.Errorf("parse workbuddy credentials: %w", err)
	}
	client, err := s.accountClient(account)
	if err != nil {
		return "", err
	}
	st, err := client.GetCompensationStatus(ctx, cred)
	if err != nil {
		return "", fmt.Errorf("compensation status: %w", err)
	}
	if !st.Pending() {
		return "无待领补偿", nil // active=false / 已领 / 无额度均为正常
	}
	credit, err := client.ClaimCompensation(ctx, cred)
	if err != nil {
		// 领取失败必须显形（源的命题是「不要静默失败」）：补偿带截止日，下次
		// 轮询前可能过期，返回错误让 ops 状态记录可见。
		return "", fmt.Errorf("claim compensation (credit_num=%d, end_time=%s): %w", st.CreditNum, st.EndTime, err)
	}
	note := fmt.Sprintf("领取补偿 %d 积分（截止 %s）", credit, st.EndTime)
	log.Printf("[WorkbuddyOps] compensation account %d %s", account.ID, note)
	// 领到积分后顺手刷新余额快照（失败不影响领取结果）。
	if _, qerr := s.oauthService.QueryWorkbuddyQuota(ctx, account.ID); qerr != nil {
		log.Printf("[WorkbuddyOps] compensation quota refresh account %d failed: %v", account.ID, qerr)
	}
	return note, nil
}

// ---------------------------------------------------------------------------
// 账号侧小工具。
// ---------------------------------------------------------------------------

// growthEligibleCredentials 解析凭证；企业账号 skip=true（调用方按跳过处理）。
// 只判**企业版账号**（计费口径 enterprise_id）——签到等对切换会话仍有效的任务用本函数。
func (s *WorkbuddyOpsService) growthEligibleCredentials(account *Account) (cred workbuddy.Credentials, skip bool, err error) {
	cred, err = parseWorkbuddyCredentials(account)
	if err != nil {
		return workbuddy.Credentials{}, false, fmt.Errorf("parse workbuddy credentials: %w", err)
	}
	if cred.IsEnterprise() {
		return workbuddy.Credentials{}, true, nil
	}
	return cred, false, nil
}

// behaviorEligibleCredentials 行为类任务的门控（移植自 workbuddy2api-panel 9282779）：
// 在 growthEligibleCredentials 之上再排除企业**切换会话**（token_source=
// enterprise_switch）——该形态下 /v2/report 行为事件返回 200 却被上游静默丢弃，
// 成长任务每轮白跑且无任何报错（实测 15 个任务全部 0/x）。签到等显式报错的任务
// 不走本门控（源同样不动签到）；skip 时调用方按跳过处理并提示用户重新授权。
func (s *WorkbuddyOpsService) behaviorEligibleCredentials(account *Account) (cred workbuddy.Credentials, skip bool, err error) {
	cred, skip, err = s.growthEligibleCredentials(account)
	if err != nil || skip {
		return cred, skip, err
	}
	if cred.IsEnterpriseSession() {
		return workbuddy.Credentials{}, true, nil
	}
	return cred, false, nil
}

// accountClient 构造挂账号代理的上游客户端。
func (s *WorkbuddyOpsService) accountClient(account *Account) (*workbuddy.Client, error) {
	proxyURL, err := s.oauthService.proxyURL(context.Background(), account.ProxyID)
	if err != nil {
		return nil, err
	}
	return newWorkbuddyUpstreamClient(proxyURL, 0), nil
}
