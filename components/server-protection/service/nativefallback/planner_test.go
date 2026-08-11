package nativefallback

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	neutralfallback "github.com/MalenkiySolovey/solovey-ui/componenthost/fallbacktargets"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
	"github.com/MalenkiySolovey/solovey-ui/service/coreinboundcontrol"
)

type fakeCoreReader struct {
	identity     coreinboundcontrol.CoreRuntimeIdentityV1
	snapshot     coreinboundcontrol.InboundFallbackSnapshotV1
	previewError error
	previewCalls int
	previewAt    time.Time
}

func (reader *fakeCoreReader) Identity(context.Context) coreinboundcontrol.CoreRuntimeIdentityV1 {
	return reader.identity
}
func (reader *fakeCoreReader) Snapshot(context.Context, uint) (coreinboundcontrol.InboundFallbackSnapshotV1, error) {
	return reader.snapshot, nil
}
func (reader *fakeCoreReader) PreviewFallbackPatch(_ context.Context, request coreinboundcontrol.PreviewFallbackPatchRequestV1) (coreinboundcontrol.FallbackPatchPreviewV1, error) {
	reader.previewCalls++
	if reader.previewError != nil {
		return coreinboundcontrol.FallbackPatchPreviewV1{}, reader.previewError
	}
	return coreinboundcontrol.FallbackPatchPreviewV1{
		Schema: coreinboundcontrol.FallbackPatchPreviewSchemaV1, PreviewID: strings.Repeat("9", 64), Digest: strings.Repeat("9", 64),
		InboundDatabaseID: reader.snapshot.InboundDatabaseID, ResourceID: reader.snapshot.ResourceID, Variant: request.Variant,
		BeforeConfigurationRevision: reader.snapshot.ConfigurationRevision, ExpectedAfterRevision: strings.Repeat("8", 64),
		RuntimeIdentityRevision: reader.snapshot.RuntimeIdentityRevision, CapabilityResolverRevision: reader.snapshot.CapabilityResolverRevision,
		EndpointProviderID: request.ApprovedEndpoint.ProviderID, EndpointID: request.ApprovedEndpoint.EndpointID,
		EndpointRevision: request.ApprovedEndpoint.EndpointRevision, ExpiresAt: reader.previewAt,
	}, nil
}

type fakeTargetReader struct {
	target neutralfallback.FallbackTargetV2
	err    error
	calls  int
	seen   neutralfallback.FallbackTargetReferenceV2
}

func (reader *fakeTargetReader) ResolveV2(_ context.Context, reference neutralfallback.FallbackTargetReferenceV2) (neutralfallback.FallbackTargetV2, error) {
	reader.calls++
	reader.seen = reference
	return reader.target, reader.err
}

type fakeManagementReader struct {
	result ManagementIsolationResultV1
	err    error
	calls  int
}

type inventoryProvider struct {
	targets       []neutralfallback.FallbackTargetV2
	mutationCalls *int
}

func (provider *inventoryProvider) ProviderID() string { return "fixture-provider" }
func (provider *inventoryProvider) InventoryV2(context.Context, neutralfallback.InventoryV2Request) (neutralfallback.InventoryV2Result, *neutralfallback.ProviderContractError) {
	return neutralfallback.InventoryV2Result{Targets: provider.targets}, nil
}
func (*inventoryProvider) ResolveV2(context.Context, neutralfallback.FallbackTargetReferenceV2) (neutralfallback.ResolveV2Result, *neutralfallback.ProviderContractError) {
	return neutralfallback.ResolveV2Result{}, unsupportedProviderRead()
}

func (provider *inventoryProvider) Reserve(context.Context, neutralfallback.ReserveRequestV1) (neutralfallback.ReservationResultV1, *neutralfallback.ProviderContractError) {
	provider.mutationCalled()
	return neutralfallback.ReservationResultV1{}, unsupportedProviderRead()
}
func (provider *inventoryProvider) FenceForMutation(context.Context, neutralfallback.ReservationMutationRequestV1) (neutralfallback.ReservationResultV1, *neutralfallback.ProviderContractError) {
	provider.mutationCalled()
	return neutralfallback.ReservationResultV1{}, unsupportedProviderRead()
}
func (provider *inventoryProvider) Activate(context.Context, neutralfallback.ReservationMutationRequestV1) (neutralfallback.ReservationResultV1, *neutralfallback.ProviderContractError) {
	provider.mutationCalled()
	return neutralfallback.ReservationResultV1{}, unsupportedProviderRead()
}
func (provider *inventoryProvider) Renew(context.Context, neutralfallback.ReservationMutationRequestV1) (neutralfallback.ReservationResultV1, *neutralfallback.ProviderContractError) {
	provider.mutationCalled()
	return neutralfallback.ReservationResultV1{}, unsupportedProviderRead()
}
func (provider *inventoryProvider) Release(context.Context, neutralfallback.ReleaseReservationRequestV1) (neutralfallback.ReservationResultV1, *neutralfallback.ProviderContractError) {
	provider.mutationCalled()
	return neutralfallback.ReservationResultV1{}, unsupportedProviderRead()
}
func (*inventoryProvider) GetReservation(context.Context, neutralfallback.GetReservationRequestV1) (neutralfallback.ReservationResultV1, *neutralfallback.ProviderContractError) {
	return neutralfallback.ReservationResultV1{}, unsupportedProviderRead()
}
func (*inventoryProvider) ListReservations(context.Context, neutralfallback.ListReservationsQueryV1) (neutralfallback.ListReservationsResultV1, *neutralfallback.ProviderContractError) {
	return neutralfallback.ListReservationsResultV1{}, unsupportedProviderRead()
}

func (provider *inventoryProvider) mutationCalled() {
	if provider.mutationCalls != nil {
		(*provider.mutationCalls)++
	}
}

func unsupportedProviderRead() *neutralfallback.ProviderContractError {
	return &neutralfallback.ProviderContractError{Class: neutralfallback.ProviderErrorUnavailable, ReasonCode: "not_supported"}
}

func (reader *fakeManagementReader) ResolveIsolation(context.Context, string, ManagementEndpointFactsV1) (ManagementIsolationResultV1, error) {
	reader.calls++
	return reader.result, reader.err
}

type plannerFixture struct {
	now        time.Time
	core       *fakeCoreReader
	targets    *fakeTargetReader
	management *fakeManagementReader
	planner    Planner
	request    PlanRequestV1
}

func newPlannerFixture(t *testing.T) plannerFixture {
	t.Helper()
	now := time.Unix(1_900_000_000, 0).UTC()
	identity := exactRuntimeIdentity()
	snapshot := vlessSnapshot(identity)
	target := targetFixture(t, now, neutralfallback.TransportSecurityTLS, []neutralfallback.ApplicationProtocol{neutralfallback.ApplicationProtocolHTTP11, neutralfallback.ApplicationProtocolHTTP2}, []string{"decoy.example"})
	reference, err := neutralfallback.ReferenceV2FromTarget(target)
	if err != nil {
		t.Fatal(err)
	}
	core := &fakeCoreReader{identity: identity, snapshot: snapshot, previewAt: now.Add(2 * time.Minute)}
	targets := &fakeTargetReader{target: target}
	management := &fakeManagementReader{result: ManagementIsolationResultV1{State: "ISOLATED", Revision: strings.Repeat("7", 64), ExpiresAt: now.Add(90 * time.Second)}}
	request := requestFor(snapshot, reference)
	planner := Planner{Core: core, Targets: targets, Management: management, Now: func() time.Time { return now }}
	return plannerFixture{now: now, core: core, targets: targets, management: management, planner: planner, request: request}
}

func TestPlannerProducesDeterministicReadOnlyBoundPlan(t *testing.T) {
	fixture := newPlannerFixture(t)
	first, err := fixture.planner.Plan(t.Context(), fixture.request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := fixture.planner.Plan(t.Context(), fixture.request)
	if err != nil {
		t.Fatal(err)
	}
	if first.PlanDigest != second.PlanDigest || first.PlanID != first.PlanDigest || !first.Eligible || first.ActualState != domain.NativeActualNotApplied {
		t.Fatalf("deterministic eligible plan mismatch: first=%#v second=%#v", first, second)
	}
	if first.ExpiresAt != fixture.now.Add(90*time.Second) || first.CorePreview.ApprovedEndpointFactDigest == "" || first.CorePreview.CurrentSafeSubtreeDigest == "" || first.CorePreview.CandidateSafeSubtreeDigest == "" {
		t.Fatalf("expiry or safety bindings are incomplete: %#v", first)
	}
	if fixture.core.previewCalls != 2 || fixture.targets.calls != 2 || fixture.management.calls != 2 || fixture.targets.seen != fixture.request.TargetReference {
		t.Fatalf("unexpected read counts core=%d targets=%d management=%d", fixture.core.previewCalls, fixture.targets.calls, fixture.management.calls)
	}
	payload, _ := json.Marshal(first)
	for _, forbidden := range []string{"decoy.example", "password", "private_key", "C:\\", "/home/", "initial-admin", `"actualState":"APPLIED"`} {
		if strings.Contains(string(payload), forbidden) {
			t.Fatalf("plan leaked forbidden content %q: %s", forbidden, payload)
		}
	}
}

func TestRegistryTargetReaderUsesOnlyExactCallerReference(t *testing.T) {
	now := time.Unix(1_900_000_000, 0).UTC()
	exact := targetFixture(t, now, neutralfallback.TransportSecurityTLS, []neutralfallback.ApplicationProtocol{neutralfallback.ApplicationProtocolHTTP11}, []string{"decoy.example"})
	other := exact
	other.Identity.TargetID = "site-newer"
	other.Publish.Revision = "publish-newer"
	other.Endpoint.EndpointID = "endpoint-newer"
	var err error
	other, err = neutralfallback.FinalizeFallbackTargetV2(other)
	if err != nil {
		t.Fatal(err)
	}
	registry := neutralfallback.NewRegistry()
	mutationCalls := 0
	unregister, err := registry.RegisterV2(&inventoryProvider{targets: []neutralfallback.FallbackTargetV2{other, exact}, mutationCalls: &mutationCalls})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(unregister)
	reference, _ := neutralfallback.ReferenceV2FromTarget(exact)
	resolved, err := (RegistryTargetReader{Registry: registry, Now: func() time.Time { return now }}).ResolveV2(t.Context(), reference)
	if err != nil || resolved.Identity.TargetID != exact.Identity.TargetID {
		t.Fatalf("exact target was replaced by inventory alternative: %#v err=%v", resolved.Identity, err)
	}
	reference.PublishRevision = "publish-stale"
	if _, err := (RegistryTargetReader{Registry: registry, Now: func() time.Time { return now }}).ResolveV2(t.Context(), reference); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("stale exact reference was accepted: %v", err)
	}
	if mutationCalls != 0 {
		t.Fatalf("read-only exact resolution invoked %d provider mutations", mutationCalls)
	}
}

func TestPlannerRuntimeConfigurationAndCorePreviewFailClosed(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*plannerFixture)
		reason domain.NativeFallbackReasonCode
	}{
		{"runtime unknown", func(f *plannerFixture) { f.core.identity.State = coreinboundcontrol.RuntimeIdentityUnknown }, domain.NativeReasonRuntimeIdentityUnknown},
		{"runtime replaced", func(f *plannerFixture) {
			f.core.identity.State = coreinboundcontrol.RuntimeIdentityUnknown
			f.core.identity.ReasonCodes = []coreinboundcontrol.ReasonCode{coreinboundcontrol.ReasonSingBoxModuleReplaced}
		}, domain.NativeReasonRuntimeIdentityMismatch},
		{"configuration stale", func(f *plannerFixture) { f.request.ExpectedConfigurationRevision = strings.Repeat("1", 64) }, domain.NativeReasonConfigurationStale},
		{"effective stale", func(f *plannerFixture) { f.request.ExpectedEffectiveRevision = strings.Repeat("2", 64) }, domain.NativeReasonEffectiveStateStale},
		{"capability unsupported", func(f *plannerFixture) {
			f.core.snapshot.Capability.Disposition = coreinboundcontrol.CapabilityUnsupported
			f.core.snapshot.Capability.Variant = coreinboundcontrol.NativeFallbackNone
		}, domain.NativeReasonCapabilityUnsupported},
		{"capability unknown", func(f *plannerFixture) { f.core.snapshot.Capability.Disposition = coreinboundcontrol.CapabilityUnknown }, domain.NativeReasonCapabilityUnknown},
		{"core preview blocked", func(f *plannerFixture) {
			f.core.previewError = &coreinboundcontrol.AdapterError{Code: coreinboundcontrol.ErrorSharedTLS}
		}, domain.NativeReasonCorePreviewBlocked},
		{"core preview stale", func(f *plannerFixture) {
			f.core.previewError = &coreinboundcontrol.AdapterError{Code: coreinboundcontrol.ErrorStalePreview}
		}, domain.NativeReasonCorePreviewStale},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newPlannerFixture(t)
			test.mutate(&fixture)
			plan, err := fixture.planner.Plan(t.Context(), fixture.request)
			if err != nil {
				t.Fatal(err)
			}
			if plan.Eligible || !slices.Contains(plan.Blocks, test.reason) || plan.ActualState != domain.NativeActualNotApplied {
				t.Fatalf("reason %s not represented: %#v", test.reason, plan)
			}
			if test.reason != domain.NativeReasonCorePreviewBlocked && test.reason != domain.NativeReasonCorePreviewStale && fixture.core.previewCalls != 0 {
				t.Fatalf("blocked input reached core preview: %d", fixture.core.previewCalls)
			}
		})
	}
}

func TestPlannerExactTargetAndProtocolMatrix(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, *plannerFixture)
		reason domain.NativeFallbackReasonCode
	}{
		{"stale publish", func(t *testing.T, f *plannerFixture) { f.request.TargetReference.PublishRevision = "publish-other" }, domain.NativeReasonTargetReferenceStale},
		{"stale endpoint", func(t *testing.T, f *plannerFixture) {
			f.request.TargetReference.EndpointRevision = strings.Repeat("1", 64)
		}, domain.NativeReasonTargetReferenceStale},
		{"stale health", func(t *testing.T, f *plannerFixture) {
			f.request.TargetReference.ProviderHealthRevision = strings.Repeat("1", 64)
		}, domain.NativeReasonTargetReferenceStale},
		{"stale capacity", func(t *testing.T, f *plannerFixture) {
			f.request.TargetReference.CapacityRevision = strings.Repeat("1", 64)
		}, domain.NativeReasonTargetReferenceStale},
		{"stale provider", func(t *testing.T, f *plannerFixture) { f.request.TargetReference.ProviderRevision = "provider-other" }, domain.NativeReasonTargetReferenceStale},
		{"plaintext reality target", func(t *testing.T, f *plannerFixture) {
			replaceTarget(t, f, neutralfallback.TransportSecurityPlaintext, []neutralfallback.ApplicationProtocol{neutralfallback.ApplicationProtocolHTTP11}, []string{"decoy.example"})
		}, domain.NativeReasonTargetTLSModeMismatch},
		{"server name mismatch", func(t *testing.T, f *plannerFixture) {
			replaceTarget(t, f, neutralfallback.TransportSecurityTLS, []neutralfallback.ApplicationProtocol{neutralfallback.ApplicationProtocolHTTP11, neutralfallback.ApplicationProtocolHTTP2}, []string{"other.example"})
		}, domain.NativeReasonTargetServerNameMismatch},
		{"alpn mismatch", func(t *testing.T, f *plannerFixture) {
			replaceTarget(t, f, neutralfallback.TransportSecurityTLS, []neutralfallback.ApplicationProtocol{neutralfallback.ApplicationProtocolHTTP11}, []string{"decoy.example"})
		}, domain.NativeReasonTargetALPNMismatch},
		{"non local", func(t *testing.T, f *plannerFixture) {
			f.targets.target.Endpoint.Address = "192.0.2.1"
			f.targets.target.Endpoint.Local = false
		}, domain.NativeReasonTargetNotLocal},
		{"proxy protocol", func(t *testing.T, f *plannerFixture) {
			f.targets.target.Endpoint.ProxyProtocol = hostresources.CapabilityYes
		}, domain.NativeReasonTargetProtocolMismatch},
		{"management yes", func(t *testing.T, f *plannerFixture) {
			f.targets.target.Endpoint.CanReachManagement = hostresources.CapabilityYes
		}, domain.NativeReasonManagementTargetForbidden},
		{"management unknown", func(t *testing.T, f *plannerFixture) {
			f.targets.target.Endpoint.CanReachManagement = hostresources.CapabilityUnknown
		}, domain.NativeReasonManagementReachabilityUnknown},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newPlannerFixture(t)
			test.mutate(t, &fixture)
			plan, err := fixture.planner.Plan(t.Context(), fixture.request)
			if err != nil {
				t.Fatal(err)
			}
			if plan.Eligible || !slices.Contains(plan.Blocks, test.reason) || fixture.core.previewCalls != 0 {
				t.Fatalf("target failure %s not closed before preview: %#v calls=%d", test.reason, plan, fixture.core.previewCalls)
			}
		})
	}
}

func TestPlannerTrojanDefaultAndExactALPNCells(t *testing.T) {
	fixture := newPlannerFixture(t)
	fixture.core.snapshot = trojanSnapshot(fixture.core.identity, false)
	replaceTarget(t, &fixture, neutralfallback.TransportSecurityPlaintext, []neutralfallback.ApplicationProtocol{neutralfallback.ApplicationProtocolHTTP11}, nil)
	fixture.request = requestFor(fixture.core.snapshot, fixture.request.TargetReference)
	plan, err := fixture.planner.Plan(t.Context(), fixture.request)
	if err != nil || !plan.Eligible || plan.SelectedVariant != domain.NativeFallbackTrojanDefaultTCP {
		t.Fatalf("Trojan default was not admitted: plan=%#v err=%v", plan, err)
	}

	fixture = newPlannerFixture(t)
	fixture.core.snapshot = trojanSnapshot(fixture.core.identity, true)
	replaceTarget(t, &fixture, neutralfallback.TransportSecurityPlaintext, []neutralfallback.ApplicationProtocol{neutralfallback.ApplicationProtocolHTTP11, neutralfallback.ApplicationProtocolHTTP2}, nil)
	fixture.request = requestFor(fixture.core.snapshot, fixture.request.TargetReference)
	plan, err = fixture.planner.Plan(t.Context(), fixture.request)
	if err != nil || !plan.Eligible || plan.SelectedVariant != domain.NativeFallbackTrojanALPNTCP {
		t.Fatalf("Trojan exhaustive ALPN was not admitted: plan=%#v err=%v", plan, err)
	}

	fixture = newPlannerFixture(t)
	fixture.core.snapshot = trojanSnapshot(fixture.core.identity, true)
	replaceTarget(t, &fixture, neutralfallback.TransportSecurityPlaintext, []neutralfallback.ApplicationProtocol{neutralfallback.ApplicationProtocolHTTP11}, nil)
	fixture.request = requestFor(fixture.core.snapshot, fixture.request.TargetReference)
	plan, err = fixture.planner.Plan(t.Context(), fixture.request)
	if err != nil || !slices.Contains(plan.Blocks, domain.NativeReasonTargetALPNMismatch) {
		t.Fatalf("partial Trojan ALPN did not fail closed: plan=%#v err=%v", plan, err)
	}

	fixture = newPlannerFixture(t)
	fixture.core.snapshot = trojanSnapshot(fixture.core.identity, false)
	replaceTarget(t, &fixture, neutralfallback.TransportSecurityTLS, []neutralfallback.ApplicationProtocol{neutralfallback.ApplicationProtocolHTTP11}, []string{"decoy.example"})
	fixture.request = requestFor(fixture.core.snapshot, fixture.request.TargetReference)
	plan, err = fixture.planner.Plan(t.Context(), fixture.request)
	if err != nil || !slices.Contains(plan.Blocks, domain.NativeReasonTargetTLSModeMismatch) {
		t.Fatalf("second Trojan TLS target did not fail closed: plan=%#v err=%v", plan, err)
	}
}

func TestPlannerHealthCapacityManagementAndCancellation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*plannerFixture)
		reason domain.NativeFallbackReasonCode
	}{
		{"health not ready", func(f *plannerFixture) { f.targets.target.Health.Readiness = neutralfallback.ReadinessNotReady }, domain.NativeReasonTargetNotReady},
		{"health unknown", func(f *plannerFixture) { f.targets.target.Health.Readiness = neutralfallback.ReadinessUnknown }, domain.NativeReasonTargetHealthUnknown},
		{"health stale", func(f *plannerFixture) { f.targets.target.Health.ExpiresAt = f.now.Add(-time.Second).Unix() }, domain.NativeReasonTargetHealthStale},
		{"capacity pressured", func(f *plannerFixture) { f.targets.target.Capacity.State = neutralfallback.CapacityPressured }, domain.NativeReasonTargetCapacityPressured},
		{"capacity exhausted", func(f *plannerFixture) {
			f.targets.target.Capacity.State = neutralfallback.CapacityExhausted
			f.targets.target.Capacity.ReservationSlotsUsed = f.targets.target.Capacity.ReservationSlotsTotal
		}, domain.NativeReasonTargetCapacityExhausted},
		{"capacity unknown", func(f *plannerFixture) { f.targets.target.Capacity.State = neutralfallback.CapacityUnknown }, domain.NativeReasonTargetCapacityUnknown},
		{"capacity stale", func(f *plannerFixture) { f.targets.target.Capacity.ExpiresAt = f.now.Add(-time.Second).Unix() }, domain.NativeReasonTargetCapacityStale},
		{"management overlap", func(f *plannerFixture) {
			f.management.result.State = "FORBIDDEN"
			f.management.result.ReasonCodes = []string{"management_target_forbidden"}
		}, domain.NativeReasonManagementTargetForbidden},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newPlannerFixture(t)
			test.mutate(&fixture)
			plan, err := fixture.planner.Plan(t.Context(), fixture.request)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(plan.Blocks, test.reason) || plan.ActualState != domain.NativeActualNotApplied {
				t.Fatalf("missing %s: %#v", test.reason, plan)
			}
		})
	}

	fixture := newPlannerFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := fixture.planner.Plan(ctx, fixture.request); err == nil || fixture.targets.calls != 0 || fixture.core.previewCalls != 0 {
		t.Fatalf("cancelled plan dispatched reads: target=%d preview=%d err=%v", fixture.targets.calls, fixture.core.previewCalls, err)
	}
}

func exactRuntimeIdentity() coreinboundcontrol.CoreRuntimeIdentityV1 {
	return coreinboundcontrol.CoreRuntimeIdentityV1{
		Schema: coreinboundcontrol.RuntimeIdentitySchemaV1, State: coreinboundcontrol.RuntimeIdentityVerified,
		SingBoxModule: coreinboundcontrol.PinnedSingBoxModule, SingBoxVersion: coreinboundcontrol.PinnedSingBoxVersion,
		SingBoxModuleSum: coreinboundcontrol.PinnedSingBoxModuleSum, SingBoxSourceRevision: coreinboundcontrol.PinnedSingBoxSourceRevision,
		UTLSModule: coreinboundcontrol.PinnedUTLSModule, UTLSVersion: coreinboundcontrol.PinnedUTLSVersion,
		UTLSModuleSum: coreinboundcontrol.PinnedUTLSModuleSum, UTLSSourceRevision: coreinboundcontrol.PinnedUTLSSourceRevision,
		WithUTLS: true, BuildProfileRevision: coreinboundcontrol.BuildProfileWithUTLSRevision,
		CapabilityResolverRevision: coreinboundcontrol.CapabilityResolverRevisionV1, IdentityRevision: coreinboundcontrol.PinnedRuntimeIdentityWithUTLSRevisionV1,
	}
}

func vlessSnapshot(identity coreinboundcontrol.CoreRuntimeIdentityV1) coreinboundcontrol.InboundFallbackSnapshotV1 {
	return coreinboundcontrol.InboundFallbackSnapshotV1{
		Schema: coreinboundcontrol.InboundSnapshotSchemaV1, InboundDatabaseID: 17, ResourceID: "core:inbound:17", Tag: "public-vless", Type: "vless",
		Listener:             coreinboundcontrol.ListenerShapeV1{Network: "tcp", AddressFamily: "ipv4", Bind: "0.0.0.0", Port: 443},
		InboundOptionsDigest: strings.Repeat("1", 64), TLSRecordDatabaseID: 3, TLSOptionsDigest: strings.Repeat("2", 64), TLSReferenceCount: 1,
		TLS:                     coreinboundcontrol.TLSShapeV1{Referenced: true, Enabled: true, ServerNameDigest: serverNameDigest("decoy.example"), ALPN: []string{"h2", "http/1.1"}, Reality: coreinboundcontrol.RealityShapeV1{Present: true, Enabled: true, Handshake: coreinboundcontrol.TargetShapeV1{Present: true, Kind: "tcp_host_port", Revision: strings.Repeat("3", 64)}}},
		RuntimeIdentityRevision: identity.IdentityRevision, CapabilityResolverRevision: coreinboundcontrol.CapabilityResolverRevisionV1,
		Effective:             coreinboundcontrol.EffectiveInboundV1{RuntimeAvailable: true, Present: true, Type: "vless", Tag: "public-vless", Revision: strings.Repeat("4", 64)},
		ConfigurationRevision: strings.Repeat("5", 64),
		Capability:            coreinboundcontrol.NativeFallbackCapabilityV1{Disposition: coreinboundcontrol.CapabilitySupportedNaturalFallback, Variant: coreinboundcontrol.NativeFallbackVLESSRealityTCP, NaturalInvalidTrafficFallback: true, CapabilityResolverRevision: coreinboundcontrol.CapabilityResolverRevisionV1},
	}
}

func trojanSnapshot(identity coreinboundcontrol.CoreRuntimeIdentityV1, alpn bool) coreinboundcontrol.InboundFallbackSnapshotV1 {
	snapshot := vlessSnapshot(identity)
	snapshot.InboundDatabaseID, snapshot.ResourceID, snapshot.Tag, snapshot.Type = 18, "core:inbound:18", "public-trojan", "trojan"
	snapshot.Effective.Type, snapshot.Effective.Tag = "trojan", "public-trojan"
	snapshot.TLS.ServerNameDigest, snapshot.TLS.Reality = "", coreinboundcontrol.RealityShapeV1{}
	snapshot.TLS.ALPN = nil
	snapshot.DefaultFallback = coreinboundcontrol.TargetShapeV1{Present: true, Kind: "tcp_host_port", Revision: strings.Repeat("6", 64)}
	snapshot.Capability = coreinboundcontrol.NativeFallbackCapabilityV1{Disposition: coreinboundcontrol.CapabilitySupported, Variant: coreinboundcontrol.NativeFallbackTrojanDefaultTCP, NaturalInvalidTrafficFallback: true, CapabilityResolverRevision: coreinboundcontrol.CapabilityResolverRevisionV1}
	if alpn {
		snapshot.TLS.ALPN = []string{"h2", "http/1.1"}
		snapshot.ALPNFallbacks = []coreinboundcontrol.ALPNFallbackShapeV1{{ALPN: "h2", Target: snapshot.DefaultFallback}, {ALPN: "http/1.1", Target: snapshot.DefaultFallback}}
		snapshot.Capability.Variant = coreinboundcontrol.NativeFallbackTrojanALPNTCP
	}
	return snapshot
}

func targetFixture(t testing.TB, now time.Time, security neutralfallback.TransportSecurity, protocols []neutralfallback.ApplicationProtocol, names []string) neutralfallback.FallbackTargetV2 {
	t.Helper()
	target, err := neutralfallback.FinalizeFallbackTargetV2(neutralfallback.FallbackTargetV2{
		Schema:           neutralfallback.TargetSchemaV2,
		Identity:         neutralfallback.TargetIdentity{ProviderID: "fixture-provider", TargetID: "site-one"},
		Publish:          neutralfallback.PublishFactsV2{Revision: "publish-one", ContentDigest: strings.Repeat("a", 64)},
		Endpoint:         neutralfallback.EndpointV2{EndpointID: "endpoint-one", Network: hostresources.NetworkTCP, AddressFamily: hostresources.AddressFamilyIPv4, Address: "127.0.0.1", Port: 8443, Local: true, TransportSecurity: security, ApplicationProtocols: protocols, AcceptedServerNames: names, ProxyProtocol: hostresources.CapabilityNo, CanReachManagement: hostresources.CapabilityNo},
		Health:           neutralfallback.HealthV2{Readiness: neutralfallback.ReadinessReady, ObservedAt: now.Add(-time.Second).Unix(), ExpiresAt: now.Add(4 * time.Minute).Unix()},
		Capacity:         neutralfallback.CapacityV2{State: neutralfallback.CapacityReady, ReservationSlotsTotal: 4, ReservationSlotsUsed: 1, ObservedAt: now.Add(-time.Second).Unix(), ExpiresAt: now.Add(3 * time.Minute).Unix()},
		ProviderRevision: "provider-one", Source: "fixture-provider", ConfidenceBP: 10000,
	})
	if err != nil {
		t.Fatal(err)
	}
	return target
}

func requestFor(snapshot coreinboundcontrol.InboundFallbackSnapshotV1, reference neutralfallback.FallbackTargetReferenceV2) PlanRequestV1 {
	return PlanRequestV1{InboundDatabaseID: snapshot.InboundDatabaseID, ExpectedResourceID: snapshot.ResourceID, ExpectedSourceRevision: SourceRevision(snapshot), ExpectedResourceRevision: ResourceRevision(snapshot), ExpectedConfigurationRevision: snapshot.ConfigurationRevision, ExpectedEffectiveRevision: snapshot.Effective.Revision, TargetReference: reference, ApplyGate: string(domain.NativeApplyDisabledByDefault)}
}

func replaceTarget(t *testing.T, fixture *plannerFixture, security neutralfallback.TransportSecurity, protocols []neutralfallback.ApplicationProtocol, names []string) {
	t.Helper()
	fixture.targets.target = targetFixture(t, fixture.now, security, protocols, names)
	reference, err := neutralfallback.ReferenceV2FromTarget(fixture.targets.target)
	if err != nil {
		t.Fatal(err)
	}
	fixture.request.TargetReference = reference
}
