// tasks.go growth/billing 域运营动作：成长任务（清单/接受/领奖）、猫猫旅行、
// 连登兑换与抽奖、对话活跃上报、夜猫子补足、新手礼包/补偿、补签卡。
//
// 移植自 workbuddy2api-panel internal/upstream/{tasks,travel,streak,report,blackcat}.go
// 与 internal/panel/{tasks,autotask,taskcenter}.go 的上游端点层 + 少量无状态闭环。
// 调度节奏（何时跑、账号间限速、当日去重）归 sub2api 调度体系，不在本包。
package workbuddy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// growth 域任务路径（chatBase + BillingHeaders）。
const (
	tasksListPath   = "/v2/activity/growth/tasks"
	tasksAcceptPath = "/v2/activity/growth/tasks/accept"
)

// mpPlatform 小程序口径头值：小程序限定任务（Sequential_Tasks_1 / school_season）
// 的列表下发、accept、claim 全链路要求 X-Client-Platform: miniprogram。
const mpPlatform = "miniprogram"

// growth 域路径（chatBase，不带 /v2 前缀，实测）。
const (
	travelStatusPath   = "/activity/growth/buddy/travel/status"
	travelDepartPath   = "/activity/growth/buddy/travel/depart"
	travelClaimPath    = "/activity/growth/buddy/travel/claim"
	buddyInfoPath      = "/activity/growth/buddy/info"
	buddyFirstPath     = "/activity/growth/buddy/first"
	buddyAgreementPath = "/activity/growth/buddy/agreement"
	streakPath         = "/activity/growth/streak"
	streakRedeemPath   = "/activity/growth/redeem"
	lotterySummaryPath = "/activity/growth/lottery/summary"
	lotteryDrawPath    = "/activity/growth/lottery/draw"
	heatmapPath        = "/activity/growth/heatmap"
	makeupCardUsePath  = "/activity/growth/makeup-cards/use"
)

// reportPath 对话活跃上报通道（billingBase 域，实测）。
const reportPath = "/v2/report"

// buddyTaskIncompleteMarker 领养门槛未达标的业务错误关键词（HTTP 400 时出现）。
const buddyTaskIncompleteMarker = "first_buddy task not completed yet"

// 动作节奏常量（对齐上游脚本实测的 1.05s 口径，避免风控；测试可缩短）。
var (
	// reportGap 连续上报之间的间隔。
	reportGap = 1050 * time.Millisecond
	// claimPollAttempts / claimPollGap 达标回读的有界轮询参数：上游计分是异步的
	//（行为事件上报后进度要数秒才刷新），一次性回读会误判「未达标」而跳过自动
	// 领奖。总预算约 12 秒。
	claimPollAttempts = 4
	claimPollGap      = 3 * time.Second
	// nightChatGap 夜猫子补足对话之间的间隔。
	nightChatGap = 4 * time.Second
)

// Task 单个成长任务的视图（字段名与上游 JSON 对齐，多余字段不透出）。
type Task struct {
	TaskCode     string `json:"task_code"`
	Title        string `json:"title,omitempty"`
	Description  string `json:"description,omitempty"` // 操作指引（含跳转说明）
	TaskDesc     string `json:"task_desc,omitempty"`   // 达成条件简述
	Credit       int64  `json:"credit,omitempty"`      // 奖励积分（上游 reward_credit）
	Energy       int64  `json:"energy,omitempty"`      // 奖励能量（上游 reward_energy）
	HasReward    bool   `json:"has_reward,omitempty"`
	RewardBuddy  bool   `json:"reward_buddy,omitempty"`
	TaskType     string `json:"task_type,omitempty"` // single（一次性）/ 累计型
	Tag          string `json:"tag,omitempty"`       // 端标记（PC 等）
	JumpURL      string `json:"jump_url,omitempty"`  // 客户端跳转协议（workbuddy://...）
	Locked       bool   `json:"locked,omitempty"`    // 上游标记未解锁
	Target       int64  `json:"target"`              // 目标次数（恒输出：0 是有效进度值）
	Current      int64  `json:"current"`             // 当前进度（恒输出：0 是有效进度值）
	AcceptStatus string `json:"accept_status,omitempty"`
	Status       string `json:"status,omitempty"`    // 上游任务状态（complete 等）
	Claimable    bool   `json:"claimable,omitempty"` // 进度达标且未领取（本地推算）
	Claimed      bool   `json:"claimed,omitempty"`   // 已领取（accept_status == claimed）
}

// growthJSON 发 growth 域请求（chatBase + BillingHeaders）并解信封。
func (c *Client) growthJSON(ctx context.Context, cred Credentials, method, path string, body any) (json.RawMessage, error) {
	req, err := c.newJSONRequest(ctx, method, c.chatBase(cred)+path, body)
	if err != nil {
		return nil, err
	}
	BillingHeaders(req, cred)
	return c.doJSON(ctx, req)
}

// growthJSONMP 同 growthJSON，叠加小程序口径头 X-Client-Platform: miniprogram。
func (c *Client) growthJSONMP(ctx context.Context, cred Credentials, method, path string, body any) (json.RawMessage, error) {
	req, err := c.newJSONRequest(ctx, method, c.chatBase(cred)+path, body)
	if err != nil {
		return nil, err
	}
	BillingHeaders(req, cred)
	req.Header.Set("X-Client-Platform", mpPlatform)
	return c.doJSON(ctx, req)
}

// ListTasks 拉取全量任务列表（默认口径，无端标记头）。
// progress 可能是 {current,target} 对象或平铺字段，两种形状都解析（对象优先）。
func (c *Client) ListTasks(ctx context.Context, cred Credentials) ([]Task, error) {
	data, err := c.growthJSON(ctx, cred, http.MethodGet, tasksListPath, nil)
	if err != nil {
		return nil, err
	}
	return parseGrowthTasks(data)
}

// ListTasksMP 拉取小程序口径的任务列表（mp 头）。小程序限定任务
// （school_season / Sequential_Tasks_1 等）仅在该口径下发，实测 mp 列表是默认口径
// 的超集；合并时调用方须按 task_code 去重。
func (c *Client) ListTasksMP(ctx context.Context, cred Credentials) ([]Task, error) {
	data, err := c.growthJSONMP(ctx, cred, http.MethodGet, tasksListPath, nil)
	if err != nil {
		return nil, err
	}
	return parseGrowthTasks(data)
}

// parseGrowthTasks 解析 growth 任务列表 data.tasks[]（默认与 mp 口径共用）。
func parseGrowthTasks(data json.RawMessage) ([]Task, error) {
	var resp struct {
		Tasks []struct {
			TaskCode     string          `json:"task_code"`
			Title        string          `json:"title"`
			Description  string          `json:"description"`
			TaskDesc     string          `json:"task_desc"`
			RewardCredit int64           `json:"reward_credit"`
			RewardEnergy int64           `json:"reward_energy"`
			HasReward    bool            `json:"has_reward"`
			RewardBuddy  bool            `json:"reward_buddy"`
			TaskType     string          `json:"task_type"`
			Tag          string          `json:"tag"`
			JumpURL      string          `json:"jump_url"`
			Locked       bool            `json:"locked"`
			AcceptStatus string          `json:"accept_status"`
			Status       string          `json:"status"`
			Target       int64           `json:"target"`
			Current      int64           `json:"current"`
			Progress     json.RawMessage `json:"progress"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	out := make([]Task, 0, len(resp.Tasks))
	for _, t := range resp.Tasks {
		cur, tgt := t.Current, t.Target
		// progress 可能是 {current,target} 对象（实测口径），覆盖平铺字段。
		if len(t.Progress) > 0 && string(t.Progress) != "null" {
			var pr struct {
				Current int64 `json:"current"`
				Target  int64 `json:"target"`
			}
			if json.Unmarshal(t.Progress, &pr) == nil && (pr.Target > 0 || pr.Current > 0) {
				cur, tgt = pr.Current, pr.Target
			}
		}
		claimed := t.AcceptStatus == "claimed"
		out = append(out, Task{
			TaskCode:     t.TaskCode,
			Title:        t.Title,
			Description:  t.Description,
			TaskDesc:     t.TaskDesc,
			Credit:       t.RewardCredit,
			Energy:       t.RewardEnergy,
			HasReward:    t.HasReward,
			RewardBuddy:  t.RewardBuddy,
			TaskType:     t.TaskType,
			Tag:          t.Tag,
			JumpURL:      t.JumpURL,
			Locked:       t.Locked,
			Target:       tgt,
			Current:      cur,
			AcceptStatus: t.AcceptStatus,
			Status:       t.Status,
			Claimable:    !claimed && tgt > 0 && cur >= tgt,
			Claimed:      claimed,
		})
	}
	return out, nil
}

// AcceptTasks 接受任务（报名；幂等：已 accepted 时上游返回成功或业务提示）。
// accept 不产生进度——进度由服务端行为事件点亮（如 chat_request_send 上报），
// 故 accept 后可幂等重放。
func (c *Client) AcceptTasks(ctx context.Context, cred Credentials, taskCodes []string) error {
	_, err := c.growthJSON(ctx, cred, http.MethodPost, tasksAcceptPath, map[string]any{"task_codes": taskCodes})
	return err
}

// AcceptTasksMP 接受小程序限定任务（mp 头；缺头实测 task not found）。
func (c *Client) AcceptTasksMP(ctx context.Context, cred Credentials, taskCodes []string) error {
	_, err := c.growthJSONMP(ctx, cred, http.MethodPost, tasksAcceptPath, map[string]any{"task_codes": taskCodes})
	return err
}

// ClaimReward 领取单个任务奖励（Web 成长中心口径）：
//
//	POST {webBase}/activity/growth/tasks/<task_code>/claim
//	（任务码在路径里，无 body；带 x-client-platform: web 头，Bearer 鉴权）
//
// 注意：CLI 域的 /v2/activity/growth/tasks/reward/claim（task_code 放 body）该路径
// 不存在，恒返回 400 "task not completed"——领奖必须走 Web 域。
// 返回 (credit, energy)：已领取过时为 (0, 0, nil)（幂等，不算错误）。
func (c *Client) ClaimReward(ctx context.Context, cred Credentials, taskCode string) (credit, energy int64, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.webBase(cred)+"/activity/growth/tasks/"+url.PathEscape(taskCode)+"/claim", nil)
	if err != nil {
		return 0, 0, err
	}
	// Web 端请求头形状（对照浏览器实际请求）：Origin/Referer 指向成长中心，
	// 带 x-client-platform: web 标记来源端。
	req.Header.Set("Authorization", "Bearer "+cred.AccessToken)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", webBaseCN)
	req.Header.Set("Referer", webBaseCN+"/profile/growth-center")
	req.Header.Set("x-client-platform", "web")
	if ua := userAgent(cred); ua != "" {
		req.Header.Set("User-Agent", ua)
	}
	if cred.UID != "" {
		req.Header.Set("X-User-Id", cred.UID)
	}
	if cred.EnterpriseID != "" {
		req.Header.Set("X-Enterprise-Id", cred.EnterpriseID)
		req.Header.Set("X-Tenant-Id", cred.EnterpriseID)
	}
	if d := strings.TrimSpace(cred.Domain); d != "" {
		req.Header.Set("X-Domain", d)
	}
	data, err := c.doJSON(ctx, req)
	if err != nil {
		return 0, 0, err
	}
	return parseClaimReward(data)
}

// ClaimRewardMP 领取小程序限定任务奖励：chat 域 /activity/growth/tasks/{code}/claim
// + mp 头；chat 域 400 时降级 Web 域领奖端点（ClaimReward）。
func (c *Client) ClaimRewardMP(ctx context.Context, cred Credentials, taskCode string) (credit, energy int64, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.chatBase(cred)+"/activity/growth/tasks/"+url.PathEscape(taskCode)+"/claim", nil)
	if err != nil {
		return 0, 0, err
	}
	BillingHeaders(req, cred)
	req.Header.Set("X-Client-Platform", mpPlatform)
	data, err := c.doJSON(ctx, req)
	if err != nil {
		// chat 域对该路径 400（部分任务/租户形态）→ Web 域降级（实测可领）。
		var ue *Error
		if errors.As(err, &ue) && ue.Status == http.StatusBadRequest {
			return c.ClaimReward(ctx, cred, taskCode)
		}
		return 0, 0, err
	}
	return parseClaimReward(data)
}

// parseClaimReward 解析领奖响应 data：{"already_claimed":bool,"credit":n,"energy":n}。
func parseClaimReward(data json.RawMessage) (credit, energy int64, err error) {
	var resp struct {
		AlreadyClaimed bool  `json:"already_claimed"`
		Credit         int64 `json:"credit"`
		Energy         int64 `json:"energy"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return 0, 0, err
	}
	if resp.AlreadyClaimed {
		return 0, 0, nil // 幂等：重复领取不算错误，但无新增奖励
	}
	return resp.Credit, resp.Energy, nil
}

// ---------------------------------------------------------------------------
// 猫猫旅行（growth 域，chatBase + BillingHeaders）。
// ---------------------------------------------------------------------------

// Buddy 账号当前猫档案；nil（data.buddy 为 null）表示无猫。
type Buddy struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// TravelState 猫猫旅行状态。
type TravelState struct {
	State             string `json:"state"`               // idle / traveling / arrived
	DailyLimitReached bool   `json:"daily_limit_reached"` // 今日已派出过（自然日 00:00 CST 重置）
	RecordID          int64  `json:"record_id"`           // 在途/到站记录 id，claim 必带
	RewardCredit      int64  `json:"reward_credit"`       // 到站可领奖励积分
}

// 旅行状态常量。
const (
	TravelStateIdle      = "idle"
	TravelStateTraveling = "traveling"
	TravelStateArrived   = "arrived"
)

// TravelLocationID 派出地点固定 4（古镇客栈）：4 个地点（1~4）收益/时长区间完全
// 相同，无最优解。
const TravelLocationID = 4

// TravelStatus 查询猫猫旅行状态。
func (c *Client) TravelStatus(ctx context.Context, cred Credentials) (*TravelState, error) {
	data, err := c.growthJSON(ctx, cred, http.MethodGet, travelStatusPath, nil)
	if err != nil {
		return nil, err
	}
	var st TravelState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

// TravelDepart 派出猫旅行；locationID 实测 1~4（收益/时长区间相同）。
func (c *Client) TravelDepart(ctx context.Context, cred Credentials, locationID int) error {
	_, err := c.growthJSON(ctx, cred, http.MethodPost, travelDepartPath, map[string]any{"location_id": locationID})
	return err
}

// TravelClaim 领取到站奖励（必须带 record_id），返回 reward_credit。
func (c *Client) TravelClaim(ctx context.Context, cred Credentials, recordID int64) (int64, error) {
	data, err := c.growthJSON(ctx, cred, http.MethodPost, travelClaimPath, map[string]any{"record_id": recordID})
	if err != nil {
		return 0, err
	}
	var resp struct {
		RewardCredit int64 `json:"reward_credit"`
	}
	if len(data) > 0 {
		// 奖励字段缺失不视为失败：调用方按 0 记日志即可。
		_ = json.Unmarshal(data, &resp)
	}
	return resp.RewardCredit, nil
}

// BuddyInfo 查询当前猫档案；返回 (nil, nil) 表示无猫（data.buddy 为 null）。
func (c *Client) BuddyInfo(ctx context.Context, cred Credentials) (*Buddy, error) {
	data, err := c.growthJSON(ctx, cred, http.MethodGet, buddyInfoPath, nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Buddy json.RawMessage `json:"buddy"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	// null / 缺字段 / 空对象都按无猫处理。
	trimmed := strings.TrimSpace(string(resp.Buddy))
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	var b Buddy
	if err := json.Unmarshal(resp.Buddy, &b); err != nil {
		return nil, err
	}
	return &b, nil
}

// BuddyFirst 领养第一只猫。无猫且已过 conversation 门槛时送 300 分。
// 门槛未达标返回 HTTP 400（见 IsBuddyTaskIncomplete），属预期行为。
func (c *Client) BuddyFirst(ctx context.Context, cred Credentials) error {
	_, err := c.growthJSON(ctx, cred, http.MethodPost, buddyFirstPath, map[string]any{})
	return err
}

// BuddyAgreement 同意协议（幂等，重复调用无副作用）。
func (c *Client) BuddyAgreement(ctx context.Context, cred Credentials) error {
	_, err := c.growthJSON(ctx, cred, http.MethodPost, buddyAgreementPath, map[string]any{"agree": true})
	return err
}

// IsBuddyTaskIncomplete 判定「领养门槛未达标」：HTTP 400 + first_buddy 关键词。
// 该错误当日不应重试（避免对上游重试轰炸）。
func IsBuddyTaskIncomplete(err error) bool {
	if err == nil {
		return false
	}
	var ue *Error
	if !errors.As(err, &ue) || ue.Status != http.StatusBadRequest {
		return false
	}
	return strings.Contains(strings.ToLower(ue.Msg), buddyTaskIncompleteMarker)
}

// RunTravelRound 单账号单趟旅行巡检（无状态状态机，移植自源项目
// scheduler/travel.go travelOne）：无猫 → 领养链（上报解锁 → 同意协议 → 领养）；
// 有猫 → 按 travel/status 分派领奖 / 派出 / 跳过，最多一个动作，不轮询不等待。
// 返回本次动作的可读描述（无动作返回 skip 说明）。
//
// 「领养门槛当日已试」的去重（adoptTriedToday）归调用方调度状态。
func (c *Client) RunTravelRound(ctx context.Context, cred Credentials) (string, error) {
	buddy, err := c.BuddyInfo(ctx, cred)
	if err != nil {
		return "", fmt.Errorf("buddy-info: %w", err)
	}
	if buddy == nil {
		return c.adoptBuddy(ctx, cred)
	}
	ts, err := c.TravelStatus(ctx, cred)
	if err != nil {
		return "", fmt.Errorf("travel status: %w", err)
	}
	switch ts.State {
	case TravelStateArrived:
		if ts.RecordID == 0 {
			return "skip (arrived but no record_id)", nil
		}
		reward, err := c.TravelClaim(ctx, cred, ts.RecordID)
		if err != nil {
			return "", fmt.Errorf("travel claim record=%d: %w", ts.RecordID, err)
		}
		return fmt.Sprintf("claim ok record=%d reward=%d", ts.RecordID, reward), nil
	case TravelStateIdle:
		if ts.DailyLimitReached {
			return "skip (daily limit reached)", nil
		}
		if err := c.TravelDepart(ctx, cred, TravelLocationID); err != nil {
			return "", fmt.Errorf("travel depart: %w", err)
		}
		return fmt.Sprintf("depart ok location=%d", TravelLocationID), nil
	case TravelStateTraveling:
		return fmt.Sprintf("skip (traveling record=%d)", ts.RecordID), nil
	default:
		return fmt.Sprintf("skip (unknown state %q)", ts.State), nil
	}
}

// adoptBuddy 领养链路：report（解锁前置）→ agreement → buddy/first。
// report 必须先跑：一条 chat_request_send 上报点亮 growth 连登并解锁 first_buddy
// 任务；未上报时 buddy/first 会返回 400 "first_buddy task not completed yet"——
// 该门槛的真实来源是「当日无活跃上报」，不是账号问题。
func (c *Client) adoptBuddy(ctx context.Context, cred Credentials) (string, error) {
	conv := fmt.Sprintf("wb2api-adopt-%d", c.Now().UnixMilli())
	if err := c.ReportChatActivity(ctx, cred, conv, ""); err != nil {
		return "", fmt.Errorf("adopt preflight report: %w", err)
	}
	// 给上游事件处理留时间（脚本实测的 1.05s 间隔口径）。
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-time.After(reportGap):
	}
	if err := c.BuddyAgreement(ctx, cred); err != nil {
		return "", fmt.Errorf("agreement: %w", err)
	}
	if err := c.BuddyFirst(ctx, cred); err != nil {
		if IsBuddyTaskIncomplete(err) {
			return "adopt skipped (conversation threshold not reached, retry next round)", nil
		}
		return "", fmt.Errorf("buddy first: %w", err)
	}
	return "adopt ok (+300 credits)", nil
}

// ---------------------------------------------------------------------------
// 连登与抽奖（growth 域）。
// ---------------------------------------------------------------------------

// StreakFull 连登完整状态（GET /activity/growth/streak）。
type StreakFull struct {
	Streak struct {
		Days              int    `json:"days"`
		MonthTotalDays    int    `json:"month_total_days"`
		NextTier          string `json:"next_tier"`
		NextTierRemaining int    `json:"next_tier_remaining"`
	} `json:"streak"`
	MakeupCards struct {
		Balance int `json:"balance"`
		Max     int `json:"max"`
	} `json:"makeup_cards"`
	RedemptionStatus struct {
		Tier7dStatus  string `json:"tier_7d_status"`
		Tier14dStatus string `json:"tier_14d_status"`
		Tier28dStatus string `json:"tier_28d_status"`
		RemainingDays int    `json:"remaining_days"`
		Tiers         []struct {
			Tier    string `json:"tier"`
			Days    int    `json:"days"`
			Credit  int    `json:"credit"`
			Energy  int    `json:"energy"`
			Cards   int    `json:"cards"`
			Chances int    `json:"chances"`
		} `json:"tiers"`
	} `json:"redemption_status"`
}

// GrowthStreak 查询连登天数（只读 oracle）。缺 streak/days 字段返回 0
// （days==0 即活跃自检的「上报 200 但静默丢弃」告警信号）。
func (c *Client) GrowthStreak(ctx context.Context, cred Credentials) (int, error) {
	data, err := c.growthJSON(ctx, cred, http.MethodGet, streakPath, nil)
	if err != nil {
		return 0, err
	}
	var resp struct {
		Streak struct {
			Days int `json:"days"`
		} `json:"streak"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return 0, err
	}
	return resp.Streak.Days, nil
}

// GrowthStreakFull 拉取连登完整状态（含补签卡余额与档位兑换状态）。
func (c *Client) GrowthStreakFull(ctx context.Context, cred Credentials) (*StreakFull, error) {
	data, err := c.growthJSON(ctx, cred, http.MethodGet, streakPath, nil)
	if err != nil {
		return nil, err
	}
	out := &StreakFull{}
	if err := json.Unmarshal(data, out); err != nil {
		return nil, err
	}
	return out, nil
}

// GrowthRedeemTier 兑换连登档位（tier: "7d"|"14d"|"28d"）。兑换发 credit/energy/
// 补签卡/抽奖次数；未解锁返回 *Error（HTTP 403「连续登录天数不足」）。
func (c *Client) GrowthRedeemTier(ctx context.Context, cred Credentials, tier string) error {
	_, err := c.growthJSON(ctx, cred, http.MethodPost, streakRedeemPath,
		map[string]any{"tier": tier, "client_token": clientToken()})
	return err
}

// LotteryChances 当前抽奖次数（GET /activity/growth/lottery/summary）。
func (c *Client) LotteryChances(ctx context.Context, cred Credentials) (int, error) {
	data, err := c.growthJSON(ctx, cred, http.MethodGet, lotterySummaryPath, nil)
	if err != nil {
		return 0, err
	}
	var resp struct {
		Chances int `json:"chances"`
		Module  struct {
			Enabled bool `json:"enabled"`
		} `json:"module"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return 0, err
	}
	return resp.Chances, nil
}

// LotteryDraw 抽奖一次，返回原始奖品载荷（prize 字段形状由活动期决定，透传给调用方）。
func (c *Client) LotteryDraw(ctx context.Context, cred Credentials) (json.RawMessage, error) {
	return c.growthJSON(ctx, cred, http.MethodPost, lotteryDrawPath,
		map[string]any{"client_token": clientToken()})
}

// clientToken 幂等令牌（前端 randomUUID 同款语义）。
func clientToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(b[0:4]), hex.EncodeToString(b[4:6]), hex.EncodeToString(b[6:8]),
		hex.EncodeToString(b[8:10]), hex.EncodeToString(b[10:16]))
}

// ---------------------------------------------------------------------------
// 对话活跃上报（billingBase 域 /v2/report）。
// ---------------------------------------------------------------------------

// chatRequestEvent 客户端 chat_request_send 事件完整形状（与官方 probe 脚本对齐）。
// 照抄客户端全字段（含 conversationId/mode/inputLength 等），勿用最小 3 字段，
// 防上游后续加严。事件必须带 userId（=账号 uid），缺失则服务端 200 但静默丢弃。
type chatRequestEvent struct {
	EventCode             string `json:"eventCode"`
	Timestamp             int64  `json:"timestamp"`
	ReportDelay           int    `json:"reportDelay"`
	Mode                  string `json:"mode"`
	ConversationID        string `json:"conversationId"`
	RequestID             string `json:"requestId"`
	InputLength           int    `json:"inputLength"`
	RequestModelID        string `json:"requestModelId"`
	RequestModelName      string `json:"requestModelName"`
	IsPlan                bool   `json:"isPlan"`
	IsAutoExecuteTerminal bool   `json:"isAutoExecuteTerminal"`
	IsAutoModify          bool   `json:"isAutoModify"`
	CodebaseEnable        bool   `json:"codebaseEnable"`
	MaxToken              int    `json:"maxToken"`
	MaxSteps              int    `json:"maxSteps"`
	Temperature           int    `json:"temperature"`
	MaxRetries            int    `json:"maxRetries"`
	MentionContexts       []any  `json:"mentionContexts"`
	KnowledgeID           []any  `json:"knowledgeId"`
	KnowledgeName         []any  `json:"knowledgeName"`
	CodebaseID            string `json:"codebaseId"`
	MentionContextCount   int    `json:"mentionContextCount"`
	Command               string `json:"command"`
	ExpertID              string `json:"expertId"`
	RecommendID           string `json:"recommendId"`
	SkillID               string `json:"skillId"`
	SkillCount            int    `json:"skillCount"`
	TotalCount            int    `json:"totalCount"`
	FileURI               string `json:"fileUri"`
	PresentAt             int64  `json:"presentAt"`
	TraceID               string `json:"traceId"`
	RootRequestID         string `json:"rootRequestId"`
	ParentConversationID  string `json:"parentConversationId"`
	AgentName             string `json:"agentName"`
	AgentType             string `json:"agentType"`
	UserID                string `json:"userId"`
}

// 默认上报模型（chat_5 等通用活跃上报用；体验某模型类任务用 ReportChatActivityModel
// 对齐实际模型）。
const (
	defaultReportModelID   = "deepseek-v4-flash"
	defaultReportModelName = "DeepSeek V4 Flash"
)

// ReportChatActivity 发送一条对话活跃上报（chat_request_send，默认模型）。
// conversationID 由调用方生成（如 wb2api-<ms>），无需真实会话——服务端不校验一致性。
// requestID 为本轮请求独立标识；空时回落 conversationID。
// 一条上报同时点亮 growth 连登 + 解锁 first_buddy 任务（领养前置）。
// 风控口径：每号每天 1 次即可（activity_hours 单时点），不做多时点高频上报。
func (c *Client) ReportChatActivity(ctx context.Context, cred Credentials, conversationID, requestID string) error {
	return c.ReportChatActivityModel(ctx, cred, conversationID, requestID, defaultReportModelID, defaultReportModelName)
}

// ReportChatActivityModel 同 ReportChatActivity，但可指定上报携带的模型：供「体验
// 某模型」类任务对齐实际模型（如 Model_chat_GLM5.2 需 requestModelId=glm-5.2 与
// 独立 requestID）。
func (c *Client) ReportChatActivityModel(ctx context.Context, cred Credentials, conversationID, requestID, modelID, modelName string) error {
	if requestID == "" {
		requestID = conversationID
	}
	if modelID == "" {
		modelID = defaultReportModelID
	}
	if modelName == "" {
		modelName = modelID
	}
	now := c.Now().UnixMilli()
	ev := chatRequestEvent{
		EventCode:             "chat_request_send",
		Timestamp:             now,
		ReportDelay:           0,
		Mode:                  "craft",
		ConversationID:        conversationID,
		RequestID:             requestID,
		InputLength:           12,
		RequestModelID:        modelID,
		RequestModelName:      modelName,
		IsPlan:                false,
		IsAutoExecuteTerminal: false,
		IsAutoModify:          false,
		CodebaseEnable:        false,
		MaxToken:              0,
		MaxSteps:              0,
		Temperature:           0,
		MaxRetries:            0,
		MentionContexts:       []any{},
		KnowledgeID:           []any{},
		KnowledgeName:         []any{},
		CodebaseID:            "",
		MentionContextCount:   0,
		Command:               "",
		ExpertID:              "",
		RecommendID:           "",
		SkillID:               "",
		SkillCount:            0,
		TotalCount:            0,
		FileURI:               "",
		PresentAt:             now,
		TraceID:               "",
		RootRequestID:         requestID,
		ParentConversationID:  conversationID,
		AgentName:             "default",
		AgentType:             "conversation",
		UserID:                cred.UID,
	}
	raw, err := json.Marshal([]chatRequestEvent{ev})
	if err != nil {
		return err
	}
	_, err = c.billingJSON(ctx, cred, http.MethodPost, reportPath, json.RawMessage(raw))
	return err
}

// ---------------------------------------------------------------------------
// 夜猫子（black_cat）。
// ---------------------------------------------------------------------------

// InNightWindow 当前是否处于夜猫子计数窗口（23:00–08:00 本地时区）。
// black_cat 要求在窗口内完成 3 次 glm-5.2 对话并上报 chat 事件链；窗口外行为不计分。
func InNightWindow(now time.Time) bool {
	h := now.Hour()
	return h >= 23 || h < 8
}

// BlackcatNeed 查 black_cat 任务剩余差额（需要再完成几次对话）。
// 任务不存在返回 0（无可做）；拉取失败返回错误。
func (c *Client) BlackcatNeed(ctx context.Context, cred Credentials) (int64, error) {
	tasks, err := c.ListTasks(ctx, cred)
	if err != nil {
		return 0, err
	}
	for _, t := range tasks {
		if t.TaskCode == "black_cat" {
			if t.Claimed || t.Current >= t.Target {
				return 0, nil
			}
			return t.Target - t.Current, nil
		}
	}
	return 0, nil
}

// RunNightChats 夜猫子：发 need 次 glm-5.2 真实对话（读干流）并上报事件链。
// 返回成功次数。对话内容极短（1+1），消耗可忽略。
func (c *Client) RunNightChats(ctx context.Context, cred Credentials, need int) (int64, error) {
	var ok int64
	for i := 0; i < need; i++ {
		if err := ctx.Err(); err != nil {
			return ok, err
		}
		body, _ := json.Marshal(map[string]any{
			"model":    "glm-5.2",
			"messages": []map[string]any{{"role": "user", "content": "1+1等于几？直接回答。"}},
		})
		resp, err := c.ChatStreamContext(ctx, cred, body, ChatMeta{})
		if err != nil {
			return ok, fmt.Errorf("第 %d 次对话失败: %w", i+1, err)
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if err := c.ReportChatActivityModel(ctx, cred,
			fmt.Sprintf("wb2api-night-%d-%d", c.Now().UnixMilli(), i), "", "glm-5.2", "GLM-5.2"); err != nil {
			return ok, fmt.Errorf("第 %d 次上报失败: %w", i+1, err)
		}
		ok++
		select {
		case <-ctx.Done():
			return ok, ctx.Err()
		case <-time.After(nightChatGap):
		}
	}
	return ok, nil
}

// ---------------------------------------------------------------------------
// 新手礼包 / 补偿 / 补签卡（billing 与 growth 域）。
// ---------------------------------------------------------------------------

// ClaimGift 领取新手礼包（每号一次，已领返回业务错误）。
func (c *Client) ClaimGift(ctx context.Context, cred Credentials) (int64, error) {
	data, err := c.billingJSON(ctx, cred, http.MethodPost, "/billing/meter/claim-gift", map[string]any{})
	if err != nil {
		return 0, err
	}
	var resp struct {
		Credit int64 `json:"credit"`
	}
	_ = json.Unmarshal(data, &resp)
	return resp.Credit, nil
}

// ClaimCompensation 领取活动补偿（有则领，无则业务错误）。
// 盲领入口——调用前应先 GetCompensationStatus 确认 Pending()（移植自
// workbuddy2api-panel d48b715：盲领让「没补偿/已领过/领取失败」在日志里完全同构，
// 补偿带截止日，错过静默过期）。
func (c *Client) ClaimCompensation(ctx context.Context, cred Credentials) (int64, error) {
	data, err := c.billingJSON(ctx, cred, http.MethodPost, "/billing/meter/claim-compensation", map[string]any{})
	if err != nil {
		return 0, err
	}
	var resp struct {
		Credit int64 `json:"credit"`
	}
	_ = json.Unmarshal(data, &resp)
	return resp.Credit, nil
}

// CompensationStatus 单账号的补偿活动状态（移植自 workbuddy2api-panel d48b715）。
type CompensationStatus struct {
	// Active 当前是否有生效中的补偿活动（无活动时为 false，属正常 200 返回，不是错误）。
	Active bool `json:"active"`
	// Claimed 本次活动是否已领取。
	Claimed bool `json:"claimed"`
	// CreditNum 待领额度（积分）。
	CreditNum int64 `json:"credit_num"`
	// ValidityPeriod 发放积分的有效期倍数。上游未明示单位，只透传不换算。
	ValidityPeriod int64 `json:"validity_period"`
	// EndTime 领取截止时间（上游 "2006-01-02 15:04:05" 无时区字符串，原样透传）。
	EndTime string `json:"end_time"`
}

// Pending 是否存在「可领且未领」的补偿——ClaimCompensation 的前置判据。
// 三者与门：active=false 是没有活动；claimed=true 是已领完；credit_num<=0 是
// 活动存在但无额度。三种都归「无需领取」。
func (s CompensationStatus) Pending() bool { return s.Active && !s.Claimed && s.CreditNum > 0 }

// GetCompensationStatus 查补偿活动状态。走 billingJSON（billingBase），与
// ClaimCompensation 同域同头。只在传输失败 / 非 2xx / 业务 code 非 0 时返回 error
//——active=false 是正常返回。
func (c *Client) GetCompensationStatus(ctx context.Context, cred Credentials) (CompensationStatus, error) {
	data, err := c.billingJSON(ctx, cred, http.MethodPost, "/billing/meter/compensation-status", map[string]any{})
	if err != nil {
		return CompensationStatus{}, err
	}
	var st CompensationStatus
	if err := json.Unmarshal(data, &st); err != nil {
		return CompensationStatus{}, err
	}
	return st, nil
}

// HeatmapYesterdayMissed 检查昨日是否漏签（heatmap cell score==0）。
func (c *Client) HeatmapYesterdayMissed(ctx context.Context, cred Credentials) (bool, error) {
	yesterday := c.Now().AddDate(0, 0, -1).Format("2006-01-02")
	data, err := c.growthJSON(ctx, cred, http.MethodGet, heatmapPath, nil)
	if err != nil {
		return false, err
	}
	var resp struct {
		Cells []struct {
			Date  string `json:"date"`
			Score int    `json:"score"`
		} `json:"cells"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return false, err
	}
	for _, cell := range resp.Cells {
		if len(cell.Date) >= 10 && cell.Date[:10] == yesterday {
			return cell.Score == 0, nil
		}
	}
	return false, nil
}

// UseMakeupCard 对指定日期使用补签卡（保住连登连续天数；无卡返回业务错误）。
func (c *Client) UseMakeupCard(ctx context.Context, cred Credentials, date string) error {
	_, err := c.growthJSON(ctx, cred, http.MethodPost, makeupCardUsePath, map[string]any{"target_date": date})
	return err
}

// ---------------------------------------------------------------------------
// 补领清扫（周期幂等 accept + claim，不跑任何行为链路）。
// ---------------------------------------------------------------------------

// TaskSweepResult 一轮补领清扫的汇总（移植自 workbuddy2api-panel 162762f）。
type TaskSweepResult struct {
	Accepted int   // 本轮接受成功的任务数
	Claimed  int   // 本轮领奖成功的任务数
	Credits  int64 // 本轮到账积分
	Energy   int64 // 本轮到账能量
}

// RunTaskSweep 对该账号做一轮幂等补领清扫（移植自 workbuddy2api-panel 162762f
// tasksweep.go）：只 accept 尚未接受的任务、只 claim 已达标未领的任务，**不跑任何
// 行为链路**（不真实对话、不上报事件）。
//
// 存在理由：RunGrowthAction 只覆盖内置动作任务，「经真实对话等链路点亮」的任务不
// 经过任何自动动作链路，达标后奖励无人领，积分长期卡住。判据只用 progress 口径
// Task.Claimable（!claimed && target>0 && current>=target），不用
// accept_status=="completed"（回读时会短暂转 completed 后回滚，据此领奖会误领）；
// claim 幂等（重复领 already_claimed → 0 分 0 能），宁可多试一轮也不漏领。
func (c *Client) RunTaskSweep(ctx context.Context, cred Credentials) (*TaskSweepResult, error) {
	res := &TaskSweepResult{}
	// 默认列表 + mp 列表按 task_code 去重合并（mp 任务在默认列表查不到）。
	// mpOnly 集合记录「只在 mp 列表出现」的任务码——accept/claim 都要走 mp 口径。
	tasks, err := c.ListTasks(ctx, cred)
	if err != nil {
		return res, err
	}
	seen := map[string]bool{}
	for _, t := range tasks {
		seen[t.TaskCode] = true
	}
	mpOnly := map[string]bool{}
	mpTasks, mpErr := c.ListTasksMP(ctx, cred)
	if mpErr == nil {
		for _, t := range mpTasks {
			if !seen[t.TaskCode] {
				tasks = append(tasks, t)
				seen[t.TaskCode] = true
				mpOnly[t.TaskCode] = true
			}
		}
	}

	// accept 按默认/mp 口径分开提交（端标记不同）。
	var codes, mpCodes []string
	for _, t := range tasks {
		if t.Claimed || t.Locked || t.AcceptStatus == "accepted" || t.AcceptStatus == "completed" {
			continue
		}
		if mpOnly[t.TaskCode] {
			mpCodes = append(mpCodes, t.TaskCode)
		} else {
			codes = append(codes, t.TaskCode)
		}
	}
	if len(codes) > 0 {
		if err := c.AcceptTasks(ctx, cred, codes); err == nil {
			res.Accepted += len(codes)
		}
	}
	if len(mpCodes) > 0 {
		if err := c.AcceptTasksMP(ctx, cred, mpCodes); err == nil {
			res.Accepted += len(mpCodes)
		}
	}

	// claim 只按 progress 口径（Claimable 已推算）。
	for _, t := range tasks {
		if !t.Claimable {
			continue
		}
		if err := ctx.Err(); err != nil {
			return res, err
		}
		var credit, energy int64
		var cerr error
		if mpOnly[t.TaskCode] {
			credit, energy, cerr = c.ClaimRewardMP(ctx, cred, t.TaskCode)
		} else {
			credit, energy, cerr = c.ClaimReward(ctx, cred, t.TaskCode)
		}
		if cerr != nil {
			continue // 单个失败不中断（源同口径：出错计数但不中断整轮）
		}
		res.Claimed++
		res.Credits += credit
		res.Energy += energy
	}
	return res, nil
}

// ---------------------------------------------------------------------------
// 成长任务「一键完成」框架（清单 + 接受 + 等待计分 + 自动领奖）。
// ---------------------------------------------------------------------------

// TaskByCode 拉取任务列表并定位单个任务；未找到返回 nil（不视为错误）。
// mp 参数为 true 时走小程序口径（小程序限定任务在默认列表查不到）。
func (c *Client) TaskByCode(ctx context.Context, cred Credentials, code string, mp bool) (*Task, error) {
	var (
		tasks []Task
		err   error
	)
	if mp {
		tasks, err = c.ListTasksMP(ctx, cred)
	} else {
		tasks, err = c.ListTasks(ctx, cred)
	}
	if err != nil {
		return nil, err
	}
	for i := range tasks {
		if tasks[i].TaskCode == code {
			return &tasks[i], nil
		}
	}
	return nil, nil
}

// WaitTaskClaimable 回读任务，若未达标则在有界预算内轮询等待（上游异步计分：
// 行为事件上报后进度要数秒才刷新，一次性回读会误判「未达标」而跳过自动领奖）。
// 已达标（claimable）立即返回；预算耗尽返回最后一次结果（可能仍未达标）。
func (c *Client) WaitTaskClaimable(ctx context.Context, cred Credentials, code string, mp bool) (*Task, error) {
	t, err := c.TaskByCode(ctx, cred, code, mp)
	if err != nil || t == nil {
		return t, err
	}
	if t.Claimable || t.Claimed {
		return t, nil
	}
	for i := 1; i < claimPollAttempts; i++ {
		select {
		case <-ctx.Done():
			return t, ctx.Err()
		case <-time.After(claimPollGap):
		}
		t2, err2 := c.TaskByCode(ctx, cred, code, mp)
		if err2 != nil {
			return t, nil // 轮询期间的查询失败不覆盖已拿到的结果
		}
		if t2 != nil {
			t = t2
			if t.Claimable || t.Claimed {
				return t, nil
			}
		}
	}
	return t, nil
}

// AcceptPendingGrowthTasks 批量接受该账号尚未接受的任务（跳过已 accepted/completed/
// claimed 与 locked 的），返回接受的个数。accept 不产生进度（进度靠行为事件点亮），
// 但让状态机规范（not_accepted → accepted → completed），是上报计分的前置。
func (c *Client) AcceptPendingGrowthTasks(ctx context.Context, cred Credentials) (int, error) {
	tasks, err := c.ListTasks(ctx, cred)
	if err != nil {
		return 0, err
	}
	var codes []string
	for _, t := range tasks {
		if t.Claimed || t.Locked || t.AcceptStatus == "accepted" || t.AcceptStatus == "completed" {
			continue
		}
		codes = append(codes, t.TaskCode)
	}
	if len(codes) == 0 {
		return 0, nil
	}
	if err := c.AcceptTasks(ctx, cred, codes); err != nil {
		return 0, err
	}
	// 小程序口径任务单独接受（默认列表不含 mp 码，accept 也要求 mp 头）。
	if mpTasks, mpErr := c.ListTasksMP(ctx, cred); mpErr == nil {
		var mpCodes []string
		seen := map[string]bool{}
		for _, t := range tasks {
			seen[t.TaskCode] = true
		}
		for _, t := range mpTasks {
			if seen[t.TaskCode] || t.Claimed || t.Locked || t.AcceptStatus == "accepted" || t.AcceptStatus == "completed" {
				continue
			}
			mpCodes = append(mpCodes, t.TaskCode)
		}
		if len(mpCodes) > 0 {
			if err := c.AcceptTasksMP(ctx, cred, mpCodes); err == nil {
				return len(codes) + len(mpCodes), nil
			}
		}
	}
	return len(codes), nil
}

// CompleteChatReportTask 完成 chat_5（每日 5 条对话上报）类任务：按差额补报
// chat_request_send 事件。target 缺省按 5 兜底。返回补报条数。
// 上报间隔 reportGap（1.05s，脚本实测口径，避免风控）。
func (c *Client) CompleteChatReportTask(ctx context.Context, cred Credentials) (int64, error) {
	const code = "chat_5"
	t, err := c.TaskByCode(ctx, cred, code, false)
	if err != nil {
		return 0, err
	}
	if t == nil {
		return 0, fmt.Errorf("任务 %s 不存在", code)
	}
	target := t.Target
	if target <= 0 {
		target = 5
	}
	need := target - t.Current
	if need <= 0 {
		return 0, nil
	}
	for i := int64(0); i < need; i++ {
		if err := ctx.Err(); err != nil {
			return i, err
		}
		cid := fmt.Sprintf("wb2api-chat5-%d-%d", c.Now().UnixMilli(), i)
		if err := c.ReportChatActivity(ctx, cred, cid, ""); err != nil {
			return i, fmt.Errorf("上报第 %d/%d 条失败: %w", i+1, need, err)
		}
		if i < need-1 {
			select {
			case <-ctx.Done():
				return i + 1, ctx.Err()
			case <-time.After(reportGap):
			}
		}
	}
	return need, nil
}

// GrowthActionResult 单个成长任务「动作 + 回读 + 自动领奖」的完整结果。
type GrowthActionResult struct {
	TaskCode string // 任务码
	Message  string // 动作结果描述
	Credit   int64  // 自动领奖到账积分（0 = 无/已领过）
	Energy   int64  // 自动领奖到账能量
	Claimed  bool   // 是否触发了自动领奖
}

// RunGrowthAction 执行单个任务的「动作 → 等待计分 → 自动领奖」闭环（mp 任务走
// mp 口径回读与领奖）。本包内置动作覆盖纯上报类任务（chat_5 / first_buddy /
// black_cat）；其余任务（模板/专家/皮肤等桌面事件链）的判据事件未移植，调用方可
// 自行上报事件后用 WaitTaskClaimable + ClaimReward 复用本闭环的后半段。
func (c *Client) RunGrowthAction(ctx context.Context, cred Credentials, code string, mp bool) (*GrowthActionResult, error) {
	res := &GrowthActionResult{TaskCode: code}
	before, err := c.TaskByCode(ctx, cred, code, mp)
	if err != nil {
		return nil, err
	}
	if before == nil {
		res.Message = "该账号无此任务"
		return res, nil
	}
	if before.Claimed {
		res.Message = "已完成（已领取）"
		return res, nil
	}
	// 内置动作分派。
	switch code {
	case "chat_5":
		n, err := c.CompleteChatReportTask(ctx, cred)
		if err != nil {
			return res, err
		}
		res.Message = fmt.Sprintf("已补报 %d 条对话事件", n)
	case "first_buddy":
		msg, err := c.adoptBuddy(ctx, cred)
		if err != nil {
			return res, err
		}
		res.Message = msg
	case "black_cat":
		if !InNightWindow(c.Now()) {
			res.Message = "当前不在 23:00–08:00 计数窗口，行为不计分"
			return res, nil
		}
		need, err := c.BlackcatNeed(ctx, cred)
		if err != nil {
			return res, err
		}
		if need <= 0 {
			res.Message = "进度已达标，无需补足"
			return res, nil
		}
		okN, err := c.RunNightChats(ctx, cred, int(need))
		if err != nil {
			res.Message = fmt.Sprintf("完成 %d/%d 次后中断: %v", okN, need, err)
			return res, nil
		}
		res.Message = fmt.Sprintf("已完成 %d 次夜间对话并上报", okN)
	default:
		// 无内置动作：若已达标则直接走领奖，否则交调用方上报判据事件后重入。
		if !before.Claimable {
			res.Message = "该任务无内置动作（需调用方上报判据事件）"
			return res, nil
		}
		res.Message = "进度已达标"
	}
	// 回读等待异步计分落定。
	after, err := c.WaitTaskClaimable(ctx, cred, code, mp)
	if err != nil || after == nil {
		return res, nil
	}
	res.Message += fmt.Sprintf("（进度 %d/%d）", after.Current, after.Target)
	if after.Claimable {
		var credit, energy int64
		var cerr error
		if mp {
			credit, energy, cerr = c.ClaimRewardMP(ctx, cred, code)
		} else {
			credit, energy, cerr = c.ClaimReward(ctx, cred, code)
		}
		if cerr == nil {
			res.Claimed = true
			res.Credit, res.Energy = credit, energy
		} else {
			res.Message += "；达标但领奖失败: " + cerr.Error()
		}
	}
	return res, nil
}
