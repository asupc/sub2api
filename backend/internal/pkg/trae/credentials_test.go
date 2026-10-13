//go:build unit

package trae

import (
	"regexp"
	"testing"
)

func TestCredentialsRoundTrip(t *testing.T) {
	m := map[string]string{
		"jwt":           "raw-jwt",
		"refresh_token": "refresh-1",
		"expires_at":    "1770000000",
		"variant":       "solo_cn",
		"uid":           "u-1",
		"device_id":     "dev-1",
		"machine_id":    "mach-1",
	}
	creds, err := ParseCredentials(m)
	if err != nil {
		t.Fatal(err)
	}
	if creds.JWT != "raw-jwt" || creds.RefreshToken != "refresh-1" ||
		creds.ExpiresAt != "1770000000" || creds.UID != "u-1" ||
		creds.DeviceID != "dev-1" || creds.MachineID != "mach-1" {
		t.Errorf("creds = %+v", creds)
	}
	if creds.VariantOf() != VariantSoloCN {
		t.Errorf("variant = %s", creds.VariantOf())
	}
	// ToMap 全字段往返（值均为字符串）。
	roundTrip := creds.ToMap()
	for key, want := range m {
		if roundTrip[key] != want {
			t.Errorf("roundTrip[%s] = %q, want %q", key, roundTrip[key], want)
		}
	}
	if len(roundTrip) != len(m) {
		t.Errorf("字段数 = %d, want %d", len(roundTrip), len(m))
	}
}

func TestParseCredentialsVariantValidation(t *testing.T) {
	if _, err := ParseCredentials(map[string]string{}); err == nil {
		t.Error("缺 variant 应报错")
	}
	if _, err := ParseCredentials(map[string]string{"variant": "doubao"}); err == nil {
		t.Error("未知 variant 应报错")
	}
	// 四个契约值 + 源别名都应通过。
	for _, v := range []string{"trae_cn", "solo_cn", "trae_global", "solo_global", "trae_work", "global"} {
		if _, err := ParseCredentials(map[string]string{"variant": v}); err != nil {
			t.Errorf("variant=%s 不应报错: %v", v, err)
		}
	}
}

func TestEnsureIdentityDerivation(t *testing.T) {
	// UID 从 JWT 解析；machine_id / device_id 从 UID 稳定派生。
	creds, err := ParseCredentials(map[string]string{"variant": "trae_cn", "jwt": makeJWT(t, "777", 3600)})
	if err != nil {
		t.Fatal(err)
	}
	creds.EnsureIdentity()
	if creds.UID != "777" {
		t.Errorf("UID = %q, want 777（从 JWT 解析）", creds.UID)
	}
	uuidPattern := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	if !uuidPattern.MatchString(creds.MachineID) {
		t.Errorf("MachineID 应为 UUID 形态: %q", creds.MachineID)
	}
	if !regexp.MustCompile(`^[0-9]+$`).MatchString(creds.DeviceID) {
		t.Errorf("DeviceID 应为数字形态: %q", creds.DeviceID)
	}

	// 稳定性：同一 UID 重复派生结果一致；不同 UID 结果不同。
	again, _ := ParseCredentials(map[string]string{"variant": "trae_cn", "uid": "777"})
	again.EnsureIdentity()
	if again.MachineID != creds.MachineID || again.DeviceID != creds.DeviceID {
		t.Error("派生必须跨实例稳定")
	}
	other, _ := ParseCredentials(map[string]string{"variant": "trae_cn", "uid": "778"})
	other.EnsureIdentity()
	if other.MachineID == creds.MachineID || other.DeviceID == creds.DeviceID {
		t.Error("不同 UID 的派生必须不同")
	}

	// 已有值不被覆盖。
	kept, _ := ParseCredentials(map[string]string{
		"variant": "trae_cn", "uid": "u", "machine_id": "m", "device_id": "d",
	})
	kept.EnsureIdentity()
	if kept.MachineID != "m" || kept.DeviceID != "d" {
		t.Error("已有标识不应被覆盖")
	}
}
