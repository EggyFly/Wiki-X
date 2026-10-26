package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type OIDCClient struct {
	ClientID     string
	SecretHash   string
	Name         string
	Provider     string
	LogoURL      string
	RedirectURIs []string
	Scopes       string
	Enabled      bool
	AuthMethod   string
	CreatedBy    *int64
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type OIDCAuthCode struct {
	CodeHash      string
	ClientID      string
	UserID        int64
	RedirectURI   string
	Scopes        string
	Nonce         string
	CodeChallenge string
	Method        string
	AuthTime      time.Time
	ExpiresAt     time.Time
	UsedAt        *time.Time
}

type OIDCGrant struct {
	GrantID   string
	ClientID  string
	UserID    int64
	Scopes    string
	CreatedAt time.Time
	RevokedAt *time.Time
}

type OIDCSession struct {
	SessionID      string
	GrantID        string
	ClientID       string
	UserID         int64
	Scopes         string
	ExpiresAt      time.Time
	CreatedAt      time.Time
	LastSeen       time.Time
	RevokedAt      *time.Time
	GrantRevokedAt *time.Time
}

type OIDCRefreshToken struct {
	TokenHash string
	SessionID string
	GrantID   string
	ClientID  string
	ExpiresAt time.Time
	UsedAt    *time.Time
	RevokedAt *time.Time
}

type OIDCSigningKey struct {
	KID           string
	PrivateKeyPEM string
	CreatedAt     time.Time
	Active        bool
}

var (
	ErrOIDCClientExists = errors.New("oidc client already exists")
	ErrOIDCCodeInvalid  = errors.New("oidc authorization code is invalid")
)

func (s *Store) CreateOIDCClient(ctx context.Context, client OIDCClient) error {
	redirects, err := json.Marshal(client.RedirectURIs)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO oidc_clients
		(client_id, secret_hash, name, provider, logo_url, redirect_uris, scopes, enabled, token_endpoint_auth_method, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		client.ClientID, client.SecretHash, client.Name, client.Provider, client.LogoURL,
		redirects, client.Scopes, client.Enabled, client.AuthMethod, client.CreatedBy)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return ErrOIDCClientExists
	}
	return err
}

func (s *Store) ListOIDCClients(ctx context.Context) ([]*OIDCClient, error) {
	rows, err := s.pool.Query(ctx, `SELECT client_id, secret_hash, name, provider, logo_url, redirect_uris,
		scopes, enabled, token_endpoint_auth_method, created_by, created_at, updated_at
		FROM oidc_clients WHERE revoked_at IS NULL ORDER BY created_at DESC, client_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var clients []*OIDCClient
	for rows.Next() {
		client, err := scanOIDCClient(rows)
		if err != nil {
			return nil, err
		}
		clients = append(clients, client)
	}
	return clients, rows.Err()
}

func (s *Store) GetOIDCClient(ctx context.Context, clientID string) (*OIDCClient, error) {
	row := s.pool.QueryRow(ctx, `SELECT client_id, secret_hash, name, provider, logo_url, redirect_uris,
		scopes, enabled, token_endpoint_auth_method, created_by, created_at, updated_at
		FROM oidc_clients WHERE client_id = $1 AND revoked_at IS NULL`, clientID)
	return scanOIDCClient(row)
}

func scanOIDCClient(row interface{ Scan(...any) error }) (*OIDCClient, error) {
	var client OIDCClient
	var raw []byte
	err := row.Scan(&client.ClientID, &client.SecretHash, &client.Name, &client.Provider, &client.LogoURL,
		&raw, &client.Scopes, &client.Enabled, &client.AuthMethod, &client.CreatedBy, &client.CreatedAt, &client.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &client.RedirectURIs); err != nil {
		return nil, fmt.Errorf("decode oidc redirects: %w", err)
	}
	return &client, nil
}

func (s *Store) UpdateOIDCClient(ctx context.Context, client OIDCClient) error {
	redirects, err := json.Marshal(client.RedirectURIs)
	if err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `UPDATE oidc_clients SET name=$2, provider=$3, logo_url=$4,
		redirect_uris=$5, scopes=$6, enabled=$7, token_endpoint_auth_method=$8, updated_at=now()
		WHERE client_id=$1 AND revoked_at IS NULL`,
		client.ClientID, client.Name, client.Provider, client.LogoURL, redirects, client.Scopes, client.Enabled, client.AuthMethod)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) UpdateOIDCClientAndRevoke(ctx context.Context, client OIDCClient) error {
	redirects, err := json.Marshal(client.RedirectURIs)
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE oidc_clients SET name=$2, provider=$3, logo_url=$4,
		redirect_uris=$5, scopes=$6, enabled=$7, token_endpoint_auth_method=$8, updated_at=now()
		WHERE client_id=$1 AND revoked_at IS NULL`,
		client.ClientID, client.Name, client.Provider, client.LogoURL, redirects, client.Scopes, client.Enabled, client.AuthMethod)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if err := revokeOIDCClientSessionsTx(ctx, tx, client.ClientID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) UpdateOIDCClientSecret(ctx context.Context, clientID, hash string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE oidc_clients SET secret_hash=$2, updated_at=now() WHERE client_id=$1 AND revoked_at IS NULL`, clientID, hash)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteOIDCClient(ctx context.Context, clientID string, actorID int64) error {
	if !s.revokeEnabled(SectionOIDCClient) {
		return ErrRevokeDisabled
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE oidc_clients
		SET revoked_at=now(), revoked_by=NULLIF($2,0), revoke_reason='deleted'
		WHERE client_id=$1 AND revoked_at IS NULL`, clientID, actorID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if err := revokeOIDCClientSessionsTx(ctx, tx, clientID); err != nil {
		return err
	}
	if err := logAudit(ctx, tx, actorID, "oidc.client.delete", clientID, "revoked"); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) CreateOIDCAuthCode(ctx context.Context, code OIDCAuthCode) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO oidc_auth_codes
		(code_hash, client_id, user_id, redirect_uri, scopes, nonce, code_challenge, code_challenge_method, auth_time, expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		code.CodeHash, code.ClientID, code.UserID, code.RedirectURI, code.Scopes, code.Nonce,
		code.CodeChallenge, code.Method, code.AuthTime, code.ExpiresAt)
	return err
}

func (s *Store) ConsumeOIDCAuthCode(ctx context.Context, hash string) (*OIDCAuthCode, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	row := tx.QueryRow(ctx, `SELECT code_hash, client_id, user_id, redirect_uri, scopes, nonce,
		code_challenge, code_challenge_method, auth_time, expires_at, used_at
		FROM oidc_auth_codes WHERE code_hash=$1 FOR UPDATE`, hash)
	var code OIDCAuthCode
	if err := row.Scan(&code.CodeHash, &code.ClientID, &code.UserID, &code.RedirectURI, &code.Scopes, &code.Nonce,
		&code.CodeChallenge, &code.Method, &code.AuthTime, &code.ExpiresAt, &code.UsedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrOIDCCodeInvalid
		}
		return nil, err
	}
	if code.UsedAt != nil || time.Now().After(code.ExpiresAt) {
		return nil, ErrOIDCCodeInvalid
	}
	if _, err := tx.Exec(ctx, `UPDATE oidc_auth_codes SET used_at=now() WHERE code_hash=$1`, hash); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &code, nil
}

func (s *Store) SaveOIDCConsent(ctx context.Context, userID int64, clientID, scopes string) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO oidc_consents (user_id, client_id, scopes, updated_at)
		VALUES ($1,$2,$3,now())
		ON CONFLICT (user_id, client_id) DO UPDATE SET scopes=$3, updated_at=now()`,
		userID, clientID, scopes)
	return err
}

func (s *Store) GetOIDCConsent(ctx context.Context, userID int64, clientID string) (string, error) {
	var scopes string
	err := s.pool.QueryRow(ctx, `SELECT scopes FROM oidc_consents WHERE user_id=$1 AND client_id=$2`, userID, clientID).Scan(&scopes)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return scopes, err
}

func (s *Store) CreateOIDCGrantSession(ctx context.Context, grantID, sessionID, clientID string, userID int64, scopes string, expiresAt time.Time) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `INSERT INTO oidc_grants (grant_id, client_id, user_id, scopes)
		VALUES ($1,$2,$3,$4)`, grantID, clientID, userID, scopes); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO oidc_sessions (session_id, grant_id, client_id, user_id, expires_at)
		VALUES ($1,$2,$3,$4,$5)`, sessionID, grantID, clientID, userID, expiresAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) GetOIDCSession(ctx context.Context, sessionID string) (*OIDCSession, error) {
	var session OIDCSession
	err := s.pool.QueryRow(ctx, `SELECT s.session_id, s.grant_id, s.client_id, s.user_id,
		g.scopes, s.expires_at, s.created_at, s.last_seen, s.revoked_at, g.revoked_at
		FROM oidc_sessions s JOIN oidc_grants g ON g.grant_id=s.grant_id
		WHERE s.session_id=$1`, sessionID).Scan(&session.SessionID, &session.GrantID, &session.ClientID,
		&session.UserID, &session.Scopes, &session.ExpiresAt, &session.CreatedAt, &session.LastSeen,
		&session.RevokedAt, &session.GrantRevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &session, err
}

func (s *Store) TouchOIDCSession(ctx context.Context, sessionID string) error {
	_, err := s.pool.Exec(ctx, `UPDATE oidc_sessions SET last_seen=now() WHERE session_id=$1`, sessionID)
	return err
}

func (s *Store) RevokeOIDCSession(ctx context.Context, sessionID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE oidc_sessions SET revoked_at=COALESCE(revoked_at, now()) WHERE session_id=$1`, sessionID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE oidc_grants SET revoked_at=COALESCE(revoked_at, now())
		WHERE grant_id=(SELECT grant_id FROM oidc_sessions WHERE session_id=$1)`, sessionID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE oidc_refresh_tokens SET revoked_at=COALESCE(revoked_at, now()) WHERE session_id=$1`, sessionID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) RevokeOIDCClientSessions(ctx context.Context, clientID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := revokeOIDCClientSessionsTx(ctx, tx, clientID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func revokeOIDCClientSessionsTx(ctx context.Context, tx pgx.Tx, clientID string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM oidc_auth_codes WHERE client_id=$1`, clientID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE oidc_sessions SET revoked_at=COALESCE(revoked_at, now()) WHERE client_id=$1`, clientID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE oidc_grants SET revoked_at=COALESCE(revoked_at, now()) WHERE client_id=$1`, clientID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE oidc_refresh_tokens SET revoked_at=COALESCE(revoked_at, now()) WHERE client_id=$1`, clientID); err != nil {
		return err
	}
	return nil
}

func (s *Store) CreateOIDCRefreshToken(ctx context.Context, hash, sessionID, grantID, clientID string, expiresAt time.Time) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO oidc_refresh_tokens
		(token_hash, session_id, grant_id, client_id, expires_at) VALUES ($1,$2,$3,$4,$5)`,
		hash, sessionID, grantID, clientID, expiresAt)
	return err
}

func (s *Store) ConsumeOIDCRefreshToken(ctx context.Context, hash string) (*OIDCRefreshToken, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	row := tx.QueryRow(ctx, `SELECT token_hash, session_id, grant_id, client_id, expires_at, used_at, revoked_at
		FROM oidc_refresh_tokens WHERE token_hash=$1 FOR UPDATE`, hash)
	var token OIDCRefreshToken
	if err := row.Scan(&token.TokenHash, &token.SessionID, &token.GrantID, &token.ClientID,
		&token.ExpiresAt, &token.UsedAt, &token.RevokedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if token.UsedAt == nil && token.RevokedAt == nil && time.Now().Before(token.ExpiresAt) {
		if _, err := tx.Exec(ctx, `UPDATE oidc_refresh_tokens SET used_at=now() WHERE token_hash=$1`, hash); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &token, nil
}

func (s *Store) GetOIDCRefreshToken(ctx context.Context, hash string) (*OIDCRefreshToken, error) {
	var token OIDCRefreshToken
	err := s.pool.QueryRow(ctx, `SELECT token_hash, session_id, grant_id, client_id, expires_at, used_at, revoked_at
		FROM oidc_refresh_tokens WHERE token_hash=$1`, hash).Scan(&token.TokenHash, &token.SessionID,
		&token.GrantID, &token.ClientID, &token.ExpiresAt, &token.UsedAt, &token.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &token, err
}

func (s *Store) GetOrCreateOIDCSigningKey(ctx context.Context, key OIDCSigningKey) (*OIDCSigningKey, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, int64(0x4f494443)); err != nil {
		return nil, err
	}
	var existing OIDCSigningKey
	err = tx.QueryRow(ctx, `SELECT kid, private_key_pem, created_at FROM oidc_signing_keys
		WHERE active=true ORDER BY created_at DESC LIMIT 1`).Scan(&existing.KID, &existing.PrivateKeyPEM, &existing.CreatedAt)
	if err == nil {
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return &existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO oidc_signing_keys (kid, private_key_pem, active)
		VALUES ($1,$2,true)`, key.KID, key.PrivateKeyPEM); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &key, nil
}

func (s *Store) SaveOIDCSigningKey(ctx context.Context, key OIDCSigningKey) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE oidc_signing_keys SET active=false WHERE active=true`); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO oidc_signing_keys (kid, private_key_pem, active)
		VALUES ($1,$2,true) ON CONFLICT (kid) DO UPDATE SET private_key_pem=$2, active=true`,
		key.KID, key.PrivateKeyPEM); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) GetActiveOIDCSigningKey(ctx context.Context) (*OIDCSigningKey, error) {
	var key OIDCSigningKey
	err := s.pool.QueryRow(ctx, `SELECT kid, private_key_pem, created_at
		FROM oidc_signing_keys WHERE active=true ORDER BY created_at DESC LIMIT 1`).Scan(&key.KID, &key.PrivateKeyPEM, &key.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	key.Active = true
	return &key, err
}

func (s *Store) ListOIDCSigningKeys(ctx context.Context) ([]*OIDCSigningKey, error) {
	rows, err := s.pool.Query(ctx, `SELECT kid, private_key_pem, created_at, active
		FROM oidc_signing_keys ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []*OIDCSigningKey
	for rows.Next() {
		key := &OIDCSigningKey{}
		if err := rows.Scan(&key.KID, &key.PrivateKeyPEM, &key.CreatedAt, &key.Active); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

func (s *Store) UpdateOIDCSigningKeyPEM(ctx context.Context, kid, pem string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE oidc_signing_keys SET private_key_pem=$2 WHERE kid=$1`, kid, pem)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) CleanupOIDC(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM oidc_auth_codes WHERE expires_at < now() - interval '1 hour'`); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM oidc_refresh_tokens
		WHERE expires_at < now() - interval '1 day'
		OR (revoked_at IS NOT NULL AND revoked_at < now() - interval '7 days')`); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM oidc_sessions WHERE expires_at < now() - interval '1 day'`); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM oidc_grants g
		WHERE g.created_at < now() - interval '30 days'
		AND NOT EXISTS (SELECT 1 FROM oidc_sessions s WHERE s.grant_id=g.grant_id)`)
	return err
}
