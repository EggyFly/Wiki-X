package server

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/EggyFly/Wiki-X/internal/store"
)

func TestInteractiveNodesFlow(t *testing.T) {
	baseURL, data, _ := setupCommunityApp(t)
	admin := loginTestClient(t, baseURL, "admin", "admin-pass-123")
	ctx := context.Background()
	user, err := data.UserByName(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	node, err := data.SaveInteractiveNode(ctx, store.InteractiveInput{
		NodeType: "widget", Key: "daily", Title: "Daily Card",
		Config:  map[string]string{"kind": "card", "line": "hello"},
		Enabled: true, ActorID: user.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	page := mustGet(t, admin, baseURL+"/widgets/daily")
	if body := readResponse(t, page); !strings.Contains(body, "Daily Card") || !strings.Contains(body, "hello") {
		t.Fatalf("widget page = %s", body)
	}
	cs := fetchCSRFToken(t, admin, baseURL+"/admin/interactive")
	bad := postForm(t, admin, baseURL+"/admin/interactive/save", url.Values{
		"csrf_token": {cs}, "node_type": {"widget"}, "key": {"bad"}, "title": {"Bad"},
		"config": {"not-json"}, "enabled": {"1"},
	})
	if bad.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad config status = %d", bad.StatusCode)
	}
	bad.Body.Close()
	created := postForm(t, admin, baseURL+"/admin/interactive/save", url.Values{
		"csrf_token": {cs}, "node_type": {"game"}, "key": {"quiz"}, "title": {"Daily Quiz"},
		"config": {`{"kind":"quiz","question":"2 plus 2","options":"3|4","answer":"4"}`}, "enabled": {"1"},
	})
	if created.StatusCode != http.StatusSeeOther {
		t.Fatalf("create status = %d", created.StatusCode)
	}
	created.Body.Close()
	game := mustGet(t, admin, baseURL+"/games/quiz")
	if body := readResponse(t, game); !strings.Contains(body, "Daily Quiz") || !strings.Contains(body, "Show answer") {
		t.Fatalf("game page = %s", body)
	}
	disabled := postForm(t, admin, baseURL+"/admin/interactive/delete", url.Values{
		"csrf_token": {cs}, "id": {strconv.FormatInt(node.ID, 10)},
	})
	if disabled.StatusCode != http.StatusSeeOther {
		t.Fatalf("delete status = %d", disabled.StatusCode)
	}
	disabled.Body.Close()
	gone := mustGet(t, admin, baseURL+"/widgets/daily")
	if gone.StatusCode != http.StatusNotFound {
		t.Fatalf("deleted widget status = %d", gone.StatusCode)
	}
	gone.Body.Close()
}
