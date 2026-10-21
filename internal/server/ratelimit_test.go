package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/EggyFly/Wiki-X/internal/config"
)

func TestGlobalRateLimit(t *testing.T) {
	app, _, _ := setupCommunityFixture(t, config.GitHubConfig{})
	app.cfg.RateLimit = config.RateLimitConfig{Enabled: true, Requests: 2, Window: "1m"}
	server := httptest.NewServer(app.Handler())
	t.Cleanup(server.Close)
	client := testHTTPClient()

	for i := 0; i < 2; i++ {
		resp := mustGet(t, client, server.URL+"/guide/")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("request %d status = %d", i, resp.StatusCode)
		}
		resp.Body.Close()
	}
	limited := mustGet(t, client, server.URL+"/guide/")
	if limited.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("limited status = %d", limited.StatusCode)
	}
	if limited.Header.Get("Retry-After") != "60" {
		t.Fatalf("retry-after = %q", limited.Header.Get("Retry-After"))
	}
	limited.Body.Close()

	health := mustGet(t, client, server.URL+"/-/healthz")
	if health.StatusCode != http.StatusOK {
		t.Fatalf("healthz should be exempt, status = %d", health.StatusCode)
	}
	health.Body.Close()
}

func TestRouteRateLimit(t *testing.T) {
	app, _, _ := setupCommunityFixture(t, config.GitHubConfig{})
	app.cfg.RateLimit = config.RateLimitConfig{
		Enabled:       true,
		Requests:      100,
		Window:        "1m",
		RouteRequests: 1,
		RouteWindow:   "1m",
	}
	server := httptest.NewServer(app.Handler())
	t.Cleanup(server.Close)
	client := testHTTPClient()

	first := mustGet(t, client, server.URL+"/guide/")
	if first.StatusCode != http.StatusOK {
		t.Fatalf("first status = %d", first.StatusCode)
	}
	first.Body.Close()
	second := mustGet(t, client, server.URL+"/guide/")
	if second.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("second status = %d", second.StatusCode)
	}
	second.Body.Close()
	other := mustGet(t, client, server.URL+"/")
	if other.StatusCode != http.StatusOK {
		t.Fatalf("other route status = %d", other.StatusCode)
	}
	other.Body.Close()
}
