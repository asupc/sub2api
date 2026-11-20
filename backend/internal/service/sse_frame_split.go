package service

// sse_frame_split.go 二开：单个 SSE data 行里塞了多个 JSON 对象时拆分（panel
// afb5f22 / issue #197）。
//
// 按 SSE 规范，一个事件的 data 值就是 "data:" 之后到行尾的全部内容，客户端只能
// 把它当一份文本解析——第二个对象必然让整轮对话作废（zcode + glm-5.3-flash 实测
// 报 "Unexpected non-whitespace character after JSON"）。上游粘帧时由网关拆开
// 逐对象写出，每个对象仍是独立事件、独立 \n\n。
//
// 切分器按字节扫，跟踪字符串状态与 {}、[] 深度；字符串里的花括号和转义引号不算
// 结构；每段再单独校验 json.Valid —— 宁可少切，也不产出半截对象把客户端带偏。

import (
	"encoding/json"
	"strings"
)

// expandSSEDataLine 返回替换原行的一个或多个 SSE 行（元素之间插入空行分隔，
// 调用方逐行写出即可）。非 data 行、合法 JSON、单对象、拆不动时原样返回。
func expandSSEDataLine(line string) []string {
	payload, ok := extractOpenAISSEDataLine(line)
	if !ok {
		return []string{line}
	}
	trimmed := strings.TrimSpace(payload)
	if trimmed == "" || trimmed == "[DONE]" {
		return []string{line}
	}
	if json.Valid([]byte(trimmed)) {
		return []string{line}
	}
	parts, ok := splitConcatenatedJSON(trimmed)
	if !ok || len(parts) < 2 {
		return []string{line}
	}
	out := make([]string, 0, len(parts)*2)
	for i, part := range parts {
		if i > 0 {
			out = append(out, "") // 事件分隔空行
		}
		out = append(out, "data: "+part)
	}
	return out
}

// splitConcatenatedJSON 把 "{"a":1}{"b":2}" 切成两个对象；任一字节无法安全归属
// （字符串未闭合、括号不配平、段内不是合法 JSON）时返回 false。
func splitConcatenatedJSON(src string) ([]string, bool) {
	var parts []string
	depth := 0
	start := -1
	inString := false
	escaped := false
	for i := 0; i < len(src); i++ {
		ch := src[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			switch ch {
			case '\\':
				escaped = true
			case '"':
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '{', '[':
			if depth == 0 {
				start = i
			}
			depth++
		case '}', ']':
			if depth == 0 {
				return nil, false // 多余的闭括号
			}
			depth--
			if depth == 0 {
				if start < 0 {
					return nil, false
				}
				part := strings.TrimSpace(src[start : i+1])
				if !json.Valid([]byte(part)) {
					return nil, false
				}
				parts = append(parts, part)
				start = -1
			}
		default:
			if depth == 0 && !isJSONSpace(ch) {
				return nil, false // 顶层出现非空白非结构字符
			}
		}
	}
	if inString || depth != 0 || start >= 0 {
		return nil, false
	}
	return parts, len(parts) > 1
}

// isJSONSpace JSON 顶层允许的空白字节。
func isJSONSpace(ch byte) bool {
	return ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n'
}
