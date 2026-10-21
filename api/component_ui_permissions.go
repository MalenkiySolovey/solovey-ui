package api

import "github.com/gin-gonic/gin"

// Read-only visibility projection of the existing API scope rules. The posture
// route has already required an authenticated panel session; this grants no
// command authority and no handler may use this projection as authorization.
func componentUIReadPermissions(c *gin.Context) []string {
	scope, hasScope := requestTokenScope(c)
	permissions := []string{}
	if auditAdminScopeAllowed(scope, hasScope) {
		permissions = append(permissions, "audit.read")
	}
	if !hasScope || scope == "admin" || scope == "read" || scope == "write" || scope == "observability" {
		permissions = append(permissions, "diagnostics.read")
	}
	return permissions
}
