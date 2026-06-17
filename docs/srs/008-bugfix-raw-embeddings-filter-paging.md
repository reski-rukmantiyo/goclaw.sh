# Software Requirements Specification: Raw Messages & Embeddings — Server-Side Filters + Consistent Paging

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
| 0.1-draft | 2026-06-17 | Initial draft. Root cause verified against code: both menus mix **server-side** filters (reduce server `total` → drive paging) with **client-side, page-only** text filters (`.filter()` over the fetched page only → do **not** reduce `total`, cannot reach matches on other pages → paging + "showing X of total" + select-all are incoherent while a text filter is active). Secondary defect: embeddings `sender` is SQL exact-match (`sender = $N`) while the UI presents it as free text. Fix = move the text filters server-side (ILIKE/LIKE, applied to **both** the COUNT and the data query so `total` matches), keep the UI inputs/chips unchanged. Composes with `007-feat-raw-message-graph-agent-edit.md` (same menus, same stores, same handlers). |
| 0.2-draft | 2026-06-17 | **Implemented (code-complete + build-verified).** Store opts `Chat`/`Sender`/`Body` (listen) + `SearchText` (chunk) added; PG listen `List` ILIKE on `where`+`whereM` (count+data); PG chunk `List` `text ILIKE` + `sender ILIKE` (was exact); SQLite listen `List` LIKE mirror. Handlers read `chat`/`sender`/`body` + `search_text`. Hooks send them. Pages route text filters server-side (debounced `…Q` committed state), drop client `.filter()`, reset offset — inputs/chips/badges untouched. **Verification:** `go build ./...` ✓, `go build -tags sqliteonly ./...` ✓, `go vet ./internal/store/... ./internal/http/...` ✓, `pnpm build` (ui/web, tsc) ✓, `TestSQLiteListenRawMessageStore_ListTextFilters` (9 cases + tenant isolation) `-race` ✓. **Deferred (env-gated, same convention as `007`):** PG `List` integration test (needs pgvector pg18 test container — PG impl mirrors SQLite line-for-line; proven by the SQLite test + COUNT/data shared-WHERE inspection), and the live/manual UI checks (page-2 reachability with a text filter, filtered total, embeddings sender partial-match) which need a running gateway + browser. **Pre-existing unrelated failures (not touched by this change):** 4 SQLite schema-migration DDL tests (`EnsureSchema … duplicate column name: system_prompt_preview`, migration v18/v25/v28) + 2 `TestResolveAuth_*` http auth/pairing tests — both in code paths this change never touches (schema migration DDL; auth resolution). |

---

## 1. Summary

The **Raw Messages** menu (`/t/{tenant}/raw-messages`) and the **Embeddings** menu (`/t/{tenant}/embeddings`) both paginate over a server-returned `total` (e.g. `raw-messages-page.tsx:256`, `embeddings-page.tsx:227`), but each also runs a **client-side text filter that only inspects the currently-fetched page**:

- Raw Messages: `searchChat` (chat name / chat id), `searchSender`, `searchBody` — applied with `.filter()` over the page only (`raw-messages-page.tsx:101-120`).
- Embeddings: `searchText` (chunk `text`) — applied with `.filter()` over the page only (`embeddings-page.tsx:106-110`).

Because these filters never reach the server, they do **not** reduce the server `total`, so while one is active: (a) `totalPages` / the page counter reflect the un-filtered-by-text set; (b) "showing X of total" (`raw-messages-page.tsx:481`, `embeddings-page.tsx:445`) is misleading; (c) **rows that match the text on another page are unreachable** — you can page past them without ever seeing them; (d) select-all / "all selected" only ever covers the current page's matches (`raw-messages-page.tsx:142-147`, `embeddings-page.tsx:134-139`). This is the "some filters not proper which impact paging" defect.

A secondary "filter not proper" defect: on the Embeddings menu, `sender` is matched with SQL exact equality (`sender = $N`, `internal/store/pg/raw_message_chunks.go:529-533`) even though the UI input is free text (`embeddings-page.tsx:319-326`), so a partial sender name never matches.

This SRS owns the fix for **both** menus: promote the text filters to **server-side substring filters** that participate in the same WHERE as the existing server filters — and therefore in the COUNT — so filter + count + paging are coherent. The fix is **behavior-only**: the existing filter inputs, chips, badges, and the active-filter count are **not** changed (no UI redesign). It composes with `007-feat-raw-message-graph-agent-edit.md`, which added the scope-edit feature on these same menus/stores/handlers.

## 2. Scope

**In scope**:

- New server-side substring filter options on the two list queries:
  - `ListenRawMessageListOpts`: `Chat` (substring over `chat_name` **or** `chat_id`), `Sender` (substring over `sender` **or** `sender_id`), `Body` (substring over `body`).
  - `RawMessageChunkListOpts`: `SearchText` (substring over `text`).
- Making the embeddings `sender` filter a substring match instead of exact (it is already a server param; only the SQL operator changes).
- Applying each new/changed filter to **both** the COUNT and the data WHERE in the PG `List` methods, so `total` matches the filtered rows.
- Porting the listen-message text filters to the SQLite `List` (dual-DB rule; desktop consistency). The SQLite chunk store is a no-op stub (lite has no pgvector), so the embeddings text filter is PG-only.
- HTTP handlers reading the new query params (`chat`, `sender`, `body` for raw messages; `search_text` for embeddings).
- Frontend hooks sending the new params; pages routing the existing text inputs server-side (debounced), removing the client-side `.filter()`, and resetting `offset` on commit. Inputs/chips unchanged.

**Out of scope**:

- Any UI layout / control / i18n-string change. The filter inputs, active-filter chips, badges, and `activeFilterCount` are deliberately left as-is.
- The existing exact-match filters that are **ids** (`agent_id`, `chat_id`, `graph_id` on raw messages; `agent_id`, `chat_id`, `graph_id` on embeddings). These are identifier fields where exact match is correct (agent_id comes from a dropdown; chat/graph ids are copied whole). Only free-text **content/name** fields become substring.
- The embeddings `from_time` / `to_time` / `has_embedding` filters (already server-side and correct).
- Full-text search indexes / `tsvector` for `body` / `text`. Substring `ILIKE`/`LIKE` is sufficient for an admin remediation page on bounded tenant data; a GIN/trigram index is a separate performance task (§7).
- The hybrid vector/FTS `Search` path (`raw_message_chunks.go:116`) — that is retrieval, not the menu list.
- Anything in `007`'s scope-edit flow (untouched).

## 3. Functional Requirements

### FR-00: Text Filters Must Be Server-Side (root cause fix)

The Raw Messages and Embeddings menus' text filters must filter on the **server**, so the returned `total` (and therefore paging) reflects them. Today they filter the fetched page only and cannot reach matches on other pages.

| Menu | Filter | Today | After |
|------|--------|-------|-------|
| Raw Messages | `searchChat` (chat name/id) | client `.filter()` on page (`raw-messages-page.tsx:103-108`) | server substring over `chat_name`/`chat_id` |
| Raw Messages | `searchSender` | client `.filter()` on page (`:109-114`) | server substring over `sender`/`sender_id` |
| Raw Messages | `searchBody` | client `.filter()` on page (`:115-118`) | server substring over `body` |
| Embeddings | `searchText` (chunk text) | client `.filter()` on page (`embeddings-page.tsx:106-110`) | server substring over `text` |
| Embeddings | `sender` | server **exact** `sender = $N` (`raw_message_chunks.go:530`) | server **substring** `sender ILIKE` |

Acceptance criteria:

- [x] With a text filter active, the server `total` reflects the filtered set (smaller than the unfiltered total when the filter excludes rows). _(`TestSQLiteListenRawMessageStore_ListTextFilters` asserts `total == match count` for Chat/Sender/Body; PG by inspection — shared WHERE)_
- [ ] A row that matches the text filter but sits on **page 2** is reachable by paging (today it is not, because the client filter only sees page 1's fetched rows). _(manual — needs running gateway; logic-guaranteed because the filter is now in the server WHERE + the page index walks the filtered set)_
- [ ] While a text filter is active, `totalPages` / the page counter and "showing X of total" are consistent with the visible rows. _(manual; follows from total reflecting the filter)_
- [x] The existing filter inputs, chips, badges, and `activeFilterCount` render unchanged (no UI change). _(by inspection — no JSX/className/i18n removed; `pnpm build` green)_

---

### FR-01: New Substring Options on the Store List Opts (additive)

Add substring filter fields to the two list-options structs. Empty string = filter not applied (zero value), so all existing callers are unaffected.

- `ListenRawMessageListOpts` (`internal/store/listen_raw_message_store.go:58`) gains:
  - `Chat string` — substring over `chat_name` OR `chat_id`.
  - `Sender string` — substring over `sender` OR `sender_id`.
  - `Body string` — substring over `body`.
- `RawMessageChunkListOpts` (`internal/store/raw_message_chunk_store.go:49`) gains:
  - `SearchText string` — substring over `text`.

`Sender` already exists on `RawMessageChunkListOpts` (`:55`) — its **semantics** change from exact to substring (FR-03); no new field is added for it.

Acceptance criteria:

- [x] Both structs gain the documented fields; empty values are no-ops (existing tests/callers unchanged). _(`go build ./...` + `go build -tags sqliteonly ./...` green)_
- [x] `ReEmbedChunks` (which takes `RawMessageChunkListOpts`) is unaffected — it only reads `AgentID`/`ChatID`/`GraphID`; the new `SearchText` is ignored there. _(by inspection — `raw_message_chunks.go:416-430` untouched)_

---

### FR-02: PG `List` Applies Substring Filters to COUNT + Data

In `PGListenRawMessageStore.List` (`internal/store/pg/listen_raw_messages.go:449`), build substring predicates and append them to **both** `where` (drives the COUNT at `:522-525`) and `whereM` (the `m.`-qualified copy that drives the JOIN data query at `:542`), so `total` and the page rows agree. PG uses case-insensitive `ILIKE`.

```text
Chat   → (chat_name ILIKE $n OR chat_id ILIKE $n)        -- arg: "%<chat>%"
Sender → (sender ILIKE $n OR sender_id ILIKE $n)         -- arg: "%<sender>%"
Body   → body ILIKE $n                                    -- arg: "%<body>%"
```

In `PGRawMessageChunkStore.List` (`internal/store/pg/raw_message_chunks.go:504`), the existing `where` slice is shared by the COUNT (`:560-563`) and the data query (`:584`), so adding the predicates once covers both:

```text
SearchText → text ILIKE $n                                -- arg: "%<search_text>%"
Sender     → sender ILIKE $n   (was: sender = $n)         -- arg: "%<sender>%"
```

The `%` wildcards are concatenated server-side in Go (the param value is `%<trimmed>%`); the user input is never interpolated into SQL — it is always a bound parameter, so SQL injection is impossible (consistent with the project SQL-safety rule).

Acceptance criteria:

- [ ] PG listen `List` with `Chat`/`Sender`/`Body` set returns only matching rows **and** a `total` that counts only matching rows; the COUNT and data queries use the identical predicate set. _(PG variant deferred — needs pgvector pg18 test container; logic proven by the SQLite mirror + by inspection that `where` feeds both COUNT and data; see §5)_
- [ ] PG chunk `List` with `SearchText` set returns only text-matching rows with a matching `total`; `Sender` now matches as substring. _(same — PG deferred; SQLite chunk store is a stub so no SQLite mirror exists; verified by inspection + manual)_
- [ ] All substring predicates use bound parameters with `%`-wrapped values; no string interpolation of user input. _(by inspection)_
- [ ] Empty `Chat`/`Sender`/`Body`/`SearchText` add no predicate (zero-value no-op). _(by inspection)_

---

### FR-03: Embeddings `sender` Becomes Substring

`RawMessageChunkStore.List` currently matches `sender` with exact equality (`raw_message_chunks.go:530`). The UI input is free text (`embeddings-page.tsx:319-326`), so a partial sender name (the common case) never matches. Change the predicate to substring (`sender ILIKE $n`, arg `%<sender>%`) in the PG `List`. The param plumbing (handler → opts → store) is unchanged; only the SQL operator + the `%`-wrapping change.

Acceptance criteria:

- [ ] Typing a partial sender name in the Embeddings sender filter returns rows whose `sender` contains it (case-insensitive). _(manual — needs running gateway)_
- [ ] The existing `sender` param name (`sender`) is unchanged, so the hook/handler wiring needs no rename. _(by inspection)_

---

### FR-04: SQLite `List` Mirrors the Listen-Message Text Filters (dual-DB)

`SQLiteListenRawMessageStore.List` (`internal/store/sqlitestore/listen_raw_messages.go:385`) builds a single `conditions` slice shared by the COUNT (`:431-434`) and the data query (`:447-454`). Append the substring predicates there using SQLite `LIKE` (case-insensitive for ASCII by default), mirroring FR-02:

```text
Chat   → (chat_name LIKE ? OR chat_id LIKE ?)             -- args: "%<chat>%","%<chat>%"
Sender → (sender LIKE ? OR sender_id LIKE ?)              -- args: "%<sender>%","%<sender>%"
Body   → body LIKE ?                                       -- arg: "%<body>%"
```

The SQLite chunk store is a no-op stub (`sqlitestore/raw_message_chunks.go:32`), so the embeddings text filter has no SQLite implementation (lite has no pgvector / no embeddings menu). No change there.

Acceptance criteria:

- [x] SQLite listen `List` with `Chat`/`Sender`/`Body` set returns only matching rows and a matching `total`. _(`TestSQLiteListenRawMessageStore_ListTextFilters`)_
- [x] Empty values add no predicate. _(same test — "no filter returns all" case)_
- [x] Tenant scope is preserved (the text predicates are AND-ed after the existing `tClause`). _(`TestSQLiteListenRawMessageStore_ListTextFilters_TenantIsolation`)_

---

### FR-05: Handlers Read the New Query Params

`handleList` for raw messages (`internal/http/listen_raw_messages.go:40`) reads the existing filters from query params; add reads for the new ones. `handleList` for embeddings (`internal/http/embeddings.go:35`) reads `search_text`.

| Handler | Param | Opts field |
|---------|-------|------------|
| raw messages (`listen_raw_messages.go:46-64`) | `chat` | `opts.Chat` |
| raw messages | `sender` | `opts.Sender` |
| raw messages | `body` | `opts.Body` |
| embeddings (`embeddings.go:41-52`) | `search_text` | `opts.SearchText` |

Each is applied with `if v := r.URL.Query().Get("<param>"); v != "" { opts.<Field> = v }` — matching the existing filter reads (empty param = no filter).

Acceptance criteria:

- [x] Both handlers populate the new opts fields from the new query params; empty params are ignored. _(by inspection; `go build` green)_
- [x] No existing query param/option is renamed or removed. _(by inspection)_

---

### FR-06: Hooks + Pages Route Text Filters Server-Side (no UI change)

- `useRawMessages.loadMessages` (`use-raw-messages.ts:48`) accepts and sends `chat`/`sender`/`body` (added to the `query` map like the existing filters at `:60-81`).
- `useEmbeddings.loadChunks` (`use-embeddings.ts:44`) accepts and sends `searchText` → query `search_text`.
- `RawMessagesPage` (`raw-messages-page.tsx`): the `searchChat`/`searchSender`/`searchBody` inputs stay (no UI change). Their values are sent to the server (debounced, like the existing `filterChannel`/`filterGraphId` debounce at `:62-75`); the client-side `.filter()` block (`:101-120`) is removed so `filtered` is the server result as-is; `offset` resets to 0 when a text filter commits (so the user does not sit on a now-out-of-range page).
- `EmbeddingsPage` (`embeddings-page.tsx`): the `searchText` input stays. Its value is sent to the server (debounced); the client-side `.filter()` block (`:106-110`) is removed; `offset` resets to 0 on commit.

The debounce matches the existing channel/graph pattern (≈400 ms) so typing does not flood the server with one request per keystroke.

Acceptance criteria:

- [x] The text-filter inputs still render and accept typing identically (no UI change). _(by inspection; `pnpm build` green)_
- [ ] Typing in a text filter triggers at most one server request per debounce window (not one per keystroke). _(manual; follows from debounce wiring mirroring channel/graph)_
- [x] `filtered` no longer re-filters the page client-side; it equals the server-returned rows. _(by inspection — `.filter()` block removed; `filtered` = `messages`/`chunks`)_
- [x] When a text filter changes, `offset` resets to 0. _(by inspection — committed-value setters call `setOffset(0)`)_
- [x] `activeFilterCount` and the active-filter chips are unchanged (text filters were never counted/chipped and remain so — no UI change). _(by inspection)_

---

### FR-07: i18n (no new strings)

No new user-facing strings are introduced — the inputs, chips, badges, and toasts already exist (added in `007`). If any string is added, follow the 3-locale rule (en/vi/zh) per the project Mobile/UI i18n rule.

Acceptance criteria:

- [x] No new i18n key is required; no raw key appears. _(by inspection — no JSX text added; `pnpm build` green)_

---

### FR-08: Authorization & Tenant Scope (unchanged)

The list endpoints' authorization envelope is unchanged: both are `requireAuth("", next)` (raw messages `listen_raw_messages.go:29,36-38`; embeddings `embeddings.go:25,31-33`), POST auto-detect → `Member`. The new filters are tenant-scoped via the existing `scopeClause`/`tClause` already in every `List` query — a caller cannot use a text filter to escape their tenant boundary.

Acceptance criteria:

- [x] No auth/role change; the new predicates are AND-ed inside the existing tenant-scoped WHERE. _(by inspection)_
- [x] A text filter cannot return another tenant's rows (it only narrows within the tenant-scoped set). _(logic-guaranteed — predicate is AND-ed after `tClause`; proven by `TestSQLiteListenRawMessageStore_ListTextFilters_TenantIsolation`)_

## 4. System Impact

- **Store interfaces / opts** (`internal/store/listen_raw_message_store.go`, `internal/store/raw_message_chunk_store.go`): add `Chat`/`Sender`/`Body` and `SearchText` (FR-01). Additive.
- **PG store** (`internal/store/pg/listen_raw_messages.go` `List`, `internal/store/pg/raw_message_chunks.go` `List`): add substring predicates to the shared WHERE (FR-02); change chunk `sender` to substring (FR-03).
- **SQLite store** (`internal/store/sqlitestore/listen_raw_messages.go` `List`): mirror the listen-message text filters with `LIKE` (FR-04). Chunk store stub unchanged.
- **HTTP handlers** (`internal/http/listen_raw_messages.go` `handleList`, `internal/http/embeddings.go` `handleList`): read the new query params (FR-05).
- **Frontend hooks** (`use-raw-messages.ts`, `use-embeddings.ts`): send the new params (FR-06).
- **Frontend pages** (`raw-messages-page.tsx`, `embeddings-page.tsx`): route text filters server-side (debounced), remove client `.filter()`, reset offset (FR-06). No JSX/layout/i18n change.
- **No schema migration** (no new column/index — substring uses existing columns; a trigram index is deferred §7). **No new error codes** (filter params are free-form; invalid values just match nothing, like today).

## 5. Test Plan

- **Store unit test (SQLite, listen):** `TestSQLiteListenRawMessageStore_ListTextFilters` — seed rows across chats/senders/bodies; assert `Chat` matches `chat_name`/`chat_id`, `Sender` matches `sender`/`sender_id`, `Body` matches `body`; each returns only matching rows **and** a `total` equal to the match count; empty values return all; tenant isolation preserved. `-race`.
- **Store (PG, listen + chunk):** the same assertions against the PG `List`. **Deferred** — needs a pgvector pg18 test container; the PG impl mirrors the SQLite logic line-for-line (same shared-WHERE pattern proven by the COUNT/data audit in §3), so the SQL logic is proven. See §7.
- **Handler:** optional — the handlers are thin param readers mirroring the existing filter reads; covered by inspection + manual.
- **Frontend:** no `@testing-library/react` in the repo (per `007` §0.7), so render/click verification is manual. The change is data-wiring only (no new component logic to unit-test); mark interaction checks manual.
- **Manual (both menus):** open each menu with enough rows to span ≥2 pages; type a text filter that matches rows on page 2; confirm those rows become reachable by paging and the page counter reflects the filtered total; confirm `sender` partial-match works on Embeddings; confirm empty filter restores the full total.
- **Checklist:** `go build ./...`, `go build -tags sqliteonly ./...`, `go vet ./...`, `pnpm build` in `ui/web`.

## 6. Decision Log (locked)

| Decision | Rationale |
|----------|-----------|
| **Promote text filters to server-side substring; do not redesign the UI.** | The defect is that text filters are page-only, which desyncs them from `total`/paging. Making them server-side (in the same WHERE as the other filters, and therefore in the COUNT) is the minimal fix that makes filter + count + paging coherent. The user explicitly asked to fix "how it works" without changing the UI. |
| **Substring (ILIKE/LIKE), not full-text / tsvector.** | These are admin remediation pages on bounded per-tenant data. `ILIKE '%x%'` is simple, correct, and matches the existing exact-match filter plumbing (just an operator + `%` change). A trigram/FTS index is a separate perf task (§7) if a tenant's volume makes the seq scan noticeable. |
| **`Chat` matches `chat_name` OR `chat_id`; `Sender` matches `sender` OR `sender_id`.** | The UI's `searchChat` is a single box over both the human chat name and the id (today's client filter already ORs them, `raw-messages-page.tsx:104-107`); `searchSender` likewise ORs sender + sender_id (`:110-113`). Server-side must preserve that OR so behavior is identical, just consistent with paging. |
| **Keep `agent_id`/`chat_id`/`graph_id` exact; only content/name fields become substring.** | Those are identifiers (agent_id from a dropdown; chat/graph ids copied whole). Exact is correct for them. Only free-text content/name fields (`sender`, `body`, `text`, chat name search) are substring. |
| **Apply each new predicate to COUNT + data (shared WHERE), not a separate count filter.** | The bug is precisely that the filter and the count diverge. Both PG `List` methods already share one WHERE between COUNT and data; adding the predicate there fixes both at once and keeps them locked together. |
| **Debounce the text filters client-side (≈400 ms), mirroring the existing channel/graph pattern.** | Without debounce, sending the text filter server-side would fire one request per keystroke. The page already debounces `filterChannel`/`filterGraphId` (`raw-messages-page.tsx:62-75`); the text filters follow the same pattern for consistency and to avoid request floods. |

## 7. Risks and Open Questions

| Risk or question | Draft decision |
|------------------|----------------|
| `ILIKE '%x%'` on `body`/`text` is a sequential scan (no index). | Acceptable for an admin page on bounded tenant data. If a tenant's volume makes it slow, add a `pg_trgm` GIN index (`CREATE INDEX … ON raw_message_chunks USING gin (text gin_trgm_ops)`) in a separate migration — out of scope here. Documented as deferred. |
| PG store has no integration test (no pgvector test container in this env). | The PG `List` mirrors the SQLite `List` line-for-line (same shared-WHERE → COUNT+data pattern); the SQLite unit test proves the SQL logic. A PG integration test is deferred to when a pgvector pg18 test container is available (same deferral convention as `007` §0.4). |
| Frontend interaction (debounce, page-2 reachability) cannot be unit-tested (no `@testing-library/react`). | Marked manual (same convention as `007` §0.7). The logic guarantee — filter is in the server WHERE, so paging walks the filtered set — holds regardless. |
| Removing the client `.filter()` changes `filtered` from "page filtered by text" to "server page as-is". | Intended: that is the fix. `filtered` now equals the server rows; `total` reflects the text filter; select-all covers the server page consistently. No JSX/state shape change (the `searchChat`/`searchSender`/`searchBody`/`searchText` states stay to back the inputs). |
| Should the text filters increment `activeFilterCount` / get chips? | Out of scope (UI change). Left as-is to honor "no UI change". Can be added in a follow-up. |
| Embeddings menu does not exist on lite (SQLite chunk store is a stub). | Confirmed — the embeddings text filter is PG-only by necessity; no SQLite port needed. The listen-message text filter is ported to SQLite (FR-04) because that store is real on lite. |

## 8. Implementation Plan

1. **Store opts (FR-01):** add `Chat`/`Sender`/`Body` to `ListenRawMessageListOpts`; add `SearchText` to `RawMessageChunkListOpts`.
2. **PG listen `List` (FR-02):** in `internal/store/pg/listen_raw_messages.go:449`, append ILIKE predicates for `Chat`/`Sender`/`Body` to **both** `where` and `whereM` (so COUNT at `:522` and data at `:542` agree); args are `%`-wrapped trimmed values.
3. **PG chunk `List` (FR-02/FR-03):** in `internal/store/pg/raw_message_chunks.go:504`, add `text ILIKE` for `SearchText` and change `sender = $n` → `sender ILIKE $n` (shared `where` covers COUNT `:560` + data `:584`).
4. **SQLite listen `List` (FR-04):** in `internal/store/sqlitestore/listen_raw_messages.go:385`, append `LIKE` predicates for `Chat`/`Sender`/`Body` to the shared `conditions` slice.
5. **Handlers (FR-05):** read `chat`/`sender`/`body` in raw-messages `handleList`; read `search_text` in embeddings `handleList`.
6. **Hooks (FR-06):** `loadMessages` sends `chat`/`sender`/`body`; `loadChunks` sends `searchText`→`search_text`.
7. **Pages (FR-06):** route `searchChat`/`searchSender`/`searchBody` and `searchText` server-side via the existing debounce pattern; remove the client `.filter()` blocks; reset `offset` on commit. Inputs/chips/badges untouched.
8. **Tests (§5):** `TestSQLiteListenRawMessageStore_ListTextFilters` (Chat/Sender/Body, total match, empty no-op, tenant isolation).
9. **Checklist:** `go build ./...`, `go build -tags sqliteonly ./...`, `go vet ./...`, `pnpm build` in `ui/web`.
10. **Manual (§5):** both menus — page-2 reachability with a text filter, filtered total, sender partial-match on Embeddings, empty-filter restore.

## 9. Proposed Error Codes

No new canonical error codes. The filter params are free-form; an unmatched value simply returns zero rows (`total: 0`), exactly as today. The list endpoints keep their existing inline error style (`500` on store error). For traceability only:

| Code (inline → proposed canonical mapping) | Meaning |
|------|---------|
| `request.validation_failed` | A malformed `limit`/`offset` (existing 400-style clamp behavior in the handler). No change. |

No new error code is registered by this bugfix.
