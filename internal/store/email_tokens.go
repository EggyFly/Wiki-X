package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	EmailTokenVerify = "verify_email"
	EmailTokenReset  = "reset_password"
)

func (s *Store) CreateEmailToken(ctx context.Context, userID int64, kind, tokenHash string, expiresAt time.Time) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `UPDATE email_tokens SET used_at = now()
		WHERE user_id = $1 AND kind = $2 AND used_at IS NULL`, userID, kind); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO email_tokens (user_id, kind, token_hash, expires_at)
		VALUES ($1, $2, $3, $4)`, userID, kind, tokenHash, expiresAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) ConsumeEmailToken(ctx context.Context, kind, tokenHash string) (int64, error) {
	var userID int64
	err := s.pool.QueryRow(ctx, `UPDATE email_tokens SET used_at = now()
		WHERE token_hash = $1 AND kind = $2 AND used_at IS NULL AND expires_at > now()
		RETURNING user_id`, tokenHash, kind).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	return userID, nil
}

func (s *Store) SetEmailVerified(ctx context.Context, userID int64, verified bool) error {
	result, err := s.pool.Exec(ctx, `UPDATE users SET email_verified = $2 WHERE id = $1`, userID, verified)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) UpdateEmail(ctx context.Context, userID int64, email string) error {
	result, err := s.pool.Exec(ctx, `UPDATE users SET email = $2, email_verified = false WHERE id = $1`, userID, email)
	if err != nil {
		if isEmailUniqueViolation(err) {
			return ErrEmailExists
		}
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteExpiredEmailTokens(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM email_tokens WHERE expires_at <= now() OR used_at IS NOT NULL`)
	return err
}
