//go:build unit

package qoder

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

// 黄金向量由源项目 qoder2api-hub 的 qoder_sign.py / qoder_fingerprint.py
// 对固定输入生成（脚本临时生成后删除）：
//   temp_key  = "0123456789abcdef"（16 hex 字符，形态对齐 uuid4().hex[:16]）
//   uid       = "u-test-1234"，date = "1760000000"
//   RSA 填充  = 确定性非零序列（(i%255)+1，与 Go 测试端 detReader 一致）
//   requestId = "0f0e0d0c-0b0a-4909-8807-654321098765"

const (
	goldUID         = "u-test-1234"
	goldTempKey     = "0123456789abcdef"
	goldDate        = "1760000000"
	goldRequestID   = "0f0e0d0c-0b0a-4909-8807-654321098765"
	goldNickname    = "测试昵称<script>&\"'\\/\u2028x"
	goldOrgName     = "组织<>&\"'"
	goldBody        = "$kwm" // qoder_encode("{}")
	goldPath        = "/api/v2/model/list"
	goldIdentityHex = "7b22616964223a22752d746573742d31323334222c226e616d65223a22e6b58be8af95e698b5e7a7b03c7363726970743e265c22275c5c2fe280a878222c226f7267616e697a6174696f6e5f6964223a226f72672d31222c226f7267616e697a6174696f6e5f6e616d65223a22e7bb84e7bb873c3e265c2227222c22726566726573685f746f6b656e223a226472742d646566343536222c2273656375726974795f6f617574685f746f6b656e223a2264742d616263313233222c22756964223a22752d746573742d31323334222c22757365725f74797065223a22706572736f6e616c5f70726f66657373696f6e616c5f747269616c222c2279785f756964223a22227d"
	goldCosyKey     = "Cj3wcD3HVy3YStP9asK+maU0xaeJ+0i+AJFvy6i+Q39VgnuJe0GnqJU0Mayi+XpFWCYzM7RTCBIfB18c9rKDD+PkJln5VHof/k7KJ5ymBAsCN+AfDFAUkDFH7MczPDfqEftng53b7KbSMYcNi1mkhTNNEQq8e5YtOwHN2vPviQE="
	goldInfo        = "S5OoyGbJK6/B7+UiTPbcZX49e4IPCTsoRVtWIyiKJSfaPjj5a8s2C884/6DOUSB57FglmGl37rML8zlJ+Gwtl2o9/iHWGH0KOPev0LIue1ylws1G3iy3AAt9/t8ZgMZB6I3JvQc8uRWsGg1AQcchWVRlLWwbDlAzq05zxZOXdZc1OMCnsqcYYFw3ir1KELb3ud2ydx6UAwerVAwUD9TsjzAhNrv64q+jruIhFt2ByH8qLCoLCjWwP6S88dO2MdJEBP2/aN7YHcmchgcYb5mL3h261CC0boSQ7vBKEHNBkTyhbqqKj1ibAV2ckAmkCkF4+IQvQMJwZtC3oICaDZ55llpV6mjcIqchRdf0PW1UIMM="
	goldPayloadB64  = "eyJjb3N5VmVyc2lvbiI6IjEuMS42NCIsImlkZVZlcnNpb24iOiIiLCJpbmZvIjoiUzVPb3lHYkpLNi9CNytVaVRQYmNaWDQ5ZTRJUENUc29SVnRXSXlpS0pTZmFQamo1YThzMkM4ODQvNkRPVVNCNTdGZ2xtR2wzN3JNTDh6bEorR3d0bDJvOS9pSFdHSDBLT1BldjBMSXVlMXlsd3MxRzNpeTNBQXQ5L3Q4WmdNWkI2STNKdlFjOHVSV3NHZzFBUWNjaFdWUmxMV3diRGxBenEwNXp4Wk9YZFpjMU9NQ25zcWNZWUZ3M2lyMUtFTGIzdWQyeWR4NlVBd2VyVkF3VUQ5VHNqekFoTnJ2NjRxK2pydUloRnQyQnlIOHFMQ29MQ2pXd1A2Uzg4ZE8yTWRKRUJQMi9hTjdZSGNtY2hnY1liNW1MM2gyNjFDQzBib1NRN3ZCS0VITkJrVHloYnFxS2oxaWJBVjJja0Fta0NrRjQrSVF2UU1Kd1p0QzNvSUNhRFo1NWxscFY2bWpjSXFjaFJkZjBQVzFVSU1NPSIsInJlcXVlc3RJZCI6IjBmMGUwZDBjLTBiMGEtNDkwOS04ODA3LTY1NDMyMTA5ODc2NSIsInZlcnNpb24iOiJ2MSJ9"
	goldSigMD5      = "44844fdb942f1c577746b7d413ce838d"
	goldRSANHex     = "c0f22307e5cd362e296bb04470f6de8fbf935ce24e8fcf511a0e2701329769c4a76e499bb938036a52af1eaf818cf79a2600620e3ce87e371d2ca6d85803606a1b3fa5e874643c9ed2db7e85673ef7227fca56e2e7c08f0927609bb896a9f24be1782099a66016a5bfdc3f1ff756bfc9e88d7b5dc5be30bf45a0223a00ebcecf"
)

// detReader 确定性随机源：前 8 字节为 temp key 种子，其后为非零填充模式
// (i%255)+1（与 Python 端 DetRand 一致）。
type detReader struct {
	i   int
	off int
}

func (d *detReader) Read(p []byte) (int, error) {
	n := 0
	for n < len(p) {
		var b byte
		if d.off < 8 {
			b = goldTempKeyBytes()[d.off]
		} else {
			b = byte(d.i%255) + 1
			d.i++
		}
		d.off++
		p[n] = b
		n++
	}
	return n, nil
}

func goldTempKeyBytes() []byte {
	raw, _ := hex.DecodeString("0123456789abcdef")
	return raw
}

func goldIdentity() Identity {
	return Identity{
		UID:              goldUID,
		Nickname:         goldNickname,
		AccessToken:      "dt-abc123",
		RefreshToken:     "drt-def456",
		UserType:         "personal_professional_trial",
		OrganizationID:   "org-1",
		OrganizationName: goldOrgName,
	}
}

func TestServerPubKeyMatchesGolden(t *testing.T) {
	if serverPubKey.E != 65537 {
		t.Fatalf("e = %d, want 65537", serverPubKey.E)
	}
	if serverPubKey.N.BitLen() != 1024 {
		t.Fatalf("modulus bits = %d, want 1024", serverPubKey.N.BitLen())
	}
	if got := serverPubKey.N.Text(16); got != goldRSANHex {
		t.Fatalf("modulus mismatch:\n got %s\nwant %s", got, goldRSANHex)
	}
}

func TestSortedCompactJSONGoldenIdentity(t *testing.T) {
	id := goldIdentity()
	identity := map[string]string{
		"name":                 id.Nickname,
		"aid":                  id.UID,
		"uid":                  id.UID,
		"yx_uid":               "",
		"organization_id":      id.OrganizationID,
		"organization_name":    id.OrganizationName,
		"user_type":            id.UserType,
		"security_oauth_token": id.AccessToken,
		"refresh_token":        id.RefreshToken,
	}
	got := SortedCompactJSON(identity)
	want, err := hex.DecodeString(goldIdentityHex)
	if err != nil {
		t.Fatalf("bad golden hex: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("identity JSON mismatch:\n got hex %x\nwant hex %x", got, want)
	}
}

// TestJSONStringPythonCompat 验证 jsonString 与 Python json.dumps
// ensure_ascii=False 的转义集合一致（含 U+2028 原样、'/' 不转义）。
func TestJSONStringPythonCompat(t *testing.T) {
	// golden 的 escape_probe：控制字符 1..0x21 + "\"/ + U+2028 U+2029 é
	input := ""
	for c := rune(1); c < 0x22; c++ {
		input += string(c)
	}
	input += "\"\\/\u2028\u2029é"
	want := `{"k":"` +
		`\u0001\u0002\u0003\u0004\u0005\u0006\u0007\b\t\n\u000b\f\r\u000e\u000f` +
		`\u0010\u0011\u0012\u0013\u0014\u0015\u0016\u0017\u0018\u0019\u001a\u001b` +
		`\u001c\u001d\u001e\u001f !\"\\/` + "\u2028\u2029é" + `"}`
	got := string(SortedCompactJSON(map[string]string{"k": input}))
	if got != want {
		t.Fatalf("escape mismatch:\n got %q\nwant %q", got, want)
	}
}

func TestNewCosySessionGolden(t *testing.T) {
	orig := randomReader
	randomReader = &detReader{}
	defer func() { randomReader = orig }()

	s, err := NewCosySession(goldIdentity())
	if err != nil {
		t.Fatalf("NewCosySession: %v", err)
	}
	if string(s.tempKey) != goldTempKey {
		t.Fatalf("temp key = %q, want %q", s.tempKey, goldTempKey)
	}
	if s.CosyKey != goldCosyKey {
		t.Fatalf("cosyKey mismatch:\n got %s\nwant %s", s.CosyKey, goldCosyKey)
	}
	if s.Info != goldInfo {
		t.Fatalf("info mismatch:\n got %s\nwant %s", s.Info, goldInfo)
	}
	// 指纹派生形状
	if s.MachineID != "e3fac2ea4adabb7c875c420475b06bd1" {
		t.Fatalf("machine id = %s", s.MachineID)
	}
	if s.MachineType != "a522ea2c0d7b4a2601" {
		t.Fatalf("machine type = %s", s.MachineType)
	}
	if s.MachineToken != "O2hPgJNw30x4jQ99Kb5_EdYW76MQKF5Ns5FXTr2mtNt" {
		t.Fatalf("machine token = %s", s.MachineToken)
	}

	payloadB64, bearer := s.Sign(goldBody, goldPath, goldDate, goldRequestID)
	if payloadB64 != goldPayloadB64 {
		t.Fatalf("payload_b64 mismatch:\n got %s\nwant %s", payloadB64, goldPayloadB64)
	}
	wantBearer := "Bearer COSY." + goldPayloadB64 + "." + goldSigMD5
	if bearer != wantBearer {
		t.Fatalf("bearer mismatch:\n got %s\nwant %s", bearer, wantBearer)
	}
}

func TestCosyPathStripsAlgoPrefix(t *testing.T) {
	cases := map[string]string{
		"https://gateway.qoder.com.cn" + ChatPath: "/api/v2/service/pro/sse/agent_chat_generation",
		"https://api1.qoder.sh" + ModelsPath:      "/api/v2/model/list",
		"https://api1.qoder.sh/api/v1/other":      "/api/v1/other",
	}
	for raw, want := range cases {
		if got := cosyPath(raw); got != want {
			t.Fatalf("cosyPath(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestCosySessionHeaders(t *testing.T) {
	s, err := NewCosySession(goldIdentity())
	if err != nil {
		t.Fatalf("NewCosySession: %v", err)
	}
	h := s.Headers(goldBody, "https://api1.qoder.sh"+ModelsPath, "qfmodel", true, "")
	checks := map[string]string{
		"cosy-data-policy":  "AGREE",
		"content-type":      "application/json",
		"cosy-clienttype":   "5",
		"cosy-user":         goldUID,
		"cosy-key":          s.CosyKey,
		"accept":            "text/event-stream",
		"cosy-clientip":     "169.254.198.161",
		"accept-encoding":   "identity",
		"cosy-version":      "1.1.64",
		"cosy-machineid":    s.MachineID,
		"cosy-machinetoken": s.MachineToken,
		"cosy-machinetype":  s.MachineType,
		"login-version":     "v2",
		"user-agent":        "Go-http-client/2.0",
		"cache-control":     "no-cache",
		"x-model-key":       "qfmodel",
		"x-model-source":    "system",
	}
	for k, want := range checks {
		if got := h.Get(k); got != want {
			t.Fatalf("header %s = %q, want %q", k, got, want)
		}
	}
	if auth := h.Get("authorization"); !strings.HasPrefix(auth, "Bearer COSY.") {
		t.Fatalf("authorization = %q", auth)
	}
	if h.Get("cosy-date") == "" {
		t.Fatal("cosy-date empty")
	}
	// 非 SSE + 无 model key
	h2 := s.Headers(goldBody, "https://api1.qoder.sh"+ModelsPath, "", false, "application/json")
	if h2.Get("cache-control") != "" {
		t.Fatal("non-sse request must not carry cache-control")
	}
	if h2.Get("x-model-key") != "" || h2.Get("x-model-source") != "" {
		t.Fatal("model headers must be omitted without model key")
	}
	if h2.Get("accept") != "application/json" {
		t.Fatalf("accept = %q", h2.Get("accept"))
	}
}

func TestCosySessionCacheTokenRotation(t *testing.T) {
	cache := NewCosySessionCache()
	id1 := goldIdentity()
	s1, err := cache.Get(id1)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	s1Again, err := cache.Get(id1)
	if err != nil {
		t.Fatalf("get again: %v", err)
	}
	if s1 != s1Again {
		t.Fatal("same token must reuse cached session")
	}
	id2 := goldIdentity()
	id2.AccessToken = "dt-rotated"
	s2, err := cache.Get(id2)
	if err != nil {
		t.Fatalf("get rotated: %v", err)
	}
	if s2 == s1 {
		t.Fatal("token rotation must rebuild session")
	}
	if s2.AccessToken != "dt-rotated" {
		t.Fatalf("rotated session token = %q", s2.AccessToken)
	}
	cache.Invalidate(goldUID)
	s3, err := cache.Get(id2)
	if err != nil {
		t.Fatalf("get after invalidate: %v", err)
	}
	if s3 == s2 {
		t.Fatal("invalidate must drop the cached session")
	}
	// 空 token 拒绝
	if _, err := cache.Get(Identity{UID: "x"}); err == nil {
		t.Fatal("empty access token must error")
	}
}

func TestRSAPaddingIsRandomized(t *testing.T) {
	// 两个不同随机源包装同一 temp key 应产生不同密文（PKCS#1 v1.5 随机填充；
	// em 结构由黄金向量测试覆盖，密文本身不可从外部检查 em 形态）
	a, err := rsaWrapTempKey(newDetRand(1), []byte(goldTempKey))
	if err != nil {
		t.Fatal(err)
	}
	b, err := rsaWrapTempKey(newDetRand(2), []byte(goldTempKey))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a, b) {
		t.Fatal("rsa padding must be randomized")
	}
	if len(a) != 128 {
		t.Fatalf("ciphertext len = %d (1024-bit key)", len(a))
	}
}

type offsetDetReader struct{ seed int }

func newDetRand(seed int) io.Reader { return &offsetDetReader{seed: seed} }

func (d *offsetDetReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = byte((d.seed)%254) + 1
		d.seed++
	}
	return len(p), nil
}

func TestUUID4Shapes(t *testing.T) {
	u := UUID4()
	if len(u) != 36 || u[8] != '-' || u[13] != '-' || u[14] != '4' || u[18] != '-' || u[23] != '-' {
		t.Fatalf("uuid4 shape invalid: %q", u)
	}
	h := UUID4Hex()
	if len(h) != 32 || strings.ContainsAny(h, "-") {
		t.Fatalf("uuid4 hex shape invalid: %q", h)
	}
}

func TestBearerBase64RoundTrip(t *testing.T) {
	s, err := NewCosySession(goldIdentity())
	if err != nil {
		t.Fatal(err)
	}
	payloadB64, _, _ := s.Bearer(goldBody, "https://api1.qoder.sh"+ModelsPath)
	raw, err := base64.StdEncoding.DecodeString(payloadB64)
	if err != nil {
		t.Fatalf("payload_b64 not valid base64: %v", err)
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("payload JSON: %v", err)
	}
	if m["cosyVersion"] != "1.1.64" || m["version"] != "v1" || m["ideVersion"] != "" {
		t.Fatalf("payload fields: %v", m)
	}
	if m["info"] != s.Info {
		t.Fatal("payload info mismatch")
	}
}
