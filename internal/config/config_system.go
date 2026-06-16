package config

import (
	"encoding/json"
	"strconv"
)

// ApplySystemConfigs overlays system_configs DB values onto the in-memory config.
// Called after startup seed and after config.apply/patch to keep cfg.* in sync with DB.
// Follows the same pattern as ApplyDBSecrets — non-empty DB values override config.json values.
// Keys must match those in cmd/gateway_system_config_sync.go seedConfigForContext().
func (c *Config) ApplySystemConfigs(configs map[string]string) {
	str := func(key string, dst *string) {
		if v, ok := configs[key]; ok && v != "" {
			*dst = v
		}
	}
	integer := func(key string, dst *int) {
		if v, ok := configs[key]; ok && v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				*dst = n
			}
		}
	}
	boolean := func(key string, dst **bool) {
		if v, ok := configs[key]; ok && v != "" {
			b := v == "true" || v == "1"
			*dst = &b
		}
	}
	boolDirect := func(key string, dst *bool) {
		if v, ok := configs[key]; ok && v != "" {
			*dst = v == "true" || v == "1"
		}
	}

	// Embedding
	if c.Agents.Defaults.Memory == nil {
		c.Agents.Defaults.Memory = &MemoryConfig{}
	}
	str("embedding.provider", &c.Agents.Defaults.Memory.EmbeddingProvider)
	str("embedding.model", &c.Agents.Defaults.Memory.EmbeddingModel)
	integer("embedding.max_chunk_len", &c.Agents.Defaults.Memory.MaxChunkLen)
	integer("embedding.chunk_overlap", &c.Agents.Defaults.Memory.ChunkOverlap)

	// Agent defaults
	str("agent.default_provider", &c.Agents.Defaults.Provider)
	str("agent.default_model", &c.Agents.Defaults.Model)
	integer("agent.context_window", &c.Agents.Defaults.ContextWindow)
	integer("agent.max_tool_iterations", &c.Agents.Defaults.MaxToolIterations)

	// Gateway behavior
	integer("gateway.rate_limit_rpm", &c.Gateway.RateLimitRPM)
	integer("gateway.max_message_chars", &c.Gateway.MaxMessageChars)
	str("gateway.injection_action", &c.Gateway.InjectionAction)
	integer("gateway.inbound_debounce_ms", &c.Gateway.InboundDebounceMs)
	boolean("gateway.block_reply", &c.Gateway.BlockReply)
	boolean("gateway.tool_status", &c.Gateway.ToolStatus)
	integer("gateway.task_recovery_interval_sec", &c.Gateway.TaskRecoveryIntervalSec)

	// Background workers (vault enrichment, consolidation)
	str("background.provider", &c.Gateway.BackgroundProvider)
	str("background.model", &c.Gateway.BackgroundModel)

	// Tools
	str("tools.profile", &c.Tools.Profile)
	integer("tools.rate_limit_per_hour", &c.Tools.RateLimitPerHour)
	boolean("tools.scrub_credentials", &c.Tools.ScrubCredentials)

	// MCP health
	integer("mcp.health_fail_threshold", &c.Tools.MCPHealthFailThreshold)
	integer("mcp.health_check_interval", &c.Tools.MCPHealthCheckInterval)
	integer("mcp.max_reconnect_attempts", &c.Tools.MCPMaxReconnectAttempts)
	integer("mcp.reconnect_cooldown", &c.Tools.MCPReconnectCooldown)
	integer("mcp.idle_timeout", &c.Tools.MCPIdleTimeout)

	// TTS
	str("tts.provider", &c.Tts.Provider)
	str("tts.auto", &c.Tts.Auto)
	str("tts.mode", &c.Tts.Mode)
	integer("tts.max_length", &c.Tts.MaxLength)

	// Cron
	integer("cron.max_retries", &c.Cron.MaxRetries)
	str("cron.default_timezone", &c.Cron.DefaultTimezone)

	// Pending message compaction
	if _, ok := configs["compaction.threshold"]; ok {
		if c.Channels.PendingCompaction == nil {
			c.Channels.PendingCompaction = &PendingCompactionConfig{}
		}
		pc := c.Channels.PendingCompaction
		integer("compaction.threshold", &pc.Threshold)
		integer("compaction.keep_recent", &pc.KeepRecent)
		integer("compaction.max_tokens", &pc.MaxTokens)
		str("compaction.provider", &pc.Provider)
		str("compaction.model", &pc.Model)
	}

	// Allowed paths (JSON array)
	if v, ok := configs["allowed_paths"]; ok && v != "" {
		var paths []string
		if err := json.Unmarshal([]byte(v), &paths); err == nil {
			c.Agents.Defaults.AllowedPaths = paths
		}
	}

	// Auth providers
	if _, ok := configs["auth.local.enabled"]; ok && c.Auth.Providers.Local == nil {
		c.Auth.Providers.Local = &LocalAuthConfig{}
	}
	if c.Auth.Providers.Local != nil {
		boolDirect("auth.local.enabled", &c.Auth.Providers.Local.Enabled)
	}
	if _, ok := configs["auth.entra_id.enabled"]; ok && c.Auth.Providers.EntraID == nil {
		c.Auth.Providers.EntraID = &EntraIDAuthConfig{}
	}
	if c.Auth.Providers.EntraID != nil {
		boolDirect("auth.entra_id.enabled", &c.Auth.Providers.EntraID.Enabled)
		str("auth.entra_id.client_id", &c.Auth.Providers.EntraID.ClientID)
		str("auth.entra_id.redirect_uri", &c.Auth.Providers.EntraID.RedirectURI)
		str("auth.entra_id.tenant_id", &c.Auth.Providers.EntraID.TenantID)
	}
	if _, ok := configs["auth.google.enabled"]; ok && c.Auth.Providers.Google == nil {
		c.Auth.Providers.Google = &GoogleAuthConfig{}
	}
	if c.Auth.Providers.Google != nil {
		boolDirect("auth.google.enabled", &c.Auth.Providers.Google.Enabled)
		str("auth.google.client_id", &c.Auth.Providers.Google.ClientID)
		str("auth.google.redirect_uri", &c.Auth.Providers.Google.RedirectURI)
	}
	integer("auth.session.timeout_minutes", &c.Auth.Session.TimeoutMinutes)
	boolDirect("auth.session.refresh_enabled", &c.Auth.Session.RefreshEnabled)
}
