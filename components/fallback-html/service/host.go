//go:build !minimal

package service

import "context"

// HostFacts supplies only the configured host facts consumed by site selection
// and safety. These facts grant neither bind authority nor runtime health.
type HostFacts interface {
	CurrentWebTarget(context.Context) (TargetView, error)
	ReservedPaths(context.Context) ([]string, error)
	InboundPorts(context.Context) ([]PortCandidate, error)
}
