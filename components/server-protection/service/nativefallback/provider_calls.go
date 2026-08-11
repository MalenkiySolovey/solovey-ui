package nativefallback

import (
	"context"
	"errors"

	neutralfallback "github.com/MalenkiySolovey/solovey-ui/componenthost/fallbacktargets"
)

func providerCallError(contractError *neutralfallback.ProviderContractError, panicked bool) error {
	if panicked {
		return &WorkflowError{Code: "provider_panicked"}
	}
	if contractError == nil {
		return nil
	}
	if contractError.Validate() != nil {
		return &WorkflowError{Code: "provider_error_invalid"}
	}
	return &WorkflowError{Code: "provider_" + string(contractError.Class)}
}

func reserveProvider(ctx context.Context, provider neutralfallback.ProviderV2, request neutralfallback.ReserveRequestV1) (result neutralfallback.ProviderTargetReservationV1, err error) {
	callCtx, cancel := context.WithTimeout(ctx, providerCallTimeout)
	defer cancel()
	panicked := false
	var response neutralfallback.ReservationResultV1
	var contractError *neutralfallback.ProviderContractError
	func() {
		defer func() { panicked = recover() != nil }()
		response, contractError = provider.Reserve(callCtx, request)
	}()
	if err = providerCallError(contractError, panicked); err != nil || callCtx.Err() != nil {
		if callCtx.Err() != nil {
			return result, &WorkflowError{Code: "provider_timeout"}
		}
		return result, err
	}
	if response.Reservation.Validate() != nil {
		return result, &WorkflowError{Code: "provider_reservation_invalid"}
	}
	return response.Reservation, nil
}

func fenceProvider(ctx context.Context, provider neutralfallback.ProviderV2, request neutralfallback.ReservationMutationRequestV1) (result neutralfallback.ProviderTargetReservationV1, err error) {
	return mutateProvider(ctx, func(callCtx context.Context) (neutralfallback.ReservationResultV1, *neutralfallback.ProviderContractError) {
		return provider.FenceForMutation(callCtx, request)
	})
}

func activateProvider(ctx context.Context, provider neutralfallback.ProviderV2, request neutralfallback.ReservationMutationRequestV1) (result neutralfallback.ProviderTargetReservationV1, err error) {
	return mutateProvider(ctx, func(callCtx context.Context) (neutralfallback.ReservationResultV1, *neutralfallback.ProviderContractError) {
		return provider.Activate(callCtx, request)
	})
}

func mutateProvider(ctx context.Context, call func(context.Context) (neutralfallback.ReservationResultV1, *neutralfallback.ProviderContractError)) (result neutralfallback.ProviderTargetReservationV1, err error) {
	callCtx, cancel := context.WithTimeout(ctx, providerCallTimeout)
	defer cancel()
	panicked := false
	var response neutralfallback.ReservationResultV1
	var contractError *neutralfallback.ProviderContractError
	func() {
		defer func() { panicked = recover() != nil }()
		response, contractError = call(callCtx)
	}()
	if err = providerCallError(contractError, panicked); err != nil || callCtx.Err() != nil {
		if callCtx.Err() != nil {
			return result, &WorkflowError{Code: "provider_timeout"}
		}
		return result, err
	}
	if response.Reservation.Validate() != nil {
		return result, &WorkflowError{Code: "provider_reservation_invalid"}
	}
	return response.Reservation, nil
}

func releaseProvider(ctx context.Context, provider neutralfallback.ProviderV2, request neutralfallback.ReleaseReservationRequestV1) (result neutralfallback.ProviderTargetReservationV1, err error) {
	callCtx, cancel := context.WithTimeout(ctx, providerCallTimeout)
	defer cancel()
	panicked := false
	var response neutralfallback.ReservationResultV1
	var contractError *neutralfallback.ProviderContractError
	func() {
		defer func() { panicked = recover() != nil }()
		response, contractError = provider.Release(callCtx, request)
	}()
	if err = providerCallError(contractError, panicked); err != nil || callCtx.Err() != nil {
		if callCtx.Err() != nil {
			return result, &WorkflowError{Code: "provider_timeout"}
		}
		return result, err
	}
	if response.Reservation.Validate() != nil {
		return result, &WorkflowError{Code: "provider_reservation_invalid"}
	}
	return response.Reservation, nil
}

func getProviderReservation(ctx context.Context, provider neutralfallback.ProviderV2, reservationID string) (result neutralfallback.ProviderTargetReservationV1, err error) {
	callCtx, cancel := context.WithTimeout(ctx, providerCallTimeout)
	defer cancel()
	panicked := false
	var response neutralfallback.ReservationResultV1
	var contractError *neutralfallback.ProviderContractError
	func() {
		defer func() { panicked = recover() != nil }()
		response, contractError = provider.GetReservation(callCtx, neutralfallback.GetReservationRequestV1{ReservationID: reservationID})
	}()
	if err = providerCallError(contractError, panicked); err != nil || callCtx.Err() != nil {
		if callCtx.Err() != nil {
			return result, &WorkflowError{Code: "provider_timeout"}
		}
		return result, err
	}
	if response.Reservation.Validate() != nil {
		return result, &WorkflowError{Code: "provider_reservation_invalid"}
	}
	return response.Reservation, nil
}

func providerInventory(ctx context.Context, provider neutralfallback.ProviderV2) (result neutralfallback.InventoryV2Result, err error) {
	callCtx, cancel := context.WithTimeout(ctx, providerCallTimeout)
	defer cancel()
	panicked := false
	var contractError *neutralfallback.ProviderContractError
	func() {
		defer func() { panicked = recover() != nil }()
		result, contractError = provider.InventoryV2(callCtx, neutralfallback.InventoryV2Request{Limit: neutralfallback.MaxTargetsV2})
	}()
	if err = providerCallError(contractError, panicked); err != nil || callCtx.Err() != nil {
		if callCtx.Err() != nil {
			return result, &WorkflowError{Code: "provider_timeout"}
		}
		return result, err
	}
	return result, nil
}

func resolveProvider(ctx context.Context, provider neutralfallback.ProviderV2, reference neutralfallback.FallbackTargetReferenceV2) (target neutralfallback.FallbackTargetV2, err error) {
	callCtx, cancel := context.WithTimeout(ctx, providerCallTimeout)
	defer cancel()
	panicked := false
	var response neutralfallback.ResolveV2Result
	var contractError *neutralfallback.ProviderContractError
	func() {
		defer func() { panicked = recover() != nil }()
		response, contractError = provider.ResolveV2(callCtx, reference)
	}()
	if err = providerCallError(contractError, panicked); err != nil || callCtx.Err() != nil {
		if callCtx.Err() != nil {
			return target, &WorkflowError{Code: "provider_timeout"}
		}
		return target, err
	}
	if response.Target.Validate() != nil {
		return target, errors.New("provider target invalid")
	}
	return response.Target, nil
}
