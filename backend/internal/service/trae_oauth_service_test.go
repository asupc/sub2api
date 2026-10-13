//go:build unit

package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/trae"
	"github.com/stretchr/testify/require"
)

// urlEncodeForTest 是测试用的查询串编码。
func urlEncodeForTest(s string) string { return url.QueryEscape(s) }

// traeTestJWT 构造一个不签名但 payload 合法的 Cloud-IDE-JWT（本地只解析不校验）。
func traeTestJWT(userID string, exp time.Time) string {
	encode := func(obj map[string]any) string {
		raw, _ := json.Marshal(obj)
		return base64.RawURLEncoding.EncodeToString(raw)
	}
	header := encode(map[string]any{"alg": "none", "typ": "JWT"})
	payload := encode(map[string]any{"data": map[string]any{"id": userID}, "exp": exp.Unix()})
	return header + "." + payload + "."
}

// traeExchangeStub 启动一个模仿 ExchangeToken 主路径的本地桩。
func traeExchangeStub(t *testing.T, accessToken, refreshToken string, hits *int) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits != nil {
			*hits++
		}
		require.Equal(t, "/trae/api/v3/oauth/ExchangeToken", r.URL.Path)
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		// 设备事实与 ClientID 必须随交换请求带上。
		if device, ok := payload["DeviceInfo"].(map[string]any); ok {
			require.NotEmpty(t, device["DeviceID"])
			require.NotEmpty(t, device["MachineID"])
		}
		require.NotEmpty(t, payload["ClientID"])
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Result": map[string]any{
				"AccessToken":  accessToken,
				"RefreshToken": refreshToken,
			},
		})
	}))
	t.Cleanup(server.Close)
	return server
}

func TestNormalizeTraeAccountCredentials(t *testing.T) {
	t.Parallel()

	t.Run("non-trae passthrough", func(t *testing.T) {
		creds := map[string]any{"api_key": "sk"}
		require.NoError(t, NormalizeTraeAccountCredentials(PlatformOpenAI, creds))
		require.NotContains(t, creds, "api_protocol")
	})

	t.Run("fills adaptive expiry identity", func(t *testing.T) {
		exp := time.Now().Add(2 * time.Hour)
		jwt := traeTestJWT("70001", exp)
		creds := map[string]any{
			"jwt":     "Cloud-IDE-JWT " + jwt, // 带前缀也应归一化
			"variant": "trae_work",            // 别名归一化为契约值
		}
		require.NoError(t, NormalizeTraeAccountCredentials(PlatformTrae, creds))
		require.Equal(t, APIProtocolAdaptive, creds["api_protocol"])
		require.Equal(t, jwt, creds["jwt"])
		require.Equal(t, "solo_cn", creds["variant"])
		require.Equal(t, "70001", creds["uid"])
		require.Equal(t, exp.Unix(), parseInt64ForTest(t, creds["expires_at"]))
		require.NotEmpty(t, creds["device_id"])
		require.NotEmpty(t, creds["machine_id"])
	})

	t.Run("overwrites explicit non adaptive protocol", func(t *testing.T) {
		creds := map[string]any{"jwt": traeTestJWT("1", time.Now().Add(time.Hour)), "variant": "trae_cn", "api_protocol": "responses"}
		require.NoError(t, NormalizeTraeAccountCredentials(PlatformTrae, creds))
		require.Equal(t, APIProtocolAdaptive, creds["api_protocol"])
	})

	t.Run("rejects missing jwt or variant", func(t *testing.T) {
		require.Error(t, NormalizeTraeAccountCredentials(PlatformTrae, map[string]any{"variant": "trae_cn"}))
		require.Error(t, NormalizeTraeAccountCredentials(PlatformTrae, map[string]any{"jwt": traeTestJWT("1", time.Now().Add(time.Hour))}))
		require.Error(t, NormalizeTraeAccountCredentials(PlatformTrae, map[string]any{"jwt": "x", "variant": "bogus"}))
	})
}

func parseInt64ForTest(t *testing.T, v any) int64 {
	t.Helper()
	text, ok := v.(string)
	require.True(t, ok, "credential value should be string, got %T", v)
	parsed, err := strconv.ParseInt(text, 10, 64)
	require.NoError(t, err)
	return parsed
}

func TestTraeOAuthService_GenerateLoginURL(t *testing.T) {
	t.Parallel()
	svc := NewTraeOAuthService(nil, nil)

	result, err := svc.GenerateLoginURL(context.Background(), "solo_cn", nil, "", "")
	require.NoError(t, err)
	require.NotEmpty(t, result.State)
	require.Contains(t, result.URL, "https://www.trae.cn/authorization")
	require.Contains(t, result.URL, "auth_from=solo")
	require.Contains(t, result.URL, "client_id=en1oxy7wnw8j9n")
	require.Contains(t, result.URL, "login_trace_id="+result.State)
	require.Contains(t, result.URL, "hide_saas_login=true")

	_, err = svc.GenerateLoginURL(context.Background(), "bogus", nil, "", "")
	require.Error(t, err)
}

func TestTraeOAuthService_CompleteLogin(t *testing.T) {
	t.Parallel()

	hits := 0
	server := traeExchangeStub(t, traeTestJWT("70001", time.Now().Add(2*time.Hour)), "fresh-refresh", &hits)
	svc := NewTraeOAuthService(nil, nil)

	start, err := svc.GenerateLoginURL(context.Background(), "trae_cn", nil, "", "")
	require.NoError(t, err)

	// 回调查询串：authCodeInfo JSON + 交换主机（桩地址）+ CSRF trace。
	authCodeInfo := `{"AuthCode":"auth-code-1"}`
	callback := "authCodeInfo=" + urlEncodeForTest(authCodeInfo) +
		"&host=" + urlEncodeForTest(server.URL) +
		"&loginTraceID=" + start.State +
		"&userInfo=" + urlEncodeForTest(`{"UserID":"70001","ScreenName":"Trae 测试号"}`)

	result, err := svc.CompleteLogin(context.Background(), start.State, callback)
	require.NoError(t, err)
	require.Equal(t, 1, hits)
	require.Equal(t, "70001", result.UserID)
	require.Equal(t, "Trae 测试号", result.DisplayName)
	require.Equal(t, trae.VariantTraeCN, result.Variant)

	creds := result.Credentials
	require.NotEmpty(t, creds["jwt"])
	require.Equal(t, "fresh-refresh", creds["refresh_token"])
	require.Equal(t, "trae_cn", creds["variant"])
	require.Equal(t, APIProtocolAdaptive, creds["api_protocol"])
	require.NotEmpty(t, creds["expires_at"])
	// 默认路径：虚拟设备身份成套落库（私钥与交换上报的公钥同源）。
	require.NotEmpty(t, creds["device_id"])
	require.Len(t, creds["device_id"], 16)
	require.Contains(t, creds["device_private_key"], "BEGIN PRIVATE KEY")
	require.NotContains(t, creds, "device_public_key")

	// 二次调用（建号）直接取缓存，不重复交换。
	again, err := svc.CompleteLogin(context.Background(), start.State, callback)
	require.NoError(t, err)
	require.Equal(t, 1, hits)
	require.Equal(t, creds, again.Credentials)

	// machine_id 同源：授权 URL 里的 machine_id 与落库值必须是同一份（会话透传）。
	machineID, _ := creds["machine_id"].(string)
	require.NotEmpty(t, machineID)
	require.Contains(t, start.URL, "machine_id="+machineID)

	// 显式 id+公钥对覆盖路径：不存私钥，id/公钥原样落库（refresh 走旧协议兜底）。
	explicitStart, err := svc.GenerateLoginURL(context.Background(), "trae_cn", nil, "9988776655443322", "-----BEGIN PUBLIC KEY-----explicit")
	require.NoError(t, err)
	explicitResult, err := svc.CompleteLogin(context.Background(), explicitStart.State,
		"authCodeInfo="+urlEncodeForTest(authCodeInfo)+
			"&host="+urlEncodeForTest(server.URL)+
			"&loginTraceID="+explicitStart.State)
	require.NoError(t, err)
	require.Equal(t, "-----BEGIN PUBLIC KEY-----explicit", explicitResult.Credentials["device_public_key"])
	require.Equal(t, "9988776655443322", explicitResult.Credentials["device_id"])
	require.NotContains(t, explicitResult.Credentials, "device_private_key")

	// 单独提供公钥（缺 device_id）按不配对拒绝。
	_, err = svc.GenerateLoginURL(context.Background(), "trae_cn", nil, "", "-----BEGIN PUBLIC KEY-----orphan")
	require.Error(t, err)
	require.Contains(t, err.Error(), "成对")

	// 未知会话报错。
	_, err = svc.CompleteLogin(context.Background(), "no-such-state", callback)
	require.Error(t, err)
}

func TestTraeOAuthService_CompleteLogin_RejectsTraceMismatch(t *testing.T) {
	t.Parallel()
	server := traeExchangeStub(t, "jwt", "refresh", nil)
	svc := NewTraeOAuthService(nil, nil)

	start, err := svc.GenerateLoginURL(context.Background(), "solo_cn", nil, "", "")
	require.NoError(t, err)

	authCodeInfo := `{"AuthCode":"auth-code-1"}`
	callback := "authCodeInfo=" + urlEncodeForTest(authCodeInfo) +
		"&host=" + urlEncodeForTest(server.URL) +
		"&loginTraceID=forged-trace"

	_, err = svc.CompleteLogin(context.Background(), start.State, callback)
	require.Error(t, err)
	require.Contains(t, err.Error(), "loginTraceID")
}

// withTraeRefreshStub 替换 pkg 刷新调用的注入点（rotated 为空 = 未轮换）。
func withTraeRefreshStub(t *testing.T, accessToken, rotated string, calls *int) {
	t.Helper()
	original := traeRefreshAccessToken
	traeRefreshAccessToken = func(_ context.Context, _ *http.Client, _ trae.Variant, creds *trae.Credentials, _ trae.ClientOptions) (*trae.TokenRefreshResult, error) {
		if calls != nil {
			*calls++
		}
		result := &trae.TokenRefreshResult{AccessToken: accessToken, RefreshToken: creds.RefreshToken}
		if rotated != "" {
			result.RefreshToken = rotated
			result.RefreshRotated = true
		}
		return result, nil
	}
	t.Cleanup(func() { traeRefreshAccessToken = original })
}

func TestTraeOAuthService_RefreshAccountToken(t *testing.T) {
	// 注入点替换全局变量，不与其他桩用例并行。

	newJWT := traeTestJWT("70001", time.Now().Add(24*time.Hour))
	calls := 0
	withTraeRefreshStub(t, newJWT, "rotated-refresh", &calls)
	svc := NewTraeOAuthService(nil, nil)

	account := &Account{
		ID:          301,
		Platform:    PlatformTrae,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{
			"jwt":                "old-jwt",
			"refresh_token":      "old-refresh",
			"variant":            "solo_global",
			"uid":                "70001",
			"device_id":          "1234567890123456",
			"machine_id":         "0f4a8b2c-1111-2222-3333-444455556666",
			"device_private_key": "-----BEGIN PRIVATE KEY-----fake",
			"api_protocol":       APIProtocolAdaptive,
		},
	}
	credentials, err := svc.RefreshAccountToken(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, newJWT, credentials["jwt"])
	require.Equal(t, "rotated-refresh", credentials["refresh_token"])
	require.Equal(t, "solo_global", credentials["variant"])
	require.Equal(t, APIProtocolAdaptive, credentials["api_protocol"])
	require.NotEmpty(t, credentials["expires_at"])
	// 设备身份成套保留。
	require.Equal(t, "1234567890123456", credentials["device_id"])
	require.Equal(t, "0f4a8b2c-1111-2222-3333-444455556666", credentials["machine_id"])
	require.Equal(t, "-----BEGIN PRIVATE KEY-----fake", credentials["device_private_key"])

	// 未轮换：结果保留旧 refresh_token。
	withTraeRefreshStub(t, newJWT, "", nil)
	credentials, err = svc.RefreshAccountToken(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "old-refresh", credentials["refresh_token"])

	// 平台不符 / 缺 refresh_token 报错（不触网）。
	_, err = svc.RefreshAccountToken(context.Background(), &Account{ID: 1, Platform: PlatformOpenAI})
	require.Error(t, err)
	noRefresh := traeTestAccount()
	delete(noRefresh.Credentials, "refresh_token")
	_, err = svc.RefreshAccountToken(context.Background(), noRefresh)
	require.Error(t, err)
}

func TestTraeTokenRefresher_Gates(t *testing.T) {
	t.Parallel()
	refresher := NewTraeTokenRefresher(nil)

	account := traeTestAccount()
	require.True(t, refresher.CanRefresh(account))
	require.Equal(t, "trae_token:201", refresher.CacheKey(account))

	noRefresh := traeTestAccount()
	delete(noRefresh.Credentials, "refresh_token")
	require.False(t, refresher.CanRefresh(noRefresh))
	require.False(t, refresher.CanRefresh(rawChatCompletionsTestAccount()))

	// expires_at 缺失 → 需要刷新；远期 → 不需要；30 分钟内 → 需要。
	require.True(t, refresher.NeedsRefresh(account, time.Hour))
	future := &Account{Platform: PlatformTrae, Type: AccountTypeOAuth, Credentials: map[string]any{
		"expires_at": time.Now().Add(2 * time.Hour).Format(time.RFC3339)}}
	require.False(t, refresher.NeedsRefresh(future, time.Hour))
	soon := &Account{Platform: PlatformTrae, Type: AccountTypeOAuth, Credentials: map[string]any{
		"expires_at": time.Now().Add(10 * time.Minute).Format(time.RFC3339)}}
	require.True(t, refresher.NeedsRefresh(soon, time.Hour))
}

func TestTraeTokenRefresher_RefreshMergesOldCredentials(t *testing.T) {
	// 注入点替换全局变量，不与其他桩用例并行。

	newJWT := traeTestJWT("70001", time.Now().Add(24*time.Hour))
	withTraeRefreshStub(t, newJWT, "rotated-refresh", nil)
	svc := NewTraeOAuthService(nil, nil)
	refresher := NewTraeTokenRefresher(svc)

	account := traeTestAccount()
	account.Credentials["model_mapping"] = map[string]any{"public": "glm-5.3"}

	merged, err := refresher.Refresh(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, newJWT, merged["jwt"])
	require.Equal(t, "rotated-refresh", merged["refresh_token"])
	// 旧凭据中的运营配置保留。
	require.Contains(t, merged, "model_mapping")
	require.Equal(t, "solo_cn", merged["variant"])
}
