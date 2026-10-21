package store

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/EggyFly/Wiki-X/internal/config"
	"github.com/EggyFly/Wiki-X/internal/content"
)

func TestStoreFlow(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	admin, err := s.CreateUser(ctx, "admin", "admin@example.com", "hash", true)
	if err != nil {
		t.Fatal(err)
	}
	if !admin.HasRight("review") || !admin.HasRight("edit") {
		t.Fatalf("admin rights = %v", admin.Rights)
	}

	ghUser, err := s.FindOrCreateGitHubUser(ctx, "123", "Alice", "alice@example.com")
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.FindOrCreateGitHubUser(ctx, "123", "Alice", "alice@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if ghUser.ID != again.ID {
		t.Fatalf("github user changed id: %d != %d", ghUser.ID, again.ID)
	}
	if ghUser.Name != "gh_alice" {
		t.Fatalf("github user name = %q", ghUser.Name)
	}

	pages := []*content.Page{
		{Route: "/", Title: "Home", Body: "hello", Version: "v1", Updated: "2026-10-20"},
		{Route: "/guide/", Title: "Guide", Body: "guide", Version: "v2", Tags: []string{"docs"}},
		{Route: "/draft/", Title: "Draft", Body: "draft", Draft: true},
	}
	created, updated, err := s.ImportPages(ctx, pages, nil)
	if err != nil {
		t.Fatal(err)
	}
	if created != 2 || updated != 0 {
		t.Fatalf("import = %d, %d", created, updated)
	}
	loaded, err := s.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 2 {
		t.Fatalf("loaded pages = %d", len(loaded))
	}

	page, err := s.GetPage(ctx, "/")
	if err != nil {
		t.Fatal(err)
	}
	if page.Title != "Home" || page.Version != "v1" {
		t.Fatalf("page = %+v", page)
	}

	revision, err := s.CreateRevision(ctx, RevisionInput{
		Route:       "/",
		Title:       "Home",
		Description: "updated",
		Body:        "hello v2",
		AuthorID:    ghUser.ID,
		Comment:     "update",
		BaseVersion: page.Version,
	})
	if err != nil {
		t.Fatal(err)
	}
	if revision.Status != StatusPending {
		t.Fatalf("status = %q", revision.Status)
	}
	if _, err := s.CreateRevision(ctx, RevisionInput{
		Route:       "/",
		Title:       "Home",
		Body:        "stale",
		AuthorID:    ghUser.ID,
		BaseVersion: "old",
	}); !errors.Is(err, ErrEditConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}

	pending, err := s.ListPendingRevisions(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].ID != revision.ID {
		t.Fatalf("pending = %+v", pending)
	}

	route, err := s.PublishRevision(ctx, revision.ID, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if route != "/" {
		t.Fatalf("route = %q", route)
	}
	page, err = s.GetPage(ctx, "/")
	if err != nil {
		t.Fatal(err)
	}
	if page.Body != "hello v2" || page.Description != "updated" {
		t.Fatalf("published page = %+v", page)
	}
	if _, err := s.PublishRevision(ctx, revision.ID, admin.ID); !errors.Is(err, ErrEditConflict) {
		t.Fatalf("expected state conflict, got %v", err)
	}

	revisions, err := s.ListRevisions(ctx, "/", ghUser.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(revisions) == 0 || revisions[0].Status != StatusPublished {
		t.Fatalf("revisions = %+v", revisions)
	}

	if err := s.CreateSession(ctx, "tokenhash", ghUser.ID, "csrf", time.Now().Add(time.Hour), "ua", "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	session, err := s.GetSession(ctx, "tokenhash")
	if err != nil {
		t.Fatal(err)
	}
	if session.User.ID != ghUser.ID || session.CSRFToken != "csrf" {
		t.Fatalf("session = %+v", session)
	}
	if err := s.DeleteSession(ctx, "tokenhash"); err != nil {
		t.Fatal(err)
	}

	media, err := s.CreateMedia(ctx, "file.png", "image/png", 42, ghUser.ID)
	if err != nil {
		t.Fatal(err)
	}
	gotMedia, err := s.GetMedia(ctx, media.FileName)
	if err != nil || gotMedia.Size != 42 {
		t.Fatalf("media = %+v, %v", gotMedia, err)
	}
}

func TestTranslationKeyPersists(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	admin, err := s.CreateUser(ctx, "admin", "", "hash", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ImportPages(ctx, []*content.Page{{
		Route:          "/guide/",
		Title:          "Guide",
		Body:           "body",
		Version:        "v1",
		TranslationKey: "guide-start",
	}}, nil); err != nil {
		t.Fatal(err)
	}
	page, err := s.GetPage(ctx, "/guide/")
	if err != nil || page.TranslationKey != "guide-start" {
		t.Fatalf("page = %+v, %v", page, err)
	}
	revision, err := s.CreateRevision(ctx, RevisionInput{
		Route:          "/guide/",
		Title:          "Guide",
		Body:           "body2",
		AuthorID:       admin.ID,
		BaseVersion:    page.Version,
		TranslationKey: "guide-start",
		Publish:        true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if revision.TranslationKey != "guide-start" {
		t.Fatalf("revision key = %q", revision.TranslationKey)
	}
	page, err = s.GetPage(ctx, "/guide/")
	if err != nil || page.TranslationKey != "guide-start" {
		t.Fatalf("published page = %+v, %v", page, err)
	}
}

func TestPageProtection(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	admin, err := s.CreateUser(ctx, "admin", "", "hash", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ImportPages(ctx, []*content.Page{{
		Route:   "/guide/",
		Title:   "Guide",
		Body:    "body",
		Version: "v1",
	}}, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPageProtected(ctx, "/guide/", true, admin.ID); err != nil {
		t.Fatal(err)
	}
	page, err := s.GetPage(ctx, "/guide/")
	if err != nil || !page.Protected || page.ProtectionLevel != ProtectionSemi {
		t.Fatalf("page = %+v, %v", page, err)
	}
	if _, err := s.CreateRevision(ctx, RevisionInput{
		Route:       "/guide/",
		Title:       "Guide",
		Body:        "edit request",
		AuthorID:    admin.ID,
		BaseVersion: page.Version,
	}); err != nil {
		t.Fatalf("semi-protected page should accept pending requests: %v", err)
	}
	if err := s.SetPageProtection(ctx, "/guide/", ProtectionFull, admin.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateRevision(ctx, RevisionInput{
		Route:       "/guide/",
		Title:       "Guide",
		Body:        "blocked",
		AuthorID:    admin.ID,
		BaseVersion: page.Version,
	}); !errors.Is(err, ErrProtected) {
		t.Fatalf("expected protected, got %v", err)
	}
	if _, err := s.CreateRevision(ctx, RevisionInput{
		Route:          "/guide/",
		Title:          "Guide",
		Body:           "allowed",
		AuthorID:       admin.ID,
		BaseVersion:    page.Version,
		Publish:        true,
		AllowProtected: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPageProtection(ctx, "/guide/", "bogus", admin.ID); !errors.Is(err, ErrBadProtection) {
		t.Fatalf("bad level error = %v", err)
	}
	if err := s.SetPageProtected(ctx, "/guide/", false, admin.ID); err != nil {
		t.Fatal(err)
	}
	page, err = s.GetPage(ctx, "/guide/")
	if err != nil || page.Protected || page.ProtectionLevel != ProtectionNone {
		t.Fatalf("unprotected page = %+v, %v", page, err)
	}
	if _, err := s.CreateRevision(ctx, RevisionInput{
		Route:       "/guide/",
		Title:       "Guide",
		Body:        "after unprotect",
		AuthorID:    admin.ID,
		BaseVersion: page.Version,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestCommentReports(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	author, err := s.CreateUser(ctx, "author", "", "hash", false)
	if err != nil {
		t.Fatal(err)
	}
	reporter, err := s.CreateUser(ctx, "reporter", "", "hash", false)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := s.CreateUser(ctx, "admin", "", "hash", true)
	if err != nil {
		t.Fatal(err)
	}

	comment, err := s.AddComment(ctx, "/guide/", author.ID, "hello")
	if err != nil {
		t.Fatal(err)
	}
	route, err := s.ReportComment(ctx, comment.ID, reporter.ID, "spam")
	if err != nil || route != "/guide/" {
		t.Fatalf("report = %q, %v", route, err)
	}
	if _, err := s.ReportComment(ctx, comment.ID, reporter.ID, "spam again"); err != nil {
		t.Fatal(err)
	}
	comments, err := s.ListComments(ctx, "/guide/", false)
	if err != nil || len(comments) != 1 || comments[0].Reports != 1 {
		t.Fatalf("comments = %+v, %v", comments, err)
	}
	if len(comments[0].ReportReasons) != 1 || comments[0].ReportReasons[0] != "spam again" {
		t.Fatalf("report reasons = %+v", comments[0].ReportReasons)
	}
	if _, err := s.ReportComment(ctx, comment.ID, author.ID, "self"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("self report = %v", err)
	}
	if _, err := s.ResolveCommentReports(ctx, comment.ID, admin.ID); err != nil {
		t.Fatal(err)
	}
	comments, err = s.ListComments(ctx, "/guide/", false)
	if err != nil || comments[0].Reports != 0 {
		t.Fatalf("reports after resolve = %+v, %v", comments, err)
	}
	if _, err := s.ReportComment(ctx, comment.ID, reporter.ID, "reopen"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.HideComment(ctx, comment.ID, admin.ID, true); err != nil {
		t.Fatal(err)
	}
	comments, err = s.ListComments(ctx, "/guide/", true)
	if err != nil || comments[0].Reports != 0 {
		t.Fatalf("reports after hide = %+v, %v", comments, err)
	}
	if _, err := s.ReportComment(ctx, comment.ID, reporter.ID, "hidden"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("hidden report = %v", err)
	}
	if comments, err = s.ListComments(ctx, "/guide/", false); err != nil || len(comments) != 0 {
		t.Fatalf("visible comments = %+v, %v", comments, err)
	}
	if comments, err = s.ListComments(ctx, "/guide/", true); err != nil || len(comments) != 1 || comments[0].Status != "hidden" {
		t.Fatalf("review comments = %+v, %v", comments, err)
	}
	resolved, err := s.ListReports(ctx, "resolved", 10, 0)
	if err != nil || len(resolved) != 1 || resolved[0].Status != "resolved" || resolved[0].ResolvedByName != "admin" {
		t.Fatalf("resolved reports = %+v, %v", resolved, err)
	}
	openCount, err := s.CountReports(ctx, "open")
	if err != nil || openCount != 0 {
		t.Fatalf("open report count = %d, %v", openCount, err)
	}
}

func TestUserAdmin(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	admin, err := s.CreateUser(ctx, "admin", "", "hash", true)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.CreateUser(ctx, "second", "", "hash", true)
	if err != nil {
		t.Fatal(err)
	}
	editor, err := s.CreateUser(ctx, "editor", "", "hash", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateUser(ctx, "admin", "", "hash", false); !errors.Is(err, ErrUserExists) {
		t.Fatalf("duplicate user = %v", err)
	}
	if err := s.GrantGroup(ctx, "editor", "editor"); err != nil {
		t.Fatal(err)
	}

	users, err := s.ListUsers(ctx, 10, 0)
	if err != nil || len(users) != 3 {
		t.Fatalf("users = %d, %v", len(users), err)
	}
	pageTwo, err := s.ListUsers(ctx, 2, 2)
	if err != nil || len(pageTwo) != 1 {
		t.Fatalf("user page two = %d, %v", len(pageTwo), err)
	}
	editor, err = s.UserByName(ctx, "editor")
	if err != nil || !containsString(editor.Groups, "editor") || !containsString(editor.Groups, "user") {
		t.Fatalf("editor groups = %+v, %v", editor, err)
	}

	if err := s.SetUserGroups(ctx, second.ID, []string{"user"}, admin.ID); err != nil {
		t.Fatal(err)
	}
	second, err = s.UserByID(ctx, second.ID)
	if err != nil || len(second.Groups) != 1 || second.Groups[0] != "user" {
		t.Fatalf("second groups = %+v, %v", second, err)
	}
	if err := s.SetUserGroups(ctx, admin.ID, []string{"editor"}, admin.ID); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("last admin demote = %v", err)
	}
	if err := s.SetUserGroups(ctx, editor.ID, []string{"unknown"}, admin.ID); !errors.Is(err, ErrBadGroup) {
		t.Fatalf("bad group = %v", err)
	}

	if err := s.CreateSession(ctx, "editor-session", editor.ID, "csrf", time.Now().Add(time.Hour), "ua", "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetUserDisabled(ctx, editor.ID, true, admin.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetSession(ctx, "editor-session"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("disabled session = %v", err)
	}
	if err := s.SetUserDisabled(ctx, admin.ID, true, admin.ID); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("last admin disable = %v", err)
	}
	if ok, err := s.HasAudit(ctx, "user.disable", "editor"); err != nil || !ok {
		t.Fatalf("disable audit = %v, %v", ok, err)
	}
	if err := s.SetUserDisabled(ctx, editor.ID, false, admin.ID); err != nil {
		t.Fatal(err)
	}
	editor, err = s.UserByID(ctx, editor.ID)
	if err != nil || editor.Disabled {
		t.Fatalf("enabled user = %+v, %v", editor, err)
	}
	if err := s.CreateSession(ctx, "password-session", editor.ID, "csrf", time.Now().Add(time.Hour), "ua", "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdatePassword(ctx, editor.ID, "new-hash", "user.password_change", editor.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetSession(ctx, "password-session"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("password session = %v", err)
	}
	editor, err = s.UserByID(ctx, editor.ID)
	if err != nil || editor.PasswordHash != "new-hash" {
		t.Fatalf("password user = %+v, %v", editor, err)
	}
	logs, err := s.ListAuditLogs(ctx, AuditFilter{}, 10, 0)
	if err != nil || len(logs) == 0 {
		t.Fatalf("audit logs = %d, %v", len(logs), err)
	}
	if count, err := s.CountAuditLogs(ctx, AuditFilter{}); err != nil || count == 0 {
		t.Fatalf("audit count = %d, %v", count, err)
	}
	filtered, err := s.ListAuditLogs(ctx, AuditFilter{Query: "password_change"}, 10, 0)
	if err != nil || len(filtered) == 0 {
		t.Fatalf("filtered audit logs = %d, %v", len(filtered), err)
	}
	empty, err := s.ListAuditLogs(ctx, AuditFilter{Query: "no-such-action"}, 10, 0)
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty audit logs = %d, %v", len(empty), err)
	}
	today := time.Now()
	from := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.Local)
	to := from.AddDate(0, 0, 1)
	dated, err := s.ListAuditLogs(ctx, AuditFilter{From: &from, To: &to}, 10, 0)
	if err != nil || len(dated) == 0 {
		t.Fatalf("dated audit logs = %d, %v", len(dated), err)
	}
	future := from.AddDate(0, 0, 7)
	futureTo := future.AddDate(0, 0, 1)
	none, err := s.ListAuditLogs(ctx, AuditFilter{From: &future, To: &futureTo}, 10, 0)
	if err != nil || len(none) != 0 {
		t.Fatalf("future audit logs = %d, %v", len(none), err)
	}
}

func TestGroupAdmin(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	admin, err := s.CreateUser(ctx, "admin", "", "hash", true)
	if err != nil {
		t.Fatal(err)
	}
	groups, err := s.ListGroups(ctx)
	if err != nil || len(groups) != 3 {
		t.Fatalf("groups = %d, %v", len(groups), err)
	}
	var adminGroup *Group
	for _, group := range groups {
		if group.Name == "admin" {
			adminGroup = group
		}
	}
	if adminGroup == nil || !adminGroup.Builtin || !containsString(adminGroup.Rights, RightManageUsers) {
		t.Fatalf("admin group = %+v", adminGroup)
	}

	if err := s.CreateGroup(ctx, "staff", []string{"edit", "upload"}, admin.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateGroup(ctx, "staff", []string{"read"}, admin.ID); !errors.Is(err, ErrGroupExists) {
		t.Fatalf("duplicate group = %v", err)
	}
	if err := s.CreateGroup(ctx, "bad/name", []string{"read"}, admin.ID); !errors.Is(err, ErrBadGroup) {
		t.Fatalf("bad group name = %v", err)
	}
	if err := s.CreateGroup(ctx, "badrights", []string{"unknown"}, admin.ID); !errors.Is(err, ErrBadRight) {
		t.Fatalf("bad right = %v", err)
	}
	groups, err = s.ListGroups(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var staff *Group
	for _, group := range groups {
		if group.Name == "staff" {
			staff = group
		}
	}
	if staff == nil || staff.Builtin || !containsString(staff.Rights, "edit") {
		t.Fatalf("staff group = %+v", staff)
	}
	if err := s.SetGroupRights(ctx, staff.ID, []string{"read", "review"}, admin.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.SetUserGroups(ctx, admin.ID, []string{"admin", "staff"}, admin.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.SetGroupRights(ctx, adminGroup.ID, []string{"read"}, admin.ID); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("admin group last manager = %v", err)
	}
	if err := s.SetGroupRights(ctx, staff.ID, []string{"read", "review", "manage_users"}, admin.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.SetGroupRights(ctx, adminGroup.ID, []string{"read"}, admin.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteGroup(ctx, staff.ID, admin.ID); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("delete last manager group = %v", err)
	}
	if err := s.SetGroupRights(ctx, adminGroup.ID, []string{"read", "manage_users"}, admin.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteGroup(ctx, staff.ID, admin.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteGroup(ctx, adminGroup.ID, admin.ID); !errors.Is(err, ErrBuiltinGroup) {
		t.Fatalf("builtin delete = %v", err)
	}
	if err := s.SetUserDisabled(ctx, admin.ID, true, admin.ID); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("disable last manager = %v", err)
	}
}

func TestTranslationBaseVersion(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	admin, err := s.CreateUser(ctx, "admin", "", "hash", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ImportPages(ctx, []*content.Page{{
		Route: "/guide/", Title: "Guide", Body: "base", Version: "v1",
	}}, nil); err != nil {
		t.Fatal(err)
	}
	page, err := s.GetPage(ctx, "/guide/")
	if err != nil {
		t.Fatal(err)
	}
	baseRevision, err := s.CreateRevision(ctx, RevisionInput{
		Route:          "/guide/",
		Title:          "Guide",
		Body:           "base v2",
		AuthorID:       admin.ID,
		BaseVersion:    page.Version,
		TranslationKey: "guide-start",
		Publish:        true,
	})
	if err != nil {
		t.Fatal(err)
	}
	page, err = s.GetPage(ctx, "/guide/")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateRevision(ctx, RevisionInput{
		Route:                  "/en/guide/",
		Title:                  "Guide EN",
		Body:                   "english",
		AuthorID:               admin.ID,
		TranslationKey:         "guide-start",
		TranslationBaseVersion: page.Version,
		Publish:                true,
	}); err != nil {
		t.Fatal(err)
	}
	translated, err := s.GetPage(ctx, "/en/guide/")
	if err != nil || translated.TranslationBaseVersion != page.Version {
		t.Fatalf("translated page = %+v, %v", translated, err)
	}
	if baseRevision.TranslationBaseVersion != "" {
		t.Fatalf("default base version = %q", baseRevision.TranslationBaseVersion)
	}
	groups, total, err := s.ListTranslationGroups(ctx, 10, 0)
	if err != nil || total != 1 || len(groups) != 1 || len(groups[0].Pages) != 2 {
		t.Fatalf("translation groups = %+v, total %d, %v", groups, total, err)
	}
	revisions, err := s.ListRevisions(ctx, "/en/guide/", admin.ID, true)
	if err != nil || len(revisions) == 0 || revisions[0].TranslationBaseVersion != page.Version {
		t.Fatalf("translation revisions = %+v, %v", revisions, err)
	}
}

func TestLinkIdentity(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	first, err := s.CreateUser(ctx, "first", "", "hash", false)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.CreateUser(ctx, "second", "", "hash", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.LinkIdentity(ctx, first.ID, "github", "42", "alice", "alice@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := s.LinkIdentity(ctx, first.ID, "github", "42", "alice", "alice@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := s.LinkIdentity(ctx, second.ID, "github", "42", "alice", "alice@example.com"); !errors.Is(err, ErrIdentityTaken) {
		t.Fatalf("expected identity taken, got %v", err)
	}
	identities, err := s.ListIdentities(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(identities) != 1 || identities[0].Username != "alice" {
		t.Fatalf("identities = %+v", identities)
	}
}

func TestRejectDraftDeletesPage(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	admin, err := s.CreateUser(ctx, "admin", "", "hash", true)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := s.CreateRevision(ctx, RevisionInput{
		Route:       "/new-page/",
		Title:       "New",
		Body:        "new body",
		AuthorID:    admin.ID,
		Description: "new",
	})
	if err != nil {
		t.Fatal(err)
	}
	page, err := s.GetPage(ctx, "/new-page/")
	if err != nil || !page.Draft {
		t.Fatalf("draft page = %+v, %v", page, err)
	}
	if _, err := s.RejectRevision(ctx, revision.ID, admin.ID, "not needed"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetPage(ctx, "/new-page/"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected draft page removed, got %v", err)
	}
}

func TestPublishStaleRevisionConflicts(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	admin, err := s.CreateUser(ctx, "admin", "", "hash", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ImportPages(ctx, []*content.Page{{Route: "/", Title: "Home", Body: "v1", Version: "v1"}}, nil); err != nil {
		t.Fatal(err)
	}
	page, err := s.GetPage(ctx, "/")
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.CreateRevision(ctx, RevisionInput{Route: "/", Title: "Home", Body: "v2", AuthorID: admin.ID, BaseVersion: page.Version})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.CreateRevision(ctx, RevisionInput{Route: "/", Title: "Home", Body: "v3", AuthorID: admin.ID, BaseVersion: page.Version})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.PublishRevision(ctx, first.ID, admin.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PublishRevision(ctx, second.ID, admin.ID); !errors.Is(err, ErrEditConflict) {
		t.Fatalf("expected stale publish conflict, got %v", err)
	}
}

func openTestStore(t *testing.T) *Store {
	t.Helper()
	url := os.Getenv("WIKIX_TEST_POSTGRES_URL")
	if url == "" {
		t.Skip("set WIKIX_TEST_POSTGRES_URL to run postgres integration tests")
	}
	ctx := context.Background()
	s, err := Open(ctx, config.DatabaseConfig{URL: url, MaxConns: 4})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	_, err = s.pool.Exec(ctx, `TRUNCATE comment_reports, watchlist, comments, audit_logs, media, sessions, page_revisions, pages, entity_fields, entities, entity_types, email_tokens, invite_codes, identities, user_groups, users RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `TRUNCATE group_rights, groups RESTART IDENTITY CASCADE`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO groups (name) VALUES ('admin'), ('editor'), ('user')`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO group_rights (group_id, right_name)
		SELECT g.id, v.right_name FROM groups g
		JOIN (VALUES
			('admin', 'read'), ('admin', 'edit'), ('admin', 'review'), ('admin', 'upload'), ('admin', 'manage_users'),
			('editor', 'read'), ('editor', 'edit'), ('editor', 'upload'),
			('user', 'read')
		) AS v(group_name, right_name) ON v.group_name = g.name`); err != nil {
		t.Fatal(err)
	}
	return s
}
