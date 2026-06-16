package http

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// RolesHandler handles role management endpoints.
type RolesHandler struct {
	roles   store.RoleStore
	users   store.UserStore
	groups  store.GroupStore
	tenants store.TenantStore
}

// NewRolesHandler creates a handler for role management endpoints.
func NewRolesHandler(roles store.RoleStore, users store.UserStore, groups store.GroupStore, tenants store.TenantStore) *RolesHandler {
	return &RolesHandler{roles: roles, users: users, groups: groups, tenants: tenants}
}

// RegisterRoutes registers all role management routes on the given mux.
func (h *RolesHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/roles", requireAuthAction("role.list", h.handleList))
	// 005: tenant-explicit list. {id} = viewed tenant UUID or slug. Authoritative
	// path for a cross-tenant owner browsing a specific tenant's roles — it does
	// not depend on the caller's ambient active tenant (unlike GET /v1/roles).
	mux.HandleFunc("GET /v1/tenants/{id}/roles", requireAuthAction("role.list", h.handleListForTenant))
	mux.HandleFunc("POST /v1/roles", requireAuthAction("role.create", h.handleCreate))
	mux.HandleFunc("GET /v1/roles/{id}", requireAuthAction("role.get", h.handleGet))
	mux.HandleFunc("PATCH /v1/roles/{id}", requireAuthAction("role.update", h.handleUpdate))
	mux.HandleFunc("DELETE /v1/roles/{id}", requireAuthAction("role.delete", h.handleDelete))
	mux.HandleFunc("GET /v1/roles/{id}/permissions", requireAuthAction("role.get", h.handleGetPermissions))
	mux.HandleFunc("PUT /v1/roles/{id}/permissions", requireAuthAction("role.update", h.handleSetPermissions))
	mux.HandleFunc("GET /v1/users/{id}/roles", requireAuthAction("user.get", h.handleListUserRoles))
	mux.HandleFunc("POST /v1/users/{id}/roles", requireAuthAction("user.assign_role", h.handleAssignUserRole))
	mux.HandleFunc("DELETE /v1/users/{id}/roles/{roleId}", requireAuthAction("user.assign_role", h.handleUnassignUserRole))
	mux.HandleFunc("GET /v1/groups/{id}/roles", requireAuthAction("group.get", h.handleListGroupRoles))
	mux.HandleFunc("POST /v1/groups/{id}/roles", requireAuthAction("group.assign_role", h.handleAssignGroupRole))
	mux.HandleFunc("DELETE /v1/groups/{id}/roles/{roleId}", requireAuthAction("group.assign_role", h.handleUnassignGroupRole))
}

func (h *RolesHandler) handleList(w http.ResponseWriter, r *http.Request) {
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

	// Master scope can filter by tenant_id query param.
	if store.IsMasterScope(ctx) {
		if tidStr := q.Get("tenant_id"); tidStr != "" {
			if tid, err := uuid.Parse(tidStr); err == nil {
				tenantID = tid
			}
		}
	}

	if tenantID == uuid.Nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "tenant required"))
		return
	}

	result, err := h.roles.ListRoles(ctx, tenantID, store.RoleListParams{
		Offset: offset,
		Limit:  limit,
		Search: q.Get("search"),
	})
	if err != nil {
		slog.Error("roles.list failed", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToList, "roles"))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"roles":  result.Roles,
		"total":  result.Total,
		"offset": result.Offset,
		"limit":  result.Limit,
	})
}

// handleListForTenant lists roles for an explicitly-specified tenant (005).
// The {id} path value is the *viewed* tenant (UUID or slug), independent of the
// caller's ambient active tenant — this is what fixes the cross-tenant owner bug
// where GET /v1/roles resolved to the owner's home (master) tenant. Non-master
// callers may only list their own tenant (no cross-tenant leak).
func (h *RolesHandler) handleListForTenant(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	ctx := r.Context()

	tenantID, ok := h.resolveTenantPathID(w, r, locale)
	if !ok {
		return
	}

	// Enforce tenant scope for non-master callers (mirror handleGet).
	if !store.IsMasterScope(ctx) {
		if tid := store.TenantIDFromContext(ctx); tid != uuid.Nil && tid != tenantID {
			writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "roles"))
			return
		}
	}

	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 {
		limit = 50
	}
	offset, _ := strconv.Atoi(q.Get("offset"))
	if offset < 0 {
		offset = 0
	}

	result, err := h.roles.ListRoles(ctx, tenantID, store.RoleListParams{
		Offset: offset,
		Limit:  limit,
		Search: q.Get("search"),
	})
	if err != nil {
		slog.Error("roles.list_for_tenant failed", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToList, "roles"))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"roles":  result.Roles,
		"total":  result.Total,
		"offset": result.Offset,
		"limit":  result.Limit,
	})
}

// resolveTenantPathID resolves the {id} path value to a tenant UUID. Accepts a
// UUID or a tenant slug (mirrors how X-GoClaw-Tenant-Id resolves via
// resolveScopedTenant). Returns false (response written) on failure.
func (h *RolesHandler) resolveTenantPathID(w http.ResponseWriter, r *http.Request, locale string) (uuid.UUID, bool) {
	idVal := r.PathValue("id")
	if idVal == "" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "tenant"))
		return uuid.Nil, false
	}
	if tid, err := uuid.Parse(idVal); err == nil {
		return tid, true
	}
	if h.tenants != nil {
		if t, err := h.tenants.GetTenantBySlug(r.Context(), idVal); err == nil && t != nil {
			return t.ID, true
		}
	}
	writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "tenant"))
	return uuid.Nil, false
}

func (h *RolesHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	ctx := r.Context()
	tenantID := store.TenantIDFromContext(ctx)

	var input struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if !bindJSON(w, r, locale, &input) {
		return
	}
	if input.Name == "" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "name"))
		return
	}

	role := &store.RoleData{
		ID:          uuid.New(),
		TenantID:    tenantID,
		Name:        input.Name,
		Description: roleStrPtr(input.Description),
		Permissions: []string{},
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := h.roles.CreateRole(ctx, role); err != nil {
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "UNIQUE") {
			writeError(w, http.StatusConflict, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "role name already exists"))
			return
		}
		slog.Error("roles.create failed", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToCreate, "role"))
		return
	}

	writeJSON(w, http.StatusCreated, role)
}

func (h *RolesHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	ctx := r.Context()

	id, ok := parsePathUUID(w, r, "id", locale, "role")
	if !ok {
		return
	}

	role, err := h.roles.GetRole(ctx, id)
	if err != nil {
		slog.Error("roles.get failed", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError))
		return
	}
	if role == nil {
		writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "role"))
		return
	}

	// Enforce tenant scope for non-master callers.
	if !store.IsMasterScope(ctx) {
		if tid := store.TenantIDFromContext(ctx); tid != uuid.Nil && role.TenantID != tid {
			writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "role"))
			return
		}
	}

	perms, err := h.roles.GetRolePermissions(ctx, id)
	if err != nil {
		slog.Error("roles.get permissions failed", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError))
		return
	}
	role.Permissions = perms

	writeJSON(w, http.StatusOK, role)
}

func (h *RolesHandler) handleUpdate(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	ctx := r.Context()

	id, ok := parsePathUUID(w, r, "id", locale, "role")
	if !ok {
		return
	}

	role, err := h.roles.GetRole(ctx, id)
	if err != nil {
		slog.Error("roles.update get failed", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError))
		return
	}
	if role == nil {
		writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "role"))
		return
	}

	if role.IsSystem {
		writeError(w, http.StatusForbidden, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "cannot modify system role"))
		return
	}

	if !store.IsMasterScope(ctx) {
		if tid := store.TenantIDFromContext(ctx); tid != uuid.Nil && role.TenantID != tid {
			writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "role"))
			return
		}
	}

	var input struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if !bindJSON(w, r, locale, &input) {
		return
	}
	if input.Name != "" {
		role.Name = input.Name
	}
	if input.Description != "" {
		role.Description = &input.Description
	}

	if err := h.roles.UpdateRole(ctx, role); err != nil {
		slog.Error("roles.update failed", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToUpdate, "role"))
		return
	}

	writeJSON(w, http.StatusOK, role)
}

func (h *RolesHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	ctx := r.Context()

	id, ok := parsePathUUID(w, r, "id", locale, "role")
	if !ok {
		return
	}

	role, err := h.roles.GetRole(ctx, id)
	if err != nil {
		slog.Error("roles.delete get failed", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError))
		return
	}
	if role == nil {
		writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "role"))
		return
	}

	if role.IsSystem {
		writeError(w, http.StatusForbidden, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "cannot delete system role"))
		return
	}

	if !store.IsMasterScope(ctx) {
		if tid := store.TenantIDFromContext(ctx); tid != uuid.Nil && role.TenantID != tid {
			writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "role"))
			return
		}
	}

	// Check assignments.
	userCount, err := h.roles.CountUserRoleAssignments(ctx, id)
	if err != nil {
		slog.Error("roles.delete count users failed", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError))
		return
	}
	groupCount, err := h.roles.CountGroupRoleAssignments(ctx, id)
	if err != nil {
		slog.Error("roles.delete count groups failed", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError))
		return
	}
	if userCount > 0 || groupCount > 0 {
		writeError(w, http.StatusConflict, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "role is still assigned to users or groups"))
		return
	}

	if err := h.roles.DeleteRole(ctx, id); err != nil {
		slog.Error("roles.delete failed", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToDelete, "role"))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *RolesHandler) handleGetPermissions(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	ctx := r.Context()

	id, ok := parsePathUUID(w, r, "id", locale, "role")
	if !ok {
		return
	}

	perms, err := h.roles.GetRolePermissions(ctx, id)
	if err != nil {
		slog.Error("roles.get_permissions failed", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"permissions": perms})
}

func (h *RolesHandler) handleSetPermissions(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	ctx := r.Context()

	id, ok := parsePathUUID(w, r, "id", locale, "role")
	if !ok {
		return
	}

	var input struct {
		Permissions []string `json:"permissions"`
	}
	if !bindJSON(w, r, locale, &input) {
		return
	}

	// System roles are fully read-only — their permission set cannot be changed.
	role, err := h.roles.GetRole(ctx, id)
	if err != nil {
		slog.Error("roles.set_permissions get failed", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError))
		return
	}
	if role == nil {
		writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "role"))
		return
	}
	if role.IsSystem {
		writeError(w, http.StatusForbidden, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "cannot modify system role"))
		return
	}

	// Validate all permissions are known.
	known := make(map[string]bool)
	for _, p := range permissions.AllPermissions() {
		known[string(p)] = true
	}
	for _, p := range input.Permissions {
		if !known[p] {
			writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "unknown permission: "+p))
			return
		}
	}

	if err := h.roles.SetRolePermissions(ctx, id, input.Permissions); err != nil {
		slog.Error("roles.set_permissions failed", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError))
		return
	}

	// Invalidate permission cache for affected users.
	if pkgPermCache != nil {
		pkgPermCache.InvalidateAll()
	}

	writeJSON(w, http.StatusOK, map[string]any{"permissions": input.Permissions})
}

func (h *RolesHandler) handleListUserRoles(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	ctx := r.Context()
	tenantID := store.TenantIDFromContext(ctx)

	idStr := r.PathValue("id")
	userID, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "user"))
		return
	}

	roles, err := h.roles.ListUserRoles(ctx, tenantID, userID.String())
	if err != nil {
		slog.Error("roles.list_user_roles failed", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"roles": roles})
}

func (h *RolesHandler) handleAssignUserRole(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	ctx := r.Context()
	tenantID := store.TenantIDFromContext(ctx)

	userIDStr := r.PathValue("id")
	_, err := uuid.Parse(userIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "user"))
		return
	}

	var input struct {
		RoleID uuid.UUID `json:"role_id"`
	}
	if !bindJSON(w, r, locale, &input) {
		return
	}
	if input.RoleID == uuid.Nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "role_id"))
		return
	}

	// Verify role belongs to the tenant.
	role, err := h.roles.GetRole(ctx, input.RoleID)
	if err != nil {
		slog.Error("roles.assign_user_role get role failed", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError))
		return
	}
	if role == nil || role.TenantID != tenantID {
		writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "role"))
		return
	}

	if err := h.roles.AssignUserRole(ctx, tenantID, userIDStr, input.RoleID); err != nil {
		slog.Error("roles.assign_user_role failed", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError))
		return
	}

	if pkgPermCache != nil {
		pkgPermCache.Invalidate(userIDStr, tenantID)
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "assigned"})
}

func (h *RolesHandler) handleUnassignUserRole(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	ctx := r.Context()
	tenantID := store.TenantIDFromContext(ctx)

	userIDStr := r.PathValue("id")
	_, err := uuid.Parse(userIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "user"))
		return
	}

	roleIDStr := r.PathValue("roleId")
	roleID, err := uuid.Parse(roleIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "role"))
		return
	}

	if err := h.roles.UnassignUserRole(ctx, tenantID, userIDStr, roleID); err != nil {
		slog.Error("roles.unassign_user_role failed", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError))
		return
	}

	if pkgPermCache != nil {
		pkgPermCache.Invalidate(userIDStr, tenantID)
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "unassigned"})
}

func (h *RolesHandler) handleListGroupRoles(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	ctx := r.Context()

	id, ok := parsePathUUID(w, r, "id", locale, "group")
	if !ok {
		return
	}

	// Verify group belongs to tenant.
	group, err := h.groups.GetGroup(ctx, id)
	if err != nil {
		slog.Error("roles.list_group_roles get group failed", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError))
		return
	}
	if group == nil {
		writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "group"))
		return
	}
	if !store.IsMasterScope(ctx) {
		if tid := store.TenantIDFromContext(ctx); tid != uuid.Nil && group.TenantID != tid {
			writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "group"))
			return
		}
	}

	roles, err := h.roles.ListGroupRoles(ctx, id)
	if err != nil {
		slog.Error("roles.list_group_roles failed", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"roles": roles})
}

func (h *RolesHandler) handleAssignGroupRole(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	ctx := r.Context()
	tenantID := store.TenantIDFromContext(ctx)

	groupID, ok := parsePathUUID(w, r, "id", locale, "group")
	if !ok {
		return
	}

	// Verify group belongs to tenant.
	group, err := h.groups.GetGroup(ctx, groupID)
	if err != nil {
		slog.Error("roles.assign_group_role get group failed", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError))
		return
	}
	if group == nil {
		writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "group"))
		return
	}
	if group.TenantID != tenantID {
		writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "group"))
		return
	}

	var input struct {
		RoleID uuid.UUID `json:"role_id"`
	}
	if !bindJSON(w, r, locale, &input) {
		return
	}
	if input.RoleID == uuid.Nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "role_id"))
		return
	}

	// Verify role belongs to the tenant.
	role, err := h.roles.GetRole(ctx, input.RoleID)
	if err != nil {
		slog.Error("roles.assign_group_role get role failed", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError))
		return
	}
	if role == nil || role.TenantID != tenantID {
		writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "role"))
		return
	}

	if err := h.roles.AssignGroupRole(ctx, groupID, input.RoleID); err != nil {
		slog.Error("roles.assign_group_role failed", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "assigned"})
}

func (h *RolesHandler) handleUnassignGroupRole(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	ctx := r.Context()

	groupID, ok := parsePathUUID(w, r, "id", locale, "group")
	if !ok {
		return
	}

	roleIDStr := r.PathValue("roleId")
	roleID, err := uuid.Parse(roleIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "role"))
		return
	}

	if err := h.roles.UnassignGroupRole(ctx, groupID, roleID); err != nil {
		slog.Error("roles.unassign_group_role failed", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "unassigned"})
}

func roleStrPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
