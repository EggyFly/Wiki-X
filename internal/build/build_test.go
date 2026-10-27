package build

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EggyFly/Wiki-X/internal/config"
	"github.com/EggyFly/Wiki-X/internal/site"
)

func TestRunWritesSiteAndCleansStalePages(t *testing.T) {
	contentDir := t.TempDir()
	writeTestFile(t, contentDir, "index.md", "---\ntitle: Home\n---\n\nHello\n")
	writeTestFile(t, contentDir, "old.md", "---\ntitle: Old\n---\n\nOld page\n")
	writeTestFile(t, contentDir, "images/pic.png", "png")

	outDir := t.TempDir()
	loaded := loadSite(t, contentDir)
	if err := Run(context.Background(), loaded, outDir); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(outDir, "index.html"))
	assertFile(t, filepath.Join(outDir, "old", "index.html"))
	assertFile(t, filepath.Join(outDir, "404.html"))
	styles, err := filepath.Glob(filepath.Join(outDir, "assets", "default", "style~*.css"))
	if err != nil || len(styles) != 1 {
		t.Fatalf("hashed styles = %v, %v", styles, err)
	}
	favicons, err := filepath.Glob(filepath.Join(outDir, "assets", "default", "favicon~*.svg"))
	if err != nil || len(favicons) != 1 {
		t.Fatalf("hashed favicons = %v, %v", favicons, err)
	}
	indexHTML, err := os.ReadFile(filepath.Join(outDir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(indexHTML), filepath.Base(styles[0])) {
		t.Fatalf("index does not reference hashed style: %s", indexHTML)
	}
	assertFile(t, filepath.Join(outDir, "media", "images", "pic.png"))
	assertFile(t, filepath.Join(outDir, manifestName))

	if err := os.Remove(filepath.Join(contentDir, "old.md")); err != nil {
		t.Fatal(err)
	}
	loaded = loadSite(t, contentDir)
	if err := Run(context.Background(), loaded, outDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "old", "index.html")); !os.IsNotExist(err) {
		t.Fatalf("stale page still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "old")); !os.IsNotExist(err) {
		t.Fatalf("empty stale directory still exists: %v", err)
	}
}

func TestPageFileRejectsUnsafeRoutes(t *testing.T) {
	for _, route := range []string{"", "index.html", "/../x/", "/a//b/"} {
		if _, err := pageFile(route); err == nil {
			t.Fatalf("route %q should fail", route)
		}
	}
	if got, err := pageFile("/guide/"); err != nil || got != path.Join("guide", "index.html") {
		t.Fatalf("page file = %q, err = %v", got, err)
	}
}

func TestSafeJoinRejectsEscape(t *testing.T) {
	root := t.TempDir()
	if _, err := safeJoin(root, "../escape.txt"); err == nil {
		t.Fatal("expected escape error")
	}
	if _, err := safeJoin(root, filepath.Join(root, "abs.txt")); err == nil {
		t.Fatal("expected absolute path error")
	}
}

func loadSite(t *testing.T, contentDir string) *site.Site {
	t.Helper()
	cfg := &config.Config{
		Site: config.SiteConfig{Title: "Test", Language: "zh-CN"},
		Content: config.ContentConfig{
			Source:      "git",
			Dir:         contentDir,
			MaxFileSize: 1 << 20,
		},
	}
	loaded, err := site.Load(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loaded.Close() })
	return loaded
}

func writeTestFile(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertFile(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.IsDir() {
		t.Fatalf("%s is a directory", path)
	}
}
