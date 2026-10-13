package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/trae"
)

// TraeOAuthService 管理 trae 平台的 OAuth 设备授权流：授权 URL 生成、回调
// code 交换（AuthCode 单次使用，交换结果在会话内缓存供建号复用）与 refresh_token
// 换新 JWT。会话表为进程内存（TTL 10 分钟），多实例部署时回调必须回到发起
// 登录的实例（与 grok 的 Redis 会话不同，trae 登录依赖本机 127.0.0.1:17388
// 探测语义，跨实例意义有限）。
//
// 已知风险（待真机验证）：服务器侧 OAuth 设备公钥可经 credentials["device_public_key"]
// 覆盖；无真实公钥时上游交换可能报 20403 Token device not match。
type TraeOAuthService struct {
	accountRepo AccountRepository
	proxyRepo   ProxyRepository

	mu       sync.Mutex
	sessions map[string]*TraeLoginSession
}

// traeRefreshAccessToken 是 pkg/trae 刷新调用的注入点（签名与
// trae.RefreshAccessToken 一致，测试可替换）。
var traeRefreshAccessToken = trae.RefreshAccessToken

// traeLoginSessionTTL 是登录会话（state → PKCE / 设备事实）的存活时间。
const traeLoginSessionTTL = 10 * time.Minute

// TraeLoginSession 一次登录的完整状态。
type TraeLoginSession struct {
	State        string
	Variant      trae.Variant
	CodeVerifier string
	Facts        trae.DeviceFacts
	// DevicePrivateKey 虚拟设备私钥（与 Facts 的公钥同源；运营显式提供公钥时为空）。
	DevicePrivateKey string
	ProxyURL         string
	CreatedAt        time.Time
	// Credentials 交换成功后缓存的凭据（AuthCode 单次使用，建号直接取缓存）。
	Credentials map[string]any
	UserID      string
	DisplayName string
}

// TraeLoginStartResult 是 login-start 端点的响应。
type TraeLoginStartResult struct {
	URL   string `json:"url"`
	State string `json:"state"`
}

// TraeLoginResult 是回调交换完成的产物。
type TraeLoginResult struct {
	Credentials map[string]any
	UserID      string
	DisplayName string
	Variant     trae.Variant
}

// NewTraeOAuthService 构造 trae OAuth 服务。
func NewTraeOAuthService(accountRepo AccountRepository, proxyRepo ProxyRepository) *TraeOAuthService {
	return &TraeOAuthService{
		accountRepo: accountRepo,
		proxyRepo:   proxyRepo,
		sessions:    make(map[string]*TraeLoginSession),
	}
}

// GenerateLoginURL 生成授权页 URL 并登记会话。默认为本次登录生成一台虚拟设备
// 身份（EC P-256 密钥对 + 数字 device_id，公钥随交换上报、私钥存 credentials
// 供 refresh 的 DeviceProof 签名——两者同源）。devicePublicKeyPEM 为可选的
// 运营侧公钥覆盖（真实设备绑定场景），**必须与 deviceID 成对提供**（同一台
// 已知设备的 id+公钥对；新造 device_id 配旧公钥会被上游按 20403 设备不匹配
// 拒绝）；此时不保存自动生成的私钥（签名/上报不同源只会 20403），refresh 走
// pkg 四步链末位的旧协议兜底。proxyID 让交换请求经代理发出。
func (s *TraeOAuthService) GenerateLoginURL(ctx context.Context, variant string, proxyID *int64, deviceID, devicePublicKeyPEM string) (*TraeLoginStartResult, error) {
	v, ok := trae.ParseVariant(variant)
	if !ok {
		return nil, infraerrors.Newf(http.StatusBadRequest, "TRAE_OAUTH_INVALID_VARIANT",
			"unknown trae variant %q (expect trae_cn|solo_cn|trae_global|solo_global)", variant)
	}
	explicitPublicKey := strings.TrimSpace(devicePublicKeyPEM)
	explicitDeviceID := strings.TrimSpace(deviceID)
	if explicitPublicKey != "" && explicitDeviceID == "" {
		return nil, infraerrors.New(http.StatusBadRequest, "TRAE_OAUTH_DEVICE_PAIR_REQUIRED",
			"device_public_key 必须与 device_id 成对提供（同一台已知设备的标识对），单独提供公钥会与新造 device_id 不配对触发上游 20403")
	}
	proxyURL, err := s.proxyURL(ctx, proxyID)
	if err != nil {
		return nil, err
	}

	pair, err := trae.GenerateDeviceIdentity()
	if err != nil {
		return nil, infraerrors.Newf(http.StatusInternalServerError, "TRAE_OAUTH_DEVICE_IDENTITY_FAILED", "%v", err)
	}
	facts := trae.DeviceFacts{
		DeviceID:   pair.DeviceID,
		MachineID:  traeRandomUUID(),
		DeviceName: "sub2api",
	}
	// 设备私钥：默认保存（公钥 = 生成对的公钥，refresh 可签名）；显式
	// id+公钥对覆盖时不保存（签名私钥与上报公钥不同源无意义）。
	devicePrivateKey := pair.PrivateKeyPEM
	if explicitPublicKey != "" {
		facts.DeviceID = explicitDeviceID
		facts.DevicePublicKeyPEM = explicitPublicKey
		devicePrivateKey = ""
	} else {
		facts.DevicePublicKeyPEM = pair.PublicKeyPEM
	}
	verifier, challenge := trae.PKCEPair()
	traceID := trae.RandomHex(16)
	authURL := trae.BuildAuthorizeURL(v, facts, 0, traceID, challenge, trae.ClientOptions{})

	s.mu.Lock()
	s.gcSessionsLocked()
	s.sessions[traceID] = &TraeLoginSession{
		State:            traceID,
		Variant:          v,
		CodeVerifier:     verifier,
		Facts:            facts,
		DevicePrivateKey: devicePrivateKey,
		ProxyURL:         proxyURL,
		CreatedAt:        time.Now(),
	}
	s.mu.Unlock()

	return &TraeLoginStartResult{URL: authURL, State: traceID}, nil
}

// CompleteLogin 用授权页回跳参数完成 code 交换，返回可直接建号的凭据。
// 同一会话重复调用直接返回缓存结果（AuthCode 单次使用）。
// callbackQuery 为回跳 URL 的原始查询串（authCodeInfo/host/loginTraceID 等）。
func (s *TraeOAuthService) CompleteLogin(ctx context.Context, state, callbackQuery string) (*TraeLoginResult, error) {
	state = strings.TrimSpace(state)
	if state == "" {
		return nil, infraerrors.New(http.StatusBadRequest, "TRAE_OAUTH_STATE_REQUIRED", "state is required")
	}
	s.mu.Lock()
	session := s.sessions[state]
	if session != nil && time.Since(session.CreatedAt) > traeLoginSessionTTL {
		delete(s.sessions, state)
		session = nil
	}
	s.mu.Unlock()
	if session == nil {
		return nil, infraerrors.New(http.StatusBadRequest, "TRAE_OAUTH_SESSION_NOT_FOUND", "login session not found or expired")
	}
	if session.Credentials != nil {
		return &TraeLoginResult{
			Credentials: session.Credentials,
			UserID:      session.UserID,
			DisplayName: session.DisplayName,
			Variant:     session.Variant,
		}, nil
	}

	if err := trae.VerifyLoginTrace(callbackQuery, state); err != nil {
		return nil, infraerrors.New(http.StatusBadRequest, "TRAE_OAUTH_INVALID_CALLBACK", err.Error())
	}
	info, err := trae.ParseCallback(callbackQuery)
	if err != nil {
		return nil, infraerrors.New(http.StatusBadRequest, "TRAE_OAUTH_INVALID_CALLBACK", err.Error())
	}
	if strings.TrimSpace(info.AuthCode) == "" {
		return nil, infraerrors.New(http.StatusBadRequest, "TRAE_OAUTH_CODE_REQUIRED", "callback carries no auth code")
	}

	host, _ := trae.ResolveExchangeHost(info.Host, session.Variant)
	client, err := traeHTTPClient(session.ProxyURL)
	if err != nil {
		return nil, err
	}
	token, err := trae.ExchangeAuthCode(ctx, client, session.Variant, host,
		info.AuthCode, session.CodeVerifier, session.Facts, trae.ClientOptions{})
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "TRAE_OAUTH_EXCHANGE_FAILED", "%v", err)
	}

	credentials := buildTraeCredentialsFromToken(token.AccessToken, token.RefreshToken, session.Variant, info.UserID)
	// 设备身份成套落库：device_id/machine_id 用会话值（EnsureIdentity 的派生值
	// 仅作兜底），私钥与交换上报的公钥同源；运营显式提供公钥时只存公钥
	// （refresh 走旧协议兜底）。
	credentials["device_id"] = session.Facts.DeviceID
	credentials["machine_id"] = session.Facts.MachineID
	if strings.TrimSpace(session.DevicePrivateKey) != "" {
		credentials["device_private_key"] = strings.TrimSpace(session.DevicePrivateKey)
	} else if strings.TrimSpace(session.Facts.DevicePublicKeyPEM) != "" {
		credentials["device_public_key"] = strings.TrimSpace(session.Facts.DevicePublicKeyPEM)
	}

	result := &TraeLoginResult{
		Credentials: credentials,
		UserID:      firstNonEmpty(info.UserID, trae.UserIDOf(token.AccessToken)),
		DisplayName: strings.TrimSpace(info.DisplayName),
		Variant:     session.Variant,
	}
	s.mu.Lock()
	if existing := s.sessions[state]; existing == session {
		session.Credentials = credentials
		session.UserID = result.UserID
		session.DisplayName = result.DisplayName
		session.CreatedAt = time.Now() // 重新计时，留足建号窗口
	}
	s.mu.Unlock()
	return result, nil
}

// AbandonLogin 丢弃一次登录会话（授权失败 / 用户取消）。
func (s *TraeOAuthService) AbandonLogin(state string) {
	state = strings.TrimSpace(state)
	if state == "" {
		return
	}
	s.mu.Lock()
	delete(s.sessions, state)
	s.mu.Unlock()
}

// RefreshAccountToken 用存储的 refresh_token 换新 JWT，返回合并旧字段后的
// 新凭据（调用方负责持久化）。
func (s *TraeOAuthService) RefreshAccountToken(ctx context.Context, account *Account) (map[string]any, error) {
	if account == nil || account.Platform != PlatformTrae {
		return nil, infraerrors.New(http.StatusBadRequest, "TRAE_OAUTH_INVALID_ACCOUNT", "account is not a trae account")
	}
	creds, err := traeAccountCredentials(account)
	if err != nil {
		return nil, infraerrors.New(http.StatusBadRequest, "TRAE_OAUTH_INVALID_ACCOUNT", err.Error())
	}
	if strings.TrimSpace(creds.RefreshToken) == "" {
		return nil, infraerrors.New(http.StatusBadRequest, "TRAE_OAUTH_NO_REFRESH_TOKEN", "no refresh token available")
	}

	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	client, err := traeHTTPClient(proxyURL)
	if err != nil {
		return nil, err
	}
	// pkg/trae 四步刷新链：带设备私钥时 DeviceProof 签名（P1363→DER→旧端点）
	// 逐级回退，末位为旧协议兜底体；refresh_token 未轮换时结果里保留旧值。
	result, err := traeRefreshAccessToken(ctx, client, creds.VariantOf(), creds, trae.ClientOptions{})
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "TRAE_OAUTH_REFRESH_FAILED", "%v", err)
	}

	newCredentials := buildTraeCredentialsFromToken(result.AccessToken, result.RefreshToken, creds.VariantOf(), creds.UID)
	// 设备身份成套保留（buildTraeCredentialsFromToken 的 EnsureIdentity 派生值
	// 仅在原值为空时兜底）。
	newCredentials["device_id"] = creds.DeviceID
	newCredentials["machine_id"] = creds.MachineID
	if strings.TrimSpace(creds.DevicePrivateKey) != "" {
		newCredentials["device_private_key"] = creds.DevicePrivateKey
	}
	if publicKey := strings.TrimSpace(account.GetCredential("device_public_key")); publicKey != "" {
		newCredentials["device_public_key"] = publicKey
	}
	return newCredentials, nil
}

// QueryAccountQuota 查询账号剩余积分并把快照写入 extra["trae_quota"]。
func (s *TraeOAuthService) QueryAccountQuota(ctx context.Context, accountID int64) (*trae.Entitlements, error) {
	if s == nil || s.accountRepo == nil {
		return nil, errors.New("trae oauth service is not configured")
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if account.Platform != PlatformTrae {
		return nil, infraerrors.New(http.StatusBadRequest, "TRAE_OAUTH_INVALID_ACCOUNT", "account is not a trae account")
	}
	return storeTraeQuotaSnapshot(ctx, s.accountRepo, account)
}

// storeTraeQuotaSnapshot 查询积分并落 extra["trae_quota"]（ops 周期任务与手动
// 查询共用）。
func storeTraeQuotaSnapshot(ctx context.Context, repo AccountRepository, account *Account) (*trae.Entitlements, error) {
	creds, err := traeAccountCredentials(account)
	if err != nil {
		return nil, err
	}
	client, err := traeHTTPClient(traeAccountProxyURL(account))
	if err != nil {
		return nil, err
	}
	entitlements, err := trae.QueryEntitlements(ctx, client, creds.VariantOf(), creds.JWT, trae.DeviceIdentity{
		DeviceID:     creds.DeviceID,
		MarketUserID: creds.UID,
	})
	if err != nil {
		return nil, err
	}
	if err := writeTraeQuotaExtra(ctx, repo, account, entitlements, nil); err != nil {
		return nil, err
	}
	return entitlements, nil
}

// writeTraeQuotaExtra 把积分快照（及可选的附加键，如 expiring_soon）写入
// extra["trae_quota"] 并持久化，同时更新内存副本。
func writeTraeQuotaExtra(ctx context.Context, repo AccountRepository, account *Account, entitlements *trae.Entitlements, extraKeys map[string]any) error {
	blob, err := json.Marshal(entitlements)
	if err != nil {
		return err
	}
	var raw map[string]any
	if err := json.Unmarshal(blob, &raw); err != nil {
		return err
	}
	for key, value := range extraKeys {
		raw[key] = value
	}
	if account.Extra == nil {
		account.Extra = make(map[string]any)
	}
	account.Extra["trae_quota"] = raw
	if repo == nil {
		return nil
	}
	return repo.UpdateExtra(ctx, account.ID, map[string]any{"trae_quota": raw})
}

// traeAccountProxyURL 返回账号配置的代理 URL（未配置返回空串）。
func traeAccountProxyURL(account *Account) string {
	if account != nil && account.ProxyID != nil && account.Proxy != nil {
		return account.Proxy.URL()
	}
	return ""
}

// buildTraeCredentialsFromToken 由交换结果组装存储层凭据（值均为 string），
// expires_at 从 JWT 的 exp 回填，身份标识缺失时确定性派生。
func buildTraeCredentialsFromToken(jwt, refreshToken string, variant trae.Variant, fallbackUserID string) map[string]any {
	creds := &trae.Credentials{
		JWT:          trae.NormalizeJWT(jwt),
		RefreshToken: strings.TrimSpace(refreshToken),
		Variant:      variant.String(),
		UID:          firstNonEmpty(fallbackUserID, trae.UserIDOf(jwt)),
	}
	if info := trae.ParseJWT(creds.JWT); info.ExpValid {
		creds.ExpiresAt = strconv.FormatInt(info.ExpTimestamp, 10)
	}
	creds.EnsureIdentity()
	out := make(map[string]any, len(creds.ToMap())+1)
	for key, value := range creds.ToMap() {
		out[key] = value
	}
	// 协议收敛：任意入站协议都走 raw CC 漏斗的 trae 分叉。
	out["api_protocol"] = APIProtocolAdaptive
	return out
}

// NormalizeTraeAccountCredentials 校验并归一化 trae 账号凭据（创建 / 更新
// 通道共用）：jwt 与 variant 必填；api_protocol 强制 adaptive（显式设置其他
// 值时覆盖并记录）；expires_at 从 JWT 回填；uid / device_id / machine_id
// 缺失时确定性派生。非 trae 平台直接通过。
func NormalizeTraeAccountCredentials(platform string, credentials map[string]any) error {
	if platform != PlatformTrae || credentials == nil {
		return nil
	}
	readString := func(key string) string {
		switch v := credentials[key].(type) {
		case string:
			return strings.TrimSpace(v)
		case json.Number:
			return v.String()
		case float64:
			return strconv.FormatInt(int64(v), 10)
		case int64:
			return strconv.FormatInt(v, 10)
		case int:
			return strconv.Itoa(v)
		default:
			return ""
		}
	}

	jwt := trae.NormalizeJWT(readString("jwt"))
	if jwt == "" {
		return errors.New("trae 账号缺少 jwt 凭据")
	}
	variantRaw := readString("variant")
	variant, ok := trae.ParseVariant(variantRaw)
	if !ok {
		return fmt.Errorf("trae 凭据的 variant 无效：%q", variantRaw)
	}
	if existing, exists := credentials["api_protocol"]; exists {
		if text := strings.TrimSpace(fmt.Sprintf("%v", existing)); text != "" && text != APIProtocolAdaptive {
			log.Printf("[TraeOAuth] account api_protocol %q normalized to adaptive (platform converges to chat_completions only)", text)
		}
	}
	credentials["api_protocol"] = APIProtocolAdaptive

	creds := &trae.Credentials{
		JWT:          jwt,
		RefreshToken: readString("refresh_token"),
		ExpiresAt:    readString("expires_at"),
		Variant:      variant.String(),
		UID:          readString("uid"),
		DeviceID:     readString("device_id"),
		MachineID:    readString("machine_id"),
	}
	if creds.ExpiresAt == "" {
		if info := trae.ParseJWT(jwt); info.ExpValid {
			creds.ExpiresAt = strconv.FormatInt(info.ExpTimestamp, 10)
		}
	}
	creds.EnsureIdentity()
	for key, value := range creds.ToMap() {
		credentials[key] = value
	}
	return nil
}

// gcSessionsLocked 清理过期会话（调用方持锁）。
func (s *TraeOAuthService) gcSessionsLocked() {
	now := time.Now()
	for state, session := range s.sessions {
		if now.Sub(session.CreatedAt) > traeLoginSessionTTL {
			delete(s.sessions, state)
		}
	}
}

func (s *TraeOAuthService) proxyURL(ctx context.Context, proxyID *int64) (string, error) {
	if proxyID == nil {
		return "", nil
	}
	if s.proxyRepo == nil {
		return "", infraerrors.New(http.StatusBadRequest, "TRAE_OAUTH_PROXY_NOT_AVAILABLE", "proxy repository is not available")
	}
	proxy, err := s.proxyRepo.GetByID(ctx, *proxyID)
	if err != nil {
		if errors.Is(err, ErrProxyNotFound) {
			return "", infraerrors.New(http.StatusBadRequest, "TRAE_OAUTH_PROXY_NOT_FOUND", "configured proxy was not found")
		}
		return "", infraerrors.New(http.StatusServiceUnavailable, "TRAE_OAUTH_PROXY_LOOKUP_FAILED", "proxy lookup is temporarily unavailable")
	}
	if proxy == nil {
		return "", infraerrors.New(http.StatusBadRequest, "TRAE_OAUTH_PROXY_NOT_FOUND", "configured proxy was not found")
	}
	return proxy.URL(), nil
}

// traeHTTPClient 构造 trae OAuth / 积分接口使用的 HTTP 客户端（可选代理）。
func traeHTTPClient(proxyURL string) (*http.Client, error) {
	transport := &http.Transport{
		MaxIdleConns:       4,
		IdleConnTimeout:    30 * time.Second,
		DisableCompression: true,
	}
	if strings.TrimSpace(proxyURL) != "" {
		parsed, err := url.Parse(proxyURL)
		if err != nil {
			return nil, infraerrors.Newf(http.StatusBadRequest, "TRAE_OAUTH_PROXY_INVALID", "invalid proxy url: %v", err)
		}
		transport.Proxy = http.ProxyURL(parsed)
	}
	return &http.Client{Transport: transport, Timeout: 60 * time.Second}, nil
}

// traeRandomUUID 生成 UUID 形态的 machine_id。
func traeRandomUUID() string {
	hexed := trae.RandomHex(16)
	return fmt.Sprintf("%s-%s-%s-%s-%s", hexed[0:8], hexed[8:12], hexed[12:16], hexed[16:20], hexed[20:32])
}

// TraeTokenRefresher 实现 TokenRefresher / OAuthRefreshExecutor：JWT 到期前
// 30 分钟触发后台刷新。
type TraeTokenRefresher struct {
	oauthService *TraeOAuthService
}

// traeTokenRefreshLead 是 JWT 到期前的提前刷新窗口。
const traeTokenRefreshLead = 30 * time.Minute

// NewTraeTokenRefresher 构造 trae token 刷新器。
func NewTraeTokenRefresher(oauthService *TraeOAuthService) *TraeTokenRefresher {
	return &TraeTokenRefresher{oauthService: oauthService}
}

// CacheKey 返回分布式锁键。
func (r *TraeTokenRefresher) CacheKey(account *Account) string {
	return "trae_token:" + strconv.FormatInt(account.ID, 10)
}

// CanRefresh 检查是否能处理此账号（trae OAuth 且带 refresh_token）。
func (r *TraeTokenRefresher) CanRefresh(account *Account) bool {
	return account != nil && account.Platform == PlatformTrae && account.Type == AccountTypeOAuth &&
		strings.TrimSpace(account.GetCredential("refresh_token")) != ""
}

// NeedsRefresh JWT 到期前 30 分钟内需要刷新；expires_at 缺失视为需要。
func (r *TraeTokenRefresher) NeedsRefresh(account *Account, _ time.Duration) bool {
	expiresAt := account.GetCredentialAsTime("expires_at")
	if expiresAt == nil {
		return true
	}
	return time.Until(*expiresAt) < traeTokenRefreshLead
}

// Refresh 执行刷新，返回合并旧字段后的新凭据。
func (r *TraeTokenRefresher) Refresh(ctx context.Context, account *Account) (map[string]any, error) {
	if r == nil || r.oauthService == nil {
		return nil, errors.New("trae oauth service is not configured")
	}
	newCredentials, err := r.oauthService.RefreshAccountToken(ctx, account)
	if err != nil {
		return nil, err
	}
	return MergeCredentials(account.Credentials, newCredentials), nil
}
