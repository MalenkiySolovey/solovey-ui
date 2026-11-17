package validation

import (
	"errors"
	"strings"

	"golang.org/x/net/idna"
)

func ValidateHostname(host string) error {
	if host == "" {
		return nil
	}
	if strings.TrimSpace(host) != host {
		return errors.New("invalid hostname")
	}
	if strings.ContainsAny(host, "/\\:@?#[]") {
		return errors.New("invalid hostname")
	}

	ascii, err := idna.Lookup.ToASCII(host)
	if err != nil {
		return errors.New("invalid hostname")
	}
	if ascii == "" || len(ascii) > 253 {
		return errors.New("invalid hostname")
	}

	labels := strings.Split(ascii, ".")
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 {
			return errors.New("invalid hostname")
		}
		if strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return errors.New("invalid hostname")
		}
		for _, r := range label {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' {
				continue
			}
			return errors.New("invalid hostname")
		}
	}
	return nil
}
