# Gateway Token Authentication

Gateway token authentication is the simplest auth method in GoClaw. The client sends a pre-shared token (configured server-side) together with a user identifier. The backend validates the token via constant-time comparison and derives the user's role and tenant scope from that combination.

> **Scope**: This document covers gateway token auth only. For JWT (email/password) or API key auth, see separate documentation.

---

## How It Works

### 1. Token Validation

The backend compares the submitted token with `gateway.token` from the config file using `subtle.ConstantTimeCompare` to prevent timing attacks.

**Path**: `internal/gateway/router.go:150`

```go
if configToken != "" && subtle.ConstantTimeCompare([]byte(params.Token), []byte(configToken)) == 1 {
    client.role = permissions.RoleAdmin
    client.authenticated = true
    client.userID = params.UserID
    // ...
}
```

### 2. Role Assignment

Role depends on the `user_id` value submitted by the client and the server's `owner_ids` config:

| Condition | Role |
|-----------|------|
| `user_id` is in `gateway.owner_ids` | `owner` |
| No `owner_ids` configured AND `user_id == "system"` | `owner` (fail-closed default) |
| Otherwise | `admin` |

**Path**: `internal/gateway/router.go:403-413`

```go
func isOwnerID(userID string, ownerIDs []string) bool {
    if userID == "" {
        return false
    }
    if len(ownerIDs) == 0 {
        return userID == "system"
    }
    return slices.Contains(ownerIDs, userID)
}
```

### 3. Tenant Resolution

#### Owner Users

Owners can scope themselves to any tenant by sending a `tenant_id` hint in the connect params. If no hint is provided, they fall back to `MasterTenantID`.

**Path**: `internal/gateway/router.go:157-168`

```go
isOwner := isOwnerID(params.UserID, r.server.cfg.Gateway.OwnerIDs)
if isOwner {
    client.role = permissions.RoleOwner
    tenantScope := params.TenantID
    if tenantScope == "" {
        tenantScope = params.TenantScope // backward compat
    }
    r.applyTenantScope(ctx, client, tenantScope)
    if client.tenantID == uuid.Nil {
        client.tenantID = store.MasterTenantID
    }
}
```

#### Non-Owner Users

Non-owner admin users must be a member of the tenant they request. The backend resolves the tenant hint to a tenant ID and validates membership via the `tenant_users` table.

**Path**: `internal/gateway/router.go:418-442`

```go
func (r *MethodRouter) resolveTenantHint(ctx context.Context, hint, userID string) (uuid.UUID, string) {
    if hint == "" || r.tenantStore == nil {
        return store.MasterTenantID, ""
    }
    t, err := r.tenantStore.GetTenantBySlug(ctx, hint)
    if err != nil || t == nil {
        return store.MasterTenantID, ""
    }
    if userID == "" {
        return uuid.Nil, protocol.ErrTenantAccessRevoked
    }
    role, err := r.getUserTenantRole(ctx, t.ID, userID)
    if err != nil || role == "" {
        return uuid.Nil, protocol.ErrTenantAccessRevoked
    }
    return t.ID, ""
}
```

### 4. Connect Response

After successful authentication, the server returns:

```json
{
  "protocol": 3,
  "role": "owner|admin|operator|viewer",
  "user_id": "string",
  "tenant_id": "uuid",
  "tenant_name": "string",
  "tenant_slug": "string",
  "is_owner": true,
  "is_master_scope": true,
  "edition": "standard|lite",
  "server": {
    "name": "goclaw",
    "version": "1.2.3"
  }
}
```

**Path**: `internal/gateway/router.go:368-401`

---

## Configuration

### Config File

Set `gateway.token` and optionally `gateway.owner_ids` in your JSON5 config:

```json5
{
  gateway: {
    token: "your-secret-gateway-token",
    owner_ids: ["system", "alice", "bob"],
    // ... other gateway settings
  }
}
```

**Config struct**: `internal/config/config_channels.go:420-439`

```go
type GatewayConfig struct {
    Host              string       `json:"host"`
    Port              int          `json:"port"`
    Token             string       `json:"token,omitempty"`       // bearer token
    OwnerIDs          []string     `json:"owner_ids,omitempty"`   // owner identifiers
    AllowedOrigins    []string     `json:"allowed_origins,omitempty"`
    // ...
}
```

### Environment Variable

The config file path is set via `GOCLAW_CONFIG`. Secrets can also be provided via `.env.local` or environment variables that are overlaid onto the config.

**Path**: `internal/config/config.go`

---

## Using the API

### Validate Token (HTTP)

Before attempting a WebSocket connection, the web UI validates the token via a lightweight HTTP call:

```bash
curl -H "Authorization: Bearer <token>" \
     -H "X-GoClaw-User-Id: <user_id>" \
     http://localhost:8080/v1/agents
```

A `401` response means the token is invalid. Any other non-2xx response indicates a server error.

**Path**: `ui/web/src/pages/login/token-form.tsx:33-48`

### WebSocket Connect

Send a `connect` frame as the first message after the WebSocket opens:

```json
{
  "type": "req",
  "id": "connect-1",
  "method": "connect",
  "params": {
    "token": "your-secret-gateway-token",
    "user_id": "system",
    "sender_id": "",
    "locale": "en",
    "tenant_hint": "",
    "tenant_id": "master",
    "protocolVersion": 3
  }
}
```

> **First request must be `connect`**. Any other request before authentication returns `ErrUnauthorized`.

**Path**: `internal/gateway/client.go:140-146`

### Get Current User Profile

```bash
curl -H "Authorization: Bearer <token>" \
     -H "X-GoClaw-User-Id: <user_id>" \
     http://localhost:8080/v1/users/me
```

Returns the user record from the `users` table (for JWT users) or a not-found error (for gateway token users, since no DB user record exists).

**Path**: `internal/http/users.go:40-66`

### List Tenants

```bash
curl -H "Authorization: Bearer <token>" \
     -H "X-GoClaw-User-Id: <user_id>" \
     http://localhost:8080/v1/tenants
```

Requires admin/owner role. Returns all tenants.

**Path**: `internal/http/tenants.go:35`

### Get Tenant Members

```bash
curl -H "Authorization: Bearer <token>" \
     -H "X-GoClaw-User-Id: <user_id>" \
     http://localhost:8080/v1/tenants/{tenant-id}/users
```

Returns the `tenant_users` membership records. Use this to verify whether a non-owner admin user belongs to a specific tenant.

**Path**: `internal/http/tenants.go:39`

---

## Using the UI

### Token Login

1. Open the web UI at `/login`
2. Select **Token** tab (default)
3. Enter **User ID** (default: `system`)
4. Enter **Gateway Token**
5. The UI sends a `GET /v1/agents` request to validate the token before storing it

**Path**: `ui/web/src/pages/login/token-form.tsx`

### View Connection Info

After login, the connection state is visible in the auth store. The topbar shows the current tenant name. The profile page (`/t/{slug}/profile`) displays:

- Auth provider badge
- Tenant name
- Member since date

**Path**: `ui/web/src/pages/profile/profile-page.tsx`

### Check Tenant Scope

The active tenant scope is stored in `localStorage` under `goclaw:tenant_id`. The frontend sends this value as:
- `tenant_id` param in the WS `connect` frame
- `X-GoClaw-Tenant-Id` header in HTTP requests

**Paths**:
- `ui/web/src/api/ws-client.ts:225`
- `ui/web/src/api/http-client.ts:121`

---

## Important Behaviors

### No User Database Record

Gateway token auth does NOT create or validate against the `users` table. The `user_id` is an arbitrary string. This means:

- `GET /v1/users/me` returns `404` for gateway token users (unless a user record was created separately via JWT or pre-provisioning)
- Profile editing is unavailable
- Audit logs show the raw `user_id` string

### Tenant Access for Non-Owners

A non-owner admin user (`role: admin`) must exist in the `tenant_users` table for the requested tenant. If not, the connect request returns `TENANT_ACCESS_REVOKED` and the client is forced to logout.

**Path**: `internal/gateway/router.go:435-440`

### Fail-Closed Default

If no `owner_ids` are configured in the config file, only `user_id == "system"` gets owner privileges. This is a fail-closed design to prevent accidental privilege escalation.

### Token Rotation

To rotate the gateway token:

1. Update `gateway.token` in the config file
2. Restart the server
3. All existing WS connections remain valid (they authenticated already)
4. New connections must use the new token

There is no built-in token expiry or revocation list for gateway tokens.

---

## Troubleshooting

### "First request must be 'connect'"

The WebSocket client must send a `connect` frame before any other RPC. This error means the client sent another method first.

### "tenant access revoked"

For non-owner users, this means the `user_id` is not a member of the requested tenant. Check the `tenant_users` table or verify the `tenant_id` localStorage value.

### "valid token or active pairing required"

The gateway token did not match `gateway.token` in the config, and no valid browser pairing exists. Verify the token value.

### Login loop after page refresh

If you are redirected to `/login` every time you open the app, check:

1. Is the auth token persisted in `localStorage` under `goclaw:auth`?
2. Is `goclaw:tenant_id` set in `localStorage`?
3. Are you navigating to an old flat route (e.g., `/agents`) instead of the tenant-scoped route (`/t/{slug}/agents`)?

See `docs/gateway-token-authentication.md` (this doc) and the session persistence fixes in the routing layer.
