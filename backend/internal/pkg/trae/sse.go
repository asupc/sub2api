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
	Reasoning  uint64 `json:"-"`
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
	return TokenUsage{
		Prompt:     prompt,
		Completion: completion,
		Total:      total,
		Reasoning:  read("reasoning_tokens"),
	}
}

// ToOpenAI 转成 OpenAI usage 对象。reasoning_tokens 不是 OpenAI 标准字段，不外发。
func (u TokenUsage) ToOpenAI() map[string]any {
	return map[string]any{
		"prompt_tokens":     u.Prompt,
		"completion_tokens": u.Completion,
		"total_tokens":      u.Total,
	}
}

// IsMeaningful 是否拿到了有效数据（全 0 视为没拿到）。
func (u TokenUsage) IsMeaningful() bool {
	return u.Total > 0 || u.Prompt > 0 || u.Completion > 0
}

// SoloEvent 一个解析完成的 SOLO 事件。
type SoloEvent struct {
	Event        string
	Response     string
	Reasoning    string
	ToolCalls    json.RawMessage
	Usage        *TokenUsage
	FinishReason string
	ErrorCode    *int64
	ErrorMessage string
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

	emit := func(event SoloEvent) bool {
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
				if converted := ConvertToolCalls(event.ToolCalls); len(converted) > 0 {
					delta["tool_calls"] = converted
				}
			}
			if len(delta) == 0 {
				return true
			}
			return onChunk(ChunkLine(chatID, model, delta, "", pendingUsage)) == nil
		case "token_usage":
			if event.Usage != nil {
				pendingUsage = event.Usage
				outcome.Usage = *event.Usage
			}
			return true
		case "done", "turn_completion":
			finish := event.FinishReason
			if finish == "" {
				finish = "stop"
			}
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
	var upstreamErr *UpstreamError
	var toolCalls []any

	consume := func(event SoloEvent) {
		switch event.Event {
		case "output", "thought":
			content.WriteString(event.Response)
			reasoning.WriteString(event.Reasoning)
			if len(event.ToolCalls) > 0 {
				toolCalls = append(toolCalls, ConvertToolCalls(event.ToolCalls)...)
			}
		case "token_usage":
			if event.Usage != nil {
				usage = *event.Usage
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
