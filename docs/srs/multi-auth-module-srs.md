# Software Requirements Specification

## User, Group & RBAC Module

**GoClaw Platform Extension for SOLARIS**

Version 1.0 | 30 May 2026

Prepared by: GoClaw Engineering Team

---

## Table of Contents

- [1. Introduction](#1-introduction)
- [2. Overall Description](#2-overall-description)
- [3. User Management](#3-user-management)
- [4. Group Management](#4-group-management)
- [5. Role-Based Access Control (RBAC)](#5-role-based-access-control-rbac)
- [6. Scope & Visibility Model](#6-scope--visibility-model)
- [7. API Specification](#7-api-specification)
- [8. Data Model](#8-data-model)
- [9. Entra ID Integration](#9-entra-id-integration)
- [10. Non-Functional Requirements](#10-non-functional-requirements)
- [11. Implementation Roadmap](#11-implementation-roadmap)
- [12. Acceptance Criteria](#12-acceptance-criteria)

---

## 1. Introduction

### 1.1 Purpose

This SRS defines the **User, Group & RBAC Module** — a foundational extension to the GoClaw platform required by SOLARIS (BI Gen AI Agent Workspace). The module introduces structured user identity, group-based organization, and role-based access control within a single tenant boundary.

This module is a **prerequisite** for all other SOLARIS modules that require:
- Department-scoped knowledge access
- Approval workflows with role enforcement
- Audit trails tied to authenticated identities
- Multi-user collaboration within organizational boundaries

### 1.2 Scope

The module covers:

- User identity management (Entra ID-backed, no local password)
- Hierarchical group structure (groups with optional parent/child)
- Group membership with role assignment (group admin, group member)
- Three-tier RBAC: tenant admin, group admin, member
- Scope-based visibility: personal, group, tenant
- Permission evaluation engine for API access control
- Self-service group membership (join requests, invitations)

**Out of scope:**
- Multi-tenant isolation (single tenant assumption)
- External identity provider integration beyond Entra ID
- Fine-grained field-level permissions
- Delegated administration across tenants

### 1.3 Definitions & Acronyms

| Term | Definition |
|------|-----------|
| Tenant | Top-level isolation boundary in GoClaw. All users and groups live within one tenant. |
| User | An authenticated identity backed by Entra ID (OIDC). |
| Group | A named collection of users representing a department, team, or organizational unit. |
| RBAC | Role-Based Access Control — permissions determined by assigned role. |
| Scope | Visibility boundary for resources: personal, group, or tenant-wide. |
| Entra ID | Microsoft Entra ID (formerly Azure AD) — external identity provider. |
| OIDC | OpenID Connect — authentication protocol used with Entra ID. |

### 1.4 References

- GoClaw Platform Architecture Documentation
- SOLARIS KAK — BI Gen AI Agent Workspace
- SRS Knowledge Lifecycle Module v1.0
- Microsoft Entra ID OIDC Specification
- GoClaw vs SOLARIS Gap Analysis (v1.0, May 2026)

---

## 2. Overall Description

### 2.1 Current State (GoClaw Baseline)

GoClaw currently provides:

- **Tenant** as isolation boundary
- **Scope** (personal, team, shared) for knowledge visibility
- **Agent** with per-scope permissions
- Basic user context from OIDC tokens (no structured user management)

**Missing:**
- No user entity or user directory
- No group/team concept with membership
- No RBAC beyond tenant-level access
- No approval authority delegation
- No department mapping

### 2.2 Target State

After this module:

- Every authenticated OIDC user has a **User record** in the tenant
- Users belong to one or more **Groups** (department, team, unit)
- Each group has designated **Group Admins** who can approve, manage members
- A **Tenant Admin** has full control across all groups
- Every API call is evaluated against the user's roles and group memberships
- Resources (knowledge artifacts, agents, configurations) respect scope visibility

### 2.3 User Classes

| Role | Description | Scope of Authority |
|------|-----------|-------------------|
| Tenant Admin | Platform administrator | Full access: manage all users, groups, settings, approve org-wide actions |
| Group Admin | Department/team lead | Manage group members, approve group-scoped actions, configure group settings |
| Member | Regular user | Access group-scoped resources, upload/personal scope, submit for approval |
| External Viewer | Read-only access (optional) | View published tenant-scoped resources only, no write access |

### 2.4 Constraints

- Authentication via tenant-configured provider (Local, Entra ID, or Google OAuth2)
- Single tenant — no cross-tenant user sharing
- Maximum groups per tenant: 200
- Maximum users per tenant: 5,000
- Maximum groups per user: 20
- Group hierarchy depth: maximum 3 levels (parent → child → grandchild)

---

## 3. User Management

### 3.1 User Entity

Each user in the system is represented by:

| Field | Type | Description |
|-------|------|-------------|
| id | UUID | Internal unique identifier |
| email | string | Primary email (unique per tenant) |
| display_name | string | User's display name |
| avatar_url | string | Profile picture URL (optional) |
| tenant_id | UUID | Tenant the user belongs to |
| auth_provider | enum | local \| entra_id \| google — primary provider used for first registration |
| password_hash | string | Bcrypt hash (local auth only, null for external providers) |
| status | enum | active \| suspended \| deactivated |
| last_login | timestamptz | Last successful authentication |
| created_at | timestamptz | Account creation time |
| updated_at | timestamptz | Last profile update |

**Identity linking:** A user may have multiple auth provider identities (e.g. logged in via both Entra ID and Google). These are stored in the `user_identities` table (see §8.2).

### 3.2 User Lifecycle

#### FR-U1: Auto-Provisioning on First Login

When a user authenticates for the first time (via any enabled provider):

1. System validates credentials (OIDC token for external, email/password for local)
2. Extracts identity: `sub` + `email` + `name` (from OIDC claims or registration form)
3. Checks if User with same email already exists → if yes, link new provider identity to existing User
4. If no existing User → creates User record with status = `active`, auth_provider = provider used
5. Creates entry in `user_identities` table with provider details
6. Assigns default role: `member` (no group membership by default)
7. User can immediately access personal scope

**Tenant Admin can pre-provision users** (create records before first login) to pre-assign groups.

#### FR-U2: Profile Sync

On each login (external providers):

1. System checks IdP claims against stored profile
2. If `email` or `display_name` changed in IdP → update local record
3. Log profile change in audit trail

#### FR-U3: User Suspension & Deactivation

- **Suspend:** Tenant Admin can suspend a user. Suspended users cannot authenticate. Their data is retained. Reversible.
- **Deactivate:** Tenant Admin can deactivate a user. Removes from all groups. Personal scope data retained for 30 days then archived. Irreversible after 30 days.
- Suspension/deactivation events are logged in audit trail.

#### FR-U4: User Directory

- Tenant Admin can view all users in tenant (paginated, searchable, filterable)
- Group Admin can view users in their groups
- Member can view users in shared groups (display name + email only)
- Search filters: name, email, group, status, role

---

## 4. Group Management

### 4.1 Group Entity

| Field | Type | Description |
|-------|------|-------------|
| id | UUID | Internal unique identifier |
| name | string | Group display name (e.g. "Risk Management") |
| slug | string | URL-safe identifier (e.g. "risk-mgmt") — auto-generated from name |
| description | text | Group description (optional) |
| parent_group_id | UUID | Parent group for hierarchy (nullable = top-level) |
| tenant_id | UUID | Tenant the group belongs to |
| visibility | enum | open \| closed |
| max_members | integer | Optional member cap (0 = unlimited) |
| created_by | UUID | User who created the group |
| created_at | timestamptz | Group creation time |
| updated_at | timestamptz | Last modification time |

### 4.2 Group Hierarchy

Groups support a parent-child hierarchy to model departments and sub-departments:

```
Bank Indonesia (Tenant)
├── Risk Management (Group - top level)
│   ├── Credit Risk (Group - child)
│   └── Market Risk (Group - child)
├── IT Operations (Group - top level)
│   ├── Infrastructure (Group - child)
│   └── Application (Group - child)
└── Compliance (Group - top level)
```

**Rules:**
- Maximum depth: 3 levels
- Parent group admins have implicit admin access to child groups
- Child group members do NOT automatically inherit parent group membership
- Visibility: child group resources are visible to parent group admins

### 4.3 Group Visibility Types

| Type | Join Behavior | Use Case |
|------|-------------|----------|
| **Open** | Any tenant member can join freely | Communities of practice, interest groups |
| **Closed** | Requires approval from Group Admin | Departments, restricted teams |

SOLARIS departments should be **Closed** groups — membership managed by Group Admin.

### 4.4 Group Lifecycle

#### FR-G1: Create Group

- **Who:** Tenant Admin
- Creates a new group with name, description, parent (optional), visibility
- Creator becomes the first Group Admin
- Slug auto-generated from name, uniqueness enforced per tenant

#### FR-G2: Update Group

- **Who:** Group Admin of that group, or Tenant Admin
- Updatable fields: name, description, visibility, max_members, parent_group_id
- Name change triggers slug regeneration
- Audit log entry created

#### FR-G3: Delete Group

- **Who:** Tenant Admin only
- Soft-delete: group marked as deleted, retained for 30 days
- Members are removed from group
- Group-scoped resources are reassigned to tenant scope or re-mapped by admin
- Hard-delete after 30 days (audit record retained permanently)

#### FR-G4: Group Membership Management

**Add member:**
- Group Admin or Tenant Admin can add any tenant user to the group
- System enforces max_members limit
- If group is Closed, only admin can add members
- If group is Open, users can self-join

**Remove member:**
- Group Admin can remove members from their group
- Tenant Admin can remove from any group
- User can leave a group voluntarily (unless they are the last Group Admin)

**Join request (Closed groups):**
- User submits join request
- Group Admin approves or rejects
- Notification sent to user on decision

**Role assignment within group:**
- Group Admin can promote a member to Group Admin
- Group Admin can demote another Group Admin to member (cannot demote self if last admin)
- Tenant Admin can assign/remove Group Admin role for any group

---

## 5. Role-Based Access Control (RBAC)

### 5.1 Role Hierarchy

```
Tenant Admin
    └── Group Admin (per group)
            └── Member
                    └── (optional) External Viewer
```

### 5.2 Permission Matrix

| Action | Tenant Admin | Group Admin | Member |
|--------|:---:|:---:|:---:|
| **User Management** | | | |
| View all users | ✅ | ❌ | ❌ |
| View group users | ✅ | ✅ (own group) | ✅ (name only) |
| Suspend/deactivate user | ✅ | ❌ | ❌ |
| Pre-provision user | ✅ | ❌ | ❌ |
| **Group Management** | | | |
| Create group | ✅ | ❌ | ❌ |
| Update group settings | ✅ | ✅ (own group) | ❌ |
| Delete group | ✅ | ❌ | ❌ |
| Manage group membership | ✅ | ✅ (own group) | ❌ |
| Assign group admin role | ✅ | ✅ (own group) | ❌ |
| View group hierarchy | ✅ | ✅ (own + children) | ✅ (own group) |
| **Knowledge / Artifacts** | | | |
| Upload to personal scope | ✅ | ✅ | ✅ |
| Upload to group scope | ✅ | ✅ (own group) | ❌ |
| Submit for review | ✅ | ✅ | ✅ (own artifacts) |
| Approve group-level publish | ✅ | ✅ (own group) | ❌ |
| Approve org-wide publish | ✅ | ❌ | ❌ |
| View group-scoped artifacts | ✅ | ✅ (own group) | ✅ (own group) |
| View tenant-scoped artifacts | ✅ | ✅ | ✅ |
| Delete artifact | ✅ | ✅ (own group) | ✅ (own only) |
| **Agent Configuration** | | | |
| Create/configure agent | ✅ | ✅ (own group) | ✅ (personal) |
| Attach knowledge scope to agent | ✅ | ✅ (own group) | ✅ (personal) |
| Publish agent to group | ✅ | ✅ (own group) | ❌ |
| Publish agent to tenant | ✅ | ❌ | ❌ |
| **Audit & Compliance** | | | |
| View audit trail (all) | ✅ | ❌ | ❌ |
| View audit trail (group) | ✅ | ✅ (own group) | ❌ |
| Export audit report | ✅ | ✅ (own group) | ❌ |
| **System Configuration** | | | |
| Manage tenant settings | ✅ | ❌ | ❌ |
| Manage Entra ID integration | ✅ | ❌ | ❌ |
| View system health | ✅ | ❌ | ❌ |

### 5.3 Permission Evaluation

Every API request goes through this evaluation:

```
1. Authenticate → validate OIDC token → resolve User
2. Resolve context → which group? which resource? which action?
3. Evaluate permissions:
   a. Is user Tenant Admin? → full access
   b. Is user Group Admin of relevant group? → group admin permissions
   c. Is user Member of relevant group? → member permissions
   d. None of the above? → deny
4. Log access decision in audit trail
```

**Permission caching:** User permissions cached in memory, invalidated on:
- Role change (promotion/demotion)
- Group membership change
- User suspension/deactivation
- Cache TTL: 5 minutes maximum

### 5.4 First Tenant Admin Bootstrap

On first deployment:

1. System creates default Tenant Admin from configuration
2. Bootstrap config specifies Entra ID `entra_id` for initial admin
3. First admin can then create groups and assign other Group Admins
4. Bootstrap record locked — cannot be deleted, only transferred

---

## 6. Scope & Visibility Model

### 6.1 Scope Definitions

| Scope | Visibility | Owner | Access Control |
|-------|-----------|-------|---------------|
| **personal** | Creator only | User who created it | Only creator can read/write. Tenant Admin can view. |
| **group** | All members of the group | Group (assigned during publish) | Group Admin + members can read. Group Admin + Tenant Admin can write/publish. |
| **tenant** | All users in tenant | Tenant-level (assigned during org-wide publish) | All authenticated users can read. Tenant Admin only can write/publish. |

### 6.2 Scope Transitions

Resources (knowledge artifacts, agents, configurations) can transition between scopes:

```
personal → group (requires: Group Admin approval)
group → tenant (requires: Tenant Admin approval)
tenant → group (requires: Tenant Admin — demotion)
group → personal (requires: Group Admin or Tenant Admin — demotion)
```

### 6.3 Cross-Group Access

- Group A members **cannot** see Group B resources (unless also a member of Group B)
- Tenant Admin can see all resources across all groups
- Parent group admins can see child group resources (read-only)
- No cross-tenant access — ever

---

## 7. API Specification

### 7.1 Authentication

All endpoints require a valid Bearer token in the `Authorization` header.

```
Authorization: Bearer <token>
```

Token source depends on the tenant's configured auth provider:

**External provider (Entra ID / Google OAuth2):**
1. Determine provider from token issuer (`iss` claim)
2. Verify signature against provider's JWKS endpoint
3. Verify `aud` claim matches GoClaw client ID
4. Verify `iss` claim matches provider issuer URL
5. Extract `sub` claim as user identity
6. Check token expiry
7. Resolve or create User record

**Local provider:**
1. Verify JWT signature using GoClaw's signing key
2. Validate claims: `sub`, `exp`, `iat`
3. Resolve User record
4. Check user status (active, not suspended)

### 7.2 Authentication Endpoints

| Method | Endpoint | Auth Required | Description |
|--------|----------|:---:|-------------|
| POST | /auth/login | ❌ | Local auth: email + password login |
| POST | /auth/refresh | ❌ | Refresh JWT session token |
| POST | /auth/logout | ✅ | Invalidate session |
| POST | /auth/password/change | ✅ | Change own password (local auth) |
| POST | /auth/password/reset-request | ❌ | Request password reset email |
| POST | /auth/password/reset | ❌ | Reset password with token |
| GET | /auth/providers | ❌ | List enabled auth providers for tenant |
| GET | /auth/entra/authorize | ❌ | Redirect to Entra ID login |
| GET | /auth/entra/callback | ❌ | Entra ID OIDC callback |
| GET | /auth/google/authorize | ❌ | Redirect to Google login |
| GET | /auth/google/callback | ❌ | Google OAuth2 callback |

### 7.3 User Endpoints

| Method | Endpoint | Min Role | Description |
|--------|----------|----------|-------------|
| GET | /api/v1/users/me | member | Get current user profile |
| PATCH | /api/v1/users/me | member | Update own profile (avatar only) |
| GET | /api/v1/users | tenant_admin | List all users (paginated, filterable) |
| GET | /api/v1/users/{id} | tenant_admin | Get user detail |
| PATCH | /api/v1/users/{id}/status | tenant_admin | Suspend / reactivate user |
| DELETE | /api/v1/users/{id} | tenant_admin | Deactivate user |

### 7.3 Group Endpoints

| Method | Endpoint | Min Role | Description |
|--------|----------|----------|-------------|
| POST | /api/v1/groups | tenant_admin | Create group |
| GET | /api/v1/groups | member | List groups (own groups for member, all for admin) |
| GET | /api/v1/groups/{id} | member (own) / any admin | Get group detail |
| PATCH | /api/v1/groups/{id} | group_admin (own) / tenant_admin | Update group |
| DELETE | /api/v1/groups/{id} | tenant_admin | Delete group |
| GET | /api/v1/groups/{id}/members | member (own) / any admin | List group members |
| POST | /api/v1/groups/{id}/members | group_admin (own) / tenant_admin | Add member to group |
| DELETE | /api/v1/groups/{id}/members/{userId} | group_admin (own) / tenant_admin | Remove member from group |
| PATCH | /api/v1/groups/{id}/members/{userId}/role | group_admin (own) / tenant_admin | Change member role in group |
| POST | /api/v1/groups/{id}/join | member | Request to join (closed groups) |
| GET | /api/v1/groups/{id}/join-requests | group_admin (own) | List pending join requests |
| PATCH | /api/v1/groups/{id}/join-requests/{reqId} | group_admin (own) | Approve / reject join request |
| GET | /api/v1/groups/tree | member | Get group hierarchy tree |

### 7.4 Permission Check Endpoint

| Method | Endpoint | Min Role | Description |
|--------|----------|----------|-------------|
| GET | /api/v1/permissions/check?action=X&resource=Y&scope=Z | member | Check if current user can perform action |
| GET | /api/v1/permissions/me | member | Get current user's full permission set (roles, groups, scopes) |

### 7.5 Audit Endpoints

| Method | Endpoint | Min Role | Description |
|--------|----------|----------|-------------|
| GET | /api/v1/audit | tenant_admin | Query audit trail (filterable) |
| GET | /api/v1/audit/groups/{id} | group_admin (own) | Query group audit trail |
| GET | /api/v1/audit/export | tenant_admin | Export audit trail as CSV/PDF |

---

## 8. Data Model

### 8.1 Entity Relationship

```
Tenant
├── User (1:N)
├── Group (1:N)
│   ├── Group Member (1:N)
│   └── Group Hierarchy (self-referential)
└── Audit Log (1:N)
```

### 8.2 Tables

#### users

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| id | UUID | PK | Internal identifier |
| email | string | UNIQUE per tenant, NOT NULL | Primary email |
| display_name | string | NOT NULL | Display name |
| avatar_url | string | | Profile picture URL |
| tenant_id | UUID | FK → tenants, NOT NULL | Belonging tenant |
| auth_provider | enum | NOT NULL | local \| entra_id \| google — primary provider |
| password_hash | string | nullable | Bcrypt hash (local auth only) |
| is_tenant_admin | boolean | DEFAULT false | Tenant admin flag |
| status | enum | NOT NULL, DEFAULT 'active' | active \| suspended \| deactivated |
| last_login_at | timestamptz | | Last successful login |
| created_at | timestamptz | NOT NULL, DEFAULT now() | Creation time |
| updated_at | timestamptz | NOT NULL, DEFAULT now() | Last update |

**Indexes:** `email` (unique per tenant), `tenant_id`, `status`

#### user_identities

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| id | UUID | PK | Internal identifier |
| user_id | UUID | FK → users(id), NOT NULL | Linked user |
| provider | enum | NOT NULL | local \| entra_id \| google |
| provider_subject | string | NOT NULL | OIDC `sub` claim or local email |
| provider_tenant | string | nullable | Entra ID tenant ID (if applicable) |
| email | string | NOT NULL | Email from this provider |
| linked_at | timestamptz | NOT NULL, DEFAULT now() | When identity was linked |
| last_used_at | timestamptz | | Last login via this provider |

**Indexes:** `(provider, provider_subject)` unique, `user_id`, `email`

**Unique constraint:** `(user_id, provider)` — one identity per provider per user

#### groups

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| id | UUID | PK | Internal identifier |
| name | string | NOT NULL | Display name |
| slug | string | NOT NULL | URL-safe identifier |
| description | text | | Group description |
| parent_group_id | UUID | FK → groups(id), nullable | Parent group for hierarchy |
| tenant_id | UUID | FK → tenants, NOT NULL | Belonging tenant |
| visibility | enum | NOT NULL, DEFAULT 'closed' | open \| closed |
| max_members | integer | DEFAULT 0 | Member cap (0 = unlimited) |
| created_by | UUID | FK → users, NOT NULL | Creator |
| status | enum | NOT NULL, DEFAULT 'active' | active \| deleted |
| created_at | timestamptz | NOT NULL, DEFAULT now() | Creation time |
| updated_at | timestamptz | NOT NULL, DEFAULT now() | Last update |

**Indexes:** `slug` (unique per tenant), `tenant_id`, `parent_group_id`

**Unique constraint:** `(slug, tenant_id)`

#### group_members

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| id | UUID | PK | Internal identifier |
| group_id | UUID | FK → groups(id), NOT NULL | Group |
| user_id | UUID | FK → users(id), NOT NULL | User |
| role | enum | NOT NULL, DEFAULT 'member' | admin \| member |
| joined_at | timestamptz | NOT NULL, DEFAULT now() | When user joined |
| joined_via | enum | NOT NULL, DEFAULT 'admin_add' | admin_add \| self_join \| request_approved |

**Indexes:** `(group_id, user_id)` unique, `user_id`

#### join_requests

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| id | UUID | PK | Internal identifier |
| group_id | UUID | FK → groups(id), NOT NULL | Target group |
| user_id | UUID | FK → users(id), NOT NULL | Requesting user |
| status | enum | NOT NULL, DEFAULT 'pending' | pending \| approved \| rejected |
| reviewed_by | UUID | FK → users(id), nullable | Admin who reviewed |
| reviewed_at | timestamptz | | Review timestamp |
| message | text | | Optional message from user |
| created_at | timestamptz | NOT NULL, DEFAULT now() | Request time |

**Indexes:** `(group_id, status)`, `user_id`

#### audit_log

| Column | Type | Constraints | Description |
|--------|------|-------------|-------------|
| id | UUID | PK | Internal identifier |
| tenant_id | UUID | FK → tenants, NOT NULL | Tenant context |
| actor_id | UUID | FK → users(id), NOT NULL | User who performed action |
| action | string | NOT NULL | Action identifier (e.g. "user.suspend", "group.member.add") |
| resource_type | enum | NOT NULL | user \| group \| membership \| permission \| system |
| resource_id | UUID | NOT NULL | ID of affected resource |
| group_id | UUID | FK → groups(id), nullable | Group context (if applicable) |
| detail | jsonb | | Additional context (before/after values, reason) |
| ip_address | string | | Client IP (best effort) |
| user_agent | string | | Client user agent |
| created_at | timestamptz | NOT NULL, DEFAULT now() | Event time |

**Indexes:** `tenant_id`, `actor_id`, `resource_type + resource_id`, `group_id`, `created_at`

**Retention:** Audit logs retained for 7 years (compliance requirement).

---

## 9. Entra ID Integration

### 9.1 Authentication Providers

GoClaw supports multiple authentication providers. Tenant Admin configures which provider(s) are active per tenant.

| Provider | Type | Use Case |
|----------|------|----------|
| **Local** | Email + password (GoClaw-managed) | Development, demo, tenants without corporate IdP |
| **Entra ID** | OIDC (Microsoft) | Enterprise tenants with existing Microsoft ecosystem |
| **Google OAuth2** | OIDC (Gmail / Google Workspace) | Tenants using Google Workspace or Gmail-based auth |
| **(Extensible)** | OIDC/SAML | Future: Okta, Keycloak, custom IdP |

**Multi-provider per tenant:** A tenant can enable multiple providers simultaneously (e.g. Local + Google). User identity is unique per provider — same email across providers links to one User record (see §9.4 Identity Linking).

### 9.2 Authentication Flow

**External provider (Entra ID / Google OAuth2):**

```
User → Browser → GoClaw UI
                    ↓
            Redirect to IdP (Entra ID / Google)
                    ↓
            User authenticates (MFA if configured)
                    ↓
            IdP returns OIDC token
                    ↓
            GoClaw validates token
                    ↓
            Resolve/create User record
                    ↓
            Evaluate permissions
                    ↓
            API access granted
```

**Local provider:**

```
User → GoClaw UI → POST /auth/login { email, password }
                    ↓
            GoClaw validates credentials (bcrypt hash)
                    ↓
            Issue JWT session token
                    ↓
            Resolve User record
                    ↓
            Evaluate permissions
                    ↓
            API access granted
```

### 9.3 Provider Configuration

Per-tenant auth configuration (Tenant Admin only):

```json
{
  "auth": {
    "providers": {
      "local": {
        "enabled": true,
        "password_policy": {
          "min_length": 12,
          "require_uppercase": true,
          "require_number": true,
          "require_special": true
        },
        "mfa_enabled": false
      },
      "entra_id": {
        "enabled": true,
        "client_id": "...",
        "tenant_id": "...",
        "redirect_uri": "https://goclaw.example.com/auth/entra/callback"
      },
      "google": {
        "enabled": true,
        "client_id": "...",
        "client_secret": "...",
        "redirect_uri": "https://goclaw.example.com/auth/google/callback"
      }
    },
    "session": {
      "timeout_minutes": 480,
      "refresh_enabled": true
    }
  }
}
```

### 9.4 Identity Linking

When multiple providers are enabled, the same email address links to a single User record:

- User `john@company.com` logs in via Entra ID → creates User with `entra_id` claim
- Same user later logs in via Google OAuth2 → system finds existing User by email → links Google `sub` claim to same User
- Linked identities stored in `user_identities` table

### 9.5 Token Validation

**External OIDC provider (Entra ID / Google):**

On every API request:

1. Extract Bearer token from `Authorization` header
2. Determine provider from token issuer (`iss` claim)
3. Verify JWT signature using provider's JWKS endpoint
4. Validate claims:
   - `aud` = GoClaw client ID
   - `iss` = provider-specific issuer URL
   - `exp` = not expired
5. Extract user identity from `sub` claim + `email` claim
6. Resolve User record (auto-provision if first login)

**Local provider:**

1. Extract Bearer token from `Authorization` header
2. Verify JWT signature using GoClaw's signing key
3. Validate claims: `sub`, `exp`, `iat`
4. Resolve User record
5. Check user status (active, not suspended)

### 9.6 Local Authentication Details

**Registration (Tenant Admin pre-provisions or self-registration if enabled):**

- Email + password (bcrypt, cost factor 12)
- Optional: email verification flow
- Optional: admin approval for self-registration
- Password policy configurable per tenant

**Login:**

```
POST /auth/login
{
  "email": "user@company.com",
  "password": "..."
}

→ 200: { "access_token": "...", "refresh_token": "...", "expires_in": 28800 }
→ 401: { "error": "invalid_credentials" }
→ 403: { "error": "account_suspended" }
```

**Token refresh:**

```
POST /auth/refresh
{
  "refresh_token": "..."
}

→ 200: { "access_token": "...", "expires_in": 28800 }
→ 401: { "error": "invalid_refresh_token" }
```

**Password management:**

| Endpoint | Description |
|----------|-------------|
| POST /auth/password/change | Change own password (requires current password) |
| POST /auth/password/reset-request | Request reset link via email |
| POST /auth/password/reset | Reset password with token |

**MFA for local auth (Phase 2):**
- TOTP-based (Google Authenticator compatible)
- Setup flow: secret generation → QR code → verification
- Not in initial build — only available via external providers (Entra ID / Google enforce their own MFA)

### 9.7 Token Refresh

- External providers: GoClaw UI handles token refresh via provider's refresh token flow
- Local provider: GoClaw issues its own JWT tokens with configurable expiry
- Session timeout: configurable per tenant (default: 8 hours)
- API does not issue its own tokens for external providers — purely IdP managed

### 9.8 Group Sync from External IdP (Phase 2)

**Future enhancement** — not in initial build:

- Sync Entra ID security groups → GoClaw groups
- Sync Google Workspace groups → GoClaw groups
- Automatic membership mapping
- One-way sync (IdP → GoClaw, not reverse)
- Scheduled poll (every 15 minutes) or webhook-driven

---

## 10. Non-Functional Requirements

### 10.1 Performance

| Metric | Target |
|--------|--------|
| Authentication overhead per API call | < 10ms (cached) / < 100ms (first call) |
| Permission evaluation | < 5ms (cached) / < 50ms (cold) |
| User directory query (1000 users) | < 500ms |
| Group membership lookup | < 20ms |
| Join request submission | < 200ms |

### 10.2 Security

- Local auth: passwords stored as bcrypt hash (cost factor 12), never plaintext
- External auth: no credential storage — Entra ID / Google handles authentication
- MFA enforced via external provider (Entra ID Conditional Access / Google 2SV)
- Local auth MFA: Phase 2 (TOTP-based)
- All API traffic over HTTPS (TLS 1.2+)
- Permission cache invalidated within 5 minutes of role change
- Failed authentication attempts logged and rate-limited (5 attempts/minute per IP)
- API rate limiting: 200 requests/minute per user
- No sensitive data in audit log (passwords, tokens, etc.)

### 10.3 Availability

- External auth: dependent on IdP availability (Entra ID / Google)
- **Grace period:** If external IdP is unreachable, cached tokens valid for up to 1 hour
- Local auth: no external dependency — always available
- Local permission cache survives service restart (Redis or in-memory with TTL)
- Database: PostgreSQL with streaming replication
- **RPO: < 1 hour | RTO: < 2 hours**

### 10.4 Scalability

- 5,000 users per tenant
- 200 groups per tenant
- 20 groups per user
- Permission cache: O(1) lookup per request
- Audit log: append-only, partitioned by month, archived after 1 year

---

## 11. Implementation Roadmap

| # | Phase | Days | Priority | Dependencies |
|---|-------|------|----------|-------------|
| 1 | Data model + migration scripts | 3 | P0 | None |
| 2a | Local auth (email/password, bcrypt, JWT) | 5 | P0 | Phase 1 |
| 2b | Entra ID OIDC integration | 5 | P0 | Phase 1 |
| 2c | Google OAuth2 integration | 3 | P1 | Phase 1 |
| 3 | User auto-provisioning + directory API | 4 | P0 | Phase 1, 2a |
| 4 | Group CRUD + hierarchy API | 5 | P0 | Phase 1, 3 |
| 5 | Membership management (add/remove/role) | 4 | P0 | Phase 4 |
| 6 | Join request workflow | 3 | P1 | Phase 5 |
| 7 | RBAC engine + permission evaluation | 5 | P0 | Phase 1, 3, 4 |
| 8 | Scope-based visibility filter | 3 | P0 | Phase 7 |
| 9 | Audit logging | 3 | P1 | Phase 1, 7 |
| 10 | Admin UI (user/group management) | 6 | P1 | Phase 3, 4, 5 |
| 11 | Permission cache + optimization | 2 | P2 | Phase 7, 8 |

**Total estimated effort: 48 developer-days (24 calendar days with 2 devs)**

### 11.1 Critical Path

```
Phase 1 → Phase 2a (Local Auth) → Phase 3 → Phase 4 → Phase 5 → Phase 7 → Phase 8
Phase 2b (Entra ID) — parallel with Phase 3+, integrates at Phase 3
Phase 2c (Google OAuth2) — can run after Phase 2b or independently
```

Phase 6 (join requests), Phase 9 (audit), and Phase 10 (UI) can run in parallel with Phases 7-8.

### 11.2 Integration with Knowledge Lifecycle Module

This module **must be completed before** Knowledge Lifecycle Module Phase 7 (Publishing Workflow). The dependency chain:

```
User/Group/RBAC Module (complete)
    → Knowledge Lifecycle can use:
        - User identity for artifact ownership
        - Group membership for dept-scoped publish
        - RBAC for approval workflow enforcement
        - Audit trail for compliance logging
```

---

## 12. Acceptance Criteria

1. User authenticates via Entra ID and is auto-provisioned on first login
2. Tenant Admin can create groups with hierarchy (parent/child)
3. Group Admin can add/remove members and approve join requests
4. Member can view resources in their group scope but not other groups
5. Permission evaluation correctly enforces all rules in the permission matrix
6. API request without valid Entra ID token is rejected with 401
7. API request from user without required role is rejected with 403
8. Group hierarchy allows parent group admin to view child group resources
9. Audit log records every membership change, role change, and access decision
10. Permission cache invalidates within 5 minutes of role change
11. System handles 200 concurrent API requests without permission evaluation degradation
12. Tenant Admin can suspend/reactivate/deactivate users with proper audit trail
