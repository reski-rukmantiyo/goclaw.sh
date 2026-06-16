package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Group visibility types.
const (
	GroupVisibilityOpen   = "open"
	GroupVisibilityClosed = "closed"
)

// Group status constants.
const (
	GroupStatusActive  = "active"
	GroupStatusDeleted = "deleted"
)

// Group member roles (deprecated — replaced by group roles).
const (
	GroupRoleAdmin  = "admin"
	GroupRoleMember = "member"
)

// Join via constants.
const (
	JoinedViaAdminAdd         = "admin_add"
	JoinedViaSelfJoin         = "self_join"
	JoinedViaRequestApproved  = "request_approved"
)

// Join request status.
const (
	JoinStatusPending  = "pending"
	JoinStatusApproved = "approved"
	JoinStatusRejected = "rejected"
)

// Audit resource types.
const (
	AuditResourceUser       = "user"
	AuditResourceGroup      = "group"
	AuditResourceMembership = "membership"
	AuditResourcePermission = "permission"
	AuditResourceSystem     = "system"
)

// GroupData represents an organizational group.
type GroupData struct {
	ID            uuid.UUID  `json:"id" db:"id"`
	Name          string     `json:"name" db:"name"`
	Slug          string     `json:"slug" db:"slug"`
	Description   *string    `json:"description,omitempty" db:"description"`
	ParentGroupID *uuid.UUID `json:"parent_group_id,omitempty" db:"parent_group_id"`
	TenantID      uuid.UUID  `json:"tenant_id" db:"tenant_id"`
	Visibility    string     `json:"visibility" db:"visibility"`
	MaxMembers    int        `json:"max_members" db:"max_members"`
	CreatedBy     *uuid.UUID `json:"created_by,omitempty" db:"created_by"`
	Status        string     `json:"status" db:"status"`
	CreatedAt     time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at" db:"updated_at"`
}

// GroupMemberData represents a user's membership in a group.
type GroupMemberData struct {
	ID          uuid.UUID `json:"id" db:"id"`
	GroupID     uuid.UUID `json:"group_id" db:"group_id"`
	UserID      uuid.UUID `json:"user_id" db:"user_id"`
	JoinedAt    time.Time `json:"joined_at" db:"joined_at"`
	JoinedVia   string    `json:"joined_via" db:"joined_via"`
	DisplayName *string   `json:"display_name,omitempty" db:"-"`
	Email       *string   `json:"email,omitempty" db:"-"`
}

// JoinRequestData represents a pending group join request.
type JoinRequestData struct {
	ID          uuid.UUID  `json:"id" db:"id"`
	GroupID     uuid.UUID  `json:"group_id" db:"group_id"`
	UserID      uuid.UUID  `json:"user_id" db:"user_id"`
	Status      string     `json:"status" db:"status"`
	ReviewedBy  *uuid.UUID `json:"reviewed_by,omitempty" db:"reviewed_by"`
	ReviewedAt  *time.Time `json:"reviewed_at,omitempty" db:"reviewed_at"`
	Message     *string    `json:"message,omitempty" db:"message"`
	CreatedAt   time.Time  `json:"created_at" db:"created_at"`
}

// AuditLogEntry represents a single audit log record.
type AuditLogEntry struct {
	ID           uuid.UUID  `json:"id" db:"id"`
	TenantID     uuid.UUID  `json:"tenant_id" db:"tenant_id"`
	ActorID      *uuid.UUID `json:"actor_id,omitempty" db:"actor_id"`
	Action       string     `json:"action" db:"action"`
	ResourceType string     `json:"resource_type" db:"resource_type"`
	ResourceID   uuid.UUID  `json:"resource_id" db:"resource_id"`
	GroupID      *uuid.UUID `json:"group_id,omitempty" db:"group_id"`
	Detail       any        `json:"detail,omitempty" db:"detail"`
	IPAddress    *string    `json:"ip_address,omitempty" db:"ip_address"`
	UserAgent    *string    `json:"user_agent,omitempty" db:"user_agent"`
	CreatedAt    time.Time  `json:"created_at" db:"created_at"`
}

// GroupTreeNode represents a group in the hierarchy tree.
type GroupTreeNode struct {
	GroupData
	Children []GroupTreeNode `json:"children,omitempty"`
}

// GroupListParams controls pagination and filtering.
type GroupListParams struct {
	Offset     int
	Limit      int
	Search     string
	ParentID   *uuid.UUID
	Visibility string
	Status     string
}

// GroupListResult contains paginated group results.
type GroupListResult struct {
	Groups []GroupData `json:"groups"`
	Total  int         `json:"total"`
	Offset int         `json:"offset"`
	Limit  int         `json:"limit"`
}

// AuditListParams controls pagination and filtering for audit logs.
type AuditListParams struct {
	Offset            int
	Limit             int
	Action            string
	ResourceType      string
	ResourceID        *uuid.UUID
	GroupID           *uuid.UUID
	ActorID           *uuid.UUID
	FromTime          *time.Time
	ToTime            *time.Time
	ExcludeActorRoles []string // When set, exclude entries from actors with these effective roles
}

// GroupStore manages groups, memberships, and join requests.
type GroupStore interface {
	// Group CRUD
	CreateGroup(ctx context.Context, group *GroupData) error
	GetGroup(ctx context.Context, id uuid.UUID) (*GroupData, error)
	GetGroupBySlug(ctx context.Context, tenantID uuid.UUID, slug string) (*GroupData, error)
	UpdateGroup(ctx context.Context, group *GroupData) error
	DeleteGroup(ctx context.Context, id uuid.UUID) error
	ListGroups(ctx context.Context, tenantID uuid.UUID, params GroupListParams) (*GroupListResult, error)
	GetGroupTree(ctx context.Context, tenantID uuid.UUID) ([]GroupTreeNode, error)
	GetAncestorGroupIDs(ctx context.Context, groupID uuid.UUID) ([]uuid.UUID, error)

	// Membership
	AddMember(ctx context.Context, member *GroupMemberData) error
	RemoveMember(ctx context.Context, groupID, userID uuid.UUID) error
	ListMembers(ctx context.Context, groupID uuid.UUID) ([]GroupMemberData, error)
	GetUserGroups(ctx context.Context, userID uuid.UUID) ([]GroupData, error)
	IsGroupMember(ctx context.Context, groupID, userID uuid.UUID) (bool, error)

	// Join requests
	CreateJoinRequest(ctx context.Context, req *JoinRequestData) error
	ListJoinRequests(ctx context.Context, groupID uuid.UUID, status string) ([]JoinRequestData, error)
	ReviewJoinRequest(ctx context.Context, reqID uuid.UUID, approved bool, reviewedBy uuid.UUID) error
}

// AuditStore manages audit log entries.
type AuditStore interface {
	Log(ctx context.Context, entry *AuditLogEntry) error
	List(ctx context.Context, tenantID uuid.UUID, params AuditListParams) ([]AuditLogEntry, int, error)
}
