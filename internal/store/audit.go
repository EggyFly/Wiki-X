package store

import (
	"context"
	"time"
)

type AuditFilter struct {
	Query string
	From  *time.Time
	To    *time.Time
}

func (s *Store) ListAuditLogs(ctx context.Context, filter AuditFilter, limit, offset int) ([]*AuditLog, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.pool.Query(ctx, `SELECT a.id, a.actor_id, COALESCE(u.name, ''), a.action, a.target, a.detail, a.created_at
		FROM audit_logs a
		LEFT JOIN users u ON u.id = a.actor_id
		WHERE ($1 = '' OR a.action ILIKE '%' || $1 || '%' OR a.target ILIKE '%' || $1 || '%'
			OR a.detail ILIKE '%' || $1 || '%' OR COALESCE(u.name, '') ILIKE '%' || $1 || '%')
			AND ($2::timestamptz IS NULL OR a.created_at >= $2)
			AND ($3::timestamptz IS NULL OR a.created_at < $3)
		ORDER BY a.created_at DESC, a.id DESC
		LIMIT $4 OFFSET $5`, filter.Query, filter.From, filter.To, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	logs := make([]*AuditLog, 0, limit)
	for rows.Next() {
		var entry AuditLog
		if err := rows.Scan(&entry.ID, &entry.ActorID, &entry.ActorName, &entry.Action,
			&entry.Target, &entry.Detail, &entry.CreatedAt); err != nil {
			return nil, err
		}
		logs = append(logs, &entry)
	}
	return logs, rows.Err()
}

func (s *Store) CountAuditLogs(ctx context.Context, filter AuditFilter) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM audit_logs a
		LEFT JOIN users u ON u.id = a.actor_id
		WHERE ($1 = '' OR a.action ILIKE '%' || $1 || '%' OR a.target ILIKE '%' || $1 || '%'
			OR a.detail ILIKE '%' || $1 || '%' OR COALESCE(u.name, '') ILIKE '%' || $1 || '%')
			AND ($2::timestamptz IS NULL OR a.created_at >= $2)
			AND ($3::timestamptz IS NULL OR a.created_at < $3)`, filter.Query, filter.From, filter.To).Scan(&count)
	return count, err
}
