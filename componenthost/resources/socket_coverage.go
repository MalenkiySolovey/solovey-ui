package resources

import (
	"time"

	hostfacts "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
)

// SocketCoverageV1 retains authenticated socket coverage projected by a
// semantic owner. Configured bind shape alone never supplies this evidence.
type SocketCoverageV1 struct {
	Socket                hostfacts.ListenerSocketIdentityV1 `json:"socket"`
	OwnerRevision         string                             `json:"ownerRevision"`
	ConfigurationRevision string                             `json:"configurationRevision"`
	AuthorityRevision     string                             `json:"authorityRevision"`
	ObservedAt            int64                              `json:"observedAt"`
	ExpiresAt             int64                              `json:"expiresAt"`
}

func (f SocketCoverageV1) CurrentFor(resource ProtectableResource, now time.Time) bool {
	return hostfacts.ValidListenerSocketIdentity(f.Socket) &&
		validManagementRevision(f.OwnerRevision) && validManagementRevision(f.ConfigurationRevision) && validManagementRevision(f.AuthorityRevision) &&
		f.OwnerRevision == resource.Capabilities.OwnerRevision && f.ConfigurationRevision == resource.Capabilities.ConfigRevision &&
		f.ObservedAt > 0 && f.ObservedAt <= now.Unix() && f.ExpiresAt > now.Unix() && f.ExpiresAt <= f.ObservedAt+120 &&
		string(f.Socket.Network) == string(NetworkForProtocol(resource.Protocol)) && int(f.Socket.Port) == resource.Port &&
		(f.Socket.Bind == NormalizeListen(resource.Listen).Value ||
			resource.Listen == "*" && f.Socket.Wildcard && len(f.Socket.CoverageFamilies) == 2)
}

// PreservationEndpointKeys keeps configured coverage conservative unless the
// resource's owner has projected an exact current socket partition. Such a
// partition must not be widened back to both families merely from an IPv6 bind.
func PreservationEndpointKeys(resource ProtectableResource, now time.Time) ([]PublicEndpointKey, bool) {
	coverage := resource.SocketCoverage
	if coverage == nil {
		return DeterministicConfiguredEndpointKeys(resource)
	}
	if !coverage.CurrentFor(resource, now) {
		return nil, false
	}
	result := make([]PublicEndpointKey, 0, len(coverage.Socket.CoverageFamilies))
	for _, family := range coverage.Socket.CoverageFamilies {
		bind := coverage.Socket.Bind
		if coverage.Socket.Wildcard {
			if family == hostfacts.FamilyIPv4 {
				bind = "0.0.0.0"
			} else {
				bind = "::"
			}
		}
		result = append(result, PublicEndpointKey{Network: Network(coverage.Socket.Network), AddressFamily: AddressFamily(family), BindAddress: bind, Port: coverage.Socket.Port})
	}
	return result, true
}
