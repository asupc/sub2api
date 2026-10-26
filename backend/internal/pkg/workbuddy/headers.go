// headers.go 构造四类上游请求头（common / chat / billing / refresh）。
//
// 移植自 workbuddy2api-panel internal/upstream/headers.go。规则来源为该项目的
// docs/api-reference.md 与官方客户端逆向（版本号逐字对齐官方分发包）。
package workbuddy

import (
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	mathrand "math/rand/v2"
	"net/http"
	"strings"
)

// 版本常量（出站 UA 与 X-IDE-Version 用）。
const (
	// defaultClientVersion 出站 WorkBuddy 客户端版本段，对齐官方 WorkBuddy Desktop
	// 分发包版本（5.5.4）。Credentials.ClientVersion 可覆盖（空 = 内置默认）。
	defaultClientVersion = "5.5.4"
	// defaultCliVersion 出站 UA 中 `CLI/<ver>` 段版本，对齐官方内置 CLI（2.137.1）。
	// Credentials.CLIVersion 可覆盖（空 = 内置默认）。
	defaultCliVersion = "2.137.1"
)

// Origin/Referer 基础域（按 realm 切换）。
const (
	originRefererCN     = "https://www.codebuddy.cn"
	originRefererGlobal = "https://www.workbuddy.ai"
)

// originRefererFor 按账号 realm 返回 Origin/Referer 基础域：
// global → https://www.workbuddy.ai；cn → https://www.codebuddy.cn。
func originRefererFor(c Credentials) string {
	if c.IsGlobal() {
		return originRefererGlobal
	}
	return originRefererCN
}

// acceptLanguageFor 按账号 realm 返回 Accept-Language：global → en-US，cn → zh-CN。
// 官方客户端按账号域发对应语言标识，对齐避免上游风控按语言缺失误判。
func acceptLanguageFor(c Credentials) string {
	if c.IsGlobal() {
		return "en-US"
	}
	return "zh-CN"
}

// clientVersion 生效的 WorkBuddy 客户端版本：Credentials.ClientVersion 非空则取之。
func clientVersion(c Credentials) string {
	if c.ClientVersion != "" {
		return c.ClientVersion
	}
	return defaultClientVersion
}

// cliVersion 生效的 CLI 版本：Credentials.CLIVersion 非空则取之。
func cliVersion(c Credentials) string {
	if c.CLIVersion != "" {
		return c.CLIVersion
	}
	return defaultCliVersion
}

// defaultWorkBuddyUA 组装默认客户端出站 UA（官方桌面端 RestOperations 层形状）：
// `WorkBuddy/<clientVersion> <platform>/<clientVersion> CLI/<cliVersion>`。
// 平台段（第二段）品牌按 realm 切换——CN 用 applicationName 同值 `WorkBuddy`，
// global 用官方国际版 productName `WorkBuddy AI`（intl 项目逆向证据：
// `WorkBuddy/5.5.2 WorkBuddy AI/5.5.2 CLI/5.5.2`）。
//
// 风控注意：global 账号送错平台段（`WorkBuddy` 非 `WorkBuddy AI`）可能触发上游
// 403 code 11140 "request illegal" 风控。官方无任何 UA 随机化，故默认确定性。
func defaultWorkBuddyUA(c Credentials) string {
	platform := "WorkBuddy"
	if c.IsGlobal() {
		platform = "WorkBuddy AI"
	}
	return "WorkBuddy/" + clientVersion(c) + " " + platform + "/" + clientVersion(c) + " CLI/" + cliVersion(c)
}

// userAgent 返回客户端出站路径（chat/refresh/模型目录）的 UA。
// 优先级：Credentials.UserAgent 显式覆盖 > 按 realm 的默认三段式。
func userAgent(c Credentials) string {
	if c.UserAgent != "" {
		return c.UserAgent
	}
	return defaultWorkBuddyUA(c)
}

// billingUA 白名单类（billing/checkin/banner）出站 UA：单段 `WorkBuddy/<clientVersion>`
// （官方 banner 显式覆写形态，不带 CLI 段），伪造官方桌面端指纹。
func billingUA(c Credentials) string {
	return "WorkBuddy/" + clientVersion(c)
}

// deriveAccountStableID 按 uid + 用途盐稳定派生 36 hex 设备/会话标识。
// 跨重启稳定（固定盐 "wb2a:"，不随进程换）、账号间互异（uid 不同则不同）、
// 同 uid 同用途恒同值（幂等）。截 sha256 前 18 字节的 hex（36 字符）。
//
// 两个用途：
//   - purpose="machine" → X-Machine-ID（设备级，跨会话稳定）
//   - purpose="session" → X-Session-ID（账号固定会话，跨重启稳定）
//
// 与 X-Device-Token 并存不冲突：那是登录时上游签发的真实设备令牌（有则发，权威）；
// 本对头是「每账号一台固定虚拟设备」的稳定指纹，防多号被上游按设备指纹缺失/漂移
// 关联风控。两者是不同头族，官方桌面端都发。
func deriveAccountStableID(uid, purpose string) string {
	sum := sha256.Sum256([]byte("wb2a:" + purpose + ":" + uid))
	return hex.EncodeToString(sum[:18]) // 36 hex chars
}

// injectAccountStableHeaders 在 req 注入 X-Machine-ID / X-Session-ID。
// uid 为空时不注入（匿名请求无设备标识，上游不要求）。
func injectAccountStableHeaders(req *http.Request, c Credentials) {
	if c.UID == "" {
		return
	}
	req.Header.Set("X-Machine-ID", deriveAccountStableID(c.UID, "machine"))
	req.Header.Set("X-Session-ID", deriveAccountStableID(c.UID, "session"))
}

// CommonHeaders 设置所有 API 共享的请求头（chat/billing 域请求的基底）。
func CommonHeaders(req *http.Request, c Credentials) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	origin := originRefererFor(c)
	req.Header.Set("Origin", origin)
	req.Header.Set("Referer", origin+"/")
	req.Header.Set("User-Agent", userAgent(c))
	// X-CodeBuddy-Request: 1（官方客户端风控闸门头，所有 API 请求必带）。
	req.Header.Set("X-CodeBuddy-Request", "1")
	req.Header.Set("Accept-Language", acceptLanguageFor(c))
	injectAccountStableHeaders(req, c)
}

// ChatMeta 一次 chat 出站的会话头族元数据（后台按 X-Conversation-Request-ID
// 聚合请求：官方客户端一次 user send 内所有 tool call/重试/换号复用同一个 ID）。
// 调用方在轮转循环外生成 conversationRequestID，循环内每次出站复用同值；
// TraceID 透传入站值（空 = 回落 conversationRequestID）。
// messageID（消息级，每条独立）由 ChatHeaders 内部生成，无需外部可见。
type ChatMeta struct {
	// ConversationID X-Conversation-ID：body 提取的入站值；空则由 injectConversationHeaders
	// 生成 32 hex 补发（2026-10-26 起恒发——国际端点收紧通道校验后，缺 X-Conversation-ID
	// 会 400/11-128 "conversation id missing"，见 workbuddy2api-panel 7a8b7a7）。
	ConversationID string
	// ConversationRequestID X-Conversation-Request-ID / X-Root-Request-ID：聚合主键，必发。
	ConversationRequestID string
	// TraceID X-Trace-ID：入站透传值，空则回落 conversationRequestID。
	TraceID string
}

// ChatHeaders 在 CommonHeaders 之上加 chat 专属的账号头。
// 缺省字段用 X-No-* 约定（与 CodeBuddy 官方 CLI 一致）。
func ChatHeaders(req *http.Request, c Credentials, meta ChatMeta) {
	CommonHeaders(req, c)
	// chat 流式 Accept 覆盖非流式默认。
	req.Header.Set("Accept", "application/json, text/event-stream")
	if c.AccessToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.AccessToken)
	} else {
		req.Header.Set("X-No-Authorization", "1")
	}
	if c.UID != "" {
		req.Header.Set("X-User-Id", c.UID)
	} else {
		req.Header.Set("X-No-User-Id", "1")
	}
	// 安全红线：绝不在 chat 请求里携带 X-Refresh-Token。
	// 企业与域头按 realm 分发：CN 走 EnterpriseID/Domain 原样透传（缺省 X-No-*）；
	// global 账号统一覆写为国际客户端形态（X-No-Enterprise-Id=1 声明无企业 +
	// X-Domain=www.workbuddy.ai 声明国际版域，不回退登录会话原值）。
	if !c.IsGlobal() {
		if c.EnterpriseID != "" {
			req.Header.Set("X-Enterprise-Id", c.EnterpriseID)
		} else {
			req.Header.Set("X-No-Enterprise-Id", "1")
		}
		if d := strings.TrimSpace(c.Domain); d != "" {
			req.Header.Set("X-Domain", d)
		} else {
			req.Header.Set("X-No-Department-Info", "1")
		}
	} else {
		req.Header.Set("X-No-Enterprise-Id", "1")
		req.Header.Set("X-Domain", "www.workbuddy.ai")
	}
	// 用量归属头：对齐官方 WorkBuddy 桌面端指纹（上游用量归因不出现空 client 特征）。
	injectAttribution(req, c)
	// 设备风控头：凭证里有则注入（官方桌面端 Turing SDK 签发）。
	if c.DeviceToken != "" {
		req.Header.Set("X-Device-Token", c.DeviceToken)
	}
	injectConversationHeaders(req, meta)
}

// injectAttribution 注入用量归属头（X-Agent-Purpose / X-IDE-* / X-Product）。
// 仅 chat/completions 路径生效。默认对齐官方桌面端：X-Agent-Purpose="conversation"
// + X-IDE-Name/Type/Product="WorkBuddy" + X-IDE-Version=客户端版本。
// （源项目支持显式 ClientName="SaaS" 还原旧归因，本包未移植该 config 开关。）
func injectAttribution(req *http.Request, c Credentials) {
	req.Header.Set("X-Agent-Purpose", "conversation")
	req.Header.Set("X-IDE-Name", "WorkBuddy")
	req.Header.Set("X-IDE-Type", "WorkBuddy")
	req.Header.Set("X-IDE-Version", clientVersion(c))
	req.Header.Set("X-Product", "WorkBuddy")
}

// injectConversationHeaders 注入官方客户端会话头族（后台聚合）。
// 头族分四层，各司其职：
//   - X-Conversation-ID：会话级，多轮稳定（body 的 conversationId）。**恒发**：
//     入站没带时本级补一个 32 hex（workbuddy2api-panel 7a8b7a7——国际端点收紧
//     通道校验后缺该头直接 400/11-128 "conversation id missing"，透传优先的旧
//     口径不再安全）。
//   - X-Conversation-Request-ID：对话轮级聚合主键，必发。一次 user send 内的
//     所有 tool call/重试/换号/降级复用同一个 → 后台按它聚合成一条（不再碎片化）。
//   - X-Conversation-Message-ID = X-Request-ID：消息级，每条独立（32 位 hex）。
//   - X-Root-Request-ID：= conversationRequestID（根请求追踪）。
//   - X-Trace-ID：入站透传或 = conversationRequestID。
//   - X-B3-TraceId / X-B3-SpanId / X-B3-Sampled：链路族。B3 规范只认 16/32 hex
//     TraceId 与 16 hex SpanId；入站 conversationRequestID 非法时 TraceId 回落
//     messageID（恒 32 hex），SpanId 取 messageID[:16]（每消息新）。
func injectConversationHeaders(req *http.Request, meta ChatMeta) {
	convReqID := meta.ConversationRequestID
	if convReqID == "" {
		// 零值 meta 也要保证聚合主键必发：本级补一个 32 hex。
		convReqID = NewMessageID()
	}
	messageID := NewMessageID()
	conversationID := meta.ConversationID
	if conversationID == "" {
		conversationID = NewMessageID()
	}
	req.Header.Set("X-Conversation-ID", conversationID)
	req.Header.Set("X-Conversation-Request-ID", convReqID)
	req.Header.Set("X-Conversation-Message-ID", messageID)
	req.Header.Set("X-Request-ID", messageID)
	req.Header.Set("X-Root-Request-ID", convReqID)
	traceID := meta.TraceID
	if traceID == "" {
		traceID = convReqID
	}
	req.Header.Set("X-Trace-ID", traceID)
	b3Trace := convReqID
	if !validTraceID(b3Trace) {
		b3Trace = messageID // 非法 B3 TraceId → 回落恒 32 hex 的消息级 ID
	}
	req.Header.Set("X-B3-TraceId", b3Trace)
	req.Header.Set("X-B3-SpanId", messageID[:16])
	req.Header.Set("X-B3-Sampled", "1")
}

// NewMessageID 生成消息级 ID：32 位 hex（UUID v4 去横线的长度形态），对齐官方
// X-Request-ID / X-Conversation-Message-ID。crypto/rand 失败（理论上不可能）时回落
// math/rand/v2 双 uint64 拼 32 hex——恒 32 hex、恒合法，可安全用作 B3 TraceId。
func NewMessageID() string {
	b := make([]byte, 16)
	if _, err := cryptorand.Read(b); err == nil {
		return hex.EncodeToString(b)
	}
	return fmt.Sprintf("%016x%016x", mathrand.Uint64()|1, mathrand.Uint64())
}

// validTraceID 判断 B3 TraceId 是否合法：16 或 32 位 hex（大小写均可）。
// 官方客户端生成的 conversationRequestId 是 32 位 hex（UUID 去横线），入站透传值
// 可能是任意形状（含横线/超长/非 hex），直接塞进 B3 头会破坏链路关联。
func validTraceID(s string) bool {
	if len(s) != 16 && len(s) != 32 {
		return false
	}
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')) {
			return false
		}
	}
	return true
}

// BillingHeaders billing 域（余额/签到/report/成长任务）请求头。
func BillingHeaders(req *http.Request, c Credentials) {
	req.Header.Set("Authorization", "Bearer "+c.AccessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CodeBuddy-Request", "1")
	req.Header.Set("Accept-Language", acceptLanguageFor(c))
	// UA：显式覆盖优先，否则单段 WorkBuddy/<ver>（官方桌面端 billing 形态）。
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	} else {
		req.Header.Set("User-Agent", billingUA(c))
	}
	if c.UID != "" {
		req.Header.Set("X-User-Id", c.UID)
	}
	if c.EnterpriseID != "" {
		req.Header.Set("X-Enterprise-Id", c.EnterpriseID)
		req.Header.Set("X-Tenant-Id", c.EnterpriseID)
	}
	if d := strings.TrimSpace(c.Domain); d != "" {
		req.Header.Set("X-Domain", d)
	}
	if c.DeviceToken != "" {
		req.Header.Set("X-Device-Token", c.DeviceToken)
	}
}

// RefreshHeaders refresh 端点专属头（X-Refresh-Token 只允许出现在这里）。
func RefreshHeaders(req *http.Request, c Credentials) {
	CommonHeaders(req, c)
	req.Header.Set("X-Refresh-Token", c.RefreshToken)
	if c.EnterpriseID != "" {
		req.Header.Set("X-Enterprise-Id", c.EnterpriseID)
	}
	// X-Auth-Refresh-Source 对齐官方客户端 refresh 渠道标识 "plugin"。
	req.Header.Set("X-Auth-Refresh-Source", "plugin")
}
