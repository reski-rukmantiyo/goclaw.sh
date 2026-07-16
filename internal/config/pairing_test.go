package config

import (
	"testing"
	"time"
)

// TestPairingConfig_Durations locks the SRS 012 FR-01 TTL + renewal-window
// parsing semantics: empty → defaults; "0" → never; explicit honoured + clamped.
func TestPairingConfig_Durations(t *testing.T) {
	default30d := 30 * 24 * time.Hour

	tests := []struct {
		name       string
		ttl        string
		window     string
		wantTTL    time.Duration
		wantWindow time.Duration
	}{
		{"empty defaults", "", "", default30d, default30d / 4},
		{"explicit ttl, auto window", "168h", "", 168 * time.Hour, (168 * time.Hour) / 4},
		{"explicit ttl + window", "720h", "48h", 720 * time.Hour, 48 * time.Hour},
		{"zero ttl = never", "0", "", 0, 0},
		{"zero ttl via 0s", "0s", "0s", 0, 0},
		{"invalid ttl → default", "not-a-duration", "", default30d, default30d / 4},
		{"window clamped to ttl", "10h", "999h", 10 * time.Hour, 10 * time.Hour},
		{"explicit zero window disables renewal", "720h", "0", 720 * time.Hour, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := PairingConfig{DeviceTTL: tc.ttl, RenewalWindow: tc.window}
			if got := p.DeviceTTLDuration(); got != tc.wantTTL {
				t.Errorf("DeviceTTLDuration: got %v want %v", got, tc.wantTTL)
			}
			if got := p.RenewalWindowDuration(p.DeviceTTLDuration()); got != tc.wantWindow {
				t.Errorf("RenewalWindowDuration: got %v want %v", got, tc.wantWindow)
			}
		})
	}
}

// TestChannelsConfig_PairingDefault ensures ChannelsConfig embeds PairingConfig
// so `config.json` `channels.pairing.*` is parsed (zero-value is valid).
func TestChannelsConfig_PairingDefault(t *testing.T) {
	var c ChannelsConfig
	if got := c.Pairing.DeviceTTLDuration(); got != 30*24*time.Hour {
		t.Errorf("default DeviceTTL: got %v want 30d", got)
	}
}
