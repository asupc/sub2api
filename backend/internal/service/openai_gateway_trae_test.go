//go:build unit

package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/trae"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// traeTestAccount 构造一个凭据齐全的 trae OAuth 测试账号。
func traeTestAccount() *Account {
	return &Account{
		ID:          201,
		Name:        "trae-test",
		Platform:    PlatformTrae,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{
			"jwt":           "trae-jwt-token",
			"refresh_token": "trae-refresh-token",
			"variant":       "solo_cn",
			"uid":           "70001",
			"device_id":     "1234567890123456",
			"machine_id":    "0f4a8b2c-1111-2222-3333-444455556666",
			"api_protocol":  APIProtocolAdaptive,
		},
	}
}

// traeSoloSSE 是一段典型的 SOLO 私有 SSE 上游响应。
func traeSoloSSE() string {
	return strings.Join([]string{
		"event: output",
		`data: {"response":"你好"}`,
		"",
		"event: token_usage",
		`data: {"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}`,
		"",
		"event: done",
		`data: {"finish_reason":"stop"}`,
		"",
	}, "\n")
}

func TestTraeChatURL_PerVariant(t *testing.T) {
	t.Parallel()
	for variant, want := range map[string]string{
		"solo_cn":     "https://trae-api-cn.mchost.guru/api/agent/v3/llm_utils_chat",
		"trae_cn":     "https://trae-api-cn.mchost.guru/api/agent/v3/llm_utils_chat",
		"solo_global": "https://core-normal.trae.ai/api/agent/v3/llm_utils_chat",
		"trae_global": "https://core-normal.trae.ai/api/agent/v3/llm_utils_chat",
	} {
		account := traeTestAccount()
		account.Credentials["variant"] = variant
		got, err := traeChatURL(account)
		require.NoError(t, err, variant)
		require.Equal(t, want, got, variant)
	}
}

func TestClassifyTraeUpstreamFailure(t *testing.T) {
	t.Parallel()
	require.Equal(t, http.StatusUnauthorized, classifyTraeUpstreamFailure(401, "Token expired"))
	require.Equal(t, http.StatusForbidden, classifyTraeUpstreamFailure(20403, "Token device not match"))
	require.Equal(t, http.StatusTooManyRequests, classifyTraeUpstreamFailure(429, "rate limited"))
	require.Equal(t, http.StatusTooManyRequests, classifyTraeUpstreamFailure(0, "insufficient credits"))
	require.Equal(t, http.StatusBadRequest, classifyTraeUpstreamFailure(4001, "param is invalid"))
	require.Equal(t, http.StatusBadRequest, classifyTraeUpstreamFailure(4023, "model is unknown"))
	require.Equal(t, http.StatusBadGateway, classifyTraeUpstreamFailure(0, "something else"))
}

// TestForwardAsRawChatCompletions_TraeStream 验证流式请求的 trae 分叉：
// URL 指向 mchost 的 llm_utils_chat、Cloud-IDE-JWT 头族、请求体改写为
// llm_utils_chat 形态，SSE 响应转换后含 OpenAI chunk / usage / [DONE]。
func TestForwardAsRawChatCompletions_TraeStream(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := []byte(`{"model":"glm-5.3","messages":[{"role":"user","content":"你好"}],"stream":true}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(traeSoloSSE())),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	account := traeTestAccount()

	result, err := svc.forwardAsRawChatCompletions(context.Background(), c, account, body, "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 10, result.Usage.InputTokens)
	require.Equal(t, 5, result.Usage.OutputTokens)

	require.NotNil(t, upstream.lastReq)
	require.Equal(t, "https://trae-api-cn.mchost.guru/api/agent/v3/llm_utils_chat", upstream.lastReq.URL.String())
	require.Equal(t, "Cloud-IDE-JWT trae-jwt-token", upstream.lastReq.Header.Get("Authorization"))
	require.Equal(t, "text/event-stream", upstream.lastReq.Header.Get("Accept"))
	require.NotContains(t, upstream.lastReq.Header.Get("Authorization"), "Bearer")

	// 请求体改写：llm_utils_chat 固定字段 + 模型名映射 + content 数组化。
	require.Equal(t, true, gjson.GetBytes(upstream.lastBody, "stream").Bool())
	require.Equal(t, trae.WorkspaceID, gjson.GetBytes(upstream.lastBody, "workspace_id").String())
	require.Equal(t, trae.FunctionSoloWork, gjson.GetBytes(upstream.lastBody, "function").String())
	require.Equal(t, "glm-5.3", gjson.GetBytes(upstream.lastBody, "config_name").String())
	require.Equal(t, "glm-5.3__dev", gjson.GetBytes(upstream.lastBody, "model_name").String())
	require.Equal(t, "70001", gjson.GetBytes(upstream.lastBody, "user_id").String())
	require.Equal(t, "1234567890123456", gjson.GetBytes(upstream.lastBody, "device_id").String())
	require.Equal(t, "text", gjson.GetBytes(upstream.lastBody, "messages.0.content.0.type").String())
	require.Equal(t, "你好", gjson.GetBytes(upstream.lastBody, "messages.0.content.0.text").String())

	// 客户端收到 OpenAI 形态的 SSE。
	out := rec.Body.String()
	require.Contains(t, out, "chat.completion.chunk")
	require.Contains(t, out, "你好")
	require.Contains(t, out, `"total_tokens":15`)
	require.Contains(t, out, "data: [DONE]")
}

// TestForwardAsRawChatCompletions_TraeNonStreamAggregates 验证非流式请求：
// 上游仍为 SSE，本地聚合为单个 chat.completion JSON。
func TestForwardAsRawChatCompletions_TraeNonStreamAggregates(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := []byte(`{"model":"glm-5.3","messages":[{"role":"user","content":"你好"}],"stream":false}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(traeSoloSSE())),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}

	result, err := svc.forwardAsRawChatCompletions(context.Background(), c, traeTestAccount(), body, "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.Stream)
	require.EqualValues(t, 15, result.Usage.InputTokens+result.Usage.OutputTokens)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	require.Equal(t, "chat.completion", gjson.Get(rec.Body.String(), "object").String())
	require.Equal(t, "你好", gjson.Get(rec.Body.String(), "choices.0.message.content").String())
	require.Equal(t, "stop", gjson.Get(rec.Body.String(), "choices.0.finish_reason").String())
	require.EqualValues(t, 15, gjson.Get(rec.Body.String(), "usage.total_tokens").Int())
}

// TestForwardAsRawChatCompletions_TraeUpstreamError 验证非流式请求的流内
// error 事件：映射为合成 HTTP 状态并按既有错误链透传给客户端。
func TestForwardAsRawChatCompletions_TraeUpstreamError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := []byte(`{"model":"no-such-model","messages":[{"role":"user","content":"你好"}],"stream":false}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))

	upstreamSSE := strings.Join([]string{
		"event: error",
		`data: {"code":4023,"message":"model is unknown"}`,
		"",
	}, "\n")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(upstreamSSE)),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}

	_, err := svc.forwardAsRawChatCompletions(context.Background(), c, traeTestAccount(), body, "")
	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "model is unknown")
}

// TestForwardAsRawChatCompletions_TraeMissingCredentials 凭据缺失在建请求
// 阶段报错（不触网）。
func TestForwardAsRawChatCompletions_TraeMissingCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := []byte(`{"model":"glm-5.3","messages":[{"role":"user","content":"你好"}],"stream":true}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))

	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: &httpUpstreamRecorder{}}
	account := traeTestAccount()
	delete(account.Credentials, "variant")

	_, err := svc.forwardAsRawChatCompletions(context.Background(), c, account, body, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "variant")
}
