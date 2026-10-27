// Package calloptions shares Call value validation; credentials remain in each
// existing inbound/outbound owner row and use the common SecretRef adapter.
package calloptions

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/internal/entities/jsonvalue"
)

func CredentialValid(field, value string) bool {
	if len(value) == 0 || len(value) > 64<<10 || strings.ContainsRune(value, 0) {
		return false
	}
	if field == "password" {
		return true
	}
	if field == "join_link" {
		if len(value) > 4096 || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\r\n") {
			return false
		}
		return true
	}
	var cookies []json.RawMessage
	if json.Unmarshal([]byte(value), &cookies) != nil || len(cookies) == 0 || len(cookies) > 128 {
		return false
	}
	seen := make(map[string]bool, len(cookies))
	for _, raw := range cookies {
		var cookie struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if jsonvalue.StrictObject(raw) != nil || decoder.Decode(&cookie) != nil || seen[cookie.Name] || (&http.Cookie{Name: cookie.Name, Value: cookie.Value}).Valid() != nil {
			return false
		}
		seen[cookie.Name] = true
	}
	return true
}
