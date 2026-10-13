//go:build unit

package workbuddy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeUpstreamLogin 模拟设备授权三端点的假上游。
type fakeUpstreamLogin struct {
	pending     atomic.Bool // token 端点是否处于 pending（业务 code != 0）
	stateSeen   atomic.Int64
	tokenSeen   atomic.Int64
	accountSeen atomic.Int64
}

func (f *fakeUpstreamLogin) handler(t *testing.T) http.Handler {
	mux := http.NewServeMux()
	writeJSON := func(w http.ResponseWriter, body string) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}
	mux.HandleFunc("POST /v2/plugin/auth/state", func(w http.ResponseWriter, r *http.Request) {
		f.stateSeen.Add(1)
		if r.URL.Query().Get("platform") != "CLI" {
			t.Errorf("state 端点应带 platform=CLI，got %q", r.URL.RawQuery)
		}
		if r.Header.Get("User-Agent") != loginClientUA {
			t.Errorf("登录 UA=%q want %q", r.Header.Get("User-Agent"), loginClientUA)
		}
		writeJSON(w, `{"code":0,"msg":"ok","data":{"state":"st-123","authUrl":"https://auth.example/xyz"}}`)
	})
	mux.HandleFunc("GET /v2/plugin/auth/token", func(w http.ResponseWriter, r *http.Request) {
		f.tokenSeen.Add(1)
		if got := r.URL.Query().Get("state"); got != "st-123" {
			t.Errorf("token 端点 state=%q want st-123", got)
		}
		if f.pending.Load() {
			writeJSON(w, `{"code":1,"msg":"login ing"}`)
			return
		}
		writeJSON(w, `{"code":0,"msg":"ok","data":{"accessToken":"at-new","refreshToken":"rt-new","expiresIn":5184000,"domain":"copilot.tencent.com"}}`)
	})
	mux.HandleFunc("GET /v2/plugin/login/account", func(w http.ResponseWriter, r *http.Request) {
		f.accountSeen.Add(1)
		if got := r.Header.Get("Authorization"); got != "Bearer at-new" {
			t.Errorf("account 端点应带新 token，got %q", got)
		}
		writeJSON(w, `{"code":0,"msg":"ok","data":{"uid":"01234567-abcd","enterpriseId":"","nickname":"测试用户"}}`)
	})
	return mux
}

// TestDeviceLoginFlow 三段流程：Start → pending 轮询 → 完成取完整凭据。
func TestDeviceLoginFlow(t *testing.T) {
	f := &fakeUpstreamLogin{}
	f.pending.Store(true)
	srv := httptest.NewServer(f.handler(t))
	defer srv.Close()

	// 端点基址经 loginEndpointsFn 注入指向假上游（见 newTestDeviceLogin）。
	dl, restore := newTestDeviceLogin(srv.URL, time.Now)
	defer restore()

	url, state, err := dl.Start(context.Background(), RealmCN)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if url != "https://auth.example/xyz" || state != "st-123" {
		t.Fatalf("Start 返回 url=%q state=%q", url, state)
	}
	if f.stateSeen.Load() != 1 {
		t.Errorf("state 端点应命中 1 次，got %d", f.stateSeen.Load())
	}

	// pending：返回 (nil, nil)。
	cred, err := dl.Poll(context.Background(), state)
	if err != nil || cred != nil {
		t.Fatalf("pending 轮询应返回 (nil,nil)，got (%v,%v)", cred, err)
	}
	if f.tokenSeen.Load() != 1 {
		t.Errorf("token 端点应命中 1 次，got %d", f.tokenSeen.Load())
	}

	// 授权完成。
	f.pending.Store(false)
	cred, err = dl.Poll(context.Background(), state)
	if err != nil || cred == nil {
		t.Fatalf("完成轮询应返回凭据，got (%v,%v)", cred, err)
	}
	if cred.AccessToken != "at-new" || cred.RefreshToken != "rt-new" {
		t.Errorf("token 字段错误: %+v", cred)
	}
	if cred.UID != "01234567-abcd" || cred.Nickname != "测试用户" {
		t.Errorf("账号字段错误: %+v", cred)
	}
	if cred.Realm != RealmCN {
		t.Errorf("realm=%q want cn", cred.Realm)
	}
	if cred.ExpiresAt <= time.Now().Unix() {
		t.Errorf("ExpiresAt=%d 应在未来", cred.ExpiresAt)
	}
	// 会话已消费：再 Poll 报未知 state。
	if _, err := dl.Poll(context.Background(), state); err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Errorf("已消费 state 应报 unknown，got %v", err)
	}
}

// TestDeviceLoginGlobalRealm global realm 走国际域端点 + realm 写入凭证。
func TestDeviceLoginGlobalRealm(t *testing.T) {
	var gotOrigin string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotOrigin = r.Header.Get("Origin")
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v2/plugin/auth/state":
			_, _ = w.Write([]byte(`{"code":0,"data":{"state":"gs-1","authUrl":"https://auth.example/g"}}`))
		case "/v2/plugin/auth/token":
			_, _ = w.Write([]byte(`{"code":0,"data":{"accessToken":"at-g","expiresIn":100,"domain":"www.workbuddy.ai"}}`))
		case "/v2/plugin/login/account":
			_, _ = w.Write([]byte(`{"code":0,"data":{"uid":"global-uid-1","nickname":"intl"}}`))
		}
	}))
	defer srv.Close()

	dl, restore := newTestDeviceLogin(srv.URL, time.Now)
	defer restore()
	_, state, err := dl.Start(context.Background(), RealmGlobal)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if gotOrigin != originRefererGlobal {
		t.Errorf("global 登录 Origin=%q want %q", gotOrigin, originRefererGlobal)
	}
	cred, err := dl.Poll(context.Background(), state)
	if err != nil || cred == nil {
		t.Fatalf("Poll: %v %v", cred, err)
	}
	if cred.Realm != RealmGlobal || !cred.IsGlobal() {
		t.Errorf("global 登录应落 realm=global，got %q", cred.Realm)
	}
}

// TestDeviceLoginTTL state 会话 TTL 过期回收。
func TestDeviceLoginTTL(t *testing.T) {
	f := &fakeUpstreamLogin{}
	f.pending.Store(true)
	srv := httptest.NewServer(f.handler(t))
	defer srv.Close()

	base := time.Unix(1700000000, 0)
	dl, restore := newTestDeviceLogin(srv.URL, func() time.Time { return base })
	defer restore()

	_, state, err := dl.Start(context.Background(), RealmCN)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	// 时钟推进超过 TTL：state 应过期。
	dl.now = func() time.Time { return base.Add(loginTTL + time.Minute) }
	if _, err := dl.Poll(context.Background(), state); err == nil || !strings.Contains(err.Error(), "unknown or expired") {
		t.Errorf("过期 state 应报 expired，got %v", err)
	}
	// 会话表已被回收（Sessions 长度归零）。
	if n := len(dl.sessions); n != 0 {
		t.Errorf("过期会话应回收，残留 %d 条", n)
	}
}

// TestDeviceLoginInvalidUID 上游返回非法 uid（路径字符）时拒绝。
func TestDeviceLoginInvalidUID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v2/plugin/auth/state":
			_, _ = w.Write([]byte(`{"code":0,"data":{"state":"s2","authUrl":"https://a/b"}}`))
		case "/v2/plugin/auth/token":
			_, _ = w.Write([]byte(`{"code":0,"data":{"accessToken":"at","expiresIn":10}}`))
		case "/v2/plugin/login/account":
			_, _ = w.Write([]byte(`{"code":0,"data":{"uid":"../../evil"}}`))
		}
	}))
	defer srv.Close()
	dl, restore := newTestDeviceLogin(srv.URL, time.Now)
	defer restore()
	_, state, err := dl.Start(context.Background(), RealmCN)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := dl.Poll(context.Background(), state); err == nil || !strings.Contains(err.Error(), "非法字符") {
		t.Errorf("非法 uid 应拒绝，got %v", err)
	}
}

// newTestDeviceLogin 构造指向 base 的 DeviceLogin（注入时钟），并临时替换端点
// 函数引用 loginEndpointsFn（login.go 中声明的可替换引用，生产恒为 loginEndpoints）。
// 返回恢复函数（defer 调用还原）。
func newTestDeviceLogin(base string, now func() time.Time) (*DeviceLogin, func()) {
	orig := loginEndpointsFn
	loginEndpointsFn = func(realm string) (string, string, string, string) {
		org := originRefererCN
		if realm == RealmGlobal {
			org = originRefererGlobal
		}
		return base + "/v2/plugin/auth/state?platform=CLI",
			base + "/v2/plugin/auth/token?state=",
			base + "/v2/plugin/login/account?state=",
			org
	}
	dl := NewDeviceLogin(nil)
	dl.now = now
	return dl, func() { loginEndpointsFn = orig }
}
