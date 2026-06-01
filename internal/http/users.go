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
	users   store.UserStore
	groups  store.GroupStore
	tenants store.TenantStore
	roles   store.RoleStore
}

// NewUsersHandler creates a handler for user management endpoints.
func NewUsersHandler(users store.UserStore, groups store.GroupStore, tenants store.TenantStore, roles store.RoleStore) *UsersHandler {
	return &UsersHandler{users: users, groups: groups, tenants: tenants, roles: roles}
}

// RegisterRoutes registers all user management routes on the given mux.
func (h *UsersHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/users/me", requireAuth("", h.handleGetMe))
	mux.HandleFunc("PATCH /v1/users/me", requireAuth("", h.handleUpdateMe))
	mux.HandleFunc("GET /v1/users/me/permissions", requireAuth("", h.handleGetMyPermissions))
	mux.HandleFunc("GET /v1/users", requireAuthAction("user.list", h.handleList))
	mux.HandleFunc("POST /v1/users", requireAuthAction("user.pre_provision", h.handleCreate))
	mux.HandleFunc("GET /v1/users/{id}", requireAuthAction("user.get", h.handleGet))
	mux.HandleFunc("PATCH /v1/users/{id}/status", requireAuthAction("user.suspend", h.handleStatusChange))
	mux.HandleFunc("GET /v1/users/{id}/tenants", requireAuthAction("user.get", h.handleListUserTenants))
	mux.HandleFunc("POST /v1/users/{id}/tenants", requireAuthAction("user.enroll", h.handleEnrollUser))
	mux.HandleFunc("DELETE /v1/users/{id}/tenants/{tenantId}", requireAuthAction("user.unenroll", h.handleUnenrollUser))
	mux.HandleFunc("DELETE /v1/users/{id}", requireAuthAction("user.deactivate", h.handleDelete))
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

// handleGetMyPermissions returns the current user's effective permissions in the current tenant.
func (h *UsersHandler) handleGetMyPermissions(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	ctx := r.Context()

	userID := store.UserIDFromContext(ctx)
	if userID == "" {
		writeError(w, http.StatusUnauthorized, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgUserIDRequired))
		return
	}

	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "tenant required"))
		return
	}

	// Check owner via context role first (covers gateway token / system users),
	// then fall back to DB is_owner flag.
	isOwner := store.IsOwnerRole(ctx)
	if !isOwner && h.tenants != nil {
		isOwner, _ = h.tenants.IsOwner(ctx, tenantID, userID)
	}

	var perms []string
	if isOwner {
		// Owner gets all permissions.
		for _, p := range permissions.AllPermissions() {
			perms = append(perms, string(p))
		}
	} else if h.roles != nil {
		var err error
		perms, err = h.roles.GetUserEffectivePermissions(ctx, userID, tenantID)
		if err != nil {
			slog.Error("users.me.permissions failed", "error", err, "user_id", userID)
			writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError))
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"is_owner":    isOwner,
		"permissions": perms,
	})
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
		Role        string `json:"role"` // tenant_users role: owner/admin/operator/member/viewer
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
	lengthOK, hasUpper, hasSymbol := auth.ValidatePasswordComplexity(input.Password)
	if !lengthOK {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgAuthPasswordTooShort, auth.MinPasswordLength))
		return
	}
	if !hasUpper || !hasSymbol {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgAuthPasswordComplexity))
		return
	}

	// Determine owner flag from legacy role input (kept for API compat).
	isOwner := input.Role == store.TenantRoleOwner

	// Check for duplicate email globally.
	existing, err := h.users.GetByEmail(ctx, input.Email)
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

	if h.tenants != nil && tenantID != uuid.Nil {
		if err := h.tenants.AddUser(ctx, tenantID, user.ID.String(), isOwner); err != nil {
			slog.Warn("users.create: failed to add tenant membership", "error", err, "user_id", user.ID)
		}
	}

	writeJSON(w, http.StatusCreated, user)
}

// checkUserTenantScope fetches a user by ID and verifies it belongs to the caller's tenant.
// Returns the user if accessible, nil + writes error response if not.
func (h *UsersHandler) checkUserTenantScope(w http.ResponseWriter, r *http.Request, id uuid.UUID) *store.UserData {
	locale := extractLocale(r)
	ctx := r.Context()

	user, err := h.users.GetByID(ctx, id)
	if err != nil {
		writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "user", id.String()))
		return nil
	}
	if user == nil {
		writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "user", id.String()))
		return nil
	}
	if tid := store.TenantIDFromContext(ctx); tid != uuid.Nil {
		memberships, _ := h.tenants.ListUserTenants(ctx, id.String())
		isMember := false
		for _, m := range memberships {
			if m.TenantID == tid {
				isMember = true
				break
			}
		}
		if !isMember {
			writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "user", id.String()))
			return nil
		}
	}
	return user
}

// handleGet returns a single user by ID.
func (h *UsersHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)

	idStr := r.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "user"))
		return
	}

	user := h.checkUserTenantScope(w, r, id)
	if user == nil {
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

	if h.checkUserTenantScope(w, r, id) == nil {
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

	if h.checkUserTenantScope(w, r, id) == nil {
		return
	}

	// Block delete if user is still enrolled in any tenant.
	if h.tenants != nil {
		memberships, err := h.tenants.ListUserTenants(ctx, id.String())
		if err != nil {
			slog.Error("users.delete check tenants failed", "error", err, "id", idStr)
			writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToDelete, "user"))
			return
		}
		if len(memberships) > 0 {
			writeError(w, http.StatusConflict, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgUserDeleteBlockedTenants, len(memberships)))
			return
		}
	}

	if err := h.users.Delete(ctx, id); err != nil {
		slog.Error("users.delete failed", "error", err, "id", idStr)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToDelete, "user"))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}


// handleListUserTenants returns the tenants a user is enrolled in.
func (h *UsersHandler) handleListUserTenants(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	ctx := r.Context()

	idStr := r.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "user"))
		return
	}

	if h.checkUserTenantScope(w, r, id) == nil {
		return
	}

	memberships, err := h.tenants.ListUserTenants(ctx, id.String())
	if err != nil {
		slog.Error("users.list_tenants failed", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"tenants": memberships})
}

// handleEnrollUser enrolls an existing user into a tenant.
func (h *UsersHandler) handleEnrollUser(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	ctx := r.Context()

	idStr := r.PathValue("id")
	_, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "user"))
		return
	}

	var input struct {
		TenantID string `json:"tenant_id"`
		IsOwner  bool   `json:"is_owner"`
	}
	if !bindJSON(w, r, locale, &input) {
		return
	}
	if input.TenantID == "" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "tenant_id"))
		return
	}

	tenantID, err := uuid.Parse(input.TenantID)
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "tenant"))
		return
	}

	if err := h.tenants.AddUser(ctx, tenantID, idStr, input.IsOwner); err != nil {
		slog.Error("users.enroll failed", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToCreate, "tenant membership"))
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{"status": "enrolled"})
}

// handleUnenrollUser removes a user from a tenant.
func (h *UsersHandler) handleUnenrollUser(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	ctx := r.Context()

	userIDStr := r.PathValue("id")
	_, err := uuid.Parse(userIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "user"))
		return
	}

	tenantIDStr := r.PathValue("tenantId")
	tenantID, err := uuid.Parse(tenantIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "tenant"))
		return
	}

	if err := h.tenants.RemoveUser(ctx, tenantID, userIDStr); err != nil {
		slog.Error("users.unenroll failed", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToDelete, "tenant membership"))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "unenrolled"})
}
