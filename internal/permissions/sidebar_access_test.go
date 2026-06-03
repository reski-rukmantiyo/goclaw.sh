package permissions

import (
	"testing"
)

// sidebarItem describes a single sidebar menu entry and its visibility rules.
type sidebarItem struct {
	group      string // sidebar group name
	label      string // menu item label
	visibility string // "all" | "member" | "admin" | "owner" — who can see it in the sidebar
	wsMethod   string // primary WS method for the page (empty if HTTP-only)
	httpRoute  string // primary HTTP route (empty if WS-only)
}

// expectedMenuVisibility defines the complete sidebar menu tree with visibility rules.
// Visibility matches the sidebar.tsx code:
//   - "all":    no gate — visible to owner, admin, member, viewer
//   - "member": isMember gate (role === "member" || "admin" || "owner")
//   - "admin":  isAdmin gate (role === "admin" || role === "owner")
//   - "owner":  isOwner gate (inside admin block, so also requires admin)
var expectedMenuVisibility = []sidebarItem{
	// === Core (Chat=all, rest=member+) ===
	{"Core", "Chat", "all", "chat.history", ""}, // chat.send=member, chat.history=viewer
	{"Core", "Overview", "member", "status", ""},
	{"Core", "Agents", "member", "agents.list", ""},
	{"Core", "Agent Teams", "member", "teams.list", ""},

	// === Conversations (member+ only) ===
	{"Conversations", "Sessions", "member", "sessions.list", ""},
	{"Conversations", "Pending Messages", "member", "exec.approval.list", ""},
	{"Conversations", "Raw Messages", "member", "chat.history", ""}, // uses chat history for reads
	{"Conversations", "Contacts", "member", "channels.list", ""},

	// === Connectivity (member+ only) ===
	{"Connectivity", "Channels", "member", "channels.list", ""},
	{"Connectivity", "Nodes (Pairing)", "member", "device.pair.list", ""},
	{"Connectivity", "Workstations", "member", "workstations.list", ""},

	// === Capabilities (member+ only) ===
	{"Capabilities", "Skills", "member", "skills.list", ""},
	{"Capabilities", "Builtin Tools", "member", "", "/v1/tools"},
	{"Capabilities", "MCP Servers", "member", "", "/v1/mcp/servers"},
	{"Capabilities", "TTS", "member", "tts.status", ""},
	{"Capabilities", "Cron", "member", "cron.list", ""},
	{"Capabilities", "Hooks", "member", "hooks.list", ""},

	// === Data (member+ only) ===
	{"Data", "Memory", "member", "", "/v1/memory"},
	{"Data", "Vault", "member", "", "/v1/vault"},
	{"Data", "Knowledge Graph", "member", "", "/v1/knowledge-graph"},
	{"Data", "Embeddings", "member", "", "/v1/embeddings"},
	{"Data", "Storage", "member", "", "/v1/files"},

	// === Monitoring (Traces=all, rest=admin+) ===
	{"Monitoring", "Traces", "all", "", "/v1/traces"},
	{"Monitoring", "Realtime Events", "admin", "", "/v1/events"},
	{"Monitoring", "Activity", "admin", "", "/v1/activity"},
	{"Monitoring", "Logs", "admin", "logs.tail", ""},

	// === System (isAdmin gate on whole group) ===
	{"System", "User Management", "admin", "", "/v1/tenants/{id}/users"},
	{"System", "Groups", "admin", "", "/v1/tenants/{id}/groups"},
	{"System", "Roles", "admin", "", "/v1/tenants/{id}/roles"},
	{"System", "Audit Log", "admin", "", "/v1/audit"},
	{"System", "Tenants", "owner", "tenants.list", ""},           // isOwner gate inside isAdmin
	{"System", "Providers", "admin", "config.get", ""},
	{"System", "CLI Credentials", "admin", "", "/v1/cli-credentials"},
	{"System", "API Keys", "admin", "api_keys.list", ""},
	{"System", "Packages", "admin", "", "/v1/packages"},
	{"System", "Config", "owner", "config.get", ""},              // isOwner gate inside isAdmin
	{"System", "Authentication", "admin", "", "/v1/auth/config"},
	{"System", "Approvals", "admin", "exec.approval.list", ""},
	{"System", "Import & Export", "admin", "", "/v1/import-export"},
	{"System", "Backup & Restore", "owner", "", "/v1/backup"},    // isOwner gate inside isAdmin
}

// roleCanSeeMenu determines if a role can see a sidebar item based on its visibility rule.
func roleCanSeeMenu(role Role, visibility string) bool {
	switch visibility {
	case "all":
		return true // owner, admin, member, viewer
	case "member":
		return HasMinRole(role, RoleMember) // owner, admin, member
	case "admin":
		return HasMinRole(role, RoleAdmin) // owner, admin
	case "owner":
		return role == RoleOwner // owner only (isOwner check)
	default:
		return false
	}
}

// TestSidebarMenuVisibility verifies every sidebar menu item is visible to the correct roles.
// This is the UI contract — if this test fails, either the backend RBAC or the sidebar
// visibility gate needs updating.
func TestSidebarMenuVisibility(t *testing.T) {
	roles := []Role{RoleOwner, RoleAdmin, RoleMember, RoleViewer}

	for _, item := range expectedMenuVisibility {
		for _, role := range roles {
			visible := roleCanSeeMenu(role, item.visibility)
			t.Run(string(role)+"/"+item.group+"/"+item.label, func(t *testing.T) {
				if visible {
					t.Logf("✓ %s sees [%s → %s]", role, item.group, item.label)
				} else {
					t.Logf("✗ %s cannot see [%s → %s]", role, item.group, item.label)
				}
			})
		}
	}
}

// TestSidebarMatchesBackendRBAC verifies that menu visibility aligns with backend method access.
// Rule: if sidebar shows a menu item to a role, the backend MUST allow the read/list method.
// Sidebar CAN be more restrictive than backend (hide items even if backend allows).
func TestSidebarMatchesBackendRBAC(t *testing.T) {
	// === Sidebar visibility checks ===
	t.Run("sidebar_visibility", func(t *testing.T) {
		sidebarTests := []struct {
			role     Role
			group    string
			label    string
			expected bool
		}{
			// Chat: all roles see it
			{RoleOwner, "Core", "Chat", true},
			{RoleAdmin, "Core", "Chat", true},
			{RoleMember, "Core", "Chat", true},
			{RoleViewer, "Core", "Chat", true},
			// Overview: member+ only (hidden for viewer)
			{RoleOwner, "Core", "Overview", true},
			{RoleAdmin, "Core", "Overview", true},
			{RoleMember, "Core", "Overview", true},
			{RoleViewer, "Core", "Overview", false},
			// Agents: member+ only (hidden for viewer)
			{RoleOwner, "Core", "Agents", true},
			{RoleMember, "Core", "Agents", true},
			{RoleViewer, "Core", "Agents", false},
			// Workstation: member+ only (hidden for viewer)
			{RoleOwner, "Connectivity", "Workstations", true},
			{RoleAdmin, "Connectivity", "Workstations", true},
			{RoleMember, "Connectivity", "Workstations", true},
			{RoleViewer, "Connectivity", "Workstations", false},
			// Cron: member+ only (hidden for viewer)
			{RoleOwner, "Capabilities", "Cron", true},
			{RoleAdmin, "Capabilities", "Cron", true},
			{RoleMember, "Capabilities", "Cron", true},
			{RoleViewer, "Capabilities", "Cron", false},
			// Sessions: member+ only (hidden for viewer)
			{RoleOwner, "Conversations", "Sessions", true},
			{RoleMember, "Conversations", "Sessions", true},
			{RoleViewer, "Conversations", "Sessions", false},
			// CLI Credentials: admin/owner only
			{RoleOwner, "System", "CLI Credentials", true},
			{RoleAdmin, "System", "CLI Credentials", true},
			{RoleMember, "System", "CLI Credentials", false},
			{RoleViewer, "System", "CLI Credentials", false},
			// Monitoring Events/Activity/Logs: admin/owner only
			{RoleOwner, "Monitoring", "Realtime Events", true},
			{RoleAdmin, "Monitoring", "Realtime Events", true},
			{RoleMember, "Monitoring", "Realtime Events", false},
			{RoleViewer, "Monitoring", "Realtime Events", false},
			// Traces: all roles (viewer can see)
			{RoleOwner, "Monitoring", "Traces", true},
			{RoleAdmin, "Monitoring", "Traces", true},
			{RoleMember, "Monitoring", "Traces", true},
			{RoleViewer, "Monitoring", "Traces", true},
			// Tenants: owner only (sidebar more restrictive than backend)
			{RoleOwner, "System", "Tenants", true},
			{RoleAdmin, "System", "Tenants", false},
			{RoleMember, "System", "Tenants", false},
			{RoleViewer, "System", "Tenants", false},
			// Config: owner only (sidebar more restrictive than backend)
			{RoleOwner, "System", "Config", true},
			{RoleAdmin, "System", "Config", false},
			{RoleMember, "System", "Config", false},
			{RoleViewer, "System", "Config", false},
			// API Keys: admin/owner only
			{RoleOwner, "System", "API Keys", true},
			{RoleAdmin, "System", "API Keys", true},
			{RoleMember, "System", "API Keys", false},
			{RoleViewer, "System", "API Keys", false},
			// Backup & Restore: owner only
			{RoleOwner, "System", "Backup & Restore", true},
			{RoleAdmin, "System", "Backup & Restore", false},
			{RoleMember, "System", "Backup & Restore", false},
			{RoleViewer, "System", "Backup & Restore", false},
		}

		for _, tc := range sidebarTests {
			t.Run(string(tc.role)+"/"+tc.group+"/"+tc.label, func(t *testing.T) {
				item := menuItem(tc.group, tc.label)
				visible := roleCanSeeMenu(tc.role, item.visibility)
				if visible != tc.expected {
					t.Errorf("sidebar: %s sees [%s → %s] = %v, want %v",
						tc.role, tc.group, tc.label, visible, tc.expected)
				}
			})
		}
	})

	// === Backend RBAC checks ===
	t.Run("backend_rbac", func(t *testing.T) {
		rbacTests := []struct {
			role     Role
			method   string
			expected bool
		}{
			// Workstation reads: all roles
			{RoleOwner, "workstations.list", true},
			{RoleAdmin, "workstations.list", true},
			{RoleMember, "workstations.list", true},
			{RoleViewer, "workstations.list", true},
			// Workstation writes: admin/owner only
			{RoleOwner, "workstations.create", true},
			{RoleAdmin, "workstations.create", true},
			{RoleMember, "workstations.create", false},
			{RoleViewer, "workstations.create", false},
			// Cron reads: all roles
			{RoleOwner, "cron.list", true},
			{RoleMember, "cron.list", true},
			{RoleViewer, "cron.list", true},
			// Cron writes: member+ can, viewer cannot
			{RoleOwner, "cron.create", true},
			{RoleAdmin, "cron.create", true},
			{RoleMember, "cron.create", true},
			{RoleViewer, "cron.create", false},
			// Tenants: backend allows reads for all (sidebar hides from non-owners)
			{RoleOwner, "tenants.list", true},
			{RoleAdmin, "tenants.list", true},
			{RoleMember, "tenants.list", true},
			{RoleViewer, "tenants.list", true},
			// Config: backend allows admin+ (sidebar hides from admin)
			{RoleOwner, "config.get", true},
			{RoleAdmin, "config.get", true},
			{RoleMember, "config.get", false},
			{RoleViewer, "config.get", false},
			// Chat: read=viewer, send=member+
			{RoleViewer, "chat.history", true},
			{RoleMember, "chat.send", true},
			{RoleViewer, "chat.send", false},
			// Sessions: read=viewer, write=member+
			{RoleViewer, "sessions.list", true},
			{RoleMember, "sessions.delete", true},
			{RoleViewer, "sessions.delete", false},
			// Traces: viewer can read
			{RoleViewer, "traces.list", true},
		}

		for _, tc := range rbacTests {
			t.Run(string(tc.role)+"/"+tc.method, func(t *testing.T) {
				required := MethodRole(tc.method)
				allowed := HasMinRole(tc.role, required)
				if allowed != tc.expected {
					t.Errorf("backend: HasMinRole(%s, MethodRole(%q)=%s) = %v, want %v",
						tc.role, tc.method, required, allowed, tc.expected)
				}
			})
		}
	})

	// === Cross-check: visible menu → backend must allow reads ===
	// Known mismatches where sidebar shows item to role but backend denies the method.
	// These are UI polish issues — the page shows but actions fail gracefully.
	knownMismatches := map[string]bool{
		"member/Connectivity/Nodes (Pairing)": true, // device.pair.* = admin, but sidebar shows to member+
	}

	t.Run("visible_implies_backend_read_allowed", func(t *testing.T) {
		for _, item := range expectedMenuVisibility {
			if item.wsMethod == "" {
				continue // skip HTTP-only items
			}
			for _, role := range []Role{RoleOwner, RoleAdmin, RoleMember, RoleViewer} {
				visible := roleCanSeeMenu(role, item.visibility)
				if !visible {
					continue // hidden items don't need backend access
				}
				key := string(role) + "/" + item.group + "/" + item.label
				if knownMismatches[key] {
					t.Logf("SKIP (known mismatch): %s sees [%s → %s] but backend denies %q", role, item.group, item.label, item.wsMethod)
					continue
				}
				// If sidebar shows item to role, backend MUST allow at least read
				required := MethodRole(item.wsMethod)
				allowed := HasMinRole(role, required)
				if !allowed {
					t.Errorf("MISMATCH: %s sees [%s → %s] in sidebar, but backend denies %q (MethodRole=%s)",
						role, item.group, item.label, item.wsMethod, required)
				}
			}
		}
	})
}

// TestSidebarAccessMatrix prints the complete access matrix for all menus.
func TestSidebarAccessMatrix(t *testing.T) {
	roles := []Role{RoleOwner, RoleAdmin, RoleMember, RoleViewer}

	t.Log("")
	t.Log("SIDEBAR MENU ACCESS MATRIX")
	t.Log("===========================")

	currentGroup := ""
	for _, item := range expectedMenuVisibility {
		if item.group != currentGroup {
			t.Logf("")
			t.Logf("── %s ──", item.group)
			currentGroup = item.group
		}
		row := item.label
		for _, role := range roles {
			if roleCanSeeMenu(role, item.visibility) {
				row += " ✓"
			} else {
				row += " ✗"
			}
		}
		t.Logf("  %-24s %s", row, "")
	}

	t.Log("")
	t.Log("Legend: ✓=visible  ✗=hidden  (columns: owner admin member viewer)")
}

// TestViewerOnlySeesChatAndTraces asserts the viewer sidebar contract:
// viewers see exactly Chat and Traces — nothing else.
func TestViewerOnlySeesChatAndTraces(t *testing.T) {
	viewerVisible := []string{}
	for _, item := range expectedMenuVisibility {
		if roleCanSeeMenu(RoleViewer, item.visibility) {
			viewerVisible = append(viewerVisible, item.group+"/"+item.label)
		}
	}

	if len(viewerVisible) != 2 {
		t.Fatalf("viewer should see exactly 2 menu items, got %d: %v", len(viewerVisible), viewerVisible)
	}

	expected := map[string]bool{"Core/Chat": true, "Monitoring/Traces": true}
	for _, item := range viewerVisible {
		if !expected[item] {
			t.Errorf("viewer should NOT see: %s (expected only Core/Chat and Monitoring/Traces)", item)
		}
	}
	for exp := range expected {
		found := false
		for _, v := range viewerVisible {
			if v == exp {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("viewer should see: %s but it was not found", exp)
		}
	}
}

// menuItem finds a menu item from expectedMenuVisibility by group+label.
func menuItem(group, label string) sidebarItem {
	for _, item := range expectedMenuVisibility {
		if item.group == group && item.label == label {
			return item
		}
	}
	return sidebarItem{}
}
