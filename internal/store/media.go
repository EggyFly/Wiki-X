package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

func (s *Store) CreateMedia(ctx context.Context, fileName, contentType string, size int64, uploaderID int64) (*Media, error) {
	media := &Media{FileName: fileName, ContentType: contentType, Size: size}
	if uploaderID > 0 {
		media.UploaderID = &uploaderID
	}
	err := s.pool.QueryRow(ctx, `INSERT INTO media (file_name, content_type, size, uploader_id)
		VALUES ($1, $2, $3, NULLIF($4, 0)) RETURNING id, created_at`,
		fileName, contentType, size, uploaderID).Scan(&media.ID, &media.CreatedAt)
	if err != nil {
		return nil, err
	}
	return media, nil
}

func (s *Store) EnsureMedia(ctx context.Context, fileName, contentType string, size int64, uploaderID int64) (*Media, error) {
	media := &Media{FileName: fileName, ContentType: contentType, Size: size}
	if uploaderID > 0 {
		media.UploaderID = &uploaderID
	}
	err := s.pool.QueryRow(ctx, `INSERT INTO media (file_name, content_type, size, uploader_id)
		VALUES ($1, $2, $3, NULLIF($4, 0))
		ON CONFLICT (file_name) DO UPDATE SET content_type = EXCLUDED.content_type, size = EXCLUDED.size
		RETURNING id, created_at`, fileName, contentType, size, uploaderID).Scan(&media.ID, &media.CreatedAt)
	if err != nil {
		return nil, err
	}
	return media, nil
}

type MediaRow struct {
	FileName    string
	ContentType string
	Size        int64
}

// ListMediaWithoutThumb 分页返回还没有缩略图的图片媒体, 按 id 升序, 供回填命令逐批处理.
func (s *Store) ListMediaWithoutThumb(ctx context.Context, afterID int64, limit int) ([]*MediaRow, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `SELECT m.file_name, m.content_type, m.size FROM media m
		WHERE m.id > $1 AND m.file_name NOT LIKE 't-%'
			AND (m.content_type LIKE 'image/%')
			AND NOT EXISTS (SELECT 1 FROM media t WHERE t.file_name = 't-' || regexp_replace(m.file_name, '\.[a-zA-Z0-9]+$', '.jpg'))
		ORDER BY m.id LIMIT $2`, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*MediaRow, 0, limit)
	for rows.Next() {
		item := &MediaRow{}
		if err := rows.Scan(&item.FileName, &item.ContentType, &item.Size); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) MaxMediaID(ctx context.Context) (int64, error) {
	var maxID int64
	err := s.pool.QueryRow(ctx, `SELECT COALESCE(max(id), 0) FROM media`).Scan(&maxID)
	return maxID, err
}

func (s *Store) GetMediaID(ctx context.Context, fileName string) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `SELECT id FROM media WHERE file_name = $1`, fileName).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	return id, err
}

func (s *Store) GetMediaByStem(ctx context.Context, stem string) (*Media, error) {
	media := &Media{}
	err := s.pool.QueryRow(ctx, `SELECT id, file_name, content_type, size, uploader_id, created_at
		FROM media WHERE file_name LIKE $1 AND file_name NOT LIKE 't-%'
		ORDER BY id DESC LIMIT 1`, stem+".%").
		Scan(&media.ID, &media.FileName, &media.ContentType, &media.Size, &media.UploaderID, &media.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return media, nil
}

func (s *Store) GetMedia(ctx context.Context, fileName string) (*Media, error) {
	media := &Media{}
	err := s.pool.QueryRow(ctx, `SELECT id, file_name, content_type, size, uploader_id, created_at
		FROM media WHERE file_name = $1`, fileName).
		Scan(&media.ID, &media.FileName, &media.ContentType, &media.Size, &media.UploaderID, &media.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return media, nil
}
