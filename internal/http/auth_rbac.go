package http

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

var pkgPermCache *permissionCache

// InitPermCache sets the shared permission cache for RBAC checks.
func InitPermCache(cache *permissionCache) {
	pkgPermCache = cache
}

// requireAuthAction checks that the authenticated user has permission to perform an action.
// Resolves user role via permission cache and checks against the RBAC matrix.
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
			role := pkgPermCache.resolveUserRole(ctx, auth.UserID, tenantID.String())
			if !permissions.UserCanPerform(role, action) {
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

// resolveUserRoleFromContext returns the user's multi-auth RBAC role from the permission cache.
func resolveUserRoleFromContext(r *http.Request) permissions.UserRole {
	if pkgPermCache == nil {
		return ""
	}
	auth := resolveAuth(r)
	if auth.UserID == "" {
		return ""
	}
	tenantID := auth.TenantID
	if tenantID == uuid.Nil {
		tenantID = store.MasterTenantID
	}
	return pkgPermCache.resolveUserRole(r.Context(), auth.UserID, tenantID.String())
}
