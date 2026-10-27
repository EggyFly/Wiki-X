package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/EggyFly/Wiki-X/internal/search"
	"github.com/EggyFly/Wiki-X/internal/site"
	"github.com/EggyFly/Wiki-X/internal/store"
)

func (a *App) handleSearch(w http.ResponseWriter, r *http.Request) {
	if a.cfg != nil && a.cfg.Search.RateLimitValue() > 0 {
		window := a.cfg.Search.RateWindowDuration()
		if !a.allowRate(r.Context(), "search:"+a.requestIP(r), a.cfg.Search.RateLimitValue(), window) {
			a.metrics.observeRateLimited()
			w.Header().Set("Retry-After", strconv.Itoa(int(window.Seconds())))
			http.Error(w, "too many requests", http.StatusTooManyRequests)
			return
		}
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if runes := []rune(query); len(runes) > 200 {
		query = string(runes[:200])
	}
	var results []site.SearchResult
	if query != "" {
		results = a.searchPages(r.Context(), query, 50)
	}
	a.renderUI(w, r, http.StatusOK, "search", searchData{
		baseData: a.baseData(r, "搜索"),
		Query:    query,
		Results:  results,
	})
}

func (a *App) searchPages(ctx context.Context, query string, limit int) []site.SearchResult {
	backend := "memory"
	if a.cfg != nil {
		backend = strings.TrimSpace(a.cfg.Search.Backend)
		if backend == "" || backend == "auto" {
			backend = "memory"
			if a.cfg.Content.Source == "postgres" {
				backend = "postgres"
			}
		}
	}
	if backend == "meilisearch" && a.searchClient != nil {
		hits, err := a.searchClient.Search(ctx, query, limit)
		if err == nil {
			results := make([]site.SearchResult, 0, len(hits))
			for _, hit := range hits {
				results = append(results, site.SearchResult{
					Route:   hit.Route,
					Title:   hit.Title,
					Snippet: site.CleanSnippet(hit.Snippet),
				})
			}
			return results
		}
		slog.Warn("meilisearch query failed, using postgres search", "err", err)
		backend = "postgres"
	}
	if backend == "postgres" && a.store != nil {
		hits, err := a.store.SearchPages(ctx, query, limit)
		if err == nil {
			results := make([]site.SearchResult, 0, len(hits))
			for _, hit := range hits {
				results = append(results, site.SearchResult{
					Route:   hit.Route,
					Title:   hit.Title,
					Snippet: site.CleanSnippet(hit.Snippet),
				})
			}
			return results
		}
		slog.Warn("postgres search failed, using memory search", "err", err)
	}
	if a.site == nil {
		return nil
	}
	return a.site.Search(query, limit)
}

func (a *App) syncSearchDocument(ctx context.Context, route string) {
	if a.searchClient == nil || a.store == nil || route == "" {
		return
	}
	searchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	page, err := a.store.GetPage(searchCtx, route)
	if errors.Is(err, store.ErrNotFound) {
		if err := a.searchClient.Delete(searchCtx, []string{route}); err != nil {
			slog.Warn("delete search document failed", "route", route, "err", err)
		}
		return
	}
	if err != nil {
		slog.Warn("load page for search index failed", "route", route, "err", err)
		return
	}
	if page.DeletedAt != nil {
		if err := a.searchClient.Delete(searchCtx, []string{route}); err != nil {
			slog.Warn("delete deleted page from search failed", "route", route, "err", err)
		}
		return
	}
	if err := a.searchClient.Index(searchCtx, []search.Document{{
		ID:          search.DocumentID(page.Route),
		Route:       page.Route,
		Title:       page.Title,
		Description: page.Description,
		Body:        page.Body,
	}}); err != nil {
		slog.Warn("index search document failed", "route", route, "err", err)
	}
}
