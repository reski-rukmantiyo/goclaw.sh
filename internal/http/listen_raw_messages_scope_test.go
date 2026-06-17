package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// mockListenRawStore satisfies store.ListenRawMessageStore for handler tests.
// It embeds the interface so all methods compile; only UpdateScope +
// ResetEmbeddedByIDs are exercised by handleUpdateScope.
type mockListenRawStore struct {
	store.ListenRawMessageStore
	updateCount       int64
	updateErr         error
	gotIDs            []uuid.UUID
	gotAgent          string
	gotGraph          string
	resetEmbeddedIDs  []uuid.UUID
	resetEmbeddedCall bool
}

func (m *mockListenRawStore) UpdateScope(_ context.Context, ids []uuid.UUID, agentID, graphID string) (int64, error) {
	m.gotIDs = ids
	m.gotAgent = agentID
	m.gotGraph = graphID
	if m.updateErr != nil {
		return 0, m.updateErr
	}
	return m.updateCount, nil
}

func (m *mockListenRawStore) ResetEmbeddedByIDs(_ context.Context, ids []uuid.UUID) (int64, error) {
	m.resetEmbeddedIDs = ids
	m.resetEmbeddedCall = true
	return int64(len(ids)), nil
}

// mockChunkStore satisfies store.RawMessageChunkStore for handler tests; only
// DeleteBySourceMsgIDs is exercised by the true-move path.
type mockChunkStore struct {
	store.RawMessageChunkStore
	delSources []uuid.UUID
	delCount   int64
	delErr     error
	gotDelIDs  []uuid.UUID
	delCalled  bool
}

func (m *mockChunkStore) DeleteBySourceMsgIDs(_ context.Context, msgIDs []uuid.UUID) ([]uuid.UUID, int64, error) {
	m.gotDelIDs = msgIDs
	m.delCalled = true
	if m.delErr != nil {
		return nil, 0, m.delErr
	}
	return m.delSources, m.delCount, nil
}

func scopeReq(body string) *http.Request {
	return httptest.NewRequest(http.MethodPost, "/v1/listen-raw-messages/scope", strings.NewReader(body))
}

func decodeScopeResp(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.NewDecoder(w.Body).Decode(&m); err != nil {
		t.Fatalf("decode response: %v (body=%s)", err, w.Body.String())
	}
	return m
}

// TestHandleUpdateScope covers the validation branches and the success path of
// handleUpdateScope (FR-02, FR-03). Auth/tenant scope are exercised in the store
// tests; the handler method itself does no auth (the requireAuth wrapper does).
func TestHandleUpdateScope(t *testing.T) {
	validID := uuid.New()
	otherID := uuid.New()

	tests := []struct {
		name         string
		body         string
		storeCount   int64
		storeErr     error
		wantStatus   int
		wantCountKey any // expected updated_count value (nil if absent)
		wantAgent    string
		wantGraph    string
		wantErr      string
	}{
		{
			name:         "both fields ok",
			body:         `{"ids":["` + validID.String() + `","` + otherID.String() + `"],"agent_id":"` + uuid.New().String() + `","graph_id":"project-x"}`,
			storeCount:   2,
			wantStatus:   http.StatusOK,
			wantCountKey: float64(2),
			wantGraph:    "project-x",
		},
		{
			name:         "agent only ok (graph_id empty after trim)",
			body:         `{"ids":["` + validID.String() + `"],"agent_id":"` + uuid.New().String() + `","graph_id":"   "}`,
			storeCount:   1,
			wantStatus:   http.StatusOK,
			wantCountKey: float64(1),
			wantGraph:    "",
		},
		{
			name:       "graph only ok, whitespace trimmed",
			body:       `{"ids":["` + validID.String() + `"],"graph_id":"  project-y  "}`,
			storeCount: 1,
			wantStatus: http.StatusOK,
			wantGraph:  "project-y",
		},
		{
			name:       "empty ids",
			body:       `{"ids":[],"graph_id":"g"}`,
			wantStatus: http.StatusBadRequest,
			wantErr:    "ids is required",
		},
		{
			name:       "invalid id uuid",
			body:       `{"ids":["not-a-uuid"],"graph_id":"g"}`,
			wantStatus: http.StatusBadRequest,
			wantErr:    "invalid id",
		},
		{
			name:       "neither field provided",
			body:       `{"ids":["` + validID.String() + `"]}`,
			wantStatus: http.StatusBadRequest,
			wantErr:    "agent_id or graph_id is required",
		},
		{
			name:       "invalid agent_id",
			body:       `{"ids":["` + validID.String() + `"],"agent_id":"nope"}`,
			wantStatus: http.StatusBadRequest,
			wantErr:    "invalid agent_id",
		},
		{
			name:       "graph_id too long",
			body:       `{"ids":["` + validID.String() + `"],"graph_id":"` + strings.Repeat("a", 256) + `"}`,
			wantStatus: http.StatusBadRequest,
			wantErr:    "graph_id too long",
		},
		{
			name:       "invalid json body",
			body:       `{not json`,
			wantStatus: http.StatusBadRequest,
			wantErr:    "invalid JSON body",
		},
		{
			name:         "store error -> 500",
			body:         `{"ids":["` + validID.String() + `"],"graph_id":"g"}`,
			storeErr:     errSentinel,
			wantStatus:   http.StatusInternalServerError,
			wantCountKey: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockListenRawStore{updateCount: tt.storeCount, updateErr: tt.storeErr}
			h := NewListenRawMessagesHandler(mock, &mockChunkStore{})
			w := httptest.NewRecorder()
			h.handleUpdateScope(w, scopeReq(tt.body))

			if w.Code != tt.wantStatus {
				t.Fatalf("status: got %d want %d (body=%s)", w.Code, tt.wantStatus, w.Body.String())
			}

			resp := decodeScopeResp(t, w)
			switch {
			case tt.wantErr != "":
				if got, _ := resp["error"].(string); !strings.Contains(got, tt.wantErr) {
					t.Errorf("error: got %q want substring %q", got, tt.wantErr)
				}
			case tt.wantStatus == http.StatusOK:
				if tt.wantCountKey != nil {
					if got := resp["updated_count"]; got != tt.wantCountKey {
						t.Errorf("updated_count: got %v want %v", got, tt.wantCountKey)
					}
				}
				if tt.wantAgent != "" && mock.gotAgent != tt.wantAgent {
					t.Errorf("store agent_id: got %q want %q", mock.gotAgent, tt.wantAgent)
				}
				if tt.wantGraph != "" || tt.name == "agent only ok (graph_id empty after trim)" {
					if mock.gotGraph != tt.wantGraph {
						t.Errorf("store graph_id: got %q want %q", mock.gotGraph, tt.wantGraph)
					}
				}
			}
		})
	}
}

// TestHandleUpdateScope_TrueMove verifies that after a successful scope edit the
// handler deletes the message's old-scope chunks and re-queues day-group neighbor
// messages (whose chunks were co-deleted) for re-embedding (SRS 007 FR-08).
func TestHandleUpdateScope_TrueMove(t *testing.T) {
	changedID := uuid.New()
	neighborID := uuid.New()

	mock := &mockListenRawStore{updateCount: 1}
	chunkMock := &mockChunkStore{
		// deleted chunks covered the changed message + one neighbor (day-group).
		delSources: []uuid.UUID{changedID, neighborID},
		delCount:   2,
	}
	h := NewListenRawMessagesHandler(mock, chunkMock)
	w := httptest.NewRecorder()
	h.handleUpdateScope(w, scopeReq(`{"ids":["`+changedID.String()+`"],"graph_id":"project-new"}`))

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200 (body=%s)", w.Code, w.Body.String())
	}
	resp := decodeScopeResp(t, w)
	if got := resp["updated_count"]; got != float64(1) {
		t.Errorf("updated_count: got %v want 1", got)
	}
	if got := resp["chunks_deleted"]; got != float64(2) {
		t.Errorf("chunks_deleted: got %v want 2", got)
	}
	if got := resp["neighbors_requeued"]; got != float64(1) {
		t.Errorf("neighbors_requeued: got %v want 1", got)
	}

	if !chunkMock.delCalled {
		t.Fatal("expected DeleteBySourceMsgIDs to be called")
	}
	if len(chunkMock.gotDelIDs) != 1 || chunkMock.gotDelIDs[0] != changedID {
		t.Errorf("DeleteBySourceMsgIDs ids: got %v want [%s]", chunkMock.gotDelIDs, changedID)
	}
	if !mock.resetEmbeddedCall {
		t.Fatal("expected ResetEmbeddedByIDs to be called for the neighbor")
	}
	if len(mock.resetEmbeddedIDs) != 1 || mock.resetEmbeddedIDs[0] != neighborID {
		t.Errorf("ResetEmbeddedByIDs ids: got %v want [%s] (neighbor only, not the changed id)",
			mock.resetEmbeddedIDs, neighborID)
	}
}

// TestHandleUpdateScope_TrueMove_NoChunkStore verifies cleanup is skipped (no
// panic) when the chunk store is nil (graceful degradation to additive).
func TestHandleUpdateScope_TrueMove_NoChunkStore(t *testing.T) {
	validID := uuid.New()
	mock := &mockListenRawStore{updateCount: 1}
	h := NewListenRawMessagesHandler(mock, nil)
	w := httptest.NewRecorder()
	h.handleUpdateScope(w, scopeReq(`{"ids":["`+validID.String()+`"],"graph_id":"g"}`))

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200", w.Code)
	}
	resp := decodeScopeResp(t, w)
	if resp["chunks_deleted"] != float64(0) {
		t.Errorf("chunks_deleted: got %v want 0 (no chunk store)", resp["chunks_deleted"])
	}
	if mock.resetEmbeddedCall {
		t.Error("ResetEmbeddedByIDs should not be called when chunk store is nil")
	}
}

var errSentinel = &sentinelErr{"store boom"}

type sentinelErr struct{ msg string }

func (e *sentinelErr) Error() string { return e.msg }
