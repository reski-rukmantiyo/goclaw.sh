package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// stubListRoleStore records the tenant it was asked to list and returns a canned
// result. Only ListRoles is exercised by handleListForTenant in the master-scope
// path; other interface methods are left as nil-embed panics (acceptable signal).
type stubListRoleStore struct {
	store.RoleStore
	calledTenant uuid.UUID
	roles        []store.RoleData
}

func (s *stubListRoleStore) ListRoles(_ context.Context, tenantID uuid.UUID, _ store.RoleListParams) (*store.RoleListResult, error) {
	s.calledTenant = tenantID
	return &store.RoleListResult{
		Roles:  s.roles,
		Total:  len(s.roles),
		Offset: 0,
		Limit:  50,
	}, nil
}

// stubSlugTenantStore resolves a single slug → tenant UUID.
type stubSlugTenantStore struct {
	store.TenantStore
	slugToID map[string]uuid.UUID
}

func (s stubSlugTenantStore) GetTenantBySlug(_ context.Context, slug string) (*store.TenantData, error) {
	if id, ok := s.slugToID[slug]; ok {
		return &store.TenantData{ID: id, Slug: slug}, nil
	}
	return nil, nil
}

// 005 FR-01/FR-02: a master-scope caller listing /v1/tenants/{id}/roles gets the
// viewed tenant's roles (independent of any ambient active tenant).
func TestHandleListForTenant_MasterScopeReturnsViewedTenantRoles(t *testing.T) {
	viewed := uuid.New()
	roles := []store.RoleData{
		{ID: uuid.New(), TenantID: viewed, Name: "Admin", IsSystem: true},
		{ID: uuid.New(), TenantID: viewed, Name: "Editor", IsSystem: false},
	}
	rs := &stubListRoleStore{roles: roles}
	h := &RolesHandler{roles: rs}

	req := httptest.NewRequest(http.MethodGet, "/v1/tenants/"+viewed.String()+"/roles", nil)
	req.SetPathValue("id", viewed.String())
	rec := httptest.NewRecorder()

	h.handleListForTenant(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if rs.calledTenant != viewed {
		t.Fatalf("ListRoles called with tenant %s, want %s", rs.calledTenant, viewed)
	}
	var body struct {
		Roles []store.RoleData `json:"roles"`
		Total int              `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Total != 2 || len(body.Roles) != 2 {
		t.Fatalf("expected 2 roles, got total=%d len=%d", body.Total, len(body.Roles))
	}
}

// 005 FR-02: a non-master caller passing a foreign tenant {id} gets 404, never
// another tenant's roles.
func TestHandleListForTenant_NonOwnerForeignTenantRejected(t *testing.T) {
	own := uuid.New()
	foreign := uuid.New()
	rs := &stubListRoleStore{roles: []store.RoleData{{ID: uuid.New(), TenantID: foreign, Name: "Leak"}}}
	h := &RolesHandler{roles: rs}

	req := httptest.NewRequest(http.MethodGet, "/v1/tenants/"+foreign.String()+"/roles", nil)
	req.SetPathValue("id", foreign.String())
	// Scope the caller to their own tenant (non-master).
	req = req.WithContext(store.WithTenantID(req.Context(), own))
	rec := httptest.NewRecorder()

	h.handleListForTenant(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for foreign tenant, got %d: %s", rec.Code, rec.Body.String())
	}
	if rs.calledTenant != uuid.Nil {
		t.Fatalf("ListRoles must not be called for a foreign tenant, called with %s", rs.calledTenant)
	}
}

// 005 FR-02: a non-master caller listing their own tenant succeeds.
func TestHandleListForTenant_NonOwnerOwnTenantAllowed(t *testing.T) {
	own := uuid.New()
	rs := &stubListRoleStore{roles: []store.RoleData{{ID: uuid.New(), TenantID: own, Name: "Admin", IsSystem: true}}}
	h := &RolesHandler{roles: rs}

	req := httptest.NewRequest(http.MethodGet, "/v1/tenants/"+own.String()+"/roles", nil)
	req.SetPathValue("id", own.String())
	req = req.WithContext(store.WithTenantID(req.Context(), own))
	rec := httptest.NewRecorder()

	h.handleListForTenant(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for own tenant, got %d: %s", rec.Code, rec.Body.String())
	}
	if rs.calledTenant != own {
		t.Fatalf("ListRoles called with %s, want %s", rs.calledTenant, own)
	}
}

// 005 FR-02: {id} resolves both UUID and slug forms.
func TestResolveTenantPathID_UUID(t *testing.T) {
	want := uuid.New()
	h := &RolesHandler{tenants: stubSlugTenantStore{}}
	req := httptest.NewRequest(http.MethodGet, "/v1/tenants/"+want.String()+"/roles", nil)
	req.SetPathValue("id", want.String())
	rec := httptest.NewRecorder()

	got, ok := h.resolveTenantPathID(rec, req, "en")
	if !ok {
		t.Fatalf("expected ok, got %d: %s", rec.Code, rec.Body.String())
	}
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestResolveTenantPathID_Slug(t *testing.T) {
	want := uuid.New()
	h := &RolesHandler{tenants: stubSlugTenantStore{slugToID: map[string]uuid.UUID{"tenant-17": want}}}
	req := httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant-17/roles", nil)
	req.SetPathValue("id", "tenant-17")
	rec := httptest.NewRecorder()

	got, ok := h.resolveTenantPathID(rec, req, "en")
	if !ok {
		t.Fatalf("expected ok for slug, got %d: %s", rec.Code, rec.Body.String())
	}
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestResolveTenantPathID_UnknownSlugNotFound(t *testing.T) {
	h := &RolesHandler{tenants: stubSlugTenantStore{slugToID: map[string]uuid.UUID{}}}
	req := httptest.NewRequest(http.MethodGet, "/v1/tenants/ghost/roles", nil)
	req.SetPathValue("id", "ghost")
	rec := httptest.NewRecorder()

	if _, ok := h.resolveTenantPathID(rec, req, "en"); ok {
		t.Fatalf("expected not-found for unknown slug, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}
