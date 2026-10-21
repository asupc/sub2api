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
		{DeriveID(uid, "machine"), "e3fac2ea-4ada-4b7c-a75c-420475b06bd1", "derive_id(machine)"},
		{DeriveID(uid, "session"), "3cadbc91-dfaa-434c-a8bd-057c4953aaa8", "derive_id(session)"},
		{DeriveID(uid, "machinecode"), "e5e70733-d1f2-44ae-a227-01bce309e443", "derive_id(machinecode)"},
		{DeriveMachineType(uid), "a522ea2c0d7b4a26a1", "derive_machine_type"},
		{DeriveMachineToken(uid), "O2hPgJNw30x4jQ99Kb5_EdYW76MQKF5Ns5FXTr2mtNt", "derive_machine_token"},
		{DeriveID("", "machine"), "fc1f3f60-3c8b-4dc0-a839-42ea0169ab1e", "empty uid -> anonymous"},
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
	uuidShape := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	if !uuidShape.MatchString(DeriveID("x", "machine")) {
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
	if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(rid) {
		t.Fatalf("request id shape: %q", rid)
	}
}
