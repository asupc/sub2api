// errors.go 上游错误分类：按 HTTP 状态码 + body 判定语义类别（供上层映射账号状态）。
//
// 移植自 workbuddy2api-panel internal/upstream/client.go 的 Classify（11 层判定链），
// 关键词与判定顺序逐字保留——顺序漂移会造成分类语义变化（见各层注释的先后依据）。
package workbuddy

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// ErrorKind 错误语义类别（字符串形态，便于日志与上层映射）。
type ErrorKind string

const (
	// KindNone 成功（HTTP 2xx 且业务 code==0）。
	KindNone ErrorKind = "none"
	// KindHardCredit 余额不足（402 或 body 关键词 / 429+14018）→ 长冷却。
	KindHardCredit ErrorKind = "hard_credit"
	// KindSoftRate 429 软限流 → 短冷却。
	KindSoftRate ErrorKind = "soft_rate"
	// KindSessionDead 401 + 12153 offline session 失效 → 需重新登录。
	KindSessionDead ErrorKind = "session_dead"
	// KindNotFound 404 上游偶发 → 短冷却，不累计错误计数（防雪崩）。
	KindNotFound ErrorKind = "not_found"
	// KindServer 5xx 上游故障。
	KindServer ErrorKind = "server"
	// KindContentBlocked 内容策略拦截（400 + 审核文案）→ 不罚账号，走降级重试。
	KindContentBlocked ErrorKind = "content_blocked"
	// KindBadParams 请求体解析失败（400 + Unmarshal chat params failed / 11101）→ 请求级错误，不罚号。
	KindBadParams ErrorKind = "bad_params"
	// KindAccountFault 账号级授权/配额故障（11140 request illegal / 14017 trial not activated）→ 冷却轮换。
	KindAccountFault ErrorKind = "account_fault"
	// KindChannelRefused 11-128「request illegal」通道校验失败（上游认为请求不是官方
	// 客户端发出）→ 账号本身健康（官方客户端可正常使用同一账号），不罚账号，由轮转换号。
	// 移植自 workbuddy2api-panel 7a8b7a7：11-128 与 11140 的 msg 同为 "request illegal"，
	// 只看文案会把通道校验失败误判成账号封禁。
	KindChannelRefused ErrorKind = "channel_refused"
	// KindUpstreamBusy 6021「queue.full.title」渠道级容量满载（上游推理集群排队打满）→
	// 非账号问题：不罚号、不轮转（换任何账号都撞同一堵墙），透传让客户端退避重试。
	// 移植自 workbuddy2api-panel f14f374：按限流处置 2 秒即可把整池健康号全冷却误杀。
	KindUpstreamBusy ErrorKind = "upstream_busy"
	// KindModelBlocked 11102「该后端无此模型」→ (账号,模型) 负缓存避让。
	KindModelBlocked ErrorKind = "model_blocked"
	// KindWafBlock 403 + 非业务信封体（APISIX WAF 拦截页/空体）→ 账号软冷却 + 退避。
	KindWafBlock ErrorKind = "waf_block"
	// KindPromptTooLong 11115「prompt is too long」→ 请求级错误，不罚号。
	KindPromptTooLong ErrorKind = "prompt_too_long"
	// KindImageInvalid 图片请求格式/数据无效 → 请求级错误，不罚号。
	KindImageInvalid ErrorKind = "image_invalid"
	// KindClient 其他 4xx / 业务错误。
	KindClient ErrorKind = "client"
)

// Error 带分类的上游错误。
type Error struct {
	Kind   ErrorKind
	Status int
	Msg    string
	// RawBody 上游原始错误 body（截断），供调用方透传原文给客户端。
	RawBody string
	// RetryAfter 上游明示的等待时长（Retry-After / retry-after-ms / x-ratelimit-reset
	// 头解析，见 ParseRetryAfter）。零值 = 上游未明示，冷却时长回落调用方计算值。
	RetryAfter time.Duration
}

func (e *Error) Error() string {
	return fmt.Sprintf("workbuddy upstream %s (http %d): %s", e.Kind, e.Status, e.Msg)
}

// hardMarkers 余额不足关键词（小写比较 + 中文原文比较双通道）。
var hardMarkers = []string{
	"insufficient credit", "no credit", "credit exhausted", "credits exhausted", "out of credit",
	"quota exceeded", "quota exhaust", "payment required", "credit not enough",
	"not enough credit",
	"积分不足", "额度不足", "余额不足", "积分用完", "额度用尽", "没有积分",
}

// softRateMarkers 限流/节流关键词（小写比较 + 中文原文比较双通道）。
// 上游在状态码非 429 时也会返回限流语义（如 200 + code 11140
// "The model provider is rate-limiting requests."、400 + "rate limit"），
// 此类响应若不识别，账号既不被冷却也不喂熔断，下次请求仍会被选中。
// 词表按子串匹配，宁缺毋滥：只收录明确指向「请求速率/模型用量被节流」的措辞。
// 连字符形式（rate-limiting / rate-limited）需单列——Contains 不跨 '-'。
var softRateMarkers = []string{
	"rate limit", // rate limit / rate limits / rate limiting
	"rate-limiting",
	"rate-limited",
	"too many requests",
	"too many",
	"usage limit", // usage limit reached / model usage limit exceeded（用量节流，非计费余额）
	"请求过于频繁", "限流",
}

// sessionDeadMarkers session 失效精确词（12153 业务码 / 官方文案）。
var sessionDeadMarkers = []string{"Offline user session not found", "12153"}

// accountFaultMarkers 账号级授权/配额故障关键词（大小写不敏感子串匹配）：
//   - code 14017（"trial not activated"）→ register 未完成的试用未激活账号。
//
// 注意 "request illegal" **不放进本词表**：WorkBuddy 的 11-128 通道校验失败
// （`{"code":11-128,"msg":"request illegal"}`）与 11140 账号限制
// （`{"code":11140,"msg":"request illegal"}`）文案完全相同，语义相反。两者的判定
// 统一走 IsAccountRestricted / isChannelRefused（code + 文案双条件，见 ClassifyError
// 注释）。另：11140 **不能**按 code 单独判定——该 code 也承载模型级限流文案
// （"The model provider is rate-limiting requests."），那种场景必须保持 KindSoftRate。
var accountFaultMarkers = []string{
	"trial not activated",
	"trial version is not yet activated",
}

// accountRestrictedMsg WorkBuddy 11140 账号限制与 11-128 通道拒绝共用的正文关键词。
const accountRestrictedMsg = "request illegal"

// IsAccountRestricted 报告上游 body 是否为 WorkBuddy 的账号级限制（11140
// "request illegal"）。必须 code + 文案双条件：
//   - 只看文案："request illegal" 也出现在 11-128 通道校验失败里（账号健康）；
//   - 只看 code：11140 也承载模型级限流文案（应归 KindSoftRate）。
//
// 移植自 workbuddy2api-panel 7a8b7a7（IsAccountRestricted）。
func IsAccountRestricted(body string) bool {
	if !strings.Contains(strings.ToLower(body), accountRestrictedMsg) {
		return false
	}
	return hasBusinessCode(body, "11140")
}

// isChannelRefused 报告上游 body 是否为 WorkBuddy 的通道校验失败（11-128
// "request illegal"）。11-128 还承载内容审核变体（"blocked by security policy"，
// 归 KindContentBlocked），故必须 code + 文案双条件，不能按 code 一刀切。
// 移植自 workbuddy2api-panel 7a8b7a7（isChannelRefused）。
func isChannelRefused(body string) bool {
	if !strings.Contains(strings.ToLower(body), accountRestrictedMsg) {
		return false
	}
	return hasBusinessCode(body, "11-128")
}

// isQueueFullBody 判定渠道级容量满载文案（6021 queue.full.title 家族）。该形态表明
// 上游推理集群排队队列打满，与具体账号无关；换号在任何时刻都会撞同一堵墙。子串口径
// 但用完整固定文案（"queue.full"/"queue full"），避免与普通限流文案误撞。
// 移植自 workbuddy2api-panel f14f374（isQueueFullBody）。
func isQueueFullBody(lower string) bool {
	return strings.Contains(lower, "queue.full") || strings.Contains(lower, "queue full")
}

// contentBlockedMarkers 内容策略拦截关键词：上游按逐字精确指纹审核，system 来源的
// 模板句触发 HTTP 400 + 以下文案。属「误报」非账号问题——不罚账号，由网关降级重试。
var contentBlockedMarkers = []string{
	"blocked by security policy",
	"unapproved channel",
	"illegal api invocation",
}

// badParamsMarkers 请求体解析失败关键词：HTTP 400 + "Unmarshal chat params
// failed..."（code 11101）。与账号健康无关——换账号同样 400，不罚号。
var (
	badParamsMarkerMsg  = "Unmarshal chat params failed"
	badParamsMarkerCode = `"code":11101`
)

// invalidImageMarkers 图片请求格式/数据无效（HTTP 400）的文案形态。
// 业务码 11135 不放这里：code 判定必须容忍 JSON 空白（`"code": 11135`），
// 字面量 marker 只能覆盖紧凑形态，故统一走 codeMarker。
var invalidImageMarkers = []string{
	"invalid image_url content",
	"invalid_image_data",
	"replace the image",
}

// promptTooLongMarkers 上下文超限 marker 双通道：
//   - `"code":11115`：业务信封 code 字段（JSON 空格容差；字符串形态也命中）；
//   - "prompt is too long"：msg 文案（大小写不敏感）。
//
// 只在 400/404/413 请求级状态码上判（429+11115 概率极低且属限流语义优先，
// 5xx 属服务端故障优先）。
var promptTooLongMarkers = []string{
	`"code":11115`,
	`"code": 11115`,
	`"code":"11115"`,
	"prompt is too long",
}

// alreadyCheckinMarkers 「今天已签到」关键词（上游对重复签到返回 code!=0，
// 实测 code=10001/14001）。只对 *Error.Msg 做包含匹配（见 IsAlreadyCheckin）。
var alreadyCheckinMarkers = []string{"已签到", "already"}

// modelRateLimitCode 明确指向「模型级 429 限流」的业务 code（6004）：
// 「该模型的使用量超限」而非账号整体被限——账号健康，只是这个模型此刻被限。
const modelRateLimitCode = "6004"

// modelBlockCode / modelBlockMsgMarker 11102「该后端无此模型」的确定性判定。
// msg 只收窄短语 "service info not found"，不收 "model ... not found" 宽正则。
//
// modelForbiddenCode / modelForbiddenMsgMarker 11158「该账号类型无权使用该模型」
// 的确定性判定（移植自 workbuddy2api-panel 6ee5491）。同一模型在不同账号类型上
// 权限不同，与 11102 同属 (账号, 模型) 级确定性拒绝：换有权限的账号仍可能成功，
// 全部账号都拒绝时才向客户端报模型不可用。
const (
	modelBlockCode          = "11102"
	modelBlockMsgMarker     = "service info not found"
	modelForbiddenCode      = "11158"
	modelForbiddenMsgMarker = "is not allowed for user type"
)

// softRateResetLoc 上游 429 重置时间固定按 UTC+8 解释（上游文案如此，与时区无关）。
var softRateResetLoc = time.FixedZone("UTC+8", 8*60*60)

// 限流判定正则预编译（错误风暴时每次分类都调用，函数体内 MustCompile 是纯浪费）。
var (
	reModelRateLimit  = regexp.MustCompile(`"code"\s*:\s*"?` + modelRateLimitCode + `"?`)
	reSoftRateResetCN = regexp.MustCompile(`将在 (.+?) 重置`)
	reSoftRateResetEN = regexp.MustCompile(`(?i)reset at (\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})`)
)

// softRateTimeLayout 上游重置时间的格式（无时区后缀；时区固定 UTC+8）。
const softRateTimeLayout = "2006-01-02 15:04:05"

// isPromptTooLongStatus 11115 只在请求级 4xx 上判（见 promptTooLongMarkers 注释）。
func isPromptTooLongStatus(status int) bool {
	return status == http.StatusBadRequest || status == http.StatusNotFound ||
		status == http.StatusRequestEntityTooLarge
}

// IsModelRateLimit 报告 429 body 是否明确指向模型级限流（业务 code 6004）。
// 用于区分「账号级软限流」（按账号冷却）与「模型级用量限流」（切模型即可用）。
func IsModelRateLimit(body string) bool {
	// `"code":6004` / `"code": 6004` / `"code":"6004"` 均可命中（JSON 空格容差）。
	return reModelRateLimit.MatchString(body)
}

// IsModelBlocked 报告 body 是否是「该后端无此模型」(11102) 或「该账号类型无权使用
// 该模型」(11158) 的确定性答复。
//
// 只比对 code/msg 等独立字段，绝不做整段文本子串匹配：错误体还带 requestId 等字段，
// 拿整段文本匹配会把 code 撞在 ID 上、误避让一个本来能用的模型。判定 =
// code 字段精确等于 "11102"/"11158"，或 msg/message 字段命中对应窄短语。
// 只看 400/404：429 带这些 code 属限流语义。字段遍历覆盖顶层与 error 子对象两层。
// 移植自 workbuddy2api-panel 6ee5491（合并 11158 判定）。
func IsModelBlocked(status int, body string) bool {
	if (status != http.StatusBadRequest && status != http.StatusNotFound) || body == "" {
		return false
	}
	lower := strings.ToLower(body)
	// 轻量预检：body 无任何已知 code/marker 时直接短路（大多数 4xx 零分配返回）。
	if !strings.Contains(body, modelBlockCode) && !strings.Contains(body, modelForbiddenCode) &&
		!strings.Contains(lower, modelBlockMsgMarker) && !strings.Contains(lower, modelForbiddenMsgMarker) {
		return false
	}
	code, msg := modelBlockFields(body)
	if code == modelBlockCode || code == modelForbiddenCode {
		return true
	}
	lowerMsg := strings.ToLower(msg)
	return strings.Contains(lowerMsg, modelBlockMsgMarker) || strings.Contains(lowerMsg, modelForbiddenMsgMarker)
}

// ModelBlockReasonFor 返回确定性模型拒绝对应的负缓存 reason 前缀（"11102"/"11158"），
// 供上层 (账号,模型) 级锁池区分两种拒绝。11158 是账号类型权限问题；其余已由
// IsModelBlocked 确认的形态沿用 11102。移植自 workbuddy2api-panel 6ee5491。
func ModelBlockReasonFor(body string) string {
	code, msg := modelBlockFields(body)
	if code == modelForbiddenCode || strings.Contains(strings.ToLower(msg), modelForbiddenMsgMarker) {
		return modelForbiddenCode
	}
	return modelBlockCode
}

// modelBlockFields 提取顶层或 error 子对象中的业务 code 与消息。
func modelBlockFields(body string) (code, msg string) {
	var root map[string]any
	if err := json.Unmarshal([]byte(body), &root); err != nil {
		return "", ""
	}
	nodes := []map[string]any{root}
	if inner, ok := root["error"].(map[string]any); ok {
		nodes = append(nodes, inner)
	}
	for _, node := range nodes {
		for _, key := range []string{"code", "errCode", "error_code"} {
			if v, ok := node[key]; ok && v != nil && code == "" {
				code = strings.TrimSpace(fmt.Sprint(v))
			}
		}
		for _, key := range []string{"msg", "message"} {
			if v, ok := node[key].(string); ok && v != "" && msg == "" {
				msg = strings.TrimSpace(v)
			}
		}
	}
	return code, msg
}

// hasBusinessCode 报告 JSON 错误信封（含嵌套 error/data 对象）中是否存在精确等于
// want 的业务 code 字段。
func hasBusinessCode(body, want string) bool {
	var root any
	if err := json.Unmarshal([]byte(body), &root); err != nil {
		return false
	}
	var walk func(any) bool
	walk = func(value any) bool {
		switch node := value.(type) {
		case map[string]any:
			if code, ok := node["code"]; ok && strings.TrimSpace(fmt.Sprint(code)) == want {
				return true
			}
			for _, child := range node {
				if walk(child) {
					return true
				}
			}
		case []any:
			for _, child := range node {
				if walk(child) {
					return true
				}
			}
		}
		return false
	}
	return walk(root)
}

// hasBusinessEnvelope 报告错误 body 是否携带上游业务信封形态（含 `"code":` 或
// `"msg":` 字段）。WAF 403 判定用「无业务信封」区分 APISIX WAF 拦截页（HTML/空体/
// 纯文本）与上游业务层 403。不做 JSON 解析：畸形 JSON 但含 `"msg":` 字样仍按业务
// 响应保守处理（宁漏判 WAF 也不误罚业务 403）。
func hasBusinessEnvelope(body string) bool {
	return strings.Contains(body, `"code":`) || strings.Contains(body, `"msg":`)
}

// IsWafBlocked 报告 403 响应是否为 WAF 拦截形态：HTTP 403 且 body 无业务信封。
// 带业务信封的 403（11140 request illegal / 11128 等）仍走既有分类链。
func IsWafBlocked(status int, body string) bool {
	return status == http.StatusForbidden && !hasBusinessEnvelope(body)
}

// codeMarker 报告小写 body 中是否含指定业务 code 的字段形态（JSON 空白与引号容差）。
func codeMarker(lower, code string) bool {
	for _, v := range []string{
		`"code":` + code, `"code": ` + code, `"code":"` + code + `"`,
		`"code": "` + code + `"`, `"code":" ` + code + `"`, `"code": '` + code + `'`,
	} {
		if strings.Contains(lower, v) {
			return true
		}
	}
	return false
}

// ClassifyError 按 HTTP 状态码 + body 判定错误类别（纯函数）。
//
// 判定顺序自「严」到「宽」，每层的先后都有语义依据：
//  0. 11102/11158（IsModelBlocked）——「该后端无此模型/该账号类型无权使用该模型」
//     确定性答复，语义最具体，最先判（只认 400/404，429+11102 属限流语义走限流层）。
//  1. 402 —— 真正的计费余额耗尽状态码，最严、最不可自愈，最先判。
//  2. sessionDeadMarkers —— 需要人工重登的终态。若 401 body 同时含 "12153" 与
//     "rate limit"（网关错误页混排），归 session_dead：短冷却救不活失效 session。
//  3. 11-128/11140 业务码分流（isChannelRefused/IsAccountRestricted）——两者 msg 同为
//     "request illegal" 但语义相反（通道拒绝 vs 账号限制），code + 文案双条件判定；
//     之后 accountFaultMarkers 兜底其余账号级文案（14017 trial not activated）。
//     必须先于 status==429 判定：14017 常带 429 状态码，若落到 429 兜底会误归
//     soft_rate（账号级故障等不来自愈）。
//  4. 429 + code 14018 —— 明确的账号积分耗尽，归 hard_credit。只认结构化业务码。
//  5. 6021 queue.full —— 渠道级容量满载，归 upstream_busy：不罚号不轮转。判在
//     status==429 兜底之前（比「限流」更具体的渠道级信号，同 14018 的具体优先哲学）。
//  6. status==429 —— 限流状态码兜底（先于 hardMarkers）：429 body 高频携带
//     "quota exceeded"/"额度不足" 等跨计费/限流两界的措辞，hardMarkers 先判会把
//     限流误归 hard_credit 硬冷却到次日 04:00，白扔号约 12h。状态码是比关键词
//     更权威的信号；真正的余额耗尽由 402（第 1 层）或 14018（第 4 层）捕获。
//  7. hardMarkers —— 非 429 响应携带计费关键词（200 业务信封 / 403 信封等）。
//  8. softRateMarkers —— 非 429 状态码携带限流文案（覆盖 200/400/403/5xx）。
//  9. 11115 —— 「prompt is too long」请求级语义：判在 404/5xx 与通用 4xx 兜底
//     之前（404 上打 11115 若落 not_found 会误冷却账号——上下文超限与账号无关）。
//  10. 404 / 5xx —— 与限流无关的常规分类。
//  11. IsWafBlocked —— 403 且无业务信封（HTML 拦截页/空体/纯文本）。判在通用 4xx
//     兜底之前：此前该形态落 client 兜底 → 只换号不罚 → 连环 403。
//  12. 内容策略/参数错误/图片无效/其他 4xx —— 通用兜底。
func ClassifyError(statusCode int, body []byte) ErrorKind {
	s := string(body)
	return classifyString(statusCode, s)
}

// classifyString ClassifyError 的字符串内部实现（与源项目 Classify 签名对齐）。
// modelScoped5xxCodes hub MODEL_SCOPED_ERROR_CODES（#499）：这些业务码出现在 5xx
// body 里时错误是**模型级**的（该后端/该参数对这个模型不成立），换任何账号都一样——
// 不罚账号、不记 fails/熔断，把上游 5xx 原样透传（11158 除外：语义是账号类型权限，
// 仍按模型锁处理）。普通 500 仍按账号故障走通用 failover。
var modelScoped5xxCodes = []string{"11102", "11129", "11134", "11150", "11158"}

// IsModelScopedServerError 报告 5xx 响应是否携带模型级业务码（hub #499）。
func IsModelScopedServerError(status int, body string) bool {
	if status < 500 || body == "" {
		return false
	}
	for _, code := range modelScoped5xxCodes {
		if hasBusinessCode(body, code) {
			return true
		}
	}
	return false
}

func classifyString(status int, body string) ErrorKind {
	// 11102「该后端无此模型」须最先判：语义比计费/限流都更具体——若不先判，
	// msg 里的 "service info not found" 会被更宽的 4xx 兜底归为 client。
	if IsModelBlocked(status, body) {
		return KindModelBlocked
	}
	if status == http.StatusPaymentRequired {
		return KindHardCredit
	}
	lower := strings.ToLower(body)
	// sessionDead / accountFault 先于 status==429：账号级终态等不来自愈，限流状态码
	// 不得掩盖它们（429+14017 必须 accountFault，401+12153 混排 "rate limit" 必须
	// sessionDead——此层 marker 是精确词，比限流层的大范围子串更具体，具体优先于宽泛）。
	for _, m := range sessionDeadMarkers {
		if strings.Contains(body, m) {
			return KindSessionDead
		}
	}
	// WorkBuddy 11-128 / 11140 的 msg 同为 "request illegal"，必须按业务码分流：
	//   - 11-128 = 通道校验失败（上游认为请求不是官方客户端发出）→ 账号健康，
	//     归 KindChannelRefused 不罚号。误判成 account_fault 会把健康账号当封禁处置。
	//   - 11140 = 账号被上游限制（异常行为/风控）→ account_fault，由上层禁用。
	// 两者都要求 code + 文案双条件，避免 11140 的模型级限流变体被误判（见
	// IsAccountRestricted 注释）。移植自 workbuddy2api-panel 7a8b7a7。
	if isChannelRefused(body) {
		return KindChannelRefused
	}
	if IsAccountRestricted(body) {
		return KindAccountFault
	}
	for _, m := range accountFaultMarkers {
		if strings.Contains(lower, strings.ToLower(m)) || strings.Contains(body, m) {
			return KindAccountFault
		}
	}
	// 14018 是明确的账号积分耗尽业务码，必须先于通用 429 兜底（否则被误判为可自愈
	// 的软限流）。仅按结构化 code 判定；无该 code 的 "credits exhausted" 文案仍保持
	// 普通 429 的软限流语义。
	if status == http.StatusTooManyRequests && hasBusinessCode(body, "14018") {
		return KindHardCredit
	}
	// 6021「queue.full.title」是渠道级容量满载（上游推理集群排队队列打满），不是账号
	// 问题：同一时刻换任何账号都会撞同一堵墙。必须先于通用 429 兜底，否则会按软限流
	// 逐号冷却——实测 2 秒内即可把整池健康号全部误杀（2026-10-15 实案）。429 + 结构化
	// code 6021 为主判据（与 14018 同口径）；"queue.full" 固定文案单列为外卡——该文案
	// 是上游 i18n key 原文，语义唯一，非 429 状态码出现同样归本类（防上游换状态码）。
	// 移植自 workbuddy2api-panel f14f374。
	if (status == http.StatusTooManyRequests && hasBusinessCode(body, "6021")) || isQueueFullBody(lower) {
		return KindUpstreamBusy
	}
	if status == http.StatusTooManyRequests {
		return KindSoftRate
	}
	for _, m := range hardMarkers {
		if strings.Contains(lower, strings.ToLower(m)) || strings.Contains(body, m) {
			return KindHardCredit
		}
	}
	for _, m := range softRateMarkers {
		if strings.Contains(lower, strings.ToLower(m)) || strings.Contains(body, m) {
			return KindSoftRate
		}
	}
	// 11115「prompt is too long」：判在 404/5xx/WAF/内容策略/参数错误/通用 4xx 之前。
	if isPromptTooLongStatus(status) {
		for _, m := range promptTooLongMarkers {
			if strings.Contains(body, m) || (m != strings.ToLower(m) && strings.Contains(lower, strings.ToLower(m))) {
				return KindPromptTooLong
			}
		}
	}
	if status == http.StatusNotFound {
		return KindNotFound
	}
	// hub #499 模型级 5xx 不熔断整池：body 带模型级业务码的 5xx 归 KindModelBlocked
	//（不罚账号；11158 子分支照旧写模型锁），普通 5xx 才按账号故障处理。
	if IsModelScopedServerError(status, body) {
		return KindModelBlocked
	}
	if status >= 500 {
		return KindServer
	}
	// WAF 403（无业务信封的拦截形态）：判在内容策略/参数错误/通用 4xx 之前。
	if IsWafBlocked(status, body) {
		return KindWafBlock
	}
	// 图片格式/数据错误是确定性的请求级错误：同 body 换账号结果不变，直接 fail-fast。
	// 11135 业务码走 codeMarker（JSON 空白容差），文案走 invalidImageMarkers。
	if status == http.StatusBadRequest && codeMarker(lower, "11135") {
		return KindImageInvalid
	}
	if status == http.StatusBadRequest {
		for _, m := range invalidImageMarkers {
			if strings.Contains(lower, m) {
				return KindImageInvalid
			}
		}
	}
	if status >= 400 {
		for _, m := range contentBlockedMarkers {
			if strings.Contains(lower, m) {
				return KindContentBlocked
			}
		}
		if strings.Contains(body, badParamsMarkerMsg) || strings.Contains(body, badParamsMarkerCode) {
			return KindBadParams
		}
		return KindClient
	}
	// HTTP 200 但业务 code 非 0 且含余额关键词的情况已被上方 hardMarkers 捕获。
	return KindNone
}

// retryAfterHeaderCandidates 冷却时长优先解析的响应头候选序列：
// retry-after（秒）/ retry-after-ms（毫秒）/ x-ratelimit-reset（epoch 秒或毫秒）。
var retryAfterHeaderCandidates = []string{"Retry-After", "Retry-After-Ms", "X-Ratelimit-Reset"}

// retryAfterSanity 解析结果的上限（超过视为上游异常值丢弃，回落本地计算）。
const retryAfterSanity = 2 * time.Hour

// ParseRetryAfter 从限流/拦截响应头解析上游明示的等待时长：
// 依次尝试 Retry-After（整数秒）→ retry-after-ms（整数毫秒）→ x-ratelimit-reset
// （纯数字按 epoch 秒/毫秒推断；HTTP-Date 形态不支持——上游族实践发的是数字）。
// 任一头缺失/非法/非正/超上限则尝试下一头；全部不可用返回 false（调用方回落既有
// 计算值，绝不臆造等待时长）。
func ParseRetryAfter(h http.Header) (time.Duration, bool) {
	for _, name := range retryAfterHeaderCandidates {
		v := strings.TrimSpace(h.Get(name))
		if v == "" {
			continue
		}
		if !isAllDigits(v) {
			continue // 非纯数字（如 HTTP-Date）不解析，宁缺毋滥
		}
		n, ok := parseRetryNumber(v, name)
		if !ok {
			continue
		}
		if n <= 0 || n > retryAfterSanity {
			continue // 非正/异常大：丢弃（回落本地计算）
		}
		return n, true
	}
	return 0, false
}

// isAllDigits 报告 s 是否为纯数字（前置快筛）。
func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// parseRetryNumber 按头名口径把纯数字串折算成时长。x-ratelimit-reset 是 epoch 时刻
// 而非时长：秒口径（10 位）与毫秒口径（13 位）都按「now + 该时刻的剩余量」折算，
// 已在过去则不可用。位数不足（8 位以下）无法判定 epoch 语义的丢弃。
func parseRetryNumber(v, headerName string) (time.Duration, bool) {
	// 上限 16 位防 int64 溢出。
	if len(v) > 16 {
		return 0, false
	}
	var n int64
	for _, r := range v {
		n = n*10 + int64(r-'0')
	}
	switch headerName {
	case "Retry-After":
		// 先做上限校验再乘 time.Second：16 位数字乘 1e9 会溢出 int64 回绕成小正数。
		if n > int64(retryAfterSanity/time.Second) {
			return 0, false
		}
		return time.Duration(n) * time.Second, true
	case "Retry-After-Ms":
		if n > int64(retryAfterSanity/time.Millisecond) {
			return 0, false
		}
		return time.Duration(n) * time.Millisecond, true
	default: // X-Ratelimit-Reset：epoch → 剩余量
		sec := n
		if len(v) >= 12 { // 毫秒口径（13 位）；11 位边界按秒（误判代价是多算 1000 倍）
			sec = n / 1000
		}
		return time.Until(time.Unix(sec, 0)), true
	}
}

// ParseRateReset 从限流响应 body 里统一解析「将在 … 重置」时间（上游 UTC+8 文案）。
// 成功返回解析出的墙钟时刻（按 UTC+8 解释），失败返回零值 + false。
//
// CN 文案优先；global 域 429 body 是英文形态（"will reset at YYYY-MM-DD HH:MM:SS
// UTC+8"）。英文正则锚定固定格式时间，自然语言（"reset at the end of the day"）
// 不匹配。没有时间文案的限流由调用方退回有界退避（绝不臆造时间）。
func ParseRateReset(body string) (time.Time, bool) {
	m := reSoftRateResetCN.FindStringSubmatch(body)
	if len(m) < 2 {
		m = reSoftRateResetEN.FindStringSubmatch(body)
	}
	if len(m) < 2 {
		return time.Time{}, false
	}
	ts := strings.TrimSpace(m[1])
	ts = strings.TrimSuffix(ts, " UTC+8") // 去掉后缀，固定按 softRateResetLoc 解释
	t, err := time.ParseInLocation(softRateTimeLayout, ts, softRateResetLoc)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// IsAlreadyCheckin 报告 err 是否表示「今天已签到」（上游幂等拒绝重复签到）。
// 只认带分类的 *Error（业务 code 或 HTTP 错误）：网络层/解析层错误不得当作幂等成功。
func IsAlreadyCheckin(err error) bool {
	var ue *Error
	if !errors.As(err, &ue) {
		return false
	}
	for _, m := range alreadyCheckinMarkers {
		if strings.Contains(ue.Msg, m) || strings.Contains(strings.ToLower(ue.Msg), strings.ToLower(m)) {
			return true
		}
	}
	return false
}
