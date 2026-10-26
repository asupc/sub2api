//go:build unit

package qoder

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// withRealm 把指定区域的 openapi 基址临时指向测试服务器，测试结束恢复。
func withRealm(t *testing.T, name, openapi string) {
	t.Helper()
	orig := RealmConfigs[name]
	cfgCopy := *orig
	cfgCopy.OpenAPI = openapi
	// 裁掉真实域名候选：doJSONOpenAPI 在主基址 404/连接失败时会换下一个基址，
	// 测试里若留下官网域名就会打出真实网络请求。只保留注入的测试基址。
	cfgCopy.Domain = ""
	cfgCopy.Website = ""
	RealmConfigs[name] = &cfgCopy
	// 换基址即换宿主：清掉上个用例记下的 404 缓存，避免测试间互相污染
	// （doJSONOpenAPI 的 (realm,path) 免探缓存是进程级的）。
	resetOpenAPIMissingCache()
	t.Cleanup(func() {
		RealmConfigs[name] = orig
		resetOpenAPIMissingCache()
	})
}

func TestMakePKCE(t *testing.T) {
	verifier, challenge := MakePKCE()
	if len(verifier) != 64 {
		t.Fatalf("verifier len = %d", len(verifier))
	}
	for _, c := range verifier {
		if !strings.ContainsRune(pkceAlphabet, c) {
			t.Fatalf("verifier char %q not in alphabet", c)
		}
	}
	sum := sha256.Sum256([]byte(verifier))
	want := base64.RawURLEncoding.EncodeToString(sum[:])
	if challenge != want {
		t.Fatalf("challenge mismatch: %s != %s", challenge, want)
	}
}

func TestStartLoginURLShape(t *testing.T) {
	cn := StartLogin("cn", "", "machine-abc")
	if !strings.HasPrefix(cn.AuthURL, "https://qoder.com.cn/device/selectAccounts?") {
		t.Fatalf("cn auth url: %s", cn.AuthURL)
	}
	q := cn.AuthURL[strings.Index(cn.AuthURL, "?")+1:]
	for _, want := range []string{
		"challenge=", "challenge_method=S256", "nonce=",
		"redirect_uri=qoder-work-cn%3A%2F%2F",
		"client_id=1c5e33e1-364d-4ce6-b02c-acaa81274a5c",
		"machine_id=machine-abc",
	} {
		if !strings.Contains(q, want) {
			t.Fatalf("cn url missing %q: %s", want, q)
		}
	}
	// CN nonce 为带横线 uuid
	if strings.Count(cn.Nonce, "-") != 4 || len(cn.Nonce) != 36 {
		t.Fatalf("cn nonce must be dashed uuid: %q", cn.Nonce)
	}
	if !strings.HasPrefix(cn.State, "qd-") {
		t.Fatalf("state: %s", cn.State)
	}

	intl := StartLogin("intl", "", "m1")
	if !strings.HasPrefix(intl.AuthURL, "https://qoder.com/device/selectAccounts?") {
		t.Fatalf("intl auth url: %s", intl.AuthURL)
	}
	qi := intl.AuthURL[strings.Index(intl.AuthURL, "?")+1:]
	if strings.Contains(qi, "redirect_uri") {
		t.Fatalf("intl url must not carry redirect_uri: %s", qi)
	}
	if !strings.Contains(qi, "client_id=e883ade2-e6e3-4d6d-adf7-f92ceff5fdcb") ||
		!strings.Contains(qi, "machine_id=m1") {
		t.Fatalf("intl url missing client_id/machine_id: %s", qi)
	}
	// intl nonce 为 32-hex
	if len(intl.Nonce) != 32 || strings.Contains(intl.Nonce, "-") {
		t.Fatalf("intl nonce must be 32-hex: %q", intl.Nonce)
	}
}

func TestPollLoginLifecycle(t *testing.T) {
	var pollCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case PathDevicePoll:
			if r.Method != http.MethodGet {
				t.Errorf("method %s", r.Method)
			}
			if r.Header.Get("User-Agent") != "QoderWork" {
				t.Errorf("poll UA = %q", r.Header.Get("User-Agent"))
			}
			if r.Header.Get("Authorization") != "" {
				t.Error("device flow poll must not carry credentials")
			}
			q := r.URL.Query()
			if q.Get("challenge_method") != "S256" || q.Get("nonce") == "" || q.Get("verifier") == "" {
				t.Errorf("poll query: %v", q)
			}
			pollCount++
			if pollCount <= 2 {
				w.WriteHeader(404) // 用户尚未完成授权
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"token":         "dt-newtoken",
				"refresh_token": "drt-newrefresh",
				"user_id":       "u-77",
				"expires_in":    2592000000, // ms -> 30 天
			})
		case PathUserinfo:
			// userinfo 补全（尽力而为）
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "u-77", "name": "登录用户", "user_type": "personal_pro"})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	// 把 openapi 指到测试服务器
	withRealm(t, "cn", srv.URL)

	sess := StartLogin("cn", "", "machine-abc")
	client := NewClient(nil)
	ctx := context.Background()

	res, err := client.PollLogin(ctx, sess)
	if err != nil || res.Status != "pending" {
		t.Fatalf("first poll: %v %+v", err, res)
	}
	res, err = client.PollLogin(ctx, sess)
	if err != nil || res.Status != "pending" {
		t.Fatalf("second poll: %v %+v", err, res)
	}
	res, err = client.PollLogin(ctx, sess)
	if err != nil {
		t.Fatalf("third poll: %v", err)
	}
	if res.Status != "ok" {
		t.Fatalf("status = %s", res.Status)
	}
	if res.Token.AccessToken != "dt-newtoken" || res.Token.RefreshToken != "drt-newrefresh" {
		t.Fatalf("token set: %+v", res.Token)
	}
	if res.Token.Kind != "device" {
		t.Fatalf("kind = %s", res.Token.Kind)
	}
	if res.UserInfo == nil || res.UserInfo.ID != "u-77" || res.UserInfo.Name != "登录用户" {
		t.Fatalf("userinfo: %+v", res.UserInfo)
	}
	wantExp := time.Now().Unix() + 2592000
	if res.Token.ExpiresAt < wantExp-60 || res.Token.ExpiresAt > wantExp+60 {
		t.Fatalf("expires at = %d, want ~%d", res.Token.ExpiresAt, wantExp)
	}
}

func TestRefreshAccessTokenPrefixRouting(t *testing.T) {
	var lastPath, lastBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, 4096)
		n, _ := r.Body.Read(body)
		lastPath = r.URL.Path
		lastBody = string(body[:n])
		if r.Header.Get("Content-Type") != "application/json" || r.Header.Get("User-Agent") != ClientUA {
			t.Errorf("refresh headers: %v", r.Header)
		}
		switch r.URL.Path {
		case PathDeviceRefresh:
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "dt-r1", "refresh_token": "drt-r1"})
		case PathJobRefresh:
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "jt-r1", "expires_in": 86400000})
		case PathJobExchange:
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "jt-e1"})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	withRealm(t, "intl", srv.URL)

	client := NewClient(nil)
	ctx := context.Background()

	// drt- -> deviceToken/refresh
	ts, err := client.RefreshAccessToken(ctx, "intl", "drt-old", "")
	if err != nil {
		t.Fatalf("drt refresh: %v", err)
	}
	if lastPath != PathDeviceRefresh || !strings.Contains(lastBody, `"refresh_token":"drt-old"`) {
		t.Fatalf("drt route: %s %s", lastPath, lastBody)
	}
	if ts.AccessToken != "dt-r1" || ts.RefreshToken != "drt-r1" || ts.Kind != "device" {
		t.Fatalf("drt result: %+v", ts)
	}

	// jrt- -> jobToken/refresh
	ts, err = client.RefreshAccessToken(ctx, "intl", "jrt-old", "")
	if err != nil {
		t.Fatalf("jrt refresh: %v", err)
	}
	if lastPath != PathJobRefresh || ts.AccessToken != "jt-r1" || ts.Kind != "job" {
		t.Fatalf("jrt route: %s %+v", lastPath, ts)
	}

	// jrt 失败 -> PAT 兜底交换
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == PathJobRefresh {
			w.WriteHeader(500)
			return
		}
		lastPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{"token": "jt-exchanged"})
	}))
	defer srv2.Close()
	withRealm(t, "intl", srv2.URL)
	ts, err = client.RefreshAccessToken(ctx, "intl", "jrt-broken", "pt-pat")
	if err != nil {
		t.Fatalf("pat fallback: %v", err)
	}
	if lastPath != PathJobExchange || ts.AccessToken != "jt-exchanged" {
		t.Fatalf("pat fallback route: %s %+v", lastPath, ts)
	}

	// 无任何凭证
	if _, err := client.RefreshAccessToken(ctx, "intl", "", ""); err != ErrNoRefreshToken {
		t.Fatalf("empty creds: %v", err)
	}
}

func TestRefreshSessionDead(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"errorCode":"TOKEN_EXPIRE","message":"Offline user session not found"}`))
	}))
	defer srv.Close()
	withRealm(t, "cn", srv.URL)

	client := NewClient(nil)
	_, err := client.RefreshAccessToken(context.Background(), "cn", "drt-dead", "")
	if err == nil {
		t.Fatal("expect session dead error")
	}
	if _, ok := err.(*SessionDeadError); !ok {
		t.Fatalf("error type %T: %v", err, err)
	}
	// 401 但非死亡标记 -> 普通刷新失败
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		_, _ = w.Write([]byte(`forbidden`))
	}))
	defer srv2.Close()
	withRealm(t, "cn", srv2.URL)
	_, err = client.RefreshAccessToken(context.Background(), "cn", "drt-dead", "")
	if err == nil || strings.Contains(err.Error(), "session dead") {
		t.Fatalf("non-dead 403 should be a plain failure: %v", err)
	}
}

func TestImportPAT(t *testing.T) {
	var exchangeBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, 1024)
		n, _ := r.Body.Read(body)
		switch r.URL.Path {
		case PathJobExchange:
			exchangeBody = string(body[:n])
			_ = json.NewEncoder(w).Encode(map[string]any{
				"token": "jt-from-pat", "refresh_token": "jrt-from-pat", "expires_in": 86400000})
		case PathUserinfo:
			if r.Header.Get("Authorization") != "Bearer jt-from-pat" {
				t.Errorf("userinfo auth: %q", r.Header.Get("Authorization"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "u-9", "name": "patuser", "user_type": "personal_standard"})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	withRealm(t, "intl", srv.URL)

	client := NewClient(nil)
	if _, _, err := client.ImportPAT(context.Background(), "intl", "not-a-pat"); err == nil {
		t.Fatal("non pt- prefix must be rejected")
	}
	ts, ui, err := client.ImportPAT(context.Background(), "intl", "pt-abc")
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if !strings.Contains(exchangeBody, `"personal_token":"pt-abc"`) {
		t.Fatalf("exchange body: %s", exchangeBody)
	}
	if ts.AccessToken != "jt-from-pat" || ts.Kind != "job" {
		t.Fatalf("token set: %+v", ts)
	}
	if ui == nil || ui.ID != "u-9" || ui.Name != "patuser" {
		t.Fatalf("userinfo: %+v", ui)
	}
}
