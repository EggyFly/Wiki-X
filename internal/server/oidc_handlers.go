package server

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/EggyFly/Wiki-X/internal/auth"
	"github.com/EggyFly/Wiki-X/internal/i18n"
	oidcsvc "github.com/EggyFly/Wiki-X/internal/oidc"
	"github.com/EggyFly/Wiki-X/internal/store"
)

type oidcAuthorizeRequest struct {
	ClientID            string
	RedirectURI         string
	ResponseType        string
	Scope               string
	State               string
	Nonce               string
	Prompt              string
	CodeChallenge       string
	CodeChallengeMethod string
}

type oidcConsentScope struct {
	Scope string
	Label string
	Desc  string
}

type oidcConsentData struct {
	baseData
	Client          *store.OIDCClient
	Scopes          []oidcConsentScope
	ClientID        string
	Redirect        string
	Scope           string
	State           string
	Nonce           string
	Challenge       string
	ChallengeMethod string
}

type adminOIDCData struct {
	baseData
	Clients     []adminOIDCClient
	NewClientID string
	NewSecret   string
	Error       string
}

type adminOIDCClient struct {
	*store.OIDCClient
	RedirectText string
}

func (a *App) handleOIDCDiscovery(w http.ResponseWriter, r *http.Request) {
	if a.oidc == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_ = json.NewEncoder(w).Encode(a.oidc.Discovery())
}

func (a *App) handleOIDCJWKS(w http.ResponseWriter, r *http.Request) {
	if a.oidc == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_ = json.NewEncoder(w).Encode(a.oidc.JWKS())
}

func (a *App) handleOIDCAuthorize(w http.ResponseWriter, r *http.Request) {
	if a.oidc == nil {
		http.NotFound(w, r)
		return
	}
	values := oidcAuthorizeValues(r)
	client, err := a.validateOIDCAuthorize(r.Context(), &values)
	if err != nil {
		a.oidcAuthorizeError(w, r, values, err)
		return
	}
	session := auth.SessionFromContext(r.Context())
	if session == nil {
		if values.Prompt == "none" {
			a.oidcRedirectError(w, r, values.RedirectURI, values.State, "login_required", "no active session")
			return
		}
		http.Redirect(w, r, "/login?next="+url.QueryEscape(safeNext(r.URL.RequestURI())), http.StatusSeeOther)
		return
	}
	consented, err := a.store.GetOIDCConsent(r.Context(), session.User.ID, client.ClientID)
	if err != nil {
		a.oidcRedirectError(w, r, values.RedirectURI, values.State, "server_error", "consent lookup failed")
		return
	}
	if values.Prompt == "none" && !oidcHasScopes(consented, values.Scope) {
		a.oidcRedirectError(w, r, values.RedirectURI, values.State, "consent_required", "user consent required")
		return
	}
	if values.Prompt == "consent" || !oidcHasScopes(consented, values.Scope) {
		a.renderOIDCConsent(w, r, client, values, session)
		return
	}
	a.oidcIssueCode(w, r, client, values, session.User.ID)
}

func (a *App) handleOIDCConsent(w http.ResponseWriter, r *http.Request) {
	if a.oidc == nil {
		http.NotFound(w, r)
		return
	}
	session := auth.SessionFromContext(r.Context())
	if session == nil {
		http.Redirect(w, r, "/login?next="+url.QueryEscape("/"), http.StatusSeeOther)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	if err := r.ParseForm(); err != nil || !a.auth.CheckCSRF(r, session) {
		a.renderOIDCMessage(w, r, http.StatusForbidden, "授权确认已过期", "请返回应用重新发起授权.")
		return
	}
	values := oidcAuthorizeValuesFromForm(r)
	client, err := a.validateOIDCAuthorize(r.Context(), &values)
	if err != nil {
		a.oidcAuthorizeError(w, r, values, err)
		return
	}
	decision := strings.TrimSpace(r.FormValue("decision"))
	if decision == "deny" {
		a.oidcRedirectError(w, r, values.RedirectURI, values.State, "access_denied", "user denied authorization")
		return
	}
	if decision != "approve" {
		a.renderOIDCMessage(w, r, http.StatusBadRequest, "授权确认无效", "请重新发起授权.")
		return
	}
	if err := a.store.SaveOIDCConsent(r.Context(), session.User.ID, client.ClientID, values.Scope); err != nil {
		slog.Error("save oidc consent failed", "err", err)
		a.oidcRedirectError(w, r, values.RedirectURI, values.State, "server_error", "consent save failed")
		return
	}
	a.oidcIssueCode(w, r, client, values, session.User.ID)
}

func (a *App) oidcIssueCode(w http.ResponseWriter, r *http.Request, client *store.OIDCClient, values oidcAuthorizeRequest, userID int64) {
	code, err := a.oidc.CreateAuthCode(r.Context(), client.ClientID, userID, values.RedirectURI,
		values.Scope, values.Nonce, values.CodeChallenge, time.Now())
	if err != nil {
		slog.Error("create oidc code failed", "err", err)
		a.oidcRedirectError(w, r, values.RedirectURI, values.State, "server_error", "code creation failed")
		return
	}
	target := values.RedirectURI
	separator := "?"
	if strings.Contains(target, "?") {
		separator = "&"
	}
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, target+separator+url.Values{"code": {code}, "state": {values.State}}.Encode(), http.StatusFound)
}

func (a *App) renderOIDCConsent(w http.ResponseWriter, r *http.Request, client *store.OIDCClient, values oidcAuthorizeRequest, session *store.SessionData) {
	w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: https: http:; style-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
	base := a.baseData(r, i18n.Text(a.uiLang(), "oidc_consent_title"))
	scopes := make([]oidcConsentScope, 0)
	for _, scope := range strings.Fields(values.Scope) {
		scopes = append(scopes, oidcConsentScope{
			Scope: scope,
			Label: base.T["oidc_scope_"+scope],
			Desc:  base.T["oidc_scope_"+scope+"_desc"],
		})
	}
	a.renderUI(w, r, http.StatusOK, "oidc_consent", oidcConsentData{
		baseData:        base,
		Client:          client,
		Scopes:          scopes,
		ClientID:        client.ClientID,
		Redirect:        values.RedirectURI,
		Scope:           values.Scope,
		State:           values.State,
		Nonce:           values.Nonce,
		Challenge:       values.CodeChallenge,
		ChallengeMethod: values.CodeChallengeMethod,
	})
}

func (a *App) renderOIDCMessage(w http.ResponseWriter, r *http.Request, status int, heading, message string) {
	lang := a.uiLang()
	heading = i18n.Message(lang, heading)
	message = i18n.Message(lang, message)
	a.renderUI(w, r, status, "message", messageData{
		baseData: a.baseData(r, heading),
		Heading:  heading,
		Message:  message,
	})
}

func (a *App) handleOIDCToken(w http.ResponseWriter, r *http.Request) {
	if a.oidc == nil {
		http.NotFound(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	if err := r.ParseForm(); err != nil {
		a.writeOIDCError(w, http.StatusBadRequest, "invalid_request", "invalid form body")
		return
	}
	client, err := a.authenticateOIDCClient(r)
	if err != nil {
		a.writeOIDCError(w, http.StatusUnauthorized, "invalid_client", "client authentication failed")
		return
	}
	grantType := r.FormValue("grant_type")
	switch grantType {
	case "authorization_code":
		code := r.FormValue("code")
		redirectURI := r.FormValue("redirect_uri")
		verifier := r.FormValue("code_verifier")
		if code == "" || redirectURI == "" || verifier == "" || len(code) > 512 || len(redirectURI) > 1024 || len(verifier) > 1024 {
			a.writeOIDCError(w, http.StatusBadRequest, "invalid_request", "code, redirect_uri and code_verifier are required")
			return
		}
		tokens, _, err := a.oidc.ExchangeCode(r.Context(), client, code, redirectURI, verifier)
		if err != nil {
			a.writeOIDCError(w, http.StatusBadRequest, "invalid_grant", "authorization code is invalid")
			return
		}
		a.writeOIDCTokens(w, tokens)
	case "refresh_token":
		refresh := r.FormValue("refresh_token")
		if refresh == "" || len(refresh) > 512 {
			a.writeOIDCError(w, http.StatusBadRequest, "invalid_request", "refresh_token is required")
			return
		}
		tokens, err := a.oidc.Refresh(r.Context(), client, refresh)
		if err != nil {
			a.writeOIDCError(w, http.StatusBadRequest, "invalid_grant", "refresh token is invalid")
			return
		}
		a.writeOIDCTokens(w, tokens)
	default:
		a.writeOIDCError(w, http.StatusBadRequest, "unsupported_grant_type", "grant type is not supported")
	}
}

func (a *App) handleOIDCUserInfo(w http.ResponseWriter, r *http.Request) {
	if a.oidc == nil {
		http.NotFound(w, r)
		return
	}
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
		a.writeOIDCError(w, http.StatusUnauthorized, "invalid_token", "bearer token required")
		return
	}
	token := strings.TrimSpace(authHeader[7:])
	if token == "" || len(token) > 4096 {
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
		a.writeOIDCError(w, http.StatusUnauthorized, "invalid_token", "bearer token required")
		return
	}
	claims, err := a.oidc.UserInfo(r.Context(), token)
	if err != nil {
		slog.Warn("oidc userinfo rejected", "err", err)
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
		a.writeOIDCError(w, http.StatusUnauthorized, "invalid_token", "token is invalid")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(claims)
}

func (a *App) handleOIDCRevoke(w http.ResponseWriter, r *http.Request) {
	if a.oidc == nil {
		http.NotFound(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	if err := r.ParseForm(); err != nil {
		a.writeOIDCError(w, http.StatusBadRequest, "invalid_request", "invalid form body")
		return
	}
	client, err := a.authenticateOIDCClient(r)
	if err != nil {
		a.writeOIDCError(w, http.StatusUnauthorized, "invalid_client", "client authentication failed")
		return
	}
	a.oidc.Revoke(r.Context(), client.ClientID, r.FormValue("token"))
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{})
}

func (a *App) validateOIDCAuthorize(ctx context.Context, values *oidcAuthorizeRequest) (*store.OIDCClient, error) {
	if values.ClientID == "" || len(values.ClientID) > 128 {
		return nil, errors.New("unknown client")
	}
	client, err := a.store.GetOIDCClient(ctx, values.ClientID)
	if err != nil || !client.Enabled {
		return nil, errors.New("unknown client")
	}
	if values.ResponseType != "code" {
		return nil, errors.New("response_type must be code")
	}
	if values.RedirectURI == "" || len(values.RedirectURI) > 1024 {
		return nil, errors.New("redirect_uri is invalid")
	}
	if !containsString(client.RedirectURIs, values.RedirectURI) {
		return nil, errors.New("redirect_uri is not registered")
	}
	if values.State == "" || len(values.State) > 512 || values.Nonce == "" || len(values.Nonce) > 256 {
		return nil, errors.New("state and nonce are required")
	}
	if values.CodeChallengeMethod != "S256" || values.CodeChallenge == "" || len(values.CodeChallenge) > 128 {
		return nil, errors.New("S256 PKCE is required")
	}
	if values.Prompt != "" && values.Prompt != "none" && values.Prompt != "consent" {
		return nil, errors.New("prompt is invalid")
	}
	if len(values.Scope) > 512 {
		return nil, errors.New("scope is too long")
	}
	requested := splitScopes(values.Scope)
	allowed := splitScopes(client.Scopes)
	if !requested["openid"] {
		return nil, errors.New("openid scope is required")
	}
	for scope := range requested {
		if !allowed[scope] {
			return nil, errors.New("scope is not allowed")
		}
	}
	values.Scope = strings.Join(sortedScopes(requested), " ")
	return client, nil
}

func (a *App) oidcAuthorizeError(w http.ResponseWriter, r *http.Request, values oidcAuthorizeRequest, err error) {
	if values.RedirectURI != "" {
		if client, lookupErr := a.store.GetOIDCClient(r.Context(), values.ClientID); lookupErr == nil && containsString(client.RedirectURIs, values.RedirectURI) {
			a.oidcRedirectError(w, r, values.RedirectURI, values.State, "invalid_request", err.Error())
			return
		}
	}
	a.writeOIDCError(w, http.StatusBadRequest, "invalid_request", err.Error())
}

func (a *App) oidcRedirectError(w http.ResponseWriter, r *http.Request, redirectURI, state, code, description string) {
	params := url.Values{"error": {code}, "error_description": {description}}
	if state != "" {
		params.Set("state", state)
	}
	separator := "?"
	if strings.Contains(redirectURI, "?") {
		separator = "&"
	}
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, redirectURI+separator+params.Encode(), http.StatusFound)
}

func (a *App) authenticateOIDCClient(r *http.Request) (*store.OIDCClient, error) {
	clientID, secret := r.FormValue("client_id"), r.FormValue("client_secret")
	if header := r.Header.Get("Authorization"); strings.HasPrefix(header, "Basic ") {
		raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(header[6:]))
		if err != nil {
			return nil, errors.New("invalid basic auth")
		}
		parts := strings.SplitN(string(raw), ":", 2)
		if len(parts) != 2 {
			return nil, errors.New("invalid basic auth")
		}
		clientID, secret = parts[0], parts[1]
	}
	if clientID == "" || secret == "" || len(clientID) > 128 || len(secret) > 512 {
		return nil, errors.New("client credentials are required")
	}
	client, err := a.store.GetOIDCClient(r.Context(), clientID)
	if err != nil || !client.Enabled || client.SecretHash == "" {
		return nil, errors.New("unknown client")
	}
	expected := oidcsvc.Hash(secret)
	if subtle.ConstantTimeCompare([]byte(expected), []byte(client.SecretHash)) != 1 {
		return nil, errors.New("invalid client secret")
	}
	return client, nil
}

func (a *App) writeOIDCTokens(w http.ResponseWriter, tokens *oidcsvc.TokenSet) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	response := map[string]any{
		"access_token": tokens.AccessToken,
		"token_type":   "Bearer",
		"expires_in":   tokens.ExpiresIn,
		"id_token":     tokens.IDToken,
		"scope":        tokens.Scope,
	}
	if tokens.RefreshToken != "" {
		response["refresh_token"] = tokens.RefreshToken
	}
	_ = json.NewEncoder(w).Encode(response)
}

func (a *App) writeOIDCError(w http.ResponseWriter, status int, code, description string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code, "error_description": description})
}

func oidcAuthorizeValues(r *http.Request) oidcAuthorizeRequest {
	return oidcAuthorizeRequest{
		ClientID:            strings.TrimSpace(r.URL.Query().Get("client_id")),
		RedirectURI:         strings.TrimSpace(r.URL.Query().Get("redirect_uri")),
		ResponseType:        r.URL.Query().Get("response_type"),
		Scope:               r.URL.Query().Get("scope"),
		State:               r.URL.Query().Get("state"),
		Nonce:               r.URL.Query().Get("nonce"),
		Prompt:              r.URL.Query().Get("prompt"),
		CodeChallenge:       r.URL.Query().Get("code_challenge"),
		CodeChallengeMethod: r.URL.Query().Get("code_challenge_method"),
	}
}

func oidcAuthorizeValuesFromForm(r *http.Request) oidcAuthorizeRequest {
	return oidcAuthorizeRequest{
		ClientID:            strings.TrimSpace(r.FormValue("client_id")),
		RedirectURI:         strings.TrimSpace(r.FormValue("redirect_uri")),
		ResponseType:        r.FormValue("response_type"),
		Scope:               r.FormValue("scope"),
		State:               r.FormValue("state"),
		Nonce:               r.FormValue("nonce"),
		Prompt:              "consent",
		CodeChallenge:       r.FormValue("code_challenge"),
		CodeChallengeMethod: r.FormValue("code_challenge_method"),
	}
}

func splitScopes(value string) map[string]bool {
	out := map[string]bool{}
	for _, scope := range strings.Fields(value) {
		out[scope] = true
	}
	return out
}

func sortedScopes(scopes map[string]bool) []string {
	out := make([]string, 0, len(scopes))
	for scope := range scopes {
		out = append(out, scope)
	}
	sort.Strings(out)
	return out
}

func oidcHasScopes(consented, requested string) bool {
	have := splitScopes(consented)
	for scope := range splitScopes(requested) {
		if !have[scope] {
			return false
		}
	}
	return true
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func (a *App) handleAdminOIDC(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireRight(w, r, auth.RightManageUsers); !ok {
		return
	}
	a.renderAdminOIDC(w, r, "", "", "")
}

func (a *App) handleAdminOIDCCreate(w http.ResponseWriter, r *http.Request) {
	session, ok := a.requireRight(w, r, auth.RightManageUsers)
	if !ok {
		return
	}
	if err := a.parseOIDCAdminForm(w, r, session); err != nil {
		a.renderAdminOIDCError(w, r, err.Error())
		return
	}
	redirects, err := validateOIDCRedirects(r.FormValue("redirects"))
	if err != nil {
		a.renderAdminOIDCError(w, r, err.Error())
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" || len([]rune(name)) > 120 {
		a.renderAdminOIDCError(w, r, "应用名不能为空且不能超过 120 个字符.")
		return
	}
	logo := strings.TrimSpace(r.FormValue("logo"))
	if !validOIDCLogo(logo) {
		a.renderAdminOIDCError(w, r, "图标地址不合法.")
		return
	}
	provider := strings.TrimSpace(r.FormValue("provider"))
	if len([]rune(provider)) > 120 {
		a.renderAdminOIDCError(w, r, "提供者名不能超过 120 个字符.")
		return
	}
	clientID := "wx_" + randomHex(8)
	secret := randomBase64(32)
	client := store.OIDCClient{
		ClientID: clientID, SecretHash: oidcsvc.Hash(secret), Name: name,
		Provider: provider, LogoURL: logo,
		RedirectURIs: redirects, Scopes: "openid profile email", Enabled: true,
		AuthMethod: "client_secret_basic", CreatedBy: &session.User.ID,
	}
	if err := a.store.CreateOIDCClient(r.Context(), client); err != nil {
		a.renderAdminOIDCError(w, r, "创建 OIDC 应用失败.")
		return
	}
	_ = a.store.LogAudit(r.Context(), session.User.ID, "oidc.client.create", clientID, name)
	a.renderAdminOIDC(w, r, clientID, secret, "")
}

func (a *App) handleAdminOIDCUpdate(w http.ResponseWriter, r *http.Request) {
	session, ok := a.requireRight(w, r, auth.RightManageUsers)
	if !ok {
		return
	}
	if err := a.parseOIDCAdminForm(w, r, session); err != nil {
		a.renderAdminOIDCError(w, r, err.Error())
		return
	}
	clientID := strings.TrimSpace(r.FormValue("client_id"))
	client, err := a.store.GetOIDCClient(r.Context(), clientID)
	if err != nil {
		a.renderAdminOIDCError(w, r, "OIDC 应用不存在.")
		return
	}
	redirects, err := validateOIDCRedirects(r.FormValue("redirects"))
	if err != nil {
		a.renderAdminOIDCError(w, r, err.Error())
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" || len([]rune(name)) > 120 {
		a.renderAdminOIDCError(w, r, "应用名不能为空且不能超过 120 个字符.")
		return
	}
	logo := strings.TrimSpace(r.FormValue("logo"))
	if !validOIDCLogo(logo) {
		a.renderAdminOIDCError(w, r, "图标地址不合法.")
		return
	}
	provider := strings.TrimSpace(r.FormValue("provider"))
	if len([]rune(provider)) > 120 {
		a.renderAdminOIDCError(w, r, "提供者名不能超过 120 个字符.")
		return
	}
	wasEnabled := client.Enabled
	client.Name = name
	client.Provider = provider
	client.LogoURL = logo
	client.RedirectURIs = redirects
	client.Enabled = r.FormValue("enabled") != ""
	var updateErr error
	if wasEnabled && !client.Enabled {
		updateErr = a.store.UpdateOIDCClientAndRevoke(r.Context(), *client)
	} else {
		updateErr = a.store.UpdateOIDCClient(r.Context(), *client)
	}
	if updateErr != nil {
		a.renderAdminOIDCError(w, r, "保存 OIDC 应用失败.")
		return
	}
	_ = a.store.LogAudit(r.Context(), session.User.ID, "oidc.client.update", clientID, name)
	a.renderAdminOIDC(w, r, "", "", "")
}

func (a *App) handleAdminOIDCReset(w http.ResponseWriter, r *http.Request) {
	session, ok := a.requireRight(w, r, auth.RightManageUsers)
	if !ok {
		return
	}
	if err := a.parseOIDCAdminForm(w, r, session); err != nil {
		a.renderAdminOIDCError(w, r, err.Error())
		return
	}
	clientID := strings.TrimSpace(r.FormValue("client_id"))
	secret := randomBase64(32)
	if err := a.store.UpdateOIDCClientSecret(r.Context(), clientID, oidcsvc.Hash(secret)); err != nil {
		a.renderAdminOIDCError(w, r, "重置 secret 失败.")
		return
	}
	_ = a.store.RevokeOIDCClientSessions(r.Context(), clientID)
	_ = a.store.LogAudit(r.Context(), session.User.ID, "oidc.client.reset_secret", clientID, "")
	a.renderAdminOIDC(w, r, clientID, secret, "")
}

func (a *App) handleAdminOIDCDelete(w http.ResponseWriter, r *http.Request) {
	session, ok := a.requireRight(w, r, auth.RightManageUsers)
	if !ok {
		return
	}
	if err := a.parseOIDCAdminForm(w, r, session); err != nil {
		a.renderAdminOIDCError(w, r, err.Error())
		return
	}
	clientID := strings.TrimSpace(r.FormValue("client_id"))
	if err := a.store.DeleteOIDCClient(r.Context(), clientID, session.User.ID); err != nil {
		if errors.Is(err, store.ErrRevokeDisabled) {
			a.renderDeleteDisabled(w, r)
			return
		}
		a.renderAdminOIDCError(w, r, "删除 OIDC 应用失败.")
		return
	}
	a.renderAdminOIDC(w, r, "", "", "")
}

func (a *App) parseOIDCAdminForm(w http.ResponseWriter, r *http.Request, session *store.SessionData) error {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	if err := r.ParseForm(); err != nil || !a.auth.CheckCSRF(r, session) {
		return errors.New("请求校验失败.")
	}
	return nil
}

func (a *App) renderAdminOIDCError(w http.ResponseWriter, r *http.Request, message string) {
	a.renderAdminOIDC(w, r, "", "", message)
}

func (a *App) renderAdminOIDC(w http.ResponseWriter, r *http.Request, newClientID, newSecret, errorText string) {
	session := auth.SessionFromContext(r.Context())
	if session == nil {
		http.Redirect(w, r, "/login?next=/admin/oidc", http.StatusSeeOther)
		return
	}
	clients, err := a.store.ListOIDCClients(r.Context())
	if err != nil {
		a.renderMessage(w, r, http.StatusInternalServerError, "读取失败", "暂时无法读取 OIDC 应用.")
		return
	}
	lang := a.uiLang()
	data := adminOIDCData{
		baseData:    a.baseData(r, i18n.Text(lang, "oidc_admin")),
		NewClientID: newClientID,
		NewSecret:   newSecret,
		Error:       i18n.Message(lang, errorText),
	}
	for _, client := range clients {
		data.Clients = append(data.Clients, adminOIDCClient{
			OIDCClient:   client,
			RedirectText: strings.Join(client.RedirectURIs, "\n"),
		})
	}
	a.renderUI(w, r, http.StatusOK, "admin_oidc", data)
}

func validateOIDCRedirects(raw string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		value := strings.TrimSpace(line)
		if value == "" {
			continue
		}
		if len(value) > 1024 || strings.ContainsAny(value, " \t\r\n\"'<>`\\") {
			return nil, errors.New("回调地址格式不正确.")
		}
		parsed, err := url.Parse(value)
		if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
			return nil, errors.New("回调地址格式不正确.")
		}
		if parsed.Scheme == "http" && !isLoopbackHost(parsed.Hostname()) {
			return nil, errors.New("非本机回调地址必须使用 https.")
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return nil, errors.New("回调地址必须是 http(s).")
		}
		if !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("至少需要一个回调地址.")
	}
	return out, nil
}

func isLoopbackHost(host string) bool {
	switch strings.ToLower(host) {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}

func validOIDCLogo(value string) bool {
	if value == "" {
		return true
	}
	if len(value) > 1024 {
		return false
	}
	if strings.ContainsAny(value, "\\\r\n\t") || strings.Contains(value, "..") {
		return false
	}
	if strings.HasPrefix(value, "/") && !strings.HasPrefix(value, "//") {
		return true
	}
	parsed, err := url.Parse(value)
	return err == nil && parsed.Host != "" && parsed.User == nil && (parsed.Scheme == "http" || parsed.Scheme == "https")
}

func randomHex(size int) string {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

func randomBase64(size int) string {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return randomHex(size)
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}
