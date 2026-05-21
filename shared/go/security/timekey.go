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

const TimeWindowSeconds int64 = 30

func GenerateTimeKey(secret string, at time.Time) string {
	window := at.Unix() / TimeWindowSeconds
	return hmacSM3([]byte(secret), []byte(fmt.Sprintf("%d", window)))
}

func ValidateTimeKey(secret, provided string, timestamp int64, now time.Time, toleranceWindows int64) (bool, bool) {
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

func HashPassword(password string) (string, error) {
	salt, err := NewToken(16)
	if err != nil {
		return "", err
	}
	return "sm3:" + salt + "$" + hmacSM3([]byte(salt), []byte(password)), nil
}

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

func hmacSM3(key, data []byte) string {
	mac := hmac.New(sm3.New, key)
	_, _ = mac.Write(data)
	return hex.EncodeToString(mac.Sum(nil))
}
