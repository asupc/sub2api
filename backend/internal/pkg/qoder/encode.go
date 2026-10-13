// Package qoder 移植自 qoder2api-hub（MIT，Python 3.9+ 零依赖实现），
// 覆盖阿里 Qoder 双区（cn / intl）网关所需的私有 COSY 签名协议、
// OAuth 设备授权 / PAT 导入 / token 刷新、模型目录、请求体构造与
// 私有 SSE 信封 → OpenAI chat.completions 的流式转换。
//
// 协议常量（RSA 公钥、自定义 Base64 字母表、COSY 版本号、端点等）
// 均逐字提取自 qoder2api-hub 源码并标注来源行号，禁止凭记忆改写。
package qoder

import (
	"encoding/base64"
	"fmt"
)

// 自定义 Base64 变体字母表（qoder_sign.py L63-65 逐字提取；
// 填充符 '=' -> '$'）。标准字母表与 base64.StdEncoding 一致。
const (
	qoderCustomAlphabet = "_doRTgHZBKcGVjlvpC,@aFSx#DPuNJme&i*MzLOEn)sUrthbf%Y^w.(kIQyXqWA!"
	qoderStdAlphabet    = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	qoderPad            = '$'
)

var (
	// encodeCustom 把标准 Base64 字符（含 '='）映射为 Qoder 变体字符。
	encodeCustom [256]byte
	// decodeCustom 为 encodeCustom 的逆映射；0 值表示非法字符。
	decodeCustom [256]byte
)

func init() {
	if len(qoderCustomAlphabet) != 64 {
		panic("qoder: bad custom alphabet length")
	}
	seen := map[byte]bool{}
	for i := 0; i < 64; i++ {
		c := qoderCustomAlphabet[i]
		if seen[c] {
			panic("qoder: duplicate char in custom alphabet")
		}
		seen[c] = true
		encodeCustom[qoderStdAlphabet[i]] = c
		decodeCustom[c] = qoderStdAlphabet[i]
	}
	encodeCustom['='] = qoderPad
	decodeCustom[qoderPad] = '='
}

// QoderEncode 把明文编码成 Qoder 自定义 Base64 变体（qoder_sign.py L75-81）：
//
//	std = standard_base64(plain)
//	a   = len(std) // 3
//	rearranged = std[n-a:] + std[a:n-a] + std[:a]   # 尾/中/首 三段轮转
//	逐字符按自定义字母表映射，'=' -> '$'
func QoderEncode(plain []byte) string {
	std := base64.StdEncoding.EncodeToString(plain)
	n := len(std)
	a := n / 3
	out := make([]byte, 0, n)
	out = append(out, std[n-a:]...)
	out = append(out, std[a:n-a]...)
	out = append(out, std[:a]...)
	for i := range out {
		out[i] = encodeCustom[out[i]]
	}
	return string(out)
}

// QoderDecode 是 QoderEncode 的逆运算（qoder_sign.py L84-95）。
// 正向: R = S[n-a:] + S[a:n-a] + S[:a]（尾/中/首 三段），逆向: S = R3 + R2 + R1。
func QoderDecode(encoded string) ([]byte, error) {
	std := make([]byte, len(encoded))
	for i := 0; i < len(encoded); i++ {
		c := decodeCustom[encoded[i]]
		if c == 0 {
			return nil, fmt.Errorf("qoder: invalid character %q in encoded body at %d", encoded[i], i)
		}
		std[i] = c
	}
	n := len(std)
	a := n / 3
	original := make([]byte, 0, n)
	original = append(original, std[n-a:]...)
	original = append(original, std[a:n-a]...)
	original = append(original, std[:a]...)
	return base64.StdEncoding.DecodeString(string(original))
}
