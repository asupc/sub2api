//go:build unit

package service

// 二开新增：trae 积分耗尽（trae_no_credit）的账号级处置与自动恢复回归。
//
// 背景（线上实证，2026-11-06）：账号 98760（id=608）积分归零后，网关继续调度
// 了 27 分钟、52 次 400~700ms 的立即拒绝（usage_logs 里 input/output 全 0，
// ops_error_logs 里一条记录都没有）。根因是 trae 流内错误处置只认 401，
// 积分类错误既不罚号也不进错误看板。

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/trae"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// traeCreditRepo 记录账号状态写操作的最小仓库。
type traeCreditRepo struct {
	AccountRepository
	mu          sync.Mutex
	errors      map[int64]string
	cleared     map[int64]bool
	schedulable map[int64]bool
	extras      map[int64]map[string]any
}

func newTraeCreditRepo() *traeCreditRepo {
	return &traeCreditRepo{
		errors:      map[int64]string{},
		cleared:     map[int64]bool{},
		schedulable: map[int64]bool{},
		extras:      map[int64]map[string]any{},
	}
}

func (r *traeCreditRepo) SetError(_ context.Context, id int64, msg string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errors[id] = msg
	return nil
}

func (r *traeCreditRepo) ClearError(_ context.Context, id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.errors, id)
	r.cleared[id] = true
	return nil
}

func (r *traeCreditRepo) SetSchedulable(_ context.Context, id int64, schedulable bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.schedulable[id] = schedulable
	return nil
}

func (r *traeCreditRepo) UpdateExtra(_ context.Context, id int64, updates map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	base := r.extras[id]
	if base == nil {
		base = map[string]any{}
		r.extras[id] = base
	}
	for key, value := range updates {
		base[key] = value
	}
	return nil
}

// TestIsTraeNoCreditFailure 严格标记只认「耗尽」语义：宽口径的 quota/credit
// 词（配额查询失败、credit 字段缺失等）不得据此冻结账号。
func TestIsTraeNoCreditFailure(t *testing.T) {
	t.Parallel()
	for _, message := range []string{
		"insufficient credits",
		"No Credit Available",
		"your credits exhausted",
		"quota exceeded",
		"Payment Required",
		"not enough credit for this request",
		"积分不足",
		"余额不足，请充值",
		"额度用尽",
	} {
		require.True(t, isTraeNoCreditFailure(message), message)
	}
	for _, message := range []string{
		"",
		"quota field missing",
		"credit parameter invalid",
		"rate limit exceeded",
		"model is unknown",
		"上游返回了未描述的错误",
	} {
		require.False(t, isTraeNoCreditFailure(message), message)
	}
}

// TestHandleTraeUpstreamStreamError_NoCreditPunishesAccount 积分耗尽 → 快照清零
// + trae_no_credit 罚号 + 停调度。
func TestHandleTraeUpstreamStreamError_NoCreditPunishesAccount(t *testing.T) {
	repo := newTraeCreditRepo()
	svc := &OpenAIGatewayService{accountRepo: repo}
	account := traeTestAccount()

	svc.handleTraeUpstreamStreamError(context.Background(), nil, account,
		&trae.UpstreamError{Code: 0, Message: "insufficient credits, please top up"})

	require.Equal(t, TraeNoCreditReason, repo.errors[account.ID])
	require.Equal(t, StatusError, account.Status)
	require.Equal(t, TraeNoCreditReason, account.ErrorMessage)
	require.False(t, account.Schedulable)

	snapshot, ok := repo.extras[account.ID][traeQuotaExtraKey].(map[string]any)
	require.True(t, ok, "积分快照应被清零")
	require.EqualValues(t, 0, snapshot["total"])
}

// TestHandleTraeUpstreamStreamError_OtherErrorsKeepAccount 非积分错误不得罚号：
// 429/瞬时错只换号/重试，4001 参数错是客户端问题。
func TestHandleTraeUpstreamStreamError_OtherErrorsKeepAccount(t *testing.T) {
	for _, upstreamErr := range []*trae.UpstreamError{
		{Code: 429, Message: "rate limit exceeded"},
		{Code: 0, Message: "upstream connection reset"},
		{Code: 4001, Message: "param is invalid"},
	} {
		repo := newTraeCreditRepo()
		svc := &OpenAIGatewayService{accountRepo: repo}
		account := traeTestAccount()
		svc.handleTraeUpstreamStreamError(context.Background(), nil, account, upstreamErr)
		require.Empty(t, repo.errors, upstreamErr.Message)
		require.Empty(t, repo.extras, upstreamErr.Message)
		require.NotEqual(t, StatusError, account.Status, upstreamErr.Message)
	}
}

// TestHandleTraeUpstreamStreamError_MarksOpsError 流内错误必须进 Ops 错误通道：
// wire 状态已固化为 200，只能走带内标记（改造前该路径零记录）。
func TestHandleTraeUpstreamStreamError_MarksOpsError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(nil))

	svc := &OpenAIGatewayService{accountRepo: newTraeCreditRepo()}
	svc.handleTraeUpstreamStreamError(context.Background(), c, traeTestAccount(),
		&trae.UpstreamError{Code: 0, Message: "insufficient credits"})

	streamErrs := GetOpsStreamErrors(c)
	require.Len(t, streamErrs, 1)
	require.Equal(t, "insufficient_quota", streamErrs[0].ErrType)
	require.Equal(t, "trae_upstream_0", streamErrs[0].Code)
	require.Equal(t, http.StatusTooManyRequests, streamErrs[0].IntendedStatus)
	require.True(t, streamErrs[0].CountTowardsSLA, "失败请求必须计入错误率")
	require.NotEmpty(t, streamErrs[0].UpstreamErrors, "上游尝试归因应被记录")
}

// TestForwardAsRawChatCompletions_TraeStreamNoCreditPunishes 端到端：流式路径
// 的积分耗尽错误既要罚号，也要让客户端收到错误帧（不是静默 200 空响应）。
func TestForwardAsRawChatCompletions_TraeStreamNoCreditPunishes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := []byte(`{"model":"deepseek-v4.1-flash","messages":[{"role":"user","content":"你好"}],"stream":true}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))

	upstreamSSE := strings.Join([]string{
		"event: error",
		`data: {"code":0,"message":"insufficient credits"}`,
		"",
	}, "\n")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(upstreamSSE)),
	}}
	repo := newTraeCreditRepo()
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream, accountRepo: repo}
	account := traeTestAccount()

	_, err := svc.forwardAsRawChatCompletions(context.Background(), c, account, body, "")
	require.NoError(t, err)

	// 流式转换在后台 goroutine 中完成：等罚号落地。
	require.Eventually(t, func() bool {
		repo.mu.Lock()
		defer repo.mu.Unlock()
		return repo.errors[account.ID] == TraeNoCreditReason
	}, 2*time.Second, 10*time.Millisecond, "积分耗尽必须罚号停调度")

	out := rec.Body.String()
	require.Contains(t, out, "insufficient credits", "错误帧仍须透传给客户端")
	require.Contains(t, out, "data: [DONE]")
}

// TestForwardAsRawChatCompletions_TraeNonStreamNoCreditPunishes 非流式路径：
// 合成 429 错误状态，同时罚号（此前只写状态、账号状态不动）。
func TestForwardAsRawChatCompletions_TraeNonStreamNoCreditPunishes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := []byte(`{"model":"deepseek-v4.1-flash","messages":[{"role":"user","content":"你好"}],"stream":false}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))

	upstreamSSE := strings.Join([]string{
		"event: error",
		`data: {"code":0,"message":"insufficient credits"}`,
		"",
	}, "\n")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(upstreamSSE)),
	}}
	// 非流式：本地聚合把流内错误合成为 429 错误体交调用方错误链
	// （该路径由 TestForwardAsRawChatCompletions_TraeUpstreamError 覆盖状态码），
	// 这里只钉住账号级处置——此前只写状态、账号状态不动。
	repo := newTraeCreditRepo()
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream, accountRepo: repo}

	_, err := svc.forwardAsRawChatCompletions(context.Background(), c, traeTestAccount(), body, "")
	require.Error(t, err)
	require.Equal(t, TraeNoCreditReason, repo.errors[201])
}

// TestRecoverTraeNoCredit 积分恢复且账号是被本逻辑罚停的 → 解冻；其他来源的
// error 或额度仍为 0 时不动。
func TestRecoverTraeNoCredit(t *testing.T) {
	repo := newTraeCreditRepo()

	// 额度恢复 + trae_no_credit 罚停 → 清错误 + 恢复调度。
	account := traeTestAccount()
	account.Status = StatusError
	account.ErrorMessage = TraeNoCreditReason
	account.Schedulable = false
	repo.errors[account.ID] = TraeNoCreditReason
	repo.schedulable[account.ID] = false

	recoverTraeNoCredit(context.Background(), repo, account, &trae.Entitlements{Total: 120})
	require.True(t, repo.cleared[account.ID])
	require.Equal(t, StatusActive, account.Status)
	require.Empty(t, account.ErrorMessage)
	require.True(t, account.Schedulable)
	require.NotEmpty(t, repo.schedulable[account.ID])

	// 额度仍为 0：不解冻（否则立刻再撞上游，形成抖动）。
	zeroAccount := traeTestAccount()
	zeroAccount.ID = 202
	zeroAccount.Status = StatusError
	zeroAccount.ErrorMessage = TraeNoCreditReason
	repo.errors[zeroAccount.ID] = TraeNoCreditReason
	recoverTraeNoCredit(context.Background(), repo, zeroAccount, &trae.Entitlements{Total: 0})
	require.False(t, repo.cleared[zeroAccount.ID])
	require.Equal(t, StatusError, zeroAccount.Status)

	// 其他来源的 error（如 jwt 失效）：额度恢复也不得误清。
	otherAccount := traeTestAccount()
	otherAccount.ID = 203
	otherAccount.Status = StatusError
	otherAccount.ErrorMessage = "trae jwt invalid (code=401)"
	repo.errors[otherAccount.ID] = otherAccount.ErrorMessage
	recoverTraeNoCredit(context.Background(), repo, otherAccount, &trae.Entitlements{Total: 500})
	require.False(t, repo.cleared[otherAccount.ID])
	require.Equal(t, StatusError, otherAccount.Status)
}

// TestWriteTraeQuotaExtraShapes 快照形状（含清零形状）必须能被消费方按同一组
// 键读取（前端 PlatformOpsQuotaCell 读 total / has_expire / expiring_soon）。
func TestWriteTraeQuotaExtraShapes(t *testing.T) {
	repo := newTraeCreditRepo()
	account := traeTestAccount()
	entitlements := &trae.Entitlements{Total: 88.5, HasExpire: true, EarliestExpire: 1796346147}
	require.NoError(t, writeTraeQuotaExtra(context.Background(), repo, account, entitlements, map[string]any{
		"expiring_soon": true,
	}))

	raw, ok := repo.extras[account.ID][traeQuotaExtraKey].(map[string]any)
	require.True(t, ok)
	require.EqualValues(t, 88.5, raw["total"])
	require.Equal(t, true, raw["has_expire"])
	require.Equal(t, true, raw["expiring_soon"])

	zeroTraeQuotaExtra(context.Background(), repo, account.ID)
	zeroed, ok := repo.extras[account.ID][traeQuotaExtraKey].(map[string]any)
	require.True(t, ok)
	require.EqualValues(t, 0, zeroed["total"])
	require.Equal(t, false, zeroed["has_expire"])
	_, hasPackages := zeroed["packages"]
	require.True(t, hasPackages, "清零快照保留 packages 键，避免消费方读到缺键")
}

// TestTraeCachedTokensReachUsageLog 缓存命中量必须一路走到 usage log 字段
// （trae-switch #154 链路在 so 侧的落点）：上游 usage 报命中量时，
// OpenAI chunk 带 prompt_tokens_details，Anthropic 出口换算成 cache_read。
func TestTraeCachedTokensReachUsageLog(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := []byte(`{"model":"deepseek-v4.1-flash","messages":[{"role":"user","content":"你好"}],"stream":true}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))

	upstreamSSE := strings.Join([]string{
		"event: output",
		`data: {"response":"你好"}`,
		"",
		"event: token_usage",
		`data: {"prompt_tokens":100,"completion_tokens":5,"total_tokens":105,"prompt_cache_hit_tokens":90}`,
		"",
		"event: done",
		`data: {"finish_reason":"stop"}`,
		"",
	}, "\n")
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(upstreamSSE)),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}

	result, err := svc.forwardAsRawChatCompletions(context.Background(), c, traeTestAccount(), body, "")
	require.NoError(t, err)
	require.NotNil(t, result)
	// OpenAI 口径：prompt_tokens 含命中量，InputTokens 保留上游原值，
	// 命中量单列在 CacheReadInputTokens（与 openAIUsageFromGJSON 一致）。
	require.Equal(t, 90, result.Usage.CacheReadInputTokens, "上游命中量必须进 usage 结果")
	require.Equal(t, 100, result.Usage.InputTokens, "prompt_tokens 含命中量，原样保留")

	// 客户端 SSE 里的 usage 也必须带命中量（否则 Trae 线永远显示"缓存不命中"）。
	require.Contains(t, rec.Body.String(), `"cached_tokens":90`)
}
