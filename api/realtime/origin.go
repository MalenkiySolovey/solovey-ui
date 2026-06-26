package realtimehttp

import (
	"net/http"
	"strings"

	securitymiddleware "github.com/MalenkiySolovey/solovey-ui/middleware/security"
	"github.com/MalenkiySolovey/solovey-ui/service"

	"github.com/gin-gonic/gin"
)

func (a *Handler) ValidateOrigin(c *gin.Context, user string) bool {
	originHeader := strings.TrimSpace(c.GetHeader("Origin"))
	if originHeader == "" {
		return true
	}
	webDomain, _ := a.SettingService.GetWebDomain()
	allowed, reason := OriginAllowed(originHeader, c.Request.Host, webDomain)
	if allowed {
		return true
	}
	originHost, originScheme := securitymiddleware.OriginAuditParts(originHeader)
	a.Audit(c, user, "ws_origin_rejected", "realtime", service.AuditSeverityWarn, map[string]any{
		"reason":       reason,
		"originScheme": originScheme,
		"originHost":   originHost,
		"requestHost":  securitymiddleware.CanonicalHostPort(c.Request.Host),
		"webDomain":    securitymiddleware.CanonicalHostname(webDomain),
	})
	c.Status(http.StatusForbidden)
	return false
}

func OriginAllowed(originHeader string, requestHost string, webDomain string) (bool, string) {
	return securitymiddleware.WebSocketOriginAllowed(originHeader, requestHost, webDomain)
}
