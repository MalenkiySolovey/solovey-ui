package resources

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/netip"
	"sort"
	"strings"
	"time"

	hostsurface "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

const SocketGraphSchemaV1 = "solovey-ui/socket-ownership-graph/v1"

type Strategy string

const (
	StrategyNativeFallback       Strategy = "NATIVE_FALLBACK"
	StrategyDirectGuarded        Strategy = "DIRECT_GUARDED"
	StrategyL4OneToOneFronting   Strategy = "L4_ONE_TO_ONE_FRONTING"
	StrategySNIPreReadFronting   Strategy = "SNI_PREREAD_FRONTING"
	StrategyHTTPTerminatingFront Strategy = "HTTP_TERMINATING_FRONTING"
	StrategyUDPQUICDirectGuarded Strategy = "UDP_QUIC_DIRECT_GUARDED"
	StrategyLocalProxyGuard      Strategy = "LOCAL_PROXY_GUARD"
	StrategyInterceptionGuard    Strategy = "INTERCEPTION_GUARD"
	StrategyUnclassified         Strategy = "UNCLASSIFIED_FAIL_CLOSED"
)

type ClaimKind string

const (
	ClaimDesired  ClaimKind = "desired"
	ClaimObserved ClaimKind = "observed"
)

// SocketClaim identifies one exact TCP/UDP and IPv4/IPv6 socket claim. Route
// selectors intentionally do not appear here because they are not owners.
type SocketClaim struct {
	ID                         string                          `json:"id"`
	Kind                       ClaimKind                       `json:"kind"`
	Key                        hostresources.PublicEndpointKey `json:"key"`
	ResourceID                 string                          `json:"resourceId,omitempty"`
	Owner                      string                          `json:"owner,omitempty"`
	OwnerRevision              string                          `json:"ownerRevision,omitempty"`
	ConfigurationRevision      string                          `json:"configurationRevision,omitempty"`
	SocketInode                string                          `json:"socketInode,omitempty"`
	SocketCookie               uint64                          `json:"socketCookie,omitempty"`
	OwnerObservationRevision   string                          `json:"ownerObservationRevision,omitempty"`
	OwnerContractRevision      string                          `json:"ownerContractRevision,omitempty"`
	InstanceID                 string                          `json:"instanceId,omitempty"`
	SourceRevision             string                          `json:"sourceRevision,omitempty"`
	ArtifactRevision           string                          `json:"artifactRevision,omitempty"`
	DeploymentID               string                          `json:"deploymentId,omitempty"`
	RuntimeRootBindingRevision string                          `json:"runtimeRootBindingRevision,omitempty"`
	ExpectedExecutableSHA256   string                          `json:"expectedExecutableSha256,omitempty"`
	ServiceIdentity            string                          `json:"serviceIdentity,omitempty"`
	Process                    hostsurface.ProcessFact         `json:"process"`
	Service                    hostsurface.ServiceFact         `json:"service"`
	ObservedAt                 int64                           `json:"observedAt,omitempty"`
	ExpiresAt                  int64                           `json:"expiresAt,omitempty"`
	Stale                      bool                            `json:"stale"`
	Truncated                  bool                            `json:"truncated"`
	Ambiguous                  bool                            `json:"ambiguous"`
	ReasonCodes                []string                        `json:"reasonCodes,omitempty"`
}

type AdapterMultiplexingContract struct {
	AdapterID           string   `json:"adapterId"`
	AdapterKind         string   `json:"adapterKind"`
	OwnedClaimIDs       []string `json:"ownedClaimIds"`
	SelectorKinds       []string `json:"selectorKinds"`
	CapabilityRevision  string   `json:"capabilityRevision"`
	ConfigurationDigest string   `json:"configurationDigest"`
}

type LocalTargetStatus struct {
	TargetID              string `json:"targetId"`
	EndpointID            string `json:"endpointId"`
	PublishRevision       string `json:"publishRevision"`
	HealthRevision        string `json:"healthRevision"`
	ConfigurationRevision string `json:"configurationRevision"`
	Health                string `json:"health"`
}

type CollisionAlternative struct {
	Code       string   `json:"code"`
	ResourceID string   `json:"resourceId,omitempty"`
	Strategy   Strategy `json:"strategy,omitempty"`
	Detail     string   `json:"detail"`
}

type SocketGraphNode struct {
	ResourceID            string                             `json:"resourceId"`
	ResourceOwner         string                             `json:"resourceOwner"`
	OwnerRevision         string                             `json:"ownerRevision"`
	ConfigurationRevision string                             `json:"configurationRevision"`
	AdvertisedEndpoints   []hostresources.AdvertisedEndpoint `json:"advertisedEndpoints"`
	DesiredClaims         []SocketClaim                      `json:"desiredClaims"`
	ObservedClaims        []SocketClaim                      `json:"observedClaims"`
	SelectedStrategy      Strategy                           `json:"selectedStrategy"`
	Adapter               *AdapterMultiplexingContract       `json:"adapter,omitempty"`
	LocalTarget           *LocalTargetStatus                 `json:"localTarget,omitempty"`
	Alternatives          []CollisionAlternative             `json:"alternatives"`
	ApplyBlocked          bool                               `json:"applyBlocked"`
	ReasonCodes           []string                           `json:"reasonCodes,omitempty"`
}

type SocketCollision struct {
	Code          string                 `json:"code"`
	LeftClaimID   string                 `json:"leftClaimId"`
	RightClaimID  string                 `json:"rightClaimId"`
	LeftResource  string                 `json:"leftResourceId"`
	RightResource string                 `json:"rightResourceId"`
	Alternatives  []CollisionAlternative `json:"alternatives"`
}

type DualStackObservation struct {
	IPv6ClaimID    string `json:"ipv6ClaimId"`
	CoversIPv4     bool   `json:"coversIpv4"`
	Source         string `json:"source"`
	SourceRevision string `json:"sourceRevision"`
	ObservedAt     int64  `json:"observedAt"`
}

type SocketGraphInput struct {
	Resources                []hostresources.ProtectableResource
	Surfaces                 []hostsurface.HostSurfaceFactV1
	InventoryTruncated       bool
	InventoryReasonCodes     []string
	Strategies               map[string]Strategy
	Adapters                 map[string]AdapterMultiplexingContract
	Targets                  map[string]LocalTargetStatus
	DualStack                []DualStackObservation
	ExpectedRevision         map[string]string
	OwnerObservationRevision string
	Now                      time.Time
}

type SocketOwnershipGraph struct {
	Schema                   string            `json:"schema"`
	Revision                 string            `json:"revision"`
	GeneratedAt              int64             `json:"generatedAt"`
	OwnerObservationRevision string            `json:"ownerObservationRevision"`
	Nodes                    []SocketGraphNode `json:"nodes"`
	Collisions               []SocketCollision `json:"collisions"`
	ApplyBlocked             bool              `json:"applyBlocked"`
	ReasonCodes              []string          `json:"reasonCodes,omitempty"`
}

func BuildSocketOwnershipGraph(input SocketGraphInput) SocketOwnershipGraph {
	now := input.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	resources := append([]hostresources.ProtectableResource(nil), input.Resources...)
	sort.Slice(resources, func(i, j int) bool { return resources[i].ID < resources[j].ID })
	surfaces := append([]hostsurface.HostSurfaceFactV1(nil), input.Surfaces...)
	sort.Slice(surfaces, func(i, j int) bool { return surfaces[i].ID < surfaces[j].ID })

	nodes := make([]SocketGraphNode, 0, len(resources))
	allDesired := make([]SocketClaim, 0)
	for _, resource := range resources {
		node := graphNode(resource, surfaces, input, now)
		nodes = append(nodes, node)
		allDesired = append(allDesired, node.DesiredClaims...)
	}
	collisions := graphCollisions(allDesired, input.Adapters, input.DualStack)
	byResource := make(map[string][]SocketCollision)
	for _, collision := range collisions {
		byResource[collision.LeftResource] = append(byResource[collision.LeftResource], collision)
		byResource[collision.RightResource] = append(byResource[collision.RightResource], collision)
	}
	blocked := input.InventoryTruncated
	reasons := make([]string, 0)
	if input.InventoryTruncated {
		reasons = append(reasons, "hostsurface_inventory_truncated")
		reasons = append(reasons, input.InventoryReasonCodes...)
	}
	for index := range nodes {
		if input.InventoryTruncated {
			nodes[index].ApplyBlocked = true
			nodes[index].ReasonCodes = append(nodes[index].ReasonCodes, "hostsurface_inventory_truncated")
		}
		for _, collision := range byResource[nodes[index].ResourceID] {
			nodes[index].ApplyBlocked = true
			nodes[index].ReasonCodes = append(nodes[index].ReasonCodes, collision.Code)
			nodes[index].Alternatives = append(nodes[index].Alternatives, collision.Alternatives...)
		}
		nodes[index].ReasonCodes = normalizedGraphStrings(nodes[index].ReasonCodes)
		nodes[index].Alternatives = normalizedAlternatives(nodes[index].Alternatives)
		if nodes[index].ApplyBlocked {
			blocked = true
			reasons = append(reasons, "resource_apply_blocked")
		}
	}
	ownerObservationRevision := strings.TrimSpace(input.OwnerObservationRevision)
	if ownerObservationRevision == "" {
		ownerObservationRevision = hostsurface.OwnerObservationSetRevision(surfaces, input.InventoryReasonCodes)
	}
	graph := SocketOwnershipGraph{Schema: SocketGraphSchemaV1, GeneratedAt: now.Unix(), OwnerObservationRevision: ownerObservationRevision, Nodes: nodes, Collisions: collisions, ApplyBlocked: blocked, ReasonCodes: normalizedGraphStrings(reasons)}
	graph.Revision = graphRevision(graph)
	return graph
}

func graphNode(resource hostresources.ProtectableResource, surfaces []hostsurface.HostSurfaceFactV1, input SocketGraphInput, now time.Time) SocketGraphNode {
	strategy := input.Strategies[resource.ID]
	if strategy == "" {
		strategy = recommendedStrategy(resource)
	}
	node := SocketGraphNode{ResourceID: resource.ID, ResourceOwner: resource.Owner, OwnerRevision: resource.Capabilities.OwnerRevision, ConfigurationRevision: resource.Capabilities.ConfigRevision, AdvertisedEndpoints: append([]hostresources.AdvertisedEndpoint(nil), resource.AdvertisedEndpoints...), SelectedStrategy: strategy, Alternatives: []CollisionAlternative{}}
	for _, claim := range resolvedDesiredClaims(resource, surfaces, now) {
		node.DesiredClaims = append(node.DesiredClaims, claim)
		if claim.Ambiguous {
			node.ApplyBlocked = true
			node.ReasonCodes = append(node.ReasonCodes, claim.ReasonCodes...)
		}
	}
	if len(node.DesiredClaims) == 0 {
		endpoint := hostresources.BuildEndpointFact(resource, hostresources.NetworkForProtocol(resource.Protocol), now)
		node.DesiredClaims = append(node.DesiredClaims, desiredClaim(resource, endpoint))
	}
	for _, surface := range surfaces {
		if surface.RegisteredResourceID != resource.ID {
			continue
		}
		for _, claim := range observedClaims(surface, now) {
			node.ObservedClaims = append(node.ObservedClaims, claim)
			if claim.Stale || claim.Truncated || claim.Ambiguous || claim.Owner == "" || claim.Owner == "unknown" {
				node.ApplyBlocked = true
				node.ReasonCodes = append(node.ReasonCodes, claim.ReasonCodes...)
			}
			if resource.Capabilities.ConfigRevision == "" || claim.ConfigurationRevision == "" || resource.Capabilities.ConfigRevision != claim.ConfigurationRevision {
				node.ApplyBlocked = true
				node.ReasonCodes = append(node.ReasonCodes, "stale_configuration_revision")
			}
			if !claimMatchesExpectedOwner(claim, resource.Capabilities.ExpectedListenerOwner) {
				node.ApplyBlocked = true
				node.ReasonCodes = append(node.ReasonCodes, "listener_deployment_mismatch")
			}
		}
	}
	if len(node.ObservedClaims) == 0 {
		node.ApplyBlocked = true
		node.ReasonCodes = append(node.ReasonCodes, "socket_owner_unknown")
	}
	if !resource.Capabilities.ExpectedListenerOwner.Valid() {
		node.ApplyBlocked = true
		node.ReasonCodes = append(node.ReasonCodes, "listener_owner_expectation_missing")
	}
	if !resolvedFamilyCoverageComplete(resource, node.DesiredClaims) {
		node.ApplyBlocked = true
		node.ReasonCodes = append(node.ReasonCodes, "endpoint_address_family_unresolved")
	}
	for _, desired := range node.DesiredClaims {
		matched := false
		for _, observed := range node.ObservedClaims {
			if !sameSocketKey(desired.Key, observed.Key) {
				continue
			}
			matched = true
			if observed.Owner != resource.Owner {
				node.ApplyBlocked = true
				node.ReasonCodes = append(node.ReasonCodes, "socket_owner_mismatch")
			}
		}
		if !matched {
			node.ApplyBlocked = true
			node.ReasonCodes = append(node.ReasonCodes, "desired_socket_unobserved")
		}
		for _, surface := range surfaces {
			// Collisions between two registered resources are handled from the
			// resolved desired-claim set above, including explicit adapters. This
			// pass is only for an otherwise unclaimed observed socket.
			if surface.RegisteredResourceID != "" || surface.Port == 0 {
				continue
			}
			foreign := hostresources.PublicEndpointKey{Network: hostresources.Network(surface.Network), AddressFamily: hostresources.AddressFamily(surface.Family), BindAddress: hostresources.NormalizeListen(surface.Bind).Value, Port: surface.Port}
			if desired.Key.Network == foreign.Network && desired.Key.AddressFamily == foreign.AddressFamily && desired.Key.Port == foreign.Port && (sameSocketKey(desired.Key, foreign) || sameFamilyCoverage(desired.Key, foreign)) {
				node.ApplyBlocked = true
				node.ReasonCodes = append(node.ReasonCodes, "foreign_socket_collision")
			}
		}
	}
	for _, observed := range node.ObservedClaims {
		matched := false
		for _, desired := range node.DesiredClaims {
			matched = matched || sameSocketKey(desired.Key, observed.Key)
		}
		if !matched {
			node.ApplyBlocked = true
			node.ReasonCodes = append(node.ReasonCodes, "observed_socket_drift")
		}
	}
	if duplicateObservedClaims(node.ObservedClaims) {
		node.ApplyBlocked = true
		node.ReasonCodes = append(node.ReasonCodes, "listener_owner_ambiguous")
	}
	if expected := strings.TrimSpace(input.ExpectedRevision[resource.ID]); expected != "" && expected != resource.Capabilities.ConfigRevision {
		node.ApplyBlocked = true
		node.ReasonCodes = append(node.ReasonCodes, "stale_expected_revision")
	}
	if strategy == StrategyUnclassified {
		node.ApplyBlocked = true
		node.ReasonCodes = append(node.ReasonCodes, "strategy_unclassified")
	}
	if adapter, ok := input.Adapters[resource.ID]; ok {
		copy := adapter
		copy.OwnedClaimIDs = normalizedGraphStrings(copy.OwnedClaimIDs)
		copy.SelectorKinds = normalizedGraphStrings(copy.SelectorKinds)
		node.Adapter = &copy
	}
	if target, ok := input.Targets[resource.ID]; ok {
		copy := target
		node.LocalTarget = &copy
		if strings.ToUpper(target.Health) != "HEALTHY" || target.PublishRevision == "" || target.HealthRevision == "" || target.ConfigurationRevision == "" {
			node.ApplyBlocked = true
			node.ReasonCodes = append(node.ReasonCodes, "target_revision_or_health_stale")
		}
	}
	sort.Slice(node.DesiredClaims, func(i, j int) bool { return node.DesiredClaims[i].ID < node.DesiredClaims[j].ID })
	sort.Slice(node.ObservedClaims, func(i, j int) bool { return node.ObservedClaims[i].ID < node.ObservedClaims[j].ID })
	sort.Slice(node.AdvertisedEndpoints, func(i, j int) bool { return node.AdvertisedEndpoints[i].ID < node.AdvertisedEndpoints[j].ID })
	node.ReasonCodes = normalizedGraphStrings(node.ReasonCodes)
	return node
}

func desiredClaim(resource hostresources.ProtectableResource, endpoint hostresources.PublicEndpoint) SocketClaim {
	reasons := append([]string(nil), endpoint.ReasonCodes...)
	claim := SocketClaim{Kind: ClaimDesired, Key: endpoint.Key, ResourceID: resource.ID, Owner: resource.Owner, OwnerRevision: resource.Capabilities.OwnerRevision, ConfigurationRevision: resource.Capabilities.ConfigRevision, ObservedAt: endpoint.ObservedAt, ReasonCodes: reasons}
	if !endpoint.Known() || resource.Owner == "" || resource.Owner == "unknown" || resource.Capabilities.OwnerRevision == "" || resource.Capabilities.ConfigRevision == "" {
		claim.Ambiguous = true
		claim.ReasonCodes = append(claim.ReasonCodes, "desired_claim_incomplete")
	}
	claim.ReasonCodes = normalizedGraphStrings(claim.ReasonCodes)
	claim.ID = socketClaimID(claim.Kind, claim.ResourceID, claim.Key, endpoint.ID)
	return claim
}

func resolvedDesiredClaims(resource hostresources.ProtectableResource, surfaces []hostsurface.HostSurfaceFactV1, now time.Time) []SocketClaim {
	intents, intentsValid := hostresources.ConfiguredListenIntents(resource)
	if !intentsValid {
		return nil
	}
	result := make([]SocketClaim, 0, len(intents)*2)
	seen := map[hostresources.PublicEndpointKey]bool{}
	for _, intent := range intents {
		for _, surface := range surfaces {
			if surface.RegisteredResourceID != resource.ID || hostresources.Network(surface.Network) != intent.Network ||
				surface.Classification != hostsurface.ClassificationManagedExact || surface.ListenerOwner == nil || !surface.ListenerOwner.Valid(now) {
				continue
			}
			owner := surface.ListenerOwner
			for _, family := range owner.Socket.CoverageFamilies {
				key := hostresources.PublicEndpointKey{Network: intent.Network, AddressFamily: hostresources.AddressFamily(family), Port: intent.Port}
				switch intent.Mode {
				case hostresources.ListenIntentExact:
					if len(intent.RequiredFamilies) != 1 || hostresources.AddressFamily(family) != intent.RequiredFamilies[0] || owner.Socket.Bind != intent.Address {
						continue
					}
					key.BindAddress = intent.Address
				case hostresources.ListenIntentWildcard, hostresources.ListenIntentDualStack:
					if family == hostsurface.FamilyIPv4 {
						key.BindAddress = "0.0.0.0"
					} else {
						key.BindAddress = "::"
					}
				default:
					continue
				}
				if seen[key] {
					continue
				}
				seen[key] = true
				endpoint := hostresources.PublicEndpoint{
					Schema: hostresources.EndpointSchemaV1, ID: "resolved:" + owner.ObservationRevision + ":" + string(intent.Network) + ":" + string(family), Key: key,
					Intent: hostresources.EndpointIntentForBind(key.BindAddress), Protocol: string(intent.Network),
					TLS: hostresources.CapabilityUnknown, Reality: hostresources.CapabilityUnknown,
					AuthenticationExpected: hostresources.CapabilityUnknown, FallbackSupported: resource.Capabilities.CanServeFallback,
					ProxyProtocol: resource.Capabilities.AcceptsProxyProtocol, ResourceID: resource.ID, Owner: resource.Owner,
					OwnerRevision: resource.Capabilities.OwnerRevision, ConfigurationRevision: resource.Capabilities.ConfigRevision,
					ObservedAt: surface.LastSeen, Source: surface.Source, ConfidenceBP: 10000,
				}
				claim := desiredClaim(resource, endpoint)
				claim.OwnerObservationRevision = owner.ObservationRevision
				result = append(result, claim)
			}
		}
	}
	if len(result) > 0 {
		return result
	}
	for _, endpoint := range resource.Endpoints {
		result = append(result, desiredClaim(resource, endpoint))
	}
	return result
}

func observedClaims(surface hostsurface.HostSurfaceFactV1, now time.Time) []SocketClaim {
	if surface.Classification == hostsurface.ClassificationManagedExact && surface.ListenerOwner != nil && surface.ListenerOwner.Valid(now) {
		result := make([]SocketClaim, 0, len(surface.ListenerOwner.Socket.CoverageFamilies))
		for _, family := range surface.ListenerOwner.Socket.CoverageFamilies {
			key := hostresources.PublicEndpointKey{Network: hostresources.Network(surface.Network), AddressFamily: hostresources.AddressFamily(family), Port: surface.Port}
			if family == hostsurface.FamilyIPv4 {
				key.BindAddress = "0.0.0.0"
				if surface.Family == hostsurface.FamilyIPv4 {
					key.BindAddress = hostresources.NormalizeListen(surface.Bind).Value
				}
			} else {
				key.BindAddress = "::"
				if surface.Family == hostsurface.FamilyIPv6 {
					key.BindAddress = hostresources.NormalizeListen(surface.Bind).Value
				}
			}
			owner := surface.ListenerOwner
			claim := SocketClaim{
				Kind: ClaimObserved, Key: key, ResourceID: surface.RegisteredResourceID, Owner: surface.DesiredOwner,
				OwnerRevision: owner.Application.ResourceOwnerRevision, ConfigurationRevision: owner.Application.ConfigurationRevision,
				SocketInode: owner.Socket.Inode, SocketCookie: owner.Socket.Cookie, OwnerObservationRevision: owner.ObservationRevision,
				OwnerContractRevision: owner.Application.OwnerContractRevision, InstanceID: owner.Application.InstanceID,
				SourceRevision: owner.Application.SourceRevision, ArtifactRevision: owner.Application.ArtifactRevision,
				DeploymentID: owner.Application.DeploymentID, RuntimeRootBindingRevision: owner.Application.RuntimeRootBindingRevision,
				ExpectedExecutableSHA256: owner.Application.ExpectedExecutableSHA256, ServiceIdentity: owner.Application.ServiceIdentity,
				Process: owner.Process, Service: owner.Service, ObservedAt: owner.ObservedAt, ExpiresAt: owner.ExpiresAt,
				Stale: surface.IsStale(now), Truncated: surface.Truncated, ReasonCodes: append([]string(nil), surface.ReasonCodes...),
			}
			if claim.Stale {
				claim.Ambiguous = true
				claim.ReasonCodes = append(claim.ReasonCodes, "listener_owner_stale")
			}
			claim.ReasonCodes = normalizedGraphStrings(claim.ReasonCodes)
			claim.ID = socketClaimID(claim.Kind, claim.ResourceID, claim.Key, owner.ObservationRevision+":"+string(family))
			result = append(result, claim)
		}
		return result
	}
	key := hostresources.PublicEndpointKey{Network: hostresources.Network(surface.Network), AddressFamily: hostresources.AddressFamily(surface.Family), BindAddress: hostresources.NormalizeListen(surface.Bind).Value, Port: surface.Port}
	owner := strings.TrimSpace(surface.DesiredOwner)
	ambiguous := true
	if owner == "" {
		owner = "unknown"
	}
	claim := SocketClaim{Kind: ClaimObserved, Key: key, ResourceID: surface.RegisteredResourceID, Owner: owner, ConfigurationRevision: surface.ConfigurationRevision, SocketInode: surface.SocketInode, SocketCookie: surface.SocketCookie, Process: surface.Process, Service: surface.Service, ObservedAt: surface.LastSeen, ExpiresAt: surface.ExpiresAt, Stale: surface.IsStale(now), Truncated: surface.Truncated, Ambiguous: ambiguous, ReasonCodes: append([]string(nil), surface.ReasonCodes...)}
	if claim.Stale {
		claim.ReasonCodes = append(claim.ReasonCodes, "socket_fact_stale")
	}
	if claim.Truncated {
		claim.ReasonCodes = append(claim.ReasonCodes, "socket_fact_truncated")
	}
	if claim.Ambiguous {
		claim.ReasonCodes = append(claim.ReasonCodes, "socket_owner_ambiguous")
	}
	switch surface.Classification {
	case hostsurface.ClassificationForeign:
		claim.ReasonCodes = append(claim.ReasonCodes, "listener_owner_foreign")
	case hostsurface.ClassificationStale:
		claim.ReasonCodes = append(claim.ReasonCodes, "listener_owner_stale")
	case hostsurface.ClassificationUnobserved:
		claim.ReasonCodes = append(claim.ReasonCodes, "listener_unobserved")
	default:
		claim.ReasonCodes = append(claim.ReasonCodes, "listener_owner_unknown")
	}
	claim.ReasonCodes = normalizedGraphStrings(claim.ReasonCodes)
	claim.ID = socketClaimID(claim.Kind, claim.ResourceID, claim.Key, surface.ID)
	return []SocketClaim{claim}
}

func claimMatchesExpectedOwner(claim SocketClaim, expected hostresources.ExpectedListenerOwnerV1) bool {
	return expected.Valid() && !claim.Ambiguous && claim.OwnerObservationRevision != "" &&
		claim.OwnerContractRevision == expected.ContractRevision && claim.InstanceID == expected.InstanceID &&
		claim.SourceRevision == expected.SourceRevision && claim.ArtifactRevision == expected.ArtifactRevision &&
		claim.DeploymentID == expected.DeploymentID && claim.RuntimeRootBindingRevision == expected.RuntimeRootBindingRevision &&
		claim.ExpectedExecutableSHA256 == expected.ExecutableSHA256 && claim.ServiceIdentity == expected.ServiceIdentity &&
		claim.Service.SystemdUnit == expected.SystemdUnit && claim.Service.FragmentPath == expected.ServiceFragmentPath &&
		claim.Service.FragmentSHA256 == expected.ServiceUnitSHA256 && claim.Service.ControlGroup == expected.ServiceControlGroup &&
		claim.Process.ControlGroup == expected.ServiceControlGroup && claim.Process.Executable == expected.ExecutablePath &&
		claim.Process.UID != nil && claim.Process.GID != nil && uint32(*claim.Process.UID) == expected.ProcessUID && uint32(*claim.Process.GID) == expected.ProcessGID &&
		claim.Process.ExeDigest == expected.ExecutableSHA256 &&
		claim.OwnerRevision != "" && claim.ConfigurationRevision != ""
}

func resolvedFamilyCoverageComplete(resource hostresources.ProtectableResource, claims []SocketClaim) bool {
	intents, valid := hostresources.ConfiguredListenIntents(resource)
	if !valid {
		return false
	}
	for _, intent := range intents {
		seen := map[hostresources.AddressFamily]bool{}
		for _, claim := range claims {
			if claim.Key.Network == intent.Network && !claim.Ambiguous &&
				(intent.Mode == hostresources.ListenIntentExact || claim.OwnerObservationRevision != "") {
				seen[claim.Key.AddressFamily] = true
			}
		}
		if intent.Mode == hostresources.ListenIntentWildcard && len(intent.RequiredFamilies) == 0 {
			if len(seen) == 0 {
				return false
			}
			continue
		}
		if len(intent.RequiredFamilies) == 0 {
			return false
		}
		for _, family := range intent.RequiredFamilies {
			if !seen[family] {
				return false
			}
		}
	}
	return len(intents) > 0
}

func duplicateObservedClaims(claims []SocketClaim) bool {
	seen := map[hostresources.PublicEndpointKey]bool{}
	for _, claim := range claims {
		if seen[claim.Key] {
			return true
		}
		seen[claim.Key] = true
	}
	return false
}

func sameSocketKey(left, right hostresources.PublicEndpointKey) bool {
	return left.Network == right.Network && left.AddressFamily == right.AddressFamily && left.Port == right.Port && hostresources.NormalizeListen(left.BindAddress).Value == hostresources.NormalizeListen(right.BindAddress).Value
}

func graphCollisions(claims []SocketClaim, adapters map[string]AdapterMultiplexingContract, dual []DualStackObservation) []SocketCollision {
	claims = append([]SocketClaim(nil), claims...)
	sort.Slice(claims, func(i, j int) bool { return claims[i].ID < claims[j].ID })
	dualObserved := make(map[string]bool)
	for _, observation := range dual {
		if observation.CoversIPv4 && observation.Source != "" && observation.SourceRevision != "" && observation.ObservedAt > 0 {
			dualObserved[observation.IPv6ClaimID] = true
		}
	}
	result := make([]SocketCollision, 0)
	for leftIndex := 0; leftIndex < len(claims); leftIndex++ {
		for rightIndex := leftIndex + 1; rightIndex < len(claims); rightIndex++ {
			left, right := claims[leftIndex], claims[rightIndex]
			if left.ResourceID == right.ResourceID || left.Key.Network != right.Key.Network || left.Key.Port != right.Key.Port {
				continue
			}
			code := claimCollisionCode(left, right, dualObserved)
			if code == "" || explicitSharedAdapter(left, right, adapters) {
				continue
			}
			result = append(result, SocketCollision{Code: code, LeftClaimID: left.ID, RightClaimID: right.ID, LeftResource: left.ResourceID, RightResource: right.ResourceID, Alternatives: collisionAlternatives(left.ResourceID, right.ResourceID)})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].LeftClaimID+"\x00"+result[i].RightClaimID+"\x00"+result[i].Code < result[j].LeftClaimID+"\x00"+result[j].RightClaimID+"\x00"+result[j].Code
	})
	return result
}

func claimCollisionCode(left, right SocketClaim, dualObserved map[string]bool) string {
	if left.Key.AddressFamily == right.Key.AddressFamily {
		if left.Key.BindAddress == right.Key.BindAddress {
			return "exact_socket_collision"
		}
		if sameFamilyCoverage(left.Key, right.Key) {
			return "wildcard_socket_collision"
		}
		return ""
	}
	var ipv6, ipv4 SocketClaim
	if left.Key.AddressFamily == hostresources.AddressFamilyIPv6 && right.Key.AddressFamily == hostresources.AddressFamilyIPv4 {
		ipv6, ipv4 = left, right
	} else if right.Key.AddressFamily == hostresources.AddressFamilyIPv6 && left.Key.AddressFamily == hostresources.AddressFamilyIPv4 {
		ipv6, ipv4 = right, left
	} else {
		return ""
	}
	if isWildcard(ipv6.Key) && dualObserved[ipv6.ID] && (isWildcard(ipv4.Key) || validExact(ipv4.Key.BindAddress)) {
		return "observed_dual_stack_collision"
	}
	return ""
}

func sameFamilyCoverage(left, right hostresources.PublicEndpointKey) bool {
	return isWildcard(left) || isWildcard(right)
}

func isWildcard(key hostresources.PublicEndpointKey) bool {
	normalized := hostresources.NormalizeListen(key.BindAddress)
	return normalized.Wildcard()
}

func validExact(value string) bool {
	_, err := netip.ParseAddr(strings.TrimSpace(value))
	return err == nil
}

func explicitSharedAdapter(left, right SocketClaim, adapters map[string]AdapterMultiplexingContract) bool {
	for _, adapter := range adapters {
		if adapter.AdapterID == "" || adapter.CapabilityRevision == "" || adapter.ConfigurationDigest == "" || len(adapter.SelectorKinds) == 0 {
			continue
		}
		owned := make(map[string]struct{}, len(adapter.OwnedClaimIDs))
		for _, claimID := range adapter.OwnedClaimIDs {
			owned[claimID] = struct{}{}
		}
		_, ownsLeft := owned[left.ID]
		_, ownsRight := owned[right.ID]
		if ownsLeft && ownsRight {
			return true
		}
	}
	return false
}

func collisionAlternatives(left, right string) []CollisionAlternative {
	return []CollisionAlternative{
		{Code: "keep_direct", Strategy: StrategyDirectGuarded, Detail: "keep the current direct owner and leave the other claim unapplied"},
		{Code: "move_adapter", Detail: "move an explicitly selected compatible adapter; do not change an inbound port silently"},
		{Code: "move_inbound", ResourceID: right, Detail: "operator may explicitly move the inbound after reviewing client impact"},
		{Code: "remain_unapplied", Detail: "preserve current ownership and remain observe-only"},
		{Code: "select_compatible_strategy", ResourceID: left, Detail: "choose a strategy whose owner matches the observed socket graph"},
	}
}

func recommendedStrategy(resource hostresources.ProtectableResource) Strategy {
	protocol := strings.ToLower(strings.TrimSpace(resource.Protocol))
	kind := strings.ToLower(strings.TrimSpace(resource.Kind))
	if kind == "tun" || kind == "redirect" || kind == "tproxy" {
		return StrategyInterceptionGuard
	}
	if protocol == "udp" || protocol == "quic" || strings.Contains(protocol, "hysteria") || strings.Contains(protocol, "tuic") {
		return StrategyUDPQUICDirectGuarded
	}
	if kind == "socks" || kind == "http" || kind == "mixed" {
		return StrategyLocalProxyGuard
	}
	if !resource.Capabilities.Known || hostresources.NetworkForProtocol(resource.Protocol) == hostresources.NetworkUnknown {
		return StrategyUnclassified
	}
	return StrategyDirectGuarded
}

func socketClaimID(kind ClaimKind, resourceID string, key hostresources.PublicEndpointKey, sourceID string) string {
	payload, _ := json.Marshal(struct {
		Kind       ClaimKind                       `json:"kind"`
		ResourceID string                          `json:"resource_id"`
		Key        hostresources.PublicEndpointKey `json:"key"`
		SourceID   string                          `json:"source_id"`
	}{kind, resourceID, key, sourceID})
	sum := sha256.Sum256(payload)
	return "claim:" + hex.EncodeToString(sum[:16])
}

func graphRevision(graph SocketOwnershipGraph) string {
	copy := graph
	copy.Revision = ""
	copy.GeneratedAt = 0
	copy.Nodes = append([]SocketGraphNode(nil), graph.Nodes...)
	for index := range copy.Nodes {
		copy.Nodes[index].DesiredClaims = append([]SocketClaim(nil), graph.Nodes[index].DesiredClaims...)
		for claimIndex := range copy.Nodes[index].DesiredClaims {
			copy.Nodes[index].DesiredClaims[claimIndex].ObservedAt = 0
			copy.Nodes[index].DesiredClaims[claimIndex].ExpiresAt = 0
		}
		copy.Nodes[index].ObservedClaims = append([]SocketClaim(nil), graph.Nodes[index].ObservedClaims...)
		for claimIndex := range copy.Nodes[index].ObservedClaims {
			copy.Nodes[index].ObservedClaims[claimIndex].ObservedAt = 0
			copy.Nodes[index].ObservedClaims[claimIndex].ExpiresAt = 0
		}
	}
	payload, _ := json.Marshal(copy)
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func normalizedGraphStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func normalizedAlternatives(values []CollisionAlternative) []CollisionAlternative {
	seen := make(map[string]struct{}, len(values))
	result := make([]CollisionAlternative, 0, len(values))
	for _, value := range values {
		key := value.Code + "\x00" + value.ResourceID + "\x00" + string(value.Strategy) + "\x00" + value.Detail
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Code+"\x00"+result[i].ResourceID < result[j].Code+"\x00"+result[j].ResourceID
	})
	return result
}
