package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/EggyFly/Wiki-X/internal/auth"
	"github.com/EggyFly/Wiki-X/internal/config"
	"github.com/EggyFly/Wiki-X/internal/site"
	"github.com/EggyFly/Wiki-X/internal/store"
)

func TestGitModeCreatesPullRequest(t *testing.T) {
	databaseURL := os.Getenv("TEST_POSTGRES_URL_SERVER")
	if databaseURL == "" {
		t.Skip("set TEST_POSTGRES_URL_SERVER to run server integration tests")
	}
	ctx := context.Background()
	data, err := store.Open(ctx, config.DatabaseConfig{URL: databaseURL, MaxConns: 4})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(data.Close)
	if err := data.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `TRUNCATE comment_reports, watchlist, comments, audit_logs, media, sessions, page_revisions, pages, identities, user_groups, users RESTART IDENTITY CASCADE`)
	pool.Close()
	if err != nil {
		t.Fatal(err)
	}
	user, err := data.CreateUser(ctx, "admin", "", "hash", true)
	if err != nil {
		t.Fatal(err)
	}

	remoteMarkdown := "---\ntitle: Guide\ndescription: old\n---\n\nold body\n"
	var putPayload map[string]string
	var putPath string
	var mu sync.Mutex
	fakeGitHub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/git/ref/heads/"):
			writeHTTPJSON(w, map[string]any{"object": map[string]string{"sha": "base-sha"}})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git/refs"):
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{}`)
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/contents/"):
			writeHTTPJSON(w, map[string]any{
				"sha":      "file-sha",
				"type":     "file",
				"encoding": "base64",
				"size":     len(remoteMarkdown),
				"content":  base64.StdEncoding.EncodeToString([]byte(remoteMarkdown)),
			})
		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/contents/"):
			var payload map[string]string
			_ = json.NewDecoder(r.Body).Decode(&payload)
			mu.Lock()
			putPayload = payload
			putPath = r.URL.Path
			mu.Unlock()
			writeHTTPJSON(w, map[string]any{"content": map[string]string{}})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/pulls"):
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"html_url":"https://github.com/owner/repo/pull/8","number":8}`)
		default:
			http.Error(w, "unexpected", http.StatusNotFound)
		}
	}))
	defer fakeGitHub.Close()

	contentDir := t.TempDir()
	writeFile(t, contentDir, "guide.md", remoteMarkdown)
	cfg := &config.Config{
		Site: config.SiteConfig{Title: "Git PR Wiki", Language: "zh-CN", CommunityUI: true},
		Content: config.ContentConfig{
			Source:      "git",
			Dir:         contentDir,
			MaxFileSize: 1 << 20,
		},
		Auth:  config.AuthConfig{SessionTTL: "1h"},
		Media: config.MediaConfig{Dir: t.TempDir(), MaxSize: 1 << 20},
		GitHub: config.GitHubConfig{
			Repo:        "owner/repo",
			Token:       "test-token",
			BaseBranch:  "main",
			ContentPath: "content",
			PRImport:    "off",
			APIBaseURL:  fakeGitHub.URL,
		},
	}
	loaded, err := site.Load(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loaded.Close() })
	page, ok := loaded.FindPage("/guide/")
	if !ok {
		t.Fatal("page not found")
	}
	authManager := auth.New(cfg.Auth, data)
	app, err := NewApp(cfg, loaded, data, authManager, nil)
	if err != nil {
		t.Fatal(err)
	}
	session := &store.SessionData{
		User:      &store.User{ID: user.ID, Name: user.Name, Rights: user.Rights},
		CSRFToken: "csrf",
	}
	req := httptest.NewRequest(http.MethodGet, "/edit?route=/guide/", nil)
	req = req.WithContext(auth.WithSession(req.Context(), session))
	view := editData{
		baseData:    app.baseData(req, "编辑"),
		Route:       "/guide/",
		PageTitle:   "Guide",
		Description: "new desc",
		Tags:        "tag",
		Body:        "new git body",
		Comment:     "test",
		BaseVersion: page.Version,
		PRMode:      true,
	}
	rec := httptest.NewRecorder()
	app.saveGitEdit(rec, req, session, view, "/guide/", "Guide", "new desc", "new git body", []string{"tag"}, "guide-start", "test", page.Version)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "https://github.com/owner/repo/pull/8") {
		t.Fatalf("pr page missing url: %s", rec.Body.String())
	}
	mu.Lock()
	gotPath, gotPayload := putPath, putPayload
	mu.Unlock()
	if !strings.HasSuffix(gotPath, "/contents/content/guide.md") {
		t.Fatalf("path = %q", gotPath)
	}
	decoded, err := base64.StdEncoding.DecodeString(gotPayload["content"])
	if err != nil {
		t.Fatal(err)
	}
	text := string(decoded)
	for _, want := range []string{"title: Guide", "slug: guide", "new git body"} {
		if !strings.Contains(text, want) {
			t.Fatalf("content missing %q: %s", want, text)
		}
	}
}

func writeHTTPJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
