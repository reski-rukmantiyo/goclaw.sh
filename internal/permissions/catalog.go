package permissions

import "strings"

// Permission represents a granular permission string in the role system.
type Permission string

// User management permissions.
const (
	PermUserList       Permission = "user.list"
	PermUserGet        Permission = "user.get"
	PermUserCreate     Permission = "user.create"
	PermUserUpdate     Permission = "user.update"
	PermUserDelete     Permission = "user.delete"
	PermUserEnroll     Permission = "user.enroll"
	PermUserUnenroll   Permission = "user.unenroll"
	PermUserAssignRole Permission = "user.assign_role"
)

// Group management permissions.
const (
	PermGroupList         Permission = "group.list"
	PermGroupGet          Permission = "group.get"
	PermGroupCreate       Permission = "group.create"
	PermGroupUpdate       Permission = "group.update"
	PermGroupDelete       Permission = "group.delete"
	PermGroupManageMembers Permission = "group.manage_members"
	PermGroupAssignRole   Permission = "group.assign_role"
)

// Role management permissions.
const (
	PermRoleList   Permission = "role.list"
	PermRoleGet    Permission = "role.get"
	PermRoleCreate Permission = "role.create"
	PermRoleUpdate Permission = "role.update"
	PermRoleDelete Permission = "role.delete"
)

// Audit permissions.
const (
	PermAuditViewAll  Permission = "audit.view_all"
	PermAuditViewGroup Permission = "audit.view_group"
	PermAuditExport   Permission = "audit.export"
)

// System permissions.
const (
	PermSystemManageSettings Permission = "system.manage_settings"
	PermSystemManageAuth     Permission = "system.manage_auth"
	PermSystemViewHealth     Permission = "system.view_health"
)

// IsReadOnlyPermission returns true if a permission string represents a read-only action.
// Read-only patterns: *.list, *.get, and *.view_* (e.g., artifact.view_group, audit.view_all).
// All other permissions are considered write actions for role derivation purposes.
func IsReadOnlyPermission(p string) bool {
	return strings.HasSuffix(p, ".list") ||
		strings.HasSuffix(p, ".get") ||
		strings.Contains(p, ".view_")
}

// AllPermissions returns the full catalog of known permissions.
func AllPermissions() []Permission {
	return []Permission{
		PermUserList, PermUserGet, PermUserCreate, PermUserUpdate, PermUserDelete, PermUserEnroll, PermUserUnenroll, PermUserAssignRole,
		PermGroupList, PermGroupGet, PermGroupCreate, PermGroupUpdate, PermGroupDelete, PermGroupManageMembers, PermGroupAssignRole,
		PermRoleList, PermRoleGet, PermRoleCreate, PermRoleUpdate, PermRoleDelete,
		PermAuditViewAll, PermAuditViewGroup, PermAuditExport,
		PermSystemManageSettings, PermSystemManageAuth, PermSystemViewHealth,
	}
}
