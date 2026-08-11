package firewall

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
)

const (
	FirewallContributionSchemaV1 = "solovey-ui/managed-firewall-contribution/v1"
	FirewallCompositionSchemaV1  = "solovey-ui/managed-firewall-composition/v1"
	FirewallTransitionSchemaV1   = "solovey-ui/managed-firewall-transition/v1"
	ContributionKindBaseline     = "BASELINE"
	ContributionKindUDPDirect    = "UDP_DIRECT_GUARDED"
	BaselineContributionID       = "managed-firewall:baseline"
)

var (
	ErrContributionConflict = errors.New("managed firewall contribution conflict")
	ErrCompositionInvalid   = errors.New("managed firewall composition is invalid")
)

type ManagedFirewallContributionV1 struct {
	Schema           string                      `json:"schema"`
	ContributionID   string                      `json:"contributionId"`
	Kind             string                      `json:"kind"`
	ResourceID       string                      `json:"resourceId"`
	EndpointID       string                      `json:"endpointId,omitempty"`
	Network          hostresources.Network       `json:"network"`
	AddressFamily    hostresources.AddressFamily `json:"addressFamily"`
	Baseline         *FirewallPlan               `json:"baseline,omitempty"`
	BaselineFallback *FirewallPlan               `json:"baselineFallback,omitempty"`
	UDPPolicy        *UDPFlowPolicyV1            `json:"udpPolicy,omitempty"`
	SemanticRevision string                      `json:"semanticRevision"`
}

type compositionBinding struct {
	ContributionID   string `json:"contributionId"`
	Kind             string `json:"kind"`
	SemanticRevision string `json:"semanticRevision"`
}

type FirewallCompositionV1 struct {
	Schema       string               `json:"schema"`
	Revision     string               `json:"revision"`
	Plan         FirewallPlan         `json:"-"`
	PlanRevision string               `json:"planRevision"`
	CandidateSHA string               `json:"candidateSha256"`
	Bindings     []compositionBinding `json:"bindings"`
}

type FirewallContributionStore interface {
	FirewallAuthority(context.Context) (protectionrepository.FirewallAuthoritySnapshot, error)
	FirewallTransition(context.Context, string) (protectionrepository.FirewallContributionTransitionModel, error)
	CreateFirewallTransition(context.Context, protectionrepository.FirewallContributionTransitionModel) error
	MarkFirewallTransitionMutation(context.Context, string, int64) error
	MarkFirewallTransitionMutationCompleted(context.Context, string, int64) error
	CommitFirewallAuthority(context.Context, string, string, string, *protectionrepository.FirewallContributionModel, protectionrepository.FirewallCompositionModel, string) error
	RecordFirewallTransitionHealth(context.Context, string, string, uint64, string, int64, int64, int64) error
	SetFirewallTransitionState(context.Context, string, string, string) error
}

func contributionFromPlan(plan FirewallPlan) (ManagedFirewallContributionV1, error) {
	udpIndexes := make([]int, 0, 1)
	for index := range plan.Endpoints {
		if plan.Endpoints[index].UDPFlowPolicy != nil {
			udpIndexes = append(udpIndexes, index)
		}
	}
	if len(udpIndexes) == 0 {
		baseline, err := normalizedBaseline(plan)
		if err != nil {
			return ManagedFirewallContributionV1{}, err
		}
		value := ManagedFirewallContributionV1{Schema: FirewallContributionSchemaV1, ContributionID: BaselineContributionID,
			Kind: ContributionKindBaseline, ResourceID: "managed-table:inet:solovey_protection", Network: hostresources.Network("inet"),
			AddressFamily: hostresources.AddressFamily("inet"), Baseline: &baseline}
		return finalizeContribution(value), nil
	}
	if len(udpIndexes) != 1 {
		return ManagedFirewallContributionV1{}, ErrCompositionInvalid
	}
	endpoint := plan.Endpoints[udpIndexes[0]]
	policy := *endpoint.UDPFlowPolicy
	baseline, err := normalizedBaseline(plan)
	if err != nil {
		return ManagedFirewallContributionV1{}, err
	}
	value := ManagedFirewallContributionV1{Schema: FirewallContributionSchemaV1,
		ContributionID: udpContributionID(endpoint.ResourceID, endpoint.Key.AddressFamily),
		Kind:           ContributionKindUDPDirect, ResourceID: endpoint.ResourceID, EndpointID: endpoint.EndpointRevision,
		Network: hostresources.NetworkUDP, AddressFamily: endpoint.Key.AddressFamily, BaselineFallback: &baseline, UDPPolicy: &policy}
	return finalizeContribution(value), nil
}

func ContributionRevision(plan FirewallPlan) string {
	value, err := contributionFromPlan(plan)
	if err != nil {
		return ""
	}
	return value.SemanticRevision
}

// udpContributionID identifies the one logical UDP guard owner for a resource
// and address family. Exact socket identity belongs to the semantic payload,
// not the authority key: socket drift must change the contribution revision
// and enter the existing CAS path instead of creating a parallel owner.
func udpContributionID(resourceID string, family hostresources.AddressFamily) string {
	revision := hostresources.Revision(struct{ Schema, ResourceID, Network, Family string }{FirewallContributionSchemaV1, resourceID, string(hostresources.NetworkUDP), string(family)})
	return "udp:" + revision
}

func normalizedBaseline(plan FirewallPlan) (FirewallPlan, error) {
	copy := cloneFirewallPlan(plan)
	for index := range copy.Endpoints {
		copy.Endpoints[index].UDPFlowPolicy = nil
	}
	copy.Revision = firewallPlanRevision(copy)
	if err := Preflight(copy); err != nil {
		return FirewallPlan{}, err
	}
	return copy, nil
}

func cloneFirewallPlan(plan FirewallPlan) FirewallPlan {
	data, _ := json.Marshal(plan)
	var result FirewallPlan
	_ = json.Unmarshal(data, &result)
	return result
}

func finalizeContribution(value ManagedFirewallContributionV1) ManagedFirewallContributionV1 {
	value.SemanticRevision = ""
	value.SemanticRevision = hostresources.Revision(value)
	return value
}

func validateContribution(value ManagedFirewallContributionV1) error {
	if value.Schema != FirewallContributionSchemaV1 || value.ContributionID == "" || len(value.ContributionID) > 128 || value.ResourceID == "" || len(value.ResourceID) > 256 ||
		value.SemanticRevision == "" || value.SemanticRevision != finalizeContribution(value).SemanticRevision {
		return ErrCompositionInvalid
	}
	switch value.Kind {
	case ContributionKindBaseline:
		if value.ContributionID != BaselineContributionID || value.Baseline == nil || value.BaselineFallback != nil || value.UDPPolicy != nil {
			return ErrCompositionInvalid
		}
		baseline, err := normalizedBaseline(*value.Baseline)
		if err != nil || baseline.Revision != value.Baseline.Revision {
			return ErrCompositionInvalid
		}
	case ContributionKindUDPDirect:
		if value.Baseline != nil || value.BaselineFallback == nil || value.UDPPolicy == nil || value.Network != hostresources.NetworkUDP || value.EndpointID == "" || value.UDPPolicy.Validate() != nil ||
			value.UDPPolicy.ResourceID != value.ResourceID || value.UDPPolicy.EndpointID != value.EndpointID || value.UDPPolicy.AddressFamily != value.AddressFamily ||
			value.ContributionID != udpContributionID(value.ResourceID, value.AddressFamily) {
			return ErrCompositionInvalid
		}
		fallback, err := normalizedBaseline(*value.BaselineFallback)
		if err != nil || fallback.Revision != value.BaselineFallback.Revision {
			return ErrCompositionInvalid
		}
	default:
		return ErrCompositionInvalid
	}
	return nil
}

func composeFirewall(values []ManagedFirewallContributionV1) (FirewallCompositionV1, error) {
	ordered := append([]ManagedFirewallContributionV1(nil), values...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ContributionID < ordered[j].ContributionID })
	seen := map[string]bool{}
	var plan FirewallPlan
	hasBaseline := false
	var fallbackRevision string
	bindings := make([]compositionBinding, 0, len(ordered))
	for _, value := range ordered {
		if err := validateContribution(value); err != nil || seen[value.ContributionID] {
			return FirewallCompositionV1{}, ErrCompositionInvalid
		}
		seen[value.ContributionID] = true
		bindings = append(bindings, compositionBinding{value.ContributionID, value.Kind, value.SemanticRevision})
		if value.Kind == ContributionKindBaseline {
			plan, hasBaseline = cloneFirewallPlan(*value.Baseline), true
		} else if !hasBaseline {
			fallback := cloneFirewallPlan(*value.BaselineFallback)
			if fallbackRevision == "" {
				plan, fallbackRevision = fallback, fallback.Revision
			} else if fallback.Revision != fallbackRevision {
				return FirewallCompositionV1{}, fmt.Errorf("%w: UDP contributions disagree on their baseline fallback", ErrCompositionInvalid)
			}
		}
	}
	if !hasBaseline {
		if fallbackRevision == "" {
			return FirewallCompositionV1{}, fmt.Errorf("%w: baseline contribution and UDP fallback are absent", ErrCompositionInvalid)
		}
		hasBaseline = true
	}
	for _, value := range ordered {
		if value.Kind != ContributionKindUDPDirect {
			continue
		}
		policy := *value.UDPPolicy
		policy.ExpectedManagedTableRevision = plan.Revision
		policy = FinalizeUDPFlowPolicy(policy)
		var err error
		plan, err = AttachUDPFlowPolicy(plan, value.EndpointID, policy)
		if err != nil {
			return FirewallCompositionV1{}, err
		}
	}
	if err := Preflight(plan); err != nil {
		return FirewallCompositionV1{}, err
	}
	candidate := RenderManagedNFT(plan)
	result := FirewallCompositionV1{Schema: FirewallCompositionSchemaV1, Plan: plan, PlanRevision: plan.Revision,
		CandidateSHA: artifactSHA([]byte(candidate)), Bindings: bindings}
	result.Revision = hostresources.Revision(struct {
		Schema, PlanRevision string
		Bindings             []compositionBinding
	}{result.Schema, result.PlanRevision, result.Bindings})
	return result, nil
}

func contributionModel(value ManagedFirewallContributionV1) (protectionrepository.FirewallContributionModel, error) {
	if err := validateContribution(value); err != nil {
		return protectionrepository.FirewallContributionModel{}, err
	}
	data, err := json.Marshal(value)
	if err != nil || len(data) > 2<<20 {
		return protectionrepository.FirewallContributionModel{}, ErrCompositionInvalid
	}
	return protectionrepository.FirewallContributionModel{ContributionID: value.ContributionID, Schema: value.Schema, Kind: value.Kind,
		ResourceID: value.ResourceID, EndpointID: value.EndpointID, Network: string(value.Network), AddressFamily: string(value.AddressFamily),
		SemanticRevision: value.SemanticRevision, SemanticJSON: data}, nil
}

func contributionFromModel(model protectionrepository.FirewallContributionModel) (ManagedFirewallContributionV1, error) {
	var value ManagedFirewallContributionV1
	if len(model.SemanticJSON) == 0 || len(model.SemanticJSON) > 2<<20 || json.Unmarshal(model.SemanticJSON, &value) != nil ||
		validateContribution(value) != nil || model.ContributionID != value.ContributionID || model.SemanticRevision != value.SemanticRevision ||
		model.Kind != value.Kind || model.ResourceID != value.ResourceID || model.EndpointID != value.EndpointID {
		return ManagedFirewallContributionV1{}, ErrCompositionInvalid
	}
	return value, nil
}

func contributionsFromSnapshot(snapshot protectionrepository.FirewallAuthoritySnapshot) ([]ManagedFirewallContributionV1, error) {
	if snapshot.HasComposition && snapshot.Composition.State != "ACTIVE" {
		return nil, ErrContributionConflict
	}
	values := make([]ManagedFirewallContributionV1, 0, len(snapshot.Contributions))
	for _, model := range snapshot.Contributions {
		value, err := contributionFromModel(model)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, nil
}

// EffectiveBaselineAuthorityRevision returns the exact baseline plan revision
// underneath the current semantic aggregate. An explicit baseline contribution
// wins; a UDP-only aggregate may use only one agreed secret-free fallback.
// The persisted composition is re-derived before the revision is exposed so a
// stale or restored row can never make a current baseline appear active.
func EffectiveBaselineAuthorityRevision(snapshot protectionrepository.FirewallAuthoritySnapshot) (string, error) {
	values, err := contributionsFromSnapshot(snapshot)
	if err != nil {
		return "", err
	}
	if !snapshot.HasComposition {
		if len(values) != 0 {
			return "", ErrCompositionInvalid
		}
		return "", nil
	}
	composition, err := composeFirewall(values)
	if err != nil || composition.Revision != snapshot.Composition.Revision || composition.PlanRevision != snapshot.Composition.ManagedPlanRevision || composition.CandidateSHA != snapshot.Composition.CandidateSHA256 {
		return "", errors.Join(ErrCompositionInvalid, err)
	}
	baselineRevision := ""
	for _, value := range values {
		if value.Kind == ContributionKindBaseline {
			return value.Baseline.Revision, nil
		}
		if value.Kind != ContributionKindUDPDirect || value.BaselineFallback == nil {
			continue
		}
		if baselineRevision == "" {
			baselineRevision = value.BaselineFallback.Revision
		} else if baselineRevision != value.BaselineFallback.Revision {
			return "", ErrCompositionInvalid
		}
	}
	if baselineRevision == "" {
		return "", ErrCompositionInvalid
	}
	return baselineRevision, nil
}

// BaselineAuthorityMatchesPlan compares the current authority with the same
// normalized baseline representation used when contributions are persisted.
func BaselineAuthorityMatchesPlan(snapshot protectionrepository.FirewallAuthoritySnapshot, plan FirewallPlan) (bool, error) {
	current, err := EffectiveBaselineAuthorityRevision(snapshot)
	if err != nil {
		return false, err
	}
	expected, err := normalizedBaseline(plan)
	if err != nil {
		return false, err
	}
	return current != "" && current == expected.Revision, nil
}

func replaceContribution(values []ManagedFirewallContributionV1, replacement *ManagedFirewallContributionV1) []ManagedFirewallContributionV1 {
	id := ""
	if replacement != nil {
		id = replacement.ContributionID
	}
	result := make([]ManagedFirewallContributionV1, 0, len(values)+1)
	for _, value := range values {
		if value.ContributionID != id {
			result = append(result, value)
		}
	}
	if replacement != nil {
		result = append(result, *replacement)
	}
	return result
}

func compositionModel(value FirewallCompositionV1) (protectionrepository.FirewallCompositionModel, error) {
	bindings, err := json.Marshal(value.Bindings)
	if err != nil || len(bindings) > 256<<10 || value.Schema != FirewallCompositionSchemaV1 || value.Revision == "" || value.PlanRevision == "" || value.CandidateSHA == "" {
		return protectionrepository.FirewallCompositionModel{}, ErrCompositionInvalid
	}
	return protectionrepository.FirewallCompositionModel{Schema: value.Schema, Revision: value.Revision, ManagedPlanRevision: value.PlanRevision,
		CandidateSHA256: value.CandidateSHA, BindingsJSON: bindings, State: "ACTIVE"}, nil
}

func contributionJSON(value ManagedFirewallContributionV1) json.RawMessage {
	data, _ := json.Marshal(value)
	return data
}

func decodeContributionJSON(data json.RawMessage) (ManagedFirewallContributionV1, error) {
	var value ManagedFirewallContributionV1
	if len(data) == 0 || strings.EqualFold(string(data), "null") || json.Unmarshal(data, &value) != nil || validateContribution(value) != nil {
		return ManagedFirewallContributionV1{}, ErrCompositionInvalid
	}
	return value, nil
}
