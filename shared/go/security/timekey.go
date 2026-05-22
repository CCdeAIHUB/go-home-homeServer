package security

import (
	"crypto/hmac"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/tjfoc/gmsm/sm3"
)

// TimeWindowSeconds 是 HMAC-SM3 时间窗口的长度（30 秒）。
// time_key 基于当前时间窗口生成，客户端和服务器在 ±2 个窗口内（共 5 个窗口，150 秒）接受。
const TimeWindowSeconds int64 = 30

// GenerateTimeKey 生成基于 SM3-HMAC 的时间窗口密钥。
// 算法：HMAC-SM3(secret, strconv.FormatInt(window))，其中 window = at.Unix() / 30。
// 用于 device.auth 和 ping 请求的防重放校验。
func GenerateTimeKey(secret string, at time.Time) string {
	window := at.Unix() / TimeWindowSeconds
	return hmacSM3([]byte(secret), []byte(fmt.Sprintf("%d", window)))
}

// ValidateTimeKey 验证提供的 time_key 是否有效。
//
// 参数：
//   - secret: 共享密钥（设备使用 auth_code）
//   - provided: 客户端提供的 time_key
//   - timestamp: 客户端发送时的时间戳（秒）
//   - now: 服务器当前时间
//   - toleranceWindows: 允许的时间窗口偏差数（通常为 2）
//
// 返回值：
//   - valid: time_key 是否验证通过
//   - clockSkew: 客户端时钟偏差是否过大（即使 key 不匹配）
//
// 验证逻辑：在 [clientWindow - tolerance, clientWindow + tolerance] 范围内
// 逐一检查是否有匹配的 HMAC 值。如果 clientWindow 与 serverWindow 相差
// 超过 toleranceWindows，则认为时钟偏差过大。
func ValidateTimeKey(secret, provided string, timestamp int64, now time.Time, toleranceWindows int64) (valid bool, clockSkew bool) {
	if provided == "" || timestamp == 0 {
		return false, false
	}
	clientWindow := timestamp / TimeWindowSeconds
	serverWindow := now.Unix() / TimeWindowSeconds
	skewTooLarge := int64(math.Abs(float64(clientWindow-serverWindow))) > toleranceWindows
	if skewTooLarge {
		return false, true
	}
	for offset := -toleranceWindows; offset <= toleranceWindows; offset++ {
		expected := hmacSM3([]byte(secret), []byte(fmt.Sprintf("%d", clientWindow+offset)))
		if hmac.Equal([]byte(expected), []byte(provided)) {
			return true, false
		}
	}
	return false, false
}

// NewToken 生成密码学安全的随机十六进制令牌。
// bytes 指定随机字节数（输出为 2*bytes 个 hex 字符）。
// 用于生成会话令牌、probe_id、session_id 等。
func NewToken(bytes int) (string, error) {
	if bytes <= 0 {
		return "", errors.New("token length must be positive")
	}
	buf := make([]byte, bytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// HashPassword 使用加盐 SM3-HMAC 创建密码哈希。
// 格式："sm3:" + salt + "$" + HMAC-SM3(salt, password)
// salt 为 16 字节随机值的 hex 编码（32 字符）。
func HashPassword(password string) (string, error) {
	salt, err := NewToken(16)
	if err != nil {
		return "", err
	}
	return "sm3:" + salt + "$" + hmacSM3([]byte(salt), []byte(password)), nil
}

// VerifyPassword 验证明文密码是否与存储的哈希匹配。
// stored 格式必须为 HashPassword 的输出格式："sm3:salt$hash"。
// 如果格式不正确或密码不匹配，返回 false。
func VerifyPassword(password, stored string) bool {
	const prefix = "sm3:"
	if len(stored) <= len(prefix) || stored[:len(prefix)] != prefix {
		return false
	}
	body := stored[len(prefix):]
	idx := -1
	for i := range body {
		if body[i] == '$' {
			idx = i
			break
		}
	}
	if idx <= 0 || idx == len(body)-1 {
		return false
	}
	salt := body[:idx]
	hash := body[idx+1:]
	return hmac.Equal([]byte(hash), []byte(hmacSM3([]byte(salt), []byte(password))))
}

// hmacSM3 计算 HMAC-SM3 并返回十六进制编码的结果。
func hmacSM3(key, data []byte) string {
	mac := hmac.New(sm3.New, key)
	// hmac.Write 永远不会返回错误
	_, _ = mac.Write(data)
	return hex.EncodeToString(mac.Sum(nil))
}
