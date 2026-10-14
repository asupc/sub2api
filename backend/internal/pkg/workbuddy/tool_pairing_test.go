//go:build unit

package workbuddy

import (
	"encoding/json"
	"strings"
	"testing"
)

// wbMsgs 解析测试用的 messages JSON 数组。
func wbMsgs(t *testing.T, s string) []any {
	t.Helper()
	var v []any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("parse messages: %v", err)
	}
	return v
}

// wbSummarize 把消息序列压成可读摘要：assistant 带 tool_calls 记 "assistant(id1,id2)"，
// tool 记 "tool(id)"，其余记 "role(-)"。用于逐条比对「清理后长什么样」。
func wbSummarize(messages []any) []string {
	out := make([]string, 0, len(messages))
	for _, m := range messages {
		msg, ok := m.(map[string]any)
		if !ok {
			out = append(out, "<non-object>")
			continue
		}
		role, _ := msg["role"].(string)
		if role == "tool" {
			id, _ := msg["tool_call_id"].(string)
			out = append(out, "tool("+id+")")
			continue
		}
		ids := []string{}
		if tcs, ok := msg["tool_calls"].([]any); ok {
			for _, tci := range tcs {
				if tc, ok := tci.(map[string]any); ok {
					id, _ := tc["id"].(string)
					ids = append(ids, id)
				}
			}
		}
		if len(ids) > 0 {
			out = append(out, role+"("+strings.Join(ids, ",")+")")
			continue
		}
		out = append(out, role+"(-)")
	}
	return out
}

func wbAssertSummary(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("消息数不符:\n got %v\nwant %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("消息[%d] = %s want %s\n got %v\nwant %v", i, got[i], want[i], got, want)
		}
	}
}

// TestCleanupOrphanToolCalls 缺一侧的配对必须两侧同口径剔除（否则残留半截配对 → 11148）。
// 移植自上游同名用例。
func TestCleanupOrphanToolCalls(t *testing.T) {
	t.Run("批内部分缺结果：调用侧对称裁剪", func(t *testing.T) {
		in := wbMsgs(t, `[
			{"role":"assistant","content":null,"tool_calls":[{"id":"c1"},{"id":"c2"}]},
			{"role":"tool","tool_call_id":"c1","content":"r1"}
		]`)
		out, changed := CleanupOrphanToolCalls(in)
		if !changed {
			t.Fatal("changed=false，期望裁剪")
		}
		wbAssertSummary(t, wbSummarize(out), []string{"assistant(c1)", "tool(c1)"})
	})

	t.Run("调用无任何结果：删 tool_calls 键", func(t *testing.T) {
		in := wbMsgs(t, `[{"role":"assistant","content":"hi","tool_calls":[{"id":"c1"}]}]`)
		out, changed := CleanupOrphanToolCalls(in)
		if !changed {
			t.Fatal("changed=false，期望删除 tool_calls")
		}
		msg := out[0].(map[string]any)
		if _, has := msg["tool_calls"]; has {
			t.Error("孤儿调用应删除整个 tool_calls 键")
		}
		if msg["content"] != "hi" {
			t.Errorf("content 被误改: %v", msg["content"])
		}
	})

	t.Run("孤儿结果整条删除", func(t *testing.T) {
		in := wbMsgs(t, `[{"role":"user","content":"x"},{"role":"tool","tool_call_id":"c9","content":"r"}]`)
		out, changed := CleanupOrphanToolCalls(in)
		if !changed {
			t.Fatal("changed=false，期望删除孤儿结果")
		}
		wbAssertSummary(t, wbSummarize(out), []string{"user(-)"})
	})

	t.Run("完整配对零改动", func(t *testing.T) {
		in := wbMsgs(t, `[
			{"role":"assistant","content":null,"tool_calls":[{"id":"c1"},{"id":"c2"}]},
			{"role":"tool","tool_call_id":"c1","content":"r1"},
			{"role":"tool","tool_call_id":"c2","content":"r2"}
		]`)
		_, changed := CleanupOrphanToolCalls(in)
		if changed {
			t.Error("changed=true，完整配对不应改动")
		}
	})

	t.Run("无工具流量零改动", func(t *testing.T) {
		if _, changed := CleanupOrphanToolCalls(wbMsgs(t, `[{"role":"user","content":"x"}]`)); changed {
			t.Error("changed=true，无工具流量不应改动")
		}
	})

	t.Run("空 id 双侧不识别为工具流量", func(t *testing.T) {
		// 已知边界：id 为空的配对无法按 id 判定，两侧都不计入工具流量 → 原样透传。
		in := wbMsgs(t, `[
			{"role":"assistant","content":null,"tool_calls":[{"id":""}]},
			{"role":"tool","tool_call_id":"","content":"r"}
		]`)
		if _, changed := CleanupOrphanToolCalls(in); changed {
			t.Error("changed=true，空 id 形态当前按原样透传处理")
		}
	})
}

// TestCleanupOrphanToolCallsEmptyName 空 function.name 的钉桩用例（线上 11133）。
// 移植自上游同名用例：上游对空 name 调用直接判 11133 model_param_invalid，且带不带
// 配对结果都会拒。删除按 id 对称裁剪，assistant 消息本身保留，配对对称不变式照常成立。
func TestCleanupOrphanToolCallsEmptyName(t *testing.T) {
	t.Run("空name带结果：调用与结果成对剔除，assistant保留", func(t *testing.T) {
		in := wbMsgs(t, `[
	{"role":"user","content":"q"},
	{"role":"assistant","content":"","tool_calls":[{"id":"c1","type":"function","function":{"name":"","arguments":"{}"}}]},
	{"role":"tool","tool_call_id":"c1","content":"r1"},
	{"role":"user","content":"next"}
]`)
		out, changed := CleanupOrphanToolCalls(in)
		if !changed {
			t.Fatal("changed=false，期望剔除空 name 调用")
		}
		wbAssertSummary(t, wbSummarize(out), []string{"user(-)", "assistant(-)", "user(-)"})
	})

	t.Run("空name且无结果：仍走孤儿路径删除", func(t *testing.T) {
		// 回归钉桩：若改成「空 name 不登记 callIDs」，hasTraffic 会被判 false 早退，
		// 该脏条目反而漏出站。必须仍删除。
		in := wbMsgs(t, `[
	{"role":"assistant","content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"","arguments":"{}"}}]},
	{"role":"user","content":"go"}
]`)
		out, changed := CleanupOrphanToolCalls(in)
		if !changed {
			t.Fatal("changed=false，期望删除空 name 孤儿调用")
		}
		msg := out[0].(map[string]any)
		if _, has := msg["tool_calls"]; has {
			t.Error("空 name 孤儿调用的 tool_calls 键应被删除")
		}
	})

	t.Run("纯空白name判脏", func(t *testing.T) {
		in := wbMsgs(t, `[
	{"role":"assistant","content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"   ","arguments":"{}"}}]},
	{"role":"tool","tool_call_id":"c1","content":"r1"}
]`)
		out, changed := CleanupOrphanToolCalls(in)
		if !changed {
			t.Fatal("changed=false，纯空白 name 应与空串同判")
		}
		wbAssertSummary(t, wbSummarize(out), []string{"assistant(-)"})
	})

	t.Run("批内混合：只剔脏项，健康项不动", func(t *testing.T) {
		in := wbMsgs(t, `[
	{"role":"assistant","content":null,"tool_calls":[
	{"id":"c1","type":"function","function":{"name":"","arguments":"{}"}},
	{"id":"c2","type":"function","function":{"name":"read","arguments":"{}"}}
	]},
	{"role":"tool","tool_call_id":"c1","content":"r1"},
	{"role":"tool","tool_call_id":"c2","content":"r2"}
]`)
		out, changed := CleanupOrphanToolCalls(in)
		if !changed {
			t.Fatal("changed=false，期望对称裁剪脏项")
		}
		wbAssertSummary(t, wbSummarize(out), []string{"assistant(c2)", "tool(c2)"})
	})

	t.Run("function或name缺失不判脏（不扩大打击面）", func(t *testing.T) {
		in := wbMsgs(t, `[
	{"role":"assistant","content":null,"tool_calls":[{"id":"c1"},{"id":"c2"}]},
	{"role":"tool","tool_call_id":"c1","content":"r1"},
	{"role":"tool","tool_call_id":"c2","content":"r2"}
]`)
		_, changed := CleanupOrphanToolCalls(in)
		if changed {
			t.Error("changed=true，缺 function 字段不应判脏")
		}
	})
}

// TestPrepareChatBodyCleansOrphanToolCalls 验证清理已接入出站管道：
// 脏 tool_call 经 PrepareChatBody 后不再出站。
func TestPrepareChatBodyCleansOrphanToolCalls(t *testing.T) {
	in := []byte(`{"model":"m","messages":[
		{"role":"assistant","content":"","tool_calls":[{"id":"c1","type":"function","function":{"name":"","arguments":"{}"}}]},
		{"role":"tool","tool_call_id":"c1","content":"r1"}
	]}`)
	obj := parseObj(t, PrepareChatBody(Credentials{}, in, ""))
	msgs, _ := obj["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("脏调用与结果应成对剔除，剩 1 条，got %d: %#v", len(msgs), msgs)
	}
	msg := msgs[0].(map[string]any)
	if _, has := msg["tool_calls"]; has {
		t.Error("空 name 的 tool_calls 应被删除")
	}
}
