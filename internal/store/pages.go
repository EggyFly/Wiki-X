package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/EggyFly/Wiki-X/internal/content"
)

func (s *Store) Load(ctx context.Context) ([]*content.Page, error) {
	rows, err := s.pool.Query(ctx, `SELECT p.route, p.title, p.description, p.body, p.tags, p.version, p.translation_key,
			p.protected, p.protection_level, p.updated_at, COALESCE(e.name, '')
		FROM pages p LEFT JOIN entities e ON e.id = p.entity_id
		WHERE p.draft = false AND p.deleted_at IS NULL ORDER BY p.route`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pages []*content.Page
	for rows.Next() {
		var page content.Page
		var updatedAt time.Time
		if err := rows.Scan(&page.Route, &page.Title, &page.Description, &page.Body, &page.Tags, &page.Version, &page.TranslationKey, &page.Protected, &page.ProtectionLevel, &updatedAt, &page.EntityKey); err != nil {
			return nil, err
		}
		page.Key = page.Route
		page.SourcePath = "postgres"
		page.Updated = updatedAt.Format("2006-01-02")
		if page.Tags == nil {
			page.Tags = []string{}
		}
		pages = append(pages, &page)
	}
	return pages, rows.Err()
}

func (s *Store) GetPage(ctx context.Context, route string) (*PageRecord, error) {
	row := s.pool.QueryRow(ctx, `SELECT p.id, p.route, p.title, p.description, p.body, p.tags, p.draft, p.version,
			p.translation_key, p.translation_base_version, p.protected, p.protection_level, p.updated_at, p.current_revision_id,
			p.entity_id, COALESCE(e.name, ''), p.deleted_at, p.deleted_by, p.delete_reason
		FROM pages p LEFT JOIN entities e ON e.id = p.entity_id WHERE p.route = $1`, route)
	var page PageRecord
	if err := row.Scan(&page.ID, &page.Route, &page.Title, &page.Description, &page.Body, &page.Tags, &page.Draft,
		&page.Version, &page.TranslationKey, &page.TranslationBaseVersion, &page.Protected, &page.ProtectionLevel, &page.UpdatedAt,
		&page.CurrentRevisionID, &page.EntityID, &page.EntityKey, &page.DeletedAt, &page.DeletedBy, &page.DeleteReason); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if page.Tags == nil {
		page.Tags = []string{}
	}
	return &page, nil
}

func (s *Store) CreateRevision(ctx context.Context, in RevisionInput) (*Revision, error) {
	tags := in.Tags
	if tags == nil {
		tags = []string{}
	}
	version := pageVersion(in.Title, in.Description, in.Body, tags, in.TranslationKey)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var pageID int64
	var currentVersion string
	var draft bool
	var protectionLevel string
	var currentRevisionID *int64
	var deletedAt *time.Time
	err = tx.QueryRow(ctx, `SELECT id, version, draft, protection_level, current_revision_id, deleted_at
		FROM pages WHERE route = $1 FOR UPDATE`, in.Route).
		Scan(&pageID, &currentVersion, &draft, &protectionLevel, &currentRevisionID, &deletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		if in.BaseVersion != "" {
			return nil, ErrEditConflict
		}
		err = tx.QueryRow(ctx, `INSERT INTO pages (route, title, description, body, tags, draft, version, translation_key, entity_id, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, now()) RETURNING id`,
			in.Route, in.Title, in.Description, in.Body, tags, !in.Publish, version, in.TranslationKey, in.EntityID).Scan(&pageID)
		if err != nil {
			if isUniqueViolation(err) {
				return nil, ErrEditConflict
			}
			return nil, err
		}
	} else if err != nil {
		return nil, err
	} else {
		if deletedAt != nil {
			return nil, ErrNotFound
		}
		if draft || in.BaseVersion == "" || in.BaseVersion != currentVersion {
			return nil, ErrEditConflict
		}
		if protectionLevel == "" {
			protectionLevel = ProtectionNone
		}
		if !in.AllowProtected {
			if protectionLevel == ProtectionFull || (protectionLevel == ProtectionSemi && in.Publish) {
				return nil, ErrProtected
			}
		}
	}

	status := StatusPending
	if in.Publish {
		status = StatusPublished
	}
	var revisionID int64
	var createdAt time.Time
	var publishedAt *time.Time
	err = tx.QueryRow(ctx, `INSERT INTO page_revisions
		(page_id, title, description, body, tags, author_id, comment, status, base_revision_id, version, translation_key, translation_base_version, entity_id, published_at)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, 0), $7, $8, $9, $10, $11, $12, $13, CASE WHEN $14 THEN now() ELSE NULL END)
		RETURNING id, created_at, published_at`,
		pageID, in.Title, in.Description, in.Body, tags, in.AuthorID, in.Comment, status, currentRevisionID,
		version, in.TranslationKey, in.TranslationBaseVersion, in.EntityID, in.Publish).
		Scan(&revisionID, &createdAt, &publishedAt)
	if err != nil {
		return nil, err
	}

	if in.Publish {
		_, err = tx.Exec(ctx, `UPDATE pages SET title = $2, description = $3, body = $4, tags = $5,
			draft = false, version = $6, updated_at = now(), current_revision_id = $7, translation_key = $8,
			translation_base_version = $9, entity_id = $10 WHERE id = $1`,
			pageID, in.Title, in.Description, in.Body, tags, version, revisionID, in.TranslationKey, in.TranslationBaseVersion, in.EntityID)
		if err != nil {
			return nil, err
		}
		if err := logAudit(ctx, tx, in.AuthorID, "revision.publish", in.Route, fmt.Sprintf("revision:%d", revisionID)); err != nil {
			return nil, err
		}
	} else {
		if err := logAudit(ctx, tx, in.AuthorID, "revision.submit", in.Route, fmt.Sprintf("revision:%d", revisionID)); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	var authorID *int64
	if in.AuthorID > 0 {
		authorID = &in.AuthorID
	}
	return &Revision{
		ID:                     revisionID,
		PageID:                 pageID,
		Route:                  in.Route,
		Title:                  in.Title,
		Description:            in.Description,
		Body:                   in.Body,
		Tags:                   tags,
		AuthorID:               authorID,
		Comment:                in.Comment,
		Status:                 status,
		BaseRevisionID:         currentRevisionID,
		Version:                version,
		TranslationKey:         in.TranslationKey,
		TranslationBaseVersion: in.TranslationBaseVersion,
		EntityID:               in.EntityID,
		CreatedAt:              createdAt,
		PublishedAt:            publishedAt,
	}, nil
}

const (
	ProtectionNone = "none"
	ProtectionSemi = "semi"
	ProtectionFull = "full"
)

func (s *Store) SetPageProtected(ctx context.Context, route string, protected bool, actorID int64) error {
	level := ProtectionNone
	if protected {
		level = ProtectionSemi
	}
	return s.SetPageProtection(ctx, route, level, actorID)
}

func (s *Store) SetPageProtection(ctx context.Context, route, level string, actorID int64) error {
	switch level {
	case ProtectionNone, ProtectionSemi, ProtectionFull:
	default:
		return ErrBadProtection
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	result, err := tx.Exec(ctx, `UPDATE pages SET protected = $2, protection_level = $3 WHERE route = $1`,
		route, level != ProtectionNone, level)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	if err := logAudit(ctx, tx, actorID, "page.protect", route, level); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) PublishRevision(ctx context.Context, revisionID, reviewerID int64) (string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	var pageID int64
	var route, title, description, body, version, status, translationKey, translationBaseVersion string
	var tags []string
	var baseRevisionID, currentRevisionID, entityID *int64
	var deletedAt *time.Time
	err = tx.QueryRow(ctx, `SELECT r.page_id, p.route, r.title, r.description, r.body, r.tags, r.version, r.status,
			r.base_revision_id, p.current_revision_id, r.translation_key, r.translation_base_version, r.entity_id, p.deleted_at
		FROM page_revisions r JOIN pages p ON p.id = r.page_id
		WHERE r.id = $1 FOR UPDATE OF r, p`, revisionID).
		Scan(&pageID, &route, &title, &description, &body, &tags, &version, &status, &baseRevisionID, &currentRevisionID, &translationKey, &translationBaseVersion, &entityID, &deletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if status != StatusPending || deletedAt != nil {
		return "", ErrEditConflict
	}
	if !sameRevision(baseRevisionID, currentRevisionID) {
		return "", ErrEditConflict
	}

	_, err = tx.Exec(ctx, `UPDATE pages SET title = $2, description = $3, body = $4, tags = $5,
		draft = false, version = $6, updated_at = now(), current_revision_id = $7, translation_key = $8,
		translation_base_version = $9, entity_id = $10 WHERE id = $1`,
		pageID, title, description, body, tags, version, revisionID, translationKey, translationBaseVersion, entityID)
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `UPDATE page_revisions SET status = $2, published_at = now() WHERE id = $1`, revisionID, StatusPublished); err != nil {
		return "", err
	}
	if err := logAudit(ctx, tx, reviewerID, "revision.approve", route, fmt.Sprintf("revision:%d", revisionID)); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return route, nil
}

func (s *Store) RejectRevision(ctx context.Context, revisionID, reviewerID int64, reason string) (string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	var pageID int64
	var route, status string
	var draft bool
	err = tx.QueryRow(ctx, `SELECT p.id, p.route, r.status, p.draft FROM page_revisions r
		JOIN pages p ON p.id = r.page_id WHERE r.id = $1 FOR UPDATE OF r, p`, revisionID).
		Scan(&pageID, &route, &status, &draft)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if status != StatusPending {
		return "", ErrEditConflict
	}
	if _, err := tx.Exec(ctx, `UPDATE page_revisions SET status = $2 WHERE id = $1`, revisionID, StatusRejected); err != nil {
		return "", err
	}
	if draft {
		var published int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM page_revisions WHERE page_id = $1 AND status = 'published'`, pageID).Scan(&published); err != nil {
			return "", err
		}
		if published == 0 {
			if _, err := tx.Exec(ctx, `DELETE FROM pages WHERE id = $1`, pageID); err != nil {
				return "", err
			}
		}
	}
	if err := logAudit(ctx, tx, reviewerID, "revision.reject", route, reason); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return route, nil
}

func sameRevision(left, right *int64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func (s *Store) ListRevisions(ctx context.Context, route string, viewerID int64, canReview bool) ([]*Revision, error) {
	rows, err := s.pool.Query(ctx, `SELECT r.id, r.page_id, p.route, r.title, r.description, r.body, r.tags,
			r.author_id, COALESCE(u.name, ''), r.comment, r.status, r.base_revision_id, r.version, r.translation_key,
			r.translation_base_version, r.entity_id, r.created_at, r.published_at
		FROM page_revisions r
		JOIN pages p ON p.id = r.page_id
		LEFT JOIN users u ON u.id = r.author_id
		WHERE p.route = $1 AND (r.status = 'published' OR ($2 AND r.status IN ('pending', 'rejected')) OR r.author_id = $3)
		ORDER BY r.created_at DESC LIMIT 200`, route, canReview, viewerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRevisions(rows)
}

func (s *Store) GetRevision(ctx context.Context, revisionID int64) (*Revision, error) {
	row := s.pool.QueryRow(ctx, `SELECT r.id, r.page_id, p.route, r.title, r.description, r.body, r.tags,
			r.author_id, COALESCE(u.name, ''), r.comment, r.status, r.base_revision_id, r.version, r.translation_key,
			r.translation_base_version, r.entity_id, r.created_at, r.published_at
		FROM page_revisions r
		JOIN pages p ON p.id = r.page_id
		LEFT JOIN users u ON u.id = r.author_id
		WHERE r.id = $1`, revisionID)
	var revision Revision
	err := row.Scan(&revision.ID, &revision.PageID, &revision.Route, &revision.Title, &revision.Description, &revision.Body, &revision.Tags,
		&revision.AuthorID, &revision.AuthorName, &revision.Comment, &revision.Status, &revision.BaseRevisionID, &revision.Version,
		&revision.TranslationKey, &revision.TranslationBaseVersion, &revision.EntityID, &revision.CreatedAt, &revision.PublishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if revision.Tags == nil {
		revision.Tags = []string{}
	}
	return &revision, nil
}

func (s *Store) ListPendingRevisions(ctx context.Context, limit int) ([]*Revision, error) {
	rows, err := s.pool.Query(ctx, `SELECT r.id, r.page_id, p.route, r.title, r.description, r.body, r.tags,
			r.author_id, COALESCE(u.name, ''), r.comment, r.status, r.base_revision_id, r.version, r.translation_key,
			r.translation_base_version, r.entity_id, r.created_at, r.published_at
		FROM page_revisions r
		JOIN pages p ON p.id = r.page_id
		LEFT JOIN users u ON u.id = r.author_id
		WHERE r.status = 'pending' ORDER BY r.created_at ASC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRevisions(rows)
}

func (s *Store) CountPendingRevisions(ctx context.Context) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM page_revisions r JOIN pages p ON p.id = r.page_id
		WHERE r.status = 'pending' AND p.deleted_at IS NULL`).Scan(&count)
	return count, err
}

func (s *Store) ImportPages(ctx context.Context, pages []*content.Page, authorID *int64) (int, int, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback(ctx)

	created := 0
	updated := 0
	for _, page := range pages {
		if page.Draft {
			continue
		}
		tags := page.Tags
		if tags == nil {
			tags = []string{}
		}
		version := page.Version
		if version == "" {
			version = pageVersion(page.Title, page.Description, page.Body, tags, page.TranslationKey)
		}

		var pageID int64
		var existingVersion string
		var deletedAt *time.Time
		err := tx.QueryRow(ctx, `SELECT id, version, deleted_at FROM pages WHERE route = $1 FOR UPDATE`, page.Route).Scan(&pageID, &existingVersion, &deletedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			err = tx.QueryRow(ctx, `INSERT INTO pages (route, title, description, body, tags, draft, version, translation_key, updated_at)
				VALUES ($1, $2, $3, $4, $5, false, $6, $7, now()) RETURNING id`,
				page.Route, page.Title, page.Description, page.Body, tags, version, page.TranslationKey).Scan(&pageID)
			if err != nil {
				return 0, 0, err
			}
			revID, err := insertImportedRevision(ctx, tx, pageID, page, tags, version, authorID)
			if err != nil {
				return 0, 0, err
			}
			if _, err := tx.Exec(ctx, `UPDATE pages SET current_revision_id = $2 WHERE id = $1`, pageID, revID); err != nil {
				return 0, 0, err
			}
			created++
			continue
		}
		if err != nil {
			return 0, 0, err
		}
		if deletedAt != nil {
			continue
		}
		if existingVersion == version {
			continue
		}
		revID, err := insertImportedRevision(ctx, tx, pageID, page, tags, version, authorID)
		if err != nil {
			return 0, 0, err
		}
		if _, err := tx.Exec(ctx, `UPDATE pages SET title = $2, description = $3, body = $4, tags = $5,
			draft = false, version = $6, updated_at = now(), current_revision_id = $7, translation_key = $8 WHERE id = $1`,
			pageID, page.Title, page.Description, page.Body, tags, version, revID, page.TranslationKey); err != nil {
			return 0, 0, err
		}
		updated++
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, 0, err
	}
	return created, updated, nil
}

func insertImportedRevision(ctx context.Context, tx pgx.Tx, pageID int64, page *content.Page, tags []string, version string, authorID *int64) (int64, error) {
	var revID int64
	err := tx.QueryRow(ctx, `INSERT INTO page_revisions
		(page_id, title, description, body, tags, author_id, comment, status, version, translation_key, published_at)
		VALUES ($1, $2, $3, $4, $5, $6, 'imported', 'published', $7, $8, now()) RETURNING id`,
		pageID, page.Title, page.Description, page.Body, tags, authorID, version, page.TranslationKey).Scan(&revID)
	return revID, err
}

func scanRevisions(rows pgx.Rows) ([]*Revision, error) {
	var revisions []*Revision
	for rows.Next() {
		var revision Revision
		if err := rows.Scan(&revision.ID, &revision.PageID, &revision.Route, &revision.Title, &revision.Description, &revision.Body,
			&revision.Tags, &revision.AuthorID, &revision.AuthorName, &revision.Comment, &revision.Status,
			&revision.BaseRevisionID, &revision.Version, &revision.TranslationKey, &revision.TranslationBaseVersion,
			&revision.EntityID, &revision.CreatedAt, &revision.PublishedAt); err != nil {
			return nil, err
		}
		if revision.Tags == nil {
			revision.Tags = []string{}
		}
		revisions = append(revisions, &revision)
	}
	return revisions, rows.Err()
}

func logAudit(ctx context.Context, tx pgx.Tx, actorID int64, action, target, detail string) error {
	_, err := tx.Exec(ctx, `INSERT INTO audit_logs (actor_id, action, target, detail)
		VALUES (NULLIF($1, 0), $2, $3, $4)`, actorID, action, target, detail)
	return err
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func isEmailUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "users_email_lower_idx"
}
