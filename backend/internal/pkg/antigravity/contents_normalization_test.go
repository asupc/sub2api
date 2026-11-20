package antigravity

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTransformClaudeToGeminiMergesConsecutiveTurns(t *testing.T) {
	raw := []byte(`{
	  "model": "gemini-4-argon-medium",
	  "max_tokens": 2048,
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

	var req ClaudeRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatal(err)
	}
	body, err := TransformClaudeToGemini(&req, "proj", "gemini-4-argon-medium")
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Request struct {
			Contents []GeminiContent `json:"contents"`
		} `json:"request"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Request.Contents) != 3 {
		t.Fatalf("expected 3 merged turns, got %d: %s", len(decoded.Request.Contents), body)
	}
	last := decoded.Request.Contents[2]
	if last.Role != "user" {
		t.Fatalf("expected user role, got %q", last.Role)
	}
	if len(last.Parts) != 2 {
		t.Fatalf("expected tool_result and text merged, got %d parts", len(last.Parts))
	}
	if last.Parts[0].FunctionResponse == nil {
		t.Fatalf("expected functionResponse first, got %+v", last.Parts[0])
	}
}

func TestTransformClaudeToGeminiMovesFunctionResponseIntoUserTurn(t *testing.T) {
	raw := []byte(`{
	  "model": "gemini-4-argon-medium",
	  "max_tokens": 1024,
	  "messages": [
	    {"role": "user", "content": [{"type": "text", "text": "hi"}]},
	    {"role": "assistant", "content": [
	      {"type": "tool_use", "id": "toolu_9", "name": "Bash", "input": {"command": "ls"}},
	      {"type": "tool_result", "tool_use_id": "toolu_9", "content": "out"}
	    ]}
	  ]
	}`)

	var req ClaudeRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatal(err)
	}
	body, err := TransformClaudeToGemini(&req, "proj", "gemini-4-argon-medium")
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Request struct {
			Contents []GeminiContent `json:"contents"`
		} `json:"request"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, content := range decoded.Request.Contents {
		for _, part := range content.Parts {
			if part.FunctionResponse != nil && content.Role != "user" {
				t.Fatalf("functionResponse leaked into %q turn: %s", content.Role, body)
			}
		}
	}
}

func TestSanitizeGeminiThoughtSignature(t *testing.T) {
	valid := "EpABCo0BAWkUfRMfAjekqUshrLDYaZSCAwjfa3FHGOQrTsWwi3QyuCgpaN1bRdMbTVuv74TrRctAZwN6CGvqCjZfxOfB2LltnDABADxq6orIeJLGQpVB8BHDB18l7c5B3zM7DaDEuR6epapfyJcg9j9jOEQ1JNuyIFXw/zMo7z7AB6J2KM5QTenzH6F7ZDTlQ2o1"

	cases := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "empty", input: "", expected: ""},
		{name: "dummy passthrough", input: DummyThoughtSignature, expected: DummyThoughtSignature},
		{name: "short foreign signature", input: "foreign-signature", expected: ""},
		{name: "non base64", input: strings.Repeat("!", 64), expected: ""},
		{name: "real gemini signature", input: valid, expected: valid},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := sanitizeGeminiThoughtSignature(testCase.input); got != testCase.expected {
				t.Fatalf("expected %q, got %q", testCase.expected, got)
			}
		})
	}
}
