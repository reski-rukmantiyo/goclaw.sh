//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SQLiteRoleStore implements store.RoleStore backed by SQLite.
type SQLiteRoleStore struct {
	db *sql.DB
}

// NewSQLiteRoleStore creates a new SQLite-backed role store.
func NewSQLiteRoleStore(db *sql.DB) *SQLiteRoleStore {
	return &SQLiteRoleStore{db: db}
}

const roleCols = `id, tenant_id, name, description, is_system, permissions, created_at, updated_at`

func scanRole(row interface{ Scan(dest ...any) error }) (*store.RoleData, error) {
	var r store.RoleData
	var desc *string
	var permsRaw string
	createdAt, updatedAt := scanTimePair()
	if err := row.Scan(
		&r.ID, &r.TenantID, &r.Name, &desc, &r.IsSystem, &permsRaw, createdAt, updatedAt); err != nil {
		return nil, err
	}
	r.Description = desc
	r.CreatedAt = createdAt.Time
	r.UpdatedAt = updatedAt.Time
	if permsRaw != "" && permsRaw != "[]" {
		_ = json.Unmarshal([]byte(permsRaw), &r.Permissions)
	}
	if r.Permissions == nil {
		r.Permissions = []string{}
	}
	return &r, nil
}

func (s *SQLiteRoleStore) CreateRole(ctx context.Context, role *store.RoleData) error {
	if role.ID == uuid.Nil {
		role.ID = store.GenNewID()
	}
	now := time.Now().UTC()
	role.CreatedAt = now
	role.UpdatedAt = now

	permsRaw, _ := json.Marshal(role.Permissions)

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO roles (id, tenant_id, name, description, is_system, permissions, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		role.ID, role.TenantID, role.Name, role.Description, role.IsSystem,
		string(permsRaw), now, now,
	)
	return err
}

func (s *SQLiteRoleStore) GetRole(ctx context.Context, id uuid.UUID) (*store.RoleData, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+roleCols+` FROM roles WHERE id = ?`, id)
	r, err := scanRole(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return r, nil
}

func (s *SQLiteRoleStore) GetRoleByName(ctx context.Context, tenantID uuid.UUID, name string) (*store.RoleData, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+roleCols+` FROM roles WHERE tenant_id = ? AND name = ?`, tenantID, name)
	r, err := scanRole(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return r, nil
}

func (s *SQLiteRoleStore) UpdateRole(ctx context.Context, role *store.RoleData) error {
	permsRaw, _ := json.Marshal(role.Permissions)
	_, err := s.db.ExecContext(ctx,
		`UPDATE roles SET name = ?, description = ?, is_system = ?, permissions = ?, updated_at = ? WHERE id = ?`,
		role.Name, role.Description, role.IsSystem, string(permsRaw), time.Now().UTC(), role.ID,
	)
	return err
}

func (s *SQLiteRoleStore) DeleteRole(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM roles WHERE id = ? AND is_system = 0`, id)
	return err
}

func (s *SQLiteRoleStore) ListRoles(ctx context.Context, tenantID uuid.UUID, params store.RoleListParams) (*store.RoleListResult, error) {
	var conditions []string
	var args []any

	conditions = append(conditions, "tenant_id = ?")
	args = append(args, tenantID)

	if params.Search != "" {
		conditions = append(conditions, "name LIKE ?")
		args = append(args, "%"+params.Search+"%")
	}

	where := " WHERE " + strings.Join(conditions, " AND ")

	var total int
	countRow := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM roles`+where, args...)
	if err := countRow.Scan(&total); err != nil {
		return nil, err
	}

	limit := params.Limit
	if limit <= 0 {
		limit = 50
	}
	offset := params.Offset
	if offset < 0 {
		offset = 0
	}

	query := `SELECT ` + roleCols + ` FROM roles` + where + ` ORDER BY created_at DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var roles []store.RoleData
	for rows.Next() {
		r, err := scanRole(rows)
		if err != nil {
			return nil, err
		}
		roles = append(roles, *r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if roles == nil {
		roles = []store.RoleData{}
	}

	return &store.RoleListResult{
		Roles:  roles,
		Total:  total,
		Offset: offset,
		Limit:  limit,
	}, nil
}

func (s *SQLiteRoleStore) SetRolePermissions(ctx context.Context, roleID uuid.UUID, perms []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM role_permissions WHERE role_id = ?`, roleID); err != nil {
		return err
	}
	for _, p := range perms {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO role_permissions (id, role_id, permission) VALUES (?, ?, ?)`,
			store.GenNewID(), roleID, p,
		); err != nil {
			return err
		}
	}
	permsRaw, _ := json.Marshal(perms)
	if _, err := tx.ExecContext(ctx,
		`UPDATE roles SET permissions = ?, updated_at = ? WHERE id = ?`,
		string(permsRaw), time.Now().UTC(), roleID,
	); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteRoleStore) GetRolePermissions(ctx context.Context, roleID uuid.UUID) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT permission FROM role_permissions WHERE role_id = ? ORDER BY permission`, roleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var perms []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		perms = append(perms, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if perms == nil {
		perms = []string{}
	}
	return perms, nil
}

func (s *SQLiteRoleStore) AssignUserRole(ctx context.Context, tenantID uuid.UUID, userID string, roleID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO user_roles (id, tenant_id, user_id, role_id) VALUES (?, ?, ?, ?)`,
		store.GenNewID(), tenantID, userID, roleID,
	)
	return err
}

func (s *SQLiteRoleStore) UnassignUserRole(ctx context.Context, tenantID uuid.UUID, userID string, roleID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM user_roles WHERE tenant_id = ? AND user_id = ? AND role_id = ?`,
		tenantID, userID, roleID,
	)
	return err
}

func (s *SQLiteRoleStore) ListUserRoles(ctx context.Context, tenantID uuid.UUID, userID string) ([]store.RoleData, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT r.`+roleCols+`
		 FROM roles r
		 JOIN user_roles ur ON ur.role_id = r.id
		 WHERE ur.tenant_id = ? AND ur.user_id = ?
		 ORDER BY r.name`,
		tenantID, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var roles []store.RoleData
	for rows.Next() {
		r, err := scanRole(rows)
		if err != nil {
			return nil, err
		}
		roles = append(roles, *r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if roles == nil {
		roles = []store.RoleData{}
	}
	return roles, nil
}

func (s *SQLiteRoleStore) AssignGroupRole(ctx context.Context, groupID, roleID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO group_roles (id, group_id, role_id) VALUES (?, ?, ?)`,
		store.GenNewID(), groupID, roleID,
	)
	return err
}

func (s *SQLiteRoleStore) UnassignGroupRole(ctx context.Context, groupID, roleID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM group_roles WHERE group_id = ? AND role_id = ?`,
		groupID, roleID,
	)
	return err
}

func (s *SQLiteRoleStore) ListGroupRoles(ctx context.Context, groupID uuid.UUID) ([]store.RoleData, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT r.`+roleCols+`
		 FROM roles r
		 JOIN group_roles gr ON gr.role_id = r.id
		 WHERE gr.group_id = ?
		 ORDER BY r.name`,
		groupID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var roles []store.RoleData
	for rows.Next() {
		r, err := scanRole(rows)
		if err != nil {
			return nil, err
		}
		roles = append(roles, *r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if roles == nil {
		roles = []store.RoleData{}
	}
	return roles, nil
}

func (s *SQLiteRoleStore) GetUserEffectivePermissions(ctx context.Context, userID string, tenantID uuid.UUID) ([]string, error) {
	// Union of:
	// 1. Direct role permissions
	// 2. Group role permissions
	// 3. Ancestor group role permissions
	// SQLite recursive CTE for ancestors, then DISTINCT union.
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT rp.permission
		 FROM role_permissions rp
		 WHERE rp.role_id IN (
			-- direct roles
			SELECT role_id FROM user_roles WHERE tenant_id = ? AND user_id = ?
			UNION
			-- group roles
			SELECT gr.role_id
			 FROM group_roles gr
			 JOIN group_members gm ON gm.group_id = gr.group_id
			 WHERE gm.user_id = ?
			UNION
			-- ancestor group roles
			SELECT gr.role_id
			 FROM group_roles gr
			 WHERE gr.group_id IN (
				WITH RECURSIVE ancestors(id) AS (
					SELECT g.parent_group_id
					FROM groups g
					JOIN group_members gm ON gm.group_id = g.id
					WHERE gm.user_id = ?
					UNION ALL
					SELECT g.parent_group_id
					FROM groups g
					JOIN ancestors a ON g.id = a.id
					WHERE g.parent_group_id IS NOT NULL
				)
				SELECT id FROM ancestors WHERE id IS NOT NULL
			)
		 )
		 ORDER BY rp.permission`,
		tenantID, userID, userID, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var perms []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		perms = append(perms, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if perms == nil {
		perms = []string{}
	}
	return perms, nil
}

func (s *SQLiteRoleStore) CountUserRoleAssignments(ctx context.Context, roleID uuid.UUID) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM user_roles WHERE role_id = ?`, roleID).Scan(&count)
	return count, err
}

func (s *SQLiteRoleStore) CountGroupRoleAssignments(ctx context.Context, roleID uuid.UUID) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM group_roles WHERE role_id = ?`, roleID).Scan(&count)
	return count, err
}
