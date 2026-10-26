package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// 复现上游 #7309：Anthropic → Responses 历史转换产出的 reasoning item 只带
// encrypted_content、不带 summary，OpenCode Zen 的 muse-spark 在第二轮起整单 400
// （"input[3] missing required field summary"）。整条丢弃即可（协议允许 drop）。
func TestSanitizeOpenCodeResponsesReasoningReplay_DropsSummarylessHistoryReasoning(t *testing.T) {
	body := []byte(`{"model":"muse-spark-1.3-contributor","stream":true,"include":["reasoning.encrypted_content"],"input":[` +
		`{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]},` +
		`{"type":"reasoning","encrypted_content":"cipher-from-previous-turn"},` +
		`{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]},` +
		`{"type":"function_call","call_id":"toolu_1","name":"lookup","arguments":"{}"},` +
		`{"type":"function_call_output","call_id":"toolu_1","output":"ok"},` +
		`{"type":"message","role":"user","content":[{"type":"input_text","text":"again"}]}` +
		`]}`)

	sanitized, changed, err := SanitizeOpenCodeResponsesReasoningReplay(body)
	require.NoError(t, err)
	require.True(t, changed)

	assert.Equal(t, int64(5), gjson.GetBytes(sanitized, "input.#").Int())
	assert.False(t, gjson.GetBytes(sanitized, `input.#(type=="reasoning")`).Exists(),
		"历史 reasoning item 必须整条删除")
	assert.Equal(t, "toolu_1", gjson.GetBytes(sanitized, "input.2.call_id").String(),
		"function_call / function_call_output 配对必须保留")
	assert.Equal(t, "muse-spark-1.3-contributor", gjson.GetBytes(sanitized, "model").String())
	assert.True(t, gjson.GetBytes(sanitized, "stream").Bool())
	assert.Equal(t, int64(1), gjson.GetBytes(sanitized, `include.#`).Int())
}

// 带完整 summary 的 reasoning item 同样丢弃：既省一次逐条修补，也天然满足
// 「reasoning 后必须紧跟 assistant message / function_call」的顺序约束。
func TestSanitizeOpenCodeResponsesReasoningReplay_DropsCompleteReasoningItems(t *testing.T) {
	body := []byte(`{"input":[` +
		`{"type":"reasoning","id":"rs_1","encrypted_content":"cipher","summary":[{"type":"summary_text","text":"t"}]},` +
		`{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}` +
		`]}`)

	sanitized, changed, err := SanitizeOpenCodeResponsesReasoningReplay(body)
	require.NoError(t, err)
	require.True(t, changed)
	assert.Equal(t, int64(1), gjson.GetBytes(sanitized, "input.#").Int())
	assert.Equal(t, "message", gjson.GetBytes(sanitized, "input.0.type").String())
}

func TestSanitizeOpenCodeResponsesReasoningReplay_NoReasoningIsNoop(t *testing.T) {
	body := []byte(`{"model":"muse-spark-1.3-contributor","input":[` +
		`{"type":"message","role":"user","content":"hi"}]}`)

	sanitized, changed, err := SanitizeOpenCodeResponsesReasoningReplay(body)
	require.NoError(t, err)
	require.False(t, changed)
	assert.Equal(t, string(body), string(sanitized))
}

func TestSanitizeOpenCodeResponsesReasoningReplay_NoInputIsNoop(t *testing.T) {
	body := []byte(`{"model":"muse-spark-1.3-contributor","instructions":"x"}`)

	sanitized, changed, err := SanitizeOpenCodeResponsesReasoningReplay(body)
	require.NoError(t, err)
	require.False(t, changed)
	assert.Equal(t, string(body), string(sanitized))
}

func TestSanitizeOpenCodeResponsesReasoningReplay_StringInputIsNoop(t *testing.T) {
	body := []byte(`{"model":"muse-spark-1.3-contributor","input":"plain prompt"}`)

	sanitized, changed, err := SanitizeOpenCodeResponsesReasoningReplay(body)
	require.NoError(t, err)
	require.False(t, changed)
	assert.Equal(t, string(body), string(sanitized))
}

func TestSanitizeOpenCodeResponsesReasoningReplay_PreservesLargeIntegers(t *testing.T) {
	body := []byte(`{"input":[` +
		`{"type":"reasoning","encrypted_content":"cipher"},` +
		`{"type":"message","role":"user","content":"hi"}` +
		`],"tools":[{"type":"function","name":"lookup","parameters":{"type":"object","properties":{"id":{"const":9007199254740993}}}}]}`)

	sanitized, changed, err := SanitizeOpenCodeResponsesReasoningReplay(body)
	require.NoError(t, err)
	require.True(t, changed)
	assert.Contains(t, string(sanitized), `"const":9007199254740993`,
		"清洗不得把 JSON 整数经 float64 回写")
}

// 收口位置：OpenCode 平台的 Responses 转发（/v1/messages、/v1/chat/completions、
// /v1/responses 共用 buildUpstreamRequest）删除 reasoning；其他平台原样保留。
// OpenAI 平台直连 opencode.ai 官方主机（User-Agent / session 同源判定）与透传
// 构造路径同样收口。
func TestBuildUpstreamRequest_StripsOpenCodeReasoningReplay(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := openCodeSessionTestService()
	body := []byte(`{"model":"muse-spark-1.3-contributor","input":[` +
		`{"type":"message","role":"user","content":"hi"},` +
		`{"type":"reasoning","encrypted_content":"cipher"},` +
		`{"type":"message","role":"assistant","content":"hello"}]}`)

	openCode := &Account{
		ID:       1,
		Platform: PlatformOpenCodeGo,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":      "sk-opencode",
			"account_mode": AccountModeZen,
		},
	}
	req, err := svc.buildUpstreamRequest(context.Background(), newOpenCodeSessionTestContext(t, ""), openCode, body, "sk-opencode", true, "", false)
	require.NoError(t, err)
	assert.Equal(t, "https://opencode.ai/zen/v1/responses", req.URL.String())
	forwarded, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	assert.Equal(t, int64(2), gjson.GetBytes(forwarded, "input.#").Int())
	assert.False(t, gjson.GetBytes(forwarded, `input.#(type=="reasoning")`).Exists())

	// OpenAI 平台账号直连官方 Zen：目标主机判定生效。
	openAIViaZen := &Account{
		ID:       2,
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "sk-zen",
			"base_url": "https://opencode.ai/zen/v1",
		},
	}
	req, err = svc.buildUpstreamRequest(context.Background(), newOpenCodeSessionTestContext(t, ""), openAIViaZen, body, "sk-zen", true, "", false)
	require.NoError(t, err)
	assert.Equal(t, "https://opencode.ai/zen/v1/responses", req.URL.String())
	forwarded, err = io.ReadAll(req.Body)
	require.NoError(t, err)
	assert.Equal(t, int64(2), gjson.GetBytes(forwarded, "input.#").Int(),
		"目标为官方 opencode.ai 主机时必须剔除 reasoning item")

	// 透传构造路径同样收口（客户端直接发来的 reasoning item）。
	passthroughReq, err := svc.buildUpstreamRequestOpenAIPassthrough(
		context.Background(), newOpenCodeSessionTestContext(t, ""), openAIViaZen, body, "sk-zen")
	require.NoError(t, err)
	passthroughBody, err := io.ReadAll(passthroughReq.Body)
	require.NoError(t, err)
	assert.Equal(t, int64(2), gjson.GetBytes(passthroughBody, "input.#").Int(),
		"透传路径同样必须剔除 reasoning item")

	openAI := &Account{
		ID:          3,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-openai", "base_url": "https://api.openai.com/v1"},
	}
	req, err = svc.buildUpstreamRequest(context.Background(), newOpenCodeSessionTestContext(t, ""), openAI, body, "sk-openai", true, "", false)
	require.NoError(t, err)
	untouched, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	assert.Equal(t, int64(3), gjson.GetBytes(untouched, "input.#").Int(),
		"非 OpenCode 平台不得改动 reasoning item")
}

// 端到端（进程内）：/v1/messages 多轮历史（thinking + tool_use + tool_result）
// 经 Anthropic → Responses 转换后发往 OpenCode Zen，出站 body 不再含 reasoning item，
// 但 tool_use / tool_result 的 call_id 配对完整保留。
func TestForwardAsAnthropic_OpenCodeDropsReplayedReasoning(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := []byte(`{"model":"muse-spark-1.3-contributor","max_tokens":64,"stream":false,"messages":[` +
		`{"role":"user","content":"hi"},` +
		`{"role":"assistant","content":[` +
		`{"type":"thinking","thinking":"prior reasoning","signature":"cipher-from-previous-turn"},` +
		`{"type":"text","text":"calling tool"},` +
		`{"type":"tool_use","id":"toolu_1","name":"lookup","input":{"q":"x"}}` +
		`]},` +
		`{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"result"}]},` +
		`{"role":"user","content":"next question"}` +
		`]}`)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`data: {"type":"response.output_text.delta","delta":"ok"}`,
			"",
			`data: {"type":"response.completed","response":{"id":"resp_oc","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1}}}`,
			"",
		}, "\n"))),
	}}
	svc := &OpenAIGatewayService{
		cfg: &config.Config{Security: config.SecurityConfig{
			URLAllowlist: config.URLAllowlistConfig{Enabled: false, AllowInsecureHTTP: true},
		}},
		httpUpstream: upstream,
	}
	account := &Account{
		ID: 7001, Name: "opencode-zen", Platform: PlatformOpenCodeGo,
		Type: AccountTypeAPIKey, Concurrency: 1,
		Credentials: map[string]any{
			"api_key":      "sk-opencode-test",
			"base_url":     "http://upstream.example",
			"account_mode": AccountModeZen,
			"api_protocol": APIProtocolResponses,
		},
	}

	result, err := svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotEmpty(t, upstream.bodies)

	forwarded := upstream.bodies[0]
	assert.Equal(t, "muse-spark-1.3-contributor", gjson.GetBytes(forwarded, "model").String())
	assert.False(t, gjson.GetBytes(forwarded, `input.#(type=="reasoning")`).Exists(),
		"多轮历史中的 reasoning item 不得再发给严格 Responses 上游")
	assert.Equal(t, "toolu_1", gjson.GetBytes(forwarded, `input.#(type=="function_call").call_id`).String(),
		"function_call 必须保留且 call_id 不变")
	assert.Equal(t, "toolu_1", gjson.GetBytes(forwarded, `input.#(type=="function_call_output").call_id`).String(),
		"function_call_output 必须保留且 call_id 不变")
	assert.Equal(t, int64(5), gjson.GetBytes(forwarded, "input.#").Int())
}
