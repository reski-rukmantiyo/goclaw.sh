package methods

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/base"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
	"github.com/nextlevelbuilder/goclaw/internal/tenantauth"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

var slugRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// TenantsMethods handles tenant management RPC methods.
type TenantsMethods struct {
	tenantStore       store.TenantStore
	tenantDBConnStore store.TenantDBConnectionStore
	tenantDBManager   store.TenantDBManager
	systemConfigStore store.SystemConfigStore
	authLoader        tenantauth.Loader
	encKey            string
	masterDB          *sql.DB
	msgBus            *bus.MessageBus
	workspace         string // base workspace directory for tenant dirs
	defaultSSLMode    string // default sslmode for auto-generated tenant DBs
	masterDSN         string // master DSN for superuser schema operations on tenant DBs
	ownerIDs          []string // global owner IDs from config; used to distinguish global owners from tenant owners
	roleStore         store.RoleStore // RBAC role assignment for tenant users
}

// NewTenantsMethods creates a new TenantsMethods handler.
func NewTenantsMethods(tenantStore store.TenantStore, tenantDBConnStore store.TenantDBConnectionStore, tenantDBManager store.TenantDBManager, masterDB *sql.DB, msgBus *bus.MessageBus, workspace string, defaultSSLMode string, masterDSN string, systemConfigStore store.SystemConfigStore, authLoader tenantauth.Loader, encKey string, ownerIDs []string, roleStore store.RoleStore) *TenantsMethods {
	return &TenantsMethods{tenantStore: tenantStore, tenantDBConnStore: tenantDBConnStore, tenantDBManager: tenantDBManager, masterDB: masterDB, msgBus: msgBus, workspace: workspace, defaultSSLMode: defaultSSLMode, masterDSN: masterDSN, systemConfigStore: systemConfigStore, authLoader: authLoader, encKey: encKey, ownerIDs: ownerIDs, roleStore: roleStore}
}

// Register registers tenant management RPC methods.
func (m *TenantsMethods) Register(router *gateway.MethodRouter) {
	router.Register("tenants.list", m.handleList)
	router.Register("tenants.get", m.handleGet)
	router.Register("tenants.create", m.handleCreate)
	router.Register("tenants.update", m.handleUpdate)
	router.Register("tenants.users.list", m.handleUsersList)
	router.Register("tenants.users.add", m.handleUsersAdd)
	router.Register("tenants.users.remove", m.handleUsersRemove)
	router.Register("tenants.delete", m.handleDelete)
	router.Register("tenants.mine", m.handleMine)
	router.Register(protocol.MethodTenantAuthGet, m.requireAdmin(m.handleAuthGet))
	router.Register(protocol.MethodTenantAuthPatch, m.requireAdmin(m.handleAuthPatch))
}

// requireAdmin gates handlers to admin+owner roles.
func (m *TenantsMethods) requireAdmin(next gateway.MethodHandler) gateway.MethodHandler {
	return func(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
		if !permissions.HasMinRole(client.Role(), permissions.RoleAdmin) {
			locale := store.LocaleFromContext(ctx)
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgPermissionDenied, req.Method)))
			return
		}
		next(ctx, client, req)
	}
}

// capitalizeRole maps lowercase role strings ("admin", "member", "viewer")
// to seeded system role names ("Admin", "Member", "Viewer").
func capitalizeRole(role string) string {
	if role == "" {
		return ""
	}
	return strings.ToUpper(role[:1]) + role[1:]
}

// seedSystemRoles creates the default system roles (Admin, Member, Viewer)
// for a newly created tenant. Mirrors migration 000083 seed data.
func (m *TenantsMethods) seedSystemRoles(ctx context.Context, tenantID uuid.UUID) {
	if m.roleStore == nil {
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
			permissions: []string{
				"user.list", "user.get", "user.create", "user.update", "user.delete",
				"user.enroll", "user.unenroll", "user.assign_role",
				"group.list", "group.get", "group.create", "group.update", "group.delete",
				"group.manage_members", "group.assign_role",
				"role.list", "role.get", "role.create", "role.update", "role.delete",
				"audit.view_all", "system.manage_settings", "system.manage_auth", "system.view_health",
			},
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
		if err := m.roleStore.CreateRole(ctx, rd); err != nil {
			slog.Warn("tenants.seed_roles.create_failed", "tenant_id", tenantID, "role", r.name, "error", err)
			continue
		}
		if err := m.roleStore.SetRolePermissions(ctx, rd.ID, r.permissions); err != nil {
			slog.Warn("tenants.seed_roles.perms_failed", "tenant_id", tenantID, "role", r.name, "error", err)
		}
	}
}

func (m *TenantsMethods) handleList(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if !slices.Contains(m.ownerIDs, client.UserID()) {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgPermissionDenied, "tenants.list")))
		return
	}

	tenants, err := m.tenantStore.ListTenants(ctx)
	if err != nil {
		slog.Error("tenants.list failed", "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToList, "tenants")))
		return
	}
	if tenants == nil {
		tenants = []store.TenantData{}
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"tenants": tenants}))
}

func (m *TenantsMethods) handleGet(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if !client.IsOwner() {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgPermissionDenied, "tenants.get")))
		return
	}

	var params struct {
		ID string `json:"id"`
	}
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
			return
		}
	}

	id, err := uuid.Parse(params.ID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "tenant")))
		return
	}

	tenant, err := m.tenantStore.GetTenant(ctx, id)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "tenant", params.ID)))
		return
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, tenant))
}

func (m *TenantsMethods) handleCreate(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if !client.IsOwner() && !client.HasScope(permissions.ScopeProvision) {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgPermissionDenied, "tenants.create")))
		return
	}

	var params struct {
		Name        string `json:"name"`
		Slug        string `json:"slug"`
		Settings    any    `json:"settings"`
		DBConnection *struct {
			Host         string `json:"host"`
			Port         int    `json:"port"`
			DatabaseName string `json:"database_name"`
			Username     string `json:"username"`
			Password     string `json:"password"`
			SSLMode      string `json:"ssl_mode"`
		} `json:"db_connection,omitempty"`
	}
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
			return
		}
	}

	if params.Name == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "name")))
		return
	}
	if params.Slug == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "slug")))
		return
	}
	if !slugRe.MatchString(params.Slug) {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidSlug, "slug")))
		return
	}

	tenant := &store.TenantData{
		ID:     store.GenNewID(),
		Name:   params.Name,
		Slug:   params.Slug,
		Status: store.TenantStatusActive,
	}

	if err := m.tenantStore.CreateTenant(ctx, tenant); err != nil {
		slog.Error("tenants.create failed", "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToCreate, "tenant", err.Error())))
		return
	}

	// Seed default system roles for the new tenant.
	m.seedSystemRoles(ctx, tenant.ID)

	// Provision tenant database if infrastructure available
	if m.tenantDBConnStore != nil && m.masterDB != nil {
		var dbConn *store.TenantDBConnection
		if params.DBConnection != nil {
			dbConn = &store.TenantDBConnection{
				TenantID:     tenant.ID,
				Host:         params.DBConnection.Host,
				Port:         params.DBConnection.Port,
				DatabaseName: params.DBConnection.DatabaseName,
				Username:     params.DBConnection.Username,
				Password:     params.DBConnection.Password,
				SSLMode:      params.DBConnection.SSLMode,
			}
		}
		provisionedConn, err := pg.ProvisionTenantDB(ctx, m.masterDB, tenant.ID, tenant.Slug, dbConn, "", m.defaultSSLMode, m.masterDSN)
		if err != nil {
			slog.Error("tenants.create: db provision failed", "tenant_id", tenant.ID, "error", err)
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgTenantDBProvisionFailed, err.Error())))
			return
		}
		if err := m.tenantDBConnStore.Create(ctx, provisionedConn); err != nil {
			slog.Error("tenants.create: failed to save db connection", "tenant_id", tenant.ID, "error", err)
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToCreate, "tenant db connection", err.Error())))
			return
		}
		if m.tenantDBManager != nil {
			m.tenantDBManager.Invalidate(tenant.ID)
		}
	}

	// Create workspace directory for the tenant.
	if m.workspace != "" {
		tenantDir := filepath.Join(m.workspace, "tenants", tenant.Slug)
		if err := os.MkdirAll(tenantDir, 0755); err != nil {
			slog.Warn("tenants.create: failed to create workspace dir", "dir", tenantDir, "error", err)
		}
	}

	m.emitCacheInvalidate(bus.CacheKindTenantUsers, tenant.ID.String())
	m.emitCacheInvalidate(bus.CacheKindTenants, "")
	client.SendResponse(protocol.NewOKResponse(req.ID, tenant))
}

func (m *TenantsMethods) handleUpdate(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if !client.IsOwner() {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgPermissionDenied, "tenants.update")))
		return
	}

	var params struct {
		ID       string         `json:"id"`
		Name     string         `json:"name"`
		Status   string         `json:"status"`
		Settings map[string]any `json:"settings"`
	}
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
			return
		}
	}

	id, err := uuid.Parse(params.ID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "tenant")))
		return
	}

	updates := make(map[string]any)
	if params.Name != "" {
		updates["name"] = params.Name
	}
	if params.Status != "" {
		updates["status"] = params.Status
	}
	if params.Settings != nil {
		updates["settings"] = params.Settings
	}

	if len(updates) == 0 {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidUpdates)))
		return
	}

	if err := m.tenantStore.UpdateTenant(ctx, id, updates); err != nil {
		slog.Error("tenants.update failed", "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToUpdate, "tenant", err.Error())))
		return
	}

	// Sync auth settings to system_configs if present
	if authRaw, ok := params.Settings["auth"]; ok && m.systemConfigStore != nil {
		authJSON, _ := json.Marshal(authRaw)
		var authCfg config.AuthConfig
		if err := json.Unmarshal(authJSON, &authCfg); err == nil {
			if err := tenantauth.SyncAuthConfigToSystemConfigs(ctx, m.systemConfigStore, id, &authCfg, m.encKey); err != nil {
				slog.Warn("tenants.update: failed to sync auth config", "tenant_id", id, "error", err)
			} else {
				m.authLoader.Invalidate(id)
			}
		}
	}

	m.emitCacheInvalidate(bus.CacheKindTenantUsers, id.String())
	m.emitCacheInvalidate(bus.CacheKindTenants, "")
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]string{"ok": "true"}))
}

func (m *TenantsMethods) handleUsersList(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if !client.IsOwner() {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgPermissionDenied, "tenants.users.list")))
		return
	}

	var params struct {
		TenantID string `json:"tenant_id"`
	}
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
			return
		}
	}

	tid, err := uuid.Parse(params.TenantID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "tenant_id")))
		return
	}

	users, err := m.tenantStore.ListUsers(ctx, tid)
	if err != nil {
		slog.Error("tenants.users.list failed", "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToList, "tenant users")))
		return
	}
	if users == nil {
		users = []store.TenantUserData{}
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"users": users}))
}

func (m *TenantsMethods) handleUsersAdd(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if !client.IsOwner() && !client.HasScope(permissions.ScopeProvision) {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgPermissionDenied, "tenants.users.add")))
		return
	}

	var params struct {
		TenantID string `json:"tenant_id"`
		UserID   string `json:"user_id"`
		Role     string `json:"role"`
	}
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
			return
		}
	}

	if params.UserID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "user_id")))
		return
	}
	normalized, err := base.NormalizeUserID(ctx, m.masterDB, params.UserID)
	if err != nil {
		slog.Error("tenants.users.add normalize failed", "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToCreate, "tenant user", err.Error())))
		return
	}
	isOwner := params.Role == store.TenantRoleOwner

	tid, err := uuid.Parse(params.TenantID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "tenant_id")))
		return
	}

	if err := m.tenantStore.AddUser(ctx, tid, normalized, isOwner); err != nil {
		slog.Error("tenants.users.add failed", "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToCreate, "tenant user", err.Error())))
		return
	}

	// Assign RBAC role for non-owner users (owner bypasses RBAC via is_owner flag).
	// System roles are seeded by migration 000083: "Admin", "Member", "Viewer".
	if !isOwner && m.roleStore != nil && params.Role != "" {
		if rbacRole, rErr := m.roleStore.GetRoleByName(ctx, tid, capitalizeRole(params.Role)); rErr == nil && rbacRole != nil {
			if aErr := m.roleStore.AssignUserRole(ctx, tid, normalized, rbacRole.ID); aErr != nil {
				slog.Warn("tenants.users.add rbac_assign_failed", "tenant_id", tid, "user_id", normalized, "role", params.Role, "error", aErr)
			}
		} else if rErr != nil {
			slog.Warn("tenants.users.add rbac_lookup_failed", "tenant_id", tid, "role", params.Role, "error", rErr)
		}
	}

	m.emitCacheInvalidate(bus.CacheKindTenantUsers, normalized)
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]string{"ok": "true"}))
}

func (m *TenantsMethods) handleUsersRemove(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if !client.IsOwner() {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgPermissionDenied, "tenants.users.remove")))
		return
	}

	var params struct {
		TenantID string `json:"tenant_id"`
		UserID   string `json:"user_id"`
	}
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
			return
		}
	}

	if params.UserID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "user_id")))
		return
	}

	tid, err := uuid.Parse(params.TenantID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "tenant_id")))
		return
	}

	if err := m.tenantStore.RemoveUser(ctx, tid, params.UserID); err != nil {
		slog.Error("tenants.users.remove failed", "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToDelete, "tenant user", err.Error())))
		return
	}

	// Unassign all RBAC roles for this user in the tenant.
	if m.roleStore != nil {
		if roles, rErr := m.roleStore.ListUserRoles(ctx, tid, params.UserID); rErr == nil {
			for _, r := range roles {
				if uErr := m.roleStore.UnassignUserRole(ctx, tid, params.UserID, r.ID); uErr != nil {
					slog.Warn("tenants.users.remove rbac_unassign_failed", "tenant_id", tid, "user_id", params.UserID, "role_id", r.ID, "error", uErr)
				}
			}
		}
	}

	m.emitCacheInvalidate(bus.CacheKindTenantUsers, params.UserID)

	// Notify affected user's WS sessions to force logout
	m.msgBus.Broadcast(bus.Event{
		Name:    protocol.EventTenantAccessRevoked,
		Payload: map[string]string{"user_id": params.UserID, "tenant_id": tid.String()},
	})

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]string{"ok": "true"}))
}

func (m *TenantsMethods) handleDelete(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if !client.IsOwner() {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgPermissionDenied, "tenants.delete")))
		return
	}

	var params struct {
		TenantID string `json:"tenant_id"`
	}
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
			return
		}
	}

	tid, err := uuid.Parse(params.TenantID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "tenant_id")))
		return
	}

	if err := m.tenantStore.DeleteTenant(ctx, tid); err != nil {
		slog.Error("tenants.delete failed", "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToDelete, "tenant", err.Error())))
		return
	}

	m.emitCacheInvalidate(bus.CacheKindTenantUsers, tid.String())
	m.emitCacheInvalidate(bus.CacheKindTenants, "")
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]string{"ok": "true"}))
}

// handleMine returns the current user's tenant memberships.
// Unlike other tenant methods, this does NOT require cross-tenant access.
// Cross-tenant admins receive all tenants instead.
func (m *TenantsMethods) handleMine(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)

	type tenantEntry struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Slug   string `json:"slug"`
		Role   string `json:"role"`
		Status string `json:"status"`
	}

	// Global owner: return all tenants with "owner" role.
	// Tenant owners (not in the global owner list) should only see their memberships.
	if client.IsOwner() && slices.Contains(m.ownerIDs, client.UserID()) {
		tenants, err := m.tenantStore.ListTenants(ctx)
		if err != nil {
			slog.Error("tenants.mine failed (cross-tenant)", "error", err)
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToList, "tenants")))
			return
		}
		entries := make([]tenantEntry, len(tenants))
		for i, t := range tenants {
			entries[i] = tenantEntry{ID: t.ID.String(), Name: t.Name, Slug: t.Slug, Role: "owner", Status: t.Status}
		}
		client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"tenants": entries}))
		return
	}

	// Regular user: return their tenant memberships enriched with name/slug
	userID := client.UserID()
	if userID == "" {
		client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"tenants": []tenantEntry{}}))
		return
	}

	memberships, err := m.tenantStore.ListUserTenants(ctx, userID)
	if err != nil {
		slog.Error("tenants.mine failed", "error", err, "user_id", userID)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToList, "tenants")))
		return
	}

	// Batch-fetch all tenant data in a single query instead of per-membership.
	ids := make([]uuid.UUID, 0, len(memberships))
	for _, mem := range memberships {
		ids = append(ids, mem.TenantID)
	}
	tenants, tErr := m.tenantStore.GetTenantsByIDs(ctx, ids)
	if tErr != nil {
		slog.Error("tenants.mine: batch fetch failed", "error", tErr, "user_id", userID)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToList, "tenants")))
		return
	}
	tenantMap := make(map[uuid.UUID]*store.TenantData, len(tenants))
	for i := range tenants {
		tenantMap[tenants[i].ID] = &tenants[i]
	}

	entries := make([]tenantEntry, 0, len(memberships))
	for _, mem := range memberships {
		if mem.TenantID == store.MasterTenantID {
			continue
		}
		t := tenantMap[mem.TenantID]
		if t == nil || t.Status != store.TenantStatusActive {
			continue
		}
		role := "member"
		if mem.IsOwner {
			role = "owner"
		}
		entries = append(entries, tenantEntry{ID: t.ID.String(), Name: t.Name, Slug: t.Slug, Role: role, Status: t.Status})
	}

		// Fallback for users created before tenant_users membership was auto-created:
		// synthesize an entry from the client's resolved tenant (set via JWT tid claim).
		if len(entries) == 0 && client.TenantID() != uuid.Nil {
			t, tErr := m.tenantStore.GetTenant(ctx, client.TenantID())
			if tErr == nil && t != nil && t.Status == store.TenantStatusActive {
				entries = append(entries, tenantEntry{ID: t.ID.String(), Name: t.Name, Slug: t.Slug, Role: "member", Status: t.Status})
			}
		}

		client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"tenants": entries}))
}

func (m *TenantsMethods) handleAuthGet(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	tenantID := client.TenantID()
	if tenantID == uuid.Nil {
		tenantID = store.MasterTenantID
	}

	cfg, err := m.authLoader.LoadAuthConfig(ctx, tenantID)
	if err != nil {
		slog.Error("tenant.auth.get failed", "tenant_id", tenantID, "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "auth config")))
		return
	}

	// Mask secrets before sending to client
	if cfg.Providers.EntraID != nil && cfg.Providers.EntraID.ClientSecret != "" {
		cfg.Providers.EntraID.ClientSecret = "***"
	}
	if cfg.Providers.Google != nil && cfg.Providers.Google.ClientSecret != "" {
		cfg.Providers.Google.ClientSecret = "***"
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, cfg))
}

func (m *TenantsMethods) handleAuthPatch(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	tenantID := client.TenantID()
	if tenantID == uuid.Nil {
		tenantID = store.MasterTenantID
	}

	var params config.AuthConfig
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
			return
		}
	}

	if err := tenantauth.SyncAuthConfigToSystemConfigs(ctx, m.systemConfigStore, tenantID, &params, m.encKey); err != nil {
		slog.Error("tenant.auth.patch sync failed", "tenant_id", tenantID, "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToUpdate, "auth config", err.Error())))
		return
	}

	m.authLoader.Invalidate(tenantID)
	m.emitCacheInvalidate(bus.CacheKindTenants, tenantID.String())
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]string{"ok": "true"}))
}

func (m *TenantsMethods) emitCacheInvalidate(kind, key string) {
	if m.msgBus == nil {
		return
	}
	m.msgBus.Broadcast(bus.Event{
		Name:    protocol.EventCacheInvalidate,
		Payload: bus.CacheInvalidatePayload{Kind: kind, Key: key},
	})
}
