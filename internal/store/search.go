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
	like := escapeLike(query)
	rows, err := s.read().Query(ctx, `WITH q AS (SELECT plainto_tsquery('simple', $1) AS tsq)
		SELECT route, title,
			CASE WHEN strpos(lower(body), lower($1)) > 0
				THEN substring(body from greatest(strpos(lower(body), lower($1)) - 80, 1) for 240)
				ELSE left(body, 240) END AS snippet
		FROM pages, q
		WHERE draft = false AND deleted_at IS NULL
			AND (title ILIKE '%' || $2 || '%' ESCAPE E'\\' OR description ILIKE '%' || $2 || '%' ESCAPE E'\\'
				OR body ILIKE '%' || $2 || '%' ESCAPE E'\\' OR search_vector @@ q.tsq)
		ORDER BY (CASE WHEN title ILIKE '%' || $2 || '%' ESCAPE E'\\' THEN 1 ELSE 0 END) DESC,
			ts_rank(search_vector, q.tsq) DESC,
			updated_at DESC
		LIMIT $3`, query, like, limit)
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

func escapeLike(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(value)
}
