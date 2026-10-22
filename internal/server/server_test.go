package server

import (
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	assetutil "github.com/EggyFly/Wiki-X/internal/assets"
	"github.com/EggyFly/Wiki-X/internal/config"
	"github.com/EggyFly/Wiki-X/internal/site"
)

func TestHandler(t *testing.T) {
	s := loadSite(t)
	handler := newHandler(s)

	t.Run("page", func(t *testing.T) {
		res := request(handler, http.MethodGet, "/", nil)
		if res.Code != http.StatusOK {
			t.Fatalf("status = %d", res.Code)
		}
		if !strings.Contains(res.Body.String(), "Test Wiki") {
			t.Fatal("site title missing")
		}
		if res.Header().Get("Content-Security-Policy") == "" {
			t.Fatal("missing csp")
		}
		if res.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatal("missing nosniff")
		}
		if res.Header().Get("ETag") == "" {
			t.Fatal("missing etag")
		}
		if !strings.Contains(res.Header().Get("Cache-Control"), "s-maxage=300") {
			t.Fatalf("cache control = %q", res.Header().Get("Cache-Control"))
		}
	})

	t.Run("etag not modified", func(t *testing.T) {
		first := request(handler, http.MethodGet, "/", nil)
		etag := first.Header().Get("ETag")
		if etag == "" {
			t.Fatal("missing etag")
		}
		res := request(handler, http.MethodGet, "/", map[string]string{"If-None-Match": etag})
		if res.Code != http.StatusNotModified {
			t.Fatalf("status = %d", res.Code)
		}
		if res.Body.Len() != 0 {
			t.Fatalf("body length = %d", res.Body.Len())
		}
	})

	t.Run("gzip", func(t *testing.T) {
		res := request(handler, http.MethodGet, "/", map[string]string{"Accept-Encoding": "gzip"})
		if res.Header().Get("Content-Encoding") != "gzip" {
			t.Fatalf("content encoding = %q", res.Header().Get("Content-Encoding"))
		}
		reader, err := gzip.NewReader(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(body), "Test Wiki") {
			t.Fatal("gzip body missing content")
		}
	})

	t.Run("gzip disabled by q zero", func(t *testing.T) {
		res := request(handler, http.MethodGet, "/", map[string]string{"Accept-Encoding": "gzip;q=0.0"})
		if res.Header().Get("Content-Encoding") != "" {
			t.Fatalf("content encoding = %q", res.Header().Get("Content-Encoding"))
		}
	})

	t.Run("trailing slash", func(t *testing.T) {
		res := request(handler, http.MethodGet, "/guide/", nil)
		if res.Code != http.StatusOK {
			t.Fatalf("status = %d", res.Code)
		}
	})

	t.Run("trailing slash redirect", func(t *testing.T) {
		res := request(handler, http.MethodGet, "/guide?x=1", nil)
		if res.Code != http.StatusMovedPermanently {
			t.Fatalf("status = %d", res.Code)
		}
		if got := res.Header().Get("Location"); got != "/guide/?x=1" {
			t.Fatalf("location = %q", got)
		}
	})

	t.Run("not found", func(t *testing.T) {
		res := request(handler, http.MethodGet, "/missing/", nil)
		if res.Code != http.StatusNotFound {
			t.Fatalf("status = %d", res.Code)
		}
		if !strings.Contains(res.Body.String(), "页面不存在") {
			t.Fatal("404 body missing")
		}
	})

	t.Run("method not allowed", func(t *testing.T) {
		res := request(handler, http.MethodPost, "/", nil)
		if res.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d", res.Code)
		}
	})

	t.Run("asset", func(t *testing.T) {
		style := assetutil.FindHashed(s.Assets(), "style", ".css")
		res := request(handler, http.MethodGet, "/assets/"+style, nil)
		if res.Code != http.StatusOK {
			t.Fatalf("status = %d", res.Code)
		}
		if got := res.Header().Get("Cache-Control"); !strings.Contains(got, "immutable") {
			t.Fatalf("cache control = %q", got)
		}
		if !strings.Contains(res.Header().Get("Content-Type"), "text/css") {
			t.Fatalf("content type = %q", res.Header().Get("Content-Type"))
		}
	})

	t.Run("versioned asset", func(t *testing.T) {
		style := assetutil.FindHashed(s.Assets(), "style", ".css")
		res := request(handler, http.MethodGet, "/assets/"+style+"?v=abc", nil)
		if res.Code != http.StatusOK {
			t.Fatalf("status = %d", res.Code)
		}
		if got := res.Header().Get("Cache-Control"); !strings.Contains(got, "immutable") {
			t.Fatalf("cache control = %q", got)
		}
	})

	t.Run("healthz", func(t *testing.T) {
		res := request(handler, http.MethodGet, "/-/healthz", nil)
		if res.Code != http.StatusOK {
			t.Fatalf("status = %d", res.Code)
		}
		if res.Body.String() != "ok\n" {
			t.Fatalf("body = %q", res.Body.String())
		}
		if res.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("cache control = %q", res.Header().Get("Cache-Control"))
		}
	})

	t.Run("skin asset", func(t *testing.T) {
		style := assetutil.FindHashed(s.AssetsFor("compact"), "style", ".css")
		res := request(handler, http.MethodGet, "/assets/compact/"+style, nil)
		if res.Code != http.StatusOK {
			t.Fatalf("status = %d", res.Code)
		}
		if !strings.Contains(res.Header().Get("Content-Type"), "text/css") {
			t.Fatalf("content type = %q", res.Header().Get("Content-Type"))
		}
	})

	t.Run("skin switch", func(t *testing.T) {
		res := request(handler, http.MethodGet, "/-/skin?name=compact&next=/", nil)
		if res.Code != http.StatusSeeOther {
			t.Fatalf("status = %d", res.Code)
		}
		if res.Header().Get("Location") != "/" {
			t.Fatalf("location = %q", res.Header().Get("Location"))
		}
		found := false
		for _, cookie := range res.Result().Cookies() {
			if cookie.Name == "wikix_skin" && cookie.Value == "compact" {
				found = true
			}
		}
		if !found {
			t.Fatal("skin cookie not set")
		}
	})

	t.Run("skin cookie changes assets", func(t *testing.T) {
		res := request(handler, http.MethodGet, "/", map[string]string{"Cookie": "wikix_skin=compact"})
		if res.Code != http.StatusOK {
			t.Fatalf("status = %d", res.Code)
		}
		if !strings.Contains(res.Body.String(), "/assets/compact/style~") {
			t.Fatal("compact assets not used")
		}
		if !strings.Contains(res.Header().Get("Vary"), "Cookie") {
			t.Fatalf("vary = %q", res.Header().Get("Vary"))
		}
	})

	t.Run("directory listing disabled", func(t *testing.T) {
		res := request(handler, http.MethodGet, "/assets/", nil)
		if res.Code != http.StatusNotFound {
			t.Fatalf("status = %d", res.Code)
		}
	})
}

func TestGitMedia(t *testing.T) {
	contentDir := t.TempDir()
	writeFile(t, contentDir, "index.md", "---\ntitle: Home\n---\n\nHello\n")
	writeFile(t, contentDir, "images/pic.png", "not-a-real-png")
	cfg := &config.Config{
		Site:    config.SiteConfig{Title: "Media Wiki", Language: "zh-CN"},
		Content: config.ContentConfig{Source: "git", Dir: contentDir, MaxFileSize: 1 << 20},
	}
	loaded, err := site.Load(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loaded.Close() })
	handler := newHandler(loaded)

	res := request(handler, http.MethodGet, "/media/images/pic.png", nil)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d", res.Code)
	}
	if !strings.Contains(res.Header().Get("Content-Type"), "image/png") {
		t.Fatalf("content type = %q", res.Header().Get("Content-Type"))
	}
	res = request(handler, http.MethodGet, "/media/index.md", nil)
	if res.Code != http.StatusNotFound {
		t.Fatalf("markdown media status = %d", res.Code)
	}
}

func request(handler http.Handler, method, target string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func loadSite(t *testing.T) *site.Site {
	t.Helper()
	contentDir := t.TempDir()
	writeFile(t, contentDir, "index.md", "---\ntitle: Home\n---\n\n"+strings.Repeat("内容", 300)+"\n")
	writeFile(t, contentDir, "guide/index.md", "---\ntitle: Guide\n---\n\nGuide\n")

	cfg := &config.Config{
		Site: config.SiteConfig{Title: "Test Wiki", Language: "zh-CN"},
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
