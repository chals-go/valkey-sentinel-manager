package web

import "testing"

func TestHashPassword_Verify(t *testing.T) {
	password := "mypassword123"
	hash := HashPassword(password)

	if !VerifyHash(password, hash) {
		t.Fatal("VerifyHash should return true for correct password")
	}
	if VerifyHash("wrongpassword", hash) {
		t.Fatal("VerifyHash should return false for incorrect password")
	}
}

func TestHashPassword_UniqueSalt(t *testing.T) {
	hash1 := HashPassword("same")
	hash2 := HashPassword("same")
	if hash1 == hash2 {
		t.Fatal("two calls with same password should produce different hashes (different salt)")
	}
	// Both should verify correctly.
	if !VerifyHash("same", hash1) || !VerifyHash("same", hash2) {
		t.Fatal("both hashes should verify")
	}
}

func TestGenerateAPIToken(t *testing.T) {
	token := GenerateAPIToken()
	if len(token) < 10 {
		t.Fatalf("token too short: %q", token)
	}
	if token[:5] != "smgr_" {
		t.Fatalf("token should start with 'smgr_', got %q", token[:5])
	}

	// Two tokens should differ.
	token2 := GenerateAPIToken()
	if token == token2 {
		t.Fatal("two generated tokens should differ")
	}
}
