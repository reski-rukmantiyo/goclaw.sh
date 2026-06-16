# Software Requirements Specification: Group-Scoped User Approval

**Project**: GoClaw Gateway
**Release**: 2026.3.0
**Version**: 0.2-draft
**Date**: 2026-06-16
**Status**: Draft
**Difficulty**: Medium-High
**Estimate**: 6 days

---

## Revision History

| Version | Date | Changes |
|---------|------|---------|
| 0.1-draft | 2026-06-16 | Initial draft. Grounded against live code: groups already exist (migrations `000079`/`000083`, full CRUD + `group_members` + `join_requests` + 3-level hierarchy + `group.*` permissions). The genuine gap is **user-level approval** — `users.status` has no `pending` value, both creation paths force `active` (`auth_handler.go:283` self-register, `users.go:271` pre-provision). Extends existing groups with a pending-activation lifecycle; no new group table. Scope decisions at draft: extend existing groups; approvers = tenant admin + group admin; pending = new `users.status='pending'` value. |
| 0.2-draft | 2026-06-16 | **Locked implementation decisions after scoping Q&A.** Triggers built this iteration: **self-register toggle** + **admin invite** + **group join-request gating (restrict access until approved)**. Pre-provision pending and external-provider (OIDC) pending are **out of scope** — `auth_provider='local'` only; OIDC first-login stays `active`. **No email infra exists** in the repo (verified: zero SMTP/SendGrid/Mailgun libs, no email service) — so: approved local users get an **admin-set temp password shown once in UI** (not an email link); notifications are **in-app/WS queue only** (no email). Join-request gating = a pending requester **MUST NOT** receive any group-scoped access until `approved` — this touches the permission/scope resolution path (new FR-JR). Estimate raised to 6 days; Difficulty Medium-High. Risks R1/R3 resolved; R6 added (engine integration); R7 added (temp-password security). |

---

## 1. Summary

This SRS defines **Group-Scoped User Approval** — a pending-activation lifecycle for tenant users tied to the existing group system, plus access-gating for pending group join-requests. Today every user is created `active` immediately, on both the self-registration path (`POST /auth/register`, `internal/http/auth_handler.go:283`) and the admin pre-provision path (`POST /v1/users`, `internal/http/users.go:271`), and the only approval flow is group join-requests (`join_requests`) that approve membership without explicitly gating resource access.

This feature adds:
1. A `pending` user status (local users only) and an approval workflow scoped by group. A pending user may carry a **target group** (`pending_group_id`); approvers = **tenant Owner/Admin or the target group's Group Admin**. Approving activates the account (`active`), enrolls in tenant + group, assigns role, and the approving admin sets a **one-time temp password** shown once in the UI. Rejecting deactivates.
2. **Group join-request gating**: a tenant user with a pending `join_request` to a closed group MUST receive **no group-scoped access** until approved — surfaced as an explicit "pending approval" state, not a silent deny, and counted in the same in-app approval queue.

Composes with — does **not** duplicate or replace:
- `002-feat-multi-auth-module-srs.md` — owns the user/group/RBAC data model. §9.6 lists "Optional: admin approval for self-registration" as deferred; **this implements that option (local users)**.
- `001-feat-tenant-user-crud.md` — owns tenant-user CRUD + `is_owner` / system-role authorization.
- `004-feat-roles-page.md` — owns the Roles page + system-role read-only policy.
- The existing `groups` / `group_members` / `join_requests` tables and `GroupStore` — reused as-is.

Out of scope (locked): external/OIDC-provider pending (OIDC first-login stays `active`), email infra, pre-provision `pending` flag.

## 2. Scope

**In scope**:

- Adding `pending` to `users.status` (PostgreSQL CHECK + SQLite schema + `store.UserStatusPending` constant + `handleStatusChange` switch).
- A per-tenant toggle **"registration requires approval"** that makes `POST /auth/register` create `local` users as `pending` (default off = current behavior).
- A new **admin invite** path (`POST /v1/users/invite`) that creates a `pending` local user, optionally targeted at a group, with a requested role.
- **Approve** (`POST /v1/users/{id}/approve`) and **reject** (`POST /v1/users/{id}/reject`) endpoints; **approve requires the admin to set a one-time temp password** returned in the response (shown once in UI).
- Listing pending users (`GET /v1/users?status=pending` — store filter exists via `UserListParams.Status`) with group-scoped visibility.
- Blocking login for `pending` local users (auth returns 403 `account_pending`).
- **Group join-request gating (FR-JR)**: pending requester gets no group-scoped access; explicit pending state surfaced; counted in the unified in-app approval queue.
- **Unified in-app/WS approval queue**: pending users + pending join-requests in one surface with badge counts. No email.
- Frontend approve/reject/invite controls + pending views, with the `001`/`006` affordance rules.
- i18n (en/vi/zh); audit events; cache invalidation.

**Out of scope**:

- External/OIDC-provider (Entra/Google) pending lifecycle. OIDC first-login stays `active`. No OIDC callback pending branch is built. (Local-only this iteration.)
- Email infrastructure / outbound email (invite/approve/reject emails). No email service exists; **all notifications are in-app/WS**.
- **Pre-provision** pending flag on `POST /v1/users`. Stays `active`.
- A new `groups` table or rewrite of the group model. Reused unchanged.
- Cross-tenant approval (always within caller's tenant).
- Email verification gate.
- Changing the Owner bootstrap (first user → Owner). See FR-03 + Risk R4.

## 3. Functional Requirements

### FR-00: Add `pending` to `users.status` (schema, no behavior change alone)

`users.status` is constrained to `active|suspended|deactivated` on both DBs:
- PostgreSQL CHECK: `migrations/000079_multi_auth_module.up.sql:21`.
- SQLite: `internal/store/sqlitestore/schema.go:782`.
- Go constants: `internal/store/user_store.go:18-22` — **no pending**.
- Handler switch: `internal/http/users.go:365-372` rejects anything outside the three values.

Adding `pending` requires a dual-DB migration **plus** the constant **plus** the handler switch update. Both DB systems updated together (CLAUDE.md "Migrations (dual-DB)").

Persisted fields / schema changes:

| Change | File / Location | Notes |
|--------|-----------------|-------|
| PG: extend `users.status` CHECK to include `'pending'` | `migrations/000089_user_pending_status.up.sql` (new) | `DROP` old CHECK, `ADD` CHECK `status IN ('pending','active','suspended','deactivated')`. |
| PG: `pending_group_id UUID` nullable column on `users` | `migrations/000089_...` (same) | Target group for a pending user; NULL = tenant-wide. Drives approver set (FR-05). |
| PG: bump `RequiredSchemaVersion` | `internal/upgrade/version.go` | 88 → 89. |
| SQLite: `schema.sql` full-schema block | `internal/store/sqlitestore/schema.sql` | Add `'pending'` to CHECK + `pending_group_id` column. |
| SQLite: incremental migration patch | `internal/store/sqlitestore/schema.go` `migrations` map | Same two changes as a versioned patch; bump `SchemaVersion` (missing this crashes desktop on startup). |
| Go constant | `internal/store/user_store.go` | Add `UserStatusPending = "pending"`. |
| Handler switch | `internal/http/users.go:365-372` | `handleStatusChange` keeps rejecting `pending` (admins cannot manually pend someone). Only approve/reject transition through `pending`. |

Acceptance criteria:

- [ ] `ALTER TABLE` applies cleanly on PG 18; `pending_group_id` nullable, no NOT NULL-default issue on existing rows.
- [ ] SQLite fresh-DB (`schema.sql`) and existing-DB (`schema.go` patch) both include the CHECK change + column; `SchemaVersion` bumped; desktop starts without crash.
- [ ] `store.UserStatusPending` exists and is used by approve/reject/store paths.
- [ ] `handleStatusChange` still rejects `pending` via `PATCH /v1/users/{id}/status` (422); only approve/reject endpoints touch `pending`.

---

### FR-01: Tenant Toggle — "Registration Requires Approval"

A per-tenant setting controls whether self-registration produces active or pending **local** users. Default: **off** (current behavior).

| Setting value | `POST /auth/register` behavior |
|---------------|-------------------------------|
| `registration_requires_approval = false` (default) | User created `active` (current, `auth_handler.go:283`). Unchanged. |
| `registration_requires_approval = true` | Local user created `pending`; `pending_group_id` from the body (optional); cannot log in until approved. |

Setting lives in the tenant auth config block (`auth.registration.requires_approval`, per `002` §9.3), read at registration time.

Acceptance criteria:

- [ ] Toggle off → self-registration byte-for-byte unchanged (regression guard).
- [ ] Toggle on → new local registrant has `status='pending'`, `pending_group_id` set or NULL, cannot authenticate (FR-06).
- [ ] Per-tenant scoping; one tenant's setting does not affect another.
- [ ] External-provider (OIDC) first-login is **unaffected** — still creates `active` (out of scope). Local-only.

---

### FR-02: Admin Invite Creates a Pending Local User

A new endpoint lets an authorized admin invite a user by email, creating the account `pending`, optionally targeted at a group with a requested role. No password is set (the approving admin sets a temp password — FR-03). No email is sent.

```text
POST /v1/users/invite
```

Request body:

```json
{
  "email": "jane@example.com",
  "display_name": "Jane Doe",
  "role": "member",
  "group_id": "01234567-89ab-cdef-0123-456789abcdef",
  "message": "Optional note to approver"
}
```

| Field | Type | Required | Notes |
|-------|------|----------|-------|
| `email` | string | yes | Globally unique; 409 if an active/suspended/pending user already has it. |
| `display_name` | string | no | Falls back to email local-part. |
| `role` | string | no | `admin|member|viewer` or custom role slug. Default `member`. `owner` → 422. |
| `group_id` | string | no | Target group UUID. If set, that group's admins become eligible approvers (FR-05). |
| `message` | string | no | Stored on the pending record for the approver. |

Behavior:
1. Guard: caller holds `user.pre_provision` (tenant admin) **or** `group.manage_members` for the named `group_id` (group admin). FR-05.
2. Duplicate-email check: existing `active|suspended|pending` user with the email → 409.
3. Create user `auth_provider='local'`, `password_hash=NULL`, `status='pending'`, `pending_group_id` from body.
4. Store requested role + message for approval (see R2).
5. Emit `user.invited` audit + `cache_invalidate` (`kind=users`). **No email.**
6. Return 201 `{ "id": "<user-uuid>", "status": "pending" }`.

Acceptance criteria:

- [ ] Tenant admin invites without group_id (tenant-wide pending).
- [ ] Group admin invites **only** with a `group_id` they administer; foreign group_id → 403.
- [ ] Duplicate active/suspended/pending email → 409.
- [ ] `role='owner'` → 422.
- [ ] Created row: `status='pending'`, `password_hash=NULL`, correct `pending_group_id`, `auth_provider='local'`.

---

### FR-03: Approve a Pending User (admin sets temp password)

```text
POST /v1/users/{id}/approve
```

Required body:

```json
{ "password": "TempStr0ng!Pass", "role": "member", "group_id": "01234567-...", "show_once": true }
```

| Field | Type | Required | Notes |
|-------|------|----------|-------|
| `password` | string | yes | Temp password the admin sets. Must pass the tenant password policy (reuse the `002` §9.3 policy). Hashed bcrypt before storage. |
| `role` | string | no | Override the requested role. Default = requested. Not `owner` (except first-user bootstrap). |
| `group_id` | string | no | Override the target group. |
| `show_once` | bool | no | If true, response echoes the plaintext password once for the admin to relay out-of-band. Default false. |

Approval transitions `pending → active` and performs enrollment, atomically (single transaction):

| Step | Action |
|------|--------|
| 1 | Validate caller authorization (FR-05). |
| 2 | Load user; require `status='pending'`. Else → 409 `user.not_pending`. |
| 3 | Validate temp password against tenant policy. Hash with bcrypt. |
| 4 | Set `status='active'`, `password_hash=<bcrypt>`. |
| 5 | Enroll in tenant: insert `tenant_users` (`TenantStore.AddUser` / `CreateTenantUserReturning`). |
| 6 | If group resolved (requested `pending_group_id` or override) → `GroupStore.AddMember` (`joined_via='admin_add'`). |
| 7 | Assign role via `RoleStore.AssignUserRole` (not `owner`). |
| 8 | Clear `pending_group_id`. |
| 9 | Emit `user.approved` audit + `cache_invalidate` (`kind=users` + `tenant_users`). |

**Temp-password security (R7):**
- The plaintext password is returned **only** when `show_once=true`, in the single approve response. It is never persisted in plaintext, never logged, never returned by any other endpoint.
- The UI must display it once (copy-to-clipboard dialog) and not retain it. The admin relays it to the user out-of-band.
- On first login, the user is encouraged (not forced this iteration) to change it. Forced-change is deferred.

**First-user-as-owner:** if approval creates the **first** tenant user, the approved user becomes Owner (`is_owner=true`, no RBAC role) — mirroring `auth_handler.go:262-266`. Override `role` ignored. See R4.

Acceptance criteria:

- [ ] Approving a `pending` user sets `active`, stores bcrypt hash, enrolls tenant + group + role — one transaction; rollback on any failure leaves the user `pending` (no partial state).
- [ ] Weak temp password (fails tenant policy) → 422; user stays `pending`.
- [ ] Non-pending user → 409 `user.not_pending`.
- [ ] First-tenant-user approval → Owner regardless of requested role.
- [ ] `pending_group_id` cleared; `password_hash` populated.
- [ ] With `show_once=true`, plaintext password appears **only** in this response; no other endpoint or log exposes it.
- [ ] Audit `user.approved` recorded with actor + role/group context.

---

### FR-04: Reject a Pending User

```text
POST /v1/users/{id}/reject
```

Optional body: `{ "reason": "..." }`. Transitions `pending → deactivated`; no enrollment, no password, no role.

Acceptance criteria:

- [ ] Rejecting a `pending` user sets `status='deactivated'`; no `tenant_users` row, no membership, no role, `password_hash` stays NULL.
- [ ] Non-pending user → 409 `user.not_pending`.
- [ ] Audit `user.rejected` recorded with actor + reason.
- [ ] A `deactivated`-by-rejection email is reusable by a later invite (R7) — `deactivated → pending` via re-invite.

---

### FR-05: Authorization — Tenant Admin OR Group Admin

| `pending_group_id` | Who may approve/reject | Permission gate |
|--------------------|------------------------|-----------------|
| NULL (tenant-wide) | Tenant Owner or Admin | `user.pre_provision` (`AdminSeedPermissions`, `catalog.go:71-79`) |
| set (group-scoped) | Tenant Owner/Admin **OR** a Group Admin of that group | `user.pre_provision` **or** `group.manage_members` for that group |

Group-admin authority is **group-scoped**: group A's admin cannot approve a user targeted at group B. Tenant Owner bypasses RBAC. Tenant Admin always permitted.

| Operation | Tenant Owner | Tenant Admin | Group Admin (target) | Group Admin (other) | Member/Viewer |
|-----------|:---:|:---:|:---:|:---:|:---:|
| List tenant-wide pending | ✅ | ✅ | ❌ | ❌ | ❌ |
| List group-scoped pending (own group) | ✅ | ✅ | ✅ | ❌ | ❌ |
| Approve/reject tenant-wide pending | ✅ | ✅ | ❌ | ❌ | ❌ |
| Approve/reject group-scoped (target = own group) | ✅ | ✅ | ✅ | ❌ | ❌ |
| Invite (tenant-wide) | ✅ | ✅ | ❌ | ❌ | ❌ |
| Invite (to own group) | ✅ | ✅ | ✅ | ❌ | ❌ |

Acceptance criteria:

- [ ] Group admin approves a pending user targeted at their group → 200.
- [ ] Group admin approves a pending user targeted at a **different** group → 403.
- [ ] Group admin approves a tenant-wide (`NULL`) pending user → 403.
- [ ] Tenant Admin approves any own-tenant pending → 200.
- [ ] Member/Viewer → 403.

---

### FR-06: Pending Local Users Cannot Authenticate

Local login rejects `pending` with 403, distinct from `suspended`/`deactivated`:

```text
POST /auth/login (pending local user)
→ 403 { "error": "account_pending" }
```

Refresh-token call for a pending user → 401/403. **OIDC/external paths are unchanged** (out of scope) — an external-provider user is never created `pending` this iteration, so no callback-pending branch is built.

Acceptance criteria:

- [ ] Local login as `pending` → 403 `account_pending`, no token.
- [ ] Refresh for `pending` → 401/403.
- [ ] `suspended`/`deactivated` behavior unchanged.
- [ ] OIDC callback path untouched (no pending branch added).

---

### FR-07: Listing Pending Users (group-scoped visibility)

`GET /v1/users?status=pending` returns pending users; the store filter exists (`UserListParams.Status`, `user_store.go:52-59`). Visibility scoping:

| Caller | Sees |
|--------|------|
| Tenant Owner/Admin | All pending in the tenant (group-scoped + tenant-wide) |
| Group Admin | Only pending with `pending_group_id` in a group they administer |
| Member/Viewer | 403 |

Acceptance criteria:

- [ ] Tenant Admin sees all pending rows; group admin sees only their groups'; member/viewer → 403.
- [ ] Response includes `pending_group_id`, requested role, message where present.
- [ ] Pagination/search reuse existing `UserListParams` (no new query path).

---

### FR-JR: Group Join-Request Gating — Restrict Access Until Approved

A tenant user with a `join_request` in `pending` status to a closed group is in a **pending-membership** state and **MUST NOT receive any group-scoped access** until the request is `approved` (→ `AddMember`). This is the heaviest FR — it touches permission/scope resolution.

**Core invariant:** `join_requests.status='pending'` grants **zero** access to group-scoped resources (artifacts, agents, audit entries, group APIs). Access is granted **only** by an approved request producing a `group_members` row + group roles.

Today the data model already enforces most of this: membership lives in `group_members`, and `join_requests` is separate, so a pending requester is not a member. The work is to **(a) verify and lock the invariant** in the permission/scope path, **(b) surface an explicit pending state**, and **(c) fold pending join-requests into the unified approval queue**.

| Concern | Requirement |
|---------|-------------|
| Membership gate | `IsGroupMember(ctx, groupID, userID)` and `GetUserGroups(ctx, userID)` MUST return false/empty for a user whose only relation is a `pending` `join_request`. Verify + add a regression test. |
| Permission resolution | Group-scoped permissions resolved from `group_roles` apply **only** to members. A pending requester resolves no group permissions. Verify `GetUserEffectivePermissions` path. |
| Scope visibility | Resource scoping (group-scoped artifacts/agents) MUST exclude pending requesters — they see the same as any non-member (nothing), but with an explicit "pending approval" surface where applicable. |
| Surfacing | API/UI exposes "my pending join-requests" so a requester sees an explicit pending state instead of a silent deny. New: `GET /v1/groups/my-join-requests` (or extend `/v1/users/me`) listing the user's pending requests. |
| Unified queue | Pending join-requests appear alongside pending users in the group admin's in-app approval queue (FR-08), reviewed via the existing `PATCH /v1/groups/{id}/join-requests/{reqId}`. |
| Approved/Rejected | Approved → existing `ReviewJoinRequest` adds membership (`joined_via='request_approved'`) + group roles grant access. Rejected → no access + in-app notification (not email). Audit already records group/membership events. |

**Implementation note (verify, do not assume):** the scope/permission engine is in `internal/permissions/` (catalog + resolution) and resource scoping spread across stores/handlers. Before changing behavior, trace exactly where group-scoped access is decided and confirm a pending requester is already excluded. If any path grants group access without a `group_members` row, close it. If all paths are already membership-gated, this FR is primarily **surface + unify + lock with tests** (lower risk). The audit/grep step is mandatory before declaring scope-engine changes.

Acceptance criteria:

- [ ] A user whose only relation to group G is a `pending` `join_request` cannot read group G's resources (artifacts/agents/audit) — verified by a test that asserts `IsGroupMember=false` and `GetUserGroups` excludes G.
- [ ] After `PATCH .../join-requests/{reqId}` approve → the user becomes a member and gains group-scoped access per existing `002` rules.
- [ ] After reject → no access; requester sees a rejection state in-app (no email).
- [ ] `GET /v1/groups/my-join-requests` (or `/v1/users/me` extension) returns the caller's pending join-requests with group + status + requested-at.
- [ ] Pending join-requests appear in the group admin's unified approval queue (FR-08).
- [ ] No permission path grants group access on a `pending` join-request alone (regression test).

---

### FR-08: Frontend — Unified Approval Queue + Approve/Reject/Invite

A unified approval surface in the tenants-admin area (reuse `001` layout). It aggregates two pending kinds:

| Kind | Source | Approver |
|------|--------|----------|
| Pending users | `GET /v1/users?status=pending` | tenant admin / group admin of `pending_group_id` |
| Pending join-requests | existing `join_requests` (group-scoped) | group admin of the group |

UI elements:
- **Approval queue** tab/list with a **badge count** of pending items, split by kind (or filterable). WS-driven (`cache_invalidate` refreshes).
- **Pending user row**: email, display_name, requested role, target group (or "tenant-wide"), requested-by, requested-at. **Approve** opens a dialog requiring a temp-password field (+ optional role/group override) before `POST /approve` with `show_once=true`; plaintext password shown once in a copy dialog. **Reject** opens a reason prompt.
- **Invite** dialog: email, role, group picker → `POST /v1/users/invite`.
- **Pending join-request row**: user, group, message, requested-at. Approve/reject via the existing join-request action.
- **Applicant view**: a user with a pending join-request sees an explicit "Pending approval" state for that group (via `GET /v1/groups/my-join-requests`) rather than a silent empty/403. A `pending` user attempting login sees an "awaiting approval" message (FR-06 `account_pending`).

Affordance rules (consistency with `001`/`006`):
- Group-admin caller sees only their groups' pending rows/requests; approve/reject enabled only where authorized.
- Tenant admin sees all rows with full approve/reject.
- Buttons that would 403 on submit are suppressed.

Hooks use `useMutation` + `HttpClient` (`001` §7.1 pattern), not WebSocket RPC for mutations; queries are WS-driven.

Acceptance criteria:

- [ ] Unified queue lists both pending kinds for the viewed tenant; group admin sees only their groups' items; member/viewer → 403 / no nav entry.
- [ ] Approve requires a temp password; on success the user leaves pending, the plaintext password shows once in a copy dialog, then the user appears in the active list.
- [ ] Reject moves the user out of pending; reason recorded.
- [ ] Invite creates a pending user appearing in the queue.
- [ ] Applicant sees explicit "pending approval" for a pending join-request group, and "awaiting approval" on a pending login attempt.
- [ ] Unauthorized actions not rendered.
- [ ] Mobile responsive: dialogs full-screen on narrow viewport, inputs `text-base md:text-sm` (16px), tables in `overflow-x-auto`.

---

### FR-09: i18n (en / vi / zh)

New user-facing strings in the `users`/`tenants` namespaces in all three locale dirs (`ui/web/src/i18n/locales/{en,vi,zh}/`). Backend messages get a key in `internal/i18n/keys.go` + translations in `catalog_en.go`/`catalog_vi.go`/`catalog_zh.go`.

New key examples: `pendingUsers`, `approveUser`, `rejectUser`, `inviteUser`, `accountPending`, `userNotPending`, `registrationRequiresApproval`, `tempPassword`, `tempPasswordShownOnce`, `joinRequestPending`, `awaitingApproval`.

Acceptance criteria:

- [ ] Every new backend error message has a key in `keys.go` + 3 catalogs.
- [ ] Every new UI string is in all 3 locale files with identical key sets.
- [ ] No raw keys render in the UI.

---

### FR-10: Audit + Cache Invalidation

| Event | Trigger | `resource_type` | `detail` includes |
|-------|---------|-----------------|-------------------|
| `user.invited` | Invite created | `user` | email, target group, requested role, invited_by |
| `user.approved` | Approve | `user` | role assigned, group enrolled, approved_by |
| `user.rejected` | Reject | `user` | reason, rejected_by |
| `group.join_request.*` | existing join-request approve/reject | `membership` | existing (`000079` audit) |
| `cache_invalidate` | any user-level mutation | — | `kind=users` (+ `tenant_users` on enroll) |

Acceptance criteria:

- [ ] All audit events fire with correct `actor_id` + resource context.
- [ ] `cache_invalidate` refreshes WS clients (queue + user lists).
- [ ] **Temp passwords are never written to audit `detail`, logs, or any cache.**

## 4. System Impact

- **DB (dual):** PG migration `000089` (status CHECK + `pending_group_id`) + `RequiredSchemaVersion` 88→89; SQLite `schema.sql` + `schema.go` patch + `SchemaVersion` bump. **Both required.**
- **Backend store:** `store.UserStatusPending`; approve/reject transactional multi-store ops (`UserStore` status+hash, `TenantStore.AddUser`, `GroupStore.AddMember`, `RoleStore.AssignUserRole`, clear `pending_group_id`). New `UserStore.ApprovePending`/`RejectPending` orchestrators (or orchestrate in handler, mirroring `tenants.go`). Reuse existing stores — no new join tables.
- **Permission/scope (FR-JR):** audit `internal/permissions/` + resource-scoping sites; verify `IsGroupMember`/`GetUserGroups`/`GetUserEffectivePermissions` exclude pending requesters; close any leak; add regression tests. This is the riskiest area — trace before changing.
- **Backend HTTP:** new routes in `internal/http/users.go`: `POST /v1/users/invite`, `POST /v1/users/{id}/approve`, `POST /v1/users/{id}/reject`; `GET /v1/groups/my-join-requests` (or `/v1/users/me` extension); extend `GET /v1/users?status=pending` group-scoping; extend `POST /auth/register` with the toggle + optional group; block pending in `handleLogin`; keep `handleStatusChange` switch locked.
- **RBAC:** reuse `user.pre_provision`, `user.list`, `group.manage_members`, `group.view_hierarchy`. **Latent bug (R5):** `users.go:40-41` guards enroll/unenroll with `user.enroll`/`user.unenroll` — **absent from the catalog**. Approval enrollment uses `user.pre_provision` + `group.manage_members`; do **not** depend on the missing strings.
- **Frontend:** unified approval queue + approve/reject/invite UI + applicant pending state in `ui/web/src/pages/tenants-admin/`; hooks via `useMutation`/`HttpClient`.
- **i18n:** new keys in 3 locale dirs + backend catalogs.
- **No email infra added.** No change to `groups`/`group_members`/`join_requests` schema.

## 5. Test Plan

- **Migration test:** `000089` forward on PG; `users.status` accepts `pending`; existing rows unaffected; `pending_group_id` nullable. SQLite fresh + upgrade paths produce CHECK + column; desktop starts.
- **Handler tests:**
  - Invite: admin invite (no group) → 201; group admin invite to own group → 201; foreign group → 403; duplicate (active/pending) → 409; `role=owner` → 422.
  - Approve: pending→active with hash+enroll+group+role in one txn; non-pending → 409; weak temp password → 422 (stays pending); first-user → Owner; group admin approves own-group → 200; foreign → 403; tenant-wide by group admin → 403; `show_once` returns plaintext only in this response.
  - Reject: pending→deactivated, no enroll/hash; non-pending → 409.
  - Login: pending → 403 `account_pending`; suspended/deactivated unchanged; OIDC untouched.
  - List pending: admin all; group admin own-group only; member → 403.
  - `PATCH /v1/users/{id}/status` with `pending` → 422 (FR-00 guard).
- **FR-JR tests:** pending requester `IsGroupMember=false`, `GetUserGroups` excludes group, no group permissions; after approve → member + access; after reject → no access; `my-join-requests` returns pending list; no permission path grants access on pending join-request alone.
- **Service/txn test:** approve rolls back fully on any sub-step failure — no partial state (no hash without enroll, etc.).
- **Security test:** plaintext temp password never persisted/logged/returned by any endpoint other than the one `show_once=true` approve response.
- **Frontend tests:** unified queue renders both kinds; approve-with-temp-password; reject; invite; applicant pending state; group-admin visibility filtering; unauthorized buttons suppressed.
- **i18n:** new keys in all 3 locale dirs; no raw keys render.
- **Manual:** toggle on → self-register → pending → approve (set temp pw, copy once) → active + can log in with temp pw + enrolled in group; reject; group admin sees only own-group pending; pending join-request requester sees "pending approval" and no group access until approved.

## 6. Risks and Open Questions

| # | Risk or question | Draft decision |
|---|------------------|----------------|
| R1 | **Trigger scope (resolved).** | This iteration builds **self-register toggle** (FR-01), **admin invite** (FR-02), and **group join-request gating** (FR-JR). Pre-provision pending and OIDC pending are out of scope. |
| R2 | `pending_group_id` on `users` — enough, or need requested-role/message/requested-by columns? | Keep `users` lean: only `pending_group_id` is structural. Requested role + message + requested-by captured in audit `detail` at invite time and read back for the pending list. If the pending list needs them cheaply without joining audit, denormalize later. |
| R3 | **Post-approval credentials (resolved).** No email infra. | Approve requires the admin to set a **temp password**, returned once (`show_once=true`) for out-of-band relay. No email link, no forgot-password dependency. |
| R4 | **First-user-as-owner vs approval.** A new tenant with `registration_requires_approval=true` has no active user until approval — who approves the first? | A bootstrap approver (the Owner who enabled the toggle, or a Gateway-Token/master-scope operator) approves the first pending user → becomes Owner. Document; edge case for brand-new tenants. |
| R5 | **Latent catalog bug.** `users.go:40-41` references `user.enroll`/`user.unenroll`, absent from `catalog.go`. | Approval enrolls via `user.pre_provision` + `group.manage_members` (present). Do **not** depend on the missing strings. File the catalog gap as separate cleanup. |
| R6 | **Join-request gating touches the permission/scope engine.** | Trace `IsGroupMember`/`GetUserGroups`/`GetUserEffectivePermissions` + resource-scoping sites **before** changing behavior. If access is already membership-gated (likely), this FR is mostly surface + unify + lock-with-tests (lower risk). If any path grants group access without a `group_members` row, close it. The trace step is mandatory. |
| R7 | **Temp-password security.** Plaintext returned once; risk of logging/caching/leak. | Returned **only** in the `show_once=true` approve response; never persisted plaintext, never logged, never in audit `detail`, never returned elsewhere. UI shows once in a copy dialog. Forced-change-on-first-login deferred (documented). |
| R8 | **No OIDC scope this iteration** means mixed tenants (local + OIDC) have inconsistent approval. | Acceptable for v1. OIDC users stay active on first login. Document the inconsistency; OIDC pending is a follow-up that requires wiring the OIDC callback handler (none found in `internal/http` — may need building first). |
| R9 | **Dual-DB migration ordering.** | Both sides in one change; `go build -tags sqliteonly ./...` in checklist; bump `SchemaVersion` + `RequiredSchemaVersion` together. |

## 7. Implementation Plan

1. **Schema (dual-DB):** PG migration `000089` (status CHECK + `pending_group_id`), `RequiredSchemaVersion` 88→89; SQLite `schema.sql` + `schema.go` patch + `SchemaVersion` bump. Add `store.UserStatusPending`. (FR-00)
2. **Trace permission/scope (FR-JR prerequisite):** audit `internal/permissions/` + group-scoping sites; confirm `IsGroupMember`/`GetUserGroups`/`GetUserEffectivePermissions` exclude pending requesters; close any leak; write regression tests first. (FR-JR)
3. **Store:** `ApprovePending` (txn: status+hash, `AddUser`, `AddMember`, `AssignUserRole`, clear `pending_group_id`), `RejectPending` (status only). (FR-03/FR-04)
4. **Auth:** block `pending` in `handleLogin` (local) → `account_pending`. OIDC untouched. (FR-06)
5. **HTTP — invite/approve/reject/list:** new routes in `internal/http/users.go` with FR-05 authz; `GET /v1/groups/my-join-requests`; extend `GET /v1/users?status=pending` group-scoping; keep `handleStatusChange` locked. (FR-02/FR-03/FR-04/FR-05/FR-07/FR-JR)
6. **Registration toggle:** read `auth.registration.requires_approval` in `handleRegister`; create `pending` when on (local only). (FR-01)
7. **Frontend:** unified approval queue + approve (temp-password dialog, one-time copy) + reject + invite + applicant pending state + affordance rules. (FR-08)
8. **i18n + audit:** keys in 3 locale dirs + backend catalogs; emit `user.invited/approved/rejected` + `cache_invalidate`. No plaintext in audit. (FR-09/FR-10)
9. **Tests:** handler/store/txn/security/FR-JR/frontend per §5.
10. **Checklist:** `go build ./...`, `go build -tags sqliteonly ./...`, `go vet ./...`, `go test -race ./tests/integration/`, `pnpm build` in `ui/web`.

## 8. Proposed Error Codes

| Code | Meaning |
|------|---------|
| `user.not_pending` | Approve/reject on a non-pending user (existing conflict path, 409). |
| `user.already_exists` | Invite email matches an existing active/suspended/pending user (existing duplicate path, 409). |
| `account_pending` | Login/refresh by a `pending` user — not yet approved (new 403, distinct from `account_suspended`). |
| `user.password_policy` | Approve temp password fails the tenant password policy (new/reuse 422). |
| `user.target_role_forbidden` | Invite `role='owner'`, or role override not permitted (reuse `001`/`006` `MsgTargetRoleForbidden` 403). |
| `group.forbidden` | Group admin acting on a group they do not administer (existing group-scope 403). |
| `user.permission_denied` | Caller is neither tenant admin nor group admin of the target (existing 403). |
| `request.validation_failed` | Malformed invite body / invalid group UUID / unknown role slug (existing `ErrInvalidRequest` 400). |
