package server

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestPageDeleteRestoreFlow(t *testing.T) {
	baseURL, data, _ := setupCommunityApp(t)
	admin := loginTestClient(t, baseURL, "admin", "admin-pass-123")
	editor := loginTestClient(t, baseURL, "editor", "editor-pass-123")

	forbidden := mustGet(t, editor, baseURL+"/admin/pages")
	if forbidden.StatusCode != http.StatusForbidden {
		t.Fatalf("editor admin pages = %d", forbidden.StatusCode)
	}
	forbidden.Body.Close()

	csrf := fetchCSRFToken(t, admin, baseURL+"/admin/pages")
	missingReason := postForm(t, admin, baseURL+"/admin/pages/delete", url.Values{
		"csrf_token": {csrf},
		"route":      {"/guide/"},
	})
	if missingReason.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing reason status = %d", missingReason.StatusCode)
	}
	missingReason.Body.Close()

	resp := postForm(t, admin, baseURL+"/admin/pages/delete", url.Values{
		"csrf_token": {csrf},
		"route":      {"/guide/"},
		"reason":     {"侵权内容"},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("delete status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	pageResp := mustGet(t, admin, baseURL+"/guide/")
	if pageResp.StatusCode != http.StatusNotFound {
		t.Fatalf("deleted page status = %d", pageResp.StatusCode)
	}
	pageResp.Body.Close()

	list := readResponse(t, mustGet(t, admin, baseURL+"/admin/pages?status=deleted"))
	if !strings.Contains(list, "侵权内容") || !strings.Contains(list, "/guide/") {
		t.Fatalf("admin page list missing deleted row:\n%s", list)
	}
	edit := mustGet(t, admin, baseURL+"/edit?route=/guide/")
	if edit.StatusCode != http.StatusNotFound {
		t.Fatalf("edit deleted page = %d", edit.StatusCode)
	}
	edit.Body.Close()

	restore := postForm(t, admin, baseURL+"/admin/pages/restore", url.Values{
		"csrf_token": {csrf},
		"route":      {"/guide/"},
	})
	if restore.StatusCode != http.StatusSeeOther {
		t.Fatalf("restore status = %d", restore.StatusCode)
	}
	restore.Body.Close()
	pageResp = mustGet(t, admin, baseURL+"/guide/")
	if pageResp.StatusCode != http.StatusOK {
		t.Fatalf("restored page status = %d", pageResp.StatusCode)
	}
	pageResp.Body.Close()

	page, err := data.GetPage(context.Background(), "/guide/")
	if err != nil || page.DeletedAt != nil {
		t.Fatalf("page = %+v, %v", page, err)
	}
	if ok, err := data.HasAudit(context.Background(), "page.delete", "/guide/"); err != nil || !ok {
		t.Fatalf("delete audit = %v, %v", ok, err)
	}
	if ok, err := data.HasAudit(context.Background(), "page.restore", "/guide/"); err != nil || !ok {
		t.Fatalf("restore audit = %v, %v", ok, err)
	}
}
