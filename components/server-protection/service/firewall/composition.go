package firewall

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	protectionhelper "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/helper"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
)

const (
	FirewallContributionSchemaV1 = "solovey-ui/managed-firewall-contribution/v1"
	FirewallCompositionSchemaV2  = "solovey-ui/managed-firewall-composition/v2"
	FirewallTransitionSchemaV2   = "solovey-ui/managed-firewall-transition/v2"
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

type FirewallCompositionV2 struct {
	Schema                      string               `json:"schema"`
	Revision                    string               `json:"revision"`
	Plan                        FirewallPlan         `json:"-"`
	PlanRevision                string               `json:"planRevision"`
	CandidateSHA                string               `json:"candidateSha256"`
	CandidateSemanticSHA        string               `json:"candidateSemanticSha256,omitempty"`
	CandidateTimedMembershipSHA string               `json:"candidateTimedMembershipSha256,omitempty"`
	Bindings                    []compositionBinding `json:"bindings"`
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
	RecordFirewallObservation(context.Context, protectionrepository.FirewallObservationModel, string) error
	RetireFirewallAuthorityAfterRuntimeLoss(context.Context, string, int, string) error
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
	// Owner-local copies may retain immutable current evidence. JSON storage
	// intentionally cannot revive that authority after a restart.
	result.mutationEvidence = plan.mutationEvidence
	return result
}

func finalizeContribution(value ManagedFirewallContributionV1) ManagedFirewallContributionV1 {
	// Storage retains observations for diagnostics and recovery. Identity uses
	// the plan owner's canonical fact, not the observation-rich stored plan.
	// A separate domain also prevents legacy whole-object hashes from being
	// silently reinterpreted as current semantic authority.
	planRevision := func(plan *FirewallPlan) string {
		if plan == nil {
			return ""
		}
		return firewallPlanRevision(*plan)
	}
	value.SemanticRevision = hostresources.Revision(struct {
		Schema           string
		ContributionID   string
		Kind             string
		ResourceID       string
		EndpointID       string
		Network          hostresources.Network
		AddressFamily    hostresources.AddressFamily
		Baseline         string
		BaselineFallback string
		UDPPolicy        *UDPFlowPolicyV1
	}{"solovey-ui/firewall-contribution-identity/v2", value.ContributionID,
		value.Kind, value.ResourceID, value.EndpointID, value.Network, value.AddressFamily,
		planRevision(value.Baseline), planRevision(value.BaselineFallback), value.UDPPolicy})
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

func composeFirewall(values []ManagedFirewallContributionV1) (FirewallCompositionV2, error) {
	ordered := append([]ManagedFirewallContributionV1(nil), values...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ContributionID < ordered[j].ContributionID })
	seen := map[string]bool{}
	var plan FirewallPlan
	hasBaseline := false
	var fallbackRevision string
	bindings := make([]compositionBinding, 0, len(ordered))
	for _, value := range ordered {
		if err := validateContribution(value); err != nil || seen[value.ContributionID] {
			return FirewallCompositionV2{}, ErrCompositionInvalid
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
				return FirewallCompositionV2{}, fmt.Errorf("%w: UDP contributions disagree on their baseline fallback", ErrCompositionInvalid)
			}
		}
	}
	if !hasBaseline {
		if fallbackRevision == "" {
			return FirewallCompositionV2{}, fmt.Errorf("%w: baseline contribution and UDP fallback are absent", ErrCompositionInvalid)
		}
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
			return FirewallCompositionV2{}, err
		}
	}
	if err := Preflight(plan); err != nil {
		return FirewallCompositionV2{}, err
	}
	candidate := RenderManagedNFT(plan)
	semanticSHA, err := protectionhelper.ManagedSemanticSHA256([]byte(candidate))
	if err != nil {
		return FirewallCompositionV2{}, err
	}
	timedMembershipSHA, err := protectionhelper.ManagedTimedMembershipSHA256([]byte(candidate))
	if err != nil {
		return FirewallCompositionV2{}, err
	}
	result := FirewallCompositionV2{Schema: FirewallCompositionSchemaV2, Plan: plan, PlanRevision: plan.Revision,
		CandidateSHA: artifactSHA([]byte(candidate)), CandidateSemanticSHA: semanticSHA, CandidateTimedMembershipSHA: timedMembershipSHA, Bindings: bindings}
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
		model.Schema != value.Schema || model.Network != string(value.Network) || model.AddressFamily != string(value.AddressFamily) || model.Kind != value.Kind || model.ResourceID != value.ResourceID || model.EndpointID != value.EndpointID {
		return ManagedFirewallContributionV1{}, ErrCompositionInvalid
	}
	return value, nil
}

func matchesCommittedComposition(value FirewallCompositionV2, model protectionrepository.FirewallCompositionModel) bool {
	var bindings []compositionBinding
	return model.Schema == FirewallCompositionSchemaV2 && json.Unmarshal(model.BindingsJSON, &bindings) == nil &&
		hostresources.Revision(bindings) == hostresources.Revision(value.Bindings) && value.Revision == model.Revision && value.PlanRevision == model.ManagedPlanRevision &&
		value.CandidateSHA == model.CandidateSHA256 && value.CandidateSemanticSHA == model.CandidateSemanticSHA256 && value.CandidateTimedMembershipSHA == model.CandidateTimedMembershipSHA256
}

func contributionsFromSnapshot(snapshot protectionrepository.FirewallAuthoritySnapshot) ([]ManagedFirewallContributionV1, error) {
	if snapshot.HasComposition && snapshot.Composition.State != "ACTIVE" {
		return nil, ErrContributionConflict
	}
	if snapshot.HasComposition && (!snapshot.HasObservation || snapshot.Observation.State != FirewallLiveMatching || !snapshot.Observation.HasCommittedAuthority ||
		snapshot.Observation.CommittedCompositionRevision != snapshot.Composition.Revision || snapshot.Observation.CurrentRevision != snapshot.Composition.ManagedPlanRevision ||
		snapshot.Observation.CurrentSemanticSHA256 != snapshot.Composition.CandidateSemanticSHA256 || snapshot.Observation.CurrentTimedMembershipSHA256 == "" ||
		snapshot.Observation.CurrentTimedMembershipSHA256 != snapshot.Observation.ExpectedTimedMembershipSHA256) {
		return nil, ErrContributionConflict
	}
	return contributionsFromModels(snapshot.Contributions)
}

func contributionsFromModels(models []protectionrepository.FirewallContributionModel) ([]ManagedFirewallContributionV1, error) {
	values := make([]ManagedFirewallContributionV1, 0, len(models))
	for _, model := range models {
		value, err := contributionFromModel(model)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, nil
}

func expectedTimedMembershipSHA(values []ManagedFirewallContributionV1, now time.Time) (string, error) {
	composition, err := composeFirewall(values)
	if err != nil {
		return "", err
	}
	plan := cloneFirewallPlan(composition.Plan)
	cutoff := now.UTC().Unix()
	for endpointIndex := range plan.Endpoints {
		kept := plan.Endpoints[endpointIndex].Contributions[:0]
		for _, contribution := range plan.Endpoints[endpointIndex].Contributions {
			if contribution.ExpiresAt > cutoff {
				kept = append(kept, contribution)
			}
		}
		plan.Endpoints[endpointIndex].Contributions = kept
	}
	return protectionhelper.ManagedTimedMembershipSHA256([]byte(RenderManagedNFT(plan)))
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
	if err != nil || composition.Revision != snapshot.Composition.Revision || composition.PlanRevision != snapshot.Composition.ManagedPlanRevision || composition.CandidateSHA != snapshot.Composition.CandidateSHA256 || composition.CandidateSemanticSHA != snapshot.Composition.CandidateSemanticSHA256 || composition.CandidateTimedMembershipSHA != snapshot.Composition.CandidateTimedMembershipSHA256 {
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

func compositionModel(value FirewallCompositionV2) (protectionrepository.FirewallCompositionModel, error) {
	bindings, err := json.Marshal(value.Bindings)
	if err != nil || len(bindings) > 256<<10 || value.Schema != FirewallCompositionSchemaV2 || value.Revision == "" || value.PlanRevision == "" || value.CandidateSHA == "" || value.CandidateSemanticSHA == "" || value.CandidateTimedMembershipSHA == "" {
		return protectionrepository.FirewallCompositionModel{}, ErrCompositionInvalid
	}
	return protectionrepository.FirewallCompositionModel{Schema: value.Schema, Revision: value.Revision, ManagedPlanRevision: value.PlanRevision,
		CandidateSHA256: value.CandidateSHA, CandidateSemanticSHA256: value.CandidateSemanticSHA, CandidateTimedMembershipSHA256: value.CandidateTimedMembershipSHA, BindingsJSON: bindings, State: "ACTIVE"}, nil
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
