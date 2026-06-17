# Software Requirements Specification

## Tenant Roles Display — Role Listing & Visibility Filtering

**GoClaw Platform — Tenant Roles Tab Enhancement**

Version 1.0 | 6 June 2026

---

## Table of Contents

- [1. Introduction](#1-introduction)
- [2. Overall Description](#2-overall-description)
- [3. Functional Requirements](#3-functional-requirements)
- [4. Frontend Specification](#4-frontend-specification)
- [5. Acceptance Criteria](#5-acceptance-criteria)
- [6. SRS Traceability](#6-srs-traceability)

---

## 1. Introduction

### 1.1 Purpose

This SRS defines enhancements to the **Tenant Roles Tab** — displaying all 4 defined user roles with caller-role-based visibility filtering, and removing the search input that is unnecessary for a small fixed set.

### 1.2 Scope

The module covers:

- Rendering all 4 user roles (owner, admin, member, viewer) in the Tenant Roles tab
- Role visibility filtering based on the accessing user's role
- Removing the search input from the Tenant Roles tab
- Frontend-only changes — no backend API modifications

**Out of scope:**

- RBAC permission management (existing RolePermissionEditor unchanged)
- Role CRUD operations (create/update/delete custom roles — separate feature)
- User management within tenants (covered in [tenant-user-crud.md](tenant-user-crud.md))

### 1.3 Definitions & Acronyms

| Term | Definition |
|------|-----------|
| Owner | User with `is_owner = true` in `tenant_users`. Full access, bypasses RBAC. Not stored as a DB role row. |
| Admin | User with admin-level RBAC permissions (`system.manage_settings`). Seeded system role. |
| Member | User with member-level RBAC permissions. Regular read + write access. |
| Viewer | User with viewer-level RBAC permissions. Read-only access. |
| Caller role | The effective role of the user currently viewing the Tenant Roles tab. |

### 1.4 References

- Tenant-Scoped User CRUD — [docs/srs/tenant-user-crud.md](tenant-user-crud.md)
- RBAC Policy Engine — `internal/permissions/policy.go`
- Role seed data — `internal/permissions/seed_roles.go`
- User role hook — `ui/web/src/hooks/use-role.ts`
- Existing Tenant Roles Tab — `ui/web/src/pages/tenants-admin/tabs/tenant-roles-tab.tsx`

---

## 2. Overall Description

### 2.1 Current State (Before)

The Tenant Roles tab (`TenantRolesTab`) fetches roles from `GET /v1/roles` API and displays them in a searchable table. Issues:

1. **Owner role not displayed** — Owner is not a DB-backed role (derived from config `gateway.owner_ids`), so it never appears in the API response. Users cannot see the Owner role in the tab.
2. **No role visibility filtering** — All users with access to the tab see the same list regardless of their role level.
3. **Unnecessary search box** — With only 3–4 roles (a small fixed set), the search input adds clutter without value.

### 2.2 Target State (After)

- All 4 defined roles displayed in the Tenant Roles tab
- Owner role shown as a static read-only entry (not from API, synthesized client-side)
- Role visibility filtered by caller's role:
  - Owner/Gateway Token: sees all 4 roles
  - Admin: sees admin, member, viewer (owner excluded)
  - Member/Viewer: no access to the Roles tab (existing restriction — no change)
- Search input removed

### 2.3 User Classes

| Caller Type | Role Visibility | Notes |
|-------------|----------------|-------|
| Platform Owner / Gateway Token | owner, admin, member, viewer | Full visibility |
| Tenant Owner (`is_owner = true`) | owner, admin, member, viewer | Full visibility |
| Tenant Admin | member, viewer | Owner and Admin roles hidden |

Member and Viewer users cannot access the Tenant Roles tab (existing sidebar/route guard — unchanged).

---

## 3. Functional Requirements

### FR-RL1: Display All 4 Defined Roles

The Tenant Roles tab shall display the 4 defined user roles from `tenant-user-crud.md` Section 1.3:

| Role | Source | Description | Editable |
|------|--------|-------------|----------|
| Owner | Synthesized client-side (not from API) | Tenant owner with full access, bypasses RBAC | No |
| Admin | `GET /v1/roles` response (system role) | Full tenant administration | Yes (permissions only) |
| Member | `GET /v1/roles` response (system role) | Regular member | Yes (permissions only) |
| Viewer | `GET /v1/roles` response (system role) | Read-only access | Yes (permissions only) |

The Owner row is synthesized client-side as a static entry with these fixed values:
- `name`: "Owner"
- `description`: derived from i18n key (e.g. "Tenant owner with full access")
- `is_system`: true
- `permissions`: display as "—" or "Full access" badge (Owner bypasses RBAC — no stored permissions)
- `type`: "System" badge
- Actions: none (no edit/delete buttons)

### FR-RL2: Role Visibility Filtering

The displayed role list shall be filtered based on the caller's effective role, resolved via `useRole()` hook from `ui/web/src/hooks/use-role.ts`:

| Caller Role | Visible Roles |
|-------------|--------------|
| Owner / Gateway Token | Owner, Admin, Member, Viewer |
| Admin | Member, Viewer |

Filtering rules:
1. Resolve caller role via `useRole()` — check `isOwner` and `isGatewayToken`
2. If caller is owner or gateway token: show all 4 roles
3. If caller is admin (not owner, not gateway token): show only Member and Viewer rows
4. The Owner row is always prepended to the API-returned list (before filtering), then filtered if needed

### FR-RL3: Remove Search Input

The search input (`SearchInput` component) shall be removed from the Tenant Roles tab.

Rationale: The role set is small and fixed (4 maximum). Search provides no value for filtering 4 items.

The layout changes from:

```
[SearchInput]                    [+ Add Role]
```

To:

```
                                 [+ Add Role]
```

The `+ Add Role` button remains right-aligned. The `useRoles` hook no longer receives a `search` parameter — always fetch with `limit: 50` and no search filter.

---

## 4. Frontend Specification

### 4.1 Component Changes

**File:** `ui/web/src/pages/tenants-admin/tabs/tenant-roles-tab.tsx`

#### 4.1.1 Remove Search State and Input

- Remove `useState` for `search`
- Remove `SearchInput` import and JSX usage
- Pass `useRoles({})` without `search` parameter

#### 4.1.2 Add Caller Role Detection

```typescript
import { useRole } from "@/hooks/use-role";
```

Use `useRole()` to determine if the caller is owner or admin:

```typescript
const { isOwner, isGatewayToken } = useRole();
const showOwnerRole = isOwner || isGatewayToken;
```

#### 4.1.3 Synthesize Owner Role Row

Define a static owner role object:

```typescript
const OWNER_ROLE: Role = {
  id: "__owner__",
  tenant_id: "",
  name: "Owner",
  description: "Tenant owner with full access, bypasses RBAC",
  is_system: true,
  permissions: [],
  created_at: "",
  updated_at: "",
};
```

#### 4.1.4 Merge and Filter Roles

After fetching roles from the API:

```typescript
const allRoles = showOwnerRole
  ? [OWNER_ROLE, ...roles]        // Owner caller: all 4 roles
  : roles.filter(r => r.name !== "Admin");  // Admin caller: member + viewer only
```

The `allRoles` array is used in the table rendering instead of the raw `roles` from the hook.

#### 4.1.5 Owner Row Rendering

The Owner row renders differently from DB-backed roles:

- **Permissions column**: Display a badge with "Full access" text (i18n key) instead of a clickable count badge
- **Actions column**: Empty — no edit or delete buttons
- **Type column**: "System" badge (same as other system roles)

#### 4.1.6 Remove Search Import

Remove unused import:

```typescript
// Remove: import { SearchInput } from "@/components/shared/search-input";
```

### 4.2 Layout Change

**Before:**

```
┌─────────────────────────────────────────────────────────┐
│ [🔍 Search roles...]                    [+ Add Role]    │
│ ┌───────────────────────────────────────────────────────┐│
│ │ Name  │ Description │ Permissions │ Type │ Actions   ││
│ │───────┼─────────────┼─────────────┼──────┼───────────││
│ │ Admin │ Full admin  │ [12]        │ Sys  │ ✏️ 🗑️     ││
│ │ Member│ Regular     │ [8]         │ Sys  │ ✏️         ││
│ │ Viewer│ Read-only   │ [4]         │ Sys  │ ✏️         ││
│ └───────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────┘
```

**After (Owner caller):**

```
┌─────────────────────────────────────────────────────────┐
│                                         [+ Add Role]    │
│ ┌───────────────────────────────────────────────────────┐│
│ │ Name   │ Description          │ Permissions │ Type    ││
│ │────────┼──────────────────────┼─────────────┼─────────││
│ │ Owner  │ Full access, bypass  │ Full access │ System  ││
│ │ Admin  │ Full admin           │ [12]        │ System  ││
│ │ Member │ Regular              │ [8]         │ System  ││
│ │ Viewer │ Read-only            │ [4]         │ System  ││
│ └───────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────┘
```

**After (Admin caller):**

```
┌─────────────────────────────────────────────────────────┐
│                                         [+ Add Role]    │
│ ┌───────────────────────────────────────────────────────┐│
│ │ Name   │ Description          │ Permissions │ Type    ││
│ │────────┼──────────────────────┼─────────────┼─────────││
│ │ Member │ Regular              │ [8]         │ System  ││
│ │ Viewer │ Read-only            │ [4]         │ System  ││
│ └───────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────┘
```

### 4.3 i18n Keys

New keys in `roleManagement` namespace:

| Key | English | Vietnamese | Chinese |
|-----|---------|------------|---------|
| `ownerDescription` | Tenant owner with full access, bypasses RBAC | Chủ sở hữu tenant với toàn quyền, vượt RBAC | 租户所有者拥有全部权限，绕过RBAC |
| `ownerFullAccess` | Full access | Toàn quyền | 全部权限 |

Add to all 3 locale files: `ui/web/src/i18n/locales/{en,vi,zh}/roleManagement.json`.

### 4.4 Data Flow

```
TenantRolesTab renders
  ├── useRole() → isOwner, isGatewayToken → determine showOwnerRole
  ├── useRoles({}) → fetch DB roles (Admin, Member, Viewer)
  ├── Synthesize OWNER_ROLE static object
  ├── Merge: showOwnerRole ? [OWNER_ROLE, ...roles] : [...roles]
  └── Render table with filtered list
```

No backend changes required. Owner role is a client-side synthesis.

---

## 5. Acceptance Criteria

| # | Criterion | Test |
|---|-----------|------|
| AC-1 | Owner caller sees all 4 roles | Owner opens Roles tab → table shows Owner, Admin, Member, Viewer rows |
| AC-2 | Gateway Token caller sees all 4 roles | Gateway Token user opens Roles tab → table shows Owner, Admin, Member, Viewer rows |
| AC-3 | Admin caller sees 2 roles (Member, Viewer only) | Admin opens Roles tab → table shows Member, Viewer rows only |
| AC-4 | Owner row has no edit/delete actions | Owner row in table → actions column is empty |
| AC-5 | Owner row shows "Full access" badge | Owner row → permissions column shows "Full access" text, not a clickable count |
| AC-6 | Owner row shows "System" badge | Owner row → type column shows "System" badge |
| AC-7 | Search input removed | Roles tab → no search input visible |
| AC-8 | Add Role button preserved | Roles tab → "+ Add Role" button present and functional |
| AC-9 | DB role rows unchanged | Admin/Member/Viewer rows render identically to current behavior (edit, permissions click, system badge) |
| AC-10 | i18n complete | All new strings present in en, vi, zh locale files |
| AC-11 | Build passes | `pnpm build` succeeds with no type errors |
| AC-12 | Mobile responsive | Table horizontally scrollable on narrow viewport (existing `overflow-x-auto` + `min-w-[500px]`) |

---

## 6. SRS Traceability

| This SRS | Parent SRS | Notes |
|----------|-----------|-------|
| FR-RL1 Display Roles | tenant-user-crud.md §1.3 | 4 defined roles listed in Definitions |
| FR-RL2 Visibility Filtering | tenant-user-crud.md §5.2 | Aligns with Permission Matrix — Admin cannot manage Owner/Admin |
| FR-RL3 Remove Search | N/A | UX simplification for small fixed dataset |
| §4.3 i18n | tenant-user-crud.md §7.4 | 3-locale support (en/vi/zh) |
| §5 AC-3 Role visibility | tenant-user-crud.md AC-21/AC-22 | Owner sees 4 roles, Admin sees 2 roles (member/viewer) — consistent with role dropdown behavior |
