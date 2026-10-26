package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *Store) DeleteUser(ctx context.Context, userID, actorID int64, reason string) (string, error) {
	if !s.revokeEnabled(SectionUser) {
		return "", ErrRevokeDisabled
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('wiki_x_user_admin')::bigint)`); err != nil {
		return "", err
	}
	var name string
	var disabled bool
	err = tx.QueryRow(ctx, `SELECT name, disabled FROM users WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, userID).Scan(&name, &disabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if !disabled {
		hasManage, err := s.userHasRightTx(ctx, tx, userID, RightManageUsers)
		if err != nil {
			return "", err
		}
		if hasManage {
			managers, err := countManagersExceptUser(ctx, tx, userID)
			if err != nil {
				return "", err
			}
			if managers == 0 {
				return "", ErrLastAdmin
			}
		}
	}
	suffix, err := randomSuffix()
	if err != nil {
		return "", err
	}
	deletedName := fmt.Sprintf("deleted-%d-%s", userID, suffix)
	if _, err := tx.Exec(ctx, `UPDATE users SET name = $2, email = '', password_hash = '', disabled = true,
		totp_secret = '', totp_enabled = false, totp_last_step = 0, totp_secret_set_at = '1970-01-01 00:00:00+00',
		email_verified = false, signup_pending = false, deleted_at = now(), deleted_by = NULLIF($3, 0)
		WHERE id = $1`, userID, deletedName, actorID); err != nil {
		return "", err
	}
	for _, statement := range []string{
		`DELETE FROM identities WHERE user_id = $1`,
		`DELETE FROM sessions WHERE user_id = $1`,
		`DELETE FROM recovery_codes WHERE user_id = $1`,
		`DELETE FROM email_tokens WHERE user_id = $1`,
		`UPDATE comments SET user_id = NULL WHERE user_id = $1`,
		`UPDATE comment_reports SET reporter_id = NULL WHERE reporter_id = $1`,
		`UPDATE comment_reports SET resolved_by = NULL WHERE resolved_by = $1`,
		`UPDATE media SET uploader_id = NULL WHERE uploader_id = $1`,
		`UPDATE page_revisions SET author_id = NULL WHERE author_id = $1`,
		`UPDATE pages SET deleted_by = NULL WHERE deleted_by = $1`,
	} {
		if _, err := tx.Exec(ctx, statement, userID); err != nil {
			return "", err
		}
	}
	for _, statement := range []string{
		`UPDATE watchlist SET revoked_at=now(), revoked_by=NULLIF($2,0), revoke_reason='user deleted'
			WHERE user_id=$1 AND revoked_at IS NULL`,
		`UPDATE user_groups SET revoked_at=now(), revoked_by=NULLIF($2,0), revoke_reason='user deleted'
			WHERE user_id=$1 AND revoked_at IS NULL`,
	} {
		if _, err := tx.Exec(ctx, statement, userID, actorID); err != nil {
			return "", err
		}
	}
	if err := logAudit(ctx, tx, actorID, "user.delete", name, reason); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return name, nil
}

type UserExport struct {
	GeneratedAt time.Time            `json:"generated_at"`
	Profile     UserExportProfile    `json:"profile"`
	Identities  []UserExportIdentity `json:"identities"`
	Sessions    []UserExportSession  `json:"sessions"`
	Comments    []UserExportComment  `json:"comments"`
	Revisions   []UserExportRevision `json:"revisions"`
	Watchlist   []string             `json:"watchlist"`
}

type UserExportProfile struct {
	ID            int64     `json:"id"`
	Name          string    `json:"name"`
	Email         string    `json:"email"`
	EmailVerified bool      `json:"email_verified"`
	CreatedAt     time.Time `json:"created_at"`
	Groups        []string  `json:"groups"`
}

type UserExportIdentity struct {
	Provider  string    `json:"provider"`
	Username  string    `json:"username"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

type UserExportSession struct {
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	IP        string    `json:"ip"`
	UserAgent string    `json:"user_agent"`
}

type UserExportComment struct {
	Route     string    `json:"route"`
	Body      string    `json:"body"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type UserExportRevision struct {
	Route         string    `json:"route"`
	Title         string    `json:"title"`
	Body          string    `json:"body"`
	BodyTruncated bool      `json:"body_truncated"`
	Comment       string    `json:"comment"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
}

func (s *Store) ExportUserData(ctx context.Context, userID int64) (*UserExport, error) {
	result := &UserExport{GeneratedAt: time.Now().UTC()}
	if err := s.pool.QueryRow(ctx, `SELECT u.id, u.name, u.email, u.email_verified, u.created_at,
			COALESCE(array_agg(g.name ORDER BY g.name) FILTER (WHERE g.name IS NOT NULL), '{}')
		FROM users u LEFT JOIN user_groups ug ON ug.user_id = u.id AND ug.revoked_at IS NULL
		LEFT JOIN groups g ON g.id = ug.group_id AND g.revoked_at IS NULL
		WHERE u.id = $1 AND u.deleted_at IS NULL GROUP BY u.id`, userID).
		Scan(&result.Profile.ID, &result.Profile.Name, &result.Profile.Email, &result.Profile.EmailVerified,
			&result.Profile.CreatedAt, &result.Profile.Groups); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	identityRows, err := s.pool.Query(ctx, `SELECT provider, username, email, created_at FROM identities WHERE user_id = $1 ORDER BY provider`, userID)
	if err != nil {
		return nil, err
	}
	for identityRows.Next() {
		var item UserExportIdentity
		if err := identityRows.Scan(&item.Provider, &item.Username, &item.Email, &item.CreatedAt); err != nil {
			identityRows.Close()
			return nil, err
		}
		result.Identities = append(result.Identities, item)
	}
	identityRows.Close()
	if err := identityRows.Err(); err != nil {
		return nil, err
	}

	sessionRows, err := s.pool.Query(ctx, `SELECT created_at, expires_at, ip, user_agent FROM sessions
		WHERE user_id = $1 ORDER BY created_at DESC LIMIT 100`, userID)
	if err != nil {
		return nil, err
	}
	for sessionRows.Next() {
		var item UserExportSession
		if err := sessionRows.Scan(&item.CreatedAt, &item.ExpiresAt, &item.IP, &item.UserAgent); err != nil {
			sessionRows.Close()
			return nil, err
		}
		result.Sessions = append(result.Sessions, item)
	}
	sessionRows.Close()
	if err := sessionRows.Err(); err != nil {
		return nil, err
	}

	commentRows, err := s.pool.Query(ctx, `SELECT route, body, status, created_at FROM comments
		WHERE user_id = $1 ORDER BY created_at DESC LIMIT 5000`, userID)
	if err != nil {
		return nil, err
	}
	for commentRows.Next() {
		var item UserExportComment
		if err := commentRows.Scan(&item.Route, &item.Body, &item.Status, &item.CreatedAt); err != nil {
			commentRows.Close()
			return nil, err
		}
		result.Comments = append(result.Comments, item)
	}
	commentRows.Close()
	if err := commentRows.Err(); err != nil {
		return nil, err
	}

	revisionRows, err := s.pool.Query(ctx, `SELECT p.route, r.title, left(r.body, 20000), char_length(r.body) > 20000,
			r.comment, r.status, r.created_at
		FROM page_revisions r JOIN pages p ON p.id = r.page_id
		WHERE r.author_id = $1 ORDER BY r.created_at DESC LIMIT 1000`, userID)
	if err != nil {
		return nil, err
	}
	for revisionRows.Next() {
		var item UserExportRevision
		if err := revisionRows.Scan(&item.Route, &item.Title, &item.Body, &item.BodyTruncated,
			&item.Comment, &item.Status, &item.CreatedAt); err != nil {
			revisionRows.Close()
			return nil, err
		}
		result.Revisions = append(result.Revisions, item)
	}
	revisionRows.Close()
	if err := revisionRows.Err(); err != nil {
		return nil, err
	}

	watchRows, err := s.pool.Query(ctx, `SELECT route FROM watchlist WHERE user_id = $1 ORDER BY route`, userID)
	if err != nil {
		return nil, err
	}
	defer watchRows.Close()
	for watchRows.Next() {
		var route string
		if err := watchRows.Scan(&route); err != nil {
			return nil, err
		}
		result.Watchlist = append(result.Watchlist, route)
	}
	return result, watchRows.Err()
}
