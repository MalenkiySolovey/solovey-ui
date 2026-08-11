package fallbacktargets

import (
	"errors"
	"reflect"
	"time"
)

const (
	ProviderTargetReservationSchemaV1 = "solovey-ui/provider-target-reservation/v1"
	MaxPrepareReservationFreshnessV1  = 5 * time.Minute
	MaxActiveReservationFreshnessV1   = 15 * time.Minute
	MaxReservationClockSkewV1         = 5 * time.Minute
)

type ReservationState string

const (
	ReservationReserved          ReservationState = "RESERVED"
	ReservationMutationPending   ReservationState = "MUTATION_PENDING"
	ReservationActive            ReservationState = "ACTIVE"
	ReservationReconcileRequired ReservationState = "RECONCILE_REQUIRED"
	ReservationReleased          ReservationState = "RELEASED"
)

type ReservationPurpose string

const (
	ReservationPurposeNativeFallback ReservationPurpose = "NATIVE_FALLBACK"
	ReservationPurposeFronting       ReservationPurpose = "FRONTING"
)

type ProviderTargetReservationV1 struct {
	Schema               string                    `json:"schema"`
	ReservationID        string                    `json:"reservationId"`
	ReservationRevision  string                    `json:"reservationRevision"`
	HolderID             string                    `json:"holderId"`
	Purpose              ReservationPurpose        `json:"purpose"`
	ExactTargetReference FallbackTargetReferenceV2 `json:"exactTargetReference"`
	State                ReservationState          `json:"state"`
	IssuedAt             int64                     `json:"issuedAt"`
	RenewedAt            int64                     `json:"renewedAt"`
	FreshnessExpiresAt   int64                     `json:"freshnessExpiresAt"`
	ReleasedAt           int64                     `json:"releasedAt,omitempty"`
	ReasonCodes          []string                  `json:"reasonCodes,omitempty"`
}

type ReservationStatusV1 struct {
	EffectiveState ReservationState `json:"effectiveState"`
	Fresh          bool             `json:"fresh"`
	BlocksMutation bool             `json:"blocksMutation"`
	ReasonCodes    []string         `json:"reasonCodes,omitempty"`
}

type ReservationMutationKind string

const (
	ReservationMutationFence    ReservationMutationKind = "FENCE_FOR_MUTATION"
	ReservationMutationActivate ReservationMutationKind = "ACTIVATE"
	ReservationMutationRenew    ReservationMutationKind = "RENEW"
	ReservationMutationRelease  ReservationMutationKind = "RELEASE"
)

type ReservationCASV1 struct {
	RequestID        string `json:"requestId"`
	ReservationID    string `json:"reservationId"`
	ExpectedRevision string `json:"expectedRevision"`
}

func (reservation ProviderTargetReservationV1) Validate() error {
	if reservation.Schema != ProviderTargetReservationSchemaV1 || !validOpaqueID(reservation.ReservationID, MaxOpaqueIDLengthV2) ||
		!validOpaqueID(reservation.ReservationRevision, MaxOpaqueIDLengthV2) || !validOpaqueID(reservation.HolderID, MaxOpaqueIDLengthV2) ||
		(reservation.Purpose != ReservationPurposeNativeFallback && reservation.Purpose != ReservationPurposeFronting) || reservation.IssuedAt <= 0 || reservation.RenewedAt < reservation.IssuedAt ||
		reservation.FreshnessExpiresAt < reservation.RenewedAt || !validReasonCodesV2(reservation.ReasonCodes) {
		return errors.New("provider_target_reservation_v1_invalid")
	}
	if err := reservation.ExactTargetReference.Validate(); err != nil {
		return errors.New("provider_target_reservation_reference_invalid")
	}
	switch reservation.State {
	case ReservationReserved, ReservationMutationPending:
		if reservation.ReleasedAt != 0 {
			return errors.New("provider_target_reservation_release_time_invalid")
		}
		if reservation.FreshnessExpiresAt-reservation.RenewedAt > int64(MaxPrepareReservationFreshnessV1/time.Second) {
			return errors.New("provider_target_reservation_freshness_invalid")
		}
	case ReservationActive, ReservationReconcileRequired:
		if reservation.ReleasedAt != 0 {
			return errors.New("provider_target_reservation_release_time_invalid")
		}
		if reservation.FreshnessExpiresAt-reservation.RenewedAt > int64(MaxActiveReservationFreshnessV1/time.Second) {
			return errors.New("provider_target_reservation_freshness_invalid")
		}
	case ReservationReleased:
		if reservation.ReleasedAt < reservation.RenewedAt || reservation.ReleasedAt <= 0 {
			return errors.New("provider_target_reservation_release_time_invalid")
		}
		if reservation.FreshnessExpiresAt-reservation.RenewedAt > int64(MaxActiveReservationFreshnessV1/time.Second) {
			return errors.New("provider_target_reservation_freshness_invalid")
		}
	default:
		return errors.New("provider_target_reservation_state_invalid")
	}
	return nil
}

func (reservation ProviderTargetReservationV1) Status(now time.Time) ReservationStatusV1 {
	if reservation.ValidateAt(now) != nil {
		return ReservationStatusV1{EffectiveState: ReservationReconcileRequired, BlocksMutation: true, ReasonCodes: []string{"provider_target_reservation_invalid"}}
	}
	fresh := reservation.FreshnessExpiresAt > now.UTC().Unix()
	switch reservation.State {
	case ReservationReserved:
		if !fresh {
			return ReservationStatusV1{EffectiveState: ReservationReserved, Fresh: false, BlocksMutation: false, ReasonCodes: []string{"provider_target_reservation_expired"}}
		}
		return ReservationStatusV1{EffectiveState: ReservationReserved, Fresh: true, BlocksMutation: true}
	case ReservationMutationPending:
		if !fresh {
			return ReservationStatusV1{EffectiveState: ReservationReconcileRequired, Fresh: false, BlocksMutation: true, ReasonCodes: []string{"provider_target_reservation_reconcile_required"}}
		}
		return ReservationStatusV1{EffectiveState: ReservationMutationPending, Fresh: true, BlocksMutation: true}
	case ReservationActive:
		if !fresh {
			return ReservationStatusV1{EffectiveState: ReservationReconcileRequired, Fresh: false, BlocksMutation: true, ReasonCodes: []string{"provider_target_reservation_reconcile_required"}}
		}
		return ReservationStatusV1{EffectiveState: ReservationActive, Fresh: true, BlocksMutation: true}
	case ReservationReconcileRequired:
		return ReservationStatusV1{EffectiveState: ReservationReconcileRequired, Fresh: fresh, BlocksMutation: true}
	case ReservationReleased:
		return ReservationStatusV1{EffectiveState: ReservationReleased, Fresh: false, BlocksMutation: false}
	default:
		return ReservationStatusV1{EffectiveState: ReservationReconcileRequired, BlocksMutation: true, ReasonCodes: []string{"provider_target_reservation_invalid"}}
	}
}

func (reservation ProviderTargetReservationV1) ValidateAt(now time.Time) error {
	if err := reservation.Validate(); err != nil {
		return err
	}
	latest := now.UTC().Add(MaxReservationClockSkewV1).Unix()
	if reservation.IssuedAt > latest || reservation.RenewedAt > latest || reservation.ReleasedAt > latest {
		return errors.New("provider_target_reservation_time_invalid")
	}
	return nil
}

func ValidateReservationCAS(current ProviderTargetReservationV1, cas ReservationCASV1) error {
	if err := current.Validate(); err != nil {
		return err
	}
	if !validOpaqueID(cas.RequestID, MaxOpaqueIDLengthV2) || cas.ReservationID != current.ReservationID ||
		!validOpaqueID(cas.ExpectedRevision, MaxOpaqueIDLengthV2) || cas.ExpectedRevision != current.ReservationRevision {
		return errors.New("provider_target_reservation_cas_stale")
	}
	return nil
}

func ValidateReservationTransition(current, next ProviderTargetReservationV1, cas ReservationCASV1, mutation ReservationMutationKind, now time.Time) error {
	if mutation == ReservationMutationRelease {
		return errors.New("provider_target_reservation_release_proof_required")
	}
	return validateReservationTransition(current, next, cas, mutation, now)
}

func ValidateReservationReleaseTransition(current, next ProviderTargetReservationV1, request ReleaseReservationRequestV1, now time.Time) error {
	if err := request.Validate(); err != nil {
		return err
	}
	cas := ReservationCASV1{RequestID: request.RequestID, ReservationID: request.ReservationID, ExpectedRevision: request.ExpectedRevision}
	return validateReservationTransition(current, next, cas, ReservationMutationRelease, now)
}

func validateReservationTransition(current, next ProviderTargetReservationV1, cas ReservationCASV1, mutation ReservationMutationKind, now time.Time) error {
	if err := ValidateReservationCAS(current, cas); err != nil {
		return err
	}
	if err := next.ValidateAt(now); err != nil {
		return err
	}
	if current.State == ReservationReleased {
		return errors.New("provider_target_reservation_terminal")
	}
	if mutation != ReservationMutationRelease {
		status := current.Status(now)
		if (current.State == ReservationReserved && !status.Fresh) ||
			((current.State == ReservationMutationPending || current.State == ReservationActive) && status.EffectiveState == ReservationReconcileRequired) {
			return errors.New("provider_target_reservation_reconcile_required")
		}
	}
	if next.ReservationRevision == current.ReservationRevision || next.ReservationID != current.ReservationID || next.HolderID != current.HolderID ||
		next.Purpose != current.Purpose || next.ExactTargetReference != current.ExactTargetReference || next.IssuedAt != current.IssuedAt ||
		next.RenewedAt < current.RenewedAt || next.FreshnessExpiresAt < current.FreshnessExpiresAt {
		return errors.New("provider_target_reservation_transition_conflict")
	}
	want, ok := legalReservationTransition(current.State, mutation)
	if !ok || next.State != want {
		return errors.New("provider_target_reservation_transition_illegal")
	}
	if mutation == ReservationMutationRenew && (next.RenewedAt <= current.RenewedAt || next.FreshnessExpiresAt <= current.FreshnessExpiresAt) {
		return errors.New("provider_target_reservation_renewal_not_advanced")
	}
	if mutation == ReservationMutationRelease {
		if next.ReleasedAt <= 0 {
			return errors.New("provider_target_reservation_release_time_invalid")
		}
	} else if next.ReleasedAt != 0 {
		return errors.New("provider_target_reservation_release_time_invalid")
	}
	return nil
}

func legalReservationTransition(current ReservationState, mutation ReservationMutationKind) (ReservationState, bool) {
	switch mutation {
	case ReservationMutationFence:
		if current == ReservationReserved {
			return ReservationMutationPending, true
		}
	case ReservationMutationActivate:
		if current == ReservationMutationPending {
			return ReservationActive, true
		}
	case ReservationMutationRenew:
		if current == ReservationActive {
			return current, true
		}
	case ReservationMutationRelease:
		// A mutation-pending reservation must remain guarded until the consumer
		// proves the core has detached. Once that proof exists, rollback needs a
		// legal terminal CAS; otherwise an exact rollback can never release the
		// provider authority it deliberately fenced before mutation.
		if current == ReservationReserved || current == ReservationMutationPending || current == ReservationActive || current == ReservationReconcileRequired {
			return ReservationReleased, true
		}
	}
	return "", false
}

func ValidateReconcileTransition(current, next ProviderTargetReservationV1, cas ReservationCASV1, now time.Time) error {
	if err := ValidateReservationCAS(current, cas); err != nil {
		return err
	}
	if err := next.ValidateAt(now); err != nil {
		return err
	}
	if current.State != ReservationReserved && current.State != ReservationMutationPending && current.State != ReservationActive {
		return errors.New("provider_target_reservation_transition_illegal")
	}
	if next.State != ReservationReconcileRequired || next.ReservationRevision == current.ReservationRevision || next.ReservationID != current.ReservationID ||
		next.HolderID != current.HolderID || next.Purpose != current.Purpose || next.ExactTargetReference != current.ExactTargetReference || next.IssuedAt != current.IssuedAt ||
		next.RenewedAt < current.RenewedAt || next.FreshnessExpiresAt < current.FreshnessExpiresAt || next.ReleasedAt != 0 {
		return errors.New("provider_target_reservation_transition_conflict")
	}
	return nil
}

func ValidateIdempotentReservationReplay(previous, replay ProviderTargetReservationV1) error {
	canonicalPrevious, previousError := canonicalReservationV1(previous)
	canonicalReplay, replayError := canonicalReservationV1(replay)
	if previousError != nil || replayError != nil || !reflect.DeepEqual(canonicalPrevious, canonicalReplay) {
		return errors.New("provider_target_reservation_replay_conflict")
	}
	return nil
}

func canonicalReservationV1(reservation ProviderTargetReservationV1) (ProviderTargetReservationV1, error) {
	if err := reservation.Validate(); err != nil {
		return ProviderTargetReservationV1{}, err
	}
	reservation.ReasonCodes = canonicalReasonCodesV2(reservation.ReasonCodes)
	return reservation, nil
}

func ValidateReplayRequest(requestID, previousRequestRevision, replayRequestRevision string, previousResult, replayResult ProviderTargetReservationV1) error {
	if !validOpaqueID(requestID, MaxOpaqueIDLengthV2) || !isSHA256(previousRequestRevision) || !isSHA256(replayRequestRevision) || previousRequestRevision != replayRequestRevision {
		return errors.New("provider_target_reservation_replay_conflict")
	}
	return ValidateIdempotentReservationReplay(previousResult, replayResult)
}

func ValidateReservationCapacity(target FallbackTargetV2, currentReservations uint32, now time.Time) error {
	if err := target.Validate(); err != nil {
		return errors.New("fallback_target_v2_invalid")
	}
	if EffectiveReadinessV2(target.Health, now) != ReadinessReady || len(target.Health.ReasonCodes) != 0 {
		return errors.New("fallback_target_health_not_actionable")
	}
	if EffectiveCapacityStateV2(target.Capacity, now) != CapacityReady || len(target.Capacity.ReasonCodes) != 0 ||
		target.Capacity.ReservationSlotsUsed >= target.Capacity.ReservationSlotsTotal || currentReservations >= target.Capacity.ReservationSlotsTotal || currentReservations >= MaxReservationsV2 {
		return errors.New("provider_target_reservation_capacity_exhausted")
	}
	return nil
}
