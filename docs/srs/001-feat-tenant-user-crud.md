# Software Requirements Specification

## Tenant-Scoped User CRUD

**GoClaw Platform — Multi-Tenant User Management**

Version 1.0 | 4 June 2026

> **Revision (2026-06-16, v1.1):** The admin visibility filter was relaxed — a tenant **admin can now see peer admins** in the same tenant. Only the tenant **Owner** remains hidden from admins. This is a **visibility-only** change; mutation authorization (create/update/delete/role-change of admins or the owner by an admin) is unchanged. Canonical spec: `006-bugfix-admin-peer-visibility.md`. Affected sections below (§1.3, §3 FR-TU2/FR-TU3, §4.3, §4.4, §5.2, §5.3, §5.4, §9.1 AC-15) are updated to the new policy.

---

## Table of Contents

- [1. Introduction](#1-introduction)
- [2. Overall Description](#2-overall-description)
- [3. Functional Requirements](#3-functional-requirements)
- [4. API Specification](#4-api-specification)
- [5. Authorization Model](#5-authorization-model)
- [6. Data Model](#6-data-model)
- [7. Frontend Specification](#7-frontend-specification)
- [8. Non-Functional Requirements](#8-non-functional-requirements)
- [9. Acceptance Criteria](#9-acceptance-criteria)
- [10. SRS Traceability](#10-srs-traceability)

---

## 1. Introduction

### 1.1 Purpose

This SRS defines the **Tenant-Scoped User CRUD** feature — dedicated HTTP REST API endpoints and a frontend UI for creating, reading, updating, and deleting users within a tenant boundary. This replaces the previous WebSocket-RPC-only user management with a proper REST API, enabling programmatic access and a richer admin experience.

### 1.2 Scope

The module covers:

- Full CRUD API for tenant-scoped users (create, list, get, update, delete)
- Role management within a tenant (assign, change, owner toggle)
- Target-role-aware authorization (admin cannot manage owner/admin users)
- Dual-mode user creation (create new + enroll, or enroll existing)
- Frontend UI with create user form, inline editing, and role management
- Safety guards: self-deletion prevention, last-owner protection

**Out of scope:**

- Global user management (cross-tenant) — see `/v1/users` endpoints
- User authentication flows (login, OAuth, password reset)
- Group membership management
- RBAC permission catalog management

### 1.3 Definitions & Acronyms

| Term | Definition |
|------|-----------|
| Tenant | Top-level isolation boundary in GoClaw. Users belong to tenants via `tenant_users`. |
| Owner | User with `is_owner = true` in `tenant_users`. Full access, bypasses RBAC. |
| Admin | User with admin-level RBAC permissions. Can view peer Admins + Member/Viewer (Owner hidden); can manage (create/update/delete/role-change) Member/Viewer users only. |
| Gateway Token | Platform-level bearer token. Grants owner-equivalent access across all tenants. |
| Target-role-aware | Authorization that checks the target user's role, not just the caller's role. |

### 1.4 References

- GoClaw Multi-Tenant Architecture — [docs/23-multi-tenant-architecture.md](23-multi-tenant-architecture.md)
- Users, Roles, and Membership — [docs/26-users-roles-and-membership.md](26-users-roles-and-membership.md)
- RBAC Permissions — [docs/27-rbac-permissions-and-sidebar-access.md](27-rbac-permissions-and-sidebar-access.md)
- HTTP REST API — [docs/18-http-api.md](18-http-api.md)
- Multi-Auth Module SRS — [docs/srs/multi-auth-module-srs.md](multi-auth-module-srs.md)

---

## 2. Overall Description

### 2.1 Current State (Before)

GoClaw provided basic tenant user management via WebSocket RPC:

- `tenants.users.list` — list users in tenant
- `tenants.users.add` — enroll existing user (by user_id)
- `tenants.users.remove` — remove user from tenant
- `tenants.users.update_role` — change user role

**Gaps:**

1. No HTTP REST API — only WebSocket RPC
2. No user creation within tenant (enroll-only, no create+enroll)
3. No individual user GET or UPDATE (display_name, phone)
4. No self-deletion guard
5. No last-owner protection
6. No admin role restrictions (admin could manage owners/other admins)
7. No phone field on user profile
8. Frontend dialog was enroll-only (no create user form)

### 2.2 Target State (After)

- Dedicated REST API with 6 endpoints under `/v1/tenants/{id}/users`
- Dual-mode creation: create new user + enroll, or enroll existing
- Full CRUD: create, list, get, update profile, update role, delete
- Target-role-aware authorization: admin restricted to Member/Viewer scope
- Safety guards: self-deletion blocked, last-owner blocked
- Phone field on user profile
- Frontend with create user form, inline editing, and role management

### 2.3 User Classes

| Caller Type | Auth Method | Tenant User Scope |
|-------------|-------------|-------------------|
| Platform Owner | Gateway Token + owner ID | Full access to all tenants, all operations |
| Tenant Owner | Per-tenant `is_owner = true` | Full access within their tenant |
| Tenant Admin | RBAC `system.manage_settings` permission | Restricted: Member/Viewer CRUD only, no role changes |
| Member/Viewer | Standard user | No access to user management |

---

## 3. Functional Requirements

### FR-TU1: Create User in Tenant (Dual Mode)

**POST /v1/tenants/{id}/users**

The system shall support two modes of adding a user to a tenant:

**Create Mode** — when `email` is provided (no `user_id`):

1. Validate required fields: `email`, `password`, `role`
2. Validate password complexity: minimum length, uppercase letter, symbol
3. Check for duplicate email (global uniqueness)
4. Create new user with `auth_provider = local`, `status = active`
5. Hash password with bcrypt
6. Enroll user in tenant with specified role
7. Assign RBAC role for non-owner users (owner bypasses RBAC)
8. Return 201 Created

**Enroll Mode** — when `user_id` is provided (no `email`):

1. Normalize user ID (resolve email to UUID if needed)
2. Enroll existing user in tenant with `is_owner` flag based on role
3. Assign RBAC role for non-owner users
4. Return 201 Created

### FR-TU2: List Tenant Users

**GET /v1/tenants/{id}/users**

1. Fetch all `tenant_users` rows for the tenant
2. Enrich each user with: `email`, `display_name`, `phone`, `status` from `users` table
3. Resolve effective role per user (owner → "owner", else derive from RBAC)
4. If caller is Admin (not Owner): filter out **Owner users only** from response. Peer Admins are visible (read-only); an admin cannot manage them (FR-TU3/FR-TU5/FR-TU6). See `006-bugfix-admin-peer-visibility.md`.
5. Return 200 with `{ "users": [...] }`

### FR-TU3: Get Single Tenant User

**GET /v1/tenants/{id}/users/{userId}**

1. Normalize target user ID
2. Auth: check caller can read the target (admin → Owner denied; Admin/Member/Viewer allowed, Admin is read-only)
3. Fetch `tenant_users` row and enrich with user data
4. Return 200 with enriched user object

### FR-TU4: Update User Profile

**PUT /v1/tenants/{id}/users/{userId}**

1. Normalize target user ID
2. Auth: check caller can update the target (admin → Member/Viewer only)
3. Accept only `display_name` and `phone` fields
4. Reject request if `email` or `role` present in body → 422
5. Update `users` table
6. Emit audit event
7. Return 200 with `{ "status": "updated" }`

### FR-TU5: Update User Role

**PUT /v1/tenants/{id}/users/{userId}/role**

1. Normalize target user ID
2. Auth: **Owner or Gateway Token only** (admin cannot change roles)
3. Validate role is one of: `owner`, `admin`, `member`, `viewer`
4. Handle role transition:
   - **To owner**: set `is_owner = true`, unassign all RBAC roles
   - **From owner to non-owner**: set `is_owner = false`, assign new RBAC role
   - **Between non-owner roles**: unassign old RBAC roles, assign new
5. Emit cache invalidation and audit event
6. Return 200 with `{ "status": "role_updated" }`

### FR-TU6: Remove User from Tenant

**DELETE /v1/tenants/{id}/users/{userId}**

1. Normalize target user ID
2. **Self-deletion guard**: if target user ID matches caller → 422
3. Auth: check caller can delete the target (admin → Member/Viewer only)
4. **Last-owner guard**: if target is owner and `CountOwners(tenantID) <= 1` → 409
5. Remove `tenant_users` row
6. Unassign all RBAC roles
7. Broadcast `tenant_access_revoked` WebSocket event (force logout)
8. Emit audit event
9. Return 200 with `{ "ok": "true" }`

---

## 4. API Specification

### 4.1 Route Summary

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `GET` | `/v1/tenants/{id}/users` | Admin+ | List tenant users (enriched) |
| `POST` | `/v1/tenants/{id}/users` | Admin+ | Create/enroll user in tenant |
| `GET` | `/v1/tenants/{id}/users/{userId}` | Admin+ | Get single tenant user |
| `PUT` | `/v1/tenants/{id}/users/{userId}` | Admin+ | Update display_name, phone |
| `PUT` | `/v1/tenants/{id}/users/{userId}/role` | Owner only | Change user role |
| `DELETE` | `/v1/tenants/{id}/users/{userId}` | Admin+ | Remove user from tenant |

All endpoints require Bearer token authentication. Admin+ means minimum `RoleAdmin` gateway role.

### 4.2 POST /v1/tenants/{id}/users — Create/Enroll User

**Create mode request body:**

```json
{
  "email": "user@example.com",
  "display_name": "John Doe",
  "phone": "+1 234 567 890",
  "password": "Str0ng!Pass",
  "role": "member"
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `email` | string | yes (create) | Unique email address |
| `display_name` | string | no | User's display name |
| `phone` | string | no | Phone number |
| `password` | string | yes (create) | Must contain uppercase + symbol |
| `role` | string | yes | One of: `owner`, `admin`, `member`, `viewer` |

**Enroll mode request body:**

```json
{
  "user_id": "01234567-89ab-cdef-0123-456789abcdef",
  "role": "member"
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `user_id` | string | yes (enroll) | Existing user UUID or email |
| `role` | string | yes | One of: `owner`, `admin`, `member`, `viewer` |

**Responses:**

| Status | Condition | Body |
|--------|-----------|------|
| 201 | Success | `{ "ok": "true" }` |
| 400 | Missing required fields | `{ "error": "..." }` |
| 403 | Admin creating Owner/Admin; insufficient permissions | `{ "error": "..." }` |
| 409 | Duplicate email | `{ "error": "..." }` |
| 422 | Invalid role value | `{ "error": "..." }` |

### 4.3 GET /v1/tenants/{id}/users — List Users

**Response (200):**

```json
{
  "users": [
    {
      "id": "uuid-membership-row",
      "tenant_id": "uuid-tenant",
      "user_id": "uuid-user",
      "display_name": "John Doe",
      "email": "john@example.com",
      "phone": "+1 234 567 890",
      "role": "member",
      "is_owner": false,
      "status": "active",
      "created_at": "2026-06-04T10:00:00Z",
      "updated_at": "2026-06-04T10:00:00Z"
    }
  ]
}
```

**Behavior by caller:**

| Caller | Response |
|--------|----------|
| Owner / Gateway Token | All users with all roles |
| Admin | Filtered — Owner excluded; peer Admins visible (read-only) |

### 4.4 GET /v1/tenants/{id}/users/{userId} — Get User

**Response (200):** Same enriched user object as list item.

**Responses:**

| Status | Condition |
|--------|-----------|
| 200 | Success (admin reading a peer Admin returns 200, read-only) |
| 400 | Invalid tenant or user ID |
| 403 | Admin reading Owner user |
| 404 | User not in tenant |

### 4.5 PUT /v1/tenants/{id}/users/{userId} — Update User

**Request body:**

```json
{
  "display_name": "Jane Doe",
  "phone": "+1 987 654 3210"
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `display_name` | string | no* | Updated display name |
| `phone` | string | no* | Updated phone number |

*At least one field must be provided. `email` and `role` are not updatable via this endpoint (422 if present).

**Responses:**

| Status | Condition | Body |
|--------|-----------|------|
| 200 | Success | `{ "status": "updated" }` |
| 400 | No fields provided, invalid ID | `{ "error": "..." }` |
| 403 | Admin updating Owner/Admin user | `{ "error": "..." }` |
| 422 | Body contains `email` or `role` | `{ "error": "..." }` |

### 4.6 PUT /v1/tenants/{id}/users/{userId}/role — Change Role

**Request body:**

```json
{
  "role": "admin"
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `role` | string | yes | One of: `owner`, `admin`, `member`, `viewer` |

**Responses:**

| Status | Condition | Body |
|--------|-----------|------|
| 200 | Success | `{ "status": "role_updated" }` |
| 403 | Caller is not Owner/Gateway Token | `{ "error": "..." }` |
| 404 | User not in tenant | `{ "error": "..." }` |
| 422 | Invalid role value | `{ "error": "..." }` |

**Role transition side effects:**

| Transition | Side Effect |
|------------|-------------|
| Any → Owner | Set `is_owner = true`, unassign all RBAC roles |
| Owner → Any non-owner | Set `is_owner = false`, assign matching RBAC role |
| Non-owner → Non-owner | Unassign old RBAC roles, assign new RBAC role |

### 4.7 DELETE /v1/tenants/{id}/users/{userId} — Remove User

**Responses:**

| Status | Condition | Body |
|--------|-----------|------|
| 200 | Success | `{ "ok": "true" }` |
| 400 | Invalid ID | `{ "error": "..." }` |
| 403 | Admin deleting Owner/Admin user | `{ "error": "..." }` |
| 409 | Last owner in tenant | `{ "error": "Cannot remove the last owner" }` |
| 422 | Self-deletion attempt | `{ "error": "Cannot delete your own account" }` |

**Side effects:**

- All RBAC role assignments removed
- `tenant_access_revoked` WebSocket event broadcast (triggers force logout for affected user)
- Cache invalidation emitted
- Audit log entry created

---

## 5. Authorization Model

### 5.1 Role Hierarchy

```
Gateway Token / Platform Owner
    └── Tenant Owner (is_owner = true)
            └── Tenant Admin (RBAC: system.manage_settings)
                    └── Member / Viewer (no user management access)
```

### 5.2 Permission Matrix

| Operation | Gateway Token | Tenant Owner | Tenant Admin | Member/Viewer |
|-----------|:---:|:---:|:---:|:---:|
| Create user (any role) | ✅ | ✅ | ❌ | ❌ |
| Create user (Member/Viewer) | ✅ | ✅ | ✅ | ❌ |
| Enroll existing user | ✅ | ✅ | ❌ | ❌ |
| List all users | ✅ | ✅ | ⚠️ filtered | ❌ |
| Get any user | ✅ | ✅ | ⚠️ filtered | ❌ |
| Update any user profile | ✅ | ✅ | ❌ | ❌ |
| Update Member/Viewer profile | ✅ | ✅ | ✅ | ❌ |
| Change any user role | ✅ | ✅ | ❌ | ❌ |
| Delete any user | ✅ | ✅ | ❌ | ❌ |
| Delete Member/Viewer | ✅ | ✅ | ✅ | ❌ |
| Delete self | ❌ | ❌ | ❌ | ❌ |

⚠️ = allowed but filtered (owner users hidden from response; peer admins visible, read-only). See `006-bugfix-admin-peer-visibility.md`.

### 5.3 Authorization Flow

```
1. resolveAuth() → determine caller role (Gateway Token / API Key / JWT)
2. checkTenantUserAuth():
   a. Is caller Gateway Token or per-tenant Owner? → full access
   b. Is caller Admin (has system.manage_settings, NOT owner)?
      - create: allowed for Member/Viewer roles only
      - read: allowed for Admin/Member/Viewer targets (Owner denied); Admin targets read-only
      - update/delete: allowed for Member/Viewer targets only (Owner/Admin denied)
      - role change: denied for Owner/Admin targets; Member↔Viewer allowed
   c. Otherwise → deny (403)
3. Special guards:
   - Self-deletion: caller ID == target ID → 422
   - Last-owner: target is owner AND CountOwners ≤ 1 → 409
```

### 5.4 Admin Visibility Filtering

When the caller is an Admin (not Owner), the list endpoint automatically filters out:

- Users with `is_owner = true` (tenant owners)

> **Policy change (2026-06-16, `006-bugfix-admin-peer-visibility.md`):** peer **Admins are no longer filtered** — an admin can now see other admins in the same tenant. Only the tenant **Owner** remains hidden, because it is the one role strictly above admin (`is_owner = true`, bypasses RBAC). This is **visibility-only**: an admin still cannot manage (create/update/delete/role-change) a peer admin or the owner. Visible peer-admin rows render with management actions disabled. The filter exists to hide *higher-privileged* users; a peer admin is not higher than the caller, so hiding it was over-broad.

---

## 6. Data Model

### 6.1 Schema Changes

**New column on `users` table (migration 000088):**

```sql
ALTER TABLE users ADD COLUMN phone TEXT;
```

### 6.2 New Store Methods

| Method | Store | Description |
|--------|-------|-------------|
| `CountOwners(ctx, tenantID)` | TenantStore | Count users with `is_owner = true` in tenant |
| `GetTenantUserByUser(ctx, tenantID, userID)` | TenantStore | Get single `tenant_users` row by user ID |
| `UpdateOwnerFlag(ctx, tenantID, userID, isOwner)` | TenantStore | Toggle `is_owner` flag |

### 6.3 Response Enrichment

API responses merge data from two tables:

| Field | Source Table |
|-------|-------------|
| `id`, `tenant_id`, `user_id`, `is_owner`, `created_at`, `updated_at` | `tenant_users` |
| `email`, `display_name`, `phone`, `status` | `users` |
| `role` | Derived: owner if `is_owner`, else resolved from RBAC |

---

## 7. Frontend Specification

### 7.1 Hook Layer (`use-tenant-detail.ts`)

All mutations use `useMutation` from TanStack Query with `HttpClient` (not WebSocket RPC). Queries remain WS-based for real-time updates.

| Hook | Transport | Pattern |
|------|-----------|---------|
| `createUser` | HTTP POST | `useMutation` + `http.post` |
| `enrollUser` | HTTP POST | `useMutation` + `http.post` |
| `updateUser` | HTTP PUT | `useMutation` + `http.put` |
| `removeUser` | HTTP DELETE | `useMutation` + `http.delete` |
| `updateUserRole` | HTTP PUT | `useMutation` + `http.put` |
| Tenant query | WS RPC | `useQuery` + `ws.call` |
| Users list query | WS RPC | `useQuery` + `ws.call` |

### 7.2 Create/Enroll User Dialog

Dual-mode dialog accessible from Tenant Detail page:

**Create mode (default):**
- Fields: email (required), display_name, phone, password (required), role dropdown
- Role dropdown: 4 roles for Owner callers; 3 roles (admin/member/viewer) for Admin callers
- Password hint: "Must contain uppercase letter and symbol"
- Toggle link to switch to enroll mode

**Enroll mode:**
- Fields: UserPickerCombobox (user ID), role dropdown
- Toggle link to switch back to create mode
- Same role restrictions as create mode

### 7.3 User Card Enhancements

- Phone number displayed below email (when present)
- Edit icon (pencil) per user row → inline edit for display_name and phone
- Save/Cancel buttons in edit mode
- Role dropdown: owner callers see all 4 roles; admin callers see 3 roles
- Owner badge remains static (cannot change own role)

### 7.4 i18n

All UI strings use the `tenants` i18n namespace. Keys exist in `en`, `vi`, `zh` locales:

| Key | English | Vietnamese | Chinese |
|-----|---------|------------|---------|
| `createUser` | Create User | Tạo người dùng | 创建用户 |
| `createUserTitle` | Create New User | Tạo người dùng mới | 创建新用户 |
| `orEnrollExisting` | Or enroll an existing user | Hoặc thêm người dùng đã có | 或添加已有用户 |
| `phone` | Phone | Số điện thoại | 电话 |
| `passwordHint` | Must contain uppercase letter and symbol | Phải chứa chữ hoa và ký tự đặc biệt | 必须包含大写字母和特殊符号 |
| `selfDeleteBlocked` | Cannot delete your own account | Không thể xóa tài khoản của chính bạn | 无法删除自己的账户 |
| `lastOwnerBlocked` | Cannot remove the last owner | Không thể xóa chủ sở hữu cuối cùng | 无法移除最后一位所有者 |
| `userCreated` | User created successfully | Đã tạo người dùng thành công | 用户创建成功 |
| `userUpdated` | User updated successfully | Đã cập nhật người dùng thành công | 用户更新成功 |
| `userDeleted` | User removed successfully | Đã xóa người dùng thành công | 用户删除成功 |

---

## 8. Non-Functional Requirements

### 8.1 Security

- All endpoints require Bearer token authentication
- Password hashed with bcrypt before storage
- Password complexity enforced: minimum length, uppercase letter, symbol
- Email uniqueness checked globally (not just per-tenant)
- Self-deletion prevention (422)
- Last-owner protection (409)
- Admin visibility filtering prevents privilege escalation

### 8.2 Audit

All mutations emit audit events via the message bus:

| Event | Trigger |
|-------|---------|
| `tenant.user.added` | User created or enrolled |
| `tenant.user.updated` | User profile updated |
| `tenant.user.role_changed` | User role changed |
| `tenant.user.removed` | User removed from tenant |

### 8.3 Cache Invalidation

All mutations emit `cache_invalidate` events with `kind = tenant_users` to ensure WebSocket clients see fresh data.

### 8.4 Real-time Updates

User removal broadcasts `tenant_access_revoked` WebSocket event to force the affected user's sessions to disconnect.

---

## 9. Acceptance Criteria

### 9.1 Backend

| # | Criterion | Test |
|---|-----------|------|
| AC-1 | POST with email creates new user + enrolls + assigns RBAC role | Create user with valid fields → 201, user appears in list |
| AC-2 | POST with user_id enrolls existing user | Enroll by UUID → 201 |
| AC-3 | Duplicate email rejected | Create user with existing email → 409 |
| AC-4 | Weak password rejected | Create with "password" → 400 |
| AC-5 | Admin cannot create Owner | Admin creates with role=owner → 403 |
| AC-6 | Admin cannot create Admin | Admin creates with role=admin → 403 |
| AC-7 | Self-deletion blocked | Delete own user ID → 422 |
| AC-8 | Last-owner protected | Delete sole owner → 409 |
| AC-9 | Admin cannot delete Owner | Admin deletes owner → 403 |
| AC-10 | Admin cannot delete Admin | Admin deletes admin → 403 |
| AC-11 | Admin cannot change roles | Admin calls update_role → 403 |
| AC-12 | Role change to owner sets is_owner=true | Update role to owner → is_owner=true in DB |
| AC-13 | Role change from owner unsets is_owner | Update owner to member → is_owner=false |
| AC-14 | Update rejects email/role fields | PUT with email in body → 422 |
| AC-15 | Admin list filters owners only | Admin lists users → no owner; peer admins present (read-only) |
| AC-15a | Admin can read peer admin detail | Admin GET `.../users/{adminId}` → 200 (read-only) |
| AC-15b | Admin cannot read owner | Admin GET `.../users/{ownerId}` → 403 |
| AC-16 | Gateway Token has full access | Gateway Token creates owner → 201 |

### 9.2 Frontend

| # | Criterion | Test |
|---|-----------|------|
| AC-17 | Create User dialog creates user | Fill form → submit → user appears in list |
| AC-18 | Enroll mode enrolls existing user | Switch to enroll → pick user → submit → user appears |
| AC-19 | Inline edit saves display_name and phone | Click edit → change name → save → name persists on refresh |
| AC-20 | Phone displayed in user card | User with phone shows phone below email |
| AC-21 | Role dropdown shows all 4 roles for owner | Owner sees owner/admin/member/viewer options |
| AC-22 | Role dropdown shows 3 roles for admin | Admin sees admin/member/viewer only |
| AC-23 | Self-delete shows error toast | Remove self → error toast with "Cannot delete your own account" |
| AC-24 | Last-owner shows error toast | Remove last owner → error toast with "Cannot remove the last owner" |
| AC-25 | Mobile responsive | Dialogs full-screen on narrow viewport, inputs 16px font |
| AC-26 | Build passes with no type errors | `pnpm build` succeeds |

---

## 10. SRS Traceability

Mapping from this SRS to the parent Multi-Auth Module SRS requirements:

| This SRS | Multi-Auth Module SRS | Notes |
|----------|----------------------|-------|
| FR-TU1 Create User | FR-U1 Auto-Provisioning | Pre-provisioning by admin (FR-U1, §3.2) |
| FR-TU2 List Users | FR-U4 User Directory | Admin views all users (FR-U4, §3.2) |
| FR-TU3 Get User | FR-U4 User Directory | Individual user lookup |
| FR-TU4 Update Profile | FR-U2 Profile Sync | Admin-driven profile update |
| FR-TU5 Update Role | §5 RBAC Permission Matrix | Role assignment enforcement |
| FR-TU6 Remove User | FR-U3 Suspension & Deactivation | Removal from tenant |
| §5 Authorization | §5.3 Permission Evaluation | Target-role-aware auth extends the evaluation chain |
| §7.2 i18n | §10 Non-Functional | 3-locale support (en/vi/zh) |
| §8.2 Audit | NFR-4 (implied) | All mutations emit audit events |
