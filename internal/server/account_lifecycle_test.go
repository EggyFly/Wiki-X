package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestAccountExport(t *testing.T) {
	baseURL, data, _ := setupCommunityApp(t)
	editor := loginTestClient(t, baseURL, "editor", "editor-pass-123")
	resp := mustGet(t, editor, baseURL+"/account/export")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export status = %d", resp.StatusCode)
	}
	if !strings.Contains(resp.Header.Get("Content-Type"), "application/json") {
		t.Fatalf("content type = %q", resp.Header.Get("Content-Type"))
	}
	var exported map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&exported); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	profile, ok := exported["profile"].(map[string]any)
	if !ok || profile["name"] != "editor" {
		t.Fatalf("profile = %+v", exported["profile"])
	}
	if _, exists := profile["password_hash"]; exists {
		t.Fatal("export must not contain password hash")
	}
	user, err := data.UserByName(context.Background(), "editor")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := data.ExportUserData(context.Background(), user.ID); err != nil {
		t.Fatal(err)
	}
}

func TestAccountSelfDelete(t *testing.T) {
	baseURL, data, _ := setupCommunityApp(t)
	editor := loginTestClient(t, baseURL, "editor", "editor-pass-123")
	csrf := fetchCSRFToken(t, editor, baseURL+"/account")

	wrong := postForm(t, editor, baseURL+"/account/delete", url.Values{
		"csrf_token": {csrf},
		"password":   {"wrong-password"},
		"confirm":    {"1"},
	})
	if wrong.StatusCode != http.StatusBadRequest {
		t.Fatalf("wrong password status = %d", wrong.StatusCode)
	}
	wrong.Body.Close()

	missingConfirm := postForm(t, editor, baseURL+"/account/delete", url.Values{
		"csrf_token": {csrf},
		"password":   {"editor-pass-123"},
	})
	if missingConfirm.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing confirm status = %d", missingConfirm.StatusCode)
	}
	missingConfirm.Body.Close()

	resp := postForm(t, editor, baseURL+"/account/delete", url.Values{
		"csrf_token": {csrf},
		"password":   {"editor-pass-123"},
		"confirm":    {"1"},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("delete status = %d", resp.StatusCode)
	}
	if location := resp.Header.Get("Location"); !strings.Contains(location, "notice=account_deleted") {
		t.Fatalf("delete location = %q", location)
	}
	resp.Body.Close()
	if got := loginStatus(t, baseURL, "editor", "editor-pass-123"); got != http.StatusUnauthorized {
		t.Fatalf("deleted user login = %d", got)
	}
	if ok, err := data.HasAudit(context.Background(), "user.delete", "editor"); err != nil || !ok {
		t.Fatalf("delete audit = %v, %v", ok, err)
	}
}

func TestAdminUserDelete(t *testing.T) {
	baseURL, data, _ := setupCommunityApp(t)
	admin := loginTestClient(t, baseURL, "admin", "admin-pass-123")
	editor := loginTestClient(t, baseURL, "editor", "editor-pass-123")
	editorUser, err := data.UserByName(context.Background(), "editor")
	if err != nil {
		t.Fatal(err)
	}
	adminUser, err := data.UserByName(context.Background(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	csrf := fetchCSRFToken(t, admin, baseURL+"/admin/users")
	editorCSRF := fetchCSRFToken(t, editor, baseURL+"/account")

	denied := postForm(t, editor, baseURL+"/admin/users/delete", url.Values{
		"csrf_token": {editorCSRF},
		"id":         {intText(editorUser.ID)},
		"reason":     {"x"},
	})
	if denied.StatusCode != http.StatusForbidden {
		t.Fatalf("editor delete user = %d", denied.StatusCode)
	}
	denied.Body.Close()

	missingReason := postForm(t, admin, baseURL+"/admin/users/delete", url.Values{
		"csrf_token": {csrf},
		"id":         {intText(editorUser.ID)},
	})
	if missingReason.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing reason = %d", missingReason.StatusCode)
	}
	missingReason.Body.Close()

	self := postForm(t, admin, baseURL+"/admin/users/delete", url.Values{
		"csrf_token": {csrf},
		"id":         {intText(adminUser.ID)},
		"reason":     {"self"},
	})
	if self.StatusCode != http.StatusBadRequest {
		t.Fatalf("self delete = %d", self.StatusCode)
	}
	self.Body.Close()

	resp := postForm(t, admin, baseURL+"/admin/users/delete", url.Values{
		"csrf_token": {csrf},
		"id":         {intText(editorUser.ID)},
		"reason":     {"长期不活跃"},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("admin delete status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	if got := loginStatus(t, baseURL, "editor", "editor-pass-123"); got != http.StatusUnauthorized {
		t.Fatalf("deleted editor login = %d", got)
	}
	if _, err := data.UserByName(context.Background(), "editor"); err == nil {
		t.Fatal("deleted user still exists")
	}
	if ok, err := data.HasAudit(context.Background(), "user.delete", "editor"); err != nil || !ok {
		t.Fatalf("delete audit = %v, %v", ok, err)
	}
}
