package http

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// GroupsHandler handles group management endpoints.
type GroupsHandler struct {
	groups store.GroupStore
}

// NewGroupsHandler creates a handler for group management endpoints.
func NewGroupsHandler(groups store.GroupStore) *GroupsHandler {
	return &GroupsHandler{groups: groups}
}

// RegisterRoutes registers all group management routes on the given mux.
func (h *GroupsHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/groups", requireAuthAction("group.create", h.handleCreate))
	mux.HandleFunc("GET /v1/groups", requireAuth("", h.handleList))
	mux.HandleFunc("GET /v1/groups/{id}", requireAuth("", h.handleGet))
	mux.HandleFunc("PATCH /v1/groups/{id}", requireAuthAction("group.update", h.handleUpdate))
	mux.HandleFunc("DELETE /v1/groups/{id}", requireAuthAction("group.delete", h.handleDelete))
	mux.HandleFunc("GET /v1/groups/{id}/members", requireAuth("", h.handleListMembers))
	mux.HandleFunc("POST /v1/groups/{id}/members", requireAuthAction("group.manage_members", h.handleAddMember))
	mux.HandleFunc("DELETE /v1/groups/{id}/members/{userId}", requireAuthAction("group.manage_members", h.handleRemoveMember))
	mux.HandleFunc("PATCH /v1/groups/{id}/members/{userId}/role", requireAuthAction("group.assign_admin", h.handleRoleChange))
	mux.HandleFunc("GET /v1/groups/{id}/join-requests", requireAuthAction("group.manage_members", h.handleListJoinRequests))
	mux.HandleFunc("PATCH /v1/groups/{id}/join-requests/{reqId}", requireAuthAction("group.manage_members", h.handleReviewJoinRequest))
}

// handleCreate creates a new group.
func (h *GroupsHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	ctx := r.Context()
	tenantID := store.TenantIDFromContext(ctx)

	var input struct {
		Name         string     `json:"name"`
		Slug         string     `json:"slug"`
		Description  *string    `json:"description"`
		Visibility   string     `json:"visibility"`
		ParentGroupID *uuid.UUID `json:"parent_group_id"`
	}
	if !bindJSON(w, r, locale, &input) {
		return
	}

	if input.Name == "" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "name"))
		return
	}

	// Auto-generate slug from name if not provided.
	slug := input.Slug
	if slug == "" {
		slug = generateSlug(input.Name)
	}
	if !isValidSlug(slug) {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidSlug, "slug"))
		return
	}

	switch input.Visibility {
	case store.GroupVisibilityOpen, store.GroupVisibilityClosed:
		// valid
	case "":
		input.Visibility = store.GroupVisibilityOpen
	default:
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidRequest, "visibility must be one of: open, closed"))
		return
	}

	// Resolve created_by from user context.
	var createdBy *uuid.UUID
	if userIDStr := store.UserIDFromContext(ctx); userIDStr != "" {
		if parsed, err := uuid.Parse(userIDStr); err == nil {
			createdBy = &parsed
		}
	}

	now := time.Now()
	group := &store.GroupData{
		ID:            store.GenNewID(),
		Name:          input.Name,
		Slug:          slug,
		Description:   input.Description,
		ParentGroupID: input.ParentGroupID,
		TenantID:      tenantID,
		Visibility:    input.Visibility,
		CreatedBy:     createdBy,
		Status:        store.GroupStatusActive,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if err := h.groups.CreateGroup(ctx, group); err != nil {
		slog.Error("groups.create failed", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToCreate, "group", "internal error"))
		return
	}

	writeJSON(w, http.StatusCreated, group)
}

// handleList returns a paginated list of groups in the caller's tenant.
func (h *GroupsHandler) handleList(w http.ResponseWriter, r *http.Request) {
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

	params := store.GroupListParams{
		Search:     q.Get("search"),
		Visibility: q.Get("visibility"),
		Status:     q.Get("status"),
		Limit:      limit,
		Offset:     offset,
	}
	if pid := q.Get("parent_id"); pid != "" {
		if id, err := uuid.Parse(pid); err == nil {
			params.ParentID = &id
		}
	}

	result, err := h.groups.ListGroups(ctx, tenantID, params)
	if err != nil {
		slog.Error("groups.list failed", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToList, "groups"))
		return
	}

	if result.Groups == nil {
		result.Groups = []store.GroupData{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items":  result.Groups,
		"total":  result.Total,
		"offset": result.Offset,
		"limit":  result.Limit,
	})
}

// handleGet returns a single group by ID.
func (h *GroupsHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	ctx := r.Context()

	id, ok := parsePathUUID(w, r, "id", locale, "group")
	if !ok {
		return
	}

	group, err := h.groups.GetGroup(ctx, id)
	if err != nil {
		slog.Error("groups.get failed", "error", err, "id", id)
		writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "group", id.String()))
		return
	}

	writeJSON(w, http.StatusOK, group)
}

// handleUpdate updates an existing group.
func (h *GroupsHandler) handleUpdate(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	ctx := r.Context()

	id, ok := parsePathUUID(w, r, "id", locale, "group")
	if !ok {
		return
	}

	group, err := h.groups.GetGroup(ctx, id)
	if err != nil {
		slog.Error("groups.update get failed", "error", err, "id", id)
		writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "group", id.String()))
		return
	}

	var input struct {
		Name         *string    `json:"name"`
		Slug         *string    `json:"slug"`
		Description  *string    `json:"description"`
		Visibility   *string    `json:"visibility"`
		ParentGroupID *uuid.UUID `json:"parent_group_id"`
	}
	if !bindJSON(w, r, locale, &input) {
		return
	}

	if input.Name != nil {
		if *input.Name == "" {
			writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "name"))
			return
		}
		group.Name = *input.Name
	}
	if input.Slug != nil {
		if !isValidSlug(*input.Slug) {
			writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
				i18n.T(locale, i18n.MsgInvalidSlug, "slug"))
			return
		}
		group.Slug = *input.Slug
	}
	if input.Description != nil {
		group.Description = input.Description
	}
	if input.Visibility != nil {
		switch *input.Visibility {
		case store.GroupVisibilityOpen, store.GroupVisibilityClosed:
			group.Visibility = *input.Visibility
		default:
			writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
				i18n.T(locale, i18n.MsgInvalidRequest, "visibility must be one of: open, closed"))
			return
		}
	}
	if input.ParentGroupID != nil {
		group.ParentGroupID = input.ParentGroupID
	}

	group.UpdatedAt = time.Now()

	if err := h.groups.UpdateGroup(ctx, group); err != nil {
		slog.Error("groups.update failed", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToUpdate, "group", "internal error"))
		return
	}

	writeJSON(w, http.StatusOK, group)
}

// handleDelete soft-deletes a group.
func (h *GroupsHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	ctx := r.Context()

	id, ok := parsePathUUID(w, r, "id", locale, "group")
	if !ok {
		return
	}

	if err := h.groups.DeleteGroup(ctx, id); err != nil {
		slog.Error("groups.delete failed", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToDelete, "group"))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleListMembers returns all members of a group.
func (h *GroupsHandler) handleListMembers(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	ctx := r.Context()

	id, ok := parsePathUUID(w, r, "id", locale, "group")
	if !ok {
		return
	}

	members, err := h.groups.ListMembers(ctx, id)
	if err != nil {
		slog.Error("groups.list_members failed", "error", err, "group_id", id)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToList, "group members"))
		return
	}

	if members == nil {
		members = []store.GroupMemberData{}
	}

	writeJSON(w, http.StatusOK, members)
}

// handleAddMember adds a user to a group.
func (h *GroupsHandler) handleAddMember(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	ctx := r.Context()

	groupID, ok := parsePathUUID(w, r, "id", locale, "group")
	if !ok {
		return
	}

	var input struct {
		UserID uuid.UUID `json:"user_id"`
		Role   string    `json:"role"`
	}
	if !bindJSON(w, r, locale, &input) {
		return
	}

	if input.Role == "" {
		input.Role = store.GroupRoleMember
	}
	switch input.Role {
	case store.GroupRoleAdmin, store.GroupRoleMember:
		// valid
	default:
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidRequest, "role must be one of: admin, member"))
		return
	}

	if input.UserID == uuid.Nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "user_id"))
		return
	}

	member := &store.GroupMemberData{
		ID:        store.GenNewID(),
		GroupID:   groupID,
		UserID:    input.UserID,
		Role:      input.Role,
		JoinedAt:  time.Now(),
		JoinedVia: store.JoinedViaAdminAdd,
	}

	if err := h.groups.AddMember(ctx, member); err != nil {
		slog.Error("groups.add_member failed", "error", err, "group_id", groupID, "user_id", input.UserID)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToCreate, "group member", "internal error"))
		return
	}

	writeJSON(w, http.StatusCreated, member)
}

// handleRemoveMember removes a user from a group.
func (h *GroupsHandler) handleRemoveMember(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	ctx := r.Context()

	groupID, ok := parsePathUUID(w, r, "id", locale, "group")
	if !ok {
		return
	}

	userIDStr := r.PathValue("userId")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "user"))
		return
	}

	if err := h.groups.RemoveMember(ctx, groupID, userID); err != nil {
		slog.Error("groups.remove_member failed", "error", err, "group_id", groupID, "user_id", userID)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToDelete, "group member"))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

// handleRoleChange updates a member's role within a group.
func (h *GroupsHandler) handleRoleChange(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	ctx := r.Context()

	groupID, ok := parsePathUUID(w, r, "id", locale, "group")
	if !ok {
		return
	}

	userIDStr := r.PathValue("userId")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "user"))
		return
	}

	var input struct {
		Role string `json:"role"`
	}
	if !bindJSON(w, r, locale, &input) {
		return
	}

	switch input.Role {
	case store.GroupRoleAdmin, store.GroupRoleMember:
		// valid
	default:
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest,
			i18n.T(locale, i18n.MsgInvalidRequest, "role must be one of: admin, member"))
		return
	}

	if err := h.groups.UpdateMemberRole(ctx, groupID, userID, input.Role); err != nil {
		slog.Error("groups.role_change failed", "error", err, "group_id", groupID, "user_id", userID)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToUpdate, "member role", "internal error"))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"role": input.Role})
}

// handleListJoinRequests returns pending join requests for a group.
func (h *GroupsHandler) handleListJoinRequests(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	ctx := r.Context()

	id, ok := parsePathUUID(w, r, "id", locale, "group")
	if !ok {
		return
	}

	status := r.URL.Query().Get("status")
	requests, err := h.groups.ListJoinRequests(ctx, id, status)
	if err != nil {
		slog.Error("groups.list_join_requests failed", "error", err, "group_id", id)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToList, "join requests"))
		return
	}

	if requests == nil {
		requests = []store.JoinRequestData{}
	}

	writeJSON(w, http.StatusOK, requests)
}

// handleReviewJoinRequest approves or rejects a join request.
func (h *GroupsHandler) handleReviewJoinRequest(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	ctx := r.Context()

	_, ok := parsePathUUID(w, r, "id", locale, "group")
	if !ok {
		return
	}

	reqIDStr := r.PathValue("reqId")
	reqID, err := uuid.Parse(reqIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "join request"))
		return
	}

	var input struct {
		Approved bool `json:"approved"`
	}
	if !bindJSON(w, r, locale, &input) {
		return
	}

	var reviewerID uuid.UUID
	if userIDStr := store.UserIDFromContext(ctx); userIDStr != "" {
		if parsed, parseErr := uuid.Parse(userIDStr); parseErr == nil {
			reviewerID = parsed
		}
	}

	if err := h.groups.ReviewJoinRequest(ctx, reqID, input.Approved, reviewerID); err != nil {
		slog.Error("groups.review_join_request failed", "error", err, "req_id", reqID)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToUpdate, "join request", "internal error"))
		return
	}

	status := "rejected"
	if input.Approved {
		status = "approved"
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": status})
}

// parsePathUUID extracts and validates a UUID path parameter.
// Writes an error response and returns false if invalid.
func parsePathUUID(w http.ResponseWriter, r *http.Request, param, locale, entity string) (uuid.UUID, bool) {
	idStr := r.PathValue(param)
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, entity))
		return uuid.Nil, false
	}
	return id, true
}

// generateSlug creates a URL-safe slug from a name.
func generateSlug(name string) string {
	s := strings.ToLower(name)
	s = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			return r
		}
		return '-'
	}, s)
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	s = strings.Trim(s, "-")
	if s == "" {
		s = "group"
	}
	return s
}
