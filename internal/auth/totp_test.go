package auth

import (
	"encoding/base32"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestTOTPRFCVector(t *testing.T) {
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte("12345678901234567890"))
	step, ok := ValidateTOTPAt(secret, "287082", time.Unix(59, 0))
	if !ok {
		t.Fatal("RFC 6238 SHA-1 vector failed at t=59")
	}
	if step != 1 {
		t.Fatalf("step = %d, want 1", step)
	}
}

func TestTOTPWindow(t *testing.T) {
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte("12345678901234567890"))
	now := time.Unix(59, 0)
	if _, ok := ValidateTOTPAt(secret, "287082", now.Add(29*time.Second)); !ok {
		t.Fatal("code within window rejected")
	}
	if _, ok := ValidateTOTPAt(secret, "287082", now.Add(91*time.Second)); ok {
		t.Fatal("code outside window accepted")
	}
}

func TestTOTPFormat(t *testing.T) {
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte("12345678901234567890"))
	bad := []string{"", "12345", "abcdef", "1234567", "１２３４５６"}
	for _, code := range bad {
		if _, ok := ValidateTOTP(secret, code); ok {
			t.Fatalf("invalid code accepted: %q", code)
		}
	}
}

func TestTOTPRejectsWeakSecret(t *testing.T) {
	short := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte("1234567890"))
	for _, secret := range []string{"", "  ", "AAAA", short, "!!!invalid!!!"} {
		if _, ok := ValidateTOTPAt(secret, "287082", time.Unix(59, 0)); ok {
			t.Fatalf("weak secret accepted: %q", secret)
		}
		if _, err := TOTPCode(secret, time.Unix(59, 0)); err == nil {
			t.Fatalf("weak secret produced a code: %q", secret)
		}
	}
}

func TestGenerateTOTPSecret(t *testing.T) {
	secret, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	if len(secret) != 32 {
		t.Fatalf("secret length = %d", len(secret))
	}
	if strings.ContainsAny(secret, "0189") {
		t.Fatalf("base32 secret contains ambiguous chars: %q", secret)
	}
	if _, err := TOTPCode(secret, time.Now()); err != nil {
		t.Fatalf("generated secret undecodable: %v", err)
	}
}

func TestOTPAuthURL(t *testing.T) {
	link := OTPAuthURL("Wiki-X & Co", "admin@example.com", "ABC234DEF")
	if !strings.HasPrefix(link, "otpauth://totp/") {
		t.Fatalf("otpauth url = %q", link)
	}
	label, query, found := strings.Cut(strings.TrimPrefix(link, "otpauth://totp/"), "?")
	if !found {
		t.Fatalf("otpauth url has no query: %q", link)
	}
	if strings.ContainsAny(label, "?#") {
		t.Fatalf("label can truncate the otpauth link: %q", label)
	}
	values, err := url.ParseQuery(query)
	if err != nil {
		t.Fatal(err)
	}
	if values.Get("secret") != "ABC234DEF" || values.Get("issuer") != "Wiki-X & Co" {
		t.Fatalf("otpauth query = %q", query)
	}
	if values.Get("algorithm") != "SHA1" || values.Get("digits") != "6" || values.Get("period") != "30" {
		t.Fatalf("otpauth parameters wrong: %q", query)
	}
}
