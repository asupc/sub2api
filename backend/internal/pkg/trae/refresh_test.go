//go:build unit

package trae

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/x509"
	"encoding/asn1"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// deviceCreds 生成一套带虚拟设备的测试凭据。
func deviceCreds(t *testing.T, variant Variant, refreshToken string) (*Credentials, DeviceKeyPair) {
	t.Helper()
	pair, err := GenerateDeviceIdentity()
	if err != nil {
		t.Fatal(err)
	}
	return &Credentials{
		Variant:          string(variant),
		RefreshToken:     refreshToken,
		DeviceID:         pair.DeviceID,
		DevicePrivateKey: pair.PrivateKeyPEM,
	}, pair
}

// TestGenerateDeviceIdentityRoundTrip ★ 生成 → PKCS8 编码 → 解析 → 签名验签往返。
func TestGenerateDeviceIdentityRoundTrip(t *testing.T) {
	pair, err := GenerateDeviceIdentity()
	if err != nil {
		t.Fatal(err)
	}
	// 形态断言。
	if len(pair.DeviceID) != 16 {
		t.Errorf("DeviceID 应为 16 位数字: %q", pair.DeviceID)
	}
	for _, c := range pair.DeviceID {
		if c < '0' || c > '9' {
			t.Fatalf("DeviceID 含非数字字符: %q", pair.DeviceID)
		}
	}
	if !strings.Contains(pair.PrivateKeyPEM, "BEGIN PRIVATE KEY") {
		t.Errorf("私钥应为 PKCS#8 PEM: %q", pair.PrivateKeyPEM[:30])
	}
	if !strings.Contains(pair.PublicKeyPEM, "BEGIN PUBLIC KEY") {
		t.Error("公钥应为 SPKI PEM")
	}
	// 私钥可解析回来，且导出的公钥与生成时的公钥一致。
	key, err := parseDevicePrivateKey(pair.PrivateKeyPEM)
	if err != nil {
		t.Fatal(err)
	}
	if key.Curve != elliptic.P256() {
		t.Error("设备私钥必须是 P-256")
	}
	derived, err := DerivePublicKeyPEM(pair.PrivateKeyPEM)
	if err != nil {
		t.Fatal(err)
	}
	if derived != pair.PublicKeyPEM {
		t.Error("从私钥导出的公钥应与生成时的公钥逐字一致")
	}
	// 两次生成互不相同（每台虚拟设备独立）。
	other, _ := GenerateDeviceIdentity()
	if other.DeviceID == pair.DeviceID || other.PrivateKeyPEM == pair.PrivateKeyPEM {
		t.Error("两次生成的设备身份必须不同")
	}
}

// verifyProofSignature 用公钥验签 DeviceProof（测试辅助：还原签名原文并验签）。
func verifyProofSignature(t *testing.T, publicKeyPEM string, proof map[string]any, signPath, clientID, refreshToken string) {
	t.Helper()
	signatureB64, _ := proof["Signature"].(string)
	timestamp := proof["Timestamp"]
	nonce, _ := proof["Nonce"].(string)
	if signatureB64 == "" || nonce == "" {
		t.Fatal("DeviceProof 字段缺失")
	}
	if _, isInt := timestamp.(float64); !isInt {
		t.Fatalf("Timestamp 必须是 JSON int, got %T", timestamp)
	}

	block, _ := pem.Decode([]byte(publicKeyPEM))
	publicKey, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	ecKey, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		t.Fatal("公钥不是 ECDSA")
	}
	raw, err := base64.StdEncoding.DecodeString(signatureB64)
	if err != nil {
		t.Fatal(err)
	}
	// 还原签名原文（与 buildDeviceProof 相同的拼法）。
	msg := fmt.Sprintf("POST\n%s\n%s\n%s\n%d\n%s", signPath, clientID, refreshToken, int64(timestamp.(float64)), nonce)
	digest := sha256.Sum256([]byte(msg))
	switch len(raw) {
	case 64: // P1363: r‖s 各 32 字节
		r := new(big.Int).SetBytes(raw[:32])
		s := new(big.Int).SetBytes(raw[32:])
		if !ecdsa.Verify(ecKey, digest[:], r, s) {
			t.Error("P1363 签名验签失败")
		}
	default: // DER
		var sig struct{ R, S *big.Int }
		if _, err := asn1.Unmarshal(raw, &sig); err != nil {
			t.Fatalf("DER 解析失败: %v", err)
		}
		if !ecdsa.Verify(ecKey, digest[:], sig.R, sig.S) {
			t.Error("DER 签名验签失败")
		}
	}
}

// refreshCapture 记录一次 refresh 请求的观测数据。
type refreshCapture struct {
	path   string
	body   map[string]any
	header http.Header
}

// refreshChainTestCase 一条四步链测试的编排。
type refreshChainTestCase struct {
	name string
	// respond 按请求观测决定响应（返回 (statusCode, body)）。
	respond func(capture refreshCapture, callIndex int) (int, string)
}

// serveRefresh 起 mock refresh 服务器。
func serveRefresh(t *testing.T, respond func(capture refreshCapture, callIndex int) (int, string)) (*httptest.Server, *[]refreshCapture) {
	t.Helper()
	captures := &[]refreshCapture{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		capture := refreshCapture{path: r.URL.Path, body: body, header: r.Header.Clone()}
		*captures = append(*captures, capture)
		status, responseBody := respond(capture, len(*captures)-1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(responseBody))
	}))
	t.Cleanup(server.Close)
	return server, captures
}

const (
	newRefreshPath     = "/trae/api/v3/oauth/ExchangeToken"
	legacyRefreshPath2 = "/cloudide/api/v3/trae/oauth/ExchangeToken"
)

// TestRefreshAccessTokenStepOneSucceeds 步 1（P1363 + DeviceProof）直接成功：
// 端点、体、头、签名全量断言（PlatformCode 写死 IDE_PC，即使是 SOLO 变体）。
func TestRefreshAccessTokenStepOneSucceeds(t *testing.T) {
	server, captures := serveRefresh(t, func(capture refreshCapture, _ int) (int, string) {
		return 200, `{"Result":{"AccessToken":"new-access","RefreshToken":"new-refresh"}}`
	})
	creds, pair := deviceCreds(t, VariantSoloCN, "rt-old")

	result, err := RefreshAccessToken(context.Background(), redirectClient(t, server), VariantSoloCN, creds, ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.AccessToken != "new-access" || result.RefreshToken != "new-refresh" || !result.RefreshRotated {
		t.Errorf("result = %+v", result)
	}
	if got := result.Authorization(); got != "Cloud-IDE-JWT new-access" {
		t.Errorf("Authorization = %q", got)
	}

	if len(*captures) != 1 {
		t.Fatalf("应只发一次请求, got %d", len(*captures))
	}
	capture := (*captures)[0]
	if capture.path != newRefreshPath {
		t.Errorf("步 1 路径 = %s", capture.path)
	}
	// ★ ClientID 按产品线：SOLO 变体用 SOLO 钥匙（与授权/AuthCode 交换三处同源）。
	if capture.body["ClientID"] != "en1oxy7wnw8j9n" {
		t.Errorf("ClientID = %v（SOLO 变体应为 en1oxy7wnw8j9n）", capture.body["ClientID"])
	}
	// ★ PlatformCode 写死 IDE_PC（源码事实，勿按线派生 SOLO_PC）。
	if capture.body["PlatformCode"] != "IDE_PC" {
		t.Errorf("PlatformCode = %v, want IDE_PC", capture.body["PlatformCode"])
	}
	if got := capture.header.Get("x-platform-code"); got != "IDE_PC" {
		t.Errorf("x-platform-code = %q, want IDE_PC", got)
	}
	if capture.body["RefreshToken"] != "rt-old" || capture.body["DeviceID"] != pair.DeviceID {
		t.Errorf("body = %v", capture.body)
	}
	proof, ok := capture.body["DeviceProof"].(map[string]any)
	if !ok {
		t.Fatal("缺 DeviceProof")
	}
	// ★ 签名验签（P1363：64 字节 → base64 恰 88 字符）。
	if len(proof["Signature"].(string)) != 88 {
		t.Errorf("P1363 base64 长度 = %d, want 88", len(proof["Signature"].(string)))
	}
	verifyProofSignature(t, pair.PublicKeyPEM, proof, newRefreshPath, "en1oxy7wnw8j9n", "rt-old")
	// 头：x-cloudide-token 空串、x-device-id 与 DeviceID 同源、x-app-id。
	if got := capture.header.Get("x-cloudide-token"); got != "" {
		t.Errorf("x-cloudide-token = %q, want 空串", got)
	}
	if got := capture.header.Get("x-device-id"); got != pair.DeviceID {
		t.Errorf("x-device-id = %q", got)
	}
	if got := capture.header.Get("x-app-id"); got != "6eefa01c-1036-4c7e-9ca5-d891f63bfcd8" {
		t.Errorf("x-app-id = %q", got)
	}
}

// TestRefreshAccessTokenFallsBackToDer ★ 步 1（P1363）失败 → 步 2（DER）成功。
func TestRefreshAccessTokenFallsBackToDer(t *testing.T) {
	server, captures := serveRefresh(t, func(capture refreshCapture, _ int) (int, string) {
		if signature, ok := capture.body["DeviceProof"].(map[string]any)["Signature"].(string); ok && len(signature) == 88 {
			// 火山信封错误：协议级失败 → 继续探测下一变体。
			return 200, `{"ResponseMetadata":{"Error":{"Code":"20405","Message":"Device proof required","StandardCode":"040037"}}}`
		}
		return 200, `{"Result":{"AccessToken":"access-der"}}`
	})
	creds, _ := deviceCreds(t, VariantTraeCN, "rt-old")

	result, err := RefreshAccessToken(context.Background(), redirectClient(t, server), VariantTraeCN, creds, ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.AccessToken != "access-der" {
		t.Errorf("result = %+v", result)
	}
	if len(*captures) != 2 {
		t.Fatalf("应恰好两次请求, got %d", len(*captures))
	}
	// 两步同 URL、不同签名编码。
	firstProof := (*captures)[0].body["DeviceProof"].(map[string]any)
	secondProof := (*captures)[1].body["DeviceProof"].(map[string]any)
	if firstProof["Signature"] == secondProof["Signature"] {
		t.Error("P1363 与 DER 的签名编码必须不同")
	}
	if len(firstProof["Signature"].(string)) == len(secondProof["Signature"].(string)) {
		t.Errorf("DER 编码长度应不同于 P1363（%d vs %d）",
			len(secondProof["Signature"].(string)), len(firstProof["Signature"].(string)))
	}
	// 未轮换：响应缺 RefreshToken ⇒ 保留旧值。
	if result.RefreshToken != "rt-old" || result.RefreshRotated {
		t.Errorf("不轮换语义错误: %+v", result)
	}
}

// TestRefreshAccessTokenFallsBackToLegacy ★★ 步 1-3 全失败 → 步 4 旧协议兜底体成功。
func TestRefreshAccessTokenFallsBackToLegacy(t *testing.T) {
	server, captures := serveRefresh(t, func(capture refreshCapture, _ int) (int, string) {
		if _, hasProof := capture.body["DeviceProof"]; hasProof {
			return 200, `{"ResponseMetadata":{"Error":{"Code":"20403","Message":"Token device not match","StandardCode":"040036"}}}`
		}
		return 200, `{"data":{"access_token":"legacy-access","refresh_token":"legacy-refresh"}}`
	})
	creds, _ := deviceCreds(t, VariantSoloGlobal, "rt-old")

	result, err := RefreshAccessToken(context.Background(), redirectClient(t, server), VariantSoloGlobal, creds, ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.AccessToken != "legacy-access" || !result.RefreshRotated {
		t.Errorf("result = %+v", result)
	}
	if len(*captures) != 4 {
		t.Fatalf("四步链应恰好四次请求, got %d", len(*captures))
	}
	// 步 1/2 新端点，步 3/4 旧端点。
	wantPaths := []string{newRefreshPath, newRefreshPath, legacyRefreshPath2, legacyRefreshPath2}
	for i, want := range wantPaths {
		if (*captures)[i].path != want {
			t.Errorf("链[%d].path = %s, want %s", i, (*captures)[i].path, want)
		}
	}
	// 步 3 的 signPath 是旧路径（验签用旧路径原文）。
	thirdProof := (*captures)[2].body["DeviceProof"].(map[string]any)
	if (*captures)[2].body["ClientID"] != "en1oxy7wnw8j9n" {
		t.Errorf("国际 SOLO 变体 ClientID = %v", (*captures)[2].body["ClientID"])
	}
	_ = thirdProof
	// 步 4 兜底体：无 DeviceProof、ClientSecret "-"、UserID 空。
	legacy := (*captures)[3].body
	if _, exists := legacy["DeviceProof"]; exists {
		t.Error("步 4 不应带 DeviceProof")
	}
	if legacy["ClientSecret"] != "-" || legacy["UserID"] != "" {
		t.Errorf("步 4 兜底体 = %v", legacy)
	}
	if legacy["ClientID"] != "en1oxy7wnw8j9n" || legacy["RefreshToken"] != "rt-old" {
		t.Errorf("步 4 = %v", legacy)
	}
	// 步 4 头：不带 x-device-id / x-cloudide-token，仍带 x-app-id + x-platform-code。
	if got := (*captures)[3].header.Get("x-device-id"); got != "" {
		t.Error("步 4 不应带 x-device-id")
	}
	if got := (*captures)[3].header.Get("x-cloudide-token"); got != "" {
		t.Error("步 4 不应带 x-cloudide-token")
	}
	if got := (*captures)[3].header.Get("x-app-id"); got == "" {
		t.Error("步 4 应带 x-app-id")
	}
	if got := (*captures)[3].header.Get("x-platform-code"); got != "IDE_PC" {
		t.Errorf("步 4 x-platform-code = %q", got)
	}
}

// TestRefreshAccessTokenWithoutPrivateKey 无设备私钥：只剩步 4（唯一路径）。
func TestRefreshAccessTokenWithoutPrivateKey(t *testing.T) {
	server, captures := serveRefresh(t, func(capture refreshCapture, _ int) (int, string) {
		return 200, `{"Result":{"AccessToken":"only-legacy","RefreshToken":"rotated"}}`
	})
	creds := &Credentials{Variant: string(VariantTraeCN), RefreshToken: "rt-old"}

	result, err := RefreshAccessToken(context.Background(), redirectClient(t, server), VariantTraeCN, creds, ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(*captures) != 1 {
		t.Fatalf("无私钥应只走步 4, got %d 次请求", len(*captures))
	}
	if (*captures)[0].path != legacyRefreshPath2 {
		t.Errorf("path = %s", (*captures)[0].path)
	}
	if result.AccessToken != "only-legacy" || result.RefreshToken != "rotated" || !result.RefreshRotated {
		t.Errorf("result = %+v", result)
	}
	// 私钥字段存在但无法解析：同样只剩步 4，不整体失败。
	broken := &Credentials{Variant: string(VariantTraeCN), RefreshToken: "rt-old", DevicePrivateKey: "not a pem"}
	*captures = nil
	if _, err := RefreshAccessToken(context.Background(), redirectClient(t, server), VariantTraeCN, broken, ClientOptions{}); err != nil {
		t.Fatal(err)
	}
	if len(*captures) != 1 {
		t.Errorf("坏私钥应跳过步 1-3 只走步 4, got %d", len(*captures))
	}
}

// TestRefreshAccessTokenServerRejectedImmediate 旧形态数字 code != 0 是「服务端明确
// 拒绝」：立即终止，不再探测后续变体。
func TestRefreshAccessTokenServerRejectedImmediate(t *testing.T) {
	server, captures := serveRefresh(t, func(capture refreshCapture, _ int) (int, string) {
		return 200, `{"code":1005,"message":"plan limit"}`
	})
	creds, _ := deviceCreds(t, VariantSoloCN, "rt-old")

	_, err := RefreshAccessToken(context.Background(), redirectClient(t, server), VariantSoloCN, creds, ClientOptions{})
	if err == nil || !strings.Contains(err.Error(), "code=1005") {
		t.Fatalf("应报业务拒绝错误, got %v", err)
	}
	if len(*captures) != 1 {
		t.Errorf("明确拒绝应立即终止, got %d 次请求", len(*captures))
	}
}

// TestRefreshAccessTokenInputValidation 入参校验。
func TestRefreshAccessTokenInputValidation(t *testing.T) {
	server, _ := serveRefresh(t, func(capture refreshCapture, _ int) (int, string) {
		return 200, `{}`
	})
	client := redirectClient(t, server)
	if _, err := RefreshAccessToken(context.Background(), client, VariantSoloCN, nil, ClientOptions{}); err == nil {
		t.Error("nil credentials 应报错")
	}
	if _, err := RefreshAccessToken(context.Background(), client, VariantSoloCN,
		&Credentials{Variant: string(VariantSoloCN)}, ClientOptions{}); err == nil {
		t.Error("空 refresh_token 应报错")
	}
}

// TestRefreshAccessTokenLegacyProofSignature 步 3（旧端点）的 DeviceProof 签名
// 原文用的是旧路径（与请求 URL 路径一致，否则 20405）。
func TestRefreshAccessTokenLegacyProofSignature(t *testing.T) {
	var capturedProof map[string]any
	server, _ := serveRefresh(t, func(capture refreshCapture, _ int) (int, string) {
		if capture.path == legacyRefreshPath2 {
			if proof, ok := capture.body["DeviceProof"].(map[string]any); ok {
				capturedProof = proof
			}
			return 200, `{"Result":{"AccessToken":"ok"}}`
		}
		return 200, `{"ResponseMetadata":{"Error":{"Code":"20405","Message":"no","StandardCode":""}}}`
	})
	creds, pair := deviceCreds(t, VariantTraeCN, "rt-old")

	if _, err := RefreshAccessToken(context.Background(), redirectClient(t, server), VariantTraeCN, creds, ClientOptions{}); err != nil {
		t.Fatal(err)
	}
	if capturedProof == nil {
		t.Fatal("旧端点未捕获到带 Proof 的请求")
	}
	// ★ 用旧路径还原签名原文验签（P1363，步 3 用 P1363 格式）。
	verifyProofSignature(t, pair.PublicKeyPEM, capturedProof, legacyRefreshPath2, "ono9krqynydwx5", "rt-old")
}

// TestCredentialsDevicePrivateKeyRoundTrip 契约字段透传。
func TestCredentialsDevicePrivateKeyRoundTrip(t *testing.T) {
	pair, err := GenerateDeviceIdentity()
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]string{
		"variant":            "trae_cn",
		"refresh_token":      "rt",
		"device_id":          pair.DeviceID,
		"device_private_key": pair.PrivateKeyPEM,
	}
	creds, err := ParseCredentials(m)
	if err != nil {
		t.Fatal(err)
	}
	if creds.DevicePrivateKey != pair.PrivateKeyPEM || creds.DeviceID != pair.DeviceID {
		t.Error("透传失败")
	}
	out := creds.ToMap()
	if out["device_private_key"] != pair.PrivateKeyPEM {
		t.Error("ToMap 透传失败")
	}
	// 空私钥省略键（omitempty 语义）。
	empty := &Credentials{Variant: "trae_cn"}
	if _, exists := empty.ToMap()["device_private_key"]; exists {
		t.Error("空私钥应省略键")
	}
}
