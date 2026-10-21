package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *Store) CreateSession(ctx context.Context, tokenHash string, userID int64, csrfToken string, expiresAt time.Time, userAgent, ip string) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO sessions (token_hash, user_id, csrf_token, expires_at, user_agent, ip)
		VALUES ($1, $2, $3, $4, $5, $6)`, tokenHash, userID, csrfToken, expiresAt, userAgent, ip)
	return err
}

func (s *Store) GetSession(ctx context.Context, tokenHash string) (*SessionData, error) {
	var user User
	var csrfToken string
	var expiresAt time.Time
	err := s.pool.QueryRow(ctx, `SELECT u.id, u.name, u.email, u.disabled, u.totp_enabled, u.created_at, s.csrf_token, s.expires_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = $1 AND s.expires_at > now() AND u.disabled = false`, tokenHash).
		Scan(&user.ID, &user.Name, &user.Email, &user.Disabled, &user.TOTPEnabled, &user.CreatedAt, &csrfToken, &expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rights, err := s.userRights(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	user.Rights = rights
	return &SessionData{User: &user, CSRFToken: csrfToken, ExpiresAt: expiresAt}, nil
}

func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, tokenHash)
	return err
}

func (s *Store) DeleteExpiredSessions(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at <= now()`)
	return err
}
