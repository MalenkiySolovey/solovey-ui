package fronting

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/fallbacktargets"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
)

type FallbackReservationDirectoryV2 interface {
	ProviderV2(string) (fallbacktargets.ProviderV2, bool)
	ListReservationsV2(context.Context, fallbacktargets.ListReservationsQueryV1) (fallbacktargets.RegistryReservationsV2, error)
}

type targetAuthorityKindV2 string

const (
	targetAuthorityEndpointV2 targetAuthorityKindV2 = "ENDPOINT_LEASE"
	targetAuthorityFallbackV2 targetAuthorityKindV2 = "FALLBACK_RESERVATION"
)

type targetAuthoritySpecV2 struct {
	Kind              targetAuthorityKindV2
	ReferenceRevision string
	Endpoint          hostresources.FrontingBackendReferenceV1
	Fallback          fallbacktargets.FallbackTargetReferenceV2
}

type currentTargetAuthorityV2 struct {
	Spec                targetAuthoritySpecV2
	EndpointProvider    hostresources.EndpointLeaseProviderV1
	EndpointLease       hostresources.EndpointLeaseV1
	FallbackProvider    fallbacktargets.ProviderV2
	FallbackReservation fallbacktargets.ProviderTargetReservationV1
}

func targetAuthoritySpecsV2(plan FrontingStrategyPlanV2) ([]targetAuthoritySpecV2, error) {
	specs := make([]targetAuthoritySpecV2, 0, len(plan.Targets.ReferenceRevisions))
	selected := make(map[string]bool, len(plan.Selectors.TargetRevisions))
	if plan.Strategy.Selected == StrategySNIPreread {
		for _, revision := range plan.Selectors.TargetRevisions {
			selected[revision] = true
		}
	}
	for _, reference := range plan.Targets.BackendReferences {
		revision := reference.CanonicalReferenceRevision
		if len(selected) == 0 || selected[revision] {
			specs = append(specs, targetAuthoritySpecV2{Kind: targetAuthorityEndpointV2, ReferenceRevision: revision, Endpoint: reference})
		}
	}
	for _, reference := range plan.Targets.FallbackReferences {
		revision := v2Revision(reference)
		if len(selected) == 0 || selected[revision] {
			specs = append(specs, targetAuthoritySpecV2{Kind: targetAuthorityFallbackV2, ReferenceRevision: revision, Fallback: reference})
		}
	}
	sort.Slice(specs, func(left, right int) bool { return specs[left].ReferenceRevision < specs[right].ReferenceRevision })
	for index, spec := range specs {
		if !frontingHexV2(spec.ReferenceRevision) || index > 0 && specs[index-1].ReferenceRevision >= spec.ReferenceRevision {
			return nil, errors.New("target_authority_conflict")
		}
	}
	if plan.Strategy.Selected == StrategyL4OneToOne && (len(specs) != 1 || specs[0].Kind != targetAuthorityEndpointV2) {
		return nil, errors.New("target_authority_conflict")
	}
	if plan.Strategy.Selected == StrategySNIPreread && len(specs) != len(plan.Selectors.TargetRevisions) {
		return nil, errors.New("target_authority_conflict")
	}
	return specs, nil
}

func authorityRevisionsV2(checkpoint CheckpointV2) (map[string]string, error) {
	result := make(map[string]string, len(checkpoint.EndpointLeases)+len(checkpoint.FallbackReservations))
	for _, lease := range checkpoint.EndpointLeases {
		if lease.Validate() != nil || result[lease.ExactReference.CanonicalReferenceRevision] != "" {
			return nil, errors.New("target_authority_stale")
		}
		result[lease.ExactReference.CanonicalReferenceRevision] = v2Revision(struct {
			Kind, ID, Revision string
			State              hostresources.EndpointLeaseState
		}{string(targetAuthorityEndpointV2), lease.LeaseID, lease.LeaseRevision, lease.State})
	}
	for _, reservation := range checkpoint.FallbackReservations {
		revision := v2Revision(reservation.ExactTargetReference)
		if reservation.Validate() != nil || result[revision] != "" {
			return nil, errors.New("target_authority_stale")
		}
		result[revision] = v2Revision(struct {
			Kind, ID, Revision string
			State              fallbacktargets.ReservationState
		}{string(targetAuthorityFallbackV2), reservation.ReservationID, reservation.ReservationRevision, reservation.State})
	}
	return result, nil
}

func canonicalizeCheckpointAuthoritiesV2(checkpoint *CheckpointV2) {
	sort.Slice(checkpoint.EndpointLeases, func(left, right int) bool {
		return checkpoint.EndpointLeases[left].ExactReference.CanonicalReferenceRevision < checkpoint.EndpointLeases[right].ExactReference.CanonicalReferenceRevision
	})
	sort.Slice(checkpoint.FallbackReservations, func(left, right int) bool {
		return v2Revision(checkpoint.FallbackReservations[left].ExactTargetReference) < v2Revision(checkpoint.FallbackReservations[right].ExactTargetReference)
	})
	if checkpoint.Plan.Strategy.Selected == StrategyL4OneToOne && len(checkpoint.EndpointLeases) == 1 && len(checkpoint.FallbackReservations) == 0 {
		checkpoint.Lease = checkpoint.EndpointLeases[0]
	} else {
		checkpoint.Lease = hostresources.EndpointLeaseV1{}
	}
}

func (w *Workflow) acquireTargetAuthoritiesV2(ctx context.Context, checkpoint *CheckpointV2) string {
	specs, err := targetAuthoritySpecsV2(checkpoint.Plan)
	if err != nil {
		return "lease_conflict"
	}
	for index, spec := range specs {
		requestID := checkpoint.OperationID + "-acquire-" + strconv.Itoa(index)
		switch spec.Kind {
		case targetAuthorityEndpointV2:
			provider, ok := w.V2Leases.EndpointLeaseProviderV1(spec.Endpoint.ProviderID)
			if !ok || provider == nil || strings.TrimSpace(provider.ProviderID()) != spec.Endpoint.ProviderID {
				return "lease_conflict"
			}
			lease, code := acquireLeaseRequestV2(ctx, provider, checkpoint.OperationID, requestID, spec.Endpoint)
			if code != "" {
				return code
			}
			checkpoint.EndpointLeases = append(checkpoint.EndpointLeases, lease)
		case targetAuthorityFallbackV2:
			if w.V2Fallbacks == nil {
				return "lease_conflict"
			}
			provider, ok := w.V2Fallbacks.ProviderV2(spec.Fallback.ProviderID)
			if !ok || provider == nil || strings.TrimSpace(provider.ProviderID()) != spec.Fallback.ProviderID {
				return "lease_conflict"
			}
			reservation, code := reserveFallbackV2(ctx, provider, checkpoint.OperationID, requestID, spec.Fallback)
			if code != "" {
				return code
			}
			checkpoint.FallbackReservations = append(checkpoint.FallbackReservations, reservation)
		}
		canonicalizeCheckpointAuthoritiesV2(checkpoint)
		if err := w.saveV2(checkpoint, "target_authority_acquired"); err != nil {
			return "artifact_integrity_failed"
		}
	}
	return ""
}

func (w *Workflow) currentTargetAuthoritiesV2(ctx context.Context, checkpoint CheckpointV2) ([]currentTargetAuthorityV2, string) {
	specs, err := targetAuthoritySpecsV2(checkpoint.Plan)
	if err != nil {
		return nil, "lease_stale"
	}
	leaseByRevision := make(map[string]hostresources.EndpointLeaseV1, len(checkpoint.EndpointLeases))
	for _, lease := range checkpoint.EndpointLeases {
		leaseByRevision[lease.ExactReference.CanonicalReferenceRevision] = lease
	}
	reservationByRevision := make(map[string]fallbacktargets.ProviderTargetReservationV1, len(checkpoint.FallbackReservations))
	for _, reservation := range checkpoint.FallbackReservations {
		reservationByRevision[v2Revision(reservation.ExactTargetReference)] = reservation
	}
	result := make([]currentTargetAuthorityV2, 0, len(specs))
	for _, spec := range specs {
		current := currentTargetAuthorityV2{Spec: spec}
		switch spec.Kind {
		case targetAuthorityEndpointV2:
			mirror, ok := leaseByRevision[spec.ReferenceRevision]
			if !ok {
				return nil, "lease_lost"
			}
			provider, ok := w.V2Leases.EndpointLeaseProviderV1(spec.Endpoint.ProviderID)
			if !ok || provider == nil || strings.TrimSpace(provider.ProviderID()) != spec.Endpoint.ProviderID {
				return nil, "lease_lost"
			}
			lease, code := getLeaseV2(ctx, provider, mirror.LeaseID)
			if code != "" || lease.LeaseRevision != mirror.LeaseRevision || lease.HolderID != checkpoint.OperationID || lease.ExactReference != spec.Endpoint ||
				lease.State != hostresources.EndpointLeaseReleased && lease.ExpiresAt <= w.nowV2().Unix() {
				return nil, firstV2Code(code, "lease_stale")
			}
			current.EndpointProvider, current.EndpointLease = provider, lease
		case targetAuthorityFallbackV2:
			mirror, ok := reservationByRevision[spec.ReferenceRevision]
			if !ok || w.V2Fallbacks == nil {
				return nil, "lease_lost"
			}
			provider, ok := w.V2Fallbacks.ProviderV2(spec.Fallback.ProviderID)
			if !ok || provider == nil || strings.TrimSpace(provider.ProviderID()) != spec.Fallback.ProviderID {
				return nil, "lease_lost"
			}
			reservation, code := getFallbackReservationV2(ctx, provider, mirror.ReservationID)
			status := reservation.Status(w.nowV2())
			if code != "" || reservation.ReservationRevision != mirror.ReservationRevision || reservation.HolderID != checkpoint.OperationID ||
				reservation.ExactTargetReference != spec.Fallback || reservation.Purpose != fallbacktargets.ReservationPurposeFronting ||
				reservation.State != fallbacktargets.ReservationReleased && !status.Fresh {
				return nil, firstV2Code(code, "lease_stale")
			}
			current.FallbackProvider, current.FallbackReservation = provider, reservation
		}
		result = append(result, current)
	}
	return result, ""
}

func (w *Workflow) currentPersistedTargetAuthoritiesV2(ctx context.Context, checkpoint CheckpointV2) ([]currentTargetAuthorityV2, string) {
	result := make([]currentTargetAuthorityV2, 0, len(checkpoint.EndpointLeases)+len(checkpoint.FallbackReservations))
	for _, mirror := range checkpoint.EndpointLeases {
		provider, ok := w.V2Leases.EndpointLeaseProviderV1(mirror.AuthorityProviderID)
		if !ok || provider == nil || strings.TrimSpace(provider.ProviderID()) != mirror.AuthorityProviderID {
			return nil, "lease_lost"
		}
		lease, code := getLeaseV2(ctx, provider, mirror.LeaseID)
		if code != "" || lease.LeaseRevision != mirror.LeaseRevision || lease.HolderID != checkpoint.OperationID || lease.ExactReference != mirror.ExactReference {
			return nil, firstV2Code(code, "lease_stale")
		}
		result = append(result, currentTargetAuthorityV2{Spec: targetAuthoritySpecV2{Kind: targetAuthorityEndpointV2,
			ReferenceRevision: mirror.ExactReference.CanonicalReferenceRevision, Endpoint: mirror.ExactReference}, EndpointProvider: provider, EndpointLease: lease})
	}
	for _, mirror := range checkpoint.FallbackReservations {
		if w.V2Fallbacks == nil {
			return nil, "lease_lost"
		}
		providerID := mirror.ExactTargetReference.ProviderID
		provider, ok := w.V2Fallbacks.ProviderV2(providerID)
		if !ok || provider == nil || strings.TrimSpace(provider.ProviderID()) != providerID {
			return nil, "lease_lost"
		}
		reservation, code := getFallbackReservationV2(ctx, provider, mirror.ReservationID)
		if code != "" || reservation.ReservationRevision != mirror.ReservationRevision || reservation.HolderID != checkpoint.OperationID ||
			reservation.ExactTargetReference != mirror.ExactTargetReference {
			return nil, firstV2Code(code, "lease_stale")
		}
		result = append(result, currentTargetAuthorityV2{Spec: targetAuthoritySpecV2{Kind: targetAuthorityFallbackV2,
			ReferenceRevision: v2Revision(mirror.ExactTargetReference), Fallback: mirror.ExactTargetReference}, FallbackProvider: provider, FallbackReservation: reservation})
	}
	sort.Slice(result, func(left, right int) bool {
		return result[left].Spec.ReferenceRevision < result[right].Spec.ReferenceRevision
	})
	return result, ""
}

func updateCheckpointAuthoritiesV2(checkpoint *CheckpointV2, current []currentTargetAuthorityV2) {
	checkpoint.EndpointLeases = checkpoint.EndpointLeases[:0]
	checkpoint.FallbackReservations = checkpoint.FallbackReservations[:0]
	for _, authority := range current {
		if authority.Spec.Kind == targetAuthorityEndpointV2 {
			checkpoint.EndpointLeases = append(checkpoint.EndpointLeases, authority.EndpointLease)
		} else {
			checkpoint.FallbackReservations = append(checkpoint.FallbackReservations, authority.FallbackReservation)
		}
	}
	canonicalizeCheckpointAuthoritiesV2(checkpoint)
}

func fenceTargetAuthoritiesV2(ctx context.Context, current []currentTargetAuthorityV2, requestPrefix string, now time.Time) ([]currentTargetAuthorityV2, string) {
	for index := range current {
		requestID := requestPrefix + "-" + strconv.Itoa(index)
		if current[index].Spec.Kind == targetAuthorityEndpointV2 {
			lease, code := fenceLeaseV2(ctx, current[index].EndpointProvider, current[index].EndpointLease, requestID, now)
			if code != "" {
				return current, code
			}
			current[index].EndpointLease = lease
		} else {
			reservation, code := fenceFallbackV2(ctx, current[index].FallbackProvider, current[index].FallbackReservation, requestID, now)
			if code != "" {
				return current, code
			}
			current[index].FallbackReservation = reservation
		}
	}
	return current, ""
}

func activateTargetAuthoritiesV2(ctx context.Context, current []currentTargetAuthorityV2, requestPrefix string, now time.Time) ([]currentTargetAuthorityV2, string) {
	for index := range current {
		requestID := requestPrefix + "-" + strconv.Itoa(index)
		if current[index].Spec.Kind == targetAuthorityEndpointV2 {
			if current[index].EndpointLease.State == hostresources.EndpointLeaseActive {
				continue
			}
			lease, code := activateLeaseV2(ctx, current[index].EndpointProvider, current[index].EndpointLease, requestID, now)
			if code != "" {
				return current, code
			}
			current[index].EndpointLease = lease
		} else {
			if current[index].FallbackReservation.State == fallbacktargets.ReservationActive {
				continue
			}
			reservation, code := activateFallbackV2(ctx, current[index].FallbackProvider, current[index].FallbackReservation, requestID, now)
			if code != "" {
				return current, code
			}
			current[index].FallbackReservation = reservation
		}
	}
	return current, ""
}

func releaseTargetAuthoritiesV2(ctx context.Context, current []currentTargetAuthorityV2, requestPrefix, detachmentRevision string, now time.Time) ([]currentTargetAuthorityV2, string) {
	for index := range current {
		requestID := requestPrefix + "-" + strconv.Itoa(index)
		if current[index].Spec.Kind == targetAuthorityEndpointV2 {
			if current[index].EndpointLease.State == hostresources.EndpointLeaseReleased {
				continue
			}
			lease, code := releaseLeaseV2(ctx, current[index].EndpointProvider, current[index].EndpointLease, requestID, detachmentRevision, now)
			if code != "" {
				return current, code
			}
			current[index].EndpointLease = lease
		} else {
			if current[index].FallbackReservation.State == fallbacktargets.ReservationReleased {
				continue
			}
			reservation, code := releaseFallbackV2(ctx, current[index].FallbackProvider, current[index].FallbackReservation, requestID, detachmentRevision, now)
			if code != "" {
				return current, code
			}
			current[index].FallbackReservation = reservation
		}
	}
	return current, ""
}

func authoritiesInStateV2(current []currentTargetAuthorityV2, endpoint hostresources.EndpointLeaseState, fallback fallbacktargets.ReservationState) bool {
	for _, authority := range current {
		if authority.Spec.Kind == targetAuthorityEndpointV2 && authority.EndpointLease.State != endpoint ||
			authority.Spec.Kind == targetAuthorityFallbackV2 && authority.FallbackReservation.State != fallback {
			return false
		}
	}
	return len(current) > 0
}

func acquireLeaseRequestV2(ctx context.Context, provider hostresources.EndpointLeaseProviderV1, holder, requestID string, reference hostresources.FrontingBackendReferenceV1) (hostresources.EndpointLeaseV1, string) {
	request := hostresources.AcquireEndpointLeaseRequestV1{RequestID: requestID, HolderID: holder, Purpose: hostresources.EndpointLeasePurposeL4FrontingV1,
		ExactReference: reference, FreshnessSeconds: uint32(hostresources.MaxEndpointLeaseFreshnessV1 / time.Second)}
	if request.Validate() != nil {
		return hostresources.EndpointLeaseV1{}, "lease_conflict"
	}
	lease, err := safeLeaseCallV2(ctx, func(callCtx context.Context) (hostresources.EndpointLeaseV1, error) {
		return provider.AcquireEndpointLease(callCtx, request)
	})
	if err != nil {
		if errors.Is(err, hostresources.ErrEndpointLeaseConflictV1) {
			return hostresources.EndpointLeaseV1{}, "lease_conflict"
		}
		return hostresources.EndpointLeaseV1{}, "ambiguous_result"
	}
	if lease.Validate() != nil || lease.AuthorityProviderID != reference.ProviderID || lease.HolderID != holder || lease.ExactReference != reference || lease.State != hostresources.EndpointLeaseReserved {
		return hostresources.EndpointLeaseV1{}, "lease_conflict"
	}
	return lease, ""
}

type fallbackReservationResultV2 struct {
	reservation fallbacktargets.ProviderTargetReservationV1
	providerErr *fallbacktargets.ProviderContractError
}

func safeFallbackCallV2(ctx context.Context, call func(context.Context) (fallbacktargets.ReservationResultV1, *fallbacktargets.ProviderContractError)) (fallbacktargets.ProviderTargetReservationV1, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	callCtx, cancel := context.WithTimeout(ctx, v2ProviderTimeout)
	defer cancel()
	result := make(chan fallbackReservationResultV2, 1)
	go func() {
		defer func() {
			if recover() != nil {
				result <- fallbackReservationResultV2{providerErr: &fallbacktargets.ProviderContractError{Class: fallbacktargets.ProviderErrorUnavailable, ReasonCode: "provider_unavailable"}}
			}
		}()
		value, providerErr := call(callCtx)
		result <- fallbackReservationResultV2{reservation: value.Reservation, providerErr: providerErr}
	}()
	select {
	case value := <-result:
		if value.providerErr != nil || callCtx.Err() != nil {
			return fallbacktargets.ProviderTargetReservationV1{}, errors.New("fallback_provider_unavailable")
		}
		return value.reservation, nil
	case <-callCtx.Done():
		return fallbacktargets.ProviderTargetReservationV1{}, errors.New("fallback_provider_unavailable")
	}
}

func reserveFallbackV2(ctx context.Context, provider fallbacktargets.ProviderV2, holder, requestID string, reference fallbacktargets.FallbackTargetReferenceV2) (fallbacktargets.ProviderTargetReservationV1, string) {
	request := fallbacktargets.ReserveRequestV1{RequestID: requestID, HolderID: holder, Purpose: fallbacktargets.ReservationPurposeFronting,
		ExactTargetReference: reference, FreshnessDurationSecs: uint32(fallbacktargets.MaxPrepareReservationFreshnessV1 / time.Second)}
	if request.Validate() != nil {
		return fallbacktargets.ProviderTargetReservationV1{}, "lease_conflict"
	}
	reservation, err := safeFallbackCallV2(ctx, func(callCtx context.Context) (fallbacktargets.ReservationResultV1, *fallbacktargets.ProviderContractError) {
		return provider.Reserve(callCtx, request)
	})
	if err != nil {
		return fallbacktargets.ProviderTargetReservationV1{}, "ambiguous_result"
	}
	if reservation.Validate() != nil || reservation.HolderID != holder || reservation.Purpose != fallbacktargets.ReservationPurposeFronting ||
		reservation.ExactTargetReference != reference || reservation.State != fallbacktargets.ReservationReserved || !reservation.Status(time.Unix(reservation.RenewedAt, 0)).Fresh {
		return fallbacktargets.ProviderTargetReservationV1{}, "lease_conflict"
	}
	return reservation, ""
}

func fenceFallbackV2(ctx context.Context, provider fallbacktargets.ProviderV2, current fallbacktargets.ProviderTargetReservationV1, requestID string, now time.Time) (fallbacktargets.ProviderTargetReservationV1, string) {
	request := fallbacktargets.ReservationMutationRequestV1{RequestID: requestID, ReservationID: current.ReservationID, ExpectedRevision: current.ReservationRevision}
	if request.Validate(false) != nil {
		return fallbacktargets.ProviderTargetReservationV1{}, "lease_stale"
	}
	next, err := safeFallbackCallV2(ctx, func(callCtx context.Context) (fallbacktargets.ReservationResultV1, *fallbacktargets.ProviderContractError) {
		return provider.FenceForMutation(callCtx, request)
	})
	if err != nil {
		return fallbacktargets.ProviderTargetReservationV1{}, "ambiguous_result"
	}
	cas := fallbacktargets.ReservationCASV1{RequestID: request.RequestID, ReservationID: request.ReservationID, ExpectedRevision: request.ExpectedRevision}
	if fallbacktargets.ValidateReservationTransition(current, next, cas, fallbacktargets.ReservationMutationFence, now) != nil {
		return fallbacktargets.ProviderTargetReservationV1{}, "lease_stale"
	}
	return next, ""
}

func activateFallbackV2(ctx context.Context, provider fallbacktargets.ProviderV2, current fallbacktargets.ProviderTargetReservationV1, requestID string, now time.Time) (fallbacktargets.ProviderTargetReservationV1, string) {
	request := fallbacktargets.ReservationMutationRequestV1{RequestID: requestID, ReservationID: current.ReservationID, ExpectedRevision: current.ReservationRevision,
		FreshnessDurationSecs: uint32(fallbacktargets.MaxActiveReservationFreshnessV1 / time.Second)}
	if request.Validate(true) != nil {
		return fallbacktargets.ProviderTargetReservationV1{}, "lease_stale"
	}
	next, err := safeFallbackCallV2(ctx, func(callCtx context.Context) (fallbacktargets.ReservationResultV1, *fallbacktargets.ProviderContractError) {
		return provider.Activate(callCtx, request)
	})
	if err != nil {
		return fallbacktargets.ProviderTargetReservationV1{}, "ambiguous_result"
	}
	cas := fallbacktargets.ReservationCASV1{RequestID: request.RequestID, ReservationID: request.ReservationID, ExpectedRevision: request.ExpectedRevision}
	if fallbacktargets.ValidateReservationTransition(current, next, cas, fallbacktargets.ReservationMutationActivate, now) != nil {
		return fallbacktargets.ProviderTargetReservationV1{}, "lease_stale"
	}
	return next, ""
}

func releaseFallbackV2(ctx context.Context, provider fallbacktargets.ProviderV2, current fallbacktargets.ProviderTargetReservationV1, requestID, detachmentRevision string, now time.Time) (fallbacktargets.ProviderTargetReservationV1, string) {
	request := fallbacktargets.ReleaseReservationRequestV1{RequestID: requestID, ReservationID: current.ReservationID, ExpectedRevision: current.ReservationRevision,
		VerifiedDetachedRevision: detachmentRevision}
	if request.Validate() != nil {
		return fallbacktargets.ProviderTargetReservationV1{}, "lease_stale"
	}
	next, err := safeFallbackCallV2(ctx, func(callCtx context.Context) (fallbacktargets.ReservationResultV1, *fallbacktargets.ProviderContractError) {
		return provider.Release(callCtx, request)
	})
	if err != nil {
		return fallbacktargets.ProviderTargetReservationV1{}, "ambiguous_result"
	}
	if fallbacktargets.ValidateReservationReleaseTransition(current, next, request, now) != nil {
		return fallbacktargets.ProviderTargetReservationV1{}, "lease_lost"
	}
	return next, ""
}

func getFallbackReservationV2(ctx context.Context, provider fallbacktargets.ProviderV2, reservationID string) (fallbacktargets.ProviderTargetReservationV1, string) {
	reservation, err := safeFallbackCallV2(ctx, func(callCtx context.Context) (fallbacktargets.ReservationResultV1, *fallbacktargets.ProviderContractError) {
		return provider.GetReservation(callCtx, fallbacktargets.GetReservationRequestV1{ReservationID: reservationID})
	})
	if err != nil || reservation.Validate() != nil || reservation.ReservationID != reservationID {
		return fallbacktargets.ProviderTargetReservationV1{}, "lease_lost"
	}
	return reservation, ""
}
