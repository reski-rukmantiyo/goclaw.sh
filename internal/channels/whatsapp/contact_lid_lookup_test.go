package whatsapp

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
	wastore "go.mau.fi/whatsmeow/store"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// stubLIDStore satisfies wastore.LIDStore with a static lid→pn map.
type stubLIDStore struct {
	pnForLID map[string]string
}

func (s stubLIDStore) PutManyLIDMappings(_ context.Context, _ []wastore.LIDMapping) error {
	return nil
}
func (s stubLIDStore) PutLIDMapping(_ context.Context, _, _ types.JID) error { return nil }
func (s stubLIDStore) GetPNForLID(_ context.Context, lid types.JID) (types.JID, error) {
	if pn, ok := s.pnForLID[lid.User]; ok {
		parsed, err := types.ParseJID(pn)
		if err != nil {
			return types.EmptyJID, err
		}
		// Mirror whatsmeow's getLIDMapping: the resolved PN carries the LID's device
		// suffix (sqlstore/lidmap.go `Device: source.Device`) — the helper must strip it.
		return types.JID{User: parsed.User, Device: lid.Device, Server: parsed.Server}, nil
	}
	return types.EmptyJID, fmt.Errorf("no mapping for LID %s", lid.User)
}
func (s stubLIDStore) GetLIDForPN(_ context.Context, _ types.JID) (types.JID, error) {
	return types.EmptyJID, fmt.Errorf("not implemented")
}
func (s stubLIDStore) GetManyLIDsForPNs(_ context.Context, _ []types.JID) (map[types.JID]types.JID, error) {
	return nil, fmt.Errorf("not implemented")
}

// withLIDMap wires a whatsmeow client whose LID store maps lid→pn (live shape:
// whatsmeow_lid_map 141089709252847 → 6281511488487).
func (c *Channel) withLIDMap(t *testing.T, pnForLID map[string]string) {
	t.Helper()
	c.client = &whatsmeow.Client{Store: &wastore.Device{LIDs: stubLIDStore{pnForLID: pnForLID}}}
}

const (
	testLIDSender = "141089709252847:83@lid"
	testPhoneJID  = "6281511488487@s.whatsapp.net"
)

// TestResolveContactLookupKey_LIDResolvesToPhone: LID sender with a known mapping
// resolves to the phone JID (SRS 015 FR-08 — the live WhatsApp LID-first case).
func TestResolveContactLookupKey_LIDResolvesToPhone(t *testing.T) {
	c := newTestChannel(t, stubAgentStore{byKey: map[string]*store.AgentData{}, byID: map[uuid.UUID]*store.AgentData{}})
	c.withLIDMap(t, map[string]string{"141089709252847": testPhoneJID})

	got := c.resolveContactLookupKey(testLIDSender)
	if got != testPhoneJID {
		t.Fatalf("lookup key = %q, want %q", got, testPhoneJID)
	}
}

// TestResolveContactLookupKey_NoMappingFallsBack: LID without a mapping stays as-is
// (graceful fallback — routing then misses the override, matching pre-FR-08 behavior).
func TestResolveContactLookupKey_NoMappingFallsBack(t *testing.T) {
	c := newTestChannel(t, stubAgentStore{byKey: map[string]*store.AgentData{}, byID: map[uuid.UUID]*store.AgentData{}})
	c.withLIDMap(t, map[string]string{})

	if got := c.resolveContactLookupKey("99999999999999:1@lid"); got != "99999999999999:1@lid" {
		t.Fatalf("lookup key = %q, want unchanged LID", got)
	}
}

// TestResolveContactLookupKey_PhonePassthroughAndNilClient: phone senders skip the
// LID store entirely; a nil client (not started / tests) is a safe no-op.
func TestResolveContactLookupKey_PhonePassthroughAndNilClient(t *testing.T) {
	c := newTestChannel(t, stubAgentStore{byKey: map[string]*store.AgentData{}, byID: map[uuid.UUID]*store.AgentData{}})
	c.withLIDMap(t, map[string]string{"141089709252847": testPhoneJID})
	if got := c.resolveContactLookupKey(testPhoneJID); got != testPhoneJID {
		t.Fatalf("phone sender = %q, want passthrough", got)
	}

	nilClient := newTestChannel(t, stubAgentStore{byKey: map[string]*store.AgentData{}, byID: map[uuid.UUID]*store.AgentData{}})
	if got := nilClient.resolveContactLookupKey(testLIDSender); got != testLIDSender {
		t.Fatalf("nil client = %q, want unchanged LID", got)
	}
}

// TestResolveAgentID_DirectLIDSenderHitsPhoneKeyedOverride: end-to-end FR-08 — a DM
// whose sender arrives LID-only (no SenderAlt; the live 2026-09-06 case) routes to the
// override agent configured under the contact's PHONE JID.
func TestResolveAgentID_DirectLIDSenderHitsPhoneKeyedOverride(t *testing.T) {
	c := newTestChannel(t, stubAgentStore{byKey: map[string]*store.AgentData{}, byID: map[uuid.UUID]*store.AgentData{}})
	c.SetAgentID("jarvis")
	c.withLIDMap(t, map[string]string{"141089709252847": testPhoneJID})
	c.addContact(testPhoneJID, &config.WhatsAppContactConfig{Name: "Reski R", AgentID: "kala"})

	// chatID is the LID chat JID (LID addressing); sender is LID-only.
	if got := c.resolveAgentID(testLIDSender, testLIDSender, "direct"); got != "kala" {
		t.Fatalf("resolveAgentID(direct, LID-only sender) = %q, want kala via LID→PN resolution", got)
	}
}

// TestResolveContactAgentUUID_LIDSenderHitsPhoneKeyedOverride: the listen-only raw-storage
// path attributes a LID-only DM to the override agent's UUID (override keyed by phone JID).
func TestResolveContactAgentUUID_LIDSenderHitsPhoneKeyedOverride(t *testing.T) {
	kalaUUID := uuid.New()
	as := stubAgentStore{
		byKey: map[string]*store.AgentData{"kala": agent("kala", kalaUUID)},
		byID:  map[uuid.UUID]*store.AgentData{kalaUUID: agent("kala", kalaUUID)},
	}
	c := newTestChannel(t, as)
	c.withLIDMap(t, map[string]string{"141089709252847": testPhoneJID})
	c.addContact(testPhoneJID, &config.WhatsAppContactConfig{AgentID: "kala"})

	if got := c.resolveContactAgentUUID(testLIDSender); got != kalaUUID.String() {
		t.Fatalf("resolveContactAgentUUID(LID sender) = %q, want kala UUID %q", got, kalaUUID)
	}
}
