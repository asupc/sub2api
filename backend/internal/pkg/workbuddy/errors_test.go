//go:build unit

package workbuddy

import (
	"errors"
	"net/http"
	"testing"
	"time"
)

// TestClassifyError 11 层判定链表驱动（用源项目真实错误报文样例）。
func TestClassifyError(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   ErrorKind
	}{
		// 第 1 层：402 计费耗尽。
		{402, ``, KindHardCredit},
		{402, `{"code":1,"msg":"余额不足"}`, KindHardCredit},
		{403, `insufficient credits`, KindHardCredit},
		{200, `{"code":10001,"msg":"积分不足，请充值"}`, KindHardCredit},
		{400, `{"code":1,"msg":"额度用尽"}`, KindHardCredit},
		{200, `quota exceeded`, KindHardCredit},
		// 第 2 层：session 死亡优先于限流文案（401+12153 需人工重登）。
		{401, `{"code":12153,"msg":"Offline user session not found, rate limit"}`, KindSessionDead},
		{401, `Offline user session not found`, KindSessionDead},
		{401, `{"code":12153,"msg":"Offline user session not found"}`, KindSessionDead},
		{401, `{"code":9999,"msg":"bad token"}`, KindClient},
		// 第 3 层：账号级故障先于 429（429+14017 不得误归限流）。
		{429, `{"code":14017,"msg":"trial not activated"}`, KindAccountFault},
		{429, `{"error":{"data":{"code":11140,"msg":"request illegal"}}}`, KindAccountFault},
		{403, `{"code":11140,"msg":"request illegal"}`, KindAccountFault},
		// 第 3 层：11-128 / 11140 同文案按业务码分流（7a8b7a7）。上游真实 code 为
		// **字符串** "11-128"（带连字符、带引号，非 JSON 数字）。
		{400, `{"code":"11-128","msg":"request illegal","displayTips":{"en":"channel verification failed"}}`, KindChannelRefused},
		{403, `{"code":"11-128","msg":"request illegal"}`, KindChannelRefused},
		{400, `{"error":{"data":{"code":"11-128","msg":"request illegal"}}}`, KindChannelRefused},
		{400, `{"code":11140,"msg":"request illegal"}`, KindAccountFault},                    // 11140 账号限制
		{200, `{"code":11140,"msg":"The model provider is rate-limiting requests."}`, KindSoftRate}, // 11140 限流变体不误判
		{400, `{"code":"11-128","msg":"blocked by security policy"}`, KindContentBlocked},   // 11-128 审核变体不误判
		// 第 5 层前置：6021 queue.full 渠道级繁忙（f14f374），先于 429 兜底。
		{429, `{"code":6021,"msg":"queue.full.title"}`, KindUpstreamBusy},
		{429, `{"error":{"data":{"code":"6021","msg":"queue.full.title"}}}`, KindUpstreamBusy},
		{429, `queue full`, KindUpstreamBusy},   // 文案外卡（防上游换状态码/丢 code）
		{503, `{"code":6021,"msg":"queue.full.title"}`, KindUpstreamBusy},
		{429, `{"code":1,"msg":"queueing, please wait"}`, KindSoftRate}, // 普通排队文案不误撞
		// 第 4 层：429 + 14018 = 明确积分耗尽。
		{429, `{"code":14018,"msg":"Credits exhausted"}`, KindHardCredit},
		{429, `{"error":{"data":{"code":"14018","msg":"Credits exhausted"}}}`, KindHardCredit},
		// 第 5 层：429 兜底优先于余额措辞（限流响应常带 quota 字样）。
		{429, ``, KindSoftRate},
		{429, `quota exceeded`, KindSoftRate},
		{429, `{"code":1,"msg":"quota exceeded, please wait"}`, KindSoftRate},
		{429, `insufficient credits`, KindSoftRate},
		{429, `积分不足，请充值`, KindSoftRate},
		{429, `{"code":1,"msg":"Credits exhausted"}`, KindSoftRate}, // 无 14018 码保持软限流
		{429, `{"requestId":"14018","msg":"Credits exhausted"}`, KindSoftRate},
		// 第 0 层（最先判）：11102 模型封禁。
		{404, `{"code":11102,"msg":"model [deepseek-v3-2-volc] service info not found"}`, KindModelBlocked},
		{400, `{"error":{"code":"11102","message":"model service info not found"}}`, KindModelBlocked},
		{400, `{"msg":"service info not found"}`, KindModelBlocked},
		{404, `{"requestId":"11102","msg":"ok"}`, KindNotFound}, // 撞 ID 不算
		{429, `{"code":11102,"msg":"service info not found"}`, KindSoftRate},
		// 11158 模型权限（6ee5491）：与 11102 同层判定。
		{400, `{"code":11158,"msg":"model [deepseek] is not allowed for user type"}`, KindModelBlocked},
		{400, `{"code":11158,"msg":"permission denied"}`, KindModelBlocked},
		{400, `{"msg":"model xxx is not allowed for user type"}`, KindModelBlocked},
		{429, `{"code":11158,"msg":"is not allowed for user type"}`, KindSoftRate}, // 429 带码仍属限流语义
		// 第 7 层：非 429 状态码携带限流文案。
		{200, `{"code":11140,"msg":"The model provider is rate-limiting requests. Please wait a moment and try again."}`, KindSoftRate},
		{400, `rate limit`, KindSoftRate},
		{403, `usage limit reached`, KindSoftRate},
		{200, `{"code":1,"msg":"model usage limit exceeded"}`, KindSoftRate},
		{200, `{"code":1,"msg":"too many requests"}`, KindSoftRate},
		{500, `rate-limited upstream`, KindSoftRate}, // 限流文案优先于 5xx
		// 第 8 层：11115 prompt too long（请求级，判在 404/5xx 前）。
		{400, `{"code":11115,"msg":"prompt is too long"}`, KindPromptTooLong},
		{404, `{"code": 11115,"msg":"Prompt is too long"}`, KindPromptTooLong},
		{413, `"code":"11115"`, KindPromptTooLong},
		// 第 9 层：404 / 5xx。
		{404, `not found`, KindNotFound},
		{500, `boom`, KindServer},
		{503, `unavailable`, KindServer},
		// 第 10 层：WAF 403（无业务信封）。
		{403, ``, KindWafBlock},
		{403, `<html><body>403 Forbidden</body></html>`, KindWafBlock},
		{403, `Forbidden`, KindWafBlock},
		{403, `{"message":"blocked by waf"}`, KindWafBlock},
		{403, `{"code":11128,"msg":"blocked by security policy"}`, KindContentBlocked}, // 带信封不进 WAF
		{403, `{"code":60001,"msg":"quota exceeded"}`, KindHardCredit},
		{403, `{"code":1,"msg":"unknown business error"}`, KindClient},
		// 第 11 层：内容策略 / 参数错误 / 图片无效 / 通用 4xx。
		{400, `Illegal API invocation from an unapproved channel`, KindContentBlocked},
		{400, `{"code":11128,"msg":"blocked by security policy"}`, KindContentBlocked},
		{400, `{"code":11101,"msg":"Unmarshal chat params failed with error: unexpected EOF"}`, KindBadParams},
		{400, `Unmarshal chat params failed`, KindBadParams},
		{400, `{"code":11101,"msg":"x"}`, KindBadParams},
		{400, `{"code":11101,"msg":"Parse message failed: invalid image_url content at index 2: json: cannot unmarshal string into Go value of type v2.ImageContent"}`, KindImageInvalid},
		{400, `{"code":11135,"msg":"invalid_image_data"}`, KindImageInvalid},
		{400, `{"code": 11135, "msg":"image data invalid"}`, KindImageInvalid}, // JSON 空白容差
		{400, `{"error":{"code": "11135", "message":"image invalid"}}`, KindImageInvalid},
		{400, `invalid_image_data`, KindImageInvalid},
		{400, `{"code": 11133, "msg":"model does not support image"}`, KindClient}, // 11133 不进 image_invalid
		{400, `bad request`, KindClient},
		// 成功形态。
		{200, ``, KindNone},
	}
	for _, c := range cases {
		if got := ClassifyError(c.status, []byte(c.body)); got != c.want {
			t.Errorf("ClassifyError(%d,%q)=%v want %v", c.status, c.body, got, c.want)
		}
	}
}

// TestIsModelRateLimit 429 body 的模型级限流判定（code 6004）。
func TestIsModelRateLimit(t *testing.T) {
	cases := []struct {
		body string
		want bool
	}{
		{`{"code":6004,"msg":"将在 2026-09-11 18:33:27 UTC+8 重置"}`, true},
		{`{"code": 6004,"msg":"x"}`, true},
		{`{"code":"6004","msg":"x"}`, true},
		{`{"code":11140,"msg":"The model provider is rate-limiting requests."}`, false},
		{`{"msg":"6004 things"}`, false},
	}
	for _, c := range cases {
		if got := IsModelRateLimit(c.body); got != c.want {
			t.Errorf("IsModelRateLimit(%q)=%v want %v", c.body, got, c.want)
		}
	}
}

// TestParseRateReset 重置时间解析（CN/EN 双形态，固定 UTC+8）。
func TestParseRateReset(t *testing.T) {
	at, ok := ParseRateReset(`{"code":6004,"msg":"将在 2026-09-11 18:33:27 UTC+8 重置"}`)
	if !ok {
		t.Fatal("CN 文案应解析成功")
	}
	want := time.Date(2026, 9, 11, 18, 33, 27, 0, softRateResetLoc)
	if !at.Equal(want) {
		t.Errorf("CN 解析=%v want %v", at, want)
	}
	at, ok = ParseRateReset(`{"msg":"will reset at 2026-09-12 09:00:00 UTC+8"}`)
	if !ok {
		t.Fatal("EN 文案应解析成功")
	}
	want = time.Date(2026, 9, 12, 9, 0, 0, 0, softRateResetLoc)
	if !at.Equal(want) {
		t.Errorf("EN 解析=%v want %v", at, want)
	}
	if _, ok := ParseRateReset(`{"msg":"no time here"}`); ok {
		t.Error("无时间文案应返回 false")
	}
	if _, ok := ParseRateReset(`{"msg":"reset at the end of the day"}`); ok {
		t.Error("自然语言时间不应匹配")
	}
}

// TestParseRetryAfter Retry-After 头族解析。
func TestParseRetryAfter(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "30")
	if d, ok := ParseRetryAfter(h); !ok || d != 30*time.Second {
		t.Errorf("Retry-After=30 → %v %v", d, ok)
	}
	h = http.Header{}
	h.Set("Retry-After-Ms", "1500")
	if d, ok := ParseRetryAfter(h); !ok || d != 1500*time.Millisecond {
		t.Errorf("Retry-After-Ms=1500 → %v %v", d, ok)
	}
	// epoch 毫秒口径折算剩余量（13 位数字串）。
	h = http.Header{}
	future := time.Now().Add(90 * time.Second).UnixMilli()
	h.Set("X-Ratelimit-Reset", itoa(future))
	d, ok := ParseRetryAfter(h)
	if !ok || d <= 0 || d > 90*time.Second+5*time.Second {
		t.Errorf("X-Ratelimit-Reset(epoch ms) → %v %v", d, ok)
	}
	// 非法/缺失/超限全部回落 false。
	for _, v := range []string{"", "abc", "-5", "99999999999999999999999"} {
		h = http.Header{}
		h.Set("Retry-After", v)
		if _, ok := ParseRetryAfter(h); ok {
			t.Errorf("Retry-After=%q 不应解析成功", v)
		}
	}
}

// itoa 简单整数转字符串（测试内联用）。
func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// TestIsAlreadyCheckin 已签到幂等判定。
func TestIsAlreadyCheckin(t *testing.T) {
	err1 := &Error{Kind: KindClient, Status: 400, Msg: `code=10001 msg=今天已签到`}
	if !IsAlreadyCheckin(err1) {
		t.Error("中文「已签到」应命中")
	}
	err2 := &Error{Kind: KindClient, Status: 400, Msg: `code=14001 msg=Already checked in today`}
	if !IsAlreadyCheckin(err2) {
		t.Error("英文 already 应命中")
	}
	if IsAlreadyCheckin(&Error{Kind: KindServer, Status: 500, Msg: "boom"}) {
		t.Error("5xx 不是幂等信号")
	}
	if IsAlreadyCheckin(errors.New("network")) {
		t.Error("网络层错误不是幂等信号")
	}
}

// TestClassifyRealChannelRefusedBody 用上游**真实** 11-128 响应体（code 为字符串
// "11-128"，带连字符，见 workbuddy2api-panel issue #197）钉住分流。护栏来历：源项目
// PR #233 提交时字面量里的连字符丢失（实现与测试同写成错误码值），双方自洽所以测试
// 全绿，但线上永远命中不了真实业务码——本用例用字节构造业务码，防止「实现和测试
// 一起写错」再次通过（移植自源 d809396）。
func TestClassifyRealChannelRefusedBody(t *testing.T) {
	code := string([]rune{49, 49, 45, 49, 50, 56}) // "11-128"
	body := `{"code":"` + code + `","msg":"request illegal","requestId":"b1bc87ea","displayTips":{"en":"This call failed channel verification."}}`
	if got := ClassifyError(400, []byte(body)); got != KindChannelRefused {
		t.Fatalf("ClassifyError(real 11-128)=%v want KindChannelRefused", got)
	}
	if got := ClassifyError(403, []byte(`{"code":"`+code+`","msg":"request illegal"}`)); got != KindChannelRefused {
		t.Fatalf("ClassifyError(real 11-128, 403)=%v want KindChannelRefused", got)
	}
}

// TestModelBlockReasonFor 11158 与 11102 的负缓存 reason 前缀区分（6ee5491）。
func TestModelBlockReasonFor(t *testing.T) {
	cases := []struct {
		body string
		want string
	}{
		{`{"code":11158,"msg":"model is not allowed for user type"}`, "11158"},
		{`{"code":11102,"msg":"service info not found"}`, "11102"},
		{`{"msg":"model xxx is not allowed for user type"}`, "11158"},
		{`{"msg":"service info not found"}`, "11102"},
	}
	for _, c := range cases {
		if got := ModelBlockReasonFor(c.body); got != c.want {
			t.Errorf("ModelBlockReasonFor(%q)=%q want %q", c.body, got, c.want)
		}
	}
}
