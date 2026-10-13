package trae

import (
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// JWTInfo Cloud-IDE-JWT 的解析结果。任何一步失败都退化为「未知」，绝不报错 panic
// （真正的鉴权由上游完成，本地不校验签名——密钥轮换会让本地校验误判）。
type JWTInfo struct {
	// UserID 用户 ID（payload 的 data.id → auth_id → sub 依次回落；空 = 未知）。
	UserID string
	// ExpTimestamp exp 的 Unix 秒；ExpValid 为 false 时无意义。
	ExpTimestamp int64
	// ExpValid 是否解析到了 exp。
	ExpValid bool
}

// ExpHours 剩余有效期（小时，可能为负表示已过期）。未解析到 exp 时第二个返回值为 false。
func (j JWTInfo) ExpHours(now time.Time) (float64, bool) {
	if !j.ExpValid {
		return 0, false
	}
	return float64(j.ExpTimestamp-now.Unix()) / 3600.0, true
}

// Status 到期状态：ok（>24h）/ warn（>0 且 ≤24h）/ expired / unknown。
// 阈值与源实现一致（TRAE_JWT_WARN_HOURS = 24.0）。
func (j JWTInfo) Status(now time.Time) string {
	hours, ok := j.ExpHours(now)
	switch {
	case !ok:
		return "unknown"
	case hours > 24.0:
		return "ok"
	case hours > 0.0:
		return "warn"
	default:
		return "expired"
	}
}

// jwtPrefix Trae 凭据头的固定前缀（VSCode 系）。
const jwtPrefix = "Cloud-IDE-JWT "

// NormalizeJWT 归一化 JWT：剥掉 Cloud-IDE-JWT / Bearer 前缀与首尾空白。
// 用户从浏览器或代理日志粘贴时经常带前缀或换行，这里统一收敛。
func NormalizeJWT(raw string) string {
	trimmed := strings.TrimSpace(raw)
	trimmed = strings.TrimPrefix(trimmed, "Cloud-IDE-JWT ")
	trimmed = strings.TrimPrefix(trimmed, "Bearer ")
	return strings.TrimSpace(trimmed)
}

// AuthorizationHeader 组装请求头所需的 Authorization 值：Cloud-IDE-JWT <token>。
// 已经是该前缀时原样返回，避免出现双前缀。
func AuthorizationHeader(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if strings.HasPrefix(trimmed, jwtPrefix) {
		return trimmed
	}
	return jwtPrefix + NormalizeJWT(trimmed)
}

// decodeJWTPayload 解析 JWT 的 payload 段（base64url，容忍填充符）。
func decodeJWTPayload(raw string) (map[string]any, bool) {
	token := NormalizeJWT(raw)
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return nil, false
	}
	// 先剥离填充符：payload 是 base64url 无填充编码，混入 `=` 会让 NO_PAD 解码失败。
	payloadB64 := strings.TrimRight(parts[1], "=")
	bytes, err := base64.RawURLEncoding.DecodeString(payloadB64)
	if err != nil {
		return nil, false
	}
	var payload map[string]any
	if err := json.Unmarshal(bytes, &payload); err != nil {
		return nil, false
	}
	return payload, true
}

// ParseJWT 解析 Trae JWT（不校验签名）。失败时返回零值（全部未知）。
func ParseJWT(raw string) JWTInfo {
	payload, ok := decodeJWTPayload(raw)
	if !ok {
		return JWTInfo{}
	}

	// user_id：data.id（字符串或数字）→ auth_id → sub 依次回落。
	info := JWTInfo{UserID: jwtUserID(payload)}

	// exp 在不同签发方下可能是整数、浮点或数字字符串，依次尝试。
	if exp, ok := jwtExp(payload); ok {
		info.ExpTimestamp = exp
		info.ExpValid = true
	}
	return info
}

// UserIDOf 从 JWT 取用户 ID（解析失败返回空串）。
func UserIDOf(raw string) string {
	return ParseJWT(raw).UserID
}

// jwtUserID 按「data.id → auth_id → sub」的顺序提取用户 ID。
func jwtUserID(payload map[string]any) string {
	if data, ok := payload["data"].(map[string]any); ok {
		if id, ok := jwtIDValue(data["id"]); ok {
			return id
		}
	}
	if id, ok := jwtIDValue(payload["auth_id"]); ok {
		return id
	}
	if id, ok := jwtIDValue(payload["sub"]); ok {
		return id
	}
	return ""
}

// jwtIDValue 把 id 值转成字符串（数字形态也接受）。
func jwtIDValue(v any) (string, bool) {
	switch value := v.(type) {
	case string:
		if value != "" {
			return value, true
		}
	case float64:
		return strconv.FormatInt(int64(value), 10), true
	}
	return "", false
}

// jwtExp 解析 exp 字段（整数 / 浮点 / 数字字符串）。
func jwtExp(payload map[string]any) (int64, bool) {
	switch value := payload["exp"].(type) {
	case float64:
		return int64(value), true
	case string:
		if n, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil {
			return n, true
		}
	}
	return 0, false
}
