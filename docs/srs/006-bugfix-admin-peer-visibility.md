# Software Requirements Specification: Tenant Admins See Peer Admins (Owner Stays Hidden)

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
| 0.1-draft | 2026-06-16 | Initial draft. Policy change: relax the tenant-admin visibility filter so an admin can see peer admins in the same tenant. Owner remains hidden (higher privilege). Visibility-only change; mutation authorization unchanged. Updates the policy canonically owned by `001-feat-tenant-user-crud.md` §5.4. |
| 0.2-draft | 2026-06-16 | **Implemented.** `enrichTenantUsers` hides Owner only; `checkTenantUserAuth` splits `read` (peer-admin allowed, Owner denied) from `update`/`delete` (Owner+Admin block unchanged); `update_role` + `handleUsersAdd` untouched (mutation auth unchanged). Frontend already gated peer-admin actions (`tenant-users-tab.tsx` `canEdit`/`canDelete`/`canChangeRole`/`canChangePassword`). `go build` (PG+SQLite) + `go vet` + `pnpm build` green; 2 handler unit tests pass (`internal/http/tenants_admin_visibility_test.go`). FR-01/03/04 boxes marked `[x]`; FR-02 (single-user read) left `[ ]` — code-complete + build-verified, pending a unit test (`checkTenantUserAuth` admin branch needs `pkgPermCache` wiring) and live confirmation. |

---

## 1. Summary

A tenant **admin** viewing the tenant user list today cannot see other admins in the same tenant — the admin visibility filter hides both the tenant **Owner** and peer **Admins** (`internal/http/tenants.go:1013`). This was over-broad: the filter's stated intent (001 SRS §5.4) is to hide **higher-privileged** users from an admin, and a peer admin is not higher than the caller. This SRS relaxes the filter so an admin can **see** peer admins; the tenant **Owner** (`is_owner = true`, bypasses RBAC) remains hidden because it is the one role strictly above admin.

This is a **visibility-only** change: it affects the list (`GET /v1/tenants/{id}/users`) and single-user read (`GET /v1/tenants/{id}/users/{userId}`). It does **not** change mutation authorization — an admin still cannot create, update, delete, or role-change an admin or the owner (001 SRS AC-9/AC-10/AC-11, enforced at `internal/http/tenants.go:339, 594-609`). The result: an admin-visible admin row must render with management actions disabled/hidden (mutation is still 403).

This SRS owns the policy change and the exact code edits. It updates the canonical policy defined in `001-feat-tenant-user-crud.md` §5.4 (whose filter language is reconciled in §3 FR-04). The earlier `005-bugfix-roles-list-master-tenant-fallback.md` FR-00 noted the user-count asymmetry as "expected"; the admin-hiding-admins portion is now this defect (owner-hiding remains expected) — `005` FR-00 is updated to cross-reference here.

## 2. Scope

**In scope**:

- Relaxing the tenant-admin visibility filter so an admin sees peer admins in the same tenant (list + single-user read).
- Keeping the tenant Owner hidden from admins.
- Keeping mutation authorization (create / update profile / delete / change role) unchanged: an admin still cannot mutate an admin or owner.
- Documenting the UI consequence: admin-visible peer-admin rows show no enabled management actions.

**Out of scope**:

- The roles 3-vs-4 list defect — owned by `005-bugfix-roles-list-master-tenant-fallback.md`.
- Changing admin mutation powers over admins/owners (create/update/delete/role-change). Deliberately unchanged.
- Cross-tenant visibility. An admin only ever sees users in their own tenant (enforced by the `{id}` path + tenant scope). No change.
- The owner/system view of users (WS `tenants.users.list`, owner bypass, no filter). Unchanged.

## 3. Functional Requirements

### FR-01: Tenant Admin Sees Peer Admins in the User List

The `GET /v1/tenants/{id}/users` admin visibility filter (`enrichTenantUsers`, `internal/http/tenants.go:1013`) must hide only the tenant **Owner**, not peer **Admins**.

| Caller | Hidden from list | Visible |
|--------|------------------|---------|
| Owner / Gateway Token | none | all users |
| Tenant Admin (today) | Owner **and** Admin | Member, Viewer |
| Tenant Admin (after) | **Owner only** | **Admin**, Member, Viewer |

Today (`tenants.go:1013`):
```go
if isAdminCaller && (role == store.TenantRoleOwner || role == store.TenantRoleAdmin) {
    continue
}
```
After:
```go
if isAdminCaller && role == store.TenantRoleOwner {
    continue
}
```

Acceptance criteria:

- [x] A tenant admin's `GET /v1/tenants/{id}/users` response includes peer admin users (role = `admin`). _(`TestEnrichTenantUsers_AdminCallerSeesPeerAdmins_OwnerHidden`)_
- [x] The tenant Owner (`is_owner = true`) is still absent from an admin's list response. _(same test)_
- [x] An owner's list response is unchanged (all users). _(`TestEnrichTenantUsers_OwnerCallerSeesAll`)_
- [x] The admin caller's own row is still present (no self-removal). _(filter skips Owner-role only; the admin's own row is never removed)_

---

### FR-02: Tenant Admin Can Read a Peer Admin's Detail

Single-user read (`GET /v1/tenants/{id}/users/{userId}` via `handleUsersGet` → `checkTenantUserAuth(..., "read")`, `tenants.go:594-600`) must allow an admin to read a peer admin, while still denying read of the Owner.

Today (`tenants.go:594-600`) the `read`, `update`, and `delete` operations share one branch that blocks both Owner and Admin:
```go
case "read", "update", "delete":
    targetRole := h.resolveTargetRole(ctx, tenantID, targetUserID)
    if targetRole == store.TenantRoleOwner || targetRole == store.TenantRoleAdmin {
        writeJSON(w, http.StatusForbidden, ...)
        return ""
    }
    return targetRole
```
After — split `read` (Owner-only block) from `update`/`delete` (unchanged Owner-or-Admin block):
```go
case "read":
    targetRole := h.resolveTargetRole(ctx, tenantID, targetUserID)
    if targetRole == store.TenantRoleOwner {
        writeJSON(w, http.StatusForbidden, ...) // i18n.MsgTargetRoleForbidden
        return ""
    }
    return targetRole
case "update", "delete":
    targetRole := h.resolveTargetRole(ctx, tenantID, targetUserID)
    if targetRole == store.TenantRoleOwner || targetRole == store.TenantRoleAdmin {
        writeJSON(w, http.StatusForbidden, ...)
        return ""
    }
    return targetRole
```

Acceptance criteria:

- [ ] An admin `GET /v1/tenants/{id}/users/{adminUserId}` returns 200 with the peer admin's enriched record. _(code-complete + build-verified; pending unit test — `checkTenantUserAuth` admin branch needs `pkgPermCache` wiring — and live confirmation)_
- [ ] An admin `GET .../users/{ownerUserId}` returns 403 (`MsgTargetRoleForbidden`). _(same — pending test + live)_
- [ ] An admin `GET .../users/{memberOrViewerUserId}` returns 200 (unchanged). _(same — pending test + live)_

---

### FR-03: Mutation Authorization Is Unchanged

Creating, updating, deleting, or role-changing an admin (or owner) by an admin caller remains forbidden. This preserves the "admin cannot manage admins" boundary (001 SRS AC-9/AC-10/AC-11).

| Operation by admin | Target Owner | Target Admin | Target Member/Viewer |
|--------------------|:---:|:---:|:---:|
| Read (FR-02) | 403 | **200** (changed) | 200 |
| Create (role=admin/owner) | 403 | 403 (`tenants.go:339`) | 200 |
| Update profile | 403 | **403** (unchanged) | 200 |
| Delete | 403 | **403** (unchanged) | 200 |
| Change role | 403 | **403** (unchanged) | 200 |

Acceptance criteria:

- [x] `update`, `delete`, and `update_role` branches in `checkTenantUserAuth` (`tenants.go:594-609`) still block an admin from Owner and Admin targets (403). _(verified by inspection: `update`/`delete` retain the Owner+Admin block; `update_role` untouched)_
- [x] `handleUsersAdd` admin restriction (`tenants.go:339`) still rejects role=admin/owner for an admin caller (403). _(untouched)_
- [x] No new mutation capability is granted to admins. _(only `read` was relaxed)_

---

### FR-04: UI Consequence — Peer-Admin Rows Render Without Enabled Management Actions

Because visibility is relaxed but mutation is not, an admin viewing a peer-admin row must not be offered enabled edit/delete/role-change controls (they would 403 on submit). This is a UI consistency requirement, not a new authorization rule.

Acceptance criteria:

- [x] For an admin caller, peer-admin user rows show profile data but disable/hide the edit (display_name/phone), delete, and role-change controls — the same disabled state already used for owner rows (which remain hidden, so this applies to the now-visible admin rows). _(pre-existing `canEdit`/`canDelete`/`canChangeRole`/`canChangePassword` in `tenant-users-tab.tsx` already return false for admin targets when `callerIsAdmin`)_
- [x] No UI control offered to an admin results in a 403 on normal use. _(the `canX` gates suppress blocked-action buttons before render)_
- [x] i18n: no new strings required; reuse existing "insufficient permission" / disabled affordances. Any added string goes into all 3 locales (en/vi/zh). _(N/A — no strings added)_

## 4. System Impact

- **Backend** `internal/http/tenants.go`:
  - `enrichTenantUsers` (`:1013`): change the admin skip condition to Owner-only (FR-01).
  - `checkTenantUserAuth` (`:594-600`): split `read` (Owner-only block) from `update`/`delete` (Owner-or-Admin block). `update_role` (`:601-608`) unchanged (FR-02/FR-03).
- **Frontend** `ui/web/src/pages/tenants-admin/` user-card / role-dropdown: disable edit/delete/role-change on peer-admin rows for admin callers (FR-04). Verify the existing admin-vs-owner affordance logic; extend to admin targets.
- **No schema migration**, no store change, no new error codes.

## 5. Test Plan

- Handler test: admin `GET /v1/tenants/{id}/users` includes admin-role users, excludes owner (FR-01).
- Handler test: admin `GET .../users/{adminUserId}` → 200; `.../{ownerUserId}` → 403; `.../{memberUserId}` → 200 (FR-02).
- Handler test (regression): admin `PUT .../users/{adminUserId}` (profile) → 403; `DELETE .../users/{adminUserId}` → 403; `PUT .../users/{adminUserId}/role` → 403; admin create role=admin → 403 (FR-03).
- Handler test (regression): owner list/get unchanged (all users, 200).
- Frontend test: for an admin caller, a peer-admin row renders with mutation controls disabled; a member/viewer row renders editable.
- Manual: tenant-17 admin opens user list → sees peer admins + members/viewers, not owner; opening a peer admin shows read-only detail; attempting edit/delete is blocked.

## 6. Risks and Open Questions

| Risk or question | Draft decision |
|------------------|----------------|
| Admins seeing peer admins may expose admin email/display-name that was previously hidden. | Acceptable — these are peer-tenant admins, same privilege tier. Owner remains private. |
| Should the single-user `read` also stay blocked for admin to preserve symmetry with mutation? | No — visibility (read) is deliberately relaxed; mutation is not. A read-only peer-admin detail is consistent with the list. |
| Frontend role-dropdown per row: an admin-visible peer-admin row must not offer role-change. | Disable the dropdown / action buttons for admin targets on admin callers (FR-04). Verify existing owner-row handling and mirror it. |
| Does relaxing read leak admin data via search/autocomplete endpoints (`/v1/tenant-users`, `user_search`)? | Verify those paths do not already apply the admin filter (they appear to be admin-gated, not admin-filtered). If they do filter, align them in the same change; otherwise no action. |

## 7. Implementation Plan

1. **Backend list filter:** `enrichTenantUsers` (`tenants.go:1013`) → Owner-only skip (FR-01).
2. **Backend read auth:** `checkTenantUserAuth` (`tenants.go:594-600`) → split `read` (Owner-only) from `update`/`delete` (Owner-or-Admin, unchanged); leave `update_role` (`:601-608`) unchanged (FR-02/FR-03).
3. **Frontend affordances:** disable edit/delete/role-change on peer-admin rows for admin callers in the tenants-admin user card (FR-04).
4. **Tests:** handler tests (FR-01/02/03) + frontend test (FR-04) per §5.
5. **Checklist:** `go build ./...`, `go build -tags sqliteonly ./...`, `go vet ./...`, and `pnpm build` in `ui/web`.
6. **Doc reconcile:** confirm `001-feat-tenant-user-crud.md` §5.4 / FR-TU2 / §4.3 / §4.4 / ACs match the new policy (updated in this same change set); confirm `005` FR-00 cross-references here.

## 8. Proposed Error Codes

| Code | Meaning |
|------|---------|
| `tenant.user.target_role_forbidden` | Admin attempted a **mutation** (create/update/delete/role-change) on an Owner or Admin target, or any read of an Owner (existing `MsgTargetRoleForbidden` 403 path, `tenants.go:597,605`). Reused unchanged; the only change is that **read of an Admin** no longer reaches it. |
| `tenant.user.permission_denied` | Caller is neither owner nor admin (existing 403, `tenants.go:612`). Unchanged. |
