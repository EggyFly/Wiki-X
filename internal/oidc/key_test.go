package oidc

import (
	"bytes"
	"strings"
	"testing"
)

const testKeyEncryptionKey = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="

func TestPrivateKeyEncryption(t *testing.T) {
	key, err := decodeKeyEncryptionKey(testKeyEncryptionKey)
	if err != nil {
		t.Fatal(err)
	}
	plain := "-----BEGIN PRIVATE KEY-----\ntest\n-----END PRIVATE KEY-----\n"
	encrypted, err := encryptPrivateKeyPEM(plain, key)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(encrypted, oidcKeyPrefix) || strings.Contains(encrypted, "PRIVATE KEY") {
		t.Fatalf("encrypted key = %q", encrypted)
	}
	decrypted, err := decryptPrivateKeyPEM(encrypted, key)
	if err != nil {
		t.Fatal(err)
	}
	if decrypted != plain {
		t.Fatalf("decrypted key = %q", decrypted)
	}
	if _, err := decryptPrivateKeyPEM(encrypted, bytes.Repeat([]byte{1}, 32)); err == nil {
		t.Fatal("wrong key should fail")
	}
	if _, err := decodeKeyEncryptionKey("bad"); err == nil {
		t.Fatal("bad key should fail")
	}
}
