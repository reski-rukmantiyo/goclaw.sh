# Software Requirements Specification: Roles List Resolves to Owner's Home Tenant (Master Fallback) for Cross-Tenant Owners

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
| 0.1-draft | 2026-06-16 | Initial draft. Root cause traced + verified against code (`internal/http/roles.go`, `internal/http/auth.go`, `internal/store/pg/roles.go`, WS `tenants.go`). Separates the two observed anomalies: roles 3-vs-4 (this bug) vs users 5-vs-2 (expected admin visibility filter, out of scope). |

---

## 1. Summary

A master-scope / system owner viewing **tenant-17** sees **3 roles**, while a tenant-17 admin sees **4 roles** (3 system + 1 custom). The owner is missing the tenant-17 custom role. This SRS defines the fix for that defect.

**Verified root cause.** The roles list endpoint `GET /v1/roles` (`internal/http/roles.go:47`) carries **no tenant identifier in its path or query** (unlike `GET /v1/tenants/{id}/users` and the WS `tenants.users.list` method, which both take an explicit tenant). For a master-scope caller, the only tenant override is the optional `?tenant_id=` query param (`roles.go:62-69`), and the frontend never sends it (`ui/web/src/pages/role-management/hooks/use-roles.ts:28-37` sends only `search`/`limit`/`offset`). So the tenant is resolved from the ambient `X-GoClaw-Tenant-Id` header (`internal/http/auth.go:196-201` → `resolveScopedTenant` → `MasterTenantID` fallback), which reflects the **caller's active/home tenant**, not the tenant being viewed. A cross-tenant owner operating from the master-scoped tenants-admin console has an active tenant of **master**, so `ListRoles(ctx, MasterTenantID, …)` (`internal/store/pg/roles.go:104`) returns the **master tenant's** 3 seeded system roles instead of tenant-17's 4 roles. Tenant-17's custom role (persisted under `tenant_id = <tenant-17>`) is invisible to the owner.

The sibling anomaly in the report — **users 5 (owner) vs 2 (admin)** — is **NOT a bug** and is out of scope (§2, §3 FR-00): the owner path is the WS `tenants.users.list` method (`tenants.go:345`) which takes an explicit `tenant_id` payload and applies no visibility filter (5 users); the admin path is `GET /v1/tenants/{id}/users` which applies the documented admin visibility filter that hides owner + admin users (001-feat-tenant-user-crud.md §5.4 / §4.3). That asymmetry is correct and expected.

This SRS owns: making the roles list tenant-explicit so a cross-tenant owner sees the **viewed** tenant's roles. It composes with `004-feat-roles-page.md` (which fixed the list-response contract and system-role read-only policy) and `001-feat-tenant-user-crud.md` (whose `/v1/tenants/{id}/users` pattern this fix mirrors).

## 2. Scope

**In scope**:

- Making the roles list resolve to the **explicitly-viewed tenant** rather than the caller's ambient active tenant, so a cross-tenant owner sees the correct tenant's roles (incl. custom roles).
- Aligning the roles list with the tenant-explicit pattern already used by the users list (explicit tenant in the request, not ambient header).
- The Roles tab of the tenant-detail page (`tenant-roles-tab.tsx`) and the standalone Roles management page (`role-management-page.tsx`), both of which call `useRoles()` today.

**Out of scope**:

- The users 5-vs-2 asymmetry. It is the expected admin visibility filter (001 SRS §5.4). Documented in §3 FR-00 as a non-defect for traceability; no code change.
- The RBAC permission catalog and system-role read-only policy (owned by `004-feat-roles-page.md`).
- Role create/update/delete/assign endpoints (`POST/PATCH/DELETE /v1/roles`, `*/roles` under users/groups). They resolve the role by `{id}` and already enforce tenant scope for non-master callers (`roles.go:154-160, 198-203, 253-258`). They are not the defect; this SRS may optionally tenant-scope them for symmetry, but the gating fix is the list.
- Per-tenant database routing. `PGRoleStore` holds a single master pool and filters by `tenant_id` (`pg/roles.go:19,109`) — both callers already hit the same pool, so this is not a DB-routing bug.

## 3. Functional Requirements

### FR-00: Users 5-vs-2 Asymmetry Is Expected (no change — documentation only)

The reported user-count difference (owner sees 5, admin sees 2 for tenant-17) is **correct, expected behavior**, not a defect.

| Caller | Path | Tenant source | Filter | tenant-17 count |
|--------|------|---------------|--------|:---------------:|
| System owner | WS `tenants.users.list` (`tenants.go:345`) | explicit `tenant_id` payload (`tenants.go:352-362`) | none (owner bypass) | 5 |
| Tenant admin | HTTP `GET /v1/tenants/{id}/users` | explicit path `{id}` | admin visibility filter hides owner + admin users (001 SRS §5.4) | 2 |

Both paths pass the tenant **explicitly**, so both are correctly scoped to tenant-17. The count differs only because the owner sees the full roster and the admin sees a privilege-filtered subset. No code change.

Acceptance criteria:

- [ ] No code change is made for the user-count asymmetry.
- [ ] Confirm against the running system: the WS owner path returns all tenant-17 users; the HTTP admin path returns tenant-17 users minus owner/admin-level users.

---

### FR-01: Roles List Must Resolve to the Explicitly-Viewed Tenant

The roles list must return the **viewed** tenant's roles, independent of the caller's ambient active tenant. Today it resolves from the ambient `X-GoClaw-Tenant-Id` header (caller's home tenant), which is wrong for a cross-tenant owner.

| Caller today | Endpoint | Tenant resolved from | Correct? |
|---|---|---|:---:|
| System owner viewing tenant-17 | `GET /v1/roles` | ambient header = owner active tenant = **master** (`auth.go:196-201` → `MasterTenantID`) | ❌ returns master's 3 roles |
| Tenant-17 admin | `GET /v1/roles` | ambient header = tenant-17 | ✅ returns tenant-17's 4 roles |

The owner path must behave like the admin path: resolve to the **viewed** tenant. The users list already achieves this by carrying the tenant explicitly (WS payload / path param); the roles list must do the same.

Acceptance criteria:

- [ ] A master-scope owner viewing tenant-17's roles sees tenant-17's roles (4: Admin, Member, Viewer + custom), not master's (3).
- [ ] The tenant used for the roles list is the **explicitly-viewed** tenant, not the caller's ambient active tenant. Changing the owner's active tenant must not change which tenant's roles a viewed-tenant Roles tab shows.
- [ ] A tenant-17 admin still sees tenant-17's roles (no regression).
- [ ] The endpoint does not expose roles from a tenant the caller is not authorized to view (owner bypass aside, a tenant member must only see their own tenant).

---

### FR-02: Tenant-Explicit Roles List Endpoint / Parameter

Provide a tenant-explicit roles list that mirrors the users list pattern, so the viewed tenant is authoritative.

**Chosen shape (mirror `GET /v1/tenants/{id}/users`):** add a tenant-scoped list — `GET /v1/tenants/{id}/roles` — where `{id}` is the viewed tenant UUID (the same shape the WS `tenants.users.list` consumes via `uuid.Parse`, `tenants.go:362`). The handler resolves `{id}`, then calls `ListRoles(ctx, resolvedTenantID, params)`. The frontend `useRoles(tenantId)` calls this endpoint with the viewed tenant from the route.

| Method | Path | Tenant source | Notes |
|--------|------|---------------|-------|
| `GET` | `/v1/tenants/{id}/roles` | path `{id}` (viewed tenant UUID) | NEW — authoritative tenant-explicit list |
| `GET` | `/v1/roles` | ambient ctx / `?tenant_id=` override | retained for backward compat / non-admin direct callers; the bug is avoided by the frontend using the tenant-explicit endpoint |

Authorization: gated by `requireAuthAction("role.list", …)` (same as today, `roles.go:32`). Owner bypasses; a tenant member without `role.list` gets 403. Non-master callers must be scoped to their own tenant — a non-owner caller passing another tenant's `{id}` returns 404 (same tenant-scope enforcement as `GET /v1/tenants/{id}/users`).

Acceptance criteria:

- [ ] `GET /v1/tenants/{id}/roles` returns the roles for tenant `{id}` with the same response shape as today: `{ "roles": [...], "total", "offset", "limit" }` (004 SRS FR-01 contract).
- [ ] A non-owner caller passing a `{id}` that is not their own tenant receives 404, never another tenant's roles.
- [ ] `GET /v1/roles` continues to work unchanged for the ctx-tenant case (no regression for admin direct callers / existing consumers).
- [ ] Grep confirms `use-roles.ts` is the only list consumer switched to the tenant-explicit endpoint.

---

### FR-03: Frontend Passes the Viewed Tenant

The roles hooks and pages must pass the **viewed** tenant to the list, derived from the route — not the ambient active tenant.

| Site | Today | After |
|------|-------|-------|
| `use-roles.ts:24,37` `useRoles()` | no tenant; `http.get("/v1/roles", queryParams)` | `useRoles({ tenantId })` → `http.get(\`/v1/tenants/${tenantId}/roles\`, queryParams)` |
| `tenant-roles-tab.tsx:23,31` | `TenantRolesTab()` no props; `useRoles({ search })` | receives viewed tenant UUID from `tenant-detail-page.tsx` (`useParams().id`, `tenant-detail-page.tsx:35`) and forwards it |
| `role-management-page.tsx:32` | `useRoles({ search })` | resolves the route tenant (`/t/{tenant}/admin/roles`) and forwards it (slug → UUID via the tenants cache, consistent with how the app resolves tenant slugs elsewhere) |

The viewed tenant UUID is already available: `tenant-detail-page.tsx:35` reads `useParams<{ id }>()` and passes it to `useTenantDetail(id)` which feeds `tenants.users.list { tenant_id: id }` — so `id` is a tenant UUID. `TenantRolesTab` is rendered inside that page (`tenant-detail-page.tsx:153`) and can receive the same UUID.

Acceptance criteria:

- [ ] `TenantRolesTab` accepts and forwards the viewed tenant UUID to `useRoles`.
- [ ] `role-management-page.tsx` forwards its route tenant (resolved to UUID) to `useRoles`.
- [ ] An owner whose active tenant is master, viewing tenant-17's Roles tab, sees 4 roles.
- [ ] Switching the owner's active tenant does not change the roles shown on a fixed tenant-17 Roles tab.

---

### FR-04: i18n (no new strings expected)

No new user-facing strings are introduced by this fix (it changes request routing, not UI text). If any string is added, follow the 3-locale rule (en/vi/zh) per the Mobile/UI i18n rule and `004-feat-roles-page.md` FR-06.

Acceptance criteria:

- [ ] No new raw keys appear in the UI as a side effect of this change.
- [ ] Any added string is present in `role-management.json` for en, vi, zh.

## 4. System Impact

- **Backend HTTP:** add `GET /v1/tenants/{id}/roles` in `internal/http/roles.go` (resolve `{id}`, enforce tenant scope for non-master callers like `handleGet` does at `roles.go:154-160`, call `ListRoles`). Reuse the existing `ListRoles` store method (`pg/roles.go:104`); no store change.
- **Backend route registration:** register the new tenant-scoped route in `RolesHandler.RegisterRoutes` (`roles.go:31`), gated by `requireAuthAction("role.list", …)`.
- **Frontend hook:** `use-roles.ts` accepts `tenantId` and calls `/v1/tenants/${tenantId}/roles` (the actual fix — removes the ambient-tenant dependency).
- **Frontend pages:** `tenant-roles-tab.tsx` receives the viewed tenant UUID from `tenant-detail-page.tsx`; `role-management-page.tsx` resolves its route tenant (slug → UUID) and forwards it.
- **No schema migration**, no store change, no startup hook.

## 5. Test Plan

- Handler test: `GET /v1/tenants/{id}/roles` as owner returns the viewed tenant's roles (incl. custom); response shape `{ roles, total, offset, limit }`.
- Handler test: non-owner caller passing a foreign tenant `{id}` → 404 (no cross-tenant leak).
- Handler test: caller without `role.list` → 403.
- Handler test: `GET /v1/roles` unchanged for the ctx-tenant case (regression guard).
- Frontend test: `useRoles({ tenantId })` hits `/v1/tenants/${tenantId}/roles` and returns the viewed tenant's rows; `total` + row count match.
- Manual: owner (active tenant = master) opens tenant-17 detail → Roles tab shows 4 roles (Admin, Member, Viewer, custom). Then opens the standalone `/t/<tenant-17>/admin/roles` → same 4 roles. Then switches active tenant to a third tenant → tenant-17 Roles tab still shows tenant-17's 4 roles.
- Manual: tenant-17 admin opens `/t/<tenant-17>/admin/roles` → still 4 roles (no regression).

## 6. Risks and Open Questions

| Risk or question | Draft decision |
|------------------|----------------|
| `role-management-page.tsx` route carries a tenant **slug**, not UUID (`/t/{tenant}/admin/roles`); the new endpoint expects a UUID. | Resolve slug → UUID in the page via the existing tenants cache (`pkgTenantCache.GetTenantBySlug` equivalent on the client), the same resolution the HTTP client header path uses (`auth.go:322`). Alternatively, accept slug-or-UUID in `{id}` (resolve both, matching `resolveScopedTenant` `auth.go:318-324`). Prefer slug-or-UUID `{id}` to avoid client-side resolution churn and to mirror how `X-GoClaw-Tenant-Id` accepts slugs today. |
| Should `GET /v1/roles` (the non-scoped endpoint) be deprecated/removed? | Keep it for backward compatibility and non-admin direct callers; just stop relying on it for the cross-tenant owner path. Deprecation is a separate cleanup. |
| Should role create/update/delete/assign also become tenant-scoped (`/v1/tenants/{id}/roles/...`) for symmetry? | Optional follow-up, not gating. Those endpoints already enforce tenant scope on the loaded role for non-master callers (`roles.go:154-160`), so they are not leaking. Track separately. |
| Confirm the owner's active tenant is indeed `master` when reproducing (the bug premise). | Verify by inspecting `localStorage["goclaw:tenant_id"]` while the owner views tenant-17 from the tenants-admin console; it should be `master` (or empty → `MasterTenantID` fallback), proving the ambient-header mismatch. |

## 7. Implementation Plan

1. **Backend — tenant-scoped list:** add `GET /v1/tenants/{id}/roles` in `internal/http/roles.go`. Resolve `{id}` (slug or UUID via the tenant cache), enforce tenant scope for non-master callers (mirror `handleGet` `roles.go:154-160`), call `ListRoles(ctx, resolvedTenantID, params)`, return `{ roles, total, offset, limit }`. Register the route in `RegisterRoutes` gated by `requireAuthAction("role.list", …)` (FR-02).
2. **Frontend hook:** `use-roles.ts` — `useRoles({ tenantId, search, limit, offset })` calls `/v1/tenants/${tenantId}/roles` (FR-03). Guard `enabled: !!tenantId` to avoid a fetch before the tenant is known.
3. **Frontend pages:** thread the viewed tenant UUID into the hook — `tenant-roles-tab.tsx` receives it from `tenant-detail-page.tsx` (`useParams().id`); `role-management-page.tsx` resolves its route tenant (slug → UUID) (FR-03).
4. **Tests:** handler tests (owner correct tenant, foreign-tenant 404, no-`role.list` 403, `GET /v1/roles` regression) + frontend test for the hook contract (§5).
5. **Manual verification:** owner-with-active-master viewing tenant-17 Roles tab + standalone roles page → 4 roles; active-tenant switch does not change viewed-tenant roles; tenant-17 admin unchanged (§5).
6. **Checklist:** `go build ./...`, `go build -tags sqliteonly ./...`, `go vet ./...`, and `pnpm build` in `ui/web`.

## 8. Proposed Error Codes

| Code | Meaning |
|------|---------|
| `role.not_found` | Role ID does not exist or is outside the caller's tenant scope (existing `ErrNotFound` path, `roles.go:150`). |
| `tenant.not_found` | The `{id}` in `/v1/tenants/{id}/roles` does not resolve to a known tenant (reuse the existing tenant-resolution not-found path). |
| `request.validation_failed` | `{id}` is neither a valid UUID nor a resolvable slug (existing `ErrInvalidRequest` 400). |
| `role.forbidden` | Caller lacks `role.list`, or a non-owner caller targets a foreign tenant (existing 403 path; canonical name aligned with `004-feat-roles-page.md` §9). |
