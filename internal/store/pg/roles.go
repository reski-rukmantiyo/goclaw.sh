package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// PGRoleStore implements store.RoleStore backed by Postgres.
type PGRoleStore struct {
	db *sql.DB
}

// NewPGRoleStore creates a new PostgreSQL-backed role store.
func NewPGRoleStore(db *sql.DB) *PGRoleStore {
	return &PGRoleStore{db: db}
}

const roleSelectCols = `id, tenant_id, name, description, is_system, permissions, created_at, updated_at`

func scanRole(row interface{ Scan(dest ...any) error }) (*store.RoleData, error) {
	var r store.RoleData
	var desc *string
	var permsRaw []byte
	if err := row.Scan(&r.ID, &r.TenantID, &r.Name, &desc, &r.IsSystem, &permsRaw, &r.CreatedAt, &r.UpdatedAt); err != nil {
		return nil, err
	}
	r.Description = desc
	if len(permsRaw) > 0 {
		_ = json.Unmarshal(permsRaw, &r.Permissions)
	}
	if r.Permissions == nil {
		r.Permissions = []string{}
	}
	return &r, nil
}

func (s *PGRoleStore) CreateRole(ctx context.Context, role *store.RoleData) error {
	if role.ID == uuid.Nil {
		role.ID = store.GenNewID()
	}
	now := time.Now()
	role.CreatedAt = now
	role.UpdatedAt = now

	permsJSON, _ := json.Marshal(role.Permissions)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO roles (id, tenant_id, name, description, is_system, permissions, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		role.ID, role.TenantID, role.Name, role.Description, role.IsSystem,
		permsJSON, now, now,
	)
	return err
}

func (s *PGRoleStore) GetRole(ctx context.Context, id uuid.UUID) (*store.RoleData, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+roleSelectCols+` FROM roles WHERE id = $1`, id)
	r, err := scanRole(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return r, nil
}

func (s *PGRoleStore) GetRoleByName(ctx context.Context, tenantID uuid.UUID, name string) (*store.RoleData, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+roleSelectCols+` FROM roles WHERE tenant_id = $1 AND name = $2`, tenantID, name)
	r, err := scanRole(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return r, nil
}

func (s *PGRoleStore) UpdateRole(ctx context.Context, role *store.RoleData) error {
	permsJSON, _ := json.Marshal(role.Permissions)
	_, err := s.db.ExecContext(ctx,
		`UPDATE roles SET name = $1, description = $2, is_system = $3, permissions = $4, updated_at = $5 WHERE id = $6`,
		role.Name, role.Description, role.IsSystem, permsJSON, time.Now(), role.ID,
	)
	return err
}

func (s *PGRoleStore) DeleteRole(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM roles WHERE id = $1 AND is_system = false`, id)
	return err
}

func (s *PGRoleStore) ListRoles(ctx context.Context, tenantID uuid.UUID, params store.RoleListParams) (*store.RoleListResult, error) {
	var conditions []string
	var args []any
	idx := 1

	conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", idx))
	args = append(args, tenantID)
	idx++

	if params.Search != "" {
		conditions = append(conditions, fmt.Sprintf("name ILIKE $%d", idx))
		args = append(args, "%"+params.Search+"%")
		idx++
	}

	limit := params.Limit
	if limit <= 0 {
		limit = 50
	}
	offset := max(params.Offset, 0)

	where := "WHERE " + strings.Join(conditions, " AND ")

	query := fmt.Sprintf(
		`SELECT %s, COUNT(*) OVER() AS total_count
		 FROM roles %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d`,
		roleSelectCols, where, idx, idx+1,
	)
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var roles []store.RoleData
	var total int
	for rows.Next() {
		var r store.RoleData
		var desc *string
		var permsRaw []byte
		var t int
		if err := rows.Scan(&r.ID, &r.TenantID, &r.Name, &desc, &r.IsSystem, &permsRaw, &r.CreatedAt, &r.UpdatedAt, &t); err != nil {
			return nil, err
		}
		total = t
		r.Description = desc
		if len(permsRaw) > 0 {
			_ = json.Unmarshal(permsRaw, &r.Permissions)
		}
		if r.Permissions == nil {
			r.Permissions = []string{}
		}
		roles = append(roles, r)
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

func (s *PGRoleStore) SetRolePermissions(ctx context.Context, roleID uuid.UUID, perms []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM role_permissions WHERE role_id = $1`, roleID); err != nil {
		return err
	}
	for _, p := range perms {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO role_permissions (id, role_id, permission) VALUES ($1, $2, $3)`,
			store.GenNewID(), roleID, p,
		); err != nil {
			return err
		}
	}
	permsJSON, _ := json.Marshal(perms)
	if _, err := tx.ExecContext(ctx,
		`UPDATE roles SET permissions = $1, updated_at = $2 WHERE id = $3`,
		permsJSON, time.Now(), roleID,
	); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *PGRoleStore) GetRolePermissions(ctx context.Context, roleID uuid.UUID) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT permission FROM role_permissions WHERE role_id = $1 ORDER BY permission`, roleID)
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

func (s *PGRoleStore) AssignUserRole(ctx context.Context, tenantID uuid.UUID, userID string, roleID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO user_roles (id, tenant_id, user_id, role_id) VALUES ($1, $2, $3, $4)`,
		store.GenNewID(), tenantID, userID, roleID,
	)
	return err
}

func (s *PGRoleStore) UnassignUserRole(ctx context.Context, tenantID uuid.UUID, userID string, roleID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM user_roles WHERE tenant_id = $1 AND user_id = $2 AND role_id = $3`,
		tenantID, userID, roleID,
	)
	return err
}

func (s *PGRoleStore) ListUserRoles(ctx context.Context, tenantID uuid.UUID, userID string) ([]store.RoleData, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT r.`+roleSelectCols+`
		 FROM roles r
		 JOIN user_roles ur ON ur.role_id = r.id
		 WHERE ur.tenant_id = $1 AND ur.user_id = $2
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

func (s *PGRoleStore) AssignGroupRole(ctx context.Context, groupID, roleID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO group_roles (id, group_id, role_id) VALUES ($1, $2, $3)`,
		store.GenNewID(), groupID, roleID,
	)
	return err
}

func (s *PGRoleStore) UnassignGroupRole(ctx context.Context, groupID, roleID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM group_roles WHERE group_id = $1 AND role_id = $2`,
		groupID, roleID,
	)
	return err
}

func (s *PGRoleStore) ListGroupRoles(ctx context.Context, groupID uuid.UUID) ([]store.RoleData, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT r.`+roleSelectCols+`
		 FROM roles r
		 JOIN group_roles gr ON gr.role_id = r.id
		 WHERE gr.group_id = $1
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

func (s *PGRoleStore) GetUserEffectivePermissions(ctx context.Context, userID string, tenantID uuid.UUID) ([]string, error) {
	// group_members.user_id is UUID in PG, while user_roles.user_id is VARCHAR.
	// Passing userID as uuid.UUID satisfies both: VARCHAR = UUID works (implicit cast),
	// but UUID = TEXT does not.
	uid, err := uuid.Parse(userID)
	if err != nil {
		return nil, err
	}

	// Union of:
	// 1. Direct role permissions
	// 2. Group role permissions (all groups user belongs to)
	// 3. Ancestor group role permissions
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT rp.permission
		 FROM role_permissions rp
		 WHERE rp.role_id IN (
			-- direct roles
			SELECT role_id FROM user_roles WHERE tenant_id = $1 AND user_id = $2
			UNION
			-- group roles
			SELECT gr.role_id
			 FROM group_roles gr
			 JOIN group_members gm ON gm.group_id = gr.group_id
			 WHERE gm.user_id = $2
			UNION
			-- ancestor group roles
			SELECT gr.role_id
			 FROM group_roles gr
			 WHERE gr.group_id IN (
				WITH RECURSIVE ancestors AS (
					SELECT g.parent_group_id
					FROM groups g
					JOIN group_members gm ON gm.group_id = g.id
					WHERE gm.user_id = $2
					UNION ALL
					SELECT g.parent_group_id
					FROM groups g
					JOIN ancestors a ON g.id = a.parent_group_id
				)
				SELECT parent_group_id FROM ancestors WHERE parent_group_id IS NOT NULL
			)
		 )
		 ORDER BY rp.permission`,
		tenantID, uid,
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

func (s *PGRoleStore) CountUserRoleAssignments(ctx context.Context, roleID uuid.UUID) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM user_roles WHERE role_id = $1`, roleID).Scan(&count)
	return count, err
}

func (s *PGRoleStore) CountGroupRoleAssignments(ctx context.Context, roleID uuid.UUID) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM group_roles WHERE role_id = $1`, roleID).Scan(&count)
	return count, err
}
