package store

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

type RevokedRecord struct {
	Kind      string
	ID        string
	Label     string
	Reason    string
	ActorName string
	RevokedAt time.Time
}

func (s *Store) ListRevokedRecords(ctx context.Context, limit int) ([]*RevokedRecord, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.pool.Query(ctx, `SELECT kind, id, label, reason, actor_name, revoked_at FROM (
		SELECT 'group' AS kind, g.id::text AS id, g.name AS label, g.revoke_reason AS reason,
			COALESCE(u.name,'') AS actor_name, g.revoked_at
		FROM groups g LEFT JOIN users u ON u.id=g.revoked_by WHERE g.revoked_at IS NOT NULL
		UNION ALL
		SELECT 'entity_type', t.id::text, t.name, t.revoke_reason, COALESCE(u.name,''), t.revoked_at
		FROM entity_types t LEFT JOIN users u ON u.id=t.revoked_by WHERE t.revoked_at IS NOT NULL
		UNION ALL
		SELECT 'entity_field', f.id::text, t.name || '.' || f.name, f.revoke_reason, COALESCE(u.name,''), f.revoked_at
		FROM entity_fields f JOIN entity_types t ON t.id=f.type_id
		LEFT JOIN users u ON u.id=f.revoked_by WHERE f.revoked_at IS NOT NULL
		UNION ALL
		SELECT 'entity', e.id::text, e.name, e.revoke_reason, COALESCE(u.name,''), e.revoked_at
		FROM entities e LEFT JOIN users u ON u.id=e.revoked_by WHERE e.revoked_at IS NOT NULL
		UNION ALL
		SELECT 'invite', i.id::text, i.label, i.revoke_reason, COALESCE(u.name,''), i.revoked_at
		FROM invite_codes i LEFT JOIN users u ON u.id=i.revoked_by WHERE i.revoked_at IS NOT NULL
		UNION ALL
		SELECT 'oidc_client', c.client_id, c.name, c.revoke_reason, COALESCE(u.name,''), c.revoked_at
		FROM oidc_clients c LEFT JOIN users u ON u.id=c.revoked_by WHERE c.revoked_at IS NOT NULL
		UNION ALL
		SELECT 'interactive', n.id::text, n.node_type || '.' || n.node_key, n.revoke_reason,
			COALESCE(u.name,''), n.revoked_at
		FROM interactive_nodes n LEFT JOIN users u ON u.id=n.revoked_by WHERE n.revoked_at IS NOT NULL
	) x ORDER BY revoked_at DESC, kind, id LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []*RevokedRecord
	for rows.Next() {
		item := &RevokedRecord{}
		if err := rows.Scan(&item.Kind, &item.ID, &item.Label, &item.Reason, &item.ActorName, &item.RevokedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) RestoreRevokedRecord(ctx context.Context, kind, id string, actorID int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	switch kind {
	case "group":
		groupID, err := parseRevokedID(id)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('wiki_x_user_admin')::bigint)`); err != nil {
			return err
		}
		var name string
		err = tx.QueryRow(ctx, `UPDATE groups SET revoked_at=NULL, revoked_by=NULL, revoke_reason=''
			WHERE id=$1 AND revoked_at IS NOT NULL RETURNING name`, groupID).Scan(&name)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			if isUniqueViolation(err) {
				return ErrEditConflict
			}
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE group_rights SET revoked_at=NULL, revoked_by=NULL, revoke_reason=''
			WHERE group_id=$1 AND revoked_at IS NOT NULL AND revoke_reason='group deleted'`, groupID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE user_groups SET revoked_at=NULL, revoked_by=NULL, revoke_reason=''
			WHERE group_id=$1 AND revoked_at IS NOT NULL AND revoke_reason='group deleted'`, groupID); err != nil {
			return err
		}
		return finishRestore(ctx, tx, actorID, "group.restore", name, "")
	case "entity_type":
		entityTypeID, err := parseRevokedID(id)
		if err != nil {
			return err
		}
		var name string
		err = tx.QueryRow(ctx, `UPDATE entity_types SET revoked_at=NULL, revoked_by=NULL, revoke_reason=''
			WHERE id=$1 AND revoked_at IS NOT NULL RETURNING name`, entityTypeID).Scan(&name)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			if isUniqueViolation(err) {
				return ErrEditConflict
			}
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE entity_fields SET revoked_at=NULL, revoked_by=NULL, revoke_reason=''
			WHERE type_id=$1 AND revoked_at IS NOT NULL AND revoke_reason='entity type deleted'`, entityTypeID); err != nil {
			return err
		}
		return finishRestore(ctx, tx, actorID, "entity_type.restore", name, "")
	case "entity_field":
		fieldID, err := parseRevokedID(id)
		if err != nil {
			return err
		}
		var label string
		err = tx.QueryRow(ctx, `WITH updated AS (
				UPDATE entity_fields SET revoked_at=NULL, revoked_by=NULL, revoke_reason=''
				WHERE id=$1 AND revoked_at IS NOT NULL RETURNING id, type_id, name
			)
			SELECT t.name || '.' || updated.name FROM updated
			JOIN entity_types t ON t.id=updated.type_id`, fieldID).Scan(&label)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			if isUniqueViolation(err) {
				return ErrEditConflict
			}
			return err
		}
		return finishRestore(ctx, tx, actorID, "entity_field.restore", label, "")
	case "entity":
		entityID, err := parseRevokedID(id)
		if err != nil {
			return err
		}
		var name string
		err = tx.QueryRow(ctx, `UPDATE entities SET revoked_at=NULL, revoked_by=NULL, revoke_reason=''
			WHERE id=$1 AND revoked_at IS NOT NULL RETURNING name`, entityID).Scan(&name)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			if isUniqueViolation(err) {
				return ErrEditConflict
			}
			return err
		}
		return finishRestore(ctx, tx, actorID, "entity.restore", name, "")
	case "invite":
		inviteID, err := parseRevokedID(id)
		if err != nil {
			return err
		}
		var label string
		err = tx.QueryRow(ctx, `UPDATE invite_codes SET revoked_at=NULL, revoked_by=NULL, revoke_reason=''
			WHERE id=$1 AND revoked_at IS NOT NULL RETURNING label`, inviteID).Scan(&label)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		return finishRestore(ctx, tx, actorID, "invite.restore", label, "")
	case "oidc_client":
		var name string
		err = tx.QueryRow(ctx, `UPDATE oidc_clients
			SET revoked_at=NULL, revoked_by=NULL, revoke_reason='', enabled=false, updated_at=now()
			WHERE client_id=$1 AND revoked_at IS NOT NULL RETURNING name`, id).Scan(&name)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		return finishRestore(ctx, tx, actorID, "oidc.client.restore", id, "enabled=false")
	case "interactive":
		nodeID, err := parseRevokedID(id)
		if err != nil {
			return err
		}
		var label string
		err = tx.QueryRow(ctx, `UPDATE interactive_nodes
			SET revoked_at=NULL, revoked_by=NULL, revoke_reason='', enabled=false, updated_at=now()
			WHERE id=$1 AND revoked_at IS NOT NULL RETURNING node_type || '.' || node_key`, nodeID).Scan(&label)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			if isUniqueViolation(err) {
				return ErrEditConflict
			}
			return err
		}
		return finishRestore(ctx, tx, actorID, "interactive.restore", label, "enabled=false")
	default:
		return ErrNotFound
	}
}

func finishRestore(ctx context.Context, tx pgx.Tx, actorID int64, action, target, detail string) error {
	if err := logAudit(ctx, tx, actorID, action, target, detail); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func parseRevokedID(value string) (int64, error) {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		return 0, ErrNotFound
	}
	return id, nil
}
