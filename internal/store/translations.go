package store

import (
	"context"
	"time"
)

type TranslationPage struct {
	Route                  string
	Title                  string
	Version                string
	TranslationBaseVersion string
	UpdatedAt              time.Time
}

type TranslationGroup struct {
	Key   string
	Pages []*TranslationPage
}

func (s *Store) ListTranslationGroups(ctx context.Context, limit, offset int) ([]*TranslationGroup, int, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(DISTINCT translation_key) FROM pages
		WHERE draft = false AND translation_key <> ''`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `SELECT translation_key FROM pages
		WHERE draft = false AND translation_key <> ''
		GROUP BY translation_key
		ORDER BY translation_key
		LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	keys := make([]string, 0, limit)
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, 0, err
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if len(keys) == 0 {
		return nil, total, nil
	}

	pageRows, err := s.pool.Query(ctx, `SELECT translation_key, route, title, version,
			translation_base_version, updated_at
		FROM pages WHERE draft = false AND translation_key = ANY($1)
		ORDER BY translation_key, route`, keys)
	if err != nil {
		return nil, 0, err
	}
	defer pageRows.Close()

	byKey := make(map[string]*TranslationGroup, len(keys))
	groups := make([]*TranslationGroup, 0, len(keys))
	for _, key := range keys {
		group := &TranslationGroup{Key: key}
		byKey[key] = group
		groups = append(groups, group)
	}
	for pageRows.Next() {
		var key string
		page := &TranslationPage{}
		if err := pageRows.Scan(&key, &page.Route, &page.Title, &page.Version,
			&page.TranslationBaseVersion, &page.UpdatedAt); err != nil {
			return nil, 0, err
		}
		if group := byKey[key]; group != nil {
			group.Pages = append(group.Pages, page)
		}
	}
	return groups, total, pageRows.Err()
}
