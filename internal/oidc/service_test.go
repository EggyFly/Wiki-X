package oidc

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/EggyFly/Wiki-X/internal/config"
	"github.com/EggyFly/Wiki-X/internal/store"
)

func TestAuthorizationCodeFlow(t *testing.T) {
	databaseURL := os.Getenv("TEST_POSTGRES_URL_OIDC")
	if databaseURL == "" {
		t.Skip("set TEST_POSTGRES_URL_OIDC to run oidc integration tests")
	}
	ctx := context.Background()
	data, err := store.Open(ctx, config.DatabaseConfig{URL: databaseURL, MaxConns: 4})
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	if err := data.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	user, err := data.CreateUser(ctx, "alice"+suffix, "alice"+suffix+"@example.com", "hash", false)
	if err != nil {
		t.Fatal(err)
	}
	client := store.OIDCClient{
		ClientID:     "test-app-" + suffix,
		SecretHash:   Hash("client-secret"),
		Name:         "Test App",
		Provider:     "Test",
		RedirectURIs: []string{"https://app.example.com/callback"},
		Scopes:       "openid profile email",
		Enabled:      true,
		AuthMethod:   "client_secret_basic",
	}
	if err := data.CreateOIDCClient(ctx, client); err != nil {
		t.Fatal(err)
	}
	service, err := New(ctx, config.OIDCConfig{
		Enabled: true, Issuer: "https://issuer.example.com", KeyEncryptionKey: testKeyEncryptionKey,
		AccessTTL: "5m", IDTTL: "5m", RefreshTTL: "1h", CodeTTL: "2m",
	}, data)
	if err != nil {
		t.Fatal(err)
	}
	encKey, err := decodeKeyEncryptionKey(testKeyEncryptionKey)
	if err != nil {
		t.Fatal(err)
	}
	signingKeys, err := data.GetActiveOIDCSigningKey(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(signingKeys.PrivateKeyPEM, oidcKeyPrefix) {
		t.Fatal("signing key was not encrypted")
	}
	plainKey, err := decryptPrivateKeyPEM(signingKeys.PrivateKeyPEM, encKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := data.UpdateOIDCSigningKeyPEM(ctx, signingKeys.KID, plainKey); err != nil {
		t.Fatal(err)
	}
	service, err = New(ctx, config.OIDCConfig{
		Enabled: true, Issuer: "https://issuer.example.com", KeyEncryptionKey: testKeyEncryptionKey,
		AccessTTL: "5m", IDTTL: "5m", RefreshTTL: "1h", CodeTTL: "2m",
	}, data)
	if err != nil {
		t.Fatal(err)
	}
	signingKeys, err = data.GetActiveOIDCSigningKey(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(signingKeys.PrivateKeyPEM, oidcKeyPrefix) {
		t.Fatal("legacy signing key was not migrated")
	}
	if err := data.SaveOIDCSigningKey(ctx, store.OIDCSigningKey{
		KID: "legacy-active", PrivateKeyPEM: plainKey,
	}); err != nil {
		t.Fatal(err)
	}
	service, err = New(ctx, config.OIDCConfig{
		Enabled: true, Issuer: "https://issuer.example.com", KeyEncryptionKey: testKeyEncryptionKey,
		AccessTTL: "5m", IDTTL: "5m", RefreshTTL: "1h", CodeTTL: "2m",
	}, data)
	if err != nil {
		t.Fatal(err)
	}
	allKeys, err := data.ListOIDCSigningKeys(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range allKeys {
		if !strings.HasPrefix(item.PrivateKeyPEM, oidcKeyPrefix) {
			t.Fatalf("signing key %s was not encrypted", item.KID)
		}
	}
	verifier := "test-verifier-1234567890"
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	code, err := service.CreateAuthCode(ctx, client.ClientID, user.ID, client.RedirectURIs[0],
		"openid profile email", "nonce-1", challenge, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	tokens, _, err := service.ExchangeCode(ctx, &client, code, client.RedirectURIs[0], verifier)
	if err != nil {
		t.Fatal(err)
	}
	if tokens.AccessToken == "" || tokens.IDToken == "" || tokens.RefreshToken == "" {
		t.Fatal("token set is incomplete")
	}
	claims, err := service.UserInfo(ctx, tokens.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if claims["preferred_username"] != "alice"+suffix || claims["email"] != "alice"+suffix+"@example.com" {
		t.Fatalf("claims = %#v", claims)
	}
	openIDCode, err := service.CreateAuthCode(ctx, client.ClientID, user.ID, client.RedirectURIs[0],
		"openid", "nonce-2", challenge, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	openIDTokens, _, err := service.ExchangeCode(ctx, &client, openIDCode, client.RedirectURIs[0], verifier)
	if err != nil {
		t.Fatal(err)
	}
	openIDClaims, err := service.UserInfo(ctx, openIDTokens.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := openIDClaims["preferred_username"]; ok {
		t.Fatalf("openid claims should not include profile: %#v", openIDClaims)
	}
	if _, ok := openIDClaims["email"]; ok {
		t.Fatalf("openid claims should not include email: %#v", openIDClaims)
	}
	refreshed, err := service.Refresh(ctx, &client, tokens.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.AccessToken == "" || refreshed.RefreshToken == tokens.RefreshToken {
		t.Fatal("refresh did not rotate")
	}
	if _, err := service.Refresh(ctx, &client, tokens.RefreshToken); err == nil {
		t.Fatal("used refresh token should fail")
	}
	if _, err := service.UserInfo(ctx, refreshed.AccessToken); err != nil {
		t.Fatal("immediate reuse should not revoke the session")
	}
	service.Revoke(ctx, client.ClientID, refreshed.AccessToken)
	if _, err := service.UserInfo(ctx, refreshed.AccessToken); err == nil {
		t.Fatal("revoked access token should fail")
	}
	client.Enabled = false
	if err := data.UpdateOIDCClient(ctx, client); err != nil {
		t.Fatal(err)
	}
	if _, err := service.UserInfo(ctx, tokens.AccessToken); err == nil {
		t.Fatal("disabled client access token should fail")
	}
	discovery := service.Discovery()
	if discovery["issuer"] != "https://issuer.example.com" {
		t.Fatalf("discovery = %#v", discovery)
	}
	jwks := service.JWKS()
	if len(jwks["keys"].([]map[string]any)) != 1 {
		t.Fatalf("jwks = %#v", jwks)
	}
}
