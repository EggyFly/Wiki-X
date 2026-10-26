package server

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/EggyFly/Wiki-X/internal/config"
	oidcsvc "github.com/EggyFly/Wiki-X/internal/oidc"
	"github.com/EggyFly/Wiki-X/internal/store"
)

const testOIDCKey = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="

func TestOIDCHTTPFlow(t *testing.T) {
	app, data, _ := setupCommunityFixture(t, config.GitHubConfig{})
	service, err := oidcsvc.New(t.Context(), config.OIDCConfig{
		Enabled: true, Issuer: "https://issuer.example.com", KeyEncryptionKey: testOIDCKey,
		AccessTTL: "5m", IDTTL: "5m", RefreshTTL: "1h", CodeTTL: "2m",
	}, data)
	if err != nil {
		t.Fatal(err)
	}
	app.oidc = service
	server := httptest.NewServer(app.Handler())
	defer server.Close()

	resp := mustGet(t, testHTTPClient(), server.URL+"/.well-known/openid-configuration")
	var discovery map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&discovery); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if discovery["issuer"] != "https://issuer.example.com" || discovery["authorization_endpoint"] != "https://issuer.example.com/oauth2/authorize" {
		t.Fatalf("discovery = %#v", discovery)
	}
	resp = mustGet(t, testHTTPClient(), server.URL+"/.well-known/jwks.json")
	var jwks struct {
		Keys []map[string]any `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(jwks.Keys) != 1 || jwks.Keys[0]["kty"] != "RSA" || jwks.Keys[0]["alg"] != "RS256" {
		t.Fatalf("jwks = %#v", jwks)
	}

	clientID := fmt.Sprintf("test-oidc-%d", time.Now().UnixNano())
	secret := "test-oidc-secret"
	redirect := "https://app.example.com/callback"
	client := store.OIDCClient{
		ClientID: clientID, SecretHash: oidcsvc.Hash(secret), Name: "Test App", Provider: "Test",
		RedirectURIs: []string{redirect}, Scopes: "openid profile email", Enabled: true,
		AuthMethod: "client_secret_basic",
	}
	if err := data.CreateOIDCClient(t.Context(), client); err != nil {
		t.Fatal(err)
	}

	verifier := "test-verifier-1234567890"
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	authorize := url.Values{
		"client_id":             {clientID},
		"redirect_uri":          {redirect},
		"response_type":         {"code"},
		"scope":                 {"openid profile email"},
		"state":                 {"state-1"},
		"nonce":                 {"nonce-1"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	authURL := server.URL + "/oauth2/authorize?" + authorize.Encode()
	anonymous := testHTTPClient()
	resp = mustGet(t, anonymous, authURL)
	if resp.StatusCode != http.StatusSeeOther || !strings.HasPrefix(resp.Header.Get("Location"), "/login?next=") {
		t.Fatalf("anonymous authorize status = %d, location = %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	resp.Body.Close()

	admin := loginTestClient(t, server.URL, "admin", "admin-pass-123")
	consentURL := server.URL + "/oauth2/authorize?" + authorize.Encode()
	resp = mustGet(t, admin, consentURL)
	if resp.StatusCode != http.StatusOK || !strings.Contains(resp.Header.Get("Content-Security-Policy"), "img-src 'self' data: https:") {
		t.Fatalf("consent page status = %d, csp = %q", resp.StatusCode, resp.Header.Get("Content-Security-Policy"))
	}
	consentBody := readResponse(t, resp)
	csrfMatch := csrfPattern.FindStringSubmatch(consentBody)
	if len(csrfMatch) < 2 {
		t.Fatal("consent csrf token not found")
	}
	csrf := csrfMatch[1]
	form := url.Values{
		"csrf_token":            {csrf},
		"client_id":             {clientID},
		"redirect_uri":          {redirect},
		"response_type":         {"code"},
		"scope":                 {"openid profile email"},
		"state":                 {"state-1"},
		"nonce":                 {"nonce-1"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"decision":              {"approve"},
	}
	resp = postForm(t, admin, server.URL+"/oauth2/authorize", form)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("consent status = %d, body = %s", resp.StatusCode, readResponse(t, resp))
	}
	location, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if location.Query().Get("state") != "state-1" || location.Query().Get("code") == "" {
		t.Fatalf("authorize redirect = %q", location.String())
	}
	code := location.Query().Get("code")

	tokens := exchangeOIDCToken(t, server.URL, clientID, secret, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirect},
		"code_verifier": {verifier},
	})
	access, _ := tokens["access_token"].(string)
	refresh, _ := tokens["refresh_token"].(string)
	if access == "" || refresh == "" || tokens["id_token"] == nil {
		t.Fatalf("token response = %#v", tokens)
	}

	claims := oidcUserInfo(t, server.URL, access)
	if claims["preferred_username"] != "admin" || claims["sub"] == "" {
		t.Fatalf("userinfo = %#v", claims)
	}

	refreshed := exchangeOIDCToken(t, server.URL, clientID, secret, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refresh},
	})
	nextAccess, _ := refreshed["access_token"].(string)
	nextRefresh, _ := refreshed["refresh_token"].(string)
	if nextAccess == "" || nextRefresh == "" || nextRefresh == refresh {
		t.Fatalf("refresh response = %#v", refreshed)
	}

	revoke := url.Values{"token": {nextRefresh}}
	req, err := http.NewRequest(http.MethodPost, server.URL+"/oauth2/revoke", strings.NewReader(revoke.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(clientID, secret)
	resp, err = testHTTPClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("revoke status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	resp = oidcUserInfoResponse(t, server.URL, nextAccess)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("userinfo after revoke status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	silent := url.Values{}
	for key, value := range authorize {
		silent[key] = append([]string(nil), value...)
	}
	silent.Set("state", "state-2")
	silent.Set("nonce", "nonce-2")
	silent.Set("prompt", "none")
	resp = mustGet(t, admin, server.URL+"/oauth2/authorize?"+silent.Encode())
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("silent authorize status = %d", resp.StatusCode)
	}
	silentLocation, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	silentCode := silentLocation.Query().Get("code")
	if silentCode == "" {
		t.Fatalf("silent authorize redirect = %q", silentLocation.String())
	}
	silentTokens := exchangeOIDCToken(t, server.URL, clientID, secret, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {silentCode},
		"redirect_uri":  {redirect},
		"code_verifier": {verifier},
	})
	silentAccess, _ := silentTokens["access_token"].(string)
	if silentAccess == "" {
		t.Fatalf("silent token response = %#v", silentTokens)
	}

	csrf = fetchCSRFToken(t, admin, server.URL+"/admin/oidc")
	update := url.Values{
		"csrf_token": {csrf},
		"client_id":  {clientID},
		"name":       {"Test App"},
		"provider":   {"Test"},
		"logo":       {""},
		"redirects":  {redirect},
	}
	resp = postForm(t, admin, server.URL+"/admin/oidc/update", update)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("disable status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	resp = oidcUserInfoResponse(t, server.URL, silentAccess)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("userinfo after disable status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	update.Set("enabled", "1")
	resp = postForm(t, admin, server.URL+"/admin/oidc/update", update)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("enable status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	resp = oidcUserInfoResponse(t, server.URL, silentAccess)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("disabled client session was not revoked, status = %d", resp.StatusCode)
	}
	resp.Body.Close()

	csrf = fetchCSRFToken(t, admin, server.URL+"/admin/oidc")
	resp = postForm(t, admin, server.URL+"/admin/oidc/delete", url.Values{
		"csrf_token": {csrf},
		"client_id":  {clientID},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	page := readResponse(t, mustGet(t, admin, server.URL+"/admin/oidc"))
	if strings.Contains(page, clientID) {
		t.Fatal("revoked OIDC client is still listed")
	}
}

func exchangeOIDCToken(t *testing.T, baseURL, clientID, secret string, form url.Values) map[string]any {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, baseURL+"/oauth2/token", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(clientID, secret)
	resp, err := testHTTPClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("token status = %d", resp.StatusCode)
	}
	var tokens map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&tokens); err != nil {
		t.Fatal(err)
	}
	return tokens
}

func oidcUserInfo(t *testing.T, baseURL, access string) map[string]any {
	t.Helper()
	resp := oidcUserInfoResponse(t, baseURL, access)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("userinfo status = %d", resp.StatusCode)
	}
	var claims map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&claims); err != nil {
		t.Fatal(err)
	}
	return claims
}

func oidcUserInfoResponse(t *testing.T, baseURL, access string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, baseURL+"/oauth2/userinfo", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+access)
	resp, err := testHTTPClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}
