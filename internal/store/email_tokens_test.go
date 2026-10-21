package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestEmailTokens(t *testing.T) {
	data := openTestStore(t)
	ctx := context.Background()
	user, err := data.CreateUser(ctx, "mailuser", "mail@example.com", "hash", false)
	if err != nil {
		t.Fatal(err)
	}
	if user.EmailVerified {
		t.Fatal("new user should not be email verified")
	}

	expires := time.Now().Add(time.Hour)
	if err := data.CreateEmailToken(ctx, user.ID, EmailTokenVerify, "hash-one", expires); err != nil {
		t.Fatal(err)
	}
	if err := data.CreateEmailToken(ctx, user.ID, EmailTokenVerify, "hash-two", expires); err != nil {
		t.Fatal(err)
	}
	if _, err := data.ConsumeEmailToken(ctx, EmailTokenVerify, "hash-one"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old token should be invalidated, err = %v", err)
	}
	if got, err := data.ConsumeEmailToken(ctx, EmailTokenVerify, "hash-two"); err != nil || got != user.ID {
		t.Fatalf("consume = %d, %v", got, err)
	}
	if _, err := data.ConsumeEmailToken(ctx, EmailTokenVerify, "hash-two"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("token should be single use, err = %v", err)
	}

	if err := data.CreateEmailToken(ctx, user.ID, EmailTokenReset, "hash-expired", time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := data.ConsumeEmailToken(ctx, EmailTokenReset, "hash-expired"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired token should fail, err = %v", err)
	}

	if err := data.SetEmailVerified(ctx, user.ID, true); err != nil {
		t.Fatal(err)
	}
	updated, err := data.UserByID(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !updated.EmailVerified {
		t.Fatal("email_verified was not persisted")
	}
	if err := data.UpdateEmail(ctx, user.ID, "new@example.com"); err != nil {
		t.Fatal(err)
	}
	updated, err = data.UserByID(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Email != "new@example.com" || updated.EmailVerified {
		t.Fatalf("updated user = %+v", updated)
	}
	found, err := data.UserByEmail(ctx, "NEW@example.com")
	if err != nil || found.ID != user.ID {
		t.Fatalf("UserByEmail = %+v, %v", found, err)
	}
	if _, err := data.CreateUser(ctx, "other", "NEW@example.com", "hash", false); !errors.Is(err, ErrEmailExists) {
		t.Fatalf("duplicate email error = %v", err)
	}
	second, err := data.CreateUser(ctx, "other", "other@example.com", "hash", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := data.UpdateEmail(ctx, second.ID, "new@example.com"); !errors.Is(err, ErrEmailExists) {
		t.Fatalf("update to duplicate email error = %v", err)
	}
}
