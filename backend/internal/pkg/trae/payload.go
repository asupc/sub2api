package trae

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// llm_utils_chat 请求体的固定字段常量（出处：gateway payload.rs / mod.rs，逐字提取）。
const (
	// LLMChatPath SOLO 对话接口路径（消耗 IDE 积分，product_id 208）。
	LLMChatPath = "/api/agent/v3/llm_utils_chat"
	// WorkspaceID 固定工作区标识。
	WorkspaceID = "e04cdd"
	// PromptMaxTokens 固定提示词上限。
	PromptMaxTokens = 168000
	// ChatMode 固定对话模式。
	ChatMode = "FunctionCall"
	// IDEVersion 随上游校验字段一同下发的 IDE 版本号。
	IDEVersion = "0.1.50"
	// IDEVersionCode IDE 版本码。
	IDEVersionCode = "20260811"
	// AppVersionCode 客户端 App 版本码（数值；上游 binding 要求 int64）。
	// 上游 2026-10 新增必填字段（缺省报 400 code=4001 "missing required
	// parameter: app_version_code"），与 IDE 版本码同源（同一客户端构建）。
	AppVersionCode = 20260811
	// ChatAppID 客户端 App ID（参考实现硬编码值，非机密）。
	ChatAppID = "6eefa01c-1036-4c7e-9ca5-d891f63bfcd8"
	// ChatPackageType 请求体的 package_type 字段（stable_cn 是 CN 稳定版通道）。
	ChatPackageType = "stable_cn"

	// DefaultModel 未指定模型时的默认值（客户端清单里的现行名）。
	DefaultModel = "deepseek-v4.1-flash"
)

// modelConfigAliases 历史别名 / 大小写归一表：模型名（小写）→ (config_name, model_name)。
// 只留「名字与客户端清单不一致」的条目；未登记的模型按派生规则生成
// （config_name = 名字、model_name = 名字 + "__dev"）——去掉后缀上游会回
// code 4023 model is unknown。
var modelConfigAliases = map[string][2]string{
	"deepseek-v4-flash":          {"DeepSeek-V4-Flash", "deepseek_v4_flash__dev"},
	"deepseek-v4-flash-official": {"DeepSeek-V4-Flash-Official", "DeepSeek-V4-Flash-Official__dev"},
	"deepseek-v4-pro":            {"DeepSeek-V4-Pro", "deepseek_v4_pro__dev"},
	"glm-5.2":                    {"glm-5.2", "glm-5.2__dev"},
	"glm-5.3":                    {"glm-5.3", "glm-5.3__dev"},
	"doubao-seed-2.1-pro":        {"Doubao-Seed-2.1-Pro", "Doubao-Seed-2.1-Pro__dev"},
	"seed-code-pro-0430":         {"Doubao-Seed-2.1-Pro", "Doubao-Seed-2.1-Pro__dev"},
	"doubao-seed-2.1-turbo":      {"Doubao-Seed-2.1-Turbo", "Doubao-Seed-2.1-Turbo__dev"},
	"kimi-k2.7-code":             {"kimi-k2.7-code", "kimi-k2.7-code__dev"},
	"minimax-m3":                 {"minimax-m3", "minimax-m3__dev"},
}

// ModelConfig 模型名 → (config_name, model_name) 映射。
//
// 规则（源码 2026-09-30 上游实测）：
//   - config_name = 客户端清单里的模型 name（本机实测 16/16 条 config_name == name）；
//   - model_name = 该名字 + "__dev"（去掉后缀直接 4023 model is unknown）；
//   - 空名退回内置默认模型；未登记的模型按派生规则生成而**不**回落默认模型
//     （回落会让「传错名字也拿到默认模型的回答」，是最难发现的错）。
func ModelConfig(model string) (configName, modelName string) {
	name := strings.TrimSpace(model)
	if name == "" {
		name = DefaultModel
	}
	if known, ok := modelConfigAliases[strings.ToLower(name)]; ok {
		return known[0], known[1]
	}
	return name, name + "__dev"
}

// newUUIDLike 生成 UUID v4 形态的随机串（上游要求 conversation_id 等字段形如 UUID）。
func newUUIDLike() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return RandomHex(16)
	}
	buf[6] = (buf[6] & 0x0f) | 0x40 // version 4
	buf[8] = (buf[8] & 0x3f) | 0x80 // variant 10xx
	hexed := hex.EncodeToString(buf)
	return fmt.Sprintf("%s-%s-%s-%s-%s", hexed[0:8], hexed[8:12], hexed[12:16], hexed[16:20], hexed[20:32])
}

// conversationIdentitySource 从 OpenAI 请求体提取「对话稳定标识原文」
// （移植 trae-workbuddy-switch 88ed652 的 conversation_identity_source）。
//
// 优先级：
//  1. prompt_cache_key —— 客户端自带的提示词缓存键，同对话内天然稳定；
//  2. metadata.session_id —— 部分客户端把自身会话号放在元数据里；
//  3. 内容指纹：system 提示 + 首条 user 消息拼串。OpenAI 协议下多轮对话的
//     历史在 messages 头部只增不改，同一对话多次请求的指纹完全一致；
//  4. user —— 客户端传入的终端用户标识，**只能垫底**。
//
// 为什么 user 排在内容指纹之后：很多客户端把 user 填成固定的安装 ID
// （Cline / Continue 这类），让它优先会让该客户端的**全部对话塌缩成同一个
// 会话号**——跨对话同号比「每请求随机」更糟。账号维度已由 uid 混入哈希承担。
//
// 返回 "" 表示连指纹都拼不出，调用方退回随机 UUID。
func conversationIdentitySource(body map[string]any) string {
	for _, item := range []struct{ prefix, field string }{
		{"cache_key:", "prompt_cache_key"},
		{"metadata:", "metadata"},
	} {
		var raw string
		switch item.field {
		case "metadata":
			// metadata 是对象，取其 session_id 子字段。
			if metadata, ok := body[item.field].(map[string]any); ok {
				raw, _ = metadata["session_id"].(string)
			}
		default:
			raw, _ = body[item.field].(string)
		}
		if trimmed := strings.TrimSpace(raw); trimmed != "" {
			return item.prefix + trimmed
		}
	}
	if fingerprint := contentFingerprint(body); fingerprint != "" {
		return fingerprint
	}
	if user, ok := body["user"].(string); ok {
		if trimmed := strings.TrimSpace(user); trimmed != "" {
			return "user:" + trimmed
		}
	}
	return ""
}

// contentFingerprint 内容指纹：system 提示 + 首条 user 消息的文本拼串。
//
// 只取头部两段而不是整段历史：多轮对话的历史在 messages 头部只增不改，
// 头部两段足以区分对话，同时不必扫描越来越长的历史数组。
// 分段数组等复杂 content 整体序列化——同客户端同请求形态下结果稳定。
func contentFingerprint(body map[string]any) string {
	messages, ok := body["messages"].([]any)
	if !ok {
		return ""
	}
	var systemText, userText string
	hasSystem, hasUser := false, false
	for _, item := range messages {
		message, ok := item.(map[string]any)
		if !ok {
			continue
		}
		role, _ := message["role"].(string)
		content, exists := message["content"]
		if !exists {
			continue
		}
		text, hasText := "", true
		switch value := content.(type) {
		case string:
			text = value
		case nil:
			hasText = false
		default:
			// 分段数组等复杂形态整体序列化（键序由 encoding/json 固定）。
			encoded, err := json.Marshal(value)
			if err != nil {
				hasText = false
			} else {
				text = string(encoded)
			}
		}
		if !hasText {
			continue
		}
		switch role {
		case "system":
			if !hasSystem {
				systemText, hasSystem = text, true
			}
		case "user":
			if !hasUser {
				userText, hasUser = text, true
			}
		}
		if hasSystem && hasUser {
			break // 两段都取到，无需继续扫描长对话。
		}
	}
	parts := make([]string, 0, 2)
	if hasSystem {
		parts = append(parts, "system:"+systemText)
	}
	if hasUser {
		parts = append(parts, "user:"+userText)
	}
	return strings.Join(parts, "|")
}

// stableConversationID 由标识原文派生**稳定**的 UUID 形态会话号：
// SHA256 摘要前 16 字节 → UUID v4 位形（移植源 stable_conversation_id）。
//
// salt 区分字段用途（conv: / sess: / proj: 各一把盐）：同一对话派生出三个
// 互不相同、又各自稳定的 ID。手工置版本位 / 变体位，保证上游看到的仍是
// 标准 v4 UUID 形态（上游只认这种形状）。
func stableConversationID(source, salt string) string {
	sum := sha256.Sum256([]byte(salt + "|" + source))
	bytes := sum[:16]
	bytes[6] = (bytes[6] & 0x0f) | 0x40 // 版本位 → v4
	bytes[8] = (bytes[8] & 0x3f) | 0x80 // 变体位 → RFC 4122
	hexed := hex.EncodeToString(bytes)
	return fmt.Sprintf("%s-%s-%s-%s-%s", hexed[0:8], hexed[8:12], hexed[12:16], hexed[16:20], hexed[20:32])
}

// ModelOf 从 OpenAI 请求体读取模型名；缺失或空白时用 defaultModel。
func ModelOf(body map[string]any, defaultModel string) string {
	if model, ok := body["model"].(string); ok {
		if trimmed := strings.TrimSpace(model); trimmed != "" {
			return trimmed
		}
	}
	return defaultModel
}

// WantsStream 客户端是否要求流式响应。
func WantsStream(body map[string]any) bool {
	stream, _ := body["stream"].(bool)
	return stream
}

// PrepareLLMChatBody 把标准 OpenAI chat/completions 请求体改写为 llm_utils_chat 请求体。
//
// 三处必须改写的形态：
//  1. content 只接受数组形态（[{"type":"text","text":…}]），字符串会被拒；
//  2. tools[].function.parameters 只接受 JSON 字符串，不接受对象；
//  3. assistant.tool_calls[].function 要改成 function_call（历史消息回传时），
//     无函数名的 tool_call 直接丢弃。
//
// 固定字段（stream=true 强制、function 按 variant 分家、workspace_id/prompt_max_tokens/
// mode/ide_version/app_id/package_type 等）覆写客户端传来的同名值——上游只认这一套。
//
// src 不是合法 JSON 对象时原样返回（交给上游报错，而不是在这里吞掉）。
func PrepareLLMChatBody(src []byte, variant Variant, defaultModel, uid, deviceID, machineID string) []byte {
	var body map[string]any
	if err := json.Unmarshal(src, &body); err != nil {
		return src
	}
	if body == nil {
		return src
	}

	normalizeMessages(body)
	normalizeToolChoice(body)
	normalizeTools(body)

	configName, modelName := ModelConfig(ModelOf(body, defaultModel))

	// 固定字段：覆盖客户端可能传来的同名值。
	body["config_name"] = configName
	body["model_name"] = modelName
	// 上游只支持流式；stream:false 由调用方本地聚合。
	body["stream"] = true
	body["function"] = FunctionFor(variant)
	// 会话三 ID：按「对话内容 + 账号」稳定派生（对话复用），取不到指纹才随机
	// （移植源 88ed652 / conversation_identity_source + stable_conversation_id）。
	// 同一对话的多次请求（含多轮追加历史）派生出相同 ID，上游才能做路由亲和
	// 与提示词前缀缓存；不同对话互不串号。uid 必须混入哈希：两个账号发起内容
	// 相同的对话时，只按内容派生会得到完全相同的 ID——跨账号同会话号是典型的
	// 风控特征。
	//
	// 二开修正（2026-11-06）：此前三个 ID 恒为**每请求随机**，前缀缓存拿不到
	// 稳定的对话归属 ⇒ 线上 trae 全平台 9477 条请求 cache_read 恒为 0。
	identitySource := conversationIdentitySource(body)
	deriveID := func(salt string) string {
		if identitySource == "" {
			return newUUIDLike()
		}
		return stableConversationID(identitySource+"|uid:"+uid, salt)
	}
	body["conversation_id"] = deriveID("conv:")
	body["user_id"] = uid
	body["session_id"] = deriveID("sess:")
	body["device_id"] = deviceID
	body["machine_id"] = machineID
	body["project_id"] = deriveID("proj:")
	body["workspace_id"] = WorkspaceID
	body["prompt_max_tokens"] = PromptMaxTokens
	body["mode"] = ChatMode
	body["ide_version"] = IDEVersion
	body["ide_version_code"] = IDEVersionCode
	body["app_version_code"] = AppVersionCode
	body["app_id"] = ChatAppID
	body["package_type"] = ChatPackageType
	// Max Mode（issue #53 三版）：官方目录 context_window_tokens 里 max > dev
	// 的模型注入 is_max_mode:true——prompt_max_tokens 恒 168000 不变（实测该
	// 字段不钳制实际上下文，1M 窗口由上游内部决策）；目录未同步 / 不支持
	// 则不注入（不猜）。目录同步见 catalog.go 的 EnsureCatalog。
	if _, _, ok := MaxModeWindow(variant.Region(), configName); ok {
		body["is_max_mode"] = true
	}

	out, err := json.Marshal(body)
	if err != nil {
		return src
	}
	return out
}

// normalizeMessages 消息数组改写：content 字符串转数组、tool_calls 转 function_call、
// 丢掉没有函数名的空 tool_call。
func normalizeMessages(body map[string]any) {
	messages, ok := body["messages"].([]any)
	if !ok {
		return
	}
	for _, item := range messages {
		message, ok := item.(map[string]any)
		if !ok {
			continue
		}

		if role, _ := message["role"].(string); role == "assistant" {
			calls, ok := message["tool_calls"].([]any)
			if ok {
				kept := make([]any, 0, len(calls))
				for _, callItem := range calls {
					call, ok := callItem.(map[string]any)
					if !ok {
						continue
					}
					if function, exists := call["function"]; exists {
						delete(call, "function")
						call["function_call"] = function
					}
					// 没有函数名的 tool_call 是上游会拒绝的脏数据，直接丢弃。
					if functionCall, ok := call["function_call"].(map[string]any); ok {
						if name, _ := functionCall["name"].(string); strings.TrimSpace(name) != "" {
							kept = append(kept, call)
						}
					}
				}
				if len(kept) == 0 {
					delete(message, "tool_calls")
				} else {
					message["tool_calls"] = kept
				}
			}
		}

		// content 字符串 → 数组形态。
		if text, ok := message["content"].(string); ok {
			message["content"] = []any{map[string]any{"type": "text", "text": text}}
		}
	}
}

// normalizeToolChoice tool_choice 归一化：上游只认 auto / required / 具体函数名 / 缺省。
// "none" 视为禁用工具（连 tools 一并移除）。
func normalizeToolChoice(body map[string]any) {
	choice, exists := body["tool_choice"]
	if !exists {
		return
	}
	delete(body, "tool_choice")
	suppress := func() {
		delete(body, "tools")
		delete(body, "functions")
	}
	switch c := choice.(type) {
	case string:
		if strings.EqualFold(strings.TrimSpace(c), "none") {
			suppress()
		} else {
			body["tool_choice"] = c
		}
	case map[string]any:
		kind, _ := c["type"].(string)
		kind = strings.ToLower(kind)
		switch kind {
		case "none":
			suppress()
		case "auto", "required":
			body["tool_choice"] = kind
		case "function":
			name := ""
			if function, ok := c["function"].(map[string]any); ok {
				name, _ = function["name"].(string)
			}
			if name == "" {
				name, _ = c["name"].(string)
			}
			name = strings.TrimSpace(name)
			if name == "" {
				name = "auto"
			}
			body["tool_choice"] = name
		}
	}
}

// normalizeTools tools 归一化：parameters 对象 → JSON 字符串；无 function 的条目丢弃。
func normalizeTools(body map[string]any) {
	raw, exists := body["tools"]
	if !exists {
		return
	}
	list, ok := raw.([]any)
	if !ok {
		return
	}
	if len(list) == 0 {
		delete(body, "tools")
		return
	}
	kept := make([]any, 0, len(list))
	for _, item := range list {
		tool, ok := item.(map[string]any)
		if !ok {
			continue
		}
		function, ok := tool["function"].(map[string]any)
		if !ok {
			continue
		}
		if parameters, ok := function["parameters"].(map[string]any); ok {
			if text, err := json.Marshal(parameters); err == nil {
				function["parameters"] = string(text)
			}
		}
		kept = append(kept, item)
	}
	if len(kept) == 0 {
		delete(body, "tools")
	} else {
		body["tools"] = kept
	}
}
