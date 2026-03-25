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

// SensitiveFields는 암호화 대상인 DNS 프로바이더 설정 필드 목록이다.
var SensitiveFields = map[string]bool{
	"access_key":    true,
	"secret_key":    true,
	"client_secret": true,
	"api_key":       true,
}

// Encryptor는 AES-256-GCM 방식으로 민감한 값의 암호화와 복호화를 처리하는 구조체다.
type Encryptor struct {
	key []byte // 32 bytes
}

// NewEncryptor는 base64로 인코딩된 키로 Encryptor를 생성한다.
// keyB64가 비어있으면 임시 무작위 키를 생성하며, 이 경우 재시작 후 데이터 복호화가 불가능하다.
func NewEncryptor(keyB64 string) *Encryptor {
	if keyB64 != "" {
		key, err := base64.StdEncoding.DecodeString(keyB64)
		if err == nil && len(key) == 32 {
			return &Encryptor{key: key}
		}
		slog.Error("invalid encryption key format, generating temporary key — encrypted data will be LOST on restart")
	} else {
		slog.Error("encryption_key not set, generating temporary key — encrypted data will be LOST on restart. Set encryption_key in config.yaml")
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return &Encryptor{key: key}
}

// Encrypt는 평문 문자열을 AES-256-GCM 방식으로 암호화하여 반환한다.
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

// Decrypt는 AES-256-GCM으로 암호화된 문자열을 복호화하여 반환한다.
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

// EncryptSensitiveFields는 설정 맵에서 SensitiveFields에 해당하는 필드를 암호화하여 새 맵으로 반환한다.
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

// DecryptSensitiveFields는 설정 맵에서 SensitiveFields에 해당하는 필드를 복호화하여 새 맵으로 반환한다.
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
