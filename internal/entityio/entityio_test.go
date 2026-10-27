package entityio

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/EggyFly/Wiki-X/internal/config"
	"github.com/EggyFly/Wiki-X/internal/store"
)

func TestExportImport(t *testing.T) {
	databaseURL := os.Getenv("TEST_POSTGRES_URL")
	if databaseURL == "" {
		t.Skip("set TEST_POSTGRES_URL to run entity io tests")
	}
	ctx := context.Background()
	data, err := store.Open(ctx, config.DatabaseConfig{URL: databaseURL, MaxConns: 4})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(data.Close)
	if err := data.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	admin, err := data.CreateUser(ctx, fmt.Sprintf("entity-admin-%d", time.Now().UnixNano()), "", "hash", true)
	if err != nil {
		t.Fatal(err)
	}
	typeName := fmt.Sprintf("io%d", time.Now().UnixNano())
	firstName := typeName + "a"
	secondName := typeName + "b"
	entityType, err := data.CreateEntityType(ctx, typeName, "IO Type", admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := data.AddEntityField(ctx, entityType.ID, "hp", "HP", store.EntityKindNumber, true, nil, 1, admin.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := data.SaveEntity(ctx, store.EntityInput{
		TypeID: entityType.ID, Name: firstName, Label: "First",
		Data: map[string]string{"hp": "100"}, ActorID: admin.ID,
	}); err != nil {
		t.Fatal(err)
	}
	doc, err := Export(ctx, data)
	if err != nil {
		t.Fatal(err)
	}
	filtered := &Document{Version: 1}
	for _, item := range doc.Types {
		if item.Name == typeName {
			filtered.Types = append(filtered.Types, item)
		}
	}
	for _, item := range doc.Entities {
		if item.Type == typeName {
			filtered.Entities = append(filtered.Entities, item)
		}
	}
	if len(filtered.Types) != 1 || len(filtered.Entities) != 1 {
		t.Fatalf("filtered export = %+v", filtered)
	}
	filtered.Entities[0].Label = "First Updated"
	filtered.Entities[0].Data["hp"] = "120"
	filtered.Entities = append(filtered.Entities, Entity{
		Type: typeName, Name: secondName, Label: "Second", Data: map[string]string{"hp": "90"},
	})
	result, err := Import(ctx, data, filtered, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Types != 0 || result.Fields != 0 || result.Saved != 2 {
		t.Fatalf("import result = %+v", result)
	}
	updated, err := data.GetEntityByName(ctx, firstName)
	if err != nil || updated.Label != "First Updated" || updated.Data["hp"] != "120" {
		t.Fatalf("updated entity = %+v, %v", updated, err)
	}
	if _, err := data.GetEntityByName(ctx, secondName); err != nil {
		t.Fatalf("new entity missing: %v", err)
	}
	newDoc := &Document{Version: 1, Types: []Type{{
		Name: typeName + "new", Label: "New Type",
		Fields: []Field{{Name: "title", Label: "Title", Kind: store.EntityKindText}},
	}}}
	newResult, err := Import(ctx, data, newDoc, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if newResult.Types != 1 || newResult.Fields != 1 {
		t.Fatalf("new type result = %+v", newResult)
	}
}
