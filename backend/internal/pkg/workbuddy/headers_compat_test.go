//go:build unit

package workbuddy

// headers_compat_test.go hub 全面对照移植（2026-10-29）批次 2 的回归测试：
// X-Product 官方值 / X-Private-Data / 桌面 chat 兼容档（WB_DESKTOP_CHAT_COMPAT）
// 的 SDK profile（去浏览器头 + x-stainless-* + B3 parent）。

import (
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestAttributionOfficialProductAndPrivateData(t *testing.T) {
	req, _ := http.NewRequest(http.MethodPost, "https://example.com", nil)
	injectAttribution(req, Credentials{})
	if got := req.Header.Get("X-Product"); got != "SaaS" {
		t.Errorf("X-Product=%q want SaaS（官方值）", got)
	}
	if got := req.Header.Get("X-Private-Data"); got != "true" {
		t.Errorf("X-Private-Data=%q want true（官方桌面端默认）", got)
	}
	// WB_PRIVATE_DATA 显式覆盖。
	t.Setenv("WB_PRIVATE_DATA", "false")
	req2, _ := http.NewRequest(http.MethodPost, "https://example.com", nil)
	injectAttribution(req2, Credentials{})
	if got := req2.Header.Get("X-Private-Data"); got != "false" {
		t.Errorf("X-Private-Data=%q want false（显式覆盖）", got)
	}
}

func TestDesktopChatCompatSDKProfile(t *testing.T) {
	t.Setenv("WB_DESKTOP_CHAT_COMPAT", "1")
	req, _ := http.NewRequest(http.MethodPost, "https://example.com", nil)
	ChatHeaders(req, Credentials{UID: "u1", AccessToken: "tok"}, ChatMeta{})

	// 浏览器系请求头在兼容档下不发。
	for _, name := range []string{"Origin", "Referer", "Accept-Language", "X-Requested-With", "X-CodeBuddy-Request", "X-Machine-ID", "X-Session-ID"} {
		if req.Header.Get(name) != "" {
			t.Errorf("兼容档不应发 %s", name)
		}
	}
	// SDK 指纹族存在（值不钉死——宿主相关）。
	for _, name := range []string{
		"x-stainless-lang", "x-stainless-package-version", "x-stainless-os",
		"x-stainless-arch", "x-stainless-runtime", "x-stainless-runtime-version",
		"x-stainless-retry-count",
	} {
		if _, has := req.Header[name]; !has {
			t.Errorf("兼容档缺少 %s", name)
		}
	}
	if _, has := req.Header["x-stainless-timeout"]; has {
		t.Error("兼容档不应带 x-stainless-timeout（桌面预算未核实）")
	}
	// B3 parent 族：四段 b3 与 traceparent 一致。
	b3 := firstHeader(req.Header["b3"]) // 直写小写键，Get 规范化查不到
	parts := strings.Split(b3, "-")
	if len(parts) != 4 {
		t.Fatalf("b3=%q want 四段", b3)
	}
	if req.Header.Get("X-B3-TraceId") != parts[0] || req.Header.Get("X-B3-SpanId") != parts[1] ||
		req.Header.Get("X-B3-ParentSpanId") != parts[3] || parts[2] != "1" {
		t.Errorf("b3 与 X-B3-* 不同源: b3=%q trace=%q span=%q parent=%q", b3,
			req.Header.Get("X-B3-TraceId"), req.Header.Get("X-B3-SpanId"), req.Header.Get("X-B3-ParentSpanId"))
	}
	if tp := firstHeader(req.Header["traceparent"]); tp != "00-"+parts[0]+"-"+parts[1]+"-01" {
		t.Errorf("traceparent=%q 与 b3 不一致", tp)
	}
	// 会话头族与鉴权保持。
	if req.Header.Get("X-Conversation-ID") == "" || req.Header.Get("Authorization") == "" {
		t.Error("兼容档不应影响会话头族与鉴权")
	}
	if got := req.Header.Get("Accept"); got != "application/json" {
		t.Errorf("兼容档 Accept=%q want application/json", got)
	}
}

func firstHeader(v []string) string {
	if len(v) == 0 {
		return ""
	}
	return v[0]
}

func TestDesktopChatCompatDefaultOff(t *testing.T) {
	os.Unsetenv("WB_DESKTOP_CHAT_COMPAT")
	req, _ := http.NewRequest(http.MethodPost, "https://example.com", nil)
	ChatHeaders(req, Credentials{UID: "u1", AccessToken: "tok"}, ChatMeta{})
	// 默认关闭：浏览器系头照发（旧口径不变）。
	for _, name := range []string{"Origin", "X-CodeBuddy-Request", "X-Machine-ID"} {
		if req.Header.Get(name) == "" {
			t.Errorf("默认应发 %s", name)
		}
	}
	if _, has := req.Header["x-stainless-lang"]; has {
		t.Error("默认不应发 x-stainless-*")
	}
	if req.Header.Get("X-B3-ParentSpanId") != "" {
		t.Error("默认不应发 X-B3-ParentSpanId")
	}
}
