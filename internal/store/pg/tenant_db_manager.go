package pg

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// PGTenantDBManager implements store.TenantDBManager.
type PGTenantDBManager struct {
	masterDB      *sql.DB
	connStore     store.TenantDBConnectionStore
	encryptionKey string

	mu     sync.RWMutex
	pools  map[string]*sql.DB // tenantID -> *sql.DB
}

// NewPGTenantDBManager creates a new tenant DB manager.
func NewPGTenantDBManager(masterDB *sql.DB, connStore store.TenantDBConnectionStore, encryptionKey string) *PGTenantDBManager {
	return &PGTenantDBManager{
		masterDB:      masterDB,
		connStore:     connStore,
		encryptionKey: encryptionKey,
		pools:         make(map[string]*sql.DB),
	}
}

// GetPool returns a cached *sql.DB for the tenant, opening one if needed.
// Returns (nil, nil) if the tenant has no tenant_db_connection record.
func (m *PGTenantDBManager) GetPool(ctx context.Context, tenantID uuid.UUID) (*sql.DB, error) {
	if tenantID == uuid.Nil {
		return nil, nil
	}
	key := tenantID.String()

	m.mu.RLock()
	if db, ok := m.pools[key]; ok {
		m.mu.RUnlock()
		return db, nil
	}
	m.mu.RUnlock()

	conn, err := m.connStore.GetByTenantID(ctx, tenantID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil // fallback to master DB
		}
		return nil, fmt.Errorf("load tenant_db_connection: %w", err)
	}

	dsn := conn.DSN()
	db, err := OpenDB(dsn)
	if err != nil {
		return nil, fmt.Errorf("open tenant db: %w", err)
	}

	m.mu.Lock()
	// Double-check: another goroutine may have opened the pool while we were waiting.
	if existing, ok := m.pools[key]; ok {
		m.mu.Unlock()
		_ = db.Close()
		return existing, nil
	}
	m.pools[key] = db
	m.mu.Unlock()

	slog.Debug("tenant_db.pool_opened", "tenant_id", tenantID)
	return db, nil
}

// Invalidate closes and removes the cached pool for a tenant.
func (m *PGTenantDBManager) Invalidate(tenantID uuid.UUID) {
	key := tenantID.String()
	m.mu.Lock()
	if db, ok := m.pools[key]; ok {
		_ = db.Close()
		delete(m.pools, key)
		slog.Debug("tenant_db.pool_invalidated", "tenant_id", tenantID)
	}
	m.mu.Unlock()
}

// AllPools returns a snapshot of all cached tenant DB pools.
func (m *PGTenantDBManager) AllPools() []*sql.DB {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*sql.DB, 0, len(m.pools))
	for _, db := range m.pools {
		result = append(result, db)
	}
	return result
}

// Close closes all cached pools.
func (m *PGTenantDBManager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var firstErr error
	for key, db := range m.pools {
		if err := db.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		delete(m.pools, key)
	}
	return firstErr
}
