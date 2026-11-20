// empty_content.go 出站请求体的两类「必然失败」形态修复（panel f987bac / a798364）：
//
//  1. 空 text 段 → 上游 400 / code 11151 "a message has empty content"；
//  2. Kimi/GLM 等按 `reasoning` 读历史的模型，客户端只发 `reasoning_content` 时
//     上游看到「assistant 没思考」，多轮一致性校验退化。
//
// 两条都是「同 body 换任何账号都必然失败」的形态：不在这里修，多号池只会把同一个
// 坏请求在上游撞满一轮，最终报成「账号都挂了」。
package workbuddy

import (
	"encoding/json"
	"strings"
)

// emptyTextPartFill 空 text 段的占位值。单个空格：上游只看长度是否为零（"   " 实测
// 放行），且该段本来就没有内容可给模型看。
const emptyTextPartFill = " "

// guardEmptyTextParts 把 messages 里所有空 text 段就地补位。
//
// 上游的校验口径（2026-11-08 实测）：
//
//	[{"type":"text","text":""}]        -> 400 / 11151
//	[{"type":"text","text":null}]      -> 400 / 11151
//	[{"type":"text"}]                  -> 400 / 11151
//	[{"type":"text","text":"   "}]     -> 200（只判长度是否为零，不 trim）
//	[{"type":"textarea","text":""}]    -> 200（未识别的 type 被忽略）
//
// 还有一条更隐蔽的规则：字符串 content 去空白后以 `[` 开头时，上游会当 parts 数组
// 解析——工具结果里抓到的表单/按钮数组正是这个形状，于是一段数据被嗅探成空文本段。
// 这里按同一口径嗅探、补值、重新序列化。
//
// 修法是**只补值、不删段**：删段会改段落数量与下标（客户端多模态顺序、工具结果
// 段序都可能被别处引用），而补一个空格对模型上下文没有语义影响。
func guardEmptyTextParts(messages []any) bool {
	if len(messages) == 0 {
		return false
	}
	changed := false
	for _, rawMsg := range messages {
		msg, ok := rawMsg.(map[string]any)
		if !ok {
			continue
		}
		content, has := msg["content"]
		if !has {
			continue
		}
		switch v := content.(type) {
		case []any:
			if fillEmptyTextParts(v) {
				changed = true
			}
		case string:
			parts := sniffParts(v)
			if parts == nil {
				continue
			}
			if !fillEmptyTextParts(parts) {
				continue
			}
			if nb, err := json.Marshal(parts); err == nil {
				msg["content"] = string(nb)
				changed = true
			}
		}
	}
	return changed
}

// sniffParts 复刻上游的嗅探口径：字符串去前导空白后以 `[` 开头、且能解析成
// 「非空的对象数组」时返回该数组，否则 nil（表示上游不会把它当 parts）。
func sniffParts(content string) []any {
	stripped := strings.TrimLeft(content, " \t\r\n")
	if !strings.HasPrefix(stripped, "[") {
		return nil
	}
	var parts []any
	if err := json.Unmarshal([]byte(stripped), &parts); err != nil {
		return nil
	}
	if len(parts) == 0 {
		return nil
	}
	for _, p := range parts {
		if _, ok := p.(map[string]any); !ok {
			return nil
		}
	}
	return parts
}

// fillEmptyTextParts 就地补位，返回是否有改动。
func fillEmptyTextParts(parts []any) bool {
	changed := false
	for _, rawPart := range parts {
		part, ok := rawPart.(map[string]any)
		if !ok || partTypeOf(part) != "text" {
			continue
		}
		if !isEmptyTextPart(part) {
			continue
		}
		part["text"] = emptyTextPartFill
		changed = true
	}
	return changed
}

// partTypeOf 取段类型。上游只对精确的 "text" 做校验，"textarea" 这类未知 type 被
// 忽略，所以这里**不做**前缀/大小写归一——归一反而会把不受校验的段拉进校验。
func partTypeOf(part map[string]any) string {
	t, _ := part["type"].(string)
	return t
}

// isEmptyTextPart 报告 text 段是否为空串/null/缺失。
func isEmptyTextPart(part map[string]any) bool {
	v, has := part["text"]
	if !has || v == nil {
		return true
	}
	s, ok := v.(string)
	return ok && s == ""
}

// upstreamReasoningModelPrefixes 上游按 `reasoning` 字段读历史的模型前缀（照抄源
// 实现，加新家族前先确认它确实读该字段）。
var upstreamReasoningModelPrefixes = []string{
	"kimi", "glm", "minimax", "mimo", "step", "space-bunny",
}

// readsUpstreamReasoning 该模型是否按 `reasoning` 字段读历史。
func readsUpstreamReasoning(model string) bool {
	lower := strings.ToLower(strings.TrimSpace(model))
	if lower == "" {
		return false
	}
	for _, p := range upstreamReasoningModelPrefixes {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}
	return false
}

// mirrorUpstreamReasoning 把这些模型的 assistant 历史里非空 reasoning_content 镜像
// 到空的 reasoning 字段；其余一律不动。
//
// 与 thinking.go 里 deepseek 那份「补齐」（含空值占位）纪律相反：这里只镜像已有的
// 非空值，绝不新建字段、绝不填占位——这些家族没有「必须有该字段」的实测依据。
// reasoning 已有非空字符串时不覆盖（上游自己写的更权威）。
func mirrorUpstreamReasoning(obj map[string]any) bool {
	model, _ := obj["model"].(string)
	if !readsUpstreamReasoning(model) {
		return false
	}
	msgs, ok := obj["messages"].([]any)
	if !ok || len(msgs) == 0 {
		return false
	}
	changed := false
	for _, rawMsg := range msgs {
		msg, ok := rawMsg.(map[string]any)
		if !ok {
			continue
		}
		if role, _ := msg["role"].(string); role != "assistant" {
			continue
		}
		rc, _ := msg["reasoning_content"].(string)
		if strings.TrimSpace(rc) == "" {
			continue
		}
		if r, ok := msg["reasoning"].(string); ok && strings.TrimSpace(r) != "" {
			continue
		}
		msg["reasoning"] = rc
		changed = true
	}
	return changed
}
