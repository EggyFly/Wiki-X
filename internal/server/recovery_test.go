package server

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/EggyFly/Wiki-X/internal/auth"
)

var recoveryCodePattern = regexp.MustCompile(`<code class="recovery-code">([A-Z0-9-]+)</code>`)

func TestRecoveryCodesFlow(t *testing.T) {
	baseURL, data, _ := setupCommunityApp(t)
	admin := loginTestClient(t, baseURL, "admin", "admin-pass-123")
	enableTOTP(t, admin, baseURL, "admin")
	user, err := data.UserByName(context.Background(), "admin")
	if err != nil {
		t.Fatal(err)
	}

	csrf := fetchCSRFToken(t, admin, baseURL+"/account")
	wrong := postForm(t, admin, baseURL+"/account/recovery/generate", url.Values{
		"csrf_token": {csrf},
		"password":   {"wrong-password"},
	})
	if wrong.StatusCode != http.StatusBadRequest {
		t.Fatalf("wrong password status = %d", wrong.StatusCode)
	}
	wrong.Body.Close()

	resp := postForm(t, admin, baseURL+"/account/recovery/generate", url.Values{
		"csrf_token": {csrf},
		"password":   {"admin-pass-123"},
	})
	body := readResponse(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("generate status = %d", resp.StatusCode)
	}
	matches := recoveryCodePattern.FindAllStringSubmatch(body, -1)
	if len(matches) != auth.RecoveryCodeCount {
		t.Fatalf("codes shown = %d", len(matches))
	}
	count, err := data.CountRecoveryCodes(context.Background(), user.ID)
	if err != nil || count != auth.RecoveryCodeCount {
		t.Fatalf("stored codes = %d, %v", count, err)
	}
	code := matches[0][1]

	client := testHTTPClient()
	loginCSRF := fetchCSRFToken(t, client, baseURL+"/login")
	login := postForm(t, client, baseURL+"/login", url.Values{
		"csrf_token":    {loginCSRF},
		"username":      {"admin"},
		"password":      {"admin-pass-123"},
		"recovery_code": {strings.ToLower(code)},
		"next":          {"/"},
	})
	if login.StatusCode != http.StatusSeeOther {
		t.Fatalf("recovery login status = %d", login.StatusCode)
	}
	login.Body.Close()

	replay := testHTTPClient()
	replayCSRF := fetchCSRFToken(t, replay, baseURL+"/login")
	again := postForm(t, replay, baseURL+"/login", url.Values{
		"csrf_token":    {replayCSRF},
		"username":      {"admin"},
		"password":      {"admin-pass-123"},
		"recovery_code": {code},
		"next":          {"/"},
	})
	if again.StatusCode != http.StatusUnauthorized {
		t.Fatalf("reused recovery code status = %d", again.StatusCode)
	}
	again.Body.Close()
	count, err = data.CountRecoveryCodes(context.Background(), user.ID)
	if err != nil || count != auth.RecoveryCodeCount-1 {
		t.Fatalf("codes after use = %d, %v", count, err)
	}
	for _, action := range []string{"user.recovery_codes_generate", "user.recovery_code_use"} {
		if ok, err := data.HasAudit(context.Background(), action, "admin"); err != nil || !ok {
			t.Fatalf("%s audit = %v, %v", action, ok, err)
		}
	}
}

func TestTOTPQRCode(t *testing.T) {
	baseURL, _, _ := setupCommunityApp(t)
	admin := loginTestClient(t, baseURL, "admin", "admin-pass-123")
	csrf := fetchCSRFToken(t, admin, baseURL+"/account")
	setup := postForm(t, admin, baseURL+"/account/totp/setup", url.Values{"csrf_token": {csrf}})
	if setup.StatusCode != http.StatusSeeOther {
		t.Fatalf("setup status = %d", setup.StatusCode)
	}
	setup.Body.Close()

	resp := mustGet(t, admin, baseURL+"/account/totp/qr")
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("qr status = %d, type = %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	png := readResponse(t, resp)
	if !strings.HasPrefix(png, "\x89PNG\r\n\x1a\n") {
		t.Fatal("qr response is not a png")
	}

	body := readResponse(t, mustGet(t, admin, baseURL+"/account"))
	match := totpSecretPattern.FindStringSubmatch(body)
	if match == nil {
		t.Fatal("secret missing")
	}
	enable := postForm(t, admin, baseURL+"/account/totp/enable", url.Values{
		"csrf_token": {csrf},
		"totp_code":  {totpCodeAt(t, match[1], 0)},
	})
	if enable.StatusCode != http.StatusSeeOther {
		t.Fatalf("enable status = %d", enable.StatusCode)
	}
	enable.Body.Close()
	gone := mustGet(t, admin, baseURL+"/account/totp/qr")
	if gone.StatusCode != http.StatusNotFound {
		t.Fatalf("qr after enable = %d", gone.StatusCode)
	}
	gone.Body.Close()

	anonymous := mustGet(t, testHTTPClient(), baseURL+"/account/totp/qr")
	if anonymous.StatusCode != http.StatusSeeOther {
		t.Fatalf("anonymous qr = %d", anonymous.StatusCode)
	}
	anonymous.Body.Close()
}
