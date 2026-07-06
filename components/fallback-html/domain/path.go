//go:build !minimal

package domain

import (
	"fmt"
	"net/netip"
	"net/url"
	"path"
	"strings"
)

var fixedReservedPrefixes = []string{
	"/api/",
	"/apiv2/",
	"/assets/",
	"/.well-known/acme-challenge/",
}

func NormalizePublicPath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = "/"
	}
	if strings.Contains(value, "\\") {
		return "", fmt.Errorf("path must use URL slashes")
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return "", fmt.Errorf("invalid public path: %w", err)
	}
	if parsed.IsAbs() || parsed.Host != "" {
		return "", fmt.Errorf("public path must be relative to this site")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("public path must not contain query or fragment")
	}
	for _, segment := range strings.Split(parsed.Path, "/") {
		if segment == "." || segment == ".." {
			return "", fmt.Errorf("public path must not contain traversal segments")
		}
	}
	cleaned := path.Clean("/" + strings.TrimPrefix(parsed.Path, "/"))
	if cleaned == "." {
		cleaned = "/"
	}
	if cleaned != "/" && !strings.HasSuffix(cleaned, "/") && !strings.Contains(path.Base(cleaned), ".") {
		cleaned += "/"
	}
	return cleaned, nil
}

func ValidatePagePath(value string, reserved []string) (string, error) {
	normalized, err := NormalizePublicPath(value)
	if err != nil {
		return "", err
	}
	if IsReservedPublicPath(normalized, reserved) {
		return "", fmt.Errorf("public path %q conflicts with a reserved panel path", normalized)
	}
	return normalized, nil
}

func NormalizeRedirectTarget(value string, reserved []string) (target string, external bool, err error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false, fmt.Errorf("redirect target is required")
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return "", false, fmt.Errorf("invalid redirect target: %w", err)
	}
	if parsed.IsAbs() || parsed.Host != "" {
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return "", false, fmt.Errorf("external redirect must use http or https")
		}
		if parsed.Host == "" {
			return "", false, fmt.Errorf("external redirect must include host")
		}
		if parsed.User != nil {
			return "", false, fmt.Errorf("external redirect must not include userinfo")
		}
		host := parsed.Hostname()
		if isLocalRedirectHost(host) {
			return "", false, fmt.Errorf("external redirect host is not public")
		}
		parsed.Scheme = strings.ToLower(parsed.Scheme)
		parsed.Host = strings.ToLower(parsed.Host)
		return parsed.String(), true, nil
	}
	target, err = ValidatePagePath(value, reserved)
	if err != nil {
		return "", false, err
	}
	return target, false, nil
}

func ValidateRedirectStatus(value int) (int, error) {
	switch value {
	case 0:
		return 302, nil
	case 301, 302, 307, 308:
		return value, nil
	default:
		return 0, fmt.Errorf("redirect status must be one of 301, 302, 307, 308")
	}
}

func IsReservedPublicPath(value string, reserved []string) bool {
	normalized, err := NormalizePublicPath(value)
	if err != nil {
		return true
	}
	for _, prefix := range append(append([]string{}, fixedReservedPrefixes...), reserved...) {
		prefix, err = NormalizePublicPath(prefix)
		if err != nil || prefix == "/" {
			continue
		}
		if normalized == strings.TrimSuffix(prefix, "/") || strings.HasPrefix(normalized, prefix) {
			return true
		}
	}
	return false
}

func PublicPathToFilePath(value string) string {
	normalized, _ := NormalizePublicPath(value)
	if normalized == "/" {
		return "index.html"
	}
	if strings.HasSuffix(normalized, "/") {
		return strings.TrimPrefix(normalized, "/") + "index.html"
	}
	return strings.TrimPrefix(normalized, "/")
}

func isLocalRedirectHost(host string) bool {
	host = strings.Trim(strings.ToLower(strings.TrimSpace(host)), "[]")
	if host == "" {
		return true
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") {
		return true
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified()
	}
	return false
}
