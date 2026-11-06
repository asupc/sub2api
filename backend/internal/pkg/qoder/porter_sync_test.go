//go:build unit

package qoder

import (
	"container/list"
	"encoding/json"
	"fmt"
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

func TestConversationIdentitySplitsPerTurn(t *testing.T) {
	resetConversationCache()
	t.Setenv("QD_CONVERSATION_SHARE", "1")
	first := map[string]any{
		"model": "auto",
		"messages": []any{
			map[string]any{"role": "user", "content": "第一问"},
		},
	}
	toolStep := map[string]any{
		"model": "auto",
		"messages": []any{
			map[string]any{"role": "user", "content": "第一问"},
			map[string]any{"role": "assistant", "content": "", "tool_calls": []any{}},
			map[string]any{"role": "tool", "content": "结果"},
		},
	}
	second := map[string]any{
		"model": "auto",
		"messages": []any{
			map[string]any{"role": "user", "content": "第一问"},
			map[string]any{"role": "assistant", "content": "答"},
			map[string]any{"role": "user", "content": "第二问"},
		},
	}
	a := ConversationIdentity("sess-turn", first)
	b := ConversationIdentity("sess-turn", toolStep) // 同一提问内的工具步进
	c := ConversationIdentity("sess-turn", second)   // 下一次用户提问
	if a == nil || b == nil || c == nil {
		t.Fatal("nil identity")
	}
	if a.id != b.id {
		t.Fatal("tool steps within one question must share identity")
	}
	if a.id == c.id {
		t.Fatal("a new user question must start a new conversation identity")
	}
}

func TestConversationIdentityDisabledVariants(t *testing.T) {
	resetConversationCache()
	for _, v := range []string{"0", "false", "False", "no"} {
		t.Setenv("QD_CONVERSATION_SHARE", v)
		if got := ConversationIdentity("sess", map[string]any{}); got != nil {
			t.Fatalf("QD_CONVERSATION_SHARE=%q should disable sharing", v)
		}
	}
}

func TestConversationTTLEnvOverride(t *testing.T) {
	if got := conversationTTL(); got != conversationTTLDefault {
		t.Fatalf("default TTL = %d", got)
	}
	t.Setenv("QD_CONVERSATION_TTL", "120")
	if got := conversationTTL(); got != 120 {
		t.Fatalf("env TTL = %d, want 120", got)
	}
	t.Setenv("QD_CONVERSATION_TTL", "bogus")
	if got := conversationTTL(); got != conversationTTLDefault {
		t.Fatalf("invalid env TTL should fall back, got %d", got)
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
	// 顺序必须是「恢复块 … FINISH(tool_calls) → USAGE」：usage 不能被吞，
	// 也不能被补发到收尾帧之前。
	var order []string
	for _, f := range frames {
		data := StripDataPrefix(string(f))
		if data == "" || data == "[DONE]" {
			continue
		}
		var m map[string]any
		if json.Unmarshal([]byte(data), &m) != nil {
			continue
		}
		if _, ok := m["usage"]; ok {
			order = append(order, "usage")
			continue
		}
		if chs, _ := m["choices"].([]any); len(chs) > 0 {
			ch, _ := chs[0].(map[string]any)
			if truthy(ch["finish_reason"]) {
				order = append(order, "finish")
			} else if delta, _ := ch["delta"].(map[string]any); truthy(delta["tool_calls"]) {
				order = append(order, "tool_call")
			}
		}
	}
	if len(order) < 2 || order[len(order)-1] != "usage" || order[len(order)-2] != "finish" {
		t.Fatalf("recovery frame order = %v, want ... finish,usage", order)
	}
}

func TestGuardTruncatedToolArgs(t *testing.T) {
	truncated := FramesOf(
		rawFrame(t, map[string]any{"tool_calls": []any{map[string]any{
			"index": 0, "id": "call_1", "type": "function",
			"function": map[string]any{"name": "terminal", "arguments": `{"cmd":"`},
		}}}, nil, nil),
	)
	if _, err := collectFrames(t, GuardTruncatedToolArgs(truncated, false)); err == nil {
		t.Fatal("truncated tool arguments must fail the stream closed")
	}
	// lenient（hub #135）：同款残片流按正常完成收尾，不报错。
	if _, err := collectFrames(t, GuardTruncatedToolArgs(truncated, true)); err != nil {
		t.Fatalf("lenient mode must finish normally, got %v", err)
	}

	complete := FramesOf(
		rawFrame(t, map[string]any{"tool_calls": []any{map[string]any{
			"index": 0, "id": "call_1", "type": "function",
			"function": map[string]any{"name": "terminal", "arguments": `{"cmd":"ls"}`},
		}}}, nil, nil),
		rawFrame(t, map[string]any{}, "tool_calls", nil),
	)
	if _, err := collectFrames(t, GuardTruncatedToolArgs(complete, false)); err != nil {
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

func TestConversationIdentityTTLBoundary(t *testing.T) {
	resetConversationCache()
	t.Setenv("QD_CONVERSATION_SHARE", "1")
	base := time.Unix(1_700_000_000, 0)
	old := conversationClock
	defer func() { conversationClock = old }()
	conversationClock = func() time.Time { return base }

	req := map[string]any{"messages": []any{map[string]any{"role": "user", "content": "边界"}}}
	a := ConversationIdentity("sess-boundary", req)
	// 恰好 6h 空闲：仍有效（源 TTL 判据 <=，且命中会刷新 at——滑动过期）
	conversationClock = func() time.Time { return base.Add(6 * time.Hour) }
	b := ConversationIdentity("sess-boundary", req)
	if b == nil || b.id != a.id {
		t.Fatal("identity must still be valid at exactly TTL")
	}
	// 自上次使用起超过 TTL：过期换新
	conversationClock = func() time.Time { return base.Add(12*time.Hour + time.Second) }
	if c := ConversationIdentity("sess-boundary", req); c == nil || c.id == a.id {
		t.Fatal("identity must expire after TTL of inactivity")
	}
}

func TestQuotaCooldownDailyLowerBound(t *testing.T) {
	// 临近日界：冷却不得短于 60s（源 max(60, ...)）。
	now := time.Date(2026, 11, 3, 23, 59, 30, 0, time.Local)
	kind, cd := QuotaCooldown(`{"code":"110"}`, now)
	if kind != QuotaKindDaily {
		t.Fatalf("kind = %q", kind)
	}
	if cd < time.Minute {
		t.Fatalf("110 cooldown near midnight = %v, want >= 1m", cd)
	}
}

func TestQuotaCooldownIgnoresEmptyUsage(t *testing.T) {
	// 空 usage 字典同样算 usage 垫底帧：不得破坏 [finish] -> [usage]。
	src := FramesOf(
		rawFrame(t, map[string]any{}, "stop", nil),
		usageFrame(t, map[string]any{}),
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
		if json.Unmarshal([]byte(data), &m) != nil {
			continue
		}
		if _, ok := m["usage"]; ok {
			order = append(order, "usage")
			continue
		}
		if chs, _ := m["choices"].([]any); len(chs) > 0 {
			ch, _ := chs[0].(map[string]any)
			if truthy(ch["finish_reason"]) {
				order = append(order, "finish")
			}
		}
	}
	if strings.Join(order, ",") != "finish,usage" {
		t.Fatalf("empty-usage frame order = %v, want [finish usage]", order)
	}
}

func TestRecoverMarkerOnlyOverflowFailsClosed(t *testing.T) {
	frames := [][]byte{rawFrame(t, map[string]any{"content": LeakMarker}, nil, nil)}
	for i := 0; i <= holdMaxFrames+2; i++ {
		frames = append(frames, rawFrame(t, map[string]any{}, nil, nil))
	}
	_, err := collectFrames(t, RecoverLeakedToolCalls(FramesOf(frames...), map[string]bool{"terminal": true}))
	if err == nil {
		t.Fatal("bare marker overflow must fail the stream closed")
	}
	if !strings.Contains(err.Error(), "marker") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRecoverSameFrameToolCallsCarried(t *testing.T) {
	// 证伪路径把暂存补发为正文时，必须搬运同帧的 tool_calls 增量（#124）。
	tc := []any{map[string]any{
		"index": 0, "id": "call_1", "type": "function",
		"function": map[string]any{"name": "terminal", "arguments": `{"a"`},
	}}
	src := FramesOf(
		rawFrame(t, map[string]any{"content": LeakMarker}, nil, nil),
		rawFrame(t, map[string]any{"content": "hello", "tool_calls": tc}, nil, nil),
	)
	frames, err := collectFrames(t, RecoverLeakedToolCalls(src, map[string]bool{"terminal": true}))
	if err != nil {
		t.Fatal(err)
	}
	carried := false
	for _, f := range frames {
		data := StripDataPrefix(string(f))
		var m map[string]any
		if json.Unmarshal([]byte(data), &m) != nil {
			continue
		}
		chs, _ := m["choices"].([]any)
		if len(chs) == 0 {
			continue
		}
		ch, _ := chs[0].(map[string]any)
		delta, _ := ch["delta"].(map[string]any)
		content, _ := delta["content"].(string)
		if !strings.HasSuffix(content, "hello") {
			continue
		}
		if tcl, _ := delta["tool_calls"].([]any); len(tcl) > 0 {
			carried = true
		}
	}
	if !carried {
		t.Fatalf("same-frame tool_calls not carried into synthesized content frame: %q", frames)
	}
}

func TestGuardNamelessToolCallsBuffersUntilName(t *testing.T) {
	src := FramesOf(
		rawFrame(t, map[string]any{"tool_calls": []any{map[string]any{
			"index": 0, "id": "call_9", "type": "function",
			"function": map[string]any{"arguments": `{"a":`},
		}}}, nil, nil),
		rawFrame(t, map[string]any{"tool_calls": []any{map[string]any{
			"index":    0,
			"function": map[string]any{"name": "terminal", "arguments": `1}`},
		}}}, nil, nil),
	)
	frames, err := collectFrames(t, GuardNamelessToolCalls(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 1 {
		t.Fatalf("expected single merged tool_call frame, got %d: %q", len(frames), frames)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(StripDataPrefix(string(frames[0]))), &m); err != nil {
		t.Fatal(err)
	}
	chs, _ := m["choices"].([]any)
	ch, _ := chs[0].(map[string]any)
	delta, _ := ch["delta"].(map[string]any)
	tcs, _ := delta["tool_calls"].([]any)
	tc, _ := tcs[0].(map[string]any)
	fn, _ := tc["function"].(map[string]any)
	if tc["id"] != "call_9" || fn["name"] != "terminal" || fn["arguments"] != `{"a":1}` {
		t.Fatalf("merged frame missing id/name/args: %v", tc)
	}
}

func TestGuardNamelessToolCallsDowngradesUnnamed(t *testing.T) {
	src := FramesOf(
		rawFrame(t, map[string]any{"tool_calls": []any{map[string]any{
			"index": 0, "function": map[string]any{"arguments": `{"a":1}`},
		}}}, nil, nil),
		rawFrame(t, map[string]any{}, "tool_calls", nil),
	)
	frames, err := collectFrames(t, GuardNamelessToolCalls(src))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range frames {
		if strings.Contains(string(f), `"tool_calls"`) {
			t.Fatalf("unnamed tool call must be dropped: %s", f)
		}
	}
	if len(frames) != 1 || !strings.Contains(string(frames[0]), `"finish_reason":"stop"`) {
		t.Fatalf("finish_reason must downgrade to stop: %q", frames)
	}
}

func TestNewChatStreamWiresTruncatedGuard(t *testing.T) {
	var b strings.Builder
	inner := map[string]any{
		"id": "c1", "model": "m", "created": 1,
		"choices": []any{map[string]any{
			"index":         0,
			"finish_reason": nil,
			"delta": map[string]any{
				"tool_calls": []any{map[string]any{
					"index": 0, "id": "call_1", "type": "function",
					"function": map[string]any{"name": "terminal", "arguments": `{"cmd":"`},
				}},
			},
		}},
	}
	b.Write(envFrame(t, inner))
	it := NewChatStream(strings.NewReader(b.String()), ChatStreamOptions{
		AllowedToolNames: map[string]bool{"terminal": true},
	})
	if _, err := collectFrames(t, it); err == nil {
		t.Fatal("NewChatStream must apply the truncated-args guard")
	}
	// ToolEchoLenient：同一输入按正常完成收尾（hub #135），且末帧仍为 [DONE]。
	stream := NewChatStream(strings.NewReader(b.String()), ChatStreamOptions{
		AllowedToolNames: map[string]bool{"terminal": true},
		ToolEchoLenient:  true,
	})
	frames, err := collectFrames(t, stream)
	if err != nil {
		t.Fatalf("lenient stream must finish normally, got %v", err)
	}
	if len(frames) == 0 || !strings.Contains(string(frames[len(frames)-1]), "[DONE]") {
		t.Fatalf("lenient stream must end with [DONE], got %v", frames)
	}
}

func TestNormalizeToolsDepthLimit(t *testing.T) {
	// 4 层 namespace 嵌套可展开到叶子；第 5 层容器（depth=4）被丢弃。
	build := func(namespaces int) map[string]any {
		var node map[string]any = map[string]any{
			"type": "function", "name": "leaf", "description": "d",
			"parameters": map[string]any{"type": "object"},
		}
		for i := 1; i <= namespaces; i++ {
			node = map[string]any{
				"type": "namespace", "name": fmt.Sprintf("ns%d", i),
				"tools": []any{node},
			}
		}
		return node
	}
	names := func(tools []any) []string {
		norm := normalizeTools(tools)
		out := make([]string, 0, len(norm))
		for _, item := range norm {
			m, _ := item.(map[string]any)
			if fn, ok := m["function"].(map[string]any); ok {
				out = append(out, fn["name"].(string))
			}
		}
		return out
	}
	if got := names([]any{build(namespaceMaxDepth)}); strings.Join(got, ",") != "leaf" {
		t.Fatalf("4-level nesting should keep leaf, got %v", got)
	}
	if got := names([]any{build(namespaceMaxDepth + 1)}); len(got) != 0 {
		t.Fatalf("5-level nesting must be dropped, got %v", got)
	}
}

func TestNormalizeToolsNamespaceWithoutType(t *testing.T) {
	// 省略 type、只给 name + children 的容器也要展开（源 _is_namespace_tool）。
	tools := []any{map[string]any{
		"name": "mcp",
		"children": []any{map[string]any{
			"type": "function", "name": "child_tool", "description": "d",
			"parameters": map[string]any{"type": "object"},
		}},
	}}
	norm := normalizeTools(tools)
	if len(norm) != 1 {
		t.Fatalf("norm len = %d: %+v", len(norm), norm)
	}
	fn, _ := norm[0].(map[string]any)["function"].(map[string]any)
	if fn["name"] != "child_tool" {
		t.Fatalf("child tool not flattened: %+v", norm[0])
	}
}

func TestToolNamesIncludeNamespaceChildren(t *testing.T) {
	payload := map[string]any{"tools": []any{
		map[string]any{"type": "function", "name": "plain"},
		map[string]any{"type": "namespace", "name": "mcp", "tools": []any{
			map[string]any{"type": "function", "name": "child_tool"},
			map[string]any{"type": "namespace", "name": "inner", "tools": []any{
				map[string]any{"type": "function", "name": "grandchild"},
			}},
		}},
	}}
	names := ToolNamesFromPayload(payload)
	for _, want := range []string{"plain", "child_tool", "grandchild"} {
		if !names[want] {
			t.Fatalf("tool %q missing from %v", want, names)
		}
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

func TestBuildAgentChatBodyHeaderSessionKeyPriority(t *testing.T) {
	resetConversationCache()
	t.Setenv("QD_CONVERSATION_SHARE", "1")
	sess := mustSession(t)
	mk := func(bodyConv string) map[string]any {
		return map[string]any{
			"model":           "qwen3.8-max",
			"conversation_id": bodyConv,
			"messages":        []any{map[string]any{"role": "user", "content": "同一问"}},
		}
	}
	build := func(req map[string]any, headerKey string) string {
		encoded, err := BuildAgentChatBody(req, sess, "", headerKey)
		if err != nil {
			t.Fatal(err)
		}
		body := decodeBody(t, encoded)
		id, _ := body["chat_record_id"].(string)
		return id
	}
	a := build(mk("body-a"), "hdr-1")
	b := build(mk("body-b"), "hdr-1") // 头优先：body 里的会话 id 被忽略
	c := build(mk("body-a"), "hdr-2")
	if a != b {
		t.Fatalf("header session key must take priority: %q vs %q", a, b)
	}
	if a == c {
		t.Fatal("different header session keys must not share identity")
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

// hub #136：窗口级会话键——session_id 的兜底口径。窗口键只用「模型 + 首条
// user」，不吃 system（system 常带 cwd/git 状态/时间等每轮变化的运行时上下文，
// 吃进去会让窗口键逐轮变 → session_id 逐轮换 → 上游 prompt 缓存归零）。
func TestConversationWindowKeyIgnoresSystemDrift(t *testing.T) {
	mk := func(system string) map[string]any {
		return map[string]any{
			"model": "kmodel",
			"messages": []any{
				map[string]any{"role": "system", "content": system},
				map[string]any{"role": "user", "content": "first question"},
				map[string]any{"role": "assistant", "content": "answer"},
				map[string]any{"role": "user", "content": "second question"},
			},
		}
	}
	a := conversationWindowKey("", mk("cwd=/repo/x time=10:00"))
	b := conversationWindowKey("", mk("cwd=/repo/y time=11:30")) // system 每轮变化
	if a == "" || a != b {
		t.Fatalf("window key must ignore per-turn system drift: %q vs %q", a, b)
	}
	if !strings.HasPrefix(a, "win:") {
		t.Fatalf("window key form = %q, want win: prefix", a)
	}
	// 首条 user 变（新对话）→ 窗口键变。
	c := conversationWindowKey("", map[string]any{
		"model":    "kmodel",
		"messages": []any{map[string]any{"role": "user", "content": "another conversation"}},
	})
	if c == a || c == "" {
		t.Fatalf("different first user must change window key: %q", c)
	}
	// 模型不同 → 窗口键不同（缓存按模型隔离）。
	d := conversationWindowKey("", map[string]any{
		"model":    "kmodel2",
		"messages": []any{map[string]any{"role": "user", "content": "first question"}},
	})
	if d == a || d == "" {
		t.Fatalf("different model must change window key: %q", d)
	}
	// 显式键优先，原样返回（不追加提问摘要）。
	if got := conversationWindowKey("client-sess-1", mk("s")); got != "client-sess-1" {
		t.Fatalf("explicit key = %q, want client-sess-1", got)
	}
	// 无 user 消息 → 空串（调用方回落提问级 id）。
	if got := conversationWindowKey("", map[string]any{"messages": []any{
		map[string]any{"role": "system", "content": "only system"}}}); got != "" {
		t.Fatalf("no-user window key = %q, want empty", got)
	}
	// 空 content 的 user 不作锚，取第一条有内容的 user。
	if got := conversationWindowKey("", map[string]any{
		"model": "kmodel",
		"messages": []any{
			map[string]any{"role": "user", "content": ""},
			map[string]any{"role": "user", "content": "real anchor"},
		}}); got == "" || got != conversationWindowKey("", map[string]any{
		"model":    "kmodel",
		"messages": []any{map[string]any{"role": "user", "content": "real anchor"}},
	}) {
		t.Fatalf("empty-content user must not be the anchor: %q", got)
	}
}

// hub #136：同一对话内两轮提问——chat_record_id（用量归组）换新，session_id
// 兜底（窗口级）恒定。session_id 的稳定性由调用方 BuildAgentChatBody 透传
// conv.windowID 实现，这里直接验证 ConversationIdentity 返回两者的口径。
func TestConversationIdentityWindowStableAcrossTurns(t *testing.T) {
	req := func(system, lastUser string) map[string]any {
		return map[string]any{
			"model": "kmodel",
			"messages": []any{
				map[string]any{"role": "system", "content": system},
				map[string]any{"role": "user", "content": "first question"},
				map[string]any{"role": "assistant", "content": "answer"},
				map[string]any{"role": "user", "content": lastUser},
			},
		}
	}
	a := ConversationIdentity("", req("ctx t1", "turn one"))
	b := ConversationIdentity("", req("ctx t2", "turn two")) // 下一轮提问
	if a == nil || b == nil {
		t.Fatal("conversation identity missing")
	}
	if a.id == b.id {
		t.Fatal("per-turn id must change across user turns")
	}
	if a.windowID == "" || a.windowID != b.windowID {
		t.Fatalf("window id must be stable across turns: %q vs %q", a.windowID, b.windowID)
	}
	if a.windowID == a.id {
		t.Fatal("window id must be decoupled from per-turn id")
	}
	// 显式键路径同样返回窗口级 id（显式键原样）。
	c := ConversationIdentity("client-sess-9", req("ctx t1", "turn one"))
	if c == nil || c.windowID != "client-sess-9" {
		t.Fatalf("explicit window id = %v, want client-sess-9", c)
	}
}
