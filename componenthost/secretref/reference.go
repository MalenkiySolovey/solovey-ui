// Package secretref preserves the public scoped-secret integration path.
// Storage, encryption and authorization remain with their existing owners.
package secretref

import canonical "github.com/MalenkiySolovey/solovey-host/secretref"

type Operation = canonical.Operation

const (
	Presence         = canonical.Presence
	UseForOwner      = canonical.UseForOwner
	OwnerUsePresence = canonical.OwnerUsePresence
	Masked           = canonical.Masked
)

var (
	ErrDenied      = canonical.ErrDenied
	ErrUnavailable = canonical.ErrUnavailable
	ErrUseFailed   = canonical.ErrUseFailed
)

type Reference = canonical.Reference
type Actor = canonical.Actor
type Access = canonical.Access
type View = canonical.View

func NewReference(owner, id string) Reference { return canonical.NewReference(owner, id) }
