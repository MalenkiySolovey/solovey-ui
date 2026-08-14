package fallbacktargets

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

type RegistrySnapshotV2 struct {
	GeneratedAt int64              `json:"generatedAt"`
	Targets     []FallbackTargetV2 `json:"targets"`
	Truncated   bool               `json:"truncated"`
	ReasonCodes []string           `json:"reasonCodes,omitempty"`
}

type RegistryReservationsV2 struct {
	Reservations []ProviderTargetReservationV1 `json:"reservations"`
	Continuation string                        `json:"continuation,omitempty"`
	Truncated    bool                          `json:"truncated"`
	ReasonCodes  []string                      `json:"reasonCodes,omitempty"`
}

func (r *Registry) RegisterV2(provider ProviderV2) (func(), error) {
	if provider == nil {
		return func() {}, errors.New("fallback_target_provider_v2_invalid")
	}
	providerID, panicked := safeProviderIDV2(provider)
	if panicked || !validOpaqueID(providerID, MaxOpaqueIDLengthV2) {
		return func() {}, errors.New("fallback_target_provider_v2_invalid")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.v2Providers) >= MaxProvidersV2 {
		return func() {}, errors.New("fallback_target_provider_v2_capacity_exceeded")
	}
	for _, registered := range r.v2Providers {
		if registered.id == providerID {
			return func() {}, errors.New("duplicate_fallback_target_provider_v2_id")
		}
	}
	id := r.next
	r.next++
	r.v2Providers[id] = providerV2Entry{id: providerID, provider: provider}
	var once sync.Once
	return func() { once.Do(func() { r.mu.Lock(); delete(r.v2Providers, id); r.mu.Unlock() }) }, nil
}

// ProviderV2 returns the exact neutral provider registered under providerID.
// The returned contract remains the provider authority; the registry does not
// cache, proxy, or reconstruct reservation state.
func (r *Registry) ProviderV2(providerID string) (ProviderV2, bool) {
	if r == nil || !validOpaqueID(providerID, MaxOpaqueIDLengthV2) {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, entry := range r.v2Providers {
		if entry.id == providerID {
			return entry.provider, true
		}
	}
	return nil, false
}

func (r *Registry) SnapshotV2(ctx context.Context, now time.Time) RegistrySnapshotV2 {
	r.mu.RLock()
	providers := make([]providerV2Entry, 0, len(r.v2Providers))
	for _, entry := range r.v2Providers {
		providers = append(providers, entry)
	}
	r.mu.RUnlock()
	sort.Slice(providers, func(i, j int) bool {
		return providers[i].id < providers[j].id
	})
	result := RegistrySnapshotV2{GeneratedAt: now.UTC().Unix(), Targets: []FallbackTargetV2{}}
	if len(providers) == 0 {
		result.ReasonCodes = []string{"fallback_target_provider_v2_absent"}
		return result
	}
	targetsSeen := make(map[string]struct{}, min(MaxTargetsV2, len(providers)))
	endpointsSeen := make(map[string]struct{}, min(MaxTargetsV2, len(providers)))
	for _, entry := range providers {
		if err := ctx.Err(); err != nil {
			result.ReasonCodes = append(result.ReasonCodes, registryContextReasonV2(err))
			break
		}
		if len(result.Targets) >= MaxTargetsV2 {
			result.Truncated = true
			result.ReasonCodes = append(result.ReasonCodes, "fallback_target_inventory_v2_truncated")
			break
		}
		providerID, provider := entry.id, entry.provider
		providerCtx, cancel := context.WithTimeout(ctx, DefaultProviderTimeoutV2)
		inventory, providerError, inventoryPanicked := safeInventoryV2(providerCtx, provider, InventoryV2Request{Limit: uint32(MaxTargetsV2 - len(result.Targets))})
		deadlineError := providerCtx.Err()
		cancel()
		if inventoryPanicked {
			result.ReasonCodes = append(result.ReasonCodes, "fallback_target_provider_v2_panicked")
			continue
		}
		if deadlineError != nil {
			result.ReasonCodes = append(result.ReasonCodes, registryContextReasonV2(deadlineError))
			continue
		}
		if providerError != nil {
			if providerError.Validate() != nil {
				result.ReasonCodes = append(result.ReasonCodes, "fallback_target_provider_v2_error_invalid")
			} else if providerError.Class == ProviderErrorTimeout {
				result.ReasonCodes = append(result.ReasonCodes, "fallback_target_provider_v2_timeout")
			} else {
				result.ReasonCodes = append(result.ReasonCodes, "fallback_target_provider_v2_unavailable")
			}
			continue
		}
		if inventory.Truncated {
			result.Truncated = true
			result.ReasonCodes = append(result.ReasonCodes, "fallback_target_inventory_v2_truncated")
		}
		if !validReasonCodesV2(inventory.ReasonCodes) {
			result.ReasonCodes = append(result.ReasonCodes, "fallback_target_provider_v2_reasons_invalid")
		} else {
			result.ReasonCodes = append(result.ReasonCodes, inventory.ReasonCodes...)
		}
		remaining := MaxTargetsV2 - len(result.Targets)
		if len(inventory.Targets) > remaining {
			inventory.Targets = inventory.Targets[:remaining]
			result.Truncated = true
			result.ReasonCodes = append(result.ReasonCodes, "fallback_target_inventory_v2_truncated")
		}
		for _, target := range inventory.Targets {
			canonicalTarget, validationError := validatedCanonicalTargetV2(target)
			if target.Identity.ProviderID != providerID || validationError != nil {
				result.ReasonCodes = append(result.ReasonCodes, "fallback_target_record_v2_invalid")
				continue
			}
			target = canonicalTarget
			targetKey := target.Identity.ProviderID + "\x00" + target.Identity.TargetID
			if _, duplicate := targetsSeen[targetKey]; duplicate {
				result.ReasonCodes = append(result.ReasonCodes, "duplicate_fallback_target_v2_id")
				continue
			}
			endpointKey := target.Identity.ProviderID + "\x00" + target.Endpoint.EndpointID
			if _, duplicate := endpointsSeen[endpointKey]; duplicate {
				result.ReasonCodes = append(result.ReasonCodes, "duplicate_fallback_endpoint_v2_id")
				continue
			}
			targetsSeen[targetKey] = struct{}{}
			endpointsSeen[endpointKey] = struct{}{}
			result.Targets = append(result.Targets, target)
		}
	}
	sort.Slice(result.Targets, func(i, j int) bool {
		left := result.Targets[i].Identity.ProviderID + "\x00" + result.Targets[i].Identity.TargetID
		right := result.Targets[j].Identity.ProviderID + "\x00" + result.Targets[j].Identity.TargetID
		return left < right
	})
	result.ReasonCodes = canonicalRegistryReasonsV2(result.ReasonCodes)
	return result
}

func (r *Registry) ResolveV2(ctx context.Context, reference FallbackTargetReferenceV2, now time.Time) (FallbackTargetV2, error) {
	if err := reference.Validate(); err != nil {
		return FallbackTargetV2{}, err
	}
	snapshot := r.SnapshotV2(ctx, now)
	for _, target := range snapshot.Targets {
		if target.Identity.ProviderID == reference.ProviderID && target.Identity.TargetID == reference.TargetID {
			if err := ResolveExactV2(reference, target, now); err != nil {
				return FallbackTargetV2{}, err
			}
			return target, nil
		}
	}
	for _, reason := range snapshot.ReasonCodes {
		if strings.Contains(reason, "timeout") || strings.Contains(reason, "canceled") || strings.Contains(reason, "unavailable") || strings.Contains(reason, "panicked") || strings.Contains(reason, "truncated") {
			return FallbackTargetV2{}, errors.New("fallback_target_inventory_v2_incomplete")
		}
	}
	return FallbackTargetV2{}, errors.New("fallback_target_v2_missing")
}

func (r *Registry) ListReservationsV2(ctx context.Context, query ListReservationsQueryV1) (RegistryReservationsV2, error) {
	if err := query.Validate(); err != nil {
		return RegistryReservationsV2{}, err
	}
	r.mu.RLock()
	providers := make([]providerV2Entry, 0, len(r.v2Providers))
	for _, entry := range r.v2Providers {
		if query.ProviderID == "" || entry.id == query.ProviderID {
			providers = append(providers, entry)
		}
	}
	r.mu.RUnlock()
	sort.Slice(providers, func(i, j int) bool {
		return providers[i].id < providers[j].id
	})
	result := RegistryReservationsV2{Reservations: []ProviderTargetReservationV1{}}
	seen := make(map[string]struct{}, min(MaxReservationsV2, len(providers)))
	resultLimit := min(int(query.Limit), MaxReservationsV2)
	for _, entry := range providers {
		if err := ctx.Err(); err != nil {
			result.ReasonCodes = append(result.ReasonCodes, registryContextReasonV2(err))
			break
		}
		if len(result.Reservations) >= resultLimit {
			result.Truncated = true
			result.ReasonCodes = append(result.ReasonCodes, "provider_target_reservation_inventory_truncated")
			break
		}
		providerID, provider := entry.id, entry.provider
		providerQuery := query
		providerQuery.ProviderID = providerID
		providerQuery.Limit = uint32(resultLimit - len(result.Reservations))
		providerCtx, cancel := context.WithTimeout(ctx, DefaultProviderTimeoutV2)
		page, providerError, listPanicked := safeListReservationsV2(providerCtx, provider, providerQuery)
		deadlineError := providerCtx.Err()
		cancel()
		if listPanicked {
			result.ReasonCodes = append(result.ReasonCodes, "fallback_target_provider_v2_panicked")
			continue
		}
		if deadlineError != nil {
			result.ReasonCodes = append(result.ReasonCodes, registryContextReasonV2(deadlineError))
			continue
		}
		if providerError != nil && providerError.Class == ProviderErrorTimeout {
			result.ReasonCodes = append(result.ReasonCodes, "fallback_target_provider_v2_timeout")
			continue
		}
		if providerError != nil {
			if providerError.Validate() != nil {
				result.ReasonCodes = append(result.ReasonCodes, "fallback_target_provider_v2_error_invalid")
			} else {
				result.ReasonCodes = append(result.ReasonCodes, "fallback_target_provider_v2_unavailable")
			}
			continue
		}
		if page.Truncated || page.Continuation != "" {
			result.Truncated = true
			result.ReasonCodes = append(result.ReasonCodes, "provider_target_reservation_inventory_truncated")
		}
		if page.Continuation != "" {
			if len(providers) == 1 && validOpaqueID(page.Continuation, MaxOpaqueIDLengthV2) {
				result.Continuation = page.Continuation
			} else {
				result.ReasonCodes = append(result.ReasonCodes, "provider_target_reservation_continuation_invalid")
			}
		}
		if !validReasonCodesV2(page.ReasonCodes) {
			result.ReasonCodes = append(result.ReasonCodes, "provider_target_reservation_reasons_invalid")
		} else {
			result.ReasonCodes = append(result.ReasonCodes, page.ReasonCodes...)
		}
		pageLimit := resultLimit - len(result.Reservations)
		if len(page.Reservations) > pageLimit {
			page.Reservations = page.Reservations[:pageLimit]
			result.Truncated = true
			result.ReasonCodes = append(result.ReasonCodes, "provider_target_reservation_inventory_truncated")
		}
		for _, reservation := range page.Reservations {
			if len(result.Reservations) >= resultLimit {
				result.Truncated = true
				result.ReasonCodes = append(result.ReasonCodes, "provider_target_reservation_inventory_truncated")
				break
			}
			canonicalReservation, validationError := canonicalReservationV1(reservation)
			if validationError != nil || reservation.ExactTargetReference.ProviderID != providerID || !reservationMatchesQueryV1(reservation, query) {
				result.ReasonCodes = append(result.ReasonCodes, "provider_target_reservation_record_invalid")
				continue
			}
			reservation = canonicalReservation
			key := providerID + "\x00" + reservation.ReservationID
			if _, duplicate := seen[key]; duplicate {
				result.ReasonCodes = append(result.ReasonCodes, "duplicate_provider_target_reservation_id")
				continue
			}
			seen[key] = struct{}{}
			result.Reservations = append(result.Reservations, reservation)
		}
	}
	sort.Slice(result.Reservations, func(i, j int) bool {
		left := result.Reservations[i].ExactTargetReference.ProviderID + "\x00" + result.Reservations[i].ReservationID
		right := result.Reservations[j].ExactTargetReference.ProviderID + "\x00" + result.Reservations[j].ReservationID
		return left < right
	})
	result.ReasonCodes = canonicalRegistryReasonsV2(result.ReasonCodes)
	return result, nil
}

func safeProviderIDV2(provider ProviderV2) (providerID string, panicked bool) {
	defer func() {
		if recover() != nil {
			providerID = ""
			panicked = true
		}
	}()
	return strings.TrimSpace(provider.ProviderID()), false
}

func safeInventoryV2(ctx context.Context, provider ProviderV2, request InventoryV2Request) (inventory InventoryV2Result, providerError *ProviderContractError, panicked bool) {
	type response struct {
		inventory     InventoryV2Result
		providerError *ProviderContractError
		panicked      bool
	}
	result := make(chan response, 1)
	go func() {
		value := response{}
		defer func() {
			if recover() != nil {
				value = response{panicked: true}
			}
			result <- value
		}()
		value.inventory, value.providerError = provider.InventoryV2(ctx, request)
	}()
	select {
	case value := <-result:
		return value.inventory, value.providerError, value.panicked
	case <-ctx.Done():
		return InventoryV2Result{}, nil, false
	}
}

func safeListReservationsV2(ctx context.Context, provider ProviderV2, query ListReservationsQueryV1) (result ListReservationsResultV1, providerError *ProviderContractError, panicked bool) {
	type response struct {
		result        ListReservationsResultV1
		providerError *ProviderContractError
		panicked      bool
	}
	resultCh := make(chan response, 1)
	go func() {
		value := response{}
		defer func() {
			if recover() != nil {
				value = response{panicked: true}
			}
			resultCh <- value
		}()
		value.result, value.providerError = provider.ListReservations(ctx, query)
	}()
	select {
	case value := <-resultCh:
		return value.result, value.providerError, value.panicked
	case <-ctx.Done():
		return ListReservationsResultV1{}, nil, false
	}
}

func canonicalRegistryReasonsV2(values []string) []string {
	seen := make(map[string]struct{}, min(len(values), MaxReasonCodesV2))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if !validSafeToken(value, MaxReasonCodeLengthV2) {
			value = "fallback_target_registry_v2_reason_invalid"
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	if len(result) > MaxReasonCodesV2 {
		result = result[:MaxReasonCodesV2]
	}
	return result
}

func registryContextReasonV2(err error) string {
	if errors.Is(err, context.Canceled) {
		return "fallback_target_registry_v2_canceled"
	}
	return "fallback_target_provider_v2_timeout"
}

func reservationMatchesQueryV1(reservation ProviderTargetReservationV1, query ListReservationsQueryV1) bool {
	return (query.ProviderID == "" || reservation.ExactTargetReference.ProviderID == query.ProviderID) &&
		(query.TargetID == "" || reservation.ExactTargetReference.TargetID == query.TargetID) &&
		(query.HolderID == "" || reservation.HolderID == query.HolderID) &&
		(query.State == "" || reservation.State == query.State)
}
