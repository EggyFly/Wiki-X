package search

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestMeilisearchClient(t *testing.T) {
	var mu sync.Mutex
	var indexed []Document
	var deleted []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/indexes/wiki_x/search"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"hits": []map[string]any{{
					"route":       "/guide/",
					"title":       "Guide",
					"description": "desc",
					"_formatted":  map[string]string{"body": "matched <em>body</em>"},
				}},
			})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/documents"):
			var documents []Document
			if err := json.NewDecoder(r.Body).Decode(&documents); err != nil {
				http.Error(w, "bad body", http.StatusBadRequest)
				return
			}
			mu.Lock()
			indexed = append(indexed, documents...)
			mu.Unlock()
			w.WriteHeader(http.StatusAccepted)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/documents/delete-batch"):
			var ids []string
			if err := json.NewDecoder(r.Body).Decode(&ids); err != nil {
				http.Error(w, "bad body", http.StatusBadRequest)
				return
			}
			mu.Lock()
			deleted = append(deleted, ids...)
			mu.Unlock()
			w.WriteHeader(http.StatusAccepted)
		case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/documents"):
			w.WriteHeader(http.StatusAccepted)
		default:
			http.Error(w, "unexpected", http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key", "wiki_x")
	hits, err := client.Search(context.Background(), "guide", 10)
	if err != nil || len(hits) != 1 || hits[0].Route != "/guide/" || !strings.Contains(hits[0].Snippet, "body") {
		t.Fatalf("hits = %+v, %v", hits, err)
	}
	doc := Document{ID: DocumentID("/guide/"), Route: "/guide/", Title: "Guide", Body: "body"}
	if err := client.Index(context.Background(), []Document{doc}); err != nil {
		t.Fatal(err)
	}
	if err := client.Delete(context.Background(), []string{"/guide/"}); err != nil {
		t.Fatal(err)
	}
	if err := client.Clear(context.Background()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(indexed) != 1 || indexed[0].Route != "/guide/" {
		t.Fatalf("indexed = %+v", indexed)
	}
	if len(deleted) != 1 || deleted[0] != DocumentID("/guide/") {
		t.Fatalf("deleted = %+v", deleted)
	}
}

func TestMeilisearchErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()
	client := NewClient(server.URL, "", "wiki_x")
	if _, err := client.Search(context.Background(), "x", 5); err == nil {
		t.Fatal("expected error")
	}
	if err := client.Index(context.Background(), []Document{{ID: "x"}}); err == nil {
		t.Fatal("expected error")
	}
}

func TestDocumentIDAndValidation(t *testing.T) {
	id := DocumentID("/guide/")
	if len(id) != 32 || strings.Contains(id, "/") || strings.Contains(id, "-") {
		t.Fatalf("id = %q", id)
	}
	if DocumentID("/guide/") != id {
		t.Fatal("document id should be stable")
	}
	if !ValidIndexName("wiki_x_test-1") || ValidIndexName("bad/index") || ValidIndexName("") {
		t.Fatal("index validation failed")
	}
	if !ValidBaseURL("http://127.0.0.1:7700") || ValidBaseURL("ftp://x") {
		t.Fatal("url validation failed")
	}
}
