package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/EggyFly/Wiki-X/internal/auth"
	"github.com/EggyFly/Wiki-X/internal/config"
	"github.com/EggyFly/Wiki-X/internal/content"
	"github.com/EggyFly/Wiki-X/internal/site"
	"github.com/EggyFly/Wiki-X/internal/store"
)

var csrfPattern = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)

func TestCommunityFlow(t *testing.T) {
	baseURL, data, _ := setupCommunityApp(t)

	t.Run("anonymous edit redirects to login", func(t *testing.T) {
		client := testHTTPClient()
		resp := mustGet(t, client, baseURL+"/edit")
		if resp.StatusCode != http.StatusSeeOther {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		if !strings.HasPrefix(resp.Header.Get("Location"), "/login?next=") {
			t.Fatalf("location = %q", resp.Header.Get("Location"))
		}
	})

	editor := loginTestClient(t, baseURL, "editor", "editor-pass-123")
	admin := loginTestClient(t, baseURL, "admin", "admin-pass-123")

	page, err := data.GetPage(context.Background(), "/guide/")
	if err != nil {
		t.Fatal(err)
	}
	editorCSRF := fetchCSRFToken(t, editor, baseURL+"/edit?route=/guide/")
	resp := postForm(t, editor, baseURL+"/edit/save", url.Values{
		"csrf_token":   {editorCSRF},
		"route":        {"/guide/"},
		"title":        {"Guide"},
		"description":  {"updated guide"},
		"tags":         {"guide, test"},
		"body":         {"editor guide v2"},
		"comment":      {"editor update"},
		"base_version": {page.Version},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("save status = %d", resp.StatusCode)
	}
	if !strings.Contains(resp.Header.Get("Location"), "notice=pending") {
		t.Fatalf("location = %q", resp.Header.Get("Location"))
	}

	pageResp := mustGet(t, editor, baseURL+"/guide/")
	if body := readResponse(t, pageResp); !strings.Contains(body, "old guide") {
		t.Fatal("pending revision should not be visible")
	}
	pending, err := data.ListPendingRevisions(context.Background(), 10)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending = %d, %v", len(pending), err)
	}
	reviewResp := mustGet(t, editor, baseURL+"/review")
	if reviewResp.StatusCode != http.StatusForbidden {
		t.Fatalf("editor review status = %d", reviewResp.StatusCode)
	}
	reviewResp.Body.Close()

	adminCSRF := fetchCSRFToken(t, admin, baseURL+"/review")
	approveResp := postForm(t, admin, baseURL+"/review/approve", url.Values{
		"csrf_token": {adminCSRF},
		"id":         {strconvInt(pending[0].ID)},
	})
	if approveResp.StatusCode != http.StatusSeeOther {
		t.Fatalf("approve status = %d", approveResp.StatusCode)
	}
	approveResp.Body.Close()
	pageResp = mustGet(t, admin, baseURL+"/guide/")
	if body := readResponse(t, pageResp); !strings.Contains(body, "editor guide v2") {
		t.Fatal("approved revision not visible")
	}

	page, err = data.GetPage(context.Background(), "/guide/")
	if err != nil {
		t.Fatal(err)
	}
	adminCSRF = fetchCSRFToken(t, admin, baseURL+"/edit?route=/guide/")
	saveResp := postForm(t, admin, baseURL+"/edit/save", url.Values{
		"csrf_token":   {adminCSRF},
		"route":        {"/guide/"},
		"title":        {"Guide"},
		"description":  {"final guide"},
		"tags":         {"guide"},
		"body":         {"admin final content"},
		"comment":      {"admin publish"},
		"base_version": {page.Version},
	})
	if saveResp.StatusCode != http.StatusSeeOther {
		t.Fatalf("admin save status = %d", saveResp.StatusCode)
	}
	if !strings.Contains(saveResp.Header.Get("Location"), "notice=published") {
		t.Fatalf("admin save location = %q", saveResp.Header.Get("Location"))
	}
	saveResp.Body.Close()
	pageResp = mustGet(t, admin, baseURL+"/guide/")
	if body := readResponse(t, pageResp); !strings.Contains(body, "admin final content") {
		t.Fatal("admin publish not visible")
	}

	searchResp := mustGet(t, admin, baseURL+"/search?q="+url.QueryEscape("admin final"))
	if body := readResponse(t, searchResp); !strings.Contains(body, "admin final content") {
		t.Fatal("search did not find content")
	}

	adminCSRF = fetchCSRFToken(t, admin, baseURL+"/review")
	png, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	uploadResp := uploadTestFile(t, admin, baseURL, adminCSRF, png)
	if uploadResp.StatusCode != http.StatusCreated {
		t.Fatalf("upload status = %d", uploadResp.StatusCode)
	}
	var uploaded struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(uploadResp.Body).Decode(&uploaded); err != nil {
		t.Fatal(err)
	}
	uploadResp.Body.Close()
	if !strings.HasPrefix(uploaded.URL, "/media/") {
		t.Fatalf("media url = %q", uploaded.URL)
	}
	mediaResp := mustGet(t, testHTTPClient(), baseURL+uploaded.URL)
	if mediaResp.StatusCode != http.StatusOK {
		t.Fatalf("media status = %d", mediaResp.StatusCode)
	}
	if mediaResp.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("media content type = %q", mediaResp.Header.Get("Content-Type"))
	}
	mediaResp.Body.Close()

	accountResp := mustGet(t, admin, baseURL+"/account")
	if accountResp.StatusCode != http.StatusOK {
		t.Fatalf("account status = %d", accountResp.StatusCode)
	}
	if body := readResponse(t, accountResp); !strings.Contains(body, "账号") {
		t.Fatal("account page missing")
	}

	badCSRF := postForm(t, admin, baseURL+"/edit/save", url.Values{
		"route":        {"/guide/"},
		"title":        {"Guide"},
		"body":         {"no csrf"},
		"base_version": {page.Version},
	})
	if badCSRF.StatusCode != http.StatusForbidden {
		t.Fatalf("csrf status = %d", badCSRF.StatusCode)
	}
	badCSRF.Body.Close()
}

func TestGovernanceFlow(t *testing.T) {
	baseURL, data, _ := setupCommunityApp(t)

	editor := loginTestClient(t, baseURL, "editor", "editor-pass-123")
	admin := loginTestClient(t, baseURL, "admin", "admin-pass-123")
	editorUser, err := data.UserByName(context.Background(), "editor")
	if err != nil {
		t.Fatal(err)
	}

	historyURL := baseURL + "/history?route=" + url.QueryEscape("/guide/")
	editorCSRF := fetchCSRFToken(t, editor, historyURL)

	watchResp := postForm(t, editor, baseURL+"/watch", url.Values{
		"csrf_token": {editorCSRF},
		"route":      {"/guide/"},
		"action":     {"watch"},
		"next":       {"/history?route=/guide/"},
	})
	if watchResp.StatusCode != http.StatusSeeOther {
		t.Fatalf("watch status = %d", watchResp.StatusCode)
	}
	watchResp.Body.Close()

	watched, err := data.IsWatched(context.Background(), editorUser.ID, "/guide/")
	if err != nil || !watched {
		t.Fatalf("watched = %v, %v", watched, err)
	}
	watchlistResp := mustGet(t, editor, baseURL+"/watchlist")
	if body := readResponse(t, watchlistResp); !strings.Contains(body, "Guide") {
		t.Fatal("watchlist does not show the watched page")
	}

	unwatchResp := postForm(t, editor, baseURL+"/watch", url.Values{
		"csrf_token": {editorCSRF},
		"route":      {"/guide/"},
		"action":     {"unwatch"},
		"next":       {"/watchlist"},
	})
	if unwatchResp.StatusCode != http.StatusSeeOther {
		t.Fatalf("unwatch status = %d", unwatchResp.StatusCode)
	}
	unwatchResp.Body.Close()
	watched, err = data.IsWatched(context.Background(), editorUser.ID, "/guide/")
	if err != nil || watched {
		t.Fatalf("watched after unwatch = %v, %v", watched, err)
	}

	forbiddenProtect := postForm(t, editor, baseURL+"/protect", url.Values{
		"csrf_token": {editorCSRF},
		"route":      {"/guide/"},
		"protected":  {"1"},
	})
	if forbiddenProtect.StatusCode != http.StatusForbidden {
		t.Fatalf("editor protect status = %d", forbiddenProtect.StatusCode)
	}
	forbiddenProtect.Body.Close()

	adminCSRF := fetchCSRFToken(t, admin, baseURL+"/edit?route="+url.QueryEscape("/guide/"))
	protectResp := postForm(t, admin, baseURL+"/protect", url.Values{
		"csrf_token": {adminCSRF},
		"route":      {"/guide/"},
		"protected":  {"1"},
	})
	if protectResp.StatusCode != http.StatusSeeOther {
		t.Fatalf("admin protect status = %d", protectResp.StatusCode)
	}
	protectResp.Body.Close()
	page, err := data.GetPage(context.Background(), "/guide/")
	if err != nil || !page.Protected {
		t.Fatalf("protected = %v, %v", page.Protected, err)
	}

	editorEdit := mustGet(t, editor, baseURL+"/edit?route="+url.QueryEscape("/guide/"))
	if editorEdit.StatusCode != http.StatusOK {
		t.Fatalf("editor edit semi-protected status = %d", editorEdit.StatusCode)
	}
	editorEdit.Body.Close()

	editorSave := postForm(t, editor, baseURL+"/edit/save", url.Values{
		"csrf_token":   {editorCSRF},
		"route":        {"/guide/"},
		"title":        {"Guide"},
		"body":         {"editor edit request"},
		"base_version": {page.Version},
	})
	if editorSave.StatusCode != http.StatusSeeOther {
		t.Fatalf("editor edit request status = %d", editorSave.StatusCode)
	}
	editorSave.Body.Close()

	adminEdit := mustGet(t, admin, baseURL+"/edit?route="+url.QueryEscape("/guide/"))
	if adminEdit.StatusCode != http.StatusOK {
		t.Fatalf("admin edit protected status = %d", adminEdit.StatusCode)
	}
	adminEdit.Body.Close()

	discussionURL := baseURL + "/discussion?route=" + url.QueryEscape("/guide/")
	anonDiscussion := mustGet(t, testHTTPClient(), discussionURL)
	if anonDiscussion.StatusCode != http.StatusOK {
		t.Fatalf("anonymous discussion status = %d", anonDiscussion.StatusCode)
	}
	anonDiscussion.Body.Close()

	editorDiscussionCSRF := fetchCSRFToken(t, editor, discussionURL)
	addResp := postForm(t, editor, baseURL+"/discussion/add", url.Values{
		"csrf_token": {editorDiscussionCSRF},
		"route":      {"/guide/"},
		"body":       {"first discussion message"},
	})
	if addResp.StatusCode != http.StatusSeeOther {
		t.Fatalf("add comment status = %d", addResp.StatusCode)
	}
	addResp.Body.Close()

	discussionResp := mustGet(t, editor, discussionURL)
	if body := readResponse(t, discussionResp); !strings.Contains(body, "first discussion message") {
		t.Fatal("comment not shown")
	}

	comments, err := data.ListComments(context.Background(), "/guide/", true)
	if err != nil || len(comments) != 1 {
		t.Fatalf("comments = %d, %v", len(comments), err)
	}
	adminDiscussionCSRF := fetchCSRFToken(t, admin, discussionURL)
	hideResp := postForm(t, admin, baseURL+"/discussion/hide", url.Values{
		"csrf_token": {adminDiscussionCSRF},
		"id":         {strconvInt(comments[0].ID)},
		"hidden":     {"1"},
	})
	if hideResp.StatusCode != http.StatusSeeOther {
		t.Fatalf("hide comment status = %d", hideResp.StatusCode)
	}
	hideResp.Body.Close()

	editorView := mustGet(t, editor, discussionURL)
	if body := readResponse(t, editorView); strings.Contains(body, "first discussion message") {
		t.Fatal("hidden comment should not be visible to editors")
	}
	adminView := mustGet(t, admin, discussionURL)
	if body := readResponse(t, adminView); !strings.Contains(body, "first discussion message") {
		t.Fatal("hidden comment should stay visible to reviewers")
	}

	showResp := postForm(t, admin, baseURL+"/discussion/hide", url.Values{
		"csrf_token": {adminDiscussionCSRF},
		"id":         {strconvInt(comments[0].ID)},
		"hidden":     {"0"},
	})
	if showResp.StatusCode != http.StatusSeeOther {
		t.Fatalf("show comment status = %d", showResp.StatusCode)
	}
	showResp.Body.Close()

	emptyResp := postForm(t, editor, baseURL+"/discussion/add", url.Values{
		"csrf_token": {editorDiscussionCSRF},
		"route":      {"/guide/"},
		"body":       {"   "},
	})
	if emptyResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty comment status = %d", emptyResp.StatusCode)
	}
	emptyResp.Body.Close()

	badCSRF := postForm(t, editor, baseURL+"/discussion/add", url.Values{
		"route": {"/guide/"},
		"body":  {"no csrf"},
	})
	if badCSRF.StatusCode != http.StatusForbidden {
		t.Fatalf("comment csrf status = %d", badCSRF.StatusCode)
	}
	badCSRF.Body.Close()

	unprotectResp := postForm(t, admin, baseURL+"/protect", url.Values{
		"csrf_token": {adminCSRF},
		"route":      {"/guide/"},
		"protected":  {"0"},
	})
	if unprotectResp.StatusCode != http.StatusSeeOther {
		t.Fatalf("unprotect status = %d", unprotectResp.StatusCode)
	}
	unprotectResp.Body.Close()
	page, err = data.GetPage(context.Background(), "/guide/")
	if err != nil || page.Protected {
		t.Fatalf("protected after unprotect = %v, %v", page.Protected, err)
	}
}

func TestCommentModeration(t *testing.T) {
	baseURL, data, _ := setupCommunityAppWithComment(t, config.CommentConfig{
		RateLimit:    2,
		RateWindow:   "1m",
		BlockedWords: []string{"BadWord"},
	})
	editor := loginTestClient(t, baseURL, "editor", "editor-pass-123")
	admin := loginTestClient(t, baseURL, "admin", "admin-pass-123")
	discussionURL := baseURL + "/discussion?route=" + url.QueryEscape("/guide/")
	editorCSRF := fetchCSRFToken(t, editor, discussionURL)

	blocked := postForm(t, editor, baseURL+"/discussion/add", url.Values{
		"csrf_token": {editorCSRF},
		"route":      {"/guide/"},
		"body":       {"this contains badword"},
	})
	if blocked.StatusCode != http.StatusBadRequest {
		t.Fatalf("blocked comment status = %d", blocked.StatusCode)
	}
	blocked.Body.Close()

	for i, body := range []string{"first comment", "second comment"} {
		resp := postForm(t, editor, baseURL+"/discussion/add", url.Values{
			"csrf_token": {editorCSRF},
			"route":      {"/guide/"},
			"body":       {body},
		})
		if resp.StatusCode != http.StatusSeeOther {
			t.Fatalf("comment %d status = %d", i, resp.StatusCode)
		}
		resp.Body.Close()
	}
	limited := postForm(t, editor, baseURL+"/discussion/add", url.Values{
		"csrf_token": {editorCSRF},
		"route":      {"/guide/"},
		"body":       {"third comment"},
	})
	if limited.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("rate limit status = %d", limited.StatusCode)
	}
	limited.Body.Close()

	comments, err := data.ListComments(context.Background(), "/guide/", true)
	if err != nil || len(comments) != 2 {
		t.Fatalf("comments = %d, %v", len(comments), err)
	}
	first, second := comments[0], comments[1]
	if first.Body != "first comment" || second.Body != "second comment" {
		t.Fatalf("comment order = %+v", comments)
	}

	editorReport := postForm(t, editor, baseURL+"/discussion/report", url.Values{
		"csrf_token": {editorCSRF},
		"id":         {strconvInt(first.ID)},
		"reason":     {"self report"},
	})
	if editorReport.StatusCode != http.StatusNotFound {
		t.Fatalf("self report status = %d", editorReport.StatusCode)
	}
	editorReport.Body.Close()

	adminCSRF := fetchCSRFToken(t, admin, discussionURL)
	report := postForm(t, admin, baseURL+"/discussion/report", url.Values{
		"csrf_token": {adminCSRF},
		"id":         {strconvInt(first.ID)},
		"reason":     {"spam"},
	})
	if report.StatusCode != http.StatusSeeOther {
		t.Fatalf("report status = %d", report.StatusCode)
	}
	report.Body.Close()

	comments, err = data.ListComments(context.Background(), "/guide/", true)
	if err != nil || comments[0].Reports != 1 {
		t.Fatalf("report count = %+v, %v", comments, err)
	}
	adminView := mustGet(t, admin, discussionURL)
	if body := readResponse(t, adminView); !strings.Contains(body, "举报 1") || !strings.Contains(body, "spam") {
		t.Fatal("report count or reason not shown to reviewers")
	}

	resolve := postForm(t, admin, baseURL+"/discussion/resolve", url.Values{
		"csrf_token": {adminCSRF},
		"id":         {strconvInt(first.ID)},
	})
	if resolve.StatusCode != http.StatusSeeOther {
		t.Fatalf("resolve status = %d", resolve.StatusCode)
	}
	resolve.Body.Close()
	comments, err = data.ListComments(context.Background(), "/guide/", true)
	if err != nil || comments[0].Reports != 0 {
		t.Fatalf("reports after resolve = %+v, %v", comments, err)
	}

	reportSecond := postForm(t, admin, baseURL+"/discussion/report", url.Values{
		"csrf_token": {adminCSRF},
		"id":         {strconvInt(second.ID)},
		"reason":     {"spam"},
	})
	if reportSecond.StatusCode != http.StatusSeeOther {
		t.Fatalf("second report status = %d", reportSecond.StatusCode)
	}
	reportSecond.Body.Close()
	hide := postForm(t, admin, baseURL+"/discussion/hide", url.Values{
		"csrf_token": {adminCSRF},
		"id":         {strconvInt(second.ID)},
		"hidden":     {"1"},
	})
	if hide.StatusCode != http.StatusSeeOther {
		t.Fatalf("hide status = %d", hide.StatusCode)
	}
	hide.Body.Close()
	comments, err = data.ListComments(context.Background(), "/guide/", true)
	if err != nil || comments[1].Reports != 0 || comments[1].Status != "hidden" {
		t.Fatalf("hidden comment = %+v, %v", comments, err)
	}
	hiddenReport := postForm(t, admin, baseURL+"/discussion/report", url.Values{
		"csrf_token": {adminCSRF},
		"id":         {strconvInt(second.ID)},
		"reason":     {"hidden"},
	})
	if hiddenReport.StatusCode != http.StatusNotFound {
		t.Fatalf("hidden report status = %d", hiddenReport.StatusCode)
	}
	hiddenReport.Body.Close()
}

func TestAdminUsers(t *testing.T) {
	baseURL, data, _ := setupCommunityApp(t)
	admin := loginTestClient(t, baseURL, "admin", "admin-pass-123")
	editor := loginTestClient(t, baseURL, "editor", "editor-pass-123")
	adminUser, err := data.UserByName(context.Background(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	editorUser, err := data.UserByName(context.Background(), "editor")
	if err != nil {
		t.Fatal(err)
	}

	adminView := mustGet(t, admin, baseURL+"/admin/users")
	if body := readResponse(t, adminView); !strings.Contains(body, "用户管理") || !strings.Contains(body, "editor") {
		t.Fatal("admin users page missing users")
	}
	editorView := mustGet(t, editor, baseURL+"/admin/users")
	if editorView.StatusCode != http.StatusForbidden {
		t.Fatalf("editor admin users status = %d", editorView.StatusCode)
	}
	editorView.Body.Close()

	adminCSRF := fetchCSRFToken(t, admin, baseURL+"/admin/users")
	create := postForm(t, admin, baseURL+"/admin/users/create", url.Values{
		"csrf_token": {adminCSRF},
		"name":       {"newbie"},
		"email":      {"newbie@example.com"},
		"password":   {"newbie-pass-123"},
	})
	if create.StatusCode != http.StatusSeeOther {
		t.Fatalf("create user status = %d", create.StatusCode)
	}
	create.Body.Close()
	newbie, err := data.UserByName(context.Background(), "newbie")
	if err != nil || !slices.Contains(newbie.Groups, "user") {
		t.Fatalf("newbie = %+v, %v", newbie, err)
	}
	duplicate := postForm(t, admin, baseURL+"/admin/users/create", url.Values{
		"csrf_token": {adminCSRF},
		"name":       {"newbie"},
		"password":   {"newbie-pass-123"},
	})
	if duplicate.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate user status = %d", duplicate.StatusCode)
	}
	duplicate.Body.Close()
	shortPassword := postForm(t, admin, baseURL+"/admin/users/create", url.Values{
		"csrf_token": {adminCSRF},
		"name":       {"shorty"},
		"password":   {"short"},
	})
	if shortPassword.StatusCode != http.StatusBadRequest {
		t.Fatalf("short password status = %d", shortPassword.StatusCode)
	}
	shortPassword.Body.Close()

	disable := postForm(t, admin, baseURL+"/admin/users/status", url.Values{
		"csrf_token": {adminCSRF},
		"id":         {strconvInt(editorUser.ID)},
		"disabled":   {"1"},
	})
	if disable.StatusCode != http.StatusSeeOther {
		t.Fatalf("disable status = %d", disable.StatusCode)
	}
	disable.Body.Close()
	disabledView := mustGet(t, editor, baseURL+"/edit")
	if disabledView.StatusCode != http.StatusSeeOther {
		t.Fatalf("disabled session status = %d", disabledView.StatusCode)
	}
	disabledView.Body.Close()

	selfStatus := postForm(t, admin, baseURL+"/admin/users/status", url.Values{
		"csrf_token": {adminCSRF},
		"id":         {strconvInt(adminUser.ID)},
		"disabled":   {"1"},
	})
	if selfStatus.StatusCode != http.StatusBadRequest {
		t.Fatalf("self disable status = %d", selfStatus.StatusCode)
	}
	selfStatus.Body.Close()
	selfGroups := postForm(t, admin, baseURL+"/admin/users/groups", url.Values{
		"csrf_token": {adminCSRF},
		"id":         {strconvInt(adminUser.ID)},
		"groups":     {"editor"},
	})
	if selfGroups.StatusCode != http.StatusBadRequest {
		t.Fatalf("self demote status = %d", selfGroups.StatusCode)
	}
	selfGroups.Body.Close()

	enable := postForm(t, admin, baseURL+"/admin/users/status", url.Values{
		"csrf_token": {adminCSRF},
		"id":         {strconvInt(editorUser.ID)},
		"disabled":   {"0"},
	})
	if enable.StatusCode != http.StatusSeeOther {
		t.Fatalf("enable status = %d", enable.StatusCode)
	}
	enable.Body.Close()

	editor = loginTestClient(t, baseURL, "editor", "editor-pass-123")
	changeGroups := postForm(t, admin, baseURL+"/admin/users/groups", url.Values{
		"csrf_token": {adminCSRF},
		"id":         {strconvInt(editorUser.ID)},
		"groups":     {"user"},
	})
	if changeGroups.StatusCode != http.StatusSeeOther {
		t.Fatalf("groups status = %d", changeGroups.StatusCode)
	}
	changeGroups.Body.Close()
	noEdit := mustGet(t, editor, baseURL+"/edit")
	if noEdit.StatusCode != http.StatusForbidden {
		t.Fatalf("demoted editor status = %d", noEdit.StatusCode)
	}
	noEdit.Body.Close()

	grant := postForm(t, admin, baseURL+"/admin/users/groups", url.Values{
		"csrf_token": {adminCSRF},
		"id":         {strconvInt(editorUser.ID)},
		"groups":     {"editor", "user"},
	})
	if grant.StatusCode != http.StatusSeeOther {
		t.Fatalf("grant status = %d", grant.StatusCode)
	}
	grant.Body.Close()
	allowed := mustGet(t, editor, baseURL+"/edit")
	if allowed.StatusCode != http.StatusOK {
		t.Fatalf("restored editor status = %d", allowed.StatusCode)
	}
	allowed.Body.Close()

	audit := mustGet(t, admin, baseURL+"/admin/audit")
	if body := readResponse(t, audit); !strings.Contains(body, "审计日志") || !strings.Contains(body, "user.groups") {
		t.Fatal("audit page missing entries")
	}
	filtered := mustGet(t, admin, baseURL+"/admin/audit?q=user.groups")
	if body := readResponse(t, filtered); !strings.Contains(body, "user.groups") {
		t.Fatal("audit filter missing matching entry")
	}
	emptyAudit := mustGet(t, admin, baseURL+"/admin/audit?q=no-such-entry")
	if body := readResponse(t, emptyAudit); !strings.Contains(body, "还没有审计记录.") {
		t.Fatal("audit filter did not clear entries")
	}
	export := mustGet(t, admin, baseURL+"/admin/audit/export?q=user.groups")
	if !strings.HasPrefix(export.Header.Get("Content-Type"), "text/csv") {
		t.Fatalf("audit export type = %q", export.Header.Get("Content-Type"))
	}
	if body := readResponse(t, export); !strings.Contains(body, "user.groups") {
		t.Fatal("audit export missing entries")
	}
	editorAudit := mustGet(t, editor, baseURL+"/admin/audit")
	if editorAudit.StatusCode != http.StatusForbidden {
		t.Fatalf("editor audit status = %d", editorAudit.StatusCode)
	}
	editorAudit.Body.Close()
}

func TestAdminReports(t *testing.T) {
	baseURL, data, _ := setupCommunityApp(t)
	admin := loginTestClient(t, baseURL, "admin", "admin-pass-123")
	editor := loginTestClient(t, baseURL, "editor", "editor-pass-123")
	discussionURL := baseURL + "/discussion?route=" + url.QueryEscape("/guide/")

	editorCSRF := fetchCSRFToken(t, editor, discussionURL)
	comment := postForm(t, editor, baseURL+"/discussion/add", url.Values{
		"csrf_token": {editorCSRF},
		"route":      {"/guide/"},
		"body":       {"reported comment"},
	})
	if comment.StatusCode != http.StatusSeeOther {
		t.Fatalf("comment status = %d", comment.StatusCode)
	}
	comment.Body.Close()
	comments, err := data.ListComments(context.Background(), "/guide/", true)
	if err != nil || len(comments) != 1 {
		t.Fatalf("comments = %d, %v", len(comments), err)
	}

	editorReports := mustGet(t, editor, baseURL+"/admin/reports")
	if editorReports.StatusCode != http.StatusForbidden {
		t.Fatalf("editor reports status = %d", editorReports.StatusCode)
	}
	editorReports.Body.Close()

	adminCSRF := fetchCSRFToken(t, admin, discussionURL)
	report := postForm(t, admin, baseURL+"/discussion/report", url.Values{
		"csrf_token": {adminCSRF},
		"id":         {strconvInt(comments[0].ID)},
		"reason":     {"spam reason"},
	})
	if report.StatusCode != http.StatusSeeOther {
		t.Fatalf("report status = %d", report.StatusCode)
	}
	report.Body.Close()

	queue := mustGet(t, admin, baseURL+"/admin/reports")
	if body := readResponse(t, queue); !strings.Contains(body, "举报管理") || !strings.Contains(body, "reported comment") || !strings.Contains(body, "spam reason") {
		t.Fatal("report queue missing content")
	}

	resolve := postForm(t, admin, baseURL+"/discussion/resolve", url.Values{
		"csrf_token": {adminCSRF},
		"id":         {strconvInt(comments[0].ID)},
		"next":       {"/admin/reports?notice=reports_resolved"},
	})
	if resolve.StatusCode != http.StatusSeeOther || !strings.Contains(resolve.Header.Get("Location"), "notice=reports_resolved") {
		t.Fatalf("resolve status = %d, location = %q", resolve.StatusCode, resolve.Header.Get("Location"))
	}
	resolve.Body.Close()
	open, err := data.ListOpenReports(context.Background(), 10)
	if err != nil || len(open) != 0 {
		t.Fatalf("open reports = %d, %v", len(open), err)
	}
	resolvedView := mustGet(t, admin, baseURL+"/admin/reports?status=resolved")
	if body := readResponse(t, resolvedView); !strings.Contains(body, "已处理") || !strings.Contains(body, "spam reason") {
		t.Fatal("resolved report history missing")
	}

	again := postForm(t, admin, baseURL+"/discussion/report", url.Values{
		"csrf_token": {adminCSRF},
		"id":         {strconvInt(comments[0].ID)},
		"reason":     {"again"},
	})
	if again.StatusCode != http.StatusSeeOther {
		t.Fatalf("second report status = %d", again.StatusCode)
	}
	again.Body.Close()
	hide := postForm(t, admin, baseURL+"/discussion/hide", url.Values{
		"csrf_token": {adminCSRF},
		"id":         {strconvInt(comments[0].ID)},
		"hidden":     {"1"},
		"next":       {"/admin/reports?notice=comment_hidden"},
	})
	if hide.StatusCode != http.StatusSeeOther || !strings.Contains(hide.Header.Get("Location"), "notice=comment_hidden") {
		t.Fatalf("hide status = %d, location = %q", hide.StatusCode, hide.Header.Get("Location"))
	}
	hide.Body.Close()
	comments, err = data.ListComments(context.Background(), "/guide/", true)
	if err != nil || len(comments) != 1 || comments[0].Status != "hidden" {
		t.Fatalf("hidden comment = %+v, %v", comments, err)
	}
	open, err = data.ListOpenReports(context.Background(), 10)
	if err != nil || len(open) != 0 {
		t.Fatalf("open reports after hide = %d, %v", len(open), err)
	}
}

func TestPasswordChange(t *testing.T) {
	baseURL, data, _ := setupCommunityApp(t)
	admin := loginTestClient(t, baseURL, "admin", "admin-pass-123")
	accountURL := baseURL + "/account"
	csrf := fetchCSRFToken(t, admin, accountURL)

	wrong := postForm(t, admin, baseURL+"/account/password", url.Values{
		"csrf_token":       {csrf},
		"current_password": {"wrong-pass-123"},
		"new_password":     {"admin-pass-456"},
		"confirm_password": {"admin-pass-456"},
	})
	if wrong.StatusCode != http.StatusBadRequest {
		t.Fatalf("wrong current status = %d", wrong.StatusCode)
	}
	wrong.Body.Close()
	mismatch := postForm(t, admin, baseURL+"/account/password", url.Values{
		"csrf_token":       {csrf},
		"current_password": {"admin-pass-123"},
		"new_password":     {"admin-pass-456"},
		"confirm_password": {"admin-pass-789"},
	})
	if mismatch.StatusCode != http.StatusBadRequest {
		t.Fatalf("mismatch status = %d", mismatch.StatusCode)
	}
	mismatch.Body.Close()

	change := postForm(t, admin, baseURL+"/account/password", url.Values{
		"csrf_token":       {csrf},
		"current_password": {"admin-pass-123"},
		"new_password":     {"admin-pass-456"},
		"confirm_password": {"admin-pass-456"},
	})
	if change.StatusCode != http.StatusSeeOther {
		t.Fatalf("change status = %d", change.StatusCode)
	}
	change.Body.Close()
	account := mustGet(t, admin, accountURL)
	if account.StatusCode != http.StatusOK {
		t.Fatalf("account after change status = %d", account.StatusCode)
	}
	account.Body.Close()
	if status := loginStatus(t, baseURL, "admin", "admin-pass-123"); status != http.StatusUnauthorized {
		t.Fatalf("old password status = %d", status)
	}
	if status := loginStatus(t, baseURL, "admin", "admin-pass-456"); status != http.StatusSeeOther {
		t.Fatalf("new password status = %d", status)
	}
	if ok, err := data.HasAudit(context.Background(), "user.password_change", "admin"); err != nil || !ok {
		t.Fatalf("password audit = %v, %v", ok, err)
	}
}

func TestAdminPasswordReset(t *testing.T) {
	baseURL, data, _ := setupCommunityApp(t)
	admin := loginTestClient(t, baseURL, "admin", "admin-pass-123")
	editor := loginTestClient(t, baseURL, "editor", "editor-pass-123")
	adminUser, err := data.UserByName(context.Background(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	editorUser, err := data.UserByName(context.Background(), "editor")
	if err != nil {
		t.Fatal(err)
	}
	csrf := fetchCSRFToken(t, admin, baseURL+"/admin/users")

	self := postForm(t, admin, baseURL+"/admin/users/password", url.Values{
		"csrf_token": {csrf},
		"id":         {strconvInt(adminUser.ID)},
		"password":   {"admin-pass-456"},
		"confirm":    {"admin-pass-456"},
	})
	if self.StatusCode != http.StatusBadRequest {
		t.Fatalf("self reset status = %d", self.StatusCode)
	}
	self.Body.Close()

	reset := postForm(t, admin, baseURL+"/admin/users/password", url.Values{
		"csrf_token": {csrf},
		"id":         {strconvInt(editorUser.ID)},
		"password":   {"editor-pass-456"},
		"confirm":    {"editor-pass-456"},
	})
	if reset.StatusCode != http.StatusSeeOther {
		t.Fatalf("reset status = %d", reset.StatusCode)
	}
	reset.Body.Close()
	oldSession := mustGet(t, editor, baseURL+"/edit")
	if oldSession.StatusCode != http.StatusSeeOther {
		t.Fatalf("old editor session status = %d", oldSession.StatusCode)
	}
	oldSession.Body.Close()
	if status := loginStatus(t, baseURL, "editor", "editor-pass-123"); status != http.StatusUnauthorized {
		t.Fatalf("old editor password status = %d", status)
	}
	loginTestClient(t, baseURL, "editor", "editor-pass-456")
	if ok, err := data.HasAudit(context.Background(), "user.password_reset", "editor"); err != nil || !ok {
		t.Fatalf("reset audit = %v, %v", ok, err)
	}
}

func TestSetPasswordForOAuthOnly(t *testing.T) {
	baseURL, data, _ := setupCommunityApp(t)
	ctx := context.Background()
	user, err := data.CreateUser(ctx, "oauthonly", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	rawToken := "test-session-token-1234567890"
	sum := sha256.Sum256([]byte(rawToken))
	if err := data.CreateSession(ctx, hex.EncodeToString(sum[:]), user.ID, "csrf", time.Now().Add(time.Hour), "ua", "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	client := testHTTPClient()
	base, err := url.Parse(baseURL)
	if err != nil {
		t.Fatal(err)
	}
	client.Jar.SetCookies(base, []*http.Cookie{{Name: auth.SessionCookieName, Value: rawToken, Path: "/"}})
	accountURL := baseURL + "/account"
	csrf := fetchCSRFToken(t, client, accountURL)
	change := postForm(t, client, baseURL+"/account/password", url.Values{
		"csrf_token":       {csrf},
		"new_password":     {"oauth-pass-123"},
		"confirm_password": {"oauth-pass-123"},
	})
	if change.StatusCode != http.StatusSeeOther {
		t.Fatalf("oauth password status = %d", change.StatusCode)
	}
	change.Body.Close()
	if status := loginStatus(t, baseURL, "oauthonly", "oauth-pass-123"); status != http.StatusSeeOther {
		t.Fatalf("oauth password login status = %d", status)
	}
}

func TestAdminGroups(t *testing.T) {
	baseURL, data, _ := setupCommunityApp(t)
	admin := loginTestClient(t, baseURL, "admin", "admin-pass-123")
	editor := loginTestClient(t, baseURL, "editor", "editor-pass-123")

	editorView := mustGet(t, editor, baseURL+"/admin/groups")
	if editorView.StatusCode != http.StatusForbidden {
		t.Fatalf("editor groups status = %d", editorView.StatusCode)
	}
	editorView.Body.Close()
	adminView := mustGet(t, admin, baseURL+"/admin/groups")
	if body := readResponse(t, adminView); !strings.Contains(body, "用户组管理") || !strings.Contains(body, "admin") {
		t.Fatal("groups page missing built-in groups")
	}

	csrf := fetchCSRFToken(t, admin, baseURL+"/admin/groups")
	create := postForm(t, admin, baseURL+"/admin/groups/create", url.Values{
		"csrf_token": {csrf},
		"name":       {"staff"},
		"rights":     {"edit", "upload"},
	})
	if create.StatusCode != http.StatusSeeOther {
		t.Fatalf("create group status = %d", create.StatusCode)
	}
	create.Body.Close()
	groups, err := data.ListGroups(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var staff *store.Group
	var adminGroup *store.Group
	for _, group := range groups {
		switch group.Name {
		case "staff":
			staff = group
		case "admin":
			adminGroup = group
		}
	}
	if staff == nil || adminGroup == nil {
		t.Fatalf("groups = %+v", groups)
	}
	editorUser, err := data.UserByName(context.Background(), "editor")
	if err != nil {
		t.Fatal(err)
	}
	assign := postForm(t, admin, baseURL+"/admin/users/groups", url.Values{
		"csrf_token": {csrf},
		"id":         {strconvInt(editorUser.ID)},
		"groups":     {"staff"},
	})
	if assign.StatusCode != http.StatusSeeOther {
		t.Fatalf("assign group status = %d", assign.StatusCode)
	}
	assign.Body.Close()
	allowed := mustGet(t, editor, baseURL+"/edit")
	if allowed.StatusCode != http.StatusOK {
		t.Fatalf("staff editor status = %d", allowed.StatusCode)
	}
	allowed.Body.Close()

	restrict := postForm(t, admin, baseURL+"/admin/groups/rights", url.Values{
		"csrf_token": {csrf},
		"id":         {strconvInt(staff.ID)},
		"rights":     {"read"},
	})
	if restrict.StatusCode != http.StatusSeeOther {
		t.Fatalf("group rights status = %d", restrict.StatusCode)
	}
	restrict.Body.Close()
	denied := mustGet(t, editor, baseURL+"/edit")
	if denied.StatusCode != http.StatusForbidden {
		t.Fatalf("restricted editor status = %d", denied.StatusCode)
	}
	denied.Body.Close()

	builtin := postForm(t, admin, baseURL+"/admin/groups/delete", url.Values{
		"csrf_token": {csrf},
		"id":         {strconvInt(adminGroup.ID)},
	})
	if builtin.StatusCode != http.StatusBadRequest {
		t.Fatalf("builtin delete status = %d", builtin.StatusCode)
	}
	builtin.Body.Close()
	remove := postForm(t, admin, baseURL+"/admin/groups/delete", url.Values{
		"csrf_token": {csrf},
		"id":         {strconvInt(staff.ID)},
	})
	if remove.StatusCode != http.StatusSeeOther {
		t.Fatalf("delete group status = %d", remove.StatusCode)
	}
	remove.Body.Close()
	groups, err = data.ListGroups(context.Background())
	if err != nil || len(groups) != 3 {
		t.Fatalf("groups after delete = %d, %v", len(groups), err)
	}
	audit := mustGet(t, admin, baseURL+"/admin/audit")
	if body := readResponse(t, audit); !strings.Contains(body, "group.create") || !strings.Contains(body, "group.delete") {
		t.Fatal("group audit entries missing")
	}
}

func TestTranslationsPage(t *testing.T) {
	baseURL, data, _ := setupCommunityApp(t)
	ctx := context.Background()
	adminUser, err := data.UserByName(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	guide, err := data.GetPage(ctx, "/guide/")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := data.CreateRevision(ctx, store.RevisionInput{
		Route:          "/guide/",
		Title:          "Guide",
		Body:           "guide base",
		AuthorID:       adminUser.ID,
		BaseVersion:    guide.Version,
		TranslationKey: "guide-start",
		Publish:        true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := data.CreateRevision(ctx, store.RevisionInput{
		Route:          "/about/",
		Title:          "About",
		Body:           "about base",
		AuthorID:       adminUser.ID,
		TranslationKey: "about-page",
		Publish:        true,
	}); err != nil {
		t.Fatal(err)
	}

	admin := loginTestClient(t, baseURL, "admin", "admin-pass-123")
	editor := loginTestClient(t, baseURL, "editor", "editor-pass-123")
	anonymous := testHTTPClient()
	if resp := mustGet(t, anonymous, baseURL+"/translations"); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("anonymous translations status = %d", resp.StatusCode)
	} else {
		resp.Body.Close()
	}
	before := mustGet(t, admin, baseURL+"/translations")
	if body := readResponse(t, before); !strings.Contains(body, "翻译管理") || !strings.Contains(body, "guide-start") || !strings.Contains(body, "未翻译") {
		t.Fatal("translation overview missing grouped pages")
	}
	prefill := mustGet(t, admin, baseURL+"/edit?route=/en/about/&translation_key=about-page")
	if body := readResponse(t, prefill); !strings.Contains(body, `value="/en/about/"`) || !strings.Contains(body, `value="about-page"`) {
		t.Fatal("translation edit prefill missing")
	}
	if resp := mustGet(t, editor, baseURL+"/translations"); resp.StatusCode != http.StatusOK {
		t.Fatalf("editor translations status = %d", resp.StatusCode)
	} else {
		resp.Body.Close()
	}

	editURL := baseURL + "/edit?route=/en/guide/&translation_key=guide-start"
	csrf := fetchCSRFToken(t, admin, editURL)
	save := postForm(t, admin, baseURL+"/edit/save", url.Values{
		"csrf_token":      {csrf},
		"route":           {"/en/guide/"},
		"title":           {"Guide EN"},
		"body":            {"english guide"},
		"translation_key": {"guide-start"},
	})
	if save.StatusCode != http.StatusSeeOther {
		t.Fatalf("translation save status = %d", save.StatusCode)
	}
	save.Body.Close()
	current := mustGet(t, admin, baseURL+"/translations")
	if body := readResponse(t, current); !strings.Contains(body, "Guide EN") || !strings.Contains(body, "已同步") {
		t.Fatal("current translation status missing")
	}

	guide, err = data.GetPage(ctx, "/guide/")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := data.CreateRevision(ctx, store.RevisionInput{
		Route:          "/guide/",
		Title:          "Guide",
		Body:           "guide updated",
		AuthorID:       adminUser.ID,
		BaseVersion:    guide.Version,
		TranslationKey: "guide-start",
		Publish:        true,
	}); err != nil {
		t.Fatal(err)
	}
	outdated := mustGet(t, admin, baseURL+"/translations")
	if body := readResponse(t, outdated); !strings.Contains(body, "可能过期") {
		t.Fatal("outdated translation status missing")
	}
}

func TestEditPreview(t *testing.T) {
	baseURL, _, _ := setupCommunityApp(t)
	admin := loginTestClient(t, baseURL, "admin", "admin-pass-123")
	csrf := fetchCSRFToken(t, admin, baseURL+"/edit")

	resp := postForm(t, admin, baseURL+"/edit/preview", url.Values{
		"csrf_token": {csrf},
		"body":       {"**bold** <script>alert(1)</script>"},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("preview status = %d", resp.StatusCode)
	}
	var result struct {
		HTML string `json:"html"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if !strings.Contains(result.HTML, "<strong>bold</strong>") || strings.Contains(result.HTML, "<script>") {
		t.Fatalf("preview html = %q", result.HTML)
	}

	noCSRF := postForm(t, admin, baseURL+"/edit/preview", url.Values{"body": {"**x**"}})
	if noCSRF.StatusCode != http.StatusForbidden {
		t.Fatalf("preview csrf status = %d", noCSRF.StatusCode)
	}
	noCSRF.Body.Close()
	anonymous := postForm(t, testHTTPClient(), baseURL+"/edit/preview", url.Values{
		"body": {"**x**"},
	})
	if anonymous.StatusCode != http.StatusUnauthorized {
		t.Fatalf("preview anonymous status = %d", anonymous.StatusCode)
	}
	anonymous.Body.Close()
}

func setupCommunityApp(t *testing.T) (string, *store.Store, *site.Site) {
	t.Helper()
	return setupCommunityAppWithComment(t, config.CommentConfig{})
}

func setupCommunityAppWithComment(t *testing.T, commentCfg config.CommentConfig) (string, *store.Store, *site.Site) {
	t.Helper()
	app, data, loaded := setupCommunityFixture(t, config.GitHubConfig{})
	app.cfg.Comment = commentCfg
	app.blockedWords = normalizeBlockedWords(commentCfg.BlockedWords)
	server := httptest.NewServer(app.Handler())
	t.Cleanup(server.Close)
	return server.URL, data, loaded
}

func setupCommunityFixture(t *testing.T, ghConfig config.GitHubConfig) (*App, *store.Store, *site.Site) {
	t.Helper()
	databaseURL := os.Getenv("WIKIX_TEST_POSTGRES_URL_SERVER")
	if databaseURL == "" {
		t.Skip("set WIKIX_TEST_POSTGRES_URL_SERVER to run server integration tests")
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
	_, err = pool.Exec(ctx, `TRUNCATE comment_reports, watchlist, comments, audit_logs, media, sessions, page_revisions, pages, entity_fields, entities, entity_types, email_tokens, invite_codes, identities, user_groups, users RESTART IDENTITY CASCADE`)
	if err == nil {
		_, err = pool.Exec(ctx, `TRUNCATE group_rights, groups RESTART IDENTITY CASCADE`)
	}
	if err == nil {
		_, err = pool.Exec(ctx, `INSERT INTO groups (name) VALUES ('admin'), ('editor'), ('user')`)
	}
	if err == nil {
		_, err = pool.Exec(ctx, `INSERT INTO group_rights (group_id, right_name)
			SELECT g.id, v.right_name FROM groups g
			JOIN (VALUES
				('admin', 'read'), ('admin', 'edit'), ('admin', 'review'), ('admin', 'upload'), ('admin', 'manage_users'),
				('editor', 'read'), ('editor', 'edit'), ('editor', 'upload'),
				('user', 'read')
			) AS v(group_name, right_name) ON v.group_name = g.name`)
	}
	pool.Close()
	if err != nil {
		t.Fatal(err)
	}

	adminHash, err := auth.HashPassword("admin-pass-123")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := data.CreateUser(ctx, "admin", "", adminHash, true); err != nil {
		t.Fatal(err)
	}
	editorHash, err := auth.HashPassword("editor-pass-123")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := data.CreateUser(ctx, "editor", "", editorHash, false); err != nil {
		t.Fatal(err)
	}
	if err := data.GrantGroup(ctx, "editor", "editor"); err != nil {
		t.Fatal(err)
	}

	pages := []*content.Page{
		{Route: "/", Title: "Home", Body: "welcome home", Version: "v-home"},
		{Route: "/guide/", Title: "Guide", Description: "guide page", Body: "old guide", Version: "v-guide"},
	}
	if _, _, err := data.ImportPages(ctx, pages, nil); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Site: config.SiteConfig{
			Title:    "Community Wiki",
			Language: "zh-CN",
			Locales: []config.LocaleConfig{
				{Code: "zh-CN", Label: "中文", Prefix: ""},
				{Code: "en", Label: "English", Prefix: "en"},
			},
		},
		Content: config.ContentConfig{
			Source:      "postgres",
			MaxFileSize: 1 << 20,
		},
		Auth: config.AuthConfig{
			SessionTTL: "1h",
		},
		Media: config.MediaConfig{
			Dir:     t.TempDir(),
			MaxSize: 1 << 20,
		},
		GitHub: ghConfig,
	}
	loaded, err := site.LoadWithSource(ctx, cfg, data)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loaded.Close() })
	authManager := auth.New(cfg.Auth, data)
	app, err := NewApp(cfg, loaded, data, authManager, nil)
	if err != nil {
		t.Fatal(err)
	}
	return app, data, loaded
}

func TestPullRequestImport(t *testing.T) {
	png, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	fakeGitHub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/pulls/42/files"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]map[string]string{
				{"filename": "content/imported.md", "status": "added"},
				{"filename": "content/removed.md", "status": "removed"},
				{"filename": "content/images/icon.png", "status": "added"},
			})
		case strings.Contains(r.URL.Path, "/contents/content/imported.md"):
			markdown := "---\ntitle: Imported\ndescription: from pr\ntags: [alpha]\n---\n\nfrom pr\n\n![icon](images/icon.png)\n"
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"type":     "file",
				"encoding": "base64",
				"content":  base64.StdEncoding.EncodeToString([]byte(markdown)),
				"size":     len(markdown),
			})
		case strings.Contains(r.URL.Path, "/contents/content/images/icon.png"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"type": "file", "sha": "blob-icon", "size": len(png)})
		case strings.HasSuffix(r.URL.Path, "/git/blobs/blob-icon"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"encoding": "base64",
				"content":  base64.StdEncoding.EncodeToString(png),
				"size":     len(png),
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer fakeGitHub.Close()

	app, data, loaded := setupCommunityFixture(t, config.GitHubConfig{
		Repo:        "owner/repo",
		Token:       "test-token",
		BaseBranch:  "main",
		ContentPath: "content",
		PRImport:    "publish",
		APIBaseURL:  fakeGitHub.URL,
	})
	ctx := context.Background()
	if _, _, err := data.ImportPages(ctx, []*content.Page{
		{Route: "/removed/", Title: "Removed", Body: "old page", Version: "old-version"},
	}, nil); err != nil {
		t.Fatal(err)
	}
	payload := githubPullRequestPayload{Number: 42}
	payload.Repository.FullName = "owner/repo"
	payload.PullRequest.Merged = true
	payload.PullRequest.MergeCommitSHA = "merge-sha"
	payload.PullRequest.Title = "Add imported page"
	payload.PullRequest.User.ID = 123
	payload.PullRequest.User.Login = "alice"

	if err := app.importPullRequest(ctx, payload); err != nil {
		t.Fatal(err)
	}
	page, err := data.GetPage(ctx, "/imported/")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(png)
	mediaName := "pr-" + hex.EncodeToString(sum[:])[:16] + ".png"
	if page.Title != "Imported" || len(page.Tags) != 1 || page.Tags[0] != "alpha" {
		t.Fatalf("imported page = %+v", page)
	}
	if !strings.Contains(page.Body, "/media/"+mediaName) {
		t.Fatalf("media path not rewritten: %s", page.Body)
	}
	if _, err := data.GetMedia(ctx, mediaName); err != nil {
		t.Fatalf("media record missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(app.cfg.Media.Dir, mediaName)); err != nil {
		t.Fatalf("media file missing: %v", err)
	}
	removedPage, err := data.GetPage(ctx, "/removed/")
	if err != nil || removedPage.DeletedAt == nil {
		t.Fatalf("removed page = %+v, %v", removedPage, err)
	}
	if _, ok := loaded.FindPage("/imported/"); !ok {
		t.Fatal("imported page not visible after reload")
	}
	if _, ok := loaded.FindPage("/removed/"); ok {
		t.Fatal("removed page should disappear after reload")
	}
	revisions, err := data.ListRevisions(ctx, "/imported/", 0, true)
	if err != nil || len(revisions) != 1 {
		t.Fatalf("revisions = %d, %v", len(revisions), err)
	}

	if err := app.importPullRequest(ctx, payload); err != nil {
		t.Fatal(err)
	}
	revisions, err = data.ListRevisions(ctx, "/imported/", 0, true)
	if err != nil || len(revisions) != 1 {
		t.Fatalf("duplicate import created revisions = %d, %v", len(revisions), err)
	}
}

func testHTTPClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{
		Jar: jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func loginTestClient(t *testing.T, baseURL, username, password string) *http.Client {
	t.Helper()
	client := testHTTPClient()
	token := fetchCSRFToken(t, client, baseURL+"/login")
	resp := postForm(t, client, baseURL+"/login", url.Values{
		"csrf_token": {token},
		"username":   {username},
		"password":   {password},
		"next":       {"/"},
	})
	if resp.StatusCode != http.StatusSeeOther {
		body := readResponse(t, resp)
		t.Fatalf("login %s status = %d, body = %s", username, resp.StatusCode, body)
	}
	resp.Body.Close()
	return client
}

func loginStatus(t *testing.T, baseURL, username, password string) int {
	t.Helper()
	client := testHTTPClient()
	token := fetchCSRFToken(t, client, baseURL+"/login")
	resp := postForm(t, client, baseURL+"/login", url.Values{
		"csrf_token": {token},
		"username":   {username},
		"password":   {password},
		"next":       {"/"},
	})
	defer resp.Body.Close()
	return resp.StatusCode
}

func fetchCSRFToken(t *testing.T, client *http.Client, target string) string {
	t.Helper()
	resp := mustGet(t, client, target)
	body := readResponse(t, resp)
	match := csrfPattern.FindStringSubmatch(body)
	if len(match) < 2 {
		t.Fatalf("csrf token not found at %s", target)
	}
	return match[1]
}

func mustGet(t *testing.T, client *http.Client, target string) *http.Response {
	t.Helper()
	resp, err := client.Get(target)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func readResponse(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func postForm(t *testing.T, client *http.Client, target string, form url.Values) *http.Response {
	t.Helper()
	resp, err := client.PostForm(target, form)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func uploadTestFile(t *testing.T, client *http.Client, baseURL, csrf string, data []byte) *http.Response {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "test.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("csrf_token", csrf); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, baseURL+"/upload", &body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func strconvInt(value int64) string {
	return strconv.FormatInt(value, 10)
}
