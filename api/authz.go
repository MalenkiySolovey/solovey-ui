package api

import (
	"net/http"

	"github.com/MalenkiySolovey/solovey-ui/service"

	"github.com/gin-gonic/gin"
)

func (a *ApiService) requireAuditAdminScope(c *gin.Context) bool {
	scope, hasScope := requestTokenScope(c)
	if auditAdminScopeAllowed(scope, hasScope) {
		return true
	}
	a.recordAudit(c, requestActor(c), "audit_scope_denied", "audit", service.AuditSeverityWarn, map[string]any{
		"scope": scope,
	})
	c.JSON(http.StatusForbidden, Msg{Success: false, Msg: "audit: insufficient scope"})
	return false
}

func auditAdminScopeAllowed(scope string, hasScope bool) bool {
	return !hasScope || scope == "admin"
}

func (a *ApiService) requireTokenScopeAny(c *gin.Context, resource string, allowed ...string) bool {
	scope, hasScope := requestTokenScope(c)
	if !hasScope {
		return true
	}
	for _, allowedScope := range allowed {
		if scope == allowedScope {
			return true
		}
	}
	a.recordAudit(c, requestActor(c), "scope_denied", resource, service.AuditSeverityWarn, map[string]any{
		"scope":    scope,
		"required": allowed,
	})
	c.JSON(http.StatusForbidden, Msg{Success: false, Msg: "insufficient scope"})
	return false
}

func requestActor(c *gin.Context) string {
	if username := c.GetString(apiUsernameKey); username != "" {
		return username
	}
	return GetLoginUser(c)
}

func requestTokenScope(c *gin.Context) (string, bool) {
	scope, ok := c.Get(apiTokenScopeKey)
	if !ok {
		return "", false
	}
	scopeString, ok := scope.(string)
	return scopeString, ok
}
