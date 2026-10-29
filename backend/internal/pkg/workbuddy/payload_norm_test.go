//go:build unit

package workbuddy

// payload_norm_test.go hub 全面对照移植（2026-10-29）的回归测试：
// 通用身份句正则 / DSH GUI 与文件引用段 / OmO 指纹 / 作用域拆分 /
// tool arguments 序列化（11101）/ stop 归一化（11101）/ tools Schema 规整
//（11129）/ 拆条合并与结果归批（11148）。

import (
	"encoding/json"
	"strings"
	"testing"
)

// --- 通用身份句正则（hub SANITIZE_IDENTITY_RE，7 动词表） ---

func TestSanitizeIdentityGenericVerbs(t *testing.T) {
	cases := map[string]string{ // 输入 → 期望输出
		"You are an AI agent powered by DeepSeek Harness.":                 "You are a helpful AI assistant.",
		"You are a coding agent powered by the deepseek-v4.1-flash model.": "You are a helpful AI assistant.",
		"You are an AI assistant built on DeepSeek.":                       "You are a helpful AI assistant.",
		"You are Product Agent, built by Vendor.":                          "You are a helpful AI assistant.",
		"You are a model created by Acme.":                                 "You are a helpful AI assistant.",
		"You are an assistant developed by Acme.":                          "You are a helpful AI assistant.",
		"You are the helper made by Acme.":                                 "You are a helpful AI assistant.",
		"You are an LLM trained by Acme.":                                  "You are a helpful AI assistant.",
		"You are powered by Acme.":                                         "You are a helpful AI assistant.",
	}
	for in, want := range cases {
		got := sanitizeText(in, true)
		if got != want {
			t.Errorf("identity rewrite:\n in=%q\ngot=%q\nwant=%q", in, got, want)
		}
		// 幂等：改写后的文本不再命中。
		if again := sanitizeText(got, true); again != got {
			t.Errorf("not idempotent: %q -> %q", got, again)
		}
	}
}

// 分词修饰宾语 / 零散文本不受影响（有界窗口，hub #550 注释）。
func TestSanitizeIdentityFalsePositivesNotTouched(t *testing.T) {
	for _, in := range []string{
		"You are free to use tools built by anyone.",
		"You should be powered by electricity.",
		"they are built by Acme and sold worldwide",
		"You are helpful.",
	} {
		if out := sanitizeText(in, true); out != in {
			t.Errorf("should be untouched: %q -> %q", in, out)
		}
	}
}

// 作用域（hub #537/#574）：身份句族只改写 system/developer/assistant；
// user/tool 内的同句属用户数据不改写；固定句改写对全角色生效。
func TestSanitizeIdentityScopeByRole(t *testing.T) {
	const identitySentence = "You are an AI agent powered by DeepSeek Harness."
	for _, role := range []string{"system", "developer", "assistant"} {
		ms := []any{map[string]any{"role": role, "content": identitySentence}}
		if !sanitizeMessages(ms) {
			t.Fatalf("%s 角色的身份句未被改写", role)
		}
		got, _ := ms[0].(map[string]any)["content"].(string)
		if strings.Contains(got, "powered by") {
			t.Errorf("%s 角色残留身份句: %q", role, got)
		}
	}
	for _, role := range []string{"user", "tool"} {
		ms := []any{map[string]any{"role": role, "content": identitySentence}}
		sanitizeMessages(ms)
		got, _ := ms[0].(map[string]any)["content"].(string)
		if got != identitySentence {
			t.Errorf("%s 角色不应改写用户数据: %q", role, got)
		}
	}
	// 固定句改写（11-128 反探测串）对 user 同样生效。
	// 反探测串字节取自包内 rewrite 对（源串与替换串仅一字之差，勿手打——会失真）。
	fingerprint := sanitizeRewrites[len(sanitizeRewrites)-1][0]
	ms := []any{map[string]any{"role": "user", "content": "code was " + fingerprint}}
	if !sanitizeMessages(ms) {
		t.Fatal("user 的反探测串未被改写")
	}
	got, _ := ms[0].(map[string]any)["content"].(string)
	if strings.Contains(got, fingerprint) {
		t.Errorf("反探测串残留: %q", got)
	}
}

// --- DSH GUI 描述段（hub #579） ---

func TestSanitizeDshGUIPhrase(t *testing.T) {
	const in = "You are interacting with the user through the DeepSeek Harness Web GUI whose URL the runtime context names.\nWhen the user refers to \"this page\" it means that GUI."
	got := sanitizeText(in, true)
	if strings.Contains(got, "whose URL the runtime context names") {
		t.Errorf("DSH GUI 指纹残留: %q", got)
	}
	if !strings.Contains(got, "whose URL is named in the runtime context.") {
		t.Errorf("DSH GUI 改写缺失: %q", got)
	}
	if !strings.Contains(got, "DeepSeek Harness Web GUI") {
		t.Errorf("品牌全名应保留: %q", got)
	}
	// 幂等。
	if again := sanitizeText(got, true); again != got {
		t.Errorf("DSH GUI 改写不幂等: %q -> %q", got, again)
	}
	// user 角色不改写（作用域一致）。
	ms := []any{map[string]any{"role": "user", "content": in}}
	sanitizeMessages(ms)
	if got, _ := ms[0].(map[string]any)["content"].(string); got != in {
		t.Error("user 角色不应改写 DSH GUI 段")
	}
}

// --- DSH 文件引用段（hub #683） ---

func TestSanitizeDshFileRefPhrase(t *testing.T) {
	const in = "Tokens prefixed with @ are paths the user explicitly referenced."
	got := sanitizeText(in, true)
	want := "Tokens prefixed with @ are paths that the user explicitly referenced."
	if got != want {
		t.Errorf("fileref rewrite:\n got=%q\nwant=%q", got, want)
	}
	if again := sanitizeText(got, true); again != got {
		t.Errorf("fileref 不幂等: %q -> %q", got, again)
	}
	// user 角色不改写。
	ms := []any{map[string]any{"role": "user", "content": in}}
	sanitizeMessages(ms)
	if got, _ := ms[0].(map[string]any)["content"].(string); got != in {
		t.Error("user 角色不应改写 fileref 段")
	}
}

// --- OmO 子代理指纹（hub #62，全角色） ---

func TestSanitizeOmoJuniorPhrase(t *testing.T) {
	const in = "Role: Sisyphus-Junior - Focused executor from OhMyOpenCode, handle the task."
	for _, role := range []string{"system", "user", "tool"} {
		ms := []any{map[string]any{"role": role, "content": in}}
		if !sanitizeMessages(ms) {
			t.Fatalf("%s 角色的 OmO 指纹未被改写", role)
		}
		got, _ := ms[0].(map[string]any)["content"].(string)
		if strings.Contains(got, "from OhMyOpenCode") {
			t.Errorf("%s 角色 OmO 指纹残留: %q", role, got)
		}
		if !strings.Contains(got, "Sisyphus-Junior - Focused executor") {
			t.Errorf("%s 角色 OmO 改写过度: %q", role, got)
		}
	}
	// 单独的 OhMyOpenCode 不改写（不扩大打击面）。
	const benign = "Install OhMyOpenCode and run it."
	if out := sanitizeText(benign, true); out != benign {
		t.Errorf("单独品牌词不应改写: %q -> %q", benign, out)
	}
}

// --- tool arguments 序列化（hub #446，11101） ---

func TestSanitizeToolCallsArgumentsSerialized(t *testing.T) {
	calls := []any{
		map[string]any{"id": "c1", "function": map[string]any{"name": "f", "arguments": map[string]any{"a": float64(1)}}},
		map[string]any{"id": "c2", "function": map[string]any{"name": "f", "arguments": nil}},
		map[string]any{"id": "c3", "function": map[string]any{"name": "f"}},
		map[string]any{"id": "c4", "function": map[string]any{"name": "f", "arguments": "{\"k\":2}"}},
	}
	if !sanitizeToolCalls(calls) {
		t.Fatal("expected change")
	}
	get := func(i int) map[string]any { return calls[i].(map[string]any)["function"].(map[string]any) }
	if s, ok := get(0)["arguments"].(string); !ok || s != `{"a":1}` {
		t.Errorf("dict arguments 应序列化为 JSON 字符串，got %#v", get(0)["arguments"])
	}
	if s, ok := get(1)["arguments"].(string); !ok || s != "{}" {
		t.Errorf("null arguments 应补 \"{}\"，got %#v", get(1)["arguments"])
	}
	if s, ok := get(2)["arguments"].(string); !ok || s != "{}" {
		t.Errorf("缺失 arguments 应补 \"{}\"，got %#v", get(2)["arguments"])
	}
	if s, ok := get(3)["arguments"].(string); !ok || s != `{"k":2}` {
		t.Errorf("字符串 arguments 应原样保留，got %#v", get(3)["arguments"])
	}
	// 净化后的 arguments 必须仍可解析（序列化不破坏 JSON）。
	var parsed any
	if err := json.Unmarshal([]byte(get(0)["arguments"].(string)), &parsed); err != nil {
		t.Errorf("序列化结果不是合法 JSON: %v", err)
	}
}

// --- stop 归一化（hub v1.7.0，11101） ---

func TestNormalizeStop(t *testing.T) {
	cases := []struct {
		in   string
		want string // 期望 marshal 后的 stop 值（无该键 = 删除）
	}{
		{`{"stop":"END"}`, `["END"]`},
		{`{"stop":["a","b"]}`, `["a","b"]`},
		{`{"stop":["a","","b",1]}`, `["a","b"]`},
		{`{"stop":""}`, ``},
		{`{"stop":[]}`, ``},
		{`{"stop":null}`, ``},
		{`{"stop":42}`, ``},
		{`{"model":"m"}`, ``},
	}
	for _, tc := range cases {
		var obj map[string]any
		if err := json.Unmarshal([]byte(tc.in), &obj); err != nil {
			t.Fatal(err)
		}
		normalizeStop(obj)
		raw, has := obj["stop"]
		if tc.want == "" {
			if has {
				t.Errorf("%s: stop 应删除，got %#v", tc.in, raw)
			}
			continue
		}
		enc, _ := json.Marshal(raw)
		if string(enc) != tc.want {
			t.Errorf("%s: stop=%s want %s", tc.in, enc, tc.want)
		}
	}
}

// --- tools Schema 规整（hub #451，11129） ---

func TestNormalizeToolsSchemaRepair(t *testing.T) {
	obj := map[string]any{
		"tools": []any{
			// 无参工具：parameters {}（缺 type）→ 补齐。
			map[string]any{"type": "function", "function": map[string]any{"name": "noop", "parameters": map[string]any{}}},
			// parameters 缺失 → 补齐。
			map[string]any{"type": "function", "function": map[string]any{"name": "noparams"}},
			// 顶层 name 风格 → 包装。
			map[string]any{"name": "toplevel", "description": "d", "parameters": map[string]any{"type": "str", "properties": map[string]any{"x": map[string]any{"type": "int", "description": nil}}}, "strict": true},
			// 类型别名 + required 脏值 + null 元数据。
			map[string]any{"type": "function", "function": map[string]any{"name": "messy", "parameters": map[string]any{
				"type":       "dict",
				"properties": map[string]any{"a": map[string]any{"type": "bool", "title": nil}},
				"required":   []any{nil, "", "a"},
			}}},
		},
	}
	normalizeTools(obj)
	tools := obj["tools"].([]any)
	if len(tools) != 4 {
		t.Fatalf("tools 数量=%d want 4", len(tools))
	}
	fnAt := func(i int) map[string]any { return tools[i].(map[string]any)["function"].(map[string]any) }
	for i := 0; i < 4; i++ {
		fn := fnAt(i)
		p := fn["parameters"].(map[string]any)
		if p["type"] != "object" {
			t.Errorf("tool %d parameters.type=%v want object", i, p["type"])
		}
		if _, ok := p["properties"].(map[string]any); !ok {
			t.Errorf("tool %d parameters.properties 应为字典", i)
		}
	}
	// 顶层 name 风格包装 + strict 随迁。
	top := fnAt(2)
	if top["name"] != "toplevel" || top["strict"] != true {
		t.Errorf("顶层工具包装错误: %#v", top)
	}
	if p := top["parameters"].(map[string]any); p["type"] != "object" {
		t.Errorf("顶层工具根 type=%v want object", p["type"])
	}
	x := top["parameters"].(map[string]any)["properties"].(map[string]any)["x"].(map[string]any)
	if x["type"] != "integer" {
		t.Errorf("别名 int 应归一 integer，got %v", x["type"])
	}
	if _, has := x["description"]; has {
		t.Error("null description 应剔除")
	}
	// messy：dict→object、required 清洗。
	messy := fnAt(3)["parameters"].(map[string]any)
	if messy["type"] != "object" {
		t.Errorf("别名 dict 应归一 object，got %v", messy["type"])
	}
	req, _ := messy["required"].([]any)
	if len(req) != 1 || req[0] != "a" {
		t.Errorf("required=%v want [a]", messy["required"])
	}
	if _, has := messy["properties"].(map[string]any)["a"].(map[string]any)["title"]; has {
		t.Error("null title 应剔除")
	}
	// functions 旧形态 → tools。
	obj2 := map[string]any{"functions": []any{map[string]any{"name": "old"}}}
	normalizeTools(obj2)
	if _, has := obj2["functions"]; has {
		t.Error("functions 应转换为 tools")
	}
	if tools2, ok := obj2["tools"].([]any); !ok || len(tools2) != 1 {
		t.Errorf("functions→tools 转换失败: %#v", obj2["tools"])
	}
}

// --- 拆条合并（hub merge_adjacent_tool_calls） ---

func TestMergeAdjacentToolCalls(t *testing.T) {
	call := func(id string) map[string]any {
		return map[string]any{"id": id, "function": map[string]any{"name": "f", "arguments": "{}"}}
	}
	// 形态 1：相邻 assistant，后条仅 tool_calls + 空 content。
	ms := []any{
		map[string]any{"role": "user", "content": "hi"},
		map[string]any{"role": "assistant", "content": "", "tool_calls": []any{call("c1")}, "reasoning_content": "r1"},
		map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{call("c2")}, "reasoning_content": "r2"},
		map[string]any{"role": "tool", "tool_call_id": "c1", "content": "ok"},
		map[string]any{"role": "tool", "tool_call_id": "c2", "content": "ok"},
	}
	out, changed := MergeAdjacentToolCalls(ms)
	if !changed {
		t.Fatal("形态 1 应合并")
	}
	if len(out) != 4 {
		t.Fatalf("len=%d want 4", len(out))
	}
	merged := out[1].(map[string]any)
	if tcs := merged["tool_calls"].([]any); len(tcs) != 2 {
		t.Errorf("合并后 tool_calls=%d want 2", len(tcs))
	}
	if rc := merged["reasoning_content"].(string); rc != "r1\nr2" {
		t.Errorf("reasoning_content=%q want r1\\nr2", rc)
	}

	// 形态 2：纯字符串 content 折进前条（[] 空 content）。
	ms2 := []any{
		map[string]any{"role": "assistant", "content": []any{}, "tool_calls": []any{call("c1")}},
		map[string]any{"role": "assistant", "content": "extra note"},
		map[string]any{"role": "tool", "tool_call_id": "c1", "content": "ok"},
	}
	out2, changed2 := MergeAdjacentToolCalls(ms2)
	if !changed2 {
		t.Fatal("形态 2 应合并")
	}
	if len(out2) != 2 {
		t.Fatalf("len=%d want 2", len(out2))
	}
	if c := out2[0].(map[string]any)["content"].(string); c != "extra note" {
		t.Errorf("content=%q want extra note", c)
	}

	// 非相邻 / 前条无 tool_calls 不合并。
	ms3 := []any{
		map[string]any{"role": "assistant", "content": "thinking"},
		map[string]any{"role": "assistant", "content": "", "tool_calls": []any{call("c1")}},
	}
	if _, changed3 := MergeAdjacentToolCalls(ms3); changed3 {
		t.Error("前条无 tool_calls 不应合并")
	}
}

// --- 结果归批（hub repack_tool_result_blocks，#420） ---

func TestRepackToolResultBlocks(t *testing.T) {
	call := func(id string) map[string]any {
		return map[string]any{"id": id, "function": map[string]any{"name": "f", "arguments": "{}"}}
	}
	// 插队消息（结果之前 + 结果之间）移到整批结果之后。
	ms := []any{
		map[string]any{"role": "user", "content": "hi"},
		map[string]any{"role": "assistant", "tool_calls": []any{call("c1"), call("c2")}},
		map[string]any{"role": "developer", "content": "image_resize_notice"}, // 插在首结果之前
		map[string]any{"role": "tool", "tool_call_id": "c1", "content": "r1"},
		map[string]any{"role": "user", "content": "intruder"}, // 插在两结果之间
		map[string]any{"role": "tool", "tool_call_id": "c2", "content": "r2"},
		map[string]any{"role": "assistant", "content": "done"},
	}
	out, changed := RepackToolResultBlocks(ms)
	if !changed {
		t.Fatal("应发生重排")
	}
	// 期望顺序：user, assistant(calls), tool c1, tool c2, developer, user intruder, assistant done
	wantRoles := []string{"user", "assistant", "tool", "tool", "developer", "user", "assistant"}
	if len(out) != len(wantRoles) {
		t.Fatalf("len=%d want %d", len(out), len(wantRoles))
	}
	for i, role := range wantRoles {
		if got, _ := out[i].(map[string]any)["role"].(string); got != role {
			t.Errorf("order[%d]=%s want %s", i, got, role)
		}
	}
	if id, _ := out[2].(map[string]any)["tool_call_id"].(string); id != "c1" {
		t.Errorf("results[0].tool_call_id=%s want c1", id)
	}
	if id, _ := out[3].(map[string]any)["tool_call_id"].(string); id != "c2" {
		t.Errorf("results[1].tool_call_id=%s want c2", id)
	}

	// 无插队 → 不改动。
	ms2 := []any{
		map[string]any{"role": "assistant", "tool_calls": []any{call("c1")}},
		map[string]any{"role": "tool", "tool_call_id": "c1", "content": "r"},
	}
	if _, changed2 := RepackToolResultBlocks(ms2); changed2 {
		t.Error("无插队不应改动")
	}
}

// --- 端到端：PrepareChatBody 接入全部新归一化 ---

func TestPrepareChatBodyPipelineAdditions(t *testing.T) {
	in := []byte(`{"model":"m","stop":"END","tool_choice":"none",
	  "tools":[{"name":"noop","parameters":{}}],
	  "messages":[
	    {"role":"system","content":"You are an AI agent powered by DeepSeek Harness."},
	    {"role":"assistant","content":"","tool_calls":[{"id":"c1","function":{"name":"noop","arguments":{"q":1}}}]},
	    {"role":"assistant","content":"","tool_calls":[{"id":"c2","function":{"name":"noop","arguments":null}}]},
	    {"role":"user","content":"note"},
	    {"role":"tool","tool_call_id":"c1","content":"r1"},
	    {"role":"tool","tool_call_id":"c2","content":"r2"}]}`)
	obj := parseObj(t, PrepareChatBody(Credentials{}, in, ""))
	if obj["tool_choice"] != "none" {
		t.Errorf("tool_choice=%v want none", obj["tool_choice"])
	}
	if _, has := obj["tools"]; !has {
		t.Error("tools 不应被删")
	}
	stop, _ := obj["stop"].([]any)
	if len(stop) != 1 || stop[0] != "END" {
		t.Errorf("stop=%v want [END]", obj["stop"])
	}
	msgs := obj["messages"].([]any)
	// 首条 system 身份句被中性句替换。
	sys, _ := msgs[0].(map[string]any)["content"].(string)
	if strings.Contains(sys, "powered by") {
		t.Errorf("身份句残留: %q", sys)
	}
	// 拆条合并 + 插队归批后：system, assistant(2 calls), tool, tool, user。
	if len(msgs) != 5 {
		t.Fatalf("messages=%d want 5: %#v", len(msgs), msgs)
	}
	merged := msgs[1].(map[string]any)
	if tcs := merged["tool_calls"].([]any); len(tcs) != 2 {
		t.Errorf("合并后 tool_calls=%d want 2", len(tcs))
	}
	// arguments 已序列化为字符串。
	args, _ := merged["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)["arguments"].(string)
	if args != `{"q":1}` {
		t.Errorf("arguments=%q want {\"q\":1}", args)
	}
}

// --- 模型级 5xx 不熔断（hub #499） ---

func TestModelScopedServerErrorClassification(t *testing.T) {
	for _, code := range []string{"11102", "11129", "11134", "11150", "11158"} {
		body := `{"code":` + code + `,"msg":"x"}`
		if !IsModelScopedServerError(500, body) {
			t.Errorf("500 + code %s 应判模型级", code)
		}
		if got := ClassifyError(500, []byte(body)); got != KindModelBlocked {
			t.Errorf("500 + code %s kind=%s want %s", code, got, KindModelBlocked)
		}
	}
	// 普通 500 仍按账号故障语义（KindServer）。
	if got := ClassifyError(500, []byte(`{"code":0,"msg":"boom"}`)); got != KindServer {
		t.Errorf("普通 500 kind=%s want %s", got, KindServer)
	}
	// 4xx 不受该规则影响（11129 的 400 仍是请求级错误，归 KindClient 由通用链
	// 透传不罚号；11129 的请求体预防见 normalizeTools）。
	if got := ClassifyError(400, []byte(`{"code":11129,"msg":"invalid function call parameters"}`)); got != KindClient {
		t.Errorf("400+11129 kind=%s want %s", got, KindClient)
	}
	// 空体 500 不误判。
	if IsModelScopedServerError(500, "") {
		t.Error("空体 500 不应判模型级")
	}
}
