package auth

import (
	"crypto/rand"
	"encoding/base32"
	"strings"
)

const RecoveryCodeCount = 10

func GenerateRecoveryCodes(count int) ([]string, error) {
	if count <= 0 || count > 50 {
		count = RecoveryCodeCount
	}
	codes := make([]string, 0, count)
	for i := 0; i < count; i++ {
		raw := make([]byte, 10)
		if _, err := rand.Read(raw); err != nil {
			return nil, err
		}
		codes = append(codes, base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw))
	}
	return codes, nil
}

func FormatRecoveryCode(code string) string {
	code = NormalizeRecoveryCode(code)
	parts := make([]string, 0, 4)
	for i := 0; i < len(code); i += 4 {
		parts = append(parts, code[i:i+4])
	}
	return strings.Join(parts, "-")
}

func NormalizeRecoveryCode(raw string) string {
	raw = strings.ToUpper(strings.TrimSpace(raw))
	raw = strings.ReplaceAll(raw, "-", "")
	raw = strings.ReplaceAll(raw, " ", "")
	return raw
}

func HashRecoveryCode(code string) string {
	return hashToken(NormalizeRecoveryCode(code))
}
