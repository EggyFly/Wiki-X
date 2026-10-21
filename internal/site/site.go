package site

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"golang.org/x/sync/singleflight"

	"wikix/internal/cache"
	"wikix/internal/config"
	"wikix/internal/content"
	gitsource "wikix/internal/content/gitsource"
	"wikix/internal/i18n"
	"wikix/internal/render"
	"wikix/internal/theme"
	"wikix/themes"
)

var ErrNotFound = errors.New("page not found")

const renderVersion = "1"

type SkinInfo struct {
	Name  string
	Label string
}

type SiteInfo struct {
	Title        string
	Description  string
	BaseURL      string
	AssetVersion string
	Community    bool
	History      bool
	Translations bool
	Nav          []config.NavItem
	Year         int
	Skins        []SkinInfo
	CurrentSkin  string
	Switchable   bool
}

type PageInfo struct {
	Title       string
	Description string
	Updated     string
	Tags        []string
	Protected   bool
}

type LocaleLink struct {
	Code   string
	Label  string
	Href   string
	Active bool
}

type PageView struct {
	Site         SiteInfo
	Page         PageInfo
	Content      template.HTML
	Path         string
	Lang         string
	T            map[string]string
	Locales      []LocaleLink
	Fallback     bool
	FallbackPath string
}

type state struct {
	pages        map[string]*content.Page
	routes       []string
	themes       map[string]*theme.Theme
	skins        []SkinInfo
	defaultSkin  string
	switchable   bool
	notFound     map[string][]byte
	siteInfo     SiteInfo
	media        fs.FS
	locales      []config.LocaleConfig
	translations map[string]map[string]string
}

type Site struct {
	cfg          *config.Config
	source       content.Source
	cache        cache.Cache
	cacheTTL     time.Duration
	configHash   string
	markdown     *render.Markdown
	group        singleflight.Group
	state        atomic.Pointer[state]
	reloadMu     sync.Mutex
	lastCacheLog atomic.Int64
}

func Load(ctx context.Context, cfg *config.Config) (*Site, error) {
	source, err := newSource(cfg)
	if err != nil {
		return nil, err
	}
	return LoadWithSource(ctx, cfg, source)
}

func LoadWithSource(ctx context.Context, cfg *config.Config, source content.Source) (*Site, error) {
	if source == nil {
		return nil, errors.New("content source is nil")
	}
	ttl := cfg.Cache.CacheTTL()
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	pageCache, err := cache.New(cache.Options{
		Driver:        cfg.Cache.Driver,
		TTL:           ttl,
		MaxEntries:    cfg.Cache.MaxEntries,
		MaxBytes:      cfg.Cache.MaxBytes,
		RedisAddr:     cfg.Cache.Redis.Addr,
		RedisPassword: cfg.Cache.Redis.Password,
		RedisDB:       cfg.Cache.Redis.DB,
		RedisPrefix:   cfg.Cache.Redis.Prefix,
	})
	if err != nil {
		return nil, err
	}

	s := &Site{
		cfg:        cfg,
		source:     source,
		cache:      pageCache,
		cacheTTL:   ttl,
		configHash: configHash(cfg),
		markdown:   render.NewMarkdown(),
	}
	st, err := s.loadState(ctx)
	if err != nil {
		_ = pageCache.Close()
		return nil, err
	}
	s.state.Store(st)
	return s, nil
}

func (s *Site) loadState(ctx context.Context) (*state, error) {
	pages, err := s.source.Load(ctx)
	if err != nil {
		return nil, err
	}
	themesMap, skins, defaultSkin, err := loadThemes(s.cfg)
	if err != nil {
		return nil, err
	}
	switchable := len(skins) > 1 && !s.cfg.Theme.DisableSwitch

	siteInfo := SiteInfo{
		Title:        s.cfg.Site.Title,
		Description:  s.cfg.Site.Description,
		BaseURL:      s.cfg.Site.BaseURL,
		Community:    !s.cfg.Site.Static && (s.cfg.Content.Source == "postgres" || s.cfg.Site.CommunityUI),
		History:      s.cfg.Content.Source == "postgres",
		Translations: s.cfg.Content.Source == "postgres",
		Nav:          append([]config.NavItem(nil), s.cfg.Site.Nav...),
		Year:         time.Now().Year(),
		Skins:        skins,
		CurrentSkin:  defaultSkin,
		Switchable:   switchable,
	}
	pageMap := make(map[string]*content.Page, len(pages))
	routes := make([]string, 0, len(pages))
	for _, page := range pages {
		if page.Draft {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := checkRoute(page.Route); err != nil {
			return nil, fmt.Errorf("page %s: %w", page.Key, err)
		}
		if _, exists := pageMap[page.Route]; exists {
			return nil, fmt.Errorf("page %s: duplicate route %s", page.Key, page.Route)
		}
		pageMap[page.Route] = page
		routes = append(routes, page.Route)
	}
	sort.Strings(routes)

	var mediaFS fs.FS
	if source, ok := s.source.(content.MediaSource); ok {
		mediaFS = source.MediaFS()
	}
	locales := s.cfg.Site.LocaleConfigs()
	translations := make(map[string]map[string]string)
	for _, page := range pageMap {
		if page.TranslationKey == "" {
			continue
		}
		locale, _ := findLocale(locales, page.Route)
		if translations[page.TranslationKey] == nil {
			translations[page.TranslationKey] = make(map[string]string)
		}
		translations[page.TranslationKey][locale.Code] = page.Route
	}

	notFound := make(map[string][]byte, len(skins))
	for _, skin := range skins {
		themePack := themesMap[skin.Name]
		info := siteInfo
		info.AssetVersion = themePack.Version()
		info.CurrentSkin = skin.Name
		data, err := s.renderNotFound(themePack, info)
		if err != nil {
			return nil, err
		}
		notFound[skin.Name] = data
	}
	return &state{
		pages:        pageMap,
		routes:       routes,
		themes:       themesMap,
		skins:        skins,
		defaultSkin:  defaultSkin,
		switchable:   switchable,
		notFound:     notFound,
		siteInfo:     siteInfo,
		media:        mediaFS,
		locales:      locales,
		translations: translations,
	}, nil
}

func (s *Site) renderNotFound(themePack *theme.Theme, siteInfo SiteInfo) ([]byte, error) {
	var buf bytes.Buffer
	view := PageView{
		Site: siteInfo,
		Page: PageInfo{Title: "404"},
		Path: "/404.html",
		Lang: s.cfg.Site.Language,
		T:    i18n.For(s.cfg.Site.Language),
	}
	if err := themePack.RenderNotFound(&buf, view); err != nil {
		return nil, fmt.Errorf("render 404 page: %w", err)
	}
	return buf.Bytes(), nil
}

func (s *Site) Render(ctx context.Context, route string) ([]byte, error) {
	return s.RenderSkin(ctx, route, "")
}

func (s *Site) RenderSkin(ctx context.Context, route, skin string) ([]byte, error) {
	st := s.state.Load()
	themePack, skinName := st.selectTheme(skin)
	if themePack == nil {
		return nil, errors.New("theme is not loaded")
	}
	locale, baseRoute := findLocale(st.locales, route)
	page, ok := st.pages[route]
	fallback := false
	if !ok && locale.Prefix != "" {
		if basePage, exists := st.pages[baseRoute]; exists {
			page = basePage
			ok = true
			fallback = true
		}
	}
	if !ok {
		return nil, ErrNotFound
	}
	info := st.siteInfo
	info.AssetVersion = themePack.Version()
	info.CurrentSkin = skinName
	key := s.cacheKey(st, page, route, skinName, themePack.Version())

	if data, ok, err := s.cache.Get(ctx, key); err != nil {
		s.logCacheError(err)
	} else if ok {
		return data, nil
	}

	value, err, _ := s.group.Do(key, func() (any, error) {
		cacheCtx := context.WithoutCancel(ctx)
		if data, ok, err := s.cache.Get(cacheCtx, key); err != nil {
			s.logCacheError(err)
		} else if ok {
			return data, nil
		}
		html, err := s.renderPage(st, page, themePack, info, route, locale, baseRoute, fallback)
		if err != nil {
			return nil, err
		}
		if err := s.cache.Set(cacheCtx, key, html, s.cacheTTL); err != nil {
			s.logCacheError(err)
		}
		return html, nil
	})
	if err != nil {
		return nil, err
	}
	html, ok := value.([]byte)
	if !ok {
		return nil, errors.New("page render returned wrong type")
	}
	return html, nil
}

func (st *state) selectTheme(skin string) (*theme.Theme, string) {
	if themePack, ok := st.themes[skin]; ok {
		return themePack, skin
	}
	return st.themes[st.defaultSkin], st.defaultSkin
}

func (s *Site) renderPage(st *state, page *content.Page, themePack *theme.Theme, info SiteInfo, requestRoute string, locale config.LocaleConfig, baseRoute string, fallback bool) ([]byte, error) {
	body, err := s.markdown.ToHTML([]byte(page.Body))
	if err != nil {
		return nil, fmt.Errorf("render page %s: %w", page.Key, err)
	}
	view := PageView{
		Site: info,
		Page: PageInfo{
			Title:       page.Title,
			Description: page.Description,
			Updated:     page.Updated,
			Tags:        page.Tags,
			Protected:   page.Protected,
		},
		Content:      body,
		Path:         requestRoute,
		Lang:         locale.Code,
		T:            i18n.For(locale.Code),
		Locales:      buildLocaleLinks(st, page, locale.Code, baseRoute, requestRoute, fallback),
		Fallback:     fallback,
		FallbackPath: baseRoute,
	}
	var buf bytes.Buffer
	if err := themePack.RenderPage(&buf, view); err != nil {
		return nil, fmt.Errorf("apply theme to %s: %w", page.Key, err)
	}
	return buf.Bytes(), nil
}

func (s *Site) cacheKey(st *state, page *content.Page, requestRoute, skin, themeVersion string) string {
	version := page.Version
	if version == "" {
		sum := sha256.Sum256([]byte(page.Body))
		version = hex.EncodeToString(sum[:])
	}
	protectFlag := "n"
	if page.Protected {
		protectFlag = "p"
	}
	return "page:" + renderVersion + ":" + s.configHash + ":" + skin + ":" + themeVersion + ":" + requestRoute + ":" + version + ":" + protectFlag
}

func (s *Site) Reload(ctx context.Context) error {
	s.reloadMu.Lock()
	defer s.reloadMu.Unlock()

	st, err := s.loadState(ctx)
	if err != nil {
		return err
	}
	s.state.Store(st)
	return nil
}

func (s *Site) AutoReload(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.Reload(ctx); err != nil {
				if ctx.Err() != nil {
					return
				}
				slog.Warn("content reload failed", "err", err)
				continue
			}
			slog.Info("content reloaded", "pages", len(s.Routes()))
		}
	}
}

func (s *Site) HasRoute(route string) bool {
	st := s.state.Load()
	_, ok := st.pages[route]
	return ok
}

func (s *Site) FindPage(route string) (*content.Page, bool) {
	st := s.state.Load()
	page, ok := st.pages[route]
	return page, ok
}

func (s *Site) Routes() []string {
	st := s.state.Load()
	return append([]string(nil), st.routes...)
}

func (s *Site) NotFound() []byte {
	return s.NotFoundSkin("")
}

func (s *Site) NotFoundSkin(skin string) []byte {
	st := s.state.Load()
	if data, ok := st.notFound[skin]; ok {
		return data
	}
	return st.notFound[st.defaultSkin]
}

func (s *Site) Assets() fs.FS {
	return s.AssetsFor("")
}

func (s *Site) Media() fs.FS {
	return s.state.Load().media
}

func (s *Site) AssetsFor(skin string) fs.FS {
	st := s.state.Load()
	if themePack, ok := st.themes[skin]; ok {
		return themePack.Assets()
	}
	return st.themes[st.defaultSkin].Assets()
}

func (s *Site) LocaleCodeForRoute(route string) string {
	st := s.state.Load()
	locale, _ := findLocale(st.locales, route)
	return locale.Code
}

func (s *Site) BaseRouteFor(route string) string {
	st := s.state.Load()
	_, base := findLocale(st.locales, route)
	return base
}

func (s *Site) LocaleRoute(prefix, base string) string {
	return localeRoute(prefix, base)
}

func (s *Site) Info() SiteInfo {
	st := s.state.Load()
	info := st.siteInfo
	if themePack, ok := st.themes[st.defaultSkin]; ok {
		info.AssetVersion = themePack.Version()
	}
	info.CurrentSkin = st.defaultSkin
	info.Skins = append([]SkinInfo(nil), st.skins...)
	info.Switchable = st.switchable
	return info
}

func (s *Site) Skins() []SkinInfo {
	st := s.state.Load()
	return append([]SkinInfo(nil), st.skins...)
}

func (s *Site) DefaultSkin() string {
	return s.state.Load().defaultSkin
}

func (s *Site) HasSkin(skin string) bool {
	st := s.state.Load()
	_, ok := st.themes[skin]
	return ok
}

func (s *Site) Switchable() bool {
	return s.state.Load().switchable
}

type SearchResult struct {
	Route   string
	Title   string
	Snippet string
}

type scoredResult struct {
	SearchResult
	score int
}

func (s *Site) Search(query string, limit int) []SearchResult {
	q := strings.TrimSpace(strings.ToLower(query))
	if q == "" {
		return nil
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	st := s.state.Load()
	matched := make([]scoredResult, 0, limit)
	for route, page := range st.pages {
		score := 0
		if strings.Contains(strings.ToLower(page.Title), q) {
			score += 2
		}
		bodyHit := strings.Contains(strings.ToLower(page.Body), q)
		if !bodyHit && score == 0 {
			continue
		}
		snippet := ""
		if bodyHit {
			for _, line := range strings.Split(page.Body, "\n") {
				if strings.Contains(strings.ToLower(line), q) {
					snippet = cleanSnippet(line)
					break
				}
			}
		}
		if snippet == "" {
			snippet = cleanSnippet(page.Description)
		}
		if snippet == "" {
			snippet = cleanSnippet(page.Body)
		}
		matched = append(matched, scoredResult{
			SearchResult: SearchResult{Route: route, Title: page.Title, Snippet: snippet},
			score:        score,
		})
	}
	sort.Slice(matched, func(i, j int) bool {
		if matched[i].score != matched[j].score {
			return matched[i].score > matched[j].score
		}
		return matched[i].Title < matched[j].Title
	})
	if len(matched) > limit {
		matched = matched[:limit]
	}
	results := make([]SearchResult, len(matched))
	for i, item := range matched {
		results[i] = item.SearchResult
	}
	return results
}

func cleanSnippet(line string) string {
	line = strings.TrimSpace(line)
	line = strings.TrimLeft(line, "#>*-_` ")
	for _, marker := range []string{"**", "__", "`"} {
		line = strings.ReplaceAll(line, marker, "")
	}
	line = strings.Join(strings.Fields(line), " ")
	runes := []rune(line)
	if len(runes) > 160 {
		return string(runes[:160]) + "..."
	}
	return line
}

func (s *Site) AccessLog() bool {
	return s.cfg.Server.AccessLog
}

func (s *Site) CacheDriver() string {
	return s.cache.Driver()
}

func (s *Site) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	return s.cache.Allow(ctx, key, limit, window)
}

func (s *Site) Close() error {
	return s.cache.Close()
}

func (s *Site) logCacheError(err error) {
	now := time.Now().Unix()
	last := s.lastCacheLog.Load()
	if now-last < 30 || !s.lastCacheLog.CompareAndSwap(last, now) {
		return
	}
	slog.Warn("page cache unavailable", "err", err)
}

func newSource(cfg *config.Config) (content.Source, error) {
	switch cfg.Content.Source {
	case "git":
		source := gitsource.New(cfg.Content.Dir)
		source.SetMaxFileSize(cfg.Content.MaxFileSize)
		return source, nil
	case "postgres":
		return nil, errors.New("postgres content source is not implemented yet")
	default:
		return nil, fmt.Errorf("unknown content source %q", cfg.Content.Source)
	}
}

func loadThemes(cfg *config.Config) (map[string]*theme.Theme, []SkinInfo, string, error) {
	themesMap := make(map[string]*theme.Theme)
	labels := make(map[string]string)
	var order []string
	add := func(name, label string, themePack *theme.Theme) {
		if _, exists := themesMap[name]; !exists {
			order = append(order, name)
		}
		themesMap[name] = themePack
		labels[name] = label
	}
	lang := cfg.Site.Language

	for _, item := range []struct {
		name  string
		label string
	}{
		{"default", i18n.Text(lang, "skin_default")},
		{"compact", i18n.Text(lang, "skin_compact")},
	} {
		themePack, err := theme.Load(themes.FS, item.name)
		if err != nil {
			return nil, nil, "", fmt.Errorf("load builtin theme %s: %w", item.name, err)
		}
		add(item.name, item.label, themePack)
	}
	if cfg.Theme.Dir != "" {
		themePack, err := theme.LoadDir(cfg.Theme.Dir)
		if err != nil {
			return nil, nil, "", fmt.Errorf("load custom theme: %w", err)
		}
		add("custom", i18n.Text(lang, "skin_custom"), themePack)
	}
	if cfg.Theme.Root != "" {
		entries, err := os.ReadDir(cfg.Theme.Root)
		if err != nil {
			return nil, nil, "", fmt.Errorf("read theme root: %w", err)
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			name := entry.Name()
			themePack, err := theme.LoadDir(filepath.Join(cfg.Theme.Root, name))
			if err != nil {
				return nil, nil, "", fmt.Errorf("load theme %s: %w", name, err)
			}
			add(name, name, themePack)
		}
	}

	defaultSkin := strings.TrimSpace(cfg.Theme.Default)
	if defaultSkin == "" {
		if cfg.Theme.Dir != "" {
			defaultSkin = "custom"
		} else {
			defaultSkin = "default"
		}
	}
	if _, ok := themesMap[defaultSkin]; !ok {
		return nil, nil, "", fmt.Errorf("default theme %q is not loaded", defaultSkin)
	}

	skins := make([]SkinInfo, 0, len(order))
	for _, name := range order {
		skins = append(skins, SkinInfo{Name: name, Label: labels[name]})
	}
	return themesMap, skins, defaultSkin, nil
}

func ValidateRoute(route string) error {
	return checkRoute(route)
}

func checkRoute(route string) error {
	if !strings.HasPrefix(route, "/") || !strings.HasSuffix(route, "/") {
		return fmt.Errorf("bad route %q", route)
	}
	if len(route) > 2048 {
		return fmt.Errorf("route is too long")
	}
	for _, r := range route {
		if unicode.IsControl(r) {
			return fmt.Errorf("route contains control characters")
		}
	}
	if route == "/" {
		return nil
	}
	segments := strings.Split(strings.Trim(route, "/"), "/")
	first := segments[0]
	switch strings.ToLower(first) {
	case "assets", "404.html", "index.html", ".wikix-build.json":
		return fmt.Errorf("route %q uses a reserved path", route)
	}
	for _, segment := range segments {
		if err := checkRouteSegment(segment); err != nil {
			return err
		}
	}
	return nil
}

func checkRouteSegment(segment string) error {
	if segment == "" || segment == "." || segment == ".." {
		return fmt.Errorf("bad route segment %q", segment)
	}
	if segment != strings.TrimSpace(segment) {
		return fmt.Errorf("bad route segment %q", segment)
	}
	if strings.HasSuffix(segment, ".") || strings.HasSuffix(segment, " ") {
		return fmt.Errorf("bad route segment %q", segment)
	}
	if isReservedName(segment) {
		return fmt.Errorf("route segment %q is reserved on windows", segment)
	}
	for _, r := range segment {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r) {
			continue
		}
		switch r {
		case '-', '_', '.', ' ':
			continue
		default:
			return fmt.Errorf("bad character %q in route", r)
		}
	}
	return nil
}

func isReservedName(segment string) bool {
	name := strings.ToUpper(segment)
	if i := strings.IndexByte(name, '.'); i >= 0 {
		name = name[:i]
	}
	switch name {
	case "CON", "PRN", "AUX", "NUL":
		return true
	}
	if len(name) == 4 && (strings.HasPrefix(name, "COM") || strings.HasPrefix(name, "LPT")) {
		return name[3] >= '1' && name[3] <= '9'
	}
	return false
}

func findLocale(locales []config.LocaleConfig, route string) (config.LocaleConfig, string) {
	best := config.LocaleConfig{}
	base := route
	bestLen := -1
	for _, locale := range locales {
		if locale.Prefix == "" {
			continue
		}
		prefix := "/" + locale.Prefix
		if route == prefix+"/" || strings.HasPrefix(route, prefix+"/") {
			if len(locale.Prefix) > bestLen {
				best = locale
				bestLen = len(locale.Prefix)
				base = strings.TrimPrefix(route, prefix)
			}
		}
	}
	if bestLen >= 0 {
		if base == "" {
			base = "/"
		}
		return best, base
	}
	for _, locale := range locales {
		if locale.Prefix == "" {
			return locale, route
		}
	}
	return config.LocaleConfig{Code: "zh-CN", Label: "zh-CN"}, route
}

func buildLocaleLinks(st *state, page *content.Page, currentCode, baseRoute, currentRoute string, fallback bool) []LocaleLink {
	links := make([]LocaleLink, 0, len(st.locales))
	group := st.translations[page.TranslationKey]
	_, baseExists := st.pages[baseRoute]
	for _, locale := range st.locales {
		target := ""
		if group != nil {
			target = group[locale.Code]
		}
		if target == "" {
			target = localeRoute(locale.Prefix, baseRoute)
		}
		active := locale.Code == currentCode && target == currentRoute
		if fallback && locale.Code == currentCode {
			target = currentRoute
			active = true
		}
		if !active {
			if _, ok := st.pages[target]; !ok && !baseExists {
				continue
			}
		}
		links = append(links, LocaleLink{Code: locale.Code, Label: locale.Label, Href: target, Active: active})
	}
	return links
}

func localeRoute(prefix, base string) string {
	if prefix == "" {
		return base
	}
	if base == "/" {
		return "/" + prefix + "/"
	}
	return "/" + prefix + base
}

func configHash(cfg *config.Config) string {
	hash := sha256.New()
	values := []string{cfg.Site.Title, cfg.Site.Description, cfg.Site.BaseURL, cfg.Site.Language,
		fmt.Sprintf("%t", cfg.Site.CommunityUI), fmt.Sprintf("%t", cfg.Site.Static), cfg.Content.Source,
		cfg.Theme.Default, fmt.Sprintf("%t", cfg.Theme.DisableSwitch), cfg.Theme.Dir, cfg.Theme.Root}
	for _, value := range values {
		hash.Write([]byte(value))
		hash.Write([]byte{0})
	}
	for _, locale := range cfg.Site.Locales {
		for _, value := range []string{locale.Code, locale.Label, locale.Prefix} {
			hash.Write([]byte(value))
			hash.Write([]byte{0})
		}
	}
	for _, item := range cfg.Site.Nav {
		hash.Write([]byte(item.Title))
		hash.Write([]byte{0})
		hash.Write([]byte(item.Href))
		hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}
