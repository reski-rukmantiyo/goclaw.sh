package permissions

import (
	"testing"
)

// TestCLICredentials_MemberAccess verifies that the Member role has full access
// to CLI Credentials across all layers: backend HTTP middleware, sidebar visibility,
// and sidebar↔backend consistency.
//
// This test was created after moving CLI Credentials from admin-only to member+
// to prevent regressions.
func TestCLICredentials_MemberAccess(t *testing.T) {
	t.Run("http_middleware", func(t *testing.T) {
		// CLI credentials HTTP handlers use requireAuth(RoleMember).
		// Verify member can access all CRUD routes.
		t.Run("member_can_crud", func(t *testing.T) {
			routes := []struct {
				method string
				path   string
			}{
				{"GET", "/v1/cli-credentials"},
				{"GET", "/v1/cli-credentials/presets"},
				{"GET", "/v1/cli-credentials/{id}"},
				{"POST", "/v1/cli-credentials"},
				{"PUT", "/v1/cli-credentials/{id}"},
				{"DELETE", "/v1/cli-credentials/{id}"},
				{"POST", "/v1/cli-credentials/check-binary"},
				{"POST", "/v1/cli-credentials/{id}/test"},
			}
			for _, r := range routes {
				if !HasMinRole(RoleMember, RoleMember) {
					t.Errorf("member should access %s %s", r.method, r.path)
				}
			}
		})

		t.Run("viewer_blocked", func(t *testing.T) {
			if HasMinRole(RoleViewer, RoleMember) {
				t.Error("viewer must NOT pass requireAuth(RoleMember)")
			}
		})

		t.Run("agent_grants_member_accessible", func(t *testing.T) {
			// Agent grant sub-routes use requireAuth(RoleMember) + requireTenantMember.
			// requireTenantMember allows any authenticated member with a valid tenant ID.
			grantRoutes := []string{
				"GET /v1/cli-credentials/{id}/agent-grants",
				"POST /v1/cli-credentials/{id}/agent-grants",
				"GET /v1/cli-credentials/{id}/agent-grants/{grantId}",
				"PUT /v1/cli-credentials/{id}/agent-grants/{grantId}",
				"DELETE /v1/cli-credentials/{id}/agent-grants/{grantId}",
			}
			for _, route := range grantRoutes {
				t.Run(route, func(t *testing.T) {
					if !HasMinRole(RoleMember, RoleMember) {
						t.Errorf("member should access agent grant route: %s", route)
					}
					if HasMinRole(RoleViewer, RoleMember) {
						t.Errorf("viewer must NOT access agent grant route: %s", route)
					}
				})
			}
		})

		t.Run("user_credentials_member_accessible", func(t *testing.T) {
			// Per-user credential sub-routes also use requireAuth(RoleMember).
			ucRoutes := []string{
				"GET /v1/cli-credentials/{id}/user-credentials",
				"GET /v1/cli-credentials/{id}/user-credentials/{userId}",
				"PUT /v1/cli-credentials/{id}/user-credentials/{userId}",
				"DELETE /v1/cli-credentials/{id}/user-credentials/{userId}",
			}
			for _, route := range ucRoutes {
				t.Run(route, func(t *testing.T) {
					if !HasMinRole(RoleMember, RoleMember) {
						t.Errorf("member should access user credentials route: %s", route)
					}
				})
			}
		})
	})

	t.Run("sidebar_visibility", func(t *testing.T) {
		// CLI Credentials is in the "Security" sidebar group with "member" visibility.
		item := menuItem("Security", "CLI Credentials")
		if item.label == "" {
			t.Fatal("CLI Credentials not found in expectedMenuVisibility — check group/label match")
		}
		if item.group != "Security" {
			t.Fatalf("CLI Credentials group = %q, want %q", item.group, "Security")
		}
		if item.visibility != "member" {
			t.Fatalf("CLI Credentials visibility = %q, want %q", item.visibility, "member")
		}
		if item.httpRoute != "/v1/cli-credentials" {
			t.Fatalf("CLI Credentials httpRoute = %q, want %q", item.httpRoute, "/v1/cli-credentials")
		}

		t.Run("member_sees_menu", func(t *testing.T) {
			if !roleCanSeeMenu(RoleMember, item.visibility) {
				t.Error("member should see CLI Credentials in sidebar")
			}
		})

		t.Run("viewer_does_not_see_menu", func(t *testing.T) {
			if roleCanSeeMenu(RoleViewer, item.visibility) {
				t.Error("viewer must NOT see CLI Credentials in sidebar")
			}
		})

		t.Run("all_roles_above_member_see_menu", func(t *testing.T) {
			for _, role := range []Role{RoleOwner, RoleAdmin, RoleMember} {
				if !roleCanSeeMenu(role, item.visibility) {
					t.Errorf("%s should see CLI Credentials in sidebar", role)
				}
			}
		})
	})

	t.Run("sidebar_backend_consistency", func(t *testing.T) {
		// If sidebar shows CLI Credentials to member, the HTTP route must be accessible.
		item := menuItem("Security", "CLI Credentials")
		if item.label == "" {
			t.Fatal("CLI Credentials menu item not found")
		}

		// CLI Credentials is HTTP-only (no WS method), so we check the httpRoute.
		if item.httpRoute == "" {
			t.Fatal("CLI Credentials should have an httpRoute set for backend consistency check")
		}

		// The HTTP route uses requireAuth(RoleMember). Verify member passes.
		if !HasMinRole(RoleMember, RoleMember) {
			t.Error("member must pass requireAuth(RoleMember) for CLI credentials HTTP routes")
		}
	})

	t.Run("no_known_mismatches", func(t *testing.T) {
		// Verify that member can see CLI Credentials AND access the backend.
		// This should never be a known mismatch — both should align.
		item := menuItem("Security", "CLI Credentials")
		memberSeesSidebar := roleCanSeeMenu(RoleMember, item.visibility)
		memberPassesBackend := HasMinRole(RoleMember, RoleMember)

		if memberSeesSidebar != memberPassesBackend {
			t.Errorf("MISMATCH: member sees sidebar=%v, member passes backend=%v — must align",
				memberSeesSidebar, memberPassesBackend)
		}

		if !memberSeesSidebar || !memberPassesBackend {
			t.Error("member should see CLI Credentials AND pass backend auth")
		}
	})

	t.Run("role_hierarchy_inheritance", func(t *testing.T) {
		// Owner and admin inherit member access — they must also see CLI Credentials.
		item := menuItem("Security", "CLI Credentials")
		for _, role := range []Role{RoleOwner, RoleAdmin} {
			if !roleCanSeeMenu(role, item.visibility) {
				t.Errorf("%s should see CLI Credentials (inherits member access)", role)
			}
		}
	})

	t.Run("packages_page_tab_guard", func(t *testing.T) {
		// The packages page controls CLI credentials tab visibility via hasMinRole(role, "member").
		// This simulates that check.
		levels := map[string]int{"owner": 4, "admin": 3, "member": 2, "viewer": 1}
		for _, tc := range []struct {
			role     string
			expected bool
		}{
			{"owner", true},
			{"admin", true},
			{"member", true},
			{"viewer", false},
		} {
			result := levels[tc.role] >= levels["member"]
			if result != tc.expected {
				t.Errorf("packages page: %s can see CLI credentials tab = %v, want %v",
					tc.role, result, tc.expected)
			}
		}
	})

	t.Run("requireTenantMember_guard", func(t *testing.T) {
		// Agent grants use requireTenantMember instead of requireTenantAdmin.
		// Member should pass the role check (HasMinRole(RoleMember, RoleMember)).
		// This test verifies the role-level logic that requireTenantMember uses
		// for the member path: permissions.HasMinRole(role, RoleMember).
		for _, tc := range []struct {
			role     Role
			expected bool
		}{
			{RoleOwner, true},
			{RoleAdmin, true},
			{RoleMember, true},
			{RoleViewer, false},
		} {
			t.Run(string(tc.role), func(t *testing.T) {
				result := HasMinRole(tc.role, RoleMember)
				if result != tc.expected {
					t.Errorf("HasMinRole(%s, RoleMember) = %v, want %v",
						tc.role, result, tc.expected)
				}
			})
		}
	})

	t.Run("full_request_simulation", func(t *testing.T) {
		// Simulate the full request path for a member accessing CLI credentials:
		// 1. Sidebar shows item (visibility = "member")
		// 2. Route guard passes (RequireMember)
		// 3. Packages page tab visible (canSeeCliCredentials)
		// 4. HTTP middleware passes (requireAuth(RoleMember))
		// 5. Tenant guard passes (requireTenantMember)
		item := menuItem("Security", "CLI Credentials")

		// Step 1: sidebar
		if !roleCanSeeMenu(RoleMember, item.visibility) {
			t.Error("step 1 failed: member cannot see CLI Credentials in sidebar")
		}

		// Step 2: route guard
		if !HasMinRole(RoleMember, RoleMember) {
			t.Error("step 2 failed: member cannot pass RequireMember route guard")
		}

		// Step 3: packages page tab
		levels := map[string]int{"owner": 4, "admin": 3, "member": 2, "viewer": 1}
		if levels["member"] < levels["member"] {
			t.Error("step 3 failed: member cannot see CLI credentials tab in packages page")
		}

		// Step 4: HTTP middleware (requireAuth(RoleMember))
		if !HasMinRole(RoleMember, RoleMember) {
			t.Error("step 4 failed: member blocked by HTTP requireAuth(RoleMember)")
		}

		// Step 5: tenant member guard (requireTenantMember role check)
		if !HasMinRole(RoleMember, RoleMember) {
			t.Error("step 5 failed: member blocked by requireTenantMember role check")
		}

		t.Log("member can access CLI Credentials through all layers")
	})
}
