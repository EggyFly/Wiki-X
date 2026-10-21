package store

import (
	"context"
	"testing"

	"github.com/EggyFly/Wiki-X/internal/content"
)

func TestSearchPages(t *testing.T) {
	data := openTestStore(t)
	ctx := context.Background()
	admin, err := data.CreateUser(ctx, "admin", "", "hash", true)
	if err != nil {
		t.Fatal(err)
	}
	pages := []*content.Page{
		{Route: "/kiana/", Title: "琪亚娜", Body: "琪亚娜是一名角色, rarity S", Version: "v1"},
		{Route: "/guide/", Title: "Guide", Body: "getting started with the wiki", Version: "v2"},
		{Route: "/deleted/", Title: "Deleted", Body: "琪亚娜 旧页面", Version: "v3"},
	}
	if _, _, err := data.ImportPages(ctx, pages, &admin.ID); err != nil {
		t.Fatal(err)
	}
	if err := data.DeletePage(ctx, "/deleted/", "test", admin.ID); err != nil {
		t.Fatal(err)
	}

	results, err := data.SearchPages(ctx, "琪亚娜", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Route != "/kiana/" || results[0].Snippet == "" {
		t.Fatalf("chinese results = %+v", results)
	}
	results, err = data.SearchPages(ctx, "getting started", 10)
	if err != nil || len(results) != 1 || results[0].Route != "/guide/" {
		t.Fatalf("english results = %+v, %v", results, err)
	}
	results, err = data.SearchPages(ctx, "缺失内容", 10)
	if err != nil || len(results) != 0 {
		t.Fatalf("missing results = %+v, %v", results, err)
	}
	results, err = data.SearchPages(ctx, "Guide", 10)
	if err != nil || len(results) != 1 || results[0].Title != "Guide" {
		t.Fatalf("title results = %+v, %v", results, err)
	}
}
