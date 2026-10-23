package audit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/EggyFly/Wiki-X/internal/config"
	"github.com/EggyFly/Wiki-X/internal/store"
)

func TestArchive(t *testing.T) {
	databaseURL := os.Getenv("TEST_POSTGRES_URL_AUDIT")
	if databaseURL == "" {
		t.Skip("set TEST_POSTGRES_URL_AUDIT to run audit integration tests")
	}
	ctx := context.Background()
	data, err := store.Open(ctx, config.DatabaseConfig{URL: databaseURL, MaxConns: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	if err := data.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, `TRUNCATE audit_logs RESTART IDENTITY`); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-200 * 24 * time.Hour)
	if _, err := pool.Exec(ctx, `INSERT INTO audit_logs (action, target, detail, created_at)
		VALUES ('old.action', 'target', 'detail', $1), ('fresh.action', 'target2', '', now())`, old); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	result, err := Archive(ctx, data, dir, time.Now().Add(-180*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if result.Count != 1 || result.Deleted != 1 || result.File == "" {
		t.Fatalf("result = %+v", result)
	}
	content, err := os.ReadFile(result.File)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), `"old.action"`) || strings.Contains(string(content), "fresh.action") {
		t.Fatalf("archive content = %s", content)
	}
	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	if len(lines) != 1 {
		t.Fatalf("archive lines = %d", len(lines))
	}
	checksum, err := os.ReadFile(result.File + ".sha256")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	if !strings.HasPrefix(string(checksum), hex.EncodeToString(sum[:])) {
		t.Fatalf("checksum mismatch: %s", checksum)
	}
	info, err := os.Stat(result.File)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("archive mode = %o", info.Mode().Perm())
	}
	if _, err := os.Stat(filepath.Join(dir, "."+filepath.Base(result.File)+".tmp")); !os.IsNotExist(err) {
		t.Fatal("temp file should be removed")
	}
	count, err := data.CountAuditLogs(ctx, store.AuditFilter{})
	if err != nil || count != 1 {
		t.Fatalf("remaining = %d, %v", count, err)
	}

	empty, err := Archive(ctx, data, dir, time.Now().Add(-180*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if empty.Count != 0 {
		t.Fatalf("second archive = %+v", empty)
	}
}
