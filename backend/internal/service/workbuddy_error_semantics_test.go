//go:build unit

package service

// workbuddy_error_semantics_test.go hub 全面对照移植（2026-10-29）批次 3：
// 401 真凭证失效判定（hub #440/#691）。

import (
	"net/http"
	"testing"
)

func TestWorkbuddyRealCredentialFailure(t *testing.T) {
	cases := []struct {
		name   string
		www    string
		body   string
		expect bool
	}{
		{"网关 WWW-Authenticate = 真失效", `Bearer realm="gateway"`, "", true},
		{"JSON 业务体 = 真失效", "", `{"code":12153,"msg":"offline"}`, true},
		{"JSON 数组体 = 真失效", "", `[{"error":1}]`, true},
		{"HTML 代理页 = 非真失效", "", "<!DOCTYPE html><html><body>proxy</body></html>", false},
		{"纯文本 = 非真失效", "", "upstream unavailable", false},
		{"空体 = 非真失效", "", "", false},
	}
	for _, tc := range cases {
		h := http.Header{}
		if tc.www != "" {
			h.Set("Www-Authenticate", tc.www)
		}
		if got := workbuddyRealCredentialFailure(h, tc.body); got != tc.expect {
			t.Errorf("%s: got %v want %v", tc.name, got, tc.expect)
		}
	}
}
