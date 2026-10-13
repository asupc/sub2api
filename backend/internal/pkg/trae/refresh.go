package trae

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/asn1"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
)

// refreshPlatformCode refresh 路径的 PlatformCode **写死 IDE_PC**（源 account.rs
// build_refresh_payload 直接引用 TRAE_PAGE_PLATFORM_CODE 常量），与授权 /
// AuthCode 交换路径的按产品线派生（SOLO_PC）不同——源码事实，勿"顺手统一"。
const refreshPlatformCode = "IDE_PC"

// refreshClientSecret 步 4 旧协议兜底体的 ClientSecret（源 oauth_client.rs
// DEFAULT_CLIENT_SECRET，逐字 "-"）。
const refreshClientSecret = "-"

// proofSigFormat DeviceProof 签名的两种编码（源 icube.rs ProofSigFormat）。
type proofSigFormat int

const (
	// proofP1363 r‖s 固定 64 字节（P1363 / WebCrypto / ring 兼容），首选。
	proofP1363 proofSigFormat = iota
	// proofDer ASN.1 DER SEQUENCE(r, s)，次选。
	proofDer
)

// ecdsaSignatureASN1 DER 编码的 ECDSA 签名结构（与 crypto/tls 同款）。
type ecdsaSignatureASN1 struct {
	R, S *big.Int
}

// buildDeviceProof 生成 DeviceProof JSON（PascalCase 字段；源码实测小写会被 20405 拒，
// Timestamp 必须为 JSON int——字符串会被服务端 schema 拒）。
//
// 签名原文（5 个 \n 连接）：POST\n<sign_path>\n<ClientID>\n<RefreshToken>\n<Timestamp>\n<Nonce>。
// ⚠️ 只有 refreshToken 刷新路径调用它；AuthCode 交换路径不得调用（源模块头的私钥分工红线）。
// signPath 必须与请求 URL 的路径逐字一致，否则服务端按另一条路径验签必然 20405。
func buildDeviceProof(key *ecdsa.PrivateKey, signPath, clientID, refreshToken string, format proofSigFormat) (map[string]any, error) {
	timestamp := nowUnix()
	nonce := RandomHex(32)
	message := fmt.Sprintf("POST\n%s\n%s\n%s\n%d\n%s", signPath, clientID, refreshToken, timestamp, nonce)

	hash := sha256.Sum256([]byte(message))
	r, s, err := ecdsa.Sign(rand.Reader, key, hash[:])
	if err != nil {
		return nil, fmt.Errorf("设备签名失败: %w", err)
	}
	var encoded []byte
	switch format {
	case proofP1363:
		// r‖s 各 32 字节大端，固定 64 字节。
		encoded = make([]byte, 64)
		r.FillBytes(encoded[:32])
		s.FillBytes(encoded[32:])
	case proofDer:
		encoded, err = asn1.Marshal(ecdsaSignatureASN1{R: r, S: s})
		if err != nil {
			return nil, fmt.Errorf("签名 DER 编码失败: %w", err)
		}
	}
	return map[string]any{
		"Signature": base64.StdEncoding.EncodeToString(encoded),
		"Timestamp": timestamp,
		"Nonce":     nonce,
	}, nil
}

// parseDevicePrivateKey 解析 PKCS#8 PEM 的设备私钥（须为 EC P-256）。
func parseDevicePrivateKey(privateKeyPEM string) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(privateKeyPEM))
	if block == nil {
		return nil, fmt.Errorf("设备私钥 PEM 解析失败")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("设备私钥解析失败（PKCS#8 无效或不受支持）")
	}
	ecKey, ok := key.(*ecdsa.PrivateKey)
	if !ok || ecKey.Curve != elliptic.P256() {
		return nil, fmt.Errorf("设备私钥必须是 EC P-256 密钥")
	}
	return ecKey, nil
}

// DerivePublicKeyPEM 从 PKCS#8 私钥导出 SPKI 公钥 PEM（供 DeviceFacts 的
// DevicePublicKeyPEM 用——首登 OAuth 交换时上报的公钥必须与刷新签名的私钥同源）。
func DerivePublicKeyPEM(privateKeyPEM string) (string, error) {
	key, err := parseDevicePrivateKey(privateKeyPEM)
	if err != nil {
		return "", err
	}
	publicDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER})), nil
}

// TokenRefreshResult refresh_token 换 access token 的结果。
type TokenRefreshResult struct {
	// AccessToken 裸 JWT（无前缀）。
	AccessToken string
	// RefreshToken 应当持久化的 refresh_token：上游回传新值时为新值，
	// 未回传（可能不轮换）时为入参旧值。
	RefreshToken string
	// RefreshRotated 上游是否轮换了 refresh_token（响应带新值即 true）。
	// ★ 轮换时必须写回，否则下次刷新用旧值必掉线。
	RefreshRotated bool
}

// Authorization 已组好的 Authorization 头值（Cloud-IDE-JWT <jwt>）。
func (t *TokenRefreshResult) Authorization() string {
	return AuthorizationHeader(t.AccessToken)
}

// refreshVariant 一个刷新变体（纯数据，便于测试断言顺序）。
type refreshVariant struct {
	tag               string
	url               string
	payload           map[string]any
	withCloudideToken bool
	withDeviceID      bool
}

// RefreshAccessToken 用 refresh_token 换新的 access token（移植源 account.rs
// exchange_token_for 的**四步变体链**，顺序即优先级）：
//
//	步 1  {icube_base}/trae/api/v3/oauth/ExchangeToken            固化体 + DeviceProof(P1363)
//	步 2  同步 1 端点                                              固化体 + DeviceProof(DER)
//	步 3  {icube_base}/cloudide/api/v3/trae/oauth/ExchangeToken   固化体 + DeviceProof(P1363)
//	步 4  同步 3 端点                                              旧协议兜底体（无 Proof、无私钥）
//
// 关键语义（与 AuthCode 交换的差异）：
//   - 步 1-3 仅在 credentials 带设备私钥且可解析时进入（签名失败跳过该变体，不整体失败）；
//     步 4 无条件存在，无设备私钥时它是唯一路径（服务器首登生成的虚拟设备即走完整四步）；
//   - ClientID 按产品线取（与授权 URL / AuthCode 交换三处同源）；
//   - PlatformCode 此路径写死 IDE_PC（源码事实）；
//   - 火山信封错误（20403/20405 等设备/协议错误）视为该变体失败、**继续探测下一变体**；
//     旧形态数字 code != 0 是「服务端明确拒绝」，**立即终止**不再探测；
//   - 响应缺 RefreshToken 视为成功（可能不轮换），返回保留旧值、RefreshRotated=false。
//
// creds 需要 RefreshToken、DeviceID、DevicePrivateKey（私钥可空）。
// httpClient 为 nil 时用 http.DefaultClient。
func RefreshAccessToken(ctx context.Context, httpClient *http.Client, variant Variant, creds *Credentials, opts ClientOptions) (*TokenRefreshResult, error) {
	if creds == nil {
		return nil, fmt.Errorf("credentials 为空")
	}
	refreshToken := strings.TrimSpace(creds.RefreshToken)
	if refreshToken == "" {
		return nil, fmt.Errorf("refresh_token 为空，无法续期")
	}

	// ★ ClientID 按产品线取——与授权 URL / AuthCode 交换用同一个取值函数，
	// 三处必须同源，否则等于拿 A 线的钥匙兑 B 线的 token。
	clientID := opts.ClientIDFor(variant.OAuthLine())
	icubeBase := variant.Endpoints().IcubeBase
	newURL := icubeBase + exchangeTokenPath
	legacyURL := icubeBase + exchangeTokenLegacyPath

	// 设备私钥可解析 ⇒ 进入步 1-3；解析失败（私钥形态变化）不整体失败——
	// 后面还有步 4 兜底，多一条路总比直接放弃好（源行为）。
	// 无凭证时**不发明 x-device-id**：自造值与签名私钥不匹配，上游只会回 20403/20405。
	var deviceKey *ecdsa.PrivateKey
	if strings.TrimSpace(creds.DevicePrivateKey) != "" {
		if key, err := parseDevicePrivateKey(creds.DevicePrivateKey); err == nil {
			deviceKey = key
		}
	}

	buildProofPayload := func(signPath string, format proofSigFormat) (map[string]any, bool) {
		if deviceKey == nil {
			return nil, false
		}
		proof, err := buildDeviceProof(deviceKey, signPath, clientID, refreshToken, format)
		if err != nil {
			// 签名失败跳过该变体而不是整体失败。
			return nil, false
		}
		return map[string]any{
			"ClientID":     clientID,
			"RefreshToken": refreshToken,
			"DeviceID":     creds.DeviceID,
			"PlatformCode": refreshPlatformCode,
			"DeviceProof":  proof,
		}, true
	}

	variants := make([]refreshVariant, 0, 4)
	if deviceKey != nil {
		proofSteps := []struct {
			format   proofSigFormat
			url      string
			signPath string
			tag      string
		}{
			{proofP1363, newURL, exchangeTokenPath, "Refresh/Proof/P1363"},
			{proofDer, newURL, exchangeTokenPath, "Refresh/Proof/DER"},
			{proofP1363, legacyURL, exchangeTokenLegacyPath, "Refresh/LegacyEndpoint/Proof/P1363"},
		}
		for _, step := range proofSteps {
			if payload, ok := buildProofPayload(step.signPath, step.format); ok {
				variants = append(variants, refreshVariant{
					tag:               step.tag,
					url:               step.url,
					payload:           payload,
					withCloudideToken: true,
					withDeviceID:      true,
				})
			}
		}
	}
	// 旧协议兜底：无条件存在（无设备私钥时唯一路径）。
	variants = append(variants, refreshVariant{
		tag: "Refresh/Legacy",
		url: legacyURL,
		payload: map[string]any{
			"ClientID":     clientID,
			"RefreshToken": refreshToken,
			"ClientSecret": refreshClientSecret,
			"UserID":       "",
		},
	})

	var errs []string
	for _, plan := range variants {
		access, newRefresh, rejected, err := tryRefreshVariant(ctx, httpClient, plan, creds.DeviceID)
		if err == nil {
			// refresh_token 可能不轮换：响应缺 RefreshToken 视为成功，保留旧值；
			// 上游轮换时必须写回，否则下次刷新用旧值必掉线。
			result := &TokenRefreshResult{AccessToken: access, RefreshToken: refreshToken}
			if rotated := strings.TrimSpace(newRefresh); rotated != "" {
				result.RefreshToken = rotated
				result.RefreshRotated = true
			}
			return result, nil
		}
		errs = append(errs, fmt.Sprintf("%s: %v", plan.tag, err))
		if rejected {
			// 服务端明确拒绝（旧形态数字 code != 0）：立即采纳，不再探测。
			return nil, err
		}
	}
	return nil, fmt.Errorf("全部刷新变体失败 → %s", strings.Join(errs, " | "))
}

// tryRefreshVariant 单个刷新变体尝试：请求 → 错误分类 → token 提取。
// 返回 (access, 新 refresh_token（可能为空 = 不轮换）, 是否服务端明确拒绝, 错误)。
func tryRefreshVariant(ctx context.Context, httpClient *http.Client, plan refreshVariant, deviceID string) (string, string, bool, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	body, err := json.Marshal(plan.payload)
	if err != nil {
		return "", "", false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, plan.url, strings.NewReader(string(body)))
	if err != nil {
		return "", "", false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("x-app-id", oauthAppID)
	req.Header.Set("x-platform-code", refreshPlatformCode)
	if plan.withDeviceID {
		// x-device-id 与 DeviceProof 的 DeviceID 同源。
		req.Header.Set("x-device-id", deviceID)
	}
	if plan.withCloudideToken {
		// 必须存在且为空串：缺失报 20403，带旧 token 报 20405。
		req.Header.Set("x-cloudide-token", "")
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", "", false, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()
	raw, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return "", "", false, fmt.Errorf("读取响应失败 (HTTP %d): %w", resp.StatusCode, readErr)
	}

	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", "", false, fmt.Errorf("解析响应失败 (HTTP %d): %w", resp.StatusCode, err)
	}

	// 火山信封错误：协议级失败（可能是 DeviceProof/设备问题），继续探测下一变体。
	if metadata, ok := parsed["ResponseMetadata"].(map[string]any); ok {
		if errObject, ok := metadata["Error"].(map[string]any); ok {
			code, _ := errObject["Code"].(string)
			if code != "" && code != "0" {
				message, _ := errObject["Message"].(string)
				if message == "" {
					message = "未知错误"
				}
				standard, _ := errObject["StandardCode"].(string)
				return "", "", false, fmt.Errorf("code=%s/%s: %s", code, standard, message)
			}
		}
	}
	// 旧形态数字 code：唯一可信的「服务端明确拒绝」信号。
	if code, ok := parsed["code"].(float64); ok && code != 0 {
		message, _ := parsed["message"].(string)
		if message == "" {
			message = "未知错误"
		}
		return "", "", true, fmt.Errorf("code=%.0f: %s", code, message)
	}

	access := findTokenValue(parsed, accessTokenKeys)
	if access == "" {
		return "", "", false, fmt.Errorf("响应中未找到 AccessToken 字段（响应键：%s）", strings.Join(keyPaths(parsed), " | "))
	}
	return access, findTokenValue(parsed, refreshTokenKeys), false, nil
}
