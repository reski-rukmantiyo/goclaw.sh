package sessionclear

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ClearScheduler runs session clear schedules for channel instances.
type ClearScheduler struct {
	mu            sync.Mutex
	tracked       []trackedSchedule
	sessionStore  store.SessionBulkStore
	instanceStore store.ChannelInstanceStore
	defaultTZ     string
	stop          chan struct{}
	reloadCh      chan struct{}
	wg            sync.WaitGroup
}

type trackedSchedule struct {
	channelName      string
	tenantID         uuid.UUID
	groupID          string // empty = channel default
	scope            Scope  // "all"|"dm"|"group" (channel default only)
	schedule         store.CronSchedule
	action           Action
	nextRun          *time.Time
	overriddenGroups []string // group IDs with overrides (excluded from channel default)
}

// NewClearScheduler creates a scheduler that reads channel instance configs and
// executes session clear schedules.
func NewClearScheduler(sessionStore store.SessionBulkStore, instanceStore store.ChannelInstanceStore, defaultTZ string) *ClearScheduler {
	return &ClearScheduler{
		sessionStore:  sessionStore,
		instanceStore: instanceStore,
		defaultTZ:     defaultTZ,
		stop:          make(chan struct{}),
		reloadCh:      make(chan struct{}, 1),
	}
}

// Start begins the evaluation loop (1-minute ticker).
func (s *ClearScheduler) Start() {
	s.wg.Add(1)
	go s.runLoop()
}

// Stop gracefully shuts down the scheduler.
func (s *ClearScheduler) Stop() {
	close(s.stop)
	s.wg.Wait()
}

// Reload signals the scheduler to re-read channel instance configs.
func (s *ClearScheduler) Reload(ctx context.Context) {
	if err := s.doReload(ctx); err != nil {
		slog.Error("session_clear.reload_failed", "error", err)
	}
	select {
	case s.reloadCh <- struct{}{}:
	default:
	}
}

func (s *ClearScheduler) runLoop() {
	defer s.wg.Done()

	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-s.stop:
			return
		case <-s.reloadCh:
			// Reload triggered — schedules already updated via Reload()
		case now := <-ticker.C:
			s.checkAndRun(context.Background(), now)
		}
	}
}

func (s *ClearScheduler) doReload(ctx context.Context) error {
	instances, err := s.instanceStore.ListAllEnabled(ctx)
	if err != nil {
		return err
	}

	var tracked []trackedSchedule
	for _, inst := range instances {
		extracted := ExtractSchedules(inst.Config)
		tenantID := inst.TenantID
		channelName := inst.Name

		// Collect overridden group IDs for channel default exclusion.
		overriddenGroups := make([]string, 0, len(extracted.Groups))
		for gid := range extracted.Groups {
			overriddenGroups = append(overriddenGroups, gid)
		}

		// Channel default schedule.
		if extracted.Channel != nil && extracted.Channel.IsEnabled() {
			now := time.Now()
			next := store.ComputeNextRun(&extracted.Channel.Schedule, now, s.defaultTZ)
			tracked = append(tracked, trackedSchedule{
				channelName:      channelName,
				tenantID:         tenantID,
				scope:            extracted.Channel.EffectiveScope(),
				schedule:         extracted.Channel.Schedule,
				action:           extracted.Channel.Action,
				nextRun:          next,
				overriddenGroups: overriddenGroups,
			})
		}

		// Per-group overrides.
		for gid, sc := range extracted.Groups {
			now := time.Now()
			next := store.ComputeNextRun(&sc.Schedule, now, s.defaultTZ)
			tracked = append(tracked, trackedSchedule{
				channelName: channelName,
				tenantID:    tenantID,
				groupID:     gid,
				schedule:    sc.Schedule,
				action:      sc.Action,
				nextRun:     next,
			})
		}
	}

	s.mu.Lock()
	s.tracked = tracked
	s.mu.Unlock()

	slog.Info("session_clear.reloaded", "schedules", len(tracked))
	return nil
}

func (s *ClearScheduler) checkAndRun(ctx context.Context, now time.Time) {
	s.mu.Lock()
	tracked := s.tracked
	s.mu.Unlock()

	for i := range tracked {
		t := &tracked[i]
		if t.nextRun == nil || t.nextRun.After(now) {
			continue
		}

		// Build context with tenant ID.
		ctx := store.WithTenantID(ctx, t.tenantID)

		var count int
		var err error

		if t.groupID != "" {
			// Group override: clear that group's sessions.
			pattern := "agent:%:" + t.channelName + ":group:" + t.groupID + "%"
			count, err = s.sessionStore.ClearSessionsByPattern(ctx, pattern, string(t.action))
		} else {
			// Channel default: clear sessions based on scope, excluding overridden groups.
			count, err = s.runChannelClear(ctx, t)
		}

		clearLogResult(t.channelName, t.groupID, string(t.action), count, err)

		// Advance schedule.
		t.nextRun = store.ComputeNextRun(&t.schedule, now, s.defaultTZ)
	}

	// Update tracked state (nextRun advances).
	s.mu.Lock()
	s.tracked = tracked
	s.mu.Unlock()
}

func (s *ClearScheduler) runChannelClear(ctx context.Context, t *trackedSchedule) (int, error) {
	switch t.scope {
	case ScopeDM:
		// DMs only — no group exclusion needed.
		pattern := "agent:%:" + t.channelName + ":direct:%"
		return s.sessionStore.ClearSessionsByPattern(ctx, pattern, string(t.action))

	case ScopeGroup:
		// Groups only — exclude overridden groups.
		return s.clearGroupsWithExclusion(ctx, t)

	default: // ScopeAll
		// All sessions — exclude overridden groups from group portion.
		if len(t.overriddenGroups) == 0 {
			// No overrides — simple pattern match.
			pattern := "agent:%:" + t.channelName + "%"
			return s.sessionStore.ClearSessionsByPattern(ctx, pattern, string(t.action))
		}
		return s.clearAllWithExclusion(ctx, t)
	}
}

func (s *ClearScheduler) clearGroupsWithExclusion(ctx context.Context, t *trackedSchedule) (int, error) {
	pattern := "agent:%:" + t.channelName + ":group:%"
	keys, err := s.queryKeys(ctx, pattern, t.tenantID)
	if err != nil {
		return 0, err
	}
	keys = filterOutGroups(keys, t.overriddenGroups)
	if len(keys) == 0 {
		return 0, nil
	}
	return s.sessionStore.ClearSessionsByKeys(ctx, keys, string(t.action))
}

func (s *ClearScheduler) clearAllWithExclusion(ctx context.Context, t *trackedSchedule) (int, error) {
	pattern := "agent:%:" + t.channelName + "%"
	keys, err := s.queryKeys(ctx, pattern, t.tenantID)
	if err != nil {
		return 0, err
	}
	keys = filterOutGroups(keys, t.overriddenGroups)
	if len(keys) == 0 {
		return 0, nil
	}
	return s.sessionStore.ClearSessionsByKeys(ctx, keys, string(t.action))
}

func (s *ClearScheduler) queryKeys(ctx context.Context, pattern string, tid uuid.UUID) ([]string, error) {
	return s.sessionStore.QuerySessionKeys(ctx, pattern)
}

func filterOutGroups(keys []string, groupIDs []string) []string {
	if len(groupIDs) == 0 {
		return keys
	}
	filtered := keys[:0]
outer:
	for _, k := range keys {
		for _, gid := range groupIDs {
			if strings.Contains(k, ":group:"+gid) {
				continue outer
			}
		}
		filtered = append(filtered, k)
	}
	return filtered
}

func clearLogResult(channelName, groupID, action string, count int, err error) {
	kind := "channel"
	if groupID != "" {
		kind = "group"
	}
	if err != nil {
		slog.Error("session_clear.failed", "kind", kind, "channel", channelName, "group", groupID, "action", action, "error", err)
	} else if count > 0 {
		slog.Info("session_clear.executed", "kind", kind, "channel", channelName, "group", groupID, "action", action, "count", count)
	}
}
