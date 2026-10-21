package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/EggyFly/Wiki-X/internal/config"
	"github.com/EggyFly/Wiki-X/internal/search"
)

func TestMeilisearchBackend(t *testing.T) {
	var mu sync.Mutex
	var indexed []search.Document
	var deleted []string
	failSearch := false
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/search"):
			mu.Lock()
			fail := failSearch
			mu.Unlock()
			if fail {
				http.Error(w, "boom", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"hits": []map[string]any{{
					"route":      "/guide/",
					"title":      "Meili Hit",
					"_formatted": map[string]string{"body": "snippet"},
				}},
			})
		case strings.HasSuffix(r.URL.Path, "/documents"):
			var documents []search.Document
			_ = json.NewDecoder(r.Body).Decode(&documents)
			mu.Lock()
			indexed = append(indexed, documents...)
			mu.Unlock()
			w.WriteHeader(http.StatusAccepted)
		case strings.HasSuffix(r.URL.Path, "/documents/delete-batch"):
			var ids []string
			_ = json.NewDecoder(r.Body).Decode(&ids)
			mu.Lock()
			deleted = append(deleted, ids...)
			mu.Unlock()
			w.WriteHeader(http.StatusAccepted)
		default:
			http.NotFound(w, r)
		}
	}))
	defer fake.Close()

	app, _, _ := setupCommunityFixture(t, config.GitHubConfig{})
	app.cfg.Search = config.SearchConfig{Backend: "meilisearch", URL: fake.URL, APIKey: "key", Index: "wikix"}
	app.searchClient = search.NewClient(fake.URL, "key", "wikix")
	server := httptest.NewServer(app.Handler())
	t.Cleanup(server.Close)

	resp := mustGet(t, testHTTPClient(), server.URL+"/search?q=guide")
	body := readResponse(t, resp)
	if !strings.Contains(body, "Meili Hit") {
		t.Fatalf("meili hit missing:\n%s", body)
	}

	admin := loginTestClient(t, server.URL, "admin", "admin-pass-123")
	page, err := app.store.GetPage(context.Background(), "/guide/")
	if err != nil {
		t.Fatal(err)
	}
	csrf := fetchCSRFToken(t, admin, server.URL+"/edit?route=/guide/")
	save := postForm(t, admin, server.URL+"/edit/save", url.Values{
		"csrf_token":   {csrf},
		"route":        {"/guide/"},
		"title":        {"Guide"},
		"body":         {"indexed body"},
		"base_version": {page.Version},
	})
	if save.StatusCode != http.StatusSeeOther {
		t.Fatalf("save status = %d", save.StatusCode)
	}
	save.Body.Close()
	mu.Lock()
	if len(indexed) == 0 || indexed[len(indexed)-1].Route != "/guide/" {
		t.Fatalf("indexed = %+v", indexed)
	}
	mu.Unlock()

	csrf = fetchCSRFToken(t, admin, server.URL+"/admin/pages")
	del := postForm(t, admin, server.URL+"/admin/pages/delete", url.Values{
		"csrf_token": {csrf},
		"route":      {"/guide/"},
		"reason":     {"test"},
	})
	if del.StatusCode != http.StatusSeeOther {
		t.Fatalf("delete status = %d", del.StatusCode)
	}
	del.Body.Close()
	mu.Lock()
	if len(deleted) == 0 || deleted[len(deleted)-1] != search.DocumentID("/guide/") {
		t.Fatalf("deleted = %+v", deleted)
	}
	mu.Unlock()

	mu.Lock()
	failSearch = true
	mu.Unlock()
	resp = mustGet(t, testHTTPClient(), server.URL+"/search?q=welcome")
	body = readResponse(t, resp)
	if !strings.Contains(body, "welcome home") {
		t.Fatalf("fallback search missing:\n%s", body)
	}
}
