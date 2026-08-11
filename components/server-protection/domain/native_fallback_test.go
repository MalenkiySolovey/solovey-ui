package domain

import (
	"strings"
	"testing"
	"time"

	neutralfallback "github.com/MalenkiySolovey/solovey-ui/componenthost/fallbacktargets"
)

func TestNativeFallbackPlanDigestBindsEverySafetyRevisionAndOrdering(t *testing.T) {
	base := nativePlanFixture()
	if err := base.Finalize(); err != nil {
		t.Fatal(err)
	}
	mutations := []func(*NativeFallbackPlanV1){
		func(value *NativeFallbackPlanV1) { value.Resource.SourceRevision = strings.Repeat("1", 64) },
		func(value *NativeFallbackPlanV1) { value.Resource.ResourceRevision = strings.Repeat("2", 64) },
		func(value *NativeFallbackPlanV1) {
			value.Resource.ConfigurationRevision = strings.Repeat("3", 64)
			value.CorePreview.BeforeConfigurationRevision = value.Resource.ConfigurationRevision
		},
		func(value *NativeFallbackPlanV1) { value.Resource.EffectiveRevision = strings.Repeat("4", 64) },
		func(value *NativeFallbackPlanV1) { value.Runtime.IdentityRevision = strings.Repeat("5", 64) },
		func(value *NativeFallbackPlanV1) { value.Runtime.CapabilityResolverRevision = strings.Repeat("6", 64) },
		func(value *NativeFallbackPlanV1) { value.Target.CanonicalTargetRevision = strings.Repeat("7", 64) },
		func(value *NativeFallbackPlanV1) {
			value.Target.EndpointRevision = strings.Repeat("0", 64)
			value.Target.Reference.EndpointRevision = value.Target.EndpointRevision
		},
		func(value *NativeFallbackPlanV1) {
			value.Target.PublishRevision = "publish-two"
			value.Target.Reference.PublishRevision = value.Target.PublishRevision
		},
		func(value *NativeFallbackPlanV1) {
			value.Target.ContentDigest = strings.Repeat("0", 64)
			value.Target.Reference.ContentDigest = value.Target.ContentDigest
		},
		func(value *NativeFallbackPlanV1) {
			value.Target.ProviderRevision = "provider-two"
			value.Target.Reference.ProviderRevision = value.Target.ProviderRevision
		},
		func(value *NativeFallbackPlanV1) {
			value.Target.HealthRevision = strings.Repeat("8", 64)
			value.Target.Reference.ProviderHealthRevision = value.Target.HealthRevision
		},
		func(value *NativeFallbackPlanV1) {
			value.Target.CapacityRevision = strings.Repeat("9", 64)
			value.Target.Reference.CapacityRevision = value.Target.CapacityRevision
		},
		func(value *NativeFallbackPlanV1) { value.ManagementIsolation.Revision = strings.Repeat("a", 64) },
		func(value *NativeFallbackPlanV1) { value.CorePreview.Digest = strings.Repeat("b", 64) },
		func(value *NativeFallbackPlanV1) {
			value.CorePreview.ExpectedAfterRevision = strings.Repeat("0", 64)
		},
		func(value *NativeFallbackPlanV1) {
			value.CorePreview.CurrentSafeSubtreeDigest = strings.Repeat("c", 64)
		},
		func(value *NativeFallbackPlanV1) {
			value.CorePreview.CandidateSafeSubtreeDigest = strings.Repeat("d", 64)
		},
		func(value *NativeFallbackPlanV1) {
			value.CorePreview.ApprovedEndpointFactDigest = strings.Repeat("e", 64)
		},
	}
	for index, mutate := range mutations {
		changed := base
		mutate(&changed)
		if err := changed.Finalize(); err != nil {
			t.Fatalf("mutation %d: %v", index, err)
		}
		if changed.PlanDigest == base.PlanDigest {
			t.Fatalf("safety mutation %d did not alter plan digest", index)
		}
	}

	reordered := base
	reordered.Target.ApplicationProtocols = []string{"HTTP_2", "HTTP_1_1", "HTTP_2"}
	reordered.Blocks = []NativeFallbackReasonCode{NativeReasonTargetNotReady, NativeReasonTargetInvalid, NativeReasonTargetNotReady}
	reordered.Eligible = false
	if err := reordered.Finalize(); err != nil {
		t.Fatal(err)
	}
	second := reordered
	second.Target.ApplicationProtocols = []string{"HTTP_1_1", "HTTP_2"}
	second.Blocks = []NativeFallbackReasonCode{NativeReasonTargetInvalid, NativeReasonTargetNotReady}
	if err := second.Finalize(); err != nil {
		t.Fatal(err)
	}
	if reordered.PlanDigest != second.PlanDigest {
		t.Fatalf("canonical ordering changed digest: %s != %s", reordered.PlanDigest, second.PlanDigest)
	}
}

func TestNativeFallbackPlanCannotClaimAppliedOrStable(t *testing.T) {
	plan := nativePlanFixture()
	plan.ActualState = NativeActualApplied
	if err := plan.Finalize(); err == nil {
		t.Fatal("plan claimed APPLIED")
	}
	plan = nativePlanFixture()
	plan.ApplyGate = NativeApplyStable
	if err := plan.Finalize(); err == nil {
		t.Fatal("plan claimed STABLE")
	}
}

func TestNativeFallbackPlanRequiresCompleteBindingsAndCannotOutliveFacts(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*NativeFallbackPlanV1)
	}{
		{"effective revision absent", func(plan *NativeFallbackPlanV1) { plan.Resource.EffectiveRevision = "" }},
		{"runtime revision absent", func(plan *NativeFallbackPlanV1) { plan.Runtime.IdentityRevision = "" }},
		{"health binding absent", func(plan *NativeFallbackPlanV1) { plan.Target.HealthRevision = "" }},
		{"target reference inconsistent", func(plan *NativeFallbackPlanV1) { plan.Target.HealthRevision = strings.Repeat("0", 64) }},
		{"core binding absent", func(plan *NativeFallbackPlanV1) { plan.CorePreview.ExpectedAfterRevision = "" }},
		{"health expiry exceeded", func(plan *NativeFallbackPlanV1) { plan.ExpiresAt = plan.Target.HealthExpiresAt.Add(time.Second) }},
		{"capacity expiry exceeded", func(plan *NativeFallbackPlanV1) { plan.ExpiresAt = plan.Target.CapacityExpiresAt.Add(time.Second) }},
		{"management expiry exceeded", func(plan *NativeFallbackPlanV1) { plan.ExpiresAt = plan.ManagementIsolation.ExpiresAt.Add(time.Second) }},
		{"preview expiry exceeded", func(plan *NativeFallbackPlanV1) { plan.ExpiresAt = plan.CorePreview.ExpiresAt.Add(time.Second) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan := nativePlanFixture()
			test.mutate(&plan)
			if err := plan.Finalize(); err == nil {
				t.Fatal("incomplete or overlong eligible plan was accepted")
			}
		})
	}
}

func TestNativeFallbackStateReasonBoundsAreEnforced(t *testing.T) {
	now := time.Unix(1_900_000_000, 0).UTC()
	state := NativeFallbackStateV1{Schema: NativeFallbackStateSchemaV1, ResourceID: "core:inbound:17", InboundDatabaseID: 17, DesiredState: NativeFallbackDesired, SelectedVariant: NativeFallbackVariantNone, ActualState: NativeActualNotApplied, CreatedAt: now, UpdatedAt: now}
	for index := 0; index < 33; index++ {
		state.ReasonCodes = append(state.ReasonCodes, NativeFallbackReasonCode("reason_"+strings.Repeat("x", index%10)+string(rune('a'+index%26))))
	}
	if err := state.ValidateStored(); err == nil {
		t.Fatal("unbounded state reasons were accepted")
	}
}

func nativePlanFixture() NativeFallbackPlanV1 {
	now := time.Unix(1_900_000_000, 0).UTC()
	return NativeFallbackPlanV1{
		Schema: NativeFallbackPlanSchemaV1, CreatedAt: now, ExpiresAt: now.Add(time.Minute),
		Resource: NativeFallbackResourceBindingV1{ResourceID: "core:inbound:17", InboundDatabaseID: 17, InboundTag: "vless-main", InboundType: "vless", SourceRevision: strings.Repeat("a", 64), ResourceRevision: strings.Repeat("b", 64), ConfigurationRevision: strings.Repeat("c", 64), EffectiveRevision: strings.Repeat("d", 64)},
		Runtime:  NativeFallbackRuntimeBindingV1{IdentityRevision: strings.Repeat("e", 64), CapabilityResolverRevision: strings.Repeat("f", 64), AdmittedVariant: NativeFallbackVLESSRealityHandshakeTCP},
		Target: NativeFallbackTargetBindingV1{
			Reference:               neutralfallback.FallbackTargetReferenceV2{Schema: neutralfallback.TargetReferenceSchemaV2, ProviderID: "fixture-provider", TargetID: "site-one", PublishRevision: "publish-one", ContentDigest: strings.Repeat("1", 64), EndpointID: "endpoint-one", EndpointRevision: strings.Repeat("2", 64), ProviderHealthRevision: strings.Repeat("3", 64), CapacityRevision: strings.Repeat("4", 64), ProviderRevision: "provider-one"},
			CanonicalTargetRevision: strings.Repeat("5", 64), EndpointID: "endpoint-one", EndpointRevision: strings.Repeat("2", 64), PublishRevision: "publish-one", ContentDigest: strings.Repeat("1", 64), ProviderRevision: "provider-one", HealthRevision: strings.Repeat("3", 64), HealthState: "READY", HealthExpiresAt: now.Add(2 * time.Minute), CapacityRevision: strings.Repeat("4", 64), CapacityState: "READY", CapacityExpiresAt: now.Add(2 * time.Minute), ReservationSlotsTotal: 4, ReservationSlotsUsed: 1, Network: "tcp", AddressFamily: "ipv4", Local: true, TransportSecurity: "TLS", ApplicationProtocols: []string{"HTTP_1_1", "HTTP_2"}, AcceptedServerNameDigests: []string{strings.Repeat("6", 64)}, RequiredServerNameDigest: strings.Repeat("6", 64), ProxyProtocol: "no", ManagementReachability: "no",
		},
		CorePreview:         NativeFallbackCorePreviewBindingV1{Digest: strings.Repeat("7", 64), BeforeConfigurationRevision: strings.Repeat("c", 64), ExpectedAfterRevision: strings.Repeat("8", 64), CurrentSafeSubtreeDigest: strings.Repeat("9", 64), CandidateSafeSubtreeDigest: strings.Repeat("a", 64), ApprovedEndpointFactDigest: strings.Repeat("b", 64), ExpiresAt: now.Add(time.Minute)},
		ManagementIsolation: NativeFallbackManagementBindingV1{State: "ISOLATED", Revision: strings.Repeat("c", 64), ExpiresAt: now.Add(time.Minute)},
		ApplyGate:           NativeApplyDisabledByDefault, DesiredState: NativeFallbackDesired, SelectedVariant: NativeFallbackVLESSRealityHandshakeTCP, ActualState: NativeActualNotApplied, Eligible: true, Warnings: []NativeFallbackReasonCode{NativeReasonApplyDisabled},
	}
}
