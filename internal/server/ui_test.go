package server

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/EggyFly/Wiki-X/internal/i18n"
	"github.com/EggyFly/Wiki-X/internal/site"
	"github.com/EggyFly/Wiki-X/internal/store"
)

func TestCSVSafe(t *testing.T) {
	cases := map[string]string{
		"":       "",
		"normal": "normal",
		"=cmd":   "'=cmd",
		"+1":     "'+1",
		"-1":     "'-1",
		"@name":  "'@name",
	}
	for input, want := range cases {
		if got := csvSafe(input); got != want {
			t.Fatalf("csvSafe(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSafeNext(t *testing.T) {
	cases := map[string]string{
		"":                 "/",
		"/guide/":          "/guide/",
		"/search?q=x":      "/search?q=x",
		"//evil.example/":  "/",
		"https://evil.com": "/",
		"/back\\slash":     "/",
	}
	for input, want := range cases {
		if got := safeNext(input); got != want {
			t.Fatalf("safeNext(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestUIRender(t *testing.T) {
	ui, err := loadUI()
	if err != nil {
		t.Fatal(err)
	}
	base := baseData{
		Site:          site.SiteInfo{Title: "Test Wiki", Year: 2026},
		Title:         "Test",
		Lang:          "zh-CN",
		LoginEnabled:  true,
		GitHubEnabled: false,
		UIAssets:      uiAssets{CSS: "app~12345678.css", JS: "app~12345678.js"},
		T:             i18n.For("zh-CN"),
	}
	now := time.Now()
	cases := []struct {
		name string
		data any
		want string
	}{
		{"login", loginData{baseData: base, HasUsers: true}, "登录"},
		{"verify", verifyData{baseData: base}, "两步验证"},
		{"edit", editData{baseData: base, Route: "/x/", PageTitle: "X", Body: "body"}, "保存"},
		{"edit", editData{baseData: base, Route: "/x/", PageTitle: "X", Body: "body"}, "preview-toggle"},
		{"edit", editData{baseData: base, Route: "/x/", PageTitle: "X", Body: "body"}, `data-md="strike"`},
		{"edit", editData{baseData: base, Route: "/x/", PageTitle: "X", Body: "body"}, `data-md="task"`},
		{"edit", editData{baseData: base, Route: "/x/", PageTitle: "X", Body: "body"}, `data-md="hr"`},
		{"edit", editData{baseData: base, Route: "/x/", PageTitle: "X", Body: "body"}, `data-chars-label="字符数"`},
		{"edit", editData{baseData: base, Route: "/x/", PageTitle: "X", Body: "body"}, `data-loading-label="预览加载中"`},
		{"login", loginData{baseData: base, HasUsers: true}, `/-/assets/app~`},
		{"history", historyData{baseData: base, Route: "/x/", Revisions: []*store.Revision{{ID: 1, Route: "/x/", Status: "published", CreatedAt: time.Now()}}}, "历史版本"},
		{"revision", revisionData{baseData: base, Revision: &store.Revision{ID: 1, Route: "/x/", Status: "published", CreatedAt: time.Now()}, Diff: "diff"}, "版本 #1"},
		{"review", reviewData{baseData: base}, "待审核"},
		{"search", searchData{baseData: base, Query: "x", Results: []site.SearchResult{{Route: "/x/", Title: "X", Snippet: "s"}}}, "1 条结果"},
		{"message", messageData{baseData: base, Heading: "Oops", Message: "bad"}, "Oops"},
		{"pr", prData{baseData: base, URL: "https://github.com/owner/repo/pull/1", Number: 1, Route: "/"}, "PR 已创建"},
		{"watchlist", watchlistData{baseData: base}, "监视列表"},
		{"discussion", discussionData{baseData: base, Route: "/x/", PageTitle: "X"}, "讨论"},
		{"admin_users", adminUsersData{baseData: base, Users: []adminUserRow{{
			User:   &store.User{ID: 1, Name: "admin", Groups: []string{"admin"}, CreatedAt: now},
			IsSelf: true,
			Choices: []groupChoice{
				{Name: "admin", Selected: true},
				{Name: "user"},
			},
		}}}, "用户管理"},
		{"admin_reports", adminReportsData{baseData: base, Reports: []*store.CommentReport{{
			ID: 1, CommentID: 2, Route: "/x/", Body: "bad comment", ReporterName: "reporter", Reason: "spam", CreatedAt: now,
		}}}, "举报管理"},
		{"admin_audit", adminAuditData{baseData: base, Logs: []*store.AuditLog{{
			ID: 1, ActorName: "admin", Action: "user.groups", Target: "editor", Detail: "editor,user", CreatedAt: now,
		}}}, "审计日志"},
		{"admin_groups", adminGroupsData{baseData: base, Rights: []string{"read", "edit"}, Groups: []adminGroupRow{{
			Group: &store.Group{ID: 2, Name: "staff", Rights: []string{"edit"}, UserCount: 2},
			Rights: []groupRightChoice{
				{Name: "read"},
				{Name: "edit", Selected: true},
			},
		}}}, "用户组管理"},
		{"translations", translationsData{baseData: base, Rows: []translationRow{{
			Key: "guide", DefaultTitle: "指南", DefaultRoute: "/guide/",
			Locales: []translationLocaleCell{
				{Code: "zh-CN", Label: "中文", Route: "/guide/", Title: "指南", EditURL: "/edit?route=/guide/"},
				{Code: "en", Label: "English", Missing: true, EditURL: "/edit?route=/en/guide/&translation_key=guide"},
			},
		}}}, "翻译管理"},
	}
	for _, tc := range cases {
		var buf bytes.Buffer
		if err := ui.render(&buf, tc.name, tc.data); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if !strings.Contains(buf.String(), tc.want) {
			t.Fatalf("%s: missing %q", tc.name, tc.want)
		}
	}

	var gitWatch bytes.Buffer
	if err := ui.render(&gitWatch, "watchlist", watchlistData{
		baseData: base,
		Pages:    []*store.WatchedPage{{Route: "/x/", Title: "X", Source: "git", ChangedAt: &now}},
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gitWatch.String(), "Git 仓库") {
		t.Fatal("git watchlist source missing")
	}
}

func TestAccountRender(t *testing.T) {
	ui, err := loadUI()
	if err != nil {
		t.Fatal(err)
	}
	base := baseData{
		Site:          site.SiteInfo{Title: "Test Wiki", Year: 2026},
		User:          &store.User{Name: "admin", Email: "admin@example.com"},
		Title:         "账号",
		Lang:          "zh-CN",
		LoginEnabled:  true,
		GitHubEnabled: true,
		T:             i18n.For("zh-CN"),
	}
	var buf bytes.Buffer
	if err := ui.render(&buf, "account", accountData{
		baseData:   base,
		Identities: []*store.Identity{{Provider: "github", Username: "alice"}},
		HasGitHub:  true,
	}); err != nil {
		t.Fatal(err)
	}
	html := buf.String()
	for _, want := range []string{"账号", "GitHub", "alice", "已绑定"} {
		if !strings.Contains(html, want) {
			t.Fatalf("missing %q", want)
		}
	}
}

func TestUIRenderEnglish(t *testing.T) {
	ui, err := loadUI()
	if err != nil {
		t.Fatal(err)
	}
	base := baseData{
		Site:         site.SiteInfo{Title: "Test Wiki", Year: 2026},
		Title:        "Log in",
		Lang:         "en",
		LoginEnabled: true,
		T:            i18n.For("en"),
	}
	var buf bytes.Buffer
	if err := ui.render(&buf, "login", loginData{baseData: base, HasUsers: true}); err != nil {
		t.Fatal(err)
	}
	html := buf.String()
	for _, want := range []string{"Log in", "Username", "Password"} {
		if !strings.Contains(html, want) {
			t.Fatalf("missing %q", want)
		}
	}
	if strings.Contains(html, "用户管理") {
		t.Fatal("chinese label leaked into english page")
	}
}
