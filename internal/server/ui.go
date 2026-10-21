package server

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"sort"

	"wikix/internal/site"
	"wikix/internal/store"
)

//go:embed ui/*.html ui/assets/*
var uiFS embed.FS

type appUI struct {
	pages   map[string]*template.Template
	assets  fs.FS
	version string
}

type baseData struct {
	Site          site.SiteInfo
	User          *store.User
	CSRF          string
	Title         string
	Lang          string
	Path          string
	LoginEnabled  bool
	GitHubEnabled bool
	Notice        string
	Error         string
	UIVersion     string
	T             map[string]string
}

type loginData struct {
	baseData
	Next     string
	HasUsers bool
}

type verifyData struct {
	baseData
}

type editData struct {
	baseData
	Route          string
	PageTitle      string
	Description    string
	Tags           string
	Body           string
	Comment        string
	BaseVersion    string
	TranslationKey string
	HistoryURL     string
	IsNew          bool
	PRMode         bool
	CanUpload      bool
	CanReview      bool
	Protected      bool
	CanProtect     bool
}

type prData struct {
	baseData
	URL    string
	Number int
	Route  string
}

type accountData struct {
	baseData
	Identities  []*store.Identity
	HasGitHub   bool
	HasPassword bool
	TOTPEnabled bool
	TOTPSecret  string
	OTPAuthURL  string
}

type pagerData struct {
	Page     int
	Pages    int
	PrevPage int
	NextPage int
	Size     int
	HasPrev  bool
	HasNext  bool
}

func newPager(page, pages, size int) pagerData {
	if pages < 1 {
		pages = 1
	}
	if page < 1 {
		page = 1
	}
	if page > pages {
		page = pages
	}
	result := pagerData{Page: page, Pages: pages, Size: size}
	if page > 1 {
		result.HasPrev = true
		result.PrevPage = page - 1
	}
	if page < pages {
		result.HasNext = true
		result.NextPage = page + 1
	}
	return result
}

type groupChoice struct {
	Name     string
	Selected bool
}

type adminUserRow struct {
	User    *store.User
	IsSelf  bool
	Choices []groupChoice
}

type adminUsersData struct {
	baseData
	Users []adminUserRow
	Pager pagerData
}

type groupRightChoice struct {
	Name     string
	Selected bool
}

type adminGroupRow struct {
	Group  *store.Group
	Rights []groupRightChoice
}

type adminGroupsData struct {
	baseData
	Groups []adminGroupRow
	Rights []string
}

type adminReportsData struct {
	baseData
	Reports []*store.CommentReport
	Status  string
	Pager   pagerData
}

type adminAuditData struct {
	baseData
	Logs  []*store.AuditLog
	Query string
	From  string
	To    string
	Pager pagerData
}

type translationLocaleCell struct {
	Code     string
	Label    string
	Route    string
	Title    string
	EditURL  string
	Missing  bool
	Outdated bool
	Unknown  bool
}

type translationRow struct {
	Key            string
	DefaultTitle   string
	DefaultRoute   string
	DefaultMissing bool
	Locales        []translationLocaleCell
}

type translationsData struct {
	baseData
	Rows  []translationRow
	Pager pagerData
}

type historyData struct {
	baseData
	Route     string
	Revisions []*store.Revision
	CanEdit   bool
	CanReview bool
	LoggedIn  bool
	Watching  bool
	Next      string
}

type watchlistData struct {
	baseData
	Pages []*store.WatchedPage
}

type discussionData struct {
	baseData
	Route     string
	PageTitle string
	Comments  []*store.Comment
	LoggedIn  bool
	CanReview bool
	Watching  bool
	History   bool
	Next      string
}

type revisionData struct {
	baseData
	Revision  *store.Revision
	Diff      string
	CanReview bool
}

type reviewData struct {
	baseData
	Revisions []*store.Revision
}

type searchData struct {
	baseData
	Query   string
	Results []site.SearchResult
}

type messageData struct {
	baseData
	Heading string
	Message string
}

func loadUI() (*appUI, error) {
	assets, err := fs.Sub(uiFS, "ui/assets")
	if err != nil {
		return nil, err
	}
	names := []string{"login", "verify", "edit", "history", "revision", "review", "search", "message", "pr", "account", "watchlist", "discussion", "admin_users", "admin_reports", "admin_audit", "admin_groups", "translations"}
	pages := make(map[string]*template.Template, len(names))
	for _, name := range names {
		set, err := template.New("layout.html").ParseFS(uiFS, "ui/layout.html", "ui/"+name+".html")
		if err != nil {
			return nil, fmt.Errorf("parse ui %s: %w", name, err)
		}
		pages[name] = set
	}
	version, err := hashAssets(assets)
	if err != nil {
		return nil, err
	}
	return &appUI{pages: pages, assets: assets, version: version}, nil
}

func hashAssets(assets fs.FS) (string, error) {
	hash := sha256.New()
	entries, err := fs.ReadDir(assets, ".")
	if err != nil {
		return "", err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		data, err := fs.ReadFile(assets, name)
		if err != nil {
			return "", err
		}
		hash.Write(data)
		hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))[:16], nil
}

func (u *appUI) render(w io.Writer, name string, data any) error {
	view, ok := u.pages[name]
	if !ok {
		return fmt.Errorf("unknown ui page %q", name)
	}
	return view.ExecuteTemplate(w, "layout.html", data)
}
