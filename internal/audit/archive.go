package audit

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/EggyFly/Wiki-X/internal/config"
	"github.com/EggyFly/Wiki-X/internal/store"
)

type Result struct {
	File    string
	Count   int
	Deleted int64
	MaxID   int64
}

type entry struct {
	ID        int64     `json:"id"`
	ActorID   *int64    `json:"actor_id"`
	ActorName string    `json:"actor_name"`
	Action    string    `json:"action"`
	Target    string    `json:"target"`
	Detail    string    `json:"detail"`
	CreatedAt time.Time `json:"created_at"`
}

func Archive(ctx context.Context, data *store.Store, dir string, before time.Time) (Result, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Result{}, fmt.Errorf("create archive dir: %w", err)
	}
	name := "audit-" + time.Now().UTC().Format("20060102T150405.000000000Z") + ".ndjson"
	target := filepath.Join(dir, name)
	temp := filepath.Join(dir, "."+name+".tmp")
	file, err := os.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return Result{}, fmt.Errorf("create archive file: %w", err)
	}
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(temp)
		}
	}()

	hasher := sha256.New()
	buffered := bufio.NewWriter(io.MultiWriter(file, hasher))
	encoder := json.NewEncoder(buffered)
	encoder.SetEscapeHTML(false)
	maxID, count, err := data.StreamAuditLogsBefore(ctx, before, func(item *store.AuditLog) error {
		return encoder.Encode(entry{
			ID:        item.ID,
			ActorID:   item.ActorID,
			ActorName: item.ActorName,
			Action:    item.Action,
			Target:    item.Target,
			Detail:    item.Detail,
			CreatedAt: item.CreatedAt,
		})
	})
	if err != nil {
		file.Close()
		return Result{}, fmt.Errorf("write archive: %w", err)
	}
	if count == 0 {
		file.Close()
		return Result{}, nil
	}
	if err := buffered.Flush(); err != nil {
		file.Close()
		return Result{}, fmt.Errorf("flush archive: %w", err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return Result{}, fmt.Errorf("sync archive: %w", err)
	}
	if err := file.Close(); err != nil {
		return Result{}, fmt.Errorf("close archive: %w", err)
	}
	if err := os.Chmod(temp, 0o600); err != nil {
		return Result{}, fmt.Errorf("chmod archive: %w", err)
	}
	if err := os.Rename(temp, target); err != nil {
		return Result{}, fmt.Errorf("rename archive: %w", err)
	}
	removeTemp = false
	checksum := hex.EncodeToString(hasher.Sum(nil)) + "  " + name + "\n"
	if err := os.WriteFile(target+".sha256", []byte(checksum), 0o600); err != nil {
		return Result{File: target, Count: count}, fmt.Errorf("write checksum: %w", err)
	}
	deleted, err := data.DeleteAuditLogsThrough(ctx, maxID, before)
	if err != nil {
		return Result{File: target, Count: count, MaxID: maxID}, fmt.Errorf("delete archived logs: %w", err)
	}
	return Result{File: target, Count: count, Deleted: deleted, MaxID: maxID}, nil
}

func RunWorker(ctx context.Context, data *store.Store, cfg config.AuditConfig) {
	retention := cfg.RetentionDuration()
	if retention <= 0 || data == nil {
		return
	}
	runOnce := func() {
		cutoff := time.Now().Add(-retention)
		result, err := Archive(ctx, data, cfg.ArchiveDir, cutoff)
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				slog.Error("audit archive failed", "err", err)
			}
			return
		}
		if result.Count > 0 {
			slog.Info("audit archive done", "file", result.File, "archived", result.Count, "deleted", result.Deleted)
		}
	}
	runOnce()
	ticker := time.NewTicker(cfg.IntervalDuration())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runOnce()
		}
	}
}
