package qoder

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// 官方 baseprompt 模板（与桌面端一致的请求体骨架，qoder_proxy.py L244-251）。
//
//go:embed assets/baseprompt.json
var basepromptJSON []byte

var (
	basepromptOnce sync.Once
	basepromptBody map[string]any
	basepromptErr  error
)

// Baseprompt 返回模板的深拷贝（每次调用独立，可安全覆写）。
func Baseprompt() (map[string]any, error) {
	basepromptOnce.Do(func() {
		if err := json.Unmarshal(basepromptJSON, &basepromptBody); err != nil {
			basepromptErr = fmt.Errorf("qoder: parse baseprompt: %w", err)
		}
	})
	if basepromptErr != nil {
		return nil, basepromptErr
	}
	return deepCopyMap(basepromptBody), nil
}

// deepCopyMap 以 JSON 往返做深拷贝（对齐源实现 json.loads(json.dumps(...))）。
func deepCopyMap(m map[string]any) map[string]any {
	raw, _ := json.Marshal(m)
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return out
}

// ---------------------------------------------------------------------------
// Python 风格字符串工具（strip/lstrip 空白集与码点长度语义）
// ---------------------------------------------------------------------------

func pyStrip(s string) string  { return strings.Trim(s, pyWhitespace) }
func pyLStrip(s string) string { return strings.TrimLeft(s, pyWhitespace) }
func pyRStrip(s string) string { return strings.TrimRight(s, pyWhitespace) }
func pyLen(s string) int       { return utf8.RuneCountInString(s) }

// pySlice 按码点切片（Python s[a:b]，越界自动夹紧；负索引不支持，调用方先处理）。
func pySlice(s string, start, end int) string {
	r := []rune(s)
	n := len(r)
	if start < 0 {
		start = 0
	}
	if end > n {
		end = n
	}
	if start >= end {
		return ""
	}
	return string(r[start:end])
}

// ---------------------------------------------------------------------------
// 出站提示词脱敏（qoder_proxy.py L2296-2337：过滤常见客户端指纹串）
// ---------------------------------------------------------------------------

var sanitizeFeatures = []string{
	"x-anthropic-billing-header",
	"cc_entrypoint=",
	"You are Claude Code, Anthropic's official CLI for Claude",
	"You are a coding agent running in the Codex CLI",
	"github.com/anthropics/",
}

var sanitizeRewrites = [][2]string{
	{"You are Claude Code, Anthropic's official CLI for Claude",
		"You are Claude Code, Anthropic's official CLI tool for Claude"},
	{"You are a coding agent running in the Codex CLI, a terminal-based coding assistant.",
		"You are a coding agent running in the Codex CLI tool, a terminal-based coding assistant."},
}

var (
	sanitizeHdrRe     = regexp.MustCompile(`(?i)x-anthropic-billing-header:[^;\r\n]*;?\s*`)
	sanitizeBareHdrRe = regexp.MustCompile(`(?i)x-anthropic-billing-header`)
	sanitizeKVRe      = regexp.MustCompile(`(?i)\bcc_[a-z0-9_]+=[^;\r\n]*;?\s*`)
)

// hasFingerprint 报告文本是否携带客户端指纹串。
func hasFingerprint(text string) bool {
	if text == "" {
		return false
	}
	for _, f := range sanitizeFeatures {
		if strings.Contains(text, f) {
			return true
		}
	}
	return sanitizeBareHdrRe.MatchString(text)
}

// SanitizeText 出站文本脱敏（sanitize_text L2323-2337）。
func SanitizeText(text string) string {
	if text == "" || !hasFingerprint(text) {
		return text
	}
	for _, rw := range sanitizeRewrites {
		text = strings.ReplaceAll(text, rw[0], rw[1])
	}
	text = sanitizeHdrRe.ReplaceAllString(text, "")
	if strings.Contains(text, "cc_") {
		for {
			prev := text
			text = sanitizeKVRe.ReplaceAllString(text, "")
			if prev == text {
				break
			}
		}
	}
	text = sanitizeBareHdrRe.ReplaceAllString(text, "x-anthropic-billing-hdr")
	return pyStrip(text)
}

// sanitizeContentValue 对消息 content（字符串或 parts 列表）脱敏，返回处理
// 后的新值；非字符串/列表原样返回。
func sanitizeContentValue(content any) any {
	switch v := content.(type) {
	case string:
		return SanitizeText(v)
	case []any:
		out := make([]any, 0, len(v))
		for _, part := range v {
			if pm, ok := part.(map[string]any); ok && pm["type"] == "text" {
				if _, has := pm["text"]; has {
					p2 := copyShallow(pm)
					if ts, ok := pm["text"].(string); ok {
						p2["text"] = SanitizeText(ts)
					}
					out = append(out, p2)
					continue
				}
			}
			out = append(out, part)
		}
		return out
	default:
		return content
	}
}

func copyShallow(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// sanitizeMessages 对整个会话脱敏（sanitize_messages L2375-2389；
// content / reasoning_content / tool_calls.function.arguments）。
func sanitizeMessages(messages []any) []any {
	out := make([]any, 0, len(messages))
	for _, m := range messages {
		mm, ok := m.(map[string]any)
		if !ok {
			out = append(out, m)
			continue
		}
		item := copyShallow(mm)
		if _, has := item["content"]; has {
			item["content"] = sanitizeContentValue(item["content"])
		}
		if rs, ok := item["reasoning_content"].(string); ok {
			item["reasoning_content"] = SanitizeText(rs)
		}
		if tcs, has := item["tool_calls"].([]any); has {
			out2 := make([]any, 0, len(tcs))
			for _, tc := range tcs {
				if tm, ok := tc.(map[string]any); ok {
					t2 := copyShallow(tm)
					if fn, ok := tm["function"].(map[string]any); ok {
						if args, ok := fn["arguments"].(string); ok {
							fn2 := copyShallow(fn)
							fn2["arguments"] = SanitizeText(args)
							t2["function"] = fn2
						}
					}
					out2 = append(out2, t2)
				} else {
					out2 = append(out2, tc)
				}
			}
			item["tool_calls"] = out2
		}
		out = append(out, item)
	}
	return out
}

// ---------------------------------------------------------------------------
// 消息压平（flatten_messages L3435-3534）
// ---------------------------------------------------------------------------

// flattenContentText 把 OpenAI content（str 或 parts 列表）压成纯文本；
// 图片另行收集（_flatten_content_text L3248-3275）。
func flattenContentText(content any) (string, []string) {
	switch v := content.(type) {
	case nil:
		return "", nil
	case string:
		return v, nil
	case []any:
		texts, images := []string{}, []string{}
		for _, piece := range v {
			switch p := piece.(type) {
			case string:
				texts = append(texts, p)
			case map[string]any:
				ptype, _ := p["type"].(string)
				switch {
				case ptype == "text" || ptype == "input_text" || ptype == "output_text" || ptype == "summary_text":
					t, _ := p["text"].(string)
					texts = append(texts, t)
				case ptype == "image_url" || ptype == "image" || ptype == "input_image" || hasKey(p, "image_url"):
					var u any
					if p["image_url"] != nil {
						u = p["image_url"]
					} else if p["url"] != nil {
						u = p["url"]
					}
					if um, ok := u.(map[string]any); ok {
						u = um["url"]
					}
					if u == nil && p["data"] != nil {
						mime := "image/png"
						if m, ok := p["mimeType"].(string); ok && m != "" {
							mime = m
						} else if m, ok := p["mime_type"].(string); ok && m != "" {
							mime = m
						}
						if d, ok := p["data"].(string); ok {
							u = "data:" + mime + ";base64," + d
						}
					}
					if us, ok := u.(string); ok && us != "" {
						images = append(images, us)
					}
				}
			}
		}
		nonEmpty := make([]string, 0, len(texts))
		for _, t := range texts {
			if t != "" {
				nonEmpty = append(nonEmpty, t)
			}
		}
		return strings.Join(nonEmpty, "\n"), images
	default:
		return fmt.Sprintf("%v", content), nil
	}
}

func hasKey(m map[string]any, k string) bool {
	_, ok := m[k]
	return ok
}

// 工具结果文本化标记（issue #8 / #11；LEAK_MARKER 序列化 assistant 工具调用、
// TOOL_RESULT_MARKER 序列化 tool 结果，泄漏回读在 sse.go 中按这些标记守卫）。
const (
	LeakMarker       = "[assistant 请求调用工具]"
	ToolResultMarker = "[工具结果"
	ToolResultClose  = "[工具结果结束]"
)

// ToolResultKeepChars 历史工具结果暴露面削减的单侧保留字符数
// （TOOL_RESULT_KEEP_DEFAULT；0 = 完全关闭削减、回退全文回灌）。
// 默认 2000，与源实现一致；集成方可按需调整。
var ToolResultKeepChars = 2000

// toolResultShrinkMargin 削后至少要省这么多字符，才值得加省略标记。
const toolResultShrinkMargin = 200

// currentTurnStart 「当前轮」起始下标（索引 >= 它的 tool 结果不削减，
// _current_turn_start L3348-3361）：从末尾往前找最后一条 user/system/developer
// 消息；找不到返回 0（宁可不削）。
func currentTurnStart(messages []any) int {
	for i := len(messages) - 1; i >= 0; i-- {
		if m, ok := messages[i].(map[string]any); ok {
			role, _ := m["role"].(string)
			if role == "user" || role == "system" || role == "developer" {
				return i + 1
			}
		}
	}
	return 0
}

// isCompleteJSON 文本是否为完整 JSON 对象/数组字面量（_is_complete_json）。
func isCompleteJSON(text string) bool {
	s := pyStrip(text)
	if s == "" || (s[0] != '{' && s[0] != '[') {
		return false
	}
	return json.Valid([]byte(s))
}

// shrinkToolResult 历史工具结果：首尾各保留 keep 字符（码点语义），
// 中间换成明确的省略标记；完整 JSON 额外宽限到 2*keep
// （_shrink_tool_result L3319-3345）。返回原文本表示不值得削。
func shrinkToolResult(text string, keep int) string {
	if keep <= 0 || pyLen(text) <= keep*2+toolResultShrinkMargin {
		return text
	}
	if isCompleteJSON(text) && pyLen(text) <= keep*4 {
		return text
	}
	head := pySlice(text, 0, keep)
	if nl := strings.LastIndex(head, "\n"); nl > keep/2 {
		head = head[:nl]
	}
	tail := pySlice(text, pyLen(text)-keep, pyLen(text))
	if nl := strings.Index(tail, "\n"); nl != -1 && nl < keep/2 {
		tail = tail[nl+1:]
	}
	omitted := pyLen(text) - pyLen(head) - pyLen(tail)
	marker := fmt.Sprintf("\n[[qoder-proxy: 已省略 %d 字符历史工具结果；需要完整内容请重新执行该命令]]\n", omitted)
	return head + marker + tail
}

// flattenResult 压平输出。
type flattenResult struct {
	systemText *string
	messages   []map[string]any
	images     []string
}

// flattenMessages 把客户端会话压平成 Qoder 上游可接受的 {role, content} 序列
// （flatten_messages L3435-3534，文本化路径——sub2api 网关已统一中间格式，
// 结构化直传分支见 structured 参数，默认关闭走 fail-safe 文本化）：
//   - 首个 system/developer 消息抽为 systemText（模板 system 的替换源）；
//   - user/assistant 原位保留（content 压平为字符串）；
//   - tool 结果降级为 user 文本（上游历史上不认识 tool 角色），带
//     TOOL_RESULT_MARKER 信封；历史（当前轮之前）按 ToolResultKeepChars 削减；
//   - assistant 的 tool_calls 序列化进 content（LEAK_MARKER + JSON 数组）；
//   - assistant 的 reasoning_content 仅在 DeepSeek 族保留；
//   - 未知角色降级为 user，不静默丢弃。
func flattenMessages(messages []any, keepReasoning, structured bool) flattenResult {
	var systemText *string
	flat := []map[string]any{}
	images := []string{}
	keep := ToolResultKeepChars
	curStart := len(messages)
	if keep > 0 {
		curStart = currentTurnStart(messages)
	}
	for idx, m := range messages {
		mm, ok := m.(map[string]any)
		if !ok {
			continue
		}
		role, _ := mm["role"].(string)
		text, imgs := flattenContentText(mm["content"])
		text = SanitizeText(text)
		images = append(images, imgs...)
		switch role {
		case "system", "developer":
			if systemText == nil && text != "" {
				t := text
				systemText = &t
			}
		case "tool":
			name, _ := mm["name"].(string)
			label := ""
			if name != "" {
				label = " (" + name + ")"
			}
			body := text
			if keep > 0 && idx < curStart {
				body = shrinkToolResult(body, keep)
			}
			if structured {
				tcID, _ := mm["tool_call_id"].(string)
				flat = append(flat, map[string]any{
					"role": "tool", "tool_call_id": tcID, "content": body})
			} else {
				// 信封前缀逐字节不变（TOOL_RESULT_MARKER + name + "]\n"）
				flat = append(flat, map[string]any{
					"role": "user", "content": ToolResultMarker + label + "]\n" + body})
			}
		case "assistant":
			if tcs, has := mm["tool_calls"].([]any); has && len(tcs) > 0 {
				if structured {
					calls := []map[string]any{}
					for _, tc := range tcs {
						tm, _ := tc.(map[string]any)
						if tm == nil {
							tm = map[string]any{}
						}
						fn, _ := tm["function"].(map[string]any)
						if fn == nil {
							fn = map[string]any{}
						}
						args := fn["arguments"]
						switch a := args.(type) {
						case map[string]any, []any:
							args = marshalNoEscape(a)
						case string:
						case nil:
							args = ""
						default:
							args = fmt.Sprintf("%v", a)
						}
						fnName, _ := fn["name"].(string)
						id, _ := tm["id"].(string)
						ttype, _ := tm["type"].(string)
						if ttype == "" {
							ttype = "function"
						}
						calls = append(calls, map[string]any{
							"id": id, "type": ttype,
							"function": map[string]any{"name": fnName, "arguments": args}})
					}
					// content 恒为空串而非 null（task-31 实测：null 会让上游
					// 转换层丢掉配对的 assistant 消息，DeepSeek/Kimi 直接拒绝；
					// Go 侧 flattenContentText 永不返回 nil，此处天然满足）。
					item := map[string]any{"role": "assistant", "content": text, "tool_calls": calls}
					if keepReasoning && mm["reasoning_content"] != nil {
						rc, _ := mm["reasoning_content"].(string)
						item["reasoning_content"] = rc
					}
					flat = append(flat, item)
					continue
				}
				calls := []map[string]any{}
				for _, tc := range tcs {
					tm, _ := tc.(map[string]any)
					fn, _ := tm["function"].(map[string]any)
					if fn == nil {
						fn = map[string]any{}
					}
					name, _ := fn["name"].(string)
					args, _ := fn["arguments"].(string)
					calls = append(calls, map[string]any{"name": name, "arguments": args})
				}
				text = text + "\n\n" + LeakMarker + "\n" + marshalNoEscape(calls)
			}
			item := map[string]any{"role": "assistant", "content": text}
			if keepReasoning && mm["reasoning_content"] != nil {
				rc, _ := mm["reasoning_content"].(string)
				item["reasoning_content"] = rc
			}
			flat = append(flat, item)
		case "user":
			flat = append(flat, map[string]any{"role": "user", "content": text})
		default:
			if role != "" {
				flat = append(flat, map[string]any{"role": "user", "content": text})
			}
		}
	}
	return flattenResult{systemText: systemText, messages: flat, images: images}
}

// marshalNoEscape 关闭 HTML 转义的 JSON 序列化（对齐 Python
// json.dumps(..., ensure_ascii=False)）。
func marshalNoEscape(v any) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return ""
	}
	return strings.TrimSuffix(buf.String(), "\n")
}

// normalizeRoles 把上游不认识的角色映射为等价角色
// （normalize_roles L2274-2290："developer" -> "system"）。
func normalizeRoles(messages []any) []any {
	out := make([]any, 0, len(messages))
	for _, m := range messages {
		if mm, ok := m.(map[string]any); ok {
			if role, _ := mm["role"].(string); role == "developer" {
				item := copyShallow(mm)
				item["role"] = "system"
				out = append(out, item)
				continue
			}
		}
		out = append(out, m)
	}
	return out
}

// deepseekModelKeys DeepSeek 上游 model key（qoder_proxy.py L257）。
var deepseekModelKeys = map[string]bool{"dmodel": true, "dfmodel": true}

// ---------------------------------------------------------------------------
// 结构化工具历史直传（qoder_proxy.py task-31/32，L3364-3432）
// ---------------------------------------------------------------------------

// structuredToolHistoryWhitelist 结构化直传的模型名前缀白名单
// （STRUCTURED_TOOL_HISTORY_WHITELIST L3371；前缀匹配、大小写不敏感）。
var structuredToolHistoryWhitelist = []string{"qwen", "glm", "gm4", "gm5"}

// structuredToolHistoryEnv 结构化直传开关的三态环境变量名（L3377）。
const structuredToolHistoryEnv = "QD_STRUCTURED_TOOL_HISTORY"

// structuredEnvMode 解析三态：off / on / auto（缺省；非法值静默回落 auto，
// 不误开——_structured_mode L3375-3384）。
func structuredEnvMode() string {
	raw := strings.ToLower(pyStrip(os.Getenv(structuredToolHistoryEnv)))
	switch raw {
	case "":
		return "auto"
	case "off", "0", "false", "no", "disable", "disabled":
		return "off"
	case "on", "1", "true", "yes", "enable", "enabled", "force":
		return "on"
	default:
		return "auto"
	}
}

// toolIDsOK 结构化前提：每条 tool 有 tool_call_id，每个 assistant.tool_calls
// 条目有 id（_tool_ids_ok L3387-3401；非 dict 消息跳过）。
func toolIDsOK(messages []any) bool {
	for _, m := range messages {
		mm, ok := m.(map[string]any)
		if !ok {
			continue
		}
		switch role, _ := mm["role"].(string); role {
		case "tool":
			if id, _ := mm["tool_call_id"].(string); pyStrip(id) == "" {
				return false
			}
		case "assistant":
			tcs, _ := mm["tool_calls"].([]any)
			for _, tc := range tcs {
				tm, ok := tc.(map[string]any)
				if !ok {
					return false
				}
				if id, _ := tm["id"].(string); pyStrip(id) == "" {
					return false
				}
			}
		}
	}
	return true
}

// StructuredToolHistoryEnabled 本次请求是否走结构化工具历史直传
// （structured_tool_history_enabled L3404-3432）。判定顺序不可换：
//  1. toolIDsOK 不满足 → false（任何模式都不可覆盖，fail-safe 回退文本化）；
//  2. off → false（一键回退）；
//  3. on → true（显式强制：DeepSeek / Kimi / INTL 补验证用）；
//  4. auto：realm 必须为 cn（INTL 未实测，故 auto 不启用）；
//  5. auto：候选 (modelKey, model) 依次小写前缀命中白名单 → true。
func StructuredToolHistoryEnabled(realm, model, modelKey string, messages []any) bool {
	if !toolIDsOK(messages) {
		return false
	}
	mode := structuredEnvMode()
	if mode == "off" {
		return false
	}
	if mode == "on" {
		return true
	}
	if realm != "cn" {
		return false
	}
	for _, cand := range []string{modelKey, model} {
		low := strings.ToLower(pyStrip(cand))
		if low == "" {
			continue
		}
		for _, prefix := range structuredToolHistoryWhitelist {
			if strings.HasPrefix(low, prefix) {
				return true
			}
		}
	}
	return false
}

// StructuredToolHistoryEnabledForRequest 是 BuildAgentChatBody 内部门控的
// 外部等价视图：以同一套规范化链（normalizeRoles → sanitizeMessages →
// backfillReasoningContent）判定，供调用方按同口径打观测日志。
func StructuredToolHistoryEnabledForRequest(openAIReq map[string]any, realm string) bool {
	model, _ := openAIReq["model"].(string)
	modelKey := ResolveUpstreamKey(model, realm)
	messages := normalizeRoles(anyList(openAIReq["messages"]))
	messages = sanitizeMessages(messages)
	messages = backfillReasoningContent(messages, model, modelKey)
	return StructuredToolHistoryEnabled(realm, model, modelKey, messages)
}

// IsDeepSeekModel 判断请求最终是否打到 DeepSeek 上游（is_deepseek_model
// L2546-2564：同时看上游 key 与客户端写的名字）。
func IsDeepSeekModel(model, modelKey string) bool {
	key := strings.ToLower(strings.TrimSpace(modelKey))
	if deepseekModelKeys[key] {
		return true
	}
	low := strings.ToLower(strings.TrimSpace(model))
	if low == "" {
		return false
	}
	if deepseekModelKeys[low] || strings.HasPrefix(low, "deepseek") {
		return true
	}
	return strings.Contains(low, "deepseek")
}

// backfillReasoningContent DeepSeek 多轮一致性：assistant 历史补
// reasoning_content（backfill_reasoning_content L2567+：仅当会话里
// 已出现 reasoning 痕迹时补空串占位）。
func backfillReasoningContent(messages []any, model, modelKey string) []any {
	if !IsDeepSeekModel(model, modelKey) {
		return messages
	}
	hasTrace := false
	for _, m := range messages {
		if mm, ok := m.(map[string]any); ok {
			if mm["reasoning"] != nil {
				hasTrace = true
				break
			}
			if _, has := mm["reasoning_content"]; has {
				hasTrace = true
				break
			}
		}
	}
	if !hasTrace {
		return messages
	}
	out := make([]any, 0, len(messages))
	for _, m := range messages {
		mm, ok := m.(map[string]any)
		if ok && mm["role"] == "assistant" {
			item := copyShallow(mm)
			if _, has := item["reasoning_content"]; !has {
				if r := item["reasoning"]; r != nil {
					item["reasoning_content"] = fmt.Sprintf("%v", r)
				} else {
					item["reasoning_content"] = ""
				}
			}
			out = append(out, item)
			continue
		}
		out = append(out, m)
	}
	return out
}

// requestedEffort 请求侧的原始思考档位（requested_effort L2467-2482）：
// reasoning_effort -> reasoning.effort -> thinking.effort / thinking.level。
// 仅提取，不做归一化；空值返回 ""。
func requestedEffort(payload map[string]any) string {
	if v, ok := payload["reasoning_effort"].(string); ok && v != "" {
		return v
	}
	if r, ok := payload["reasoning"].(map[string]any); ok {
		if v, ok := r["effort"].(string); ok && v != "" {
			return v
		}
	}
	if t, ok := payload["thinking"].(map[string]any); ok {
		if v, ok := t["effort"].(string); ok && v != "" {
			return v
		}
		if v, ok := t["level"].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// findCatalogEntry 按上游 key 在两区静态快照里找目录元数据
// （build_qoder_body L3597-3607 的回落语义：先本区，再双区静态）。
func findCatalogEntry(realm, modelKey string) CatalogEntry {
	if realm != "" {
		if entries, err := StaticModels(realm); err == nil {
			for _, e := range entries {
				if e.Key == modelKey {
					return e
				}
			}
		}
	}
	for _, r := range []string{"cn", "intl"} {
		if entries, err := StaticModels(r); err == nil {
			for _, e := range entries {
				if e.Key == modelKey {
					return e
				}
			}
		}
	}
	return CatalogEntry{}
}

// maxInputTokensDefault 目录缺省上下文（build_qoder_body L3618）。
const maxInputTokensDefault = 180000

// BuildAgentChatBody 以官方 baseprompt.json 为骨架构造 agent_chat_generation
// 上游请求体并返回 qoder_encode 后的出站 body（对齐 qoder_proxy.py
// build_qoder_body L3537-3673 的覆写逻辑）：
//
//   - messages：压平 + 脱敏 + DeepSeek reasoning 回填，覆写模板会话；
//   - model_config / chat_context（高亮 prompt + 模型配置副本 + 图片）；
//   - parameters：max_tokens 与归一化后的 reasoning_effort；
//   - tools：客户端声明则透传，否则置空（不泄漏 Qoder 桌面端 agent 工具）；
//   - business 会话卡片与 request/session id。
//
// session 提供身份（aliyun_user_type）；模型 key 由 openAIReq["model"]
// 经 ResolveUpstreamKey 解析（带 realm，对齐 Python resolve_upstream_key(model,
// realm=realm)——不带区域会丢跨区显示名校验）；realm 供结构化工具历史门控
// 判断（cn/intl）与目录元数据查取。
func BuildAgentChatBody(openAIReq map[string]any, session *CosySession, realm string) (string, error) {
	body, err := Baseprompt()
	if err != nil {
		return "", err
	}
	model, _ := openAIReq["model"].(string)
	modelKey := ResolveUpstreamKey(model, realm)
	if session == nil {
		return "", fmt.Errorf("qoder: BuildAgentChatBody requires cosy session")
	}

	messages := normalizeRoles(anyList(openAIReq["messages"]))
	messages = sanitizeMessages(messages)
	messages = backfillReasoningContent(messages, model, modelKey)

	// 结构化工具历史直传（task-31/32 根治泄漏回声）：门控通过时 tool 消息与
	// assistant.tool_calls 结构化透传，不再产生 LeakMarker 文本；否则维持
	// fail-safe 文本化（qoder_proxy.py L3551-3563 的形态决策单点）。
	structured := StructuredToolHistoryEnabled(realm, model, modelKey, messages)
	res := flattenMessages(messages, IsDeepSeekModel(model, modelKey), structured)

	// 模板只保留 system 基座（自带示例 user 轮是样例内容，必须丢弃）
	if res.systemText != nil {
		body["messages"] = []any{map[string]any{"role": "system", "content": *res.systemText}}
	} else {
		tmplSystem := []any{}
		for _, m := range anyList(body["messages"]) {
			if mm, ok := m.(map[string]any); ok && mm["role"] == "system" {
				tmplSystem = append(tmplSystem, mm)
			}
		}
		body["messages"] = tmplSystem
	}
	msgs := anyList(body["messages"])
	for _, m := range res.messages {
		msgs = append(msgs, m)
	}
	body["messages"] = msgs

	// 最新用户提示词（上游 chat_context 高亮 + business 会话名）
	prompt := ""
	for i := len(res.messages) - 1; i >= 0; i-- {
		m := res.messages[i]
		if m["role"] == "user" {
			if c, ok := m["content"].(string); ok && c != "" {
				prompt = c
				break
			}
		}
	}

	nid := UUID4()
	body["request_id"] = nid
	body["chat_record_id"] = nid
	body["request_set_id"] = UUID4()
	body["session_id"] = UUID4()
	body["stream"] = true
	body["agent_id"] = "agent_common"
	aliyunUserType := session.UserType
	if aliyunUserType == "" {
		aliyunUserType = DefaultUserType
	}
	body["aliyun_user_type"] = aliyunUserType

	// model_config（目录元数据 + 动态 key）：先本区再双区（Python L3756-3765
	// 先 models_for_realm(r) 再双区静态——双区同 key 元数据不同，如 mmodel
	// intl=MiniMax-M3/is_vl=true、cn=MiniMax-M2.7/is_vl=false）。
	catalogMeta := findCatalogEntry(realm, modelKey)
	mc, _ := body["model_config"].(map[string]any)
	if mc == nil {
		mc = map[string]any{}
	}
	displayName := catalogMeta.DisplayName
	if displayName == "" {
		displayName = modelKey
	}
	maxInput := catalogMeta.MaxInputTokens
	if maxInput == 0 {
		maxInput = maxInputTokensDefault
	}
	mc["key"] = modelKey
	mc["display_name"] = displayName
	mc["model"] = ""
	mc["format"] = "openai"
	mc["is_vl"] = catalogMeta.IsVL
	mc["is_reasoning"] = catalogMeta.IsReasoning
	mc["api_key"] = ""
	mc["url"] = ""
	mc["source"] = "system"
	mc["max_input_tokens"] = maxInput
	body["model_config"] = mc

	// chat_context 高亮与模型配置副本
	cc, _ := body["chat_context"].(map[string]any)
	if cc == nil {
		cc = map[string]any{}
	}
	txt, _ := cc["text"].(map[string]any)
	if txt == nil {
		txt = map[string]any{}
	}
	txt["text"] = prompt
	cc["text"] = txt
	extra, _ := cc["extra"].(map[string]any)
	if extra == nil {
		extra = map[string]any{}
	}
	oc, _ := extra["originalContent"].(map[string]any)
	if oc == nil {
		oc = map[string]any{}
	}
	oc["text"] = prompt
	extra["originalContent"] = oc
	extra["modelConfig"] = copyShallow(mc)
	cc["extra"] = extra
	if len(res.images) > 0 {
		cc["imageUrls"] = res.images
		body["image_urls"] = res.images
	}
	body["chat_context"] = cc

	// parameters：max_tokens / reasoning_effort
	params, _ := body["parameters"].(map[string]any)
	if params == nil {
		params = map[string]any{}
	}
	maxTokens := firstNonZeroInt(openAIReq["max_tokens"], openAIReq["max_completion_tokens"])
	if maxTokens > 0 {
		params["max_tokens"] = maxTokens
	}
	if effort := requestedEffort(openAIReq); effort != "" {
		if norm, _ := catalogMeta.NormalizeReasoningEffort(effort); norm != "" {
			params["reasoning_effort"] = norm
		}
	}
	body["parameters"] = params

	// tools：客户端给了就用客户端的，否则置空
	if tools, ok := openAIReq["tools"].([]any); ok && len(tools) > 0 {
		body["tools"] = tools
	} else {
		body["tools"] = []any{}
	}

	// business 会话卡片
	biz, _ := body["business"].(map[string]any)
	if biz == nil {
		biz = map[string]any{}
	}
	biz["id"] = UUID4()
	biz["begin_at"] = time.Now().UnixMilli()
	name := "chat"
	if prompt != "" {
		name = pySlice(prompt, 0, 30)
	}
	biz["name"] = name
	setDefault(biz, "product", "cli")
	setDefault(biz, "type", "agent")
	setDefault(biz, "stage", "start")
	body["business"] = biz

	encoded := QoderEncode([]byte(marshalNoEscape(body)))
	return encoded, nil
}

func anyList(v any) []any {
	if l, ok := v.([]any); ok {
		return l
	}
	return nil
}

func firstNonZeroInt(values ...any) int64 {
	for _, v := range values {
		switch t := v.(type) {
		case float64:
			if t > 0 {
				return int64(t)
			}
		case int:
			if t > 0 {
				return int64(t)
			}
		case int64:
			if t > 0 {
				return t
			}
		case string:
			var n int64
			if _, err := fmt.Sscanf(t, "%d", &n); err == nil && n > 0 {
				return n
			}
		}
	}
	return 0
}

func setDefault(m map[string]any, key string, value any) {
	if _, ok := m[key]; !ok {
		m[key] = value
	}
}
