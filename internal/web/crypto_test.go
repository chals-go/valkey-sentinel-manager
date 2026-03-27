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

func TestNewEncryptor_EmptyKey(t *testing.T) {
	// Should not panic; generates a temporary key.
	enc := NewEncryptor("")
	if enc == nil {
		t.Fatal("NewEncryptor('') should not return nil")
	}
	// Round-trip should still work with temporary key.
	encrypted := enc.Encrypt("test")
	decrypted := enc.Decrypt(encrypted)
	if decrypted != "test" {
		t.Fatalf("round-trip failed with temp key: got %q", decrypted)
	}
}

func TestNewEncryptor_InvalidKey(t *testing.T) {
	// Invalid base64 → generates temporary key.
	enc := NewEncryptor("not-valid-base64!!!")
	if enc == nil {
		t.Fatal("NewEncryptor with invalid key should not return nil")
	}
	encrypted := enc.Encrypt("test")
	decrypted := enc.Decrypt(encrypted)
	if decrypted != "test" {
		t.Fatalf("round-trip failed with fallback key: got %q", decrypted)
	}
}

func TestDecrypt_CorruptedData(t *testing.T) {
	enc := testEncryptor(t)
	// Corrupted encrypted data with valid prefix.
	corrupted := encPrefix + base64.StdEncoding.EncodeToString([]byte("garbage data"))
	result := enc.Decrypt(corrupted)
	if result != "" {
		t.Fatalf("corrupted data should return empty, got %q", result)
	}
}

func TestDecrypt_WrongKey(t *testing.T) {
	enc1 := testEncryptor(t)
	encrypted := enc1.Encrypt("secret-value")

	// Different key.
	key2 := make([]byte, 32)
	for i := range key2 {
		key2[i] = byte(i + 100)
	}
	enc2 := NewEncryptor(base64.StdEncoding.EncodeToString(key2))

	result := enc2.Decrypt(encrypted)
	if result == "secret-value" {
		t.Fatal("decrypting with wrong key should not return original value")
	}
	if result != "" {
		t.Fatalf("decrypting with wrong key should return empty, got %q", result)
	}
}

func TestEncryptDecrypt_SpecialChars(t *testing.T) {
	enc := testEncryptor(t)
	special := "한글テスト🎉 <script>alert('xss')</script> password=p@$$w0rd!"
	encrypted := enc.Encrypt(special)
	decrypted := enc.Decrypt(encrypted)
	if decrypted != special {
		t.Fatalf("special chars round-trip failed: got %q", decrypted)
	}
}

func TestEncryptSensitiveFields_ApiToken(t *testing.T) {
	enc := testEncryptor(t)
	cfg := map[string]string{
		"type":      "cloudflare",
		"api_token": "cf-token-secret",
		"zone_id":   "zone123",
	}

	encrypted := enc.EncryptSensitiveFields(cfg)
	if !strings.HasPrefix(encrypted["api_token"], encPrefix) {
		t.Fatal("api_token should be encrypted (Cloudflare field)")
	}
	if encrypted["zone_id"] != "zone123" {
		t.Fatal("zone_id should not be encrypted")
	}

	decrypted := enc.DecryptSensitiveFields(encrypted)
	if decrypted["api_token"] != "cf-token-secret" {
		t.Fatalf("decrypted api_token = %q", decrypted["api_token"])
	}
}
