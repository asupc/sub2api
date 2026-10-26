package service

// openai_gateway_messages_images.go /v1/messages 入站图片的出站策略（移植自
// trae-workbuddy-switch 76a97c0 to_upstream_request_adaptive / forward_images_enabled）：
// WorkBuddy / Trae 上游是否接受 content 里的结构化 image_url parts 尚未实测，
// 默认**丢弃图片但留痕**——「静默丢图」会让模型与用户一起猜「为什么它看不见图」，
// 是最难排查的一类故障；丢弃时在 user 文本里写明「已移除 N 张图片」。需要带图时
// 设 SO_ANTHROPIC_FORWARD_IMAGES=1（on/true/yes 同效）打开转发。
//
// 只作用于 Anthropic 入口（/v1/messages）经 CC 转换的出站体——OpenAI 原生
// /v1/chat/completions 的 image_url parts 不受影响（源同样只在 anthropic 自适应
// 转换路径丢图）。Qoder 不走本策略（实测上游只读 content parts，恒带图）。

import (
	"encoding/json"
	"os"
	"strings"
)

// anthropicForwardImagesEnabled 报告 /v1/messages 的图片是否转发给 WB/Trae 上游。
// 默认关；SO_ANTHROPIC_FORWARD_IMAGES=1/on/true/yes 打开。
func anthropicForwardImagesEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("SO_ANTHROPIC_FORWARD_IMAGES"))) {
	case "1", "on", "true", "yes":
		return true
	}
	return false
}

// anthropicImagesDroppedNote 图片被丢弃时写进 user 文本的提示。措辞刻意点明
// **是网关丢的**，否则模型会顺着用户的问题编（「请上传图片」），用户则以为客户端
// 或模型出了问题。移植自 trae-workbuddy-switch 76a97c0 images_dropped_note。
func anthropicImagesDroppedNote(count int) string {
	return "[已移除 " + itoa(count) + " 张图片：当前通道未启用图片转发]"
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// dropChatBodyImagesWithNote 从转换后的 CC body 里剔除 image parts 并留痕：
//   - 每条消息的 content parts 数组里剔除 type=="image_url" 的 part，计数；
//   - 有 text part 时把提示追加到最后一个 text part（原文保留在前）；
//   - 剔除后 parts 为空（纯图片消息）时 content 直接置为提示文本——绝不让消息
//     凭空消失，否则用户的问题会变成「无的放矢」（源同款注记语义）。
//
// 返回处理后的 body 与是否发生了剔除。
func dropChatBodyImagesWithNote(body []byte) ([]byte, bool) {
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		return body, false
	}
	msgs, ok := obj["messages"].([]any)
	if !ok {
		return body, false
	}
	changed := false
	for _, rawMsg := range msgs {
		msg, ok := rawMsg.(map[string]any)
		if !ok {
			continue
		}
		parts, ok := msg["content"].([]any)
		if !ok {
			continue
		}
		dropped := 0
		kept := make([]any, 0, len(parts))
		lastText := -1
		for _, rawPart := range parts {
			part, ok := rawPart.(map[string]any)
			if !ok {
				kept = append(kept, rawPart)
				continue
			}
			if ptype, _ := part["type"].(string); ptype == "image_url" {
				dropped++
				continue
			}
			if ptype, _ := part["type"].(string); ptype == "text" {
				lastText = len(kept)
			}
			kept = append(kept, rawPart)
		}
		if dropped == 0 {
			continue
		}
		changed = true
		note := anthropicImagesDroppedNote(dropped)
		if len(kept) == 0 {
			// 纯图片消息：用提示文本顶替 content，消息本身保留。
			msg["content"] = note
			continue
		}
		if lastText >= 0 {
			if tp, ok := kept[lastText].(map[string]any); ok {
				if t, _ := tp["text"].(string); t != "" {
					tp["text"] = t + "\n\n" + note
				} else {
					tp["text"] = note
				}
			}
		} else {
			kept = append(kept, map[string]any{"type": "text", "text": note})
		}
		msg["content"] = kept
	}
	if !changed {
		return body, false
	}
	out, err := json.Marshal(obj)
	if err != nil {
		return body, false
	}
	return out, true
}
