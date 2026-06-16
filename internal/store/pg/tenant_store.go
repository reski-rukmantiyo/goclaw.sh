package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/base"
)

// PGTenantStore implements store.TenantStore backed by Postgres.
type PGTenantStore struct {
	db *sql.DB
}

// NewPGTenantStore creates a new PostgreSQL-backed tenant store.
func NewPGTenantStore(db *sql.DB) *PGTenantStore {
	return &PGTenantStore{db: db}
}

// ============================================================
// Tenant CRUD
// ============================================================

func (s *PGTenantStore) CreateTenant(ctx context.Context, tenant *store.TenantData) error {
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
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		tenant.ID, tenant.Name, tenant.Slug, tenant.Status, settings, now, now,
	)
	return err
}

func (s *PGTenantStore) GetTenant(ctx context.Context, id uuid.UUID) (*store.TenantData, error) {
	var d store.TenantData
	err := pkgSqlxDB.GetContext(ctx, &d,
		`SELECT id, name, slug, status, settings, created_at, updated_at
		 FROM tenants WHERE id = $1`, id)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (s *PGTenantStore) GetTenantBySlug(ctx context.Context, slug string) (*store.TenantData, error) {
	var d store.TenantData
	err := pkgSqlxDB.GetContext(ctx, &d,
		`SELECT id, name, slug, status, settings, created_at, updated_at
		 FROM tenants WHERE slug = $1`, slug)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (s *PGTenantStore) ListTenants(ctx context.Context) ([]store.TenantData, error) {
	var tenants []store.TenantData
	err := pkgSqlxDB.SelectContext(ctx, &tenants,
		`SELECT id, name, slug, status, settings, created_at, updated_at
		 FROM tenants ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	return tenants, nil
}

func (s *PGTenantStore) GetTenantsByIDs(ctx context.Context, ids []uuid.UUID) ([]store.TenantData, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	// Chunk by 500 to stay well within PG param limits.
	const chunkSize = 500
	var all []store.TenantData
	for start := 0; start < len(ids); start += chunkSize {
		end := min(start+chunkSize, len(ids))
		var chunk []store.TenantData
		if err := pkgSqlxDB.SelectContext(ctx, &chunk,
			`SELECT id, name, slug, status, settings, created_at, updated_at
			 FROM tenants WHERE id = ANY($1)`,
			pq.Array(ids[start:end])); err != nil {
			return nil, err
		}
		all = append(all, chunk...)
	}
	return all, nil
}

func (s *PGTenantStore) UpdateTenant(ctx context.Context, id uuid.UUID, updates map[string]any) error {
	if _, ok := updates["slug"]; ok {
		return errors.New("slug cannot be modified")
	}
	return execMapUpdate(ctx, s.db, "tenants", id, updates)
}

func (s *PGTenantStore) DeleteTenant(ctx context.Context, id uuid.UUID) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	deleteStmts := []string{
		// Tier 5+ leaf tables
		`DELETE FROM team_task_comments WHERE tenant_id = $1`,
		`DELETE FROM team_task_events WHERE tenant_id = $1`,
		`DELETE FROM team_task_attachments WHERE tenant_id = $1`,
		`DELETE FROM webhook_calls WHERE tenant_id = $1`,
		`DELETE FROM hook_executions WHERE hook_id IN (SELECT id FROM hooks WHERE tenant_id = $1)`,
		`DELETE FROM hook_agents WHERE hook_id IN (SELECT id FROM hooks WHERE tenant_id = $1)`,
		`DELETE FROM agent_workstation_links WHERE tenant_id = $1`,
		`DELETE FROM workstation_permissions WHERE tenant_id = $1`,
		`DELETE FROM workstation_activity WHERE tenant_id = $1`,
		`DELETE FROM workstation_group_permissions WHERE tenant_id = $1`,
		`DELETE FROM memory_chunks WHERE tenant_id = $1`,
		`DELETE FROM kg_relations WHERE tenant_id = $1`,
		`DELETE FROM kg_dedup_candidates WHERE tenant_id = $1`,
		`DELETE FROM vault_links WHERE from_doc_id IN (SELECT id FROM vault_documents WHERE tenant_id = $1)`,
		`DELETE FROM agent_config_permissions WHERE tenant_id = $1`,
		`DELETE FROM agent_context_files WHERE tenant_id = $1`,
		`DELETE FROM user_context_files WHERE tenant_id = $1`,
		`DELETE FROM user_agent_profiles WHERE tenant_id = $1`,
		`DELETE FROM user_agent_overrides WHERE tenant_id = $1`,
		`DELETE FROM agent_shares WHERE tenant_id = $1`,
		`DELETE FROM agent_links WHERE tenant_id = $1`,
		`DELETE FROM episodic_summaries WHERE tenant_id = $1`,
		`DELETE FROM agent_evolution_metrics WHERE tenant_id = $1`,
		`DELETE FROM agent_evolution_suggestions WHERE tenant_id = $1`,
		`DELETE FROM channel_contacts WHERE tenant_id = $1`,
		`DELETE FROM channel_pending_messages WHERE tenant_id = $1`,
		`DELETE FROM pairing_requests WHERE tenant_id = $1`,
		`DELETE FROM paired_devices WHERE tenant_id = $1`,
		`DELETE FROM traces WHERE tenant_id = $1`,
		`DELETE FROM spans WHERE tenant_id = $1`,
		`DELETE FROM activity_logs WHERE tenant_id = $1`,
		`DELETE FROM usage_snapshots WHERE tenant_id = $1`,
		`DELETE FROM embedding_cache WHERE tenant_id = $1`,
		`DELETE FROM listen_raw_messages WHERE tenant_id = $1`,
		`DELETE FROM raw_message_chunks WHERE tenant_id = $1`,
		`DELETE FROM system_configs WHERE tenant_id = $1`,
		`DELETE FROM builtin_tool_tenant_configs WHERE tenant_id = $1`,
		`DELETE FROM skill_tenant_configs WHERE tenant_id = $1`,
		`DELETE FROM subagent_tasks WHERE tenant_id = $1`,
		`DELETE FROM tenant_hook_budget WHERE tenant_id = $1`,
		`DELETE FROM cron_jobs WHERE tenant_id = $1`,
		`DELETE FROM webhooks WHERE tenant_id = $1`,
		`DELETE FROM hooks WHERE tenant_id = $1`,
		// Tier 4
		`DELETE FROM team_tasks WHERE tenant_id = $1`,
		`DELETE FROM team_user_grants WHERE tenant_id = $1`,
		// Tier 3
		`DELETE FROM skill_agent_grants WHERE tenant_id = $1`,
		`DELETE FROM skill_user_grants WHERE tenant_id = $1`,
		`DELETE FROM mcp_agent_grants WHERE tenant_id = $1`,
		`DELETE FROM mcp_user_grants WHERE tenant_id = $1`,
		`DELETE FROM mcp_access_requests WHERE tenant_id = $1`,
		`DELETE FROM mcp_user_credentials WHERE tenant_id = $1`,
		`DELETE FROM secure_cli_agent_grants WHERE tenant_id = $1`,
		`DELETE FROM secure_cli_user_credentials WHERE tenant_id = $1`,
		`DELETE FROM agent_team_members WHERE tenant_id = $1`,
		`DELETE FROM memory_documents WHERE tenant_id = $1`,
		`DELETE FROM kg_entities WHERE tenant_id = $1`,
		`DELETE FROM vault_documents WHERE tenant_id = $1`,
		// Tier 2
		`DELETE FROM tenant_users WHERE tenant_id = $1`,
		`DELETE FROM sessions WHERE tenant_id = $1`,
		`DELETE FROM api_keys WHERE tenant_id = $1`,
		`DELETE FROM config_secrets WHERE tenant_id = $1`,
		`DELETE FROM skills WHERE tenant_id = $1`,
		`DELETE FROM mcp_servers WHERE tenant_id = $1`,
		`DELETE FROM secure_cli_binaries WHERE tenant_id = $1`,
		`DELETE FROM channel_instances WHERE tenant_id = $1`,
		`DELETE FROM agent_teams WHERE tenant_id = $1`,
		`DELETE FROM llm_providers WHERE tenant_id = $1`,
		`DELETE FROM workstations WHERE tenant_id = $1`,
		`DELETE FROM agents WHERE tenant_id = $1`,
		`DELETE FROM tenant_db_connections WHERE tenant_id = $1`,
		// Root
		`DELETE FROM tenants WHERE id = $1`,
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

func (s *PGTenantStore) AddUser(ctx context.Context, tenantID uuid.UUID, userID string, isOwner bool) error {
	normalized, err := base.NormalizeUserID(ctx, s.db, userID)
	if err != nil {
		return fmt.Errorf("normalize user_id: %w", err)
	}
	now := time.Now()
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO tenant_users (id, tenant_id, user_id, is_owner, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (tenant_id, user_id) DO UPDATE SET is_owner = EXCLUDED.is_owner, updated_at = EXCLUDED.updated_at`,
		store.GenNewID(), tenantID, normalized, isOwner, now, now,
	)
	return err
}

func (s *PGTenantStore) GetTenantUser(ctx context.Context, id uuid.UUID) (*store.TenantUserData, error) {
	var d store.TenantUserData
	err := pkgSqlxDB.GetContext(ctx, &d,
		`SELECT id, tenant_id, user_id, display_name, is_owner, metadata, created_at, updated_at
		 FROM tenant_users WHERE id = $1`, id)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (s *PGTenantStore) CreateTenantUserReturning(ctx context.Context, tenantID uuid.UUID, userID, displayName string) (*store.TenantUserData, error) {
	normalized, err := base.NormalizeUserID(ctx, s.db, userID)
	if err != nil {
		return nil, fmt.Errorf("normalize user_id: %w", err)
	}
	now := time.Now()
	var dn *string
	if displayName != "" {
		dn = &displayName
	}
	row := s.db.QueryRowContext(ctx,
		`INSERT INTO tenant_users (id, tenant_id, user_id, display_name, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (tenant_id, user_id) DO UPDATE SET
		   display_name = COALESCE(EXCLUDED.display_name, tenant_users.display_name),
		   updated_at = EXCLUDED.updated_at
		 RETURNING id, tenant_id, user_id, display_name, is_owner, metadata, created_at, updated_at`,
		store.GenNewID(), tenantID, normalized, dn, now, now,
	)
	var d store.TenantUserData
	if err := row.Scan(&d.ID, &d.TenantID, &d.UserID, &d.DisplayName, &d.IsOwner, &d.Metadata, &d.CreatedAt, &d.UpdatedAt); err != nil {
		return nil, err
	}
	return &d, nil
}

func (s *PGTenantStore) RemoveUser(ctx context.Context, tenantID uuid.UUID, userID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM tenant_users WHERE tenant_id = $1 AND user_id = $2`,
		tenantID, userID,
	)
	return err
}

func (s *PGTenantStore) IsOwner(ctx context.Context, tenantID uuid.UUID, userID string) (bool, error) {
	var isOwner bool
	err := s.db.QueryRowContext(ctx,
		`SELECT is_owner FROM tenant_users tu
		 LEFT JOIN users u ON u.email = tu.user_id
		 WHERE tu.tenant_id = $1 AND (tu.user_id = $2 OR u.id::text = $2)
		 LIMIT 1`,
		tenantID, userID,
	).Scan(&isOwner)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return isOwner, err
}

func (s *PGTenantStore) ListUsers(ctx context.Context, tenantID uuid.UUID) ([]store.TenantUserData, error) {
	var result []store.TenantUserData
	err := pkgSqlxDB.SelectContext(ctx, &result,
		`SELECT id, tenant_id, user_id, display_name, is_owner, metadata, created_at, updated_at
		 FROM tenant_users WHERE tenant_id = $1 ORDER BY created_at`, tenantID)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *PGTenantStore) ListUserTenants(ctx context.Context, userID string) ([]store.TenantUserData, error) {
	var result []store.TenantUserData
	err := pkgSqlxDB.SelectContext(ctx, &result,
		`SELECT tu.id, tu.tenant_id, tu.user_id, tu.display_name, tu.is_owner, tu.metadata, tu.created_at, tu.updated_at
		 FROM tenant_users tu
		 LEFT JOIN users u ON u.email = tu.user_id
		 WHERE tu.user_id = $1 OR u.id::text = $1
		 ORDER BY tu.created_at`, userID)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *PGTenantStore) CountOwners(ctx context.Context, tenantID uuid.UUID) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tenant_users WHERE tenant_id = $1 AND is_owner = true`, tenantID,
	).Scan(&count)
	return count, err
}

func (s *PGTenantStore) GetTenantUserByUser(ctx context.Context, tenantID uuid.UUID, userID string) (*store.TenantUserData, error) {
	var d store.TenantUserData
	err := pkgSqlxDB.GetContext(ctx, &d,
		`SELECT id, tenant_id, user_id, display_name, is_owner, metadata, created_at, updated_at
		 FROM tenant_users WHERE tenant_id = $1 AND user_id = $2`, tenantID, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (s *PGTenantStore) UpdateOwnerFlag(ctx context.Context, tenantID uuid.UUID, userID string, isOwner bool) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE tenant_users SET is_owner = $3, updated_at = NOW() WHERE tenant_id = $1 AND user_id = $2`,
		tenantID, userID, isOwner,
	)
	return err
}

func (s *PGTenantStore) ResolveUserTenant(ctx context.Context, userID string) (uuid.UUID, error) {
	var tenantID uuid.UUID
	// Prefer non-Master tenants — Master is a shared system tenant that
	// non-owners cannot scope to.  ORDER BY (tenant_id = master) pushes
	// Master to the end so a user with both Master + real tenants resolves
	// to the real tenant.  Falls back to Master if that's the only membership.
	err := s.db.QueryRowContext(ctx,
		`SELECT tu.tenant_id FROM tenant_users tu
		 LEFT JOIN users u ON u.email = tu.user_id
		 WHERE tu.user_id = $1 OR u.id::text = $1
		 ORDER BY (tu.tenant_id = $2) ASC, tu.created_at ASC LIMIT 1`,
		userID, store.MasterTenantID,
	).Scan(&tenantID)
	if errors.Is(err, sql.ErrNoRows) {
		return store.MasterTenantID, nil
	}
	if err != nil {
		return uuid.Nil, err
	}
	return tenantID, nil
}
