//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/base"
)

// SQLiteTenantStore implements store.TenantStore backed by SQLite.
type SQLiteTenantStore struct {
	db *sql.DB
}

func NewSQLiteTenantStore(db *sql.DB) *SQLiteTenantStore {
	return &SQLiteTenantStore{db: db}
}

// ============================================================
// Tenant CRUD
// ============================================================

func (s *SQLiteTenantStore) CreateTenant(ctx context.Context, tenant *store.TenantData) error {
	if tenant.ID == uuid.Nil {
		tenant.ID = store.GenNewID()
	}
	now := time.Now()
	tenant.CreatedAt = now
	tenant.UpdatedAt = now

	settings := tenant.Settings
	if len(settings) == 0 {
		settings = json.RawMessage(`{}`)
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO tenants (id, name, slug, status, settings, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		tenant.ID, tenant.Name, tenant.Slug, tenant.Status, settings, now, now,
	)
	return err
}

const tenantSelectCols = `id, name, slug, status, settings, created_at, updated_at`

func (s *SQLiteTenantStore) GetTenant(ctx context.Context, id uuid.UUID) (*store.TenantData, error) {
	var row tenantRow
	err := pkgSqlxDB.GetContext(ctx, &row,
		`SELECT `+tenantSelectCols+` FROM tenants WHERE id = ?`, id)
	if err != nil {
		return nil, err
	}
	d := row.toTenantData()
	return &d, nil
}

func (s *SQLiteTenantStore) GetTenantBySlug(ctx context.Context, slug string) (*store.TenantData, error) {
	var row tenantRow
	err := pkgSqlxDB.GetContext(ctx, &row,
		`SELECT `+tenantSelectCols+` FROM tenants WHERE slug = ?`, slug)
	if err != nil {
		return nil, err
	}
	d := row.toTenantData()
	return &d, nil
}

func (s *SQLiteTenantStore) ListTenants(ctx context.Context) ([]store.TenantData, error) {
	var rows []tenantRow
	err := pkgSqlxDB.SelectContext(ctx, &rows,
		`SELECT `+tenantSelectCols+` FROM tenants ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	result := make([]store.TenantData, 0, len(rows))
	for _, r := range rows {
		result = append(result, r.toTenantData())
	}
	return result, nil
}

func (s *SQLiteTenantStore) GetTenantsByIDs(ctx context.Context, ids []uuid.UUID) ([]store.TenantData, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	const chunkSize = 500
	var all []store.TenantData
	for start := 0; start < len(ids); start += chunkSize {
		end := min(start+chunkSize, len(ids))
		chunk := ids[start:end]
		ph := make([]string, len(chunk))
		args := make([]any, len(chunk))
		for i, id := range chunk {
			ph[i] = "?"
			args[i] = id.String()
		}
		q := `SELECT ` + tenantSelectCols + ` FROM tenants WHERE id IN (` + strings.Join(ph, ",") + `)`
		var rows []tenantRow
		if err := pkgSqlxDB.SelectContext(ctx, &rows, q, args...); err != nil {
			return nil, err
		}
		for _, r := range rows {
			all = append(all, r.toTenantData())
		}
	}
	return all, nil
}

func (s *SQLiteTenantStore) UpdateTenant(ctx context.Context, id uuid.UUID, updates map[string]any) error {
	if _, ok := updates["slug"]; ok {
		return errors.New("slug cannot be modified")
	}
	return execMapUpdate(ctx, s.db, "tenants", id, updates)
}

func (s *SQLiteTenantStore) DeleteTenant(ctx context.Context, id uuid.UUID) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	deleteStmts := []string{
		`DELETE FROM team_task_comments WHERE tenant_id = ?`,
		`DELETE FROM team_task_events WHERE tenant_id = ?`,
		`DELETE FROM team_task_attachments WHERE tenant_id = ?`,
		`DELETE FROM webhook_calls WHERE tenant_id = ?`,
		`DELETE FROM hook_executions WHERE hook_id IN (SELECT id FROM hooks WHERE tenant_id = ?)`,
		`DELETE FROM hook_agents WHERE hook_id IN (SELECT id FROM hooks WHERE tenant_id = ?)`,
		`DELETE FROM agent_workstation_links WHERE tenant_id = ?`,
		`DELETE FROM workstation_permissions WHERE tenant_id = ?`,
		`DELETE FROM workstation_activity WHERE tenant_id = ?`,
		`DELETE FROM workstation_group_permissions WHERE tenant_id = ?`,
		`DELETE FROM memory_chunks WHERE tenant_id = ?`,
		`DELETE FROM kg_relations WHERE tenant_id = ?`,
		`DELETE FROM kg_dedup_candidates WHERE tenant_id = ?`,
		`DELETE FROM vault_links WHERE from_doc_id IN (SELECT id FROM vault_documents WHERE tenant_id = ?)`,
		`DELETE FROM agent_config_permissions WHERE tenant_id = ?`,
		`DELETE FROM agent_context_files WHERE tenant_id = ?`,
		`DELETE FROM user_context_files WHERE tenant_id = ?`,
		`DELETE FROM user_agent_profiles WHERE tenant_id = ?`,
		`DELETE FROM user_agent_overrides WHERE tenant_id = ?`,
		`DELETE FROM agent_shares WHERE tenant_id = ?`,
		`DELETE FROM agent_links WHERE tenant_id = ?`,
		`DELETE FROM episodic_summaries WHERE tenant_id = ?`,
		`DELETE FROM agent_evolution_metrics WHERE tenant_id = ?`,
		`DELETE FROM agent_evolution_suggestions WHERE tenant_id = ?`,
		`DELETE FROM channel_contacts WHERE tenant_id = ?`,
		`DELETE FROM channel_pending_messages WHERE tenant_id = ?`,
		`DELETE FROM pairing_requests WHERE tenant_id = ?`,
		`DELETE FROM paired_devices WHERE tenant_id = ?`,
		`DELETE FROM traces WHERE tenant_id = ?`,
		`DELETE FROM spans WHERE tenant_id = ?`,
		`DELETE FROM activity_logs WHERE tenant_id = ?`,
		`DELETE FROM usage_snapshots WHERE tenant_id = ?`,
		`DELETE FROM embedding_cache WHERE tenant_id = ?`,
		`DELETE FROM listen_raw_messages WHERE tenant_id = ?`,
		`DELETE FROM raw_message_chunks WHERE tenant_id = ?`,
		`DELETE FROM system_configs WHERE tenant_id = ?`,
		`DELETE FROM builtin_tool_tenant_configs WHERE tenant_id = ?`,
		`DELETE FROM skill_tenant_configs WHERE tenant_id = ?`,
		`DELETE FROM subagent_tasks WHERE tenant_id = ?`,
		`DELETE FROM tenant_hook_budget WHERE tenant_id = ?`,
		`DELETE FROM cron_jobs WHERE tenant_id = ?`,
		`DELETE FROM webhooks WHERE tenant_id = ?`,
		`DELETE FROM hooks WHERE tenant_id = ?`,
		`DELETE FROM team_tasks WHERE tenant_id = ?`,
		`DELETE FROM team_user_grants WHERE tenant_id = ?`,
		`DELETE FROM skill_agent_grants WHERE tenant_id = ?`,
		`DELETE FROM skill_user_grants WHERE tenant_id = ?`,
		`DELETE FROM mcp_agent_grants WHERE tenant_id = ?`,
		`DELETE FROM mcp_user_grants WHERE tenant_id = ?`,
		`DELETE FROM mcp_access_requests WHERE tenant_id = ?`,
		`DELETE FROM mcp_user_credentials WHERE tenant_id = ?`,
		`DELETE FROM secure_cli_agent_grants WHERE tenant_id = ?`,
		`DELETE FROM secure_cli_user_credentials WHERE tenant_id = ?`,
		`DELETE FROM agent_team_members WHERE tenant_id = ?`,
		`DELETE FROM memory_documents WHERE tenant_id = ?`,
		`DELETE FROM kg_entities WHERE tenant_id = ?`,
		`DELETE FROM vault_documents WHERE tenant_id = ?`,
		`DELETE FROM tenant_users WHERE tenant_id = ?`,
		`DELETE FROM sessions WHERE tenant_id = ?`,
		`DELETE FROM api_keys WHERE tenant_id = ?`,
		`DELETE FROM config_secrets WHERE tenant_id = ?`,
		`DELETE FROM skills WHERE tenant_id = ?`,
		`DELETE FROM mcp_servers WHERE tenant_id = ?`,
		`DELETE FROM secure_cli_binaries WHERE tenant_id = ?`,
		`DELETE FROM channel_instances WHERE tenant_id = ?`,
		`DELETE FROM agent_teams WHERE tenant_id = ?`,
		`DELETE FROM llm_providers WHERE tenant_id = ?`,
		`DELETE FROM workstations WHERE tenant_id = ?`,
		`DELETE FROM agents WHERE tenant_id = ?`,
		`DELETE FROM tenant_db_connections WHERE tenant_id = ?`,
		`DELETE FROM tenants WHERE id = ?`,
	}

	for _, stmt := range deleteStmts {
		if _, err := tx.ExecContext(ctx, stmt, id); err != nil {
			return fmt.Errorf("delete tenant data: %w", err)
		}
	}

	return tx.Commit()
}

// ============================================================
// Tenant-user membership
// ============================================================

func (s *SQLiteTenantStore) AddUser(ctx context.Context, tenantID uuid.UUID, userID string, isOwner bool) error {
	normalized, err := base.NormalizeUserID(ctx, s.db, userID)
	if err != nil {
		return fmt.Errorf("normalize user_id: %w", err)
	}
	now := time.Now()
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO tenant_users (id, tenant_id, user_id, is_owner, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT (tenant_id, user_id) DO UPDATE SET is_owner = excluded.is_owner, updated_at = excluded.updated_at`,
		store.GenNewID(), tenantID, normalized, isOwner, now, now,
	)
	return err
}

const tenantUserSelectCols = `id, tenant_id, user_id, display_name, is_owner, metadata, created_at, updated_at`

func (s *SQLiteTenantStore) GetTenantUser(ctx context.Context, id uuid.UUID) (*store.TenantUserData, error) {
	var row tenantUserRow
	err := pkgSqlxDB.GetContext(ctx, &row,
		`SELECT `+tenantUserSelectCols+` FROM tenant_users WHERE id = ?`, id)
	if err != nil {
		return nil, err
	}
	d := row.toTenantUserData()
	return &d, nil
}

func (s *SQLiteTenantStore) CreateTenantUserReturning(ctx context.Context, tenantID uuid.UUID, userID, displayName string) (*store.TenantUserData, error) {
	normalized, err := base.NormalizeUserID(ctx, s.db, userID)
	if err != nil {
		return nil, fmt.Errorf("normalize user_id: %w", err)
	}
	now := time.Now()
	var dn *string
	if displayName != "" {
		dn = &displayName
	}
	// SQLite 3.35+ supports RETURNING.
	row := s.db.QueryRowContext(ctx,
		`INSERT INTO tenant_users (id, tenant_id, user_id, display_name, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT (tenant_id, user_id) DO UPDATE SET
		   display_name = COALESCE(excluded.display_name, tenant_users.display_name),
		   updated_at = excluded.updated_at
		 RETURNING id, tenant_id, user_id, display_name, is_owner, metadata, created_at, updated_at`,
		store.GenNewID(), tenantID, normalized, dn, now, now,
	)
	var d store.TenantUserData
	createdAt, updatedAt := scanTimePair()
	if err := row.Scan(&d.ID, &d.TenantID, &d.UserID, &d.DisplayName, &d.IsOwner, &d.Metadata, createdAt, updatedAt); err != nil {
		return nil, err
	}
	d.CreatedAt = createdAt.Time
	d.UpdatedAt = updatedAt.Time
	return &d, nil
}

func (s *SQLiteTenantStore) RemoveUser(ctx context.Context, tenantID uuid.UUID, userID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM tenant_users WHERE tenant_id = ? AND user_id = ?`,
		tenantID, userID,
	)
	return err
}

func (s *SQLiteTenantStore) IsOwner(ctx context.Context, tenantID uuid.UUID, userID string) (bool, error) {
	var isOwner bool
	err := s.db.QueryRowContext(ctx,
		`SELECT is_owner FROM tenant_users tu
		 LEFT JOIN users u ON u.email = tu.user_id
		 WHERE tu.tenant_id = ? AND (tu.user_id = ? OR u.id = ?)
		 LIMIT 1`,
		tenantID, userID, userID,
	).Scan(&isOwner)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return isOwner, err
}

func (s *SQLiteTenantStore) ListUsers(ctx context.Context, tenantID uuid.UUID) ([]store.TenantUserData, error) {
	var rows []tenantUserRow
	err := pkgSqlxDB.SelectContext(ctx, &rows,
		`SELECT `+tenantUserSelectCols+` FROM tenant_users WHERE tenant_id = ? ORDER BY created_at`, tenantID)
	if err != nil {
		return nil, err
	}
	return convertTenantUserRows(rows), nil
}

func (s *SQLiteTenantStore) ListUserTenants(ctx context.Context, userID string) ([]store.TenantUserData, error) {
	var rows []tenantUserRow
	err := pkgSqlxDB.SelectContext(ctx, &rows,
		`SELECT `+tenantUserSelectCols+` FROM tenant_users tu
		 LEFT JOIN users u ON u.email = tu.user_id
		 WHERE tu.user_id = ? OR u.id = ?
		 ORDER BY tu.created_at`, userID, userID)
	if err != nil {
		return nil, err
	}
	return convertTenantUserRows(rows), nil
}

func (s *SQLiteTenantStore) ResolveUserTenant(ctx context.Context, userID string) (uuid.UUID, error) {
	var tenantID uuid.UUID
	err := s.db.QueryRowContext(ctx,
		`SELECT tu.tenant_id FROM tenant_users tu
		 LEFT JOIN users u ON u.email = tu.user_id
		 WHERE tu.user_id = ? OR u.id = ?
		 ORDER BY tu.created_at LIMIT 1`,
		userID, userID,
	).Scan(&tenantID)
	if errors.Is(err, sql.ErrNoRows) {
		return store.MasterTenantID, nil
	}
	if err != nil {
		return uuid.Nil, err
	}
	return tenantID, nil
}

// ============================================================
// Conversion helpers
// ============================================================

func convertTenantUserRows(rows []tenantUserRow) []store.TenantUserData {
	result := make([]store.TenantUserData, 0, len(rows))
	for _, r := range rows {
		result = append(result, r.toTenantUserData())
	}
	return result
}
