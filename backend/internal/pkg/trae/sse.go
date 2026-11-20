package trae

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// TokenUsage 从上游 token_usage 事件提取的用量。
type TokenUsage struct {
	Prompt     uint64 `json:"prompt_tokens"`
	Completion uint64 `json:"completion_tokens"`
	Total      uint64 `json:"total_tokens"`
	// Cached 缓存命中量（trae-switch #154/#169）：上游四种写法都认（见
	// cachedTokensOf），命中量在语义上是输入的子集，脏数据钳到 Prompt。
	//
	// db4754a：指针三态——nil = 上游未回报（未观测），&0 = 明确未命中，
	// &n = 命中 n。旧实现用 uint64 把「没测过」与「测了没中」混为一谈，
	// 命中率分母被稀释。
	Cached    *uint64 `json:"-"`
	Reasoning uint64  `json:"-"`
}

// cachedTokensOf 从上游 usage 对象读缓存命中量（trae-switch #154，
// cached_tokens_of 同款四种上游形态）：
//
//	DeepSeek  prompt_cache_hit_tokens
//	OpenAI    prompt_tokens_details.cached_tokens
//	Anthropic cache_read_input_tokens
//	裸字段    cached_tokens
//
// 上限钳到 prompt_tokens：命中量报得比 prompt 还大时，下游命中率会 > 100%。
func cachedTokensOf(value map[string]any) (uint64, bool) {
	read := func(key string) (uint64, bool) {
		if v, ok := value[key].(float64); ok {
			return uint64(v), true
		}
		return 0, false
	}
	hit, ok := read("prompt_cache_hit_tokens")
	if !ok {
		if details, ok2 := value["prompt_tokens_details"].(map[string]any); ok2 {
			hit, ok = read2(details, "cached_tokens")
		}
	}
	if !ok {
		hit, ok = read("cache_read_input_tokens")
	}
	if !ok {
		hit, ok = read("cached_tokens")
	}
	if !ok {
		return 0, false
	}
	if prompt, okP := read("prompt_tokens"); okP && prompt > 0 && hit > prompt {
		return prompt, true
	}
	return hit, true
}

// read2 map 读取辅助（cachedTokensOf 的嵌套明细用）。
func read2(m map[string]any, key string) (uint64, bool) {
	if v, ok := m[key].(float64); ok {
		return uint64(v), true
	}
	return 0, false
}

// cachePoolAssigned 把 metadata 的 cache pool 记录转成三态（db4754a）：
// 缺失 = 老上游未回报（nil）；显式 null = 本次明确无池（&false）；对象/数组/非空值
// = 已挂池（&true）。真假值 0、false 也按无池——这不是布尔字段，其余标量都不是
// 有效池记录。
func cachePoolAssigned(raw map[string]any) *bool {
	record, has := raw["prompt_cache_pool_record"]
	if !has {
		return nil
	}
	t, f := true, false
	switch record.(type) {
	case nil:
		return &f
	case map[string]any, []any:
		return &t
	default:
		return &f
	}
}

// resolveCacheObservation metadata 已证明本次没有 cache pool 时，缓存计数 0 不是
// 可测得的 miss；非零计数仍原样保留（真实命中优先于无池声明）；未见到 metadata
// 时保持旧行为，避免把老上游的明确 0 误降级。
func resolveCacheObservation(previous *uint64, poolAssigned *bool, measured *uint64) *uint64 {
	if measured != nil {
		if *measured == 0 && poolAssigned != nil && !*poolAssigned {
			return nil
		}
		return measured
	}
	return previous
}

// mergeTokenUsage 把一个 usage 事件的可信度与已积累的用量合并。
func mergeTokenUsage(previous *TokenUsage, latest TokenUsage, poolAssigned *bool) TokenUsage {
	var prevCached *uint64
	if previous != nil {
		prevCached = previous.Cached
	}
	latest.Cached = resolveCacheObservation(prevCached, poolAssigned, latest.Cached)
	return latest
}

// TokenUsageFromValue 从事件 payload 解析；字段缺失按 0 处理，
// total 缺失（或为 0）时用 prompt + completion 补齐。
func TokenUsageFromValue(value map[string]any) TokenUsage {
	read := func(key string) uint64 {
		if v, ok := value[key].(float64); ok && v > 0 {
			return uint64(v)
		}
		return 0
	}
	prompt := read("prompt_tokens")
	completion := read("completion_tokens")
	total := read("total_tokens")
	if total == 0 {
		total = prompt + completion
	}
	var cached *uint64
	if hit, ok := cachedTokensOf(value); ok {
		cached = &hit
	}
	return TokenUsage{
		Prompt:     prompt,
		Completion: completion,
		Total:      total,
		Cached:     cached,
		Reasoning:  read("reasoning_tokens"),
	}
}

// ToOpenAI 转成 OpenAI usage 对象。reasoning_tokens 不是 OpenAI 标准字段，不外发。
//
// prompt_tokens_details.cached_tokens 只在**观测到缓存状态**时写（db4754a）：
// 未观测（nil）缺键——多一个恒在的键会让严格校验的客户端拒帧；实测 0 保留 0，
// 否则下游无法区分「没测过」与「测了没中」，命中率分母失真。
func (u TokenUsage) ToOpenAI() map[string]any {
	usage := map[string]any{
		"prompt_tokens":     u.Prompt,
		"completion_tokens": u.Completion,
		"total_tokens":      u.Total,
	}
	if u.Cached != nil {
		usage["prompt_tokens_details"] = map[string]any{"cached_tokens": *u.Cached}
	}
	return usage
}

// IsMeaningful 是否拿到了有效数据（全 0 视为没拿到）。
func (u TokenUsage) IsMeaningful() bool {
	return u.Total > 0 || u.Prompt > 0 || u.Completion > 0
}

// SoloEvent 一个解析完成的 SOLO 事件。
type SoloEvent struct {
	Event     string
	Response  string
	Reasoning string
	ToolCalls json.RawMessage
	Usage     *TokenUsage
	// CachePoolAssigned Trae metadata 三态：&true=明确有池，&false=明确无池，
	// nil=未回报。只有明确有池时，token_usage 的缓存 0 才是可信的「未命中」。
	CachePoolAssigned *bool
	FinishReason      string
	ErrorCode         *int64
	ErrorMessage      string
}

// SoloParser 增量 SSE 解析器：跨 chunk 保留未完成的行与未结束的事件。
//
// 缓冲按字节保存：网络分片可能把同一个多字节字符切成两半，先转字符串再拼接
// 会得到替换字符（U+FFFD）并污染正文。
type SoloParser struct {
	buffer []byte
	event  string
	data   strings.Builder
}

// Feed 喂入一段字节，返回本次解析出的完整事件（可能为空）。
func (p *SoloParser) Feed(chunk []byte) []SoloEvent {
	p.buffer = append(p.buffer, chunk...)
	var events []SoloEvent
	for {
		index := bytes.IndexByte(p.buffer, '\n')
		if index < 0 {
			break
		}
		line := string(p.buffer[:index])
		p.buffer = p.buffer[index+1:]
		if event, ok := p.scanLine(strings.TrimSuffix(line, "\r")); ok {
			events = append(events, event)
		}
	}
	return events
}

// Finish 流结束后冲刷：把没有以空行收尾的最后一个事件也解析出来。
func (p *SoloParser) Finish() []SoloEvent {
	var events []SoloEvent
	if len(p.buffer) > 0 {
		tail := string(p.buffer)
		p.buffer = nil
		for _, line := range strings.Split(tail, "\n") {
			if event, ok := p.scanLine(strings.TrimSuffix(line, "\r")); ok {
				events = append(events, event)
			}
		}
	}
	if p.event != "" || p.data.Len() > 0 {
		events = append(events, p.buildEvent())
	}
	return events
}

// scanLine 处理一行；第二个返回值表示一个事件已收尾（遇到空行）。
// id:N / retry: 等字段对本网关无意义，忽略。
func (p *SoloParser) scanLine(line string) (SoloEvent, bool) {
	if line == "" {
		if p.event == "" && p.data.Len() == 0 {
			return SoloEvent{}, false
		}
		return p.buildEvent(), true
	}
	if rest, ok := strings.CutPrefix(line, "event:"); ok {
		p.event = strings.TrimSpace(rest)
	} else if rest, ok := strings.CutPrefix(line, "data:"); ok {
		p.data.WriteString(rest)
	}
	return SoloEvent{}, false
}

func (p *SoloParser) buildEvent() SoloEvent {
	event := p.event
	p.event = ""
	data := p.data.String()
	p.data.Reset()
	return ParseSoloEvent(event, data)
}

// ParseSoloEvent 把 (event, data) 解析为 SoloEvent。data 非法 JSON / 非对象时
// 只填事件名，其余保持零值。
func ParseSoloEvent(event, data string) SoloEvent {
	parsed := SoloEvent{Event: strings.TrimSpace(event)}
	if data == "" {
		return parsed
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(data), &raw); err != nil {
		return parsed
	}

	switch parsed.Event {
	case "output", "thought":
		// llm_utils_chat 用 output；create_agent_task 用 thought。两者都接，
		// 这样上游换端点时不必改代码。
		if text, ok := raw["response"].(string); ok {
			parsed.Response = text
		} else if text, ok := raw["thought"].(string); ok {
			parsed.Response = text
		}
		if text, ok := raw["reasoning_content"].(string); ok {
			parsed.Reasoning = text
		}
		if calls, ok := raw["tool_calls"]; ok && calls != nil {
			parsed.ToolCalls, _ = json.Marshal(calls)
		}
	case "token_usage":
		usage := TokenUsageFromValue(raw)
		parsed.Usage = &usage
	case "metadata":
		parsed.CachePoolAssigned = cachePoolAssigned(raw)
	case "done":
		if reason, ok := raw["finish_reason"].(string); ok && reason != "" {
			parsed.FinishReason = reason
		} else {
			parsed.FinishReason = "stop"
		}
	case "turn_completion":
		parsed.FinishReason = "stop"
	case "error":
		if code, ok := raw["code"].(float64); ok {
			c := int64(code)
			parsed.ErrorCode = &c
		}
		if message, ok := raw["message"].(string); ok && message != "" {
			parsed.ErrorMessage = message
		} else {
			parsed.ErrorMessage = "上游返回了未描述的错误"
		}
	}
	return parsed
}

// ---------------------------------------------------------------------------
// 工具调用跨帧累加（移植 trae-workbuddy-switch ce0a2c4 / issue #172）
// ---------------------------------------------------------------------------

// toolCallSlot 一条上游工具调用在累加器里的槽位。
type toolCallSlot struct {
	id        string
	name      string
	arguments string
	announced bool
}

// absorbedTool 一条上游调用在本帧**新增**的字段（已去重）。
type absorbedTool struct {
	slot      int
	id        *string
	name      string
	arguments string
}

// toolCallAccumulator 按上游 index / id 定位槽位，再把每帧新增的片段下发。
// SOLO 的 tool_calls 是逐帧增量：首帧给 id+name，后续帧只给 arguments；
// 每帧独立转换会把一次调用拆成「有名无参」+「无名有参」两条残缺记录。
type toolCallAccumulator struct {
	slots      []*toolCallSlot
	byUpstream map[int]int
	byID       map[string]int
	last       int
	hasLast    bool
}

func newToolCallAccumulator() *toolCallAccumulator {
	return &toolCallAccumulator{
		byUpstream: map[int]int{},
		byID:       map[string]int{},
		last:       -1,
	}
}

func (a *toolCallAccumulator) pushSlot() int {
	a.slots = append(a.slots, &toolCallSlot{})
	return len(a.slots) - 1
}

// slotOf 定位一条上游调用该落在哪个槽位。
func (a *toolCallAccumulator) slotOf(call map[string]any) int {
	if idx, ok := jsonInt(call["index"]); ok {
		slot, seen := a.byUpstream[idx]
		if !seen {
			slot = a.pushSlot()
			a.byUpstream[idx] = slot
		}
		a.last = slot
		a.hasLast = true
		return slot
	}
	id, _ := call["id"].(string)
	if id != "" {
		if slot, seen := a.byID[id]; seen {
			a.last = slot
			a.hasLast = true
			return slot
		}
	} else if a.hasLast {
		return a.last
	}
	slot := a.pushSlot()
	a.last = slot
	a.hasLast = true
	return slot
}

// absorb 吸收一帧里的一条上游调用，返回本帧真正新增的字段。
func (a *toolCallAccumulator) absorb(call map[string]any) absorbedTool {
	slotIdx := a.slotOf(call)
	slot := a.slots[slotIdx]
	function, _ := call["function"].(map[string]any)
	if function == nil {
		function, _ = call["function_call"].(map[string]any)
	}
	out := absorbedTool{slot: slotIdx}

	if id, _ := call["id"].(string); id != "" && slot.id == "" {
		slot.id = id
		a.byID[id] = slotIdx
		out.id = &id
	}

	if function != nil {
		if name, _ := function["name"].(string); name != "" {
			known := slot.name
			switch {
			case strings.HasPrefix(name, known):
				out.name = name[len(known):]
				slot.name = name
			case !strings.HasPrefix(known, name):
				out.name = name
				slot.name += name
			}
		}
		if args := toolArgumentsText(function["arguments"]); args != "" && slot.arguments != args {
			slot.arguments += args
			out.arguments = args
		}
	}
	return out
}

// absorbAll 非流式：吸收一帧全部调用（收尾时统一取 merged）。
func (a *toolCallAccumulator) absorbAll(raw json.RawMessage) {
	var calls []any
	if json.Unmarshal(raw, &calls) != nil {
		return
	}
	for _, item := range calls {
		if call, ok := item.(map[string]any); ok {
			a.absorb(call)
		}
	}
}

// hasCalls 是否真的产出过调用（空帧不算）。
func (a *toolCallAccumulator) hasCalls() bool {
	for _, slot := range a.slots {
		if slot.id != "" || slot.name != "" || slot.arguments != "" {
			return true
		}
	}
	return false
}

// streamingDeltas 把一帧上游 tool_calls 转成 OpenAI delta.tool_calls 分片。
// 每个槽位只在首帧带 id/type，之后只带 function 的新增部分。
func (a *toolCallAccumulator) streamingDeltas(raw json.RawMessage) []any {
	var calls []any
	if json.Unmarshal(raw, &calls) != nil {
		return nil
	}
	entries := make([]any, 0, len(calls))
	for _, item := range calls {
		call, ok := item.(map[string]any)
		if !ok {
			continue
		}
		absorbed := a.absorb(call)
		slot := a.slots[absorbed.slot]
		hasPayload := absorbed.id != nil || absorbed.name != "" || absorbed.arguments != ""
		if slot.announced && !hasPayload {
			continue
		}
		entry := map[string]any{"index": absorbed.slot}
		if !slot.announced {
			slot.announced = true
			entry["type"] = "function"
			id := slot.id
			if id == "" {
				// Anthropic 的 tool_use 块没有 id 就无法回填结果。
				id = fmt.Sprintf("call_%d", absorbed.slot)
			}
			entry["id"] = id
		} else if absorbed.id != nil {
			entry["id"] = *absorbed.id
		}
		function := map[string]any{}
		if absorbed.name != "" {
			function["name"] = absorbed.name
		}
		if absorbed.arguments != "" {
			function["arguments"] = absorbed.arguments
		}
		if len(function) > 0 {
			entry["function"] = function
		}
		entries = append(entries, entry)
	}
	return entries
}

// merged 非流式：拼成完整的 message.tool_calls（每条都是完整调用，不带 index）。
func (a *toolCallAccumulator) merged() []any {
	out := make([]any, 0, len(a.slots))
	for idx, slot := range a.slots {
		if slot.id == "" && slot.name == "" && slot.arguments == "" {
			continue
		}
		id := slot.id
		if id == "" {
			id = fmt.Sprintf("call_%d", idx)
		}
		out = append(out, map[string]any{
			"id":   id,
			"type": "function",
			"function": map[string]any{
				"name":      slot.name,
				"arguments": slot.arguments,
			},
		})
	}
	return out
}

// promoteToolFinish 有工具调用时 finish_reason 必须是 tool_calls：上游 done
// 固定报 stop，Anthropic 侧会映射成 end_turn，客户端据此丢弃工具结果。
// length / content_filter 等其它原因不覆盖。
func promoteToolFinish(finish string, hasCalls bool) string {
	if hasCalls && (finish == "" || finish == "stop") {
		return "tool_calls"
	}
	return finish
}

// jsonInt JSON 数字转 int（仅接受整数值）。
func jsonInt(value any) (int, bool) {
	switch v := value.(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return int(n), true
		}
	}
	return 0, false
}

// toolArgumentsText 参数统一成字符串再累加（上游偶尔直接给对象）。
func toolArgumentsText(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	default:
		out, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(out)
	}
}

// ConvertToolCalls 把 SOLO 的 tool_calls 转成 OpenAI 形态：
// function_call → function，并去掉上游私有字段（namespace / partial_arguments
// 是上游内部的流式增量，OpenAI 客户端不认识）。
func ConvertToolCalls(raw json.RawMessage) []any {
	var calls []any
	if err := json.Unmarshal(raw, &calls); err != nil {
		return nil
	}
	out := make([]any, 0, len(calls))
	for _, item := range calls {
		call, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if function, exists := call["function_call"]; exists {
			delete(call, "function_call")
			call["function"] = function
		}
		if function, ok := call["function"].(map[string]any); ok {
			clean := make(map[string]any, len(function))
			for key, value := range function {
				if key == "namespace" || key == "partial_arguments" {
					continue
				}
				clean[key] = value
			}
			call["function"] = clean
		}
		out = append(out, call)
	}
	return out
}

// ChunkLine 构造一个 OpenAI chat.completion.chunk 的 data: 行。
// usage 为 nil 或全 0 时不外发（否则客户端会显示 0 token）。
func ChunkLine(chatID, model string, delta map[string]any, finishReason string, usage *TokenUsage) string {
	choice := map[string]any{
		"index": 0,
		"delta": delta,
	}
	if finishReason != "" {
		choice["finish_reason"] = finishReason
	}
	chunk := map[string]any{
		"id":      chatID,
		"object":  "chat.completion.chunk",
		"created": nowUnix(),
		"model":   model,
		"choices": []any{choice},
	}
	if usage != nil && usage.IsMeaningful() {
		chunk["usage"] = usage.ToOpenAI()
	}
	line, err := json.Marshal(chunk)
	if err != nil {
		return "data: {}\n\n"
	}
	return fmt.Sprintf("data: %s\n\n", line)
}

// DoneLine 是 data: [DONE]\n\n。无论上游正常结束、中途断流还是报错，
// 都必须补发它——客户端（尤其 VS Code 系插件）会一直等到它为止。
func DoneLine() string {
	return "data: [DONE]\n\n"
}

// ErrorLine 构造 OpenAI 形态的错误事件行。
func ErrorLine(code int64, message string) string {
	payload := map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    "api_error",
			"code":    code,
		},
	}
	line, _ := json.Marshal(payload)
	return fmt.Sprintf("data: %s\n\n", line)
}

// ErrorBody OpenAI 形态的错误响应体（非流式）。
func ErrorBody(code, message string) map[string]any {
	return map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    "api_error",
			"code":    code,
		},
	}
}

func nowUnix() int64 {
	return time.Now().Unix()
}

// UpstreamError 上游在流内报错的 (code, message)。
type UpstreamError struct {
	Code    int64
	Message string
}

func (e *UpstreamError) Error() string {
	return fmt.Sprintf("上游错误 %d: %s", e.Code, e.Message)
}

// StreamOutcome 一次流式转换的结果。
type StreamOutcome struct {
	// Error 上游报错（含中途断流，code=0）。
	Error *UpstreamError
	// Usage 实测用量（用于账本与日志）。
	Usage TokenUsage
	// SawDone 是否见过结束标记。
	SawDone bool
}

// StreamConvert 把上游响应体流式转换为 OpenAI SSE，逐行送入 onChunk。
//
// 总是以 data: [DONE] 收尾（除非 onChunk 返回错误表示客户端已断开），
// 保证客户端不会悬挂。token_usage 事件先记录、挂到之后的 chunk 上
// （usage 通常在正文之后才到，done chunk 一定带得上）。
func StreamConvert(reader io.Reader, chatID, model string, onChunk func(line string) error) StreamOutcome {
	parser := &SoloParser{}
	outcome := StreamOutcome{}
	var pendingUsage *TokenUsage
	// db4754a：metadata 的 cache pool 三态（后到覆盖先到）。
	var cachePool *bool
	// issue #172：工具调用必须跨帧累加，按帧各自输出会被拆成残缺条目。
	tools := newToolCallAccumulator()

	emit := func(event SoloEvent) bool {
		if event.CachePoolAssigned != nil {
			cachePool = event.CachePoolAssigned
		}
		switch event.Event {
		case "output", "thought":
			delta := map[string]any{}
			if event.Response != "" {
				delta["content"] = event.Response
			}
			if event.Reasoning != "" {
				delta["reasoning_content"] = event.Reasoning
			}
			if len(event.ToolCalls) > 0 {
				if converted := tools.streamingDeltas(event.ToolCalls); len(converted) > 0 {
					delta["tool_calls"] = converted
				}
			}
			if len(delta) == 0 {
				return true
			}
			return onChunk(ChunkLine(chatID, model, delta, "", pendingUsage)) == nil
		case "token_usage":
			if event.Usage != nil {
				merged := mergeTokenUsage(pendingUsage, *event.Usage, cachePool)
				pendingUsage = &merged
				outcome.Usage = merged
			}
			return true
		case "done", "turn_completion":
			finish := event.FinishReason
			if finish == "" {
				finish = "stop"
			}
			// issue #172：上游固定报 stop，有工具调用时必须改口成 tool_calls。
			finish = promoteToolFinish(finish, tools.hasCalls())
			if onChunk(ChunkLine(chatID, model, map[string]any{}, finish, pendingUsage)) != nil {
				return false
			}
			outcome.SawDone = true
			return onChunk(DoneLine()) == nil
		case "error":
			code := int64(0)
			if event.ErrorCode != nil {
				code = *event.ErrorCode
			}
			outcome.Error = &UpstreamError{Code: code, Message: event.ErrorMessage}
			if onChunk(ErrorLine(code, event.ErrorMessage)) != nil {
				return false
			}
			outcome.SawDone = true
			return onChunk(DoneLine()) == nil
		}
		// metadata / history / timing_cost 等事件对 OpenAI 客户端无意义，忽略。
		return true
	}

	buf := make([]byte, 32*1024)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			for _, event := range parser.Feed(buf[:n]) {
				if !emit(event) {
					return outcome
				}
			}
		}
		if err != nil {
			if err != io.EOF && outcome.Error == nil {
				// 上游中途断流：不是账号问题，但调用方仍需补发收尾（见下方 SawDone 逻辑）。
				outcome.Error = &UpstreamError{Code: 0, Message: fmt.Sprintf("上游连接中断：%v", err)}
			}
			break
		}
	}

	for _, event := range parser.Finish() {
		if !emit(event) {
			return outcome
		}
	}

	if !outcome.SawDone {
		_ = onChunk(DoneLine())
		outcome.SawDone = true
	}
	return outcome
}

// Aggregate 非流式聚合：读完整条事件流，拼成单个 chat.completion 响应体。
// 上游只支持流式，stream:false 的客户端请求由本函数在本地聚合。
// 上游报错时第一个返回值为 nil。
func Aggregate(reader io.Reader, chatID, model string) (map[string]any, *UpstreamError, TokenUsage) {
	parser := &SoloParser{}
	var content, reasoning strings.Builder
	finishReason := "stop"
	var usage TokenUsage
	var cachePool *bool
	var upstreamErr *UpstreamError
	// issue #172：非流式同样跨帧累加后再输出完整调用。
	tools := newToolCallAccumulator()

	consume := func(event SoloEvent) {
		if event.CachePoolAssigned != nil {
			cachePool = event.CachePoolAssigned
		}
		switch event.Event {
		case "output", "thought":
			content.WriteString(event.Response)
			reasoning.WriteString(event.Reasoning)
			if len(event.ToolCalls) > 0 {
				tools.absorbAll(event.ToolCalls)
			}
		case "token_usage":
			if event.Usage != nil {
				usage = mergeTokenUsage(&usage, *event.Usage, cachePool)
			}
		case "done", "turn_completion":
			if event.FinishReason != "" {
				finishReason = event.FinishReason
			}
		case "error":
			code := int64(0)
			if event.ErrorCode != nil {
				code = *event.ErrorCode
			}
			upstreamErr = &UpstreamError{Code: code, Message: event.ErrorMessage}
		}
	}

	buf := make([]byte, 32*1024)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			for _, event := range parser.Feed(buf[:n]) {
				consume(event)
			}
		}
		if err != nil {
			if err != io.EOF && upstreamErr == nil {
				upstreamErr = &UpstreamError{Code: 0, Message: fmt.Sprintf("上游连接中断：%v", err)}
			}
			break
		}
	}
	for _, event := range parser.Finish() {
		consume(event)
	}

	if upstreamErr != nil {
		return nil, upstreamErr, usage
	}

	toolCalls := tools.merged()
	finishReason = promoteToolFinish(finishReason, len(toolCalls) > 0)

	message := map[string]any{
		"role":    "assistant",
		"content": content.String(),
	}
	if reasoning.Len() > 0 {
		message["reasoning_content"] = reasoning.String()
	}
	if len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
	}

	response := map[string]any{
		"id":      chatID,
		"object":  "chat.completion",
		"created": nowUnix(),
		"model":   model,
		"choices": []any{map[string]any{
			"index":         0,
			"message":       message,
			"finish_reason": finishReason,
		}},
	}
	if usage.IsMeaningful() {
		response["usage"] = usage.ToOpenAI()
	}
	return response, nil, usage
}
