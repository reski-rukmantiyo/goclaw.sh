package auth

import (
	"testing"
)

func TestHashPassword(t *testing.T) {
	password := "SecureP@ssw0rd!"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if hash == "" {
		t.Fatal("HashPassword returned empty hash")
	}
	if hash == password {
		t.Fatal("hash should not equal plaintext")
	}
}

func TestCheckPassword(t *testing.T) {
	password := "SecureP@ssw0rd!"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	if !CheckPassword(password, hash) {
		t.Error("CheckPassword should return true for correct password")
	}
	if CheckPassword("wrong", hash) {
		t.Error("CheckPassword should return false for wrong password")
	}
}

func TestGenerateRefreshToken(t *testing.T) {
	raw, hash, err := GenerateRefreshToken()
	if err != nil {
		t.Fatalf("GenerateRefreshToken: %v", err)
	}
	if raw == "" {
		t.Fatal("raw token should not be empty")
	}
	if hash == "" {
		t.Fatal("hash should not be empty")
	}
	if raw == hash {
		t.Fatal("raw and hash should differ")
	}
	if len(raw) != 64 {
		t.Fatalf("raw token should be 64 hex chars, got %d", len(raw))
	}
}

func TestVerifyTokenHash(t *testing.T) {
	raw, hash, _ := GenerateRefreshToken()
	if !VerifyTokenHash(raw, hash) {
		t.Error("VerifyTokenHash should return true for matching token")
	}
	if VerifyTokenHash("wrong", hash) {
		t.Error("VerifyTokenHash should return false for wrong token")
	}
}

func BenchmarkHashPassword(b *testing.B) {
	password := "BenchmarkP@ss123"
	for i := 0; i < b.N; i++ {
		_, _ = HashPassword(password)
	}
}

func BenchmarkCheckPassword(b *testing.B) {
	password := "BenchmarkP@ss123"
	hash, _ := HashPassword(password)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = CheckPassword(password, hash)
	}
}
