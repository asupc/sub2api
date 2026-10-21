package qoder

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"io"
	"maps"
	"math/big"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// randomReader 包内随机源，默认 crypto/rand；测试注入确定性Reader做黄金向量。
var randomReader io.Reader = rand.Reader

// COSY 协议版本（qoder_sign.py L522-526）：官方 0.4.3 客户端
// （qoder-agent-sdk / qoder-cn-agent-sdk）内置常量 "1.1.64"，
// cosy-version 头与签名 payload 共用。
const COSYVersion = "1.1.64"

// DefaultUserType 身份体缺省用户类型（qoder_sign.py L526）。
const DefaultUserType = "personal_professional_trial"

// serverPubPEM Qoder 服务端 RSA 公钥，逐字提取自 qoder_sign.py L462-467
// （官方客户端硬编码；实测 1024 bit / e=65537，配 PKCS#1 v1.5 type-2 加密。
// 强度低于当代推荐属协议层决策，客户端侧无法单方面更换）。
const serverPubPEM = `-----BEGIN PUBLIC KEY-----
MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQDA8iMH5c02LilrsERw9t6Pv5Nc
4k6Pz1EaDicBMpdpxKduSZu5OANqUq8er4GM95omAGIOPOh+Nx0spthYA2BqGz+l
6HRkPJ7S236FZz73In/KVuLnwI8JJ2CbuJap8kvheCCZpmAWpb/cPx/3Vr/J6I17
XcW+ML9FoCI6AOvOzwIDAQAB
-----END PUBLIC KEY-----`

var serverPubKey *rsa.PublicKey

func init() {
	block, _ := pem.Decode([]byte(serverPubPEM))
	if block == nil {
		panic("qoder: bad server public key PEM")
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		panic("qoder: parse server public key: " + err.Error())
	}
	rsaKey, ok := key.(*rsa.PublicKey)
	if !ok {
		panic("qoder: server public key is not RSA")
	}
	serverPubKey = rsaKey
}

// jsonString 按 Python json.dumps(s, ensure_ascii=False) 的转义规则编码
// JSON 字符串：仅转义 \" \\ \b \f \n \r \t 与 <0x20 控制字符（\u00XX），
// 非 ASCII（含 U+2028/U+2029、'/'）原样输出。Go 标准库会额外转义
// HTML 字符与 U+2028/U+2029，与服务端签名字节绑定不符，故手工实现。
func jsonString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// SortedCompactJSON 键排序 + 无空白的紧凑 JSON（qoder_sign.py L529-536）。
// 服务端签名与 identity/payload 字节强绑定；值均为字符串（nil 语义不出现，
// 空值由调用方传入 ""）。
func SortedCompactJSON(mapping map[string]string) []byte {
	keys := slices.Sorted(maps.Keys(mapping))
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, jsonString(k)+":"+jsonString(mapping[k]))
	}
	return []byte("{" + strings.Join(parts, ",") + "}")
}

// aesCBCEncrypt AES-128-CBC 加密（PKCS7 填充），qoder info 加密使用
// key == iv == temp_key（qoder_sign.py L209-223）。
func aesCBCEncrypt(plain, key, iv []byte) ([]byte, error) {
	if len(key) != 16 || len(iv) != 16 {
		return nil, fmt.Errorf("qoder: aes key/iv must be 16 bytes, got %d/%d", len(key), len(iv))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	pad := 16 - len(plain)%16
	data := append(plain[:len(plain):len(plain)], bytes.Repeat([]byte{byte(pad)}, pad)...)
	dst := make([]byte, len(data))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(dst, data)
	return dst, nil
}

// rsaWrapTempKey RSA PKCS#1 v1.5 (type 2) 公钥加密，用于包裹会话 AES key
// （qoder_sign.py L501-516 的纯 Python 实现逐行对齐）。不用标准库
// rsa.EncryptPKCS1v15：Go 1.27 起其忽略传入的 random 源（内部固定走全局
// CSPRNG），黄金向量测试无法注入确定性填充；手写实现与源同构且可测。
func rsaWrapTempKey(random io.Reader, plain []byte) ([]byte, error) {
	n, e := serverPubKey.N, big.NewInt(int64(serverPubKey.E))
	k := (n.BitLen() + 7) / 8
	if len(plain) > k-11 {
		return nil, fmt.Errorf("qoder: message too long for RSA block")
	}
	psLen := k - 3 - len(plain)
	ps := make([]byte, 0, psLen)
	buf := make([]byte, psLen)
	for len(ps) < psLen {
		if _, err := io.ReadFull(random, buf[:psLen-len(ps)]); err != nil {
			return nil, err
		}
		for _, b := range buf[:psLen-len(ps)] {
			if b != 0 {
				ps = append(ps, b)
			}
		}
	}
	em := make([]byte, 0, k)
	em = append(em, 0x00, 0x02)
	em = append(em, ps...)
	em = append(em, 0x00)
	em = append(em, plain...)
	m := new(big.Int).SetBytes(em)
	c := new(big.Int).Exp(m, e, n)
	out := c.FillBytes(make([]byte, k))
	return out, nil
}

// Identity 构建 COSY 会话所需的账号身份字段（对应 qoder_sign.py L546-547
// 的 CosySession 构造参数）。
type Identity struct {
	UID              string
	Nickname         string
	AccessToken      string
	RefreshToken     string
	UserType         string
	OrganizationID   string
	OrganizationName string
}

// CosySession 单账号的 COSY 签名会话（qoder_sign.py L539-623）。
// machine 系列字段按账号 UID 稳定派生（同一账号永远来自同一台虚拟设备）；
// tempKey/cosyKey/info 在会话创建时生成，access token 轮换后整体重建。
type CosySession struct {
	UID             string
	MachineID       string
	MachineToken    string
	MachineType     string
	MachineHostname string
	AccessToken     string
	// UserType 透传给上游请求体的 aliyun_user_type（build_qoder_body）。
	UserType string

	// tempKey 为 16 个 ASCII hex 字符，整体作为 UTF-8 字节充当 AES-128 的
	// key/iv（key == iv）。已知熵 64 bit 属协议层限制（服务端以官方 RSA
	// 私钥解包后必须逐字节一致），不得单方面加固，见源文件风险标注。
	tempKey []byte
	// CosyKey = base64(RSA_PKCS1v15(tempKey))。
	CosyKey string
	// Info = base64(AES-CBC(identity_json_sorted_compact, key=iv=tempKey))。
	Info string
}

// NewCosySession 创建会话并完成 cosyKey/info 的预计算。
func NewCosySession(id Identity) (*CosySession, error) {
	if id.AccessToken == "" {
		return nil, fmt.Errorf("qoder: cosy session requires access token")
	}
	uid := id.UID
	userType := id.UserType
	if userType == "" {
		userType = DefaultUserType
	}
	s := &CosySession{
		UID:             uid,
		MachineID:       DeriveID(uid, "machine"),
		MachineToken:    DeriveMachineToken(uid),
		MachineType:     DeriveMachineType(uid),
		MachineHostname: DeriveMachineHostname(uid),
		AccessToken:     id.AccessToken,
		UserType:        userType,
	}
	// 对齐 qoder_sign.py L555 uuid4().hex[:16] 的形态：16 个十六进制字符。
	buf := make([]byte, 8)
	if _, err := io.ReadFull(randomReader, buf); err != nil {
		return nil, err
	}
	s.tempKey = []byte(hex.EncodeToString(buf))
	wrapped, err := rsaWrapTempKey(randomReader, s.tempKey)
	if err != nil {
		return nil, fmt.Errorf("qoder: rsa wrap temp key: %w", err)
	}
	s.CosyKey = base64.StdEncoding.EncodeToString(wrapped)

	// 身份体 9 键（qoder_sign.py L561-571，唯一真源；增删键必须同步服务端预期）。
	identity := map[string]string{
		"name":                 id.Nickname,
		"aid":                  uid,
		"uid":                  uid,
		"yx_uid":               "",
		"organization_id":      id.OrganizationID,
		"organization_name":    id.OrganizationName,
		"user_type":            userType,
		"security_oauth_token": id.AccessToken,
		"refresh_token":        id.RefreshToken,
	}
	plain := SortedCompactJSON(identity)
	ct, err := aesCBCEncrypt(plain, s.tempKey, s.tempKey)
	if err != nil {
		return nil, err
	}
	s.Info = base64.StdEncoding.EncodeToString(ct)
	return s, nil
}

// cosyPath 取 URL path 并剥 "/algo" 前缀（qoder_sign.py L580-582）。
func cosyPath(rawURL string) string {
	p := "/"
	if u, err := url.Parse(rawURL); err == nil && u.Path != "" {
		p = u.Path
	}
	return strings.TrimPrefix(p, "/algo")
}

// Bearer 生成一次推理/模型接口调用的签名三元组（qoder_sign.py L578-594）。
// date 为 unix 秒的十进制字符串；requestId 为随机 uuid4。
func (s *CosySession) Bearer(body, rawURL string) (payloadB64, date, bearer string) {
	date = strconv.FormatInt(time.Now().Unix(), 10)
	payloadB64, bearer = s.Sign(body, cosyPath(rawURL), date, UUID4())
	return
}

// Sign 签名确定性核心（黄金向量测试入口）：
//
//	payload = sorted-compact {cosyVersion, ideVersion:"", info, requestId, version:"v1"}
//	sig     = md5hex(payload_b64 + "\n" + cosy_key + "\n" + date + "\n" + body + "\n" + path)
//	bearer  = "Bearer COSY." + payload_b64 + "." + sig
func (s *CosySession) Sign(body, path, date, requestID string) (payloadB64, bearer string) {
	payload := map[string]string{
		"cosyVersion": COSYVersion,
		"ideVersion":  "",
		"info":        s.Info,
		"requestId":   requestID,
		"version":     "v1",
	}
	payloadB64 = base64.StdEncoding.EncodeToString(SortedCompactJSON(payload))
	raw := strings.Join([]string{payloadB64, s.CosyKey, date, body, path}, "\n")
	sig := fmt.Sprintf("%x", md5.Sum([]byte(raw)))
	return payloadB64, "Bearer COSY." + payloadB64 + "." + sig
}

// Headers 构造一次推理/模型接口的完整 COSY 签名头（qoder_sign.py L596-623）。
// accept 传空串时默认 "text/event-stream"。
func (s *CosySession) Headers(body, rawURL, modelKey string, sse bool, accept string) http.Header {
	if accept == "" {
		accept = "text/event-stream"
	}
	_, date, bearer := s.Bearer(body, rawURL)
	h := http.Header{}
	h.Set("cosy-data-policy", "AGREE")
	h.Set("content-type", "application/json")
	h.Set("cosy-machinetype", s.MachineType)
	h.Set("cosy-clienttype", "5")
	h.Set("cosy-date", date)
	h.Set("cosy-user", s.UID)
	h.Set("cosy-key", s.CosyKey)
	h.Set("accept", accept)
	h.Set("cosy-clientip", "169.254.198.161")
	h.Set("authorization", bearer)
	h.Set("accept-encoding", "identity")
	h.Set("cosy-version", COSYVersion)
	h.Set("cosy-machineid", s.MachineID)
	h.Set("cosy-machinetoken", s.MachineToken)
	h.Set("login-version", "v2")
	h.Set("user-agent", "Go-http-client/2.0")
	if sse {
		h.Set("cache-control", "no-cache")
		if cosyOSHeadersEnabled() {
			h.Set("cosy-machineos", cosyMachineOS())
			h.Set("cosy-machinehostname", s.MachineHostname)
		}
	}
	if modelKey != "" {
		h.Set("x-model-key", modelKey)
		h.Set("x-model-source", "system")
	}
	return h
}

type cosyCacheEntry struct {
	token string
	sess  *CosySession
}

// CosySessionCache 账号 UID -> CosySession 的进程级缓存；
// access token 变化即失效重建（qoder_sign.py L626-658）。
type CosySessionCache struct {
	mu      sync.Mutex
	entries map[string]cosyCacheEntry
}

// NewCosySessionCache 创建空缓存。
func NewCosySessionCache() *CosySessionCache {
	return &CosySessionCache{entries: map[string]cosyCacheEntry{}}
}

// Get 取或建会话；UID 为空时以 token 前 16 字符作缓存键。
func (c *CosySessionCache) Get(id Identity) (*CosySession, error) {
	token := id.AccessToken
	if token == "" {
		return nil, fmt.Errorf("qoder: cosy: empty access token")
	}
	key := id.UID
	if key == "" {
		key = token[:min(16, len(token))]
	}
	c.mu.Lock()
	hit, ok := c.entries[key]
	c.mu.Unlock()
	if ok && hit.token == token {
		return hit.sess, nil
	}
	sess, err := NewCosySession(id)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.entries[key] = cosyCacheEntry{token: token, sess: sess}
	c.mu.Unlock()
	return sess, nil
}

// Invalidate 丢弃指定 UID 的缓存会话（token 刷新后必须调用，
// 旧 COSY 会话携带旧 token）。
func (c *CosySessionCache) Invalidate(uid string) {
	c.mu.Lock()
	delete(c.entries, uid)
	c.mu.Unlock()
}

// UUID4 生成随机 UUID v4 的标准小写带横线文本形式。
func UUID4() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("qoder: rand failed: " + err.Error())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return formatUUID(b[:])
}

// UUID4Hex 生成无横线的 32 位十六进制形式（Python uuid4().hex）。
func UUID4Hex() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("qoder: rand failed: " + err.Error())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return hex.EncodeToString(b[:])
}

func formatUUID(b []byte) string {
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
