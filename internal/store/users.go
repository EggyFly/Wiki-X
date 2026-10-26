package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/jackc/pgx/v5"
)

func (s *Store) CreateUser(ctx context.Context, name, email, passwordHash string, admin bool) (*User, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("user name is empty")
	}
	email = strings.TrimSpace(email)
	group := "user"
	if admin {
		group = "admin"
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `INSERT INTO users (name, email, password_hash) VALUES ($1, $2, $3)`,
		name, email, passwordHash); err != nil {
		if isEmailUniqueViolation(err) {
			return nil, ErrEmailExists
		}
		if isUniqueViolation(err) {
			return nil, fmt.Errorf("%w: %s", ErrUserExists, name)
		}
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO user_groups (user_id, group_id)
		SELECT u.id, g.id FROM users u JOIN groups g ON g.name = $2 AND g.revoked_at IS NULL WHERE u.name = $1`, name, group); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.UserByName(ctx, name)
}

func (s *Store) UserByName(ctx context.Context, name string) (*User, error) {
	return s.scanUser(ctx, `SELECT id, name, email, email_verified, password_hash, disabled, totp_secret, totp_enabled, totp_secret_set_at, created_at FROM users WHERE name = $1 AND deleted_at IS NULL`, name)
}

func (s *Store) UserByID(ctx context.Context, id int64) (*User, error) {
	return s.scanUser(ctx, `SELECT id, name, email, email_verified, password_hash, disabled, totp_secret, totp_enabled, totp_secret_set_at, created_at FROM users WHERE id = $1 AND deleted_at IS NULL`, id)
}

func (s *Store) UserByEmail(ctx context.Context, email string) (*User, error) {
	return s.scanUser(ctx, `SELECT id, name, email, email_verified, password_hash, disabled, totp_secret, totp_enabled, totp_secret_set_at, created_at FROM users WHERE lower(email) = lower($1) AND email <> '' ORDER BY id LIMIT 1`, email)
}

func (s *Store) scanUser(ctx context.Context, query string, arg any) (*User, error) {
	user := &User{Rights: make(map[string]bool)}
	err := s.pool.QueryRow(ctx, query, arg).Scan(&user.ID, &user.Name, &user.Email, &user.EmailVerified, &user.PasswordHash, &user.Disabled, &user.TOTPSecret, &user.TOTPEnabled, &user.TOTPSecretSetAt, &user.CreatedAt)
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
	groups, err := s.userGroups(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	user.Groups = groups
	return user, nil
}

func (s *Store) userGroups(ctx context.Context, userID int64) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT g.name FROM user_groups ug
		JOIN groups g ON g.id = ug.group_id
		WHERE ug.user_id = $1 AND ug.revoked_at IS NULL AND g.revoked_at IS NULL ORDER BY g.name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	groups := make([]string, 0, 3)
	for rows.Next() {
		var group string
		if err := rows.Scan(&group); err != nil {
			return nil, err
		}
		groups = append(groups, group)
	}
	return groups, rows.Err()
}

func (s *Store) userRights(ctx context.Context, userID int64) (map[string]bool, error) {
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT gr.right_name
		FROM user_groups ug
		JOIN group_rights gr ON gr.group_id = ug.group_id
		JOIN groups g ON g.id = ug.group_id
		WHERE ug.user_id = $1 AND ug.revoked_at IS NULL AND gr.revoked_at IS NULL AND g.revoked_at IS NULL`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	rights := make(map[string]bool)
	for rows.Next() {
		var right string
		if err := rows.Scan(&right); err != nil {
			return nil, err
		}
		rights[right] = true
	}
	return rights, rows.Err()
}

func (s *Store) GrantGroup(ctx context.Context, userName, groupName string) error {
	result, err := s.pool.Exec(ctx, `INSERT INTO user_groups (user_id, group_id)
		SELECT u.id, g.id FROM users u JOIN groups g ON g.name = $2 AND g.revoked_at IS NULL WHERE u.name = $1
		ON CONFLICT (user_id, group_id) DO UPDATE SET revoked_at=NULL, revoked_by=NULL, revoke_reason=''`, userName, groupName)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		var exists bool
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users u JOIN groups g ON g.name = $2 AND g.revoked_at IS NULL WHERE u.name = $1)`,
			userName, groupName).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
	}
	return nil
}

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE deleted_at IS NULL`).Scan(&count)
	return count, err
}

func (s *Store) ListUsers(ctx context.Context, limit, offset int) ([]*User, error) {
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.pool.Query(ctx, `SELECT u.id, u.name, u.email, u.email_verified, u.disabled, u.totp_enabled, u.created_at,
			COALESCE(array_agg(g.name ORDER BY g.name) FILTER (WHERE g.name IS NOT NULL), '{}')
		FROM users u
		LEFT JOIN user_groups ug ON ug.user_id = u.id AND ug.revoked_at IS NULL
		LEFT JOIN groups g ON g.id = ug.group_id AND g.revoked_at IS NULL
		WHERE u.deleted_at IS NULL
		GROUP BY u.id
		ORDER BY u.created_at DESC, u.id DESC
		LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	users := make([]*User, 0, limit)
	for rows.Next() {
		user := &User{}
		if err := rows.Scan(&user.ID, &user.Name, &user.Email, &user.EmailVerified, &user.Disabled, &user.TOTPEnabled, &user.CreatedAt, &user.Groups); err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

func (s *Store) SetUserGroups(ctx context.Context, userID int64, groups []string, actorID int64) error {
	unique := uniqueStrings(groups)
	if len(unique) == 0 {
		return ErrBadGroup
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('wiki_x_user_admin')::bigint)`); err != nil {
		return err
	}

	var name string
	var disabled bool
	err = tx.QueryRow(ctx, `SELECT name, disabled FROM users WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, userID).Scan(&name, &disabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}

	rows, err := tx.Query(ctx, `SELECT name FROM groups WHERE name = ANY($1) AND revoked_at IS NULL`, unique)
	if err != nil {
		return err
	}
	found := make(map[string]bool, len(unique))
	for rows.Next() {
		var group string
		if err := rows.Scan(&group); err != nil {
			rows.Close()
			return err
		}
		found[group] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(found) != len(unique) {
		return ErrBadGroup
	}

	hasManage, err := s.userHasRightTx(ctx, tx, userID, RightManageUsers)
	if err != nil {
		return err
	}
	newManage, err := s.groupsHaveRight(ctx, tx, unique, RightManageUsers)
	if err != nil {
		return err
	}
	if hasManage && !disabled && !newManage {
		managers, err := countManagersExceptUser(ctx, tx, userID)
		if err != nil {
			return err
		}
		if managers == 0 {
			return ErrLastAdmin
		}
	}

	if _, err := tx.Exec(ctx, `UPDATE user_groups
		SET revoked_at=now(), revoked_by=NULLIF($2,0), revoke_reason='user groups update'
		WHERE user_id=$1 AND revoked_at IS NULL
		AND group_id NOT IN (SELECT id FROM groups WHERE name = ANY($3) AND revoked_at IS NULL)`, userID, actorID, unique); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO user_groups (user_id, group_id)
		SELECT $1, id FROM groups WHERE name = ANY($2) AND revoked_at IS NULL
		ON CONFLICT (user_id, group_id) DO UPDATE SET revoked_at=NULL, revoked_by=NULL, revoke_reason=''`, userID, unique); err != nil {
		return err
	}
	if err := logAudit(ctx, tx, actorID, "user.groups", name, strings.Join(unique, ",")); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) SetUserDisabled(ctx context.Context, userID int64, disabled bool, actorID int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('wiki_x_user_admin')::bigint)`); err != nil {
		return err
	}

	var name string
	var current bool
	err = tx.QueryRow(ctx, `SELECT name, disabled FROM users WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, userID).Scan(&name, &current)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if current == disabled {
		return nil
	}
	if disabled {
		hasManage, err := s.userHasRightTx(ctx, tx, userID, RightManageUsers)
		if err != nil {
			return err
		}
		if hasManage {
			managers, err := countManagersExceptUser(ctx, tx, userID)
			if err != nil {
				return err
			}
			if managers == 0 {
				return ErrLastAdmin
			}
		}
	}

	if _, err := tx.Exec(ctx, `UPDATE users SET disabled = $2 WHERE id = $1`, userID, disabled); err != nil {
		return err
	}
	if disabled {
		if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1`, userID); err != nil {
			return err
		}
	}
	action := "user.enable"
	if disabled {
		action = "user.disable"
	}
	if err := logAudit(ctx, tx, actorID, action, name, ""); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) UpdatePassword(ctx context.Context, userID int64, passwordHash, action string, actorID int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var name string
	err = tx.QueryRow(ctx, `UPDATE users SET password_hash = $2 WHERE id = $1 RETURNING name`, userID, passwordHash).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1`, userID); err != nil {
		return err
	}
	if err := logAudit(ctx, tx, actorID, action, name, ""); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) SetTOTPSecret(ctx context.Context, userID int64, secret string) error {
	result, err := s.pool.Exec(ctx, `UPDATE users SET totp_secret = $2, totp_enabled = false, totp_secret_set_at = now() WHERE id = $1`, userID, secret)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) EnableTOTP(ctx context.Context, userID int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	result, err := tx.Exec(ctx, `UPDATE users SET totp_enabled = true WHERE id = $1 AND totp_secret <> '' AND totp_enabled = false`, userID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1`, userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) DisableTOTP(ctx context.Context, userID int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	result, err := tx.Exec(ctx, `UPDATE users SET totp_secret = '', totp_enabled = false, totp_last_step = 0 WHERE id = $1`, userID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM recovery_codes WHERE user_id = $1`, userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) ConsumeTOTP(ctx context.Context, userID int64, step int64) (bool, error) {
	result, err := s.pool.Exec(ctx, `UPDATE users SET totp_last_step = $2 WHERE id = $1 AND totp_last_step < $2`, userID, step)
	if err != nil {
		return false, err
	}
	return result.RowsAffected() == 1, nil
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func (s *Store) LinkIdentity(ctx context.Context, userID int64, provider, subject, username, email string) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO identities (user_id, provider, subject, username, email)
		VALUES ($1, $2, $3, $4, $5)`, userID, provider, subject, username, email)
	if err == nil {
		return nil
	}
	if !isUniqueViolation(err) {
		return err
	}
	var existingUserID int64
	if lookupErr := s.pool.QueryRow(ctx, `SELECT user_id FROM identities WHERE provider = $1 AND subject = $2`, provider, subject).Scan(&existingUserID); lookupErr != nil {
		return err
	}
	if existingUserID == userID {
		return nil
	}
	return ErrIdentityTaken
}

func (s *Store) ListIdentities(ctx context.Context, userID int64) ([]*Identity, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, user_id, provider, subject, username, email, created_at
		FROM identities WHERE user_id = $1 ORDER BY provider`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var identities []*Identity
	for rows.Next() {
		identity := &Identity{}
		if err := rows.Scan(&identity.ID, &identity.UserID, &identity.Provider, &identity.Subject, &identity.Username, &identity.Email, &identity.CreatedAt); err != nil {
			return nil, err
		}
		identities = append(identities, identity)
	}
	return identities, rows.Err()
}

func (s *Store) UserIDByIdentity(ctx context.Context, provider, subject string) (int64, bool, error) {
	var userID int64
	err := s.pool.QueryRow(ctx, `SELECT user_id FROM identities WHERE provider = $1 AND subject = $2`, provider, subject).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return userID, true, nil
}

func (s *Store) HasAudit(ctx context.Context, action, target string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM audit_logs WHERE action = $1 AND target = $2)`, action, target).Scan(&exists)
	return exists, err
}

func (s *Store) FindOrCreateGitHubUser(ctx context.Context, subject, username, email string) (*User, error) {
	var userID int64
	err := s.pool.QueryRow(ctx, `SELECT user_id FROM identities WHERE provider = 'github' AND subject = $1`, subject).Scan(&userID)
	if err == nil {
		return s.UserByID(ctx, userID)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	base := githubUserName(username)
	for attempt := 0; attempt < 5; attempt++ {
		name := base
		if attempt > 0 {
			suffix, err := randomSuffix()
			if err != nil {
				return nil, err
			}
			name = base + "_" + suffix
		}

		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return nil, err
		}
		var createdID int64
		err = tx.QueryRow(ctx, `INSERT INTO users (name, email, password_hash) VALUES ($1, $2, '') RETURNING id`,
			name, strings.TrimSpace(email)).Scan(&createdID)
		if err != nil {
			_ = tx.Rollback(ctx)
			if isUniqueViolation(err) {
				continue
			}
			return nil, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO identities (user_id, provider, subject, username, email)
			VALUES ($1, 'github', $2, $3, $4)`, createdID, subject, username, email); err != nil {
			_ = tx.Rollback(ctx)
			if isUniqueViolation(err) {
				var existingID int64
				if lookupErr := s.pool.QueryRow(ctx, `SELECT user_id FROM identities WHERE provider = 'github' AND subject = $1`, subject).Scan(&existingID); lookupErr == nil {
					return s.UserByID(ctx, existingID)
				}
			}
			return nil, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO user_groups (user_id, group_id)
			SELECT $1, id FROM groups WHERE name = 'user' AND revoked_at IS NULL`, createdID); err != nil {
			_ = tx.Rollback(ctx)
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return s.UserByID(ctx, createdID)
	}
	return nil, errors.New("could not create unique github user name")
}

func githubUserName(username string) string {
	base := strings.ToLower(strings.TrimSpace(username))
	var builder strings.Builder
	builder.WriteString("gh_")
	for _, r := range base {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' {
			builder.WriteRune(r)
		} else {
			builder.WriteRune('-')
		}
	}
	name := strings.Trim(builder.String(), "-_")
	if name == "gh" || name == "gh_" || name == "" {
		return "gh_user"
	}
	return name
}

func randomSuffix() (string, error) {
	buf := make([]byte, 2)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func (s *Store) LogAudit(ctx context.Context, actorID int64, action, target, detail string) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO audit_logs (actor_id, action, target, detail)
		VALUES (NULLIF($1, 0), $2, $3, $4)`, actorID, action, target, detail)
	return err
}
