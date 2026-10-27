package store

import (
	"context"
	"errors"
	"strconv"
	"testing"
)

func TestInteractiveNodes(t *testing.T) {
	data := openTestStore(t)
	ctx := context.Background()
	admin, err := data.CreateUser(ctx, "interactive-admin", "", "hash", true)
	if err != nil {
		t.Fatal(err)
	}
	node, err := data.SaveInteractiveNode(ctx, InteractiveInput{
		NodeType: "widget", Key: "daily-card", Title: "Daily Card",
		Description: "hello", Config: map[string]string{"kind": "card", "line": "text"},
		Enabled: true, ActorID: admin.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if node.ID == 0 || node.Config["line"] != "text" {
		t.Fatalf("node = %+v", node)
	}
	got, err := data.GetInteractiveNode(ctx, "widget", "daily-card")
	if err != nil || got.Title != "Daily Card" {
		t.Fatalf("get node = %+v, %v", got, err)
	}
	if _, err := data.SaveInteractiveNode(ctx, InteractiveInput{
		NodeType: "widget", Key: "daily-card", Title: "Duplicate", Enabled: true, ActorID: admin.ID,
	}); !errors.Is(err, ErrInteractiveExists) {
		t.Fatalf("duplicate = %v", err)
	}
	updated, err := data.SaveInteractiveNode(ctx, InteractiveInput{
		ID: node.ID, NodeType: "game", Key: "daily-card", Title: "Daily Game",
		Config:  map[string]string{"kind": "quiz", "question": "2+2", "options": "3|4", "answer": "4"},
		Enabled: true, ActorID: admin.ID,
	})
	if err != nil || updated.NodeType != "game" || updated.Title != "Daily Game" {
		t.Fatalf("updated = %+v, %v", updated, err)
	}
	if _, err := data.GetInteractiveNode(ctx, "widget", "daily-card"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old type still exists: %v", err)
	}
	nodes, total, err := data.ListInteractiveNodes(ctx, 50, 0)
	if err != nil || len(nodes) != 1 || total != 1 {
		t.Fatalf("list = %d, total %d, %v", len(nodes), total, err)
	}
	emptyPage, _, err := data.ListInteractiveNodes(ctx, 50, 100)
	if err != nil || len(emptyPage) != 0 {
		t.Fatalf("out of range page = %d, %v", len(emptyPage), err)
	}
	if err := data.DeleteInteractiveNode(ctx, node.ID, admin.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := data.GetInteractiveNode(ctx, "game", "daily-card"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted node still visible: %v", err)
	}
	revoked, err := data.ListRevokedRecords(ctx, 20)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range revoked {
		if item.Kind == "interactive" {
			found = true
		}
	}
	if !found {
		t.Fatal("interactive node not listed as revoked")
	}
	if err := data.RestoreRevokedRecord(ctx, "interactive", strconv.FormatInt(node.ID, 10), admin.ID); err != nil {
		t.Fatal(err)
	}
	restored, err := data.GetInteractiveNodeAny(ctx, node.ID)
	if err != nil || restored.Enabled {
		t.Fatalf("restored = %+v, %v", restored, err)
	}
	if _, err := data.SaveInteractiveNode(ctx, InteractiveInput{
		NodeType: "widget", Key: "bad key", Title: "Bad", Enabled: true, ActorID: admin.ID,
	}); !errors.Is(err, ErrBadInteractive) {
		t.Fatalf("bad key = %v", err)
	}
}
