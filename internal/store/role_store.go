package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// RoleData represents a tenant-scoped role definition.
type RoleData struct {
	ID          uuid.UUID `json:"id" db:"id"`
	TenantID    uuid.UUID `json:"tenant_id" db:"tenant_id"`
	Name        string    `json:"name" db:"name"`
	Description *string   `json:"description,omitempty" db:"description"`
	IsSystem    bool      `json:"is_system" db:"is_system"`
	Permissions []string  `json:"permissions" db:"-"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}

// RoleListParams controls pagination and filtering for roles.
type RoleListParams struct {
	Offset int
	Limit  int
	Search string
}

// RoleListResult contains paginated role results.
type RoleListResult struct {
	Roles  []RoleData `json:"roles"`
	Total  int        `json:"total"`
	Offset int        `json:"offset"`
	Limit  int        `json:"limit"`
}

// RoleStore manages tenant-scoped roles and their assignments.
type RoleStore interface {
	// Role CRUD
	CreateRole(ctx context.Context, role *RoleData) error
	GetRole(ctx context.Context, id uuid.UUID) (*RoleData, error)
	GetRoleByName(ctx context.Context, tenantID uuid.UUID, name string) (*RoleData, error)
	UpdateRole(ctx context.Context, role *RoleData) error
	DeleteRole(ctx context.Context, id uuid.UUID) error
	ListRoles(ctx context.Context, tenantID uuid.UUID, params RoleListParams) (*RoleListResult, error)

	// Permissions
	SetRolePermissions(ctx context.Context, roleID uuid.UUID, perms []string) error
	GetRolePermissions(ctx context.Context, roleID uuid.UUID) ([]string, error)

	// User assignments
	AssignUserRole(ctx context.Context, tenantID uuid.UUID, userID string, roleID uuid.UUID) error
	UnassignUserRole(ctx context.Context, tenantID uuid.UUID, userID string, roleID uuid.UUID) error
	ListUserRoles(ctx context.Context, tenantID uuid.UUID, userID string) ([]RoleData, error)

	// Group assignments
	AssignGroupRole(ctx context.Context, groupID, roleID uuid.UUID) error
	UnassignGroupRole(ctx context.Context, groupID, roleID uuid.UUID) error
	ListGroupRoles(ctx context.Context, groupID uuid.UUID) ([]RoleData, error)

	// Effective permission resolution
	GetUserEffectivePermissions(ctx context.Context, userID string, tenantID uuid.UUID) ([]string, error)

	// Assignment counts (for deletion guards)
	CountUserRoleAssignments(ctx context.Context, roleID uuid.UUID) (int, error)
	CountGroupRoleAssignments(ctx context.Context, roleID uuid.UUID) (int, error)
}
