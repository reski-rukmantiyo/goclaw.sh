package pg

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// Compile-time interface check.
var _ store.GroupStore = (*PGGroupStore)(nil)

// PGGroupStore implements store.GroupStore backed by Postgres.
type PGGroupStore struct {
	db *sql.DB
}

// NewPGGroupStore creates a new PGGroupStore.
func NewPGGroupStore(db *sql.DB) *PGGroupStore {
	return &PGGroupStore{db: db}
}

// --- Column constants ---

const groupSelectCols = `id, name, slug, description, parent_group_id, tenant_id, visibility, max_members, created_by, status, created_at, updated_at`

// ============================================================
// Group CRUD
// ============================================================

func (s *PGGroupStore) CreateGroup(ctx context.Context, group *store.GroupData) error {
	if group.ID == uuid.Nil {
		group.ID = store.GenNewID()
	}
	now := time.Now()
	group.CreatedAt = now
	group.UpdatedAt = now

	if group.Slug == "" {
		group.Slug = slugify(group.Name)
	}
	if group.Status == "" {
		group.Status = store.GroupStatusActive
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO groups (id, name, slug, description, parent_group_id, tenant_id, visibility, max_members, created_by, status, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		group.ID, group.Name, group.Slug,
		nilStr(derefStrPtr(group.Description)),
		nilUUID(group.ParentGroupID),
		group.TenantID, group.Visibility, group.MaxMembers,
		group.CreatedBy, group.Status, now, now,
	)
	return err
}

func (s *PGGroupStore) GetGroup(ctx context.Context, id uuid.UUID) (*store.GroupData, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+groupSelectCols+` FROM groups WHERE id = $1`, id)
	g, err := scanGroupRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return g, nil
}

func (s *PGGroupStore) GetGroupBySlug(ctx context.Context, tenantID uuid.UUID, slug string) (*store.GroupData, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+groupSelectCols+` FROM groups WHERE tenant_id = $1 AND slug = $2`, tenantID, slug)
	g, err := scanGroupRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return g, nil
}

func (s *PGGroupStore) UpdateGroup(ctx context.Context, group *store.GroupData) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE groups SET name = $1, description = $2, visibility = $3, max_members = $4, parent_group_id = $5, updated_at = $6
		 WHERE id = $7`,
		group.Name,
		nilStr(derefStrPtr(group.Description)),
		group.Visibility, group.MaxMembers,
		nilUUID(group.ParentGroupID),
		time.Now(), group.ID,
	)
	return err
}

func (s *PGGroupStore) DeleteGroup(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE groups SET status = $1, updated_at = $2 WHERE id = $3`,
		store.GroupStatusDeleted, time.Now(), id,
	)
	return err
}

func (s *PGGroupStore) ListGroups(ctx context.Context, tenantID uuid.UUID, params store.GroupListParams) (*store.GroupListResult, error) {
	var conditions []string
	var args []any
	idx := 1

	conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", idx))
	args = append(args, tenantID)
	idx++

	// Default to active status if not specified.
	status := params.Status
	if status == "" {
		status = store.GroupStatusActive
	}
	conditions = append(conditions, fmt.Sprintf("status = $%d", idx))
	args = append(args, status)
	idx++

	if params.Search != "" {
		conditions = append(conditions, fmt.Sprintf("name ILIKE $%d", idx))
		args = append(args, "%"+params.Search+"%")
		idx++
	}

	if params.ParentID != nil {
		conditions = append(conditions, fmt.Sprintf("parent_group_id = $%d", idx))
		args = append(args, *params.ParentID)
		idx++
	}

	if params.Visibility != "" {
		conditions = append(conditions, fmt.Sprintf("visibility = $%d", idx))
		args = append(args, params.Visibility)
		idx++
	}

	limit := params.Limit
	if limit <= 0 {
		limit = 50
	}
	offset := params.Offset
	if offset < 0 {
		offset = 0
	}

	where := "WHERE " + strings.Join(conditions, " AND ")

	query := fmt.Sprintf(
		`SELECT %s, COUNT(*) OVER() AS total_count
		 FROM groups %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d`,
		groupSelectCols, where, idx, idx+1,
	)
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []store.GroupData
	var total int
	for rows.Next() {
		g, t, err := scanGroupRowWithTotal(rows)
		if err != nil {
			return nil, err
		}
		total = t
		groups = append(groups, *g)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if groups == nil {
		groups = []store.GroupData{}
	}

	return &store.GroupListResult{
		Groups: groups,
		Total:  total,
		Offset: offset,
		Limit:  limit,
	}, nil
}

func (s *PGGroupStore) GetGroupTree(ctx context.Context, tenantID uuid.UUID) ([]store.GroupTreeNode, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+groupSelectCols+` FROM groups WHERE tenant_id = $1 AND status = $2 ORDER BY name`,
		tenantID, store.GroupStatusActive,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Collect all groups.
	var allGroups []store.GroupData
	for rows.Next() {
		g, err := scanGroupRowFromRows(rows)
		if err != nil {
			return nil, err
		}
		allGroups = append(allGroups, *g)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Build a map from group ID to TreeNode.
	nodeMap := make(map[uuid.UUID]*store.GroupTreeNode, len(allGroups))
	for i := range allGroups {
		nodeMap[allGroups[i].ID] = &store.GroupTreeNode{
			GroupData: allGroups[i],
			Children:  []store.GroupTreeNode{},
		}
	}

	// Build tree: attach children to parents, collect roots.
	var roots []store.GroupTreeNode
	for i := range allGroups {
		node := nodeMap[allGroups[i].ID]
		if allGroups[i].ParentGroupID == nil || *allGroups[i].ParentGroupID == uuid.Nil {
			roots = append(roots, *node)
		} else if parent, ok := nodeMap[*allGroups[i].ParentGroupID]; ok {
			parent.Children = append(parent.Children, *node)
		} else {
			// Parent not found (orphaned) — treat as root.
			roots = append(roots, *node)
		}
	}

	if roots == nil {
		roots = []store.GroupTreeNode{}
	}
	return roots, nil
}

func (s *PGGroupStore) GetAncestorGroupIDs(ctx context.Context, groupID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := s.db.QueryContext(ctx,
		`WITH RECURSIVE ancestors AS (
			SELECT id, parent_group_id FROM groups WHERE id = $1
			UNION ALL
			SELECT g.id, g.parent_group_id FROM groups g JOIN ancestors a ON g.id = a.parent_group_id
		) SELECT id FROM ancestors`, groupID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if ids == nil {
		ids = []uuid.UUID{}
	}
	return ids, nil
}

// ============================================================
// Membership
// ============================================================

func (s *PGGroupStore) AddMember(ctx context.Context, member *store.GroupMemberData) error {
	if member.ID == uuid.Nil {
		member.ID = store.GenNewID()
	}
	if member.JoinedAt.IsZero() {
		member.JoinedAt = time.Now()
	}

	// Check max_members constraint.
	var maxMembers int
	var currentCount int
	err := s.db.QueryRowContext(ctx,
		`SELECT max_members, (SELECT COUNT(*) FROM group_members WHERE group_id = $1) FROM groups WHERE id = $1`,
		member.GroupID,
	).Scan(&maxMembers, &currentCount)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("group not found")
		}
		return err
	}
	if maxMembers > 0 && currentCount >= maxMembers {
		return fmt.Errorf("group has reached maximum member limit (%d)", maxMembers)
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO group_members (id, group_id, user_id, role, joined_at, joined_via)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		member.ID, member.GroupID, member.UserID,
		member.Role, member.JoinedAt, member.JoinedVia,
	)
	return err
}

func (s *PGGroupStore) RemoveMember(ctx context.Context, groupID, userID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM group_members WHERE group_id = $1 AND user_id = $2`,
		groupID, userID,
	)
	return err
}

func (s *PGGroupStore) UpdateMemberRole(ctx context.Context, groupID, userID uuid.UUID, role string) error {
	// If demoting from admin, check last-admin constraint.
	if role != store.GroupRoleAdmin {
		var adminCount int
		err := s.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM group_members WHERE group_id = $1 AND role = $2`,
			groupID, store.GroupRoleAdmin,
		).Scan(&adminCount)
		if err != nil {
			return err
		}

		// Check if the target user is currently an admin.
		var currentRole string
		err = s.db.QueryRowContext(ctx,
			`SELECT role FROM group_members WHERE group_id = $1 AND user_id = $2`,
			groupID, userID,
		).Scan(&currentRole)
		if err != nil {
			return err
		}

		if currentRole == store.GroupRoleAdmin && adminCount <= 1 {
			return fmt.Errorf("cannot remove the last admin from the group")
		}
	}

	_, err := s.db.ExecContext(ctx,
		`UPDATE group_members SET role = $1 WHERE group_id = $2 AND user_id = $3`,
		role, groupID, userID,
	)
	return err
}

func (s *PGGroupStore) GetMemberRole(ctx context.Context, groupID, userID uuid.UUID) (string, error) {
	var role string
	err := s.db.QueryRowContext(ctx,
		`SELECT role FROM group_members WHERE group_id = $1 AND user_id = $2`,
		groupID, userID,
	).Scan(&role)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return role, nil
}

func (s *PGGroupStore) ListMembers(ctx context.Context, groupID uuid.UUID) ([]store.GroupMemberData, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT m.id, m.group_id, m.user_id, m.role, m.joined_at, m.joined_via,
		        COALESCE(u.display_name, '') AS display_name
		 FROM group_members m
		 LEFT JOIN users u ON u.id = m.user_id
		 WHERE m.group_id = $1
		 ORDER BY m.joined_at`,
		groupID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var members []store.GroupMemberData
	for rows.Next() {
		var m store.GroupMemberData
		var displayName string
		if err := rows.Scan(
			&m.ID, &m.GroupID, &m.UserID, &m.Role, &m.JoinedAt, &m.JoinedVia,
			&displayName,
		); err != nil {
			return nil, err
		}
		// displayName is available for callers but not part of GroupMemberData struct.
		// Keep it in case the struct is extended; suppress unused warning.
		_ = displayName
		members = append(members, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if members == nil {
		members = []store.GroupMemberData{}
	}
	return members, nil
}

func (s *PGGroupStore) GetUserGroups(ctx context.Context, userID uuid.UUID) ([]store.GroupData, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT g.`+groupSelectCols+`
		 FROM groups g
		 JOIN group_members gm ON gm.group_id = g.id
		 WHERE gm.user_id = $1 AND g.status = $2
		 ORDER BY g.name`,
		userID, store.GroupStatusActive,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []store.GroupData
	for rows.Next() {
		g, err := scanGroupRowFromRows(rows)
		if err != nil {
			return nil, err
		}
		groups = append(groups, *g)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if groups == nil {
		groups = []store.GroupData{}
	}
	return groups, nil
}

func (s *PGGroupStore) GetGroupAdminIDs(ctx context.Context, groupID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT user_id FROM group_members WHERE group_id = $1 AND role = $2`,
		groupID, store.GroupRoleAdmin,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if ids == nil {
		ids = []uuid.UUID{}
	}
	return ids, nil
}

func (s *PGGroupStore) IsGroupAdmin(ctx context.Context, groupID, userID uuid.UUID) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM group_members WHERE group_id = $1 AND user_id = $2 AND role = $3)`,
		groupID, userID, store.GroupRoleAdmin,
	).Scan(&exists)
	return exists, err
}

// ============================================================
// Join requests
// ============================================================

func (s *PGGroupStore) CreateJoinRequest(ctx context.Context, req *store.JoinRequestData) error {
	if req.ID == uuid.Nil {
		req.ID = store.GenNewID()
	}
	if req.Status == "" {
		req.Status = store.JoinStatusPending
	}
	now := time.Now()
	req.CreatedAt = now

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO group_join_requests (id, group_id, user_id, status, reviewed_by, reviewed_at, message, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		req.ID, req.GroupID, req.UserID, req.Status,
		nilUUID(req.ReviewedBy),
		nilTime(req.ReviewedAt),
		nilStr(derefStrPtr(req.Message)),
		now,
	)
	return err
}

func (s *PGGroupStore) ListJoinRequests(ctx context.Context, groupID uuid.UUID, status string) ([]store.JoinRequestData, error) {
	query := `SELECT id, group_id, user_id, status, reviewed_by, reviewed_at, message, created_at
			  FROM group_join_requests WHERE group_id = $1`
	args := []any{groupID}
	idx := 2

	if status != "" {
		query += fmt.Sprintf(" AND status = $%d", idx)
		args = append(args, status)
		idx++
	}
	query += " ORDER BY created_at DESC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var requests []store.JoinRequestData
	for rows.Next() {
		var r store.JoinRequestData
		var reviewedBy sql.NullString
		var reviewedAt sql.NullTime
		var message sql.NullString
		if err := rows.Scan(
			&r.ID, &r.GroupID, &r.UserID, &r.Status,
			&reviewedBy, &reviewedAt, &message, &r.CreatedAt,
		); err != nil {
			return nil, err
		}
		if reviewedBy.Valid {
			id, err := uuid.Parse(reviewedBy.String)
			if err == nil {
				r.ReviewedBy = &id
			}
		}
		if reviewedAt.Valid {
			t := reviewedAt.Time
			r.ReviewedAt = &t
		}
		if message.Valid {
			r.Message = nilStr(message.String)
		}
		requests = append(requests, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if requests == nil {
		requests = []store.JoinRequestData{}
	}
	return requests, nil
}

func (s *PGGroupStore) ReviewJoinRequest(ctx context.Context, reqID uuid.UUID, approved bool, reviewedBy uuid.UUID) error {
	status := store.JoinStatusRejected
	if approved {
		status = store.JoinStatusApproved
	}
	now := time.Now()

	_, err := s.db.ExecContext(ctx,
		`UPDATE group_join_requests SET status = $1, reviewed_by = $2, reviewed_at = $3 WHERE id = $4`,
		status, reviewedBy, now, reqID,
	)
	return err
}

// ============================================================
// Scan helpers
// ============================================================

func scanGroupRow(row *sql.Row) (*store.GroupData, error) {
	var g store.GroupData
	var description sql.NullString
	var parentGroupID uuid.NullUUID
	err := row.Scan(
		&g.ID, &g.Name, &g.Slug, &description, &parentGroupID,
		&g.TenantID, &g.Visibility, &g.MaxMembers, &g.CreatedBy,
		&g.Status, &g.CreatedAt, &g.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if description.Valid {
		g.Description = nilStr(description.String)
	}
	if parentGroupID.Valid {
		g.ParentGroupID = &parentGroupID.UUID
	}
	return &g, nil
}

func scanGroupRowFromRows(rows *sql.Rows) (*store.GroupData, error) {
	var g store.GroupData
	var description sql.NullString
	var parentGroupID uuid.NullUUID
	err := rows.Scan(
		&g.ID, &g.Name, &g.Slug, &description, &parentGroupID,
		&g.TenantID, &g.Visibility, &g.MaxMembers, &g.CreatedBy,
		&g.Status, &g.CreatedAt, &g.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if description.Valid {
		g.Description = nilStr(description.String)
	}
	if parentGroupID.Valid {
		g.ParentGroupID = &parentGroupID.UUID
	}
	return &g, nil
}

func scanGroupRowWithTotal(rows *sql.Rows) (*store.GroupData, int, error) {
	var g store.GroupData
	var total int
	var description sql.NullString
	var parentGroupID uuid.NullUUID
	err := rows.Scan(
		&g.ID, &g.Name, &g.Slug, &description, &parentGroupID,
		&g.TenantID, &g.Visibility, &g.MaxMembers, &g.CreatedBy,
		&g.Status, &g.CreatedAt, &g.UpdatedAt, &total,
	)
	if err != nil {
		return nil, 0, err
	}
	if description.Valid {
		g.Description = nilStr(description.String)
	}
	if parentGroupID.Valid {
		g.ParentGroupID = &parentGroupID.UUID
	}
	return &g, total, nil
}

// ============================================================
// Helpers
// ============================================================

// derefStrPtr returns the string value from a pointer, or "" if nil.
func derefStrPtr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// slugify converts a name into a URL-friendly slug.
func slugify(name string) string {
	s := strings.ToLower(name)
	s = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	return s
}
