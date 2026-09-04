package whatsapp

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// fakeSystemConfigs is an in-memory store.SystemConfigStore for worker tests.
type fakeSystemConfigs struct {
	mu   sync.Mutex
	data map[string]string
}

func newFakeSystemConfigs() *fakeSystemConfigs {
	return &fakeSystemConfigs{data: map[string]string{}}
}

func (f *fakeSystemConfigs) Get(ctx context.Context, key string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.data[key], nil
}

func (f *fakeSystemConfigs) Set(ctx context.Context, key, value string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.data[key] = value
	return nil
}

func (f *fakeSystemConfigs) Delete(ctx context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.data, key)
	return nil
}

func (f *fakeSystemConfigs) List(ctx context.Context) (map[string]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make(map[string]string, len(f.data))
	for k, v := range f.data {
		out[k] = v
	}
	return out, nil
}

// TestEnrichBodyWithDescriptions covers the FR-03 body format contract:
// tag + escaped <description> block, one per image tag in order, captions /
// prefixes / reply-context suffixes untouched, other media tags untouched,
// failure marker, truncation, and no-tag no-op.
func TestEnrichBodyWithDescriptions(t *testing.T) {
	cases := []struct {
		name string
		body string
		desc []string
		want string
	}{
		{
			name: "single image with caption",
			body: "[From: Alice]\n<media:image>\nlook at this",
			desc: []string{"a whiteboard with a sprint plan"},
			want: "[From: Alice]\n<media:image>\n<description>a whiteboard with a sprint plan</description>\nlook at this",
		},
		{
			name: "multiple images in order",
			body: "<media:image>\n<media:image>",
			desc: []string{"first photo", "second photo"},
			want: "<media:image>\n<description>first photo</description>\n<media:image>\n<description>second photo</description>",
		},
		{
			name: "group-history prefix preserved",
			body: "[From: Bob] [Replying to: Carol]\n<media:image>\nsending the slide",
			desc: []string{"a bar chart"},
			want: "[From: Bob] [Replying to: Carol]\n<media:image>\n<description>a bar chart</description>\nsending the slide",
		},
		{
			name: "reply-context suffix stays attached to its tag line",
			body: "<media:image> (from replied message)\nand my own pic\n<media:image>",
			desc: []string{"replied photo", "own photo"},
			want: "<media:image> (from replied message)\n<description>replied photo</description>\nand my own pic\n<media:image>\n<description>own photo</description>",
		},
		{
			name: "other media tags untouched",
			body: "<media:video>\n<media:image>\n<media:document name=\"spec.pdf\">",
			desc: []string{"a logo"},
			want: "<media:video>\n<media:image>\n<description>a logo</description>\n<media:document name=\"spec.pdf\">",
		},
		{
			name: "failure marker per failed ref",
			body: "<media:image>\n<media:image>",
			desc: []string{"a cat photo", "[analysis failed]"},
			want: "<media:image>\n<description>a cat photo</description>\n<media:image>\n<description>[analysis failed]</description>",
		},
		{
			name: "description is XML-escaped",
			body: "<media:image>",
			desc: []string{"note <b>bold</b> & \"quotes\""},
			want: "<media:image>\n<description>note &lt;b&gt;bold&lt;/b&gt; &amp; &#34;quotes&#34;</description>",
		},
		{
			name: "truncated at cap with suffix",
			body: "<media:image>",
			desc: []string{strings.Repeat("x", mediaDescriptionMaxRunes+50)},
			want: "<media:image>\n<description>" + strings.Repeat("x", mediaDescriptionMaxRunes) + " … [truncated]</description>",
		},
		{
			name: "sticker-only row (no tag) untouched",
			body: "[From: Alice]\nsticker only",
			desc: []string{"a sticker"},
			want: "[From: Alice]\nsticker only",
		},
		{
			name: "more tags than descriptions leaves extras bare",
			body: "<media:image>\n<media:image>",
			desc: []string{"only first"},
			want: "<media:image>\n<description>only first</description>\n<media:image>",
		},
		{
			name: "no descriptions no-op",
			body: "<media:image>\ncaption",
			desc: nil,
			want: "<media:image>\ncaption",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := enrichBodyWithDescriptions(tc.body, tc.desc)
			if got != tc.want {
				t.Errorf("enrichBodyWithDescriptions:\ngot  %q\nwant %q", got, tc.want)
			}
		})
	}
}

// TestImageMediaRefs verifies image-only classification with MIME fallback
// (Decision D3: sticker/video/document excluded).
func TestImageMediaRefs(t *testing.T) {
	m := store.ListenRawMessage{MediaRefs: []store.RawMediaRef{
		{MediaID: "1", MediaType: "image"},
		{MediaID: "2", MediaType: "sticker"},
		{MediaID: "3", MediaType: "video"},
		{MediaID: "4", ContentType: "image/png"}, // MediaType empty → MIME fallback
		{MediaID: "5", MediaType: "document"},
	}}
	got := imageMediaRefs(m)
	if len(got) != 2 || got[0].MediaID != "1" || got[1].MediaID != "4" {
		t.Fatalf("imageMediaRefs: got %+v want refs 1 and 4", got)
	}
}

// TestEnsureEnrichActivation verifies FR-06 activation semantics: first
// registration persists enriched_since exactly once and bulk-marks historical
// rows when backfill is off; restarts reuse the stored value and never re-mark;
// backfill on skips the bulk mark.
func TestEnsureEnrichActivation(t *testing.T) {
	t.Run("first registration writes cutoff once and bulk-marks", func(t *testing.T) {
		raw := newFakeRawMsgStore()
		cfg := newFakeSystemConfigs()

		cutoff, backfill := ensureEnrichActivation(context.Background(), MediaEnrichWorkerDeps{
			RawMsgStore: raw, SystemConfigs: cfg,
		})
		if backfill {
			t.Fatal("backfill must default off")
		}
		if raw.bulkMarkCalls != 1 {
			t.Fatalf("bulk mark calls: got %d want 1", raw.bulkMarkCalls)
		}
		if raw.bulkMarkCutoff != cutoff {
			t.Fatalf("bulk mark cutoff must equal persisted cutoff")
		}
		stored, _ := cfg.Get(context.Background(), cfgKeyEnrichedSince)
		if stored == "" {
			t.Fatal("enriched_since must be persisted")
		}

		// Restart: stored value reused (second precision — enriched_since is
		// persisted as RFC3339 without sub-second digits), no second bulk mark.
		cutoff2, _ := ensureEnrichActivation(context.Background(), MediaEnrichWorkerDeps{
			RawMsgStore: raw, SystemConfigs: cfg,
		})
		if !cutoff2.Equal(cutoff.Truncate(time.Second)) {
			t.Fatalf("restart must reuse stored cutoff: got %v want %v", cutoff2, cutoff.Truncate(time.Second))
		}
		if raw.bulkMarkCalls != 1 {
			t.Fatalf("bulk mark must run once ever, got %d calls", raw.bulkMarkCalls)
		}
	})

	t.Run("backfill on skips bulk mark", func(t *testing.T) {
		raw := newFakeRawMsgStore()
		cfg := newFakeSystemConfigs()
		_ = cfg.Set(context.Background(), cfgKeyMediaBackfillEnabled, "true")

		_, backfill := ensureEnrichActivation(context.Background(), MediaEnrichWorkerDeps{
			RawMsgStore: raw, SystemConfigs: cfg,
		})
		if !backfill {
			t.Fatal("backfill flag must be true")
		}
		if raw.bulkMarkCalls != 0 {
			t.Fatalf("bulk mark must be skipped when backfill on, got %d calls", raw.bulkMarkCalls)
		}
	})
}

// TestMediaAnalysisEnabled verifies the master gate semantics: default on,
// "false"/"0" off.
func TestMediaAnalysisEnabled(t *testing.T) {
	cfg := newFakeSystemConfigs()
	if !mediaAnalysisEnabled(context.Background(), cfg) {
		t.Fatal("default must be enabled")
	}
	for _, v := range []string{"false", "0"} {
		_ = cfg.Set(context.Background(), cfgKeyMediaAnalysisEnabled, v)
		if mediaAnalysisEnabled(context.Background(), cfg) {
			t.Fatalf("value %q must disable", v)
		}
	}
}

// fakeRawMsgStore records the calls the worker makes, for state-machine tests.
// The embedded interface is nil — only the enrichment-path methods are
// exercised by these tests.
type fakeRawMsgStore struct {
	store.ListenRawMessageStore

	mu             sync.Mutex
	pendingRows    []store.ListenRawMessage
	bulkMarkCalls  int
	bulkMarkCutoff time.Time
	markedByIDs    []uuid.UUID
	enriched       map[uuid.UUID]string
}

func newFakeRawMsgStore() *fakeRawMsgStore {
	return &fakeRawMsgStore{enriched: map[uuid.UUID]string{}}
}

func (f *fakeRawMsgStore) ListPendingMediaEnrichment(ctx context.Context, filter store.MediaEnrichFilter) ([]store.ListenRawMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pendingRows, nil
}

func (f *fakeRawMsgStore) MarkMediaAnalyzedByIDs(ctx context.Context, ids []uuid.UUID) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.markedByIDs = append(f.markedByIDs, ids...)
	return int64(len(ids)), nil
}

func (f *fakeRawMsgStore) MarkMediaAnalyzedBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bulkMarkCalls++
	f.bulkMarkCutoff = cutoff
	return 0, nil
}

func (f *fakeRawMsgStore) MarkMediaEnriched(ctx context.Context, id uuid.UUID, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.enriched[id] = body
	return nil
}
