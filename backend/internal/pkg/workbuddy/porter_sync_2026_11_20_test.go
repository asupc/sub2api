//go:build unit

package workbuddy

// porter_sync_2026_11_20_test.go 2026-11-20 平台移植源同步的回归用例
//（workbuddy2api-panel 96265d7 / 733942a / e14a3d0 / f987bac / e4e6f5c /
// 4830686 / d3edea9 / b35a8d0）。

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestClientIdentityHeadersRealmIsolation(t *testing.T) {
	cn := Credentials{UID: "u1"}
	req := &http.Request{Header: http.Header{}}
	CommonHeaders(req, cn)
	if got := req.Header.Get("X-Client-Version"); got != defaultClientVersionCN {
		t.Fatalf("cn X-Client-Version = %q", got)
	}
	if req.Header.Get("X-Client-Platform") != "windows" || req.Header.Get("X-Client-Product") != "WorkBuddy" {
		t.Fatalf("cn X-Client-* missing: %+v", req.Header)
	}

	gl := Credentials{UID: "u1", Realm: "global"}
	req2 := &http.Request{Header: http.Header{}}
	CommonHeaders(req2, gl)
	if req2.Header.Get("X-Client-Version") != "" || req2.Header.Get("X-Client-Product") != "" {
		t.Fatalf("global must not send X-Client-*: %+v", req2.Header)
	}

	req3 := &http.Request{Header: http.Header{}}
	BillingHeaders(req3, cn)
	if req3.Header.Get("X-Client-Version") == "" {
		t.Fatal("billing must carry X-Client-* for cn")
	}
	req4 := &http.Request{Header: http.Header{}}
	BillingHeaders(req4, gl)
	if req4.Header.Get("X-Client-Version") != "" {
		t.Fatal("billing must not carry X-Client-* for global")
	}
}

func TestDeepNormalizeSchemaProtectsDataAndContainers(t *testing.T) {
	schema := map[string]any{
		"type":    "object",
		"default": map[string]any{"type": "str", "items": []any{"a"}},
		"const":   nil,
		"properties": map[string]any{
			"p": map[string]any{
				"type": "object",
				"dependentRequired": map[string]any{
					"items": []any{"type", "items"},
				},
				"required":    []any{"a", "a", " "},
				"minimum":     "1",
				"description": 123,
				"pattern":     `^[^\uD800-\uDFFF]*$`,
			},
		},
	}
	out := deepNormalizeSchema(schema)
	def, _ := out["default"].(map[string]any)
	if def["type"] != "str" {
		t.Fatalf("default must be treated as data: %#v", def)
	}
	if v, ok := out["const"]; !ok || v != nil {
		t.Fatalf("const:null must be preserved: %#v", out)
	}
	p := out["properties"].(map[string]any)["p"].(map[string]any)
	if dep, _ := p["dependentRequired"].(map[string]any); dep == nil || len(dep["items"].([]any)) != 2 {
		t.Fatalf("dependentRequired keys are property names: %#v", p["dependentRequired"])
	}
	if req, _ := p["required"].([]any); len(req) != 1 {
		t.Fatalf("required dedupe = %#v", p["required"])
	}
	if p["minimum"] != float64(1) {
		t.Fatalf("minimum coercion = %#v", p["minimum"])
	}
	if _, has := p["description"]; has {
		t.Fatalf("non-string description must be dropped: %#v", p["description"])
	}
	if pat, _ := p["pattern"].(string); pat != `^[^\x{D800}-\x{DFFF}]*$` {
		t.Fatalf("pattern translation = %q", pat)
	}
}

func TestNormalizeToolParametersExpandsTopLevelRef(t *testing.T) {
	fn := map[string]any{"parameters": map[string]any{
		"$ref": "#/$defs/Args",
		"$defs": map[string]any{
			"Args": map[string]any{"type": "object", "properties": map[string]any{"q": map[string]any{"type": "string"}}},
		},
	}}
	normalizeToolParameters(fn)
	params := fn["parameters"].(map[string]any)
	if _, has := params["$ref"]; has {
		t.Fatal("top-level $ref must be removed")
	}
	if _, ok := params["properties"].(map[string]any)["q"]; !ok {
		t.Fatalf("$ref must be expanded: %#v", params)
	}
}

func TestReorderEarlyToolResultsCrossRoundReusedIDs(t *testing.T) {
	call := func(id string) map[string]any {
		return map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"id": id}}}
	}
	tool := func(id string) map[string]any {
		return map[string]any{"role": "tool", "tool_call_id": id, "content": "r"}
	}
	msgs := []any{
		map[string]any{"role": "user", "content": "u1"},
		call("call_0"), tool("call_0"),
		map[string]any{"role": "user", "content": "u2"},
		call("call_0"), tool("call_0"),
	}
	out, changed := reorderEarlyToolResults(msgs)
	if changed {
		t.Fatalf("already-correct history must be untouched, got %#v", out)
	}

	// 第二轮结果早到：只允许把这一条挪到第二轮声明之后。
	early := []any{
		map[string]any{"role": "user", "content": "u1"},
		call("call_0"), tool("call_0"),
		map[string]any{"role": "user", "content": "u2"},
		tool("call_0"), call("call_0"),
	}
	out2, changed2 := reorderEarlyToolResults(early)
	if !changed2 {
		t.Fatal("second-round early result must be reordered")
	}
	if out2[1].(map[string]any)["role"] != "assistant" || out2[2].(map[string]any)["role"] != "tool" {
		t.Fatalf("first round pair disturbed: %#v", out2)
	}
	if out2[4].(map[string]any)["role"] != "assistant" || out2[5].(map[string]any)["role"] != "tool" {
		t.Fatalf("second round pair not fixed: %#v", out2)
	}
}

func TestGuardEmptyTextParts(t *testing.T) {
	msgs := []any{
		map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "text", "text": ""},
			map[string]any{"type": "text", "text": "keep"},
			map[string]any{"type": "textarea", "text": ""},
			map[string]any{"type": "text", "text": nil},
		}},
		map[string]any{"role": "tool", "content": `[{"type":"text","text":""}]`},
	}
	if !guardEmptyTextParts(msgs) {
		t.Fatal("expected changes")
	}
	parts := msgs[0].(map[string]any)["content"].([]any)
	if parts[0].(map[string]any)["text"] != " " || parts[3].(map[string]any)["text"] != " " {
		t.Fatalf("empty text not filled: %#v", parts)
	}
	if parts[2].(map[string]any)["text"] != "" {
		t.Fatal("unknown part type must stay untouched")
	}
	toolContent := msgs[1].(map[string]any)["content"].(string)
	if toolContent == `[{"type":"text","text":""}]` {
		t.Fatalf("sniffed parts not filled: %s", toolContent)
	}
}

func TestMirrorUpstreamReasoning(t *testing.T) {
	obj := map[string]any{
		"model": "kimi-k3",
		"messages": []any{
			map[string]any{"role": "assistant", "reasoning_content": "think"},
			map[string]any{"role": "assistant", "reasoning_content": "x", "reasoning": "native"},
			map[string]any{"role": "user", "reasoning_content": "u"},
		},
	}
	if !mirrorUpstreamReasoning(obj) {
		t.Fatal("expected mirror")
	}
	msgs := obj["messages"].([]any)
	if msgs[0].(map[string]any)["reasoning"] != "think" {
		t.Fatal("reasoning not mirrored")
	}
	if msgs[1].(map[string]any)["reasoning"] != "native" {
		t.Fatal("existing reasoning must not be overwritten")
	}
	if _, has := msgs[2].(map[string]any)["reasoning"]; has {
		t.Fatal("user messages must not be mirrored")
	}
	gpt := map[string]any{"model": "gpt-5.4", "messages": []any{map[string]any{"role": "assistant", "reasoning_content": "x"}}}
	if mirrorUpstreamReasoning(gpt) {
		t.Fatal("gpt must not be mirrored")
	}
}

func TestClassifyRequestLevelCodes(t *testing.T) {
	cases := []struct {
		name string
		body string
		want ErrorKind
	}{
		{"tool name 11152", `{"code":11152,"msg":"invalid tool name"}`, KindBadParams},
		{"empty content 11151", `{"code":11151,"msg":"empty content"}`, KindBadParams},
		{"backend unsupported 11103", `{"code":11103,"msg":"Backend [x] is not supported"}`, KindModelBlocked},
		{"provider overload 11134 429", `{"code":11134,"msg":"the model provider is temporarily unavailable"}`, KindUpstreamBusy},
		{"restriction marker", `{"code":11140,"msg":"账号存在异常行为，已被限制使用"}`, KindAccountFault},
		{"plain 11140 rate limit", `{"code":11140,"msg":"The model provider is rate-limiting requests."}`, KindSoftRate},
	}
	for _, c := range cases {
		if got := ClassifyError(400, []byte(c.body)); got != c.want {
			t.Errorf("%s: got %s want %s", c.name, got, c.want)
		}
	}
	if got := ClassifyError(429, []byte(`{"code":11134,"msg":"provider unavailable"}`)); got != KindUpstreamBusy {
		t.Fatalf("11134 on 429 = %s", got)
	}
}

func TestParseBodyRetryAfter(t *testing.T) {
	d, ok := ParseBodyRetryAfter([]byte(`{"code":11134,"retry_policy":{"retry_after":8}}`))
	if !ok || d.Seconds() != 8 {
		t.Fatalf("retry_after = %v %v", d, ok)
	}
	if _, ok := ParseBodyRetryAfter([]byte(`{"code":0}`)); ok {
		t.Fatal("missing retry_after must be false")
	}
}

func TestEnsureConsoleSystemDoesNotDeclareIdentity(t *testing.T) {
	out := ensureConsoleSystem([]byte(`{"messages":[{"role":"user","content":"hi"}]}`))
	if string(out) == "" {
		t.Fatal("empty output")
	}
	var obj map[string]any
	if err := json.Unmarshal(out, &obj); err != nil {
		t.Fatal(err)
	}
	msgs := obj["messages"].([]any)
	first := msgs[0].(map[string]any)
	if first["role"] != "system" {
		t.Fatalf("fallback system missing: %#v", first)
	}
	if text, _ := first["content"].(string); text != consoleSystemFallback {
		t.Fatalf("fallback text = %q", text)
	}
	already := []byte(`{"messages":[{"role":"system","content":"s"},{"role":"user","content":"u"}]}`)
	if string(ensureConsoleSystem(already)) != string(already) {
		t.Fatal("existing system must be untouched")
	}
}
