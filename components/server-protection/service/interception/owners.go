package interception

import (
	"context"
	"time"

	hostresources "github.com/MalenkiySolovey/solovey-host/componenthost/resources"
)

type InterceptionSource interface {
	FactsV1(context.Context, time.Time) ([]hostresources.InterceptionInboundFactV1, error)
	ResolveV1(context.Context, hostresources.InterceptionReferenceV1, time.Time) (hostresources.InterceptionInboundFactV1, error)
}

type IngressScopeSource interface {
	FactsV1(context.Context, time.Time) ([]hostresources.ForwardedIngressScopeFactV1, error)
}

// ProbeInventory reports availability; it cannot perform or authorize mutation.
type ProbeInventory interface{ ProviderCount() int }
