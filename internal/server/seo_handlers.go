package server

import (
	"log/slog"
	"net/http"
	"strings"
)

func (a *App) handleRobots(w http.ResponseWriter, r *http.Request) {
	base := a.seoBaseURL(r)
	a.seoCacheHeaders(w, r)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write(a.site.Robots(base))
}

func (a *App) handleSitemap(w http.ResponseWriter, r *http.Request) {
	data, err := a.site.Sitemap(r.Context(), a.seoBaseURL(r))
	if err != nil {
		slog.Error("build sitemap failed", "err", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	a.seoCacheHeaders(w, r)
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	_, _ = w.Write(data)
}

func (a *App) handleFeed(w http.ResponseWriter, r *http.Request) {
	data, err := a.site.Feed(r.Context(), a.seoBaseURL(r), 50)
	if err != nil {
		slog.Error("build feed failed", "err", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	a.seoCacheHeaders(w, r)
	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	_, _ = w.Write(data)
}

func (a *App) seoBaseURL(r *http.Request) string {
	if a.cfg != nil {
		if base := strings.TrimRight(strings.TrimSpace(a.cfg.Site.BaseURL), "/"); base != "" {
			return base
		}
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if r.Host == "" {
		return ""
	}
	return scheme + "://" + r.Host
}

func (a *App) seoCacheHeaders(w http.ResponseWriter, r *http.Request) {
	if a.cfg != nil && strings.TrimSpace(a.cfg.Site.BaseURL) != "" {
		w.Header().Set("Cache-Control", "public, max-age=600")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
}
