package qoder

import (
	"crypto/md5"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"time"
)

// 本文件移植自 qoder_fingerprint.py：由账号 UID + 加盐哈希单向派生固定的
// 伪物理设备特征，保证同一账号长期来自同一台"虚拟设备"，多账号之间天然
// 隔离，避免随机机器码漂移触发上游风控。
//
// 与源实现的差异（源模块另有本机虚拟化检测 / 桌面指纹上报，属于看板与
// 活动上报能力，不在本包范围内）：
//   - 源实现偶有"优先读取本机官方客户端真实 machine_id"的分支（服务器
//     场景不存在该客户端），本包一律走 uid 稳定派生；
//   - 派生输出形状（长度 / 字符集）与源逐字一致。

// pyWhitespace 与 Python str.strip()/lstrip() 的默认空白集保持一致。
const pyWhitespace = " \t\n\r\v\f"

// DeriveID 由 uid + salt 稳定派生 32 位十六进制设备/会话标识
// （qoder_fingerprint.py L20-33）。幂等：同一账号每次调用产生相同值。
// md5 hexdigest 恒为 32 个十六进制字符，源实现 [:36] 是防御性恒等切片。
func DeriveID(uid, salt string) string {
	if uid == "" {
		uid = "anonymous"
	}
	seed := salt + ":" + uid
	sum := md5.Sum([]byte(seed))
	out := hex.EncodeToString(sum[:])
	if len(out) > 36 {
		out = out[:36] // 防御性保留（对 32 位 hex 恒不触发）
	}
	return out
}

// GenerateRequestID 生成带稳定前缀与微秒时间戳的防风控请求 ID
// （qoder_fingerprint.py L36-40）。
func GenerateRequestID(uid string) string {
	prefix := DeriveID(uid, "req")
	suffix := fmt.Sprintf("%06d", time.Now().UnixNano()%1_000_000)
	return prefix + "-" + suffix
}

// DeriveMachineType 稳定派生 18 位 machine_type（qoder_fingerprint.py L43-45）。
// md5 hex 不含 '-'，replace 为防御性保留；输出恒为 18 个十六进制字符。
func DeriveMachineType(uid string) string {
	return DeriveID(uid, "machinetype")[:18]
}

// DeriveMachineToken 稳定派生 machine_token（qoder_fingerprint.py L48-52）：
// base64url(sha512("machinetoken:"+uid)) 去填充后取前 43 字符
// （64 字节摘要 → 86 字符无填充 base64，43 为源实现截断长度）。
func DeriveMachineToken(uid string) string {
	sum := sha512.Sum512([]byte("machinetoken:" + uid))
	return base64.RawURLEncoding.EncodeToString(sum[:])[:43]
}
