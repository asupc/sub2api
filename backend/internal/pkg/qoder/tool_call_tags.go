package qoder

// 伪 XML 工具调用回读：`<tool_call> Name {json} </tool_call>`（qoder2api-hub
// e6e7622 / issue #140，报告者 patch 采纳版）。
//
// 上游（尤其自定义供应商 / 经转换层中转的模型）会吐这种非标准形态：
//
//	[assistant 请求调用工具] <tool_call> Bash {"command":"ls -la"} </tool_call>
//
// 它既不是 OpenAI 结构化 tool_calls、也不是 DSML、也不是网关自己的
// marker+JSON 数组——三个既有解析器全部认不出，整段被判定成「无法还原的
// 协议残片」，流式中途 fail-closed，客户端看到的就是整轮静默中断。
//
// 本文件移植四形态解析（A：JSON 对象；B：`Bash {…}`；C：名字在属性；
// D：`Bash, {…}` / `Bash: {…}`）、宽松参数修复（escapeUnescapedQuotes /
// repairArgsJSON，hub #140 _loads_lenient + 70ba58b _repair_args_json 合并
// 口径）与流式候选判定（toolTagCandidate，供 leakRecover hold 链复用）。

import (
	"encoding/json"
	"io"
	"regexp"
	"strings"
)

// toolCallOpen 伪 XML 开标签的规范前缀（小写比对基准）。
const toolCallOpen = "<tool_call"

// toolCallTagOpenRe 闭合形态开标签（无属性，toolTagCandidate 的证伪判定用）。
var toolCallTagOpenRe = regexp.MustCompile(`(?i)<\s*tool_call\s*>`)

// toolCallTagOpenAttrRe 开标签（可带属性，如 <tool_call name="Bash">）。
var toolCallTagOpenAttrRe = regexp.MustCompile(`(?i)<\s*tool_call\b([^>]*)>`)

// toolCallTagCloseRe 闭标签。
var toolCallTagCloseRe = regexp.MustCompile(`(?i)<\s*/\s*tool_call\s*>`)

// toolCallTagAttrNameRe 属性里的工具名（name/tool/tool_name/function）。
var toolCallTagAttrNameRe = regexp.MustCompile(`(?i)\b(?:name|tool|tool_name|function)\s*=\s*(?:"([^"]+)"|'([^']+)')`)

// toolCallTagWordRe 标签体开头的工具名词法（形态 B/D 共用）。
// 源正则带前瞻（名后紧跟 `({[` / `,:=` / 结尾），RE2 无环视——前瞻部分在
// parseOneCallTag 里手工判定后续字符。
var toolCallTagWordRe = regexp.MustCompile(`^[ \t\r\n]*([A-Za-z_][A-Za-z0-9_.:\-]*)`)

// toolCallNameOKRe 还原出的工具名合法性（hub _make_tool_call 同款）。
var toolCallNameOKRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.:\-]*$`)

// trailingCommaRe 尾随逗号（对象/数组闭合符前）。
var trailingCommaRe = regexp.MustCompile(`,\s*([}\]])`)

// leakStructRe 协议结构特征（hub _LEAK_STRUCT_RE）：尾部残留里出现任一特征
// 即认为「没剥干净」，宁可交回正文也不伪造调用。
var leakStructRe = regexp.MustCompile(`name\s*=\s*["']|<tool_call|"(?:arguments|name)"\s*:|\{"`)

// escapeUnescapedQuotes 把 JSON 字符串值里**未转义**的双引号补上转义
// （hub _escape_unescaped_quotes）。
//
// 模型写 `<tool_call>Bash {"command":"echo "hi" && ls"}</tool_call>` 时，内层
// 引号没有转义，整个对象都不是合法 JSON。判据：字符串内遇到的 `"`，只有其后
// 第一个非空白字符是 `, } ] :` 或文本结束才算闭合引号，否则是内层字面引号。
// 已经写成 `\"` 的保持原样。
func escapeUnescapedQuotes(s string) string {
	var out strings.Builder
	out.Grow(len(s))
	inStr := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !inStr {
			out.WriteByte(c)
			if c == '"' {
				inStr = true
			}
			continue
		}
		if c == '\\' && i+1 < len(s) {
			out.WriteByte(c)
			out.WriteByte(s[i+1]) // 已成对的转义序列整体搬运
			i++
			continue
		}
		if c == '"' {
			j := i + 1
			for j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == '\r' || s[j] == '\n') {
				j++
			}
			if j >= len(s) || s[j] == ',' || s[j] == '}' || s[j] == ']' || s[j] == ':' {
				out.WriteByte(c) // 闭合引号
				inStr = false
			} else {
				out.WriteString(`\"`) // 内层字面引号
			}
			continue
		}
		out.WriteByte(c)
	}
	return out.String()
}

// repairArgsJSON 尽量把模型写坏的参数文本修成合法 JSON（hub #140
// _loads_lenient + 70ba58b _repair_args_json 合并口径）。
//
// 模型高频写法：内层双引号未转义（`"command":"echo "hi" && ls"`）、尾随逗号。
// 依次尝试：严格 → 转义内层裸引号 → 去尾随逗号 → 两者叠加。
// 返回修复后的字符串（空输入返回 "{}"）；全部失败 ok=false（调用方按失败
// 处理，不伪造调用）。
func repairArgsJSON(raw string) (string, bool) {
	t := strings.TrimSpace(raw)
	if t == "" {
		return "{}", true
	}
	esc := escapeUnescapedQuotes(t)
	noTrail := trailingCommaRe.ReplaceAllString(t, "$1")
	cands := []string{t}
	if esc != t {
		cands = append(cands, esc)
	}
	if noTrail != t {
		cands = append(cands, noTrail)
	}
	if esc != t && noTrail != t {
		cands = append(cands, trailingCommaRe.ReplaceAllString(esc, "$1"))
	}
	for _, cand := range cands {
		if json.Valid([]byte(cand)) {
			return cand, true
		}
	}
	return t, false
}

// loadsLenient 解析工具参数文本为 JSON 值；全部尝试失败返回 nil。
func loadsLenient(raw string) any {
	fixed, ok := repairArgsJSON(raw)
	if !ok {
		return nil
	}
	var v any
	if err := json.Unmarshal([]byte(fixed), &v); err != nil {
		return nil
	}
	return v
}

// stripJSONFence 剥掉**首/尾**代码围栏残余（头部 ```[json]、尾部 ```，
// hub _strip_json_fence）。只动首尾，不碰中段——流式切分会把开围栏切进
// 散文，块内只剩尾部残余的形态也要能剥。
func stripJSONFence(text string) string {
	t := strings.TrimSpace(text)
	if strings.HasPrefix(t, "```") {
		inner := t[3:]
		if len(inner) >= 4 && strings.EqualFold(inner[:4], "json") {
			inner = inner[4:]
		}
		t = strings.TrimLeft(inner, pyWhitespace)
	}
	if strings.HasSuffix(t, "```") && len(t) > 3 {
		t = strings.TrimRight(t[:len(t)-3], pyWhitespace)
	}
	return t
}

// makeToolCall 组装成结构化 tool_call；名字非法或参数不是合法 JSON 时
// 返回 nil（hub _make_tool_call）。
func makeToolCall(name string, args any) *ToolCall {
	name = strings.Trim(strings.TrimSpace(name), "\"'`")
	if name == "" || !toolCallNameOKRe.MatchString(name) {
		return nil
	}
	var argstr string
	switch a := args.(type) {
	case map[string]any, []any:
		argstr = marshalNoEscape(a)
	case string:
		argstr = strings.TrimSpace(a)
		if argstr == "" {
			argstr = "{}"
		}
	case nil:
		argstr = "{}"
	default:
		argstr = marshalNoEscape(a)
	}
	if !json.Valid([]byte(argstr)) {
		return nil
	}
	return &ToolCall{ID: newCallID(), Type: "function", Name: name, Arguments: argstr}
}

// parseOneCallTag 单个标签块 -> tool_call（hub _parse_one_call_tag）：
//
//	形态 A：`{"name": "Bash", "arguments": {...}}`（JSON 对象）
//	形态 B：`Bash {"command": "ls"}` / `Bash({...})` / 只有工具名
//	形态 C：`<tool_call name="Bash">{...}</tool_call>`（名字在属性里）
//	形态 D：`Bash, {"command":"ls"}` / `Bash: {...}`（名字后用逗号/冒号分隔）
func parseOneCallTag(body, attrName string) *ToolCall {
	b := stripJSONFence(body)
	if attrName != "" {
		// 形态 C：属性给了工具名，正文就是参数本身。
		b = strings.TrimLeft(b, "=:")
		b = strings.TrimSpace(b)
		if b == "" {
			return makeToolCall(attrName, nil)
		}
		args := loadsLenient(b)
		if args == nil {
			return nil
		}
		return makeToolCall(attrName, args)
	}
	if b == "" {
		return nil
	}
	if b[0] == '{' {
		var obj map[string]any
		if err := json.Unmarshal([]byte(b), &obj); err != nil {
			// 内层裸引号 / 尾随逗号的宽松重试（hub _loads_lenient）。
			fixed, ok := repairArgsJSON(b)
			if !ok || json.Unmarshal([]byte(fixed), &obj) != nil {
				return nil
			}
		}
		name, _ := obj["name"].(string)
		if name == "" {
			name, _ = obj["tool"].(string)
		}
		if name == "" {
			name, _ = obj["tool_name"].(string)
		}
		if strings.TrimSpace(name) == "" {
			return nil // 只有一个裸参数对象，没有工具名 -> 无法还原
		}
		for _, key := range [...]string{"arguments", "input", "args", "parameters"} {
			if v, ok := obj[key]; ok {
				return makeToolCall(name, v)
			}
		}
		return makeToolCall(name, nil)
	}
	// 形态 B/D：词法匹配工具名，后续字符手工判定（源正则前瞻的等价实现）。
	m := toolCallTagWordRe.FindStringSubmatch(b)
	if m == nil {
		return nil
	}
	rest := b[len(m[0]):]
	// 名后第一个非空白字符：形态 B 允许 `({[` 或结尾；形态 D 另允许 `,` `:` `=`。
	// （形态 D 的探测必须先于形态 B 的严格判定：`Bash, {…}` 若按 B 的前瞻
	// 要求「名后紧跟 ({[」会整个失败。这里统一按 D 的宽集匹配，B 是其子集。）
	rest = strings.TrimLeft(rest, " \t\r\n")
	if rest != "" {
		switch rest[0] {
		case ',', ':', '=', '(', '{', '[':
		default:
			return nil
		}
	}
	// 字符类含 `:` 是为了命名空间工具（`ns:tool`）；但 `Read: {…}` 里的冒号
	// 是分隔符，会被贪心吃进名字。只剥**尾**冒号，命名空间名不受影响。
	name := strings.TrimRight(m[1], ":")
	rest = strings.TrimLeft(strings.TrimLeft(rest, ",:="), " \t\r\n")
	if strings.HasPrefix(rest, "(") && strings.HasSuffix(rest, ")") {
		rest = strings.TrimSpace(rest[1 : len(rest)-1])
	}
	if rest == "" {
		return makeToolCall(name, nil)
	}
	args := loadsLenient(rest)
	if args == nil {
		return nil
	}
	return makeToolCall(name, args)
}

// ParseToolCallTags 把 `<tool_call> Name {json} </tool_call>` 还原成结构化
// tool_calls（hub parse_tool_call_tags）。严格守卫（避免把「讲解这个格式」的
// 普通回复误判成一次调用）：
//  1. 至少一个标签块能解析出工具名 + 合法 JSON 参数；
//  2. 本次请求声明了 tools 时，解析出的工具名必须全部命中声明集合；
//  3. 最后一个标签块之后只能剩空白 / 围栏残片——真实泄漏永远在消息末尾，
//     讲解示例后面通常还有说明文字。
//
// 识别成功返回 (calls, 剥掉标签块与 marker 后的剩余正文)；未识别返回
// (nil, 原文)。
func ParseToolCallTags(text string, allowedNames map[string]bool) (calls []ToolCall, clean string) {
	if text == "" || !strings.Contains(strings.ToLower(text), toolCallOpen) {
		return nil, text
	}
	t := text
	type span struct{ start, end int }
	var spans []span
	pos := 0
	for {
		m := toolCallTagOpenAttrRe.FindStringSubmatchIndex(t[pos:])
		if m == nil {
			break
		}
		openStart, openEnd := pos+m[0], pos+m[1]
		attrName := ""
		// m[2]:m[3] 为开标签的属性段（group 1 = `[^>]*`）。
		if openEnd >= openStart && m[3] > m[2] {
			if am := toolCallTagAttrNameRe.FindStringSubmatch(t[m[2]:m[3]]); am != nil {
				// group(1) 双引号 / group(2) 单引号。
				if am[1] != "" {
					attrName = am[1]
				} else {
					attrName = am[2]
				}
			}
		}
		closeLoc := toolCallTagCloseRe.FindStringIndex(t[openEnd:])
		nextLoc := toolCallTagOpenAttrRe.FindStringIndex(t[openEnd:])
		var body string
		var end int
		if nextLoc != nil && (closeLoc == nil || nextLoc[0] < closeLoc[0]) {
			// 缺闭标签（流被截断）：拿下一个开标签前的文本当参数。
			body, end = t[openEnd:openEnd+nextLoc[0]], openEnd+nextLoc[0]
		} else if closeLoc != nil {
			body, end = t[openEnd:openEnd+closeLoc[0]], openEnd+closeLoc[1]
		} else {
			body, end = t[openEnd:], len(t)
		}
		if call := parseOneCallTag(body, attrName); call != nil {
			calls = append(calls, *call)
			spans = append(spans, span{start: openStart, end: end})
		}
		if end <= openEnd {
			pos = openEnd // 防御：零长度块不推进会死循环
		} else {
			pos = end
		}
	}
	if len(calls) == 0 {
		return nil, text
	}
	// 未声明 tools 的会话里 `<tool_call>` 几乎都是在讨论这个格式（教程、提问、
	// 粘贴示例）——不还原，交回正文，避免把聊天内容变成客户端无法执行的调用。
	if len(allowedNames) == 0 {
		return nil, text
	}
	for _, c := range calls {
		if !allowedNames[c.Name] {
			return nil, text
		}
	}
	// 有没有网关 marker 决定了判定宽严：
	//   · 有 marker  -> 模型在复述网关写进历史的协议（真泄漏）；
	//   · 无 marker  -> 只是正文里出现了标签，必须「整段就是一个块」才算，
	//                   否则讲解示例（标签前后都有散文）会被误判成真实调用。
	hasMarker := leakMarkerRe.MatchString(t)
	lead := pyStrip(leakMarkerRe.ReplaceAllString(t[:spans[0].start], ""))
	lead = pyStrip(strings.Trim(lead, "`"))
	if !hasMarker && lead != "" {
		return nil, text
	}
	// 块之后的残留：允许 marker 变体、围栏、以及**不含协议结构的短散文**
	// （模型常写「以上是本次操作」之类收尾）。带标签/JSON 键的残留仍判失败，
	// 那是真的没剥干净，宁可交回正文也不伪造调用。
	tail := pyStrip(leakMarkerRe.ReplaceAllString(t[spans[len(spans)-1].end:], ""))
	tail = pyStrip(strings.Trim(tail, "`"))
	if tail != "" && (leakStructRe.MatchString(tail) || toolCallTagOpenRe.MatchString(tail)) {
		return nil, text
	}
	if !hasMarker && tail != "" {
		return nil, text
	}
	var b strings.Builder
	last := 0
	for _, sp := range spans {
		b.WriteString(t[last:sp.start])
		last = sp.end
	}
	b.WriteString(t[last:])
	out := leakMarkerRe.ReplaceAllString(b.String(), "")
	out = assistantTagRe.ReplaceAllString(out, "")
	return calls, pyStrip(out)
}

// assistantTagRe 裸 assistant 标签（剥块后残留的变体形态，hub 同款正则）。
var assistantTagRe = regexp.MustCompile(`<\s*assistant\s*请求调用工具\s*>`)

// toolTagCandidate 这段文本是否仍是「`<tool_call> Name {json} </tool_call>`」
// 候选（hub _tool_tag_candidate，流式 hold 判定用）。
//
// 只对**块形态本身**判定，不管前面的 marker：
//   - 标签还没打完（"<" "<tool_ca" …）-> 仍是候选，等下一帧；
//   - 标签已开、闭标签未到 -> 仍是候选；
//   - 闭标签已到 -> 之后只能剩空白/围栏/marker，出现别的字即证伪
//     （"例如 <tool_call>…</tool_call> 就是这样"这类讲解不会被吞）。
func toolTagCandidate(text string) bool {
	t := pyLStrip(text)
	if t == "" {
		return true
	}
	low := strings.ToLower(t)
	if strings.HasPrefix(toolCallOpen, low) {
		return true // "<" / "<tool_c" / "<tool_call" …（前缀未打完）
	}
	if !strings.HasPrefix(low, toolCallOpen) {
		return false
	}
	// "<tool_callXXX" 只有紧跟分隔符才算标签：`<tool_calling>` 是普通词。
	if len(low) > len(toolCallOpen) {
		nxt := low[len(toolCallOpen)]
		switch nxt {
		case '>', ' ', '\t', '\n', '\r', '/':
		default:
			return false
		}
	}
	// 开标签可以带属性（`<tool_call name="Bash">`）；只要 `>` 还没到就继续等。
	if !strings.Contains(t, ">") {
		return true
	}
	if !toolCallTagOpenAttrRe.MatchString(t) {
		return false
	}
	closeLoc := toolCallTagCloseRe.FindStringIndex(t)
	if closeLoc == nil {
		return true
	}
	rest := pyStrip(leakMarkerRe.ReplaceAllString(t[closeLoc[1]:], ""))
	rest = pyStrip(strings.Trim(rest, "`"))
	if rest == "" {
		return true
	}
	// 块后已出现正文：只要不含协议结构就仍是候选（收尾散文很常见）。
	return !leakStructRe.MatchString(rest)
}

// toolTagTailHold 返回「疑似 `<tool_call` 开头但还没打完」的尾巴（跨帧暂留，
// hub _tool_tag_tail_hold）。标签已完整（含 >）返回空——交由块候选判定。
func toolTagTailHold(text string) string {
	i := strings.LastIndexByte(text, '<')
	if i < 0 || len(text)-i > len(toolCallOpen)+8 {
		return ""
	}
	tail := text[i:]
	if strings.Contains(tail, ">") {
		return ""
	}
	if strings.HasPrefix(toolCallOpen, strings.ToLower(tail)) {
		return tail // "<" / "<tool_c" / "<tool_call" …
	}
	return ""
}

// toolTagHoldCandidate 文中含 `<tool_call>` 块候选（leakRecover hold 链的
// 证伪判定用，hub _sse_recover_tool_tags 的 buf 持有语义在 so 持续证伪模型
// 下的等价实现）：从首个标签出现处起按块候选判定；只剩未打完的标签前缀
// 尾巴也算候选（跨帧暂留）。
func toolTagHoldCandidate(text string) bool {
	low := strings.ToLower(text)
	if i := strings.Index(low, toolCallOpen); i >= 0 {
		return toolTagCandidate(text[i:])
	}
	// 尾部未打完的标签前缀（跨帧暂留场景）。
	return toolTagTailHold(text) != ""
}

// toolTagSplitHold 把正文切成 (立即发出, 暂存候选)：`<tool_call` 块出现处起
// 整段暂存（含块前散文——流末 parse 失败时原样补发，fail-open）；未打完的
// 标签前缀尾巴跨帧暂留。
func toolTagSplitHold(text string) (emit, hold string) {
	low := strings.ToLower(text)
	if i := strings.Index(low, toolCallOpen); i >= 0 {
		return text[:i], text[i:]
	}
	if tail := toolTagTailHold(text); tail != "" {
		return text[:len(text)-len(tail)], tail
	}
	return text, ""
}

// ---------------------------------------------------------------------------
// 流式独立守卫（hub _sse_recover_tool_tags，issue #140）。
// ---------------------------------------------------------------------------

// toolTagsRecover 把流式正文里的 `<tool_call> Name {json} </tool_call>` 还原
// 为 tool_calls。策略与 DSML 恢复一致：平时只暂留「像标签开头」的几个字符；
// 一旦正文出现 `<tool_call` 就转入缓冲直到流末统一解析。解析出调用则补发
// 散文 + tool_calls + finish=tool_calls；解析不出则原样补发（fail-open）。
// 未声明 tools 的会话一律不缓冲（`<tool_call>` 只会是在讨论这个格式）。
//
// 作为独立迭代器挂在 marker 回读链（RecoverLeakedToolCalls）之后——marker 链
// 的前缀暂留可能把标签开头切进散文先行放出，由这里在下游重新接住。
type toolTagsRecover struct {
	src     FrameIter
	allowed map[string]bool
	pending [][]byte
	// buf 正文缓冲（含块前散文）；空 = 未进入缓冲模式。
	buf string
	// tail 未打完的标签前缀尾巴（跨帧暂留）。
	tail string
	// emittedFenceCount 已放行正文里 ``` 围栏的累计计数（奇数 = 当前处于
	// 未闭合围栏内）。围栏判定必须在进入缓冲之前：正文已经走完的 ``` 若为
	// 奇数，说明标签落在未闭合的代码块里 = 文档示例，绝不能转成真实调用。
	emittedFenceCount int
	// finished 扣住的收尾帧（finish_reason 非空、content 为空的原帧）。
	finished []byte
	// finishRaw 收尾帧的原始 JSON（还原成功时改写 finish_reason 再发）。
	finishRaw map[string]any
	// tailUsage 缓冲期间到达的 usage 垫底帧。
	tailUsage [][]byte
	meta      streamMeta
	recovered bool
	done      bool
}

// RecoverToolTags 包装一个 "data: {...}" 帧迭代器（hub _sse_recover_tool_tags）。
func RecoverToolTags(src FrameIter, allowedNames map[string]bool) FrameIter {
	return &toolTagsRecover{src: src, allowed: allowedNames}
}

func (tt *toolTagsRecover) emit(frame []byte) {
	tt.pending = append(tt.pending, frame)
}

func (tt *toolTagsRecover) emitContent(text string) {
	tt.emit(tt.meta.frame(map[string]any{"content": text}, nil))
}

func (tt *toolTagsRecover) Next() ([]byte, error) {
	for {
		if len(tt.pending) > 0 {
			f := tt.pending[0]
			tt.pending = tt.pending[1:]
			return f, nil
		}
		if tt.done {
			return nil, io.EOF
		}
		raw, err := tt.src.Next()
		if err == io.EOF {
			tt.finishStream()
			tt.done = true
			continue
		}
		if err != nil {
			return nil, err
		}
		tt.process(raw)
	}
}

func (tt *toolTagsRecover) process(raw []byte) {
	data := StripDataPrefix(string(raw))
	if data == "" || data == "[DONE]" {
		tt.emit(raw)
		return
	}
	var payload map[string]any
	if json.Unmarshal([]byte(data), &payload) != nil {
		tt.emit(raw)
		return
	}
	choices := anyList(payload["choices"])
	if len(choices) == 0 {
		if _, ok := payload["usage"].(map[string]any); ok {
			if tt.buf != "" || tt.tail != "" {
				// 缓冲期间的 usage 垫底帧：缓存到收尾帧之后补发。
				tt.tailUsage = append(tt.tailUsage, raw)
				return
			}
			if tt.finished != nil {
				// 收尾帧已扣住但缓冲为空：先发收尾帧再透传 usage，
				// 恢复 [finish] -> [usage] 标准顺序。
				tt.emit(tt.finished)
				tt.finished = nil
				tt.finishRaw = nil
			}
		}
		tt.emit(raw)
		return
	}
	if id, ok := payload["id"].(string); ok && id != "" {
		tt.meta.id = id
	}
	if m, ok := payload["model"].(string); ok && m != "" {
		tt.meta.model = m
	}
	if c, ok := payload["created"].(float64); ok && c != 0 {
		tt.meta.created = int64(c)
	}
	choice, _ := choices[0].(map[string]any)
	if choice == nil {
		tt.emit(raw)
		return
	}
	delta, _ := choice["delta"].(map[string]any)
	if delta == nil {
		delta = map[string]any{}
	}
	piece, _ := delta["content"].(string)
	fin, hasFin := choice["finish_reason"].(string)
	if piece == "" {
		if hasFin && fin != "" {
			// 收尾帧扣住：还原成功时改写 finish_reason 再发。
			tt.finished = raw
			tt.finishRaw = payload
		} else {
			tt.emit(raw)
		}
		return
	}
	piece = tt.tail + piece
	tt.tail = ""
	if tt.buf == "" {
		hold := toolTagTailHold(piece)
		body := piece
		if hold != "" {
			body = piece[:len(piece)-len(hold)]
		}
		if strings.Contains(strings.ToLower(body), toolCallOpen) {
			if tt.fenceOddAfter(body) {
				// 标签落在未闭合围栏里 = 文档示例：当普通正文放行。
				tt.emitRewritten(payload, body)
				tt.tail = hold
				return
			}
			tt.buf = body
			tt.tail = hold
			return
		}
		if body != "" {
			tt.emitRewritten(payload, body)
		}
		tt.tail = hold
		return
	}
	tt.buf += piece
}

// fenceOddAfter 假设把 body 追加到已放行正文之后，围栏是否变为未闭合
// （``` 计数为奇数）。计数必须累计整段再算——围栏标记会跨帧拆开
// （"``" + "`"），逐帧计数会漏。
func (tt *toolTagsRecover) fenceOddAfter(body string) bool {
	return (tt.emittedFenceCount+strings.Count(body, "```"))%2 == 1
}

// emitRewritten 以原帧元数据重写 content 后放行，并累计围栏计数。
func (tt *toolTagsRecover) emitRewritten(payload map[string]any, body string) {
	tt.emittedFenceCount += strings.Count(body, "```")
	tt.emit(tt.meta.frame(map[string]any{"content": body}, nil))
}

func (tt *toolTagsRecover) finishStream() {
	if tt.buf != "" {
		calls, clean := ParseToolCallTags(tt.buf, tt.allowed)
		if len(calls) > 0 {
			tt.recovered = true
			if clean != "" {
				tt.emitContent(clean)
			}
			for i, call := range calls {
				tt.emit(tt.meta.frame(map[string]any{
					"tool_calls": []any{map[string]any{
						"index":    i,
						"id":       call.ID,
						"type":     "function",
						"function": map[string]any{"name": call.Name, "arguments": call.Arguments},
					}},
				}, nil))
			}
		} else {
			// fail-open：不是可执行的调用（讲解示例 / 参数坏了 / 名字没声明）。
			tt.emitContent(tt.buf)
		}
	}
	if tt.tail != "" {
		tt.emitContent(tt.tail)
		tt.tail = ""
	}
	if tt.finished != nil {
		frame := tt.finished
		if tt.recovered && tt.finishRaw != nil {
			// 还原成功：收尾帧的 finish_reason 改写为 tool_calls。
			rewritten := copyShallow(tt.finishRaw)
			if chs := anyList(rewritten["choices"]); chs != nil {
				newChoices := make([]any, 0, len(chs))
				for _, c := range chs {
					if ch, ok := c.(map[string]any); ok && truthy(ch["finish_reason"]) {
						ch2 := copyShallow(ch)
						ch2["finish_reason"] = "tool_calls"
						newChoices = append(newChoices, ch2)
						continue
					}
					newChoices = append(newChoices, c)
				}
				rewritten["choices"] = newChoices
			}
			frame = []byte("data: " + marshalNoEscape(rewritten) + "\n\n")
		}
		tt.emit(frame)
		tt.finished = nil
		tt.finishRaw = nil
	}
	for _, u := range tt.tailUsage {
		tt.emit(u)
	}
	tt.tailUsage = nil
}
