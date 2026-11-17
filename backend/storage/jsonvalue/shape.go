// Package jsonvalue owns JSON shape checks shared by persisted entity domains.
package jsonvalue

import (
	"bytes"
	"encoding/json"
	"fmt"
)

func OptionalObject(label string, raw json.RawMessage) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	var value map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &value); err != nil || value == nil {
		return fmt.Errorf("%s must be a JSON object", label)
	}
	return nil
}

func OptionalArray(label string, raw json.RawMessage) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	var value []json.RawMessage
	if err := json.Unmarshal(trimmed, &value); err != nil || value == nil {
		return fmt.Errorf("%s must be a JSON array", label)
	}
	return nil
}
