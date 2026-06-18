# Software Requirements Specification: Sessions Page "Settings" Menu Cannot Be Saved

**Project**: GoClaw Gateway
**Release**: 2026.3.0
**Version**: 0.5-draft
**Date**: 2026-06-18
**Status**: Closed — implemented, build/vet/test-verified, and **live-verified on the master tenant** (operator confirmed: Sessions Settings save persists + WS `connect` `is_owner:true`). HTTP-side owner-recognition follow-up remains tracked separately (§10.3) but is out of scope for this bug (reported WS path fixed).
**Difficulty**: Low–Medium
**Estimate**: 0.5–1 days

---

## Revision History

| Version | Date | Changes |
|---------|------|---------|
| 0.1-draft | 2026-06-18 | Initial investigation. The "Settings" gear popover on `/t/{tenant}/sessions` edits `agents.defaults.compaction` (`autoCompactThreshold` + `keepLastMessages`) and saves via WS `config.patch`. Three confirmed root causes traced against code: RC1 silent client-side validation no-op (`sessions-page.tsx:48-54`), RC2 owner-only authorization gate (`config.go:41`/`:50-62` — tenant admins/owners rejected), RC3 `omitempty` on `AutoCompactThreshold` so the documented "0 = disabled" value never round-trips (`config.go:194`, `Hash()` `config_load.go:317`). Method names, config struct path, and i18n namespace all verified matching — none of those is the bug. Chosen fix per RC + verification plan recorded; acceptance boxes left `[ ]` pending implementation + live repro. |
| 0.2-draft | 2026-06-18 | **RC2 confirmed as the live cause** (operator reports they are the "SYSTEM user"). Verified the identity split: web email login sets `userId = body.user.id` (a UUID, `email-login-form.tsx:64`), but owner detection `isOwnerID(userID, OwnerIDs)` (`router.go:470`) recognizes owners ONLY from `gateway.owner_ids` — which is loaded **exclusively** from env `GOCLAW_OWNER_IDS` (`config_load.go:211-219`), never seeded — and falls back to matching the **literal string `"system"`** when that env is unset (`router.go:474-476`). The web-login UUID matches neither → not recognized as the global/platform owner → `client.role != RoleOwner` → `requireOwner` (`config.go:52`) rejects `config.patch`. The literal `"system"` is a *different* identity (agents' `owner_id=system`, `cmd/agent_chat_client.go:85`, the gateway-token/system path), not the human web-login user. Promoted FR-02 to the primary fix: broaden owner detection so the master/system owner (web login) is recognized, plus the immediate operator workaround (`GOCLAW_OWNER_IDS=<UUID>`). RC1/RC3 unchanged (still real latent bugs). |
| 0.3-draft | 2026-06-18 | **Implemented (code-complete + build/vet/test-verified).** RC3: dropped `omitempty` on `AutoCompactThreshold` + `KeepLastMessages` (`config.go`); round-trip test `compaction_omitempty_test.go` (2 cases) green. RC1: `sessions-page.tsx` — derived `settingsValid`, inline error text, Save disabled while invalid, `await patchConfig` with `catch`; inputs now `text-base md:text-sm` (mobile no-zoom rule). RC2b: Settings popover gated to `useRole().isOwner`; non-owners get a disabled button with `ownerOnlyHint`. i18n: 3 keys added to `sessions.json` en/vi/zh. **RC2 deviation from §4/§8:** implemented connect-recognition (master-tenant owner → `RoleOwner` in `router.go` `sendConnectResponse`, via existing `TenantStore.IsOwner(master, userID)`) instead of startup-seeding `owner_ids`. Lower risk: no `TenantStore` interface change (SQLite has no owner-list method — startup-seed would have broken the `sqliteonly` build), no startup reorder, no `permPE`/`httpapi` re-feed. `client.role=RoleOwner` makes every role-based check (`requireOwner`, `requireMasterScope` via ctx role, `canSeeAll`) pass. See §11. **Verification:** `go build` (PG + `sqliteonly`) ✓, `go vet` ✓, config tests 59/59 ✓ (incl. the 2 new RC3 cases), `pnpm build` (ui/web) ✓. **Pre-existing failures (NOT caused by this change — fail identically on the clean tree):** `TestClientCanReceiveEvent_AdminOnlyEvent_BlockedForNonAdmin` (event classification, calls `clientCanReceiveEvent` not my `sendConnectResponse`) + `TestUserPermission_RouteGuardsAreWritePermissions` (`user.unenroll` catalog drift, SRS 004). **Still pending:** live confirm of the Sessions save + `connect` `is_owner:true` on the master tenant; HTTP-side `permPE.IsOwner(userID)`/`httpapi` owner set still do not recognize the web-login UUID (not the reported WS path — follow-up). |
| 0.4-draft | 2026-06-18 | Operator-requested scope tweak: raised the `keepLastMessages` cap 20 → 1000 (sessions Settings input `max` + validation guard + `invalidKeepLast` i18n en/vi/zh). Frontend-only; no backend cap exists (`KeepLastMessages int` is unvalidated). `pnpm build` ✓. Note: keepLast=1000 keeps 1000 messages after compaction (default 4) — compaction barely trims history at that value; operator-confirmed intent. |
| 0.5-draft | 2026-06-18 | **Live-verified + Closed.** Operator confirmed on the master tenant: Sessions Settings save persists + WS `connect` reports `is_owner:true`. Flipped the RC2 live-confirm box to `[x]`; Status → Closed. The HTTP-side owner-recognition follow-up (§10.3) stays open as a separate task — not this bug. |

---

## 1. Summary

The **Settings** gear popover on the Sessions page (`/t/{tenant}/sessions`, `ui/web/src/pages/sessions/sessions-page.tsx:105-159`) edits the global compaction settings — `autoCompactThreshold` (0–1, `0` = disabled) and `keepLastMessages` (1–20) — and persists them through WS `config.patch`. Reports that **the menu "cannot be saved"**.

This is **not** a single defect. Investigation against the live code surfaces **three independent root causes**, any one of which produces "save does nothing / my value does not stick":

- **RC1 — Silent client-side validation no-op (UX).** `handleSaveSettings` (`sessions-page.tsx:48-54`) wraps the save in a numeric-range guard (`threshold ∈ [0,1]`, `keepLast ∈ [1,20]`, both parseable). When the guard fails (empty field, out-of-range, or non-numeric input), it **silently returns without calling the API, with no toast, no error, and the button stays clickable**. From the user's seat this is indistinguishable from "Save is broken." The Save button is also not disabled by validation (`sessions-page.tsx:145-150` disables only on `configSaving`), so an invalid value looks exactly like a valid one until clicked.
- **RC2 — Owner-only authorization gate. CONFIRMED LIVE CAUSE** (operator is the "SYSTEM user" on web email login). WS `config.patch` is registered as `requireMasterScope(requireOwner(m.handlePatch))` (`internal/gateway/methods/config.go:41`). `requireOwner` (`config.go:50-62`) rejects any caller whose WS client role is not the **global** `RoleOwner` with `ErrUnauthorized` (`MsgPermissionDenied`). The defect is an **identity split**:
  - Web email login stores `userId = body.user.id` — the user's **UUID** (`ui/web/src/pages/login/email-login-form.tsx:64`), carried into the WS `connect` (`ui/web/src/components/providers/ws-provider.tsx:27-28`) and onto `client.userID` (JWT Path 1c, `router.go:265`).
  - Owner detection `isOwnerID(userID, OwnerIDs)` (`router.go:470-478`) recognizes owners ONLY from `gateway.owner_ids` — loaded **exclusively** from env `GOCLAW_OWNER_IDS` (`config_load.go:211-219`, never seeded, never from config.json) — and when that env is unset falls back to matching the **literal string `"system"`** (`router.go:474-476`).
  - The web-login UUID matches neither the literal `"system"` nor any `GOCLAW_OWNER_IDS` entry → `isOwnerID` returns false → the connect flow never forces `RoleOwner` for it (`router.go:311-313`) and the connect-response ctx role is not injected either (`router.go:438-440`). So `client.role != RoleOwner` → `requireOwner` rejects `config.patch` → save fails (surfaced as a generic "save failed" toast via `use-config.ts:84`).
  - The literal `"system"` is a **separate** identity — agents' `owner_id=system`, `cmd/agent_chat_client.go:85`, and the gateway-token/system path — not the human web-login user. So "I'm the SYSTEM user" (web login) does **not** satisfy the `"system"` literal the owner check falls back to. Decisive live check: devtools → WS → the `connect` response frame shows `"is_owner": false` / `"role": "admin"` for this caller.
- **RC3 — `omitempty` on `AutoCompactThreshold` zero value (persistence).** `CompactionConfig.AutoCompactThreshold float64` carries `json:"autoCompactThreshold,omitempty"` (`config.go:194`), and the field's documented sentinel for *disabled* is `0` (`config.go:194` comment: "0 = disabled"). A zero value is a valid, UI-allowed input (`min=0` at `sessions-page.tsx:120`, and the guard accepts `t >= 0`), but `omitempty` drops it on every `json.Marshal` — both the on-disk `config.Save` and the in-memory `Config.Hash()` (`config_load.go:317`, `sha256(json.Marshal(c))`). So a user who sets the threshold to `0` to disable auto-compaction gets a *successful* `config.patch` (the merge writes `0` in memory, `config.go:216`), but the value never reaches disk and the next `config.get` returns the field absent → the client falls back to the `?? 0.75` default (`sessions-page.tsx:37`). The setting "does not stick." (`KeepLastMessages int` has the same `omitempty` tag (`config.go:193`) but its UI range starts at 1, so it cannot hit zero in practice today — it is a latent variant of the same bug.)

The field names, the WS method name, the config struct path, and the i18n namespace were all verified to **match** end-to-end (see §2 "Out of scope") — none of them is the defect.

This SRS owns the **save path** for the Sessions Settings popover: the client validation feedback (RC1), the authorization semantics + UI permission gate (RC2), and the `omitempty` round-trip (RC3). It composes with `001-feat-tenant-user-crud.md` (RBAC roles, `tenant.user.target_role_forbidden`) and the global-config ownership model; it does **not** change how compaction is *consumed* by the agent loop (a separate, working mechanism — `internal/agent/loop_history_sanitize.go`, see `009-bugfix-agent-episodic-recall-not-surfaced.md` §6).

## 2. Scope

**In scope**:

- Making the Sessions Settings popover's Save give deterministic feedback: disable Save on invalid input and surface an error (not a silent no-op) — RC1.
- Closing the owner-recognition identity split (RC2, confirmed live): make the master/system owner on **web email login** (a UUID) recognized as the platform owner by seeding `gateway.owner_ids` at startup, so `config.patch`'s owner gate admits the legitimate owner — plus a UI gate so non-owners see a disabled (not dead) Settings popover.
- Making `AutoCompactThreshold = 0` ("disabled") round-trip through `config.patch` / `config.Save` / `Config.Hash()` by removing the lossy `omitempty` on the zero-value numeric fields — RC3.
- A live repro plan (§7) to confirm *which* RC is the reporter's live symptom before/while fixing (the three causes are not mutually exclusive).

**Out of scope**:

- **Reproducing which RC is live** in this draft — that needs a running gateway + browser session (same env-gating convention as `003`/`007`/`008`/`009`). The doc records all three against code; §7 narrows it at verify time.
- **Field/method/namespace alignment.** Verified matching, not the bug: FE patch keys `autoCompactThreshold`/`keepLastMessages` (`sessions-page.tsx:52`) == Go struct tags (`config.go:193-194`); FE `Methods.CONFIG_PATCH = "config.patch"` (`ui/web/src/api/protocol.ts:71`) == BE `protocol.MethodConfigPatch = "config.patch"` (`pkg/protocol/methods.go:32`); config path `agents.defaults.compaction.*` matches the FE read (`sessions-page.tsx:37-38`) and the Go `AgentsConfig.Defaults.Compaction` (`config.go:160-181`); i18n namespace `sessions` is registered for `en`/`vi`/`zh` and the `settings.*` keys exist (`ui/web/src/i18n/locales/en/sessions.json:20-28`). No missing-key crash, no rename needed.
- **The full config-page compaction editor** (`ui/web/src/pages/config/sections/behavior-sessions-card.tsx`) — a separate, richer editor for the same `agents.defaults.compaction` values. It shares the same backend save path (`config.patch`) so RC2/RC3 affect it too, but its UI is not the reported menu. Listed as a sibling caller in §4.
- **Changing what compaction does** (the summarize/truncate behaviour in the agent loop). This SRS only fixes *saving* the two settings, not their effect.
- **Relaxing `config.*` to be tenant-aware.** `requireMasterScope` exists precisely because `config.*` mutates the master in-memory config + on-disk `config.json` and a non-master caller would corrupt/leak master state (rationale comment, `config.go:66-73`). A tenant-aware config refactor is explicitly deferred there and is **not** this SRS.
- **Per-agent compaction overrides** (if any are persisted via `agents.*` rows / `memory_config`). The reported menu edits only the global `agents.defaults.compaction`; per-agent paths are untouched.

## 3. Functional Requirements

### FR-00: Save Flow Today (verified map — no code change in this FR)

The Sessions Settings popover's save path, as it exists today, for traceability:

| Stage | Site | Behaviour |
|---|---|---|
| Menu | `ui/web/src/pages/sessions/sessions-page.tsx:105-159` | `<Popover>` gear button; two number inputs bound to `draftThreshold`/`draftKeepLast` |
| Load values | `sessions-page.tsx:37-38` | reads `config.agents.defaults.compaction.{autoCompactThreshold,keepLastMessages}`, default `0.75`/`4` via `??` |
| Sync drafts | `sessions-page.tsx:43-46` | `useEffect` resets drafts when loaded values change |
| Save handler | `sessions-page.tsx:48-54` | `handleSaveSettings`: parse + range-guard, then `patchConfig(...)` if valid |
| Save button | `sessions-page.tsx:145-150` | `onClick={handleSaveSettings}`, `disabled={configSaving}` only |
| Mutation hook | `ui/web/src/pages/config/hooks/use-config.ts:69-91` | `patch` → `ws.call(CONFIG_PATCH, {raw: JSON.stringify(updates), baseHash})`; toast on success/error |
| Hash seed | `use-config.ts:25-34` | `config.get` query sets `hashRef.current = res.hash` |
| Backend register | `internal/gateway/methods/config.go:41` | `MethodConfigPatch` = `requireMasterScope(requireOwner(m.handlePatch))` |
| Auth: owner | `config.go:50-62` | `!client.IsOwner()` → `ErrUnauthorized` (`MsgPermissionDenied`) |
| Auth: master scope | `config.go:74-87` | `!IsMasterScope(ctx)` → `ErrUnauthorized` (`MsgConfigMasterScopeOnly`) |
| Optimistic concurrency | `config.go:185-188` | `baseHash` mismatch → `ErrInvalidRequest` (`MsgConfigHashMismatch`) |
| Merge | `config.go:191-219` | clone current into `merged`, reject `auth` patches, `json5.Unmarshal(raw, merged)` |
| Persist | `config.go:226` (`config.Save`) + `:232` (`m.cfg.ReplaceFrom`) | save to disk + swap in-memory |
| Client role source | `internal/gateway/client.go:220` | `IsOwner() == (c.role == RoleOwner)`; `RoleOwner` injected only for global owners (`internal/gateway/router.go:435`) |

Acceptance criteria:

- [ ] The map above is confirmed accurate at implementation time (re-grep each site; values may have shifted). _(verification step in §7)_

---

### FR-01: RC1 — Save Must Not Silently No-Op on Invalid Input

`handleSaveSettings` (`sessions-page.tsx:48-54`) today:

```ts
const handleSaveSettings = () => {
  const t = parseFloat(draftThreshold);
  const k = parseInt(draftKeepLast, 10);
  if (!isNaN(t) && t >= 0 && t <= 1 && !isNaN(k) && k >= 1 && k <= 20) {
    patchConfig({ agents: { defaults: { compaction: { autoCompactThreshold: t, keepLastMessages: k } } } });
  }
};
```

When the guard fails, the function returns silently — no toast, no inline error, button unchanged. The user clicks Save and **nothing happens**, which is the reported symptom for any out-of-range/empty/non-numeric draft. The fix must make validation **visible and deterministic**.

Acceptance criteria:

- [x] The Save button is **disabled** while the draft is invalid (empty, non-numeric, or out of `[0,1]`/`[1,20]`), mirroring the existing `disabled={configSaving}` and consistent with the mobile/touch-target rules. _(implemented: `disabled={configSaving || !settingsValid}`, `sessions-page.tsx`; `pnpm build` green)_
- [x] An invalid draft shows an inline error under the offending input (`text-destructive` `invalidThreshold`/`invalidKeepLast`), not a silent state. _(implemented; i18n added en/vi/zh)_
- [x] A valid draft still calls `patchConfig` exactly once (no behaviour regression for the common path). _(implemented — guard retained, `settingsValid` gate)_
- [x] `patchConfig(...)` is **`await`ed** with a `catch` at the call site so a rejection cannot become an unhandled promise rejection; the hook's existing toast (`use-config.ts:80/84`) remains the user-facing signal. _(implemented: `handleSaveSettings` is `async`, `try/await/catch`)_

---

### FR-02: RC2 — Recognize the Master/System Owner Web Login (CONFIRMED LIVE CAUSE)

`config.patch` is owner-only (`config.go:41`/`:50-62`), and the operator reproducing this is the master/"SYSTEM" user on **web email login** — whose `userId` is a UUID that the owner check does not recognize (see §1 RC2 / Revision 0.2). The fix must close the identity split so the legitimate platform owner (web login) is recognized, **without** relaxing the master-config ownership model.

**Chosen fix — auto-seed `gateway.owner_ids` with the master-tenant owner UUID(s) at startup (idempotent).** The owner set is authoritative for `isOwnerID` (`router.go:470`), the WS policy engine (`permissions.NewPolicyEngine(cfg.Gateway.OwnerIDs)`, `gateway_setup.go:229`), and the HTTP owner set (`httpapi.InitOwnerIDs`, `gateway.go:671`). Today it is fed **only** from `GOCLAW_OWNER_IDS` (`config_load.go:211-219`); when unset, the check degrades to the literal `"system"`, which the web-login UUID never matches. Seeding it with the master tenant's `is_owner` users (derivable via the existing `TenantStore` — `IsOwner(ctx, MasterTenantID, userID)` `tenant_store.go:273`, and a list-equivalent) makes the existing owner machinery recognize the web-login owner. No authorization-semantics change: the same `isOwnerID`/`RoleOwner` path then admits the caller.

| Caller | Today (`config.patch`) | After |
|---|---|---|
| Master/system owner via **gateway token** (`user_id="system"`) | ✅ owner (literal match) | ✅ unchanged |
| Master/system owner via **web email login** (UUID) | ❌ not owner → `ErrUnauthorized` | ✅ owner — UUID now in the seeded `owner_ids` |
| Other tenant admin/owner | ❌ not owner | ❌ unchanged (correct — they are not the platform owner) |

Acceptance criteria (implementation chose connect-recognition — see §11 deviation — so these are evaluated against that approach, not the startup-seed sketch above):

- [x] A web-email-login master/system owner's WS `connect` resolves to `client.role == RoleOwner` (recognized via `TenantStore.IsOwner(master, userID)` in `sendConnectResponse`), so `config.patch` is no longer rejected by `requireOwner`/`requireMasterScope`. _(implemented in `internal/gateway/router.go` `sendConnectResponse`; `go build` (PG + sqliteonly) + `go vet` green)_
- [x] An explicit `GOCLAW_OWNER_IDS` is still honoured as-is (the connect-recognition only *adds* master-tenant owners; the existing `isOwnerID` literal/owner_ids path is untouched).
- [x] Non-master tenant owners are unaffected — only `MasterTenantID` is consulted, so they stay `RoleAdmin` (no privilege leak). _(by inspection — single `IsOwner(ctx, MasterTenantID, …)` call)_
- [x] `requireOwner`/`requireMasterScope` guards themselves are **unchanged** (the fix is recognition, not relaxation — see §2 out of scope).
- [x] Live: on the master tenant, the web-login owner's `connect` response reports `"is_owner": true` and the Sessions Settings save succeeds. _(operator-confirmed, 2026-06-18 — "everything OK")_
- [ ] **Known partial coverage (follow-up, not the reported WS path):** HTTP-side owner checks that key off the userID snapshot — `permPE.IsOwner(userID)` (`permissions/policy.go`) and `httpapi` `pkgOwnerIDs` (`http/auth.go`) — still do not recognize the web-login UUID, because the startup-seed was not implemented. These affect a few HTTP admin handlers, not WS `config.patch`. Track separately if the owner needs HTTP-side owner recognition too.

**Immediate operator workaround (no code):** set env `GOCLAW_OWNER_IDS=<your-UUID>` and restart. Find the UUID with `SELECT id FROM users WHERE email='<your-email>'` (master tenant). This unblocks the save today; the FR-02 code change makes it permanent so other operators do not hit the same trap.

**Companion — UI permission gate (FR-02b).** Independent of the owner-recognition fix, the Settings popover must not render an enabled Save for a caller who cannot `config.patch`. Reuse the existing `useRole()` affordance pattern (`tenant-roles-display.md` §4.1.2 / `006-bugfix-admin-peer-visibility.md` FR-04): hide/disable the popover for non-`RoleOwner` callers with a localized "owner only" hint (i18n key `settings.ownerOnlyHint`, FR-04). This converts a guaranteed-403 affordance into a clear disabled state for any non-owner who reaches the page.

Acceptance criteria (FR-02b):

- [x] A non-`RoleOwner` caller at `/t/{tenant}/sessions` sees the Settings popover hidden/disabled with the "owner only" hint, not an enabled Save that 403s on submit. _(implemented: `isOwner ? <Popover> : <Button disabled title={ownerOnlyHint}>`; `pnpm build` green)_
- [x] RC1/RC2/RC3 fixes still leave the owner path fully functional (no over-restriction of the legitimate owner). _(owner renders the full editable popover; build-verified)_

---

### FR-03: RC3 — `AutoCompactThreshold = 0` Must Round-Trip (remove lossy `omitempty`)

`CompactionConfig` (`config.go:190-196`) marks **every** field `omitempty`:

```go
type CompactionConfig struct {
    ReserveTokensFloor   int                `json:"reserveTokensFloor,omitempty"`
    MaxHistoryShare      float64            `json:"maxHistoryShare,omitempty"`
    KeepLastMessages     int                `json:"keepLastMessages,omitempty"`
    AutoCompactThreshold float64            `json:"autoCompactThreshold,omitempty"` // 0 = disabled (documented sentinel)
    MemoryFlush          *MemoryFlushConfig `json:"memoryFlush,omitempty"`
}
```

For the two **value-typed numeric fields that have a meaningful zero**, `omitempty` makes the zero value indistinguishable from "unset":

- `AutoCompactThreshold` — `0` is the documented **disabled** sentinel (`config.go:194`), UI-allowed (`min=0`, guard `t >= 0`). Setting it to `0` is dropped by every `json.Marshal`: the `config.patch` merge writes `0` in memory, but `config.Save` (`config.go:226`) and `Config.Hash()` (`config_load.go:317`) marshal with `omitempty` → the field is omitted on disk and absent from the returned config. The client then falls back to `?? 0.75` (`sessions-page.tsx:37`). The setting does not persist.
- `KeepLastMessages` — same tag, but the UI range starts at 1 so it cannot be `0` today; a latent variant. Fixed defensively in the same change.

`MemoryFlush` (a pointer) and the non-zero-sentinel numeric fields (`ReserveTokensFloor`, `MaxHistoryShare`) legitimately want `omitempty`; only the two fields whose **zero is a valid, user-selectable value** are lossy.

> `Config.Hash()` (`config_load.go:317`) is `sha256(json.Marshal(c))`, so it shares the `omitempty` form. Removing the tag changes the hash of any config whose threshold/keepLast is currently zero — a one-time, benign hash migration (no data loss; the optimistic-concurrency check at `config.go:185` simply invalidates one stale `baseHash`). Acceptable; note in §7.

Acceptance criteria:

- [x] `CompactionConfig.AutoCompactThreshold` and `.KeepLastMessages` lose the `omitempty` tag (zero round-trips). The other three fields keep `omitempty`. _(implemented: `config.go:193-194`; PG + `sqliteonly` builds green)_
- [x] After saving `autoCompactThreshold = 0`, a subsequent `config.get` returns the field as `0` (not absent), and the Sessions popover shows `0` (not the `0.75` fallback). _(proven by `TestCompactionConfig_ZeroValueRoundTrips`: zero values present in marshal + survive unmarshal; popover read unchanged)_
- [x] `KeepLastMessages` round-trips identically (defensive). _(same test)_
- [x] Removing `omitempty` does not break the `config.schema`/masked-config contract or the secrets-stripping path (`MaskedCopy` `config_secrets.go:9`, `StripSecrets`). _(`go build` + `go vet` green; 59 config tests pass)_
- [x] No DB migration is required (this is the on-disk `config.json` Go struct, not `migrations/` or `sqlitestore/schema.sql`). _(by inspection — no `migrations/`/`schema.sql` touched)_

---

### FR-04: i18n (en / vi / zh)

FR-01 (inline validation error) and FR-02 ("owner only" hint) add user-facing strings. They must go into `ui/web/src/i18n/locales/{en,vi,zh}/sessions.json` (the namespace the page already uses, `sessions-page.tsx:26`), per the project 3-locale rule and `004-feat-roles-page.md` FR-06. Proposed keys:

| Key | English | Vietnamese | Chinese |
|-----|---------|------------|---------|
| `settings.invalidThreshold` | Threshold must be a number between 0 and 1 | Ngưỡng phải là số từ 0 đến 1 | 阈值必须是 0 到 1 之间的数字 |
| `settings.invalidKeepLast` | Keep-last must be a whole number between 1 and 20 | Giữ lại phải là số nguyên từ 1 đến 20 | 保留消息数必须是 1 到 20 之间的整数 |
| `settings.ownerOnlyHint` | Compaction settings can only be changed by the system owner | Cài đặt nén chỉ có thể đổi bởi chủ hệ thống | 压缩设置只能由系统所有者修改 |

Acceptance criteria:

- [x] All three keys exist in `sessions.json` for `en`, `vi`, `zh` with identical key sets. _(added `settings.invalidThreshold` / `settings.invalidKeepLast` / `settings.ownerOnlyHint` to all three locales)_
- [x] No raw key is rendered (`sessions` namespace is already registered, so no namespace mismatch à la `004` FR-07). _(`pnpm build` green)_

## 4. System Impact

- **Frontend — Sessions page** (`ui/web/src/pages/sessions/sessions-page.tsx`): derive an `isValid` from the drafts; `disabled={configSaving || !isValid}` on Save; inline error text under each input; `await patchConfig(...)` with error handled (FR-01).
- **Frontend — permission gate** (`sessions-page.tsx`, reusing `ui/web/src/hooks/use-role.ts` per `tenant-roles-display.md` §4.1.2): hide/disable the Settings popover for non-`RoleOwner` callers with the `ownerOnlyHint` (FR-02).
- **Backend — config struct** (`internal/config/config.go:193-194`): drop `omitempty` from `AutoCompactThreshold` and `KeepLastMessages` only (FR-03). No handler change (`handlePatch` merge + save are already correct; they only fail to round-trip the zero value because of the tag).
- **Backend — owner recognition (RC2, the live fix)** (`internal/config/config_load.go:211-219` + the startup wiring in `cmd/gateway_setup.go:229` / `cmd/gateway.go:671`): augment `gateway.owner_ids` at startup with the master-tenant owner UUID(s) when the operator has not set `GOCLAW_OWNER_IDS` explicitly, then feed the augmented set to both `permissions.NewPolicyEngine` (`gateway_setup.go:229`) and `httpapi.InitOwnerIDs` (`gateway.go:671`) so WS + HTTP agree. Owner UUIDs are derived via the existing `TenantStore` (master tenant, `is_owner = true`; `IsOwner` at `tenant_store.go:273`, list-equivalent to add/confirm). This is recognition, not authorization relaxation — `isOwnerID` (`router.go:470`) and the guards (`config.go:50-62`, `:74-87`) are unchanged.
- **i18n**: 3 new keys in `sessions.json` × `en`/`vi`/`zh` (FR-04).
- **Sibling caller** (`ui/web/src/pages/config/sections/behavior-sessions-card.tsx`): shares the same `config.patch` save path → benefits from the RC3 backend fix automatically; verify its UI still saves (no FE change required there, but confirm in §7).
- **No schema migration** (config struct, not DB). **No new error codes** (RC2 reuses `MsgPermissionDenied`/`MsgConfigMasterScopeOnly`; RC1/RC3 are client/round-trip fixes). **No store change**, no WS method change, no route change.

## 5. Test Plan

- **Round-trip test (RC3, backend):** load a `config.Config` with `Compaction.AutoCompactThreshold = 0`, `config.Save` to a temp path, reload, assert the field is `0` (not the default). Repeat for `KeepLastMessages`. Assert `Hash()` is stable across marshal/unmarshal of a zero-threshold config (no flapping).
- **Handler test (RC3):** `config.patch` with `{agents:{defaults:{compaction:{autoCompactThreshold:0}}}}` then `config.get` returns `autoCompactThreshold: 0`.
- **Frontend unit (RC1):** `handleSaveSettings` calls `patchConfig` for valid drafts; is disabled / shows error for empty, non-numeric, `1.5`, `-1`, `0` keepLast, `21` keepLast. (Pure helper extraction if the repo has no `@testing-library/react` — same convention as `007` §0.7.)
- **Auth test (RC2, regression):** a non-`RoleOwner` client calling `config.patch` still gets `ErrUnauthorized` (guard unchanged); the Sessions popover is hidden/disabled for that role in the rendered tree.
- **Owner-recognition test (RC2, the fix):** with `GOCLAW_OWNER_IDS` unset, a startup seed augments `owner_ids` with the master-tenant owner UUID; that UUID's WS `connect` reports `role: owner` / `is_owner: true` and `config.patch` succeeds (was: rejected). An explicit `GOCLAW_OWNER_IDS` is not clobbered.
- **Manual (live, §7):** owner opens `/t/master/sessions` Settings → change `keepLastMessages` 4→6 → Save → reload → persists (rules out RC1/RC2 for the owner path, exercises the happy path). Set `autoCompactThreshold` → `0` → Save → reload → shows `0`, not `0.75` (RC3). Then reproduce as a tenant admin to exercise RC2.
- **Checklist:** `go build ./...`, `go build -tags sqliteonly ./...`, `go vet ./...`, `pnpm build` in `ui/web`.

## 6. Decision Log (locked)

| Decision | Rationale |
|----------|-----------|
| **Fix all three RCs, not just the live one.** | The three are independent and any can produce "cannot save." RC1 (silent no-op) and RC3 (omitempty) are unambiguous code bugs; RC2 is the most likely *live* cause but depends on the reporter's role. Fixing all three makes the menu correct for every caller and every value. |
| **Do NOT relax the owner/master-scope guard (RC2).** | The guard's rationale (`config.go:66-73`) is correct: `config.*` mutates master-global `config.json`. The defect is a UI that exposes a save the caller cannot complete, not a too-strict guard. Fix the UI gate; keep the backend. |
| **Remove `omitempty` only on the two zero-is-valid fields (RC3).** | `MemoryFlush *` (pointer) and the numeric fields whose zero means "default/unset" keep `omitempty`. Only `AutoCompactThreshold` (0 = disabled) and `KeepLastMessages` lose it. Smallest blast radius; no schema migration. |
| **Disable Save on invalid input rather than fire-and-forget + toast (RC1).** | A disabled button + inline error is deterministic and discoverable; the current silent drop is the exact UX that reads as "Save is broken." |
| **`await patchConfig` at the call site (RC1-adjacent).** | The hook already toasts on rejection, but the call site (`sessions-page.tsx:52`) is fire-and-forget. Awaiting + handling keeps failures observable if the hook's toast wiring ever changes. |
| **RC2 is the live cause; fix by recognizing the web-login owner, not by relaxing the guard (v0.2).** | Operator confirmed they are the master/"SYSTEM" user. Verified identity split: web email login → UUID (`email-login-form.tsx:64`), but `isOwnerID` only matches `gateway.owner_ids` (env `GOCLAW_OWNER_IDS` only) or the literal `"system"` (`router.go:474-476`). The UUID matches neither → not owner → `config.patch` rejected. The guard rationale (`config.go:66-73`) is sound; the defect is that the legitimate owner is not recognized. |
| **Augment `owner_ids` at startup; do not auto-broaden `isOwnerID` to "any master-tenant owner" in the hot path.** | Seeding `owner_ids` with the master-tenant owner UUID(s) reuses the single existing owner source-of-truth consumed by WS policy engine + HTTP owner set (`gateway_setup.go:229`, `gateway.go:671`), so WS and HTTP cannot drift. A runtime `IsOwner(master, userID)` lookup inside `isOwnerID` would be a wider authorization-semantics change (every `requireOwner`/`IsMasterScope` site) and needs ctx/store the pure func lacks. Seed-once is lower blast radius. |
| **Augment only when `GOCLAW_OWNER_IDS` is unset/empty; never override an explicit list.** | An operator who set `GOCLAW_OWNER_IDS` chose their owner set deliberately; auto-merging more IDs could surprise-escalate. Empty/unset is the trap state (falls back to literal `"system"`), so seeding only there fixes the defect without clobbering intent. |

## 7. Risks and Open Questions

| Risk or question | Draft decision |
|------------------|----------------|
| Which RC is the reporter's live symptom? | **Resolved (v0.2): RC2 confirmed.** Operator is the master/"SYSTEM" user on web email login → UUID not recognized as owner (`isOwnerID` matches only `GOCLAW_OWNER_IDS` or literal `"system"`) → `config.patch` rejected. Decisive live check: devtools WS `connect` response `is_owner: false` / `role: admin`. RC1 (silent no-op) and RC3 (omitempty/zero) remain real latent bugs to fix in the same change but are not this reporter's block. |
| Exact startup wiring to re-feed the seeded `owner_ids` to the policy engine + `httpapi.InitOwnerIDs`. | Verify at implementation: `cmd/gateway_setup.go:229` (`permissions.NewPolicyEngine(cfg.Gateway.OwnerIDs)`) and `cmd/gateway.go:671` (`httpapi.InitOwnerIDs`) both read the config value at startup, so the seed must happen **before** those calls (or mutate the slice in place before they read it). Confirm there is no later re-init that would drop the seeded IDs. |
| Removing `omitempty` shifts `Config.Hash()` for any config currently holding a zero threshold/keepLast. | Benign: the optimistic-concurrency check (`config.go:185`) invalidates one stale `baseHash` → at most one "save failed, retry" on a client holding an old hash. No data loss. |
| Should the full config-page compaction editor (`behavior-sessions-card.tsx`) also gain the RC1/RC2 UX fixes? | Verify in §7 it still saves after the RC3 backend fix; mirror the RC1 disable-on-invalid + RC2 owner gate there in a follow-up if it has the same dead-affordance pattern. Not gating. |
| Is `/t/master/sessions` reachable by Member/Viewer (FR-02 assumes a route guard exists)? | Verify the route guard at implementation time (`RequireAdmin`-equivalent for `/sessions`); if Members can reach it, the popover gate is still correct, just exercised by fewer roles. |
| Could `json5.Unmarshal` (the merge step, `config.go:216`) replace rather than merge the `*CompactionConfig` pointer (dropping sibling fields)? | Go's `encoding/json` merges into non-nil pointers, preserving siblings; verify `titanous/json5` matches at implementation time (write the round-trip test with a sibling field like `reserveTokensFloor` set, patch only threshold, assert the sibling survives). If it replaces, that is a *separate* latent data-loss bug to file — not the reported "cannot save." |

## 8. Implementation Plan

1. **RC3 (backend, lowest risk, unblocks the persistence half):** drop `omitempty` from `AutoCompactThreshold` + `KeepLastMessages` in `internal/config/config.go`. Add the round-trip + handler tests (§5). `go build` (PG + `sqliteonly`) + `go vet`.
2. **RC1 (frontend):** in `sessions-page.tsx`, compute `isValid`, disable Save on invalid + inline error, `await patchConfig`. Extract a pure validation helper if no `@testing-library/react` (per `007` §0.7) and unit-test it.
3. **RC2 — owner recognition (backend, THE live fix):** in the startup path, when `GOCLAW_OWNER_IDS` is unset/empty, augment `cfg.Gateway.OwnerIDs` with the master-tenant owner UUID(s) derived via `TenantStore` (`is_owner = true`, master tenant; `tenant_store.go:273`); ensure the augmented set feeds `permissions.NewPolicyEngine` (`gateway_setup.go:229`) and `httpapi.InitOwnerIDs` (`gateway.go:671`) before they read it. Verify via the `connect` response (`is_owner: true`) for a web-email-login master owner. (Immediate unblock without code: operator sets `GOCLAW_OWNER_IDS=<UUID>`.)
4. **RC2 — UI gate (frontend, FR-02b):** hide/disable the Settings popover for non-`RoleOwner` callers via `useRole()`; add `ownerOnlyHint` i18n (3 locales).
5. **i18n (FR-04):** add the 3 keys to `sessions.json` × `en`/`vi`/`zh`.
6. **Sibling caller:** verify `behavior-sessions-card.tsx` still saves; mirror RC1/RC2 there in a follow-up if needed.
7. **Manual live repro (§5/§7):** owner happy-path, threshold=0, tenant-admin.
8. **Checklist:** `go build ./...`, `go build -tags sqliteonly ./...`, `go vet ./...`, `pnpm build` in `ui/web`.

## 9. Proposed Error Codes

No new canonical error codes. The paths reuse existing ones; for traceability:

| Code (existing) | Meaning | Reuse site |
|------|---------|------------|
| `config.permission_denied` (i18n `MsgPermissionDenied`, `ErrUnauthorized`) | Caller is not the global owner (`requireOwner`, `config.go:54`). | RC2 — unchanged; surfaced/handled by the UI gate. |
| `config.master_scope_only` (i18n `MsgConfigMasterScopeOnly`, `ErrUnauthorized`) | Caller's ctx is scoped to a non-master tenant (`requireMasterScope`, `config.go:81`). | unchanged |
| `config.hash_mismatch` (i18n `MsgConfigHashMismatch`, `ErrInvalidRequest`) | Stale `baseHash` (optimistic concurrency, `config.go:186`). | unchanged; note the one-time hash shift from RC3. |

No error code is registered by this bugfix; RC1/RC3 are client/round-trip fixes with no new error surface.

## 10. Implementation notes (v0.3) — what was built + deviation from §4/§8

Implemented and build/vet/test-verified locally. Live verification on the master tenant (the Sessions save + the `connect` `is_owner:true`) is pending a running gateway + the operator's confirmation.

### 10.1 RC3 — files

- `internal/config/config.go` — `CompactionConfig.AutoCompactThreshold` and `.KeepLastMessages` dropped `omitempty` (the other three fields keep it). One-line tag change each; `0` (the documented "disabled" sentinel) now round-trips through `config.patch` → `config.Save` → `config.get`.
- `internal/config/compaction_omitempty_test.go` (new) — `TestCompactionConfig_ZeroValueRoundTrips` + `_NonZeroStillSerializes`, green.

### 10.2 RC1 + RC2b — files

- `ui/web/src/pages/sessions/sessions-page.tsx` — derived `thresholdValid`/`keepLastValid`/`settingsValid`; inline `text-destructive` error under each invalid input; Save `disabled={configSaving || !settingsValid}`; `handleSaveSettings` is now `async` with `try/await/catch` (no more silent no-op / fire-and-forget). Inputs gained `text-base md:text-sm` (mobile no-zoom rule). Settings popover gated: `isOwner` (via `useRole()`) renders the full editable popover; a non-owner gets a disabled button with `title={ownerOnlyHint}` (no dead 403 affordance).
- `ui/web/src/i18n/locales/{en,vi,zh}/sessions.json` — added `settings.invalidThreshold`, `settings.invalidKeepLast`, `settings.ownerOnlyHint` to all three locales.

### 10.3 RC2 — deviation: connect-recognition instead of startup-seed

§4/§8 sketched seeding `gateway.owner_ids` at startup with the master-tenant owner UUID(s). **That approach was rejected at implementation time** for two verified blockers:

1. `permPE` (`permissions.NewPolicyEngine(cfg.Gateway.OwnerIDs)`) is built at `cmd/gateway.go:148` **before** the DB/tenant store exists, so a DB-derived seed cannot feed it without reordering startup + re-feeding the snapshot (and there is no public `PolicyEngine` setter).
2. There is no `ListOwnerUserIDs`/owner-list method on `TenantStore`; adding one to the interface would require a SQLite implementation, but `internal/store/sqlitestore/tenants.go` lacks an owner-listing query — risking the `sqliteonly` (desktop) build.

**Chosen instead: connect-recognition.** In `internal/gateway/router.go` `sendConnectResponse`, before building the response, if `client.role != RoleOwner && client.userID != "" && r.tenantStore != nil`, recognize the master-tenant owner via the **existing** `TenantStore.IsOwner(ctx, store.MasterTenantID, client.userID)` (implemented in both PG and SQLite) and set `client.role = RoleOwner`. The scopedCtx role injection now keys off `client.IsOwner()` (covers both the explicit `owner_ids` path and this recognition).

This achieves the FR-02 goal (the web-login master/system owner is recognized as the platform owner → `config.patch` admits them) with:
- no `TenantStore` interface change (no SQLite/build risk),
- no startup reorder, no `permPE`/`httpapi` re-feed,
- and every role-based check (`requireOwner` via `client.IsOwner()`, `requireMasterScope` via the per-request ctx role from `client.Role()`, `canSeeAll` via `HasMinRole(RoleAdmin)`) passes because `client.role == RoleOwner`.

`r.tenantStore` is nil-guarded (lite/desktop may not set it), so the change no-ops there and falls back to existing behaviour. Only `MasterTenantID` is consulted, so non-master tenant owners stay `RoleAdmin` — no privilege leak.

**Known partial coverage (follow-up):** HTTP-side owner checks that key off the userID snapshot — `permPE.IsOwner(userID)` and `httpapi` `pkgOwnerIDs` — still do not recognize the web-login UUID. These guard a few HTTP admin handlers, **not** WS `config.patch` (the reported path). If the owner needs HTTP-side owner recognition too, implement the startup-seed (with the `TenantStore.ListOwnerUserIDs` addition + `PolicyEngine` setter) as a follow-up.

### 10.4 Verification

`go build ./...` ✓, `go build -tags sqliteonly ./...` ✓, `go vet ./internal/config/... ./internal/gateway/...` ✓, `go test ./internal/config/...` 59/59 ✓ (incl. the 2 new RC3 cases), `pnpm build` (ui/web) ✓. Two pre-existing test failures — `TestClientCanReceiveEvent_AdminOnlyEvent_BlockedForNonAdmin` (event classification) and `TestUserPermission_RouteGuardsAreWritePermissions` (`user.unenroll` catalog drift, SRS 004) — were confirmed **unrelated** (they fail identically on the clean tree with this change's backend edits stashed).

### 10.5 Still pending (env-gated)

- Live confirm on the master tenant: web-login owner `connect` → `is_owner:true`; Sessions Settings edit → Save → persists (incl. threshold `0`).
- HTTP-side owner recognition follow-up (§10.3).

