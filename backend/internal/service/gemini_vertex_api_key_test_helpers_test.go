//go:build unit

package service

// 上游 gemini_vertex_api_key_forward_test.go 已随显式缓存模块裁剪，这里仅保留
// 新增测试（生图分组开关 / 流式客户端中断计费）依赖的两个构造辅助与响应常量。

import (
	"io"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

const geminiVertexAPIKeyTestResponse = `{"candidates":[{"content":{"parts":[{"text":"hello"}],"role":"model"},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5,"totalTokenCount":15}}`

func newGeminiVertexAPIKeyTestService() (*GeminiMessagesCompatService, *geminiCompatHTTPUpstreamStub) {
	httpStub := &geminiCompatHTTPUpstreamStub{
		response: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}, "X-Request-Id": []string{"vertex-req-1"}},
			Body:       io.NopCloser(strings.NewReader(geminiVertexAPIKeyTestResponse)),
		},
	}
	// tokenProvider 留空：API Key 认证不依赖 access token。
	return &GeminiMessagesCompatService{httpUpstream: httpStub, cfg: &config.Config{}}, httpStub
}

func geminiVertexAPIKeyTestAccount(projectID string) *Account {
	credentials := map[string]any{
		"auth_mode": "apikey",
		"api_key":   "vertex-test-key",
		"location":  "us-central1",
		"tier_id":   "vertex",
	}
	if projectID != "" {
		credentials["project_id"] = projectID
	}
	return &Account{
		ID:          811,
		Platform:    PlatformGemini,
		Type:        AccountTypeServiceAccount,
		Credentials: credentials,
		Concurrency: 1,
	}
}
