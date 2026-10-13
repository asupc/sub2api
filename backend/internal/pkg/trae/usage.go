package trae

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"runtime"
	"time"
)

// 积分 / 签到 / 状态接口路径（出处：core mod.rs，抓包固化）。
const (
	// CheckinPath 签到接口。
	CheckinPath = "/trae/api/v2/ug/checkin_credits/claim"
	// CheckinStatusPath 签到状态预检接口。
	CheckinStatusPath = "/trae/api/v2/ug/checkin_credits/status"
	// EntitlementPath 剩余积分（额度）查询接口。
	EntitlementPath = "/trae/api/v2/pay/ide_user_ent_usage"
)

// 上游请求头的固定取值（出处：credits.rs 顶部，抓包复刻；线上契约，勿按语义改写）。
const (
	uaVSCodeVersion = "1.107.1"
	uaProduct       = "TRAE SOLO CN"
	marketAID       = "787976"
	sdkType         = "3"
	headerPkgType   = "stable_cn"
	userRegion      = "CN"
	appVersionHdr   = "0.1.45"
)

// UserAgent 完整 UA：VSCode <版本> (<产品名>)。
func UserAgent() string {
	return fmt.Sprintf("VSCode %s (%s)", uaVSCodeVersion, uaProduct)
}

// MarketClientID x-market-client-id：只有 VSCode <版本>，不带产品名。
func MarketClientID() string {
	return "VSCode " + uaVSCodeVersion
}

// PlatformTag x-lscbd-platform 取值：按真实运行平台上报（源参考实现写死 windows）。
func PlatformTag() string {
	switch runtime.GOOS {
	case "windows":
		return "windows"
	case "darwin":
		return "macos"
	}
	return "linux"
}

// TraceID 生成 x-tt-trace-id：00-<16hex>-01。
func TraceID() string {
	return "00-" + RandomHex(8)[:16] + "-01"
}

// DeviceIdentity 请求头里的账号设备标识（一账号一设备隔离的核心：
// 沿用同一套 x-device-id 会让上游把所有账号关联到同一设备，风控随之触发）。
type DeviceIdentity struct {
	// DeviceID x-device-id。
	DeviceID string
	// MarketUserID x-market-user-id（可空）。
	MarketUserID string
	// SessionID vscode-sessionid（可空）。
	SessionID string
}

// BuildHeaders 构造签到 / 状态 / 积分接口共用的请求头。
//
// 每个账号使用自己的设备标识与 session。刻意不发送 accept-encoding：
// 声明 gzip 而不解压会让响应变成不可解析的二进制，交给传输层自行协商。
func BuildHeaders(jwtValue string, device DeviceIdentity) map[string]string {
	return map[string]string{
		"accept":             "*/*",
		"accept-language":    "zh-CN",
		"authorization":      AuthorizationHeader(jwtValue),
		"content-type":       "application/json",
		"user-agent":         UserAgent(),
		"x-market-client-id": MarketClientID(),
		"x-market-user-id":   device.MarketUserID,
		"x-user-region":      userRegion,
		"x-device-id":        device.DeviceID,
		"x-lgw-req-sdk-type": sdkType,
		"package-type":       headerPkgType,
		"x-request-id":       newUUIDLike(),
		"x-lscbd-aid":        marketAID,
		"x-lscbd-platform":   PlatformTag(),
		"app-version":        appVersionHdr,
		"x-tt-trace-id":      TraceID(),
		"vscode-sessionid":   device.SessionID,
		"sec-fetch-dest":     "empty",
		"sec-fetch-mode":     "no-cors",
		"sec-fetch-site":     "none",
	}
}

// postAccountBase 对该变体的 account_base 发一个 POST（体固定为 `{}`，与源实现一致），
// 返回 (HTTP 状态码, 响应体文本)。网络层失败返回 status = 0。
func postAccountBase(ctx context.Context, httpClient *http.Client, variant Variant,
	path, jwtValue string, device DeviceIdentity) (int, string) {

	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	url := variant.Endpoints().AccountBase + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader([]byte("{}")))
	if err != nil {
		return 0, err.Error()
	}
	for key, value := range BuildHeaders(jwtValue, device) {
		req.Header.Set(key, value)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, err.Error()
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, ""
	}
	return resp.StatusCode, string(body)
}

// postAccountBaseParsed POST 并解析 JSON。网络失败或非 JSON 响应时返回 Err。
func postAccountBaseParsed(ctx context.Context, httpClient *http.Client, variant Variant,
	path, jwtValue string, device DeviceIdentity) (int, map[string]any, error) {

	status, body := postAccountBase(ctx, httpClient, variant, path, jwtValue, device)
	if status == 0 {
		return 0, nil, fmt.Errorf("%s", body)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		snippet := body
		if len(snippet) > 200 {
			// 截断长文本用于错误文案（按字符语义取前 200 字节以内即可定位）。
			snippet = snippet[:200]
		}
		return status, nil, fmt.Errorf("HTTP %d: 非 JSON 响应: %s", status, snippet)
	}
	return status, parsed, nil
}

// round2 保留两位小数。
func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

// CreditPackage 单个积分包明细。
type CreditPackage struct {
	// PackageCode 包编码（上游字段缺失时为空）。
	PackageCode string `json:"package_code,omitempty"`
	// PackageName 包名（上游字段缺失时为空）。
	PackageName string `json:"package_name,omitempty"`
	// Total 包总额度（credits_limit）。
	Total float64 `json:"total"`
	// Remaining 剩余额度（credits_limit - usage.credits_amount，下限 0）。
	Remaining float64 `json:"remaining"`
	// Used 已用额度（usage.credits_amount，无 usage 视为 0）。
	Used float64 `json:"used"`
	// ExpireAt 到期时间（Unix 秒；0 = 无）。
	ExpireAt int64 `json:"expire_at"`
	// Expired 是否已过期（expire_at 存在且 < now）。
	Expired bool `json:"expired"`
	// ExpiringSoon 是否 7 天内到期（未过期且 expire_at - now <= 7 天）。
	ExpiringSoon bool `json:"expiring_soon"`
}

// expiringSoonWindow 「7 天内到期」的判定窗口（秒）。
const expiringSoonWindow int64 = 7 * 24 * 3600

// Entitlements 剩余积分查询结果。
type Entitlements struct {
	// Total 剩余积分合计（各包 max(limit-used, 0) 之和）。
	Total float64 `json:"total"`
	// PurchasedToday 今日新购入额度（北京时间口径切日）。
	PurchasedToday float64 `json:"purchased_today"`
	// EarliestExpire 最早到期的**有剩余**包的到期时刻（Unix 秒；HasExpire=false 表示无）。
	EarliestExpire int64 `json:"earliest_expire"`
	// HasExpire 是否存在有效的最早到期时刻。
	HasExpire bool `json:"has_expire"`
	// Packages 逐包明细。
	Packages []CreditPackage `json:"packages"`
}

// QueryEntitlements 查询账号剩余积分（POST ide_user_ent_usage，请求体 `{}`）。
//
// 只统计 entitlement_base_info.quota.credits_limit 存在的包——无额度上限的包
// 不参与剩余量统计（可能是无限制资源）。
func QueryEntitlements(ctx context.Context, httpClient *http.Client, variant Variant,
	jwtValue string, device DeviceIdentity) (*Entitlements, error) {

	_, body, err := postAccountBaseParsed(ctx, httpClient, variant, EntitlementPath, jwtValue, device)
	if err != nil {
		return nil, err
	}
	packs, ok := body["user_entitlement_pack_list"].([]any)
	if !ok {
		return nil, fmt.Errorf("响应中缺少 user_entitlement_pack_list")
	}
	return BuildEntitlements(packs, time.Now()), nil
}

// BuildEntitlements 由 user_entitlement_pack_list 元素聚合剩余积分（纯函数，便于单测）。
// 「今日购入」按固定 UTC+8 切日：上游时间戳按北京时间切日，用本地时区会算错。
func BuildEntitlements(packs []any, now time.Time) *Entitlements {
	nowTs := now.Unix()
	cst := time.FixedZone("CST", 8*3600)
	todayStart := time.Date(now.In(cst).Year(), now.In(cst).Month(), now.In(cst).Day(), 0, 0, 0, 0, cst).Unix()
	todayEnd := todayStart + 86400

	result := &Entitlements{Packages: ParseCreditPackages(packs, nowTs)}
	for _, pack := range packs {
		object, ok := pack.(map[string]any)
		if !ok {
			continue
		}
		limit := packCreditsLimit(object)
		if !limit.ok {
			// 无额度上限的包不参与剩余量统计。
			continue
		}
		used := packCreditsUsed(object)
		result.Total += math.Max(limit.value-used, 0)

		base, _ := object["entitlement_base_info"].(map[string]any)
		chargeAmount, _ := base["charge_amount"].(float64)
		if chargeAmount > 0 {
			if startTime, _ := base["start_time"].(float64); startTime >= float64(todayStart) && startTime < float64(todayEnd) {
				result.PurchasedToday += limit.value
			}
		}
	}
	result.Total = round2(result.Total)
	result.PurchasedToday = round2(result.PurchasedToday)
	if expire, ok := EarliestLiveExpire(packs, nowTs); ok {
		result.EarliestExpire = expire
		result.HasExpire = true
	}
	return result
}

// floatOK 一个「存在且有效」的浮点取值。
type floatOK struct {
	value float64
	ok    bool
}

func packCreditsLimit(pack map[string]any) floatOK {
	base, _ := pack["entitlement_base_info"].(map[string]any)
	quota, _ := base["quota"].(map[string]any)
	if v, ok := quota["credits_limit"].(float64); ok {
		return floatOK{value: v, ok: true}
	}
	return floatOK{}
}

func packCreditsUsed(pack map[string]any) float64 {
	usage, _ := pack["usage"].(map[string]any)
	v, _ := usage["credits_amount"].(float64)
	return v
}

// ParseCreditPackages 由一组 user_entitlement_pack_list 元素解析逐包明细（纯函数）。
func ParseCreditPackages(packs []any, nowTs int64) []CreditPackage {
	packages := make([]CreditPackage, 0, len(packs))
	for _, pack := range packs {
		object, ok := pack.(map[string]any)
		if !ok {
			continue
		}
		limit := packCreditsLimit(object)
		if !limit.ok {
			continue
		}
		used := packCreditsUsed(object)
		remaining := math.Max(limit.value-used, 0)

		var expireAt int64
		expired, expiringSoon := false, false
		if expire, ok := object["expire_time"].(float64); ok {
			expireAt = int64(expire)
			expired = expireAt > 0 && expireAt < nowTs
			expiringSoon = !expired && expireAt-nowTs <= expiringSoonWindow
		}

		var base map[string]any
		base, _ = object["entitlement_base_info"].(map[string]any)
		packages = append(packages, CreditPackage{
			PackageCode:  baseString(base, "package_code", "package_type"),
			PackageName:  baseString(base, "package_name", "name"),
			Total:        round2(limit.value),
			Remaining:    round2(remaining),
			Used:         round2(used),
			ExpireAt:     expireAt,
			Expired:      expired,
			ExpiringSoon: expiringSoon,
		})
	}
	return packages
}

// baseString 从 entitlement_base_info 里依次尝试取非空字符串字段。
func baseString(base map[string]any, keys ...string) string {
	for _, key := range keys {
		if v, ok := base[key].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// EarliestLiveExpire 「最早到期」：只在还有剩余、且 expire_time > now 的包里取
// （纯函数）。已用完的包只要 expire_time 未到仍带着到期时刻，把它算进去会把
// 账号伪装成快过期（issue #7）；已过期的包额度已作废，同样不参与。
func EarliestLiveExpire(packs []any, nowTs int64) (int64, bool) {
	var earliest int64
	found := false
	for _, pack := range packs {
		object, ok := pack.(map[string]any)
		if !ok {
			continue
		}
		limit := packCreditsLimit(object)
		if !limit.ok {
			continue
		}
		if math.Max(limit.value-packCreditsUsed(object), 0) <= 0 {
			// 已用完的包不得参与「最早到期」。
			continue
		}
		if expire, ok := object["expire_time"].(float64); ok && int64(expire) > nowTs {
			e := int64(expire)
			if !found || e < earliest {
				earliest = e
				found = true
			}
		}
	}
	return earliest, found
}

// ClassifyError 把签到 / 额度接口的失败映射成 (错误类型, 冷却秒数)，供账号层决定冷却。
//   - 200 + 业务码 1005：套餐额度限制，半天内不会有变化；
//   - SessionDead 的冷却秒数约定为 -1（永久）。
func ClassifyError(httpStatus int, code *int64) (string, int64) {
	if httpStatus == 200 && code != nil && *code == 1005 {
		return "PlanLimit", 43200
	}
	switch {
	case httpStatus == 429:
		return "SoftRate", 60
	case httpStatus == 401:
		return "SessionDead", -1
	case httpStatus == 404:
		return "NotFound", 60
	case httpStatus >= 500 && httpStatus <= 599:
		return "Server", 600
	case httpStatus >= 400 && httpStatus <= 499:
		return "Client", 600
	default:
		if code != nil && *code != 0 {
			return "BusinessError", 300
		}
		return "Unknown", 0
	}
}
