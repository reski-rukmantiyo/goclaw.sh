package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// pgAutoInjector implements AutoInjector backed by EpisodicStore + FTS search.
type pgAutoInjector struct {
	episodicStore store.EpisodicStore
	metricsStore  store.EvolutionMetricsStore // nil = metrics disabled
}

// NewAutoInjector creates an AutoInjector backed by episodic store search.
func NewAutoInjector(es store.EpisodicStore, ms store.EvolutionMetricsStore) AutoInjector {
	return &pgAutoInjector{episodicStore: es, metricsStore: ms}
}

// Inject searches episodic memory for relevant episodes and formats a prompt
// section. Surfaces a short L1 summary (the episode's actual Summary) for the
// top hits, not just the ~50-token L0 abstract (009 FR-03). Honors the agent's
// memory config (Enabled/Threshold/MaxEntries/MaxTokens) forwarded by the
// caller (009 FR-02). On continuity/temporal queries it runs a broader recall
// so the agent answers "what did we discuss Monday?" without needing to call
// memory_search (009 FR-04). Best-effort records recall for surfaced episodes
// so recall_count/last_recalled_at reflect all recall paths (009 FR-05).
func (a *pgAutoInjector) Inject(ctx context.Context, params InjectParams) (*InjectResult, error) {
	if a.episodicStore == nil {
		return &InjectResult{}, nil
	}
	if !params.Enabled { // 009 FR-02 operator opt-out
		return &InjectResult{}, nil
	}
	if isTrivialMessage(params.UserMessage) {
		return &InjectResult{}, nil
	}

	maxEntries := params.MaxEntries
	if maxEntries <= 0 {
		maxEntries = 5
	}
	threshold := params.Threshold
	if threshold <= 0 {
		threshold = 0.3
	}
	maxTokens := params.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 500
	}
	l1Depth := params.L1Depth
	if l1Depth < 0 {
		l1Depth = 0
	}
	l1PerHit := params.L1PerHitMaxTokens
	if l1PerHit <= 0 {
		l1PerHit = 120
	}

	// Context-aware recall: RecentContext enriches the query so vector search
	// resolves pronouns/implicit references in follow-up questions.
	searchQuery := buildRecallQuery(params.UserMessage, params.RecentContext)

	// 009 FR-04: continuity/temporal queries trigger a broader recall so the
	// referenced prior episode is actually surfaced.
	continuity := isContinuityQuery(params.UserMessage)
	searchMax := maxEntries * 2
	if continuity {
		searchMax = maxEntries * 4
		if searchMax < 12 {
			searchMax = 12
		}
	}

	// Weights + threshold: continuity queries rely on semantic vector recall
	// (FTS strict-AND often misses "what did we discuss Monday?" when the prior
	// summary lacks those exact tokens — the Monday episode still matches by
	// embedding similarity ~0.7+). So bias toward vector and lower the bar for
	// continuity; otherwise keep the FTS-favoured defaults.
	vw, tw, effThreshold := 0.3, 0.7, threshold
	if continuity {
		vw, tw = 0.6, 0.4
		effThreshold = threshold * 0.5
		if effThreshold < 0.15 {
			effThreshold = 0.15
		}
	}

	results, err := a.episodicStore.Search(ctx, searchQuery, params.AgentID, params.UserID,
		store.EpisodicSearchOptions{
			MaxResults:   searchMax,
			MinScore:     effThreshold,
			VectorWeight: vw,
			TextWeight:   tw,
		})
	if err != nil {
		return nil, fmt.Errorf("auto-inject search: %w", err)
	}
	if len(results) == 0 {
		return &InjectResult{}, nil
	}

	debugRecallDiag(params.AgentID, params.UserMessage, continuity, vw, tw, effThreshold, results)

	section, injected, topScore, recalled := a.buildMemorySection(ctx, results, maxEntries, maxTokens, l1Depth, l1PerHit, continuity)
	if injected == 0 {
		return &InjectResult{MatchCount: len(results)}, nil
	}

	// 009 FR-05: best-effort record recall on all auto-inject paths.
	a.recordRecall(ctx, recalled)

	result := &InjectResult{
		Section:    section,
		MatchCount: len(results),
		Injected:   injected,
		TopScore:   topScore,
	}

	// Record retrieval metric non-blocking (best-effort).
	a.recordRetrievalMetric(params, result)

	return result, nil
}

// recallHit pairs an episodic id with its relevance score for recall bookkeeping.
type recallHit struct {
	id    string
	score float64
}

// buildMemorySection formats the injected memory section. The first l1Depth
// entries include a truncated L1 summary (the episode's actual Summary) fetched
// via Get, in addition to the L0 abstract; the rest get the L0 abstract only
// (009 FR-03). Total size is bounded by an approximate token budget
// (tokens ≈ runes/4, generous for CJK). Returns the section text, count
// injected, top score, and the hits to record-recall (009 FR-05).
func (a *pgAutoInjector) buildMemorySection(ctx context.Context, results []store.EpisodicSearchResult,
	maxEntries, maxTokens, l1Depth, l1PerHit int, continuity bool) (string, int, float64, []recallHit) {

	var sb strings.Builder
	if continuity {
		// 009 FR-04: label the section so the agent knows prior context was
		// recalled automatically and should be used/cited.
		sb.WriteString("## Recalled Prior Context\n\nThe user may be referring to earlier work. " +
			"Relevant past-session memories (recalled automatically — use them; call memory_expand(id) for full details):\n")
	} else {
		sb.WriteString("## Memory Context\n\nRelevant memories from past sessions (use memory_search for details):\n")
	}

	injected := 0
	var topScore float64
	var recalled []recallHit
	// Approximate token budget in runes (≈4 runes/token for Latin; generous for
	// CJK where 1 rune ≈ 1 token, so this over-allocates safely).
	budgetRunes := maxTokens * 4
	for _, r := range results {
		if injected >= maxEntries {
			break
		}
		abstract := strings.TrimSpace(r.L0Abstract)
		if abstract == "" {
			continue
		}

		// 009 FR-03: L1 summary for the top l1Depth entries.
		var l1 string
		if injected < l1Depth {
			l1 = a.fetchL1Summary(ctx, r.EpisodicID, l1PerHit)
		}
		entryRunes := runeLen(abstract) + runeLen(l1)
		if injected > 0 && entryRunes > budgetRunes {
			break // token budget exhausted
		}

		sb.WriteString("- ")
		sb.WriteString(abstract)
		sb.WriteString("\n")
		if l1 != "" {
			sb.WriteString("    ")
			sb.WriteString(l1)
			sb.WriteString("\n")
		}
		budgetRunes -= entryRunes
		injected++
		recalled = append(recalled, recallHit{id: r.EpisodicID, score: r.Score})
		if r.Score > topScore {
			topScore = r.Score
		}
	}
	return sb.String(), injected, topScore, recalled
}

// fetchL1Summary returns a head-truncated slice of the episode's Summary, used
// to surface actual prior content (not just the L0 abstract). Empty on any
// failure (best-effort). 009 FR-03.
func (a *pgAutoInjector) fetchL1Summary(ctx context.Context, episodicID string, maxTokens int) string {
	if episodicID == "" || maxTokens <= 0 {
		return ""
	}
	ep, err := a.episodicStore.Get(ctx, episodicID)
	if err != nil || ep == nil {
		return ""
	}
	s := strings.TrimSpace(ep.Summary)
	if s == "" {
		return ""
	}
	return headClipRunes(s, maxTokens*4)
}

// debugRecallDiag is a TEMPORARY diagnostic (remove after 009 live verification)
// that appends one JSON line per auto-inject search to /tmp/raka_diag.jsonl so
// the recall path can be observed without gateway log access.
func debugRecallDiag(agentID, msg string, continuity bool, vw, tw, thr float64, results []store.EpisodicSearchResult) {
	type entry struct {
		AgentID    string  `json:"agent_id"`
		Msg        string  `json:"msg"`
		Continuity bool    `json:"continuity"`
		VW         float64 `json:"vw"`
		TW         float64 `json:"tw"`
		Threshold  float64 `json:"threshold"`
		N          int     `json:"n"`
		Top        []struct {
			SK    string  `json:"sk"`
			Score float64 `json:"score"`
		} `json:"top"`
	}
	e := entry{AgentID: agentID, Msg: msg, Continuity: continuity, VW: vw, TW: tw, Threshold: thr, N: len(results)}
	for i, r := range results {
		if i >= 5 {
			break
		}
		e.Top = append(e.Top, struct {
			SK    string  `json:"sk"`
			Score float64 `json:"score"`
		}{SK: r.SessionKey, Score: r.Score})
	}
	b, _ := json.Marshal(e)
	if f, err := os.OpenFile("/tmp/raka_diag.jsonl", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
		f.Write(b)
		f.WriteString("\n")
		f.Close()
	}
}

// recordRecall best-effort updates recall_count/last_recalled_at for the
// surfaced episodes via a detached background context, so a request
// cancellation cannot abort the bookkeeping and a write failure never fails
// the turn. 009 FR-05.
func (a *pgAutoInjector) recordRecall(ctx context.Context, hits []recallHit) {
	if len(hits) == 0 {
		return
	}
	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == (uuid.UUID{}) {
		return // no tenant scope to write under
	}
	go func() {
		bgCtx, cancel := context.WithTimeout(store.WithTenantID(context.Background(), tenantID), 5*time.Second)
		defer cancel()
		for _, h := range hits {
			if h.id == "" {
				continue
			}
			if err := a.episodicStore.RecordRecall(bgCtx, h.id, h.score); err != nil {
				slog.Debug("memory.auto_inject.record_recall_failed", "episodic_id", h.id, "error", err)
			}
		}
	}()
}

// recordRetrievalMetric records an auto-inject retrieval metric in a background goroutine.
func (a *pgAutoInjector) recordRetrievalMetric(params InjectParams, result *InjectResult) {
	if a.metricsStore == nil || params.TenantID == "" {
		return
	}
	tenantID, err := uuid.Parse(params.TenantID)
	if err != nil {
		return
	}
	agentID, err := uuid.Parse(params.AgentID)
	if err != nil {
		return
	}
	go func() {
		bgCtx, cancel := context.WithTimeout(store.WithTenantID(context.Background(), tenantID), 5*time.Second)
		defer cancel()
		value, _ := json.Marshal(map[string]any{
			"result_count":  result.MatchCount,
			"injected":      result.Injected,
			"top_score":     result.TopScore,
			"used_in_reply": result.Injected > 0,
		})
		if err := a.metricsStore.RecordMetric(bgCtx, store.EvolutionMetric{
			ID:         uuid.New(),
			TenantID:   tenantID,
			AgentID:    agentID,
			MetricType: store.MetricRetrieval,
			MetricKey:  "auto_inject",
			Value:      value,
		}); err != nil {
			slog.Debug("evolution.metric.auto_inject_failed", "error", err)
		}
	}()
}
