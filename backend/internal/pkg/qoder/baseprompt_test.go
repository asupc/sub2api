//go:build unit

package qoder

import (
	"time"
	"encoding/json"
	"strings"
	"testing"
)

func mustSession(t *testing.T) *CosySession {
	t.Helper()
	s, err := NewCosySession(goldIdentity())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// decodeBody 解开 BuildAgentChatBody 的出站 body 供断言。
func decodeBody(t *testing.T, encoded string) map[string]any {
	t.Helper()
	raw, err := QoderDecode(encoded)
	if err != nil {
		t.Fatalf("qoder_decode: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	return body
}

func TestBuildAgentChatBodyOverwrites(t *testing.T) {
	sess := mustSession(t)
	req := map[string]any{
		"model": "qwen3.8-max",
		"messages": []any{
			map[string]any{"role": "system", "content": "你是一个严谨的助手"},
			map[string]any{"role": "user", "content": "你好，第一问"},
			map[string]any{"role": "assistant", "content": "第一答"},
			map[string]any{"role": "user", "content": "第二问：写个函数"},
		},
		"max_tokens": 4096,
	}
	encoded, err := BuildAgentChatBody(req, sess, "")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	body := decodeBody(t, encoded)

	if body["stream"] != true {
		t.Fatal("stream must be true")
	}
	if body["agent_id"] != "agent_common" {
		t.Fatalf("agent_id = %v", body["agent_id"])
	}
	if body["aliyun_user_type"] != "personal_professional_trial" {
		t.Fatalf("aliyun_user_type = %v", body["aliyun_user_type"])
	}
	// request_id == chat_record_id，其余 id 各自独立
	if body["request_id"] != body["chat_record_id"] {
		t.Fatal("request_id must equal chat_record_id")
	}
	if body["request_set_id"] == body["session_id"] {
		t.Fatal("request_set_id / session_id should be independent uuids")
	}

	// messages：客户端 system 覆盖模板，示例 user 轮被丢弃，真实会话追加
	msgs, _ := body["messages"].([]any)
	if len(msgs) != 4 {
		t.Fatalf("messages len = %d: %+v", len(msgs), msgs)
	}
	first, _ := msgs[0].(map[string]any)
	if first["role"] != "system" || first["content"] != "你是一个严谨的助手" {
		t.Fatalf("first message: %+v", first)
	}
	last, _ := msgs[3].(map[string]any)
	if last["role"] != "user" || last["content"] != "第二问：写个函数" {
		t.Fatalf("last message: %+v", last)
	}

	// model_config
	mc, _ := body["model_config"].(map[string]any)
	if mc["key"] != "qmodel_38max" {
		t.Fatalf("model key = %v", mc["key"])
	}
	if mc["format"] != "openai" || mc["source"] != "system" || mc["model"] != "" ||
		mc["api_key"] != "" || mc["url"] != "" {
		t.Fatalf("model config: %+v", mc)
	}
	if mc["display_name"] != "Qwen3.8-Max" {
		t.Fatalf("display_name = %v", mc["display_name"])
	}
	if mc["max_input_tokens"] == nil {
		t.Fatal("max_input_tokens must be set")
	}

	// chat_context 高亮 = 最新用户提示词
	cc, _ := body["chat_context"].(map[string]any)
	txt, _ := cc["text"].(map[string]any)
	if txt["text"] != "第二问：写个函数" {
		t.Fatalf("chat_context text = %v", txt["text"])
	}
	extra, _ := cc["extra"].(map[string]any)
	oc, _ := extra["originalContent"].(map[string]any)
	if oc["text"] != "第二问：写个函数" {
		t.Fatalf("originalContent = %v", oc["text"])
	}

	// parameters
	params, _ := body["parameters"].(map[string]any)
	if params["max_tokens"] != float64(4096) {
		t.Fatalf("max_tokens = %v", params["max_tokens"])
	}

	// 未声明 tools -> 置空（不泄漏桌面端 agent 工具）
	if tools, _ := body["tools"].([]any); len(tools) != 0 {
		t.Fatalf("tools must be empty, got %d", len(tools))
	}

	// business：name = prompt 前 30 码点，product/type/stage 保留模板值
	biz, _ := body["business"].(map[string]any)
	if biz["name"] != "第二问：写个函数" {
		t.Fatalf("business name = %v", biz["name"])
	}
	if biz["product"] != "cli" || biz["type"] != "agent" || biz["stage"] != "start" {
		t.Fatalf("business: %+v", biz)
	}
	if _, ok := biz["begin_at"].(float64); !ok {
		t.Fatalf("begin_at = %v", biz["begin_at"])
	}
}

func TestBuildAgentChatBodyToolHistory(t *testing.T) {
	sess := mustSession(t)
	req := map[string]any{
		"model": "qwen3.7-plus",
		"messages": []any{
			map[string]any{"role": "user", "content": "run"},
			map[string]any{"role": "assistant", "content": "",
				"tool_calls": []any{map[string]any{
					"id": "call_1", "type": "function",
					"function": map[string]any{"name": "terminal", "arguments": "{\"cmd\":\"ls\"}"}}}},
			map[string]any{"role": "tool", "tool_call_id": "call_1", "name": "terminal",
				"content": "file-a\nfile-b"},
			map[string]any{"role": "user", "content": "thanks"},
		},
		"tools": []any{map[string]any{"type": "function", "function": map[string]any{
			"name": "terminal", "description": "run"}}},
	}
	encoded, err := BuildAgentChatBody(req, sess, "")
	if err != nil {
		t.Fatal(err)
	}
	body := decodeBody(t, encoded)
	msgs, _ := body["messages"].([]any)
	// 无客户端 system：模板 system 基座保留 + user/assistant/tool/user 共 5 条
	if len(msgs) != 5 {
		t.Fatalf("messages len = %d", len(msgs))
	}
	if m, _ := msgs[0].(map[string]any); m["role"] != "system" {
		t.Fatalf("first message must be template system: %+v", msgs[0])
	}
	// assistant 的 tool_calls 序列化进 content（LEAK_MARKER + JSON 数组）
	asst, _ := msgs[2].(map[string]any)
	content, _ := asst["content"].(string)
	if !strings.HasPrefix(content, "\n\n"+LeakMarker+"\n") {
		t.Fatalf("assistant content must carry leak marker: %q", content)
	}
	if !strings.Contains(content, `"name":"terminal"`) || !strings.Contains(content, `"arguments":"{\"cmd\":\"ls\"}"`) {
		t.Fatalf("serialized calls: %q", content)
	}
	// tool 结果 -> user 文本信封
	toolMsg, _ := msgs[3].(map[string]any)
	if toolMsg["role"] != "user" {
		t.Fatalf("tool role must degrade to user: %+v", toolMsg)
	}
	if toolMsg["content"] != ToolResultMarker+" (terminal)]\nfile-a\nfile-b" {
		t.Fatalf("tool envelope: %q", toolMsg["content"])
	}
	// 客户端 tools 透传
	tools, _ := body["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools = %d", len(tools))
	}
}

// -------- 结构化工具历史直传（task-31/32） --------

// 形如真实入站（Anthropic 桥转换后）的含工具历史消息。
func toolHistoryMessages() []any {
	return []any{
		map[string]any{"role": "user", "content": "run"},
		map[string]any{"role": "assistant", "content": "",
			"tool_calls": []any{map[string]any{
				"id": "call_1", "type": "function",
				"function": map[string]any{"name": "terminal", "arguments": "{\"cmd\":\"ls\"}"}}}},
		map[string]any{"role": "tool", "tool_call_id": "call_1", "name": "terminal",
			"content": "file-a\nfile-b"},
		map[string]any{"role": "user", "content": "thanks"},
	}
}

func TestStructuredToolHistoryGate(t *testing.T) {
	msgs := toolHistoryMessages()

	// auto（缺省/非法值）：仅 cn + 白名单前缀
	t.Setenv(structuredToolHistoryEnv, "")
	if !StructuredToolHistoryEnabled("cn", "qwen3.8-max", "qmodel_38max", msgs) {
		t.Fatal("auto/cn/qwen must enable")
	}
	if !StructuredToolHistoryEnabled("cn", "GLM-5.3-Flash", "gfmodel", msgs) {
		t.Fatal("auto/cn/glm must enable")
	}
	if !StructuredToolHistoryEnabled("cn", "qwen3.7-plus", "gm51model", msgs) {
		t.Fatal("auto/cn/key-prefix gm5 must enable")
	}
	// issue #60 follow-up：DeepSeek 族纳入白名单（deepseek 前缀 + 上游 key dfmodel）。
	if !StructuredToolHistoryEnabled("cn", "deepseek-v4-pro", "dmodel", msgs) {
		t.Fatal("auto/cn/deepseek must enable")
	}
	if !StructuredToolHistoryEnabled("cn", "DeepSeek-V4-Flash", "dfmodel", msgs) {
		t.Fatal("auto/cn/dfmodel key must enable")
	}
	if StructuredToolHistoryEnabled("cn", "kimi-k2.8", "kmodel", msgs) {
		t.Fatal("auto/cn/kimi must not enable")
	}
	if StructuredToolHistoryEnabled("intl", "qwen3.8-max", "qmodel_38max", msgs) {
		t.Fatal("auto/intl must not enable")
	}
	t.Setenv(structuredToolHistoryEnv, "banana")
	if !StructuredToolHistoryEnabled("cn", "qwen3.8-max", "qmodel_38max", msgs) {
		t.Fatal("illegal env value must fall back to auto")
	}

	// off：一键回退
	for _, v := range []string{"off", "0", "false", "no", "disable", "disabled"} {
		t.Setenv(structuredToolHistoryEnv, v)
		if StructuredToolHistoryEnabled("cn", "qwen3.8-max", "qmodel_38max", msgs) {
			t.Fatalf("off value %q must disable", v)
		}
	}

	// on：压过 realm 与白名单
	for _, v := range []string{"on", "1", "true", "yes", "enable", "enabled", "force"} {
		t.Setenv(structuredToolHistoryEnv, v)
		if !StructuredToolHistoryEnabled("intl", "deepseek-v4-pro", "dmodel", msgs) {
			t.Fatalf("on value %q must force enable", v)
		}
	}

	// id 不齐备：任何模式都不可覆盖（fail-safe）
	t.Setenv(structuredToolHistoryEnv, "on")
	noID := []any{
		map[string]any{"role": "assistant", "content": "",
			"tool_calls": []any{map[string]any{
				"type":     "function",
				"function": map[string]any{"name": "terminal", "arguments": "{}"}}}},
		map[string]any{"role": "tool", "tool_call_id": "call_1", "content": "x"},
	}
	if StructuredToolHistoryEnabled("cn", "qwen3.8-max", "qmodel_38max", noID) {
		t.Fatal("missing tool_call id must fail-safe to text mode even with on")
	}
	noToolCallID := []any{
		map[string]any{"role": "tool", "content": "x"},
	}
	if StructuredToolHistoryEnabled("cn", "qwen3.8-max", "qmodel_38max", noToolCallID) {
		t.Fatal("missing tool_call_id must fail-safe to text mode")
	}
}

func TestBuildAgentChatBodyStructuredToolHistory(t *testing.T) {
	t.Setenv(structuredToolHistoryEnv, "on")
	sess := mustSession(t)
	req := map[string]any{
		"model":    "qwen3.8-max",
		"messages": toolHistoryMessages(),
		"tools": []any{map[string]any{"type": "function", "function": map[string]any{
			"name": "terminal", "description": "run"}}},
	}
	encoded, err := BuildAgentChatBody(req, sess, "intl")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	body := decodeBody(t, encoded)
	msgs, _ := body["messages"].([]any)
	// 模板 system 基座 + user/assistant/tool/user 共 5 条
	if len(msgs) != 5 {
		t.Fatalf("messages len = %d", len(msgs))
	}
	asst, _ := msgs[2].(map[string]any)
	if asst["role"] != "assistant" {
		t.Fatalf("unexpected order: %+v", asst)
	}
	if c, _ := asst["content"].(string); c != "" {
		t.Fatalf("structured assistant content must be empty string, got %q", c)
	}
	calls, _ := asst["tool_calls"].([]any)
	if len(calls) != 1 {
		t.Fatalf("tool_calls = %+v", asst["tool_calls"])
	}
	call, _ := calls[0].(map[string]any)
	fn, _ := call["function"].(map[string]any)
	if call["id"] != "call_1" || call["type"] != "function" ||
		fn["name"] != "terminal" || fn["arguments"] != "{\"cmd\":\"ls\"}" {
		t.Fatalf("structured call shape: %+v", call)
	}
	// tool 消息保留 role/tool_call_id 配对，丢弃标记与 name 字段
	toolMsg, _ := msgs[3].(map[string]any)
	if toolMsg["role"] != "tool" || toolMsg["tool_call_id"] != "call_1" {
		t.Fatalf("tool message must keep pairing: %+v", toolMsg)
	}
	if toolMsg["content"] != "file-a\nfile-b" {
		t.Fatalf("tool content: %q", toolMsg["content"])
	}
	if _, has := toolMsg["name"]; has {
		t.Fatalf("structured tool message must drop name: %+v", toolMsg)
	}
	// 全 body 不含任何内部标记
	raw := marshalNoEscape(body)
	if strings.Contains(raw, LeakMarker) || strings.Contains(raw, ToolResultMarker) {
		t.Fatalf("structured body must not carry echo markers: %s", raw)
	}
	// tools 透传与结构化无关
	tools, _ := body["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools = %d", len(tools))
	}
}

// 文本化路径（门控未命中）保持既有形态：标记 + 信封仍在。
func TestBuildAgentChatBodyTextModeKeepsMarkers(t *testing.T) {
	t.Setenv(structuredToolHistoryEnv, "off")
	sess := mustSession(t)
	req := map[string]any{
		"model":    "deepseek-v4-pro",
		"messages": toolHistoryMessages(),
	}
	encoded, err := BuildAgentChatBody(req, sess, "cn")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	raw := marshalNoEscape(decodeBody(t, encoded))
	if !strings.Contains(raw, LeakMarker) || !strings.Contains(raw, ToolResultMarker) {
		t.Fatalf("text mode must keep markers: %s", raw)
	}
}

func TestBuildAgentChatBodyNoSystemKeepsTemplate(t *testing.T) {
	sess := mustSession(t)
	req := map[string]any{
		"model":    "auto",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	}
	encoded, err := BuildAgentChatBody(req, sess, "")
	if err != nil {
		t.Fatal(err)
	}
	body := decodeBody(t, encoded)
	msgs, _ := body["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("messages len = %d", len(msgs))
	}
	first, _ := msgs[0].(map[string]any)
	if first["role"] != "system" {
		t.Fatalf("template system must be kept: %+v", first)
	}
	sysText, _ := first["content"].(string)
	if sysText == "" {
		t.Fatal("template system prompt must be non-empty")
	}
	// developer 角色归一为 system
	req2 := map[string]any{
		"model": "auto",
		"messages": []any{
			map[string]any{"role": "developer", "content": "dev instructions"},
			map[string]any{"role": "user", "content": "hi"},
		},
	}
	encoded2, err := BuildAgentChatBody(req2, sess, "")
	if err != nil {
		t.Fatal(err)
	}
	body2 := decodeBody(t, encoded2)
	msgs2, _ := body2["messages"].([]any)
	sys2, _ := msgs2[0].(map[string]any)
	if sys2["role"] != "system" || sys2["content"] != "dev instructions" {
		t.Fatalf("developer -> system: %+v", sys2)
	}
}

func TestBuildAgentChatBodyImagesAndVisionParts(t *testing.T) {
	sess := mustSession(t)
	req := map[string]any{
		"model": "qwen3.8-flash",
		"messages": []any{
			map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "text", "text": "看图"},
				map[string]any{"type": "image_url",
					"image_url": map[string]any{"url": "https://x/img.png"}},
			}},
		},
	}
	encoded, err := BuildAgentChatBody(req, sess, "")
	if err != nil {
		t.Fatal(err)
	}
	body := decodeBody(t, encoded)
	msgs, _ := body["messages"].([]any)
	// 无客户端 system：[0] 为模板 system，[1] 为真实 user
	if len(msgs) != 2 {
		t.Fatalf("messages len = %d", len(msgs))
	}
	if m, _ := msgs[0].(map[string]any); m["role"] != "system" {
		t.Fatalf("first message must be template system: %+v", msgs[0])
	}
	userMsg, _ := msgs[1].(map[string]any)
	// 多模态（#36/#72 收口）：带图消息 content 保留结构化 parts，图片真正
	// 传上游（旧语义是丢弃图片只留文本）。
	parts, ok := userMsg["content"].([]any)
	if !ok || len(parts) != 2 {
		t.Fatalf("flattened content must be multimodal parts, got %v", userMsg["content"])
	}
	p0, _ := parts[0].(map[string]any)
	if p0["type"] != "text" || p0["text"] != "看图" {
		t.Fatalf("text part = %v", parts[0])
	}
	p1, _ := parts[1].(map[string]any)
	if p1["type"] != "image_url" {
		t.Fatalf("image part = %v", parts[1])
	}
	// issue #59：上游只读 messages parts，image_urls / chat_context.imageUrls 不再重复下发。
	cc, _ := body["chat_context"].(map[string]any)
	if _, ok := cc["imageUrls"]; ok {
		t.Fatalf("imageUrls must not be duplicated: %v", cc["imageUrls"])
	}
	if _, ok := body["image_urls"]; ok {
		t.Fatal("top-level image_urls must not be duplicated")
	}
}

func TestBuildAgentChatBodyRealmMetadata(t *testing.T) {
	sess := mustSession(t)
	req := map[string]any{
		"model":    "minimax-m3",
		"messages": []any{map[string]any{"role": "user", "content": "q"}},
	}
	// intl：mmodel = MiniMax-M3 / is_vl=true（本区目录优先，不能落到 cn 的
	// MiniMax-M2.7 / is_vl=false）。
	encoded, err := BuildAgentChatBody(req, sess, "intl")
	if err != nil {
		t.Fatal(err)
	}
	mc, _ := decodeBody(t, encoded)["model_config"].(map[string]any)
	if mc["key"] != "mmodel" || mc["display_name"] != "MiniMax-M3" || mc["is_vl"] != true {
		t.Fatalf("intl mmodel metadata: %+v", mc)
	}
	// cn：MiniMax-M2.7 请求名解析到同一 key，但元数据按 cn 快照（is_vl=false）。
	reqCN := map[string]any{
		"model":    "minimax-m2.7",
		"messages": []any{map[string]any{"role": "user", "content": "q"}},
	}
	encoded, err = BuildAgentChatBody(reqCN, sess, "cn")
	if err != nil {
		t.Fatal(err)
	}
	mc, _ = decodeBody(t, encoded)["model_config"].(map[string]any)
	if mc["key"] != "mmodel" || mc["display_name"] != "MiniMax-M2.7" || mc["is_vl"] != false {
		t.Fatalf("cn mmodel metadata: %+v", mc)
	}
}

func TestBuildAgentChatBodyReasoningEffort(t *testing.T) {
	sess := mustSession(t)
	// qmodel_38max 目录 thinking_config 支持 low/medium/xhigh（默认 medium）
	req := map[string]any{
		"model":            "qmodel_38max",
		"messages":         []any{map[string]any{"role": "user", "content": "q"}},
		"reasoning_effort": "extreme", // 未知名 -> 默认档
	}
	encoded, err := BuildAgentChatBody(req, sess, "")
	if err != nil {
		t.Fatal(err)
	}
	body := decodeBody(t, encoded)
	params, _ := body["parameters"].(map[string]any)
	if params["reasoning_effort"] == nil {
		t.Fatal("reasoning_effort must be normalized and set")
	}
	if v, _ := params["reasoning_effort"].(string); v != "medium" {
		t.Fatalf("reasoning_effort = %v (unknown level -> model default medium)", params["reasoning_effort"])
	}
	// 支持档位透传
	req1b := map[string]any{
		"model":            "qmodel_38max",
		"messages":         req["messages"],
		"reasoning_effort": "xhigh",
	}
	encoded1b, _ := BuildAgentChatBody(req1b, sess, "")
	body1b := decodeBody(t, encoded1b)
	params1b, _ := body1b["parameters"].(map[string]any)
	if params1b["reasoning_effort"] != "xhigh" {
		t.Fatalf("supported level must pass through: %v", params1b["reasoning_effort"])
	}
	// thinking.level 兼容形态 + none 关闭
	req2 := map[string]any{
		"model":    "qmodel_38max",
		"messages": req["messages"],
		"thinking": map[string]any{"level": "none"},
	}
	encoded2, _ := BuildAgentChatBody(req2, sess, "")
	body2 := decodeBody(t, encoded2)
	params2, _ := body2["parameters"].(map[string]any)
	if params2["reasoning_effort"] != "none" {
		t.Fatalf("thinking.level none -> %v", params2["reasoning_effort"])
	}
}

func TestSanitizeTextStripsClientFingerprints(t *testing.T) {
	in := "System: You are Claude Code, Anthropic's official CLI for Claude. x-anthropic-billing-header: abc123; cc_entrypoint=cli;"
	out := SanitizeText(in)
	if strings.Contains(out, "x-anthropic-billing-header:") || strings.Contains(out, "cc_entrypoint=") {
		t.Fatalf("fingerprints not stripped: %q", out)
	}
	if !strings.Contains(out, "official CLI tool") {
		t.Fatalf("rewrite missing: %q", out)
	}
	if SanitizeText("plain text") != "plain text" {
		t.Fatal("plain text must pass through")
	}
}

func TestShrinkToolResult(t *testing.T) {
	orig := ToolResultKeepChars
	defer func() { ToolResultKeepChars = orig }()

	long := strings.Repeat("y", 5000)
	// 关闭削减
	ToolResultKeepChars = 0
	if shrinkToolResult(long, ToolResultKeepChars) != long {
		t.Fatal("keep=0 must disable shrinking")
	}
	// 正常削减：首尾保留 + 省略标记
	ToolResultKeepChars = 2000
	out := shrinkToolResult(long, 2000)
	if pyLen(out) >= pyLen(long) {
		t.Fatalf("must shrink: %d", pyLen(out))
	}
	if !strings.Contains(out, "已省略") {
		t.Fatalf("omission marker missing: %q", out[:80])
	}
	if !strings.HasPrefix(out, "yyyy") || !strings.HasSuffix(out, "yyyy") {
		t.Fatal("head/tail must be preserved")
	}
	// 短文本不动
	if shrinkToolResult("short", 2000) != "short" {
		t.Fatal("short result must stay")
	}
	// 完整 JSON 双倍预算
	jsonLong := `{"rows":[` + strings.Repeat(`{"a":1},`, 200) + `{"a":1}]}`
	if pyLen(jsonLong) > 4000 && pyLen(jsonLong) <= 8000 {
		if out := shrinkToolResult(jsonLong, 2000); out != jsonLong {
			t.Fatal("complete JSON within 4x budget must stay")
		}
	}
}

func TestBuildAgentChatBodyNormalizesFlatTools(t *testing.T) {
	sess := mustSession(t)
	req := map[string]any{
		"model": "qwen3.8-max",
		"messages": []any{
			map[string]any{"role": "user", "content": "查天气"},
		},
		"tools": []any{
			// 平铺 function 工具（responses 风格顶层 name）→ 必须包成嵌套
			map[string]any{
				"type":        "function",
				"name":        "get_weather",
				"description": "查询城市天气",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"city": map[string]any{"type": "string"},
					},
				},
				"strict": true,
			},
			// 已是嵌套形态 → 原样透传（幂等）
			map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":       "read_file",
					"parameters": map[string]any{"type": "object"},
				},
			},
			// 非对象条目 → 丢弃
			"bogus",
			// 带 name 缺 description/parameters 的平铺工具 → 补空串/空对象
			map[string]any{"type": "function", "name": "bare"},
		},
	}
	encoded, err := BuildAgentChatBody(req, sess, "")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	body := decodeBody(t, encoded)

	tools, _ := body["tools"].([]any)
	if len(tools) != 3 {
		t.Fatalf("tools len = %d（非对象条目应被丢弃）: %+v", len(tools), tools)
	}

	// ① 平铺 → 嵌套，strict 保留，description/parameters 原样
	wrapped, _ := tools[0].(map[string]any)
	fn, _ := wrapped["function"].(map[string]any)
	if wrapped["type"] != "function" || fn == nil || fn["name"] != "get_weather" {
		t.Fatalf("flat tool not wrapped: %+v", tools[0])
	}
	if fn["description"] != "查询城市天气" {
		t.Fatalf("description lost: %+v", fn)
	}
	if fn["strict"] != true {
		t.Fatalf("strict must carry over: %+v", fn)
	}
	params, _ := fn["parameters"].(map[string]any)
	if params["type"] != "object" {
		t.Fatalf("parameters lost: %+v", fn)
	}

	// ② 已嵌套 → 字段级原样
	nested, _ := tools[1].(map[string]any)
	nestedFn, _ := nested["function"].(map[string]any)
	if nestedFn["name"] != "read_file" {
		t.Fatalf("nested tool must pass through: %+v", tools[1])
	}

	// ③ 缺失字段补默认：description 空串、parameters 空对象
	bare, _ := tools[2].(map[string]any)
	bareFn, _ := bare["function"].(map[string]any)
	if bareFn == nil || bareFn["name"] != "bare" || bareFn["description"] != "" {
		t.Fatalf("bare flat tool must get defaults: %+v", tools[2])
	}
	if _, ok := bareFn["parameters"].(map[string]any); !ok || len(bareFn["parameters"].(map[string]any)) != 0 {
		t.Fatalf("bare parameters must default to empty object: %+v", bareFn)
	}
}

func TestStructuredRejectMemory(t *testing.T) {
	structuredRejectMu.Lock()
	saved := structuredRejectMem
	structuredRejectMem = map[string]time.Time{}
	structuredRejectMu.Unlock()
	t.Cleanup(func() {
		structuredRejectMu.Lock()
		structuredRejectMem = saved
		structuredRejectMu.Unlock()
	})

	msgs := toolHistoryMessages()
	// 命中记忆前：结构化启用；记入后同模型立即回文本模式，区域隔离不连累。
	if !StructuredToolHistoryEnabled("cn", "deepseek-v4-flash", "dfmodel", msgs) {
		t.Fatal("deepseek must enable before memory")
	}
	NoteStructuredRejected("cn", "dfmodel")
	if StructuredToolHistoryEnabled("cn", "deepseek-v4-flash", "dfmodel", msgs) {
		t.Fatal("rejected model must fall back to text mode")
	}
	if StructuredToolHistoryEnabled("intl", "deepseek-v4-flash", "dfmodel", msgs) {
		t.Fatal("intl must never enable (realm gate)")
	}
	// 显式 on 压过记忆（用户手动强制时承担 400）。
	t.Setenv(structuredToolHistoryEnv, "on")
	if !StructuredToolHistoryEnabled("cn", "deepseek-v4-flash", "dfmodel", msgs) {
		t.Fatal("on mode must override memory")
	}
	// 过期即忘。
	structuredRejectMu.Lock()
	structuredRejectMem["cn|dfmodel"] = time.Now().Add(-structuredRejectTTL - time.Second)
	structuredRejectMu.Unlock()
	t.Setenv(structuredToolHistoryEnv, "")
	if !StructuredToolHistoryEnabled("cn", "deepseek-v4-flash", "dfmodel", msgs) {
		t.Fatal("expired memory must be forgotten")
	}
}

func TestCleanupOrphanToolHistory(t *testing.T) {
	batch := func(ids ...string) []any {
		out := []any{}
		for _, id := range ids {
			out = append(out, map[string]any{"id": id, "type": "function",
				"function": map[string]any{"name": "t", "arguments": "{}"}})
		}
		return out
	}
	toolMsg := func(id string) map[string]any {
		return map[string]any{"role": "tool", "tool_call_id": id, "content": "r"}
	}
	msgs := []any{
		map[string]any{"role": "user", "content": "hi"},
		map[string]any{"role": "assistant", "tool_calls": batch("a", "b")},
		toolMsg("a"),
		map[string]any{"role": "assistant", "tool_calls": batch("c")},
		map[string]any{"role": "assistant", "content": "done"},
	}
	out := CleanupOrphanToolHistory(msgs)
	// b 无结果 → 第一批整批删除（连 a 一起，防悬空）；c 无结果 → 删。
	got := 0
	for _, m := range out {
		mm := m.(map[string]any)
		if _, has := mm["tool_calls"]; has {
			t.Fatalf("orphan batch must be dropped, got %+v", mm)
		}
		if mm["role"] == "tool" {
			t.Fatalf("tool result of dropped batch must go too, got %+v", mm)
		}
		got++
	}
	if got != 4 {
		t.Fatalf("expect user + 3 assistants, got %d", got)
	}
	// 配对完整时原样保留。
	full := []any{
		map[string]any{"role": "assistant", "tool_calls": batch("x")},
		toolMsg("x"),
	}
	if out := CleanupOrphanToolHistory(full); len(out) != 2 {
		t.Fatalf("complete pairs must survive, got %d", len(out))
	}
}


// issue #101：出站 parameters.max_tokens 的钳制（超限压上限、0/负值压 1、缺省不注入）。
func TestBuildAgentChatBodyMaxTokensClamp(t *testing.T) {
	resetMaxOutLimits(t)
	sess := mustSession(t)
	LearnMaxOutputLimit(`Range of max_tokens should be [1, 131072]`, "qmodel_38max", "cn")

	// 超限 → 压到上限。
	req := map[string]any{
		"model":      "qwen3.8-max",
		"messages":   []any{map[string]any{"role": "user", "content": "hi"}},
		"max_tokens": float64(200000),
	}
	body := decodeBody(t, mustBuild(t, req, sess, "cn"))
	params, _ := body["parameters"].(map[string]any)
	if params["max_tokens"] != float64(131072) {
		t.Fatalf("clamped max_tokens = %v, want 131072", params["max_tokens"])
	}

	// 区间内原样。
	req2 := map[string]any{
		"model":      "qwen3.8-max",
		"messages":   []any{map[string]any{"role": "user", "content": "hi"}},
		"max_tokens": float64(256),
	}
	body2 := decodeBody(t, mustBuild(t, req2, sess, "cn"))
	params2, _ := body2["parameters"].(map[string]any)
	if params2["max_tokens"] != float64(256) {
		t.Fatalf("passthrough max_tokens = %v", params2["max_tokens"])
	}

	// 0/负值 → 1（不依赖是否学到上限）。
	req3 := map[string]any{
		"model":      "unknown-model-x",
		"messages":   []any{map[string]any{"role": "user", "content": "hi"}},
		"max_tokens": float64(0),
	}
	body3 := decodeBody(t, mustBuild(t, req3, sess, "cn"))
	params3, _ := body3["parameters"].(map[string]any)
	if params3["max_tokens"] != float64(1) {
		t.Fatalf("zero max_tokens = %v, want 1", params3["max_tokens"])
	}

	// 未声明 → 沿用模板默认值（模板自带 32768）；已知更低上限时同样压。
	req4 := map[string]any{
		"model":    "unknown-model-x",
		"messages": []any{map[string]any{"role": "user", "content": "hi"}},
	}
	body4 := decodeBody(t, mustBuild(t, req4, sess, "cn"))
	params4, _ := body4["parameters"].(map[string]any)
	if params4["max_tokens"] != float64(32768) {
		t.Fatalf("absent max_tokens must keep template default 32768, got %v", params4["max_tokens"])
	}
	// 模板默认值 32768 也要被学到的更低上限压住（否则模板默认会把低上限模型打成 400）。
	LearnMaxOutputLimit(`Range of max_tokens should be [1, 1000]`, "unknown-model-x", "cn")
	body4b := decodeBody(t, mustBuild(t, req4, sess, "cn"))
	params4b, _ := body4b["parameters"].(map[string]any)
	if params4b["max_tokens"] != float64(1000) {
		t.Fatalf("template default must clamp to learned limit, got %v", params4b["max_tokens"])
	}

	// max_completion_tokens 经 firstNonZeroInt 折叠后走同一路径。
	req5 := map[string]any{
		"model":                "qwen3.8-max",
		"messages":             []any{map[string]any{"role": "user", "content": "hi"}},
		"max_completion_tokens": float64(200000),
	}
	body5 := decodeBody(t, mustBuild(t, req5, sess, "cn"))
	params5, _ := body5["parameters"].(map[string]any)
	if params5["max_tokens"] != float64(131072) {
		t.Fatalf("max_completion_tokens clamp = %v, want 131072", params5["max_tokens"])
	}
}

func mustBuild(t *testing.T, req map[string]any, sess *CosySession, realm string) string {
	t.Helper()
	encoded, err := BuildAgentChatBody(req, sess, realm)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	return encoded
}

// 前缀会话亲和：关闭时每次随机（行为不变）；开启时同前缀复用。
func TestBuildAgentChatBodySessionAffinity(t *testing.T) {
	sess := mustSession(t)
	msgs := []any{
		map[string]any{"role": "system", "content": "sys"},
		map[string]any{"role": "user", "content": "hello there"},
	}

	// 默认关闭：同前缀两次 session_id 不同。
	t.Setenv("QD_SESSION_CACHE", "off")
	ResetAffinityStoreForTest()
	t.Cleanup(ResetAffinityStoreForTest)
	req := map[string]any{"model": "qwen3.8-max", "messages": msgs}
	body1 := decodeBody(t, mustBuild(t, req, sess, "cn"))
	body2 := decodeBody(t, mustBuild(t, req, sess, "cn"))
	if body1["session_id"] == body2["session_id"] {
		t.Fatalf("disabled cache must randomize session_id: %v", body1["session_id"])
	}

	// 开启（memory）：同前缀复用稳定 session_id。
	t.Setenv("QD_SESSION_CACHE", "memory")
	t.Setenv("QD_SESSION_CACHE_TTL", "3600")
	ResetAffinityStoreForTest()
	body3 := decodeBody(t, mustBuild(t, req, sess, "cn"))
	body4 := decodeBody(t, mustBuild(t, req, sess, "cn"))
	if body3["session_id"] != body4["session_id"] {
		t.Fatalf("enabled cache must reuse session_id: %v vs %v",
			body3["session_id"], body4["session_id"])
	}
	if body3["session_id"] == "" {
		t.Fatal("session_id must not be empty")
	}
	// 换模型不共享。
	reqOther := map[string]any{"model": "kmodel", "messages": msgs}
	body5 := decodeBody(t, mustBuild(t, reqOther, sess, "cn"))
	if body5["session_id"] == body3["session_id"] {
		t.Fatal("different model must not share session")
	}
}
