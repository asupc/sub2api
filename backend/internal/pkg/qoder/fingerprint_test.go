//go:build unit

package qoder

import (
	"regexp"
	"testing"
)

// 黄金向量由源实现 qoder_fingerprint.py 对固定 uid 生成。
func TestDeriveGolden(t *testing.T) {
	uid := "u-test-1234"
	cases := []struct {
		got  string
		want string
		desc string
	}{
		{DeriveID(uid, "machine"), "e3fac2ea4adabb7c875c420475b06bd1", "derive_id(machine)"},
		{DeriveID(uid, "session"), "3cadbc91dfaa734ce8bd057c4953aaa8", "derive_id(session)"},
		{DeriveID(uid, "machinecode"), "e5e70733d1f2d4ae022701bce309e443", "derive_id(machinecode)"},
		{DeriveMachineType(uid), "a522ea2c0d7b4a2601", "derive_machine_type"},
		{DeriveMachineToken(uid), "O2hPgJNw30x4jQ99Kb5_EdYW76MQKF5Ns5FXTr2mtNt", "derive_machine_token"},
		{DeriveID("", "machine"), "fc1f3f603c8bcdc0783942ea0169ab1e", "empty uid -> anonymous"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Fatalf("%s = %q, want %q", c.desc, c.got, c.want)
		}
	}
}

// TestDeriveStability 同一 uid 必须派生出相同值；不同 uid 必须不同。
func TestDeriveStability(t *testing.T) {
	uid := "stable-uid-42"
	if DeriveID(uid, "machine") != DeriveID(uid, "machine") {
		t.Fatal("derive must be deterministic")
	}
	if DeriveMachineToken(uid) != DeriveMachineToken(uid) {
		t.Fatal("machine token must be deterministic")
	}
	if DeriveID(uid, "machine") == DeriveID("other-uid", "machine") {
		t.Fatal("different uids must derive different ids")
	}
	if DeriveID(uid, "machine") == DeriveID(uid, "session") {
		t.Fatal("different salts must derive different ids")
	}
}

// TestDeriveShapes 输出形状（长度/字符集）与源实现一致。
func TestDeriveShapes(t *testing.T) {
	hex32 := regexp.MustCompile(`^[0-9a-f]{32}$`)
	if !hex32.MatchString(DeriveID("x", "machine")) {
		t.Fatalf("derive_id shape: %q", DeriveID("x", "machine"))
	}
	hex18 := regexp.MustCompile(`^[0-9a-f]{18}$`)
	if !hex18.MatchString(DeriveMachineType("x")) {
		t.Fatalf("machine_type shape: %q", DeriveMachineType("x"))
	}
	b64url43 := regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
	if !b64url43.MatchString(DeriveMachineToken("x")) {
		t.Fatalf("machine_token shape: %q", DeriveMachineToken("x"))
	}
	rid := GenerateRequestID("x")
	if !regexp.MustCompile(`^[0-9a-f]{32}-\d{6}$`).MatchString(rid) {
		t.Fatalf("request id shape: %q", rid)
	}
}
