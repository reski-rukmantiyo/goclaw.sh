package auth

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// JWT claims for GoClaw session tokens.
type Claims struct {
	jwt.RegisteredClaims
	Email string `json:"email"`
	TID   string `json:"tid"`   // tenant ID
	Role  string `json:"role"`  // user role
}

// JWTManager handles signing and validation of JWT session tokens.
type JWTManager struct {
	signingKey    *ecdsa.PrivateKey
	signingMethod jwt.SigningMethod
	issuer        string
	audience      string
	accessTTL     time.Duration
}

// NewJWTManager generates a new ECDSA P-256 signing key and returns a manager.
func NewJWTManager(issuer, audience string, accessTTL time.Duration) (*JWTManager, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate signing key: %w", err)
	}
	return &JWTManager{
		signingKey:    key,
		signingMethod: jwt.SigningMethodES256,
		issuer:        issuer,
		audience:      audience,
		accessTTL:     accessTTL,
	}, nil
}

// IssueAccessToken creates a signed JWT access token for the given user.
func (m *JWTManager) IssueAccessToken(userID uuid.UUID, email string, tenantID uuid.UUID, role string) (string, error) {
	now := time.Now()
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID.String(),
			Issuer:    m.issuer,
			Audience:  []string{m.audience},
			ExpiresAt: jwt.NewNumericDate(now.Add(m.accessTTL)),
			IssuedAt:  jwt.NewNumericDate(now),
			ID:        uuid.Must(uuid.NewV7()).String(),
		},
		Email: email,
		TID:   tenantID.String(),
		Role:  role,
	}
	token := jwt.NewWithClaims(m.signingMethod, claims)
	return token.SignedString(m.signingKey)
}

// ValidateToken parses and validates a JWT token string.
// Returns the claims if valid, or an error.
func (m *JWTManager) ValidateToken(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		if t.Method != m.signingMethod {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return &m.signingKey.PublicKey, nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token claims")
	}
	return claims, nil
}

// sha256Hex returns the hex-encoded SHA-256 hash of a string.
func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// VerifyTokenHash checks a raw token against its stored SHA-256 hex hash.
func VerifyTokenHash(raw, storedHash string) bool {
	return subtle.ConstantTimeCompare([]byte(sha256Hex(raw)), []byte(storedHash)) == 1
}

// SigningKeyPublicKey returns the public key for external verification (e.g. JWKS endpoint).
func (m *JWTManager) PublicKey() crypto.PublicKey {
	return &m.signingKey.PublicKey
}
