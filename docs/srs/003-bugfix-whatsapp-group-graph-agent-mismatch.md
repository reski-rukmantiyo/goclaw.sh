# Bug: WhatsApp group "Mini - AMC DELL IOH" routed to wrong agent + wrong KG graph-id

**Status:** Investigation complete. Root cause(s) identified. RC1 + RC2 **code implemented + unit-verified** (§6.5 build/vet/test green); **awaiting live verification (§7) + data remediation (§5.3)** on the real master tenant. See §11 for implementation notes/deviations.
**Date:** 2026-06-14
**Doc version:** 1.4 (see Revision history, §10)
**Scope:** `internal/channels/whatsapp/` listen-only message ingestion → Knowledge Graph scoping
**Tenant:** Master (`0193a5b0-7000-7000-8000-000000000001`)

---

## 1. Summary (TL;DR)

WhatsApp group **"Mini - AMC DELL IOH"** (`chat_id` `120363409245069998@g.us`) is configured for
agent **Felix** with `listen_graph_id` **`project-dell-ioh`**. Despite this, its messages are being
stored under agent **Jarvis** (the channel default) and graph-id **`project-sovereign`**.

Two independent defects cause this:

1. **RC1 — Wrong AGENT (structural, always-present bug).** The listen-only code path resolves the
   per-group agent from a cached map `groupAgentUUIDs` that is **only rebuilt when the whole channel
   instance reloads**. Any group whose `agent_id` override is added/changed at runtime (e.g. by a
   `group_join_rules` match) is silently dropped from this map, so listen-only messages fall back to
   the **channel default agent** instead of the group's configured agent. → explains **Jarvis**.

2. **RC2 — Wrong GRAPH (config not hot-applied).** The live channel's in-memory config was not
   updated after the DB config was edited to `project-dell-ioh`, so `resolveGraphID` kept returning
   the previous value `project-sovereign`. Proven by timestamps: messages stored **after** the config
   write still carried the old graph-id. → explains **`project-sovereign`**.

> Note on naming: the report mentioned graph-id `channel-sovereign`. The value actually persisted in
> the database is **`project-sovereign`** (the value written by the matching `"Sovereign Rule"` join
> rule). No `channel-sovereign` scope exists in this tenant's data. This is the same defect.

---

## 2. Evidence (live DB queries)

### 2.1 Channel + group config (the INTENT)

Channel instance `whatsapp-reski` ("Whatsapp Reski", `019d6334-0062-7b75-a4eb-39937929f6c6`),
default agent = **Jarvis** (`019d2c1a-8697-79ee-b2d8-dc4c8c94662a`, agent_key `fox-spirit`).

`channel_instances.config` JSONB, group entry for "Mini - AMC DELL IOH":

```json
"120363409245069998@g.us": {
  "name": "Mini - AMC DELL IOH",
  "agent_id": "felix",                 // ← intended agent
  "listen_only": true,
  "listen_graph_id": "project-dell-ioh", // ← intended graph scope
  "require_mention": false
}
```

`channel_instances.updated_at` = **2026-06-14 09:07:16 UTC** (config last written).

The matching join rule that originally auto-configured this group:

```json
{ "name": "Sovereign Rule",
  "added_by": "6281511488487@s.whatsapp.net",   // reski's number
  "agent_id": "felix",
  "listen_only": true,
  "listen_graph_id": "project-sovereign" }        // ← the value that "stuck" in memory
```

### 2.2 Stored messages (the REALITY)

`listen_raw_messages` for the same `chat_id` — **all** rows:

```
graph_id         | agent_id                              | n | first_ts             | last_ts
project-sovereign| 019d2c1a-8697-79ee-b2d8-dc4c8c94662a  | 12| 2026-06-14 08:39:17  | 2026-06-14 09:25:04
```

`raw_message_chunks`: 8 chunks, same `graph_id`/`agent_id`.

- `agent_id 019d2c1a...` = **Jarvis** (`fox-spirit`) — the *channel default*, NOT Felix.
- `graph_id` = **`project-sovereign`** — the join-rule value, NOT the configured `project-dell-ioh`.

### 2.3 The decisive timestamp proof (not stale data)

The config was written at **09:07:16**. Messages stored at **09:22:50** and **09:25:04** — i.e.
*after* the edit — still carried `project-sovereign` + Jarvis. **This rules out "old messages from
before the edit". The running gateway simply did not apply the edited config.**

### 2.4 The same defect is visible on sibling groups (collateral)

Several other Felix/IOH groups show the config-vs-stored divergence, confirming the pattern is
systemic, not unique to one group:

| chat_id (tail) | name | config graph | stored graph(s) |
|---|---|---|---|
| `...090069998` | Mini - AMC DELL IOH | `project-dell-ioh` | `project-sovereign` ✗ |
| `...9373562694` | MII - LA - IOH VMware Exit | `project-dell-ioh` | `project-sovereign` ✗ |
| `...08522364211` | Channel IOH-Dell | `project-dell-ioh` | `project-ioh` + `project-dell-ioh` (graph changed mid-life) |
| `...05965733771` | [INT] - IOH - CMI Switch | `project-cmi-switch` | `project-cmi-switch` + `project-sovereign` |

---

## 3. Code-path trace

Inbound WhatsApp group message (listen-only) → `internal/channels/whatsapp/inbound.go`:

```go
// inbound.go:228-281  (listen-only branch)
if c.isListenOnly(chatID, peerKind) {
    ...
    effectiveAgentUUID := c.groupAgentUUID(chatID)   // ← AGENT source  (RC1)
    graphID := c.resolveGraphID(chatID, peerKind)    // ← GRAPH source  (RC2)
    ...
    c.listenBuf.Add(graphID, listenEntry{
        ...
        AgentID: effectiveAgentUUID,
    })
}
```

### 3.1 RC1 — agent resolution (the structural bug)

`groupAgentUUID(chatID)` reads `c.groupAgentUUIDs[chatID]` (`whatsapp.go:785`). That map is populated
**only** by `ResolveGroupAgentOverrides()` (`whatsapp.go:755`), which is called **only** from
`instance_loader.go:435` during `loadInstance` (i.e. at startup or on a full `InstanceLoader.Reload`).

Runtime mutations of `c.config.Groups` do **not** refresh `groupAgentUUIDs`. Critically,
`applyJoinRules()` (`whatsapp.go:436-494`) — fired when the bot is added to a group — writes the new
group into `c.config.Groups` in-memory and persists it to DB, but **never calls
`ResolveGroupAgentOverrides`**. So a join-rule-created group has:

| Setting | Resolved from | Honored at runtime? |
|---|---|---|
| `listen_graph_id` | `c.config.Groups[chatID]` (live map) | ✅ yes (`resolveGraphID`) |
| `require_mention`, `enabled`, name | `c.config.Groups[chatID]` (live map) | ✅ yes |
| **`agent_id`** | **`c.groupAgentUUIDs[chatID]` (stale snapshot)** | ❌ **no → falls back to channel default** |

The fallback is explicit in `listen_buffer.go:113-121`:

```go
effectiveAgentUUID := agentUUID        // buffer default = CHANNEL agent (Jarvis)
if entry.AgentID != "" {               // entry.AgentID = groupAgentUUID(chatID); "" if missing
    effectiveAgentUUID = entry.AgentID
}
```

So whenever `groupAgentUUIDs` lacks the chat → messages stored under **Jarvis**. This is exactly what
we observe, and it is independent of any config edit.

> Asymmetry note: the **non-listen (response)** path uses `resolveAgentID(chatID, peerKind)`
> (`inbound.go:186`), which reads `c.config.Groups` directly and **would** correctly return `felix`.
> But "Mini - AMC DELL IOH" is `listen_only: true`, so it never reaches that path — it always hits the
> listen-only branch with the stale map. This is why the bug is silent: only listen-only groups with
> runtime-added overrides are affected.

### 3.2 RC2 — graph resolution (config not applied)

`resolveGraphID(chatID, peerKind)` (`whatsapp.go:649`) reads `c.config.Groups[chatID].ListenGraphID`
directly from the **live** in-memory config. It returns the *current* in-memory value, not a stale
snapshot. The fact that it returned `project-sovereign` after the DB was edited to `project-dell-ioh`
means the **live `c.config.Groups[Mini]` was never updated** — i.e. the channel instance was not
reloaded after the edit.

Reload trigger: `InstanceLoader.Reload` runs only on a `bus.EventCacheInvalidate` with
`CacheKindChannelInstances` (`cmd/gateway_channels_setup.go:187-196`), which is published by
`methods.ChannelInstancesMethods.handleUpdate` (`methods/channel_instances.go:227`) and the HTTP
equivalent. The web UI's group-override editor saves via exactly that WS method
(`channel-groups-tab.tsx:87-99` → `channels.instances.update`), so **a UI edit does invalidate**.

Conclusion: the **09:07 edit did not go through the invalidating handler** — most likely a direct DB
`UPDATE` on `channel_instances.config` (no event bus in that path). Direct DB edits never reach the
in-memory channel; they only take effect on the next full gateway restart / `InstanceLoader.Reload`.

---

## 4. Root causes (confirmed)

| # | Defect | Effect | Severity |
|---|---|---|---|
| **RC1** | `groupAgentUUIDs` is a startup/Reload-only snapshot. `applyJoinRules` (and any runtime group mutation) updates `c.config.Groups` but not `groupAgentUUIDs`. Listen-only path uses the stale map → agent falls to channel default. | Listen-only messages stored under **wrong agent** (Jarvis instead of Felix). KG extracted into wrong agent's graph. | **High — structural, reproduces without any config edit** |
| **RC2** | Editing `channel_instances.config` directly in the DB (bypassing `channels.instances.update`) does not reload the live channel instance. | Edited `listen_graph_id` (and any field) is not applied until restart. Messages keep old graph-id. | **Medium — operator-footgun; UI edits are NOT affected** |

Both RC1 and RC2 were simultaneously active for this group, producing the double mismatch.

---

## 5. Solution (chosen approach)

This bug has **two independent defects (RC1 + RC2)** plus a data-remediation step. One option is
chosen per defect; rejected alternatives are listed with rationale so the decision is auditable.
**Nothing is implemented yet** — this section records what will be built once approved.

### 5.1 RC1 (wrong agent) — CHOSEN: Option A, variant 1 (unify to a single live source)

**Build:** make the listen-only path resolve the per-group agent from the **same live source** the
response path uses (`c.config.Groups`), instead of the stale `groupAgentUUIDs` chat-keyed snapshot.

- In `inbound.go` listen-only branch, replace
  `effectiveAgentUUID := c.groupAgentUUID(chatID)` with: read the group's `agent_id`
  (`resolveAgentID(chatID, peerKind)` → agent_key, e.g. `"felix"`), then resolve agent_key → UUID via
  a small **agent_key → UUID cache** (not a chat-keyed map). Fall back to channel default only when no
  override exists (correct behavior).
- Repurpose/replace `groupAgentUUIDs` (chatID→UUID) with an `agentKey→UUID` resolver, or wire the
  existing `agentStore` reference held by `ResolveGroupAgentOverrides` so the channel can resolve on
  demand. Cache must be safe for concurrent reads (RWMutex, same pattern as existing fields).
- Must **not** add a DB lookup per inbound message — resolve through the cache; refresh cache on
  agent-create/update (existing agent cache-invalidate events) and on channel reload.

**Why this variant:** the bug exists *because* there are two agent sources that diverge. Variant 1
removes the second source entirely, so no future mutation site can silently reintroduce the drift.
It also generalizes the fix to every listen-only group with a runtime-added override, not just this
one.

**Rejected:**
- *Option A, variant 2 (rebuild `groupAgentUUIDs` inside `applyJoinRules` and every mutation).*
  Smaller diff, but keeps two sources and is fragile — the next runtime mutation site that forgets to
  call the rebuild reintroduces the exact same bug. Chosen only if hot-path agent-store wiring proves
  impractical.

### 5.2 RC2 (wrong graph — config not applied) — CHOSEN: Option B, code variant (periodic config resync)

**Build:** add a lightweight periodic resync that reconciles running channel instances against the DB.

- Background ticker (e.g. every 60 s) compares each loaded channel instance's last-loaded
  `updated_at` against `channel_instances.updated_at`. When it changed, trigger the existing
  `InstanceLoader.Reload` (same path as the cache-invalidate event), so the live `c.config.Groups`
  reflects the edited `listen_graph_id`.
- Keep the existing UI `channels.instances.update` → emitCacheInvalidate → Reload path intact
  (it already works and gives immediate apply).

**Why chosen:** the operator workflow that surfaced this bug uses **direct DB edits** on
`channel_instances.config` (confirmed: the 09:07 edit did not go through the invalidating WS handler).
UI-only guidance (the no-code variant) would not satisfy that workflow. Periodic resync makes direct
DB edits eventually-consistent without a manual restart, at low cost.

**Rejected:**
- *Operator guidance only (document "edit via UI / restart after direct SQL").* Insufficient — it
  relies on humans remembering a non-obvious rule and leaves the silent failure mode in place.
- *Restart-on-change / invalidate-on-startup only.* Heavier and not live; does not help a
  long-running gateway.

### 5.3 Data remediation (mis-scoped rows) — CHOSEN: Option C, re-key (sequenced, with backup)

**Build (after 5.1 + 5.2 are live and verified):**

1. Take a DB backup / `pg_dump` of `listen_raw_messages`, `raw_message_chunks`, and the affected KG
   `entities`/`relations`.
2. Re-key the affected `listen_raw_messages` + `raw_message_chunks` rows for the groups in §2.4 to
   their configured `graph_id`, and re-attribute to the correct `agent_id` (Felix UUID).
3. Re-attribute already-extracted KG entities whose `agent_id`/`user_id` were written under the wrong
   scope (so Felix's `project-dell-ioh` graph actually contains them).
4. Re-verify via the §6.1 / §7 queries.

**Why chosen:** the 12 messages + 8 chunks (and sibling-group rows) were extracted into the wrong
agent/scope and are invisible to Felix otherwise. Leaving them = lost context. Re-keying recovers
them. This is **optional data recovery**, not a code change.

**Guard:** run **only after** 5.1 is verified, with a backup, target agent confirmed per group.
> Do NOT run remediation SQL before the RC1 code fix; otherwise new messages keep landing in the
> wrong place and re-keying has to be repeated.

### 5.4 Implementation order

1. RC1 fix (§5.1) → build, vet, verify §6.1–6.2.
2. RC2 fix (§5.2) → verify §6.3 (direct DB edit now reconciles within ~60 s).
3. Regression sweep (§6.4) + build/test/safety (§6.5).
4. Data remediation (§5.3) → verify §7 query shows single correct `(graph_id, agent_id)`.

---

## 6. Acceptance Criteria (Definition of Done)

A fix is accepted **only when every box below is checked**. These are the gating conditions; the
verification steps in §7 describe *how* to exercise them.

### 6.1 Functional — correct routing for the target group

- [ ] A new inbound message in "Mini - AMC DELL IOH" (`120363409245069998@g.us`) is stored in
      `listen_raw_messages` with `agent_id = 019d6771-abce-7ad1-8e4d-8ee0a211c3cc` (**Felix**), not
      the channel default Jarvis.
- [ ] The same row has `graph_id = project-dell-ioh` (matches the group config), not
      `project-sovereign`.
- [ ] Extracted KG entities for new messages are written with
      `agent_id = Felix UUID` and `user_id = project-dell-ioh` (verified via `extract_worker.go`
      `UserID = graphID`), so they appear in Felix's `project-dell-ioh` graph scope.
- [ ] The store log line `"whatsapp listen: raw message stored"` shows `override=true` (agent
      override applied), not the channel-default fallback.

### 6.2 Functional — RC1 fix generalized (runtime-added overrides)

- [ ] A group added at runtime via a `group_join_rules` match (no full reload) routes its
      listen-only messages to the rule's/configured `agent_id`, not the channel default.
- [ ] `applyJoinRules` (or the chosen fix) leaves `groupAgentUUIDs` (or its replacement) consistent
      with `c.config.Groups` immediately after the group is added.
- [ ] Changing a group's `agent_id` at runtime (without a full channel reload) is reflected on the
      next inbound listen-only message.

### 6.3 Functional — RC2 fix (config edit propagation)

- [ ] Editing `listen_graph_id` via the web UI is applied to the live channel (auto-invalidate →
      reload) and reflected on the next message.
- [ ] *(If Option B "code" is chosen)* A direct DB edit to `channel_instances.config` is reconciled
      within the documented interval (eventual consistency) or a documented restart/reload step is
      enforced and documented in operator docs.

### 6.4 No regressions

- [ ] Sibling groups (§2.4) still scope to their configured agent + graph after the fix.
- [ ] A group with **no** `agent_id` override still falls back to the channel default agent (existing
      behavior preserved — the fallback itself is correct; only the *missing-override-due-to-stale-map*
      case was the bug).
- [ ] A non-listen-only group still routes through `resolveAgentID` and responds normally.
- [ ] DM listen-only messages (global `listen_only`) are unaffected.

### 6.5 Build, test & safety

- [x] `go build ./...` passes.
- [x] `go build -tags sqliteonly ./...` passes (whatsapp code is shared with desktop).
- [x] `go vet ./internal/channels/whatsapp/... ./internal/channels/... ./cmd/...` is clean.
- [x] RC1 regression unit test added: `internal/channels/whatsapp/group_agent_test.go` (5 cases:
      runtime-override-no-reload, no-override-fallback, cache-hit-after-nil-store, refresh-rebuild,
      unresolvable-key). `go test -race ./internal/channels/whatsapp/ -run TestResolveGroupAgentUUID`
      green. (Package also has 2 pre-existing, unrelated `TestMimeToExt` failures in
      `media_utils_test.go` — not touched by this change.)
- [x] No per-message DB lookup introduced on the hot path (agent_key → UUID resolved via cache, not a
      fresh query per inbound message).
- [ ] Any SQL remediation (Option C) is run **only after** the code fix is live and verified, against
      a **backup**, and with the target agent explicitly confirmed.

### 6.6 Operator / docs

- [ ] If RC2 is addressed by guidance only (no code), the requirement to edit channel config via the
      UI / `channels.instances.update` (or restart after direct DB edits) is recorded in operator docs.

---

## 7. Verification plan (run after any fix, before declaring done)

1. **Build:** `go build ./...` and `go build -tags sqliteonly ./...` (whatsapp path is shared).
2. **Reproduce gate (before):** send a message into the live "Mini - AMC DELL IOH" group and confirm
   the new `listen_raw_messages` row shows `project-sovereign` + Jarvis (proves the bug exists).
3. **Apply fix** (Option A and/or B).
4. **Reload** the channel (restart gateway, or trigger a valid config invalidation).
5. **Reproduce gate (after):** send a message; new row must show `graph_id = project-dell-ioh` and
   `agent_id = Felix UUID (019d6771-abce-7ad1-8e4d-8ee0a211c3cc)`.
6. **Regression sweep:** confirm sibling groups (§2.4) and a non-join-rule group still scope correctly.
7. Query to confirm ongoing correctness:
   ```sql
   SELECT graph_id, agent_id, count(*), max(msg_timestamp)
   FROM listen_raw_messages
   WHERE chat_id = '120363409245069998@g.us'
   GROUP BY 1,2 ORDER BY max(msg_timestamp) DESC;
   ```

---

## 8. Open questions / disambiguation (need the live gateway)

The gateway process that ingested the 09:22–09:25 messages is **no longer running** locally (only the
`goclaw-pg` container is up), so its slog output for the routing decision could not be read. To
fully confirm RC2's trigger path, capture these log lines during a reproduction (they already exist
in `inbound.go`):

- `"whatsapp group routing"` (`chat_id`, `default_agent`, `groups_count`)
- `"whatsapp group agent override applied"` vs `"whatsapp group no override found"` (`available_keys`)
- `"whatsapp routing resolved"` (`final_agent`, `override_applied`, `groups_configured`)
- `"whatsapp listen-only agent resolution"` (`effective_agent_uuid`, `fallback_to_default`)
- `"whatsapp listen: raw message stored"` (`graph_id`, `agent_id`, `override`)

Expected for a buggy run: `override_applied=false`, `fallback_to_default=true`,
`groups_configured` may be non-zero (proving the map has the group but `groupAgentUUIDs` doesn't),
and the stored `graph_id=project-sovereign`.

If instead `override_applied=true` is logged while the stored agent is still Jarvis, RC1's mechanism
would be different and must be re-traced. Based on current code this is not expected.

---

## 9. Key files

- `internal/channels/whatsapp/inbound.go:185-286` — listen-only routing + agent/graph resolution
- `internal/channels/whatsapp/whatsapp.go:436-494` — `applyJoinRules` (mutates `Groups`, not `groupAgentUUIDs`)
- `internal/channels/whatsapp/whatsapp.go:647-690` — `resolveGraphID` / `resolveAgentID`
- `internal/channels/whatsapp/whatsapp.go:753-790` — `ResolveGroupAgentOverrides` / `groupAgentUUID`
- `internal/channels/whatsapp/listen_buffer.go:94-149` — `Add()` empty-agent fallback to channel default
- `internal/channels/instance_loader.go:152-193, 433-435` — `Reload` + where overrides are resolved
- `cmd/gateway_channels_setup.go:185-197` — reload trigger on `CacheKindChannelInstances`
- `internal/gateway/methods/channel_instances.go:173-230` — `handleUpdate` + `emitCacheInvalidate`
- `ui/web/src/pages/channels/channel-detail/channel-groups-tab.tsx:87-105` — UI save path

---

## 10. Revision history

| Version | Date       | Author | Change |
|---------|------------|--------|--------|
| 1.0     | 2026-06-14 | —      | Initial investigation: evidence (DB queries + timestamps), code-path trace, RC1 + RC2 root causes, solution options (A/B/C), verification plan, open questions, key files. |
| 1.1     | 2026-06-14 | —      | Added §6 Acceptance Criteria (Definition of Done) checklist (6 groups: target routing, RC1 generalization, RC2 propagation, regressions, build/safety, docs). Renumbered Verification/Open questions/Key files → §7/§8/§9. |
| 1.2     | 2026-06-14 | —      | Rewrote §5 from options → **chosen approach**: RC1 = Option A var1 (unify to live `c.config.Groups` + agent_key→UUID cache); RC2 = Option B code (60 s config resync); remediation = Option C (re-key, sequenced). Rejected alternatives listed w/ rationale. Added §5.4 build order. Updated top Status. |
| 1.3     | 2026-06-14 | —      | Added doc versioning: `Doc version` header field + this Revision history (§10). |
| 1.4     | 2026-06-14 | —      | **Implemented** RC1 + RC2 (code-only, unit-verified). See §11 for deviations from the §5 design assumptions + the files touched. Marked §6.5 build/vet/test boxes done. Live verification (§7) + remediation (§5.3) still pending. |

---

## 11. Implementation notes (v1.4) — what was built + deviations from §5

Both fixes implemented and unit-verified locally (PG build + SQLite build + vet clean; see §6.5).
Live verification (§7) is deferred to a run against the real master tenant (local `goclaw-pg` is a
seed DB with no master-tenant data; the ingesting gateway is not running).

### 11.1 RC1 — files + deviation from the §5.1 assumption

The §5.1 plan assumed the channel already held an `agentStore` reference ("wire the existing
agentStore reference held by `ResolveGroupAgentOverrides`"). **That assumption was wrong**:
`agentStore` was only a method *parameter* of `ResolveGroupAgentOverrides`, never stored on the
struct. New wiring was required.

Changes:
- `internal/channels/whatsapp/whatsapp.go` — replaced the chatID→UUID `groupAgentUUIDs` snapshot
  with: `agentStore store.AgentStore`, `tenantDBMgr store.TenantDBManager`, and an `agent_key→UUID`
  `agentKeyCache` guarded by `agentKeyMu sync.RWMutex`. New `resolveGroupAgentUUID(chatID)` reads the
  **live** `c.config.Groups` (under `c.mu`), resolves key→UUID via cache (miss → on-demand
  `GetByKey`/`GetByID`, cached), returns `""` when no override (caller falls back to channel default).
  `RefreshGroupAgentCache` rebuilds the cache from live config (called on reload + CacheKindAgent).
  `SetAgentStore` / `SetTenantDBManager` / `tenantScopedCtx()` added.
- `internal/channels/whatsapp/inbound.go` — listen-only branch calls `resolveGroupAgentUUID`;
  dropped the now-meaningless `group_agent_uuids_nil` log field.
- `internal/channels/instance_loader.go` — `loadInstance` wires `SetTenantDBManager` **before**
  `ResolveGroupAgentOverrides` (order matters: the warm-up resolves via `tenantScopedCtx`, which
  needs `tenantDBMgr`).
- `cmd/gateway_channels_setup.go` — subscribes `TopicCacheAgent`; on `CacheKindAgent` it iterates
  loaded channels and calls `RefreshGroupAgentCache` on any implementing channel.
- `internal/channels/whatsapp/group_agent_test.go` (new) — 5 regression cases incl. the core
  stale-map repro (runtime override resolves without reload).

**Multi-tenant correctness (verified during impl):** `PGAgentStore.GetByKey` selects its DB via
`TenantDBFromContext`, which is only populated by `store.ResolveTenantDB`. So `resolveGroupAgentUUID`
builds the ctx as `WithTenantID(tenantID)` **+** `ResolveTenantDB(tenantDBMgr)` — otherwise
non-master tenants would silently resolve against the master DB, miss the agent, and fall back to the
channel default (a regression for them). `tenantDBMgr` is nil only on code paths that never had group
override resolution anyway (config-based channels without an InstanceLoader); `resolveGroupAgentUUID`
returns `""` there, matching prior behavior.

### 11.2 RC2 — files

- `internal/channels/instance_loader.go` — `loadedUpdatedAt map[string]time.Time` recorded in
  `loadInstance`, reset in `Reload`/`Stop`. `ResyncIfStale(ctx)` snapshots under `l.mu`, releases,
  then re-lists from DB and calls `Reload` if any `updated_at` moved or the enabled set changed.
  `StartConfigResync(ctx, interval)` spawns the ticker goroutine (exits on ctx cancel).
- `cmd/gateway.go` — `instanceLoader.StartConfigResync(ctx, 60*time.Second)` after `StartAll`
  (`ctx` is the gateway root ctx, cancelled on shutdown → no goroutine leak; `instanceLoader.Stop` is
  not called anywhere, which is pre-existing and harmless for the goroutine).

### 11.3 Still pending (per chosen verify/remediation plan)

- Live §7 repro + §6.1/§6.3 acceptance on the real master tenant (Felix UUID
  `019d6771-abce-7ad1-8e4d-8ee0a211c3cc` is asserted in §6.1 but **unverified locally** — resolve
  from live DB at verification time).
- §5.3 data remediation (re-key `listen_raw_messages` / `raw_message_chunks` / KG entities for the
  §2.4 groups), only after the code fix is live-verified + a backup is taken.
