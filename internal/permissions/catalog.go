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
	PermUserAssignRole   Permission = "user.assign_role"
	PermUserPreProvision Permission = "user.pre_provision"
	PermUserSuspend      Permission = "user.suspend"
	PermUserDeactivate   Permission = "user.deactivate"
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

// Artifact and agent permissions (used in Member/Viewer seed roles).
const (
	PermArtifactUploadPersonal Permission = "artifact.upload_personal"
	PermArtifactSubmitReview   Permission = "artifact.submit_review"
	PermArtifactViewGroup      Permission = "artifact.view_group"
	PermArtifactViewTenant     Permission = "artifact.view_tenant"
	PermArtifactDeleteOwn      Permission = "artifact.delete_own"
	PermAgentCreatePersonal    Permission = "agent.create_personal"
	PermGroupViewHierarchy     Permission = "group.view_hierarchy"
)

// AdminSeedPermissions is the canonical set of permissions assigned to the Admin
// system role when a new tenant is created. Both the HTTP and WS seedSystemRoles
// functions must use this slice — do NOT inline the list.
//
// Keep in sync with: sqlitestore/schema.go Admin INSERT, migrations/000083+000085.
var AdminSeedPermissions = []string{
	"user.list", "user.get", "user.create", "user.update", "user.delete",
	"user.enroll", "user.unenroll", "user.assign_role",
	"user.pre_provision", "user.suspend", "user.deactivate",
	"group.list", "group.get", "group.create", "group.update", "group.delete",
	"group.manage_members", "group.assign_role",
	"role.list", "role.get", "role.create", "role.update", "role.delete",
	"audit.view_all", "system.manage_settings", "system.manage_auth", "system.view_health",
}

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
		PermUserList, PermUserGet, PermUserCreate, PermUserUpdate, PermUserDelete, PermUserEnroll, PermUserUnenroll, PermUserAssignRole, PermUserPreProvision, PermUserSuspend, PermUserDeactivate,
		PermGroupList, PermGroupGet, PermGroupCreate, PermGroupUpdate, PermGroupDelete, PermGroupManageMembers, PermGroupAssignRole,
		PermRoleList, PermRoleGet, PermRoleCreate, PermRoleUpdate, PermRoleDelete,
		PermAuditViewAll, PermAuditViewGroup, PermAuditExport,
		PermSystemManageSettings, PermSystemManageAuth, PermSystemViewHealth,
		PermArtifactUploadPersonal, PermArtifactSubmitReview, PermArtifactViewGroup, PermArtifactViewTenant, PermArtifactDeleteOwn,
		PermAgentCreatePersonal, PermGroupViewHierarchy,
	}
}
