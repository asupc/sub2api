package service

// workbuddy_oauth_service.go 二开新增：WorkBuddy 平台的登录会话管理与账号 token 刷新。
//
// 上游协议实现见 internal/pkg/workbuddy（设备授权流 / token 刷新 / billing / growth），
// 本文件只做 sub2api 侧的会话编排与凭据持久化：
//   - StartLogin/PollLogin 包装 pkg 的 DeviceLogin（state 会话进程内、TTL 15min）；
//   - RefreshAccountToken 刷新并把新凭据写回账号（保留 api_protocol / model_mapping
//     等运营字段，只覆盖 token 相关键）；
//   - WorkbuddyTokenRefresher 实现 TokenRefresher/OAuthRefreshExecutor，供
//     TokenRefreshService 后台保活（NeedsRefresh = expires_at 近 30min）。

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/workbuddy"
	"golang.org/x/sync/singleflight"
)

// workbuddyRefreshEarlyWindow access token 到期前的提前刷新窗口（30min）。
// 上游实测 token 有效期 60d，提前刷新只为兜底，不是热路径。
const workbuddyRefreshEarlyWindow = 30 * time.Minute

// workbuddyLoginSessionTTL 登录 state 会话的保留时长（与 pkg loginTTL 对齐 15min）。
const workbuddyLoginSessionTTL = 15 * time.Minute

// WorkbuddyOAuthService WorkBuddy 设备流登录与账号 token 刷新。
type WorkbuddyOAuthService struct {
	accountRepo AccountRepository
	proxyRepo   ProxyRepository

	mu       sync.Mutex
	sessions map[string]*workbuddyLoginSession // state → 会话

	// refreshFlight 同账号 refresh 单飞（移植自 workbuddy2api-panel 3c4eb88）：
	// 上游对 refreshToken 反重放——同一 RT 被消费后再用返回 12153 并整条 RT 链作废，
	// 账号从此恒 401 只能人工重登。请求路径 401 强制刷新、后台保活、管理端手动刷新
	// 会并发触发同一账号的 refresh，并发越高掉登录概率越大。按 UID 单飞后并发调用
	// 等待并复用首个结果，不重复打上游；不同账号仍并行。
	refreshFlight singleflight.Group
}

// workbuddyLoginSession 一次设备授权的进程内会话（持有各自代理的 DeviceLogin）。
type workbuddyLoginSession struct {
	device  *workbuddy.DeviceLogin
	realm   string
	created time.Time
	done    *workbuddy.Credentials // 授权完成后缓存凭证：state 建号路径幂等取回，TTL 内有效
}

// NewWorkbuddyOAuthService 构造服务。proxyRepo 为 nil 时登录/刷新均不走代理。
// 同时把自己登记为请求路径强制刷新的提供方（见 workbuddyRequestPathRefresher）。
func NewWorkbuddyOAuthService(accountRepo AccountRepository, proxyRepo ProxyRepository) *WorkbuddyOAuthService {
	svc := &WorkbuddyOAuthService{
		accountRepo: accountRepo,
		proxyRepo:   proxyRepo,
		sessions:    map[string]*workbuddyLoginSession{},
	}
	workbuddyRequestPathRefresher.Store(svc)
	return svc
}

// StartLogin 发起设备授权：返回用户浏览器打开的授权 URL 与 state。
// realm ∈ {cn, global}（空按 cn）；proxyID 可选（登录走代理）。
func (s *WorkbuddyOAuthService) StartLogin(ctx context.Context, realm string, proxyID *int64) (authURL string, state string, err error) {
	realm = workbuddy.ResolveRealm(realm, "")
	proxyURL, err := s.proxyURL(ctx, proxyID)
	if err != nil {
		return "", "", err
	}
	device := newWorkbuddyDeviceLogin(proxyURL)
	authURL, state, err = device.Start(ctx, realm)
	if err != nil {
		return "", "", infraerrors.Newf(http.StatusBadGateway, "WORKBUDDY_OAUTH_LOGIN_START_FAILED", "发起 WorkBuddy 设备授权失败: %v", err)
	}
	s.mu.Lock()
	s.sweepLoginSessionsLocked(time.Now())
	s.sessions[state] = &workbuddyLoginSession{device: device, realm: realm, created: time.Now()}
	s.mu.Unlock()
	return authURL, state, nil
}

// PollLogin 轮询登录态：(nil, nil) 表示仍在等待授权（done=false）；
// 完成后返回完整凭证并缓存在会话中——create-from-oauth 以 state 二次调用时
// 幂等取回同一凭证（与 trae CompleteLogin 同语义），会话由 TTL/sweep 回收。
func (s *WorkbuddyOAuthService) PollLogin(ctx context.Context, state string) (*workbuddy.Credentials, error) {
	if strings.TrimSpace(state) == "" {
		return nil, infraerrors.New(http.StatusBadRequest, "WORKBUDDY_OAUTH_STATE_REQUIRED", "state is required")
	}
	s.mu.Lock()
	sess, ok := s.sessions[state]
	if ok && time.Since(sess.created) > workbuddyLoginSessionTTL {
		delete(s.sessions, state)
		ok = false
	}
	// 已完成的授权直接取缓存，不重复触网（state 建号路径）。
	if ok && sess.done != nil {
		cached := sess.done
		s.mu.Unlock()
		return cached, nil
	}
	s.mu.Unlock()
	if !ok {
		return nil, infraerrors.New(http.StatusBadRequest, "WORKBUDDY_OAUTH_SESSION_NOT_FOUND", "登录会话不存在或已过期，请重新发起添加账号")
	}
	cred, err := sess.device.Poll(ctx, state)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "WORKBUDDY_OAUTH_POLL_FAILED", "%v", err)
	}
	if cred == nil {
		return nil, nil // 授权尚未完成，前端继续轮询
	}
	s.mu.Lock()
	sess.done = cred
	s.mu.Unlock()
	return cred, nil
}

// sweepLoginSessionsLocked 回收过期登录会话（调用方持锁）。
func (s *WorkbuddyOAuthService) sweepLoginSessionsLocked(now time.Time) {
	for state, sess := range s.sessions {
		if now.Sub(sess.created) > workbuddyLoginSessionTTL {
			delete(s.sessions, state)
		}
	}
}

// BuildAccountCredentials 由登录凭证构造账号 credentials（强制 api_protocol=adaptive，
// 使任意入站协议收敛到 raw Chat Completions 漏斗的 workbuddy 分叉）。
func (s *WorkbuddyOAuthService) BuildAccountCredentials(cred *workbuddy.Credentials) map[string]any {
	if cred == nil {
		return nil
	}
	out := map[string]any{}
	for k, v := range cred.ToMap() {
		out[k] = v
	}
	out["api_protocol"] = APIProtocolAdaptive
	return out
}

// RefreshAccountToken 刷新账号 access token 并写回 credentials（合并保留运营字段）。
// 返回刷新后的账号（含新凭据）。
func (s *WorkbuddyOAuthService) RefreshAccountToken(ctx context.Context, account *Account) (*Account, error) {
	if account == nil || account.Platform != PlatformWorkbuddy {
		return nil, infraerrors.New(http.StatusBadRequest, "WORKBUDDY_OAUTH_INVALID_ACCOUNT", "account is not a WorkBuddy account")
	}
	updated, err := s.refreshAccountCredentials(ctx, account)
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// refreshAccountCredentials 刷新并持久化凭据；成功时同时清理 session 容忍计数
// （token 换新说明会话恢复，历史 strike 不再有意义）。
//
// 并发入口（请求路径 401 强制刷新 / 后台保活 / 管理端手动刷新）统一走同账号
// single-flight（见 refreshFlight 注释）：并发调用等待并复用首个结果。
func (s *WorkbuddyOAuthService) refreshAccountCredentials(ctx context.Context, account *Account) (*Account, error) {
	if account == nil {
		return nil, infraerrors.New(http.StatusBadRequest, "WORKBUDDY_OAUTH_INVALID_ACCOUNT", "account is nil")
	}
	v, err, _ := s.refreshFlight.Do(strconv.FormatInt(account.ID, 10), func() (any, error) {
		return s.doRefreshAccountCredentials(ctx, account)
	})
	if err != nil {
		return nil, err
	}
	updated, _ := v.(*Account)
	return updated, nil
}

// doRefreshAccountCredentials 单飞内的实际刷新（只应经 refreshAccountCredentials 调用）。
func (s *WorkbuddyOAuthService) doRefreshAccountCredentials(ctx context.Context, account *Account) (*Account, error) {
	cred, err := parseWorkbuddyCredentials(account)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadRequest, "WORKBUDDY_OAUTH_INVALID_CREDENTIALS", "账号凭证不完整: %v", err)
	}
	if strings.TrimSpace(cred.RefreshToken) == "" {
		return nil, infraerrors.New(http.StatusBadRequest, "WORKBUDDY_OAUTH_NO_REFRESH_TOKEN", "no refresh token available")
	}
	proxyURL, err := s.proxyURL(ctx, account.ProxyID)
	if err != nil {
		return nil, err
	}
	client := newWorkbuddyUpstreamClient(proxyURL, 30*time.Second)
	newCred, err := client.RefreshToken(ctx, cred)
	if err != nil {
		var ue *workbuddy.Error
		if errors.As(err, &ue) && (ue.Kind == workbuddy.KindSessionDead || ue.Status == http.StatusUnauthorized) {
			// 12153 兜底（移植自 workbuddy2api-panel 3c4eb88）：DB 里的 auth 可能已被
			// 其他进程/入口 rotate 过，本次拿旧 RT 撞了反重放。采纳 DB 最新 token 重试
			// 一次——若库里 RT 已换新，用新值能刷新成功，跨入口竞态也能自救。
			if recovered, ok := s.retryWithPersistedCredentials(ctx, account, cred, client); ok {
				return recovered, nil
			}
			// refresh_token 已失效：不可自愈，需人工重新登录。
			return nil, infraerrors.Newf(http.StatusUnauthorized, "WORKBUDDY_OAUTH_REFRESH_REJECTED", "refresh token 已失效，需重新登录: %s", ue.Msg)
		}

		return nil, infraerrors.Newf(http.StatusBadGateway, "WORKBUDDY_OAUTH_REFRESH_FAILED", "刷新 WorkBuddy token 失败: %v", err)
	}
	merged := MergeCredentials(account.Credentials, workbuddyCredentialsAnyMap(newCred))
	if account.Extra != nil {
		if _, has := account.Extra[workbuddySessionStrikesExtraKey]; has {
			_ = s.accountRepo.UpdateExtra(ctx, account.ID, map[string]any{workbuddySessionStrikesExtraKey: 0})
		}
	}
	if s.accountRepo == nil {
		account.Credentials = merged
		return account, nil
	}
	if err := persistAccountCredentials(ctx, s.accountRepo, account, merged); err != nil {
		return nil, infraerrors.Newf(http.StatusInternalServerError, "WORKBUDDY_OAUTH_PERSIST_FAILED", "凭据写回失败: %v", err)
	}
	return account, nil
}

// retryWithPersistedCredentials 12153 自救路径：从 DB 重读凭据，若 refresh_token 已被
// 他处换新则用新 RT 重试刷新一次；返回 (updated, true) 表示自救成功。
func (s *WorkbuddyOAuthService) retryWithPersistedCredentials(
	ctx context.Context,
	account *Account,
	used workbuddy.Credentials,
	client *workbuddy.Client,
) (*Account, bool) {
	if s.accountRepo == nil {
		return nil, false
	}
	fresh, err := s.accountRepo.GetByID(ctx, account.ID)
	if err != nil || fresh == nil || fresh.ID != account.ID {
		return nil, false
	}
	freshCred, err := parseWorkbuddyCredentials(fresh)
	if err != nil || strings.TrimSpace(freshCred.RefreshToken) == "" ||
		freshCred.RefreshToken == used.RefreshToken {
		return nil, false // 库内 RT 与本次用的相同 → 确实已作废，不重试
	}
	newCred, err := client.RefreshToken(ctx, freshCred)
	if err != nil {
		return nil, false
	}
	merged := MergeCredentials(fresh.Credentials, workbuddyCredentialsAnyMap(newCred))
	if fresh.Extra != nil {
		if _, has := fresh.Extra[workbuddySessionStrikesExtraKey]; has {
			_ = s.accountRepo.UpdateExtra(ctx, fresh.ID, map[string]any{workbuddySessionStrikesExtraKey: 0})
		}
	}
	if err := persistAccountCredentials(ctx, s.accountRepo, fresh, merged); err != nil {
		return nil, false
	}
	return fresh, true
}

// proxyURL 解析代理 ID → URL（nil ID / nil repo 返回空串即直连）。
func (s *WorkbuddyOAuthService) proxyURL(ctx context.Context, proxyID *int64) (string, error) {
	if proxyID == nil {
		return "", nil
	}
	if s.proxyRepo == nil {
		return "", infraerrors.New(http.StatusBadRequest, "WORKBUDDY_OAUTH_PROXY_NOT_AVAILABLE", "proxy repository is not available")
	}
	proxy, err := s.proxyRepo.GetByID(ctx, *proxyID)
	if err != nil {
		if errors.Is(err, ErrProxyNotFound) {
			return "", infraerrors.New(http.StatusBadRequest, "WORKBUDDY_OAUTH_PROXY_NOT_FOUND", "configured proxy was not found")
		}
		return "", infraerrors.New(http.StatusServiceUnavailable, "WORKBUDDY_OAUTH_PROXY_LOOKUP_FAILED", "proxy lookup is temporarily unavailable")
	}
	if proxy == nil {
		return "", infraerrors.New(http.StatusBadRequest, "WORKBUDDY_OAUTH_PROXY_NOT_FOUND", "configured proxy was not found")
	}
	return proxy.URL(), nil
}

// ---------------------------------------------------------------------------
// TokenRefresher 实现（TokenRefreshService 后台保活）。
// ---------------------------------------------------------------------------

// WorkbuddyTokenRefresher WorkBuddy 平台的 token 刷新器（CanRefresh=有 refresh_token；
// NeedsRefresh=expires_at 近窗口或已过期/缺失；Refresh=刷新并合并写回）。
type WorkbuddyTokenRefresher struct {
	oauthService *WorkbuddyOAuthService
}

// NewWorkbuddyTokenRefresher 构造刷新器。
func NewWorkbuddyTokenRefresher(oauthService *WorkbuddyOAuthService) *WorkbuddyTokenRefresher {
	return &WorkbuddyTokenRefresher{oauthService: oauthService}
}

// CacheKey 返回分布式刷新锁的缓存键。
func (r *WorkbuddyTokenRefresher) CacheKey(account *Account) string {
	return "workbuddy:account:" + strconv.FormatInt(account.ID, 10)
}

// CanRefresh 处理 workbuddy 平台的 OAuth 账号（须有 refresh_token）。
func (r *WorkbuddyTokenRefresher) CanRefresh(account *Account) bool {
	return account != nil && account.Platform == PlatformWorkbuddy && account.Type == AccountTypeOAuth &&
		strings.TrimSpace(account.GetCredential("refresh_token")) != ""
}

// NeedsRefresh expires_at 近 refreshWindow（下限 30min）内、已过期或缺失时为真。
func (r *WorkbuddyTokenRefresher) NeedsRefresh(account *Account, refreshWindow time.Duration) bool {
	if account == nil || !r.CanRefresh(account) {
		return false
	}
	if refreshWindow < workbuddyRefreshEarlyWindow {
		refreshWindow = workbuddyRefreshEarlyWindow
	}
	cred, err := parseWorkbuddyCredentials(account)
	if err != nil {
		return false
	}
	return cred.NeedsRefresh(refreshWindow, time.Now())
}

// Refresh 刷新 token 并返回合并后的完整 credentials（保留运营字段）。
func (r *WorkbuddyTokenRefresher) Refresh(ctx context.Context, account *Account) (map[string]any, error) {
	if r == nil || r.oauthService == nil {
		return nil, errors.New("workbuddy oauth service is not configured")
	}
	cred, err := parseWorkbuddyCredentials(account)
	if err != nil {
		return nil, fmt.Errorf("parse workbuddy credentials: %w", err)
	}
	proxyURL, err := r.oauthService.proxyURL(ctx, account.ProxyID)
	if err != nil {
		return nil, err
	}
	client := newWorkbuddyUpstreamClient(proxyURL, 30*time.Second)
	newCred, err := client.RefreshToken(ctx, cred)
	if err != nil {
		return nil, fmt.Errorf("workbuddy token refresh: %w", err)
	}
	merged := MergeCredentials(account.Credentials, workbuddyCredentialsAnyMap(newCred))
	// 归一化强制 adaptive，防止历史数据缺该键后回落非 CC 路径。
	merged["api_protocol"] = APIProtocolAdaptive
	return merged, nil
}

// ---------------------------------------------------------------------------
// 共享小工具（oauth / usage / ops / 网关分叉共用）。
// ---------------------------------------------------------------------------

// ValidateWorkbuddyCredentials 校验 workbuddy 凭证必填项：access_token /
// refresh_token / uid 非空，realm ∈ {cn, global}（空按 domain 推断，仍空补 cn）。
// 可选字段（nickname / enterprise_id / domain / device_token / 版本段 / user_agent）
// 原样透传，不在此枚举。
func ValidateWorkbuddyCredentials(credentials map[string]any) error {
	if len(credentials) == 0 {
		return infraerrors.New(http.StatusBadRequest, "WORKBUDDY_CREDENTIALS_REQUIRED", "workbuddy credentials are required")
	}
	get := func(key string) string {
		if v, ok := credentials[key]; ok && v != nil {
			if s, ok := v.(string); ok {
				return strings.TrimSpace(s)
			}
		}
		return ""
	}
	if get("access_token") == "" {
		return infraerrors.New(http.StatusBadRequest, "WORKBUDDY_CREDENTIALS_MISSING_ACCESS_TOKEN", "access_token is required")
	}
	if get("refresh_token") == "" {
		return infraerrors.New(http.StatusBadRequest, "WORKBUDDY_CREDENTIALS_MISSING_REFRESH_TOKEN", "refresh_token is required")
	}
	if get("uid") == "" {
		return infraerrors.New(http.StatusBadRequest, "WORKBUDDY_CREDENTIALS_MISSING_UID", "uid is required")
	}
	realm := get("realm")
	if realm == "" {
		realm = workbuddy.ResolveRealm("", get("domain"))
	}
	if realm != workbuddy.RealmCN && realm != workbuddy.RealmGlobal {
		return infraerrors.Newf(http.StatusBadRequest, "WORKBUDDY_CREDENTIALS_INVALID_REALM", "realm must be cn or global, got %q", realm)
	}
	return nil
}

// NormalizeWorkbuddyAccountCredentials 创建/更新路径的凭据归一化：非 workbuddy
// 平台 no-op；workbuddy 校验必填项并强制 api_protocol=adaptive（协议收敛架构：
// 任意入站协议 → raw Chat Completions 漏斗的 workbuddy 分叉）、补齐 realm。
func NormalizeWorkbuddyAccountCredentials(platform string, credentials map[string]any) error {
	if platform != PlatformWorkbuddy || credentials == nil {
		return nil
	}
	if err := ValidateWorkbuddyCredentials(credentials); err != nil {
		return err
	}
	credentials["api_protocol"] = APIProtocolAdaptive
	if strings.TrimSpace(stringValue(credentials["realm"])) == "" {
		credentials["realm"] = workbuddy.ResolveRealm("", strings.TrimSpace(stringValue(credentials["domain"])))
	}
	return nil
}

// parseWorkbuddyCredentials 从账号 credentials 解析 pkg 侧凭证（缺 access_token 报错）。
func parseWorkbuddyCredentials(account *Account) (workbuddy.Credentials, error) {
	if account == nil {
		return workbuddy.Credentials{}, errors.New("account is nil")
	}
	return workbuddy.ParseCredentials(workbuddyCredentialsStringMap(account))
}

// workbuddyCredentialsStringMap 把 JSONB 形态的 credentials 归一为 map[string]string
// （GetCredential 兼容数字/字符串两种值形态）。
func workbuddyCredentialsStringMap(account *Account) map[string]string {
	out := make(map[string]string, len(account.Credentials))
	for k := range account.Credentials {
		if v := account.GetCredential(k); v != "" {
			out[k] = v
		}
	}
	return out
}

// workbuddyCredentialsAnyMap pkg 凭证 → JSONB 形态 credentials。
func workbuddyCredentialsAnyMap(cred workbuddy.Credentials) map[string]any {
	out := map[string]any{}
	for k, v := range cred.ToMap() {
		out[k] = v
	}
	return out
}

// newWorkbuddyHTTPClient 构造挂代理的 http.Client（oauth/ops/quota 侧自用；
// 网关转发侧仍走既有 httpUpstream 传输）。timeout 为总超时（流式 chat 不用本 client）。
func newWorkbuddyHTTPClient(proxyURL string, timeout time.Duration) *http.Client {
	transport := &http.Transport{
		MaxIdleConns:          16,
		MaxIdleConnsPerHost:   8,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 20 * time.Second,
		ForceAttemptHTTP2:     true,
	}
	if trimmed := strings.TrimSpace(proxyURL); trimmed != "" {
		parsed, err := url.Parse(trimmed)
		if err == nil && parsed.Scheme != "" && parsed.Host != "" {
			transport.Proxy = http.ProxyURL(parsed)
		}
	}
	return &http.Client{Transport: transport, Timeout: timeout}
}

// newWorkbuddyUpstreamClient 统一的 pkg 客户端工厂（oauth/usage/ops 共用）。
// 包级变量是测试接缝：测试注入 RoundTripper 桩（workbuddy.NewClient(&http.Client{
// Transport: ...})），避免直连真实上游域。
var newWorkbuddyUpstreamClient = func(proxyURL string, timeout time.Duration) *workbuddy.Client {
	return workbuddy.NewClient(newWorkbuddyHTTPClient(proxyURL, timeout))
}

// newWorkbuddyDeviceLogin 设备授权管理器工厂（测试接缝，同 newWorkbuddyUpstreamClient）。
var newWorkbuddyDeviceLogin = func(proxyURL string) *workbuddy.DeviceLogin {
	return workbuddy.NewDeviceLogin(newWorkbuddyHTTPClient(proxyURL, 30*time.Second))
}
