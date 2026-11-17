// Package ownedvalue forwards to the canonical Backend owner envelope.
package ownedvalue

import (
	"context"
	"encoding/json"
	"github.com/MalenkiySolovey/solovey-host/secretref"
	canonical "github.com/MalenkiySolovey/solovey-ui/backend/secrets/ownedvalue"
)

const MaxValueBytes = canonical.MaxValueBytes
const MaxEnvelopeBytes = canonical.MaxEnvelopeBytes

func View(access secretref.Access, captured string) secretref.View {
	return canonical.View(access, captured)
}
func AAD(ref secretref.Reference) string { return canonical.AAD(ref) }
func Access(ctx context.Context, access secretref.Access, captured string, open func() ([]byte, error), admit func(secretref.View) error, use func([]byte) error) (secretref.View, error) {
	return canonical.Access(ctx, access, captured, open, admit, use)
}
func Apply(ref secretref.Reference, current string, raw json.RawMessage, seal func(string, string) (string, error)) (string, error) {
	return canonical.Apply(ref, current, raw, seal)
}
