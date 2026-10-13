package trae

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// 授权页与交换端点的路径常量（出处：oauth.rs / mod.rs，抓包固化 2026-09-16）。
const (
	// authorizePath 授权页路径（挂在变体的 ConsoleBase 后面；路径与客户端
	// `${loginHost}/authorization` 的拼法逐字一致）。
	authorizePath = "/authorization"
	// callbackPath 本地回调路径（与构造进 auth_callback_url 的路径必须一致）。
	callbackPath = "/authorize"
	// callbackPort 本地回调监听端口。上游授权页在「认证中」阶段会从浏览器侧探测
	// 127.0.0.1:17388 确认客户端在线，探不到就永远停在「认证中」，因此必须固定。
	callbackPort = 17388
	// exchangeTokenPath AuthCode 交换主路径。完整 URL 由回调回传的 host 拼接。
	exchangeTokenPath = "/trae/api/v3/oauth/ExchangeToken"
	// exchangeTokenLegacyPath 旧 cloudide 交换端点（保留作兜底链末位）。
	exchangeTokenLegacyPath = "/cloudide/api/v3/trae/oauth/ExchangeToken"

	// pagePluginVersion 授权页 plugin_version 参数默认值（抓包固化）。
	pagePluginVersion = "2.3.83560"
	// pageAppVersion 授权页 x_app_version 参数默认值（抓包固化）。
	pageAppVersion = "3.3.100"
	// oauthAppID 云 IDE API 的 x-app-id 请求头取值（照抄参考实现，非机密）。
	oauthAppID = "6eefa01c-1036-4c7e-9ca5-d891f63bfcd8"
)

// DeviceFacts 一次登录用到的全部客户端侧事实：授权 URL 与 ExchangeToken 请求体里
// 四对同源字段（机器标识 / 应用版本 / 设备型号 / 系统版本）必须从这里、且只从
// 这里取值——两处各取一份来源会被上游以 20403/040036 Token device not match 拒绝。
type DeviceFacts struct {
	// DeviceID 授权 URL 的 device_id / x_device_id、请求体的 DeviceInfo.DeviceID 与
	// x-device-id 头三方同源。
	DeviceID string
	// MachineID 授权 URL 的 machine_id / x_machine_id 与请求体的 DeviceInfo.MachineID 同源。
	MachineID string
	// AppVersion x_app_version / ClientVersion / IDEVersion 共用；空 → pageAppVersion。
	AppVersion string
	// BuildVersion plugin_version；空 → pagePluginVersion。
	BuildVersion string
	// AppType x_app_type；空 → "stable"。
	AppType string
	// DeviceName 设备名（不在授权 URL 里，不参与同源比对，可空）。
	DeviceName string
	// DeviceModel x_device_brand 与请求体的 DeviceModel 同源。
	DeviceModel string
	// DeviceBrand 请求体的 DeviceBrand。
	DeviceBrand string
	// DeviceCPU 请求体的 DeviceCPU。
	DeviceCPU string
	// OSName x_device_type 与请求体的 OSInfo 同源（客户端报 "windows" 这类）。
	OSName string
	// OSVersion x_os_version 与请求体的 OSVersion 同源。
	OSVersion string
	// DevicePublicKeyPEM 请求体的 DevicePublicKey（AuthCode 路径不签名、不发 DeviceProof）。
	DevicePublicKeyPEM string
}

func (f DeviceFacts) appVersion() string {
	if v := strings.TrimSpace(f.AppVersion); v != "" {
		return v
	}
	return pageAppVersion
}

func (f DeviceFacts) buildVersion() string {
	if v := strings.TrimSpace(f.BuildVersion); v != "" {
		return v
	}
	return pagePluginVersion
}

func (f DeviceFacts) appType() string {
	if v := strings.TrimSpace(f.AppType); v != "" {
		return v
	}
	return "stable"
}

// ClientOptions client_id 覆盖（正常不设，是「上游换钥匙」的逃生口）。
// 授权与交换必须同线：同一变体在两处经 ClientIDFor 取到同一个值。
type ClientOptions struct {
	// ClientIDSolo SOLO 产品线覆盖；空 → 内置默认 en1oxy7wnw8j9n。
	ClientIDSolo string
	// ClientIDTrae TRAE 产品线覆盖；空 → 内置默认 ono9krqynydwx5。
	ClientIDTrae string
}

// ClientIDFor 取某条产品线实际要用的 client_id（覆盖优先，空串覆盖视为未设）。
func (o ClientOptions) ClientIDFor(line OAuthLine) string {
	perLine := o.ClientIDSolo
	if line == OAuthLineTrae {
		perLine = o.ClientIDTrae
	}
	if v := strings.TrimSpace(perLine); v != "" {
		return v
	}
	return line.DefaultClientID()
}

// RandomHex 生成 n 字节的随机 hex（2n 个字符）。trace_id 用 32、PKCE verifier 用 64。
func RandomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand 失败极罕见（系统熵源不可用）；退化用 sha256(随机时间) 兜底，
		// 保证流程可继续而不是 panic。
		seed := sha256.Sum256([]byte(fmt.Sprintf("%v", time.Now().UnixNano())))
		copy(buf, seed[:])
	}
	return hex.EncodeToString(buf)
}

// PKCEPair 生成 PKCE (verifier, challenge)：challenge = BASE64URL_NOPAD(SHA256(verifier))。
// verifier 是 RFC 7636 允许的 64 字节 hex（unreserved 字符集），只存内存、不落盘、不进日志。
func PKCEPair() (verifier, challenge string) {
	verifier = RandomHex(64)
	digest := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(digest[:])
	return verifier, challenge
}

// percentEncode 通用百分号编码（空格 → %20，unreserved 之外全部转义）。
// 授权页回跳参数里合法含有 `=`/`&`/非 ASCII（设备型号、OS 版本），必须整段编码。
func percentEncode(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
			b.WriteByte(c)
		case c == '-' || c == '_' || c == '.' || c == '~':
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// BuildAuthorizeURL 构造授权页 URL（22 参数逐字对齐抓包固化值；SOLO 线再追加
// 从属参数 hide_saas_login=true）。授权页域按区域分家（ConsoleBase），auth_from /
// client_id 按产品线分家（OAuthLine）——两个轴不要混。
//
// port 传 0 时用默认固定端口 17388。
func BuildAuthorizeURL(variant Variant, facts DeviceFacts, port int, traceID, codeChallenge string, opts ClientOptions) string {
	if port <= 0 {
		port = callbackPort
	}
	line := variant.OAuthLine()
	callback := fmt.Sprintf("http://127.0.0.1:%d%s", port, callbackPath)

	var b strings.Builder
	b.WriteString(variant.Endpoints().ConsoleBase)
	b.WriteString(authorizePath)
	b.WriteString("?login_version=1")
	b.WriteString("&auth_from=" + line.AuthFrom())
	b.WriteString("&login_channel=native_ide")
	b.WriteString("&plugin_version=" + facts.buildVersion())
	b.WriteString("&auth_type=local")
	b.WriteString("&client_id=" + opts.ClientIDFor(line))
	b.WriteString("&redirect=0")
	b.WriteString("&login_trace_id=" + traceID)
	b.WriteString("&auth_callback_url=" + percentEncode(callback))
	b.WriteString("&machine_id=" + facts.MachineID)
	b.WriteString("&device_id=" + facts.DeviceID)
	b.WriteString("&x_device_id=" + facts.DeviceID)
	b.WriteString("&x_machine_id=" + facts.MachineID)
	// x_device_brand 装的是设备型号（客户端原文 x_device_brand: deviceModel），不是主机名。
	b.WriteString("&x_device_brand=" + percentEncode(facts.DeviceModel))
	b.WriteString("&x_device_type=" + percentEncode(facts.OSName))
	b.WriteString("&x_os_version=" + percentEncode(facts.OSVersion))
	b.WriteString("&x_env=")
	b.WriteString("&x_app_version=" + facts.appVersion())
	b.WriteString("&x_app_type=" + facts.appType())
	b.WriteString("&code_challenge=" + codeChallenge)
	b.WriteString("&code_challenge_method=S256")
	b.WriteString("&channel_name=common")
	// hide_saas_login 是 auth_from=solo 的从属参数，追加在最末，与客户端拼串顺序一致。
	if line.HideSaasLogin() {
		b.WriteString("&hide_saas_login=true")
	}
	return b.String()
}

// CallbackInfo 授权页回调参数的解析结果。
type CallbackInfo struct {
	// AuthCode authCodeInfo.AuthCode（主路径）或 code（标准授权码）。
	AuthCode string
	// RefreshToken refreshToken（容错读，老形态回调直接给）。
	RefreshToken string
	// Host 交换端点主机（回调回传，https://api.trae.cn 形态）。
	Host string
	// UserRegion 用户区域。
	UserRegion string
	// LoginTraceID CSRF 回调值。
	LoginTraceID string
	// UserID 用户 ID（可能为空，还能从 JWT 解析）。
	UserID string
	// DisplayName 展示名。
	DisplayName string
	// Avatar 头像 URL。
	Avatar string
}

// credentialMarkers 「回调而非探测」的凭据标记：请求带上任一参数即为真正的回调。
var credentialMarkers = []string{
	"authCodeInfo", "code", "accessToken", "access_token", "refreshToken", "refresh_token",
}

// HasCredentialMarker 回调查询串里是否带凭据标记（不带的是授权页在线探测）。
func HasCredentialMarker(params map[string]string) bool {
	for _, marker := range credentialMarkers {
		if _, ok := params[marker]; ok {
			return true
		}
	}
	return false
}

// ParseCallbackQuery 把回调 URL 的查询串解析成键值对。
//
// 手写而不用 net/url：回调里只有一小组扁平参数，且必须容忍浏览器对
// auth_callback_url 的二次编码。三步顺序不可调整（源码踩过的坑）：
//  1. 先按 `&` 切分（值里被编码成 %26 的 & 不会被误切）；
//  2. 再整体解码每段（≤3 次，处理二/三次编码）；
//  3. 最后按第一个 `=` 切键值（JWT/refresh_token 的值里合法含有 =）。
func ParseCallbackQuery(rawQuery string) map[string]string {
	out := map[string]string{}
	for _, pair := range strings.Split(rawQuery, "&") {
		if pair == "" {
			continue
		}
		decoded := pair
		for i := 0; i < 3; i++ {
			next := percentDecode(decoded)
			if next == decoded {
				break
			}
			decoded = next
		}
		// 无 `=` 的裸段按「键存在、值为空」处理，不丢弃。
		key, value, found := strings.Cut(decoded, "=")
		if !found {
			value = ""
		}
		out[key] = value
	}
	return out
}

// percentDecode 最小百分号解码（%XX 与 +）。非法的 % 序列原样保留。
func percentDecode(input string) string {
	var b strings.Builder
	for i := 0; i < len(input); i++ {
		c := input[i]
		switch {
		case c == '%' && i+2 < len(input):
			if byte, err := parseHexByte(input[i+1], input[i+2]); err == nil {
				b.WriteByte(byte)
				i += 2
			} else {
				b.WriteByte(c)
			}
		case c == '+':
			b.WriteByte(' ')
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

func parseHexByte(hi, lo byte) (byte, error) {
	var out byte
	for _, c := range [2]byte{hi, lo} {
		out <<= 4
		switch {
		case c >= '0' && c <= '9':
			out |= c - '0'
		case c >= 'a' && c <= 'f':
			out |= c - 'a' + 10
		case c >= 'A' && c <= 'F':
			out |= c - 'A' + 10
		default:
			return 0, fmt.Errorf("invalid hex %q", string(c))
		}
	}
	return out, nil
}

// param 取首个非空字符串参数。
func param(params map[string]string, keys ...string) string {
	for _, key := range keys {
		if v := strings.TrimSpace(params[key]); v != "" {
			return v
		}
	}
	return ""
}

// displayNameKeys 展示名候选键（真实回调是 userInfo.ScreenName，其余为旧形态）。
var displayNameKeys = []string{
	"ScreenName", "NickName", "nickname", "nickName", "name", "userName", "email",
}

// ParseCallback 解析回调参数（AuthCode 主路径 + code/refreshToken 容错读）。
// authCodeInfo 是 URL 编码的 JSON，解码失败必须报错——吞掉会把「授权页改格式」
// 表现成「回调里没有凭据」。
func ParseCallback(rawQuery string) (CallbackInfo, error) {
	params := ParseCallbackQuery(rawQuery)
	info := CallbackInfo{
		Host:         param(params, "host"),
		UserRegion:   param(params, "userRegion", "user_region"),
		LoginTraceID: param(params, "loginTraceID", "login_trace_id"),
	}

	// ---- authCodeInfo（主路径）----
	if raw := strings.TrimSpace(params["authCodeInfo"]); raw != "" {
		var parsed struct {
			AuthCode string `json:"AuthCode"`
		}
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
			return info, fmt.Errorf("authCodeInfo 解析失败（%w）：授权页回调格式异常", err)
		}
		info.AuthCode = strings.TrimSpace(parsed.AuthCode)
	}
	// 标准授权码兜底（code）。
	if info.AuthCode == "" {
		info.AuthCode = param(params, "code")
	}

	// ---- refreshToken（容错读，键名三种历史拼法；值可能被包在 JSON 引号里）----
	for _, key := range []string{"refreshToken", "refresh_token", "RefreshToken"} {
		if v := params[key]; v != "" {
			cleaned := strings.Trim(strings.TrimSpace(v), "\"")
			if cleaned != "" {
				info.RefreshToken = cleaned
				break
			}
		}
	}

	// ---- userInfo（容错：解析失败不报错，只用于展示名兜底）----
	var userInfo map[string]any
	for _, key := range []string{"userInfo", "user_info", "UserInfo"} {
		if raw := strings.TrimSpace(params[key]); raw != "" {
			if err := json.Unmarshal([]byte(raw), &userInfo); err == nil {
				break
			}
		}
	}
	info.UserID = jsonObjectString(userInfo, "UserID", "userId")
	if info.UserID == "" {
		info.UserID = param(params, "UserID", "userId", "user_id")
	}
	for _, key := range displayNameKeys {
		if v := jsonObjectString(userInfo, key); v != "" {
			info.DisplayName = v
			break
		}
	}
	if info.DisplayName == "" {
		info.DisplayName = param(params, "userName", "user_name", "nickname")
	}
	info.Avatar = jsonObjectString(userInfo, "AvatarUrl")
	if info.Avatar == "" {
		info.Avatar = param(params, "avatar")
	}
	return info, nil
}

// jsonObjectString 从解析后的 JSON 对象里取非空字符串字段（依次尝试）。
func jsonObjectString(object map[string]any, keys ...string) string {
	for _, key := range keys {
		if v, ok := object[key].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// VerifyLoginTrace 回调 CSRF 校验。expected 为在途会话的 trace_id（空表示无在途会话）：
//   - 回调带 loginTraceID 且有在途会话：必须相等，否则报错；
//   - 回调带值、无在途会话：放行（重启后粘贴回调的场景）；
//   - 回调无值、有在途会话：拒绝（新协议回调必带该字段）；
//   - 都没有：宽容放行。
func VerifyLoginTrace(rawQuery string, expected string) error {
	params := ParseCallbackQuery(rawQuery)
	callbackTrace := param(params, "loginTraceID", "login_trace_id")
	expected = strings.TrimSpace(expected)
	switch {
	case callbackTrace != "" && expected != "":
		if callbackTrace != expected {
			return fmt.Errorf("回调与本机发起的登录不匹配（loginTraceID 校验失败），可能为伪造或重放，已拒绝")
		}
		return nil
	case callbackTrace != "":
		return nil
	case expected != "":
		return fmt.Errorf("回调缺少 loginTraceID，已拒绝")
	}
	return nil
}

// ResolveExchangeHost 解析交换端点主机：回调回传的 host 优先，缺失时回落该变体的
// account_base（真机客户端兑换打的就是 account.trae.normal，从不是 iCube 基址）。
// 第二个返回值表示是否用了回落值。
func ResolveExchangeHost(callbackHost string, variant Variant) (string, bool) {
	host := strings.Trim(strings.TrimSpace(callbackHost), "/")
	if host != "" {
		return host, false
	}
	return variant.Endpoints().AccountBase, true
}

// ExchangedToken 交换结果。
type ExchangedToken struct {
	// AccessToken 裸 JWT（无前缀）。
	AccessToken string
	// RefreshToken 刷新令牌。
	RefreshToken string
}

// Authorization 已组好的 Authorization 头值（Cloud-IDE-JWT <jwt>）。
func (t *ExchangedToken) Authorization() string {
	return AuthorizationHeader(t.AccessToken)
}

// buildExchangePayload 构造 AuthCode 交换主路径请求体。
// AuthCode 场景不发 DeviceProof（那是 refreshToken 刷新场景专属），
// DevicePublicKey 直接取信封里的 publicKeyPEM，不推导。
func buildExchangePayload(clientID, authCode, codeVerifier string, facts DeviceFacts, line OAuthLine) map[string]any {
	return map[string]any{
		"ClientID":     clientID,
		"AuthCode":     authCode,
		"CodeVerifier": codeVerifier,
		"DeviceInfo": map[string]any{
			"DeviceID":     facts.DeviceID,
			"MachineID":    facts.MachineID,
			"PlatformCode": line.PlatformCode(),
			"DeviceType":   "PC",
			"DeviceName":   facts.DeviceName,
			// 与授权 URL 的 x_device_brand 同源（客户端装的是 deviceModel）。
			"DeviceModel": facts.DeviceModel,
			// 与授权 URL 的 x_app_version 同源（客户端三处同值）。
			"ClientVersion":   facts.appVersion(),
			"DevicePublicKey": facts.DevicePublicKeyPEM,
			"DeviceBrand":     facts.DeviceBrand,
			"DeviceCPU":       facts.DeviceCPU,
			// 与授权 URL 的 x_device_type / x_os_version 同源。
			"OSInfo":    facts.OSName,
			"OSVersion": facts.OSVersion,
		},
		"IDEVersion": facts.appVersion(),
	}
}

// buildAuthCodeFallbackPayload 构造旧端点兜底请求体（无 DeviceProof、无公钥）。
// codeKey 取 "AuthCode" 或 "Code"（旧端点对字段名的两种历史拼法）。
func buildAuthCodeFallbackPayload(clientID, authCode, codeVerifier, deviceID, platformCode, codeKey string) map[string]any {
	return map[string]any{
		"ClientID":     clientID,
		codeKey:        authCode,
		"CodeVerifier": codeVerifier,
		"DeviceID":     deviceID,
		// 与主变体同源（按产品线派生），不写死。
		"PlatformCode": platformCode,
	}
}

// ExchangeAuthCode 用 AuthCode 交换 token（三变体链：主路径 + 两个旧端点兜底）。
//
// ★ 授权与交换必须同线（同一 client_id）：交换请求体的 ClientID 与授权 URL 用的
// 那把钥匙同源派生（variant.OAuthLine() → opts.ClientIDFor），拿 A 线的钥匙去兑
// B 线签发的 AuthCode 上游只会拒绝。AuthCode 场景要求 access 与 refresh 都存在
// （没有 refresh_token 的账号无法自动续期）。
//
// httpClient 为 nil 时用 http.DefaultClient。host 由 ResolveExchangeHost 得到。
func ExchangeAuthCode(ctx context.Context, httpClient *http.Client, variant Variant,
	host, authCode, codeVerifier string, facts DeviceFacts, opts ClientOptions) (*ExchangedToken, error) {

	line := variant.OAuthLine()
	clientID := opts.ClientIDFor(line)
	platformCode := line.PlatformCode()
	legacyURL := variant.Endpoints().IcubeBase + exchangeTokenLegacyPath

	type exchangeVariant struct {
		tag               string
		url               string
		payload           map[string]any
		withCloudideToken bool
	}
	variants := []exchangeVariant{
		{
			tag:               "ExchangeToken/DeviceInfo",
			url:               strings.TrimRight(host, "/") + exchangeTokenPath,
			payload:           buildExchangePayload(clientID, authCode, codeVerifier, facts, line),
			withCloudideToken: true,
		},
		{
			tag:     "ExchangeToken/AuthCode",
			url:     legacyURL,
			payload: buildAuthCodeFallbackPayload(clientID, authCode, codeVerifier, facts.DeviceID, platformCode, "AuthCode"),
		},
		{
			tag:     "ExchangeToken/Code",
			url:     legacyURL,
			payload: buildAuthCodeFallbackPayload(clientID, authCode, codeVerifier, facts.DeviceID, platformCode, "Code"),
		},
	}

	var errs []string
	for _, v := range variants {
		token, err := tryExchange(ctx, httpClient, v.url, v.payload, facts.DeviceID, platformCode, v.withCloudideToken)
		if err == nil {
			return token, nil
		}
		errs = append(errs, fmt.Sprintf("%s: %v", v.tag, err))
	}
	return nil, fmt.Errorf("全部交换变体失败 → %s", strings.Join(errs, " | "))
}

// accessTokenKeys / refreshTokenTokenKeys token 字段的候选键（响应包裹层不统一）。
var (
	accessTokenKeys  = []string{"AccessToken", "access_token", "token", "Jwt", "JWT"}
	refreshTokenKeys = []string{"RefreshToken", "refresh_token"}
)

// tryExchange 单个交换变体尝试。
func tryExchange(ctx context.Context, httpClient *http.Client, url string, payload map[string]any,
	deviceID, platformCode string, withCloudideToken bool) (*ExchangedToken, error) {

	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("x-device-id", deviceID)
	req.Header.Set("x-app-id", oauthAppID)
	req.Header.Set("x-platform-code", platformCode)
	if withCloudideToken {
		// 必须存在且为空串：缺失报 20403，带旧 token 报 20405。
		req.Header.Set("x-cloudide-token", "")
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败 (HTTP %d): %w", resp.StatusCode, err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("解析响应失败 (HTTP %d): %w", resp.StatusCode, err)
	}

	// 火山引擎标准信封：错误在 ResponseMetadata.Error.{Code,Message,StandardCode}。
	if metadata, ok := parsed["ResponseMetadata"].(map[string]any); ok {
		if errObject, ok := metadata["Error"].(map[string]any); ok {
			code, _ := errObject["Code"].(string)
			if code != "" && code != "0" {
				message, _ := errObject["Message"].(string)
				if message == "" {
					message = "未知错误"
				}
				standard, _ := errObject["StandardCode"].(string)
				return nil, fmt.Errorf("code=%s/%s: %s", code, standard, message)
			}
		}
	}
	// 兼容旧解析（顶层 code/message 形态）。
	if code, ok := parsed["code"].(float64); ok && code != 0 {
		message, _ := parsed["message"].(string)
		if message == "" {
			message = "未知错误"
		}
		return nil, fmt.Errorf("code=%.0f: %s", code, message)
	}

	access := findTokenValue(parsed, accessTokenKeys)
	refresh := findTokenValue(parsed, refreshTokenKeys)
	switch {
	case access != "" && refresh != "":
		return &ExchangedToken{AccessToken: access, RefreshToken: refresh}, nil
	case access != "":
		return nil, fmt.Errorf("响应缺少 RefreshToken（该账号将无法自动续期，已拒绝）")
	default:
		return nil, fmt.Errorf("响应中未找到 Token 字段（响应键：%s）", strings.Join(keyPaths(parsed), " | "))
	}
}

// findTokenValue 在 JSON 树中递归查找指定键（大小写不敏感）的首个非空字符串值。
// 先查 Result / data / Data 容器再全树深挖——上游响应包裹层在不同接口上并不统一。
func findTokenValue(value any, keys []string) string {
	var search func(v any) string
	search = func(v any) string {
		switch node := v.(type) {
		case map[string]any:
			for _, key := range keys {
				if s, ok := node[key].(string); ok && s != "" {
					return s
				}
			}
			for key, candidate := range node {
				for _, k := range keys {
					if strings.EqualFold(key, k) {
						if s, ok := candidate.(string); ok && s != "" {
							return s
						}
					}
				}
			}
			for _, candidate := range node {
				if found := search(candidate); found != "" {
					return found
				}
			}
		case []any:
			for _, item := range node {
				if found := search(item); found != "" {
					return found
				}
			}
		}
		return ""
	}

	for _, container := range []string{"Result", "data", "Data"} {
		if c, ok := value.(map[string]any); ok {
			if found := search(c[container]); found != "" {
				return found
			}
		}
	}
	return search(value)
}

// keyPaths 收集 JSON 的键路径（只有键名、不含值），用于「找不到 token」时的诊断。
func keyPaths(value any) []string {
	var out []string
	var walk func(v any, prefix string, depth int)
	walk = func(v any, prefix string, depth int) {
		if depth > 6 || len(out) >= 40 {
			return
		}
		switch node := v.(type) {
		case map[string]any:
			for key, child := range node {
				path := key
				if prefix != "" {
					path = prefix + "." + key
				}
				out = append(out, path)
				walk(child, path, depth+1)
			}
		case []any:
			for i, child := range node {
				if i >= 3 {
					break
				}
				walk(child, fmt.Sprintf("%s[%d]", prefix, i), depth+1)
			}
		}
	}
	walk(value, "", 0)
	return out
}
