package auth

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

const (
	// Entra ID OIDC endpoints (public cloud).
	EntraCommonIssuer  = "https://login.microsoftonline.com/common/v2.0"
	EntraCommonAuthURL = "https://login.microsoftonline.com/common/oauth2/v2.0/authorize"
	EntraCommonTokenURL = "https://login.microsoftonline.com/common/oauth2/v2.0/token"
	EntraCommonJWKSURI = "https://login.microsoftonline.com/common/discovery/v2.0/keys"

	// Google OAuth2 endpoints.
	GoogleIssuer   = "https://accounts.google.com"
	GoogleAuthURL  = "https://accounts.google.com/o/oauth2/v2/auth"
	GoogleTokenURL = "https://oauth2.googleapis.com/token"
	GoogleJWKSURI  = "https://www.googleapis.com/oauth2/v3/certs"
)

// NewEntraIDProvider creates an OIDCProvider for Microsoft Entra ID.
func NewEntraIDProvider(clientID, clientSecret, redirectURI string) *OIDCProvider {
	return &OIDCProvider{
		Name:         "entra_id",
		Issuer:       EntraCommonIssuer,
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURI:  redirectURI,
		JWKSURI:      EntraCommonJWKSURI,
		Scopes:       []string{"openid", "email", "profile"},
		AuthURL:      EntraCommonAuthURL,
		TokenURL:     EntraCommonTokenURL,
	}
}

// NewGoogleProvider creates an OIDCProvider for Google OAuth2.
func NewGoogleProvider(clientID, clientSecret, redirectURI string) *OIDCProvider {
	return &OIDCProvider{
		Name:         "google",
		Issuer:       GoogleIssuer,
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURI:  redirectURI,
		JWKSURI:      GoogleJWKSURI,
		Scopes:       []string{"openid", "email", "profile"},
		AuthURL:      GoogleAuthURL,
		TokenURL:     GoogleTokenURL,
	}
}

// BuildAuthorizeURL constructs the authorization redirect URL for an OIDC provider.
func BuildAuthorizeURL(provider *OIDCProvider, state string) string {
	scopes := "openid"
	for _, s := range provider.Scopes {
		if s == "openid" {
			continue
		}
		scopes += " " + s
	}
	return fmt.Sprintf("%s?client_id=%s&redirect_uri=%s&response_type=code&scope=%s&state=%s",
		provider.AuthURL, provider.ClientID, provider.RedirectURI, scopes, state)
}

// ExchangeCode exchanges an authorization code for tokens at the provider's token endpoint.
func ExchangeCode(provider *OIDCProvider, code string) (*TokenResponse, error) {
	data := map[string][]string{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {provider.ClientID},
		"client_secret": {provider.ClientSecret},
		"redirect_uri":  {provider.RedirectURI},
	}

	resp, err := httpPostForm(provider.TokenURL, data)
	if err != nil {
		return nil, fmt.Errorf("token exchange: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token exchange: status %d", resp.StatusCode)
	}

	var tokenResp TokenResponse
	if err := decodeJSON(resp.Body, &tokenResp); err != nil {
		return nil, fmt.Errorf("token exchange decode: %w", err)
	}
	return &tokenResp, nil
}

// TokenResponse represents the response from an OIDC token exchange.
type TokenResponse struct {
	AccessToken string `json:"access_token"`
	IDToken     string `json:"id_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
	Scope       string `json:"scope"`
}

// OIDCValidator validates OIDC tokens from external providers.
type OIDCValidator struct {
	providers map[string]*OIDCProvider
	caches    map[string]*jwksCache
}

// NewOIDCValidator creates a validator for the given providers.
func NewOIDCValidator(providers []*OIDCProvider) *OIDCValidator {
	v := &OIDCValidator{
		providers: make(map[string]*OIDCProvider, len(providers)),
		caches:    make(map[string]*jwksCache, len(providers)),
	}
	for _, p := range providers {
		v.providers[p.Name] = p
		v.caches[p.Name] = newJWKSCache(p.JWKSURI, 24*time.Hour)
	}
	return v
}

// Validate validates an OIDC token by determining the provider from the issuer.
func (v *OIDCValidator) Validate(ctx context.Context, tokenStr string) (*OIDCClaims, *OIDCProvider, error) {
	// Try each provider until one succeeds
	for name, provider := range v.providers {
		cache := v.caches[name]
		claims, err := ValidateOIDCToken(ctx, tokenStr, provider, cache)
		if err == nil {
			return claims, provider, nil
		}
	}
	return nil, nil, fmt.Errorf("token did not validate against any configured provider")
}
