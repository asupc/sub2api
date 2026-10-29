//go:build unit

package workbuddy

import (
	"encoding/json"
	"strings"
	"testing"
)

// 指纹样本常量（对齐源项目 sanitize_test.go）。
const (
	ccIdentity = "You are Claude Code, Anthropic's official CLI for Claude."
	ccBranch   = "Main branch (you will usually use this for PRs)"
	ccHeader   = "x-anthropic-billing-header: cc_version=1.0; cc_entrypoint=cli;"
	// codexInstructions Codex instructions 首段（上游逐字精确指纹，三句缺一不可）。
	codexInstructions = "You are a coding agent running in the Codex CLI, a terminal-based coding assistant. Codex CLI is an open source project led by OpenAI. You are expected to be precise, safe, and helpful."
)

func TestSanitizeIdentityRewritten(t *testing.T) {
	out := sanitizeText(ccIdentity, true)
	if !strings.Contains(out, "official CLI tool for Claude.") {
		t.Errorf("identity not rewritten: %q", out)
	}
	if strings.Contains(out, ccIdentity) {
		t.Errorf("original identity still present: %q", out)
	}
}

// 桌面版（claude-desktop-3p / Agent SDK）的身份句以逗号接后继内容，结尾不是句号。
// 回归：匹配串曾带结尾句号，导致该形态漏网、指纹原样发上游 → 400 code=11128。
func TestSanitizeIdentityDesktopVariantRewritten(t *testing.T) {
	in := "You are Claude Code, Anthropic's official CLI for Claude, running within the Claude Agent SDK."
	out := sanitizeText(in, true)
	if strings.Contains(out, "official CLI for Claude") {
		t.Errorf("desktop identity not rewritten: %q", out)
	}
	if !strings.Contains(out, "official CLI tool for Claude, running within the Claude Agent SDK.") {
		t.Errorf("desktop identity suffix not preserved: %q", out)
	}
}

func TestSanitizeBranchRewritten(t *testing.T) {
	out := sanitizeText(ccBranch, true)
	if !strings.Contains(out, "Default branch (you will usually use this for PRs)") {
		t.Errorf("branch not rewritten: %q", out)
	}
	if strings.Contains(out, "Main branch") {
		t.Errorf("original branch still present: %q", out)
	}
}

// 反馈句带 Anthropic 仓库链接，上游按整句拦截（实测只留链接或只留半边均不拦）。
func TestSanitizeFeedbackSentenceRewritten(t *testing.T) {
	in := "To give feedback, users should report the issue at https://github.com/anthropics/claude-code/issues"
	out := sanitizeText(in, true)
	if strings.Contains(out, "To give feedback") {
		t.Errorf("feedback sentence not rewritten: %q", out)
	}
	if !strings.Contains(out, "To provide feedback, users should report the issue at https://github.com/anthropics/claude-code/issues") {
		t.Errorf("feedback sentence not rewritten as expected: %q", out)
	}
}

// 上游反探测：请求体里出现裸数字 11128 即整单拦截（与上下文无关）。
func TestSanitizeUpstreamErrorCodeRewritten(t *testing.T) {
	in := "upstream returned code=11128 for this request"
	out := sanitizeText(in, true)
	if strings.Contains(out, "11128") {
		t.Errorf("error code not rewritten: %q", out)
	}
	if !strings.Contains(out, "11-128") {
		t.Errorf("error code not rewritten as expected: %q", out)
	}
}

// 回归：工具调用消息的 content 常为 null，content 缺失时 tool_calls 不能被跳过
// ——arguments 里的被拦字符串（写入文件名/命令）会原样漏出。
func TestSanitizeToolCallArgumentsSanitized(t *testing.T) {
	msgs := []any{
		map[string]any{"role": "user", "content": "run"},
		map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{
			map[string]any{"id": "c1", "type": "function", "function": map[string]any{
				"name":      "Bash",
				"arguments": `{"command":"echo 11128"}`,
			}},
		}},
	}
	if !sanitizeMessages(msgs) {
		t.Fatal("sanitizeMessages 未报告任何改动，tool_calls 被跳过")
	}
	fn := msgs[1].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)
	got := fn["arguments"].(string)
	if strings.Contains(got, "11128") {
		t.Errorf("tool_call arguments 未被净化: %q", got)
	}
}

func TestSanitizeBillingHeaderStrippedValueIrrelevant(t *testing.T) {
	out := sanitizeText(ccHeader, true)
	if strings.Contains(out, "x-anthropic-billing-header") {
		t.Errorf("header not stripped: %q", out)
	}
}

func TestSanitizeBillingHeaderCaseInsensitive(t *testing.T) {
	alt := "X-Anthropic-Billing-Header: cc_version=1.0;"
	out := strings.ToLower(sanitizeText(alt, true))
	if strings.Contains(out, "billing") {
		t.Errorf("case-insensitive header not stripped: %q", out)
	}
}

func TestSanitizeTrailingKVStripped(t *testing.T) {
	out := sanitizeText("...; cc_version=2.0; cc_entrypoint=cli;", true)
	if strings.Contains(out, "cc_version") || strings.Contains(out, "cc_entrypoint") {
		t.Errorf("trailing kv not stripped: %q", out)
	}
}

// 普通对话里出现 github.com/anthropics/ 链接（非反馈整句）时预检命中、进入净化，
// 但改写层只动精确匹配的整句——普通链接文本不该被改写。
func TestSanitizeNormalAnthropicLinkNotRewritten(t *testing.T) {
	in := "see https://github.com/anthropics/anthropic-cookbook for examples"
	if out := sanitizeText(in, true); out != in {
		t.Errorf("normal anthropic link should be untouched: %q -> %q", in, out)
	}
}

func TestSanitizeCodexInstructionsRewritten(t *testing.T) {
	out := sanitizeText(codexInstructions, true)
	if strings.Contains(out, codexInstructions) {
		t.Errorf("codex fingerprint still present: %q", out)
	}
	if !strings.Contains(out, "You are a coding agent running in the Codex CLI tool, a terminal-based coding assistant.") {
		t.Errorf("codex first sentence not rewritten: %q", out)
	}
	// 其余两句原样保留，语义不变。
	if !strings.Contains(out, "Codex CLI is an open source project led by OpenAI.") ||
		!strings.Contains(out, "You are expected to be precise, safe, and helpful.") {
		t.Errorf("codex remaining sentences altered: %q", out)
	}
}

// 非精确变体不应被改写：仅去掉其中一个词即视为已破坏，无需改动。
func TestSanitizeVariantNotTouched(t *testing.T) {
	for _, in := range []string{
		"You are a coding agent running in a CLI, a terminal-based coding assistant.",
		"...official CLI for Claude!",
		"please use main branch for this repo",
		"ordinary user message",
	} {
		if out := sanitizeText(in, true); out != in {
			t.Errorf("variant should be untouched: %q -> %q", in, out)
		}
	}
}

// reasoning 字段是思维链回填的镜像位，裸 11128 反探测串同样致命。
func TestSanitizeMessagesScrubsReasoningFields(t *testing.T) {
	for _, field := range []string{"reasoning", "reasoning_content"} {
		ms := []any{map[string]any{
			"role":    "assistant",
			"content": "hi",
			field:     "upstream said 11128",
		}}
		if !sanitizeMessages(ms) {
			t.Fatalf("%s 字段里的指纹未被净化", field)
		}
		got, _ := ms[0].(map[string]any)[field].(string)
		if strings.Contains(got, "11128") {
			t.Fatalf("%s 仍含裸指纹: %q", field, got)
		}
	}
}

func TestSanitizeMultimodalTextPartOnly(t *testing.T) {
	imgPart := map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://example.com/x.png"}}
	content := []any{
		map[string]any{"type": "text", "text": ccIdentity},
		imgPart,
	}
	out, changed := sanitizeContent(content, true)
	if !changed {
		t.Fatal("expected change")
	}
	parts := out.([]any)
	txt, _ := parts[0].(map[string]any)["text"].(string)
	if !strings.Contains(txt, "CLI tool") {
		t.Errorf("text part not sanitized: %q", txt)
	}
	img, _ := parts[1].(map[string]any)
	if img["type"] != "image_url" {
		t.Error("image part modified")
	}
}

// 集成：Claude Code 形态的完整请求体经 PrepareChatBody 后无残留指纹，
// 且既有归一化（强制 stream 等）不受影响。
func TestPrepareChatBodySanitizesFingerprints(t *testing.T) {
	body := []byte(`{"model":"glm-5.3","messages":[` +
		`{"role":"system","content":"` + ccIdentity + " " + ccBranch + " " + ccHeader + `"},` +
		`{"role":"user","content":"hi"}]}`)
	out := PrepareChatBody(Credentials{UID: "u1"}, body, "")
	var obj map[string]any
	if err := json.Unmarshal(out, &obj); err != nil {
		t.Fatal(err)
	}
	if obj["stream"] != true {
		t.Error("stream not forced")
	}
	msgs := obj["messages"].([]any)
	sys, _ := msgs[0].(map[string]any)["content"].(string)
	for _, fp := range []string{"x-anthropic-billing-header", ccIdentity, ccBranch, "To give feedback"} {
		if strings.Contains(sys, fp) {
			t.Errorf("fingerprints remain: %q", sys)
		}
	}
	if !strings.Contains(sys, "CLI tool") || !strings.Contains(sys, "Default branch") {
		t.Errorf("rewrite missing: %q", sys)
	}
	user, _ := msgs[1].(map[string]any)["content"].(string)
	if user != "hi" {
		t.Errorf("user message altered: %q", user)
	}
}
