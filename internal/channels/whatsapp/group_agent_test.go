package whatsapp

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// stubAgentStore satisfies store.AgentStore for the resolveGroupAgentUUID path.
// Only GetByKey/GetByID are exercised by the resolver; the other interface methods are left
// nil via embedding (they are never called in these tests).
type stubAgentStore struct {
	store.AgentStore
	byKey map[string]*store.AgentData
	byID  map[uuid.UUID]*store.AgentData
}

func (s stubAgentStore) GetByKey(_ context.Context, key string) (*store.AgentData, error) {
	if a, ok := s.byKey[key]; ok {
		return a, nil
	}
	return nil, fmt.Errorf("agent not found: %s", key)
}

func (s stubAgentStore) GetByID(_ context.Context, id uuid.UUID) (*store.AgentData, error) {
	if a, ok := s.byID[id]; ok {
		return a, nil
	}
	return nil, fmt.Errorf("agent not found: %s", id)
}

// agent builds an AgentData with the given key + ID (ID lives on the embedded BaseModel).
func agent(key string, id uuid.UUID) *store.AgentData {
	return &store.AgentData{BaseModel: store.BaseModel{ID: id}, AgentKey: key}
}

func newTestChannel(t *testing.T, as store.AgentStore) *Channel {
	t.Helper()
	c := &Channel{
		BaseChannel: channels.NewBaseChannel("wa-test", nil, nil),
	}
	c.SetAgentStore(as)
	return c
}

// addGroup simulates applyJoinRules writing a runtime group override into the live config
// WITHOUT rebuilding any chat→agent snapshot map (the RC1 bug condition).
func (c *Channel) addGroup(chatID string, grp *config.WhatsAppGroupConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.config.Groups == nil {
		c.config.Groups = make(map[string]*config.WhatsAppGroupConfig)
	}
	c.config.Groups[chatID] = grp
}

// TestResolveGroupAgentUUID_RuntimeOverrideNoReload is the core RC1 regression: a group whose
// agent_id override was added at runtime (e.g. by a group_join_rules match) must resolve to the
// configured agent even though ResolveGroupAgentOverrides was never re-run for it. Before the
// fix this returned "" because the chat→UUID snapshot map was stale.
func TestResolveGroupAgentUUID_RuntimeOverrideNoReload(t *testing.T) {
	felixUUID := uuid.New()
	as := stubAgentStore{
		byKey: map[string]*store.AgentData{"felix": agent("felix", felixUUID)},
		byID:  map[uuid.UUID]*store.AgentData{felixUUID: agent("felix", felixUUID)},
	}
	c := newTestChannel(t, as)

	const chatID = "120363409245069998@g.us"
	// Runtime mutation — NO ResolveGroupAgentOverrides / cache rebuild afterwards.
	c.addGroup(chatID, &config.WhatsAppGroupConfig{
		Name:           "Mini - AMC DELL IOH",
		AgentID:        "felix",
		ListenGraphID:  "project-dell-ioh",
	})

	got := c.resolveGroupAgentUUID(chatID)
	if got != felixUUID.String() {
		t.Fatalf("resolveGroupAgentUUID = %q, want felix UUID %q (runtime override must resolve without reload)", got, felixUUID)
	}
}

// TestResolveGroupAgentUUID_NoOverrideFallsBack confirms a group with no agent_id override
// returns "" so the caller stores under the channel default agent (correct existing behavior).
func TestResolveGroupAgentUUID_NoOverrideFallsBack(t *testing.T) {
	c := newTestChannel(t, stubAgentStore{byKey: map[string]*store.AgentData{}, byID: map[uuid.UUID]*store.AgentData{}})

	const chatID = "plain-group@g.us"
	c.addGroup(chatID, &config.WhatsAppGroupConfig{Name: "Plain"}) // no AgentID

	if got := c.resolveGroupAgentUUID(chatID); got != "" {
		t.Fatalf("resolveGroupAgentUUID = %q, want empty (no override → channel default fallback)", got)
	}
}

// TestResolveGroupAgentUUID_CachesAcrossCalls confirms the on-demand lookup populates the cache
// so subsequent calls do not need the store (proves no per-message DB hit after warmup).
func TestResolveGroupAgentUUID_CachesAcrossCalls(t *testing.T) {
	felixUUID := uuid.New()
	as := stubAgentStore{
		byKey: map[string]*store.AgentData{"felix": agent("felix", felixUUID)},
	}
	c := newTestChannel(t, as)
	const chatID = "cache-group@g.us"
	c.addGroup(chatID, &config.WhatsAppGroupConfig{AgentID: "felix"})

	first := c.resolveGroupAgentUUID(chatID)
	if first != felixUUID.String() {
		t.Fatalf("first resolve = %q, want %q", first, felixUUID)
	}

	// Drop the store ref; if the cache works the second call still resolves.
	c.agentKeyMu.Lock()
	c.agentStore = nil
	c.agentKeyMu.Unlock()

	second := c.resolveGroupAgentUUID(chatID)
	if second != felixUUID.String() {
		t.Fatalf("second resolve (cache hit, nil store) = %q, want %q", second, felixUUID)
	}
}

// TestResolveGroupAgentUUID_RefreshRebuildsCache confirms RefreshGroupAgentCache picks up a
// renamed/recreated agent (CacheKindAgent path) so a stale cached UUID is corrected.
func TestResolveGroupAgentUUID_RefreshRebuildsCache(t *testing.T) {
	oldUUID := uuid.New()
	as := stubAgentStore{
		byKey: map[string]*store.AgentData{"felix": agent("felix", oldUUID)},
	}
	c := newTestChannel(t, as)
	const chatID = "refresh-group@g.us"
	c.addGroup(chatID, &config.WhatsAppGroupConfig{AgentID: "felix"})

	if got := c.resolveGroupAgentUUID(chatID); got != oldUUID.String() {
		t.Fatalf("pre-refresh resolve = %q, want %q", got, oldUUID)
	}

	// Simulate agent recreate: same key, new UUID. Refresh must update the cache.
	newUUID := uuid.New()
	as.byKey["felix"] = agent("felix", newUUID)
	c.RefreshGroupAgentCache(context.Background())

	if got := c.resolveGroupAgentUUID(chatID); got != newUUID.String() {
		t.Fatalf("post-refresh resolve = %q, want new UUID %q", got, newUUID)
	}
}

// TestResolveGroupAgentUUID_UnresolvableKeyReturnsEmpty confirms a misconfigured agent_key that
// does not exist in the store yields "" (channel default fallback) rather than panicking.
func TestResolveGroupAgentUUID_UnresolvableKeyReturnsEmpty(t *testing.T) {
	c := newTestChannel(t, stubAgentStore{
		byKey: map[string]*store.AgentData{},
		byID:  map[uuid.UUID]*store.AgentData{},
	})
	const chatID = "bad-group@g.us"
	c.addGroup(chatID, &config.WhatsAppGroupConfig{AgentID: "ghost"})

	if got := c.resolveGroupAgentUUID(chatID); got != "" {
		t.Fatalf("resolve for missing agent_key = %q, want empty (fallback)", got)
	}
}
