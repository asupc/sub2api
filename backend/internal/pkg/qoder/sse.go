package qoder

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// 本文件移植 qoder_proxy.py 的私有 SSE 信封处理链：
//
//	IterInnerSSE（信封解包 L4398）→ RecoverLeakedToolCalls（泄漏回读 L3086）
//	→ WithHeartbeat（心跳/空闲超时 L4255）→ 必发 "data: [DONE]"（L7161）
//	以及 AggregateStream（非流式聚合 L4452）与 DSML 工具调用回退解析（L2692）。

// FrameIter 产出一帧已编码的 SSE 字节（形如 "data: {...}\n\n" 或 ": ping\n\n"）；
// io.EOF 表示流结束。
type FrameIter interface {
	Next() ([]byte, error)
}

// sliceFrames 把固定帧列表包装成 FrameIter（测试用）。
type sliceFrames struct {
	items [][]byte
}

// FramesOf 用给定帧构造 FrameIter（测试用）。
func FramesOf(items ...[]byte) FrameIter {
	return &sliceFrames{items: items}
}

func (s *sliceFrames) Next() ([]byte, error) {
	if len(s.items) == 0 {
		return nil, io.EOF
	}
	item := s.items[0]
	s.items = s.items[1:]
	return item, nil
}

// UpstreamStatusError 上游 SSE 信封携带非 200 状态
// （UpstreamStatus，qoder_proxy.py L272-278：HTTP 200 建流后业务错以
// 信封 statusCodeValue 投递）。Status 保留信封原值（int 或字符串）。
type UpstreamStatusError struct {
	Status any
	Detail string
}

func (e *UpstreamStatusError) Error() string {
	return fmt.Sprintf("qoder: upstream status %v", e.Status)
}

// IntStatus 信封状态统一成 int（失败回 502，_to_int_status L3862-3867）。
func (e *UpstreamStatusError) IntStatus() int {
	switch v := e.Status.(type) {
	case int:
		return v
	case float64:
		return int(v)
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return int(n)
		}
	case string:
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil {
			return n
		}
	}
	return 502
}

// StripDataPrefix 剥掉 SSE 行的 data: 前缀；注释行/空行返回空串
// （strip_data_prefix L2187-2195）。
func StripDataPrefix(line string) string {
	line = pyStrip(line)
	if line == "" || strings.HasPrefix(line, ":") {
		return ""
	}
	for strings.HasPrefix(line, "data:") {
		line = pyStrip(line[5:])
	}
	if line == "" || strings.HasPrefix(line, ":") {
		return ""
	}
	return line
}

// noiseKeys 网关给 delta 填充的空噪声字段（qoder_proxy.py L253）。
var noiseKeys = []string{"extra_fields", "refusal", "reasoning_content"}

// truthy Python 真值语义（None/false/空串/空列表/空字典/0 为假）。
func truthy(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	case string:
		return t != ""
	case []any:
		return len(t) > 0
	case map[string]any:
		return len(t) > 0
	case float64:
		return t != 0
	case json.Number:
		return t.String() != "0"
	case int:
		return t != 0
	default:
		return true
	}
}

// CleanChunk 清洗一帧内层 chunk：剔除空噪声字段，全空且无收尾的帧丢弃
// （clean_chunk L2198-2224）。返回 "" 表示该帧应跳过。
func CleanChunk(raw string) string {
	var obj map[string]any
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		return raw
	}
	changed := false
	choices, _ := obj["choices"].([]any)
	for _, c := range choices {
		ch, ok := c.(map[string]any)
		if !ok {
			continue
		}
		delta, ok := ch["delta"].(map[string]any)
		if !ok {
			continue
		}
		if fc, has := delta["function_call"]; has && fc != nil {
			fcEmpty := false
			if fcm, ok := fc.(map[string]any); ok {
				fcEmpty = !truthy(fcm["name"])
			} else {
				fcEmpty = !truthy(fc)
			}
			if fcEmpty {
				delete(delta, "function_call")
				changed = true
			}
		}
		if tcl, ok := delta["tool_calls"].([]any); ok && len(tcl) == 0 {
			delete(delta, "tool_calls")
			changed = true
		}
		for _, key := range noiseKeys {
			if v, has := delta[key]; has && !truthy(v) {
				delete(delta, key)
				changed = true
			}
		}
		if len(delta) == 0 && !truthy(ch["finish_reason"]) {
			return ""
		}
	}
	if !changed {
		return raw
	}
	return marshalNoEscape(obj)
}

// UsageHolder usage 透传容器（首个非空 usage 记入）。
type UsageHolder struct {
	mu    sync.Mutex
	Usage map[string]any
}

// SetIfEmpty 仅当尚未记录时写入。
func (h *UsageHolder) SetIfEmpty(u map[string]any) {
	if h == nil || u == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.Usage == nil {
		h.Usage = u
	}
}

// Get 返回已记录的 usage（可为 nil）。
func (h *UsageHolder) Get() map[string]any {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.Usage
}

// innerSSE 私有信封解包（iter_inner_sse L4398-4449）。
// 上游帧形态：
//
//	data:{"headers":{...},"body":"<内层 OpenAI chunk JSON 字符串>","statusCodeValue":200}
//	data:{"body":"[DONE]"}
//	event:finish{...}          <- 计时元数据，忽略
type innerSSE struct {
	reader *bufio.Reader
	usage  *UsageHolder
	closed bool
}

// NewInnerSSE 解包上游信封流，产出标准 "data: {...}\n\n" 帧。
// usage 非 nil 时，内层 chunk 的首个非空 usage 记入。
func NewInnerSSE(r io.Reader, usage *UsageHolder) FrameIter {
	return &innerSSE{reader: bufio.NewReaderSize(r, 64*1024), usage: usage}
}

func (it *innerSSE) Next() ([]byte, error) {
	for {
		if it.closed {
			return nil, io.EOF
		}
		lineBytes, err := it.reader.ReadString('\n')
		if err == io.EOF && lineBytes == "" {
			return nil, io.EOF
		}
		if err != nil && err != io.EOF {
			return nil, err
		}
		data := StripDataPrefix(lineBytes)
		if data == "" {
			if err == io.EOF {
				return nil, io.EOF
			}
			continue
		}
		dec := json.NewDecoder(strings.NewReader(data))
		dec.UseNumber()
		var outer map[string]any
		if derr := dec.Decode(&outer); derr != nil {
			if err == io.EOF {
				return nil, io.EOF
			}
			continue
		}
		if !envelopeStatusOK(outer["statusCodeValue"]) {
			detail := fmt.Sprintf("%v", outer)
			if body, ok := outer["body"].(string); ok {
				detail = body
			}
			return nil, &UpstreamStatusError{Status: outer["statusCodeValue"], Detail: clip(detail, 400)}
		}
		body, ok := outer["body"].(string)
		if !ok {
			if err == io.EOF {
				return nil, io.EOF
			}
			continue
		}
		if body == "[DONE]" {
			it.closed = true
			return nil, io.EOF
		}
		var inner map[string]any
		if ierr := json.Unmarshal([]byte(body), &inner); ierr != nil {
			if err == io.EOF {
				return nil, io.EOF
			}
			continue
		}
		if inner != nil {
			if u, ok := inner["usage"].(map[string]any); ok && len(u) > 0 {
				it.usage.SetIfEmpty(u)
			}
		}
		cleaned := CleanChunk(body)
		if cleaned == "" {
			if err == io.EOF {
				return nil, io.EOF
			}
			continue
		}
		frame := []byte("data: " + cleaned + "\n\n")
		if err == io.EOF {
			// 最后一帧：标记流结束，下一次调用返回 io.EOF
			it.closed = true
			return frame, nil
		}
		return frame, nil
	}
}

// envelopeStatusOK statusCodeValue in (nil, 200, "200")。
func envelopeStatusOK(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case json.Number:
		return t.String() == "200"
	case float64:
		return t == 200
	case int:
		return t == 200
	case string:
		return t == "200" || t == "200.0"
	default:
		return false
	}
}

// ---------------------------------------------------------------------------
// 泄漏文本回读（issue #8/#9/#11/#16 家族）
// ---------------------------------------------------------------------------

// markerHoldWindow 跨帧补全窗口 = 最长标记长度 - 1（码点语义，
// _MARKER_HOLD_WINDOW L2739：18 - 1 = 17）。
const markerHoldWindow = 17

// hold 上限兜底（issue #16：hold 缓冲超限即 fail-open 放行）。
const (
	holdMaxChars  = 32768
	holdMaxFrames = 200
)

// ---------------------------------------------------------------------------
// marker 变体识别（issue #60 follow-up：模型可能把 marker 写成
// [<assistant …] / 尾括号缺失 / 多括号 / 前导尖括号等形态，逐字前缀匹配
// 会整族漏识别——一律经正则与归一化处理）。
// ---------------------------------------------------------------------------

// leakMarkerOpenRe marker 开头（前导括号族 + assistant + 文案，不含尾括号）。
var leakMarkerOpenRe = regexp.MustCompile(`^[\[<(]*\s*assistant\s*请求调用工具`)

// leakMarkerRe marker 变体全文匹配（开头分支另配 ^，此处用于扫描与 span）。
var leakMarkerRe = regexp.MustCompile(`[\[<(]*\s*assistant\s*请求调用工具\s*[\]>)]?`)

// dsmlCloseRe 流中 DSML 闭合标签（issue #51：泄漏块可能以闭标签开头）。
var dsmlCloseRe = regexp.MustCompile(`^(?:\s*</[^>]*DSML[^>]*>)+`)

// leakMarkerCore marker 去空白主体（markerKey 的比对基准）。
const leakMarkerCore = "assistant请求调用工具"

// leakMarkerOpenCanonical 规范 marker 开头（归一化目标形，不含尾括号）。
const leakMarkerOpenCanonical = "[assistant 请求调用工具"

// markerKey 去掉空白后的 marker 主体（半角与全角空格）。
func markerKey(text string) string {
	return strings.ReplaceAll(strings.ReplaceAll(text, " ", ""), "\u3000", "")
}
// markerPrefixOK text 是否是某个 marker 变体的**前半截**（跨帧/截断用）。
func markerPrefixOK(text string) bool {
	raw := pyLStrip(text)
	body := strings.TrimLeft(raw, "[<")
	key := markerKey(body)
	if key == "" {
		switch strings.TrimSpace(raw) {
		case "", "[", "<", "[<":
			return true
		}
		return false
	}
	return strings.HasPrefix(leakMarkerCore, key)
}

// markerPrefixLen 文本尾部是 marker 变体真前缀的最长码点数；0 = 无。
// 窗口上限覆盖变体最长形态（前导括号族 + assistant + 文案 + 空白）。
func markerPrefixLen(text string) int {
	r := []rune(text)
	maxN := min(len(r), markerHoldWindow+6)
	for n := maxN; n > 0; n-- {
		if markerPrefixOK(string(r[len(r)-n:])) {
			return n
		}
	}
	return 0
}

// matchLeakMarker 在 s 开头匹配一个 marker 变体（含尾随空白与可选尾括号），
// 返回匹配结束位置与是否带尾括号；不匹配返回 -1。> / ) 尾括号视为 ]。
func matchLeakMarker(s string) (end int, hasClose bool) {
	m := leakMarkerOpenRe.FindStringIndex(s)
	if m == nil {
		return -1, false
	}
	end = m[1]
	rest := s[end:]
	trimmed := strings.TrimLeft(rest, pyWhitespace)
	if trimmed != "" && (trimmed[0] == ']' || trimmed[0] == '>' || trimmed[0] == ')') {
		return end + (len(rest) - len(trimmed)) + 1, true
	}
	return end, false
}

// lastMarkerSpan 返回最后一个 marker 变体的 (start, end)；没有则 (-1, -1)。
func lastMarkerSpan(text string) (int, int) {
	last, end := -1, -1
	for _, loc := range leakMarkerRe.FindAllStringIndex(text, -1) {
		last, end = loc[0], loc[1]
	}
	return last, end
}

// isBareMarker 整段恰好是一个 marker 变体（含任意括号组合与尾括号）。
func isBareMarker(t string) bool {
	if t == LeakMarker {
		return true
	}
	end, _ := matchLeakMarker(t)
	return end == len(t) && end != -1
}

// normalizeLeakMarker 归一 marker 变体：只归一**已存在**的括号（原本没写
// 尾括号就不补——否则跨帧拼接多出一个字符；写了 > / ) / ] 统一成 ]，
// 保持「一个 marker 只有一个闭括号」）；``` 围栏内的变体同理。
func normalizeLeakMarker(text string) string {
	t := pyLStrip(text)
	if t == "" {
		return text
	}
	lead := text[:len(text)-len(t)]
	if end, hasClose := matchLeakMarker(t); end != -1 {
		closing := ""
		if hasClose {
			closing = "]"
		}
		return lead + leakMarkerOpenCanonical + closing + t[end:]
	}
	if strings.HasPrefix(t, "```") {
		rest := t[3:]
		low := strings.ToLower(pyLStrip(rest))
		after := pyLStrip(rest)
		if strings.HasPrefix(low, "json") {
			after = pyLStrip(pyLStrip(rest)[4:])
		}
		if end, hasClose := matchLeakMarker(after); end != -1 {
			_ = hasClose // 围栏内变体统一归一到完整 marker
			return lead + t[:len(t)-len(after)] + LeakMarker + after[end:]
		}
	}
	return text
}

// leakBodyOffset 把「marker 之前的围栏前缀 / marker 之后的正文」解析出来
// （_leak_body_offset）。state: "pre"（围栏/前缀区）、"body"
// （已越过 marker）、"no"（显然不是）。
func leakBodyOffset(text string) (state string, body string) {
	t := pyLStrip(normalizeLeakMarker(text))
	if t == "" || strings.HasPrefix(LeakMarker, t) || markerPrefixOK(t) {
		return "pre", ""
	}
	if end, _ := matchLeakMarker(t); end != -1 {
		b := pyLStrip(t[end:])
		// issue #49：模型会连写多个 marker（数组在最后一个之后）——逐个跳过，
		// 否则 hold 会被判成「body 不是数组」而在流式中途证伪、整块补发。
		for {
			e, _ := matchLeakMarker(b)
			if e == -1 {
				break
			}
			b = pyLStrip(b[e:])
		}
		// 重复 marker 还没打完（跨帧，如 "[assista"）——仍算候选，内容留空
		// 等下一帧；不回 "pre" 是因为此处已越过首个 marker。
		if b != "" && markerPrefixOK(b) {
			return "body", ""
		}
		return "body", b
	}
	if !strings.HasPrefix(t, "`") {
		return "no", ""
	}
	if pyLen(t) < 3 {
		if strings.HasPrefix("```", t) {
			return "pre", ""
		}
		return "no", ""
	}
	if !strings.HasPrefix(t, "```") {
		return "no", ""
	}
	rest := t[3:]
	low := strings.ToLower(pyLStrip(rest))
	var after string
	switch {
	case strings.HasPrefix(low, "json"):
		after = pyLStrip(pyLStrip(rest)[4:])
	case strings.HasPrefix(LeakMarker, low) || leakMarkerRe.MatchString(low):
		after = pyLStrip(rest) // 无语言标记的 ``` 围栏
	case strings.HasPrefix("json", low):
		return "pre", "" // 语言标记还没打完（j/js/json）
	default:
		return "no", ""
	}
	if strings.HasPrefix(LeakMarker, after) || markerPrefixOK(after) {
		return "pre", ""
	}
	if end, _ := matchLeakMarker(after); end != -1 {
		return "body", after[end:]
	}
	return "no", ""
}

// leakPrefixHold 文本仍可能是泄漏块的前缀？（流式 hold-back 用）
func leakPrefixHold(text string) bool {
	st, _ := leakBodyOffset(text)
	return st != "no"
}

// findMarkerSplit 把正文切成 (立即发出, 暂存候选)：在首个完整标记处切开
// （issue #16-B1，_find_marker_split L2895-2923）。帧尾是某标记的真前缀
// （长度 < 标记）时，把这段前缀留给下一帧。
func findMarkerSplit(text string, allowPrefix bool) (emit, hold string) {
	if text == "" {
		return "", ""
	}
	// 首个完整标记处切开：marker 变体按正则定位（issue #60 follow-up），
	// 工具结果 marker 保持精确匹配。
	best := -1
	if loc := leakMarkerRe.FindStringIndex(text); loc != nil {
		best = loc[0]
	}
	if i := strings.Index(text, ToolResultMarker); i != -1 && (best == -1 || i < best) {
		best = i
	}
	if best != -1 {
		return text[:best], text[best:]
	}
	if !allowPrefix {
		return text, ""
	}
	r := []rune(text)
	tail := r
	if len(r) > markerHoldWindow {
		tail = r[len(r)-markerHoldWindow:]
	}
	tailStr := string(tail)
	// 尾部是 marker 变体的真前缀（1 字符/帧时 '[' 或 '<' 单独到达也不能当
	// 散文放出，否则 marker 开头丢失）；工具结果 marker 前缀保持精确匹配。
	if n := markerPrefixLen(tailStr); n > 0 {
		return string(r[:len(r)-n]), string(r[len(r)-n:])
	}
	{
		mk := ToolResultMarker
		mkr := []rune(mk)
		for n := min(len(tail), len(mkr)-1); n > 0; n-- {
			if strings.HasSuffix(tailStr, pySlice(mk, 0, n)) {
				return string(r[:len(r)-n]), string(r[len(r)-n:])
			}
		}
	}
	return text, ""
}

// jsonArrayPrefixOK 报告 s 是否仍可能扩展成合法 JSON 数组字面量（可能尾随
// 未写完的 ``` 围栏）。语义对齐 _json_array_prefix_ok（L2858-2892）；Python
// 版依赖 json.loads 的异常消息（Unterminated string / Invalid \uXXXX escape /
// Extra data / pos），Go 标准库报错形态不同，这里用手写扫描器实现同一判定：
// 任何语法错误落在最后一个字符 → 仍可能补全；错误在末尾之前 → 证伪。
func jsonArrayPrefixOK(s string) bool {
	s = pyStrip(s)
	if s == "" {
		return true
	}
	if s[0] != '[' {
		return false
	}
	n := len(s)
	// couldComplete: 错误位置在最后一个字符上（还可能由下一帧补全）
	couldComplete := func(p int) bool { return p >= n-1 }
	depth := 0
	var objStack []bool // 每层容器是否为对象（'{'），决定 ':' 是否合法
	inStr, esc := false, false
	expectValue := true
	i := 0
	for i < n {
		c := s[i]
		if inStr {
			if esc {
				esc = false
				i++
				continue
			}
			switch c {
			case '\\':
				esc = true
			case '"':
				inStr = false
				expectValue = false
			default:
				if c < 0x20 {
					return couldComplete(i) // 字符串内裸控制字符
				}
			}
			i++
			continue
		}
		switch c {
		case ' ', '\t', '\n', '\r':
			i++
			continue
		case '"':
			if !expectValue {
				return couldComplete(i) // 值之间缺分隔符
			}
			inStr = true
		case '[', '{':
			if !expectValue {
				return couldComplete(i)
			}
			depth++
			objStack = append(objStack, c == '{')
		case ']', '}':
			depth--
			if depth == 0 {
				// 顶层数组已闭合：尾随内容必须是空白或 "```" 的前缀
				tail := pyStrip(s[i+1:])
				return tail == "" || strings.HasPrefix("```", tail)
			}
			if depth < 0 {
				return false
			}
			objStack = objStack[:len(objStack)-1]
			expectValue = false
		case ',':
			if expectValue {
				return couldComplete(i)
			}
			expectValue = true
		case ':':
			if expectValue || len(objStack) == 0 || !objStack[len(objStack)-1] {
				return couldComplete(i) // ':' 仅在对象内合法
			}
			expectValue = true // 对象键值分隔，后面是值
		case 't':
			if !expectValue || !strings.HasPrefix(s[i:], "true") {
				return couldComplete(i)
			}
			i += 4
			expectValue = false
			continue
		case 'f':
			if !expectValue || !strings.HasPrefix(s[i:], "false") {
				return couldComplete(i)
			}
			i += 5
			expectValue = false
			continue
		case 'n':
			if !expectValue || !strings.HasPrefix(s[i:], "null") {
				return couldComplete(i)
			}
			i += 4
			expectValue = false
			continue
		case '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
			if !expectValue {
				return couldComplete(i)
			}
			j := i
			if s[j] == '-' {
				j++
			}
			for j < n && s[j] >= '0' && s[j] <= '9' {
				j++
			}
			if j < n && s[j] == '.' {
				j++
				for j < n && s[j] >= '0' && s[j] <= '9' {
					j++
				}
			}
			if j < n && (s[j] == 'e' || s[j] == 'E') {
				j++
				if j < n && (s[j] == '+' || s[j] == '-') {
					j++
				}
				for j < n && s[j] >= '0' && s[j] <= '9' {
					j++
				}
			}
			i = j
			expectValue = false
			continue
		default:
			return couldComplete(i)
		}
		i++
	}
	// 末尾仍未闭合（字符串/转义/括号/字面量截断）：仍可能补全
	return true
}

// ToolCall 泄漏回读还原出的结构化调用。
type ToolCall struct {
	ID        string
	Type      string
	Name      string
	Arguments string
}

// ParseLeakedToolCalls 把模型复述的「marker + JSON 数组」还原成结构化
// tool_calls（parse_leaked_tool_calls L2748-2802）。严格守卫：
//  1. 整段正文 strip 后必须恰好是 marker + JSON 数组（可带 ``` 围栏）；
//  2. 数组非空，每项 name 非空字符串、arguments 为合法 JSON（字符串或对象）；
//  3. 声明了 allowedNames 时，所有 name 必须命中声明集合。
//
// 识别成功返回 (calls, "")；未识别返回 (nil, 原文)。
func ParseLeakedToolCalls(text string, allowedNames map[string]bool) (calls []ToolCall, clean string) {
	t := pyStrip(text)
	if strings.HasPrefix(t, "```") && strings.HasSuffix(t, "```") && pyLen(t) > 6 {
		inner := t[3:]
		if len(inner) >= 4 && strings.ToLower(inner[:4]) == "json" {
			inner = inner[4:]
		}
		t = pyStrip(inner[:len(inner)-3])
	}
	// issue #51：泄漏块可能以 DSML 闭合标签开头（「闭标签 + 数组」形态），
	// 先剥掉再决定起点。
	t = pyStrip(dsmlCloseRe.ReplaceAllString(t, ""))
	// issue #60 follow-up：marker 可能写成 [<assistant …] / 尾括号缺失，
	// 按变体匹配最后一个 marker；正文里再出现的重复 marker 一并剥掉。
	mpos, mend := lastMarkerSpan(t)
	if mpos < 0 {
		return nil, text
	}
	t = pyStrip(t[mend:])
	for {
		e, _ := matchLeakMarker(t)
		if e == -1 {
			break
		}
		t = pyStrip(t[e:])
	}
	if !strings.HasPrefix(t, "[") || !strings.HasSuffix(t, "]") {
		return nil, text
	}
	var arr []any
	if err := json.Unmarshal([]byte(t), &arr); err != nil {
		return nil, text
	}
	if len(arr) == 0 {
		return nil, text
	}
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, text
		}
		name, ok := m["name"].(string)
		if !ok || pyStrip(name) == "" {
			return nil, text
		}
		args := m["arguments"]
		switch a := args.(type) {
		case map[string]any, []any:
			args = marshalNoEscape(a)
		case string:
		default:
			return nil, text
		}
		argsStr, _ := args.(string)
		if pyStrip(argsStr) != "" {
			if !json.Valid([]byte(argsStr)) {
				return nil, text
			}
		}
		calls = append(calls, ToolCall{
			ID:        newCallID(),
			Type:      "function",
			Name:      pyStrip(name),
			Arguments: argsStr,
		})
	}
	if allowedNames != nil {
		for _, c := range calls {
			if !allowedNames[c.Name] {
				return nil, text
			}
		}
	}
	return calls, ""
}

// newCallID 生成 call_ 前缀的调用 ID（_new_id L4620-4621）。
func newCallID() string {
	return "call_" + UUID4Hex()
}

// stillLeakCandidate 整段文本目前仍是「marker + JSON 数组」候选？
func stillLeakCandidate(text string) bool {
	state, body := leakBodyOffset(text)
	if state == "pre" {
		return true
	}
	if state == "no" {
		return false
	}
	rest := pyLStrip(body)
	if rest == "" {
		return true
	}
	if strings.HasSuffix(rest, "```") {
		rest = rest[:len(rest)-3]
	}
	return jsonArrayPrefixOK(rest)
}

// leakedPartialDroppable issue #9：正文是网关自己写入历史的工具调用回声、
// 但被截断（JSON 不完整/非法）时应吞掉整段（_leaked_partial_droppable
// L2954-2987）。三个条件：以 marker 开头 + 声明了 tools + 仍是数组前缀。
func leakedPartialDroppable(text string, allowedNames map[string]bool) bool {
	if text == "" || allowedNames == nil || len(allowedNames) == 0 {
		return false
	}
	t := pyStrip(text)
	if t == "" {
		return false
	}
	if isBareMarker(t) {
		// 只有 marker（含括号变体）、连数组都没开始写：判据 3 的退化情形，
		// 同样是回声
		return true
	}
	state, body := leakBodyOffset(t)
	if state != "body" {
		return false
	}
	rest := pyStrip(body)
	if strings.HasSuffix(rest, "```") {
		rest = pyStrip(rest[:len(rest)-3])
	}
	if !jsonArrayPrefixOK(rest) {
		return false
	}
	return !json.Valid([]byte(rest))
}

// leakedMalformedEcho 畸形回声兜底（本仓新增，上游 issue #16 未覆盖）：
// 模型复述「marker + JSON 数组」时把转义写坏（如 arguments 内层引号未转义），
// JSON 从中部坏掉（已闭合但非法，或闭合前被证伪）——严格还原失败，
// leakedPartialDroppable 只认"仍可补全的前缀"也不认它，整段原文会透传给
// 客户端。条件叠加后吞掉（fail-safe：未声明 tools、marker 后是散文、
// 合法 JSON 一律放行）：
//  1. 客户端声明了 tools（与既有守卫一致）；
//  2. leakBodyOffset 判定为 marker 之后（裸 marker 归 leakedPartialDroppable）；
//  3. 剥可选 ``` 围栏后以 "[" 开头、json.Valid == false（可达前提是 JSON
//     在中部坏掉：仍是合法前缀的由 leakedPartialDroppable 先处理）；
//  4. 同时包含 "name" 与 "arguments" 两个键（网关序列化的调用条目只有这两
//     种键，签名可靠，避免吞掉恰好含坏 JSON 的普通回复）。
func leakedMalformedEcho(text string, allowedNames map[string]bool) bool {
	if text == "" || allowedNames == nil || len(allowedNames) == 0 {
		return false
	}
	t := pyStrip(text)
	if t == "" || isBareMarker(t) {
		return false
	}
	state, body := leakBodyOffset(t)
	if state != "body" {
		return false
	}
	rest := pyStrip(body)
	if strings.HasSuffix(rest, "```") {
		rest = pyStrip(rest[:len(rest)-3])
	}
	if !strings.HasPrefix(rest, "[") {
		return false
	}
	if json.Valid([]byte(rest)) {
		return false
	}
	return strings.Contains(rest, `"name"`) && strings.Contains(rest, `"arguments"`)
}

// toolEchoHead 切分「[工具结果<name>]」开标记行 → (body, ok)
// （_tool_echo_head L2990-2999）。
func toolEchoHead(text string) (string, bool) {
	t := pyStrip(text)
	if !strings.HasPrefix(t, ToolResultMarker) {
		return "", false
	}
	rest := t[len(ToolResultMarker):]
	close := strings.Index(rest, "]")
	if close == -1 || strings.Contains(rest[:close], "\n") {
		return "", false // 标记行未闭合 / 换行后才有 ]：非本网关形态
	}
	return rest[close+1:], true
}

// toolEchoDroppable issue #11：正文是「[工具结果...]」回声块时吞掉整段
// （_tool_echo_droppable L3002-3027）。
func toolEchoDroppable(text string, allowedNames map[string]bool) bool {
	if text == "" || allowedNames == nil || len(allowedNames) == 0 {
		return false
	}
	body, ok := toolEchoHead(text)
	if !ok {
		return false
	}
	if strings.Contains(body, ToolResultClose) {
		return true
	}
	rest := pyLStrip(body)
	if rest == "" {
		return true // 只有开标记：退化形态
	}
	switch rest[0] {
	case '{', '[', '"', '<':
		return true
	}
	return false
}

// toolEchoPrefixHold 流式 hold-back：这段缓冲仍可能是 issue #11 的回声块？
func toolEchoPrefixHold(text string) bool {
	t := pyLStrip(text)
	if t == "" {
		return true
	}
	if strings.HasPrefix(ToolResultMarker, t) {
		return true // 标记本身还没打完
	}
	body, ok := toolEchoHead(t)
	if !ok {
		return false // 不是标记形态 -> 证伪，立即放行
	}
	if strings.Contains(body, ToolResultClose) {
		return true
	}
	rest := pyLStrip(body)
	if rest == "" {
		return true
	}
	switch rest[0] {
	case '{', '[', '"', '<':
		return true
	}
	return false
}

// echoHoldCandidate 统一暂存判定：仍可能是 #8 数组回声或 #11 工具结果回声？
func echoHoldCandidate(text string) bool {
	return stillLeakCandidate(text) || toolEchoPrefixHold(text)
}

// ToolNamesFromPayload 取客户端本次请求声明的工具名集合
// （_tool_names_from_payload L2845-2855：兼容 chat 与 responses 两种形态）。
// 未声明 tools 时返回 nil。
func ToolNamesFromPayload(payload map[string]any) map[string]bool {
	names := map[string]bool{}
	tools, _ := payload["tools"].([]any)
	for _, t := range tools {
		tm, ok := t.(map[string]any)
		if !ok {
			continue
		}
		fn, ok := tm["function"].(map[string]any)
		if !ok {
			fn = tm
		}
		if name, ok := fn["name"].(string); ok && name != "" {
			names[name] = true
		}
	}
	if len(names) == 0 {
		return nil
	}
	return names
}

// streamMeta 从上游帧捕获的 (id, model, created)，用于合成帧沿用上游标识。
type streamMeta struct {
	id      string
	model   string
	created int64
}

func (m *streamMeta) frame(delta map[string]any, finishReason any) []byte {
	id := m.id
	if id == "" {
		id = "chatcmpl-qoder"
	}
	created := m.created
	if created == 0 {
		created = time.Now().Unix()
	}
	payload := map[string]any{
		"id":      id,
		"object":  "chat.completion.chunk",
		"created": created,
		"model":   m.model,
		"choices": []any{map[string]any{
			"index":         0,
			"delta":         delta,
			"finish_reason": finishReason,
		}},
	}
	return []byte("data: " + marshalNoEscape(payload) + "\n\n")
}

// frameWithoutContent 复制一帧并去掉 delta.content（保留 tool_calls/
// reasoning/收尾信息，_frame_without_content L3067-3083）。
func frameWithoutContent(payload map[string]any) map[string]any {
	out := copyShallow(payload)
	choices, _ := out["choices"].([]any)
	newChoices := make([]any, 0, len(choices))
	for _, c := range choices {
		if ch, ok := c.(map[string]any); ok {
			ch2 := copyShallow(ch)
			if delta, ok := ch2["delta"].(map[string]any); ok {
				if _, has := delta["content"]; has {
					d2 := copyShallow(delta)
					delete(d2, "content")
					ch2["delta"] = d2
				}
			}
			newChoices = append(newChoices, ch2)
			continue
		}
		newChoices = append(newChoices, c)
	}
	out["choices"] = newChoices
	return out
}

// leakRecover 流式泄漏回读（recover_leaked_tool_calls L3086-3242）：
// 把模型逐块吐出的「marker + JSON 数组」正文恢复成结构化 tool_calls 增量帧；
// 其余内容原样透传（fail-open）。
type leakRecover struct {
	src     FrameIter
	allowed map[string]bool

	pending  [][]byte
	done     bool
	hold     string
	holding  bool
	holdN    int
	finished *finishedFrame
	meta     streamMeta
}

type finishedFrame struct {
	payload map[string]any
	fin     any
}

// RecoverLeakedToolCalls 包装一个 "data: {...}" 帧迭代器。
func RecoverLeakedToolCalls(src FrameIter, allowedNames map[string]bool) FrameIter {
	return &leakRecover{src: src, allowed: allowedNames}
}

func (lr *leakRecover) emit(frame []byte) {
	lr.pending = append(lr.pending, frame)
}

func (lr *leakRecover) emitContent(text string) {
	lr.emit(lr.meta.frame(map[string]any{"content": text}, nil))
}

func (lr *leakRecover) Next() ([]byte, error) {
	for {
		if len(lr.pending) > 0 {
			f := lr.pending[0]
			lr.pending = lr.pending[1:]
			return f, nil
		}
		if lr.done {
			return nil, io.EOF
		}
		raw, err := lr.src.Next()
		if err == io.EOF {
			lr.finishStream()
			lr.done = true
			continue
		}
		if err != nil {
			return nil, err
		}
		lr.process(raw)
	}
}

func (lr *leakRecover) process(raw []byte) {
	text := string(raw)
	data := StripDataPrefix(text)
	var payload map[string]any
	if data != "" && data != "[DONE]" {
		_ = json.Unmarshal([]byte(data), &payload)
	}
	if payload == nil {
		lr.emit(raw)
		return
	}
	if id, ok := payload["id"].(string); ok && id != "" {
		lr.meta.id = id
	}
	if model, ok := payload["model"].(string); ok && model != "" {
		lr.meta.model = model
	}
	if created, ok := payload["created"].(float64); ok && created != 0 {
		lr.meta.created = int64(created)
	}
	var choice map[string]any
	if choices := anyList(payload["choices"]); len(choices) > 0 {
		choice, _ = choices[0].(map[string]any)
	}
	var delta map[string]any
	if choice != nil {
		delta, _ = choice["delta"].(map[string]any)
	}
	if delta == nil {
		delta = map[string]any{}
	}
	pieceRaw, hasContent := delta["content"]
	piece, pieceIsStr := "", false
	if hasContent {
		piece, pieceIsStr = pieceRaw.(string)
	}
	var fin any
	if choice != nil {
		fin = choice["finish_reason"]
	}

	if hasContent && !pieceIsStr {
		// 非字符串 content（罕见）：不参与暂存判断，原样透传
		if lr.holding {
			lr.emitContent(lr.hold)
			lr.holding = false
			lr.holdN = 0
		}
		lr.emit(raw)
		return
	}
	if lr.holding {
		lr.hold += piece
		lr.holdN++
		if echoHoldCandidate(lr.hold) {
			if pyLen(lr.hold) > holdMaxChars || lr.holdN > holdMaxFrames {
				// issue #16 兜底：超限即 fail-open 放行
				lr.emitContent(lr.hold)
				lr.holding = false
				lr.holdN = 0
				if fin != nil {
					lr.finished = &finishedFrame{payload: frameWithoutContent(payload), fin: fin}
				}
				return
			}
			if fin != nil {
				// 该帧 content 已进入 hold，收尾帧必须去掉 content（防发两次）
				lr.finished = &finishedFrame{payload: frameWithoutContent(payload), fin: fin}
			} else if truthy(delta["tool_calls"]) || truthy(delta["reasoning_content"]) || truthy(delta["role"]) {
				other := frameWithoutContent(payload)
				lr.emit([]byte("data: " + marshalNoEscape(other) + "\n\n"))
			}
			return
		}
		// 证伪：暂存整段补发为普通正文（畸形回声例外——转义损坏块在流中
		// 就会被证伪，若不在此吞掉，修了流末路径仍会泄漏，见 leakedMalformedEcho）
		if leakedMalformedEcho(lr.hold, lr.allowed) {
			lr.holding = false
			lr.holdN = 0
			if fin != nil {
				lr.finished = &finishedFrame{payload: frameWithoutContent(payload), fin: fin}
			}
			return
		}
		lr.emitContent(lr.hold)
		lr.holding = false
		lr.holdN = 0
		if fin != nil {
			lr.finished = &finishedFrame{payload: frameWithoutContent(payload), fin: fin}
		}
		return
	}
	if fin != nil && piece == "" {
		lr.finished = &finishedFrame{payload: payload, fin: fin}
		return
	}
	if piece != "" {
		emit16, hold16 := findMarkerSplit(piece, true)
		if hold16 != "" {
			if emit16 != "" {
				lr.emitContent(emit16)
			}
			lr.hold = hold16
			lr.holding = true
			lr.holdN = 1
			if fin != nil {
				lr.finished = &finishedFrame{payload: frameWithoutContent(payload), fin: fin}
			}
			return
		}
		if leakPrefixHold(piece) || toolEchoPrefixHold(piece) {
			lr.hold = piece
			lr.holding = true
			lr.holdN = 1
			if fin != nil {
				lr.finished = &finishedFrame{payload: frameWithoutContent(payload), fin: fin}
			}
			return
		}
		lr.emit(lr.meta.frame(map[string]any{"content": piece}, fin))
		return
	}
	lr.emit(raw)
}

func (lr *leakRecover) finishStream() {
	recovered := false
	if lr.holding {
		calls, clean := ParseLeakedToolCalls(lr.hold, lr.allowed)
		if len(calls) > 0 {
			recovered = true
			for i, call := range calls {
				typ := call.Type
				if typ == "" {
					typ = "function"
				}
				lr.emit(lr.meta.frame(map[string]any{
					"tool_calls": []any{map[string]any{
						"index":    i,
						"id":       call.ID,
						"type":     typ,
						"function": map[string]any{"name": call.Name, "arguments": call.Arguments},
					}},
				}, nil))
			}
			if clean != "" {
				lr.emitContent(clean)
			}
		} else if leakedPartialDroppable(lr.hold, lr.allowed) {
			// issue #9：截断的 marker+JSON 回声无法还原，吞掉整段
		} else if leakedMalformedEcho(lr.hold, lr.allowed) {
			// 畸形回声（转义损坏，数组已闭合但 JSON 非法）：无法可靠还原，
			// 吞掉整段；还原为工具调用必须零偏差，否则宁可放弃（见函数注释）
		} else if toolEchoDroppable(lr.hold, lr.allowed) {
			// issue #11：模型复述的工具结果回声块，吞掉不还原
		} else {
			lr.emitContent(lr.hold)
		}
	}
	if lr.finished != nil {
		payload := lr.finished.payload
		if recovered {
			payload = copyShallow(payload)
			if choices := anyList(payload["choices"]); choices != nil {
				newChoices := make([]any, 0, len(choices))
				for _, c := range choices {
					if ch, ok := c.(map[string]any); ok && truthy(ch["finish_reason"]) {
						ch2 := copyShallow(ch)
						ch2["finish_reason"] = "tool_calls"
						newChoices = append(newChoices, ch2)
						continue
					}
					newChoices = append(newChoices, c)
				}
				payload["choices"] = newChoices
			}
		}
		lr.emit([]byte("data: " + marshalNoEscape(payload) + "\n\n"))
	} else if recovered {
		lr.emit(lr.meta.frame(map[string]any{}, "tool_calls"))
	}
}

// ---------------------------------------------------------------------------
// 心跳 / 空闲超时（sse_with_heartbeat L4255-4318）
// ---------------------------------------------------------------------------

// heartbeatIter 在等待上游数据的空档插入 SSE 注释心跳（": ping"），
// 防止客户端/中间代理空闲超时断开；单次空闲超过 idleLimit 返回错误。
// 读协程把上游帧送入队列，Next 只在空闲时补心跳，保证顺序与错误传播。
type heartbeatIter struct {
	src       FrameIter
	interval  time.Duration
	idleLimit time.Duration
	ch        chan hbItem
	started   bool
	// waited 连续空闲时长（跨 Next 调用累积；收到数据即清零）
	waited time.Duration
}

type hbItem struct {
	frame []byte
	err   error
}

// WithHeartbeat 包装 FrameIter。interval <= 0 时退化为直通；
// idleLimit <= 0 时用默认 900s。
func WithHeartbeat(src FrameIter, interval, idleLimit time.Duration) FrameIter {
	if idleLimit <= 0 {
		idleLimit = 900 * time.Second
	}
	return &heartbeatIter{src: src, interval: interval, idleLimit: idleLimit, ch: make(chan hbItem, 16)}
}

func (h *heartbeatIter) Next() ([]byte, error) {
	if h.interval <= 0 {
		return h.src.Next()
	}
	if !h.started {
		h.started = true
		go func() {
			defer close(h.ch)
			for {
				frame, err := h.src.Next()
				h.ch <- hbItem{frame: frame, err: err}
				if err != nil {
					return
				}
			}
		}()
	}
	waited := &h.waited
	timer := time.NewTimer(h.interval)
	defer timer.Stop()
	for {
		select {
		case item, ok := <-h.ch:
			if !ok {
				return nil, io.EOF
			}
			if item.err != nil {
				return nil, item.err
			}
			*waited = 0
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(h.interval)
			return item.frame, nil
		case <-timer.C:
			*waited += h.interval
			if *waited >= h.idleLimit {
				return nil, fmt.Errorf("qoder: upstream produced no data for %.0fs", waited.Seconds())
			}
			timer.Reset(h.interval)
			return []byte(": ping\n\n"), nil
		}
	}
}

// ---------------------------------------------------------------------------
// 顶层转换器：私有 SSE 流 -> OpenAI chat.completion.chunk 流
// ---------------------------------------------------------------------------

// ChatStreamOptions 流式转换选项。
type ChatStreamOptions struct {
	// AllowedToolNames 客户端本次请求声明的工具名（泄漏回读守卫）；
	// nil 表示未声明 tools。
	AllowedToolNames map[string]bool
	// Usage usage 透传容器（可为 nil）。
	Usage *UsageHolder
	// HeartbeatInterval 心跳间隔；<= 0 关闭心跳。
	HeartbeatInterval time.Duration
	// IdleLimit 单次空闲上限（默认 900s，超时返回错误）。
	IdleLimit time.Duration
}

// chatStream 顶层流转换器：信封解包 → 泄漏回读 → 心跳，流末必发
// "data: [DONE]\n\n"（do_POST 流式路径 L7092-7172 的管道）。
type chatStream struct {
	src      FrameIter
	doneSent bool
}

// NewChatStream 把上游私有 SSE 流（io.Reader）转换为 OpenAI
// chat.completion.chunk 帧序列。
func NewChatStream(r io.Reader, opts ChatStreamOptions) FrameIter {
	inner := NewInnerSSE(r, opts.Usage)
	recovered := RecoverLeakedToolCalls(inner, opts.AllowedToolNames)
	var src FrameIter = recovered
	if opts.HeartbeatInterval > 0 {
		src = WithHeartbeat(recovered, opts.HeartbeatInterval, opts.IdleLimit)
	}
	return &chatStream{src: src}
}

func (s *chatStream) Next() ([]byte, error) {
	if s.doneSent {
		return nil, io.EOF
	}
	frame, err := s.src.Next()
	if err == io.EOF {
		s.doneSent = true
		return []byte("data: [DONE]\n\n"), nil
	}
	if err != nil {
		return nil, err
	}
	return frame, nil
}

// ---------------------------------------------------------------------------
// DSML 工具调用回退解析（DeepSeek 族；qoder_proxy.py L2679-2714）
// ---------------------------------------------------------------------------

// Python 源正则带 re.DOTALL（L2681-2689），Go 以 (?s) 前缀等价表达。
var (
	dsmlCallsRe  = regexp.MustCompile(`(?s)<[^>]*DSML[^>]*calls>(.*?)</[^>]*DSML[^>]*calls>`)
	dsmlInvokeRe = regexp.MustCompile(`(?s)<[^>]*DSML[^>]*invoke\s+name=[\x22\x27]([^\x22\x27]+)[\x22\x27]>(.*?)</[^>]*invoke>`)
	dsmlParamRe  = regexp.MustCompile(`(?s)<[^>]*DSML[^>]*parameter\s+name=[\x22\x27]([^\x22\x27]+)[\x22\x27][^>]*>(.*?)</[^>]*parameter>`)
)

// ParseDsmlToolCalls 把模型正文里的 <…DSML…calls> 块解析成结构化调用
// （parse_dsml_tool_calls）。命中时返回 (calls, 清理后的正文)；
// 未命中返回 (nil, 原文)。源实现用于 Responses 链路的回退，
// 本包保留同等解析能力供调用方按需接线。
func ParseDsmlToolCalls(text string) (calls []ToolCall, clean string) {
	if text == "" || !strings.Contains(text, "DSML") {
		return nil, text
	}
	m := dsmlCallsRe.FindStringSubmatchIndex(text)
	if m == nil {
		return nil, text
	}
	callsBlock := text[m[2]:m[3]]
	for _, inv := range dsmlInvokeRe.FindAllStringSubmatch(callsBlock, -1) {
		funcName, paramsBlock := inv[1], inv[2]
		params := map[string]string{}
		for _, pm := range dsmlParamRe.FindAllStringSubmatch(paramsBlock, -1) {
			params[pm[1]] = pyStrip(pm[2])
		}
		calls = append(calls, ToolCall{
			ID:        newCallID(),
			Name:      funcName,
			Arguments: marshalNoEscape(params),
		})
	}
	clean = pyStrip(pyStrip(text[:m[0]]) + " " + pyStrip(text[m[1]:]))
	return calls, clean
}

// ---------------------------------------------------------------------------
// 非流式聚合（aggregate_stream L4452-4593）
// ---------------------------------------------------------------------------

// AggregatedChat 非流式聚合结果（chat.completion 形态 + 计时诊断）。
type AggregatedChat struct {
	ID           string         `json:"id"`
	Object       string         `json:"object"`
	Created      int64          `json:"created"`
	Model        string         `json:"model"`
	Message      AggMessage     `json:"message"`
	FinishReason string         `json:"finish_reason"`
	Usage        map[string]any `json:"usage,omitempty"`
	ElapsedMS    int64          `json:"elapsed_ms"`
	FirstChunkAt *float64       `json:"first_chunk_at"`
}

// AggMessage 聚合出的 assistant 消息。
type AggMessage struct {
	Role             string     `json:"role"`
	Content          string     `json:"content"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
}

// AggregateStream 把上游信封流折叠成一个非流式 chat.completion 对象。
// allowedNames 为本次请求声明的工具名集合（泄漏回读守卫）。
func AggregateStream(r io.Reader, model string, usage *UsageHolder, allowedNames map[string]bool) (*AggregatedChat, error) {
	started := time.Now()
	var content, reasoning []string
	finish := "stop"
	toolCalls := map[int]*ToolCall{}
	respID := ""
	created := int64(0)
	var firstChunkAt *float64

	it := NewInnerSSE(r, usage)
	for {
		frame, err := it.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		data := StripDataPrefix(string(frame))
		if data == "" || data == "[DONE]" {
			continue
		}
		var chunk map[string]any
		if json.Unmarshal([]byte(data), &chunk) != nil {
			continue
		}
		if firstChunkAt == nil {
			elapsed := time.Since(started).Seconds()
			firstChunkAt = &elapsed
		}
		if id, ok := chunk["id"].(string); ok && id != "" {
			respID = id
		}
		if m, ok := chunk["model"].(string); ok && m != "" {
			model = m
		}
		if c, ok := chunk["created"].(float64); ok && c != 0 {
			created = int64(c)
		}
		if u, ok := chunk["usage"].(map[string]any); ok && len(u) > 0 {
			usage.SetIfEmpty(u)
		}
		for _, cs := range anyList(chunk["choices"]) {
			choice, ok := cs.(map[string]any)
			if !ok {
				continue
			}
			delta, _ := choice["delta"].(map[string]any)
			if delta == nil {
				delta = map[string]any{}
			}
			switch cv := delta["content"].(type) {
			case string:
				if cv != "" {
					content = append(content, cv)
				}
			case []any:
				for _, p := range cv {
					switch pt := p.(type) {
					case string:
						content = append(content, pt)
					case map[string]any:
						if t, ok := pt["text"].(string); ok {
							content = append(content, t)
						}
					}
				}
			}
			if rc, ok := delta["reasoning_content"].(string); ok && rc != "" {
				reasoning = append(reasoning, rc)
			}
			for _, tcs := range anyList(delta["tool_calls"]) {
				tc, ok := tcs.(map[string]any)
				if !ok {
					continue
				}
				idx := len(toolCalls)
				if i, ok := tc["index"].(float64); ok {
					idx = int(i)
				}
				fn, _ := tc["function"].(map[string]any)
				if fn == nil {
					fn = map[string]any{}
				}
				callID, _ := tc["id"].(string)
				fnName, _ := fn["name"].(string)
				fnArgs, _ := fn["arguments"].(string)
				entry, exists := toolCalls[idx]
				if !exists {
					newID := callID
					if newID == "" {
						newID = newCallID()
					}
					typ, _ := tc["type"].(string)
					if typ == "" {
						typ = "function"
					}
					toolCalls[idx] = &ToolCall{ID: newID, Type: typ, Name: fnName, Arguments: fnArgs}
				} else {
					if callID != "" {
						entry.ID = callID
					}
					if fnName != "" {
						entry.Name += fnName
					}
					if fnArgs != "" {
						entry.Arguments += fnArgs
					}
				}
			}
			if fc, ok := delta["function_call"].(map[string]any); ok && truthy(fc["name"]) {
				idx := 0
				fnName, _ := fc["name"].(string)
				fnArgs, _ := fc["arguments"].(string)
				if entry, exists := toolCalls[idx]; !exists {
					toolCalls[idx] = &ToolCall{ID: newCallID(), Type: "function", Name: fnName, Arguments: fnArgs}
				} else {
					if fnName != "" && entry.Name == "" {
						entry.Name = fnName
					}
					if fnArgs != "" {
						entry.Arguments += fnArgs
					}
				}
			}
			if fr, ok := choice["finish_reason"].(string); ok && fr != "" {
				finish = fr
			}
		}
	}

	msg := AggMessage{Role: "assistant", Content: strings.Join(content, "")}
	if len(reasoning) > 0 {
		msg.ReasoningContent = strings.Join(reasoning, "")
	}
	// 回读（issue #8，非流式侧）：正文可能是「散文 + 回声块」
	if len(toolCalls) == 0 && msg.Content != "" {
		head16, block16 := findMarkerSplit(msg.Content, false)
		echoSrc := block16
		if echoSrc != "" {
			if rec, clean := ParseLeakedToolCalls(echoSrc, allowedNames); len(rec) > 0 {
				msg.Content = head16 + clean
				for i, c := range rec {
					cc := c
					toolCalls[i] = &cc
				}
			} else if leakedPartialDroppable(echoSrc, allowedNames) {
				msg.Content = head16
			} else if leakedMalformedEcho(echoSrc, allowedNames) {
				// 畸形回声（转义损坏）：吞掉不还原（同流式路径）
				msg.Content = head16
			} else if toolEchoDroppable(echoSrc, allowedNames) {
				msg.Content = head16
			}
		}
	}
	// 二次防御：剔除「无函数名」的空 tool_call，防止客户端死等
	hasCalls := false
	for _, tc := range toolCalls {
		if tc.Name != "" {
			hasCalls = true
			break
		}
	}
	if hasCalls {
		idxs := make([]int, 0, len(toolCalls))
		for idx, tc := range toolCalls {
			if tc.Name != "" {
				idxs = append(idxs, idx)
			}
		}
		sort.Ints(idxs)
		for _, idx := range idxs {
			msg.ToolCalls = append(msg.ToolCalls, *toolCalls[idx])
		}
		if finish == "stop" {
			finish = "tool_calls"
		}
	} else if finish == "tool_calls" {
		// 占位被全部过滤掉 -> 降级为正常结束，防止客户端无限挂起
		finish = "stop"
	}
	out := &AggregatedChat{
		ID:           respID,
		Object:       "chat.completion",
		Created:      created,
		Model:        model,
		Message:      msg,
		FinishReason: finish,
		Usage:        usage.Get(),
		ElapsedMS:    time.Since(started).Milliseconds(),
		FirstChunkAt: firstChunkAt,
	}
	if out.ID == "" {
		out.ID = "chatcmpl-qoder"
	}
	if out.Created == 0 {
		out.Created = time.Now().Unix()
	}
	return out, nil
}
