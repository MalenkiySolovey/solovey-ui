package validation

import (
	"errors"
	"net/netip"
	"net/url"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/backend/security/ssrf"
)

func ValidateOptionalHTTPURL(value string) error {
	if containsControlCharacter(value) {
		return errors.New("invalid URL setting")
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" {
		return errors.New("invalid URL setting")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("invalid URL setting")
	}
	if parsed.User != nil {
		return errors.New("invalid URL setting")
	}
	if host := parsed.Hostname(); host != "" {
		if addr, err := netip.ParseAddr(host); err == nil && ssrf.IsBlockedAddr(addr) {
			return errors.New("invalid URL setting")
		}
	}
	if strings.Contains(value, "#") || parsed.Fragment != "" || parsed.RawFragment != "" {
		return errors.New("invalid URL setting")
	}
	if containsControlCharacter(parsed.Path) ||
		containsControlCharacter(parsed.RawPath) ||
		containsControlCharacter(parsed.RawQuery) ||
		containsQueryControlCharacter(parsed) {
		return errors.New("invalid URL setting")
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
