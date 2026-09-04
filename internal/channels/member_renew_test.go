package channels

import (
	"context"
	"testing"
	"time"
)

// TestTouchGroupMember_RenewsOwnPairing — SRS 012 FR-08: a member active in a
// group renews their own paired-device expiry (IsPaired is the renewal hook)
// even when the group policy is "open" (which otherwise never reaches IsPaired).
func TestTouchGroupMember_RenewsOwnPairing(t *testing.T) {
	bc := NewBaseChannel("test", nil, nil)
	ps := newMockPairingStore()
	ps.setPaired("member1", "test")
	bc.SetPairingService(ps)
	ctx := context.Background()

	// "open" group policy: group admitted without IsPaired for the GROUP, but
	// the member's own renewal must still fire.
	if r := bc.CheckGroupPolicy(ctx, "member1", "chat1", "open"); r != PolicyAllow {
		t.Fatalf("open policy: got %v want Allow", r)
	}
	if ps.isPairedCalls != 1 {
		t.Errorf("open policy: member IsPaired calls=%d want 1 (member renewal must fire)", ps.isPairedCalls)
	}
	if ps.lastIsPairedSender != "member1" {
		t.Errorf("member renewal checked sender %q want %q (member's own row, not group:)", ps.lastIsPairedSender, "member1")
	}
}

// TestTouchGroupMember_RateLimited — the member renewal is TTL-bounded: a
// burst of messages from the same member triggers one IsPaired, then the cache
// gates repeats until memberRenewCacheTTL elapses.
func TestTouchGroupMember_RateLimited(t *testing.T) {
	bc := NewBaseChannel("test", nil, nil)
	ps := newMockPairingStore()
	ps.setPaired("member1", "test")
	bc.SetPairingService(ps)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		bc.CheckGroupPolicy(ctx, "member1", "chat1", "open")
	}
	if ps.isPairedCalls != 1 {
		t.Errorf("burst of 5: IsPaired calls=%d want 1 (memberRenewCacheTTL must gate repeats)", ps.isPairedCalls)
	}

	// Stale entry (inject old stamp): next message renews again.
	bc.memberRenewed.Store("member1", time.Now().Add(-(memberRenewCacheTTL + time.Second)))
	bc.CheckGroupPolicy(ctx, "member1", "chat1", "open")
	if ps.isPairedCalls != 2 {
		t.Errorf("post-TTL: IsPaired calls=%d want 2 (stale entry must re-validate)", ps.isPairedCalls)
	}
}

// TestTouchGroupMember_UnpairedMemberStillRenews — TouchGroupMember is
// best-effort and never gates the message: an unpaired member under "open"
// policy is still admitted (IsPaired returning false must not flip the verdict).
func TestTouchGroupMember_UnpairedMemberStillRenews(t *testing.T) {
	bc := NewBaseChannel("test", nil, nil)
	ps := newMockPairingStore() // no paired devices
	bc.SetPairingService(ps)
	ctx := context.Background()

	if r := bc.CheckGroupPolicy(ctx, "stranger", "chat1", "open"); r != PolicyAllow {
		t.Fatalf("open policy unpaired member: got %v want Allow (member renewal must not gate the message)", r)
	}
	if ps.isPairedCalls != 1 {
		t.Errorf("unpaired member: IsPaired calls=%d want 1 (renewal attempt still fires)", ps.isPairedCalls)
	}
}

// TestTouchGroupMember_NoPairingService — nil pairing service: renewal is a
// no-op, policy still resolves (no panic).
func TestTouchGroupMember_NoPairingService(t *testing.T) {
	bc := NewBaseChannel("test", nil, nil)
	if r := bc.CheckGroupPolicy(context.Background(), "member1", "chat1", "open"); r != PolicyAllow {
		t.Fatalf("nil pairing service: got %v want Allow", r)
	}
}

// TestClearGroupMemberRenewal — explicit clear forces the next message to
// re-validate immediately (e.g. after revoke).
func TestClearGroupMemberRenewal(t *testing.T) {
	bc := NewBaseChannel("test", nil, nil)
	ps := newMockPairingStore()
	ps.setPaired("member1", "test")
	bc.SetPairingService(ps)
	ctx := context.Background()

	bc.CheckGroupPolicy(ctx, "member1", "chat1", "open")
	bc.ClearGroupMemberRenewal("member1")
	bc.CheckGroupPolicy(ctx, "member1", "chat1", "open")
	if ps.isPairedCalls != 2 {
		t.Errorf("after ClearGroupMemberRenewal: IsPaired calls=%d want 2", ps.isPairedCalls)
	}
}
