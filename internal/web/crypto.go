package web

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"log/slog"
	"strings"
)

const encPrefix = "enc:"

// SensitiveFields are DNS provider config fields that should be encrypted.
var SensitiveFields = map[string]bool{
	"access_key":    true,
	"secret_key":    true,
	"client_secret": true,
	"api_key":       true,
}

// Encryptor handles AES-256-GCM encryption/decryption of sensitive values.
type Encryptor struct {
	key []byte // 32 bytes
}

// NewEncryptor creates an Encryptor from a base64-encoded key.
// If keyB64 is empty, a random key is generated (data won't survive restart).
func NewEncryptor(keyB64 string) *Encryptor {
	if keyB64 != "" {
		key, err := base64.StdEncoding.DecodeString(keyB64)
		if err == nil && len(key) == 32 {
			return &Encryptor{key: key}
		}
		slog.Warn("invalid encryption key, generating temporary key")
	} else {
		slog.Warn("SMGR_ENCRYPTION_KEY not set, generating temporary key — encrypted data won't survive restart")
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return &Encryptor{key: key}
}

// Encrypt encrypts a plaintext string using AES-256-GCM.
func (e *Encryptor) Encrypt(plaintext string) string {
	if plaintext == "" || strings.HasPrefix(plaintext, encPrefix) {
		return plaintext
	}
	block, err := aes.NewCipher(e.key)
	if err != nil {
		slog.Error("aes cipher creation failed", "error", err)
		return plaintext
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		slog.Error("gcm creation failed", "error", err)
		return plaintext
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		slog.Error("crypto/rand failed for nonce", "error", err)
		return plaintext
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return encPrefix + base64.StdEncoding.EncodeToString(ciphertext)
}

// Decrypt decrypts an AES-256-GCM encrypted string.
func (e *Encryptor) Decrypt(encrypted string) string {
	if encrypted == "" || !strings.HasPrefix(encrypted, encPrefix) {
		return encrypted
	}
	raw, err := base64.StdEncoding.DecodeString(encrypted[len(encPrefix):])
	if err != nil {
		slog.Error("base64 decode failed", "error", err)
		return ""
	}
	block, err := aes.NewCipher(e.key)
	if err != nil {
		return ""
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return ""
	}
	nonceSize := gcm.NonceSize()
	if len(raw) < nonceSize {
		return ""
	}
	plaintext, err := gcm.Open(nil, raw[:nonceSize], raw[nonceSize:], nil)
	if err != nil {
		slog.Error("decryption failed", "error", err)
		return ""
	}
	return string(plaintext)
}

// EncryptSensitiveFields encrypts sensitive fields in a config map.
func (e *Encryptor) EncryptSensitiveFields(cfg map[string]string) map[string]string {
	result := make(map[string]string, len(cfg))
	for k, v := range cfg {
		if SensitiveFields[k] && v != "" {
			result[k] = e.Encrypt(v)
		} else {
			result[k] = v
		}
	}
	return result
}

// DecryptSensitiveFields decrypts sensitive fields in a config map.
func (e *Encryptor) DecryptSensitiveFields(cfg map[string]string) map[string]string {
	result := make(map[string]string, len(cfg))
	for k, v := range cfg {
		if SensitiveFields[k] && v != "" {
			result[k] = e.Decrypt(v)
		} else {
			result[k] = v
		}
	}
	return result
}
