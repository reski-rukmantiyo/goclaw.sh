//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SQLiteWorkstationCommandGroupStore implements store.WorkstationCommandGroupStore.
type SQLiteWorkstationCommandGroupStore struct {
	db *sql.DB
}

// NewSQLiteWorkstationCommandGroupStore creates a SQLiteWorkstationCommandGroupStore.
func NewSQLiteWorkstationCommandGroupStore(db *sql.DB) *SQLiteWorkstationCommandGroupStore {
	return &SQLiteWorkstationCommandGroupStore{db: db}
}

const cgSelectColsSQLite = `id, tenant_id, name, description, patterns, is_builtin, created_at, updated_at, created_by`

func (s *SQLiteWorkstationCommandGroupStore) List(ctx context.Context) ([]store.WorkstationCommandGroup, error) {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+cgSelectColsSQLite+` FROM workstation_command_groups
		 WHERE tenant_id = ? OR tenant_id IS NULL
		 ORDER BY is_builtin DESC, name`,
		tid.String())
	if err != nil {
		return nil, fmt.Errorf("workstation_command_groups list: %w", err)
	}
	defer rows.Close()
	return scanCGRowsSQLite(rows)
}

func (s *SQLiteWorkstationCommandGroupStore) GetByID(ctx context.Context, id uuid.UUID) (*store.WorkstationCommandGroup, error) {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return nil, sql.ErrNoRows
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT `+cgSelectColsSQLite+` FROM workstation_command_groups
		 WHERE id = ? AND (tenant_id = ? OR tenant_id IS NULL)`,
		id.String(), tid.String())
	g, err := scanCGRowSQLite(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, sql.ErrNoRows
		}
		return nil, fmt.Errorf("workstation_command_groups get: %w", err)
	}
	return &g, nil
}

func (s *SQLiteWorkstationCommandGroupStore) Create(ctx context.Context, group *store.WorkstationCommandGroup) error {
	if group.ID == uuid.Nil {
		group.ID = store.GenNewID()
	}
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("tenant_id required")
	}
	group.TenantID = &tid
	group.IsBuiltin = false
	if group.CreatedAt.IsZero() {
		group.CreatedAt = time.Now()
	}
	if group.UpdatedAt.IsZero() {
		group.UpdatedAt = group.CreatedAt
	}
	patternsJSON, err := json.Marshal(group.Patterns)
	if err != nil {
		return fmt.Errorf("marshal patterns: %w", err)
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO workstation_command_groups
		 (id, tenant_id, name, description, patterns, is_builtin, created_at, updated_at, created_by)
		 VALUES (?,?,?,?,?,?,?,?,?)`,
		group.ID.String(), tid.String(), group.Name, group.Description, patternsJSON,
		boolToInt(group.IsBuiltin), group.CreatedAt.Format(time.RFC3339Nano), group.UpdatedAt.Format(time.RFC3339Nano), group.CreatedBy,
	)
	if err != nil {
		return fmt.Errorf("workstation_command_groups create: %w", err)
	}
	return nil
}

func (s *SQLiteWorkstationCommandGroupStore) Update(ctx context.Context, id uuid.UUID, updates map[string]any) error {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("tenant_id required")
	}
	var isBuiltin bool
	err := s.db.QueryRowContext(ctx,
		`SELECT is_builtin FROM workstation_command_groups WHERE id = ? AND (tenant_id = ? OR tenant_id IS NULL)`,
		id.String(), tid.String()).Scan(&isBuiltin)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return sql.ErrNoRows
		}
		return fmt.Errorf("workstation_command_groups update check: %w", err)
	}
	if isBuiltin {
		return fmt.Errorf("cannot update built-in command group")
	}

	setMap := make(map[string]any)
	for k, v := range updates {
		switch k {
		case "name":
			setMap["name"] = v
		case "description":
			setMap["description"] = v
		case "patterns":
			if patterns, ok := v.([]string); ok {
				b, err := json.Marshal(patterns)
				if err != nil {
					return fmt.Errorf("marshal patterns: %w", err)
				}
				setMap["patterns"] = b
			}
		}
	}
	setMap["updated_at"] = time.Now().Format(time.RFC3339Nano)
	if err := execMapUpdateWhereTenant(ctx, s.db, "workstation_command_groups", setMap, id, tid); err != nil {
		return fmt.Errorf("workstation_command_groups update: %w", err)
	}
	return nil
}

func (s *SQLiteWorkstationCommandGroupStore) Delete(ctx context.Context, id uuid.UUID) error {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("tenant_id required")
	}
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM workstation_command_groups WHERE id = ? AND tenant_id = ? AND is_builtin = FALSE`,
		id.String(), tid.String())
	if err != nil {
		return fmt.Errorf("workstation_command_groups delete: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// --- WorkstationGroupPermissionStore ---

// SQLiteWorkstationGroupPermissionStore implements store.WorkstationGroupPermissionStore.
type SQLiteWorkstationGroupPermissionStore struct {
	db *sql.DB
}

// NewSQLiteWorkstationGroupPermissionStore creates a SQLiteWorkstationGroupPermissionStore.
func NewSQLiteWorkstationGroupPermissionStore(db *sql.DB) *SQLiteWorkstationGroupPermissionStore {
	return &SQLiteWorkstationGroupPermissionStore{db: db}
}

const gpSelectColsSQLite = `id, workstation_id, group_id, tenant_id, enabled, created_at`

func (s *SQLiteWorkstationGroupPermissionStore) ListForWorkstation(ctx context.Context, workstationID uuid.UUID) ([]store.WorkstationGroupPermission, error) {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+gpSelectColsSQLite+` FROM workstation_group_permissions
		 WHERE workstation_id = ? AND tenant_id = ?
		 ORDER BY created_at`,
		workstationID.String(), tid.String())
	if err != nil {
		return nil, fmt.Errorf("workstation_group_permissions list: %w", err)
	}
	defer rows.Close()
	return scanGPRowsSQLite(rows)
}

func (s *SQLiteWorkstationGroupPermissionStore) Add(ctx context.Context, link *store.WorkstationGroupPermission) error {
	if link.ID == uuid.Nil {
		link.ID = store.GenNewID()
	}
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("tenant_id required")
	}
	link.TenantID = tid
	if link.CreatedAt.IsZero() {
		link.CreatedAt = time.Now()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO workstation_group_permissions
		 (id, workstation_id, group_id, tenant_id, enabled, created_at)
		 VALUES (?,?,?,?,?,?)
		 ON CONFLICT (workstation_id, group_id) DO NOTHING`,
		link.ID.String(), link.WorkstationID.String(), link.GroupID.String(), tid.String(),
		boolToInt(link.Enabled), link.CreatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("workstation_group_permissions add: %w", err)
	}
	return nil
}

func (s *SQLiteWorkstationGroupPermissionStore) Remove(ctx context.Context, id uuid.UUID) error {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("tenant_id required")
	}
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM workstation_group_permissions WHERE id = ? AND tenant_id = ?`,
		id.String(), tid.String())
	if err != nil {
		return fmt.Errorf("workstation_group_permissions remove: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *SQLiteWorkstationGroupPermissionStore) SetEnabled(ctx context.Context, id uuid.UUID, enabled bool) error {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("tenant_id required")
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE workstation_group_permissions SET enabled = ? WHERE id = ? AND tenant_id = ?`,
		boolToInt(enabled), id.String(), tid.String())
	return err
}

// --- scanning helpers ---

func scanCGRowsSQLite(rows *sql.Rows) ([]store.WorkstationCommandGroup, error) {
	var result []store.WorkstationCommandGroup
	for rows.Next() {
		g, err := scanCGRowSQLite(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, g)
	}
	return result, rows.Err()
}

func scanCGRowSQLite(s interface{ Scan(...any) error }) (store.WorkstationCommandGroup, error) {
	var g store.WorkstationCommandGroup
	var tenantIDStr sql.NullString
	var patternsRaw []byte
	var isBuiltinInt int
	var createdAtStr, updatedAtStr string
	err := s.Scan(&g.ID, &tenantIDStr, &g.Name, &g.Description, &patternsRaw, &isBuiltinInt, &createdAtStr, &updatedAtStr, &g.CreatedBy)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return g, fmt.Errorf("scan workstation_command_group: %w", err)
	}
	if tenantIDStr.Valid {
		if tid, err := uuid.Parse(tenantIDStr.String); err == nil {
			g.TenantID = &tid
		}
	}
	g.IsBuiltin = isBuiltinInt == 1
	if len(patternsRaw) > 0 {
		_ = json.Unmarshal(patternsRaw, &g.Patterns)
	}
	g.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAtStr)
	g.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAtStr)
	return g, err
}

func scanGPRowsSQLite(rows *sql.Rows) ([]store.WorkstationGroupPermission, error) {
	var result []store.WorkstationGroupPermission
	for rows.Next() {
		p, err := scanGPRowSQLite(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

func scanGPRowSQLite(s interface{ Scan(...any) error }) (store.WorkstationGroupPermission, error) {
	var p store.WorkstationGroupPermission
	var enabledInt int
	var createdAtStr string
	err := s.Scan(&p.ID, &p.WorkstationID, &p.GroupID, &p.TenantID, &enabledInt, &createdAtStr)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return p, fmt.Errorf("scan workstation_group_permission: %w", err)
	}
	p.Enabled = enabledInt == 1
	p.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAtStr)
	return p, err
}
