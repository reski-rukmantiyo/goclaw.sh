# Software Requirements Specification: Per-Contact Agent Routing for WhatsApp Direct Messages

**Project**: GoClaw Gateway
**Release**: 2026.3.0
**Version**: 0.5-draft
**Date**: 2026-09-06
**Status**: Implemented (code-complete + build/vet/test-verified). Live on-device DM round-trip on the master tenant pending (§5 manual items).
**Difficulty**: Low–Medium
**Estimate**: 1–2 days

---

## Revision History

| Version | Date | Changes |
|---------|------|---------|
| 0.1-draft | 2026-09-04 | Initial draft. Current-state map verified against code: DM routing always uses the channel default agent (`resolveAgentID` group-only override, `internal/channels/whatsapp/whatsapp.go:674-684`); gateway-level `cfg.Bindings` routing (`resolveAgentRoute`, `cmd/gateway_consumer_helpers.go:20`) is never consulted for WhatsApp because the channel always sets `msg.AgentID` (`cmd/gateway_consumer_normal.go:39-42`). Design mirrors the existing per-group override mechanism (`WhatsAppGroupConfig.AgentID` keyed by group JID in `channel_instances.config`). |
| 0.2-draft | 2026-09-06 | **Implemented (code-complete + build/vet/test-verified).** Config: `WhatsAppContactConfig` + `Contacts` on `WhatsAppConfig` (`config_channels.go`), round-trip + nil-map-omitted tests green. Resolution: `resolveAgentID` gained the `direct` branch; `resolveGroupAgentUUID` refactored into shared `resolveAgentKeyUUID` with new sibling `resolveContactAgentUUID` (same cache pattern as 003 RC1); `RefreshGroupAgentCache` walks `Contacts` too. Inbound: contact-disabled early return + DM routing **Debug** log (per FR-02.4); listen-only DM attribution via `resolveContactAgentUUID(senderID)`. UI: `whatsapp-contact-overrides.tsx` (picker via `listContacts("","whatsapp","direct","user")`, agent Select, enabled toggle, manual JID entry) wired into the WhatsApp branch of `channel-groups-tab.tsx` with the shared Save; `contact-jid.ts` pure helper + vitest. i18n: `whatsappContactOverrides` block (13 keys — superset of the 9 proposed: added `hint`/`nameLabel`/`enabledHint`/`knownContacts` needed by the component) × en/vi/zh. **Deviations:** (1) `resolveAgentID` signature extended to `(chatID, senderID, peerKind)` — the DM lookup key is the normalized sender JID (FR-03), which differs from raw `chatID` under LID addressing; 4 callers updated (`inbound.go`, `commands.go` ×3). (2) No `resolveContactAgentUUID` DB lookup per message — shared cache proven by `TestResolveContactAgentUUID_CachesAcrossCalls`. **Verification:** `go build ./...` ✓, `go build -tags sqliteonly ./...` ✓, `go vet ./...` ✓, `go test -race ./internal/channels/whatsapp/` 144/146 (2 pre-existing `TestMimeToExt`, documented since `003`), `go test -race ./internal/config/ ./internal/channels/` 211 ✓, `pnpm build` (ui/web) ✓, `contact-jid.test.ts` 6/6 ✓. `go fix` skipped (unrelated-churn convention, 007/009/012/013/014). **Deferred (env-gated):** live master-tenant DM round-trip (override contact → Raka, unlisted → default, listen-only attribution, runtime edit), UI render/click checks, session-switch check. |
| 0.5-draft | 2026-09-06 | **Live round-3 root cause found + fixed: the DB-instance factory dropped `contacts` on load.** After the v0.4 device-strip rebuild the DM STILL routed to the default agent, while `channel_contacts` proved resolution end-to-end (new row keyed bare `6281511488487@s.whatsapp.net` = config key). Root cause: `internal/channels/whatsapp/factory.go` decodes the instance config JSONB into a hand-mapped `whatsappInstanceConfig` struct and copies fields **one by one** into `config.WhatsAppConfig`; `Groups` had an explicit wrapper parse but **`Contacts` was never mapped** → `c.config.Contacts` always nil for DB instances → the FR-02 direct branch's `c.config.Contacts != nil` guard skipped → channel default. The same two-config-sources drift class as `003` RC1 (FR-01's "reload restores it" assumption was true for the opaque JSONB persistence but wrong for the load path — that AC's evidence is corrected here). Fix: the wrapper parse now also maps `contacts` (+ a `whatsapp contact overrides loaded` log mirroring the groups log). Confirmed single construct site (`config.WhatsAppConfig{` appears only in `factory.go:74`; config.json deployments parse the full struct directly). Checklist green (builds PG+sqliteonly, vet, whatsapp 149/151 — 2 pre-existing `TestMimeToExt`); binary rebuilt 05:19 WIB. Operator restart + retest pending (third round). |
| 0.4-draft | 2026-09-06 | **Live round-2 root cause found + fixed: device suffix on the resolved PN.** After the v0.3 rebuild+restart the DM still routed to the default agent, while `channel_contacts` proved FR-08 was resolving — the new contact row was keyed **`6281511488487:78@s.whatsapp.net`** (suffixed) vs the config key `6281511488487@s.whatsapp.net` (bare). Root cause: whatsmeow's `getLIDMapping` constructs the resolved PN with `Device: source.Device` (`store/sqlstore/lidmap.go:100`) — the LID's `:78` device suffix leaks onto the phone JID. Fix: `resolveContactLookupKey` zeroes `pn.Device` before `String()` (bare phone form; `JID.String()` omits device 0). Test stub updated to mirror the real suffix-carrying behavior (`Device: lid.Device`), so `TestResolveContactLookupKey_LIDResolvesToPhone` now fails without the strip. Checklist re-run green (149/151 whatsapp, 2 pre-existing `TestMimeToExt`); gateway binary rebuilt 05:15 WIB — operator restart + retest pending. |
| 0.3-draft | 2026-09-06 | **Live verification found + fixed the §6 LID-only risk (now FR-08).** Operator live test: contact `6281511488487@s.whatsapp.net` → agent `kala`, DM still answered by channel default. DB diagnosis: config persisted ✓ (`config->'contacts'`), agent_key `kala` exists ✓, gateway = fresh binary with FR-02 code ✓ (built 04:43 WIB, test 04:48), but `channel_contacts` shows the sender's actual identity is **`141089709252847:83@lid`** — a LID-only DM (no `SenderAlt`), so the phone-JID lookup key never matched. Exactly the §6 row-3 accepted risk, now materialized by WhatsApp's LID-first rollout. **Fix (operator-approved Option 2 — routing-only):** new `resolveContactLookupKey(senderID)` on `Channel` resolves `@lid` senders to their phone JID via whatsmeow's native LID map (`client.Store.LIDs.GetPNForLID` — in-process, cache-backed; live `whatsmeow_lid_map` holds 476 mappings incl. `141089709252847 → 6281511488487`, auto-maintained by whatsmeow from group/message traffic). Applied in 3 routing-path sites: `resolveAgentID` direct branch, `resolveContactAgentUUID`, and the DM `EnsureContact` key (so the UI contact picker stores phone-JID keys that match the override map). Graceful fallback: nil client/store, unmapped LID, or parse error → raw senderID (pre-FR-08 behavior). **Deliberately NOT touched:** DM policy/pairing keys — the operator's `paired_devices` row is LID-keyed (`141089709252847:83@lid`), so normalizing identity for pairing would force re-pairing (Option 1 rejected; full identity unification deferred as a separate task). DM routing Debug log now also carries `sender` + `lookup_key` for triage. Tests: `contact_lid_lookup_test.go` — 5 cases (LID→phone, unmapped fallback, phone passthrough, nil client, end-to-end `resolveAgentID` + `resolveContactAgentUUID` with LID-only sender against a phone-keyed override) with a stub `wastore.LIDStore`; all green `-race`. §6 LID row decision updated: risk materialized + resolved by FR-08 (Option 2); identity unification (pairing/allowlist LID keys) remains a separate follow-up. |

---

## 1. Summary

This SRS defines **per-contact agent routing for WhatsApp Direct Messages**: each WhatsApp contact (phone JID) that chats 1-on-1 with the gateway can be routed to a different agent, independent of the channel's default agent. Example: contact `6281511488487@s.whatsapp.net` routes to **Jarvis** (channel default), while contact `6281581484242@s.whatsapp.net` routes to **Raka**.

GoClaw already implements this pattern for **WhatsApp groups** — a `groups` map in the channel instance config (`channel_instances.config` JSONB) keyed by group JID, with an `agent_id` override per group (`config.WhatsAppGroupConfig`, `internal/config/config_channels.go:193-200`), resolved on the inbound hot path (`resolveAgentID`, `whatsapp.go:674`) and on the listen-only raw-storage path (`resolveGroupAgentUUID`, `whatsapp.go:851`). This SRS adds the symmetric **`contacts` map** for DMs, with no schema migration (config is opaque JSONB), no new endpoints (reuses `channels.instances.update`), and no new error codes.

This SRS owns: the config data shape, the DM agent-resolution behavior (response path + listen-only raw-storage path), the web UI contact-override editor, and the i18n strings. It composes with `003-bugfix-whatsapp-group-graph-agent-mismatch.md` (RC1's live-config resolution + agent_key→UUID cache pattern, reused as-is), `012-feat-paired-device-sliding-expiry.md` (DM pairing gate — unchanged, runs before routing), and `009-bugfix-agent-episodic-recall-not-surfaced.md` (per-agent memory/session behavior — noted, not changed).

## 2. Scope

**In scope**:

- New `WhatsAppContactConfig` struct + `contacts` map on `config.WhatsAppConfig`, keyed by contact phone JID (`<number>@s.whatsapp.net`), persisted inside the existing `channel_instances.config` JSONB (no migration).
- DM agent resolution: when `peerKind == "direct"` and the contact has an `agent_id` override, the inbound message routes to that agent instead of the channel default — on both the **response path** (`resolveAgentID`) and the **listen-only raw-storage path** (agent UUID attribution for `listen_raw_messages`).
- Key normalization: DM override lookup uses the already-normalized **phone JID** sender identity (LID→phone normalization exists at `internal/channels/whatsapp/inbound.go:36-43`), so LID-addressed chats resolve the same override.
- Web UI: a contact-override editor in the WhatsApp channel detail page (mirrors `WhatsAppGroupOverrides`): contact picker fed by discovered DM contacts (`GET /v1/contacts?channel_type=whatsapp&peer_kind=direct&contact_type=user`), agent `<Select>` from the agents list, manual JID entry fallback.
- i18n strings for the new UI (en / vi / zh, `channels.json` namespace).
- Runtime hot-apply of contact overrides (live `c.config` read, same as groups — honored without restart after UI edit; the 60 s config resync from `003` RC2 covers direct DB edits).

**Out of scope**:

- Per-contact **listen-only** / **listen graph ID** overrides (`listen_only`, `listen_graph_id` per contact). The global DM `listen_only` + `listen_graph_id` (`config_channels.go:229-230`) keep governing DM capture; the per-contact override changes only **which agent** owns the captured rows. Per-contact listen toggles are a follow-up if operators ask.
- Per-contact `require_mention` (meaningless in DMs), skills/tool allow-lists, system prompts, or quotas per contact.
- Group routing (unchanged — groups keep their existing `groups` map).
- Other channels (Telegram/Zalo/Discord/…). The gateway-level static `cfg.Bindings` (`resolveAgentRoute`) already offers cross-channel per-peer routing for config-file deployments and is untouched; this SRS is the WhatsApp-instance-config equivalent, editable from the web UI.
- Desktop/lite UI. Lite has no channels feature (`internal/edition` limits); the shared Go code must still compile under `-tags sqliteonly`.
- New HTTP/WS endpoints, new store methods, new error codes, DB migration. None required.

## 3. Functional Requirements

### FR-00: Current-State Map — DM Routing Today (verified, no code change)

For traceability, the DM routing path as it exists today:

| Stage | Site | Behavior |
|-------|------|----------|
| Policy gate | `inbound.go:55-57` `checkDMPolicy` | DM pairing/allowlist gate runs **before** any agent resolution. |
| Agent resolution | `inbound.go:186` `resolveAgentID(chatID, peerKind)` | `whatsapp.go:674-684`: override branch is `peerKind == "group"` only → **DM always returns the channel default agent key** (`c.AgentID()`, set from `agents.agent_key` by InstanceLoader at `instance_loader.go:380-388`). |
| Gateway bindings | `cmd/gateway_consumer_normal.go:39-42` | `resolveAgentRoute` (static `config.json` `bindings`) is consulted **only when `msg.AgentID == ""`** — WhatsApp always sets it, so bindings never apply to WhatsApp DMs. |
| Listen-only DM | `inbound.go:235` `resolveGroupAgentUUID(chatID)` | Reads the `groups` map only → DM lookup misses → `""` → `ListenBuffer.Add` falls back to the channel default agent UUID (`listen_buffer.go:113-121`). |
| Session | `cmd/gateway_consumer_normal.go:55` | `BuildScopedSessionKey(agentID, channel, "direct", chatID)` — session isolation is already per-agent, so a contact routed to a different agent automatically gets that agent's own session. |

Acceptance criteria:

- [x] The map above is re-verified at implementation time (re-grep each site; line numbers may drift). _(re-verified 2026-09-06: all FR-00 sites still accurate — `resolveAgentID` `whatsapp.go:674-684`, DM policy `inbound.go:55-57`, LID normalization `inbound.go:36-43`, group-disabled `inbound.go:192-195`, listen attribution `inbound.go:235`, `agentKeyCache` `whatsapp.go:79-87`, `RefreshGroupAgentCache` `whatsapp.go:804-842`, consumer dispatch `gateway_consumer_normal.go:39-42,55`)_

---

### FR-01: Data Model — `contacts` Map in Channel Instance Config

Add a per-contact override map to the WhatsApp channel config, persisted inside the existing `channel_instances.config` JSONB column (opaque to the store — no migration, no `RequiredSchemaVersion` bump, no SQLite schema change).

Persisted fields (new struct, `internal/config/config_channels.go`, mirrors `WhatsAppGroupConfig`):

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| `name` | string | `""` | Human-readable alias (from the contact picker when available). |
| `agent_id` | string | `""` | `agent_key` to route to (e.g. `"raka"`). `""` or `"__default__"` = channel default. UUID form also accepted (mirrors group override fallback, `whatsapp.go:825-828`). |
| `enabled` | `*bool` | `nil` (true) | `false` = bot ignores this contact's DMs entirely (parity with group `enabled`, `inbound.go:192-195`). |

```go
// WhatsAppContactConfig defines per-contact overrides for a WhatsApp channel (DMs).
type WhatsAppContactConfig struct {
    Name    string `json:"name,omitempty"`
    AgentID string `json:"agent_id,omitempty"`
    Enabled *bool  `json:"enabled,omitempty"`
}

// On WhatsAppConfig (config_channels.go:215):
Contacts map[string]*WhatsAppContactConfig `json:"contacts,omitempty"` // per-contact DM overrides, keyed by phone JID
```

Map key: the contact's **phone JID** — `<number>@s.whatsapp.net` (e.g. `6281581484242@s.whatsapp.net`). Operators habitually write local-format numbers (`081581484242`); the UI contact picker prevents this class of mistake by inserting the discovered JID verbatim (FR-04), and the manual-entry path documents the required format.

Example config fragment:

```json
"contacts": {
  "6281511488487@s.whatsapp.net": { "name": "Reski", "agent_id": "jarvis" },
  "6281581484242@s.whatsapp.net": { "name": "Andi",  "agent_id": "raka" }
}
```

Acceptance criteria:

- [x] `WhatsAppContactConfig` struct + `Contacts` field added to `WhatsAppConfig`; JSON round-trips (marshal/unmarshal test). _(`internal/config/whatsapp_contacts_test.go`: `TestWhatsAppContactsConfig_RoundTrip` + `_NilMapOmitted`)_
- [x] Config persists through the existing update path: `channels.instances.update { updates: { config: … } }` stores the map; reload restores it (`channel_instances.go:173-230` treats `config` as opaque except `session_clear` — no handler change expected; verify none needed). _(persistence verified live: `config->'contacts'` in `channel_instances` holds the map. **Load path initially BROKEN and fixed in v0.5**: `factory.go` field-copies the decoded config and dropped `contacts` until the wrapper parse was extended — see revision 0.5. The `session_clear` validator (`channel_instances.go:314-342`) ignores `contacts`; no handler change made.)_
- [x] No migration file, no `RequiredSchemaVersion` change, no SQLite `schema.sql`/`schema.go` change (verification only). _(by inspection — no `migrations/` file touched; `internal/upgrade/version.go` + `sqlitestore/schema.go` untouched)_
- [x] `go build ./...` and `go build -tags sqliteonly ./...` green (struct is shared code). _(both builds ✓, 2026-09-06)_

---

### FR-02: DM Agent Resolution — Contact Override on Both Paths

`resolveAgentID(chatID, peerKind)` (`whatsapp.go:674-684`) gains a `peerKind == "direct"` branch: when `c.config.Contacts[key]` exists with a non-empty `agent_id` (≠ `"__default__"`), that agent key wins; otherwise the channel default (unchanged).

| Input (DM) | Resolved agent |
|------------|----------------|
| No `contacts` entry | channel default (unchanged behavior) |
| Entry with `agent_id: "raka"` | `raka` |
| Entry with `agent_id: ""` or `"__default__"` | channel default |
| Entry with `enabled: false` | message dropped (`PolicyAllow` never reached — mirror the group-disabled early return at `inbound.go:192-195`) |
| Entry with `agent_id` that no longer resolves (deleted/renamed agent) | channel default + `slog.Warn` (mirror `whatsapp.go:830-833` — the dispatch layer's `Agents.Get` failure drop at `gateway_consumer_normal.go:44-48` is the existing last-resort guard) |

Resolution details:

1. **Response path** — `inbound.go:186`: `targetAgentID := c.resolveAgentID(chatID, peerKind)` now returns the contact override for DMs. Downstream is already agent-key based (`bus.InboundMessage.AgentID` → `deps.Agents.Get(ctx, agentID)` → `BuildScopedSessionKey(agentID, …)`), so no dispatch change. The agent_key→UUID resolution for the override (used by KG scoping paths) rides the existing `agentKeyCache` (`whatsapp.go:79-87`) — the cache warm-up `RefreshGroupAgentCache` (`whatsapp.go:804-842`) additionally walks `c.config.Contacts` so renamed/recreated agents resolve without restart (the `CacheKindAgent` subscription in `cmd/gateway_channels_setup.go` already calls it).
2. **Listen-only raw-storage path** — `inbound.go:235`: for `peerKind == "direct"`, agent attribution uses the contact override UUID (channel default when absent), so DM raw messages captured under global `listen_only` land in the override agent's scope. Implementation: extend `resolveGroupAgentUUID` (or add a sibling `resolveContactAgentUUID`) that reads the live `c.config.Contacts` under `c.mu` and resolves agent_key→UUID through the same cache — the `003` RC1 pattern (live config + cached resolution, no per-message DB lookup), generalized to the contacts map.
3. **Concurrency** — reads of `c.config.Contacts` happen under `c.mu` exactly like `c.config.Groups` reads today (`whatsapp.go:853-862`).
4. **Routing logs** — mirror the group logs (`inbound.go:187-214`) at **Debug** level, not Info: DM volume is 1-on-1 and continuous, so per-message Info logs (which exist for groups mainly to support routing triage) would be noisy. Debug is sufficient — operators diagnosing a mis-routed DM can lower the log level:

```go
slog.Debug("whatsapp dm routing", "chat_id", chatID,
    "default_agent", c.AgentID(), "final_agent", targetAgentID,
    "override_applied", targetAgentID != c.AgentID(),
    "contacts_configured", len(c.config.Contacts))
```

Acceptance criteria:

- [ ] A DM from a contact with `agent_id: "raka"` produces an agent run under `raka` (verify: session key `BuildScopedSessionKey("raka", <instance>, "direct", <chatID>)` in the `inbound: scheduling message` log, `gateway_consumer_normal.go:194-201`). _(manual — needs running gateway + real device)_
- [x] A DM from a contact with no entry, `""`, or `"__default__"` runs the channel default agent (no regression). _(`TestResolveAgentID_DirectDefaults` — 3 cases)_
- [x] A DM from a contact with `enabled: false` is dropped before routing (no agent run, no reply), mirroring the group-disabled path. _(by inspection — `inbound.go` direct branch returns before routing when `ct.Enabled != nil && !*ct.Enabled`, mirroring the group-disabled return; the disabled-drop precedes listen-only capture too, so a disabled contact stores nothing)_
- [x] With global `listen_only` on, a DM from an override contact stores its `listen_raw_messages` row with the **override agent's UUID** (not the channel default). _(resolution proven by `TestResolveContactAgentUUID_RuntimeOverrideNoReload`; wiring by inspection — `inbound.go` listen branch calls `resolveContactAgentUUID(senderID)` for `peerKind == "direct"`, and `ListenBuffer.Add` honors `entry.AgentID` over the channel default `listen_buffer.go:113-121`)_
- [x] Runtime edit of a contact's `agent_id` via the UI is honored on the next inbound DM without a gateway restart (UI path → `emitCacheInvalidate` → `InstanceLoader.Reload`; live-config read guarantees fresh values, same as groups per `003` §11.1). _(the live-config property is unit-proven: `TestResolveContactAgentUUID_RuntimeOverrideNoReload` adds an override at runtime with no reload and resolves on the next call — the same RC1-style guarantee as `003`'s `TestResolveGroupAgentUUID_RuntimeOverrideNoReload`; UI→invalidate→Reload path is existing, unchanged)_
- [x] A direct DB edit to `channel_instances.config` is reconciled within the documented resync interval (existing 60 s `ResyncIfStale`, `003` RC2 — no new code). _(existing mechanism, untouched — by inspection)_
- [x] No per-message DB lookup on the hot path: contact override agent_key→UUID resolves via `agentKeyCache` (cache hit after warm-up; on-demand + cache on miss). _(`TestResolveContactAgentUUID_CachesAcrossCalls` — second call resolves with the store ref nil'd, i.e. pure cache hit)_

---

### FR-03: Key Normalization — LID vs Phone JID

WhatsApp uses dual identity: phone JID (`@s.whatsapp.net`) and LID (`@lid`). `handleIncomingMessage` already normalizes the **sender** to the phone JID when LID addressing is in play (`inbound.go:36-43`, `SenderAlt` fallback). For DMs the chat peer **is** the sender, so:

- The **override lookup key** for DMs is the normalized sender JID (`senderID` after `inbound.go:39-43`), not the raw `evt.Info.Chat` string. This guarantees a contact's override matches whether WhatsApp addressed the message via LID or phone JID.
- The `contacts` map **stores** phone-JID keys only (FR-01). No `@lid` keys are written by the UI.

Acceptance criteria:

- [x] A LID-addressed DM (`AddressingMode == LID`, `SenderAlt` present) from an override contact routes to the override agent. _(`TestResolveAgentID_DirectLIDNormalizedSender` — `@lid` chat JID + normalized phone sender JID resolves the override; upstream normalization `inbound.go:39-41` unchanged)_
- [x] Unit test covers: phone-JID lookup hit, LID-mode lookup hit via normalized sender, miss → default. _(same test + `TestResolveAgentID_DirectOverride` + `_DirectDefaults`)_

---

### FR-04: Web UI — Contact Override Editor (WhatsApp Channel Detail)

A new **Contacts** section in the WhatsApp channel detail, mirroring `WhatsAppGroupOverrides` (`ui/web/src/pages/channels/whatsapp-group-overrides.tsx`):

- Editable list of contact overrides: JID (readonly once added), display name, agent `<Select>` (options = `__default__` + the agents list already passed into the tab, `channel-groups-tab.tsx:24`), enabled toggle, remove button.
- **Add flow**: a contact picker fed by discovered DM contacts — `listContacts("", "whatsapp", "direct", "user")` (`use-channel-detail.ts:99-111` → `GET /v1/contacts`, populated by `EnsureContact(..., peerKind, "user", …)` at `inbound.go:175-178` and `gateway_consumer_normal.go:131`) — inserting the contact's `sender_id` (already a phone JID) as the map key and prefilling `name` from the contact's display name. A manual "enter JID" input remains for contacts that have never messaged (placeholder documents the `<number>@s.whatsapp.net` format; validated client-side against that pattern).
- **Save**: merges into the instance config alongside the existing WhatsApp save (`channel-groups-tab.tsx:152-173` — the same `onUpdate({ config })` → `channels.instances.update` path; add `contacts: hasContacts ? contacts : undefined` to the merge). One Save button for groups + join rules + contacts in the WhatsApp tab is acceptable (they already share the tab's save handler).
- Mobile rules: inputs `text-base md:text-sm`; any list/table layout follows the existing responsive pattern of the groups editor; hit areas ≥44px via the existing `@media (pointer: coarse)` CSS.

Acceptance criteria:

- [ ] Operator can add a discovered contact, pick an agent, save, and see the override persist after page reload (config round-trip). _(manual — needs browser; code-complete, `pnpm build` green)_
- [x] Manual JID entry rejects a value not matching `^\d+@s\.whatsapp\.net$` (inline error, no save). _(`contact-jid.test.ts` — `isValidWhatsAppPhoneJid` 6 cases incl. local-format/LID/group-JID/`+`-prefixed rejects; component shows `whatsappContactOverrides.invalidJid` inline and blocks add)_
- [x] Removing all overrides omits `contacts` from the saved config (no stale `{}` payload difference — mirror the groups `undefined` cleanup, `channel-groups-tab.tsx:88-93`). _(by inspection — `channel-groups-tab.tsx` `handleSave`: `contacts: hasContacts ? contacts : undefined` with `hasContacts = Object.keys(contacts).length > 0`, identical to the groups cleanup)_
- [x] `pnpm build` (ui/web) green; no raw i18n keys rendered. _(build ✓; all keys present in en/vi/zh, namespace `channels` already registered — `useTranslation("channels")` in the new component)_

---

### FR-05: i18n (en / vi / zh)

New keys in `ui/web/src/i18n/locales/{en,vi,zh}/channels.json` under a `whatsappContactOverrides` block (mirroring `whatsappGroupOverrides` at `channels.json:559`). Proposed keys:

| Key | English | Vietnamese | Chinese |
|-----|---------|------------|---------|
| `whatsappContactOverrides.title` | Direct Message Contacts | Danh bạ tin nhắn riêng | 私聊联系人 |
| `whatsappContactOverrides.addContact` | Add contact | Thêm liên hệ | 添加联系人 |
| `whatsappContactOverrides.contactJid` | Contact JID (e.g. 628123456789@s.whatsapp.net) | JID liên hệ (vd: 628123456789@s.whatsapp.net) | 联系人 JID（例：628123456789@s.whatsapp.net） |
| `whatsappContactOverrides.invalidJid` | Must be a phone JID like 628123456789@s.whatsapp.net | Phải là JID số điện thoại như 628123456789@s.whatsapp.net | 必须是形如 628123456789@s.whatsapp.net 的手机号 JID |
| `whatsappContactOverrides.agent` | Agent | Agent | Agent |
| `whatsappContactOverrides.defaultAgent` | Channel default | Mặc định của kênh | 渠道默认 |
| `whatsappContactOverrides.enabled` | Enabled | Bật | 启用 |
| `whatsappContactOverrides.saveContacts` | Save | Lưu | 保存 |
| `whatsappContactOverrides.saving` | Saving… | Đang lưu… | 保存中… |

Acceptance criteria:

- [x] All keys present in `en`, `vi`, `zh` with identical key sets. _(13 keys per locale — the 9 proposed + `hint`/`nameLabel`/`enabledHint`/`knownContacts` the component also renders; added to all three `channels.json` files)_
- [x] Namespace `channels` already registered — no `004`-FR-07-style mismatch (verify `useTranslation("channels")` at the new component). _(verified — `whatsapp-contact-overrides.tsx` uses `useTranslation("channels")`; `pnpm build` green)_

---

### FR-06: Authorization & Tenant Scope (unchanged envelope)

No new endpoint. Contact overrides are read/written through the existing `channels.instances.update` WS method and its HTTP sibling, with their existing admin gating and audit emission (`channel_instances.go:221-229` — `emitCacheInvalidate` + `emitAudit("channel_instance.updated", …)`). The config lives inside the tenant-scoped `channel_instances` row; the DM policy/pairing gate (`inbound.go:55-57`, `012` sliding expiry) runs **before** routing and is untouched — a per-contact agent override never bypasses pairing.

Acceptance criteria:

- [x] No new WS method, HTTP route, or store method; no auth change to the existing update path. _(by inspection — only `config_channels.go` (struct), `whatsapp.go` (resolution), `inbound.go` (routing/drop/log), `commands.go` (caller signature), UI files touched)_
- [x] DM policy still gates before agent resolution (a non-paired contact with an override still gets the pairing flow, not the override agent). _(by inspection — `checkDMPolicy` at `inbound.go:55-57` runs before `resolveAgentID` and the contacts block; contact resolution added after the gate, none before)_
- [x] Contact override resolution is confined to the channel's own instance config (per-instance; two WhatsApp instances in the same tenant have independent `contacts` maps). _(by inspection — `c.config.Contacts` is the per-instance config struct loaded per channel instance; no shared/global map)_

---

### FR-07: No Regressions — Groups, Sessions, Memory

- **Group routing unchanged**: the `groups` map and its resolution are untouched; all `003` acceptance behavior preserved.
- **Session behavior (documented, by design)**: sessions are keyed per agent (`BuildScopedSessionKey(agentID, …)`), so a contact routed to Raka has a **separate session history** from the channel-default agent's history with the same contact. Changing a contact's override agent later starts a fresh session under the new agent (the old session remains in the Sessions menu). This matches group behavior (a group's agent switch has the same effect) and is the expected semantic, not a defect.
- **Memory/KG**: the override agent's episodic memory, knowledge graph, and `user_context_files` apply automatically because the whole agent loop runs under the override agent — no extra wiring. DM raw capture (listen-only) attributes to the override agent per FR-02.2.

Acceptance criteria:

- [x] Group override routing still passes the existing `group_agent_test.go` suite (`003` RC1 regression tests). _(all 5 `TestResolveGroupAgentUUID_*` green + new `TestResolveAgentID_GroupBranchUnchanged` proving a sender's contact entry never affects group routing)_
- [ ] A contact switched from agent A to agent B starts a new session under B; A's prior session is still listed in Sessions (manual check). _(manual — needs running gateway)_
- [x] `go test -race ./internal/channels/whatsapp/` green (modulo the 2 pre-existing `TestMimeToExt` failures documented since `003`). _(144 passed / 2 pre-existing `TestMimeToExt` failures — byte-identical failure set on untouched `media_utils_test.go`, documented since `003`/`013`/`014`)_

---

### FR-08: LID→Phone Resolution for Contact Overrides (routing-only)

**Added at live verification (v0.3).** WhatsApp's LID-first rollout delivers some DMs **LID-only**: the sender arrives as `<lid>:<device>@lid` with no `SenderAlt`, so the FR-03 normalization (`inbound.go:39-41`) cannot fire and the sender stays LID-keyed. Contact overrides are keyed by phone JID (FR-01) → lookup misses → channel default. Observed live 2026-09-06: sender `141089709252847:83@lid`, override `6281511488487@s.whatsapp.net`, reply from the default agent despite everything else being correct.

Fix: `resolveContactLookupKey(senderID)` resolves `@lid` senders to their phone JID via whatsmeow's native LID map — `client.Store.LIDs.GetPNForLID(ctx, lid)` (`go.mau.fi/whatsmeow/store` `LIDStore`). The map (`whatsmeow_lid_map` table) is auto-maintained by whatsmeow from group-participant and message traffic; the lookup is in-process and cache-backed (`CachedLIDMap.lidToPNCache`) — no network call, no DB hit after the first lookup per LID.

| Sender (DM) | Lookup key used for `contacts` map |
|---|---|
| `<number>@s.whatsapp.net` (phone JID) | unchanged (passthrough — no LID store call) |
| `<lid>:<dev>@lid`, mapping known | resolved phone JID, **device suffix stripped** — bare `<pn>@s.whatsapp.net` (whatsmeow's `getLIDMapping` returns the PN carrying the LID's `:dev` suffix, `sqlstore/lidmap.go:100`; the helper zeroes `pn.Device` so the key matches FR-01's bare-phone-JID keys) |
| `<lid>:<dev>@lid`, mapping unknown / store unavailable / client nil | raw senderID (graceful fallback = pre-FR-08 behavior) |

Applied at 3 routing-path sites only:
1. `resolveAgentID` direct branch (response-path agent routing).
2. `resolveContactAgentUUID` (listen-only raw-storage attribution).
3. The DM `EnsureContact` call — the contact collector stores the resolved phone JID as `sender_id`, so the UI contact picker (fed by `channel_contacts`) inserts phone-JID keys that match the override map.

**Out of scope by choice (Option 2, operator-approved):** the DM policy/pairing gate keeps using the raw sender key. Existing `paired_devices` rows may be LID-keyed (the operator's is: `141089709252847:83@lid`); normalizing identity before the pairing check would invalidate those rows and force re-pairing. Full LID/phone identity unification (pairing + allowlist + contacts history) is a separate follow-up task.

Acceptance criteria:

- [x] A LID-only DM (no `SenderAlt`) from a contact whose override is keyed by phone JID routes to the override agent. _(`TestResolveAgentID_DirectLIDSenderHitsPhoneKeyedOverride` — LID chat + LID sender + phone-keyed override → `kala`; stub mirrors whatsmeow's suffix-carrying PN so the device-strip is exercised end-to-end)_
- [x] The listen-only raw-storage path attributes such a DM to the override agent's UUID. _(`TestResolveContactAgentUUID_LIDSenderHitsPhoneKeyedOverride`)_
- [x] Phone-JID senders skip the LID store entirely (no lookup cost on the common path). _(`TestResolveContactLookupKey_PhonePassthroughAndNilClient` — passthrough asserted)_
- [x] Unmapped LID / nil client / store unavailable → raw senderID fallback (no panic, no changed behavior vs pre-FR-08). _(same test — nil client case; `TestResolveContactLookupKey_NoMappingFallsBack`)_
- [x] No network call and no per-message DB hit: resolution rides whatsmeow's cache-backed `LIDStore` (`CachedLIDMap`), in-process. _(by construction — `GetPNForLID` on the sqlstore's `CachedLIDMap`, no whatsmeow API call)_
- [x] Pairing/policy identity untouched: `checkDMPolicy` still receives the raw sender, so LID-keyed `paired_devices` rows keep matching. _(by inspection — `resolveContactLookupKey` is called only inside the 3 routing sites, never before the policy gate)_
- [ ] Live: the operator's DM (`141089709252847:83@lid`, override → `kala`) is answered by Kala after rebuild+restart. _(manual — needs gateway restart + real device)_

## 4. System Impact

- **Config** (`internal/config/config_channels.go`): new `WhatsAppContactConfig` struct + `Contacts` field on `WhatsAppConfig` (FR-01).
- **Channel** (`internal/channels/whatsapp/whatsapp.go`): `resolveAgentID` direct branch (FR-02); contact-aware agent-UUID resolution for the listen path (extend `resolveGroupAgentUUID` or sibling); `RefreshGroupAgentCache` also walks `Contacts` (FR-02.1); `resolveContactLookupKey` LID→phone resolution for the 3 contact-override lookup sites (FR-08).
- **Channel factory** (`internal/channels/whatsapp/factory.go`): the DB-instance config decode maps `contacts` into `config.WhatsAppConfig.Contacts` (wrapper parse alongside `groups`) — without this, DB instances load with an empty contacts map and every override silently misses (v0.5 fix; the factory field-copies rather than decoding the full struct, so every NEW WhatsAppConfig map field must be mapped here too).
- **Inbound** (`internal/channels/whatsapp/inbound.go`): contact-disabled early return for DMs (FR-02 table); DM routing debug log (FR-02.4); listen-only branch passes the contact-resolved agent UUID (FR-02.2).
- **WS methods** (`internal/gateway/methods/channel_instances.go`): no change expected (config opaque; verify `session_clear`-style validation not needed for `contacts`).
- **Web UI** (`ui/web/src/pages/channels/`): new `whatsapp-contact-overrides.tsx` (mirror of `whatsapp-group-overrides.tsx`), wired into the WhatsApp branch of `channel-groups-tab.tsx` (`:131-205`) with the shared save merge (FR-04).
- **i18n**: 9 keys × en/vi/zh in `channels.json` (FR-05).
- **No DB migration**, no store change, no new error codes, no desktop UI change (lite has no channels).

## 5. Test Plan

- **Unit — resolution matrix** (`internal/channels/whatsapp/`): `resolveAgentID` for `peerKind == "direct"` — override hit / no entry / `""` / `"__default__"` / `enabled:false` drop; group branch regression (existing `group_agent_test.go` still green). `-race`.
- **Unit — LID normalization** (FR-03): LID-mode DM resolves the override via normalized sender JID.
- **Unit — listen-path attribution**: contact override agent_key→UUID resolves through `agentKeyCache` (cache hit, on-demand miss + cache, unresolvable-key warn + fallback `""`), mirroring the 5 `003` cases generalized to the contacts map.
- **Unit — config round-trip**: `WhatsAppConfig` with `contacts` marshals/unmarshals; nil map omitted (`omitempty`).
- **Frontend**: pure helper test for the JID validation regex (`^\d+@s\.whatsapp\.net$`) if extracted per the `007` §0.7 convention (no `@testing-library/react` in repo); render/click checks manual.
- **Manual (live, master tenant)**: add contact `6281581484242@s.whatsapp.net` → agent Raka via the UI; DM from that phone → reply identity/model is Raka's (session log shows agent `raka`); DM from an unlisted contact → channel default (Jarvis); remove override → back to default; with global `listen_only` on, the DM raw row carries Raka's UUID.
- **Checklist**: `go build ./...`, `go build -tags sqliteonly ./...`, `go vet ./...`, `go test -race ./internal/channels/whatsapp/`, `pnpm build` in `ui/web`.

## 6. Risks and Open Questions

| Risk or question | Draft decision |
|------------------|----------------|
| Operators entering local-format numbers (`0815…`) instead of intl JIDs (`62815…`) → override never matches. | UI picker inserts discovered JIDs verbatim; manual entry validates the `@s.whatsapp.net` pattern with an inline error (FR-04). Document the format in the input placeholder + this SRS. |
| Contact chats the bot from a **different** WhatsApp number than the one configured (multi-SIM / number change) → no match, routes to default. | Accepted — override is per-JID by design. Operator adds the new JID. A phone-number-prefix fuzzy match was considered and rejected (ambiguous, breaks LID normalization guarantees). |
| LID-only contacts (no `SenderAlt` available) cannot be normalized to a phone JID → lookup misses. | **Materialized live 2026-09-06** (WhatsApp LID-first rollout) and **resolved for routing by FR-08 (v0.3)**: `resolveContactLookupKey` resolves `@lid` senders to phone JIDs via whatsmeow's `LIDStore` (`whatsmeow_lid_map`, cache-backed, in-process), applied on the 3 contact-override routing sites + the DM contact-collector key. **Pairing/policy identity deliberately untouched** (Option 2): the operator's `paired_devices` row is LID-keyed, and early identity normalization would force re-pairing. Full LID/phone identity unification (pairing, allowlist, historical `channel_contacts` LID rows) = separate follow-up task. |
| Switching a contact's agent strands the old session history under the old agent. | By design (FR-07) — matches group behavior; old sessions remain visible in the Sessions menu. |
| Override agent deleted/renamed after configuration. | Resolution warns + falls back to channel default (FR-02 table); `RefreshGroupAgentCache` (extended to contacts) re-resolves on `CacheKindAgent` events so renames heal without restart — same lifecycle as group overrides (`003` §11.1). |
| Should DM overrides also support per-contact `listen_graph_id`? | Deferred (out of scope). Global DM `listen_graph_id` applies; the override changes only agent attribution. Follow-up if operators want per-contact KG scopes. |
| Should this generalize to a shared `PeerConfig` for all channels in instance config? | Deferred. WhatsApp-first mirrors how `groups` shipped; a cross-channel generalization can unify later without a breaking config change (additive map). |
| Telegram/other channels parity? | Out of scope. Telegram already has per-group/topic overrides; per-peer DM routing for other channels can adopt the same instance-config pattern per channel when requested. |

## 7. Implementation Plan

1. **Config struct (FR-01)**: add `WhatsAppContactConfig` + `Contacts` to `WhatsAppConfig`; marshal/unmarshal round-trip test.
2. **Resolution (FR-02/FR-03)**: extend `resolveAgentID` with the direct branch (keyed on normalized sender JID); extend the listen-path UUID resolution + `RefreshGroupAgentCache` to walk `Contacts`; add the DM disabled-drop and debug log in `inbound.go`.
3. **Unit tests**: resolution matrix, LID normalization, cache behavior, config round-trip (§5).
4. **i18n (FR-05)**: add the 9 keys to `channels.json` × en/vi/zh **before** wiring the UI (per the i18n ordering rule).
5. **UI (FR-04)**: new `whatsapp-contact-overrides.tsx`; wire into `channel-groups-tab.tsx` WhatsApp branch + save merge; JID validation helper + test.
6. **Checklist**: `go build ./...`, `go build -tags sqliteonly ./...`, `go vet ./...`, `go test -race ./internal/channels/whatsapp/`, `pnpm build` (ui/web).
7. **Live verification (§5 manual)**: master-tenant round-trip — override contact → Raka replies; unlisted contact → default; listen-only DM attribution; runtime edit without restart.

## 8. Proposed Error Codes

No new canonical error codes. The feature rides existing surfaces:

| Code (existing) | Meaning | Reuse site |
|------|---------|------------|
| `INVALID_REQUEST` (`pkg/protocol/errors.go`) | Malformed instance update payload (existing `channels.instances.update` guards). | Unchanged. |
| `channel_instance.updated` (audit event) | Audit trail for config edits incl. contact overrides. | Existing `emitAudit` at `channel_instances.go:228`. |
| _(inline UI validation)_ | JID pattern mismatch in manual entry — client-side inline error (`whatsappContactOverrides.invalidJid`), never reaches the server. | New UI-only. |
