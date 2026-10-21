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
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM pages p `+filter, status, query).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `SELECT p.id, p.route, p.title, p.protected, p.updated_at, p.deleted_at,
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

func (s *Store) RestorePage(ctx context.Context, route string, actorID int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var title string
	err = tx.QueryRow(ctx, `UPDATE pages SET deleted_at = NULL, deleted_by = NULL, delete_reason = ''
		WHERE route = $1 AND deleted_at IS NOT NULL RETURNING title`, route).Scan(&title)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if err := logAudit(ctx, tx, actorID, "page.restore", route, ""); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
