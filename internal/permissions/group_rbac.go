package permissions

// GroupRBAC provides role-based access control for the multi-auth module.
// It operates alongside the existing gateway PolicyEngine, adding group-level
// permission checks for user-facing API endpoints.

// UserRole represents a user's role in the multi-auth system.
type UserRole string

const (
	// UserRoleTenantAdmin has full access across all groups.
	UserRoleTenantAdmin UserRole = "tenant_admin"
	// UserRoleGroupAdmin manages members and group-scoped resources.
	UserRoleGroupAdmin UserRole = "group_admin"
	// UserRoleMember is a regular group member.
	UserRoleMember UserRole = "member"
)

// UserCanPerform checks if a user with the given role can perform an action.
// This is used by the multi-auth HTTP API layer, not the gateway WS router.
func UserCanPerform(role UserRole, action string) bool {
	switch action {
	// User management
	case "user.list", "user.get", "user.suspend", "user.deactivate", "user.pre_provision":
		return role == UserRoleTenantAdmin
	case "user.view_group":
		return role == UserRoleTenantAdmin || role == UserRoleGroupAdmin
	case "user.view_own_group":
		return role == UserRoleMember

	// Group management
	case "group.create", "group.delete":
		return role == UserRoleTenantAdmin
	case "group.update", "group.manage_members", "group.assign_admin":
		return role == UserRoleTenantAdmin || role == UserRoleGroupAdmin
	case "group.view_hierarchy":
		return role == UserRoleTenantAdmin || role == UserRoleGroupAdmin || role == UserRoleMember
	case "group.view_own":
		return role == UserRoleMember

	// Knowledge/artifact scope
	case "artifact.upload_personal":
		return role == UserRoleTenantAdmin || role == UserRoleGroupAdmin || role == UserRoleMember
	case "artifact.upload_group":
		return role == UserRoleTenantAdmin || role == UserRoleGroupAdmin
	case "artifact.submit_review":
		return role == UserRoleTenantAdmin || role == UserRoleGroupAdmin || role == UserRoleMember
	case "artifact.approve_group":
		return role == UserRoleTenantAdmin || role == UserRoleGroupAdmin
	case "artifact.approve_tenant":
		return role == UserRoleTenantAdmin
	case "artifact.view_group":
		return role == UserRoleTenantAdmin || role == UserRoleGroupAdmin || role == UserRoleMember
	case "artifact.view_tenant":
		return role == UserRoleTenantAdmin || role == UserRoleGroupAdmin || role == UserRoleMember
	case "artifact.delete_own":
		return role == UserRoleTenantAdmin || role == UserRoleGroupAdmin || role == UserRoleMember
	case "artifact.delete_group":
		return role == UserRoleTenantAdmin || role == UserRoleGroupAdmin

	// Agent configuration
	case "agent.create_personal":
		return role == UserRoleTenantAdmin || role == UserRoleGroupAdmin || role == UserRoleMember
	case "agent.publish_group":
		return role == UserRoleTenantAdmin || role == UserRoleGroupAdmin
	case "agent.publish_tenant":
		return role == UserRoleTenantAdmin

	// Audit
	case "audit.view_all":
		return role == UserRoleTenantAdmin
	case "audit.view_group":
		return role == UserRoleTenantAdmin || role == UserRoleGroupAdmin
	case "audit.export":
		return role == UserRoleTenantAdmin || role == UserRoleGroupAdmin

	// System configuration
	case "system.manage_settings", "system.manage_auth", "system.view_health":
		return role == UserRoleTenantAdmin
	}

	return false
}

// HasMinUserRole checks if a role meets the minimum required level.
func HasMinUserRole(role, required UserRole) bool {
	levels := map[UserRole]int{
		UserRoleTenantAdmin: 3,
		UserRoleGroupAdmin:  2,
		UserRoleMember:      1,
	}
	return levels[role] >= levels[required]
}
