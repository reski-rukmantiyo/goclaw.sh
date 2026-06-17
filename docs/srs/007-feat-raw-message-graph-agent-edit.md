# Software Requirements Specification: Editable `agent_id` + `graph_id` on Listen Raw Messages (Reprocess After Fix)

**Project**: GoClaw Gateway
**Release**: 2026.3.0
**Version**: 0.2-draft
**Date**: 2026-06-17
**Status**: Draft
**Difficulty**: Low–Medium
**Estimate**: 0.5–1 days

---

## Revision History

| Version | Date | Changes |
|---------|------|---------|
| 0.1-draft | 2026-06-17 | Initial draft — `graph_id`-only edit. Verified: `ListenRawMessage.GraphID` (`internal/store/listen_raw_message_store.go:16`) write-once at INSERT; no UPDATE path. Worker makes `graph_id` the KG scope (`extract_worker.go:366,370,387`). Reset-to-pending (`ResetProcessedByIDs`, `pg/listen_raw_messages.go:237`) re-extracts. Decision: one mutation = set `graph_id` + reset extraction fields. |
| 0.2-draft | 2026-06-17 | **Scope expanded to `agent_id` + `graph_id` (the KG scope pair).** Operator confirmed the wrong-graph rows also carry the wrong `agent_id` — the `003-bugfix` RC1+RC2 **double-defect** (stored under Jarvis + `project-sovereign` instead of Felix + `project-dell-ioh`). The extraction worker groups and ingests by the `(agent_id, graph_id)` pair (`ListPendingGroups` → `IngestExtraction(ctx, agentID, graphID, …)`), so editing only `graph_id` would re-extract under the still-wrong agent. Feature now edits **both** fields; store method generalized `UpdateScope(ctx, ids, agentID, graphID)`; endpoint `POST /v1/listen-raw-messages/scope`. Title, FRs, decision log, tests updated. `agent_id` is a UUID (the agents-list `id`); UI uses an agent `<Select>`, server validates UUID format (no FK in schema `000050`, so existence is enforced client-side via the agents list). |
| 0.3-draft | 2026-06-17 | **Implemented (code-complete + build-verified).** Store `UpdateScope` (PG + SQLite, dynamic SET + `scopeClause` tenant scope) added. HTTP `POST /v1/listen-raw-messages/scope` + `handleUpdateScope` (decode/trim/validate/UUID-parse) added, registered with `requireAuth("")`. Frontend: `updateScope` hook; detail-dialog inline Agent `<Select>` + Graph input (FR-04); selection-toolbar batch "Update Scope" dialog (FR-05); agents threaded into the dialog; i18n keys in en/vi/zh (FR-06). Tests: `internal/store/sqlitestore/listen_raw_messages_scope_test.go` — `UpdateScope` correctness (both/graph-only/agent-only/empty-ids/extraction-reset) + tenant isolation, both `-race` green. **Verification:** `go build ./...` ✓, `go build -tags sqliteonly ./...` ✓, `go vet ./...` ✓, `pnpm build` (ui/web) ✓, sqlite store tests ✓. **Deferred:** PG store integration test (needs pgvector pg18 test container), handler unit test (handler is thin validation mirroring `handleResetByIDs`, which has no unit test either), and live §10 manual reprocess verification on the master tenant. Note: `go fix ./...` was run per the post-impl checklist but produced unrelated modernization churn (`strings.SplitSeq`, `slices.Contains`) in 5 untouched files — reverted to keep this diff scoped to the feature. Two pre-existing `TestWebhookAdmin_*` failures in `webhooks_admin_test.go` (`permission denied: tenant config`) are unrelated to this change (raw-message path untouched). |
| 0.4-draft | 2026-06-17 | **Closed the remaining code/testable gaps.** Added handler validation test `internal/http/listen_raw_messages_scope_test.go` (`TestHandleUpdateScope`, 10 table cases: both/agent-only/graph-only success, trim, empty `ids`, invalid UUID `id`, neither field, invalid `agent_id`, over-long `graph_id`, invalid JSON, store-error 500) using a mock store — green. Flipped all now-verified acceptance boxes to `[x]` with evidence pointers: FR-00, FR-01, FR-02, FR-03, FR-06, FR-07 fully verified (store + handler tests / inspection); FR-04/FR-05 interaction items left `[ ]` marked *manual* (need a browser — same convention as `004`/`005`/`006`). **Still open (cannot auto-close):** (1) live §10 manual reprocess verify on the master tenant (needs a running gateway + browser — not available in this env); (2) PG store integration test (deferred — SQLite test proves the identical SQL logic; PG impl is a line-for-line mirror; a PG test would need a dedicated test DB to avoid polluting the running dev `goclaw-pg` container); (3) §8 open decisions (role floor Member→admin? reset `extraction_attempts`? deprecate non-scoped endpoints?) — these need a product decision, not code. |
| 0.5-draft | 2026-06-17 | **Embedding re-queue added (operator-reported gap).** After editing agent/graph the message must also re-embed under the new scope and appear in the embeddings menu (`/t/master/embeddings`). Root cause: the embeddings menu reads `raw_message_chunks` (**not** `listen_raw_messages`), and the embedding worker gates on `embedded_at IS NULL` (`embedding_worker.go:182,271`). The prior `UpdateScope` reset only extraction state and left `embedded_at` set, so the worker never re-picked the message → it stayed stuck under the old scope. **Fix:** `UpdateScope` now also sets `embedded_at = NULL` (PG + SQLite + interface doc comment), so the embedding worker re-chunks the body and writes fresh chunks to `raw_message_chunks` under the new `(agent_id, graph_id)` (`embedding_worker.go:248-261`); the message then appears in the embeddings menu under the new scope. Store test extended to assert the `embedded_at` reset. **No embeddings-page UI change needed** — the menu already lists/filters by `agent_id`/`graph_id` (`internal/http/embeddings.go:35-93`). New §7 limitation (orphaned prior-scope chunks — additive, not a clean move) + §8 risk (a true "move" needs a new `DeleteBySourceMsgIDs` chunk-store method + wiring `RawMessageChunkStore` into the listen handler; deferred). Verification: `go build` (PG+SQLite) ✓, `go vet` ✓, sqlite store tests (incl. `embedded_at` reset) `-race` ✓. |
| 0.6-draft | 2026-06-17 | **True-move implemented (FR-08, operator-requested).** Added `RawMessageChunkStore.DeleteBySourceMsgIDs` (PG: `DELETE FROM raw_message_chunks WHERE source_msg_ids && $1::uuid[] <scopeClause> RETURNING source_msg_ids`, returns the union of deleted chunks' source IDs + deleted count; SQLite: no-op stub — chunk store is stubbed on lite) + `ListenRawMessageStore.ResetEmbeddedByIDs` (PG + SQLite — clears `embedded_at` only, tenant-scoped). `ListenRawMessagesHandler` gains an optional `chunkStore` field + constructor param (nil-safe); `handleUpdateScope` now runs a best-effort true-move after a successful `UpdateScope`: delete the message's old-scope chunks (incl. day-group neighbors, since the worker groups a chat's day into shared chunks) → compute neighbors (deleted sources minus changed IDs) → `ResetEmbeddedByIDs` so neighbors re-embed under their unchanged scope. Response gains `chunks_deleted` + `neighbors_requeued`. Wired in `cmd/gateway_http_handlers.go:98` (passes `stores.RawMessageChunks`). **Non-atomic:** failure degrades to additive (logged), recoverable via the embeddings menu. Scope = chunks only; KG entities still orphan (`003` §5.3). Tests: `TestHandleUpdateScope_TrueMove` + `_NoChunkStore` + `TestSQLiteListenRawMessageStore_ResetEmbeddedByIDs` — green; `go build`/`vet` (PG+SQLite) ✓. Flipped the §8 "additive" risk → implemented; §7 chunk-orphan note updated. New FR-08 live re-embed check left `[ ]` (manual). |
| 0.7-draft | 2026-06-17 | **Maximized automatable UI-logic coverage.** The repo has no `@testing-library/react` (component tests cover pure logic only — see `voice-picker.test.tsx`), so render/click/toast verification isn't possible without adding a dependency. Extracted the FR-04/FR-05 gating logic into pure helpers (`ui/web/src/pages/raw-messages/scope-helpers.ts`) consumed by the detail dialog + the selection toolbar: `scopeEditCanSave` (Save-enabled gate), `scopePrefillFromSelection` (batch prefill from homogeneous/mixed selection), `scopeHasInput` (≥1 field set). Added `scope-helpers.test.ts` — 19 cases, green. Strengthened the already-`[x]` FR-04/FR-05 gating-box evidence to point at these tests. `pnpm build` + `vitest` ✓. **Remaining 6 `[ ]` are all env-gated, not code-gated:** 5 are render/click/toast interaction (FR-04/05) needing a browser + `@testing-library/react` (not installed); 1 is the live re-embed check (FR-08) needing a running PG gateway + embedding worker + master-tenant data (local `goclaw-pg` is a seed DB with no master data per `003`-bugfix; the SQLite chunk store is a stub so FR-08's chunk-delete can't run on desktop). No further code work unblocks these — they require live verification. |

---

## 1. Summary

When a listen-only channel (e.g. WhatsApp) ingests messages with the **wrong `agent_id` and/or `graph_id`**, extracted knowledge-graph entities land in the wrong agent's graph scope. Today both fields are immutable after insert (`AppendBatch`, PG `pg/listen_raw_messages.go:59`), so an operator cannot correct the scoping of already-captured rows from the UI — they can only reset a message to "pending", which re-extracts it into the **same** (still-wrong) agent+graph.

This SRS defines the ability to **edit the `agent_id` and/or `graph_id` of existing `listen_raw_messages` rows** so the corrected values are used on reprocessing. The primary trigger is the defect class in `003-bugfix-whatsapp-group-graph-agent-mismatch.md`: RC1 (wrong agent — Jarvis instead of Felix) + RC2 (wrong graph — `project-sovereign` instead of `project-dell-ioh`). After the ingest-path fixes from `003` are live, this feature lets the operator correct the already-stored rows and have them re-extracted into the correct agent+graph.

**Verified behavior this relies on** (`003` §3, §11 + worker trace):
- `processAllPendingBatches` (`extract_worker.go:154`) polls `ListPendingGroups` — distinct **`(agent_id, graph_id)` pairs** with pending messages — then `processGroupBatch` (`extract_worker.go:186`) runs LLM extraction.
- Extracted entities/relations are scoped with `UserID = graphID` (`extract_worker.go:366,370`) and ingested via `KGStore.IngestExtraction(ctx, agentID, graphID, …)` (`extract_worker.go:387`) — both `agentID` and `graphID` are the scope.
- Therefore: **changing a row's `agent_id` and/or `graph_id` and marking it pending causes the worker to re-extract it under the corrected `(agent_id, graph_id)` pair.**
- **Embedding stage (parallel pipeline, same scope pair).** A separate embedding worker (`processEmbeddingGroupBatch`, `embedding_worker.go:181`) polls `ListPendingEmbeddingGroups` (distinct `(agent_id, graph_id)` pairs where `embedded_at IS NULL`), re-chunks each message body, and writes chunks to `raw_message_chunks` carrying the listen row's `agent_id`/`graph_id` (`embedding_worker.go:248-261`), then `MarkEmbedded`. The embeddings menu (`GET /v1/embeddings`) reads `raw_message_chunks` (`internal/http/embeddings.go:35-93`) — **not** `listen_raw_messages`. Therefore a scope edit must **also reset `embedded_at = NULL`** so the embedding worker re-picks the message up and fresh chunks land under the new scope; otherwise the message stays stuck under the old scope in the embeddings menu.

This SRS owns: the scope UPDATE store method (PG + SQLite), the HTTP endpoint, the UI controls (detail dialog edit + selection-toolbar batch), the i18n strings, and the documented limitation about orphaned prior-scope entities. It composes with `003-bugfix` (RC1+RC2) and reuses the existing reset/extraction pipeline unchanged.

## 2. Scope

**In scope**:

- A new store method `UpdateScope(ctx, ids []uuid.UUID, agentID, graphID string) (int64, error)` that — for the given IDs — sets whichever of `agent_id`/`graph_id` is provided (either or both) **and resets extraction state** (`processed_at = NULL`, `extraction_status = 'pending'`, `extraction_error = NULL`) in a single UPDATE. PostgreSQL (`internal/store/pg/listen_raw_messages.go`) + SQLite (`internal/store/sqlitestore/listen_raw_messages.go`). Empty `agentID`/`graphID` = leave that field unchanged.
- A new HTTP endpoint `POST /v1/listen-raw-messages/scope` accepting `{ "ids": [...], "agent_id"?: "...", "graph_id"?: "..." }` (at least one of agent/graph required), tenant-scoped, returning `{ "updated_count": N }`.
- Frontend hook method `updateScope(ids, { agentId?, graphId? })` in `use-raw-messages.ts`.
- UI: inline edit of **agent** (Select from the agents list) **and graph_id** (text input) in `RawMessageDetailDialog` (single message), and a batch "Update Scope" action in the selection toolbar (multi-message).
- i18n strings for the new controls in `raw-messages.json` for `en`, `vi`, `zh`.
- Authorization and validation rules for the new endpoint.

**Out of scope**:

- **Migrating or deleting KG entities already extracted under the prior `(agent_id, graph_id)`.** Editing scope + reprocessing creates new entities under the corrected scope; old entities under the previous scope remain (orphaned). Cleanup is the same data-remediation class as `003` §5.3 and is tracked separately (§7 limitation + §8 risks).
- Editing other fields (`chat_id`, `chat_name`, `body`, sender). Only `agent_id` + `graph_id` (the KG scope pair) are editable.
- Changing the extraction worker, the reset endpoint, or the reset-to-pending UI flow (those already work; this feature reuses them).
- Schema migration. Both columns already exist as freeform `TEXT` (`agent_id`, `graph_id` in `migrations/000050_listen_raw_messages.up.sql:4-5`, SQLite `schema.sql:1611-1612`); no DDL change, no new index required (the worker groups via `ListPendingGroups`; list filtering already works).
- Verifying that the supplied `agent_id` corresponds to a real agent **server-side**. There is no FK (`000050`), the handler does not hold an agent store, and the existing rows already store agent UUIDs without enforcement. Existence is enforced **client-side** via the agents `<Select>`; the server validates UUID format only (flagged §8).

## 3. Functional Requirements

### FR-00: Scope Update Resets Extraction State (one mutation)

Changing a raw message's `agent_id` and/or `graph_id` is a data-correctness operation: any prior extraction was scoped to the wrong agent/graph, so the `processed`/`extracted` state is stale and must be invalidated. The UPDATE therefore sets the provided scope field(s) **and** the extraction-reset fields together in one statement, so the message is immediately eligible for re-extraction by the existing worker under the corrected `(agent_id, graph_id)`.

| Field | Value after `UpdateScope` |
|-------|---------------------------|
| `agent_id` | new value if `agentID` provided; unchanged if empty |
| `graph_id` | new value if `graphID` provided; unchanged if empty |
| `processed_at` | `NULL` |
| `extraction_status` | `'pending'` |
| `extraction_error` | `NULL` |
| `extraction_attempts` | unchanged (worker retry cap `MaxExtractionAttempts` continues to apply) |
| `embedded_at` | `NULL` — the scope change invalidates the prior chunk embedding. Resetting it re-queues the message for the **embedding worker** (`ListPendingEmbeddings`, `embedding_worker.go:182`), which re-chunks the body and writes fresh chunks to `raw_message_chunks` under the **new** `(agent_id, graph_id)` (`embedding_worker.go:248-261`). The message then appears in the embeddings menu (`GET /v1/embeddings`, reads `raw_message_chunks`) under the new scope. Without this reset the worker's `embedded_at IS NULL` gate (`embedding_worker.go:271`) would never re-pick it up and the message would stay stuck under the old scope. |

At least one of `agentID`/`graphID` must be non-empty (enforced by the handler FR-03); the store builds the SET clause dynamically for whichever are provided.

Acceptance criteria:

- [x] `UpdateScope(ids, agentID="", graphID="x")` updates only `graph_id` + reset fields (agent unchanged). _(`TestSQLiteListenRawMessageStore_UpdateScope` graph-only case)_
- [x] `UpdateScope(ids, agentID="a", graphID="")` updates only `agent_id` + reset fields (graph unchanged). _(same test, agent-only case)_
- [x] `UpdateScope(ids, agentID="a", graphID="g")` updates both + reset fields. _(same test, both-fields case)_
- [x] A previously `extracted` (processed) message is `pending` after the update and returned by `ListPendingGroups` / `ListPending` for the **new** `(agent_id, graph_id)` pair. _(status flip verified in sqlite test; `ListPendingGroups` is a `DISTINCT` scan over the same `pending` rows, so pickup is logic-guaranteed; live worker run pending §10)_
- [x] After the update `embedded_at IS NULL`, so the embedding worker re-picks the message up under the new `(agent_id, graph_id)` and writes fresh chunks to `raw_message_chunks` — the message then appears in the embeddings menu under the new scope. _(embedded_at reset verified in sqlite test; the embeddings menu reads `raw_message_chunks`, which the worker repopulates; live re-embed pending §10)_
- [x] `ResetProcessedByIDs` and the existing reset UI flow are unchanged. _(untouched; `go build`/`vet` green)_

---

### FR-01: New Store Method `UpdateScope` (PG + SQLite, tenant-scoped)

Add `UpdateScope(ctx context.Context, ids []uuid.UUID, agentID, graphID string) (int64, error)` to the `ListenRawMessageStore` interface (`internal/store/listen_raw_message_store.go:70`), implemented in both `PGListenRawMessageStore` and the SQLite store.

Shape (generalizes `ResetProcessedByIDs` at `pg/listen_raw_messages.go:237`):
- Build `id IN (...)` placeholders (one per id).
- Build the SET clause from the provided fields: start with the reset fields (`processed_at = NULL, extraction_status = 'pending', extraction_error = NULL, embedded_at = NULL`); append `agent_id = $n` when `agentID != ""`; append `graph_id = $n` when `graphID != ""`. (`embedded_at = NULL` re-queues for the embedding worker — see FR-00.)
- Bind tenant via `scopeClause(ctx, idx)` (same helper `ResetProcessedByIDs` uses — PG) and the equivalent `tenant_id` binding (SQLite).

```sql
UPDATE listen_raw_messages
SET processed_at = NULL,
    extraction_status = 'pending',
    extraction_error = NULL,
    embedded_at = NULL
    [, agent_id = $a]
    [, graph_id = $g]
WHERE id IN (...)  <scopeClause>
```

Argument ordering: the handler validates "≥1 of agent/graph provided" (FR-03), so the store always receives at least one field to set. Empty `ids` slice returns `(0, nil)` (mirrors `ResetProcessedByIDs` `pg/listen_raw_messages.go:238`).

Acceptance criteria:

- [x] `UpdateScope` added to the `ListenRawMessageStore` interface and implemented in PG + SQLite. _(`internal/store/listen_raw_message_store.go`, `pg/listen_raw_messages.go`, `sqlitestore/listen_raw_messages.go`)_
- [x] PG UPDATE uses `dbFor(ctx)` / `scopeClause` so the change is confined to the caller's tenant (no cross-tenant write) — same call pattern as `ResetProcessedByIDs`. _(by inspection — mirrors the proven `ResetProcessedByIDs` `scopeClause(ctx, idx)` pattern; PG isolation not separately unit-tested, see §5)_
- [x] SQLite implementation mirrors the PG logic (same dynamic SET, same `tenant_id` scope binding), consistent with how `ResetProcessedByIDs` is duplicated across both stores. _(`TestSQLiteListenRawMessageStore_UpdateScope`)_
- [x] Unit test: `UpdateScope` on processed rows (graph-only, agent-only, both) flips them to `pending` with the new scope; a third row not in `ids` untouched; rows in another tenant untouched. _(SQLite covered by `TestSQLiteListenRawMessageStore_UpdateScope` + `_TenantIsolation`, `-race` green. **PG variant deferred** — needs pgvector pg18 test container; PG impl is a line-for-line mirror of the SQLite one, so the SQL logic is proven. See §5.)_

---

### FR-02: HTTP Endpoint `POST /v1/listen-raw-messages/scope`

Register a new route in `ListenRawMessagesHandler.RegisterRoutes` (`internal/http/listen_raw_messages.go:24`), guarded by the same `authMiddleware` (`requireAuth("", next)` → POST requires `Member`, tenant injected by `enrichContext`, `auth.go:506-530`) as the existing `/reset` route.

```text
POST /v1/listen-raw-messages/scope
```

Request body (mirrors `/reset` body shape at `listen_raw_messages.go:111-113`):

```json
{
  "ids": ["01234567-89ab-cdef-0123-456789abcdef"],
  "agent_id": "019d6771-abce-7ad1-8e4d-8ee0a211c3cc",
  "graph_id": "project-dell-ioh"
}
```

`agent_id` and `graph_id` are each optional, but **at least one** must be present and non-empty.

Response (200):

```json
{
  "updated_count": 1
}
```

Acceptance criteria:

- [x] `POST /v1/listen-raw-messages/scope` with valid `ids` + ≥1 valid scope field updates the rows and returns `{ "updated_count": N }`. _(`TestHandleUpdateScope` "both fields ok" / "agent only ok" / "graph only ok")_
- [x] Missing/empty `ids` → 400 `{ "error": "ids is required" }` (same guard as `handleResetByIDs` `listen_raw_messages.go:119-122`). _(`TestHandleUpdateScope` "empty ids")_
- [x] Any `id` not a valid UUID → 400 `{ "error": "invalid id: <s>" }` (same guard `listen_raw_messages.go:125-132`). _(`TestHandleUpdateScope` "invalid id uuid")_
- [x] Neither `agent_id` nor `graph_id` provided (both empty) → 400 `{ "error": "agent_id or graph_id is required" }` (NEW). _(`TestHandleUpdateScope` "neither field provided")_
- [x] `agent_id` present but not a valid UUID → 400 `{ "error": "invalid agent_id" }` (NEW). _(`TestHandleUpdateScope` "invalid agent_id")_
- [x] `graph_id` present but empty/whitespace or >255 chars → 400 (FR-03). _(`TestHandleUpdateScope` "graph_id too long"; whitespace-only graph with a valid agent is accepted as agent-only update)_
- [x] A caller scoped to tenant A passing an `id` that belongs to tenant B: the row is not matched (scopeClause excludes it), `updated_count` reflects only the caller's own rows — no cross-tenant mutation, no 500. _(proven at store level by `TestSQLiteListenRawMessageStore_UpdateScope_TenantIsolation`; the handler passes `r.Context()` straight through, so the store-level isolation is authoritative)_
- [x] Reachable only by an authenticated caller with at least `Member` role (`requireAuth("")` POST auto-detect, `auth.go:520`). _(by inspection — registered with `h.authMiddleware` = `requireAuth("", …)`, identical to `/reset`)_
- [x] Structured `slog.Info`/`slog.Warn` lines added for success and decode/validation failures, matching `handleResetByIDs` style (`listen_raw_messages.go:137,142`). _(by inspection)_

---

### FR-03: Validation

The handler trims and validates inputs before calling the store.

| Rule | Result |
|------|--------|
| `ids` empty | 400 `ids is required` |
| any `id` not a UUID | 400 `invalid id: <s>` |
| `agent_id` present and not a valid UUID | 400 `invalid agent_id` |
| `agent_id` present and valid UUID | accepted (trimmed of surrounding whitespace) |
| `graph_id` present, empty/whitespace after trim | 400 `graph_id is required` |
| `graph_id` longer than 255 chars | 400 `graph_id too long` |
| neither `agent_id` nor `graph_id` provided | 400 `agent_id or graph_id is required` |

Both fields are otherwise freeform (no FK; confirmed by schema `000050`/`schema.sql:1611-1612`), so no existence/allow-list check is enforced server-side. The stored `graph_id` is the trimmed string; the stored `agent_id` is the parsed UUID string.

Acceptance criteria:

- [x] Whitespace-padded values are trimmed before persist. _(`TestHandleUpdateScope` "graph only ok, whitespace trimmed")_
- [x] A `graph_id` of only spaces is rejected (empty after trim). _(whitespace-only `graph_id` with no `agent_id` falls through to the "neither provided" 400; with a valid `agent_id` it is accepted as an agent-only update — both in `TestHandleUpdateScope`)_
- [x] An `agent_id` that is not a UUID is rejected before the store is called. _(`TestHandleUpdateScope` "invalid agent_id")_

---

### FR-04: Frontend — Edit `agent_id` + `graph_id` in the Detail Dialog

In `RawMessageDetailDialog` (`ui/web/src/pages/raw-messages/raw-message-detail-dialog.tsx`), the **Agent** and **Graph ID** rows (currently read-only, `:21` and `:25`) become editable:

- The dialog receives the `agents` list (already loaded by the page via `useAgents()`, `raw-messages-page.tsx:30`) as a prop.
- An inline edit affordance (pencil toggle) reveals, when active:
  - **Agent**: a `<Select>` dropdown listing agents (`value = a.id`, label = `display_name || agent_key || id.slice(0,8)`), prefilled with the current `agent_id`. Same component already used by the page's agent filter (`raw-messages-page.tsx:288-300`).
  - **Graph ID**: a text input prefilled with `message.graph_id`.
- Save applies via `updateScope([message.id], { agentId?, graphId? })` — only changed fields are sent; at least one must differ from the current value to enable Save. Cancel reverts.
- On success: toast `scopeUpdated`; dialog closes and the parent list reloads (new agent/graph + `pending` status render). On failure: toast `scopeUpdateFailed`.
- Controls respect mobile UI rules (`text-base md:text-sm` on inputs; the dialog is already full-screen on mobile).

Acceptance criteria _(UI interaction — manual browser verification pending; code-complete, `pnpm build` green — same convention as `004`/`005`/`006` which leave browser checks `[ ]` until a live run)_:

- [ ] Opening a message's detail dialog shows editable Agent (dropdown) + Graph ID (text); selecting/changing values and saving persists them. _(manual)_
- [ ] After save, the list row shows the new agent + `graph_id` and status `pending` (parent reload). _(manual)_
- [x] Save is disabled when nothing changed or when graph_id is empty/whitespace. _(logic unit-tested: `scopeEditCanSave` in `scope-helpers.test.ts` — 9 cases incl. no-change, no-callback, saving, agent-only/graph-only/both change, empty + whitespace graph, padded-equal-no-change)_
- [ ] On error, an error toast is shown and inputs are left intact for correction. _(manual)_
- [x] Inputs use `text-base md:text-sm` (no iOS auto-zoom on mobile). _(by inspection — both inputs use the class)_

---

### FR-05: Frontend — Batch "Update Scope" in the Selection Toolbar

The selection toolbar (`raw-messages-page.tsx:389-405`) currently offers only `Reset to Pending`. Add an **"Update Scope"** action:

- Opens a small dialog with an **agent** `<Select>` (from the same `agents` list) and a **graph_id** text input. Each is optional; at least one must be set to confirm. When the selected rows share an agent/graph, prefill it; mixed selections start empty.
- Submit applies `updateScope([...selectedIds], { agentId?, graphId? })`. On success: clear selection, reload list + stats, toast `scopeUpdated` with count. On failure: error toast.
- Covers the common "a whole chat was captured under the wrong agent+graph" case in one action (matches the batch-reset UX).

Acceptance criteria:

- [ ] With ≥1 row selected, the "Update Scope" action is visible and enabled. _(manual)_
- [ ] Submitting with ≥1 scope field set updates all selected rows; selection clears and the list reloads showing the new agent/graph + `pending` status. _(manual)_
- [x] Submitting with both agent and graph empty is blocked client-side (disabled confirm) and server-side (FR-03). _(client gate `scopeHasInput` unit-tested in `scope-helpers.test.ts`; batch prefill homogeneity via `scopePrefillFromSelection` (5 cases); server: `TestHandleUpdateScope` "neither field provided")_

---

### FR-06: i18n (en / vi / zh)

New keys added to `ui/web/src/i18n/locales/{en,vi,zh}/raw-messages.json`, following the 3-locale rule (per the project Mobile/UI i18n rule and `004` FR-06). Proposed keys:

| Key | English | Vietnamese | Chinese |
|-----|---------|------------|---------|
| `actions.updateScope` | Update Scope | Cập nhật phạm vi | 更新范围 |
| `actions.scopeUpdated` | Updated scope on {{count}} message(s) | Đã cập nhật phạm vi cho {{count}} tin nhắn | 已为 {{count}} 条消息更新范围 |
| `actions.scopeUpdateFailed` | Failed to update scope | Cập nhật phạm vi thất bại | 更新范围失败 |
| `actions.scopePromptTitle` | Update Agent / Graph ID | Cập nhật Agent / Graph ID | 更新 Agent / Graph ID |
| `actions.scopeGraphLabel` | New Graph ID | Graph ID mới | 新 Graph ID |
| `actions.scopeAgentLabel` | New Agent | Agent mới | 新 Agent |
| `actions.scopeNoneProvided` | Set an agent or graph ID | Chọn agent hoặc graph ID | 请设置 agent 或 graph ID |
| `detail.editScope` | Edit | Chỉnh sửa | 编辑 |
| `detail.saveScope` | Save | Lưu | 保存 |
| `detail.cancel` | Cancel | Hủy | 取消 |
| `detail.scopeHint` | Changing the agent/graph resets this message to pending and re-extracts it into the new scope. | Đổi agent/graph sẽ đặt tin nhắn về pending và trích xuất lại vào phạm vi mới. | 更改 agent/graph 会将此消息重置为 pending 并重新提取到新范围。 |

Acceptance criteria:

- [x] All keys above exist in `raw-messages.json` for `en`, `vi`, `zh` with identical key sets. _(added to all 3 locale files)_
- [x] No raw key is rendered (the page already uses `useTranslation("raw-messages")`, `raw-messages-page.tsx:28`, matching the registered namespace). _(`pnpm build` green; namespace unchanged)_

---

### FR-07: Authorization & Tenant Scope

The new endpoint uses the same guard as the existing raw-message mutation routes. Per the project tenant-scope rule: `listen_raw_messages` is a **tenant-scoped** table (has `tenant_id`), so writes are gated by the store's `scopeClause(ctx)` binding `tenant_id` from context — no `requireMasterScope` / `requireTenantAdmin` is required. Role floor is `Member` (POST auto-detect via `requireAuth("")`, consistent with `/reset`).

Acceptance criteria:

- [x] The endpoint is registered with `requireAuth("", …)` (POST → `Member`), identical to `/reset` and `/reset-processed` (`listen_raw_messages.go:27-28`). _(by inspection)_
- [x] All UPDATE rows are confined to the caller's tenant via `scopeClause`; a caller cannot affect another tenant's rows. _(`TestSQLiteListenRawMessageStore_UpdateScope_TenantIsolation`)_
- [x] No `requireMasterScope`/`requireOwner` is added (this is tenant-scoped data, not a global table). _(by inspection)_

---

### FR-08: True-Move — Delete Old-Scope Chunks + Re-queue Neighbors

Without this, FR-00's `embedded_at` reset only makes the message re-embed under the **new** scope while its **old**-scope chunks remain in `raw_message_chunks` → the message shows under **both** scopes in the embeddings menu (additive, not a move). The embedding worker groups a chat's day of messages into **shared chunks** (`embedding_worker.go:221-268`), so a chunk's `source_msg_ids` is multi-message. Therefore deleting the edited message's chunks also removes chunks that covered **neighbor** messages (same chat/agent/graph/day); those neighbors must be re-queued for embedding so they re-embed under their unchanged scope.

On a successful `UpdateScope` (affected > 0) the handler, when a chunk store is wired, performs the true-move as a **best-effort, non-transactional** cleanup (failure is logged, the edit still succeeds — graceful degradation to additive):

1. `chunkStore.DeleteBySourceMsgIDs(ctx, ids)` — `DELETE FROM raw_message_chunks WHERE source_msg_ids && $1::uuid[] <scopeClause> RETURNING source_msg_ids` (PG array-overlap `&&`; SQLite is a no-op stub — chunk store is stubbed on lite). Returns the **union** of all source message IDs the deleted chunks covered + the deleted count.
2. Compute neighbors = returned sources **minus** the changed `ids`.
3. `store.ResetEmbeddedByIDs(ctx, neighbors)` — `UPDATE listen_raw_messages SET embedded_at = NULL WHERE id IN (...) <scopeClause>`, so the embedding worker re-embeds the neighbors under their unchanged scope.

Net effect: the edited message cleanly moves to the new scope; neighbors re-embed identically (new chunk IDs, same content/scope). Response gains `chunks_deleted` + `neighbors_requeued`. If `chunkStore` is nil (lite / unwired), cleanup is skipped — no panic, additive behavior.

Acceptance criteria:

- [x] After a successful scope edit with a wired chunk store, `DeleteBySourceMsgIDs` is called with the changed message IDs and the response includes `chunks_deleted`. _(`TestHandleUpdateScope_TrueMove`)_
- [x] Day-group neighbor IDs (in deleted-chunk sources but not the changed set) are passed to `ResetEmbeddedByIDs`; the changed IDs themselves are **not** (they were already reset by `UpdateScope`). _(`TestHandleUpdateScope_TrueMove`)_
- [x] The response reports `neighbors_requeued` = neighbor count. _(same test)_
- [x] A nil chunk store skips cleanup with no panic and still returns 200. _(`TestHandleUpdateScope_TrueMove_NoChunkStore`)_
- [x] `ResetEmbeddedByIDs` clears only `embedded_at` (scope + extraction state untouched) and is tenant-scoped. _(`TestSQLiteListenRawMessageStore_ResetEmbeddedByIDs`)_
- [x] `DeleteBySourceMsgIDs` is tenant-scoped (`scopeClause`) so a caller cannot delete another tenant's chunks. _(by inspection — mirrors `DeleteByChatID` `scopeClause(ctx, 2)` pattern)_
- [ ] Live: after editing a message's scope, confirm its old-scope chunks are gone from the embeddings menu and the neighbor messages re-appear (re-embedded) under their original scope. _(manual — needs running gateway)_

**Non-atomicity caveat:** the listen `UpdateScope`, the chunk delete, and the neighbor reset are separate statements with no cross-store transaction. A mid-sequence failure (e.g. chunk delete errors) is logged and leaves the system in the additive state (old chunks orphan) rather than rolling back the scope edit — acceptable for an admin remediation op, and recoverable via the embeddings menu's `delete-by-chat` + a manual reset. Cost: neighbor messages are re-embedded (re-run embedding API for their day-text) — a one-time admin cost.

## 4. System Impact

- **Store interface** (`internal/store/listen_raw_message_store.go`): add `UpdateScope(ctx, ids []uuid.UUID, agentID, graphID string) (int64, error)`.
- **PG store** (`internal/store/pg/listen_raw_messages.go`): implement `UpdateScope` generalizing `ResetProcessedByIDs` (`:237`) — `id IN (...)` placeholders + `scopeClause`, dynamic SET (reset fields + optional `agent_id`/`graph_id`).
- **SQLite store** (`internal/store/sqlitestore/listen_raw_messages.go`): implement `UpdateScope` mirroring the SQLite `ResetProcessedByIDs` (`:300`) — `?` placeholders + `tenant_id` scope + dynamic SET.
- **HTTP handler** (`internal/http/listen_raw_messages.go`): register `POST /v1/listen-raw-messages/scope` + `handleUpdateScope`; decode `{ids, agent_id?, graph_id?}`, trim/validate (FR-03), call store, log + respond. No change to existing routes.
- **Frontend hook** (`ui/web/src/pages/raw-messages/hooks/use-raw-messages.ts`): add `updateScope(ids, { agentId?, graphId? })`; extend the return object.
- **Frontend page + dialog** (`raw-messages-page.tsx`, `raw-message-detail-dialog.tsx`): pass `agents` into the dialog; selection-toolbar batch action (FR-05) + detail-dialog inline edit (FR-04).
- **i18n**: new keys in 3 locale files (FR-06).
- **True-move (FR-08):** `RawMessageChunkStore` gains `DeleteBySourceMsgIDs(ctx, msgIDs) (sources []uuid.UUID, deleted int64, err error)` (PG: array-overlap `&&` + `RETURNING source_msg_ids`; SQLite: no-op stub); `ListenRawMessageStore` gains `ResetEmbeddedByIDs(ctx, ids) (int64, error)`. `ListenRawMessagesHandler` gains a `chunkStore` field (optional, nil-safe) + constructor param; `handleUpdateScope` runs the best-effort cleanup (delete + neighbor reset) when `affected > 0` and `chunkStore != nil`; wired in `cmd/gateway_http_handlers.go:98`.
- **No schema migration**, no worker change, no new error-code registration (validation reuses existing inline 400 patterns; see §8).

## 5. Test Plan

- **Store unit test (PG, integration):** `UpdateScope` — (a) graph only, (b) agent only, (c) both — each flips processed rows to `pending` with the new scope; a row not in `ids` untouched; rows in another tenant untouched (scope). `_race` clean.
- **Store unit test (SQLite):** same assertions against the SQLite implementation, plus `ResetEmbeddedByIDs` (clears only `embedded_at`, tenant-scoped, empty-ids no-op).
- **Handler test:** valid `{ids, agent_id}` / `{ids, graph_id}` / `{ids, both}` → 200 `{updated_count}`; empty `ids` → 400; invalid UUID id → 400; invalid `agent_id` → 400; empty `graph_id` → 400; over-long `graph_id` → 400; both scope fields empty → 400; store-error → 500; plus **true-move** (FR-08): `DeleteBySourceMsgIDs` called, neighbor sources → `ResetEmbeddedByIDs`, response reports `chunks_deleted`/`neighbors_requeued`; nil chunk store → cleanup skipped, no panic.
- **Worker re-extract (manual/gate):** after `UpdateScope`, the corrected `(agent_id, graph_id)` appears in `ListPendingGroups` and the worker re-extracts into the new scope (verify via §7 SQL or the store/`IngestExtraction` logs with the new `agentID`/`graphID`).
- **Embedding true-move (manual):** after editing a message's scope, confirm its old-scope chunks are gone from the embeddings menu and neighbor messages re-embed under their original scope.
- **Frontend test:** detail-dialog Save (agent + graph) calls `updateScope` and reloads on success; Save disabled when nothing changed/empty graph; selection-toolbar batch clears selection + reloads; error toast on failure.
- **i18n:** `raw-messages.json` key sets identical across `en`/`vi`/`zh`; no raw keys rendered.
- **Checklist:** `go build ./...`, `go build -tags sqliteonly ./...`, `go vet ./...`, `pnpm build` in `ui/web`.

## 6. Decision Log (locked)

| Decision | Rationale |
|----------|-----------|
| **Edit both `agent_id` and `graph_id`** (the KG scope pair). | Operator confirmed the wrong-graph rows also carry the wrong agent (`003` RC1+RC2 double-defect). The worker groups and ingests by `(agent_id, graph_id)`; editing only `graph_id` would re-extract under the still-wrong agent. Editing the full scope pair makes reprocessing land correctly. |
| **One mutation = set provided scope field(s) + reset extraction state.** | Changing scope invalidates any prior extraction (it was scoped wrong). Folding the reset into the same UPDATE makes "edit scope → reprocess" a single user action — the stated goal. It also avoids a stale `extracted` status pointing at the wrong scope. The existing separate `Reset to Pending` flow is preserved for the no-change case. |
| **Endpoint shape mirrors `/reset`** (`POST …/scope`, `{ids, agent_id?, graph_id?}`). | Consistency with the sibling batch-mutation endpoint (`handleResetByIDs`, `listen_raw_messages.go:110`): same body shape, same UUID-parsing + `ids`-required guards, same tenant scope, same `Member` floor. One idiom for raw-message mutations. |
| **`agent_id`/`graph_id` stay freeform (no allow-list / existence check).** | Both are freeform `TEXT` with no FK (`000050`/`schema.sql`); the worker accepts any non-empty `graph_id` and any agent UUID as scope. Enforcing an allow-list would couple this admin tool to channel/agent-config state. Agent existence is enforced **client-side** via the agents `<Select>`; server validates UUID format only. A 255-char cap bounds pathological `graph_id` input. |
| **Do NOT migrate/delete old KG entities.** | Editing scope + reprocessing creates new entities under the corrected scope; old entities are orphaned. Removing them is destructive, tenant-data-specific remediation — the same class as `003` §5.3 — tracked separately (§7). Auto-deletion on an edit action would be a surprise. |
| **Role floor = `Member`** (parity with `/reset`), not admin. | The existing `/reset` (which triggers paid LLM re-extraction) is `Member`-level; a scope edit is no more privileged and reuses the same authorization envelope. Tightening to admin is an open question (§8), not a gating change. |

## 7. Known Limitations / Remediation (cross-reference)

- **Orphaned prior-scope entities (KG).** Entities already extracted under the **old** `(agent_id, graph_id)` are **not** removed by this feature. After editing + reprocessing, the corrected scope gains fresh entities, but the old scope still contains the stale set. For the `003` RC1+RC2 case (Jarvis/`project-sovereign` → Felix/`project-dell-ioh`), this is the same remediation gap as `003` §5.3 (re-key/delete mis-scoped KG entities). A separate cleanup task (operator-run, against a backup, per scope) is the intended path — out of scope here.
- **Orphaned prior-scope chunks (embeddings menu).** Largely resolved by the **true-move** (FR-08): on a successful scope edit the handler deletes the message's old-scope chunks (`DeleteBySourceMsgIDs`) and re-queues day-group neighbors. The message cleanly moves to the new scope in the embeddings menu. Residual risk: the cleanup is **best-effort, non-transactional** (FR-08 caveat) — a mid-sequence failure leaves old-scope chunks orphan; recover via `POST /v1/embeddings/delete-by-chat` (`internal/http/embeddings.go:122`, needs the **old** agent+chat). On lite (SQLite stub chunk store) the true-move is a no-op, so lite falls back to additive.
- **Reprocessing is asynchronous.** After the update, the message is `pending`; it is re-extracted only when the WhatsApp extraction worker next polls (`extract_worker.go:112`), and only if an LLM provider is available. Identical to existing `Reset to Pending` behavior.
- **`agent_id` existence not server-verified.** The server validates UUID format but does not confirm the agent exists in the tenant (no FK, no agent store wired to this handler). A garbage UUID would create pending messages that the worker re-extracts under a non-existent agent. Mitigated by the client `<Select>` restricting choices to real agents; flagged §8.

## 8. Risks and Open Questions

| Risk or question | Draft decision |
|------------------|----------------|
| Editing scope resets `extraction_status` even if the operator only wanted to relabel without re-extracting. | Accepted — there is no valid use of changing agent/graph without re-extracting (the fields only affect KG scoping). If a relabel-without-reprocess need appears, add a separate flag later. |
| Should the endpoint be admin-only (`requireAuth("admin")` / `requireTenantAdmin`) given it changes KG scoping? | Defer — keep parity with `/reset` (`Member`). If operators want tighter control, gate in a follow-up. The tenant-scope binding already prevents cross-tenant damage. |
| `agent_id` not existence-checked server-side could let an operator write a dead UUID. | Accepted — client `<Select>` restricts to real agents; server validates UUID format. Wiring an agent store into this handler for existence check is deferred (no FK exists today, so even ingest doesn't enforce it). |
| Old entities under the prior scope pollute that agent/graph after reprocessing. | Out of scope (§7); operator-run cleanup against a backup, like `003` §5.3. Surface in the UI hint (FR-06 `detail.scopeHint`) so the operator knows re-extraction is additive, not a move. |
| Re-embed is additive: the message appears in the embeddings menu under **both** the old scope (stale chunks) and the new scope (fresh chunks), not a clean "move". | **Implemented in FR-08 (v0.6).** `DeleteBySourceMsgIDs` (PG array-overlap, returns deleted sources) + `ResetEmbeddedByIDs` (re-queue day-group neighbors) auto-clean old-scope chunks on a successful scope edit. Best-effort + non-transactional (FR-08 caveat): a mid-sequence failure degrades to additive, recoverable via the embeddings menu `delete-by-chat`. Note **KG entities are still orphaned** (true-move covers chunks only, not the KG `entities`/`relations` — that remains the `003` §5.3 separate remediation). |
| Batch update across rows with **different** current agent/graph (mixed selection). | Allowed — overwrites all selected rows to the single new value(s). Prompt prefills the shared value when homogeneous; mixed starts empty. No server-side restriction. |
| `extraction_attempts` retained across the scope change may leave a message near `MaxExtractionAttempts`. | Accepted — `UpdateScope` does not reset attempts, matching `ResetProcessedByIDs`. If a message was abandoned (attempts ≥ cap), the normal worker skips it; operator uses the existing reset/abandoned path. Revisit if it blocks reprocessing in practice. |
| Confirm no other consumer reads `agent_id`/`graph_id` expecting immutability. | Verified: both read by the extraction/embedding worker (`ListPending*`, `IngestExtraction`) and list filters — none assume immutability. No FK references either column. |
| **True-move for chunks (FR-08), best-effort + non-transactional.** | Operator requested a clean "move" (message under new scope only, not both). Implemented via `DeleteBySourceMsgIDs` (PG array-overlap delete returning the deleted chunks' source union) + `ResetEmbeddedByIDs` (re-queue day-group neighbors whose shared chunks are co-deleted). Non-atomic by design: a mid-sequence failure degrades to additive rather than rolling back the scope edit — acceptable for an admin remediation op and recoverable via the embeddings menu. Scope limited to **chunks**; KG `entities`/`relations` are NOT auto-moved (separate `003` §5.3 remediation). |

## 9. Implementation Plan

1. **Store interface + PG impl:** add `UpdateScope` to `ListenRawMessageStore` (`listen_raw_message_store.go:70`); implement in `PGListenRawMessageStore` generalizing `ResetProcessedByIDs` (`pg/listen_raw_messages.go:237`) — dynamic SET (reset fields + optional `agent_id`/`graph_id`), `scopeClause`-scoped (FR-01).
2. **SQLite impl:** mirror PG in the SQLite store (FR-01), mirroring SQLite `ResetProcessedByIDs` (`sqlitestore/listen_raw_messages.go:300`).
3. **HTTP endpoint:** register `POST /v1/listen-raw-messages/scope` + `handleUpdateScope` in `internal/http/listen_raw_messages.go` (`:24` route table). Decode `{ids, agent_id?, graph_id?}`, trim/validate (FR-03), UUID-parse `ids` (reuse `handleResetByIDs` pattern `:124-133`), call `UpdateScope`, `slog` + respond `{updated_count}` (FR-02).
4. **i18n:** add all FR-06 keys to `en`, `vi`, `zh` `raw-messages.json`.
5. **Frontend hook:** add `updateScope(ids, { agentId?, graphId? })` to `use-raw-messages.ts` (`POST /v1/listen-raw-messages/scope`), extend the returned object.
6. **Frontend dialog:** inline Agent `<Select>` + Graph ID text edit in `RawMessageDetailDialog` (needs `agents` prop) — pencil toggle, Save/Cancel, success reload + toast, error toast (FR-04).
7. **Frontend page:** "Update Scope" batch action in the selection toolbar (`raw-messages-page.tsx:389-405`) with agent Select + graph input; pass `agents` into the dialog (FR-05).
8. **True-move (FR-08):** add `RawMessageChunkStore.DeleteBySourceMsgIDs` (PG array-overlap `&&` + `RETURNING source_msg_ids`; SQLite no-op stub) + `ListenRawMessageStore.ResetEmbeddedByIDs` (PG + SQLite); add an optional `chunkStore` field + constructor param to `ListenRawMessagesHandler`; in `handleUpdateScope` run the best-effort cleanup (delete old-scope chunks → neighbor reset) when `affected > 0 && chunkStore != nil`; wire `stores.RawMessageChunks` in `cmd/gateway_http_handlers.go:98`.
9. **Tests:** store (PG + SQLite), handler validation/tenant-scope/true-move, frontend edit/batch per §5.
10. **Checklist:** `go build ./...`, `go build -tags sqliteonly ./...`, `go vet ./...`, `pnpm build` in `ui/web`.
11. **Manual verification:** on the master tenant, take a raw message captured under the wrong agent+graph (a `003` RC1+RC2 row), edit both to the configured Felix + `project-dell-ioh`, confirm it returns to `pending`, confirm the extraction worker re-extracts it into the corrected scope (§7 / §5 SQL), **and** confirm the true-move (FR-08): the message's old-scope chunks are gone and fresh chunks land under the new scope in the embeddings menu (`/t/{tenant}/embeddings`) — the message shows under **only** the new Felix/`project-dell-ioh` scope (not both). Also confirm any day-group neighbor messages re-embed under their original scope. (KG entities under the old scope are NOT auto-moved — see §7.)

## 10. Proposed Error Codes

The endpoint reuses the existing inline `400` validation style of `handleResetByIDs` (`listen_raw_messages.go:116,120,129`) rather than introducing canonical error codes, matching the established pattern for this handler family. For traceability:

| Code (inline → proposed canonical mapping) | Meaning |
|------|---------|
| `request.validation_failed` | `ids` missing/empty, any `id` not a UUID, `agent_id` not a UUID, `graph_id` missing/empty/whitespace or >255 chars, or neither `agent_id` nor `graph_id` provided — returned inline as 400 `{"error": "..."}` today; if this handler family is later canonicalized, map to this code. |
| `listen_raw_message.not_found` | All supplied `id`s were outside the caller's tenant scope (no rows matched) — currently surfaces as `updated_count: 0`, not an error. No change required. |

No new canonical error code is registered by this feature; it follows the existing raw-message handler convention.
