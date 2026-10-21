package store

import (
	"context"
	"testing"
	"time"
)

func TestAuditArchiveStream(t *testing.T) {
	data := openTestStore(t)
	ctx := context.Background()
	old := time.Now().Add(-200 * 24 * time.Hour)
	if _, err := data.pool.Exec(ctx, `INSERT INTO audit_logs (action, target, detail, created_at)
		VALUES ('old.one', 'a', '', $1), ('old.two', 'b', '', $1), ('new.one', 'c', '', now())`, old); err != nil {
		t.Fatal(err)
	}
	var seen []string
	maxID, count, err := data.StreamAuditLogsBefore(ctx, time.Now().Add(-180*24*time.Hour), func(item *AuditLog) error {
		seen = append(seen, item.Action)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 || len(seen) != 2 || maxID == 0 {
		t.Fatalf("count = %d, seen = %+v, maxID = %d", count, seen, maxID)
	}
	deleted, err := data.DeleteAuditLogsThrough(ctx, maxID, time.Now().Add(-180*24*time.Hour))
	if err != nil || deleted != 2 {
		t.Fatalf("deleted = %d, %v", deleted, err)
	}
	remaining, err := data.CountAuditLogs(ctx, AuditFilter{})
	if err != nil || remaining != 1 {
		t.Fatalf("remaining = %d, %v", remaining, err)
	}
}
