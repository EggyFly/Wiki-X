package server

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func TestProtectionLevelsAndEditRequest(t *testing.T) {
	baseURL, data, _ := setupCommunityApp(t)
	admin := loginTestClient(t, baseURL, "admin", "admin-pass-123")
	editor := loginTestClient(t, baseURL, "editor", "editor-pass-123")
	if _, err := data.GetPage(context.Background(), "/guide/"); err != nil {
		t.Fatal(err)
	}
	adminCSRF := fetchCSRFToken(t, admin, baseURL+"/edit?route=/guide/")

	resp := postForm(t, admin, baseURL+"/protect", url.Values{
		"csrf_token": {adminCSRF},
		"route":      {"/guide/"},
		"level":      {"semi"},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("semi protect status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	protected, err := data.GetPage(context.Background(), "/guide/")
	if err != nil || protected.ProtectionLevel != "semi" || !protected.Protected {
		t.Fatalf("protected page = %+v, %v", protected, err)
	}

	editorPage := mustGet(t, editor, baseURL+"/edit?route=/guide/")
	editorHTML := readResponse(t, editorPage)
	if editorPage.StatusCode != http.StatusOK || !strings.Contains(editorHTML, "半保护") {
		t.Fatalf("editor edit page = %d", editorPage.StatusCode)
	}
	editorCSRF := csrfFromPage(t, editorHTML)
	resp = postForm(t, editor, baseURL+"/edit/save", url.Values{
		"csrf_token":   {editorCSRF},
		"route":        {"/guide/"},
		"title":        {"Guide"},
		"body":         {"edit request body"},
		"base_version": {protected.Version},
		"comment":      {"please apply"},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("edit request status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	pending, err := data.ListPendingRevisions(context.Background(), 10)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending = %d, %v", len(pending), err)
	}
	if pending[0].Body != "edit request body" {
		t.Fatalf("pending revision = %+v", pending[0])
	}

	reviewCSRF := fetchCSRFToken(t, admin, baseURL+"/review")
	approve := postForm(t, admin, baseURL+"/review/approve", url.Values{
		"csrf_token": {reviewCSRF},
		"id":         {strconv.FormatInt(pending[0].ID, 10)},
	})
	if approve.StatusCode != http.StatusSeeOther {
		t.Fatalf("approve status = %d", approve.StatusCode)
	}
	approve.Body.Close()
	pageHTML := readResponse(t, mustGet(t, testHTTPClient(), baseURL+"/guide/"))
	if !strings.Contains(pageHTML, "edit request body") {
		t.Fatal("approved edit request not published")
	}

	adminCSRF = fetchCSRFToken(t, admin, baseURL+"/edit?route=/guide/")
	resp = postForm(t, admin, baseURL+"/protect", url.Values{
		"csrf_token": {adminCSRF},
		"route":      {"/guide/"},
		"level":      {"full"},
	})
	resp.Body.Close()
	editorPage = mustGet(t, editor, baseURL+"/edit?route=/guide/")
	if editorPage.StatusCode != http.StatusForbidden {
		t.Fatalf("editor full-protected edit = %d", editorPage.StatusCode)
	}
	editorPage.Body.Close()
	current, err := data.GetPage(context.Background(), "/guide/")
	if err != nil {
		t.Fatal(err)
	}
	editorCSRF = fetchCSRFToken(t, editor, baseURL+"/account")
	resp = postForm(t, editor, baseURL+"/edit/save", url.Values{
		"csrf_token":   {editorCSRF},
		"route":        {"/guide/"},
		"title":        {"Guide"},
		"body":         {"bypass"},
		"base_version": {current.Version},
	})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("editor full-protected save = %d", resp.StatusCode)
	}
	resp.Body.Close()

	adminCSRF = fetchCSRFToken(t, admin, baseURL+"/edit?route=/guide/")
	bad := postForm(t, admin, baseURL+"/protect", url.Values{
		"csrf_token": {adminCSRF},
		"route":      {"/guide/"},
		"level":      {"bogus"},
	})
	if bad.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad level status = %d", bad.StatusCode)
	}
	bad.Body.Close()
	resp = postForm(t, admin, baseURL+"/protect", url.Values{
		"csrf_token": {adminCSRF},
		"route":      {"/guide/"},
		"level":      {"none"},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("unprotect status = %d", resp.StatusCode)
	}
	resp.Body.Close()
}
