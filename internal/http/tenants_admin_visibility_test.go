package http

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// 006 supports: an admin caller must now see peer admins in the tenant user list,
// while the tenant Owner stays hidden. These stubs feed resolveTargetRole so
// enrichTenantUsers can be exercised without a DB.

type visTenantStore struct {
	store.TenantStore
	owners map[string]bool // userID -> is_owner
}

func (s visTenantStore) GetTenantUserByUser(_ context.Context, _ uuid.UUID, userID string) (*store.TenantUserData, error) {
	return &store.TenantUserData{UserID: userID, IsOwner: s.owners[userID]}, nil
}

type visRoleStore struct {
	store.RoleStore
	userAdmin map[string]bool    // userID -> carries manage_settings (admin)
	roleAdmin map[uuid.UUID]bool // roleID -> admin perms (mutable across calls)
}

func (s *visRoleStore) ListUserRoles(_ context.Context, _ uuid.UUID, userID string) ([]store.RoleData, error) {
	rid := uuid.New()
	if s.roleAdmin == nil {
		s.roleAdmin = map[uuid.UUID]bool{}
	}
	s.roleAdmin[rid] = s.userAdmin[userID]
	return []store.RoleData{{ID: rid}}, nil
}

func (s *visRoleStore) GetRolePermissions(_ context.Context, roleID uuid.UUID) ([]string, error) {
	if s.roleAdmin[roleID] {
		return []string{string(permissions.PermSystemManageSettings)}, nil // → admin
	}
	return []string{"user.create"}, nil // write perm, not read-only → member
}

// 006 FR-01: admin caller sees peer admins + members/viewers; owner hidden.
func TestEnrichTenantUsers_AdminCallerSeesPeerAdmins_OwnerHidden(t *testing.T) {
	tenantID := uuid.New()
	const ownerU, adminU, memberU = "owner-1", "admin-1", "member-1"

	h := &TenantsHandler{
		tenantStore: visTenantStore{owners: map[string]bool{ownerU: true}},
		roleStore:   &visRoleStore{userAdmin: map[string]bool{adminU: true}},
		// userStore nil → enrichment maps stay empty, filter logic still runs
	}

	users := []store.TenantUserData{
		{UserID: ownerU, TenantID: tenantID},
		{UserID: adminU, TenantID: tenantID},
		{UserID: memberU, TenantID: tenantID},
	}

	got := h.enrichTenantUsers(context.Background(), users, true) // isAdminCaller

	seen := map[string]bool{}
	for _, row := range got {
		seen[row["user_id"].(string)] = true
	}
	if seen[ownerU] {
		t.Errorf("owner must be hidden from admin caller, but was present")
	}
	if !seen[adminU] {
		t.Errorf("peer admin must be visible to admin caller (006), but was hidden")
	}
	if !seen[memberU] {
		t.Errorf("member must be visible to admin caller, but was hidden")
	}
	if len(got) != 2 {
		t.Errorf("expected 2 rows (admin+member), got %d", len(got))
	}
}

// 006 FR-01 regression: an owner caller (isAdminCaller=false) sees everyone.
func TestEnrichTenantUsers_OwnerCallerSeesAll(t *testing.T) {
	tenantID := uuid.New()
	const ownerU, adminU, memberU = "owner-1", "admin-1", "member-1"

	h := &TenantsHandler{
		tenantStore: visTenantStore{owners: map[string]bool{ownerU: true}},
		roleStore:   &visRoleStore{userAdmin: map[string]bool{adminU: true}},
	}

	users := []store.TenantUserData{
		{UserID: ownerU, TenantID: tenantID},
		{UserID: adminU, TenantID: tenantID},
		{UserID: memberU, TenantID: tenantID},
	}

	got := h.enrichTenantUsers(context.Background(), users, false) // owner caller

	if len(got) != 3 {
		t.Errorf("owner caller must see all 3 users, got %d", len(got))
	}
}
