package agent

import (
	"fmt"
)

// ErrContextGuardBlocked is returned when the context guard blocks a message.
// It carries the reason and scope so the loop can emit a polite refusal.
type ErrContextGuardBlocked struct {
	Reason string
	Scope  string
}

func (e *ErrContextGuardBlocked) Error() string {
	return fmt.Sprintf("message blocked: %s", e.Reason)
}

// Is allows errors.Is(err, ErrContextGuardBlocked) to work.
func (e *ErrContextGuardBlocked) Is(target error) bool {
	_, ok := target.(*ErrContextGuardBlocked)
	return ok
}

// GuardBlockedError creates a new ErrContextGuardBlocked.
func GuardBlockedError(reason, scope string) error {
	return &ErrContextGuardBlocked{Reason: reason, Scope: scope}
}
