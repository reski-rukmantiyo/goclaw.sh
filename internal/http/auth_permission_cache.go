package http

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// permCacheEntry holds a cached user role resolution.
type permCacheEntry struct {
	role      permissions.UserRole
	expiresAt time.Time
}

// permissionCache caches resolved user roles with TTL.
// Follows the same pattern as apiKeyCache.
type permissionCache struct {
	mu      sync.RWMutex
	entries map[string]permCacheEntry // key: "userID:tenantID"
	ttl     time.Duration
	users   store.UserStore
	groups  store.GroupStore
}

func NewPermissionCache(users store.UserStore, groups store.GroupStore, ttl time.Duration) *permissionCache {
	return &permissionCache{
		entries: make(map[string]permCacheEntry),
		ttl:     ttl,
		users:   users,
		groups:  groups,
	}
}

// get returns the cached role if present and not expired.
func (c *permissionCache) get(key string) (permissions.UserRole, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.entries[key]
	if !ok || time.Now().After(entry.expiresAt) {
		return "", false
	}
	return entry.role, true
}

// resolveUserRole determines the user's role in the multi-auth RBAC system.
// Checks: tenant admin → group admin → member.
func (c *permissionCache) resolveUserRole(ctx context.Context, userIDStr, tenantIDStr string) permissions.UserRole {
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		return permissions.UserRoleMember
	}
	key := fmt.Sprintf("%s:%s", userIDStr, tenantIDStr)

	if role, ok := c.get(key); ok {
		return role
	}

	// Cache miss — resolve from DB
	user, err := c.users.GetByID(ctx, userID)
	if err != nil || user == nil {
		return permissions.UserRoleMember
	}

	var role permissions.UserRole
	if user.IsTenantAdmin {
		role = permissions.UserRoleTenantAdmin
	} else if c.groups != nil {
		// Check if user is admin of any group
		groups, err := c.groups.GetUserGroups(ctx, userID)
		if err == nil {
			for _, g := range groups {
				if memberRole, err := c.groups.GetMemberRole(ctx, g.ID, userID); err == nil && memberRole == store.GroupRoleAdmin {
					role = permissions.UserRoleGroupAdmin
					break
				}
			}
		}
	}

	if role == "" {
		role = permissions.UserRoleMember
	}

	// Store in cache
	c.mu.Lock()
	c.entries[key] = permCacheEntry{
		role:      role,
		expiresAt: time.Now().Add(c.ttl),
	}
	c.mu.Unlock()

	return role
}

// invalidate removes a specific user's cached role.
func (c *permissionCache) invalidate(userID, tenantID string) {
	key := fmt.Sprintf("%s:%s", userID, tenantID)
	c.mu.Lock()
	delete(c.entries, key)
	c.mu.Unlock()
}

// invalidateAll clears all cached entries.
func (c *permissionCache) invalidateAll() {
	c.mu.Lock()
	c.entries = make(map[string]permCacheEntry)
	c.mu.Unlock()
}
