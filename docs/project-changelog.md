# Project Changelog

Significant changes, features, and fixes in reverse chronological order.

---

## v3.11.3E-rclaw — 2026-05-17

### Features

- **Session Clear Scheduler** — Automated session clearing via `sessionclear.ClearScheduler`. Supports channel-level and per-group schedules stored in channel instance config JSONB. Actions: reset (clear history) / delete (remove session). Scopes: all/dm/group. Listen-only channels and groups excluded. 1-minute evaluation ticker. Subscribes to channel reload events.
- **Raw Message Embedding Pipeline** — Full pipeline from WhatsApp listen-only message capture through KG extraction (batch 50, max 3 concurrent) to embedding worker (separate worker, configurable batch/concurrent/poll/chunk/overlap) to `raw_message_chunks` with hybrid FTS + vector + RRF search.
- **Shared Knowledge Search** — New `shared_knowledge_search` tool enabling two-phase cross-scope search (initial query then entity drill-down). Hidden when no shared KG scopes configured. Date range extraction support.
- **OpenRouter Provider Routing** — New `OpenRouterRoutingConfig` stored in provider settings JSONB. Fields: order, allow_fallbacks, require_parameters, data_collection, only, ignore, quantizations, sort, max_price. Injected at request time as `provider` object.
- **OpenRouter Unified Reasoning** — Sends `{"reasoning": {"effort": "<level>"}}` object (not top-level string). Explicit `"none"` for disabled thinking.
- **Tenant-scoped Provider Cache** — Provider create/update/delete emits tenant-scoped `cache:provider` invalidation events.
- **Embedding HTTP Endpoints** — `GET /v1/embeddings`, `POST /v1/embeddings/delete`, `POST /v1/embeddings/delete-by-chat`, `POST /v1/embeddings/re-embed`.

### Fixes

- Session clear removed when channel is set to listen-only.
- Listen-only channels excluded from session clear schedules.
- `shared_knowledge_search` hidden when no shared KG scopes configured.
- Inherited WhatsApp groups filtered from bound channels section.
- Tenant context included in provider lookup; debounced input syncing for routing configuration.

### Migrations

- **PG:** `000064_raw_message_chunks` — `raw_message_chunks` table (HNSW on embedding, GIN on tsv), `embedded_at` column on `listen_raw_messages`.
- **PG:** `000065_vector_dimensions_768` — Resize all vector columns from 1536 to 768 dimensions. Drops HNSW indexes, clears embedding cache, alters columns, recreates indexes.
- **PG:** `000066_raw_msg_chunks_text_columns` — Additional text columns on `raw_message_chunks`.
- **PG:** `000067_embedded_chunks` — `embedded_chunks` field on `usage_snapshots`.
- **PG:** `000068_add_openrouter_routing` — `openrouter_routing` key support in provider settings JSONB.

### Upgrade notes

- All cached embeddings are cleared by migration 000065 and will regenerate on next embedding pass.
- OpenRouter routing config moved from agent table to provider settings JSONB. Any agent-level routing config must be re-entered at the provider level.
- Embedding dimension changed from 1536 to 768 to match embeddinggemma-300m model output.

---

## v3.11.3 — 2026-04-26

### Fixes

- **`goclaw providers verify`** — empty body now triggers ping mode (provider registered/reachable check) and returns `{valid:true}` for registered providers. New `--model <alias>` flag for chat-verify against a specific model. CLI response parser switched from stale `{success, models}` to `{valid, error}`. Onboard auto-verify path fixed identically (was silently printing "FAILED" on every successful provider creation). (#1034)
- **`goclaw providers delete`** — succeeds when referenced by `agent_heartbeats`. FK changed to `ON DELETE SET NULL`; `DeleteProvider` (PG + SQLite) now wraps in a transaction that also disables affected heartbeats so the next scheduler tick cannot fire stale config. `slog.Warn("heartbeat.provider_cleared")` emitted with the disabled count. (#1034)
- **`goclaw doctor`** — provider rows with empty `display_name` now render the canonical `name` instead of a blank line. Query switched from `COALESCE(display_name, name)` to `COALESCE(NULLIF(display_name, ''), name)`. (#1034)

### Migrations

- **PG:** `000057_heartbeat_provider_fk_set_null` — defensive orphan cleanup, drop existing FK by lookup, re-add with `ON DELETE SET NULL`. Brief `ACCESS EXCLUSIVE` lock on `agent_heartbeats` during ALTER (sub-second on small tables; heartbeat workers may pause briefly).
- **SQLite:** schema v25 → v26 — full table rebuild for `agent_heartbeats` with new FK clause; explicit 25-column INSERT/SELECT preserves existing rows. `idx_heartbeats_due` recreated.

### Upgrade notes

- **Docker users:** MUST pull the new image (`ghcr.io/nextlevelbuilder/goclaw:v3.11.3`) AND run `goclaw upgrade` (or `goclaw migrate up`). Stale images on v3.11.2 will fail boot with `schema version mismatch: required 57, current 56` after the migration runs.
- **Bare-metal users:** rebuild and run `./goclaw upgrade`.

### OpenAPI

- `/v1/providers/{id}/verify` — `requestBody.required: false`; `model` documented as optional with ping-mode semantics.

---

## 2026-04-24

### Tools: Config-driven shell deny-groups + read_audio routing fixes

**Features**

- **`shellDenyGroups` runtime config:** `config.tools.shellDenyGroups` (map[string]bool) allows operators to toggle shell deny-groups (e.g. `package_install`, `env_dump`) from the /config Web UI without restarting. Merged with per-agent overrides with per-key agent precedence; multi-tenant invariant preserved. Subscribed to `bus.TopicConfigChanged` for live reload.

**Fixes**

- **Credentialed CLI wording scope:** "operation requires admin approval" error wording now scoped to `[CREDENTIALED EXEC]` marker only — was over-applied to generic shell failures, causing unjustified LLM pre-refusals.
- **read_audio transcription routing:** Fixed silent fallback on missing API credentials for transcription/gemini/openai paths — now hard-errors with clear message. Fixed openai_compat providers (e.g. DashScope) not reaching `/v1/audio/transcriptions` endpoint; moved transcription model check above provider type switch.

**Tests**

- 6 unit tests for shell deny-groups merge/defensive-copy semantics.
- 3 pub/sub dispatch tests for config reload lifecycle.
- 3 regression tests for read_audio fail-fast paths.

---

## 2026-04-22

### Providers: Native image generation for Codex + OpenAI-compat

**Features**

- **Codex native track:** `CodexProvider` now attaches the `image_generation` tool object to `POST /codex/responses` when the agent permits it. Streams `response.image_generation_call.partial_image` intermediate frames + `response.output_item.done` (type `image_generation_call`) final images; non-stream path walks `response.output[]`. Deduped per `item_id`, partial frames emitted as `ImageContent{Partial:true}` for UI progressive render.
- **OpenAI-compat track:** `tools[]` serializer passes `{type:"image_generation"}` entries through natively; response parser reads `choices[0].message.images[]` / `choices[0].delta.images[]` (data URLs) into `ChatResponse.Images`.
- **Media persistence:** `internal/agent/media.go` `persistAssistantImages()` writes final images to `{workspace}/media/{sha256}.{ext}`, returns `MediaRef` entries, clears inline base64. Idempotent on hash. Wired via `pipeline.Deps.PersistAssistantImages` callback from `FinalizeStage`. Partial frames skipped.
- **Capabilities + gate:** `ProviderCapabilities.ImageGeneration` flag, set true on Codex provider. Tri-level gate in agent loop: provider capability AND `AgentConfig.AllowImageGeneration` (read from `other_config.allow_image_generation`, default true) AND request not opted-out via `x-goclaw-no-image-gen` header.
- **Web UI:** Composer "Images" toggle chip (visible only when provider supports image gen, per-agent persistence in localStorage). Streaming placeholder skeleton in `ActiveRunZone` while partials arrive. `MediaGallery` assigns `generated-{timestamp}.png` filename for assistant-generated PNGs.

**Wire format**

Implementation is evidence-backed against the native ChatGPT Responses API event shape, not the compat shim shape. Research notes in `plans/reports/`.

**i18n**

- 1 UI key (`imageGenDownloadName`) in `ui/web/src/i18n/locales/{en,vi,zh}/chat.json` — download filename for generated images.

**Tests**

- Unit tests across providers (Codex native + OpenAI-compat), agent media persistence, store config. Full test sweep: 2618 pass.

**Internal docs**

- `plans/260422-1349-goclaw-chatgpt-image-gen/` — plan + phase files.
- `plans/reports/researcher-260422-1414-codex-native-image-events.md` — native event schema.

## 2026-04-20

### Pipeline: accurate context token tracking + dynamic compaction

**Features**

- **Session token display from metadata:** `sessions.metadata` now carries `last_prompt_tokens` and `last_message_count` on finalize. List query reads from metadata; fallback to octet/rune heuristic when absent. Fixes stale token display across session re-opens.
- **Tool-schema token accounting:** `TokenCounter.CountToolSchemas(model, tools)` new method counts tool definitions serialized as JSON. Tool-schema tokens included in `OverheadTokens` at ContextStage.
- **Dynamic compaction max_tokens:** Compaction `max_tokens` now derived from `in/25` with clamp `[1024, 8192]`. Applied to both summarization flow (`loop_compact.go`) and history sanitization (`loop_history_sanitize.go`). Replaces static 4096 limit — adapts budget to context size.

**Code**

- `internal/store/pg/sessions_list.go` — read/write `last_prompt_tokens` and `last_message_count` in metadata.
- `internal/store/sqlitestore/sessions*.go` — parity SQLite store updates.
- `internal/tokencount/token_counter.go` — `CountToolSchemas` interface method + `tiktoken_counter.go` impl.
- `internal/pipeline/context_stage.go` — include tool overhead in `OverheadTokens`.
- `internal/agent/loop_compact.go` — `dynamicSummaryMax` function; apply to compaction call.
- `internal/agent/loop_history_sanitize.go` — apply dynamic max to sanitization.

**Tests**

- `internal/tokencount/count_tool_schemas_test.go` — tool schema token counting.
- `internal/agent/loop_compact_dynamic_max_test.go` — dynamic max_tokens clamping.
- `internal/pipeline/context_stage_tool_overhead_test.go` — tool overhead integration.
- `internal/store/sqlitestore/sessions_display_tokens_integration_test.go` — metadata round-trip.

---

### TTS: timeout tenant-config + Gemini text-only 400 fix

**Features & Fixes**

- **Tenant-config timeout:** HTTP `/v1/tts/synthesize` and `/v1/tts/test-connection` now read `tts.timeout_ms` from system_configs (default 120s, was hardcoded 15s/10s). Gemini client default bumped 30s→120s for end-to-end alignment.
- **Gemini text-only error recovery:** Gemini preview models occasionally emit 400 "text generation" responses. Fixed by: (1) prepending inline prefix `"Speak naturally: "` to single-voice synthesis (multi-speaker untouched), (2) 1-retry with stronger prefix `"Read the following text aloud without translating, commenting, or modifying: "`, (3) new sentinel `gemini.ErrTextOnlyResponse` preserved through fallback chain via `errors.Join`.
- **Error UX:** HTTP returns 422 with localized `MsgTtsGeminiTextOnly` message. Agent TTS tool branches on sentinel to emit locale-translated ForLLM response.
- **Model default:** Gemini default model bumped `gemini-2.5-flash-preview-tts` → `gemini-3.1-flash-tts-preview` for higher stability.
- **UI bounds:** TTS timeout input now has `max=300000` (5 min).

**i18n**

- New key `MsgTtsGeminiTextOnly` in EN/VI/ZH catalogs for HTTP 422 + agent-tool ForLLM mapping.

**Code**

- `internal/audio/tts.go` — read tenant timeout in synthesize handlers.
- `internal/audio/gemini/` — inline prefix logic, retry budget, text-only sentinel.
- `internal/tools/tts.go` — agent-tool i18n branching on sentinel.
- `internal/http/methods/tts.go` — HTTP 422 error mapping.

---

### Tools: `send_file` — explicit workspace file delivery

**Features**

- **`send_file` tool** (`internal/tools/send_file.go`): dedicated tool for sending existing workspace files as chat attachments. Takes `path` (required) and `caption` (optional). Replaces implicit `message(MEDIA:path)` convention for re-delivering already-created files. Marks `DeliveredMedia` on success to prevent duplicate delivery.
- **`DeliveredMedia` mark on `message(MEDIA:)` sends** (`internal/tools/message.go`): patched to call `IsDelivered` / mark after successful MEDIA upload — closes the cross-tool duplicate-delivery gap where a file sent via `message(MEDIA:)` was not tracked and could be re-sent by `send_file`.
- Registered as builtin tool in `cmd/gateway_tools_wiring.go` and seeded in `cmd/gateway_builtin_tools.go`.

---

## 2026-04-22

### Codex OAuth pool routing strategy cleanup

**Changes**

- Removed `primary_first` from the public Codex OAuth routing strategy surface. The API, OpenAPI schema, and web UI now expose only `round_robin` and `priority_order`.
- Legacy `primary_first` and `manual` routing values now normalize to `priority_order` on read in the backend store layer.
- Activity endpoints now default empty/no-pool responses to `priority_order` instead of `primary_first`.
- Agent overrides that explicitly persist `extra_provider_names: []` continue to behave as single-account-only routing after the migration.

**Docs**

- Updated `docs/02-providers.md` and `docs/18-http-api.md` to describe the two-strategy model and the compatibility migration.

## 2026-04-19

### TTS: Gemini provider + ProviderCapabilities schema engine

**Features**

- **Gemini TTS provider** (`internal/audio/gemini/`): supports `gemini-2.5-flash-preview-tts` and `gemini-2.5-pro-preview-tts`. 30 prebuilt voices, 70+ languages, multi-speaker mode (up to 2 simultaneous speakers with distinct voices), audio-tag styling, WAV output via PCM-to-WAV conversion.
- **`ProviderCapabilities` schema** (`internal/audio/capabilities.go`): dynamic per-provider param descriptor. Each provider exposes `Capabilities()` returning `[]ParamSchema` (type, range, default, dependsOn conditions, hidden flag) + `CustomFeatures` flags. UI reads `GET /v1/tts/capabilities` and renders param editors without hard-coded field lists.
- **Dual-read TTS storage**: tenant config read from both legacy flat keys (`tts.provider`, `tts.voice_id`, …) and new params blob (`tts.<provider>.params` JSON). Blob wins on conflict. Allows gradual migration; no data loss on downgrade.
- **`VoiceListProvider` interface** refactor: dynamic voice fetching (ElevenLabs, MiniMax) now via `ListVoices(ctx, ListVoicesOptions)` instead of per-provider ad-hoc methods. Unified `audio.Voice` type.
- **`POST /v1/tts/test-connection`**: ephemeral provider creation from request credentials + short synthesis smoke test. Returns `{ success, latency_ms }`. No provider registration; no config mutation. Operator role required.
- **`GET /v1/tts/capabilities`**: returns `ProviderCapabilities` JSON for all registered providers.

**i18n**

- Backend sentinel error keys (`MsgTtsGeminiInvalidVoice`, `MsgTtsGeminiInvalidModel`, `MsgTtsGeminiSpeakerLimit`, `MsgTtsParamOutOfRange`, `MsgTtsParamDependsOn`, `MsgTtsMiniMaxVoicesFailed`) in all 3 catalogs (EN/VI/ZH).
- HTTP 422 responses for Gemini sentinel errors now use `i18n.T(locale, key, args...)` — locale from `Accept-Language` header.
- ~80 param `label`/`help` keys across web + desktop locale files (EN/VI/ZH); parity enforced by `ui/web/src/__tests__/i18n-tts-key-parity.test.ts`.

**Security**

- SSRF guard on `api_base` override for test-connection (`validateProviderURL()`) — blocks `127.0.0.1` / `localhost` / RFC1918 ranges.

**Docs**

- `docs/tts-provider-capabilities.md` — schema reference + per-provider param tables + storage format + "Adding a new provider" checklist.
- `docs/codebase-summary.md` — TTS subsystem section documenting manager, providers, storage, endpoints.
