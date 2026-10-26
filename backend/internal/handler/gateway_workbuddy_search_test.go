//go:build unit

package handler

// gateway_workbuddy_search_test.go 单元测试：/v1/search/messages 的协议壳——
// web_search 工具声明校验 / query 提取剥前缀 / 响应契约 / id 唯一性。
// 断言口径移植自 workbuddy2api-panel internal/server/messages_test.go
// （commit 4f31005 + ee9db99 回归项）。

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/workbuddy"
)

func TestWorkbuddyDeclaresWebSearch(t *testing.T) {
	cases := []struct {
		name  string
		tools []workbuddySearchMessagesTool
		want  bool
	}{
		{"type 官方形态", []workbuddySearchMessagesTool{{Type: "web_search_20250305"}}, true},
		{"type 前缀命中", []workbuddySearchMessagesTool{{Type: "web_search"}}, true},
		{"name 简化形态", []workbuddySearchMessagesTool{{Name: "web_search"}}, true},
		{"混合工具只认搜索", []workbuddySearchMessagesTool{{Type: "custom"}, {Name: "web_search"}}, true},
		{"普通工具不认", []workbuddySearchMessagesTool{{Type: "function", Name: "get_weather"}}, false},
		{"空工具", nil, false},
	}
	for _, tc := range cases {
		if got := workbuddyDeclaresWebSearch(tc.tools); got != tc.want {
			t.Errorf("%s: got=%v want=%v", tc.name, got, tc.want)
		}
	}
}

func TestExtractWorkbuddySearchQuery(t *testing.T) {
	strTurn := func(role, text string) workbuddySearchTurnItem {
		raw, _ := json.Marshal(text)
		return workbuddySearchTurnItem{Role: role, Content: raw}
	}
	blocksTurn := func(role string, texts ...string) workbuddySearchTurnItem {
		blocks := make([]workbuddySearchContentBlock, 0, len(texts))
		for _, tx := range texts {
			blocks = append(blocks, workbuddySearchContentBlock{Type: "text", Text: tx})
		}
		raw, _ := json.Marshal(blocks)
		return workbuddySearchTurnItem{Role: role, Content: raw}
	}

	cases := []struct {
		name  string
		items []workbuddySearchTurnItem
		want  string
	}{
		{
			name:  "字符串 content + 前缀剥离",
			items: []workbuddySearchTurnItem{strTurn("user", "Perform a web search for the query: golang generics")},
			want:  "golang generics",
		},
		{
			name:  "块数组 content + 多 text 拼接",
			items: []workbuddySearchTurnItem{blocksTurn("user", "Perform a web search for the query: part one", "part two")},
			want:  "part one\npart two",
		},
		{
			name:  "前缀大小写不敏感",
			items: []workbuddySearchTurnItem{strTurn("user", "perform a web search for the query: Mixed Case")},
			want:  "Mixed Case",
		},
		{
			name: "只取最后一条带文本的消息（不拼接历史）",
			items: []workbuddySearchTurnItem{
				strTurn("user", "Perform a web search for the query: old question"),
				strTurn("assistant", "old answer"),
				strTurn("user", "Perform a web search for the query: new question"),
			},
			want: "new question",
		},
		{
			name:  "无前缀按原文",
			items: []workbuddySearchTurnItem{strTurn("user", "raw query here")},
			want:  "raw query here",
		},
		{
			name:  "空 content",
			items: []workbuddySearchTurnItem{{Role: "user"}},
			want:  "",
		},
	}
	for _, tc := range cases {
		if got := extractWorkbuddySearchQuery(tc.items); got != tc.want {
			t.Errorf("%s: got=%q want=%q", tc.name, got, tc.want)
		}
	}
}

func TestBuildWorkbuddySearchResponseShape(t *testing.T) {
	out := &workbuddy.SearchOutcome{
		Query: "q",
		Results: []workbuddy.SearchResult{
			{Title: "t1", URL: "https://a.example/1", Snippet: "s1"},
			{Title: "", URL: "https://b.example/2", Snippet: ""},
		},
		TotalResults: 2,
	}
	resp := buildWorkbuddySearchResponse("q", "m1", out)

	if resp.Type != "message" || resp.Role != "assistant" || resp.Model != "m1" {
		t.Fatalf("envelope=%+v", resp)
	}
	if resp.StopReason != "end_turn" || resp.StopSequence != nil {
		t.Errorf("stop=%q seq=%v", resp.StopReason, resp.StopSequence)
	}
	if resp.Usage.InputTokens != 0 || resp.Usage.OutputTokens != 0 {
		t.Errorf("usage=%+v want 全 0（搜索不消耗模型 token）", resp.Usage)
	}

	// 首个 block 必须是 web_search_tool_result（DSH mapAnthropicResponse 硬要求）。
	first, ok := resp.Content[0].(workbuddySearchToolResultBlock)
	if !ok {
		t.Fatalf("content[0]=%T want workbuddySearchToolResultBlock", resp.Content[0])
	}
	if first.Type != "web_search_tool_result" || !strings.HasPrefix(first.ToolUseID, "srvtoolu_") {
		t.Errorf("toolResult=%+v", first)
	}
	if len(first.Content) != 2 {
		t.Fatalf("items=%d want 2", len(first.Content))
	}
	if first.Content[0].Type != "web_search_result" || first.Content[0].URL == "" {
		t.Errorf("items[0]=%+v", first.Content[0])
	}
	// 无标题用 URL 兜底，客户端渲染 title ?? hostname 才不会空。
	if first.Content[1].Title != "https://b.example/2" {
		t.Errorf("items[1].Title=%q want URL 兜底", first.Content[1].Title)
	}

	// 有摘要时必须有 text block 承载 citations（web_search_result 本身不带摘要）。
	if len(resp.Content) != 2 {
		t.Fatalf("content blocks=%d want 2（tool_result + citations text）", len(resp.Content))
	}
	textBlk, ok := resp.Content[1].(workbuddySearchTextBlock)
	if !ok {
		t.Fatalf("content[1]=%T want workbuddySearchTextBlock", resp.Content[1])
	}
	if len(textBlk.Citations) != 1 {
		t.Fatalf("citations=%d want 1（仅第一条有摘要）", len(textBlk.Citations))
	}
	cit := textBlk.Citations[0]
	if cit.Type != "web_search_result_location" || cit.CitedText != "s1" || cit.URL != "https://a.example/1" {
		t.Errorf("citation=%+v", cit)
	}

	// 无摘要时不应有多余 text block。
	resp2 := buildWorkbuddySearchResponse("q", "", &workbuddy.SearchOutcome{
		Results: []workbuddy.SearchResult{{Title: "t", URL: "https://c.example"}},
	})
	if len(resp2.Content) != 1 {
		t.Errorf("无摘要时 content blocks=%d want 1", len(resp2.Content))
	}
	if resp2.Model != "web-search" {
		t.Errorf("model=%q want 空模型回落 web-search", resp2.Model)
	}
}

// TestWorkbuddySearchResponseIDsAreUnique 同一 query 的重复搜索必须返回不同
// msg id 与 tool_use_id（ee9db99 回归项：id 标识「一次工具调用」而非查询内容）。
func TestWorkbuddySearchResponseIDsAreUnique(t *testing.T) {
	out := &workbuddy.SearchOutcome{
		Results: []workbuddy.SearchResult{{Title: "t", URL: "https://x.example"}},
	}
	seenMsg := map[string]bool{}
	seenTool := map[string]bool{}
	for i := 0; i < 20; i++ {
		resp := buildWorkbuddySearchResponse("same query", "m", out)
		if seenMsg[resp.ID] {
			t.Fatalf("重复 msg id: %s", resp.ID)
		}
		seenMsg[resp.ID] = true
		blk := resp.Content[0].(workbuddySearchToolResultBlock)
		if seenTool[blk.ToolUseID] {
			t.Fatalf("重复 tool_use_id: %s", blk.ToolUseID)
		}
		seenTool[blk.ToolUseID] = true
	}
}

// TestWorkbuddySearchMessagesRouteNotShadowMessages 端点归一化不得把
// /v1/search/messages 折进 /v1/messages（两者语义不同：搜索 vs 模型对话）。
func TestWorkbuddySearchMessagesRouteNotShadowMessages(t *testing.T) {
	if got := NormalizeInboundEndpoint("/v1/search/messages"); got != EndpointSearchMessages {
		t.Errorf("normalize=/v1/search/messages → %q want %q", got, EndpointSearchMessages)
	}
	if got := NormalizeInboundEndpoint("/v1/messages"); got != EndpointMessages {
		t.Errorf("normalize=/v1/messages → %q want %q", got, EndpointMessages)
	}
}
