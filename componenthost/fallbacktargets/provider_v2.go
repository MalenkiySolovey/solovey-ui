package fallbacktargets

import (
	"context"
	"errors"
	"time"
)

type ProviderErrorClass string

const (
	ProviderErrorInvalid     ProviderErrorClass = "INVALID"
	ProviderErrorUnavailable ProviderErrorClass = "UNAVAILABLE"
	ProviderErrorTimeout     ProviderErrorClass = "TIMEOUT"
	ProviderErrorConflict    ProviderErrorClass = "CONFLICT"
	ProviderErrorStale       ProviderErrorClass = "STALE"
	ProviderErrorCapacity    ProviderErrorClass = "CAPACITY"
	ProviderErrorNotFound    ProviderErrorClass = "NOT_FOUND"
	ProviderErrorInternal    ProviderErrorClass = "INTERNAL"
)

type ProviderContractError struct {
	Class      ProviderErrorClass `json:"class"`
	ReasonCode string             `json:"reasonCode"`
}

func (providerError *ProviderContractError) Error() string {
	if providerError == nil {
		return ""
	}
	return "fallback target provider request failed: " + string(providerError.Class)
}

func (providerError *ProviderContractError) Validate() error {
	if providerError == nil {
		return nil
	}
	switch providerError.Class {
	case ProviderErrorInvalid, ProviderErrorUnavailable, ProviderErrorTimeout, ProviderErrorConflict, ProviderErrorStale, ProviderErrorCapacity, ProviderErrorNotFound, ProviderErrorInternal:
	default:
		return errors.New("fallback_target_provider_error_class_invalid")
	}
	if !validSafeToken(providerError.ReasonCode, MaxReasonCodeLengthV2) {
		return errors.New("fallback_target_provider_error_reason_invalid")
	}
	return nil
}

type InventoryV2Request struct {
	Limit uint32 `json:"limit"`
}

type InventoryV2Result struct {
	Targets     []FallbackTargetV2 `json:"targets"`
	Truncated   bool               `json:"truncated"`
	ReasonCodes []string           `json:"reasonCodes,omitempty"`
}

type ResolveV2Result struct {
	Target FallbackTargetV2 `json:"target"`
}

type ReserveRequestV1 struct {
	RequestID             string                    `json:"requestId"`
	HolderID              string                    `json:"holderId"`
	Purpose               ReservationPurpose        `json:"purpose"`
	ExactTargetReference  FallbackTargetReferenceV2 `json:"exactTargetReference"`
	FreshnessDurationSecs uint32                    `json:"freshnessDurationSeconds"`
}

type ReservationMutationRequestV1 struct {
	RequestID             string `json:"requestId"`
	ReservationID         string `json:"reservationId"`
	ExpectedRevision      string `json:"expectedRevision"`
	FreshnessDurationSecs uint32 `json:"freshnessDurationSeconds,omitempty"`
}

type ReleaseReservationRequestV1 struct {
	RequestID                string `json:"requestId"`
	ReservationID            string `json:"reservationId"`
	ExpectedRevision         string `json:"expectedRevision"`
	VerifiedDetachedRevision string `json:"verifiedDetachedRevision"`
}

type GetReservationRequestV1 struct {
	ReservationID string `json:"reservationId"`
}

type ListReservationsQueryV1 struct {
	ProviderID   string           `json:"providerId,omitempty"`
	TargetID     string           `json:"targetId,omitempty"`
	HolderID     string           `json:"holderId,omitempty"`
	State        ReservationState `json:"state,omitempty"`
	Limit        uint32           `json:"limit"`
	Continuation string           `json:"continuation,omitempty"`
}

type ReservationResultV1 struct {
	Reservation ProviderTargetReservationV1 `json:"reservation"`
}

type ListReservationsResultV1 struct {
	Reservations []ProviderTargetReservationV1 `json:"reservations"`
	Continuation string                        `json:"continuation,omitempty"`
	Truncated    bool                          `json:"truncated"`
	ReasonCodes  []string                      `json:"reasonCodes,omitempty"`
}

type ProviderV2 interface {
	ProviderID() string
	InventoryV2(context.Context, InventoryV2Request) (InventoryV2Result, *ProviderContractError)
	ResolveV2(context.Context, FallbackTargetReferenceV2) (ResolveV2Result, *ProviderContractError)
	Reserve(context.Context, ReserveRequestV1) (ReservationResultV1, *ProviderContractError)
	FenceForMutation(context.Context, ReservationMutationRequestV1) (ReservationResultV1, *ProviderContractError)
	Activate(context.Context, ReservationMutationRequestV1) (ReservationResultV1, *ProviderContractError)
	Renew(context.Context, ReservationMutationRequestV1) (ReservationResultV1, *ProviderContractError)
	Release(context.Context, ReleaseReservationRequestV1) (ReservationResultV1, *ProviderContractError)
	GetReservation(context.Context, GetReservationRequestV1) (ReservationResultV1, *ProviderContractError)
	ListReservations(context.Context, ListReservationsQueryV1) (ListReservationsResultV1, *ProviderContractError)
}

func (request InventoryV2Request) Validate() error {
	if request.Limit == 0 || request.Limit > MaxTargetsV2 {
		return errors.New("fallback_target_inventory_v2_limit_invalid")
	}
	return nil
}

func (request ReserveRequestV1) Validate() error {
	if !validOpaqueID(request.RequestID, MaxOpaqueIDLengthV2) || !validOpaqueID(request.HolderID, MaxOpaqueIDLengthV2) ||
		(request.Purpose != ReservationPurposeNativeFallback && request.Purpose != ReservationPurposeFronting) ||
		request.FreshnessDurationSecs == 0 || request.FreshnessDurationSecs > uint32(MaxPrepareReservationFreshnessV1/time.Second) {
		return errors.New("provider_target_reserve_request_invalid")
	}
	return request.ExactTargetReference.Validate()
}

func (request ReservationMutationRequestV1) Validate(requireFreshness bool) error {
	if !validOpaqueID(request.RequestID, MaxOpaqueIDLengthV2) || !validOpaqueID(request.ReservationID, MaxOpaqueIDLengthV2) || !validOpaqueID(request.ExpectedRevision, MaxOpaqueIDLengthV2) {
		return errors.New("provider_target_reservation_mutation_request_invalid")
	}
	if requireFreshness {
		if request.FreshnessDurationSecs == 0 || request.FreshnessDurationSecs > uint32(MaxActiveReservationFreshnessV1/time.Second) {
			return errors.New("provider_target_reservation_freshness_invalid")
		}
	} else if request.FreshnessDurationSecs != 0 {
		return errors.New("provider_target_reservation_freshness_invalid")
	}
	return nil
}

func (request ReleaseReservationRequestV1) Validate() error {
	if !validOpaqueID(request.RequestID, MaxOpaqueIDLengthV2) || !validOpaqueID(request.ReservationID, MaxOpaqueIDLengthV2) ||
		!validOpaqueID(request.ExpectedRevision, MaxOpaqueIDLengthV2) || !isSHA256(request.VerifiedDetachedRevision) {
		return errors.New("provider_target_reservation_release_request_invalid")
	}
	return nil
}

func (query ListReservationsQueryV1) Validate() error {
	if query.Limit == 0 || query.Limit > MaxReservationListPageV2 || (query.ProviderID != "" && !validOpaqueID(query.ProviderID, MaxOpaqueIDLengthV2)) ||
		(query.TargetID != "" && !validOpaqueID(query.TargetID, MaxOpaqueIDLengthV2)) || (query.HolderID != "" && !validOpaqueID(query.HolderID, MaxOpaqueIDLengthV2)) ||
		(query.Continuation != "" && !validOpaqueID(query.Continuation, MaxOpaqueIDLengthV2)) {
		return errors.New("provider_target_reservation_list_query_invalid")
	}
	if query.Continuation != "" && query.ProviderID == "" {
		return errors.New("provider_target_reservation_continuation_provider_required")
	}
	if query.State != "" {
		switch query.State {
		case ReservationReserved, ReservationMutationPending, ReservationActive, ReservationReconcileRequired, ReservationReleased:
		default:
			return errors.New("provider_target_reservation_list_state_invalid")
		}
	}
	return nil
}
