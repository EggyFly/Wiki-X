package entityio

import (
	"context"
	"errors"
	"fmt"

	"github.com/EggyFly/Wiki-X/internal/store"
)

type Document struct {
	Version  int      `json:"version"`
	Types    []Type   `json:"types"`
	Entities []Entity `json:"entities"`
}

// EntityTx 是 entityio 需要的存储写入面, *store.Store 和它的事务包装都实现这组方法.
type EntityTx interface {
	ListEntityTypes(ctx context.Context) ([]*store.EntityType, error)
	CreateEntityType(ctx context.Context, name, label string, actorID int64) (*store.EntityType, error)
	AddEntityField(ctx context.Context, typeID int64, name, label, kind string, required bool, options []string, sortOrder int, actorID int64) error
	GetEntityByName(ctx context.Context, name string) (*store.EntityRecord, error)
	SaveEntity(ctx context.Context, in store.EntityInput) (*store.EntityRecord, error)
}

type Type struct {
	Name   string  `json:"name"`
	Label  string  `json:"label"`
	Fields []Field `json:"fields"`
}

type Field struct {
	Name      string   `json:"name"`
	Label     string   `json:"label"`
	Kind      string   `json:"kind"`
	Required  bool     `json:"required"`
	Options   []string `json:"options,omitempty"`
	SortOrder int      `json:"sort_order,omitempty"`
}

type Entity struct {
	Type  string            `json:"type"`
	Name  string            `json:"name"`
	Label string            `json:"label"`
	Data  map[string]string `json:"data"`
}

type Result struct {
	Types  int
	Fields int
	Saved  int
}

func Export(ctx context.Context, data *store.Store) (*Document, error) {
	types, err := data.ListEntityTypes(ctx)
	if err != nil {
		return nil, err
	}
	doc := &Document{Version: 1, Types: make([]Type, 0, len(types)), Entities: make([]Entity, 0)}
	for _, item := range types {
		exportedType := Type{Name: item.Name, Label: item.Label, Fields: make([]Field, 0, len(item.Fields))}
		for _, field := range item.Fields {
			exportedType.Fields = append(exportedType.Fields, Field{
				Name: field.Name, Label: field.Label, Kind: field.Kind,
				Required: field.Required, Options: field.Options, SortOrder: field.SortOrder,
			})
		}
		doc.Types = append(doc.Types, exportedType)
		entities, err := data.ListEntitiesByType(ctx, item.Name)
		if err != nil {
			return nil, err
		}
		for _, entity := range entities {
			doc.Entities = append(doc.Entities, Entity{
				Type: item.Name, Name: entity.Name, Label: entity.Label, Data: entity.Data,
			})
		}
	}
	return doc, nil
}

func Import(ctx context.Context, data *store.Store, doc *Document, actorID int64) (*Result, error) {
	if doc == nil {
		return nil, fmt.Errorf("empty entity document")
	}
	if doc.Version != 0 && doc.Version != 1 {
		return nil, fmt.Errorf("unsupported entity document version %d", doc.Version)
	}
	if len(doc.Types) > 500 || len(doc.Entities) > 10000 {
		return nil, fmt.Errorf("entity document is too large")
	}
	tx, err := data.BeginTx(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	result, err := importTx(ctx, tx, doc, actorID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

func importTx(ctx context.Context, tx EntityTx, doc *Document, actorID int64) (*Result, error) {
	result := &Result{}
	existingTypes, err := tx.ListEntityTypes(ctx)
	if err != nil {
		return nil, err
	}
	typeIndex := make(map[string]*store.EntityType, len(existingTypes))
	for _, item := range existingTypes {
		typeIndex[item.Name] = item
	}
	for _, item := range doc.Types {
		entityType := typeIndex[item.Name]
		if entityType == nil {
			created, err := tx.CreateEntityType(ctx, item.Name, item.Label, actorID)
			if err != nil {
				return nil, fmt.Errorf("create entity type %s: %w", item.Name, err)
			}
			entityType = created
			typeIndex[item.Name] = created
			result.Types++
		}
		fields := make(map[string]bool, len(entityType.Fields))
		for _, field := range entityType.Fields {
			fields[field.Name] = true
		}
		for _, field := range item.Fields {
			if fields[field.Name] {
				continue
			}
			if err := tx.AddEntityField(ctx, entityType.ID, field.Name, field.Label, field.Kind,
				field.Required, field.Options, field.SortOrder, actorID); err != nil {
				return nil, fmt.Errorf("create entity field %s.%s: %w", item.Name, field.Name, err)
			}
			fields[field.Name] = true
			result.Fields++
		}
	}
	for _, item := range doc.Entities {
		entityType := typeIndex[item.Type]
		if entityType == nil {
			return nil, fmt.Errorf("unknown entity type %s", item.Type)
		}
		input := store.EntityInput{
			TypeID: entityType.ID, Name: item.Name, Label: item.Label,
			Data: item.Data, ActorID: actorID,
		}
		existing, err := tx.GetEntityByName(ctx, item.Name)
		if err == nil {
			if existing.TypeID != entityType.ID {
				return nil, fmt.Errorf("entity %s belongs to another type", item.Name)
			}
			input.ID = existing.ID
		} else if !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
		if _, err := tx.SaveEntity(ctx, input); err != nil {
			return nil, fmt.Errorf("save entity %s: %w", item.Name, err)
		}
		result.Saved++
	}
	return result, nil
}
