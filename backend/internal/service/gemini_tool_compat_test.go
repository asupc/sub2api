package service

import (
	"encoding/json"
	"strings"
	"testing"
)

func decodeJSONObject(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return out
}

func TestCleanToolSchemaDropsGeminiUnsupportedKeywords(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"lookup": map[string]any{
				"type":          "object",
				"propertyNames": map[string]any{"type": "string"},
				"additionalProperties": map[string]any{
					"anyOf": []any{
						map[string]any{"type": "string"},
						map[string]any{"type": "null"},
					},
				},
			},
			"tuple": map[string]any{
				"type":        "array",
				"prefixItems": []any{map[string]any{"type": "string"}},
			},
			"merged": map[string]any{
				"allOf": []any{
					map[string]any{
						"type":       "object",
						"properties": map[string]any{"a": map[string]any{"type": "string"}},
						"required":   []any{"a"},
					},
					map[string]any{
						"type":       "object",
						"properties": map[string]any{"b": map[string]any{"type": "integer"}},
					},
				},
			},
			"one": map[string]any{
				"oneOf": []any{
					map[string]any{"type": "string"},
					map[string]any{"type": "integer"},
				},
			},
		},
		"required": []any{"lookup"},
	}

	cleaned, ok := cleanToolSchema(schema).(map[string]any)
	if !ok {
		t.Fatal("expected object schema")
	}

	encoded, err := json.Marshal(cleaned)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{`"propertyNames"`, `"additionalProperties"`, `"prefixItems"`, `"allOf"`, `"oneOf"`} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("schema still contains %s: %s", forbidden, encoded)
		}
	}

	properties, _ := cleaned["properties"].(map[string]any)
	if _, exists := properties["lookup"]; !exists {
		t.Fatal("lookup property dropped")
	}
	merged, _ := properties["merged"].(map[string]any)
	mergedProps, _ := merged["properties"].(map[string]any)
	if _, exists := mergedProps["a"]; !exists {
		t.Fatalf("allOf properties not merged: %v", merged)
	}
	if _, exists := mergedProps["b"]; !exists {
		t.Fatalf("allOf properties not merged: %v", merged)
	}
	one, _ := properties["one"].(map[string]any)
	if _, exists := one["anyOf"]; !exists {
		t.Fatalf("oneOf was not folded into anyOf: %v", one)
	}
}

func TestConvertClaudeMessagesMergesConsecutiveUserTurns(t *testing.T) {
	body := []byte(`{
	  "model": "gemini-4-hydrogen",
	  "max_tokens": 1024,
	  "messages": [
	    {"role": "user", "content": [{"type": "text", "text": "list files"}]},
	    {"role": "assistant", "content": [
	      {"type": "tool_use", "id": "toolu_1", "name": "Bash", "input": {"command": "ls"}}
	    ]},
	    {"role": "user", "content": [
	      {"type": "tool_result", "tool_use_id": "toolu_1", "content": "a.txt"}
	    ]},
	    {"role": "user", "content": [{"type": "text", "text": "now read a.txt"}]}
	  ],
	  "tools": [
	    {"name": "Bash", "description": "run", "input_schema": {"type": "object", "properties": {"command": {"type": "string"}}}}
	  ]
	}`)

	out, err := convertClaudeMessagesToGeminiGenerateContent(body, "gemini-4-hydrogen")
	if err != nil {
		t.Fatal(err)
	}
	decoded := decodeJSONObject(t, out)
	contents, _ := decoded["contents"].([]any)
	// 工具结果之后紧跟的用户文本仍单独成轮：Gemini 拒绝 functionResponse 之后
	// 还带其它 part 的轮次（上游 v0.2.24 修复），因此这里是 4 轮而不是合并后的 3 轮，
	// 最后一轮只承载文本。
	if len(contents) != 4 {
		t.Fatalf("expected 4 turns (tool result and trailing text split), got %d: %s", len(contents), out)
	}
	toolTurn, _ := contents[2].(map[string]any)
	toolParts, _ := toolTurn["parts"].([]any)
	if len(toolParts) != 1 {
		t.Fatalf("expected the tool_result turn to carry only functionResponse, got %v", toolParts)
	}
	last, _ := contents[3].(map[string]any)
	parts, _ := last["parts"].([]any)
	if len(parts) != 1 {
		t.Fatalf("expected trailing user text in its own turn, got %v", parts)
	}
	if role, _ := last["role"].(string); role != "user" {
		t.Fatalf("expected user role, got %v", last["role"])
	}
	first, _ := toolParts[0].(map[string]any)
	if _, ok := first["functionResponse"]; !ok {
		t.Fatalf("expected functionResponse as first part: %v", toolParts[0])
	}
}

func TestToolUseSignatureFallsBackToDummyWhenImplausible(t *testing.T) {
	body := []byte(`{
	  "model": "gemini-4-hydrogen",
	  "max_tokens": 256,
	  "messages": [
	    {"role": "user", "content": [{"type": "text", "text": "hi"}]},
	    {"role": "assistant", "content": [
	      {"type": "tool_use", "id": "toolu_1", "name": "Bash", "input": {"command": "ls"}, "signature": "foreign-signature"}
	    ]},
	    {"role": "user", "content": [{"type": "tool_result", "tool_use_id": "toolu_1", "content": "ok"}]}
	  ]
	}`)
	out, err := convertClaudeMessagesToGeminiGenerateContent(body, "gemini-4-hydrogen")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), geminiDummyThoughtSignature) {
		t.Fatalf("expected dummy signature fallback, got: %s", out)
	}
	if strings.Contains(string(out), "foreign-signature") {
		t.Fatalf("implausible signature leaked upstream: %s", out)
	}
}
