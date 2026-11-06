//go:build unit

package qoder

// 伪 XML <tool_call> 回读（hub e6e7622 / issue #140）与工具参数坏 JSON
// 修复（hub 70ba58b DSH 现场）的移植测试。

import (
	"strings"
	"testing"
)

var tagNames = map[string]bool{"Bash": true, "Read": true, "ns:tool": true}

// 四形态解析：A JSON 对象 / B `Name {…}` / C 属性名 / D 逗号冒号分隔。
func TestParseToolCallTagsFourForms(t *testing.T) {
	cases := []struct {
		name  string
		block string
	}{
		{"A", "<tool_call> {\"name\": \"Bash\", \"arguments\": {\"command\": \"ls\"}} </tool_call>"},
		{"B", "<tool_call> Bash {\"command\": \"ls\"} </tool_call>"},
		{"C", "<tool_call name=\"Bash\"> {\"command\": \"ls\"} </tool_call>"},
		{"D1", "<tool_call> Bash, {\"command\": \"ls\"} </tool_call>"},
		{"D2", "<tool_call> Bash: {\"command\": \"ls\"} </tool_call>"},
		{"Fence", "<tool_call>\n```json\n{\"name\":\"Bash\",\"arguments\":{\"command\":\"ls\"}}\n```\n</tool_call>"},
	}
	for _, c := range cases {
		calls, clean := ParseToolCallTags(c.block, tagNames)
		if len(calls) != 1 {
			t.Fatalf("form %s: calls = %+v (clean %q)", c.name, calls, clean)
		}
		if calls[0].Name != "Bash" {
			t.Fatalf("form %s: name = %q", c.name, calls[0].Name)
		}
		if !strings.Contains(calls[0].Arguments, "ls") {
			t.Fatalf("form %s: args = %q", c.name, calls[0].Arguments)
		}
		if strings.Contains(clean, "tool_call") {
			t.Fatalf("form %s: clean = %q", c.name, clean)
		}
	}
	// 裸名形态：参数为 {}。
	if calls, _ := ParseToolCallTags("<tool_call> Bash </tool_call>", tagNames); len(calls) != 1 ||
		calls[0].Arguments != "{}" {
		t.Fatalf("bare name form: %+v", calls)
	}
}

// 守卫：讲解示例 / 未声明 tools / 名字不在集合 / 块后有协议结构 -> 不还原。
func TestParseToolCallTagsGuards(t *testing.T) {
	block := "<tool_call> Bash {\"command\": \"ls\"} </tool_call>"
	// 未声明 tools：讨论格式的普通回复不还原。
	if calls, _ := ParseToolCallTags(block, nil); calls != nil {
		t.Fatal("no declared tools must not recover")
	}
	if calls, _ := ParseToolCallTags(block, map[string]bool{}); calls != nil {
		t.Fatal("empty declared tools must not recover")
	}
	// 名字不在声明集合。
	if calls, _ := ParseToolCallTags(block, map[string]bool{"Other": true}); calls != nil {
		t.Fatal("undeclared name must not recover")
	}
	// 无 marker 时标签后有散文（讲解示例）。
	if calls, _ := ParseToolCallTags("例如 "+block+" 就是这样用的。", tagNames); calls != nil {
		t.Fatal("trailing prose without marker must not recover")
	}
	// 无 marker 时标签前有散文。
	if calls, _ := ParseToolCallTags("示例如下："+block, tagNames); calls != nil {
		t.Fatal("leading prose without marker must not recover")
	}
	// 无 marker 时尾部散文也不还原（hub 语义：无 marker 必须整段就是一个块；
	// 收尾散文的容忍只发生在流式候选判定，不在 parse 守卫里）。
	if calls, _ := ParseToolCallTags(block+" 以上是本次操作", tagNames); calls != nil {
		t.Fatal("trailing prose without marker must not recover")
	}
	// marker 前缀形态（真泄漏）：标签前后散文可容忍，收尾散文保留在 clean。
	if calls, clean := ParseToolCallTags("好的，我来执行。\n"+LeakMarker+"\n"+block+" 以上是本次操作", tagNames); calls == nil {
		t.Fatal("marker-prefixed block must recover")
	} else if !strings.Contains(clean, "我来执行") || !strings.Contains(clean, "以上是本次操作") {
		t.Fatalf("marker form clean = %q", clean)
	}
	// 块后再开新块（缺闭标签的截断块）-> hub 语义：尽量还原两个调用。
	if calls, _ := ParseToolCallTags(block+" <tool_call> Read {} ", tagNames); len(calls) != 2 {
		t.Fatalf("truncated second block should also recover: %+v", calls)
	}
	// 块后残留协议结构（JSON 键形态散文）-> 不还原。
	if calls, _ := ParseToolCallTags(block+" 补充 {\"name\": 正文", tagNames); calls != nil {
		t.Fatal("protocol residue after block must not recover")
	}
}

// 参数宽松修复：内层裸引号 / 尾随逗号。
func TestRepairArgsJSON(t *testing.T) {
	// 内层裸引号（DSH 现场：echo "hi" && ls）。
	fixed, ok := repairArgsJSON(`{"command":"echo "hi" && ls"}`)
	if !ok || !strings.Contains(fixed, `\"hi\"`) {
		t.Fatalf("unescaped quotes: ok=%v fixed=%q", ok, fixed)
	}
	// 尾随逗号。
	fixed, ok = repairArgsJSON(`{"a":1,}`)
	if !ok || strings.Contains(fixed, ",}") {
		t.Fatalf("trailing comma: ok=%v fixed=%q", ok, fixed)
	}
	// 两者叠加。
	fixed, ok = repairArgsJSON(`{"cmd":"echo "x"",}`)
	if !ok {
		t.Fatalf("combined repair failed: %q", fixed)
	}
	// 合法输入原样返回。
	fixed, ok = repairArgsJSON(`{"a":1}`)
	if !ok || fixed != `{"a":1}` {
		t.Fatalf("valid passthrough: ok=%v fixed=%q", ok, fixed)
	}
	// 空输入 → {}。
	if fixed, ok = repairArgsJSON("  "); !ok || fixed != "{}" {
		t.Fatalf("empty: ok=%v fixed=%q", ok, fixed)
	}
	// 修不好 → ok=false。
	if _, ok = repairArgsJSON(`{"a": `); ok {
		t.Fatal("unfixable must report false")
	}
}

// 坏参数的标签块：修复后还原成真实调用（hub 70ba58b + #140 组合场景）。
func TestParseToolCallTagsRepairsBadArgs(t *testing.T) {
	block := `<tool_call> Bash {"command":"echo "hi" && ls"} </tool_call>`
	calls, _ := ParseToolCallTags(block, tagNames)
	if len(calls) != 1 || calls[0].Name != "Bash" {
		t.Fatalf("bad-args block must recover after repair: %+v", calls)
	}
	if !strings.Contains(calls[0].Arguments, "hi") {
		t.Fatalf("args = %q", calls[0].Arguments)
	}
}

// marker+JSON 数组回读的坏参数（hub 70ba58b parse_leaked_tool_calls 部分）。
func TestParseLeakedToolCallsRepairsBadArgs(t *testing.T) {
	text := LeakMarker + "\n[{\"name\":\"Bash\",\"arguments\":\"{\\\"command\\\":\\\"echo \\\"hi\\\" && ls\\\"}\"}]"
	// 上面转义层次不好手写，直接构造内层未转义形态。
	raw := LeakMarker + "\n" + `[{"name":"Bash","arguments":"{\"command\":\"echo \"hi\" && ls\"}"}]`
	calls, _ := ParseLeakedToolCalls(raw, tagNames)
	if len(calls) != 1 {
		t.Fatalf("leaked array with bad args must recover after repair: %+v", calls)
	}
	if !strings.Contains(calls[0].Arguments, "hi") {
		t.Fatalf("args = %q", calls[0].Arguments)
	}
	_ = text
}

// 流式候选判定。
func TestToolTagCandidates(t *testing.T) {
	// 未打完的前缀。
	for _, frag := range []string{"<", "<tool", "<tool_call", "<tool_call "} {
		if !toolTagCandidate(frag) {
			t.Fatalf("fragment %q must be candidate", frag)
		}
	}
	// 普通词不打扰。
	if toolTagCandidate("<tool_calling> hello") {
		t.Fatal("<tool_calling> is a plain word, not a tag")
	}
	// 块开未闭合。
	if !toolTagCandidate("<tool_call> Bash {\"command\"") {
		t.Fatal("open block must be candidate")
	}
	// 闭标签后纯空白 / 收尾散文 -> 候选。
	if !toolTagCandidate("<tool_call> Bash {} </tool_call>") {
		t.Fatal("closed block must be candidate")
	}
	if !toolTagCandidate("<tool_call> Bash {} </tool_call>\n以上。") {
		t.Fatal("closed block with trailing prose must stay candidate")
	}
	// 闭标签后出现协议结构 -> 证伪。
	if toolTagCandidate("<tool_call> Bash {} </tool_call> <tool_call> Read") {
		// 注意：块后再开新块仍是候选（下一个块没收完）；证伪的是
		// 「非协议结构的判定」——此例其实应仍为候选（新块前缀）。
		// 换成散文里带 JSON 键的形态来证伪：
		_ = 0
	}
	if toolTagCandidate("<tool_call> Bash {} </tool_call> 后面是 {\"name\": 正文") {
		t.Fatal("protocol structure after close must falsify")
	}
	// 尾巴暂留。
	if toolTagTailHold("散文尾巴 <tool_c") != "<tool_c" {
		t.Fatal("partial tail must be held")
	}
	if toolTagTailHold("完整 <b>") != "" {
		t.Fatal("closed tag is not a tail candidate")
	}
	// 切分：块出现处起整段暂存。
	emit, hold := toolTagSplitHold("操作如下：<tool_call> Bash {} </tool_call>")
	if emit != "操作如下：" || !strings.HasPrefix(hold, "<tool_call") {
		t.Fatalf("split: emit=%q hold=%q", emit, hold)
	}
	// hold 判定（含散文前缀的整段）。
	if !toolTagHoldCandidate("散文 " + "<tool_call> Bash {}") {
		t.Fatal("hold candidate with prose lead")
	}
}

// 流式集成：伪 XML 块分帧到达（marker 链会把块内字符误当前缀切散）->
// 下游 RecoverToolTags 重新接住，流末还原 tool_calls、finish 改写。
func TestRecoverToolTagsStreaming(t *testing.T) {
	src := FramesOf(
		rawFrame(t, map[string]any{"content": "我来执行："}, nil, nil),
		rawFrame(t, map[string]any{"content": "<tool_call> Ba"}, nil, nil),
		rawFrame(t, map[string]any{"content": "sh {\"command\":\"ls -la\"} </tool"}, nil, nil),
		rawFrame(t, map[string]any{"content": "_call>"}, nil, nil),
		rawFrame(t, map[string]any{"content": ""}, "stop", nil),
	)
	pipeline := RecoverToolTags(RecoverLeakedToolCalls(src, tagNames), tagNames)
	frames, err := collectFrames(t, pipeline)
	if err != nil {
		t.Fatal(err)
	}
	parsed := parseFrames(t, frames)
	var sawToolCall bool
	var content string
	for _, p := range parsed {
		if _, ok := p.delta["tool_calls"]; ok {
			sawToolCall = true
		}
		if c, ok := p.delta["content"].(string); ok {
			content += c
		}
	}
	if !sawToolCall {
		t.Fatalf("must recover tool_calls from split frames: %+v", parsed)
	}
	if strings.Contains(content, "tool_call") {
		t.Fatalf("tag block must not leak as content: %q", content)
	}
	if !strings.Contains(content, "我来执行") {
		t.Fatalf("leading prose must be preserved: %q", content)
	}
	if parsed[len(parsed)-1].fin != "tool_calls" {
		t.Fatalf("finish = %v, want tool_calls", parsed[len(parsed)-1].fin)
	}
}

// 流式守卫：讲解形态（未声明 tools）原样透传正文。
func TestRecoverToolTagsUndeclaredPassthrough(t *testing.T) {
	src := FramesOf(
		rawFrame(t, map[string]any{"content": "<tool_call> Bash {} </tool_call>"}, nil, nil),
		rawFrame(t, map[string]any{"content": ""}, "stop", nil),
	)
	frames, _ := collectFrames(t, RecoverLeakedToolCalls(src, nil))
	var content string
	for _, p := range parseFrames(t, frames) {
		if c, ok := p.delta["content"].(string); ok {
			content += c
		}
	}
	if content != "<tool_call> Bash {} </tool_call>" {
		t.Fatalf("undeclared tools must passthrough: %q", content)
	}
}
