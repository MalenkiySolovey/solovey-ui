package nativefallback

import (
	"context"
	"errors"
	"strings"
	"time"

	neutralfallback "github.com/MalenkiySolovey/solovey-host/componenthost/fallbacktargets"
)

// RegistryTargetReader consumes only neutral current facts. The exact
// provider/target identity comes from the caller; inventory ordering never
// selects or substitutes a target.
type RegistryTargetReader struct {
	Registry TargetInventory
	Now      func() time.Time
}

type TargetInventory interface {
	SnapshotV2(context.Context, time.Time) neutralfallback.TargetSnapshotV2
}

func (reader RegistryTargetReader) ResolveV2(ctx context.Context, reference neutralfallback.FallbackTargetReferenceV2) (neutralfallback.FallbackTargetV2, error) {
	if err := ctx.Err(); err != nil {
		return neutralfallback.FallbackTargetV2{}, err
	}
	if err := reference.Validate(); err != nil {
		return neutralfallback.FallbackTargetV2{}, errors.New("target_reference_invalid")
	}
	registry := reader.Registry
	if registry == nil {
		return neutralfallback.FallbackTargetV2{}, errors.New("target_inventory_unavailable")
	}
	now := time.Now().UTC()
	if reader.Now != nil {
		now = reader.Now().UTC()
	}
	snapshot := registry.SnapshotV2(ctx, now)
	for _, target := range snapshot.Targets {
		if target.Identity.ProviderID != reference.ProviderID || target.Identity.TargetID != reference.TargetID {
			continue
		}
		current, err := neutralfallback.ReferenceV2FromTarget(target)
		if err != nil {
			return neutralfallback.FallbackTargetV2{}, errors.New("target_invalid")
		}
		if current != reference {
			return neutralfallback.FallbackTargetV2{}, errors.New("target_reference_stale")
		}
		return target, nil
	}
	for _, reason := range snapshot.ReasonCodes {
		if strings.Contains(reason, "timeout") || strings.Contains(reason, "canceled") || strings.Contains(reason, "unavailable") || strings.Contains(reason, "panicked") || strings.Contains(reason, "truncated") {
			return neutralfallback.FallbackTargetV2{}, errors.New("target_inventory_incomplete")
		}
	}
	return neutralfallback.FallbackTargetV2{}, errors.New("target_missing")
}
