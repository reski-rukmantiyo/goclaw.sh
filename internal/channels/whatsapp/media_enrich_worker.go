package whatsapp

import (
	"context"
	"html"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/semaphore"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// Media enrichment worker (SRS 014). Turns persisted WhatsApp images into text
// descriptions via the configured vision LLM and writes them into
// listen_raw_messages.body as <description> blocks next to the bare
// <media:image> tag, so the existing embedding and KG-extraction workers
// consume the description as the single source of truth for media content.
//
// Runs strictly OUT of band from the inbound path (FR-00). Rows are invisible
// to the embed/extract pending queries until media_analyzed_at is set (FR-04),
// so every path here must mark each row exactly once: enrichment success,
// failure marker, or pass-through (no provider / disabled / non-image /
// pre-cutoff with backfill off).

const (
	defaultMediaEnrichPollSec = 30
	mediaEnrichMinPollSec     = 5
	mediaEnrichBatchSize      = 50
	mediaEnrichMaxConcurrent  = 2
	mediaDescriptionMaxRunes  = 2000

	cfgKeyMediaBackfillEnabled = "listen.media_analysis.backfill_enabled"
	cfgKeyEnrichedSince        = "listen.media_analysis.enriched_since"
	cfgKeyMediaAnalysisEnabled = "listen.media_analysis.enabled"
)

// MediaEnrichWorkerDeps bundles dependencies for the media enrichment worker.
type MediaEnrichWorkerDeps struct {
	RawMsgStore   store.ListenRawMessageStore
	ChunkStore    store.RawMessageChunkStore // optional: backfill chunk invalidation (nil-safe)
	SystemConfigs store.SystemConfigStore
	Analyzer      *MediaAnalyzer // optional: nil = pass-through only (no vision calls)
	TenantID      uuid.UUID
	PollSec       int
}

// RegisterMediaEnrichWorker starts a background goroutine that periodically
// polls listen_raw_messages for media-bearing rows awaiting enrichment,
// analyzes image refs via the MediaAnalyzer, rewrites the body, and marks the
// row analyzed. Registered unconditionally (Decision D6): even with no vision
// provider the worker must pass-through-mark rows so the FR-04 pending gates
// keep draining.
func RegisterMediaEnrichWorker(deps MediaEnrichWorkerDeps) func() {
	if deps.RawMsgStore == nil || deps.SystemConfigs == nil {
		slog.Info("whatsapp media enrich worker: skipped, missing stores")
		return func() {}
	}

	basePollSec := deps.PollSec
	if basePollSec <= 0 {
		basePollSec = defaultMediaEnrichPollSec
	}

	stopCh := make(chan struct{})
	go func() {
		for {
			ctx := store.WithTenantID(context.Background(), deps.TenantID)

			cutoff, backfill := ensureEnrichActivation(ctx, deps)
			processed := processMediaEnrichment(ctx, deps, store.MediaEnrichFilter{
				Cutoff:  cutoff,
				Backfill: false,
				MaxRows: mediaEnrichBatchSize,
			})
			if backfill {
				processed += processMediaEnrichment(ctx, deps, store.MediaEnrichFilter{
					Cutoff:  cutoff,
					Backfill: true,
					MaxRows: mediaEnrichBatchSize,
				})
			}

			nextInterval := time.Duration(basePollSec) * time.Second
			if processed >= mediaEnrichBatchSize {
				nextInterval = time.Duration(mediaEnrichMinPollSec) * time.Second
			}

			select {
			case <-time.After(nextInterval):
			case <-stopCh:
				return
			}
		}
	}()

	slog.Info("whatsapp media enrich worker: started",
		"poll_interval", (time.Duration(basePollSec) * time.Second).String(),
		"batch_size", mediaEnrichBatchSize, "max_concurrent", mediaEnrichMaxConcurrent)
	return func() { close(stopCh) }
}

// ensureEnrichActivation resolves (or first-writes) the enriched_since
// activation cutoff and the backfill flag (SRS 014 FR-06). On the very first
// registration — key absent — the cutoff is persisted once, and when backfill
// is off all pre-cutoff media rows are bulk pass-through-marked so the FR-04
// gates drain without spending vision calls. Restarts reuse the stored value
// and never re-cut the window.
func ensureEnrichActivation(ctx context.Context, deps MediaEnrichWorkerDeps) (time.Time, bool) {
	backfill := false
	if v, err := deps.SystemConfigs.Get(ctx, cfgKeyMediaBackfillEnabled); err == nil && (v == "true" || v == "1") {
		backfill = true
	}

	if raw, err := deps.SystemConfigs.Get(ctx, cfgKeyEnrichedSince); err == nil && raw != "" {
		if t, perr := time.Parse(time.RFC3339, raw); perr == nil {
			return t, backfill
		}
	}

	// First registration: persist the cutoff exactly once.
	now := time.Now().UTC()
	if err := deps.SystemConfigs.Set(ctx, cfgKeyEnrichedSince, now.Format(time.RFC3339)); err != nil {
		slog.Warn("whatsapp media enrich worker: failed to persist enriched_since", "error", err)
	}
	if !backfill {
		if n, err := deps.RawMsgStore.MarkMediaAnalyzedBefore(ctx, now); err != nil {
			slog.Warn("whatsapp media enrich worker: bulk pass-through mark failed", "error", err)
		} else if n > 0 {
			slog.Info("whatsapp media enrich worker: bulk-marked historical media rows (backfill off)",
				"marked", n, "cutoff", now.Format(time.RFC3339))
		}
	}
	return now, backfill
}

// mediaAnalysisEnabled reads the master gate. Enabled by default unless
// explicitly "false"/"0" (same semantics as MediaAnalyzer.loadLimits).
func mediaAnalysisEnabled(ctx context.Context, sysCfg store.SystemConfigStore) bool {
	if sysCfg == nil {
		return true
	}
	v, err := sysCfg.Get(ctx, cfgKeyMediaAnalysisEnabled)
	if err != nil {
		return true
	}
	return v != "false" && v != "0"
}

// processMediaEnrichment polls one filter mode and processes the batch.
// Rows are grouped by (agent_id, graph_id) and processed with bounded
// concurrency; each group logs its outcome counters (FR-07). Returns the
// number of rows picked up this pass (backlog signal for the poll interval).
func processMediaEnrichment(ctx context.Context, deps MediaEnrichWorkerDeps, filter store.MediaEnrichFilter) int {
	rows, err := deps.RawMsgStore.ListPendingMediaEnrichment(ctx, filter)
	if err != nil {
		slog.Warn("whatsapp media enrich worker: failed to list pending rows", "backfill", filter.Backfill, "error", err)
		return 0
	}
	if len(rows) == 0 {
		return 0
	}

	providerOK := deps.Analyzer != nil && deps.Analyzer.HasVisionProvider(ctx)
	enabled := mediaAnalysisEnabled(ctx, deps.SystemConfigs)
	lim := mediaLimits{}
	if deps.Analyzer != nil {
		lim = deps.Analyzer.loadLimits(ctx)
	}

	// Group rows by (agent_id, graph_id) preserving order.
	type groupKey struct{ agent, graph string }
	order := make([]groupKey, 0)
	groups := make(map[groupKey][]store.ListenRawMessage)
	for _, m := range rows {
		k := groupKey{m.AgentID, m.GraphID}
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], m)
	}

	sem := semaphore.NewWeighted(mediaEnrichMaxConcurrent)
	var wg sync.WaitGroup
	for _, k := range order {
		if err := sem.Acquire(ctx, 1); err != nil {
			break
		}
		wg.Add(1)
		go func(k groupKey, msgs []store.ListenRawMessage) {
			defer wg.Done()
			defer sem.Release(1)
			enrichGroup(ctx, deps, k.agent, k.graph, msgs, providerOK && enabled, lim)
		}(k, groups[k])
	}
	wg.Wait()
	return len(rows)
}

// enrichGroup processes one (agent_id, graph_id) group: pass-through-mark rows
// that must not be analyzed (Decision D1/D3, disabled gate, idempotent guard),
// enrich the rest, and invalidate stale day-group chunks for rows that were
// already embedded (backfill, SRS 014 FR-06 mirroring 007 FR-08).
func enrichGroup(ctx context.Context, deps MediaEnrichWorkerDeps, agentID, graphID string, msgs []store.ListenRawMessage, analyze bool, lim mediaLimits) {
	var (
		passThroughIDs []uuid.UUID
		enriched       int
		failed         int
	)

	for _, m := range msgs {
		if !analyze || strings.Contains(m.Body, "<description>") || len(imageMediaRefs(m)) == 0 {
			// Pass-through: no-provider / disabled / non-image-only / already
			// described. Mark analyzed WITHOUT touching body or failure markers.
			passThroughIDs = append(passThroughIDs, m.ID)
			continue
		}
		if enrichRow(ctx, deps, m, lim) {
			enriched++
		} else {
			failed++
		}
	}

	if len(passThroughIDs) > 0 {
		if _, err := deps.RawMsgStore.MarkMediaAnalyzedByIDs(ctx, passThroughIDs); err != nil {
			slog.Warn("whatsapp media enrich worker: pass-through mark failed",
				"agent_id", agentID, "graph_id", graphID, "error", err)
		}
	}

	slog.Info("whatsapp media enrich worker: batch complete",
		"agent_id", agentID, "graph_id", graphID,
		"enriched", enriched, "passed_through", len(passThroughIDs), "failed", failed,
		"media", mediaRefsSummary(msgs))
}

// enrichRow analyzes the image refs of one row, rewrites the body per the
// FR-03 contract, and persists via MarkMediaEnriched (single UPDATE incl.
// pipeline state reset). Vision failures never drop the message — the failed
// ref gets a <description>[analysis failed]</description> marker and the row
// is still marked analyzed (non-blocking, 009/011 silent best-effort).
func enrichRow(ctx context.Context, deps MediaEnrichWorkerDeps, m store.ListenRawMessage, lim mediaLimits) bool {
	refs := imageMediaRefs(m)
	descs := make([]string, len(refs))
	for i, ref := range refs {
		_, content, err := deps.Analyzer.analyzeOne(ctx, ref, lim)
		if err != nil {
			slog.Warn("whatsapp media enrich worker: image analysis failed",
				"msg_id", m.ID, "agent_id", m.AgentID, "graph_id", m.GraphID,
				"media_type", ref.MediaType, "error", err)
			descs[i] = "[analysis failed]"
			continue
		}
		descs[i] = content
	}

	newBody := enrichBodyWithDescriptions(m.Body, descs)
	if err := deps.RawMsgStore.MarkMediaEnriched(ctx, m.ID, newBody); err != nil {
		slog.Warn("whatsapp media enrich worker: failed to persist enriched body",
			"msg_id", m.ID, "agent_id", m.AgentID, "graph_id", m.GraphID, "error", err)
		return false
	}

	// Backfill already-embedded rows share day-group chunks with neighbors:
	// delete this message's chunks and re-queue the uncovered neighbors so the
	// day group re-embeds with the description in the chunk text (FR-06,
	// mirroring 007 FR-08's true-move). No-op for never-embedded fresh rows.
	invalidateChunksForEnrichedRow(ctx, deps, m.ID)
	return true
}

// invalidateChunksForEnrichedRow deletes the enriched row's chunks (returns
// nothing when the row was never embedded) and re-queues day-group neighbors
// whose chunks were co-deleted. Best-effort and non-transactional, mirroring
// the 007 FR-08 handler sequence.
func invalidateChunksForEnrichedRow(ctx context.Context, deps MediaEnrichWorkerDeps, id uuid.UUID) {
	if deps.ChunkStore == nil {
		return
	}
	sources, deleted, err := deps.ChunkStore.DeleteBySourceMsgIDs(ctx, []uuid.UUID{id})
	if err != nil {
		slog.Warn("whatsapp media enrich worker: chunk invalidation failed", "msg_id", id, "error", err)
		return
	}
	if deleted == 0 {
		return
	}
	var neighbors []uuid.UUID
	for _, s := range sources {
		if s != id {
			neighbors = append(neighbors, s)
		}
	}
	if len(neighbors) == 0 {
		return
	}
	if _, err := deps.RawMsgStore.ResetEmbeddedByIDs(ctx, neighbors); err != nil {
		slog.Warn("whatsapp media enrich worker: neighbor re-queue failed", "error", err)
		return
	}
	slog.Debug("whatsapp media enrich worker: day-group chunks invalidated",
		"msg_id", id, "chunks_deleted", deleted, "neighbors_requeued", len(neighbors))
}

// imageMediaRefs returns the row's image refs in media_refs order (the same
// order BuildMediaTags emits <media:image> tags, so description i maps to tag
// i). Stickers/videos/documents are excluded — v1 is image-only (Decision D3).
func imageMediaRefs(m store.ListenRawMessage) []store.RawMediaRef {
	var refs []store.RawMediaRef
	for _, r := range m.MediaRefs {
		mt := r.MediaType
		if mt == "" {
			mt = mediaTypeFromMime(r.ContentType)
		}
		if mt == "image" {
			refs = append(refs, r)
		}
	}
	return refs
}

// enrichBodyWithDescriptions rewrites each bare <media:image> tag in body into
// tag + <description> block, in order (FR-03). descriptions[i] is embedded
// after the i-th image tag's line (so "(from replied message)" suffixes stay
// attached to their tag). Extra tags without a description are left untouched.
// The tag itself is KEPT — the description is additive (008 body filters and
// tag-grepping keep working). Descriptions are XML-escaped like transcripts
// and truncated at mediaDescriptionMaxRunes.
func enrichBodyWithDescriptions(body string, descriptions []string) string {
	const tag = "<media:image>"
	if !strings.Contains(body, tag) || len(descriptions) == 0 {
		return body
	}
	var b strings.Builder
	rest := body
	di := 0
	for di < len(descriptions) {
		idx := strings.Index(rest, tag)
		if idx < 0 {
			break
		}
		end := idx + len(tag)
		if lineEnd := strings.Index(rest[end:], "\n"); lineEnd >= 0 {
			end += lineEnd
		} else {
			end = len(rest)
		}
		b.WriteString(rest[:end])
		b.WriteString("\n<description>")
		b.WriteString(escapeDescription(descriptions[di]))
		b.WriteString("</description>")
		rest = rest[end:]
		di++
	}
	b.WriteString(rest)
	return b.String()
}

// escapeDescription truncates at mediaDescriptionMaxRunes runes (rune-safe for
// vi/zh content) and XML-escapes, mirroring the <transcript> escaping in
// BuildMediaTags.
func escapeDescription(desc string) string {
	r := []rune(desc)
	if len(r) > mediaDescriptionMaxRunes {
		return html.EscapeString(string(r[:mediaDescriptionMaxRunes])) + " … [truncated]"
	}
	return html.EscapeString(string(r))
}
