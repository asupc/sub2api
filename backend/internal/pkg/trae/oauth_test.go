//go:build unit

package trae

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// redirectClient 构造一个把**所有**请求都改写到测试服务器的 http.Client。
// 交换链的兜底变体指向固定的 icube / account 域，无法通过参数注入测试服务器，
// 用改写 URL 的 RoundTripper 让全部请求落在本地 mock 上（源实现的 e2e 思路）。
// 注意不能用 http.Transport.Proxy：对 https 目标那会走 CONNECT 隧道，mock 收不到。
func redirectClient(t *testing.T, server *httptest.Server) *http.Client {
	t.Helper()
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{
		Transport: &rewriteTransport{target: target, base: http.DefaultTransport},
	}
}

// rewriteTransport 把请求 URL 的 scheme/host 改写为 mock 服务器后再转发。
type rewriteTransport struct {
	target *url.URL
	base   http.RoundTripper
}

func (t *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	cloned := req.Clone(req.Context())
	cloned.URL.Scheme = t.target.Scheme
	cloned.URL.Host = t.target.Host
	cloned.Host = ""
	return t.base.RoundTrip(cloned)
}

// TestBuildAuthorizeURLParams ★ 授权 URL 全参数断言（逐字对齐源 oauth.rs 的 22 参数
// + SOLO 线追加 hide_saas_login）。授权页域按区域分家、client_id 按产品线分家。
func TestBuildAuthorizeURLParams(t *testing.T) {
	facts := DeviceFacts{
		DeviceID:    "dev-1",
		MachineID:   "mach-1",
		DeviceModel: "System Product Name",
		OSName:      "windows",
		OSVersion:   "Windows 11 Pro",
	}
	cases := []struct {
		variant       Variant
		console       string
		authFrom      string
		clientID      string
		hideSaasLogin bool
	}{
		{VariantSoloCN, "https://www.trae.cn", "solo", "en1oxy7wnw8j9n", true},
		{VariantTraeCN, "https://www.trae.cn", "trae", "ono9krqynydwx5", false},
		{VariantSoloGlobal, "https://www.trae.ai", "solo", "en1oxy7wnw8j9n", true},
		{VariantTraeGlobal, "https://www.trae.ai", "trae", "ono9krqynydwx5", false},
	}
	for _, tc := range cases {
		raw := BuildAuthorizeURL(tc.variant, facts, 0, "trace-1", "challenge-1", ClientOptions{})
		if !strings.HasPrefix(raw, tc.console+"/authorization?") {
			t.Errorf("%s 授权页域 = %s, want %s", tc.variant, raw, tc.console)
		}
		for _, want := range []string{
			"login_version=1",
			"auth_from=" + tc.authFrom,
			"login_channel=native_ide",
			"plugin_version=2.3.83560",
			"auth_type=local",
			"client_id=" + tc.clientID,
			"redirect=0",
			"login_trace_id=trace-1",
			"auth_callback_url=" + url.QueryEscape("http://127.0.0.1:17388/authorize"),
			"machine_id=mach-1",
			"device_id=dev-1",
			"x_device_id=dev-1",
			"x_machine_id=mach-1",
			"x_env=",
			"x_app_version=3.3.100",
			"x_app_type=stable",
			"code_challenge=challenge-1",
			"code_challenge_method=S256",
			"channel_name=common",
		} {
			// percentEncode 的空格是 %20，QueryEscape 的是 +，统一按 %20 断言。
			want = strings.ReplaceAll(want, "+", "%20")
			if !strings.Contains(raw, want) {
				t.Errorf("%s 授权 URL 缺少参数 %s\n%s", tc.variant, want, raw)
			}
		}
		if strings.Contains(raw, "x_device_brand=System%20Product%20Name") != true {
			t.Errorf("%s x_device_brand 应为 URL 编码的设备型号", tc.variant)
		}
		if got := strings.Contains(raw, "hide_saas_login=true"); got != tc.hideSaasLogin {
			t.Errorf("%s hide_saas_login 出现 = %v, want %v", tc.variant, got, tc.hideSaasLogin)
		}
	}
}

// TestBuildAuthorizeURLClientIDOverride client_id 覆盖（上游换钥匙的逃生口）。
func TestBuildAuthorizeURLClientIDOverride(t *testing.T) {
	raw := BuildAuthorizeURL(VariantTraeCN, DeviceFacts{}, 0, "t", "c",
		ClientOptions{ClientIDTrae: "trae-x", ClientIDSolo: "solo-x"})
	if !strings.Contains(raw, "client_id=trae-x") {
		t.Errorf("TRAE 线覆盖未生效: %s", raw)
	}
	raw = BuildAuthorizeURL(VariantSoloCN, DeviceFacts{}, 0, "t", "c",
		ClientOptions{ClientIDTrae: "trae-x", ClientIDSolo: "solo-x"})
	if !strings.Contains(raw, "client_id=solo-x") {
		t.Errorf("SOLO 线覆盖未生效: %s", raw)
	}
	// 空白覆盖视为未设，回落内置默认。
	if got := (ClientOptions{ClientIDSolo: "  "}).ClientIDFor(OAuthLineSolo); got != "en1oxy7wnw8j9n" {
		t.Errorf("空白覆盖应回落默认, got %s", got)
	}
}

func TestPKCEPair(t *testing.T) {
	verifier, challenge := PKCEPair()
	if len(verifier) != 128 {
		t.Errorf("verifier 长度 = %d, want 128（64 字节 hex）", len(verifier))
	}
	if challenge == "" || strings.ContainsAny(challenge, "+/=") {
		t.Errorf("challenge 应为 base64url 无填充: %s", challenge)
	}
	if v2, _ := PKCEPair(); v2 == verifier {
		t.Error("两次生成的 verifier 必须不同")
	}
}

// TestParseCallbackAuthCode 主路径：authCodeInfo（URL 编码 JSON）+ host/userInfo。
func TestParseCallbackAuthCode(t *testing.T) {
	authCodeInfo, _ := json.Marshal(map[string]any{"AuthCode": "ac-1"})
	userInfo, _ := json.Marshal(map[string]any{
		"UserID": "u-1", "ScreenName": "张三", "AvatarUrl": "https://a/b.png",
	})
	raw := "authCodeInfo=" + url.QueryEscape(string(authCodeInfo)) +
		"&userInfo=" + url.QueryEscape(string(userInfo)) +
		"&host=https://api.trae.cn&userRegion=CN&loginTraceID=trace-1"

	info, err := ParseCallback(raw)
	if err != nil {
		t.Fatal(err)
	}
	if info.AuthCode != "ac-1" {
		t.Errorf("AuthCode = %q", info.AuthCode)
	}
	if info.Host != "https://api.trae.cn" || info.UserRegion != "CN" || info.LoginTraceID != "trace-1" {
		t.Errorf("info = %+v", info)
	}
	if info.UserID != "u-1" || info.DisplayName != "张三" || info.Avatar != "https://a/b.png" {
		t.Errorf("user info = %+v", info)
	}
}

// TestParseCallbackMalformedAuthCode 解码失败必须报错，不得吞掉。
func TestParseCallbackMalformedAuthCode(t *testing.T) {
	if _, err := ParseCallback("authCodeInfo=not-json"); err == nil {
		t.Error("非法 authCodeInfo 应报错")
	}
}

// TestParseCallbackRefreshTokenFallback code 兜底 + refreshToken 历史键名。
func TestParseCallbackRefreshTokenFallback(t *testing.T) {
	info, err := ParseCallback("code=std-code&refresh_token=%22tok%22")
	if err != nil {
		t.Fatal(err)
	}
	if info.AuthCode != "std-code" {
		t.Errorf("code 兜底失败: %q", info.AuthCode)
	}
	if info.RefreshToken != "tok" {
		t.Errorf("refresh_token（含剥引号）= %q", info.RefreshToken)
	}
	if !HasCredentialMarker(ParseCallbackQuery("code=x")) {
		t.Error("code 是凭据标记")
	}
	if HasCredentialMarker(ParseCallbackQuery("foo=1")) {
		t.Error("非凭据参数不应被当成回调")
	}
}

// TestParseCallbackQueryDoubleEncoded 编码过的参数必须解得开
// （源实现踩过的坑：先按 & 切、再反复解码、最后按第一个 = 切键值——
// 在解码前切 = 会让整段变成一个键，get("refreshToken") 恒为空）。
func TestParseCallbackQueryDoubleEncoded(t *testing.T) {
	// 单次编码：refreshToken%3Dtok（键与值被整体编码成一个段）。
	params := ParseCallbackQuery("refreshToken%3Dtok&userRegion=CN")
	if params["refreshToken"] != "tok" {
		t.Errorf("refreshToken = %q, want tok", params["refreshToken"])
	}
	if params["userRegion"] != "CN" {
		t.Errorf("userRegion = %q", params["userRegion"])
	}
	// 二次编码（% 被再编码成 %25）：反复解码（≤3 次）后仍要解出键值。
	params = ParseCallbackQuery("refreshToken%253Dtok")
	if params["refreshToken"] != "tok" {
		t.Errorf("二次编码 refreshToken = %q, want tok", params["refreshToken"])
	}
	// 值里的 = 不被截断（JWT 填充场景）。
	params = ParseCallbackQuery("t=abc=def==")
	if params["t"] != "abc=def==" {
		t.Errorf("t = %q", params["t"])
	}
	// 裸段按键存在处理。
	params = ParseCallbackQuery("flag&x=1")
	if _, ok := params["flag"]; !ok {
		t.Error("裸段 flag 应存在")
	}
}

func TestVerifyLoginTraceBranches(t *testing.T) {
	if err := VerifyLoginTrace("loginTraceID=t1", "t1"); err != nil {
		t.Errorf("① 相等应放行: %v", err)
	}
	if err := VerifyLoginTrace("loginTraceID=t2", "t1"); err == nil {
		t.Error("② 不等应拒绝")
	}
	if err := VerifyLoginTrace("loginTraceID=t1", ""); err != nil {
		t.Errorf("③ 有回调无会话应放行: %v", err)
	}
	if err := VerifyLoginTrace("foo=1", "t1"); err == nil {
		t.Error("④ 无回调有会话应拒绝")
	}
	if err := VerifyLoginTrace("foo=1", ""); err != nil {
		t.Errorf("⑤ 都没有应放行: %v", err)
	}
}

func TestResolveExchangeHost(t *testing.T) {
	if host, fallback := ResolveExchangeHost(" https://api.trae.cn/ ", VariantSoloCN); host != "https://api.trae.cn" || fallback {
		t.Errorf("host = %q fallback = %v", host, fallback)
	}
	if host, fallback := ResolveExchangeHost("", VariantSoloCN); host != "https://api.trae.cn" || !fallback {
		t.Errorf("CN 回落 = %q fallback = %v", host, fallback)
	}
	if host, _ := ResolveExchangeHost("  ", VariantSoloGlobal); host != "https://grow-normal.trae.ai" {
		t.Errorf("intl 回落 = %q", host)
	}
}

// exchangeCapture 记录一次交换请求的观测数据。
type exchangeCapture struct {
	path    string
	headers http.Header
	body    map[string]any
}

// serveExchange 起 mock 交换服务器：首个 handler 的响应由 respond 决定。
func serveExchange(t *testing.T, respond func(w http.ResponseWriter, r *http.Request)) (*httptest.Server, *[]exchangeCapture) {
	t.Helper()
	captures := &[]exchangeCapture{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		*captures = append(*captures, exchangeCapture{path: r.URL.Path, headers: r.Header.Clone(), body: body})
		respond(w, r)
	}))
	t.Cleanup(server.Close)
	return server, captures
}

// TestExchangeAuthCodeSameLine ★★ 授权与交换必须同线（同一 client_id）：
// SOLO 变体的交换请求体 ClientID 必须与授权 URL 用的是同一把钥匙。
func TestExchangeAuthCodeSameLine(t *testing.T) {
	server, captures := serveExchange(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Result":{"AccessToken":"access-1","RefreshToken":"refresh-1"}}`))
	})
	facts := DeviceFacts{DeviceID: "dev-1", MachineID: "mach-1", DeviceModel: "M", OSName: "windows", OSVersion: "W"}

	cases := []struct {
		variant    Variant
		clientID   string
		platform   string
		consoleURL string
	}{
		{VariantSoloCN, "en1oxy7wnw8j9n", "SOLO_PC", BuildAuthorizeURL(VariantSoloCN, facts, 0, "t", "c", ClientOptions{})},
		{VariantTraeCN, "ono9krqynydwx5", "IDE_PC", BuildAuthorizeURL(VariantTraeCN, facts, 0, "t", "c", ClientOptions{})},
	}
	for _, tc := range cases {
		*captures = nil
		token, err := ExchangeAuthCode(context.Background(), redirectClient(t, server), tc.variant,
			"https://mock.host", "ac-1", "verifier-1", facts, ClientOptions{})
		if err != nil {
			t.Fatalf("%s 交换失败: %v", tc.variant, err)
		}
		if token.AccessToken != "access-1" || token.RefreshToken != "refresh-1" {
			t.Errorf("token = %+v", token)
		}
		if got := token.Authorization(); got != "Cloud-IDE-JWT access-1" {
			t.Errorf("Authorization = %q", got)
		}

		capture := (*captures)[0]
		if capture.path != "/trae/api/v3/oauth/ExchangeToken" {
			t.Errorf("主路径 = %s", capture.path)
		}
		// ★ 同线断言：请求体 ClientID 与授权 URL 的 client_id 是同一把钥匙。
		if capture.body["ClientID"] != tc.clientID {
			t.Errorf("%s 交换体 ClientID = %v, want %s（必须与授权 URL 同线）", tc.variant, capture.body["ClientID"], tc.clientID)
		}
		if !strings.Contains(tc.consoleURL, "client_id="+tc.clientID) {
			t.Errorf("%s 授权 URL 的 client_id 不是 %s", tc.variant, tc.clientID)
		}
		// PlatformCode 同样按线派生。
		deviceInfo := capture.body["DeviceInfo"].(map[string]any)
		if deviceInfo["PlatformCode"] != tc.platform {
			t.Errorf("PlatformCode = %v, want %s", deviceInfo["PlatformCode"], tc.platform)
		}
		if deviceInfo["DeviceID"] != "dev-1" || deviceInfo["MachineID"] != "mach-1" {
			t.Errorf("DeviceInfo 与 facts 不同源: %v", deviceInfo)
		}
		if capture.body["IDEVersion"] != "3.3.100" {
			t.Errorf("IDEVersion = %v", capture.body["IDEVersion"])
		}
		// 请求头：主路径必须带空串 x-cloudide-token 与产品线 platform_code。
		if got := capture.headers.Get("x-cloudide-token"); got != "" {
			t.Errorf("x-cloudide-token = %q, want 空串", got)
		}
		if got := capture.headers.Get("x-platform-code"); got != tc.platform {
			t.Errorf("x-platform-code = %q", got)
		}
		if got := capture.headers.Get("x-app-id"); got != "6eefa01c-1036-4c7e-9ca5-d891f63bfcd8" {
			t.Errorf("x-app-id = %q", got)
		}
		if got := capture.headers.Get("x-device-id"); got != "dev-1" {
			t.Errorf("x-device-id = %q", got)
		}
	}
}

// TestExchangeAuthCodeErrorEnvelope 火山信封错误必须转成可读错误。
func TestExchangeAuthCodeErrorEnvelope(t *testing.T) {
	server, captures := serveExchange(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ResponseMetadata":{"Error":{"Code":"20403","Message":"Token device not match","StandardCode":"040036"}}}`))
	})
	_, err := ExchangeAuthCode(context.Background(), redirectClient(t, server), VariantSoloCN,
		"https://mock.host", "ac", "ver", DeviceFacts{}, ClientOptions{})
	if err == nil {
		t.Fatal("应报错")
	}
	if !strings.Contains(err.Error(), "20403/040036") || !strings.Contains(err.Error(), "Token device not match") {
		t.Errorf("错误文案 = %v", err)
	}
	// 三条变体链都应被尝试（主路径 + 两个旧端点兜底）。
	if len(*captures) != 3 {
		t.Errorf("变体链 = %d 条, want 3", len(*captures))
	}
	wantPaths := []string{
		"/trae/api/v3/oauth/ExchangeToken",
		"/cloudide/api/v3/trae/oauth/ExchangeToken",
		"/cloudide/api/v3/trae/oauth/ExchangeToken",
	}
	for i, want := range wantPaths {
		if (*captures)[i].path != want {
			t.Errorf("链[%d].path = %s, want %s", i, (*captures)[i].path, want)
		}
	}
	// 兜底变体不带 x-cloudide-token 头、不带 DeviceInfo。
	if (*captures)[1].headers.Get("x-cloudide-token") != "" {
		t.Error("兜底变体不应带 x-cloudide-token")
	}
	if _, exists := (*captures)[1].body["DeviceInfo"]; exists {
		t.Error("兜底变体不应带 DeviceInfo")
	}
	if (*captures)[1].body["AuthCode"] != "ac" {
		t.Error("兜底变体 AuthCode 键错误")
	}
	if (*captures)[2].body["Code"] != "ac" {
		t.Error("第二条兜底变体应用 Code 键")
	}
}

// TestExchangeAuthCodeRequiresRefresh 只有 access 没有 refresh 时必须拒绝
// （没有 refresh_token 的账号无法自动续期，等于把问题推迟到 24 小时后爆发）。
func TestExchangeAuthCodeRequiresRefresh(t *testing.T) {
	server, _ := serveExchange(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"access_token":"only-access"}}`))
	})
	_, err := ExchangeAuthCode(context.Background(), redirectClient(t, server), VariantTraeCN,
		"https://mock.host", "ac", "ver", DeviceFacts{}, ClientOptions{})
	if err == nil || !strings.Contains(err.Error(), "RefreshToken") {
		t.Errorf("缺 refresh 应拒绝, got %v", err)
	}
}

// TestExchangeAuthCodeNestedTokenLookup token 字段大小写不敏感、嵌套任意层都能找到。
func TestExchangeAuthCodeNestedTokenLookup(t *testing.T) {
	server, _ := serveExchange(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"list":[{"jwt":"nested-jwt","REFRESHTOKEN":"nested-refresh"}]}}`))
	})
	token, err := ExchangeAuthCode(context.Background(), redirectClient(t, server), VariantSoloCN,
		"https://mock.host", "ac", "ver", DeviceFacts{}, ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if token.AccessToken != "nested-jwt" || token.RefreshToken != "nested-refresh" {
		t.Errorf("token = %+v", token)
	}
}

// TestClientOptionsSameLine 授权与交换共用 ClientOptions 时，两处取值必然一致。
func TestClientOptionsSameLine(t *testing.T) {
	opts := ClientOptions{ClientIDSolo: "solo-key", ClientIDTrae: "trae-key"}
	for _, v := range []Variant{VariantSoloCN, VariantSoloGlobal} {
		url := BuildAuthorizeURL(v, DeviceFacts{}, 0, "t", "c", opts)
		if !strings.Contains(url, "client_id=solo-key") {
			t.Errorf("%s 授权 URL 应用 solo 覆盖", v)
		}
	}
	// base64 冒烟：PKCE challenge 可被标准解码器还原（base64url 无填充）。
	_, challenge := PKCEPair()
	if _, err := base64.RawURLEncoding.DecodeString(challenge); err != nil {
		t.Errorf("challenge 非法: %v", err)
	}
}
