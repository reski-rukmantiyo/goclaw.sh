package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// stubRoleStore embeds the RoleStore interface (nil) so only the methods this
// test exercises need to be overridden. Any other call would panic — acceptable
// for a focused guard test, and a signal if the handler grows new dependencies.
type stubRoleStore struct {
	store.RoleStore
	role   *store.RoleData
	err    error
	setErr error
}

func (s stubRoleStore) GetRole(_ context.Context, id uuid.UUID) (*store.RoleData, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.role != nil {
		s.role.ID = id
	}
	return s.role, nil
}

func (s stubRoleStore) SetRolePermissions(_ context.Context, _ uuid.UUID, _ []string) error {
	return s.setErr
}

// PUT /v1/roles/{id}/permissions on a system role must be rejected with 403
// (FR-04: system roles are fully read-only). This is the new guard in
// handleSetPermissions — it mirrors the existing rename/delete guards.
func TestHandleSetPermissions_SystemRoleRejected(t *testing.T) {
	h := &RolesHandler{roles: stubRoleStore{role: &store.RoleData{Name: "Admin", IsSystem: true}}}

	id := uuid.New()
	req := httptest.NewRequest(http.MethodPut, "/v1/roles/"+id.String()+"/permissions",
		strings.NewReader(`{"permissions":["user.list"]}`))
	req.SetPathValue("id", id.String())
	rec := httptest.NewRecorder()

	h.handleSetPermissions(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for system role, got %d: %s", rec.Code, rec.Body.String())
	}
}

// A custom (non-system) role must still pass the guard and reach the store.
func TestHandleSetPermissions_CustomRoleAllowed(t *testing.T) {
	h := &RolesHandler{roles: stubRoleStore{role: &store.RoleData{Name: "Editor", IsSystem: false}}}

	id := uuid.New()
	req := httptest.NewRequest(http.MethodPut, "/v1/roles/"+id.String()+"/permissions",
		strings.NewReader(`{"permissions":["user.list"]}`))
	req.SetPathValue("id", id.String())
	rec := httptest.NewRecorder()

	h.handleSetPermissions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for custom role, got %d: %s", rec.Code, rec.Body.String())
	}
}
