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

type AuditCursor struct {
	CreatedAt time.Time
	ID        int64
}

func (s *Store) ListAuditLogsPage(ctx context.Context, filter AuditFilter, cursor *AuditCursor, limit int) ([]*AuditLog, *AuditCursor, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var cursorTime any
	var cursorID int64
	if cursor != nil {
		cursorTime = cursor.CreatedAt
		cursorID = cursor.ID
	}
	rows, err := s.pool.Query(ctx, `SELECT a.id, a.actor_id, COALESCE(u.name, ''), a.action, a.target, a.detail, a.created_at
		FROM audit_logs a
		LEFT JOIN users u ON u.id = a.actor_id
		WHERE ($1 = '' OR a.action ILIKE '%' || $1 || '%' OR a.target ILIKE '%' || $1 || '%'
			OR a.detail ILIKE '%' || $1 || '%' OR COALESCE(u.name, '') ILIKE '%' || $1 || '%')
			AND ($2::timestamptz IS NULL OR a.created_at >= $2)
			AND ($3::timestamptz IS NULL OR a.created_at < $3)
			AND ($4::timestamptz IS NULL OR (a.created_at, a.id) < ($4::timestamptz, $5::bigint))
		ORDER BY a.created_at DESC, a.id DESC
		LIMIT $6`, filter.Query, filter.From, filter.To, cursorTime, cursorID, limit+1)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	logs := make([]*AuditLog, 0, limit)
	for rows.Next() {
		var entry AuditLog
		if err := rows.Scan(&entry.ID, &entry.ActorID, &entry.ActorName, &entry.Action,
			&entry.Target, &entry.Detail, &entry.CreatedAt); err != nil {
			return nil, nil, err
		}
		logs = append(logs, &entry)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	var next *AuditCursor
	if len(logs) > limit {
		last := logs[limit-1]
		next = &AuditCursor{CreatedAt: last.CreatedAt, ID: last.ID}
		logs = logs[:limit]
	}
	return logs, next, nil
}

func (s *Store) StreamAuditLogsBefore(ctx context.Context, before time.Time, fn func(*AuditLog) error) (int64, int, error) {
	lastID := int64(0)
	count := 0
	for {
		rows, err := s.pool.Query(ctx, `SELECT a.id, a.actor_id, COALESCE(u.name, ''), a.action, a.target, a.detail, a.created_at
			FROM audit_logs a LEFT JOIN users u ON u.id = a.actor_id
			WHERE a.created_at < $1 AND a.id > $2
			ORDER BY a.id LIMIT 500`, before, lastID)
		if err != nil {
			return lastID, count, err
		}
		batch := 0
		for rows.Next() {
			var entry AuditLog
			if err := rows.Scan(&entry.ID, &entry.ActorID, &entry.ActorName, &entry.Action,
				&entry.Target, &entry.Detail, &entry.CreatedAt); err != nil {
				rows.Close()
				return lastID, count, err
			}
			if err := fn(&entry); err != nil {
				rows.Close()
				return lastID, count, err
			}
			lastID = entry.ID
			count++
			batch++
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return lastID, count, err
		}
		rows.Close()
		if batch == 0 {
			return lastID, count, nil
		}
	}
}

func (s *Store) DeleteAuditLogsThrough(ctx context.Context, maxID int64, before time.Time) (int64, error) {
	if maxID <= 0 {
		return 0, nil
	}
	result, err := s.pool.Exec(ctx, `DELETE FROM audit_logs WHERE id <= $1 AND created_at < $2`, maxID, before)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
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
