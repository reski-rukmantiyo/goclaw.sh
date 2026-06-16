# internal/permissions — RBAC Policy Engine

Role-based access control for gateway RPC methods and HTTP endpoints.

## Role Hierarchy

```
Owner(4) > Admin(3) > Member(2) > Viewer(1) > None(0)
```

`roleLevel()` in `policy.go:466` drives all comparisons.

## Files

| File | Purpose |
|------|---------|
| `policy.go` | `Role`/`Scope` types, `PolicyEngine`, `MethodRole()` classification, `CanAccess()`, role/scope derivation |
| `catalog.go` | `Permission` string constants (user/group/role/audit/system), `IsReadOnlyPermission()`, `AllPermissions()` |
| `resolver.go` | `Resolver` — computes effective permissions per (user, tenant) from DB with TTL cache |
| `policy_test.go` | Core policy engine tests |
| `policy_cron_ws_test.go` | Cron/WS method classification tests |
| `role_derivation_test.go` | Role-from-permissions derivation tests |
| `sidebar_access_test.go` | Sidebar ↔ backend access matrix verification |
| `cli_credentials_member_test.go` | CLI Credentials member access across all layers (HTTP, sidebar, packages page tab, tenant guard) |

## Key Functions

### Method Classification (`policy.go`)

Every RPC method must appear in exactly one list:

| Function | Required Role | Use for |
|----------|:------------:|---------|
| `isPublicMethod()` | Viewer | Pre-auth: `connect`, `health`, `status`, `browser.pairing.status` |
| `isReadMethod()` | Viewer | Read-only: `*.list`, `*.get`, `*.view_*` — **explicit allowlist** |
| `isWriteMethod()` | Member | Mutations: `chat.send`, `sessions.delete`, `cron.create`, etc. |
| `isAdminMethod()` | Admin | Admin: `config.*`, `agents.create`, `teams.create`, `api_keys.*`, `logs.tail` |
| (none of the above) | **Denied** | `RoleNone` — fail-closed, reject everyone |

**Critical:** New RPCs must be added to the correct list. Unlisted methods return `RoleNone` and are denied for all roles including owner. This is the CVE #866 fix.

### Scope → Role Mapping (`policy.go`)

`RoleFromScopes(scopes)` derives role from API key scopes:
- Has `admin` → Admin
- Has `write`/`approvals`/`pairing` → Member
- Has `read` → Viewer
- Otherwise → Viewer

### Permission Catalog (`catalog.go`)

Fine-grained permission strings (e.g., `user.list`, `system.manage_settings`).

`IsReadOnlyPermission(p)` returns true for `*.list`, `*.get`, `*.view_*` suffixes. Used by role derivation to distinguish viewer vs member when no explicit admin permission exists.

### Resolver (`resolver.go`)

Computes effective permissions for a user in a tenant:
1. Direct role permissions via `store.RoleStore.GetUserEffectivePermissions()`
2. Cached per `"userID:tenantID"` key with configurable TTL (default 5 min)
3. `Invalidate(userID, tenantID)` / `InvalidateAll()` for cache busting after mutations

## Permission Enforcement Flow

```
Request → Auth Resolution (HTTP/WS)
        → Master tenant rejection for non-owners    // Layer 0: non-owners denied master scope
        → PolicyEngine.CanAccess(role, method)     // Layer 1: role check
        → requireOwner/requireMasterScope/          // Layer 2: scope guard
           requireTenantAdmin/requireAuthAction
        → SQL WHERE tenant_id = $N                  // Layer 3: tenant isolation
```

### Layer 0: Master Tenant Restriction

Non-owners **cannot scope to the Master tenant**. Enforced at auth resolution before any role or scope checks:

- **WS connect:** Non-owner users resolving to Master get `ErrTenantAccessRevoked` (`gateway/router.go:199`, `:319`)
- **HTTP auth:** Non-owner users resolving to Master get empty `authResult{}` = unauthenticated (`http/auth.go:213`, `:291`)
- **Tenant resolution:** `ResolveUserTenant()` prefers non-Master tenants via `ORDER BY (tenant_id = master_uuid) ASC` (`store/pg/tenant_store.go:313`). Only returns Master when it's the sole membership.
- **`tenants.mine`:** Skips Master from results for non-owners (`gateway/methods/tenants.go:548`)

### Owner ID Configuration

`gateway.owner_ids` (config.json or `GOCLAW_OWNER_IDS` env) determines global owner recognition. When empty/unset, only user ID `"system"` is treated as owner (fail-closed default). **All deployments must set `owner_ids`** — otherwise no user gets owner privileges.

Owner check functions: `isOwnerID()` (`gateway/router.go:470`), `isHTTPOwnerID()` (`http/auth.go:133`).

## Adding a New Method

1. Define method constant in `pkg/protocol/methods.go`
2. Add to the correct classification list in `policy.go`:
   - `isAdminMethod()` for admin-only
   - `isWriteMethod()` for member+ writes
   - `isReadMethod()` for viewer+ reads
   - **Never skip this step** — unclassified = denied for everyone
3. Add scope mapping in `MethodScopes()` if applicable
4. Add test in the appropriate `*_test.go` file
5. Update `docs/27-rbac-permissions-and-sidebar-access.md`

## Scope Guards (used by callers, not defined here)

| Guard | Location | Check |
|-------|----------|-------|
| `requireOwner` | `gateway/methods/config.go` | `client.IsOwner()` |
| `requireMasterScope` | `http/tenant_auth_helpers.go` + `gateway/methods/config.go` | `store.IsMasterScope(ctx)` |
| `requireTenantAdmin` | `http/tenant_auth_helpers.go` | Owner bypass OR `PermSystemManageSettings` |
| `requireAuthAction` | `http/auth_rbac.go` | Specific permission string check |

## Related

- Full access matrix: `docs/27-rbac-permissions-and-sidebar-access.md`
- Security model: `docs/09-security.md`
- Data model: `docs/26-users-roles-and-membership.md`
- Tenant isolation: `docs/23-multi-tenant-architecture.md`
