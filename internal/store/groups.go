package store

import (
	"context"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

var builtinGroups = map[string]bool{"admin": true, "editor": true, "user": true}

func (s *Store) ListGroups(ctx context.Context) ([]*Group, error) {
	rows, err := s.read().Query(ctx, `SELECT g.id, g.name,
			COALESCE(array_agg(DISTINCT gr.right_name ORDER BY gr.right_name) FILTER (WHERE gr.right_name IS NOT NULL), '{}'),
			count(DISTINCT ug.user_id)
		FROM groups g
		LEFT JOIN group_rights gr ON gr.group_id = g.id AND gr.revoked_at IS NULL
		LEFT JOIN user_groups ug ON ug.group_id = g.id AND ug.revoked_at IS NULL
		WHERE g.revoked_at IS NULL
		GROUP BY g.id
		ORDER BY g.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []*Group
	for rows.Next() {
		var group Group
		if err := rows.Scan(&group.ID, &group.Name, &group.Rights, &group.UserCount); err != nil {
			return nil, err
		}
		group.Builtin = builtinGroups[group.Name]
		groups = append(groups, &group)
	}
	return groups, rows.Err()
}

func (s *Store) CreateGroup(ctx context.Context, name string, rights []string, actorID int64) error {
	name, err := validateGroupName(name)
	if err != nil {
		return err
	}
	rights, err = normalizeRights(rights)
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var groupID int64
	if err := tx.QueryRow(ctx, `INSERT INTO groups (name) VALUES ($1) RETURNING id`, name).Scan(&groupID); err != nil {
		if isUniqueViolation(err) {
			return ErrGroupExists
		}
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO group_rights (group_id, right_name)
		SELECT $1, unnest($2::text[])`, groupID, rights); err != nil {
		return err
	}
	if err := logAudit(ctx, tx, actorID, "group.create", name, strings.Join(rights, ",")); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) SetGroupRights(ctx context.Context, groupID int64, rights []string, actorID int64) error {
	rights, err := normalizeRights(rights)
	if err != nil {
		return err
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
	err = tx.QueryRow(ctx, `SELECT name FROM groups WHERE id = $1 AND revoked_at IS NULL FOR UPDATE`, groupID).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	var hadManage bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM group_rights
		WHERE group_id = $1 AND right_name = $2 AND revoked_at IS NULL)`, groupID, RightManageUsers).Scan(&hadManage); err != nil {
		return err
	}
	if hadManage && !containsString(rights, RightManageUsers) {
		managers, err := countManagersExceptGroup(ctx, tx, groupID)
		if err != nil {
			return err
		}
		if managers == 0 {
			return ErrLastAdmin
		}
	}

	if _, err := tx.Exec(ctx, `UPDATE group_rights
		SET revoked_at=now(), revoked_by=NULLIF($2,0), revoke_reason='group rights update'
		WHERE group_id=$1 AND revoked_at IS NULL AND NOT (right_name = ANY($3))`, groupID, actorID, rights); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO group_rights (group_id, right_name)
		SELECT $1, unnest($2::text[])
		ON CONFLICT (group_id, right_name) DO UPDATE
		SET revoked_at=NULL, revoked_by=NULL, revoke_reason=''`, groupID, rights); err != nil {
		return err
	}
	if err := logAudit(ctx, tx, actorID, "group.rights", name, strings.Join(rights, ",")); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) DeleteGroup(ctx context.Context, groupID, actorID int64) error {
	if !s.revokeEnabled(SectionGroup) {
		return ErrRevokeDisabled
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
	err = tx.QueryRow(ctx, `SELECT name FROM groups WHERE id = $1 AND revoked_at IS NULL FOR UPDATE`, groupID).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if builtinGroups[name] {
		return ErrBuiltinGroup
	}
	var hasManage bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM group_rights
		WHERE group_id = $1 AND right_name = $2 AND revoked_at IS NULL)`, groupID, RightManageUsers).Scan(&hasManage); err != nil {
		return err
	}
	if hasManage {
		managers, err := countManagersExceptGroup(ctx, tx, groupID)
		if err != nil {
			return err
		}
		if managers == 0 {
			return ErrLastAdmin
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE groups
		SET revoked_at=now(), revoked_by=NULLIF($2,0), revoke_reason='deleted'
		WHERE id=$1 AND revoked_at IS NULL`, groupID, actorID); err != nil {
		return err
	}
	for _, table := range []string{"group_rights", "user_groups"} {
		if _, err := tx.Exec(ctx, `UPDATE `+table+`
			SET revoked_at=now(), revoked_by=NULLIF($2,0), revoke_reason='group deleted'
			WHERE group_id=$1 AND revoked_at IS NULL`, groupID, actorID); err != nil {
			return err
		}
	}
	if err := logAudit(ctx, tx, actorID, "group.delete", name, "revoked"); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) userHasRightTx(ctx context.Context, tx pgx.Tx, userID int64, right string) (bool, error) {
	var exists bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM user_groups ug
		JOIN group_rights gr ON gr.group_id = ug.group_id
		JOIN groups g ON g.id = ug.group_id
		WHERE ug.user_id = $1 AND gr.right_name = $2
		AND ug.revoked_at IS NULL AND gr.revoked_at IS NULL AND g.revoked_at IS NULL)`, userID, right).Scan(&exists)
	return exists, err
}

func (s *Store) groupsHaveRight(ctx context.Context, tx pgx.Tx, groups []string, right string) (bool, error) {
	var exists bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM groups g
		JOIN group_rights gr ON gr.group_id = g.id
		WHERE g.name = ANY($1) AND gr.right_name = $2
		AND g.revoked_at IS NULL AND gr.revoked_at IS NULL)`, groups, right).Scan(&exists)
	return exists, err
}

func countManagersExceptUser(ctx context.Context, tx pgx.Tx, userID int64) (int, error) {
	var count int
	err := tx.QueryRow(ctx, `SELECT count(DISTINCT u.id) FROM users u
		JOIN user_groups ug ON ug.user_id = u.id
		JOIN group_rights gr ON gr.group_id = ug.group_id
		JOIN groups g ON g.id = ug.group_id
		WHERE gr.right_name = $2 AND u.disabled = false AND u.deleted_at IS NULL AND u.id <> $1
		AND ug.revoked_at IS NULL AND gr.revoked_at IS NULL AND g.revoked_at IS NULL`, userID, RightManageUsers).Scan(&count)
	return count, err
}

func countManagersExceptGroup(ctx context.Context, tx pgx.Tx, groupID int64) (int, error) {
	var count int
	err := tx.QueryRow(ctx, `SELECT count(DISTINCT u.id) FROM users u
		JOIN user_groups ug ON ug.user_id = u.id
		JOIN group_rights gr ON gr.group_id = ug.group_id
		JOIN groups g ON g.id = ug.group_id
		WHERE gr.right_name = $2 AND u.disabled = false AND u.deleted_at IS NULL AND ug.group_id <> $1
		AND ug.revoked_at IS NULL AND gr.revoked_at IS NULL AND g.revoked_at IS NULL`, groupID, RightManageUsers).Scan(&count)
	return count, err
}

func validateGroupName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 64 {
		return "", ErrBadGroup
	}
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r) {
			continue
		}
		switch r {
		case '-', '_', '.', ' ':
			continue
		default:
			return "", ErrBadGroup
		}
	}
	return name, nil
}

func normalizeRights(rights []string) ([]string, error) {
	seen := make(map[string]bool, len(rights))
	for _, right := range rights {
		right = strings.TrimSpace(right)
		if !validRight(right) {
			return nil, ErrBadRight
		}
		seen[right] = true
	}
	if len(seen) == 0 {
		return nil, ErrBadRight
	}
	result := make([]string, 0, len(seen))
	for _, right := range allRights {
		if seen[right] {
			result = append(result, right)
		}
	}
	return result, nil
}

func validRight(right string) bool {
	for _, known := range allRights {
		if right == known {
			return true
		}
	}
	return false
}
