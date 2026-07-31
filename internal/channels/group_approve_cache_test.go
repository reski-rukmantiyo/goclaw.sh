package channels

import (
	"context"
	"testing"
	"time"
)

// TestGroupApproveCache_TTL — SRS 012: the group approval cache is TTL-bounded
// so a stale entry re-validates via IsPaired (renewal + expiry enforce) instead
// of admitting a group forever.
func TestGroupApproveCache_TTL(t *testing.T) {
	c := NewBaseChannel("test", nil, nil)

	// Fresh: marked now → approved.
	c.MarkGroupApproved("g1")
	if !c.IsGroupApproved("g1") {
		t.Error("freshly-approved group not reported approved")
	}

	// Stale: inject an entry older than the TTL → not approved AND evicted.
	c.approvedGroups.Store("g2", time.Now().Add(-(groupApproveCacheTTL + time.Second)))
	if c.IsGroupApproved("g2") {
		t.Error("stale (past TTL) group still approved")
	}
	if _, ok := c.approvedGroups.Load("g2"); ok {
		t.Error("stale group not evicted from cache")
	}

	// Unknown chatID → not approved.
	if c.IsGroupApproved("unknown") {
		t.Error("unknown group reported approved")
	}

	// Fresh-again after stale eviction: a re-Mark makes it approved once more.
	c.MarkGroupApproved("g2")
	if !c.IsGroupApproved("g2") {
		t.Error("re-approved group not reported approved after re-mark")
	}
}

// TestCheckGroupPolicy_ActiveGroupRevalidatesAfterCacheTTL — SRS 012 FR-07:
// the guarantee that an active group never expires during active use rests on
// the renewal hook (IsPaired) being reached periodically. A cache HIT does NOT
// refresh the stamp, so even a group messaged constantly ages out after
// groupApproveCacheTTL and the next message re-runs IsPaired (→ sliding renewal).
func TestCheckGroupPolicy_ActiveGroupRevalidatesAfterCacheTTL(t *testing.T) {
	bc := NewBaseChannel("test", nil, nil)
	ps := newMockPairingStore()
	ps.setPaired("group:chat1", "test")
	bc.SetPairingService(ps)
	ctx := context.Background()

	// 1st message: cache empty → IsPaired called (renewal hook reached) → approved.
	if r := bc.CheckGroupPolicy(ctx, "s1", "chat1", "pairing"); r != PolicyAllow {
		t.Fatalf("1st msg: got %v want Allow", r)
	}
	if ps.isPairedCalls != 1 {
		t.Fatalf("1st msg: IsPaired calls=%d want 1", ps.isPairedCalls)
	}

	// 2nd message while cache fresh: cache HIT → IsPaired NOT called (fast path).
	bc.CheckGroupPolicy(ctx, "s2", "chat1", "pairing")
	bc.CheckGroupPolicy(ctx, "s3", "chat1", "pairing")
	if ps.isPairedCalls != 1 {
		t.Errorf("fresh-cache burst: IsPaired calls=%d want 1 (cache hits must not reach IsPaired)", ps.isPairedCalls)
	}

	// Cache ages out (inject stale stamp): next message re-validates → IsPaired
	// called again (renewal fires for an in-window device). This is what keeps an
	// active group from expiring.
	bc.approvedGroups.Store("chat1", time.Now().Add(-(groupApproveCacheTTL + time.Second)))
	if r := bc.CheckGroupPolicy(ctx, "s4", "chat1", "pairing"); r != PolicyAllow {
		t.Fatalf("post-TTL msg: got %v want Allow", r)
	}
	if ps.isPairedCalls != 2 {
		t.Errorf("post-TTL msg: IsPaired calls=%d want 2 (stale cache must re-validate)", ps.isPairedCalls)
	}
}
