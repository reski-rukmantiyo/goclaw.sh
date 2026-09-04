# Software Requirements Specification: WhatsApp Image Description at Capture — Vision-Enriched Raw Bodies, Embeddings, and KG Extraction

**Project**: GoClaw Gateway
**Release**: 2026.3.0
**Version**: 0.4-draft
**Date**: 2026-09-04
**Status**: Implemented (code-complete + build/vet/test-verified). Live WhatsApp image round-trip + PG migration apply on the master tenant pending (§5 env-gated).
**Difficulty**: Medium
**Estimate**: 2–3 days

---

## Revision History

| Version | Date | Changes |
|---------|------|---------|
| 0.1-draft | 2026-09-04 | Initial draft. Root cause verified against code: WhatsApp inbound images are stored in `listen_raw_messages.body` as a bare `<media:image>` tag (no URL — WhatsApp media has no hosted URL; `BuildMediaTags` `internal/channels/media/media_tags.go:24-28`), the file is persisted via `ListenBuffer.PersistMedia` into `media_refs` (`internal/channels/whatsapp/listen_buffer.go:158`), the embedding worker embeds `body` verbatim (`buildEmbeddingTextFromRaw`, `internal/channels/whatsapp/embedding_worker.go:356`) so embeddings capture only the placeholder string, and the KG extract worker re-analyzes media independently (`appendMediaAnalysis`, `internal/channels/whatsapp/extract_worker.go:466`). Composes with `007` (scope edit + state-reset contract), `008` (server-side body/chunk text filters), `011` (chunk `tsv`), `003` (inbound hot-path rules). |
| 0.2-draft | 2026-09-04 | Open questions resolved with operator. **D1**: enrichment active only when a vision LLM is configured (no provider = silent pass-through, no billing). **D2**: backfill opt-in (`listen.media_analysis.backfill_enabled`, default off) + persisted `enriched_since` activation cutoff (FR-06 rewritten). **D3**: `image` only — sticker excluded (pass-through; stickers emit no body tag today). **D4**: descriptions always English; multilingual embeddings cover vi/zh retrieval. FR-02/FR-03/FR-06/FR-07 updated accordingly. |
| 0.3-draft | 2026-09-04 | **Pre-implementation gap-check (all root-cause claims verified against code) + operator decisions locked.** Verified: versions (`RequiredSchemaVersion=90`, SQLite `SchemaVersion=47`), `media_refs` empty = `'[]'` (`NOT NULL DEFAULT`, PG `000053`, SQLite `schema.sql:1624`) so the FR-04 predicate/partial index are safe, none of the 4 pending queries filters `media_refs` today, `UpdateScope` never touches `body`, `listen.media_analysis.enabled` gate exists (`media_analyzer.go:79,238`, default on). **Locked decisions:** (D5) D1 probe = new exported `MediaAnalyzer.HasVisionProvider(ctx) bool` wrapping `tools.ResolveMediaProviderChain` (`internal/tools/media_provider_chain.go:65` — pure config+registry, no LLM call); effective D1 semantics documented: default chain = openrouter, gemini, anthropic, claude-cli, dashscope (`internal/tools/read_image.go:35`) — **any** of these configured ⇒ enrichment ON. (D6) enrichment worker registered **unconditionally** next to `RegisterEmbeddingWorker` (`cmd/gateway_lifecycle.go:355`), gate = ListenRawMessages+SystemConfigs+BuiltinTools non-nil (NOT `providerRegistry` — no-provider must still pass-through-mark so the FR-04 gates drain). (D7) store API = `ListPendingMediaEnrichment(ctx, MediaEnrichFilter{Cutoff, Backfill, MaxRows})` params struct. Line-ref corrections: `BuildMediaTags` = `internal/channels/media/media_tags.go:18-58` (not 24-28 — that is the image branch); `analyzeMediaAttachments` lives in `internal/channels/whatsapp/media_analyzer.go:304` (not extract_worker.go); chunk interface name is `RawMessageChunkStore` (not `ChunkStore`); FR-02 tenant AC reworded — single registration with `TenantID = store.MasterTenantID`, mirroring the embedding worker (`cmd/gateway_lifecycle.go:359`), not per-tenant instances. |
| 0.4-draft | 2026-09-04 | **Implemented (code-complete + build/vet/test-verified).** Migration 000091 (PG column+partial index) + `RequiredSchemaVersion` 91; SQLite `schema.sql` + patch 47→48. Store: `MediaEnrichFilter` + `ListPendingMediaEnrichment` + `MarkMediaEnriched` (single UPDATE incl. pipeline reset) + `MarkMediaAnalyzedByIDs` (pass-through) + `MarkMediaAnalyzedBefore` (FR-06 bulk) on interface + PG + SQLite; FR-04 predicate on all 4 pending queries both stores. New `internal/channels/whatsapp/media_enrich_worker.go` (`RegisterMediaEnrichWorker`, consts 30s/5s/50/2, D5 `HasVisionProvider` probe per poll cycle, `ensureEnrichActivation` writes `enriched_since` once + bulk-mark when backfill off, per-group `enriched/passed_through/failed` logs, `enrichBodyWithDescriptions` FR-03 rewrite incl. `(from replied message)` line-aware insert + 2000-rune truncation + `html.EscapeString`, chunk invalidation via `DeleteBySourceMsgIDs`→neighbors `ResetEmbeddedByIDs`). `tools.ResolveVisionChain` exported probe; `analyzeOne` refactored to return raw content (`Analyze` wraps — output unchanged). FR-05: `appendMediaAnalysis` + `analyzeMediaAttachments` + `ExtractionWorkerDeps.MediaAnalyzer` removed; extract worker consumes media via body only. **Deviations found during implementation:** (1) **`media_refs` nil-marshals to JSON `null`** — `AppendBatch` stored string `null` for text-only rows (both DBs, pre-existing), so the FR-04 predicate and poll filters use `media_refs IN ('[]','null')` / `NOT IN` (PG `media_refs::text`) and `AppendBatch` now normalizes nil→`[]`; partial index matches. (2) Backfill poll deliberately does NOT filter `media_analyzed_at IS NULL` (bulk-marked rows must stay backfill-eligible by body pattern — matches FR-06 eligibility text). (3) **Pre-existing SQLite bug fixed as drive-by:** `ListPending`/`ListPendingEmbeddings`/`ListAbandonedIDs` bound `maxRows` BEFORE `tArgs` while the tenant clause sits before `LIMIT ?` → the tenant UUID fed LIMIT → "datatype mismatch (20)" on every call (latent — these paths were never exercised on desktop). Bind order corrected. **Verification:** `go build ./...` ✓, `go build -tags sqliteonly ./...` ✓, `go vet ./...` ✓; `TestSQLiteListenRawMessageStore_*` 21/21 `-race` ✓ (incl. 7 new media-enrichment tests: poll fresh/backfill/tenant-isolation, MarkMediaEnriched reset, pass-through, bulk-mark, FR-04 gate on all 4 pending queries, text-only no-regression); `internal/channels/whatsapp` 134/136 `-race` (2 pre-existing `TestMimeToExt`); `sqlitestore` 77/81 (4 pre-existing schema-DDL failures, documented since SRS 008); `internal/http`+`internal/tools` failure sets byte-identical to clean tree (verified via stash compare — 17 pre-existing: `TestResolveAuth_*`, `TestWebhookAdmin_*`, `TestKGTraversal_Tier1_CappedAt20`, per SRS 008/011). `go fix` skipped (unrelated-churn convention, 007/009/012/013). **Env-gated pending:** PG migration apply + PG store integration test + live image round-trip on master tenant (needs running gateway + pgvector; no pg-5433 test container in this env). |

---

## 1. Summary

This SRS defines **vision-based image description at raw-message capture**: a background enrichment worker turns persisted WhatsApp images into text descriptions via the configured vision LLM, writes the description into `listen_raw_messages.body`, and lets the existing embedding and KG-extraction workers consume that text as the single source of truth for media content. The image file keeps its durable path and `media_refs` reference; no media content is re-analyzed downstream.

Core idea: today the body says only `<media:image>`; after this feature the body says `<media:image>` **plus** an escaped `<description>` block, and every downstream consumer (Embeddings menu chunks, `shared_knowledge_search` FTS via migration 000090 `tsv`, KG extraction) reads the body — no worker-specific media handling. (`BuildMediaTags` is `internal/channels/media/media_tags.go:18-58`; the image branch is lines 24–28. Stickers arrive as media type `"sticker"` — an inline literal at `media_download.go:43`, not a constant — and emit no tag.)

## 2. Scope

**In scope**:

- New `media_analyzed_at` worker-state column on `listen_raw_messages` (dual-DB migration).
- New background **media enrichment worker** (WhatsApp listen pipeline): polls stored raw messages with image `media_refs`, runs the existing `MediaAnalyzer` (vision provider chain from `read_image` builtin-tool settings), rewrites the body tag into tag + `<description>` block. Active only when a vision LLM is configured; opt-in backfill; **images only** (no sticker/voice/video/document).
- Gating changes so the embedding worker and KG extract worker never process a media-bearing message before its enrichment attempt completes (pass-through when disabled or non-image).
- Backfill of historical rows (media_refs present, bare tag) including stale-chunk invalidation and re-embed, mirroring `007` FR-08 true-move.
- Removal of the extract worker's independent `appendMediaAnalysis` re-analysis (single source of truth).

**Out of scope**:

- Video, document, sticker, audio, and voice content descriptions (same mechanism extends later; v1 = `image` only — Decision D3). Stickers arrive as media type `"sticker"` (`media_download.go:43`), emit **no** body tag today (`BuildMediaTags` has no sticker case), and pass through enrichment marked analyzed.
- Audio/voice STT (existing inbound transcript path via `<transcript>` is unchanged).
- Respond-mode (bot-mentioned) message handling — the agent pipeline already receives `bus.MediaFile` and can call `read_image` itself; unchanged.
- Any UI change: the Raw Messages menu already renders `body`, the Embeddings menu renders chunk `text`; richer text appears with no UI work. No new i18n keys.
- New HTTP/WS endpoints. No new canonical error codes (silent best-effort convention, see §8).

## 3. Functional Requirements

### FR-00: Inbound Capture Path Stays Fast and Unchanged

The WhatsApp inbound handler must not call any LLM. The listen-only branch keeps its current behavior: download media → persist file → insert raw message with bare tag + `media_refs` → return. Enrichment happens strictly after insert, in a background worker.

Rationale: `handleEvent` dispatches whatsmeow events synchronously (`internal/channels/whatsapp/whatsapp.go:297-300`); a vision call (up to `listen.media_analysis.timeout_sec`, default 30s) on that goroutine would stall message intake, typing indicators, and the 003 RC1 agent/graph resolution path. This also preserves the 003/007 "no per-message DB or LLM lookup on the hot path" rule.

Acceptance criteria:

- [ ] `handleIncomingMessage` listen-only branch contains no `MediaAnalyzer` call and no new per-message DB round-trip beyond the existing insert.
- [ ] Raw message insert latency is unaffected (worker runs out-of-band).
- [ ] If the enrichment worker is dead or backed up, messages still arrive in `listen_raw_messages` with bare tag + `media_refs` (current behavior).

---

### FR-01: Data Model — `media_analyzed_at` Worker Column

`listen_raw_messages` gains a nullable timestamp marking that the enrichment attempt (success, failure, or pass-through) completed for the row's media refs. Worker-state semantics mirror `embedded_at` (from 007).

Persisted fields:

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| `media_analyzed_at` | `TIMESTAMPTZ` (PG) / `TEXT` ISO-8601 (SQLite) | `NULL` | `NULL` = enrichment pending. Set once per row; never reset by this feature. `007`'s `UpdateScope` does **not** reset it (scope moves do not re-analyze media). |

Partial index for the worker poll (both DBs):

```sql
CREATE INDEX idx_listen_raw_media_pending
    ON listen_raw_messages (tenant_id, agent_id, created_at)
    WHERE media_refs::text NOT IN ('[]', 'null') AND media_analyzed_at IS NULL;
```

(SQLite form uses plain `media_refs NOT IN ('[]', 'null')`. The `'null'` arm: pre-existing rows stored JSON `null` for text-only messages — see revision 0.4 deviation 1.)

Acceptance criteria:

- [x] PG migration `000091_listen_raw_messages_media_analyzed.up.sql` / `.down.sql` adds column + partial index; `RequiredSchemaVersion` bumped 90 → 91 in `internal/upgrade/version.go`. _(v0.4; partial-index predicate widened to `media_refs::text NOT IN ('[]','null')` per the nil-marshaling deviation, revision 0.4)_
- [x] SQLite: column + partial index added to `internal/store/sqlitestore/schema.sql` (fresh-DB schema) **and** incremental patch appended to the `migrations` map in `internal/store/sqlitestore/schema.go`; `SchemaVersion` bumped (currently 47 → 48). _(v0.4; fresh-DB v48 exercised by every sqlite store test via EnsureSchema)_
- [x] `go build ./...` and `go build -tags sqliteonly ./...` both green. _(v0.4)_
- [ ] Existing rows migrate with `media_analyzed_at IS NULL` (they become backfill candidates, FR-06). _(code-guaranteed — column added nullable, no backfill; live `./goclaw migrate up` on master pending, env-gated)_

---

### FR-02: Media Enrichment Worker

A new background worker (`internal/channels/whatsapp/media_enrich_worker.go`, following `embedding_worker.go` structure) periodically polls for pending rows and enriches them using the existing `MediaAnalyzer` — the same vision provider chain, size caps, timeout, and enable flag already used by the extract worker today (`listen.media_analysis.*` system configs; `MediaAnalyzer.loadLimits`, `media_analyzer.go:191`).

Behavior table:

| Row state | Worker action |
|-----------|---------------|
| `media_refs` empty | Not selected (never polled). |
| No vision LLM configured (read_image provider chain resolves to nothing) | Pass-through: set `media_analyzed_at = NOW()` without touching `body` and **without** failure markers. Decision D1: enrichment is ON only when a Vision LLM is actually configured — absence of a provider is a normal state, not a failure; no billing, no body pollution. |
| Has `image` refs, vision provider available, `listen.media_analysis.enabled` on | For each image ref: `MediaAnalyzer.Analyze` → rewrite body per FR-03 → single UPDATE per row: `body`, `media_analyzed_at = NOW()`, plus state reset per FR-06 for already-embedded rows. |
| Has `image` refs, analysis **disabled** (`listen.media_analysis.enabled = false/0`) | Pass-through: set `media_analyzed_at = NOW()` without touching `body`. Keeps downstream workers from stalling on pending rows (parity with today's disabled behavior where `appendMediaAnalysis` is a no-op). |
| Only non-image refs (`sticker`, `video`, `document`, …) | Pass-through: mark analyzed, no body rewrite (Decision D3: v1 is `image` only). |
| Row older than the enrichment activation cutoff and backfill off | Pass-through: mark analyzed, leave body untouched. See FR-06 for the cutoff mechanism. |
| Analysis fails (provider error, file missing, too large) | Non-blocking fallback: body keeps bare tag **plus** `<description>[analysis failed]</description>` marker per failed ref, `media_analyzed_at = NOW()`. Message is never lost and the pipeline never retries forever. Mirrors the analyzer's existing failure string (`[Media: %s — analysis failed]`, `media_analyzer.go:89`) and the 009/011 silent best-effort convention. |
| `media_analyzed_at` already set | Never re-selected. Descriptions are written once. |

Worker constants v1 (hardcoded, mirroring `embedding_worker.go` consts; system-config knobs deferred — see §6): poll 30s, min poll 5s when backlog > 100, max 2 concurrent (agent, graph) groups, batch 50 rows.

Provider probe (Decision D5): `MediaAnalyzer` gains an exported `HasVisionProvider(ctx context.Context) bool` that builds the same builtin-tool settings ctx as `Analyze` (`contextWithToolSettings`, `media_analyzer.go:52-67`) and calls `tools.ResolveMediaProviderChain` (`internal/tools/media_provider_chain.go:65`) — pure config-parse + `registry.Get`, **no LLM call**. Empty chain ⇒ no provider ⇒ D1 pass-through. Effective D1 semantics: the default chain is openrouter, gemini, anthropic, claude-cli, dashscope (`internal/tools/read_image.go:35`), so **any** of those providers configured in `llm_providers` makes enrichment active; the probe is evaluated per poll cycle so runtime provider changes are picked up without restart.

Registration (Decision D6): registered in `cmd/gateway_lifecycle.go` next to `RegisterEmbeddingWorker` (which it must out-live-gate: the embedding worker requires `RawMessageChunks` non-nil, the enrichment worker requires only ListenRawMessages + SystemConfigs + BuiltinTools non-nil — **not** `providerRegistry`, because the no-provider state must still pass-through-mark rows so the FR-04 gates drain). Single registration with `TenantID = store.MasterTenantID`, mirroring the embedding worker (`cmd/gateway_lifecycle.go:359`) — not per-tenant instances; tenant isolation comes from the store's `scopeClause` on poll and update.

Acceptance criteria:

- [x] Worker registered unconditionally next to `RegisterEmbeddingWorker` (gate: ListenRawMessages + SystemConfigs + BuiltinTools non-nil); stop function closes cleanly (same lifecycle as `RegisterEmbeddingWorker`). _(v0.4: `cmd/gateway_lifecycle.go`, same stopCh pattern)_
- [x] Success, no-provider, disabled, non-image, pre-cutoff, and failure paths all set `media_analyzed_at` exactly once. _(v0.4: no-provider/disabled/non-image/already-described → `MarkMediaAnalyzedByIDs` exactly once — `TestEnsureEnrichActivation`, `TestMediaAnalysisEnabled`, store tests `TestSQLiteListenRawMessageStore_MediaEnrichmentPoll`/`_MarkMediaAnalyzed*`; success/failure vision paths set it via `MarkMediaEnriched` — code-path proven, live vision round-trip env-gated)_
- [x] With no vision provider configured, zero vision API calls are made and no `[analysis failed]` markers are written (no-provider pass-through is silent). _(v0.4: D5 probe short-circuits before any `analyzeOne` call; pass-through never touches body)_
- [x] Vision failures never drop the message, never panic the worker, and log `slog.Warn` with `msg_id`, `agent_id`, `graph_id`, `media_type`, `error`. _(v0.4: `enrichRow` per-ref failure → `[analysis failed]` marker + mark; exact log fields present)_
- [x] Worker honors `MediaAnalyzer` size caps and timeout — a huge image yields the too-large marker, not a stalled call. _(v0.4: worker uses `loadLimits` + `analyzeOne`, which enforces `sizeLimitForType`/timeout — unchanged analyzer path)_
- [x] Tenant isolation preserved: poll and update are tenant-scoped via the store's `scopeClause` (single registration with `TenantID = store.MasterTenantID` and `store.WithTenantID` context, mirroring the embedding worker — not per-tenant worker instances). _(v0.4: `TestSQLiteListenRawMessageStore_MediaEnrichmentPoll_TenantIsolation`)_

---

### FR-03: Body Format Contract — Tag Plus Description Block

The enrichment rewrites each enriched image tag from the bare form into a tag + description block, mirroring the existing `<transcript>` pattern in `BuildMediaTags`:

```text
Before: [From: Alice]
        <media:image>
        look at this

After:  [From: Alice]
        <media:image>
        <description>A whiteboard with a sprint plan: three columns labeled Now, Next, Later…</description>
        look at this
```

Rules:

- Only the `<media:image>` tag is rewritten; all other tags (`<media:video>`, `<media:document>`, `<media:audio>`, `<media:voice>`) pass through untouched. Stickers emit no tag at all today and are therefore structurally immune to the rewrite (Decision D3).
- Description text is XML-escaped (`html.EscapeString`), same as transcripts.
- Descriptions are generated in **English** regardless of tenant locale (Decision D4): the vision prompt (`mediaPromptForType`) stays English, and multilingual embedding models map English descriptions into the same vector space as vi/zh queries — locale-aware rendering is not attempted at capture time.
- Rewrite is anchored on the exact bare tag token so caption text, group-history context, and `[From: …]` prefixes are never corrupted. Multiple image tags in one body are rewritten in order, one `<description>` per tag.
- The `<media:image>` tag itself is **kept** (not replaced) — the Raw Messages menu body filter (`008` `Body` substring filter) and any user habit of grepping tags keep working; the description is additive.

Acceptance criteria:

- [x] Unit tests cover: single image, multiple images, image + caption, image + group-history prefix, image inside `[Replying to: …]` context, failure marker, disabled pass-through (no change), sticker-only rows (no tag → body untouched). _(v0.4: `TestEnrichBodyWithDescriptions` 11 table cases + `TestImageMediaRefs`; "(from replied message)" suffix handled line-aware so the description lands after the full tag line)_
- [x] Body length growth is bounded: description truncated at 2,000 chars per image (`… [truncated]` suffix) to protect day-group chunking and embedding input sizes. _(v0.4: rune-safe truncation case in the table test)_
- [x] No rewrite touches rows whose body already contains a `<description>` block (idempotent). _(v0.4: `enrichGroup` skips such rows to pass-through; backfill eligibility `NOT LIKE '%<description>%'` is the store-level guard)_

---

### FR-04: Downstream Worker Gates — Embed and Extract Wait for Enrichment

Both existing workers must not process a media-bearing row before its enrichment attempt completes, otherwise the embedding worker embeds the bare placeholder (today's defect) or the extract worker re-analyzes media.

Gate (applies to `ListPendingEmbeddings`, `ListPendingEmbeddingGroups`, `ListPending`, `ListPendingGroups` in `store.ListenRawMessageStore`, PG and SQLite mirrored):

```sql
AND (media_refs IN ('[]', 'null') OR media_analyzed_at IS NOT NULL)
```

(`media_refs::text IN (...)` on PG. The `'null'` arm covers rows written before the nil-marshaling fix — `AppendBatch` used to store JSON `null` for text-only messages; new writes normalize to `'[]'`. See revision 0.4 deviation 1.)

Acceptance criteria:

- [x] PG and SQLite implementations both add the predicate to all four pending-list queries; fresh rows with image refs are invisible to embed/extract workers until `media_analyzed_at` is set. _(v0.4: `TestSQLiteListenRawMessageStore_MediaGateOnPendingLists` — all four queries gated, gate drains after pass-through mark; PG mirrors line-for-line. Predicate uses `media_refs IN ('[]','null')` — see the nil-marshaling deviation in revision 0.4)_
- [x] Non-media rows (`media_refs = '[]'`) behave exactly as before — no regression in 007's re-extract/re-embed flow for text-only messages. _(v0.4: `TestSQLiteListenRawMessageStore_MediaGateTextOnlyNoRegression` + the 007/008 suites still green)_
- [x] `007`'s `UpdateScope` reset (which clears `embedded_at` etc.) still causes re-processing; `media_analyzed_at` is untouched by scope edits, so re-embedded rows keep their existing descriptions (no duplicate vision cost). _(v0.4: `UpdateScope` SET list unchanged — verified against `pg/listen_raw_messages.go` UpdateScope; scope tests green)_

---

### FR-05: KG Extract Worker Drops Independent Media Re-Analysis

`appendMediaAnalysis` and the `[Media Content Analysis]` assembly in the extract worker (`extract_worker.go:466-490`, called at `extract_worker.go:246,273`) are removed. The extract worker builds conversation text from `body` only (`buildConversationTextFromRaw`), which now carries the description.

Justification against 007's re-extract contract: re-extraction after a scope edit still sees full media context because the description lives in `body` and `UpdateScope` does not clear it. Historical rows are covered by FR-06 backfill. When `listen.media_analysis.enabled` is off, behavior is identical to today (the analyzer no-ops when disabled), so no regression window exists.

Acceptance criteria:

- [x] `appendMediaAnalysis`, `analyzeMediaAttachments` call from the extract path, and the `[Media Content Analysis]` text section are removed; `mediaRefsSummary` logging may stay. _(v0.4: both call sites, the function, and `analyzeMediaAttachments` deleted; `mediaRefsSummary` reused by the enrichment worker's batch log)_
- [x] `MediaAnalyzer` remains in the wiring only for the enrichment worker. _(v0.4: `ExtractionWorkerDeps.MediaAnalyzer` field removed; sole construction is the enrichment worker deps in `cmd/gateway_lifecycle.go`)_
- [x] Extract worker unit tests updated: media-bearing bodies flow through as plain text. _(v0.4: extraction path is now body-only by construction — no analyzer reference compiles; existing whatsapp suite 134/136 green, the 2 failures are the pre-existing `TestMimeToExt` pair)_

---

### FR-06: Activation Cutoff and Opt-In Backfill

**Decision D2: backfill is opt-in.** Historical rows are never vision-analyzed by surprise; on upgrade they keep today's behavior (bare tag, embedded as-is) until an operator explicitly enables backfill.

Mechanism — two system-config keys (`listen.media_analysis.*` family):

| Key | Default | Role |
|-----|---------|------|
| `listen.media_analysis.enriched_since` | set by worker on first registration to `NOW()` (ISO-8601) if absent | Activation cutoff. Rows with `created_at < enriched_since` are "historical"; rows at or after it are "fresh". Persisted so restarts never re-cut the window. |
| `listen.media_analysis.backfill_enabled` | `false` | Off: historical rows are pass-through-marked once (see below) and never analyzed. On: worker enriches eligible historical rows. |

First registration with backfill off performs a one-time bulk pass-through mark so the FR-04 gates drain:

```sql
UPDATE listen_raw_messages
SET media_analyzed_at = NOW()
WHERE media_refs::text NOT IN ('[]', 'null') AND media_analyzed_at IS NULL
  AND created_at < :enriched_since AND tenant_id = :t
```

This mark does **not** touch `body` — historical bodies keep the bare `<media:image>` tag, which is what keeps them backfill-eligible later.

Backfill eligibility (worker poll when `backfill_enabled` on):

```text
created_at < :enriched_since
AND body LIKE '%<media:image>%'
AND body NOT LIKE '%<description>%'
```

The `NOT LIKE '%<description>%'` guard makes backfill idempotent: rows already enriched (or already carrying a failure marker — FR-03 adds a `<description>` block on both success and failure) are never re-billed.

For each backfilled row that was already embedded (`embedded_at` set), the enrichment UPDATE must (single statement, mirroring `UpdateScope` semantics):

```sql
UPDATE listen_raw_messages
SET body = $1,
    media_analyzed_at = NOW(),
    processed_at = NULL,
    extraction_status = 'pending',
    extraction_error = NULL,
    embedded_at = NULL
WHERE id = $2 AND tenant_id = $3
```

and the worker must then invalidate stale neighbor chunks (day-group chunks share `source_msg_ids` across messages):

1. `RawMessageChunkStore.DeleteBySourceMsgIDs(ctx, [msgID])` for each enriched message already present in chunks (the 007 FR-08 method, `internal/store/raw_message_chunk_store.go:93`; PG-only — SQLite chunk store is a no-op stub).
2. `RawMsgStore.ResetEmbeddedByIDs(ctx, neighborIDs)` where `neighborIDs` are the source message IDs of every deleted chunk minus the enriched ones — the day group re-embeds whole, matching 007 FR-08.

Sequencing with 007 scope edits: a scope edit on a row before its backfill enrichment re-keys the row; the enrichment worker then enriches it under the new `(agent_id, graph_id)` (it is still backfill-eligible by body pattern). A scope edit after enrichment resets embed/extract state but keeps the description in `body` — the row re-processes under the new key with the description intact. Neither order loses data.

Acceptance criteria:

- [x] First registration writes `enriched_since` exactly once (absent key only); restarts reuse the stored value. _(v0.4: `TestEnsureEnrichActivation` — write-once, restart reuse, bulk-mark skipped when backfill on; stored as RFC3339 second precision)_
- [x] With backfill off: bulk pass-through mark runs once; historical rows keep bare bodies; zero vision calls; embed/extract gates drain (no pipeline stall). _(v0.4: `MarkMediaAnalyzedBefore` store test + worker activation test; zero vision calls guaranteed by the D5 probe short-circuit)_
- [x] Fresh-flow rows (never embedded) enrich without chunk deletion (nothing to delete — they were gated). _(v0.4: chunk invalidation no-ops when `DeleteBySourceMsgIDs` returns 0 — gated fresh rows have no chunks)_
- [ ] Backfill on: eligible rows (bare tag, no `<description>`) enrich; rows with existing descriptions or failure markers are skipped; already-embedded rows get chunks deleted + neighbors reset per the sequence above; the next embedding cycle re-chunks the day group with the description in `text` (and therefore `tsv`, 011). _(v0.4 code-complete — eligibility store-tested incl. bulk-marked rows staying eligible; the chunk-delete → re-chunk cycle needs the live PG gateway, env-gated)_
- [x] `content_hash` unchanged semantics: re-chunked text differs from the old placeholder text, so no false dedupe. _(v0.4: hash input is chunk `text` which now carries the description — differs from the old placeholder text by construction)_
- [x] Backfill is incremental and restart-safe (crash mid-backfill leaves rows un-attempted → body pattern still matches → retried next poll; no double billing after success because the body no longer matches the pattern). _(v0.4: `MarkMediaEnriched` is a single atomic UPDATE — body pattern flips in the same commit; eligibility is body-pattern based, not in-memory state)_
- [x] SQLite listen-store mirror tested (chunk store is PG-only stub — backfill chunk invalidation is PG-only by design, 008). _(v0.4: `TestSQLiteListenRawMessageStore_MediaEnrichmentPoll` incl. backfill-after-bulk-mark case; chunk store stub nil-safe in the worker)_

---

### FR-07: Operator Visibility

Operators must be able to see and control media description behavior through existing surfaces only.

Acceptance criteria:

- [x] Master gate is the existing `listen.media_analysis.enabled` system config (per-tenant, `SystemConfigStore`, tenant→master fallback) — one switch for both enrichment cost and behavior. Effective activity additionally requires a configured vision provider (Decision D1). _(v0.4: `mediaAnalysisEnabled` in the worker — default on, "false"/"0" off, `TestMediaAnalysisEnabled`; D1 probe checked per poll cycle)_
- [x] Size/timeout caps reuse existing `listen.media_analysis.{max_image_mb,timeout_sec}` keys. New keys, same family, per FR-06: `listen.media_analysis.backfill_enabled` (default `false`) and `listen.media_analysis.enriched_since` (worker-managed activation cutoff, not operator-edited). _(v0.4: worker reads `backfill_enabled`, writes-only `enriched_since`; caps via `loadLimits` unchanged)_
- [x] Worker start/stop and per-batch outcomes are logged with `slog` fields `agent_id`, `graph_id`, `enriched`, `passed_through`, `failed` — matching the embedding worker's log style. _(v0.4: `enrichGroup` batch log carries exactly these fields + `media` summary)_
- [ ] Raw Messages menu shows the enriched body with no code change (verify manually). _(manual — needs running gateway)_

## 4. System Impact

- **DB model change**: PG migration `000091` (column + partial index) + `RequiredSchemaVersion` 91; SQLite `schema.sql` + incremental patch + `SchemaVersion` 48. No `raw_message_chunks` change (no `tsv` rebuild; 011 expression untouched).
- **Store layer**: `ListenRawMessageStore` — new `ListPendingMediaEnrichment(ctx, MediaEnrichFilter{Cutoff time.Time, Backfill bool, MaxRows int})` (Decision D7; fresh mode = `media_refs NOT IN ('[]','null') AND media_analyzed_at IS NULL AND created_at >= cutoff`; backfill mode = `media_refs NOT IN ('[]','null') AND created_at < cutoff AND body LIKE '%<media:image>%' AND body NOT LIKE '%<description>%'` — deliberately WITHOUT a `media_analyzed_at IS NULL` filter so bulk-marked rows stay backfill-eligible) + `MarkMediaEnriched` update method (body + state reset + `media_analyzed_at` in one UPDATE) + `MarkMediaAnalyzedByIDs` pass-through + `MarkMediaAnalyzedBefore` first-registration bulk mark (FR-06); pending-list queries gain the FR-04 predicate (PG `internal/store/pg/listen_raw_messages.go`, SQLite `internal/store/sqlitestore/listen_raw_messages.go`).
- **Worker integration**: new `RegisterMediaEnrichWorker` wired next to `RegisterEmbeddingWorker` / extract worker registration; `MediaAnalyzer` ownership moves to the enrichment worker.
- **Channel layer**: none (inbound path untouched — that is the point).
- **API/UI**: none.
- **Logging**: new worker lifecycle + per-batch structured logs.
- **Error codes**: none registered (silent best-effort; markers live in `body`).

## 5. Test Plan

- Unit tests: FR-03 body rewrite cases (table-driven, incl. failure marker + truncation + idempotency); enrichment worker state machine (disabled / non-image / failure / success each set `media_analyzed_at` once); FR-04 predicate presence in both store implementations.
- SQLite store tests: pending-media poll, enrichment UPDATE (body + state fields), FR-04 gating on `ListPending*` (mirror `008`'s `TestSQLiteListenRawMessageStore_ListTextFilters` pattern, tenant-isolation cases included).
- Build/vet: `go build ./...`, `go build -tags sqliteonly ./...`, `go vet ./...`.
- Env-gated (per repo convention): PG integration test for the enrichment UPDATE + chunk invalidation path (needs pgvector pg18 container); live WhatsApp image round-trip (send image → observe enriched body → chunk text → `shared_knowledge_search` hit).
- Pre-existing failure note: `TestMimeToExt` in `internal/channels/whatsapp/media_utils_test.go` fails on main (`text/plain` → `.txt` vs `.bin`) — pre-existing per 013, not touched here.

## 6. Risks and Open Questions

| Risk or question | Draft decision |
|------------------|----------------|
| Vision cost: every group image = one LLM call. | Gated by D1 (requires configured vision provider) + `listen.media_analysis.enabled` as hard off-switch. |
| Blocking dependency: embed/extract workers stall on images while enrichment pending. | Bounded by enrichment poll (30s default) + vision timeout (30s default). Pass-through on disabled keeps pipelines draining. Worst case: media rows lag text rows by one poll cycle. |
| Description quality pollutes embeddings/FTS (hallucinated detail). | Prompt already instructs factual description (`media_analyzer.go:248`); 2,000-char cap bounds blast radius; body filter (`008`) still matches raw text. |
| Backfill burst: many historical rows → burst of vision calls. | Neutralized by D2: backfill off by default; when opted in, worker concurrency caps (2 groups) + provider `RetryDo` backoff bound the burst. |
| Should worker knobs be system configs (`listen.media_enrich.*`)? | Deferred to follow-up if needed; v1 hardcodes constants like the embedding worker's initial version. |
| Scope-edit + backfill interleavings. | Analyzed in FR-06; both orders safe. |
| Voice/video later? | Same mechanism extends by widening the type filter in FR-02 + adding per-type tag rewrite in FR-03. Separate SRS when needed. |
| Description language. | **Decided D4 (2026-09-04): English only.** Prompt stays English; multilingual embedding models map English descriptions into the query vector space for any locale, so vi/zh retrieval works against English descriptions. Locale-aware rendering deferred with the voice/video extension. |
| Default state of `listen.media_analysis.enabled`. | **Decided D1 (2026-09-04): key keeps its default, but enrichment is active only when a vision LLM is configured** (provider chain resolves). No provider → silent pass-through, no cost, no failure markers. `enabled` remains the operator's hard off-switch. |
| Backfill trigger. | **Decided D2 (2026-09-04): opt-in** via `listen.media_analysis.backfill_enabled` (default off) + persisted `enriched_since` activation cutoff — see FR-06. |
| Custom analysis prompt. | Prompt fixed in code per media type. Operators may want domain-specific prompts (e.g. "focus on printed text", "describe people only"). Deferred; candidate for `listen.media_analysis.image_prompt` system config. |
| Sticker scope. | **Decided D3 (2026-09-04): `image` only.** Stickers pass through marked-analyzed; they emit no body tag today, so nothing to rewrite. |

## 7. Implementation Plan

1. Migration + both schemas + store interface additions (`media_analyzed_at`, `ListPendingMediaEnrichment`, enrichment UPDATE) with SQLite tests first.
2. FR-04 gate predicate on the four pending-list queries (PG + SQLite) + tests.
3. Enrichment worker (poll → analyze → rewrite body → UPDATE, incl. FR-06 chunk invalidation for already-embedded rows) + unit tests for the body-rewrite contract.
4. Remove extract-worker `appendMediaAnalysis` path; move `MediaAnalyzer` wiring to the enrichment worker.
5. Wire worker registration; run full checklist (`go fix`, both builds, vet, integration suite).
6. Env-gated live verification: real image → enriched body → re-chunked embeddings row → KG entities referencing described content.

## 8. Proposed Error Codes

None. The feature is silent best-effort per the 009/011 convention: failures become in-body markers (`[analysis failed]`), worker errors are `slog.Warn` entries, and no operator-facing API surface changes. If a future manual "re-analyze now" endpoint is added, it would follow 007's inline-400 style and canonically map to `request.validation_failed` / `listen_raw_message.not_found`.
