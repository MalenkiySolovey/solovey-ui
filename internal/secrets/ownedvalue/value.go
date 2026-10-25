// Package ownedvalue adapts existing owner storage to the shared SecretRef
// contract. It owns no persistence, key provider, transport or reveal endpoint.
package ownedvalue

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"unicode/utf8"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/secretref"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/jsonvalue"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"github.com/MalenkiySolovey/solovey-ui/util/secretbox"
)

const MaxValueBytes = 1 << 20
const MaxEnvelopeBytes = 2 << 20

// View fingerprints ciphertext only. Historical plaintext storage has no
// rotation revision; its presence never discloses or hashes the credential.
func View(access secretref.Access, captured string) secretref.View {
	revision := ""
	if secretbox.IsEncrypted(captured) {
		digest := sha256.Sum256([]byte(captured))
		revision = hex.EncodeToString(digest[:])
	}
	return access.View(captured != "", revision)
}

// Access borrows exactly the captured owner value. The owner supplies its
// existing codec and synchronous authorization/audit admission. Nothing may
// retain the borrowed plaintext; an admitted runtime operation owns its scoped
// copy. Codec, admission and consumer errors never escape as secret-bearing text.
func Access(ctx context.Context, access secretref.Access, captured string,
	open func() ([]byte, error), admit func(secretref.View) error, use func([]byte) error,
) (secretref.View, error) {
	if ctx == nil || access.Validate() != nil || (access.Operation == secretref.UseForOwner) != (use != nil) {
		return secretref.View{}, secretref.ErrDenied
	}
	if len(captured) > MaxEnvelopeBytes {
		return secretref.View{}, secretref.ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return secretref.View{}, err
	}
	view := View(access, captured)
	if access.Operation == secretref.Presence {
		return view, nil
	}
	if open == nil || admit == nil {
		return view, secretref.ErrDenied
	}
	value, err := open()
	defer common.WipeBytes(value)
	if err != nil || len(value) > MaxValueBytes {
		return view, secretref.ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return view, err
	}
	if err := admit(view); err != nil {
		if errors.Is(err, secretref.ErrDenied) {
			return view, secretref.ErrDenied
		}
		return view, secretref.ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return view, err
	}
	if err := use(value); err != nil {
		return view, secretref.ErrUseFailed
	}
	return view, nil
}

// AAD binds a credential to its owner and immutable field reference. Owners
// derive the reference from trusted row identity and a fixed field declaration,
// never from a request-supplied owner, JSON path, SQL fragment or binary path.
func AAD(reference secretref.Reference) string {
	return "owner-secret:v1:" + reference.Owner + ":" + reference.ID
}

type mutation struct {
	Intent           string               `json:"intent"`
	Value            *string              `json:"value,omitempty"`
	Reference        *secretref.Reference `json:"reference,omitempty"`
	Present          *bool                `json:"present,omitempty"`
	RotationRevision *string              `json:"rotationRevision,omitempty"`
}

// Apply accepts explicit unchanged/replace/clear intents. The unchanged intent
// must match the exact owner reference, presence and captured ciphertext
// revision. A copied reference or stale editor cannot replace another field.
// Owners persist the returned envelope in their existing transaction; this
// helper never allocates a second secret store or writes a database row.
func Apply(reference secretref.Reference, current string, raw json.RawMessage,
	seal func(plaintext, associatedData string) (string, error),
) (string, error) {
	access := secretref.Access{Reference: reference, Actor: secretref.Actor{Component: reference.Owner}, Operation: secretref.Presence, IntentID: "credential-write"}
	if access.Validate() != nil || len(current) > MaxEnvelopeBytes || len(raw) > MaxEnvelopeBytes || jsonvalue.StrictObject(raw) != nil {
		return "", secretref.ErrDenied
	}
	var request mutation
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil {
		return "", secretref.ErrDenied
	}
	view := View(access, current)
	switch request.Intent {
	case "unchanged":
		if request.Value != nil || request.Reference == nil || request.Present == nil || request.RotationRevision == nil ||
			*request.Reference != reference || *request.Present != view.Present || *request.RotationRevision != view.RotationRevision {
			return "", secretref.ErrDenied
		}
		if current != "" && !secretbox.IsEncrypted(current) {
			return "", secretref.ErrUnavailable
		}
		return current, nil
	case "clear":
		if request.Value != nil || request.Reference != nil || request.Present != nil || request.RotationRevision != nil {
			return "", secretref.ErrDenied
		}
		return "", nil
	case "replace":
		if request.Reference != nil || request.Present != nil || request.RotationRevision != nil || request.Value == nil ||
			*request.Value == "" || !utf8.ValidString(*request.Value) || len(*request.Value) > MaxValueBytes || secretbox.IsEncrypted(*request.Value) || seal == nil {
			return "", secretref.ErrDenied
		}
		captured, err := seal(*request.Value, AAD(reference))
		if err != nil || !secretbox.IsEncrypted(captured) || len(captured) > MaxEnvelopeBytes {
			return "", secretref.ErrUnavailable
		}
		return captured, nil
	default:
		return "", secretref.ErrDenied
	}
}
