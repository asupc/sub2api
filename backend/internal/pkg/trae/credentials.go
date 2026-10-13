package trae

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"strconv"
)

// Credentials sub2api 渠道凭据（存储层 credentials JSONB 的契约形态，值均为 string，
// json tag 下划线）。
//
// 字段说明：
//   - JWT：Cloud-IDE-JWT 凭据（裸 token 或带前缀均可，读取侧会归一化）；
//   - RefreshToken：OAuth 交换得到的刷新令牌（用于换取新 JWT）；
//   - ExpiresAt：JWT 到期时间（Unix 秒，字符串形态）；
//   - Variant：产品线变体（trae_cn|solo_cn|trae_global|solo_global）；
//   - UID：用户 ID（JWT data.id；缺失时读取侧可从 JWT 解析补齐）；
//   - DeviceID / MachineID：上游请求的设备标识；
//   - DevicePrivateKey：虚拟设备的 EC P-256 私钥（PKCS#8 PEM）。首登时经
//     GenerateDeviceIdentity 生成、与 DeviceID 成套写入；refresh 换 token 时用它
//     生成 DeviceProof 签名（详见 refresh.go）。
type Credentials struct {
	JWT              string `json:"jwt"`
	RefreshToken     string `json:"refresh_token"`
	ExpiresAt        string `json:"expires_at"`
	Variant          string `json:"variant"`
	UID              string `json:"uid"`
	DeviceID         string `json:"device_id"`
	MachineID        string `json:"machine_id"`
	DevicePrivateKey string `json:"device_private_key,omitempty"`
}

// ParsedCredentials 从存储层的 credentials map 解析（map[string]string，值均为字符串）。
// 只要求 variant 可识别；其余字段缺失时由调用方按需补齐（见 EnsureIdentity）。
func ParseCredentials(m map[string]string) (*Credentials, error) {
	c := &Credentials{
		JWT:              m["jwt"],
		RefreshToken:     m["refresh_token"],
		ExpiresAt:        m["expires_at"],
		Variant:          m["variant"],
		UID:              m["uid"],
		DeviceID:         m["device_id"],
		MachineID:        m["machine_id"],
		DevicePrivateKey: m["device_private_key"],
	}
	if c.Variant == "" {
		return nil, fmt.Errorf("trae 凭据缺少 variant 字段")
	}
	if _, ok := ParseVariant(c.Variant); !ok {
		return nil, fmt.Errorf("trae 凭据的 variant 无效：%q", c.Variant)
	}
	return c, nil
}

// ToMap 序列化回存储层的 credentials map（值均为字符串）。
// 私钥为空时省略该键（与 json tag 的 omitempty 语义一致）。
func (c *Credentials) ToMap() map[string]string {
	m := map[string]string{
		"jwt":           c.JWT,
		"refresh_token": c.RefreshToken,
		"expires_at":    c.ExpiresAt,
		"variant":       c.Variant,
		"uid":           c.UID,
		"device_id":     c.DeviceID,
		"machine_id":    c.MachineID,
	}
	if c.DevicePrivateKey != "" {
		m["device_private_key"] = c.DevicePrivateKey
	}
	return m
}

// DeviceKeyPair 首登生成的虚拟设备身份。
type DeviceKeyPair struct {
	// DeviceID 数字形态的设备标识（对齐上游 icube 信封的形态）。
	DeviceID string
	// PrivateKeyPEM EC P-256 私钥（PKCS#8 PEM），存入 credentials 的
	// device_private_key；refresh 换 token 时用它生成 DeviceProof 签名。
	PrivateKeyPEM string
	// PublicKeyPEM SPKI 公钥 PEM，首登 OAuth 交换时填进 DeviceFacts 的
	// DevicePublicKeyPEM（交换上报的公钥必须与刷新签名的私钥同源）。
	PublicKeyPEM string
}

// GenerateDeviceIdentity 生成一台新的虚拟设备身份（EC P-256 密钥对 + 随机数字 device_id）。
//
// 源实现的设备身份来自本机客户端的 icube 加密信封（storage.json）；服务器场景没有
// 本机客户端可读，首登生成即代表一台新虚拟设备——DeviceID 与私钥成套写入 credentials，
// 后续 refresh 的 DeviceProof 签名与 OAuth 交换上报的公钥天然同源。
func GenerateDeviceIdentity() (DeviceKeyPair, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return DeviceKeyPair{}, fmt.Errorf("生成设备密钥失败: %w", err)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return DeviceKeyPair{}, fmt.Errorf("编码设备私钥失败: %w", err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return DeviceKeyPair{}, fmt.Errorf("编码设备公钥失败: %w", err)
	}
	return DeviceKeyPair{
		DeviceID:      hexToDigits(RandomHex(8)),
		PrivateKeyPEM: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER})),
		PublicKeyPEM:  string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER})),
	}, nil
}

// hexToDigits 把 16 位 hex 逐位映射成 0-9（保持上游 device_id 的纯数字形态且稳定）。
func hexToDigits(hexed string) string {
	digits := make([]byte, len(hexed))
	for i := 0; i < len(hexed); i++ {
		d, _ := strconv.ParseInt(string(hexed[i]), 16, 8)
		digits[i] = byte('0' + d%10)
	}
	return string(digits)
}

// VariantOf 返回解析后的变体（ParseCredentials 已保证可识别）。
func (c *Credentials) VariantOf() Variant {
	v, _ := ParseVariant(c.Variant)
	return v
}

// EnsureIdentity 补齐缺失的身份字段：
//   - UID 缺失时从 JWT 解析（data.id → auth_id → sub）；
//   - MachineID 缺失时从 UID 稳定派生（sha256 截断的 UUID 形态）；
//   - DeviceID 缺失时从 UID 稳定派生（sha256 截断）。
//
// 派生说明：源实现的 machine_id 取自客户端 telemetry.machineId（UUID 形态）、
// device_id 取自 iCube 设备信封（数字串形态），服务器场景没有本机客户端可读，
// 故按 UID 确定性派生（同一账号永远得到同一标识，跨重启稳定），并用不同盐隔离
// 两个用途。若上游对 OAuth 交换的设备校验（20403）较严，应通过 OAuth 流程拿真实值。
func (c *Credentials) EnsureIdentity() {
	if c.UID == "" && c.JWT != "" {
		c.UID = UserIDOf(c.JWT)
	}
	if c.MachineID == "" && c.UID != "" {
		c.MachineID = deriveUUID("trae:machine:", c.UID)
	}
	if c.DeviceID == "" && c.UID != "" {
		c.DeviceID = deriveDeviceID("trae:device:", c.UID)
	}
}

// deriveUUID 从盐 + UID 派生 UUID 形态的稳定标识（8-4-4-4-12）。
func deriveUUID(salt, uid string) string {
	sum := sha256.Sum256([]byte(salt + uid))
	hexed := hex.EncodeToString(sum[:16])
	return fmt.Sprintf("%s-%s-%s-%s-%s", hexed[0:8], hexed[8:12], hexed[12:16], hexed[16:20], hexed[20:32])
}

// deriveDeviceID 从盐 + UID 派生数字串形态的设备标识（对齐源形态：纯数字）。
func deriveDeviceID(salt, uid string) string {
	sum := sha256.Sum256([]byte(salt + uid))
	return hexToDigits(hex.EncodeToString(sum[:8]))
}
