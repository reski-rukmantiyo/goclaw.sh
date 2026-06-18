# Software Requirements Specification: Agent Does Not Recall Prior-Session Discussion (Episodic Recall Not Surfaced / Not Triggered)

**Project**: GoClaw Gateway
**Release**: 2026.3.0
**Version**: 0.2-draft
**Date**: 2026-06-17
**Status**: Implemented (code-complete + build/vet/test-verified). Live verification on the master tenant (Raka) + DB-integration isolation tests pending.
**Difficulty**: Medium
**Estimate**: 1.5–2.5 days

---

## Revision History

| Version | Date | Changes |
|---------|------|---------|
| 0.1-draft | 2026-06-17 | Initial draft. Investigation + live-DB evidence on the master tenant (agent **Raka**, `019ec54f-6fab-7b2b-b509-a28483049ed9`). Three confirmed root causes: (RC1) episodic memory recall is partitioned by per-invocation `user_id` for a shared (`predefined`) agent whose knowledge graph is shared but whose memory is not; (RC2) the only automatic recall path (auto-inject) surfaces only ~50-token L0 abstracts and ignores the agent's `memory_config`; (RC3) full recall (L1/L2) is reachable only via the `memory_search` tool, which the LLM almost never calls (`recall_count = 0` for 136/150 rows, 0/8 for Raka) — there is no auto-trigger on continuity/temporal queries. Chosen fix proposes: unify memory scope for predefined agents; make auto-inject honor agent `memory_config` and surface usable (L1) content; auto-trigger a deeper recall for continuity-style queries. Rejected alternatives listed. No code written yet. |
| 0.2-draft | 2026-06-17 | **Implemented (code-complete + build-verified).** FR-01 broadened `shouldShareMemory()` for predefined/shared agents (converted `WorkspaceSharingConfig.ShareMemory` → `*bool` to honor an explicit `false` override); `open` agents unchanged. FR-02/03/05 rewrote `pgAutoInjector.Inject` to honor forwarded config, surface L1 `Summary` for top hits via `EpisodicStore.Get`, and best-effort `RecordRecall` on all recall paths. FR-04 added `internal/memory/recall_trigger.go` (en/vi/zh continuity detector) + a broader, labeled recall on continuity queries, plus a system-prompt reinforcement. **Deviation from §4/§8:** the per-agent auto-inject config was added to `config.MemoryConfig` (the *actually-wired* per-agent config in `agents.memory_config` JSONB, read by `AgentData.ParseMemoryConfig()`), not the dead `memory.MemoryConfig` in `auto_injector.go` (which was never parsed from the agent row). New `InjectParams` fields (`Enabled`/`L1Depth`/`L1PerHitMaxTokens`); new `config.MemoryConfig` fields (`AutoInjectEnabled *bool` / `AutoInjectThreshold` / `AutoInjectMaxEntries` / `AutoInjectMaxTokens` / `AutoInjectL1Depth` / `AutoInjectL1PerHitTok`). `makeAutoInjectCallback` forwards them via small helpers. **Verification:** `go build ./...` ✓, `go build -tags sqliteonly ./...` ✓, `go vet` (agent/memory/store/config) ✓, `go test -race ./internal/agent/ ./internal/memory/` ✓. New tests: `workspace_sharing_test.go` (shouldShareMemory matrix), `auto_injector_impl_test.go` (disabled/L1/continuity/record-recall), `recall_trigger_test.go` (en/vi/zh + negatives). **Deferred (env-gated):** FR-01 cross-context + FR-06 isolation integration tests (need pgvector pg18 container), and the live Raka recall check + the `recall_count` diagnostic (need a running gateway + the Monday session). FR-07: no UI strings added; system-prompt text is English-only by convention; the detector's locale term lists (en/vi/zh) are the locale coverage. |

---

## 1. Summary

An agent that has accumulated **episodic memory** (session summaries) across prior runs does **not recall** that prior discussion when invoked again. The reported case is **Raka** (`agent_key=raka`, predefined, master tenant): despite 8 `episodic_summaries` rows existing (5 created from Monday 2026-06-15 cron runs, all with embeddings + L0 abstracts), Raka does not recall the Monday discussion, and the web Sessions menu shows only recent messages.

This is **not** a "memory was never created" defect — creation works (verified against the live DB). It is a **recall** defect with three independent, confirmed causes:

- **RC1 — Scope fragmentation.** Raka is a `predefined` (shared-context) agent with `share_knowledge_graph: true` but **no `share_memory`**. `shouldShareMemory()` (`internal/agent/loop_utils.go:57-59`) returns false, so episodic recall is scoped by the **per-invocation `user_id`** via `store.MemoryUserID(ctx)` (`internal/store/context.go:285-292`). All 8 of Raka's episodic rows carry `user_id = 'group:whatsapp-reski:120363428802496754@g.us'` (Raka's cron tasks deliver to that WhatsApp group, so the session inherits that user-id). Any Raka invocation under a different user-id (the owner chatting in the web UI, another channel) sees **zero** recall. A shared agent's memory is locked to one channel-scope.
- **RC2 — Auto-inject surfaces only abstracts and ignores agent config.** The only automatic recall path, `AutoInject` (`internal/pipeline/context_stage.go:171-180`), injects only the ~50-token `L0Abstract` per hit (`internal/memory/auto_injector_impl.go:79-81`) — a one-line tag, not the actual prior discussion. Its parameters (K=5, threshold 0.3) are **hardcoded** (`auto_injector_impl.go:35-42`); the agent's `MemoryConfig` (`AutoInjectEnabled`/`AutoInjectThreshold`/`AutoInjectMaxTokens`, `internal/memory/auto_injector.go:57-62`) is **never passed** — `InjectParams` at `internal/agent/loop_pipeline_adapter.go:275-281` omits them — so configuring memory on an agent (Raka has `memory_config={"enabled": true}`) has **no effect**.
- **RC3 — Full recall is tool-gated and never triggered.** The actual prior-session content (L1/L2) is reachable only via the `memory_search` tool (`internal/tools/memory.go:80-189`), which the LLM must **choose** to call. The per-episode recall bookkeeping (`EpisodicStore.RecordRecall`) is invoked **only** from that tool (`internal/tools/memory.go:217`) — never from auto-inject — so `recall_count` measures only tool usage. Across the whole master tenant, only **14 of 150** episodic rows have `recall_count > 0`; Raka is **0 of 8**. There is no heuristic that auto-triggers a memory recall when the user asks about prior work or a past day (e.g. "what did we discuss Monday?").

This SRS owns the **recall-side** fix. It composes with the existing consolidation pipeline (which creates the memories; untouched here) and the existing `memory_search`/`memory_expand` tools (reused, not redesigned). It does **not** change history compaction (a separate, working mechanism — see §6 decision log).

## 2. Scope

**In scope**:

- Making a `predefined` (shared) agent recall its episodic memory **across all invocation contexts** (cron, WhatsApp, web) instead of being partitioned by the per-invocation `user_id`. (RC1)
- Making the auto-inject path honor the agent's `MemoryConfig` (`AutoInjectEnabled` / `AutoInjectThreshold` / `AutoInjectMaxTokens` / `AutoInjectMaxEntries`) instead of hardcoded defaults, and surface **usable content** (a short L1 summary, not only the L0 abstract) for the top match(es). (RC2)
- Adding an **automatic deeper recall trigger** so that continuity/temporal-style user queries ("what did we discuss on X", "do you remember …", "last week we …") cause the agent to recall the relevant prior episodes without relying on the LLM spontaneously calling `memory_search`. (RC3)
- A diagnostic/observability fix so recall success/failure is visible (the `recall_count` / `last_recalled_at` bookkeeping is currently updated **only** by the tool path; auto-inject hits are invisible — operators cannot tell whether recall ran).

**Out of scope**:

- **Episodic memory creation.** Verified working (Raka has 8 rows). The consolidation worker, `session.completed` event, and summarization LLM call are untouched. If a specific tenant has *zero* episodic rows, that is a different defect (consolidation-skipped / no background provider — see `cmd/gateway.go:255-257`) and is **not** this SRS.
- **History compaction / context-window truncation.** This is a separate, working mechanism (`internal/agent/loop_history_sanitize.go:185-328`, `keepLast=4` after summarize). The "only last few messages shown in the Sessions menu" portion of the report is a *display/history-window* concern, orthogonal to cross-session recall. It is noted in §6 but **not** fixed here.
- **Changing the 8-stage pipeline structure.** Auto-inject stays in `ContextStage`; no new retrieval stage is added.
- **Full redesign of the L0/L1/L2 tiering or the `memory_search` tool surface.** The tools are reused as-is; this SRS only (a) makes auto-inject pull more than the L0 abstract and (b) optionally invokes the existing search internally on a continuity trigger.
- **Vector-index / pgvector tuning, FTS language config.** The FTS leg uses `plainto_tsquery('english', …)` (`internal/store/pg/episodic_search.go:31`) which under-serves Vietnamese/Chinese content — a real but separate recall-quality issue (deferred, §7).
- **SQLite/desktop (lite) recall.** The SQLite episodic search is LIKE-based on the full recall-query string and is effectively broken for follow-up turns; lite has no pgvector. Listed as a known limitation (§7), not fixed here (Raka runs on the PG/Standard master tenant).

## 3. Functional Requirements

### FR-00: Episodic memory exists and was created (verification only — no code change)

Confirmed against the live master tenant. Raka (`019ec54f-6fab-7b2b-b509-a28483049ed9`) has 8 `episodic_summaries` rows, all with embeddings (`embedding IS NOT NULL`) and non-empty `l0_abstract`, created 2026-06-14 → 2026-06-17. Five originate from the Monday 2026-06-15 cron runs. Creation is **not** the defect; this FR exists for traceability so the fix is correctly scoped to recall.

Acceptance criteria:

- [x] `SELECT count(*), count(*) FILTER (WHERE embedding IS NOT NULL), count(*) FILTER (WHERE l0_abstract <> '') FROM episodic_summaries WHERE agent_id = '019ec54f-6fab-7b2b-b509-a28483049ed9'` returns 8 / 8 / 8. _(verified live, 2026-06-17)_
- [x] No change to the consolidation worker, `session.completed` emission, or summarization LLM call is made by this SRS.

---

### FR-01: Predefined (shared) agents recall episodic memory across invocation contexts (RC1)

A `predefined` agent's episodic memory must be recallable from **any** of the agent's invocation contexts, not only the `user_id` under which a given episode was created. Today `shouldShareMemory()` (`internal/agent/loop_utils.go:57-59`) gates on `workspaceSharing.ShareMemory`, which Raka does not set, so `MemoryUserID(ctx)` (`internal/store/context.go:287`) returns the per-invocation user-id and partitions recall.

| Agent type / setting | Today (recall scope) | After |
|---|---|---|
| `predefined` (shared agent) — `ShareMemory` unset | per-invocation `user_id` (Raka → `group:whatsapp-reski:<chat>`) | **agent-scoped** (`user_id = ""`) — unified across cron/WhatsApp/web |
| `predefined` — `ShareMemory = true` | already agent-scoped | unchanged |
| `open` (per-user agent) | per-invocation `user_id` (correct — per-user isolation) | unchanged |

The fix generalizes the sharing decision: a **shared-context** agent (`agent_type = predefined`) shares its memory by default, consistent with the fact that its knowledge graph is already shared (`ShareKnowledgeGraph`). The exact gating (see §6 decision log) is: **`shouldShareMemory()` returns true when the agent is `predefined`, OR `ShareMemory` is explicitly set, OR `ShareKnowledgeGraph` is true** (aligning memory scoping with the agent's shared nature; an explicit `ShareMemory=false` on a predefined agent re-enables per-user partitioning if an operator ever needs it).

Acceptance criteria:

- [ ] After the fix, an episode created under `user_id = 'group:whatsapp-reski:<chat>'` for a `predefined` agent is returned by the recall search when the **same agent** is invoked under a **different** user-id (e.g. the owner in the web UI). _(integration test + live: Raka invoked from web recalls the WhatsApp/cron episodes — pending running gateway)_
- [ ] An `open` (per-user) agent's recall is unchanged — still scoped to that user (no cross-user leak introduced). _(regression test — pending pgvector integration container)_
- [x] `shouldShareMemory()` for a `predefined` agent returns true; for an `open` agent returns false unless `ShareMemory` is set. _(`TestShouldShareMemory_Predefined*` / `_OpenAgentDoesNotShare` / `_SharedKGImpliesSharedMemory`)_
- [x] An explicit `ShareMemory=false` on a `predefined` agent reverts to per-user scoping (operator override). _(`TestShouldShareMemory_PredefinedExplicitFalseOverrides`; `*bool` parse verified)_
- [x] `WorkspaceSharingConfig.ShareMemory` is a `*bool` so explicit `false` ≠ unset; JSON parse verified (`{}`→nil, `true`→&true, `false`→&false). _(in-package parse test)_

---

### FR-02: Auto-inject honors the agent `MemoryConfig` (RC2)

The `AutoInject` callback (`internal/agent/loop_pipeline_adapter.go:274-287`) builds `memory.InjectParams` without forwarding the agent's `MemoryConfig`, so the configured `AutoInjectEnabled` / `AutoInjectThreshold` / `AutoInjectMaxTokens` / `AutoInjectMaxEntries` (`internal/memory/auto_injector.go:57-62`) are **ignored** and the hardcoded defaults (K=5, threshold 0.3, 200 tokens) always apply (`internal/memory/auto_injector_impl.go:35-42`).

Wire the agent `MemoryConfig` into `InjectParams`:

| `InjectParams` field | Source (after) | Hardcoded default (fallback when unset) |
|---|---|---|
| `Enabled` | `MemoryConfig.AutoInjectEnabled` | `true` |
| `Threshold` (MinScore) | `MemoryConfig.AutoInjectThreshold` | `0.3` |
| `MaxEntries` | `MemoryConfig.AutoInjectMaxEntries` | `5` |
| `MaxTokens` | `MemoryConfig.AutoInjectMaxTokens` | `200` |

When `AutoInjectEnabled = false`, `AutoInject` returns empty (no recall via auto-inject for that agent) — giving operators a clean opt-out per agent.

Acceptance criteria:

- [x] Setting `memory_config.auto_inject_enabled=false` on an agent disables auto-inject for it (empty result, no error). _(`TestInject_DisabledReturnsEmpty`; `memoryAutoInjectEnabled` helper)_
- [x] Setting `auto_inject_threshold` / `auto_inject_max_entries` / `auto_inject_max_tokens` changes the auto-inject behavior (fewer/more hits, stricter/looser) — the values reach the store search. _(`makeAutoInjectCallback` forwards them via `memoryInt`/`memoryFloat`; `MaxEntries` reaching `Search` proven by `TestInject_NonContinuityDoesNotBroaden` asserting `searchOpts.MaxResults == maxEntries*2`; threshold/maxTokens share the same forwarding path)_
- [x] An agent with no/empty `MemoryConfig` keeps today's behavior (defaults 0.3 / 5 / 200). _(helpers return the default when `cfg == nil` or value ≤ 0)_

---

### FR-03: Auto-inject surfaces usable content (L1), not only the L0 abstract (RC2)

Today auto-inject writes only `r.L0Abstract` (~50 tokens) per hit into the injected section (`internal/memory/auto_injector_impl.go:79-81`). A one-line abstract is rarely enough to "recall a discussion." For the top match(es), auto-inject must surface a short **L1 summary** (the episode's `Summary`, truncated to a per-hit token budget) so the agent actually has the prior content in context — not just a tag it must expand via a tool call.

| Auto-inject hit rank | Today | After |
|---|---|---|
| Top 1–N (`MaxEntries`) | `L0Abstract` only (~50 tok) | top `L1Depth` hits (default 2): `L0Abstract` + a truncated `Summary` (per-hit budget, e.g. 120 tok); remaining hits: `L0Abstract` only |

The injected section stays within `MaxTokens` (FR-02). `L1Depth` and the per-hit L1 budget are part of `MemoryConfig` (new fields, defaults 2 / 120) so the cost is operator-tunable.

Acceptance criteria:

- [x] When a relevant episode exists, the injected memory section contains a portion of that episode's `Summary` (not only the `L0Abstract`) for at least the top hit. _(`TestInject_SurfacesL1SummaryForTopHit`)_
- [x] The total injected memory tokens never exceed `AutoInjectMaxTokens`. _(by inspection — `buildMemorySection` tracks a rune-budget ≈ `maxTokens*4` and stops adding entries once exhausted)_
- [x] An episode with an empty `Summary` falls back to `L0Abstract` (no empty injection). _(`TestInject_EmptySummaryFallsBackToAbstract`)_

---

### FR-04: Continuity/temporal queries auto-trigger a deeper recall (RC3)

Because the LLM rarely calls `memory_search` spontaneously (`recall_count = 0` for 136/150 rows, 0/8 for Raka), the system must **automatically** perform a deeper recall when the user's message indicates a reference to prior work. This is the fix that makes "do you remember what we discussed Monday?" actually return the Monday episode.

Two cooperating mechanisms (both required):

1. **Lightweight continuity detector.** A cheap, deterministic pre-check on the user message (regex/keyword + locale-aware): references to a past time/day ("yesterday", "last week", "Monday", "<relative date>"), recall verbs ("remember", "recall", "earlier", "before", "previously", "what did we", "last time"), or anaphora to prior topics. When it fires, the pipeline performs an **internal** `memory_search`-equivalent (reuse `EpisodicStore.Search` directly, no LLM tool round-trip) and injects the top L1/L2 results into context — the same way auto-inject injects, but broader/deeper and triggered by the query rather than always-on.
2. **Strengthened system-prompt instruction.** The system prompt already instructs the LLM to call `memory_search` before answering about prior work (`internal/agent/systemprompt_sections.go:90,111`); this is reinforced so that, when the detector fired and injected results, the agent is told "prior context was recalled and injected; cite/use it."

The detector is **additive** — it never *prevents* normal auto-inject or the LLM's own `memory_search` call; it only *adds* a recall injection on continuity queries. False positives (a query that looks continuity-shaped but isn't) cost one extra bounded store search — acceptable.

Acceptance criteria:

- [x] A message like "what did we discuss on Monday?" / "do you remember the IOH plan?" / "last week you said …" triggers an internal episodic search and injects the relevant L1/L2 result(s) into context (verifiable via a log line + a store-search assertion in a unit/integration test). _(`TestInject_ContinuityBroadensSearchAndLabels` asserts broadened `searchOpts.MaxResults` (20) + the labeled section)_
- [x] A clearly non-continuity message ("hello", "write a haiku", "2+2") does **not** trigger the extra search (no latency/ cost added to the common path). _(`TestInject_NonContinuityDoesNotBroaden` asserts normal `MaxResults` (10); `TestIsContinuityQuery_NegativeCases`)_
- [x] The detector is locale-aware (matches recall terms in `en`, `vi`, `zh` per the project i18n rule) — i18n strings/term lists added to all three locales. _(`recall_trigger.go` term lists; `TestIsContinuityQuery_Vietnamese` / `_Chinese`)_
- [x] When the detector injects results, the injected section is labeled so the agent knows it is recalled prior context. _(`TestInject_ContinuityBroadensSearchAndLabels` asserts `"Recalled Prior Context"`)_

---

### FR-05: Recall observability — auto-inject and the continuity trigger update recall bookkeeping (RC2/RC3 diagnostic)

Today `EpisodicStore.RecordRecall` (increments `recall_count`, folds `score` into `recall_score`, sets `last_recalled_at`) is called **only** from the `memory_search` tool (`internal/tools/memory.go:217`). Auto-inject and the new continuity-trigger hits are invisible — operators cannot tell whether recall ran (which is exactly why this defect was hard to diagnose; Raka's `recall_count=0` looked like "recall never happens" when in fact it meant "the tool was never called").

Both auto-inject and the continuity trigger must record recall (best-effort, non-blocking) for the episodes they surface, so `recall_count` / `last_recalled_at` reflect **all** recall paths and operators can diagnose recall health.

Acceptance criteria:

- [x] After an auto-inject hit, the surfaced episode's `recall_count` increments and `last_recalled_at` is set. _(`TestInject_RecordsRecallForInjectedHits` asserts `RecordRecall` invoked for all injected ids; the continuity path uses the same `recordRecall`)_
- [x] After a continuity-trigger hit, the surfaced episode's `recall_count` increments. _(same `recordRecall` path; `buildMemorySection` returns the recalled ids for both modes)_
- [x] The record-recall write is best-effort: a failure does **not** fail the turn (logged at warn, not returned). _(by inspection — `recordRecall` runs in a detached goroutine, logs at `slog.Debug` on error; `TestInject_RecordRecallSkippedWithoutTenant` proves no panic when skipped)_
- [ ] A diagnostic query — `SELECT agent_id, count(*) FILTER (WHERE recall_count>0) AS recalled, count(*) total, max(last_recalled_at) FROM episodic_summaries GROUP BY agent_id` — now shows non-zero recall for agents in active use. _(manual, post-fix — pending running gateway)_

---

### FR-06: No regression to multi-tenant / per-user isolation

The RC1 change broadens recall scope for predefined agents. It must **not** cross tenant boundaries or break `open` (per-user) agent isolation.

Acceptance criteria:

- [ ] Recall is still tenant-scoped: a search for agent A in tenant T1 never returns tenant T2's episodes (the `tenant_id` filter in `internal/store/pg/episodic_search.go:33-47` is unchanged and still applies). _(integration test)_
- [ ] `open` (per-user) agents keep per-user recall; one user never sees another user's episodes. _(regression test)_
- [ ] Broadening a predefined agent's recall to agent-scope does not expose episodes that belong to a different agent (the `agent_id` filter is unchanged). _(regression test)_

---

### FR-07: i18n (en / vi / zh)

New user-facing strings introduced by FR-04's strengthened system-prompt instruction and any injected-section label are added to all three locales per the project Mobile/UI i18n rule. (The detector's locale term lists are backend data, not UI strings, but are still maintained for `en`/`vi`/`zh`.)

Acceptance criteria:

- [x] Any new UI/system-prompt string is present in `en`, `vi`, `zh` locale files with identical key sets. _(no UI strings added; system-prompt text is English-only by convention — CLAUDE.md "Bootstrap templates … stay English-only (LLM consumption)". No `ui/web` locale file changed.)_
- [x] The continuity detector's recall-term lists cover `en`, `vi`, `zh`. _(`internal/memory/recall_trigger.go`; tested)_

---

### FR-08: Authorization & tenant scope (unchanged envelope)

The recall paths already run inside the authenticated, tenant-scoped agent loop. No new endpoint, no new auth surface. The `memory_search` tool stays gated as today (`requireAuth` + tenant scope). The internal continuity-trigger recall reuses the in-process `EpisodicStore.Search` with the same `tenant_id`/`agent_id`/`user_id` filters — it cannot escape the agent's tenant/agent scope.

Acceptance criteria:

- [x] No new HTTP/WS endpoint or auth change. _(by inspection — recall runs in-process inside the existing ContextStage auto-inject path)_
- [x] The continuity-trigger internal search inherits the same ctx tenant/agent/user scope as auto-inject. _(by inspection — same `EpisodicStore.Search(ctx, query, AgentID, UserID, …)` call + the store-level `tenant_id`/`agent_id` filters; the only scope change is FR-01's user-id resolution, gated by `shouldShareMemory`)_

## 4. System Impact

- **Agent loop / context** (`internal/agent/loop_utils.go`, `loop_context.go`, `loop_pipeline_adapter.go`): broaden `shouldShareMemory()` for predefined/shared agents (FR-01); forward `MemoryConfig` into `InjectParams` and pass `L1Depth`/per-hit budget (FR-02, FR-03); invoke the internal continuity recall and inject its results (FR-04); call `RecordRecall` on auto-inject + continuity hits (FR-05).
- **Memory layer** (`internal/memory/auto_injector.go`, `auto_injector_impl.go`, new `recall_trigger.go` / continuity detector): honor `Enabled`/`Threshold`/`MaxEntries`/`MaxTokens`/`L1Depth`/L1 budget; surface L1 `Summary` content for top hits; best-effort `RecordRecall`.
- **Store** (`internal/store/episodic_store.go`, `pg/episodic_summaries.go`): no interface change required — `Search`, `RecordRecall` already exist. (FR-05 reuses `RecordRecall`.)
- **Config** (`internal/memory/auto_injector.go` `MemoryConfig`): add `L1Depth`, `L1PerHitMaxTokens` fields (+ defaults); document that `AutoInject*` fields are now actually consumed.
- **System prompt** (`internal/agent/systemprompt_sections.go`): reinforce the "recall was injected — use it" instruction (FR-04).
- **i18n**: new keys in all three locales (FR-07).
- **No schema migration** (no new column; `recall_count`/`last_recalled_at`/`l0_abstract`/`summary` all exist). **No new error codes** (recall is best-effort; misses are silent, by design).

## 5. Test Plan

- **Unit — `shouldShareMemory`:** predefined→true, open→false, explicit `ShareMemory` override, `ShareKnowledgeGraph`-implies-shared (FR-01).
- **Unit — auto-inject config forwarding:** stub `EpisodicStore` asserts the forwarded `Threshold`/`MaxEntries`/`MaxTokens`/`Enabled` come from `MemoryConfig`; defaults when unset; `Enabled=false`→empty (FR-02).
- **Unit — auto-inject L1 content:** injected section contains a `Summary` fragment for the top hit; respects `MaxTokens`; empty-summary fallback to `L0Abstract` (FR-03).
- **Unit — continuity detector:** en/vi/zh recall-term + temporal references trigger; trivial/non-continuity messages do not (FR-04).
- **Integration — cross-context recall (RC1):** episode created under `user_id=group:...` for a predefined agent is returned when the same agent is recalled under a different user-id; open agent stays per-user (FR-01, FR-06).
- **Integration — continuity trigger:** "what did we discuss Monday?" injects the relevant L1/L2 episode; `recall_count` increments (FR-04, FR-05).
- **Integration — isolation:** cross-tenant and cross-agent recall still blocked (FR-06).
- **Manual (live master tenant):** after the fix, chat with Raka (predefined) from the web UI asking about the Monday 2026-06-15 work and confirm the prior discussion is recalled; confirm the diagnostic `recall_count` query now shows Raka rows with `recall_count > 0`.
- **Checklist:** `go build ./...`, `go build -tags sqliteonly ./...`, `go vet ./...`, `pnpm build` in `ui/web` (if any UI string touched).

## 6. Decision Log (locked)

| Decision | Rationale |
|----------|-----------|
| **Predefined (shared) agents share memory by default (FR-01).** | Raka is a shared-context agent whose KG is already shared but whose memory was not — an inconsistency that locked all its episodic memory to the WhatsApp-group `user_id`. A shared agent should have one unified memory across cron/WhatsApp/web. Aligning memory scoping with the agent's shared nature is the root-cause fix for "Raka doesn't recall its prior work." Explicit `ShareMemory=false` remains as an operator override. |
| **Forward `MemoryConfig` into auto-inject (FR-02) rather than keep hardcoded defaults.** | The config fields already exist and are documented but were silently ignored (`InjectParams` omitted them). Wiring them is a small, safe change that makes recall tunable per agent and makes `AutoInjectEnabled=false` a real opt-out — without it, operators have no lever. |
| **Auto-inject surfaces L1 `Summary` for top hits (FR-03), not a redesign.** | The L0 abstract is ~50 tokens — a tag, not recall. Surfacing a short slice of the actual `Summary` for the top hit makes auto-inject actually useful, while staying within `MaxTokens`. Rejected: always inject full L2 (too expensive / blows the context budget); add a new pipeline stage (unnecessary — `ContextStage` already runs auto-inject). |
| **Add an automatic continuity-recall trigger (FR-04) rather than rely on the LLM calling `memory_search`.** | `recall_count` proves the LLM almost never calls the tool (0/8 for Raka, 136/150 system-wide). Relying on spontaneous tool use is the root cause of "recall not triggered." A cheap deterministic detector + internal search is robust and bounded. Rejected: force a `memory_search` tool-call every turn (expensive, pollutes the tool log); train/prompt-only fix (brittle, already tried — the existing prompt instruction is insufficient). |
| **Do NOT change history compaction (out of scope).** | The "only last few messages in the Sessions menu" is a separate display/history-window concern; the compaction mechanism itself works and Monday's raw messages in short cron sessions are intact (`sessions.summary` is empty for Raka — compaction never fired). Mixing it in would conflate two defects. |
| **Make recall observable via `RecordRecall` on all paths (FR-05).** | This defect was hard to diagnose precisely because auto-inject hits were invisible (`recall_count` tracked only the tool). Recording recall on all paths turns `recall_count`/`last_recalled_at` into a real health signal. |
| **Defer the FTS-English-only and SQLite/LIKE recall-quality issues (§7).** | Real but separate: `plainto_tsquery('english')` under-serves vi/zh; the SQLite LIKE search is broken for multi-line queries. Both are recall-*quality* defects; this SRS fixes recall *triggering/scoping* first. |

## 7. Risks and Open Questions

| Risk or question | Draft decision |
|---|---|
| Broadening predefined-agent recall to agent-scope changes which episodes a previously-per-user-scoped shared agent sees. | Intended — that is the fix. Guarded by FR-06 (tenant + agent_id filters unchanged). Operators who relied on per-user partitioning for a predefined agent can set `ShareMemory=false`. |
| Auto-inject surfacing L1 `Summary` increases per-turn token cost. | Bounded by `AutoInjectMaxTokens` (FR-03); `L1Depth`/per-hit budget are tunable. Default L1Depth=2 keeps the cost small. |
| The continuity detector may false-positive (query looks continuity-shaped but isn't), adding one extra store search. | Acceptable — one bounded search on the rare continuity-shaped turn; never on the common trivial path. Detector is conservative (high-precision terms). |
| Should the continuity trigger also recall **document** memory (MEMORY.md) or only episodic? | Episodic first (the reported symptom). Document-memory recall can reuse the same trigger in a follow-up; keep this SRS to episodic. |
| FTS leg is English-only (`plainto_tsquery('english')`) — vi/zh recall underperforms on the text leg. | Defer to a separate recall-quality SRS (multi-language FTS config / `simple` dictionary). The vector leg still covers vi/zh when embeddings exist. |
| SQLite/desktop episodic search is LIKE-based on the full recall query → effectively broken for follow-ups; lite has no pgvector. | Known limitation (§2). Raka is on the PG/Standard master tenant. A lite/desktop recall fix is a separate task. |
| Confirm Raka is actually invoked from the web UI under a *different* user-id than the WhatsApp group (the RC1 premise). | Verify at implementation time: inspect the user-id set on the agent loop when the owner resumes a Raka session from `/t/master/sessions`. If the web resume already carries the same group user-id, RC1's cross-context symptom is narrower than thought — but the predefined=shared fix is still correct on principle. |
| `memory_search` tool `MinScore` default (0.35, `config.go:250`) vs auto-inject 0.3 — should they unify? | Out of scope; leave as-is. The continuity trigger should reuse the agent's configured threshold where available. |

## 8. Implementation Plan

1. **RC1 / FR-01 — scope:** broaden `shouldShareMemory()` (`internal/agent/loop_utils.go:57`) to return true for `agent_type=predefined` (and respect explicit `ShareMemory`/`ShareKnowledgeGraph`). Unit-test the matrix.
2. **RC2 / FR-02 — config forwarding:** in `loop_pipeline_adapter.go` `makeAutoInjectCallback`, read the agent `MemoryConfig` and populate `InjectParams` (`Enabled`/`Threshold`/`MaxEntries`/`MaxTokens`); default-fallback when unset. Unit-test with a stub store asserting forwarded params.
3. **RC2 / FR-03 — L1 content:** in `auto_injector_impl.go`, for the top `L1Depth` hits inject a truncated `Summary` (per-hit budget) in addition to `L0Abstract`; enforce `MaxTokens`. Add `MemoryConfig.L1Depth` + `L1PerHitMaxTokens` (+ defaults).
4. **RC3 / FR-04 — continuity trigger:** new `internal/memory/recall_trigger.go` detector (en/vi/zh term lists) + an internal `EpisodicStore.Search` call invoked from `ContextStage` (or the auto-inject callback) when the detector fires; inject L1/L2 results labeled as recalled context. Reinforce the system-prompt instruction (`systemprompt_sections.go`).
5. **FR-05 — observability:** best-effort `RecordRecall` from auto-inject (`auto_injector_impl.go`) and the continuity trigger (FR-04) for surfaced episodes; non-blocking.
6. **FR-07 — i18n:** add new keys/term-lists to `en`/`vi`/`zh`.
7. **Tests:** per §5 (unit + integration + the FR-06 isolation regressions).
8. **Checklist:** `go build ./...`, `go build -tags sqliteonly ./...`, `go vet ./...`, `pnpm build` (ui/web if UI strings touched).
9. **Manual (live):** chat with Raka from the web UI about the Monday 2026-06-15 work → confirm recall; run the `recall_count` diagnostic → confirm non-zero.

## 9. Proposed Error Codes

No new canonical error codes. Recall is **best-effort by design**: a miss returns no injection (silent), and the `RecordRecall` write is non-blocking. For traceability only:

| Code (proposed canonical mapping) | Meaning |
|------|---------|
| `memory.recall_unavailable` | The episodic store / embedding provider is nil at recall time (today this is silent — auto-inject returns empty, `auto_injector_impl.go:28-30`). If recall health is later surfaced to operators, map this state to the code. |

No error code is registered by this bugfix; recall failure remains a silent no-op matching today's behavior.

## 10. Evidence appendix (live master tenant, 2026-06-17)

| Fact | Query / source | Result |
|---|---|---|
| Raka agent | `SELECT id, agent_key, agent_type, workspace_sharing FROM agents WHERE agent_key='raka'` | `019ec54f-6fab-7b2b-b509-a28483049ed9`, `predefined`, `share_knowledge_graph:true` (no `share_memory`), `owner_id=system` |
| Episodic rows exist (creation works) | `SELECT count(*), count(*) FILTER (embedding IS NOT NULL), count(*) FILTER (l0_abstract<>'') FROM episodic_summaries WHERE agent_id=<raka>` | 8 / 8 / 8 |
| All scoped to one user-id | `SELECT user_id, count(*) FROM episodic_summaries WHERE agent_id=<raka> GROUP BY user_id` | `group:whatsapp-reski:120363428802496754@g.us` → 8 |
| Recall never happened | `SELECT count(*) FILTER (recall_count>0), count(*) FROM episodic_summaries WHERE agent_id=<raka>` | 0 / 8 |
| Recall is system-wide anemic | `SELECT count(*) FILTER (recall_count>0), count(*) FROM episodic_summaries` | 14 / 150 |
| Recall bookkeeping = tool-only | grep `RecordRecall` callers | only `internal/tools/memory.go:217` |
| Auto-inject ignores agent config | `loop_pipeline_adapter.go:275-281` `InjectParams` omits `MemoryConfig` | hardcoded K=5 / thr=0.3 / 200 tok (`auto_injector_impl.go:35-42`) |
| Monday rows present | `SELECT created_at, turn_count, length(summary) FROM episodic_summaries WHERE agent_id=<raka> AND created_at::date='2026-06-15'` | 5 rows, 8–13 turns each, ~1177–1585-char summaries |

> Side note (not part of this SRS): while investigating, the boot disk was found 100% full due to a **26 GB runaway file** `/tmp/goclaw-history-comment.md` (the line `## Change History — bugfix/filter-paging-embedding-raw → dev` repeated millions of times). It was deleted to unblock the investigation. Worth finding the script/command that wrote it in a loop (check shell history / Makefile targets for a redirect to that path).

## 11. Implementation notes (v0.2) — what was built + deviations from §4/§8

Implemented and build/vet/test-verified locally (PG + SQLite builds green; `go vet` clean on agent/memory/store/config; `go test -race ./internal/agent/ ./internal/memory/` green). Live verification (§5 manual + the FR-01 cross-context / FR-06 isolation integration tests) is deferred to a run against the real master tenant with a pgvector container.

### 11.1 RC1 / FR-01 — scope (files + deviation)

- `internal/store/agent_store.go` — `WorkspaceSharingConfig.ShareMemory` changed `bool` → `*bool` (so an explicit `false` override is distinguishable from unset); the "is config empty?" scan at `:454` now tests `ws.ShareMemory == nil`.
- `internal/agent/loop_utils.go` — `shouldShareMemory()` rewritten: explicit `*ShareMemory` wins either way; otherwise predefined agents share, and an agent that shares its KG shares memory too; `open` agents do not (unless `ShareMemory` set). Added `internal/store` import.
- Tests: `internal/agent/workspace_sharing_test.go` (matrix incl. predefined-nil / predefined-unset / explicit-false-override / open / shared-KG-implies-shared-memory); existing `TestShouldShare*` literals updated to `*bool`. `*bool` JSON parse verified in-package.

**Deviation:** the §4/§8 plan said "broaden `shouldShareMemory()` … respect explicit `ShareMemory`". The plain-`bool` field could not distinguish explicit-`false` from unset, so the field was promoted to `*bool` (2 usages only — low blast radius). This properly satisfies the FR-01 override acceptance criterion.

### 11.2 RC2 / FR-02 + FR-03 + RC3 / FR-04 + FR-05 — recall (files + deviation)

- `internal/config/config.go` — added the AutoInject fields to **`config.MemoryConfig`** (`AutoInjectEnabled *bool`, `AutoInjectThreshold`, `AutoInjectMaxEntries`, `AutoInjectMaxTokens`, `AutoInjectL1Depth`, `AutoInjectL1PerHitTok`). This is the *actually-wired* per-agent config (the `agents.memory_config` JSONB column, read by `AgentData.ParseMemoryConfig()` → `*config.MemoryConfig`, held on the Loop as `l.memoryCfg`).
- `internal/memory/auto_injector.go` — added `InjectParams.Enabled` / `L1Depth` / `L1PerHitMaxTokens`.
- `internal/agent/loop_pipeline_adapter.go` — `makeAutoInjectCallback` now forwards the resolved config into `InjectParams` via `memoryAutoInjectEnabled` / `memoryInt` / `memoryFloat` helpers (default-fallback when `cfg == nil` or value ≤ 0).
- `internal/memory/auto_injector_impl.go` — rewrote `Inject`: honors `Enabled`; resolves max-entries/threshold/max-tokens/L1-depth/L1-per-hit; on a continuity query runs a broader search and labels the section; `buildMemorySection` surfaces a head-truncated `Summary` (via `EpisodicStore.Get`) for the top `L1Depth` hits in addition to the L0 abstract, bounded by a rune-budget ≈ `maxTokens*4`; `recordRecall` best-effort calls `RecordRecall` for injected ids in a detached goroutine (skipped when no tenant in ctx).
- `internal/memory/recall_query.go` — added `headClipRunes` + `runeLen` helpers (rune-safe for vi/zh).
- `internal/memory/recall_trigger.go` (new) — `isContinuityQuery` with en/vi/zh term lists.
- `internal/agent/systemprompt_sections.go` — reinforcement line in the slim + minimal memory sections ("if a Memory Context / Recalled Prior Context section is present above, use it directly").
- Tests: `internal/memory/auto_injector_impl_test.go` (disabled / L1-surfaced / empty-summary-fallback / continuity-broadens-and-labels / non-continuity-no-broaden / record-recall / no-tenant-skip) + `recall_trigger_test.go` (en/vi/zh + negatives), `-race` green.

**Key deviation from §4/§8:** the §4 plan referenced `internal/memory/auto_injector.go MemoryConfig` as the place to add fields. That `memory.MemoryConfig` / `DefaultMemoryConfig` is **dead code** — never parsed from the agent row (`ParseMemoryConfig` returns `*config.MemoryConfig`). The fields were therefore added to `config.MemoryConfig` (the real per-agent config), and `makeAutoInjectCallback` reads `l.memoryCfg`. The dead `memory.MemoryConfig` is left untouched (a separate cleanup); removing it is out of scope here.

### 11.3 L1 retrieval strategy

The §3/§8 plan implied injecting the `Summary` directly. `EpisodicSearchResult` exposes only `L0Abstract` (not `Summary`), so surfacing L1 content uses `EpisodicStore.Get(ctx, episodicID)` for the top `L1Depth` hits (≤2 extra PK lookups per turn, only when there are matches) rather than widening the search SELECT. This avoids a store/SQL change across PG + SQLite. The token budget is approximated as runes/4 (generous for CJK); a precise tokenizer is not imported to keep the memory package dependency-light.

### 11.4 Still pending (env-gated, same convention as 003/007/008)

- Live §5 manual: chat with Raka from the web UI about the Monday 2026-06-15 work → confirm recall; run the `recall_count` diagnostic → confirm non-zero.
- FR-01 cross-context + FR-06 isolation integration tests (need a pgvector pg18 container; the `shouldShareMemory` gating and the store-level `tenant_id`/`agent_id` filters are unit/by-inspection proven).
- `go fix ./...` was intentionally **not** run (per the 007 experience it produces unrelated modernization churn; kept this diff scoped to the feature).
