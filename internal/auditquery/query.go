package auditquery

import (
	"strconv"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

func Limit(raw string, defaultValue, maximum int) (int, error) {
	if raw == "" {
		return defaultValue, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 {
		return 0, common.NewError("invalid limit")
	}
	if limit > maximum {
		return maximum, nil
	}
	return limit, nil
}

func Cursor(raw string) (uint64, error) {
	if raw == "" {
		return 0, nil
	}
	cursor, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, common.NewError("invalid cursor")
	}
	return cursor, nil
}

func Event(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}
	if len(value) > 64 {
		return "", common.NewError("invalid event filter")
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' || r == ':' {
			continue
		}
		return "", common.NewError("invalid event filter")
	}
	return value, nil
}

func Severity(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	switch value {
	case "", "info", "warn":
		return value, nil
	default:
		return "", common.NewError("invalid severity filter")
	}
}

func UnixSeconds(name, raw string) (int64, error) {
	if raw == "" {
		return 0, nil
	}
	if len(raw) > 10 {
		return 0, common.NewError("invalid " + name)
	}
	for _, r := range raw {
		if r < '0' || r > '9' {
			return 0, common.NewError("invalid " + name)
		}
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, common.NewError("invalid " + name)
	}
	return value, nil
}
