package tasks

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const defaultSessionAutoCompactInterval = 5 * time.Minute
const defaultIdleGuard = 5 * time.Minute
const minMessagesToCompact = 6

// SessionCompactionTicker periodically scans for idle sessions whose estimated
// token count exceeds a configurable threshold of their context window and
// truncates their history (truncate-only, no LLM summarization).
type SessionCompactionTicker struct {
	sessions store.SessionStore
	cfg      *config.Config
	interval time.Duration

	stopCh chan struct{}
	wg     sync.WaitGroup
}

// NewSessionCompactionTicker creates a new background ticker for auto-compaction.
// intervalSec <= 0 falls back to 300s (5min).
func NewSessionCompactionTicker(sessions store.SessionStore, cfg *config.Config, intervalSec int) *SessionCompactionTicker {
	interval := defaultSessionAutoCompactInterval
	if intervalSec > 0 {
		interval = time.Duration(intervalSec) * time.Second
	}
	return &SessionCompactionTicker{
		sessions: sessions,
		cfg:      cfg,
		interval: interval,
		stopCh:   make(chan struct{}),
	}
}

// Start launches the background compaction loop.
func (t *SessionCompactionTicker) Start() {
	t.wg.Add(1)
	go t.loop()
	slog.Info("session_compaction_ticker started", "interval", t.interval)
}

// Stop signals the ticker to stop and waits for completion.
func (t *SessionCompactionTicker) Stop() {
	close(t.stopCh)
	t.wg.Wait()
	slog.Info("session_compaction_ticker stopped")
}

func (t *SessionCompactionTicker) loop() {
	defer t.wg.Done()

	// Immediate first scan.
	t.compactOverThreshold()

	ticker := time.NewTicker(t.interval)
	defer ticker.Stop()

	for {
		select {
		case <-t.stopCh:
			return
		case <-ticker.C:
			t.compactOverThreshold()
		}
	}
}

func (t *SessionCompactionTicker) compactOverThreshold() {
	threshold := t.effectiveThreshold()
	if threshold <= 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sessions, err := t.sessions.ListOverThreshold(ctx, threshold, defaultIdleGuard)
	if err != nil {
		slog.Warn("session_compaction_ticker: list over threshold failed", "error", err)
		return
	}
	if len(sessions) == 0 {
		return
	}

	keepLast := t.effectiveKeepLast()

	for _, info := range sessions {
		history := t.sessions.GetHistory(ctx, info.Key)
		if len(history) < minMessagesToCompact {
			continue
		}

		t.sessions.TruncateHistory(ctx, info.Key, keepLast)
		t.sessions.IncrementCompaction(ctx, info.Key)
		if err := t.sessions.Save(ctx, info.Key); err != nil {
			slog.Warn("session_compaction_ticker: save failed", "key", info.Key, "error", err)
			continue
		}

		slog.Info("session_auto_compact",
			"key", info.Key,
			"before", len(history),
			"after", keepLast,
			"tokens", info.EstimatedTokens,
			"window", info.ContextWindow,
			"threshold", threshold,
		)
	}
}

func (t *SessionCompactionTicker) effectiveThreshold() float64 {
	if t.cfg != nil && t.cfg.Agents.Defaults.Compaction != nil && t.cfg.Agents.Defaults.Compaction.AutoCompactThreshold > 0 {
		return t.cfg.Agents.Defaults.Compaction.AutoCompactThreshold
	}
	return config.DefaultAutoCompactThreshold
}

func (t *SessionCompactionTicker) effectiveKeepLast() int {
	if t.cfg != nil && t.cfg.Agents.Defaults.Compaction != nil && t.cfg.Agents.Defaults.Compaction.KeepLastMessages > 0 {
		return t.cfg.Agents.Defaults.Compaction.KeepLastMessages
	}
	return 4
}
