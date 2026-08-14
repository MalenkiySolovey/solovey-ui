package fallbacktargets

import (
	"context"
	"time"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

// legacyProviderV2 keeps the V1 API as a projection over the sole V2 provider
// registry. It deliberately exposes no mutation authority because Provider V1
// never owned reservations.
type legacyProviderV2 struct {
	Provider
}

func (provider legacyProviderV2) InventoryV2(ctx context.Context, request InventoryV2Request) (InventoryV2Result, *ProviderContractError) {
	if err := request.Validate(); err != nil {
		return InventoryV2Result{}, legacyProviderError(ProviderErrorInvalid, "legacy_provider_inventory_request_invalid")
	}
	targets, err := provider.ListTargets(ctx)
	if err != nil {
		return InventoryV2Result{}, legacyProviderError(ProviderErrorUnavailable, "legacy_provider_inventory_unavailable")
	}
	result := InventoryV2Result{Targets: make([]FallbackTargetV2, 0, min(len(targets), int(request.Limit)))}
	if len(targets) > int(request.Limit) {
		targets = targets[:request.Limit]
		result.Truncated = true
		result.ReasonCodes = []string{"fallback_target_inventory_v2_truncated"}
	}
	for _, target := range targets {
		converted, conversionErr := legacyTargetV2(target)
		if conversionErr != nil {
			result.ReasonCodes = append(result.ReasonCodes, "fallback_target_record_v2_invalid")
			continue
		}
		result.Targets = append(result.Targets, converted)
	}
	result.ReasonCodes = canonicalRegistryReasonsV2(result.ReasonCodes)
	return result, nil
}

func (provider legacyProviderV2) ResolveV2(ctx context.Context, reference FallbackTargetReferenceV2) (ResolveV2Result, *ProviderContractError) {
	if err := reference.Validate(); err != nil {
		return ResolveV2Result{}, legacyProviderError(ProviderErrorInvalid, "legacy_provider_reference_invalid")
	}
	inventory, providerError := provider.InventoryV2(ctx, InventoryV2Request{Limit: MaxTargetsV2})
	if providerError != nil {
		return ResolveV2Result{}, providerError
	}
	for _, target := range inventory.Targets {
		if target.Identity.ProviderID == reference.ProviderID && target.Identity.TargetID == reference.TargetID {
			if err := ResolveExactV2(reference, target, targetTime(target)); err != nil {
				return ResolveV2Result{}, legacyProviderError(ProviderErrorStale, "legacy_provider_reference_stale")
			}
			return ResolveV2Result{Target: target}, nil
		}
	}
	return ResolveV2Result{}, legacyProviderError(ProviderErrorNotFound, "legacy_provider_target_missing")
}

func (legacyProviderV2) Reserve(context.Context, ReserveRequestV1) (ReservationResultV1, *ProviderContractError) {
	return ReservationResultV1{}, legacyProviderReadOnly()
}

func (legacyProviderV2) FenceForMutation(context.Context, ReservationMutationRequestV1) (ReservationResultV1, *ProviderContractError) {
	return ReservationResultV1{}, legacyProviderReadOnly()
}

func (legacyProviderV2) Activate(context.Context, ReservationMutationRequestV1) (ReservationResultV1, *ProviderContractError) {
	return ReservationResultV1{}, legacyProviderReadOnly()
}

func (legacyProviderV2) Renew(context.Context, ReservationMutationRequestV1) (ReservationResultV1, *ProviderContractError) {
	return ReservationResultV1{}, legacyProviderReadOnly()
}

func (legacyProviderV2) Release(context.Context, ReleaseReservationRequestV1) (ReservationResultV1, *ProviderContractError) {
	return ReservationResultV1{}, legacyProviderReadOnly()
}

func (legacyProviderV2) GetReservation(context.Context, GetReservationRequestV1) (ReservationResultV1, *ProviderContractError) {
	return ReservationResultV1{}, legacyProviderReadOnly()
}

func (legacyProviderV2) ListReservations(context.Context, ListReservationsQueryV1) (ListReservationsResultV1, *ProviderContractError) {
	return ListReservationsResultV1{Reservations: []ProviderTargetReservationV1{}}, nil
}

func legacyTargetV2(target TargetV1) (FallbackTargetV2, error) {
	if err := validateTarget(target, targetTimeV1(target)); err != nil {
		return FallbackTargetV2{}, err
	}
	transportSecurity := TransportSecurityUnknown
	switch target.Endpoint.TLS {
	case hostresources.CapabilityNo:
		transportSecurity = TransportSecurityPlaintext
	case hostresources.CapabilityYes:
		transportSecurity = TransportSecurityTLS
	}
	capacityState := CapacityUnknown
	totalSlots := uint32(0)
	if target.Readiness == ReadinessReady {
		capacityState = CapacityReady
		totalSlots = 1
	}
	converted := FallbackTargetV2{
		Identity: target.Identity,
		Publish:  PublishFactsV2{Revision: target.PublishRevision, ContentDigest: target.ContentDigest},
		Endpoint: EndpointV2{
			EndpointID: target.Endpoint.EndpointID, Network: target.Endpoint.Network, AddressFamily: target.Endpoint.Family,
			Address: target.Endpoint.Bind, Port: target.Endpoint.Port, Local: target.Endpoint.Local,
			TransportSecurity: transportSecurity, ApplicationProtocols: []ApplicationProtocol{ApplicationProtocolHTTP11},
			AcceptedServerNames: []string{}, ProxyProtocol: hostresources.CapabilityNo,
			CanReachManagement: target.Endpoint.CanReachManagement,
		},
		Health:           HealthV2{Readiness: target.Readiness, ObservedAt: target.ObservedAt, ExpiresAt: target.ExpiresAt, ReasonCodes: target.ReasonCodes},
		Capacity:         CapacityV2{State: capacityState, ReservationSlotsTotal: totalSlots, ObservedAt: target.ObservedAt, ExpiresAt: target.ExpiresAt},
		ProviderRevision: "legacy-v1:" + target.ProviderHealthRevision,
		Source:           target.Source, ConfidenceBP: target.ConfidenceBP,
	}
	return FinalizeFallbackTargetV2(converted)
}

func targetTimeV1(target TargetV1) time.Time {
	return time.Unix(target.ObservedAt, 0).UTC()
}

func targetTime(target FallbackTargetV2) time.Time {
	return time.Unix(target.Health.ObservedAt, 0).UTC()
}

func legacyProviderReadOnly() *ProviderContractError {
	return legacyProviderError(ProviderErrorUnavailable, "legacy_provider_read_only")
}

func legacyProviderError(class ProviderErrorClass, reason string) *ProviderContractError {
	return &ProviderContractError{Class: class, ReasonCode: reason}
}

var _ ProviderV2 = legacyProviderV2{}
