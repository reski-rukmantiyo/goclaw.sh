package http

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/auth"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tenantauth"
)

// stateEntry tracks CSRF state along with the tenant that initiated the flow.
type stateEntry struct {
	tenantID  uuid.UUID
	expiresAt time.Time
}

// OIDCHandler handles OIDC authorization and callback endpoints.
type OIDCHandler struct {
	users   store.UserStore
	groups  store.GroupStore
	tenants store.TenantStore
	loader  tenantauth.Loader
	jwt     *auth.JWTManager
	states  map[string]stateEntry
	stateMu sync.RWMutex
}

// NewOIDCHandler creates a handler for OIDC endpoints.
func NewOIDCHandler(users store.UserStore, groups store.GroupStore, tenants store.TenantStore, loader tenantauth.Loader, jwt *auth.JWTManager) *OIDCHandler {
	return &OIDCHandler{
		users:   users,
		groups:  groups,
		tenants: tenants,
		loader:  loader,
		jwt:     jwt,
		states:  make(map[string]stateEntry),
	}
}

// RegisterRoutes registers all OIDC routes on the given mux.
func (h *OIDCHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/entra/authorize", h.handleAuthorize("entra_id"))
	mux.HandleFunc("GET /auth/entra/callback", h.handleCallback("entra_id"))
	mux.HandleFunc("GET /auth/google/authorize", h.handleAuthorize("google"))
	mux.HandleFunc("GET /auth/google/callback", h.handleCallback("google"))
}

// storeState saves a CSRF state token with 10-minute TTL.
func (h *OIDCHandler) storeState(state string, tenantID uuid.UUID) {
	h.stateMu.Lock()
	h.states[state] = stateEntry{tenantID: tenantID, expiresAt: time.Now().Add(10 * time.Minute)}
	h.stateMu.Unlock()
}

// validateAndConsumeState checks a state token and removes it (one-time use).
func (h *OIDCHandler) validateAndConsumeState(state string) (uuid.UUID, bool) {
	h.stateMu.Lock()
	defer h.stateMu.Unlock()
	entry, ok := h.states[state]
	if !ok {
		return uuid.Nil, false
	}
	delete(h.states, state)
	return entry.tenantID, time.Now().Before(entry.expiresAt)
}

// pruneStates removes expired state tokens.
func (h *OIDCHandler) pruneStates() {
	h.stateMu.Lock()
	now := time.Now()
	for s, entry := range h.states {
		if now.After(entry.expiresAt) {
			delete(h.states, s)
		}
	}
	h.stateMu.Unlock()
}

// buildProvidersFromConfig creates OIDC providers from tenant auth config.
func buildProvidersFromConfig(cfg *config.AuthConfig) []*auth.OIDCProvider {
	var providers []*auth.OIDCProvider
	if cfg.EntraIDEnabled() {
		providers = append(providers, auth.NewEntraIDProvider(
			cfg.Providers.EntraID.ClientID,
			cfg.Providers.EntraID.ClientSecret,
			cfg.Providers.EntraID.RedirectURI,
		))
	}
	if cfg.GoogleEnabled() {
		providers = append(providers, auth.NewGoogleProvider(
			cfg.Providers.Google.ClientID,
			cfg.Providers.Google.ClientSecret,
			cfg.Providers.Google.RedirectURI,
		))
	}
	return providers
}

// handleAuthorize redirects to the OIDC provider's authorization URL.
func (h *OIDCHandler) handleAuthorize(providerName string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		tenantID := resolveTenantForAuth(r)

		cfg, err := h.loader.LoadAuthConfig(ctx, tenantID)
		if err != nil {
			slog.Warn("auth.oidc_load_config_failed", "tenant_id", tenantID, "error", err)
			writeError(w, http.StatusNotFound, "provider_not_found", "auth provider not configured")
			return
		}

		providers := buildProvidersFromConfig(cfg)
		var provider *auth.OIDCProvider
		for _, p := range providers {
			if p.Name == providerName {
				provider = p
				break
			}
		}
		if provider == nil {
			writeError(w, http.StatusNotFound, "provider_not_found", "auth provider not configured")
			return
		}

		state := uuid.Must(uuid.NewV7()).String()
		h.storeState(state, tenantID)

		// Prune expired states periodically
		h.pruneStates()

		authURL := auth.BuildAuthorizeURL(provider, state)
		http.Redirect(w, r, authURL, http.StatusFound)
	}
}

// handleCallback handles the OIDC callback, validates the code, auto-provisions user.
func (h *OIDCHandler) handleCallback(providerName string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		locale := extractLocale(r)
		ctx := r.Context()

		code := r.URL.Query().Get("code")
		state := r.URL.Query().Get("state")
		if code == "" || state == "" {
			writeError(w, http.StatusBadRequest, "invalid_request", "missing code or state parameter")
			return
		}

		tenantID, ok := h.validateAndConsumeState(state)
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid_state", i18n.T(locale, i18n.MsgAuthStateInvalid))
			return
		}

		cfg, err := h.loader.LoadAuthConfig(ctx, tenantID)
		if err != nil {
			slog.Warn("auth.oidc_load_config_failed", "tenant_id", tenantID, "error", err)
			writeError(w, http.StatusNotFound, "provider_not_found", "auth provider not configured")
			return
		}

		providers := buildProvidersFromConfig(cfg)
		var provider *auth.OIDCProvider
		for _, p := range providers {
			if p.Name == providerName {
				provider = p
				break
			}
		}
		if provider == nil {
			writeError(w, http.StatusNotFound, "provider_not_found", "auth provider not configured")
			return
		}

		// Exchange code for tokens
		tokenResp, err := auth.ExchangeCode(provider, code)
		if err != nil {
			slog.Error("auth.oidc_code_exchange_failed", "provider", providerName, "error", err)
			writeError(w, http.StatusUnauthorized, "oidc_failed", i18n.T(locale, i18n.MsgAuthOIDCFailed, err.Error()))
			return
		}

		if tokenResp.IDToken == "" {
			writeError(w, http.StatusUnauthorized, "oidc_failed", i18n.T(locale, i18n.MsgAuthOIDCFailed, "no id_token"))
			return
		}

		// Validate ID token
		validator := auth.NewOIDCValidator(providers)
		claims, _, err := validator.Validate(ctx, tokenResp.IDToken)
		if err != nil {
			slog.Error("auth.oidc_token_validation_failed", "provider", providerName, "error", err)
			writeError(w, http.StatusUnauthorized, "oidc_failed", i18n.T(locale, i18n.MsgAuthOIDCFailed, err.Error()))
			return
		}

		// Auto-provision user
		user, err := h.resolveOrCreateUser(ctx, claims, providerName, tenantID)
		if err != nil {
			slog.Error("auth.user_provision_failed", "provider", providerName, "email", claims.Email, "error", err)
			writeError(w, http.StatusInternalServerError, "internal_error", i18n.T(locale, i18n.MsgInternalError, err.Error()))
			return
		}

		if user.Status != store.UserStatusActive {
			writeError(w, http.StatusForbidden, "account_suspended", i18n.T(locale, i18n.MsgAuthAccountSuspended))
			return
		}

		// Determine role from tenant_users membership
		role := resolveUserRoleForJWT(r.Context(), h.tenants, tenantID, user.ID.String())

		// Issue JWT session token
		accessToken, err := h.jwt.IssueAccessToken(user.ID, user.Email, tenantID, role)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", i18n.T(locale, i18n.MsgInternalError, "token issue"))
			return
		}

		// Return token as JSON (frontend will handle redirect)
		writeJSON(w, http.StatusOK, map[string]any{
			"access_token": accessToken,
			"token_type":   "Bearer",
			"expires_in":   28800,
			"user":         user,
		})
	}
}

// resolveOrCreateUser implements auto-provisioning (SRS §3.2 FR-U1).
func (h *OIDCHandler) resolveOrCreateUser(ctx context.Context, claims *auth.OIDCClaims, providerName string, tenantID uuid.UUID) (*store.UserData, error) {
	// 1. Check if identity exists
	identity, err := h.users.GetIdentityByProviderSubject(ctx, providerName, claims.Sub)
	if err == nil && identity != nil {
		// Existing identity — load user, update last used
		user, err := h.users.GetByID(ctx, identity.UserID)
		if err != nil {
			return nil, err
		}
		go func() {
			tctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = h.users.UpdateIdentityLastUsed(tctx, identity.ID)
			_ = h.users.UpdateLastLogin(tctx, user.ID)
		}()
		return user, nil
	}

	// 2. Check if user with same email exists globally
	user, err := h.users.GetByEmail(ctx, claims.Email)
	if err == nil && user != nil {
		// Link new identity to existing user
		_, err := h.createIdentity(ctx, user.ID, providerName, claims)
		if err != nil {
			slog.Warn("auth.identity_link_failed", "user_id", user.ID, "provider", providerName, "error", err)
		}
		// Ensure tenant membership exists
		if h.tenants != nil {
			_ = h.tenants.AddUser(ctx, tenantID, user.ID.String(), false)
		}
		go func() {
			tctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = h.users.UpdateLastLogin(tctx, user.ID)
		}()
		return user, nil
	}

	// 3. Create new user + identity
	displayName := claims.Name
	if displayName == "" {
		displayName = claims.Email
	}
	newUser := &store.UserData{
		ID:           uuid.Must(uuid.NewV7()),
		Email:        claims.Email,
		DisplayName:  displayName,
		AuthProvider: providerName,
		Status:       store.UserStatusActive,
	}
	if err := h.users.Create(ctx, newUser); err != nil {
		return nil, err
	}

	if h.tenants != nil {
		if err := h.tenants.AddUser(ctx, tenantID, newUser.ID.String(), false); err != nil {
			slog.Warn("auth.oidc: failed to add tenant membership", "error", err, "user_id", newUser.ID)
		}
	}

	if _, err := h.createIdentity(ctx, newUser.ID, providerName, claims); err != nil {
		slog.Warn("auth.identity_create_failed", "user_id", newUser.ID, "provider", providerName, "error", err)
	}

	go func() {
		tctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = h.users.UpdateLastLogin(tctx, newUser.ID)
	}()

	return newUser, nil
}

func (h *OIDCHandler) createIdentity(ctx context.Context, userID uuid.UUID, providerName string, claims *auth.OIDCClaims) (*store.UserIdentity, error) {
	identity := &store.UserIdentity{
		ID:              uuid.Must(uuid.NewV7()),
		UserID:          userID,
		Provider:        providerName,
		ProviderSubject: claims.Sub,
		Email:           claims.Email,
	}
	if claims.TID != "" {
		identity.ProviderTenant = &claims.TID
	}
	if err := h.users.CreateIdentity(ctx, identity); err != nil {
		return nil, err
	}
	return identity, nil
}
