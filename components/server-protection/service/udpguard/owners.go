package udpguard

import (
	"context"
	"time"

	componenthealth "github.com/MalenkiySolovey/solovey-host/componenthost/health"
	hostresources "github.com/MalenkiySolovey/solovey-host/componenthost/resources"
)

type TransportSource interface {
	Facts(context.Context, time.Time) ([]hostresources.InboundTransportCapabilityV2, error)
}

type ProbeSource interface {
	Capability(context.Context, componenthealth.ProtocolProbeTargetV1) componenthealth.ProtocolProbeCapabilityV1
	ProbeFresh(context.Context, componenthealth.ProtocolProbeRequestV1) (componenthealth.ProtocolProbeObservationV1, error)
}
