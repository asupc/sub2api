package qoder

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// pkceAlphabet RFC 7636 verifier 字符集（qoder_accounts.py L3288-3295）。
const pkceAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~"

// MakePKCE 生成 RFC 7636 S256 (verifier, challenge)。
func MakePKCE() (verifier, challenge string) {
	raw := make([]byte, 64)
	if _, err := rand.Read(raw); err != nil {
		panic("qoder: rand failed: " + err.Error())
	}
	var b strings.Builder
	b.Grow(len(raw))
	for _, x := range raw {
		b.WriteByte(pkceAlphabet[int(x)%len(pkceAlphabet)])
	}
	verifier = b.String()
	sum := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	return verifier, challenge
}

// LoginSession 一次设备授权登录的会话状态（Pool.start_login L3098-3131）。
type LoginSession struct {
	State    string
	AuthURL  string
	Verifier string
	Nonce    string
	Realm    string
	Platform string
	// CreatedAt 登录窗口起点；超过 LoginTTLSeconds 应作废重开。
	CreatedAt time.Time
}

// StartLogin 构造 PKCE 设备授权 URL（浏览器打开完成授权）。
//
// 双区 URL 参数差异（官方逆向，qoder_accounts.py L3098-3131）：
//
//	CN   : challenge, challenge_method, nonce(带横线), redirect_uri,
//	       client_id, machine_id
//	Intl : challenge, challenge_method, nonce(32-hex), client_id,
//	       machine_id（新协议带 client_id/machine_id、不带 redirect_uri）
//
// machineID 为设备标识：源实现优先读本机官方客户端的 machine_id 文件、
// 缺则生成 uuid；服务器场景由调用方传入（建议稳定存储，如渠道级配置，
// 缺省此处生成 uuid4）。
func StartLogin(realm, platform, machineID string) *LoginSession {
	cfg := Realm(realm)
	verifier, challenge := MakePKCE()
	nonce := UUID4Hex()
	if cfg.NonceDashed {
		nonce = UUID4()
	}
	// 与源实现的 urlencode 插入顺序一致（challenge, challenge_method, nonce,
	// [redirect_uri], [client_id, machine_id]）
	q := "challenge=" + queryEscape(challenge) +
		"&challenge_method=S256" +
		"&nonce=" + queryEscape(nonce)
	if cfg.SendRedirectURI {
		q += "&redirect_uri=" + queryEscape(cfg.RedirectURI)
	}
	if cfg.SendClientID {
		q += "&client_id=" + queryEscape(cfg.ClientID) +
			"&machine_id=" + queryEscape(machineID)
	}
	if platform == "" {
		platform = "CLI"
	}
	return &LoginSession{
		State:     fmt.Sprintf("qd-%d", time.Now().UnixNano()),
		AuthURL:   cfg.Website + "/device/selectAccounts?" + q,
		Verifier:  verifier,
		Nonce:     nonce,
		Realm:     realm,
		Platform:  platform,
		CreatedAt: time.Now(),
	}
}

func queryEscape(s string) string {
	return url.QueryEscape(s)
}

// readAllClip 读取响应体（上限 1MB，防御异常大响应）。
func readAllClip(r io.Reader) (string, error) {
	data, err := io.ReadAll(io.LimitReader(r, 1<<20))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// PollResult 设备授权轮询结果。
type PollResult struct {
	// Status: pending（等待浏览器完成授权）/ ok / error。
	Status string
	// Token 设备令牌族凭证（dt- access / drt- refresh）。
	Token *TokenSet
	// UserInfo 尽力而为拉取的身份（可为 nil）。
	UserInfo *UserInfo
	Message  string
}

// TokenSet 一组刷新后的凭证。
type TokenSet struct {
	AccessToken  string
	RefreshToken string
	// ExpiresAt unix 秒。
	ExpiresAt int64
	// Kind: device（deviceToken/refresh）/ job（jobToken/refresh 或 exchange）。
	Kind string
}

// SessionDeadError 会话已死（401/403 + TOKEN_EXPIRE 标记）：
// 刷新无意义，需要重新登录。
type SessionDeadError struct {
	Detail string
}

func (e *SessionDeadError) Error() string {
	return "qoder: session dead (TOKEN_EXPIRE): re-login required: " + e.Detail
}

// PollLogin 轮询设备授权（Pool.poll_login L3133-3227）。
//
// GET {openapi}/api/v1/deviceToken/poll?nonce=&verifier=&challenge_method=S256，
// 本调用不带账号凭证（设备流的 nonce/verifier challenge，无 Authorization）；
// 404/202 = 用户尚未完成授权（继续轮询）。
func (c *Client) PollLogin(ctx context.Context, sess *LoginSession) (*PollResult, error) {
	cfg := Realm(sess.Realm)
	q := "nonce=" + queryEscape(sess.Nonce) +
		"&verifier=" + queryEscape(sess.Verifier) +
		"&challenge_method=S256"
	url := cfg.OpenAPI + PathDevicePoll + "?" + q

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "QoderWork")
	resp, err := c.http().Do(req)
	if err != nil {
		// 网络类错误视作 pending（与源实现一致，继续轮询）
		return &PollResult{Status: "pending", Message: "poll error: " + err.Error()}, nil
	}
	defer resp.Body.Close()
	body, err := readAllClip(resp.Body)
	if err != nil {
		return &PollResult{Status: "pending", Message: "poll error: " + err.Error()}, nil
	}
	if resp.StatusCode == 404 || resp.StatusCode == 202 {
		return &PollResult{Status: "pending", Message: "等待浏览器完成 Qoder 设备授权"}, nil
	}
	if resp.StatusCode != http.StatusOK {
		return &PollResult{Status: "error",
			Message: fmt.Sprintf("poll http %d %s", resp.StatusCode, clip(body, 160))}, nil
	}
	var data struct {
		Token        string `json:"token"`
		DeviceToken  string `json:"device_token"`
		RefreshToken string `json:"refresh_token"`
		UserID       string `json:"user_id"`
		ExpiresIn    any    `json:"expires_in"`
		ExpiresAt    string `json:"expires_at"`
	}
	if err := json.Unmarshal([]byte(body), &data); err != nil {
		return &PollResult{Status: "pending", Message: "waiting for grant"}, nil
	}
	token := data.Token
	if token == "" {
		token = data.DeviceToken
	}
	if token == "" {
		return &PollResult{Status: "pending", Message: "waiting for token"}, nil
	}
	ts := &TokenSet{
		AccessToken:  token,
		RefreshToken: data.RefreshToken,
		ExpiresAt:    deviceExpiry(data.ExpiresIn, data.ExpiresAt, time.Now()),
		Kind:         "device",
	}
	// 拉取 userinfo 补全昵称/用户类型（尽力而为，不阻塞入库）
	var ui *UserInfo
	uiReq, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.OpenAPI+PathUserinfo, nil)
	if err == nil {
		uiReq.Header.Set("Accept", "application/json")
		uiReq.Header.Set("User-Agent", ClientUA)
		uiReq.Header.Set("Authorization", "Bearer "+token)
		if uiResp, err := c.http().Do(uiReq); err == nil {
			defer uiResp.Body.Close()
			if uiResp.StatusCode == http.StatusOK {
				ui = &UserInfo{}
				if err := json.NewDecoder(uiResp.Body).Decode(ui); err != nil {
					ui = nil
				}
			}
		}
	}
	return &PollResult{Status: "ok", Token: ts, UserInfo: ui}, nil
}

// deviceExpiry deviceToken 响应的过期时间（qoder_accounts.py L2452-2464）：
// expires_in(ms) 优先，其次 expires_at(RFC3339 前 19 字符，本地时区)，
// 缺省 30 天。
func deviceExpiry(expiresIn any, expiresAt string, now time.Time) int64 {
	switch v := expiresIn.(type) {
	case float64:
		if v > 0 {
			return now.Unix() + int64(v)/1000
		}
	case string:
		var ms float64
		if _, err := fmt.Sscanf(v, "%g", &ms); err == nil && ms > 0 {
			return now.Unix() + int64(ms)/1000
		}
	}
	if len(expiresAt) >= 19 {
		if t, err := time.ParseInLocation("2006-01-02T15:04:05",
			expiresAt[:19], time.Local); err == nil {
			return t.Unix()
		}
	}
	return now.Unix() + 30*86400
}

// ImportPAT 导入 pt- 个人访问令牌：交换 jobToken 并拉取身份
// （Pool.import_pat L3234-3285）。返回 (jobToken 凭证, 身份, 错误)；
// userinfo 拉取失败时身份为 nil（尽力而为）。
func (c *Client) ImportPAT(ctx context.Context, realm, pat string) (*TokenSet, *UserInfo, error) {
	pat = strings.TrimSpace(pat)
	if !strings.HasPrefix(pat, "pt-") {
		return nil, nil, fmt.Errorf("qoder: PAT must start with pt-")
	}
	cfg := Realm(realm)
	payload, _ := json.Marshal(map[string]string{"personal_token": pat})
	h := tokenPostHeaders()
	var data struct {
		Token        string `json:"token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    any    `json:"expires_in"`
	}
	if err := c.doJSON(ctx, http.MethodPost, cfg.OpenAPI+PathJobExchange, payload, h, &data); err != nil {
		return nil, nil, err
	}
	if data.Token == "" {
		return nil, nil, fmt.Errorf("qoder: jobToken exchange returned no token")
	}
	expires := time.Now().Unix() + 24*3600
	if v, ok := data.ExpiresIn.(float64); ok && v > 0 {
		expires = time.Now().Unix() + int64(v)/1000
	}
	ts := &TokenSet{AccessToken: data.Token, RefreshToken: data.RefreshToken,
		ExpiresAt: expires, Kind: "job"}
	ui, _ := c.Userinfo(ctx, Credentials{AccessToken: data.Token, Realm: realm})
	return ts, ui, nil
}

// tokenPostHeaders 刷新/交换端点的统一头（_post_token L1752-1757）。
func tokenPostHeaders() http.Header {
	h := http.Header{}
	h.Set("Content-Type", "application/json")
	h.Set("Accept", "application/json")
	h.Set("User-Agent", ClientUA)
	return h
}

// ErrNoRefreshToken 既无 refresh token 也无 PAT（需重新登录）。
var ErrNoRefreshToken = fmt.Errorf("qoder: no refresh token; sign in again")

// RefreshAccessToken 刷新 access token，按 refresh token 前缀路由
// （Account.refresh L1728-1750）：
//
//	drt- -> POST /api/v1/deviceToken/refresh {"refresh_token"}
//	jrt- -> POST /api/v1/jobToken/refresh    {"refresh_token"}，
//	        失败回落 PAT 重新交换（personalToken）
//
// 401/403 且命中会话死亡标记时返回 *SessionDeadError。
// 返回的 TokenSet 在刷新响应未携带新 refresh token 时沿用旧值。
func (c *Client) RefreshAccessToken(ctx context.Context, realm, refreshToken, personalToken string) (*TokenSet, error) {
	cfg := Realm(realm)
	base := cfg.OpenAPI

	postToken := func(path string, payload []byte, kind string) (*TokenSet, error) {
		var data struct {
			Token        string `json:"token"`
			DeviceToken  string `json:"device_token"`
			RefreshToken string `json:"refresh_token"`
			ExpiresIn    any    `json:"expires_in"`
		}
		err := c.doJSON(ctx, http.MethodPost, base+path, payload, tokenPostHeaders(), &data)
		if err != nil {
			if se, ok := err.(*StatusError); ok && (se.Code == 401 || se.Code == 403) {
				if SessionDead(se.Body) {
					return nil, &SessionDeadError{Detail: se.Body}
				}
				return nil, fmt.Errorf("qoder: refresh failed: HTTP %d %s",
					se.Code, clip(se.Body, 160))
			}
			return nil, fmt.Errorf("qoder: refresh failed: %w", err)
		}
		token := data.Token
		if token == "" {
			token = data.DeviceToken
		}
		if token == "" {
			return nil, fmt.Errorf("qoder: refresh returned no token")
		}
		refresh := data.RefreshToken
		if refresh == "" {
			refresh = refreshToken
		}
		var expires int64
		if kind == "device" {
			expires = deviceExpiry(data.ExpiresIn, "", time.Now())
		} else if v, ok := data.ExpiresIn.(float64); ok && v > 0 {
			expires = time.Now().Unix() + int64(v)/1000
		}
		return &TokenSet{AccessToken: token, RefreshToken: refresh,
			ExpiresAt: expires, Kind: kind}, nil
	}

	// 1) OAuth 设备族
	if strings.HasPrefix(refreshToken, "drt-") {
		payload, _ := json.Marshal(map[string]string{"refresh_token": refreshToken})
		return postToken(PathDeviceRefresh, payload, "device")
	}
	// 2) jobToken 族：jrt- 优先，失败回落 PAT 重新交换
	if refreshToken != "" {
		payload, _ := json.Marshal(map[string]string{"refresh_token": refreshToken})
		if ts, err := postToken(PathJobRefresh, payload, "job"); err == nil {
			return ts, nil
		}
	}
	if personalToken != "" {
		payload, _ := json.Marshal(map[string]string{"personal_token": personalToken})
		if ts, err := postToken(PathJobExchange, payload, "job"); err == nil {
			return ts, nil
		}
	}
	if refreshToken == "" && personalToken == "" {
		return nil, ErrNoRefreshToken
	}
	return nil, fmt.Errorf("qoder: all refresh routes failed")
}
