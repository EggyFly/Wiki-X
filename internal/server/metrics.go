package server

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/EggyFly/Wiki-X/internal/site"
)

type metricsRegistry struct {
	mu          sync.Mutex
	requests    map[string]uint64
	rateLimited atomic.Uint64
}

func newMetrics() *metricsRegistry {
	return &metricsRegistry{requests: make(map[string]uint64)}
}

func (m *metricsRegistry) observeRequest(method string, status int) {
	key := method + "|" + strconv.Itoa(status)
	m.mu.Lock()
	m.requests[key]++
	m.mu.Unlock()
}

func (m *metricsRegistry) observeRateLimited() {
	m.rateLimited.Add(1)
}

type metricsWriter struct {
	http.ResponseWriter
	status int
}

func (w *metricsWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *metricsWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(data)
}

func (a *App) withMetrics(next http.Handler) http.Handler {
	path := a.cfg.Metrics.Path
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == path {
			next.ServeHTTP(w, r)
			return
		}
		writer := &metricsWriter{ResponseWriter: w}
		next.ServeHTTP(writer, r)
		status := writer.status
		if status == 0 {
			status = http.StatusOK
		}
		a.metrics.observeRequest(r.Method, status)
	})
}

func (a *App) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if token := strings.TrimSpace(a.cfg.Metrics.Token); token != "" {
		header := strings.TrimSpace(r.Header.Get("Authorization"))
		got := strings.TrimPrefix(header, "Bearer ")
		if len(got) != len(token) || subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			http.NotFound(w, r)
			return
		}
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	var builder strings.Builder
	builder.WriteString("# HELP wikix_up Whether the application is serving requests.\n")
	builder.WriteString("# TYPE wikix_up gauge\nwikix_up 1\n")

	stats := site.Stats{}
	if a.site != nil {
		stats = a.site.Stats()
	}
	builder.WriteString("# HELP wikix_page_cache_hits_total Page cache hits.\n")
	builder.WriteString("# TYPE wikix_page_cache_hits_total counter\n")
	fmt.Fprintf(&builder, "wikix_page_cache_hits_total %d\n", stats.CacheHits)
	builder.WriteString("# HELP wikix_page_cache_misses_total Page cache misses.\n")
	builder.WriteString("# TYPE wikix_page_cache_misses_total counter\n")
	fmt.Fprintf(&builder, "wikix_page_cache_misses_total %d\n", stats.CacheMisses)
	builder.WriteString("# HELP wikix_page_renders_total Page renders.\n")
	builder.WriteString("# TYPE wikix_page_renders_total counter\n")
	fmt.Fprintf(&builder, "wikix_page_renders_total %d\n", stats.Renders)
	builder.WriteString("# HELP wikix_page_render_failures_total Page render failures.\n")
	builder.WriteString("# TYPE wikix_page_render_failures_total counter\n")
	fmt.Fprintf(&builder, "wikix_page_render_failures_total %d\n", stats.RenderFailed)
	builder.WriteString("# HELP wikix_page_render_duration_seconds Page render duration.\n")
	builder.WriteString("# TYPE wikix_page_render_duration_seconds summary\n")
	fmt.Fprintf(&builder, "wikix_page_render_duration_seconds_sum %s\n", strconv.FormatFloat(float64(stats.RenderNanos)/1e9, 'f', 6, 64))
	fmt.Fprintf(&builder, "wikix_page_render_duration_seconds_count %d\n", stats.Renders)
	builder.WriteString("# HELP wikix_rate_limited_total Requests rejected by the rate limiter.\n")
	builder.WriteString("# TYPE wikix_rate_limited_total counter\n")
	fmt.Fprintf(&builder, "wikix_rate_limited_total %d\n", a.metrics.rateLimited.Load())

	a.metrics.mu.Lock()
	keys := make([]string, 0, len(a.metrics.requests))
	for key := range a.metrics.requests {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	builder.WriteString("# HELP wikix_http_requests_total HTTP requests by method and status.\n")
	builder.WriteString("# TYPE wikix_http_requests_total counter\n")
	for _, key := range keys {
		method, status, _ := strings.Cut(key, "|")
		fmt.Fprintf(&builder, "wikix_http_requests_total{method=%q,status=%q} %d\n", method, status, a.metrics.requests[key])
	}
	a.metrics.mu.Unlock()

	if a.store != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if count, err := a.store.CountPendingRevisions(ctx); err == nil {
			builder.WriteString("# HELP wikix_pending_revisions Pending revisions.\n")
			builder.WriteString("# TYPE wikix_pending_revisions gauge\n")
			fmt.Fprintf(&builder, "wikix_pending_revisions %d\n", count)
		}
		if count, err := a.store.CountReports(ctx, "open"); err == nil {
			builder.WriteString("# HELP wikix_open_reports Open comment reports.\n")
			builder.WriteString("# TYPE wikix_open_reports gauge\n")
			fmt.Fprintf(&builder, "wikix_open_reports %d\n", count)
		}
	}
	_, _ = w.Write([]byte(builder.String()))
}
