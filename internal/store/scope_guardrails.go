package store

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ScopeGuardrailsConfig constrains an agent to a defined conversational scope.
// Stored in AgentData.OtherConfig JSONB under key "scope_guardrails".
type ScopeGuardrailsConfig struct {
	Enabled          bool     `json:"enabled"`
	Enforcement      string   `json:"enforcement,omitempty"`        // "soft" (default) | "strict"
	ScopeDescription string   `json:"scope_description,omitempty"`  // natural language domain description
	AllowedTopics    []string `json:"allowed_topics,omitempty"`     // domains agent MAY discuss
	DeniedTopics     []string `json:"denied_topics,omitempty"`      // domains agent MUST NOT discuss
	OffTopicResponse string   `json:"off_topic_response,omitempty"` // custom decline message
}

// ParseScopeGuardrails returns scope guardrails config from OtherConfig JSONB.
// Returns nil when not configured or disabled. Auto-derives ScopeDescription
// from Frontmatter and AgentDescription when empty.
func (a *AgentData) ParseScopeGuardrails() *ScopeGuardrailsConfig {
	if len(a.OtherConfig) == 0 {
		return nil
	}
	var bag map[string]json.RawMessage
	if json.Unmarshal(a.OtherConfig, &bag) != nil {
		return nil
	}
	raw, ok := bag["scope_guardrails"]
	if !ok {
		return nil
	}
	var cfg ScopeGuardrailsConfig
	if json.Unmarshal(raw, &cfg) != nil {
		return nil
	}
	if !cfg.Enabled {
		return nil
	}
	// Default enforcement to "strict"
	if cfg.Enforcement == "" {
		cfg.Enforcement = "strict"
	}
	// Normalize: only "soft" or "strict"
	if cfg.Enforcement != "soft" && cfg.Enforcement != "strict" {
		cfg.Enforcement = "strict"
	}
	// Auto-derive scope description from agent metadata when empty
	if cfg.ScopeDescription == "" {
		var parts []string
		if a.Frontmatter != "" {
			parts = append(parts, a.Frontmatter)
		}
		if a.AgentDescription != "" {
			parts = append(parts, a.AgentDescription)
		}
		if len(parts) > 0 {
			cfg.ScopeDescription = strings.Join(parts, ". ")
		} else if a.DisplayName != "" {
			cfg.ScopeDescription = fmt.Sprintf("Agent: %s", a.DisplayName)
		}
	}
	return &cfg
}
