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
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"wikix/internal/content"
	gitsource "wikix/internal/content/gitsource"
	"wikix/internal/site"
	"wikix/internal/store"
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
	imported := 0
	published := 0
	for _, file := range files {
		if file.Status == "removed" || !strings.EqualFold(fileExt(file.Filename), ".md") {
			continue
		}
		relPath := file.Filename
		if prefix != "" {
			if !strings.HasPrefix(relPath, prefix+"/") {
				continue
			}
			relPath = strings.TrimPrefix(relPath, prefix+"/")
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
		baseVersion := ""
		if current, err := a.store.GetPage(ctx, page.Route); err == nil {
			if samePageContent(current, page) {
				continue
			}
			baseVersion = current.Version
		} else if !errors.Is(err, store.ErrNotFound) {
			slog.Warn("load page for github import failed", "route", page.Route, "err", err)
			continue
		}
		comment := fmt.Sprintf("GitHub PR #%d: %s (%s)", payload.Number, payload.PullRequest.Title, payload.PullRequest.User.Login)
		comment = truncateRunes(comment, 500)
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
		}
	}
	if published > 0 {
		a.reloadSite(ctx)
	}
	_ = a.store.LogAudit(ctx, 0, "github.pr.import", target, fmt.Sprintf("files=%d imported=%d published=%d", len(files), imported, published))
	slog.Info("github pull request imported", "number", payload.Number, "files", len(files), "imported", imported, "published", published)
	return nil
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
