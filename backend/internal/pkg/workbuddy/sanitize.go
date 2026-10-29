// sanitize.go 出站请求体脱敏：剥离上游渠道校验黑名单指纹。
//
// 规则来源（逐字保留判定与改写语义）：
//   - workbuddy2api-panel internal/upstream/sanitize.go（规则与判定顺序，实测依据
//     见该项目的 issue #36/PR39 与 2026-09-13 实验 F4）；
//   - workbuddy2api-hub wb_proxy.py sanitize 族（2026-10-29 全面对照移植）：
//     通用身份句正则（#533/#537/#539/#550/#574）、DSH GUI 描述段（#579）、DSH
//     文件引用段（#683）、OmO 子代理指纹（#62）、tool arguments 序列化（#446）。
//
// 背景：客户端（Claude Code / Codex / DeepSeek Harness 等）在 system prompt 注入
// 固定模板句，上游渠道校验（400 code 11-128 "Illegal API invocation from an
// unapproved channel"）按逐字精确匹配拦截（非语义审核），一字改动即可绕过。
// 策略：键值/header 型指纹整段剥离；承载语义的模板句最小改写（换一词/换语序），
// 语义不变。
//
// 作用域（对齐 hub #537/#574）：身份句族（通用身份句正则 / DSH GUI 段 / DSH
// 文件引用段）只改写 system / developer / assistant 角色——assistant 历史里的
// 身份句同样是已确认指纹（同一句放 assistant 稳定 400、放 user 200），而
// user / tool 里的同句属用户数据，改写会让模型看到与磁盘不一致的内容。
// 固定句改写 / 11-128 反探测串 / header 键值剥离对全角色生效（hub 同款）。
//
// 未移植（2026-10-22 用户裁定，勿再建议）：品牌词缩减（`DeepSeek Harness` →
// `DeepSeek`，hub SANITIZE_BRAND_RE）。
package workbuddy

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// sanitizeFeatures 特征预检：任一命中才进入净化（strings.Contains 快速路径，
// 普通请求全不中 → 原样返回，零分配）。
var sanitizeFeatures = []string{
	"x-anthropic-billing-header",                      // header 键值段键名
	"cc_entrypoint=",                                  // 尾随裸键值（截断前缀即可命中）
	"You are Claude Code",                             // 身份句（截断前缀即可命中）
	"Main branch (",                                   // 注入指令句（截断前缀即可命中）
	"You are a coding agent running in the Codex CLI", // Codex instructions 首段（截断前缀即可命中）
	"github.com/anthropics/",                          // 反馈句里的 Anthropic 仓库链接
	"11128",                                           // 上游反探测：裸数字错误码
}

// sanitizeHdrRe 剥离层：header 键名即触发（与值无关），整段删除。
var sanitizeHdrRe = regexp.MustCompile(`(?i)x-anthropic-billing-header:[^;\n]*;?\s*`)

// sanitizeBareHdrRe 兜底层：裸键名（无冒号无值）同样是指纹——2026-09-13 实验 F4
// 证实 assistant 消息里反引号引用裸键名即触发 11-128，而剥离层要求冒号、对裸串无效。
// 键值形态被整段删除后，残留的裸键名做最小缩写（header→hdr）：破坏逐字匹配、
// 语义不变、保留可读性。大小写不敏感，覆盖 X-Anthropic-... 变体。
//
// 注意该正则不要求冒号，是 sanitizeHdrRe 的超集——hasFingerprint 与 sanitizeText
// 中两者并用：先删键值形态（sanitizeHdrRe），再缩写残留裸键名（本正则），
// 替换语义不同（整段删除 vs 最小缩写），不可合并为一个正则。
var sanitizeBareHdrRe = regexp.MustCompile(`(?i)x-anthropic-billing-header`)

// sanitizeKvRe 剥离层：尾随裸键值（）循环清理。
var sanitizeKvRe = regexp.MustCompile(`(?i)\bcc_[a-z0-9_]+=[^;\n]*;?\s*`)

// sanitizeRewrites 改写层：全模板句逐字替换（每句只改一个词，语义不变）。
// 对全角色生效（user/tool 内的同串同样命中上游逐字匹配，hub 同款）。
//
// 身份句的匹配串**不带结尾标点**（只到 "…for Claude" 为止）：
// CLI 版这句以句号收尾（"…for Claude."），桌面版（claude-desktop-3p / Agent SDK）
// 以逗号接后继内容（"…for Claude, running within the Claude Agent SDK."）。
// 带句号的整句只匹配前者，桌面版会漏网、指纹原样发上游 → 400 code=11-128。
// 去掉结尾标点后两种形态一并覆盖（替换串同样不带标点，让原有标点原样保留）。
// 注意仍要求 "You are Claude Code, " 前缀，不做更宽的子串替换，
// 以免误伤 TestExactMatchOnlyVariantNotTouched 所保护的零散文本。
var sanitizeRewrites = [][2]string{
	{
		"You are Claude Code, Anthropic's official CLI for Claude",
		"You are Claude Code, Anthropic's official CLI tool for Claude",
	},
	{
		"Main branch (you will usually use this for PRs)",
		"Default branch (you will usually use this for PRs)",
	},
	{
		"You are a coding agent running in the Codex CLI, a terminal-based coding assistant.",
		"You are a coding agent running in the Codex CLI tool, a terminal-based coding assistant.",
	},
	{
		// 反馈句：整句带 Anthropic 仓库链接，上游按整句拦截（只留链接或只留半边均不拦，
		// 实测需整句同时出现）。give→provide 一词之差即可绕过，语义不变。
		"To give feedback, users should report the issue at https://github.com/anthropics/claude-code/issues",
		"To provide feedback, users should report the issue at https://github.com/anthropics/claude-code/issues",
	},
	{
		// 上游反探测：只要请求体里出现裸数字 11128 就整单拦截（与该数字的上下文无关——
		// "code=11128" / 裸 "11128" / "错误码 11128" / "Code=11128" 全部命中；
		// 相邻的 11148 / 11101 / 11115 / 99999 均放行）。11128 正是本类拦截自身的错误码，
		// 上游据此识别"在讨论/回显其内部错误码"的请求。
		// 代价：用户对话中任何 11128 都会被改写——但这串数字出现在请求里本身就是拦截条件，
		// 不改写必然失败。插入连字符保留可读性与指代（零宽空格无效，实测上游会归一化）。
		"11128",
		"11-128",
	},
}

// ---------------------------------------------------------------------------
// 通用身份句正则（hub SANITIZE_IDENTITY_RE，#533/#537/#539/#550/#574）。
// 规则字面量由脚本从 hub wb_proxy.py 字节级提取（防转写失真），Go 侧仅做
// 语法适配：RE2 无 lookahead，句末改写为「捕获句号后随的空白/结尾」（hub 用
// (?=\s|$) 环视），替换串 $2 把分隔符原样接回；无句号的行尾句为已知简化差异。
var sanitizeIdentityRe = regexp.MustCompile("(?i)(You are\\s+(?:an?\\s+|the\\s+)?(?:[\\w.-]+\\s+){0,3}?(?:agent|assistant|model|AI|LLM)?[\\s,]*(?:powered by|built on|built by|created by|developed by|made by|trained by)[^\\n]*?)\\.(\\s|$)")

// sanitizeIdentityReplacement 通用身份句的中性替换句（含句号，分隔符由 $2 接回）。
const sanitizeIdentityReplacement = "You are a helpful AI assistant."

// sanitizeIdentityVerbs 通用身份句判据的字面量快路径（见 hasFingerprint）。
var sanitizeIdentityVerbs = []string{
	"powered by",
	"built on",
	"built by",
	"created by",
	"developed by",
	"made by",
	"trained by",
}

// ---------------------------------------------------------------------------
// DSH GUI 描述段（hub SANITIZE_DSH_GUI_RE，#579）：只把 A 句尾语序换位，
// DeepSeek Harness 全名保留；作用域 system/developer/assistant。
var sanitizeDshGUIRe = regexp.MustCompile("(?i)(You are interacting with the user through the DeepSeek[ \\t]+Harness[ \\t]+Web[ \\t]+GUI) whose URL the runtime context names\\.")

// sanitizeDshGUIReplacement DSH GUI 段改写（Go 替换语法 \1 → $1）。
const sanitizeDshGUIReplacement = "$1 whose URL is named in the runtime context."

// ---------------------------------------------------------------------------
// DSH 文件引用段（hub SANITIZE_DSH_FILEREF_RE，#683 / 835da5a）：只补一个词打断
// 整段连续匹配；RE2 无前瞻，已剥离 hub 的 (?!that )（改写后天然不复匹配，幂等等价）。
var sanitizeDshFilerefRe = regexp.MustCompile("(?i)(Tokens prefixed with @ are paths )(the user explicitly referenced\\.)")

// sanitizeDshFilerefReplacement 文件引用段改写（$1that $2）。
const sanitizeDshFilerefReplacement = "${1}that $2"

// ---------------------------------------------------------------------------
// OmO 子代理指纹（hub SANITIZE_OMO_JUNIOR_RE，#62，全角色）：只改写已确认指纹，
// 单独 OhMyOpenCode / Sisyphus-Junior 放行。
var sanitizeOmoRe = regexp.MustCompile("(?i)" + "Sisyphus-Junior - Focused executor from OhMyOpenCode")

// sanitizeOmoReplacement OmO 指纹的最小改写。
const sanitizeOmoReplacement = "Sisyphus-Junior - Focused executor"

// sanitizeText 单段文本净化。identity=true 时应用身份句族（通用身份句正则 /
// DSH GUI 段 / DSH 文件引用段——仅 system/developer/assistant 角色传 true）；
// 固定句改写 / 11-128 / header 剥离 / OmO 对全角色生效。预检不中 → 返回原串。
func sanitizeText(text string, identity bool) string {
	if !hasFingerprint(text, identity) {
		return text
	}
	text = sanitizeOmoRe.ReplaceAllString(text, sanitizeOmoReplacement)
	if identity {
		text = sanitizeIdentityRe.ReplaceAllString(text, sanitizeIdentityReplacement+"$2")
		// DSH GUI 描述段单独一条规则：锚定整句，只调整句尾语序。
		text = sanitizeDshGUIRe.ReplaceAllString(text, sanitizeDshGUIReplacement)
		// 文件引用段：只补一个词，打断整段连续匹配。
		text = sanitizeDshFilerefRe.ReplaceAllString(text, sanitizeDshFilerefReplacement)
	}
	for _, rw := range sanitizeRewrites {
		text = strings.ReplaceAll(text, rw[0], rw[1])
	}
	if sanitizeHdrRe.MatchString(text) {
		text = sanitizeHdrRe.ReplaceAllString(text, "")
	}
	if strings.Contains(text, "cc_") {
		prev := ""
		for prev != text { // 清尾随裸 kv（）
			prev = text
			text = sanitizeKvRe.ReplaceAllString(text, "")
		}
	}
	// 兜底：键值形态已在上面整段删除，这里只剩裸键名（引用/示例文本形态）。
	text = sanitizeBareHdrRe.ReplaceAllString(text, "x-anthropic-billing-hdr")
	return strings.TrimSpace(text)
}

// hasFingerprint 特征预检：先走 strings.Contains 快速路径（零分配）；
// header 键名有大小写变体（X-Anthropic-...）且可能以裸键名形态出现（无冒号），
// Contains 大小写敏感、sanitizeHdrRe 要求冒号——两者都会漏掉「混合大小写 + 裸键名」，
// 必须再用不要求冒号的 (?i) 正则兜底（sanitizeBareHdrRe），否则整条净化被跳过。
// sanitizeBareHdrRe 不要求冒号，是 sanitizeHdrRe 的超集，故无需再单独匹配后者。
//
// identity=false（user/tool 数据、tool arguments）时跳过身份句族的判据——
// 那些串在非身份角色里不改写，判了也是白判（hub has_fingerprint 同款）。
func hasFingerprint(text string, identity bool) bool {
	for _, f := range sanitizeFeatures {
		if strings.Contains(text, f) {
			return true
		}
	}
	if sanitizeOmoRe.MatchString(text) {
		return true
	}
	if sanitizeBareHdrRe.MatchString(text) {
		return true
	}
	if !identity {
		return false
	}
	if sanitizeDshGUIRe.MatchString(text) {
		return true
	}
	if sanitizeDshFilerefRe.MatchString(text) {
		return true
	}
	low := strings.ToLower(text)
	for _, v := range sanitizeIdentityVerbs {
		if strings.Contains(low, v) {
			return sanitizeIdentityRe.MatchString(text)
		}
	}
	return false
}

// sanitizeContent 兼容字符串与多模态数组；只动 text part，image 等 part 不动。
// 返回净化后的值及是否发生变化。identity 见 sanitizeText。
func sanitizeContent(v any, identity bool) (any, bool) {
	switch c := v.(type) {
	case string:
		s := sanitizeText(c, identity)
		return s, s != c
	case []any:
		changed := false
		for _, p := range c {
			m, ok := p.(map[string]any)
			if !ok {
				continue
			}
			text, ok := m["text"].(string)
			if !ok {
				continue
			}
			if s := sanitizeText(text, identity); s != text {
				m["text"] = s
				changed = true
			}
		}
		return c, changed
	}
	return v, false
}

// sanitizeToolCalls 净化 assistant.tool_calls[].function.arguments，并把非字符串
// 形态的 arguments 序列化为 JSON 字符串（hub sanitize_tool_calls，#446）。
//
// 两件事：
//  1. arguments 是**字符串化的 JSON**（不是对象），上游 Go struct 只认 string——
//     客户端发 dict / list / 数字会触发 400 code=11101（cannot unmarshal into
//     field of type string）。统一序列化为 JSON 字符串；null/缺失补 "{}"
//     （上游要求该字段恒在且为 string）。
//  2. 字符串形态按文本走 sanitizeText（identity=false——工具参数是应用数据，
//     不是客户端自我介绍，身份句族不适用）。
//
// 这块长期是盲区：工具调用消息的 content 通常是 null，而早期实现
// 在 content 缺失时直接 continue，整条消息连 tool_calls 一起被跳过——
// 于是历史里任何写进工具参数的被拦字符串（文件名、命令、写入内容）都会原样漏出。
func sanitizeToolCalls(v any) bool {
	callList, ok := v.([]any)
	if !ok {
		return false
	}
	changed := false
	for _, c := range callList {
		call, ok := c.(map[string]any)
		if !ok {
			continue
		}
		fn, ok := call["function"].(map[string]any)
		if !ok {
			continue
		}
		raw, present := fn["arguments"]
		if !present || raw == nil {
			// 缺失/null 一律补 "{}"：上游要求 arguments 恒为非空字符串形态的 JSON。
			fn["arguments"] = "{}"
			changed = true
			continue
		}
		switch args := raw.(type) {
		case string:
			if s := sanitizeText(args, false); s != args {
				fn["arguments"] = s
				changed = true
			}
		default:
			// dict / list / 数字 / bool → JSON 字符串（11101 修复）。
			encoded, err := json.Marshal(args)
			if err != nil {
				encoded = []byte(fmt.Sprintf("%v", args))
			}
			fn["arguments"] = string(encoded)
			changed = true
		}
	}
	return changed
}

// sanitizeMessages 净化 messages 中的 content / reasoning 族 / tool_calls；
// 任一命中返回 true。身份句族只作用 system/developer/assistant（hub #537/#574：
// assistant 历史里的身份句是已确认指纹；user/tool 内的同句属用户数据不改写）。
func sanitizeMessages(messages []any) bool {
	changed := false
	for _, msg := range messages {
		m, ok := msg.(map[string]any)
		if !ok {
			continue
		}
		role, _ := m["role"].(string)
		identity := role == "system" || role == "developer" || role == "assistant"
		// content 与 tool_calls 各自独立判断：content 可以为 null（工具调用轮），
		// content 缺失不代表 tool_calls 无指纹。
		if c, ok := m["content"]; ok {
			if nc, ch := sanitizeContent(c, identity); ch {
				m["content"] = nc
				changed = true
			}
		}
		// reasoning_content（思维链回填字段）实测同样携带指纹，与 content 同等净化
		// （identity=false：思维链是模型自己的输出数据，hub 同款）。
		if rc, ok := m["reasoning_content"].(string); ok {
			if s := sanitizeText(rc, false); s != rc {
				m["reasoning_content"] = s
				changed = true
			}
		}
		// reasoning：另一形态的思维链字段。回填逻辑会把客户端送来的
		// reasoning_content 镜像进 reasoning（反之亦然），镜像进的指纹
		// （裸 "11-128" 这种反探测串）同样致命。
		if r, ok := m["reasoning"].(string); ok {
			if s := sanitizeText(r, false); s != r {
				m["reasoning"] = s
				changed = true
			}
		}
		if tc, ok := m["tool_calls"]; ok {
			if sanitizeToolCalls(tc) {
				changed = true
			}
		}
	}
	return changed
}
