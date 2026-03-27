package web

import "testing"

func TestHashPassword_Verify(t *testing.T) {
	password := "mypassword123"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword error: %v", err)
	}

	if !VerifyHash(password, hash) {
		t.Fatal("VerifyHash should return true for correct password")
	}
	if VerifyHash("wrongpassword", hash) {
		t.Fatal("VerifyHash should return false for incorrect password")
	}
}

func TestHashPassword_UniqueSalt(t *testing.T) {
	hash1, err := HashPassword("same")
	if err != nil {
		t.Fatalf("HashPassword error: %v", err)
	}
	hash2, err := HashPassword("same")
	if err != nil {
		t.Fatalf("HashPassword error: %v", err)
	}
	if hash1 == hash2 {
		t.Fatal("two calls with same password should produce different hashes (different salt)")
	}
	if !VerifyHash("same", hash1) || !VerifyHash("same", hash2) {
		t.Fatal("both hashes should verify")
	}
}

func TestGenerateAPIToken(t *testing.T) {
	token, err := GenerateAPIToken()
	if err != nil {
		t.Fatalf("GenerateAPIToken error: %v", err)
	}
	if len(token) < 10 {
		t.Fatalf("token too short: %q", token)
	}
	if token[:5] != "smgr_" {
		t.Fatalf("token should start with 'smgr_', got %q", token[:5])
	}

	token2, err := GenerateAPIToken()
	if err != nil {
		t.Fatalf("GenerateAPIToken error: %v", err)
	}
	if token == token2 {
		t.Fatal("two generated tokens should differ")
	}
}

func TestVerifyHash_LegacySHA256(t *testing.T) {
	// Legacy format: plain SHA-256 without salt (no '$' separator).
	// sha256("admin") = 8c6976e5b5410415bde908bd4dee15dfb167a9c873fc4bb8a81f6f2ab448a918
	legacyHash := "8c6976e5b5410415bde908bd4dee15dfb167a9c873fc4bb8a81f6f2ab448a918"
	if !VerifyHash("admin", legacyHash) {
		t.Fatal("legacy SHA-256 hash should verify for 'admin'")
	}
	if VerifyHash("wrong", legacyHash) {
		t.Fatal("legacy SHA-256 hash should not verify for wrong password")
	}
}

func TestVerifyHash_WrongPassword(t *testing.T) {
	hash, err := HashPassword("correct-password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if VerifyHash("wrong-password", hash) {
		t.Fatal("wrong password should not verify")
	}
}

func TestHashPassword_Uniqueness(t *testing.T) {
	hashes := make(map[string]bool)
	for i := 0; i < 5; i++ {
		h, err := HashPassword("same-password")
		if err != nil {
			t.Fatalf("HashPassword: %v", err)
		}
		if hashes[h] {
			t.Fatal("hash collision: same password should produce unique hashes")
		}
		hashes[h] = true
	}
}
