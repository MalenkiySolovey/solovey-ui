package fronting

import (
	"errors"
	"net/netip"
	"sort"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/fallbacktargets"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

const FrontingStrategyPlanSchemaV2 = "solovey-ui/fronting-strategy-plan/v2"

type FrontingActualStateV2 string

const FrontingActualNotAppliedV2 FrontingActualStateV2 = "NOT_APPLIED"

type FrontingApplyGateV2 string

const FrontingApplyExperimentalDisabledV2 FrontingApplyGateV2 = "EXPERIMENTAL_DISABLED_BY_DEFAULT"

type StrategyProjectionV2 struct {
	Desired  FrontingStrategy      `json:"desired"`
	Selected FrontingStrategy      `json:"selected,omitempty"`
	Actual   FrontingActualStateV2 `json:"actual"`
}

type FrontingRuntimePlanFactsV2 struct {
	IdentityRevision               string              `json:"identityRevision"`
	State                          NginxRuntimeStateV2 `json:"state"`
	StreamCapabilityRevision       string              `json:"streamCapabilityRevision"`
	SSLPrereadCapabilityRevision   string              `json:"sslPrereadCapabilityRevision"`
	ModuleCapabilityRevision       string              `json:"moduleCapabilityRevision"`
	ValidationCapabilityRevision   string              `json:"validationCapabilityRevision"`
	ReloadCapabilityRevision       string              `json:"reloadCapabilityRevision"`
	VerificationCapabilityRevision string              `json:"verificationCapabilityRevision"`
}

type FrontingTargetPlanFactsV2 struct {
	BackendReferences  []hostresources.FrontingBackendReferenceV1  `json:"backendReferences"`
	FallbackReferences []fallbacktargets.FallbackTargetReferenceV2 `json:"fallbackReferences"`
	SelectedProxyMode  hostresources.ProxyMode                     `json:"selectedProxyMode"`
	ReferenceRevisions []string                                    `json:"referenceRevisions"`
}

type FrontingInputExpiryV2 struct {
	Kind      string `json:"kind"`
	Revision  string `json:"revision"`
	ExpiresAt int64  `json:"expiresAt"`
}

type FrontingSafetyPlanFactsV2 struct {
	ManagementExclusionRevision string                  `json:"managementExclusionRevision"`
	Projection                  StrategyProjectionV2    `json:"projection"`
	Blocks                      []string                `json:"blocks"`
	Warnings                    []string                `json:"warnings"`
	ReasonCodes                 []string                `json:"reasonCodes"`
	InputExpiries               []FrontingInputExpiryV2 `json:"inputExpiries"`
}

type FrontingStrategyPlanV2 struct {
	Schema                     string                     `json:"schema"`
	PlanID                     string                     `json:"planId"`
	CanonicalPlanDigest        string                     `json:"canonicalPlanDigest"`
	CreatedAt                  int64                      `json:"createdAt"`
	ExpiresAt                  int64                      `json:"expiresAt"`
	Strategy                   StrategyProjectionV2       `json:"strategy"`
	ApplyGate                  FrontingApplyGateV2        `json:"applyGate"`
	StrategyCapabilityRevision string                     `json:"strategyCapabilityRevision"`
	Runtime                    FrontingRuntimePlanFactsV2 `json:"runtime"`
	PublicSocket               FrontingSocketClaimV1      `json:"publicSocket"`
	Targets                    FrontingTargetPlanFactsV2  `json:"targets"`
	Selectors                  SelectorSetV1              `json:"selectors"`
	Safety                     FrontingSafetyPlanFactsV2  `json:"safety"`
}

type FallbackPlanningTargetV2 struct {
	Reference fallbacktargets.FallbackTargetReferenceV2
	Target    fallbacktargets.FallbackTargetV2
}

type FrontingPlanInputV2 struct {
	Now               time.Time
	DesiredStrategy   FrontingStrategy
	Runtime           NginxRuntimeIdentityV2
	Socket            FrontingSocketClaimV1
	Inventory         []hostresources.FrontingBackendFactV1
	BackendReferences []hostresources.FrontingBackendReferenceV1
	FallbackTargets   []FallbackPlanningTargetV2
	Selectors         SelectorSetV1
	ProxyMode         hostresources.ProxyMode
}

// PlanFrontingStrategyV2 is intentionally value-only. It has no store, lease,
// operation, artifact, helper, DNS or network dependency and therefore cannot
// mutate product or provider state.
func PlanFrontingStrategyV2(input FrontingPlanInputV2) (FrontingStrategyPlanV2, error) {
	now := input.Now.UTC()
	if now.IsZero() {
		return FrontingStrategyPlanV2{}, errors.New("fronting_plan_time_required")
	}
	if input.ProxyMode != hostresources.ProxyModeOff && input.ProxyMode != hostresources.ProxyModeOn {
		return FrontingStrategyPlanV2{}, errors.New("fronting_plan_proxy_mode_invalid")
	}
	if len(input.Inventory) > hostresources.MaxResourceFacts {
		return FrontingStrategyPlanV2{}, errors.New("fronting_plan_inventory_limit_exceeded")
	}
	// Validate potentially outward-bearing socket/selector text against the
	// observation time before constructing any plan. Freshness relative to now
	// remains an explicit block below, but malformed input is never retained.
	if err := input.Socket.Validate(time.Unix(input.Socket.ObservedAt, 0)); err != nil {
		return FrontingStrategyPlanV2{}, errors.New("fronting_plan_socket_contract_invalid")
	}
	if err := input.Selectors.Validate(); err != nil {
		return FrontingStrategyPlanV2{}, errors.New("fronting_plan_selector_contract_invalid")
	}
	blocks, warnings, reasons := []string{}, []string{}, []string{}
	expiries := []FrontingInputExpiryV2{
		{Kind: "runtime", Revision: input.Runtime.CanonicalRuntimeIdentityRevision, ExpiresAt: input.Runtime.ExpiresAt},
		{Kind: "socket", Revision: input.Socket.ClaimRevision, ExpiresAt: input.Socket.ExpiresAt},
	}
	if err := input.Runtime.Validate(now); err != nil {
		blocks = append(blocks, "runtime identity is invalid or stale")
		reasons = append(reasons, "runtime_identity_invalid")
	}
	if err := input.Socket.Validate(now); err != nil {
		blocks = append(blocks, "public socket claim is invalid or stale")
		reasons = append(reasons, "socket_claim_invalid")
	} else if !input.Socket.TopologyMutationEligible || len(input.Socket.ReasonCodes) != 0 {
		blocks = append(blocks, "public socket topology is not eligible")
		reasons = append(reasons, "socket_topology_ineligible")
	}
	if input.Socket.ManagementExclusionRevision != input.Runtime.ManagementExclusionsRevision {
		blocks = append(blocks, "management exclusion revision is inconsistent")
		reasons = append(reasons, "management_exclusion_revision_mismatch")
	}
	if len(input.BackendReferences)+len(input.FallbackTargets) > MaxFixedTargetsV1 {
		blocks = append(blocks, "fixed target limit is exceeded")
		reasons = append(reasons, "fixed_target_limit_exceeded")
	}
	facts := make(map[string]hostresources.FrontingBackendFactV1, len(input.Inventory))
	for _, fact := range input.Inventory {
		key := exactBackendKeyV2(fact.ProviderID, fact.ResourceID, fact.EndpointID)
		if _, exists := facts[key]; exists {
			blocks = append(blocks, "backend inventory contains an ambiguous exact identity")
			reasons = append(reasons, "backend_inventory_ambiguous")
			continue
		}
		facts[key] = fact
	}
	backendReferences := append([]hostresources.FrontingBackendReferenceV1(nil), input.BackendReferences...)
	sort.Slice(backendReferences, func(i, j int) bool {
		return backendReferences[i].CanonicalReferenceRevision < backendReferences[j].CanonicalReferenceRevision
	})
	referenceRevisions := make([]string, 0, len(backendReferences)+len(input.FallbackTargets))
	seenReferenceRevisions := map[string]bool{}
	backendProxy := CapabilitySupportedV2
	for _, reference := range backendReferences {
		if err := reference.Validate(); err != nil {
			return FrontingStrategyPlanV2{}, errors.New("fronting_plan_backend_reference_invalid")
		}
		if seenReferenceRevisions[reference.CanonicalReferenceRevision] {
			blocks = append(blocks, "duplicate exact backend reference")
			reasons = append(reasons, "backend_reference_duplicate")
		}
		seenReferenceRevisions[reference.CanonicalReferenceRevision] = true
		fact, exists := facts[exactBackendKeyV2(reference.ProviderID, reference.ResourceID, reference.EndpointID)]
		if !exists {
			blocks = append(blocks, "exact backend reference is missing")
			reasons = append(reasons, "backend_reference_missing")
			continue
		}
		if input.ProxyMode == hostresources.ProxyModeOn && fact.AcceptsProxyProtocol != hostresources.CapabilityYes {
			backendProxy = backendProxyTruthV2(fact.AcceptsProxyProtocol)
		}
		if reference.SelectedProxyMode != input.ProxyMode || hostresources.ResolveExactFrontingBackendV1(reference, fact, now) != nil {
			blocks = append(blocks, "exact backend reference is stale or ineligible")
			reasons = append(reasons, "backend_reference_stale")
			continue
		}
		expiries = append(expiries, FrontingInputExpiryV2{Kind: "backend", Revision: reference.CanonicalReferenceRevision, ExpiresAt: fact.ExpiresAt})
		referenceRevisions = append(referenceRevisions, reference.CanonicalReferenceRevision)
	}
	fallbackReferences := make([]fallbacktargets.FallbackTargetReferenceV2, 0, len(input.FallbackTargets))
	seenFallbackRevisions := map[string]bool{}
	for _, item := range input.FallbackTargets {
		if err := item.Reference.Validate(); err != nil {
			return FrontingStrategyPlanV2{}, errors.New("fronting_plan_fallback_reference_invalid")
		}
		if fallbacktargets.ResolveExactV2(item.Reference, item.Target, now) != nil {
			blocks = append(blocks, "exact fallback target is stale or ineligible")
			reasons = append(reasons, "fallback_reference_stale")
			continue
		}
		fallbackRevision := v2Revision(item.Reference)
		if seenFallbackRevisions[fallbackRevision] {
			blocks = append(blocks, "duplicate exact fallback reference")
			reasons = append(reasons, "fallback_reference_duplicate")
		}
		seenFallbackRevisions[fallbackRevision] = true
		endpoint := item.Target.Endpoint
		address, addressErr := netip.ParseAddr(endpoint.Address)
		if addressErr != nil || address.Is4In6() || endpoint.Network != hostresources.NetworkTCP || !endpoint.Local || !address.IsLoopback() || endpoint.Port == 0 ||
			(endpoint.AddressFamily == hostresources.AddressFamilyIPv4) != address.Is4() || endpoint.CanReachManagement != hostresources.CapabilityNo {
			blocks = append(blocks, "fallback target endpoint is not an isolated exact local TCP target")
			reasons = append(reasons, "fallback_endpoint_ineligible")
		}
		if input.ProxyMode == hostresources.ProxyModeOn && endpoint.ProxyProtocol != hostresources.CapabilityYes {
			backendProxy = backendProxyTruthV2(endpoint.ProxyProtocol)
			blocks = append(blocks, "fallback target PROXY receive capability is unproven")
			reasons = append(reasons, "proxy_receive_unproven")
		}
		fallbackReferences = append(fallbackReferences, item.Reference)
		referenceRevisions = append(referenceRevisions, fallbackRevision)
		expiries = append(expiries,
			FrontingInputExpiryV2{Kind: "fallback_health", Revision: item.Reference.ProviderHealthRevision, ExpiresAt: item.Target.Health.ExpiresAt},
			FrontingInputExpiryV2{Kind: "fallback_capacity", Revision: item.Reference.CapacityRevision, ExpiresAt: item.Target.Capacity.ExpiresAt})
	}
	sort.Slice(fallbackReferences, func(i, j int) bool { return v2Revision(fallbackReferences[i]) < v2Revision(fallbackReferences[j]) })
	sort.Strings(referenceRevisions)
	if input.DesiredStrategy == StrategyL4OneToOne && len(backendReferences) != 1 {
		blocks = append(blocks, "L4 one-to-one requires exactly one ordinary backend")
		reasons = append(reasons, "l4_exact_backend_required")
	}
	if input.DesiredStrategy == StrategyL4OneToOne && (len(fallbackReferences) != 0 || len(input.Selectors.TargetRevisions) != 0 ||
		input.Selectors.Default.Policy != SelectorDefaultReject) {
		blocks = append(blocks, "L4 one-to-one forbids additional default or fallback targets")
		reasons = append(reasons, "l4_additional_target_forbidden")
	}
	knownTargets := make(map[string]bool, len(referenceRevisions))
	for _, revision := range referenceRevisions {
		knownTargets[revision] = true
	}
	for _, revision := range input.Selectors.TargetRevisions {
		if !knownTargets[revision] {
			blocks = append(blocks, "selector target reference is not an exact supplied target")
			reasons = append(reasons, "selector_target_missing")
		}
	}
	if input.Selectors.Default.Policy == SelectorDefaultNonTLSFixedTarget {
		blocks = append(blocks, "non-TLS classification is not independently proven")
		reasons = append(reasons, "non_tls_discriminator_unproven")
	}
	if input.DesiredStrategy == StrategyL4OneToOne && len(input.Selectors.Tuples) != 0 {
		blocks = append(blocks, "L4 one-to-one does not accept selectors")
		reasons = append(reasons, "l4_selector_forbidden")
	}
	if input.DesiredStrategy == StrategySNIPreread && len(input.Selectors.Tuples) == 0 {
		blocks = append(blocks, "SNI preread requires a finite selector set")
		reasons = append(reasons, "sni_selector_required")
	}
	if input.DesiredStrategy == StrategySNIPreread && selectorSetHasALPNV2(input.Selectors) {
		// NGINX renders ALPN protocol IDs into one comma-delimited variable
		// without escaping commas inside an individual protocol ID. A single
		// token containing ',' is therefore indistinguishable from multiple
		// advertised tokens, so exact membership cannot be proven.
		blocks = append(blocks, "ALPN routing is unavailable because the NGINX preread projection is lossy")
		reasons = append(reasons, "alpn_exact_projection_unavailable")
	}
	capability := ResolveNginxStrategyCapabilityV2(input.Runtime, input.DesiredStrategy, input.ProxyMode, backendProxy, now)
	blocks = append(blocks, capability.Blocks...)
	warnings = append(warnings, capability.Warnings...)
	reasons = append(reasons, capability.ReasonCodes...)
	projection := StrategyProjectionV2{Desired: input.DesiredStrategy, Actual: FrontingActualNotAppliedV2}
	blocks = canonicalMessagesV2(blocks)
	warnings = canonicalMessagesV2(warnings)
	reasons = canonicalRuntimeReasonsV2(reasons)
	if capability.Actionable && len(blocks) == 0 {
		projection.Selected = input.DesiredStrategy
	}
	expiries = canonicalExpiriesV2(expiries)
	expiresAt := now.Unix()
	if len(expiries) > 0 {
		expiresAt = expiries[0].ExpiresAt
	}
	plan := FrontingStrategyPlanV2{
		Schema: FrontingStrategyPlanSchemaV2, CreatedAt: now.Unix(), ExpiresAt: expiresAt,
		Strategy: projection, ApplyGate: FrontingApplyExperimentalDisabledV2,
		StrategyCapabilityRevision: capability.CapabilityRevision,
		Runtime: FrontingRuntimePlanFactsV2{IdentityRevision: input.Runtime.CanonicalRuntimeIdentityRevision,
			State: input.Runtime.State, StreamCapabilityRevision: input.Runtime.Stream.Revision,
			SSLPrereadCapabilityRevision: input.Runtime.SSLPreread.Revision, ModuleCapabilityRevision: input.Runtime.ModuleCapabilityRevision,
			ValidationCapabilityRevision: input.Runtime.ValidationMethod.Revision, ReloadCapabilityRevision: input.Runtime.ReloadMethod.Revision,
			VerificationCapabilityRevision: v2Revision([]string{input.Runtime.ActiveVerification.Revision, input.Runtime.ProcessVerification.Revision, input.Runtime.ListenerVerification.Revision})},
		PublicSocket: input.Socket,
		Targets: FrontingTargetPlanFactsV2{BackendReferences: backendReferences, FallbackReferences: fallbackReferences,
			SelectedProxyMode: input.ProxyMode, ReferenceRevisions: referenceRevisions},
		Selectors: input.Selectors,
		Safety: FrontingSafetyPlanFactsV2{ManagementExclusionRevision: input.Runtime.ManagementExclusionsRevision,
			Projection: projection, Blocks: blocks, Warnings: warnings, ReasonCodes: reasons, InputExpiries: expiries},
	}
	digest := v2Revision(frontingPlanDigestInput(plan))
	plan.CanonicalPlanDigest, plan.PlanID = digest, "fronting_"+digest[:24]
	return plan, nil
}

func selectorSetHasALPNV2(value SelectorSetV1) bool {
	for _, tuple := range value.Tuples {
		if tuple.ALPN != "" {
			return true
		}
	}
	return false
}

func (value FrontingStrategyPlanV2) Validate() error {
	if value.Schema != FrontingStrategyPlanSchemaV2 || value.PlanID == "" || value.CreatedAt <= 0 || value.ExpiresAt <= 0 ||
		value.Strategy.Actual != FrontingActualNotAppliedV2 || value.Safety.Projection.Actual != FrontingActualNotAppliedV2 ||
		value.ApplyGate != FrontingApplyExperimentalDisabledV2 || !frontingHexV2(value.CanonicalPlanDigest) ||
		value.CanonicalPlanDigest != v2Revision(frontingPlanDigestInput(value)) || value.PlanID != "fronting_"+value.CanonicalPlanDigest[:24] ||
		!inputExpiriesCanonicalV2(value.Safety.InputExpiries) || len(value.Safety.InputExpiries) > 0 && value.ExpiresAt != value.Safety.InputExpiries[0].ExpiresAt {
		return errors.New("fronting_strategy_plan_v2_invalid")
	}
	return nil
}

func frontingPlanDigestInput(value FrontingStrategyPlanV2) FrontingStrategyPlanV2 {
	value.PlanID, value.CanonicalPlanDigest = "", ""
	return value
}

func canonicalExpiriesV2(values []FrontingInputExpiryV2) []FrontingInputExpiryV2 {
	seen := map[FrontingInputExpiryV2]bool{}
	result := make([]FrontingInputExpiryV2, 0, len(values))
	for _, value := range values {
		if value.ExpiresAt > 0 && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].ExpiresAt != result[j].ExpiresAt {
			return result[i].ExpiresAt < result[j].ExpiresAt
		}
		if result[i].Kind != result[j].Kind {
			return result[i].Kind < result[j].Kind
		}
		return result[i].Revision < result[j].Revision
	})
	return result
}

func inputExpiriesCanonicalV2(values []FrontingInputExpiryV2) bool {
	expected := canonicalExpiriesV2(values)
	if len(expected) != len(values) {
		return false
	}
	for index := range values {
		if values[index] != expected[index] || safeRuntimeTokenV2(values[index].Kind, 64) == "" ||
			values[index].Revision != "" && !frontingHexV2(values[index].Revision) {
			return false
		}
	}
	return true
}
