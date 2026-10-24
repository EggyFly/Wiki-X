package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type WatchedPage struct {
	Route      string
	Title      string
	UpdatedAt  *time.Time
	RevisionID *int64
	AuthorName string
	Comment    string
	ChangedAt  *time.Time
	Source     string
}

func (s *Store) Watch(ctx context.Context, userID int64, route string) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO watchlist (user_id, route) VALUES ($1, $2)
		ON CONFLICT (user_id, route) DO NOTHING`, userID, route)
	return err
}

func (s *Store) Unwatch(ctx context.Context, userID int64, route string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM watchlist WHERE user_id = $1 AND route = $2`, userID, route)
	return err
}

func (s *Store) IsWatched(ctx context.Context, userID int64, route string) (bool, error) {
	var watched bool
	err := s.read().QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM watchlist WHERE user_id = $1 AND route = $2)`, userID, route).Scan(&watched)
	return watched, err
}

func (s *Store) ListWatchedRevisions(ctx context.Context, userID int64, limit int) ([]*WatchedPage, error) {
	rows, err := s.read().Query(ctx, `SELECT w.route, COALESCE(p.title, w.route), p.updated_at,
			r.id, COALESCE(u.name, ''), COALESCE(r.comment, ''), r.created_at
		FROM watchlist w
		LEFT JOIN pages p ON p.route = w.route
		LEFT JOIN LATERAL (
			SELECT rv.id, rv.author_id, rv.comment, rv.created_at
			FROM page_revisions rv
			WHERE rv.page_id = p.id AND rv.status = 'published'
			ORDER BY rv.created_at DESC LIMIT 1
		) r ON true
		LEFT JOIN users u ON u.id = r.author_id
		WHERE w.user_id = $1
		ORDER BY w.created_at DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pages []*WatchedPage
	for rows.Next() {
		var page WatchedPage
		if err := rows.Scan(&page.Route, &page.Title, &page.UpdatedAt, &page.RevisionID,
			&page.AuthorName, &page.Comment, &page.ChangedAt); err != nil {
			return nil, err
		}
		pages = append(pages, &page)
	}
	return pages, rows.Err()
}

func (s *Store) AddComment(ctx context.Context, route string, userID int64, body string) (*Comment, error) {
	var comment Comment
	err := s.pool.QueryRow(ctx, `WITH inserted AS (
			INSERT INTO comments (route, user_id, body) VALUES ($1, $2, $3)
			RETURNING id, route, user_id, body, status, created_at
		)
		SELECT i.id, i.route, i.user_id, COALESCE(u.name, ''), i.body, i.status, i.created_at
		FROM inserted i LEFT JOIN users u ON u.id = i.user_id`, route, userID, body).
		Scan(&comment.ID, &comment.Route, &comment.UserID, &comment.UserName, &comment.Body, &comment.Status, &comment.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &comment, nil
}

func (s *Store) ListComments(ctx context.Context, route string, includeHidden bool) ([]*Comment, error) {
	rows, err := s.read().Query(ctx, `SELECT c.id, c.route, c.user_id, COALESCE(u.name, ''), c.body, c.status,
			COALESCE(rep.total, 0), COALESCE(rep.reasons, '{}'), c.created_at
		FROM comments c
		LEFT JOIN users u ON u.id = c.user_id
		LEFT JOIN (
			SELECT comment_id, count(*) AS total,
				COALESCE((array_agg(reason ORDER BY created_at DESC))[1:10], '{}') AS reasons
			FROM comment_reports WHERE status = 'open' GROUP BY comment_id
		) rep ON rep.comment_id = c.id
		WHERE c.route = $1 AND ($2 OR c.status = 'visible')
		ORDER BY c.created_at ASC LIMIT 500`, route, includeHidden)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var comments []*Comment
	for rows.Next() {
		var comment Comment
		if err := rows.Scan(&comment.ID, &comment.Route, &comment.UserID, &comment.UserName,
			&comment.Body, &comment.Status, &comment.Reports, &comment.ReportReasons, &comment.CreatedAt); err != nil {
			return nil, err
		}
		comments = append(comments, &comment)
	}
	return comments, rows.Err()
}

func (s *Store) HideComment(ctx context.Context, commentID, actorID int64, hidden bool) (string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	status := "visible"
	action := "comment.show"
	if hidden {
		status = "hidden"
		action = "comment.hide"
	}
	var route string
	err = tx.QueryRow(ctx, `UPDATE comments SET status = $2 WHERE id = $1 RETURNING route`, commentID, status).Scan(&route)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if hidden {
		if _, err := tx.Exec(ctx, `UPDATE comment_reports SET status = 'resolved', resolved_at = now(), resolved_by = $2
			WHERE comment_id = $1 AND status = 'open'`, commentID, actorID); err != nil {
			return "", err
		}
	}
	if err := logAudit(ctx, tx, actorID, action, route, fmt.Sprintf("comment:%d", commentID)); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return route, nil
}

func (s *Store) ReportComment(ctx context.Context, commentID, reporterID int64, reason string) (string, error) {
	var route string
	err := s.pool.QueryRow(ctx, `WITH target AS (
			SELECT c.id, c.route FROM comments c
			WHERE c.id = $1 AND c.status = 'visible' AND (c.user_id IS NULL OR c.user_id <> $2)
		), upsert AS (
			INSERT INTO comment_reports (comment_id, reporter_id, reason)
			SELECT id, $2, $3 FROM target
			ON CONFLICT (comment_id, reporter_id) DO UPDATE
			SET reason = EXCLUDED.reason, status = 'open', created_at = now(), resolved_at = NULL, resolved_by = NULL
			RETURNING comment_id
		)
		SELECT t.route FROM target t JOIN upsert u ON u.comment_id = t.id`, commentID, reporterID, reason).Scan(&route)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return route, nil
}

func (s *Store) ResolveCommentReports(ctx context.Context, commentID, actorID int64) (string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	var route string
	err = tx.QueryRow(ctx, `SELECT route FROM comments WHERE id = $1`, commentID).Scan(&route)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `UPDATE comment_reports SET status = 'resolved', resolved_at = now(), resolved_by = $2
		WHERE comment_id = $1 AND status = 'open'`, commentID, actorID); err != nil {
		return "", err
	}
	if err := logAudit(ctx, tx, actorID, "comment.reports.resolve", route, fmt.Sprintf("comment:%d", commentID)); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return route, nil
}

func (s *Store) ListOpenReports(ctx context.Context, limit int) ([]*CommentReport, error) {
	return s.ListReports(ctx, "open", limit, 0)
}

func (s *Store) ListReports(ctx context.Context, status string, limit, offset int) ([]*CommentReport, error) {
	switch status {
	case "open", "resolved", "all":
	default:
		status = "open"
	}
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.read().Query(ctx, `SELECT r.id, r.comment_id, c.route,
			CASE WHEN char_length(c.body) > 300 THEN left(c.body, 300) || '…' ELSE c.body END,
			c.status, COALESCE(u.name, ''), r.reason, r.status, r.created_at,
			r.resolved_at, COALESCE(ru.name, '')
		FROM comment_reports r
		JOIN comments c ON c.id = r.comment_id
		LEFT JOIN users u ON u.id = r.reporter_id
		LEFT JOIN users ru ON ru.id = r.resolved_by
		WHERE ($1 = 'all' OR r.status = $1)
		ORDER BY r.created_at DESC LIMIT $2 OFFSET $3`, status, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reports []*CommentReport
	for rows.Next() {
		var report CommentReport
		if err := rows.Scan(&report.ID, &report.CommentID, &report.Route, &report.Body,
			&report.CommentStatus, &report.ReporterName, &report.Reason, &report.Status,
			&report.CreatedAt, &report.ResolvedAt, &report.ResolvedByName); err != nil {
			return nil, err
		}
		reports = append(reports, &report)
	}
	return reports, rows.Err()
}

func (s *Store) CountReports(ctx context.Context, status string) (int, error) {
	switch status {
	case "open", "resolved", "all":
	default:
		status = "open"
	}
	var count int
	err := s.read().QueryRow(ctx, `SELECT count(*) FROM comment_reports WHERE ($1 = 'all' OR status = $1)`, status).Scan(&count)
	return count, err
}
