package store

import (
	"context"
	"testing"
	"time"
)

func TestAuditLogsCursorPagination(t *testing.T) {
	data := openTestStore(t)
	ctx := context.Background()
	base := time.Now().Add(-time.Hour)
	for i := 0; i < 7; i++ {
		at := base.Add(time.Duration(i) * time.Second)
		if _, err := data.pool.Exec(ctx, `INSERT INTO audit_logs (action, target, created_at)
			VALUES ($1, $2, $3)`, "page.action", "target", at); err != nil {
			t.Fatal(err)
		}
	}
	seen := make(map[int64]bool)
	var cursor *AuditCursor
	pages := 0
	for {
		logs, next, err := data.ListAuditLogsPage(ctx, AuditFilter{}, cursor, 3)
		if err != nil {
			t.Fatal(err)
		}
		pages++
		for _, entry := range logs {
			if seen[entry.ID] {
				t.Fatalf("duplicate id %d", entry.ID)
			}
			seen[entry.ID] = true
		}
		if next == nil {
			break
		}
		cursor = next
		if pages > 5 {
			t.Fatal("too many pages")
		}
	}
	if len(seen) != 7 || pages != 3 {
		t.Fatalf("seen = %d, pages = %d", len(seen), pages)
	}

	from := base.Add(3 * time.Second)
	to := base.Add(5 * time.Second)
	logs, _, err := data.ListAuditLogsPage(ctx, AuditFilter{From: &from, To: &to}, nil, 10)
	if err != nil || len(logs) != 2 {
		t.Fatalf("filtered logs = %d, %v", len(logs), err)
	}
}
