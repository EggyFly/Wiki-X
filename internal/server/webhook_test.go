package server

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/EggyFly/Wiki-X/internal/config"
)

func TestGitHubWebhook(t *testing.T) {
	app := &App{
		cfg: &config.Config{
			Content: config.ContentConfig{Source: "git"},
			GitHub: config.GitHubConfig{
				WebhookSecret: "0123456789abcdef",
				Repo:          "owner/repo",
			},
		},
		reloadCh: make(chan struct{}, 1),
	}
	body := []byte(`{"repository":{"full_name":"owner/repo"}}`)

	resp := webhookRequest(app, body, "push", signWebhook(body, "0123456789abcdef"))
	if resp.Code != http.StatusAccepted {
		t.Fatalf("valid webhook status = %d", resp.Code)
	}
	select {
	case <-app.reloadCh:
	default:
		t.Fatal("reload was not requested")
	}

	resp = webhookRequest(app, body, "push", "sha256=deadbeef")
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("invalid signature status = %d", resp.Code)
	}

	resp = webhookRequest(app, []byte(`{}`), "ping", signWebhook([]byte(`{}`), "0123456789abcdef"))
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), "pong") {
		t.Fatalf("ping = %d, %q", resp.Code, resp.Body.String())
	}

	other := []byte(`{"repository":{"full_name":"other/repo"}}`)
	resp = webhookRequest(app, other, "push", signWebhook(other, "0123456789abcdef"))
	if resp.Code != http.StatusAccepted || !strings.Contains(resp.Body.String(), "ignored") {
		t.Fatalf("repo mismatch = %d, %q", resp.Code, resp.Body.String())
	}
}

func webhookRequest(app *App, body []byte, event, signature string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/-/webhooks/github", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", event)
	req.Header.Set("X-Hub-Signature-256", signature)
	rec := httptest.NewRecorder()
	app.handleGitHubWebhook(rec, req)
	return rec
}

func signWebhook(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
