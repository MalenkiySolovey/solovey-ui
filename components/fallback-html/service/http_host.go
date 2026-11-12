//go:build !minimal

package service

import (
	"context"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

// HTTPHostFacts reports existing transient port and configured TLS facts.
// These checks never produce a PublicationBinding or reservation.
type HTTPHostFacts interface {
	PortAvailability(context.Context, string, int) (string, string)
	FreeLoopbackPort(context.Context, bool) (PortCandidate, bool)
	AddressFallback(context.Context, string) (bool, string)
	TLSConfigured(context.Context) bool
}

// HTTPBindingScope identifies one verified publication candidate. It grants no
// arbitrary address, certificate, storage or reservation authority.
type HTTPBindingScope struct {
	Resource            hostresources.ResourceIdentityV1
	PublicationRevision string
	ExpectedGeneration  uint64
}

// PublicationBinding is issued by the Backend for the exact targets read in
// the existing publication transaction. Bind consumes that admitted candidate
// once; a declaration or availability probe cannot construct one.
type PublicationBinding interface {
	Scope() HTTPBindingScope
	Bind(context.Context, HTTPBindingScope) error
}

// HTTPServing observes and drains the existing serving owner. Drain neither
// unpublishes a revision nor releases durable target reservations or files.
type HTTPServing interface {
	Owns(string, int) bool
	OwnsKey(string) bool
	TLSAcceptsServerName(string, int, string) bool
	Drain(context.Context) error
}
