//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func newListenRawTestStore(t *testing.T) (*SQLiteListenRawMessageStore, context.Context, *sql.DB) {
	t.Helper()
	db, err := OpenDB(filepath.Join(t.TempDir(), "raw.db"))
	if err != nil {
		t.Fatalf("OpenDB error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema error: %v", err)
	}
	ctx := store.WithTenantID(context.Background(), store.MasterTenantID)
	return NewSQLiteListenRawMessageStore(db), ctx, db
}

func newListenRawMsg(agent, graph string) store.ListenRawMessage {
	return store.ListenRawMessage{
		ID:           uuid.Must(uuid.NewV7()),
		ChannelName:  "test-channel",
		ChatID:       "chat-1",
		ChatName:     "Test Chat",
		GraphID:      graph,
		Sender:       "Sender",
		SenderID:     "s-1",
		Body:         "hello",
		MsgTimestamp: time.Now(),
		AgentID:      agent,
	}
}

func assertListenRawRow(t *testing.T, db *sql.DB, id uuid.UUID, wantAgent, wantGraph, wantStatus string, wantProcessed bool) {
	t.Helper()
	var agent, graph, status string
	var processed sql.NullString
	err := db.QueryRow(
		"SELECT agent_id, graph_id, extraction_status, processed_at FROM listen_raw_messages WHERE id = ?",
		id,
	).Scan(&agent, &graph, &status, &processed)
	if err != nil {
		t.Fatalf("query row %s: %v", id, err)
	}
	if agent != wantAgent {
		t.Errorf("agent_id: got %q want %q", agent, wantAgent)
	}
	if graph != wantGraph {
		t.Errorf("graph_id: got %q want %q", graph, wantGraph)
	}
	if status != wantStatus {
		t.Errorf("extraction_status: got %q want %q", status, wantStatus)
	}
	if wantProcessed && !processed.Valid {
		t.Errorf("expected processed_at set, got NULL")
	}
	if !wantProcessed && processed.Valid {
		t.Errorf("expected processed_at NULL, got %v", processed.String)
	}
}

// TestSQLiteListenRawMessageStore_UpdateScope covers the dynamic SET (both
// fields, graph-only, agent-only), the extraction reset, the no-op on empty ids,
// and that untouched rows are not modified. See SRS 007 FR-00/FR-01.
func TestSQLiteListenRawMessageStore_UpdateScope(t *testing.T) {
	s, ctx, db := newListenRawTestStore(t)

	m1 := newListenRawMsg("agent-old", "graph-old")
	m2 := newListenRawMsg("agent-old", "graph-old")
	m3 := newListenRawMsg("agent-old", "graph-old")
	if err := s.AppendBatch(ctx, []store.ListenRawMessage{m1, m2, m3}); err != nil {
		t.Fatalf("AppendBatch: %v", err)
	}

	// Simulate already-extracted rows (processed_at set, status=extracted).
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(
		"UPDATE listen_raw_messages SET processed_at = ?, extraction_status = ?",
		now, store.ExtractionStatusExtracted,
	); err != nil {
		t.Fatalf("seed processed: %v", err)
	}

	// Update m1 + m2: both agent + graph.
	n, err := s.UpdateScope(ctx, []uuid.UUID{m1.ID, m2.ID}, "agent-new", "graph-new")
	if err != nil {
		t.Fatalf("UpdateScope both: %v", err)
	}
	if n != 2 {
		t.Fatalf("affected: got %d want 2", n)
	}
	assertListenRawRow(t, db, m1.ID, "agent-new", "graph-new", store.ExtractionStatusPending, false)
	assertListenRawRow(t, db, m2.ID, "agent-new", "graph-new", store.ExtractionStatusPending, false)
	// m3 untouched (still extracted/processed).
	assertListenRawRow(t, db, m3.ID, "agent-old", "graph-old", store.ExtractionStatusExtracted, true)

	// graph-only update on m3 → agent unchanged, graph changed, reset.
	n, err = s.UpdateScope(ctx, []uuid.UUID{m3.ID}, "", "graph-only")
	if err != nil {
		t.Fatalf("UpdateScope graph-only: %v", err)
	}
	if n != 1 {
		t.Fatalf("affected: got %d want 1", n)
	}
	assertListenRawRow(t, db, m3.ID, "agent-old", "graph-only", store.ExtractionStatusPending, false)

	// agent-only update on m1 → graph unchanged, agent changed.
	n, err = s.UpdateScope(ctx, []uuid.UUID{m1.ID}, "agent-x", "")
	if err != nil {
		t.Fatalf("UpdateScope agent-only: %v", err)
	}
	if n != 1 {
		t.Fatalf("affected: got %d want 1", n)
	}
	assertListenRawRow(t, db, m1.ID, "agent-x", "graph-new", store.ExtractionStatusPending, false)

	// empty ids → no-op.
	n, err = s.UpdateScope(ctx, nil, "a", "g")
	if err != nil {
		t.Fatalf("UpdateScope empty ids: %v", err)
	}
	if n != 0 {
		t.Fatalf("affected: got %d want 0", n)
	}
}

// TestSQLiteListenRawMessageStore_UpdateScope_TenantIsolation verifies a caller
// scoped to tenant B cannot UpdateScope rows that belong to the master tenant.
// (Rows are inserted under master because tenant_id has an FK to tenants; we
// update under a ctx scoped to a non-inserted tenant B, so scopeClause excludes
// the master row.)
func TestSQLiteListenRawMessageStore_UpdateScope_TenantIsolation(t *testing.T) {
	s, _, db := newListenRawTestStore(t)
	ctxMaster := store.WithTenantID(context.Background(), store.MasterTenantID)
	ctxOther := store.WithTenantID(context.Background(), uuid.New()) // never inserted

	m := newListenRawMsg("agent-a", "graph-a")
	if err := s.AppendBatch(ctxMaster, []store.ListenRawMessage{m}); err != nil {
		t.Fatalf("AppendBatch: %v", err)
	}

	n, err := s.UpdateScope(ctxOther, []uuid.UUID{m.ID}, "agent-b", "graph-b")
	if err != nil {
		t.Fatalf("UpdateScope cross-tenant: %v", err)
	}
	if n != 0 {
		t.Fatalf("cross-tenant update should affect 0 rows, got %d", n)
	}
	// Row unchanged — still the master tenant's scope.
	assertListenRawRow(t, db, m.ID, "agent-a", "graph-a", store.ExtractionStatusPending, false)
}
