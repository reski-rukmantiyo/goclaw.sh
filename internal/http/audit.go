package http

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// emitAudit broadcasts an audit event via msgBus for async persistence.
func emitAudit(msgBus *bus.MessageBus, r *http.Request, action, entityType, entityID string) {
	if msgBus == nil {
		return
	}
	actorID := store.UserIDFromContext(r.Context())
	if actorID == "" {
		actorID = extractUserID(r)
	}
	if actorID == "" {
		actorID = "system"
	}
	msgBus.Broadcast(bus.Event{
		Name: protocol.EventAuditLog,
		Payload: bus.AuditEventPayload{
			ActorType:  "user",
			ActorID:    actorID,
			Action:     action,
			EntityType: entityType,
			EntityID:   entityID,
			IPAddress:  r.RemoteAddr,
			TenantID:   store.TenantIDFromContext(r.Context()),
		},
	})
}

// AuditHandler handles audit log query endpoints.
type AuditHandler struct {
	audit store.AuditStore
}

// NewAuditHandler creates a handler for audit log endpoints.
func NewAuditHandler(audit store.AuditStore) *AuditHandler {
	return &AuditHandler{audit: audit}
}

// RegisterRoutes registers all audit log routes on the given mux.
func (h *AuditHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/audit", requireAuth(permissions.RoleAdmin, h.handleList))
}

// handleList returns a paginated list of audit log entries.
func (h *AuditHandler) handleList(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	ctx := r.Context()
	tenantID := store.TenantIDFromContext(ctx)

	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 {
		limit = 50
	}
	offset, _ := strconv.Atoi(q.Get("offset"))
	if offset < 0 {
		offset = 0
	}

	params := store.AuditListParams{
		Action:       q.Get("action"),
		ResourceType: q.Get("resource_type"),
		Limit:        limit,
		Offset:       offset,
	}
	if rid := q.Get("resource_id"); rid != "" {
		if id, err := uuid.Parse(rid); err == nil {
			params.ResourceID = &id
		}
	}
	if gid := q.Get("group_id"); gid != "" {
		if id, err := uuid.Parse(gid); err == nil {
			params.GroupID = &id
		}
	}
	if aid := q.Get("actor_id"); aid != "" {
		if id, err := uuid.Parse(aid); err == nil {
			params.ActorID = &id
		}
	}

	entries, total, err := h.audit.List(ctx, tenantID, params)
	if err != nil {
		slog.Error("audit.list failed", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToList, "audit logs"))
		return
	}

	if entries == nil {
		entries = []store.AuditLogEntry{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items":  entries,
		"total":  total,
	})
}
