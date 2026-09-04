//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// setListenRawCreatedAt forces a fixed ISO-8601 created_at so cutoff string
// comparisons in the enrichment poll are deterministic regardless of the
// driver's time serialization.
func setListenRawCreatedAt(t *testing.T, db *sql.DB, id uuid.UUID, ts string) {
	t.Helper()
	if _, err := db.Exec("UPDATE listen_raw_messages SET created_at = ? WHERE id = ?", ts, id); err != nil {
		t.Fatalf("set created_at %s: %v", id, err)
	}
}

func mediaMsg(body string, refs []store.RawMediaRef) store.ListenRawMessage {
	if refs == nil {
		refs = []store.RawMediaRef{{MediaID: "m1", FilePath: "/tmp/x.jpg", MediaType: "image", ContentType: "image/jpeg", FileName: "x.jpg", FileSize: 100}}
	}
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
		AgentID:      "agent-1",
		MediaRefs:    refs,
	}
}

// TestSQLiteListenRawMessageStore_MediaEnrichmentPoll verifies the enrichment
// worker poll: fresh mode returns media-bearing rows at/after the cutoff;
// backfill mode returns only pre-cutoff rows still carrying the bare
// <media:image> tag with no <description> block (idempotent body-pattern
// eligibility). See SRS 014 FR-02/FR-06.
func TestSQLiteListenRawMessageStore_MediaEnrichmentPoll(t *testing.T) {
	s, ctx, db := newListenRawTestStore(t)

	cutoff := "2026-09-04T00:00:00Z"

	fresh := mediaMsg("[From: Alice]\n<media:image>\ncaption", nil)
	histBare := mediaMsg("[From: Bob]\n<media:image>", nil)
	histEnriched := mediaMsg("[From: Carol]\n<media:image>\n<description>already described</description>", nil)
	histNoImageTag := mediaMsg("[From: Dave]\n<media:document>", []store.RawMediaRef{{MediaID: "m2", FilePath: "/tmp/x.pdf", MediaType: "document", ContentType: "application/pdf", FileName: "x.pdf"}})
	textOnly := newListenRawMsg("agent-1", "graph-1")
	textOnly.Body = "plain text"

	for _, m := range []store.ListenRawMessage{fresh, histBare, histEnriched, histNoImageTag, textOnly} {
		if err := s.AppendBatch(ctx, []store.ListenRawMessage{m}); err != nil {
			t.Fatalf("AppendBatch: %v", err)
		}
	}
	setListenRawCreatedAt(t, db, fresh.ID, "2026-09-04T12:00:00Z")
	setListenRawCreatedAt(t, db, histBare.ID, "2026-08-01T00:00:00Z")
	setListenRawCreatedAt(t, db, histEnriched.ID, "2026-08-01T00:00:00Z")
	setListenRawCreatedAt(t, db, histNoImageTag.ID, "2026-08-01T00:00:00Z")
	setListenRawCreatedAt(t, db, textOnly.ID, "2026-08-01T00:00:00Z")

	cut, err := time.Parse(time.RFC3339, cutoff)
	if err != nil {
		t.Fatalf("parse cutoff: %v", err)
	}

	got, err := s.ListPendingMediaEnrichment(ctx, store.MediaEnrichFilter{Cutoff: cut, MaxRows: 50})
	if err != nil {
		t.Fatalf("ListPendingMediaEnrichment fresh: %v", err)
	}
	if len(got) != 1 || got[0].ID != fresh.ID {
		t.Fatalf("fresh poll: want only fresh media row %s, got %d rows", fresh.ID, len(got))
	}

	got, err = s.ListPendingMediaEnrichment(ctx, store.MediaEnrichFilter{Cutoff: cut, Backfill: true, MaxRows: 50})
	if err != nil {
		t.Fatalf("ListPendingMediaEnrichment backfill: %v", err)
	}
	if len(got) != 1 || got[0].ID != histBare.ID {
		t.Fatalf("backfill poll: want only bare-tag historical row %s, got %d rows", histBare.ID, len(got))
	}

	// Bulk-marked (pass-through) rows stay backfill-eligible: the backfill poll
	// must NOT require media_analyzed_at IS NULL (SRS 014 FR-06).
	if _, err := s.MarkMediaAnalyzedByIDs(ctx, []uuid.UUID{histBare.ID}); err != nil {
		t.Fatalf("MarkMediaAnalyzedByIDs: %v", err)
	}
	got, err = s.ListPendingMediaEnrichment(ctx, store.MediaEnrichFilter{Cutoff: cut, Backfill: true, MaxRows: 50})
	if err != nil {
		t.Fatalf("ListPendingMediaEnrichment backfill after mark: %v", err)
	}
	if len(got) != 1 || got[0].ID != histBare.ID {
		t.Fatalf("backfill poll after pass-through mark: bare-tag row must stay eligible, got %d rows", len(got))
	}
}

// TestSQLiteListenRawMessageStore_MediaEnrichmentPoll_TenantIsolation verifies
// the enrichment poll is tenant-scoped. See SRS 014 FR-02.
func TestSQLiteListenRawMessageStore_MediaEnrichmentPoll_TenantIsolation(t *testing.T) {
	s, ctxMaster, _ := newListenRawTestStore(t)
	ctxOther := store.WithTenantID(context.Background(), uuid.New())

	m := mediaMsg("<media:image>", nil)
	if err := s.AppendBatch(ctxMaster, []store.ListenRawMessage{m}); err != nil {
		t.Fatalf("AppendBatch: %v", err)
	}

	got, err := s.ListPendingMediaEnrichment(ctxOther, store.MediaEnrichFilter{Cutoff: time.Time{}, MaxRows: 50})
	if err != nil {
		t.Fatalf("ListPendingMediaEnrichment cross-tenant: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("cross-tenant poll must return 0 rows, got %d", len(got))
	}
}

// TestSQLiteListenRawMessageStore_MarkMediaEnriched verifies the single-UPDATE
// enrichment write: body, media_analyzed_at, and the full pipeline state reset
// (processed/extraction/embedding) mirroring UpdateScope semantics. See SRS
// 014 FR-02/FR-06.
func TestSQLiteListenRawMessageStore_MarkMediaEnriched(t *testing.T) {
	s, ctx, db := newListenRawTestStore(t)

	m := mediaMsg("<media:image>", nil)
	if err := s.AppendBatch(ctx, []store.ListenRawMessage{m}); err != nil {
		t.Fatalf("AppendBatch: %v", err)
	}
	// Simulate a row that was already extracted + embedded (backfill case).
	if _, err := db.Exec(`UPDATE listen_raw_messages
		SET processed_at = '2026-08-02T00:00:00Z', extraction_status = 'extracted',
		    extraction_error = 'old error', embedded_at = '2026-08-02T00:00:00Z'
		WHERE id = ?`, m.ID); err != nil {
		t.Fatalf("seed processed row: %v", err)
	}

	newBody := "<media:image>\n<description>a whiteboard</description>"
	if err := s.MarkMediaEnriched(ctx, m.ID, newBody); err != nil {
		t.Fatalf("MarkMediaEnriched: %v", err)
	}

	var body, status string
	var processed, embedded, analyzed, extractErr sql.NullString
	err := db.QueryRow(`SELECT body, extraction_status, extraction_error, processed_at, embedded_at, media_analyzed_at
		FROM listen_raw_messages WHERE id = ?`, m.ID).
		Scan(&body, &status, &extractErr, &processed, &embedded, &analyzed)
	if err != nil {
		t.Fatalf("query row: %v", err)
	}
	if body != newBody {
		t.Errorf("body: got %q want %q", body, newBody)
	}
	if !analyzed.Valid || analyzed.String == "" {
		t.Errorf("media_analyzed_at must be set, got %v", analyzed)
	}
	if status != store.ExtractionStatusPending {
		t.Errorf("extraction_status: got %q want %q", status, store.ExtractionStatusPending)
	}
	if extractErr.Valid && extractErr.String != "" {
		t.Errorf("extraction_error must be NULL, got %q", extractErr.String)
	}
	if processed.Valid {
		t.Errorf("processed_at must be NULL, got %q", processed.String)
	}
	if embedded.Valid {
		t.Errorf("embedded_at must be NULL, got %q", embedded.String)
	}
}

// TestSQLiteListenRawMessageStore_MarkMediaAnalyzedByIDs verifies pass-through
// marking: media_analyzed_at set, body + pipeline state untouched, only rows
// still NULL are affected. See SRS 014 FR-02.
func TestSQLiteListenRawMessageStore_MarkMediaAnalyzedByIDs(t *testing.T) {
	s, ctx, db := newListenRawTestStore(t)

	a := mediaMsg("<media:image>", nil)
	b := mediaMsg("<media:image>", nil)
	if err := s.AppendBatch(ctx, []store.ListenRawMessage{a, b}); err != nil {
		t.Fatalf("AppendBatch: %v", err)
	}
	// Row b already analyzed — must not be re-touched (idempotent).
	firstMark := "2026-09-01T00:00:00Z"
	if _, err := db.Exec("UPDATE listen_raw_messages SET media_analyzed_at = ? WHERE id = ?", firstMark, b.ID); err != nil {
		t.Fatalf("seed analyzed row: %v", err)
	}

	n, err := s.MarkMediaAnalyzedByIDs(ctx, []uuid.UUID{a.ID, b.ID})
	if err != nil {
		t.Fatalf("MarkMediaAnalyzedByIDs: %v", err)
	}
	if n != 1 {
		t.Errorf("affected: got %d want 1 (already-analyzed row skipped)", n)
	}

	var bMark string
	if err := db.QueryRow("SELECT media_analyzed_at FROM listen_raw_messages WHERE id = ?", b.ID).Scan(&bMark); err != nil {
		t.Fatalf("query b: %v", err)
	}
	if bMark != firstMark {
		t.Errorf("existing media_analyzed_at must be preserved: got %q want %q", bMark, firstMark)
	}

	var aBody string
	var aProcessed sql.NullString
	if err := db.QueryRow("SELECT body, processed_at FROM listen_raw_messages WHERE id = ?", a.ID).Scan(&aBody, &aProcessed); err != nil {
		t.Fatalf("query a: %v", err)
	}
	if aBody != "<media:image>" {
		t.Errorf("pass-through must not touch body: got %q", aBody)
	}
	if aProcessed.Valid {
		t.Errorf("pass-through must not touch pipeline state: processed_at = %q", aProcessed.String)
	}
}

// TestSQLiteListenRawMessageStore_MarkMediaAnalyzedBefore verifies the FR-06
// one-time bulk pass-through mark: only media-bearing pre-cutoff rows are
// marked, body untouched (stays backfill-eligible).
func TestSQLiteListenRawMessageStore_MarkMediaAnalyzedBefore(t *testing.T) {
	s, ctx, db := newListenRawTestStore(t)

	cutoff := "2026-09-04T00:00:00Z"
	histMedia := mediaMsg("<media:image>", nil)
	freshMedia := mediaMsg("<media:image>", nil)
	histText := newListenRawMsg("agent-1", "graph-1")
	for _, m := range []store.ListenRawMessage{histMedia, freshMedia, histText} {
		if err := s.AppendBatch(ctx, []store.ListenRawMessage{m}); err != nil {
			t.Fatalf("AppendBatch: %v", err)
		}
	}
	setListenRawCreatedAt(t, db, histMedia.ID, "2026-08-01T00:00:00Z")
	setListenRawCreatedAt(t, db, freshMedia.ID, "2026-09-04T12:00:00Z")
	setListenRawCreatedAt(t, db, histText.ID, "2026-08-01T00:00:00Z")

	cut, _ := time.Parse(time.RFC3339, cutoff)
	n, err := s.MarkMediaAnalyzedBefore(ctx, cut)
	if err != nil {
		t.Fatalf("MarkMediaAnalyzedBefore: %v", err)
	}
	if n != 1 {
		t.Fatalf("affected: got %d want 1 (only historical media row)", n)
	}

	for id, wantSet := range map[uuid.UUID]bool{histMedia.ID: true, freshMedia.ID: false, histText.ID: false} {
		var analyzed sql.NullString
		if err := db.QueryRow("SELECT media_analyzed_at FROM listen_raw_messages WHERE id = ?", id).Scan(&analyzed); err != nil {
			t.Fatalf("query %s: %v", id, err)
		}
		if wantSet && !analyzed.Valid {
			t.Errorf("row %s must be marked analyzed", id)
		}
		if !wantSet && analyzed.Valid {
			t.Errorf("row %s must NOT be marked analyzed", id)
		}
	}

	var body string
	if err := db.QueryRow("SELECT body FROM listen_raw_messages WHERE id = ?", histMedia.ID).Scan(&body); err != nil {
		t.Fatalf("query body: %v", err)
	}
	if body != "<media:image>" {
		t.Errorf("bulk mark must not touch body (backfill eligibility), got %q", body)
	}
}

// TestSQLiteListenRawMessageStore_MediaGateOnPendingLists verifies the FR-04
// gate: media-bearing rows are invisible to the extraction and embedding
// pending queries until media_analyzed_at is set; text-only rows behave as
// before. Mirrors SRS 014 FR-04.
func TestSQLiteListenRawMessageStore_MediaGateOnPendingLists(t *testing.T) {
	s, ctx, _ := newListenRawTestStore(t)

	mediaRow := mediaMsg("<media:image>", nil)
	mediaRow.AgentID, mediaRow.GraphID = "agent-1", "graph-1"
	textRow := newListenRawMsg("agent-1", "graph-1")
	if err := s.AppendBatch(ctx, []store.ListenRawMessage{mediaRow, textRow}); err != nil {
		t.Fatalf("AppendBatch: %v", err)
	}

	// Before analysis the group exists (text row is ungated) but the media row
	// must be invisible to both pending queries.
	pending, err := s.ListPending(ctx, "agent-1", "graph-1", 10)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(pending) != 1 || pending[0].ID != textRow.ID {
		t.Fatalf("ListPending before analysis: want only text row %s (media gated), got %d rows", textRow.ID, len(pending))
	}
	emb, err := s.ListPendingEmbeddings(ctx, "agent-1", "graph-1", 10)
	if err != nil {
		t.Fatalf("ListPendingEmbeddings: %v", err)
	}
	if len(emb) != 1 || emb[0].ID != textRow.ID {
		t.Fatalf("ListPendingEmbeddings before analysis: want only text row %s (media gated), got %d rows", textRow.ID, len(emb))
	}

	// Pass-through mark drains the gate: the media row becomes visible too.
	if _, err := s.MarkMediaAnalyzedByIDs(ctx, []uuid.UUID{mediaRow.ID}); err != nil {
		t.Fatalf("MarkMediaAnalyzedByIDs: %v", err)
	}
	pending, err = s.ListPending(ctx, "agent-1", "graph-1", 10)
	if err != nil {
		t.Fatalf("ListPending after analysis: %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("ListPending after analysis: want 2 (media + text), got %d", len(pending))
	}
	emb, err = s.ListPendingEmbeddings(ctx, "agent-1", "graph-1", 10)
	if err != nil {
		t.Fatalf("ListPendingEmbeddings after analysis: %v", err)
	}
	if len(emb) != 2 {
		t.Fatalf("ListPendingEmbeddings after analysis: want 2 (media + text), got %d", len(emb))
	}
}

// TestSQLiteListenRawMessageStore_MediaGateTextOnlyNoRegression verifies the
// FR-04 predicate is a no-op for text-only rows (media_refs = '[]') — they are
// pending/embeddable exactly as before the gate existed. See SRS 014 FR-04.
func TestSQLiteListenRawMessageStore_MediaGateTextOnlyNoRegression(t *testing.T) {
	s, ctx, _ := newListenRawTestStore(t)

	textRow := newListenRawMsg("agent-1", "graph-1")
	if err := s.AppendBatch(ctx, []store.ListenRawMessage{textRow}); err != nil {
		t.Fatalf("AppendBatch: %v", err)
	}

	pending, err := s.ListPending(ctx, "agent-1", "graph-1", 10)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("text-only row must be pending immediately, got %d", len(pending))
	}
	groups, err := s.ListPendingEmbeddingGroups(ctx)
	if err != nil {
		t.Fatalf("ListPendingEmbeddingGroups: %v", err)
	}
	if len(groups) != 1 || groups[0].AgentID != "agent-1" {
		t.Fatalf("text-only row must be embeddable immediately, got %+v", groups)
	}
}
