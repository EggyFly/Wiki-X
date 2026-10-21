package store

import (
	"context"
	"strings"
)

type SearchHit struct {
	Route   string
	Title   string
	Snippet string
}

func (s *Store) SearchPages(ctx context.Context, query string, limit int) ([]*SearchHit, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.pool.Query(ctx, `SELECT route, title,
			CASE WHEN strpos(lower(body), lower($1)) > 0
				THEN substring(body from greatest(strpos(lower(body), lower($1)) - 80, 1) for 240)
				ELSE left(body, 240) END AS snippet
		FROM pages
		WHERE draft = false AND deleted_at IS NULL
			AND (title ILIKE '%' || $1 || '%' OR description ILIKE '%' || $1 || '%' OR body ILIKE '%' || $1 || '%'
				OR to_tsvector('simple', title || ' ' || description || ' ' || body) @@ plainto_tsquery('simple', $1))
		ORDER BY (CASE WHEN title ILIKE '%' || $1 || '%' THEN 1 ELSE 0 END) DESC,
			ts_rank(to_tsvector('simple', title || ' ' || description || ' ' || body), plainto_tsquery('simple', $1)) DESC,
			updated_at DESC
		LIMIT $2`, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	results := make([]*SearchHit, 0, limit)
	for rows.Next() {
		item := &SearchHit{}
		if err := rows.Scan(&item.Route, &item.Title, &item.Snippet); err != nil {
			return nil, err
		}
		results = append(results, item)
	}
	return results, rows.Err()
}
