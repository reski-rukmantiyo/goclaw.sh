package whatsapp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"github.com/nextlevelbuilder/goclaw/internal/knowledgegraph"
	"github.com/nextlevelbuilder/goclaw/internal/providerresolve"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	defaultExtractPollSec    = 30
	extractBatchSize         = 20
	maxRetryBackoffSec       = 600 // 10 minutes cap
	providerCacheTTL         = 5 * time.Minute
	maxConcurrentGroups      = 2
	defaultMaxConcurrentLLM  = 8
)

// llmSemaphore implements knowledgegraph.LLMRateLimiter using a channel-based semaphore.
type llmSemaphore struct {
 sem chan struct{}
}

// NewLLMSemaphore creates an LLM concurrency limiter. maxConcurrent <= 0 uses the default (8).
func NewLLMSemaphore(maxConcurrent int) *llmSemaphore {
 if maxConcurrent <= 0 {
  maxConcurrent = defaultMaxConcurrentLLM
 }
 return &llmSemaphore{sem: make(chan struct{}, maxConcurrent)}
}

func (s *llmSemaphore) Acquire(ctx context.Context) error {
 select {
 case s.sem <- struct{}{}:
  return nil
 case <-ctx.Done():
  return ctx.Err()
 }
}

func (s *llmSemaphore) Release() { <-s.sem }

// Current returns the number of currently in-flight LLM calls.
func (s *llmSemaphore) Current() int { return len(s.sem) }

// Cap returns the maximum concurrent LLM calls allowed.
func (s *llmSemaphore) Cap() int { return cap(s.sem) }

// groupRetryState tracks consecutive extraction failures for a (agentID, graphID) group.
type groupRetryState struct {
	consecutiveFailures int
	nextAttempt         time.Time
}

// cachedProvider holds a resolved LLM provider with a TTL for reuse across ticks.
type cachedProvider struct {
	provider      providers.Provider
	model         string
	minConfidence float64
	source        string
	resolvedAt    time.Time
}

// ExtractionWorkerDeps bundles dependencies for the listen-only KG extraction worker.
type ExtractionWorkerDeps struct {
	RawMsgStore   store.ListenRawMessageStore
	KGStore       store.KnowledgeGraphStore
	SystemConfigs store.SystemConfigStore
	BuiltinTools  store.BuiltinToolStore
	Registry      *providers.Registry
	TenantID      uuid.UUID
	PollSec       int // poll interval in seconds (default 30)
	MediaAnalyzer *MediaAnalyzer
	LLMSem        *llmSemaphore // global LLM concurrency cap
	DebugBuffer   *ExtractionDebugBuffer // optional: captures extraction debug records

	mu           sync.Mutex                  // protects retryTracker and provider for concurrent group processing
	retryTracker map[string]*groupRetryState // key: agentID+"/"+graphID
	provider     *cachedProvider              // cached LLM provider
}

// resolveProvider returns a cached LLM provider or resolves a new one.
// Safe for concurrent use via d.mu.
func (d *ExtractionWorkerDeps) resolveProvider(ctx context.Context) (providers.Provider, string, float64, string) {
	d.mu.Lock()
	if d.provider != nil && time.Since(d.provider.resolvedAt) < providerCacheTTL {
		p := d.provider.provider
		m := d.provider.model
		mc := d.provider.minConfidence
		s := d.provider.source
		d.mu.Unlock()
		return p, m, mc, s
	}
	d.mu.Unlock()

	var p providers.Provider
	var model string
	var minConfidence float64 = 0.75
	var providerSource string

	if d.BuiltinTools != nil {
		p, model, minConfidence, providerSource = resolveKGProvider(ctx, d)
	}

	if p == nil {
		p, model = providerresolve.ResolveBackgroundProvider(ctx, d.TenantID, d.Registry, d.SystemConfigs)
		if p != nil {
			providerSource = "background"
		}
	}

	if p != nil {
		d.mu.Lock()
		d.provider = &cachedProvider{
			provider: p, model: model, minConfidence: minConfidence,
			source: providerSource, resolvedAt: time.Now(),
		}
		d.mu.Unlock()
	}
	return p, model, minConfidence, providerSource
}

// RegisterExtractionWorker starts a background goroutine that periodically polls
// listen_raw_messages for unprocessed batches and runs KG extraction.
// Returns a cleanup function that stops the worker.
func RegisterExtractionWorker(deps *ExtractionWorkerDeps) func() {
	if deps.RawMsgStore == nil || deps.KGStore == nil {
		slog.Info("whatsapp extraction worker: skipped, missing stores")
		return func() {}
	}

	deps.retryTracker = make(map[string]*groupRetryState)

	pollSec := deps.PollSec
	if pollSec <= 0 {
		pollSec = defaultExtractPollSec
	}
	pollInterval := time.Duration(pollSec) * time.Second

	stopCh := make(chan struct{})
	go func() {
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				processAllPendingBatches(deps)
			case <-stopCh:
				return
			}
		}
	}()

	slog.Info("whatsapp extraction worker: started",
		"poll_interval", pollInterval, "batch_size", extractBatchSize,
		"media_analyzer", deps.MediaAnalyzer != nil)
	return func() { close(stopCh) }
}

// processAllPendingBatches finds all (agentID, graphID) groups with pending
// messages and processes up to maxConcurrentGroups batches concurrently.
func processAllPendingBatches(deps *ExtractionWorkerDeps) {
	ctx := store.WithTenantID(context.Background(), deps.TenantID)

	groups, err := deps.RawMsgStore.ListPendingGroups(ctx)
	if err != nil {
		slog.Warn("whatsapp extraction worker: failed to list pending groups", "error", err)
		return
	}
	if len(groups) == 0 {
		return
	}

	slog.Debug("whatsapp extraction worker: processing groups",
		"count", len(groups), "max_concurrent", maxConcurrentGroups)

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrentGroups)

	for _, grp := range groups {
		grp := grp
		g.Go(func() error {
			processGroupBatch(gctx, deps, grp.AgentID, grp.GraphID)
			return nil
		})
	}
	g.Wait()
}

// processGroupBatch processes one batch of pending messages for a given (agentID, graphID).
func processGroupBatch(ctx context.Context, deps *ExtractionWorkerDeps, agentID, graphID string) {
	groupKey := agentID + "/" + graphID
	extractID := uuid.Must(uuid.NewV7()).String()[:8]
	logger := slog.With("extract_id", extractID, "agent_id", agentID, "graph_id", graphID)

	rec := newDebugRecord(agentID, graphID)
	rec.ID = extractID
	defer func() {
		rec.finalize(nil)
		if deps.DebugBuffer != nil {
			deps.DebugBuffer.Add(rec)
		}
	}()

	// Check backoff: skip this group if we're in a retry cooldown.
	deps.mu.Lock()
	rs, hasBackoff := deps.retryTracker[groupKey]
	if hasBackoff && time.Now().Before(rs.nextAttempt) {
		failures := rs.consecutiveFailures
		nextAttempt := rs.nextAttempt.Format("15:04:05")
		deps.mu.Unlock()
		logger.Debug("whatsapp extraction worker: skipping group due to retry backoff",
			"consecutive_failures", failures,
			"next_attempt", nextAttempt)
		return
	}
	deps.mu.Unlock()

	msgs, err := deps.RawMsgStore.ListPending(ctx, agentID, graphID, extractBatchSize)
	if err != nil {
		logger.Warn("whatsapp extraction worker: failed to list pending messages", "error", err)
		return
	}
	if len(msgs) == 0 {
		return
	}

	// Resolve KG extraction provider (cached for 5 minutes).
	p, model, minConfidence, providerSource := deps.resolveProvider(ctx)

	if p == nil {
		logger.Warn("whatsapp extraction worker: no LLM provider available")
		recordExtractionFailure(deps, groupKey, agentID, graphID, "no LLM provider available", msgs)
		return
	}

	logger.Info("whatsapp extraction worker: extracting KG from batch",
		"messages", len(msgs),
		"provider", p.Name(), "model", model,
		"provider_source", providerSource, "min_confidence", minConfidence)

	batchStart := time.Now()
	// Build full raw text (used for fallback path).
	fullText := buildConversationTextFromRaw(msgs)
	if fullText == "" {
		return
	}

	// Group messages by date, build text per date, analyze media per date,
	// then summarize each date separately for coherent narratives.
	dateGroups := groupMessagesByDate(msgs)

	// Summarize date groups concurrently (up to 4 in parallel).
	type dateResult struct {
		date    string
		summary string
		rawLen  int
		err     error
	}
	const maxConcurrentDates = 4
	results := make([]dateResult, len(dateGroups.order))

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrentDates)

	for i, date := range dateGroups.order {
		i, date := i, date
		dayMsgs := dateGroups.groups[date]
		g.Go(func() error {
			dayText := buildConversationTextFromRaw(dayMsgs)
			if dayText == "" {
				results[i] = dateResult{date: date}
				return nil
			}

			// Analyze media for this date's messages.
			dayText = appendMediaAnalysis(gctx, deps, dayMsgs, dayText, logger)

			// Rate-limit LLM calls via global semaphore.
			if deps.LLMSem != nil {
				if err := deps.LLMSem.Acquire(gctx); err != nil {
					results[i] = dateResult{date: date, err: err}
					return nil
				}
				defer deps.LLMSem.Release()
			}

			summary, err := summarizeConversation(gctx, p, model, dayText, logger)
			results[i] = dateResult{date: date, summary: summary, rawLen: len(dayText), err: err}
			return nil // don't cancel others on individual date failure
		})
	}
	g.Wait()

	logger.Info("whatsapp extraction worker: summarization complete",
		"dates", len(dateGroups.order), "elapsed", time.Since(batchStart).Round(time.Millisecond))
	rec.addStep("summarization", batchStart, nil)

	// Build combined summary from ordered results.
	var combinedSummary strings.Builder
	summarizeFailCount := 0
	for _, r := range results {
		if r.summary == "" && r.err == nil {
			continue // empty day (no text)
		}

		summary := r.summary
		ds := DateSummary{Date: r.date, RawLen: r.rawLen, SummaryLen: len(r.summary)}
		if r.err != nil {
			summarizeFailCount++
			ds.Error = r.err.Error()
			logger.Warn("whatsapp extraction worker: summarization failed for date, using raw text for this date",
				"date", r.date, "error", r.err)
			// Rebuild raw text for fallback.
			dayMsgs := dateGroups.groups[r.date]
			summary = buildConversationTextFromRaw(dayMsgs)
			ds.SummaryLen = len(summary)
		}
		rec.DateSummaries = append(rec.DateSummaries, ds)

		if combinedSummary.Len() > 0 {
			combinedSummary.WriteString("\n\n")
		}
		fmt.Fprintf(&combinedSummary, "== %s ==\n%s", r.date, summary)

		if knowledgegraph.VerboseLogging() {
			preview := summary
			if len(preview) > 300 {
				preview = preview[:300] + "..."
			}
			logger.Info("whatsapp extraction worker: processed date",
				"date", r.date, "raw_len", r.rawLen, "summary_len", len(summary), "summary", preview)
		}
	}

	// Only fall back to full raw text if ALL dates failed summarization.
	if summarizeFailCount == len(dateGroups.order) && len(dateGroups.order) > 0 {
		logger.Warn("whatsapp extraction worker: all dates failed summarization, using full raw text")
		fullText = appendMediaAnalysis(ctx, deps, msgs, fullText, logger)
		extractor := knowledgegraph.NewExtractorWithPrompt(p, model, minConfidence, listenExtractSystemPrompt)
		if deps.LLMSem != nil {
			extractor.SetRateLimiter(deps.LLMSem)
		}
		extractStart := time.Now()
		result, err := extractor.Extract(ctx, fullText)
		rec.addStep("extraction", extractStart, err)
		if err != nil {
			logger.Warn("whatsapp extraction worker: extraction failed", "error", err)
			recordExtractionFailure(deps, groupKey, agentID, graphID, fmt.Sprintf("fallback extraction failed: %s", err), msgs)
			return
		}
		rec.EntityCount = len(result.Entities)
		rec.RelationCount = len(result.Relations)
		ingestAndFinalize(ctx, deps, result, agentID, graphID, msgs, groupKey, logger, rec)
		return
	}

	if summarizeFailCount > 0 {
		logger.Info("whatsapp extraction worker: some dates used raw text fallback",
			"failed_dates", summarizeFailCount, "total_dates", len(dateGroups.order))
	}

	extractStart := time.Now()
	// Extract KG from the combined per-date summaries using the default extraction prompt.
	extractionText := combinedSummary.String()
	extractor := knowledgegraph.NewExtractor(p, model, minConfidence)
	if deps.LLMSem != nil {
		extractor.SetRateLimiter(deps.LLMSem)
	}
	result, err := extractor.Extract(ctx, extractionText)
	rec.addStep("extraction", extractStart, err)
	if err != nil {
		logger.Warn("whatsapp extraction worker: extraction failed", "error", err)
		recordExtractionFailure(deps, groupKey, agentID, graphID, fmt.Sprintf("summary extraction failed: %s", err), msgs)
		return
	}
	rec.EntityCount = len(result.Entities)
	rec.RelationCount = len(result.Relations)

	logger.Info("whatsapp extraction worker: extraction complete",
		"extraction_elapsed", time.Since(extractStart).Round(time.Millisecond),
		"total_elapsed", time.Since(batchStart).Round(time.Millisecond))

	ingestAndFinalize(ctx, deps, result, agentID, graphID, msgs, groupKey, logger, rec)
}

// ingestAndFinalize handles entity scoping, KG ingestion, dedup, and marking messages as processed.
func ingestAndFinalize(ctx context.Context, deps *ExtractionWorkerDeps, result *knowledgegraph.ExtractionResult, agentID, graphID string, msgs []store.ListenRawMessage, groupKey string, logger *slog.Logger, rec *ExtractionDebugRecord) {
	if len(result.Entities) == 0 && len(result.Relations) == 0 {
		logger.Debug("whatsapp extraction worker: no entities extracted", "messages", len(msgs))
	} else {
		for i, e := range result.Entities {
			logger.Debug("whatsapp extraction worker: extracted entity",
				"idx", i, "name", e.Name, "type", e.EntityType, "confidence", fmt.Sprintf("%.2f", e.Confidence))
		}
		for i, r := range result.Relations {
			logger.Debug("whatsapp extraction worker: extracted relation",
				"idx", i, "source", r.SourceEntityID, "target", r.TargetEntityID, "type", r.RelationType)
		}
	}

	// Scope entities/relations to (agentID, graphID).
	now := time.Now().UTC()
	for i := range result.Entities {
		result.Entities[i].AgentID = agentID
		result.Entities[i].UserID = graphID
		result.Entities[i].ValidFrom = &now
	}
	for i := range result.Relations {
		result.Relations[i].AgentID = agentID
		result.Relations[i].UserID = graphID
		result.Relations[i].ValidFrom = &now
	}

	// Fallback: for event entities without extracted event_time, derive from message batch.
	for i := range result.Entities {
		if result.Entities[i].EntityType == "event" && result.Entities[i].EventTime == nil && len(msgs) > 0 {
			earliest := msgs[0].MsgTimestamp
			for _, m := range msgs[1:] {
				if m.MsgTimestamp.Before(earliest) {
					earliest = m.MsgTimestamp
				}
			}
			result.Entities[i].EventTime = &earliest
		}
	}

	// Ingest into KG store.
	if len(result.Entities) > 0 || len(result.Relations) > 0 {
		ingestStart := time.Now()
		entityIDs, err := deps.KGStore.IngestExtraction(ctx, agentID, graphID,
			result.Entities, result.Relations)
		rec.addStep("ingest", ingestStart, err)
		if err != nil {
			logger.Warn("whatsapp extraction worker: KG ingest failed", "error", err)
			recordExtractionFailure(deps, groupKey, agentID, graphID, fmt.Sprintf("KG ingest failed: %s", err), msgs)
			return
		} else {
			rec.IngestedIDs = len(entityIDs)
			logger.Info("whatsapp extraction worker: KG extraction complete",
				"entities", len(result.Entities),
				"relations", len(result.Relations),
				"ingested_ids", len(entityIDs))

			// Run dedup asynchronously (best-effort, non-blocking).
			if len(entityIDs) > 0 {
				dedupCtx := context.WithoutCancel(ctx)
				go func() {
						dedupStart := time.Now()
						if merged, flagged, dedupErr := deps.KGStore.DedupAfterExtraction(dedupCtx, agentID, graphID, entityIDs); dedupErr != nil {
							logger.Debug("whatsapp extraction worker: dedup failed", "error", dedupErr)
						} else {
							rec.DedupMerged = merged
							rec.DedupFlagged = flagged
							if merged > 0 || flagged > 0 {
								logger.Info("whatsapp extraction worker: dedup results",
									"merged", merged, "flagged", flagged)
							}
						}
						rec.addStep("dedup", dedupStart, nil)
					}()
			}
		}
	}

	// Mark messages as processed.
	ids := make([]uuid.UUID, len(msgs))
	for i, m := range msgs {
		ids[i] = m.ID
	}
	if err := deps.RawMsgStore.MarkProcessed(ctx, ids); err != nil {
		logger.Warn("whatsapp extraction worker: failed to mark processed", "error", err)
	} else {
		// Reset retry tracker on success.
		deps.mu.Lock()
		delete(deps.retryTracker, groupKey)
		deps.mu.Unlock()
	}
}

// recordExtractionFailure increments the consecutive failure counter, persists the
// error to the database for UI visibility, and applies exponential backoff.
func recordExtractionFailure(deps *ExtractionWorkerDeps, groupKey, agentID, graphID, errorMsg string, msgs []store.ListenRawMessage) {
	// Persist failure to DB for each message in the batch.
	if len(msgs) > 0 && deps.RawMsgStore != nil {
		ids := make([]uuid.UUID, len(msgs))
		for i, m := range msgs {
			ids[i] = m.ID
		}
		failCtx := store.WithTenantID(context.Background(), deps.TenantID)
		if err := deps.RawMsgStore.MarkExtractionFailed(failCtx, ids, errorMsg); err != nil {
			slog.Warn("whatsapp extraction worker: failed to mark extraction failure",
				"agent_id", agentID, "graph_id", graphID, "error", err)
		}
	}

	deps.mu.Lock()
	s := deps.retryTracker[groupKey]
	if s == nil {
		s = &groupRetryState{}
		deps.retryTracker[groupKey] = s
	}
	s.consecutiveFailures++
	backoffSec := math.Min(float64(defaultExtractPollSec)*math.Pow(2, float64(s.consecutiveFailures)), float64(maxRetryBackoffSec))
	s.nextAttempt = time.Now().Add(time.Duration(backoffSec) * time.Second)
	failures := s.consecutiveFailures
	nextAttempt := s.nextAttempt.Format("15:04:05")
	deps.mu.Unlock()

	if failures >= 3 {
		slog.Warn("whatsapp extraction worker: group has consecutive failures, backing off",
			"agent_id", agentID, "graph_id", graphID,
			"error", errorMsg,
			"consecutive_failures", failures,
			"backoff_sec", int(backoffSec),
			"next_attempt", nextAttempt)
	}
}

// summarizeConversation calls the LLM to summarize raw WhatsApp text into polished narrative
// while preserving specific details (names, IDs, timestamps, structured data).
// On truncation, it recursively splits the input and summarizes each half.
func summarizeConversation(ctx context.Context, p providers.Provider, model, text string, logger *slog.Logger) (string, error) {
	return summarizeConversationDepth(ctx, p, model, text, 0, logger)
}

// summarizeConversationDepth performs summarization with recursive splitting on truncation.
// depth limits recursion to prevent infinite loops.
func summarizeConversationDepth(ctx context.Context, p providers.Provider, model, text string, depth int, logger *slog.Logger) (string, error) {
	req := providers.ChatRequest{
		Messages: []providers.Message{
			{Role: "system", Content: listenSummarizePrompt},
			{Role: "user", Content: text},
		},
		Model: model,
		Options: map[string]any{
			"max_tokens":  8192,
			"temperature": 0.3,
		},
	}

	resp, err := p.Chat(ctx, req)
	if err != nil {
		return "", fmt.Errorf("summarize conversation: %w", err)
	}

	if resp.FinishReason == "length" && depth < 1 {
		logger.Warn("whatsapp extraction worker: summarization truncated, splitting input",
			"input_len", len(text), "output_len", len(resp.Content), "depth", depth)

		// Split at midpoint, preferring paragraph boundary.
		half := len(text) / 2
		if idx := strings.LastIndex(text[:half], "\n\n"); idx > half/2 {
			half = idx
		}

		// Split concurrently (both halves in parallel).
		g, gctx := errgroup.WithContext(ctx)
		g.SetLimit(2)
		var sum1, sum2 string
		var err1, err2 error
		g.Go(func() error {
			sum1, err1 = summarizeConversationDepth(gctx, p, model, text[:half], depth+1, logger)
			return nil
		})
		g.Go(func() error {
			sum2, err2 = summarizeConversationDepth(gctx, p, model, text[half:], depth+1, logger)
			return nil
		})
		g.Wait()

		if err1 != nil {
			logger.Warn("whatsapp extraction worker: first-half summary failed, using partial",
				"error", err1)
			return strings.TrimSpace(resp.Content), nil
		}
		if err2 != nil {
			logger.Warn("whatsapp extraction worker: second-half summary failed, using first half",
				"error", err2)
			return sum1, nil
		}

		return sum1 + "\n\n" + sum2, nil
	}

	return strings.TrimSpace(resp.Content), nil
}

// dateGroups holds messages grouped by date string with insertion order preserved.
type dateGroups struct {
	order  []string
	groups map[string][]store.ListenRawMessage
}

// groupMessagesByDate splits messages into groups keyed by their local date (YYYY-MM-DD).
func groupMessagesByDate(msgs []store.ListenRawMessage) dateGroups {
	dg := dateGroups{groups: make(map[string][]store.ListenRawMessage)}
	for _, m := range msgs {
		date := m.MsgTimestamp.Format("2006-01-02")
		if _, exists := dg.groups[date]; !exists {
			dg.order = append(dg.order, date)
		}
		dg.groups[date] = append(dg.groups[date], m)
	}
	return dg
}

// appendMediaAnalysis analyzes media attachments for the given messages and appends
// descriptions to the text. Returns text unchanged if no media or no analyzer.
func appendMediaAnalysis(ctx context.Context, deps *ExtractionWorkerDeps, msgs []store.ListenRawMessage, text string, logger *slog.Logger) string {
	mediaSummary := mediaRefsSummary(msgs)
	if mediaSummary == "" {
		return text
	}
	logger.Info("whatsapp extraction worker: analyzing media attachments", "media", mediaSummary)
	mediaDescs := analyzeMediaAttachments(ctx, msgs, deps.MediaAnalyzer)
	if len(mediaDescs) == 0 {
		return text
	}
	var mediaText strings.Builder
	mediaText.WriteString("\n\n[Media Content Analysis]\n")
	for _, m := range msgs {
		if desc, ok := mediaDescs[m.ID]; ok {
			ts := m.MsgTimestamp.Format("2006-01-02 15:04:05")
			fmt.Fprintf(&mediaText, "\n[%s] %s:\n%s\n", ts, m.Sender, desc)
		}
	}
	mediaStr := mediaText.String()
	logger.Info("whatsapp extraction worker: media analysis result",
		"media_text_len", len(mediaStr))
	return text + mediaStr
}

// buildConversationTextFromRaw formats raw messages into structured text for LLM extraction.
func buildConversationTextFromRaw(msgs []store.ListenRawMessage) string {
	if len(msgs) == 0 {
		return ""
	}

	// Group messages by chatID for multi-group context.
	grouped := make(map[string][]store.ListenRawMessage)
	var order []string
	for _, m := range msgs {
		if _, ok := grouped[m.ChatID]; !ok {
			order = append(order, m.ChatID)
		}
		grouped[m.ChatID] = append(grouped[m.ChatID], m)
	}

	var b strings.Builder
	for i, chatID := range order {
		msgs := grouped[chatID]
		if i > 0 {
			b.WriteString("\n\n")
		}
		chatName := msgs[0].ChatName
		if chatName == "" {
			chatName = chatID
		}
		fmt.Fprintf(&b, "[Messages from WhatsApp: %s (%s)]\n", chatName, chatID)
		for _, m := range msgs {
			ts := m.MsgTimestamp.Format("2006-01-02 15:04:05")
			fmt.Fprintf(&b, "\n[%s] %s:\n%s\n", ts, m.Sender, m.Body)
		}
	}
	return b.String()
}

// kgExtractionSettings mirrors the builtin_tools knowledge_graph_search settings JSON.
type kgExtractionSettings struct {
	ExtractionProvider string  `json:"extraction_provider"`
	ExtractionModel    string  `json:"extraction_model"`
	MinConfidence      float64 `json:"min_confidence"`
}

// resolveKGProvider reads KG extraction provider/model from builtin_tools settings.
// Returns the provider, model, min confidence, and source description.
func resolveKGProvider(ctx context.Context, deps *ExtractionWorkerDeps) (providers.Provider, string, float64, string) {
	raw, err := deps.BuiltinTools.GetSettings(ctx, "knowledge_graph_search")
	if err != nil || raw == nil {
		slog.Debug("whatsapp extraction worker: no KG settings in builtin_tools", "error", err)
		return nil, "", 0.75, ""
	}
	var settings kgExtractionSettings
	if err := json.Unmarshal(raw, &settings); err != nil {
		slog.Debug("whatsapp extraction worker: invalid KG settings", "error", err)
		return nil, "", 0.75, ""
	}
	if settings.ExtractionProvider == "" {
		return nil, "", 0.75, ""
	}
	p, err := deps.Registry.Get(ctx, settings.ExtractionProvider)
	if err != nil || p == nil {
		slog.Warn("whatsapp extraction worker: KG provider not found",
			"provider", settings.ExtractionProvider, "error", err)
		return nil, "", 0.75, ""
	}
	model := settings.ExtractionModel
	if model == "" {
		model = p.DefaultModel()
	}
	minConf := settings.MinConfidence
	if minConf <= 0 {
		minConf = 0.75
	}
	return p, model, minConf, "kg_settings"
}
