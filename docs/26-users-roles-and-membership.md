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

## 7. Group Chat Roles (`group_members` table)

Channel group chats (Telegram groups, Discord guilds, etc.) track member roles for mention-gating and admin checks.

| Constant | Value | Meaning |
|----------|-------|---------|
| `GroupRoleAdmin` | `admin` | Can change group settings, trigger bot commands |
| `GroupRoleMember` | `member` | Standard participant |

### Source

- `internal/store/group_store.go`: `GroupRoleAdmin`, `GroupRoleMember`

---

## 8. Summary of All Role Systems

| System | Table / Code | Applies to | Values |
|--------|-------------|------------|--------|
| **Tenant membership** | `tenant_users` | Human users per tenant | `owner`, `admin`, `operator`, `member`, `viewer` |
| **Gateway/API key** | `internal/permissions/policy.go` | Authenticated callers | `owner`, `admin`, `operator`, `viewer`, `none` |
| **Agent shares** | `agent_shares` | Human users per agent | `admin`, `operator`, `viewer` (implicit `owner`) |
| **Team agent members** | `agent_team_members` | Agents per team | `lead`, `member`, `reviewer` |
| **Team user grants** | `team_user_grants` | Human users per team | Any string (application-defined) |
| **Group chat** | `group_members` | Users per chat group | `admin`, `member` |

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

-- Group chat members
CREATE TABLE group_members (
    id         UUID PRIMARY KEY,
    group_id   UUID NOT NULL,
    user_id    UUID NOT NULL,
    role       VARCHAR NOT NULL DEFAULT 'member',
    joined_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    joined_via VARCHAR,
    UNIQUE (group_id, user_id)
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
| Group store interface | `internal/store/group_store.go` | `GroupRoleAdmin`, `GroupRoleMember` |
| PG implementations | `internal/store/pg/users.go`, `tenant_store.go`, `agents.go`, `teams.go`, `groups.go` | SQL queries and scans |
| SQLite implementations | `internal/store/sqlitestore/tenants.go`, `agents_access.go`, `teams.go`, `groups.go` | Lite edition parity |
