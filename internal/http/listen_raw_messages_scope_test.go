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
// It embeds the interface so all methods compile; only UpdateScope is exercised
// by handleUpdateScope, so only it is implemented meaningfully.
type mockListenRawStore struct {
	store.ListenRawMessageStore
	updateCount int64
	updateErr   error
	gotIDs      []uuid.UUID
	gotAgent    string
	gotGraph    string
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

func scopeReq(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/v1/listen-raw-messages/scope", strings.NewReader(body))
	return req
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
			h := NewListenRawMessagesHandler(mock)
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

var errSentinel = &sentinelErr{"store boom"}

type sentinelErr struct{ msg string }

func (e *sentinelErr) Error() string { return e.msg }
