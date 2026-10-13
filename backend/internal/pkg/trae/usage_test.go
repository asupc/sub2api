//go:build unit

package trae

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// accountCapture 记录一次 account_base 请求的观测数据。
type accountCapture struct {
	method string
	path   string
	body   string
	header http.Header
}

// serveAccountBase 起 mock account_base 服务器，记录请求观测。
func serveAccountBase(t *testing.T, respond func(w http.ResponseWriter, r *http.Request)) (*httptest.Server, *[]accountCapture) {
	t.Helper()
	captures := &[]accountCapture{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		*captures = append(*captures, accountCapture{
			method: r.Method,
			path:   r.URL.Path,
			body:   string(body),
			header: r.Header.Clone(),
		})
		respond(w, r)
	}))
	t.Cleanup(server.Close)
	return server, captures
}

// TestBuildHeadersFixedValues ★ 请求头固定取值逐字断言（抓包复刻的线上契约）。
func TestBuildHeadersFixedValues(t *testing.T) {
	headers := BuildHeaders("raw-jwt", DeviceIdentity{DeviceID: "dev-1", MarketUserID: "mu-1", SessionID: "sess-1"})
	cases := map[string]string{
		"accept":             "*/*",
		"accept-language":    "zh-CN",
		"authorization":      "Cloud-IDE-JWT raw-jwt",
		"content-type":       "application/json",
		"user-agent":         "VSCode 1.107.1 (TRAE SOLO CN)",
		"x-market-client-id": "VSCode 1.107.1",
		"x-market-user-id":   "mu-1",
		"x-user-region":      "CN",
		"x-device-id":        "dev-1",
		"x-lgw-req-sdk-type": "3",
		"package-type":       "stable_cn",
		"x-lscbd-aid":        "787976",
		"app-version":        "0.1.45",
		"vscode-sessionid":   "sess-1",
		"sec-fetch-dest":     "empty",
		"sec-fetch-mode":     "no-cors",
		"sec-fetch-site":     "none",
	}
	for key, want := range cases {
		if got := headers[key]; got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	// 生成型字段的形态校验。
	if got := headers["x-request-id"]; len(got) != 36 || !strings.Contains(got, "-") {
		t.Errorf("x-request-id 应为 UUID: %q", got)
	}
	if got := headers["x-tt-trace-id"]; !strings.HasPrefix(got, "00-") || !strings.HasSuffix(got, "-01") || len(got) != 22 {
		t.Errorf("x-tt-trace-id 形态错误: %q", got)
	}
	if got := headers["x-lscbd-platform"]; got != PlatformTag() {
		t.Errorf("x-lscbd-platform = %q", got)
	}
}

// TestQueryEntitlementsRequest ★ httptest 断言端点 / 方法 / 头 / 体。
func TestQueryEntitlementsRequest(t *testing.T) {
	server, captures := serveAccountBase(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"user_entitlement_pack_list":[]}`))
	})
	client := redirectClient(t, server)

	if _, err := QueryEntitlements(context.Background(), client, VariantSoloCN, "jwt-1",
		DeviceIdentity{DeviceID: "dev-1"}); err != nil {
		t.Fatal(err)
	}

	if len(*captures) != 1 {
		t.Fatalf("captures = %d", len(*captures))
	}
	capture := (*captures)[0]
	if capture.method != http.MethodPost {
		t.Errorf("method = %s", capture.method)
	}
	if capture.path != "/trae/api/v2/pay/ide_user_ent_usage" {
		t.Errorf("path = %s", capture.path)
	}
	if capture.body != "{}" {
		t.Errorf("body = %q, want {}", capture.body)
	}
	if got := capture.header.Get("Authorization"); got != "Cloud-IDE-JWT jwt-1" {
		t.Errorf("Authorization = %q", got)
	}
	if got := capture.header.Get("User-Agent"); got != "VSCode 1.107.1 (TRAE SOLO CN)" {
		t.Errorf("User-Agent = %q", got)
	}
	if got := capture.header.Get("X-Lscbd-Aid"); got != "787976" {
		t.Errorf("x-lscbd-aid = %q", got)
	}
	if got := capture.header.Get("Package-Type"); got != "stable_cn" {
		t.Errorf("package-type = %q", got)
	}
}

// TestQueryEntitlementsIntlBase 国际变体也必须打同一接口路径（端点按区域派生），
// 缺字段时报错而不是静默。
func TestQueryEntitlementsIntlBase(t *testing.T) {
	server, captures := serveAccountBase(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	})
	client := redirectClient(t, server)
	if _, err := QueryEntitlements(context.Background(), client, VariantTraeGlobal, "jwt",
		DeviceIdentity{DeviceID: "d"}); err == nil {
		t.Fatal("缺 user_entitlement_pack_list 应报错")
	}
	if len(*captures) != 1 || (*captures)[0].path != "/trae/api/v2/pay/ide_user_ent_usage" {
		t.Errorf("captures = %+v", *captures)
	}
}

func TestQueryEntitlementsParsing(t *testing.T) {
	server, _ := serveAccountBase(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"user_entitlement_pack_list":[
			{"entitlement_base_info":{"quota":{"credits_limit":100},"package_name":"月度包","charge_amount":1,"start_time":9999999999},"usage":{"credits_amount":30},"expire_time":9999999999},
			{"entitlement_base_info":{"quota":{"credits_limit":50}},"usage":{"credits_amount":50},"expire_time":9999999998},
			{"entitlement_base_info":{"name":"无上限包"}}
		]}`))
	})
	client := redirectClient(t, server)
	result, err := QueryEntitlements(context.Background(), client, VariantSoloCN, "jwt", DeviceIdentity{})
	if err != nil {
		t.Fatal(err)
	}
	// 总量 = max(100-30,0) + max(50-50,0)，无上限包不参与。
	if result.Total != 70 {
		t.Errorf("Total = %v, want 70", result.Total)
	}
	// 用完的包（剩余 0）不得参与「最早到期」。
	if !result.HasExpire || result.EarliestExpire != 9999999999 {
		t.Errorf("EarliestExpire = %v (has=%v), want 9999999999", result.EarliestExpire, result.HasExpire)
	}
	if len(result.Packages) != 2 {
		t.Fatalf("Packages = %d 条, want 2（无上限包跳过）", len(result.Packages))
	}
	first := result.Packages[0]
	if first.PackageName != "月度包" || first.Total != 100 || first.Remaining != 70 || first.Used != 30 {
		t.Errorf("first = %+v", first)
	}
	if first.Expired || first.ExpiringSoon {
		t.Error("远期包不应标记过期/临期")
	}
	if result.Packages[1].Remaining != 0 {
		t.Errorf("用完的包剩余应为 0: %+v", result.Packages[1])
	}
}

// TestBuildEntitlementsPurchasedToday 「今日购入」按 UTC+8 切日。
func TestBuildEntitlementsPurchasedToday(t *testing.T) {
	now := time.Now()
	cst := time.FixedZone("CST", 8*3600)
	todayStart := time.Date(now.In(cst).Year(), now.In(cst).Month(), now.In(cst).Day(), 0, 0, 0, 0, cst).Unix()
	packs := []any{
		map[string]any{
			"entitlement_base_info": map[string]any{
				"quota":         map[string]any{"credits_limit": 60.0},
				"charge_amount": 1.0,
				"start_time":    float64(todayStart + 3600),
			},
			"usage": map[string]any{"credits_amount": 0.0},
		},
		map[string]any{
			"entitlement_base_info": map[string]any{
				"quota":         map[string]any{"credits_limit": 40.0},
				"charge_amount": 1.0,
				"start_time":    float64(todayStart - 86400), // 昨天购入
			},
			"usage": map[string]any{"credits_amount": 0.0},
		},
	}
	result := BuildEntitlements(packs, now)
	if result.PurchasedToday != 60 {
		t.Errorf("PurchasedToday = %v, want 60", result.PurchasedToday)
	}
	if result.Total != 100 {
		t.Errorf("Total = %v, want 100", result.Total)
	}
}

// TestParseCreditPackagesExpiryFields 到期字段三态（过期 / 7 天内临期 / 正常）。
func TestParseCreditPackagesExpiryFields(t *testing.T) {
	now := time.Now().Unix()
	packs := []any{
		map[string]any{
			"entitlement_base_info": map[string]any{"quota": map[string]any{"credits_limit": 10.0}},
			"expire_time":           float64(now - 100),
		},
		map[string]any{
			"entitlement_base_info": map[string]any{"quota": map[string]any{"credits_limit": 10.0}},
			"expire_time":           float64(now + 3*24*3600),
		},
		map[string]any{
			"entitlement_base_info": map[string]any{"quota": map[string]any{"credits_limit": 10.0}},
		},
	}
	packages := ParseCreditPackages(packs, now)
	if !packages[0].Expired || packages[0].ExpiringSoon {
		t.Errorf("过期包 = %+v", packages[0])
	}
	if packages[1].Expired || !packages[1].ExpiringSoon {
		t.Errorf("临期包 = %+v", packages[1])
	}
	if packages[2].Expired || packages[2].ExpiringSoon {
		t.Errorf("无到期包 = %+v", packages[2])
	}
}

// TestEarliestLiveExpireExcludesEmptyAndExpired 已用完 / 已过期的包不参与最早到期。
func TestEarliestLiveExpireExcludesEmptyAndExpired(t *testing.T) {
	now := time.Now().Unix()
	packs := []any{
		map[string]any{ // 用完但有远期到期：必须排除（否则伪装成快过期）。
			"entitlement_base_info": map[string]any{"quota": map[string]any{"credits_limit": 5.0}},
			"usage":                 map[string]any{"credits_amount": 5.0},
			"expire_time":           float64(now + 3600),
		},
		map[string]any{ // 已过期：额度作废，不参与。
			"entitlement_base_info": map[string]any{"quota": map[string]any{"credits_limit": 5.0}},
			"usage":                 map[string]any{"credits_amount": 0.0},
			"expire_time":           float64(now - 3600),
		},
		map[string]any{ // 有剩余、明天到期：命中。
			"entitlement_base_info": map[string]any{"quota": map[string]any{"credits_limit": 5.0}},
			"usage":                 map[string]any{"credits_amount": 1.0},
			"expire_time":           float64(now + 86400),
		},
	}
	expire, ok := EarliestLiveExpire(packs, now)
	if !ok || expire != now+86400 {
		t.Errorf("expire = %d (ok=%v), want %d", expire, ok, now+86400)
	}
}

// TestClassifyError 错误分类表（决定账号冷却时长）。
func TestClassifyError(t *testing.T) {
	c1005 := int64(1005)
	c0 := int64(0)
	cases := []struct {
		status   int
		code     *int64
		want     string
		cooldown int64
	}{
		{200, &c1005, "PlanLimit", 43200},
		{429, nil, "SoftRate", 60},
		{401, nil, "SessionDead", -1},
		{404, nil, "NotFound", 60},
		{500, nil, "Server", 600},
		{503, nil, "Server", 600},
		{400, nil, "Client", 600},
		{200, &c0, "Unknown", 0},
		{200, nil, "Unknown", 0},
	}
	for _, tc := range cases {
		gotType, gotCooldown := ClassifyError(tc.status, tc.code)
		if gotType != tc.want || gotCooldown != tc.cooldown {
			t.Errorf("ClassifyError(%d, %v) = (%s, %d), want (%s, %d)",
				tc.status, tc.code, gotType, gotCooldown, tc.want, tc.cooldown)
		}
	}
	other := int64(2001)
	if gotType, _ := ClassifyError(200, &other); gotType != "BusinessError" {
		t.Errorf("非零业务码应为 BusinessError, got %s", gotType)
	}
}
