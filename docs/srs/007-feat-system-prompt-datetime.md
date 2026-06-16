# Software Requirements Specification: Current Datetime + Per-User Timezone in the Default System Prompt

**Project**: GoClaw Gateway
**Release**: 2026.3.0
**Version**: 0.1-draft
**Date**: 2026-06-16
**Status**: Draft
**Difficulty**: Low
**Estimate**: 0.5 days

---

## Revision History

| Version | Date | Changes |
|---------|------|---------|
| 0.1-draft | 2026-06-16 | Initial draft. Scopes the feature branch `feat/add-datetime-to-system-prompt`. Verified against live code: a current-datetime section (`buildTimeSection`) is already injected into every system prompt, but it uses the **system-wide** `cron.default_timezone` — there is **no per-user timezone** propagation today, so the agent cannot contextualize relative-time questions ("yesterday", "next Monday") against the user's own timezone. The new work = propagate the user's timezone to the backend (mirroring the existing `locale` pattern) and make the time section + datetime tool use it. Covers both stated requirements: (1) datetime in the default prompt, (2) agent understanding the context between a question and the current datetime. |

---

## 1. Summary

The agent system prompt must always carry the **current date/time** so the model knows "now", and it must carry it in the **requesting user's timezone** so the agent can interpret relative-time references ("today", "yesterday", "tomorrow at 9am", "next Monday", "in 2 hours") correctly. This SRS defines that feature.

**Current state (verified).** A current-datetime section already exists and is injected into every system prompt (all modes except `none`): `buildTimeSection` (`internal/agent/systemprompt_sections.go:330-346`) emits a line of the form `Current date/time: 2026-06-16 Monday 12:34 (UTC) / 2026-06-16 Monday 19:34 (Asia/Ho_Chi_Minh)` and is appended in `BuildSystemPrompt` (`internal/agent/systemprompt.go:488-491`, section 8, deliberately placed below the cache boundary so a changing date does not bust the stable cached prefix). The timezone it renders is the **system-wide** default `cron.default_timezone`, plumbed through `SystemPromptConfig.DefaultTimezone` (`systemprompt.go:164-166`) and `RunContext.DefaultTimezone` (`internal/store/run_context.go:51`), resolved live from the `SystemConfigs` store (`internal/agent/resolver.go:638-648`). This was committed in `1dc1e28e` ("add local timezone support to system prompt time section") and `5be7a1cf` ("integrate run-context aware default timezone"); the feature branch (`feat/add-datetime-to-system-prompt`) has no commits ahead of `dev` yet — the work is greenfield.

**The gap.** There is **no per-user timezone** anywhere on the backend:

- The WS `connect` handshake params (`internal/gateway/router.go:142-150`) carry `locale` but **no timezone**; the `Client` struct (`internal/gateway/client.go`) has a `locale` field but no timezone field.
- HTTP requests reach locale only via the `Accept-Language` header (`internal/http/auth.go:450, 565-568`); there is no timezone header.
- There is **no** `TimezoneKey` context constant and **no** `WithTimezone`/`TimezoneFromContext` helper — `internal/store/context.go` defines `LocaleKey` (l. 28) and its helpers (l. 348-358) but no timezone sibling.
- The web UI **does** hold the user's timezone (`useUiStore.timezone`, sourced from `Intl.DateTimeFormat().resolvedOptions().timeZone` — an IANA name) but it is **display-only** and is **never sent** to the gateway (confirmed absent from connect params and all HTTP request structs).
- Consequently the agent is *instructed* to learn the timezone by asking the user and writing it into `USER.md` (`internal/agent/loop_history.go:99` — "Timezone: not yet known. When the user mentions times, schedules, or reminders, ask for their timezone and update USER.md."; plus prompt copy at `systemprompt.go:317, 337`).

So requirement (1) — datetime in the default prompt — is **largely satisfied**; the new work **formalizes** it (incl. closing a preview-path gap, FR-01) and **makes it per-user** (FR-02/FR-03). Requirement (2) — the agent understanding the context between a question and the current datetime — is **not satisfied today**: without the user's timezone the agent can only reason in UTC / the system default, so it cannot reliably answer "what day is tomorrow for me" or "remind me at 9am". FR-03/FR-04/FR-05 deliver that.

This SRS owns: per-user timezone transport (mirroring `locale`), the time-section resolution order, the agent's relative-time reasoning guidance, and the conditional removal of the "ask for timezone" hint. It composes with the existing bootstrap/prompt assembly in `internal/agent/` and does not duplicate any prior SRS scope.

## 2. Scope

**In scope**:

- Making the current date/time section a guaranteed part of the default system prompt for every agent turn (open + predefined, full + compact; formalizes existing behavior + closes the preview-path gap — FR-01).
- Propagating the **requesting user's timezone** from client to backend, mirroring the existing `locale` propagation exactly: a WS `connect` param, an HTTP header, and a new request-scoped context key + helpers (FR-02).
- Resolving the timezone shown in the time section as **per-user tz → system default → UTC**, with graceful fallback (FR-03).
- Making the agent **contextualize relative-time references** against the injected current datetime in the user's timezone, via strengthened prompt copy + the `datetime` tool using the per-user tz (FR-04).
- Making the existing "ask the user for their timezone" hint **conditional** — only when the timezone is unknown (FR-05).

**Out of scope**:

- **Persisting** the timezone to a `users.timezone` column or other DB table. The feature follows the `locale` precedent: timezone is request-scoped (sent by the client each connect/request), not a persisted preference. A persisted-preference follow-up is recorded as an open question (§6), not built here.
- **Channel ingestion** paths (Telegram, WhatsApp, Feishu/Lark, Zalo, Discord). Those message sources have no browser client and therefore no WS `connect`; their timezone sourcing is a separate concern. They continue to use the system default / per-channel tz. Recorded as an open question (§6).
- Calendar, scheduling, booking, or reminder *business logic*. This feature only ensures the agent **knows** the correct now/user-tz; it does not build scheduling features on top.
- Changing the timezone format (it stays human-readable `2006-01-02 Monday 15:04`, intentionally — not ISO/RFC3339 — to stay LLM-friendly and cache-stable).
- The `cron.default_timezone` system config itself (consumed unchanged as the fallback).

## 3. Functional Requirements

### FR-00: Current State — datetime injection already exists (verification + gap, no feature code)

The current-datetime section is already injected into the system prompt. This FR records the verified baseline so the feature is scoped as an *extension* (per-user tz + reasoning guidance), not a from-scratch injection.

| Claim | Evidence |
|-------|----------|
| Time section injected every turn | `buildTimeSection` (`systemprompt_sections.go:330-346`), appended in `BuildSystemPrompt` at `systemprompt.go:488-491`, gated `!isNone` (all modes except `none`) |
| Timezone source is system-wide only | `SystemPromptConfig.DefaultTimezone` (`systemprompt.go:164-166`) ← `cron.default_timezone` (`resolver.go:638-648`) |
| No per-user tz on the wire | WS connect params have `locale` but no tz (`router.go:142-150`); `Client` has `locale` field, no tz field (`client.go`); HTTP has `Accept-Language`, no tz header (`auth.go:450,565`) |
| No tz context key | No `TimezoneKey`/`WithTimezone`/`TimezoneFromContext` in `internal/store/context.go` (only `LocaleKey` + helpers, l. 28/348-358) |
| Agent told to *ask* for tz | `loop_history.go:99` hint + prompt copy `systemprompt.go:317,337` |
| UI tz never sent to backend | `useUiStore.timezone` is display-only (`message-bubble.tsx` formatting); absent from connect payload + all HTTP request structs |

Acceptance criteria:

- [ ] Verified: `buildTimeSection` injects `Current date/time: … (UTC) / … (<tz>)` on every turn (read `systemprompt_sections.go:330-346`).
- [ ] Verified: no per-user timezone reaches the backend today (grep `TimezoneKey|WithTimezone` in `internal/store/context.go` → 0 matches; grep `timezone` in `router.go` connect params → 0 matches).
- [ ] No code is written for this FR — it is the baseline record.

---

### FR-01: Current Datetime Is Part of the Default System Prompt (formalize)

The system prompt for every agent turn — both agent types (`open` per-user context, `predefined` shared + `USER.md`), and every prompt mode except `none` — must include a current date/time section. This formalizes existing behavior and closes one gap: the **preview** path (`BuildPreviewPrompt`, `internal/agent/preview_prompt.go:246`) does not set `DefaultTimezone`, so a previewed prompt renders the time section UTC-only (or omits the resolved tz). The preview must resolve the same timezone as the live path.

Acceptance criteria:

- [ ] `BuildSystemPrompt` always appends the time section except in `none` mode (already `systemprompt.go:488-491` `if !isNone` — keep).
- [ ] The section applies to both `open` and `predefined` agent types (the section is type-agnostic; verified — it depends only on the timezone, not `cfg.AgentType`).
- [ ] `BuildPreviewPrompt` resolves and passes the effective timezone (per-user → system default → UTC) so the previewed time section matches the live prompt (closes the `preview_prompt.go:246` gap).
- [ ] The section stays below the cache boundary (section 8, `systemprompt.go:488`) — a changing date must not bust the stable cached prefix (existing intentional placement, preserved).

---

### FR-02: Per-User Timezone Transport (mirror the `locale` pattern)

The requesting user's timezone must reach the backend per-request, exactly as `locale` does today. Implementation mirrors the locale siblings 1:1 so the new path is conventional and reviewable.

| Transport | `locale` (existing) | `timezone` (NEW) |
|-----------|---------------------|------------------|
| WS handshake param | `locale` (`router.go:146`) | `timezone` IANA name, e.g. `Asia/Ho_Chi_Minh` |
| WS client field | `client.locale` set at `router.go:156` | `client.timezone` set in `handleConnect` |
| Per-request context injection | `store.WithLocale(ctx, …)` at `router.go:105` | `store.WithTimezone(ctx, …)` at the same per-request site |
| HTTP header | `Accept-Language` (`auth.go:565`) | `X-GoClaw-Timezone` |
| HTTP context injection | `store.WithLocale` in `enrichContext` (`auth.go:450`) | `store.WithTimezone` in `enrichContext` |
| Context key + helpers | `LocaleKey` + `WithLocale`/`LocaleFromContext` (`context.go:27, 348-358`) | `TimezoneKey` + `WithTimezone`/`TimezoneFromContext` (NEW, same shape) |

Value contract: the timezone is an **IANA timezone name** (`time.LoadLocation`-parseable). The client supplies it from the browser (`Intl.DateTimeFormat().resolvedOptions().timeZone`). An empty/absent value is valid and means "unknown" (fall through to system default, FR-03). An **invalid** value (not a real IANA name) must be ignored with a warning, never cause a request failure.

Acceptance criteria:

- [ ] WS `connect` accepts a `timezone` param (`router.go` params struct + `Client.timezone` field), stored on the connection like `locale`.
- [ ] Every WS request injects `store.WithTimezone(ctx, client.timezone)` alongside the existing `store.WithLocale` (`router.go:105` region).
- [ ] HTTP requests accept `X-GoClaw-Timezone` and inject it via `enrichContext` (`auth.go:450` region), alongside `Accept-Language`.
- [ ] `internal/store/context.go` adds `TimezoneKey` + `WithTimezone`/`TimezoneFromContext` mirroring the `LocaleKey`/`WithLocale`/`LocaleFromContext` shape (the helper returns `""` when unset, unlike locale which defaults to `"en"` — empty = unknown).
- [ ] An invalid IANA timezone (`time.LoadLocation` fails) is ignored with `slog.Warn` and does not error the request.
- [ ] Frontend (web `ui/web` + desktop `ui/desktop`) sends the user's IANA timezone in the WS `connect` payload (from `useUiStore.timezone` / `Intl.DateTimeFormat().resolvedOptions().timeZone`).

---

### FR-03: Time Section Resolves Per-User tz → System Default → UTC

The timezone rendered in the time section must be the **user's** timezone when available, falling back to the system default, then UTC. Today `buildTimeSection(defaultTimezone)` (`systemprompt_sections.go:330`) takes only one tz; it must receive the per-user tz as the primary, with `cron.default_timezone` as fallback.

Resolution order (first non-empty, `LoadLocation`-valid wins):

```text
userTimezone (from context)  →  cron.default_timezone (system)  →  UTC
```

The rendering stays the existing two-value form when a tz is known — UTC always, plus the local value with the tz name — so the model sees both an absolute anchor (UTC) and the user's local frame:

```text
Current date/time: 2026-06-16 Monday 12:34 (UTC) / 2026-06-16 Monday 19:34 (Asia/Ho_Chi_Minh)
```

Acceptance criteria:

- [ ] With a per-user timezone in context, the time section renders the **user's** local date/time + tz name (not the system default).
- [ ] With no per-user timezone but a system default set, behavior is unchanged from today (system tz rendered).
- [ ] With neither, the section renders UTC-only (unchanged `systemprompt_sections.go:333` path).
- [ ] An invalid per-user tz (passed but not `LoadLocation`-valid) falls back to the system default and logs `slog.Warn` (mirrors the existing warn at `systemprompt_sections.go:341-343`) — no panic, no request failure.
- [ ] The effective timezone is threaded through `SystemPromptConfig` (e.g. a `UserTimezone` field alongside `DefaultTimezone`, `systemprompt.go:164-166`) and/or resolved upstream where `cfg.DefaultTimezone` is set today (`internal/agent/loop_history.go:254` region), keeping the single-source principle.

---

### FR-04: Agent Contextualizes Relative-Time References (requirement 2)

The agent must interpret relative and deictic time references in the user's question against the injected current datetime **in the user's timezone**. Two mechanisms deliver this:

1. **Prompt guidance.** The time section and the `datetime`-tool hint (`systemprompt.go:197` — "Get current date/time with timezone — use before …") must state that relative expressions ("now", "today", "yesterday", "tomorrow", "next/last <weekday>", "in N hours/days", "this morning/afternoon") resolve relative to the **displayed current date/time in the user's timezone**, not UTC. The agent should compute the referenced instant accordingly before answering or scheduling.
2. **`datetime` tool uses the user's tz.** `DateTimeTool.Execute` (`internal/tools/datetime.go:44-54`) today falls back to `RunContext.DefaultTimezone` (system). It must prefer the per-user timezone from the request context (via the new `TimezoneFromContext`, or a `RunContext` field populated from it), so a tool call returns the user's local time — directly supporting correct relative computation.

Acceptance criteria:

- [ ] Given the current datetime in the user's tz in the prompt, the agent answers "what day is tomorrow?" / "what's the date next Monday?" correctly for the user's timezone (manual / eval).
- [ ] `DateTimeTool` returns the user's local time when a per-user tz is present, system default when absent (unit test).
- [ ] The time section / tool-hint copy explicitly instructs relative-time resolution against the shown (user-tz) current datetime (no ambiguity with UTC).
- [ ] No new tool is added — the existing `datetime` tool is reused (`internal/tools/datetime.go`).

---

### FR-05: "Ask for Timezone" Hint Becomes Conditional

The bootstrap hint that tells the agent to ask the user for their timezone (`loop_history.go:99`) and the prompt copy that has the agent "naturally learn … timezone" (`systemprompt.go:317, 337`) exist **because** the backend did not have the tz. Once the per-user tz is provided on the wire (FR-02), that hint is redundant and can cause the agent to pester users who already have a known tz. The hint must become conditional: shown only when the timezone is **unknown** (not present in context).

Acceptance criteria:

- [ ] When a per-user timezone is present in the request context, the `loop_history.go:99` hint is **not** injected (the agent already knows the tz).
- [ ] When the timezone is unknown (no `timezone` on the wire), the existing hint + "learn timezone" copy remain (fallback to asking — unchanged behavior).
- [ ] The prompt copy at `systemprompt.go:317/337` is reconciled so it does not instruct the agent to ask for a tz it was already given.

---

### FR-06: i18n (no new UI strings expected)

This feature changes request transport and prompt content, not user-facing UI text. The timezone is data on the wire, not a translated string. No new i18n keys are expected. If any user-facing string is added, it follows the 3-locale rule (`en`/`vi`/`zh`) per the Mobile/UI i18n rule and `004-feat-roles-page.md` FR-06.

Acceptance criteria:

- [ ] No new raw keys appear in the UI as a side effect of this change.
- [ ] Any added string is present in all 3 locales (`ui/web/src/i18n/locales/{en,vi,zh}/`).

## 4. System Impact

- **Backend — context:** `internal/store/context.go` — add `TimezoneKey` + `WithTimezone`/`TimezoneFromContext` (mirror `LocaleKey`/`WithLocale`/`LocaleFromContext`, l. 27/348-358). Returns `""` when unset (unlike locale's `"en"` default — empty = unknown).
- **Backend — WS:** `internal/gateway/router.go` — add `timezone` to the `connect` params struct (l. 142-150), a `timezone` field on `Client` (`client.go`, mirror `locale` l. 30), set it in `handleConnect` (l. 156 region), and inject `store.WithTimezone` per request alongside `store.WithLocale` (l. 105 region).
- **Backend — HTTP:** `internal/http/auth.go` — read `X-GoClaw-Timezone` in `extractLocale`'s sibling / `enrichContext` (l. 450 region) and inject `store.WithTimezone`.
- **Backend — prompt assembly:** `internal/agent/systemprompt.go` + `internal/agent/systemprompt_sections.go` — thread the per-user tz into the time section; `buildTimeSection` resolves per-user → system → UTC (FR-03); strengthen relative-time guidance + reconcile "learn timezone" copy (FR-04/FR-05). `internal/agent/preview_prompt.go:246` resolves the effective tz (FR-01 preview gap).
- **Backend — hint:** `internal/agent/loop_history.go:99` — make the "ask for timezone" hint conditional on tz being unknown (FR-05).
- **Backend — tool:** `internal/tools/datetime.go:44-54` — prefer per-user tz from context over `RunContext.DefaultTimezone` (FR-04). Optionally carry the resolved tz on `RunContext` (`internal/store/run_context.go:51`) as the single source the tool reads.
- **Frontend:** `ui/web` + `ui/desktop` — send the user's IANA timezone in the WS `connect` payload (from `useUiStore.timezone` / `Intl.DateTimeFormat().resolvedOptions().timeZone`). No persisted-preference change.
- **No schema migration** (PG or SQLite), no store-method change, no startup hook, no new canonical error code.

## 5. Test Plan

- Unit: `WithTimezone`/`TimezoneFromContext` round-trip + default `""` (`internal/store/context_test.go`, mirror the locale test).
- Unit: `buildTimeSection` resolves per-user tz → system default → UTC; invalid user tz warns + falls back to system (extend `internal/agent/systemprompt_cache_test.go` `TestTimeSectionWithTimezone`/`TestTimeSectionWithoutTimezone`, l. 40-60).
- Unit: WS `connect` with `timezone` sets `client.timezone`; requests inject `TimezoneFromContext` correctly (router test).
- Unit: HTTP handler with `X-GoClaw-Timezone` header injects tz into context (auth test).
- Unit: `DateTimeTool` returns the user's local time when context tz present, system default otherwise (`internal/tools/datetime_test.go`).
- Unit: `loop_history.go` hint is suppressed when tz is known, present when unknown.
- Integration: a chat turn where the client connects with `timezone=Asia/Ho_Chi_Minh` produces a system prompt whose time section shows that tz (not the system default); the agent answers a "what day is tomorrow" question in that timezone.
- Manual: web + desktop send tz on connect (inspect the `connect` frame); an agent in a non-system-default tz answers relative-time questions correctly.

## 6. Risks and Open Questions

| Risk or question | Draft decision |
|------------------|----------------|
| Should the timezone be **persisted** (e.g. a `users.timezone` column) so it survives reconnect / is consistent across clients, rather than request-scoped? | Out of scope here. Follow the `locale` precedent (request-scoped, client-supplied). A persisted-preference follow-up can reuse the same context plumbing — add a store read that pre-fills `TimezoneFromContext` when the client omits it. Defer. |
| **Channel ingestion** (Telegram/WhatsApp/Feishu/Lark/Zalo/Discord) has no browser client → no WS `connect`. Where does the per-user tz come from there? | Out of scope. Channels continue to use the system default / per-channel tz. Channel senders that carry a tz (e.g. a learned value in `USER.md`) can be wired later via the same context key. Recorded, not built. |
| Header name choice for HTTP. | `X-GoClaw-Timezone`, mirroring the existing `X-GoClaw-Tenant-Id` vendor-header convention. |
| Does removing the "ask for timezone" hint (FR-05) regress the `USER.md`-learning UX for users who never set a browser tz? | No — the hint stays when tz is **unknown**. Only clients that already supply a tz stop being asked. |
| Could a stale cached system-prompt prefix serve a wrong datetime? | No — the time section is intentionally below the cache boundary (`systemprompt.go:488`), so `time.Now()` is fresh every turn; this change preserves that placement. |
| Frontend `useUiStore.timezone` default when the browser denies tz resolution. | Empty/`""` = unknown → backend falls back to system default (FR-03). No error path. |

## 7. Implementation Plan

1. **Context plumbing:** add `TimezoneKey` + `WithTimezone`/`TimezoneFromContext` to `internal/store/context.go` (mirror `LocaleKey`/`WithLocale`/`LocaleFromContext`). Returns `""` when unset.
2. **WS transport:** `router.go` — `timezone` in `connect` params, `Client.timezone` field, set in `handleConnect`, inject `store.WithTimezone` per request (FR-02).
3. **HTTP transport:** `auth.go` — read `X-GoClaw-Timezone` in `enrichContext`, inject `store.WithTimezone` (FR-02).
4. **Time section resolution:** thread per-user tz into `SystemPromptConfig`; `buildTimeSection` resolves per-user → system → UTC; close the preview-path gap in `preview_prompt.go` (FR-01/FR-03).
5. **Agent reasoning:** strengthen the time-section / `datetime`-tool-hint copy for relative-time resolution; `DateTimeTool` (`datetime.go:44-54`) prefers per-user tz (FR-04).
6. **Conditional hint:** make the `loop_history.go:99` "ask for timezone" hint conditional on tz unknown; reconcile `systemprompt.go:317/337` copy (FR-05).
7. **Frontend:** web + desktop send `timezone` in the WS `connect` payload (FR-02).
8. **Tests + verification:** per §5.
9. **Checklist:** `go build ./...`, `go build -tags sqliteonly ./...`, `go vet ./...`, and `pnpm build` in `ui/web` (and `ui/desktop/frontend`).

## 8. Proposed Error Codes

No new canonical error codes are introduced. Timezone handling is **graceful by design** — an invalid or absent timezone always falls back (per-user → system → UTC) and never fails a request. The existing warning channel covers the invalid case:

| Code | Meaning |
|------|---------|
| _(none — new)_ | No new error code. An invalid IANA timezone reuses the existing `slog.Warn("agent.invalid_default_timezone", …)` path (`systemprompt_sections.go:341-343`), extended to cover an invalid _user_ timezone, then falls back to the system default / UTC. |
