package apicompat

import (
	"encoding/json"
	"testing"
)

// TestResponsesBridgeMergesSameTurnTextWithToolCalls 回归 workbuddy2api-panel
// 9ad206b / PR #281：Codex 把同一轮的 assistant 文字和 function_call 拆成多个
// item，回放时必须合并成一条 assistant（否则 Kimi 类模型在进度句后停住），且
// 文字与调用之间的第二段 reasoning 不能丢。
func TestResponsesBridgeMergesSameTurnTextWithToolCalls(t *testing.T) {
	input := json.RawMessage(`[
		{"role":"user","content":"fix tests"},
		{"type":"reasoning","summary":"inspect"},
		{"type":"message","role":"assistant","phase":"commentary","content":[{"type":"output_text","text":"running tests"}]},
		{"type":"reasoning","summary":"run both checks"},
		{"type":"function_call","call_id":"c1","name":"check","arguments":"{}"},
		{"type":"function_call","call_id":"c2","name":"check","arguments":"{}"},
		{"type":"function_call_output","call_id":"c1","output":"failed"},
		{"type":"function_call_output","call_id":"c2","output":"ok"},
		{"type":"function_call","call_id":"c3","name":"check","arguments":"{}"},
		{"type":"function_call_output","call_id":"c3","output":"fixed"},
		{"type":"message","role":"assistant","phase":"final_answer","content":"done"}
	]`)
	messages, err := responsesInputToChatMessages("", input)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 7 {
		t.Fatalf("expected 7 messages, got %d: %+v", len(messages), messages)
	}
	m := messages[1]
	if m.Role != "assistant" || chatMessageContentText(m.Content) != "running tests" {
		t.Fatalf("assistant history changed: %+v", m)
	}
	if m.ReasoningContent != "inspect\nrun both checks" {
		t.Fatalf("reasoning=%q want both segments", m.ReasoningContent)
	}
	if len(m.ToolCalls) != 2 || m.ToolCalls[0].ID != "c1" || m.ToolCalls[1].ID != "c2" {
		t.Fatalf("expected two parallel calls: %+v", m.ToolCalls)
	}
	if messages[2].Role != "tool" || messages[2].ToolCallID != "c1" ||
		messages[3].Role != "tool" || messages[3].ToolCallID != "c2" {
		t.Fatalf("tool replies changed: %+v", messages[2:4])
	}
	if messages[4].Role != "assistant" || len(messages[4].ToolCalls) != 1 || messages[4].ToolCalls[0].ID != "c3" {
		t.Fatalf("later assistant turn changed: %+v", messages[4])
	}
}

// TestResponsesBridgeDoesNotMergeAcrossTurnBoundaries final_answer 与 compaction
// 是明确轮次边界：其后的 function_call 不得并进之前的 assistant 文字。
func TestResponsesBridgeDoesNotMergeAcrossTurnBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name   string
		prefix string
	}{
		{"final answer", `{"type":"message","role":"assistant","phase":"final_answer","content":"done"}`},
		{"opaque compaction", `{"role":"assistant","content":"done"},{"type":"compaction","encrypted_content":"AAAA"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := json.RawMessage(`[` + tc.prefix + `,
				{"type":"function_call","call_id":"c1","name":"check","arguments":"{}"},
				{"type":"function_call_output","call_id":"c1","output":"ok"}]`)
			messages, err := responsesInputToChatMessages("", input)
			if err != nil {
				t.Fatal(err)
			}
			if len(messages) != 3 {
				t.Fatalf("messages=%+v", messages)
			}
			if messages[0].Role != "assistant" || len(messages[0].ToolCalls) != 0 {
				t.Fatalf("merged calls across %s: %+v", tc.name, messages)
			}
			if messages[1].Role != "assistant" || len(messages[1].ToolCalls) != 1 ||
				messages[1].ToolCalls[0].ID != "c1" {
				t.Fatalf("tool call must start a new assistant turn: %+v", messages)
			}
			if messages[2].Role != "tool" || messages[2].ToolCallID != "c1" {
				t.Fatalf("tool reply: %+v", messages)
			}
		})
	}
}

// TestResponsesBridgeReasoningMergeDeduplicates 没有新 reasoning 时沿用上一轮
// 思考，但不能把同一段内容重复追加。
func TestResponsesBridgeReasoningMergeDeduplicates(t *testing.T) {
	input := json.RawMessage(`[
		{"type":"reasoning","summary":"inspect"},
		{"type":"message","role":"assistant","content":"looking"},
		{"type":"function_call","call_id":"c1","name":"check","arguments":"{}"}
	]`)
	messages, err := responsesInputToChatMessages("", input)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 {
		t.Fatalf("messages=%+v", messages)
	}
	if messages[0].ReasoningContent != "inspect" {
		t.Fatalf("reasoning=%q", messages[0].ReasoningContent)
	}
}
