# Software Requirements Specification: Group-Scoped User Approval

**Project**: GoClaw Gateway
**Release**: 2026.3.0
**Version**: 0.1-draft
**Date**: 2026-06-16
**Status**: Draft
**Difficulty**: Medium
**Estimate**: 5 days

---

## Revision History

| Version | Date | Changes |
|---------|------|---------|
| 0.1-draft | 2026-06-16 | Initial draft. Grounded against live code: groups already exist (migrations `000079`/`000083`, full CRUD + `group_members` + `join_requests` + 3-level hierarchy + `group.*` permissions). The genuine gap is **user-level approval** — `users.status` has no `pending` value, and both creation paths force `active` (`auth_handler.go:283` self-register, `users.go:271` pre-provision). This SRS extends existing groups with a pending-activation lifecycle; it does **not** add a new group table. Approved scope decisions: extend existing groups (not new entity); approvers = tenant admin + group admin; pending modeled as a new `users.status='pending'` value (not a separate approvals table). |

---

## 1. Summary

This SRS defines **Group-Scoped User Approval** — a pending-activation lifecycle for tenant users that ties into the existing group system. Today every user is created with `status='active'` immediately, on both the self-registration path (`POST /auth/register`, `internal/http/auth_handler.go:283`) and the admin pre-provision path (`POST /v1/users`, `internal/http/users.go:271`). A tenant that needs to vet new members has no mechanism to do so: there is no `pending` status, no approval queue, and no approver role between "submitted" and "can log in."

This feature adds a `pending` user status and an approval workflow scoped by group. A pending user may carry an optional **target group** (`pending_group_id`); the set of approvers is then **the tenant Owner/Admin or the target group's Group Admin**. Approving a pending user activates the account (`active`), enrolls it in the tenant, adds it to the target group (if any), and assigns the requested role. Rejecting deactivates it.

This SRS **composes with** — does **not** duplicate or replace:
- `002-feat-multi-auth-module-srs.md` — owns the user/group/RBAC data model. §9.6 lists "Optional: admin approval for self-registration" as deferred; **this SRS implements that deferred option**.
- `001-feat-tenant-user-crud.md` — owns tenant-user CRUD + the `is_owner` / system-role authorization model (Owner hidden from admins, admins cannot manage admins).
- `004-feat-roles-page.md` — owns the Roles page and system-role read-only policy.
- The existing `groups` / `group_members` / `join_requests` tables and `GroupStore` — reused as-is.

The group **join-request** flow (`POST /v1/groups/{id}/join`, `ReviewJoinRequest`) already exists for existing-tenant-members requesting entry to a closed group. That flow is **out of scope** here — this SRS covers approval of *new or not-yet-active users*, not membership-in-group changes for already-active users.

## 2. Scope

**In scope**:

- Adding `pending` to the `users.status` value set (PostgreSQL CHECK + SQLite schema + `store.UserStatusPending` constant + the `handleStatusChange` switch).
- A per-tenant toggle **"registration requires approval"** that, when enabled, makes `POST /auth/register` create users as `pending` instead of `active`.
- A new **admin invite** path (`POST /v1/users/invite`) that creates a `pending` user, optionally targeted at a group, with a requested role.
- Listing pending users (`GET /v1/users?status=pending` — the store filter already exists via `UserListParams.Status`) with group-scoped visibility for group admins.
- **Approve** (`POST /v1/users/{id}/approve`) and **reject** (`POST /v1/users/{id}/reject`) endpoints with the tenant-admin-OR-group-admin authorization rule.
- Blocking login for `pending` users (auth returns 403, not a session).
- Frontend: a pending-users view + approve/reject controls in the tenants-admin users area, with the admin-vs-owner affordance rules from `001`/`006`.
- i18n (en/vi/zh) for all new strings; audit events; cache invalidation.

**Out of scope**:

- A new `groups` table or rewrite of the existing group model (`000079`/`000083`). Groups are reused unchanged.
- The group **join-request** flow (`join_requests` / `ReviewJoinRequest`). Already implemented for active tenant members joining a closed group.
- Email verification (a separate, optional identity-verification gate). Invite/registration may *trigger* an email, but email-verify-as-gate is not introduced here.
- Cross-tenant approval. Approval is always within the caller's tenant scope (enforced by the existing tenant-scoped user store and `001` auth model).
- Changing the Owner bootstrap rule (first user in a tenant auto-becomes owner, `auth_handler.go:262-266`). When registration requires approval, the *first* pending registrant still becomes Owner on approval — see FR-03 + Risk R4.
- The existing `tenant_users.role` semantics. Roles are assigned via `user_roles` (`000083`), unchanged.

## 3. Functional Requirements

### FR-00: Add `pending` to `users.status` (schema, no behavior change alone)

The `users.status` column must accept `pending`. Today it is constrained to `active|suspended|deactivated` on both DBs:
- PostgreSQL CHECK: `migrations/000079_multi_auth_module.up.sql:21`.
- SQLite: `internal/store/sqlitestore/schema.go:782`.
- Go constant set: `internal/store/user_store.go:18-22` (`UserStatusActive/Suspended/Deactivated`) — **no pending**.
- Handler switch: `internal/http/users.go:365-372` rejects anything outside the three values with `"status must be one of: active, suspended, deactivated"`.

Adding `pending` therefore requires a dual-DB migration **plus** the constant **plus** the handler switch update. Per CLAUDE.md "Migrations (dual-DB)", both DB systems must be updated together.

Persisted fields / schema changes:

| Change | File / Location | Notes |
|--------|-----------------|-------|
| PG: extend `users.status` CHECK to include `'pending'` | `migrations/000089_user_pending_status.up.sql` (new) | `ALTER TABLE users DROP CONSTRAINT users_status_check, ADD CONSTRAINT users_status_check CHECK (status IN ('pending','active','suspended','deactivated'));` |
| PG: `pending_group_id UUID` nullable column on `users` | `migrations/000089_...` (same) | The group a pending user is targeted at; NULL = tenant-wide pending. Drives the approver set (FR-05). |
| PG: bump `RequiredSchemaVersion` | `internal/upgrade/version.go` | 88 → 89. |
| SQLite: schema.sql full-schema block | `internal/store/sqlitestore/schema.sql` | Add `'pending'` to the CHECK + `pending_group_id` column. |
| SQLite: incremental migration patch | `internal/store/sqlitestore/schema.go` `migrations` map | Add the same two changes as a versioned patch; bump `SchemaVersion` constant (per CLAUDE.md dual-DB rule — missing this crashes the desktop edition on startup). |
| Go constant | `internal/store/user_store.go` | Add `UserStatusPending = "pending"`. |
| Handler switch | `internal/http/users.go:365-372` | `handleStatusChange` must allow `pending` only when set internally (approve/reject do not set `pending` directly; see FR-04). Direct `PATCH /v1/users/{id}/status` with `pending` stays rejected (an admin cannot manually pend someone) — keep the switch locked to `active|suspended|deactivated` for that endpoint. |

Acceptance criteria:

- [ ] `ALTER TABLE` migration applies cleanly on PG 18 (forward). `pending_group_id` is nullable, no NOT NULL default issues on existing rows.
- [ ] SQLite schema.sql (fresh-DB path) and the schema.go incremental patch (existing-DB path) both include the CHECK change + column; `SchemaVersion` bumped; desktop edition starts without crash.
- [ ] `store.UserStatusPending` constant exists and is used by the approve/reject/store paths.
- [ ] `handleStatusChange` (`PATCH /v1/users/{id}/status`) still rejects `pending` with the existing 422 message (admins cannot manually pend a user); only approve/reject endpoints transition into/out of `pending`.

---

### FR-01: Tenant Toggle — "Registration Requires Approval"

A per-tenant setting controls whether self-registration produces an active or a pending user. Default: **off** (current behavior preserved — self-register → `active`).

| Setting value | `POST /auth/register` behavior |
|---------------|-------------------------------|
| `registration_requires_approval = false` (default) | User created `active` (current behavior, `auth_handler.go:283`). Unchanged. |
| `registration_requires_approval = true` | User created `pending`; `pending_group_id` from the request body (optional); cannot log in until approved. |

The setting lives in the tenant auth config block already documented in `002` §9.3 (`auth.registration.requires_approval`). It is read from the tenant's config at registration time.

Acceptance criteria:

- [ ] With the toggle off, self-registration is byte-for-byte unchanged (regression guard).
- [ ] With the toggle on, a newly registered user row has `status='pending'`, `pending_group_id` set to the requested group or NULL, and cannot authenticate (FR-06).
- [ ] The toggle is scoped per-tenant; one tenant requiring approval does not affect another.

---

### FR-02: Admin Invite Creates a Pending User

A new endpoint lets an authorized admin invite a user by email, creating the account in the `pending` state, optionally targeted at a group with a requested role.

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
  "message": "Optional note to approver/reviewer"
}
```

| Field | Type | Required | Notes |
|-------|------|----------|-------|
| `email` | string | yes | Globally unique; 409 if an active user already exists with it. |
| `display_name` | string | no | Falls back to email local-part. |
| `role` | string | no | One of `admin|member|viewer` or a custom role slug. Default `member`. `owner` rejected (422). |
| `group_id` | string | no | Target group UUID. If set, that group's admins become eligible approvers (FR-05). |
| `message` | string | no | Stored on the pending record for the approver. |

Behavior:
1. Guard: caller holds `user.pre_provision` (tenant admin) **or** `group.manage_members` for the named `group_id` (group admin). See FR-05.
2. Duplicate-email check: if a user with the email already exists and is `active|suspended`, return 409. If a `pending` user with the email exists, return 409 (no duplicate invites).
3. Create the user with `auth_provider='local'`, `password_hash=NULL` (no password yet — set on first login / invite-accept), `status='pending'`, `pending_group_id` from the body.
4. Store the requested role + message for use at approval (see FR-03 design note on where this lives).
5. Emit an invite/audit event; send an invite email if email is configured (best-effort; not a gate).
6. Return 201 with `{ "id": "<user-uuid>", "status": "pending" }`.

Acceptance criteria:

- [ ] Tenant admin can invite without a group_id (tenant-wide pending).
- [ ] Group admin can invite **only** with a `group_id` they administer; a foreign group_id returns 403.
- [ ] Duplicate active-email → 409; duplicate pending-email → 409.
- [ ] `role='owner'` → 422.
- [ ] The created row has `status='pending'` and the correct `pending_group_id`.

---

### FR-03: Approve a Pending User

```text
POST /v1/users/{id}/approve
```

Optional body:

```json
{ "role": "member", "group_id": "01234567-...", "send_credentials": true }
```

Approving transitions `pending → active` and performs enrollment side effects. The approver may override the requested role/group.

Transition side effects (must be atomic — single transaction):

| Step | Action |
|------|--------|
| 1 | Validate caller authorization (FR-05). |
| 2 | Load user; require `status='pending'`. If `active|suspended|deactivated` → 409 (`user.not_pending`). |
| 3 | Set `status='active'`. |
| 4 | Enroll in tenant: insert `tenant_users` (reuse `TenantStore.AddUser` / `CreateTenantUserReturning`). |
| 5 | If a group is resolved (requested `pending_group_id` or override) → add to `group_members` via `group.manage_members` path (`GroupStore.AddMember`, `joined_via='admin_add'`). |
| 6 | Assign role via `RoleStore.AssignUserRole` (not `owner`). |
| 7 | Clear `pending_group_id`. |
| 8 | Emit audit event `user.approved` + `cache_invalidate` (`kind=users`). |

**First-user-as-owner rule:** if this approval creates the **first** tenant user, the approved user becomes Owner (`is_owner=true`, no RBAC role) — mirroring the existing bootstrap at `auth_handler.go:262-266`. The override `role` is ignored in that case. See Risk R4.

Acceptance criteria:

- [ ] Approving a `pending` user sets `status='active'`, enrolls in tenant, adds to target group (if any), assigns role — all in one transaction (rollback on any failure).
- [ ] Approving a non-pending user → 409 `user.not_pending`.
- [ ] First-tenant-user approval promotes to Owner regardless of requested role.
- [ ] `pending_group_id` is cleared after approval.
- [ ] Audit event `user.approved` recorded with actor, resource, and the group/role context in `detail`.

---

### FR-04: Reject a Pending User

```text
POST /v1/users/{id}/reject
```

Optional body: `{ "reason": "..." }`.

Rejection transitions `pending → deactivated` and performs **no** enrollment. The account can never log in.

Acceptance criteria:

- [ ] Rejecting a `pending` user sets `status='deactivated'`; no `tenant_users` row created; no group membership; no role assigned.
- [ ] Rejecting a non-pending user → 409 `user.not_pending`.
- [ ] Audit event `user.rejected` recorded with actor + reason.

---

### FR-05: Authorization — Tenant Admin OR Group Admin

The approver set depends on whether the pending user is group-scoped.

| `pending_group_id` | Who may approve/reject | Permission gate |
|--------------------|------------------------|-----------------|
| NULL (tenant-wide) | Tenant Owner or Admin (system role Admin) | `user.pre_provision` (already in `AdminSeedPermissions`, `catalog.go:71-79`) |
| set (group-scoped) | Tenant Owner/Admin **OR** a Group Admin of that group | `user.pre_provision` **or** `group.manage_members` resolved for that group |

Group-admin authority is **group-scoped**: a group admin of group A cannot approve a pending user targeted at group B. Tenant Owner bypasses RBAC entirely (`001` model). Tenant Admin approval is always permitted (admins outrank group admins).

This mirrors the existing `001`/`006` authorization pattern: visibility and mutation are decided together by caller role + target scope, not caller role alone.

| Operation | Tenant Owner | Tenant Admin | Group Admin (target group) | Group Admin (other group) | Member/Viewer |
|-----------|:---:|:---:|:---:|:---:|:---:|
| List tenant-wide pending | ✅ | ✅ | ❌ | ❌ | ❌ |
| List group-scoped pending (own group) | ✅ | ✅ | ✅ | ❌ | ❌ |
| Approve/reject tenant-wide pending | ✅ | ✅ | ❌ | ❌ | ❌ |
| Approve/reject group-scoped pending (target = own group) | ✅ | ✅ | ✅ | ❌ | ❌ |
| Invite (tenant-wide) | ✅ | ✅ | ❌ | ❌ | ❌ |
| Invite (to own group) | ✅ | ✅ | ✅ | ❌ | ❌ |

Acceptance criteria:

- [ ] A group admin approving a pending user targeted at their group succeeds (200).
- [ ] A group admin approving a pending user targeted at a **different** group → 403.
- [ ] A group admin approving a tenant-wide (`pending_group_id` NULL) pending user → 403.
- [ ] Tenant Admin approves any pending user (own-tenant) → 200.
- [ ] Member/Viewer hitting approve/reject → 403.

---

### FR-06: Pending Users Cannot Authenticate

The login path must reject `pending` users with 403, distinct from `suspended`/`deactivated`. Today login rejects only `account_suspended` (`002` §9.6). Add `account_pending`:

```text
POST /auth/login (pending user)
→ 403 { "error": "account_pending" }
```

OIDC/external-provider first-login for a pending user must also short-circuit: if the resolved/created user is `pending`, do not issue a session. (Self-registration via external provider is tenant-configured; if `registration_requires_approval` is on, an external first-login creates a `pending` user and returns an "awaiting approval" page rather than a session.)

Acceptance criteria:

- [ ] Local login as a `pending` user → 403 `account_pending`, no token issued.
- [ ] Refresh-token call for a `pending` user → 401/403.
- [ ] External-provider callback that resolves to a `pending` user → renders "awaiting approval", no session.

---

### FR-07: Listing Pending Users (group-scoped visibility)

`GET /v1/users?status=pending` returns pending users. The store filter already exists (`UserListParams.Status`, `user_store.go:52-59`). This FR adds **visibility scoping**:

| Caller | Sees |
|--------|------|
| Tenant Owner/Admin | All pending users in the tenant (group-scoped + tenant-wide) |
| Group Admin | Only pending users with `pending_group_id` in a group they administer |
| Member/Viewer | 403 (no `user.list`) |

Acceptance criteria:

- [ ] Tenant Admin sees all pending rows; a group admin sees only their groups' pending rows; a member/viewer gets 403.
- [ ] The response includes `pending_group_id` and requested role/message where present.
- [ ] Pagination/search reuse the existing `UserListParams` (no new query path).

---

### FR-08: Frontend — Pending Users View + Approve/Reject

A pending-users surface in the tenants-admin users area (reuse the existing tenants-admin layout from `001`):

- A "Pending" tab/filter on the tenant users page showing `status=pending` users (or a dedicated pending list).
- Each pending row shows: email, display_name, requested role, target group (or "tenant-wide"), requested-by, requested-at.
- **Approve** action: opens a small confirmation allowing role/group override before `POST /approve`.
- **Reject** action: opens a reason prompt before `POST /reject`.
- **Invite** action: a dialog (email, role, group picker) → `POST /v1/users/invite`.

Affordance rules (consistency with `001`/`006`):
- A group-admin caller sees only their groups' pending rows; approve/reject controls are enabled only for rows they are authorized on.
- A tenant admin sees all pending rows with full approve/reject.
- Buttons that would 403 on submit must be suppressed (never offered).

Hooks use `useMutation` + `HttpClient` (the `001` §7.1 pattern), not WebSocket RPC.

Acceptance criteria:

- [ ] Pending tab lists pending users for the viewed tenant; a group admin sees only their groups' rows.
- [ ] Approve with a role override applies the override; the user disappears from pending and appears in the active users list.
- [ ] Reject removes the user from pending (moves to a deactivated/past view or filters out).
- [ ] Invite dialog creates a pending user and it appears in the pending list.
- [ ] Unauthorized actions are not rendered (no 403 on normal use).
- [ ] Mobile responsive: dialogs full-screen on narrow viewport, inputs `text-base md:text-sm` (16px), tables in `overflow-x-auto`.

---

### FR-09: i18n (en / vi / zh)

All new user-facing strings added to the `users` (and/or `tenants`) namespace in all three locale dirs (`ui/web/src/i18n/locales/{en,vi,zh}/`). Backend user-facing messages get a key in `internal/i18n/keys.go` + translations in `catalog_en.go`/`catalog_vi.go`/`catalog_zh.go`.

New key examples: `pendingUsers`, `approveUser`, `rejectUser`, `inviteUser`, `accountPending`, `userNotPending`, `registrationRequiresApproval`.

Acceptance criteria:

- [ ] Every new backend error message has a key in `keys.go` + 3 catalogs.
- [ ] Every new UI string is in all 3 locale files with identical key sets.
- [ ] No raw keys render in the UI.

---

### FR-10: Audit + Cache Invalidation

Every approve/reject/invite emits:

| Event | Trigger | `resource_type` | `detail` includes |
|-------|---------|-----------------|-------------------|
| `user.invited` | Invite created | `user` | email, target group, requested role, invited_by |
| `user.approved` | Approve | `user` | role assigned, group enrolled, approved_by |
| `user.rejected` | Reject | `user` | reason, rejected_by |
| `cache_invalidate` | any of the above | — | `kind=users` (+ `kind=tenant_users` on enroll) |

Acceptance criteria:

- [ ] All three audit events fire with correct actor (`actor_id`) and resource context.
- [ ] `cache_invalidate` ensures WS clients see fresh pending/active user lists.

## 4. System Impact

- **DB (dual):** PG migration `000089` (status CHECK + `pending_group_id` column) + `RequiredSchemaVersion` 88→89; SQLite `schema.sql` + `schema.go` migration patch + `SchemaVersion` bump. **Both required** — missing SQLite side crashes desktop.
- **Backend store:** `store.UserStatusPending` constant; approve/reject are transactional multi-store operations (`TenantStore` + `GroupStore` + `RoleStore`). Consider a new `UserStore.ApprovePending` / `RejectPending` method that orchestrates the transaction, or orchestrate in the handler (mirrors how `tenants.go` orchestrates `UpdateOwnerFlag` + RBAC). Reuse existing `AddUser`, `AddMember`, `AssignUserRole` — no new join tables.
- **Backend HTTP:** new routes in `internal/http/users.go`: `POST /v1/users/invite`, `POST /v1/users/{id}/approve`, `POST /v1/users/{id}/reject`. Extend `POST /auth/register` (`auth_handler.go`) with the approval toggle + optional group. Extend `GET /v1/users` with group-scoped pending visibility. Update `handleStatusChange` switch. Block pending in `handleLogin`.
- **Backend auth:** login (local + external callback) must reject `pending`.
- **RBAC:** reuse `user.pre_provision`, `user.list`, `group.manage_members`, `group.view_hierarchy`. **Latent-bug note:** `users.go:40-41` guards `enroll`/`unenroll` routes with permission strings (`user.enroll`/`user.unenroll`) that are **absent from the catalog** (`internal/permissions/catalog.go`). The approve path enrolls via `user.pre_provision`; do **not** depend on the missing `user.enroll`. Track the catalog gap separately (Risk R5).
- **Frontend:** new pending-users tab + approve/reject/invite UI in `ui/web/src/pages/tenants-admin/`; hooks in `use-tenant-detail.ts` (`useMutation` + `HttpClient`).
- **i18n:** new keys in 3 locale dirs + backend catalogs.
- **No change** to `groups`/`group_members`/`join_requests` tables.

## 5. Test Plan

- **Migration test:** `000089` applies forward on PG; `users.status` accepts `pending`; existing rows unaffected; `pending_group_id` nullable. SQLite fresh-DB + existing-DB upgrade paths both produce the CHECK + column; desktop starts.
- **Handler tests:**
  - Invite: tenant admin invite (no group) → 201 pending; group admin invite to own group → 201; group admin invite to foreign group → 403; duplicate email (active or pending) → 409; `role=owner` → 422.
  - Approve: pending→active with enroll+group+role in one txn; non-pending → 409; first-tenant-user → Owner; group admin approves own-group target → 200; foreign group → 403; tenant-wide pending approved by group admin → 403.
  - Reject: pending→deactivated, no enroll; non-pending → 409.
  - Login: pending user → 403 `account_pending`; suspended/deactivated unchanged.
  - List pending: admin sees all; group admin sees only own-group rows; member → 403.
  - `PATCH /v1/users/{id}/status` with `pending` → still 422 (regression guard on FR-00).
- **Service/txn test:** approve rolls back fully if any sub-step (enroll/add-member/assign-role) fails — no partial state.
- **Frontend tests:** pending tab renders rows; approve with override; reject; invite dialog; group-admin visibility filtering; unauthorized buttons suppressed.
- **i18n:** new keys present in all 3 locale dirs; no raw keys render.
- **Manual:** toggle registration-requires-approval on → self-register → pending → approve → active + can log in + enrolled in group; reject path; group admin sees only own-group pending.

## 6. Risks and Open Questions

| # | Risk or question | Draft decision |
|---|------------------|----------------|
| R1 | **Trigger scope ambiguity (open assumption).** The approval *trigger* question (self-register / invite / group-join / pre-provision) was not locked in scoping. | This draft covers **self-registration** (via the tenant toggle, FR-01) and **admin invite** (FR-02) as the pending-producing paths. The group **join-request** flow is already implemented (`join_requests`) and explicitly out of scope. **Pre-provision** (`POST /v1/users`) stays `active` by default; if desired, add an optional `pending=true` body flag later. Confirm these triggers with the owner before build. |
| R2 | `pending_group_id` lives on `users` — is one nullable column enough, or do we need requested-role / message / requested-by columns too? | Keep `users` lean: only `pending_group_id` is structural. Requested role + message + requested-by are captured in the **audit log `detail` JSONB** at invite time and read back for the pending list. If the pending list needs those fields cheaply without joining audit_log, add a small `pending_requests` view or denormalized columns — deferred. |
| R3 | **Invite with no password** — a `pending` local user has `password_hash=NULL`. How do they authenticate after approval? | On approval, either (a) email a one-time set-password/invite-accept link, or (b) require the user to go through "forgot password" on first login. Decide before build; email link is the standard pattern. External-provider (OIDC) pending users have no password and authenticate via their provider after approval. |
| R4 | **First-user-as-owner vs approval.** If registration requires approval, the *first* registrant is `pending` until approved. On approval they become Owner. Until then the tenant has no active user — who approves them? | The tenant creator / an existing Owner, or a Gateway-Token/master-scope operator, approves the first pending user. Document that a brand-new tenant with `registration_requires_approval=true` must have at least one bootstrap approver (the Owner who enabled the toggle). Edge case for new tenants; note in operator docs. |
| R5 | **Latent catalog bug.** `users.go:40-41` references `user.enroll`/`user.unenroll` permissions absent from `internal/permissions/catalog.go`. The approve flow enrolls; if it routes through those guards it will be un-grantable. | Route approval enrollment through `user.pre_provision` (present in `AdminSeedPermissions`) + `group.manage_members` (present). Do **not** rely on `user.enroll`. File the catalog gap as a separate cleanup (add `PermUserEnroll`/`PermUserUnenroll` or remove the routes). |
| R6 | **Dual-DB migration ordering.** Missing the SQLite side crashes the desktop edition on startup (CLAUDE.md). | Both sides shipped in the same change; `go build -tags sqliteonly ./...` in the checklist; bump `SchemaVersion` + `RequiredSchemaVersion` together. |
| R7 | Should a rejected (`deactivated`) user be re-invitable (same email)? | Yes — `deactivated` is not deleted; a new invite to the same email can flip it back to `pending`. Document the transition `deactivated → pending` via re-invite. |

## 7. Implementation Plan

1. **Schema (dual-DB):** PG migration `000089` (status CHECK + `pending_group_id`), bump `RequiredSchemaVersion` 88→89; SQLite `schema.sql` + `schema.go` patch + `SchemaVersion` bump. Add `store.UserStatusPending`. (FR-00)
2. **Store:** approve/reject orchestration method (transactional: status + `TenantStore.AddUser` + `GroupStore.AddMember` + `RoleStore.AssignUserRole` + clear `pending_group_id`); reject = status only. (FR-03/FR-04)
3. **Auth:** block `pending` in `handleLogin` (local) + external callback; return `account_pending`. (FR-06)
4. **HTTP — invite/approve/reject:** new routes in `internal/http/users.go` with FR-05 authorization (tenant-admin OR group-admin of `pending_group_id`). Extend `GET /v1/users?status=pending` group-scoping. Keep `handleStatusChange` switch locked (no manual `pending`). (FR-02/FR-03/FR-04/FR-05/FR-07)
5. **Registration toggle:** read `auth.registration.requires_approval` in `handleRegister`; create `pending` when on. (FR-01)
6. **Frontend:** pending-users tab + approve/reject/invite UI + hooks (`useMutation`/`HttpClient`) + affordance rules. (FR-08)
7. **i18n + audit:** keys in 3 locale dirs + backend catalogs; emit `user.invited/approved/rejected` + `cache_invalidate`. (FR-09/FR-10)
8. **Tests:** handler/store/txn/frontend tests per §5.
9. **Checklist:** `go build ./...`, `go build -tags sqliteonly ./...`, `go vet ./...`, `go test -race ./tests/integration/`, `pnpm build` in `ui/web`.

## 8. Proposed Error Codes

| Code | Meaning |
|------|---------|
| `user.not_pending` | Approve/reject called on a user whose status is not `pending` (existing conflict path, 409). |
| `user.already_exists` | Invite email matches an existing active/suspended/pending user (existing duplicate-email path, 409). |
| `account_pending` | Login/refresh by a `pending` user — not yet approved (new 403, distinct from `account_suspended`). |
| `user.target_role_forbidden` | Invite with `role='owner'`, or role override not permitted for caller (reuse the `001`/`006` `MsgTargetRoleForbidden` 403 path). |
| `group.forbidden` | Group admin acting on a group they do not administer (existing group-scope 403). |
| `user.permission_denied` | Caller is neither tenant admin nor group admin of the target (existing 403). |
| `request.validation_failed` | Malformed invite body / invalid group UUID / unknown role slug (existing `ErrInvalidRequest` 400). |
