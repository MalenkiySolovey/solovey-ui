package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	neutralfallback "github.com/MalenkiySolovey/solovey-ui/componenthost/fallbacktargets"
)

const (
	NativeFallbackPlanSchemaV1  = "solovey-ui/native-fallback-plan/v1"
	NativeFallbackStateSchemaV1 = "solovey-ui/native-fallback-state/v1"
	MaxNativeFallbackPlanLife   = 5 * time.Minute
)

type NativeFallbackReasonCode string

const (
	NativeReasonRuntimeIdentityUnknown        NativeFallbackReasonCode = "runtime_identity_unknown"
	NativeReasonRuntimeIdentityMismatch       NativeFallbackReasonCode = "runtime_identity_mismatch"
	NativeReasonCapabilityUnsupported         NativeFallbackReasonCode = "capability_unsupported"
	NativeReasonCapabilityUnknown             NativeFallbackReasonCode = "capability_unknown"
	NativeReasonConfigurationStale            NativeFallbackReasonCode = "configuration_stale"
	NativeReasonEffectiveStateStale           NativeFallbackReasonCode = "effective_state_stale"
	NativeReasonTargetReferenceStale          NativeFallbackReasonCode = "target_reference_stale"
	NativeReasonTargetInvalid                 NativeFallbackReasonCode = "target_invalid"
	NativeReasonTargetNotLocal                NativeFallbackReasonCode = "target_not_local"
	NativeReasonTargetProtocolMismatch        NativeFallbackReasonCode = "target_protocol_mismatch"
	NativeReasonTargetTLSModeMismatch         NativeFallbackReasonCode = "target_tls_mode_mismatch"
	NativeReasonTargetServerNameMismatch      NativeFallbackReasonCode = "target_server_name_mismatch"
	NativeReasonTargetALPNMismatch            NativeFallbackReasonCode = "target_alpn_mismatch"
	NativeReasonTargetHealthUnknown           NativeFallbackReasonCode = "target_health_unknown"
	NativeReasonTargetHealthStale             NativeFallbackReasonCode = "target_health_stale"
	NativeReasonTargetNotReady                NativeFallbackReasonCode = "target_not_ready"
	NativeReasonTargetCapacityUnknown         NativeFallbackReasonCode = "target_capacity_unknown"
	NativeReasonTargetCapacityStale           NativeFallbackReasonCode = "target_capacity_stale"
	NativeReasonTargetCapacityPressured       NativeFallbackReasonCode = "target_capacity_pressured"
	NativeReasonTargetCapacityExhausted       NativeFallbackReasonCode = "target_capacity_exhausted"
	NativeReasonManagementReachabilityUnknown NativeFallbackReasonCode = "management_reachability_unknown"
	NativeReasonManagementTargetForbidden     NativeFallbackReasonCode = "management_target_forbidden"
	NativeReasonCorePreviewBlocked            NativeFallbackReasonCode = "core_preview_blocked"
	NativeReasonCorePreviewStale              NativeFallbackReasonCode = "core_preview_stale"
	NativeReasonApplyDisabled                 NativeFallbackReasonCode = "apply_disabled"
	NativeReasonExperimentalOnly              NativeFallbackReasonCode = "experimental_only"
	NativeReasonStateAbsent                   NativeFallbackReasonCode = "state_absent"
	NativeReasonStateRecordInvalid            NativeFallbackReasonCode = "state_record_invalid"
	NativeReasonStateReconciliationRequired   NativeFallbackReasonCode = "actual_state_reverification_required"
)

type NativeFallbackDesiredState string

const NativeFallbackDesired NativeFallbackDesiredState = "NATIVE_FALLBACK"

type NativeFallbackVariant string

const (
	NativeFallbackVariantNone              NativeFallbackVariant = "NONE"
	NativeFallbackVariantUnsupported       NativeFallbackVariant = "UNSUPPORTED"
	NativeFallbackVLESSRealityHandshakeTCP NativeFallbackVariant = "VLESS_REALITY_HANDSHAKE_TCP"
	NativeFallbackTrojanDefaultTCP         NativeFallbackVariant = "TROJAN_DEFAULT_FALLBACK_TCP"
	NativeFallbackTrojanALPNTCP            NativeFallbackVariant = "TROJAN_ALPN_FALLBACK_TCP"
)

type NativeFallbackApplyGate string

const (
	NativeApplyDisabledByDefault NativeFallbackApplyGate = "DISABLED_BY_DEFAULT"
	NativeApplyExperimental      NativeFallbackApplyGate = "EXPERIMENTAL"
	NativeApplyStable            NativeFallbackApplyGate = "STABLE"
)

type NativeFallbackActualState string

const (
	NativeActualNotApplied        NativeFallbackActualState = "NOT_APPLIED"
	NativeActualPrepared          NativeFallbackActualState = "PREPARED"
	NativeActualApplying          NativeFallbackActualState = "APPLYING"
	NativeActualHealth            NativeFallbackActualState = "HEALTH"
	NativeActualApplied           NativeFallbackActualState = "APPLIED"
	NativeActualDegraded          NativeFallbackActualState = "DEGRADED"
	NativeActualRollingBack       NativeFallbackActualState = "ROLLING_BACK"
	NativeActualRolledBack        NativeFallbackActualState = "ROLLED_BACK"
	NativeActualRollbackFailed    NativeFallbackActualState = "ROLLBACK_FAILED"
	NativeActualReconcileRequired NativeFallbackActualState = "RECONCILE_REQUIRED"
)

type NativeFallbackResourceBindingV1 struct {
	ResourceID            string `json:"resourceId"`
	InboundDatabaseID     uint   `json:"inboundDatabaseId"`
	InboundTag            string `json:"inboundTag"`
	InboundType           string `json:"inboundType"`
	SourceRevision        string `json:"sourceRevision"`
	ResourceRevision      string `json:"resourceRevision"`
	ConfigurationRevision string `json:"configurationRevision"`
	EffectiveRevision     string `json:"effectiveRevision,omitempty"`
}

type NativeFallbackRuntimeBindingV1 struct {
	IdentityRevision           string                `json:"identityRevision"`
	CapabilityResolverRevision string                `json:"capabilityResolverRevision"`
	AdmittedVariant            NativeFallbackVariant `json:"admittedVariant"`
}

type NativeFallbackTargetBindingV1 struct {
	Reference                 neutralfallback.FallbackTargetReferenceV2 `json:"reference"`
	CanonicalTargetRevision   string                                    `json:"canonicalTargetRevision,omitempty"`
	EndpointID                string                                    `json:"endpointId,omitempty"`
	EndpointRevision          string                                    `json:"endpointRevision,omitempty"`
	PublishRevision           string                                    `json:"publishRevision,omitempty"`
	ContentDigest             string                                    `json:"contentDigest,omitempty"`
	ProviderRevision          string                                    `json:"providerRevision,omitempty"`
	HealthRevision            string                                    `json:"healthRevision,omitempty"`
	HealthState               string                                    `json:"healthState,omitempty"`
	HealthExpiresAt           time.Time                                 `json:"healthExpiresAt,omitempty"`
	CapacityRevision          string                                    `json:"capacityRevision,omitempty"`
	CapacityState             string                                    `json:"capacityState,omitempty"`
	CapacityExpiresAt         time.Time                                 `json:"capacityExpiresAt,omitempty"`
	ReservationSlotsTotal     uint32                                    `json:"reservationSlotsTotal,omitempty"`
	ReservationSlotsUsed      uint32                                    `json:"reservationSlotsUsed,omitempty"`
	Network                   string                                    `json:"network,omitempty"`
	AddressFamily             string                                    `json:"addressFamily,omitempty"`
	Local                     bool                                      `json:"local"`
	TransportSecurity         string                                    `json:"transportSecurity,omitempty"`
	ApplicationProtocols      []string                                  `json:"applicationProtocols,omitempty"`
	AcceptedServerNameDigests []string                                  `json:"acceptedServerNameDigests,omitempty"`
	RequiredServerNameDigest  string                                    `json:"requiredServerNameDigest,omitempty"`
	ProxyProtocol             string                                    `json:"proxyProtocol,omitempty"`
	ManagementReachability    string                                    `json:"managementReachability,omitempty"`
}

type NativeFallbackCorePreviewBindingV1 struct {
	Digest                      string    `json:"digest,omitempty"`
	BeforeConfigurationRevision string    `json:"beforeConfigurationRevision,omitempty"`
	ExpectedAfterRevision       string    `json:"expectedAfterRevision,omitempty"`
	CurrentSafeSubtreeDigest    string    `json:"currentSafeSubtreeDigest,omitempty"`
	CandidateSafeSubtreeDigest  string    `json:"candidateSafeSubtreeDigest,omitempty"`
	ApprovedEndpointFactDigest  string    `json:"approvedEndpointFactDigest,omitempty"`
	ReplaceDefaultToo           bool      `json:"replaceDefaultToo"`
	ExpiresAt                   time.Time `json:"expiresAt,omitempty"`
}

type NativeFallbackManagementBindingV1 struct {
	State       string                     `json:"state"`
	Revision    string                     `json:"revision,omitempty"`
	ExpiresAt   time.Time                  `json:"expiresAt,omitempty"`
	ReasonCodes []NativeFallbackReasonCode `json:"reasonCodes,omitempty"`
}

type NativeFallbackPlanV1 struct {
	Schema              string                             `json:"schema"`
	PlanID              string                             `json:"planId"`
	PlanDigest          string                             `json:"planDigest"`
	CreatedAt           time.Time                          `json:"createdAt"`
	ExpiresAt           time.Time                          `json:"expiresAt"`
	Resource            NativeFallbackResourceBindingV1    `json:"resource"`
	Runtime             NativeFallbackRuntimeBindingV1     `json:"runtime"`
	Target              NativeFallbackTargetBindingV1      `json:"target"`
	CorePreview         NativeFallbackCorePreviewBindingV1 `json:"corePreview"`
	ManagementIsolation NativeFallbackManagementBindingV1  `json:"managementIsolation"`
	ApplyGate           NativeFallbackApplyGate            `json:"applyGate"`
	DesiredState        NativeFallbackDesiredState         `json:"desiredState"`
	SelectedVariant     NativeFallbackVariant              `json:"selectedVariant"`
	ActualState         NativeFallbackActualState          `json:"actualState"`
	Eligible            bool                               `json:"eligible"`
	Blocks              []NativeFallbackReasonCode         `json:"blocks,omitempty"`
	Warnings            []NativeFallbackReasonCode         `json:"warnings,omitempty"`
	ReasonCodes         []NativeFallbackReasonCode         `json:"reasonCodes,omitempty"`
}

func (plan *NativeFallbackPlanV1) Finalize() error {
	if plan == nil {
		return errors.New("native fallback plan is nil")
	}
	canonicalizeNativePlan(plan)
	plan.PlanID, plan.PlanDigest = "", ""
	payload, err := json.Marshal(plan)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(payload)
	plan.PlanDigest = hex.EncodeToString(sum[:])
	plan.PlanID = plan.PlanDigest
	return plan.Validate()
}

func (plan NativeFallbackPlanV1) Validate() error {
	if plan.Schema != NativeFallbackPlanSchemaV1 || !ValidSHA256(plan.PlanID) || plan.PlanID != plan.PlanDigest ||
		plan.CreatedAt.IsZero() || plan.ExpiresAt.IsZero() || plan.ExpiresAt.After(plan.CreatedAt.Add(MaxNativeFallbackPlanLife)) ||
		!ValidContractID(plan.Resource.ResourceID, 256) || plan.Resource.InboundDatabaseID == 0 || !ValidContractID(plan.Resource.InboundTag, 128) || !ValidContractID(plan.Resource.InboundType, 64) ||
		!ValidSHA256(plan.Resource.SourceRevision) || !ValidSHA256(plan.Resource.ResourceRevision) || !ValidSHA256(plan.Resource.ConfigurationRevision) ||
		plan.DesiredState != NativeFallbackDesired || plan.ActualState != NativeActualNotApplied ||
		(plan.ApplyGate != NativeApplyDisabledByDefault && plan.ApplyGate != NativeApplyExperimental) || !validNativeVariant(plan.SelectedVariant) || !validNativeVariant(plan.Runtime.AdmittedVariant) {
		return errors.New("native fallback plan identity or safety state is invalid")
	}
	if err := plan.Target.Reference.Validate(); err != nil {
		return errors.New("native fallback target reference is invalid")
	}
	if !validNativeReasons(plan.Blocks) || !validNativeReasons(plan.Warnings) || !validNativeReasons(plan.ReasonCodes) {
		return errors.New("native fallback plan reasons are invalid")
	}
	if plan.Eligible != (len(plan.Blocks) == 0) {
		return errors.New("native fallback plan eligibility is inconsistent")
	}
	if plan.Resource.EffectiveRevision != "" && !ValidSHA256(plan.Resource.EffectiveRevision) {
		return errors.New("native fallback effective revision is invalid")
	}
	if err := validateNativePlanBindings(plan); err != nil {
		return err
	}
	copy := plan
	canonicalizeNativePlan(&copy)
	copy.PlanID, copy.PlanDigest = "", ""
	payload, _ := json.Marshal(copy)
	sum := sha256.Sum256(payload)
	if hex.EncodeToString(sum[:]) != plan.PlanDigest {
		return errors.New("native fallback plan digest mismatch")
	}
	return nil
}

func validateNativePlanBindings(plan NativeFallbackPlanV1) error {
	for _, value := range []string{plan.Target.EndpointID, plan.Target.PublishRevision, plan.Target.ProviderRevision, plan.Target.HealthState,
		plan.Target.CapacityState, plan.Target.Network, plan.Target.AddressFamily, plan.Target.TransportSecurity, plan.Target.ProxyProtocol,
		plan.Target.ManagementReachability, plan.ManagementIsolation.State} {
		if value != "" && !ValidContractID(value, 128) {
			return errors.New("native fallback plan contains an unsafe target fact")
		}
	}
	for _, value := range plan.Target.ApplicationProtocols {
		if !ValidContractID(value, 64) {
			return errors.New("native fallback target protocol is invalid")
		}
	}
	for _, value := range plan.Target.AcceptedServerNameDigests {
		if !ValidSHA256(value) {
			return errors.New("native fallback accepted server name binding is invalid")
		}
	}
	for _, value := range []string{plan.Target.CanonicalTargetRevision, plan.Target.EndpointRevision, plan.Target.ContentDigest,
		plan.Target.HealthRevision, plan.Target.CapacityRevision, plan.Target.RequiredServerNameDigest,
		plan.Runtime.IdentityRevision, plan.Runtime.CapabilityResolverRevision, plan.CorePreview.Digest,
		plan.CorePreview.BeforeConfigurationRevision, plan.CorePreview.ExpectedAfterRevision, plan.CorePreview.CurrentSafeSubtreeDigest,
		plan.CorePreview.CandidateSafeSubtreeDigest, plan.CorePreview.ApprovedEndpointFactDigest, plan.ManagementIsolation.Revision} {
		if value != "" && !ValidSHA256(value) {
			return errors.New("native fallback plan revision binding is invalid")
		}
	}
	for _, factExpiry := range []time.Time{
		plan.Target.HealthExpiresAt,
		plan.Target.CapacityExpiresAt,
		plan.ManagementIsolation.ExpiresAt,
		plan.CorePreview.ExpiresAt,
	} {
		if !factExpiry.IsZero() && plan.ExpiresAt.After(factExpiry) {
			return errors.New("native fallback plan outlives an input fact")
		}
	}
	if plan.Eligible {
		if plan.SelectedVariant == NativeFallbackVariantNone || plan.SelectedVariant == NativeFallbackVariantUnsupported ||
			plan.Runtime.AdmittedVariant != plan.SelectedVariant || !plan.ExpiresAt.After(plan.CreatedAt) ||
			plan.Resource.EffectiveRevision == "" || plan.Runtime.IdentityRevision == "" || plan.Runtime.CapabilityResolverRevision == "" ||
			!plan.Target.Local || plan.Target.CanonicalTargetRevision == "" || plan.Target.EndpointID == "" || plan.Target.EndpointRevision == "" ||
			plan.Target.PublishRevision == "" || plan.Target.ContentDigest == "" || plan.Target.ProviderRevision == "" ||
			plan.Target.HealthRevision == "" || plan.Target.HealthExpiresAt.IsZero() || plan.Target.CapacityRevision == "" || plan.Target.CapacityExpiresAt.IsZero() ||
			plan.CorePreview.Digest == "" || plan.CorePreview.BeforeConfigurationRevision == "" || plan.CorePreview.ExpectedAfterRevision == "" ||
			plan.CorePreview.CurrentSafeSubtreeDigest == "" || plan.CorePreview.CandidateSafeSubtreeDigest == "" ||
			plan.CorePreview.ApprovedEndpointFactDigest == "" || plan.CorePreview.ExpiresAt.IsZero() ||
			plan.ManagementIsolation.State != "ISOLATED" || plan.ManagementIsolation.Revision == "" || plan.ManagementIsolation.ExpiresAt.IsZero() {
			return errors.New("eligible native fallback plan lacks exact safety bindings")
		}
		if plan.Target.EndpointID != plan.Target.Reference.EndpointID || plan.Target.EndpointRevision != plan.Target.Reference.EndpointRevision ||
			plan.Target.PublishRevision != plan.Target.Reference.PublishRevision || plan.Target.ContentDigest != plan.Target.Reference.ContentDigest ||
			plan.Target.ProviderRevision != plan.Target.Reference.ProviderRevision || plan.Target.HealthRevision != plan.Target.Reference.ProviderHealthRevision ||
			plan.Target.CapacityRevision != plan.Target.Reference.CapacityRevision ||
			plan.CorePreview.BeforeConfigurationRevision != plan.Resource.ConfigurationRevision {
			return errors.New("eligible native fallback plan has inconsistent exact bindings")
		}
	}
	return nil
}

type NativeFallbackStateV1 struct {
	Schema                      string                     `json:"schema"`
	ResourceID                  string                     `json:"resourceId"`
	InboundDatabaseID           uint                       `json:"inboundDatabaseId"`
	LatestPlanID                string                     `json:"latestPlanId,omitempty"`
	LatestPlanDigest            string                     `json:"latestPlanDigest,omitempty"`
	RuntimeIdentityRevision     string                     `json:"runtimeIdentityRevision,omitempty"`
	CapabilityResolverRevision  string                     `json:"capabilityResolverRevision,omitempty"`
	BeforeConfigurationRevision string                     `json:"beforeConfigurationRevision,omitempty"`
	AfterConfigurationRevision  string                     `json:"afterConfigurationRevision,omitempty"`
	EffectiveRevision           string                     `json:"effectiveRevision,omitempty"`
	TargetRevision              string                     `json:"targetRevision,omitempty"`
	ProviderRevision            string                     `json:"providerRevision,omitempty"`
	EndpointRevision            string                     `json:"endpointRevision,omitempty"`
	PublishRevision             string                     `json:"publishRevision,omitempty"`
	HealthRevision              string                     `json:"healthRevision,omitempty"`
	CapacityRevision            string                     `json:"capacityRevision,omitempty"`
	ProviderReservationID       string                     `json:"providerReservationId,omitempty"`
	ProviderReservationRevision string                     `json:"providerReservationRevision,omitempty"`
	OperationID                 string                     `json:"operationId,omitempty"`
	OperationRevision           string                     `json:"operationRevision,omitempty"`
	DesiredState                NativeFallbackDesiredState `json:"desiredState"`
	SelectedVariant             NativeFallbackVariant      `json:"selectedVariant"`
	ActualState                 NativeFallbackActualState  `json:"actualState"`
	LastGoodCheckpointID        string                     `json:"lastGoodCheckpointId,omitempty"`
	LastGoodCheckpointDigest    string                     `json:"lastGoodCheckpointDigest,omitempty"`
	ReasonCodes                 []NativeFallbackReasonCode `json:"reasonCodes,omitempty"`
	CreatedAt                   time.Time                  `json:"createdAt,omitempty"`
	UpdatedAt                   time.Time                  `json:"updatedAt,omitempty"`
}

func (state NativeFallbackStateV1) ValidateStored() error {
	if state.Schema != NativeFallbackStateSchemaV1 || !ValidContractID(state.ResourceID, 256) || state.InboundDatabaseID == 0 ||
		state.DesiredState != NativeFallbackDesired || !validNativeVariant(state.SelectedVariant) || !validNativeActualState(state.ActualState) ||
		state.CreatedAt.IsZero() || state.UpdatedAt.IsZero() || state.UpdatedAt.Before(state.CreatedAt) || !validNativeReasons(state.ReasonCodes) {
		return errors.New("native fallback state is invalid")
	}
	for _, revision := range []string{state.LatestPlanID, state.LatestPlanDigest, state.RuntimeIdentityRevision, state.CapabilityResolverRevision,
		state.BeforeConfigurationRevision, state.AfterConfigurationRevision, state.EffectiveRevision, state.TargetRevision, state.EndpointRevision,
		state.HealthRevision, state.CapacityRevision, state.LastGoodCheckpointDigest} {
		if revision != "" && !ValidSHA256(revision) {
			return errors.New("native fallback state revision is invalid")
		}
	}
	for _, opaque := range []string{state.ProviderRevision, state.PublishRevision, state.ProviderReservationID, state.ProviderReservationRevision,
		state.OperationID, state.OperationRevision, state.LastGoodCheckpointID} {
		if opaque != "" && !ValidContractID(opaque, 128) {
			return errors.New("native fallback state reference is invalid")
		}
	}
	return nil
}

func CanonicalNativeFallbackReasons(values ...[]NativeFallbackReasonCode) []NativeFallbackReasonCode {
	seen := make(map[NativeFallbackReasonCode]struct{})
	for _, group := range values {
		for _, value := range group {
			if ValidContractID(string(value), maxReasonCodeBytes) {
				seen[value] = struct{}{}
			}
		}
	}
	result := make([]NativeFallbackReasonCode, 0, len(seen))
	for value := range seen {
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	if len(result) > maxReasonCodes {
		result = result[:maxReasonCodes]
	}
	return result
}

func canonicalizeNativePlan(plan *NativeFallbackPlanV1) {
	plan.CreatedAt = plan.CreatedAt.UTC().Truncate(time.Second)
	plan.ExpiresAt = plan.ExpiresAt.UTC().Truncate(time.Second)
	plan.Target.HealthExpiresAt = plan.Target.HealthExpiresAt.UTC().Truncate(time.Second)
	plan.Target.CapacityExpiresAt = plan.Target.CapacityExpiresAt.UTC().Truncate(time.Second)
	plan.CorePreview.ExpiresAt = plan.CorePreview.ExpiresAt.UTC().Truncate(time.Second)
	plan.ManagementIsolation.ExpiresAt = plan.ManagementIsolation.ExpiresAt.UTC().Truncate(time.Second)
	plan.Target.ApplicationProtocols = canonicalNativeStrings(plan.Target.ApplicationProtocols)
	plan.Target.AcceptedServerNameDigests = canonicalNativeStrings(plan.Target.AcceptedServerNameDigests)
	plan.ManagementIsolation.ReasonCodes = CanonicalNativeFallbackReasons(plan.ManagementIsolation.ReasonCodes)
	plan.Blocks = CanonicalNativeFallbackReasons(plan.Blocks)
	plan.Warnings = CanonicalNativeFallbackReasons(plan.Warnings)
	plan.ReasonCodes = CanonicalNativeFallbackReasons(plan.Blocks, plan.Warnings, plan.ReasonCodes)
	plan.Eligible = len(plan.Blocks) == 0
}

func canonicalNativeStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			seen[value] = struct{}{}
		}
	}
	result := make([]string, 0, len(seen))
	for value := range seen {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func validNativeReasons(values []NativeFallbackReasonCode) bool {
	if len(values) > maxReasonCodes {
		return false
	}
	for index, value := range values {
		if !ValidContractID(string(value), maxReasonCodeBytes) || index > 0 && values[index-1] >= value {
			return false
		}
	}
	return true
}

func validNativeVariant(value NativeFallbackVariant) bool {
	switch value {
	case NativeFallbackVariantNone, NativeFallbackVariantUnsupported, NativeFallbackVLESSRealityHandshakeTCP, NativeFallbackTrojanDefaultTCP, NativeFallbackTrojanALPNTCP:
		return true
	default:
		return false
	}
}

func validNativeActualState(value NativeFallbackActualState) bool {
	switch value {
	case NativeActualNotApplied, NativeActualPrepared, NativeActualApplying, NativeActualHealth, NativeActualApplied, NativeActualDegraded,
		NativeActualRollingBack, NativeActualRolledBack, NativeActualRollbackFailed, NativeActualReconcileRequired:
		return true
	default:
		return false
	}
}

func NativeFallbackStateNeedsReconciliation(value NativeFallbackActualState) bool {
	switch value {
	case NativeActualPrepared, NativeActualApplying, NativeActualHealth, NativeActualApplied, NativeActualDegraded, NativeActualRollingBack, NativeActualRollbackFailed:
		return true
	default:
		return false
	}
}
