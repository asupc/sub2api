// auth.go 账号凭证（Credentials）与 realm 归一化。
//
// Credentials 对接 sub2api account.credentials（JSONB，map[string]string 视角）：
// json tag 用下划线，expires_at 以十进制字符串存储（读取时兼容数字字符串）。
// realm 判定移植自 workbuddy2api-panel internal/auth/auth.go：显式 realm >
// domain 后缀 .workbuddy.ai > cn。
package workbuddy

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
)

// Credentials 归一化的 WorkBuddy 账号凭证（值类型，刷新后由调用方整体写回存储）。
type Credentials struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	// ExpiresAt Unix 秒。与 sub2api credentials 的 string 值互转时序列化为十进制字符串。
	ExpiresAt int64 `json:"expires_at"`
	// Realm 账号域："cn" | "global"。空 = 按 Domain 后缀推断（最终恒非空，见 Realm()）。
	Realm string `json:"realm"`
	UID   string `json:"uid"`
	// EnterpriseID 非空表示企业版账号：上游无个人成长体系（签到/成长任务/旅行/
	// 夜猫子一律 403 或 400 code 10001），额度查询走企业口径端点。
	EnterpriseID string `json:"enterprise_id"`
	Domain       string `json:"domain"`
	Nickname     string `json:"nickname"`
	// DeviceToken 设备风控 Token（X-Device-Token 头）。登录时上游签发；空则不注入该头。
	DeviceToken string `json:"device_token"`
	// ClientVersion WorkBuddy 客户端版本段（出站 UA 的 `WorkBuddy/<ver>` 与 X-IDE-Version）。
	// 空 = 内置默认 5.5.4（对齐官方分发包）。
	ClientVersion string `json:"client_version"`
	// CLIVersion 出站 UA 中 `CLI/<ver>` 段版本。空 = 内置默认 2.137.1。
	CLIVersion string `json:"cli_version"`
	// UserAgent 显式覆盖整串出站 UA（非空时全路径生效，优先于默认三段式）。
	UserAgent string `json:"user_agent"`
}

// Realm 常量。
const (
	RealmCN     = "cn"
	RealmGlobal = "global"
)

// ParseCredentials 从 sub2api account.credentials 的 map[string]string 视图解析凭证。
// expires_at 兼容十进制字符串（标准形态）与残留数字形态；AccessToken 为空视为无效
// 凭证返回错误（与源项目 auth.Parse 的守卫一致）。
func ParseCredentials(m map[string]string) (Credentials, error) {
	c := Credentials{
		AccessToken:   strings.TrimSpace(m["access_token"]),
		RefreshToken:  strings.TrimSpace(m["refresh_token"]),
		Realm:         strings.TrimSpace(m["realm"]),
		UID:           strings.TrimSpace(m["uid"]),
		EnterpriseID:  strings.TrimSpace(m["enterprise_id"]),
		Domain:        strings.TrimSpace(m["domain"]),
		Nickname:      m["nickname"],
		DeviceToken:   strings.TrimSpace(m["device_token"]),
		ClientVersion: strings.TrimSpace(m["client_version"]),
		CLIVersion:    strings.TrimSpace(m["cli_version"]),
		UserAgent:     strings.TrimSpace(m["user_agent"]),
	}
	if raw, ok := m["expires_at"]; ok && strings.TrimSpace(raw) != "" {
		// 兼容数字字符串与十进制字符串两种形态（历史数据可能以数字写入）。
		n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if err != nil {
			// 非数字（如 RFC3339）不在此解析：ExpiresAt 保持 0（视为需刷新）。
			if t, terr := time.Parse(time.RFC3339, strings.TrimSpace(raw)); terr == nil {
				c.ExpiresAt = t.Unix()
			}
		} else {
			c.ExpiresAt = n
		}
	}
	if c.AccessToken == "" {
		return Credentials{}, errEmptyAccessToken
	}
	return c, nil
}

// errEmptyAccessToken 缺少 accessToken 的凭证错误。
var errEmptyAccessToken = errors.New("parse_error: missing accessToken")

// ToMap 把凭证写回 sub2api account.credentials 的 map[string]string 视图。
// expires_at 序列化为十进制字符串（sub2api credentials 值均为 string）。
// 空可选字段不写入（保持 map 干净，与源项目 SaveAtomic「空值不落键」同口径）。
func (c Credentials) ToMap() map[string]string {
	m := map[string]string{
		"access_token":  c.AccessToken,
		"refresh_token": c.RefreshToken,
		"expires_at":    strconv.FormatInt(c.ExpiresAt, 10),
		"realm":         c.RealmNormalized(),
		"uid":           c.UID,
		"domain":        c.Domain,
	}
	if c.EnterpriseID != "" {
		m["enterprise_id"] = c.EnterpriseID
	}
	if c.Nickname != "" {
		m["nickname"] = c.Nickname
	}
	if c.DeviceToken != "" {
		m["device_token"] = c.DeviceToken
	}
	if c.ClientVersion != "" {
		m["client_version"] = c.ClientVersion
	}
	if c.CLIVersion != "" {
		m["cli_version"] = c.CLIVersion
	}
	if c.UserAgent != "" {
		m["user_agent"] = c.UserAgent
	}
	return m
}

// RealmNormalized 返回归一化域：显式 Realm=="global" 或 domain 后缀
// .workbuddy.ai → "global"，否则 "cn"。显式 global 优先于 domain 回落。
// 空 realm + 空 domain → "cn"（老 CN 凭证零回归）。
func (c Credentials) RealmNormalized() string {
	return ResolveRealm(c.Realm, c.Domain)
}

// ResolveRealm 归一化 realm（cn/global）：显式非空优先，否则按原始 domain 推断。
// domain 也为空 → "cn"。显式值原样透传（不做白名单收紧——写入侧已校验）。
func ResolveRealm(explicit, domain string) string {
	if r := strings.TrimSpace(explicit); r != "" {
		return r
	}
	if isGlobalDomain(domain) {
		return RealmGlobal
	}
	return RealmCN
}

// IsGlobal 报告账号是否属于 global realm。
func (c Credentials) IsGlobal() bool { return c.RealmNormalized() == RealmGlobal }

// IsEnterprise 报告账号是否为企业版（enterpriseId 非空）。
// 企业版无个人成长体系：签到 / 成长任务 / 旅行 / 夜猫子上游一律被拒，调用方须跳过。
func (c Credentials) IsEnterprise() bool { return strings.TrimSpace(c.EnterpriseID) != "" }

// IsEnterpriseSession 报告当前 token 是否为企业**切换会话**形态（JWT payload 的
// token_source=enterprise_switch）。与 IsEnterprise（计费口径，看 enterpriseId）刻意
// 隔离：切换会话下额度查询等读端点照常（个人口径），但 /v2/report 行为事件被上游
// 静默丢弃（200 但不入账）——成长任务等行为类动作会白跑。移植自 workbuddy2api-panel
// 9282779：现解码 JWT 判定、不缓存——refresh 会整体换发 token，切换状态可能随重新
// 授权翻转。
func (c Credentials) IsEnterpriseSession() bool {
	return jwtTokenSource(c.AccessToken) == "enterprise_switch"
}

// jwtTokenSource 提取 access token JWT payload 的 token_source 声明（空 = 无）。
// 只解码不验签（真正鉴权由上游完成）；任何解析失败都按「无」处理，绝不报错。
func jwtTokenSource(accessToken string) string {
	parts := strings.Split(strings.TrimSpace(accessToken), ".")
	if len(parts) < 2 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return ""
	}
	var claims struct {
		TokenSource string `json:"token_source"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	return strings.TrimSpace(claims.TokenSource)
}

// NeedsRefresh 报告 token 是否将在 within 内过期（或已过期 / 无 expiry）。
func (c Credentials) NeedsRefresh(within time.Duration, now time.Time) bool {
	if c.ExpiresAt <= 0 {
		return true
	}
	return now.Add(within).Unix() >= c.ExpiresAt
}

// isGlobalDomain 判定 domain 是否指向 www.workbuddy.ai 家族：
// 裸域 workbuddy.ai 或任意 .workbuddy.ai 子域。
func isGlobalDomain(d string) bool {
	d = strings.ToLower(strings.TrimSpace(d))
	return d == "workbuddy.ai" || strings.HasSuffix(d, ".workbuddy.ai")
}
