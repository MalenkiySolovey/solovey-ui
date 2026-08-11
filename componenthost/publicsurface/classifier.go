package publicsurface

import (
	"net/url"
	"strings"
	"unicode/utf8"
)

const maxClassifiedInputBytes = 4096

func ClassifyPath(raw string, reserved bool) string {
	if reserved {
		return "reserved_panel"
	}
	if !utf8.ValidString(raw) {
		return "invalid_utf8"
	}
	if len(raw) > maxClassifiedInputBytes {
		return "overlong_uri"
	}
	parsed, err := url.ParseRequestURI(raw)
	if err != nil {
		return "invalid_uri"
	}
	value := strings.ToLower(parsed.Path)
	for _, markers := range [][]string{
		{"/.env", "/.git", "/config.json"},
		{"/wp-admin", "/wp-login", "/wordpress", "/xmlrpc.php"},
		{"/phpmyadmin", "/vendor/phpunit", ".php"},
		{"/cgi-bin", "/boaform"},
		{"/actuator", "/server-status", "/geoserver", "/login.action"},
	} {
		for _, marker := range markers {
			if strings.Contains(value, marker) {
				return "scanner_path"
			}
		}
	}
	for _, suffix := range []string{".css", ".js", ".png", ".jpg", ".jpeg", ".ico", ".woff", ".woff2"} {
		if strings.HasSuffix(value, suffix) {
			return "static_asset"
		}
	}
	return "fallback_path"
}

func ClassifyUserAgent(raw string) string {
	if raw == "" {
		return "ua_empty"
	}
	if !utf8.ValidString(raw) {
		return "invalid_utf8"
	}
	if len(raw) > maxClassifiedInputBytes {
		return "ua_overlong"
	}
	lower := strings.ToLower(raw)
	for _, marker := range []string{"curl", "wget", "python-requests", "go-http-client", "masscan", "zgrab", "nmap", "nikto", "sqlmap", "acunetix", "nessus"} {
		if strings.Contains(lower, marker) {
			return "ua_scanner"
		}
	}
	if strings.Contains(lower, "mozilla/") || strings.Contains(lower, "applewebkit/") {
		return "ua_browser_like"
	}
	if strings.Contains(lower, "bot") || strings.Contains(lower, "crawler") || strings.Contains(lower, "spider") {
		return "ua_bot"
	}
	return "ua_other"
}

func ClassifyMethod(raw string) string {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "GET", "HEAD":
		return "get"
	case "POST":
		return "post"
	case "OPTIONS":
		return "other"
	default:
		return "unexpected"
	}
}

func ClassifyStatus(status int) string {
	switch {
	case status >= 200 && status < 300:
		return "2xx"
	case status >= 300 && status < 400:
		return "3xx"
	case status >= 400 && status < 500:
		return "4xx"
	case status >= 500 && status < 600:
		return "5xx"
	default:
		return "none"
	}
}

func ClassifyBytes(value int64) string {
	switch {
	case value <= 0:
		return "zero"
	case value < 1024:
		return "small"
	case value < 64*1024:
		return "medium"
	default:
		return "large"
	}
}

func ClassifyDuration(milliseconds int64) string {
	switch {
	case milliseconds < 10:
		return "instant"
	case milliseconds < 100:
		return "fast"
	case milliseconds < 1000:
		return "normal"
	default:
		return "slow"
	}
}
