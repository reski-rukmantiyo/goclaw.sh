package config

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestWhatsAppContactsConfig_RoundTrip verifies the per-contact DM override map marshals
// and unmarshals through the channel instance config JSONB shape (SRS 015 FR-01).
func TestWhatsAppContactsConfig_RoundTrip(t *testing.T) {
	enabled := false
	in := WhatsAppConfig{
		Contacts: map[string]*WhatsAppContactConfig{
			"6281511488487@s.whatsapp.net": {Name: "Reski", AgentID: "jarvis"},
			"6281581484242@s.whatsapp.net": {Name: "Andi", AgentID: "raka", Enabled: &enabled},
		},
	}

	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"contacts"`) {
		t.Fatalf("expected contacts key in JSON, got: %s", data)
	}

	var out WhatsAppConfig
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Contacts) != 2 {
		t.Fatalf("expected 2 contacts, got %d", len(out.Contacts))
	}
	got := out.Contacts["6281581484242@s.whatsapp.net"]
	if got == nil {
		t.Fatal("missing contact 6281581484242@s.whatsapp.net")
	}
	if got.AgentID != "raka" || got.Name != "Andi" {
		t.Fatalf("unexpected contact fields: %+v", got)
	}
	if got.Enabled == nil || *got.Enabled {
		t.Fatalf("expected enabled=false, got %+v", got.Enabled)
	}
}

// TestWhatsAppContactsConfig_NilMapOmitted verifies an empty contacts map is omitted
// from the persisted config (omitempty), so no stale `{}` payload is written.
func TestWhatsAppContactsConfig_NilMapOmitted(t *testing.T) {
	in := WhatsAppConfig{Enabled: true}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), "contacts") {
		t.Fatalf("expected contacts omitted, got: %s", data)
	}

	// A present-but-empty map is also omitted (len 0 + omitempty).
	empty := WhatsAppConfig{Contacts: map[string]*WhatsAppContactConfig{}}
	data, err = json.Marshal(empty)
	if err != nil {
		t.Fatalf("marshal empty map: %v", err)
	}
	if strings.Contains(string(data), "contacts") {
		t.Fatalf("expected empty contacts map omitted, got: %s", data)
	}
}
