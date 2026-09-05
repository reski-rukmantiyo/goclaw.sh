# Software Requirements Specification: Paired Device ("Nodes") Sliding Expiry — Active Devices Auto-Renew, No 30-Day Re-Pairing

**Project**: GoClaw Gateway
**Release**: 2026.3.0
**Version**: 0.6-draft
**Date**: 2026-08-17
**Status**: Implemented (code-complete + build/vet/test-verified). UI render + live on-tenant verification pending (§3 FR-02/FR-03 manual boxes).
**Difficulty**: Low–Medium
**Estimate**: 0.5–1 days

---

## Revision History

| Version | Date | Changes |
|---------|------|---------|
| 0.6-draft | 2026-09-04 | **Expires-column display bug fixed (FR-02 pending AC root-caused).** Operator reported the Nodes UI Expires column showed "just now" for a WhatsApp DM pairing that was verifiably healthy in the DB (`expires_at = 2026-10-04`, 29d remaining; `whatsapp-reski` instance, live PG). Verified NOT an expiry/renewal defect: store + WS path send correct Unix-ms (`pg/pairing.go` `ListPaired` → `ExpiresAt.UnixMilli()`; `methods/pairing.go` `handleList` returns DTO verbatim). Root cause: `formatRelativeTime` in `ui/web/src/lib/format.ts` was **past-only** — `diffMs = now - d.getTime()` is negative for any future date, and the first branch `if (diffSec < 60) return "just now"` swallows every negative value, so ALL future timestamps rendered "just now". Same latent bug hit 3 other future-date callers: `heartbeat-card.tsx:109` (`nextRunAt`), `api-keys-page.tsx:176` (`expires_at`), `cron-jobs-card.tsx:57` (next run). Fix: formatter now computes sign-aware diff (`d.getTime() - Date.now()`, `Math.abs` for magnitude) and renders future as `in Xm/Xh/Xd`; past output byte-identical (all ~40 past-timestamp call sites unaffected). Desktop UI (`ui/desktop/frontend`) has past-only copies of the same formatter but no future-date caller today — latent, untouched. Also verified live DB state: only 1 `paired_devices` row remains (re-paired 2026-09-04 13:51 UTC after old row lapsed pre-restart — old gateway process predated the sliding-renewal rebuild at 22:23 local, matching FR-08.1 "expired row cannot self-resurrect"); `pairing_requests` empty; `channels.pairing` config absent → defaults (30d TTL, 7.5d window) active. **Verification:** `pnpm build` (ui/web) ✓. |
| 0.5-draft | 2026-08-17 | **Unified member sliding expiry (FR-08).** Operator report: group chats with expired members still worked while the same members' **personal (DM) chats** forced re-pairing. Root cause (verified against live `whatsapp-reski` instance, DB `postgres@localhost:5432`): (a) the instance runs `group_policy = "open"` → `CheckGroupPolicy` returns `PolicyAllow` **before** reaching `IsPaired` (the renewal hook), so group activity never renewed anything; (b) the **group pairing row** (`group:<chatID>`) and the **member's personal row** (`<senderID>`) are separate `paired_devices` rows — even under `group_policy = "pairing"`, group messages renew only the group row, never the member's own. A member active exclusively in groups hit the 30-day wall → DM re-auth. Fix (FR-08): `CheckGroupPolicy` now calls `TouchGroupMember(ctx, senderID)` first — a best-effort, TTL-gated (`memberRenewCacheTTL = 60 min`) re-run of `IsPaired` for the **member's own senderID**, renewing their personal row regardless of the group-policy verdict. Single sliding expiry per member: **any** qualifying activity (DM *or* any group message) extends the same `expires_at`. Renewal never gates the message (result ignored; unpaired members stay governed by the group policy). Covers every `CheckGroupPolicy` caller: WhatsApp, Discord, Feishu, Slack, Zalo personal. New tests: `TestTouchGroupMember_RenewsOwnPairing`, `_RateLimited`, `_UnpairedMemberStillRenews`, `_NoPairingService`, `TestClearGroupMemberRenewal`; `TestCheckGroupPolicy_ActiveGroupRevalidatesAfterCacheTTL` updated for the member-renewal call. **Also fixed stale doc:** FR-06/FR-07 previously documented `groupApproveCacheTTL = 10 min`; the shipped constant is `60 * time.Minute` (`channel.go`, commit daf7dd02) — doc now matches code. **Verification:** `go test -race ./internal/channels/` 140/140 ✓. |
| 0.1-draft | 2026-07-15 | Initial draft. Verified root cause against code: every paired device gets a hard `expires_at = paired_at + 30d` (`internal/store/pg/pairing.go:20,112`; `internal/store/sqlitestore/pairing.go:24,98`) and the TTL is never refreshed — `IsPaired` only *reads* the expiry (`pg/pairing.go:167`; `sqlite:152`), the only `UPDATE paired_devices` is `MigrateGroupChatID` which does not touch `expires_at` (`pg/pairing.go:265`; `sqlite:249`). So an active device that messages daily still dies at day 30 and is pruned on the next `ListPaired` (`pg/pairing.go:231`; `sqlite:204`) → operator must re-pair ("register nodes again"). **No schema migration required** — the `expires_at` column already exists in both DBs (PG `migrations/000021_paired_devices_expiry.up.sql:3`; SQLite `schema.sql:539`). Chosen fix: sliding renewal inside the store `IsPaired` (single point covers every channel + browser), gated by a renewal window to avoid a write-per-message, plus a configurable TTL + renewal window, and surfacing `expires_at` in the `pairing.list` response + Nodes UI so the expiry is visible. |
| 0.2-draft | 2026-07-15 | **Implemented (code-complete + build/vet/test-verified).** Config: `PairingConfig` (`config_channels.go`) with `DeviceTTLDuration()` (30d default, `0`=never) + `RenewalWindowDuration(ttl)` (auto ttl/4, clamp, `0`=disabled), threaded through `store.StoreConfig` (`types.go` + `DefaultPairedDeviceTTL`) → `pg/factory.go`/`sqlitestore/factory.go` → constructors `NewPGPairingStore(db,ttl,window)`/`NewSQLitePairingStore(db,ttl,window)`; 4 gateway build sites + onboard seed store wired (`cmd/gateway_stores_*.go`, `cmd/onboard_managed.go`). Stores: `IsPaired` runs a window-gated, best-effort, tenant-scoped renewal UPDATE after the COUNT verdict (PG + SQLite); `ApprovePairing` writes `expires_at = NULL` when ttl≤0; `ListPaired` SELECT + `pairedDeviceRow` add `expires_at`; `PairedDeviceData.ExpiresAt *int64` (`pairing_store.go`). Hardcoded `pairedDeviceTTL` consts removed. i18n: 4 keys × en/vi/zh in `nodes.json`. UI: `PairedDevice.expires_at?` (`use-nodes.ts`) + Expires column + never/expired/expiring-soon badges (`nodes-page.tsx`). **Verification:** `go build ./...` ✓, `go build -tags sqliteonly ./...` ✓, `go vet ./...` clean, `go test -tags sqliteonly -race ./internal/store/sqlitestore/ -run TestSQLitePairingStore_` 7/7 ✓, `go test ./internal/config/` 69/69 ✓ (incl. new `TestPairingConfig_Durations` 8 subtests + `TestChannelsConfig_PairingDefault`), `pnpm build` (ui/web) ✓. `go fix ./...` reverted — produced unrelated modernization churn (agent/memory/http-tenants tests etc.), same convention as `007`/`009`. **Deferred (env-gated):** PG pairing store integration test (needs pgvector pg18 container; PG impl mirrors SQLite line-for-line — SQLite tests prove the SQL logic), live on-tenant manual check (active device's `expires_at` advances; idle device pruned), and the FR-02/FR-03 UI render checks (needs browser). **Pre-existing unrelated failures:** 4 SQLite schema-migration DDL tests (`TestEnsureSchema_MigrationV11*`, `TestSQLiteSchemaUpgrade_23_to_24`, `_25_to_26_HeartbeatFK`) — same class documented in `008`; fail identically on the clean tree. |
| 0.3-draft | 2026-07-22 | **Live-verification gap closed (FR-06).** Operator reported a WhatsApp group showed `expires ≈ now` ("just now") yet still accepted messages. Root cause: the in-memory group-approval cache `approvedGroups` (`channel.go`) had **no TTL** — a group was cached on first `IsPaired` hit and every later message hit the cache → `PolicyAllow` **before** reaching `IsPaired`. So for groups the FR-00 renewal (which lives in `IsPaired`) never fired (expiry lapsed) **and** a revoked/expired group kept working for the whole process lifetime. Fix (centralized in `IsGroupApproved`/`MarkGroupApproved`): cache now stores `chatID → time.Time` (approved-at); `IsGroupApproved` evicts + returns false when older than `groupApproveCacheTTL` (10 min), so the next message re-validates via `IsPaired` → renews (FR-00) **or** evicts an expired/revoked group. Covers WhatsApp + Telegram (all `IsGroupApproved` callers). New `TestGroupApproveCache_TTL`. **Verification:** `go build` (PG+sqliteonly) ✓, `go vet ./internal/channels/...` clean, `go test -race ./internal/channels/ -run TestGroupApproveCache` ✓. **Note:** requires a gateway rebuild + restart to take effect; the old in-memory cache only clears on restart. |
| 0.4-draft | 2026-07-22 | **Explicit active-node guarantee (FR-07) + live-diagnosis.** Operator: "as long as there's activity from each node — especially WhatsApp + WhatsApp group — the node should extend its sliding window so it never expires during active use." Verified this is **already satisfied** by FR-00 (renewal) + FR-06 (cache TTL): DMs call `IsPaired` every message (no cache); groups' `approvedGroups` cache HIT does **not** re-stamp → ages out after `groupApproveCacheTTL` → next msg re-runs `IsPaired` (renewal). So an active group is renewed within ≤10 min of entering the 7.5-day window. Added `FR-07` with the guarantee + ACs. New `TestCheckGroupPolicy_ActiveGroupRevalidatesAfterCacheTTL` (mock `isPairedCalls`: fresh-cache burst = 1, post-TTL = 2) — proves cache hits skip `IsPaired` but a stale cache re-validates (the renewal path an active group relies on). Added `isPairedCalls` counter to `mockPairingStore`. **Live diagnosis:** staged the 3 `whatsapp-reski` devices into the renewal window (`expires_at = now+2h`) to force a slide; the operator's test messages were <10 min apart, so the 2nd hit the still-fresh cache → `IsPaired` skipped → no slide observed (expected, not a defect). Devices **restored to originals** (Aug 7/10/20, healthy 16–29d) to avoid lockout. **Verification:** `go test -race ./internal/channels/` 135/135 ✓ (incl. the 2 cache tests). **Still pending:** the live slide (device in-window + message >10 min after the previous one). |

---

## 1. Summary

The **Nodes** page (`/t/{tenant}/nodes`, `ui/web/src/pages/nodes/nodes-page.tsx`) lists approved device pairings — Telegram/WhatsApp/Discord/Feishu/Zalo/Slack chat bindings + the browser pairing flow, backed by the `paired_devices` table (`internal/gateway/methods/pairing.go`). Operators report that **paired devices must be re-registered ("re-paired") after some time**, even though the device is in constant use.

**Confirmed root cause (single, code):** every approved pairing is written with a hard expiry `expires_at = now + pairedDeviceTTL`, where `pairedDeviceTTL = 30 * 24 * time.Hour` — a hardcoded package-level const duplicated in **both** stores (`internal/store/pg/pairing.go:20`, `internal/store/sqlitestore/pairing.go:24`, set at `pg/pairing.go:112` and `sqlite:98`). The expiry is **never refreshed after approval**:

- `IsPaired` (`internal/store/pg/pairing.go:163-174`; `internal/store/sqlitestore/pairing.go:150-156`) only *reads* `expires_at` in its WHERE clause: `... AND (expires_at IS NULL OR expires_at > NOW())`. A hit returns true; it does **not** bump `expires_at`.
- The only `UPDATE paired_devices` in the codebase is `MigrateGroupChatID` (`internal/store/pg/pairing.go:254-313`; `internal/store/sqlitestore/pairing.go:249`), which rewrites `sender_id`/`chat_id` — it does **not** touch `expires_at`.
- So a device that sends a message every day still hits the 30-day wall. After it passes, `IsPaired` returns false → the channel routes the next message to `PolicyNeedsPairing` (`internal/channels/channel.go:335,374`) → the user must re-pair. Worse, `ListPaired` (the Nodes-page data source) **deletes** the expired row on first read (`internal/store/pg/pairing.go:231`; `internal/store/sqlitestore/pairing.go:204`): `DELETE FROM paired_devices WHERE expires_at IS NOT NULL AND expires_at < NOW()`.

The TTL is **not configurable** — no pairing field exists in `internal/config/config.go` (grep empty); the value is the two duplicate consts.

This SRS owns the **sliding-expiry** fix: renew `expires_at` on legitimate activity so active devices never expire, make the TTL + renewal window operator-tunable (incl. a never-expire option), and surface the expiry in the API + Nodes UI so "why must I re-register" is observable instead of a silent day-30 eviction. It composes with the existing pairing/approval/revoke flow (`internal/gateway/methods/pairing.go`) and channel policy gate (`internal/channels/channel.go`) **unchanged** — no new endpoint, no new auth surface, no schema migration.

---

## 2. Scope

**In scope**:

- **Sliding renewal of `expires_at` on activity.** When a paired device passes the `IsPaired` gate on a real message, the store bumps `expires_at = now + pairedDeviceTTL`, so an active device is never evicted. Renewal runs inside the store `IsPaired` (the single gate every channel + the browser flow already calls), gated by a **renewal window** (only renew when within the last fraction of the TTL) so it does **not** write once per message.
- **Configurable TTL + renewal window.** Move `pairedDeviceTTL` out of the two hardcoded consts into config, threaded through `NewPGPairingStore` / `NewSQLitePairingStore` and the store factories. Default 30 days (unchanged). A TTL of `0` (or a dedicated "never" value) means **no expiry** — operator opt-out, matching the migration-021 intent that `NULL expires_at = no expiry` (`migrations/000021_paired_devices_expiry.up.sql:2`).
- **Surfacing expiry.** Add `ExpiresAt` to `PairedDeviceData` (`internal/store/pairing_store.go:18-25`) and to the `ListPaired` SELECT (`internal/store/pg/pairing.go:227`; `internal/store/sqlitestore/pairing.go:198`) so `pairing.list` (`internal/gateway/methods/pairing.go:167-175`) returns it and the Nodes page can show "expires / expired / never".
- **Nodes UI.** Render the expiry column + an "expiring soon" / "expired" / "never" badge in the paired-devices table (`ui/web/src/pages/nodes/nodes-page.tsx:104-148`).
- **i18n** for the new UI strings (en/vi/zh).

**Out of scope**:

- **The pairing *approval* flow** (`RequestPairing`/`ApprovePairing`/`DenyPairing`/`RevokePairing`, `internal/store/pairing_store.go:29-32`, `internal/gateway/methods/pairing.go`). Unchanged — approve still sets the initial `expires_at`; revoke still hard-deletes.
- **The per-channel pairing *request* UX** (Telegram `/pair`, WhatsApp/Slack/Discord/Feishu/Zalo pairing messages). Unchanged.
- **`pairing_requests` (pending) expiry / `codeTTL`.** The 60-minute pairing-code TTL (`internal/store/pg/pairing.go:19`) is a separate, correct short-lived-code expiry and is **not** a sliding window. Untouched.
- **Per-device or per-channel TTL overrides.** A single global TTL (config) is in scope; per-pairing custom expiry is a follow-up.
- **Schema migration.** The `expires_at` column already exists in both DBs (PG `migrations/000021_paired_devices_expiry.up.sql:3`; SQLite `internal/store/sqlitestore/schema.sql:539` + `schema.go:121`). No DDL, no `RequiredSchemaVersion` bump, no SQLite `SchemaVersion` bump, no `schema.go` migration patch (FR-05 verification only).
- **New WS/HTTP endpoints or auth changes.** Renewal is an in-store side-effect of the existing `IsPaired` read; the API surface is additive only (`expires_at` added to an existing response).
- **A batch "renew all" admin job.** Sliding renewal is per-activity; a sweep job that extends idle devices would defeat the "evict stale pairings" purpose and is out of scope.

---

## 3. Functional Requirements

### FR-00: Active paired devices renew their expiry (sliding window) — root-cause fix

`IsPaired` is the single gate every channel + the browser flow call to decide a device is bound (`internal/channels/channel.go:335,374`; `internal/channels/telegram/handlers.go:159-160,266,321,377`; `internal/http/auth.go:281`; `internal/gateway/router.go:353`; `internal/gateway/methods/pairing.go:249`). Today it is read-only. It must **renew** `expires_at` when it admits a device, so an in-use device does not fall off at day 30.

Renewal is **gated by a renewal window** so it does not fire once per message:

| State on `IsPaired` hit | Behaviour |
|---|---|
| `expires_at IS NULL` (never-expire pairing) | no write (already never-expiring) |
| `expires_at - now > renewalWindow` (not yet near expiry) | no write (still comfortably valid) |
| `0 < expires_at - now <= renewalWindow` (within renewal window) | bump `expires_at = now + pairedDeviceTTL` |
| `expires_at <= now` | (already excluded by the existing `expires_at > NOW()` predicate) — no renewal; row pruned by `ListPaired` as today |

`renewalWindow` defaults to the last 25% of `pairedDeviceTTL` (≈7.5 days of 30). This bounds the renewal write to **at most ~once per week** per active device even under high message volume, instead of once per message. The renewal UPDATE is **best-effort, non-blocking to the gate**: `IsPaired` still returns its true paired/not-paired verdict based on the SELECT; a renewal-write failure is logged (`slog.Warn("security.pairing_renew_failed", …)`) and **does not** flip the device to not-paired (the `IsPaired` fail-open posture at `channel.go:337-339` is preserved).

Implement in **both** stores (dual-DB rule):
- PG `IsPaired` (`internal/store/pg/pairing.go:163`): after the COUNT SELECT returns `count > 0`, run a conditional UPDATE.
- SQLite `IsPaired` (`internal/store/sqlitestore/pairing.go:150`): mirror.

The conditional PG renewal UPDATE (single statement, tenant-scoped via the existing `tenantIDForInsert(ctx)`):

```sql
UPDATE paired_devices
SET expires_at = NOW() + $ttl
WHERE sender_id = $1 AND channel = $2 AND tenant_id = $3
  AND expires_at IS NOT NULL
  AND expires_at <= NOW() + $renewalWindow
```

(The SQLite mirror uses `?` params and `datetime('now', '+N seconds')` for both the new expiry and the window bound — consistent with the existing SQLite `datetime('now')` usage in `sqlitestore/pairing.go`.)

Acceptance criteria:

- [x] A paired device whose `expires_at` is **within** `renewalWindow` of now, on the next `IsPaired` hit, has its `expires_at` advanced to `now + ttl`. _(`TestSQLitePairingStore_RenewsWithinWindow` — SQLite; PG mirrors line-for-line)_
- [x] A paired device whose `expires_at` is **outside** `renewalWindow` (freshly approved) is **not** written on an `IsPaired` hit (no churn). _(`TestSQLitePairingStore_NoRenewalOutsideWindow` — 100 hits, expiry byte-identical)_
- [x] A device with `expires_at IS NULL` is never renewed (and never expires). _(`TestSQLitePairingStore_NeverExpireNull`)_
- [x] A renewal-write error does **not** change `IsPaired`'s verdict (the device stays admitted if it was paired; logged at warn, not returned). _(by inspection — `pg/pairing.go` IsPaired computes `paired` from the COUNT SELECT *before* the renewal UPDATE, and the UPDATE error is only `slog.Warn`'d, not returned; SQLite identical)_
- [x] Over a burst of consecutive `IsPaired` hits on a freshly-approved device, at most **one** renewal write occurs (the renewal-window guard holds). _(`TestSQLitePairingStore_WindowGateStopsRepeatRenewal` — 2nd immediate hit is a no-op; `_NoRenewalOutsideWindow` — 100 hits no-op)_

---

### FR-01: Configurable TTL + renewal window (operator-tunable; `0` = never expire)

`pairedDeviceTTL` is a hardcoded package-level const in two stores (`internal/store/pg/pairing.go:20`, `internal/store/sqlitestore/pairing.go:24`). Make it configurable so an operator can extend, shorten, or disable device expiry without a code change.

| Config field | Default | Meaning |
|---|---|---|
| `pairedDeviceTTL` (duration) | `720h` (30 days) | lifetime granted on approve and on each renewal |
| `pairedDeviceRenewalWindow` (duration) | `180h` (≈25% of TTL) | the near-expiry window inside which a hit renews (FR-00) |

Semantics:
- `pairedDeviceTTL = 0` → **never expire**. `ApprovePairing` writes `expires_at = NULL` (honouring the migration-021 "NULL = no expiry" contract, `migrations/000021_paired_devices_expiry.up.sql:2`); `IsPaired`'s `expires_at IS NULL OR expires_at > NOW()` predicate already admits it forever; FR-00 renewal no-ops on NULL rows.
- `pairedDeviceRenewalWindow` is clamped to `[0, pairedDeviceTTL]`; `0` disables renewal (devices expire exactly at TTL — re-introduces today's behaviour, useful if an operator wants forced periodic re-authorization).

Wiring (additive — no existing field renamed):
1. Config field on `GatewayConfig` (or a new small `PairingConfig`), parsed from `config.json` / env like other durations (`internal/config/config.go`, JSON5 via `internal/config`).
2. Thread through the store constructors: `NewPGPairingStore(db, ttl, renewalWindow)` (`internal/store/pg/pairing.go:31`) and `NewSQLitePairingStore(db, ttl, renewalWindow)` (`internal/store/sqlitestore/pairing.go:34`). When `ttl <= 0`, the store treats expiry as "never" (`expires_at = NULL` on approve; no renewal).
3. Factory wiring: `internal/store/pg/factory.go:36` (`Pairing: NewPGPairingStore(db)`) and `internal/store/sqlitestore/factory.go:54` read the resolved config and pass `ttl`/`renewalWindow` into the constructors. Zero/absent config falls back to the current `30d` / 25%-window constants so behaviour is byte-identical for operators who set nothing.

Acceptance criteria:

- [x] Setting `pairedDeviceTTL = 0` in config makes newly-approved pairings `expires_at = NULL` (never-expire); they remain paired indefinitely and `ListPaired` never prunes them. _(`TestSQLitePairingStore_ConfigurableTTLNever` — approve writes NULL; prune DELETE has `WHERE expires_at IS NOT NULL` so NULL rows are excluded by inspection)_
- [x] Setting `pairedDeviceTTL = 168h` (7 days) makes approve set `expires_at = now + 7d`. _(`TestPairingConfig_Durations` "explicit ttl" + `TestSQLitePairingStore_ConfigurableTTLFinite` proves finite approve)_
- [x] Absent/zero config keeps today's behaviour: 30-day TTL, 25% renewal window. _(`TestPairingConfig_Durations` "empty defaults" + `TestChannelsConfig_PairingDefault`; constructor default path)_
- [x] `pairedDeviceRenewalWindow` clamped to `[0, ttl]`; `0` disables renewal (FR-00 writes nothing). _(`TestPairingConfig_Durations` "window clamped to ttl" + "explicit zero window disables renewal")_
- [x] The two hardcoded `pairedDeviceTTL` consts are replaced by the constructor params (no second source of truth that can drift — the exact defect class this SRS addresses). _(by inspection — `pairedDeviceTTL` const removed from both `pg/pairing.go` and `sqlitestore/pairing.go`; replaced by constructor `ttl` param + `store.DefaultPairedDeviceTTL`/`config.defaultPairedDeviceTTL` fallbacks)_

---

### FR-02: Surface `expires_at` in the `pairing.list` response + Nodes UI

`PairedDeviceData` (`internal/store/pairing_store.go:18-25`) today carries `SenderID/Channel/ChatID/PairedAt/PairedBy/Metadata` but **no** `ExpiresAt`, and the `ListPaired` SELECT omits the column (`internal/store/pg/pairing.go:227-236`; `internal/store/sqlitestore/pairing.go:198`). So the Nodes page cannot show why/when a device will lapse. Add it.

Store + DTO:
- `PairedDeviceData` gains `ExpiresAt *int64 \`json:"expires_at,omitempty"\`` (`*int64`: nil = never-expire / unknown; non-nil = Unix-ms). A `*int64` (not `int64`) so "never" (NULL) is distinguishable from a zero timestamp — mirrors the `*string`/`*time.Time` nullable-column convention in CLAUDE.md.
- `pairedDeviceRow` (`internal/store/pg/pairing.go:189-197`) gains `ExpiresAt *time.Time`; the `ListPaired` SELECT adds `expires_at` (`pg/pairing.go:234`; `sqlite:205`); the row→DTO map sets `ExpiresAt` from the nullable value (nil when the column is NULL).

WS handler: `handleList` (`internal/gateway/methods/pairing.go:167-175`) already returns `paired` straight from `ListPaired`, so `expires_at` flows to the client with no handler change once the DTO carries it.

UI:
- `useNodes.PairedDevice` (`ui/web/src/pages/nodes/hooks/use-nodes.ts:17-23`) gains `expires_at?: number | null`.
- The paired-devices table (`nodes-page.tsx:104-148`) gains an **"Expires"** column rendering one of: `never` (when null/0), a relative date, or an "expired"/"expiring soon" badge (e.g. within `renewalWindow`). Respects the mobile table rule (`overflow-x-auto` + `min-w-[600px]` already present at `nodes-page.tsx:110-111`).

Acceptance criteria:

- [x] `pairing.list` response items include `expires_at` (Unix-ms) for finite-expiry pairings; the field is **absent** (or null) for never-expire pairings. _(`TestSQLitePairingStore_ConfigurableTTLFinite` asserts `ExpiresAt` surfaced; `_ConfigurableTTLNever` asserts nil; PG `ListPaired` SELECT + `pairedDeviceRow.ExpiresAt *time.Time` mirror by inspection)_
- [ ] The Nodes paired-devices table shows an "Expires" column: `never` badge for NULL, a date for finite, and an "expired"/"expiring soon" badge when within the renewal window / past. _(manual — needs browser; code-complete: `nodes-page.tsx` Expires column + `expires.never`/`expired`/`expiringSoon` badges, `pnpm build` green)_
- [x] `PairedDeviceData.ExpiresAt` is `*int64` so NULL never-expiry rows are distinguishable from a zero timestamp. _(by inspection — `internal/store/pairing_store.go` `ExpiresAt *int64 \`json:"expires_at,omitempty"\`)_

---

### FR-03: i18n (en / vi / zh)

New Nodes-UI strings (expiry column + badges) go into the `nodes` namespace (`ui/web/src/i18n/locales/{en,vi,zh}/nodes.json`, the namespace `nodes-page.tsx:22` already uses), per the project 3-locale rule and `004` FR-06.

| Key | English | Vietnamese | Chinese |
|-----|---------|------------|---------|
| `columns.expires` | Expires | Hết hạn | 到期 |
| `expires.never` | Never | Không bao giờ | 永不过期 |
| `expires.expired` | Expired | Đã hết hạn | 已过期 |
| `expires.expiringSoon` | Expiring soon | Sắp hết hạn | 即将到期 |

Acceptance criteria:

- [x] All four keys exist in `nodes.json` for `en`, `vi`, `zh` with identical key sets. _(added `columns.expires` + `expires.{never,expired,expiringSoon}` to all 3 locale files)_
- [x] No raw key renders (`nodes` namespace already registered — no `004`-FR-07-style mismatch). _(by inspection — `nodes-page.tsx:22` already uses `useTranslation("nodes")`; `pnpm build` green)_

---

### FR-04: Authorization & tenant scope (unchanged envelope)

Renewal is a side-effect of the existing `IsPaired` read; no new endpoint, no new auth surface. The renewal UPDATE carries the same `tenant_id = $N` binding (`tenantIDForInsert(ctx)`) as every other pairing write, so a caller cannot renew another tenant's pairing. The channel-policy fail-open posture (`channel.go:337-339`, `security.pairing_check_failed` assuming paired) is **preserved** — a renewal error does not change the gate verdict.

Acceptance criteria:

- [x] No new WS/HTTP endpoint or auth change. _(by inspection — renewal is an in-store side-effect of the existing `IsPaired` read)_
- [x] The renewal UPDATE is tenant-scoped (`tenant_id = $N`); a renewal hit on tenant A cannot bump tenant B's `expires_at`. _(`TestSQLitePairingStore_RenewalTenantIsolation`)_
- [x] `IsPaired` fail-open behaviour on store error (`channel.go:337-339`) is unchanged; a renewal-write failure is logged, not surfaced as "not paired". _(by inspection — renewal UPDATE error is `slog.Warn("security.pairing_renew_failed")` only; the gate verdict + the existing `security.pairing_check_failed` fail-open are untouched)_

---

### FR-05: No schema migration (verification only)

The `expires_at` column already exists in both DBs — added for PG by `migrations/000021_paired_devices_expiry.up.sql:3` and present in the SQLite fresh-DB schema at `internal/store/sqlitestore/schema.sql:539` (+ `schema.go:121`). Sliding renewal is a store-layer UPDATE on that existing column; configurable TTL is constructor plumbing; expiry surfacing is a SELECT + DTO addition. **No DDL, no version bump.**

Acceptance criteria:

- [x] No new file under `migrations/`; `RequiredSchemaVersion` stays `90` (`internal/upgrade/version.go:5`). _(by inspection — no `migrations/` file added; version.go untouched)_
- [x] No SQLite `schema.go` migration patch added; `SchemaVersion` stays `47` (`internal/store/sqlitestore/schema.go:19`). _(by inspection — schema.go untouched)_
- [x] The desktop `sqliteonly` build is green (the renewal + config + DTO change is shared code; no SQLite-only DDL). _(`go build -tags sqliteonly ./...` ✓)_

---

### FR-06: Group approval cache is TTL-bounded (renewal + expiry enforce reach groups)

**Gap found at live verification:** the in-memory group-approval cache `approvedGroups` (`internal/channels/channel.go:187`) admits a group on its first `IsPaired` hit (`MarkGroupApproved`) and every subsequent message hits `IsGroupApproved` → `PolicyAllow` **before** reaching `IsPaired` (`channel.go:369-370`). The cache had **no TTL**, so for groups `IsPaired` was never called again → (a) the FR-00 sliding renewal never fired for groups (their `expires_at` stayed at the approve value and lapsed), and (b) a revoked/expired group kept working for the whole gateway process lifetime (the cache never re-checked `expires_at`). This is why a WhatsApp group showed `expires ≈ now` ("just now" in the Nodes UI) yet still accepted messages. DMs were unaffected (no cache → `IsPaired` every message → renewal fired).

Fix: make the cache TTL-bounded so the next message after the TTL re-validates via `IsPaired` — which renews an in-window device (FR-00) **or** evicts an expired/revoked one.

| Cache state on message | Behaviour |
|---|---|
| no entry | fall through to `IsPaired` (renew / evict) → `MarkGroupApproved` |
| entry fresh (`now - approvedAt <= groupApproveCacheTTL`) | `PolicyAllow` (current fast path, no DB) |
| entry stale (`now - approvedAt > groupApproveCacheTTL`) | evict entry → fall through to `IsPaired` (renew / evict) → re-`MarkGroupApproved` |

`groupApproveCacheTTL` is `60 * time.Minute` (`channel.go`; note — the 0.3 revision row says 10 min, but the shipped constant from commit daf7dd02 is 60 min). Short enough to renew well before the 30-day wall; long enough to keep the DB-skip benefit on active groups. The change is centralized in `IsGroupApproved`/`MarkGroupApproved`, so every caller benefits: WhatsApp (`whatsapp/policy.go:16` → `CheckGroupPolicy`) and Telegram (`handlers.go:264,319,375`).

Acceptance criteria:

- [x] `approvedGroups` stores `chatID → time.Time` (approved-at), not a bare `true`. _(by inspection — `channel.go:187` + `MarkGroupApproved`)_
- [x] `IsGroupApproved` returns false (and evicts the entry) when the cached approval is older than `groupApproveCacheTTL`; returns true when fresh. _(`TestGroupApproveCache_TTL`)_
- [x] A stale/unknown group falls through to `IsPaired` so FR-00 renewal fires for groups and an expired group is evicted (not admitted forever). _(by inspection — `CheckGroupPolicy` `channel.go:369-385` calls `IsPaired` then re-`MarkGroupApproved` on a cache miss; `IsGroupApproved` now misses when stale)_
- [x] `go build ./...`, `go build -tags sqliteonly ./...`, `go vet ./internal/channels/...` green. _(`go build` PG+sqliteonly ✓, vet clean)_

---

### FR-07: Active-node sliding guarantee — activity extends expiry; no expiry during active use

**User-facing guarantee (operator request):** as long as a node — **especially a WhatsApp DM and a WhatsApp group** — has message activity, its `expires_at` is extended by the sliding renewal (FR-00), so it **never expires during periods of activity**. Only **inactivity beyond the TTL** expires a node. This is the property the "register nodes again after some time" report violated, and it must hold for every channel, not just DMs.

The guarantee rests on the renewal hook (`IsPaired`) being reached while the device is in-window. Three reach paths:

| Node type | Renewal-hook reach | Why active ⇒ no expiry |
|---|---|---|
| **DM** (WhatsApp/Telegram/…) | `CheckDMPolicy` has **no cache** → `IsPaired` called on **every** message (`channel.go:335`) | every active msg renews while in-window |
| **Group row** (`group:<chatID>`) | `approvedGroups` cache HIT does **not** refresh the stamp (`channel.go`) → entry ages out exactly `groupApproveCacheTTL` (60 min) after the last miss → next msg re-runs `IsPaired` for the group row | an active group is re-validated (renewed) within ≤60 min of entering the 7.5-day renewal window — far inside the window, so it never lapses |
| **Member's personal row** (FR-08) | every group msg fires `TouchGroupMember` (`CheckGroupPolicy` entry) → member's own `IsPaired` re-run, TTL-gated to ≤1/hour (`memberRenewCacheTTL`) | a member active **only in groups** still renews their personal pairing — DM and group activity feed ONE expiry |

Critical property verified: a cache **hit does not re-stamp** the entry, so even a group messaged every few seconds still ages out after the TTL and re-runs `IsPaired`. If hits re-stamped, a constantly-active group would never re-validate and would expire — the exact symptom the cache-TTL fix (FR-06) closed.

Acceptance criteria:

- [x] A cache HIT on an approved group does NOT call `IsPaired` (fast path) and does NOT refresh the approval stamp. _(`TestCheckGroupPolicy_ActiveGroupRevalidatesAfterCacheTTL` — fresh-cache burst leaves `isPairedCalls` at 1)_
- [x] After the cache entry ages past `groupApproveCacheTTL`, the next group message re-runs `IsPaired` (the renewal hook) and re-approves. _(same test — `isPairedCalls` 1→2 after stale injection)_
- [x] DMs have no approval cache → `IsPaired` runs on every DM message (renewal fires every active msg while in-window). _(by inspection — `CheckDMPolicy` `channel.go:320-347` has no `IsGroupApproved`/cache short-circuit)_
- [x] `whatsapp.go:485` `MarkGroupApproved` is the join-rule **auto-approval** path only (one-time, on group config), not a per-message re-stamp. _(by inspection — `whatsapp.go:460-486` is the join-rule branch)_
- [ ] Live: an active WhatsApp group whose `expires_at` is within the renewal window has its expiry extended within ≤`groupApproveCacheTTL` of a message (manual — needs a device in-window + a message after the cache TTL; the staged live test was cache-blocked because the two test messages were <10 min apart).

---

### FR-08: Unified member sliding expiry — group activity renews the member's personal pairing

**Gap found at live verification (2026-08-17):** operator reported group chats still worked for members whose pairings had expired, while those same members' **personal (DM) chats** demanded re-pairing. Two causes, both verified against the live `whatsapp-reski` instance (`channel_instances`: `group_policy = "open"`, `dm_policy = "pairing"`):

1. **`group_policy = "open"` bypasses the renewal hook entirely.** `CheckGroupPolicy` returns `PolicyAllow` on the `default` branch without ever calling `IsPaired` (`channel.go`). Group activity therefore renewed nothing, and *any* member — expired, revoked, or never-paired — was admitted for the life of the process. ("Expired nodes still can chat in group.")
2. **Group and personal pairings are separate rows.** Even under `group_policy = "pairing"`, the group check runs `IsPaired("group:<chatID>", …)` — the **group's** row. A member active exclusively in groups never re-ran `IsPaired` for their own senderID, so their personal row hit the 30-day wall and the next DM forced re-pairing. ("Personal chat has to re-authenticate.")

Fix: `CheckGroupPolicy` now fires `TouchGroupMember(ctx, senderID)` **before** evaluating the group policy. It re-runs `IsPaired` for the **member's own senderID** — the FR-00 renewal hook — so any group message from a paired member advances that member's personal `expires_at`. Result is deliberately ignored: renewal is best-effort and must never gate the group message (whether a member may speak in a group is the group policy's decision, not the renewal's).

| Property | Behaviour |
|---|---|
| single expiry per member | DM msgs (FR-07 DM path) **and** group msgs (this FR) renew the **same** `paired_devices` row (`sender_id = <member JID>`) |
| fires on every group policy | `"open"` / `"allowlist"` / `"pairing"` / default — `TouchGroupMember` runs before the policy switch |
| rate-limited | per-member `memberRenewed` cache (`senderID → time.Time`, `memberRenewCacheTTL = 60 min`) — a busy group adds ≤1 member `IsPaired` per member per hour, not per message |
| never gates the message | return value ignored; unpaired/unknown members unaffected — group admission still decided by the group policy |
| revocation-friendly | `ClearGroupMemberRenewal(senderID)` forces immediate re-validation (e.g. post-revoke) |

Acceptance criteria:

- [x] A group message under `group_policy = "open"` from a paired member re-runs `IsPaired` for the member's **own** senderID. _(`TestTouchGroupMember_RenewsOwnPairing` — asserts `lastIsPairedSender == "member1"`, not `group:chat1`)_
- [x] Member renewal is TTL-gated: a 5-message burst → 1 `IsPaired`; after cache expiry → 1 more. _(`TestTouchGroupMember_RateLimited`)_
- [x] An unpaired member under `"open"` is still admitted (renewal never flips the policy verdict). _(`TestTouchGroupMember_UnpairedMemberStillRenews`)_
- [x] Nil pairing service → no-op, no panic. _(`TestTouchGroupMember_NoPairingService`)_
- [x] `ClearGroupMemberRenewal` forces re-validation on the next message. _(`TestClearGroupMemberRenewal`)_
- [x] `go test -race ./internal/channels/` green (140/140, incl. the updated `TestCheckGroupPolicy_ActiveGroupRevalidatesAfterCacheTTL`).
- [ ] Live: a member whose personal row is inside the renewal window sends a WhatsApp group message → `expires_at` advances within ≤`memberRenewCacheTTL` (manual).

#### FR-08.1: Behaviour matrix — an EXPIRED member sends a message

"Expired" = the member's personal row has `expires_at < NOW()` (`IsPaired` COUNT → 0). The renewal UPDATE is gated on `paired == true` (`pg/pairing.go` IsPaired — the UPDATE runs only inside the `if paired` branch), so **an already-expired row can never resurrect itself by activity** — re-pairing is the only recovery. Extension works only while the row is still live (inside the renewal window = last 25% of TTL).

| # | Scenario | What happens | Member row: expired or not | Member row: extended or not |
|---|---|---|---|---|
| 1 | **Personal (DM) chat** — `dm_policy = "pairing"` | `CheckDMPolicy` → `IsPaired(member)` → COUNT 0 → `PolicyNeedsPairing` → pairing-reply message sent; **member cannot chat** until operator approves a new pairing code | Still expired (row untouched; pruned on next `ListPaired` — see FR-02) | **No** — renewal UPDATE gated on `paired == true`, which failed |
| 2 | **WhatsApp group, `group_policy = "open"`** | `TouchGroupMember` → `IsPaired(member)` → false, no renewal (fire-and-forget, result ignored) → policy `"open"` → `PolicyAllow` — **member CAN still chat in the group**; group access ignores member pairing by design | Still expired | **No** — renewal attempted (FR-08 hook fired) but gated on `paired == true`; an expired row cannot self-resurrect |
| 3 | **WhatsApp group, `group_policy = "pairing"` (not open)** | `TouchGroupMember` → member `IsPaired` → false, no effect on verdict. Group admission decided by the **group row** (`group:<chatID>`): group row valid (or in allowlist) → `PolicyAllow`, member can chat; group row also absent/expired → `PolicyNeedsPairing` → pairing request issued for the **group**, not the member | Still expired | **No** for the member row (same `paired == true` gate). The **group row** may extend (its own renewal, FR-00/FR-06) — but that is a different `paired_devices` row |

Design consequence (accepted): under `group_policy = "open"`, an expired member keeps group access forever (open = trust the group invite, no per-member gate). If the operator wants expired members locked out of groups too, set `group_policy = "allowlist"` (member must be in the channel allowlist) — not `"pairing"` alone, which gates the **group**, not each member.

#### FR-08.2: Behaviour matrix — a NON-EXPIRED member sends a message

"Not expired" = the member's personal row has `expires_at > NOW()` (COUNT ≥ 1 → `paired == true`). Two sub-states, because the renewal UPDATE fires **only inside the renewal window** (`expires_at <= NOW() + renewalWindow`, default = last 25% of TTL):

- **Healthy** — `expires_at` outside the renewal window (> 75% of TTL remaining): `IsPaired` returns true, but the window predicate fails → **no UPDATE**. Correct: the row is nowhere near lapsing; the write would be wasted.
- **In renewal window** — `expires_at` within the last 25% of TTL: UPDATE fires → `expires_at = NOW() + TTL` (full 30d re-granted).

**Timing values (implementation defaults, FR-01):**

| Setting | Default | Where configured | Meaning |
|---|---|---|---|
| `device_ttl` | `720h` (30 days) | `channels.pairing.device_ttl` in config.json (absent → default) | lifetime granted on approve **and on each renewal** — renewal re-grants the FULL TTL from the renewing message, not a partial add |
| `renewal_window` | `180h` (7.5 days = TTL/4) | `channels.pairing.renewal_window` (absent → TTL/4; `"0"` → renewal disabled) | near-expiry window that arms the renewal UPDATE |
| `memberRenewCacheTTL` | `60 min` | code const (`internal/channels/channel.go`) | how often a group message re-runs the member's `IsPaired` (rate limit, not a security window) |
| `groupApproveCacheTTL` | `60 min` | code const (`internal/channels/channel.go`) | how often a group message re-runs the group row's `IsPaired` |

With defaults: a member is extended **the moment a qualifying message arrives while ≤7.5 days remain**, and the extension sets `expires_at = message time + 30 days`. A member active at least once every 22.5 days (30 − 7.5) never lapses. Verified in code: the renewal UPDATE writes `now.Add(s.ttl)` (`pg/pairing.go` / `sqlitestore/pairing.go` `IsPaired`), gated on `paired == true` AND `expires_at <= now + renewalWindow` — an expired row (COUNT 0) never reaches the UPDATE.

Live deployment note (2026-08-17): the running gateway's `config.json` has **no `channels.pairing` block** → defaults are active (30d TTL, 7.5d window). Operator tuning example: add under `channels`:

```json5
"pairing": { "device_ttl": "720h", "renewal_window": "180h" }
```

| # | Scenario | What happens | Member row: expired or not | Member row: extended or not |
|---|---|---|---|---|
| 1 | **Personal (DM) chat** — `dm_policy = "pairing"` | `CheckDMPolicy` → `IsPaired(member)` on **every** msg (no cache) → `PolicyAllow` → member chats normally | Not expired | **Yes, but only when in the renewal window** — every DM msg evaluates the window; healthy rows stay untouched (no write), in-window rows extend to `now + 30d` on the very first msg |
| 2 | **WhatsApp group, `group_policy = "open"`** | `TouchGroupMember` (TTL-gated: first msg, then ≤1/hour/member) → member `IsPaired` → true → policy `"open"` → `PolicyAllow` → member chats normally | Not expired | **Yes, when in the renewal window** — renewal fires on the first group msg per `memberRenewCacheTTL` (60 min). A member active **only in groups** keeps their personal row alive (the FR-08 fix). Group activity alone, pre-FR-08, let this row lapse |
| 3 | **WhatsApp group, `group_policy = "pairing"` (not open)** | `TouchGroupMember` → member renewal (same as #2). Group admission via the **group row** (`group:<chatID>`): cache fresh → `PolicyAllow` (no DB); cache stale/miss → `IsPaired(group)` → valid → re-cache + allow, invalid → `PolicyNeedsPairing` for the group | Not expired | **Yes, when in the renewal window** — member row via `TouchGroupMember` (independent of the group verdict). The **group row** re-validates on its own cadence (`groupApproveCacheTTL` = 60 min) and extends itself when in-window (FR-00/FR-06) |

Net effect (the FR-08 guarantee): for a non-expired member, **any** qualifying activity — DM, open group, or pairing-gated group — feeds the **same single sliding expiry**. The row only ever reaches the expired state through `memberRenewCacheTTL` + `groupApproveCacheTTL` (≤ 60 min of re-validation lag) plus the full renewal window (~7.5d at default TTL) of total inactivity — i.e. an active member cannot lapse.

| Sub-state (row before msg) | DM msg | Group msg (`open` or `pairing`) |
|---|---|---|
| Healthy (> 22.5 days left) | no write — `expires_at` unchanged | no write — `expires_at` unchanged (renewal hook may not even fire if member cache is fresh) |
| In renewal window (≤ 7.5 days left) | extended → `msg time + 30 days` | extended → `msg time + 30 days` (on the first msg after the member cache expires, ≤ 60 min lag) |

---

## 4. System Impact

- **Channel policy gate** (`internal/channels/channel.go`): `CheckGroupPolicy` fires `TouchGroupMember` (FR-08) + TTL-bounded `IsGroupApproved`/`MarkGroupApproved` (FR-06). New fields: `memberRenewed sync.Map`; new consts `memberRenewCacheTTL`, `groupApproveCacheTTL`. No store-interface change — renewal rides the existing `IsPaired`.
- **Store interface + DTO** (`internal/store/pairing_store.go`): add `ExpiresAt *int64` to `PairedDeviceData` (FR-02). No new interface method — renewal is internal to `IsPaired`.
- **PG store** (`internal/store/pg/pairing.go`): (a) `IsPaired` gains a conditional renewal UPDATE after the COUNT SELECT (FR-00); (b) `pairedDeviceTTL` const → constructor param `ttl`, add `renewalWindow`; `ApprovePairing` writes `NULL` when `ttl <= 0` (FR-01); (c) `pairedDeviceRow` + `ListPaired` SELECT add `expires_at` → map to `*int64` (FR-02). Constructor `NewPGPairingStore(db, ttl, renewalWindow)`.
- **SQLite store** (`internal/store/sqlitestore/pairing.go`): mirror (a)/(b)/(c). Constructor `NewSQLitePairingStore(db, ttl, renewalWindow)`.
- **Store factories** (`internal/store/pg/factory.go:36`, `internal/store/sqlitestore/factory.go:54`): resolve the config TTL/window and pass into the constructors; fall back to `30d` / 25% when config absent.
- **Config** (`internal/config/config.go` + JSON5 loader): add `pairedDeviceTTL` / `pairedDeviceRenewalWindow` durations (default `720h` / `180h`).
- **WS handler** (`internal/gateway/methods/pairing.go`): no logic change — `handleList` already returns `ListPaired` verbatim, so `expires_at` flows once the DTO carries it. (Optional: nothing else.)
- **Web UI** (`ui/web/src/pages/nodes/hooks/use-nodes.ts`, `nodes-page.tsx`): `PairedDevice.expires_at?`; "Expires" column + badges (FR-02/FR-03).
- **i18n**: 4 keys × `en`/`vi`/`zh` in `nodes.json` (FR-03).
- **No schema migration**, no new error-code registration (FR-05), no new endpoint.

## 5. Test Plan

- **Store unit test (PG + SQLite) — renewal (FR-00):** (a) device within `renewalWindow` → `expires_at` advanced to `now+ttl` on `IsPaired`; (b) device outside window → no write; (c) `expires_at IS NULL` → no write, never expires; (d) renewal-UPDATE failure → `IsPaired` verdict unchanged; (e) 100-hit burst on a fresh device → ≤1 renewal write. `-race`.
- **Store unit test — configurable TTL (FR-01):** `ttl=0` → approve writes `expires_at = NULL`; `ttl=168h` → approve writes `now+7d`; absent config → 30d (factory default).
- **Store unit test — tenant isolation (FR-04):** a renewal hit scoped to tenant A does not bump tenant B's `expires_at`.
- **DTO/handler test — expiry surfaced (FR-02):** `ListPaired`/`pairing.list` returns `expires_at` (Unix-ms) for finite rows and omits/nulls it for never-expire rows.
- **Frontend (manual, no `@testing-library/react` per `007` §0.7):** Nodes table renders the Expires column + badges; `never` for NULL, date for finite, "expiring soon"/"expired" within window.
- **Build/safety:** `go build ./...`, `go build -tags sqliteonly ./...`, `go vet ./internal/store/... ./internal/config/... ./internal/channels/...`, `pnpm build` (ui/web).
- **Manual (live):** pair a device on the master tenant; before the fix it dies at day 30; after the fix, confirm (via the new Expires column) that an active device's `expires_at` keeps moving forward and it is **not** pruned, while an idle device (no messages past TTL) **is** evicted as before.

## 6. Decision Log (locked)

| Decision | Rationale |
|----------|-----------|
| **Renew inside the store `IsPaired` (single point), not per-channel.** | `IsPaired` is the one gate every channel + the browser flow already call (`channel.go:335,374`, telegram, `http/auth.go:281`, `router.go:353`, `methods/pairing.go:249`). Renewing there covers all paths with one edit. Per-channel renewal (`CheckDMPolicy`/`CheckGroupPolicy` in `channel.go`) would miss the browser flow, duplicate logic, and still rely on `IsPaired` underneath. |
| **Gate renewal by a renewal window (last ~25% of TTL), not write-per-message.** | Channels call `IsPaired` on every inbound message (telegram even twice — user + group, `handlers.go:159-160`). Renewing unconditionally would add an UPDATE per message. Bounding the renewal to the near-expiry window keeps it to ≤~1 write/week/device while still guaranteeing no active device lapses (it renews the moment it enters the window). |
| **Renewal is best-effort + non-blocking; fail-open preserved.** | `IsPaired` already drives the security gate. A renewal-write failure must not flip a paired device to not-paired — that would lock users out on a transient DB hiccup. The existing fail-open posture (`channel.go:337-339`) is kept; renewal errors are logged, not returned. |
| **Make TTL configurable (`0` = never), not just bump the constant.** | The 30d wall is the reported pain; operators have different trust/retention needs. `0`/NULL honours the migration-021 "NULL = no expiry" contract as a clean opt-out. Keeping a single hardcoded const would re-fix only one number. |
| **Surface `expires_at` in the API + UI.** | The defect was invisible: a device vanishes on day 30 with no warning. Showing the expiry makes the behaviour observable and lets an operator see a device is "expiring soon" before it lapses — directly answering "why must I re-register". |
| **No schema migration.** | The `expires_at` column exists in both DBs (PG migration 021; SQLite `schema.sql:539`). Renewal is an UPDATE on it; configurable TTL is constructor plumbing; expiry surfacing is a SELECT/DTO addition. Adding a migration would be cargo-culting — and would force a `sqliteonly` schema bump that the desktop edition does not need. |
| **Member renewal ignores the `IsPaired` verdict and never gates the group message (FR-08).** | Whether a member may speak in a group is the group policy's decision; the member's own expiry is a background concern. Coupling them (e.g. denying group msgs for expired members under `"open"`) would silently change `open`'s contract — groups are admitted without per-member pairing by design. Renewal is a side-effect of the check, not a gate. |
| **Member renewal is TTL-gated in the channel layer (60 min), not window-gated in the store.** | The store's renewal window (FR-00) bounds the *write* frequency; the channel-side cache bounds the *IsPaired round-trip* frequency. A per-message member lookup would add a DB hit to every group msg in busy groups; 1/hour/member keeps the overhead invisible while still renewing long before the 30-day wall. |
| **Do NOT add a sweep/"renew all" job.** | Sliding renewal is per-activity by design: active devices survive, idle ones lapse (the eviction purpose of the original expiry). A background sweep that extends idle pairings would defeat that purpose and re-grant access to dormant/abandoned devices — a security regression. |

## 7. Risks and Open Questions

| Risk or question | Draft decision |
|---|---|
| Renewing inside a *read* method (`IsPaired`) makes a read have a write side-effect — surprising for future maintainers. | Document it loudly at the method + in this SRS. The pairing store already does lazy writes in read-ish paths (`RequestPairing`/`ListPending`/`ListPaired` all prune `DELETE` expired rows), so an `IsPaired` renewal UPDATE is consistent with the established "lazy cleanup on access" pattern, not a new idiom. |
| `IsPaired` is called on the hot path (per message); an extra UPDATE even when windowed adds load. | Bounded by the renewal window (≤~1 write/week/device). The conditional UPDATE touches a tenant-scoped unique row (`idx_paired_devices_tenant_sender_channel`) — index-backed, single-row. Acceptable; if a very-high-volume tenant proves otherwise, move renewal to the channel-policy in-memory approve-cache path (`MarkGroupApproved`, `channel.go:381`) which already de-dupes per chat. |
| Telegram calls `IsPaired` twice per message (user + group, `handlers.go:159-160`) — double renewal attempt. | The renewal-window guard makes the second attempt a no-op (the first already advanced `expires_at` past the window). No correctness issue; at most redundant SELECTs. Acceptable. |
| Configurable `ttl=0` (never) removes the periodic re-authorization defence-in-depth the original expiry was added for (`migrations/000021` comment "defense-in-depth"). | Operator choice. Default stays 30d. Document the trade-off: never-expire loses the periodic re-auth guarantee; operators who want it keep the TTL. `ttl=0` is opt-in, not the default. |
| `PairedDeviceData.ExpiresAt` as `*int64` may need a WS-consumer update if any client assumes the field is always present. | Additive + `omitempty`; the field is absent when NULL. `use-nodes.ts` treats it as optional. No existing consumer assumes it (it did not exist). Verify the Nodes page handles absent gracefully. |
| Should renewal also fire on the browser pairing status check (`methods/pairing.go:249`, `router.go:353`)? | Yes — those call `IsPaired`, so renewal is automatic (FR-00 covers all callers). A browser session that is active keeps its pairing alive the same way a chat does. No extra work. |
| Pre-021 pairings (created before migration 021) have `expires_at = NULL` and therefore "never expire" already. | Unchanged — they stay never-expiring (consistent with FR-01 `ttl=0`). Only post-021 pairings (which got the 30d wall) are affected by this fix. |

## 8. Implementation Plan

1. **Store constructors + config (FR-01):** add `pairedDeviceTTL` / `pairedDeviceRenewalWindow` to config; thread `ttl, renewalWindow` through `NewPGPairingStore` / `NewSQLitePairingStore`; wire from `pg/factory.go:36` + `sqlitestore/factory.go:54` with `30d`/25% fallback. Replace the two hardcoded consts with the params (keep them only as defaults).
2. **Sliding renewal (FR-00):** in PG `IsPaired` (`pg/pairing.go:163`) and SQLite `IsPaired` (`sqlite:150`), after the COUNT SELECT returns paired, run the conditional renewal UPDATE (within-window only). Best-effort + logged on error. `ApprovePairing` writes `expires_at = NULL` when `ttl <= 0`.
3. **DTO + SELECT (FR-02):** add `ExpiresAt *int64` to `PairedDeviceData`; add `expires_at` to the `ListPaired` SELECT + `pairedDeviceRow` + row→DTO map (PG + SQLite).
4. **i18n (FR-03):** add the 4 keys to `nodes.json` × `en`/`vi`/`zh`.
5. **Web UI (FR-02):** `PairedDevice.expires_at?` in `use-nodes.ts`; "Expires" column + `never`/date/"expiring soon"/"expired" badges in `nodes-page.tsx`.
6. **Tests:** store renewal + configurable-TTL + tenant-isolation + DTO tests per §5.
7. **No-migration verification (FR-05):** confirm no `migrations/` file, `RequiredSchemaVersion=90`, `SchemaVersion=47`, `sqliteonly` build green.
8. **Checklist:** `go build ./...`, `go build -tags sqliteonly ./...`, `go vet ./internal/store/... ./internal/config/... ./internal/channels/...`, `pnpm build` in `ui/web`.
9. **Manual (live):** on the master tenant, confirm an active device's `expires_at` advances (visible in the new Expires column) and is not pruned, while an idle device past TTL is evicted as before.

## 9. Proposed Error Codes

No new canonical error codes. Renewal is a silent best-effort side-effect of an existing read; a renewal-write failure is logged (`security.pairing_renew_failed`) and does not change the gate verdict. The config validation (negative window, non-duration) follows the existing inline `config` validation style. For traceability only:

| Code (inline → proposed canonical mapping) | Meaning |
|------|---------|
| `security.pairing_renew_failed` | The `IsPaired` renewal UPDATE errored (transient DB hiccup); logged at warn, device stays paired (fail-open). Not returned to any caller — observability only. |
| `request.validation_failed` | A malformed `pairedDeviceTTL` / `pairedDeviceRenewalWindow` config value (non-duration, negative window). Existing config-load inline error path. No change. |

No new error code is registered by this feature.

---

## 10. Key files (verified)

- `internal/store/pg/pairing.go:20` — `pairedDeviceTTL` const (to become a constructor param); `:112` `expiresAt := now.Add(pairedDeviceTTL)` (approve); `:163-174` `IsPaired` (renewal hook); `:189-252` `ListPaired` + prune at `:231`; `:265` `MigrateGroupChatID` UPDATE (does not touch `expires_at`).
- `internal/store/sqlitestore/pairing.go:24` — `pairedDeviceTTL` const; `:98` approve expiry; `:150-156` `IsPaired`; `:198-211` `ListPaired` + prune at `:204`; `:249` `MigrateGroupChatID` UPDATE.
- `internal/store/pairing_store.go:18-40` — `PairedDeviceData` (no `ExpiresAt` today) + `PairingStore` interface (no `Touch`/renew method — renewal is internal to `IsPaired`).
- `internal/store/pg/factory.go:36` / `internal/store/sqlitestore/factory.go:54` — store construction (TTL wiring point).
- `internal/config/config.go` — no pairing config today (add point).
- `internal/channels/channel.go:335,374` — generic `IsPaired` policy gate (DM + group); `:337-339` fail-open posture to preserve.
- `internal/gateway/methods/pairing.go:167-175` — `handleList` (returns `ListPaired` verbatim → `expires_at` flows once DTO carries it).
- `ui/web/src/pages/nodes/hooks/use-nodes.ts:17-47` — `PairedDevice` type + `PAIRING_LIST` load.
- `ui/web/src/pages/nodes/nodes-page.tsx:104-148` — paired-devices table (add Expires column + badges).
- `migrations/000021_paired_devices_expiry.up.sql:3` — `expires_at` column (PG, already applied).
- `internal/store/sqlitestore/schema.sql:539` + `schema.go:121` — `expires_at` column (SQLite, already present).
- `internal/upgrade/version.go:5` — `RequiredSchemaVersion = 90` (unchanged).
- `internal/store/sqlitestore/schema.go:19` — `SchemaVersion = 47` (unchanged).
