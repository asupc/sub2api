//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/qoder"
	"github.com/stretchr/testify/require"
)

// patchQoderRealm 把指定 realm 的 openapi 基址指到本地测试服务器（pkg 的
// RealmConfigs 是导出 map，测试就地替换并在结束后还原；期间禁止并行测试）。
func patchQoderRealm(t *testing.T, realm, openapi string) {
	t.Helper()
	old := qoder.RealmConfigs[realm]
	cfg := *old
	cfg.OpenAPI = openapi
	qoder.RealmConfigs[realm] = &cfg
	t.Cleanup(func() { qoder.RealmConfigs[realm] = old })
}

// qoderOAuthStubRepo 记录凭据与状态写操作的最小账号仓库。
type qoderOAuthStubRepo struct {
	AccountRepository
	mu          sync.Mutex
	accounts    map[int64]*Account
	credentials map[int64]map[string]any
	extras      map[int64]map[string]any
	errors      map[int64]string
	cleared     map[int64]bool
	schedulable map[int64]bool
}

func newQoderOAuthStubRepo(accounts ...*Account) *qoderOAuthStubRepo {
	repo := &qoderOAuthStubRepo{
		accounts:    map[int64]*Account{},
		credentials: map[int64]map[string]any{},
		extras:      map[int64]map[string]any{},
		errors:      map[int64]string{},
		cleared:     map[int64]bool{},
		schedulable: map[int64]bool{},
	}
	for _, account := range accounts {
		repo.accounts[account.ID] = account
	}
	return repo
}

func (r *qoderOAuthStubRepo) GetByID(_ context.Context, id int64) (*Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	// 返回存储指针（而非拷贝）：恢复逻辑改动的是取回的账号对象，测试需观测同
	// 一实例上的状态翻转。
	account, ok := r.accounts[id]
	if !ok {
		return nil, ErrAccountNotFound
	}
	return account, nil
}

func (r *qoderOAuthStubRepo) UpdateCredentials(_ context.Context, id int64, credentials map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.credentials[id] = credentials
	if account, ok := r.accounts[id]; ok {
		account.Credentials = credentials
	}
	return nil
}

func (r *qoderOAuthStubRepo) UpdateExtra(_ context.Context, id int64, updates map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	base := r.extras[id]
	if base == nil {
		base = map[string]any{}
		r.extras[id] = base
	}
	for k, v := range updates {
		base[k] = v
	}
	return nil
}

func (r *qoderOAuthStubRepo) SetError(_ context.Context, id int64, msg string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errors[id] = msg
	return nil
}

func (r *qoderOAuthStubRepo) ClearError(_ context.Context, id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cleared[id] = true
	delete(r.errors, id)
	return nil
}

func (r *qoderOAuthStubRepo) SetSchedulable(_ context.Context, id int64, schedulable bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.schedulable[id] = schedulable
	return nil
}

// qoderTokenEndpoints 构造记录路径的 openapi 测试服务器（刷新/交换/身份/额度）。
type qoderTokenEndpoints struct {
	server *httptest.Server
	mu     sync.Mutex
	paths  []string
	// handler 覆盖指定路径的响应（默认 200 + {"token":"..."}）。
	overrides map[string]http.HandlerFunc
}

func newQoderTokenEndpoints() *qoderTokenEndpoints {
	e := &qoderTokenEndpoints{overrides: map[string]http.HandlerFunc{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		e.mu.Lock()
		e.paths = append(e.paths, r.Method+" "+r.URL.Path)
		override, ok := e.overrides[r.URL.Path]
		e.mu.Unlock()
		if ok {
			override(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"dt-default-new","refresh_token":"drt-default-new","expires_in":3600000}`))
	})
	e.server = httptest.NewServer(mux)
	return e
}

func (e *qoderTokenEndpoints) handle(path string, handler http.HandlerFunc) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.overrides[path] = handler
}

func (e *qoderTokenEndpoints) called() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.paths...)
}

func TestValidateQoderCredentials(t *testing.T) {
	t.Parallel()
	valid := func(mutate func(map[string]any)) map[string]any {
		creds := map[string]any{
			"access_token":  "dt-x",
			"refresh_token": "drt-x",
			"token_kind":    "device",
			"realm":         "cn",
			"uid":           "u-1",
		}
		mutate(creds)
		return creds
	}
	require.NoError(t, ValidateQoderCredentials(valid(func(map[string]any) {})))
	require.Error(t, ValidateQoderCredentials(valid(func(c map[string]any) { delete(c, "access_token") })))
	require.Error(t, ValidateQoderCredentials(valid(func(c map[string]any) { delete(c, "uid") })))
	require.Error(t, ValidateQoderCredentials(valid(func(c map[string]any) { c["token_kind"] = "bogus" })))
	require.Error(t, ValidateQoderCredentials(valid(func(c map[string]any) { c["realm"] = "moon" })))
	// job 缺 refresh_token 报错；pat 要求 personal_token。
	require.Error(t, ValidateQoderCredentials(valid(func(c map[string]any) {
		c["token_kind"] = "job"
		delete(c, "refresh_token")
	})))
	require.NoError(t, ValidateQoderCredentials(valid(func(c map[string]any) {
		c["token_kind"] = "pat"
		delete(c, "refresh_token")
		c["personal_token"] = "pt-x"
	})))
	require.Error(t, ValidateQoderCredentials(valid(func(c map[string]any) {
		c["token_kind"] = "pat"
		delete(c, "refresh_token")
	})))
}

func TestNormalizeQoderAccountCredentials(t *testing.T) {
	t.Parallel()
	// 非 qoder 平台 no-op。
	creds := map[string]any{"api_protocol": "responses"}
	require.NoError(t, NormalizeQoderAccountCredentials(PlatformOpenAI, creds))
	require.Equal(t, "responses", creds["api_protocol"])

	qoderCreds := map[string]any{
		"access_token": "dt-x", "refresh_token": "drt-x",
		"token_kind": "device", "uid": "u-1",
	}
	require.NoError(t, NormalizeQoderAccountCredentials(PlatformQoder, qoderCreds))
	require.Equal(t, APIProtocolAdaptive, qoderCreds["api_protocol"])
	require.Equal(t, "cn", qoderCreds["realm"])
}

func TestQoderTokenRefresher_Eligibility(t *testing.T) {
	t.Parallel()
	refresher := NewQoderTokenRefresher(nil)

	device := qoderTestAccount()
	require.True(t, refresher.CanRefresh(device))
	require.False(t, refresher.NeedsRefresh(device, time.Hour)) // expires_at 远在未来

	expiring := qoderTestAccount()
	expiring.Credentials["expires_at"] = strconv.FormatInt(time.Now().Add(10*time.Minute).Unix(), 10)
	require.True(t, refresher.NeedsRefresh(expiring, time.Hour))

	missing := qoderTestAccount()
	delete(missing.Credentials, "expires_at")
	require.True(t, refresher.NeedsRefresh(missing, time.Hour))

	pat := qoderTestAccount()
	pat.Credentials["token_kind"] = "pat"
	require.False(t, refresher.CanRefresh(pat)) // PAT 来源不走后台刷新

	noRefresh := qoderTestAccount()
	delete(noRefresh.Credentials, "refresh_token")
	require.False(t, refresher.CanRefresh(noRefresh))
}

// TestQoderRefreshAccountToken_PrefixRouting 刷新按 refresh token 前缀路由：
// drt- → deviceToken/refresh；jrt- → jobToken/refresh；PAT 账号（无 refresh）
// → jobToken/exchange 重交换。凭据写回并失效 COSY 会话缓存。
func TestQoderRefreshAccount_PrefixRouting(t *testing.T) {
	endpoints := newQoderTokenEndpoints()
	defer endpoints.server.Close()
	patchQoderRealm(t, "cn", endpoints.server.URL)

	endpoints.handle("/api/v1/deviceToken/refresh", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"dt-new","refresh_token":"drt-new2","expires_in":3600000}`))
	})
	endpoints.handle("/api/v1/jobToken/refresh", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"jt-new","expires_in":7200000}`))
	})
	endpoints.handle("/api/v1/jobToken/exchange", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"jt-from-pat","expires_in":7200000}`))
	})

	// drt- 设备族。
	account := qoderTestAccount()
	repo := newQoderOAuthStubRepo(account)
	svc := NewQoderOAuthService(repo, nil)
	oldCreds, err := qoderAccountCredentials(account)
	require.NoError(t, err)
	oldSession, err := qoderCosySessions.Get(oldCreds.Identity())
	require.NoError(t, err)

	updated, err := svc.RefreshAccountToken(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "dt-new", updated.GetCredential("access_token"))
	require.Equal(t, "drt-new2", updated.GetCredential("refresh_token"))
	require.Equal(t, "device", updated.GetCredential("token_kind"))
	require.Equal(t, APIProtocolAdaptive, updated.Credentials["api_protocol"])
	require.Equal(t, []string{"POST /api/v1/deviceToken/refresh"}, endpoints.called())

	// COSY 会话缓存已失效（旧身份再次取会话时丢弃重建，指针不同）。
	newSession, err := qoderCosySessions.Get(oldCreds.Identity())
	require.NoError(t, err)
	require.NotSame(t, oldSession, newSession)

	// jrt- jobToken 族。
	endpoints.mu.Lock()
	endpoints.paths = nil
	endpoints.mu.Unlock()
	jobAccount := qoderTestAccount()
	jobAccount.Credentials["refresh_token"] = "jrt-old"
	jobAccount.Credentials["token_kind"] = "job"
	jobSvc := NewQoderOAuthService(newQoderOAuthStubRepo(jobAccount), nil)
	updated, err = jobSvc.RefreshAccountToken(context.Background(), jobAccount)
	require.NoError(t, err)
	require.Equal(t, "jt-new", updated.GetCredential("access_token"))
	require.Equal(t, []string{"POST /api/v1/jobToken/refresh"}, endpoints.called())

	// PAT 账号：无 refresh_token，经 personal_token 重交换。
	endpoints.mu.Lock()
	endpoints.paths = nil
	endpoints.mu.Unlock()
	patAccount := qoderTestAccount()
	patAccount.Credentials["token_kind"] = "pat"
	patAccount.Credentials["personal_token"] = "pt-keep"
	delete(patAccount.Credentials, "refresh_token")
	patSvc := NewQoderOAuthService(newQoderOAuthStubRepo(patAccount), nil)
	updated, err = patSvc.RefreshAccountToken(context.Background(), patAccount)
	require.NoError(t, err)
	require.Equal(t, "jt-from-pat", updated.GetCredential("access_token"))
	require.Equal(t, "pat", updated.GetCredential("token_kind")) // 来源标记保留
	require.Equal(t, "pt-keep", updated.GetCredential("personal_token"))
	require.Equal(t, []string{"POST /api/v1/jobToken/exchange"}, endpoints.called())
}

// TestQoderRefreshAccount_SessionDead 会话死亡（401 + TOKEN_EXPIRE）：
// 刷新无意义，账号置 error 且返回需重登错误。
func TestQoderRefreshAccount_SessionDead(t *testing.T) {
	endpoints := newQoderTokenEndpoints()
	defer endpoints.server.Close()
	patchQoderRealm(t, "cn", endpoints.server.URL)
	endpoints.handle("/api/v1/deviceToken/refresh", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"errorCode":"TOKEN_EXPIRE","message":"Offline user session not found"}`))
	})

	account := qoderTestAccount()
	repo := newQoderOAuthStubRepo(account)
	svc := NewQoderOAuthService(repo, nil)

	_, err := svc.RefreshAccountToken(context.Background(), account)
	require.Error(t, err)
	require.Contains(t, err.Error(), "QODER_OAUTH_SESSION_DEAD")
	require.Contains(t, repo.errors[account.ID], "qd_session_dead")
}

// TestQoderLoginPollFlow 登录会话：StartLogin 产出授权 URL；轮询 pending → done。
func TestQoderLoginPollFlow(t *testing.T) {
	endpoints := newQoderTokenEndpoints()
	defer endpoints.server.Close()
	patchQoderRealm(t, "cn", endpoints.server.URL)

	var pollCount int
	endpoints.handle("/api/v1/deviceToken/poll", func(w http.ResponseWriter, _ *http.Request) {
		pollCount++
		if pollCount < 2 {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"dt-xyz","refresh_token":"drt-abc","expires_in":86400000,"user_id":"u-9"}`))
	})
	endpoints.handle("/api/v1/userinfo", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"u-9","name":"Qoder User","user_type":"pro"}`))
	})

	svc := NewQoderOAuthService(newQoderOAuthStubRepo(), nil)
	authURL, state, err := svc.StartLogin(context.Background(), "cn", nil)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(authURL, "https://qoder.com.cn/device/selectAccounts?"), authURL)
	require.Contains(t, authURL, "challenge_method=S256")
	require.NotEmpty(t, state)

	// 未知 state 报错。
	_, err = svc.PollLogin(context.Background(), "qd-unknown")
	require.Error(t, err)

	// 第一次轮询：pending。
	result, err := svc.PollLogin(context.Background(), state)
	require.NoError(t, err)
	require.False(t, result.Done)

	// 第二次轮询：完成，凭证含身份与 api_protocol=adaptive。
	result, err = svc.PollLogin(context.Background(), state)
	require.NoError(t, err)
	require.True(t, result.Done)
	require.Equal(t, "dt-xyz", result.Credentials["access_token"])
	require.Equal(t, "u-9", result.Credentials["uid"])
	require.Equal(t, "Qoder User", result.Credentials["nickname"])
	require.Equal(t, "device", result.Credentials["token_kind"])
	require.Equal(t, APIProtocolAdaptive, result.Credentials["api_protocol"])

	// 完成后会话已消费：再次轮询报会话不存在。
	_, err = svc.PollLogin(context.Background(), state)
	require.Error(t, err)
}

// TestQoderImportPATFlow PAT 导入：pt- 交换 jobToken + 拉取身份，token_kind=pat
// 且保留 personal_token。
func TestQoderImportPATFlow(t *testing.T) {
	endpoints := newQoderTokenEndpoints()
	defer endpoints.server.Close()
	patchQoderRealm(t, "cn", endpoints.server.URL)
	endpoints.handle("/api/v1/jobToken/exchange", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		require.Contains(t, string(body), `"personal_token":"pt-my-pat"`)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"jt-exchanged","expires_in":7200000}`))
	})
	endpoints.handle("/api/v1/userinfo", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"u-77","name":"PAT User"}`))
	})

	svc := NewQoderOAuthService(newQoderOAuthStubRepo(), nil)
	result, err := svc.ImportPAT(context.Background(), "pt-my-pat", "cn", nil)
	require.NoError(t, err)
	require.True(t, result.Done)
	require.Equal(t, "jt-exchanged", result.Credentials["access_token"])
	require.Equal(t, "pat", result.Credentials["token_kind"])
	require.Equal(t, "pt-my-pat", result.Credentials["personal_token"])
	require.Equal(t, "u-77", result.Credentials["uid"])
	require.Equal(t, APIProtocolAdaptive, result.Credentials["api_protocol"])

	// 非 pt- 前缀直接拒绝。
	_, err = svc.ImportPAT(context.Background(), "jt-not-pat", "cn", nil)
	require.Error(t, err)
}

// TestQoderQueryAccountQuota_NoCreditRecovery 额度查询落 qd_quota 快照，且
// qd_no_credit 罚停账号在积分恢复后自动回 active。
func TestQoderQueryAccountQuota_NoCreditRecovery(t *testing.T) {
	endpoints := newQoderTokenEndpoints()
	defer endpoints.server.Close()
	patchQoderRealm(t, "cn", endpoints.server.URL)
	endpoints.handle("/api/v2/quota/usage", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"userQuota":{"remaining":8,"used":2,"total":10},"addOnQuota":{"remaining":2,"used":0,"total":2},"isQuotaExceeded":false}`))
	})
	endpoints.handle("/api/v2/user/plan", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"plan_tier_name":"Pro"}`))
	})

	account := qoderTestAccount()
	account.Status = StatusError
	account.ErrorMessage = QoderNoCreditReason
	account.Schedulable = false
	repo := newQoderOAuthStubRepo(account)
	svc := NewQoderOAuthService(repo, nil)

	snapshot, err := svc.QueryAccountQuota(context.Background(), account.ID)
	require.NoError(t, err)
	require.Equal(t, 10, snapshot.Remain)
	require.Equal(t, 12, snapshot.Total)
	require.Equal(t, "Pro", snapshot.Plan)
	require.NotEmpty(t, snapshot.UpdatedAt)

	persisted, ok := repo.extras[account.ID][qoderQuotaExtraKey].(QoderQuotaSnapshot)
	require.True(t, ok)
	require.Equal(t, 10, persisted.Remain)

	// 恢复判定：罚停标记清除、恢复调度。
	require.True(t, repo.cleared[account.ID])
	require.True(t, repo.schedulable[account.ID])
	require.Equal(t, StatusActive, account.Status)
	require.Empty(t, account.ErrorMessage)
}
