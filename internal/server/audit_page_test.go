package server

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

var auditCursorPattern = regexp.MustCompile(`cursor=([A-Za-z0-9_-]+)`)

func TestAdminAuditCursorPagination(t *testing.T) {
	baseURL, data, _ := setupCommunityApp(t)
	for i := 0; i < 12; i++ {
		if err := data.LogAudit(context.Background(), 0, "test.page", "target", "row"); err != nil {
			t.Fatal(err)
		}
	}
	admin := loginTestClient(t, baseURL, "admin", "admin-pass-123")
	first := readResponse(t, mustGet(t, admin, baseURL+"/admin/audit?q=test.page&page_size=10"))
	if strings.Count(first, "<code>test.page</code>") != 10 {
		t.Fatalf("first page rows = %d", strings.Count(first, "<code>test.page</code>"))
	}
	match := auditCursorPattern.FindStringSubmatch(first)
	if len(match) < 2 {
		t.Fatal("next cursor missing")
	}
	second := readResponse(t, mustGet(t, admin, baseURL+"/admin/audit?q=test.page&page_size=10&cursor="+match[1]))
	if strings.Count(second, "<code>test.page</code>") != 2 {
		t.Fatalf("second page rows = %d", strings.Count(second, "<code>test.page</code>"))
	}
	if auditCursorPattern.MatchString(second) {
		t.Fatal("last page should not have a next cursor")
	}
	if !strings.Contains(second, "返回第一页") {
		t.Fatal("first page link missing")
	}

	bad := mustGet(t, admin, baseURL+"/admin/audit?cursor=not-a-cursor")
	if bad.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad cursor status = %d", bad.StatusCode)
	}
	bad.Body.Close()
}
