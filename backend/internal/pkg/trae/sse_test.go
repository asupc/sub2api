//go:build unit

package trae

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParserHandlesChunkSplitAcrossEventBoundaries(t *testing.T) {
	parser := &SoloParser{}
	first := parser.Feed([]byte("id:1\nevent:output\ndata:{\"response\":\"he"))
	if len(first) != 0 {
		t.Fatal("半截事件不该产出")
	}
	second := parser.Feed([]byte("llo\"}\n\n"))
	if len(second) != 1 {
		t.Fatalf("got %d events", len(second))
	}
	if second[0].Event != "output" || second[0].Response != "hello" {
		t.Errorf("event = %+v", second[0])
	}
}

func TestParserKeepsMultibyteIntactAcrossChunks(t *testing.T) {
	parser := &SoloParser{}
	payload := `{"response":"你好"}`
	// 在「你」的 UTF-8 中间切开（网络分片不顾字符边界）。
	split := strings.Index(payload, "你") + 1
	head := append([]byte("event:output\ndata:"), payload[:split]...)
	if events := parser.Feed(head); len(events) != 0 {
		t.Fatal("半截事件不该产出")
	}
	tail := append([]byte(payload[split:]), []byte("\n\n")...)
	events := parser.Feed(tail)
	if len(events) != 1 || events[0].Response != "你好" {
		t.Fatalf("多字节被截断: %+v", events)
	}
}

func TestParserFinishFlushesUnterminatedEvent(t *testing.T) {
	parser := &SoloParser{}
	parser.Feed([]byte("event:output\ndata:{\"response\":\"tail\"}"))
	flushed := parser.Finish()
	if len(flushed) != 1 || flushed[0].Response != "tail" {
		t.Fatalf("flushed = %+v", flushed)
	}
}

func TestParseSoloEventReadsEveryDocumentedEvent(t *testing.T) {
	output := ParseSoloEvent("output", `{"response":"a","reasoning_content":"r","tool_calls":[{"function_call":{"name":"f"}}]}`)
	if output.Response != "a" || output.Reasoning != "r" || len(output.ToolCalls) == 0 {
		t.Errorf("output = %+v", output)
	}

	usage := ParseSoloEvent("token_usage", `{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12,"reasoning_tokens":1}`)
	want := TokenUsage{Prompt: 10, Completion: 2, Total: 12, Reasoning: 1}
	if usage.Usage == nil || *usage.Usage != want {
		t.Errorf("usage = %+v, want %+v", usage.Usage, want)
	}

	done := ParseSoloEvent("done", `{"finish_reason":"length"}`)
	if done.FinishReason != "length" {
		t.Errorf("finish_reason = %s", done.FinishReason)
	}

	turn := ParseSoloEvent("turn_completion", "{}")
	if turn.FinishReason != "stop" {
		t.Error("turn_completion 应为 stop")
	}

	errEvent := ParseSoloEvent("error", `{"code":1005,"message":"plan limit"}`)
	if errEvent.ErrorCode == nil || *errEvent.ErrorCode != 1005 || errEvent.ErrorMessage != "plan limit" {
		t.Errorf("error = %+v", errEvent)
	}
}

func TestParseSoloEventToleratesMalformedPayloads(t *testing.T) {
	if ParseSoloEvent("output", "not json").Response != "" {
		t.Error("非 JSON data 应保持零值")
	}
	if ParseSoloEvent("output", "[1,2]").Response != "" {
		t.Error("非对象 data 应保持零值")
	}
	// thought 事件的正文在 thought 字段。
	if ParseSoloEvent("thought", `{"thought":"t"}`).Response != "t" {
		t.Error("thought 正文读取失败")
	}
}

func TestTokenUsageFillsTotalAndMeaningfulness(t *testing.T) {
	partial := TokenUsageFromValue(map[string]any{"prompt_tokens": 3.0, "completion_tokens": 4.0})
	if partial.Total != 7 {
		t.Errorf("total 应补齐为 7, got %d", partial.Total)
	}
	if !partial.IsMeaningful() {
		t.Error("非全 0 应为 meaningful")
	}
	if (TokenUsage{}).IsMeaningful() {
		t.Error("全 0 不应 meaningful")
	}

	openai := TokenUsage{Prompt: 1, Completion: 2, Total: 3}.ToOpenAI()
	if openai["prompt_tokens"] != uint64(1) || openai["total_tokens"] != uint64(3) {
		t.Errorf("openai usage = %v", openai)
	}
	if _, exists := openai["reasoning_tokens"]; exists {
		t.Error("reasoning_tokens 不是 OpenAI 标准字段，不外发")
	}
}

func TestConvertToolCallsStripsPrivateFields(t *testing.T) {
	converted := ConvertToolCalls(json.RawMessage(`[
		{"id": "1", "function_call": {"name": "f", "arguments": "{}", "namespace": "x", "partial_arguments": "y"}}
	]`))
	if len(converted) != 1 {
		t.Fatalf("got %d 条", len(converted))
	}
	call := converted[0].(map[string]any)
	function := call["function"].(map[string]any)
	if function["name"] != "f" {
		t.Errorf("name = %v", function["name"])
	}
	if _, exists := function["namespace"]; exists {
		t.Error("namespace 必须剔除")
	}
	if _, exists := function["partial_arguments"]; exists {
		t.Error("partial_arguments 必须剔除")
	}
	if _, exists := call["function_call"]; exists {
		t.Error("不得保留 function_call 键")
	}
}

func TestChunkLineOmitsEmptyFinishReasonAndZeroUsage(t *testing.T) {
	parse := func(line string) map[string]any {
		t.Helper()
		trimmed := strings.TrimSuffix(strings.TrimPrefix(line, "data: "), "\n\n")
		var parsed map[string]any
		if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
			t.Fatal(err)
		}
		return parsed
	}

	plain := parse(ChunkLine("id", "m", map[string]any{"content": "x"}, "", nil))
	if plain["object"] != "chat.completion.chunk" || plain["model"] != "m" {
		t.Errorf("chunk 形状漂了: %v", plain)
	}
	choice := plain["choices"].([]any)[0].(map[string]any)
	if _, exists := choice["finish_reason"]; exists {
		t.Error("空 finish_reason 不应外发")
	}
	if _, exists := plain["usage"]; exists {
		t.Error("无 usage 不应外发")
	}

	// 全 0 的 usage 不应外发（否则客户端会显示 0 token）。
	withZero := parse(ChunkLine("id", "m", map[string]any{}, "stop", &TokenUsage{}))
	if _, exists := withZero["usage"]; exists {
		t.Error("全 0 usage 不应外发")
	}
	if withZero["choices"].([]any)[0].(map[string]any)["finish_reason"] != "stop" {
		t.Error("finish_reason 丢失")
	}

	withUsage := parse(ChunkLine("id", "m", map[string]any{}, "stop", &TokenUsage{Prompt: 1, Completion: 2, Total: 3}))
	if withUsage["usage"].(map[string]any)["total_tokens"] != float64(3) {
		t.Errorf("usage = %v", withUsage["usage"])
	}
}

func TestStreamConvertEmitsChunksThenDone(t *testing.T) {
	raw := "id:1\nevent:output\ndata:{\"response\":\"he\"}\n\n" +
		"id:2\nevent:output\ndata:{\"response\":\"llo\"}\n\n" +
		"id:3\nevent:token_usage\ndata:{\"prompt_tokens\":5,\"completion_tokens\":2,\"total_tokens\":7}\n\n" +
		"id:4\nevent:done\ndata:{\"finish_reason\":\"stop\"}\n\n"

	var lines []string
	outcome := StreamConvert(strings.NewReader(raw), "chatcmpl-1", "glm-5.3", func(line string) error {
		lines = append(lines, line)
		return nil
	})

	if len(lines) != 4 {
		t.Fatalf("got %d 行, want 4（两个正文 chunk + 结束 chunk + [DONE]）: %v", len(lines), lines)
	}
	if !strings.Contains(lines[0], `"content":"he"`) {
		t.Errorf("lines[0] = %s", lines[0])
	}
	if !strings.Contains(lines[1], `"content":"llo"`) {
		t.Errorf("lines[1] = %s", lines[1])
	}
	if !strings.Contains(lines[2], `"finish_reason":"stop"`) {
		t.Errorf("lines[2] = %s", lines[2])
	}
	// usage 应挂在最后一个 chunk 上。
	if !strings.Contains(lines[2], `"total_tokens":7`) {
		t.Errorf("usage 未挂上: %s", lines[2])
	}
	if lines[3] != "data: [DONE]\n\n" {
		t.Errorf("lines[3] = %q", lines[3])
	}
	if !outcome.SawDone || outcome.Usage.Total != 7 || outcome.Error != nil {
		t.Errorf("outcome = %+v", outcome)
	}
}

func TestStreamConvertForwardsErrorThenDone(t *testing.T) {
	raw := "id:1\nevent:error\ndata:{\"code\":1005,\"message\":\"plan limit\"}\n\n"
	var lines []string
	outcome := StreamConvert(strings.NewReader(raw), "chatcmpl-2", "m", func(line string) error {
		lines = append(lines, line)
		return nil
	})
	if len(lines) != 2 {
		t.Fatalf("got %d 行, want 2（error chunk + [DONE]）", len(lines))
	}
	if !strings.Contains(lines[0], `"code":1005`) {
		t.Errorf("lines[0] = %s", lines[0])
	}
	if lines[1] != "data: [DONE]\n\n" {
		t.Errorf("lines[1] = %q", lines[1])
	}
	if outcome.Error == nil || outcome.Error.Code != 1005 || outcome.Error.Message != "plan limit" {
		t.Errorf("outcome.Error = %+v", outcome.Error)
	}
	if !outcome.SawDone {
		t.Error("error 后必须 SawDone")
	}
}

// TestStreamConvertAlwaysSendsDone 上游中途断流（无 done 事件）也必须补发 [DONE]，
// 绝不悬挂连接。
func TestStreamConvertAlwaysSendsDone(t *testing.T) {
	raw := "id:1\nevent:output\ndata:{\"response\":\"partial\"}\n\n"
	var lines []string
	outcome := StreamConvert(strings.NewReader(raw), "chatcmpl-3", "m", func(line string) error {
		lines = append(lines, line)
		return nil
	})
	if len(lines) != 2 {
		t.Fatalf("got %d 行, want 2", len(lines))
	}
	if lines[1] != "data: [DONE]\n\n" {
		t.Errorf("末行 = %q", lines[1])
	}
	if !outcome.SawDone {
		t.Error("断流也必须 SawDone")
	}
}

func TestAggregateBuildsChatCompletion(t *testing.T) {
	raw := "id:1\nevent:output\ndata:{\"response\":\"he\"}\n\n" +
		"id:2\nevent:output\ndata:{\"response\":\"llo\",\"reasoning_content\":\"think\"}\n\n" +
		"id:3\nevent:output\ndata:{\"tool_calls\":[{\"function_call\":{\"name\":\"read_file\",\"arguments\":\"{}\",\"namespace\":\"x\"}}]}\n\n" +
		"id:4\nevent:token_usage\ndata:{\"prompt_tokens\":5,\"completion_tokens\":2,\"total_tokens\":7}\n\n" +
		"id:5\nevent:done\ndata:{\"finish_reason\":\"stop\"}\n\n"

	response, upstreamErr, usage := Aggregate(strings.NewReader(raw), "chatcmpl-4", "glm-5.3")
	if upstreamErr != nil {
		t.Fatalf("unexpected error: %v", upstreamErr)
	}
	if response["object"] != "chat.completion" || response["id"] != "chatcmpl-4" {
		t.Errorf("response = %v", response)
	}
	choice := response["choices"].([]any)[0].(map[string]any)
	message := choice["message"].(map[string]any)
	if message["content"] != "hello" {
		t.Errorf("content = %v", message["content"])
	}
	if message["reasoning_content"] != "think" {
		t.Errorf("reasoning = %v", message["reasoning_content"])
	}
	calls := message["tool_calls"].([]any)
	call := calls[0].(map[string]any)
	if _, exists := call["function_call"]; exists {
		t.Error("聚合结果也不得保留 function_call 键")
	}
	if call["function"].(map[string]any)["name"] != "read_file" {
		t.Errorf("tool call = %v", call)
	}
	if choice["finish_reason"] != "stop" {
		t.Errorf("finish_reason = %v", choice["finish_reason"])
	}
	if usage.Total != 7 {
		t.Errorf("usage = %+v", usage)
	}
	if response["usage"].(map[string]any)["total_tokens"] != uint64(7) {
		t.Error("聚合响应应带 usage")
	}
}

func TestAggregateReturnsUpstreamError(t *testing.T) {
	raw := "id:1\nevent:error\ndata:{\"code\":1005,\"message\":\"plan limit\"}\n\n"
	response, upstreamErr, _ := Aggregate(strings.NewReader(raw), "id", "m")
	if response != nil {
		t.Error("报错时响应体应为 nil")
	}
	if upstreamErr == nil || upstreamErr.Code != 1005 {
		t.Errorf("upstreamErr = %+v", upstreamErr)
	}
}
