package http

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/auth"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tenantauth"
)

// AuthHandler handles authentication session endpoints.
type AuthHandler struct {
	users  store.UserStore
	tenants store.TenantStore
	jwt     *auth.JWTManager
	loader  tenantauth.Loader
}

// NewAuthHandler creates a handler for auth session endpoints.
func NewAuthHandler(users store.UserStore, tenants store.TenantStore, jwt *auth.JWTManager, loader tenantauth.Loader) *AuthHandler {
	return &AuthHandler{users: users, tenants: tenants, jwt: jwt, loader: loader}
}

// resolveTenantForAuth extracts tenant ID from request context, headers, or query params.
// Falls back to master tenant if no tenant is specified.
func resolveTenantForAuth(r *http.Request) uuid.UUID {
	ctx := r.Context()
	if tid := store.TenantIDFromContext(ctx); tid != uuid.Nil {
		return tid
	}
	if h := r.Header.Get("X-GoClaw-Tenant-Id"); h != "" {
		if tid, err := uuid.Parse(h); err == nil {
			return tid
		}
	}
	if s := r.URL.Query().Get("tenant"); s != "" {
		if tid, err := uuid.Parse(s); err == nil {
			return tid
		}
	}
	return store.MasterTenantID
}

// RegisterRoutes registers all auth session routes on the given mux.
func (h *AuthHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /auth/login", h.handleLogin)
	mux.HandleFunc("POST /auth/register", h.handleRegister)
	mux.HandleFunc("POST /auth/refresh", h.handleRefresh)
	mux.HandleFunc("POST /auth/logout", h.handleLogout)
	mux.HandleFunc("GET /auth/providers", h.handleProviders)
	mux.HandleFunc("POST /auth/password/change", requireAuth("", h.handlePasswordChange))
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginResponse struct {
	AccessToken  string        `json:"access_token"`
	RefreshToken string        `json:"refresh_token"`
	ExpiresIn    int           `json:"expires_in"`
	User         *store.UserData `json:"user"`
	TenantSlug   string        `json:"tenant_slug"`
}

func (h *AuthHandler) handleLogin(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	ctx := r.Context()

	var req loginRequest
	if !parseJSON(w, r, &req) {
		return
	}
	if req.Email == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", i18n.T(locale, i18n.MsgRequired, "email and password"))
		return
	}

	tenantID := resolveTenantForAuth(r)

	cfg, err := h.loader.LoadAuthConfig(ctx, tenantID)
	if err != nil {
		slog.Error("auth.load_config_failed", "tenant_id", tenantID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", i18n.T(locale, i18n.MsgInternalError, "auth config"))
		return
	}

	if !cfg.LocalEnabled() {
		writeError(w, http.StatusForbidden, "auth_disabled", i18n.T(locale, i18n.MsgInvalidRequest, "local auth is disabled for this tenant"))
		return
	}

	user, err := h.users.GetByEmail(ctx, req.Email)
	if err != nil || user == nil {
		slog.Warn("auth.login_failed", "email", req.Email, "tenant_id", tenantID, "error", err)
		writeError(w, http.StatusUnauthorized, "invalid_credentials", i18n.T(locale, i18n.MsgAuthInvalidCredentials))
		return
	}

	// Resolve tenant from tenant_users membership; fall back to request-resolved tenant
	if h.tenants != nil {
		resolvedTenantID, rErr := h.tenants.ResolveUserTenant(ctx, user.ID.String())
		if rErr == nil && resolvedTenantID != uuid.Nil && resolvedTenantID != store.MasterTenantID {
			tenantID = resolvedTenantID
		}
	}

	// Re-load per-tenant auth config for the resolved tenant
	if tenantID != resolveTenantForAuth(r) {
		cfg, err = h.loader.LoadAuthConfig(ctx, tenantID)
		if err != nil {
			slog.Error("auth.load_config_failed", "tenant_id", tenantID, "error", err)
			writeError(w, http.StatusInternalServerError, "internal_error", i18n.T(locale, i18n.MsgInternalError, "auth config"))
			return
		}
		if !cfg.LocalEnabled() {
			writeError(w, http.StatusForbidden, "auth_disabled", i18n.T(locale, i18n.MsgInvalidRequest, "local auth is disabled for this tenant"))
			return
		}
	}

	if user.PasswordHash == nil || !auth.CheckPassword(req.Password, *user.PasswordHash) {
		slog.Warn("auth.login_failed", "email", req.Email, "tenant_id", tenantID, "reason", "password_mismatch")
		writeError(w, http.StatusUnauthorized, "invalid_credentials", i18n.T(locale, i18n.MsgAuthInvalidCredentials))
		return
	}

	if user.Status != store.UserStatusActive {
		writeError(w, http.StatusForbidden, "account_suspended", i18n.T(locale, i18n.MsgAuthAccountSuspended))
		return
	}

	// Determine role for JWT claims from tenant_users membership
	role := resolveUserRoleForJWT(ctx, h.tenants, tenantID, user.ID.String())

	// Issue access token scoped to resolved tenant
	accessToken, err := h.jwt.IssueAccessToken(user.ID, user.Email, tenantID, role)
	if err != nil {
		slog.Error("auth.issue_token_failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", i18n.T(locale, i18n.MsgInternalError, "token issue"))
		return
	}

	// Issue refresh token
	var refreshToken string
	if cfg.Session.IsRefreshEnabled() {
		raw, hash, err := auth.GenerateRefreshToken()
		if err != nil {
			slog.Error("auth.refresh_token_failed", "error", err)
			writeError(w, http.StatusInternalServerError, "internal_error", i18n.T(locale, i18n.MsgInternalError, "refresh token"))
			return
		}
		refreshToken = raw
		expiresAt := time.Now().Add(7 * 24 * time.Hour) // 7 days
		if err := h.users.StoreRefreshToken(ctx, user.ID, hash, "", expiresAt); err != nil {
			slog.Error("auth.store_refresh_token_failed", "error", err)
		}
	}

	// Update last login
	go func() {
		tctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = h.users.UpdateLastLogin(tctx, user.ID)
	}()

	var tenantSlug string
	if h.tenants != nil {
		if t, err := h.tenants.GetTenant(ctx, tenantID); err == nil && t != nil {
			tenantSlug = t.Slug
		}
	}

	timeout := cfg.Session.SessionTimeout()
	writeJSON(w, http.StatusOK, loginResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    timeout * 60,
		User:         user,
		TenantSlug:   tenantSlug,
	})
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type registerRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}

type registerResponse struct {
	AccessToken  string          `json:"access_token"`
	RefreshToken string          `json:"refresh_token"`
	ExpiresIn    int             `json:"expires_in"`
	User         *store.UserData `json:"user"`
	TenantSlug   string          `json:"tenant_slug"`
}

type refreshResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

func (h *AuthHandler) handleRegister(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	ctx := r.Context()
	tenantID := resolveTenantForAuth(r)

	cfg, err := h.loader.LoadAuthConfig(ctx, tenantID)
	if err != nil {
		slog.Error("auth.load_config_failed", "tenant_id", tenantID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", i18n.T(locale, i18n.MsgInternalError, "auth config"))
		return
	}

	if !cfg.LocalEnabled() {
		writeError(w, http.StatusForbidden, "auth_disabled", i18n.T(locale, i18n.MsgInvalidRequest, "local auth is disabled for this tenant"))
		return
	}

	var req registerRequest
	if !parseJSON(w, r, &req) {
		return
	}
	if req.Email == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", i18n.T(locale, i18n.MsgRequired, "email and password"))
		return
	}

	// Validate password complexity
	lengthOK, hasUpper, hasDigit, hasSymbol := auth.ValidatePasswordComplexity(req.Password)
	if !lengthOK {
		writeError(w, http.StatusBadRequest, "invalid_request", i18n.T(locale, i18n.MsgAuthPasswordTooShort, auth.MinPasswordLength))
		return
	}
	if !hasUpper || !hasDigit || !hasSymbol {
		writeError(w, http.StatusBadRequest, "invalid_request", i18n.T(locale, i18n.MsgAuthPasswordComplexity))
		return
	}

	// Check for duplicate email (global uniqueness).
	existing, err := h.users.GetByEmail(ctx, req.Email)
	if err != nil && err.Error() != "not found" {
		slog.Error("auth.register duplicate check failed", "error", err)
	}
	if existing != nil {
		writeError(w, http.StatusConflict, "invalid_request", i18n.T(locale, i18n.MsgInvalidRequest, "email already exists"))
		return
	}

	// Check if any users exist in target tenant — first user becomes owner
	isFirstUser := false
	if existingUserList, err := h.users.List(ctx, tenantID, store.UserListParams{Limit: 1}); err == nil && existingUserList != nil {
		isFirstUser = existingUserList.Total == 0
	}

	// Hash password
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		slog.Error("auth.register hash failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", i18n.T(locale, i18n.MsgInternalError, "password hash"))
		return
	}

	now := time.Now().UTC()
	user := &store.UserData{
		ID:           uuid.New(),
		Email:        req.Email,
		DisplayName:  req.DisplayName,
		AuthProvider: store.AuthProviderLocal,
		PasswordHash: &hash,
		Status:       store.UserStatusActive,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := h.users.Create(ctx, user); err != nil {
		slog.Error("auth.register create user failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", i18n.T(locale, i18n.MsgFailedToCreate, "user", "internal error"))
		return
	}

	// Add tenant_users membership
	isOwner := false
	if isFirstUser {
		isOwner = true
	}
	if h.tenants != nil {
		if err := h.tenants.AddUser(ctx, tenantID, user.ID.String(), isOwner); err != nil {
			slog.Warn("auth.register: failed to add tenant membership", "error", err, "user_id", user.ID)
		}
	}

	// Issue JWT
	jwtRole := resolveUserRoleForJWT(ctx, h.tenants, tenantID, user.ID.String())
	accessToken, err := h.jwt.IssueAccessToken(user.ID, user.Email, tenantID, jwtRole)
	if err != nil {
		slog.Error("auth.register issue token failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", i18n.T(locale, i18n.MsgInternalError, "token issue"))
		return
	}

	var tenantSlug string
	if h.tenants != nil {
		if t, err := h.tenants.GetTenant(ctx, tenantID); err == nil && t != nil {
			tenantSlug = t.Slug
		}
	}

	timeout := cfg.Session.SessionTimeout()
	writeJSON(w, http.StatusCreated, registerResponse{
		AccessToken:  accessToken,
		RefreshToken: "",
		ExpiresIn:    timeout * 60,
		User:         user,
		TenantSlug:   tenantSlug,
	})
}

func (h *AuthHandler) handleRefresh(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	ctx := r.Context()

	var req refreshRequest
	if !parseJSON(w, r, &req) {
		return
	}
	if req.RefreshToken == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", i18n.T(locale, i18n.MsgRequired, "refresh_token"))
		return
	}

	tokenHash := auth.SHA256Hex(req.RefreshToken)
	userID, err := h.users.ValidateRefreshToken(ctx, tokenHash)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid_refresh_token", i18n.T(locale, i18n.MsgAuthRefreshTokenInvalid))
		return
	}

	user, err := h.users.GetByID(ctx, userID)
	if err != nil || user == nil {
		writeError(w, http.StatusUnauthorized, "invalid_refresh_token", i18n.T(locale, i18n.MsgAuthRefreshTokenInvalid))
		return
	}

	if user.Status != store.UserStatusActive {
		writeError(w, http.StatusForbidden, "account_suspended", i18n.T(locale, i18n.MsgAuthAccountSuspended))
		return
	}

	// Resolve tenant from tenant_users membership as default
	tenantID := store.MasterTenantID
	if h.tenants != nil {
		resolvedTenantID, rErr := h.tenants.ResolveUserTenant(ctx, user.ID.String())
		if rErr == nil && resolvedTenantID != uuid.Nil {
			tenantID = resolvedTenantID
		}
	}

	// Allow explicit tenant override via header so refresh stays scoped
	if scope := r.Header.Get("X-GoClaw-Tenant-Id"); scope != "" && h.tenants != nil {
		if t, tErr := h.tenants.GetTenantBySlug(ctx, scope); tErr == nil && t != nil {
			if isOwner, _ := h.tenants.IsOwner(ctx, t.ID, user.ID.String()); isOwner {
				tenantID = t.ID
			} else {
				memberships, _ := h.tenants.ListUserTenants(ctx, user.ID.String())
				for _, m := range memberships {
					if m.TenantID == t.ID {
						tenantID = t.ID
						break
					}
				}
			}
		}
	}

	role := resolveUserRoleForJWT(ctx, h.tenants, tenantID, user.ID.String())

	accessToken, err := h.jwt.IssueAccessToken(user.ID, user.Email, tenantID, role)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", i18n.T(locale, i18n.MsgInternalError, "token issue"))
		return
	}

	cfg, err := h.loader.LoadAuthConfig(ctx, tenantID)
	if err != nil {
		slog.Warn("auth.refresh_load_config_failed", "tenant_id", tenantID, "error", err)
	}

	timeout := 480
	if cfg != nil {
		timeout = cfg.Session.SessionTimeout()
	}
	writeJSON(w, http.StatusOK, refreshResponse{
		AccessToken: accessToken,
		ExpiresIn:   timeout * 60,
	})
}

type logoutRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func (h *AuthHandler) handleLogout(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req logoutRequest
	if !parseJSON(w, r, &req) {
		return
	}
	if req.RefreshToken != "" {
		tokenHash := auth.SHA256Hex(req.RefreshToken)
		_ = h.users.RevokeRefreshToken(ctx, tokenHash)
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *AuthHandler) handleProviders(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID := resolveTenantForAuth(r)

	cfg, err := h.loader.LoadAuthConfig(ctx, tenantID)
	if err != nil {
		slog.Warn("auth.providers_load_config_failed", "tenant_id", tenantID, "error", err)
		writeJSON(w, http.StatusOK, map[string]any{"providers": []map[string]string{}})
		return
	}

	providers := cfg.EnabledProviders()
	type providerInfo struct {
		Name string `json:"name"`
	}
	var result []providerInfo
	for _, p := range providers {
		result = append(result, providerInfo{Name: p})
	}
	if result == nil {
		result = []providerInfo{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"providers": result,
	})
}

type passwordChangeRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

func (h *AuthHandler) handlePasswordChange(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	ctx := r.Context()

	var req passwordChangeRequest
	if !parseJSON(w, r, &req) {
		return
	}
	if req.CurrentPassword == "" || req.NewPassword == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", i18n.T(locale, i18n.MsgRequired, "current_password and new_password"))
		return
	}
	if len(req.NewPassword) < auth.MinPasswordLength {
		writeError(w, http.StatusBadRequest, "password_too_short", i18n.T(locale, i18n.MsgAuthPasswordTooShort, auth.MinPasswordLength))
		return
	}
	_, hasUpper, hasDigit, hasSymbol := auth.ValidatePasswordComplexity(req.NewPassword)
	if !hasUpper || !hasDigit || !hasSymbol {
		writeError(w, http.StatusBadRequest, "password_complexity", i18n.T(locale, i18n.MsgAuthPasswordComplexity))
		return
	}

	userID := store.UserIDFromContext(ctx)
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized", i18n.T(locale, i18n.MsgUserIDRequired))
		return
	}

	id, err := uuid.Parse(userID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", i18n.T(locale, i18n.MsgInvalidID, "user"))
		return
	}

	user, err := h.users.GetByID(ctx, id)
	if err != nil || user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", i18n.T(locale, i18n.MsgNotFound, "user", userID))
		return
	}

	if user.PasswordHash == nil || !auth.CheckPassword(req.CurrentPassword, *user.PasswordHash) {
		writeError(w, http.StatusBadRequest, "password_mismatch", i18n.T(locale, i18n.MsgAuthPasswordMismatch))
		return
	}

	hash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", i18n.T(locale, i18n.MsgInternalError, "password hash"))
		return
	}

	user.PasswordHash = &hash
	if err := h.users.Update(ctx, user); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", i18n.T(locale, i18n.MsgFailedToUpdate, "user", err.Error()))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// parseJSON decodes a JSON request body into dst.
// Returns false if decoding failed (response already written).
func parseJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		locale := extractLocale(r)
		writeError(w, http.StatusBadRequest, "invalid_json", i18n.T(locale, i18n.MsgInvalidJSON))
		return false
	}
	return true
}

// resolveUserRoleForJWT derives the user's role for JWT claims from effective permissions.
// Returns one of: "owner", "admin", "member", "viewer".
func resolveUserRoleForJWT(ctx context.Context, tenants store.TenantStore, tenantID uuid.UUID, userID string) string {
	// 1. Owner check
	if tenants != nil && tenantID != uuid.Nil && userID != "" {
		isOwner, err := tenants.IsOwner(ctx, tenantID, userID)
		if err == nil && isOwner {
			return "owner"
		}
	}
	// 2. Permission-based derivation (same logic as resolveJWTRole in auth.go)
	if pkgPermCache != nil && tenantID != uuid.Nil && userID != "" {
		perms, err := pkgPermCache.EffectivePermissions(ctx, userID, tenantID)
		if err == nil && len(perms) > 0 {
			if perms[string(permissions.PermSystemManageSettings)] {
				return "admin"
			}
			hasWrite := false
			for p := range perms {
				if !permissions.IsReadOnlyPermission(p) {
					hasWrite = true
					break
				}
			}
			if hasWrite {
				return "member"
			}
			return "viewer"
		}
	}
	// 3. Fallback: member (tenant member exists)
	return "member"
}
