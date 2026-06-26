package security

import (
	"net"
	"net/url"
	"strings"
)

func WebSocketOriginAllowed(originHeader, requestHost, webDomain string) (bool, string) {
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
	if requestHost != "" && originHostPort == CanonicalHostPort(requestHost) {
		return true, "request_host"
	}
	originHost := CanonicalHostname(originURL.Host)
	webDomainHost := CanonicalHostname(webDomain)
	if webDomainHost != "" && originHost == webDomainHost {
		return true, "web_domain"
	}
	if webDomainHostPort := CanonicalHostPort(webDomain); webDomainHostPort != "" && originHostPort == webDomainHostPort {
		return true, "web_domain"
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
	if parsed, err := url.Parse(value); err == nil && parsed.Host != "" {
		value = parsed.Host
	}
	if host, port, err := net.SplitHostPort(value); err == nil {
		return strings.TrimSuffix(strings.ToLower(strings.Trim(host, "[]")), ".") + ":" + port
	}
	return strings.TrimSuffix(strings.ToLower(strings.Trim(value, "[]")), ".")
}

func CanonicalHostname(value string) string {
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
