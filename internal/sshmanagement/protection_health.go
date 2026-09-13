package sshmanagement

import (
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"slices"
)

// SameProtectionEndpoint compares the preserved configured SSH path with a
// resource freshly projected by ProtectableResources. Observation timestamps,
// process generations and socket inode changes are not configured topology.
// The current projection has already validated their authority and freshness.
func SameProtectionEndpoint(planned, current hostresources.ProtectableResource) bool {
	if planned.Kind != ProtectionResourceKind || planned.Owner != ProtectionResourceOwner ||
		current.Kind != ProtectionResourceKind || current.Owner != ProtectionResourceOwner ||
		planned.ID != current.ID || planned.Source != current.Source || planned.Protocol != current.Protocol ||
		planned.Listen != current.Listen || planned.Port != current.Port ||
		planned.Capabilities.ConfigRevision != current.Capabilities.ConfigRevision ||
		len(planned.Endpoints) == 0 || len(planned.Endpoints) != len(current.Endpoints) ||
		len(planned.ManagementEndpoints) == 0 || len(planned.ManagementEndpoints) != len(current.ManagementEndpoints) {
		return false
	}
	for _, endpoint := range planned.Endpoints {
		if !slices.ContainsFunc(current.Endpoints, func(live hostresources.PublicEndpoint) bool {
			return endpoint.ID == live.ID && endpoint.Key == live.Key
		}) {
			return false
		}
	}
	for _, endpoint := range planned.ManagementEndpoints {
		if !slices.ContainsFunc(current.ManagementEndpoints, func(live hostresources.ManagementEndpointV1) bool {
			return endpoint.ID == live.ID && endpoint.ResourceID == live.ResourceID && endpoint.Owner == live.Owner &&
				endpoint.ServiceKind == live.ServiceKind && endpoint.Network == live.Network && endpoint.Family == live.Family &&
				endpoint.Bind == live.Bind && endpoint.Port == live.Port && endpoint.ConfigurationRevision == live.ConfigurationRevision
		}) {
			return false
		}
	}
	return true
}
