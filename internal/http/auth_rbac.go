package http

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

var pkgPermCache *permissions.Resolver

// InitPermCache sets the shared permission resolver for RBAC checks.
func InitPermCache(resolver *permissions.Resolver) {
	pkgPermCache = resolver
}

// GetUserPermissions returns the effective permissions for a user in a tenant.
// Returns nil if the resolver is unavailable or an error occurs.
// Exported for use by the gateway router to re-derive roles during WS connect.
func GetUserPermissions(ctx context.Context, userID string, tenantID uuid.UUID) (map[string]bool, error) {
	if pkgPermCache == nil {
		return nil, nil
	}
	return pkgPermCache.EffectivePermissions(ctx, userID, tenantID)
}

// requireAuthAction checks that the authenticated user has permission to perform an action.
func requireAuthAction(action string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		locale := extractLocale(r)
		auth := resolveAuth(r)

		if !auth.Authenticated {
			writeJSON(w, http.StatusUnauthorized, map[string]string{
				"error": i18n.T(locale, i18n.MsgUnauthorized),
			})
			return
		}

		ctx := enrichContext(r.Context(), r, auth)

		if pkgPermCache != nil && auth.UserID != "" {
			tenantID := auth.TenantID
			if tenantID == uuid.Nil {
				tenantID = store.MasterTenantID
			}
			if !pkgPermCache.HasPermission(ctx, auth.UserID, tenantID, permissions.Permission(action)) {
				writeJSON(w, http.StatusForbidden, map[string]string{
					"error": i18n.T(locale, i18n.MsgPermissionDenied, action),
				})
				return
			}
		} else {
			// Fallback to gateway role check for non-multi-auth users
			required := httpMinRole(r.Method)
			if !permissions.HasMinRole(auth.Role, required) {
				writeJSON(w, http.StatusForbidden, map[string]string{
					"error": i18n.T(locale, i18n.MsgPermissionDenied, r.URL.Path),
				})
				return
			}
		}

		next(w, r.WithContext(ctx))
	}
}
