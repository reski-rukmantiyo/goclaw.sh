package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Auth provider constants.
const (
	AuthProviderLocal  = "local"
	AuthProviderEntraID = "entra_id"
	AuthProviderGoogle  = "google"
)

// User status constants.
const (
	UserStatusActive      = "active"
	UserStatusSuspended   = "suspended"
	UserStatusDeactivated = "deactivated"
)

// UserData represents an authenticated user.
type UserData struct {
	ID           uuid.UUID  `json:"id" db:"id"`
	Email        string     `json:"email" db:"email"`
	DisplayName  string     `json:"display_name" db:"display_name"`
	AvatarURL    *string    `json:"avatar_url,omitempty" db:"avatar_url"`
	AuthProvider string     `json:"auth_provider" db:"auth_provider"`
	PasswordHash *string    `json:"-" db:"password_hash"`
	Status       string     `json:"status" db:"status"`
	Phone        *string    `json:"phone,omitempty" db:"phone"`
	Timezone     *string    `json:"timezone,omitempty" db:"timezone"`
	LastLoginAt  *time.Time `json:"last_login_at,omitempty" db:"last_login_at"`
	CreatedAt    time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at" db:"updated_at"`
}

// UserIdentity represents a linked auth provider identity.
type UserIdentity struct {
	ID             uuid.UUID  `json:"id" db:"id"`
	UserID         uuid.UUID  `json:"user_id" db:"user_id"`
	Provider       string     `json:"provider" db:"provider"`
	ProviderSubject string    `json:"provider_subject" db:"provider_subject"`
	ProviderTenant *string    `json:"provider_tenant,omitempty" db:"provider_tenant"`
	Email          string     `json:"email" db:"email"`
	LinkedAt       time.Time  `json:"linked_at" db:"linked_at"`
	LastUsedAt     *time.Time `json:"last_used_at,omitempty" db:"last_used_at"`
}

// UserListParams controls pagination and filtering for user listing.
type UserListParams struct {
	Offset  int
	Limit   int
	Search  string
	GroupID *uuid.UUID
	Status  string
	Role    string
}

// UserListResult contains paginated user results.
type UserListResult struct {
	Users      []UserData `json:"users"`
	Total      int        `json:"total"`
	Offset     int        `json:"offset"`
	Limit      int        `json:"limit"`
}

// UserStore manages user identity records.
type UserStore interface {
	// Create inserts a new user record.
	Create(ctx context.Context, user *UserData) error
	// GetByID retrieves a user by internal UUID.
	GetByID(ctx context.Context, id uuid.UUID) (*UserData, error)
	// GetByEmail retrieves a user by email (global lookup, no tenant scoping).
	GetByEmail(ctx context.Context, email string) (*UserData, error)
	// Update updates user fields.
	Update(ctx context.Context, user *UserData) error
	// UpdateStatus changes user status (active/suspended/deactivated).
	UpdateStatus(ctx context.Context, id uuid.UUID, status string) error
	// UpdateLastLogin sets the last_login_at timestamp.
	UpdateLastLogin(ctx context.Context, id uuid.UUID) error
	// List returns a paginated, filterable list of users in a tenant.
	List(ctx context.Context, tenantID uuid.UUID, params UserListParams) (*UserListResult, error)
	// Delete permanently removes a user (hard delete for deactivated users past retention).
	Delete(ctx context.Context, id uuid.UUID) error
	// GetByIDs returns users matching the given UUIDs in a single query.
	GetByIDs(ctx context.Context, ids []uuid.UUID) ([]UserData, error)

	// Identity management
	// CreateIdentity links a new auth provider identity to a user.
	CreateIdentity(ctx context.Context, identity *UserIdentity) error
	// GetIdentityByProviderSubject looks up an identity by (provider, provider_subject).
	GetIdentityByProviderSubject(ctx context.Context, provider, subject string) (*UserIdentity, error)
	// GetIdentities returns all linked identities for a user.
	GetIdentities(ctx context.Context, userID uuid.UUID) ([]UserIdentity, error)
	// UpdateIdentityLastUsed updates the last_used_at for an identity.
	UpdateIdentityLastUsed(ctx context.Context, id uuid.UUID) error

	// Refresh token management
	// StoreRefreshToken stores a new refresh token hash.
	StoreRefreshToken(ctx context.Context, userID uuid.UUID, tokenHash string, deviceInfo string, expiresAt time.Time) error
	// ValidateRefreshToken checks a refresh token hash and returns the user ID.
	ValidateRefreshToken(ctx context.Context, tokenHash string) (uuid.UUID, error)
	// RevokeRefreshToken removes a refresh token.
	RevokeRefreshToken(ctx context.Context, tokenHash string) error
	// RevokeAllRefreshTokens removes all refresh tokens for a user.
	RevokeAllRefreshTokens(ctx context.Context, userID uuid.UUID) error
}
