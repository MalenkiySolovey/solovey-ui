package realtimehttp

import (
	"net/http"
	"net/url"
	"strings"

	clientidentity "github.com/MalenkiySolovey/solovey-ui/internal/httpsecurity/clientidentity"
	securitymiddleware "github.com/MalenkiySolovey/solovey-ui/middleware/security"
	"github.com/MalenkiySolovey/solovey-ui/service"

	"github.com/gin-gonic/gin"
)

func (a *Handler) ValidateOrigin(c *gin.Context, user string) bool {
	if len(c.Request.Header.Values("Origin")) != 1 {
		a.Audit(c, user, "ws_origin_rejected", "realtime", service.AuditSeverityWarn, map[string]any{
			"reason": "missing_or_multiple_origin",
		})
		c.Status(http.StatusForbidden)
		return false
	}
	originHeader := strings.TrimSpace(c.GetHeader("Origin"))
	if originHeader == "" {
		a.Audit(c, user, "ws_origin_rejected", "realtime", service.AuditSeverityWarn, map[string]any{
			"reason": "missing_origin",
		})
		c.Status(http.StatusForbidden)
		return false
	}
	identity := clientidentity.ResolveRequest(c.Request)
	webDomain, _ := a.SettingService.GetWebDomain()
	allowed, reason := OriginAllowedV1(originHeader, identity, webDomain)
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

func OriginAllowedV1(originHeader string, identity clientidentity.V1, _ string) (bool, string) {
	if identity.ExternalHost == "" || !identity.ForwardedValid {
		return false, "invalid_request_authority"
	}
	originURL, err := url.Parse(originHeader)
	if err != nil || originURL.User != nil || originURL.Scheme == "" || originURL.Host == "" ||
		originURL.RawQuery != "" || originURL.Fragment != "" || (originURL.Path != "" && originURL.Path != "/") {
		return false, "invalid_origin"
	}
	if originURL.Scheme != "http" && originURL.Scheme != "https" {
		return false, "invalid_scheme"
	}
	if !strings.EqualFold(originURL.Scheme, identity.DesiredScheme) {
		return false, "scheme_mismatch"
	}
	originHost := clientidentity.CanonicalHostPort(originURL.Host)
	if originHost != "" && originHost == identity.ExternalHost {
		return true, "external_origin"
	}
	return false, "host_mismatch"
}

func OriginAllowed(originHeader string, requestHost string, webDomain string) (bool, string) {
	return securitymiddleware.WebSocketOriginAllowed(originHeader, requestHost, webDomain)
}
