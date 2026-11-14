package fallbacktargets

import (
	canonical "github.com/MalenkiySolovey/solovey-host/componenthost/fallbacktargets"
	"time"
)

const (
	ProviderTargetReservationSchemaV1 = canonical.ProviderTargetReservationSchemaV1
	MaxPrepareReservationFreshnessV1  = canonical.MaxPrepareReservationFreshnessV1
	MaxActiveReservationFreshnessV1   = canonical.MaxActiveReservationFreshnessV1
	MaxReservationClockSkewV1         = canonical.MaxReservationClockSkewV1
)

type ReservationState = canonical.ReservationState

const (
	ReservationReserved          = canonical.ReservationReserved
	ReservationMutationPending   = canonical.ReservationMutationPending
	ReservationActive            = canonical.ReservationActive
	ReservationReconcileRequired = canonical.ReservationReconcileRequired
	ReservationReleased          = canonical.ReservationReleased
)

type ReservationPurpose = canonical.ReservationPurpose

const (
	ReservationPurposeNativeFallback = canonical.ReservationPurposeNativeFallback
	ReservationPurposeFronting       = canonical.ReservationPurposeFronting
)

type ProviderTargetReservationV1 = canonical.ProviderTargetReservationV1

type ReservationStatusV1 = canonical.ReservationStatusV1

type ReservationMutationKind = canonical.ReservationMutationKind

const (
	ReservationMutationFence    = canonical.ReservationMutationFence
	ReservationMutationActivate = canonical.ReservationMutationActivate
	ReservationMutationRenew    = canonical.ReservationMutationRenew
	ReservationMutationRelease  = canonical.ReservationMutationRelease
)

type ReservationCASV1 = canonical.ReservationCASV1

func ValidateReservationCAS(current ProviderTargetReservationV1, cas ReservationCASV1) error {
	return canonical.ValidateReservationCAS(current, cas)
}

func ValidateReservationTransition(current, next ProviderTargetReservationV1, cas ReservationCASV1, mutation ReservationMutationKind, now time.Time) error {
	return canonical.ValidateReservationTransition(current, next, cas, mutation, now)
}

func ValidateReservationReleaseTransition(current, next ProviderTargetReservationV1, request ReleaseReservationRequestV1, now time.Time) error {
	return canonical.ValidateReservationReleaseTransition(current, next, request, now)
}

func ValidateReconcileTransition(current, next ProviderTargetReservationV1, cas ReservationCASV1, now time.Time) error {
	return canonical.ValidateReconcileTransition(current, next, cas, now)
}

func ValidateIdempotentReservationReplay(previous, replay ProviderTargetReservationV1) error {
	return canonical.ValidateIdempotentReservationReplay(previous, replay)
}

func ValidateReplayRequest(requestID, previousRequestRevision, replayRequestRevision string, previousResult, replayResult ProviderTargetReservationV1) error {
	return canonical.ValidateReplayRequest(requestID, previousRequestRevision, replayRequestRevision, previousResult, replayResult)
}

func ValidateReservationCapacity(target FallbackTargetV2, currentReservations uint32, now time.Time) error {
	return canonical.ValidateReservationCapacity(target, currentReservations, now)
}

func canonicalReservationV1(value ProviderTargetReservationV1) (ProviderTargetReservationV1, error) {
	return canonical.CanonicalReservationV1(value)
}
