package security

import (
	"net"
	"net/url"
	"strings"

	clientidentity "github.com/MalenkiySolovey/solovey-ui/internal/httpsecurity/clientidentity"
)

func WebSocketOriginAllowed(originHeader, requestHost, _ string) (bool, string) {
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

	originHostPort := CanonicalHostPort(originURL.Host)
	if originHostPort == "" {
		return false, "invalid_origin"
	}
	canonicalRequestHost := CanonicalHostPort(requestHost)
	if canonicalRequestHost == "" {
		return false, "invalid_request_host"
	}
	if originHostPort == canonicalRequestHost {
		return true, "request_host"
	}
	return false, "host_mismatch"
}

func OriginAuditParts(originHeader string) (string, string) {
	originURL, err := url.Parse(originHeader)
	if err != nil {
		return "", ""
	}
	return CanonicalHostPort(originURL.Host), originURL.Scheme
}

func CanonicalHostPort(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if parsed, err := url.Parse(value); err == nil && parsed.Host != "" && parsed.User == nil {
		value = parsed.Host
	}
	return clientidentity.CanonicalHostPort(value)
}

func CanonicalHostname(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if parsed, err := url.Parse(value); err == nil && parsed.Host != "" && parsed.User == nil {
		value = parsed.Host
	}
	canonical := clientidentity.CanonicalHostPort(value)
	if canonical == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(canonical); err == nil {
		return strings.Trim(host, "[]")
	}
	return strings.Trim(canonical, "[]")
}
