package http

import (
	"context"
	"net/http"
	"strconv"
)

// ExtractionDebugHandler exposes extraction worker status and debug history via HTTP.
// Uses interfaces to avoid importing the whatsapp package (prevents import cycles).
type ExtractionDebugHandler struct {
	statusFn func(ctx context.Context) any
	history  interface {
		List(limit int) any
		GetByID(id string) any
	}
}

// NewExtractionDebugHandler creates a handler using the provided status and history functions.
func NewExtractionDebugHandler(statusFn func(ctx context.Context) any, history interface {
	List(limit int) any
	GetByID(id string) any
}) *ExtractionDebugHandler {
	return &ExtractionDebugHandler{statusFn: statusFn, history: history}
}

// RegisterRoutes registers extraction debug routes on the given mux.
func (h *ExtractionDebugHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/kg/extraction/status", requireAuth("", h.handleStatus))
	mux.HandleFunc("GET /v1/kg/extraction/history", requireAuth("", h.handleHistory))
	mux.HandleFunc("GET /v1/kg/extraction/{id}", requireAuth("", h.handleDetail))
}

func (h *ExtractionDebugHandler) handleStatus(w http.ResponseWriter, r *http.Request) {
	if h.statusFn == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "extraction worker not available"})
		return
	}
	status := h.statusFn(r.Context())
	writeJSON(w, http.StatusOK, status)
}

func (h *ExtractionDebugHandler) handleHistory(w http.ResponseWriter, r *http.Request) {
	if h.history == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	records := h.history.List(limit)
	if records == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	writeJSON(w, http.StatusOK, records)
}

func (h *ExtractionDebugHandler) handleDetail(w http.ResponseWriter, r *http.Request) {
	if h.history == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "debug buffer not enabled"})
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing id"})
		return
	}
	rec := h.history.GetByID(id)
	if rec == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

// Ensure ExtractionDebugHandler satisfies the handler interface used by the server.
var _ interface{ RegisterRoutes(*http.ServeMux) } = (*ExtractionDebugHandler)(nil)
