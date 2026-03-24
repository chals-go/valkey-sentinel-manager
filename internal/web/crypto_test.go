package web

import (
	"encoding/base64"
	"strings"
	"testing"
)

func testEncryptor(t *testing.T) *Encryptor {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	return NewEncryptor(base64.StdEncoding.EncodeToString(key))
}

func TestEncryptor_RoundTrip(t *testing.T) {
	enc := testEncryptor(t)
	original := "my-secret-value"

	encrypted := enc.Encrypt(original)
	if encrypted == original {
		t.Fatal("encrypted should differ from original")
	}
	if !strings.HasPrefix(encrypted, encPrefix) {
		t.Fatalf("encrypted should start with %q", encPrefix)
	}

	decrypted := enc.Decrypt(encrypted)
	if decrypted != original {
		t.Fatalf("Decrypt = %q, want %q", decrypted, original)
	}
}

func TestEncryptor_EmptyValue(t *testing.T) {
	enc := testEncryptor(t)

	if got := enc.Encrypt(""); got != "" {
		t.Fatalf("Encrypt('') = %q, want empty", got)
	}
	if got := enc.Decrypt(""); got != "" {
		t.Fatalf("Decrypt('') = %q, want empty", got)
	}
}

func TestEncryptor_AlreadyEncrypted(t *testing.T) {
	enc := testEncryptor(t)
	encrypted := enc.Encrypt("secret")

	// Encrypting again should return the same value (no double-encrypt).
	again := enc.Encrypt(encrypted)
	if again != encrypted {
		t.Fatal("should not double-encrypt an already encrypted value")
	}
}

func TestEncryptor_PlaintextPassthrough(t *testing.T) {
	enc := testEncryptor(t)

	// Decrypt of a non-encrypted value should return as-is.
	plain := "just-plain-text"
	if got := enc.Decrypt(plain); got != plain {
		t.Fatalf("Decrypt(plaintext) = %q, want %q", got, plain)
	}
}

func TestEncryptSensitiveFields(t *testing.T) {
	enc := testEncryptor(t)
	cfg := map[string]string{
		"type":       "route53",
		"zone_id":    "Z12345",
		"access_key": "AKIATEST",
		"secret_key": "secrettest",
		"region":     "ap-northeast-2",
	}

	encrypted := enc.EncryptSensitiveFields(cfg)
	if !strings.HasPrefix(encrypted["access_key"], encPrefix) {
		t.Fatal("access_key should be encrypted")
	}
	if !strings.HasPrefix(encrypted["secret_key"], encPrefix) {
		t.Fatal("secret_key should be encrypted")
	}
	if encrypted["zone_id"] != "Z12345" {
		t.Fatal("non-sensitive field should not be encrypted")
	}

	decrypted := enc.DecryptSensitiveFields(encrypted)
	if decrypted["access_key"] != "AKIATEST" {
		t.Fatalf("decrypted access_key = %q", decrypted["access_key"])
	}
	if decrypted["secret_key"] != "secrettest" {
		t.Fatalf("decrypted secret_key = %q", decrypted["secret_key"])
	}
}
