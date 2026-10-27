package store

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

var interactiveKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)

const maxInteractiveConfig = 8 << 10

type InteractiveNode struct {
	ID          int64
	NodeType    string
	Key         string
	Title       string
	Description string
	Config      map[string]string
	Enabled     bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
	CreatedByID *int64
}

type InteractiveInput struct {
	ID          int64
	NodeType    string
	Key         string
	Title       string
	Description string
	Config      map[string]string
	Enabled     bool
	ActorID     int64
}

func (s *Store) ListInteractiveNodes(ctx context.Context, limit, offset int) ([]*InteractiveNode, int, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	var total int
	if err := s.read().QueryRow(ctx, `SELECT count(*) FROM interactive_nodes WHERE revoked_at IS NULL`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.read().Query(ctx, `SELECT id, node_type, node_key, title, description, config, enabled,
		created_at, updated_at, created_by FROM interactive_nodes
		WHERE revoked_at IS NULL ORDER BY node_type, node_key LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]*InteractiveNode, 0, limit)
	for rows.Next() {
		item, err := scanInteractiveNode(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (s *Store) GetInteractiveNode(ctx context.Context, nodeType, key string) (*InteractiveNode, error) {
	nodeType = strings.ToLower(strings.TrimSpace(nodeType))
	key = strings.TrimSpace(key)
	if !validInteractiveType(nodeType) {
		return nil, ErrNotFound
	}
	row := s.read().QueryRow(ctx, `SELECT id, node_type, node_key, title, description, config, enabled,
		created_at, updated_at, created_by FROM interactive_nodes
		WHERE node_type = $1 AND node_key = $2 AND revoked_at IS NULL AND enabled = true`, nodeType, key)
	item, err := scanInteractiveNode(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return item, err
}

func (s *Store) SaveInteractiveNode(ctx context.Context, in InteractiveInput) (*InteractiveNode, error) {
	in.NodeType = strings.ToLower(strings.TrimSpace(in.NodeType))
	in.Key = strings.TrimSpace(in.Key)
	in.Title = strings.TrimSpace(in.Title)
	in.Description = strings.TrimSpace(in.Description)
	if !validInteractiveType(in.NodeType) || !interactiveKeyPattern.MatchString(in.Key) {
		return nil, ErrBadInteractive
	}
	if in.Title == "" || utf8.RuneCountInString(in.Title) > 200 || utf8.RuneCountInString(in.Description) > 2000 {
		return nil, ErrBadInteractive
	}
	config, err := cleanInteractiveConfig(in.Config)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var id int64
	action := "interactive.create"
	if in.ID > 0 {
		action = "interactive.update"
		err = tx.QueryRow(ctx, `UPDATE interactive_nodes SET node_type=$2, node_key=$3, title=$4, description=$5,
			config=$6, enabled=$7, updated_at=now() WHERE id=$1 AND revoked_at IS NULL RETURNING id`,
			in.ID, in.NodeType, in.Key, in.Title, in.Description, encoded, in.Enabled).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
	} else {
		err = tx.QueryRow(ctx, `INSERT INTO interactive_nodes
			(node_type, node_key, title, description, config, enabled, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, 0)) RETURNING id`,
			in.NodeType, in.Key, in.Title, in.Description, encoded, in.Enabled, in.ActorID).Scan(&id)
	}
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrInteractiveExists
		}
		return nil, err
	}
	if err := logAudit(ctx, tx, in.ActorID, action, in.NodeType+"."+in.Key, ""); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.GetInteractiveNodeAny(ctx, id)
}

func (s *Store) GetInteractiveNodeAny(ctx context.Context, id int64) (*InteractiveNode, error) {
	row := s.pool.QueryRow(ctx, `SELECT id, node_type, node_key, title, description, config, enabled,
		created_at, updated_at, created_by FROM interactive_nodes WHERE id = $1 AND revoked_at IS NULL`, id)
	item, err := scanInteractiveNode(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return item, err
}

func (s *Store) DeleteInteractiveNode(ctx context.Context, id, actorID int64) error {
	if !s.revokeEnabled(SectionInteractive) {
		return ErrRevokeDisabled
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var nodeType, key string
	err = tx.QueryRow(ctx, `UPDATE interactive_nodes
		SET revoked_at=now(), revoked_by=NULLIF($2,0), revoke_reason='deleted'
		WHERE id=$1 AND revoked_at IS NULL RETURNING node_type, node_key`, id, actorID).Scan(&nodeType, &key)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if err := logAudit(ctx, tx, actorID, "interactive.delete", nodeType+"."+key, "revoked"); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func scanInteractiveNode(rows entityRows) (*InteractiveNode, error) {
	item := &InteractiveNode{}
	var raw []byte
	if err := rows.Scan(&item.ID, &item.NodeType, &item.Key, &item.Title, &item.Description, &raw,
		&item.Enabled, &item.CreatedAt, &item.UpdatedAt, &item.CreatedByID); err != nil {
		return nil, err
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &item.Config); err != nil {
			return nil, err
		}
	}
	if item.Config == nil {
		item.Config = map[string]string{}
	}
	return item, nil
}

func cleanInteractiveConfig(config map[string]string) (map[string]string, error) {
	if len(config) > 50 {
		return nil, ErrBadInteractive
	}
	result := make(map[string]string, len(config))
	keys := make([]string, 0, len(config))
	for key := range config {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, rawKey := range keys {
		key := strings.TrimSpace(rawKey)
		value := strings.TrimSpace(config[rawKey])
		if key == "" || len(key) > 50 || utf8.RuneCountInString(value) > 1000 {
			return nil, ErrBadInteractive
		}
		result[key] = value
	}
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) > maxInteractiveConfig {
		return nil, ErrBadInteractive
	}
	return result, nil
}

func validInteractiveType(value string) bool {
	return value == "widget" || value == "game"
}
