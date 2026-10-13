//go:build unit

package trae

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// makeJWT 构造一个真实形态的 Trae JWT（Cloud-IDE-JWT + 载荷 data.id/exp）。
func makeJWT(t *testing.T, userID string, expOffsetSecs int64) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"data": map[string]any{"id": userID},
		"exp":  time.Now().Unix() + expOffsetSecs,
	})
	if err != nil {
		t.Fatal(err)
	}
	b64 := base64.RawURLEncoding.EncodeToString(payload)
	return "Cloud-IDE-JWT header." + b64 + ".signature"
}

// makeRawJWT 构造不带前缀的三段式 JWT（载荷自定）。
func makeRawJWT(t *testing.T, payload map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	b64 := base64.RawURLEncoding.EncodeToString(raw)
	return "h." + b64 + ".s"
}

func TestParseJWTExtractsUserIDAndExpiry(t *testing.T) {
	jwt := makeJWT(t, "1234567890123456", 48*3600)
	info := ParseJWT(jwt)
	if info.UserID != "1234567890123456" {
		t.Errorf("user_id = %q", info.UserID)
	}
	if !info.ExpValid {
		t.Fatal("exp 未解析到")
	}
	hours, ok := info.ExpHours(time.Now())
	if !ok || hours <= 47.0 || hours > 48.0 {
		t.Errorf("exp_hours = %v(ok=%v), want (47,48]", hours, ok)
	}
	if info.Status(time.Now()) != "ok" {
		t.Errorf("status = %s, want ok", info.Status(time.Now()))
	}
}

func TestParseJWTToleratesPrefixesAndWhitespace(t *testing.T) {
	jwt := makeJWT(t, "999", 10*3600)
	bare := NormalizeJWT(jwt)
	cases := []string{
		jwt,
		"Bearer " + bare,
		"Cloud-IDE-JWT " + bare,
		"  " + bare + "\n",
	}
	for _, c := range cases {
		if got := ParseJWT(c).UserID; got != "999" {
			t.Errorf("ParseJWT(%q).user_id = %q, want 999", c, got)
		}
	}
}

func TestParseJWTFallsBackToAuthIDThenSub(t *testing.T) {
	if got := ParseJWT(makeRawJWT(t, map[string]any{"auth_id": "auth-1"})).UserID; got != "auth-1" {
		t.Errorf("auth_id 回落失败: %q", got)
	}
	if got := ParseJWT(makeRawJWT(t, map[string]any{"sub": "sub-1"})).UserID; got != "sub-1" {
		t.Errorf("sub 回落失败: %q", got)
	}
}

func TestParseJWTNumericUserIDAndStringExp(t *testing.T) {
	jwt := makeRawJWT(t, map[string]any{
		"data": map[string]any{"id": 12345},
		"exp":  fmt.Sprintf("%d", time.Now().Unix()+3600),
	})
	info := ParseJWT(jwt)
	if info.UserID != "12345" {
		t.Errorf("数字 user_id = %q, want 12345", info.UserID)
	}
	if !info.ExpValid {
		t.Error("字符串 exp 未解析到")
	}
}

func TestParseJWTPaddedBase64Payload(t *testing.T) {
	// 某些签发方会补 `=`，必须仍能解析。
	payload, _ := json.Marshal(map[string]any{"data": map[string]any{"id": "42"}})
	padded := base64.URLEncoding.EncodeToString(payload)
	info := ParseJWT("h." + padded + ".s")
	if info.UserID != "42" {
		t.Errorf("user_id = %q, want 42", info.UserID)
	}
}

func TestParseJWTGarbageReturnsUnknown(t *testing.T) {
	for _, bad := range []string{"", "not-a-jwt", "only.two", "a.b.c"} {
		info := ParseJWT(bad)
		if info.UserID != "" || info.ExpValid {
			t.Errorf("ParseJWT(%q) 应为未知, got %+v", bad, info)
		}
		if info.Status(time.Now()) != "unknown" {
			t.Errorf("ParseJWT(%q).status 应为 unknown", bad)
		}
	}
}

func TestJWTStatusThresholds(t *testing.T) {
	now := time.Now()
	cases := []struct {
		offsetSecs int64
		want       string
	}{
		{25 * 3600, "ok"},
		{24 * 3600, "warn"},
		{-60, "expired"},
	}
	for _, tc := range cases {
		info := JWTInfo{ExpTimestamp: now.Unix() + tc.offsetSecs, ExpValid: true}
		if got := info.Status(now); got != tc.want {
			t.Errorf("offset=%d status = %s, want %s", tc.offsetSecs, got, tc.want)
		}
	}
	if (JWTInfo{}).Status(now) != "unknown" {
		t.Error("无 exp 应为 unknown")
	}
}

func TestAuthorizationHeaderNormalizesOnce(t *testing.T) {
	if got := AuthorizationHeader("abc"); got != "Cloud-IDE-JWT abc" {
		t.Errorf("got %q", got)
	}
	if got := AuthorizationHeader("Cloud-IDE-JWT abc"); got != "Cloud-IDE-JWT abc" {
		t.Errorf("got %q", got)
	}
	if got := AuthorizationHeader("  Bearer abc  "); got != "Cloud-IDE-JWT abc" {
		t.Errorf("got %q", got)
	}
	// 不重复加前缀。
	if got := AuthorizationHeader("Cloud-IDE-JWT Cloud-IDE-JWT abc"); got != "Cloud-IDE-JWT Cloud-IDE-JWT abc" {
		t.Errorf("got %q", got)
	}
}
