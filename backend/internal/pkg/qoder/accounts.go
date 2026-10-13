package qoder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Client Qoder openapi 域 HTTP 客户端（http.Client 注入，纯标准库）。
type Client struct {
	httpClient *http.Client
}

// NewClient 创建客户端；httpClient 为 nil 时用 http.DefaultClient。
func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{httpClient: httpClient}
}

func (c *Client) http() *http.Client {
	if c == nil || c.httpClient == nil {
		return http.DefaultClient
	}
	return c.httpClient
}

// StatusError 非 2xx 响应（对齐源实现里 urllib HTTPError 的语义：
// 调用方按 code 分支，body 为错误详情）。
type StatusError struct {
	Code int
	Body string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("qoder: HTTP %d: %s", e.Code, clip(e.Body, 200))
}

// doJSON 发送 JSON 请求并解析 JSON 响应（单次尝试；重试由调用方治理）。
func (c *Client) doJSON(ctx context.Context, method, url string, body []byte,
	headers http.Header, out any) error {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return err
	}
	req.Header = headers
	resp, err := c.http().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &StatusError{Code: resp.StatusCode, Body: string(data)}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("qoder: decode response: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Credentials 契约（sub2api credentials map[string]string，json tag 下划线）
// ---------------------------------------------------------------------------

// Credentials 渠道凭证字段。access_token 前缀决定凭证族：
// dt-/drt- 为 OAuth 设备族、jt-/jrt- 为 jobToken 族、pt- 为 PAT。
// expires_at 为 unix 秒的十进制字符串；machine_token 为空时由指纹派生。
type Credentials struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	TokenKind        string `json:"token_kind"` // device|job|pat
	ExpiresAt        string `json:"expires_at"`
	Realm            string `json:"realm"` // cn|intl
	UID              string `json:"uid"`
	Nickname         string `json:"nickname"`
	UserType         string `json:"user_type"`
	OrganizationID   string `json:"organization_id"`
	OrganizationName string `json:"organization_name"`
	MachineID        string `json:"machine_id"`
	MachineToken     string `json:"machine_token"`
}

// ParseCredentials 从 sub2api 渠道 credentials map 解析。
func ParseCredentials(m map[string]string) Credentials {
	return Credentials{
		AccessToken:      m["access_token"],
		RefreshToken:     m["refresh_token"],
		TokenKind:        m["token_kind"],
		ExpiresAt:        m["expires_at"],
		Realm:            m["realm"],
		UID:              m["uid"],
		Nickname:         m["nickname"],
		UserType:         m["user_type"],
		OrganizationID:   m["organization_id"],
		OrganizationName: m["organization_name"],
		MachineID:        m["machine_id"],
		MachineToken:     m["machine_token"],
	}
}

// ToMap 序列化回 sub2api 渠道 credentials map（值均为 string）。
func (c Credentials) ToMap() map[string]string {
	return map[string]string{
		"access_token":      c.AccessToken,
		"refresh_token":     c.RefreshToken,
		"token_kind":        c.TokenKind,
		"expires_at":        c.ExpiresAt,
		"realm":             c.Realm,
		"uid":               c.UID,
		"nickname":          c.Nickname,
		"user_type":         c.UserType,
		"organization_id":   c.OrganizationID,
		"organization_name": c.OrganizationName,
		"machine_id":        c.MachineID,
		"machine_token":     c.MachineToken,
	}
}

// NormalizedRealm 规范化区域名（未知/空回落 cn）。
func (c Credentials) NormalizedRealm() string {
	if ValidRealm(c.Realm) {
		return c.Realm
	}
	return "cn"
}

// Identity 由凭证构造 COSY 会话身份。
func (c Credentials) Identity() Identity {
	userType := c.UserType
	if userType == "" {
		userType = DefaultUserType
	}
	return Identity{
		UID:              c.UID,
		Nickname:         c.Nickname,
		AccessToken:      c.AccessToken,
		RefreshToken:     c.RefreshToken,
		UserType:         userType,
		OrganizationID:   c.OrganizationID,
		OrganizationName: c.OrganizationName,
	}
}

// ---------------------------------------------------------------------------
// 出站头
// ---------------------------------------------------------------------------

// OpenAPIHeaders openapi 域纯 Bearer 出站头（qoder_accounts.py L1646-1658）。
func (c Credentials) OpenAPIHeaders() http.Header {
	cfg := Realm(c.NormalizedRealm())
	h := http.Header{}
	h.Set("Content-Type", "application/json")
	h.Set("Accept", "application/json, text/plain, */*")
	h.Set("User-Agent", ClientUA)
	h.Set("Authorization", "Bearer "+c.AccessToken)
	h.Set("X-Request-ID", GenerateRequestID(c.UID))
	h.Set("X-Machine-ID", DeriveID(c.UID, "machine"))
	h.Set("X-Session-ID", DeriveID(c.UID, "session"))
	h.Set("Origin", cfg.Website)
	h.Set("Referer", cfg.Website+"/")
	return h
}

// DesktopHeaders 桌面端同款出站头（活动平台 /sash/... 必需，
// qoder_accounts.py L1660-1710）。
//
// issue #10 实测（Linux/Docker）：全套**派生** cosy-machine* 六头会被服务端
// 判定为非官方客户端、整条过滤 CLAIMABLE 活动；因此无原生桥真身份时
// **一律不发**机器头，只发 UA / cosy-clienttype / cosy-version。服务器场景
// 没有原生桥——仅当凭证里存有真实 machine_token（从官方客户端导入）时
// 才发送全套六头。
func (c Credentials) DesktopHeaders() http.Header {
	h := c.OpenAPIHeaders()
	h.Set("User-Agent", "Qoder")
	h.Set("cosy-clienttype", DesktopClientType)
	h.Set("cosy-version", DesktopClientVersion)
	if c.MachineToken != "" {
		machineID := c.MachineID
		if machineID == "" {
			machineID = DeriveID(c.UID, "machine")
		}
		h.Set("cosy-machineid", machineID)
		h.Set("cosy-machinetoken", c.MachineToken)
		h.Set("cosy-machinetype", DeriveMachineType(c.UID))
		h.Set("cosy-machinecode", DeriveID(c.UID, "machinecode"))
		h.Set("cosy-machineos", MachineOS)
		h.Set("cosy-machinehostname", MachineHostname)
	}
	return h
}

// ---------------------------------------------------------------------------
// 业务动作（全部挂 openapi 基址）
// ---------------------------------------------------------------------------

// UserInfo /api/v1/userinfo 的身份字段（qoder_accounts.py poll_login /
// import_pat 消费的字段子集）。
type UserInfo struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	UserType         string `json:"user_type"`
	OrganizationID   string `json:"organization_id"`
	OrganizationName string `json:"organization_name"`
}

// Userinfo 拉取账号身份（GET /api/v1/userinfo）。
func (c *Client) Userinfo(ctx context.Context, creds Credentials) (*UserInfo, error) {
	cfg := Realm(creds.NormalizedRealm())
	h := http.Header{}
	h.Set("Accept", "application/json")
	h.Set("User-Agent", ClientUA)
	h.Set("Authorization", "Bearer "+creds.AccessToken)
	var out UserInfo
	if err := c.doJSON(ctx, http.MethodGet, cfg.OpenAPI+PathUserinfo, nil, h, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// QuotaPackage 单个额度包（基础额度 / 赠送签到额度）。
type QuotaPackage struct {
	Name        string `json:"name"`
	Remain      int    `json:"remain"`
	Used        int    `json:"used"`
	Size        int    `json:"size"`
	ExpiresAt   int64  `json:"expires_at,omitempty"`
	DaysLeft    any    `json:"days_left,omitempty"`
	NoExpiry    *bool  `json:"no_expiry,omitempty"`
	PackageCode string `json:"package_code,omitempty"`
}

// QuotaUsage /api/v2/quota/usage 聚合结果（fetch_credits L2316-2385）。
type QuotaUsage struct {
	Remain    int            `json:"remain"`
	Used      int            `json:"used"`
	Size      int            `json:"size"`
	Exceeded  bool           `json:"exceeded"`
	UsagePct  any            `json:"usage_pct"`
	ExpiresAt int64          `json:"expires_at"`
	Packages  []QuotaPackage `json:"packages"`
}

type quotaUsageRaw struct {
	UserQuota            map[string]any `json:"userQuota"`
	AddOnQuota           map[string]any `json:"addOnQuota"`
	IsQuotaExceeded      bool           `json:"isQuotaExceeded"`
	TotalUsagePercentage any            `json:"totalUsagePercentage"`
	ExpiresAt            any            `json:"expiresAt"`
}

// QuotaUsage 拉取并聚合额度（GET /api/v2/quota/usage：基础 + 赠送/签到）。
func (c *Client) QuotaUsage(ctx context.Context, creds Credentials) (*QuotaUsage, error) {
	cfg := Realm(creds.NormalizedRealm())
	var raw quotaUsageRaw
	if err := c.doJSON(ctx, http.MethodGet, cfg.OpenAPI+PathQuotaUsage, nil,
		creds.OpenAPIHeaders(), &raw); err != nil {
		return nil, err
	}
	num := func(d map[string]any, k string) float64 {
		if d == nil {
			return 0
		}
		switch v := d[k].(type) {
		case float64:
			return v
		case string:
			f, _ := strconv.ParseFloat(v, 64)
			return f
		default:
			return 0
		}
	}
	uq, aq := raw.UserQuota, raw.AddOnQuota
	out := &QuotaUsage{
		Remain:    int(num(uq, "remaining") + num(aq, "remaining")),
		Used:      int(num(uq, "used") + num(aq, "used")),
		Size:      int(num(uq, "total") + num(aq, "total")),
		Exceeded:  raw.IsQuotaExceeded,
		UsagePct:  raw.TotalUsagePercentage,
		ExpiresAt: normalizeEpoch(raw.ExpiresAt),
		Packages: []QuotaPackage{
			{Name: "基础额度", Remain: int(num(uq, "remaining")), Used: int(num(uq, "used")), Size: int(num(uq, "total"))},
			{Name: "赠送/签到额度", Remain: int(num(aq, "remaining")), Used: int(num(aq, "used")), Size: int(num(aq, "total"))},
		},
	}
	return out, nil
}

// normalizeEpoch 上游时间值归一化为 unix 秒（qoder_accounts.py L831-838：
// >1e11 视为毫秒）。
func normalizeEpoch(v any) int64 {
	var f float64
	switch t := v.(type) {
	case float64:
		f = t
	case string:
		p, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		if err != nil {
			return 0
		}
		f = p
	case int64:
		f = float64(t)
	case int:
		f = float64(t)
	default:
		return 0
	}
	if f > 1e11 {
		f /= 1000.0
	}
	return int64(f)
}

// UserPlan 套餐名（GET /api/v2/user/plan -> plan_tier_name / user_type）。
func (c *Client) UserPlan(ctx context.Context, creds Credentials) (string, error) {
	cfg := Realm(creds.NormalizedRealm())
	var out struct {
		PlanTierName string `json:"plan_tier_name"`
		UserType     string `json:"user_type"`
	}
	if err := c.doJSON(ctx, http.MethodGet, cfg.OpenAPI+PathUserPlan, nil,
		creds.OpenAPIHeaders(), &out); err != nil {
		return "", err
	}
	if out.PlanTierName != "" {
		return out.PlanTierName, nil
	}
	return out.UserType, nil
}

// CheckinStatus 每日签到状态（GET /sash/api/v1/me/daily-check-in/status）。
// 接口在本区域不存在（404/405/410，实测国际版即如此）时返回
// ErrCheckinUnavailable；上游按 lastClaimedAt 是否为今日判断今天是否已签。
var ErrCheckinUnavailable = fmt.Errorf("qoder: checkin endpoint not found in this realm")

// CheckinStatusResult 签到状态归一化结果（checkin_status L1832-1882）。
type CheckinStatusResult struct {
	Status             string `json:"status"`
	Active             bool   `json:"active"`
	TodayCheckedIn     bool   `json:"today_checked_in"`
	StreakDays         int    `json:"streak_days"`
	TotalClaimDays     int    `json:"total_claim_days"`
	RewardCredits      int    `json:"reward_credits"`
	TotalRewardCredits int    `json:"total_reward_credits"`
	NextClaimAt        int64  `json:"next_claim_at"`
	LastClaimedAt      int64  `json:"last_claimed_at"`
	RewardExpiresAt    int64  `json:"reward_expires_at"`
}

// CheckinStatus 查询每日签到状态。
func (c *Client) CheckinStatus(ctx context.Context, creds Credentials) (*CheckinStatusResult, error) {
	cfg := Realm(creds.NormalizedRealm())
	var q struct {
		Status             string `json:"status"`
		CurrentStreakDays  int    `json:"currentStreakDays"`
		TotalClaimDays     int    `json:"totalClaimDays"`
		RewardCredits      int    `json:"rewardCredits"`
		TotalRewardCredits int    `json:"totalRewardCredits"`
		NextClaimAt        int64  `json:"nextClaimAt"`
		LastClaimedAt      int64  `json:"lastClaimedAt"`
		RewardExpiresAt    int64  `json:"rewardExpiresAt"`
	}
	err := c.doJSON(ctx, http.MethodGet, cfg.OpenAPI+PathCheckinStatus, nil,
		creds.OpenAPIHeaders(), &q)
	if err != nil {
		if se, ok := err.(*StatusError); ok &&
			(se.Code == 404 || se.Code == 405 || se.Code == 410) {
			return nil, ErrCheckinUnavailable
		}
		return nil, err
	}
	st := &CheckinStatusResult{
		Status:             q.Status,
		Active:             q.Status == "CLAIMABLE" || q.Status == "CLAIMED",
		StreakDays:         q.CurrentStreakDays,
		TotalClaimDays:     q.TotalClaimDays,
		RewardCredits:      q.RewardCredits,
		TotalRewardCredits: q.TotalRewardCredits,
		NextClaimAt:        q.NextClaimAt,
		LastClaimedAt:      q.LastClaimedAt,
		RewardExpiresAt:    q.RewardExpiresAt,
	}
	st.TodayCheckedIn = q.Status == "CLAIMED" &&
		time.Unix(q.LastClaimedAt, 0).Format("2006-01-02") == time.Now().Format("2006-01-02")
	return st, nil
}

// CheckinResult 每日签到动作结果。
type CheckinResult struct {
	OK            bool   `json:"ok"`
	Already       bool   `json:"already,omitempty"`
	Unavailable   bool   `json:"unavailable,omitempty"`
	RewardCredits int    `json:"reward_credits,omitempty"`
	Message       string `json:"message"`
}

// Checkin 每日签到：先查状态，未签则领取（checkin L1884-1937）。
// 409 ALREADY_CLAIMED 归一化为"今日已签到"。
func (c *Client) Checkin(ctx context.Context, creds Credentials) (*CheckinResult, error) {
	st, err := c.CheckinStatus(ctx, creds)
	if err != nil {
		if err == ErrCheckinUnavailable {
			return &CheckinResult{OK: true, Unavailable: true,
				Message: "本区域未开放 /sash/api/v1/me/daily-check-in 接口"}, nil
		}
		return nil, err
	}
	if st.TodayCheckedIn {
		return &CheckinResult{OK: true, Already: true, Message: "今日已签到",
			RewardCredits: st.RewardCredits}, nil
	}
	if !st.Active {
		// CLAIMABLE / CLAIMED 之外的状态（如 DISABLED：活动批次下线），
		// 不发起无意义的 claim，按"活动未开放"成功跳过。
		return &CheckinResult{OK: true, Message: fmt.Sprintf("官方签到活动未开放 (status=%s)", st.Status)}, nil
	}
	cfg := Realm(creds.NormalizedRealm())
	var res struct {
		Success       *bool  `json:"success"`
		Error         string `json:"error"`
		RewardCredits int    `json:"rewardCredits"`
		Result        string `json:"result"`
	}
	err = c.doJSON(ctx, http.MethodPost, cfg.OpenAPI+PathCheckinClaim, []byte("{}"),
		creds.OpenAPIHeaders(), &res)
	if err != nil {
		if se, ok := err.(*StatusError); ok {
			if se.Code == 409 || strings.Contains(se.Body, "ALREADY_CLAIMED") {
				return &CheckinResult{OK: true, Already: true, Message: "今日已签到"}, nil
			}
			return nil, err
		}
		return nil, err
	}
	if res.Result == "ALREADY_CLAIMED" || (res.Success != nil && !*res.Success &&
		strings.Contains(res.Error, "ALREADY")) {
		return &CheckinResult{OK: true, Already: true, Message: "今日已签到"}, nil
	}
	if res.Success != nil && !*res.Success {
		// 复查一次：上游可能已记账
		if st2, err2 := c.CheckinStatus(ctx, creds); err2 == nil && st2.TodayCheckedIn {
			return &CheckinResult{OK: true, Already: true, Message: "今日已签到（复查确认）"}, nil
		}
		return nil, fmt.Errorf("qoder: checkin failed: %s", clip(res.Error, 160))
	}
	return &CheckinResult{OK: true, Message: fmt.Sprintf("签到成功 +%d 积分", res.RewardCredits),
		RewardCredits: res.RewardCredits}, nil
}

// ProEligibility Pro 升级包资格（GET /sash/api/v1/me/pro-upgrade/eligibility）。
// 端点不存在 / 活动已下线（404/403/410）时返回 (false, nil)。
func (c *Client) ProEligibility(ctx context.Context, creds Credentials) (bool, error) {
	cfg := Realm(creds.NormalizedRealm())
	var out struct {
		Eligible bool `json:"eligible"`
	}
	err := c.doJSON(ctx, http.MethodGet, cfg.OpenAPI+PathProEligibility, nil,
		creds.OpenAPIHeaders(), &out)
	if err != nil {
		if se, ok := err.(*StatusError); ok &&
			(se.Code == 404 || se.Code == 403 || se.Code == 410) {
			return false, nil
		}
		return false, err
	}
	return out.Eligible, nil
}

// ProClaim 领取 Pro 升级包（POST /sash/api/v1/me/pro-upgrade/claim，body {}）。
// 409 / ALREADY 归一化为已领取（pro_claim L2419-2437）。
func (c *Client) ProClaim(ctx context.Context, creds Credentials) (*CheckinResult, error) {
	cfg := Realm(creds.NormalizedRealm())
	var res struct {
		Success *bool  `json:"success"`
		Message string `json:"message"`
	}
	err := c.doJSON(ctx, http.MethodPost, cfg.OpenAPI+PathProClaim, []byte("{}"),
		creds.OpenAPIHeaders(), &res)
	if err != nil {
		if se, ok := err.(*StatusError); ok {
			if se.Code == 409 || strings.Contains(se.Body, "ALREADY") {
				return &CheckinResult{OK: true, Already: true, Message: "Pro 升级包已领取过"}, nil
			}
		}
		return nil, err
	}
	if res.Success != nil && !*res.Success {
		msg := res.Message
		if msg == "" {
			msg = "unknown"
		}
		return nil, fmt.Errorf("qoder: pro claim failed: %s", clip(msg, 160))
	}
	return &CheckinResult{OK: true, Message: "Pro 升级包领取成功"}, nil
}

// CampaignItem 官方活动条目（campaigns L2014-2057 的归一化字段）。
type CampaignItem struct {
	TitleZh                string          `json:"title_zh"`
	TitleEn                string          `json:"title_en"`
	DescZh                 string          `json:"desc_zh"`
	DetailURL              string          `json:"detail_url"`
	ButtonText             string          `json:"button_text"`
	CampaignID             string          `json:"campaign_id"`
	CampaignKey            string          `json:"campaign_key"`
	ActionType             string          `json:"action_type"`
	ClaimStatus            string          `json:"claim_status"`
	StartAt                int64           `json:"start_at"`
	EndAt                  int64           `json:"end_at"`
	BenefitKind            string          `json:"benefit_kind"`
	BenefitAmount          int             `json:"benefit_amount"`
	RequiredAchievementKey string          `json:"required_achievement_key"`
	AchievementCompleted   bool            `json:"achievement_completed"`
	UnavailableReason      string          `json:"unavailable_reason"`
	Placements             json.RawMessage `json:"placements,omitempty"`
}

// CampaignsStatus 活动平台状态（campaigns L1967-2076）。
type CampaignsStatus struct {
	OK             bool           `json:"ok"`
	Available      bool           `json:"available"`
	ShowCampaign   bool           `json:"show_campaign"`
	Claimable      bool           `json:"claimable"`
	CampaignURL    string         `json:"campaign_url"`
	Campaigns      []CampaignItem `json:"campaigns"`
	MachineHeaders string         `json:"machine_headers"` // native|omitted
	Error          string         `json:"error,omitempty"`
}

// Campaigns 拉取官方活动平台列表（GET /sash/api/v1/me/campaigns，双区域通用）。
// 桌面端头被拒（401/403）时回退普通头（_campaigns_get L1939-1965）。
func (c *Client) Campaigns(ctx context.Context, creds Credentials) (*CampaignsStatus, error) {
	cfg := Realm(creds.NormalizedRealm())
	url := cfg.OpenAPI + PathCampaigns
	var raw json.RawMessage
	err := c.doJSON(ctx, http.MethodGet, url, nil, creds.DesktopHeaders(), &raw)
	if err != nil {
		// 桌面端头被拒（401/403）时回退普通头，至少保留"能不能看到"的信息
		if se, ok := err.(*StatusError); ok && (se.Code == 401 || se.Code == 403) {
			err = c.doJSON(ctx, http.MethodGet, url, nil, creds.OpenAPIHeaders(), &raw)
		}
	}
	if err != nil {
		if se, ok := err.(*StatusError); ok &&
			(se.Code == 404 || se.Code == 405 || se.Code == 410) {
			return &CampaignsStatus{Available: false, Error: se.Error()}, nil
		}
		return &CampaignsStatus{Available: true, Error: err.Error()}, nil
	}
	st := &CampaignsStatus{MachineHeaders: "omitted"}
	if creds.MachineToken != "" {
		st.MachineHeaders = "native"
	}
	var q struct {
		ShowCampaign bool             `json:"showCampaign"`
		Claimable    bool             `json:"claimable"`
		CampaignURL  string           `json:"campaignUrl"`
		Campaigns    []map[string]any `json:"campaigns"`
	}
	if err := json.Unmarshal(raw, &q); err != nil {
		return &CampaignsStatus{Available: true, Error: err.Error()}, nil
	}
	st.OK = true
	st.Available = true
	st.ShowCampaign = q.ShowCampaign
	st.Claimable = q.Claimable
	st.CampaignURL = q.CampaignURL
	for _, item := range q.Campaigns {
		st.Campaigns = append(st.Campaigns, normalizeCampaignItem(item))
	}
	return st, nil
}

// normalizeCampaignItem 官方条目 -> 展示字段（campaigns L2022-2057：
// placements[].content.zh/en 的标题/说明/详情页/按钮，benefit.kind/amount）。
func normalizeCampaignItem(c map[string]any) CampaignItem {
	item := CampaignItem{
		CampaignID:  strOf(c, "campaignId", "campaign_id"),
		CampaignKey: strOf(c, "campaignKey", "campaign_key"),
		ActionType:  strOf(c, "actionType", "action_type"),
		ClaimStatus: strOf(c, "claimStatus", "claim_status"),
		StartAt:     normalizeEpoch(firstOf(c, "startAt", "start_at")),
		EndAt:       normalizeEpoch(firstOf(c, "endAt", "end_at")),
	}
	if v, ok := c["requiredAchievementKey"].(string); ok {
		item.RequiredAchievementKey = v
	}
	if v, ok := c["achievementCompleted"].(bool); ok {
		item.AchievementCompleted = v
	}
	item.UnavailableReason = strOf(c, "unavailableReason")
	if benefit, ok := c["benefit"].(map[string]any); ok {
		item.BenefitKind, _ = benefit["kind"].(string)
		item.BenefitAmount = toIntAny(benefit["amount"])
	}
	if placements, ok := c["placements"].([]any); ok {
		if raw, err := json.Marshal(placements); err == nil {
			item.Placements = raw
		}
		for _, p := range placements {
			pm, ok := p.(map[string]any)
			if !ok {
				continue
			}
			content, _ := pm["content"].(map[string]any)
			if content == nil {
				continue
			}
			zh, _ := content["zh"].(map[string]any)
			en, _ := content["en"].(map[string]any)
			if item.TitleZh == "" {
				item.TitleZh, _ = zh["title"].(string)
			}
			if item.TitleEn == "" {
				item.TitleEn, _ = en["title"].(string)
			}
			if item.DescZh == "" {
				item.DescZh, _ = zh["description"].(string)
			}
			if item.DetailURL == "" {
				du, _ := zh["detailUrl"].(string)
				if du == "" {
					du, _ = en["detailUrl"].(string)
				}
				item.DetailURL = du
			}
			if item.ButtonText == "" {
				bt, _ := zh["buttonText"].(string)
				if bt == "" {
					bt, _ = en["buttonText"].(string)
				}
				item.ButtonText = bt
			}
			if item.TitleZh != "" && item.DescZh != "" && item.DetailURL != "" {
				break
			}
		}
	}
	return item
}

func strOf(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// toIntAny 上游数值字段（float64 / 字符串数字）转 int。
func toIntAny(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(t))
		return n
	default:
		return 0
	}
}

func firstOf(m map[string]any, keys ...string) any {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			return v
		}
	}
	return nil
}

// CampaignClaimResult 活动领取结果（claim_campaign L2088-2158 的归一化）。
type CampaignClaimResult struct {
	OK             bool   `json:"ok"`
	Status         string `json:"status"`
	Replayed       bool   `json:"replayed"`
	Blocked        bool   `json:"blocked,omitempty"`
	FailureCode    string `json:"failure_code"`
	GrantID        string `json:"grant_id"`
	RedemptionCode string `json:"redemption_code"`
	Amount         int    `json:"amount"`
	Message        string `json:"message"`
}

// campaignFailureCN 领取失败码的中文解释（qoder_accounts.py L784-794）。
var campaignFailureCN = map[string]string{
	"NOT_ELIGIBLE":                 "当前账号不满足领取条件",
	"ALREADY_CLAIMED":              "本轮已领取过",
	"REDEMPTION_CODE_OUT_OF_STOCK": "兑换码名额已发完，次日再试",
	"ACHIEVEMENT_NOT_COMPLETED":    "需先完成新人任务（成就未完成）",
	"CAMPAIGN_NOT_ACTIVE":          "活动已结束/未开始",
	"RISK_BLOCKED":                 "风控拦截（当前设备/账号不可领取）",
	"RISK_DEPENDENCY_UNAVAILABLE":  "风控服务不可用，稍后重试",
}

// ClaimCampaign 领取单个活动奖励（POST …/campaigns/{id}/claim，官方幂等语义：
// 已领取返回 CLAIMED+replayed；同机器身份下其他账号已领返回 BLOCKED+
// SAME_PERSON_ALREADY_CLAIMED，服务端按"人"去重）。
func (c *Client) ClaimCampaign(ctx context.Context, creds Credentials, campaignID string) (*CampaignClaimResult, error) {
	cfg := Realm(creds.NormalizedRealm())
	var r struct {
		Status         string          `json:"status"`
		FailureCode    string          `json:"failureCode"`
		Replayed       bool            `json:"replayed"`
		GrantID        string          `json:"grantId"`
		RedemptionCode string          `json:"redemptionCode"`
		Amount         int             `json:"amount"`
		Benefit        json.RawMessage `json:"benefit"`
		Success        *bool           `json:"success"`
		Error          string          `json:"error"`
	}
	err := c.doJSON(ctx, http.MethodPost, cfg.OpenAPI+CampaignClaimPath(campaignID),
		[]byte("{}"), creds.DesktopHeaders(), &r)
	if err != nil {
		if se, ok := err.(*StatusError); ok {
			code := ""
			var eb struct {
				ErrorCode string `json:"errorCode"`
			}
			_ = json.Unmarshal([]byte(se.Body), &eb)
			code = eb.ErrorCode
			upper := strings.ToUpper(code)
			if se.Code == 409 || upper == "ALREADY_CLAIMED" || upper == "REPLAYED" {
				return &CampaignClaimResult{OK: true, Status: "CLAIMED", Replayed: true,
					Message: "今日已领取（上游幂等确认）"}, nil
			}
			return nil, err
		}
		return nil, err
	}
	status := strings.ToUpper(r.Status)
	failure := strings.ToUpper(r.FailureCode)
	amount := r.Amount
	if len(r.Benefit) > 0 {
		var b struct {
			Amount int `json:"amount"`
		}
		if json.Unmarshal(r.Benefit, &b) == nil && b.Amount > 0 {
			amount = b.Amount
		}
	}
	if failure == "SAME_PERSON_ALREADY_CLAIMED" || status == "BLOCKED" {
		if failure == "" {
			failure = "BLOCKED"
		}
		return &CampaignClaimResult{Blocked: true, Status: status, FailureCode: failure,
			Amount: amount, Message: "同人已领取（同一设备/身份下其他账号本轮已领，服务端按人去重）"}, nil
	}
	if status == "" && r.Success != nil && !*r.Success {
		msg := r.Error
		if msg == "" {
			msg = "unknown"
		}
		return nil, fmt.Errorf("qoder: campaign claim failed: %s", clip(msg, 160))
	}
	code := strings.TrimSpace(r.RedemptionCode)
	if msg, ok := campaignFailureCN[failure]; ok {
		if status == "" {
			status = "NOT_ELIGIBLE"
		}
		return &CampaignClaimResult{Status: status, FailureCode: failure,
			RedemptionCode: code, Message: msg}, nil
	}
	res := &CampaignClaimResult{
		OK:             status == "CLAIMED" || status == "GRANTED" || status == "SUCCESS",
		Status:         status,
		Replayed:       r.Replayed,
		FailureCode:    failure,
		GrantID:        r.GrantID,
		RedemptionCode: code,
		Amount:         amount,
	}
	if r.Replayed {
		res.Message = "已领取"
	} else {
		res.Message = "领取成功"
	}
	if code != "" {
		res.Message += "，兑换码：" + code
	}
	return res, nil
}

// CampaignReward 查询单个活动的发放状态（幂等，只读）。
func (c *Client) CampaignReward(ctx context.Context, creds Credentials, campaignID string) (json.RawMessage, error) {
	cfg := Realm(creds.NormalizedRealm())
	var out json.RawMessage
	if err := c.doJSON(ctx, http.MethodGet, cfg.OpenAPI+CampaignRewardPath(campaignID),
		nil, creds.DesktopHeaders(), &out); err != nil {
		return nil, err
	}
	return out, nil
}
