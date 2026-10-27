package store

import (
	"context"
	"errors"
	"testing"

	"github.com/EggyFly/Wiki-X/internal/content"
)

func TestPageDeleteRestore(t *testing.T) {
	data := openTestStore(t)
	ctx := context.Background()
	admin, err := data.CreateUser(ctx, "admin", "", "hash", true)
	if err != nil {
		t.Fatal(err)
	}
	pages := []*content.Page{{Route: "/guide/", Title: "Guide", Body: "old", Version: "v1"}}
	if _, _, err := data.ImportPages(ctx, pages, &admin.ID); err != nil {
		t.Fatal(err)
	}
	if err := data.DeletePage(ctx, "/guide/", "侵权内容", admin.ID); err != nil {
		t.Fatal(err)
	}
	if err := data.DeletePage(ctx, "/guide/", "again", admin.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete error = %v", err)
	}
	page, err := data.GetPage(ctx, "/guide/")
	if err != nil {
		t.Fatal(err)
	}
	if page.DeletedAt == nil || page.DeleteReason != "侵权内容" || page.DeletedBy == nil {
		t.Fatalf("deleted page = %+v", page)
	}
	loaded, err := data.Load(ctx)
	if err != nil || len(loaded) != 0 {
		t.Fatalf("loaded = %d, %v", len(loaded), err)
	}
	if _, err := data.CreateRevision(ctx, RevisionInput{
		Route: "/guide/", Title: "Guide", Body: "new", AuthorID: admin.ID, BaseVersion: "v1", Publish: true,
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revision on deleted page error = %v", err)
	}
	updated := []*content.Page{{Route: "/guide/", Title: "Guide", Body: "imported", Version: "v2"}}
	if created, changed, err := data.ImportPages(ctx, updated, &admin.ID); err != nil || created != 0 || changed != 0 {
		t.Fatalf("import deleted page = %d, %d, %v", created, changed, err)
	}
	list, total, err := data.ListAdminPages(ctx, "deleted", "guide", 10, 0)
	if err != nil || total != 1 || len(list) != 1 || list[0].DeleteReason != "侵权内容" {
		t.Fatalf("admin pages = %+v, total %d, %v", list, total, err)
	}
	if err := data.RestorePage(ctx, "/guide/", admin.ID); err != nil {
		t.Fatal(err)
	}
	loaded, err = data.Load(ctx)
	if err != nil || len(loaded) != 1 {
		t.Fatalf("loaded after restore = %d, %v", len(loaded), err)
	}
	page, err = data.GetPage(ctx, "/guide/")
	if err != nil || page.DeletedAt != nil || page.DeleteReason != "" {
		t.Fatalf("restored page = %+v, %v", page, err)
	}
	if ok, err := data.HasAudit(ctx, "page.delete", "/guide/"); err != nil || !ok {
		t.Fatalf("delete audit = %v, %v", ok, err)
	}
	if ok, err := data.HasAudit(ctx, "page.restore", "/guide/"); err != nil || !ok {
		t.Fatalf("restore audit = %v, %v", ok, err)
	}
}

func TestPageRename(t *testing.T) {
	data := openTestStore(t)
	ctx := context.Background()
	admin, err := data.CreateUser(ctx, "admin", "", "hash", true)
	if err != nil {
		t.Fatal(err)
	}
	pages := []*content.Page{
		{Route: "/guide/", Title: "Guide", Body: "body", Version: "v1"},
		{Route: "/taken/", Title: "Taken", Body: "body", Version: "v1"},
	}
	if _, _, err := data.ImportPages(ctx, pages, &admin.ID); err != nil {
		t.Fatal(err)
	}
	if err := data.Watch(ctx, admin.ID, "/guide/"); err != nil {
		t.Fatal(err)
	}
	if _, err := data.AddComment(ctx, "/guide/", admin.ID, "hello"); err != nil {
		t.Fatal(err)
	}
	if err := data.RenamePage(ctx, "/guide/", "/manual/", admin.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := data.GetPage(ctx, "/guide/"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old route still exists: %v", err)
	}
	page, err := data.GetPage(ctx, "/manual/")
	if err != nil || page.Title != "Guide" {
		t.Fatalf("renamed page = %+v, %v", page, err)
	}
	comments, err := data.ListComments(ctx, "/manual/", true)
	if err != nil || len(comments) != 1 {
		t.Fatalf("comments = %d, %v", len(comments), err)
	}
	if watched, err := data.IsWatched(ctx, admin.ID, "/manual/"); err != nil || !watched {
		t.Fatalf("watch not moved: %v, %v", watched, err)
	}
	if watched, err := data.IsWatched(ctx, admin.ID, "/guide/"); err != nil || watched {
		t.Fatalf("old watch still exists: %v, %v", watched, err)
	}
	if err := data.RenamePage(ctx, "/manual/", "/Taken/", admin.ID); !errors.Is(err, ErrRouteExists) {
		t.Fatalf("case-insensitive conflict = %v", err)
	}
	if ok, err := data.HasAudit(ctx, "page.rename", "/guide/"); err != nil || !ok {
		t.Fatalf("rename audit = %v, %v", ok, err)
	}
}
