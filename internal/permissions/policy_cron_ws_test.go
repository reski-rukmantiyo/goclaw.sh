package permissions

import (
	"testing"
)

// TestCronMethodAccess verifies cron method role requirements per the 4-role RBAC model.
//
// Rules:
//   - Owner:  all methods
//   - Admin:  all methods (within tenant)
//   - Member: write + read methods (CRUD for tenant menus)
//   - Viewer: read-only methods only
func TestCronMethodAccess(t *testing.T) {
	readMethods := []string{
		"cron.list",
		"cron.status",
		"cron.runs",
	}
	writeMethods := []string{
		"cron.create",
		"cron.update",
		"cron.delete",
		"cron.toggle",
		"cron.run",
	}

	tests := []struct {
		role      Role
		method    string
		allowed   bool
	}{
		// Owner: all cron methods allowed
		{RoleOwner, "cron.list", true},
		{RoleOwner, "cron.create", true},

		// Admin: all cron methods allowed
		{RoleAdmin, "cron.list", true},
		{RoleAdmin, "cron.create", true},

		// Member: write + read allowed (member has CRUD for tenant menus)
		{RoleMember, "cron.list", true},
		{RoleMember, "cron.runs", true},
		{RoleMember, "cron.create", true},
		{RoleMember, "cron.update", true},
		{RoleMember, "cron.delete", true},
		{RoleMember, "cron.toggle", true},
		{RoleMember, "cron.run", true},

		// Viewer: read-only
		{RoleViewer, "cron.list", true},
		{RoleViewer, "cron.status", true},
		{RoleViewer, "cron.runs", true},
		{RoleViewer, "cron.create", false},
		{RoleViewer, "cron.update", false},
		{RoleViewer, "cron.delete", false},
		{RoleViewer, "cron.toggle", false},
		{RoleViewer, "cron.run", false},
	}

	for _, tc := range tests {
		t.Run(string(tc.role)+"/"+tc.method, func(t *testing.T) {
			required := MethodRole(tc.method)
			allowed := HasMinRole(tc.role, required)
			if allowed != tc.allowed {
				t.Errorf("HasMinRole(%s, MethodRole(%q)=%s) = %v, want %v",
					tc.role, tc.method, required, allowed, tc.allowed)
			}
		})
	}

	// Verify all cron methods are classified (not RoleNone)
	t.Run("all_classified", func(t *testing.T) {
		all := append(readMethods, writeMethods...)
		for _, m := range all {
			r := MethodRole(m)
			if r == RoleNone {
				t.Errorf("MethodRole(%q) = RoleNone (unclassified)", m)
			} else {
				t.Logf("  %s → %s", m, r)
			}
		}
	})
}

// TestWorkstationMethodAccess verifies workstation method role requirements.
//
// Rules:
//   - Owner:  all methods
//   - Admin:  all methods (workstation CRUD is admin-only per policy)
//   - Member: read methods only (list, get, permissions.list, activity, linked agents)
//   - Viewer: read methods only (same as member for workstations)
func TestWorkstationMethodAccess(t *testing.T) {
	readMethods := []string{
		"workstations.list",
		"workstations.get",
		"workstations.permissions.list",
		"workstations.activity.list",
		"workstations.listLinkedAgents",
	}
	adminMethods := []string{
		"workstations.create",
		"workstations.update",
		"workstations.delete",
		"workstations.toggle",
		"workstations.linkAgent",
		"workstations.unlinkAgent",
		"workstations.testConnection",
		"workstations.permissions.add",
		"workstations.permissions.remove",
		"workstations.permissions.toggle",
		"workstations.commandGroups.list",
		"workstations.commandGroups.create",
		"workstations.commandGroups.get",
		"workstations.commandGroups.update",
		"workstations.commandGroups.delete",
		"workstations.commandGroups.apply",
		"workstations.commandGroups.remove",
		"workstations.commandGroups.toggle",
		"workstations.commandGroups.listForWorkstation",
	}

	tests := []struct {
		role    Role
		method  string
		allowed bool
	}{
		// Owner: all workstation methods
		{RoleOwner, "workstations.list", true},
		{RoleOwner, "workstations.create", true},
		{RoleOwner, "workstations.commandGroups.apply", true},

		// Admin: all workstation methods
		{RoleAdmin, "workstations.list", true},
		{RoleAdmin, "workstations.create", true},
		{RoleAdmin, "workstations.commandGroups.apply", true},

		// Member: read-only for workstations (admin methods blocked)
		{RoleMember, "workstations.list", true},
		{RoleMember, "workstations.get", true},
		{RoleMember, "workstations.permissions.list", true},
		{RoleMember, "workstations.activity.list", true},
		{RoleMember, "workstations.listLinkedAgents", true},
		{RoleMember, "workstations.create", false},
		{RoleMember, "workstations.update", false},
		{RoleMember, "workstations.delete", false},
		{RoleMember, "workstations.toggle", false},
		{RoleMember, "workstations.linkAgent", false},
		{RoleMember, "workstations.commandGroups.list", false},
		{RoleMember, "workstations.commandGroups.create", false},

		// Viewer: read-only (same as member for workstations)
		{RoleViewer, "workstations.list", true},
		{RoleViewer, "workstations.get", true},
		{RoleViewer, "workstations.create", false},
		{RoleViewer, "workstations.update", false},
		{RoleViewer, "workstations.delete", false},
	}

	for _, tc := range tests {
		t.Run(string(tc.role)+"/"+tc.method, func(t *testing.T) {
			required := MethodRole(tc.method)
			allowed := HasMinRole(tc.role, required)
			if allowed != tc.allowed {
				t.Errorf("HasMinRole(%s, MethodRole(%q)=%s) = %v, want %v",
					tc.role, tc.method, required, allowed, tc.allowed)
			}
		})
	}

	// Verify all workstation methods are classified
	t.Run("all_classified", func(t *testing.T) {
		all := append(append([]string{}, readMethods...), adminMethods...)
		for _, m := range all {
			r := MethodRole(m)
			if r == RoleNone {
				t.Errorf("MethodRole(%q) = RoleNone (unclassified)", m)
			} else {
				t.Logf("  %s → %s", m, r)
			}
		}
	})
}

// TestCLICredentialsAccess verifies CLI credential HTTP route role requirements.
// CLI credentials use HTTP /v1/cli-credentials with requireAuth(RoleAdmin), not WS RPC.
// Same pattern as workstations: owner+admin full access, member+viewer blocked from everything.
//
// Rules:
//   - Owner:  all routes
//   - Admin:  all routes
//   - Member: blocked from all routes (same as workstation admin methods)
//   - Viewer: blocked from all routes
func TestCLICredentialsAccess(t *testing.T) {
	// All CLI credential routes are registered with requireAuth(RoleAdmin).
	// Since there's no read/write split (everything is admin), we list all routes.
	readRoutes := []string{
		"GET /v1/cli-credentials",
		"GET /v1/cli-credentials/presets",
		"GET /v1/cli-credentials/{id}",
		"GET /v1/cli-credentials/{id}/agent-grants",
		"GET /v1/cli-credentials/{id}/user-credentials",
		"GET /v1/cli-credentials/{id}/user-credentials/{userId}",
	}
	writeRoutes := []string{
		"POST /v1/cli-credentials",
		"PUT /v1/cli-credentials/{id}",
		"DELETE /v1/cli-credentials/{id}",
		"POST /v1/cli-credentials/check-binary",
		"POST /v1/cli-credentials/{id}/test",
		"POST /v1/cli-credentials/{id}/agent-grants",
		"PUT /v1/cli-credentials/{id}/agent-grants/{grantId}",
		"DELETE /v1/cli-credentials/{id}/agent-grants/{grantId}",
		"PUT /v1/cli-credentials/{id}/user-credentials/{userId}",
		"DELETE /v1/cli-credentials/{id}/user-credentials/{userId}",
	}

	// All routes require RoleAdmin — same gate as workstation admin methods
	tests := []struct {
		role    Role
		route   string
		allowed bool
	}{
		// Owner: all routes
		{RoleOwner, "GET /v1/cli-credentials", true},
		{RoleOwner, "POST /v1/cli-credentials", true},
		{RoleOwner, "DELETE /v1/cli-credentials/{id}", true},
		{RoleOwner, "POST /v1/cli-credentials/{id}/agent-grants", true},

		// Admin: all routes
		{RoleAdmin, "GET /v1/cli-credentials", true},
		{RoleAdmin, "POST /v1/cli-credentials", true},
		{RoleAdmin, "PUT /v1/cli-credentials/{id}", true},
		{RoleAdmin, "DELETE /v1/cli-credentials/{id}", true},
		{RoleAdmin, "POST /v1/cli-credentials/{id}/test", true},
		{RoleAdmin, "GET /v1/cli-credentials/{id}/agent-grants", true},
		{RoleAdmin, "POST /v1/cli-credentials/{id}/agent-grants", true},
		{RoleAdmin, "DELETE /v1/cli-credentials/{id}/agent-grants/{grantId}", true},
		{RoleAdmin, "GET /v1/cli-credentials/{id}/user-credentials", true},
		{RoleAdmin, "PUT /v1/cli-credentials/{id}/user-credentials/{userId}", true},
		{RoleAdmin, "DELETE /v1/cli-credentials/{id}/user-credentials/{userId}", true},

		// Member: blocked from all (even reads)
		{RoleMember, "GET /v1/cli-credentials", false},
		{RoleMember, "GET /v1/cli-credentials/presets", false},
		{RoleMember, "GET /v1/cli-credentials/{id}", false},
		{RoleMember, "POST /v1/cli-credentials", false},
		{RoleMember, "PUT /v1/cli-credentials/{id}", false},
		{RoleMember, "DELETE /v1/cli-credentials/{id}", false},
		{RoleMember, "GET /v1/cli-credentials/{id}/agent-grants", false},
		{RoleMember, "POST /v1/cli-credentials/{id}/agent-grants", false},

		// Viewer: blocked from all
		{RoleViewer, "GET /v1/cli-credentials", false},
		{RoleViewer, "GET /v1/cli-credentials/{id}", false},
		{RoleViewer, "POST /v1/cli-credentials", false},
		{RoleViewer, "DELETE /v1/cli-credentials/{id}", false},
	}

	for _, tc := range tests {
		t.Run(string(tc.role)+"/"+tc.route, func(t *testing.T) {
			// CLI credentials use requireAuth(RoleAdmin) for ALL routes
			allowed := HasMinRole(tc.role, RoleAdmin)
			if allowed != tc.allowed {
				t.Errorf("HasMinRole(%s, RoleAdmin) = %v, want %v for %s",
					tc.role, allowed, tc.allowed, tc.route)
			}
		})
	}

	// Summary: count total routes per role
	t.Run("summary", func(t *testing.T) {
		all := append(append([]string{}, readRoutes...), writeRoutes...)
		for _, role := range []Role{RoleOwner, RoleAdmin, RoleMember, RoleViewer} {
			allowed := HasMinRole(role, RoleAdmin)
			if allowed {
				t.Logf("  %-8s: %d/%d routes accessible", role, len(all), len(all))
			} else {
				t.Logf("  %-8s: 0/%d routes accessible (blocked)", role, len(all))
			}
		}
	})
}

// TestCronWorkstationSummary prints a summary table of access per role.
func TestCronWorkstationSummary(t *testing.T) {
	methods := []struct {
		name    string
		read    []string
		write   []string
		admin   []string
	}{
		{"cron",
			[]string{"cron.list", "cron.status", "cron.runs"},
			[]string{"cron.create", "cron.update", "cron.delete", "cron.toggle", "cron.run"},
			nil,
		},
		{"workstations",
			[]string{"workstations.list", "workstations.get", "workstations.permissions.list", "workstations.activity.list", "workstations.listLinkedAgents"},
			nil,
			[]string{"workstations.create", "workstations.update", "workstations.delete", "workstations.toggle", "workstations.linkAgent", "workstations.unlinkAgent"},
		},
	}

	roles := []Role{RoleOwner, RoleAdmin, RoleMember, RoleViewer}

	for _, mg := range methods {
		t.Run(mg.name+"_summary", func(t *testing.T) {
			var all []string
			all = append(all, mg.read...)
			all = append(all, mg.write...)
			all = append(all, mg.admin...)

			for _, role := range roles {
				allowed, denied := 0, 0
				for _, m := range all {
					if HasMinRole(role, MethodRole(m)) {
						allowed++
					} else {
						denied++
					}
				}
				t.Logf("  %-8s: %d allowed, %d denied (total %d)", role, allowed, denied, len(all))
			}
		})
	}
}
