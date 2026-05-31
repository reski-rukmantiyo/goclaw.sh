package store

import (
	"context"
	"database/sql"

	"github.com/google/uuid"
)

// TenantDBManager manages per-tenant database connection pools.
type TenantDBManager interface {
	// GetPool returns the tenant's *sql.DB pool, or nil if no tenant_db_connection record exists.
	// Nil means "use master DB" — backward compat for existing tenants.
	GetPool(ctx context.Context, tenantID uuid.UUID) (*sql.DB, error)
	// Invalidate removes the cached pool for a tenant. Call when credentials change.
	Invalidate(tenantID uuid.UUID)
	// Close closes all cached pools. Call on shutdown.
	Close() error
}

// ResolveTenantDB injects the tenant-specific DB into context if available.
// Returns ctx unchanged if tenant has no separate DB (fallback to master).
func ResolveTenantDB(ctx context.Context, mgr TenantDBManager) context.Context {
	if mgr == nil {
		return ctx
	}
	tid := TenantIDFromContext(ctx)
	if tid == uuid.Nil || IsMasterScope(ctx) {
		return ctx
	}
	db, err := mgr.GetPool(ctx, tid)
	if err != nil || db == nil {
		return ctx
	}
	return WithTenantDB(ctx, db)
}
