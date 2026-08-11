package api

import (
	"net/http"

	clientidentity "github.com/MalenkiySolovey/solovey-ui/internal/httpsecurity/clientidentity"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"github.com/gin-gonic/gin"
)

const clientIdentityContextKey = "sui.client_identity.v1"

func RequestClientIdentity(c *gin.Context) clientidentity.V1 {
	if cached, ok := c.Get(clientIdentityContextKey); ok {
		if identity, typeOK := cached.(clientidentity.V1); typeOK {
			return identity
		}
	}
	identity := clientidentity.ResolveRequest(c.Request)
	c.Set(clientIdentityContextKey, identity)
	return identity
}

// requestAuthorityMiddleware rejects ambiguous Host and trusted-forwarding
// authority before authentication, CSRF, or route handlers consume it.
func (a *APIHandler) requestAuthorityMiddleware(c *gin.Context) {
	identity := RequestClientIdentity(c)
	config := clientidentity.ConfigFromEnvironment()
	a.auditTrustedProxyConfig(c, config)
	if identity.ExternalHost == "" || !identity.ForwardedValid {
		a.recordAudit(c, "system", "request_authority_rejected", "network", service.AuditSeverityWarn, map[string]any{
			"hostValid":      identity.ExternalHost != "",
			"forwardedValid": identity.ForwardedValid,
			"configRevision": identity.ConfigRevision,
		})
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
			"success": false,
			"msg":     "Invalid request authority",
			"obj":     nil,
		})
		return
	}
	c.Next()
}

func (a *APIHandler) auditTrustedProxyConfig(c *gin.Context, config clientidentity.Config) {
	a.proxyConfigMu.Lock()
	changed := a.proxyConfigSeen && a.proxyRevision != config.Revision
	first := !a.proxyConfigSeen
	if first || changed {
		a.proxyConfigSeen = true
		a.proxyRevision = config.Revision
	}
	a.proxyConfigMu.Unlock()
	if !first && !changed {
		return
	}
	event := "trusted_proxy_config_observed"
	if changed {
		event = "trusted_proxy_config_changed"
	}
	severity := service.AuditSeverityInfo
	if len(config.Warnings) > 0 {
		severity = service.AuditSeverityWarn
	}
	a.recordAudit(c, "system", event, "network", severity, map[string]any{
		"source":       config.Source,
		"revision":     config.Revision,
		"configured":   len(config.TrustedProxies),
		"warningCodes": config.Warnings,
	})
}

func getRemoteIp(c *gin.Context) string {
	return RequestClientIdentity(c).ClientIP
}

func canonicalClientIP(value string) string {
	return clientidentity.CanonicalIP(value)
}

// RequestIsHTTPS reports whether the request arrived over HTTPS, trusting
// X-Forwarded-Proto only when the peer is a configured trusted proxy. Exported
// so the security-headers middleware can reuse this gated check for its HSTS
// decision (a spoofed X-Forwarded-Proto from an untrusted client must not
// trigger HSTS).
func RequestIsHTTPS(c *gin.Context) bool {
	return RequestClientIdentity(c).DesiredScheme == "https"
}
