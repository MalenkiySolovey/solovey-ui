package sshmanagement

import (
	"errors"
	"sort"
	"strconv"
	"time"

	hostfacts "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

const (
	ProtectionResourceOwner  = "ssh-management"
	ProtectionResourceKind   = "ssh_management"
	protectionResourceSource = "ssh-management:listener-authority"
)

// ProtectionResourceID derives the stable resource identity from the SSH
// owner's configured endpoint identities. Runtime-local supervisor keys,
// process generations and socket cookies deliberately do not become resource
// identity.
func ProtectionResourceID(authority SSHListenerAuthorityV1) string {
	ids := normalizedAuthorityEndpointIDs(authority.EndpointIDs)
	if len(ids) == 0 || len(ids) != len(authority.EndpointIDs) {
		return ""
	}
	return ProtectionResourceOwner + ":" + Revision(ids)[:32]
}

// ProtectableResources projects only current, sealed listener authorities.
// It is the SSH semantic owner's resource view; consumers must not rediscover
// Dropbear/OpenSSH or infer SSH from a port or executable name.
func ProtectableResources(posture SSHPostureV1, now time.Time) ([]hostresources.ProtectableResource, error) {
	now = now.UTC()
	if err := posture.Validate(now); err != nil {
		return nil, err
	}
	endpoints := make(map[string]hostresources.ManagementEndpointV1, len(posture.Endpoints))
	for _, endpoint := range posture.Endpoints {
		endpoints[endpoint.ID] = endpoint
	}
	claimedEndpoints := make(map[string]bool, len(posture.Endpoints))
	seenResources := make(map[string]bool, len(posture.ListenerAuthorities))
	result := make([]hostresources.ProtectableResource, 0, len(posture.ListenerAuthorities))
	for _, authority := range posture.ListenerAuthorities {
		resourceID := ProtectionResourceID(authority)
		if resourceID == "" || seenResources[resourceID] {
			return nil, errors.New("SSH listener resource identity is ambiguous")
		}
		for _, endpointID := range authority.EndpointIDs {
			endpoint, ok := endpoints[endpointID]
			if !ok || claimedEndpoints[endpointID] || !authorityCoversManagementEndpoint(authority, endpoint) {
				return nil, errors.New("SSH listener authority endpoint binding is ambiguous")
			}
			claimedEndpoints[endpointID] = true
		}
		seenResources[resourceID] = true
		listen := authority.Socket.Bind
		families := append([]hostfacts.Family(nil), authority.Socket.CoverageFamilies...)
		if authority.Socket.Wildcard && containsSSHFamily(families, hostfacts.FamilyIPv4) && containsSSHFamily(families, hostfacts.FamilyIPv6) {
			listen = "*"
		}
		resource := hostresources.ProtectableResource{
			ID: resourceID, Kind: ProtectionResourceKind, Owner: ProtectionResourceOwner,
			Name: "SSH management " + strconv.Itoa(int(authority.Socket.Port)), Protocol: "tcp",
			Listen: listen, Port: int(authority.Socket.Port), TLS: false, Source: protectionResourceSource,
			Capabilities: hostresources.ProtectableResourceCapabilities{
				Known: true, AcceptsProxyProtocol: hostresources.CapabilityNo,
				SupportsGracefulDrain: hostresources.CapabilityNo, CanServeFallback: hostresources.CapabilityNo,
				RequiresACMEHTTP01: hostresources.CapabilityNo, RequiresTLSALPN01: hostresources.CapabilityNo,
				OwnerRevision: posture.SemanticRevision, ConfigRevision: authority.ConfigurationRevision,
			},
		}
		resource.ListenIntent = hostresources.BuildConfiguredListenIntent(resource)
		resource.SocketCoverage = &hostresources.SocketCoverageV1{Socket: authority.Socket,
			OwnerRevision: posture.SemanticRevision, ConfigurationRevision: authority.ConfigurationRevision,
			AuthorityRevision: authority.Revision, ObservedAt: authority.ObservedAt, ExpiresAt: authority.ExpiresAt}
		for _, id := range authority.EndpointIDs {
			resource.ManagementEndpoints = append(resource.ManagementEndpoints, currentManagementEndpoint(endpoints[id], posture, authority))
		}
		if listen == "*" {
			resource.ListenIntent.Mode = hostresources.ListenIntentDualStack
			resource.ListenIntent.RequiredFamilies = []hostresources.AddressFamily{hostresources.AddressFamilyIPv4, hostresources.AddressFamilyIPv6}
		}
		for _, family := range families {
			bind := authority.Socket.Bind
			if authority.Socket.Wildcard {
				switch family {
				case hostfacts.FamilyIPv4:
					bind = "0.0.0.0"
				case hostfacts.FamilyIPv6:
					bind = "::"
				default:
					return nil, errors.New("SSH listener authority has an unknown address family")
				}
			}
			endpointResource := resource
			endpointResource.Listen = bind
			endpoint := hostresources.BuildEndpointFact(endpointResource, hostresources.NetworkTCP, time.Unix(authority.ObservedAt, 0).UTC())
			endpoint.TLS = hostresources.CapabilityNo
			endpoint.Reality = hostresources.CapabilityUnknown
			endpoint.AuthenticationExpected = hostresources.CapabilityYes
			endpoint.FallbackSupported = hostresources.CapabilityNo
			endpoint.ProxyProtocol = hostresources.CapabilityNo
			resource.Endpoints = append(resource.Endpoints, endpoint)
		}
		if len(resource.Endpoints) == 0 {
			return nil, errors.New("SSH listener resource has no address-family coverage")
		}
		result = append(result, resource)
	}
	if len(claimedEndpoints) != len(endpoints) {
		return nil, errors.New("SSH listener authorities do not exactly partition configured endpoints")
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

// CurrentManagementEndpoints preserves the stable configured endpoint ID but
// binds its read projection to the current authenticated owner generation.
// Workflow posture remains the original provider fact.
func CurrentManagementEndpoints(posture SSHPostureV1, now time.Time) ([]hostresources.ManagementEndpointV1, error) {
	if err := posture.Validate(now); err != nil {
		return nil, err
	}
	result := make([]hostresources.ManagementEndpointV1, 0, len(posture.Endpoints))
	for _, authority := range posture.ListenerAuthorities {
		for _, id := range authority.EndpointIDs {
			endpoint, _ := endpointByID(posture.Endpoints, id)
			result = append(result, currentManagementEndpoint(endpoint, posture, authority))
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func currentManagementEndpoint(endpoint hostresources.ManagementEndpointV1, posture SSHPostureV1, authority SSHListenerAuthorityV1) hostresources.ManagementEndpointV1 {
	endpoint.Owner, endpoint.Source = ProtectionResourceOwner, protectionResourceSource
	endpoint.ResourceID = ProtectionResourceID(authority)
	endpoint.OwnerRevision, endpoint.SemanticRevision = posture.SemanticRevision, posture.SemanticRevision
	endpoint.RuntimeRevision = authority.Revision
	endpoint.ObservedAt, endpoint.ExpiresAt = authority.ObservedAt, authority.ExpiresAt
	return endpoint
}

func authorityCoversManagementEndpoint(authority SSHListenerAuthorityV1, endpoint hostresources.ManagementEndpointV1) bool {
	if endpoint.ServiceKind != hostresources.ManagementSSH || endpoint.Network != hostresources.NetworkTCP ||
		endpoint.Port != authority.Socket.Port || !containsSSHFamily(authority.Socket.CoverageFamilies, hostfacts.Family(endpoint.Family)) {
		return false
	}
	configured := hostresources.NormalizeListen(endpoint.Bind).Value
	return configured == authority.Socket.Bind || endpoint.Wildcard && authority.Socket.Wildcard
}

func containsSSHFamily(values []hostfacts.Family, expected hostfacts.Family) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
