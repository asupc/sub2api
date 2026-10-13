//go:build unit

package qoder

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// 判据对照源项目 tests/_test_failure_families.py：
// A 段（信封层）+ B 段（直连层）逐条对应。
func TestClassifyEnvelopeFamilies(t *testing.T) {
	cases := []struct {
		code   int
		detail string
		multi  bool
		want   FailureFamily
		desc   string
	}{
		{429, "10605 isQueued retryAfterSeconds=30", true, FamilyNone, "A1 信封 429（排队）-> 三族零调用"},
		{502, "unknown envelope failure", true, FamilyUnknown, "A2 信封其它非 200 -> unknown"},
		{400, "InternalError.Algo.DataInspectionFailed: input text", true, FamilyNone, "A3 信封内容审核 -> 零调用"},
		{403, "forbidden", true, FamilyNone, "A4 信封 401/403 -> 零调用（专门路径）"},
		{502, "unknown envelope failure", false, FamilyNone, "A5 单号池信封其它 -> 零调用"},
	}
	for _, c := range cases {
		if got := ClassifyEnvelopeError(c.code, c.detail, c.multi); got != c.want {
			t.Fatalf("%s: got %s want %s", c.desc, got, c.want)
		}
	}
}

func TestClassifyDirectFamilies(t *testing.T) {
	cases := []struct {
		code   int
		detail string
		multi  bool
		want   FailureFamily
		desc   string
	}{
		{429, "too many requests", true, FamilySoftRate, "B1 直连 429 -> soft_rate"},
		{429, "too many requests", false, FamilyNone, "B8 单号池直连 429 -> 零调用"},
		{500, "boom", true, FamilyFailure, "B2 HTTP 500（瞬时重试耗尽）-> failure"},
		{401, "token expired", true, FamilyNone, "B5 直连 401 -> 零调用"},
		{402, "insufficient balance", true, FamilyNone, "B6 直连 402（余额不足）-> 零调用"},
		{400, `{"error":{"type":"invalid_parameter_error","message":"Range of max_tokens"}}`, true, FamilyNone, "客户端参数错 -> 零调用"},
		{418, "provider_error: upstream boom", true, FamilyFailure, "418 provider_error -> failure"},
		{422, "some other 4xx", true, FamilyNone, "其它 4xx 快速失败 -> 零调用"},
	}
	for _, c := range cases {
		if got := ClassifyDirectError(c.code, c.detail, c.multi); got != c.want {
			t.Fatalf("%s: got %s want %s", c.desc, got, c.want)
		}
	}
}

func TestIsTransientUpstream(t *testing.T) {
	if IsTransientUpstream(401, "x") || IsTransientUpstream(403, "x") || IsTransientUpstream(429, "x") {
		t.Fatal("401/403/429 have dedicated paths")
	}
	if IsTransientUpstream(400, "invalid_parameter_error: bad") {
		t.Fatal("client fault must never retry")
	}
	if IsTransientUpstream(400, `DataInspectionFailed input text data may contain inappropriate content`) {
		t.Fatal("content policy must never retry")
	}
	for _, code := range []int{418, 500, 502, 503, 504} {
		if !IsTransientUpstream(code, "x") {
			t.Fatalf("%d must be transient", code)
		}
	}
	if !IsTransientUpstream(400, "Error in upstream response: provider_error") {
		t.Fatal("4xx with provider_error is transient")
	}
}

func TestBackoffCurves(t *testing.T) {
	// 软限流：600 * 2^(n-1) 封顶 7200
	if SoftBackoff(1) != 600 || SoftBackoff(2) != 1200 || SoftBackoff(3) != 2400 {
		t.Fatalf("soft backoff: %v %v %v", SoftBackoff(1), SoftBackoff(2), SoftBackoff(3))
	}
	if SoftBackoff(50) != 7200 {
		t.Fatalf("soft backoff cap: %v", SoftBackoff(50))
	}
	// 熔断窗口公式：1800 * 2^(n-3) 封顶 21600（阈值门控在调用方：
	// fails >= BreakerThreshold 才把窗口写进账号状态）
	if BreakerBackoff(3) != 1800 || BreakerBackoff(4) != 3600 {
		t.Fatalf("breaker backoff: %v %v", BreakerBackoff(3), BreakerBackoff(4))
	}
	if BreakerBackoff(20) != 21600 {
		t.Fatalf("breaker cap: %v", BreakerBackoff(20))
	}
	// 降权窗口公式：600 * 2^(n-5) 封顶 7200（阈值门控同样在调用方）
	if DegradeBackoff(5) != 600 || DegradeBackoff(6) != 1200 {
		t.Fatalf("degrade backoff: %v %v", DegradeBackoff(5), DegradeBackoff(6))
	}
	if DegradeBackoff(20) != 7200 {
		t.Fatalf("degrade cap: %v", DegradeBackoff(20))
	}
}

func TestEnvelopeCooldown(t *testing.T) {
	// 429 -> retryAfterSeconds（模型级）
	cd, model := EnvelopeCooldown(429, "10605 isQueued retryAfterSeconds=30", false, true)
	if cd != 30 || !model {
		t.Fatalf("429: %v %v", cd, model)
	}
	// 403 非排队 -> 账号级 60s
	cd, model = EnvelopeCooldown(403, "forbidden", false, true)
	if cd != 60 || model {
		t.Fatalf("403 plain: %v %v", cd, model)
	}
	// 403 死会话 -> 300s
	cd, _ = EnvelopeCooldown(403, "TOKEN_EXPIRE", true, true)
	if cd != 300 {
		t.Fatalf("dead session: %v", cd)
	}
	// 403 排队 -> retryAfterSeconds 模型级
	cd, model = EnvelopeCooldown(403, "10605 isQueued retryAfterSeconds=45", false, true)
	if cd != 45 || !model {
		t.Fatalf("403 queue: %v %v", cd, model)
	}
	// 其它 -> 15s（多号）/ 3s（单号）
	cd, _ = EnvelopeCooldown(502, "unknown", false, true)
	if cd != 15 {
		t.Fatalf("other multi: %v", cd)
	}
	cd, _ = EnvelopeCooldown(502, "unknown", false, false)
	if cd != 3 {
		t.Fatalf("other single: %v", cd)
	}
}

func TestParseRetryAfterSeconds(t *testing.T) {
	if parseRetryAfterSeconds("10605 isQueued retryAfterSeconds=30") != 30 {
		t.Fatal("standard form")
	}
	if parseRetryAfterSeconds("retryAfterSeconds: 75") != 75 {
		t.Fatal("colon form")
	}
	if parseRetryAfterSeconds("no marker") != 0 {
		t.Fatal("missing marker")
	}
}

func TestFriendlyUpstreamError(t *testing.T) {
	msg, etype := FriendlyUpstreamError(418, "DataInspectionFailed: input text may contain inappropriate content")
	if etype != "content_policy_rejected" || !strings.Contains(msg, "重试无效") {
		t.Fatalf("content policy: %s %s", msg, etype)
	}
	msg, etype = FriendlyUpstreamError(502, "upstream boom")
	if etype != "upstream_transient_error" || !strings.Contains(msg, "请稍后重试") {
		t.Fatalf("transient: %s %s", msg, etype)
	}
	msg, etype = FriendlyUpstreamError(400, "invalid_parameter_error")
	if etype != "upstream_error" {
		t.Fatalf("plain: %s %s", msg, etype)
	}
}

func TestIsQueuedRejectAndRetryAfterSeconds(t *testing.T) {
	// 10605 排队（403 信封）与普通 401/403 鉴权失败必须可区分：
	// 排队不得触发 token 刷新或罚号（service 层据此分流）。
	require.True(t, IsQueuedReject(`{"code":"10605","message":"{\"isQueued\":true,\"retryAfterSeconds\":30}"}`))
	require.True(t, IsQueuedReject("upstream isQueued p3"))
	require.False(t, IsQueuedReject("401 unauthorized"))
	require.False(t, IsQueuedReject(""))

	require.Equal(t, 30, RetryAfterSeconds("10605 isQueued retryAfterSeconds=30"))
	require.Equal(t, 45, RetryAfterSeconds(`{"retryAfterSeconds":45}`))
	require.Equal(t, 0, RetryAfterSeconds("no retry field"))
}
