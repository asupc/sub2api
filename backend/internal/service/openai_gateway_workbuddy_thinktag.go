package service

// openai_gateway_workbuddy_thinktag.go 二开：workbuddy 平台 /v1/messages 出口的
// 内联思考标签拆分（panel 771c887 / issue #194）。
//
// 上游 hy4 的 vLLM reasoning parser 偶发把整段 CoT 当 content 下发，正文首尾带
// <think:SUF>…</think:SUF> 控制 token；Anthropic 客户端只认 thinking 块，会把
// 控制 token 当正文显示。拆在 CC 层（content → reasoning_content）后，既有的
// CC→Anthropic 转换自然把它渲染成 thinking。

import (
	"encoding/json"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/workbuddy"
)

// splitWorkbuddyThinkChunk 把一片 CC 流增量里的内联思考标签拆到 reasoning_content。
func splitWorkbuddyThinkChunk(tt *workbuddy.ThinkTag, chunk *apicompat.ChatCompletionsChunk) {
	if tt == nil || chunk == nil {
		return
	}
	for i := range chunk.Choices {
		d := &chunk.Choices[i].Delta
		if d.Content == nil || *d.Content == "" {
			continue
		}
		think, body := tt.Feed(*d.Content)
		if think != "" {
			if d.ReasoningContent != nil {
				merged := *d.ReasoningContent + think
				d.ReasoningContent = &merged
			} else {
				t := think
				d.ReasoningContent = &t
			}
		}
		if body == "" {
			d.Content = nil
		} else {
			b := body
			d.Content = &b
		}
	}
}

// flushWorkbuddyThinkChunk 把拆分器尾部残留作为最后一帧发出（无残留返回 nil）。
func flushWorkbuddyThinkChunk(tt *workbuddy.ThinkTag) *apicompat.ChatCompletionsChunk {
	if tt == nil {
		return nil
	}
	think, body := tt.Flush()
	if think == "" && body == "" {
		return nil
	}
	d := apicompat.ChatDelta{}
	if think != "" {
		d.ReasoningContent = &think
	}
	if body != "" {
		d.Content = &body
	}
	return &apicompat.ChatCompletionsChunk{
		Object:  "chat.completion.chunk",
		Choices: []apicompat.ChatChunkChoice{{Index: 0, Delta: d}},
	}
}

// splitWorkbuddyThinkResponse 非流式响应：把字符串 content 里的内联思考标签拆到
// reasoning_content。content 为 parts 数组等非字符串形态时不动。
func splitWorkbuddyThinkResponse(resp *apicompat.ChatCompletionsResponse) {
	if resp == nil {
		return
	}
	for i := range resp.Choices {
		msg := &resp.Choices[i].Message
		if len(msg.Content) == 0 {
			continue
		}
		var content string
		if err := json.Unmarshal(msg.Content, &content); err != nil || content == "" {
			continue
		}
		var tt workbuddy.ThinkTag
		think, body := tt.Feed(content)
		t2, b2 := tt.Flush()
		think += t2
		body += b2
		if !tt.Active() && think == "" {
			continue // 未启用：整段原样，零改动
		}
		if think != "" {
			msg.ReasoningContent += think
		}
		if nb, err := json.Marshal(body); err == nil {
			msg.Content = nb
		}
	}
}
