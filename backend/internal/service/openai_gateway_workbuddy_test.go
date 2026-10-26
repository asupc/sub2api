//go:build unit

package service

// openai_gateway_workbuddy_test.go 单元测试：raw CC 漏斗的 workbuddy 分叉——
// URL（realm 选路）/ UA 三段式与 X-Machine-ID 头族 / 体归一化（强制 stream、
// prompt_cache_key）/ SSE 透传含 usage / 非流式聚合 / 错误分类 → 账号状态映射。

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func wbGatewayAccount(realm string) *Account {
	credentials := map[string]any{
		"access_token":  "wb-at",
		"refresh_token": "wb-rt",
		"expires_at":    time.Now().Add(24 * time.Hour).Unix(),
		"realm":         realm,
		"uid":           "uid-abc123",
		"api_protocol":  "adaptive",
	}
	return &Account{
		ID:          7,
		Name:        "wb-gw",
		Platform:    PlatformWorkbuddy,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: credentials,
		Extra:       map[string]any{},
	}
}

// wbSSEStream 组装一段标准 OpenAI CC SSE 响应（含 usage 末帧与 [DONE]）。
func wbSSEStream() string {
	var b strings.Builder
	b.WriteString(`data: {"id":"cmpl-1","object":"chat.completion.chunk","created":1770000000,"model":"glm-5.3","choices":[{"index":0,"delta":{"role":"assistant","content":"你"},"finish_reason":null}]}` + "\n\n")
	b.WriteString(`data: {"id":"cmpl-1","object":"chat.completion.chunk","created":1770000000,"model":"glm-5.3","choices":[{"index":0,"delta":{"content":"好"},"finish_reason":null}]}` + "\n\n")
	b.WriteString(`data: {"id":"cmpl-1","object":"chat.completion.chunk","created":1770000000,"model":"glm-5.3","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}` + "\n\n")
	b.WriteString(`data: {"id":"cmpl-1","object":"chat.completion.chunk","created":1770000000,"model":"glm-5.3","choices":[],"usage":{"prompt_tokens":11,"completion_tokens":22,"total_tokens":33}}` + "\n\n")
	b.WriteString("data: [DONE]\n\n")
	return b.String()
}

func wbNewForwardContext(body []byte) (*gin.Context, *bytes.Buffer) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c, rec.Body
}

func TestWorkbuddyForward_StreamTransportAndSSERelay(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(wbSSEStream())),
	}}
	repo := &wbRepoRecorder{}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream, accountRepo: repo}
	account := wbGatewayAccount("cn")

	body := []byte(`{"model":"glm-5.3","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	c, responseBody := wbNewForwardContext(body)
	result, err := svc.forwardAsRawChatCompletions(context.Background(), c, account, body, "")
	require.NoError(t, err)
	require.NotNil(t, result)

	req := upstream.lastReq
	require.NotNil(t, req)
	// URL：realm cn → copilot.tencent.com + /v2 单路径。
	require.Equal(t, "https://copilot.tencent.com/v2/chat/completions", req.URL.String())
	// UA 三段式（WorkBuddy/<ver> WorkBuddy/<ver> CLI/<ver>）。
	require.Equal(t, "WorkBuddy/5.5.4 WorkBuddy/5.5.4 CLI/2.137.1", req.Header.Get("User-Agent"))
	// 头族：Bearer / X-CodeBuddy-Request / 稳定机器与会话 ID（36 hex）。
	require.Equal(t, "Bearer wb-at", req.Header.Get("Authorization"))
	require.Equal(t, "1", req.Header.Get("X-CodeBuddy-Request"))
	require.Regexp(t, regexp.MustCompile(`^[0-9a-f]{36}$`), req.Header.Get("X-Machine-ID"))
	require.Regexp(t, regexp.MustCompile(`^[0-9a-f]{36}$`), req.Header.Get("X-Session-ID"))
	require.NotEqual(t, req.Header.Get("X-Machine-ID"), req.Header.Get("X-Session-ID"))
	require.Equal(t, "uid-abc123", req.Header.Get("X-User-Id"))
	// 会话聚合主键必发。
	require.Regexp(t, regexp.MustCompile(`^[0-9a-f]{32}$`), req.Header.Get("X-Conversation-Request-ID"))
	// 安全红线：chat 请求绝不携带 X-Refresh-Token。
	require.Empty(t, req.Header.Get("X-Refresh-Token"))

	// 体归一化：stream 保持、include_usage、prompt_cache_key（账号 uid 前缀隔离）。
	upstreamBody := upstream.lastBody
	require.True(t, gjson.GetBytes(upstreamBody, "stream").Bool())
	require.True(t, gjson.GetBytes(upstreamBody, "stream_options.include_usage").Bool())
	cacheKey := gjson.GetBytes(upstreamBody, "prompt_cache_key").String()
	// pkg buildCacheKey 的 uid8 = uid 前 8 字符（跨账号缓存键不碰撞）。
	require.True(t, strings.HasPrefix(cacheKey, "wb2a-uid-abc1-"), "cache key 须按账号 uid 隔离，got %q", cacheKey)

	// SSE 透传给客户端 + usage 提取。
	require.Contains(t, responseBody.String(), `"content":"你"`)
	require.Contains(t, responseBody.String(), "[DONE]")
	require.Equal(t, 11, result.Usage.InputTokens)
	require.Equal(t, 22, result.Usage.OutputTokens)
	require.Equal(t, workbuddyChatEndpointPath, result.UpstreamEndpoint)
}

func TestWorkbuddyForward_GlobalRealmRoutingAndUA(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(wbSSEStream())),
	}}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream, accountRepo: &wbRepoRecorder{}}
	account := wbGatewayAccount("global")

	body := []byte(`{"model":"gpt-6-astra","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	c, _ := wbNewForwardContext(body)
	_, err := svc.forwardAsRawChatCompletions(context.Background(), c, account, body, "")
	require.NoError(t, err)

	require.Equal(t, "https://www.workbuddy.ai/v2/chat/completions", upstream.lastReq.URL.String())
	// global 平台段品牌是 WorkBuddy AI。
	require.Equal(t, "WorkBuddy/5.5.4 WorkBuddy AI/5.5.4 CLI/2.137.1", upstream.lastReq.Header.Get("User-Agent"))
	require.Equal(t, "www.workbuddy.ai", upstream.lastReq.Header.Get("X-Domain"))
}

func TestWorkbuddyForward_NonStreamClientAggregatesSSE(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(wbSSEStream())),
	}}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream, accountRepo: &wbRepoRecorder{}}
	account := wbGatewayAccount("cn")

	// 客户端非流式：上游仍以流出站（上游拒绝非流式）。
	body := []byte(`{"model":"glm-5.3","stream":false,"messages":[{"role":"user","content":"hi"}]}`)
	c, responseBody := wbNewForwardContext(body)
	result, err := svc.forwardAsRawChatCompletions(context.Background(), c, account, body, "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.Stream)

	require.True(t, gjson.GetBytes(upstream.lastBody, "stream").Bool(), "上游侧必须强制 stream")

	// 聚合出标准 chat.completion JSON。
	resp := responseBody.String()
	require.Equal(t, "chat.completion", gjson.Get(resp, "object").String())
	require.Equal(t, "你好", gjson.Get(resp, "choices.0.message.content").String())
	require.Equal(t, "stop", gjson.Get(resp, "choices.0.finish_reason").String())
	require.Equal(t, "glm-5.3", gjson.Get(resp, "model").String())
	require.Equal(t, 11, result.Usage.InputTokens)
	require.Equal(t, 22, result.Usage.OutputTokens)
	require.NotContains(t, resp, "data:", "非流式响应不得残留 SSE 帧")
}

// 二开：workbuddy 上游 usage.credit（单数）经聚合回填，供每日积分统计。
// apicompat.ChatUsage 无 credits 字段，聚合器会丢——由 wrapWorkbuddyChatResponse
// 用 scanCCStream 的原始提取值 sjson 回填。
func TestWorkbuddyForward_NonStreamAggregateCarriesCredit(t *testing.T) {
	stream := strings.Replace(wbSSEStream(),
		`"usage":{"prompt_tokens":11,"completion_tokens":22,"total_tokens":33}`,
		`"usage":{"prompt_tokens":11,"completion_tokens":22,"total_tokens":33,"credit":1.75}`,
		1)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(stream)),
	}}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream, accountRepo: &wbRepoRecorder{}}
	account := wbGatewayAccount("cn")

	body := []byte(`{"model":"glm-5.3","stream":false,"messages":[{"role":"user","content":"hi"}]}`)
	c, responseBody := wbNewForwardContext(body)
	result, err := svc.forwardAsRawChatCompletions(context.Background(), c, account, body, "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 1.75, result.Usage.Credits, "聚合路径的 usage.credit 必须被提取")
	require.Equal(t, 1.75, gjson.Get(responseBody.String(), "usage.credit").Float(), "聚合 JSON 必须回填 usage.credit")
}

// ---------------------------------------------------------------------------
// 错误分类 → 账号状态映射。
// ---------------------------------------------------------------------------

func wbNewGatewayWithRepo(repo *wbRepoRecorder) *OpenAIGatewayService {
	return &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: &httpUpstreamRecorder{}, accountRepo: repo}
}

func wbErrorContext() *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader("{}"))
	return c
}

func TestWorkbuddyUpstreamError_PlainRateLimit(t *testing.T) {
	repo := &wbRepoRecorder{}
	svc := wbNewGatewayWithRepo(repo)
	account := wbGatewayAccount("cn")
	resp := &http.Response{StatusCode: 429, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"code":0,"msg":"请求过于频繁，将在 2099-01-01 10:00:00 重置"}`))}

	handled, foErr := svc.handleWorkbuddyUpstreamError(context.Background(), wbErrorContext(), account, resp, []byte(`{"code":0,"msg":"请求过于频繁，将在 2099-01-01 10:00:00 重置"}`), "请求过于频繁", "glm-5.3")
	require.True(t, handled)
	require.NotNil(t, foErr, "账号级软限流须换号")
	require.Len(t, repo.rateLimited, 1)
	// 2099 重置时间超出 2h 封顶 → 截到封顶。
	require.WithinDuration(t, time.Now().Add(workbuddyRateLimitCooldownCap), repo.rateLimited[0], time.Minute)
	require.Empty(t, repo.modelLimits)
	require.Empty(t, repo.setErrors)
}

func TestWorkbuddyUpstreamError_ModelLevel6004(t *testing.T) {
	repo := &wbRepoRecorder{}
	svc := wbNewGatewayWithRepo(repo)
	account := wbGatewayAccount("cn")
	body := []byte(`{"code":6004,"msg":"该模型的使用量超限"}`)

	handled, foErr := svc.handleWorkbuddyUpstreamError(context.Background(), wbErrorContext(), account,
		&http.Response{StatusCode: 429, Header: http.Header{}}, body, "模型用量超限", "glm-5.3")
	require.True(t, handled)
	require.NotNil(t, foErr)
	// 6004：模型维度冷却，不动账号整体。
	require.Len(t, repo.modelLimits, 1)
	require.Equal(t, "glm-5.3", repo.modelLimits[0].scope)
	require.Empty(t, repo.rateLimited)
}

func TestWorkbuddyUpstreamError_HardCreditDisablesAccount(t *testing.T) {
	repo := &wbRepoRecorder{account: wbGatewayAccount("cn")}
	svc := wbNewGatewayWithRepo(repo)
	account := repo.account
	body := []byte(`{"code":14018,"msg":"积分不足"}`)

	handled, foErr := svc.handleWorkbuddyUpstreamError(context.Background(), wbErrorContext(), account,
		&http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{}}, body, "积分不足", "glm-5.3")
	require.True(t, handled)
	require.NotNil(t, foErr)
	require.Equal(t, []string{WorkbuddyNoCreditReason}, repo.setErrors)
	require.Equal(t, StatusError, account.Status)
	require.False(t, account.Schedulable)
	// 快照清零（extra 写入）。
	zeroed := false
	for _, e := range repo.updateExtras {
		for _, updates := range e {
			if _, ok := updates[workbuddyQuotaExtraKey]; ok {
				zeroed = true
			}
		}
	}
	require.True(t, zeroed, "402 应清零 wb_quota 快照")
}

func TestWorkbuddyUpstreamError_SessionDeadStrikeTolerance(t *testing.T) {
	repo := &wbRepoRecorder{account: wbGatewayAccount("cn")}
	svc := wbNewGatewayWithRepo(repo)
	account := repo.account
	body := []byte(`{"code":12153,"msg":"Offline user session not found"}`)
	resp := &http.Response{StatusCode: 401, Header: http.Header{}}

	// 前两次：仅换号，不罚号。
	for i := 1; i <= 2; i++ {
		handled, foErr := svc.handleWorkbuddyUpstreamError(context.Background(), wbErrorContext(), account, resp, body, "session", "glm-5.3")
		require.True(t, handled)
		require.NotNil(t, foErr)
		require.Empty(t, repo.setErrors, "strike %d 不应罚号", i)
	}
	// 第三次：罚号。
	handled, foErr := svc.handleWorkbuddyUpstreamError(context.Background(), wbErrorContext(), account, resp, body, "session", "glm-5.3")
	require.True(t, handled)
	require.NotNil(t, foErr)
	require.Equal(t, []string{"wb_session_dead"}, repo.setErrors)
	require.Equal(t, 3, account.Extra[workbuddySessionStrikesExtraKey])
}

func TestWorkbuddyUpstreamError_ModelBlockedAndAccountFaultPassThrough(t *testing.T) {
	repo := &wbRepoRecorder{account: wbGatewayAccount("cn")}
	svc := wbNewGatewayWithRepo(repo)

	// 11102 模型封禁：不罚账号、不换号（handled + nil → 原文透传）。
	handled, foErr := svc.handleWorkbuddyUpstreamError(context.Background(), wbErrorContext(), repo.account,
		&http.Response{StatusCode: 400, Header: http.Header{}},
		[]byte(`{"code":11102,"msg":"service info not found"}`), "no model", "glm-5.3")
	require.True(t, handled)
	require.Nil(t, foErr)
	require.Empty(t, repo.setErrors)
	require.Empty(t, repo.rateLimited)

	// 11140 request illegal（账号被上游限制，重登无效需官方申诉）：置 error 停调度
	// + 换号轮转（移植自 workbuddy2api-panel 7a8b7a7——此前仅透传不处置，被限制的
	// 账号会继续被选中送死刷风控）。
	handled, foErr = svc.handleWorkbuddyUpstreamError(context.Background(), wbErrorContext(), repo.account,
		&http.Response{StatusCode: 403, Header: http.Header{}},
		[]byte(`{"code":11140,"msg":"request illegal"}`), "request illegal", "glm-5.3")
	require.True(t, handled)
	require.NotNil(t, foErr)
	require.Equal(t, []string{"wb_restricted: account restricted by upstream (11140 request illegal); appeal in WorkBuddy to lift the restriction"}, repo.setErrors)
}

// TestWorkbuddyUpstreamError_ChannelRefusedAndUpstreamBusy 11-128 通道拒绝与
// 6021 渠道满载的分流处置（移植自 workbuddy2api-panel 7a8b7a7 / f14f374）：
// 两者都不罚号；11-128 换号轮转，6021 不轮转且合成 503。
func TestWorkbuddyUpstreamError_ChannelRefusedAndUpstreamBusy(t *testing.T) {
	// 11-128 通道拒绝：账号健康，不罚号、换号。
	repo := &wbRepoRecorder{account: wbGatewayAccount("cn")}
	svc := wbNewGatewayWithRepo(repo)
	handled, foErr := svc.handleWorkbuddyUpstreamError(context.Background(), wbErrorContext(), repo.account,
		&http.Response{StatusCode: 400, Header: http.Header{}},
		[]byte(`{"code":"11-128","msg":"request illegal"}`), "request illegal", "glm-5.3")
	require.True(t, handled)
	require.NotNil(t, foErr, "通道拒绝应换号轮转")
	require.Empty(t, repo.setErrors, "通道拒绝不得罚号")
	require.Empty(t, repo.rateLimited, "通道拒绝不得冷却")

	// 6021 queue.full：渠道级满载，不罚号、不轮转（fail-fast）、合成 503。
	repo2 := &wbRepoRecorder{account: wbGatewayAccount("cn")}
	svc2 := wbNewGatewayWithRepo(repo2)
	resp := &http.Response{StatusCode: 429, Header: http.Header{}}
	handled, foErr = svc2.handleWorkbuddyUpstreamError(context.Background(), wbErrorContext(), repo2.account,
		resp, []byte(`{"code":6021,"msg":"queue.full.title"}`), "queue.full.title", "glm-5.3")
	require.True(t, handled)
	require.Nil(t, foErr, "渠道满载不换号")
	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode, "合成 503 让客户端退避")
	require.Empty(t, repo2.setErrors, "渠道满载不得罚号")
	require.Empty(t, repo2.rateLimited, "渠道满载不得冷却")
}

func TestWorkbuddyUpstreamError_ServerErrorsFallBackToGenericChain(t *testing.T) {
	repo := &wbRepoRecorder{}
	svc := wbNewGatewayWithRepo(repo)
	account := wbGatewayAccount("cn")

	// 5xx / 404 未接管 → handled=false，交既有通用 failover 链。
	for _, status := range []int{500, 502, 404} {
		handled, _ := svc.handleWorkbuddyUpstreamError(context.Background(), wbErrorContext(), account,
			&http.Response{StatusCode: status, Header: http.Header{}}, []byte(`{"code":0,"msg":"boom"}`), "boom", "glm-5.3")
		require.False(t, handled, "status %d 应回落通用链", status)
	}
}

// TestWorkbuddyUpstreamError_WafBlockSoftCooldown 回归：403 无业务信封体
//（APISIX WAF 拦截页/空体/纯文本）此前未接管，回落通用链走 handleOpenAI403
// 的计数升级路径——WAF 形态多为空体/纯文本（不享 HTML 豁免），三次即
// SetError 永久禁号。修复后：账号软冷却（优先 Retry-After，缺失 60s 基数）、
// 不罚号、本次换号。
func TestWorkbuddyUpstreamError_WafBlockSoftCooldown(t *testing.T) {
	repo := &wbRepoRecorder{}
	svc := wbNewGatewayWithRepo(repo)
	account := wbGatewayAccount("cn")

	// 空体 403（WAF APISIX 拦截典型形态）。
	handled, foErr := svc.handleWorkbuddyUpstreamError(context.Background(), wbErrorContext(), account,
		&http.Response{StatusCode: http.StatusForbidden, Header: http.Header{}}, []byte(``), "", "glm-5.3")
	require.True(t, handled, "WAF 403 必须由平台层接管，不得回落通用计数链")
	require.NotNil(t, foErr)
	require.Empty(t, repo.setErrors, "WAF 不得罚号")
	require.Len(t, repo.rateLimited, 1)
	require.WithinDuration(t, time.Now().Add(workbuddyWafCooldownBase), repo.rateLimited[0], 10*time.Second)

	// 纯文本拦截页 + Retry-After 头：优先上游明示时长。
	repo2 := &wbRepoRecorder{}
	svc2 := wbNewGatewayWithRepo(repo2)
	handled, foErr = svc2.handleWorkbuddyUpstreamError(context.Background(), wbErrorContext(), account,
		&http.Response{StatusCode: http.StatusForbidden, Header: http.Header{"Retry-After": []string{"120"}}},
		[]byte(`Request blocked by APISIX`), "blocked", "glm-5.3")
	require.True(t, handled)
	require.NotNil(t, foErr)
	require.Empty(t, repo2.setErrors)
	require.Len(t, repo2.rateLimited, 1)
	require.WithinDuration(t, time.Now().Add(120*time.Second), repo2.rateLimited[0], 10*time.Second)
}

func TestWorkbuddyRateLimitResetAtParsesUpstreamResetTime(t *testing.T) {
	// 上游明示重置时间（UTC+8 文案）在下个整点附近 → 直接采用。
	future := time.Now().In(time.FixedZone("UTC+8", 8*3600)).Add(45 * time.Minute)
	body := "请求过于频繁，将在 " + future.Format("2006-01-02 15:04:05") + " 重置"
	resetAt := workbuddyRateLimitResetAt(http.Header{}, body)
	require.WithinDuration(t, future, resetAt, 2*time.Minute)

	// Retry-After 头（秒）优先于本地兜底。
	resetAt = workbuddyRateLimitResetAt(http.Header{"Retry-After": []string{"120"}}, "")
	require.WithinDuration(t, time.Now().Add(2*time.Minute), resetAt, 30*time.Second)

	// 无线索 → 兜底 10min。
	resetAt = workbuddyRateLimitResetAt(http.Header{}, "")
	require.WithinDuration(t, time.Now().Add(workbuddyRateLimitFallbackCooldown), resetAt, 30*time.Second)
}

// ---------------------------------------------------------------------------
// CC 回退路径（/v1/messages 与 /v1/responses 入站）——workbuddy 的核心验收点：
// Claude Code 经 forwardAnthropicViaRawChatCompletions 直连。
// ---------------------------------------------------------------------------

func TestWorkbuddyAnthropicInboundNonStreamViaCCFallback(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(wbSSEStream())),
	}}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream, accountRepo: &wbRepoRecorder{}}
	account := wbGatewayAccount("cn")

	// Claude Code 非流式 /v1/messages 请求体。
	body := []byte(`{"model":"glm-5.3","max_tokens":128,"stream":false,"messages":[{"role":"user","content":"hi"}]}`)
	c, responseBody := wbNewForwardContext(body)
	c.Request.URL.Path = "/v1/messages"
	result, err := svc.forwardAnthropicViaRawChatCompletions(context.Background(), c, account, body, "")
	require.NoError(t, err)
	require.NotNil(t, result)

	// 出站：workbuddy 端点 + 头族 + 体归一化（非流式客户端仍以流出站）。
	require.Equal(t, "https://copilot.tencent.com/v2/chat/completions", upstream.lastReq.URL.String())
	require.Equal(t, "Bearer wb-at", upstream.lastReq.Header.Get("Authorization"))
	require.True(t, gjson.GetBytes(upstream.lastBody, "stream").Bool(), "上游侧必须强制 stream")
	require.True(t, gjson.GetBytes(upstream.lastBody, "stream_options.include_usage").Bool())

	// 回程：SSE 已聚合为 JSON 并转换为 Anthropic 响应形态。
	resp := responseBody.String()
	require.Equal(t, "你好", gjson.Get(resp, "content.0.text").String())
	require.Equal(t, "end_turn", gjson.Get(resp, "stop_reason").String())
	require.NotContains(t, resp, "data:", "非流式响应不得残留 SSE 帧")
	require.Equal(t, 11, result.Usage.InputTokens)
	require.Equal(t, 22, result.Usage.OutputTokens)
}

func TestWorkbuddyResponsesInboundNonStreamViaCCFallback(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(wbSSEStream())),
	}}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream, accountRepo: &wbRepoRecorder{}}
	account := wbGatewayAccount("cn")

	body := []byte(`{"model":"glm-5.3","stream":false,"input":"hi"}`)
	c, responseBody := wbNewForwardContext(body)
	c.Request.URL.Path = "/v1/responses"
	result, err := svc.forwardResponsesViaRawChatCompletions(context.Background(), c, account, body, time.Now())
	require.NoError(t, err)
	require.NotNil(t, result)

	require.Equal(t, "https://copilot.tencent.com/v2/chat/completions", upstream.lastReq.URL.String())
	require.True(t, gjson.GetBytes(upstream.lastBody, "stream").Bool())

	resp := responseBody.String()
	require.Equal(t, "response", gjson.Get(resp, "object").String())
	require.Equal(t, "你好", gjson.Get(resp, "output.0.content.0.text").String())
	require.NotContains(t, resp, "data:")
	require.Equal(t, 11, result.Usage.InputTokens)
	require.Equal(t, 22, result.Usage.OutputTokens)
}

func TestWorkbuddyResolveCCFallbackTarget(t *testing.T) {
	svc := &OpenAIGatewayService{cfg: &config.Config{}}

	token, targetURL, err := svc.resolveCCFallbackTarget(wbGatewayAccount("cn"))
	require.NoError(t, err)
	require.Equal(t, "wb-at", token)
	require.Equal(t, "https://copilot.tencent.com/v2/chat/completions", targetURL)

	token, targetURL, err = svc.resolveCCFallbackTarget(wbGatewayAccount("global"))
	require.NoError(t, err)
	require.Equal(t, "wb-at", token)
	require.Equal(t, "https://www.workbuddy.ai/v2/chat/completions", targetURL)

	// 凭据缺失 → 明确报错（而非回落 openai api_key 路径）。
	broken := wbGatewayAccount("cn")
	delete(broken.Credentials, "access_token")
	_, _, err = svc.resolveCCFallbackTarget(broken)
	require.Error(t, err)
	require.Contains(t, err.Error(), "workbuddy")
}
