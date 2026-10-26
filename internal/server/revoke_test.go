package server

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/EggyFly/Wiki-X/internal/revoke"
)

func TestAdminRevokedRestore(t *testing.T) {
	baseURL, data, _ := setupCommunityApp(t)
	admin, err := data.UserByName(t.Context(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := data.CreateGroup(t.Context(), "revived", []string{"read"}, admin.ID); err != nil {
		t.Fatal(err)
	}
	groups, err := data.ListGroups(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var groupID int64
	for _, group := range groups {
		if group.Name == "revived" {
			groupID = group.ID
		}
	}
	if groupID == 0 {
		t.Fatal("group not found")
	}
	if err := data.DeleteGroup(t.Context(), groupID, admin.ID); err != nil {
		t.Fatal(err)
	}

	client := loginTestClient(t, baseURL, "admin", "admin-pass-123")
	csrf := fetchCSRFToken(t, client, baseURL+"/admin/revoked")
	page := readResponse(t, mustGet(t, client, baseURL+"/admin/revoked"))
	if !strings.Contains(page, "revived") {
		t.Fatal("revoked group is not listed")
	}
	resp := postForm(t, client, baseURL+"/admin/revoked/restore", url.Values{
		"csrf_token": {csrf},
		"kind":       {"group"},
		"id":         {strconvInt(groupID)},
	})
	if resp.StatusCode != http.StatusSeeOther || !strings.Contains(resp.Header.Get("Location"), "notice=revoked_restored") {
		t.Fatalf("restore status = %d, location = %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	resp.Body.Close()
	groups, err = data.ListGroups(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, group := range groups {
		if group.Name == "revived" {
			found = true
		}
	}
	if !found {
		t.Fatal("group was not restored")
	}
}

func TestAdminDeleteDisabledByPolicy(t *testing.T) {
	baseURL, data, _ := setupCommunityApp(t)
	admin, err := data.UserByName(t.Context(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := data.CreateGroup(t.Context(), "protected", []string{"read"}, admin.ID); err != nil {
		t.Fatal(err)
	}
	groups, err := data.ListGroups(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var groupID int64
	for _, group := range groups {
		if group.Name == "protected" {
			groupID = group.ID
		}
	}
	data.SetRevokePolicy(revoke.Policy{Sections: map[string]bool{revoke.SectionGroup: false}})
	client := loginTestClient(t, baseURL, "admin", "admin-pass-123")
	csrf := fetchCSRFToken(t, client, baseURL+"/admin/groups")
	resp := postForm(t, client, baseURL+"/admin/groups/delete", url.Values{
		"csrf_token": {csrf},
		"id":         {strconvInt(groupID)},
	})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("delete status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	groups, err = data.ListGroups(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, group := range groups {
		if group.Name == "protected" {
			return
		}
	}
	t.Fatal("group was deleted while section was disabled")
}
