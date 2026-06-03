package http

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/base"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// TenantsHandler handles tenant CRUD and membership endpoints.
type TenantsHandler struct {
	tenantStore       store.TenantStore
	tenantDBConnStore store.TenantDBConnectionStore
	tenantDBManager   store.TenantDBManager
	masterDB          *sql.DB
	msgBus            *bus.MessageBus
	workspace         string // base workspace directory for tenant dirs
	defaultSSLMode    string // default sslmode for auto-generated tenant DBs
	masterDSN         string // master DSN for superuser schema operations on tenant DBs
	roleStore         store.RoleStore
}

// NewTenantsHandler creates a handler for tenant management endpoints.
func NewTenantsHandler(tenantStore store.TenantStore, tenantDBConnStore store.TenantDBConnectionStore, tenantDBManager store.TenantDBManager, masterDB *sql.DB, msgBus *bus.MessageBus, workspace string, defaultSSLMode string, masterDSN string, roleStore store.RoleStore) *TenantsHandler {
	return &TenantsHandler{tenantStore: tenantStore, tenantDBConnStore: tenantDBConnStore, tenantDBManager: tenantDBManager, masterDB: masterDB, msgBus: msgBus, workspace: workspace, defaultSSLMode: defaultSSLMode, masterDSN: masterDSN, roleStore: roleStore}
}

// RegisterRoutes registers all tenant management routes on the given mux.
func (h *TenantsHandler) RegisterRoutes(mux *http.ServeMux) {
	admin := func(next http.HandlerFunc) http.HandlerFunc {
		return requireAuth(permissions.RoleAdmin, next)
	}
	mux.HandleFunc("GET /v1/tenants", admin(h.handleList))
	mux.HandleFunc("POST /v1/tenants", admin(h.handleCreate))
	mux.HandleFunc("GET /v1/tenants/{id}", admin(h.handleGet))
	mux.HandleFunc("PATCH /v1/tenants/{id}", admin(h.handleUpdate))
	mux.HandleFunc("DELETE /v1/tenants/{id}", admin(h.handleDelete))
	mux.HandleFunc("GET /v1/tenants/{id}/users", admin(h.handleUsersList))
	mux.HandleFunc("POST /v1/tenants/{id}/users", admin(h.handleUsersAdd))
	mux.HandleFunc("DELETE /v1/tenants/{id}/users/{userId}", admin(h.handleUsersRemove))
}

func (h *TenantsHandler) handleList(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	if !store.IsMasterScope(r.Context()) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": i18n.T(locale, i18n.MsgPermissionDenied, "tenants.list")})
		return
	}

	tenants, err := h.tenantStore.ListTenants(r.Context())
	if err != nil {
		slog.Error("tenants.list failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToList, "tenants")})
		return
	}
	if tenants == nil {
		tenants = []store.TenantData{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tenants": tenants})
}

func (h *TenantsHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	if !store.IsOwnerRole(r.Context()) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": i18n.T(locale, i18n.MsgPermissionDenied, "tenants.create")})
		return
	}

	var input struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	}
	if !bindJSON(w, r, locale, &input) {
		return
	}

	if input.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "name")})
		return
	}
	if input.Slug == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "slug")})
		return
	}
	if !isValidSlug(input.Slug) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidSlug, "slug")})
		return
	}

	tenant := &store.TenantData{
		ID:     store.GenNewID(),
		Name:   input.Name,
		Slug:   input.Slug,
		Status: store.TenantStatusActive,
	}

	if err := h.tenantStore.CreateTenant(r.Context(), tenant); err != nil {
		slog.Error("tenants.create failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToCreate, "tenant", err.Error())})
		return
	}

	// Seed default system roles for the new tenant.
	h.seedSystemRoles(r.Context(), tenant.ID)

	// Provision tenant database if infrastructure available
	if h.tenantDBConnStore != nil && h.masterDB != nil {
		provisionedConn, err := pg.ProvisionTenantDB(r.Context(), h.masterDB, tenant.ID, tenant.Slug, nil, "", h.defaultSSLMode, h.masterDSN)
		if err != nil {
			slog.Error("tenants.create: db provision failed", "tenant_id", tenant.ID, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgTenantDBProvisionFailed, err.Error())})
			return
		}
		if err := h.tenantDBConnStore.Create(r.Context(), provisionedConn); err != nil {
			slog.Error("tenants.create: failed to save db connection", "tenant_id", tenant.ID, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToCreate, "tenant db connection", err.Error())})
			return
		}
		if h.tenantDBManager != nil {
			h.tenantDBManager.Invalidate(tenant.ID)
		}
	}

	// Create workspace directory for the tenant.
	if h.workspace != "" {
		tenantDir := filepath.Join(h.workspace, "tenants", tenant.Slug)
		if err := os.MkdirAll(tenantDir, 0755); err != nil {
			slog.Warn("tenants.create: failed to create workspace dir", "dir", tenantDir, "error", err)
		}
	}

	h.emitCacheInvalidate(bus.CacheKindTenantUsers, tenant.ID.String())
	emitAudit(h.msgBus, r, "tenant.created", "tenant", tenant.ID.String())
	writeJSON(w, http.StatusCreated, tenant)
}

func (h *TenantsHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	if !store.IsOwnerRole(r.Context()) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": i18n.T(locale, i18n.MsgPermissionDenied, "tenants.get")})
		return
	}

	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "tenant")})
		return
	}

	tenant, err := h.tenantStore.GetTenant(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "tenant", id.String())})
		return
	}
	writeJSON(w, http.StatusOK, tenant)
}

func (h *TenantsHandler) handleUpdate(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	if !store.IsOwnerRole(r.Context()) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": i18n.T(locale, i18n.MsgPermissionDenied, "tenants.update")})
		return
	}

	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "tenant")})
		return
	}

	var input struct {
		Name     string         `json:"name"`
		Status   string         `json:"status"`
		Settings map[string]any `json:"settings"`
	}
	if !bindJSON(w, r, locale, &input) {
		return
	}

	updates := make(map[string]any)
	if input.Name != "" {
		updates["name"] = input.Name
	}
	if input.Status != "" {
		updates["status"] = input.Status
	}
	if input.Settings != nil {
		updates["settings"] = input.Settings
	}

	if len(updates) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidUpdates)})
		return
	}

	if err := h.tenantStore.UpdateTenant(r.Context(), id, updates); err != nil {
		slog.Error("tenants.update failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToUpdate, "tenant", err.Error())})
		return
	}

	h.emitCacheInvalidate(bus.CacheKindTenantUsers, id.String())
	emitAudit(h.msgBus, r, "tenant.updated", "tenant", id.String())
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

func (h *TenantsHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	if !store.IsOwnerRole(r.Context()) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": i18n.T(locale, i18n.MsgPermissionDenied, "tenants.delete")})
		return
	}

	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "tenant")})
		return
	}

	if err := h.tenantStore.DeleteTenant(r.Context(), id); err != nil {
		slog.Error("tenants.delete failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToDelete, "tenant", err.Error())})
		return
	}

	h.emitCacheInvalidate(bus.CacheKindTenantUsers, id.String())
	h.emitCacheInvalidate(bus.CacheKindTenants, "")
	emitAudit(h.msgBus, r, "tenant.deleted", "tenant", id.String())
	w.WriteHeader(http.StatusNoContent)
}

func (h *TenantsHandler) handleUsersList(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	if !store.IsOwnerRole(r.Context()) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": i18n.T(locale, i18n.MsgPermissionDenied, "tenants.users.list")})
		return
	}

	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "tenant")})
		return
	}

	users, err := h.tenantStore.ListUsers(r.Context(), id)
	if err != nil {
		slog.Error("tenants.users.list failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToList, "tenant users")})
		return
	}
	if users == nil {
		users = []store.TenantUserData{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

func (h *TenantsHandler) handleUsersAdd(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	if !store.IsOwnerRole(r.Context()) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": i18n.T(locale, i18n.MsgPermissionDenied, "tenants.users.add")})
		return
	}

	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "tenant")})
		return
	}

	var input struct {
		UserID string `json:"user_id"`
		Role   string `json:"role"`
	}
	if !bindJSON(w, r, locale, &input) {
		return
	}

	if input.UserID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "user_id")})
		return
	}
	normalized, err := base.NormalizeUserID(r.Context(), h.masterDB, input.UserID)
	if err != nil {
		slog.Error("tenants.users.add normalize failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToCreate, "tenant user", err.Error())})
		return
	}
	isOwner := input.Role == store.TenantRoleOwner

	if err := h.tenantStore.AddUser(r.Context(), id, normalized, isOwner); err != nil {
		slog.Error("tenants.users.add failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToCreate, "tenant user", err.Error())})
		return
	}

	// Assign RBAC role for non-owner users (owner bypasses RBAC via is_owner flag).
	// System roles are seeded by migration 000083: "Admin", "Member", "Viewer".
	if !isOwner && h.roleStore != nil && input.Role != "" {
		if rbacRole, rErr := h.roleStore.GetRoleByName(r.Context(), id, capitalizeRole(input.Role)); rErr == nil && rbacRole != nil {
			if aErr := h.roleStore.AssignUserRole(r.Context(), id, normalized, rbacRole.ID); aErr != nil {
				slog.Warn("tenants.users.add rbac_assign_failed", "tenant_id", id, "user_id", normalized, "role", input.Role, "error", aErr)
			}
		} else if rErr != nil {
			slog.Warn("tenants.users.add rbac_lookup_failed", "tenant_id", id, "role", input.Role, "error", rErr)
		}
	}

	h.emitCacheInvalidate(bus.CacheKindTenantUsers, normalized)
	emitAudit(h.msgBus, r, "tenant.user.added", "tenant", id.String())
	writeJSON(w, http.StatusCreated, map[string]string{"ok": "true"})
}

func (h *TenantsHandler) handleUsersRemove(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	if !store.IsOwnerRole(r.Context()) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": i18n.T(locale, i18n.MsgPermissionDenied, "tenants.users.remove")})
		return
	}

	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "tenant")})
		return
	}

	userID := r.PathValue("userId")
	if userID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "userId")})
		return
	}

	if err := h.tenantStore.RemoveUser(r.Context(), id, userID); err != nil {
		slog.Error("tenants.users.remove failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToDelete, "tenant user", err.Error())})
		return
	}

	// Unassign all RBAC roles for this user in the tenant.
	if h.roleStore != nil {
		if roles, rErr := h.roleStore.ListUserRoles(r.Context(), id, userID); rErr == nil {
			for _, rl := range roles {
				if uErr := h.roleStore.UnassignUserRole(r.Context(), id, userID, rl.ID); uErr != nil {
					slog.Warn("tenants.users.remove rbac_unassign_failed", "tenant_id", id, "user_id", userID, "role_id", rl.ID, "error", uErr)
				}
			}
		}
	}

	h.emitCacheInvalidate(bus.CacheKindTenantUsers, userID)
	emitAudit(h.msgBus, r, "tenant.user.removed", "tenant", id.String())

	// Notify affected user's WS sessions to force logout
	if h.msgBus != nil {
		h.msgBus.Broadcast(bus.Event{
			Name:    protocol.EventTenantAccessRevoked,
			Payload: map[string]string{"user_id": userID, "tenant_id": id.String()},
		})
	}

	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

func (h *TenantsHandler) emitCacheInvalidate(kind, key string) {
	if h.msgBus == nil {
		return
	}
	h.msgBus.Broadcast(bus.Event{
		Name:    protocol.EventCacheInvalidate,
		Payload: bus.CacheInvalidatePayload{Kind: kind, Key: key},
	})
}

// capitalizeRole maps a lowercase role string to the RBAC system role name
// (e.g. "admin" → "Admin", "member" → "Member").
func capitalizeRole(role string) string {
	if role == "" {
		return ""
	}
	return strings.ToUpper(role[:1]) + role[1:]
}

// seedSystemRoles creates the default system roles (Admin, Member, Viewer)
// for a newly created tenant. Mirrors migration 000083 seed data.
func (h *TenantsHandler) seedSystemRoles(ctx context.Context, tenantID uuid.UUID) {
	if h.roleStore == nil {
		return
	}

	type sysRole struct {
		name        string
		description string
		permissions []string
	}
	roles := []sysRole{
		{
			name:        "Admin",
			description: "Full tenant administration",
			permissions: permissions.AdminSeedPermissions,
		},
		{
			name:        "Member",
			description: "Regular member",
			permissions: []string{
				"group.list", "group.get", "group.view_hierarchy",
				"artifact.upload_personal", "artifact.submit_review",
				"agent.create_personal", "artifact.view_group",
				"artifact.view_tenant", "artifact.delete_own",
			},
		},
		{
			name:        "Viewer",
			description: "Read-only access",
			permissions: []string{
				"group.list", "group.get",
				"artifact.view_group", "artifact.view_tenant",
			},
		},
	}

	for _, r := range roles {
		rd := &store.RoleData{
			ID:          store.GenNewID(),
			TenantID:    tenantID,
			Name:        r.name,
			Description: &r.description,
			IsSystem:    true,
			Permissions: r.permissions,
		}
		if err := h.roleStore.CreateRole(ctx, rd); err != nil {
			slog.Warn("tenants.seed_roles.create_failed", "tenant_id", tenantID, "role", r.name, "error", err)
			continue
		}
		if err := h.roleStore.SetRolePermissions(ctx, rd.ID, r.permissions); err != nil {
			slog.Warn("tenants.seed_roles.perms_failed", "tenant_id", tenantID, "role", r.name, "error", err)
		}
	}
}