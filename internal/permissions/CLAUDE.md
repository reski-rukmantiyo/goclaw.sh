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
        → PolicyEngine.CanAccess(role, method)     // Layer 1: role check
        → requireOwner/requireMasterScope/          // Layer 2: scope guard
           requireTenantAdmin/requireAuthAction
        → SQL WHERE tenant_id = $N                  // Layer 3: tenant isolation
```

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
