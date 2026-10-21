package gitsource

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EggyFly/Wiki-X/internal/content"
)

func TestLoadPages(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "index.md", "---\ntitle: 首页\ndescription: start\nupdated: 2026-10-20\ntags: [demo]\n---\n\nHello\n")
	writeFile(t, root, "guide/index.md", "---\ntitle: 指南\n---\n\nGuide\n")
	writeFile(t, root, "guide/start.md", "---\ntitle: 开始\nslug: custom/start\n---\n\nStart\n")
	writeFile(t, root, "draft.md", "---\ntitle: 草稿\ndraft: true\n---\n\nDraft\n")
	writeFile(t, root, "about-us.md", "---\ndescription: no title\n---\n\nAbout\n")
	writeFile(t, root, "ignore.txt", "noise\n")

	pages, err := New(root).Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 5 {
		t.Fatalf("pages = %d", len(pages))
	}

	routes := make([]string, 0, len(pages))
	byRoute := make(map[string]string)
	for _, page := range pages {
		routes = append(routes, page.Route)
		byRoute[page.Route] = page.Title
	}
	wantRoutes := []string{"/", "/about-us/", "/custom/start/", "/draft/", "/guide/"}
	for i, want := range wantRoutes {
		if routes[i] != want {
			t.Fatalf("routes[%d] = %q, want %q", i, routes[i], want)
		}
	}
	if byRoute["/about-us/"] != "about us" {
		t.Fatalf("fallback title = %q", byRoute["/about-us/"])
	}

	first := pages[0]
	if first.Description != "start" || first.Updated != "2026-10-20" || len(first.Tags) != 1 {
		t.Fatalf("unexpected metadata: %+v", first)
	}
	if first.Version == "" {
		t.Fatal("page version is empty")
	}
	if !pages[3].Draft {
		t.Fatal("draft flag not loaded")
	}
}

func TestVersionChangesWithContent(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "index.md", "---\ntitle: Home\n---\n\nalpha\n")

	pages, err := New(root).Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	first := pages[0].Version

	writeFile(t, root, "index.md", "---\ntitle: Home\n---\n\nbeta\n")
	pages, err = New(root).Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first == pages[0].Version {
		t.Fatal("version should change with content")
	}
}

func TestLoadRejectsOversizedFile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "index.md", "---\ntitle: Home\n---\n\n"+strings.Repeat("x", 100)+"\n")

	source := New(root)
	source.SetMaxFileSize(20)
	_, err := source.Load(context.Background())
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadRejectsYAMLAlias(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "index.md", "---\ntitle: &name Home\ndescription: *name\n---\n\nBody\n")
	_, err := New(root).Load(context.Background())
	if err == nil || !strings.Contains(err.Error(), "alias") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadRejectsUnclosedFrontMatter(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "index.md", "---\ntitle: Broken\n")
	_, err := New(root).Load(context.Background())
	if err == nil || !strings.Contains(err.Error(), "not closed") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadRejectsDuplicateRoute(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "one.md", "---\ntitle: One\nslug: same\n---\n\nOne\n")
	writeFile(t, root, "two.md", "---\ntitle: Two\nslug: same\n---\n\nTwo\n")
	_, err := New(root).Load(context.Background())
	if err == nil || !strings.Contains(err.Error(), "route /same/") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadRejectsUnsafeSlug(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "index.md", "---\ntitle: Bad\nslug: ../secret\n---\n\nBad\n")
	_, err := New(root).Load(context.Background())
	if err == nil {
		t.Fatal("expected unsafe slug error")
	}
}

func TestLoadRejectsReservedWindowsName(t *testing.T) {
	for _, slug := range []string{"con", "con.txt", "COM1"} {
		root := t.TempDir()
		writeFile(t, root, "index.md", "---\ntitle: Bad\nslug: "+slug+"\n---\n\nBad\n")
		_, err := New(root).Load(context.Background())
		if err == nil {
			t.Fatalf("slug %q should fail", slug)
		}
	}
}

func TestLoadRejectsCaseInsensitiveDuplicateRoute(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "one.md", "---\ntitle: One\nslug: Guide\n---\n\nOne\n")
	writeFile(t, root, "two.md", "---\ntitle: Two\nslug: guide\n---\n\nTwo\n")
	_, err := New(root).Load(context.Background())
	if err == nil {
		t.Fatal("expected case-insensitive route conflict")
	}
}

func TestFormatAndParseDocument(t *testing.T) {
	page := &content.Page{
		Route:          "/guide/start/",
		Title:          "Start",
		Description:    "guide start",
		Tags:           []string{"a", "b"},
		Body:           "# Hello\n\nbody",
		Updated:        "2026-10-20",
		TranslationKey: "guide-start",
	}
	data, err := FormatDocument(page)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseDocument("whatever.md", data, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Route != page.Route || parsed.Title != page.Title || parsed.Description != page.Description || parsed.TranslationKey != page.TranslationKey {
		t.Fatalf("parsed = %+v", parsed)
	}
	if strings.Join(parsed.Tags, ",") != "a,b" || parsed.Body != page.Body+"\n" {
		t.Fatalf("parsed tags/body = %+v", parsed)
	}
	if FileForRoute("/guide/start/") != "guide/start.md" || FileForRoute("/") != "index.md" {
		t.Fatal("unexpected route file mapping")
	}
}

func TestRouteFromFile(t *testing.T) {
	cases := map[string]string{
		"index.md":                 "/",
		"about.md":                 "/about/",
		"guide/index.md":           "/guide/",
		"guide/getting-started.md": "/guide/getting-started/",
	}
	for file, want := range cases {
		got, err := routeFromFile(file)
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		if got != want {
			t.Fatalf("%s route = %q, want %q", file, got, want)
		}
	}
}

func writeFile(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
