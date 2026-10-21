package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/EggyFly/Wiki-X/internal/config"
	"github.com/EggyFly/Wiki-X/internal/store"
)

var inviteCodePattern = regexp.MustCompile(`<code class="invite-code">([A-Z0-9-]+)</code>`)

func setupRegistrationApp(t *testing.T, mode string) (string, *fakeMailer, *store.Store) {
	t.Helper()
	app, data, _ := setupCommunityFixture(t, config.GitHubConfig{})
	app.cfg.Registration = config.RegistrationConfig{Mode: mode, DefaultGroup: "user"}
	app.cfg.Site.BaseURL = "https://wiki.example"
	mailer := &fakeMailer{}
	app.mailer = mailer
	server := httptest.NewServer(app.Handler())
	t.Cleanup(server.Close)
	return server.URL, mailer, data
}

func TestInviteRegistrationFlow(t *testing.T) {
	baseURL, _, data := setupRegistrationApp(t, "invite")
	admin := loginTestClient(t, baseURL, "admin", "admin-pass-123")
	csrf := fetchCSRFToken(t, admin, baseURL+"/admin/invites")
	resp := postForm(t, admin, baseURL+"/admin/invites/create", url.Values{
		"csrf_token":   {csrf},
		"label":        {"streamer"},
		"max_uses":     {"5"},
		"expires_days": {"7"},
	})
	body := readResponse(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create invite status = %d", resp.StatusCode)
	}
	match := inviteCodePattern.FindStringSubmatch(body)
	if len(match) < 2 {
		t.Fatalf("invite code not shown: %s", body)
	}
	code := match[1]
	invites, err := data.ListInvites(context.Background(), 10)
	if err != nil || len(invites) != 1 || invites[0].Label != "streamer" {
		t.Fatalf("invites = %+v, %v", invites, err)
	}

	client := testHTTPClient()
	page := mustGet(t, client, baseURL+"/register")
	html := readResponse(t, page)
	if page.StatusCode != http.StatusOK || !strings.Contains(html, "邀请码") {
		t.Fatalf("register page = %d", page.StatusCode)
	}

	bad := postForm(t, client, baseURL+"/register", url.Values{
		"csrf_token":       {csrfFromPage(t, html)},
		"username":         {"badinvite"},
		"email":            {"badinvite@example.com"},
		"password":         {"invite-pass-123"},
		"confirm_password": {"invite-pass-123"},
		"invite_code":      {"AAAA-BBBB-CCCC-DDDD"},
	})
	if bad.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad invite status = %d", bad.StatusCode)
	}
	bad.Body.Close()

	resp = postForm(t, client, baseURL+"/register", url.Values{
		"csrf_token":       {csrfFromPage(t, html)},
		"username":         {"inviteduser"},
		"email":            {"invited@example.com"},
		"password":         {"invite-pass-123"},
		"confirm_password": {"invite-pass-123"},
		"invite_code":      {code},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("register status = %d", resp.StatusCode)
	}
	if location := resp.Header.Get("Location"); !strings.Contains(location, "notice=registered") {
		t.Fatalf("register location = %q", location)
	}
	resp.Body.Close()
	if got := loginStatus(t, baseURL, "inviteduser", "invite-pass-123"); got != http.StatusSeeOther {
		t.Fatalf("invited login = %d", got)
	}
	user, err := data.UserByName(context.Background(), "inviteduser")
	if err != nil {
		t.Fatal(err)
	}
	if len(user.Groups) != 1 || user.Groups[0] != "user" {
		t.Fatalf("groups = %+v", user.Groups)
	}

	dup := postForm(t, client, baseURL+"/register", url.Values{
		"csrf_token":       {csrfFromPage(t, html)},
		"username":         {"inviteduser"},
		"email":            {"other@example.com"},
		"password":         {"invite-pass-123"},
		"confirm_password": {"invite-pass-123"},
		"invite_code":      {code},
	})
	if dup.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate user status = %d", dup.StatusCode)
	}
	dup.Body.Close()

	nav := readResponse(t, mustGet(t, admin, baseURL+"/admin/users"))
	if !strings.Contains(nav, `/admin/invites`) {
		t.Fatal("invite admin link missing from navigation")
	}
	adminCSRF := fetchCSRFToken(t, admin, baseURL+"/admin/invites")
	del := postForm(t, admin, baseURL+"/admin/invites/delete", url.Values{
		"csrf_token": {adminCSRF},
		"id":         {intText(invites[0].ID)},
	})
	if del.StatusCode != http.StatusSeeOther {
		t.Fatalf("delete invite status = %d", del.StatusCode)
	}
	del.Body.Close()
}

func TestOpenRegistrationFlow(t *testing.T) {
	baseURL, mailer, data := setupRegistrationApp(t, "open")
	client := testHTTPClient()
	html := readResponse(t, mustGet(t, client, baseURL+"/register"))

	resp := postForm(t, client, baseURL+"/register", url.Values{
		"csrf_token":       {csrfFromPage(t, html)},
		"username":         {"openuser"},
		"email":            {"open@example.com"},
		"password":         {"open-pass-123"},
		"confirm_password": {"open-pass-123"},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("open register status = %d", resp.StatusCode)
	}
	if location := resp.Header.Get("Location"); !strings.Contains(location, "/register?sent=1") {
		t.Fatalf("register location = %q", location)
	}
	resp.Body.Close()
	if mailer.count() != 1 {
		t.Fatalf("mails = %d", mailer.count())
	}
	if got := loginStatus(t, baseURL, "openuser", "open-pass-123"); got != http.StatusUnauthorized {
		t.Fatalf("pending login = %d", got)
	}
	user, err := data.UserByName(context.Background(), "openuser")
	if err != nil {
		t.Fatal(err)
	}
	if !user.Disabled || user.EmailVerified {
		t.Fatalf("pending user = %+v", user)
	}

	match := mailTokenPattern.FindStringSubmatch(mailer.last().Body)
	if len(match) < 2 {
		t.Fatalf("token not found: %s", mailer.last().Body)
	}
	verifyPath := "/verify-email?token=" + match[1]
	verifyHTML := readResponse(t, mustGet(t, client, baseURL+verifyPath))
	resp = postForm(t, client, baseURL+"/verify-email", url.Values{
		"csrf_token": {csrfFromPage(t, verifyHTML)},
		"token":      {match[1]},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("verify status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	if got := loginStatus(t, baseURL, "openuser", "open-pass-123"); got != http.StatusSeeOther {
		t.Fatalf("verified login = %d", got)
	}
	verified, err := data.UserByID(context.Background(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if verified.Disabled || !verified.EmailVerified {
		t.Fatalf("verified user = %+v", verified)
	}

	verifyPage := readResponse(t, mustGet(t, client, baseURL+"/register/verify"))
	resp = postForm(t, client, baseURL+"/register/verify", url.Values{
		"csrf_token": {csrfFromPage(t, verifyPage)},
		"email":      {"nobody@example.com"},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("resend unknown status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	if mailer.count() != 1 {
		t.Fatal("unknown email must not trigger a verification mail")
	}
}

func TestRegistrationOff(t *testing.T) {
	baseURL, _, _ := setupCommunityApp(t)
	resp := mustGet(t, testHTTPClient(), baseURL+"/register")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("register status = %d", resp.StatusCode)
	}
}

func csrfFromPage(t *testing.T, html string) string {
	t.Helper()
	match := csrfPattern.FindStringSubmatch(html)
	if len(match) < 2 {
		t.Fatal("csrf token not found in page")
	}
	return match[1]
}

func intText(value int64) string {
	return strconv.FormatInt(value, 10)
}
