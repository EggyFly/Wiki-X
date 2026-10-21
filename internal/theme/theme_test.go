package theme

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadVersionAndAssets(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "layout.html", "<!doctype html><body>{{template \"main\" .}}{{.Site.AssetVersion}}</body>")
	writeFile(t, dir, "page.html", "{{define \"main\"}}page{{end}}")
	writeFile(t, dir, "404.html", "{{define \"main\"}}404{{end}}")
	writeFile(t, dir, "assets/style.css", "body{}")

	themePack, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	first := themePack.Version()
	if len(first) != 64 {
		t.Fatalf("version = %q", first)
	}
	css, err := fs.ReadFile(themePack.Assets(), "style.css")
	if err != nil || string(css) != "body{}" {
		t.Fatalf("assets: %q, %v", css, err)
	}

	writeFile(t, dir, "assets/style.css", "body{color:red}")
	themePack, err = LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if themePack.Version() == first {
		t.Fatal("theme version should change")
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
