//go:build unit

package trae

import (
	"encoding/json"
	"regexp"
	"testing"
)

// preparedBody 构造并改写请求体，返回解析后的对象。
func preparedBody(t *testing.T, body map[string]any, variant Variant) map[string]any {
	t.Helper()
	src, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	out := PrepareLLMChatBody(src, variant, "deepseek-v4-flash", "u1", "dev1", "mach1")
	if !json.Valid(out) {
		t.Fatalf("输出不是合法 JSON: %s", out)
	}
	var parsed map[string]any
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatal(err)
	}
	return parsed
}

func TestModelConfigIsCaseInsensitiveAndFallsBack(t *testing.T) {
	cases := []struct {
		model          string
		configNameWant string
		modelNameWant  string
	}{
		{"DeepSeek-V4-Flash", "DeepSeek-V4-Flash", "deepseek_v4_flash__dev"},
		{"  GLM-5.3 ", "glm-5.3", "glm-5.3__dev"},
		{"seed-code-pro-0430", "Doubao-Seed-2.1-Pro", "Doubao-Seed-2.1-Pro__dev"},
		{"DeepSeek-V4-Flash-Official", "DeepSeek-V4-Flash-Official", "DeepSeek-V4-Flash-Official__dev"},
		{"kimi-k2.7-code", "kimi-k2.7-code", "kimi-k2.7-code__dev"},
	}
	for _, tc := range cases {
		configName, modelName := ModelConfig(tc.model)
		if configName != tc.configNameWant || modelName != tc.modelNameWant {
			t.Errorf("ModelConfig(%q) = (%s, %s), want (%s, %s)",
				tc.model, configName, modelName, tc.configNameWant, tc.modelNameWant)
		}
	}
}

// TestModelConfigUnknownDerived ★ 表里没有的模型名按规则派生（config_name = 名字、
// model_name = 名字 + "__dev"），绝不静默回落成默认模型——回落会让用户拿到
// 另一个模型的回答且没有任何报错。
func TestModelConfigUnknownDerived(t *testing.T) {
	cases := []struct{ model, want string }{
		{"glm-5.3-flash", "glm-5.3-flash__dev"},
		{"  qwen3.8-flash  ", "qwen3.8-flash__dev"},
		{"step-5-preview", "step-5-preview__dev"},
	}
	for _, tc := range cases {
		configName, modelName := ModelConfig(tc.model)
		if modelName != tc.want {
			t.Errorf("ModelConfig(%q).model_name = %s, want %s", tc.model, modelName, tc.want)
		}
		if configName == "DeepSeek-V4-Flash" {
			t.Errorf("未知模型 %q 不许被换成 DeepSeek", tc.model)
		}
	}
	// 空名退回内置默认模型再解析。
	if configName, _ := ModelConfig(""); configName != DefaultModel {
		t.Errorf("空名应等同于内置默认模型, got %s", configName)
	}
}

func TestStringContentConvertedToTextArray(t *testing.T) {
	out := preparedBody(t, map[string]any{
		"model":    "glm-5.3",
		"messages": []any{map[string]any{"role": "user", "content": "你好"}},
	}, VariantSoloCN)
	messages := out["messages"].([]any)
	content := messages[0].(map[string]any)["content"].([]any)
	if len(content) != 1 {
		t.Fatalf("content 数组长度 = %d, want 1", len(content))
	}
	segment := content[0].(map[string]any)
	if segment["type"] != "text" || segment["text"] != "你好" {
		t.Errorf("content 段 = %v", segment)
	}
}

func TestAssistantToolCallsBecomeFunctionCall(t *testing.T) {
	out := preparedBody(t, map[string]any{
		"model": "glm-5.3",
		"messages": []any{map[string]any{
			"role":    "assistant",
			"content": "",
			"tool_calls": []any{
				map[string]any{"id": "1", "function": map[string]any{"name": "read_file", "arguments": "{}"}},
				map[string]any{"id": "2", "function": map[string]any{"name": "  ", "arguments": "{}"}},
			},
		}},
	}, VariantSoloCN)
	calls := out["messages"].([]any)[0].(map[string]any)["tool_calls"].([]any)
	if len(calls) != 1 {
		t.Fatalf("无名 tool_call 必须被丢弃, got %d 条", len(calls))
	}
	call := calls[0].(map[string]any)
	if _, exists := call["function"]; exists {
		t.Error("不得同时保留 function")
	}
	if name := call["function_call"].(map[string]any)["name"]; name != "read_file" {
		t.Errorf("function_call.name = %v", name)
	}
}

func TestToolParametersStringified(t *testing.T) {
	out := preparedBody(t, map[string]any{
		"model":    "glm-5.3",
		"messages": []any{},
		"tools": []any{map[string]any{
			"type":     "function",
			"function": map[string]any{"name": "f", "parameters": map[string]any{"type": "object", "properties": map[string]any{}}},
		}},
	}, VariantSoloCN)
	tools := out["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools = %d 条", len(tools))
	}
	parameters := tools[0].(map[string]any)["function"].(map[string]any)["parameters"]
	text, ok := parameters.(string)
	if !ok {
		t.Fatalf("parameters 必须是 JSON 字符串, got %T", parameters)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("parameters 字符串不是合法 JSON: %v", err)
	}
	if parsed["type"] != "object" {
		t.Errorf("parameters.type = %v", parsed["type"])
	}
}

func TestToolChoiceNoneSuppressesTools(t *testing.T) {
	out := preparedBody(t, map[string]any{
		"model":       "glm-5.3",
		"messages":    []any{},
		"tool_choice": "none",
		"tools":       []any{map[string]any{"type": "function", "function": map[string]any{"name": "f"}}},
	}, VariantSoloCN)
	if _, exists := out["tools"]; exists {
		t.Error("tool_choice=none 应删除 tools")
	}
	if _, exists := out["tool_choice"]; exists {
		t.Error("tool_choice=none 应删除 tool_choice")
	}
}

func TestToolChoiceFunctionMapsToName(t *testing.T) {
	out := preparedBody(t, map[string]any{
		"model":       "glm-5.3",
		"messages":    []any{},
		"tool_choice": map[string]any{"type": "function", "function": map[string]any{"name": "read_file"}},
	}, VariantSoloCN)
	if out["tool_choice"] != "read_file" {
		t.Errorf("tool_choice = %v, want read_file", out["tool_choice"])
	}
}

// TestRequiredFieldsInjected ★ 固定字段全量断言（逐字对齐源 payload.rs）。
func TestRequiredFieldsInjected(t *testing.T) {
	out := preparedBody(t, map[string]any{
		"model":      "glm-5.3",
		"messages":   []any{},
		"stream":     false,
		"max_tokens": 1,
	}, VariantSoloCN)

	if out["stream"] != true {
		t.Error("上游只支持流式：客户端说 false 仍必须发 true")
	}
	if out["function"] != "solo_work_lite" {
		t.Errorf("function = %v, want solo_work_lite", out["function"])
	}
	if out["app_id"] != "6eefa01c-1036-4c7e-9ca5-d891f63bfcd8" {
		t.Errorf("app_id = %v", out["app_id"])
	}
	if out["ide_version"] != "0.1.50" {
		t.Errorf("ide_version = %v", out["ide_version"])
	}
	if out["ide_version_code"] != "20260811" {
		t.Errorf("ide_version_code = %v", out["ide_version_code"])
	}
	if out["workspace_id"] != "e04cdd" {
		t.Errorf("workspace_id = %v", out["workspace_id"])
	}
	if out["prompt_max_tokens"] != float64(168000) {
		t.Errorf("prompt_max_tokens = %v", out["prompt_max_tokens"])
	}
	if out["mode"] != "FunctionCall" {
		t.Errorf("mode = %v", out["mode"])
	}
	if out["package_type"] != "stable_cn" {
		t.Errorf("package_type = %v", out["package_type"])
	}
	if out["user_id"] != "u1" {
		t.Errorf("user_id = %v", out["user_id"])
	}
	if out["device_id"] != "dev1" {
		t.Errorf("device_id = %v", out["device_id"])
	}
	if out["machine_id"] != "mach1" {
		t.Errorf("machine_id = %v", out["machine_id"])
	}
	if out["config_name"] != "glm-5.3" || out["model_name"] != "glm-5.3__dev" {
		t.Errorf("模型映射 = (%v, %v)", out["config_name"], out["model_name"])
	}
	// 生成型字段必须存在且形如 UUID。
	uuidPattern := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	for _, key := range []string{"conversation_id", "session_id", "project_id"} {
		value, _ := out[key].(string)
		if !uuidPattern.MatchString(value) {
			t.Errorf("%s = %q, 不是 UUID 形态", key, value)
		}
	}
	// 三次生成互不相同。
	out2 := preparedBody(t, map[string]any{"model": "glm-5.3", "messages": []any{}}, VariantSoloCN)
	if out["conversation_id"] == out2["conversation_id"] {
		t.Error("conversation_id 应每次随机生成")
	}
}

// TestFunctionPerVariant ★ function 按程序位分家：TraeCode 家族必须是 chat_v3。
func TestFunctionPerVariant(t *testing.T) {
	out := preparedBody(t, map[string]any{
		"model":    "glm-5.3-flash",
		"messages": []any{map[string]any{"role": "user", "content": "ping"}},
	}, VariantTraeCN)
	if out["function"] != "chat_v3" {
		t.Errorf("TraeCode 的 function = %v, want chat_v3", out["function"])
	}
	// 客户端清单里没有该名的别名 ⇒ 只能派生，派生结果必须原样是模型名。
	if out["config_name"] != "glm-5.3-flash" || out["model_name"] != "glm-5.3-flash__dev" {
		t.Errorf("派生 = (%v, %v)", out["config_name"], out["model_name"])
	}
}

func TestMissingModelUsesDefault(t *testing.T) {
	out := preparedBody(t, map[string]any{"messages": []any{}}, VariantSoloCN)
	if out["config_name"] != "DeepSeek-V4-Flash" || out["model_name"] != "deepseek_v4_flash__dev" {
		t.Errorf("默认模型映射 = (%v, %v)", out["config_name"], out["model_name"])
	}
}

func TestInvalidJSONPassedThrough(t *testing.T) {
	raw := []byte("not json at all")
	if got := PrepareLLMChatBody(raw, VariantSoloCN, "m", "u", "d", "m"); string(got) != string(raw) {
		t.Error("非法 JSON 应原样返回")
	}
	array := []byte("[1,2,3]")
	if got := PrepareLLMChatBody(array, VariantSoloCN, "m", "u", "d", "m"); string(got) != string(array) {
		t.Error("合法 JSON 但非对象应原样返回")
	}
}

func TestModelAndStreamReaders(t *testing.T) {
	if ModelOf(map[string]any{"model": "x"}, "d") != "x" {
		t.Error("model 读取失败")
	}
	if ModelOf(map[string]any{"model": "  "}, "d") != "d" {
		t.Error("空白 model 应回落默认")
	}
	if ModelOf(map[string]any{}, "d") != "d" {
		t.Error("缺 model 应回落默认")
	}
	if WantsStream(map[string]any{}) {
		t.Error("缺 stream 应为 false")
	}
	if !WantsStream(map[string]any{"stream": true}) {
		t.Error("stream=true 应为 true")
	}
}

// TestModelsResponse 静态清单内容与 /v1/models 形状。
func TestModelsResponse(t *testing.T) {
	// 实测被上游拒绝的条目不得出现在对外清单。
	for _, bad := range []string{"openrouter//stealth/ox-alpha", "doubao-seed-2.0-code"} {
		for _, name := range ModelNames {
			if name == bad {
				t.Errorf("清单不得包含实测 4001 的 %s", bad)
			}
		}
	}
	// 默认模型与实测可调的名字必须在清单里。
	mustHave := []string{
		"deepseek-v4-flash", "Doubao-Seed-Evolving", "step-5-preview",
		"deepseek-v4.1-flash", "qwen3.8-max", "DeepSeek-V4-Flash-Official",
	}
	set := map[string]bool{}
	for _, name := range ModelNames {
		set[name] = true
	}
	for _, name := range mustHave {
		if !set[name] {
			t.Errorf("清单缺少 %s", name)
		}
	}
	// 每个名字都能解析出非空且 __dev 形态的 model_name。
	for _, name := range ModelNames {
		_, modelName := ModelConfig(name)
		if modelName == "" {
			t.Errorf("%s 解析为空", name)
		}
	}

	response := ModelsResponse()
	if response["object"] != "list" {
		t.Error("object 应为 list")
	}
	data := response["data"].([]any)
	if len(data) != len(ModelNames) {
		t.Fatalf("data = %d 条, want %d", len(data), len(ModelNames))
	}
	first := data[0].(map[string]any)
	if first["object"] != "model" || first["owned_by"] != "trae" {
		t.Errorf("条目形状漂了: %v", first)
	}
	if first["context_length"] != int64(131072) {
		t.Errorf("context_length = %v, want 131072", first["context_length"])
	}
}

func TestModelsResponseInjectable(t *testing.T) {
	// 可注入覆盖：真实上下文窗口优先；0 回落默认常量。
	response := ModelsResponseFor([]ModelEntry{
		{Name: "a", ContextLength: 256000},
		{Name: "b"},
	})
	data := response["data"].([]any)
	if data[0].(map[string]any)["id"] != "a" {
		t.Error("注入条目丢失")
	}
	if data[0].(map[string]any)["context_length"] != int64(256000) {
		t.Error("真实窗口优先失败")
	}
	if data[1].(map[string]any)["context_length"] != int64(131072) {
		t.Error("缺窗口必须回落成数值，不能是 0 或 null")
	}
}
