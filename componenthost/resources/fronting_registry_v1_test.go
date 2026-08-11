package resources

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

type orderedFrontingProviderV1 struct {
	id   string
	fact FrontingBackendFactV1
}

func (p orderedFrontingProviderV1) ProviderID() string { return p.id }
func (p orderedFrontingProviderV1) FrontingBackendFactsV1(context.Context, time.Time) ([]FrontingBackendFactV1, error) {
	return []FrontingBackendFactV1{p.fact}, nil
}
func (orderedFrontingProviderV1) AcquireEndpointLease(context.Context, AcquireEndpointLeaseRequestV1) (EndpointLeaseV1, error) {
	return EndpointLeaseV1{}, errors.New("not used")
}
func (orderedFrontingProviderV1) FenceEndpointLease(context.Context, MutateEndpointLeaseRequestV1) (EndpointLeaseV1, error) {
	return EndpointLeaseV1{}, errors.New("not used")
}
func (orderedFrontingProviderV1) ActivateEndpointLease(context.Context, MutateEndpointLeaseRequestV1) (EndpointLeaseV1, error) {
	return EndpointLeaseV1{}, errors.New("not used")
}
func (orderedFrontingProviderV1) ReleaseEndpointLease(context.Context, ReleaseEndpointLeaseRequestV1) (EndpointLeaseV1, error) {
	return EndpointLeaseV1{}, errors.New("not used")
}
func (orderedFrontingProviderV1) GetEndpointLease(context.Context, GetEndpointLeaseRequestV1) (EndpointLeaseV1, error) {
	return EndpointLeaseV1{}, errors.New("not used")
}
func (orderedFrontingProviderV1) ListEndpointLeases(context.Context, ListEndpointLeasesRequestV1) ([]EndpointLeaseV1, error) {
	return []EndpointLeaseV1{}, nil
}

func TestFrontingBackendRegistryOrderAndRepeatedLifecycleAreDeterministic(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	provider := func(id string) orderedFrontingProviderV1 {
		fact := frontingFactFixture(t, now, "127.0.0.1", AddressFamilyIPv4)
		fact.ProviderID, fact.ContributorID, fact.ProviderRevision = id, id, id+"-v1"
		return orderedFrontingProviderV1{id: id, fact: fact}
	}
	collect := func(order []orderedFrontingProviderV1) []FrontingBackendReferenceV1 {
		registry := NewFrontingBackendRegistryV1()
		unregister := make([]func(), 0, len(order))
		for _, item := range order {
			cleanup, err := registry.Register(item)
			if err != nil {
				t.Fatal(err)
			}
			unregister = append(unregister, cleanup)
		}
		facts, err := registry.FactsV1(t.Context(), now)
		if err != nil {
			t.Fatal(err)
		}
		references := make([]FrontingBackendReferenceV1, 0, len(facts))
		for _, fact := range facts {
			reference, err := ReferenceFrontingBackendV1(fact, ProxyModeOff, now)
			if err != nil {
				t.Fatal(err)
			}
			references = append(references, reference)
		}
		for index := len(unregister) - 1; index >= 0; index-- {
			unregister[index]()
		}
		if _, err := registry.FactsV1(t.Context(), now); err == nil {
			t.Fatal("provider registry retained state after lifecycle cleanup")
		}
		cleanup, err := registry.Register(order[0])
		if err != nil {
			t.Fatalf("provider could not register after cleanup: %v", err)
		}
		cleanup()
		return references
	}
	a, b := provider("a-provider"), provider("b-provider")
	first := collect([]orderedFrontingProviderV1{b, a})
	second := collect([]orderedFrontingProviderV1{a, b})
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("provider registration order changed references: first=%#v second=%#v", first, second)
	}
}
