package resources

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	hostsurface "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

const (
	SocketOwnershipGraphEvidenceSchemaV1 = "solovey-ui/socket-ownership-graph-evidence/v1"
	MaxSocketGraphEvidenceNodes          = hostresources.MaxResourceFacts
	MaxSocketGraphEvidenceReasons        = 32
	MaxSocketGraphEvidenceObservations   = 4096
	MaxSocketGraphEvidenceClaimsPerNode  = 8192
)

// SocketOwnershipGraphEvidenceV1 is a bounded, read-only explanation of the
// exact resource, host-surface and graph inputs used by one firewall baseline plan. It
// is deliberately separate from graph construction so diagnostics cannot
// promote or weaken an eligibility classification.
type SocketOwnershipGraphEvidenceV1 struct {
	Schema                   string                      `json:"schema"`
	Revision                 string                      `json:"revision"`
	GraphRevision            string                      `json:"graphRevision"`
	OwnerObservationRevision string                      `json:"ownerObservationRevision"`
	Nodes                    []SocketGraphNodeEvidenceV1 `json:"nodes"`
}

type SocketGraphNodeEvidenceV1 struct {
	ResourceID             string                                 `json:"resourceId"`
	ResourceRevision       string                                 `json:"resourceRevision"`
	OwnerRevision          string                                 `json:"ownerRevision"`
	ConfigurationRevision  string                                 `json:"configurationRevision"`
	ConfiguredListenIntent hostresources.ConfiguredListenIntentV1 `json:"configuredListenIntent"`
	Endpoints              []SocketEndpointEvidenceV1             `json:"endpoints"`
	DesiredClaims          []SocketClaimEvidenceV1                `json:"desiredClaims"`
	ObservedClaims         []SocketClaimEvidenceV1                `json:"observedClaims"`
	OwnerObservations      []SocketOwnerObservationEvidenceV1     `json:"ownerObservations"`
	ApplyBlocked           bool                                   `json:"applyBlocked"`
	ReasonCodes            []string                               `json:"reasonCodes"`
}

type SocketEndpointEvidenceV1 struct {
	EndpointID string                          `json:"endpointId"`
	Key        hostresources.PublicEndpointKey `json:"key"`
}

type SocketClaimEvidenceV1 struct {
	ClaimID                  string                          `json:"claimId"`
	Kind                     ClaimKind                       `json:"kind"`
	SourceID                 string                          `json:"sourceId"`
	Key                      hostresources.PublicEndpointKey `json:"key"`
	SocketInode              string                          `json:"socketInode,omitempty"`
	SocketCookie             uint64                          `json:"socketCookie,omitempty"`
	SocketProofMethod        string                          `json:"socketProofMethod,omitempty"`
	OwnerObservationRevision string                          `json:"ownerObservationRevision,omitempty"`
	SemanticOwnerSource      string                          `json:"semanticOwnerSource,omitempty"`
	SemanticOwnerRevision    string                          `json:"semanticOwnerRevision,omitempty"`
	Stale                    bool                            `json:"stale"`
	Ambiguous                bool                            `json:"ambiguous"`
	ReasonCodes              []string                        `json:"reasonCodes"`
}

type SocketOwnerObservationEvidenceV1 struct {
	EvidenceSource            string                                     `json:"evidenceSource"`
	SurfaceID                 string                                     `json:"surfaceId,omitempty"`
	RegisteredResourceID      string                                     `json:"registeredResourceId,omitempty"`
	Classification            hostsurface.Classification                 `json:"classification"`
	HostSurfaceClassification hostsurface.Classification                 `json:"hostSurfaceClassification"`
	ListenerOwnerCurrent      bool                                       `json:"listenerOwnerCurrent"`
	SemanticOwnerCurrent      bool                                       `json:"semanticOwnerCurrent"`
	OwnerObservationRevision  string                                     `json:"ownerObservationRevision,omitempty"`
	DeploymentBindingRevision string                                     `json:"deploymentBindingRevision,omitempty"`
	SemanticOwner             *hostsurface.SemanticOwnerFactV1           `json:"semanticOwner,omitempty"`
	Socket                    SocketIdentityEvidenceV1                   `json:"socket"`
	Process                   *ProcessIdentityEvidenceV1                 `json:"process,omitempty"`
	Service                   *ServiceIdentityEvidenceV1                 `json:"service,omitempty"`
	Application               *hostsurface.ListenerApplicationIdentityV1 `json:"application,omitempty"`
	ReasonCodes               []string                                   `json:"reasonCodes"`
}

type SocketIdentityEvidenceV1 struct {
	ProofMethod      string               `json:"proofMethod,omitempty"`
	Network          hostsurface.Network  `json:"network"`
	Family           hostsurface.Family   `json:"family"`
	Bind             string               `json:"bind"`
	Port             uint16               `json:"port"`
	Inode            string               `json:"inode,omitempty"`
	Cookie           uint64               `json:"cookie,omitempty"`
	IPv6Only         *bool                `json:"ipv6Only,omitempty"`
	CoverageFamilies []hostsurface.Family `json:"coverageFamilies"`
}

type ProcessIdentityEvidenceV1 struct {
	PID                  *int   `json:"pid,omitempty"`
	UID                  *int   `json:"uid,omitempty"`
	GID                  *int   `json:"gid,omitempty"`
	StartTime            string `json:"startTime"`
	ExecutableSHA256     string `json:"executableSha256"`
	ExecutablePathSHA256 string `json:"executablePathSha256"`
	ExecutableDevice     uint64 `json:"executableDevice"`
	ExecutableInode      uint64 `json:"executableInode"`
	ControlGroupSHA256   string `json:"controlGroupSha256"`
}

type ServiceIdentityEvidenceV1 struct {
	ProofKind          string   `json:"proofKind"`
	MainPID            *int     `json:"mainPid,omitempty"`
	ActiveState        string   `json:"activeState"`
	SubState           string   `json:"subState"`
	SystemdUnit        string   `json:"systemdUnit,omitempty"`
	FragmentSHA256     string   `json:"fragmentSha256,omitempty"`
	FragmentPathSHA256 string   `json:"fragmentPathSha256,omitempty"`
	ControlGroupSHA256 string   `json:"controlGroupSha256,omitempty"`
	StartMonotonicUsec uint64   `json:"startMonotonicUsec,omitempty"`
	ProcdService       string   `json:"procdService,omitempty"`
	ProcdInstance      string   `json:"procdInstance,omitempty"`
	ProcdCommand       []string `json:"procdCommand,omitempty"`
	ProcdUser          string   `json:"procdUser,omitempty"`
	ProcdGroup         string   `json:"procdGroup,omitempty"`
}

func BuildSocketOwnershipGraphEvidence(graph SocketOwnershipGraph, resources []hostresources.ProtectableResource, surfaces []hostsurface.HostSurfaceFactV1, now time.Time) (SocketOwnershipGraphEvidenceV1, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	if graph.Schema != SocketGraphSchemaV1 || !graphEvidenceRevisionToken(graph.Revision) || !graphEvidenceRevisionToken(graph.OwnerObservationRevision) {
		return SocketOwnershipGraphEvidenceV1{}, errors.New("socket graph revision contract is unavailable")
	}
	if len(graph.Nodes) > MaxSocketGraphEvidenceNodes || len(resources) > MaxSocketGraphEvidenceNodes || len(surfaces) > MaxSocketGraphEvidenceObservations {
		return SocketOwnershipGraphEvidenceV1{}, errors.New("socket graph evidence cardinality exceeds its contract")
	}
	resourceByID := make(map[string]hostresources.ProtectableResource, len(resources))
	for _, resource := range resources {
		if _, exists := resourceByID[resource.ID]; exists {
			return SocketOwnershipGraphEvidenceV1{}, fmt.Errorf("duplicate graph evidence resource %q", resource.ID)
		}
		resourceByID[resource.ID] = resource
	}
	if len(resourceByID) != len(graph.Nodes) {
		return SocketOwnershipGraphEvidenceV1{}, errors.New("socket graph evidence resource set differs from its graph")
	}
	surfaces = append([]hostsurface.HostSurfaceFactV1(nil), surfaces...)
	sort.Slice(surfaces, func(i, j int) bool { return surfaces[i].ID < surfaces[j].ID })
	evidence := SocketOwnershipGraphEvidenceV1{
		Schema: SocketOwnershipGraphEvidenceSchemaV1, GraphRevision: graph.Revision,
		OwnerObservationRevision: graph.OwnerObservationRevision, Nodes: make([]SocketGraphNodeEvidenceV1, 0, len(graph.Nodes)),
	}
	for _, node := range graph.Nodes {
		resource, exists := resourceByID[node.ResourceID]
		if !exists {
			return SocketOwnershipGraphEvidenceV1{}, fmt.Errorf("graph evidence resource %q is absent", node.ResourceID)
		}
		nodeEvidence, err := buildSocketGraphNodeEvidence(node, resource, graphEvidenceNodeSurfaces(node, surfaces), now)
		if err != nil {
			return SocketOwnershipGraphEvidenceV1{}, fmt.Errorf("graph evidence node %q: %w", node.ResourceID, err)
		}
		evidence.Nodes = append(evidence.Nodes, nodeEvidence)
	}
	evidence.Revision = socketGraphEvidenceRevision(evidence)
	if err := ValidateSocketOwnershipGraphEvidence(evidence, graph); err != nil {
		return SocketOwnershipGraphEvidenceV1{}, err
	}
	return evidence, nil
}

func graphEvidenceNodeSurfaces(node SocketGraphNode, surfaces []hostsurface.HostSurfaceFactV1) []hostsurface.HostSurfaceFactV1 {
	result := make([]hostsurface.HostSurfaceFactV1, 0)
	for _, surface := range surfaces {
		if surface.RegisteredResourceID == node.ResourceID {
			result = append(result, surface)
			continue
		}
		if surface.RegisteredResourceID != "" || surface.Port == 0 {
			continue
		}
		foreign := hostresources.PublicEndpointKey{Network: hostresources.Network(surface.Network), AddressFamily: hostresources.AddressFamily(surface.Family), BindAddress: hostresources.NormalizeListen(surface.Bind).Value, Port: surface.Port}
		for _, desired := range node.DesiredClaims {
			if desired.Key.Network == foreign.Network && desired.Key.AddressFamily == foreign.AddressFamily && desired.Key.Port == foreign.Port && (sameSocketKey(desired.Key, foreign) || sameFamilyCoverage(desired.Key, foreign)) {
				result = append(result, surface)
				break
			}
		}
	}
	return result
}

func buildSocketGraphNodeEvidence(node SocketGraphNode, resource hostresources.ProtectableResource, surfaces []hostsurface.HostSurfaceFactV1, now time.Time) (SocketGraphNodeEvidenceV1, error) {
	reasons, err := boundedGraphEvidenceReasons(node.ReasonCodes)
	if err != nil {
		return SocketGraphNodeEvidenceV1{}, err
	}
	if len(node.DesiredClaims) > MaxSocketGraphEvidenceClaimsPerNode || len(node.ObservedClaims) > MaxSocketGraphEvidenceClaimsPerNode || len(surfaces) > MaxSocketGraphEvidenceObservations {
		return SocketGraphNodeEvidenceV1{}, errors.New("claim or observation count exceeds its contract")
	}
	intent := resource.ListenIntent
	if intent.Schema == "" {
		intent = hostresources.BuildConfiguredListenIntent(resource)
	}
	intent.RequiredFamilies = append([]hostresources.AddressFamily(nil), intent.RequiredFamilies...)
	sort.Slice(intent.RequiredFamilies, func(i, j int) bool { return intent.RequiredFamilies[i] < intent.RequiredFamilies[j] })
	resourceRevision := strings.TrimSpace(resource.Fingerprint)
	if resourceRevision == "" {
		resourceRevision = hostresources.Fingerprint(resource)
	}
	result := SocketGraphNodeEvidenceV1{
		ResourceID: node.ResourceID, ResourceRevision: resourceRevision, OwnerRevision: node.OwnerRevision,
		ConfigurationRevision: node.ConfigurationRevision, ConfiguredListenIntent: intent,
		Endpoints: []SocketEndpointEvidenceV1{}, DesiredClaims: []SocketClaimEvidenceV1{}, ObservedClaims: []SocketClaimEvidenceV1{},
		OwnerObservations: []SocketOwnerObservationEvidenceV1{}, ApplyBlocked: node.ApplyBlocked, ReasonCodes: reasons,
	}
	endpoints := append([]hostresources.PublicEndpoint(nil), resource.Endpoints...)
	if len(endpoints) == 0 {
		endpoints = append(endpoints, hostresources.BuildEndpointFact(resource, hostresources.NetworkForProtocol(resource.Protocol), now))
	}
	if len(endpoints) > hostresources.MaxEndpointsPerResource {
		return SocketGraphNodeEvidenceV1{}, errors.New("endpoint count exceeds its contract")
	}
	for _, endpoint := range endpoints {
		result.Endpoints = append(result.Endpoints, SocketEndpointEvidenceV1{EndpointID: endpoint.ID, Key: endpoint.Key})
	}
	sort.Slice(result.Endpoints, func(i, j int) bool { return result.Endpoints[i].EndpointID < result.Endpoints[j].EndpointID })
	desiredSources := desiredClaimSources(resource, endpoints, node.DesiredClaims)
	observedSources := observedClaimSources(surfaces, now)
	for _, claim := range node.DesiredClaims {
		evidenceClaim, err := buildClaimEvidence(claim, desiredSources[claim.ID])
		if err != nil || evidenceClaim.SourceID == "" {
			return SocketGraphNodeEvidenceV1{}, fmt.Errorf("desired claim %q has no bounded source identity", claim.ID)
		}
		result.DesiredClaims = append(result.DesiredClaims, evidenceClaim)
	}
	for _, claim := range node.ObservedClaims {
		evidenceClaim, err := buildClaimEvidence(claim, observedSources[claim.ID])
		if err != nil || evidenceClaim.SourceID == "" {
			return SocketGraphNodeEvidenceV1{}, fmt.Errorf("observed claim %q has no bounded source identity", claim.ID)
		}
		result.ObservedClaims = append(result.ObservedClaims, evidenceClaim)
	}
	sort.Slice(result.DesiredClaims, func(i, j int) bool { return result.DesiredClaims[i].ClaimID < result.DesiredClaims[j].ClaimID })
	sort.Slice(result.ObservedClaims, func(i, j int) bool { return result.ObservedClaims[i].ClaimID < result.ObservedClaims[j].ClaimID })
	for _, surface := range surfaces {
		observation, err := buildOwnerObservationEvidence(surface, now)
		if err != nil {
			return SocketGraphNodeEvidenceV1{}, err
		}
		result.OwnerObservations = append(result.OwnerObservations, observation)
	}
	if len(result.OwnerObservations) == 0 {
		result.OwnerObservations = append(result.OwnerObservations, SocketOwnerObservationEvidenceV1{
			EvidenceSource: "no_host_surface", Classification: hostsurface.ClassificationUnobserved,
			Socket:      SocketIdentityEvidenceV1{Network: hostsurface.Network(intent.Network), Port: intent.Port, CoverageFamilies: []hostsurface.Family{}},
			ReasonCodes: []string{"socket_owner_unknown"},
		})
	}
	sort.Slice(result.OwnerObservations, func(i, j int) bool {
		left, right := result.OwnerObservations[i], result.OwnerObservations[j]
		return left.SurfaceID+"\x00"+string(left.Classification)+"\x00"+left.OwnerObservationRevision < right.SurfaceID+"\x00"+string(right.Classification)+"\x00"+right.OwnerObservationRevision
	})
	return result, nil
}

func desiredClaimSources(resource hostresources.ProtectableResource, endpoints []hostresources.PublicEndpoint, claims []SocketClaim) map[string]string {
	result := make(map[string]string, len(claims))
	for _, endpoint := range endpoints {
		result[socketClaimID(ClaimDesired, resource.ID, endpoint.Key, endpoint.ID)] = endpoint.ID
	}
	for _, claim := range claims {
		if claim.OwnerObservationRevision == "" {
			continue
		}
		sourceID := "resolved:" + claim.OwnerObservationRevision + ":" + string(claim.Key.Network) + ":" + string(claim.Key.AddressFamily)
		if socketClaimID(ClaimDesired, resource.ID, claim.Key, sourceID) == claim.ID {
			result[claim.ID] = sourceID
		}
	}
	return result
}

func observedClaimSources(surfaces []hostsurface.HostSurfaceFactV1, now time.Time) map[string]string {
	result := make(map[string]string)
	for _, surface := range surfaces {
		if exactSSHSemanticSurface(surface, now) {
			for _, family := range surface.SemanticOwner.Socket.CoverageFamilies {
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
				claimID := socketClaimID(ClaimObserved, surface.RegisteredResourceID, key, surface.SemanticOwner.Revision+":"+surface.SemanticOwner.AuthorityRevision+":"+surface.ID+":"+string(family))
				result[claimID] = surface.ID
			}
			continue
		}
		if surface.Classification == hostsurface.ClassificationManagedExact && surface.ListenerOwner != nil && surface.ListenerOwner.Valid(now) {
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
				claimID := socketClaimID(ClaimObserved, surface.RegisteredResourceID, key, surface.ListenerOwner.ObservationRevision+":"+string(family))
				result[claimID] = surface.ID
			}
			continue
		}
		key := hostresources.PublicEndpointKey{Network: hostresources.Network(surface.Network), AddressFamily: hostresources.AddressFamily(surface.Family), BindAddress: hostresources.NormalizeListen(surface.Bind).Value, Port: surface.Port}
		result[socketClaimID(ClaimObserved, surface.RegisteredResourceID, key, surface.ID)] = surface.ID
	}
	return result
}

func buildClaimEvidence(claim SocketClaim, sourceID string) (SocketClaimEvidenceV1, error) {
	reasons, err := boundedGraphEvidenceReasons(claim.ReasonCodes)
	if err != nil {
		return SocketClaimEvidenceV1{}, err
	}
	return SocketClaimEvidenceV1{
		ClaimID: claim.ID, Kind: claim.Kind, SourceID: sourceID, Key: claim.Key,
		SocketInode: claim.SocketInode, SocketCookie: claim.SocketCookie, SocketProofMethod: claim.SocketProofMethod, OwnerObservationRevision: claim.OwnerObservationRevision,
		SemanticOwnerSource: claim.SemanticOwnerSource, SemanticOwnerRevision: claim.SemanticOwnerRevision,
		Stale: claim.Stale, Ambiguous: claim.Ambiguous, ReasonCodes: reasons,
	}, nil
}

func buildOwnerObservationEvidence(surface hostsurface.HostSurfaceFactV1, now time.Time) (SocketOwnerObservationEvidenceV1, error) {
	classification, valid := socketGraphEvidenceClassification(surface.Classification)
	if !valid {
		return SocketOwnerObservationEvidenceV1{}, fmt.Errorf("unsupported HostSurface classification %q", surface.Classification)
	}
	reasons, err := boundedGraphEvidenceReasons(surface.ReasonCodes)
	if err != nil {
		return SocketOwnerObservationEvidenceV1{}, err
	}
	result := SocketOwnerObservationEvidenceV1{
		EvidenceSource: "host_surface", SurfaceID: surface.ID, RegisteredResourceID: surface.RegisteredResourceID, Classification: classification, HostSurfaceClassification: surface.Classification,
		Socket:      SocketIdentityEvidenceV1{Network: surface.Network, Family: surface.Family, Bind: surface.Bind, Port: surface.Port, Inode: surface.SocketInode, Cookie: surface.SocketCookie, CoverageFamilies: []hostsurface.Family{}},
		ReasonCodes: reasons,
	}
	if exactSSHSemanticSurface(surface, now) {
		semanticOwner := *surface.SemanticOwner
		semanticOwner.Socket.CoverageFamilies = append([]hostsurface.Family(nil), surface.SemanticOwner.Socket.CoverageFamilies...)
		if surface.SemanticOwner.Socket.IPv6Only != nil {
			ipv6Only := *surface.SemanticOwner.Socket.IPv6Only
			semanticOwner.Socket.IPv6Only = &ipv6Only
		}
		result.SemanticOwnerCurrent = true
		result.SemanticOwner = &semanticOwner
		result.OwnerObservationRevision = semanticOwner.Revision
		result.Socket = SocketIdentityEvidenceV1{
			ProofMethod: surface.SemanticOwner.Socket.ProofMethod, Network: surface.SemanticOwner.Socket.Network, Family: surface.SemanticOwner.Socket.Family,
			Bind: surface.SemanticOwner.Socket.Bind, Port: surface.SemanticOwner.Socket.Port,
			Inode: surface.SemanticOwner.Socket.Inode, Cookie: surface.SemanticOwner.Socket.Cookie,
			IPv6Only:         surface.SemanticOwner.Socket.IPv6Only,
			CoverageFamilies: append([]hostsurface.Family(nil), surface.SemanticOwner.Socket.CoverageFamilies...),
		}
		result.Process = freezeProcessIdentityEvidence(surface.Process)
		service := freezeServiceIdentityEvidence(surface.Service)
		if !semanticSSHServiceIdentityEvidenceValid(service) {
			return SocketOwnerObservationEvidenceV1{}, errors.New("validated SSH semantic owner service proof cannot be frozen completely")
		}
		result.Service = &service
		return result, nil
	}
	owner := surface.ListenerOwner
	if owner == nil {
		return result, nil
	}
	result.ListenerOwnerCurrent = owner.Valid(now)
	if graphEvidenceRevisionToken(owner.ObservationRevision) {
		result.OwnerObservationRevision = owner.ObservationRevision
	}
	complete := result.ListenerOwnerCurrent
	if !complete && owner.ObservedAt > 0 {
		complete = owner.Valid(time.Unix(owner.ObservedAt, 0).UTC())
	}
	if !complete {
		return result, nil
	}
	result.Socket = SocketIdentityEvidenceV1{
		ProofMethod: owner.Socket.ProofMethod, Network: owner.Socket.Network, Family: owner.Socket.Family, Bind: owner.Socket.Bind, Port: owner.Socket.Port,
		Inode: owner.Socket.Inode, Cookie: owner.Socket.Cookie, IPv6Only: owner.Socket.IPv6Only,
		CoverageFamilies: append([]hostsurface.Family(nil), owner.Socket.CoverageFamilies...),
	}
	sort.Slice(result.Socket.CoverageFamilies, func(i, j int) bool { return result.Socket.CoverageFamilies[i] < result.Socket.CoverageFamilies[j] })
	result.Process = freezeProcessIdentityEvidence(owner.Process)
	serviceEvidence := freezeServiceIdentityEvidence(owner.Service)
	if !serviceIdentityEvidenceValid(serviceEvidence) {
		return SocketOwnerObservationEvidenceV1{}, errors.New("validated listener owner service proof cannot be frozen completely")
	}
	result.Service = &serviceEvidence
	application := owner.Application
	result.Application = &application
	result.DeploymentBindingRevision = hostresources.Revision(application)
	return result, nil
}

func freezeProcessIdentityEvidence(process hostsurface.ProcessFact) *ProcessIdentityEvidenceV1 {
	return &ProcessIdentityEvidenceV1{
		PID: process.PID, UID: process.UID, GID: process.GID,
		StartTime: process.StartTime, ExecutableSHA256: process.ExeDigest,
		ExecutablePathSHA256: graphEvidenceStringDigest(process.Executable), ExecutableDevice: process.ExeDevice,
		ExecutableInode: process.ExeInode, ControlGroupSHA256: graphEvidenceStringDigest(process.ControlGroup),
	}
}

func freezeServiceIdentityEvidence(service hostsurface.ServiceFact) ServiceIdentityEvidenceV1 {
	result := ServiceIdentityEvidenceV1{MainPID: service.MainPID, ActiveState: service.ActiveState, SubState: service.SubState}
	if service.ProcdService != "" {
		result.ProofKind = "procd"
		result.ProcdService = service.ProcdService
		result.ProcdInstance = service.ProcdInstance
		result.ProcdCommand = append([]string(nil), service.ProcdCommand...)
		result.ProcdUser = service.ProcdUser
		result.ProcdGroup = service.ProcdGroup
		return result
	}
	result.ProofKind = "systemd"
	result.SystemdUnit = service.SystemdUnit
	result.FragmentSHA256 = service.FragmentSHA256
	result.FragmentPathSHA256 = graphEvidenceStringDigest(service.FragmentPath)
	result.ControlGroupSHA256 = graphEvidenceStringDigest(service.ControlGroup)
	result.StartMonotonicUsec = service.StartMonotonicUsec
	return result
}

func ValidateSocketOwnershipGraphEvidence(evidence SocketOwnershipGraphEvidenceV1, graph SocketOwnershipGraph) error {
	if evidence.Schema != SocketOwnershipGraphEvidenceSchemaV1 || evidence.GraphRevision != graph.Revision || evidence.OwnerObservationRevision != graph.OwnerObservationRevision || len(evidence.Nodes) != len(graph.Nodes) || len(evidence.Nodes) > MaxSocketGraphEvidenceNodes {
		return errors.New("socket graph evidence does not match the frozen graph")
	}
	if !graphEvidenceRevisionToken(evidence.Revision) || evidence.Revision != socketGraphEvidenceRevision(evidence) {
		return errors.New("socket graph evidence revision is invalid")
	}
	previous := ""
	for index, node := range evidence.Nodes {
		graphNode := graph.Nodes[index]
		if node.ResourceID == "" || node.ResourceID <= previous || node.ResourceID != graphNode.ResourceID || node.ApplyBlocked != graphNode.ApplyBlocked || !equalGraphEvidenceStrings(node.ReasonCodes, graphNode.ReasonCodes) {
			return errors.New("socket graph evidence node identity or reasons do not match the frozen graph")
		}
		previous = node.ResourceID
		if len(node.ReasonCodes) > MaxSocketGraphEvidenceReasons || len(node.Endpoints) == 0 || len(node.Endpoints) > hostresources.MaxEndpointsPerResource || len(node.DesiredClaims) > MaxSocketGraphEvidenceClaimsPerNode || len(node.ObservedClaims) > MaxSocketGraphEvidenceClaimsPerNode || len(node.OwnerObservations) == 0 || len(node.OwnerObservations) > MaxSocketGraphEvidenceObservations {
			return errors.New("socket graph evidence node is missing details or exceeds its bounds")
		}
		if !graphEvidenceClaimsMatch(node.DesiredClaims, graphNode.DesiredClaims) || !graphEvidenceClaimsMatch(node.ObservedClaims, graphNode.ObservedClaims) {
			return errors.New("socket graph evidence claims do not match the frozen graph")
		}
		if err := validateGraphEvidenceClaimSources(node); err != nil {
			return fmt.Errorf("socket graph evidence node %q claim sources do not match its endpoints or HostSurface observations: %w", node.ResourceID, err)
		}
		previousObservation := ""
		for _, observation := range node.OwnerObservations {
			expectedClassification, rawValid := socketGraphEvidenceClassification(observation.HostSurfaceClassification)
			observationKey := observation.SurfaceID + "\x00" + string(observation.Classification) + "\x00" + observation.OwnerObservationRevision
			if !socketGraphEffectiveClassification(observation.Classification) || len(observation.ReasonCodes) > MaxSocketGraphEvidenceReasons || observation.EvidenceSource != "host_surface" && observation.EvidenceSource != "no_host_surface" || observationKey <= previousObservation {
				return errors.New("socket graph owner observation is invalid")
			}
			previousObservation = observationKey
			if observation.EvidenceSource == "no_host_surface" {
				if observation.Classification != hostsurface.ClassificationUnobserved || observation.HostSurfaceClassification != "" || observation.SurfaceID != "" || observation.RegisteredResourceID != "" || observation.ListenerOwnerCurrent || observation.SemanticOwnerCurrent || observation.OwnerObservationRevision != "" || observation.DeploymentBindingRevision != "" || observation.SemanticOwner != nil || observation.Process != nil || observation.Service != nil || observation.Application != nil {
					return errors.New("socket graph absent-owner observation contains mixed HostSurface facts")
				}
				continue
			}
			if observation.SurfaceID == "" || !rawValid || observation.Classification != expectedClassification {
				return errors.New("socket graph owner classification does not match its HostSurface fact")
			}
			if observation.Classification == hostsurface.ClassificationManagedExact && !observation.ListenerOwnerCurrent {
				return errors.New("socket graph MANAGED_EXACT observation is not current")
			}
			if observation.ListenerOwnerCurrent && (observation.SemanticOwnerCurrent || observation.SemanticOwner != nil) {
				return errors.New("socket graph owner observation mixes listener and semantic ownership")
			}
			if observation.ListenerOwnerCurrent && (!graphEvidenceRevisionToken(observation.OwnerObservationRevision) || !graphEvidenceRevisionToken(observation.DeploymentBindingRevision) || observation.Process == nil || observation.Process.PID == nil || observation.Process.UID == nil || observation.Process.GID == nil || observation.Service == nil || !serviceIdentityEvidenceValid(*observation.Service) || observation.Application == nil) {
				return errors.New("socket graph current owner observation has incomplete bounded identity proof")
			}
			if observation.SemanticOwnerCurrent {
				if observation.ListenerOwnerCurrent || observation.HostSurfaceClassification != hostsurface.ClassificationExpectedExternal || observation.Classification != hostsurface.ClassificationUnknownOwner ||
					observation.SemanticOwner == nil || observation.SemanticOwner.ManagementService != hostsurface.ManagementServiceSSH ||
					!safeSemanticOwnerSource(observation.SemanticOwner.Source) || !graphEvidenceRevisionToken(observation.SemanticOwner.Revision) || !graphEvidenceRevisionToken(observation.SemanticOwner.AuthorityRevision) ||
					!exactSemanticSocketCoverage(observation.SemanticOwner.Socket) || !semanticOwnerSocketMatchesEvidence(observation.SemanticOwner.Socket, observation.Socket) ||
					observation.OwnerObservationRevision != observation.SemanticOwner.Revision || observation.DeploymentBindingRevision != "" || observation.Application != nil ||
					!semanticSSHProcessIdentityEvidenceValid(observation.Process) || observation.Service == nil || !semanticSSHServiceIdentityEvidenceValid(*observation.Service) {
					return errors.New("socket graph current SSH semantic owner observation has incomplete bounded identity proof")
				}
			} else if observation.SemanticOwner != nil {
				return errors.New("socket graph noncurrent observation retains a semantic owner")
			}
		}
	}
	return nil
}

func semanticOwnerSocketMatchesEvidence(socket hostsurface.ListenerSocketIdentityV1, evidence SocketIdentityEvidenceV1) bool {
	if socket.ProofMethod != evidence.ProofMethod || socket.Network != evidence.Network || socket.Family != evidence.Family || socket.Bind != evidence.Bind || socket.Port != evidence.Port ||
		socket.Inode != evidence.Inode || socket.Cookie != evidence.Cookie || len(socket.CoverageFamilies) != len(evidence.CoverageFamilies) {
		return false
	}
	if (socket.IPv6Only == nil) != (evidence.IPv6Only == nil) || socket.IPv6Only != nil && *socket.IPv6Only != *evidence.IPv6Only {
		return false
	}
	for index := range socket.CoverageFamilies {
		if socket.CoverageFamilies[index] != evidence.CoverageFamilies[index] {
			return false
		}
	}
	return true
}

func safeSemanticOwnerSource(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && len(value) <= 128 && !strings.ContainsAny(value, "\x00\r\n\t")
}

func semanticSSHProcessIdentityEvidenceValid(value *ProcessIdentityEvidenceV1) bool {
	return value != nil && value.PID != nil && *value.PID > 1 && value.UID != nil && *value.UID == 0 && value.GID != nil && *value.GID == 0 &&
		value.StartTime != "" && graphEvidenceRevisionToken(value.ExecutableSHA256) && graphEvidenceRevisionToken(value.ExecutablePathSHA256) &&
		value.ExecutableDevice != 0 && value.ExecutableInode != 0
}

func semanticSSHServiceIdentityEvidenceValid(value ServiceIdentityEvidenceV1) bool {
	if value.ProofKind != "procd" {
		return serviceIdentityEvidenceValid(value)
	}
	if value.MainPID == nil || *value.MainPID <= 1 || value.ActiveState != "active" || value.SubState != "running" ||
		value.SystemdUnit != "" || value.FragmentSHA256 != "" || value.FragmentPathSHA256 != "" || value.ControlGroupSHA256 != "" || value.StartMonotonicUsec != 0 ||
		value.ProcdService == "" || value.ProcdInstance == "" || len(value.ProcdCommand) == 0 || len(value.ProcdCommand) > 64 {
		return false
	}
	for _, argument := range value.ProcdCommand {
		if argument == "" || len(argument) > 512 || strings.ContainsAny(argument, "\x00\r\n") {
			return false
		}
	}
	return true
}

func serviceIdentityEvidenceValid(value ServiceIdentityEvidenceV1) bool {
	if value.MainPID == nil || *value.MainPID <= 1 || value.ActiveState != "active" || value.SubState != "running" {
		return false
	}
	switch value.ProofKind {
	case "systemd":
		return value.SystemdUnit != "" && graphEvidenceRevisionToken(value.FragmentSHA256) &&
			graphEvidenceRevisionToken(value.FragmentPathSHA256) && graphEvidenceRevisionToken(value.ControlGroupSHA256) &&
			value.StartMonotonicUsec > 0 && value.ProcdService == "" && value.ProcdInstance == "" &&
			len(value.ProcdCommand) == 0 && value.ProcdUser == "" && value.ProcdGroup == ""
	case "procd":
		if value.SystemdUnit != "" || value.FragmentSHA256 != "" || value.FragmentPathSHA256 != "" ||
			value.ControlGroupSHA256 != "" || value.StartMonotonicUsec != 0 || value.ProcdService == "" ||
			value.ProcdInstance == "" || value.ProcdUser == "" || value.ProcdGroup == "" || len(value.ProcdCommand) == 0 || len(value.ProcdCommand) > 32 {
			return false
		}
		for _, argument := range value.ProcdCommand {
			if argument == "" || len(argument) > 1024 || strings.ContainsAny(argument, "\x00\r\n") {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func validateGraphEvidenceClaimSources(node SocketGraphNodeEvidenceV1) error {
	endpointIDs := make(map[string]struct{}, len(node.Endpoints))
	for _, endpoint := range node.Endpoints {
		if endpoint.EndpointID == "" {
			return errors.New("endpoint source ID is empty")
		}
		endpointIDs[endpoint.EndpointID] = struct{}{}
	}
	observationsByID := make(map[string]SocketOwnerObservationEvidenceV1, len(node.OwnerObservations))
	for _, observation := range node.OwnerObservations {
		if observation.SurfaceID != "" {
			observationsByID[observation.SurfaceID] = observation
		}
	}
	for _, claim := range node.DesiredClaims {
		if _, exists := endpointIDs[claim.SourceID]; exists {
			continue
		}
		resolved := "resolved:" + claim.OwnerObservationRevision + ":" + string(claim.Key.Network) + ":" + string(claim.Key.AddressFamily)
		if claim.OwnerObservationRevision == "" || claim.SourceID != resolved || !graphEvidenceHasResolvedOwner(node.OwnerObservations, claim) {
			return fmt.Errorf("desired claim %q lacks its resolved current owner", claim.ClaimID)
		}
	}
	for _, claim := range node.ObservedClaims {
		observation, exists := observationsByID[claim.SourceID]
		if !exists {
			return fmt.Errorf("observed claim %q lacks HostSurface source %q", claim.ClaimID, claim.SourceID)
		}
		if claim.OwnerObservationRevision != "" {
			listenerOwnerMatch := claim.SemanticOwnerRevision == "" && claim.SemanticOwnerSource == "" &&
				claim.OwnerObservationRevision == observation.OwnerObservationRevision && observation.ListenerOwnerCurrent && observation.Classification == hostsurface.ClassificationManagedExact
			semanticOwnerMatch := claim.SemanticOwnerRevision != "" && claim.SemanticOwnerSource != "" && observation.SemanticOwnerCurrent && observation.SemanticOwner != nil &&
				claim.OwnerObservationRevision == observation.OwnerObservationRevision && claim.SemanticOwnerRevision == observation.SemanticOwner.Revision && claim.SemanticOwnerSource == observation.SemanticOwner.Source
			if (!listenerOwnerMatch && !semanticOwnerMatch) || claim.SocketInode != observation.Socket.Inode || claim.SocketCookie != observation.Socket.Cookie || claim.SocketProofMethod != observation.Socket.ProofMethod || claim.Key.Network != hostresources.Network(observation.Socket.Network) || claim.Key.Port != observation.Socket.Port || !graphEvidenceCoversFamily(observation.Socket.CoverageFamilies, claim.Key.AddressFamily) {
				return fmt.Errorf("observed claim %q differs from HostSurface source %q", claim.ClaimID, claim.SourceID)
			}
		}
	}
	return nil
}

func graphEvidenceHasResolvedOwner(observations []SocketOwnerObservationEvidenceV1, claim SocketClaimEvidenceV1) bool {
	for _, observation := range observations {
		listenerOwnerCurrent := observation.Classification == hostsurface.ClassificationManagedExact && observation.ListenerOwnerCurrent
		semanticOwnerCurrent := observation.SemanticOwnerCurrent && observation.SemanticOwner != nil && observation.SemanticOwner.ManagementService == hostsurface.ManagementServiceSSH
		if (listenerOwnerCurrent || semanticOwnerCurrent) && observation.OwnerObservationRevision == claim.OwnerObservationRevision && claim.Key.Network == hostresources.Network(observation.Socket.Network) && claim.Key.Port == observation.Socket.Port && graphEvidenceCoversFamily(observation.Socket.CoverageFamilies, claim.Key.AddressFamily) {
			return true
		}
	}
	return false
}

func graphEvidenceCoversFamily(families []hostsurface.Family, family hostresources.AddressFamily) bool {
	for _, value := range families {
		if hostresources.AddressFamily(value) == family {
			return true
		}
	}
	return false
}

func graphEvidenceClaimsMatch(evidence []SocketClaimEvidenceV1, claims []SocketClaim) bool {
	if len(evidence) != len(claims) {
		return false
	}
	for index, value := range evidence {
		claim := claims[index]
		if value.ClaimID != claim.ID || value.Kind != claim.Kind || value.SourceID == "" || value.Key != claim.Key || value.SocketInode != claim.SocketInode || value.SocketCookie != claim.SocketCookie || value.SocketProofMethod != claim.SocketProofMethod || value.OwnerObservationRevision != claim.OwnerObservationRevision || value.SemanticOwnerSource != claim.SemanticOwnerSource || value.SemanticOwnerRevision != claim.SemanticOwnerRevision || value.Stale != claim.Stale || value.Ambiguous != claim.Ambiguous || !equalGraphEvidenceStrings(value.ReasonCodes, claim.ReasonCodes) {
			return false
		}
	}
	return true
}

func socketGraphEvidenceRevision(evidence SocketOwnershipGraphEvidenceV1) string {
	copy := evidence
	copy.Revision = ""
	payload, _ := json.Marshal(copy)
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func boundedGraphEvidenceReasons(values []string) ([]string, error) {
	result := normalizedGraphStrings(values)
	if len(result) > MaxSocketGraphEvidenceReasons {
		return nil, errors.New("reason-code count exceeds its contract")
	}
	for _, value := range result {
		if len(value) > 128 || value != strings.ToLower(value) || strings.ContainsAny(value, "/\\?#&={}[]<>\"'\r\n\t ") {
			return nil, errors.New("reason code is not a bounded token")
		}
	}
	return result, nil
}

func socketGraphEvidenceClassification(value hostsurface.Classification) (hostsurface.Classification, bool) {
	switch value {
	case hostsurface.ClassificationManagedExact, hostsurface.ClassificationForeign, hostsurface.ClassificationUnknownOwner, hostsurface.ClassificationUnobserved, hostsurface.ClassificationStale:
		return value, true
	case hostsurface.ClassificationExpectedManaged, hostsurface.ClassificationExpectedExternal, hostsurface.ClassificationLocalOnly, hostsurface.ClassificationUnexpectedPublic:
		return hostsurface.ClassificationUnknownOwner, true
	default:
		return "", false
	}
}

func socketGraphEffectiveClassification(value hostsurface.Classification) bool {
	return value == hostsurface.ClassificationManagedExact || value == hostsurface.ClassificationForeign || value == hostsurface.ClassificationUnknownOwner || value == hostsurface.ClassificationUnobserved || value == hostsurface.ClassificationStale
}

func graphEvidenceRevisionToken(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func graphEvidenceStringDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func equalGraphEvidenceStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
