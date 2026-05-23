package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
)

// SanitizedWorkstation is the safe API view of a Workstation — no secret fields.
// Used in all HTTP/WS responses to prevent credentials from reaching clients.
type SanitizedWorkstation struct {
	ID              uuid.UUID          `json:"id"`
	WorkstationKey  string             `json:"workstationKey"`
	TenantID        uuid.UUID          `json:"tenantId"`
	Name            string             `json:"name"`
	BackendType     WorkstationBackend `json:"backendType"`
	DefaultCWD      string             `json:"defaultCwd"`
	Active          bool               `json:"active"`
	CreatedAt       time.Time          `json:"createdAt"`
	UpdatedAt       time.Time          `json:"updatedAt"`
	CreatedBy       string             `json:"createdBy"`
	MetadataSummary map[string]any     `json:"metadataSummary,omitempty"`
}

// WorkstationBackend is the backend type for a workstation.
type WorkstationBackend string

const (
	BackendSSH    WorkstationBackend = "ssh"
	BackendDocker WorkstationBackend = "docker"
)

// Workstation represents a remote execution environment registered to a tenant.
// Metadata and DefaultEnv are stored AES-256-GCM encrypted; in-memory they are plaintext JSON.
// SECURITY: Metadata and DefaultEnv are excluded from JSON serialization (json:"-") to prevent
// SSH private keys / passwords from leaking in API responses. Use SanitizedView() for responses.
type Workstation struct {
	ID             uuid.UUID          `json:"id"`
	WorkstationKey string             `json:"workstationKey"`
	TenantID       uuid.UUID          `json:"tenantId"`
	Name           string             `json:"name"`
	BackendType    WorkstationBackend `json:"backendType"`
	// Metadata holds backend-specific config (SSH or Docker). Plaintext after decrypt.
	// json:"-" prevents SSH keys/passwords from appearing in API responses.
	Metadata   []byte    `json:"-"`
	DefaultCWD string    `json:"defaultCwd"`
	// DefaultEnv holds a JSON map of env overrides. Plaintext after decrypt.
	// json:"-" prevents env secrets from appearing in API responses.
	DefaultEnv []byte    `json:"-"`
	Active     bool      `json:"active"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
	CreatedBy  string    `json:"createdBy"`
}

// SanitizedView returns a safe representation for API responses.
// SSH metadata is summarized (host/port/user/hasKey) without private keys.
// Docker metadata is summarized (image/containerName) without credentials.
// Raw Metadata and DefaultEnv bytes are never included.
func (ws *Workstation) SanitizedView() *SanitizedWorkstation {
	sv := &SanitizedWorkstation{
		ID:             ws.ID,
		WorkstationKey: ws.WorkstationKey,
		TenantID:       ws.TenantID,
		Name:           ws.Name,
		BackendType:    ws.BackendType,
		DefaultCWD:     ws.DefaultCWD,
		Active:         ws.Active,
		CreatedAt:      ws.CreatedAt,
		UpdatedAt:      ws.UpdatedAt,
		CreatedBy:      ws.CreatedBy,
	}
	// Build metadata summary without exposing credentials.
	switch ws.BackendType {
	case BackendSSH:
		if m, err := UnmarshalSSHMetadata(ws.Metadata); err == nil {
			sv.MetadataSummary = map[string]any{
				"host":   m.Host,
				"port":   m.Port,
				"user":   m.User,
				"hasKey": m.PrivateKey != "",
			}
		}
	case BackendDocker:
		if m, err := UnmarshalDockerMetadata(ws.Metadata); err == nil {
			sv.MetadataSummary = map[string]any{
				"image":         m.Image,
				"containerName": m.Host,
			}
		}
	}
	return sv
}

// AgentWorkstationLink binds an agent to a workstation within a tenant.
type AgentWorkstationLink struct {
	AgentID       uuid.UUID `json:"agentId"`
	WorkstationID uuid.UUID `json:"workstationId"`
	TenantID      uuid.UUID `json:"tenantId"`
	IsDefault     bool      `json:"isDefault"`
	CreatedAt     time.Time `json:"createdAt"`
}

// SSHMetadata contains SSH-specific connection parameters.
// Either PrivateKey (inline PEM) or Password must be set for auth.
// KnownHostsFingerprint is the SHA256 fingerprint of the host's public key (base64).
// If empty on first connect, TOFU (Trust On First Use) accepts and logs the fingerprint.
type SSHMetadata struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	// PrivateKey holds inline PEM-encoded private key material (decrypted by store layer).
	PrivateKey string `json:"privateKey,omitempty"`
	// IdentityFile is a filesystem path to a private key. If set and PrivateKey is empty,
	// the store layer reads the file and populates PrivateKey. Supports ~ expansion.
	IdentityFile string `json:"identityFile,omitempty"`
	// Password is optional; prefer key-based auth.
	Password              string `json:"password,omitempty"`
	// KnownHostsFingerprint is the expected SHA256 fingerprint (e.g. "SHA256:abc...").
	// Empty → TOFU on first connect; subsequent calls must match.
	KnownHostsFingerprint string `json:"knownHostsFingerprint,omitempty"`
	// ConnectTimeoutSec overrides the default 10s TCP dial timeout.
	ConnectTimeoutSec int `json:"connectTimeoutSec,omitempty"`
}

// DockerMetadata contains Docker-specific connection parameters.
type DockerMetadata struct {
	Host      string `json:"host"`
	Image     string `json:"image"`
	Network   string `json:"network,omitempty"`
	SocketPath string `json:"socketPath,omitempty"`
}

// UnmarshalSSHMetadata parses and validates SSH metadata bytes.
// If IdentityFile is set and PrivateKey is empty, reads the file and populates PrivateKey.
func UnmarshalSSHMetadata(raw []byte) (*SSHMetadata, error) {
	var m SSHMetadata
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	if m.Host == "" {
		return nil, fmt.Errorf("host is required")
	}
	if m.User == "" {
		return nil, fmt.Errorf("user is required")
	}
	if m.Port == 0 {
		m.Port = 22
	}
	if m.Port < 1 || m.Port > 65535 {
		return nil, fmt.Errorf("port %d out of range", m.Port)
	}
	if m.IdentityFile != "" && m.PrivateKey == "" {
		path := m.IdentityFile
		if len(path) > 0 && path[0] == '~' {
			if home, err := os.UserHomeDir(); err == nil {
				if len(path) > 1 && path[1] == '/' {
					path = filepath.Join(home, path[2:])
				} else {
					path = home
				}
			}
		}
		pem, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read identityFile %q: %w", m.IdentityFile, err)
		}
		m.PrivateKey = string(pem)
	}
	if m.PrivateKey == "" && m.Password == "" {
		return nil, fmt.Errorf("privateKey, identityFile, or password is required")
	}
	return &m, nil
}

// UnmarshalDockerMetadata parses and validates Docker metadata bytes.
func UnmarshalDockerMetadata(raw []byte) (*DockerMetadata, error) {
	var m DockerMetadata
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	if m.Host == "" && m.SocketPath == "" {
		return nil, fmt.Errorf("host or socketPath is required")
	}
	if m.Image == "" {
		return nil, fmt.Errorf("image is required")
	}
	return &m, nil
}

// ValidateMetadata parses and validates metadata for the given backend type.
// Returns a non-nil error if the shape is invalid.
func ValidateMetadata(backend WorkstationBackend, raw []byte) error {
	switch backend {
	case BackendSSH:
		_, err := UnmarshalSSHMetadata(raw)
		return err
	case BackendDocker:
		_, err := UnmarshalDockerMetadata(raw)
		return err
	default:
		return fmt.Errorf("unknown backend: %s", backend)
	}
}

// MergeSSHMetadata overlays selective updates onto existing SSH metadata.
// Structural fields (host, port, user) are always overwritten when non-empty/non-zero.
// Auth fields (identityFile, password, knownHostsFingerprint, connectTimeoutSec)
// are only overwritten when their key is explicitly present in the updates map.
// This allows callers to change host/port/user without re-supplying credentials.
func MergeSSHMetadata(current []byte, updates map[string]any) ([]byte, error) {
	var old SSHMetadata
	if len(current) > 0 {
		if err := json.Unmarshal(current, &old); err != nil {
			return nil, fmt.Errorf("unmarshal current: %w", err)
		}
	}

	updBytes, err := json.Marshal(updates)
	if err != nil {
		return nil, fmt.Errorf("marshal updates: %w", err)
	}
	var new SSHMetadata
	if err := json.Unmarshal(updBytes, &new); err != nil {
		return nil, fmt.Errorf("unmarshal updates: %w", err)
	}

	if new.Host != "" {
		old.Host = new.Host
	}
	if new.Port != 0 {
		old.Port = new.Port
	}
	if new.User != "" {
		old.User = new.User
	}

	if _, ok := updates["privateKey"]; ok {
		old.PrivateKey = new.PrivateKey
	}
	if _, ok := updates["identityFile"]; ok {
		old.IdentityFile = new.IdentityFile
	}
	if _, ok := updates["password"]; ok {
		old.Password = new.Password
	}
	if _, ok := updates["knownHostsFingerprint"]; ok {
		old.KnownHostsFingerprint = new.KnownHostsFingerprint
	}
	if _, ok := updates["connectTimeoutSec"]; ok {
		old.ConnectTimeoutSec = new.ConnectTimeoutSec
	}

	return json.Marshal(old)
}

// WorkstationStore defines CRUD operations for workstations (tenant-scoped).
// All mutations include tenant_id in WHERE — never cross-tenant writes.
type WorkstationStore interface {
	// Create inserts a new workstation. Encrypts metadata + default_env.
	Create(ctx context.Context, ws *Workstation) error
	// GetByID fetches by UUID within the caller's tenant. Returns sql.ErrNoRows if not found.
	GetByID(ctx context.Context, id uuid.UUID) (*Workstation, error)
	// GetByKey fetches by workstation_key within the caller's tenant.
	GetByKey(ctx context.Context, key string) (*Workstation, error)
	// List returns all active workstations for the caller's tenant.
	List(ctx context.Context) ([]Workstation, error)
	// Update applies a field map to a workstation, enforcing tenant_id in WHERE.
	Update(ctx context.Context, id uuid.UUID, updates map[string]any) error
	// SetActive soft-deletes (active=false) or re-activates a workstation.
	SetActive(ctx context.Context, id uuid.UUID, active bool) error
	// Delete permanently removes a workstation (hard delete, tenant-scoped).
	Delete(ctx context.Context, id uuid.UUID) error
}

// AgentWorkstationLinkStore manages agent↔workstation bindings.
type AgentWorkstationLinkStore interface {
	// Link creates a binding between an agent and a workstation.
	Link(ctx context.Context, link *AgentWorkstationLink) error
	// Unlink removes the binding.
	Unlink(ctx context.Context, agentID, workstationID uuid.UUID) error
	// SetDefault marks a workstation as default for an agent (clears prior default).
	SetDefault(ctx context.Context, agentID, workstationID uuid.UUID) error
	// ListForAgent returns all workstations linked to an agent.
	ListForAgent(ctx context.Context, agentID uuid.UUID) ([]AgentWorkstationLink, error)
	// ListForWorkstation returns all agents linked to a workstation.
	ListForWorkstation(ctx context.Context, workstationID uuid.UUID) ([]AgentWorkstationLink, error)
}
