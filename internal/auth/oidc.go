package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// OIDCProvider holds configuration for an external OIDC identity provider.
type OIDCProvider struct {
	Name         string
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURI  string
	JWKSURI      string
	Scopes       []string
	AuthURL      string
	TokenURL     string
}

// OIDCClaims represents the standard claims extracted from an OIDC ID token.
type OIDCClaims struct {
	Sub           string `json:"sub"`
	Email         string `json:"email"`
	Name          string `json:"name"`
	EmailVerified bool   `json:"email_verified"`
	TID           string `json:"tid,omitempty"` // Entra ID tenant ID
}

// jwksCache caches JWKS keys from an external provider.
type jwksCache struct {
	mu       sync.RWMutex
	keys     map[string]any
	fetched  time.Time
	ttl      time.Duration
	jwksURL  string
	client   *http.Client
}

// newJWKSCache creates a new JWKS key cache.
func newJWKSCache(jwksURL string, ttl time.Duration) *jwksCache {
	return &jwksCache{
		keys:    make(map[string]any),
		ttl:     ttl,
		jwksURL: jwksURL,
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

// jwksResponse represents the JSON response from a JWKS endpoint.
type jwksResponse struct {
	Keys []json.RawMessage `json:"keys"`
}

// jwkKey represents a single JWK key with kid.
type jwkKey struct {
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	Use string `json:"use"`
	N   string `json:"n,omitempty"`
	E   string `json:"e,omitempty"`
	X   string `json:"x,omitempty"`
	Y   string `json:"y,omitempty"`
	Crv string `json:"crv,omitempty"`
}

// getKey fetches a key by kid from the cache, refreshing if stale.
func (c *jwksCache) getKey(ctx context.Context, kid string) (any, error) {
	c.mu.RLock()
	if key, ok := c.keys[kid]; ok && time.Since(c.fetched) < c.ttl {
		c.mu.RUnlock()
		return key, nil
	}
	c.mu.RUnlock()

	if err := c.refresh(ctx); err != nil {
		return nil, err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()
	if key, ok := c.keys[kid]; ok {
		return key, nil
	}
	return nil, fmt.Errorf("key with kid %q not found", kid)
}

func (c *jwksCache) refresh(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring write lock
	if time.Since(c.fetched) < c.ttl && len(c.keys) > 0 {
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.jwksURL, nil)
	if err != nil {
		return fmt.Errorf("jwks request: %w", err)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("jwks fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("jwks fetch: status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("jwks read: %w", err)
	}

	var jwks jwksResponse
	if err := json.Unmarshal(body, &jwks); err != nil {
		return fmt.Errorf("jwks parse: %w", err)
	}

	newKeys := make(map[string]any, len(jwks.Keys))
	for _, raw := range jwks.Keys {
		var jwk jwkKey
		if err := json.Unmarshal(raw, &jwk); err != nil {
			continue
		}
		if jwk.Kid == "" {
			continue
		}
		// Parse JWK to a usable public key
		// Use jwt.ParseRSAPublicKeyFromPEM is for PEM, not raw JWK.
		// Instead, decode the JWK math directly for RSA keys, or use
		// the key as-is in a format the jwt library can consume.
		key, err := parseJWK(jwk)
		if err != nil {
			continue
		}
		newKeys[jwk.Kid] = key
	}

	c.keys = newKeys
	c.fetched = time.Now()
	return nil
}

// ValidateOIDCToken validates an OIDC token from an external provider.
// It verifies signature via JWKS, checks issuer, audience, and expiry.
func ValidateOIDCToken(ctx context.Context, tokenStr string, provider *OIDCProvider, cache *jwksCache) (*OIDCClaims, error) {
	// Parse unverified to get kid from header
	unverifiedToken, _, err := jwt.NewParser().ParseUnverified(tokenStr, &jwt.MapClaims{})
	if err != nil {
		return nil, fmt.Errorf("parse token header: %w", err)
	}

	kid, _ := unverifiedToken.Header["kid"].(string)
	if kid == "" {
		return nil, fmt.Errorf("token missing kid header")
	}

	// Get the verification key
	key, err := cache.getKey(ctx, kid)
	if err != nil {
		return nil, fmt.Errorf("get verification key: %w", err)
	}

	// Parse and verify
	token, err := jwt.ParseWithClaims(tokenStr, &jwt.MapClaims{}, func(t *jwt.Token) (any, error) {
		return key, nil
	})
	if err != nil {
		return nil, fmt.Errorf("verify token: %w", err)
	}

	claims, ok := token.Claims.(*jwt.MapClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}

	// Verify issuer
	iss, _ := (*claims)["iss"].(string)
	if iss != provider.Issuer {
		return nil, fmt.Errorf("invalid issuer: %s", iss)
	}

	// Verify audience
	aud, _ := (*claims)["aud"].(string)
	if aud != provider.ClientID {
		// aud might be an array
		if audArr, ok := (*claims)["aud"].([]any); ok {
			found := false
			for _, a := range audArr {
				if s, ok := a.(string); ok && s == provider.ClientID {
					found = true
					break
				}
			}
			if !found {
				return nil, fmt.Errorf("invalid audience: %v", aud)
			}
		} else {
			return nil, fmt.Errorf("invalid audience: %v", aud)
		}
	}

	// Extract standard claims
	result := &OIDCClaims{}
	result.Sub, _ = (*claims)["sub"].(string)
	result.Email, _ = (*claims)["email"].(string)
	result.Name, _ = (*claims)["name"].(string)
	result.EmailVerified, _ = (*claims)["email_verified"].(bool)
	result.TID, _ = (*claims)["tid"].(string)

	if result.Sub == "" {
		return nil, fmt.Errorf("token missing sub claim")
	}

	return result, nil
}
