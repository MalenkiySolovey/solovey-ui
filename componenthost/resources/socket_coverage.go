package resources

import (
	canonical "github.com/MalenkiySolovey/solovey-host/componenthost/resources"
	"time"
)

type SocketCoverageV1 = canonical.SocketCoverageV1

func PreservationEndpointKeys(resource ProtectableResource, now time.Time) ([]PublicEndpointKey, bool) {
	return canonical.PreservationEndpointKeys(resource, now)
}
