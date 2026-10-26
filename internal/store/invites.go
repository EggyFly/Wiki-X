package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrInviteInvalid = errors.New("invite code is invalid")

type InviteCode struct {
	ID            int64
	Label         string
	MaxUses       int
	Uses          int
	ExpiresAt     *time.Time
	CreatedByName string
	CreatedAt     time.Time
}

func (i *InviteCode) Status() string {
	if i.Uses >= i.MaxUses {
		return "used"
	}
	if i.ExpiresAt != nil && time.Now().After(*i.ExpiresAt) {
		return "expired"
	}
	return "active"
}

func (i *InviteCode) UsesText() string {
	return fmt.Sprintf("%d / %d", i.Uses, i.MaxUses)
}

type RegisterInput struct {
	Name         string
	Email        string
	PasswordHash string
	Group        string
	InviteHash   string
	Pending      bool
	Detail       string
}

func (s *Store) RegisterUser(ctx context.Context, in RegisterInput) (*User, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	if in.InviteHash != "" {
		var inviteID int64
		err := tx.QueryRow(ctx, `UPDATE invite_codes SET uses = uses + 1
			WHERE code_hash = $1 AND revoked_at IS NULL AND uses < max_uses AND (expires_at IS NULL OR expires_at > now())
			RETURNING id`, in.InviteHash).Scan(&inviteID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrInviteInvalid
		}
		if err != nil {
			return nil, err
		}
	}

	var userID int64
	err = tx.QueryRow(ctx, `INSERT INTO users (name, email, password_hash, disabled, signup_pending)
		VALUES ($1, $2, $3, $4, $4) RETURNING id`,
		strings.TrimSpace(in.Name), strings.TrimSpace(in.Email), in.PasswordHash, in.Pending).Scan(&userID)
	if err != nil {
		if isEmailUniqueViolation(err) {
			return nil, ErrEmailExists
		}
		if isUniqueViolation(err) {
			return nil, ErrUserExists
		}
		return nil, err
	}
	group := strings.TrimSpace(in.Group)
	if group == "" {
		group = "user"
	}
	result, err := tx.Exec(ctx, `INSERT INTO user_groups (user_id, group_id)
		SELECT $1, id FROM groups WHERE name = $2 AND revoked_at IS NULL`, userID, group)
	if err != nil {
		return nil, err
	}
	if result.RowsAffected() == 0 {
		return nil, ErrBadGroup
	}
	if err := logAudit(ctx, tx, 0, "user.register", in.Name, in.Detail); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.UserByID(ctx, userID)
}

func (s *Store) CompleteEmailVerification(ctx context.Context, userID int64) (bool, error) {
	var pending bool
	err := s.pool.QueryRow(ctx, `WITH target AS (
			SELECT signup_pending FROM users WHERE id = $1
		)
		UPDATE users SET email_verified = true,
			disabled = CASE WHEN (SELECT signup_pending FROM target) THEN false ELSE disabled END,
			signup_pending = false
		WHERE id = $1
		RETURNING (SELECT signup_pending FROM target)`, userID).Scan(&pending)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, err
	}
	return pending, nil
}

func (s *Store) CreateInvite(ctx context.Context, codeHash, label string, maxUses int, expiresAt *time.Time, actorID int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `INSERT INTO invite_codes (code_hash, label, max_uses, expires_at, created_by)
		VALUES ($1, $2, $3, $4, NULLIF($5, 0))`, codeHash, label, maxUses, expiresAt, actorID); err != nil {
		return err
	}
	if err := logAudit(ctx, tx, actorID, "invite.create", label, fmt.Sprintf("max_uses:%d", maxUses)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) ListInvites(ctx context.Context, limit int) ([]*InviteCode, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.read().Query(ctx, `SELECT i.id, i.label, i.max_uses, i.uses, i.expires_at,
			COALESCE(u.name, ''), i.created_at
		FROM invite_codes i
		LEFT JOIN users u ON u.id = i.created_by
		WHERE i.revoked_at IS NULL
		ORDER BY i.created_at DESC, i.id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var invites []*InviteCode
	for rows.Next() {
		invite := &InviteCode{}
		if err := rows.Scan(&invite.ID, &invite.Label, &invite.MaxUses, &invite.Uses,
			&invite.ExpiresAt, &invite.CreatedByName, &invite.CreatedAt); err != nil {
			return nil, err
		}
		invites = append(invites, invite)
	}
	return invites, rows.Err()
}

func (s *Store) DeleteInvite(ctx context.Context, id, actorID int64) error {
	if !s.revokeEnabled(SectionInvite) {
		return ErrRevokeDisabled
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var label string
	err = tx.QueryRow(ctx, `UPDATE invite_codes
		SET revoked_at=now(), revoked_by=NULLIF($2,0), revoke_reason='deleted'
		WHERE id=$1 AND revoked_at IS NULL RETURNING label`, id, actorID).Scan(&label)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if err := logAudit(ctx, tx, actorID, "invite.delete", label, fmt.Sprintf("id:%d;revoked", id)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
