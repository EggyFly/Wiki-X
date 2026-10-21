package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestTOTPStore(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	admin, err := s.CreateUser(ctx, "admin", "", "hash", true)
	if err != nil {
		t.Fatal(err)
	}
	secret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	if err := s.SetTOTPSecret(ctx, admin.ID, secret); err != nil {
		t.Fatal(err)
	}
	user, err := s.UserByID(ctx, admin.ID)
	if err != nil || user.TOTPSecret != secret || user.TOTPEnabled {
		t.Fatalf("pending user = %+v, %v", user, err)
	}
	if err := s.EnableTOTP(ctx, admin.ID); err != nil {
		t.Fatal(err)
	}
	user, err = s.UserByID(ctx, admin.ID)
	if err != nil || !user.TOTPEnabled || user.TOTPSecret != secret {
		t.Fatalf("enabled user = %+v, %v", user, err)
	}
	if err := s.DisableTOTP(ctx, admin.ID); err != nil {
		t.Fatal(err)
	}
	user, err = s.UserByID(ctx, admin.ID)
	if err != nil || user.TOTPEnabled || user.TOTPSecret != "" {
		t.Fatalf("disabled user = %+v, %v", user, err)
	}
	if err := s.EnableTOTP(ctx, admin.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("enable without secret = %v", err)
	}
	if err := s.SetTOTPSecret(ctx, 99999, secret); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing user err = %v", err)
	}
}

func TestTOTPSecretWindow(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	user, err := s.CreateUser(ctx, "editor", "", "hash", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetTOTPSecret(ctx, user.ID, "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"); err != nil {
		t.Fatal(err)
	}
	loaded, err := s.UserByID(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.TOTPSecretSetAt.IsZero() {
		t.Fatal("secret set time not recorded")
	}
}

func TestTOTPSessionAndReplay(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	user, err := s.CreateUser(ctx, "admin", "", "hash", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetTOTPSecret(ctx, user.ID, "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"); err != nil {
		t.Fatal(err)
	}
	if err := s.EnableTOTP(ctx, user.ID); err != nil {
		t.Fatal(err)
	}

	tokenHash := "totp-test-session-hash"
	if err := s.CreateSession(ctx, tokenHash, user.ID, "csrf-token", time.Now().Add(time.Hour), "test", "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetSession(ctx, tokenHash); err != nil {
		t.Fatalf("session before disable = %v", err)
	}
	if err := s.DisableTOTP(ctx, user.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetSession(ctx, tokenHash); !errors.Is(err, ErrNotFound) {
		t.Fatalf("session after disable = %v", err)
	}

	if err := s.SetTOTPSecret(ctx, user.ID, "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"); err != nil {
		t.Fatal(err)
	}
	if err := s.EnableTOTP(ctx, user.ID); err != nil {
		t.Fatal(err)
	}
	fresh, err := s.ConsumeTOTP(ctx, user.ID, 100)
	if err != nil || !fresh {
		t.Fatalf("first consume = %v, %v", fresh, err)
	}
	fresh, err = s.ConsumeTOTP(ctx, user.ID, 100)
	if err != nil || fresh {
		t.Fatalf("replayed step accepted: %v, %v", fresh, err)
	}
	fresh, err = s.ConsumeTOTP(ctx, user.ID, 101)
	if err != nil || !fresh {
		t.Fatalf("next step rejected: %v, %v", fresh, err)
	}
	fresh, err = s.ConsumeTOTP(ctx, user.ID, 100)
	if err != nil || fresh {
		t.Fatalf("older step accepted after newer: %v, %v", fresh, err)
	}
	if err := s.DisableTOTP(ctx, user.ID); err != nil {
		t.Fatal(err)
	}
	loaded, err := s.UserByID(ctx, user.ID)
	if err != nil || loaded.TOTPSecret != "" || loaded.TOTPEnabled {
		t.Fatalf("user after disable = %+v, %v", loaded, err)
	}
}
