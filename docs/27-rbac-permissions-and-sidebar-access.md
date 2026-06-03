# 27 — RBAC Permissions and Sidebar Access Matrix

GoClaw uses a multi-layer permission system spanning backend (Go) and frontend (React). This document maps every role to what a user **sees** (sidebar/routes) and what they **can do** (CRUD / view-only) on the backend.

---

## 1. Role Hierarchy

Four roles exist, ordered by privilege level. Higher roles inherit all lower-role capabilities.

| Level | Role | Description |
|:-----:|------|-------------|
| 4 | **Owner** | Global system owner. Full access to all tenants and config. Only role that sees master tenant data, config page, tenants admin, and backup/restore. |
| 3 | **Admin** | Tenant-level administrator. Full CRUD within their tenant. Sees system section (users, providers, API keys, etc.) but NOT config, tenants, or backup/restore. |
| 2 | **Member** | Standard user. Can chat, create sessions, run agents, use skills, manage monitoring, API keys, CLI credentials, approvals, and nodes. Cannot access admin pages or mutation endpoints for admin-scoped resources. |
| 1 | **Viewer** | Read-only user. Can browse data but cannot create, update, or delete anything. |

### Role Resolution Flow

A user's role is resolved in this priority order:

```
1. Config-based owner IDs (gateway.owner_ids in config.json5) → RoleOwner
   ⚠ Fail-closed: when owner_ids is empty/unset, only user ID "system" is treated as owner.
2. Tenant ownership (tenant_users.is_owner = true) → RoleOwner
3. RBAC permission derivation via Resolver:
   a. Has system.manage_settings → RoleAdmin
   b. Has any write permission (not *.list/*.get/*.view_*) → RoleMember
   c. Only read permissions → RoleViewer
4. Fallback → RoleMember (if tenant membership exists)
5. Fallback → RoleViewer (if caches unavailable)
```

### Master Tenant Restriction for Non-Owners

Non-owners **cannot scope to the Master tenant**. This is enforced at auth resolution time (not just at the SQL layer):

- **WS connect:** If a non-owner resolves to Master tenant, the connection is rejected with `ErrTenantAccessRevoked`.
- **HTTP auth:** If a non-owner's tenant resolution yields Master, `resolveAuth()` returns an empty `authResult` (unauthenticated).
- **Tenant resolution:** `ResolveUserTenant()` prefers non-Master tenants — a user with both Master + real tenant memberships resolves to the real tenant. Master is returned only when it's the sole membership.
- **`tenants.mine`:** The WS `tenants.mine` method skips Master from results for non-owners, so the tenant selector never offers Master as a selectable scope.

**Code locations:**
- Master rejection (WS): `internal/gateway/router.go:199`, `internal/gateway/router.go:319`
- Master rejection (HTTP): `internal/http/auth.go:213`, `internal/http/auth.go:291`
- Non-master preference: `internal/store/pg/tenant_store.go:313` (`ResolveUserTenant`)
- `tenants.mine` filter: `internal/gateway/methods/tenants.go:548`

**Code locations:**
- Role levels: `internal/permissions/policy.go:466` (`roleLevel`)
- JWT role resolution: `internal/http/auth_handler.go:535` (`resolveUserRoleForJWT`)
- WS connect role resolution: `internal/gateway/router.go:533` (`getUserTenantRole`)
- HTTP role resolution: `internal/http/auth.go:341` (`resolveJWTRole`)

---

## 2. Authentication Paths

The system supports multiple auth methods, each producing a role:

| Auth Method | Role Derivation | Scope |
|-------------|-----------------|-------|
| Gateway token + owner ID | RoleOwner | All tenants (master scope) |
| Gateway token + non-owner ID | RoleAdmin | Scoped to user's first non-master tenant membership. Master tenant is **explicitly denied** — if no other membership exists, auth fails. |
| API key | From key scopes (admin/write/read) | Key's bound tenant or master |
| JWT (email/OIDC login) | From tenant_users + RBAC | JWT `tid` claim. Login resolves to first non-master tenant via `ResolveUserTenant`. |
| Browser pairing | RoleMember | Paired user's first non-master tenant. Master is denied. |
| No token (dev mode) | RoleAdmin | Master tenant |

---

## 3. Sidebar Menu Access Matrix

### 3.1 Full Access Matrix

| Sidebar Menu | Owner | Admin | Member | Viewer | Backend Guard |
|:-------------|:-----:|:-----:|:------:|:------:|:--------------|
| **Core** | | | | | |
| Chat | ✓ | ✓ | ✓ | ✓ | Read=Viewer, Send=Member+ (viewer can full CRUD via chat) |
| Overview | ✓ | ✓ | ✓ | ✗ | Viewer (hidden for viewer) |
| Agents | ✓ | ✓ | ✓ | ✗ | Read=Viewer, CUD=Admin (hidden for viewer) |
| Agent Teams | ✓ | ✓ | ✓ | ✗ | Read=Viewer, CUD=Admin (hidden for viewer) |
| **Conversations** *(member+ only)* | | | | | |
| Sessions | ✓ | ✓ | ✓ | ✗ | Read=Viewer, Delete/Reset=Member (hidden for viewer) |
| Pending Messages | ✓ | ✓ | ✓ | ✗ | Viewer (hidden for viewer) |
| Raw Messages | ✓ | ✓ | ✓ | ✗ | Viewer (hidden for viewer) |
| Contacts | ✓ | ✓ | ✓ | ✗ | Viewer (hidden for viewer) |
| **Connectivity** *(member+ only)* | | | | | |
| Channels | ✓ | ✓ | ✓ | ✗ | Read=Viewer, Toggle/CUD=Admin (hidden for viewer) |
| Nodes (Pairing) | ✓ | ✓ | ✓ | ✗ | Member+ for approve/deny/revoke/list |
| Workstations | ✓ | ✓ | ✓ | ✗ | Read=Viewer, CUD=Admin (hidden for viewer) |
| **Capabilities** *(member+ only)* | | | | | |
| Skills | ✓ | ✓ | ✓ | ✗ | Read=Viewer, Update=Admin (hidden for viewer) |
| Builtin Tools | ✓ | ✓ | ✓ | ✗ | Viewer (hidden for viewer) |
| MCP | ✓ | ✓ | ✓ | ✗ | Viewer (hidden for viewer) |
| TTS | ✓ | ✓ | ✓ | ✗ | Read=Viewer, Convert=Member, Config=Admin (hidden for viewer) |
| Cron | ✓ | ✓ | ✓ | ✗ | Read=Viewer, CUD=Member (hidden for viewer) |
| Hooks | ✓ | ✓ | ✓ | ✗ | Read=Viewer, CUD=Admin (hidden for viewer) |
| **Data** *(member+ only)* | | | | | |
| Memory | ✓ | ✓ | ✓ | ✗ | Viewer (hidden for viewer) |
| Vault | ✓ | ✓ | ✓ | ✗ | Viewer, owner-only write ops (hidden for viewer) |
| Knowledge Graph | ✓ | ✓ | ✓ | ✗ | Viewer (hidden for viewer) |
| Embeddings | ✓ | ✓ | ✓ | ✗ | Viewer (hidden for viewer) |
| Storage | ✓ | ✓ | ✓ | ✗ | Viewer (hidden for viewer) |
| **Monitoring** *(member+ for events/activity/logs)* | | | | | |
| Traces | ✓ | ✓ | ✓ | ✓ | Viewer |
| Events | ✓ | ✓ | ✓ | ✗ | Member+ (hidden for viewer) |
| Activity | ✓ | ✓ | ✓ | ✗ | Member+ (hidden for viewer) |
| Logs | ✓ | ✓ | ✓ | ✗ | Member+ via `logs.tail` (hidden for viewer) |
| **Security** *(member+ only)* | | | | | |
| CLI Credentials | ✓ | ✓ | ✓ | ✗ | Redirects to `packages?tab=cli-credentials` (no guard on redirect, `RequireMember` on Packages page) |
| API Keys | ✓ | ✓ | ✓ | ✗ | Member+ list/create/revoke (hidden for viewer) |
| Approvals | ✓ | ✓ | ✓ | ✗ | Read=Viewer, Approve/Deny=Member (hidden for viewer) |
| **System** *(admin-only section)* | | | | | |
| User Management | ✓ | ✓ | ✗ | ✗ | Admin + `requireTenantAdmin` |
| Groups | ✓ | ✓ | ✗ | ✗ | Admin + `requireTenantAdmin` |
| Roles | ✓ | ✓ | ✗ | ✗ | Admin + RBAC action check |
| Audit Log | ✓ | ✓ | ✗ | ✗ | Admin |
| Tenants | ✓ | ✗ | ✗ | ✗ | **Owner-only** (`isOwner` guard) |
| Providers | ✓ | ✓ | ✗ | ✗ | Admin |
| Packages | ✓ | ✓ | ⚠ | ✗ | Sidebar: admin+. Route: `RequireMember`. Members reach via CLI Credentials redirect, see only `cli-credentials` tab. |
| Authentication | ✓ | ✓ | ✗ | ✗ | Admin |
| Import/Export | ✓ | ✓ | ✗ | ✗ | Admin |
| Config | ✓ | ✗ | ✗ | ✗ | **Owner-only** (`isOwner` + `requireMasterScope`) |
| Backup & Restore | ✓ | ✗ | ✗ | ✗ | **Owner-only** (`isOwner` guard) |

### 3.2 Legend

| Symbol | Meaning |
|:------:|---------|
| ✓ | Menu visible, full backend access for that role's level |
| ✗ | Menu hidden in sidebar, backend denies access |
| ⚠ | Menu visible in sidebar, but backend rejects write/mutation calls for non-admin roles |
| Read= | View-only methods allowed at stated role level |
| CUD= | Create/Update/Delete requires stated role level |

---

## 4. Backend Permission Layers

The backend enforces permissions at three layers. All must pass for a request to succeed.

### Layer 1: Gateway Token / Auth Resolution

Every request (HTTP or WS) goes through auth resolution:

```
HTTP: resolveAuth() → authResult{Role, TenantID, Authenticated}
WS:   handleConnect() → client{role, tenantID, authenticated}
```

If `Authenticated == false`, the request is rejected immediately with `401 Unauthorized`.

**Non-owner master rejection:** For non-owners, if the resolved tenant is the Master tenant, the auth result is cleared (treated as unauthenticated). This happens before role checks or scope guards. The tenant resolution functions (`ResolveUserTenant`, `resolveDefaultTenant`) prefer non-Master tenants using SQL `ORDER BY (tenant_id = master_uuid) ASC`, pushing Master to the end.

**Code:** `internal/http/auth.go:178` (HTTP), `internal/gateway/router.go:140` (WS)

### Layer 2: Role-Based Method Access

The `PolicyEngine.CanAccess(role, method)` check enforces minimum role per RPC method:

```go
MethodRole(method) → RoleNone | RoleViewer | RoleMember | RoleAdmin
CanAccess(role, method) → roleLevel(role) >= roleLevel(MethodRole(method))
```

**Method classification** (`internal/permissions/policy.go`):

| Category | Required Role | Examples |
|----------|:------------:|----------|
| Public (pre-auth) | Viewer | `connect`, `health`, `status` |
| Read-only | Viewer | `*.list`, `*.get`, `*.view_*` |
| Write | Member | `chat.send`, `sessions.delete`, `cron.create`, `device.pair.*`, `api_keys.*`, `logs.tail` |
| Admin | Admin | `config.*`, `agents.create`, `teams.create` |
| Unclassified | **Denied** (RoleNone) | Any new method not yet classified |

**Fail-closed design:** Methods absent from all allowlists return `RoleNone` and are denied for **every** role including owner. New RPCs must be explicitly added to the appropriate list. This prevents security regressions like CVE #866.

### Layer 3: Scope Guards (Tenant Isolation)

Beyond role checks, specific handlers enforce scope guards:

| Guard | Function | Effect |
|-------|----------|--------|
| `requireOwner` | `client.IsOwner()` | Only global owners pass |
| `requireMasterScope` | `store.IsMasterScope(ctx)` | Owner OR nil/master tenant passes |
| `requireTenantAdmin` | Owner bypass OR tenant admin via RBAC | Owner or admin within the tenant passes |
| `requireAuthAction(action)` | RBAC permission check | User must have the specific permission string |

**When to use which guard:**

| Resource type | Table has `tenant_id`? | Write guard |
|:-------------|:----------------------:|:-----------:|
| Global (builtin_tools, disk config, packages) | No | `requireMasterScope` |
| Tenant-scoped (agents, sessions, hooks, skills) | Yes | `requireTenantAdmin` |
| Owner-only (tenants, config, backup) | Mixed | `requireOwner` + `requireMasterScope` |

---

## 5. Granular RBAC Permission Catalog

Beyond the four-role hierarchy, GoClaw supports fine-grained permissions via the `permissions.Resolver`. These are stored in the `roles` and `role_permissions` tables and resolved per-user per-tenant.

**Code:** `internal/permissions/catalog.go`

### 5.1 Permission Strings

| Domain | Permission | Type | Description |
|--------|-----------|:----:|-------------|
| **User Management** | `user.list` | Read | List users |
| | `user.get` | Read | View user details |
| | `user.create` | Write | Create new user |
| | `user.update` | Write | Edit user profile |
| | `user.delete` | Write | Delete/deactivate user |
| | `user.enroll` | Write | Enroll user in tenant |
| | `user.unenroll` | Write | Remove user from tenant |
| | `user.assign_role` | Write | Change user's role |
| **Group Management** | `group.list` | Read | List groups |
| | `group.get` | Read | View group details |
| | `group.create` | Write | Create group |
| | `group.update` | Write | Edit group |
| | `group.delete` | Write | Delete group |
| | `group.manage_members` | Write | Add/remove group members |
| | `group.assign_role` | Write | Assign role to group |
| **Role Management** | `role.list` | Read | List roles |
| | `role.get` | Read | View role details |
| | `role.create` | Write | Create custom role |
| | `role.update` | Write | Edit role permissions |
| | `role.delete` | Write | Delete custom role |
| **Audit** | `audit.view_all` | Read | View all audit entries |
| | `audit.view_group` | Read | View group-scoped audit |
| | `audit.export` | Write | Export audit data |
| **System** | `system.manage_settings` | Write | Access config/settings (→ maps to Admin role) |
| | `system.manage_auth` | Write | Manage auth providers |
| | `system.view_health` | Read | View system health |

### 5.2 Read-Only Detection

A permission is considered read-only if it matches:
- `*.list` suffix
- `*.get` suffix
- `*.view_*` pattern

All other permissions are write actions. This classification drives role derivation:

```go
// internal/permissions/catalog.go:57
func IsReadOnlyPermission(p string) bool
```

### 5.3 Permission Resolution

The `Resolver` (`internal/permissions/resolver.go`) computes effective permissions:

```
User effective permissions = Union of:
  1. Direct role permissions (user_roles → roles → role_permissions)
  2. Group role permissions (group_members → group_roles → roles → role_permissions)
  3. Ancestor group roles (recursive CTE for nested groups)
```

Results are cached per `(userID, tenantID)` with configurable TTL (default 5 minutes).

---

## 6. Frontend Route Guards

### 6.1 Route-Level Guards

React Router wraps pages with guard components:

| Guard Component | Required Role | Pages Protected |
|----------------|:------------:|-----------------|
| `RequireAuth` | Any authenticated | All app pages (checks token/userId) |
| `RequireSetup` | Setup completed | All pages under `/t/:slug` |
| `RequireMember` | Member or above | Events, Activity, Logs, API Keys, Packages (incl. CLI Credentials tab), TTS |
| `RequireAdmin` | Admin or Owner | Config, Providers, TTS, User Mgmt, Groups, Audit, Roles, Import/Export, Authentication |
| `RequireCrossTenant` | Owner (deprecated alias) | Config, Tenants Admin, Tenant Detail |

**Code:** `ui/web/src/components/shared/require-role.tsx`

```typescript
const ROLE_LEVELS = { owner: 4, admin: 3, member: 2, viewer: 1 };
function hasMinRole(role, required) → ROLE_LEVELS[role] >= ROLE_LEVELS[required]
```

### 6.2 Sidebar Visibility Logic

Sidebar visibility is driven by role from `useAuthStore` and `isOwner` from `useTenants`:

```typescript
const role = useAuthStore((s) => s.role);           // "owner" | "admin" | "member" | "viewer"
const { isOwner, currentTenantSlug } = useTenants();
const isAdmin = role === "admin" || role === "owner";
const isMember = role === "member" || isAdmin;
```

| Condition | Visible Items |
|-----------|--------------|
| **Viewer** (only) | Chat, Traces |
| **Member+** (`isMember`) | Overview, Agents, Teams, Conversations, Connectivity (incl. Nodes), Capabilities, Data, Monitoring (Events, Activity, Logs), Security (CLI Credentials, API Keys, Approvals) |
| **Admin+** (`isAdmin`) | User Mgmt, Groups, Roles, Audit Log, Providers, Packages, Authentication, Import/Export (System section) |

> **Note:** "CLI Credentials" sidebar link redirects to the Packages page (`../packages?tab=cli-credentials`). The Packages sidebar item is admin-only, but the Packages route guard is `RequireMember` — members can access it via the CLI Credentials redirect. Members see only the `cli-credentials` tab; admin+ sees all tabs.
| **Owner** (`isOwner`) | Tenants, Config, Backup & Restore (within System section) |

**Code:** `ui/web/src/components/layout/sidebar.tsx`

---

## 7. CRUD Access by Role and Domain

### 7.1 Agents

| Action | Owner | Admin | Member | Viewer |
|--------|:-----:|:-----:|:------:|:------:|
| List / Get | ✓ | ✓ | ✓ | ✓ |
| Create / Update / Delete | ✓ | ✓ | ✗ | ✗ |
| File get / list | ✓ | ✓ | ✓ | ✓ |
| File set | ✓ | ✓ | ✗ | ✗ |
| Links list | ✓ | ✓ | ✓ | ✓ |
| Links CUD | ✓ | ✓ | ✗ | ✗ |

### 7.2 Teams

| Action | Owner | Admin | Member | Viewer |
|--------|:-----:|:-----:|:------:|:------:|
| List / Get | ✓ | ✓ | ✓ | ✓ |
| Create / Delete / Update | ✓ | ✓ | ✗ | ✗ |
| Task list / get / comments / events | ✓ | ✓ | ✓ | ✓ |
| Task create / assign | ✓ | ✓ | ✓ | ✗ |
| Task approve / reject / comment | ✓ | ✓ | ✓ | ✗ |
| Task delete / bulk delete | ✓ | ✓ | ✗ | ✗ |
| Member add / remove | ✓ | ✓ | ✗ | ✗ |
| Workspace list / read | ✓ | ✓ | ✓ | ✓ |
| Workspace delete | ✓ | ✓ | ✗ | ✗ |

### 7.3 Chat & Sessions

| Action | Owner | Admin | Member | Viewer |
|--------|:-----:|:-----:|:------:|:------:|
| Send message | ✓ | ✓ | ✓ | ✗ |
| Abort / Inject | ✓ | ✓ | ✓ | ✗ |
| Chat history | ✓ | ✓ | ✓ | ✓ |
| Session list / preview | ✓ | ✓ | ✓ | ✓ |
| Session delete / reset / patch / compact | ✓ | ✓ | ✓ | ✗ |

### 7.4 Config & System

| Action | Owner | Admin | Member | Viewer |
|--------|:-----:|:-----:|:------:|:------:|
| Config get / apply / patch / schema | ✓ | ✗ | ✗ | ✗ |
| Config permissions list/grant/revoke | ✓ | ✗ | ✗ | ✗ |
| Health / Status | ✓ | ✓ | ✓ | ✓ |
| Logs tail | ✓ | ✓ | ✓ | ✗ |

### 7.5 Channels & Connectivity

| Action | Owner | Admin | Member | Viewer |
|--------|:-----:|:-----:|:------:|:------:|
| Channels list / status | ✓ | ✓ | ✓ | ✓ |
| Channels toggle | ✓ | ✓ | ✗ | ✗ |
| Channel instances CUD | ✓ | ✓ | ✗ | ✗ |
| Pairing request | ✓ | ✓ | ✓ | ✗ |
| Pairing approve / deny / revoke / list | ✓ | ✓ | ✓ | ✗ |

### 7.6 Tenants

| Action | Owner | Admin | Member | Viewer |
|--------|:-----:|:-----:|:------:|:------:|
| List / Get / Mine | ✓ | ✓ | ✓ | ✓ |
| Mine (non-owner) | ✓ | ✓ | ✓* | ✓* |

*\*For non-owners, `tenants.mine` omits the Master tenant from results. Only real tenant memberships are returned, affecting the tenant selector UI.*
| Create / Update / Delete | ✓ | ✗ | ✗ | ✗ |
| Users list | ✓ | ✓ | ✓ | ✓ |
| Users add / remove / updateRole | ✓ | ✗ | ✗ | ✗ |

### 7.7 API Keys

| Action | Owner | Admin | Member | Viewer |
|--------|:-----:|:-----:|:------:|:------:|
| List / Create / Revoke | ✓ | ✓ | ✓ | ✗ |

### 7.8 Cron & Hooks

| Action | Owner | Admin | Member | Viewer |
|--------|:-----:|:-----:|:------:|:------:|
| Cron list / status / runs | ✓ | ✓ | ✓ | ✓ |
| Cron create / update / delete / toggle / run | ✓ | ✓ | ✓ | ✗ |
| Hooks list / history | ✓ | ✓ | ✓ | ✓ |
| Hooks create / update / delete / toggle | ✓ | ✓ | ✗ | ✗ |
| Hooks test | ✓ | ✓ | ✓ | ✗ |

### 7.9 Tools & Skills

| Action | Owner | Admin | Member | Viewer |
|--------|:-----:|:-----:|:------:|:------:|
| Skills list / get | ✓ | ✓ | ✓ | ✓ |
| Skills update | ✓ | ✓ | ✗ | ✗ |

### 7.10 Monitoring

| Action | Owner | Admin | Member | Viewer |
|--------|:-----:|:-----:|:------:|:------:|
| Traces list / get / export | ✓ | ✓ | ✓ | ✓ |
| Events (realtime) | ✓ | ✓ | ✓ | ✗ |
| Activity list | ✓ | ✓ | ✓ | ✗ |
| Logs tail | ✓ | ✓ | ✓ | ✗ |

### 7.11 Security

| Action | Owner | Admin | Member | Viewer |
|--------|:-----:|:-----:|:------:|:------:|
| CLI Credentials CRUD | ✓ | ✓ | ✓ | ✗ |
| API Keys list / create / revoke | ✓ | ✓ | ✓ | ✗ |
| Approvals list | ✓ | ✓ | ✓ | ✗ |
| Approvals approve / deny | ✓ | ✓ | ✓ | ✗ |

### 7.12 TTS

| Action | Owner | Admin | Member | Viewer |
|--------|:-----:|:-----:|:------:|:------:|
| Status / providers / voices list | ✓ | ✓ | ✓ | ✓ |
| Convert (synthesize) | ✓ | ✓ | ✓ | ✗ |
| Enable / disable / set provider | ✓ | ✓ | ✗ | ✗ |

---

## 8. Scope Guard Reference

### 8.1 HTTP Guards

| Guard | Location | Check |
|-------|----------|-------|
| `requireAuth(minRole)` | `internal/http/auth.go:506` | Authenticated + `HasMinRole(role, minRole)` |
| `requireAuthAction(action)` | `internal/http/auth_rbac.go:31` | Authenticated + RBAC permission check |
| `requireMasterScope(w, r)` | `internal/http/tenant_auth_helpers.go:75` | `store.IsMasterScope(ctx)` |
| `requireTenantAdmin(w, r, ts)` | `internal/http/tenant_auth_helpers.go:23` | Owner bypass OR `PermSystemManageSettings` |

### 8.2 WebSocket Guards

| Guard | Location | Check |
|-------|----------|-------|
| `PolicyEngine.CanAccess(role, method)` | `internal/gateway/router.go:74` | Method classification + role level |
| `requireOwner(next)` | `internal/gateway/methods/config.go:50` | `client.IsOwner()` |
| `requireMasterScope(next)` | `internal/gateway/methods/config.go:74` | `store.IsMasterScope(ctx)` |

### 8.3 Context Predicates

| Predicate | Location | Returns true when |
|-----------|----------|-------------------|
| `store.IsOwnerRole(ctx)` | `internal/store/context.go:395` | `RoleFromContext(ctx) == "owner"` |
| `store.IsMasterScope(ctx)` | `internal/store/context.go:409` | IsOwnerRole OR tenant is nil/master |

---

## 9. Known Gaps and Design Notes

### 9.1 Viewer Sidebar — Minimal by Design

Viewers see only **Chat** and **Traces** in the sidebar. All other menus (Overview, Agents, Conversations, Connectivity, Capabilities, Data, Monitoring, Security) are hidden behind `isMember` guard. This is intentionally restrictive — viewers can chat and view traces, nothing else.

### 9.2 Nodes (Pairing) — Member Access

Members can approve/deny/revoke/list pairing requests via `device.pair.*` RPC methods (classified as `isWriteMethod` → Member+). The sidebar shows "Nodes" to members+ in the Connectivity group. Previously this was a known gap where the sidebar showed the menu but the backend blocked management actions — now resolved.

### 9.3 Workstations — Mixed Access

Workstation read operations are available to all roles (`workstations.list`, `workstations.get`), but create/update/delete/toggle and command group management require Admin. The sidebar shows Workstations to members+ only (hidden for viewers).

### 9.4 Sidebar vs Backend Consistency

The sidebar uses `isAdmin` (admin OR owner) for most System section items. Three items use `isOwner` (owner only):

| Item | Sidebar Guard | Backend Guard |
|------|:------------:|:-------------:|
| Tenants | `isOwner` | `requireMasterScope` |
| Config | `isOwner` | `requireOwner` + `requireMasterScope` |
| Backup & Restore | `isOwner` | Owner-only route (`RequireCrossTenant`) |

The sidebar is intentionally stricter than backend for these items — even if an admin could technically call some backend endpoints, the UI hides them.

### 9.7 Packages Page — Sidebar vs Route Guard Mismatch

The Packages page has a dual-access pattern:

| Access Path | Sidebar Guard | Route Guard | What the user sees |
|-------------|:------------:|:-----------:|-------------------|
| Packages sidebar item | `isAdmin` (admin+ only) | `RequireMember` | All tabs (system, python, node, github, cli-credentials) |
| CLI Credentials sidebar item | `isMember` (member+) | Redirect to `../packages?tab=cli-credentials` (no guard) | Only the `cli-credentials` tab |

Members never see the Packages sidebar item but can reach the Packages page via the CLI Credentials redirect. The `PackagesPage` component gates the CLI Credentials tab with `hasMinRole(role, "member")`, and the tab trigger is hidden for viewers.

### 9.8 Owner IDs Configuration

The `gateway.owner_ids` config field (in `config.json` or via `GOCLAW_OWNER_IDS` env var) controls which user IDs are recognized as global owners. When unset/empty, the system falls back to recognizing only the literal user ID `"system"` as owner. **All deployments must set `owner_ids`** — otherwise no user gets owner privileges, locking out the tenant admin UI and config page.

### 9.5 Logs Access — Security Consideration

Logs (`logs.tail`) were previously admin-only due to CVE #866 (data exfiltration risk from unauthenticated access). Members now have access with tenant isolation (SQL `WHERE tenant_id = $N`) providing defense-in-depth against cross-tenant log exfiltration. Viewer access remains restricted.

### 9.6 RBAC Resolver Cache

Effective permissions are cached with a 5-minute TTL (`internal/permissions/resolver.go`). Role changes may take up to 5 minutes to propagate. Call `Resolver.Invalidate(userID, tenantID)` after role mutations for immediate effect.

---

## 10. Permission System Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                        CLIENT                                    │
│  React App ──→ Sidebar (isAdmin / isOwner)                      │
│            ──→ Route Guards (RequireAdmin / RequireOwner)        │
│            ──→ WS connect {token} ──→ HTTP Authorization header  │
└──────────────────────────────┬──────────────────────────────────┘
                               │
┌──────────────────────────────▼──────────────────────────────────┐
│                     AUTH RESOLUTION                              │
│                                                                  │
│  HTTP: resolveAuth()         WS: handleConnect()                 │
│    Gateway token ─→ Owner/Admin    Gateway token ─→ Owner/Admin │
│    API key ─→ Scope-derived role   API key ─→ Scope-derived     │
│    JWT ─→ Tenant + RBAC role       JWT ─→ Tenant + RBAC role    │
│    Browser pairing ─→ Member       Browser pairing ─→ Member    │
│    No token (dev) ─→ Admin          No token (dev) ─→ Member    │
└──────────────────────────────┬──────────────────────────────────┘
                               │
┌──────────────────────────────▼──────────────────────────────────┐
│                   LAYER 1: ROLE CHECK                            │
│                                                                  │
│  PolicyEngine.CanAccess(role, method)                            │
│    MethodRole(method) → Viewer | Member | Admin | None          │
│    roleLevel(role) >= roleLevel(required)                        │
│    Unclassified → denied (fail-closed)                           │
└──────────────────────────────┬──────────────────────────────────┘
                               │
┌──────────────────────────────▼──────────────────────────────────┐
│                   LAYER 2: SCOPE GUARD                           │
│                                                                  │
│  requireOwner()          → client.IsOwner()                      │
│  requireMasterScope()    → IsOwnerRole OR master tenant          │
│  requireTenantAdmin()    → Owner bypass OR PermSystemManage...   │
│  requireAuthAction(p)    → RBAC fine-grained permission          │
└──────────────────────────────┬──────────────────────────────────┘
                               │
┌──────────────────────────────▼──────────────────────────────────┐
│                   LAYER 3: TENANT ISOLATION                      │
│                                                                  │
│  store.WithTenantID(ctx, tenantID)                               │
│  SQL: WHERE tenant_id = $N                                       │
│  Tenant DB pool resolution for greenfield tenants                │
└─────────────────────────────────────────────────────────────────┘
```

---

## 11. Quick Reference: Adding a New RPC Method

1. **Define the method** in `pkg/protocol/methods.go`
2. **Classify it** in `internal/permissions/policy.go`:
   - Add to `isAdminMethod()` for admin-only
   - Add to `isWriteMethod()` for member+ writes
   - Add to `isReadMethod()` for viewer+ reads
   - **Never leave it unclassified** — `RoleNone` = denied for everyone
3. **Add scope mapping** in `MethodScopes()` if applicable
4. **Add scope guard** if the method touches global or tenant-scoped resources
5. **Update this document** with the new method's access level

---

*Related docs: [09-security.md](09-security.md) for threat model, [23-multi-tenant-architecture.md](23-multi-tenant-architecture.md) for tenant isolation, [26-users-roles-and-membership.md](26-users-roles-and-membership.md) for data model.*
