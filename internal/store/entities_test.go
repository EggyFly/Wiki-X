package store

import (
	"context"
	"errors"
	"testing"
)

func TestEntityAdmin(t *testing.T) {
	data := openTestStore(t)
	ctx := context.Background()
	admin, err := data.CreateUser(ctx, "admin", "", "hash", true)
	if err != nil {
		t.Fatal(err)
	}
	entityType, err := data.CreateEntityType(ctx, "character", "角色", admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := data.CreateEntityType(ctx, "character", "重复", admin.ID); !errors.Is(err, ErrEntityTypeExists) {
		t.Fatalf("duplicate type error = %v", err)
	}
	if _, err := data.CreateEntityType(ctx, "bad name", "", admin.ID); !errors.Is(err, ErrBadEntity) {
		t.Fatalf("bad type error = %v", err)
	}

	fields := []struct {
		name     string
		label    string
		kind     string
		required bool
		options  []string
	}{
		{"display_name", "名称", EntityKindText, true, nil},
		{"rarity", "稀有度", EntityKindSelect, true, []string{"S", "A"}},
		{"level", "等级", EntityKindNumber, false, nil},
		{"released", "上架时间", EntityKindDate, false, nil},
		{"portrait", "立绘", EntityKindImage, false, nil},
	}
	for _, field := range fields {
		if err := data.AddEntityField(ctx, entityType.ID, field.name, field.label, field.kind, field.required, field.options, 0, admin.ID); err != nil {
			t.Fatal(err)
		}
	}
	if err := data.AddEntityField(ctx, entityType.ID, "rarity", "重复", EntityKindText, false, nil, 0, admin.ID); !errors.Is(err, ErrEntityFieldExists) {
		t.Fatalf("duplicate field error = %v", err)
	}
	if err := data.AddEntityField(ctx, entityType.ID, "bad_kind", "", "wat", false, nil, 0, admin.ID); !errors.Is(err, ErrBadEntity) {
		t.Fatalf("bad kind error = %v", err)
	}
	if err := data.AddEntityField(ctx, entityType.ID, "empty_select", "", EntityKindSelect, false, nil, 0, admin.ID); !errors.Is(err, ErrBadEntity) {
		t.Fatalf("empty select error = %v", err)
	}

	types, err := data.ListEntityTypes(ctx)
	if err != nil || len(types) != 1 || len(types[0].Fields) != 5 {
		t.Fatalf("types = %+v, %v", types, err)
	}

	valid := map[string]string{
		"display_name": "琪亚娜",
		"rarity":       "S",
		"level":        "80",
		"released":     "2026-01-02",
		"portrait":     "/media/example.png",
	}
	entity, err := data.SaveEntity(ctx, EntityInput{TypeID: entityType.ID, Name: "kiana", Label: "琪亚娜", Data: valid, ActorID: admin.ID})
	if err != nil {
		t.Fatal(err)
	}
	if entity.Name != "kiana" || entity.Data["rarity"] != "S" || entity.TypeName != "character" {
		t.Fatalf("entity = %+v", entity)
	}
	if _, err := data.SaveEntity(ctx, EntityInput{TypeID: entityType.ID, Name: "kiana", Label: "重复", Data: valid, ActorID: admin.ID}); !errors.Is(err, ErrEntityExists) {
		t.Fatalf("duplicate entity error = %v", err)
	}
	missing := map[string]string{"rarity": "S"}
	if _, err := data.SaveEntity(ctx, EntityInput{TypeID: entityType.ID, Name: "missing", Data: missing, ActorID: admin.ID}); !errors.Is(err, ErrBadEntity) {
		t.Fatalf("missing required error = %v", err)
	}
	for name, bad := range map[string]map[string]string{
		"bad-number": {"display_name": "x", "rarity": "S", "level": "max"},
		"bad-date":   {"display_name": "x", "rarity": "S", "released": "2026-13-40"},
		"bad-select": {"display_name": "x", "rarity": "Z"},
		"bad-image":  {"display_name": "x", "rarity": "S", "portrait": "javascript:alert(1)"},
		"unknown":    {"display_name": "x", "rarity": "S", "nope": "x"},
	} {
		if _, err := data.SaveEntity(ctx, EntityInput{TypeID: entityType.ID, Name: name, Data: bad, ActorID: admin.ID}); !errors.Is(err, ErrBadEntity) {
			t.Fatalf("%s error = %v", name, err)
		}
	}

	valid["level"] = "90"
	updated, err := data.SaveEntity(ctx, EntityInput{ID: entity.ID, TypeID: entityType.ID, Name: "kiana", Label: "琪亚娜", Data: valid, ActorID: admin.ID})
	if err != nil || updated.Data["level"] != "90" {
		t.Fatalf("updated = %+v, %v", updated, err)
	}
	byName, err := data.GetEntityByName(ctx, "kiana")
	if err != nil || byName.ID != entity.ID {
		t.Fatalf("by name = %+v, %v", byName, err)
	}
	loaded, err := data.LoadEntities(ctx)
	if err != nil || len(loaded) != 1 {
		t.Fatalf("loaded = %+v, %v", loaded, err)
	}
	if len(loaded[0].Fields) != 5 || loaded[0].Fields[0].Name != "display_name" {
		t.Fatalf("loaded fields = %+v", loaded[0].Fields)
	}

	var levelFieldID int64
	for _, field := range types[0].Fields {
		if field.Name == "level" {
			levelFieldID = field.ID
		}
	}
	if levelFieldID == 0 {
		t.Fatal("level field missing")
	}
	if err := data.DeleteEntityField(ctx, levelFieldID, admin.ID); err != nil {
		t.Fatal(err)
	}
	afterFieldDelete, err := data.GetEntity(ctx, entity.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := afterFieldDelete.Data["level"]; exists {
		t.Fatalf("deleted field value still present: %+v", afterFieldDelete.Data)
	}
	otherType, err := data.CreateEntityType(ctx, "weapon", "武器", admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := data.SaveEntity(ctx, EntityInput{ID: entity.ID, TypeID: otherType.ID, Name: "kiana", Data: map[string]string{}}); !errors.Is(err, ErrBadEntity) {
		t.Fatalf("cross-type update error = %v", err)
	}
	if err := data.DeleteEntityType(ctx, otherType.ID, admin.ID); err != nil {
		t.Fatal(err)
	}

	if err := data.DeleteEntityType(ctx, entityType.ID, admin.ID); !errors.Is(err, ErrEntityTypeNotEmpty) {
		t.Fatalf("delete non-empty type error = %v", err)
	}
	if err := data.DeleteEntity(ctx, entity.ID, admin.ID); err != nil {
		t.Fatal(err)
	}
	if err := data.DeleteEntityType(ctx, entityType.ID, admin.ID); err != nil {
		t.Fatal(err)
	}
	for action, target := range map[string]string{
		"entity_type.create":  "character",
		"entity_field.create": "character.display_name",
		"entity_field.delete": "character.level",
		"entity.create":       "kiana",
		"entity.update":       "kiana",
		"entity.delete":       "kiana",
		"entity_type.delete":  "character",
	} {
		if ok, err := data.HasAudit(ctx, action, target); err != nil || !ok {
			t.Fatalf("%s audit = %v, %v", action, ok, err)
		}
	}
}
