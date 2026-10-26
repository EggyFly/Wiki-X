package server

import (
	"net/http"
	"strings"
	"testing"
)

func TestUserProfile(t *testing.T) {
	baseURL, _, _ := setupCommunityApp(t)
	client := testHTTPClient()
	page := readResponse(t, mustGet(t, client, baseURL+"/u/admin"))
	if !strings.Contains(page, "admin") || !strings.Contains(page, "加入于") {
		t.Fatalf("profile page = %s", page)
	}
	resp := mustGet(t, client, baseURL+"/u/missing-user")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing profile status = %d", resp.StatusCode)
	}
	resp.Body.Close()
}
