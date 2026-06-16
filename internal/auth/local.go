package auth

import (
	"crypto/rand"
	"encoding/hex"
	"unicode"

	"golang.org/x/crypto/bcrypt"
)

const (
	bcryptCost        = 12
	MinPasswordLength = 8
)

// ValidatePasswordComplexity checks minimum length, uppercase letter, digit, and symbol.
func ValidatePasswordComplexity(password string) (lengthOK, hasUpper, hasDigit, hasSymbol bool) {
	lengthOK = len(password) >= MinPasswordLength
	for _, r := range password {
		if unicode.IsUpper(r) {
			hasUpper = true
		}
		if unicode.IsDigit(r) {
			hasDigit = true
		}
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			hasSymbol = true
		}
	}
	return
}

// HashPassword hashes a plaintext password using bcrypt.
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// CheckPassword compares a plaintext password against a bcrypt hash.
func CheckPassword(password, hash string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// GenerateRefreshToken creates a cryptographically random refresh token.
// Returns the raw token (to give to client) and its SHA-256 hex hash (to store).
func GenerateRefreshToken() (raw string, hash string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", err
	}
	raw = hex.EncodeToString(b)
	// Hash with same method as API keys for consistency
	hash = SHA256Hex(raw)
	return raw, hash, nil
}
