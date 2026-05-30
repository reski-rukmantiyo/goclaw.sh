package store

import (
	"context"
	"strings"
)

// IsSkillVisibleTo returns true if the caller identified by ctx can discover
// the given skill. Rules:
//   - System skills are visible to everyone.
//   - Empty or "public" visibility is treated as public (legacy rows default
//     to "public" for safety since older stores did not enforce the field).
//   - "private" skills are only visible to the owner. Three identity strings
//     are considered (actor, user, sender) to match the same identities
//     isOwnerOfSkill checks for backward compatibility (#915).
//
// Admin/master-scope bypass is the caller's responsibility — this helper
// reflects the non-privileged baseline.
func IsSkillVisibleTo(ctx context.Context, ownerID, visibility string, isSystem bool) bool {
	if isSystem {
		return true
	}
	// Normalize to defend against historical rows with mixed case / whitespace
	// that bypassed the write-path normalizer.
	switch strings.ToLower(strings.TrimSpace(visibility)) {
	case "", "public":
		return true
	case "private":
		if ownerID == "" {
			// No owner recorded — treat as public (historical data).
			return true
		}
		actorID := ActorIDFromContext(ctx)
		userID := UserIDFromContext(ctx)
		senderID := SenderIDFromContext(ctx)
		return ownerID == actorID || ownerID == userID || ownerID == senderID
	default:
		// Unknown enum value: fail closed (hide).
		return false
	}
}

// FilterVisibleSkills returns skills the caller can discover. Uses
// IsSkillVisibleTo for each entry.
func FilterVisibleSkills(ctx context.Context, skills []SkillInfo) []SkillInfo {
	out := make([]SkillInfo, 0, len(skills))
	for _, s := range skills {
		if IsSkillVisibleTo(ctx, s.OwnerID, s.Visibility, s.IsSystem) {
			out = append(out, s)
		}
	}
	return out
}

// Scope constants for the multi-auth module resource visibility.
const (
	ScopePersonal = "personal"
	ScopeGroup    = "group"
	ScopeTenant   = "tenant"
)

// ScopeTransition defines a valid visibility scope transition.
// Transitions require escalating authority:
// personal→group requires group_admin, group→tenant requires tenant_admin.
var scopeLevel = map[string]int{
	ScopePersonal: 0,
	ScopeGroup:    1,
	ScopeTenant:   2,
}

// CanTransitionScope returns true if the user with the given role can transition
// a resource from oldScope to newScope.
func CanTransitionScope(userRole string, oldScope, newScope string) bool {
	oldLevel := scopeLevel[oldScope]
	newLevel := scopeLevel[newScope]

	switch {
	case newLevel > oldLevel:
		// Promotion: personal→group or group→tenant
		if newLevel == scopeLevel[ScopeGroup] {
			// Need at least group_admin
			return userRole == "tenant_admin" || userRole == "group_admin"
		}
		if newLevel == scopeLevel[ScopeTenant] {
			// Need tenant_admin
			return userRole == "tenant_admin"
		}
		return false
	case newLevel < oldLevel:
		// Demotion: tenant→group or group→personal
		if oldLevel == scopeLevel[ScopeTenant] {
			return userRole == "tenant_admin"
		}
		return userRole == "tenant_admin" || userRole == "group_admin"
	default:
		// Same scope — always allowed (no-op)
		return true
	}
}

// IsResourceVisibleTo checks if a resource with the given scope and ownership
// is visible to the caller identified by ctx.
//   - personal: only creator (ownerID) or tenant_admin
//   - group: all members of the group (groupID), group admins, tenant admin
//   - tenant: all authenticated users
func IsResourceVisibleTo(ctx context.Context, scope, ownerID string, groupID string) bool {
	role := RoleFromContext(ctx)

	switch scope {
	case ScopePersonal:
		userID := UserIDFromContext(ctx)
		return ownerID == userID || role == "owner" || role == "admin"
	case ScopeGroup:
		// Visible to group members, group admins, tenant admin
		grpRole := GroupRoleFromContext(ctx)
		grpID := GroupIDFromContext(ctx)
		return role == "owner" || role == "admin" || grpRole != "" || grpID.String() == groupID
	case ScopeTenant:
		return true
	default:
		return false
	}
}
