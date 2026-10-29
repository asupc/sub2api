//go:build unit

package workbuddy

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func cnCred() Credentials {
	return Credentials{AccessToken: "at", UID: "uid-1", EnterpriseID: "ent-1", Domain: "copilot.tencent.com"}
}

func globalCred() Credentials {
	return Credentials{AccessToken: "at", UID: "uid-1", Realm: "global"}
}

// TestDefaultWorkBuddyUA UA 三段式两态（CN / global 平台段切换）。
func TestDefaultWorkBuddyUA(t *testing.T) {
	if got, want := defaultWorkBuddyUA(cnCred()), "WorkBuddy/5.8.3 WorkBuddy/5.8.3 CLI/2.161.0"; got != want {
		t.Errorf("CN UA=%q want %q", got, want)
	}
	if got, want := defaultWorkBuddyUA(globalCred()), "WorkBuddy/5.8.4 workbuddy-ai/5.8.4 CLI/2.161.0"; got != want {
		t.Errorf("global UA=%q want %q", got, want)
	}
	// 凭据版本覆盖。
	c := cnCred()
	c.ClientVersion, c.CLIVersion = "6.0.0", "3.1.4"
	if got, want := defaultWorkBuddyUA(c), "WorkBuddy/6.0.0 WorkBuddy/6.0.0 CLI/3.1.4"; got != want {
		t.Errorf("override UA=%q want %q", got, want)
	}
	// 显式整串覆盖优先。
	c.UserAgent = "MyAgent/1.0"
	if got := userAgent(c); got != "MyAgent/1.0" {
		t.Errorf("UserAgent override=%q", got)
	}
}

// TestBillingUA billing 域单段 UA。
func TestBillingUA(t *testing.T) {
	if got, want := billingUA(cnCred()), "WorkBuddy/5.8.3"; got != want {
		t.Errorf("billingUA=%q want %q", got, want)
	}
}

// TestDeriveAccountStableID 黄金值断言（sha256("wb2a:"+purpose+":"+uid) 截 36 hex）。
func TestDeriveAccountStableID(t *testing.T) {
	const uid = "0123456789abcdef"
	cases := []struct{ purpose, want string }{
		{"machine", "afc35c912cde5031c3454e08a547ba6dde76"},
		{"session", "99d13d2f77bd8a418c817ccbdc0d6b205ac4"},
	}
	for _, c := range cases {
		if got := deriveAccountStableID(uid, c.purpose); got != c.want {
			t.Errorf("deriveAccountStableID(%q,%q)=%q want %q", uid, c.purpose, got, c.want)
		}
	}
	// 幂等 + 账号互异 + 长度恒 36。
	if deriveAccountStableID("second-uid", "machine") != "0b60e5442f4606e4f321db96f9c7bc854405" {
		t.Error("second uid 黄金值不匹配")
	}
	for _, uid := range []string{"a", "b", ""} {
		if got := deriveAccountStableID(uid, "machine"); len(got) != 36 {
			t.Errorf("len(%q)=%d want 36", got, len(got))
		}
	}
}

// TestInjectConversationHeaders 会话头族完整性。
func TestInjectConversationHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	meta := ChatMeta{
		ConversationID:        "conv-in",
		ConversationRequestID: "0123456789abcdef0123456789abcdef",
		TraceID:               "trace-in",
	}
	injectConversationHeaders(req, meta, false)
	want := map[string]string{
		"X-Conversation-ID":         "conv-in",
		"X-Conversation-Request-ID": meta.ConversationRequestID,
		"X-Root-Request-ID":         meta.ConversationRequestID,
		"X-Trace-ID":                "trace-in",
		"X-B3-Sampled":              "1",
	}
	for k, v := range want {
		if got := req.Header.Get(k); got != v {
			t.Errorf("%s=%q want %q", k, got, v)
		}
	}
	msgID := req.Header.Get("X-Conversation-Message-ID")
	if msgID == "" || len(msgID) != 32 || !validTraceID(msgID) {
		t.Errorf("messageID=%q want 32 hex", msgID)
	}
	if req.Header.Get("X-Request-ID") != msgID {
		t.Error("X-Request-ID 应等于 X-Conversation-Message-ID")
	}
	if got := req.Header.Get("X-B3-TraceId"); got != meta.ConversationRequestID {
		t.Errorf("合法 convReqID 应直接作 B3 TraceId，got %q", got)
	}
	if got := req.Header.Get("X-B3-SpanId"); got != msgID[:16] {
		t.Errorf("B3 SpanId=%q want messageID 前 16 hex", got)
	}

	// 非法 convReqID（非 hex）→ B3 TraceId 回落 messageID（聚合主键本身仍原样透传）。
	req2 := httptest.NewRequest(http.MethodPost, "/", nil)
	injectConversationHeaders(req2, ChatMeta{ConversationRequestID: "not-hex-id!!"}, false)
	if got := req2.Header.Get("X-B3-TraceId"); got != req2.Header.Get("X-Conversation-Message-ID") {
		t.Errorf("非法 convReqID 的 B3 TraceId 应回落 messageID，got %q", got)
	}
	if got := req2.Header.Get("X-Conversation-Request-ID"); got != "not-hex-id!!" {
		t.Errorf("非空 convReqID 应原样作聚合主键，got %q", got)
	}
	// 零值 meta：本级补发 32 hex 聚合主键与会话 ID，保证三元组恒发
	//（缺 X-Conversation-ID 会被国际端点 400/11-128 conversation id missing）。
	req3 := httptest.NewRequest(http.MethodPost, "/", nil)
	injectConversationHeaders(req3, ChatMeta{}, false)
	if got := req3.Header.Get("X-Conversation-Request-ID"); got == "" || !validTraceID(got) {
		t.Errorf("零值 meta 应补发 32 hex 聚合主键，got %q", got)
	}
	if got := req3.Header.Get("X-Conversation-ID"); got == "" || !validTraceID(got) {
		t.Errorf("空 ConversationID 应补发 32 hex 会话 ID（恒发），got %q", got)
	}
}

// TestChatHeaders chat 专属头（X-No-* 约定 / global 覆写 / 归因头 / 红线）。
func TestChatHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	ChatHeaders(req, cnCred(), ChatMeta{})
	for k, v := range map[string]string{
		"Authorization":       "Bearer at",
		"X-User-Id":           "uid-1",
		"X-Enterprise-Id":     "ent-1",
		"X-Domain":            "copilot.tencent.com",
		"Accept":              "application/json, text/event-stream",
		"Origin":              originRefererCN,
		"Referer":             originRefererCN + "/",
		"X-CodeBuddy-Request": "1",
		"X-Agent-Purpose":     "conversation",
		"X-IDE-Name":          "WorkBuddy",
		"X-IDE-Version":       "5.8.3",
		"X-Machine-ID":        deriveAccountStableID("uid-1", "machine"),
		"X-Session-ID":        deriveAccountStableID("uid-1", "session"),
		"Accept-Language":     "zh-CN",
	} {
		if got := req.Header.Get(k); got != v {
			t.Errorf("CN chat header %s=%q want %q", k, got, v)
		}
	}
	if _, ok := req.Header["X-Refresh-Token"]; ok {
		t.Error("红线：chat 请求不得携带 X-Refresh-Token")
	}

	// 缺省字段的 X-No-* 约定。
	req2 := httptest.NewRequest(http.MethodPost, "/", nil)
	ChatHeaders(req2, Credentials{AccessToken: "at"}, ChatMeta{})
	if req2.Header.Get("X-No-User-Id") != "1" || req2.Header.Get("X-No-Enterprise-Id") != "1" ||
		req2.Header.Get("X-No-Department-Info") != "1" {
		t.Error("缺省 uid/企业/域应发 X-No-* 头")
	}

	// global 账号：企业/域头统一覆写为国际形态。
	req3 := httptest.NewRequest(http.MethodPost, "/", nil)
	ChatHeaders(req3, Credentials{AccessToken: "at", UID: "u", EnterpriseID: "ent", Domain: "x", Realm: "global"}, ChatMeta{})
	if req3.Header.Get("X-No-Enterprise-Id") != "1" || req3.Header.Get("X-Domain") != "www.workbuddy.ai" {
		t.Error("global chat 应覆写 X-No-Enterprise-Id=1 + X-Domain=www.workbuddy.ai")
	}
	if req3.Header.Get("Origin") != originRefererGlobal || req3.Header.Get("Accept-Language") != "en-US" {
		t.Error("global Origin/Accept-Language 应切国际域")
	}
}

// TestBillingAndRefreshHeaders billing / refresh 域头族。
func TestBillingAndRefreshHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	BillingHeaders(req, cnCred())
	for k, v := range map[string]string{
		"Authorization":       "Bearer at",
		"X-User-Id":           "uid-1",
		"X-Enterprise-Id":     "ent-1",
		"X-Tenant-Id":         "ent-1",
		"X-Domain":            "copilot.tencent.com",
		"User-Agent":          "WorkBuddy/5.8.3",
		"X-CodeBuddy-Request": "1",
	} {
		if got := req.Header.Get(k); got != v {
			t.Errorf("billing header %s=%q want %q", k, got, v)
		}
	}

	req2 := httptest.NewRequest(http.MethodPost, "/", nil)
	c := cnCred()
	c.RefreshToken = "rt-xyz"
	RefreshHeaders(req2, c)
	if req2.Header.Get("X-Refresh-Token") != "rt-xyz" {
		t.Error("X-Refresh-Token 缺失")
	}
	if req2.Header.Get("X-Auth-Refresh-Source") != "plugin" {
		t.Error("X-Auth-Refresh-Source 应为 plugin")
	}
	if req2.Header.Get("User-Agent") != "WorkBuddy/5.8.3 WorkBuddy/5.8.3 CLI/2.161.0" {
		t.Errorf("refresh 应走三段式 UA，got %q", req2.Header.Get("User-Agent"))
	}
}
