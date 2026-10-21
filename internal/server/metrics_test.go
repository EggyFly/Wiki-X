package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/EggyFly/Wiki-X/internal/config"
)

func TestMetricsEndpoint(t *testing.T) {
	app, _, _ := setupCommunityFixture(t, config.GitHubConfig{})
	app.cfg.Metrics = config.MetricsConfig{Enabled: true, Path: "/-/metrics"}
	server := httptest.NewServer(app.Handler())
	t.Cleanup(server.Close)
	client := testHTTPClient()

	for i := 0; i < 2; i++ {
		resp := mustGet(t, client, server.URL+"/guide/")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("page status = %d", resp.StatusCode)
		}
		resp.Body.Close()
	}
	resp := mustGet(t, client, server.URL+"/-/metrics")
	body := readResponse(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("metrics status = %d", resp.StatusCode)
	}
	if !strings.Contains(body, "wikix_page_cache_misses_total 1") {
		t.Fatalf("misses missing:\n%s", body)
	}
	if !strings.Contains(body, "wikix_page_cache_hits_total 1") {
		t.Fatalf("hits missing:\n%s", body)
	}
	if !strings.Contains(body, "wikix_page_renders_total 1") {
		t.Fatalf("renders missing:\n%s", body)
	}
	if !strings.Contains(body, `wikix_http_requests_total{method="GET",status="200"}`) {
		t.Fatalf("http requests missing:\n%s", body)
	}
	if !strings.Contains(body, "wikix_pending_revisions 0") {
		t.Fatalf("pending revisions missing:\n%s", body)
	}
}

func TestMetricsToken(t *testing.T) {
	app, _, _ := setupCommunityFixture(t, config.GitHubConfig{})
	app.cfg.Metrics = config.MetricsConfig{Enabled: true, Path: "/-/metrics", Token: "secret"}
	server := httptest.NewServer(app.Handler())
	t.Cleanup(server.Close)
	client := testHTTPClient()

	denied := mustGet(t, client, server.URL+"/-/metrics")
	if denied.StatusCode != http.StatusNotFound {
		t.Fatalf("metrics without token = %d", denied.StatusCode)
	}
	denied.Body.Close()

	request, err := http.NewRequest(http.MethodGet, server.URL+"/-/metrics", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer secret")
	allowed, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if allowed.StatusCode != http.StatusOK {
		t.Fatalf("metrics with token = %d", allowed.StatusCode)
	}
	allowed.Body.Close()
}
