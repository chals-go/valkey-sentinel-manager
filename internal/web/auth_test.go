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
