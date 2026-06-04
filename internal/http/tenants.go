package http

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/auth"
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
	userStore         store.UserStore
}

// NewTenantsHandler creates a handler for tenant management endpoints.
func NewTenantsHandler(tenantStore store.TenantStore, tenantDBConnStore store.TenantDBConnectionStore, tenantDBManager store.TenantDBManager, masterDB *sql.DB, msgBus *bus.MessageBus, workspace string, defaultSSLMode string, masterDSN string, roleStore store.RoleStore, userStore store.UserStore) *TenantsHandler {
	return &TenantsHandler{tenantStore: tenantStore, tenantDBConnStore: tenantDBConnStore, tenantDBManager: tenantDBManager, masterDB: masterDB, msgBus: msgBus, workspace: workspace, defaultSSLMode: defaultSSLMode, masterDSN: masterDSN, roleStore: roleStore, userStore: userStore}
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
	mux.HandleFunc("GET /v1/tenants/{id}/users/{userId}", admin(h.handleUsersGet))
	mux.HandleFunc("PUT /v1/tenants/{id}/users/{userId}", admin(h.handleUsersUpdate))
	mux.HandleFunc("PUT /v1/tenants/{id}/users/{userId}/role", admin(h.handleUsersUpdateRole))
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
	ctx := r.Context()

	tenantID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "tenant")})
		return
	}

	users, err := h.tenantStore.ListUsers(ctx, tenantID)
	if err != nil {
		slog.Error("tenants.users.list failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToList, "tenant users")})
		return
	}
	if users == nil {
		users = []store.TenantUserData{}
	}

	// Determine caller level for filtering
	isAdminCaller := h.isCallerAdmin(ctx, tenantID) && !h.isCallerOwnerOrGateway(ctx, tenantID)

	// Enrich with email, role, phone
	enriched := h.enrichTenantUsers(ctx, users, isAdminCaller)

	writeJSON(w, http.StatusOK, map[string]any{"users": enriched})
}

func (h *TenantsHandler) handleUsersAdd(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	ctx := r.Context()

	tenantID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "tenant")})
		return
	}

	var input struct {
		// Enrollment mode (existing user)
		UserID string `json:"user_id"`
		// Create mode (new user)
		Email       string  `json:"email"`
		DisplayName string  `json:"display_name"`
		Password    string  `json:"password"`
		Phone       *string `json:"phone"`
		// Shared
		Role string `json:"role"`
	}
	if !bindJSON(w, r, locale, &input) {
		return
	}

	// Determine mode: create (email) vs enroll (user_id)
	isCreateMode := input.Email != "" && input.UserID == ""
	if !isCreateMode && input.UserID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "user_id or email")})
		return
	}

	var normalized string

	if isCreateMode {
		// ── Create + enroll mode ──
		// Auth: Owner/Gateway can create any role; Admin can only create Member/Viewer
		isOwnerOrGateway := h.isCallerOwnerOrGateway(ctx, tenantID)
		isAdmin := h.isCallerAdmin(ctx, tenantID)

		if !isOwnerOrGateway && !isAdmin {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": i18n.T(locale, i18n.MsgPermissionDenied, "tenants.users.add")})
			return
		}

		// Role validation
		if input.Role == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "role")})
			return
		}
		validRoles := map[string]bool{
			store.TenantRoleOwner: true, store.TenantRoleAdmin: true,
			store.TenantRoleMember: true, store.TenantRoleViewer: true,
		}
		if !validRoles[input.Role] {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRole)})
			return
		}
		// Admin cannot create Owner or Admin
		if isAdmin && (input.Role == store.TenantRoleOwner || input.Role == store.TenantRoleAdmin) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": i18n.T(locale, i18n.MsgRoleNotPermitted, input.Role)})
			return
		}

		// Password validation
		if input.Password == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "password")})
			return
		}
		lengthOK, hasUpper, hasSymbol := auth.ValidatePasswordComplexity(input.Password)
		if !lengthOK || !hasUpper || !hasSymbol {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgAuthPasswordComplexity)})
			return
		}

		// Duplicate email check
		existing, _ := h.userStore.GetByEmail(ctx, input.Email)
		if existing != nil {
			writeJSON(w, http.StatusConflict, map[string]string{"error": i18n.T(locale, i18n.MsgAlreadyExists, "email", input.Email)})
			return
		}

		// Create user
		hash, hashErr := auth.HashPassword(input.Password)
		if hashErr != nil {
			slog.Error("tenants.users.add hash_password failed", "error", hashErr)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, hashErr.Error())})
			return
		}

		now := time.Now()
		newUser := &store.UserData{
			ID:           uuid.New(),
			Email:        input.Email,
			DisplayName:  input.DisplayName,
			Phone:        input.Phone,
			AuthProvider: store.AuthProviderLocal,
			PasswordHash: &hash,
			Status:       store.UserStatusActive,
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		if err := h.userStore.Create(ctx, newUser); err != nil {
			slog.Error("tenants.users.add create_user failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToCreate, "user", err.Error())})
			return
		}
		normalized = newUser.ID.String()
	} else {
		// ── Enrollment mode (existing user) ──
		if !h.isCallerOwnerOrGateway(ctx, tenantID) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": i18n.T(locale, i18n.MsgPermissionDenied, "tenants.users.add")})
			return
		}
		normalized, err = base.NormalizeUserID(ctx, h.masterDB, input.UserID)
		if err != nil {
			slog.Error("tenants.users.add normalize failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToCreate, "tenant user", err.Error())})
			return
		}
	}

	isOwner := input.Role == store.TenantRoleOwner

	if err := h.tenantStore.AddUser(ctx, tenantID, normalized, isOwner); err != nil {
		slog.Error("tenants.users.add failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToCreate, "tenant user", err.Error())})
		return
	}

	// Assign RBAC role for non-owner users
	if !isOwner && h.roleStore != nil && input.Role != "" {
		if rbacRole, rErr := h.roleStore.GetRoleByName(ctx, tenantID, capitalizeRole(input.Role)); rErr == nil && rbacRole != nil {
			if aErr := h.roleStore.AssignUserRole(ctx, tenantID, normalized, rbacRole.ID); aErr != nil {
				slog.Warn("tenants.users.add rbac_assign_failed", "tenant_id", tenantID, "user_id", normalized, "role", input.Role, "error", aErr)
			}
		} else if rErr != nil {
			slog.Warn("tenants.users.add rbac_lookup_failed", "tenant_id", tenantID, "role", input.Role, "error", rErr)
		}
	}

	h.emitCacheInvalidate(bus.CacheKindTenantUsers, normalized)
	emitAudit(h.msgBus, r, "tenant.user.added", "tenant", tenantID.String())
	writeJSON(w, http.StatusCreated, map[string]string{"ok": "true"})
}

func (h *TenantsHandler) handleUsersRemove(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	ctx := r.Context()

	tenantID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "tenant")})
		return
	}

	userID := r.PathValue("userId")
	if userID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "userId")})
		return
	}

	normalized, normErr := base.NormalizeUserID(ctx, h.masterDB, userID)
	if normErr != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "user")})
		return
	}

	// Self-deletion guard
	callerID := store.UserIDFromContext(ctx)
	if callerID == normalized {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": i18n.T(locale, i18n.MsgSelfDeleteBlocked)})
		return
	}

	// Auth check with target-role awareness
	targetRole := h.checkTenantUserAuth(w, r, tenantID, normalized, "delete")
	if targetRole == "" {
		return
	}

	// Last-owner guard
	if targetRole == store.TenantRoleOwner {
		count, countErr := h.tenantStore.CountOwners(ctx, tenantID)
		if countErr != nil {
			slog.Error("tenants.users.remove count_owners failed", "error", countErr)
		} else if count <= 1 {
			writeJSON(w, http.StatusConflict, map[string]string{"error": i18n.T(locale, i18n.MsgLastOwnerBlocked)})
			return
		}
	}

	if err := h.tenantStore.RemoveUser(ctx, tenantID, normalized); err != nil {
		slog.Error("tenants.users.remove failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToDelete, "tenant user", err.Error())})
		return
	}

	// Unassign all RBAC roles
	if h.roleStore != nil {
		if roles, rErr := h.roleStore.ListUserRoles(ctx, tenantID, normalized); rErr == nil {
			for _, rl := range roles {
				if uErr := h.roleStore.UnassignUserRole(ctx, tenantID, normalized, rl.ID); uErr != nil {
					slog.Warn("tenants.users.remove rbac_unassign_failed", "tenant_id", tenantID, "user_id", normalized, "role_id", rl.ID, "error", uErr)
				}
			}
		}
	}

	h.emitCacheInvalidate(bus.CacheKindTenantUsers, normalized)
	emitAudit(h.msgBus, r, "tenant.user.removed", "tenant", tenantID.String())

	// Notify affected user's WS sessions to force logout
	if h.msgBus != nil {
		h.msgBus.Broadcast(bus.Event{
			Name:    protocol.EventTenantAccessRevoked,
			Payload: map[string]string{"user_id": normalized, "tenant_id": tenantID.String()},
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

// ──────────────────────────────────────────────────────────────
// Target-role-aware authorization helpers
// ──────────────────────────────────────────────────────────────

// resolveTargetRole returns the effective role of a tenant user.
// Returns "owner" if is_owner=true, otherwise derives from RBAC roles.
func (h *TenantsHandler) resolveTargetRole(ctx context.Context, tenantID uuid.UUID, userID string) string {
	tu, err := h.tenantStore.GetTenantUserByUser(ctx, tenantID, userID)
	if err != nil || tu == nil {
		return ""
	}
	if tu.IsOwner {
		return store.TenantRoleOwner
	}
	if h.roleStore == nil {
		return store.TenantRoleMember
	}
	roles, err := h.roleStore.ListUserRoles(ctx, tenantID, userID)
	if err != nil || len(roles) == 0 {
		return store.TenantRoleMember
	}
	perms, _ := h.roleStore.GetRolePermissions(ctx, roles[0].ID)
	for _, p := range perms {
		if p == string(permissions.PermSystemManageSettings) {
			return store.TenantRoleAdmin
		}
	}
	for _, p := range perms {
		if p != "group.list" && p != "group.get" && p != "artifact.view_group" && p != "artifact.view_tenant" {
			return store.TenantRoleMember
		}
	}
	return store.TenantRoleViewer
}

// isCallerOwnerOrGateway returns true if the caller is a system-wide owner
// (Gateway Token holder) or a per-tenant owner.
func (h *TenantsHandler) isCallerOwnerOrGateway(ctx context.Context, tenantID uuid.UUID) bool {
	if store.IsOwnerRole(ctx) {
		return true
	}
	userID := store.UserIDFromContext(ctx)
	isOwner, err := h.tenantStore.IsOwner(ctx, tenantID, userID)
	return err == nil && isOwner
}

// isCallerAdmin returns true if the caller has admin-level permissions
// (but is NOT an owner — use isCallerOwnerOrGateway for that).
func (h *TenantsHandler) isCallerAdmin(ctx context.Context, tenantID uuid.UUID) bool {
	if store.IsOwnerRole(ctx) {
		return false
	}
	userID := store.UserIDFromContext(ctx)
	if pkgPermCache != nil && pkgPermCache.HasPermission(ctx, userID, tenantID, permissions.PermSystemManageSettings) {
		isOwner, _ := h.tenantStore.IsOwner(ctx, tenantID, userID)
		return !isOwner
	}
	return false
}

// checkTenantUserAuth performs target-role-aware authorization for tenant user operations.
// Returns the resolved target role or "" on denial (error response written).
func (h *TenantsHandler) checkTenantUserAuth(w http.ResponseWriter, r *http.Request, tenantID uuid.UUID, targetUserID string, operation string) string {
	ctx := r.Context()
	locale := store.LocaleFromContext(ctx)

	// Owner or Gateway Token → full access
	if h.isCallerOwnerOrGateway(ctx, tenantID) {
		return h.resolveTargetRole(ctx, tenantID, targetUserID)
	}

	// Admin callers — restricted access
	if h.isCallerAdmin(ctx, tenantID) {
		switch operation {
		case "create":
			writeJSON(w, http.StatusForbidden, map[string]string{"error": i18n.T(locale, i18n.MsgTargetRoleForbidden)})
			return ""
		case "read", "update", "delete":
			targetRole := h.resolveTargetRole(ctx, tenantID, targetUserID)
			if targetRole == store.TenantRoleOwner || targetRole == store.TenantRoleAdmin {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": i18n.T(locale, i18n.MsgTargetRoleForbidden)})
				return ""
			}
			return targetRole
		case "update_role":
			writeJSON(w, http.StatusForbidden, map[string]string{"error": i18n.T(locale, i18n.MsgTargetRoleForbidden)})
			return ""
		}
	}

	writeJSON(w, http.StatusForbidden, map[string]string{"error": i18n.T(locale, i18n.MsgPermissionDenied, "tenant.user."+operation)})
	return ""
}

// ──────────────────────────────────────────────────────────────
// Tenant User CRUD handlers
// ──────────────────────────────────────────────────────────────

// handleUsersGet returns a single tenant user with enriched data.
func (h *TenantsHandler) handleUsersGet(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	ctx := r.Context()

	tenantID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "tenant")})
		return
	}

	userID := r.PathValue("userId")
	if userID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "userId")})
		return
	}

	normalized, err := base.NormalizeUserID(ctx, h.masterDB, userID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "user")})
		return
	}

	targetRole := h.checkTenantUserAuth(w, r, tenantID, normalized, "read")
	if targetRole == "" {
		return
	}

	tu, err := h.tenantStore.GetTenantUserByUser(ctx, tenantID, normalized)
	if err != nil {
		slog.Error("tenants.users.get failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, err.Error())})
		return
	}
	if tu == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "user", normalized)})
		return
	}

	resp := h.enrichTenantUser(ctx, tu, targetRole)
	writeJSON(w, http.StatusOK, resp)
}

// handleUsersUpdate updates a tenant user's display_name and phone.
func (h *TenantsHandler) handleUsersUpdate(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	ctx := r.Context()

	tenantID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "tenant")})
		return
	}

	userID := r.PathValue("userId")
	if userID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "userId")})
		return
	}

	normalized, err := base.NormalizeUserID(ctx, h.masterDB, userID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "user")})
		return
	}

	targetRole := h.checkTenantUserAuth(w, r, tenantID, normalized, "update")
	if targetRole == "" {
		return
	}

	var input struct {
		DisplayName *string `json:"display_name"`
		Phone       *string `json:"phone"`
		Email       *string `json:"email"`
		Role        *string `json:"role"`
	}
	if !bindJSON(w, r, locale, &input) {
		return
	}

	if input.Email != nil || input.Role != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": i18n.T(locale, i18n.MsgFieldNotUpdatable, "email, role")})
		return
	}
	if input.DisplayName == nil && input.Phone == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgNoUpdatesProvided)})
		return
	}

	userUUID, parseErr := uuid.Parse(normalized)
	if parseErr != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "user")})
		return
	}
	user, err := h.userStore.GetByID(ctx, userUUID)
	if err != nil {
		slog.Error("tenants.users.update get_user failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgInternalError, err.Error())})
		return
	}
	if user == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "user", normalized)})
		return
	}

	if input.DisplayName != nil {
		user.DisplayName = *input.DisplayName
	}
	if input.Phone != nil {
		user.Phone = input.Phone
	}

	if err := h.userStore.Update(ctx, user); err != nil {
		slog.Error("tenants.users.update failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToUpdate, "user", err.Error())})
		return
	}

	emitAudit(h.msgBus, r, "tenant.user.updated", "tenant_user", normalized)
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// handleUsersUpdateRole changes a tenant user's role.
func (h *TenantsHandler) handleUsersUpdateRole(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	ctx := r.Context()

	tenantID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "tenant")})
		return
	}

	userID := r.PathValue("userId")
	if userID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "userId")})
		return
	}

	normalized, err := base.NormalizeUserID(ctx, h.masterDB, userID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "user")})
		return
	}

	// Only Owner or Gateway Token can change roles
	if !h.isCallerOwnerOrGateway(ctx, tenantID) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": i18n.T(locale, i18n.MsgTargetRoleForbidden)})
		return
	}

	var input struct {
		Role string `json:"role"`
	}
	if !bindJSON(w, r, locale, &input) {
		return
	}

	validRoles := map[string]bool{
		store.TenantRoleOwner:  true,
		store.TenantRoleAdmin:  true,
		store.TenantRoleMember: true,
		store.TenantRoleViewer: true,
	}
	if !validRoles[input.Role] {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRole)})
		return
	}

	tu, err := h.tenantStore.GetTenantUserByUser(ctx, tenantID, normalized)
	if err != nil || tu == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "user", normalized)})
		return
	}

	newIsOwner := input.Role == store.TenantRoleOwner
	oldIsOwner := tu.IsOwner

	// Handle owner flag transition
	if newIsOwner != oldIsOwner {
		if err := h.tenantStore.UpdateOwnerFlag(ctx, tenantID, normalized, newIsOwner); err != nil {
			slog.Error("tenants.users.update_role owner_flag failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToUpdate, "user role", err.Error())})
			return
		}
	}

	if newIsOwner {
		// Owner: unassign all RBAC roles (owner bypasses RBAC)
		if h.roleStore != nil {
			if roles, rErr := h.roleStore.ListUserRoles(ctx, tenantID, normalized); rErr == nil {
				for _, rl := range roles {
					if uErr := h.roleStore.UnassignUserRole(ctx, tenantID, normalized, rl.ID); uErr != nil {
						slog.Warn("tenants.users.update_role unassign_failed", "role_id", rl.ID, "error", uErr)
					}
				}
			}
		}
	} else {
		// Non-owner: unassign old, assign new
		if h.roleStore != nil {
			if roles, rErr := h.roleStore.ListUserRoles(ctx, tenantID, normalized); rErr == nil {
				for _, rl := range roles {
					if uErr := h.roleStore.UnassignUserRole(ctx, tenantID, normalized, rl.ID); uErr != nil {
						slog.Warn("tenants.users.update_role unassign_failed", "role_id", rl.ID, "error", uErr)
					}
				}
			}
			if rbacRole, rErr := h.roleStore.GetRoleByName(ctx, tenantID, capitalizeRole(input.Role)); rErr == nil && rbacRole != nil {
				if aErr := h.roleStore.AssignUserRole(ctx, tenantID, normalized, rbacRole.ID); aErr != nil {
					slog.Warn("tenants.users.update_role assign_failed", "role", input.Role, "error", aErr)
				}
			}
		}
	}

	h.emitCacheInvalidate(bus.CacheKindTenantUsers, normalized)
	emitAudit(h.msgBus, r, "tenant.user.role_changed", "tenant_user", normalized)
	writeJSON(w, http.StatusOK, map[string]string{"status": "role_updated"})
}

// enrichTenantUser builds a response object from a TenantUserData with resolved role and email.
func (h *TenantsHandler) enrichTenantUser(ctx context.Context, tu *store.TenantUserData, role string) map[string]any {
	resp := map[string]any{
		"id":           tu.ID,
		"tenant_id":    tu.TenantID,
		"user_id":      tu.UserID,
		"display_name": tu.DisplayName,
		"is_owner":     tu.IsOwner,
		"role":         role,
		"created_at":   tu.CreatedAt,
		"updated_at":   tu.UpdatedAt,
	}
	if h.userStore != nil {
		if userUUID, parseErr := uuid.Parse(tu.UserID); parseErr == nil {
			if u, err := h.userStore.GetByID(ctx, userUUID); err == nil && u != nil {
				resp["email"] = u.Email
				resp["display_name"] = u.DisplayName
				resp["phone"] = u.Phone
				resp["status"] = u.Status
			}
		}
	}
	return resp
}

// enrichTenantUsers enriches a list of TenantUserData with resolved roles and emails.
// If isAdminCaller is true, filters out owner and admin users.
func (h *TenantsHandler) enrichTenantUsers(ctx context.Context, users []store.TenantUserData, isAdminCaller bool) []map[string]any {
	var result []map[string]any

	var userUUIDs []uuid.UUID
	for _, u := range users {
		if id, err := uuid.Parse(u.UserID); err == nil {
			userUUIDs = append(userUUIDs, id)
		}
	}
	emailMap := make(map[string]string)
	nameMap := make(map[string]string)
	phoneMap := make(map[string]*string)
	statusMap := make(map[string]string)
	if h.userStore != nil && len(userUUIDs) > 0 {
		if userData, err := h.userStore.GetByIDs(ctx, userUUIDs); err == nil {
			for _, u := range userData {
				emailMap[u.ID.String()] = u.Email
				nameMap[u.ID.String()] = u.DisplayName
				phoneMap[u.ID.String()] = u.Phone
				statusMap[u.ID.String()] = u.Status
			}
		}
	}

	for _, tu := range users {
		role := h.resolveTargetRole(ctx, tu.TenantID, tu.UserID)

		if isAdminCaller && (role == store.TenantRoleOwner || role == store.TenantRoleAdmin) {
			continue
		}

		result = append(result, map[string]any{
			"id":           tu.ID,
			"tenant_id":    tu.TenantID,
			"user_id":      tu.UserID,
			"display_name": nameMap[tu.UserID],
			"email":        emailMap[tu.UserID],
			"is_owner":     tu.IsOwner,
			"role":         role,
			"phone":        phoneMap[tu.UserID],
			"status":       statusMap[tu.UserID],
			"created_at":   tu.CreatedAt,
			"updated_at":   tu.UpdatedAt,
		})
	}

	if result == nil {
		result = []map[string]any{}
	}
	return result
}