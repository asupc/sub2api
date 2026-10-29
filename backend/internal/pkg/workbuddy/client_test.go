//go:build unit

package workbuddy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// rewriteTransport 把所有出站请求的 scheme/host 重写到测试服务器
// （Client 基址是常量，注入点选在 http.Client 的 Transport——不改生产结构）。
type rewriteTransport struct {
	base string
	rt   http.RoundTripper
}

func (t rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.URL.Scheme = "http"
	clone.URL.Host = strings.TrimPrefix(t.base, "http://")
	if t.rt != nil {
		return t.rt.RoundTrip(clone)
	}
	return http.DefaultTransport.RoundTrip(clone)
}

// newTestClient 构造所有上游域都指向 srv 的客户端。
func newTestClient(srv *httptest.Server) *Client {
	return NewClient(&http.Client{Transport: rewriteTransport{base: srv.URL}})
}

// sha256Hex16 sha256(s) 前 16 字节的 hex（黄金值计算辅助）。
func sha256Hex16(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:16])
}

// TestRefreshToken 刷新端点：URL/方法/头/体与凭证更新逻辑。
func TestRefreshToken(t *testing.T) {
	var gotPath, gotMethod, gotRefresh, gotSource string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		gotRefresh = r.Header.Get("X-Refresh-Token")
		gotSource = r.Header.Get("X-Auth-Refresh-Source")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"accessToken":"at-2","refreshToken":"rt-2","expiresIn":5184000,"domain":"copilot.tencent.com"}}`))
	}))
	defer srv.Close()
	c := newTestClient(srv)

	base := time.Unix(1700000000, 0)
	c.now = func() time.Time { return base }
	cred := Credentials{AccessToken: "at-1", RefreshToken: "rt-1", UID: "u"}

	out, err := c.RefreshToken(context.Background(), cred)
	if err != nil {
		t.Fatalf("RefreshToken: %v", err)
	}
	if gotPath != "/v2/plugin/auth/token/refresh" || gotMethod != http.MethodPost {
		t.Errorf("endpoint=%s %s", gotMethod, gotPath)
	}
	if gotRefresh != "rt-1" || gotSource != "plugin" {
		t.Errorf("refresh headers: token=%q source=%q", gotRefresh, gotSource)
	}
	if out.AccessToken != "at-2" || out.RefreshToken != "rt-2" {
		t.Errorf("token 未更新: %+v", out)
	}
	if want := base.Add(60 * 24 * time.Hour).Unix(); out.ExpiresAt != want {
		t.Errorf("ExpiresAt=%d want %d（60d）", out.ExpiresAt, want)
	}
	// 传入的凭证不被改写（值语义）。
	if cred.AccessToken != "at-1" {
		t.Error("原凭证被改写")
	}

	// 无 refreshToken 拒绝。
	if _, err := c.RefreshToken(context.Background(), Credentials{AccessToken: "x"}); err == nil {
		t.Error("缺 refreshToken 应报错")
	}
}

// TestRefreshTokenDirtyExpiresIn 超量级 expiresIn 按脏值处理（保留旧过期时间）。
func TestRefreshTokenDirtyExpiresIn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"data":{"accessToken":"at-2","expiresIn":999999999999}}`))
	}))
	defer srv.Close()
	c := newTestClient(srv)
	cred := Credentials{AccessToken: "at-1", RefreshToken: "rt-1", ExpiresAt: 111}
	out, err := c.RefreshToken(context.Background(), cred)
	if err != nil {
		t.Fatalf("RefreshToken: %v", err)
	}
	if out.ExpiresAt != 111 {
		t.Errorf("脏 expiresIn 不应写回，got %d want 111", out.ExpiresAt)
	}
	if out.RefreshToken != "rt-1" {
		t.Errorf("缺省 refreshToken 应保留旧值，got %q", out.RefreshToken)
	}
}

// TestChatStreamContext chat 出站：路径/头/体归一化 + 错误分类 + Retry-After。
func TestChatStreamContext(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	var gotUA, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotUA = r.Header.Get("User-Agent")
		gotAuth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[]}\n\n"))
	}))
	defer srv.Close()
	c := newTestClient(srv)
	cred := cnCred()
	cred.UID = "0123456789abcdef"

	body, _ := json.Marshal(map[string]any{
		"model":    "glm-5.2",
		"messages": []map[string]any{{"role": "user", "content": "hi"}},
	})
	resp, err := c.ChatStreamContext(context.Background(), cred, body, ChatMeta{ConversationID: "conv-9"})
	if err != nil {
		t.Fatalf("ChatStreamContext: %v", err)
	}
	defer resp.Body.Close()
	if gotPath != chatCompletionsPath {
		t.Errorf("path=%s want %s", gotPath, chatCompletionsPath)
	}
	if gotUA != "WorkBuddy/5.8.3 WorkBuddy/5.8.3 CLI/2.161.0" {
		t.Errorf("UA=%q", gotUA)
	}
	if gotAuth != "Bearer at" {
		t.Errorf("Authorization=%q", gotAuth)
	}
	if gotBody["stream"] != true {
		t.Error("应强制 stream:true")
	}
	so, _ := gotBody["stream_options"].(map[string]any)
	if so == nil || so["include_usage"] != true {
		t.Error("应补 stream_options.include_usage")
	}
	// prompt_cache_key 黄金值（uid=0123456789abcdef, conv=conv-9）。
	if gotBody["prompt_cache_key"] != "wb2a-01234567-"+sha256Hex16("0123456789abcdef|conv-9") {
		t.Errorf("prompt_cache_key=%v", gotBody["prompt_cache_key"])
	}
}

// TestChatStreamError 错误路径：429 分类 + Retry-After 头解析。
func TestChatStreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "77")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"code":6004,"msg":"将在 2026-09-11 18:33:27 UTC+8 重置"}`))
	}))
	defer srv.Close()
	c := newTestClient(srv)
	resp, err := c.ChatStreamContext(context.Background(), cnCred(), []byte(`{"model":"m"}`), ChatMeta{})
	if resp != nil {
		t.Error("错误路径应返回 nil 响应")
	}
	ue, ok := err.(*Error)
	if !ok {
		t.Fatalf("应返回 *Error，got %T %v", err, err)
	}
	if ue.Kind != KindSoftRate {
		t.Errorf("Kind=%v want soft_rate", ue.Kind)
	}
	if ue.Status != http.StatusTooManyRequests {
		t.Errorf("Status=%d", ue.Status)
	}
	if ue.RetryAfter != 77*time.Second {
		t.Errorf("RetryAfter=%v want 77s", ue.RetryAfter)
	}
	if !strings.Contains(ue.RawBody, "6004") {
		t.Errorf("RawBody 应带原文: %q", ue.RawBody)
	}
}

// TestChatStreamSessionDead session 失效错误直达分类。
func TestChatStreamSessionDead(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":12153,"msg":"Offline user session not found"}`))
	}))
	defer srv.Close()
	c := newTestClient(srv)
	_, err := c.ChatStreamContext(context.Background(), cnCred(), []byte(`{"model":"m"}`), ChatMeta{})
	ue, ok := err.(*Error)
	if !ok || ue.Kind != KindSessionDead {
		t.Fatalf("应分类 session_dead，got %v", err)
	}
}

// parseObj 解析 JSON 到全新 map（避免多次 Unmarshal 合并残留键）。
func parseObj(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		t.Fatalf("parse %s: %v", body, err)
	}
	return obj
}

// TestPrepareChatBody body 协议归一化各分支。
func TestPrepareChatBody(t *testing.T) {
	// max_completion_tokens 翻译 + 显式优先 + GPT 下限抬升。
	in := []byte(`{"model":"gpt-5.5","max_completion_tokens":8,"messages":[]}`)
	obj := parseObj(t, PrepareChatBody(Credentials{}, in, ""))
	if _, has := obj["max_completion_tokens"]; has {
		t.Error("别名应删除")
	}
	if obj["max_tokens"] != float64(16) {
		t.Errorf("GPT max_tokens<16 应抬到 16，got %v", obj["max_tokens"])
	}
	// 非 GPT 不抬。
	in = []byte(`{"model":"glm-5.2","max_tokens":8,"messages":[]}`)
	obj = parseObj(t, PrepareChatBody(Credentials{}, in, ""))
	if obj["max_tokens"] != float64(8) {
		t.Errorf("非 GPT 不应抬升，got %v", obj["max_tokens"])
	}

	// tool_choice 对象 → 字符串；developer → system；image_url 字符串 → 对象。
	in = []byte(`{"model":"m","tool_choice":{"type":"function","function":{"name":"myfn"}},
	  "messages":[{"role":"developer","content":"sys"},
	              {"role":"user","content":[{"type":"image_url","image_url":"data:image/png;base64,xx"},{"type":"text","text":"hi"}]}]}`)
	obj = parseObj(t, PrepareChatBody(Credentials{}, in, ""))
	if obj["tool_choice"] != "myfn" {
		t.Errorf("tool_choice=%v want myfn", obj["tool_choice"])
	}
	msgs := obj["messages"].([]any)
	first := msgs[0].(map[string]any)
	if first["role"] != "system" {
		t.Errorf("developer 应归一 system，got %v", first["role"])
	}
	second := msgs[1].(map[string]any)
	parts := second["content"].([]any)
	imgPart := parts[0].(map[string]any)
	if img, ok := imgPart["image_url"].(map[string]any); !ok || img["url"] != "data:image/png;base64,xx" {
		t.Errorf("image_url 应转对象形态，got %v", imgPart["image_url"])
	}

	// tool_choice none 保留工具声明（hub v1.5.7：删声明会把模型调用降级成
	// DSML 文本、Agent 原地空转；tool_choice 以字符串 "none" 表达「本轮不调用」）。
	in = []byte(`{"model":"m","tool_choice":{"type":"none"},"tools":[{"type":"function"}]}`)
	obj = parseObj(t, PrepareChatBody(Credentials{}, in, ""))
	if obj["tool_choice"] != "none" {
		t.Errorf("tool_choice none 应归一为字符串 \"none\"，got %v", obj["tool_choice"])
	}
	if _, has := obj["tools"]; !has {
		t.Error("tool_choice none 不应连带删 tools")
	}
	// 字符串 "none" 同款。
	in = []byte(`{"model":"m","tool_choice":"NONE","tools":[{"type":"function"}]}`)
	obj = parseObj(t, PrepareChatBody(Credentials{}, in, ""))
	if obj["tool_choice"] != "none" {
		t.Errorf("字符串 NONE 应归一为 \"none\"，got %v", obj["tool_choice"])
	}
	if _, has := obj["tools"]; !has {
		t.Error("字符串 none 不应删 tools")
	}

	// 客户端自带 prompt_cache_key 不覆盖。
	in = []byte(`{"model":"m","prompt_cache_key":"keep-me","messages":[]}`)
	obj = parseObj(t, PrepareChatBody(Credentials{UID: "u"}, in, "conv"))
	if obj["prompt_cache_key"] != "keep-me" {
		t.Errorf("显式 cache key 不应覆盖，got %v", obj["prompt_cache_key"])
	}

	// 坏 body 原样返回。
	bad := []byte(`{not-json`)
	if got := PrepareChatBody(Credentials{}, bad, ""); string(got) != string(bad) {
		t.Error("坏 body 应原样返回")
	}
}

// TestPrepareChatBodyGlobalEffort global 域 effort 降级（静态表）。
func TestPrepareChatBodyGlobalEffort(t *testing.T) {
	// deepseek-v4.1-flash 国际版只认 high：low → high（≤请求档的最高支持档）。
	in := []byte(`{"model":"deepseek-v4.1-flash","reasoning_effort":"low","messages":[]}`)
	obj := parseObj(t, PrepareChatBody(Credentials{Realm: "global"}, in, ""))
	if obj["reasoning_effort"] != "high" {
		t.Errorf("global low 应降级 high，got %v", obj["reasoning_effort"])
	}
	// max（高于全部支持档）→ 最低支持档 high。
	in = []byte(`{"model":"deepseek-v4.1-flash","reasoning_effort":"max","messages":[]}`)
	obj = parseObj(t, PrepareChatBody(Credentials{Realm: "global"}, in, ""))
	if obj["reasoning_effort"] != "high" {
		t.Errorf("global max 应回落 high，got %v", obj["reasoning_effort"])
	}
	// CN 域不降级（三档表但本包 CN 不做静态降级——远端权威，未缓存时透传）。
	in = []byte(`{"model":"deepseek-v4.1-flash","reasoning_effort":"low","messages":[]}`)
	obj = parseObj(t, PrepareChatBody(Credentials{}, in, ""))
	if obj["reasoning_effort"] != "low" {
		t.Errorf("CN 未缓存应透传 low，got %v", obj["reasoning_effort"])
	}
	// 未知模型透传。
	in = []byte(`{"model":"mystery","reasoning_effort":"low","messages":[]}`)
	obj = parseObj(t, PrepareChatBody(Credentials{Realm: "global"}, in, ""))
	if obj["reasoning_effort"] != "low" {
		t.Errorf("未知模型应透传，got %v", obj["reasoning_effort"])
	}
}

// TestEnsureConsoleSystem global 兜底 system 注入。
func TestEnsureConsoleSystem(t *testing.T) {
	out := ensureConsoleSystem([]byte(`{"messages":[{"role":"user","content":"hi"}]}`))
	var obj map[string]any
	_ = json.Unmarshal(out, &obj)
	msgs := obj["messages"].([]any)
	if len(msgs) != 2 || msgs[0].(map[string]any)["role"] != "system" {
		t.Errorf("应前置 system，got %v", msgs)
	}
	// 首条已是 system 不重复注入。
	out = ensureConsoleSystem([]byte(`{"messages":[{"role":"system","content":"s"},{"role":"user","content":"u"}]}`))
	_ = json.Unmarshal(out, &obj)
	msgs = obj["messages"].([]any)
	if len(msgs) != 2 {
		t.Errorf("已有 system 不应注入，got %d 条", len(msgs))
	}
}

// TestBuildCacheKey cache key 黄金值与账号隔离。
func TestBuildCacheKey(t *testing.T) {
	if got, want := buildCacheKey("0123456789abcdef", "conv-1"), "wb2a-01234567-1eec5cb5280979f3a70cf5c9762a4e7d"; got != want {
		t.Errorf("cacheKey=%q want %q", got, want)
	}
	if got, want := buildCacheKey("second-uid", "conv-1"), "wb2a-second-u-6d548920fd24be4fb6f59aa3bd35e6fd"; got != want {
		t.Errorf("cacheKey2=%q want %q", got, want)
	}
	// 空 uid 用占位段（仍注入）。
	if got := buildCacheKey("", "c"); !strings.HasPrefix(got, "wb2a--") {
		t.Errorf("空 uid 应有占位段，got %q", got)
	}
}

// TestDropReasoningContentParts 11101 防御：content 里的 reasoning_text/reasoning
// 片段剔除（Codex 经 Responses→chat 转换后透传思考片段，上游 content 白名单不含，
// 命中 400 code=11101）。移植自 workbuddy2api-panel dea68c4。
// 直接单测本函数而非 PrepareChatBody：后者的工具配对清理会把无配对 tool 结果的
// tool_calls 一并裁掉，与本用例的「tool_calls 消息保留」断言互相干扰。
func TestDropReasoningContentParts(t *testing.T) {
	msgs := []any{
		map[string]any{"role": "user", "content": "q"},
		map[string]any{"role": "assistant", "content": []any{
			map[string]any{"type": "reasoning_text", "text": "thinking..."},
			map[string]any{"type": "text", "text": "answer"},
		}},
		map[string]any{"role": "assistant", "content": []any{
			map[string]any{"type": "reasoning", "text": "thinking2"},
		}},
		map[string]any{"role": "assistant", "content": []any{
			map[string]any{"type": "reasoning_text", "text": "t3"},
		}, "tool_calls": []any{map[string]any{"id": "c1"}}},
	}
	out, changed := dropReasoningContentParts(msgs)
	if !changed {
		t.Fatal("应报告有剔除")
	}
	if len(out) != 3 {
		t.Fatalf("被清空的无 tool_calls 消息应整条删除，剩 %d 条", len(out))
	}
	second := out[1].(map[string]any)
	parts := second["content"].([]any)
	if len(parts) != 1 || parts[0].(map[string]any)["type"] != "text" {
		t.Errorf("reasoning_text 应剔除只留 text，got %v", parts)
	}
	third := out[2].(map[string]any)
	if _, has := third["tool_calls"]; !has {
		t.Error("有 tool_calls 的消息应保留")
	}
	if got := third["content"].([]any); len(got) != 0 {
		t.Errorf("tool_calls 消息的 reasoning 应剔除、content 变空数组，got %v", got)
	}

	// 无思考片段：不改动、changed=false。
	plain := []any{map[string]any{"role": "user", "content": "q"}}
	if _, changed := dropReasoningContentParts(plain); changed {
		t.Error("无思考片段不应报告变更")
	}
}
