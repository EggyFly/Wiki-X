package server

import (
	"net/http"
	"strings"

	"wikix/internal/site"
)

func (a *App) handleSearch(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if runes := []rune(query); len(runes) > 200 {
		query = string(runes[:200])
	}
	var results []site.SearchResult
	if query != "" {
		results = a.site.Search(query, 50)
	}
	a.renderUI(w, r, http.StatusOK, "search", searchData{
		baseData: a.baseData(r, "搜索"),
		Query:    query,
		Results:  results,
	})
}
