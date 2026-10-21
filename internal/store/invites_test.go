package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"
)

func TestInviteRegistration(t *testing.T) {
	data := openTestStore(t)
	ctx := context.Background()
	admin, err := data.CreateUser(ctx, "admin", "", "hash", true)
	if err != nil {
		t.Fatal(err)
	}

	code := testTokenHash("TEST-INVITE-0001")
	expires := time.Now().Add(time.Hour)
	if err := data.CreateInvite(ctx, code, "first", 1, &expires, admin.ID); err != nil {
		t.Fatal(err)
	}
	invites, err := data.ListInvites(ctx, 10)
	if err != nil || len(invites) != 1 {
		t.Fatalf("invites = %d, %v", len(invites), err)
	}
	if invites[0].Status() != "active" || invites[0].UsesText() != "0 / 1" {
		t.Fatalf("invite = %+v", invites[0])
	}

	user, err := data.RegisterUser(ctx, RegisterInput{
		Name:         "invited",
		Email:        "invited@example.com",
		PasswordHash: "hash",
		Group:        "user",
		InviteHash:   code,
		Detail:       "invite",
	})
	if err != nil {
		t.Fatal(err)
	}
	if user.Disabled || len(user.Groups) != 1 || user.Groups[0] != "user" {
		t.Fatalf("registered user = %+v", user)
	}
	if _, err := data.RegisterUser(ctx, RegisterInput{
		Name:         "second",
		Email:        "second@example.com",
		PasswordHash: "hash",
		Group:        "user",
		InviteHash:   code,
	}); !errors.Is(err, ErrInviteInvalid) {
		t.Fatalf("reused invite error = %v", err)
	}

	expired := time.Now().Add(-time.Minute)
	if err := data.CreateInvite(ctx, testTokenHash("EXPIRED"), "old", 1, &expired, admin.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := data.RegisterUser(ctx, RegisterInput{
		Name:         "expireduser",
		Email:        "expired@example.com",
		PasswordHash: "hash",
		Group:        "user",
		InviteHash:   testTokenHash("EXPIRED"),
	}); !errors.Is(err, ErrInviteInvalid) {
		t.Fatalf("expired invite error = %v", err)
	}

	if err := data.DeleteInvite(ctx, invites[0].ID, admin.ID); err != nil {
		t.Fatal(err)
	}
	if err := data.DeleteInvite(ctx, invites[0].ID, admin.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete missing invite error = %v", err)
	}
}

func testTokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func TestPendingRegistrationVerified(t *testing.T) {
	data := openTestStore(t)
	ctx := context.Background()
	if _, err := data.CreateUser(ctx, "admin", "", "hash", true); err != nil {
		t.Fatal(err)
	}
	pending, err := data.RegisterUser(ctx, RegisterInput{
		Name:         "signup",
		Email:        "signup@example.com",
		PasswordHash: "hash",
		Group:        "user",
		Pending:      true,
		Detail:       "open",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !pending.Disabled {
		t.Fatal("pending signup should be disabled")
	}
	activated, err := data.CompleteEmailVerification(ctx, pending.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !activated {
		t.Fatal("pending signup should be activated")
	}
	verified, err := data.UserByID(ctx, pending.ID)
	if err != nil {
		t.Fatal(err)
	}
	if verified.Disabled || !verified.EmailVerified {
		t.Fatalf("verified user = %+v", verified)
	}
	activated, err = data.CompleteEmailVerification(ctx, verified.ID)
	if err != nil {
		t.Fatal(err)
	}
	if activated {
		t.Fatal("already active user should not report activation")
	}
}
