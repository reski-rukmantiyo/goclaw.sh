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
//
// FR-08 note: each message ALSO fires the member renewal (TouchGroupMember,
// sender-scoped, TTL-gated). The sender here is unpaired, so those calls
// return false and are counted below — the GROUP-row assertions track the
// delta past the member's single first-message call.
func TestCheckGroupPolicy_ActiveGroupRevalidatesAfterCacheTTL(t *testing.T) {
	bc := NewBaseChannel("test", nil, nil)
	ps := newMockPairingStore()
	ps.setPaired("group:chat1", "test")
	bc.SetPairingService(ps)
	ctx := context.Background()

	// 1st message: cache empty → member renewal (s1) + group IsPaired → Allow.
	if r := bc.CheckGroupPolicy(ctx, "s1", "chat1", "pairing"); r != PolicyAllow {
		t.Fatalf("1st msg: got %v want Allow", r)
	}
	afterFirst := ps.isPairedCalls
	if afterFirst != 2 {
		t.Fatalf("1st msg: IsPaired calls=%d want 2 (member renewal + group check)", ps.isPairedCalls)
	}

	// Burst while both caches fresh: no further IsPaired (fast paths).
	bc.CheckGroupPolicy(ctx, "s1", "chat1", "pairing")
	bc.CheckGroupPolicy(ctx, "s1", "chat1", "pairing")
	if ps.isPairedCalls != afterFirst {
		t.Errorf("fresh-cache burst: IsPaired calls=%d want %d (cache hits must not reach IsPaired)", ps.isPairedCalls, afterFirst)
	}

	// Group cache ages out (inject stale stamp; member entry left fresh so the
	// member renewal stays silent): next message re-validates the GROUP → IsPaired
	// called again (renewal fires for an in-window group device).
	bc.approvedGroups.Store("chat1", time.Now().Add(-(groupApproveCacheTTL + time.Second)))
	if r := bc.CheckGroupPolicy(ctx, "s1", "chat1", "pairing"); r != PolicyAllow {
		t.Fatalf("post-TTL msg: got %v want Allow", r)
	}
	if ps.isPairedCalls != afterFirst+1 {
		t.Errorf("post-TTL msg: IsPaired calls=%d want %d (stale group cache must re-validate)", ps.isPairedCalls, afterFirst+1)
	}
	if ps.lastIsPairedSender != "group:chat1" {
		t.Errorf("post-TTL msg: last IsPaired sender=%q want group:chat1 (group row re-validated)", ps.lastIsPairedSender)
	}
}
