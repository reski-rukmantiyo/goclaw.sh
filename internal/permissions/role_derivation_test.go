package permissions

import "testing"

func TestIsReadOnlyPermission(t *testing.T) {
	tests := []struct {
		perm string
		want bool
	}{
		// Read permissions
		{"group.list", true},
		{"group.get", true},
		{"user.list", true},
		{"user.get", true},
		{"artifact.view_group", true},
		{"artifact.view_tenant", true},
		{"audit.view_all", true},
		{"audit.view_group", true},
		{"system.view_health", true},
		// Write permissions
		{"user.create", false},
		{"user.update", false},
		{"user.delete", false},
		{"artifact.upload_personal", false},
		{"artifact.submit_review", false},
		{"artifact.delete_own", false},
		{"agent.create_personal", false},
		{"system.manage_settings", false},
		{"system.manage_auth", false},
	}
	for _, tt := range tests {
		got := IsReadOnlyPermission(tt.perm)
		if got != tt.want {
			t.Errorf("IsReadOnlyPermission(%q) = %v, want %v", tt.perm, got, tt.want)
		}
	}
}

func TestRoleDerivation(t *testing.T) {
	// Viewer: all perms are read-only → viewer
	viewerPerms := map[string]bool{"group.list": true, "group.get": true, "artifact.view_group": true, "artifact.view_tenant": true}
	hasWrite := false
	for p := range viewerPerms {
		if !IsReadOnlyPermission(p) { hasWrite = true; break }
	}
	if hasWrite { t.Error("viewer should have no write perms") }

	// Member: has write perms → member
	memberPerms := map[string]bool{"group.list": true, "artifact.upload_personal": true, "agent.create_personal": true}
	hasWrite = false
	for p := range memberPerms {
		if !IsReadOnlyPermission(p) { hasWrite = true; break }
	}
	if !hasWrite { t.Error("member should have write perms") }

	// Admin: has system.manage_settings → admin
	_, ok := viewerPerms[string(PermSystemManageSettings)]
	if ok { t.Error("viewer should not have manage_settings") }
}
