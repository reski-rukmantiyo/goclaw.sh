package pg

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// PGTenantDBConnectionStore implements store.TenantDBConnectionStore.
type PGTenantDBConnectionStore struct {
	db            *sql.DB
	encryptionKey string
}

// NewPGTenantDBConnectionStore creates a new PostgreSQL-backed tenant DB connection store.
func NewPGTenantDBConnectionStore(db *sql.DB, encryptionKey string) *PGTenantDBConnectionStore {
	return &PGTenantDBConnectionStore{db: db, encryptionKey: encryptionKey}
}

func (s *PGTenantDBConnectionStore) GetByTenantID(ctx context.Context, tenantID uuid.UUID) (*store.TenantDBConnection, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT tenant_id, host, port, database_name, username, password, ssl_mode, created_at, updated_at
		 FROM tenant_db_connections WHERE tenant_id = $1`,
		tenantID,
	)
	var conn store.TenantDBConnection
	var encryptedPassword string
	err := row.Scan(&conn.TenantID, &conn.Host, &conn.Port, &conn.DatabaseName, &conn.Username, &encryptedPassword, &conn.SSLMode, &conn.CreatedAt, &conn.UpdatedAt)
	if err != nil {
		return nil, err
	}
	password, err := crypto.Decrypt(encryptedPassword, s.encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("decrypt tenant_db password: %w", err)
	}
	conn.Password = password
	return &conn, nil
}

func (s *PGTenantDBConnectionStore) Create(ctx context.Context, conn *store.TenantDBConnection) error {
	encryptedPassword, err := crypto.Encrypt(conn.Password, s.encryptionKey)
	if err != nil {
		return fmt.Errorf("encrypt tenant_db password: %w", err)
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO tenant_db_connections (tenant_id, host, port, database_name, username, password, ssl_mode, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		conn.TenantID, conn.Host, conn.Port, conn.DatabaseName, conn.Username, encryptedPassword, conn.SSLMode, conn.CreatedAt, conn.UpdatedAt,
	)
	return err
}

func (s *PGTenantDBConnectionStore) Update(ctx context.Context, conn *store.TenantDBConnection) error {
	encryptedPassword, err := crypto.Encrypt(conn.Password, s.encryptionKey)
	if err != nil {
		return fmt.Errorf("encrypt tenant_db password: %w", err)
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE tenant_db_connections
		 SET host = $1, port = $2, database_name = $3, username = $4, password = $5, ssl_mode = $6, updated_at = $7
		 WHERE tenant_id = $8`,
		conn.Host, conn.Port, conn.DatabaseName, conn.Username, encryptedPassword, conn.SSLMode, conn.UpdatedAt, conn.TenantID,
	)
	return err
}

func (s *PGTenantDBConnectionStore) Delete(ctx context.Context, tenantID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM tenant_db_connections WHERE tenant_id = $1`,
		tenantID,
	)
	return err
}
