package store

import (
	"context"
	"testing"
)

func TestRecoveryCodeStore(t *testing.T) {
	data := openTestStore(t)
	ctx := context.Background()
	user, err := data.CreateUser(ctx, "recoveryuser", "", "hash", false)
	if err != nil {
		t.Fatal(err)
	}
	hashes := []string{"hash-one", "hash-two"}
	if err := data.ReplaceRecoveryCodes(ctx, user.ID, hashes, user.ID); err != nil {
		t.Fatal(err)
	}
	count, err := data.CountRecoveryCodes(ctx, user.ID)
	if err != nil || count != 2 {
		t.Fatalf("count = %d, %v", count, err)
	}
	used, err := data.ConsumeRecoveryCode(ctx, user.ID, "hash-one")
	if err != nil || !used {
		t.Fatalf("consume first = %v, %v", used, err)
	}
	used, err = data.ConsumeRecoveryCode(ctx, user.ID, "hash-one")
	if err != nil || used {
		t.Fatalf("consume reused = %v, %v", used, err)
	}
	count, err = data.CountRecoveryCodes(ctx, user.ID)
	if err != nil || count != 1 {
		t.Fatalf("count after use = %d, %v", count, err)
	}
	if err := data.ReplaceRecoveryCodes(ctx, user.ID, []string{"hash-three"}, user.ID); err != nil {
		t.Fatal(err)
	}
	count, err = data.CountRecoveryCodes(ctx, user.ID)
	if err != nil || count != 1 {
		t.Fatalf("count after replace = %d, %v", count, err)
	}
	used, err = data.ConsumeRecoveryCode(ctx, user.ID, "hash-two")
	if err != nil || used {
		t.Fatalf("old code after replace = %v, %v", used, err)
	}

	if err := data.SetTOTPSecret(ctx, user.ID, "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP"); err != nil {
		t.Fatal(err)
	}
	if err := data.EnableTOTP(ctx, user.ID); err != nil {
		t.Fatal(err)
	}
	if err := data.DisableTOTP(ctx, user.ID); err != nil {
		t.Fatal(err)
	}
	count, err = data.CountRecoveryCodes(ctx, user.ID)
	if err != nil || count != 0 {
		t.Fatalf("count after disable = %d, %v", count, err)
	}
	if ok, err := data.HasAudit(ctx, "user.recovery_codes_generate", "recoveryuser"); err != nil || !ok {
		t.Fatalf("recovery audit = %v, %v", ok, err)
	}
}
