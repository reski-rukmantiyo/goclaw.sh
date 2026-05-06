package whatsapp

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ExtractionStep records timing for one pipeline step.
type ExtractionStep struct {
	Name     string        `json:"name"`
	Duration time.Duration `json:"duration_ms"`
	Error    string        `json:"error,omitempty"`
}

// DateSummary captures per-date summarization results.
type DateSummary struct {
	Date       string `json:"date"`
	RawLen     int    `json:"raw_len"`
	SummaryLen int    `json:"summary_len"`
	Error      string `json:"error,omitempty"`
}

// ExtractionDebugRecord captures timing and intermediate results for one batch.
type ExtractionDebugRecord struct {
	ID        string           `json:"id"`
	AgentID   string           `json:"agent_id"`
	GraphID   string           `json:"graph_id"`
	StartedAt time.Time        `json:"started_at"`
	Steps     []ExtractionStep `json:"steps,omitempty"`

	DateSummaries []DateSummary `json:"date_summaries,omitempty"`

	EntityCount   int `json:"entity_count,omitempty"`
	RelationCount int `json:"relation_count,omitempty"`

	IngestedIDs int `json:"ingested_ids,omitempty"`
	DedupMerged int `json:"dedup_merged,omitempty"`
	DedupFlagged int `json:"dedup_flagged,omitempty"`

	CompletedAt *time.Time `json:"completed_at,omitempty"`
	Error       string     `json:"error,omitempty"`
}

// ExtractionDebugBuffer is a thread-safe ring buffer for recent extraction records.
type ExtractionDebugBuffer struct {
	mu      sync.RWMutex
	records []*ExtractionDebugRecord
	size    int
	head    int
	count   int
}

// NewExtractionDebugBuffer creates a ring buffer holding the last `size` records.
func NewExtractionDebugBuffer(size int) *ExtractionDebugBuffer {
	if size <= 0 {
		size = 50
	}
	return &ExtractionDebugBuffer{
		records: make([]*ExtractionDebugRecord, size),
		size:    size,
	}
}

// Add inserts a new record, evicting the oldest if full.
func (b *ExtractionDebugBuffer) Add(rec *ExtractionDebugRecord) {
	b.mu.Lock()
	b.records[b.head] = rec
	b.head = (b.head + 1) % b.size
	if b.count < b.size {
		b.count++
	}
	b.mu.Unlock()
}

// List returns the most recent n records (newest first).
func (b *ExtractionDebugBuffer) List(limit int) []*ExtractionDebugRecord {
	if limit <= 0 {
		limit = 20
	}
	b.mu.RLock()
	defer b.mu.RUnlock()

	if limit > b.count {
		limit = b.count
	}
	result := make([]*ExtractionDebugRecord, limit)
	for i := 0; i < limit; i++ {
		idx := (b.head - 1 - i + b.size) % b.size
		result[i] = b.records[idx]
	}
	return result
}

// GetByID returns a single record by its ID, or nil if not found.
func (b *ExtractionDebugBuffer) GetByID(id string) *ExtractionDebugRecord {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for i := 0; i < b.count; i++ {
		idx := (b.head - 1 - i + b.size) % b.size
		if r := b.records[idx]; r != nil && r.ID == id {
			return r
		}
	}
	return nil
}

// ListAny returns records as any (for http handler interface).
func (b *ExtractionDebugBuffer) ListAny(limit int) any { return b.List(limit) }

// GetByIDAny returns a single record as any (for http handler interface).
func (b *ExtractionDebugBuffer) GetByIDAny(id string) any { return b.GetByID(id) }

// debugBufferAdapter wraps ExtractionDebugBuffer to satisfy the HTTP handler's history interface.
type debugBufferAdapter struct{ buf *ExtractionDebugBuffer }

// NewDebugBufferAdapter creates an adapter for the HTTP handler.
func NewDebugBufferAdapter(buf *ExtractionDebugBuffer) debugBufferAdapter {
	return debugBufferAdapter{buf: buf}
}

func (a debugBufferAdapter) List(limit int) any    { return a.buf.List(limit) }
func (a debugBufferAdapter) GetByID(id string) any { return a.buf.GetByID(id) }

// InFlightExtraction tracks a currently running extraction batch.
type InFlightExtraction struct {
	ID           string           `json:"id"`
	AgentID      string           `json:"agent_id"`
	GraphID      string           `json:"graph_id"`
	StartedAt    time.Time        `json:"started_at"`
	CurrentStep  string           `json:"current_step"`
	StepStarted  time.Time        `json:"step_started"`
	Dates        []InFlightDate   `json:"dates,omitempty"`
	MessageCount int              `json:"message_count"`
}

// InFlightDate tracks per-date status during extraction.
type InFlightDate struct {
	Date   string `json:"date"`
	Status string `json:"status"` // "pending", "summarizing", "done", "failed"
}

// WorkerStatus is a point-in-time snapshot of the extraction worker state.
type WorkerStatus struct {
	PendingGroups     []PendingGroupStatus    `json:"pending_groups,omitempty"`
	RetryBackoffs     []RetryBackoffStatus    `json:"retry_backoffs,omitempty"`
	ProviderCache     *ProviderCacheStatus    `json:"provider_cache,omitempty"`
	LLMSemaphore      SemaphoreStatus         `json:"llm_semaphore"`
	RecentExtractions int                     `json:"recent_extractions"`
	InFlight          []InFlightExtraction    `json:"in_flight,omitempty"`
}

// PendingGroupStatus describes a (agentID, graphID) group with pending messages.
type PendingGroupStatus struct {
	AgentID string `json:"agent_id"`
	GraphID string `json:"graph_id"`
}

// RetryBackoffStatus describes a group currently in retry backoff.
type RetryBackoffStatus struct {
	AgentID             string    `json:"agent_id"`
	GraphID             string    `json:"graph_id"`
	ConsecutiveFailures int       `json:"consecutive_failures"`
	NextAttempt         time.Time `json:"next_attempt"`
}

// ProviderCacheStatus describes the cached LLM provider state.
type ProviderCacheStatus struct {
	Provider   string    `json:"provider"`
	Model      string    `json:"model"`
	Source     string    `json:"source"`
	ResolvedAt time.Time `json:"resolved_at"`
	ExpiresAt  time.Time `json:"expires_at"`
}

// SemaphoreStatus describes the LLM concurrency semaphore state.
type SemaphoreStatus struct {
	Current int `json:"current_in_flight"`
	Max     int `json:"max_concurrent"`
}

// Status returns a point-in-time snapshot of the worker state.
func (d *ExtractionWorkerDeps) Status(ctx context.Context) *WorkerStatus {
	status := &WorkerStatus{}

	if d.LLMSem != nil {
		status.LLMSemaphore = SemaphoreStatus{
			Current: d.LLMSem.Current(),
			Max:     d.LLMSem.Cap(),
		}
	}

	d.mu.Lock()
	for key, rs := range d.retryTracker {
		aid, gid := splitGroupKey(key)
		status.RetryBackoffs = append(status.RetryBackoffs, RetryBackoffStatus{
			AgentID:             aid,
			GraphID:             gid,
			ConsecutiveFailures: rs.consecutiveFailures,
			NextAttempt:         rs.nextAttempt,
		})
	}
	if d.provider != nil {
		status.ProviderCache = &ProviderCacheStatus{
			Provider:   d.provider.provider.Name(),
			Model:      d.provider.model,
			Source:     d.provider.source,
			ResolvedAt: d.provider.resolvedAt,
			ExpiresAt:  d.provider.resolvedAt.Add(providerCacheTTL),
		}
	}
	d.mu.Unlock()

	if d.RawMsgStore != nil {
		groups, err := d.RawMsgStore.ListPendingGroups(ctx)
		if err == nil {
			for _, g := range groups {
				status.PendingGroups = append(status.PendingGroups, PendingGroupStatus{
					AgentID: g.AgentID,
					GraphID: g.GraphID,
				})
			}
		}
	}

	if d.DebugBuffer != nil {
		status.RecentExtractions = d.DebugBuffer.count
	}

	// Collect in-flight extractions.
	d.InFlight.Range(func(key, value any) bool {
		if f, ok := value.(*InFlightExtraction); ok {
			status.InFlight = append(status.InFlight, *f)
		}
		return true
	})

	return status
}

// splitGroupKey splits "agentID/graphID" back into its parts.
func splitGroupKey(key string) (string, string) {
	for i := len(key) - 1; i >= 0; i-- {
		if key[i] == '/' {
			return key[:i], key[i+1:]
		}
	}
	return key, ""
}

// newDebugRecord creates a debug record with a generated ID.
func newDebugRecord(agentID, graphID string) *ExtractionDebugRecord {
	return &ExtractionDebugRecord{
		ID:        uuid.Must(uuid.NewV7()).String()[:8],
		AgentID:   agentID,
		GraphID:   graphID,
		StartedAt: time.Now(),
	}
}

// addStep appends a named, timed step to the record.
func (r *ExtractionDebugRecord) addStep(name string, start time.Time, err error) {
	step := ExtractionStep{Name: name, Duration: time.Since(start)}
	if err != nil {
		step.Error = err.Error()
	}
	r.Steps = append(r.Steps, step)
}

// finalize sets the completed timestamp and optional error.
func (r *ExtractionDebugRecord) finalize(err error) {
	now := time.Now()
	r.CompletedAt = &now
	if err != nil {
		r.Error = err.Error()
	}
}
