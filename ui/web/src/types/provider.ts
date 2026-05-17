import type {
  ChatGPTOAuthRoutingConfig,
  EffectiveChatGPTOAuthRoutingStrategy,
} from "./agent";

export interface ProviderData {
  id: string;
  name: string;
  display_name: string;
  provider_type: string;
  api_base: string;
  api_key: string; // masked "***" from server
  enabled: boolean;
  settings?: Record<string, unknown>;
  created_at: string;
  updated_at: string;
}

export interface ProviderInput {
  name: string;
  display_name?: string;
  provider_type: string;
  api_base?: string;
  api_key?: string;
  enabled?: boolean;
  settings?: Record<string, unknown>;
}

export interface ModelInfo {
  id: string;
  name?: string;
  reasoning?: ReasoningCapability;
}

export interface ProviderReasoningDefaults {
  effort?: string;
  fallback?: "downgrade" | "provider_default" | "off";
}

export interface ProviderModelsResponse {
  models: ModelInfo[];
  reasoning_defaults?: ProviderReasoningDefaults;
}

export interface ReasoningCapability {
  levels?: string[];
  default_effort?: string;
}

export interface EmbeddingSettings {
  enabled: boolean;
  model?: string;
  api_base?: string;
  dimensions?: number; // truncate output to N dims (e.g. 1536); 0/undefined = model default
}

export interface NormalizedChatGPTOAuthProviderRouting {
  strategy: EffectiveChatGPTOAuthRoutingStrategy;
  extraProviderNames: string[];
}

/** Extract embedding settings from provider.settings */
export function getEmbeddingSettings(settings?: Record<string, unknown>): EmbeddingSettings | null {
  if (!settings?.embedding) return null;
  return settings.embedding as EmbeddingSettings;
}

function normalizeProviderNames(names: unknown): string[] {
  if (!Array.isArray(names)) return [];
  return Array.from(
    new Set(
      names
        .filter((name): name is string => typeof name === "string")
        .map((name) => name.trim())
        .filter(Boolean),
    ),
  );
}

export function normalizeChatGPTOAuthStrategy(
  strategy: unknown,
): EffectiveChatGPTOAuthRoutingStrategy {
  if (strategy === "round_robin") return "round_robin";
  return "priority_order";
}

export function normalizeReasoningEffort(value: unknown): string {
  if (typeof value !== "string") return "";
  const normalized = value.trim().toLowerCase();
  return [
    "off", "auto", "none", "minimal", "low", "medium", "high", "xhigh",
  ].includes(normalized) ? normalized : "";
}

export function normalizeReasoningFallback(
  value: unknown,
): "downgrade" | "provider_default" | "off" {
  if (value === "provider_default" || value === "off") {
    return value;
  }
  return "downgrade";
}

/** Maps advanced reasoning effort levels to the legacy three-tier thinking_level. */
export function deriveLegacyThinkingLevel(effort: string): string {
  switch (effort) {
    case "low":
    case "medium":
    case "high":
      return effort;
    case "minimal":
      return "low";
    case "xhigh":
      return "high";
    default:
      return "off";
  }
}

export function getProviderReasoningDefaults(
  settings?: Record<string, unknown>,
): ProviderReasoningDefaults | null {
  const raw = settings?.reasoning_defaults;
  if (!raw || typeof raw !== "object") return null;
  const reasoning = raw as Record<string, unknown>;
  const effort = normalizeReasoningEffort(reasoning.effort) || "off";
  const fallback = normalizeReasoningFallback(reasoning.fallback);
  if (effort === "off" && fallback === "downgrade") {
    return null;
  }
  return { effort, fallback };
}

export function getChatGPTOAuthProviderRouting(
  settings?: Record<string, unknown>,
): NormalizedChatGPTOAuthProviderRouting | null {
  const rawPool = settings?.codex_pool;
  if (!rawPool || typeof rawPool !== "object") return null;
  const pool = rawPool as Record<string, unknown>;
  const strategy = normalizeChatGPTOAuthStrategy(pool.strategy);
  const extraProviderNames = normalizeProviderNames(pool.extra_provider_names);
  if (strategy === "priority_order" && extraProviderNames.length === 0) {
    return null;
  }
  return {
    strategy,
    extraProviderNames,
  };
}

export function buildProviderSettingsWithChatGPTOAuthRouting(
  settings: Record<string, unknown> | undefined,
  routing: ChatGPTOAuthRoutingConfig,
): Record<string, unknown> {
  const next: Record<string, unknown> = { ...(settings ?? {}) };
  const strategy = normalizeChatGPTOAuthStrategy(routing.strategy);
  const extraProviderNames = normalizeProviderNames(routing.extra_provider_names);

  delete next.codex_pool;
  if (extraProviderNames.length > 0) {
    next.codex_pool = {
      strategy,
      extra_provider_names: extraProviderNames,
    };
  }

  return next;
}

export function buildProviderSettingsWithReasoningDefaults(
  settings: Record<string, unknown> | undefined,
  reasoning: ProviderReasoningDefaults | null,
): Record<string, unknown> {
  const next: Record<string, unknown> = { ...(settings ?? {}) };
  const effort = normalizeReasoningEffort(reasoning?.effort) || "off";
  const fallback = normalizeReasoningFallback(reasoning?.fallback);

  delete next.reasoning_defaults;
  if (effort !== "off" || fallback !== "downgrade") {
    next.reasoning_defaults = {
      effort,
      fallback,
    };
  }

  return next;
}

// --- OpenRouter Routing (provider-level) ---

export interface OpenRouterMaxPrice {
  prompt?: number;
  completion?: number;
}

export interface OpenRouterRoutingConfig {
  order?: string[];
  allow_fallbacks?: boolean | null;
  require_parameters?: boolean | null;
  data_collection?: "allow" | "deny" | "";
  only?: string[];
  ignore?: string[];
  quantizations?: string[];
  sort?: "price" | "throughput" | "latency" | "";
  max_price?: OpenRouterMaxPrice | null;
}

export function normalizeOpenRouterRouting(
  raw: OpenRouterRoutingConfig,
): OpenRouterRoutingConfig {
  const result: OpenRouterRoutingConfig = {};
  if (Array.isArray(raw.order)) {
    const cleaned = raw.order
      .filter((s): s is string => typeof s === "string")
      .map((s) => s.trim())
      .filter(Boolean);
    if (cleaned.length > 0) result.order = cleaned;
  }
  if (raw.allow_fallbacks !== undefined && raw.allow_fallbacks !== null) {
    result.allow_fallbacks = Boolean(raw.allow_fallbacks);
  }
  if (raw.require_parameters !== undefined && raw.require_parameters !== null) {
    result.require_parameters = Boolean(raw.require_parameters);
  }
  if (raw.data_collection === "allow" || raw.data_collection === "deny") {
    result.data_collection = raw.data_collection;
  }
  for (const [key, rawArr] of [
    ["only", raw.only],
    ["ignore", raw.ignore],
    ["quantizations", raw.quantizations],
  ] as const) {
    if (Array.isArray(rawArr)) {
      const cleaned = rawArr
        .filter((s): s is string => typeof s === "string")
        .map((s) => s.trim())
        .filter(Boolean);
      if (cleaned.length > 0) (result as Record<string, string[]>)[key] = cleaned;
    }
  }
  if (
    raw.sort === "price" ||
    raw.sort === "throughput" ||
    raw.sort === "latency"
  ) {
    result.sort = raw.sort;
  }
  if (raw.max_price && typeof raw.max_price === "object") {
    const mp: OpenRouterMaxPrice = {};
    if (typeof raw.max_price.prompt === "number" && raw.max_price.prompt > 0) {
      mp.prompt = raw.max_price.prompt;
    }
    if (
      typeof raw.max_price.completion === "number" &&
      raw.max_price.completion > 0
    ) {
      mp.completion = raw.max_price.completion;
    }
    if (mp.prompt !== undefined || mp.completion !== undefined) {
      result.max_price = mp;
    }
  }
  return result;
}

export function isOpenRouterRoutingEmpty(
  cfg: OpenRouterRoutingConfig,
): boolean {
  return (
    (cfg.order?.length ?? 0) === 0 &&
    cfg.allow_fallbacks == null &&
    cfg.require_parameters == null &&
    cfg.data_collection !== "allow" &&
    cfg.data_collection !== "deny" &&
    (cfg.only?.length ?? 0) === 0 &&
    (cfg.ignore?.length ?? 0) === 0 &&
    (cfg.quantizations?.length ?? 0) === 0 &&
    cfg.sort !== "price" &&
    cfg.sort !== "throughput" &&
    cfg.sort !== "latency" &&
    cfg.max_price == null
  );
}

/** Extract OpenRouter routing config from provider.settings */
export function getOpenRouterRouting(
  settings?: Record<string, unknown>,
): OpenRouterRoutingConfig | null {
  const raw = settings?.openrouter_routing;
  if (!raw || typeof raw !== "object") return null;
  return normalizeOpenRouterRouting(raw as OpenRouterRoutingConfig);
}

/** Merge OpenRouter routing into provider settings, removing key if empty. */
export function buildProviderSettingsWithOpenRouterRouting(
  settings: Record<string, unknown> | undefined,
  routing: OpenRouterRoutingConfig,
): Record<string, unknown> {
  const next: Record<string, unknown> = { ...(settings ?? {}) };
  const normalized = normalizeOpenRouterRouting(routing);
  delete next.openrouter_routing;
  if (!isOpenRouterRoutingEmpty(normalized)) {
    next.openrouter_routing = normalized;
  }
  return next;
}
