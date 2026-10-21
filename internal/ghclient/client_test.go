package ghclient

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestCreatePullRequest(t *testing.T) {
	var putPayload map[string]string
	var prPayload map[string]string
	existing := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/git/ref/heads/"):
			writeJSON(w, map[string]any{"object": map[string]string{"sha": "base-sha"}})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git/refs"):
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{}`)
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/contents/"):
			if !existing {
				http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
				return
			}
			writeJSON(w, map[string]string{"sha": "file-sha"})
		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/contents/"):
			_ = json.NewDecoder(r.Body).Decode(&putPayload)
			writeJSON(w, map[string]any{"content": map[string]string{}})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/pulls"):
			_ = json.NewDecoder(r.Body).Decode(&prPayload)
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"html_url":"https://github.com/owner/repo/pull/3","number":3}`)
		default:
			http.Error(w, "unexpected", http.StatusNotFound)
		}
	}))
	defer server.Close()

	client, err := New(Config{Repo: "owner/repo", Token: "test-token", BaseURL: server.URL, BaseBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	created, err := client.CreatePullRequest(context.Background(), PullRequestInput{
		Path:    "content/page.md",
		Content: []byte("hello"),
		Title:   "Edit page",
		Body:    "body",
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.URL != "https://github.com/owner/repo/pull/3" || created.Number != 3 {
		t.Fatalf("created = %+v", created)
	}
	if _, ok := putPayload["sha"]; ok {
		t.Fatal("new file should not send sha")
	}
	decoded, err := base64.StdEncoding.DecodeString(putPayload["content"])
	if err != nil || string(decoded) != "hello" {
		t.Fatalf("content = %q, %v", decoded, err)
	}
	if !strings.HasPrefix(putPayload["branch"], "github.com/EggyFly/Wiki-X/") {
		t.Fatalf("branch = %q", putPayload["branch"])
	}
	if prPayload["title"] != "Edit page" || prPayload["base"] != "main" {
		t.Fatalf("pr payload = %+v", prPayload)
	}

	existing = true
	if _, err := client.CreatePullRequest(context.Background(), PullRequestInput{
		Path:    "content/page.md",
		Content: []byte("hello2"),
		Title:   "Edit page",
	}); err != nil {
		t.Fatal(err)
	}
	if putPayload["sha"] != "file-sha" {
		t.Fatalf("sha = %q", putPayload["sha"])
	}
}

func TestPullRequestFilesAndContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/pulls/9/files"):
			writeJSON(w, []map[string]string{{"filename": "content/page.md", "status": "added"}})
		case strings.Contains(r.URL.Path, "/contents/content/page.md"):
			encoded := base64.StdEncoding.EncodeToString([]byte("page content"))
			writeJSON(w, map[string]any{"type": "file", "encoding": "base64", "content": encoded, "size": len("page content")})
		case strings.Contains(r.URL.Path, "/contents/content/icon.png"):
			writeJSON(w, map[string]any{"type": "file", "sha": "blob-sha", "size": 4})
		case strings.HasSuffix(r.URL.Path, "/git/blobs/blob-sha"):
			writeJSON(w, map[string]any{"encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte("PNG!")), "size": 4})
		default:
			http.Error(w, "unexpected", http.StatusNotFound)
		}
	}))
	defer server.Close()

	client, err := New(Config{Repo: "owner/repo", Token: "test-token", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	files, err := client.PullRequestFiles(context.Background(), 9)
	if err != nil || len(files) != 1 || files[0].Filename != "content/page.md" {
		t.Fatalf("files = %+v, %v", files, err)
	}
	content, err := client.FileContent(context.Background(), "content/page.md", "merge-sha")
	if err != nil || string(content) != "page content" {
		t.Fatalf("content = %q, %v", content, err)
	}
	blob, err := client.FileBlob(context.Background(), "content/icon.png", "merge-sha", 1024)
	if err != nil || string(blob) != "PNG!" {
		t.Fatalf("blob = %q, %v", blob, err)
	}
	if _, err := client.FileBlob(context.Background(), "content/icon.png", "merge-sha", 2); err == nil {
		t.Fatal("oversized blob should fail")
	}
}

func TestPullRequestFilesPagination(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/pulls/9/files") {
			http.Error(w, "unexpected", http.StatusNotFound)
			return
		}
		var files []map[string]string
		count := 1
		if r.URL.Query().Get("page") == "1" {
			count = 100
		}
		for i := 0; i < count; i++ {
			files = append(files, map[string]string{"filename": "content/page.md", "status": "modified"})
		}
		writeJSON(w, files)
	}))
	defer server.Close()

	client, err := New(Config{Repo: "owner/repo", Token: "test-token", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	files, err := client.PullRequestFiles(context.Background(), 9)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 101 {
		t.Fatalf("files = %d", len(files))
	}
}

func TestGitHubAppAuth(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	var tokenRequests atomic.Int32
	var apiRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/access_tokens"):
			if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
				http.Error(w, "missing jwt", http.StatusUnauthorized)
				return
			}
			tokenRequests.Add(1)
			writeJSON(w, map[string]string{
				"token":      "installation-token",
				"expires_at": "2030-01-01T00:00:00Z",
			})
		case strings.HasSuffix(r.URL.Path, "/pulls/9/files"):
			if r.Header.Get("Authorization") != "Bearer installation-token" {
				http.Error(w, "wrong token", http.StatusUnauthorized)
				return
			}
			apiRequests.Add(1)
			writeJSON(w, []map[string]string{{"filename": "content/a.md", "status": "added"}})
		default:
			http.Error(w, "unexpected", http.StatusNotFound)
		}
	}))
	defer server.Close()

	client, err := New(Config{
		Repo:           "owner/repo",
		AppID:          1,
		InstallationID: 7,
		PrivateKey:     keyPEM,
		BaseURL:        server.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		files, err := client.PullRequestFiles(context.Background(), 9)
		if err != nil || len(files) != 1 {
			t.Fatalf("files = %+v, %v", files, err)
		}
	}
	if tokenRequests.Load() != 1 {
		t.Fatalf("token requests = %d", tokenRequests.Load())
	}
	if apiRequests.Load() != 2 {
		t.Fatalf("api requests = %d", apiRequests.Load())
	}
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
