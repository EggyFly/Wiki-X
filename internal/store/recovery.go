package store

import (
	"context"
	"fmt"
)

func (s *Store) ReplaceRecoveryCodes(ctx context.Context, userID int64, hashes []string, actorID int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM recovery_codes WHERE user_id = $1`, userID); err != nil {
		return err
	}
	for _, hash := range hashes {
		if _, err := tx.Exec(ctx, `INSERT INTO recovery_codes (user_id, code_hash) VALUES ($1, $2)`, userID, hash); err != nil {
			return err
		}
	}
	var name string
	if err := tx.QueryRow(ctx, `SELECT name FROM users WHERE id = $1`, userID).Scan(&name); err != nil {
		return err
	}
	if err := logAudit(ctx, tx, actorID, "user.recovery_codes_generate", name, fmt.Sprintf("count:%d", len(hashes))); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) CountRecoveryCodes(ctx context.Context, userID int64) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM recovery_codes WHERE user_id = $1 AND used_at IS NULL`, userID).Scan(&count)
	return count, err
}

func (s *Store) ConsumeRecoveryCode(ctx context.Context, userID int64, hash string) (bool, error) {
	result, err := s.pool.Exec(ctx, `UPDATE recovery_codes SET used_at = now()
		WHERE user_id = $1 AND code_hash = $2 AND used_at IS NULL`, userID, hash)
	if err != nil {
		return false, err
	}
	return result.RowsAffected() == 1, nil
}
