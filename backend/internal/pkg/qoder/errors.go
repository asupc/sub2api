package qoder

import (
	"fmt"
	"strings"
)

// 失败治理三族（qoder_accounts.py L888-918 常量 + qoder_proxy.py
// open_upstream / _handle_envelope_account_cooldown 的分类逻辑，
// 判据对照 tests/_test_failure_families.py）：
//   - 软限流（429）：第 n 次连续退避 min(600 * 2^(n-1), 7200) 秒；
//   - 硬错误熔断：连续 >= 3 次才起跳，窗口 min(1800 * 2^(n-3), 21600) 秒；
//   - 未知错误降权：连续 >= 5 次起跳，窗口 min(600 * 2^(n-5), 7200) 秒，
//     降权不停用账号（只影响选号优先级）。
//
// 专门路径（不进任何族）：401/403 会话被拒、402 余额停放、内容审核、
// 单账号池（一次排队不能把唯一账号冷死）、信封 429 排队。
const (
	SoftRateBase      = 600.0  // 账号级 429 首次退避：10 分钟
	SoftRateMax       = 7200.0 // 按幂翻倍，封顶 2 小时
	BreakerThreshold  = 3      // 连续硬失败达到该次数才起跳熔断
	BreakerCooldown   = 1800.0 // 首次熔断窗口：30 分钟
	BreakerMax        = 21600.0
	DegradeThreshold  = 5 // 连续未知失败达到该次数才降权
	DegradeCooldown   = 600.0
	DegradeMax        = 7200.0
	errorBackoffSteps = 20 // 最多放大 20 步（防溢出）
)

// FailureFamily 失败分类族。
type FailureFamily string

const (
	// FamilyNone 不计任何族（专门路径 / 单号池 / 内容审核 / 客户端参数错）。
	FamilyNone FailureFamily = "none"
	// FamilySoftRate 账号级软限流（429 类）→ 指数退避。
	FamilySoftRate FailureFamily = "soft_rate"
	// FamilyFailure 硬错误熔断（5xx / 传输类瞬时重试耗尽）。
	FamilyFailure FailureFamily = "failure"
	// FamilyUnknown 未知错误降权窗口（不是不可用）。
	FamilyUnknown FailureFamily = "unknown"
	// FamilySuccess 成功复位（清空三族计数与窗口）。
	FamilySuccess FailureFamily = "success"
)

// 客户端参数/内容审核类标记（qoder_proxy.py L3772-3795）：确定性拒绝，
// 重试无效，不计账号失败。
var (
	clientFaultMarkers = []string{
		"invalid_parameter_error",
		"invalid_request_error",
		"authentication_error",
		"permission_error",
		`"Range of `,
		// 上游内容安全审核：确定性拒绝，重试无效
		"DataInspectionFailed",
		"inappropriate content",
		"input text data may contain",
		"ContentFilter",
		"SensitiveContent",
	}
	contentPolicyMarkers = []string{
		"DataInspectionFailed",
		"inappropriate content",
		"input text data may contain",
		"ContentFilter",
		"SensitiveContent",
	}
)

// transientHTTPCodes 上游瞬时故障码（qoder_proxy.py L3769）：
// 418 = 上游把 provider 故障包装后投递。
var transientHTTPCodes = []int{418, 500, 502, 503, 504}

// TransientMaxRetries 同账号对瞬时故障的额外快速重试次数（1s/2s 退避）。
const TransientMaxRetries = 2

// ExponentialBackoff base * 2^(count-offset)，封顶 cap、最多放大 20 步
// （qoder_accounts.py L898-901）。
func ExponentialBackoff(count int, base, cap float64, offset int) float64 {
	step := count - offset
	if step < 0 {
		step = 0
	}
	if step > errorBackoffSteps {
		step = errorBackoffSteps
	}
	v := base
	for i := 0; i < step; i++ {
		v *= 2
		if v >= cap {
			return cap
		}
	}
	if v > cap {
		return cap
	}
	return v
}

// SoftBackoff 连续 streak 次软限流对应的退避秒数。
func SoftBackoff(streak int) float64 {
	return ExponentialBackoff(streak, SoftRateBase, SoftRateMax, 1)
}

// BreakerBackoff 连续 fails 次硬失败对应的熔断窗口秒数。
func BreakerBackoff(fails int) float64 {
	return ExponentialBackoff(fails, BreakerCooldown, BreakerMax, BreakerThreshold)
}

// DegradeBackoff 连续 fails 次未知失败对应的降权窗口秒数。
func DegradeBackoff(fails int) float64 {
	return ExponentialBackoff(fails, DegradeCooldown, DegradeMax, DegradeThreshold)
}

// IsTransientUpstream 判断一次上游 HTTP 错误是否属于瞬时故障（可重试）
// （qoder_proxy.py L3798-3814）：
//   - 401/403/429 各有专门处理路径，不属瞬时重试类；
//   - 客户端参数/权限/内容审核类错误（clientFaultMarkers）→ 永不重试；
//   - 418 与 5xx → 瞬时；
//   - 其余 4xx 带 provider_error（"Error in upstream response"）→ 瞬时。
func IsTransientUpstream(code int, detail string) bool {
	if code == 401 || code == 403 || code == 429 {
		return false
	}
	for _, m := range clientFaultMarkers {
		if strings.Contains(detail, m) {
			return false
		}
	}
	for _, c := range transientHTTPCodes {
		if code == c {
			return true
		}
	}
	if code >= 500 {
		return true
	}
	if code >= 400 && code < 500 && strings.Contains(detail, "provider_error") {
		return true
	}
	return false
}

// IsContentPolicy 报告错误详情是否为内容安全审核类（确定性拒绝）。
func IsContentPolicy(detail string) bool {
	for _, m := range contentPolicyMarkers {
		if strings.Contains(detail, m) {
			return true
		}
	}
	return false
}

// IsClientFault 报告错误详情是否命中客户端参数/权限类标记。
func IsClientFault(detail string) bool {
	for _, m := range clientFaultMarkers {
		if strings.Contains(detail, m) {
			return true
		}
	}
	return false
}

// ClassifyDirectError 直连层（建流前 HTTP 错误）的失败家族分类
// （qoder_proxy.py L4159-4230，判据对照 _test_failure_families.py B 段）：
//
//	multiAccount = 池内可用账号数 > 1（单号池不接任何族，防一次失败冷死唯一账号）
//	429                 -> SoftRate（multi）/ None（单号）
//	401/403/402/其它 4xx -> None（专门路径：换号冷却，不进三族）
//	瞬时上游故障         -> Failure（multi）
func ClassifyDirectError(statusCode int, detail string, multiAccount bool) FailureFamily {
	switch {
	case statusCode == 429:
		if multiAccount {
			return FamilySoftRate
		}
		return FamilyNone
	case statusCode == 401 || statusCode == 403 || statusCode == 402:
		return FamilyNone
	case IsContentPolicy(detail):
		return FamilyNone
	case IsTransientUpstream(statusCode, detail):
		if multiAccount {
			return FamilyFailure
		}
		return FamilyNone
	default:
		// 其他 4xx（客户端参数/请求形态问题）：快速失败换号，不进三族
		return FamilyNone
	}
}

// ClassifyEnvelopeError 流内信封层（HTTP 200 建流后 statusCodeValue != 200）
// 的失败家族分类（qoder_proxy.py L3870-3915，判据对照 A 段）：
//
//	429（排队）          -> None（仅模型级冷却）
//	401/403              -> None（专门路径，含 10605 排队）
//	内容审核/客户端参数错 -> None
//	其它                 -> Unknown（multi）
func ClassifyEnvelopeError(statusCode int, detail string, multiAccount bool) FailureFamily {
	switch {
	case statusCode == 429:
		return FamilyNone
	case statusCode == 401 || statusCode == 403:
		return FamilyNone
	case !multiAccount:
		return FamilyNone
	case IsClientFault(detail):
		return FamilyNone
	default:
		return FamilyUnknown
	}
}

// IsQueuedReject 报告 401/403 信封是否为 10605 排队（免费档服务不可用，
// retryAfterSeconds 后恢复）——不是鉴权失败，不得触发 token 刷新或罚号。
func IsQueuedReject(detail string) bool {
	return strings.Contains(detail, "10605") || strings.Contains(detail, "isQueued")
}

// RetryAfterSeconds 导出的排队/限流等待秒数解析（detail 无该字段返回 0）。
func RetryAfterSeconds(detail string) int { return parseRetryAfterSeconds(detail) }

// EnvelopeCooldown 信封层错误的冷却时长（qoder_proxy.py L3882-3901）：
//
//	429            -> retryAfterSeconds（解析自 detail，缺省 30）
//	401/403        -> 会话死亡 300s；10605/isQueued 排队 retryAfterSeconds（模型级）；否则 60s
//	其它           -> 15s（单账号池 3s）
func EnvelopeCooldown(statusCode int, detail string, sessionDead, multiAccount bool) (cooldown float64, modelLevel bool) {
	retrySecs := parseRetryAfterSeconds(detail)
	if retrySecs <= 0 {
		retrySecs = 30
	}
	switch {
	case statusCode == 429:
		return float64(retrySecs), true
	case statusCode == 401 || statusCode == 403:
		if sessionDead {
			return 300, false
		}
		isQueue := strings.Contains(detail, "10605") || strings.Contains(detail, "isQueued")
		if isQueue {
			return float64(retrySecs), true
		}
		return 60, false
	default:
		if multiAccount {
			return 15, false
		}
		return 3, false
	}
}

// parseRetryAfterSeconds 从 "10605 isQueued retryAfterSeconds=30" 形态解析
// 冷却秒数（qoder_proxy.py L3883-3889 的正则 retryAfterSeconds\D+(\d+)）。
func parseRetryAfterSeconds(detail string) int {
	i := strings.Index(detail, "retryAfterSeconds")
	if i < 0 {
		return 0
	}
	rest := detail[i+len("retryAfterSeconds"):]
	// 跳过非数字字符后取连续数字
	j := 0
	for j < len(rest) && (rest[j] < '0' || rest[j] > '9') {
		j++
	}
	if j >= len(rest) {
		return 0
	}
	k := j
	for k < len(rest) && rest[k] >= '0' && rest[k] <= '9' {
		k++
	}
	n := 0
	for _, c := range rest[j:k] {
		n = n*10 + int(c-'0')
		if n > 1_000_000 {
			break
		}
	}
	return n
}

// FriendlyUpstreamError 把上游错误转成对客户端可读的消息
// （qoder_proxy.py L3836-3859）：内容审核给确定性说明、瞬时故障给重试指引。
func FriendlyUpstreamError(code int, detail string) (message, errType string) {
	detail = strings.TrimSpace(detail)
	if IsContentPolicy(detail) {
		return fmt.Sprintf("上游内容安全审核未通过 (DataInspectionFailed)：输入可能含不当内容，"+
				"属确定性拒绝、重试无效。请检查/缩短输入（系统提示词、超长历史、"+
				"工具定义或粘贴的代码/文本）后重试。上游详情：%s", clip(detail, 300)),
			"content_policy_rejected"
	}
	if IsTransientUpstream(code, detail) ||
		(strings.Contains(detail, "provider_error") && !strings.Contains(detail, "invalid_")) {
		d := detail
		if d == "" {
			d = "(无详情)"
		}
		return fmt.Sprintf("上游瞬时故障 (HTTP %d)：网关已对同账号自动重试仍失败，"+
			"请稍后重试。上游详情：%s", code, clip(d, 300)), "upstream_transient_error"
	}
	return fmt.Sprintf("upstream %d: %s", code, detail), "upstream_error"
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
