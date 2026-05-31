package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// TenantDBConnectionStore manages tenant database connection credentials.
// All methods operate on the master database.
type TenantDBConnectionStore interface {
	GetByTenantID(ctx context.Context, tenantID uuid.UUID) (*TenantDBConnection, error)
	Create(ctx context.Context, conn *TenantDBConnection) error
	Update(ctx context.Context, conn *TenantDBConnection) error
	Delete(ctx context.Context, tenantID uuid.UUID) error
}

// TenantDBConnection holds the database credentials for a single tenant.
type TenantDBConnection struct {
	TenantID     uuid.UUID `db:"tenant_id"`
	Host         string    `db:"host"`
	Port         int       `db:"port"`
	DatabaseName string    `db:"database_name"`
	Username     string    `db:"username"`
	Password     string    `db:"password"` // encrypted at rest, plaintext in memory
	SSLMode      string    `db:"ssl_mode"`
	CreatedAt    time.Time `db:"created_at"`
	UpdatedAt    time.Time `db:"updated_at"`
}

// DSN builds a PostgreSQL connection string from the connection fields.
func (c *TenantDBConnection) DSN() string {
	sslMode := c.SSLMode
	if sslMode == "" {
		sslMode = "require"
	}
	port := c.Port
	if port == 0 {
		port = 5432
	}
	return fmt.Sprintf("host=%s port=%d dbname=%s user=%s password=%s sslmode=%s",
		c.Host, port, c.DatabaseName, c.Username, c.Password, sslMode,
	)
}
