package http

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/eventbus"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/workstation"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// WorkstationsHandler handles HTTP CRUD for workstations.
// Routes are only registered when edition is Standard — callers MUST gate.
type WorkstationsHandler struct {
	wsStore       store.WorkstationStore
	linkStore     store.AgentWorkstationLinkStore
	tenantStore   store.TenantStore
	permStore     store.WorkstationPermissionStore     // Phase 6; may be nil
	activityStore store.WorkstationActivityStore       // Phase 7; may be nil
	groupStore    store.WorkstationCommandGroupStore   // Phase 8; may be nil
	groupPermStore store.WorkstationGroupPermissionStore // Phase 8; may be nil
	eventBus      eventbus.DomainEventBus              // may be nil; used for allowlist cache invalidation
}

// NewWorkstationsHandler creates a WorkstationsHandler.
func NewWorkstationsHandler(
	wsStore store.WorkstationStore,
	linkStore store.AgentWorkstationLinkStore,
	tenantStore store.TenantStore,
) *WorkstationsHandler {
	return &WorkstationsHandler{wsStore: wsStore, linkStore: linkStore, tenantStore: tenantStore}
}

// SetPermStore wires the permission store for allowlist CRUD endpoints.
func (h *WorkstationsHandler) SetPermStore(ps store.WorkstationPermissionStore) {
	h.permStore = ps
}

// SetActivityStore wires the activity store for audit log endpoints (Phase 7).
func (h *WorkstationsHandler) SetActivityStore(as store.WorkstationActivityStore) {
	h.activityStore = as
}

// SetGroupStore wires the command group store for group CRUD endpoints (Phase 8).
func (h *WorkstationsHandler) SetGroupStore(gs store.WorkstationCommandGroupStore) {
	h.groupStore = gs
}

// SetGroupPermStore wires the group permission store for applying groups to workstations (Phase 8).
func (h *WorkstationsHandler) SetGroupPermStore(gps store.WorkstationGroupPermissionStore) {
	h.groupPermStore = gps
}

// SetEventBus wires the domain event bus for allowlist cache invalidation.
func (h *WorkstationsHandler) SetEventBus(eb eventbus.DomainEventBus) {
	h.eventBus = eb
}

func (h *WorkstationsHandler) emitPermChanged(workstationID uuid.UUID) {
	if h.eventBus == nil {
		return
	}
	h.eventBus.Publish(eventbus.DomainEvent{
		ID:        uuid.New().String(),
		Type:      eventbus.EventWorkstationPermChanged,
		SourceID:  workstationID.String(),
		TenantID:  "",
		Timestamp: time.Now(),
		Payload:   map[string]any{"workstation_id": workstationID.String()},
	})
}

func (h *WorkstationsHandler) emitUpdated(workstationID uuid.UUID) {
	if h.eventBus == nil {
		return
	}
	h.eventBus.Publish(eventbus.DomainEvent{
		ID:        uuid.New().String(),
		Type:      eventbus.EventWorkstationUpdated,
		SourceID:  workstationID.String(),
		TenantID:  "",
		Timestamp: time.Now(),
		Payload:   map[string]any{"workstation_id": workstationID.String()},
	})
}

func (h *WorkstationsHandler) emitDeleted(workstationID uuid.UUID) {
	if h.eventBus == nil {
		return
	}
	h.eventBus.Publish(eventbus.DomainEvent{
		ID:        uuid.New().String(),
		Type:      eventbus.EventWorkstationDeleted,
		SourceID:  workstationID.String(),
		TenantID:  "",
		Timestamp: time.Now(),
		Payload:   map[string]any{"workstation_id": workstationID.String()},
	})
}

// RegisterRoutes registers all workstation endpoints onto mux.
// MUST only be called after edition gate check — never in Lite builds.
func (h *WorkstationsHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/workstations", h.auth(h.handleList))
	mux.HandleFunc("POST /v1/workstations", h.auth(h.handleCreate))
	mux.HandleFunc("GET /v1/workstations/{id}", h.auth(h.handleGet))
	mux.HandleFunc("PUT /v1/workstations/{id}", h.auth(h.handleUpdate))
	mux.HandleFunc("POST /v1/workstations/{id}/toggle", h.auth(h.handleToggle))
	mux.HandleFunc("DELETE /v1/workstations/{id}", h.auth(h.handleDelete))
	mux.HandleFunc("POST /v1/workstations/{id}/test", h.auth(h.handleTest))
	// Agent links
	mux.HandleFunc("GET /v1/workstations/{id}/agents", h.auth(h.handleListLinkedAgents))
	mux.HandleFunc("POST /v1/workstations/{id}/agents", h.auth(h.handleLinkAgent))
	mux.HandleFunc("DELETE /v1/workstations/{id}/agents/{agentId}", h.auth(h.handleUnlinkAgent))
	// Phase 6: permission allowlist CRUD
	mux.HandleFunc("GET /v1/workstations/{id}/permissions", h.auth(h.handlePermList))
	mux.HandleFunc("POST /v1/workstations/{id}/permissions", h.auth(h.handlePermAdd))
	mux.HandleFunc("DELETE /v1/workstations/{id}/permissions/{permId}", h.auth(h.handlePermRemove))
	mux.HandleFunc("PUT /v1/workstations/{id}/permissions/{permId}/toggle", h.auth(h.handlePermToggle))
	// Phase 7: activity audit log
	mux.HandleFunc("GET /v1/workstations/activity", h.auth(h.handleActivityListAll))
	mux.HandleFunc("GET /v1/workstations/{id}/activity", h.auth(h.handleActivityList))
	// Phase 8: command group CRUD
	// Using /v1/workstation-command-groups (not nested under /v1/workstations) to avoid
	// route conflict with /v1/workstations/{id} wildcard.
	mux.HandleFunc("GET /v1/workstation-command-groups", h.auth(h.handleCGList))
	mux.HandleFunc("POST /v1/workstation-command-groups", h.auth(h.handleCGCreate))
	mux.HandleFunc("GET /v1/workstation-command-groups/{id}", h.auth(h.handleCGGet))
	mux.HandleFunc("PUT /v1/workstation-command-groups/{id}", h.auth(h.handleCGUpdate))
	mux.HandleFunc("DELETE /v1/workstation-command-groups/{id}", h.auth(h.handleCGDelete))
	// Phase 8: group-to-workstation links
	mux.HandleFunc("GET /v1/workstations/{id}/command-groups", h.auth(h.handleCGListForWorkstation))
	mux.HandleFunc("POST /v1/workstations/{id}/command-groups/{groupId}/apply", h.auth(h.handleCGApply))
	mux.HandleFunc("DELETE /v1/workstations/{id}/command-groups/{groupId}", h.auth(h.handleCGRemove))
	mux.HandleFunc("PUT /v1/workstations/{id}/command-groups/{groupId}/toggle", h.auth(h.handleCGToggle))
}

func (h *WorkstationsHandler) auth(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth(permissions.RoleAdmin, next)
}

func (h *WorkstationsHandler) handleList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	locale := store.LocaleFromContext(ctx)
	if !requireTenantAdmin(w, r, h.tenantStore) {
		return
	}
	wss, err := h.wsStore.List(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgFailedToList, "workstations"))
		return
	}
	views := make([]*store.SanitizedWorkstation, len(wss))
	for i := range wss {
		views[i] = wss[i].SanitizedView()
	}
	writeJSON(w, http.StatusOK, map[string]any{"workstations": views})
}

func (h *WorkstationsHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	locale := store.LocaleFromContext(ctx)
	if !requireTenantAdmin(w, r, h.tenantStore) {
		return
	}
	idStr := r.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidID, "workstation"))
		return
	}
	ws, err := h.wsStore.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, protocol.ErrNotFound,
				i18n.T(locale, i18n.MsgWorkstationNotFound, idStr))
			return
		}
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgInternalError, err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"workstation": ws.SanitizedView()})
}

func (h *WorkstationsHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	locale := store.LocaleFromContext(ctx)
	if !requireTenantAdmin(w, r, h.tenantStore) {
		return
	}

	var body struct {
		WorkstationKey string                   `json:"workstationKey"`
		Name           string                   `json:"name"`
		BackendType    store.WorkstationBackend `json:"backendType"`
		Metadata       json.RawMessage          `json:"metadata"`
		DefaultCWD     string                   `json:"defaultCwd"`
		DefaultEnv     json.RawMessage          `json:"defaultEnv"`
	}
	if !bindJSON(w, r, locale, &body) {
		return
	}

	if body.WorkstationKey == "" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgRequired, "workstationKey"))
		return
	}
	if !workstation.ValidateWorkstationKey(body.WorkstationKey) {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidSlug, "workstationKey"))
		return
	}
	if !workstation.ValidateBackend(body.BackendType) {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidBackend, string(body.BackendType)))
		return
	}
	metaBytes := []byte(body.Metadata)
	if err := store.ValidateMetadata(body.BackendType, metaBytes); err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidMetadataShape, string(body.BackendType), err.Error()))
		return
	}
	envBytes := []byte(body.DefaultEnv)
	if len(envBytes) == 0 {
		envBytes = []byte("{}")
	}

	userID := store.UserIDFromContext(ctx)
	ws := &store.Workstation{
		WorkstationKey: body.WorkstationKey,
		Name:           body.Name,
		BackendType:    body.BackendType,
		Metadata:       metaBytes,
		DefaultCWD:     body.DefaultCWD,
		DefaultEnv:     envBytes,
		Active:         true,
		CreatedBy:      userID,
	}
	if err := h.wsStore.Create(ctx, ws); err != nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgFailedToCreate, "workstation", err.Error()))
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"workstation": ws.SanitizedView()})
}

func (h *WorkstationsHandler) handleUpdate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	locale := store.LocaleFromContext(ctx)
	if !requireTenantAdmin(w, r, h.tenantStore) {
		return
	}
	idStr := r.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidID, "workstation"))
		return
	}
	var updates map[string]any
	if !bindJSON(w, r, locale, &updates) {
		return
	}
	if len(updates) == 0 {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgNoUpdatesProvided))
		return
	}
	// I2 fix: validate metadata shape when metadata is being updated.
	// Fetch current workstation to obtain backend_type for validation.
	if _, hasMetadata := updates["metadata"]; hasMetadata {
		current, err := h.wsStore.GetByID(ctx, id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, http.StatusNotFound, protocol.ErrNotFound,
					i18n.T(locale, i18n.MsgWorkstationNotFound, idStr))
				return
			}
			writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
				i18n.T(locale, i18n.MsgInternalError, err.Error()))
			return
		}
		metaBytes, err := json.Marshal(updates["metadata"])
		if err != nil {
			writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
				i18n.T(locale, i18n.MsgInvalidMetadataShape, string(current.BackendType), err.Error()))
			return
		}
		if err := store.ValidateMetadata(current.BackendType, metaBytes); err != nil {
			writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
				i18n.T(locale, i18n.MsgInvalidMetadataShape, string(current.BackendType), err.Error()))
			return
		}
		// Merge metadata to preserve auth fields not explicitly changed.
		if current.BackendType == store.BackendSSH {
			if metaMap, ok := updates["metadata"].(map[string]any); ok {
				merged, err := store.MergeSSHMetadata(current.Metadata, metaMap)
				if err != nil {
					writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
						i18n.T(locale, i18n.MsgInternalError, err.Error()))
					return
				}
				updates["metadata"] = merged
			}
		}
	}
	if err := h.wsStore.Update(ctx, id, updates); err != nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgFailedToUpdate, "workstation", err.Error()))
		return
	}
	h.emitUpdated(id)
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

func (h *WorkstationsHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	locale := store.LocaleFromContext(ctx)
	if !requireTenantAdmin(w, r, h.tenantStore) {
		return
	}
	idStr := r.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidID, "workstation"))
		return
	}
	if err := h.wsStore.Delete(ctx, id); err != nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgFailedToDelete, "workstation", err.Error()))
		return
	}
	h.emitDeleted(id)
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

func (h *WorkstationsHandler) handleToggle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	locale := store.LocaleFromContext(ctx)
	if !requireTenantAdmin(w, r, h.tenantStore) {
		return
	}
	idStr := r.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidID, "workstation"))
		return
	}
	var body struct {
		Active bool `json:"active"`
	}
	if !bindJSON(w, r, locale, &body) {
		return
	}
	if err := h.wsStore.SetActive(ctx, id, body.Active); err != nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgFailedToUpdate, "workstation", err.Error()))
		return
	}
	h.emitUpdated(id)
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "active": body.Active})
}

// handleTest is a stub — real implementation in Phase 2/3.
func (h *WorkstationsHandler) handleTest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	locale := store.LocaleFromContext(ctx)
	if !requireTenantAdmin(w, r, h.tenantStore) {
		return
	}
	writeError(w, http.StatusNotImplemented, protocol.ErrNotImplemented,
		i18n.T(locale, i18n.MsgNotImplemented, "workstations.testConnection"))
}

func (h *WorkstationsHandler) handleListLinkedAgents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	locale := store.LocaleFromContext(ctx)
	if !requireTenantAdmin(w, r, h.tenantStore) {
		return
	}
	idStr := r.PathValue("id")
	wsID, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidID, "workstation"))
		return
	}
	links, err := h.linkStore.ListForWorkstation(ctx, wsID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgFailedToList, "agent links"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"links": links})
}

func (h *WorkstationsHandler) handleLinkAgent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	locale := store.LocaleFromContext(ctx)
	if !requireTenantAdmin(w, r, h.tenantStore) {
		return
	}
	idStr := r.PathValue("id")
	wsID, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidID, "workstation"))
		return
	}
	var body struct {
		AgentID   string `json:"agent_id"`
		IsDefault bool   `json:"is_default"`
	}
	if !bindJSON(w, r, locale, &body) {
		return
	}
	agentID, err := uuid.Parse(body.AgentID)
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidID, "agent"))
		return
	}
	link := &store.AgentWorkstationLink{
		AgentID:       agentID,
		WorkstationID: wsID,
		IsDefault:     body.IsDefault,
	}
	if err := h.linkStore.Link(ctx, link); err != nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgFailedToCreate, "agent_workstation_link", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"linked": true})
}

func (h *WorkstationsHandler) handleUnlinkAgent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	locale := store.LocaleFromContext(ctx)
	if !requireTenantAdmin(w, r, h.tenantStore) {
		return
	}
	wsIDStr := r.PathValue("id")
	wsID, err := uuid.Parse(wsIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidID, "workstation"))
		return
	}
	agentIDStr := r.PathValue("agentId")
	agentID, err := uuid.Parse(agentIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidID, "agent"))
		return
	}
	if err := h.linkStore.Unlink(ctx, agentID, wsID); err != nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgFailedToDelete, "agent_workstation_link", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"unlinked": true})
}

// --- Phase 6: workstation permission allowlist CRUD ---

func (h *WorkstationsHandler) requirePermStore(w http.ResponseWriter, locale string) bool {
	if h.permStore == nil {
		writeError(w, http.StatusNotImplemented, protocol.ErrNotImplemented,
			i18n.T(locale, i18n.MsgNotImplemented, "workstations permissions"))
		return false
	}
	return true
}

func (h *WorkstationsHandler) handlePermList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	locale := store.LocaleFromContext(ctx)
	if !requireTenantAdmin(w, r, h.tenantStore) || !h.requirePermStore(w, locale) {
		return
	}
	wsID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidID, "workstation"))
		return
	}
	// Ownership check: verify workstation belongs to caller's tenant before listing perms.
	// GetByID scopes the query by tenant_id — returns ErrNoRows for a different tenant.
	if _, err := h.wsStore.GetByID(ctx, wsID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, protocol.ErrNotFound,
				i18n.T(locale, i18n.MsgWorkstationNotFound, wsID.String()))
			return
		}
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgInternalError, err.Error()))
		return
	}
	perms, err := h.permStore.ListForWorkstation(ctx, wsID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgFailedToList, "permissions"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"permissions": perms})
}

func (h *WorkstationsHandler) handlePermAdd(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	locale := store.LocaleFromContext(ctx)
	if !requireTenantAdmin(w, r, h.tenantStore) || !h.requirePermStore(w, locale) {
		return
	}
	wsID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidID, "workstation"))
		return
	}
	// I5 fix: verify workstation belongs to caller's tenant before adding permission.
	// GetByID scopes the query by tenant_id in the WHERE clause — returns ErrNoRows if
	// the workstation exists in a different tenant.
	if _, err := h.wsStore.GetByID(ctx, wsID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, protocol.ErrNotFound,
				i18n.T(locale, i18n.MsgWorkstationNotFound, wsID.String()))
			return
		}
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgInternalError, err.Error()))
		return
	}
	var body struct {
		Pattern string `json:"pattern"`
	}
	if !bindJSON(w, r, locale, &body) {
		return
	}
	if body.Pattern == "" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgRequired, "pattern"))
		return
	}
	userID := store.UserIDFromContext(ctx)
	perm := &store.WorkstationPermission{
		WorkstationID: wsID,
		Pattern:       body.Pattern,
		Enabled:       true,
		CreatedBy:     userID,
	}
	if err := h.permStore.Add(ctx, perm); err != nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgFailedToCreate, "permission", err.Error()))
		return
	}
	h.emitPermChanged(wsID)
	writeJSON(w, http.StatusCreated, map[string]any{"permission": perm})
}

func (h *WorkstationsHandler) handlePermRemove(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	locale := store.LocaleFromContext(ctx)
	if !requireTenantAdmin(w, r, h.tenantStore) || !h.requirePermStore(w, locale) {
		return
	}
	permID, err := uuid.Parse(r.PathValue("permId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidID, "permission"))
		return
	}
	perm, err := h.permStore.GetByID(ctx, permID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, protocol.ErrNotFound,
				i18n.T(locale, i18n.MsgWorkstationPermNotFound, permID.String()))
			return
		}
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgFailedToDelete, "permission", err.Error()))
		return
	}
	if err := h.permStore.Remove(ctx, permID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, protocol.ErrNotFound,
				i18n.T(locale, i18n.MsgWorkstationPermNotFound, permID.String()))
			return
		}
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgFailedToDelete, "permission", err.Error()))
		return
	}
	h.emitPermChanged(perm.WorkstationID)
	writeJSON(w, http.StatusOK, map[string]any{"id": permID})
}

func (h *WorkstationsHandler) handlePermToggle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	locale := store.LocaleFromContext(ctx)
	if !requireTenantAdmin(w, r, h.tenantStore) || !h.requirePermStore(w, locale) {
		return
	}
	permID, err := uuid.Parse(r.PathValue("permId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidID, "permission"))
		return
	}
	perm, err := h.permStore.GetByID(ctx, permID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, protocol.ErrNotFound,
				i18n.T(locale, i18n.MsgWorkstationPermNotFound, permID.String()))
			return
		}
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgFailedToUpdate, "permission", err.Error()))
		return
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if !bindJSON(w, r, locale, &body) {
		return
	}
	if err := h.permStore.SetEnabled(ctx, permID, body.Enabled); err != nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgFailedToUpdate, "permission", err.Error()))
		return
	}
	h.emitPermChanged(perm.WorkstationID)
	writeJSON(w, http.StatusOK, map[string]any{"id": permID, "enabled": body.Enabled})
}

// --- Phase 7: workstation activity audit log ---

func (h *WorkstationsHandler) handleActivityList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	locale := store.LocaleFromContext(ctx)
	if !requireTenantAdmin(w, r, h.tenantStore) {
		return
	}
	if h.activityStore == nil {
		writeError(w, http.StatusNotImplemented, protocol.ErrNotImplemented,
			i18n.T(locale, i18n.MsgNotImplemented, "workstations activity"))
		return
	}
	wsID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidID, "workstation"))
		return
	}

	// Ownership check: verify the workstation belongs to the caller's tenant.
	// GetByID scopes by tenant_id — returns ErrNoRows if workstation is in a different tenant.
	if _, err := h.wsStore.GetByID(ctx, wsID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, protocol.ErrNotFound,
				i18n.T(locale, i18n.MsgWorkstationNotFound, wsID.String()))
			return
		}
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgInternalError, err.Error()))
		return
	}

	limit := 50
	if lStr := r.URL.Query().Get("limit"); lStr != "" {
		if l, err := strconv.Atoi(lStr); err == nil && l > 0 && l <= 200 {
			limit = l
		}
	}
	var cursor *uuid.UUID
	if cStr := r.URL.Query().Get("cursor"); cStr != "" {
		if cID, err := uuid.Parse(cStr); err == nil {
			cursor = &cID
		}
	}

	rows, nextCursor, err := h.activityStore.List(ctx, wsID, limit, cursor)
	if err != nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgFailedToList, "activity"))
		return
	}

	resp := map[string]any{"activity": rows}
	if nextCursor != nil {
		resp["nextCursor"] = nextCursor.String()
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *WorkstationsHandler) handleActivityListAll(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	locale := store.LocaleFromContext(ctx)
	if !requireTenantAdmin(w, r, h.tenantStore) {
		return
	}
	if h.activityStore == nil {
		writeError(w, http.StatusNotImplemented, protocol.ErrNotImplemented,
			i18n.T(locale, i18n.MsgNotImplemented, "workstations activity"))
		return
	}

	var wsID *uuid.UUID
	if wsStr := r.URL.Query().Get("workstation_id"); wsStr != "" {
		if id, err := uuid.Parse(wsStr); err == nil {
			wsID = &id
		}
	}
	var agentID *string
	if agentStr := r.URL.Query().Get("agent_id"); agentStr != "" {
		agentID = &agentStr
	}

	limit := 50
	if lStr := r.URL.Query().Get("limit"); lStr != "" {
		if l, err := strconv.Atoi(lStr); err == nil && l > 0 && l <= 200 {
			limit = l
		}
	}
	var cursor *uuid.UUID
	if cStr := r.URL.Query().Get("cursor"); cStr != "" {
		if cID, err := uuid.Parse(cStr); err == nil {
			cursor = &cID
		}
	}

	rows, nextCursor, err := h.activityStore.ListAll(ctx, wsID, agentID, limit, cursor)
	if err != nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgFailedToList, "activity"))
		return
	}

	resp := map[string]any{"activity": rows}
	if nextCursor != nil {
		resp["nextCursor"] = nextCursor.String()
	}
	writeJSON(w, http.StatusOK, resp)
}

// --- Phase 8: command group CRUD ---

func (h *WorkstationsHandler) requireGroupStore(w http.ResponseWriter, locale string) bool {
	if h.groupStore == nil {
		writeError(w, http.StatusNotImplemented, protocol.ErrNotImplemented,
			i18n.T(locale, i18n.MsgNotImplemented, "workstations command groups"))
		return false
	}
	return true
}

func (h *WorkstationsHandler) handleCGList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	locale := store.LocaleFromContext(ctx)
	if !requireTenantAdmin(w, r, h.tenantStore) || !h.requireGroupStore(w, locale) {
		return
	}
	groups, err := h.groupStore.List(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgFailedToList, "command groups"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"groups": groups})
}

func (h *WorkstationsHandler) handleCGGet(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	locale := store.LocaleFromContext(ctx)
	if !requireTenantAdmin(w, r, h.tenantStore) || !h.requireGroupStore(w, locale) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidID, "command group"))
		return
	}
	group, err := h.groupStore.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, protocol.ErrNotFound,
				i18n.T(locale, i18n.MsgNotFound, "command group"))
			return
		}
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgInternalError, err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"group": group})
}

func (h *WorkstationsHandler) handleCGCreate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	locale := store.LocaleFromContext(ctx)
	if !requireTenantAdmin(w, r, h.tenantStore) || !h.requireGroupStore(w, locale) {
		return
	}
	var body struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Patterns    []string `json:"patterns"`
	}
	if !bindJSON(w, r, locale, &body) {
		return
	}
	if body.Name == "" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgRequired, "name"))
		return
	}
	userID := store.UserIDFromContext(ctx)
	group := &store.WorkstationCommandGroup{
		Name:        body.Name,
		Description: body.Description,
		Patterns:    body.Patterns,
		CreatedBy:   userID,
	}
	if err := h.groupStore.Create(ctx, group); err != nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgFailedToCreate, "command group", err.Error()))
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"group": group})
}

func (h *WorkstationsHandler) handleCGUpdate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	locale := store.LocaleFromContext(ctx)
	if !requireTenantAdmin(w, r, h.tenantStore) || !h.requireGroupStore(w, locale) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidID, "command group"))
		return
	}
	var updates map[string]any
	if !bindJSON(w, r, locale, &updates) {
		return
	}
	if len(updates) == 0 {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgNoUpdatesProvided))
		return
	}
	if err := h.groupStore.Update(ctx, id, updates); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, protocol.ErrNotFound,
				i18n.T(locale, i18n.MsgNotFound, "command group"))
			return
		}
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgFailedToUpdate, "command group", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

func (h *WorkstationsHandler) handleCGDelete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	locale := store.LocaleFromContext(ctx)
	if !requireTenantAdmin(w, r, h.tenantStore) || !h.requireGroupStore(w, locale) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidID, "command group"))
		return
	}
	if err := h.groupStore.Delete(ctx, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, protocol.ErrNotFound,
				i18n.T(locale, i18n.MsgNotFound, "command group"))
			return
		}
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgFailedToDelete, "command group", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

func (h *WorkstationsHandler) requireGroupPermStore(w http.ResponseWriter, locale string) bool {
	if h.groupPermStore == nil {
		writeError(w, http.StatusNotImplemented, protocol.ErrNotImplemented,
			i18n.T(locale, i18n.MsgNotImplemented, "workstation group permissions"))
		return false
	}
	return true
}

func (h *WorkstationsHandler) handleCGListForWorkstation(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	locale := store.LocaleFromContext(ctx)
	if !requireTenantAdmin(w, r, h.tenantStore) || !h.requireGroupPermStore(w, locale) {
		return
	}
	wsID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidID, "workstation"))
		return
	}
	if _, err := h.wsStore.GetByID(ctx, wsID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, protocol.ErrNotFound,
				i18n.T(locale, i18n.MsgWorkstationNotFound, wsID.String()))
			return
		}
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgInternalError, err.Error()))
		return
	}
	links, err := h.groupPermStore.ListForWorkstation(ctx, wsID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgFailedToList, "group permissions"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"links": links})
}

func (h *WorkstationsHandler) handleCGApply(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	locale := store.LocaleFromContext(ctx)
	if !requireTenantAdmin(w, r, h.tenantStore) || !h.requireGroupPermStore(w, locale) || !h.requireGroupStore(w, locale) {
		return
	}
	wsID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidID, "workstation"))
		return
	}
	groupID, err := uuid.Parse(r.PathValue("groupId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidID, "command group"))
		return
	}
	if _, err := h.wsStore.GetByID(ctx, wsID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, protocol.ErrNotFound,
				i18n.T(locale, i18n.MsgWorkstationNotFound, wsID.String()))
			return
		}
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgInternalError, err.Error()))
		return
	}
	group, err := h.groupStore.GetByID(ctx, groupID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, protocol.ErrNotFound,
				i18n.T(locale, i18n.MsgNotFound, "command group"))
			return
		}
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgInternalError, err.Error()))
		return
	}
	link := &store.WorkstationGroupPermission{
		WorkstationID: wsID,
		GroupID:       groupID,
		Enabled:       true,
	}
	if err := h.groupPermStore.Add(ctx, link); err != nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgFailedToCreate, "group permission", err.Error()))
		return
	}
	h.emitPermChanged(wsID)
	writeJSON(w, http.StatusOK, map[string]any{"linked": true, "group": group})
}

func (h *WorkstationsHandler) handleCGRemove(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	locale := store.LocaleFromContext(ctx)
	if !requireTenantAdmin(w, r, h.tenantStore) || !h.requireGroupPermStore(w, locale) {
		return
	}
	wsID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidID, "workstation"))
		return
	}
	groupID, err := uuid.Parse(r.PathValue("groupId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidID, "command group"))
		return
	}
	// Find the link by workstation_id + group_id, then remove by link ID.
	links, err := h.groupPermStore.ListForWorkstation(ctx, wsID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgFailedToList, "group permissions"))
		return
	}
	var linkID uuid.UUID
	for _, l := range links {
		if l.GroupID == groupID {
			linkID = l.ID
			break
		}
	}
	if linkID == uuid.Nil {
		writeError(w, http.StatusNotFound, protocol.ErrNotFound,
			i18n.T(locale, i18n.MsgNotFound, "group permission"))
		return
	}
	if err := h.groupPermStore.Remove(ctx, linkID); err != nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgFailedToDelete, "group permission", err.Error()))
		return
	}
	h.emitPermChanged(wsID)
	writeJSON(w, http.StatusOK, map[string]any{"id": linkID})
}

func (h *WorkstationsHandler) handleCGToggle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	locale := store.LocaleFromContext(ctx)
	if !requireTenantAdmin(w, r, h.tenantStore) || !h.requireGroupPermStore(w, locale) {
		return
	}
	wsID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidID, "workstation"))
		return
	}
	groupID, err := uuid.Parse(r.PathValue("groupId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidID, "command group"))
		return
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if !bindJSON(w, r, locale, &body) {
		return
	}
	links, err := h.groupPermStore.ListForWorkstation(ctx, wsID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgFailedToList, "group permissions"))
		return
	}
	var linkID uuid.UUID
	for _, l := range links {
		if l.GroupID == groupID {
			linkID = l.ID
			break
		}
	}
	if linkID == uuid.Nil {
		writeError(w, http.StatusNotFound, protocol.ErrNotFound,
			i18n.T(locale, i18n.MsgNotFound, "group permission"))
		return
	}
	if err := h.groupPermStore.SetEnabled(ctx, linkID, body.Enabled); err != nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal,
			i18n.T(locale, i18n.MsgFailedToUpdate, "group permission", err.Error()))
		return
	}
	h.emitPermChanged(wsID)
	writeJSON(w, http.StatusOK, map[string]any{"id": linkID, "enabled": body.Enabled})
}
