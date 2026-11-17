package validation

import (
	"errors"
	"fmt"
	"strings"
)

// reservedPathPrefixes are framework-owned routes that custom web/sub paths
// must not shadow. `/assets/` stays reserved even when webPath is `/`, because
// the embedded frontend serves hashed chunks from that absolute route.
var reservedPathPrefixes = []string{
	"/api/",
	"/apiv2/",
	"/ws",
	"/assets/",
	"/sub/",
	"/json/",
	"/clash/",
	"/xray/",
}

func validatePath(path string, reserved []string) error {
	if path == "" {
		return nil
	}
	if !strings.HasPrefix(path, "/") {
		return errors.New("invalid path")
	}
	if strings.Contains(path, "\\") ||
		strings.Contains(path, "..") ||
		strings.Contains(path, "//") ||
		strings.Contains(path, ":") ||
		strings.Contains(path, "*") {
		return errors.New("invalid path")
	}
	for _, r := range path {
		if r == 0 || r < 0x20 || r == 0x7f {
			return errors.New("invalid path")
		}
	}
	for _, prefix := range reserved {
		if hasReservedPathPrefix(path, prefix) {
			return fmt.Errorf("reserved path prefix: %s", prefix)
		}
	}
	return nil
}

func hasReservedPathPrefix(path string, prefix string) bool {
	if prefix == "" {
		return false
	}
	path = strings.ToLower(path)
	prefix = strings.ToLower(prefix)
	if strings.HasSuffix(prefix, "/") {
		return path == strings.TrimSuffix(prefix, "/") || strings.HasPrefix(path, prefix)
	}
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}
