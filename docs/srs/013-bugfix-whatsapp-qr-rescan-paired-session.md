# Software Requirements Specification: WhatsApp QR Rescan/Re-link Fails (`… no user ID in the client's Store`, `… invalid use of deleted device`, `… must be called before connecting`, false `QR session timed out`, and stuck on `Waiting for QR code…`)

**Project**: GoClaw Gateway
**Release**: 2026.3.0
**Version**: 0.7-draft
**Date**: 2026-08-01
**Status**: Implemented (code-complete + build/vet/test-verified) — covers all five rescan failure modes AND upgrades the stale whatsmeow dependency that was the actual pairing blocker (WA 405 client-outdated). Live on-device rescan verification pending (FR-00/FR-01/FR-03/FR-04/FR-05 manual boxes).
**Difficulty**: Medium
**Estimate**: 0.5 days

---

## Revision History

| Version | Date | Changes |
|---------|------|---------|
| 0.1-draft | 2026-07-31 | Initial root-cause analysis. Confirmed defect spans two layers: (1) backend QR flow keys off connection state (`waAuthenticated`) instead of pairing state (`client.Store.ID`); (2) UI auto-starts rescan with `force_reauth=false` and gates the only `force_reauth=true` control behind the `connected` status the user is not in. whatsmeow precondition cited from module source. |
| 0.2-draft | 2026-07-31 | **Implemented (code-complete + build/vet/test-verified).** Backend: `auth.go` adds `ErrAlreadyPairedDisconnected` sentinel + `StartQRFlow` guards `client.Store.ID != nil` before `GetQRChannel` (returns sentinel, never leaks whatsmeow raw error); `Reauth()` makes `Store.Delete` failure a hard error (was warn-only). `qr_methods.go` adds reason consts + maps failures in `whatsapp.qr.done` (`already_paired_disconnected` / `session_clear_failed` / `qr_start_failed`); Reauth failure now returns early with `session_clear_failed`. UI: `use-whatsapp-qr-login.ts` captures+exposes `reason`; `whatsapp-reauth-dialog.tsx` renders the paired-disconnected message + **Re-link Device** button (`force_reauth=true`) from the error state — the previously-unreachable action. i18n: 3 keys × en/vi/zh in `channels.json`. New `TestStartQRFlow_PairedButDisconnected` + `TestStartQRFlow_NotAuthenticatedFlagDoesNotSuppressPairedGuard`. **Verification:** `go build ./...` ✓, `go build -tags sqliteonly ./...` ✓, `go vet ./internal/channels/...` clean, `go test -race ./internal/channels/whatsapp/ -run TestStartQRFlow` 2/2 ✓, `pnpm build` (ui/web) ✓. `go fix ./...` skipped (same unrelated-modernization-churn convention as 007/009/012). **Deviations:** (1) backend `internal/i18n` keys NOT added — the failure is carried as a machine-readable `reason` code translated client-side, so only web locale files were touched (cleaner than the §5 overspec); (2) desktop N/A — no WhatsApp QR hook in `ui/desktop/frontend/`. **Pre-existing unrelated failure:** `TestMimeToExt` (media_utils, "text/plain"→".txt" vs ".bin") — file untouched by this fix. **Pending:** live rescan on a paired-but-disconnected device (FR-01/FR-03). |
| 0.3-draft | 2026-07-31 | **Second rescan failure mode found + fixed (build/vet/test-verified).** After fixing mode 1, a real signout surfaced a *different* error: `whatsapp connect for QR: invalid use of deleted device`. Root cause §3.8: on `events.LoggedOut`, whatsmeow *itself* calls `cli.Store.Delete()` (`connectionevents.go:44,129`) which nils `Store.ID`, sets `Store.Deleted=true`, and removes the DB row. The v0.2 `Store.ID != nil` guard does not fire (ID is now nil), `GetQRChannel` succeeds (it doesn't check `Deleted`), but `Connect()` returns `store.ErrDeviceDeleted` (`client.go:505`). The deleted client is unrecoverable. Fix: `auth.go` `StartQRFlow` now auto-recreates the client when `client == nil || client.Store.Deleted` (new `clientNeedsRecreate()` helper) via `GetFirstDevice` (returns a fresh `NewDevice` once the row is gone — `sqlstore/container.go:175`). This is **non-destructive auto-recovery** (signout already destroyed the identity), so no operator prompt — unlike the `Store.ID != nil` mode which prompts Re-link. New `TestClientNeedsRecreate` (nil / fresh / deleted). **Verification:** `go build` (pg+sqliteonly) ✓, `go vet ./internal/channels/whatsapp/` clean, `go test -race -run "TestStartQRFlow|TestClientNeedsRecreate"` 6/6 ✓. **Pending:** live rescan after a real signout (FR-00 deleted-device AC). |
| 0.4-draft | 2026-07-31 | **Third rescan failure mode found + fixed (build/vet/test-verified).** After the v0.3 fix, a retry surfaced a *third* error: `whatsapp get QR channel: GetQRChannel must be called before connecting`. Root cause §3.9: `GetQRChannel` returns `ErrQRAlreadyConnected` (`errors.go:31`, checked at `qrchan.go:163`) when the client is already connected. A prior QR attempt (or the reconnect watchdog) leaves an anonymous pre-pairing socket up; the retry calls `GetQRChannel` on that connected client. whatsmeow's contract is `GetQRChannel` → `Connect` (in that order). Fix: `StartQRFlow` now disconnects an already-connected client before `GetQRChannel`. Safe because that line is only reachable when `!IsAuthenticated() && Store.ID == nil` — a live socket there is an anonymous pre-pairing one; paired+connected clients are caught earlier by the `IsAuthenticated()` early-return or the `Store.ID != nil` guard. **Verification:** `go build` (pg+sqliteonly) ✓, `go vet ./internal/channels/whatsapp/` clean, existing QR/recreate tests still 6/6 ✓ (disconnect path needs a live socket → live verification). **Pending:** live multi-retry rescan (FR-00 already-connected AC). |
| 0.5-draft | 2026-07-31 | **Fourth rescan failure mode found + fixed (build/vet/test-verified).** Live retry showed `whatsapp.qr.start` fired 6× in 6s, ending in `QR session timed out — restart to try again` with no QR rendered. Root cause §3.10: each `whatsapp.qr.start` cancels the prior in-flight session (`qr_methods.go` Swap→cancel); the cancelled session's `ctx.Done()` then emitted the false `"QR session timed out"` — it was *superseded*, not timed out. Every rapid retry (StrictMode double-mount + clicks) thus produced a false timeout from the session it superseded, and no session survived long enough to deliver a QR. Fix (backend): `runQRSession` detects supersede (`activeSessions.Load != entry`) and exits silently; only a real `qrSessionTimeout` (still-active entry) emits the timeout (now with `reason: "timeout"`). Fix (UI): `use-whatsapp-qr-login.ts` adds an `inFlight` ref guard so re-entrant/duplicate `start()` calls (StrictMode double + rapid clicks) are suppressed — Relink/Retry buttons are already `disabled={loading}`, so no legitimate action is blocked. **Verification:** `go build` (pg+sqliteonly) ✓, `go vet ./internal/channels/whatsapp/` clean, `go test -race ./internal/channels/whatsapp/` 117/119 ✓ (2 pre-existing `TestMimeToExt` failures, unrelated), `pnpm build` (ui/web) ✓. **Pending:** live rescan confirms a single QR renders under retry. |
| 0.6-draft | 2026-07-31 | **Fifth rescan failure mode found + fixed (build/vet/test-verified).** After v0.5, a re-link attempt stuck on "Waiting for QR code…" forever — no `whatsapp.qr.code`, no `qr.done`. Root cause §3.11 (TWO defects): (A) **silent QR-channel exit** — whatsmeow's QR channel (`qrchan.go` `handleEvent`) emits `QRChannelErrUnexpectedEvent` ("err-unexpected-state") on `ConnectFailure`/`Connected`/`LoggedOut`/`TemporaryBan` during pairing, then *closes the channel*; `runQRSession`'s switch only handled `"code"`/`"success"`/`"timeout"`, so the `err-*`/`error` items hit no case, the loop read the closed channel (`!ok`), and it `return`ed silently — **no `qr.done` sent**, UI waits forever. Fix: handle all event types + channel-close → emit `qr.done` (`reason` `unexpected_state`/`pair_failed`/`closed`). (B) **nil whatsmeow logger** — `whatsmeow.NewClient(store, nil)` selected the noop logger, swallowing ALL connection/QR/pair diagnostics (the reason pairing failed was invisible). Fix: new `logging.go` `slogWhatsAppLogger` adapts `waLog.Logger` → slog; all 3 `NewClient` sites now pass `c.whatsmeowLogger()` (`Start`, `Reauth`, `StartQRFlow` recreate). **Verification:** `go build` (pg+sqliteonly) ✓, `go vet ./internal/channels/whatsapp/` clean, QR/recreate tests 6/6 ✓. **Pending:** live re-link — the UI now shows a concrete failure reason + the backend log shows the whatsmeow cause. |
| 0.7-draft | 2026-08-01 | **Actual pairing blocker identified + fixed: stale whatsmeow (WA 405 client-outdated).** The v0.6 logger finally revealed the real cause — backend log: `Client outdated (405) connect failure (client version: 2.3000.1035920091)` → `Closing channel with status {Event:err-client-outdated}`. Modes 1–5 were all necessary infrastructure (the QR flow now reconciles every whatsmeow state + surfaces every failure), but the *actual* reason no QR rendered was that WhatsApp's server rejected the embedded client version of the pinned `go.mau.fi/whatsmeow v0.0.0-20260327181659-02ec817e7cf4` (2026-03-27, ~4 months stale) with HTTP 405. Fix: `go get go.mau.fi/whatsmeow@v0.0.0-20260730092514-662ad1dc6900` (2026-07-30) + `go mod tidy` (transitive bumps: `go.mau.fi/util` 0.9.6→0.9.12, `x/crypto` 0.48→0.54, `x/net` 0.50→0.57, `x/text` 0.34→0.40, `x/sys`, `x/term`, `x/sync`, `x/mod`, `x/exp`). Plus `runQRSession` now maps `err-client-outdated` → `reason:"client_outdated"` with an actionable "update GoClaw" message (defensive — if WA bumps again). **Verification:** `go build` (pg+sqliteonly) ✓, `go vet ./internal/channels/whatsapp/` clean, `go test -race ./internal/channels/whatsapp/` 117/119 ✓ (2 pre-existing `TestMimeToExt`, unrelated). **Pending:** live re-link with the upgraded client version. |

---

## 1. Summary

When an operator reopens the WhatsApp QR wizard to **rescan an already-paired instance that is currently disconnected**, the scan fails with one of three raw whatsmeow errors depending on the exact disconnected state:

```
# Mode 1 — paired identity lingers, account not connected
whatsapp get QR channel: GetQRChannel can only be called when there's no user ID in the client's Store

# Mode 2 — account was signed out (events.LoggedOut)
whatsapp connect for QR: invalid use of deleted device

# Mode 3 — a prior attempt left an anonymous pre-pairing socket connected
whatsapp get QR channel: GetQRChannel must be called before connecting

# Mode 4 — rapid retry: each start cancels the prior session, which emits a false timeout
QR session timed out — restart to try again

# Mode 5 — pairing fails mid-handshake; UI stuck forever, no event at all
Waiting for QR code…   (no whatsapp.qr.code, no whatsapp.qr.done)

# Mode 6 — the actual blocker: WhatsApp rejects the client version (stale whatsmeow)
client version: 2.3000.1035920091 → 405 client-outdated
```

This SRS documents **why** all three errors happen and specifies the fixes. The shared root cause is a state-model mismatch: GoClaw decided QR-readiness from a single **connection** flag (`waAuthenticated`) while whatsmeow's QR path keys off three other states — **pairing identity** (`client.Store.ID == nil`, enforced by `GetQRChannel`), **device-deletion** (`!client.Store.Deleted`, enforced by `Connect()`), and **socket-not-yet-connected** (`!client.IsConnected()`, enforced by `GetQRChannel`). GoClaw reconciled none of them.

- **Mode 1** (§3.1–3.7): paired-but-disconnected (`Store.ID != nil`, `waAuthenticated=false`). The QR request reaches `GetQRChannel` against a store that still holds a paired identity, and whatsmeow refuses. Fix: detect `Store.ID != nil`, surface a structured `already_paired_disconnected` reason, and make the **Re-link** (`force_reauth=true`) action reachable from the error state (it was gated behind `status==="connected"`).
- **Mode 2** (§3.8): signed-out. whatsmeow itself deletes the device on `events.LoggedOut` (nils `Store.ID`, sets `Store.Deleted=true`, removes the DB row), so the v0.2 `Store.ID != nil` guard does not fire, `GetQRChannel` succeeds, but `Connect()` returns `store.ErrDeviceDeleted`. Fix: auto-recreate the client from a fresh device store — non-destructive (signout already destroyed the identity), no prompt needed.
- **Mode 3** (§3.9): already-connected. A prior QR attempt or the reconnect watchdog left an anonymous pre-pairing socket up; the retry calls `GetQRChannel` on the connected client, and whatsmeow returns `ErrQRAlreadyConnected` (its contract is `GetQRChannel` → `Connect`, in that order). Fix: disconnect an already-connected client before `GetQRChannel`. Safe — that line is only reachable when `!IsAuthenticated() && Store.ID == nil`.
- **Mode 4** (§3.10): session-supersede false-timeout under retry. Each `whatsapp.qr.start` cancels the prior in-flight session; the cancelled session's `ctx.Done()` emitted a false `"QR session timed out"` (it was superseded, not timed out). Rapid retries (StrictMode double-mount + clicks) thus spammed false timeouts and no session survived long enough to render a QR. Fix: silent exit on supersede (real timeout only) + a UI in-flight guard against duplicate `start()`.
- **Mode 5** (§3.11): silent QR-channel exit + nil whatsmeow logger. When pairing hits a `ConnectFailure`/`LoggedOut`/etc., whatsmeow emits `err-unexpected-state` and *closes the QR channel*; `runQRSession` only handled `code`/`success`/`timeout`, so it exited silently with no `qr.done` — UI stuck on "Waiting for QR code…" forever. And the nil whatsmeow logger swallowed the reason pairing failed. Fix: handle all QR-channel events + close, and route whatsmeow diagnostics to slog.
- **Mode 6** (§3.12) — **the actual pairing blocker**: stale whatsmeow. The v0.6 logger revealed `Client outdated (405) connect failure (client version: 2.3000.1035920091)` → WhatsApp rejected the pinned client version with HTTP 405. Modes 1–5 were all necessary infrastructure, but this dependency upgrade is what actually unblocks pairing. Fix: upgrade `go.mau.fi/whatsmeow` 2026-03-27 → 2026-07-30; plus a specific `client_outdated` reason if WA bumps again.

This is independent of the `paired_devices` table expiry work in [012](012-feat-paired-device-sliding-expiry.md) — that layer governs chat-binding authorisation rows; all six findings here are at the whatsmeow device-store / socket / QR-session / logging / dependency layer.

## 2. Scope

**In scope**:

- Root-cause analysis of the `ErrQRStoreContainsID` error during WhatsApp QR rescan.
- Reconciling pairing state (`client.Store.ID`) before requesting a QR in `internal/channels/whatsapp/`.
- Making `force_reauth` (session clear) reachable from the disconnected/logged-out state in the WhatsApp re-auth UI (`ui/web/`).
- Structured, actionable error surfaced to the UI instead of whatsmeow's raw internal message.

**Out of scope**:

- The `paired_devices` sliding-expiry feature ([012](012-feat-paired-device-sliding-expiry.md)) — different table, different layer.
- Reconnect/backoff tuning for healthy paired sessions (existing `startReconnectWatchdog` is unaffected).
- Desktop (`ui/desktop/`) WhatsApp flow — tracked separately; this SRS targets the web wizard, with a note to mirror the fix if the desktop dialog shares the hook.
- Adding a "reconnect existing session" (non-QR) path as a first-class feature — listed as an open question (§7), not required for the fix.

## 3. Root Cause Analysis

### 3.1 The two states that were conflated

WhatsApp linkage has **two independent states**. GoClaw tracks only the first; whatsmeow's QR precondition checks only the second.

| State | Source | Meaning |
|-------|--------|---------|
| **Connection** | `c.waAuthenticated` (`whatsapp.go:56`) | TCP/WebSocket link to WhatsApp servers is up. Set `true` on `events.Connected` (`whatsapp.go:340`), `false` on `events.Disconnected` (`whatsapp.go:356`) and `events.LoggedOut` (`whatsapp.go:369`). |
| **Pairing identity** | `c.client.Store.ID` (whatsmeow device store) | The device holds a logged-in account JID. Set on first successful QR scan; cleared only by explicit store deletion. |

`IsAuthenticated()` returns the **connection** flag only:

```go
// internal/channels/whatsapp/whatsapp.go:117-122
func (c *Channel) IsAuthenticated() bool {
    c.lastQRMu.RLock()
    defer c.lastQRMu.RUnlock()
    return c.waAuthenticated
}
```

### 3.2 whatsmeow's QR precondition (the gate that fires)

`GetQRChannel` has two hard preconditions (`go.mau.fi/whatsmeow qrchan.go:162-167`):

```go
func (cli *Client) GetQRChannel(ctx context.Context) (<-chan QRChannelItem, error) {
    if cli == nil {
        return nil, ErrClientIsNil
    } else if cli.IsConnected() {
        return nil, ErrQRAlreadyConnected
    } else if cli.Store.ID != nil {
        return nil, ErrQRStoreContainsID   // ← our error
    }
    ...
}
```

with the sentinel defined at `go.mau.fi/whatsmeow errors.go:32`:

```go
ErrQRStoreContainsID = errors.New("GetQRChannel can only be called when there's no user ID in the client's Store")
```

So a QR can be issued **only when the device has no paired identity** (`Store.ID == nil`). A paired device cannot be re-paired by scanning another QR — the old identity must be cleared first.

### 3.3 State matrix — where the bug lives

| `Store.ID` (pairing) | `waAuthenticated` (connection) | Real-world meaning | `GetQRChannel` result |
|---|---|---|---|
| `nil` | `false` | Never paired (fresh instance) | ✅ OK — fresh QR |
| `set` | `true` | Healthy, connected | (never reached — short-circuited at `qr_methods.go:115`) |
| **`set`** | **`false`** | **Paired but disconnected / logged-out** | **❌ `ErrQRStoreContainsID` — THE BUG** |

The bottom row is exactly the "rescan existing QR" scenario the operator hit.

### 3.4 Backend path — why the request reaches `GetQRChannel` unguarded

`QRMethods.runQRSession` (`internal/channels/whatsapp/qr_methods.go`) decides the flow with connection-state checks only:

```go
// qr_methods.go:114-126 — short-circuit only when connected AND not forcing
if wa.IsAuthenticated() && !forceReauth {
    // emit already_connected, return
}

// qr_methods.go:128-133 — clear session ONLY when force_reauth=true
if forceReauth {
    if err := wa.Reauth(); err != nil { ... }
}

// qr_methods.go:148 — request QR unconditionally otherwise
qrChan, err := wa.StartQRFlow(ctx)
```

`StartQRFlow` (`internal/channels/whatsapp/auth.go:16-53`) likewise gates only on the connection flag and then calls `GetQRChannel` with no pairing-state check:

```go
// auth.go:37-44
if c.IsAuthenticated() {
    return nil, nil // caller checks this
}
qrChan, err := c.client.GetQRChannel(ctx)
if err != nil {
    return nil, fmt.Errorf("whatsapp get QR channel: %w", err)   // ← surfaced error
}
```

In the bug-zone row (`Store.ID != nil`, `waAuthenticated == false`): the `IsAuthenticated()` guard is `false` (so no early return), `forceReauth` is `false` (so `Reauth()` is skipped), and execution falls straight into `GetQRChannel` against a store that still holds a paired identity → `ErrQRStoreContainsID`.

`Reauth()` is the **only** code path that clears the identity, and it only runs when explicitly forced:

```go
// auth.go:71-80 — deletes the device store to force a fresh QR
if c.client != nil {
    c.client.Disconnect()
}
if c.client != nil && c.client.Store.ID != nil {
    if err := c.client.Store.Delete(context.Background()); err != nil {
        slog.Warn("whatsapp: failed to delete device store", "error", err)  // warn-only
    }
}
```

### 3.5 The status hint contradicts the code path

`handleLoggedOut` marks the channel degraded with the message **"Re-scan QR to reconnect"** but does **not** clear `Store.ID`:

```go
// whatsapp.go:365-374
func (c *Channel) handleLoggedOut(evt *events.LoggedOut) {
    slog.Warn("whatsapp: logged out", "reason", evt.Reason, "channel", c.Name())
    c.lastQRMu.Lock()
    c.waAuthenticated = false
    c.lastQRMu.Unlock()
    c.MarkDegraded("WhatsApp logged out", "Re-scan QR to reconnect",
        channels.ChannelFailureKindAuth, false)
}
```

So the operator is told to "Re-scan QR", but the rescan code cannot succeed because the paired identity (`Store.ID`) is still present and nobody clears it on the non-forced path.

### 3.6 UI layer — why the operator cannot self-recover

The web re-auth dialog auto-starts the QR flow on open **without** forcing reauth (`ui/web/src/pages/channels/whatsapp/whatsapp-reauth-dialog.tsx:31-33`):

```tsx
useEffect(() => {
  if (open && status === "idle") start();   // start(false) → force_reauth: false
}, [open]);
```

`start` defaults `forceReauth` to `false` (`use-whatsapp-qr-login.ts:13,19`), and `triggerReauth` is the only caller that passes `true` (`use-whatsapp-qr-login.ts:27`). Crucially, the **"Relink Device"** button that invokes `triggerReauth` renders **only inside the `status === "connected"` branch** (`whatsapp-reauth-dialog.tsx:59-72`) — a state the user is never in when the device is paired-but-disconnected.

The reachable failure state (`status === "error"`) offers only a **Retry** button (`whatsapp-reauth-dialog.tsx:109-111`) bound to `retry` = `start` = `start(false)` — which replays the identical failing path forever. There is no UI escape to `force_reauth=true`.

### 3.7 End-to-end trigger sequence

1. Operator opens "rescan QR" on a paired-but-disconnected WhatsApp instance.
2. Dialog `useEffect` → `start()` → `whatsapp.qr.start { force_reauth: false }`.
3. `runQRSession`: `IsAuthenticated()`=false → skip already-connected; `forceReauth`=false → skip `Reauth()`; → `StartQRFlow`.
4. `StartQRFlow`: `IsAuthenticated()`=false → no early return → `GetQRChannel` → `Store.ID != nil` → `ErrQRStoreContainsID`.
5. Backend emits `whatsapp.qr.done { success: false, error: "whatsapp get QR channel: …" }`.
6. UI sets `status="error"`, shows the raw whatsmeow string. "Relink Device" (`forceReauth=true`) is not rendered. Retry loops at step 2.

### 3.8 Mode 2 — second failure: `invalid use of deleted device` after signout

After the v0.2 fix shipped, a real signout surfaced a **different** error at a **different** call site:

```
whatsapp connect for QR: invalid use of deleted device
```

The prefix `whatsapp connect for QR:` is wrapped by `StartQRFlow` around `client.Connect()` (`auth.go`). The trailing string is whatsmeow's `store.ErrDeviceDeleted` (`go.mau.fi/whatsmeow store/store.go:264`), returned by `unlockedConnect` when the device is marked deleted (`client.go:505-507`):

```go
func (cli *Client) unlockedConnect(ctx context.Context) error {
    if cli.Store.Deleted {
        return store.ErrDeviceDeleted
    }
    ...
}
```

**What signs out do to the store.** On a server-forced logout, whatsmeow **itself** fires `events.LoggedOut` and then deletes the device store (`go.mau.fi/whatsmeow connectionevents.go:43-44` and `:128-129`):

```go
go cli.dispatchEvent(&events.LoggedOut{...})
err := cli.Store.Delete(ctx)
```

`Device.Delete()` (`store/store.go:270-283`) does three things:

```go
err := device.Container.DeleteDevice(ctx, device) // 1. remove the DB row
device.ID = nil                                    // 2. nil the paired identity
device.Deleted = true                              // 3. mark deleted
device.SetAllStores(&NoopStore{ErrDeviceDeleted})  // 4. noop all stores
```

So after signout the in-memory client is `Store.ID == nil` **AND** `Store.Deleted == true`, and the DB row is gone. GoClaw's `handleLoggedOut` (`whatsapp.go:365-374`) only flips `waAuthenticated=false` + marks degraded — it does not recreate the client, so the channel keeps pointing at the dead, deleted client.

**Why the v0.2 guard did not catch it.** The v0.2 `StartQRFlow` guard checks `client.Store.ID != nil`. After a real signout `Store.ID` is **nil** (whatsmeow nilled it), so:

1. `clientNeedsRecreate` (v0.3) / lazy-init (v0.2): `client != nil`, and v0.2 only recreated when `client == nil` → skipped.
2. `IsAuthenticated()` = false → no early return.
3. `Store.ID != nil` guard → false (ID is nil) → does not return `ErrAlreadyPairedDisconnected`.
4. `GetQRChannel(ctx)` → succeeds (its preconditions are `!IsConnected()` and `Store.ID == nil`; it does **not** check `Deleted`).
5. `!client.IsConnected()` → `client.Connect()` → `Store.Deleted == true` → **`store.ErrDeviceDeleted`**.

The deleted client is unrecoverable — it cannot be Connected or re-paired in place; it must be replaced with a fresh device store.

**Fix (v0.3).** `StartQRFlow` now recreates the client when `client == nil || client.Store.Deleted` (new `clientNeedsRecreate()` helper, `auth.go`). Recreation calls `container.GetFirstDevice`, which returns a brand-new `NewDevice` when the container is empty (`sqlstore/container.go:175-184`) — and a device reloaded from any lingering row has `Deleted=false` (the `Deleted` flag is in-memory, set only by `Device.Delete`, never read back from DB). The new client has `Store.Deleted == false`, so `Connect()` proceeds and a fresh QR is delivered.

**Why this is auto-recovery, not a prompt.** Unlike Mode 1 (`Store.ID != nil`, an active linkage that Re-link would destroy), a deleted device has **already** been destroyed by signout — there is nothing to lose and no ambiguity to confirm. The operator simply gets a fresh QR; no `force_reauth` / Re-link prompt is needed. The recreation is logged (`slog.Info "whatsapp: replacing deleted device store for QR flow"`) for observability.

### 3.9 Mode 3 — third failure: `GetQRChannel must be called before connecting`

After the v0.3 fix, a **retry** surfaced a third error:

```
whatsapp get QR channel: GetQRChannel must be called before connecting
```

The trailing string is whatsmeow's `ErrQRAlreadyConnected` (`go.mau.fi/whatsmeow errors.go:31`), returned by `GetQRChannel` when the client is already connected (`qrchan.go:162-167`):

```go
} else if cli.IsConnected() {
    return nil, ErrQRAlreadyConnected
}
```

whatsmeow's QR contract is **`GetQRChannel` → `Connect`**, in that order: `GetQRChannel` registers the QR event handler, then `Connect` opens the socket and the server pushes the QR code down it. Calling `GetQRChannel` on an already-connected client has no point at which to inject the handler, so it refuses.

**Trigger.** A prior QR attempt (or the reconnect watchdog / `Start()`) leaves an **anonymous pre-pairing** socket up — `client.IsConnected() == true` but `Store.ID == nil` (never paired). On retry, `StartQRFlow` reaches `GetQRChannel` on this connected client → `ErrQRAlreadyConnected`. The v0.2/v0.3 guards do not fire because `Store.ID == nil` and `Store.Deleted == false`.

**Fix (v0.4).** `StartQRFlow` now disconnects an already-connected client before `GetQRChannel` (`auth.go`):

```go
if c.client.IsConnected() {
    c.client.Disconnect()
}
qrChan, err := c.client.GetQRChannel(ctx)
```

**Safety.** This line is only reachable when `!IsAuthenticated() && Store.ID == nil`. A live socket in that state is necessarily an anonymous pre-pairing connection, so tearing it down to let `GetQRChannel` drive a fresh pairing connection is correct and lossless. A paired+connected client never reaches here:

- paired + connected + `waAuthenticated=true` → `IsAuthenticated()` early-returns `nil, nil` (already-connected short-circuit).
- paired + connected + `waAuthenticated=false` (desync) → the `Store.ID != nil` guard returns `ErrAlreadyPairedDisconnected` first.

So no healthy paired session is ever disconnected by this path. (Not unit-tested — `IsConnected()` requires a live socket; covered by build + live multi-retry verification.)

### 3.10 Mode 4 — fourth failure: false `QR session timed out` under rapid retry (session supersede)

After the v0.1–v0.3 fixes, a live rescan produced this log tail and no rendered QR:

```
whatsapp.qr.start  req-7   (StrictMode double → req-7 & req-8 share the same ms)
whatsapp.qr.start  req-8
whatsapp.qr.start  req-9
whatsapp.qr.start  req-10
whatsapp.qr.start  req-11
whatsapp.qr.start  req-12
QR session timed out — restart to try again
```

Two compounding defects:

**A. Backend — supersede reuses the timeout message.** `handleQRStart` cancels any prior in-flight session for the same instance before starting a new one (`qr_methods.go`):

```go
if prev, loaded := m.activeSessions.Swap(params.InstanceID, entry); loaded {
    if prevEntry, ok := prev.(*cancelEntry); ok {
        prevEntry.cancel()   // cancels the prior session's qrCtx
    }
}
```

The prior session's `runQRSession` is blocked in its event `select` on `<-ctx.Done()`; that case unconditionally emitted `whatsapp.qr.done { success: false, error: "QR session timed out — restart to try again" }`. But `ctx.Done()` fires for **two** reasons: (a) a newer `whatsapp.qr.start` superseded this session (Swap replaced the entry and called `cancel`), or (b) the `qrSessionTimeout` (3 min) deadline lapsed. Only (b) is a real timeout; (a) is a normal supersede. Reusing the timeout message for (a) meant every rapid retry made the session it superseded emit a false timeout.

**B. UI — `qr.start` fired repeatedly.** The wizard auto-start effect (`whatsapp-wizard-steps.tsx`) fires `start()` on mount; React StrictMode double-mounts effects in dev (two `start()` calls in the same tick — visible as req-7/req-8 sharing one timestamp). Additional calls came from retries. Each extra call cancelled the prior session (defect A → false timeout) and started a new one, so sessions kept superseding each other and none survived long enough for whatsmeow to push a QR `code`.

**Fix (v0.5).**

- Backend (`qr_methods.go` `runQRSession`): on `<-ctx.Done()`, distinguish supersede from real timeout by checking whether this session is still the active one:

  ```go
  case <-ctx.Done():
      if v, ok := m.activeSessions.Load(instanceIDStr); ok && v != entry {
          return // superseded by a newer QR session — silent exit
      }
      // ...emit "QR session timed out" (real timeout), now with reason: "timeout"
  ```

  A superseded session's entry has been replaced in `activeSessions`, so `Load != entry` → silent exit. A real timeout leaves the entry in place → `Load == entry` → emit. The deferred `CompareAndDelete(instanceIDStr, entry)` correctly no-ops on supersede (the entry no longer matches) and cleans up on real timeout.

- UI (`use-whatsapp-qr-login.ts`): an `inFlight` ref guard suppresses re-entrant/duplicate `start()` calls while one is already in flight (the StrictMode double + rapid clicks). Relink/Retry buttons are already `disabled={loading}`, and `start()` resolves as soon as the backend ACKs (QR events arrive asynchronously), so the guard never blocks a legitimate action — it only drops the redundant duplicate calls that caused the supersede storm.

**Net effect:** a single `qr.start` session survives and delivers a QR; retries no longer spam false timeouts.

### 3.11 Mode 5 — fifth failure: stuck on "Waiting for QR code…" forever (silent QR-channel exit + nil logger)

After v0.5, a re-link attempt stuck on "Waiting for QR code…" indefinitely — no `whatsapp.qr.code`, no `whatsapp.qr.done`. Two compounding defects:

**A. Silent QR-channel exit.** whatsmeow's QR channel terminates pairing with more than just `code`/`success`/`timeout`. Its `handleEvent` (`go.mau.fi/whatsmeow qrchan.go`) maps several events to terminal items and then **closes the channel**:

```go
case *events.Disconnected:                 outputType = QRChannelTimeout
case *events.Connected, *events.ConnectFailure,
     *events.LoggedOut, *events.TemporaryBan: outputType = QRChannelErrUnexpectedEvent
case *events.PairError:                    outputType = QRChannelItem{Event: "error", Error: evt.Error}
...
qrc.output <- outputType
close(qrc.output)
```

So a `ConnectFailure` (server rejects the new device), a premature `Connected`, a `LoggedOut`, or a `TemporaryBan` during pairing → whatsmeow pushes one `err-unexpected-state` item and closes the channel. GoClaw's `runQRSession` switch only handled `"code"` / `"success"` / `"timeout"`:

```go
switch evt.Event {
case "code":   ...
case "success": ...
case "timeout": ...
}   // ← no default: err-unexpected-state / err-client-outdated / error fall through
```

The `err-*` / `error` item matched no case → loop continued → next read returned the closed channel (`ok == false`) → `return` **with no `qr.done` sent**. The UI therefore stayed in `status="waiting"` forever (no code, no done, no timeout — the goroutine simply exited).

**B. Nil whatsmeow logger.** Every `whatsmeow.NewClient(deviceStore, nil)` selected the built-in noop logger, so ALL of whatsmeow's internal diagnostics were silently dropped — connection failures, QR-generation errors, pair errors, stream errors. The *reason* pairing failed was invisible, which is why Modes 2–5 each took a live cycle to diagnose.

**Fix (v0.6).**

- `runQRSession` now handles every QR-channel outcome:
  - `default` branch maps `err-unexpected-state` → `reason:"unexpected_state"`, `err-client-outdated`/`err-scanned-without-multidevice` → likewise, and `"error"` (PairError) → `reason:"pair_failed"` with `evt.Error.Error()` — each emits a `qr.done` failure and returns.
  - channel-close (`!ok`) without a recognised terminal event → `reason:"closed"` failure (belt-and-suspenders).
  - `timeout` items now carry `reason:"timeout"`.
- New `logging.go` `slogWhatsAppLogger` implements `waLog.Logger` (`util/log/log.go:17`) by routing `Debugf/Infof/Warnf/Errorf` → slog, scoped `component=whatsmeow` + `channel=<name>`; `Sub(module)` tags `wa_sub`. All three `NewClient` sites (`whatsapp.go` `Start`, `auth.go` `Reauth`, `auth.go` `StartQRFlow` recreate) now pass `c.whatsmeowLogger()` instead of `nil`. The gateway log level still gates verbosity.

**Net effect:** a failed pairing now surfaces a concrete `qr.done` reason to the UI (no more forever-hang) AND the backend log shows the whatsmeow cause (ConnectFailure reason, etc.), turning the next live cycle from guesswork into a log line.

### 3.12 Mode 6 — the actual pairing blocker: stale whatsmeow (WhatsApp 405 client-outdated)

The v0.6 logger fix was the turning point. With diagnostics visible, the very next re-link attempt printed the real cause:

```
ERROR  Client outdated (405) connect failure (client version: 2.3000.1035920091)  component=whatsmeow channel=whatsapp-reski
DEBUG  <failure location="lla" reason="405"/>
DEBUG  Closing channel with status {Event:err-client-outdated Error:<nil> Code: Timeout:0s}
WARN   whatsapp QR: channel ended  event=err-client-outdated
```

WhatsApp's server returned `<failure … reason="405"/>` — **client-outdated** — rejecting the embedded WA client version `2.3000.1035920091` shipped by the pinned dependency `go.mau.fi/whatsmeow v0.0.0-20260327181659-02ec817e7cf4` (dated 2026-03-27, ~4 months stale). WhatsApp periodically raises the minimum accepted client version and 405s older ones; whatsmeow releases new pseudo-versions precisely to bump the embedded version. No amount of GoClaw logic could have fixed this — **the dependency had to be upgraded**.

This reframes Modes 1–5: they were all **real, necessary** fixes (the QR flow genuinely mishandled pairing/identity/supersede/event states, and the nil logger hid everything), but none of them was the *actual* reason a QR never rendered on the user's tenant. The 405 was. The earlier modes simply made the 405 observable instead of an inscrutable hang.

**Fix (v0.7).**

- Upgrade: `go get go.mau.fi/whatsmeow@v0.0.0-20260730092514-662ad1dc6900` (2026-07-30) + `go mod tidy`. Transitive bumps: `go.mau.fi/util` 0.9.6→0.9.12-0.20260717, `golang.org/x/crypto` 0.48→0.54, `x/net` 0.50→0.57, `x/text` 0.34→0.40, plus `x/sys`/`x/term`/`x/sync`/`x/mod`/`x/exp`. No GoClaw API breakage — `go build` (pg+sqliteonly) clean, `go vet` clean.
- Defensive UX: `runQRSession` now maps `err-client-outdated` (`QRChannelClientOutdated`) to `reason:"client_outdated"` with an actionable message ("WhatsApp rejected the gateway client version (outdated). Update GoClaw to the latest release and restart."), so a future WA version bump produces a clear operator message instead of a generic error.

**Operational note:** client-outdated is a recurring external failure mode. Pairing 405s should always prompt checking for a newer whatsmeow/GoClaw release. This is now documented + detectable via the `component=whatsmeow` logs.

## 4. Functional Requirements

### FR-00: Reconcile device state (pairing identity AND deletion) before requesting a QR

The QR flow must not call `GetQRChannel` / `Connect()` against an unusable device store. Two unusable states must be reconciled before a QR: (a) a lingering paired identity (`client.Store.ID != nil`) and (b) a deleted device (`client.Store.Deleted == true`, set by whatsmeow on signout).

Acceptance criteria:

- [x] `StartQRFlow` (or its caller) checks `c.client.Store.ID` before `GetQRChannel`; it never forwards the raw `ErrQRStoreContainsID` to the client. *(auth.go guard + `TestStartQRFlow_PairedButDisconnected`)*
- [x] When `Store.ID != nil` and a fresh QR is requested, the paired identity is cleared (via the existing `Reauth()` store-delete path) before `GetQRChannel` is called. *(forced path: `qr_methods.go` calls `Reauth()` → `StartQRFlow`; `Reauth` now hard-fails if `Store.Delete` errors)*
- [x] `IsAuthenticated()` semantics are documented as **connection** state; a separate check is used for **pairing** state. The two are not conflated in any QR gating decision. *(auth.go comment + `TestStartQRFlow_NotAuthenticatedFlagDoesNotSuppressPairedGuard`)*
- [x] `StartQRFlow` recreates the client when `client == nil || client.Store.Deleted`, so a signed-out device (`store.ErrDeviceDeleted` from `Connect()`) is replaced with a fresh device store before the QR flow. Auto-recovery — no operator prompt, since signout already destroyed the identity. *(v0.3: `clientNeedsRecreate()` + `TestClientNeedsRecreate`)*
- [x] `StartQRFlow` disconnects an already-connected client before `GetQRChannel`, so a retry against an anonymous pre-pairing socket (`ErrQRAlreadyConnected`) re-runs the QR handshake instead of failing. A paired+connected client is never disconnected here (caught earlier by `IsAuthenticated()` or the `Store.ID != nil` guard). *(v0.4)*
- [ ] After a real signout (`events.LoggedOut`), reopening the QR wizard delivers a scannable QR without manual restart or server-side intervention. *(live verification pending)*
- [ ] Retrying the QR scan (second `whatsapp.qr.start` after a failed/timed-out first attempt) delivers a fresh QR rather than `ErrQRAlreadyConnected`. *(live verification pending)*

### FR-01: Make session-clear reachable from the disconnected/logged-out state

`force_reauth` (the only path that clears `Store.ID`) must be reachable when the instance is paired-but-disconnected — the exact state that today has no UI escape.

Acceptance criteria:

- [x] The re-auth dialog offers a "Relink / Re-pair device" action (sends `force_reauth: true`) from the disconnected/logged-out/error states, not only from `status === "connected"`. *(whatsapp-reauth-dialog.tsx renders Re-link when `reason === "already_paired_disconnected"`)*
- [x] A plain rescan on a paired-but-disconnected instance either auto-clears the stale identity or prompts the operator to confirm re-pairing (explicit choice — see §7). *(prompt path chosen: backend surfaces `already_paired_disconnected`, UI shows Re-link)*
- [ ] After `force_reauth: true`, a fresh QR is delivered and scans successfully on the real device. *(live verification pending)*

### FR-02: Structured, actionable error instead of raw whatsmeow text

The `whatsapp.qr.done` failure payload must carry a machine-readable `reason`, and the UI must render a human message + the correct next action (re-pair), never the internal whatsmeow string.

Acceptance criteria:

- [x] `whatsapp.qr.done` failure payloads include a `reason` field (e.g. `already_paired_disconnected`) in addition to `error`. *(qr_methods.go)*
- [x] The UI maps `reason` to a translated message and the re-pair action; the raw whatsmeow error is logged server-side only, not shown to the operator. *(use-whatsapp-qr-login.ts + whatsapp-reauth-dialog.tsx; raw error kept in server `slog.Warn`)*
- [x] New user-facing strings are added to `internal/i18n/keys.go` + `catalog_en.go` / `catalog_vi.go` / `catalog_zh.go` and to all three `ui/web/src/i18n/locales/{en,vi,zh}/channels.json` namespaces. *(3 web locale files updated; backend `internal/i18n` intentionally not touched — `reason` is a code translated client-side, see §0.2 revision note)*

### FR-03: Consistent "logged out" recovery

`handleLoggedOut` and the degraded status "Re-scan QR to reconnect" must be backed by a code path that can actually produce a QR (i.e. clears `Store.ID`), so the operator hint is truthful.

Acceptance criteria:

- [ ] After a `LoggedOut` event, the rescan flow reaches a scannable QR without manual server restart. *(code path fixed: guard + Reauth clear; live verification pending)*
- [x] The degraded status message accurately reflects the recoverable action. *"Re-scan QR to reconnect" is now backed by a working path that prompts Re-link when the identity lingers.*

### FR-04: QR session supersede must not emit a false timeout

A newer `whatsapp.qr.start` cancels the prior in-flight session for the same instance. The superseded session must exit silently — only a real `qrSessionTimeout` (the session is still active) may emit the timeout.

Acceptance criteria:

- [x] `runQRSession` distinguishes supersede (`activeSessions.Load != entry`) from real timeout (`Load == entry`); supersede exits silently, real timeout emits `whatsapp.qr.done { reason: "timeout" }`. *(v0.5 backend)*
- [x] The UI does not fire duplicate/re-entrant `qr.start` calls (StrictMode double-mount + rapid clicks); an in-flight guard suppresses them. *(v0.5 UI `inFlight` ref)*
- [ ] Retrying the QR scan renders a single QR rather than a storm of false `QR session timed out` events. *(live verification pending)*

### FR-05: Every QR-channel outcome must surface a `qr.done`; whatsmeow diagnostics must be logged

A pairing that fails mid-handshake (ConnectFailure, LoggedOut, TemporaryBan, PairError, or the channel closing) must emit a `qr.done` failure — never exit silently leaving the UI on "Waiting for QR code…" forever. And whatsmeow's internal diagnostics (the *reason* it failed) must reach the gateway log, not be swallowed by a noop logger.

Acceptance criteria:

- [x] `runQRSession` handles every whatsmeow QR-channel item — `err-unexpected-state`, `err-client-outdated`, `err-scanned-without-multidevice`, `error` (PairError), and channel-close (`!ok`) — each emitting a `qr.done` failure with a `reason` and returning; no silent exit. *(v0.6 backend)*
- [x] All `whatsmeow.NewClient` sites pass a slog-backed `waLog.Logger` (`logging.go` `slogWhatsAppLogger`), not `nil`. *(v0.6 — `Start`, `Reauth`, `StartQRFlow` recreate)*
- [ ] A failed re-link surfaces a concrete failure reason to the UI, and the backend log shows the whatsmeow cause. *(live verification pending)*

## 5. System Impact

- [Service layer change] `internal/channels/whatsapp/auth.go` (`StartQRFlow`) and/or `qr_methods.go` (`runQRSession`) — reconcile `client.Store.ID` before `GetQRChannel`.
- [API handler / event change] `whatsapp.qr.done` failure payload gains a `reason` field (`pkg/protocol` event consumers + `qr_methods.go`).
- [i18n change] New keys in `internal/i18n/keys.go` + 3 catalogs; 3 web locale files.
- [Web UI change] `ui/web/src/pages/channels/whatsapp/whatsapp-reauth-dialog.tsx` + `use-whatsapp-qr-login.ts` — expose re-pair from non-connected states; render structured reason.
- [Structured logging change] Log `Store.ID != nil` rescan attempts and the chosen reconciliation (auto-clear vs. prompted) under the existing `slog` whatsapp prefix; raw whatsmeow error logged, not surfaced.
- [Structured logging change] New `internal/channels/whatsapp/logging.go` — `slogWhatsAppLogger` routes whatsmeow's `waLog.Logger` diagnostics into slog; all `NewClient` sites pass it (was `nil`/noop).
- No DB migration. No schema change. No new RPC method (reuse `whatsapp.qr.start` with `force_reauth`).
- [Dependency change] `go.mau.fi/whatsmeow` upgraded `v0.0.0-20260327181659-02ec817e7cf4` → `v0.0.0-20260730092514-662ad1dc6900` (§3.12 — the actual pairing blocker, WA 405 client-outdated) + transitive `go.mod` bumps from `go mod tidy`.
- [Canonical error code registration] New `reason` value(s) in the `whatsapp.qr.*` event vocabulary (§8).

## 6. Test Plan

- Unit tests for `StartQRFlow` reconciliation:
  - `Store.ID == nil`, not connected → QR issued (existing behaviour preserved).
  - `Store.ID != nil`, connected → already-connected short-circuit.
  - `Store.ID != nil`, not connected → identity cleared, then QR issued (the fix).
- Unit test that the raw `ErrQRStoreContainsID` string is never present in any `whatsapp.qr.done` payload sent to a client.
- Unit test for `whatsapp.qr.done` `reason` field on each failure branch.
- Web component test: re-auth dialog renders the re-pair action from the disconnected/error states and sends `force_reauth: true`.
- Manual integration test: pair an instance, force a `LoggedOut`/disconnect, reopen the wizard, confirm a scannable QR is delivered and completes on the real device.

## 7. Risks and Open Questions

| Risk or question | Draft decision |
|------------------|----------------|
| Should a plain rescan (no explicit "re-pair" click) on a paired-but-disconnected instance auto-clear the identity, or always prompt? | Prompt by default — clearing `Store.ID` severs the existing linkage, which is destructive and should be explicit. Auto-clear only behind the existing `force_reauth=true` confirmation. (Needs operator confirmation.) |
| Is "reconnect the existing paired session" (call `client.Connect()`, no QR) a desired alternative path? | Out of scope for this fix. whatsmeow's auto-reconnect + `startReconnectWatchdog` already cover transient reconnects. Only pursue if operators report wanting reconnect-without-re-pair as a distinct action. |
| Does `Reauth()` reliably clear `Store.ID` when `Store.Delete` errors? | Today `Store.Delete` failure is warn-only (`auth.go:78`). The fix must treat a failed delete as a hard failure (return the error) so `GetQRChannel` is never called against a still-populated store. |
| Desktop parity — does `ui/desktop/frontend/` share the WhatsApp QR hook? | Investigate during implementation; mirror the UI fix if the same `force_reauth` gating exists there. |
| Multi-device/companion: after re-pair, does the old companion node row need cleanup? | Separate concern ([012](012-feat-paired-device-sliding-expiry.md) pairing-table layer); confirm no stale row blocks the new linkage. |

## 8. Implementation Plan

1. **Backend reconciliation**: in `StartQRFlow` (or `runQRSession` before calling it), check `c.client.Store.ID`. If non-nil and a fresh QR is requested, run the `Reauth()` store-clear path first; make `Store.Delete` failure a hard error (not warn-only).
2. **Structured failure reason**: extend the `whatsapp.qr.done` failure payload with `reason`; emit `already_paired_disconnected` for this case. Keep the raw whatsmeow error in server logs only.
3. **i18n**: add the new keys to `internal/i18n/keys.go` + `catalog_en.go` / `catalog_vi.go` / `catalog_zh.go` and the 3 web locale files **before** wiring the UI.
4. **Web UI**: render the re-pair action (`force_reauth: true`) from the disconnected/logged-out/error states in `whatsapp-reauth-dialog.tsx`; map `reason` to a translated message in `use-whatsapp-qr-login.ts`.
5. **Truthful status**: ensure `handleLoggedOut`'s "Re-scan QR to reconnect" is backed by the reconciled path from step 1.
6. **Tests + manual verification**: unit tests from §6, then a live rescan on a paired-but-disconnected instance.

## 9. Proposed Error Codes

| Code / `reason` | Meaning |
|------|---------|
| `whatsapp.qr.already_paired_disconnected` | Device holds a paired identity (`Store.ID != nil`) but is not connected; a QR cannot be issued until the identity is cleared (re-pair) or the session is reconnected. Surfaced in `whatsapp.qr.done` `reason`. |
| `whatsapp.qr.session_clear_failed` | `Reauth()` failed to delete the device store; the QR flow was aborted rather than calling `GetQRChannel` against a populated store. Surfaced in `whatsapp.qr.done` `reason`; raw error logged server-side. |
| `whatsapp.qr.timeout` (reason `"timeout"`) | The QR session was still active but the `qrSessionTimeout` (3 min) deadline lapsed with no scan, OR whatsmeow emitted a `QRChannelTimeout` (Disconnected). Surfaced in `whatsapp.qr.done` `reason`. A superseded session (replaced by a newer `whatsapp.qr.start`) exits silently and emits nothing. |
| `whatsapp.qr.unexpected_state` (reason `"unexpected_state"`) | whatsmeow emitted `QRChannelErrUnexpectedEvent` during pairing — a `ConnectFailure`, premature `Connected`, `LoggedOut`, or `TemporaryBan` (server rejected the new device / connection dropped). Surfaced in `whatsapp.qr.done` `reason`; the specific cause is in the backend log via the wired whatsmeow logger. |
| `whatsapp.qr.pair_failed` (reason `"pair_failed"`) | whatsmeow emitted a `PairError` (QRChannelEventError) — pairing was attempted but failed. `error` carries `evt.Error`. Surfaced in `whatsapp.qr.done`. |
| `whatsapp.qr.client_outdated` (reason `"client_outdated"`) | WhatsApp rejected the embedded client version (HTTP 405, `<failure reason="405"/>`) — the whatsmeow dependency is stale. `error` tells the operator to update GoClaw. The real fix is upgrading `go.mau.fi/whatsmeow`, not retrying. Surfaced in `whatsapp.qr.done` `reason`; the rejected client version is in the backend log. |
| `whatsapp.qr.closed` (reason `"closed"`) | The QR channel closed without a recognised terminal event (defensive fallback). Surfaced in `whatsapp.qr.done` `reason`. |
| _(deleted device — no code)_ | Mode 2 (`store.ErrDeviceDeleted` after signout) is **auto-recovered** in `StartQRFlow` (`clientNeedsRecreate`): the client is replaced with a fresh device store and a normal QR is delivered, so no `reason`/error is surfaced to the UI. The recovery is logged server-side (`slog.Info "whatsapp: replacing deleted device store for QR flow"`). |
| _(already connected — no code)_ | Mode 3 (`ErrQRAlreadyConnected`) is **auto-recovered** in `StartQRFlow`: an already-connected anonymous pre-pairing client is disconnected before `GetQRChannel`, so a retry re-runs the QR handshake and a normal QR is delivered. No `reason`/error surfaced to the UI. |
| `NOT_FOUND` (`pkg/protocol/errors.go:13`) | Existing — instance not found / not a WhatsApp instance (`qr_methods.go:59`). Unchanged. |
| `INVALID_REQUEST` (`pkg/protocol/errors.go:5`) | Existing — malformed `instance_id` (`qr_methods.go:53`). Unchanged. |
