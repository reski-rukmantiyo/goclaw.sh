// Package tenantauth provides per-tenant authentication configuration resolution.
package tenantauth

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// Loader resolves auth configuration for a specific tenant.
// Overrides come from system_configs; missing keys fall back to the global config.
type Loader interface {
	LoadAuthConfig(ctx context.Context, tenantID uuid.UUID) (*config.AuthConfig, error)
	Invalidate(tenantID uuid.UUID)
}

// SystemConfigLoader loads auth config from the per-tenant system_configs table.
type SystemConfigLoader struct {
	sc       store.SystemConfigStore
	fallback *config.AuthConfig
	encKey   string

	mu    sync.RWMutex
	cache map[uuid.UUID]*cachedAuthConfig
	ttl   time.Duration
}

type cachedAuthConfig struct {
	cfg       *config.AuthConfig
	expiresAt time.Time
}

// NewSystemConfigLoader creates a loader backed by system_configs.
// fallback is used when a tenant has no auth overrides.
func NewSystemConfigLoader(sc store.SystemConfigStore, fallback config.AuthConfig, encKey string) *SystemConfigLoader {
	return &SystemConfigLoader{
		sc:       sc,
		fallback: &fallback,
		encKey:   encKey,
		cache:    make(map[uuid.UUID]*cachedAuthConfig),
		ttl:      60 * time.Second,
	}
}

// LoadAuthConfig returns the auth config for tenantID.
// It queries system_configs with tenantID in context, overlays onto a copy of fallback.
func (l *SystemConfigLoader) LoadAuthConfig(ctx context.Context, tenantID uuid.UUID) (*config.AuthConfig, error) {
	// Fast path: cached
	l.mu.RLock()
	c, ok := l.cache[tenantID]
	if ok && time.Now().Before(c.expiresAt) {
		l.mu.RUnlock()
		return c.cfg, nil
	}
	l.mu.RUnlock()

	// Slow path: load from DB
	cfg, err := l.loadFromDB(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	l.mu.Lock()
	l.cache[tenantID] = &cachedAuthConfig{cfg: cfg, expiresAt: time.Now().Add(l.ttl)}
	l.mu.Unlock()
	return cfg, nil
}

// Invalidate clears the cache for a tenant.
func (l *SystemConfigLoader) Invalidate(tenantID uuid.UUID) {
	l.mu.Lock()
	delete(l.cache, tenantID)
	l.mu.Unlock()
}

func (l *SystemConfigLoader) loadFromDB(ctx context.Context, tenantID uuid.UUID) (*config.AuthConfig, error) {
	ctx = store.WithTenantID(ctx, tenantID)

	configs, err := l.sc.List(ctx)
	if err != nil {
		// If tenant has no configs at all, return fallback directly
		return l.copyFallback(), nil
	}

	// Start from fallback copy
	cfg := l.copyFallback()

	// Apply auth overrides using same keys as config.ApplySystemConfigs
	boolDirect := func(key string, dst *bool) {
		if v, ok := configs[key]; ok && v != "" {
			*dst = v == "true" || v == "1"
		}
	}
	str := func(key string, dst *string) {
		if v, ok := configs[key]; ok && v != "" {
			*dst = v
		}
	}
	integer := func(key string, dst *int) {
		if v, ok := configs[key]; ok && v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				*dst = n
			}
		}
	}
	decryptStr := func(key string, dst *string) {
		if v, ok := configs[key]; ok && v != "" {
			plain, err := crypto.Decrypt(v, l.encKey)
			if err != nil {
				slog.Warn("tenant_auth.decrypt_failed", "key", key, "tenant_id", tenantID, "error", err)
				*dst = v // use as-is if decrypt fails (may be plaintext legacy)
			} else {
				*dst = plain
			}
		}
	}

	if _, ok := configs["auth.local.enabled"]; ok && cfg.Providers.Local == nil {
		cfg.Providers.Local = &config.LocalAuthConfig{}
	}
	if cfg.Providers.Local != nil {
		boolDirect("auth.local.enabled", &cfg.Providers.Local.Enabled)
	}

	if _, ok := configs["auth.entra_id.enabled"]; ok && cfg.Providers.EntraID == nil {
		cfg.Providers.EntraID = &config.EntraIDAuthConfig{}
	}
	if cfg.Providers.EntraID != nil {
		boolDirect("auth.entra_id.enabled", &cfg.Providers.EntraID.Enabled)
		str("auth.entra_id.client_id", &cfg.Providers.EntraID.ClientID)
		str("auth.entra_id.redirect_uri", &cfg.Providers.EntraID.RedirectURI)
		str("auth.entra_id.tenant_id", &cfg.Providers.EntraID.TenantID)
		decryptStr("auth.entra_id.client_secret", &cfg.Providers.EntraID.ClientSecret)
	}

	if _, ok := configs["auth.google.enabled"]; ok && cfg.Providers.Google == nil {
		cfg.Providers.Google = &config.GoogleAuthConfig{}
	}
	if cfg.Providers.Google != nil {
		boolDirect("auth.google.enabled", &cfg.Providers.Google.Enabled)
		str("auth.google.client_id", &cfg.Providers.Google.ClientID)
		str("auth.google.redirect_uri", &cfg.Providers.Google.RedirectURI)
		decryptStr("auth.google.client_secret", &cfg.Providers.Google.ClientSecret)
	}

	integer("auth.session.timeout_minutes", &cfg.Session.TimeoutMinutes)
	boolDirect("auth.session.refresh_enabled", &cfg.Session.RefreshEnabled)

	return cfg, nil
}

func (l *SystemConfigLoader) copyFallback() *config.AuthConfig {
	// Deep copy via JSON round-trip (same as MaskedCopy)
	data, err := json.Marshal(l.fallback)
	if err != nil {
		return &config.AuthConfig{}
	}
	cp := &config.AuthConfig{}
	if err := json.Unmarshal(data, cp); err != nil {
		return &config.AuthConfig{}
	}
	return cp
}

// SyncAuthConfigToSystemConfigs writes auth config keys into system_configs for a tenant.
// Used by tenants.update when settings contain auth overrides.
func SyncAuthConfigToSystemConfigs(ctx context.Context, sc store.SystemConfigStore, tenantID uuid.UUID, auth *config.AuthConfig, encKey string) error {
	ctx = store.WithTenantID(ctx, tenantID)

	set := func(key, val string) error {
		if val == "" {
			_ = sc.Delete(ctx, key)
			return nil
		}
		return sc.Set(ctx, key, val)
	}
	setBool := func(key string, val bool) error {
		return set(key, fmt.Sprintf("%t", val))
	}
	setInt := func(key string, val int) error {
		if val == 0 {
			_ = sc.Delete(ctx, key)
			return nil
		}
		return set(key, fmt.Sprintf("%d", val))
	}
	setSecret := func(key, val string) error {
		if val == "" {
			_ = sc.Delete(ctx, key)
			return nil
		}
		enc, err := crypto.Encrypt(val, encKey)
		if err != nil {
			return fmt.Errorf("encrypt %s: %w", key, err)
		}
		return sc.Set(ctx, key, enc)
	}

	if auth.Providers.Local != nil {
		if err := setBool("auth.local.enabled", auth.Providers.Local.Enabled); err != nil {
			return err
		}
	}

	if auth.Providers.EntraID != nil {
		if err := setBool("auth.entra_id.enabled", auth.Providers.EntraID.Enabled); err != nil {
			return err
		}
		if err := set("auth.entra_id.client_id", auth.Providers.EntraID.ClientID); err != nil {
			return err
		}
		if err := set("auth.entra_id.redirect_uri", auth.Providers.EntraID.RedirectURI); err != nil {
			return err
		}
		if err := set("auth.entra_id.tenant_id", auth.Providers.EntraID.TenantID); err != nil {
			return err
		}
		if err := setSecret("auth.entra_id.client_secret", auth.Providers.EntraID.ClientSecret); err != nil {
			return err
		}
	}

	if auth.Providers.Google != nil {
		if err := setBool("auth.google.enabled", auth.Providers.Google.Enabled); err != nil {
			return err
		}
		if err := set("auth.google.client_id", auth.Providers.Google.ClientID); err != nil {
			return err
		}
		if err := set("auth.google.redirect_uri", auth.Providers.Google.RedirectURI); err != nil {
			return err
		}
		if err := setSecret("auth.google.client_secret", auth.Providers.Google.ClientSecret); err != nil {
			return err
		}
	}

	if err := setInt("auth.session.timeout_minutes", auth.Session.TimeoutMinutes); err != nil {
		return err
	}
	if err := setBool("auth.session.refresh_enabled", auth.Session.RefreshEnabled); err != nil {
		return err
	}

	return nil
}

// GetEncryptionKey returns GOCLAW_ENCRYPTION_KEY from environment.
func GetEncryptionKey() string {
	return os.Getenv("GOCLAW_ENCRYPTION_KEY")
}
