# 26 — Users, Roles, and Membership

GoClaw separates **global user identity** from **tenant membership**. A single person has one global `users` record, but can belong to many tenants via `tenant_users` rows — each with its own role, display name, and metadata. Additional role systems govern access to agents, teams, and groups.

---

## 1. Global User (`users` table)

The `users` table stores identity-level data. One row per person.

| Field | Type | Description |
|-------|------|-------------|
| `id` | UUID v7 | Internal primary key |
| `email` | VARCHAR | Unique email address |
| `display_name` | VARCHAR | Human-readable name |
| `avatar_url` | VARCHAR | Optional profile image |
| `tenant_id` | UUID | **Deprecated fallback tenant** — not the source of truth for membership |
| `auth_provider` | VARCHAR | `local` \| `entra_id` \| `google` |
| `password_hash` | VARCHAR | bcrypt hash (local auth only) |
| `is_tenant_admin` | BOOLEAN | **Deprecated** — roles now live in `tenant_users` |
| `status` | VARCHAR | `active` \| `suspended` \| `deactivated` |
| `last_login_at` | TIMESTAMPTZ | Most recent successful login |
| `created_at` | TIMESTAMPTZ | Account creation time |
| `updated_at` | TIMESTAMPTZ | Last profile update |

### Source of truth

- **Identity**: `users` owns email, auth provider, password, status.
- **Tenant access**: `tenant_users` owns role, display name override, and per-tenant metadata.
- **Migration note**: `users.tenant_id` and `users.is_tenant_admin` are legacy columns kept for backward compatibility. New code should read membership from `tenant_users`.

### User identities (`user_identities` table)

Users can link multiple OAuth providers. Each row maps `(provider, provider_subject)` → `user_id`.

| Provider | `provider_subject` example |
|----------|---------------------------|
| `entra_id` | Azure AD object ID |
| `google` | Google sub claim |

---

## 2. Tenant User (`tenant_users` table)

The `tenant_users` table is a **many-to-many join** between `users` and `tenants`. One user can belong to multiple tenants; each tenant contains many users.

| Field | Type | Description |
|-------|------|-------------|
| `id` | UUID v7 | Membership row primary key |
| `tenant_id` | UUID FK | Belongs to `tenants` |
| `user_id` | VARCHAR | References `users.id` (stored as string) |
| `display_name` | VARCHAR | Optional per-tenant override |
| `role` | VARCHAR | Tenant role: `owner`, `admin`, `operator`, `member`, `viewer` |
| `metadata` | JSONB | Arbitrary per-tenant user settings |
| `created_at` | TIMESTAMPTZ | Joined timestamp |
| `updated_at` | TIMESTAMPTZ | Last update |

### Unique constraint

```sql
UNIQUE (tenant_id, user_id)
```

A user can only have one role per tenant. Upserts update `role`, `display_name`, and `updated_at` on conflict.

### Tenant role hierarchy

Defined in `internal/store/tenant_store.go`:

| Constant | Value | Level | Typical use |
|----------|-------|-------|-------------|
| `TenantRoleOwner` | `owner` | 5 | Full tenant management, billing, deletion |
| `TenantRoleAdmin` | `admin` | 4 | Manage users, agents, config |
| `TenantRoleOperator` | `operator` | 3 | Run agents, chat, create sessions |
| `TenantRoleMember` | `member` | 2 | Basic participation ( Teams, groups) |
| `TenantRoleViewer` | `viewer` | 1 | Read-only access |

**Comparison with API-key roles:** Tenant roles are **membership labels** used by the application layer and UI. They are **not** the same as gateway `RoleOwner/RoleAdmin/RoleOperator/RoleViewer` (see §3), though the names overlap. A user with `tenant_users.role = admin` still needs an API key or gateway token with `operator.admin` scope to perform admin RPCs.

### Store interface (`TenantStore`)

```go
AddUser(ctx, tenantID, userID, role string) error
RemoveUser(ctx, tenantID, userID string) error
GetUserRole(ctx, tenantID, userID string) (string, error)
ListUsers(ctx, tenantID) ([]TenantUserData, error)
ListUserTenants(ctx, userID) ([]TenantUserData, error)
ResolveUserTenant(ctx, userID) (uuid.UUID, error) // fallback to MasterTenantID
```

---

## 3. Gateway / API Key Roles (`internal/permissions`)

These roles control **which RPC methods and HTTP endpoints** a caller can access. They are derived from the authentication credential, not from `tenant_users`.

### Role constants

| Constant | Value | Level | How obtained |
|----------|-------|-------|--------------|
| `RoleOwner` | `owner` | 4 | Gateway token + user ID in `GOCLAW_OWNER_IDS` |
| `RoleAdmin` | `admin` | 3 | Gateway token (non-owner) with admin scope, or API key with `operator.admin` |
| `RoleOperator` | `operator` | 2 | API key with `operator.write` / `operator.approvals` / `operator.pairing` |
| `RoleViewer` | `viewer` | 1 | API key with `operator.read` only |
| `RoleNone` | `""` | 0 | Unclassified method or no valid auth (fail-closed) |

### Scope → role derivation

```go
if scopes contains "operator.admin"      → RoleAdmin
if scopes contains "operator.write"       → RoleOperator
if scopes contains "operator.approvals"   → RoleOperator
if scopes contains "operator.pairing"     → RoleOperator
if scopes contains "operator.read"        → RoleViewer
default                                    → RoleViewer
```

### Role vs tenant role — key difference

| Dimension | Gateway/API Key Role | Tenant Role (`tenant_users.role`) |
|-----------|---------------------|-----------------------------------|
| **Purpose** | Gates RPC/HTTP access | Gates tenant-level application features |
| **Source** | `api_keys.scopes` or gateway token | `tenant_users` table |
| **Evaluated by** | `permissions.PolicyEngine.CanAccess()` | Application handlers, UI visibility |
| **Cross-tenant** | Owner can cross tenants | Per-tenant only |
| **Example** | `RoleAdmin` lets you call `agents.create` | `TenantRoleAdmin` lets you see tenant settings UI |

---

## 4. Agent Sharing Roles (`agent_shares` table)

Agents can be shared with individual users beyond the owner. The `agent_shares` table stores `UNIQUE(agent_id, user_id)` grants.

| Role value | Meaning |
|------------|---------|
| `owner` | Not stored in `agent_shares` — implicit via `agents.owner_id` |
| `user` | Not stored — implicit via `agents.is_default = true` |
| `admin` | Full control over agent (edit, delete, share) |
| `operator` | Can chat and use the agent |
| `viewer` | Read-only (see agent in lists, view config) |

### Access check pipeline (`AgentAccessStore.CanAccess`)

1. Agent exists and is not soft-deleted.
2. If `is_default = true` → allow (role = `owner` if owner_id matches, else `user`).
3. If `owner_id = userID` → allow (role = `owner`).
4. If row exists in `agent_shares` → allow (role from share).
5. Otherwise → deny.

### Source

- `internal/store/agent_store.go`: `AgentAccessStore` interface
- `internal/store/pg/agents.go`: `ShareAgent`, `RevokeShare`, `CanAccess`

---

## 5. Team Membership Roles (`agent_team_members` table)

Teams are groups of agents with a shared task board. `agent_team_members` links agents (not human users) to teams.

| Constant | Value | Meaning |
|----------|-------|---------|
| `TeamRoleLead` | `lead` | Lead agent — typically the `lead_agent_id` referenced by `agent_teams` |
| `TeamRoleMember` | `member` | Standard participating agent |
| `TeamRoleReviewer` | `reviewer` | Can review and approve tasks |

**Note:** This table links **agents** to teams. Human users access teams via `team_user_grants` (see §6).

### Source

- `internal/store/team_store.go`: `TeamCRUDStore` interface, `TeamRoleLead`, `TeamRoleMember`, `TeamRoleReviewer`

---

## 6. Team User Grants (`team_user_grants` table)

Human users are granted access to teams via `team_user_grants`. Unlike `agent_team_members`, this links **users** to teams.

| Field | Description |
|-------|-------------|
| `team_id` | Target team |
| `user_id` | Human user ID |
| `role` | Arbitrary string — typically mirrors tenant roles (`admin`, `operator`, `viewer`) or custom team roles |
| `granted_by` | Who issued the grant |

There are no hardcoded constants for `team_user_grants.role`. The application layer interprets the value.

### Source

- `internal/store/team_store.go`: `TeamAccessStore` interface, `TeamUserGrant` struct
- `internal/store/pg/teams.go`: `GrantTeamAccess`, `RevokeTeamAccess`, `ListTeamGrants`

---

## 7. Organizational Groups (`groups` table)

Groups are **tenant-scoped organizational units** — departments, teams, projects, or business units. They are separate from chat groups (Telegram/Discord channels) which are managed by the Channel Manager.

| Field | Type | Description |
|-------|------|-------------|
| `id` | UUID v7 | Primary key |
| `name` | VARCHAR | Display name |
| `slug` | VARCHAR | URL-safe identifier, unique per tenant |
| `description` | TEXT | Optional description |
| `parent_group_id` | UUID | Parent group for hierarchy (max 3 levels) |
| `tenant_id` | UUID FK | Scoped to tenant |
| `visibility` | VARCHAR | `open` (anyone can join) or `closed` (invite/approval only) |
| `max_members` | INT | `0` = unlimited |
| `created_by` | UUID | Who created the group |
| `status` | VARCHAR | `active` or `deleted` (soft delete) |

### Hierarchy

- Root groups: `parent_group_id IS NULL` (level 1)
- Max depth: 3 levels enforced by PostgreSQL trigger `trg_group_hierarchy_depth`
- Tree API: `GET /v1/groups/tree`

### Correlation with users and tenant users

| Relationship | How |
|-------------|-----|
| **Users** | `group_members.user_id` references `users.id` (UUID). A user can belong to many groups within the same tenant. |
| **Tenant users** | Groups are tenant-scoped (`groups.tenant_id`). Membership is implicitly valid only for users who also have a `tenant_users` row in that tenant. |
| **User deletion** | A user cannot be deleted while they still have active `group_members` rows. The delete handler checks `GetUserGroups` and returns `409 Conflict` if any remain. |

### Group membership (`group_members` table)

| Field | Description |
|-------|-------------|
| `group_id` | References `groups` |
| `user_id` | References `users.id` |
| `role` | `admin` or `member` |
| `joined_at` | Timestamp |
| `joined_via` | `admin_add`, `self_join`, `request_approved` |

**Last-admin guard:** `UpdateMemberRole` prevents demoting the final admin of a group. If `admin_count <= 1` and the current role is `admin`, demotion is rejected.

### Group roles vs tenant roles

| Dimension | Group role (`group_members.role`) | Tenant role (`tenant_users.role`) |
|-----------|-----------------------------------|-----------------------------------|
| **Scope** | Single group | Single tenant |
| **Values** | `admin`, `member` | `owner`, `admin`, `operator`, `member`, `viewer` |
| **Manages** | Group members, join requests | Tenant-wide users, agents, config |
| **Example** | Group admin can approve join requests | Tenant admin can create API keys |

### Join requests

Closed groups support self-service join requests:

1. User calls `POST /v1/groups/{id}/join-requests`
2. Group admin reviews: `PATCH /v1/groups/{id}/join-requests/{reqId}` (`approved` or `rejected`)
3. On approval, a `group_members` row is created with `joined_via = request_approved`

| Status | Meaning |
|--------|---------|
| `pending` | Awaiting review |
| `approved` | Accepted, member row created |
| `rejected` | Denied |

### Resource visibility scoping

Groups enable a 3-tier visibility model for resources (skills, documents, etc.):

| Scope | Visible to |
|-------|-----------|
| `personal` | Owner only (or tenant admin) |
| `group` | Group members, group admins, tenant admin |
| `tenant` | All authenticated users in the tenant |

**Scope transitions** require escalating authority:
- `personal → group`: requires `group_admin` or `tenant_admin`
- `group → tenant`: requires `tenant_admin`
- Demotions require the same level as the original scope.

Evaluated by `internal/store/visibility_filter.go`: `IsResourceVisibleTo`, `CanTransitionScope`.

### Audit logging

Group actions (create, update, delete, member add/remove, role change, join request review) are logged to `audit_log` with:
- `resource_type`: `group`, `membership`, `user`, `permission`, `system`
- `group_id`: Link to the affected group
- `actor_id`: Who performed the action
- `detail`: JSONB payload

### Source

- `internal/store/group_store.go`: `GroupData`, `GroupMemberData`, `GroupStore`, `GroupRoleAdmin`, `GroupRoleMember`
- `internal/store/visibility_filter.go`: `ScopePersonal`, `ScopeGroup`, `ScopeTenant`, `IsResourceVisibleTo`, `CanTransitionScope`
- `internal/http/groups.go`: HTTP handlers
- `internal/http/users.go`: User deletion guard (`GetUserGroups`)

---

## 8. Summary of All Role Systems

| System | Table / Code | Applies to | Values |
|--------|-------------|------------|--------|
| **Tenant membership** | `tenant_users` | Human users per tenant | `owner`, `admin`, `operator`, `member`, `viewer` |
| **Gateway/API key** | `internal/permissions/policy.go` | Authenticated callers | `owner`, `admin`, `operator`, `viewer`, `none` |
| **Agent shares** | `agent_shares` | Human users per agent | `admin`, `operator`, `viewer` (implicit `owner`) |
| **Team agent members** | `agent_team_members` | Agents per team | `lead`, `member`, `reviewer` |
| **Team user grants** | `team_user_grants` | Human users per team | Any string (application-defined) |
| **Organizational group** | `group_members` | Users per group (tenant-scoped) | `admin`, `member` |

---

## 9. Database Schema

```sql
-- Global identity
CREATE TABLE users (
    id              UUID PRIMARY KEY,
    email           VARCHAR NOT NULL,
    display_name    VARCHAR,
    avatar_url      VARCHAR,
    tenant_id       UUID,          -- deprecated fallback
    auth_provider   VARCHAR NOT NULL DEFAULT 'local',
    password_hash   VARCHAR,
    is_tenant_admin BOOLEAN DEFAULT false, -- deprecated
    status          VARCHAR NOT NULL DEFAULT 'active',
    last_login_at   TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Tenant membership (many-to-many)
CREATE TABLE tenant_users (
    id           UUID PRIMARY KEY,
    tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id      VARCHAR NOT NULL,
    display_name VARCHAR,
    role         VARCHAR NOT NULL DEFAULT 'viewer',
    metadata     JSONB DEFAULT '{}',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, user_id)
);

-- Agent shares
CREATE TABLE agent_shares (
    id          UUID PRIMARY KEY,
    agent_id    UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    user_id     VARCHAR NOT NULL,
    role        VARCHAR NOT NULL,
    granted_by  VARCHAR,
    tenant_id   UUID,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (agent_id, user_id)
);

-- Team agent members
CREATE TABLE agent_team_members (
    team_id    UUID NOT NULL REFERENCES agent_teams(id) ON DELETE CASCADE,
    agent_id   UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    role       VARCHAR NOT NULL DEFAULT 'member',
    joined_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    tenant_id  UUID,
    PRIMARY KEY (team_id, agent_id)
);

-- Team user grants
CREATE TABLE team_user_grants (
    id          UUID PRIMARY KEY,
    team_id     UUID NOT NULL REFERENCES agent_teams(id) ON DELETE CASCADE,
    user_id     VARCHAR NOT NULL,
    role        VARCHAR NOT NULL,
    granted_by  VARCHAR,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    tenant_id   UUID,
    UNIQUE (team_id, user_id)
);

-- Organizational groups
CREATE TABLE groups (
    id              UUID PRIMARY KEY,
    name            VARCHAR NOT NULL,
    slug            VARCHAR NOT NULL,
    description     TEXT,
    parent_group_id UUID REFERENCES groups(id) ON DELETE SET NULL,
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    visibility      VARCHAR NOT NULL DEFAULT 'closed',
    max_members     INTEGER NOT NULL DEFAULT 0,
    created_by      UUID REFERENCES users(id),
    status          VARCHAR NOT NULL DEFAULT 'active',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (slug, tenant_id)
);

-- Group members
CREATE TABLE group_members (
    id         UUID PRIMARY KEY,
    group_id   UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role       VARCHAR NOT NULL DEFAULT 'member',
    joined_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    joined_via VARCHAR NOT NULL DEFAULT 'admin_add',
    UNIQUE (group_id, user_id)
);

-- Group join requests
CREATE TABLE join_requests (
    id          UUID PRIMARY KEY,
    group_id    UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    status      VARCHAR NOT NULL DEFAULT 'pending',
    reviewed_by UUID REFERENCES users(id),
    reviewed_at TIMESTAMPTZ,
    message     TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (group_id, user_id, status)
);

-- Audit log
CREATE TABLE audit_log (
    id            UUID PRIMARY KEY,
    tenant_id     UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    actor_id      UUID REFERENCES users(id),
    action        VARCHAR NOT NULL,
    resource_type VARCHAR NOT NULL,
    resource_id   UUID NOT NULL,
    group_id      UUID REFERENCES groups(id) ON DELETE SET NULL,
    detail        JSONB,
    ip_address    VARCHAR,
    user_agent    TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

---

## 10. File Reference

| Module | Path | Purpose |
|---|---|---|
| User store interface | `internal/store/user_store.go` | `UserData`, `UserIdentity`, `UserStore` |
| Tenant store interface | `internal/store/tenant_store.go` | `TenantData`, `TenantUserData`, `TenantStore`, tenant role constants |
| Permission engine | `internal/permissions/policy.go` | `Role`, `Scope`, `PolicyEngine`, gateway role constants |
| Agent access store | `internal/store/agent_store.go` | `AgentAccessStore`, `AgentShareData` |
| Team store interface | `internal/store/team_store.go` | `TeamCRUDStore`, `TeamAccessStore`, `TeamUserGrant`, team role constants |
| Group store interface | `internal/store/group_store.go` | `GroupData`, `GroupMemberData`, `GroupStore`, `GroupRoleAdmin`, `GroupRoleMember` |
| Visibility filter | `internal/store/visibility_filter.go` | `ScopePersonal`, `ScopeGroup`, `ScopeTenant`, `IsResourceVisibleTo`, `CanTransitionScope` |
| PG implementations | `internal/store/pg/users.go`, `tenant_store.go`, `agents.go`, `teams.go`, `groups.go` | SQL queries and scans |
| SQLite implementations | `internal/store/sqlitestore/tenants.go`, `agents_access.go`, `teams.go`, `groups.go` | Lite edition parity |
| HTTP handlers | `internal/http/groups.go`, `internal/http/users.go` | Group CRUD + user deletion guard |
