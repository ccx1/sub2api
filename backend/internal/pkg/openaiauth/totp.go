package openaiauth

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"strings"
	"time"
)

// TOTP 计算一个标准的 6 位 RFC 6238 TOTP（30 秒步长，SHA-1），
// 与 customer_recovery.totp 行为一致：忽略空白、大写、按需补齐 base32 填充。
func TOTP(secret string) (string, error) {
	return totpAt(secret, time.Now().Unix())
}

func totpAt(secret string, unix int64) (string, error) {
	cleaned := strings.ToUpper(strings.Join(strings.Fields(secret), ""))
	if cleaned == "" {
		return "", errors.New("invalid totp secret")
	}
	if pad := len(cleaned) % 8; pad != 0 {
		cleaned += strings.Repeat("=", 8-pad)
	}
	key, err := base32.StdEncoding.DecodeString(cleaned)
	if err != nil {
		return "", errors.New("invalid totp secret")
	}
	counter := make([]byte, 8)
	binary.BigEndian.PutUint64(counter, uint64(unix/30))
	mac := hmac.New(sha1.New, key)
	mac.Write(counter)
	digest := mac.Sum(nil)
	offset := digest[len(digest)-1] & 0x0f
	code := (binary.BigEndian.Uint32(digest[offset:offset+4]) & 0x7fffffff) % 1000000
	out := make([]byte, 6)
	for i := 5; i >= 0; i-- {
		out[i] = byte('0' + code%10)
		code /= 10
	}
	return string(out), nil
}

// ValidateTOTPSecret 仅校验密钥是否可用于生成 TOTP，不返回验证码。
func ValidateTOTPSecret(secret string) error {
	_, err := TOTP(secret)
	return err
}
