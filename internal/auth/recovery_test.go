package auth

import (
	"strings"
	"testing"
)

func TestRecoveryCodes(t *testing.T) {
	codes, err := GenerateRecoveryCodes(3)
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != 3 {
		t.Fatalf("codes = %d", len(codes))
	}
	for _, code := range codes {
		if len(code) != 16 {
			t.Fatalf("code length = %d, code = %q", len(code), code)
		}
		formatted := FormatRecoveryCode(code)
		if strings.Count(formatted, "-") != 3 {
			t.Fatalf("formatted = %q", formatted)
		}
		if NormalizeRecoveryCode(formatted) != code {
			t.Fatalf("normalize(%q) != %q", formatted, code)
		}
		if HashRecoveryCode(formatted) != HashRecoveryCode(code) {
			t.Fatal("formatted and raw codes should hash the same")
		}
	}
	first, second, err := func() (string, string, error) {
		a, err := GenerateRecoveryCodes(1)
		if err != nil {
			return "", "", err
		}
		b, err := GenerateRecoveryCodes(1)
		if err != nil {
			return "", "", err
		}
		return a[0], b[0], nil
	}()
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("recovery codes should be random")
	}
}
