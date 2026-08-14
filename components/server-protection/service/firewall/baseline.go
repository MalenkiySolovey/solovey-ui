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
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
	protectionhelper "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/helper"
	protectionpolicy "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/policy"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
	protectionresources "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/resources"
	protectionresponse "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/response"
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
	Repository   *protectionrepository.Repository
	Capabilities BaselineCapabilitySource
}

type BaselineCapabilitySource interface {
	Capabilities(context.Context) (*protectionhelper.CapabilitiesResult, error)
}

type BaselineCapabilityAssessment struct {
	CapabilityRevision   string `json:"capabilityRevision,omitempty"`
	TTLRequired          bool   `json:"ttlRequired"`
	TTLSupported         bool   `json:"ttlSupported"`
	RateRequired         bool   `json:"rateRequired"`
	RateSupported        bool   `json:"rateSupported"`
	CandidateSupported   bool   `json:"candidateSupported"`
	AdvancedState        string `json:"advancedState"`
	Consequence          string `json:"acceptanceConsequence"`
	SSHRecoverySupported bool   `json:"sshRecoverySupported"`
	SSHVerifierRevision  string `json:"sshVerifierRevision,omitempty"`
}

type DecisionResolutionPreview struct {
	Resolution                  protectionresponse.Resolution          `json:"resolution"`
	ManagementGuard             protectionpolicy.ManagementGuardResult `json:"managementGuard"`
	BindingRevision             string                                 `json:"bindingRevision"`
	BaselineEligibilityRevision string                                 `json:"baselineEligibilityRevision"`
	EndpointRevision            string                                 `json:"endpointRevision"`
	ResourceRevision            string                                 `json:"resourceRevision"`
	ConfigurationRevision       string                                 `json:"configurationRevision"`
	Actual                      string                                 `json:"actual"`
}

var ErrUnknownBaselineEndpoint = errors.New("decision target is absent from the configured endpoint inventory")

func NewBaselineService(repository *protectionrepository.Repository) *BaselineService {
	return &BaselineService{Repository: repository}
}

func (s *BaselineService) CapabilityAssessment(ctx context.Context, plan FirewallPlan) BaselineCapabilityAssessment {
	if s == nil || s.Capabilities == nil {
		return AssessBaselineCapabilities(plan, nil)
	}
	capabilities, err := s.Capabilities.Capabilities(ctx)
	if err != nil {
		return AssessBaselineCapabilities(plan, nil)
	}
	return AssessBaselineCapabilities(plan, capabilities)
}

func AssessBaselineCapabilities(plan FirewallPlan, capabilities *protectionhelper.CapabilitiesResult) BaselineCapabilityAssessment {
	result := BaselineCapabilityAssessment{AdvancedState: "DEFERRED_UNPROVEN", Consequence: "BASELINE_BLOCKED"}
	for _, endpoint := range plan.Endpoints {
		for _, contribution := range endpoint.Contributions {
			result.TTLRequired = true
			if contribution.Intent == domain.IntentSoftGraylist || contribution.Intent == domain.IntentRateLimit || contribution.Intent == domain.IntentTemporaryQuarantine {
				result.RateRequired = true
			}
		}
	}
	if capabilities == nil || !capabilities.NFT.Available || !protectionhelper.CapabilityAvailable(capabilities, protectionhelper.OperationNFTValidate) {
		return result
	}
	result.CapabilityRevision = capabilities.Revision
	result.SSHRecoverySupported = capabilities.SSHRecovery.Available && protectionhelper.CapabilityAvailable(capabilities, protectionhelper.OperationSSHRecoveryObserve) && domain.ValidExactRevision(capabilities.SSHRecovery.VerifierRevision)
	if result.SSHRecoverySupported {
		result.SSHVerifierRevision = capabilities.SSHRecovery.VerifierRevision
	}
	result.TTLSupported = capabilities.NFT.TTLSet
	result.RateSupported = capabilities.NFT.RateLimit
	result.CandidateSupported = (!result.TTLRequired || result.TTLSupported) && (!result.RateRequired || result.RateSupported)
	if result.TTLSupported && result.RateSupported {
		result.AdvancedState = "SUPPORTED_BY_READ_ONLY_CHECK"
	} else if result.CandidateSupported {
		result.Consequence = "BASELINE_ONLY_ADVANCED_SCENARIOS_NOT_SHIPPED"
	}
	if result.CandidateSupported && result.Consequence == "BASELINE_BLOCKED" {
		result.Consequence = "CURRENT_CANDIDATE_SUPPORTED"
	}
	return result
}

// ResolveDecisionPreview owns the single policy-to-capability resolution path
// for the current endpoint-managed baseline. HTTP adapters only validate and
// serialize its typed result.
func (s *BaselineService) ResolveDecisionPreview(ctx context.Context, decision domain.ProtectionDecisionV2, expectedBindingRevision string, now time.Time) (DecisionResolutionPreview, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	if err := decision.Validate(now); err != nil {
		return DecisionResolutionPreview{}, err
	}
	state, err := s.Snapshot(ctx, true, nil)
	if err != nil {
		return DecisionResolutionPreview{}, err
	}
	if !domain.ValidExactRevision(expectedBindingRevision) || expectedBindingRevision != state.Binding.Revision {
		return DecisionResolutionPreview{}, ErrPlanRevision
	}
	resource, endpoint, found := BaselineTarget(state, decision.Scope.TargetResourceID)
	if !found {
		return DecisionResolutionPreview{}, ErrUnknownBaselineEndpoint
	}
	guard := protectionpolicy.EvaluateManagementGuard(protectionpolicy.ManagementGuardInput{
		Scope: decision.Scope, Subject: decision.Subject, EndpointKey: endpoint.Key,
		Management: state.Management, RecoveryPaths: state.Recovery, TrustedSources: state.Trusted,
		MayRestrictTraffic: decision.RequestedIntent != domain.IntentObserve, Now: now,
	})
	resolution := protectionresponse.Resolve(protectionresponse.ResolveInput{
		Decision: decision, Strategy: endpoint.Strategy,
		ActionScopeRevision: EndpointActionScopeRevision(state.Plan.Resources), EndpointRevision: endpoint.EndpointRevision,
		ResourceRevision: EndpointActionResourceRevision(resource), ConfigurationRevision: resource.Capabilities.ConfigRevision,
		BaselineApplyBlocked: state.Plan.ApplyBlocked || !state.Plan.BaselineEligibility.CandidateEligible,
		EndpointKnown:        endpoint.EndpointRevision != "", Guard: guard, Now: now,
	})
	return DecisionResolutionPreview{
		Resolution: resolution, ManagementGuard: guard, BindingRevision: state.Binding.Revision,
		BaselineEligibilityRevision: state.Plan.BaselineEligibility.Revision, EndpointRevision: endpoint.EndpointRevision,
		ResourceRevision:      hostresources.Revision(CanonicalPlanResources([]hostresources.ProtectableResource{resource})),
		ConfigurationRevision: resource.Capabilities.ConfigRevision, Actual: "NOT_APPLIED",
	}, nil
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
