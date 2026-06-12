package importxui

import (
	"fmt"
	"strings"
)

// toAnySlice coerces a value to []any (nil when it is not a slice).
func toAnySlice(v any) []any {
	if s, ok := v.([]any); ok {
		return s
	}
	return nil
}

func stringList(value any) []string {
	var result []string
	switch v := value.(type) {
	case []any:
		for _, item := range v {
			if s := strings.TrimSpace(fmt.Sprint(item)); s != "" {
				result = append(result, s)
			}
		}
	case []string:
		result = append(result, v...)
	case string:
		if strings.TrimSpace(v) != "" {
			result = append(result, strings.TrimSpace(v))
		}
	}
	return result
}

func appendString(value any, item string) []string {
	if existing, ok := value.([]string); ok {
		return append(existing, item)
	}
	return []string{item}
}
