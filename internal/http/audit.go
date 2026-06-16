package http

import (
	"context"
	"encoding/csv"
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
// Accessible to Member+ with role-scoped filtering (SRS §6.7.14).
func (h *AuditHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/audit", requireAuth(permissions.RoleMember, h.handleList))
	mux.HandleFunc("GET /v1/audit/export", requireAuthAction(string(permissions.PermAuditExport), h.handleExport))
}

// handleList returns a paginated list of audit log entries.
// Member callers receive silently filtered results (Member/Viewer entries only).
// Admin+ callers with audit.view_all see all entries.
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

	// Role-scoped filtering (SRS §7.1.6):
	// Member callers without audit.view_all see only Member/Viewer entries.
	// Owner/Admin entries are silently excluded.
	if !hasPermission(ctx, string(permissions.PermAuditViewAll)) {
		params.ExcludeActorRoles = []string{"owner", "admin"}
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
		"items": entries,
		"total": total,
	})
}

// hasPermission checks if the current user has a specific permission via the resolver cache.
func hasPermission(ctx context.Context, perm string) bool {
	if pkgPermCache == nil {
		return false
	}
	userID := store.UserIDFromContext(ctx)
	tenantID := store.TenantIDFromContext(ctx)
	if userID == "" || tenantID == uuid.Nil {
		return false
	}
	return pkgPermCache.HasPermission(ctx, userID, tenantID, permissions.Permission(perm))
}

// handleExport streams all audit log entries as a CSV download.
// Requires the audit.export permission (SRS §6.6.6).
func (h *AuditHandler) handleExport(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	ctx := r.Context()
	tenantID := store.TenantIDFromContext(ctx)

	q := r.URL.Query()
	params := store.AuditListParams{
		Action:       q.Get("action"),
		ResourceType: q.Get("resource_type"),
		Limit:        10000, // max export size
		Offset:       0,
	}
	if rid := q.Get("resource_id"); rid != "" {
		if id, err := uuid.Parse(rid); err == nil {
			params.ResourceID = &id
		}
	}

	entries, _, err := h.audit.List(ctx, tenantID, params)
	if err != nil {
		slog.Error("audit.export failed", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToList, "audit logs"))
		return
	}

	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", "attachment; filename=audit-log.csv")

	cw := csv.NewWriter(w)
	defer cw.Flush()

	// Header row
	cw.Write([]string{"id", "timestamp", "actor_id", "action", "resource_type", "resource_id", "group_id", "ip_address"})

	for _, e := range entries {
		actorID := ""
		if e.ActorID != nil {
			actorID = e.ActorID.String()
		}
		groupID := ""
		if e.GroupID != nil {
			groupID = e.GroupID.String()
		}
		ipAddress := ""
		if e.IPAddress != nil {
			ipAddress = *e.IPAddress
		}
		cw.Write([]string{
			e.ID.String(),
			e.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
			actorID,
			e.Action,
			e.ResourceType,
			e.ResourceID.String(),
			groupID,
			ipAddress,
		})
	}
}
