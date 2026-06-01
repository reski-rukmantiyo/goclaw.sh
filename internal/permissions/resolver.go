package permissions

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// Resolver computes effective permissions for a user in a tenant.
// Sources: direct roles + all group roles + ancestor group roles.
type Resolver struct {
	roles  store.RoleStore
	groups store.GroupStore
	ttl    time.Duration

	mu      sync.RWMutex
	entries map[string]resolverEntry // key: "userID:tenantID"
}

type resolverEntry struct {
	perms     map[string]bool
	expiresAt time.Time
}

// NewResolver creates a permission resolver with the given stores and cache TTL.
func NewResolver(roles store.RoleStore, groups store.GroupStore, ttl time.Duration) *Resolver {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &Resolver{
		roles:   roles,
		groups:  groups,
		ttl:     ttl,
		entries: make(map[string]resolverEntry),
	}
}

func (r *Resolver) key(userID string, tenantID uuid.UUID) string {
	return fmt.Sprintf("%s:%s", userID, tenantID.String())
}

func (r *Resolver) get(key string) (map[string]bool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry, ok := r.entries[key]
	if !ok || time.Now().After(entry.expiresAt) {
		return nil, false
	}
	return entry.perms, true
}

func (r *Resolver) set(key string, perms map[string]bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries[key] = resolverEntry{
		perms:     perms,
		expiresAt: time.Now().Add(r.ttl),
	}
}

// EffectivePermissions returns the union of all permission strings for a user in a tenant.
func (r *Resolver) EffectivePermissions(ctx context.Context, userID string, tenantID uuid.UUID) (map[string]bool, error) {
	key := r.key(userID, tenantID)
	if perms, ok := r.get(key); ok {
		return perms, nil
	}

	perms, err := r.resolve(ctx, userID, tenantID)
	if err != nil {
		return nil, err
	}
	r.set(key, perms)
	return perms, nil
}

// HasPermission checks if the effective permission set includes a permission.
func (r *Resolver) HasPermission(ctx context.Context, userID string, tenantID uuid.UUID, perm Permission) bool {
	perms, err := r.EffectivePermissions(ctx, userID, tenantID)
	if err != nil {
		return false
	}
	return perms[string(perm)]
}

// Invalidate clears the cache for a user/tenant.
func (r *Resolver) Invalidate(userID string, tenantID uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.entries, r.key(userID, tenantID))
}

// InvalidateAll clears the entire cache.
func (r *Resolver) InvalidateAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = make(map[string]resolverEntry)
}

func (r *Resolver) resolve(ctx context.Context, userID string, tenantID uuid.UUID) (map[string]bool, error) {
	result := make(map[string]bool)

	// 1. Direct role permissions
	if r.roles != nil {
		directPerms, err := r.roles.GetUserEffectivePermissions(ctx, userID, tenantID)
		if err != nil {
			return nil, err
		}
		for _, p := range directPerms {
			result[p] = true
		}
	}

	return result, nil
}
