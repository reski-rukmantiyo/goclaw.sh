package sessionclear

import (
	"encoding/json"
)

// ExtractedSchedules holds parsed session clear schedules from a channel instance config.
type ExtractedSchedules struct {
	Channel *ClearSchedule
	Groups  map[string]*ClearSchedule // groupID → schedule
}

// ExtractSchedules parses session clear schedules from a channel instance's config JSONB.
func ExtractSchedules(configJSON json.RawMessage) ExtractedSchedules {
	var raw struct {
		ListenOnly   *bool          `json:"listen_only"`
		SessionClear *ClearSchedule `json:"session_clear"`
		Groups       map[string]*struct {
			ListenOnly   *bool          `json:"listen_only"`
			SessionClear *ClearSchedule `json:"session_clear"`
		} `json:"groups"`
	}
	if len(configJSON) == 0 {
		return ExtractedSchedules{}
	}
	_ = json.Unmarshal(configJSON, &raw)

	// Channel-level schedule: skip if channel is listen-only.
	var channelSchedule *ClearSchedule
	if raw.SessionClear != nil && raw.SessionClear.IsEnabled() && !(raw.ListenOnly != nil && *raw.ListenOnly) {
		channelSchedule = raw.SessionClear
	}

	result := ExtractedSchedules{
		Channel: channelSchedule,
	}

	if len(raw.Groups) > 0 {
		result.Groups = make(map[string]*ClearSchedule, len(raw.Groups))
		for gid, gc := range raw.Groups {
			if gc == nil || gc.SessionClear == nil || !gc.SessionClear.IsEnabled() {
				continue
			}
			if gc.ListenOnly != nil && *gc.ListenOnly {
				continue
			}
			result.Groups[gid] = gc.SessionClear
		}
	}

	return result
}
