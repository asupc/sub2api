// login.go WorkBuddy OAuth 设备授权流程（无 PKCE——设备流由服务端签发 state）。
//
// 移植自 workbuddy2api-panel internal/panel/login.go：
//
//	POST /v2/plugin/auth/state?platform=CLI   → 拿 state + authUrl（用户浏览器打开授权）
//	GET  /v2/plugin/auth/token?state=<state>  → 授权完成前业务 code 非 0（"login ing"）
//	GET  /v2/plugin/login/account?state=<state> → 取 uid/nickname/enterpriseId
//
// 调用方节奏：Start 拿授权 URL 给用户 → 每 3s 轮询 Poll（nil,nil = 仍在等待）→
// 完成后拿到完整 Credentials。state 会话存进程内（map+mutex，TTL 过期回收）。
package workbuddy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// loginClientUA 设备授权专用 UA（CLI 形态，逐字对齐源项目常量）。
const loginClientUA = "CLI/2.63.2 CodeBuddy/2.63.2"

// loginTTL 授权 state 会话的最长有效期：超时的 state 直接回收
// （源项目实测 15 分钟；防「开弹窗走开」的 state 滞留）。
const loginTTL = 15 * time.Minute

// loginEndpoints 按 realm 返回设备授权三端点 + Origin/Referer 基础域。
// realm=="global" → 国际版（workbuddy.ai 同域）；cn/缺省 → CN。
func loginEndpoints(realm string) (state, token, account, origin string) {
	base, org := chatBaseCN, originRefererCN
	if realm == RealmGlobal {
		base, org = globalBase, originRefererGlobal
	}
	return base + "/v2/plugin/auth/state?platform=CLI",
		base + "/v2/plugin/auth/token?state=",
		base + "/v2/plugin/login/account?state=",
		org
}

// loginEndpointsFn 端点解析的可替换引用（测试注入 httptest 基址用）。
var loginEndpointsFn = loginEndpoints

// loginSession 进行中的设备授权会话。
type loginSession struct {
	created time.Time
	realm   string
}

// DeviceLogin OAuth 设备授权管理器：进程内 state 会话表（map+mutex）。
// 实例持有（非包级单例），调用方按需创建；httpClient 为 nil 用缺省短超时 client。
type DeviceLogin struct {
	httpClient *http.Client
	now        func() time.Time

	mu       sync.Mutex
	sessions map[string]loginSession // state → 会话
}

// NewDeviceLogin 构造设备授权管理器（30s 超时、无 cookie——每请求携带 state）。
func NewDeviceLogin(httpClient *http.Client) *DeviceLogin {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &DeviceLogin{httpClient: httpClient, now: time.Now, sessions: map[string]loginSession{}}
}

// Start 发起设备授权：POST auth/state 拿授权 URL，state 会话记 realm。
// 返回 (authURL, state, error)；调用方把 authURL 交给用户在浏览器完成授权。
func (d *DeviceLogin) Start(ctx context.Context, realm string) (authURL string, state string, err error) {
	if realm == "" {
		realm = RealmCN
	}
	epState, _, _, origin := loginEndpointsFn(realm)
	data, _, err := d.doJSON(ctx, http.MethodPost, epState, "", nil, origin)
	if err != nil {
		return "", "", fmt.Errorf("auth state: %w", err)
	}
	var st struct {
		State   string `json:"state"`
		AuthURL string `json:"authUrl"`
	}
	if err := json.Unmarshal(data, &st); err != nil || st.State == "" || st.AuthURL == "" {
		return "", "", fmt.Errorf("auth state: missing state or authUrl")
	}
	d.mu.Lock()
	// 顺手回收过期会话，防 state 滞留。
	for s, sess := range d.sessions {
		if d.since(sess.created) > loginTTL {
			delete(d.sessions, s)
		}
	}
	d.sessions[st.State] = loginSession{created: d.nowTime(), realm: realm}
	d.mu.Unlock()
	return st.AuthURL, st.State, nil
}

// Poll 轮询登录态。返回 (nil, nil) 表示仍在等待授权（调用方继续轮询）；
// 授权完成后返回完整凭证（含 realm 标识）并移除会话；state 未知/过期返回错误。
//
// token 端点是权威登录状态端点：pending 时业务 code 非 0（"login ing"）。
// account 端点失败不阻塞（仅缺展示名），但 uid 缺失视为失败（无 uid 无法出站）。
func (d *DeviceLogin) Poll(ctx context.Context, state string) (*Credentials, error) {
	if state == "" {
		return nil, fmt.Errorf("missing state")
	}
	d.mu.Lock()
	sess, known := d.sessions[state]
	d.mu.Unlock()
	if !known {
		return nil, fmt.Errorf("unknown or expired state（请重新发起添加账号）")
	}
	if d.since(sess.created) > loginTTL {
		d.mu.Lock()
		delete(d.sessions, state)
		d.mu.Unlock()
		return nil, fmt.Errorf("unknown or expired state（请重新发起添加账号）")
	}
	_, epToken, epAcct, origin := loginEndpointsFn(sess.realm)

	tokRaw, _, err := d.doJSON(ctx, http.MethodGet, epToken+state, "", nil, origin)
	if err != nil {
		// pending / 未完成：调用方继续轮询。
		return nil, nil
	}
	var tok struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ExpiresIn    int64  `json:"expiresIn"`
		Domain       string `json:"domain"`
	}
	if err := json.Unmarshal(tokRaw, &tok); err != nil || tok.AccessToken == "" {
		return nil, nil // 等待授权完成
	}

	// 完成：取 uid/nickname/enterpriseId（失败不阻塞，仅缺展示名）。
	var acct struct {
		UID          string `json:"uid"`
		EnterpriseID string `json:"enterpriseId"`
		Nickname     string `json:"nickname"`
	}
	if acctRaw, _, aerr := d.doJSON(ctx, http.MethodGet, epAcct+state, tok.AccessToken, nil, origin); aerr == nil {
		_ = json.Unmarshal(acctRaw, &acct)
	}
	if acct.UID == "" {
		return nil, fmt.Errorf("login done but no uid（token 已发但账号信息获取失败，请重试）")
	}
	// UID 来自上游响应，未经校验就拼存储键会被路径穿越利用。UID 是腾讯侧账号标识，
	// 实测为 UUID 形态，故只放行 [A-Za-z0-9_-] 且长度 <= 64。
	if !validUID(acct.UID) {
		return nil, fmt.Errorf("上游返回的 uid 含非法字符，拒绝（防路径穿越）")
	}

	cred := &Credentials{
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		ExpiresAt:    d.nowTime().Add(time.Duration(tok.ExpiresIn) * time.Second).Unix(),
		Domain:       tok.Domain,
		Realm:        sess.realm,
		UID:          acct.UID,
		EnterpriseID: acct.EnterpriseID,
		Nickname:     acct.Nickname,
	}
	d.mu.Lock()
	delete(d.sessions, state)
	d.mu.Unlock()
	return cred, nil
}

// doJSON 发一次登录域 JSON 请求并解信封（与主客户端 doJSON 同语义但独立超时与头）。
// 返回 (data, httpStatus, error)；HTTP >= 300 或业务 code != 0 时带 status 返回错误
// （Poll 据此把 pending 与真错误都按「未完成」处理，Start 则直接报错）。
func (d *DeviceLogin) doJSON(ctx context.Context, method, fullURL, bearer string, body io.Reader, origin string) (json.RawMessage, int, error) {
	req, err := http.NewRequestWithContext(ctx, method, fullURL, body)
	if err != nil {
		return nil, 0, err
	}
	// 登录域请求头（与官方 CLI 设备流一致）：无 X-CodeBuddy-Request、无 Bearer 指纹头。
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Origin", origin)
	req.Header.Set("Referer", origin+"/")
	req.Header.Set("User-Agent", loginClientUA)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := d.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return nil, resp.StatusCode, fmt.Errorf("http_error: upstream %d", resp.StatusCode)
	}
	var env apiEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("parse failed: %w", err)
	}
	if env.Code != 0 {
		return nil, resp.StatusCode, fmt.Errorf("code=%d msg=%s", env.Code, env.Msg)
	}
	return env.Data, resp.StatusCode, nil
}

// validUID 校验上游返回的 uid 是否可安全用于拼存储键：只放行字母、数字、下划线、
// 连字符，长度上限 64；拒绝 . / \ 等路径字符与空串。
func validUID(uid string) bool {
	if uid == "" || len(uid) > 64 {
		return false
	}
	for _, c := range uid {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}

// nowTime 返回管理器时钟（测试可注入）。
func (d *DeviceLogin) nowTime() time.Time {
	if d.now != nil {
		return d.now()
	}
	return time.Now()
}

// since 会话存活时长。
func (d *DeviceLogin) since(t time.Time) time.Duration { return d.nowTime().Sub(t) }
