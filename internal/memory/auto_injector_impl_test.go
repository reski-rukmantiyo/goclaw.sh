package memory

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// fakeEpisodicStore implements only the methods the auto-injector calls
// (Search/Get/RecordRecall); the rest of the interface is embedded as nil and
// would panic if touched (it isn't in these tests).
type fakeEpisodicStore struct {
	store.EpisodicStore

	mu           sync.Mutex
	searchOpts   store.EpisodicSearchOptions
	lastQuery    string
	searchCalls  int
	searchResult []store.EpisodicSearchResult
	searchErr    error

	summaries map[string]*store.EpisodicSummary // id -> full summary (for Get)

	recMu     sync.Mutex
	recorded  []recallSample
	recordErr error
}

type recallSample struct {
	id    string
	score float64
}

func (f *fakeEpisodicStore) Search(_ context.Context, query, _, _ string, opts store.EpisodicSearchOptions) ([]store.EpisodicSearchResult, error) {
	f.mu.Lock()
	f.searchOpts = opts
	f.lastQuery = query
	f.searchCalls++
	f.mu.Unlock()
	return f.searchResult, f.searchErr
}

func (f *fakeEpisodicStore) Get(_ context.Context, id string) (*store.EpisodicSummary, error) {
	if s, ok := f.summaries[id]; ok {
		return s, nil
	}
	return nil, errors.New("not found")
}

func (f *fakeEpisodicStore) RecordRecall(_ context.Context, id string, score float64) error {
	f.recMu.Lock()
	defer f.recMu.Unlock()
	f.recorded = append(f.recorded, recallSample{id: id, score: score})
	return f.recordErr
}

// waitForRecorded polls until n recall samples arrive or times out (the injector
// records recall in a background goroutine).
func (f *fakeEpisodicStore) waitForRecorded(t *testing.T, n int) {
	t.Helper()
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		f.recMu.Lock()
		got := len(f.recorded)
		f.recMu.Unlock()
		if got >= n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func baseParams(msg string) InjectParams {
	return InjectParams{
		AgentID:    uuid.New().String(),
		TenantID:   uuid.New().String(),
		UserMessage: msg,
		Enabled:    true,
		MaxEntries: 5,
		MaxTokens:  200,
		Threshold:  0.3,
		L1Depth:    2,
		L1PerHitMaxTokens: 120,
	}
}

func tenantCtx() context.Context {
	return store.WithTenantID(context.Background(), uuid.New())
}

// FR-02: Enabled=false → empty result, no search.
func TestInject_DisabledReturnsEmpty(t *testing.T) {
	f := &fakeEpisodicStore{
		searchResult: []store.EpisodicSearchResult{{EpisodicID: "x", L0Abstract: "abs", Score: 0.9}},
	}
	a := NewAutoInjector(f, nil).(*pgAutoInjector)

	p := baseParams("tell me about the deployment process")
	p.Enabled = false
	res, err := a.Inject(context.Background(), p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Section != "" || res.Injected != 0 {
		t.Errorf("disabled inject must return empty, got section=%q injected=%d", res.Section, res.Injected)
	}
	if f.searchCalls != 0 {
		t.Errorf("disabled inject must not search, got %d calls", f.searchCalls)
	}
}

// FR-03: top hit surfaces an L1 summary fragment in addition to the L0 abstract.
func TestInject_SurfacesL1SummaryForTopHit(t *testing.T) {
	f := &fakeEpisodicStore{
		searchResult: []store.EpisodicSearchResult{
			{EpisodicID: "ep1", L0Abstract: "L0 abstract one", Score: 0.9},
		},
		summaries: map[string]*store.EpisodicSummary{
			"ep1": {Summary: "DETAILED SUMMARY ABOUT THE IOH DEPLOYMENT PLAN"},
		},
	}
	a := NewAutoInjector(f, nil).(*pgAutoInjector)

	res, err := a.Inject(tenantCtx(), baseParams("tell me about the deployment process"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Injected != 1 {
		t.Fatalf("expected 1 injected, got %d", res.Injected)
	}
	if !contains(res.Section, "L0 abstract one") {
		t.Errorf("section must contain the L0 abstract: %q", res.Section)
	}
	if !contains(res.Section, "DETAILED SUMMARY") {
		t.Errorf("FR-03: section must contain the L1 summary fragment: %q", res.Section)
	}
}

// FR-03: empty Summary falls back to L0 abstract only (no empty injection).
func TestInject_EmptySummaryFallsBackToAbstract(t *testing.T) {
	f := &fakeEpisodicStore{
		searchResult: []store.EpisodicSearchResult{
			{EpisodicID: "ep1", L0Abstract: "only abstract", Score: 0.9},
		},
		summaries: map[string]*store.EpisodicSummary{"ep1": {Summary: ""}},
	}
	a := NewAutoInjector(f, nil).(*pgAutoInjector)

	res, err := a.Inject(tenantCtx(), baseParams("tell me about the deployment process"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !contains(res.Section, "only abstract") {
		t.Errorf("section must contain the abstract: %q", res.Section)
	}
}

// FR-04: a continuity query broadens the search and labels the section.
func TestInject_ContinuityBroadensSearchAndLabels(t *testing.T) {
	f := &fakeEpisodicStore{
		searchResult: []store.EpisodicSearchResult{
			{EpisodicID: "ep1", L0Abstract: "monday work", Score: 0.8},
		},
	}
	a := NewAutoInjector(f, nil).(*pgAutoInjector)

	res, err := a.Inject(tenantCtx(), baseParams("what did we discuss on Monday?"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	f.mu.Lock()
	maxRes := f.searchOpts.MaxResults
	vw := f.searchOpts.VectorWeight
	tw := f.searchOpts.TextWeight
	minScore := f.searchOpts.MinScore
	f.mu.Unlock()
	// maxEntries=5 → broadened search max = 5*4 = 20.
	if maxRes != 20 {
		t.Errorf("FR-04: continuity query must broaden search to 20, got %d", maxRes)
	}
	// Continuity biases toward vector recall (FTS strict-AND often misses) and
	// lowers the bar so semantic hits surface.
	if vw != 0.6 || tw != 0.4 {
		t.Errorf("FR-04: continuity must use vector-favorable weights vw=0.6 tw=0.4, got vw=%v tw=%v", vw, tw)
	}
	if minScore > 0.16 {
		t.Errorf("FR-04: continuity must lower MinScore to ~0.15, got %v", minScore)
	}
	if !contains(res.Section, "Recalled Prior Context") {
		t.Errorf("FR-04: section must be labeled as recalled prior context: %q", res.Section)
	}
}

// FR-04: a non-continuity, non-trivial query does NOT broaden the search.
func TestInject_NonContinuityDoesNotBroaden(t *testing.T) {
	f := &fakeEpisodicStore{
		searchResult: []store.EpisodicSearchResult{
			{EpisodicID: "ep1", L0Abstract: "abstract", Score: 0.8},
		},
	}
	a := NewAutoInjector(f, nil).(*pgAutoInjector)

	_, err := a.Inject(tenantCtx(), baseParams("please write me a short poem about cats"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	f.mu.Lock()
	maxRes := f.searchOpts.MaxResults
	vw := f.searchOpts.VectorWeight
	tw := f.searchOpts.TextWeight
	f.mu.Unlock()
	// maxEntries=5 → normal search max = 5*2 = 10 (not broadened).
	if maxRes != 10 {
		t.Errorf("non-continuity query must not broaden search; want 10, got %d", maxRes)
	}
	// Non-continuity keeps the FTS-favoured defaults.
	if vw != 0.3 || tw != 0.7 {
		t.Errorf("non-continuity must use FTS-favoured weights vw=0.3 tw=0.7, got vw=%v tw=%v", vw, tw)
	}
}

// FR-05: injected episodes are recorded for recall bookkeeping.
func TestInject_RecordsRecallForInjectedHits(t *testing.T) {
	f := &fakeEpisodicStore{
		searchResult: []store.EpisodicSearchResult{
			{EpisodicID: "ep1", L0Abstract: "abstract one", Score: 0.9},
			{EpisodicID: "ep2", L0Abstract: "abstract two", Score: 0.7},
		},
	}
	a := NewAutoInjector(f, nil).(*pgAutoInjector)

	if _, err := a.Inject(tenantCtx(), baseParams("tell me about the deployment process")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	f.waitForRecorded(t, 2)

	f.recMu.Lock()
	defer f.recMu.Unlock()
	if len(f.recorded) != 2 {
		t.Fatalf("FR-05: expected 2 recorded recalls, got %d", len(f.recorded))
	}
	got := map[string]bool{}
	for _, r := range f.recorded {
		got[r.id] = true
	}
	if !got["ep1"] || !got["ep2"] {
		t.Errorf("FR-05: expected ep1+ep2 recorded, got %v", f.recorded)
	}
}

// FR-05: no tenant in ctx → recordRecall is skipped (best-effort, no panic).
func TestInject_RecordRecallSkippedWithoutTenant(t *testing.T) {
	f := &fakeEpisodicStore{
		searchResult: []store.EpisodicSearchResult{
			{EpisodicID: "ep1", L0Abstract: "abstract one", Score: 0.9},
		},
	}
	a := NewAutoInjector(f, nil).(*pgAutoInjector)

	// No tenant in ctx.
	if _, err := a.Inject(context.Background(), baseParams("tell me about the deployment process")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	f.waitForRecorded(t, 1)
	f.recMu.Lock()
	defer f.recMu.Unlock()
	if len(f.recorded) != 0 {
		t.Errorf("recordRecall must be skipped without a tenant, got %d", len(f.recorded))
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
