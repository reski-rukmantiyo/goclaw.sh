//go:build integration

// Package pglistenmedia holds PostgreSQL integration tests for the SRS 014
// media-enrichment store additions. It lives in its own subpackage because the
// parent integration package currently has an unrelated pre-existing build
// breakage (tests/integration/tenant_provision_test.go:166 — stale AddUser
// signature), which would otherwise block these tests from building.
//
// Regression origin: the live worker hit "expected 3 arguments, got 2" — a PG
// placeholder/argument desync in ListPendingMediaEnrichment that unit tests
// (SQLite-only) could not catch.
package pglistenmedia

import (
	"context"
	"database/sql"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

const defaultTestDSN = "postgres://postgres:test@localhost:5433/goclaw_test?sslmode=disable"

var (
	sharedDB     *sql.DB
	sharedDBOnce sync.Once
	sharedDBErr  error
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	sharedDBOnce.Do(func() {
		dsn := os.Getenv("TEST_DATABASE_URL")
		if dsn == "" {
			dsn = defaultTestDSN
		}
		db, err := sql.Open("pgx", dsn)
		if err != nil {
			sharedDBErr = err
			return
		}
		if err := db.Ping(); err != nil {
			sharedDBErr = err
			return
		}
		m, err := migrate.New("file://../../../migrations", dsn)
		if err != nil {
			sharedDBErr = err
			return
		}
		if err := m.Up(); err != nil && err != migrate.ErrNoChange {
			sharedDBErr = err
			return
		}
		m.Close()
		pg.InitSqlx(db)
		sharedDB = db
	})
	if sharedDBErr != nil {
		t.Skipf("test PG not available: %v", sharedDBErr)
	}
	return sharedDB
}

// seedTenant creates a minimal tenant row for FK satisfaction and cleans up
// its listen_raw_messages rows after the test.
func seedTenant(t *testing.T, db *sql.DB) (uuid.UUID, context.Context) {
	t.Helper()
	tenantID := uuid.New()
	if _, err := db.Exec(
		`INSERT INTO tenants (id, name, slug, status) VALUES ($1, $2, $3, 'active') ON CONFLICT DO NOTHING`,
		tenantID, "test-tenant-"+tenantID.String()[:8], "t"+tenantID.String()[:8]); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM listen_raw_messages WHERE tenant_id = $1", tenantID)
		db.Exec("DELETE FROM tenants WHERE id = $1", tenantID)
	})
	return tenantID, store.WithTenantID(context.Background(), tenantID)
}

func newMediaEnrichRow(agentID uuid.UUID, body string, refs []store.RawMediaRef) store.ListenRawMessage {
	return store.ListenRawMessage{
		ID:           uuid.Must(uuid.NewV7()),
		ChannelName:  "test-channel",
		ChatID:       "chat-1",
		ChatName:     "Test Chat",
		GraphID:      "graph-1",
		Sender:       "Sender",
		SenderID:     "s-1",
		Body:         body,
		MsgTimestamp: time.Now(),
		AgentID:      agentID.String(),
		MediaRefs:    refs,
	}
}

func imageRef(id string) store.RawMediaRef {
	return store.RawMediaRef{MediaID: id, FilePath: "/tmp/" + id + ".jpg", MediaType: "image", ContentType: "image/jpeg", FileName: id + ".jpg", FileSize: 100}
}

func setPGCreatedAt(t *testing.T, db *sql.DB, id uuid.UUID, ts time.Time) {
	t.Helper()
	if _, err := db.Exec("UPDATE listen_raw_messages SET created_at = $1 WHERE id = $2", ts, id); err != nil {
		t.Fatalf("set created_at %s: %v", id, err)
	}
}

// TestPGListenRawMediaEnrichment covers the SRS 014 store additions against a
// real PostgreSQL: the enrichment poll (fresh + backfill, incl. bulk-marked
// rows staying backfill-eligible), the enrichment UPDATE with pipeline reset,
// pass-through marking, the FR-06 bulk mark, and the FR-04 pending-query gates.
func TestPGListenRawMediaEnrichment(t *testing.T) {
	db := testDB(t)
	_, ctx := seedTenant(t, db)
	s := pg.NewPGListenRawMessageStore(db)
	agentID := uuid.New()

	cutoff := time.Now().UTC().Truncate(time.Second)

	fresh := newMediaEnrichRow(agentID, "[From: Alice]\n<media:image>\ncaption", []store.RawMediaRef{imageRef("f1")})
	histBare := newMediaEnrichRow(agentID, "[From: Bob]\n<media:image>", []store.RawMediaRef{imageRef("h1")})
	histEnriched := newMediaEnrichRow(agentID, "[From: Carol]\n<media:image>\n<description>already described</description>", []store.RawMediaRef{imageRef("h2")})
	histDoc := newMediaEnrichRow(agentID, "[From: Dave]\n<media:document>", []store.RawMediaRef{{MediaID: "h3", FilePath: "/tmp/h3.pdf", MediaType: "document", ContentType: "application/pdf", FileName: "h3.pdf"}})
	textOnly := newMediaEnrichRow(agentID, "plain text", nil)

	if err := s.AppendBatch(ctx, []store.ListenRawMessage{fresh, histBare, histEnriched, histDoc, textOnly}); err != nil {
		t.Fatalf("AppendBatch: %v", err)
	}
	setPGCreatedAt(t, db, fresh.ID, cutoff.Add(time.Hour))
	setPGCreatedAt(t, db, histBare.ID, cutoff.Add(-24*time.Hour))
	setPGCreatedAt(t, db, histEnriched.ID, cutoff.Add(-24*time.Hour))
	setPGCreatedAt(t, db, histDoc.ID, cutoff.Add(-24*time.Hour))
	setPGCreatedAt(t, db, textOnly.ID, cutoff.Add(-24*time.Hour))

	t.Run("fresh poll selects only fresh unanalyzed media rows", func(t *testing.T) {
		got, err := s.ListPendingMediaEnrichment(ctx, store.MediaEnrichFilter{Cutoff: cutoff, MaxRows: 50})
		if err != nil {
			t.Fatalf("ListPendingMediaEnrichment fresh: %v", err)
		}
		if len(got) != 1 || got[0].ID != fresh.ID {
			t.Fatalf("fresh poll: want only %s, got %d rows", fresh.ID, len(got))
		}
	})

	t.Run("backfill poll selects bare-tag historical rows regardless of pass-through mark", func(t *testing.T) {
		got, err := s.ListPendingMediaEnrichment(ctx, store.MediaEnrichFilter{Cutoff: cutoff, Backfill: true, MaxRows: 50})
		if err != nil {
			t.Fatalf("ListPendingMediaEnrichment backfill: %v", err)
		}
		if len(got) != 1 || got[0].ID != histBare.ID {
			t.Fatalf("backfill poll: want only bare-tag historical %s, got %d rows", histBare.ID, len(got))
		}
		// Bulk-marked rows stay eligible (FR-06: backfill does not filter
		// media_analyzed_at IS NULL).
		if _, err := s.MarkMediaAnalyzedByIDs(ctx, []uuid.UUID{histBare.ID}); err != nil {
			t.Fatalf("MarkMediaAnalyzedByIDs: %v", err)
		}
		got, err = s.ListPendingMediaEnrichment(ctx, store.MediaEnrichFilter{Cutoff: cutoff, Backfill: true, MaxRows: 50})
		if err != nil {
			t.Fatalf("ListPendingMediaEnrichment backfill after mark: %v", err)
		}
		if len(got) != 1 || got[0].ID != histBare.ID {
			t.Fatalf("backfill poll after mark: bare-tag row must stay eligible, got %d rows", len(got))
		}
	})

	t.Run("MarkMediaEnriched writes body + analyzed + pipeline reset", func(t *testing.T) {
		// Simulate a row already extracted + embedded (backfill case).
		if _, err := db.Exec(`UPDATE listen_raw_messages
			SET processed_at = NOW(), extraction_status = 'extracted',
			    extraction_error = 'old', embedded_at = NOW()
			WHERE id = $1`, fresh.ID); err != nil {
			t.Fatalf("seed processed row: %v", err)
		}
		newBody := "<media:image>\n<description>a whiteboard</description>"
		if err := s.MarkMediaEnriched(ctx, fresh.ID, newBody); err != nil {
			t.Fatalf("MarkMediaEnriched: %v", err)
		}
		var body, status string
		var processed, embedded, analyzed, extractErr sql.NullTime
		if err := db.QueryRow(`SELECT body, extraction_status, extraction_error, processed_at, embedded_at, media_analyzed_at
			FROM listen_raw_messages WHERE id = $1`, fresh.ID).
			Scan(&body, &status, &extractErr, &processed, &embedded, &analyzed); err != nil {
			t.Fatalf("query row: %v", err)
		}
		if body != newBody {
			t.Errorf("body: got %q want %q", body, newBody)
		}
		if !analyzed.Valid {
			t.Error("media_analyzed_at must be set")
		}
		if status != store.ExtractionStatusPending {
			t.Errorf("extraction_status: got %q want pending", status)
		}
		if extractErr.Valid {
			t.Errorf("extraction_error must be NULL, got %v", extractErr)
		}
		if processed.Valid {
			t.Error("processed_at must be NULL")
		}
		if embedded.Valid {
			t.Error("embedded_at must be NULL")
		}
	})

	t.Run("MarkMediaAnalyzedBefore marks only historical media rows", func(t *testing.T) {
		n, err := s.MarkMediaAnalyzedBefore(ctx, cutoff)
		if err != nil {
			t.Fatalf("MarkMediaAnalyzedBefore: %v", err)
		}
		// Pre-cutoff media rows still unanalyzed: histDoc and histEnriched
		// (histBare was marked above). textOnly (no media) and fresh excluded.
		if n < 1 {
			t.Fatalf("bulk mark affected %d, want ≥1", n)
		}
		var body string
		if err := db.QueryRow("SELECT body FROM listen_raw_messages WHERE id = $1", histDoc.ID).Scan(&body); err != nil {
			t.Fatalf("query histDoc: %v", err)
		}
		if body != "[From: Dave]\n<media:document>" {
			t.Errorf("bulk mark must not touch body, got %q", body)
		}
	})

	t.Run("FR-04 gates hide unanalyzed media rows from pending queries", func(t *testing.T) {
		newMedia := newMediaEnrichRow(agentID, "<media:image>", []store.RawMediaRef{imageRef("g1")})
		if err := s.AppendBatch(ctx, []store.ListenRawMessage{newMedia}); err != nil {
			t.Fatalf("AppendBatch newMedia: %v", err)
		}
		setPGCreatedAt(t, db, newMedia.ID, cutoff.Add(2*time.Hour))

		pending, err := s.ListPending(ctx, agentID.String(), "graph-1", 50)
		if err != nil {
			t.Fatalf("ListPending: %v", err)
		}
		for _, m := range pending {
			if m.ID == newMedia.ID {
				t.Fatal("unanalyzed media row must be invisible to ListPending")
			}
		}
		emb, err := s.ListPendingEmbeddings(ctx, agentID.String(), "graph-1", 50)
		if err != nil {
			t.Fatalf("ListPendingEmbeddings: %v", err)
		}
		for _, m := range emb {
			if m.ID == newMedia.ID {
				t.Fatal("unanalyzed media row must be invisible to ListPendingEmbeddings")
			}
		}
		// Text-only rows stay visible immediately (no regression).
		found := false
		for _, m := range pending {
			if m.ID == textOnly.ID {
				found = true
			}
		}
		if !found {
			t.Fatal("text-only row must be pending immediately")
		}

		// After pass-through marking, the media row becomes visible.
		if _, err := s.MarkMediaAnalyzedByIDs(ctx, []uuid.UUID{newMedia.ID}); err != nil {
			t.Fatalf("MarkMediaAnalyzedByIDs: %v", err)
		}
		pending, err = s.ListPending(ctx, agentID.String(), "graph-1", 50)
		if err != nil {
			t.Fatalf("ListPending after mark: %v", err)
		}
		visible := false
		for _, m := range pending {
			if m.ID == newMedia.ID {
				visible = true
			}
		}
		if !visible {
			t.Fatal("marked media row must be visible to ListPending")
		}
	})
}

// TestPGListenRawMediaEnrichmentLegacyNullRefs pins the legacy data shape: rows
// written before the nil-normalization fix carry media_refs = JSON null. They
// must count as NO media for the FR-04 gates (pending immediately) and must
// never be selected by the enrichment poll.
func TestPGListenRawMediaEnrichmentLegacyNullRefs(t *testing.T) {
	db := testDB(t)
	_, ctx := seedTenant(t, db)
	s := pg.NewPGListenRawMessageStore(db)
	agentID := uuid.New()

	legacy := newMediaEnrichRow(agentID, "legacy text-only row", nil)
	if err := s.AppendBatch(ctx, []store.ListenRawMessage{legacy}); err != nil {
		t.Fatalf("AppendBatch: %v", err)
	}
	if _, err := db.Exec(`UPDATE listen_raw_messages SET media_refs = 'null'::jsonb WHERE id = $1`, legacy.ID); err != nil {
		t.Fatalf("set legacy media_refs: %v", err)
	}

	pending, err := s.ListPending(ctx, agentID.String(), "graph-1", 10)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(pending) != 1 || pending[0].ID != legacy.ID {
		t.Fatalf("legacy null-refs row must be pending immediately (gate treats null as no-media), got %d rows", len(pending))
	}

	got, err := s.ListPendingMediaEnrichment(ctx, store.MediaEnrichFilter{Cutoff: time.Time{}, MaxRows: 50})
	if err != nil {
		t.Fatalf("ListPendingMediaEnrichment: %v", err)
	}
	for _, m := range got {
		if m.ID == legacy.ID {
			t.Fatal("legacy null-refs row must never be selected by the enrichment poll")
		}
	}
}
