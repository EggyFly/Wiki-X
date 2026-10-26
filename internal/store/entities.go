package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/EggyFly/Wiki-X/internal/content"
)

const (
	EntityKindText   = "text"
	EntityKindNumber = "number"
	EntityKindBool   = "bool"
	EntityKindDate   = "date"
	EntityKindURL    = "url"
	EntityKindImage  = "image"
	EntityKindSelect = "select"
)

var entityNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)

type EntityType struct {
	ID        int64
	Name      string
	Label     string
	Fields    []*EntityField
	CreatedAt time.Time
}

type EntityField struct {
	ID        int64
	TypeID    int64
	Name      string
	Label     string
	Kind      string
	Required  bool
	Options   []string
	SortOrder int
}

type EntityRecord struct {
	ID          int64
	TypeID      int64
	TypeName    string
	TypeLabel   string
	Name        string
	Label       string
	Data        map[string]string
	Route       string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	CreatedByID *int64
}

type EntityInput struct {
	ID      int64
	TypeID  int64
	Name    string
	Label   string
	Data    map[string]string
	ActorID int64
}

func EntityKinds() []string {
	return []string{EntityKindText, EntityKindNumber, EntityKindBool, EntityKindDate, EntityKindURL, EntityKindImage, EntityKindSelect}
}

func (s *Store) ListEntityTypes(ctx context.Context) ([]*EntityType, error) {
	rows, err := s.read().Query(ctx, `SELECT id, name, label, created_at FROM entity_types WHERE revoked_at IS NULL ORDER BY name`)
	if err != nil {
		return nil, err
	}
	types := make([]*EntityType, 0)
	index := make(map[int64]*EntityType)
	for rows.Next() {
		item := &EntityType{}
		if err := rows.Scan(&item.ID, &item.Name, &item.Label, &item.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		types = append(types, item)
		index[item.ID] = item
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	fieldRows, err := s.read().Query(ctx, `SELECT id, type_id, name, label, kind, required, options, sort_order
		FROM entity_fields WHERE revoked_at IS NULL ORDER BY type_id, sort_order, name`)
	if err != nil {
		return nil, err
	}
	defer fieldRows.Close()
	for fieldRows.Next() {
		field, err := scanEntityField(fieldRows)
		if err != nil {
			return nil, err
		}
		if item := index[field.TypeID]; item != nil {
			item.Fields = append(item.Fields, field)
		}
	}
	return types, fieldRows.Err()
}

func scanEntityField(rows pgx.Rows) (*EntityField, error) {
	field := &EntityField{}
	var options []byte
	if err := rows.Scan(&field.ID, &field.TypeID, &field.Name, &field.Label, &field.Kind,
		&field.Required, &options, &field.SortOrder); err != nil {
		return nil, err
	}
	if len(options) > 0 {
		if err := json.Unmarshal(options, &field.Options); err != nil {
			return nil, err
		}
	}
	if field.Options == nil {
		field.Options = []string{}
	}
	return field, nil
}

func (s *Store) CreateEntityType(ctx context.Context, name, label string, actorID int64) (*EntityType, error) {
	name = strings.TrimSpace(name)
	label = strings.TrimSpace(label)
	if !entityNamePattern.MatchString(name) {
		return nil, ErrBadEntity
	}
	if utf8.RuneCountInString(label) > 100 {
		return nil, ErrBadEntity
	}
	if label == "" {
		label = name
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var id int64
	err = tx.QueryRow(ctx, `INSERT INTO entity_types (name, label) VALUES ($1, $2) RETURNING id`, name, label).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrEntityTypeExists
		}
		return nil, err
	}
	if err := logAudit(ctx, tx, actorID, "entity_type.create", name, ""); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &EntityType{ID: id, Name: name, Label: label, Fields: []*EntityField{}}, nil
}

func (s *Store) DeleteEntityType(ctx context.Context, id, actorID int64) error {
	if !s.revokeEnabled(SectionEntity) {
		return ErrRevokeDisabled
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM entities WHERE type_id = $1 AND revoked_at IS NULL`, id).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return ErrEntityTypeNotEmpty
	}
	var name string
	err = tx.QueryRow(ctx, `UPDATE entity_types
		SET revoked_at=now(), revoked_by=NULLIF($2,0), revoke_reason='deleted'
		WHERE id=$1 AND revoked_at IS NULL RETURNING name`, id, actorID).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE entity_fields
		SET revoked_at=now(), revoked_by=NULLIF($2,0), revoke_reason='entity type deleted'
		WHERE type_id=$1 AND revoked_at IS NULL`, id, actorID); err != nil {
		return err
	}
	if err := logAudit(ctx, tx, actorID, "entity_type.delete", name, "revoked"); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) AddEntityField(ctx context.Context, typeID int64, name, label, kind string, required bool, options []string, sortOrder int, actorID int64) error {
	name = strings.TrimSpace(name)
	label = strings.TrimSpace(label)
	kind = strings.TrimSpace(kind)
	if !entityNamePattern.MatchString(name) || !validEntityKind(kind) {
		return ErrBadEntity
	}
	if utf8.RuneCountInString(label) > 100 {
		return ErrBadEntity
	}
	if label == "" {
		label = name
	}
	options = cleanOptions(options)
	if kind != EntityKindSelect {
		options = []string{}
	}
	if kind == EntityKindSelect && len(options) == 0 {
		return ErrBadEntity
	}
	encoded, err := json.Marshal(options)
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var typeName string
	if err := tx.QueryRow(ctx, `SELECT name FROM entity_types WHERE id = $1 AND revoked_at IS NULL`, typeID).Scan(&typeName); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if sortOrder <= 0 {
		if err := tx.QueryRow(ctx, `SELECT COALESCE(max(sort_order), 0) + 1 FROM entity_fields WHERE type_id = $1 AND revoked_at IS NULL`, typeID).Scan(&sortOrder); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO entity_fields (type_id, name, label, kind, required, options, sort_order)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`, typeID, name, label, kind, required, encoded, sortOrder); err != nil {
		if isUniqueViolation(err) {
			return ErrEntityFieldExists
		}
		return err
	}
	if err := logAudit(ctx, tx, actorID, "entity_field.create", typeName+"."+name, kind); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) DeleteEntityField(ctx context.Context, id, actorID int64) error {
	if !s.revokeEnabled(SectionEntity) {
		return ErrRevokeDisabled
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var name, typeName string
	var typeID int64
	err = tx.QueryRow(ctx, `SELECT f.name, t.name, f.type_id FROM entity_fields f JOIN entity_types t ON t.id = f.type_id
		WHERE f.id = $1 AND f.revoked_at IS NULL AND t.revoked_at IS NULL`, id).Scan(&name, &typeName, &typeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	var archived string
	if err := tx.QueryRow(ctx, `SELECT COALESCE(jsonb_object_agg(id::text, value)::text, '{}') FROM (
		SELECT id, data->$2 AS value FROM entities
		WHERE type_id=$1 AND data ? $2 ORDER BY id LIMIT 100
	) x`, typeID, name).Scan(&archived); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE entity_fields
		SET revoked_at=now(), revoked_by=NULLIF($2,0), revoke_reason='deleted'
		WHERE id=$1 AND revoked_at IS NULL`, id, actorID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE entities SET data = data - $2 WHERE type_id = $1`, typeID, name); err != nil {
		return err
	}
	if err := logAudit(ctx, tx, actorID, "entity_field.delete", typeName+"."+name,
		"revoked; archived_values="+truncateAuditText(archived, 4000)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) ListEntities(ctx context.Context, limit int) ([]*EntityRecord, error) {
	if limit <= 0 || limit > 5000 {
		limit = 500
	}
	rows, err := s.read().Query(ctx, entitySelectSQL+` WHERE e.revoked_at IS NULL AND t.revoked_at IS NULL ORDER BY e.type_id, e.name LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*EntityRecord, 0, limit)
	for rows.Next() {
		item, err := scanEntityRecord(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

const entitySelectSQL = `SELECT e.id, e.type_id, t.name, t.label, e.name, e.label, e.data, COALESCE(p.route, ''),
	e.created_at, e.updated_at, e.created_by
	FROM entities e
	JOIN entity_types t ON t.id = e.type_id
	LEFT JOIN LATERAL (SELECT route FROM pages WHERE entity_id = e.id ORDER BY route LIMIT 1) p ON true`

func scanEntityRecord(rows entityRows) (*EntityRecord, error) {
	item := &EntityRecord{}
	var data []byte
	if err := rows.Scan(&item.ID, &item.TypeID, &item.TypeName, &item.TypeLabel, &item.Name, &item.Label,
		&data, &item.Route, &item.CreatedAt, &item.UpdatedAt, &item.CreatedByID); err != nil {
		return nil, err
	}
	item.Data = make(map[string]string)
	if len(data) > 0 {
		if err := json.Unmarshal(data, &item.Data); err != nil {
			return nil, err
		}
	}
	return item, nil
}

func (s *Store) GetEntity(ctx context.Context, id int64) (*EntityRecord, error) {
	row := s.read().QueryRow(ctx, entitySelectSQL+` WHERE e.id = $1 AND e.revoked_at IS NULL AND t.revoked_at IS NULL`, id)
	item, err := scanEntityRecord(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return item, err
}

func (s *Store) GetEntityByName(ctx context.Context, name string) (*EntityRecord, error) {
	row := s.read().QueryRow(ctx, entitySelectSQL+` WHERE e.name = $1 AND e.revoked_at IS NULL AND t.revoked_at IS NULL`, name)
	item, err := scanEntityRecord(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return item, err
}

type entityRows interface {
	Scan(dest ...any) error
}

func (s *Store) SaveEntity(ctx context.Context, in EntityInput) (*EntityRecord, error) {
	name := strings.TrimSpace(in.Name)
	label := strings.TrimSpace(in.Label)
	if !entityNamePattern.MatchString(name) || utf8.RuneCountInString(label) > 200 {
		return nil, ErrBadEntity
	}
	if label == "" {
		label = name
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var typeName string
	err = tx.QueryRow(ctx, `SELECT name FROM entity_types WHERE id = $1 AND revoked_at IS NULL`, in.TypeID).Scan(&typeName)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	fields, err := s.entityFieldsTx(ctx, tx, in.TypeID)
	if err != nil {
		return nil, err
	}
	if err := validateEntityData(fields, in.Data); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(in.Data)
	if err != nil {
		return nil, err
	}
	var id int64
	action := "entity.create"
	if in.ID > 0 {
		var existingType int64
		err := tx.QueryRow(ctx, `SELECT type_id FROM entities WHERE id = $1 AND revoked_at IS NULL FOR UPDATE`, in.ID).Scan(&existingType)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		if existingType != in.TypeID {
			return nil, ErrBadEntity
		}
		err = tx.QueryRow(ctx, `UPDATE entities SET name = $2, label = $3, data = $4, updated_at = now()
			WHERE id = $1 AND revoked_at IS NULL RETURNING id`, in.ID, name, label, encoded).Scan(&id)
		action = "entity.update"
	} else {
		err = tx.QueryRow(ctx, `INSERT INTO entities (type_id, name, label, data, created_by)
			VALUES ($1, $2, $3, $4, NULLIF($5, 0)) RETURNING id`, in.TypeID, name, label, encoded, in.ActorID).Scan(&id)
	}
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrEntityExists
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if err := logAudit(ctx, tx, in.ActorID, action, name, typeName); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.GetEntity(ctx, id)
}

func (s *Store) DeleteEntity(ctx context.Context, id, actorID int64) error {
	if !s.revokeEnabled(SectionEntity) {
		return ErrRevokeDisabled
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var name string
	err = tx.QueryRow(ctx, `UPDATE entities
		SET revoked_at=now(), revoked_by=NULLIF($2,0), revoke_reason='deleted'
		WHERE id=$1 AND revoked_at IS NULL RETURNING name`, id, actorID).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE pages SET entity_id=NULL WHERE entity_id=$1`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE page_revisions SET entity_id=NULL WHERE entity_id=$1`, id); err != nil {
		return err
	}
	if err := logAudit(ctx, tx, actorID, "entity.delete", name, "revoked"); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) LoadEntities(ctx context.Context) ([]*content.Entity, error) {
	types, err := s.ListEntityTypes(ctx)
	if err != nil {
		return nil, err
	}
	typeIndex := make(map[int64]*EntityType, len(types))
	for _, item := range types {
		typeIndex[item.ID] = item
	}
	rows, err := s.read().Query(ctx, `SELECT id, type_id, name, label, data FROM entities
		WHERE revoked_at IS NULL AND type_id IN (SELECT id FROM entity_types WHERE revoked_at IS NULL) ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*content.Entity
	for rows.Next() {
		var id, typeID int64
		var name, label string
		var raw []byte
		if err := rows.Scan(&id, &typeID, &name, &label, &raw); err != nil {
			return nil, err
		}
		item := &content.Entity{Key: name, Label: label}
		if typeInfo := typeIndex[typeID]; typeInfo != nil {
			item.Type = typeInfo.Label
		}
		values := make(map[string]string)
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &values); err != nil {
				return nil, err
			}
		}
		if typeInfo := typeIndex[typeID]; typeInfo != nil {
			for _, field := range typeInfo.Fields {
				item.Fields = append(item.Fields, content.EntityField{
					Name:  field.Name,
					Label: field.Label,
					Value: values[field.Name],
				})
			}
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) entityFieldsTx(ctx context.Context, tx pgx.Tx, typeID int64) ([]*EntityField, error) {
	rows, err := tx.Query(ctx, `SELECT id, type_id, name, label, kind, required, options, sort_order
		FROM entity_fields WHERE type_id = $1 AND revoked_at IS NULL ORDER BY sort_order, name`, typeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var fields []*EntityField
	for rows.Next() {
		field, err := scanEntityField(rows)
		if err != nil {
			return nil, err
		}
		fields = append(fields, field)
	}
	return fields, rows.Err()
}

func validateEntityData(fields []*EntityField, data map[string]string) error {
	known := make(map[string]*EntityField, len(fields))
	for _, field := range fields {
		known[field.Name] = field
	}
	total := 0
	for key, value := range data {
		field, ok := known[key]
		if !ok {
			return fmt.Errorf("%w: unknown field %s", ErrBadEntity, key)
		}
		value = strings.TrimSpace(value)
		data[key] = value
		total += len(key) + len(value)
		if utf8.RuneCountInString(value) > 2000 {
			return fmt.Errorf("%w: field %s is too long", ErrBadEntity, key)
		}
		if value == "" {
			continue
		}
		switch field.Kind {
		case EntityKindNumber:
			if _, err := strconv.ParseFloat(value, 64); err != nil {
				return fmt.Errorf("%w: field %s must be a number", ErrBadEntity, key)
			}
		case EntityKindBool:
			switch strings.ToLower(value) {
			case "true", "false", "1", "0", "yes", "no":
			default:
				return fmt.Errorf("%w: field %s must be a boolean", ErrBadEntity, key)
			}
		case EntityKindDate:
			if _, err := time.Parse("2006-01-02", value); err != nil {
				return fmt.Errorf("%w: field %s must be a date", ErrBadEntity, key)
			}
		case EntityKindURL:
			parsed, err := url.Parse(value)
			if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
				return fmt.Errorf("%w: field %s must be a http(s) url", ErrBadEntity, key)
			}
		case EntityKindImage:
			if !validEntityImage(value) {
				return fmt.Errorf("%w: field %s must be a site path or http(s) url", ErrBadEntity, key)
			}
		case EntityKindSelect:
			if !containsString(field.Options, value) {
				return fmt.Errorf("%w: field %s is not an allowed option", ErrBadEntity, key)
			}
		case EntityKindText:
		default:
			return fmt.Errorf("%w: field %s has an unknown kind", ErrBadEntity, key)
		}
	}
	if total > 16*1024 {
		return fmt.Errorf("%w: entity data is too large", ErrBadEntity)
	}
	for _, field := range fields {
		if field.Required && strings.TrimSpace(data[field.Name]) == "" {
			return fmt.Errorf("%w: field %s is required", ErrBadEntity, field.Name)
		}
	}
	return nil
}

func validEntityImage(value string) bool {
	if strings.HasPrefix(value, "/") {
		if strings.HasPrefix(value, "//") || strings.Contains(value, "\\") || strings.Contains(value, "..") {
			return false
		}
		for _, r := range value {
			if r < 0x20 {
				return false
			}
		}
		return true
	}
	parsed, err := url.Parse(value)
	return err == nil && parsed.Host != "" && (parsed.Scheme == "http" || parsed.Scheme == "https")
}

func validEntityKind(kind string) bool {
	for _, item := range EntityKinds() {
		if item == kind {
			return true
		}
	}
	return false
}

func cleanOptions(options []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(options))
	for _, option := range options {
		option = strings.TrimSpace(option)
		if option == "" || seen[option] || utf8.RuneCountInString(option) > 100 {
			continue
		}
		seen[option] = true
		result = append(result, option)
		if len(result) >= 50 {
			break
		}
	}
	sort.Strings(result)
	return result
}

func truncateAuditText(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "..."
}
