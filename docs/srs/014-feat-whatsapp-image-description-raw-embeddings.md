# Software Requirements Specification: WhatsApp Image Description at Capture — Vision-Enriched Raw Bodies, Embeddings, and KG Extraction

**Project**: GoClaw Gateway
**Release**: 2026.3.0
**Version**: 0.2-draft
**Date**: 2026-09-04
**Status**: Draft
**Difficulty**: Medium
**Estimate**: 2–3 days

---

## Revision History

| Version | Date | Changes |
|---------|------|---------|
| 0.1-draft | 2026-09-04 | Initial draft. Root cause verified against code: WhatsApp inbound images are stored in `listen_raw_messages.body` as a bare `<media:image>` tag (no URL — WhatsApp media has no hosted URL; `BuildMediaTags` `internal/channels/media/media_tags.go:24-28`), the file is persisted via `ListenBuffer.PersistMedia` into `media_refs` (`internal/channels/whatsapp/listen_buffer.go:158`), the embedding worker embeds `body` verbatim (`buildEmbeddingTextFromRaw`, `internal/channels/whatsapp/embedding_worker.go:356`) so embeddings capture only the placeholder string, and the KG extract worker re-analyzes media independently (`appendMediaAnalysis`, `internal/channels/whatsapp/extract_worker.go:466`). Composes with `007` (scope edit + state-reset contract), `008` (server-side body/chunk text filters), `011` (chunk `tsv`), `003` (inbound hot-path rules). |
| 0.2-draft | 2026-09-04 | Open questions resolved with operator. **D1**: enrichment active only when a vision LLM is configured (no provider = silent pass-through, no billing). **D2**: backfill opt-in (`listen.media_analysis.backfill_enabled`, default off) + persisted `enriched_since` activation cutoff (FR-06 rewritten). **D3**: `image` only — sticker excluded (pass-through; stickers emit no body tag today). **D4**: descriptions always English; multilingual embeddings cover vi/zh retrieval. FR-02/FR-03/FR-06/FR-07 updated accordingly. |

---

## 1. Summary

This SRS defines **vision-based image description at raw-message capture**: a background enrichment worker turns persisted WhatsApp images into text descriptions via the configured vision LLM, writes the description into `listen_raw_messages.body`, and lets the existing embedding and KG-extraction workers consume that text as the single source of truth for media content. The image file keeps its durable path and `media_refs` reference; no media content is re-analyzed downstream.

Core idea: today the body says only `<media:image>`; after this feature the body says `<media:image>` **plus** an escaped `<description>` block, and every downstream consumer (Embeddings menu chunks, `shared_knowledge_search` FTS via migration 000090 `tsv`, KG extraction) reads the body — no worker-specific media handling.

## 2. Scope

**In scope**:

- New `media_analyzed_at` worker-state column on `listen_raw_messages` (dual-DB migration).
- New background **media enrichment worker** (WhatsApp listen pipeline): polls stored raw messages with image `media_refs`, runs the existing `MediaAnalyzer` (vision provider chain from `read_image` builtin-tool settings), rewrites the body tag into tag + `<description>` block. Active only when a vision LLM is configured; opt-in backfill; **images only** (no sticker/voice/video/document).
- Gating changes so the embedding worker and KG extract worker never process a media-bearing message before its enrichment attempt completes (pass-through when disabled or non-image).
- Backfill of historical rows (media_refs present, bare tag) including stale-chunk invalidation and re-embed, mirroring `007` FR-08 true-move.
- Removal of the extract worker's independent `appendMediaAnalysis` re-analysis (single source of truth).

**Out of scope**:

- Video, document, sticker, audio, and voice content descriptions (same mechanism extends later; v1 = `image` only — Decision D3). Stickers arrive as media type `"sticker"` (`media_download.go:42`), emit **no** body tag today (`BuildMediaTags` has no sticker case), and pass through enrichment marked analyzed.
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
    WHERE media_refs <> '[]' AND media_analyzed_at IS NULL;
```

Acceptance criteria:

- [ ] PG migration `000091_listen_raw_messages_media_analyzed.up.sql` / `.down.sql` adds column + partial index; `RequiredSchemaVersion` bumped 90 → 91 in `internal/upgrade/version.go`.
- [ ] SQLite: column + partial index added to `internal/store/sqlitestore/schema.sql` (fresh-DB schema) **and** incremental patch appended to the `migrations` map in `internal/store/sqlitestore/schema.go`; `SchemaVersion` bumped (currently 47 → 48).
- [ ] `go build ./...` and `go build -tags sqliteonly ./...` both green.
- [ ] Existing rows migrate with `media_analyzed_at IS NULL` (they become backfill candidates, FR-06).

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

Acceptance criteria:

- [ ] Worker registered alongside the existing listen workers; stop function closes cleanly (same lifecycle as `RegisterEmbeddingWorker`).
- [ ] Success, no-provider, disabled, non-image, pre-cutoff, and failure paths all set `media_analyzed_at` exactly once.
- [ ] With no vision provider configured, zero vision API calls are made and no `[analysis failed]` markers are written (no-provider pass-through is silent).
- [ ] Vision failures never drop the message, never panic the worker, and log `slog.Warn` with `msg_id`, `agent_id`, `graph_id`, `media_type`, `error`.
- [ ] Worker honors `MediaAnalyzer` size caps and timeout — a huge image yields the too-large marker, not a stalled call.
- [ ] Tenant isolation preserved: poll and update are tenant-scoped (worker runs per tenant, same as embedding worker's `store.WithTenantID` context).

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

- [ ] Unit tests cover: single image, multiple images, image + caption, image + group-history prefix, image inside `[Replying to: …]` context, failure marker, disabled pass-through (no change), sticker-only rows (no tag → body untouched).
- [ ] Body length growth is bounded: description truncated at 2,000 chars per image (`… [truncated]` suffix) to protect day-group chunking and embedding input sizes.
- [ ] No rewrite touches rows whose body already contains a `<description>` block (idempotent).

---

### FR-04: Downstream Worker Gates — Embed and Extract Wait for Enrichment

Both existing workers must not process a media-bearing row before its enrichment attempt completes, otherwise the embedding worker embeds the bare placeholder (today's defect) or the extract worker re-analyzes media.

Gate (applies to `ListPendingEmbeddings`, `ListPendingEmbeddingGroups`, `ListPending`, `ListPendingGroups` in `store.ListenRawMessageStore`, PG and SQLite mirrored):

```sql
AND (media_refs = '[]' OR media_analyzed_at IS NOT NULL)
```

Acceptance criteria:

- [ ] PG and SQLite implementations both add the predicate to all four pending-list queries; fresh rows with image refs are invisible to embed/extract workers until `media_analyzed_at` is set.
- [ ] Non-media rows (`media_refs = '[]'`) behave exactly as before — no regression in 007's re-extract/re-embed flow for text-only messages.
- [ ] `007`'s `UpdateScope` reset (which clears `embedded_at` etc.) still causes re-processing; `media_analyzed_at` is untouched by scope edits, so re-embedded rows keep their existing descriptions (no duplicate vision cost).

---

### FR-05: KG Extract Worker Drops Independent Media Re-Analysis

`appendMediaAnalysis` and the `[Media Content Analysis]` assembly in the extract worker (`extract_worker.go:466-490`, called at `extract_worker.go:246,273`) are removed. The extract worker builds conversation text from `body` only (`buildConversationTextFromRaw`), which now carries the description.

Justification against 007's re-extract contract: re-extraction after a scope edit still sees full media context because the description lives in `body` and `UpdateScope` does not clear it. Historical rows are covered by FR-06 backfill. When `listen.media_analysis.enabled` is off, behavior is identical to today (the analyzer no-ops when disabled), so no regression window exists.

Acceptance criteria:

- [ ] `appendMediaAnalysis`, `analyzeMediaAttachments` call from the extract path, and the `[Media Content Analysis]` text section are removed; `mediaRefsSummary` logging may stay.
- [ ] `MediaAnalyzer` remains in the wiring only for the enrichment worker.
- [ ] Extract worker unit tests updated: media-bearing bodies flow through as plain text.

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
WHERE media_refs <> '[]' AND media_analyzed_at IS NULL
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

1. `ChunkStore.DeleteBySourceMsgIDs(ctx, [msgID])` for each enriched message already present in chunks.
2. `RawMsgStore.ResetEmbeddedByIDs(ctx, neighborIDs)` where `neighborIDs` are the source message IDs of every deleted chunk minus the enriched ones — the day group re-embeds whole, matching 007 FR-08.

Sequencing with 007 scope edits: a scope edit on a row before its backfill enrichment re-keys the row; the enrichment worker then enriches it under the new `(agent_id, graph_id)` (it is still backfill-eligible by body pattern). A scope edit after enrichment resets embed/extract state but keeps the description in `body` — the row re-processes under the new key with the description intact. Neither order loses data.

Acceptance criteria:

- [ ] First registration writes `enriched_since` exactly once (absent key only); restarts reuse the stored value.
- [ ] With backfill off: bulk pass-through mark runs once; historical rows keep bare bodies; zero vision calls; embed/extract gates drain (no pipeline stall).
- [ ] Fresh-flow rows (never embedded) enrich without chunk deletion (nothing to delete — they were gated).
- [ ] Backfill on: eligible rows (bare tag, no `<description>`) enrich; rows with existing descriptions or failure markers are skipped; already-embedded rows get chunks deleted + neighbors reset per the sequence above; the next embedding cycle re-chunks the day group with the description in `text` (and therefore `tsv`, 011).
- [ ] `content_hash` unchanged semantics: re-chunked text differs from the old placeholder text, so no false dedupe.
- [ ] Backfill is incremental and restart-safe (crash mid-backfill leaves rows un-attempted → body pattern still matches → retried next poll; no double billing after success because the body no longer matches the pattern).
- [ ] SQLite listen-store mirror tested (chunk store is PG-only stub — backfill chunk invalidation is PG-only by design, 008).

---

### FR-07: Operator Visibility

Operators must be able to see and control media description behavior through existing surfaces only.

Acceptance criteria:

- [ ] Master gate is the existing `listen.media_analysis.enabled` system config (per-tenant, `SystemConfigStore`, tenant→master fallback) — one switch for both enrichment cost and behavior. Effective activity additionally requires a configured vision provider (Decision D1).
- [ ] Size/timeout caps reuse existing `listen.media_analysis.{max_image_mb,timeout_sec}` keys. New keys, same family, per FR-06: `listen.media_analysis.backfill_enabled` (default `false`) and `listen.media_analysis.enriched_since` (worker-managed activation cutoff, not operator-edited).
- [ ] Worker start/stop and per-batch outcomes are logged with `slog` fields `agent_id`, `graph_id`, `enriched`, `passed_through`, `failed` — matching the embedding worker's log style.
- [ ] Raw Messages menu shows the enriched body with no code change (verify manually).

## 4. System Impact

- **DB model change**: PG migration `000091` (column + partial index) + `RequiredSchemaVersion` 91; SQLite `schema.sql` + incremental patch + `SchemaVersion` 48. No `raw_message_chunks` change (no `tsv` rebuild; 011 expression untouched).
- **Store layer**: `ListenRawMessageStore` — new `ListPendingMediaEnrichment(ctx, maxRows)` + `MarkMediaAnalyzed`-style update method (body + state reset + `media_analyzed_at` in one UPDATE); pending-list queries gain the FR-04 predicate (PG `internal/store/pg/listen_raw_messages.go`, SQLite `internal/store/sqlitestore/listen_raw_messages.go`).
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
