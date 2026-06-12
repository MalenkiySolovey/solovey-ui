package service

import (
	"net/netip"
	"net/url"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"github.com/MalenkiySolovey/solovey-ui/util/ssrf"
)

func validateOptionalHTTPURL(value string) error {
	if containsControlCharacter(value) {
		return common.NewError("invalid URL setting")
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" {
		return common.NewError("invalid URL setting")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return common.NewError("invalid URL setting")
	}
	if parsed.User != nil {
		return common.NewError("invalid URL setting")
	}
	if host := parsed.Hostname(); host != "" {
		if addr, err := netip.ParseAddr(host); err == nil && ssrf.IsBlockedAddr(addr) {
			return common.NewError("invalid URL setting")
		}
	}
	if strings.Contains(value, "#") || parsed.Fragment != "" || parsed.RawFragment != "" {
		return common.NewError("invalid URL setting")
	}
	if containsControlCharacter(parsed.Path) ||
		containsControlCharacter(parsed.RawPath) ||
		containsControlCharacter(parsed.RawQuery) ||
		containsQueryControlCharacter(parsed) {
		return common.NewError("invalid URL setting")
	}
	return nil
}

func containsControlCharacter(value string) bool {
	return strings.ContainsFunc(value, func(r rune) bool {
		return r < 0x20 || r == 0x7f
	})
}

func containsQueryControlCharacter(parsed *url.URL) bool {
	for _, part := range strings.Split(parsed.RawQuery, "&") {
		key, value, _ := strings.Cut(part, "=")
		if queryComponentHasControl(key) || queryComponentHasControl(value) {
			return true
		}
	}
	return false
}

func queryComponentHasControl(value string) bool {
	decoded, err := url.QueryUnescape(value)
	if err != nil {
		return false
	}
	return containsControlCharacter(decoded)
}
