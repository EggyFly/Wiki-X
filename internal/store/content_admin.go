package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type AdminPageRow struct {
	ID            int64
	Route         string
	Title         string
	Protected     bool
	UpdatedAt     time.Time
	DeletedAt     *time.Time
	DeletedByName string
	DeleteReason  string
	Revisions     int
}

func (s *Store) ListAdminPages(ctx context.Context, status, query string, limit, offset int) ([]*AdminPageRow, int, error) {
	switch status {
	case "all", "active", "deleted":
	default:
		status = "all"
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	filter := `WHERE ($1 = 'all' OR ($1 = 'deleted' AND p.deleted_at IS NOT NULL) OR ($1 = 'active' AND p.deleted_at IS NULL))
		AND ($2 = '' OR p.route ILIKE '%' || $2 || '%' OR p.title ILIKE '%' || $2 || '%')`
	var total int
	if err := s.read().QueryRow(ctx, `SELECT count(*) FROM pages p `+filter, status, query).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.read().Query(ctx, `SELECT p.id, p.route, p.title, p.protected, p.updated_at, p.deleted_at,
			COALESCE(u.name, ''), p.delete_reason,
			(SELECT count(*) FROM page_revisions r WHERE r.page_id = p.id)
		FROM pages p LEFT JOIN users u ON u.id = p.deleted_by `+filter+`
		ORDER BY p.updated_at DESC, p.id DESC LIMIT $3 OFFSET $4`, status, query, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]*AdminPageRow, 0, limit)
	for rows.Next() {
		item := &AdminPageRow{}
		if err := rows.Scan(&item.ID, &item.Route, &item.Title, &item.Protected, &item.UpdatedAt,
			&item.DeletedAt, &item.DeletedByName, &item.DeleteReason, &item.Revisions); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (s *Store) DeletePage(ctx context.Context, route, reason string, actorID int64) error {
	if !s.revokeEnabled(SectionPage) {
		return ErrRevokeDisabled
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var title string
	err = tx.QueryRow(ctx, `UPDATE pages SET deleted_at = now(), deleted_by = NULLIF($2, 0), delete_reason = $3
		WHERE route = $1 AND deleted_at IS NULL RETURNING title`, route, actorID, reason).Scan(&title)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if err := logAudit(ctx, tx, actorID, "page.delete", route, reason); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) RenamePage(ctx context.Context, oldRoute, newRoute string, actorID int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var pageID int64
	err = tx.QueryRow(ctx, `SELECT id FROM pages WHERE route = $1 AND deleted_at IS NULL FOR UPDATE`, oldRoute).Scan(&pageID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	var occupied bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pages
		WHERE lower(route) = lower($1) AND deleted_at IS NULL AND id <> $2)`, newRoute, pageID).Scan(&occupied); err != nil {
		return err
	}
	if occupied {
		return ErrRouteExists
	}
	if _, err := tx.Exec(ctx, `UPDATE pages SET route = $2 WHERE id = $1`, pageID, newRoute); err != nil {
		if isRouteUniqueViolation(err) {
			return ErrRouteExists
		}
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM watchlist w USING watchlist w2
		WHERE w.route = $1 AND w2.route = $2 AND w.user_id = w2.user_id AND w2.revoked_at IS NULL`, oldRoute, newRoute); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM watchlist w USING watchlist old
		WHERE w.route = $2 AND w.revoked_at IS NOT NULL AND old.route = $1
			AND old.user_id = w.user_id AND old.revoked_at IS NULL`, oldRoute, newRoute); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM watchlist w USING watchlist old
		WHERE w.route = $1 AND w.revoked_at IS NOT NULL AND old.route = $2
			AND old.user_id = w.user_id AND old.revoked_at IS NOT NULL`, oldRoute, newRoute); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE watchlist SET route = $2 WHERE route = $1`, oldRoute, newRoute); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE comments SET route = $2 WHERE route = $1`, oldRoute, newRoute); err != nil {
		return err
	}
	if err := logAudit(ctx, tx, actorID, "page.rename", oldRoute, newRoute); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) RestorePage(ctx context.Context, route string, actorID int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var pageID int64
	err = tx.QueryRow(ctx, `SELECT id FROM pages
		WHERE route = $1 AND deleted_at IS NOT NULL ORDER BY id DESC LIMIT 1 FOR UPDATE`, route).Scan(&pageID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	var occupied bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pages
		WHERE lower(route) = lower($1) AND deleted_at IS NULL)`, route).Scan(&occupied); err != nil {
		return err
	}
	if occupied {
		return ErrEditConflict
	}
	var title string
	if err := tx.QueryRow(ctx, `UPDATE pages SET deleted_at = NULL, deleted_by = NULL, delete_reason = ''
		WHERE id = $1 RETURNING title`, pageID).Scan(&title); err != nil {
		if isUniqueViolation(err) {
			return ErrEditConflict
		}
		return err
	}
	if err := logAudit(ctx, tx, actorID, "page.restore", route, ""); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
