package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestDeleteUserAnonymizes(t *testing.T) {
	data := openTestStore(t)
	ctx := context.Background()
	admin, err := data.CreateUser(ctx, "admin", "", "hash", true)
	if err != nil {
		t.Fatal(err)
	}
	user, err := data.CreateUser(ctx, "player", "player@example.com", "hash", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := data.LinkIdentity(ctx, user.ID, "github", "123", "player-gh", ""); err != nil {
		t.Fatal(err)
	}
	if err := data.CreateSession(ctx, "token-hash", user.ID, "csrf", time.Now().Add(time.Hour), "ua", "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if err := data.Watch(ctx, user.ID, "/guide/"); err != nil {
		t.Fatal(err)
	}
	if _, err := data.AddComment(ctx, "/guide/", user.ID, "hello"); err != nil {
		t.Fatal(err)
	}
	if err := data.ReplaceRecoveryCodes(ctx, user.ID, []string{"hash"}, user.ID); err != nil {
		t.Fatal(err)
	}

	exported, err := data.ExportUserData(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if exported.Profile.Name != "player" || exported.Profile.Email != "player@example.com" || len(exported.Identities) != 1 {
		t.Fatalf("export = %+v", exported)
	}
	if len(exported.Sessions) != 1 || len(exported.Comments) != 1 || len(exported.Watchlist) != 1 {
		t.Fatalf("export details = %+v", exported)
	}

	name, err := data.DeleteUser(ctx, user.ID, admin.ID, "user request")
	if err != nil || name != "player" {
		t.Fatalf("delete = %q, %v", name, err)
	}
	if _, err := data.UserByID(ctx, user.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UserByID after delete = %v", err)
	}
	if _, err := data.ExportUserData(ctx, user.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("export after delete = %v", err)
	}
	var deletedName, email, passwordHash string
	var deletedAt *time.Time
	if err := data.pool.QueryRow(ctx, `SELECT name, email, password_hash, deleted_at FROM users WHERE id = $1`, user.ID).
		Scan(&deletedName, &email, &passwordHash, &deletedAt); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(deletedName, "deleted-2-") || email != "" || passwordHash != "" || deletedAt == nil {
		t.Fatalf("raw user = %q, %q, %q, %v", deletedName, email, passwordHash, deletedAt)
	}
	var commentUserID *int64
	if err := data.pool.QueryRow(ctx, `SELECT user_id FROM comments WHERE body = 'hello'`).Scan(&commentUserID); err != nil {
		t.Fatal(err)
	}
	if commentUserID != nil {
		t.Fatal("comment user_id should be cleared")
	}
	var identities int
	if err := data.pool.QueryRow(ctx, `SELECT count(*) FROM identities WHERE user_id = $1`, user.ID).Scan(&identities); err != nil || identities != 0 {
		t.Fatalf("identities = %d, %v", identities, err)
	}
	var sessions int
	if err := data.pool.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE user_id = $1`, user.ID).Scan(&sessions); err != nil || sessions != 0 {
		t.Fatalf("sessions = %d, %v", sessions, err)
	}
	if ok, err := data.HasAudit(ctx, "user.delete", "player"); err != nil || !ok {
		t.Fatalf("delete audit = %v, %v", ok, err)
	}
}

func TestDeleteLastAdminRejected(t *testing.T) {
	data := openTestStore(t)
	ctx := context.Background()
	admin, err := data.CreateUser(ctx, "admin", "", "hash", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := data.DeleteUser(ctx, admin.ID, admin.ID, "self"); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("last admin delete error = %v", err)
	}
}
