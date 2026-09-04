//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SQLiteListenRawMessageStore implements store.ListenRawMessageStore backed by SQLite.
type SQLiteListenRawMessageStore struct {
	db *sql.DB
}

// NewSQLiteListenRawMessageStore creates a new SQLiteListenRawMessageStore.
func NewSQLiteListenRawMessageStore(db *sql.DB) *SQLiteListenRawMessageStore {
	return &SQLiteListenRawMessageStore{db: db}
}

func (s *SQLiteListenRawMessageStore) AppendBatch(ctx context.Context, msgs []store.ListenRawMessage) error {
	if len(msgs) == 0 {
		return nil
	}

	const cols = 13
	placeholders := make([]string, len(msgs))
	args := make([]any, 0, len(msgs)*cols)
	now := time.Now()
	tid := tenantIDForInsert(ctx)

	for i := range msgs {
		if msgs[i].ID == uuid.Nil {
			msgs[i].ID = uuid.Must(uuid.NewV7())
		}
		// Normalize nil → empty array so text-only rows store '[]' (a nil slice
		// marshals to JSON null, which the SRS 014 media gates treat as no-media).
		mediaRefs := msgs[i].MediaRefs
		if mediaRefs == nil {
			mediaRefs = []store.RawMediaRef{}
		}
		mediaJSON, _ := json.Marshal(mediaRefs)
		placeholders[i] = "(?,?,?,?,?,?,?,?,?,?,?,?,?)"
		args = append(args, msgs[i].ID, msgs[i].ChannelName, msgs[i].ChatID,
			msgs[i].ChatName, msgs[i].GraphID, msgs[i].Sender, msgs[i].SenderID,
			msgs[i].Body, msgs[i].MsgTimestamp, msgs[i].AgentID, now, tid, string(mediaJSON))
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO listen_raw_messages (id, channel_name, chat_id, chat_name, graph_id, sender, sender_id, body, msg_timestamp, agent_id, created_at, tenant_id, media_refs)
					 VALUES `+strings.Join(placeholders, ","),
		args...,
	)
	return err
}

func scanRawMessages(rows *sql.Rows) ([]store.ListenRawMessage, error) {
	var result []store.ListenRawMessage
	for rows.Next() {
		var m store.ListenRawMessage
		var processedAt sql.NullString
		var createdAt sql.NullString
		var msgTimestamp sql.NullString
		var mediaRefsJSON string
		var extractionStatus sql.NullString
		var extractionError sql.NullString
		var extractionAttempts sql.NullInt64
		var lastAttemptedAt sql.NullString
		if err := rows.Scan(&m.ID, &m.ChannelName, &m.ChatID, &m.ChatName,
			&m.GraphID, &m.Sender, &m.SenderID, &m.Body,
			&msgTimestamp, &m.AgentID, &createdAt, &processedAt, &mediaRefsJSON,
			&extractionStatus, &extractionError, &extractionAttempts, &lastAttemptedAt); err != nil {
			return nil, err
		}
		if msgTimestamp.Valid {
			t, _ := time.Parse(time.RFC3339Nano, msgTimestamp.String)
			m.MsgTimestamp = t
		}
		if createdAt.Valid {
			t, _ := time.Parse(time.RFC3339Nano, createdAt.String)
			m.CreatedAt = t
		}
		if processedAt.Valid && processedAt.String != "" {
			t, _ := time.Parse(time.RFC3339Nano, processedAt.String)
			m.ProcessedAt = &t
		}
		if mediaRefsJSON != "" && mediaRefsJSON != "[]" {
			_ = json.Unmarshal([]byte(mediaRefsJSON), &m.MediaRefs)
		}
		if extractionStatus.Valid {
			m.ExtractionStatus = extractionStatus.String
		}
		if extractionError.Valid {
			m.ExtractionError = extractionError.String
		}
		if extractionAttempts.Valid {
			m.ExtractionAttempts = int(extractionAttempts.Int64)
		}
		if lastAttemptedAt.Valid && lastAttemptedAt.String != "" {
			t, _ := time.Parse(time.RFC3339Nano, lastAttemptedAt.String)
			m.LastAttemptedAt = &t
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

func (s *SQLiteListenRawMessageStore) ListPending(ctx context.Context, agentID, graphID string, maxRows int) ([]store.ListenRawMessage, error) {
	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return nil, err
	}
	// Bind order must match placeholder order: the tenant scope clause sits
	// BEFORE `LIMIT ?` in the SQL, so tArgs bind before maxRows. (Binding
	// maxRows first fed the tenant UUID into LIMIT — "datatype mismatch".)
	args := append(append([]any{agentID, graphID,
		store.ExtractionStatusPending, store.ExtractionStatusFailed, store.MaxExtractionAttempts}, tArgs...), maxRows)
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, channel_name, chat_id, chat_name, graph_id, sender, sender_id, body, msg_timestamp, agent_id, created_at, processed_at, media_refs,
		        extraction_status, extraction_error, extraction_attempts, last_attempted_at
		 FROM listen_raw_messages
		 WHERE agent_id = ? AND graph_id = ?
		   AND (extraction_status = ? OR (extraction_status = ? AND extraction_attempts < ?))
		   AND (media_refs IN ('[]', 'null') OR media_analyzed_at IS NOT NULL)`+tClause+`
		 ORDER BY msg_timestamp DESC
		 LIMIT ?`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRawMessages(rows)
}

func (s *SQLiteListenRawMessageStore) MarkProcessed(ctx context.Context, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	placeholders := make([]string, len(ids))
	args := make([]any, 0, len(ids)+2)
	args = append(args, now, store.ExtractionStatusExtracted)
	for i, id := range ids {
		placeholders[i] = "?"
		args = append(args, id)
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE listen_raw_messages SET processed_at = ?, extraction_status = ?, extraction_error = NULL WHERE id IN (`+strings.Join(placeholders, ",")+`)`,
		args...,
	)
	return err
}

func (s *SQLiteListenRawMessageStore) MarkFailed(ctx context.Context, ids []uuid.UUID, extractErr string) error {
	if len(ids) == 0 {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	placeholders := make([]string, len(ids))
	args := make([]any, 0, len(ids)+3)
	args = append(args, store.ExtractionStatusFailed, extractErr, now)
	for i, id := range ids {
		placeholders[i] = "?"
		args = append(args, id)
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE listen_raw_messages
		 SET extraction_status = ?, extraction_error = ?,
		     extraction_attempts = extraction_attempts + 1,
		     last_attempted_at = ?, processed_at = ?
		 WHERE id IN (`+strings.Join(placeholders, ",")+`)`,
		args...,
	)
	return err
}

func (s *SQLiteListenRawMessageStore) ListPendingGroups(ctx context.Context) ([]store.ListenRawMessageGroup, error) {
	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return nil, err
	}
	args := append([]any{store.ExtractionStatusPending, store.ExtractionStatusFailed, store.MaxExtractionAttempts}, tArgs...)
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT agent_id, graph_id
		 FROM listen_raw_messages
		 WHERE (extraction_status = ? OR (extraction_status = ? AND extraction_attempts < ?))
		   AND (media_refs IN ('[]', 'null') OR media_analyzed_at IS NOT NULL)`+tClause,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []store.ListenRawMessageGroup
	for rows.Next() {
		var g store.ListenRawMessageGroup
		if err := rows.Scan(&g.AgentID, &g.GraphID); err != nil {
			return nil, err
		}
		result = append(result, g)
	}
	return result, rows.Err()
}

func (s *SQLiteListenRawMessageStore) ListPendingEmbeddingGroups(ctx context.Context) ([]store.ListenRawMessageGroup, error) {
	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT agent_id, graph_id FROM listen_raw_messages WHERE embedded_at IS NULL
		   AND (media_refs IN ('[]', 'null') OR media_analyzed_at IS NOT NULL)`+tClause,
		tArgs...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []store.ListenRawMessageGroup
	for rows.Next() {
		var g store.ListenRawMessageGroup
		if err := rows.Scan(&g.AgentID, &g.GraphID); err != nil {
			return nil, err
		}
		result = append(result, g)
	}
	return result, rows.Err()
}

func (s *SQLiteListenRawMessageStore) ListPendingEmbeddings(ctx context.Context, agentID, graphID string, maxRows int) ([]store.ListenRawMessage, error) {
	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, channel_name, chat_id, chat_name, graph_id, sender, sender_id, body, msg_timestamp, agent_id, created_at, processed_at, media_refs,
		        extraction_status, extraction_error, extraction_attempts, last_attempted_at
		 FROM listen_raw_messages
		 WHERE agent_id = ? AND graph_id = ? AND embedded_at IS NULL
		   AND (media_refs IN ('[]', 'null') OR media_analyzed_at IS NOT NULL)`+tClause+`
		 ORDER BY msg_timestamp ASC
		 LIMIT ?`,
		// tArgs bind before maxRows — the tenant clause sits before LIMIT ? above.
		append(append([]any{agentID, graphID}, tArgs...), maxRows)...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRawMessages(rows)
}

func (s *SQLiteListenRawMessageStore) MarkEmbedded(ctx context.Context, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	placeholders := make([]string, len(ids))
	args := make([]any, 0, len(ids)+1)
	args = append(args, now)
	for i, id := range ids {
		placeholders[i] = "?"
		args = append(args, id)
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE listen_raw_messages SET embedded_at = ? WHERE id IN (`+strings.Join(placeholders, ",")+`)`,
		args...,
	)
	return err
}


func (s *SQLiteListenRawMessageStore) ResetProcessed(ctx context.Context, agentID, graphID string) (int64, error) {
	var conditions []string
	var args []any

	if agentID != "" {
		conditions = append(conditions, "agent_id = ?")
		args = append(args, agentID)
	}
	if graphID != "" {
		conditions = append(conditions, "graph_id = ?")
		args = append(args, graphID)
	}
	conditions = append(conditions, "processed_at IS NOT NULL")

	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return 0, err
	}

	where := strings.Join(conditions, " AND ")
	args = append(args, store.ExtractionStatusPending)
	q := `UPDATE listen_raw_messages SET processed_at = NULL, extraction_status = ?, extraction_error = NULL WHERE ` + where + tClause
	args = append(args, tArgs...)

	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *SQLiteListenRawMessageStore) ResetProcessedByIDs(ctx context.Context, ids []uuid.UUID) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, 0, len(ids)+2)
	for i, id := range ids {
		placeholders[i] = "?"
		args = append(args, id)
	}
	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return 0, err
	}
	args = append(args, store.ExtractionStatusPending)
	args = append(args, tArgs...)
	q := `UPDATE listen_raw_messages SET processed_at = NULL, extraction_status = ?, extraction_error = NULL WHERE id IN (` + strings.Join(placeholders, ",") + `) AND processed_at IS NOT NULL` + tClause
	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *SQLiteListenRawMessageStore) UpdateScope(ctx context.Context, ids []uuid.UUID, agentID, graphID string) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	// SET clause: always reset extraction + embedding state (scope change invalidates
	// both prior KG extraction and prior chunk embedding); append agent_id/graph_id
	// when provided. Resetting embedded_at re-queues the message for the embedding
	// worker under the new (agent_id, graph_id) so fresh chunks land in the new scope.
	setParts := []string{"processed_at = NULL", "extraction_status = ?", "extraction_error = NULL", "embedded_at = NULL"}
	args := make([]any, 0, len(ids)+3)
	args = append(args, store.ExtractionStatusPending)
	if agentID != "" {
		setParts = append(setParts, "agent_id = ?")
		args = append(args, agentID)
	}
	if graphID != "" {
		setParts = append(setParts, "graph_id = ?")
		args = append(args, graphID)
	}
	placeholders := make([]string, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args = append(args, id)
	}
	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return 0, err
	}
	args = append(args, tArgs...)
	q := `UPDATE listen_raw_messages SET ` + strings.Join(setParts, ", ") +
		` WHERE id IN (` + strings.Join(placeholders, ",") + `)` + tClause
	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *SQLiteListenRawMessageStore) ResetEmbeddedByIDs(ctx context.Context, ids []uuid.UUID) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, 0, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args = append(args, id)
	}
	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return 0, err
	}
	args = append(args, tArgs...)
	q := `UPDATE listen_raw_messages SET embedded_at = NULL WHERE id IN (` + strings.Join(placeholders, ",") + `)` + tClause
	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *SQLiteListenRawMessageStore) List(ctx context.Context, opts store.ListenRawMessageListOpts) ([]store.ListenRawMessage, int, error) {
	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return nil, 0, err
	}

	var conditions []string
	var args []any

	if opts.ChannelName != "" {
		conditions = append(conditions, "channel_name = ?")
		args = append(args, opts.ChannelName)
	}
	if opts.ChatID != "" {
		conditions = append(conditions, "chat_id = ?")
		args = append(args, opts.ChatID)
	}
	if opts.AgentID != "" {
		conditions = append(conditions, "agent_id = ?")
		args = append(args, opts.AgentID)
	}
	if opts.GraphID != "" {
		conditions = append(conditions, "graph_id = ?")
		args = append(args, opts.GraphID)
	}
	// Substring text filters (SRS 008 FR-00/FR-04). Added to the shared `conditions`
	// so they apply to both COUNT and data. SQLite uses positional ?, so each OR'd
	// column binds its own %<text>% arg. LIKE is ASCII case-insensitive by default.
	if opts.Chat != "" {
		pat := "%" + opts.Chat + "%"
		conditions = append(conditions, "(chat_name LIKE ? OR chat_id LIKE ?)")
		args = append(args, pat, pat)
	}
	if opts.Sender != "" {
		pat := "%" + opts.Sender + "%"
		conditions = append(conditions, "(sender LIKE ? OR sender_id LIKE ?)")
		args = append(args, pat, pat)
	}
	if opts.Body != "" {
		conditions = append(conditions, "body LIKE ?")
		args = append(args, "%"+opts.Body+"%")
	}
	if opts.Processed != nil {
		if *opts.Processed {
			conditions = append(conditions, "processed_at IS NOT NULL")
		} else {
			conditions = append(conditions, "processed_at IS NULL")
		}
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = " AND " + strings.Join(conditions, " AND ")
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}

	// Count total.
	var total int
	countArgs := append(tArgs, args...)
	err = s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM listen_raw_messages WHERE 1=1`+tClause+whereClause,
		countArgs...,
	).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	// Fetch page.
	offset := opts.Offset
	if offset < 0 {
		offset = 0
	}
	pageArgs := append(tArgs, args...)
	pageArgs = append(pageArgs, limit, offset)

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, channel_name, chat_id, chat_name, graph_id, sender, sender_id, body, msg_timestamp, agent_id, created_at, processed_at, media_refs,
		        extraction_status, extraction_error, extraction_attempts, last_attempted_at
		 FROM listen_raw_messages WHERE 1=1`+tClause+whereClause+`
		 ORDER BY created_at DESC
		 LIMIT ? OFFSET ?`,
		pageArgs...,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	result, scanErr := scanRawMessages(rows)
	return result, total, scanErr
}

func (s *SQLiteListenRawMessageStore) ExtractionStats(ctx context.Context) (map[string]int, error) {
	return nil, nil
}

func (s *SQLiteListenRawMessageStore) EmbeddingStats(ctx context.Context) (int, int, error) {
	return 0, 0, nil
}

func (s *SQLiteListenRawMessageStore) ListAbandonedGroups(ctx context.Context) ([]store.ListenRawMessageGroup, error) {
	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return nil, err
	}
	args := append([]any{store.ExtractionStatusFailed, store.MaxExtractionAttempts}, tArgs...)
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT agent_id, graph_id
		 FROM listen_raw_messages
		 WHERE extraction_status = ? AND extraction_attempts >= ?`+tClause,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []store.ListenRawMessageGroup
	for rows.Next() {
		var g store.ListenRawMessageGroup
		if err := rows.Scan(&g.AgentID, &g.GraphID); err != nil {
			return nil, err
		}
		result = append(result, g)
	}
	return result, rows.Err()
}

func (s *SQLiteListenRawMessageStore) ListAbandonedIDs(ctx context.Context, agentID, graphID string, maxRows int) ([]uuid.UUID, error) {
	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return nil, err
	}
	// tArgs bind before maxRows — the tenant clause sits before LIMIT ? above.
	args := append(append([]any{agentID, graphID, store.ExtractionStatusFailed, store.MaxExtractionAttempts}, tArgs...), maxRows)
	rows, err := s.db.QueryContext(ctx,
		`SELECT id FROM listen_raw_messages
		 WHERE agent_id = ? AND graph_id = ?
		   AND extraction_status = ? AND extraction_attempts >= ?`+tClause+`
		 LIMIT ?`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ListPendingMediaEnrichment returns media-bearing rows awaiting the enrichment
// attempt (SRS 014 FR-02). Fresh mode: created_at >= cutoff. Backfill mode:
// created_at < cutoff AND still carrying the bare <media:image> tag with no
// <description> block (idempotent — enriched/failed rows never re-match).
func (s *SQLiteListenRawMessageStore) ListPendingMediaEnrichment(ctx context.Context, filter store.MediaEnrichFilter) ([]store.ListenRawMessage, error) {
	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return nil, err
	}
	cutoff := filter.Cutoff.UTC().Format(time.RFC3339Nano)
	var args []any
	where := " WHERE media_refs NOT IN ('[]', 'null')"
	if filter.Backfill {
		// Backfill eligibility is body-pattern based and deliberately does NOT
		// require media_analyzed_at IS NULL: rows bulk pass-through-marked at
		// first registration (backfill off) must remain backfill-eligible after
		// the operator flips backfill on. Already-enriched rows are excluded by
		// the NOT LIKE description guard (idempotent, never re-billed).
		where += " AND created_at < ? AND body LIKE ? AND body NOT LIKE ?"
		args = append(args, cutoff, "%<media:image>%", "%<description>%")
	} else {
		where += " AND media_analyzed_at IS NULL AND created_at >= ?"
		args = append(args, cutoff)
	}
	args = append(args, tArgs...)
	args = append(args, filter.MaxRows)
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, channel_name, chat_id, chat_name, graph_id, sender, sender_id, body, msg_timestamp, agent_id, created_at, processed_at, media_refs,
		        extraction_status, extraction_error, extraction_attempts, last_attempted_at
		 FROM listen_raw_messages`+where+tClause+`
		 ORDER BY created_at ASC
		 LIMIT ?`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRawMessages(rows)
}

// MarkMediaEnriched writes the enriched body and marks the row analyzed in one
// UPDATE, resetting pipeline state (mirroring UpdateScope) so already-embedded
// backfilled rows re-embed with the description (SRS 014 FR-02/FR-06).
func (s *SQLiteListenRawMessageStore) MarkMediaEnriched(ctx context.Context, id uuid.UUID, body string) error {
	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	args := append([]any{body, now, store.ExtractionStatusPending, id}, tArgs...)
	_, err = s.db.ExecContext(ctx,
		`UPDATE listen_raw_messages
		 SET body = ?,
		     media_analyzed_at = ?,
		     processed_at = NULL,
		     extraction_status = ?,
		     extraction_error = NULL,
		     embedded_at = NULL
		 WHERE id = ?`+tClause,
		args...,
	)
	return err
}

// MarkMediaAnalyzedByIDs pass-through marks rows analyzed without touching body
// or pipeline state (no-provider / disabled / non-image / pre-cutoff rows), so
// the FR-04 pending gates keep draining (SRS 014 FR-02).
func (s *SQLiteListenRawMessageStore) MarkMediaAnalyzedByIDs(ctx context.Context, ids []uuid.UUID) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, 0, len(ids)+1)
	args = append(args, time.Now().UTC().Format(time.RFC3339Nano))
	for i, id := range ids {
		placeholders[i] = "?"
		args = append(args, id)
	}
	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return 0, err
	}
	args = append(args, tArgs...)
	q := `UPDATE listen_raw_messages SET media_analyzed_at = ?
	      WHERE id IN (` + strings.Join(placeholders, ",") + `) AND media_analyzed_at IS NULL` + tClause
	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// MarkMediaAnalyzedBefore bulk pass-through marks ALL media-bearing rows created
// before the cutoff — the one-time first-registration mark (SRS 014 FR-06).
// Body is untouched so the rows stay backfill-eligible by body pattern.
func (s *SQLiteListenRawMessageStore) MarkMediaAnalyzedBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return 0, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	cutoffStr := cutoff.UTC().Format(time.RFC3339Nano)
	q := `UPDATE listen_raw_messages SET media_analyzed_at = ?
	      WHERE media_refs NOT IN ('[]', 'null') AND media_analyzed_at IS NULL AND created_at < ?` + tClause
	res, err := s.db.ExecContext(ctx, q, append([]any{now, cutoffStr}, tArgs...)...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
