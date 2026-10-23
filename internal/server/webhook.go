package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/EggyFly/Wiki-X/internal/content"
	gitsource "github.com/EggyFly/Wiki-X/internal/content/gitsource"
	"github.com/EggyFly/Wiki-X/internal/site"
	"github.com/EggyFly/Wiki-X/internal/store"
)

type githubRepository struct {
	FullName string `json:"full_name"`
}

type githubPushPayload struct {
	Repository githubRepository `json:"repository"`
}

type githubPullRequestPayload struct {
	Action      string           `json:"action"`
	Number      int              `json:"number"`
	Repository  githubRepository `json:"repository"`
	PullRequest struct {
		Merged         bool   `json:"merged"`
		MergeCommitSHA string `json:"merge_commit_sha"`
		Title          string `json:"title"`
		HTMLURL        string `json:"html_url"`
		Base           struct {
			Ref string `json:"ref"`
		} `json:"base"`
		User struct {
			ID    int64  `json:"id"`
			Login string `json:"login"`
		} `json:"user"`
	} `json:"pull_request"`
}

func (a *App) handleGitHubWebhook(w http.ResponseWriter, r *http.Request) {
	if a.cfg == nil || a.cfg.GitHub.WebhookSecret == "" {
		http.NotFound(w, r)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 5<<20))
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	signature := strings.TrimSpace(r.Header.Get("X-Hub-Signature-256"))
	if !validGitHubSignature(body, signature, a.cfg.GitHub.WebhookSecret) {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}
	event := r.Header.Get("X-GitHub-Event")
	switch event {
	case "ping":
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "pong\n")
		return
	case "pull_request":
		a.handlePullRequestEvent(w, body)
		return
	case "push":
		a.handlePushEvent(w, body)
		return
	default:
		webhookAccepted(w, "ignored")
	}
}

func (a *App) handlePushEvent(w http.ResponseWriter, body []byte) {
	if a.cfg.GitHub.Repo != "" {
		var payload githubPushPayload
		if err := json.Unmarshal(body, &payload); err != nil {
			http.Error(w, "bad payload", http.StatusBadRequest)
			return
		}
		if !strings.EqualFold(payload.Repository.FullName, a.cfg.GitHub.Repo) {
			webhookAccepted(w, "ignored")
			return
		}
	}
	if a.cfg.Content.Source == "git" {
		a.requestReload()
		slog.Info("github push accepted")
	}
	webhookAccepted(w, "accepted")
}

func (a *App) handlePullRequestEvent(w http.ResponseWriter, body []byte) {
	if a.cfg.Content.Source != "postgres" || a.cfg.GitHub.PRImport == "off" || a.github == nil || a.store == nil {
		webhookAccepted(w, "ignored")
		return
	}
	var payload githubPullRequestPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "bad payload", http.StatusBadRequest)
		return
	}
	if a.cfg.GitHub.Repo != "" && !strings.EqualFold(payload.Repository.FullName, a.cfg.GitHub.Repo) {
		webhookAccepted(w, "ignored")
		return
	}
	if payload.Action != "closed" || !payload.PullRequest.Merged || payload.Number <= 0 || payload.PullRequest.MergeCommitSHA == "" {
		webhookAccepted(w, "ignored")
		return
	}
	if !strings.EqualFold(payload.PullRequest.Base.Ref, a.cfg.GitHub.BaseBranch) {
		webhookAccepted(w, "ignored")
		return
	}

	payloadCopy := payload
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if err := a.importPullRequest(ctx, payloadCopy); err != nil {
			slog.Error("import github pull request failed", "number", payloadCopy.Number, "err", err)
		}
	}()
	webhookAccepted(w, "accepted")
}

func (a *App) importPullRequest(ctx context.Context, payload githubPullRequestPayload) error {
	target := fmt.Sprintf("%s#%d", a.github.Repo(), payload.Number)
	if exists, err := a.store.HasAudit(ctx, "github.pr.import", target); err != nil {
		return err
	} else if exists {
		return nil
	}

	files, err := a.github.PullRequestFiles(ctx, payload.Number)
	if err != nil {
		return err
	}
	authorID := int64(0)
	subject := strconv.FormatInt(payload.PullRequest.User.ID, 10)
	if id, ok, err := a.store.UserIDByIdentity(ctx, "github", subject); err == nil && ok {
		authorID = id
	}

	prefix := strings.Trim(a.cfg.GitHub.ContentPath, "/")
	publish := a.cfg.GitHub.PRImport == "publish"
	comment := truncateRunes(fmt.Sprintf("GitHub PR #%d: %s (%s)", payload.Number, payload.PullRequest.Title, payload.PullRequest.User.Login), 500)

	mediaMap := make(map[string]string)
	mediaCount, mediaBytes := 0, int64(0)
	for _, file := range files {
		if file.Status == "removed" || !mediaFileExt(fileExt(file.Filename)) {
			continue
		}
		relPath, ok := contentRelPath(file.Filename, prefix)
		if !ok {
			continue
		}
		if mediaCount >= 100 || mediaBytes >= 64<<20 {
			slog.Warn("github import media limit reached", "number", payload.Number)
			break
		}
		data, err := a.github.FileBlob(ctx, file.Filename, payload.PullRequest.MergeCommitSHA, a.cfg.Media.MaxSize)
		if err != nil {
			slog.Warn("fetch github media failed", "file", file.Filename, "err", err)
			continue
		}
		contentType, ext := detectMedia(data)
		if ext == "" {
			slog.Warn("github media type rejected", "file", file.Filename)
			continue
		}
		sum := sha256.Sum256(data)
		name := "pr-" + hex.EncodeToString(sum[:])[:16] + ext
		if err := a.saveImportedMedia(ctx, name, data); err != nil {
			slog.Warn("save github media failed", "file", file.Filename, "err", err)
			continue
		}
		if _, err := a.store.EnsureMedia(ctx, name, contentType, int64(len(data)), authorID); err != nil {
			slog.Warn("record github media failed", "file", file.Filename, "err", err)
			continue
		}
		mediaMap[relPath] = "/media/" + name
		mediaCount++
		mediaBytes += int64(len(data))
	}
	mediaMap = uniqueMediaBasenames(mediaMap)

	imported := 0
	published := 0
	removed := 0
	searchRoutes := make([]string, 0)
	for _, file := range files {
		if !strings.EqualFold(fileExt(file.Filename), ".md") {
			continue
		}
		relPath, ok := contentRelPath(file.Filename, prefix)
		if !ok {
			continue
		}
		if file.Status == "removed" {
			route, err := gitsource.RouteForFile(relPath)
			if err != nil {
				slog.Warn("github removed file route rejected", "file", file.Filename, "err", err)
				continue
			}
			current, err := a.store.GetPage(ctx, route)
			if errors.Is(err, store.ErrNotFound) {
				continue
			}
			if err != nil {
				slog.Warn("load deleted page for github import failed", "route", route, "err", err)
				continue
			}
			if current.Protected {
				slog.Warn("github import skipped protected page delete", "route", route)
				continue
			}
			if current.DeletedAt != nil {
				continue
			}
			reason := truncateRunes(comment, 500)
			if err := a.store.DeletePage(ctx, route, reason, authorID); err != nil {
				slog.Warn("github import delete failed", "route", route, "err", err)
				continue
			}
			removed++
			searchRoutes = append(searchRoutes, route)
			continue
		}
		data, err := a.github.FileContent(ctx, file.Filename, payload.PullRequest.MergeCommitSHA)
		if err != nil {
			slog.Warn("fetch github file failed", "file", file.Filename, "err", err)
			continue
		}
		page, err := gitsource.ParseDocument(relPath, data, time.Now())
		if err != nil {
			slog.Warn("parse github file failed", "file", file.Filename, "err", err)
			continue
		}
		if err := site.ValidateRoute(page.Route); err != nil {
			slog.Warn("github file route rejected", "file", file.Filename, "err", err)
			continue
		}
		page.Body = rewriteMediaPaths(page.Body, mediaMap)
		page.Version = ""
		baseVersion := ""
		if current, err := a.store.GetPage(ctx, page.Route); err == nil {
			if current.DeletedAt != nil {
				slog.Warn("github import skipped deleted page", "route", page.Route)
				continue
			}
			if samePageContent(current, page) {
				continue
			}
			baseVersion = current.Version
		} else if !errors.Is(err, store.ErrNotFound) {
			slog.Warn("load page for github import failed", "route", page.Route, "err", err)
			continue
		}
		_, err = a.store.CreateRevision(ctx, store.RevisionInput{
			Route:          page.Route,
			Title:          page.Title,
			Description:    page.Description,
			Body:           page.Body,
			Tags:           page.Tags,
			AuthorID:       authorID,
			Comment:        comment,
			BaseVersion:    baseVersion,
			TranslationKey: page.TranslationKey,
			Publish:        publish,
		})
		if errors.Is(err, store.ErrProtected) {
			slog.Warn("github import skipped protected page", "route", page.Route, "file", file.Filename)
			continue
		}
		if errors.Is(err, store.ErrEditConflict) {
			slog.Warn("github import conflict", "route", page.Route, "file", file.Filename)
			continue
		}
		if err != nil {
			slog.Warn("github import revision failed", "route", page.Route, "err", err)
			continue
		}
		imported++
		if publish {
			published++
			searchRoutes = append(searchRoutes, page.Route)
		}
	}
	if published > 0 || removed > 0 {
		a.reloadSite(ctx)
	}
	for _, route := range searchRoutes {
		a.syncSearchDocument(ctx, route)
	}
	_ = a.store.LogAudit(ctx, 0, "github.pr.import", target,
		fmt.Sprintf("files=%d media=%d imported=%d published=%d removed=%d", len(files), mediaCount, imported, published, removed))
	slog.Info("github pull request imported", "number", payload.Number, "files", len(files),
		"media", mediaCount, "imported", imported, "published", published, "removed", removed)
	return nil
}

func contentRelPath(filename, prefix string) (string, bool) {
	relPath := filename
	if prefix != "" {
		if !strings.HasPrefix(relPath, prefix+"/") {
			return "", false
		}
		relPath = strings.TrimPrefix(relPath, prefix+"/")
	}
	if relPath == "" || strings.Contains(relPath, "..") {
		return "", false
	}
	return relPath, true
}

func mediaFileExt(ext string) bool {
	switch strings.ToLower(ext) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".wav", ".mp3", ".ogg", ".flac":
		return true
	default:
		return false
	}
}

func (a *App) saveImportedMedia(ctx context.Context, name string, data []byte) error {
	if a.mediaStore == nil {
		return errors.New("media store is not configured")
	}
	return a.mediaStore.Put(ctx, name, mediaContentType(name), data)
}

func uniqueMediaBasenames(media map[string]string) map[string]string {
	count := make(map[string]int, len(media))
	for relPath := range media {
		count[filepath.Base(relPath)]++
	}
	result := make(map[string]string, len(media)*3)
	for relPath, url := range media {
		result[relPath] = url
		base := filepath.Base(relPath)
		if count[base] == 1 {
			result[base] = url
			result["./"+base] = url
		}
		result["/"+relPath] = url
	}
	return result
}

func rewriteMediaPaths(body string, media map[string]string) string {
	keys := make([]string, 0, len(media))
	for from := range media {
		keys = append(keys, from)
	}
	sort.Slice(keys, func(i, j int) bool {
		if len(keys[i]) != len(keys[j]) {
			return len(keys[i]) > len(keys[j])
		}
		return keys[i] < keys[j]
	})
	for _, from := range keys {
		body = strings.ReplaceAll(body, from, media[from])
	}
	return body
}

func samePageContent(current *store.PageRecord, incoming *content.Page) bool {
	if current.Title != incoming.Title || current.Description != incoming.Description || current.Body != incoming.Body || current.TranslationKey != incoming.TranslationKey {
		return false
	}
	if len(current.Tags) != len(incoming.Tags) {
		return false
	}
	for i := range current.Tags {
		if current.Tags[i] != incoming.Tags[i] {
			return false
		}
	}
	return true
}

func fileExt(name string) string {
	if index := strings.LastIndexByte(name, '.'); index >= 0 {
		return name[index:]
	}
	return ""
}

func truncateRunes(value string, limit int) string {
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	return string([]rune(value)[:limit])
}

func webhookAccepted(w http.ResponseWriter, message string) {
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusAccepted)
	_, _ = io.WriteString(w, message+"\n")
}

func validGitHubSignature(body []byte, signature, secret string) bool {
	if !strings.HasPrefix(signature, "sha256=") {
		return false
	}
	got, err := hex.DecodeString(strings.TrimPrefix(signature, "sha256="))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(got, mac.Sum(nil))
}
