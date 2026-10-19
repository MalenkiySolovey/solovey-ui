// Package secretref describes access to existing owner secret storage. It owns
// no storage, crypto, authentication endpoint or raw-reveal operation.
package secretref

import (
	"errors"
	"strings"

	"github.com/MalenkiySolovey/solovey-ui/internal/ops/operationcoordination"
)

type Operation string

const (
	Presence         Operation = "PRESENCE"
	UseForOwner      Operation = "USE_FOR_OWNER_OPERATION"
	OwnerUsePresence           = "OWNER_USE_AND_PRESENCE"
	Masked                     = "MASKED"
)

var (
	ErrDenied      = errors.New("secret_access_denied")
	ErrUnavailable = errors.New("secret_unavailable")
	ErrUseFailed   = errors.New("secret_owner_use_failed")
)

type Reference struct {
	Owner      string `json:"owner"`
	ID         string `json:"id"`
	Policy     string `json:"policy"`
	Projection string `json:"projection"`
}

func NewReference(owner, id string) Reference {
	return Reference{Owner: owner, ID: id, Policy: OwnerUsePresence, Projection: Masked}
}

// Actor is supplied by trusted host composition, never decoded from an HTTP
// request. Components in this process are trusted code, not a security sandbox.
type Actor struct {
	Component string `json:"component"`
}
type Access struct {
	Reference Reference
	Actor     Actor
	Operation Operation
	IntentID  string
}

func (a Access) Validate() error {
	if !token(a.Reference.Owner, 96) || !token(a.Reference.ID, 128) || !token(a.IntentID, 96) ||
		a.Actor.Component != a.Reference.Owner || a.Reference.Policy != OwnerUsePresence || a.Reference.Projection != Masked ||
		(a.Operation != Presence && a.Operation != UseForOwner) {
		return ErrDenied
	}
	return nil
}

type View struct {
	Reference Reference `json:"reference"`
	Present   bool      `json:"present"`
	// Ciphertext identity only. Empty for legacy plaintext/absent values; it
	// never fingerprints plaintext or invents a rotation timestamp/history.
	RotationRevision string                            `json:"rotationRevision,omitempty"`
	Correlation      operationcoordination.Correlation `json:"correlation"`
}

func (a Access) View(present bool, rotationRevision string) View {
	if a.Validate() != nil {
		return View{}
	}
	if len(rotationRevision) != 64 || strings.Trim(rotationRevision, "0123456789abcdef") != "" {
		rotationRevision = ""
	}
	return View{Reference: a.Reference, Present: present, RotationRevision: rotationRevision,
		Correlation: operationcoordination.NewReference(a.Reference.Owner, "secret:"+a.Reference.ID, a.IntentID).Correlation()}
}

func token(value string, max int) bool {
	return value != "" && len(value) <= max && strings.Trim(value, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._:-") == ""
}
