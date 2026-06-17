package http

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ListenRawMessagesHandler handles listen-only raw message listing endpoints.
type ListenRawMessagesHandler struct {
	store      store.ListenRawMessageStore
	chunkStore store.RawMessageChunkStore // optional; enables true-move chunk cleanup on scope edit
}

// NewListenRawMessagesHandler creates a handler for raw message endpoints. The
// chunk store enables the scope-edit "true move" (delete old-scope chunks + re-queue
// neighbors). It may be nil to disable cleanup (graceful degradation to additive).
func NewListenRawMessagesHandler(s store.ListenRawMessageStore, cs store.RawMessageChunkStore) *ListenRawMessagesHandler {
	return &ListenRawMessagesHandler{store: s, chunkStore: cs}
}

// RegisterRoutes registers raw message routes on the given mux.
func (h *ListenRawMessagesHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/listen-raw-messages", h.authMiddleware(h.handleList))
	mux.HandleFunc("GET /v1/listen-raw-messages/stats", h.authMiddleware(h.handleStats))
	mux.HandleFunc("POST /v1/listen-raw-messages/reset-processed", h.authMiddleware(h.handleResetProcessed))
	mux.HandleFunc("POST /v1/listen-raw-messages/reset", h.authMiddleware(h.handleResetByIDs))
	mux.HandleFunc("POST /v1/listen-raw-messages/scope", h.authMiddleware(h.handleUpdateScope))
}

func (h *ListenRawMessagesHandler) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth("", next)
}

func (h *ListenRawMessagesHandler) handleList(w http.ResponseWriter, r *http.Request) {
	opts := store.ListenRawMessageListOpts{
		Limit:  50,
		Offset: 0,
	}

	if v := r.URL.Query().Get("channel_name"); v != "" {
		opts.ChannelName = v
	}
	if v := r.URL.Query().Get("chat_id"); v != "" {
		opts.ChatID = v
	}
	if v := r.URL.Query().Get("agent_id"); v != "" {
		opts.AgentID = v
	}
	if v := r.URL.Query().Get("graph_id"); v != "" {
		opts.GraphID = v
	}
	// Substring text filters (SRS 008 FR-00/FR-05). Server-side so they participate
	// in the COUNT/paging (previously page-only in the UI).
	if v := r.URL.Query().Get("chat"); v != "" {
		opts.Chat = v
	}
	if v := r.URL.Query().Get("sender"); v != "" {
		opts.Sender = v
	}
	if v := r.URL.Query().Get("body"); v != "" {
		opts.Body = v
	}
	if v := r.URL.Query().Get("processed"); v != "" {
		b := v == "true" || v == "1"
		opts.Processed = &b
	}
	if v := r.URL.Query().Get("extraction_status"); v != "" {
		opts.ExtractionStatus = v
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			opts.Limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			opts.Offset = n
		}
	}

	msgs, total, err := h.store.List(r.Context(), opts)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if msgs == nil {
		msgs = []store.ListenRawMessage{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"messages": msgs,
		"total":    total,
		"limit":    opts.Limit,
		"offset":   opts.Offset,
	})
}

func (h *ListenRawMessagesHandler) handleResetProcessed(w http.ResponseWriter, r *http.Request) {
	agentID := r.URL.Query().Get("agent_id")
	graphID := r.URL.Query().Get("graph_id")

	affected, err := h.store.ResetProcessed(r.Context(), agentID, graphID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	resp := map[string]any{
		"reset_count": affected,
	}
	if agentID != "" {
		resp["agent_id"] = agentID
	}
	if graphID != "" {
		resp["graph_id"] = graphID
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *ListenRawMessagesHandler) handleResetByIDs(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		slog.Warn("http.reset_by_ids: invalid body", "error", err)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	if len(body.IDs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "ids is required"})
		return
	}

	ids := make([]uuid.UUID, 0, len(body.IDs))
	for _, s := range body.IDs {
		id, err := uuid.Parse(s)
		if err != nil {
			slog.Warn("http.reset_by_ids: invalid id", "id", s, "error", err)
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id: " + s})
			return
		}
		ids = append(ids, id)
	}

	affected, err := h.store.ResetProcessedByIDs(r.Context(), ids)
	if err != nil {
		slog.Error("http.reset_by_ids: store error", "count", len(ids), "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	slog.Info("http.reset_by_ids: ok", "requested", len(ids), "affected", affected)
	writeJSON(w, http.StatusOK, map[string]any{
		"reset_count": affected,
	})
}

// handleUpdateScope edits the agent_id and/or graph_id (the KG scope pair) of the
// given raw messages and resets their extraction state to pending, so the
// extraction worker re-processes them under the corrected scope. See SRS 007.
func (h *ListenRawMessagesHandler) handleUpdateScope(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs     []string `json:"ids"`
		AgentID string   `json:"agent_id"`
		GraphID string   `json:"graph_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		slog.Warn("http.update_scope: invalid body", "error", err)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	if len(body.IDs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "ids is required"})
		return
	}

	ids := make([]uuid.UUID, 0, len(body.IDs))
	for _, s := range body.IDs {
		id, err := uuid.Parse(s)
		if err != nil {
			slog.Warn("http.update_scope: invalid id", "id", s, "error", err)
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id: " + s})
			return
		}
		ids = append(ids, id)
	}

	agentID := strings.TrimSpace(body.AgentID)
	graphID := strings.TrimSpace(body.GraphID)
	if agentID == "" && graphID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "agent_id or graph_id is required"})
		return
	}
	if agentID != "" {
		if _, err := uuid.Parse(agentID); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid agent_id"})
			return
		}
	}
	if len(graphID) > 255 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "graph_id too long"})
		return
	}

	affected, err := h.store.UpdateScope(r.Context(), ids, agentID, graphID)
	if err != nil {
		slog.Error("http.update_scope: store error", "count", len(ids), "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// True-move cleanup: delete the message's old-scope chunks (including day-group
	// neighbor chunks, since the embedding worker groups a chat's day of messages
	// into shared chunks) and re-queue those neighbors for re-embedding under their
	// unchanged scope, so the message cleanly moves to the new scope instead of
	// appearing under both in the embeddings menu. Best-effort, non-transactional;
	// logged. On failure the edit still succeeds (degrades to additive). See SRS 007 FR-08.
	var chunksDeleted int64
	var neighborsRequeued int64
	if affected > 0 && h.chunkStore != nil {
		sources, deleted, derr := h.chunkStore.DeleteBySourceMsgIDs(r.Context(), ids)
		if derr != nil {
			slog.Warn("http.update_scope: chunk cleanup failed (edit still applied)", "error", derr)
		} else {
			chunksDeleted = deleted
			changed := make(map[uuid.UUID]struct{}, len(ids))
			for _, id := range ids {
				changed[id] = struct{}{}
			}
			var neighbors []uuid.UUID
			for _, src := range sources {
				if _, ok := changed[src]; !ok {
					neighbors = append(neighbors, src)
				}
			}
			if len(neighbors) > 0 {
				if rq, rerr := h.store.ResetEmbeddedByIDs(r.Context(), neighbors); rerr != nil {
					slog.Warn("http.update_scope: neighbor re-queue failed (neighbors may need manual reset)", "error", rerr)
				} else {
					neighborsRequeued = rq
				}
			}
		}
	}

	slog.Info("http.update_scope: ok",
		"requested", len(ids), "affected", affected,
		"agent_id", agentID, "graph_id", graphID,
		"chunks_deleted", chunksDeleted, "neighbors_requeued", neighborsRequeued)
	writeJSON(w, http.StatusOK, map[string]any{
		"updated_count":      affected,
		"chunks_deleted":     chunksDeleted,
		"neighbors_requeued": neighborsRequeued,
	})
}

func (h *ListenRawMessagesHandler) handleStats(w http.ResponseWriter, r *http.Request) {
	extractionStats, err := h.store.ExtractionStats(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if extractionStats == nil {
		extractionStats = map[string]int{}
	}

	pendingEmb, embeddedEmb, err := h.store.EmbeddingStats(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"extraction": extractionStats,
		"embedding": map[string]int{
			"pending":  pendingEmb,
			"embedded": embeddedEmb,
		},
	})
}
