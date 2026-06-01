package http

import (
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// NewPermissionCache creates a permission resolver backed by the given stores.
func NewPermissionCache(roles store.RoleStore, groups store.GroupStore, ttl time.Duration) *permissions.Resolver {
	return permissions.NewResolver(roles, groups, ttl)
}
