//go:build unit

package qoder

import (
	"container/list"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// 本文件覆盖移植源同步（2026-11-03）引入的 qoder 侧改动：
// business.model_key 计费归因、会话身份稳定化、动态显示名、额度类 403、
// SSE usage 帧序/吞帧、namespace 工具展开。

func resetConversationCache() {
	conversationMu.Lock()
	defer conversationMu.Unlock()
	conversationLRU = list.New()
	conversationIndex = map[string]*list.Element{}
}

func TestConversationIdentityStablePerSession(t *testing.T) {
	resetConversationCache()
	t.Setenv("QD_CONVERSATION_SHARE", "1")
	req := map[string]any{
		"model":    "auto",
		"messages": []any{map[string]any{"role": "user", "content": "你好"}},
	}
	a := ConversationIdentity("sess-1", req)
	b := ConversationIdentity("sess-1", req)
	c := ConversationIdentity("sess-2", req)
	if a == nil || b == nil || c == nil {
		t.Fatalf("identity nil: %+v %+v %+v", a, b, c)
	}
	if a.id != b.id || a.beginAt != b.beginAt {
		t.Fatalf("same session must reuse identity: %+v vs %+v", a, b)
	}
	if a.id == c.id {
		t.Fatal("different sessions must not share identity")
	}
}

func TestConversationIdentityHistoryFallback(t *testing.T) {
	resetConversationCache()
	t.Setenv("QD_CONVERSATION_SHARE", "1")
	req := map[string]any{
		"model": "auto",
		"messages": []any{
			map[string]any{"role": "system", "content": "sys"},
			map[string]any{"role": "user", "content": "同一段首问"},
		},
	}
	a := ConversationIdentity("", req)
	b := ConversationIdentity("", req)
	if a == nil || b == nil || a.id != b.id {
		t.Fatalf("history fallback should be stable: %+v vs %+v", a, b)
	}
	other := map[string]any{
		"model":    "auto",
		"messages": []any{map[string]any{"role": "user", "content": "另一个问题"}},
	}
	if c := ConversationIdentity("", other); c != nil && c.id == a.id {
		t.Fatal("different histories must not share identity")
	}
}

func TestConversationIdentityDisabled(t *testing.T) {
	resetConversationCache()
	t.Setenv("QD_CONVERSATION_SHARE", "0")
	if got := ConversationIdentity("sess", map[string]any{"messages": []any{}}); got != nil {
		t.Fatalf("disabled should return nil, got %+v", got)
	}
}

func TestConversationIdentityTTLExpiry(t *testing.T) {
	resetConversationCache()
	t.Setenv("QD_CONVERSATION_SHARE", "1")
	base := time.Unix(1_700_000_000, 0)
	old := conversationClock
	conversationClock = func() time.Time { return base }
	defer func() { conversationClock = old }()

	req := map[string]any{"messages": []any{map[string]any{"role": "user", "content": "x"}}}
	a := ConversationIdentity("sess-ttl", req)
	conversationClock = func() time.Time { return base.Add(7 * time.Hour) }
	b := ConversationIdentity("sess-ttl", req)
	if a == nil || b == nil {
		t.Fatal("nil identity")
	}
	if a.id == b.id {
		t.Fatal("identity must expire after TTL")
	}
}

func TestQuotaCooldownClassification(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.Local)
	cases := []struct {
		detail   string
		wantKind string
	}{
		{`{"code":"112"}`, QuotaKindExhausted},
		{`{\"code\":\"112\"}`, QuotaKindExhausted},
		{`pricingUrl: https://x`, QuotaKindExhausted},
		{`{"code":"110"}`, QuotaKindDaily},
		{`Billing daily count exceeded`, QuotaKindDaily},
		{`{"code":"10605","isQueued":true}`, ""},
		{`some other 403`, ""},
	}
	for _, tc := range cases {
		kind, cd := QuotaCooldown(tc.detail, now)
		if kind != tc.wantKind {
			t.Fatalf("detail %q: kind=%q want %q", tc.detail, kind, tc.wantKind)
		}
		if kind == "" && cd != 0 {
			t.Fatalf("detail %q: non-quota must have zero cooldown", tc.detail)
		}
	}
	if _, cd := QuotaCooldown(`{"code":"112"}`, now); cd != 1800*time.Second {
		t.Fatalf("112 cooldown = %v, want 30m", cd)
	}
	if _, cd := QuotaCooldown(`{"code":"110"}`, now); cd <= 0 || cd > 6*time.Hour+time.Second {
		t.Fatalf("110 cooldown out of range: %v", cd)
	}
}

func TestLiveDisplayNameResolution(t *testing.T) {
	liveDisplayMu.Lock()
	old := liveDisplayIndex
	liveDisplayIndex = map[string]string{}
	liveDisplayMu.Unlock()
	defer func() {
		liveDisplayMu.Lock()
		liveDisplayIndex = old
		liveDisplayMu.Unlock()
	}()

	registerLiveDisplayNames(map[string]string{"Kimi-K4-Preview": "kmodel_latest"})
	if got := ResolveUpstreamKey("kimi-k4-preview", ""); got != "kmodel_latest" {
		t.Fatalf("dynamic display name resolve = %q", got)
	}
	// 离线别名兜底（官方改名窗口期）
	if got := ResolveUpstreamKey("kimi-k3.1-pro", ""); got != "kmodel_latest" {
		t.Fatalf("alias kimi-k3.1-pro resolve = %q", got)
	}
}

// usageFrame 构造 choices 为空的 usage 垫底帧。
func usageFrame(t *testing.T, usage map[string]any) []byte {
	t.Helper()
	return rawFrame(t, nil, nil, map[string]any{
		"choices": []any{},
		"usage":   usage,
	})
}

func TestRecoverFinishBeforeUsage(t *testing.T) {
	src := FramesOf(
		rawFrame(t, map[string]any{"content": "你好"}, nil, nil),
		rawFrame(t, map[string]any{}, "stop", nil),
		usageFrame(t, map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}),
	)
	frames, err := collectFrames(t, RecoverLeakedToolCalls(src, nil))
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, f := range frames {
		data := StripDataPrefix(string(f))
		if data == "" || data == "[DONE]" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(data), &m); err != nil {
			continue
		}
		if _, ok := m["usage"]; ok {
			order = append(order, "usage")
			continue
		}
		if choices, _ := m["choices"].([]any); len(choices) > 0 {
			if ch, ok := choices[0].(map[string]any); ok && truthy(ch["finish_reason"]) {
				order = append(order, "finish")
			} else {
				order = append(order, "content")
			}
		}
	}
	want := []string{"content", "finish", "usage"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Fatalf("frame order = %v, want %v", order, want)
	}
}

func TestRecoverUsageNotSwallowedDuringHold(t *testing.T) {
	leak := leak27 // 既有测试夹具：marker + JSON 回声
	src := FramesOf(
		rawFrame(t, map[string]any{"content": leak[:9]}, nil, nil),
		rawFrame(t, map[string]any{"content": leak[9:]}, nil, nil),
		rawFrame(t, map[string]any{}, "stop", nil),
		usageFrame(t, map[string]any{"prompt_tokens": 2531, "completion_tokens": 192, "total_tokens": 2723}),
	)
	frames, err := collectFrames(t, RecoverLeakedToolCalls(src, map[string]bool{"terminal": true}))
	if err != nil {
		t.Fatal(err)
	}
	sawUsage := false
	for _, f := range frames {
		if strings.Contains(string(f), `"usage"`) {
			sawUsage = true
		}
	}
	if !sawUsage {
		t.Fatalf("usage frame swallowed during hold recovery: %q", frames)
	}
}

func TestGuardTruncatedToolArgs(t *testing.T) {
	truncated := FramesOf(
		rawFrame(t, map[string]any{"tool_calls": []any{map[string]any{
			"index": 0, "id": "call_1", "type": "function",
			"function": map[string]any{"name": "terminal", "arguments": `{"cmd":"`},
		}}}, nil, nil),
	)
	if _, err := collectFrames(t, GuardTruncatedToolArgs(truncated)); err == nil {
		t.Fatal("truncated tool arguments must fail the stream closed")
	}

	complete := FramesOf(
		rawFrame(t, map[string]any{"tool_calls": []any{map[string]any{
			"index": 0, "id": "call_1", "type": "function",
			"function": map[string]any{"name": "terminal", "arguments": `{"cmd":"ls"}`},
		}}}, nil, nil),
		rawFrame(t, map[string]any{}, "tool_calls", nil),
	)
	if _, err := collectFrames(t, GuardTruncatedToolArgs(complete)); err != nil {
		t.Fatalf("complete tool call should pass, got %v", err)
	}
}

func TestCampaignListNeedsRetry(t *testing.T) {
	if !campaignListNeedsRetry(&CampaignsStatus{OK: true}, nil) {
		t.Fatal("empty campaign list should retry")
	}
	if !campaignListNeedsRetry(&CampaignsStatus{OK: true, Campaigns: []CampaignItem{
		{CampaignID: "a", ClaimStatus: "CLAIMED", BenefitKind: "CREDITS"},
	}}, []string{"CREDITS"}) {
		t.Fatal("no CLAIMABLE item should retry")
	}
	if campaignListNeedsRetry(&CampaignsStatus{OK: true, ShowCampaign: true, Campaigns: []CampaignItem{
		{CampaignID: "a", ClaimStatus: "CLAIMABLE", BenefitKind: "CREDITS"},
	}}, []string{"CREDITS"}) {
		t.Fatal("claimable item must stop retrying")
	}
	if campaignListNeedsRetry(&CampaignsStatus{OK: false}, nil) {
		t.Fatal("failed query is not retried here")
	}
}

func TestNormalizeToolsFlattensNamespace(t *testing.T) {
	tools := []any{
		map[string]any{
			"type": "namespace",
			"name": "mcp",
			"tools": []any{
				map[string]any{
					"type": "function", "name": "echo", "description": "d",
					"parameters": map[string]any{"type": "object"},
				},
				map[string]any{
					"type": "namespace", "name": "nested",
					"tools": []any{map[string]any{
						"type": "function", "name": "inner", "description": "i",
						"parameters": map[string]any{"type": "object"},
					}},
				},
			},
		},
		map[string]any{"type": "function", "name": "echo", "description": "dup"},
	}
	norm := normalizeTools(tools)
	names := make([]string, 0, len(norm))
	for _, item := range norm {
		m := item.(map[string]any)
		if fn, ok := m["function"].(map[string]any); ok {
			names = append(names, fn["name"].(string))
		}
	}
	if strings.Join(names, ",") != "echo,inner" {
		t.Fatalf("flattened names = %v", names)
	}
}

func TestBuildAgentChatBodyBusinessModelKey(t *testing.T) {
	resetConversationCache()
	t.Setenv("QD_CONVERSATION_SHARE", "1")
	sess := mustSession(t)
	req := map[string]any{
		"model":           "qwen3.8-max",
		"conversation_id": "conv-1",
		"messages":        []any{map[string]any{"role": "user", "content": "你好"}},
	}
	encoded, err := BuildAgentChatBody(req, sess, "")
	if err != nil {
		t.Fatal(err)
	}
	body := decodeBody(t, encoded)
	biz, _ := body["business"].(map[string]any)
	if biz == nil {
		t.Fatal("business missing")
	}
	if got, _ := biz["model_key"].(string); got != "qmodel_38max" {
		t.Fatalf("business.model_key = %q", got)
	}
	if got, _ := biz["version"].(string); got != COSYVersion {
		t.Fatalf("business.version = %q, want %q", got, COSYVersion)
	}
	firstRecord, _ := body["chat_record_id"].(string)
	firstBiz, _ := biz["id"].(string)

	// 同一客户端会话 → 下一请求复用同一上游会话身份
	encoded2, err := BuildAgentChatBody(req, sess, "")
	if err != nil {
		t.Fatal(err)
	}
	body2 := decodeBody(t, encoded2)
	biz2, _ := body2["business"].(map[string]any)
	if got, _ := body2["chat_record_id"].(string); got != firstRecord {
		t.Fatalf("chat_record_id not stable: %q vs %q", got, firstRecord)
	}
	if got, _ := biz2["id"].(string); got != firstBiz {
		t.Fatalf("business.id not stable: %q vs %q", got, firstBiz)
	}
	if got, _ := body2["request_id"].(string); got == "" || got == firstRecord {
		t.Fatalf("request_id must stay per-request unique, got %q", got)
	}
}
