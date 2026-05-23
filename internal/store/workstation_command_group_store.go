package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// WorkstationCommandGroup is a named collection of command patterns that can be
// linked to workstations. tenant_id IS NULL = global built-in; otherwise tenant-scoped.
type WorkstationCommandGroup struct {
	ID          uuid.UUID  `json:"id"`
	TenantID    *uuid.UUID `json:"tenantId"` // nil for global built-ins
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Patterns    []string   `json:"patterns"`
	IsBuiltin   bool       `json:"isBuiltin"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	CreatedBy   string     `json:"createdBy"`
}

// WorkstationCommandGroupStore manages command group definitions.
type WorkstationCommandGroupStore interface {
	// List returns built-in groups (tenant_id IS NULL) plus tenant-scoped custom groups.
	List(ctx context.Context) ([]WorkstationCommandGroup, error)

	// GetByID returns a single group by ID. Built-ins are visible to all tenants.
	GetByID(ctx context.Context, id uuid.UUID) (*WorkstationCommandGroup, error)

	// Create inserts a new tenant-scoped custom group.
	Create(ctx context.Context, group *WorkstationCommandGroup) error

	// Update modifies a tenant-owned group. Built-ins cannot be updated.
	Update(ctx context.Context, id uuid.UUID, updates map[string]any) error

	// Delete removes a tenant-owned group. Built-ins cannot be deleted.
	Delete(ctx context.Context, id uuid.UUID) error
}

// WorkstationGroupPermission links a command group to a workstation.
type WorkstationGroupPermission struct {
	ID            uuid.UUID `json:"id"`
	WorkstationID uuid.UUID `json:"workstationId"`
	GroupID       uuid.UUID `json:"groupId"`
	TenantID      uuid.UUID `json:"tenantId"`
	Enabled       bool      `json:"enabled"`
	CreatedAt     time.Time `json:"createdAt"`
}

// WorkstationGroupPermissionStore manages group-to-workstation links.
type WorkstationGroupPermissionStore interface {
	// ListForWorkstation returns all group links for the given workstation.
	ListForWorkstation(ctx context.Context, workstationID uuid.UUID) ([]WorkstationGroupPermission, error)

	// Add links a group to a workstation. Idempotent on (workstation_id, group_id).
	Add(ctx context.Context, link *WorkstationGroupPermission) error

	// Remove deletes a group link by ID.
	Remove(ctx context.Context, id uuid.UUID) error

	// SetEnabled toggles a group link.
	SetEnabled(ctx context.Context, id uuid.UUID, enabled bool) error
}
