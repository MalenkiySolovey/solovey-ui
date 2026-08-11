package nativefallback

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/netip"
	"sort"
	"strings"
	"time"

	neutralfallback "github.com/MalenkiySolovey/solovey-ui/componenthost/fallbacktargets"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
	"github.com/MalenkiySolovey/solovey-ui/service/coreinboundcontrol"
)

type Planner struct {
	Core       CoreReader
	Targets    TargetReader
	Management ManagementReader
	Now        func() time.Time
}

type PlannerError struct{ Code string }

func (err *PlannerError) Error() string {
	if err == nil || err.Code == "" {
		return "native fallback planning failed"
	}
	return err.Code
}

func (planner Planner) Plan(ctx context.Context, request PlanRequestV1) (domain.NativeFallbackPlanV1, error) {
	if err := ctx.Err(); err != nil {
		return domain.NativeFallbackPlanV1{}, &PlannerError{Code: "planner_cancelled"}
	}
	if planner.Core == nil || planner.Targets == nil || planner.Management == nil || request.InboundDatabaseID == 0 || request.TargetReference.Validate() != nil {
		return domain.NativeFallbackPlanV1{}, &PlannerError{Code: "planner_input_invalid"}
	}
	createdAt := time.Now().UTC().Truncate(time.Second)
	if planner.Now != nil {
		createdAt = planner.Now().UTC().Truncate(time.Second)
	}
	identity := planner.Core.Identity(ctx)
	if err := ctx.Err(); err != nil {
		return domain.NativeFallbackPlanV1{}, &PlannerError{Code: "planner_cancelled"}
	}
	snapshot, err := planner.Core.Snapshot(ctx, request.InboundDatabaseID)
	if err != nil {
		return domain.NativeFallbackPlanV1{}, &PlannerError{Code: "core_snapshot_unavailable"}
	}
	sourceRevision := SourceRevision(snapshot)
	resourceRevision := ResourceRevision(snapshot)
	gate, gateReason, err := requestedApplyGate(request.ApplyGate)
	if err != nil {
		return domain.NativeFallbackPlanV1{}, err
	}
	plan := domain.NativeFallbackPlanV1{
		Schema: domain.NativeFallbackPlanSchemaV1, CreatedAt: createdAt, ExpiresAt: createdAt.Add(domain.MaxNativeFallbackPlanLife),
		Resource: domain.NativeFallbackResourceBindingV1{
			ResourceID: snapshot.ResourceID, InboundDatabaseID: snapshot.InboundDatabaseID, InboundTag: snapshot.Tag, InboundType: snapshot.Type,
			SourceRevision: sourceRevision, ResourceRevision: resourceRevision, ConfigurationRevision: snapshot.ConfigurationRevision,
			EffectiveRevision: snapshot.Effective.Revision,
		},
		Runtime:             domain.NativeFallbackRuntimeBindingV1{IdentityRevision: identity.IdentityRevision, CapabilityResolverRevision: snapshot.CapabilityResolverRevision, AdmittedVariant: domain.NativeFallbackVariantNone},
		Target:              domain.NativeFallbackTargetBindingV1{Reference: request.TargetReference},
		ManagementIsolation: domain.NativeFallbackManagementBindingV1{State: "UNKNOWN"},
		ApplyGate:           gate, DesiredState: domain.NativeFallbackDesired, SelectedVariant: domain.NativeFallbackVariantNone, ActualState: domain.NativeActualNotApplied,
		Warnings: []domain.NativeFallbackReasonCode{gateReason},
	}

	if !runtimeIdentityExact(identity, snapshot) {
		if identity.State == coreinboundcontrol.RuntimeIdentityUnknown && !identityHasMismatch(identity) {
			block(&plan, domain.NativeReasonRuntimeIdentityUnknown)
		} else {
			block(&plan, domain.NativeReasonRuntimeIdentityMismatch)
		}
	}
	if request.ExpectedResourceID != snapshot.ResourceID || request.ExpectedSourceRevision != sourceRevision || request.ExpectedResourceRevision != resourceRevision || request.ExpectedConfigurationRevision != snapshot.ConfigurationRevision {
		block(&plan, domain.NativeReasonConfigurationStale)
	}
	if request.ExpectedEffectiveRevision != snapshot.Effective.Revision || !snapshot.Effective.RuntimeAvailable || !snapshot.Effective.Present || snapshot.Effective.Type != snapshot.Type || snapshot.Effective.Tag != snapshot.Tag {
		block(&plan, domain.NativeReasonEffectiveStateStale)
	}

	patchVariant, selected, replaceDefault, capabilityReason := admittedVariant(snapshot)
	if capabilityReason != "" {
		block(&plan, capabilityReason)
		if capabilityReason == domain.NativeReasonCapabilityUnsupported {
			plan.SelectedVariant = domain.NativeFallbackVariantUnsupported
		}
	} else {
		plan.Runtime.AdmittedVariant = selected
	}

	target, targetErr := planner.Targets.ResolveV2(ctx, request.TargetReference)
	if targetErr != nil {
		if strings.Contains(strings.ToLower(targetErr.Error()), "stale") {
			block(&plan, domain.NativeReasonTargetReferenceStale)
		} else {
			block(&plan, domain.NativeReasonTargetInvalid)
		}
	} else {
		planner.bindAndValidateTarget(&plan, snapshot, selected, target, createdAt)
	}

	var approved coreinboundcontrol.ApprovedEndpointV1
	if len(plan.Blocks) == 0 {
		approved = approvedEndpoint(target)
		management, managementErr := planner.Management.ResolveIsolation(ctx, snapshot.ResourceID, ManagementEndpointFactsV1{
			EndpointID: target.Endpoint.EndpointID, EndpointRevision: target.Endpoint.EndpointRevision, Network: string(target.Endpoint.Network),
			AddressFamily: string(target.Endpoint.AddressFamily), Address: target.Endpoint.Address, Port: target.Endpoint.Port,
			Local: target.Endpoint.Local, ManagementReachability: string(target.Endpoint.CanReachManagement),
		})
		if managementErr != nil {
			block(&plan, domain.NativeReasonManagementReachabilityUnknown)
		} else {
			plan.ManagementIsolation = domain.NativeFallbackManagementBindingV1{State: management.State, Revision: management.Revision, ExpiresAt: management.ExpiresAt}
			for _, reason := range management.ReasonCodes {
				switch reason {
				case string(domain.NativeReasonManagementTargetForbidden):
					plan.ManagementIsolation.ReasonCodes = append(plan.ManagementIsolation.ReasonCodes, domain.NativeReasonManagementTargetForbidden)
					block(&plan, domain.NativeReasonManagementTargetForbidden)
				default:
					plan.ManagementIsolation.ReasonCodes = append(plan.ManagementIsolation.ReasonCodes, domain.NativeReasonManagementReachabilityUnknown)
					block(&plan, domain.NativeReasonManagementReachabilityUnknown)
				}
			}
			if management.State != "ISOLATED" {
				if management.State == "FORBIDDEN" {
					block(&plan, domain.NativeReasonManagementTargetForbidden)
				} else {
					block(&plan, domain.NativeReasonManagementReachabilityUnknown)
				}
			}
			plan.ExpiresAt = earliest(plan.ExpiresAt, management.ExpiresAt)
		}
	}

	if len(plan.Blocks) == 0 {
		endpointDigest := ApprovedEndpointFactDigest(approved)
		currentSafe := CurrentSafeSubtreeDigest(snapshot, patchVariant, replaceDefault)
		candidateSafe := CandidateSafeSubtreeDigest(snapshot, patchVariant, replaceDefault, endpointDigest)
		preview, previewErr := planner.Core.PreviewFallbackPatch(ctx, coreinboundcontrol.PreviewFallbackPatchRequestV1{
			Expected: coreinboundcontrol.FallbackPatchExpectationsV1{
				InboundDatabaseID: snapshot.InboundDatabaseID, ResourceID: snapshot.ResourceID, ConfigurationRevision: snapshot.ConfigurationRevision,
				RuntimeIdentityRevision: identity.IdentityRevision, CapabilityResolverRevision: snapshot.CapabilityResolverRevision,
				EndpointRevision: target.Endpoint.EndpointRevision,
			},
			Variant: patchVariant, ApprovedEndpoint: approved, ReplaceDefaultToo: replaceDefault,
		})
		if previewErr != nil {
			if coreinboundcontrol.IsAdapterError(previewErr, coreinboundcontrol.ErrorStalePreview) || coreinboundcontrol.IsAdapterError(previewErr, coreinboundcontrol.ErrorStaleBeforeRevision) {
				block(&plan, domain.NativeReasonCorePreviewStale)
			} else {
				block(&plan, domain.NativeReasonCorePreviewBlocked)
			}
		} else if !previewMatches(preview, snapshot, target, patchVariant, createdAt) {
			block(&plan, domain.NativeReasonCorePreviewStale)
		} else {
			plan.CorePreview = domain.NativeFallbackCorePreviewBindingV1{
				Digest: preview.Digest, BeforeConfigurationRevision: preview.BeforeConfigurationRevision, ExpectedAfterRevision: preview.ExpectedAfterRevision,
				CurrentSafeSubtreeDigest: currentSafe, CandidateSafeSubtreeDigest: candidateSafe, ApprovedEndpointFactDigest: endpointDigest,
				ReplaceDefaultToo: replaceDefault, ExpiresAt: preview.ExpiresAt,
			}
			plan.ExpiresAt = earliest(plan.ExpiresAt, preview.ExpiresAt)
			plan.SelectedVariant = selected
		}
	}
	if len(plan.Blocks) != 0 && plan.SelectedVariant != domain.NativeFallbackVariantUnsupported {
		plan.SelectedVariant = domain.NativeFallbackVariantNone
	}
	if err := plan.Finalize(); err != nil {
		return domain.NativeFallbackPlanV1{}, &PlannerError{Code: "plan_contract_invalid"}
	}
	return plan, nil
}

func (planner Planner) bindAndValidateTarget(plan *domain.NativeFallbackPlanV1, snapshot coreinboundcontrol.InboundFallbackSnapshotV1, variant domain.NativeFallbackVariant, target neutralfallback.FallbackTargetV2, now time.Time) {
	plan.Target = targetBinding(plan.Target.Reference, target, snapshot.TLS.ServerNameDigest)
	plan.ExpiresAt = earliest(plan.ExpiresAt, plan.Target.HealthExpiresAt, plan.Target.CapacityExpiresAt)
	if target.Endpoint.Network != hostresources.NetworkTCP {
		block(plan, domain.NativeReasonTargetProtocolMismatch)
	}
	address, addressErr := netip.ParseAddr(target.Endpoint.Address)
	if addressErr != nil || !target.Endpoint.Local || !address.IsLoopback() {
		block(plan, domain.NativeReasonTargetNotLocal)
	}
	if target.Endpoint.ProxyProtocol != hostresources.CapabilityNo {
		block(plan, domain.NativeReasonTargetProtocolMismatch)
	}
	if target.Endpoint.CanReachManagement == hostresources.CapabilityUnknown {
		block(plan, domain.NativeReasonManagementReachabilityUnknown)
	} else if target.Endpoint.CanReachManagement != hostresources.CapabilityNo {
		block(plan, domain.NativeReasonManagementTargetForbidden)
	}
	if err := target.Validate(); err != nil {
		block(plan, domain.NativeReasonTargetInvalid)
	} else if currentReference, err := neutralfallback.ReferenceV2FromTarget(target); err != nil || currentReference != plan.Target.Reference {
		block(plan, domain.NativeReasonTargetReferenceStale)
	}
	validateHealthCapacity(plan, target, now)
	validateTargetProtocol(plan, snapshot, variant, target)
}

func validateHealthCapacity(plan *domain.NativeFallbackPlanV1, target neutralfallback.FallbackTargetV2, now time.Time) {
	switch neutralfallback.EffectiveReadinessV2(target.Health, now) {
	case neutralfallback.ReadinessReady:
	case neutralfallback.ReadinessStale:
		block(plan, domain.NativeReasonTargetHealthStale)
	case neutralfallback.ReadinessUnknown:
		block(plan, domain.NativeReasonTargetHealthUnknown)
	default:
		block(plan, domain.NativeReasonTargetNotReady)
	}
	if len(target.Health.ReasonCodes) != 0 {
		block(plan, domain.NativeReasonTargetNotReady)
	}
	switch neutralfallback.EffectiveCapacityStateV2(target.Capacity, now) {
	case neutralfallback.CapacityReady:
		if target.Capacity.ReservationSlotsTotal == 0 || target.Capacity.ReservationSlotsUsed >= target.Capacity.ReservationSlotsTotal {
			block(plan, domain.NativeReasonTargetCapacityExhausted)
		}
	case neutralfallback.CapacityPressured:
		block(plan, domain.NativeReasonTargetCapacityPressured)
	case neutralfallback.CapacityExhausted:
		block(plan, domain.NativeReasonTargetCapacityExhausted)
	case neutralfallback.CapacityStale:
		block(plan, domain.NativeReasonTargetCapacityStale)
	default:
		block(plan, domain.NativeReasonTargetCapacityUnknown)
	}
	if len(target.Capacity.ReasonCodes) != 0 {
		block(plan, domain.NativeReasonTargetCapacityUnknown)
	}
}

func validateTargetProtocol(plan *domain.NativeFallbackPlanV1, snapshot coreinboundcontrol.InboundFallbackSnapshotV1, variant domain.NativeFallbackVariant, target neutralfallback.FallbackTargetV2) {
	protocols := targetProtocols(target.Endpoint.ApplicationProtocols)
	switch variant {
	case domain.NativeFallbackVLESSRealityHandshakeTCP:
		if target.Endpoint.TransportSecurity != neutralfallback.TransportSecurityTLS {
			block(plan, domain.NativeReasonTargetTLSModeMismatch)
		}
		if snapshot.TLS.ServerNameDigest == "" || !containsString(plan.Target.AcceptedServerNameDigests, snapshot.TLS.ServerNameDigest) {
			block(plan, domain.NativeReasonTargetServerNameMismatch)
		}
		if !containsAllExact(protocols, snapshot.TLS.ALPN) {
			block(plan, domain.NativeReasonTargetALPNMismatch)
		}
	case domain.NativeFallbackTrojanDefaultTCP:
		if target.Endpoint.TransportSecurity != neutralfallback.TransportSecurityPlaintext {
			block(plan, domain.NativeReasonTargetTLSModeMismatch)
		}
		if !containsString(protocols, "http/1.1") {
			block(plan, domain.NativeReasonTargetProtocolMismatch)
		}
	case domain.NativeFallbackTrojanALPNTCP:
		if target.Endpoint.TransportSecurity != neutralfallback.TransportSecurityPlaintext {
			block(plan, domain.NativeReasonTargetTLSModeMismatch)
		}
		if !sameExactSet(protocols, snapshot.TLS.ALPN) {
			block(plan, domain.NativeReasonTargetALPNMismatch)
		}
	default:
		block(plan, domain.NativeReasonCapabilityUnknown)
	}
}

func admittedVariant(snapshot coreinboundcontrol.InboundFallbackSnapshotV1) (coreinboundcontrol.FallbackPatchVariantV1, domain.NativeFallbackVariant, bool, domain.NativeFallbackReasonCode) {
	capability := snapshot.Capability
	if capability.CapabilityResolverRevision != coreinboundcontrol.CapabilityResolverRevisionV1 || capability.Disposition == coreinboundcontrol.CapabilityUnknown {
		return "", domain.NativeFallbackVariantNone, false, domain.NativeReasonCapabilityUnknown
	}
	switch capability.Variant {
	case coreinboundcontrol.NativeFallbackVLESSRealityTCP:
		if capability.Disposition != coreinboundcontrol.CapabilitySupportedNaturalFallback {
			break
		}
		return coreinboundcontrol.FallbackPatchVLESSRealityHandshakeTCP, domain.NativeFallbackVLESSRealityHandshakeTCP, false, ""
	case coreinboundcontrol.NativeFallbackTrojanDefaultTCP:
		if capability.Disposition != coreinboundcontrol.CapabilitySupported {
			break
		}
		return coreinboundcontrol.FallbackPatchTrojanDefaultTCP, domain.NativeFallbackTrojanDefaultTCP, false, ""
	case coreinboundcontrol.NativeFallbackTrojanALPNTCP:
		if capability.Disposition != coreinboundcontrol.CapabilitySupported {
			break
		}
		return coreinboundcontrol.FallbackPatchTrojanALPNTCP, domain.NativeFallbackTrojanALPNTCP, false, ""
	case coreinboundcontrol.NativeFallbackTrojanDefaultALPNTCP:
		if capability.Disposition != coreinboundcontrol.CapabilitySupported {
			break
		}
		return coreinboundcontrol.FallbackPatchTrojanALPNTCP, domain.NativeFallbackTrojanALPNTCP, true, ""
	}
	return "", domain.NativeFallbackVariantUnsupported, false, domain.NativeReasonCapabilityUnsupported
}

func runtimeIdentityExact(identity coreinboundcontrol.CoreRuntimeIdentityV1, snapshot coreinboundcontrol.InboundFallbackSnapshotV1) bool {
	expectedProfile := coreinboundcontrol.BuildProfileWithoutUTLSRevision
	if identity.WithUTLS {
		expectedProfile = coreinboundcontrol.BuildProfileWithUTLSRevision
	}
	return identity.Schema == coreinboundcontrol.RuntimeIdentitySchemaV1 && identity.State == coreinboundcontrol.RuntimeIdentityVerified && len(identity.ReasonCodes) == 0 &&
		identity.SingBoxModule == coreinboundcontrol.PinnedSingBoxModule && identity.SingBoxVersion == coreinboundcontrol.PinnedSingBoxVersion && identity.SingBoxModuleSum == coreinboundcontrol.PinnedSingBoxModuleSum &&
		identity.SingBoxSourceRevision == coreinboundcontrol.PinnedSingBoxSourceRevision && identity.UTLSModule == coreinboundcontrol.PinnedUTLSModule && identity.UTLSVersion == coreinboundcontrol.PinnedUTLSVersion &&
		identity.UTLSModuleSum == coreinboundcontrol.PinnedUTLSModuleSum && identity.UTLSSourceRevision == coreinboundcontrol.PinnedUTLSSourceRevision && identity.BuildProfileRevision == expectedProfile && identity.IdentityRevision != "" &&
		identity.IdentityRevision == snapshot.RuntimeIdentityRevision && identity.CapabilityResolverRevision == coreinboundcontrol.CapabilityResolverRevisionV1 && snapshot.CapabilityResolverRevision == identity.CapabilityResolverRevision
}

func identityHasMismatch(identity coreinboundcontrol.CoreRuntimeIdentityV1) bool {
	for _, reason := range identity.ReasonCodes {
		value := string(reason)
		if strings.Contains(value, "mismatch") || strings.Contains(value, "replaced") || strings.Contains(value, "inconsistent") {
			return true
		}
	}
	return false
}

func requestedApplyGate(value string) (domain.NativeFallbackApplyGate, domain.NativeFallbackReasonCode, error) {
	switch domain.NativeFallbackApplyGate(strings.ToUpper(strings.TrimSpace(value))) {
	case "", domain.NativeApplyDisabledByDefault:
		return domain.NativeApplyDisabledByDefault, domain.NativeReasonApplyDisabled, nil
	case domain.NativeApplyExperimental:
		return domain.NativeApplyExperimental, domain.NativeReasonExperimentalOnly, nil
	default:
		return "", "", &PlannerError{Code: "apply_gate_invalid"}
	}
}

func targetBinding(reference neutralfallback.FallbackTargetReferenceV2, target neutralfallback.FallbackTargetV2, requiredServerNameDigest string) domain.NativeFallbackTargetBindingV1 {
	serverNames := make([]string, 0, len(target.Endpoint.AcceptedServerNames))
	for _, serverName := range target.Endpoint.AcceptedServerNames {
		serverNames = append(serverNames, serverNameDigest(serverName))
	}
	protocols := make([]string, 0, len(target.Endpoint.ApplicationProtocols))
	for _, protocol := range target.Endpoint.ApplicationProtocols {
		protocols = append(protocols, string(protocol))
	}
	return domain.NativeFallbackTargetBindingV1{
		Reference: reference, CanonicalTargetRevision: target.CanonicalTargetRevision, EndpointID: target.Endpoint.EndpointID, EndpointRevision: target.Endpoint.EndpointRevision,
		PublishRevision: target.Publish.Revision, ContentDigest: target.Publish.ContentDigest, ProviderRevision: target.ProviderRevision,
		HealthRevision: target.Health.Revision, HealthState: string(target.Health.Readiness), HealthExpiresAt: time.Unix(target.Health.ExpiresAt, 0).UTC(),
		CapacityRevision: target.Capacity.Revision, CapacityState: string(target.Capacity.State), CapacityExpiresAt: time.Unix(target.Capacity.ExpiresAt, 0).UTC(),
		ReservationSlotsTotal: target.Capacity.ReservationSlotsTotal, ReservationSlotsUsed: target.Capacity.ReservationSlotsUsed,
		Network: string(target.Endpoint.Network), AddressFamily: string(target.Endpoint.AddressFamily), Local: target.Endpoint.Local,
		TransportSecurity: string(target.Endpoint.TransportSecurity), ApplicationProtocols: protocols, AcceptedServerNameDigests: serverNames,
		RequiredServerNameDigest: requiredServerNameDigest, ProxyProtocol: string(target.Endpoint.ProxyProtocol), ManagementReachability: string(target.Endpoint.CanReachManagement),
	}
}

func approvedEndpoint(target neutralfallback.FallbackTargetV2) coreinboundcontrol.ApprovedEndpointV1 {
	return coreinboundcontrol.ApprovedEndpointV1{
		ProviderID: target.Identity.ProviderID, EndpointID: target.Endpoint.EndpointID, EndpointRevision: target.Endpoint.EndpointRevision,
		Network: string(target.Endpoint.Network), AddressFamily: string(target.Endpoint.AddressFamily), Bind: target.Endpoint.Address, Port: target.Endpoint.Port,
		Local: target.Endpoint.Local, TransportSecurity: coreTransportSecurity(target.Endpoint.TransportSecurity), ApplicationProtocols: targetProtocols(target.Endpoint.ApplicationProtocols),
	}
}

func coreTransportSecurity(value neutralfallback.TransportSecurity) string {
	if value == neutralfallback.TransportSecurityPlaintext {
		return "none"
	}
	return strings.ToLower(string(value))
}

func targetProtocols(values []neutralfallback.ApplicationProtocol) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		switch value {
		case neutralfallback.ApplicationProtocolHTTP11:
			result = append(result, "http/1.1")
		case neutralfallback.ApplicationProtocolHTTP2:
			result = append(result, "h2")
		default:
			result = append(result, "UNKNOWN")
		}
	}
	sort.Strings(result)
	return result
}

func previewMatches(preview coreinboundcontrol.FallbackPatchPreviewV1, snapshot coreinboundcontrol.InboundFallbackSnapshotV1, target neutralfallback.FallbackTargetV2, variant coreinboundcontrol.FallbackPatchVariantV1, now time.Time) bool {
	return preview.Schema == coreinboundcontrol.FallbackPatchPreviewSchemaV1 && preview.Digest != "" && preview.PreviewID == preview.Digest &&
		preview.InboundDatabaseID == snapshot.InboundDatabaseID && preview.ResourceID == snapshot.ResourceID && preview.Variant == variant &&
		preview.BeforeConfigurationRevision == snapshot.ConfigurationRevision && preview.ExpectedAfterRevision != "" &&
		preview.RuntimeIdentityRevision == snapshot.RuntimeIdentityRevision && preview.CapabilityResolverRevision == snapshot.CapabilityResolverRevision &&
		preview.EndpointProviderID == target.Identity.ProviderID && preview.EndpointID == target.Endpoint.EndpointID && preview.EndpointRevision == target.Endpoint.EndpointRevision && preview.ExpiresAt.After(now)
}

func SourceRevision(snapshot coreinboundcontrol.InboundFallbackSnapshotV1) string {
	return digest(struct {
		Schema, InboundOptionsDigest, TLSOptionsDigest string
		TLSRecordDatabaseID                            uint
	}{"solovey-ui/native-fallback-source-binding/v1", snapshot.InboundOptionsDigest, snapshot.TLSOptionsDigest, snapshot.TLSRecordDatabaseID})
}

func ResourceRevision(snapshot coreinboundcontrol.InboundFallbackSnapshotV1) string {
	return digest(struct {
		Schema, ResourceID, Tag, Type string
		InboundDatabaseID             uint
		Listener                      coreinboundcontrol.ListenerShapeV1
	}{"solovey-ui/native-fallback-resource-binding/v1", snapshot.ResourceID, snapshot.Tag, snapshot.Type, snapshot.InboundDatabaseID, snapshot.Listener})
}

func ApprovedEndpointFactDigest(endpoint coreinboundcontrol.ApprovedEndpointV1) string {
	protocols := append([]string(nil), endpoint.ApplicationProtocols...)
	sort.Strings(protocols)
	endpoint.ApplicationProtocols = protocols
	return digest(struct {
		Schema   string
		Endpoint coreinboundcontrol.ApprovedEndpointV1
	}{"solovey-ui/native-fallback-approved-endpoint/v1", endpoint})
}

func CurrentSafeSubtreeDigest(snapshot coreinboundcontrol.InboundFallbackSnapshotV1, variant coreinboundcontrol.FallbackPatchVariantV1, replaceDefault bool) string {
	value := struct {
		Schema         string
		Variant        coreinboundcontrol.FallbackPatchVariantV1
		Reality        coreinboundcontrol.TargetShapeV1
		Default        coreinboundcontrol.TargetShapeV1
		ALPN           []coreinboundcontrol.ALPNFallbackShapeV1
		ReplaceDefault bool
	}{"solovey-ui/native-fallback-safe-subtree/v1", variant, snapshot.TLS.Reality.Handshake, snapshot.DefaultFallback, append([]coreinboundcontrol.ALPNFallbackShapeV1(nil), snapshot.ALPNFallbacks...), replaceDefault}
	return digest(value)
}

func CandidateSafeSubtreeDigest(snapshot coreinboundcontrol.InboundFallbackSnapshotV1, variant coreinboundcontrol.FallbackPatchVariantV1, replaceDefault bool, endpointDigest string) string {
	alpn := append([]string(nil), snapshot.TLS.ALPN...)
	sort.Strings(alpn)
	return digest(struct {
		Schema, Variant, EndpointDigest string
		ALPN                            []string
		ReplaceDefault                  bool
	}{"solovey-ui/native-fallback-candidate-safe-subtree/v1", string(variant), endpointDigest, alpn, replaceDefault})
}

func serverNameDigest(value string) string {
	return digest(struct{ Schema, ServerName string }{"solovey-ui/inbound-tls-server-name/v1", strings.ToLower(value)})
}

func digest(value any) string {
	payload, _ := json.Marshal(value)
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func block(plan *domain.NativeFallbackPlanV1, reason domain.NativeFallbackReasonCode) {
	plan.Blocks = append(plan.Blocks, reason)
}

func earliest(values ...time.Time) time.Time {
	var result time.Time
	for _, value := range values {
		if value.IsZero() {
			continue
		}
		if result.IsZero() || value.Before(result) {
			result = value
		}
	}
	return result
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func containsAllExact(values, required []string) bool {
	for _, value := range required {
		if !containsString(values, value) {
			return false
		}
	}
	return true
}

func sameExactSet(left, right []string) bool {
	left = append([]string(nil), left...)
	right = append([]string(nil), right...)
	sort.Strings(left)
	sort.Strings(right)
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
