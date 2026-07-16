package store

import (
	"time"

	"github.com/google/uuid"
)

// BaseModel provides common fields for all database models.
type BaseModel struct {
	ID        uuid.UUID `json:"id" db:"id"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

// GenNewID generates a new UUID v7 (time-ordered).
func GenNewID() uuid.UUID {
	return uuid.Must(uuid.NewV7())
}

// StoreConfig configures the store layer.
type StoreConfig struct {
	// PostgresDSN is the Postgres connection string (required for postgres backend).
	PostgresDSN string

	// SQLitePath is the path to the SQLite database file (required for sqlite backend).
	SQLitePath string

	// StorageBackend selects the database backend: "postgres" (default) or "sqlite".
	StorageBackend string

	// SkillsStorageDir is the directory for skill file content (default: dataDir/skills-store/).
	SkillsStorageDir string

	// Workspace is the default agent workspace path.
	Workspace string

	// GlobalSkillsDir is the global skills directory (e.g. ~/.goclaw/skills).
	GlobalSkillsDir string

	// BuiltinSkillsDir is the builtin skills directory (bundled with binary).
	BuiltinSkillsDir string

	// EncryptionKey is the AES-256 key for encrypting sensitive data (API keys).
	// If empty, sensitive data is stored in plain text.
	EncryptionKey string

	// PairingDeviceTTL is the expiry lifetime granted to a paired device on approve
	// and on each sliding renewal (SRS 012). 0 → never expire (approve writes
	// expires_at = NULL); >0 → that lifetime. Gateway build sites always set this
	// from config (empty → DefaultPairedDeviceTTL); callers without a config MUST
	// set DefaultPairedDeviceTTL explicitly so an accidental zero is not read as
	// "never expire".
	PairingDeviceTTL time.Duration

	// PairingRenewalWindow is the near-expiry window inside which an IsPaired hit
	// slides the expiry forward. <=0 → auto (TTL/4). Clamped to TTL by the store.
	// 0 → renewal disabled.
	PairingRenewalWindow time.Duration
}

// DefaultPairedDeviceTTL is the 30-day expiry used for paired devices when no
// TTL is configured (SRS 012). Mirrors the pre-SRS-012 hardcoded const.
const DefaultPairedDeviceTTL = 30 * 24 * time.Hour
