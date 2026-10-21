package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const totpStep = 30 * time.Second
const totpSeconds = int64(totpStep / time.Second)

// 密钥太短会让 HMAC 输出可预测, 空密钥更会退化成所有人都能算出的固定口令
const minTOTPKeySize = 16

func GenerateTOTPSecret() (string, error) {
	buf := make([]byte, 20)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf), nil
}

func totpCodeAt(secret string, step int64) (string, error) {
	key, err := decodeSecret(secret)
	if err != nil {
		return "", err
	}
	if len(key) < minTOTPKeySize {
		return "", fmt.Errorf("totp key too short")
	}
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(step))
	mac := hmac.New(sha1.New, key)
	mac.Write(buf[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := (binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff) % 1000000
	return fmt.Sprintf("%06d", value), nil
}

func TOTPCode(secret string, t time.Time) (string, error) {
	return totpCodeAt(secret, t.Unix()/totpSeconds)
}

func TOTPStep(t time.Time) int64 {
	return t.Unix() / totpSeconds
}

func ValidateTOTP(secret, code string) (int64, bool) {
	return ValidateTOTPAt(secret, code, time.Now())
}

func ValidateTOTPAt(secret, code string, now time.Time) (int64, bool) {
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return 0, false
	}
	for _, r := range code {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	base := TOTPStep(now)
	for i := int64(-1); i <= 1; i++ {
		want, err := totpCodeAt(secret, base+i)
		if err == nil && subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			return base + i, true
		}
	}
	return 0, false
}

func OTPAuthURL(issuer, identifier, secret string) string {
	query := url.Values{}
	query.Set("secret", secret)
	query.Set("issuer", issuer)
	query.Set("algorithm", "SHA1")
	query.Set("digits", "6")
	query.Set("period", "30")
	return "otpauth://totp/" + url.PathEscape(issuer+":"+identifier) + "?" + query.Encode()
}

func decodeSecret(secret string) ([]byte, error) {
	clean := strings.ToUpper(strings.ReplaceAll(secret, " ", ""))
	return base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(clean)
}
