//go:build unit

package workbuddy

import (
	"encoding/base64"
	"testing"
	"time"
)

// TestResolveRealm realm 归一化表驱动（显式 realm > domain 后缀 > cn）。
func TestResolveRealm(t *testing.T) {
	cases := []struct {
		name     string
		explicit string
		domain   string
		want     string
	}{
		{"显式 global", "global", "", RealmGlobal},
		{"显式 cn", "cn", "www.workbuddy.ai", RealmCN}, // 显式优先于 domain 回落
		{"显式带空白", " global ", "", RealmGlobal},
		{"domain 裸域", "", "workbuddy.ai", RealmGlobal},
		{"domain 子域", "", "www.workbuddy.ai", RealmGlobal},
		{"domain 大写", "", "WWW.WorkBuddy.AI", RealmGlobal},
		{"domain CN 域", "", "copilot.tencent.com", RealmCN},
		{"全空回落 cn", "", "", RealmCN},
		{"相似后缀不算", "", "notworkbuddy.ai", RealmCN},
		{"未知显式值透传", "eu", "", "eu"},
	}
	for _, c := range cases {
		if got := ResolveRealm(c.explicit, c.domain); got != c.want {
			t.Errorf("%s: ResolveRealm(%q,%q)=%q want %q", c.name, c.explicit, c.domain, got, c.want)
		}
	}
}

// TestCredentialsRoundTrip ParseCredentials / ToMap 互转（含 expires_at 数字字符串兼容）。
func TestCredentialsRoundTrip(t *testing.T) {
	src := map[string]string{
		"access_token":   "at-123",
		"refresh_token":  "rt-456",
		"expires_at":     "1767225600", // 十进制字符串（sub2api 标准形态）
		"realm":          "global",
		"uid":            "uid-abc",
		"enterprise_id":  "ent-1",
		"domain":         "www.workbuddy.ai",
		"nickname":       "测试号",
		"device_token":   "dt-9",
		"client_version": "9.9.9",
		"cli_version":    "3.0.0",
		"user_agent":     "Custom/1.0",
	}
	c, err := ParseCredentials(src)
	if err != nil {
		t.Fatalf("ParseCredentials: %v", err)
	}
	if c.ExpiresAt != 1767225600 {
		t.Errorf("ExpiresAt=%d want 1767225600", c.ExpiresAt)
	}
	if !c.IsGlobal() || !c.IsEnterprise() {
		t.Errorf("IsGlobal=%v IsEnterprise=%v want true/true", c.IsGlobal(), c.IsEnterprise())
	}
	out := c.ToMap()
	for _, k := range []string{"access_token", "refresh_token", "expires_at", "realm", "uid", "domain",
		"enterprise_id", "nickname", "device_token", "client_version", "cli_version", "user_agent"} {
		if out[k] == "" {
			t.Errorf("ToMap missing key %q", k)
		}
	}
	if out["expires_at"] != "1767225600" {
		t.Errorf("expires_at 序列化为 %q want 十进制字符串", out["expires_at"])
	}
	// 再解析一次应等值（幂等往返）。
	c2, err := ParseCredentials(out)
	if err != nil {
		t.Fatalf("re-ParseCredentials: %v", err)
	}
	if c2 != c {
		t.Errorf("round trip mismatch: %+v vs %+v", c2, c)
	}
}

// TestParseCredentialsEdgeCases 空 token / RFC3339 兼容 / 空可选字段不落键。
func TestParseCredentialsEdgeCases(t *testing.T) {
	if _, err := ParseCredentials(map[string]string{"uid": "u"}); err == nil {
		t.Error("缺 access_token 应报错")
	}
	// RFC3339 形态兜底解析。
	c, err := ParseCredentials(map[string]string{
		"access_token": "at",
		"expires_at":   "2027-01-01T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("ParseCredentials: %v", err)
	}
	if want := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC).Unix(); c.ExpiresAt != want {
		t.Errorf("RFC3339 expires_at=%d want %d", c.ExpiresAt, want)
	}
	// 空可选字段不写入 map。
	m := Credentials{AccessToken: "at", UID: "u", ExpiresAt: 100}.ToMap()
	for _, k := range []string{"enterprise_id", "nickname", "device_token", "client_version", "cli_version", "user_agent"} {
		if _, ok := m[k]; ok {
			t.Errorf("空字段 %q 不应写入 ToMap", k)
		}
	}
	// realm 归一化写入。
	if m["realm"] != RealmCN {
		t.Errorf("ToMap realm=%q want cn", m["realm"])
	}
}

// TestNeedsRefresh 过期判定。
func TestNeedsRefresh(t *testing.T) {
	now := time.Unix(1000000, 0)
	c := Credentials{ExpiresAt: now.Add(10 * time.Minute).Unix()}
	if c.NeedsRefresh(5*time.Minute, now) {
		t.Error("10 分钟后过期，5 分钟窗口内不应刷新")
	}
	if !c.NeedsRefresh(30*time.Minute, now) {
		t.Error("10 分钟后过期，30 分钟窗口应刷新")
	}
	if !(Credentials{}).NeedsRefresh(time.Minute, now) {
		t.Error("无 expiry 恒需刷新")
	}
}

// TestIsEnterpriseSession 企业切换会话识别（移植自 workbuddy2api-panel 9282779）：
// JWT payload 的 token_source=enterprise_switch 判定，与 IsEnterprise（计费口径）隔离。
func TestIsEnterpriseSession(t *testing.T) {
	mkToken := func(claims string) string {
		payload := base64.RawURLEncoding.EncodeToString([]byte(claims))
		return "hdr." + payload + ".sig"
	}
	cases := []struct {
		name string
		cred Credentials
		want bool
	}{
		{"enterprise_switch", Credentials{AccessToken: mkToken(`{"token_source":"enterprise_switch","uid":"u1"}`)}, true},
		{"normal token", Credentials{AccessToken: mkToken(`{"token_source":"password","uid":"u1"}`)}, false},
		{"no token_source", Credentials{AccessToken: mkToken(`{"uid":"u1"}`)}, false},
		{"not a jwt", Credentials{AccessToken: "opaque-token"}, false},
		{"empty", Credentials{}, false},
		{"malformed payload", Credentials{AccessToken: "hdr.!!!.sig"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.cred.IsEnterpriseSession(); got != c.want {
				t.Errorf("IsEnterpriseSession()=%v want %v", got, c.want)
			}
		})
	}
}
