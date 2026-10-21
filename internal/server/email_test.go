package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/EggyFly/Wiki-X/internal/auth"
	"github.com/EggyFly/Wiki-X/internal/config"
	"github.com/EggyFly/Wiki-X/internal/store"
)

type fakeMail struct {
	To      string
	Subject string
	Body    string
}

type fakeMailer struct {
	mu    sync.Mutex
	mails []fakeMail
}

func (f *fakeMailer) Send(ctx context.Context, to, subject, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mails = append(f.mails, fakeMail{To: to, Subject: subject, Body: body})
	return nil
}

func (f *fakeMailer) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.mails)
}

func (f *fakeMailer) last() fakeMail {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.mails) == 0 {
		return fakeMail{}
	}
	return f.mails[len(f.mails)-1]
}

var mailTokenPattern = regexp.MustCompile(`token=([A-Za-z0-9_-]+)`)

func setupMailApp(t *testing.T) (string, *store.Store, *fakeMailer) {
	t.Helper()
	app, data, _ := setupCommunityFixture(t, config.GitHubConfig{})
	mailer := &fakeMailer{}
	app.mailer = mailer
	app.cfg.Site.BaseURL = "https://wiki.example"
	server := httptest.NewServer(app.Handler())
	t.Cleanup(server.Close)
	return server.URL, data, mailer
}

func createMailUser(t *testing.T, data *store.Store, name, email, password string) *store.User {
	t.Helper()
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	user, err := data.CreateUser(context.Background(), name, email, hash, false)
	if err != nil {
		t.Fatal(err)
	}
	return user
}

func TestEmailResetFlow(t *testing.T) {
	baseURL, data, mailer := setupMailApp(t)
	user := createMailUser(t, data, "resetuser", "reset@example.com", "old-pass-123")
	client := testHTTPClient()

	csrf := fetchCSRFToken(t, client, baseURL+"/forgot")
	resp := postForm(t, client, baseURL+"/forgot", url.Values{
		"csrf_token": {csrf},
		"email":      {"reset@example.com"},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("forgot status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	if mailer.count() != 0 {
		t.Fatal("unverified email must not receive a reset link")
	}

	if err := data.SetEmailVerified(context.Background(), user.ID, true); err != nil {
		t.Fatal(err)
	}
	resp = postForm(t, client, baseURL+"/forgot", url.Values{
		"csrf_token": {csrf},
		"email":      {"reset@example.com"},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("forgot status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	if mailer.count() != 1 {
		t.Fatalf("mails = %d", mailer.count())
	}
	mail := mailer.last()
	if mail.To != "reset@example.com" || !strings.Contains(mail.Subject, "密码") {
		t.Fatalf("mail = %+v", mail)
	}
	match := mailTokenPattern.FindStringSubmatch(mail.Body)
	if len(match) < 2 {
		t.Fatalf("token not found in mail body: %s", mail.Body)
	}
	token := match[1]

	resetPage := mustGet(t, client, baseURL+"/reset?token="+token)
	if resetPage.StatusCode != http.StatusOK {
		t.Fatalf("reset page = %d", resetPage.StatusCode)
	}
	resetPage.Body.Close()
	resetCSRF := fetchCSRFToken(t, client, baseURL+"/reset?token="+token)

	noCSRF := postForm(t, client, baseURL+"/reset", url.Values{
		"token":        {token},
		"new_password": {"new-pass-123"},
	})
	if noCSRF.StatusCode != http.StatusForbidden {
		t.Fatalf("reset without csrf = %d", noCSRF.StatusCode)
	}
	noCSRF.Body.Close()

	resp = postForm(t, client, baseURL+"/reset", url.Values{
		"csrf_token":       {resetCSRF},
		"token":            {token},
		"new_password":     {"new-pass-123"},
		"confirm_password": {"new-pass-123"},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("reset status = %d", resp.StatusCode)
	}
	if location := resp.Header.Get("Location"); !strings.Contains(location, "notice=password_reset") {
		t.Fatalf("reset location = %q", location)
	}
	resp.Body.Close()

	reuse := postForm(t, client, baseURL+"/reset", url.Values{
		"token":            {token},
		"new_password":     {"another-pass-123"},
		"confirm_password": {"another-pass-123"},
	})
	if reuse.StatusCode == http.StatusSeeOther {
		t.Fatal("reset token must be single use")
	}
	reuse.Body.Close()

	if got := loginStatus(t, baseURL, "resetuser", "old-pass-123"); got != http.StatusUnauthorized {
		t.Fatalf("old password login = %d", got)
	}
	if got := loginStatus(t, baseURL, "resetuser", "new-pass-123"); got != http.StatusSeeOther {
		t.Fatalf("new password login = %d", got)
	}
	if ok, err := data.HasAudit(context.Background(), "user.password_reset", "resetuser"); err != nil || !ok {
		t.Fatalf("password reset audit = %v, %v", ok, err)
	}
}

func TestEmailVerifyFlow(t *testing.T) {
	baseURL, data, mailer := setupMailApp(t)
	createMailUser(t, data, "verifyuser", "verify@example.com", "verify-pass-123")
	client := loginTestClient(t, baseURL, "verifyuser", "verify-pass-123")
	csrf := fetchCSRFToken(t, client, baseURL+"/account")

	resp := postForm(t, client, baseURL+"/account/email/resend", url.Values{
		"csrf_token": {csrf},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("resend status = %d", resp.StatusCode)
	}
	if location := resp.Header.Get("Location"); !strings.Contains(location, "notice=email_verify_sent") {
		t.Fatalf("resend location = %q", location)
	}
	resp.Body.Close()
	if mailer.count() != 1 {
		t.Fatalf("mails = %d", mailer.count())
	}
	match := mailTokenPattern.FindStringSubmatch(mailer.last().Body)
	if len(match) < 2 {
		t.Fatalf("token not found: %s", mailer.last().Body)
	}
	token := match[1]

	page := mustGet(t, client, baseURL+"/verify-email?token="+token)
	if page.StatusCode != http.StatusOK {
		t.Fatalf("verify page = %d", page.StatusCode)
	}
	body := readResponse(t, page)
	if !strings.Contains(body, "验证邮箱") {
		t.Fatal("verify page missing heading")
	}
	verifyCSRF := csrfPattern.FindStringSubmatch(body)
	if len(verifyCSRF) < 2 {
		t.Fatal("verify page csrf missing")
	}
	resp = postForm(t, client, baseURL+"/verify-email", url.Values{
		"csrf_token": {verifyCSRF[1]},
		"token":      {token},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("verify status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	user, err := data.UserByName(context.Background(), "verifyuser")
	if err != nil {
		t.Fatal(err)
	}
	if !user.EmailVerified {
		t.Fatal("user email should be verified")
	}
	reuse := postForm(t, client, baseURL+"/verify-email", url.Values{
		"token": {token},
	})
	if reuse.StatusCode == http.StatusSeeOther {
		t.Fatal("verify token must be single use")
	}
	reuse.Body.Close()
	if ok, err := data.HasAudit(context.Background(), "user.email_verify", ""); err != nil || !ok {
		t.Fatalf("email verify audit = %v, %v", ok, err)
	}
}

func TestAccountEmailChange(t *testing.T) {
	baseURL, data, mailer := setupMailApp(t)
	user := createMailUser(t, data, "changeuser", "old@example.com", "change-pass-123")
	if err := data.SetEmailVerified(context.Background(), user.ID, true); err != nil {
		t.Fatal(err)
	}
	client := loginTestClient(t, baseURL, "changeuser", "change-pass-123")
	csrf := fetchCSRFToken(t, client, baseURL+"/account")

	bad := postForm(t, client, baseURL+"/account/email", url.Values{
		"csrf_token": {csrf},
		"email":      {"new@example.com"},
		"password":   {"wrong-pass"},
	})
	if bad.StatusCode != http.StatusBadRequest {
		t.Fatalf("wrong password status = %d", bad.StatusCode)
	}
	bad.Body.Close()

	resp := postForm(t, client, baseURL+"/account/email", url.Values{
		"csrf_token": {csrf},
		"email":      {"new@example.com"},
		"password":   {"change-pass-123"},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("email change status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	updated, err := data.UserByID(context.Background(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Email != "new@example.com" || updated.EmailVerified {
		t.Fatalf("updated user = %+v", updated)
	}
	if mailer.count() != 1 || mailer.last().To != "new@example.com" {
		t.Fatalf("mails = %+v", mailer.mails)
	}
}

func TestForgotUnknownEmail(t *testing.T) {
	baseURL, _, mailer := setupMailApp(t)
	client := testHTTPClient()
	csrf := fetchCSRFToken(t, client, baseURL+"/forgot")
	resp := postForm(t, client, baseURL+"/forgot", url.Values{
		"csrf_token": {csrf},
		"email":      {"nobody@example.com"},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	if mailer.count() != 0 {
		t.Fatal("unknown email must not send mail")
	}
}

func TestMailRoutesRequireConfig(t *testing.T) {
	baseURL, _, _ := setupCommunityApp(t)
	resp := mustGet(t, testHTTPClient(), baseURL+"/forgot")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("forgot status without mail = %d", resp.StatusCode)
	}
}
