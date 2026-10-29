// headers.go 构造四类上游请求头（common / chat / billing / refresh）。
//
// 移植自 workbuddy2api-panel internal/upstream/headers.go。规则来源为该项目的
// docs/api-reference.md 与官方客户端逆向（版本号逐字对齐官方分发包）。
package workbuddy

import (
	"os"
	"runtime"

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
	// defaultClientVersionCN / defaultClientVersionGlobal 出站 WorkBuddy 桌面端
	// 版本段，按 realm 切换（hub wb_identity.py _VERSION_DEFAULTS，2026-10-18 两区
	// 桌面端实跑/抓包核对值）。Credentials.ClientVersion 可覆盖（空 = 内置默认）。
	defaultClientVersionCN     = "5.8.3"
	defaultClientVersionGlobal = "5.8.4"
	// defaultCliVersion 出站 UA 中 `CLI/<ver>` 段版本 = 客户端内置 CLI 版本
	//（hub desktop_cli）。Credentials.CLIVersion 可覆盖（空 = 内置默认）。
	defaultCliVersion = "2.161.0"
	// defaultSDKVersion / defaultNodeVersion 桌面 chat 兼容档（WB_DESKTOP_CHAT_COMPAT）
	// 的 x-stainless-* SDK 指纹版本（hub sdk / node，OpenAI Node SDK 6.25.0 默认值）。
	defaultSDKVersion  = "6.25.0"
	defaultNodeVersion = "v22.21.1"
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

// clientVersion 生效的 WorkBuddy 桌面端版本：Credentials.ClientVersion 非空则取之，
// 否则按 realm 取内置默认（CN / global 版本号不同）。
func clientVersion(c Credentials) string {
	if c.ClientVersion != "" {
		return c.ClientVersion
	}
	if c.IsGlobal() {
		return defaultClientVersionGlobal
	}
	return defaultClientVersionCN
}

// cliVersion 生效的 CLI 版本：Credentials.CLIVersion 非空则取之。
func cliVersion(c Credentials) string {
	if c.CLIVersion != "" {
		return c.CLIVersion
	}
	return defaultCliVersion
}

// defaultWorkBuddyUA 组装默认客户端出站 UA（官方桌面端 buildUserAgent 形状）：
// `WorkBuddy/<clientVersion> <platform>/<clientVersion> CLI/<cliVersion>`。
// 平台段（第二段）按 realm 切换——CN 用 applicationName 同值 `WorkBuddy`，
// global 用官方国际版 applicationName `workbuddy-ai`（hub wb_identity.py
// desktop_ua，5.8.4 国际版桌面客户端抓包：`WorkBuddy/5.8.4 workbuddy-ai/5.8.4
// CLI/2.161.0`）。
//
// 风控注意：平台段送错可能触发上游 403 code 11140 "request illegal" 风控。
// 官方无任何 UA 随机化，故默认确定性。
func defaultWorkBuddyUA(c Credentials) string {
	platform := "WorkBuddy"
	if c.IsGlobal() {
		platform = "workbuddy-ai"
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
	commonHeaders(req, c, false)
}

// commonHeaders CommonHeaders 的兼容档实现。sdkProfile=true（桌面 chat 兼容档，
// hub #643 WB_DESKTOP_CHAT_COMPAT）时**去掉浏览器系请求头**：X-Requested-With /
// Origin / Referer / X-CodeBuddy-Request / Accept-Language / X-Machine-ID /
// X-Session-ID——官方桌面 chat 由内置 CLI（OpenAI Node SDK）发出，浏览器头一个都
// 没有。只影响 chat 出站；billing / refresh / 模型目录走 CommonHeaders 不变。
func commonHeaders(req *http.Request, c Credentials, sdkProfile bool) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if !sdkProfile {
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
		origin := originRefererFor(c)
		req.Header.Set("Origin", origin)
		req.Header.Set("Referer", origin+"/")
		// X-CodeBuddy-Request: 1（官方客户端风控闸门头，浏览器系请求必带）。
		req.Header.Set("X-CodeBuddy-Request", "1")
		req.Header.Set("Accept-Language", acceptLanguageFor(c))
		injectAccountStableHeaders(req, c)
	}
	req.Header.Set("User-Agent", userAgent(c))
}

// desktopChatCompatEnabled 桌面 chat 可选协议一致性开关（hub #643）：
// 仅进程环境 WB_DESKTOP_CHAT_COMPAT=1 显式开启（默认关闭，行为与旧版一致）。
// 开启后仅桌面 chat 出站改走 SDK profile（去浏览器头 + x-stainless-* + B3 parent）；
// CLI/VSCode 形态、计费 REST、请求体、设备 token 与退避逻辑均不变。这是协议形态
// 对齐，不是 TLS 指纹模拟、也不构成免封保证。
func desktopChatCompatEnabled() bool {
	return strings.TrimSpace(os.Getenv("WB_DESKTOP_CHAT_COMPAT")) == "1"
}

// privateDataValue X-Private-Data 取值（hub #516）：WB_PRIVATE_DATA=true|false
// 显式覆盖；auto/缺省 = 官方桌面端默认 "true"（个人号 false / 企业号 true 的口径
// 属 CLI 身份，本包只模拟桌面身份，取桌面默认）。
func privateDataValue() string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("WB_PRIVATE_DATA"))) {
	case "true":
		return "true"
	case "false":
		return "false"
	}
	return "true"
}

// identityOS / identityArch / identityNodeVersion x-stainless-* 的宿主元数据
// （hub _identity_os/_identity_arch/node 版本）：环境变量 WB_IDENTITY_OS /
// WB_IDENTITY_ARCH / WB_IDENTITY_NODE_VERSION 可覆盖（与 TLS/网络栈同源取宿主
// 默认），固定值兜底保证确定性。
func identityOS() string {
	if v := strings.TrimSpace(os.Getenv("WB_IDENTITY_OS")); v != "" {
		return v
	}
	switch runtime.GOOS {
	case "windows":
		return "Windows"
	case "darwin":
		return "MacOS"
	default:
		return "Linux"
	}
}

func identityArch() string {
	if v := strings.TrimSpace(os.Getenv("WB_IDENTITY_ARCH")); v != "" {
		return v
	}
	switch runtime.GOARCH {
	case "amd64":
		return "x64"
	case "arm64":
		return "arm64"
	case "386":
		return "ia32"
	default:
		return "x64"
	}
}

func identityNodeVersion() string {
	if v := strings.TrimSpace(os.Getenv("WB_IDENTITY_NODE_VERSION")); v != "" {
		return v
	}
	return defaultNodeVersion
}

// injectSDKRuntimeHeaders 注入 OpenAI Node SDK 的 x-stainless-* 指纹族（hub
// cli_stainless_headers，桌面兼容档去掉 x-stainless-timeout——桌面请求预算未核实，
// 不照抄 CLI 的值）。头名按官方 wire 小写（SDK Headers 迭代时键已小写化），
// 直写 map 绕过 Go 的规范化。
func injectSDKRuntimeHeaders(req *http.Request) {
	req.Header["x-stainless-lang"] = []string{"js"}
	req.Header["x-stainless-package-version"] = []string{defaultSDKVersion}
	req.Header["x-stainless-os"] = []string{identityOS()}
	req.Header["x-stainless-arch"] = []string{identityArch()}
	req.Header["x-stainless-runtime"] = []string{"node"}
	req.Header["x-stainless-runtime-version"] = []string{identityNodeVersion()}
	req.Header["x-stainless-retry-count"] = []string{"0"}
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
	compat := desktopChatCompatEnabled()
	commonHeaders(req, c, compat)
	if compat {
		// 兼容档：官方 SDK 的 Accept 是 application/json（上游仍按 stream 应答）。
		req.Header.Set("Accept", "application/json")
		injectSDKRuntimeHeaders(req)
	} else {
		// chat 流式 Accept 覆盖非流式默认。
		req.Header.Set("Accept", "application/json, text/event-stream")
	}
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
	injectConversationHeaders(req, meta, compat)
}

// injectAttribution 注入用量归属头（X-Agent-Purpose / X-IDE-* / X-Product）。
// 仅 chat/completions 路径生效。默认对齐官方桌面端：X-Agent-Purpose="conversation"
// + X-IDE-Name/Type="WorkBuddy" + X-IDE-Version=客户端版本 + X-Product="SaaS"。
// X-Product 官方值是 "SaaS"（hub wb_identity.py 三套身份统一；"WorkBuddy" 为旧
// 自创组合，属渠道指纹差异，hub v1.5.0 修正）。
func injectAttribution(req *http.Request, c Credentials) {
	req.Header.Set("X-Agent-Purpose", "conversation")
	req.Header.Set("X-IDE-Name", "WorkBuddy")
	req.Header.Set("X-IDE-Type", "WorkBuddy")
	req.Header.Set("X-IDE-Version", clientVersion(c))
	req.Header.Set("X-Product", "SaaS")
	// X-Private-Data 数据用途声明（hub #516）：官方桌面端默认 "true"；
	// WB_PRIVATE_DATA=true|false 显式覆盖（auto/缺省 = 官方默认）。
	req.Header.Set("X-Private-Data", privateDataValue())
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
func injectConversationHeaders(req *http.Request, meta ChatMeta, sdkProfile bool) {
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
	if sdkProfile {
		// 兼容档（hub trace_headers(with_parent=True)）：补 X-B3-ParentSpanId、
		// 四段 b3 单头（<trace>-<span>-1-<parent>）与 traceparent（官方自身代码只
		// 有第三方埋点库发 traceparent，仅兼容档发）。网关没有真实父子 span，
		// 合成一个父 span id，保证 b3 尾段与 X-B3-ParentSpanId 同源。
		span := messageID[:16]
		parent := NewMessageID()[:16]
		req.Header.Set("X-B3-ParentSpanId", parent)
		req.Header["b3"] = []string{b3Trace + "-" + span + "-1-" + parent}
		req.Header["traceparent"] = []string{"00-" + b3Trace + "-" + span + "-01"}
	}
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
