package service

// qoder_oauth_service.go 二开新增：Qoder 平台的登录会话管理、PAT 导入与账号
// token 刷新。
//
// 上游协议实现见 internal/pkg/qoder（COSY 签名 / 设备授权 / jobToken 刷新 /
// 业务动作），本文件只做 sub2api 侧的会话编排与凭据持久化：
//   - StartLogin/PollLogin 包装 pkg 的设备授权（state 会话进程内、TTL 15min，
//     pkg 侧授权窗口 10min）；
//   - ImportPAT 交换 pt- 个人令牌为 jobToken 并拉取身份（token_kind=pat，
//     保留 personal_token 供刷新回落重交换）；
//   - RefreshAccountToken 按 refresh token 前缀路由刷新（drt- 设备族 / jrt-
//     jobToken 族 / PAT 重交换兜底），写回 credentials 并失效 COSY 会话缓存；
//   - QoderTokenRefresher 实现 TokenRefresher/OAuthRefreshExecutor，供
//     TokenRefreshService 后台保活（NeedsRefresh = expires_at 近 30min）。

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/qoder"
)

// qoderLoginSessionTTL 登录 state 会话的保留时长（pkg 上游授权窗口 10min，
// 本地留 15min 缓冲建号）。
const qoderLoginSessionTTL = 15 * time.Minute

// qoderRefreshEarlyWindow access token 到期前的提前刷新窗口（30min）。
const qoderRefreshEarlyWindow = 30 * time.Minute

// QoderOAuthService Qoder 设备授权登录、PAT 导入与账号 token 刷新。
type QoderOAuthService struct {
	accountRepo AccountRepository
	proxyRepo   ProxyRepository

	mu       sync.Mutex
	sessions map[string]*qoderLoginSession // state → 会话
}

// qoderLoginSession 一次设备授权的进程内会话（pkce verifier 在 pkg LoginSession 内）。
type qoderLoginSession struct {
	login   *qoder.LoginSession
	client  *qoder.Client
	created time.Time
}

// NewQoderOAuthService 构造服务；同时登记到请求路径刷新注册表（网关 401/403
// 路径触发强制刷新，注册表模式与 workbuddyRequestPathRefresher 同款）。
func NewQoderOAuthService(accountRepo AccountRepository, proxyRepo ProxyRepository) *QoderOAuthService {
	s := &QoderOAuthService{
		accountRepo: accountRepo,
		proxyRepo:   proxyRepo,
		sessions:    map[string]*qoderLoginSession{},
	}
	qoderRequestPathRefresher.Store(s)
	return s
}

// StartLogin 发起设备授权：返回用户浏览器打开的授权 URL 与 state。
// realm ∈ {cn, intl}（空按 cn）；proxyID 可选（登录与轮询走代理）。
func (s *QoderOAuthService) StartLogin(ctx context.Context, realm string, proxyID *int64) (authURL string, state string, err error) {
	realm = strings.TrimSpace(realm)
	if realm == "" {
		realm = "cn"
	}
	if !qoder.ValidRealm(realm) {
		return "", "", infraerrors.Newf(http.StatusBadRequest, "QODER_OAUTH_INVALID_REALM", "realm must be cn or intl, got %q", realm)
	}
	proxyURL, err := s.proxyURL(ctx, proxyID)
	if err != nil {
		return "", "", err
	}
	login := qoder.StartLogin(realm, "CLI", qoder.UUID4())
	client := qoder.NewClient(newQoderHTTPClient(proxyURL, 30*time.Second))
	s.mu.Lock()
	s.sweepLoginSessionsLocked(time.Now())
	s.sessions[login.State] = &qoderLoginSession{login: login, client: client, created: time.Now()}
	s.mu.Unlock()
	return login.AuthURL, login.State, nil
}

// PollLogin 轮询登录态：done=false 表示仍在等待浏览器授权；授权完成后返回
// 完整凭证并移除会话；state 未知/过期返回错误。上游轮询终态失败（用户拒绝 /
// 窗口超时）以错误形态返回，前端提示重开。
func (s *QoderOAuthService) PollLogin(ctx context.Context, state string) (*QoderLoginResult, error) {
	if strings.TrimSpace(state) == "" {
		return nil, infraerrors.New(http.StatusBadRequest, "QODER_OAUTH_STATE_REQUIRED", "state is required")
	}
	s.mu.Lock()
	sess, ok := s.sessions[state]
	if ok && time.Since(sess.created) > qoderLoginSessionTTL {
		delete(s.sessions, state)
		ok = false
	}
	s.mu.Unlock()
	if !ok {
		return nil, infraerrors.New(http.StatusBadRequest, "QODER_OAUTH_SESSION_NOT_FOUND", "登录会话不存在或已过期，请重新发起添加账号")
	}
	result, err := sess.client.PollLogin(ctx, sess.login)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "QODER_OAUTH_POLL_FAILED", "轮询 Qoder 设备授权失败: %v", err)
	}
	switch result.Status {
	case "pending":
		return &QoderLoginResult{Done: false, Message: result.Message}, nil
	case "ok":
		// 双保险：会话可能在并发轮询中被移除。
		s.mu.Lock()
		delete(s.sessions, state)
		s.mu.Unlock()
		if result.Token == nil || result.Token.AccessToken == "" {
			return nil, infraerrors.New(http.StatusBadGateway, "QODER_OAUTH_POLL_NO_TOKEN", "授权完成但未取到 token")
		}
		creds := qoderCredentialsFromTokenSet(result.Token, sess.login.Realm, result.UserInfo)
		out := &QoderLoginResult{
			Done:        true,
			Credentials: qoderCredentialsAnyMap(creds, ""),
			UserID:      creds.UID,
			Nickname:    creds.Nickname,
		}
		return out, nil
	default:
		return nil, infraerrors.Newf(http.StatusBadGateway, "QODER_OAUTH_POLL_FAILED", "设备授权失败: %s", result.Message)
	}
}

// QoderLoginResult login-poll 的响应形状。
type QoderLoginResult struct {
	Done        bool
	Credentials map[string]any
	UserID      string
	Nickname    string
	Message     string
}

// ImportPAT 导入 pt- 个人访问令牌：交换 jobToken 并拉取身份。返回可直接建号的
// 凭证（token_kind=pat，保留 personal_token 供刷新回落重交换）。
func (s *QoderOAuthService) ImportPAT(ctx context.Context, pat, realm string, proxyID *int64) (*QoderLoginResult, error) {
	realm = strings.TrimSpace(realm)
	if realm == "" {
		realm = "cn"
	}
	if !qoder.ValidRealm(realm) {
		return nil, infraerrors.Newf(http.StatusBadRequest, "QODER_OAUTH_INVALID_REALM", "realm must be cn or intl, got %q", realm)
	}
	proxyURL, err := s.proxyURL(ctx, proxyID)
	if err != nil {
		return nil, err
	}
	client := qoder.NewClient(newQoderHTTPClient(proxyURL, 30*time.Second))
	ts, ui, err := client.ImportPAT(ctx, realm, pat)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "QODER_OAUTH_PAT_IMPORT_FAILED", "导入 Qoder PAT 失败: %v", err)
	}
	// 交换产物是 jobToken；token_kind=pat 标记来源（禁用后台刷新，改走重交换），
	// 原始 PAT 存 personal_token 供 RefreshAccessToken 回落。
	ts.Kind = "pat"
	creds := qoderCredentialsFromTokenSet(ts, realm, ui)
	return &QoderLoginResult{
		Done:        true,
		Credentials: qoderCredentialsAnyMap(creds, strings.TrimSpace(pat)),
		UserID:      creds.UID,
		Nickname:    creds.Nickname,
	}, nil
}

// qoderCredentialsFromTokenSet 由 pkg 刷新/交换结果组装存储层凭证。
func qoderCredentialsFromTokenSet(ts *qoder.TokenSet, realm string, ui *qoder.UserInfo) qoder.Credentials {
	creds := qoder.Credentials{
		AccessToken:  ts.AccessToken,
		RefreshToken: ts.RefreshToken,
		TokenKind:    ts.Kind,
		ExpiresAt:    strconv.FormatInt(ts.ExpiresAt, 10),
		Realm:        realm,
	}
	if ui != nil {
		creds.UID = ui.ID
		creds.Nickname = ui.Name
		creds.UserType = ui.UserType
		creds.OrganizationID = ui.OrganizationID
		creds.OrganizationName = ui.OrganizationName
	}
	return creds
}

// qoderCredentialsAnyMap pkg 凭证 → JSONB 形态 credentials（强制 api_protocol=
// adaptive：任意入站协议收敛到 raw CC 漏斗的 qoder 分叉）；personalToken 非空
// 时一并落 personal_token。
func qoderCredentialsAnyMap(creds qoder.Credentials, personalToken string) map[string]any {
	out := map[string]any{}
	for k, v := range creds.ToMap() {
		out[k] = v
	}
	out["api_protocol"] = APIProtocolAdaptive
	if personalToken != "" {
		out["personal_token"] = personalToken
	}
	return out
}

// sweepLoginSessionsLocked 回收过期登录会话（调用方持锁）。
func (s *QoderOAuthService) sweepLoginSessionsLocked(now time.Time) {
	for state, sess := range s.sessions {
		if now.Sub(sess.created) > qoderLoginSessionTTL {
			delete(s.sessions, state)
		}
	}
}

// ---------------------------------------------------------------------------
// 账号 token 刷新。
// ---------------------------------------------------------------------------

// RefreshAccountToken 刷新账号 access token 并写回 credentials（合并保留运营
// 字段），同时失效该账号的 COSY 会话缓存（旧会话携带旧 token）。
func (s *QoderOAuthService) RefreshAccountToken(ctx context.Context, account *Account) (*Account, error) {
	if account == nil || account.Platform != PlatformQoder {
		return nil, infraerrors.New(http.StatusBadRequest, "QODER_OAUTH_INVALID_ACCOUNT", "account is not a qoder account")
	}
	creds, err := qoderAccountCredentials(account)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadRequest, "QODER_OAUTH_INVALID_CREDENTIALS", "账号凭证不完整: %v", err)
	}
	personalToken := strings.TrimSpace(account.GetCredential("personal_token"))
	if creds.RefreshToken == "" && personalToken == "" {
		return nil, infraerrors.New(http.StatusBadRequest, "QODER_OAUTH_NO_REFRESH_TOKEN", "no refresh token available")
	}
	proxyURL, err := s.proxyURL(ctx, account.ProxyID)
	if err != nil {
		return nil, err
	}
	client := qoder.NewClient(newQoderHTTPClient(proxyURL, 30*time.Second))
	ts, err := client.RefreshAccessToken(ctx, creds.NormalizedRealm(), creds.RefreshToken, personalToken)
	if err != nil {
		var dead *qoder.SessionDeadError
		if errors.As(err, &dead) {
			// 会话已死（上游吊销离线会话）：刷新无意义，需人工重新登录。
			if s.accountRepo != nil {
				_ = s.accountRepo.SetError(ctx, account.ID, "qd_session_dead: "+dead.Error())
			}
			return nil, infraerrors.Newf(http.StatusUnauthorized, "QODER_OAUTH_SESSION_DEAD", "Qoder 会话已失效，需重新登录: %s", dead.Detail)
		}
		return nil, infraerrors.Newf(http.StatusBadGateway, "QODER_OAUTH_REFRESH_FAILED", "刷新 Qoder token 失败: %v", err)
	}
	newCreds := creds
	newCreds.AccessToken = ts.AccessToken
	newCreds.RefreshToken = firstNonEmpty(ts.RefreshToken, creds.RefreshToken)
	newCreds.ExpiresAt = strconv.FormatInt(ts.ExpiresAt, 10)
	// token_kind 保留账号原值（pat 账号的重交换产物仍是 job token，来源标记不变）。
	merged := MergeCredentials(account.Credentials, qoderCredentialsAnyMap(newCreds, personalToken))
	if s.accountRepo == nil {
		account.Credentials = merged
		qoderCosySessions.Invalidate(creds.UID)
		return account, nil
	}
	if err := persistAccountCredentials(ctx, s.accountRepo, account, merged); err != nil {
		return nil, infraerrors.Newf(http.StatusInternalServerError, "QODER_OAUTH_PERSIST_FAILED", "凭据写回失败: %v", err)
	}
	qoderCosySessions.Invalidate(creds.UID)
	return account, nil
}

// QoderTokenRefresher 实现 TokenRefresher/OAuthRefreshExecutor：access token
// 到期前 30 分钟触发后台刷新。PAT 账号（token_kind=pat）不走后台刷新——
// 令牌交换按需发生，保活由 ops 的 keepalive 任务驱动。
type QoderTokenRefresher struct {
	oauthService *QoderOAuthService
}

// NewQoderTokenRefresher 构造刷新器。
func NewQoderTokenRefresher(oauthService *QoderOAuthService) *QoderTokenRefresher {
	return &QoderTokenRefresher{oauthService: oauthService}
}

// CacheKey 返回分布式刷新锁的缓存键。
func (r *QoderTokenRefresher) CacheKey(account *Account) string {
	return "qoder:account:" + strconv.FormatInt(account.ID, 10)
}

// CanRefresh 处理 qoder 平台 OAuth 账号（PAT 来源除外，须有 refresh_token）。
func (r *QoderTokenRefresher) CanRefresh(account *Account) bool {
	return account != nil && account.Platform == PlatformQoder && account.Type == AccountTypeOAuth &&
		strings.TrimSpace(account.GetCredential("token_kind")) != "pat" &&
		strings.TrimSpace(account.GetCredential("refresh_token")) != ""
}

// NeedsRefresh expires_at 近 30min 窗口、已过期或缺失时为真。
func (r *QoderTokenRefresher) NeedsRefresh(account *Account, refreshWindow time.Duration) bool {
	if account == nil || !r.CanRefresh(account) {
		return false
	}
	if refreshWindow < qoderRefreshEarlyWindow {
		refreshWindow = qoderRefreshEarlyWindow
	}
	expiresAt := account.GetCredentialAsTime("expires_at")
	if expiresAt == nil {
		return true
	}
	return time.Until(*expiresAt) < refreshWindow
}

// Refresh 刷新 token 并返回合并后的完整 credentials（保留运营字段）。
func (r *QoderTokenRefresher) Refresh(ctx context.Context, account *Account) (map[string]any, error) {
	if r == nil || r.oauthService == nil {
		return nil, errors.New("qoder oauth service is not configured")
	}
	updated, err := r.oauthService.RefreshAccountToken(ctx, account)
	if err != nil {
		return nil, fmt.Errorf("qoder token refresh: %w", err)
	}
	return updated.Credentials, nil
}

// ---------------------------------------------------------------------------
// 额度查询（qd_quota 快照）。
// ---------------------------------------------------------------------------

// Qoder 账号 extra / 错误原因常量。
const (
	// qoderQuotaExtraKey 额度快照在 extra 中的键。
	qoderQuotaExtraKey = "qd_quota"
	// qoderCreditsUsageExtraKey 官方积分消耗快照在 extra 中的键。
	qoderCreditsUsageExtraKey = "qd_credits_usage"
	// QoderNoCreditReason 积分耗尽罚号的 error reason（恢复逻辑按它识别）。
	QoderNoCreditReason = "qd_no_credit"
	// qoderCreditsUsageDailyDays 快照里保留的逐日消耗天数（源 daily 全量 365
	// 天，落库控制体积只留最近 30 天）。
	qoderCreditsUsageDailyDays = 30
)

// QoderCreditsUsageDay 逐日积分消耗。
type QoderCreditsUsageDay struct {
	Date  string  `json:"date"`
	Value float64 `json:"value"`
}

// QoderCreditsUsageSnapshot 官方积分消耗快照（extra["qd_credits_usage"] 的形状）。
// 字段口径对齐源 fetch_credit_usage 的 summary；Daily 只留最近 30 天控制体积。
type QoderCreditsUsageSnapshot struct {
	TotalCredits float64                 `json:"total_credits"`
	PeakCredits  float64                 `json:"peak_credits"`
	PeakDate     string                  `json:"peak_date,omitempty"`
	Unit         string                  `json:"unit,omitempty"`
	Daily        []QoderCreditsUsageDay  `json:"daily,omitempty"`
	UpdatedAt    string                  `json:"updated_at"`
}

// QoderQuotaSnapshot 额度快照（extra["qd_quota"] 的形状）。
type QoderQuotaSnapshot struct {
	Remain           int    `json:"remain"`
	Used             int    `json:"used"`
	Total            int    `json:"total"`
	Exceeded         bool   `json:"exceeded,omitempty"`
	Plan             string `json:"plan,omitempty"`
	UpdatedAt        string `json:"updated_at"`
	// issue #39/#67：共享组织配额（有效余额已计入 Remain）。
	SharedRemain     int    `json:"shared_remain,omitempty"`
	SharedUsed       int    `json:"shared_used,omitempty"`
	SharedSize       int    `json:"shared_size,omitempty"`
	SharedAvailable  bool   `json:"shared_available,omitempty"`
	SharedUnlimited  bool   `json:"shared_unlimited,omitempty"`
	SharedSource     string `json:"shared_source,omitempty"`
	EffectiveRemain  int    `json:"effective_remain,omitempty"`
	// CreditsUsage 官方积分消耗（issue: 官方积分消耗）：跟随额度快照一起透出，
	// admin quota 端点无需新造即可看到。拉取失败为 nil（不影响额度快照）。
	CreditsUsage     *QoderCreditsUsageSnapshot `json:"credits_usage,omitempty"`
}

// QueryAccountQuota 查询账号剩余积分：落 extra["qd_quota"] 快照并做 qd_no_credit
// 恢复判定（积分恢复且账号是本逻辑罚停的 → 清错误并恢复调度）。
func (s *QoderOAuthService) QueryAccountQuota(ctx context.Context, accountID int64) (*QoderQuotaSnapshot, error) {
	if s == nil || s.accountRepo == nil {
		return nil, infraerrors.New(http.StatusInternalServerError, "QODER_QUOTA_REPO_NOT_CONFIGURED", "account repository is not configured")
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if account.Platform != PlatformQoder {
		return nil, infraerrors.New(http.StatusBadRequest, "QODER_QUOTA_INVALID_ACCOUNT", "account platform does not match qoder quota endpoint")
	}
	snapshot, err := s.ProbeQoderQuota(ctx, account)
	if err != nil {
		return nil, err
	}
	if err := writeQoderQuotaExtra(ctx, s.accountRepo, account, snapshot); err != nil {
		return nil, err
	}
	s.recoverQoderNoCredit(ctx, account, snapshot)
	return snapshot, nil
}

// ProbeQoderQuota 拉取上游额度与套餐名（不落库；ops 与手动查询共用）。
func (s *QoderOAuthService) ProbeQoderQuota(ctx context.Context, account *Account) (*QoderQuotaSnapshot, error) {
	creds, err := qoderAccountCredentials(account)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadRequest, "QODER_QUOTA_INVALID_CREDENTIALS", "账号凭证不完整: %v", err)
	}
	proxyURL, err := s.proxyURL(ctx, account.ProxyID)
	if err != nil {
		return nil, err
	}
	client := qoder.NewClient(newQoderHTTPClient(proxyURL, 30*time.Second))
	quota, err := client.QuotaUsage(ctx, creds)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "QODER_QUOTA_UPSTREAM_FAILED", "查询 Qoder 额度失败: %v", err)
	}
	snapshot := &QoderQuotaSnapshot{
		Remain:           quota.EffectiveRemain,
		Used:             quota.Used,
		Total:            quota.Size,
		Exceeded:         quota.Exceeded,
		UpdatedAt:        time.Now().Format(time.RFC3339),
		SharedRemain:     quota.SharedRemain,
		SharedUsed:       quota.SharedUsed,
		SharedSize:       quota.SharedSize,
		SharedAvailable:  quota.SharedAvailable,
		SharedUnlimited:  quota.SharedUnlimited,
		SharedSource:     quota.SharedSource,
		EffectiveRemain:  quota.EffectiveRemain,
	}
	// 套餐名尽力而为（拉取失败不影响额度快照）。
	if plan, perr := client.UserPlan(ctx, creds); perr == nil && plan != "" {
		snapshot.Plan = plan
	}
	// 官方积分消耗尽力而为（issue: 官方积分消耗；拉取失败不影响额度快照）。
	// summary + heatmap 走 pkg 侧 5 分钟缓存，ops balance 6h 周期不会打爆上游。
	if usage, uerr := client.FetchCreditUsage(ctx, creds, qoderCreditsUsageDailyDays); uerr == nil {
		snapshot.CreditsUsage = &QoderCreditsUsageSnapshot{
			TotalCredits: usage.Total,
			PeakCredits:  usage.Peak,
			PeakDate:     usage.PeakDate,
			Unit:         usage.Unit,
			Daily:        toQoderCreditsUsageDays(usage.Daily),
			UpdatedAt:    time.Now().Format(time.RFC3339),
		}
	}
	return snapshot, nil
}

// toQoderCreditsUsageDays pkg 侧逐日消耗 → 快照形状。
func toQoderCreditsUsageDays(in []qoder.CreditDay) []QoderCreditsUsageDay {
	if len(in) == 0 {
		return nil
	}
	out := make([]QoderCreditsUsageDay, 0, len(in))
	for _, d := range in {
		out = append(out, QoderCreditsUsageDay{Date: d.Date, Value: d.Value})
	}
	return out
}

// writeQoderQuotaExtra 把额度快照写入 extra["qd_quota"] 并持久化（同步内存副本；
// 存值拷贝而非指针，JSONB 反序列化后断言形状一致）。积分消耗同时按
// extra["qd_credits_usage"] 独立落一份（任务约定键名，便于单独消费）。
func writeQoderQuotaExtra(ctx context.Context, repo AccountRepository, account *Account, snapshot *QoderQuotaSnapshot) error {
	if account == nil || snapshot == nil {
		return nil
	}
	value := *snapshot
	if account.Extra == nil {
		account.Extra = map[string]any{}
	}
	account.Extra[qoderQuotaExtraKey] = value
	extra := map[string]any{qoderQuotaExtraKey: value}
	if snapshot.CreditsUsage != nil {
		usage := *snapshot.CreditsUsage
		account.Extra[qoderCreditsUsageExtraKey] = usage
		extra[qoderCreditsUsageExtraKey] = usage
	}
	if repo == nil {
		return nil
	}
	return repo.UpdateExtra(ctx, account.ID, extra)
}

// recoverQoderNoCredit 积分恢复且账号是被 qd_no_credit 罚停的，清除错误状态并
// 恢复调度；其他来源的 error 不动（避免误恢复人工停用的账号）。
func (s *QoderOAuthService) recoverQoderNoCredit(ctx context.Context, account *Account, snapshot *QoderQuotaSnapshot) {
	if snapshot == nil || s.accountRepo == nil || account == nil {
		return
	}
	if snapshot.Remain <= 0 || account.Status != StatusError || account.ErrorMessage != QoderNoCreditReason {
		return
	}
	if err := s.accountRepo.ClearError(ctx, account.ID); err != nil {
		return
	}
	if err := s.accountRepo.SetSchedulable(ctx, account.ID, true); err != nil {
		return
	}
	account.Status = StatusActive
	account.ErrorMessage = ""
	account.Schedulable = true
}

// zeroQoderQuotaExtra 积分耗尽时把快照清零（402 路径调用，配合罚号）。
func zeroQoderQuotaExtra(ctx context.Context, repo AccountRepository, accountID int64) {
	if repo == nil {
		return
	}
	snapshot := QoderQuotaSnapshot{Remain: 0, Total: 0, UpdatedAt: time.Now().Format(time.RFC3339)}
	if err := repo.UpdateExtra(ctx, accountID, map[string]any{qoderQuotaExtraKey: snapshot}); err != nil {
		slog.Warn("qoder quota zero-out failed", "account_id", accountID, "error", err)
	}
}

// ---------------------------------------------------------------------------
// 凭据校验 / 归一化（创建与更新账号共用）。
// ---------------------------------------------------------------------------

// ValidateQoderCredentials 校验 qoder 凭证必填项：access_token / token_kind
// (device|job|pat) / realm (cn|intl) / uid；refresh_token 在非 pat 来源必填，
// pat 来源要求 personal_token（刷新回落用）。
func ValidateQoderCredentials(credentials map[string]any) error {
	if len(credentials) == 0 {
		return infraerrors.New(http.StatusBadRequest, "QODER_CREDENTIALS_REQUIRED", "qoder credentials are required")
	}
	read := func(key string) string {
		return strings.TrimSpace(stringValue(credentials[key]))
	}
	if read("access_token") == "" {
		return infraerrors.New(http.StatusBadRequest, "QODER_CREDENTIALS_MISSING_ACCESS_TOKEN", "access_token is required")
	}
	kind := read("token_kind")
	switch kind {
	case "device", "job":
		if read("refresh_token") == "" {
			return infraerrors.New(http.StatusBadRequest, "QODER_CREDENTIALS_MISSING_REFRESH_TOKEN", "refresh_token is required for device/job tokens")
		}
	case "pat":
		if read("personal_token") == "" {
			return infraerrors.New(http.StatusBadRequest, "QODER_CREDENTIALS_MISSING_PERSONAL_TOKEN", "personal_token is required for pat accounts")
		}
	default:
		return infraerrors.Newf(http.StatusBadRequest, "QODER_CREDENTIALS_INVALID_TOKEN_KIND", "token_kind must be device, job or pat, got %q", kind)
	}
	realm := read("realm")
	if realm == "" {
		realm = "cn"
	}
	if !qoder.ValidRealm(realm) {
		return infraerrors.Newf(http.StatusBadRequest, "QODER_CREDENTIALS_INVALID_REALM", "realm must be cn or intl, got %q", realm)
	}
	if read("uid") == "" {
		return infraerrors.New(http.StatusBadRequest, "QODER_CREDENTIALS_MISSING_UID", "uid is required")
	}
	return nil
}

// NormalizeQoderAccountCredentials 创建/更新路径的凭据归一化：非 qoder 平台
// no-op；qoder 校验必填项并强制 api_protocol=adaptive（协议收敛架构：任意入站
// 协议 → raw Chat Completions 漏斗的 qoder 分叉）、补齐 realm 缺省。
func NormalizeQoderAccountCredentials(platform string, credentials map[string]any) error {
	if platform != PlatformQoder || credentials == nil {
		return nil
	}
	if err := ValidateQoderCredentials(credentials); err != nil {
		return err
	}
	credentials["api_protocol"] = APIProtocolAdaptive
	if strings.TrimSpace(stringValue(credentials["realm"])) == "" {
		credentials["realm"] = "cn"
	}
	return nil
}

// ---------------------------------------------------------------------------
// 小工具。
// ---------------------------------------------------------------------------

// qoderAccountCredentials 从账号 credentials 解析 pkg 契约字段（缺 access_token
// 报错；其余字段缺省由 pkg 归一）。
func qoderAccountCredentials(account *Account) (qoder.Credentials, error) {
	if account == nil {
		return qoder.Credentials{}, errors.New("qoder account is nil")
	}
	if account.Platform != PlatformQoder {
		return qoder.Credentials{}, fmt.Errorf("account %d is not a qoder account", account.ID)
	}
	m := map[string]string{}
	for _, key := range []string{
		"access_token", "refresh_token", "token_kind", "expires_at", "realm",
		"uid", "nickname", "user_type", "organization_id", "organization_name",
		"machine_id", "machine_token",
	} {
		if v := account.GetCredential(key); v != "" {
			m[key] = v
		}
	}
	creds := qoder.ParseCredentials(m)
	if creds.AccessToken == "" {
		return creds, fmt.Errorf("qoder account %d missing access_token credential", account.ID)
	}
	return creds, nil
}

// proxyURL 解析代理 ID → URL（nil ID / nil repo 返回空串即直连）。
func (s *QoderOAuthService) proxyURL(ctx context.Context, proxyID *int64) (string, error) {
	if proxyID == nil {
		return "", nil
	}
	if s.proxyRepo == nil {
		return "", infraerrors.New(http.StatusBadRequest, "QODER_OAUTH_PROXY_NOT_AVAILABLE", "proxy repository is not available")
	}
	proxy, err := s.proxyRepo.GetByID(ctx, *proxyID)
	if err != nil {
		if errors.Is(err, ErrProxyNotFound) {
			return "", infraerrors.New(http.StatusBadRequest, "QODER_OAUTH_PROXY_NOT_FOUND", "configured proxy was not found")
		}
		return "", infraerrors.New(http.StatusServiceUnavailable, "QODER_OAUTH_PROXY_LOOKUP_FAILED", "proxy lookup is temporarily unavailable")
	}
	if proxy == nil {
		return "", infraerrors.New(http.StatusBadRequest, "QODER_OAUTH_PROXY_NOT_FOUND", "configured proxy was not found")
	}
	return proxy.URL(), nil
}

// newQoderHTTPClient 构造挂代理的 http.Client（oauth/ops/额度侧自用；网关转发
// 侧仍走既有 httpUpstream 传输）。timeout 为总超时（流式 chat 不用本 client）。
func newQoderHTTPClient(proxyURL string, timeout time.Duration) *http.Client {
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
