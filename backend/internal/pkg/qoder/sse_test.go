//go:build unit

package qoder

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"
)

// 夹具移植自源项目 _test_qoder.py [7]/[27]/[27.5]/[31]/[16] 各节。

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// envFrame 构造一帧私有信封（statusCodeValue 200 + body 为内层 chunk JSON 字符串）。
func envFrame(t *testing.T, inner map[string]any) []byte {
	t.Helper()
	return []byte("data: " + mustJSON(t, map[string]any{
		"statusCodeValue": 200,
		"body":            mustJSON(t, inner),
	}) + "\n\n")
}

// rawFrame 构造一帧已解包的 "data: {...}" 帧（recover 层输入）。
func rawFrame(t *testing.T, delta map[string]any, fin any, extra map[string]any) []byte {
	t.Helper()
	inner := map[string]any{
		"id": "c27", "model": "m27", "created": 1,
		"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": fin}},
	}
	for k, v := range extra {
		inner[k] = v
	}
	return []byte("data: " + mustJSON(t, inner) + "\n\n")
}

// parseFrames 解出各帧 choices[0] 供断言。
type parsedFrame struct {
	delta map[string]any
	fin   any
	raw   map[string]any
}

func parseFrames(t *testing.T, frames [][]byte) []parsedFrame {
	t.Helper()
	out := []parsedFrame{}
	for _, f := range frames {
		data := StripDataPrefix(string(f))
		if data == "" || data == "[DONE]" {
			continue
		}
		var m map[string]any
		if json.Unmarshal([]byte(data), &m) != nil {
			t.Fatalf("bad frame: %q", f)
		}
		pf := parsedFrame{raw: m}
		if choices, _ := m["choices"].([]any); len(choices) > 0 {
			if ch, ok := choices[0].(map[string]any); ok {
				pf.delta, _ = ch["delta"].(map[string]any)
				if pf.delta == nil {
					pf.delta = map[string]any{}
				}
				pf.fin = ch["finish_reason"]
			}
		}
		out = append(out, pf)
	}
	return out
}

func collectFrames(t *testing.T, it FrameIter) ([][]byte, error) {
	t.Helper()
	var out [][]byte
	for {
		f, err := it.Next()
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return out, err
		}
		out = append(out, f)
	}
}

// -------- [7] 信封解包与聚合 --------

func TestInnerSSEUnwrap(t *testing.T) {
	inner1 := map[string]any{"id": "cc1", "model": "qmodel", "created": 1,
		"choices": []any{map[string]any{"delta": map[string]any{"content": "hi"}}}}
	inner2 := map[string]any{
		"choices": []any{map[string]any{"delta": map[string]any{}, "finish_reason": "stop"}},
		"usage":   map[string]any{"prompt_tokens": 3, "completion_tokens": 2, "total_tokens": 5}}
	input := strings.Join([]string{
		string(envFrame(t, inner1)),
		"data: " + mustJSON(t, map[string]any{"body": mustJSON(t, inner2), "statusCodeValue": 200}) + "\n\n",
		"data:{\"body\":\"[DONE]\"}\n\n",
		"event:finish{\"totalTime\":10}\n",
	}, "")

	usage := &UsageHolder{}
	frames, err := collectFrames(t, NewInnerSSE(strings.NewReader(input), usage))
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 2 || !strings.HasPrefix(string(frames[0]), "data: ") {
		t.Fatalf("frames: %q", frames)
	}
	if usage.Get() == nil || usage.Get()["total_tokens"] != float64(5) {
		t.Fatalf("usage: %v", usage.Get())
	}
}

func TestInnerSSENon200Raises(t *testing.T) {
	input := "data: " + mustJSON(t, map[string]any{"body": "quota exceeded", "statusCodeValue": 503}) + "\n\n"
	_, err := collectFrames(t, NewInnerSSE(strings.NewReader(input), nil))
	if err == nil {
		t.Fatal("non-200 envelope must raise")
	}
	se, ok := err.(*UpstreamStatusError)
	if !ok {
		t.Fatalf("error type %T", err)
	}
	if se.IntStatus() != 503 || !strings.Contains(se.Detail, "quota") {
		t.Fatalf("status error: %+v", se)
	}
}

func TestCleanChunkStripsNoise(t *testing.T) {
	noisy := mustJSON(t, map[string]any{"choices": []any{map[string]any{
		"delta": map[string]any{
			"function_call":     map[string]any{"name": "", "arguments": ""},
			"reasoning_content": "",
		}}}})
	cleaned := CleanChunk(noisy)
	if cleaned != "" {
		t.Fatalf("all-noise chunk must be dropped, got %q", cleaned)
	}
	// 非空 reasoning_content 保留
	keep := CleanChunk(mustJSON(t, map[string]any{"choices": []any{map[string]any{
		"delta": map[string]any{"reasoning_content": "思考中"}}}}))
	if !strings.Contains(keep, "思考中") {
		t.Fatalf("non-empty reasoning must stay: %q", keep)
	}
	// 空帧但有 finish_reason：保留
	withFin := CleanChunk(mustJSON(t, map[string]any{"choices": []any{map[string]any{
		"delta": map[string]any{"reasoning_content": ""}, "finish_reason": "stop"}}}))
	if !strings.Contains(withFin, "stop") {
		t.Fatalf("finish frame must stay: %q", withFin)
	}
}

// -------- [27] 泄漏回读（issue #8） --------

var (
	leakCalls27 = `[{"name":"terminal","arguments":"{\"cmd\":\"ls\"}"}]`
	leak27      = LeakMarker + "\n" + leakCalls27
)

func TestParseLeakedToolCalls(t *testing.T) {
	names := map[string]bool{"terminal": true}
	calls, clean := ParseLeakedToolCalls(leak27, names)
	if len(calls) == 0 || clean != "" {
		t.Fatalf("strict form: %+v %q", calls, clean)
	}
	if calls[0].Name != "terminal" {
		t.Fatalf("name: %+v", calls[0])
	}
	var args map[string]any
	if json.Unmarshal([]byte(calls[0].Arguments), &args) != nil || args["cmd"] != "ls" {
		t.Fatalf("arguments: %q", calls[0].Arguments)
	}
	// 围栏形态
	if c, _ := ParseLeakedToolCalls("```json\n"+leak27+"\n```", names); len(c) == 0 {
		t.Fatal("fenced json form must recover")
	}
	if c, _ := ParseLeakedToolCalls("```\n"+leak27+"\n```", names); len(c) == 0 {
		t.Fatal("bare fence form must recover")
	}
	// 普通正文（讨论该标记）不误判
	if c, _ := ParseLeakedToolCalls("网关会写入 "+LeakMarker+" 这样的提示，不是调用。", nil); c != nil {
		t.Fatal("prose mentioning the marker must not recover")
	}
	// 数组后带多余文本
	if c, _ := ParseLeakedToolCalls(leak27+"\n以上。", names); c != nil {
		t.Fatal("trailing text must not recover")
	}
	// 未声明的工具名
	if c, _ := ParseLeakedToolCalls(leak27, map[string]bool{"other": true}); c != nil {
		t.Fatal("undeclared tool name must not recover")
	}
	// 空数组
	if c, _ := ParseLeakedToolCalls(LeakMarker+"\n[]", nil); c != nil {
		t.Fatal("empty array must not recover")
	}
	// arguments 为对象 -> 规范化为 JSON 字符串
	objLeak := LeakMarker + "\n" + `[{"name":"t","arguments":{"a":1}}]`
	c, _ := ParseLeakedToolCalls(objLeak, map[string]bool{"t": true})
	if len(c) == 0 {
		t.Fatal("object arguments must recover")
	}
	var obj map[string]any
	if json.Unmarshal([]byte(c[0].Arguments), &obj) != nil || obj["a"] != float64(1) {
		t.Fatalf("normalized arguments: %q", c[0].Arguments)
	}
	// arguments 非法 JSON 字符串
	badLeak := LeakMarker + "\n" + `[{"name":"t","arguments":"{not-json"}]`
	if c, _ := ParseLeakedToolCalls(badLeak, map[string]bool{"t": true}); c != nil {
		t.Fatal("invalid arguments JSON must not recover")
	}
}

func TestToolNamesFromPayload(t *testing.T) {
	got := ToolNamesFromPayload(map[string]any{"tools": []any{
		map[string]any{"type": "function", "function": map[string]any{"name": "a"}},
		map[string]any{"type": "function", "name": "b"},
	}})
	if !got["a"] || !got["b"] || len(got) != 2 {
		t.Fatalf("names: %v", got)
	}
	if ToolNamesFromPayload(map[string]any{}) != nil {
		t.Fatal("no tools -> nil")
	}
}

// 流式：marker 跨增量分段仍回读为 tool_calls 增量 + 收尾帧改写
func TestRecoverLeakedStreaming(t *testing.T) {
	src := FramesOf(
		rawFrame(t, map[string]any{"content": leak27[:9]}, nil, nil),
		rawFrame(t, map[string]any{"content": leak27[9:]}, nil, nil),
		rawFrame(t, map[string]any{"content": ""}, "stop", nil),
	)
	frames, err := collectFrames(t, RecoverLeakedToolCalls(src, map[string]bool{"terminal": true}))
	if err != nil {
		t.Fatal(err)
	}
	parsed := parseFrames(t, frames)
	var sawToolCall, sawContent bool
	for _, p := range parsed {
		if len(p.delta) > 0 {
			if _, ok := p.delta["content"]; ok {
				sawContent = true
			}
			if _, ok := p.delta["tool_calls"]; ok {
				sawToolCall = true
			}
		}
	}
	if sawContent || !sawToolCall {
		t.Fatalf("must recover tool_calls without content: %+v", parsed)
	}
	if parsed[len(parsed)-1].fin != "tool_calls" {
		t.Fatalf("final finish_reason = %v", parsed[len(parsed)-1].fin)
	}
}

// 流式：以 [ 开头但被证伪 -> 原样补发正文，不改 finish
func TestRecoverFalsifiedBracket(t *testing.T) {
	src := FramesOf(
		rawFrame(t, map[string]any{"content": "[1, 2"}, nil, nil),
		rawFrame(t, map[string]any{"content": ", 3] 这是正文"}, nil, nil),
		rawFrame(t, map[string]any{"content": ""}, "stop", nil),
	)
	frames, _ := collectFrames(t, RecoverLeakedToolCalls(src, nil))
	parsed := parseFrames(t, frames)
	var content string
	for _, p := range parsed {
		if c, ok := p.delta["content"].(string); ok {
			content += c
		}
	}
	if content != "[1, 2, 3] 这是正文" {
		t.Fatalf("content = %q", content)
	}
	if parsed[len(parsed)-1].fin != "stop" {
		t.Fatalf("finish = %v", parsed[len(parsed)-1].fin)
	}
}

// 流式：未声明工具名 -> 不吞正文，按普通文本透传
func TestRecoverUndeclaredToolPassthrough(t *testing.T) {
	src := FramesOf(
		rawFrame(t, map[string]any{"content": leak27}, nil, nil),
		rawFrame(t, map[string]any{"content": ""}, "stop", nil),
	)
	frames, _ := collectFrames(t, RecoverLeakedToolCalls(src, map[string]bool{"other_tool": true}))
	var content string
	for _, p := range parseFrames(t, frames) {
		if c, ok := p.delta["content"].(string); ok {
			content += c
		}
	}
	if content != leak27 {
		t.Fatalf("content = %q", content)
	}
}

// -------- [27.5] 截断回声必须吞掉（issue #9） --------

func TestLeakedPartialDroppable(t *testing.T) {
	names := map[string]bool{"terminal": true}
	trunc := LeakMarker + "\n" + leakCalls27[:40]
	if !leakedPartialDroppable(trunc, names) {
		t.Fatal("truncated array must be droppable")
	}
	if !leakedPartialDroppable(LeakMarker, names) {
		t.Fatal("bare marker must be droppable")
	}
	if !leakedPartialDroppable("```json\n"+trunc, names) {
		t.Fatal("fenced truncated array must be droppable")
	}
	if leakedPartialDroppable(LeakMarker+"\n这是一段解释文字。", names) {
		t.Fatal("marker + prose must not be droppable")
	}
	if leakedPartialDroppable("网关会写入 "+LeakMarker+" 这样的提示。", names) {
		t.Fatal("prose must not be droppable")
	}
	if leakedPartialDroppable(trunc, nil) || leakedPartialDroppable(trunc, map[string]bool{}) {
		t.Fatal("no declared tools -> not droppable")
	}
	if leakedPartialDroppable(leak27, map[string]bool{"other": true}) {
		t.Fatal("complete array with undeclared names -> not droppable (fail-open)")
	}
}

func TestRecoverTruncatedEchoSwallowed(t *testing.T) {
	names := map[string]bool{"terminal": true}
	trunc := LeakMarker + "\n" + leakCalls27[:40]
	// 码点语义切片（Python 测试按字符切，Go 字节切片会切断多字节字符）
	src := FramesOf(
		rawFrame(t, map[string]any{"content": pySlice(trunc, 0, 12)}, nil, nil),
		rawFrame(t, map[string]any{"content": pySlice(trunc, 12, pyLen(trunc))}, nil, nil),
		rawFrame(t, map[string]any{"content": ""}, "stop", nil),
	)
	frames, _ := collectFrames(t, RecoverLeakedToolCalls(src, names))
	parsed := parseFrames(t, frames)
	var content string
	for _, p := range parsed {
		if c, ok := p.delta["content"].(string); ok {
			content += c
		}
	}
	if content != "" {
		t.Fatalf("truncated echo must be swallowed, got %q", content)
	}
	if strings.Contains(string(mustJSON(t, frames)), LeakMarker) {
		t.Fatal("marker must never leak to the client")
	}
	if parsed[len(parsed)-1].fin != "stop" {
		t.Fatalf("finish must stay stop: %v", parsed[len(parsed)-1].fin)
	}
}

// 证伪（marker 后接散文）-> fail-open 补发原文
func TestRecoverFalsifiedMarker(t *testing.T) {
	names := map[string]bool{"terminal": true}
	src := FramesOf(
		rawFrame(t, map[string]any{"content": LeakMarker}, nil, nil),
		rawFrame(t, map[string]any{"content": "\n这是一段解释文字。"}, nil, nil),
		rawFrame(t, map[string]any{"content": ""}, "stop", nil),
	)
	frames, _ := collectFrames(t, RecoverLeakedToolCalls(src, names))
	var content string
	for _, p := range parseFrames(t, frames) {
		if c, ok := p.delta["content"].(string); ok {
			content += c
		}
	}
	if !strings.Contains(content, LeakMarker) || !strings.Contains(content, "解释文字") {
		t.Fatalf("fail-open must flush original: %q", content)
	}
}

// -------- [16-B1] 散文与 marker 同帧：散文立即发出 --------

func TestRecoverProseAndMarkerSameFrame(t *testing.T) {
	names := map[string]bool{"terminal": true}
	frame1 := "先说结论：内容如下。" + leak27
	src := FramesOf(
		rawFrame(t, map[string]any{"content": frame1}, nil, nil),
		rawFrame(t, map[string]any{"content": ""}, "stop", nil),
	)
	frames, _ := collectFrames(t, RecoverLeakedToolCalls(src, names))
	parsed := parseFrames(t, frames)
	var firstContent string
	for _, p := range parsed {
		if c, ok := p.delta["content"].(string); ok && c != "" && firstContent == "" {
			firstContent = c
		}
	}
	if firstContent != "先说结论：内容如下。" {
		t.Fatalf("prose must be emitted immediately, got %q", firstContent)
	}
	var sawToolCall bool
	for _, p := range parsed {
		if _, ok := p.delta["tool_calls"]; ok {
			sawToolCall = true
		}
	}
	if !sawToolCall {
		t.Fatal("marker block must still be recovered")
	}
}

// -------- [31] 工具结果回声块（issue #11） --------

var (
	echoNames31 = map[string]bool{"terminal": true}
	echoProd31  = ToolResultMarker + " (terminal)]\n" + `{"output": "done"}`
)

func TestToolEchoDroppable(t *testing.T) {
	if !toolEchoDroppable(echoProd31, echoNames31) {
		t.Fatal("echo block must be droppable")
	}
	if !toolEchoDroppable(ToolResultMarker+"]\n"+`{"output": "partial`, echoNames31) {
		t.Fatal("truncated echo must be droppable")
	}
	if !toolEchoDroppable(ToolResultMarker+"]", echoNames31) {
		t.Fatal("bare open marker must be droppable")
	}
	if toolEchoDroppable(ToolResultMarker+"] 这段是普通说明", echoNames31) {
		t.Fatal("prose after marker must not be droppable")
	}
	if toolEchoDroppable("网关会写入 "+ToolResultMarker+"] 这样的提示", echoNames31) {
		t.Fatal("prose mentioning marker must not be droppable")
	}
	if toolEchoDroppable(echoProd31, nil) || toolEchoDroppable(echoProd31, map[string]bool{}) {
		t.Fatal("no declared tools -> not droppable")
	}
	if toolEchoDroppable(ToolResultMarker+" 没有右括号", echoNames31) {
		t.Fatal("unclosed marker line must not be droppable")
	}
	if toolEchoDroppable(ToolResultMarker+"\n换行后才]闭合", echoNames31) {
		t.Fatal("newline before ] must not be droppable")
	}
}

func TestRecoverToolEchoSwallowed(t *testing.T) {
	src := FramesOf(
		rawFrame(t, map[string]any{"content": echoProd31[:20]}, nil, nil),
		rawFrame(t, map[string]any{"content": echoProd31[20:]}, nil, nil),
		rawFrame(t, map[string]any{"content": ""}, "stop", nil),
	)
	frames, _ := collectFrames(t, RecoverLeakedToolCalls(src, echoNames31))
	var content string
	for _, p := range parseFrames(t, frames) {
		if c, ok := p.delta["content"].(string); ok {
			content += c
		}
	}
	if content != "" {
		t.Fatalf("tool echo must be swallowed, got %q", content)
	}
}

// -------- 畸形回声兜底（本仓新增；转义损坏，上游 issue #16 未覆盖） --------

// 用户真实样例的转写：模型复述时 arguments 内层引号未转义（\\" 变 "），
// 数组已闭合但 JSON 非法。
var leakMalformed = LeakMarker + "\n" +
	`[{"arguments":"{"command":"grep -n \"^## \\" CHANGELOG-SO.md | tail -8","description":"List changelog section headings"}","name":"Bash"}]`

func TestLeakedMalformedEchoDroppable(t *testing.T) {
	names := map[string]bool{"Bash": true}
	if !leakedMalformedEcho(leakMalformed, names) {
		t.Fatal("malformed echo (unescaped quotes) must be droppable")
	}
	if !leakedMalformedEcho("```json\n"+leakMalformed+"\n```", names) {
		t.Fatal("fenced malformed echo must be droppable")
	}
	// 干净回声归严格还原，不走本兜底
	if leakedMalformedEcho(leak27, names) {
		t.Fatal("valid echo must not be treated as malformed")
	}
	// 仍可补全的前缀归 leakedPartialDroppable 先处理
	if leakedMalformedEcho(leakMalformed[:60], names) {
		t.Fatal("completable prefix must not be treated as malformed here")
	}
	// 未声明 tools：一律 fail-open
	if leakedMalformedEcho(leakMalformed, nil) || leakedMalformedEcho(leakMalformed, map[string]bool{}) {
		t.Fatal("no declared tools -> not droppable")
	}
	// 散文/裸 marker 不吞
	if leakedMalformedEcho(LeakMarker, names) {
		t.Fatal("bare marker belongs to leakedPartialDroppable")
	}
	if leakedMalformedEcho(LeakMarker+"\n这是一段解释文字。", names) {
		t.Fatal("marker + prose must not be droppable")
	}
	if leakedMalformedEcho("网关会写入 "+LeakMarker+" 这样的提示。", names) {
		t.Fatal("prose must not be droppable")
	}
	// 坏 JSON 但无调用签名（name/arguments 键）：不吞
	noSig := LeakMarker + "\n" + `[{"a": 1, "b":]`
	if leakedMalformedEcho(noSig, names) {
		t.Fatal("bad JSON without call signature must not be droppable")
	}
	// marker 之后不以 [ 开头：不吞
	notArr := LeakMarker + "\n" + `{"arguments":"x", "name":]`
	if leakedMalformedEcho(notArr, names) {
		t.Fatal("non-array body must not be droppable")
	}
}

// 流式全链路：畸形回声分帧喂入（跨帧证伪路径）必须吞掉整段，不泄漏。
func TestRecoverMalformedEchoSwallowed(t *testing.T) {
	names := map[string]bool{"Bash": true}
	half := pyLen(leakMalformed) / 2
	src := FramesOf(
		rawFrame(t, map[string]any{"content": pySlice(leakMalformed, 0, half)}, nil, nil),
		rawFrame(t, map[string]any{"content": pySlice(leakMalformed, half, pyLen(leakMalformed))}, nil, nil),
		rawFrame(t, map[string]any{"content": ""}, "stop", nil),
	)
	frames, err := collectFrames(t, RecoverLeakedToolCalls(src, names))
	if err != nil {
		t.Fatal(err)
	}
	parsed := parseFrames(t, frames)
	var content string
	var sawToolCall bool
	for _, p := range parsed {
		if c, ok := p.delta["content"].(string); ok {
			content += c
		}
		if _, ok := p.delta["tool_calls"]; ok {
			sawToolCall = true
		}
	}
	if content != "" {
		t.Fatalf("malformed echo must be swallowed, got %q", content)
	}
	if sawToolCall {
		t.Fatal("malformed echo must not be reconstructed into tool_calls")
	}
	if parsed[len(parsed)-1].fin != "stop" {
		t.Fatalf("finish = %v", parsed[len(parsed)-1].fin)
	}
}

// 流末单帧（整段一次到达 + finish）：同样吞掉。
func TestRecoverMalformedEchoSwallowedAtFinish(t *testing.T) {
	names := map[string]bool{"Bash": true}
	src := FramesOf(
		rawFrame(t, map[string]any{"content": leakMalformed}, nil, nil),
		rawFrame(t, map[string]any{"content": ""}, "stop", nil),
	)
	frames, _ := collectFrames(t, RecoverLeakedToolCalls(src, names))
	var content string
	for _, p := range parseFrames(t, frames) {
		if c, ok := p.delta["content"].(string); ok {
			content += c
		}
	}
	if content != "" {
		t.Fatalf("malformed echo at finish must be swallowed, got %q", content)
	}
}

// 非流式聚合：畸形回声吞掉，正文清空且不产生 tool_calls。
func TestAggregateStreamMalformedEchoSwallowed(t *testing.T) {
	half := pyLen(leakMalformed) / 2
	inner1 := map[string]any{"id": "c-mal", "model": "m", "created": 1,
		"choices": []any{map[string]any{"delta": map[string]any{"content": pySlice(leakMalformed, 0, half)}}}}
	inner2 := map[string]any{"choices": []any{map[string]any{
		"delta": map[string]any{"content": pySlice(leakMalformed, half, pyLen(leakMalformed))}, "finish_reason": nil}}}
	inner3 := map[string]any{"choices": []any{map[string]any{
		"delta": map[string]any{}, "finish_reason": "stop"}}}
	input := strings.Join([]string{
		string(envFrame(t, inner1)), string(envFrame(t, inner2)), string(envFrame(t, inner3)),
	}, "")
	agg, err := AggregateStream(strings.NewReader(input), "m", nil, map[string]bool{"Bash": true})
	if err != nil {
		t.Fatal(err)
	}
	if agg.Message.Content != "" {
		t.Fatalf("content must be cleared: %q", agg.Message.Content)
	}
	if len(agg.Message.ToolCalls) != 0 {
		t.Fatalf("malformed echo must not reconstruct calls: %+v", agg.Message.ToolCalls)
	}
}

// -------- [16-B1] 帧尾真前缀暂留 --------

func TestFindMarkerSplitPrefixHold(t *testing.T) {
	emit, hold := findMarkerSplit("普通文本"+LeakMarker[:7], true)
	if emit != "普通文本" || hold != LeakMarker[:7] {
		t.Fatalf("prefix hold: %q %q", emit, hold)
	}
	emit, hold = findMarkerSplit("完整正文", true)
	if emit != "完整正文" || hold != "" {
		t.Fatalf("no marker: %q %q", emit, hold)
	}
	emit, hold = findMarkerSplit("前缀"+LeakMarker+"尾巴", true)
	if emit != "前缀" || hold != LeakMarker+"尾巴" {
		t.Fatalf("full marker split: %q %q", emit, hold)
	}
}

func TestJSONArrayPrefixOK(t *testing.T) {
	cases := []struct {
		in   string
		want bool
		desc string
	}{
		{"", true, "empty"},
		{"[", true, "open bracket"},
		{`["a`, true, "unterminated string"},
		{`[1, 2`, true, "unclosed"},
		{`[1, 2]`, true, "complete"},
		{`[{"a": 1`, true, "nested unclosed"},
		{`[1, 2, 3] 这是正文`, false, "trailing prose"},
		{`["a" "b"]`, false, "missing comma"},
		{"text", false, "not array"},
		{`[]`, true, "empty array"},
		{`[1, 2]`, true, "complete no tail"},
		{"[1, 2]\n```", true, "complete with closing fence"},
		{"[1, 2]\n``", true, "fence prefix in progress"},
		{"[1, 2]\n`x", false, "broken fence"},
		{`["a\u00`, true, "truncated escape"},
		{`[tru`, false, "truncated keyword"},
		{`[1, t`, true, "keyword starting at last char"},
		{`[1, ]`, true, "trailing comma at end"},
		{`[01]`, true, "leading zero (lenient; final parse still rejects)"},
	}
	for _, c := range cases {
		if got := jsonArrayPrefixOK(c.in); got != c.want {
			t.Fatalf("%s: jsonArrayPrefixOK(%q) = %v, want %v", c.desc, c.in, got, c.want)
		}
	}
}

// -------- 心跳与 [DONE] --------

type blockingFrames struct {
	release chan struct{}
	sent    bool
}

func (b *blockingFrames) Next() ([]byte, error) {
	if b.sent {
		<-b.release
		return nil, io.EOF
	}
	b.sent = true
	return []byte("data: {}\n\n"), nil
}

func TestWithHeartbeatPings(t *testing.T) {
	src := &blockingFrames{release: make(chan struct{})}
	it := WithHeartbeat(src, 10*time.Millisecond, 0)
	first, err := it.Next()
	if err != nil || string(first) != "data: {}\n\n" {
		t.Fatalf("first frame: %q %v", first, err)
	}
	var sawPing bool
	for i := 0; i < 5; i++ {
		f, err := it.Next()
		if err != nil {
			t.Fatalf("ping iteration: %v", err)
		}
		if string(f) == ": ping\n\n" {
			sawPing = true
		}
	}
	if !sawPing {
		t.Fatal("heartbeat pings must be emitted while idle")
	}
	close(src.release)
	if _, err := it.Next(); err != io.EOF {
		t.Fatalf("after release: %v", err)
	}
}

func TestWithHeartbeatIdleTimeout(t *testing.T) {
	src := &blockingFrames{release: make(chan struct{})}
	defer close(src.release)
	it := WithHeartbeat(src, 10*time.Millisecond, 25*time.Millisecond)
	if _, err := it.Next(); err != nil { // 第一帧正常
		t.Fatal(err)
	}
	_, err := it.Next()
	for i := 0; i < 10 && err == nil; i++ {
		_, err = it.Next()
	}
	if err == nil || !strings.Contains(err.Error(), "no data") {
		t.Fatalf("idle limit must fire: %v", err)
	}
}

func TestChatStreamAlwaysEndsWithDone(t *testing.T) {
	inner := map[string]any{"id": "x", "model": "m", "created": 1,
		"choices": []any{map[string]any{"delta": map[string]any{"content": "hi"}}}}
	input := string(envFrame(t, inner)) + "data:{\"body\":\"[DONE]\"}\n\n"
	stream := NewChatStream(strings.NewReader(input), ChatStreamOptions{})
	var last string
	count := 0
	for {
		f, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		last = string(f)
		count++
	}
	if count != 2 {
		t.Fatalf("frame count = %d", count)
	}
	if last != "data: [DONE]\n\n" {
		t.Fatalf("last frame = %q", last)
	}
}

// -------- 非流式聚合 --------

func TestAggregateStreamBasic(t *testing.T) {
	inner1 := map[string]any{"id": "cc1", "model": "qmodel", "created": 1,
		"choices": []any{map[string]any{"delta": map[string]any{"content": "he"}}}}
	inner2 := map[string]any{"choices": []any{map[string]any{
		"delta": map[string]any{"content": "llo"}, "finish_reason": nil}},
		"usage": map[string]any{"total_tokens": 9}}
	inner3 := map[string]any{"choices": []any{map[string]any{
		"delta": map[string]any{}, "finish_reason": "stop"}}}
	input := strings.Join([]string{
		string(envFrame(t, inner1)), string(envFrame(t, inner2)), string(envFrame(t, inner3)),
		"data:{\"body\":\"[DONE]\"}\n\n",
	}, "")
	usage := &UsageHolder{}
	agg, err := AggregateStream(strings.NewReader(input), "fallback-model", usage, nil)
	if err != nil {
		t.Fatal(err)
	}
	if agg.Message.Content != "hello" {
		t.Fatalf("content = %q", agg.Message.Content)
	}
	if agg.FinishReason != "stop" {
		t.Fatalf("finish = %q", agg.FinishReason)
	}
	if agg.ID != "cc1" || agg.Model != "qmodel" || agg.Object != "chat.completion" {
		t.Fatalf("meta: %+v", agg)
	}
	if agg.Usage == nil || agg.Usage["total_tokens"] != float64(9) {
		t.Fatalf("usage: %v", agg.Usage)
	}
}

func TestAggregateStreamToolCallFragments(t *testing.T) {
	f1 := map[string]any{"choices": []any{map[string]any{"delta": map[string]any{
		"tool_calls": []any{map[string]any{"index": 0, "id": "call_9",
			"function": map[string]any{"name": "term", "arguments": ""}}}}}}}
	f2 := map[string]any{"choices": []any{map[string]any{"delta": map[string]any{
		"tool_calls": []any{map[string]any{"index": 0,
			"function": map[string]any{"arguments": `{"cmd":"`}}}}}}}
	f3 := map[string]any{"choices": []any{map[string]any{"delta": map[string]any{
		"tool_calls": []any{map[string]any{"index": 0,
			"function": map[string]any{"arguments": `ls"}`}}}}}}}
	f4 := map[string]any{"choices": []any{map[string]any{
		"delta": map[string]any{}, "finish_reason": "tool_calls"}}}
	input := strings.Join([]string{
		string(envFrame(t, f1)), string(envFrame(t, f2)), string(envFrame(t, f3)),
		string(envFrame(t, f4)), "data:{\"body\":\"[DONE]\"}\n\n",
	}, "")
	agg, err := AggregateStream(strings.NewReader(input), "m", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(agg.Message.ToolCalls) != 1 {
		t.Fatalf("tool calls: %+v", agg.Message.ToolCalls)
	}
	tc := agg.Message.ToolCalls[0]
	if tc.ID != "call_9" || tc.Name != "term" || tc.Arguments != `{"cmd":"ls"}` {
		t.Fatalf("merged call: %+v", tc)
	}
	if agg.FinishReason != "tool_calls" {
		t.Fatalf("finish = %q", agg.FinishReason)
	}
}

// 非流式：泄漏正文 -> 结构化 tool_calls（_test_qoder.py [27] 非流式断言）
func TestAggregateStreamLeakRecovery(t *testing.T) {
	inner1 := map[string]any{"id": "c27", "model": "m27", "created": 1,
		"choices": []any{map[string]any{"delta": map[string]any{"content": pySlice(leak27, 0, 12)}}}}
	inner2 := map[string]any{"choices": []any{map[string]any{
		"delta": map[string]any{"content": pySlice(leak27, 12, pyLen(leak27))}, "finish_reason": nil}}}
	inner3 := map[string]any{"choices": []any{map[string]any{
		"delta": map[string]any{}, "finish_reason": "stop"}}}
	input := strings.Join([]string{
		string(envFrame(t, inner1)), string(envFrame(t, inner2)), string(envFrame(t, inner3)),
	}, "")
	agg, err := AggregateStream(strings.NewReader(input), "m27", nil, map[string]bool{"terminal": true})
	if err != nil {
		t.Fatal(err)
	}
	if agg.Message.Content != "" {
		t.Fatalf("content must be cleared: %q", agg.Message.Content)
	}
	if len(agg.Message.ToolCalls) != 1 || agg.Message.ToolCalls[0].Name != "terminal" {
		t.Fatalf("tool calls: %+v", agg.Message.ToolCalls)
	}
	if agg.FinishReason != "tool_calls" {
		t.Fatalf("finish = %q", agg.FinishReason)
	}
}

// -------- DSML 回退解析 --------

func TestParseDsmlToolCalls(t *testing.T) {
	text := "前缀散文 <qm:DSML:calls><qm:DSML:invoke name=\"terminal\">" +
		"<qm:DSML:parameter name=\"cmd\">ls -la</qm:DSML:parameter>" +
		"</qm:DSML:invoke></qm:DSML:calls> 尾部散文"
	calls, clean := ParseDsmlToolCalls(text)
	if len(calls) != 1 {
		t.Fatalf("calls: %+v (clean %q)", calls, clean)
	}
	if calls[0].Name != "terminal" {
		t.Fatalf("name: %+v", calls[0])
	}
	var args map[string]any
	if json.Unmarshal([]byte(calls[0].Arguments), &args) != nil || args["cmd"] != "ls -la" {
		t.Fatalf("arguments: %q", calls[0].Arguments)
	}
	if !strings.HasPrefix(clean, "前缀散文") || !strings.HasSuffix(clean, "尾部散文") {
		t.Fatalf("clean: %q", clean)
	}
	if strings.Contains(clean, "DSML") {
		t.Fatalf("dsml tags must be removed from clean text: %q", clean)
	}
	// 无 DSML -> 原样
	if c, clean2 := ParseDsmlToolCalls("普通回复"); c != nil || clean2 != "普通回复" {
		t.Fatalf("plain text: %+v %q", c, clean2)
	}
}

func TestStripDataPrefix(t *testing.T) {
	cases := map[string]string{
		"data: {\"a\":1}": "{\"a\":1}",
		"data:{\"a\":1}":  "{\"a\":1}",
		"data: data: x":   "x",
		": comment":       "",
		"":                "",
		"   data: y  ":    "y",
	}
	for in, want := range cases {
		if got := StripDataPrefix(in); got != want {
			t.Fatalf("StripDataPrefix(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLeakMarkerVariants(t *testing.T) {
	// issue #60 follow-up：marker 的括号变体（尖括号 / 缺尾括号 / 多括号）
	// 必须与规范形同样被识别、归一与回读。
	names := map[string]bool{"t": true}
	variants := []string{
		"[<assistant 请求调用工具\n" + `[{"name":"t","arguments":{"a":1}}]`,
		"[assistant 请求调用工具\n" + `[{"name":"t","arguments":{"a":1}}]`,
		"<assistant 请求调用工具>\n" + `[{"name":"t","arguments":{"a":1}}]`,
		"[<assistant  请求调用工具]\n" + `[{"name":"t","arguments":{"a":1}}]`,
		"</tool_callsDSML>\n[assistant 请求调用工具]\n" + `[{"name":"t","arguments":{"a":1}}]`,
		// 报告者原文形态：散文前缀 + 尖括号 marker + 数组（#60 实测真机形态）。
		"歪比巴布，先回答你。[<assistant 请求调用工具] " + `[{"name":"t","arguments":"{}"}]`,
	}
	for _, v := range variants {
		if c, _ := ParseLeakedToolCalls(v, names); c == nil {
			t.Fatalf("variant must recover tool_calls: %q", v)
		}
	}
	// 变体裸 marker / 截断前缀的守卫语义。
	if !leakedPartialDroppable("[<assistant 请求调用工具", names) {
		t.Fatal("angle-bracket bare marker must be droppable")
	}
	if state, _ := leakBodyOffset("[<assista"); state != "pre" {
		t.Fatalf("in-flight variant prefix must hold, got %q", state)
	}
	// 变体归一：尾括号形态统一，不补不缺；多余闭括号视为正文杂字保留。
	if got := normalizeLeakMarker("[<assistant 请求调用工具{\"a\":1}"); got != "[assistant 请求调用工具{\"a\":1}" {
		t.Fatalf("normalize no-close must not invent one: %q", got)
	}
	if got := normalizeLeakMarker("<assistant 请求调用工具>{\"a\":1}"); got != "[assistant 请求调用工具]{\"a\":1}" {
		t.Fatalf("normalize close: %q", got)
	}
	// 散文 + marker 同帧：marker 之前先放行，marker 起进入 hold。
	emit, hold := findMarkerSplit("好的，马上[<assistant 请求调用工具]"+`[{"name":"t"`, true)
	if emit != "好的，马上" || !strings.HasPrefix(hold, "[<assistant 请求调用工具]") {
		t.Fatalf("prose+marker same frame: emit=%q hold=%q", emit, hold)
	}
}
