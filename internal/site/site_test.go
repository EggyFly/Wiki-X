package site

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	assetutil "github.com/EggyFly/Wiki-X/internal/assets"
	"github.com/EggyFly/Wiki-X/internal/config"
)

func TestLoadWithBuiltinTheme(t *testing.T) {
	contentDir := t.TempDir()
	writeFile(t, contentDir, "index.md", "---\ntitle: 首页\n---\n\n# 欢迎\n")
	writeFile(t, contentDir, "draft.md", "---\ntitle: 草稿\ndraft: true\n---\n\nDraft\n")

	loaded, err := Load(context.Background(), testConfig(contentDir))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loaded.Close() })

	routes := loaded.Routes()
	if len(routes) != 1 || routes[0] != "/" {
		t.Fatalf("routes = %v", routes)
	}
	page, err := loaded.Render(context.Background(), "/")
	if err != nil || !strings.Contains(string(page), "首页") {
		t.Fatalf("render failed: %v, %s", err, page)
	}
	if _, err := loaded.Render(context.Background(), "/draft/"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("draft should not be published: %v", err)
	}
	if !strings.Contains(string(loaded.NotFound()), "页面不存在") {
		t.Fatal("404 page missing")
	}
	css, err := fs.ReadFile(loaded.Assets(), assetutil.FindHashed(loaded.Assets(), "style", ".css"))
	if err != nil || len(css) == 0 {
		t.Fatalf("assets: %v", err)
	}
	if loaded.CacheDriver() != "memory" {
		t.Fatalf("cache driver = %q", loaded.CacheDriver())
	}
}

func TestLoadWithCustomTheme(t *testing.T) {
	contentDir := t.TempDir()
	themeDir := t.TempDir()
	writeFile(t, contentDir, "index.md", "---\ntitle: Home\n---\n\nBody\n")
	writeFile(t, themeDir, "layout.html", "<!doctype html><body>{{template \"main\" .}}</body>")
	writeFile(t, themeDir, "page.html", "{{define \"main\"}}CUSTOM {{.Page.Title}}{{end}}")
	writeFile(t, themeDir, "404.html", "{{define \"main\"}}NOPE{{end}}")
	writeFile(t, themeDir, "assets/style.css", "body{}")

	cfg := testConfig(contentDir)
	cfg.Theme.Dir = themeDir
	loaded, err := Load(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loaded.Close() })

	page, err := loaded.Render(context.Background(), "/")
	if err != nil || !strings.Contains(string(page), "CUSTOM Home") {
		t.Fatalf("custom theme not used: %v, %s", err, page)
	}
}

func TestReloadRefreshesStateAndCache(t *testing.T) {
	contentDir := t.TempDir()
	writeFile(t, contentDir, "index.md", "---\ntitle: Reload\n---\n\nalpha\n")

	loaded, err := Load(context.Background(), testConfig(contentDir))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loaded.Close() })

	first, err := loaded.Render(context.Background(), "/")
	if err != nil || !strings.Contains(string(first), "alpha") {
		t.Fatalf("first render: %v, %s", err, first)
	}

	writeFile(t, contentDir, "index.md", "---\ntitle: Reload\n---\n\nbeta\n")
	cached, err := loaded.Render(context.Background(), "/")
	if err != nil || !strings.Contains(string(cached), "alpha") {
		t.Fatalf("expected old cached page before reload: %v, %s", err, cached)
	}

	if err := loaded.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	refreshed, err := loaded.Render(context.Background(), "/")
	if err != nil || !strings.Contains(string(refreshed), "beta") {
		t.Fatalf("expected refreshed page: %v, %s", err, refreshed)
	}
}

func TestWarmAndInvalidateRoute(t *testing.T) {
	contentDir := t.TempDir()
	writeFile(t, contentDir, "index.md", "---\ntitle: 首页\n---\n\nhome\n")
	writeFile(t, contentDir, "guide.md", "---\ntitle: 指南\n---\n\nguide\n")
	loaded, err := Load(context.Background(), testConfig(contentDir))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loaded.Close() })

	if _, err := loaded.Render(context.Background(), "/"); err != nil {
		t.Fatal(err)
	}
	keys, err := loaded.InvalidateRoute(context.Background(), "/")
	if err != nil || keys < 1 {
		t.Fatalf("invalidate = %d, %v", keys, err)
	}
	count, err := loaded.Warm(context.Background(), "", 2)
	if err != nil || count != 2 {
		t.Fatalf("warm = %d, %v", count, err)
	}
	if _, err := loaded.Warm(context.Background(), "missing", 1); err == nil {
		t.Fatal("unknown skin should fail")
	}
}

func TestLoadRejectsPostgresUntilImplemented(t *testing.T) {
	cfg := testConfig(t.TempDir())
	cfg.Content.Source = "postgres"
	if _, err := Load(context.Background(), cfg); err == nil {
		t.Fatal("expected postgres error")
	}
}

func TestCheckRoute(t *testing.T) {
	for _, route := range []string{"/assets/", "/Assets/", "/404.html/", "/index.html/", "/.wiki_x-build.json/", "/bad&route/", "/con/", "/guide/trailing /"} {
		if err := ValidateRoute(route); err == nil {
			t.Fatalf("route %q should be rejected", route)
		}
	}
	for _, route := range []string{"/", "/guide/", "/guide/start/", "/guide/v1.2/"} {
		if err := ValidateRoute(route); err != nil {
			t.Fatalf("route %q: %v", route, err)
		}
	}
}

func TestSkins(t *testing.T) {
	contentDir := t.TempDir()
	writeFile(t, contentDir, "index.md", "---\ntitle: Home\n---\n\nBody\n")
	loaded, err := Load(context.Background(), testConfig(contentDir))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loaded.Close() })

	skins := loaded.Skins()
	if len(skins) < 2 || !loaded.HasSkin("compact") {
		t.Fatalf("skins = %+v", skins)
	}
	if loaded.Info().CurrentSkin != "default" {
		t.Fatalf("current skin = %q", loaded.Info().CurrentSkin)
	}
	compact, err := loaded.RenderSkin(context.Background(), "/", "compact")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(compact), "/assets/compact/style~") {
		t.Fatal("compact skin assets missing")
	}
	fallback, err := loaded.RenderSkin(context.Background(), "/", "missing")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(fallback), "/assets/default/style~") {
		t.Fatal("skin fallback missing")
	}
}

func TestContentLocales(t *testing.T) {
	contentDir := t.TempDir()
	writeFile(t, contentDir, "index.md", "---\ntitle: 首页\n---\n\n中文内容\n")
	writeFile(t, contentDir, "en/index.md", "---\ntitle: Home\n---\n\nEnglish content\n")
	cfg := testConfig(contentDir)
	cfg.Site.Language = "zh-CN"
	cfg.Site.Locales = []config.LocaleConfig{
		{Code: "zh-CN", Label: "中文", Prefix: ""},
		{Code: "en", Label: "English", Prefix: "en"},
	}
	loaded, err := Load(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loaded.Close() })

	english, err := loaded.Render(context.Background(), "/en/")
	if err != nil {
		t.Fatal(err)
	}
	html := string(english)
	for _, want := range []string{"Updated", "Language", "English content", "href=\"/\""} {
		if !strings.Contains(html, want) {
			t.Fatalf("missing %q", want)
		}
	}
	if loaded.LocaleCodeForRoute("/en/") != "en" || loaded.LocaleCodeForRoute("/") != "zh-CN" {
		t.Fatal("locale detection failed")
	}
	chinese, err := loaded.Render(context.Background(), "/")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(chinese), "href=\"/en/\"") || !strings.Contains(string(chinese), "更新于") {
		t.Fatal("chinese locale links missing")
	}
}

func TestEnglishUI(t *testing.T) {
	contentDir := t.TempDir()
	writeFile(t, contentDir, "index.md", "---\ntitle: Home\n---\n\nBody\n")
	cfg := testConfig(contentDir)
	cfg.Site.Language = "en"
	cfg.Site.CommunityUI = true
	loaded, err := Load(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loaded.Close() })

	page, err := loaded.Render(context.Background(), "/")
	if err != nil {
		t.Fatal(err)
	}
	html := string(page)
	for _, want := range []string{"Updated", "Search", "Edit"} {
		if !strings.Contains(html, want) {
			t.Fatalf("missing %q", want)
		}
	}
	if !strings.Contains(string(loaded.NotFound()), "Page not found") {
		t.Fatal("english 404 missing")
	}
	skins := loaded.Skins()
	if len(skins) == 0 || skins[0].Label != "Default" {
		t.Fatalf("skins = %+v", skins)
	}
}

func TestTranslationFallbackAndGroup(t *testing.T) {
	contentDir := t.TempDir()
	writeFile(t, contentDir, "index.md", "---\ntitle: 首页\n---\n\n首页\n")
	writeFile(t, contentDir, "about.md", "---\ntitle: 关于\n---\n\n关于正文\n")
	writeFile(t, contentDir, "guide.md", "---\ntitle: 指南\ntranslation_key: guide-start\n---\n\n指南正文\n")
	writeFile(t, contentDir, "en/start.md", "---\ntitle: Guide\ntranslation_key: guide-start\n---\n\nEnglish guide\n")

	cfg := testConfig(contentDir)
	cfg.Site.Language = "zh-CN"
	cfg.Site.CommunityUI = true
	cfg.Site.Locales = []config.LocaleConfig{
		{Code: "zh-CN", Label: "中文", Prefix: ""},
		{Code: "en", Label: "English", Prefix: "en"},
	}
	loaded, err := Load(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loaded.Close() })

	defaultPage, err := loaded.Render(context.Background(), "/guide/")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(defaultPage), `href="/en/start/"`) {
		t.Fatal("translation group link missing")
	}
	if !strings.Contains(string(defaultPage), "page-actions") || !strings.Contains(string(defaultPage), "%2fguide%2f") {
		t.Fatal("community page actions missing")
	}
	if strings.Contains(string(defaultPage), "history?route=") {
		t.Fatal("git mode should not link to postgres history")
	}

	fallback, err := loaded.Render(context.Background(), "/en/about/")
	if err != nil {
		t.Fatal(err)
	}
	html := string(fallback)
	for _, want := range []string{"This page has not been translated yet", "关于正文", "%2fabout%2f"} {
		if !strings.Contains(html, want) {
			t.Fatalf("fallback missing %q", want)
		}
	}

	if _, err := loaded.Render(context.Background(), "/en/missing/"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestStaticSiteHidesCommunityEntrypoints(t *testing.T) {
	contentDir := t.TempDir()
	writeFile(t, contentDir, "index.md", "---\ntitle: Home\n---\n\nBody\n")
	cfg := testConfig(contentDir)
	cfg.Site.CommunityUI = true
	cfg.Site.Static = true
	cfg.Theme.DisableSwitch = true
	loaded, err := Load(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loaded.Close() })
	page, err := loaded.Render(context.Background(), "/")
	if err != nil {
		t.Fatal(err)
	}
	html := string(page)
	for _, unwanted := range []string{`href="/edit`, `href="/search"`, "/-/skin"} {
		if strings.Contains(html, unwanted) {
			t.Fatalf("static page contains %q", unwanted)
		}
	}
}

func TestSearch(t *testing.T) {
	contentDir := t.TempDir()
	writeFile(t, contentDir, "index.md", "---\ntitle: 首页\n---\n\nalpha beta\n")
	writeFile(t, contentDir, "guide.md", "---\ntitle: 指南\n---\n\ngamma\n")
	loaded, err := Load(context.Background(), testConfig(contentDir))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loaded.Close() })

	results := loaded.Search("alpha", 10)
	if len(results) != 1 || results[0].Route != "/" || results[0].Title != "首页" {
		t.Fatalf("results = %+v", results)
	}
	if got := loaded.Search("", 10); len(got) != 0 {
		t.Fatalf("empty query results = %+v", got)
	}
	if err := ValidateRoute("/bad&route/"); err == nil {
		t.Fatal("unsafe route accepted")
	}
}

func testConfig(contentDir string) *config.Config {
	return &config.Config{
		Site: config.SiteConfig{
			Title:    "Test Wiki",
			Language: "zh-CN",
		},
		Content: config.ContentConfig{
			Source:      "git",
			Dir:         contentDir,
			MaxFileSize: 1 << 20,
		},
		Server: config.ServerConfig{Addr: ":0"},
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
