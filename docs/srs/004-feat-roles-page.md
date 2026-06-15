# Software Requirements Specification: Roles Page — Default System Roles Visibility

**Project**: GoClaw Gateway
**Release**: 2026.3.0
**Version**: 0.2-draft
**Date**: 2026-06-16
**Status**: Draft
**Difficulty**: Low
**Estimate**: 0.5 days

---

## Revision History

| Version | Date | Changes |
|---------|------|---------|
| 0.1-draft | 2026-06-16 | Initial draft. Hypothesized two root causes: unseeded master tenant + frontend list-response contract mismatch. Proposed runtime backfill + editable system-role permissions. |
| 0.2-draft | 2026-06-16 | Corrected after code verification. Migration `000083` already seeds Admin/Member/Viewer for every tenant incl. master (`ON CONFLICT DO NOTHING`, schema version 88 = applied), so data is present — runtime backfill is out of scope. The empty page root cause is the frontend contract mismatch (FR-01). Decisions locked: minimal fix, system roles fully read-only, leave existing seeded permissions as-is (catalog drift deferred). |
| 0.3-draft | 2026-06-16 | Folded in the i18n namespace-mismatch defect (formerly `005-bugfix-roles-i18n-namespace-mismatch.md`). Added **FR-07**: Roles UI renders raw keys because `useTranslation("role-management")` (kebab) does not match the registered `roleManagement` (camel) namespace. Fix = 2 call-site changes in `role-management-page.tsx` + `role-permission-editor.tsx`. |

---

## 1. Summary

This SRS defines the requirements to make the **Roles management page** (`/t/{tenant}/admin/roles`) render the three default system roles — **Admin, Member, Viewer** — for users who hold an eligible role. The page renders empty today. Verified root cause: the React query hook reads `data?.items` (`ui/web/src/pages/role-management/hooks/use-roles.ts:32`) while the HTTP handler returns `{ "roles": [...] }` (`internal/http/roles.go:87`), so `items` is always `undefined`. The role data itself already exists — migration `000083_user_group_role_refactor.up.sql` seeds the three system roles for every tenant (looping `SELECT id FROM tenants`, `ON CONFLICT (tenant_id, name) DO NOTHING`) and `permissions.SeedSystemRoles` runs on each new tenant create.

This SRS owns: the contract fix, the page-rendering requirements, the visibility policy, and the **fully read-only** system-role policy. The RBAC authorization model and role-seed definition are specified in `002-feat-multi-auth-module-srs.md` (RBAC) and `internal/permissions/seed_roles.go`.

## 2. Scope

**In scope**:

- Aligning the `GET /v1/roles` response shape between the HTTP handler and the React query hook so seeded roles actually render (the actual fix for the empty page).
- Rendering the three default system roles on the Roles page with name, description, permission count, and a system badge.
- Defining which users ("eligible roles") may view and navigate to the Roles page.
- Defining the **fully read-only** mutability policy for system roles (name, description, permissions, and deletion all locked).
- Fixing the i18n namespace-resolution defect that renders raw keys (`editRole`, `editRoleDescription`, …) in the Roles UI (see FR-07).

**Out of scope**:

- Runtime system-role backfill. Migration `000083` already backfills all tenants incl. master; `SeedSystemRoles` runs on each new tenant create. No startup backfill or `CreateRole` idempotency change is required (see §6 decision log).
- Resyncing existing seeded permission sets to the Go `permissions.AdminSeedPermissions` / `MemberSeedPermissions` / `ViewerSeedPermissions` catalogs. The migration-seeded lists drift from the Go catalog (deferred — separate cleanup).
- Creating new custom roles or the create/edit/delete dialogs (already implemented in `ui/web/src/pages/role-management/role-management-page.tsx`).
- User → role and group → role assignment flows (covered by `002-feat-multi-auth-module-srs.md` §4.4 and the `/v1/users/{id}/roles`, `/v1/groups/{id}/roles` endpoints).
- The RBAC permission catalog itself (`internal/permissions/catalog.go`) — this SRS consumes it, does not change it.

## 3. Functional Requirements

### FR-00: Data Presence (no code change — verification only)

The three default system roles (Admin, Member, Viewer) are guaranteed present in every tenant, including master, by two existing mechanisms: migration `000083` (seeds all tenants at migration time) and `permissions.SeedSystemRoles` (runs on each new tenant create). No runtime backfill code is added.

Acceptance criteria:

- [ ] Verify against the running master DB that `SELECT name, is_system FROM roles WHERE tenant_id = <master>` returns Admin, Member, Viewer with `is_system = true` (Viewer may also exist with/without a removed Operator row from `000084` — both states are acceptable).
- [ ] No new backfill code, startup hook, or `CreateRole` idempotency change is introduced for this feature.

---

### FR-01: List-Response Contract Alignment

The `GET /v1/roles` handler and the React query hook must agree on the response shape. Today the handler returns `{ "roles": [...], "total", "offset", "limit" }` (`internal/http/roles.go:87`) but the hook reads `data?.items` (`ui/web/src/pages/role-management/hooks/use-roles.ts:32`), so `items` is always `undefined` and the rendered list is always empty regardless of backend data. The sibling tenant-users hook reads the matching `res?.users` key (`ui/web/src/pages/tenants-admin/hooks/use-tenant-detail.ts:47`) — the roles hook must follow the same `{ <plural> }` convention.

Acceptance criteria:

- [ ] The hook reads `data?.roles` + `data?.total` (the handler keeps returning `roles`, matching the `{ "users": [...] }` convention in `01-feat-tenant-user-crud.md` §4.3). No backend shape change.
- [ ] After alignment, the hook returns the backend `total` and the rendered row count matches the number of roles the tenant actually has.
- [ ] No other caller of `GET /v1/roles` is broken (grep confirms `use-roles.ts` is the only consumer of the list response body).

---

### FR-02: Roles Page Renders the Three Default System Roles

When an eligible user opens `/t/{tenant}/admin/roles`, the page must list the three default system roles.

| Default role | Seed description | Seed source |
|--------------|------------------|-------------|
| `Admin` | "Full tenant administration" | `permissions.AdminSeedPermissions` (`catalog.go:71`) |
| `Member` | "Regular member" | `permissions.MemberSeedPermissions` (`seed_roles.go:14`) |
| `Viewer` | "Read-only access" | `permissions.ViewerSeedPermissions` (`seed_roles.go:23`) |

Each row must show: role name, description (or `—` when absent), permission count as a badge, and a "system" badge (`is_system = true`). The create/edit/delete UI already exists (`role-management-page.tsx`).

Acceptance criteria:

- [ ] Opening `/t/master/admin/roles` lists three rows: Admin, Member, Viewer (plus any additional seeded/system rows the tenant has).
- [ ] Each system role row shows the "system" badge; the permission-count badge shows the number of permissions persisted for that role.
- [ ] The page shows the total count via the `total` field.
- [ ] The empty state (`EmptyState` with `ShieldCheck`) only appears when the tenant genuinely has zero roles, not when roles exist but fail to render.
- [ ] Search filters rows by name as before.

---

### FR-03: Page Visibility for Eligible Roles

The Roles page and its data endpoint must be reachable only by users who hold an eligible role ("eligible roles"). Eligible roles are: **Owner** (always, bypasses RBAC) and **Admin** (the system role that carries `role.list`, see `AdminSeedPermissions` at `catalog.go:77`). Member and Viewer are not eligible.

| Caller | Route guard | Can view page | `GET /v1/roles` returns |
|--------|-------------|:-------------:|:-----------------------:|
| Owner | `RequireAdmin` admits (role level 4 ≥ 3) | ✅ | Roles for caller's tenant |
| Admin | `RequireAdmin` passes (`hasMinRole ≥ admin`, `require-role.tsx`) | ✅ | Roles for caller's tenant |
| Member / Viewer | `RequireAdmin` redirects to overview | ❌ | n/a (blocked client-side); 403 server-side without `role.list` |

Acceptance criteria:

- [ ] Frontend route `admin/roles` is wrapped in `RequireAdmin` (`ui/web/src/routes.tsx:241`) — verified already in place; Member/Viewer are redirected to overview.
- [ ] Backend `GET /v1/roles` is gated by `requireAuthAction("role.list", ...)` (`internal/http/roles.go:32`); callers without `role.list` receive 403.
- [ ] An Owner (who bypasses RBAC) sees the page and the three roles.
- [ ] An Admin (seed role carries `role.list`) sees the page and the three roles.
- [ ] A Member or Viewer hitting the endpoint directly receives 403 and never sees the roles.
- [ ] The Roles nav entry is hidden from Member/Viewer (existing sidebar access matrix in `internal/permissions/sidebar_access_test.go`).

---

### FR-04: System Role Mutability Policy — Fully Read-Only

The three default system roles are **fully protected**: their name, description, permission set cannot be changed, and they cannot be deleted. This diverges from the current backend, which allows permission editing via `PUT /v1/roles/{id}/permissions` — that endpoint must reject system roles.

| Field / action | System role | Custom role |
|----------------|:-----------:|:-----------:|
| Rename (`PATCH` name/desc) | ❌ 403 "cannot modify system role" (`roles.go:193`, existing) | ✅ |
| Delete (`DELETE`) | ❌ 403 "cannot delete system role" (`roles.go:248`, existing) | ✅ |
| Edit permission set (`PUT /permissions`) | ❌ 403 "cannot modify system role" (NEW — must be added) | ✅ |

Acceptance criteria:

- [ ] System role rows hide the delete button (already: `role-management-page.tsx:135` guards `!role.is_system`).
- [ ] Opening edit on a system role disables the name/description inputs (read-only) and shows the permission set read-only (no save).
- [ ] `PATCH /v1/roles/{id}` on a system role returns 403; `DELETE` returns 403 (already enforced).
- [ ] `PUT /v1/roles/{id}/permissions` on a system role returns 403 "cannot modify system role" (NEW enforcement in `roles.go` `handleSetPermissions`).
- [ ] `PUT /v1/roles/{id}/permissions` on a custom role still succeeds and invalidates the permission cache (`pkgPermCache.InvalidateAll()`).

---

### FR-05: Permission Editor — Read-Only for System Roles

The edit dialog must surface the full permission catalog (`permissions.AllPermissions()`, `catalog.go:91`) grouped by domain (user, group, role, audit, system, artifact/agent). For system roles the editor is **read-only** (FR-04): it displays the current permission set but offers no save action. For custom roles it remains fully editable.

Acceptance criteria:

- [ ] `RolePermissionEditor` lists all permissions from `AllPermissions()` grouped by domain prefix.
- [ ] For a system role, the editor renders checkboxes/toggles in a disabled (read-only) state and hides/disables the Save button.
- [ ] For a custom role, saving applies via `PUT /v1/roles/{id}/permissions`; unknown permissions are rejected 400 by the handler (`roles.go:327`).
- [ ] After saving a custom role, the permission-count badge on the row updates to reflect the new set.

---

### FR-06: i18n (en / vi / zh)

The `role-management` namespace exists in all three locales (`ui/web/src/i18n/locales/{en,vi,zh}/role-management.json`). Any new string introduced by this feature (e.g. a "system role — read only" hint in the edit dialog) must be added to all three locale files per the Mobile/UI i18n rule.

Acceptance criteria:

- [ ] Any new UI string is added to `role-management.json` in `en`, `vi`, and `zh`.
- [ ] The three locale files have identical key sets (no missing keys that would cause runtime fallback).

---

### FR-07: i18n Namespace Alignment (defect fix)

The Roles UI currently renders **raw translation keys** (`editRole`, `editRoleDescription`, `permissions`, `savePermissions`, `systemRoleReadonly`) instead of localized strings — in the `/admin/roles` edit view and the tenant-detail Roles "Edit Permissions" dialog.

**Root cause — namespace name mismatch.** The `roleManagement` namespace is registered **camelCase** in `ui/web/src/i18n/index.ts` (`ns` array at `:172`; resource keys en/vi/zh at `:201`/`:227`/`:253`), but two components call `useTranslation("role-management")` (kebab-case). i18next finds no `role-management` namespace and returns the raw key:

| Site | Namespace | Status |
|------|-----------|:------:|
| `role-management-page.tsx:28` | `"role-management"` | ❌ |
| `role-permission-editor.tsx:65` | `"role-management"` | ❌ |
| `tenant-roles-tab.tsx:24` | `roleManagement` | ✅ (but embeds the ❌ editor at `:21`) |

The locale JSON files are correct and complete — only the lookup fails. `RolePermissionEditor` is shared, so its wrong namespace leaks into both `/admin/roles` (page edit dialog) and `/admin/tenants/{id}` Roles tab (permissions dialog).

Acceptance criteria:

- [x] Both broken call sites use `roleManagement`: `role-management-page.tsx:28` and `role-permission-editor.tsx:65`.
- [x] `grep -rn 'useTranslation("role-management")' ui/web/src` returns 0 matches.
- [ ] `/admin/roles` edit view shows localized strings (e.g. "Edit Role", "Update role details and permissions", "Permissions", "Save Permissions") — no raw keys — in `en`, `vi`, `zh`. _(manual — pending browser check)_
- [ ] Tenant-detail Roles "Edit Permissions" dialog shows a localized header/buttons (no raw keys). _(manual — pending browser check)_
- [ ] `systemRoleReadonly` hint renders localized, not as a raw key. _(manual — pending browser check)_
- [x] `cd ui/web && pnpm build` succeeds.

**Prevention:** enforce one namespace form (camelCase is the current majority) — the `useTranslation()` namespace argument MUST match the registered namespace key in `ui/web/src/i18n/index.ts` exactly (case-sensitive). Add a grep gate in CI.

## 4. System Impact

- **Backend HTTP:** `internal/http/roles.go` `handleSetPermissions` must reject system roles with 403 before applying (load the role, check `IsSystem`). No other backend change; the list response shape stays `{ "roles", "total", "offset", "limit" }`.
- **Frontend hook:** `ui/web/src/pages/role-management/hooks/use-roles.ts` reads `data?.roles` / `data?.total` instead of `data?.items` (the actual empty-page fix).
- **Frontend page:** edit dialog disables name/description inputs for `is_system` roles (FR-04); the `RolePermissionEditor` renders read-only for `is_system` roles and hides Save (FR-05).
- **i18n:** optional new key(s) for the read-only hint, added to all 3 locales. **Plus the FR-07 defect fix:** change `useTranslation("role-management")` → `useTranslation("roleManagement")` in `role-management-page.tsx:28` and `role-permission-editor.tsx:65` (2 one-line changes).
- **No schema migration**, no startup hook, no store change.

## 5. Test Plan

- Integration test: `GET /v1/roles` for master tenant returns Admin/Member/Viewer with `is_system = true` (confirms FR-00 data presence).
- Handler test: Owner and Admin receive the list (200); Member/Viewer without `role.list` receive 403.
- Handler test: `PATCH`/`DELETE` on a system role → 403 (existing); `PUT /permissions` on a system role → 403 (NEW).
- Handler test: `PUT /permissions` on a custom role → 200 and cache invalidated.
- Frontend test: `use-roles` returns the seeded rows when the handler returns `{ roles: [...], total }`; row count + total render correctly.
- Frontend test: editing a system role shows read-only name/desc and read-only permission set (no Save).
- Manual: open `/t/master/admin/roles` as Owner → three roles visible; as Admin → three roles visible; as Member → redirected.
- i18n (FR-07): `grep -rn 'useTranslation("role-management")' ui/web/src` → 0 matches after fix.
- i18n (FR-07): `/admin/roles` edit view + tenant Roles "Edit Permissions" dialog show localized strings (not raw keys) in `en`/`vi`/`zh`.

## 6. Decision Log (locked)

| Decision | Rationale |
|----------|-----------|
| **Minimal fix, no runtime backfill.** | Migration `000083` already seeds Admin/Member/Viewer for every tenant incl. master (`FOR t_rec IN SELECT id FROM tenants`, `ON CONFLICT DO NOTHING`), and `RequiredSchemaVersion = 88` confirms it is applied. So role data is already present; the empty page is the frontend contract bug (FR-01). A startup backfill + `CreateRole` idempotency change would be redundant. |
| **System roles fully read-only.** | Lock name, description, permissions, and deletion for system roles to preserve seed integrity. This requires one new backend check (deny `PUT /permissions` on system roles) and read-only rendering in the edit dialog. |
| **Leave existing seeded permissions as-is.** | The migration-seeded permission lists drift from the Go `permissions.*SeedPermissions` catalogs (e.g. `000083` Admin has `user.enroll`/`user.unenroll`; the Go catalog has `user.pre_provision`/`user.suspend`/`user.deactivate`). Resyncing would change effective permissions on live tenants and is deferred to a separate cleanup task. |

## 7. Risks and Open Questions

| Risk or question | Draft decision |
|------------------|----------------|
| Confirm the running master DB actually has the seeded roles (not just that 000083 ran). | Verify via `SELECT name, is_system FROM roles WHERE tenant_id = <master>` before declaring done. If absent (e.g. DB migrated before 000083 existed and never re-migrated), fall back to a one-shot manual seed, not a runtime hook. |
| `PUT /permissions` 403 for system roles may surprise admins currently able to tune them. | Acceptable — the decision is fully read-only. UI shows read-only state to make it clear. |
| Permission-count badge reflects persisted permissions, which may differ from the Go catalog (drift). | Acceptable for this feature; documented as deferred resync. |

## 8. Implementation Plan

1. **Verify data presence:** query the running master DB for the three system roles (FR-00). If present, proceed minimal.
2. **Frontend contract fix:** `use-roles.ts` read `data?.roles` / `data?.total` (FR-01).
3. **Backend read-only enforcement:** in `internal/http/roles.go` `handleSetPermissions`, load the role and return 403 when `IsSystem` (FR-04).
4. **Frontend edit dialog:** disable name/description inputs and render `RolePermissionEditor` read-only (hide Save) for `is_system` roles (FR-04/FR-05).
5. **i18n:** add any new read-only hint key to `en`/`vi`/`zh` `role-management.json` (FR-06). **Plus FR-07 defect fix:** change `useTranslation("role-management")` → `useTranslation("roleManagement")` in `role-management-page.tsx` + `role-permission-editor.tsx`.
6. **Tests:** handler test (system role `PUT /permissions` → 403) + frontend test (read-only rendering, `use-roles` contract).
7. **Manual verification:** open `/t/master/admin/roles` as Owner and Admin; confirm three roles render; confirm editing a system role is read-only; confirm Member is blocked.
8. **Checklist:** `go build ./...`, `go build -tags sqliteonly ./...`, `go vet ./...`, and `pnpm build` in `ui/web`.

## 9. Proposed Error Codes

| Code | Meaning |
|------|---------|
| `role.not_found` | Role ID does not exist or is outside the caller's tenant scope (existing `ErrNotFound` path). |
| `role.system_immutable` | Attempt to rename, delete, or change permissions of a system role (rename/delete existing 403 at `roles.go:193`/`:248`; permission-change 403 is NEW in `handleSetPermissions`). |
| `role.in_use` | Attempt to delete a role still assigned to users or groups (existing conflict path, `roles.go:273`). |
| `role.unknown_permission` | `PUT /permissions` body contains a permission not in `AllPermissions()` (existing `ErrInvalidRequest` 400, `roles.go:327`). |
