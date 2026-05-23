package pg

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

// PGWorkstationCommandGroupStore implements store.WorkstationCommandGroupStore.
type PGWorkstationCommandGroupStore struct {
	db *sql.DB
}

// NewPGWorkstationCommandGroupStore creates a PGWorkstationCommandGroupStore.
func NewPGWorkstationCommandGroupStore(db *sql.DB) *PGWorkstationCommandGroupStore {
	return &PGWorkstationCommandGroupStore{db: db}
}

const cgSelectCols = `id, tenant_id, name, description, patterns, is_builtin, created_at, updated_at, created_by`

func (s *PGWorkstationCommandGroupStore) List(ctx context.Context) ([]store.WorkstationCommandGroup, error) {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+cgSelectCols+` FROM workstation_command_groups
		 WHERE tenant_id = $1 OR tenant_id IS NULL
		 ORDER BY is_builtin DESC, name`,
		tid)
	if err != nil {
		return nil, fmt.Errorf("workstation_command_groups list: %w", err)
	}
	defer rows.Close()
	return scanCGRows(rows)
}

func (s *PGWorkstationCommandGroupStore) GetByID(ctx context.Context, id uuid.UUID) (*store.WorkstationCommandGroup, error) {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return nil, sql.ErrNoRows
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT `+cgSelectCols+` FROM workstation_command_groups
		 WHERE id = $1 AND (tenant_id = $2 OR tenant_id IS NULL)`,
		id, tid)
	g, err := scanCGRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, sql.ErrNoRows
		}
		return nil, fmt.Errorf("workstation_command_groups get: %w", err)
	}
	return &g, nil
}

func (s *PGWorkstationCommandGroupStore) Create(ctx context.Context, group *store.WorkstationCommandGroup) error {
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
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		group.ID, tid, group.Name, group.Description, patternsJSON,
		group.IsBuiltin, group.CreatedAt, group.UpdatedAt, group.CreatedBy,
	)
	if err != nil {
		return fmt.Errorf("workstation_command_groups create: %w", err)
	}
	return nil
}

func (s *PGWorkstationCommandGroupStore) Update(ctx context.Context, id uuid.UUID, updates map[string]any) error {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("tenant_id required")
	}
	// Verify the group is tenant-owned (not built-in) before updating.
	var isBuiltin bool
	err := s.db.QueryRowContext(ctx,
		`SELECT is_builtin FROM workstation_command_groups WHERE id = $1 AND (tenant_id = $2 OR tenant_id IS NULL)`,
		id, tid).Scan(&isBuiltin)
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
	setMap["updated_at"] = time.Now()
	if err := execMapUpdateWhereTenant(ctx, s.db, "workstation_command_groups", setMap, id, tid); err != nil {
		return fmt.Errorf("workstation_command_groups update: %w", err)
	}
	return nil
}

func (s *PGWorkstationCommandGroupStore) Delete(ctx context.Context, id uuid.UUID) error {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("tenant_id required")
	}
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM workstation_command_groups WHERE id = $1 AND tenant_id = $2 AND is_builtin = FALSE`,
		id, tid)
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

// PGWorkstationGroupPermissionStore implements store.WorkstationGroupPermissionStore.
type PGWorkstationGroupPermissionStore struct {
	db *sql.DB
}

// NewPGWorkstationGroupPermissionStore creates a PGWorkstationGroupPermissionStore.
func NewPGWorkstationGroupPermissionStore(db *sql.DB) *PGWorkstationGroupPermissionStore {
	return &PGWorkstationGroupPermissionStore{db: db}
}

const gpSelectCols = `id, workstation_id, group_id, tenant_id, enabled, created_at`

func (s *PGWorkstationGroupPermissionStore) ListForWorkstation(ctx context.Context, workstationID uuid.UUID) ([]store.WorkstationGroupPermission, error) {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+gpSelectCols+` FROM workstation_group_permissions
		 WHERE workstation_id = $1 AND tenant_id = $2
		 ORDER BY created_at`,
		workstationID, tid)
	if err != nil {
		return nil, fmt.Errorf("workstation_group_permissions list: %w", err)
	}
	defer rows.Close()
	return scanGPRows(rows)
}

func (s *PGWorkstationGroupPermissionStore) Add(ctx context.Context, link *store.WorkstationGroupPermission) error {
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
		 VALUES ($1,$2,$3,$4,$5,$6)
		 ON CONFLICT (workstation_id, group_id) DO NOTHING`,
		link.ID, link.WorkstationID, link.GroupID, tid, link.Enabled, link.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("workstation_group_permissions add: %w", err)
	}
	return nil
}

func (s *PGWorkstationGroupPermissionStore) Remove(ctx context.Context, id uuid.UUID) error {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("tenant_id required")
	}
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM workstation_group_permissions WHERE id = $1 AND tenant_id = $2`,
		id, tid)
	if err != nil {
		return fmt.Errorf("workstation_group_permissions remove: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *PGWorkstationGroupPermissionStore) SetEnabled(ctx context.Context, id uuid.UUID, enabled bool) error {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("tenant_id required")
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE workstation_group_permissions SET enabled = $1 WHERE id = $2 AND tenant_id = $3`,
		enabled, id, tid)
	return err
}

// --- scanning helpers ---

func scanCGRows(rows *sql.Rows) ([]store.WorkstationCommandGroup, error) {
	var result []store.WorkstationCommandGroup
	for rows.Next() {
		g, err := scanCGRow(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, g)
	}
	return result, rows.Err()
}

func scanCGRow(s interface{ Scan(...any) error }) (store.WorkstationCommandGroup, error) {
	var g store.WorkstationCommandGroup
	var tenantIDStr *string
	var patternsRaw []byte
	err := s.Scan(&g.ID, &tenantIDStr, &g.Name, &g.Description, &patternsRaw, &g.IsBuiltin, &g.CreatedAt, &g.UpdatedAt, &g.CreatedBy)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return g, fmt.Errorf("scan workstation_command_group: %w", err)
	}
	if tenantIDStr != nil {
		if tid, err := uuid.Parse(*tenantIDStr); err == nil {
			g.TenantID = &tid
		}
	}
	if len(patternsRaw) > 0 {
		_ = json.Unmarshal(patternsRaw, &g.Patterns)
	}
	return g, err
}

func scanGPRows(rows *sql.Rows) ([]store.WorkstationGroupPermission, error) {
	var result []store.WorkstationGroupPermission
	for rows.Next() {
		p, err := scanGPRow(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

func scanGPRow(s interface{ Scan(...any) error }) (store.WorkstationGroupPermission, error) {
	var p store.WorkstationGroupPermission
	err := s.Scan(&p.ID, &p.WorkstationID, &p.GroupID, &p.TenantID, &p.Enabled, &p.CreatedAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return p, fmt.Errorf("scan workstation_group_permission: %w", err)
	}
	return p, err
}
