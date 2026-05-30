package permissions

import "testing"

func TestUserCanPerform(t *testing.T) {
	tests := []struct {
		role   UserRole
		action string
		want   bool
	}{
		// Tenant admin can do everything
		{UserRoleTenantAdmin, "user.list", true},
		{UserRoleTenantAdmin, "group.create", true},
		{UserRoleTenantAdmin, "artifact.approve_tenant", true},
		{UserRoleTenantAdmin, "audit.view_all", true},
		{UserRoleTenantAdmin, "system.manage_settings", true},

		// Group admin can manage group but not tenant
		{UserRoleGroupAdmin, "user.list", false},
		{UserRoleGroupAdmin, "group.update", true},
		{UserRoleGroupAdmin, "group.create", false},
		{UserRoleGroupAdmin, "artifact.approve_group", true},
		{UserRoleGroupAdmin, "artifact.approve_tenant", false},
		{UserRoleGroupAdmin, "audit.view_group", true},
		{UserRoleGroupAdmin, "audit.view_all", false},

		// Member can do basic operations
		{UserRoleMember, "artifact.upload_personal", true},
		{UserRoleMember, "artifact.upload_group", false},
		{UserRoleMember, "group.create", false},
		{UserRoleMember, "user.list", false},
	}

	for _, tt := range tests {
		got := UserCanPerform(tt.role, tt.action)
		if got != tt.want {
			t.Errorf("UserCanPerform(%s, %s) = %v, want %v", tt.role, tt.action, got, tt.want)
		}
	}
}

func TestHasMinUserRole(t *testing.T) {
	tests := []struct {
		role     UserRole
		required UserRole
		want     bool
	}{
		{UserRoleTenantAdmin, UserRoleMember, true},
		{UserRoleTenantAdmin, UserRoleGroupAdmin, true},
		{UserRoleTenantAdmin, UserRoleTenantAdmin, true},
		{UserRoleGroupAdmin, UserRoleMember, true},
		{UserRoleGroupAdmin, UserRoleTenantAdmin, false},
		{UserRoleMember, UserRoleGroupAdmin, false},
		{UserRoleMember, UserRoleTenantAdmin, false},
	}

	for _, tt := range tests {
		got := HasMinUserRole(tt.role, tt.required)
		if got != tt.want {
			t.Errorf("HasMinUserRole(%s, %s) = %v, want %v", tt.role, tt.required, got, tt.want)
		}
	}
}
