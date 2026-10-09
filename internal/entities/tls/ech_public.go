package entitytls

import (
	"encoding/pem"
	"errors"
	"strings"
)

// PublicECHConfig preserves public ECH configuration bytes and line boundaries.
// Server key payloads and malformed metadata never enter a public projection.
func PublicECHConfig(value any) (string, error) {
	var lines []string
	switch config := value.(type) {
	case nil:
		return "", nil
	case string:
		lines = []string{config}
	case []string:
		lines = config
	case []any:
		for _, line := range config {
			text, ok := line.(string)
			if !ok {
				return "", errors.New("ECH public configuration must contain text")
			}
			lines = append(lines, text)
		}
	default:
		return "", errors.New("ECH public configuration must contain text")
	}
	text := strings.Join(lines, "\n")
	if strings.Contains(text, "PRIVATE KEY") || strings.Contains(text, "ECH KEYS") {
		return "", errors.New("private ECH material is unavailable in public export")
	}
	for rest := text; ; {
		start := strings.Index(rest, "-----BEGIN ")
		if start < 0 {
			if strings.Contains(rest, "-----END ") {
				return "", errors.New("malformed ECH public PEM payload")
			}
			break
		}
		if strings.Contains(rest[:start], "-----END ") {
			return "", errors.New("malformed ECH public PEM payload")
		}
		payload := rest[start:]
		end := strings.Index(payload, "-----END ")
		if end < 0 || strings.Contains(payload[len("-----BEGIN "):end], "-----BEGIN ") {
			return "", errors.New("malformed ECH public PEM payload")
		}
		endMarker := strings.Index(payload[end+len("-----END "):], "-----")
		if endMarker < 0 {
			return "", errors.New("malformed ECH public PEM payload")
		}
		end += len("-----END ") + endMarker + len("-----")
		block, trailing := pem.Decode([]byte(payload[:end]))
		if block == nil || len(trailing) != 0 {
			return "", errors.New("malformed ECH public PEM payload")
		}
		if block.Type != "ECH CONFIGS" {
			return "", errors.New("unsupported ECH public PEM payload")
		}
		rest = payload[end:]
	}
	return text, nil
}
