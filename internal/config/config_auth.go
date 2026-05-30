package config

// AuthConfig configures the multi-auth module (local, Entra ID, Google OAuth2).
type AuthConfig struct {
	Providers AuthProvidersConfig `json:"providers,omitempty"`
	Session   AuthSessionConfig   `json:"session,omitempty"`
}

// AuthProvidersConfig holds per-provider auth configuration.
// Only enabled providers are used; nil pointer = disabled.
type AuthProvidersConfig struct {
	Local   *LocalAuthConfig   `json:"local,omitempty"`
	EntraID *EntraIDAuthConfig `json:"entra_id,omitempty"`
	Google  *GoogleAuthConfig  `json:"google,omitempty"`
}

// LocalAuthConfig configures email+password authentication.
type LocalAuthConfig struct {
	Enabled bool `json:"enabled"`
}

// EntraIDAuthConfig configures Microsoft Entra ID (Azure AD) OIDC authentication.
// ClientSecret should be set via GOCLAW_ENTRA_CLIENT_SECRET env var, not in config.json.
type EntraIDAuthConfig struct {
	Enabled      bool   `json:"enabled"`
	ClientID     string `json:"client_id,omitempty"`
	ClientSecret string `json:"-"` // from env GOCLAW_ENTRA_CLIENT_SECRET only
	RedirectURI  string `json:"redirect_uri,omitempty"`
	TenantID     string `json:"tenant_id,omitempty"`
}

// GoogleAuthConfig configures Google OAuth2 authentication.
// ClientSecret should be set via GOCLAW_GOOGLE_CLIENT_SECRET env var, not in config.json.
type GoogleAuthConfig struct {
	Enabled      bool   `json:"enabled"`
	ClientID     string `json:"client_id,omitempty"`
	ClientSecret string `json:"-"` // from env GOCLAW_GOOGLE_CLIENT_SECRET only
	RedirectURI  string `json:"redirect_uri,omitempty"`
}

// AuthSessionConfig configures JWT session parameters.
type AuthSessionConfig struct {
	TimeoutMinutes int  `json:"timeout_minutes,omitempty"` // access token TTL (default 480 = 8h)
	RefreshEnabled bool `json:"refresh_enabled,omitempty"` // issue refresh tokens (default true)
}

// SessionTimeout returns the configured session timeout, defaulting to 8 hours.
func (s AuthSessionConfig) SessionTimeout() int {
	if s.TimeoutMinutes <= 0 {
		return 480
	}
	return s.TimeoutMinutes
}

// IsRefreshEnabled returns whether refresh tokens should be issued.
func (s AuthSessionConfig) IsRefreshEnabled() bool {
	return s.RefreshEnabled
}

// LocalEnabled returns true if local auth is configured and enabled.
func (a AuthConfig) LocalEnabled() bool {
	return a.Providers.Local != nil && a.Providers.Local.Enabled
}

// EntraIDEnabled returns true if Entra ID auth is configured and enabled.
func (a AuthConfig) EntraIDEnabled() bool {
	return a.Providers.EntraID != nil && a.Providers.EntraID.Enabled && a.Providers.EntraID.ClientID != ""
}

// GoogleEnabled returns true if Google auth is configured and enabled.
func (a AuthConfig) GoogleEnabled() bool {
	return a.Providers.Google != nil && a.Providers.Google.Enabled && a.Providers.Google.ClientID != ""
}

// EnabledProviders returns names of all enabled auth providers.
func (a AuthConfig) EnabledProviders() []string {
	var providers []string
	if a.LocalEnabled() {
		providers = append(providers, "local")
	}
	if a.EntraIDEnabled() {
		providers = append(providers, "entra_id")
	}
	if a.GoogleEnabled() {
		providers = append(providers, "google")
	}
	return providers
}
