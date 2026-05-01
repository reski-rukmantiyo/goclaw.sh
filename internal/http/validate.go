package http

import (
	"fmt"
	"log/slog"
	"regexp"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
)

var slugRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// isValidSlug checks whether s matches the slug format: lowercase alphanumeric + hyphens,
// cannot start or end with a hyphen.
func isValidSlug(s string) bool {
	return slugRe.MatchString(s)
}

// filterAllowedKeys returns a new map containing only keys present in the allowlist.
// Defense-in-depth: prevents column injection and unauthorized field updates.
func filterAllowedKeys(updates map[string]any, allowed map[string]bool) map[string]any {
	filtered := make(map[string]any, len(updates))
	for k, v := range updates {
		if allowed[k] {
			filtered[k] = v
		} else {
			slog.Warn("security.filtered_unknown_field", "field", k)
		}
	}
	return filtered
}

// validateAgentTTSParams is a thin wrapper around audio.ValidateAgentTTSParams
// so HTTP handlers can call it without importing the audio package directly.
// The allow-list is owned by internal/audio (single source of truth, Action D).
func validateAgentTTSParams(ttsParams map[string]any) error {
	return audio.ValidateAgentTTSParams(ttsParams)
}

// validateScopeGuardrails validates the scope_guardrails object within other_config.
func validateScopeGuardrails(sg map[string]any) error {
	// Validate enforcement value
	if enforcement, ok := sg["enforcement"]; ok && enforcement != nil {
		e, ok := enforcement.(string)
		if !ok {
			return fmt.Errorf("scope_guardrails.enforcement must be a string")
		}
		if e != "soft" && e != "strict" {
			return fmt.Errorf("scope_guardrails.enforcement must be \"soft\" or \"strict\"")
		}
	}
	// Validate allowed_topics is a string array
	if topics, ok := sg["allowed_topics"]; ok && topics != nil {
		if _, ok := topics.([]any); !ok {
			return fmt.Errorf("scope_guardrails.allowed_topics must be an array")
		}
	}
	// Validate denied_topics is a string array
	if topics, ok := sg["denied_topics"]; ok && topics != nil {
		if _, ok := topics.([]any); !ok {
			return fmt.Errorf("scope_guardrails.denied_topics must be an array")
		}
	}
	// Validate off_topic_response length
	if resp, ok := sg["off_topic_response"]; ok && resp != nil {
		s, ok := resp.(string)
		if !ok {
			return fmt.Errorf("scope_guardrails.off_topic_response must be a string")
		}
		if len(s) > 500 {
			return fmt.Errorf("scope_guardrails.off_topic_response must be at most 500 characters")
		}
	}
	// When enabled, require at least one scope definition
	if enabled, ok := sg["enabled"]; ok {
		if e, ok := enabled.(bool); ok && e {
			hasScope := false
			if desc, ok := sg["scope_description"]; ok && desc != nil {
				if s, ok := desc.(string); ok && s != "" {
					hasScope = true
				}
			}
			if topics, ok := sg["allowed_topics"]; ok {
				if arr, ok := topics.([]any); ok && len(arr) > 0 {
					hasScope = true
				}
			}
			if topics, ok := sg["denied_topics"]; ok {
				if arr, ok := topics.([]any); ok && len(arr) > 0 {
					hasScope = true
				}
			}
			if !hasScope {
				return fmt.Errorf("scope_guardrails requires at least one of: scope_description, allowed_topics, or denied_topics when enabled")
			}
		}
	}
	return nil
}

// --- Field allowlists for update endpoints ---
// Each map lists the columns that HTTP clients may update.
// Immutable fields (id, owner_id, created_at, deleted_at) are excluded.

var agentAllowedFields = map[string]bool{
	"agent_key": true, "agent_type": true, "display_name": true,
	"provider": true, "model": true, "status": true,
	"context_window": true, "max_tool_iterations": true,
	"workspace": true,
	"frontmatter": true, "compaction_config": true,
	"memory_config": true, "other_config": true, "tools_config": true,
	"sandbox_config": true, "context_pruning": true,
	"is_default": true, "budget_monthly_cents": true, "subagents_config": true,
	// Promoted from other_config
	"emoji": true, "agent_description": true, "thinking_level": true, "max_tokens": true,
	"self_evolve": true, "skill_evolve": true, "skill_nudge_interval": true,
	"reasoning_config": true, "workspace_sharing": true, "chatgpt_oauth_routing": true,
	"shell_deny_groups": true, "kg_dedup_config": true,
}

var providerAllowedFields = map[string]bool{
	"name": true, "provider_type": true, "api_key": true,
	"api_base": true, "base_url": true, "default_model": true,
	"extra_headers": true, "config": true, "enabled": true,
	"display_name": true, "display_order": true, "settings": true,
}

var customToolAllowedFields = map[string]bool{
	"name": true, "description": true, "command": true,
	"parameters": true, "agent_id": true, "env": true,
	"tags": true, "requires": true, "timeout_seconds": true,
	"enabled": true,
}

var mcpServerAllowedFields = map[string]bool{
	"name": true, "transport": true, "command": true, "args": true,
	"url": true, "api_key": true, "env": true, "headers": true,
	"enabled": true, "tool_prefix": true, "timeout_sec": true,
	"agent_id": true, "config": true, "settings": true,
}

var channelInstanceAllowedFields = map[string]bool{
	"name": true, "channel_type": true, "credentials": true, "agent_id": true,
	"enabled": true, "group_policy": true, "allow_from": true,
	"metadata": true, "webhook_secret": true, "config": true,
	"display_name": true,
}
