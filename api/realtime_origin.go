package api

import (
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/service"

	"github.com/gin-gonic/gin"
)

func (a *ApiService) validateWSOrigin(c *gin.Context, user string) bool {
	originHeader := strings.TrimSpace(c.GetHeader("Origin"))
	if originHeader == "" {
		return true
	}
	webDomain, _ := a.SettingService.GetWebDomain()
	allowed, reason := wsOriginAllowed(originHeader, c.Request.Host, webDomain)
	if allowed {
		return true
	}
	originHost, originScheme := originAuditParts(originHeader)
	a.recordAudit(c, user, "ws_origin_rejected", "realtime", service.AuditSeverityWarn, map[string]any{
		"reason":       reason,
		"originScheme": originScheme,
		"originHost":   originHost,
		"requestHost":  canonicalHostPort(c.Request.Host),
		"webDomain":    canonicalHostname(webDomain),
	})
	c.Status(http.StatusForbidden)
	return false
}

func wsOriginAllowed(originHeader string, requestHost string, webDomain string) (bool, string) {
	originURL, err := url.Parse(originHeader)
	if err != nil || originURL.Scheme == "" || originURL.Host == "" {
		return false, "invalid_origin"
	}
	if originURL.Scheme != "http" && originURL.Scheme != "https" {
		return false, "invalid_scheme"
	}
	if originURL.RawQuery != "" || originURL.Fragment != "" || (originURL.Path != "" && originURL.Path != "/") {
		return false, "invalid_origin"
	}

	originHostPort := canonicalHostPort(originURL.Host)
	if originHostPort == "" {
		return false, "invalid_origin"
	}
	if requestHost != "" && originHostPort == canonicalHostPort(requestHost) {
		return true, "request_host"
	}

	originHost := canonicalHostname(originURL.Host)
	webDomainHost := canonicalHostname(webDomain)
	if webDomainHost != "" && originHost == webDomainHost {
		return true, "web_domain"
	}
	if webDomainHostPort := canonicalHostPort(webDomain); webDomainHostPort != "" && originHostPort == webDomainHostPort {
		return true, "web_domain"
	}
	return false, "host_mismatch"
}

func originAuditParts(originHeader string) (string, string) {
	originURL, err := url.Parse(originHeader)
	if err != nil {
		return "", ""
	}
	return canonicalHostPort(originURL.Host), originURL.Scheme
}

func canonicalHostPort(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if parsed, err := url.Parse(value); err == nil && parsed.Host != "" {
		value = parsed.Host
	}
	if host, port, err := net.SplitHostPort(value); err == nil {
		return strings.TrimSuffix(strings.ToLower(strings.Trim(host, "[]")), ".") + ":" + port
	}
	return strings.TrimSuffix(strings.ToLower(strings.Trim(value, "[]")), ".")
}

func canonicalHostname(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if parsed, err := url.Parse(value); err == nil && parsed.Host != "" {
		value = parsed.Host
	}
	if host, _, err := net.SplitHostPort(value); err == nil {
		value = host
	}
	return strings.TrimSuffix(strings.ToLower(strings.Trim(value, "[]")), ".")
}
