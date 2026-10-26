//go:build unit

package service

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestDropChatBodyImagesWithNote /v1/messages 图片默认丢弃留痕（移植自
// trae-workbuddy-switch 76a97c0）：image parts 剔除、提示写进 user 文本、
// 纯图片消息用提示顶替 content 不消失。
func TestDropChatBodyImagesWithNote(t *testing.T) {
	// 图文混合：图剔除、提示追加到 text。
	in := []byte(`{"messages":[
	  {"role":"user","content":[
	    {"type":"text","text":"这图是什么"},
	    {"type":"image_url","image_url":{"url":"data:image/png;base64,xx"}},
	    {"type":"image_url","image_url":{"url":"https://a/b.png"}}
	  ]}
	]}`)
	out, changed := dropChatBodyImagesWithNote(in)
	if !changed {
		t.Fatal("应报告有剔除")
	}
	var obj map[string]any
	if err := json.Unmarshal(out, &obj); err != nil {
		t.Fatal(err)
	}
	parts := obj["messages"].([]any)[0].(map[string]any)["content"].([]any)
	if len(parts) != 1 {
		t.Fatalf("应只剩 text part，got %d", len(parts))
	}
	text := parts[0].(map[string]any)["text"].(string)
	if !strings.HasPrefix(text, "这图是什么") {
		t.Errorf("原文应保留在前，got %q", text)
	}
	if !strings.Contains(text, "已移除 2 张图片") {
		t.Errorf("提示应含剔除张数，got %q", text)
	}

	// 纯图片消息：content 用提示顶替，消息保留。
	in = []byte(`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,yy"}}]}]}`)
	out, changed = dropChatBodyImagesWithNote(in)
	if !changed {
		t.Fatal("应报告有剔除")
	}
	if err := json.Unmarshal(out, &obj); err != nil {
		t.Fatal(err)
	}
	msg := obj["messages"].([]any)[0].(map[string]any)
	content, isStr := msg["content"].(string)
	if !isStr || !strings.Contains(content, "已移除 1 张图片") {
		t.Errorf("纯图片消息 content 应为提示文本，got %v", msg["content"])
	}

	// 无图片：原样返回。
	in = []byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`)
	if _, changed := dropChatBodyImagesWithNote(in); changed {
		t.Error("无图片不应报告变更")
	}
}
