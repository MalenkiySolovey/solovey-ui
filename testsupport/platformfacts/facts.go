// Package platformfacts exposes the existing owner's value types to integration
// fixtures. It provides no platform observation or execution capability.
package platformfacts

import owner "github.com/MalenkiySolovey/solovey-ui/internal/deployment"

type PlatformFact = owner.PlatformFact

const (
	Available = owner.Available
	Unknown   = owner.Unknown
)
