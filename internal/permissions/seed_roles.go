package permissions

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// MemberSeedPermissions is the canonical set of permissions assigned to the Member
// system role when a new tenant is created.
var MemberSeedPermissions = []string{
	"group.list", "group.get", "group.view_hierarchy",
	"artifact.upload_personal", "artifact.submit_review",
	"agent.create_personal", "artifact.view_group",
	"artifact.view_tenant", "artifact.delete_own",
}

// ViewerSeedPermissions is the canonical set of permissions assigned to the Viewer
// system role when a new tenant is created.
var ViewerSeedPermissions = []string{
	"group.list", "group.get",
	"artifact.view_group", "artifact.view_tenant",
}

// SeedSystemRoles creates the Admin, Member, and Viewer system roles for a new tenant.
// This is the single source of truth — both HTTP and WS tenant handlers must call this
// function instead of inlining the logic.
func SeedSystemRoles(ctx context.Context, roleStore store.RoleStore, tenantID uuid.UUID) {
	if roleStore == nil {
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
			permissions: AdminSeedPermissions,
		},
		{
			name:        "Member",
			description: "Regular member",
			permissions: MemberSeedPermissions,
		},
		{
			name:        "Viewer",
			description: "Read-only access",
			permissions: ViewerSeedPermissions,
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
		if err := roleStore.CreateRole(ctx, rd); err != nil {
			slog.Warn("tenants.seed_roles.create_failed", "tenant_id", tenantID, "role", r.name, "error", err)
			continue
		}
		if err := roleStore.SetRolePermissions(ctx, rd.ID, r.permissions); err != nil {
			slog.Warn("tenants.seed_roles.perms_failed", "tenant_id", tenantID, "role", r.name, "error", err)
		}
	}
}
