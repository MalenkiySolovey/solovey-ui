package fallbacktargets

import (
	canonical "github.com/MalenkiySolovey/solovey-host/componenthost/fallbacktargets"
)

type ProviderErrorClass = canonical.ProviderErrorClass

const (
	ProviderErrorInvalid     = canonical.ProviderErrorInvalid
	ProviderErrorUnavailable = canonical.ProviderErrorUnavailable
	ProviderErrorTimeout     = canonical.ProviderErrorTimeout
	ProviderErrorConflict    = canonical.ProviderErrorConflict
	ProviderErrorStale       = canonical.ProviderErrorStale
	ProviderErrorCapacity    = canonical.ProviderErrorCapacity
	ProviderErrorNotFound    = canonical.ProviderErrorNotFound
	ProviderErrorInternal    = canonical.ProviderErrorInternal
)

type ProviderContractError = canonical.ProviderContractError

type InventoryV2Request = canonical.InventoryV2Request

type InventoryV2Result = canonical.InventoryV2Result

type ResolveV2Result = canonical.ResolveV2Result

type ReserveRequestV1 = canonical.ReserveRequestV1

type ReservationMutationRequestV1 = canonical.ReservationMutationRequestV1

type ReleaseReservationRequestV1 = canonical.ReleaseReservationRequestV1

type GetReservationRequestV1 = canonical.GetReservationRequestV1

type ListReservationsQueryV1 = canonical.ListReservationsQueryV1

type ReservationResultV1 = canonical.ReservationResultV1

type ListReservationsResultV1 = canonical.ListReservationsResultV1

type ProviderV2 = canonical.ProviderV2
