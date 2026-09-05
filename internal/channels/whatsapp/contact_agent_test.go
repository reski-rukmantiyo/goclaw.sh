package whatsapp

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// addContact simulates a UI edit writing a per-contact DM override into the live config
// (SRS 015). No cache rebuild is performed — resolution must read the live config.
func (c *Channel) addContact(jid string, ct *config.WhatsAppContactConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.config.Contacts == nil {
		c.config.Contacts = make(map[string]*config.WhatsAppContactConfig)
	}
	c.config.Contacts[jid] = ct
}

// TestResolveAgentID_DirectContactOverride is the core SRS 015 FR-02 case: a DM from a
// contact with an agent_id override routes to the override agent, not the channel default.
func TestResolveAgentID_DirectContactOverride(t *testing.T) {
	c := newTestChannel(t, stubAgentStore{
		byKey: map[string]*store.AgentData{},
		byID:  map[uuid.UUID]*store.AgentData{},
	})
	c.SetAgentID("jarvis")

	const sender = "6281581484242@s.whatsapp.net"
	c.addContact(sender, &config.WhatsAppContactConfig{Name: "Andi", AgentID: "raka"})

	if got := c.resolveAgentID(sender, sender, "direct"); got != "raka" {
		t.Fatalf("resolveAgentID(direct, override) = %q, want raka", got)
	}
}

// TestResolveAgentID_DirectDefaults covers the no-override variants: no entry at all,
// empty agent_id, and the explicit "__default__" sentinel — all must return the channel
// default agent (no regression for unlisted contacts).
func TestResolveAgentID_DirectDefaults(t *testing.T) {
	c := newTestChannel(t, stubAgentStore{
		byKey: map[string]*store.AgentData{},
		byID:  map[uuid.UUID]*store.AgentData{},
	})
	c.SetAgentID("jarvis")

	cases := []struct {
		name    string
		sender  string
		contact *config.WhatsAppContactConfig
	}{
		{"no entry", "6281000000001@s.whatsapp.net", nil},
		{"empty agent_id", "6281000000002@s.whatsapp.net", &config.WhatsAppContactConfig{AgentID: ""}},
		{"default sentinel", "6281000000003@s.whatsapp.net", &config.WhatsAppContactConfig{AgentID: "__default__"}},
	}
	for _, tc := range cases {
		if tc.contact != nil {
			c.addContact(tc.sender, tc.contact)
		}
		if got := c.resolveAgentID(tc.sender, tc.sender, "direct"); got != "jarvis" {
			t.Fatalf("%s: resolveAgentID(direct) = %q, want channel default jarvis", tc.name, got)
		}
	}
}

// TestResolveAgentID_DirectUnresolvableAgentFallsBack: an override agent_key that no longer
// exists still returns the override key on the response path (the dispatch layer's
// Agents.Get failure drop is the last-resort guard, gateway_consumer_normal.go) — the
// listen-path UUID resolver is the one that falls back to "" (tested below).
func TestResolveAgentID_DirectUnresolvableAgent(t *testing.T) {
	c := newTestChannel(t, stubAgentStore{
		byKey: map[string]*store.AgentData{},
		byID:  map[uuid.UUID]*store.AgentData{},
	})
	c.SetAgentID("jarvis")
	const sender = "6281000000004@s.whatsapp.net"
	c.addContact(sender, &config.WhatsAppContactConfig{AgentID: "ghost"})

	if got := c.resolveAgentID(sender, sender, "direct"); got != "ghost" {
		t.Fatalf("resolveAgentID(direct, unresolvable override) = %q, want ghost (dispatch layer drops)", got)
	}
}

// TestResolveAgentID_GroupBranchUnchanged is the 003 regression guard: the group branch
// still honors group overrides and ignores the contacts map.
func TestResolveAgentID_GroupBranchUnchanged(t *testing.T) {
	c := newTestChannel(t, stubAgentStore{
		byKey: map[string]*store.AgentData{},
		byID:  map[uuid.UUID]*store.AgentData{},
	})
	c.SetAgentID("jarvis")

	const groupChat = "120363409245069998@g.us"
	const senderInGroup = "6281511488487@s.whatsapp.net"
	c.addGroup(groupChat, &config.WhatsAppGroupConfig{AgentID: "felix"})
	// A contact entry for the SENDER must not affect group routing.
	c.addContact(senderInGroup, &config.WhatsAppContactConfig{AgentID: "raka"})

	if got := c.resolveAgentID(groupChat, senderInGroup, "group"); got != "felix" {
		t.Fatalf("resolveAgentID(group) = %q, want felix (group override)", got)
	}
	// No group override → channel default even when the sender has a contact override.
	const groupNoOverride = "120363409245069999@g.us"
	if got := c.resolveAgentID(groupNoOverride, senderInGroup, "group"); got != "jarvis" {
		t.Fatalf("resolveAgentID(group, no override) = %q, want channel default jarvis", got)
	}
}

// TestResolveAgentID_DirectLIDNormalizedSender: the override lookup uses the NORMALIZED
// sender JID (phone form), not the raw LID chat JID — a LID-addressed DM from an override
// contact resolves the same override (SRS 015 FR-03).
func TestResolveAgentID_DirectLIDNormalizedSender(t *testing.T) {
	c := newTestChannel(t, stubAgentStore{
		byKey: map[string]*store.AgentData{},
		byID:  map[uuid.UUID]*store.AgentData{},
	})
	c.SetAgentID("jarvis")

	const phoneSender = "6281581484242@s.whatsapp.net"
	const lidChat = "98765432109876@lid"
	c.addContact(phoneSender, &config.WhatsAppContactConfig{AgentID: "raka"})

	// LID-mode: chat JID is @lid but the sender (normalized upstream) is the phone JID.
	if got := c.resolveAgentID(lidChat, phoneSender, "direct"); got != "raka" {
		t.Fatalf("resolveAgentID(direct, LID chat) = %q, want raka via normalized sender", got)
	}
	// Phone-mode: chat == sender — same result.
	if got := c.resolveAgentID(phoneSender, phoneSender, "direct"); got != "raka" {
		t.Fatalf("resolveAgentID(direct, phone chat) = %q, want raka", got)
	}
}

// TestResolveContactAgentUUID_RuntimeOverrideNoReload mirrors the 003 RC1 regression for
// the contacts map: a contact override added at runtime (UI edit, no reload) resolves on
// the very next listen-only DM.
func TestResolveContactAgentUUID_RuntimeOverrideNoReload(t *testing.T) {
	rakaUUID := uuid.New()
	as := stubAgentStore{
		byKey: map[string]*store.AgentData{"raka": agent("raka", rakaUUID)},
		byID:  map[uuid.UUID]*store.AgentData{rakaUUID: agent("raka", rakaUUID)},
	}
	c := newTestChannel(t, as)

	const sender = "6281581484242@s.whatsapp.net"
	c.addContact(sender, &config.WhatsAppContactConfig{AgentID: "raka"})

	if got := c.resolveContactAgentUUID(sender); got != rakaUUID.String() {
		t.Fatalf("resolveContactAgentUUID = %q, want raka UUID %q", got, rakaUUID)
	}
}

// TestResolveContactAgentUUID_NoOverrideFallsBack: no entry / "__default__" → "" so the
// listen buffer falls back to the channel default agent UUID (existing behavior).
func TestResolveContactAgentUUID_NoOverrideFallsBack(t *testing.T) {
	c := newTestChannel(t, stubAgentStore{
		byKey: map[string]*store.AgentData{},
		byID:  map[uuid.UUID]*store.AgentData{},
	})
	if got := c.resolveContactAgentUUID("6281000000009@s.whatsapp.net"); got != "" {
		t.Fatalf("no entry: resolveContactAgentUUID = %q, want empty", got)
	}
	c.addContact("6281000000010@s.whatsapp.net", &config.WhatsAppContactConfig{AgentID: "__default__"})
	if got := c.resolveContactAgentUUID("6281000000010@s.whatsapp.net"); got != "" {
		t.Fatalf("default sentinel: resolveContactAgentUUID = %q, want empty", got)
	}
}

// TestResolveContactAgentUUID_CachesAcrossCalls: on-demand lookup populates the shared
// agent_key → UUID cache; a second call resolves with a nil store (no per-message DB hit).
func TestResolveContactAgentUUID_CachesAcrossCalls(t *testing.T) {
	rakaUUID := uuid.New()
	as := stubAgentStore{byKey: map[string]*store.AgentData{"raka": agent("raka", rakaUUID)}}
	c := newTestChannel(t, as)
	const sender = "6281000000011@s.whatsapp.net"
	c.addContact(sender, &config.WhatsAppContactConfig{AgentID: "raka"})

	if got := c.resolveContactAgentUUID(sender); got != rakaUUID.String() {
		t.Fatalf("first resolve = %q, want %q", got, rakaUUID)
	}

	c.agentKeyMu.Lock()
	c.agentStore = nil
	c.agentKeyMu.Unlock()

	if got := c.resolveContactAgentUUID(sender); got != rakaUUID.String() {
		t.Fatalf("second resolve (cache hit, nil store) = %q, want %q", got, rakaUUID)
	}
}

// TestResolveContactAgentUUID_UnresolvableKeyReturnsEmpty: an override pointing at a
// deleted/renamed agent yields "" (channel default fallback) with a warn, no panic.
func TestResolveContactAgentUUID_UnresolvableKeyReturnsEmpty(t *testing.T) {
	c := newTestChannel(t, stubAgentStore{
		byKey: map[string]*store.AgentData{},
		byID:  map[uuid.UUID]*store.AgentData{},
	})
	const sender = "6281000000012@s.whatsapp.net"
	c.addContact(sender, &config.WhatsAppContactConfig{AgentID: "ghost"})

	if got := c.resolveContactAgentUUID(sender); got != "" {
		t.Fatalf("resolve for missing agent_key = %q, want empty (fallback)", got)
	}
}

// TestRefreshGroupAgentCache_WalksContacts: the cache refresh also resolves contact
// override keys so a renamed/recreated agent heals without a gateway restart
// (CacheKindAgent path, same lifecycle as group overrides per 003 §11.1).
func TestRefreshGroupAgentCache_WalksContacts(t *testing.T) {
	oldUUID := uuid.New()
	as := stubAgentStore{byKey: map[string]*store.AgentData{"raka": agent("raka", oldUUID)}}
	c := newTestChannel(t, as)
	const sender = "6281000000013@s.whatsapp.net"
	c.addContact(sender, &config.WhatsAppContactConfig{AgentID: "raka"})

	if got := c.resolveContactAgentUUID(sender); got != oldUUID.String() {
		t.Fatalf("pre-refresh resolve = %q, want %q", got, oldUUID)
	}

	// Simulate agent recreate: same key, new UUID. Refresh must update the cache.
	newUUID := uuid.New()
	as.byKey["raka"] = agent("raka", newUUID)
	c.RefreshGroupAgentCache(context.Background())

	if got := c.resolveContactAgentUUID(sender); got != newUUID.String() {
		t.Fatalf("post-refresh resolve = %q, want new UUID %q", got, newUUID)
	}
}
