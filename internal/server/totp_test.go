package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/EggyFly/Wiki-X/internal/auth"
	"github.com/EggyFly/Wiki-X/internal/config"
)

var totpSecretPattern = regexp.MustCompile(`<code class="totp-secret">([A-Z2-7]+)</code>`)

// 验证码防重放会拒绝同一个时间步重复使用, 后续步骤要取更大的时间步, 且必须落在服务端 ±1 窗口内
func totpCodeAt(t *testing.T, secret string, offset int) string {
	t.Helper()
	code, err := auth.TOTPCode(secret, time.Now().Add(time.Duration(offset)*30*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	return code
}

func enableTOTP(t *testing.T, client *http.Client, baseURL, username string) string {
	t.Helper()
	csrf := fetchCSRFToken(t, client, baseURL+"/account")
	setup := postForm(t, client, baseURL+"/account/totp/setup", url.Values{"csrf_token": {csrf}})
	if setup.StatusCode != http.StatusSeeOther {
		t.Fatalf("totp setup status = %d", setup.StatusCode)
	}
	setup.Body.Close()
	account := mustGet(t, client, baseURL+"/account")
	body := readResponse(t, account)
	match := totpSecretPattern.FindStringSubmatch(body)
	if match == nil {
		t.Fatalf("totp secret not found on account page")
	}
	secret := match[1]
	enable := postForm(t, client, baseURL+"/account/totp/enable", url.Values{
		"csrf_token": {csrf},
		"totp_code":  {totpCodeAt(t, secret, 0)},
	})
	if enable.StatusCode != http.StatusSeeOther {
		t.Fatalf("totp enable status = %d", enable.StatusCode)
	}
	enable.Body.Close()
	return secret
}

func TestTOTPLoginFlow(t *testing.T) {
	baseURL, data, _ := setupCommunityApp(t)
	admin := loginTestClient(t, baseURL, "admin", "admin-pass-123")
	secret := enableTOTP(t, admin, baseURL, "admin")
	if _, err := data.UserByName(context.Background(), "admin"); err != nil {
		t.Fatal(err)
	}

	passwordOnly := loginStatus(t, baseURL, "admin", "admin-pass-123")
	if passwordOnly != http.StatusUnauthorized {
		t.Fatalf("password-only login status = %d", passwordOnly)
	}

	client := testHTTPClient()
	token := fetchCSRFToken(t, client, baseURL+"/login")
	bad := postForm(t, client, baseURL+"/login", url.Values{
		"csrf_token": {token},
		"username":   {"admin"},
		"password":   {"admin-pass-123"},
		"totp_code":  {"000000"},
		"next":       {"/"},
	})
	if bad.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad code status = %d", bad.StatusCode)
	}
	bad.Body.Close()

	code := totpCodeAt(t, secret, 1)
	good := postForm(t, client, baseURL+"/login", url.Values{
		"csrf_token": {token},
		"username":   {"admin"},
		"password":   {"admin-pass-123"},
		"totp_code":  {code},
		"next":       {"/"},
	})
	if good.StatusCode != http.StatusSeeOther {
		t.Fatalf("totp login status = %d", good.StatusCode)
	}
	good.Body.Close()
	account := mustGet(t, client, baseURL+"/account")
	if account.StatusCode != http.StatusOK {
		t.Fatalf("account after totp login = %d", account.StatusCode)
	}
	account.Body.Close()

	replayClient := testHTTPClient()
	replay := postForm(t, replayClient, baseURL+"/login", url.Values{
		"csrf_token": {fetchCSRFToken(t, replayClient, baseURL+"/login")},
		"username":   {"admin"},
		"password":   {"admin-pass-123"},
		"totp_code":  {code},
		"next":       {"/"},
	})
	if replay.StatusCode != http.StatusUnauthorized {
		t.Fatalf("replayed code status = %d", replay.StatusCode)
	}
	replay.Body.Close()
}

func TestAdminDisablesTOTP(t *testing.T) {
	baseURL, data, _ := setupCommunityApp(t)
	admin := loginTestClient(t, baseURL, "admin", "admin-pass-123")
	editor := loginTestClient(t, baseURL, "editor", "editor-pass-123")
	editorUser, err := data.UserByName(context.Background(), "editor")
	if err != nil {
		t.Fatal(err)
	}
	enableTOTP(t, editor, baseURL, "editor")
	editorUser, err = data.UserByID(context.Background(), editorUser.ID)
	if err != nil || !editorUser.TOTPEnabled {
		t.Fatalf("editor totp = %+v, %v", editorUser, err)
	}

	blocked := postForm(t, editor, baseURL+"/admin/users/totp/disable", url.Values{
		"csrf_token": {fetchCSRFToken(t, editor, baseURL+"/account")},
		"id":         {strconvInt(editorUser.ID)},
	})
	if blocked.StatusCode != http.StatusForbidden {
		t.Fatalf("editor disable status = %d", blocked.StatusCode)
	}
	blocked.Body.Close()

	csrf := fetchCSRFToken(t, admin, baseURL+"/admin/users")
	self := postForm(t, admin, baseURL+"/admin/users/totp/disable", url.Values{
		"csrf_token": {csrf},
		"id":         {"1"},
	})
	if self.StatusCode != http.StatusBadRequest {
		t.Fatalf("self disable status = %d", self.StatusCode)
	}
	self.Body.Close()

	disable := postForm(t, admin, baseURL+"/admin/users/totp/disable", url.Values{
		"csrf_token": {csrf},
		"id":         {strconvInt(editorUser.ID)},
	})
	if disable.StatusCode != http.StatusSeeOther {
		t.Fatalf("disable status = %d", disable.StatusCode)
	}
	disable.Body.Close()
	editorUser, err = data.UserByID(context.Background(), editorUser.ID)
	if err != nil || editorUser.TOTPEnabled || editorUser.TOTPSecret != "" {
		t.Fatalf("editor after disable = %+v, %v", editorUser, err)
	}
	if account := mustGet(t, editor, baseURL+"/account"); account.StatusCode != http.StatusSeeOther {
		t.Fatalf("editor session after forced disable = %d", account.StatusCode)
	} else {
		account.Body.Close()
	}
	if status := loginStatus(t, baseURL, "editor", "editor-pass-123"); status != http.StatusSeeOther {
		t.Fatalf("editor password login = %d", status)
	}
}

func TestAccountPageShowsTOTPPanel(t *testing.T) {
	baseURL, _, _ := setupCommunityApp(t)
	admin := loginTestClient(t, baseURL, "admin", "admin-pass-123")
	account := mustGet(t, admin, baseURL+"/account")
	body := readResponse(t, account)
	if !strings.Contains(body, "两步验证") || !strings.Contains(body, "开启两步验证") {
		t.Fatal("account page missing totp panel")
	}

	csrf := fetchCSRFToken(t, admin, baseURL+"/account")
	setup := postForm(t, admin, baseURL+"/account/totp/setup", url.Values{"csrf_token": {csrf}})
	if setup.StatusCode != http.StatusSeeOther {
		t.Fatalf("setup status = %d", setup.StatusCode)
	}
	setup.Body.Close()
	pending := mustGet(t, admin, baseURL+"/account")
	pendingBody := readResponse(t, pending)
	match := totpSecretPattern.FindStringSubmatch(pendingBody)
	if match == nil {
		t.Fatal("pending secret missing")
	}

	wrong := postForm(t, admin, baseURL+"/account/totp/enable", url.Values{
		"csrf_token": {csrf},
		"totp_code":  {"000000"},
	})
	if wrong.StatusCode != http.StatusBadRequest {
		t.Fatalf("wrong enable status = %d", wrong.StatusCode)
	}
	wrong.Body.Close()

	stale := postForm(t, admin, baseURL+"/account/totp/enable", url.Values{
		"csrf_token": {csrf},
		"totp_code":  {totpCodeAt(t, match[1], -4)},
	})
	if stale.StatusCode != http.StatusBadRequest {
		t.Fatalf("expired window enable status = %d", stale.StatusCode)
	}
	stale.Body.Close()
}

func TestTOTPSetupBlockedWhenEnabled(t *testing.T) {
	baseURL, data, _ := setupCommunityApp(t)
	admin := loginTestClient(t, baseURL, "admin", "admin-pass-123")
	enableTOTP(t, admin, baseURL, "admin")

	before, err := data.UserByName(context.Background(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	csrf := fetchCSRFToken(t, admin, baseURL+"/account")
	setup := postForm(t, admin, baseURL+"/account/totp/setup", url.Values{"csrf_token": {csrf}})
	if setup.StatusCode != http.StatusConflict {
		t.Fatalf("setup while enabled = %d", setup.StatusCode)
	}
	setup.Body.Close()

	after, err := data.UserByName(context.Background(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	if !after.TOTPEnabled || after.TOTPSecret != before.TOTPSecret {
		t.Fatalf("secret changed while 2FA on: %+v", after)
	}

	password := postForm(t, admin, baseURL+"/account/totp/disable", url.Values{
		"csrf_token": {csrf},
		"password":   {"wrong-password"},
	})
	if password.StatusCode != http.StatusBadRequest {
		t.Fatalf("disable with wrong password = %d", password.StatusCode)
	}
	password.Body.Close()

	body := readResponse(t, mustGet(t, admin, baseURL+"/account"))
	if !strings.Contains(body, "关闭两步验证") {
		t.Fatal("account page missing disable panel")
	}
	disable := postForm(t, admin, baseURL+"/account/totp/disable", url.Values{
		"csrf_token": {csrf},
		"password":   {"admin-pass-123"},
	})
	if disable.StatusCode != http.StatusSeeOther {
		t.Fatalf("disable status = %d", disable.StatusCode)
	}
	disable.Body.Close()
	user, err := data.UserByName(context.Background(), "admin")
	if err != nil || user.TOTPEnabled || user.TOTPSecret != "" {
		t.Fatalf("admin after disable = %+v, %v", user, err)
	}
	againCSRF := fetchCSRFToken(t, admin, baseURL+"/account")
	again := postForm(t, admin, baseURL+"/account/totp/setup", url.Values{"csrf_token": {againCSRF}})
	if again.StatusCode != http.StatusSeeOther {
		t.Fatalf("setup after disable = %d", again.StatusCode)
	}
	again.Body.Close()
}

func TestVerifyPageAfterTicket(t *testing.T) {
	app, data, _ := setupCommunityFixture(t, config.GitHubConfig{})
	server := httptest.NewServer(app.Handler())
	t.Cleanup(server.Close)
	baseURL := server.URL

	admin := loginTestClient(t, baseURL, "admin", "admin-pass-123")
	secret := enableTOTP(t, admin, baseURL, "admin")
	user, err := data.UserByName(context.Background(), "admin")
	if err != nil {
		t.Fatal(err)
	}

	noTicket := mustGet(t, testHTTPClient(), baseURL+"/verify")
	if noTicket.StatusCode != http.StatusUnauthorized {
		t.Fatalf("verify without ticket = %d", noTicket.StatusCode)
	}
	noTicket.Body.Close()

	recorder := httptest.NewRecorder()
	if err := app.auth.IssueTOTPTicket(recorder, user.ID); err != nil {
		t.Fatal(err)
	}
	setCookie := recorder.Result().Header.Get("Set-Cookie")
	_, ticket, found := strings.Cut(setCookie, "=")
	if !found {
		t.Fatalf("ticket cookie missing: %q", setCookie)
	}
	ticket = strings.Split(ticket, ";")[0]

	client := testHTTPClient()
	request, err := http.NewRequest(http.MethodGet, baseURL+"/verify", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.AddCookie(&http.Cookie{Name: "wikix_totp_ticket", Value: ticket})
	page, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body := readResponse(t, page)
	if page.StatusCode != http.StatusOK || !strings.Contains(body, `name="totp_code"`) {
		t.Fatalf("verify page = %d", page.StatusCode)
	}
	csrfMatch := csrfPattern.FindStringSubmatch(body)
	if len(csrfMatch) < 2 {
		t.Fatal("csrf token missing on verify page")
	}
	csrf := csrfMatch[1]

	bad := postVerifyForm(t, client, baseURL, csrf, ticket, "000000")
	if bad.StatusCode != http.StatusUnauthorized {
		t.Fatalf("verify bad code = %d", bad.StatusCode)
	}
	bad.Body.Close()

	good := postVerifyForm(t, client, baseURL, csrf, ticket, totpCodeAt(t, secret, 1))
	if good.StatusCode != http.StatusSeeOther {
		t.Fatalf("verify code = %d", good.StatusCode)
	}
	good.Body.Close()
	account := mustGet(t, client, baseURL+"/account")
	if account.StatusCode != http.StatusOK {
		t.Fatalf("account after verify = %d", account.StatusCode)
	}
	account.Body.Close()
}

func postVerifyForm(t *testing.T, client *http.Client, baseURL, csrf, ticket, code string) *http.Response {
	t.Helper()
	form := url.Values{
		"csrf_token": {csrf},
		"totp_code":  {code},
	}
	request, err := http.NewRequest(http.MethodPost, baseURL+"/verify", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(&http.Cookie{Name: "wikix_totp_ticket", Value: ticket})
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}
