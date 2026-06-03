package permissions

import (
	"strings"
	"testing"
)

// INVARIANT: Every permission string used in requireAuthAction() route guards
// for user management endpoints (internal/http/users.go) must exist in the
// permission catalog and AllPermissions(). A mismatch causes runtime 403 errors
// even for correctly-seeded Admin roles.
//
// This test was created after reski7@test.com (Admin on tenant-17) could not
// create users — the route guard used "user.pre_provision" but only "user.create"
// was seeded in the Admin role.
func TestUserPermission_RouteGuardsMatchCatalog(t *testing.T) {
	// Permission strings used in requireAuthAction() calls in users.go.
	// Keep this list in sync with the route registrations.
	routeGuardPermissions := []struct {
		perm     string
		endpoint string // HTTP route that uses this permission
	}{
		{"user.list", "GET /v1/users"},
		{"user.get", "GET /v1/users/{id}"},
		{"user.pre_provision", "POST /v1/users"},
		{"user.suspend", "PATCH /v1/users/{id}/status"},
		{"user.enroll", "POST /v1/users/{id}/tenants"},
		{"user.unenroll", "DELETE /v1/users/{id}/tenants/{tenantId}"},
		{"user.deactivate", "DELETE /v1/users/{id}"},
	}

	allPerms := AllPermissions()
	permSet := make(map[string]bool, len(allPerms))
	for _, p := range allPerms {
		permSet[string(p)] = true
	}

	for _, rg := range routeGuardPermissions {
		t.Run(rg.perm, func(t *testing.T) {
			if !permSet[rg.perm] {
				t.Errorf("INVARIANT VIOLATION: route guard %q uses permission %q but it is NOT in AllPermissions(). "+
					"Add a Perm constant to catalog.go and include it in AllPermissions().", rg.endpoint, rg.perm)
			}
		})
	}
}

// INVARIANT: The standard Admin role permission set must include all user
// management route-guard permissions. If the Admin role is missing a permission,
// tenant admins get 403 on user management endpoints.
func TestUserPermission_AdminSeedIncludesRouteGuardPerms(t *testing.T) {
	// Use canonical seed — same slice used by both seedSystemRoles functions.
	adminSeedPerms := AdminSeedPermissions

	// Route guard permissions that Admin MUST have
	requiredForAdmin := []string{
		"user.pre_provision",
		"user.suspend",
		"user.deactivate",
	}

	adminSet := make(map[string]bool, len(adminSeedPerms))
	for _, p := range adminSeedPerms {
		adminSet[p] = true
	}

	for _, req := range requiredForAdmin {
		t.Run(req, func(t *testing.T) {
			if !adminSet[req] {
				t.Errorf("INVARIANT VIOLATION: Admin role seed is missing %q. "+
					"Update migrations/000083 and sqlitestore/schema.go Admin seed data.", req)
			}
		})
	}
}

// INVARIANT: All permission constants in catalog.go must appear in AllPermissions().
func TestUserPermission_AllConstantsInAllPermissions(t *testing.T) {
	allPerms := AllPermissions()
	permSet := make(map[string]bool, len(allPerms))
	for _, p := range allPerms {
		permSet[string(p)] = true
	}

	constants := []struct {
		name string
		perm Permission
	}{
		{"PermUserPreProvision", PermUserPreProvision},
		{"PermUserSuspend", PermUserSuspend},
		{"PermUserDeactivate", PermUserDeactivate},
		{"PermUserCreate", PermUserCreate},
		{"PermUserList", PermUserList},
		{"PermUserGet", PermUserGet},
		{"PermUserUpdate", PermUserUpdate},
		{"PermUserDelete", PermUserDelete},
		{"PermUserEnroll", PermUserEnroll},
		{"PermUserUnenroll", PermUserUnenroll},
		{"PermUserAssignRole", PermUserAssignRole},
	}

	for _, c := range constants {
		t.Run(c.name, func(t *testing.T) {
			if !permSet[string(c.perm)] {
				t.Errorf("INVARIANT VIOLATION: %s = %q is not in AllPermissions(). "+
					"Add it to the AllPermissions() return value in catalog.go.", c.name, c.perm)
			}
		})
	}
}

// INVARIANT: AdminSeedPermissions (the canonical runtime seed used by both HTTP
// and WS seedSystemRoles) must include every permission used in requireAuthAction()
// route guards. A mismatch means newly created tenants have broken Admin roles.
//
// This test catches the exact bug that hit reski6@test.com: seedSystemRoles inlined
// the migration-000083 permissions (missing user.pre_provision/suspend/deactivate)
// while the route guard required them. Migration 85 fixed existing tenants but the
// runtime path was never updated.
func TestUserPermission_RuntimeSeedIncludesRouteGuardPerms(t *testing.T) {
	// Permissions required by requireAuthAction() in internal/http/users.go
	routeGuardPerms := []struct {
		perm     string
		endpoint string
	}{
		{"user.pre_provision", "POST /v1/users"},
		{"user.suspend", "PATCH /v1/users/{id}/status"},
		{"user.deactivate", "DELETE /v1/users/{id}"},
		{"user.list", "GET /v1/users"},
		{"user.get", "GET /v1/users/{id}"},
		{"user.enroll", "POST /v1/users/{id}/tenants"},
		{"user.unenroll", "DELETE /v1/users/{id}/tenants/{tenantId}"},
	}

	seedSet := make(map[string]bool, len(AdminSeedPermissions))
	for _, p := range AdminSeedPermissions {
		seedSet[p] = true
	}

	for _, rg := range routeGuardPerms {
		t.Run(rg.perm, func(t *testing.T) {
			if !seedSet[rg.perm] {
				t.Errorf("INVARIANT VIOLATION: AdminSeedPermissions missing %q (required by %s). "+
					"Add it to AdminSeedPermissions in catalog.go.", rg.perm, rg.endpoint)
			}
		})
	}
}

// INVARIANT: AdminSeedPermissions must be a superset of the seed data in the
// SQLite schema migration. The SQLite schema is the source of truth for desktop
// edition fresh installs.
func TestUserPermission_RuntimeSeedMatchesSchemaSeed(t *testing.T) {
	// These are the Admin permissions from sqlitestore/schema.go migration 43
	schemaAdminPerms := []string{
		"user.list", "user.get", "user.create", "user.update", "user.delete",
		"user.enroll", "user.unenroll", "user.assign_role",
		"user.pre_provision", "user.suspend", "user.deactivate",
		"group.list", "group.get", "group.create", "group.update", "group.delete",
		"group.manage_members", "group.assign_role",
		"role.list", "role.get", "role.create", "role.update", "role.delete",
		"audit.view_all", "system.manage_settings", "system.manage_auth", "system.view_health",
	}

	seedSet := make(map[string]bool, len(AdminSeedPermissions))
	for _, p := range AdminSeedPermissions {
		seedSet[p] = true
	}

	for _, p := range schemaAdminPerms {
		t.Run(p, func(t *testing.T) {
			if !seedSet[p] {
				t.Errorf("INVARIANT VIOLATION: AdminSeedPermissions missing %q which exists in SQLite schema seed.", p)
			}
		})
	}

	// Also verify no extra perms in runtime that schema doesn't have
	schemaSet := make(map[string]bool, len(schemaAdminPerms))
	for _, p := range schemaAdminPerms {
		schemaSet[p] = true
	}
	for _, p := range AdminSeedPermissions {
		t.Run("extra_"+p, func(t *testing.T) {
			if !schemaSet[p] {
				t.Errorf("INVARIANT VIOLATION: AdminSeedPermissions has extra %q not in SQLite schema seed. "+
					"Update schema.go too.", p)
			}
		})
	}
}

// INVARIANT: User route-guard permissions must be write permissions (not read-only).
// Creating, suspending, or deactivating users are mutation operations.
func TestUserPermission_RouteGuardsAreWritePermissions(t *testing.T) {
	writePerms := []string{
		"user.pre_provision",
		"user.suspend",
		"user.deactivate",
		"user.create",
		"user.update",
		"user.delete",
		"user.enroll",
		"user.unenroll",
		"user.assign_role",
	}

	for _, p := range writePerms {
		t.Run(p, func(t *testing.T) {
			if IsReadOnlyPermission(p) {
				t.Errorf("INVARIANT VIOLATION: %q is classified as read-only but it is a write/mutation permission", p)
			}
			// Verify it's a known permission
			found := false
			for _, ap := range AllPermissions() {
				if string(ap) == p {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("INVARIANT VIOLATION: %q is not in AllPermissions()", p)
			}
			// Verify string form matches the expected domain prefix
			if !strings.HasPrefix(p, "user.") {
				t.Errorf("permission %q should have 'user.' prefix", p)
			}
		})
	}
}
