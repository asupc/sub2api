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

// VirtualDeviceName 首登生成的虚拟设备名（授权、交换、refresh 三处同源；
// service 首登与此处 refreshDeviceFacts 共用，勿在各处再写字面量）。
const VirtualDeviceName = "sub2api"

// refreshDeviceFacts 从存储凭据重建 refresh 链的设备事实：与首登授权/交换
// 发的是同一台虚拟设备（DeviceName 固定值，其余系统字段与首登零值一致）；
// DevicePublicKey 从私钥推导（与上报公钥同源）。无私钥（显式公钥对账号）
// 时公钥留空——「诚实地说我没有私钥」，让上游判定。
func refreshDeviceFacts(creds *Credentials) DeviceFacts {
	facts := DeviceFacts{
		DeviceID:   creds.DeviceID,
		MachineID:  creds.MachineID,
		DeviceName: VirtualDeviceName,
	}
	if strings.TrimSpace(creds.DevicePrivateKey) != "" {
		if pub, err := DerivePublicKeyPEM(creds.DevicePrivateKey); err == nil {
			facts.DevicePublicKeyPEM = pub
		}
	}
	return facts
}

// buildRefreshPayload 构造 refresh 交换请求体（逐字对齐客户端 1.107.1 的
// exchangeTokenByRefreshToken）：
//
//	{ ClientID, ClientSecret:"", RefreshToken, DeviceInfo, DeviceProof?, IDEVersion }
//
// ClientSecret 是**空串**（不是旧协议兜底体的 "-"）；DeviceInfo 与 AuthCode
// 交换发同一个（deviceInfoPayload 唯一派生点）；无设备私钥时**不发**
// DeviceProof——签名原文必须用私钥，编一个出来只会 20405，少一个字段是
// 「诚实地说我没有私钥」。
func buildRefreshPayload(clientID, refreshToken string, deviceInfo map[string]any, appVersion string, proof map[string]any) map[string]any {
	payload := map[string]any{
		"ClientID":     clientID,
		"ClientSecret": "",
		"RefreshToken": refreshToken,
		"DeviceInfo":   deviceInfo,
		"IDEVersion":   appVersion,
	}
	if proof != nil {
		payload["DeviceProof"] = proof
	}
	return payload
}

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
	platformCode      string
	withCloudideToken bool
	withDeviceID      bool
}

// RefreshAccessToken 用 refresh_token 换新的 access token（移植源 account.rs
// build_refresh_variants 的**变体链**，顺序即优先级；2026-10-22 真机缺陷修复
// 后的形态）。变体三条维度，与客户端 1.107.1 的失败面一一对应：
//
//	| 维度 | 取值 | 为什么 |
//	| 端点 | account_base 新路径 / icube_base 旧端点 | 客户端 this.apiHost vs 旧 cloudide 路由 |
//	| 签名 | P1363 / DER / 无 proof | 私钥编码形态与「没有私钥」的诚实形态 |
//	| 产品线 | 本变体的线 / 另一条线 | ★ refresh_token 绑的是**签发它的那把钥匙** |
//
// ★ 为什么必须试「另一条线」：上游对「拿 A 线的 refresh_token 去换 B 线的
// token」回 10101/040004 + "refresh token is not matched to the client"（官方
// 客户端自己也会撞）。一个账号的 refresh_token 可能属于另一条产品线的客户端，
// 本线的钥匙被拒时再试另一把，成功即写回。
//
// 变体清单（有设备私钥时）：
//	| 步 | 端点 | DeviceProof | 产品线 |
//	| 1 | {account_base}/trae/api/v3/oauth/ExchangeToken      | P1363 | 本线 |
//	| 2 | 同 1                                                  | DER   | 本线 |
//	| 3 | {icube_base}/cloudide/api/v3/trae/oauth/ExchangeToken | P1363 | 本线 |
//	| 4 | 同 1                                                  | 无    | 本线 |
//	| 5 | 同 1                                                  | P1363 | 另一条线 |
//	| 6 | 同 1                                                  | 无    | 另一条线 |
// 无设备私钥时每线只发一步「无 proof」形态（共两步）。
//
// 关键语义（与 AuthCode 交换的差异）：
//   - ClientID 按变体链当前步的产品线取（不再固定本线）；
//   - 火山信封错误（20403/20405 等设备/协议错误，含 10101/040004 参数无效）
//     视为该变体失败、**继续探测下一变体**（下一变体形状不同，值得一试）；
//     旧形态数字 code != 0 是「服务端明确拒绝」，**立即终止**不再探测；
//   - 响应缺 RefreshToken 视为成功（可能不轮换），返回保留旧值、RefreshRotated=false。
//
// creds 需要 RefreshToken、DeviceID、MachineID、DevicePrivateKey（私钥可空）。
// httpClient 为 nil 时用 http.DefaultClient。
func RefreshAccessToken(ctx context.Context, httpClient *http.Client, variant Variant, creds *Credentials, opts ClientOptions) (*TokenRefreshResult, error) {
	if creds == nil {
		return nil, fmt.Errorf("credentials 为空")
	}
	refreshToken := strings.TrimSpace(creds.RefreshToken)
	if refreshToken == "" {
		return nil, fmt.Errorf("refresh_token 为空，无法续期")
	}

	ownLine := variant.OAuthLine()
	otherLine := OAuthLineTrae
	if ownLine == OAuthLineTrae {
		otherLine = OAuthLineSolo
	}
	endpoints := variant.Endpoints()
	newURL := endpoints.AccountBase + exchangeTokenPath
	legacyURL := endpoints.IcubeBase + exchangeTokenLegacyPath

	// 设备私钥可解析 ⇒ 进入签名变体；解析失败（私钥形态变化）不整体失败——
	// 后面还有无 proof 的兜底，多一条路总比直接放弃好。
	// 无凭证时**不发明 x-device-id**：自造值与签名私钥不匹配，上游只会回
	// 20403/20405。
	var deviceKey *ecdsa.PrivateKey
	if strings.TrimSpace(creds.DevicePrivateKey) != "" {
		if key, err := parseDevicePrivateKey(creds.DevicePrivateKey); err == nil {
			deviceKey = key
		}
	}
	facts := refreshDeviceFacts(creds)

	type variantPlan struct {
		line    OAuthLine
		url     string
		signPath string
		format  *proofSigFormat
		tag     string
	}
	plans := []variantPlan{}
	for i, ln := range []OAuthLine{ownLine, otherLine} {
		prefix := ""
		if i == 1 {
			prefix = "OtherLine/"
		}
		if deviceKey == nil {
			// 无设备私钥：只发「无 proof」形态。
			plans = append(plans, variantPlan{ln, newURL, "", nil, prefix + "Refresh/NoProof"})
			continue
		}
		if prefix == "" {
			// 本线：P1363 / DER（新端点）+ P1363（旧端点）+ 无 proof。
			p1363, der := proofP1363, proofDer
			plans = append(plans,
				variantPlan{ln, newURL, exchangeTokenPath, &p1363, prefix + "Refresh/Proof/P1363"},
				variantPlan{ln, newURL, exchangeTokenPath, &der, prefix + "Refresh/Proof/DER"},
				variantPlan{ln, legacyURL, exchangeTokenLegacyPath, &p1363, prefix + "Refresh/LegacyEndpoint/Proof/P1363"},
				variantPlan{ln, newURL, "", nil, prefix + "Refresh/NoProof"},
			)
		} else {
			// 另一条线是兜底轴，不必铺满：P1363 + 无 proof。
			p1363 := proofP1363
			plans = append(plans,
				variantPlan{ln, newURL, exchangeTokenPath, &p1363, prefix + "Refresh/Proof/P1363"},
				variantPlan{ln, newURL, "", nil, prefix + "Refresh/NoProof"},
			)
		}
	}

	variants := make([]refreshVariant, 0, len(plans))
	for _, plan := range plans {
		clientID := opts.ClientIDFor(plan.line)
		var proof map[string]any
		if plan.format != nil {
			p, err := buildDeviceProof(deviceKey, plan.signPath, clientID, refreshToken, *plan.format)
			if err != nil {
				// 签名失败跳过该变体而不是整体失败。
				continue
			}
			proof = p
		}
		payload := buildRefreshPayload(clientID, refreshToken,
			deviceInfoPayload(facts, plan.line), facts.appVersion(), proof)
		variants = append(variants, refreshVariant{
			tag:               plan.tag,
			url:               plan.url,
			payload:           payload,
			platformCode:      plan.line.PlatformCode(),
			withCloudideToken: true,
			withDeviceID:      proof != nil,
		})
	}

	var errs []string
	sawInvalidParam := false
	for _, plan := range variants {
		access, newRefresh, rejected, invalidParam, err := tryRefreshVariant(ctx, httpClient, plan, creds.DeviceID)
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
		if invalidParam {
			sawInvalidParam = true
		}
		if rejected {
			// 服务端明确拒绝（旧形态数字 code != 0）：立即采纳，不再探测。
			return nil, err
		}
	}
	if sawInvalidParam {
		return nil, fmt.Errorf("全部刷新变体失败（含「请求参数无效 10101/040004」——请求形状与上游协议不符，请重新登录该账号并反馈工具更新）→ %s", strings.Join(errs, " | "))
	}
	return nil, fmt.Errorf("全部刷新变体失败 → %s", strings.Join(errs, " | "))
}

// tryRefreshVariant 单个刷新变体尝试：请求 → 错误分类 → token 提取。
// 返回 (access, 新 refresh_token（可能为空 = 不轮换）, 是否服务端明确拒绝, 错误)。
func tryRefreshVariant(ctx context.Context, httpClient *http.Client, plan refreshVariant, deviceID string) (string, string, bool, bool, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	body, err := json.Marshal(plan.payload)
	if err != nil {
		return "", "", false, false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, plan.url, strings.NewReader(string(body)))
	if err != nil {
		return "", "", false, false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("x-app-id", oauthAppID)
	req.Header.Set("x-platform-code", plan.platformCode)
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
		return "", "", false, false, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()
	raw, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return "", "", false, false, fmt.Errorf("读取响应失败 (HTTP %d): %w", resp.StatusCode, readErr)
	}

	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", "", false, false, fmt.Errorf("解析响应失败 (HTTP %d): %w", resp.StatusCode, err)
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
				// 10101/040004 是「请求参数无效」（协议/形状问题，非凭据问题）：
				// 火山信封里的 Message 常是未替换的占位符，只能靠码判定。继续探测
				// 下一变体（形状不同，值得一试），并标记供全链失败后的文案。
				invalidParam := code == "10101" || standard == "040004"
				return "", "", false, invalidParam, fmt.Errorf("code=%s/%s: %s", code, standard, message)
			}
		}
	}
	// 旧形态数字 code：唯一可信的「服务端明确拒绝」信号。
	if code, ok := parsed["code"].(float64); ok && code != 0 {
		message, _ := parsed["message"].(string)
		if message == "" {
			message = "未知错误"
		}
		return "", "", true, false, fmt.Errorf("code=%.0f: %s", code, message)
	}

	access := findTokenValue(parsed, accessTokenKeys)
	if access == "" {
return "", "", false, false, fmt.Errorf("响应中未找到 AccessToken 字段（响应键：%s）", strings.Join(keyPaths(parsed), " | "))
	}
	return access, findTokenValue(parsed, refreshTokenKeys), false, false, nil
}
