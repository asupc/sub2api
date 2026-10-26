package qoder

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
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
// openapi 基址候选（issue: 国际版签不到到；qoder_accounts.py _openapi_json）
// ---------------------------------------------------------------------------

// openapiMissingTTL 已知 404 的免探缓存有效期（源 _OPENAPI_404_TTL = 6h）。
const openapiMissingTTL = 6 * time.Hour

var (
	openapiMissingMu sync.Mutex
	openapiMissing   = map[string]time.Time{} // "realm\x00path" -> 判定时刻
)

func openapiMissingKey(realm, path string) string { return realm + "\x00" + path }

// OpenAPIPathMissing 报告该 (区域, 路径) 是否已被判定为 404（TTL 6h）。
func OpenAPIPathMissing(realm, path string) bool {
	openapiMissingMu.Lock()
	defer openapiMissingMu.Unlock()
	ts, ok := openapiMissing[openapiMissingKey(realm, path)]
	return ok && time.Since(ts) < openapiMissingTTL
}

func markOpenAPIPathMissing(realm, path string) {
	openapiMissingMu.Lock()
	defer openapiMissingMu.Unlock()
	openapiMissing[openapiMissingKey(realm, path)] = time.Now()
}

// resetOpenAPIMissingCache 清空 404 免探缓存（测试换基址时调用）。
func resetOpenAPIMissingCache() {
	openapiMissingMu.Lock()
	defer openapiMissingMu.Unlock()
	openapiMissing = map[string]time.Time{}
}

// doJSONOpenAPI 按区域 openapi 基址候选依次请求（qoder_accounts.py _openapi_json
// L2423-2465）。仅**连接失败或 404/405/410** 时换下一个基址；业务 4xx
// （401/403/429）不换——那说明地址是对的，只是这次被拒。
//
// 已知 404 的 (区域,路径) 直接返回，不再探任何基址（否则 intl 的缺失端点每次都要
// 0.x~10s 才失败；TTL 6h 后允许重探，端点上线仍能被发现）。fallback=false 时只探
// 主基址（legacy 端点在国际版必然 404，多探一个基址就是白等一个 RTT）。
func (c *Client) doJSONOpenAPI(ctx context.Context, realm, path, method string,
	body []byte, headers http.Header, out any, fallback bool) error {
	realm = normalizedRealm(realm)
	if OpenAPIPathMissing(realm, path) {
		return &StatusError{Code: http.StatusNotFound,
			Body: "cached 404 (known missing in " + realm + ")"}
	}
	bases := OpenAPICandidates(realm)
	if !fallback {
		bases = bases[:1]
	}
	var last error = &StatusError{Code: 0, Body: "no openapi candidate"}
	tried := 0
	for _, base := range bases {
		tried++
		err := c.doJSON(ctx, method, base+path, body, headers, out)
		if err == nil {
			return nil
		}
		last = err
		var se *StatusError
		if errors.As(err, &se) {
			if se.Code == http.StatusNotFound || se.Code == http.StatusMethodNotAllowed ||
				se.Code == http.StatusGone {
				// 首个基址 404：再试一个候选；第二个仍 404 即判定该路径本区域缺失。
				if tried >= 2 {
					markOpenAPIPathMissing(realm, path)
					return last
				}
				continue
			}
			return last // 业务错误：地址是对的，不换基址
		}
		continue // 连接层失败（DNS/TCP/TLS/超时）：换下一个基址
	}
	return last
}

// normalizedRealm 与 Credentials.NormalizedRealm 同口径（未知/空回落 cn）。
func normalizedRealm(realm string) string {
	if ValidRealm(realm) {
		return realm
	}
	return "cn"
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

// OpenAPIHeaders openapi 域纯 Bearer 出站头（qoder_accounts.py headers()）。
// UA 必须与所属区域的官方客户端一致（CN: QoderWork/…，INTL: Qoder/…）——
// Go 语言 HTTP 客户端的默认 UA 等于在每个 openapi 请求上自报「我是脚本」。
func (c Credentials) OpenAPIHeaders() http.Header {
	cfg := Realm(c.NormalizedRealm())
	h := http.Header{}
	h.Set("Content-Type", "application/json")
	h.Set("Accept", "application/json, text/plain, */*")
	ua := cfg.UA
	if ua == "" {
		ua = ClientUA
	}
	h.Set("User-Agent", ua)
	h.Set("Authorization", "Bearer "+c.AccessToken)
	h.Set("X-Request-ID", GenerateRequestID(c.UID))
	h.Set("X-Machine-ID", DeriveID(c.UID, "machine"))
	h.Set("X-Session-ID", DeriveID(c.UID, "session"))
	h.Set("Origin", cfg.Website)
	h.Set("Referer", cfg.Website+"/")
	return h
}

// DesktopHeaders 桌面端同款出站头（活动平台 /sash/... 必需，
// qoder_accounts.py L1722-1772）。
//
// issue #10 实测（Linux/Docker）：全套**派生** cosy-machine* 六头会被服务端
// 判定为非官方客户端、整条过滤 CLAIMABLE 活动；因此无真身份时**一律不发**
// 机器头，只发 UA / cosy-clienttype / cosy-version。真身份两个来源，优先级：
//  1. 原生身份桥（runtime-info 组件，见 umid.go）——机器级真值，intl 活动平台
//     按它过滤活动列表；
//  2. 凭证里导入的 machine_token（官方客户端凭据导入，type/code 派生兜底）。
func (c Credentials) DesktopHeaders() http.Header {
	h, _ := c.DesktopHeadersIdentity()
	return h
}

// 机器头身份来源（Python MACHINE_IDENTITY_NATIVE / MACHINE_HEADERS_OMITTED
// 口径 + sub2api 特有的凭证导入来源）。
const (
	MachineIdentityNative   = "runtime-info"
	MachineIdentityImported = "imported"
	MachineIdentityOmitted  = "omitted"
)

// desktopBaseHeaders 桌面端基础头（desktop_headers 的非机器头部分，对齐
// 官方桌面端 0.4.8 主进程 createRequestHeaders + createAuthorizedHeaders，
// 不多不少）：Accept: application/json、User-Agent: Qoder、Authorization、
// Cosy-ClientType: 10、Cosy-Version: <桌面端版本>。没有 Content-Type /
// X-Request-ID / X-Machine-ID / X-Session-ID / Origin / Referer；带请求体的
// 调用（claim）由调用方自行补 Content-Type。
func (c Credentials) desktopBaseHeaders() http.Header {
	h := http.Header{}
	h.Set("Accept", "application/json")
	h.Set("User-Agent", "Qoder")
	h.Set("Authorization", "Bearer "+c.AccessToken)
	h.Set("cosy-clienttype", DesktopClientType)
	h.Set("cosy-version", desktopClientVersion())
	return h
}

// DesktopHeadersIdentity 桌面端出站头 + 本次机器头身份来源（native 优先，
// 凭证导入次之，无真身份不发机器头）。
func (c Credentials) DesktopHeadersIdentity() (http.Header, string) {
	h := c.desktopBaseHeaders()
	if ident := NativeMachineIdentity(c.NormalizedRealm(), c.UID); ident.valid() {
		c.applyMachineHeaders(h, ident.MachineToken, ident.MachineType, ident.MachineCode)
		return h, MachineIdentityNative
	}
	if c.MachineToken != "" {
		c.applyMachineHeaders(h, c.MachineToken, DeriveMachineType(c.UID), DeriveID(c.UID, "machinecode"))
		return h, MachineIdentityImported
	}
	return h, MachineIdentityOmitted
}

// desktopHeadersWithIdentity 用指定身份构造桌面端头（自愈重试用：新身份
// 直接注入，不再过缓存）。
func (c Credentials) desktopHeadersWithIdentity(ident MachineIdentity) http.Header {
	h := c.desktopBaseHeaders()
	c.applyMachineHeaders(h, ident.MachineToken, ident.MachineType, ident.MachineCode)
	return h
}

// desktopHeadersMinimal 最小机器头形态（v1.6.9）：基础头 + 仅 machineos +
// machinehostname。仅 showCampaign=false 且本次未发机器头时用一次。
func (c Credentials) desktopHeadersMinimal() http.Header {
	h := c.desktopBaseHeaders()
	h.Set("cosy-machineos", cosyMachineOS())
	if hn := machineHostname(); hn != "" {
		h.Set("cosy-machinehostname", hn)
	}
	return h
}

// machineMinimalDisabled 最小机器头兜底开关（QD_INTL_MACHINE_FALLBACK，
// v1.6.9 默认开）。
func machineMinimalDisabled() bool {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv("QD_INTL_MACHINE_FALLBACK")))
	switch raw {
	case "0", "off", "false", "no", "disable", "disabled":
		return true
	}
	return false
}

// applyMachineHeaders 注入全套 cosy-machine* 六头。machineid 按账号派生
// （Python 同款：即便原生身份，machineid 也用 derive_id）；token/type/code
// 用真值（原生身份或导入 token 时由调用方决定）。machineos 与推理链路
// 共用同一取值（cosyMachineOS）；machinehostname 用本机主机名（空则不发）。
func (c Credentials) applyMachineHeaders(h http.Header, token, machineType, machineCode string) {
	machineID := c.MachineID
	if machineID == "" {
		machineID = DeriveID(c.UID, "machine")
	}
	h.Set("cosy-machineid", machineID)
	h.Set("cosy-machinetoken", token)
	h.Set("cosy-machinetype", machineType)
	h.Set("cosy-machinecode", machineCode)
	h.Set("cosy-machineos", cosyMachineOS())
	if hn := machineHostname(); hn != "" {
		h.Set("cosy-machinehostname", hn)
	}
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
	Remain           int            `json:"remain"`
	Used             int            `json:"used"`
	Size             int            `json:"size"`
	Exceeded         bool           `json:"exceeded"`
	UsagePct         any            `json:"usage_pct"`
	ExpiresAt        int64          `json:"expires_at"`
	Packages         []QuotaPackage `json:"packages"`
	// issue #39/#67：共享组织配额（orgResourcePackage 等）。
	SharedRemain     int    `json:"shared_remain,omitempty"`
	SharedUsed       int    `json:"shared_used,omitempty"`
	SharedSize       int    `json:"shared_size,omitempty"`
	SharedAvailable  bool   `json:"shared_available,omitempty"`
	SharedUnlimited  bool   `json:"shared_unlimited,omitempty"`
	SharedSource     string `json:"shared_source,omitempty"`
	EffectiveRemain  int    `json:"effective_remain"`
}

type quotaUsageRaw struct {
	UserQuota            map[string]any `json:"userQuota"`
	AddOnQuota           map[string]any `json:"addOnQuota"`
	IsQuotaExceeded      bool           `json:"isQuotaExceeded"`
	TotalUsagePercentage any            `json:"totalUsagePercentage"`
	ExpiresAt            any            `json:"expiresAt"`
	OrgResourcePackage   any            `json:"orgResourcePackage"`
	SharedQuota          any            `json:"sharedQuota"`
	TeamQuota            any            `json:"teamQuota"`
	PoolQuota            any            `json:"poolQuota"`
	SharedCredits        any            `json:"sharedCredits"`
	SharedCredit         any            `json:"sharedCredit"`
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
	remain := int(num(uq, "remaining") + num(aq, "remaining"))
	used := int(num(uq, "used") + num(aq, "used"))
	size := int(num(uq, "total") + num(aq, "total"))

	// issue #39/#67：解析共享组织配额（orgResourcePackage 等）。
	var sharedSrc map[string]any
	var sharedAny any
	var sharedKey string
	for _, cand := range []struct {
		key string
		val any
	}{
		{"orgResourcePackage", raw.OrgResourcePackage},
		{"sharedQuota", raw.SharedQuota},
		{"teamQuota", raw.TeamQuota},
		{"poolQuota", raw.PoolQuota},
		{"sharedCredits", raw.SharedCredits},
		{"sharedCredit", raw.SharedCredit},
	} {
		if m, ok := cand.val.(map[string]any); ok {
			sharedSrc = m
			sharedAny = cand.val
			sharedKey = cand.key
			break
		}
		if n, ok := cand.val.(float64); ok {
			// 数值形态：视为 remain/size
			sharedSrc = map[string]any{"remaining": n, "used": float64(0), "cap": n}
			sharedAny = sharedSrc
			sharedKey = cand.key
			break
		}
	}
	sharedRemain, sharedUsed, sharedSize := 0, 0, 0
	sharedAvailable := false
	sharedUnlimited := false
	if sharedSrc != nil {
		sharedRemain = int(num(sharedSrc, "remaining"))
		sharedUsed = int(num(sharedSrc, "used"))
		// cap 优先，其次 total
		if capv := num(sharedSrc, "cap"); capv != 0 {
			sharedSize = int(capv)
		} else {
			sharedSize = int(num(sharedSrc, "total"))
		}
		// available：缺键视为 true；显式 false/0/null 视为 false
		if v, ok := sharedSrc["available"]; ok {
			switch t := v.(type) {
			case bool:
				sharedAvailable = t
			case float64:
				sharedAvailable = t != 0
			case nil:
				sharedAvailable = false
			default:
				sharedAvailable = true
			}
		} else {
			sharedAvailable = true
		}
		if sharedSize < 0 {
			sharedUnlimited = true
		}
	}
	_ = sharedAny

	// 有效余额 = 套餐 remain + 可用的共享 remain（无限池不计入占位 remaining）。
	sharedAvailRemain := 0
	if sharedSrc != nil && sharedAvailable && !sharedUnlimited {
		sharedAvailRemain = sharedRemain
	}
	effectiveRemain := remain + sharedAvailRemain

	packages := []QuotaPackage{
		{Name: "基础额度", Remain: int(num(uq, "remaining")), Used: int(num(uq, "used")), Size: int(num(uq, "total"))},
		{Name: "赠送/签到额度", Remain: int(num(aq, "remaining")), Used: int(num(aq, "used")), Size: int(num(aq, "total"))},
	}
	if sharedSrc != nil {
		packages = append(packages, QuotaPackage{
			Name:   "共享额度",
			Remain: sharedRemain,
			Used:   sharedUsed,
			Size:   sharedSize,
		})
	}

	out := &QuotaUsage{
		Remain:           remain,
		Used:             used,
		Size:             size,
		Exceeded:         raw.IsQuotaExceeded,
		UsagePct:         raw.TotalUsagePercentage,
		ExpiresAt:        normalizeEpoch(raw.ExpiresAt),
		Packages:         packages,
		SharedRemain:     sharedRemain,
		SharedUsed:       sharedUsed,
		SharedSize:       sharedSize,
		SharedAvailable:  sharedAvailable,
		SharedUnlimited:  sharedUnlimited,
		SharedSource:     sharedKey,
		EffectiveRemain:  effectiveRemain,
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
//
// issue: 国际版签不到到 —— 基址走候选（openapi.qoder.sh 在部分网络不可达），
// 只在连接失败 / 404 时换下一个，业务 4xx 不换。fallback=false：该端点是
// legacy（国际版必然 404），多探一个基址就是白等一个 RTT（源 _openapi_json）。
func (c *Client) CheckinStatus(ctx context.Context, creds Credentials) (*CheckinStatusResult, error) {
	realm := creds.NormalizedRealm()
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
	err := c.doJSONOpenAPI(ctx, realm, PathCheckinStatus, http.MethodGet, nil,
		creds.OpenAPIHeaders(), &q, false)
	if err != nil {
		if se, ok := err.(*StatusError); ok &&
			(se.Code == 404 || se.Code == 405 || se.Code == 410) {
			// 本区域无此端点：记 404 缓存（TTL 6h 后允许重探，端点上线仍能被发现）。
			markOpenAPIPathMissing(realm, PathCheckinStatus)
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
	var res struct {
		Success       *bool  `json:"success"`
		Error         string `json:"error"`
		RewardCredits int    `json:"rewardCredits"`
		Result        string `json:"result"`
	}
	// 同上：基址走候选（fallback=false，legacy 端点只探主基址）。
	err = c.doJSONOpenAPI(ctx, creds.NormalizedRealm(), PathCheckinClaim, http.MethodPost,
		[]byte("{}"), creds.OpenAPIHeaders(), &res, false)
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

// campaignsPayload 活动列表响应的业务字段子集。
type campaignsPayload struct {
	ShowCampaign bool             `json:"showCampaign"`
	Claimable    bool             `json:"claimable"`
	CampaignURL  string           `json:"campaignUrl"`
	Campaigns    []map[string]any `json:"campaigns"`
}

// Campaigns 拉取官方活动平台列表（GET /sash/api/v1/me/campaigns，双区域通用）。
// 桌面端头被拒（401/403）时回退普通头（_campaigns_get L1939-1965）；原生身份
// 被服务端拒绝（showCampaign=false）时清种子换新身份重试一次（campaigns
// L2064-2072 的自愈：同种子下组件只会返回同一个被拒身份，不换种子自愈空转）。
func (c *Client) Campaigns(ctx context.Context, creds Credentials) (*CampaignsStatus, error) {
	realm := creds.NormalizedRealm()
	headers, source := creds.DesktopHeadersIdentity()
	var raw json.RawMessage
	// 基址走候选（源 _campaigns_get -> _openapi_json：仅连接失败/404 换下一个）。
	err := c.doJSONOpenAPI(ctx, realm, PathCampaigns, http.MethodGet, nil, headers, &raw, true)
	if err != nil {
		// 桌面端头被拒（401/403）时回退普通头，至少保留"能不能看到"的信息
		if se, ok := err.(*StatusError); ok && (se.Code == 401 || se.Code == 403) {
			err = c.doJSONOpenAPI(ctx, realm, PathCampaigns, http.MethodGet, nil,
				creds.OpenAPIHeaders(), &raw, true)
		}
	}
	if err != nil {
		if se, ok := err.(*StatusError); ok &&
			(se.Code == 404 || se.Code == 405 || se.Code == 410) {
			return &CampaignsStatus{Available: false, Error: se.Error()}, nil
		}
		return &CampaignsStatus{Available: true, Error: err.Error()}, nil
	}
	var q campaignsPayload
	if err := json.Unmarshal(raw, &q); err != nil {
		return &CampaignsStatus{Available: true, Error: err.Error()}, nil
	}
	// v1.6.9 intl 兜底：列表被隐藏且本次未发机器头（无原生身份/导入 token）时，
	// 用「最小机器头」（只发 machineos + machinehostname）再试一次——实测该
	// 组合仍被服务端视为官方客户端（六头全发才会被过滤）；QD_INTL_MACHINE_FALLBACK
	// 可关。属于「让列表回来」的最小代价。
	if !q.ShowCampaign && source == MachineIdentityOmitted && !machineMinimalDisabled() {
		var retryRaw json.RawMessage
		if rerr := c.doJSONOpenAPI(ctx, realm, PathCampaigns, http.MethodGet, nil,
			creds.desktopHeadersMinimal(), &retryRaw, true); rerr == nil {
			var rq campaignsPayload
			if jerr := json.Unmarshal(retryRaw, &rq); jerr == nil && rq.ShowCampaign {
				raw = retryRaw
				q = rq
			}
		}
	}
	// 身份被拒自愈：原生身份 + showCampaign=false（服务端不认当前身份）→
	// 清组件种子强制换新身份重试一次；重试仍不认则如实返回（等下次再试）。
	if !q.ShowCampaign && source == MachineIdentityNative {
		if ident := RefreshNativeIdentity(realm, creds.UID); ident.valid() {
			var retryRaw json.RawMessage
			if rerr := c.doJSONOpenAPI(ctx, realm, PathCampaigns, http.MethodGet, nil,
				creds.desktopHeadersWithIdentity(ident), &retryRaw, true); rerr == nil {
				var rq campaignsPayload
				if jerr := json.Unmarshal(retryRaw, &rq); jerr == nil && rq.ShowCampaign {
					raw = retryRaw
					q = rq
				}
			}
		}
	}
	st := &CampaignsStatus{MachineHeaders: source}
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

// ---------------------------------------------------------------------------
// 活动平台签到编排（qoder_accounts.py campaign_checkin L2228-2390 的移植）。
// ---------------------------------------------------------------------------

// CampaignCheckinInput 活动平台签到编排入参。
type CampaignCheckinInput struct {
	Creds Credentials
	// OnlyKinds 非 nil 时只领这些 benefit.kind（大写比较；checkin 任务传
	// {"", "CREDITS"}，对齐 Python only_kinds=("", "CREDITS")）。
	OnlyKinds []string
	// BlockedUntil cid→到期 unix 秒（service 从 extra["qd_campaign_blocked"]
	// 读入）；命中则不再 POST claim，直接计入 Blocked（同人去重冷却）。
	BlockedUntil map[string]int64
	// ClaimGap 相邻两次 claim 请求的间隔；<=0 用默认 1s（防风控）。
	ClaimGap time.Duration
}

// CampaignCodeGrant 活动发放的兑换码（campaign_checkin codes 项）。
type CampaignCodeGrant struct {
	Campaign string `json:"campaign"`
	Code     string `json:"code"`
}

// CampaignCheckinOutcome 活动平台签到编排结果。
type CampaignCheckinOutcome struct {
	// OK 列表查询成功且无未归类错误（对齐 Python ok: not errors）；
	// false 时 service 侧走旧 sash Checkin 兜底。
	OK bool `json:"ok"`
	// Available 本区域存在活动平台（列表 404/405/410 → false）。
	Available bool `json:"available"`
	// Error 列表查询失败原因（触发兜底）。
	Error string `json:"error,omitempty"`
	// Claimed 真实新增领取的活动展示名（非 replayed）。
	Claimed []string `json:"claimed,omitempty"`
	// Already / Blocked / Pending / Locked / Views 分类结论（展示名）。
	Already           []string            `json:"already,omitempty"`
	Blocked           []string            `json:"blocked,omitempty"`
	Pending           []string            `json:"pending,omitempty"`
	Locked            []string            `json:"locked,omitempty"`
	Views             []string            `json:"views,omitempty"`
	Codes             []CampaignCodeGrant `json:"codes,omitempty"`
	Errors            []string            `json:"errors,omitempty"`
	Earned            int                 `json:"earned"` // 仅 claimed 且非 replayed 累计
	Message           string              `json:"message"`
	NextAvailableAt   int64               `json:"next_available_at,omitempty"`
	NextAvailableNote string              `json:"next_available_note,omitempty"`
	// BlockedMarks 本轮新触发的 cid→now+6h 冷却标记，由 service 落 extra。
	BlockedMarks map[string]int64 `json:"blocked_marks,omitempty"`
}

// defaultCampaignClaimGap 相邻 claim 的最小间隔（Python CLAIM_GAP_DEFAULT）。
const defaultCampaignClaimGap = time.Second

// CampaignCheckin 活动平台签到：列活动 → 分类 → 逐个领取 CLAIMABLE 目标类条目。
// 判定链逐条对齐 qoder_accounts.py L2262-2390；本函数不抛业务错误（列表/领取
// 失败均归入 Outcome 字段），仅 ctx 取消等传输层异常经 error 返回。
func (c *Client) CampaignCheckin(ctx context.Context, in CampaignCheckinInput) (*CampaignCheckinOutcome, error) {
	out := &CampaignCheckinOutcome{BlockedMarks: map[string]int64{}}
	st, err := c.Campaigns(ctx, in.Creds) // Go 无列表缓存，直接查最新（Python force=True 的对应物）
	if err == nil && st != nil && st.OK {
		out.Available = st.Available
	} else {
		out.Available = st != nil && st.Available
		if err != nil {
			out.Error = clip(err.Error(), 200)
		} else if st != nil {
			out.Error = clip(st.Error, 200)
		} else {
			out.Error = "campaigns: nil status"
		}
		out.NextAvailableAt, out.NextAvailableNote = nextCheckinWindow(time.Now())
		return out, nil
	}
	gap := in.ClaimGap
	if gap <= 0 {
		gap = defaultCampaignClaimGap
	}
	now := time.Now()
	out.NextAvailableAt, out.NextAvailableNote = nextCheckinWindow(now)
	var codes []CampaignCodeGrant
	// hasPending/hasLocked 仅用于 message 附注去重（对齐 Python "名额已发完/新人任务"关键词短路）。
	var hasPending, hasLocked bool
	for _, item := range st.Campaigns {
		if in.OnlyKinds != nil && !kindAllowed(in.OnlyKinds, strings.ToUpper(strings.TrimSpace(item.BenefitKind))) {
			continue // 只要"每日签到领积分"时跳过券类/其它奖励
		}
		cid := item.CampaignID
		if cid == "" {
			continue
		}
		label := campaignLabel(item)
		status := strings.ToUpper(strings.TrimSpace(item.ClaimStatus))
		actionType := strings.ToUpper(strings.TrimSpace(item.ActionType))
		reason := strings.ToUpper(strings.TrimSpace(item.UnavailableReason))
		amount := item.BenefitAmount
		if status == "CLAIMED" {
			// #25 遗留：归类先于 action_type 过滤——官方领取后会把 actionType
			// 降级成 VIEW_DETAILS（intl 实测），先过滤会丢掉"已领"事实、误报
			// "暂无可领取活动"。有奖励（amount>0）或仍是可领类 → already；
			// 真·详情类（无奖励）才算 views。
			if amount > 0 || actionType == "" || actionType == "CLAIM_BENEFIT" {
				out.Already = append(out.Already, label)
			} else {
				out.Views = append(out.Views, label)
			}
			continue
		}
		// 以下只针对"还没领"的条目：详情类无奖励，不参与领取/归类。
		if actionType != "" && actionType != "CLAIM_BENEFIT" {
			continue
		}
		if status != "CLAIMABLE" {
			// 不可领取但并非"与我们无关"：名额发完/成就未完成如实报出。
			// （Go 的 AchievementCompleted 无法区分缺失与 false，只取
			// unavailable_reason 主判定路径，漏分的条目与 Python 一样不领取。）
			switch reason {
			case "REDEMPTION_CODE_OUT_OF_STOCK":
				out.Pending = append(out.Pending, label)
				hasPending = true
			case "ACHIEVEMENT_NOT_COMPLETED":
				out.Locked = append(out.Locked, label)
				hasLocked = true
			}
			continue
		}
		if exp, ok := in.BlockedUntil[cid]; ok && exp > now.Unix() {
			out.Blocked = append(out.Blocked, label+"（冷却中）")
			continue // 同人已领（冷却中）：不再重复 POST
		}
		res, cerr := c.ClaimCampaign(ctx, in.Creds, cid)
		if cerr != nil {
			errName := item.CampaignKey
			if errName == "" {
				errName = cid
			}
			out.Errors = append(out.Errors, fmt.Sprintf("%s: %s", errName, clip(cerr.Error(), 120)))
		} else if res.Blocked {
			out.BlockedMarks[cid] = time.Now().Add(6 * time.Hour).Unix()
			out.Blocked = append(out.Blocked, label)
		} else if res.FailureCode != "" {
			// 已知失败码按"待重试/被锁"分类，不算失败。
			if res.FailureCode == "REDEMPTION_CODE_OUT_OF_STOCK" {
				out.Pending = append(out.Pending, label)
				hasPending = true
			} else {
				out.Locked = append(out.Locked, label)
				hasLocked = true
			}
		} else if res.Replayed {
			out.Already = append(out.Already, label)
			if res.RedemptionCode != "" {
				codes = append(codes, CampaignCodeGrant{Campaign: label, Code: res.RedemptionCode})
			}
		} else if !res.OK {
			// 未知结局（状态非 CLAIMED/GRANTED/SUCCESS 且无失败码）：如实报错，
			// 不冒领（对齐 Python else → errors）。
			msg := res.Status
			if msg == "" {
				msg = "unknown"
			}
			out.Errors = append(out.Errors, fmt.Sprintf("%s: status=%s", cid, msg))
		} else {
			earned := res.Amount
			if earned <= 0 {
				earned = item.BenefitAmount
			}
			if earned < 0 {
				earned = 0
			}
			out.Claimed = append(out.Claimed, label)
			out.Earned += earned
			if res.RedemptionCode != "" {
				codes = append(codes, CampaignCodeGrant{Campaign: label, Code: res.RedemptionCode})
			}
		}
		// 每个 claim 请求之后统一等待一次（成功/被挡/失败都刚打过上游）。
		if err := sleepCtx(ctx, gap); err != nil {
			return out, err
		}
	}
	out.Codes = codes
	out.OK = len(out.Errors) == 0
	out.Message = campaignCheckinMessage(out)
	if hasPending && !strings.Contains(out.Message, "名额已发完") {
		out.Message += "；另有活动今日名额已发完、次日 10:00 后重试：" + strings.Join(out.Pending, "、")
	} else if hasLocked && !strings.Contains(out.Message, "新人任务") {
		out.Message += "；另有活动需先完成新人任务：" + strings.Join(out.Locked, "、")
	}
	return out, nil
}

// campaignCheckinMessage 结论优先级链（Python L2345-2367）。
func campaignCheckinMessage(out *CampaignCheckinOutcome) string {
	switch {
	case len(out.Claimed) > 0:
		return fmt.Sprintf("活动领取成功 +%d Credits（%s）", out.Earned, strings.Join(out.Claimed, "、"))
	case len(out.Blocked) > 0:
		return "同人已领取：同一设备/身份下的其他账号本轮已领过（服务端按人去重）"
	case len(out.Already) > 0:
		return "今日活动奖励已领取（" + strings.Join(out.Already, "、") + "）"
	case len(out.Pending) > 0:
		return "名额已发完，次日 10:00 后可再领：" + strings.Join(out.Pending, "、")
	case len(out.Locked) > 0:
		return "需先在官方桌面端完成新人任务后可领：" + strings.Join(out.Locked, "、")
	case len(out.Errors) > 0:
		return "活动领取失败：" + clip(strings.Join(out.Errors, "; "), 200)
	default:
		return "当前账号暂无可领取的官方活动"
	}
}

// campaignLabel 活动展示名：官方中文标题优先，其次 key，最后 id
// （Python campaign_label L781-784）。
func campaignLabel(item CampaignItem) string {
	if t := strings.TrimSpace(item.TitleZh); t != "" {
		return t
	}
	if t := strings.TrimSpace(item.CampaignKey); t != "" {
		return t
	}
	return item.CampaignID
}

// kindAllowed OnlyKinds 匹配（大小写不敏感）。
func kindAllowed(kinds []string, kind string) bool {
	for _, k := range kinds {
		if strings.EqualFold(strings.TrimSpace(k), kind) {
			return true
		}
	}
	return false
}

// checkinWindowHourUTC8 官方每日名额刷新时刻（UTC+8 固定，不能按系统本地时区
// 计算——容器常为 UTC，差 8 小时对不上官方说明；Python L844-848）。
const checkinWindowHourUTC8 = 10

var qoderUTC8 = time.FixedZone("UTC+8", 8*3600)

// nextCheckinWindow 下一个「每日 10:00（UTC+8）」窗口 → (epoch 秒, 可读文本)。
// 边界（Python next_checkin_window L851-867，issue #20 口径）：今天 10:00 之前
// → 今天 10:00；到达/晚于 10:00（含刚领取成功）→ 明天 10:00（到达即算下一轮，
// 不返回已经到点的时刻）。note 形如 "10-15 10:00（UTC+8）"。
func nextCheckinWindow(now time.Time) (int64, string) {
	n := now.In(qoderUTC8)
	boundary := time.Date(n.Year(), n.Month(), n.Day(), checkinWindowHourUTC8, 0, 0, 0, qoderUTC8)
	if !n.Before(boundary) {
		boundary = boundary.Add(24 * time.Hour)
	}
	return boundary.Unix(), boundary.Format("01-02 15:04") + "（UTC+8）"
}

// sleepCtx 可中断等待。
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// ---------------------------------------------------------------------------
// 官方积分消耗（credits-summary / credits-heatmap，issue: 官方积分消耗）
// ---------------------------------------------------------------------------

// creditUsageTTL 积分消耗缓存有效期（源 CREDIT_USAGE_TTL = 300s：该接口是全量
// 聚合，别跟着轮询打）。
const creditUsageTTL = 5 * time.Minute

// CreditDay 逐日积分消耗（heatmap 条目）。
type CreditDay struct {
	Date  string  `json:"date"`
	Value float64 `json:"value"`
}

// CreditUsage 官方积分消耗（fetch_credit_usage L2351-2396 的返回形状）。
type CreditUsage struct {
	// Total 窗口内累计消耗积分（官方口径，不是我们的 token 估算）。
	Total float64 `json:"total"`
	// Peak 单日峰值；PeakDate 为对应日期。
	Peak     float64    `json:"peak"`
	PeakDate string     `json:"peak_date"`
	Unit     string     `json:"unit"`
	Daily    []CreditDay `json:"daily"`
}

type creditUsageCacheEntry struct {
	at  time.Time
	val CreditUsage
}

var (
	creditUsageMu    sync.Mutex
	creditUsageCache = map[string]creditUsageCacheEntry{} // uid -> 缓存
)

func creditUsageKey(creds Credentials) string {
	return creds.NormalizedRealm() + "\x00" + creds.UID
}

// resetCreditUsageCache 清空积分消耗缓存（测试用）。
func resetCreditUsageCache() {
	creditUsageMu.Lock()
	defer creditUsageMu.Unlock()
	creditUsageCache = map[string]creditUsageCacheEntry{}
}

// FetchCreditUsage 官方积分消耗（GET credits-summary + credits-heatmap）。
//
// summary 形态 {totalCredits, peakCredits, peakDate, unit}；heatmap 365 天逐日。
// 结果按账号缓存 5 分钟（CREDIT_USAGE_TTL：全量聚合接口，别跟着轮询打）。
// summary 拉取失败时返回 ok=false 的错误信息（heatmap 不再请求）；heatmap 失败
// 只丢弃 daily，summary 仍可用（源 _c2/_e2 被忽略）。
//
// days>0 时只保留最近 N 天（源 daily[-days:]）；days<=0 保留全部。
func (c *Client) FetchCreditUsage(ctx context.Context, creds Credentials, days int) (CreditUsage, error) {
	key := creditUsageKey(creds)
	creditUsageMu.Lock()
	if hit, ok := creditUsageCache[key]; ok && time.Since(hit.at) < creditUsageTTL {
		creditUsageMu.Unlock()
		return hit.val, nil
	}
	creditUsageMu.Unlock()

	var sum struct {
		TotalCredits float64 `json:"totalCredits"`
		PeakCredits  float64 `json:"peakCredits"`
		PeakDate     string  `json:"peakDate"`
		Unit         string  `json:"unit"`
	}
	err := c.doJSONOpenAPI(ctx, creds.NormalizedRealm(), PathCreditsSummary,
		http.MethodGet, nil, creds.DesktopHeaders(), &sum, true)
	if err != nil {
		// 源语义：summary 失败时整体 ok=false（即便缓存也记，避免反复打全量接口）。
		out := CreditUsage{Unit: "credits"}
		creditUsageMu.Lock()
		creditUsageCache[key] = creditUsageCacheEntry{at: time.Now(), val: out}
		creditUsageMu.Unlock()
		return out, err
	}

	out := CreditUsage{
		Total:    sum.TotalCredits,
		Peak:     sum.PeakCredits,
		PeakDate: sum.PeakDate,
		Unit:     sum.Unit,
	}
	if out.Unit == "" {
		out.Unit = "credits"
	}
	var heat struct {
		Items []struct {
			Date  string   `json:"date"`
			Value *float64 `json:"value"`
		} `json:"items"`
	}
	// heatmap 失败只丢 daily（源 _c2/_e2 被忽略，summary 仍可用）。
	if herr := c.doJSONOpenAPI(ctx, creds.NormalizedRealm(), PathCreditsHeatmap,
		http.MethodGet, nil, creds.DesktopHeaders(), &heat, true); herr == nil {
		out.Daily = make([]CreditDay, 0, len(heat.Items))
		for _, item := range heat.Items {
			v := 0.0
			if item.Value != nil {
				v = *item.Value
			}
			out.Daily = append(out.Daily, CreditDay{Date: item.Date, Value: v})
		}
	}
	if days > 0 && len(out.Daily) > days {
		out.Daily = out.Daily[len(out.Daily)-days:]
	}

	creditUsageMu.Lock()
	creditUsageCache[key] = creditUsageCacheEntry{at: time.Now(), val: out}
	creditUsageMu.Unlock()
	return out, nil
}
