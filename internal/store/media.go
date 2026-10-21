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
