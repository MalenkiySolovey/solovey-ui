package redact

import (
	"strings"
	"testing"
)

func TestSnellCredentialsAreSensitive(t *testing.T) {
	for _, key := range []string{"psk", "userkey", "user-key"} {
		if !IsSensitiveKey(key) {
			t.Fatal("Snell credential key is not redacted")
		}
		result := Value(map[string]any{key: "fixture-only"}).(map[string]any)
		if result[key] != Marker {
			t.Fatal("structured credential leaked")
		}
		if strings.Contains(String(key+"=fixture-only"), "fixture-only") {
			t.Fatal("text credential leaked")
		}
	}
}
