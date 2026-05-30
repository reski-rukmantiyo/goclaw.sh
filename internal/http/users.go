package http

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/auth"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// UsersHandler handles user management endpoints.
type UsersHandler struct {
	users store.UserStore
}

// NewUsersHandler creates a handler for user management endpoints.
func NewUsersHandler(users store.UserStore) *UsersHandler {
	return &UsersHandler{users: users}
}

// RegisterRoutes registers all user management routes on the given mux.
func (h *UsersHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/users/me", requireAuth("", h.handleGetMe))
	mux.HandleFunc("PATCH /v1/users/me", requireAuth("", h.handleUpdateMe))
	mux.HandleFunc("GET /v1/users", requireAuth(permissions.RoleAdmin, h.handleList))
	mux.HandleFunc("POST /v1/users", requireAuth(permissions.RoleAdmin, h.handleCreate))
	mux.HandleFunc("GET /v1/users/{id}", requireAuth(permissions.RoleAdmin, h.handleGet))
	mux.HandleFunc("PATCH /v1/users/{id}/status", requireAuth(permissions.RoleAdmin, h.handleStatusChange))
	mux.HandleFunc("DELETE /v1/users/{id}", requireAuth(permissions.RoleAdmin, h.handleDelete))
}

// handleGetMe returns the current authenticated user's profile.
func (h *UsersHandler) handleGetMe(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	ctx := r.Context()

	userID := store.UserIDFromContext(ctx)
	if userID == "" {
		writeError(w, http.StatusUnauthorized, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgUserIDRequired))
		return
	}

	// UserID from context is a string (external ID). Try parsing as UUID.
	id, err := uuid.Parse(userID)
	if err != nil {
		writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "user", userID))
		return
	}

	user, err := h.users.GetByID(ctx, id)
	if err != nil {
		slog.Error("users.get_me failed", "error", err, "user_id", userID)
		writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "user", userID))
		return
	}

	writeJSON(w, http.StatusOK, user)
}

// handleUpdateMe updates the current authenticated user's profile.
func (h *UsersHandler) handleUpdateMe(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	ctx := r.Context()

	userID := store.UserIDFromContext(ctx)
	if userID == "" {
		writeError(w, http.StatusUnauthorized, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgUserIDRequired))
		return
	}

	id, err := uuid.Parse(userID)
	if err != nil {
		writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "user", userID))
		return
	}

	user, err := h.users.GetByID(ctx, id)
	if err != nil {
		slog.Error("users.update_me get failed", "error", err, "user_id", userID)
		writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "user", userID))
		return
	}

	var input struct {
		AvatarURL *string `json:"avatar_url"`
	}
	if !bindJSON(w, r, locale, &input) {
		return
	}

	if input.AvatarURL != nil {
		user.AvatarURL = input.AvatarURL
	}

	if err := h.users.Update(ctx, user); err != nil {
		slog.Error("users.update_me failed", "error", err, "user_id", userID)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToUpdate, "user", "internal error"))
		return
	}

	writeJSON(w, http.StatusOK, user)
}

// handleList returns a paginated list of users in the caller's tenant.
func (h *UsersHandler) handleList(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	ctx := r.Context()
	tenantID := store.TenantIDFromContext(ctx)

	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 {
		limit = 50
	}
	offset, _ := strconv.Atoi(q.Get("offset"))
	if offset < 0 {
		offset = 0
	}

	params := store.UserListParams{
		Search: q.Get("search"),
		Status: q.Get("status"),
		Limit:  limit,
		Offset: offset,
	}
	if gid := q.Get("group_id"); gid != "" {
		if id, err := uuid.Parse(gid); err == nil {
			params.GroupID = &id
		}
	}

	result, err := h.users.List(ctx, tenantID, params)
	if err != nil {
		slog.Error("users.list failed", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToList, "users"))
		return
	}

	if result.Users == nil {
		result.Users = []store.UserData{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items":  result.Users,
		"total":  result.Total,
		"offset": result.Offset,
		"limit":  result.Limit,
	})
}

// handleCreate creates a new local user in the caller's tenant.
func (h *UsersHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	ctx := r.Context()
	tenantID := store.TenantIDFromContext(ctx)

	var input struct {
		Email       string `json:"email"`
		DisplayName string `json:"display_name"`
		Password    string `json:"password"`
	}
	if !bindJSON(w, r, locale, &input) {
		return
	}

	if input.Email == "" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "email is required"))
		return
	}
	if input.Password == "" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "password is required"))
		return
	}

	// Check for duplicate email within tenant.
	existing, err := h.users.GetByEmail(ctx, tenantID, input.Email)
	if err != nil && err.Error() != "not found" {
		slog.Error("users.create check duplicate failed", "error", err)
	}
	if existing != nil {
		writeError(w, http.StatusConflict, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "email already exists"))
		return
	}

	hash, err := auth.HashPassword(input.Password)
	if err != nil {
		slog.Error("users.create hash password failed", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToCreate, "user", "internal error"))
		return
	}

	now := time.Now().UTC()
	user := &store.UserData{
		ID:           uuid.New(),
		Email:        input.Email,
		DisplayName:  input.DisplayName,
		TenantID:     tenantID,
		AuthProvider: store.AuthProviderLocal,
		PasswordHash: &hash,
		Status:       store.UserStatusActive,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := h.users.Create(ctx, user); err != nil {
		slog.Error("users.create failed", "error", err, "email", input.Email)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToCreate, "user", "internal error"))
		return
	}

	writeJSON(w, http.StatusCreated, user)
}

// handleGet returns a single user by ID.
func (h *UsersHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	ctx := r.Context()

	idStr := r.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "user"))
		return
	}

	user, err := h.users.GetByID(ctx, id)
	if err != nil {
		slog.Error("users.get failed", "error", err, "id", idStr)
		writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "user", idStr))
		return
	}

	writeJSON(w, http.StatusOK, user)
}

// handleStatusChange updates a user's status (active/suspended/deactivated).
func (h *UsersHandler) handleStatusChange(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	ctx := r.Context()

	idStr := r.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "user"))
		return
	}

	var input struct {
		Status string `json:"status"`
	}
	if !bindJSON(w, r, locale, &input) {
		return
	}

	switch input.Status {
	case store.UserStatusActive, store.UserStatusSuspended, store.UserStatusDeactivated:
		// valid
	default:
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidRequest, "status must be one of: active, suspended, deactivated"))
		return
	}

	if err := h.users.UpdateStatus(ctx, id, input.Status); err != nil {
		slog.Error("users.status_change failed", "error", err, "id", idStr, "status", input.Status)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToUpdate, "user status", "internal error"))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": input.Status})
}

// handleDelete permanently removes a user.
func (h *UsersHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	ctx := r.Context()

	idStr := r.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "user"))
		return
	}

	if err := h.users.Delete(ctx, id); err != nil {
		slog.Error("users.delete failed", "error", err, "id", idStr)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToDelete, "user"))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
