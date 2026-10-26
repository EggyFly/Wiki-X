package oidc

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

const oidcKeyPrefix = "v1:"

func decodeKeyEncryptionKey(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	for _, encoding := range []*base64.Encoding{
		base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding,
	} {
		key, err := encoding.DecodeString(value)
		if err == nil && len(key) == 32 {
			return key, nil
		}
	}
	return nil, errors.New("oidc.key_encryption_key must be 32 bytes base64")
}

func encryptPrivateKeyPEM(value string, key []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := aead.Seal(nonce, nonce, []byte(value), nil)
	return oidcKeyPrefix + base64.RawURLEncoding.EncodeToString(sealed), nil
}

func decryptPrivateKeyPEM(value string, key []byte) (string, error) {
	if !strings.HasPrefix(value, oidcKeyPrefix) {
		return "", errors.New("oidc private key is not encrypted")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, oidcKeyPrefix))
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < aead.NonceSize() {
		return "", fmt.Errorf("oidc private key is too short")
	}
	plain, err := aead.Open(nil, raw[:aead.NonceSize()], raw[aead.NonceSize():], nil)
	if err != nil {
		return "", fmt.Errorf("decrypt oidc private key: %w", err)
	}
	return string(plain), nil
}
