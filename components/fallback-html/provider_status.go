//go:build !minimal

package fallbackhtml

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"time"

	neutral "github.com/MalenkiySolovey/solovey-ui/componenthost/fallbacktargets"
	fallbackapi "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/api"
	"github.com/MalenkiySolovey/solovey-ui/components/fallback-html/authority"
	fallbackservice "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/service"
	"gorm.io/gorm"
)

func fallbackProviderStatus(ctx context.Context, db *gorm.DB, runtime *fallbackservice.Runtime, siteID uint, now time.Time) (fallbackapi.ProviderStatusView, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if db == nil || siteID == 0 {
		return fallbackapi.ProviderStatusView{}, errors.New("provider status is unavailable")
	}
	targetID := "site:" + strconv.FormatUint(uint64(siteID), 10)
	view := fallbackapi.ProviderStatusView{
		TargetID:           targetID,
		EndpointMode:       "UNKNOWN",
		Readiness:          "UNKNOWN",
		HealthFreshness:    "UNKNOWN",
		CapacityState:      "UNKNOWN",
		CapacitySlotsTotal: authority.SlotsPerTarget,
		Reservations:       []fallbackapi.ProviderReservationStateView{},
		ReasonCodes:        []string{},
	}
	provider := targetProvider{db: db, runtime: runtime, now: func() time.Time { return now }}
	inventory, providerErr := provider.InventoryV2(ctx, neutral.InventoryV2Request{Limit: neutral.MaxTargetsV2})
	if providerErr != nil {
		view.ReasonCodes = append(view.ReasonCodes, providerErr.ReasonCode)
	} else {
		view.ReasonCodes = append(view.ReasonCodes, inventory.ReasonCodes...)
		for _, target := range inventory.Targets {
			if target.Identity.TargetID != targetID {
				continue
			}
			view.EndpointMode = string(target.Endpoint.TransportSecurity)
			view.Readiness = string(target.Health.Readiness)
			view.HealthObservedAt = target.Health.ObservedAt
			view.HealthExpiresAt = target.Health.ExpiresAt
			view.HealthFreshness = "FRESH"
			if target.Health.ExpiresAt <= now.Unix() {
				view.HealthFreshness = "STALE"
			}
			view.CapacityState = string(target.Capacity.State)
			view.CapacitySlotsUsed = target.Capacity.ReservationSlotsUsed
			view.CapacitySlotsTotal = target.Capacity.ReservationSlotsTotal
			view.ReasonCodes = append(view.ReasonCodes, target.Health.ReasonCodes...)
			view.ReasonCodes = append(view.ReasonCodes, target.Capacity.ReasonCodes...)
			break
		}
	}
	if view.Readiness == "UNKNOWN" {
		view.ReasonCodes = append(view.ReasonCodes, "target_not_published_or_ready")
	}
	var rows []authority.ReservationModel
	if db.Migrator().HasTable(&authority.ReservationModel{}) {
		if err := db.WithContext(ctx).Where("provider_id = ? AND target_id = ?", authority.ProviderID, targetID).
			Order("reservation_id ASC").Find(&rows).Error; err != nil {
			return fallbackapi.ProviderStatusView{}, err
		}
	}
	counts := map[string]int{}
	for _, row := range rows {
		reservation, err := authority.DecodeReservation(row)
		if err != nil {
			view.InUse = true
			view.ReconcileRequired = true
			view.ReasonCodes = append(view.ReasonCodes, "reservation_authority_invalid")
			continue
		}
		status := reservation.Status(now)
		if !status.BlocksMutation {
			continue
		}
		state := string(status.EffectiveState)
		counts[state]++
		view.InUse = true
		if state == string(neutral.ReservationReconcileRequired) {
			view.ReconcileRequired = true
		}
	}
	states := make([]string, 0, len(counts))
	for state := range counts {
		states = append(states, state)
	}
	sort.Strings(states)
	for _, state := range states {
		view.Reservations = append(view.Reservations, fallbackapi.ProviderReservationStateView{State: state, Count: counts[state]})
	}
	sort.Strings(view.ReasonCodes)
	view.ReasonCodes = compactProviderReasons(view.ReasonCodes)
	return view, nil
}

func compactProviderReasons(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || (len(result) > 0 && result[len(result)-1] == value) {
			continue
		}
		result = append(result, value)
		if len(result) == 16 {
			break
		}
	}
	return result
}
