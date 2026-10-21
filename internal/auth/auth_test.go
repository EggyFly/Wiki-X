package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"wikix/internal/config"
	"wikix/internal/store"
)

func TestOAuthStateKinds(t *testing.T) {
	manager := New(config.AuthConfig{}, nil)

	rec := httptest.NewRecorder()
	state, err := manager.NewLinkOAuthState(rec)
	if err != nil {
		t.Fatal(err)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("missing oauth state cookie")
	}
	req := httptest.NewRequest(http.MethodGet, "/auth/github/callback?state="+url.QueryEscape(state), nil)
	req.AddCookie(cookies[0])
	kind, ok := manager.CheckOAuthState(httptest.NewRecorder(), req)
	if !ok || kind != "link" {
		t.Fatalf("kind = %q, ok = %v", kind, ok)
	}

	rec = httptest.NewRecorder()
	state, err = manager.NewOAuthState(rec)
	if err != nil {
		t.Fatal(err)
	}
	cookies = rec.Result().Cookies()
	req = httptest.NewRequest(http.MethodGet, "/auth/github/callback?state="+url.QueryEscape(state), nil)
	req.AddCookie(cookies[0])
	kind, ok = manager.CheckOAuthState(httptest.NewRecorder(), req)
	if !ok || kind != "login" {
		t.Fatalf("kind = %q, ok = %v", kind, ok)
	}
}

func TestPasswordHash(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword(hash, "correct horse battery staple") {
		t.Fatal("password should verify")
	}
	if VerifyPassword(hash, "wrong") {
		t.Fatal("wrong password verified")
	}
	if VerifyPassword("not-a-hash", "x") {
		t.Fatal("malformed hash verified")
	}
}

func TestVerifyLogin(t *testing.T) {
	if VerifyLogin(nil, "anything") {
		t.Fatal("nil user verified")
	}
	if VerifyLogin(&store.User{}, "anything") {
		t.Fatal("empty password hash verified")
	}
	hash, err := HashPassword("password123")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyLogin(&store.User{PasswordHash: hash}, "password123") {
		t.Fatal("valid login failed")
	}
}

func TestLoginCSRF(t *testing.T) {
	manager := New(config.AuthConfig{}, nil)
	get := httptest.NewRequest(http.MethodGet, "/login", nil)
	rec := httptest.NewRecorder()
	token := manager.LoginCSRF(rec, get)
	if token == "" {
		t.Fatal("empty csrf token")
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("csrf cookie not set")
	}

	form := url.Values{"csrf_token": {token}}
	post := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	post.AddCookie(cookies[0])
	if !manager.CheckLoginCSRF(post) {
		t.Fatal("csrf check failed")
	}
	post.Form.Set("csrf_token", "wrong")
	if manager.CheckLoginCSRF(post) {
		t.Fatal("wrong csrf accepted")
	}
}

func TestLoginLimit(t *testing.T) {
	manager := New(config.AuthConfig{}, nil)
	for i := 0; i < 10; i++ {
		if !manager.AllowLogin("key") {
			t.Fatalf("attempt %d blocked too early", i)
		}
		manager.RecordLoginFailure("key")
	}
	if manager.AllowLogin("key") {
		t.Fatal("rate limit did not block")
	}
	manager.RecordLoginSuccess("key")
	if !manager.AllowLogin("key") {
		t.Fatal("rate limit did not reset")
	}
}

func TestSessionTokens(t *testing.T) {
	token, hash, err := NewSessionToken()
	if err != nil {
		t.Fatal(err)
	}
	if token == "" || hash == "" || token == hash {
		t.Fatalf("token = %q, hash = %q", token, hash)
	}
	csrf, err := NewCSRFToken()
	if err != nil || csrf == "" {
		t.Fatalf("csrf = %q, %v", csrf, err)
	}
}

func TestClientIPWithTrustedProxies(t *testing.T) {
	direct := New(config.AuthConfig{}, nil)
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "203.0.113.7:5555"
	request.Header.Set("X-Forwarded-For", "198.51.100.9")
	if got := direct.ClientIP(request); got != "203.0.113.7" {
		t.Fatalf("without trusted proxy = %q", got)
	}

	behind := New(config.AuthConfig{TrustedProxies: []string{"10.0.0.0/8"}}, nil)
	request.RemoteAddr = "10.1.2.3:443"
	request.Header.Set("X-Forwarded-For", "203.0.113.33, 10.9.9.9")
	if got := behind.ClientIP(request); got != "203.0.113.33" {
		t.Fatalf("rightmost untrusted = %q", got)
	}
	request.Header.Set("X-Forwarded-For", "198.51.100.9")
	if got := behind.ClientIP(request); got != "198.51.100.9" {
		t.Fatalf("single untrusted hop = %q", got)
	}
	request.Header.Del("X-Forwarded-For")
	if got := behind.ClientIP(request); got != "10.1.2.3" {
		t.Fatalf("missing header = %q", got)
	}
}

func TestTOTPTicket(t *testing.T) {
	manager := New(config.AuthConfig{}, nil)
	recorder := httptest.NewRecorder()
	if err := manager.IssueTOTPTicket(recorder, 42); err != nil {
		t.Fatal(err)
	}
	var ticket string
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == totpTicketCookie {
			ticket = cookie.Value
		}
	}
	if ticket == "" {
		t.Fatal("ticket cookie not set")
	}

	request := httptest.NewRequest(http.MethodGet, "/verify", nil)
	if _, ok := manager.ReadTOTPTicket(request); ok {
		t.Fatal("ticket read without cookie")
	}
	request.AddCookie(&http.Cookie{Name: totpTicketCookie, Value: ticket})
	userID, ok := manager.ReadTOTPTicket(request)
	if !ok || userID != 42 {
		t.Fatalf("ticket read = %d, %v", userID, ok)
	}

	payload := "7." + strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)
	other := hmac.New(sha256.New, []byte("not-the-manager-key"))
	other.Write([]byte(payload))
	request = httptest.NewRequest(http.MethodGet, "/verify", nil)
	request.AddCookie(&http.Cookie{Name: totpTicketCookie, Value: payload + "." + hex.EncodeToString(other.Sum(nil))})
	if _, ok := manager.ReadTOTPTicket(request); ok {
		t.Fatal("forged ticket accepted")
	}

	expired := "9." + strconv.FormatInt(time.Now().Add(-time.Minute).Unix(), 10)
	expiredMAC := hmac.New(sha256.New, manager.totpKey)
	expiredMAC.Write([]byte(expired))
	request = httptest.NewRequest(http.MethodGet, "/verify", nil)
	request.AddCookie(&http.Cookie{Name: totpTicketCookie, Value: expired + "." + hex.EncodeToString(expiredMAC.Sum(nil))})
	if _, ok := manager.ReadTOTPTicket(request); ok {
		t.Fatal("expired ticket accepted")
	}

	recorder = httptest.NewRecorder()
	manager.ClearTOTPTicket(recorder)
	cleared := recorder.Result().Cookies()[0]
	if cleared.Name != totpTicketCookie || cleared.MaxAge >= 0 {
		t.Fatalf("clear cookie = %+v", cleared)
	}
}
