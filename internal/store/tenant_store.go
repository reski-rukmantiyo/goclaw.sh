package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// MasterTenantID is the fixed UUID v7 for the default/master tenant.
// All existing data defaults to this tenant during migration.
var MasterTenantID = uuid.MustParse("0193a5b0-7000-7000-8000-000000000001")

// Tenant status constants.
const (
	TenantStatusActive    = "active"
	TenantStatusSuspended = "suspended"
	TenantStatusArchived  = "archived"
)

// Tenant role constants (hierarchy: owner > admin > member > viewer).
const (
	TenantRoleOwner  = "owner"
	TenantRoleAdmin  = "admin"
	TenantRoleMember = "member"
	TenantRoleViewer = "viewer"
)

// TenantData represents a tenant in the database.
type TenantData struct {
	ID        uuid.UUID       `json:"id" db:"id"`
	Name      string          `json:"name" db:"name"`
	Slug      string          `json:"slug" db:"slug"`
	Status    string          `json:"status" db:"status"`
	Settings  json.RawMessage `json:"settings,omitempty" db:"settings"`
	CreatedAt time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt time.Time       `json:"updated_at" db:"updated_at"`
}

// TenantUserData represents a user's membership in a tenant.
type TenantUserData struct {
	ID          uuid.UUID       `json:"id" db:"id"`
	TenantID    uuid.UUID       `json:"tenant_id" db:"tenant_id"`
	UserID      string          `json:"user_id" db:"user_id"`
	DisplayName *string         `json:"display_name,omitempty" db:"display_name"`
	IsOwner     bool            `json:"is_owner" db:"is_owner"`
	Metadata    json.RawMessage `json:"metadata,omitempty" db:"metadata"`
	CreatedAt   time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at" db:"updated_at"`
}

// TenantStore manages tenants and tenant-user membership.
type TenantStore interface {
	// Tenant CRUD
	CreateTenant(ctx context.Context, tenant *TenantData) error
	GetTenant(ctx context.Context, id uuid.UUID) (*TenantData, error)
	GetTenantBySlug(ctx context.Context, slug string) (*TenantData, error)
	ListTenants(ctx context.Context) ([]TenantData, error)
	UpdateTenant(ctx context.Context, id uuid.UUID, updates map[string]any) error

	// DeleteTenant removes a tenant and all its data.
	DeleteTenant(ctx context.Context, id uuid.UUID) error

	// Tenant-user membership
	AddUser(ctx context.Context, tenantID uuid.UUID, userID string, isOwner bool) error
	RemoveUser(ctx context.Context, tenantID uuid.UUID, userID string) error
	IsOwner(ctx context.Context, tenantID uuid.UUID, userID string) (bool, error)
	ListUsers(ctx context.Context, tenantID uuid.UUID) ([]TenantUserData, error)
	ListUserTenants(ctx context.Context, userID string) ([]TenantUserData, error)

	// GetTenantsByIDs returns tenants matching the given UUIDs in a single query.
	GetTenantsByIDs(ctx context.Context, ids []uuid.UUID) ([]TenantData, error)

	// ResolveUserTenant returns the tenant_id for a user.
	// If user belongs to multiple tenants, returns the first (by created_at).
	// If no membership, returns MasterTenantID (backward compat).
	ResolveUserTenant(ctx context.Context, userID string) (uuid.UUID, error)

	// GetTenantUser returns a single tenant_user by primary key.
	GetTenantUser(ctx context.Context, id uuid.UUID) (*TenantUserData, error)

	// CreateTenantUserReturning creates a tenant_user and returns the row.
	// On conflict (tenant_id, user_id), updates display_name and returns existing row.
	CreateTenantUserReturning(ctx context.Context, tenantID uuid.UUID, userID, displayName string) (*TenantUserData, error)

	// CountOwners returns the number of tenant_users with is_owner=true for a tenant.
	CountOwners(ctx context.Context, tenantID uuid.UUID) (int, error)

	// GetTenantUserByUser returns the tenant_user record for a specific (tenantID, userID) pair.
	GetTenantUserByUser(ctx context.Context, tenantID uuid.UUID, userID string) (*TenantUserData, error)

	// UpdateOwnerFlag sets or clears the is_owner flag for a tenant_user membership.
	UpdateOwnerFlag(ctx context.Context, tenantID uuid.UUID, userID string, isOwner bool) error
}
