package http

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/auth"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// AuthHandler handles authentication session endpoints.
type AuthHandler struct {
	users store.UserStore
	jwt   *auth.JWTManager
	cfg   *config.AuthConfig
}

// NewAuthHandler creates a handler for auth session endpoints.
func NewAuthHandler(users store.UserStore, jwt *auth.JWTManager, cfg *config.AuthConfig) *AuthHandler {
	return &AuthHandler{users: users, jwt: jwt, cfg: cfg}
}

// RegisterRoutes registers all auth session routes on the given mux.
func (h *AuthHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/auth/login", h.handleLogin)
	mux.HandleFunc("POST /v1/auth/refresh", h.handleRefresh)
	mux.HandleFunc("POST /v1/auth/logout", h.handleLogout)
	mux.HandleFunc("GET /v1/auth/providers", h.handleProviders)
	mux.HandleFunc("POST /v1/auth/password/change", requireAuth("", h.handlePasswordChange))
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

	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		tenantID = store.MasterTenantID
	}

	user, err := h.users.GetByEmail(ctx, tenantID, req.Email)
	if err != nil || user == nil {
		slog.Warn("auth.login_failed", "email", req.Email, "error", err)
		writeError(w, http.StatusUnauthorized, "invalid_credentials", i18n.T(locale, i18n.MsgAuthInvalidCredentials))
		return
	}

	if user.PasswordHash == nil || !auth.CheckPassword(req.Password, *user.PasswordHash) {
		slog.Warn("auth.login_failed", "email", req.Email, "reason", "password_mismatch")
		writeError(w, http.StatusUnauthorized, "invalid_credentials", i18n.T(locale, i18n.MsgAuthInvalidCredentials))
		return
	}

	if user.Status != store.UserStatusActive {
		writeError(w, http.StatusForbidden, "account_suspended", i18n.T(locale, i18n.MsgAuthAccountSuspended))
		return
	}

	// Determine role for JWT claims
	role := "member"
	if user.IsTenantAdmin {
		role = "tenant_admin"
	}

	// Issue access token
	accessToken, err := h.jwt.IssueAccessToken(user.ID, user.Email, user.TenantID, role)
	if err != nil {
		slog.Error("auth.issue_token_failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", i18n.T(locale, i18n.MsgInternalError, "token issue"))
		return
	}

	// Issue refresh token
	var refreshToken string
	if h.cfg.Session.IsRefreshEnabled() {
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

	timeout := h.cfg.Session.SessionTimeout()
	writeJSON(w, http.StatusOK, loginResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    timeout * 60,
		User:         user,
	})
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type refreshResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
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

	role := "member"
	if user.IsTenantAdmin {
		role = "tenant_admin"
	}

	accessToken, err := h.jwt.IssueAccessToken(user.ID, user.Email, user.TenantID, role)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", i18n.T(locale, i18n.MsgInternalError, "token issue"))
		return
	}

	timeout := h.cfg.Session.SessionTimeout()
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
	providers := h.cfg.EnabledProviders()
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
	if len(req.NewPassword) < 8 {
		writeError(w, http.StatusBadRequest, "password_too_short", i18n.T(locale, i18n.MsgAuthPasswordTooShort, 8))
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
