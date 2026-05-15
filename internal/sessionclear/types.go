package sessionclear

import (
	"fmt"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// Action defines what happens when a session clear schedule fires.
type Action string

const (
	ActionReset  Action = "reset"  // clear history, keep session
	ActionDelete Action = "delete" // remove session entirely
)

// Scope controls which sessions are affected by a channel-level clear schedule.
type Scope string

const (
	ScopeAll   Scope = "all"   // DMs + groups (default)
	ScopeDM    Scope = "dm"    // DM sessions only
	ScopeGroup Scope = "group" // group sessions only
)

// ClearSchedule defines when and how sessions are cleared for a channel or group.
type ClearSchedule struct {
	Enabled  *bool              `json:"enabled,omitempty"`
	Schedule store.CronSchedule `json:"schedule"`
	Action   Action             `json:"action"`
	// Scope controls which session types are cleared (channel default only).
	// Group overrides always target that group's sessions regardless of this field.
	Scope Scope `json:"scope,omitempty"`
}

// IsEnabled returns true unless explicitly set to false.
func (s *ClearSchedule) IsEnabled() bool {
	return s == nil || s.Enabled == nil || *s.Enabled
}

// EffectiveScope returns the scope, defaulting to ScopeAll.
func (s *ClearSchedule) EffectiveScope() Scope {
	if s.Scope == "" {
		return ScopeAll
	}
	return s.Scope
}

// ValidateClearSchedule checks that a ClearSchedule is structurally valid.
// isGroup should be true for per-group schedules (scope field is ignored).
func ValidateClearSchedule(s *ClearSchedule, isGroup bool) error {
	if s == nil {
		return nil
	}

	// Validate action.
	switch s.Action {
	case ActionReset, ActionDelete:
	default:
		return fmt.Errorf("invalid session_clear action: %q (must be 'reset' or 'delete')", s.Action)
	}

	// Validate scope (only for channel-level schedules).
	if !isGroup {
		switch s.Scope {
		case "", ScopeAll, ScopeDM, ScopeGroup:
		default:
			return fmt.Errorf("invalid session_clear scope: %q (must be 'all', 'dm', or 'group')", s.Scope)
		}
	}

	// Validate schedule.
	return store.ValidateCronSchedule(&s.Schedule)
}
