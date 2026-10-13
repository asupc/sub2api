//go:build unit

package service

// workbuddy_oauth_service_test.go 单元测试：登录会话 / token 刷新写回 /
// NeedsRefresh 边界 / 凭据校验归一化 / quota 快照与恢复。
// 上游一律经 RoundTripper 桩（newWorkbuddyUpstreamClient / newWorkbuddyDeviceLogin 接缝）。

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/workbuddy"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// 测试工具。
// ---------------------------------------------------------------------------

// wbRoundTripFunc RoundTripper 桩。
type wbRoundTripFunc func(*http.Request) (*http.Response, error)

func (f wbRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func wbJSONResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

// wbRepoRecorder 记录型账号仓库桩（未覆盖方法经 nil 嵌入 panic，测试失败显式暴露）。
type wbRepoRecorder struct {
	AccountRepository
	mu            sync.Mutex
	account       *Account
	platformList  []Account
	updateCreds   map[int64]map[string]any
	updateExtras  []map[int64]map[string]any
	rateLimited   []time.Time
	modelLimits   []struct {
		id    int64
		scope string
	}
	setErrors      []string
	clearErrors    []int64
	setSchedulable []bool
}

func (r *wbRepoRecorder) GetByID(_ context.Context, id int64) (*Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.account != nil && r.account.ID == id {
		clone := *r.account
		return &clone, nil
	}
	return nil, ErrAccountNotFound
}

func (r *wbRepoRecorder) ListByPlatform(_ context.Context, _ string) ([]Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Account, len(r.platformList))
	copy(out, r.platformList)
	return out, nil
}

func (r *wbRepoRecorder) UpdateCredentials(_ context.Context, id int64, credentials map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.updateCreds == nil {
		r.updateCreds = map[int64]map[string]any{}
	}
	r.updateCreds[id] = credentials
	return nil
}

func (r *wbRepoRecorder) UpdateExtra(_ context.Context, id int64, updates map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.updateExtras = append(r.updateExtras, map[int64]map[string]any{id: updates})
	return nil
}

func (r *wbRepoRecorder) SetRateLimited(_ context.Context, _ int64, resetAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rateLimited = append(r.rateLimited, resetAt)
	return nil
}

func (r *wbRepoRecorder) SetModelRateLimit(_ context.Context, id int64, scope string, _ time.Time, _ ...string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.modelLimits = append(r.modelLimits, struct {
		id    int64
		scope string
	}{id, scope})
	return nil
}

func (r *wbRepoRecorder) SetError(_ context.Context, _ int64, errorMsg string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.setErrors = append(r.setErrors, errorMsg)
	return nil
}

func (r *wbRepoRecorder) ClearError(_ context.Context, id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clearErrors = append(r.clearErrors, id)
	return nil
}

func (r *wbRepoRecorder) SetSchedulable(_ context.Context, _ int64, schedulable bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.setSchedulable = append(r.setSchedulable, schedulable)
	return nil
}

// wbStubClientFactory 替换包级客户端工厂为 RoundTripper 桩，返回还原函数。
func wbStubClientFactory(t *testing.T, fn wbRoundTripFunc) {
	t.Helper()
	prev := newWorkbuddyUpstreamClient
	newWorkbuddyUpstreamClient = func(_ string, _ time.Duration) *workbuddy.Client {
		return workbuddy.NewClient(&http.Client{Transport: fn})
	}
	t.Cleanup(func() { newWorkbuddyUpstreamClient = prev })
}

func wbWorkbuddyAccount() *Account {
	return &Account{
		ID:      42,
		Name:    "wb-1",
		Platform: PlatformWorkbuddy,
		Type:    AccountTypeOAuth,
		Status:  StatusActive,
		Credentials: map[string]any{
			"access_token":  "old-access",
			"refresh_token": "old-refresh",
			"expires_at":    time.Now().Add(2 * time.Hour).Unix(),
			"realm":         "cn",
			"uid":           "uid-abc123",
			"api_protocol":  "adaptive",
			"model_mapping": map[string]any{"public": "glm-5.3"},
		},
		Extra: map[string]any{},
	}
}

// ---------------------------------------------------------------------------
// TokenRefresher。
// ---------------------------------------------------------------------------

func TestWorkbuddyTokenRefresher_CanAndNeedsRefreshBoundaries(t *testing.T) {
	refresher := NewWorkbuddyTokenRefresher(nil)

	account := wbWorkbuddyAccount()
	require.True(t, refresher.CanRefresh(account))
	require.False(t, refresher.NeedsRefresh(account, 0), "距到期 2h、窗口下限 30min：不应刷新")

	// 边界：距到期 40min（> 30min 下限）不刷新；20min 刷新。
	creds40 := wbWorkbuddyAccount()
	creds40.Credentials["expires_at"] = time.Now().Add(40 * time.Minute).Unix()
	require.False(t, refresher.NeedsRefresh(creds40, 0))
	creds20 := wbWorkbuddyAccount()
	creds20.Credentials["expires_at"] = time.Now().Add(20 * time.Minute).Unix()
	require.True(t, refresher.NeedsRefresh(creds20, 0))

	// 显式大窗口：距到期 3h、窗口 4h → 刷新。
	creds3h := wbWorkbuddyAccount()
	creds3h.Credentials["expires_at"] = time.Now().Add(3 * time.Hour).Unix()
	require.True(t, refresher.NeedsRefresh(creds3h, 4*time.Hour))

	// expires_at 缺失 / 非 workbuddy / 无 refresh_token → 不满足条件。
	noExpiry := wbWorkbuddyAccount()
	delete(noExpiry.Credentials, "expires_at")
	require.True(t, refresher.NeedsRefresh(noExpiry, 0))
	other := wbWorkbuddyAccount()
	other.Platform = PlatformGrok
	require.False(t, refresher.CanRefresh(other))
	require.False(t, refresher.NeedsRefresh(other, time.Hour))
	noRefresh := wbWorkbuddyAccount()
	noRefresh.Credentials["refresh_token"] = ""
	require.False(t, refresher.CanRefresh(noRefresh))
}

// ---------------------------------------------------------------------------
// 刷新写回。
// ---------------------------------------------------------------------------

func TestWorkbuddyRefreshAccountToken_MergesAndWritesBackCredentials(t *testing.T) {
	var gotPath string
	wbStubClientFactory(t, func(r *http.Request) (*http.Response, error) {
		gotPath = r.URL.Path
		require.Equal(t, "old-refresh", r.Header.Get("X-Refresh-Token"))
		require.Equal(t, "plugin", r.Header.Get("X-Auth-Refresh-Source"))
		return wbJSONResponse(http.StatusOK, `{"code":0,"msg":"","data":{"accessToken":"new-access","refreshToken":"new-refresh","expiresIn":5184000,"domain":"example.corp"}}`), nil
	})
	repo := &wbRepoRecorder{account: wbWorkbuddyAccount()}
	svc := NewWorkbuddyOAuthService(repo, nil)

	updated, err := svc.RefreshAccountToken(context.Background(), wbWorkbuddyAccount())
	require.NoError(t, err)
	require.Equal(t, "/v2/plugin/auth/token/refresh", gotPath)
	require.NotNil(t, repo.updateCreds[42])

	creds := repo.updateCreds[42]
	require.Equal(t, "new-access", creds["access_token"])
	require.Equal(t, "new-refresh", creds["refresh_token"])
	// 运营字段保留，token 字段更新。
	require.Equal(t, "adaptive", creds["api_protocol"])
	require.Equal(t, map[string]any{"public": "glm-5.3"}, creds["model_mapping"])
	require.Equal(t, "cn", creds["realm"])
	require.WithinDuration(t, time.Now().Add(60*24*time.Hour), time.Unix(parseInt64(t, creds["expires_at"]), 0), 2*time.Minute)
	require.Equal(t, "new-access", updated.Credentials["access_token"])
}

func parseInt64(t *testing.T, v any) int64 {
	t.Helper()
	switch n := v.(type) {
	case string:
		var out int64
		for _, ch := range n {
			out = out*10 + int64(ch-'0')
		}
		return out
	default:
		t.Fatalf("unexpected expires_at type %T", v)
		return 0
	}
}

func TestWorkbuddyRefreshAccountToken_RejectedRefreshTokenIsTerminal(t *testing.T) {
	wbStubClientFactory(t, func(r *http.Request) (*http.Response, error) {
		return wbJSONResponse(http.StatusUnauthorized, `{"code":12153,"msg":"Offline user session not found"}`), nil
	})
	repo := &wbRepoRecorder{account: wbWorkbuddyAccount()}
	svc := NewWorkbuddyOAuthService(repo, nil)

	_, err := svc.RefreshAccountToken(context.Background(), wbWorkbuddyAccount())
	require.Error(t, err)
	require.Contains(t, err.Error(), "WORKBUDDY_OAUTH_REFRESH_REJECTED")
	require.Empty(t, repo.updateCreds, "拒绝的刷新不得写回凭据")
}

// ---------------------------------------------------------------------------
// 登录会话。
// ---------------------------------------------------------------------------

func TestWorkbuddyLogin_StartAndPollLifecycle(t *testing.T) {
	var tokenPolls int
	pollTrip := wbRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(r.URL.Path, "/v2/plugin/auth/state"):
			return wbJSONResponse(http.StatusOK, `{"code":0,"msg":"","data":{"state":"st-1","authUrl":"https://auth.example/authorize?x=1"}}`), nil
		case strings.Contains(r.URL.Path, "/v2/plugin/auth/token"):
			tokenPolls++
			if tokenPolls == 1 {
				// pending：业务 code 非 0（"login ing"）。
				return wbJSONResponse(http.StatusOK, `{"code":1,"msg":"login ing"}`), nil
			}
			return wbJSONResponse(http.StatusOK, `{"code":0,"msg":"","data":{"accessToken":"at-1","refreshToken":"rt-1","expiresIn":3600,"domain":"example.corp"}}`), nil
		case strings.Contains(r.URL.Path, "/v2/plugin/login/account"):
			return wbJSONResponse(http.StatusOK, `{"code":0,"msg":"","data":{"uid":"uid-xyz","nickname":"猫猫","enterpriseId":""}}`), nil
		default:
			return wbJSONResponse(http.StatusNotFound, `{}`), nil
		}
	})
	prevDevice := newWorkbuddyDeviceLogin
	newWorkbuddyDeviceLogin = func(_ string) *workbuddy.DeviceLogin {
		return workbuddy.NewDeviceLogin(&http.Client{Transport: pollTrip})
	}
	t.Cleanup(func() { newWorkbuddyDeviceLogin = prevDevice })

	svc := NewWorkbuddyOAuthService(&wbRepoRecorder{}, nil)
	authURL, state, err := svc.StartLogin(context.Background(), "cn", nil)
	require.NoError(t, err)
	require.Equal(t, "https://auth.example/authorize?x=1", authURL)
	require.Equal(t, "st-1", state)

	// 未完成：(nil, nil)。
	cred, err := svc.PollLogin(context.Background(), state)
	require.NoError(t, err)
	require.Nil(t, cred)

	// 完成：拿到完整凭证。
	cred, err = svc.PollLogin(context.Background(), state)
	require.NoError(t, err)
	require.NotNil(t, cred)
	require.Equal(t, "at-1", cred.AccessToken)
	require.Equal(t, "uid-xyz", cred.UID)
	require.Equal(t, "cn", cred.RealmNormalized())

	// 授权完成后凭证缓存在会话中：state 建号路径二次 poll 幂等取回同一凭证，
	// 不重复请求 token 端点（修复「登录会话不存在或已过期」建号失败）。
	again, err := svc.PollLogin(context.Background(), state)
	require.NoError(t, err)
	require.Equal(t, cred, again)
	require.Equal(t, 2, tokenPolls, "缓存命中不得再次触网")
}

// ---------------------------------------------------------------------------
// 凭据校验与归一化。
// ---------------------------------------------------------------------------

func TestValidateAndNormalizeWorkbuddyCredentials(t *testing.T) {
	valid := func() map[string]any {
		return map[string]any{
			"access_token":  "at",
			"refresh_token": "rt",
			"uid":           "u1",
			"realm":         "cn",
		}
	}
	require.NoError(t, ValidateWorkbuddyCredentials(valid()))

	for key := range map[string]string{"access_token": "", "refresh_token": "", "uid": ""} {
		bad := valid()
		delete(bad, key)
		err := ValidateWorkbuddyCredentials(bad)
		require.Error(t, err)
	}

	badRealm := valid()
	badRealm["realm"] = "mars"
	require.Error(t, ValidateWorkbuddyCredentials(badRealm))

	// 归一化：realm 缺省按 domain 推断（global），api_protocol 强制 adaptive。
	creds := valid()
	delete(creds, "realm")
	creds["domain"] = "tenant.workbuddy.ai"
	require.NoError(t, NormalizeWorkbuddyAccountCredentials(PlatformWorkbuddy, creds))
	require.Equal(t, "global", creds["realm"])
	require.Equal(t, "adaptive", creds["api_protocol"])

	// 非 workbuddy 平台 no-op。
	other := map[string]any{"foo": "bar"}
	require.NoError(t, NormalizeWorkbuddyAccountCredentials(PlatformGrok, other))
	require.NotContains(t, other, "api_protocol")
}

// ---------------------------------------------------------------------------
// quota 快照与恢复。
// ---------------------------------------------------------------------------

func TestWorkbuddyQueryQuota_SnapshotAndNoCreditRecovery(t *testing.T) {
	wbStubClientFactory(t, func(r *http.Request) (*http.Response, error) {
		require.Contains(t, r.URL.Path, "/billing/meter/get-user-resource")
		return wbJSONResponse(http.StatusOK, `{"code":0,"msg":"","data":{"Response":{"Data":{"Accounts":[{"PackageName":"free","CapacityRemain":120,"CapacitySize":500,"CycleEndTime":"2099-01-01 00:00:00"}]}}}}`), nil
	})

	// 积分耗尽罚停的账号：余额恢复 → 清错误 + 恢复调度。
	account := wbWorkbuddyAccount()
	account.Status = StatusError
	account.ErrorMessage = WorkbuddyNoCreditReason
	account.Schedulable = false
	repo := &wbRepoRecorder{account: account}
	svc := NewWorkbuddyOAuthService(repo, nil)

	snapshot, err := svc.QueryWorkbuddyQuota(context.Background(), 42)
	require.NoError(t, err)
	require.Equal(t, int64(120), snapshot.Remain)
	require.Equal(t, int64(500), snapshot.Total)
	// CycleEndTime 2099 距 now 超过 48h 快过期窗口 → Expiring 计 0。
	require.Equal(t, int64(0), snapshot.Expiring)

	require.NotEmpty(t, repo.updateExtras, "wb_quota 快照应写入 extra")
	require.Equal(t, []int64{42}, repo.clearErrors)
	require.Equal(t, []bool{true}, repo.setSchedulable)
}

func TestWorkbuddyQueryQuota_NoRecoveryForUnrelatedErrors(t *testing.T) {
	wbStubClientFactory(t, func(r *http.Request) (*http.Response, error) {
		return wbJSONResponse(http.StatusOK, `{"code":0,"msg":"","data":{"Response":{"Data":{"Accounts":[{"CapacityRemain":10,"CapacitySize":500,"CycleEndTime":"2099-01-01 00:00:00"}]}}}}`), nil
	})
	account := wbWorkbuddyAccount()
	account.Status = StatusError
	account.ErrorMessage = "manually_disabled_by_operator"
	repo := &wbRepoRecorder{account: account}
	svc := NewWorkbuddyOAuthService(repo, nil)

	_, err := svc.QueryWorkbuddyQuota(context.Background(), 42)
	require.NoError(t, err)
	require.Empty(t, repo.clearErrors, "非 wb_no_credit 的错误不得被余额恢复逻辑误清")
}
