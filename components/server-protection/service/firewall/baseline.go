package firewall

import (
	"context"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/netip"
	"sort"
	"strings"
	"time"

	neutralfallback "github.com/MalenkiySolovey/solovey-ui/componenthost/fallbacktargets"
	hostfacts "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	managementregistry "github.com/MalenkiySolovey/solovey-ui/componenthost/management"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
	protectionresources "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/resources"
)

const FirewallBaselineSnapshotBindingSchemaV1 = "solovey-ui/firewall-baseline-snapshot-binding/v1"

var firewallBaselineRuntimeRevision = func() string {
	seed := make([]byte, 32)
	if _, err := cryptorand.Read(seed); err != nil {
		seed = []byte(time.Now().UTC().Format(time.RFC3339Nano))
	}
	sum := sha256.Sum256(seed)
	return hex.EncodeToString(sum[:])
}()

type BaselineSnapshotBindingV1 struct {
	Schema                   string `json:"schema"`
	Revision                 string `json:"revision"`
	RuntimeRevision          string `json:"runtimeRevision"`
	ResourceRevision         string `json:"resourceRevision"`
	GraphRevision            string `json:"graphRevision"`
	GraphEvidenceRevision    string `json:"graphEvidenceRevision"`
	OwnerObservationRevision string `json:"ownerObservationRevision"`
	ConfigurationRevision    string `json:"configurationRevision"`
	PolicyRevision           string `json:"policyRevision"`
	RecoveryRevision         string `json:"recoveryRevision"`
	PlanRevision             string `json:"planRevision"`
	CandidateSHA256          string `json:"candidateSha256"`
	CapturedAt               int64  `json:"capturedAt"`
}

// BaselineState is the service-owned semantic input to firewall preview and
// mutation workflows. Listener observations remain evidence and never replace
// the configured endpoint claims carried by Plan.
type BaselineState struct {
	Graph           protectionresources.SocketOwnershipGraph
	GraphEvidence   protectionresources.SocketOwnershipGraphEvidenceV1
	Topology        ListenerTopologyMutationEligibility
	Plan            FirewallPlan
	Management      []hostresources.ManagementEndpointV1
	Recovery        []hostresources.RecoveryPathV1
	Trusted         []string
	InvalidRecovery int
	Binding         BaselineSnapshotBindingV1
}

type BaselineService struct {
	Repository *protectionrepository.Repository
}

func NewBaselineService(repository *protectionrepository.Repository) *BaselineService {
	return &BaselineService{Repository: repository}
}

func (s *BaselineService) Snapshot(ctx context.Context, refresh bool, selected map[string]struct{}) (BaselineState, error) {
	if s == nil || s.Repository == nil {
		return BaselineState{}, errors.New("firewall baseline repository is unavailable")
	}
	now := time.Now().UTC()
	inventory := protectionresources.Snapshot(ctx, refresh)
	if err := InventoryReady(inventory); err != nil {
		return BaselineState{}, err
	}
	resources := append([]hostresources.ProtectableResource(nil), inventory.Resources...)
	if len(selected) > 0 {
		filtered := make([]hostresources.ProtectableResource, 0, len(selected))
		for _, resource := range resources {
			if _, exists := selected[resource.ID]; exists {
				filtered = append(filtered, resource)
			}
		}
		resources = filtered
		if len(resources) == 0 {
			return BaselineState{}, errors.New("selected endpoint resources are unavailable")
		}
	}

	surfaceSnapshot := hostfacts.CurrentSnapshot()
	targets := make(map[string]protectionresources.LocalTargetStatus)
	targetSnapshot := neutralfallback.Default.Snapshot(ctx, now)
	for _, resource := range resources {
		targetID := strings.TrimSpace(resource.Capabilities.FallbackTargetID)
		if targetID == "" {
			continue
		}
		for _, target := range targetSnapshot.Targets {
			identity := target.Identity.ProviderID + ":" + target.Identity.TargetID
			if targetID != target.Identity.TargetID && targetID != identity {
				continue
			}
			health := string(target.Readiness)
			if target.Readiness == neutralfallback.ReadinessReady {
				health = "HEALTHY"
			}
			targets[resource.ID] = protectionresources.LocalTargetStatus{
				TargetID: identity, EndpointID: target.Endpoint.EndpointID, PublishRevision: target.PublishRevision,
				HealthRevision: target.ProviderHealthRevision, ConfigurationRevision: target.ContentDigest, Health: health,
			}
			break
		}
	}

	graph := protectionresources.BuildSocketOwnershipGraph(protectionresources.SocketGraphInput{
		Resources: resources, Surfaces: surfaceSnapshot.Facts, InventoryTruncated: surfaceSnapshot.Truncated,
		InventoryReasonCodes: surfaceSnapshot.ReasonCodes, OwnerObservationRevision: surfaceSnapshot.OwnerObservationRevision,
		Targets: targets, Now: now,
	})
	topology := EvaluateListenerTopologyMutationEligibility(graph)
	graphEvidence, err := protectionresources.BuildSocketOwnershipGraphEvidence(graph, resources, surfaceSnapshot.Facts, now)
	if err != nil {
		graphEvidence = protectionresources.SocketOwnershipGraphEvidenceV1{}
	}
	management, recovery, invalidRecovery, err := s.ManagementContracts(ctx, inventory.Resources, surfaceSnapshot, now)
	if err != nil {
		return BaselineState{}, err
	}
	trusted, actions, err := s.PolicyInputs(ctx, now)
	if err != nil {
		return BaselineState{}, err
	}
	semanticInput := EndpointPlanInput{
		Graph: graph, Resources: resources, Management: management, RecoveryPaths: recovery,
		TrustedSources: trusted, Actions: actions, RequireSSHKeep: true, Now: now,
	}
	resourceRevision := hostresources.Revision(CanonicalPlanResources(resources))
	configurationRevision := BaselineConfigurationRevision(management)
	policyRevision := hostresources.Revision(struct {
		Trusted []string
		Actions []EndpointActionInput
	}{append([]string(nil), trusted...), append([]EndpointActionInput(nil), actions...)})
	recoveryRevision := hostresources.Revision(recovery)
	inputRevision := hostresources.Revision(struct {
		Schema, Runtime, Resources, Configuration, Policy, Recovery, Semantic string
	}{FirewallBaselineSnapshotBindingSchemaV1, firewallBaselineRuntimeRevision, resourceRevision, configurationRevision, policyRevision, recoveryRevision, EndpointInputRevision(semanticInput)})
	semanticInput.InputRevision = inputRevision
	plan := BuildEndpointPlan(semanticInput)
	binding := BaselineSnapshotBindingV1{
		Schema: FirewallBaselineSnapshotBindingSchemaV1, Revision: inputRevision, RuntimeRevision: firewallBaselineRuntimeRevision,
		ResourceRevision: resourceRevision, GraphRevision: graph.Revision, GraphEvidenceRevision: graphEvidence.Revision,
		OwnerObservationRevision: graph.OwnerObservationRevision, ConfigurationRevision: configurationRevision,
		PolicyRevision: policyRevision, RecoveryRevision: recoveryRevision, PlanRevision: plan.Revision,
		CandidateSHA256: baselineSHA256([]byte(RenderManagedNFT(plan))), CapturedAt: now.Unix(),
	}
	return BaselineState{
		Graph: graph, GraphEvidence: graphEvidence, Topology: topology, Plan: plan,
		Management: management, Recovery: recovery, Trusted: trusted,
		InvalidRecovery: invalidRecovery, Binding: binding,
	}, nil
}

func (s *BaselineService) ManagementContracts(ctx context.Context, resources []hostresources.ProtectableResource, surfaces hostfacts.Snapshot, now time.Time) ([]hostresources.ManagementEndpointV1, []hostresources.RecoveryPathV1, int, error) {
	if s == nil || s.Repository == nil {
		return nil, nil, 0, errors.New("firewall baseline repository is unavailable")
	}
	management := managementregistry.Endpoints(resources, surfaces, now)
	sort.Slice(management, func(i, j int) bool { return management[i].ID < management[j].ID })
	evidence := managementregistry.RecoveryEvidence(ctx, now)
	recovery := make([]hostresources.RecoveryPathV1, 0, len(evidence.Paths))
	invalid := len(evidence.ReasonCodes)
	for _, value := range evidence.Paths {
		value = managementregistry.Effective(value, management, now)
		if !hostresources.RecoveryPathValid(value, now) {
			invalid++
			continue
		}
		recovery = append(recovery, value)
	}
	sort.Slice(recovery, func(i, j int) bool { return recovery[i].ID < recovery[j].ID })
	return management, recovery, invalid, nil
}

func (s *BaselineService) PolicyInputs(ctx context.Context, now time.Time) ([]string, []EndpointActionInput, error) {
	if s == nil || s.Repository == nil {
		return nil, nil, errors.New("firewall baseline repository is unavailable")
	}
	allowlist, _, err := s.Repository.ListIPAllowlist(ctx, protectionrepository.PageQuery{Page: 1, Limit: 4096})
	if err != nil {
		return nil, nil, err
	}
	trusted := make([]string, 0, len(allowlist))
	for _, item := range allowlist {
		if item.ExpiresAt != nil && *item.ExpiresAt <= now.Unix() {
			continue
		}
		prefix, parseErr := netip.ParsePrefix(strings.TrimSpace(item.IPCIDR))
		if parseErr != nil || prefix.Masked().String() != strings.TrimSpace(item.IPCIDR) {
			return nil, nil, errors.New("active trusted source is invalid")
		}
		trusted = append(trusted, prefix.String())
	}
	sort.Strings(trusted)
	// Legacy graylist rows are record-only compatibility data. Kernel planning
	// accepts only a separately validated exact AppliedActionV1.
	return trusted, []EndpointActionInput{}, nil
}

func InventoryReady(inventory protectionresources.InventorySnapshot) error {
	if len(inventory.Errors) > 0 || len(inventory.Resources) == 0 {
		return errors.New("protectable_resource_inventory_incomplete")
	}
	return nil
}

func BaselineConfigurationRevision(endpoints []hostresources.ManagementEndpointV1) string {
	values := make([]struct{ ID, Revision string }, 0, len(endpoints))
	for _, endpoint := range endpoints {
		values = append(values, struct{ ID, Revision string }{endpoint.ID, endpoint.ConfigurationRevision})
	}
	sort.Slice(values, func(i, j int) bool {
		return values[i].ID+"\x00"+values[i].Revision < values[j].ID+"\x00"+values[j].Revision
	})
	return hostresources.Revision(values)
}

func BaselineTarget(state BaselineState, resourceID string) (hostresources.ProtectableResource, EndpointPolicy, bool) {
	var resource hostresources.ProtectableResource
	for _, candidate := range state.Plan.Resources {
		if candidate.ID == resourceID {
			resource = candidate
			break
		}
	}
	if resource.ID == "" {
		return hostresources.ProtectableResource{}, EndpointPolicy{}, false
	}
	matches := make([]EndpointPolicy, 0, 1)
	for _, endpoint := range state.Plan.Endpoints {
		if endpoint.ResourceID == resourceID {
			matches = append(matches, endpoint)
		}
	}
	if len(matches) == 1 {
		return resource, matches[0], true
	}
	return hostresources.ProtectableResource{}, EndpointPolicy{}, false
}

func baselineSHA256(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}
