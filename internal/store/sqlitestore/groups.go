//go:build sqlite || sqliteonly

package sqlitestore

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
var _ store.GroupStore = (*SQLiteGroupStore)(nil)

// SQLiteGroupStore implements store.GroupStore backed by SQLite.
type SQLiteGroupStore struct {
	db *sql.DB
}

// NewSQLiteGroupStore creates a new SQLite-backed group store.
func NewSQLiteGroupStore(db *sql.DB) *SQLiteGroupStore {
	return &SQLiteGroupStore{db: db}
}

// ============================================================
// Group CRUD
// ============================================================

const groupCols = `id, name, slug, description, parent_group_id, tenant_id, visibility, max_members, created_by, status, created_at, updated_at`

func scanGroup(row interface{ Scan(dest ...any) error }) (*store.GroupData, error) {
	var g store.GroupData
	var desc, parentID *string
	createdAt, updatedAt := scanTimePair()
	if err := row.Scan(
		&g.ID, &g.Name, &g.Slug, &desc, &parentID,
		&g.TenantID, &g.Visibility, &g.MaxMembers,
		&g.CreatedBy, &g.Status, createdAt, updatedAt,
	); err != nil {
		return nil, err
	}
	g.CreatedAt = createdAt.Time
	g.UpdatedAt = updatedAt.Time
	if desc != nil {
		g.Description = desc
	}
	if parentID != nil {
		pid, err := uuid.Parse(*parentID)
		if err == nil {
			g.ParentGroupID = &pid
		}
	}
	return &g, nil
}

func (s *SQLiteGroupStore) CreateGroup(ctx context.Context, group *store.GroupData) error {
	if group.ID == uuid.Nil {
		group.ID = store.GenNewID()
	}
	if group.Slug == "" {
		group.Slug = slugify(group.Name)
	}
	now := time.Now().UTC()
	group.CreatedAt = now
	group.UpdatedAt = now
	if group.Status == "" {
		group.Status = store.GroupStatusActive
	}
	if group.Visibility == "" {
		group.Visibility = store.GroupVisibilityClosed
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO groups (id, name, slug, description, parent_group_id, tenant_id, visibility, max_members, created_by, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		group.ID, group.Name, group.Slug, group.Description, nilUUID(group.ParentGroupID),
		group.TenantID, group.Visibility, group.MaxMembers,
		group.CreatedBy, group.Status, group.CreatedAt, group.UpdatedAt,
	)
	return err
}

func (s *SQLiteGroupStore) GetGroup(ctx context.Context, id uuid.UUID) (*store.GroupData, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+groupCols+` FROM groups WHERE id = ? AND status = ?`, id, store.GroupStatusActive)
	g, err := scanGroup(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return g, err
}

func (s *SQLiteGroupStore) GetGroupBySlug(ctx context.Context, tenantID uuid.UUID, slug string) (*store.GroupData, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+groupCols+` FROM groups WHERE tenant_id = ? AND slug = ? AND status = ?`,
		tenantID, slug, store.GroupStatusActive)
	g, err := scanGroup(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return g, err
}

func (s *SQLiteGroupStore) UpdateGroup(ctx context.Context, group *store.GroupData) error {
	now := time.Now().UTC()
	group.UpdatedAt = now

	_, err := s.db.ExecContext(ctx,
		`UPDATE groups SET name = ?, slug = ?, description = ?, parent_group_id = ?, visibility = ?, max_members = ?, status = ?, updated_at = ?
		 WHERE id = ?`,
		group.Name, group.Slug, group.Description, nilUUID(group.ParentGroupID),
		group.Visibility, group.MaxMembers, group.Status, now,
		group.ID,
	)
	return err
}

func (s *SQLiteGroupStore) DeleteGroup(ctx context.Context, id uuid.UUID) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx,
		`UPDATE groups SET status = ?, updated_at = ? WHERE id = ?`,
		store.GroupStatusDeleted, now, id,
	)
	return err
}

func (s *SQLiteGroupStore) ListGroups(ctx context.Context, tenantID uuid.UUID, params store.GroupListParams) (*store.GroupListResult, error) {
	// Build dynamic WHERE clause.
	var conditions []string
	var args []any

	conditions = append(conditions, "tenant_id = ?")
	args = append(args, tenantID)

	conditions = append(conditions, "status = ?")
	args = append(args, store.GroupStatusActive)

	if params.Search != "" {
		conditions = append(conditions, "name LIKE ?")
		args = append(args, "%"+params.Search+"%")
	}
	if params.ParentID != nil {
		conditions = append(conditions, "parent_group_id = ?")
		args = append(args, *params.ParentID)
	}
	if params.Visibility != "" {
		conditions = append(conditions, "visibility = ?")
		args = append(args, params.Visibility)
	}

	where := " WHERE " + strings.Join(conditions, " AND ")

	// Count query.
	var total int
	countArgs := make([]any, len(args))
	copy(countArgs, args)
	countRow := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM groups`+where, countArgs...)
	if err := countRow.Scan(&total); err != nil {
		return nil, err
	}

	// Data query.
	limit := params.Limit
	if limit <= 0 {
		limit = 50
	}
	offset := params.Offset
	if offset < 0 {
		offset = 0
	}

	query := `SELECT ` + groupCols + ` FROM groups` + where + ` ORDER BY created_at DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []store.GroupData
	for rows.Next() {
		g, err := scanGroup(rows)
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

	return &store.GroupListResult{
		Groups: groups,
		Total:  total,
		Offset: offset,
		Limit:  limit,
	}, nil
}

func (s *SQLiteGroupStore) GetGroupTree(ctx context.Context, tenantID uuid.UUID) ([]store.GroupTreeNode, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+groupCols+` FROM groups WHERE tenant_id = ? AND status = ? ORDER BY name`,
		tenantID, store.GroupStatusActive)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Load all groups.
	var allGroups []store.GroupData
	for rows.Next() {
		g, err := scanGroup(rows)
		if err != nil {
			return nil, err
		}
		allGroups = append(allGroups, *g)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Build lookup and tree.
	nodeMap := make(map[uuid.UUID]*store.GroupTreeNode, len(allGroups))
	for i := range allGroups {
		nodeMap[allGroups[i].ID] = &store.GroupTreeNode{GroupData: allGroups[i]}
	}

	var roots []store.GroupTreeNode
	for _, g := range allGroups {
		node := nodeMap[g.ID]
		if g.ParentGroupID == nil {
			roots = append(roots, *node)
		} else if parent, ok := nodeMap[*g.ParentGroupID]; ok {
			parent.Children = append(parent.Children, *node)
		} else {
			// Orphan — treat as root.
			roots = append(roots, *node)
		}
	}

	return roots, nil
}

func (s *SQLiteGroupStore) GetAncestorGroupIDs(ctx context.Context, groupID uuid.UUID) ([]uuid.UUID, error) {
	// Load all active groups to walk parent chain in Go.
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, parent_group_id FROM groups WHERE status = ?`, store.GroupStatusActive)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	parentMap := make(map[uuid.UUID]*uuid.UUID)
	for rows.Next() {
		var id string
		var parentID *string
		if err := rows.Scan(&id, &parentID); err != nil {
			return nil, err
		}
		uid, err := uuid.Parse(id)
		if err != nil {
			continue
		}
		if parentID != nil {
			pid, err := uuid.Parse(*parentID)
			if err == nil {
				parentMap[uid] = &pid
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Walk parent chain from groupID.
	var ancestors []uuid.UUID
	visited := make(map[uuid.UUID]bool)
	current := groupID
	for {
		parent, ok := parentMap[current]
		if !ok || parent == nil {
			break
		}
		if visited[*parent] {
			break // cycle guard
		}
		visited[*parent] = true
		ancestors = append(ancestors, *parent)
		current = *parent
	}
	return ancestors, nil
}

// ============================================================
// Membership
// ============================================================

const memberCols = `id, group_id, user_id, role, joined_at, joined_via`

func scanMember(row interface{ Scan(dest ...any) error }) (*store.GroupMemberData, error) {
	var m store.GroupMemberData
	joinedAt := &sqliteTime{}
	if err := row.Scan(&m.ID, &m.GroupID, &m.UserID, &m.Role, joinedAt, &m.JoinedVia); err != nil {
		return nil, err
	}
	m.JoinedAt = joinedAt.Time
	return &m, nil
}

func (s *SQLiteGroupStore) AddMember(ctx context.Context, member *store.GroupMemberData) error {
	if member.ID == uuid.Nil {
		member.ID = store.GenNewID()
	}
	now := time.Now().UTC()
	member.JoinedAt = now

	// Check max_members if group enforces a limit.
	if member.GroupID != uuid.Nil {
		var maxMembers int
		var currentMembers int
		err := s.db.QueryRowContext(ctx,
			`SELECT max_members FROM groups WHERE id = ?`, member.GroupID,
		).Scan(&maxMembers)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("check max_members: %w", err)
		}
		if maxMembers > 0 {
			err := s.db.QueryRowContext(ctx,
				`SELECT COUNT(*) FROM group_members WHERE group_id = ?`, member.GroupID,
			).Scan(&currentMembers)
			if err != nil {
				return fmt.Errorf("count members: %w", err)
			}
			if currentMembers >= maxMembers {
				return fmt.Errorf("group %s has reached max_members limit (%d)", member.GroupID, maxMembers)
			}
		}
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO group_members (id, group_id, user_id, role, joined_at, joined_via)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT (group_id, user_id) DO UPDATE SET role = excluded.role, joined_via = excluded.joined_via`,
		member.ID, member.GroupID, member.UserID, member.Role, now, member.JoinedVia,
	)
	return err
}

func (s *SQLiteGroupStore) RemoveMember(ctx context.Context, groupID, userID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM group_members WHERE group_id = ? AND user_id = ?`,
		groupID, userID,
	)
	return err
}

func (s *SQLiteGroupStore) UpdateMemberRole(ctx context.Context, groupID, userID uuid.UUID, role string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE group_members SET role = ? WHERE group_id = ? AND user_id = ?`,
		role, groupID, userID,
	)
	return err
}

func (s *SQLiteGroupStore) GetMemberRole(ctx context.Context, groupID, userID uuid.UUID) (string, error) {
	var role string
	err := s.db.QueryRowContext(ctx,
		`SELECT role FROM group_members WHERE group_id = ? AND user_id = ?`,
		groupID, userID,
	).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return role, err
}

func (s *SQLiteGroupStore) ListMembers(ctx context.Context, groupID uuid.UUID) ([]store.GroupMemberData, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT m.id, m.group_id, m.user_id, m.role, m.joined_at, m.joined_via,
		        COALESCE(u.display_name, '') AS display_name
		 FROM group_members m
		 LEFT JOIN users u ON u.id = m.user_id
		 WHERE m.group_id = ?
		 ORDER BY m.joined_at`,
		groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var members []store.GroupMemberData
	for rows.Next() {
		m, err := scanMember(rows)
		if err != nil {
			return nil, err
		}
		members = append(members, *m)
	}
	return members, rows.Err()
}

func (s *SQLiteGroupStore) GetUserGroups(ctx context.Context, userID uuid.UUID) ([]store.GroupData, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT g.`+groupCols+`
		 FROM groups g
		 JOIN group_members gm ON gm.group_id = g.id
		 WHERE gm.user_id = ? AND g.status = ?
		 ORDER BY g.name`,
		userID, store.GroupStatusActive)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []store.GroupData
	for rows.Next() {
		g, err := scanGroup(rows)
		if err != nil {
			return nil, err
		}
		groups = append(groups, *g)
	}
	return groups, rows.Err()
}

func (s *SQLiteGroupStore) GetGroupAdminIDs(ctx context.Context, groupID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT user_id FROM group_members WHERE group_id = ? AND role = ?`,
		groupID, store.GroupRoleAdmin)
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
	return ids, rows.Err()
}

func (s *SQLiteGroupStore) IsGroupAdmin(ctx context.Context, groupID, userID uuid.UUID) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM group_members WHERE group_id = ? AND user_id = ? AND role = ?`,
		groupID, userID, store.GroupRoleAdmin,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// ============================================================
// Join requests
// ============================================================

const joinReqCols = `id, group_id, user_id, status, reviewed_by, reviewed_at, message, created_at`

func scanJoinRequest(row interface{ Scan(dest ...any) error }) (*store.JoinRequestData, error) {
	var r store.JoinRequestData
	var reviewedBy *string
	reviewedAt := &nullSqliteTime{}
	createdAt := &sqliteTime{}
	if err := row.Scan(
		&r.ID, &r.GroupID, &r.UserID, &r.Status,
		&reviewedBy, reviewedAt, &r.Message, createdAt,
	); err != nil {
		return nil, err
	}
	r.CreatedAt = createdAt.Time
	if reviewedBy != nil {
		rb, err := uuid.Parse(*reviewedBy)
		if err == nil {
			r.ReviewedBy = &rb
		}
	}
	if reviewedAt.Valid {
		r.ReviewedAt = &reviewedAt.Time
	}
	return &r, nil
}

func (s *SQLiteGroupStore) CreateJoinRequest(ctx context.Context, req *store.JoinRequestData) error {
	if req.ID == uuid.Nil {
		req.ID = store.GenNewID()
	}
	now := time.Now().UTC()
	req.CreatedAt = now
	if req.Status == "" {
		req.Status = store.JoinStatusPending
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO join_requests (id, group_id, user_id, status, message, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		req.ID, req.GroupID, req.UserID, req.Status, req.Message, now,
	)
	return err
}

func (s *SQLiteGroupStore) ListJoinRequests(ctx context.Context, groupID uuid.UUID, status string) ([]store.JoinRequestData, error) {
	query := `SELECT ` + joinReqCols + ` FROM join_requests WHERE group_id = ?`
	args := []any{groupID}

	if status != "" {
		query += ` AND status = ?`
		args = append(args, status)
	}
	query += ` ORDER BY created_at DESC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reqs []store.JoinRequestData
	for rows.Next() {
		r, err := scanJoinRequest(rows)
		if err != nil {
			return nil, err
		}
		reqs = append(reqs, *r)
	}
	return reqs, rows.Err()
}

func (s *SQLiteGroupStore) ReviewJoinRequest(ctx context.Context, reqID uuid.UUID, approved bool, reviewedBy uuid.UUID) error {
	status := store.JoinStatusRejected
	if approved {
		status = store.JoinStatusApproved
	}
	now := time.Now().UTC()

	_, err := s.db.ExecContext(ctx,
		`UPDATE join_requests SET status = ?, reviewed_by = ?, reviewed_at = ? WHERE id = ?`,
		status, reviewedBy, now, reqID,
	)
	return err
}

// ============================================================
// Helpers
// ============================================================

// slugify converts a name to a URL-friendly slug.
var nonAlphaNum = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = nonAlphaNum.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 100 {
		s = s[:100]
	}
	return s
}
