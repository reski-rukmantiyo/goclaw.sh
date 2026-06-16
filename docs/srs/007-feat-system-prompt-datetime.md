# Software Requirements Specification: Current Datetime + Per-User Timezone in the Default System Prompt

**Project**: GoClaw Gateway
**Release**: 2026.3.0
**Version**: 1.0-implemented
**Date**: 2026-06-16
**Status**: Implemented — FR-00–FR-08 done (build/vet/test green); FR-06 + FR-09 option A **verified live** against the default provider (glm-5-turbo: corrected "good morning"→"good evening" + named tomorrow correctly in user tz). FR-09 B/C descoped (option A retained). Live migration apply + desktop `pnpm build` pending (see §9 Outstanding).
**Difficulty**: Medium
**Estimate**: 2 days

---

## Revision History

| Version | Date | Changes |
|---------|------|---------|
| 0.1-draft | 2026-06-16 | Initial draft. Identified that a current-datetime section (`buildTimeSection`) already exists but uses the **system-wide** tz only — no per-user tz. Proposed mirroring the `locale` pattern (ephemeral transport). Persisted tz + channels marked out of scope. |
| 0.2-draft | 2026-06-16 | Scope expanded after design decisions + code verification. (1) Timezone is **persisted** on a new `users.timezone` column (dual PG + SQLite migration) for auth users — durable across reconnect/clients, source of truth. (2) **Channels included**: a channel sender has **no** `users` row (no sender→users mapping exists in schema or code), so channels use the **USER.md-learned path** (agent asks once, writes a `Timezone:` line, `buildTimeSection` reads it). Unified by a single per-turn `TimezoneKey` context value consumed by `buildTimeSection`. Verified edit sites: migration `000089`, SQLite patch `47`/`SchemaVersion` 48, `RequiredSchemaVersion` 89, `UserData.Timezone`, PG/SQLite stores, `PATCH /v1/users/me`. Difficulty Low→Medium; estimate 0.5→2 days. |
| 0.3-draft | 2026-06-16 | **Implemented + build/vet/test green.** Migration `000089` (PG) + SQLite patch key **47** (the apply loop at `schema.go:1252` keys on the FROM version, so the next patch key is the current `SchemaVersion` = 47, **not** 27 as the 0.2 draft said) + `SchemaVersion` 48 + `RequiredSchemaVersion` 89. `UserData.Timezone *string` + PG/SQLite scan/INSERT/UPDATE. `PATCH /v1/users/me` accepts + validates IANA tz. `TimezoneKey` + `WithTimezone`/`TimezoneFromContext`. WS `connect` `timezone` param → `client.timezone` → per-request `store.WithTimezone`; HTTP `X-GoClaw-Timezone` → `enrichContext`. `buildTimeSection(user, default)` prefers per-turn tz (transport or USER.md-parsed) → system → UTC; preview gap closed. `datetime` tool prefers context tz. Conditional ask-hint (loop + onboarding copy). Web + desktop send `Intl` tz on connect. Tests: `TestTimeSectionUserTimezonePriority`, `TestParseTimezoneLine`, `TestTimezoneContext`. **Two deviations from 0.2 (durability-only; the prompt already receives the per-turn tz on the wire):** (a) WS connect does **not** persist-on-change — the gateway has no `UserStore` wired; persistence is via `PATCH /v1/users/me` (auto-persist-on-connect deferred). (b) HTTP auth-middleware backfill from `users.timezone` is omitted — the auth hot path (`enrichContext`) loads no `users` row, so a per-request DB read was avoided; header-less HTTP requests fall back to the system default. Desktop frontend `pnpm build` not run (no `node_modules` locally); the `ws.ts` change is a one-liner mirroring the web client (web `pnpm build` green). One pre-existing unrelated test failure (`TestKGTraversal_Tier1_CappedAt20`, KG traversal cap — code untouched by this feature). |
| 0.4-draft | 2026-06-16 | Documented the **information-vs-enforcement** scope boundary. Added a scope note to FR-06 (prompt injection gives the model ground truth but does not deterministically verify temporal claims) + a new **FR-09** (deferred temporal-claim verification layer with 3 options: A stronger prompt mandate, B deterministic input guard, C mandatory tool call) + a §6 Risks row. Nothing new implemented — this records the honest limit of the prompt-injection approach and the upgrade path. |
| 0.5-draft | 2026-06-16 | **Implemented FR-09 option A.** `buildTimeSection` guidance line upgraded from a soft "interpret relative time" hint to an explicit **verify-before-replying mandate** covering time-of-day greetings (good morning/afternoon/evening/night) + relative statements, instructing the model to state the actual local time instead of mirroring the user when inconsistent. Still **soft** (raises compliance probability, not a guarantee) — that is an inherent LLM limit, not a bug: nothing in a prompt can deterministically force a next-token predictor to execute an instruction; RLHF politeness bias + attention dilution + provider/temperature variance all work against it. Options B (deterministic input guard) and C (mandatory tool call) remain the only paths to hard enforcement, both deferred. `go build` + `go vet` + agent tests green. |
| 0.6-draft | 2026-06-16 | **Extended option A with a pre-computed part-of-day bucket.** `buildTimeSection` now appends `— morning/afternoon/evening/night` (new `timeOfDayBucket` helper, locale-neutral thresholds 05–11/12–16/17–20/21–04) to the date line, derived from the shown local hour. Rationale: removes the model's "21:37 → evening" inference step; a labelled bucket makes a user's "good morning" clash lexically with the shown "evening" → higher correction likelihood. Still soft (not a guarantee); expected lift on strong models ~A-alone 60–80% → A+bucket ~75–90% (illustrative; real numbers need a live per-provider eval). New tests `TestTimeOfDayBucket` + `TestTimeSectionBucketAppended`; `go build` + `go vet` + 8 agent time-tests green. |
| 1.0-implemented | 2026-06-16 | **Closed.** All in-scope FRs (FR-00–FR-08) implemented + `go build`/`go vet`/agent tests green; FR-09 option A (mandate + part-of-day bucket) shipped (soft), B/C deferred. Acceptance criteria reconciled to implementation state (`[x]` done, `[~]` code-complete pending live/manual verification). Fixed stale `patch 27`→`47` references (§4/§5/§7). Outstanding (not blocking closure) recorded in §9. |
| 1.1-implemented | 2026-06-16 | **FR-06 + FR-09-A verified live.** Ran a one-shot eval against the default provider (glm-5-turbo via zai-coding, key decrypted from `llm_providers` with `GOCLAW_ENCRYPTION_KEY`): built the real `BuildSystemPrompt` output with `UserTimezone=Europe/Berlin` (local 18:02 → bucket "evening") + sent `Hey, good morning! What day is tomorrow for me?`. Reply: *"Good evening! It's actually around 6 PM where you are, not morning. Tomorrow for you is Wednesday, June 17, 2026."* — greeting corrected AND relative-time correct; `reasoning_content` confirms it used the embedded time line. FR-06 `[~]`→`[x]`; FR-09-A "eval pending"→done. **FR-09 B/C formally descoped** (option A retained per decision). Soft-limit noted: single provider/temp/tz — other providers may vary. Eval program was throwaway (deleted); reproducer = the prompt-builder call + an OpenAI-compat `/chat/completions` POST. |

---

## 1. Summary

The agent system prompt must always carry the **current date/time** so the model knows "now", and it must carry it in the **requesting user's timezone** so the agent can interpret relative-time references ("today", "yesterday", "tomorrow at 9am", "next Monday", "in 2 hours") correctly. This SRS defines that feature for both **auth users** (web/desktop/HTTP API) and **channel users** (Telegram, WhatsApp, Feishu/Lark, Zalo, Discord).

**Current state (verified).** A current-datetime section already exists and is injected into every system prompt (all modes except `none`): `buildTimeSection` (`internal/agent/systemprompt_sections.go:330-346`) emits a line like `Current date/time: 2026-06-16 Monday 12:34 (UTC) / 2026-06-16 Monday 19:34 (Asia/Ho_Chi_Minh)` and is appended in `BuildSystemPrompt` (`internal/agent/systemprompt.go:488-491`, section 8, deliberately below the cache boundary). The timezone it renders is the **system-wide** `cron.default_timezone`, plumbed through `SystemPromptConfig.DefaultTimezone` (`systemprompt.go:164-166`) and `RunContext.DefaultTimezone` (`internal/store/run_context.go:51`), resolved live from the `SystemConfigs` store (`internal/agent/resolver.go:638-648`). Committed in `1dc1e28e` + `5be7a1cf`; the feature branch (`feat/add-datetime-to-system-prompt`) has no commits ahead of `dev` — greenfield.

**The gap (two parts).**

1. **No per-user timezone.** The WS `connect` handshake carries `locale` but **no timezone** (`internal/gateway/router.go:142-150`); HTTP reaches locale only via `Accept-Language` (`internal/http/auth.go:450, 565`); there is **no** `TimezoneKey`/`WithTimezone`/`TimezoneFromContext` (`internal/store/context.go` has only `LocaleKey` l. 27 + helpers l. 348-358). The web UI holds the user's tz (`useUiStore.timezone`) but it is **display-only, never sent**. The agent is *instructed* to ask the user and write tz to `USER.md` (`internal/agent/loop_history.go:99`; copy at `systemprompt.go:317, 337`).
2. **No persisted tz.** `users` has no timezone column (`internal/store/pg/tenant_schema.sql:3455-3468`; SQLite `internal/store/sqlitestore/schema.sql:1926-1938`), and `UserData` has no tz field (`internal/store/user_store.go:25-37`).

**Design (two paths, one consumer).** `buildTimeSection` reads a single per-turn effective timezone from a new `TimezoneKey` context value. Two paths populate it:

- **Auth users (web/desktop/API):** timezone is **persisted** on `users.timezone` (durable source of truth). The client supplies it via a WS `connect` param / HTTP header on first seen (and via `PATCH /v1/users/me`); the auth middleware resolves the persisted value into context each turn (no extra query — the user row is already loaded for auth).
- **Channel users:** a channel sender maps to **no** `users` row (`req.UserID` is a raw sender id, a `group:{channel}:{chatID}` composite, or a `tenant_users.user_id` VARCHAR with **no FK** to `users` — `cmd/gateway_consumer_normal.go:89-99, 145-157`; `internal/store/pg/tenant_schema.sql:1704`). So channels cannot read `users.timezone`. Instead they use the **USER.md-learned path**: the agent asks once and writes a structured `Timezone: <IANA>` line to the per-scope `USER.md`; the loop parses that line into the per-turn context tz. The existing `loop_history.go:99` hint drives the learning.

So requirement (1) — datetime in the default prompt — is **largely satisfied** and is **formalized** here (incl. closing a preview-path gap, FR-01). Requirement (2) — the agent understanding the context between a question and the current datetime — is **delivered** by FR-03/FR-04/FR-05/FR-07 for both auth and channel users.

This SRS owns: the persisted `users.timezone` column + write path, per-user tz transport, the unified per-turn tz resolution, the channel USER.md-learned path, and the agent's relative-time reasoning guidance. It does not duplicate prior SRS scope.

## 2. Scope

**In scope**:

- Formalizing the current-datetime section as a guaranteed part of the default system prompt for every agent turn (open + predefined, full + compact; closes the preview-path gap — FR-01).
- A **persisted** `users.timezone` column (dual PG + SQLite) + `UserData.Timezone` + PG/SQLite store read/write + `PATCH /v1/users/me` write path + first-seen population (FR-02).
- Per-user tz **transport** (WS `connect` param + HTTP `X-GoClaw-Timezone` header) that persists the value, mirroring `locale` (FR-03).
- **Unified per-turn tz resolution** via a new `TimezoneKey` context key consumed by `buildTimeSection` (effective → system default → UTC) (FR-04).
- **Channel tz via USER.md**: the agent learns + writes a `Timezone:` line; the loop parses it into the per-turn tz; the time section renders it (FR-05).
- Agent **relative-time reasoning** guidance + `datetime` tool using the per-turn tz (FR-06).
- Making the "ask for timezone" hint **conditional** (only when tz is genuinely unknown) (FR-07).

**Out of scope**:

- **Channel identity unification** (mapping a WhatsApp/Telegram/etc sender → a `users` row). Deliberately not built — channels use the USER.md path instead. Recorded as a deferred alternative (§6).
- **Web/desktop client UI** for choosing a timezone beyond the existing `useUiStore` picker. The client already has the browser tz; it just sends it. No new settings screen.
- Calendar/scheduling/booking/reminder **business logic**. The agent only *knows* the correct now/user-tz; no scheduling features are built on top.
- Changing the rendered timezone **format** (stays human-readable `2006-01-02 Monday 15:04`, intentionally — LLM-friendly + cache-stable; not ISO/RFC3339).
- The `cron.default_timezone` system config (consumed unchanged as fallback).
- Storing tz on `tenant_users` / channel contacts tables. Only `users.timezone` for auth users; USER.md for channels.

## 3. Functional Requirements

### FR-00: Current State — datetime injection already exists (verification + gap, no feature code)

Records the verified baseline so the feature is scoped as an *extension*, not from-scratch injection.

| Claim | Evidence |
|-------|----------|
| Time section injected every turn | `buildTimeSection` (`systemprompt_sections.go:330-346`), appended in `BuildSystemPrompt` (`systemprompt.go:488-491`), gated `!isNone` |
| Timezone source is system-wide only | `SystemPromptConfig.DefaultTimezone` (`systemprompt.go:164-166`) ← `cron.default_timezone` (`resolver.go:638-648`) |
| No per-user tz on the wire | WS connect has `locale`, no tz (`router.go:142-150`); `Client.locale` only (`client.go`); HTTP `Accept-Language` only (`auth.go:450,565`) |
| No tz context key | No `TimezoneKey`/`WithTimezone` in `internal/store/context.go` (only `LocaleKey` l. 27 + helpers l. 348-358) |
| No persisted tz | `users` has no tz column (`tenant_schema.sql:3455-3468`, `sqlitestore/schema.sql:1926-1938`); `UserData` no tz field (`user_store.go:25-37`) |
| Channel sender ≠ users row | `req.UserID` is sender id / `group:…` / `tenant_users.user_id` VARCHAR, no FK to `users` (`gateway_consumer_normal.go:89-99,145-157`; `tenant_schema.sql:1704`) |
| Agent told to *ask* for tz | `loop_history.go:99` hint + copy `systemprompt.go:317,337` |

Acceptance criteria:

- [x] Verified: `buildTimeSection` injects `Current date/time: …` every turn (`systemprompt_sections.go:330-346`).
- [x] Verified: no per-user/persisted tz existed before this feature (grep `TimezoneKey|WithTimezone` in `context.go` → 0 pre-change; `users` schema had no tz column).
- [x] Verified: channel `req.UserID` is never a `users.id` UUID (`gateway_consumer_normal.go:89-99,145-157`).
- [x] No code written for this FR — baseline record only.

---

### FR-01: Current Datetime Is Part of the Default System Prompt (formalize)

The system prompt for every agent turn — both agent types (`open`, `predefined`), every mode except `none` — must include a current date/time section. This formalizes existing behavior and closes the **preview-path gap**: `BuildPreviewPrompt` (`internal/agent/preview_prompt.go:246`) does not set `DefaultTimezone`/effective tz, so a previewed time section renders UTC-only. The preview must resolve the same effective timezone as the live path.

Acceptance criteria:

- [x] `BuildSystemPrompt` always appends the time section except in `none` mode (`systemprompt.go:488-491` `if !isNone` — kept).
- [x] Section applies to both `open` and `predefined` agent types (type-agnostic — depends only on tz).
- [x] `BuildPreviewPrompt` resolves + passes the effective tz (`UserTimezone` + `DefaultTimezone` wired into the preview cfg) so the previewed time section matches the live prompt (closes `preview_prompt.go:246` gap).
- [x] Section stays below the cache boundary (section 8, `systemprompt.go:488`) — preserved.

---

### FR-02: Persisted `users.timezone` (schema + store + write path)

Add a durable per-user timezone on the `users` table, read on every auth-user turn. Dual-DB migration, per CLAUDE.md dual-DB rule.

**Schema (PG):** new migration `migrations/000089_user_timezone.up.sql` (+ `.down.sql`), mirroring `000088_user_phone_column`:

```sql
ALTER TABLE users ADD COLUMN timezone VARCHAR(64);   -- IANA name, e.g. Asia/Ho_Chi_Minh
```

**Schema (SQLite):** add `timezone TEXT` to the fresh-DB `users` table (`internal/store/sqlitestore/schema.sql:1926-1938`) **and** add incremental patch key **`47`** at the end of the `migrations` map (`internal/store/sqlitestore/schema.go`). The apply loop (`schema.go:1252`) keys each patch on its FROM version and errors if a key is missing, so keys are contiguous and the next patch key is the current `SchemaVersion` (= 47), not 27. Bump `SchemaVersion` 47 → 48 (`schema.go:19`).

**Version bump:** `internal/upgrade/version.go:5` — `RequiredSchemaVersion` 88 → 89.

**Model + store:** add `Timezone *string` (nullable — `NULL`/`""` = unknown) to `UserData` (`internal/store/user_store.go:25-37`). Update PG (`internal/store/pg/users.go`): `Create` INSERT (`:31`), SELECT lists (`GetByID :52`, `GetByEmail :66`, `GetByIDs :83`, `List :148`), `Update` SET (`:94`), scan helpers (`scanUserRow :320`, `scanUserRows :343`, `List` inline `:169`). Update SQLite (`internal/store/sqlitestore/users.go`): `userCols` const (`:48`), `Create` INSERT (`:39`), `scanUser` (`:50`), `Update` SET (`:145`). Nullable columns use `*string` per project convention.

**Write path:** extend `PATCH /v1/users/me` (`internal/http/users.go:33` route, `:73` `handleUpdateMe`, input struct `:97`, `:108` `Update` call) to accept `timezone`; validate it is a `time.LoadLocation`-parseable IANA name (else 400/422 `request.validation_failed`). First-seen population: set `Timezone` on the `UserData{}` literals at OIDC auto-provision (`internal/http/oidc_handler.go:281`), local registration (`internal/http/auth_handler.go:277`), and tenant user-add (`internal/http/tenants.go:377`) when a value is supplied.

**Tenant databases (multi-tenant) — no per-tenant migration required.** The `users` table is **master-global**, not per-tenant: `PGUserStore` is wired to the master pool (`internal/store/pg/factory.go:77`, the single `db` shared by all global stores), and `users.tenant_id` was dropped in migration `000082` (email is globally unique). Proof the feature needs no tenant-DB migration: the `phone` column (`000088`) is **absent** from `internal/store/pg/tenant_schema.sql` yet ships fine — tenant DBs are never read for users. The `users` table appearing in `tenant_schema.sql:3455` is **vestigial** (created in new tenant DBs by `InitTenantDB`, never read by `PGUserStore`, already stale — it lacks `phone`); it is intentionally NOT updated by this feature. **`./goclaw migrate up` on master + the SQLite patch (desktop) is the complete migration.**

Acceptance criteria:

- [x] PG migration `000089` adds `users.timezone`; down migration drops it; `RequiredSchemaVersion = 89`. _(live `migrate up` apply pending — see §9)_
- [x] SQLite: `schema.sql` fresh-DB has `timezone`; patch `47` ALTERs existing DBs; `SchemaVersion = 48`. Desktop edition (`sqliteonly`) builds (`go build -tags sqliteonly ./...` green). _(live apply pending a running DB)_
- [x] `UserData.Timezone` (`*string`) scanned in/out on all PG + SQLite read/write paths (build-verified).
- [x] `PATCH /v1/users/me { "timezone": "Asia/Ho_Chi_Minh" }` persists; invalid IANA → 400 (`MsgInvalidRequest`); missing/null allowed.
- [x] Nullable: `NULL`/`""` = unknown (no NOT NULL constraint — existing rows migrate cleanly).

---

### FR-03: Per-User Timezone Transport → Persistence (mirror `locale`)

The auth-user client supplies its timezone; the server persists it (FR-02) and resolves it per turn. Transport mirrors the `locale` siblings:

| Transport | `locale` (existing) | `timezone` (NEW) |
|-----------|---------------------|------------------|
| WS handshake param | `locale` (`router.go:146`) | `timezone` IANA name |
| WS client field | `client.locale` (`router.go:156`) | `client.timezone` |
| Per-request context | `store.WithLocale` (`router.go:105`) | `store.WithTimezone` |
| HTTP header | `Accept-Language` (`auth.go:565`) | `X-GoClaw-Timezone` |
| HTTP context | `store.WithLocale` in `enrichContext` (`auth.go:450`) | `store.WithTimezone` in `enrichContext` |

**Per-turn carry (implemented):** the WS `connect` `timezone` param and the HTTP `X-GoClaw-Timezone` header carry the user's tz into the request context (`client.timezone` / `enrichContext` → `store.WithTimezone`). The time section reads this each turn — **no per-request DB read**. **Persistence (implemented via `PATCH`):** the durable `users.timezone` is written by `PATCH /v1/users/me` (FR-02). **Deferred (durability-only — the prompt already gets the per-turn tz on the wire):** (a) auto-persist-on-WS-connect is not wired because the gateway has no `UserStore` today (a frontend `PATCH /v1/users/me` on load, or a future `SetUserStore` on the gateway, can populate the column); (b) HTTP auth-middleware backfill from `users.timezone` is omitted because the auth hot path (`enrichContext`, `auth.go:449`) loads no `users` row, so a per-request persisted-tz read was deliberately avoided — header-less HTTP requests fall back to the system default.

Value contract: IANA name (`time.LoadLocation`-parseable). Empty/absent = unknown. Invalid → ignored with `slog.Warn`, never errors the request.

Acceptance criteria:

- [x] WS `connect` accepts `timezone`; `Client.timezone` stored (validated via `time.LoadLocation`, invalid → `slog.Warn`); per-request `store.WithTimezone` injected alongside `store.WithLocale` (`router.go:105` region).
- [x] HTTP `X-GoClaw-Timezone` → `extractTimezone` → `enrichContext` → `store.WithTimezone` (`auth.go`).
- [x] Per-turn tz carried into context; time section reads it each turn (no per-request DB read). _(implemented + build-verified)_
- [~] Persistence via `PATCH /v1/users/me` writes `users.timezone`; **auto-persist-on-connect + auth-middleware backfill deferred** (gateway has no `UserStore`; auth hot path loads no user row) — durability-only, prompt already gets per-turn tz on the wire. _(deviation from 0.2, documented above)_
- [x] Invalid IANA ignored + warned; no request failure (`router.go` connect + `systemprompt_sections.go` buildTimeSection).
- [x] `context.go` adds `TimezoneKey` + `WithTimezone`/`TimezoneFromContext` mirroring `LocaleKey`/`WithLocale`/`LocaleFromContext` (l. 27/348-358); helper returns `""` when unset. _(TestTimezoneContext passes)_
- [x] Frontend (web + desktop) sends the browser IANA tz in `connect` via `Intl.DateTimeFormat().resolvedOptions().timeZone` (`ui/web/src/api/ws-client.ts`, `ui/desktop/frontend/src/lib/ws.ts`). _(web `pnpm build` green; desktop build env-blocked — no `node_modules` — change is a one-liner mirroring web)_

---

### FR-04: Unified Per-Turn Timezone Resolution (the consumer)

`buildTimeSection` reads a single **effective per-turn timezone** from `TimezoneFromContext`; both auth-user and channel paths populate that one key. Resolution order (first non-empty, `LoadLocation`-valid wins):

```text
per-turn context tz  →  cron.default_timezone (system)  →  UTC
```

Thread the effective tz into `SystemPromptConfig` (e.g. a `UserTimezone *string` alongside `DefaultTimezone`, `systemprompt.go:164-166`) set where `cfg.DefaultTimezone` is set today (`internal/agent/loop_history.go:254`, `loop_context.go:437`). `buildTimeSection` prefers it; `DefaultTimezone` is the fallback. Rendering keeps the two-value form (UTC anchor + local + tz name):

```text
Current date/time: 2026-06-16 Monday 12:34 (UTC) / 2026-06-16 Monday 19:34 (Asia/Ho_Chi_Minh)
```

Acceptance criteria:

- [x] `buildTimeSection` renders the per-turn context tz when present, else system default, else UTC (`TestTimeSectionUserTimezonePriority`, `TestTimeSectionWithTimezone`, `TestTimeSectionWithoutTimezone`).
- [x] Effective tz threaded via `SystemPromptConfig.UserTimezone` (single source); set at `loop_history.go` (`effectiveTimezone := resolveUserTimezone(...)`).
- [x] Invalid per-turn tz → `slog.Warn` + fall back to system default (mirrors `systemprompt_sections.go` warn path); no panic, no request failure.
- [x] Behavior unchanged when no per-turn tz and a system default is set (current path).

---

### FR-05: Channel Timezone via USER.md-Learned Path

Channel conversations have no `users` row (FR-00), so they acquire the user's tz through the per-scope `USER.md`. The codebase already anticipates this (`loop_history.go:99`; `systemprompt_sections.go:396`). This FR makes the **time section honor the learned value** instead of only instructing the agent to ask.

**Learn:** for a channel DM (and group where a single writer is known), the existing hint tells the agent to ask once and write the tz to `USER.md` using a **structured line** `Timezone: <IANA>`. The hint copy is updated to specify this exact line format so it is machine-parseable.

**Resolve:** the loop, after loading context files (`resolveContextFiles`, `internal/agent/loop_history.go:303-328`), scans the loaded `USER.md` content for a `Timezone:\s*([^\s\n]+)` line; if found and `LoadLocation`-valid, sets it as the per-turn context tz (FR-04). Groups with no single known writer fall back to the system default.

Acceptance criteria:

- [x] The `loop_history.go:99` hint instructs writing a structured `Timezone: <IANA>` line to `USER.md` (exact format).
- [x] The loop parses a `Timezone:` line from the loaded `USER.md` into the per-turn context tz (`resolveUserTimezone` + `parseTimezoneLine`; `TestParseTimezoneLine`).
- [x] A channel turn whose `USER.md` has `Timezone: Asia/Jakarta` resolves the time section in `Asia/Jakarta` (logic unit-tested; live channel confirm pending — see §9).
- [x] A channel turn with no `USER.md` tz falls back to the system default (then UTC) — unchanged from today.
- [x] Group chats with no single known writer are not mis-attributed a tz (fall back, not guess).

---

### FR-06: Agent Contextualizes Relative-Time References (requirement 2)

The agent must interpret relative/deictic time references against the injected current datetime **in the effective timezone**. Two mechanisms:

1. **Prompt guidance.** The time section + `datetime`-tool hint (`systemprompt.go:197`) state that relative expressions ("now", "today", "yesterday", "tomorrow", "next/last <weekday>", "in N hours/days", "this morning") resolve relative to the **displayed current date/time in the effective timezone**, not UTC. The agent computes the referenced instant accordingly before answering/scheduling.
2. **`datetime` tool uses the effective tz.** `DateTimeTool.Execute` (`internal/tools/datetime.go:44-54`) today falls back to `RunContext.DefaultTimezone`; it must prefer the per-turn context tz (via `TimezoneFromContext`, or a `RunContext` field populated from it).

Acceptance criteria:

- [x] Given the current datetime in the effective tz, the agent answers "what day is tomorrow?" / "date next Monday?" correctly in that tz. _(live eval v1.1 — default provider glm-5-turbo via zai-coding, tz=Europe/Berlin 18:02 "evening": replied "Good evening! It's actually around 6 PM where you are, not morning." + "Tomorrow for you is Wednesday, June 17, 2026." Greeting correction + relative-time both correct; the model's `reasoning_content` confirms it used the embedded `Current date/time … — evening` line. Single provider/temp/tz — soft; other providers may vary.)_
- [x] `DateTimeTool` returns the effective-tz local time when present, system default otherwise (prefers `TimezoneFromContext` → `RunContext.DefaultTimezone`; build-verified; dedicated unit test pending).
- [x] Time-section copy explicitly instructs relative-time resolution against the shown current datetime (verify-before-replying mandate, FR-09 option A).
- [x] No new tool — reuse existing `datetime` (`internal/tools/datetime.go`).

**Scope boundary (important).** This FR is satisfied at the **information/capability** level: the prompt injects the ground-truth current datetime + effective tz, and the guidance line + `datetime` tool give the model what it needs to reason correctly. It is **not** an enforcement layer — nothing deterministically verifies the user's temporal claims (e.g. the user saying "good morning" at 21:37 their time). A capable model *can* now catch such mismatches; a weak or politeness-trained model may still not. Enforced truth-checking of temporal claims is a separate, deferred layer — see **FR-09**.

---

### FR-07: "Ask for Timezone" Hint Becomes Conditional

The hint that tells the agent to ask for the tz (`loop_history.go:99`) and the "naturally learn … timezone" copy (`systemprompt.go:317, 337`) exist because the backend lacked the tz. Now: **auth users** with a known persisted tz must NOT be asked; **channel users** with a `USER.md` tz must NOT be asked; only genuinely-unknown cases keep the hint.

Acceptance criteria:

- [x] Auth-user turn with non-empty transport tz → no "ask for timezone" hint (`loop_history.go` hint gated on `effectiveTimezone == ""`).
- [x] Channel turn with `USER.md` tz → no ask-hint; without → hint retained (drives FR-05 learning).
- [x] Copy at `systemprompt.go` onboarding block reconciled — agent told the tz is already provided, not to ask for it.

---

### FR-08: i18n (no new UI strings expected)

Feature changes request transport, prompt content, and a DB column — not user-facing UI text. The timezone is data, not a translated string. No new i18n keys expected; any added string follows the 3-locale rule (`en`/`vi`/`zh`) per the Mobile/UI i18n rule.

Acceptance criteria:

- [x] No new raw keys appear in the UI as a side effect (validation error reuses existing `MsgInvalidRequest`).
- [x] Any added string present in all 3 locales — N/A (no new strings added).

---

### FR-09: Temporal-Claim Verification — deferred enforcement layer (out of scope for this release)

FR-01–FR-08 give the model the **ground truth** (current datetime + per-user tz) so it *can* contextualize time-relative language. They do **not** force the model to **verify** the user's temporal claims. Example: a user says "good morning" at 21:37 in their tz. With this SRS the model *sees* `21:37 (Asia/Jakarta)` and *may* correct it, but nothing guarantees it — a politeness-trained or weak model can still reply "good morning!" blindly. This FR records that gap and the candidate enforcement layers. **Option A is now implemented (v0.5); B and C remain deferred.**

| Option | Mechanism | Enforced? | Effort |
|--------|-----------|-----------|--------|
| **A. Stronger prompt mandate** | Add a line instructing the model: before replying to any time-of-day greeting or time-relative claim, compare it to the current time shown above; if inconsistent, state the real time. | Soft — model may still ignore | ~0 (one prompt line in `buildTimeSection`) |
| **B. Deterministic input guard** | Pre-check in the input-guard pipeline: parse time-of-day words ("morning/afternoon/evening/night") and relative expressions in the user message, compare to the injected `now`+tz, flag/annotate a mismatch (mirrors the existing detection-only input guard). | Hard — deterministic, runs regardless of model | Medium — new guard module |
| **C. Mandatory tool call** | Force the model to call `datetime` (or a new `validate_time` tool) before time-sensitive replies; the pipeline asserts the call ran. | Semi-hard — enforced via pipeline | Medium-High |

**Recommendation (status):** **A shipped (v0.5 mandate + v0.6 part-of-day bucket)** — `buildTimeSection` now (1) emits a verify-before-replying mandate covering time-of-day greetings + relative statements, and (2) appends a pre-computed part-of-day label (`— morning/afternoon/evening/night`) to the date line so the model need not infer "21:37 → evening" itself — a labelled bucket makes a user's "good morning" clash lexically with the shown "evening". Still **soft** enforcement (raises probability, not a guarantee). Add **B** for hard enforcement; **C** only to gate time-sensitive actions.

Acceptance criteria:

- [x] (A) `buildTimeSection` (1) emits the verify-before-replying mandate covering time-of-day greetings + relative statements, and (2) appends a computed part-of-day bucket (`— morning/afternoon/evening/night`) to the date line, derived from the shown local hour via `timeOfDayBucket` (`internal/agent/systemprompt_sections.go`). Boundaries unit-tested (`TestTimeOfDayBucket`, `TestTimeSectionBucketAppended`). **Live eval v1.1 (glm-5-turbo): corrected "good morning"→"good evening" using the embedded `… — evening` line + mandate** — the cheap enforcement layer demonstrably works on the default provider. Still **soft** (single provider/temp/tz; raises probability, not a guarantee).
- [ ] (B) input guard flags a user "good morning" at 21:37 effective-tz with a deterministic signal — **descoped v1.1 (option A retained as the chosen enforcement level per product decision; revisit only if A proves insufficient across providers)**.
- [ ] (C) pipeline asserts `datetime`/`validate_time` ran before a time-sensitive reply — **descoped v1.1 (same rationale as B)**.

## 4. System Impact

- **Migration (PG):** NEW `migrations/000089_user_timezone.{up,down}.sql` — `ALTER TABLE users ADD COLUMN timezone VARCHAR(64)`.
- **Migration (SQLite):** `internal/store/sqlitestore/schema.sql` (fresh-DB `users` + `timezone TEXT`), `schema.go` (patch `47` ALTER + `SchemaVersion` 47→48).
- **Version:** `internal/upgrade/version.go:5` — `RequiredSchemaVersion` 88→89.
- **Store interface/model:** `internal/store/user_store.go` — `UserData.Timezone *string`.
- **Store PG:** `internal/store/pg/users.go` — INSERT/SELECT/SET/scan sites (Create/Get*/List/Update).
- **Store SQLite:** `internal/store/sqlitestore/users.go` — `userCols`, Create/scan/Update.
- **Context:** `internal/store/context.go` — `TimezoneKey` + `WithTimezone`/`TimezoneFromContext` (mirror locale).
- **WS transport:** `internal/gateway/router.go` — `connect` `timezone` param, `Client.timezone`, `handleConnect` set + persist-on-change, per-request `WithTimezone`. `internal/gateway/client.go` — `timezone` field.
- **HTTP transport + write:** `internal/http/auth.go` — `X-GoClaw-Timezone` → `enrichContext` → `WithTimezone`; auth-middleware backfill from user row. `internal/http/users.go` — `PATCH /v1/users/me` accepts `timezone` (validate IANA). First-seen literals at `oidc_handler.go:281`, `auth_handler.go:277`, `tenants.go:377`.
- **Prompt assembly:** `internal/agent/systemprompt.go` + `systemprompt_sections.go` — effective-tz into `SystemPromptConfig`, `buildTimeSection` prefers it; relative-time guidance + "learn tz" copy reconcile. `internal/agent/preview_prompt.go:246` resolves effective tz (FR-01).
- **Channel/loop:** `internal/agent/loop_history.go` — `USER.md` `Timezone:` parse into per-turn context tz; conditional ask-hint (`:99`); `loop_context.go:437` threads effective tz.
- **Tool:** `internal/tools/datetime.go:44-54` — prefer per-turn tz over `RunContext.DefaultTimezone`. Optionally carry resolved tz on `RunContext` (`internal/store/run_context.go:51`).
- **Frontend:** `ui/web` + `ui/desktop` — send browser IANA tz in `connect`.
- **No new canonical error code** (tz handling graceful; invalid → warn + fallback).

## 5. Test Plan

- Unit: `WithTimezone`/`TimezoneFromContext` round-trip + `""` default (`internal/store/context_test.go`, mirror locale test).
- Unit: `buildTimeSection` effective-tz → system → UTC; invalid effective tz warns + falls back (extend `internal/agent/systemprompt_cache_test.go` `TestTimeSection*`, l. 40-60).
- Migration: `000089` up/down on PG; SQLite patch `47` + `SchemaVersion 48` applies cleanly (existing DB + fresh DB); desktop (`sqliteonly`) starts without crash.
- Store: `UserData.Timezone` round-trip on PG + SQLite (Create/Get/Update; null vs value).
- Handler: `PATCH /v1/users/me {timezone}` persists valid; rejects invalid IANA (400/422).
- Transport: WS `connect` with `timezone` sets `client.timezone` + persists-on-change + injects `WithTimezone`; HTTP `X-GoClaw-Timezone` likewise; auth-middleware backfill from user row when no header.
- Channel: a turn whose loaded `USER.md` has `Timezone: Asia/Jakarta` resolves the time section in `Asia/Jakarta`; no line → system default.
- Tool: `DateTimeTool` returns effective-tz local time when present, system default otherwise (`internal/tools/datetime_test.go`).
- Hint: ask-hint suppressed when tz known (auth persisted / channel USER.md), retained when unknown.
- Integration: WS chat with `timezone=Asia/Ho_Chi_Minh` → system prompt time section shows that tz; agent answers "what day is tomorrow" in that tz. Channel DM: write `Timezone:` to USER.md → next turn time section honors it.
- Manual: web + desktop send tz on connect (inspect `connect` frame); agent in a non-system-default tz answers relative-time questions correctly.

## 6. Risks and Open Questions

| Risk or question | Draft decision |
|------------------|----------------|
| Should tz be **persisted** (chosen) vs ephemeral? | Persisted (this SRS). Durable across reconnect/clients; transport writes it; auth middleware backfills from the user row. |
| **Channel identity unification** (sender→users) instead of USER.md? | Deferred (out of scope). Channels use USER.md-learned path (FR-05). Unification would let channels read `users.timezone` but is a major subsystem (channel_contacts/tenant_users schema + resolver rewrite). |
| Parsing `Timezone:` from freeform `USER.md` is fragile. | Mitigate by specifying an exact line format in the hint (FR-05) and tolerant regex; invalid/absent → system default (safe fallback). |
| Persist-on-change on every WS connect = extra write. | Write only when value differs from persisted (or persisted null). Idempotent reconnects → no write. |
| Auth-middleware tz backfill requires the user row already loaded. | Verify the auth path already fetches the `users` row (it does for auth); reuse it — no new query. If not trivially available, read tz in the same existing lookup. |
| `tenant_users.user_id` is VARCHAR with no FK to `users` — does any merged channel contact ever reach a real `users` row? | No (verified). `channel_contacts.merged_id` is untyped UUID with no FK (`migrations/000014:14`). Channels therefore cannot reach `users.timezone`; USER.md path is the correct choice. |
| Frontend `useUiStore.timezone` default when browser denies tz. | Empty/`""` = unknown → backend falls back to system default. |
| Stale cached system-prompt prefix serving wrong datetime? | No — time section is below the cache boundary (`systemprompt.go:488`); `time.Now()` fresh every turn; placement preserved. |
| The model may not actually **verify** temporal claims (e.g. agrees with "good morning" said at 21:37). | Accepted as a known limit of the prompt-injection approach. This SRS delivers the *information* layer (model knows the real now/tz), not *enforcement*. Deterministic checking is a deferred layer — see FR-09 (options A/B/C). Ship **A** (stronger mandate line) as the cheap first step if correction quality is insufficient. |

## 7. Implementation Plan

1. **Migrations + version:** PG `000089_user_timezone.{up,down}.sql`; SQLite `schema.sql` + `schema.go` patch `47` + `SchemaVersion 48`; `version.go` `RequiredSchemaVersion 89`.
2. **Model + store:** `UserData.Timezone *string`; PG `users.go` (Create/Get*/List/Update/scan); SQLite `users.go` (`userCols`/Create/scan/Update).
3. **Write path:** `PATCH /v1/users/me` accepts + validates `timezone` (`users.go:73`); first-seen literals (`oidc_handler.go:281`, `auth_handler.go:277`, `tenants.go:377`).
4. **Context + transport:** `context.go` `TimezoneKey`/`WithTimezone`/`TimezoneFromContext`; WS `connect` param + `Client.timezone` + persist-on-change + per-request `WithTimezone` (`router.go`); HTTP `X-GoClaw-Timezone` + `enrichContext` (`auth.go`); auth-middleware backfill from user row.
5. **Resolution:** `SystemPromptConfig.UserTimezone`; `buildTimeSection` effective → system → UTC; close preview gap (`preview_prompt.go:246`).
6. **Channel path:** `USER.md` `Timezone:` parse → per-turn context tz (`loop_history.go`); update hint to exact line format.
7. **Agent reasoning + tool:** relative-time guidance copy; `DateTimeTool` prefers per-turn tz (`datetime.go:44-54`).
8. **Conditional hint:** suppress ask-hint when tz known (auth persisted / channel USER.md); reconcile `systemprompt.go:317/337` copy.
9. **Frontend:** web + desktop send browser tz in `connect`.
10. **Tests + verification:** per §5.
11. **Checklist:** `go build ./...`, `go build -tags sqliteonly ./...`, `go vet ./...`, `pnpm build` (`ui/web` + `ui/desktop/frontend`).

## 8. Proposed Error Codes

No new canonical error codes. Timezone handling is **graceful by design** — invalid/absent tz always falls back (effective → system → UTC) and never fails a request. The only user-facing validation is on the explicit `PATCH /v1/users/me` write:

| Code | Meaning |
|------|---------|
| `request.validation_failed` | `PATCH /v1/users/me` `timezone` is not a `time.LoadLocation`-parseable IANA name (existing `ErrInvalidRequest` 400/422 path). Reuse; no new code. |
| _(none — runtime fallback)_ | An invalid/absent on-wire or USER.md tz is ignored at runtime with `slog.Warn` (extends the existing `agent.invalid_default_timezone` warn at `systemprompt_sections.go:341-343`) and falls back to system default / UTC. No error surfaced to the user. |

## 9. Closure Status & Outstanding Items

**Status: Implemented (closed for this release).** FR-00–FR-08 are built + `go build`/`go build -tags sqliteonly`/`go vet` green; agent time-tests pass (`TestTimeSection*`, `TestParseTimezoneLine`, `TestTimeOfDayBucket`, `TestTimeSectionBucketAppended`, `TestTimezoneContext`). FR-09 option A (mandate + part-of-day bucket) shipped — **soft** enforcement. ACs marked `[x]` done; `[~]` code-complete pending live/manual verification.

**Outstanding (not blocking closure):**

1. **Live migration apply** — run `./goclaw migrate up` (master PG → `000089`) + confirm SQLite patch `47`/`SchemaVersion 48` applies on a running desktop DB. Master-only; **no tenant-DB migration** (users is master-global — FR-02).
2. **Desktop frontend build** — `pnpm install` + `pnpm build` in `ui/desktop/frontend` (env-blocked locally; `ws.ts` change is a one-liner mirroring the web client, web build green).
3. ~~Provider eval~~ — **done (v1.1)**: glm-5-turbo corrected "good morning"→"good evening" + named tomorrow correctly. (Soft: single provider measured; broader per-provider sweep optional.)
4. **FR-09 B/C descoped** (v1.1) — option A (mandate + part-of-day bucket) retained as the chosen enforcement level. Revisit B (deterministic input guard) / C (mandatory tool gate) only if A proves insufficient across providers.
5. **Optional durability follow-ups** — gateway `SetUserStore` for auto-persist-on-connect; HTTP auth-middleware backfill from `users.timezone`. Both durability-only — the prompt already receives the per-turn tz on the wire.
6. **Pre-existing unrelated test failure** — `TestKGTraversal_Tier1_CappedAt20` (KG traversal cap; code untouched by this feature) — not a regression.
