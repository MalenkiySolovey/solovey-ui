//go:build minimal

package app

import "github.com/MalenkiySolovey/solovey-ui/componenthost"

// WithSelectedObservationOwners adds no optional owners in the minimal profile.
func WithSelectedObservationOwners(owners componenthost.APIDeps) componenthost.APIDeps {
	return owners
}
