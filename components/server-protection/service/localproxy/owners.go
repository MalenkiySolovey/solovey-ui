package localproxy

import (
	"context"
	"time"

	componenthealth "github.com/MalenkiySolovey/solovey-host/componenthost/health"
	hostsurface "github.com/MalenkiySolovey/solovey-host/componenthost/hostsurface"
	hostresources "github.com/MalenkiySolovey/solovey-host/componenthost/resources"
)

// ProviderDirectory resolves current facts and the existing lease authority.
// Registration, lease storage and runtime execution remain with that owner.
type ProviderDirectory interface {
	FactsV1(context.Context, time.Time) ([]hostresources.LocalProxyFactV1, error)
	ResolveV1(context.Context, hostresources.LocalProxyReferenceV1, time.Time) (hostresources.LocalProxyFactV1, error)
	Provider(string) (hostresources.LocalProxyProviderV1, bool)
	LeasesByHolderV1(context.Context, string) ([]hostresources.LocalProxyGuardLeaseV1, error)
}

type ProbeSource interface {
	Capability(context.Context, componenthealth.LocalProxyProbeTargetV1) componenthealth.LocalProxyProbeCapabilityV1
	ProbeFresh(context.Context, componenthealth.LocalProxyProbeRequestV1) (componenthealth.LocalProxyProbeObservationV1, error)
}

type SurfaceSource interface {
	Reconcile(context.Context) hostsurface.Snapshot
}
