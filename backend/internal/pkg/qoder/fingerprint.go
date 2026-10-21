package qoder

import (
	"crypto/md5"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/google/uuid"
)

// 本文件移植自 qoder_fingerprint.py：由账号 UID + 加盐哈希单向派生固定的
// 伪物理设备特征，保证同一账号长期来自同一台"虚拟设备"，多账号之间天然
// 隔离，避免随机机器码漂移触发上游风控。
//
// 2026-10-20 对齐官方形态（issue #61）：machineId / sessionId 由 32 位无连字符
// hex 改为带连字符的 UUID 形状（版本位 4、变体位 a），request id 改为每请求
// 纯随机 32 位 hex（不再掺 uid 派生前缀）。QD_DEVICE_ID_LEGACY=1 可保留旧形态
// 以平滑存量号池（对上游等于换设备）。

// pyWhitespace 与 Python str.strip()/lstrip() 的默认空白集保持一致。
const pyWhitespace = " \t\n\r\v\f"

// deviceIDLegacy 报告是否保留旧的 32 位无连字符 hex 形态（QD_DEVICE_ID_LEGACY）。
func deviceIDLegacy() bool {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv("QD_DEVICE_ID_LEGACY")))
	switch raw {
	case "1", "on", "true", "yes", "enable", "enabled":
		return true
	}
	return false
}

// cosyOSHeadersEnabled 报告推理头里是否带 cosy-machineos/hostname（QD_COSY_OS_HEADERS，默认开）。
func cosyOSHeadersEnabled() bool {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv("QD_COSY_OS_HEADERS")))
	switch raw {
	case "0", "off", "false", "no", "disable", "disabled":
		return false
	}
	return true
}

// cosyMachineOS 返回 cosy-machineos 取值（QD_COSY_MACHINE_OS 或按平台默认）。
func cosyMachineOS() string {
	if v := strings.TrimSpace(os.Getenv("QD_COSY_MACHINE_OS")); v != "" {
		return v
	}
	if runtime.GOOS == "windows" {
		return "x86_64_win32"
	}
	return "x86_64_linux"
}

// DeriveID 由 uid + salt 稳定派生设备/会话标识（qoder_fingerprint.py L20-33）。
// 默认输出带连字符的 UUID 形状；QD_DEVICE_ID_LEGACY=1 时保留 32 位无连字符 hex。
func DeriveID(uid, salt string) string {
	if uid == "" {
		uid = "anonymous"
	}
	seed := salt + ":" + uid
	sum := md5.Sum([]byte(seed))
	h := hex.EncodeToString(sum[:])
	if deviceIDLegacy() {
		return h
	}
	h = h[:12] + "4" + h[13:16] + "a" + h[17:]
	return fmt.Sprintf("%s-%s-%s-%s-%s", h[0:8], h[8:12], h[12:16], h[16:20], h[20:32])
}

// GenerateRequestID 生成 X-Request-ID：32 位无连字符 hex，每请求纯随机
// （qoder_fingerprint.py L36-40，issue #61）。
func GenerateRequestID(uid string) string {
	_ = uid
	id := uuid.New()
	b := id[:]
	out := make([]byte, len(b)*2)
	hex.Encode(out, b)
	return string(out)
}

// DeriveMachineType 稳定派生 18 位 machine_type（qoder_fingerprint.py L75-77）：
// derive_id(...) 去横线后取前 18。
func DeriveMachineType(uid string) string {
	return strings.ReplaceAll(DeriveID(uid, "machinetype"), "-", "")[:18]
}

// DeriveMachineHostname 按账号稳定派生主机名（形如 DESKTOP-1A2B3C4），
// 供 infer-SSE 的 cosy-machinehostname 头使用（issue #64）。
func DeriveMachineHostname(uid string) string {
	if uid == "" {
		uid = "anonymous"
	}
	sum := md5.Sum([]byte("hostname:" + uid))
	return "DESKTOP-" + strings.ToUpper(hex.EncodeToString(sum[:])[:7])
}

// DeriveMachineToken 稳定派生 machine_token（qoder_fingerprint.py L48-52）：
// base64url(sha512("machinetoken:"+uid)) 去填充后取前 43 字符
// （64 字节摘要 → 86 字符无填充 base64，43 为源实现截断长度）。
func DeriveMachineToken(uid string) string {
	sum := sha512.Sum512([]byte("machinetoken:" + uid))
	return base64.RawURLEncoding.EncodeToString(sum[:])[:43]
}
