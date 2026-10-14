//go:build unit

package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/qoder"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// qoderTestAccount 构造一个凭据齐全的 qoder OAuth 测试账号。
func qoderTestAccount() *Account {
	return &Account{
		ID:          301,
		Name:        "qoder-test",
		Platform:    PlatformQoder,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":  "dt-test-token",
			"refresh_token": "drt-test-refresh",
			"token_kind":    "device",
			"expires_at":    "4102444800",
			"realm":         "cn",
			"uid":           "u-1001",
			"nickname":      "tester",
			"user_type":     "personal_professional_trial",
			"api_protocol":  APIProtocolAdaptive,
		},
	}
}

// qoderEnvelopeSSE 构造一段典型的私有 SSE 信封上游响应（内层为 OpenAI chunk）。
func qoderEnvelopeSSE() string {
	wrap := func(body string) string {
		return `data:{"headers":{},"body":` + strconv.Quote(body) + `,"statusCodeValue":200}`
	}
	frames := []string{
		wrap(`{"id":"chatcmpl_qd1","object":"chat.completion.chunk","created":1760000000,"model":"qmodel","choices":[{"index":0,"delta":{"role":"assistant","content":"你好"},"finish_reason":null}]}`),
		wrap(`{"id":"chatcmpl_qd1","object":"chat.completion.chunk","created":1760000000,"model":"qmodel","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`),
		`data:{"body":"[DONE]"}`,
	}
	return strings.Join(frames, "\n\n") + "\n\n"
}

func TestQoderChatURL_PerRealm(t *testing.T) {
	t.Parallel()
	for realm, wantHost := range map[string]string{
		"cn":   "https://gateway.qoder.com.cn",
		"intl": "https://api1.qoder.sh",
	} {
		account := qoderTestAccount()
		account.Credentials["realm"] = realm
		got, err := qoderChatURL(account)
		require.NoError(t, err, realm)
		require.Equal(t, wantHost+qoder.ChatPath, got, realm)
	}
}

func TestClassifyQoderUpstreamFailure(t *testing.T) {
	t.Parallel()
	require.Equal(t, http.StatusUnauthorized, classifyQoderUpstreamFailure(401, "unauthorized"))
	require.Equal(t, http.StatusUnauthorized, classifyQoderUpstreamFailure(403, "TOKEN_EXPIRE: Offline user session not found"))
	require.Equal(t, http.StatusTooManyRequests, classifyQoderUpstreamFailure(402, "insufficient credits"))
	require.Equal(t, http.StatusTooManyRequests, classifyQoderUpstreamFailure(429, "rate limited retryAfterSeconds=30"))
	require.Equal(t, http.StatusBadRequest, classifyQoderUpstreamFailure(400, "invalid_request_error: bad param"))
	require.Equal(t, http.StatusBadRequest, classifyQoderUpstreamFailure(200, "DataInspectionFailed: input text data may contain inappropriate content"))
	require.Equal(t, http.StatusBadGateway, classifyQoderUpstreamFailure(500, "internal"))
	require.Equal(t, http.StatusBadGateway, classifyQoderUpstreamFailure(418, "provider_error wrapped"))
}

func TestForwardAsRawChatCompletions_QoderStream(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := []byte(`{"model":"qmodel","messages":[{"role":"user","content":"你好"}],"stream":true}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(qoderEnvelopeSSE())),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}

	result, err := svc.forwardAsRawChatCompletions(context.Background(), c, qoderTestAccount(), body, "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 10, result.Usage.InputTokens)
	require.Equal(t, 5, result.Usage.OutputTokens)
	require.Equal(t, qoderChatEndpointPath, result.UpstreamEndpoint)

	// URL：realm=cn 主选网关 + agent_chat_generation 端点。
	require.Equal(t, "https://gateway.qoder.com.cn"+qoder.ChatPath, upstream.lastReq.URL.String())

	// COSY 签名头族替换默认 Bearer 头。
	auth := upstream.lastReq.Header.Get("Authorization")
	require.True(t, strings.HasPrefix(auth, "Bearer COSY."), auth)
	require.Equal(t, "u-1001", upstream.lastReq.Header.Get("cosy-user"))
	require.Equal(t, qoder.COSYVersion, upstream.lastReq.Header.Get("cosy-version"))
	require.Equal(t, "application/json", upstream.lastReq.Header.Get("Content-Type"))
	require.Equal(t, "text/event-stream", upstream.lastReq.Header.Get("Accept"))
	require.Equal(t, "AGREE", upstream.lastReq.Header.Get("cosy-data-policy"))

	// 出站体为 qoder_encode 载荷：解码后是 baseprompt 骨架（模型 key 已解析）。
	decoded, derr := qoder.QoderDecode(string(upstream.lastBody))
	require.NoError(t, derr)
	require.Equal(t, "agent_common", gjson.GetBytes(decoded, "agent_id").String())
	require.Equal(t, "qmodel", gjson.GetBytes(decoded, "model_config.key").String())
	require.Equal(t, true, gjson.GetBytes(decoded, "stream").Bool())
	require.Equal(t, "你好", gjson.GetBytes(decoded, "chat_context.text.text").String())

	// 客户端收到 OpenAI 形态的 SSE。
	out := rec.Body.String()
	require.Contains(t, out, "chat.completion.chunk")
	require.Contains(t, out, "你好")
	require.Contains(t, out, `"total_tokens":15`)
	require.Contains(t, out, "data: [DONE]")
}

// TestForwardAsRawChatCompletions_QoderNonStreamAggregates 验证非流式请求：
// 上游仍为 SSE 信封，本地聚合为单个 chat.completion JSON。
func TestForwardAsRawChatCompletions_QoderNonStreamAggregates(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := []byte(`{"model":"qmodel","messages":[{"role":"user","content":"你好"}],"stream":false}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(qoderEnvelopeSSE())),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}

	result, err := svc.forwardAsRawChatCompletions(context.Background(), c, qoderTestAccount(), body, "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.Stream)
	require.Equal(t, 15, result.Usage.InputTokens+result.Usage.OutputTokens)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	require.Equal(t, "chat.completion", gjson.Get(rec.Body.String(), "object").String())
	require.Equal(t, "你好", gjson.Get(rec.Body.String(), "choices.0.message.content").String())
	require.Equal(t, "stop", gjson.Get(rec.Body.String(), "choices.0.finish_reason").String())
	require.Equal(t, int64(15), gjson.Get(rec.Body.String(), "usage.total_tokens").Int())
}

// qoderEnvelopeSSEWithCredits 在 usage 帧带复数 credits（上游实测字段，
// qoder2api-hub qoder_proxy.py L394-398：单数 credit 恒为 None）。
func qoderEnvelopeSSEWithCredits(credits float64) string {
	wrap := func(body string) string {
		return `data:{"headers":{},"body":` + strconv.Quote(body) + `,"statusCodeValue":200}`
	}
	usage := `{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15,"credits":` + strconv.FormatFloat(credits, 'f', -1, 64) + `}`
	frames := []string{
		wrap(`{"id":"chatcmpl_qd1","object":"chat.completion.chunk","created":1760000000,"model":"qmodel","choices":[{"index":0,"delta":{"role":"assistant","content":"你好"},"finish_reason":null}]}`),
		wrap(`{"id":"chatcmpl_qd1","object":"chat.completion.chunk","created":1760000000,"model":"qmodel","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":` + usage + `}`),
		`data:{"body":"[DONE]"}`,
	}
	return strings.Join(frames, "\n\n") + "\n\n"
}

// 二开：qoder 非流式聚合路径必须提取 usage.credits（复数优先）供每日积分统计。
func TestForwardAsRawChatCompletions_QoderNonStreamCarriesCredits(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := []byte(`{"model":"qmodel","messages":[{"role":"user","content":"你好"}],"stream":false}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(qoderEnvelopeSSEWithCredits(1.5))),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}

	result, err := svc.forwardAsRawChatCompletions(context.Background(), c, qoderTestAccount(), body, "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 1.5, result.Usage.Credits, "非流式聚合必须提取 usage.credits")
}

// 二开：qoder 流式透传路径经 CC 中继的逐帧 usage 提取也必须拿到 credits。
func TestForwardAsRawChatCompletions_QoderStreamCarriesCredits(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := []byte(`{"model":"qmodel","messages":[{"role":"user","content":"你好"}],"stream":true}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	c.Request.Header.Set("Accept", "text/event-stream")

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(qoderEnvelopeSSEWithCredits(2.25))),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}

	result, err := svc.forwardAsRawChatCompletions(context.Background(), c, qoderTestAccount(), body, "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 2.25, result.Usage.Credits, "流式透传必须经 CC 中继提取 usage.credits")
}

// TestForwardAsRawChatCompletions_QoderEnvelopeError 验证流内信封错误（HTTP 200
// 建流后 statusCodeValue=429）：合成为 429 状态交既有错误链（failover）。
func TestForwardAsRawChatCompletions_QoderEnvelopeError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := []byte(`{"model":"qmodel","messages":[{"role":"user","content":"你好"}],"stream":false}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))

	upstreamSSE := strings.Join([]string{
		`data:{"headers":{},"body":"rate limited retryAfterSeconds=30","statusCodeValue":429}`,
		"",
	}, "\n")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(upstreamSSE)),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}

	_, err := svc.forwardAsRawChatCompletions(context.Background(), c, qoderTestAccount(), body, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "429")
}

// TestForwardAsRawChatCompletions_QoderStreamEnvelopeQueueFailover 回归（流式）：
// HTTP 200 建流后**首帧**信封即 10605 排队（statusCodeValue=403）时，此前直接把
// 200 + 错误帧写给客户端——首字节已出，failover 链无法再换号，多账号池也会失败。
// 对照 qoder_proxy.py should_retry_envelope L3917-3934：未向客户端写出任何字节前
// 应重开换号。修复后流式与非流式一致：合成 429 错误状态以 failover 信号上抛，
// 账号按限流冷却、不罚号、不触发 token 刷新。
func TestForwardAsRawChatCompletions_QoderStreamEnvelopeQueueFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)

	prev := qoderRequestPathRefresher.Load()
	qoderRequestPathRefresher.Store(nil)
	t.Cleanup(func() { qoderRequestPathRefresher.Store(prev) })

	queued := `{"code":"403","message":"{\"code\":\"10605\",\"isQueued\":true,\"queueType\":\"p3\",\"serviceAvailable\":false,\"retryAfterSeconds\":30,\"waitTime\":30}"}`
	upstreamSSE := `data:{"headers":{},"body":` + strconv.Quote(queued) + `,"statusCodeValue":403}` + "\n\n"
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(upstreamSSE)),
	}}
	repo := newQoderPolicyRepo()
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream, accountRepo: repo}
	account := qoderTestAccount()

	body := []byte(`{"model":"qmodel","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))

	_, err := svc.forwardAsRawChatCompletions(context.Background(), c, account, body, "")
	require.Error(t, err, "排队需以 failover 信号上抛换号")
	var foErr *UpstreamFailoverError
	require.ErrorAs(t, err, &foErr)
	require.Equal(t, http.StatusTooManyRequests, foErr.StatusCode)
	require.Empty(t, rec.Body.String(), "不得向客户端写出 200 + 错误帧")
	require.Empty(t, repo.errors, "排队不得罚号")
	resetAt, ok := repo.rateLimited[account.ID]
	require.True(t, ok, "排队应按限流冷却账号")
	require.WithinDuration(t, time.Now().Add(30*time.Second), resetAt, 5*time.Second)
}

// TestForwardAsRawChatCompletions_QoderStreamQueueSinglePoolKeepsAccountHot 回归
// （单号池）：429/10605 排队是**模型级**上游状态（真机验证：同一账号 kmodel 可用
// 而 qmodel 排队），多轮 30s 账号级冻结会把唯一账号的可用模型一起拖死——表现为
// "成功一次之后一直报错"。对齐 qoder_accounts.py note_error 的 single_account
// 语义（一次排队不能把唯一账号冷死）：单号池排队/429 不 SetRateLimited，仅以
// failover 信号上抛（本次请求拿真实上游结果，后续请求继续真实撞上游）。
func TestForwardAsRawChatCompletions_QoderStreamQueueSinglePoolKeepsAccountHot(t *testing.T) {
	gin.SetMode(gin.TestMode)

	prev := qoderRequestPathRefresher.Load()
	qoderRequestPathRefresher.Store(nil)
	t.Cleanup(func() { qoderRequestPathRefresher.Store(prev) })

	queued := `{"code":"403","message":"{\"code\":\"10605\",\"isQueued\":true,\"retryAfterSeconds\":30}"}`
	upstreamSSE := `data:{"headers":{},"body":` + strconv.Quote(queued) + `,"statusCodeValue":403}` + "\n\n"
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(upstreamSSE)),
	}}
	repo := newQoderPolicyRepo()
	repo.platformAccounts = []Account{*qoderTestAccount()} // 平台仅 1 个可调度账号
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream, accountRepo: repo}
	account := qoderTestAccount()

	body := []byte(`{"model":"qmodel","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))

	_, err := svc.forwardAsRawChatCompletions(context.Background(), c, account, body, "")
	require.Error(t, err, "排队仍以 failover 信号上抛（换号语义不变）")
	var foErr *UpstreamFailoverError
	require.ErrorAs(t, err, &foErr)
	require.Empty(t, rec.Body.String(), "不得向客户端写出 200 + 错误帧")
	require.Empty(t, repo.rateLimited, "单号池排队不得冻结账号（拖死可用模型）")
	require.Empty(t, repo.errors, "排队不得罚号")

	// 对照组：多账号池保持 30s 冷却（既有行为）。
	repo2 := newQoderPolicyRepo()
	repo2.platformAccounts = []Account{*qoderTestAccount(), {ID: 402, Platform: PlatformQoder}}
	svc2 := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream, accountRepo: repo2}
	upstream2 := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(upstreamSSE)),
	}}
	svc2.httpUpstream = upstream2
	rec2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(rec2)
	c2.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	_, err = svc2.forwardAsRawChatCompletions(context.Background(), c2, account, body, "")
	require.Error(t, err)
	resetAt, ok := repo2.rateLimited[account.ID]
	require.True(t, ok, "多账号池排队仍按限流冷却")
	require.WithinDuration(t, time.Now().Add(30*time.Second), resetAt, 5*time.Second)
}

// TestForwardAsRawChatCompletions_QoderStreamMidStreamErrorKeepsErrorFrame 对照组：
// 首帧为正常内容、错误信封在流中段到达时，客户端输出早已开始（对应 Python
// emitted_bytes=True 不再重开），维持既有行为：200 + 流内错误帧 + [DONE]，
// 账号仍按错误族处置（排队冷却）。
func TestForwardAsRawChatCompletions_QoderStreamMidStreamErrorKeepsErrorFrame(t *testing.T) {
	gin.SetMode(gin.TestMode)

	wrap := func(body string, status int) string {
		return `data:{"headers":{},"body":` + strconv.Quote(body) + `,"statusCodeValue":` + strconv.Itoa(status) + `}`
	}
	content := `{"id":"chatcmpl_qd1","object":"chat.completion.chunk","created":1760000000,"model":"qmodel","choices":[{"index":0,"delta":{"role":"assistant","content":"你好"},"finish_reason":null}]}`
	queued := `{"code":"403","message":"{\"code\":\"10605\",\"isQueued\":true,\"retryAfterSeconds\":30}"}`
	upstreamSSE := strings.Join([]string{wrap(content, 200), wrap(queued, 403), ""}, "\n\n") + "\n"
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(upstreamSSE)),
	}}
	repo := newQoderPolicyRepo()
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream, accountRepo: repo}
	account := qoderTestAccount()

	body := []byte(`{"model":"qmodel","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))

	_, err := svc.forwardAsRawChatCompletions(context.Background(), c, account, body, "")
	require.NoError(t, err)
	out := rec.Body.String()
	require.Contains(t, out, "你好")
	require.Contains(t, out, `"error"`)
	require.Contains(t, out, "data: [DONE]")
	require.Len(t, repo.rateLimited, 1, "流内排队错误仍应冷却账号")
}

// TestForwardAsRawChatCompletions_QoderGatewayFailover 验证网关主机故障切换：
// intl 主选 api1 返回 502（瞬时故障）→ 换 api2 重试成功；body 与 COSY 签名头
// 跨主机保持不变（签名只覆盖 path）。
func TestForwardAsRawChatCompletions_QoderGatewayFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)

	account := qoderTestAccount()
	account.Credentials["realm"] = "intl"
	body := []byte(`{"model":"qmodel","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))

	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		{
			StatusCode: http.StatusBadGateway,
			Header:     http.Header{"Content-Type": []string{"text/plain"}},
			Body:       io.NopCloser(strings.NewReader("bad gateway")),
		},
		{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(qoderEnvelopeSSE())),
		},
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}

	result, err := svc.forwardAsRawChatCompletions(context.Background(), c, account, body, "")
	require.NoError(t, err)
	require.NotNil(t, result)

	require.Len(t, upstream.requests, 2)
	require.Equal(t, "api1.qoder.sh", upstream.requests[0].URL.Host)
	require.Equal(t, "api2.qoder.sh", upstream.requests[1].URL.Host)
	require.Equal(t, "/algo/api/v2/service/pro/sse/agent_chat_generation", upstream.requests[1].URL.Path)
	// 换主机不换签名：body 与 Authorization 完全一致。
	require.Equal(t, upstream.bodies[0], upstream.bodies[1])
	require.Equal(t,
		upstream.requests[0].Header.Get("Authorization"),
		upstream.requests[1].Header.Get("Authorization"))
}

// TestForwardAsRawChatCompletions_QoderMissingCredentials 凭据缺失在建请求
// 阶段报错（不触网）。
func TestForwardAsRawChatCompletions_QoderMissingCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := []byte(`{"model":"qmodel","messages":[{"role":"user","content":"你好"}],"stream":true}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))

	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: &httpUpstreamRecorder{}}
	account := qoderTestAccount()
	delete(account.Credentials, "access_token")

	_, err := svc.forwardAsRawChatCompletions(context.Background(), c, account, body, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "access_token")
}

// TestWrapQoderChatResponse_ClientAbandonDrainsHeartbeat 客户端中途断开时，
// 转换链全部 goroutine 必须退出：WithHeartbeat 的读协程可能阻塞在 16 深度
// 缓冲 channel 的发送上（帧数 > 16 时必现），forward 层 Close 底层 body 后还
// 需排空迭代器，否则 goroutine 泄漏（pkg-qoder 遗留清单第 4 条）。
func TestWrapQoderChatResponse_ClientAbandonDrainsHeartbeat(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 假上游：io.Pipe 持续供帧（25 帧超过心跳 channel 的 16 深度缓冲），
	// 上游永不主动收流，唯一退出路径是 forward 层关闭 body。
	upstreamReader, upstreamWriter := io.Pipe()
	feederDone := make(chan struct{})
	testDone := make(chan struct{})
	t.Cleanup(func() {
		close(testDone)
		_ = upstreamWriter.Close()
	})
	go func() {
		defer close(feederDone)
		frame := `data:{"headers":{},"body":"{\"id\":\"c1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"x\"}}]}","statusCodeValue":200}` + "\n\n"
		for i := 0; i < 25; i++ {
			if _, err := upstreamWriter.Write([]byte(frame)); err != nil {
				return // 读端被 forward 层关闭（正是断开语义）
			}
		}
		<-testDone // 上游持续在线，永不主动收流（测试结束兜底退出）
	}()

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       upstreamReader,
	}
	origBody := []byte(`{"model":"qmodel","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	svc := &OpenAIGatewayService{}

	baseline := runtime.NumGoroutine()
	require.NoError(t, svc.wrapQoderChatResponse(context.Background(), qoderTestAccount(), resp, true, origBody))

	// 客户端读一帧后断开（关闭转换输出的读端）。
	buf := make([]byte, 4096)
	_, _ = resp.Body.Read(buf)
	require.NoError(t, resp.Body.Close())

	// 断开后 pump + 心跳读协程 + 供帧协程都应退出（排空迭代器让阻塞在读
	// channel 发送上的协程拿到读错误自然收尾）；不给排空则恒差 1 个。
	deadline := time.Now().Add(8 * time.Second)
	for runtime.NumGoroutine() > baseline {
		if time.Now().After(deadline) {
			require.FailNowf(t, "conversion goroutines leaked after client abandon",
				"baseline=%d now=%d", baseline, runtime.NumGoroutine())
		}
		time.Sleep(50 * time.Millisecond)
	}
	// 供帧协程因读端关闭而退出。
	select {
	case <-feederDone:
	case <-time.After(2 * time.Second):
		t.Fatal("upstream feeder goroutine did not exit after body close")
	}
}

// ---------------------------------------------------------------------------
// 错误族 → 账号状态映射。
// ---------------------------------------------------------------------------

// qoderPolicyRepo 记录账号状态写操作的最小仓库。
type qoderPolicyRepo struct {
	AccountRepository
	mu          sync.Mutex
	errors      map[int64]string
	rateLimited map[int64]time.Time
	extras      map[int64]map[string]any
	// platformAccounts 模拟 ListSchedulableByPlatform 的返回；nil 表示查询
	// 失败（qoderSingleAccountPool 保守按多账号处理，维持既有冷却断言）。
	platformAccounts []Account
}

func newQoderPolicyRepo() *qoderPolicyRepo {
	return &qoderPolicyRepo{
		errors:      map[int64]string{},
		rateLimited: map[int64]time.Time{},
		extras:      map[int64]map[string]any{},
	}
}

func (r *qoderPolicyRepo) ListSchedulableByPlatform(_ context.Context, _ string) ([]Account, error) {
	if r.platformAccounts == nil {
		return nil, fmt.Errorf("platform accounts not configured")
	}
	return r.platformAccounts, nil
}

func (r *qoderPolicyRepo) SetError(_ context.Context, id int64, msg string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errors[id] = msg
	return nil
}

func (r *qoderPolicyRepo) SetRateLimited(_ context.Context, id int64, resetAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rateLimited[id] = resetAt
	return nil
}

func (r *qoderPolicyRepo) UpdateExtra(_ context.Context, id int64, updates map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	base := r.extras[id]
	if base == nil {
		base = map[string]any{}
		r.extras[id] = base
	}
	for k, v := range updates {
		base[k] = v
	}
	return nil
}

func TestApplyQoderUpstreamFailurePolicy(t *testing.T) {
	// 401 路径会经注册表异步触发强制刷新：测试内先摘除注册表，避免触网。
	prev := qoderRequestPathRefresher.Load()
	qoderRequestPathRefresher.Store(nil)
	t.Cleanup(func() { qoderRequestPathRefresher.Store(prev) })

	repo := newQoderPolicyRepo()
	svc := &OpenAIGatewayService{accountRepo: repo}
	account := qoderTestAccount()
	ctx := context.Background()

	// 402 积分耗尽：qd_quota 清零 + status=error（qd_no_credit）。
	svc.applyQoderUpstreamFailurePolicy(ctx, account, 402, "insufficient credits")
	require.Equal(t, QoderNoCreditReason, repo.errors[account.ID])
	quota, ok := repo.extras[account.ID][qoderQuotaExtraKey].(QoderQuotaSnapshot)
	require.True(t, ok)
	require.Equal(t, 0, quota.Remain)
	require.Equal(t, StatusError, account.Status)
	require.False(t, account.Schedulable)

	// 429 软限流：SetRateLimited 尊重信封 retryAfterSeconds（解析不到时兜底）。
	svc.applyQoderUpstreamFailurePolicy(ctx, account, 429, "rate limited retryAfterSeconds=30")
	require.Equal(t, QoderNoCreditReason, repo.errors[account.ID]) // 402 的罚号记录仍在
	resetAt := repo.rateLimited[account.ID]
	require.False(t, resetAt.IsZero())
	require.WithinDuration(t, time.Now().Add(30*time.Second), resetAt, 5*time.Second)

	// 客户端参数错 / 瞬时错：不计任何账号状态。
	repo2 := newQoderPolicyRepo()
	svc2 := &OpenAIGatewayService{accountRepo: repo2}
	svc2.applyQoderUpstreamFailurePolicy(ctx, account, 400, "invalid_request_error")
	svc2.applyQoderUpstreamFailurePolicy(ctx, account, 500, "transient")
	require.Empty(t, repo2.errors)
	require.Empty(t, repo2.rateLimited)
	require.Empty(t, repo2.extras)
}

func TestClassifyQoderUpstreamFailure_QueueIsRateLimit(t *testing.T) {
	t.Parallel()

	// 10605 排队（403 信封）必须归 429 限流语义，而不是 401 鉴权失败
	//（否则测试/转发误触发强制 token 刷新与罚号）。
	queued := `{"code":"403","message":"{\"code\":\"10605\",\"isQueued\":true,\"retryAfterSeconds\":30}"}`
	require.Equal(t, http.StatusTooManyRequests, classifyQoderUpstreamFailure(http.StatusForbidden, queued))
	// 普通 403 仍是鉴权失败。
	require.Equal(t, http.StatusUnauthorized, classifyQoderUpstreamFailure(http.StatusForbidden, "forbidden"))
}

// TestHandleQoderUpstreamError_QueuedRejectCoolsNotPunishes 回归：建流前 HTTP 403
// 的 10605 排队信封此前命中 `code == http.StatusForbidden` 分支被误判为 token
// 失效——同步阻塞式触发强制刷新，刷新必失败（token 本身有效）后 SetError 罚号，
// 且账号不冷却、下次请求继续撞排队。修复后：冷却 retryAfterSeconds、不刷新、
// 不罚号，本次换号。
func TestHandleQoderUpstreamError_QueuedRejectCoolsNotPunishes(t *testing.T) {
	// 摘除刷新注册表：若误入 401/403 分支会在测试内同步触发刷新 goroutine
	//（触网并写 SetError），本用例将因 setErrors 非空而失败——保留真实分派路径。
	prev := qoderRequestPathRefresher.Load()
	qoderRequestPathRefresher.Store(nil)
	t.Cleanup(func() { qoderRequestPathRefresher.Store(prev) })

	repo := newQoderPolicyRepo()
	svc := &OpenAIGatewayService{accountRepo: repo}
	account := qoderTestAccount()
	body := []byte(`{"code":"403","message":"{\"code\":\"10605\",\"isQueued\":true,\"serviceAvailable\":false,\"retryAfterSeconds\":30}"}`)
	resp := &http.Response{StatusCode: http.StatusForbidden, Header: http.Header{}}

	handled, foErr := svc.handleQoderUpstreamError(context.Background(), wbErrorContext(), account, resp, body, "queued", "auto")
	require.True(t, handled)
	require.NotNil(t, foErr, "排队应换号重试")
	require.Empty(t, repo.errors, "排队不得罚号")
	resetAt, ok := repo.rateLimited[account.ID]
	require.True(t, ok, "排队应按限流冷却账号")
	require.WithinDuration(t, time.Now().Add(30*time.Second), resetAt, 5*time.Second)

	// 对照组：普通 403 仍按 token 失效路径（换号 + 触发刷新；注册表已摘除，
	// 仅断言未落入冷却）。
	repo2 := newQoderPolicyRepo()
	svc2 := &OpenAIGatewayService{accountRepo: repo2}
	handled, foErr = svc2.handleQoderUpstreamError(context.Background(), wbErrorContext(), account,
		&http.Response{StatusCode: http.StatusForbidden, Header: http.Header{}}, []byte(`{"message":"forbidden"}`), "forbidden", "auto")
	require.True(t, handled)
	require.NotNil(t, foErr)
	require.Empty(t, repo2.rateLimited, "普通 403 不按限流冷却")
	require.Empty(t, repo2.errors, "普通 403 由刷新侧异步处置，此处不罚号")
}

// TestMessagesFallback_QoderQueuedRejectNotAuthFailure 回归（跨路径）：/v1/messages
// （Claude Code 落点）CC 回退路径此前未接平台错误分类，qoder 10605 排队（建流前
// HTTP 403）直落通用 failover 链被当鉴权失败。接入统一入口后：账号按限流冷却
// 而非罚号，错误以 failover 信号上抛换号。
func TestMessagesFallback_QoderQueuedRejectNotAuthFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)

	prev := qoderRequestPathRefresher.Load()
	qoderRequestPathRefresher.Store(nil)
	t.Cleanup(func() { qoderRequestPathRefresher.Store(prev) })

	queuedBody := `{"code":"403","message":"{\"code\":\"10605\",\"isQueued\":true,\"serviceAvailable\":false,\"retryAfterSeconds\":30}"}`
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusForbidden,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(queuedBody)),
	}}
	repo := newQoderPolicyRepo()
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream, accountRepo: repo}
	account := qoderTestAccount()

	body := []byte(`{"model":"auto","max_tokens":128,"stream":false,"messages":[{"role":"user","content":"hi"}]}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))

	_, err := svc.forwardAnthropicViaRawChatCompletions(context.Background(), c, account, body, "")
	require.Error(t, err, "排队需以 failover 信号上抛换号")
	var foErr *UpstreamFailoverError
	require.ErrorAs(t, err, &foErr)
	require.Empty(t, repo.errors, "排队不得罚号")
	require.Len(t, repo.rateLimited, 1, "排队应冷却账号")
}

// x-model-key / x-model-source 头必须下发（qoder_sign.py L26：「x-model-key
// 决定上游模型路由」；此前传空导致全部推理/测试请求都不带模型路由头）。
func TestApplyQoderChatTransportModelKeyHeader(t *testing.T) {
	t.Parallel()
	account := qoderTestAccount()
	req := httptest.NewRequest(http.MethodPost, "https://gateway.qoder.com.cn"+qoder.ChatPath, nil)
	origBody := []byte(`{"model":"qwen3.8-max","messages":[{"role":"user","content":"hi"}]}`)
	require.NoError(t, applyQoderChatTransport(req, account, []byte("encoded"), origBody))
	require.Equal(t, "qmodel_38max", req.Header.Get("x-model-key"))
	require.Equal(t, "system", req.Header.Get("x-model-source"))
	require.True(t, strings.HasPrefix(req.Header.Get("Authorization"), "Bearer COSY."),
		"authorization = %q", req.Header.Get("Authorization"))

	// intl 账号 + 官方显示名（大小写不敏感别名）→ 同样解析为上游 key。
	intlAccount := qoderTestAccount()
	intlAccount.Credentials["realm"] = "intl"
	req2 := httptest.NewRequest(http.MethodPost, "https://api1.qoder.sh"+qoder.ChatPath, nil)
	require.NoError(t, applyQoderChatTransport(req2, intlAccount, []byte("encoded"), []byte(`{"model":"Auto"}`)))
	require.Equal(t, "auto", req2.Header.Get("x-model-key"))
}
