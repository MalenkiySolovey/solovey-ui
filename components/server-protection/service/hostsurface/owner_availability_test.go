package hostsurface

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	hostfacts "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	protectionhelper "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/helper"
)

type ownerExecutorFunc func(context.Context, protectionhelper.Request) (protectionhelper.Response, protectionhelper.ExecutionMetadata, error)

func (f ownerExecutorFunc) ExecuteWithMetadata(ctx context.Context, request protectionhelper.Request) (protectionhelper.Response, protectionhelper.ExecutionMetadata, error) {
	return f(ctx, request)
}

func TestOwnerAvailabilityStagesAreTypedAndBounded(t *testing.T) {
	now := time.Unix(7_000, 0).UTC()
	resource := firewallBaselineHostResource()
	validResult := testOwnerResult([]hostfacts.ListenerOwnerFactV1{firewallBaselineOwnerFact(resource, now)})
	validMetadata := testOwnerMetadata(resource)
	tests := []struct {
		name       string
		response   protectionhelper.Response
		metadata   protectionhelper.ExecutionMetadata
		err        error
		mutate     func(*hostresources.ProtectableResource, *protectionhelper.ListenerOwnerObserveResult)
		want       OwnerAvailability
		wantReason string
	}{
		{name: "operation_not_advertised", response: protectionhelper.Response{Code: protectionhelper.CodeMissingCapability, Reason: "missing_capability"}, metadata: validMetadata, want: OwnerOperationNotAdvertised, wantReason: "listener_owner_operation_not_advertised"},
		{name: "helper_identity_mismatch", response: protectionhelper.Response{Code: protectionhelper.CodeProcessFailed, Reason: "helper_identity_mismatch"}, metadata: validMetadata, err: protectionhelper.ErrHelperIdentityMismatch, want: OwnerHelperIdentityMismatch, wantReason: "listener_owner_helper_identity_mismatch"},
		{name: "helper_protocol_or_contract_unsupported", response: protectionhelper.Response{Code: protectionhelper.CodeMissingCapability, Reason: "helper_version_mismatch"}, metadata: validMetadata, err: errors.New("negotiation failed"), want: OwnerHelperContractUnsupported, wantReason: "listener_owner_helper_contract_unsupported"},
		{name: "owner_contract_capability_mismatch", response: testOwnerResponse(validResult), metadata: func() protectionhelper.ExecutionMetadata {
			value := validMetadata
			value.ListenerOwnerContractRevision = strings.Repeat("1", 64)
			return value
		}(), want: OwnerContractMismatch, wantReason: "listener_owner_contract_mismatch"},
		{name: "runtime_root_mismatch", response: testOwnerResponse(validResult), metadata: validMetadata, mutate: func(_ *hostresources.ProtectableResource, result *protectionhelper.ListenerOwnerObserveResult) {
			result.Facts[0].Application.RuntimeRootBindingRevision = strings.Repeat("1", 64)
			result.Facts[0].Seal()
			testSealOwnerResult(result)
		}, want: OwnerContractMismatch, wantReason: "listener_owner_contract_mismatch"},
		{name: "application_owner_contract_mismatch", response: testOwnerResponse(validResult), metadata: validMetadata, mutate: func(_ *hostresources.ProtectableResource, result *protectionhelper.ListenerOwnerObserveResult) {
			result.Facts[0].Application.OwnerContractRevision = strings.Repeat("1", 64)
			result.Facts[0].Seal()
			testSealOwnerResult(result)
		}, want: OwnerContractMismatch, wantReason: "listener_owner_contract_mismatch"},
		{name: "deployment_binding_mismatch", response: testOwnerResponse(validResult), metadata: validMetadata, mutate: func(_ *hostresources.ProtectableResource, result *protectionhelper.ListenerOwnerObserveResult) {
			result.Facts[0].Application.DeploymentID = "dep-" + strings.Repeat("1", 64)
			result.Facts[0].Seal()
			testSealOwnerResult(result)
		}, want: OwnerDeploymentBindingMismatch, wantReason: "listener_owner_deployment_binding_mismatch"},
		{name: "observation_timeout", response: protectionhelper.Response{Code: protectionhelper.CodeTimeout, Reason: "timeout"}, metadata: validMetadata, err: context.DeadlineExceeded, want: OwnerObservationTimeout, wantReason: "listener_owner_observation_timeout"},
		{name: "typed_helper_failure", response: testOwnerResponse(testOwnerResult(nil, "listener_service_unavailable")), metadata: validMetadata, want: OwnerObservationFailed, wantReason: "listener_owner_observation_failed"},
		{name: "observation_stale", response: testOwnerResponse(validResult), metadata: validMetadata, mutate: func(_ *hostresources.ProtectableResource, result *protectionhelper.ListenerOwnerObserveResult) {
			result.Facts[0].ObservedAt = now.Add(-time.Minute).Unix()
			result.Facts[0].ExpiresAt = now.Add(-30 * time.Second).Unix()
			result.Facts[0].Seal()
			testSealOwnerResult(result)
		}, want: OwnerObservationStale, wantReason: "listener_owner_observation_stale"},
		{name: "observation_ambiguous", response: testOwnerResponse(testOwnerResult(nil, "listener_owner_ambiguous")), metadata: validMetadata, want: OwnerObservationAmbiguous, wantReason: "listener_owner_observation_ambiguous"},
		{name: "observation_success", response: testOwnerResponse(validResult), metadata: validMetadata, want: OwnerObservationSuccess},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			selectedResource := resource
			response := test.response
			if response.ListenerOwner != nil {
				copy := *response.ListenerOwner
				copy.Facts = append([]hostfacts.ListenerOwnerFactV1(nil), response.ListenerOwner.Facts...)
				copy.ReasonCodes = append([]string(nil), response.ListenerOwner.ReasonCodes...)
				response.ListenerOwner = &copy
			}
			if test.mutate != nil {
				test.mutate(&selectedResource, response.ListenerOwner)
			}
			observer := HelperOwnerObserver{Now: func() time.Time { return now }, Helper: ownerExecutorFunc(func(_ context.Context, request protectionhelper.Request) (protectionhelper.Response, protectionhelper.ExecutionMetadata, error) {
				if request.Operation != protectionhelper.OperationListenerOwnerObserve || request.ListenerOwnerObserve == nil || request.ListenerOwnerObserve.ResourceID != selectedResource.ID {
					t.Fatalf("owner observer emitted an untyped request: %#v", request)
				}
				return response, test.metadata, test.err
			})}
			outcome := observer.ObserveOwner(context.Background(), selectedResource)
			if outcome.Availability != test.want {
				t.Fatalf("availability=%s reasons=%v", outcome.Availability, outcome.ReasonCodes)
			}
			if test.wantReason != "" && !slices.Contains(outcome.ReasonCodes, test.wantReason) {
				t.Fatalf("typed reason %q missing from %v", test.wantReason, outcome.ReasonCodes)
			}
			for _, reason := range outcome.ReasonCodes {
				if strings.ContainsAny(reason, "/\\\r\n\t") || len(reason) > 96 {
					t.Fatalf("unbounded diagnostic reason leaked: %q", reason)
				}
			}
		})
	}
}

func TestProviderWiringInvocationAndSnapshotFences(t *testing.T) {
	now := time.Unix(7_000, 0).UTC()
	resource := firewallBaselineHostResource()
	raw := firewallBaselineRawSocket()
	metadata := testOwnerMetadata(resource)
	var calls int
	observer := HelperOwnerObserver{Now: func() time.Time { return now }, Helper: ownerExecutorFunc(func(_ context.Context, request protectionhelper.Request) (protectionhelper.Response, protectionhelper.ExecutionMetadata, error) {
		calls++
		if request.ListenerOwnerObserve.ResourceID != resource.ID || request.ListenerOwnerObserve.ExpectedDeploymentID != resource.Capabilities.ExpectedListenerOwner.DeploymentID || request.ListenerOwnerObserve.ExpectedConfigurationRevision != resource.Capabilities.ConfigRevision {
			t.Fatalf("owner request escaped the frozen resource binding: %#v", request.ListenerOwnerObserve)
		}
		return testOwnerResponse(testOwnerResult([]hostfacts.ListenerOwnerFactV1{firewallBaselineOwnerFact(resource, now)})), metadata, nil
	})}
	provider := NewProvider(observer)
	provider.Now = func() time.Time { return now }
	provider.Resources = func(context.Context) hostresources.ResourceSnapshot {
		return hostresources.ResourceSnapshot{Resources: []hostresources.ProtectableResource{resource}}
	}
	provider.ObservePlatform = func(context.Context, hostfacts.Limits) (PlatformSnapshot, error) {
		return PlatformSnapshot{Sockets: []RawSocket{raw}}, nil
	}
	if provider.ObservationTimeout() != 80*time.Second {
		t.Fatalf("owner provider budget=%s", provider.ObservationTimeout())
	}
	observation, err := provider.Observe(context.Background(), hostfacts.DefaultLimits())
	if err != nil || calls != 1 || len(observation.Facts) != 1 || observation.Facts[0].Classification != hostfacts.ClassificationManagedExact || observation.Facts[0].ListenerOwner == nil {
		t.Fatalf("production-style owner path was not invoked exactly once: calls=%d observation=%#v err=%v", calls, observation, err)
	}
	if !exactOwnerRevision(observation.OwnerObservationRevision) {
		t.Fatalf("owner set revision is missing: %q", observation.OwnerObservationRevision)
	}

	state := ownerState(OwnerObservation{Availability: OwnerObservationSuccess, Observation: testOwnerResult([]hostfacts.ListenerOwnerFactV1{firewallBaselineOwnerFact(resource, now)}), HelperIdentityRevision: metadata.HelperIdentityRevision, CapabilityRevision: metadata.CapabilityRevision, ListenerOwnerContractRevision: metadata.ListenerOwnerContractRevision, ListenerOwnerObserverRevision: metadata.ListenerOwnerObserverRevision}, provider.BindingRevision)
	first := NormalizeWithOwners(PlatformSnapshot{Sockets: []RawSocket{raw}}, hostresources.ResourceSnapshot{Resources: []hostresources.ProtectableResource{resource}}, map[string]ownerObservationState{resource.ID: state}, now)
	state.CapabilityRevision = strings.Repeat("1", 64)
	second := NormalizeWithOwners(PlatformSnapshot{Sockets: []RawSocket{raw}}, hostresources.ResourceSnapshot{Resources: []hostresources.ProtectableResource{resource}}, map[string]ownerObservationState{resource.ID: state}, now)
	state.CapabilityRevision = metadata.CapabilityRevision
	state.HelperIdentityRevision = strings.Repeat("2", 64)
	third := NormalizeWithOwners(PlatformSnapshot{Sockets: []RawSocket{raw}}, hostresources.ResourceSnapshot{Resources: []hostresources.ProtectableResource{resource}}, map[string]ownerObservationState{resource.ID: state}, now)
	state.HelperIdentityRevision = metadata.HelperIdentityRevision
	state.ProviderBindingRevision = strings.Repeat("3", 64)
	fourth := NormalizeWithOwners(PlatformSnapshot{Sockets: []RawSocket{raw}}, hostresources.ResourceSnapshot{Resources: []hostresources.ProtectableResource{resource}}, map[string]ownerObservationState{resource.ID: state}, now)
	if first.OwnerObservationRevision == second.OwnerObservationRevision || first.OwnerObservationRevision == third.OwnerObservationRevision || first.OwnerObservationRevision == fourth.OwnerObservationRevision {
		t.Fatal("helper capability, executable identity, or provider binding change retained the frozen owner revision")
	}
}

func TestProviderDistinguishesUnboundObserverFromAdvertisedCapability(t *testing.T) {
	now := time.Unix(7_000, 0).UTC()
	resource := firewallBaselineHostResource()
	manualProof := HelperOwnerObserver{Now: func() time.Time { return now }, Helper: ownerExecutorFunc(func(context.Context, protectionhelper.Request) (protectionhelper.Response, protectionhelper.ExecutionMetadata, error) {
		return testOwnerResponse(testOwnerResult([]hostfacts.ListenerOwnerFactV1{firewallBaselineOwnerFact(resource, now)})), testOwnerMetadata(resource), nil
	})}.ObserveOwner(context.Background(), resource)
	if manualProof.Availability != OwnerObservationSuccess {
		t.Fatalf("manual observer fixture is not valid: %#v", manualProof)
	}
	provider := NewProvider()
	provider.Now = func() time.Time { return now }
	provider.Resources = func(context.Context) hostresources.ResourceSnapshot {
		return hostresources.ResourceSnapshot{Resources: []hostresources.ProtectableResource{resource}}
	}
	provider.ObservePlatform = func(context.Context, hostfacts.Limits) (PlatformSnapshot, error) {
		return PlatformSnapshot{Sockets: []RawSocket{firewallBaselineRawSocket()}}, nil
	}
	observation, err := provider.Observe(context.Background(), hostfacts.DefaultLimits())
	if err != nil || len(observation.Facts) != 1 || observation.Facts[0].Classification != hostfacts.ClassificationUnknownOwner || !slices.Contains(observation.Facts[0].ReasonCodes, "listener_owner_observer_not_bound") {
		t.Fatalf("unbound production provider was not typed fail-closed: %#v err=%v", observation, err)
	}
	registered := UnavailableOwnerObserver{Availability: OwnerObserverNotRegistered}.ObserveOwner(context.Background(), resource)
	if registered.Availability != OwnerObserverNotRegistered || !slices.Contains(registered.ReasonCodes, "listener_owner_observer_not_registered") {
		t.Fatalf("observer registration failure collapsed: %#v", registered)
	}
	notInstalled := UnavailableOwnerObserver{Availability: OwnerHelperNotInstalled}.ObserveOwner(context.Background(), resource)
	if notInstalled.Availability != OwnerHelperNotInstalled || !slices.Contains(notInstalled.ReasonCodes, "listener_owner_helper_not_installed") {
		t.Fatalf("missing helper installation collapsed: %#v", notInstalled)
	}
}

func TestProviderUsesGenericObserverForPanelAndSubscriptionAndRejectsCapabilityDrift(t *testing.T) {
	now := time.Unix(7_000, 0).UTC()
	panel := firewallBaselineHostResource()
	subscription := panel
	subscription.ID = "core:subscription:default"
	subscription.Port = 2096
	subscription.Capabilities.OwnerRevision = strings.Repeat("8", 64)
	subscription.Capabilities.ConfigRevision = strings.Repeat("9", 64)
	subscription.ListenIntent = hostresources.BuildConfiguredListenIntent(subscription)
	var mu sync.Mutex
	called := []string{}
	observer := ownerExecutorFunc(func(_ context.Context, request protectionhelper.Request) (protectionhelper.Response, protectionhelper.ExecutionMetadata, error) {
		mu.Lock()
		called = append(called, request.ListenerOwnerObserve.ResourceID)
		mu.Unlock()
		resource := panel
		if request.ListenerOwnerObserve.ResourceID == subscription.ID {
			resource = subscription
		}
		metadata := testOwnerMetadata(resource)
		if resource.ID == subscription.ID {
			metadata.CapabilityRevision = strings.Repeat("1", 64)
		}
		return testOwnerResponse(testOwnerResult(nil, "listener_unobserved")), metadata, nil
	})
	provider := NewProvider(HelperOwnerObserver{Now: func() time.Time { return now }, Helper: observer})
	provider.Now = func() time.Time { return now }
	provider.Resources = func(context.Context) hostresources.ResourceSnapshot {
		return hostresources.ResourceSnapshot{Resources: []hostresources.ProtectableResource{subscription, panel}}
	}
	provider.ObservePlatform = func(context.Context, hostfacts.Limits) (PlatformSnapshot, error) { return PlatformSnapshot{}, nil }
	observation, err := provider.Observe(context.Background(), hostfacts.DefaultLimits())
	sort.Strings(called)
	if err != nil || !slices.Equal(called, []string{panel.ID, subscription.ID}) || len(observation.Facts) != 2 {
		t.Fatalf("generic resource owner path was not used: called=%v observation=%#v err=%v", called, observation, err)
	}
	for _, fact := range observation.Facts {
		if !slices.Contains(fact.ReasonCodes, "listener_owner_helper_capability_stale") || fact.Classification != hostfacts.ClassificationUnobserved {
			t.Fatalf("cross-resource capability drift was not fenced: %#v", fact)
		}
	}
}

func TestOwnerObserverNeverAcceptsManualInputsMutationOrSecretDiagnostics(t *testing.T) {
	resource := firewallBaselineHostResource()
	metadata := testOwnerMetadata(resource)
	observer := HelperOwnerObserver{Helper: ownerExecutorFunc(func(_ context.Context, request protectionhelper.Request) (protectionhelper.Response, protectionhelper.ExecutionMetadata, error) {
		if request.Operation != protectionhelper.OperationListenerOwnerObserve || request.ListenerOwnerObserve == nil || request.NFTApply != nil || request.NFTRollback != nil || request.NFTValidate != nil || request.ListenerProbe != nil {
			t.Fatalf("owner observation expanded into mutation or generic helper input: %#v", request)
		}
		payload, _ := json.Marshal(request.ListenerOwnerObserve)
		if strings.Contains(string(payload), "password") || strings.Contains(string(payload), "secret") || strings.Contains(string(payload), "/proc/") {
			t.Fatalf("owner request leaked manual/secret input: %s", payload)
		}
		return protectionhelper.Response{Code: protectionhelper.CodeProcessFailed, Reason: "/proc/123/fd/secret"}, metadata, errors.New("raw process error")
	})}
	outcome := observer.ObserveOwner(context.Background(), resource)
	if outcome.Availability != OwnerObservationFailed {
		t.Fatalf("typed helper failure availability=%s", outcome.Availability)
	}
	payload, _ := json.Marshal(outcome)
	if strings.Contains(string(payload), "/proc/") || strings.Contains(string(payload), "secret") || strings.Contains(string(payload), "raw process error") {
		t.Fatalf("secret or raw helper diagnostic crossed owner boundary: %s", payload)
	}
}

func testOwnerMetadata(resource hostresources.ProtectableResource) protectionhelper.ExecutionMetadata {
	return protectionhelper.ExecutionMetadata{
		HelperIdentityRevision: strings.Repeat("d", 64), CapabilityRevision: strings.Repeat("e", 64),
		ListenerOwnerContractRevision: resource.Capabilities.ExpectedListenerOwner.ContractRevision,
		ListenerOwnerObserverRevision: strings.Repeat("f", 64),
	}
}

func testOwnerResponse(result *protectionhelper.ListenerOwnerObserveResult) protectionhelper.Response {
	return protectionhelper.Response{OK: true, ListenerOwner: result}
}

func testOwnerResult(facts []hostfacts.ListenerOwnerFactV1, reasons ...string) *protectionhelper.ListenerOwnerObserveResult {
	result := &protectionhelper.ListenerOwnerObserveResult{Facts: append([]hostfacts.ListenerOwnerFactV1(nil), facts...), ReasonCodes: append([]string(nil), reasons...)}
	testSealOwnerResult(result)
	return result
}

func testSealOwnerResult(result *protectionhelper.ListenerOwnerObserveResult) {
	if result == nil {
		return
	}
	sort.Slice(result.Facts, func(i, j int) bool { return result.Facts[i].ObservationRevision < result.Facts[j].ObservationRevision })
	sort.Strings(result.ReasonCodes)
	copy := *result
	copy.ObservationRevision = ""
	copy.Facts = append([]hostfacts.ListenerOwnerFactV1(nil), result.Facts...)
	for index := range copy.Facts {
		copy.Facts[index].ObservedAt, copy.Facts[index].ExpiresAt = 0, 0
	}
	data, _ := json.Marshal(copy)
	sum := sha256.Sum256(data)
	result.ObservationRevision = hex.EncodeToString(sum[:])
}
