# Software Requirements Specification: WhatsApp QR Rescan Fails on Paired-but-Disconnected Session (`GetQRChannel … no user ID in the client's Store`)

**Project**: GoClaw Gateway
**Release**: 2026.3.0
**Version**: 0.2-draft
**Date**: 2026-07-31
**Status**: Implemented (code-complete + build/vet/test-verified). Live on-device rescan verification pending (FR-01/FR-03 manual boxes).
**Difficulty**: Medium
**Estimate**: 0.5 days

---

## Revision History

| Version | Date | Changes |
|---------|------|---------|
| 0.1-draft | 2026-07-31 | Initial root-cause analysis. Confirmed defect spans two layers: (1) backend QR flow keys off connection state (`waAuthenticated`) instead of pairing state (`client.Store.ID`); (2) UI auto-starts rescan with `force_reauth=false` and gates the only `force_reauth=true` control behind the `connected` status the user is not in. whatsmeow precondition cited from module source. |
| 0.2-draft | 2026-07-31 | **Implemented (code-complete + build/vet/test-verified).** Backend: `auth.go` adds `ErrAlreadyPairedDisconnected` sentinel + `StartQRFlow` guards `client.Store.ID != nil` before `GetQRChannel` (returns sentinel, never leaks whatsmeow raw error); `Reauth()` makes `Store.Delete` failure a hard error (was warn-only). `qr_methods.go` adds reason consts + maps failures in `whatsapp.qr.done` (`already_paired_disconnected` / `session_clear_failed` / `qr_start_failed`); Reauth failure now returns early with `session_clear_failed`. UI: `use-whatsapp-qr-login.ts` captures+exposes `reason`; `whatsapp-reauth-dialog.tsx` renders the paired-disconnected message + **Re-link Device** button (`force_reauth=true`) from the error state — the previously-unreachable action. i18n: 3 keys × en/vi/zh in `channels.json`. New `TestStartQRFlow_PairedButDisconnected` + `TestStartQRFlow_NotAuthenticatedFlagDoesNotSuppressPairedGuard`. **Verification:** `go build ./...` ✓, `go build -tags sqliteonly ./...` ✓, `go vet ./internal/channels/...` clean, `go test -race ./internal/channels/whatsapp/ -run TestStartQRFlow` 2/2 ✓, `pnpm build` (ui/web) ✓. `go fix ./...` skipped (same unrelated-modernization-churn convention as 007/009/012). **Deviations:** (1) backend `internal/i18n` keys NOT added — the failure is carried as a machine-readable `reason` code translated client-side, so only web locale files were touched (cleaner than the §5 overspec); (2) desktop N/A — no WhatsApp QR hook in `ui/desktop/frontend/`. **Pre-existing unrelated failure:** `TestMimeToExt` (media_utils, "text/plain"→".txt" vs ".bin") — file untouched by this fix. **Pending:** live rescan on a paired-but-disconnected device (FR-01/FR-03). |

---

## 1. Summary

When an operator reopens the WhatsApp QR wizard to **rescan an already-paired instance that is currently disconnected** (logged out from the phone, evicted companion node, network drop, or gateway restart mid-reconnect), the scan fails with the raw whatsmeow error:

```
whatsapp get QR channel: GetQRChannel can only be called when there's no user ID in the client's Store
```

This SRS documents **why** the error happens and specifies the fix. The defect is a state-model mismatch: GoClaw decides QR-readiness from a **connection** flag (`waAuthenticated`) while whatsmeow's `GetQRChannel` precondition is about **pairing identity** (`client.Store.ID == nil`). In the paired-but-disconnected state the two disagree, the QR flow is requested against a store that still holds a paired identity, and whatsmeow refuses. The UI then offers no path to the `force_reauth=true` action that would clear the identity and unblock the scan.

This is independent of the `paired_devices` table expiry work in [012](012-feat-paired-device-sliding-expiry.md) — that layer governs chat-binding authorisation rows; this bug is at the whatsmeow device-store identity layer (`client.Store.ID`).

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

## 4. Functional Requirements

### FR-00: Reconcile pairing identity before requesting a QR

The QR flow must not call `GetQRChannel` while `client.Store.ID != nil`. Before requesting a QR, the system must detect a lingering paired identity and either clear it (when the operator's intent is to re-pair) or surface a structured reason.

Acceptance criteria:

- [x] `StartQRFlow` (or its caller) checks `c.client.Store.ID` before `GetQRChannel`; it never forwards the raw `ErrQRStoreContainsID` to the client. *(auth.go guard + `TestStartQRFlow_PairedButDisconnected`)*
- [x] When `Store.ID != nil` and a fresh QR is requested, the paired identity is cleared (via the existing `Reauth()` store-delete path) before `GetQRChannel` is called. *(forced path: `qr_methods.go` calls `Reauth()` → `StartQRFlow`; `Reauth` now hard-fails if `Store.Delete` errors)*
- [x] `IsAuthenticated()` semantics are documented as **connection** state; a separate check is used for **pairing** state. The two are not conflated in any QR gating decision. *(auth.go comment + `TestStartQRFlow_NotAuthenticatedFlagDoesNotSuppressPairedGuard`)*

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

## 5. System Impact

- [Service layer change] `internal/channels/whatsapp/auth.go` (`StartQRFlow`) and/or `qr_methods.go` (`runQRSession`) — reconcile `client.Store.ID` before `GetQRChannel`.
- [API handler / event change] `whatsapp.qr.done` failure payload gains a `reason` field (`pkg/protocol` event consumers + `qr_methods.go`).
- [i18n change] New keys in `internal/i18n/keys.go` + 3 catalogs; 3 web locale files.
- [Web UI change] `ui/web/src/pages/channels/whatsapp/whatsapp-reauth-dialog.tsx` + `use-whatsapp-qr-login.ts` — expose re-pair from non-connected states; render structured reason.
- [Structured logging change] Log `Store.ID != nil` rescan attempts and the chosen reconciliation (auto-clear vs. prompted) under the existing `slog` whatsapp prefix; raw whatsmeow error logged, not surfaced.
- No DB migration. No schema change. No new RPC method (reuse `whatsapp.qr.start` with `force_reauth`).
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
| `NOT_FOUND` (`pkg/protocol/errors.go:13`) | Existing — instance not found / not a WhatsApp instance (`qr_methods.go:59`). Unchanged. |
| `INVALID_REQUEST` (`pkg/protocol/errors.go:5`) | Existing — malformed `instance_id` (`qr_methods.go:53`). Unchanged. |
