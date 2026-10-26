package store

import (
	"context"
	"testing"
	"time"
)

func TestCleanupOIDC(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	user, err := s.CreateUser(ctx, "oidc-cleanup", "oidc-cleanup@example.com", "hash", false)
	if err != nil {
		t.Fatal(err)
	}
	client := OIDCClient{
		ClientID:     "cleanup-client",
		SecretHash:   "hash",
		Name:         "Cleanup",
		RedirectURIs: []string{"https://app.example.com/callback"},
		Scopes:       "openid",
		Enabled:      true,
		AuthMethod:   "client_secret_basic",
	}
	if err := s.CreateOIDCClient(ctx, client); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := s.CreateOIDCAuthCode(ctx, OIDCAuthCode{
		CodeHash: "expired-code", ClientID: client.ClientID, UserID: user.ID,
		RedirectURI: client.RedirectURIs[0], Scopes: "openid", CodeChallenge: "challenge",
		Method: "S256", AuthTime: now, ExpiresAt: now.Add(-2 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateOIDCAuthCode(ctx, OIDCAuthCode{
		CodeHash: "active-code", ClientID: client.ClientID, UserID: user.ID,
		RedirectURI: client.RedirectURIs[0], Scopes: "openid", CodeChallenge: "challenge",
		Method: "S256", AuthTime: now, ExpiresAt: now.Add(2 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateOIDCGrantSession(ctx, "grant-1", "session-1", client.ClientID, user.ID, "openid", now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateOIDCRefreshToken(ctx, "expired-refresh", "session-1", "grant-1", client.ClientID, now.Add(-48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateOIDCRefreshToken(ctx, "active-refresh", "session-1", "grant-1", client.ClientID, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.CleanupOIDC(ctx); err != nil {
		t.Fatal(err)
	}
	checks := []struct {
		query string
		want  int
	}{
		{`SELECT count(*) FROM oidc_auth_codes WHERE code_hash='expired-code'`, 0},
		{`SELECT count(*) FROM oidc_auth_codes WHERE code_hash='active-code'`, 1},
		{`SELECT count(*) FROM oidc_refresh_tokens WHERE token_hash='expired-refresh'`, 0},
		{`SELECT count(*) FROM oidc_refresh_tokens WHERE token_hash='active-refresh'`, 1},
	}
	for _, check := range checks {
		var count int
		if err := s.pool.QueryRow(ctx, check.query).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != check.want {
			t.Fatalf("%s = %d, want %d", check.query, count, check.want)
		}
	}
}
