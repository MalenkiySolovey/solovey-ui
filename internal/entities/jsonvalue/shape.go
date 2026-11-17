// Package jsonvalue preserves persisted entity JSON validation through Backend.
package jsonvalue

import (
	"encoding/json"
	canonical "github.com/MalenkiySolovey/solovey-ui/backend/storage/jsonvalue"
)

const MaxStrictDepth = canonical.MaxStrictDepth
const MaxStrictMembers = canonical.MaxStrictMembers

func StrictObject(raw json.RawMessage) error { return canonical.StrictObject(raw) }
func OptionalObject(label string, raw json.RawMessage) error {
	return canonical.OptionalObject(label, raw)
}
func OptionalArray(label string, raw json.RawMessage) error {
	return canonical.OptionalArray(label, raw)
}
