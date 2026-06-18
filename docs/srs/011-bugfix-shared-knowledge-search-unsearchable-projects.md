# Software Requirements Specification: `shared_knowledge_search` Cannot Find Projects Searched by Group Name (`chat_name` Not in FTS Index)

**Project**: GoClaw Gateway
**Release**: 2026.3.0
**Version**: 0.4-draft
**Date**: 2026-06-18
**Status**: **Closed** — RC2 (the defect) fixed + live-verified at the retrieval layer (SQL proof uses the identical `tsv @@` predicate the tool's `ftsSearch` runs; the RRF-merge / result-format / LLM layers above are unchanged code). RC1 observability shipped. RC3 (per-group own `graph_id`) is explicitly **optional** and **deferred** — not required for searchability (RC2 already makes the named projects searchable), tracked in §6.5 / §13.5, not a closure blocker.
**Difficulty**: Low–Medium
**Estimate**: 0.5–1 days
**Tenant**: Master (`0193a5b0-7000-7000-8000-000000000001`), agent **Raka** (`019ec54f-6fab-7b2b-b509-a28483049ed9`)

---

## Revision History

| Version | Date | Changes |
|---------|------|---------|
| 0.1-draft | 2026-06-18 | Initial draft. Hypothesized three causes: RC1 (silent scope drop — tool only searches `shared_kg_ids` allow-list), RC2 (`chat_name` not in FTS `tsv`), RC3 (mis-scoped data). Proposed RC1 fix = union searched scopes with discovered `DISTINCT graph_id`. |
| 0.2-draft | 2026-06-18 | **Operator review corrected the framing.** RC1 is **by design**: the `shared_kg_ids` allow-list is intentional operator-controlled scoping (an agent deliberately searches only its configured projects) — not a defect. Decisive live evidence confirms this: both ❌ chats (**Nexto-LA HW maintenance IOH** = 18 chunks, **IOH-LA-Kyndryl-EID-Dell** = 17 chunks) are stored under **`project-dell-ioh`**, which **IS in Raka's allow-list** — so the scope is already searched; the allow-list is not the blocker. The actual blocker is **RC2**: the FTS index `tsv` is generated from chunk `text` only, so a query naming the project by its **group/chat name** cannot match unless the name's tokens also appear verbatim in the message body. RC2 is now the sole confirmed **code** cause; RC3 (giving each project group its own `graph_id`) becomes an **optional data-organization** improvement (operator-confirmed feasible "as long as the WhatsApp group has its own graph id"). RC1's union-discovery fix is **withdrawn** (would violate operator's explicit scoping intent). Title, summary, FRs, decision log, impl plan updated to reflect RC2-primary. |
| 0.3-draft | 2026-06-18 | **Implemented (code-complete + build/vet-verified + live-verified).** RC2: migration `000090_raw_message_chunks_tsv_chat_name` (DROP+ADD `tsv` = `to_tsvector('simple', coalesce(chat_name,'') \|\| ' ' \|\| coalesce(text,''))`, recreate `idx_rmc_tsv`) + `RequiredSchemaVersion` 88→90 (also pulls pending `000089` user_timezone). Applied to live master DB → schema version 90, not dirty. Live verification: token `nexto` now matches the **Nexto-LA HW maintenance IOH** chat (18 chunks; was 0 before) and a full project-name query retrieves Nexto (18) + IOH-LA-Kyndryl-EID-Dell (17). RC1 observability: result header now always lists searched scopes (`shared_knowledge_search.go:214-227`, dropped the `len(scopes) <= 3` gate; caps at 20 with `+N more`). Regression: `raw_message_chunks` row count unchanged (8176), body-token match still works (`dell`→749), `idx_rmc_tsv` recreated. SQLite chunk store is a no-op stub → no SQLite schema change. **Verification:** `go build ./...` ✓, `go build -tags sqliteonly ./...` ✓, `go vet ./internal/tools/... ./internal/store/... ./internal/upgrade/...` ✓. **Pre-existing unrelated failure:** `TestKGTraversal_Tier1_CappedAt20` (expects KG `Traverse` cap 20; code `maxTraversalResults=30`) — in `executeTraversal`, untouched by this change. **Deferred (operator-run, optional):** RC3 per-group scope remediation via `007`. |
| 0.4-draft | 2026-06-18 | **Closed.** RC2 verified at the retrieval layer: the SQL `tsv @@` predicate used in the live proof IS the exact predicate the tool's `ftsSearch` runs (`internal/store/pg/raw_message_chunks.go:206`), so the live SQL result (Nexto chat 18, Kyndryl 17) is equivalent proof that `shared_knowledge_search` Phase 1 now retrieves them; the RRF merge, result formatting, and LLM invocation above that predicate are unchanged code. RC1 observability shipped. RC3 (per-group `graph_id`) confirmed **optional/deferred** — RC2 already makes the named projects searchable under the existing `project-dell-ioh` scope; RC3 is a separate data-organization task tracked in §6.5 / §13.5, not a closure blocker (same pattern as `010` closing its WS path while leaving an HTTP follow-up tracked). |

---

## 1. Summary (TL;DR)

Agent **Raka** (`shared_knowledge_search`) retrieves some WhatsApp-listen projects but not others when the user searches by **project/group name**. The operator's reported matrix on the master tenant:

```
✅ Channel IOH-Dell
✅ IOH Telco Cloud
✅ INT - IOH - CMI Switch
✅ Channel AI Ran / AI RAN Cluster Deployment

❌ Nexto-LA HW Maintenance IOH
❌ IOH-LA-Kyndryl-EID-Dell
```

**Confirmed root cause (RC2 — code):** the chunk full-text index is a STORED generated column

```sql
tsv tsvector GENERATED ALWAYS AS (to_tsvector('simple', text)) STORED
```

(`migrations/000064_raw_message_chunks.up.sql`, verified via `\d+ raw_message_chunks`) — built from the chunk **`text`** (message body) **only**. The **`chat_name`** — the WhatsApp group/project name the user searches by ("Nexto-LA HW maintenance IOH", "IOH-LA-Kyndryl-EID-Dell") — is **not indexed**. So a query naming the project only matches if those literal tokens also appear in the message bodies.

Live proof that this is the blocker (not scope): both ❌ chats are stored under **`project-dell-ioh`**, which **IS in Raka's `shared_kg_ids` allow-list** — so the scope is searched. The "Nexto-LA HW maintenance IOH" chat has **18 chunks** there, but an FTS match for the token `nexto` returns chunks **only from the sibling chat "Channel IOH-Dell"** — the Nexto chat's chunks do not contain the literal token `nexto` in their body, so a project-name query cannot retrieve them, and they lose the vector top-K race against the 688-chunk `project-dell-ioh` pool. Indexing `chat_name` makes the name match.

**Two framing clarifications (operator-confirmed, v0.2):**

- **RC1 — `shared_kg_ids` allow-list is BY DESIGN, not a defect.** It is intentional operator-controlled scoping: an agent searches only the projects configured for it. Raka's list is the 4 ✅ scopes, and both ❌ chats already live under one of them (`project-dell-ioh`), so the allow-list is not the blocker for the reported case. No code change to the allow-list logic. (An optional observability line — "searched N scopes" — is suggested so an operator can tell a genuine miss from a not-in-allow-list scope, but the scoping behavior is correct.)
- **RC3 — optional data-organization improvement.** Today several distinct project groups (Channel IOH-Dell, Mini-AMC, Nexto-LA, IOH-LA-Kyndryl, Vitus, MII-LA-IOH-VMware-Exit) all share one scope `project-dell-ioh`. Giving each its own `graph_id` is operator-confirmed feasible ("as long as the WhatsApp group has its own graph id") and would make per-project scoping + the embeddings menu cleaner — but it is **not required** to make the named projects searchable; RC2 alone does that. RC3 is remediation via the existing `007` scope-edit tool.

This SRS owns the **RC2 code fix** (FTS index) + the **RC1 observability nudge** + documents the **RC3 optional remediation**. It composes with `003` (wrong graph-id at ingest — the source of the shared-scope folding), `007` (editable `agent_id`/`graph_id` — the RC3 remediation tool), and `008` (which already made the **embeddings menu** filter by `chat_name` substring, but did **not** touch the **retrieval/Search** path that RC2 fixes). It does not change ingest, the consolidation workers, or the 8-stage pipeline structure.

---

## 2. Evidence (live master DB, 2026-06-18)

### 2.1 Raka's configured scope allow-list — both ❌ projects are ALREADY covered

```sql
SELECT agent_key, agent_type, workspace_sharing
FROM agents WHERE agent_key='raka';
```

```
raka | predefined | {"shared_kg_ids": ["project-dell-ioh","project-cmi-switch",
                   "project-ai-ran","project-ioh-telco-cloud"],
                   "share_knowledge_graph": true}
```

→ the 4 ✅ scopes. Crucially, both ❌ chats live under `project-dell-ioh` (in the list):

```sql
SELECT chat_name, count(*) FROM raw_message_chunks
WHERE graph_id='project-dell-ioh' GROUP BY chat_name ORDER BY 2 DESC;
```

```
Channel IOH-Dell              | 621
Mini - AMC DELL IOH           |  26
Nexto-LA HW maintenance IOH   |  18   ← ❌, but under an IN-LIST scope
IOH-LA-Kyndryl-EID-Dell       |  17   ← ❌, but under an IN-LIST scope
Vitus(DELL) AMC Support - IOH |   4
MII - LA - IOH VMware Exit    |   2
```

→ the scope is searched; the allow-list is not the blocker (RC1 is by design).

### 2.2 RC2 smoking gun — `nexto` matches the wrong chat; the named chat's chunks have no literal token

```sql
SELECT chat_name, left(text,80) AS txt
FROM raw_message_chunks
WHERE graph_id='project-dell-ioh'
  AND tsv @@ (SELECT replace(plainto_tsquery('simple','nexto')::text,'&','|')::tsquery)
LIMIT 5;
```

→ **all** matches come from chat **"Channel IOH-Dell"** (message bodies that literally contain "Nexto" / "nex"), **none** from the chat **"Nexto-LA HW maintenance IOH"** (its 18 chunks exist but their bodies do not repeat the token `nexto`). So a project-name query cannot retrieve them, and they are buried among the 688-chunk `project-dell-ioh` pool in the vector leg.

### 2.3 `tsv` definition (RC2 — chat_name is NOT indexed)

```
\d+ raw_message_chunks
tsv | tsvector | generated always as (to_tsvector('simple'::regconfig, text)) stored
... idx_rmc_tsv gin (tsv)
```

Source: `migrations/000064_raw_message_chunks.up.sql` — `tsv` = `to_tsvector('simple', text)`. `chat_name` is a plain column on the row, never fed into `tsv`. (Compare `agents.tsv` and `team_tasks.tsv`, which both coalesce multiple columns into the index — `migrations/000002`, `000004`.)

---

## 3. Code-path trace

`shared_knowledge_search` (`internal/tools/shared_knowledge_search.go`) Phase 1 raw-message search:

```go
scopes := explicitScope or store.SharedKGIDsFromCtx(ctx)   // :98-104  ← allow-list (by design)
if t.chunkStore != nil && len(scopes) > 0 {                 // :127
    for _, scope := range scopes {                          // :141
        opts.GraphID = scope                                // :143
        rmResults, err := t.chunkStore.Search(ctx, q, agentStr, opts)  // :145
        ...
    }
}
```

Chunk `Search` (`internal/store/pg/raw_message_chunks.go:116`): hybrid FTS + vector with RRF fusion.

- `ftsSearch` (`:162`): when `opts.GraphID != ""` scopes by **`graph_id = $scope`** (not `agent_id`); match predicate `tsv @@ plainto_tsquery('simple', query)` with `&`→`|` OR-rewrite (`:202-206`).
- `vectorSearch` (`:231`): same `graph_id = $scope` scoping; `1 - (embedding <=> q)` similarity (`:269-274`).
- `MinScore` filter only when `opts.MinScore > 0` — `shared_knowledge_search` never sets it (`:151`). Not a cause.

Because both legs scope by `graph_id = project-dell-ioh` (in the allow-list), the Nexto/Kyndryl chunks are **in the candidate set** — they simply never match a name query because the name is not in `tsv` and the body lacks the token. This is the RC2 defect.

Chunk `graph_id` provenance (RC3 context): the WhatsApp embedding worker writes `GraphID: graphID` from the `(agent_id, graph_id)` group in `ListPendingEmbeddingGroups` (`internal/channels/whatsapp/embedding_worker.go:181,248-261`), which traces to the group's `listen_graph_id` via `resolveGraphID` (`internal/channels/whatsapp/whatsapp.go:647-690`) — the same value the `003` RC2 defect showed is **not hot-applied** when config is edited directly in the DB. This is why several distinct project groups share one scope today (RC3).

---

## 4. Root cause (confirmed)

| # | Item | Verdict | Action |
|---|---|---|---|
| **RC2** | FTS index `tsv` generated from chunk **`text`** only; **`chat_name`** (the project/group name users search by) is not indexed. | **Defect (code)** — the direct cause of the reported "cannot search Nexto-LA / IOH-LA-Kyndryl-EID-Dell by name." | **Fix:** include `chat_name` in the `tsv` generated column (§5.1). |
| **RC1** | Tool searches only scopes in the agent's `shared_kg_ids` allow-list; the list is hand-maintained. | **By design** — operator-controlled scoping. Both ❌ chats are already under an in-list scope (`project-dell-ioh`), so it is not the blocker. | **No code change** to scoping. Optional observability line (§5.2). |
| **RC3** | Several distinct project groups share one `graph_id` (Nexto/Kyndryl/Mini-AMC/Vitus/MII all under `project-dell-ioh`); typo scope `proect-ai-ran` (18 chunks) splits AI-RAN. | **Data organization** — optional improvement, operator-confirmed feasible with per-group `graph_id`. Not required for searchability (RC2 covers it). | **Optional remediation** via `007` (§5.3). |

---

## 5. Solution (chosen approach)

One option per item; rejected alternatives listed. **Nothing is implemented yet.**

### 5.1 RC2 (`chat_name` unindexed) — CHOSEN: Option A (include `chat_name` in the FTS generated column)

**Build:** change the `tsv` generated column to index `chat_name` + `text`:

```sql
ALTER TABLE raw_message_chunks DROP COLUMN tsv;
ALTER TABLE raw_message_chunks ADD COLUMN tsv tsvector
    GENERATED ALWAYS AS (to_tsvector('simple', COALESCE(chat_name,'') || ' ' || COALESCE(text,''))) STORED;
CREATE INDEX idx_rmc_tsv ON raw_message_chunks USING GIN(tsv);   -- recreate after column drop
```

PostgreSQL cannot `ALTER` a generated-column expression in place, so it is DROP+ADD (the GIN index must be dropped first / recreated). The column is `STORED`, so PG repopulates it for all rows in the same DDL — a one-time rewrite proportional to the table size (acceptable on this tenant's volume; note in §10). Bump `RequiredSchemaVersion` (`internal/upgrade/version.go`) and add the PG migration under `migrations/`. SQLite chunk store is a no-op stub on lite (`internal/store/sqlitestore/raw_message_chunks.go`), so no SQLite schema change is needed and the desktop build is unaffected.

After this, the FTS predicate `tsv @@ plainto_tsquery('simple', 'Nexto-LA HW maintenance IOH')` matches the 18 Nexto-chat chunks via the `chat_name` tokens (`nexto`/`maintenance`/`ioh` — the rare token `nexto` ranks them high against the shared pool). The retrieval `Search` SQL (`raw_message_chunks.go:202-206`) is unchanged; only the index content changes.

**Why chosen:** `chat_name` is the user's actual project identifier. Indexing it is the minimal, root-cause fix that makes "search by project/group name" work — the exact reported symptom. `008` already made the embeddings *menu* filter by `chat_name` substring; RC2 extends the same principle to the *retrieval/Search* path. `coalesce(...||...)` is null-safe; `'simple'` dictionary matches the existing tokenizer choice (consistent with the deferred multi-language-FTS item in `009` §7).

**Rejected:**
- *Append `chat_name` into `text` at chunk-write time.* Pollutes the body shown to the LLM (the result formatter prints `c.Text`, `shared_knowledge_search.go:167`), and would require re-chunking all history. The generated-column change indexes without changing stored `text`.
- *Add a separate `chat_name ILIKE` predicate in `ftsSearch`/`vectorSearch`.* Adds query complexity and only helps FTS (not the scoring blend); the generated column helps FTS uniformly and is the conventional PG pattern (cf. `agents.tsv`, `team_tasks.tsv`).

### 5.2 RC1 (observability nudge, no scoping change) — CHOSEN: print searched scopes

The allow-list logic is correct and unchanged. To help an operator distinguish a **genuine miss** from a **not-in-allow-list scope** (the two are indistinguishable today — both yield "No results"), add a short line to the `shared_knowledge_search` result header showing which scopes were searched:

```text
Raw message search for "<q>" across N scope(s) [project-dell-ioh, project-cmi-switch, ...]:
```

The tool already builds this header (`shared_knowledge_search.go:214-219`) — it currently lists scopes only when `len(scopes) <= 3`. Relax that to always list them (or list up to N) so the operator sees the searched set. No behavior change; observability only.

**Rejected:**
- *Union searched scopes with discovered `DISTINCT graph_id` (v0.1 proposal).* **Withdrawn** — the operator confirmed the allow-list is intentional scoping. Auto-adding discovered scopes would surface projects the operator deliberately excluded, violating their intent. The allow-list is the source of truth for which projects an agent may search.

### 5.3 RC3 (optional data organization) — CHOSEN: Option C (per-group `graph_id` via `007`, operator-run, sequenced)

**Build (optional, after 5.1 is live — and only if the operator wants per-project scopes):**

1. Back up `raw_message_chunks` + `listen_raw_messages` (+ affected KG `entities`/`relations`) for the in-scope scopes.
2. Assign each project group its **own `listen_graph_id`** (e.g. `project-nexto-la`, `project-ioh-la-kyndryl`) in the channel group config, then use the `007` scope-edit (`POST /v1/listen-raw-messages/scope`) to move the relevant `listen_raw_messages` rows out of `project-dell-ioh` into the new dedicated `graph_id`. `007` re-extracts + re-embeds (true-move, `007` FR-08) under the new scope.
3. Add each new scope to Raka's `shared_kg_ids` allow-list (operator config — required, since the allow-list is authoritative per RC1).
4. **Merge the typo scope:** re-key the 18 `proect-ai-ran` rows → `project-ai-ran` (same `007` mechanism) so AI-RAN is a single scope.
5. Re-verify via the §6 / §7 queries.

**Why optional:** RC2 alone makes Nexto/Kyndryl searchable by name (they stay under `project-dell-ioh`, which is in the allow-list). RC3 is only worth doing if the operator wants the embeddings menu + scoping to reflect one-scope-per-project (cleaner organization, narrower per-search pools → better recall precision). The operator confirmed this is feasible "as long as the WhatsApp group has its own graph id."

**Guard:** run **only after** RC2 is verified, against a backup, target scope confirmed per chat. Do NOT run before RC2 — otherwise the operator cannot tell whether a continued miss is the indexing bug or the remediation.

---

## 6. Acceptance Criteria (Definition of Done)

A fix is accepted **only when every box below is checked**. §7 describes *how* to exercise them.

### 6.1 Functional — RC2: project/group name is searchable

- [x] After the `tsv` migration, a query "Nexto-LA HW maintenance IOH" retrieves the 18 chunks of that chat (via the indexed `chat_name`), proven by an FTS match that is empty before the migration. _(live-verified: token `nexto` now matches the Nexto chat 18 chunks — was 0 before; `tsv @@ phraseto_tsquery('simple','Nexto-LA HW maintenance IOH')` returns 18)_
- [x] A query "IOH-LA-Kyndryl-EID-Dell" retrieves its 17 chunks by group name. _(live-verified: `tsv @@ to_tsquery('simple','kyndryl')` returns the IOH-LA-Kyndryl-EID-Dell chat 17 chunks)_
- [x] Existing body-text matches are unchanged (no regression in `text`-only retrieval). _(live-verified: body token `dell` still matches 749 chunks; `tsv` is additive — chat_name prepended, `text` leg intact)_
- [x] The migration rewrites the `tsv` column for all rows and recreates `idx_rmc_tsv`. _(live: `generation_expression` = `to_tsvector('simple', coalesce(chat_name,'') || ' ' || coalesce(text,''))`; `idx_rmc_tsv` present; row count unchanged 8176)_

### 6.2 Functional — RC1: observability (no scoping change)

- [x] The `shared_knowledge_search` result header lists the scopes actually searched (not only when ≤3), so an operator can tell a genuine miss from a not-in-allow-list scope. _(implemented `shared_knowledge_search.go:214-227`: always lists scopes, caps at 20 with `+N more`)_
- [x] The allow-list scoping behavior is unchanged: an agent still searches only its `shared_kg_ids` (explicit `scope` override aside). _(by inspection — scope resolution `:98-104` untouched)_

### 6.3 No regressions

- [x] `tsv` index change does not alter the stored `text` column or the LLM-visible chunk text (the result formatter prints `c.Text`, unchanged). _(by inspection — only the generated `tsv` column changed; `text` and the formatter `:167` untouched)_
- [x] Tenant isolation holds: every `Search` is tenant-scoped (`scopeClause`); the index change adds no new data exposure. _(by inspection — `ftsSearch`/`vectorSearch` `scopeClause` unchanged)_
- [x] Agent scoping holds: `graph_id = $scope` scoping unchanged. _(by inspection — `primaryCol`/`primaryVal` logic unchanged)_
- [x] Explicit `scope` param still overrides; `entity_id` drill-down path unchanged. _(by inspection — `:98-104`, `:111-120` untouched)_
- [x] Desktop/lite (`sqliteonly`) build is green (SQLite chunk store is a stub — no schema change required there). _(`go build -tags sqliteonly ./...` ✓)_

### 6.4 Build, test & safety

- [x] `go build ./...` passes.
- [x] `go build -tags sqliteonly ./...` passes.
- [x] `go vet ./internal/tools/... ./internal/store/... ./internal/upgrade/...` is clean.
- [x] Migration applied + live-verified on the master DB (name query matches rows it did not before; body-token matches unchanged; row count stable). _(stronger than a container unit test — verified against real tenant data)_
- [x] No view/index depended on the old `tsv` expression; `idx_rmc_tsv` recreated cleanly (migration applied non-dirty). _(the only dependent was `idx_rmc_tsv`, recreated; `migrate up` → version 90 dirty=false)_

### 6.5 Operator / docs

- [ ] **RC3 (optional, deferred — not a closure blocker):** if per-group scope organization is later wanted, run it **only after** RC2 is live (✓), against a **backup**, target scope confirmed per chat, each new scope added to the agent's `shared_kg_ids` (RC1 allow-list is authoritative). RC2 already makes the named projects searchable without it.

---

## 7. Verification plan (run after any fix, before declaring done)

1. **Build:** `go build ./...` and `go build -tags sqliteonly ./...`.
2. **Reproduce gate (before):** FTS name query for the Nexto chat returns 0 of its own chunks (RC2 proven); confirm both ❌ chats are under the in-list `project-dell-ioh` scope (RC1 is not the blocker).
3. **Apply fix** (RC2 migration; RC1 observability header).
4. **Reproduce gate (after):** a name query "Nexto-LA HW maintenance IOH" returns the 18 Nexto-chat chunks; "IOH-LA-Kyndryl-EID-Dell" returns its 17.
5. **Regression sweep:** confirm body-token retrieval unchanged; confirm tenant + agent scoping; confirm explicit-scope + drill-down paths intact; confirm the result header lists searched scopes.
6. **RC3 (optional remediation):** assign per-group `graph_id` + move via `007` + add to `shared_kg_ids`; merge `proect-ai-ran` → `project-ai-ran`; re-verify:
   ```sql
   SELECT graph_id, count(*) FROM raw_message_chunks
   WHERE graph_id IN ('project-ai-ran','proect-ai-ran','project-nexto-la','project-ioh-la-kyndryl')
   GROUP BY 1;
   ```

---

## 8. Open questions / disambiguation

- **Should `chat_id` also be folded into `tsv` (RC2)?** Optional. `chat_name` covers the user-facing project identifier; `chat_id` is a raw WhatsApp JID rarely typed by users. Recommend `chat_name` only for v1; add `chat_id` if operators search by JID.
- **`tsv` rewrite cost on the STORED column.** Proportional to `raw_message_chunks` size (this tenant ≈ 8k rows — seconds). For much larger tenants, run during a maintenance window. Non-blocking; note in §10.
- **Should RC3 (per-group scope) be done at all?** Operator decision. RC2 makes the named projects searchable without it. RC3 only improves organization / per-search precision. If done, each new scope must be added to `shared_kg_ids` (RC1 is authoritative).
- **Confirm no DB view/index depends on the old `tsv`.** Check `pg_depend` / `\dv` before the DROP+ADD. The only known dependent is `idx_rmc_tsv` (recreated). TBD at impl time.

---

## 9. Key files

- `internal/tools/shared_knowledge_search.go:79-222` — `Execute` Phase 1 scope iteration + chunk search; result header `:214-219` (RC1 observability)
- `internal/store/pg/raw_message_chunks.go:116-296` — `Search` / `ftsSearch` / `vectorSearch` (graph_id scoping + tsv predicate; unchanged by RC2)
- `migrations/000064_raw_message_chunks.up.sql` — `tsv` generated-column definition (RC2 migration target)
- `internal/upgrade/version.go` — `RequiredSchemaVersion` (bump for RC2 migration)
- `internal/channels/whatsapp/embedding_worker.go:181,248-261` — chunk `GraphID` provenance (RC3 source)
- `internal/channels/whatsapp/whatsapp.go:647-690` — `resolveGraphID` (003 RC2; RC3 origin)
- `internal/store/agent_store.go:354-368,448-458` — `WorkspaceSharingConfig.SharedKGIDs` (RC1 allow-list, unchanged)

---

## 10. Risks and Open Questions

| Risk or question | Draft decision |
|---|---|
| `tsv` STORED-column rewrite rewrites the whole table once. | One-time, proportional to table size; seconds on this tenant. Schedule for a maintenance window on very large tenants. No data loss (generated column). |
| DROP+ADD `tsv` could break a dependent view/index. | Only known dependent is `idx_rmc_tsv` (recreated). Check `pg_depend` / `\dv` before applying. |
| FTS still uses `plainto_tsquery('simple')` — Vietnamese/Chinese content under-served on the text leg (same as `009` §7). | Out of scope here (recall-quality, deferred to a multi-language-FTS SRS). The `chat_name` index + vector leg cover the reported English/mixed project names. |
| Indexing `chat_name` increases the common-token pool (e.g. "ioh"/"dell" now match all dell-ioh chunks via chat_name too), lowering ts_rank for those tokens. | Acceptable — the rare, discriminative tokens (`nexto`, `kyndryl`) still rank their chunks high. The OR-rewrite FTS (`&`→`|`) already tolerates common tokens. |
| RC3 remediation moves data between scopes (non-transactional true-move per `007` FR-08). | Run only after RC2 verified, against a backup, per chat. KG entities under the old scope are NOT auto-moved (`003` §5.3 / `007` §7) — separate cleanup. |
| `proect-ai-ran` typo scope merge is destructive (re-keys 18 rows). | Use `007` scope-edit (re-extract + true-move). Confirm no consumer reads the typo literally (none do — it is a data-entry mistake). |

---

## 11. Implementation Plan

1. **RC2 (FTS `chat_name`) — backend, the fix:** add PG migration DROP+ADD `tsv` (`to_tsvector('simple', coalesce(chat_name,'')||' '||coalesce(text,''))`), recreate `idx_rmc_tsv`; bump `RequiredSchemaVersion` (`internal/upgrade/version.go`). No SQLite change (stub). Verify name-query match (§7).
2. **RC1 (observability) — small frontend-of-tool edit:** in `shared_knowledge_search.go:214-219`, always list the searched scopes in the result header (drop the `len(scopes) <= 3` gate). No scoping change.
3. **Tests:** migration name-query match (chat_name token hits rows it missed before); body-token regression (unchanged); header lists scopes.
4. **Regression:** tenant + agent isolation; explicit-scope + drill-down; desktop/lite build.
5. **RC3 (data remediation, OPTIONAL, after 1–2 live):** assign per-group `graph_id` + move via `007` + add to `shared_kg_ids`; merge `proect-ai-ran`, against a backup.
6. **Checklist:** `go build ./...`, `go build -tags sqliteonly ./...`, `go vet ./...`, `go test` (store/tools), `pnpm build` (ui/web — no UI change expected).

---

## 12. Proposed Error Codes

No new canonical error codes. Recall is best-effort by design; a miss returns "No results found" (`shared_knowledge_search.go:210-211`), and the migration/observability change is internal. For traceability only:

| Code (proposed canonical mapping) | Meaning |
|------|---------|
| `memory.recall_unavailable` | The chunk store / embedding provider is nil at recall time (silent empty today). If recall health is surfaced later, map this state. |
| `request.validation_failed` | Malformed explicit `scope` (free-form; an unmatched scope simply yields 0 rows, as today). No change. |

No error code is registered by this bugfix; recall failure remains a silent no-op matching today's behavior, with the added observability line (§5.2) so operators can distinguish a genuine miss from a not-in-allow-list scope.

---

## 13. Implementation notes (v0.3) — what was built + verified

Implemented and build/vet-verified; RC2 migration applied + live-verified on the master tenant (agent Raka). RC3 remains an optional operator-run remediation.

### 13.1 RC2 — `chat_name` in the FTS index (files)

- `migrations/000090_raw_message_chunks_tsv_chat_name.up.sql` (new) — `DROP INDEX idx_rmc_tsv; DROP COLUMN tsv; ADD COLUMN tsv GENERATED ALWAYS AS (to_tsvector('simple', coalesce(chat_name,'') || ' ' || coalesce(text,''))) STORED; CREATE INDEX idx_rmc_tsv USING GIN(tsv)`. PG cannot ALTER a generated-column expression, hence DROP+ADD; the STORED column is repopulated for all rows during ADD.
- `migrations/000090_raw_message_chunks_tsv_chat_name.down.sql` (new) — reverts `tsv` to `to_tsvector('simple', text)`.
- `internal/upgrade/version.go` — `RequiredSchemaVersion` 88 → 90 (requires `000090`; also pulls the pending `000089` user_timezone).
- SQLite: **no change** — `SQLiteRawMessageChunkStore` is a no-op stub (`internal/store/sqlitestore/raw_message_chunks.go`, all methods return nil) and the SQLite `schema.sql` has no `tsv` column for this table. Honors the dual-DB rule (nothing to mirror on a stub).

### 13.2 RC1 — observability header (files)

- `internal/tools/shared_knowledge_search.go:214-227` — result header now **always** lists the searched scopes (dropped the `len(scopes) <= 3` gate); caps at 20 with a `+N more` suffix so a 16-scope agent header stays readable. Scope resolution (`:98-104`) is untouched — the allow-list scoping is unchanged (by design).

### 13.3 Live verification (master DB, 2026-06-18)

`./goclaw migrate up` → `version=90 dirty=false`. Then:

| Check | Before | After |
|---|---|---|
| `tsv` generation expression | `to_tsvector('simple', text)` | `to_tsvector('simple', coalesce(chat_name,'') \|\| ' ' \|\| coalesce(text,''))` |
| token `nexto` FTS, `project-dell-ioh` | only "Channel IOH-Dell" | **"Nexto-LA HW maintenance IOH" 18** + "Channel IOH-Dell" 99 + "Mini - AMC DELL IOH" 1 |
| name query "Nexto-LA HW maintenance IOH" | 0 rows | **18** (Nexto chat) |
| name query IOH-LA-Kyndryl (`kyndryl`) | 0 rows | **17** (IOH-LA-Kyndryl-EID-Dell chat) |
| body token `dell` (regression) | matches | matches (749) |
| `raw_message_chunks` row count | 8176 | 8176 (unchanged) |
| `idx_rmc_tsv` | present | recreated, present |

### 13.4 Verification commands

`go build ./...` ✓, `go build -tags sqliteonly ./...` ✓, `go vet ./internal/tools/... ./internal/store/... ./internal/upgrade/...` ✓, `go test ./internal/tools/...` 948 pass.

**Pre-existing unrelated failure:** `TestKGTraversal_Tier1_CappedAt20` (`internal/tools/knowledge_graph_test.go:391`) expects the KG `Traverse` cap at 20, but `executeTraversal` uses `const maxTraversalResults = 30` (`shared_knowledge_search.go:281`). That code path is untouched by this change (the edit is in the Phase-1 raw-message header, not `executeTraversal`). Fails identically on the clean tree.

### 13.5 Still pending

- **RC3 (optional, operator-run):** give each project group its own `listen_graph_id` + move via `007` scope-edit + add to `shared_kg_ids`; merge the typo scope `proect-ai-ran` → `project-ai-ran`. Only if the operator wants one-scope-per-project organization. Not required for searchability — RC2 already makes Nexto/Kyndryl searchable by name under the existing `project-dell-ioh` scope.
- **Other shared agents:** the RC2 fix applies tenant-wide (the `tsv` column is global), so every agent's `shared_knowledge_search` benefits automatically. No per-agent action.
