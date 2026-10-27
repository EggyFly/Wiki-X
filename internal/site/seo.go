package site

import (
	"context"
	"encoding/xml"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/EggyFly/Wiki-X/internal/content"
)

type sitemapURL struct {
	Loc     string `xml:"loc"`
	LastMod string `xml:"lastmod,omitempty"`
}

type sitemapDoc struct {
	XMLName xml.Name     `xml:"urlset"`
	Xmlns   string       `xml:"xmlns,attr"`
	URLs    []sitemapURL `xml:"url"`
}

type rssDoc struct {
	XMLName xml.Name   `xml:"rss"`
	Version string     `xml:"version,attr"`
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Title         string    `xml:"title"`
	Link          string    `xml:"link"`
	Description   string    `xml:"description"`
	LastBuildDate string    `xml:"lastBuildDate,omitempty"`
	Items         []rssItem `xml:"item"`
}

type rssItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	GUID        string `xml:"guid"`
	PubDate     string `xml:"pubDate,omitempty"`
	Description string `xml:"description,omitempty"`
}

func (s *Site) Sitemap(ctx context.Context, baseURL string) ([]byte, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	cacheable := baseURL != "" && baseURL == strings.TrimRight(strings.TrimSpace(s.cfg.Site.BaseURL), "/")
	cacheKey := "sitemap:" + s.configHash + ":" + baseURL
	if cacheable {
		if data, ok, err := s.cache.Get(ctx, cacheKey); err == nil && ok {
			return data, nil
		}
	}
	routes := s.Routes()
	sort.Strings(routes)
	doc := sitemapDoc{Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9", URLs: make([]sitemapURL, 0, len(routes))}
	for _, route := range routes {
		page, ok := s.FindPage(route)
		if !ok {
			continue
		}
		item := sitemapURL{Loc: absoluteURL(baseURL, route)}
		if _, err := time.Parse("2006-01-02", page.Updated); err == nil {
			item.LastMod = page.Updated
		}
		doc.URLs = append(doc.URLs, item)
	}
	data, err := marshalXML(doc)
	if err == nil && cacheable {
		_ = s.cache.Set(ctx, cacheKey, data, 5*time.Minute)
	}
	return data, err
}

func (s *Site) Feed(ctx context.Context, baseURL string, limit int) ([]byte, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	cacheable := baseURL != "" && baseURL == strings.TrimRight(strings.TrimSpace(s.cfg.Site.BaseURL), "/")
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	cacheKey := "feed:" + s.configHash + ":" + baseURL + ":" + strconv.Itoa(limit)
	if cacheable {
		if data, ok, err := s.cache.Get(ctx, cacheKey); err == nil && ok {
			return data, nil
		}
	}
	info := s.Info()
	pages := make([]*content.Page, 0, limit)
	for _, route := range s.Routes() {
		page, ok := s.FindPage(route)
		if ok {
			pages = append(pages, page)
		}
	}
	sort.SliceStable(pages, func(i, j int) bool {
		if pages[i].Updated != pages[j].Updated {
			return pages[i].Updated > pages[j].Updated
		}
		return pages[i].Title < pages[j].Title
	})
	if len(pages) > limit {
		pages = pages[:limit]
	}
	doc := rssDoc{
		Version: "2.0",
		Channel: rssChannel{
			Title:         info.Title,
			Link:          absoluteURL(baseURL, "/"),
			Description:   info.Description,
			LastBuildDate: time.Now().Format(time.RFC1123Z),
			Items:         make([]rssItem, 0, len(pages)),
		},
	}
	for _, page := range pages {
		item := rssItem{
			Title:       page.Title,
			Link:        absoluteURL(baseURL, page.Route),
			GUID:        absoluteURL(baseURL, page.Route),
			Description: page.Description,
		}
		if updated, err := time.Parse("2006-01-02", page.Updated); err == nil {
			item.PubDate = updated.Format(time.RFC1123Z)
		}
		doc.Channel.Items = append(doc.Channel.Items, item)
	}
	data, err := marshalXML(doc)
	if err == nil && cacheable {
		_ = s.cache.Set(ctx, cacheKey, data, 5*time.Minute)
	}
	return data, err
}

func (s *Site) Robots(baseURL string) []byte {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	var buf strings.Builder
	buf.WriteString("User-agent: *\n")
	buf.WriteString("Allow: /\n")
	for _, path := range []string{"/admin/", "/account/", "/edit", "/review", "/history", "/revision", "/translations", "/watchlist", "/discussion", "/search"} {
		buf.WriteString("Disallow: " + path + "\n")
	}
	if baseURL != "" {
		buf.WriteString("\nSitemap: " + absoluteURL(baseURL, "/sitemap.xml") + "\n")
	}
	return []byte(buf.String())
}

func absoluteURL(baseURL, route string) string {
	if baseURL == "" {
		return route
	}
	if route == "/" {
		return baseURL + "/"
	}
	return baseURL + route
}

func marshalXML(value any) ([]byte, error) {
	data, err := xml.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), append(data, '\n')...), nil
}
