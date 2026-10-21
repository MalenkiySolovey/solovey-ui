//go:build !minimal

package remote

import (
	"encoding/json"
	"errors"

	subcanonical "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/canonical"
	"gorm.io/gorm"
)

var errRefreshIdentityAmbiguous = errors.New("MANUAL_MAPPING_REQUIRED: source endpoint identity is ambiguous")

// A refresh-local plan reserves existing semantic identities before any label
// fallback can reuse them. SourceKey remains a compatible opaque source locator;
// EndpointIR, rather than a display label, decides endpoint continuity.
type endpointRefreshPlan struct {
	matches  map[int]RemoteOutboundConnection
	reserved map[uint]int
	claimed  map[uint]bool
	occupied map[string]struct{}
}

func newEndpointRefreshPlan(tx *gorm.DB, subscriptionID uint, outbounds []map[string]any, snapshot subcanonical.Snapshot) (*endpointRefreshPlan, error) {
	plan := &endpointRefreshPlan{matches: map[int]RemoteOutboundConnection{}, reserved: map[uint]int{}, claimed: map[uint]bool{}, occupied: map[string]struct{}{}}
	var existing []RemoteOutboundConnection
	if err := tx.Where("subscription_id = ?", subscriptionID).Order("id").Find(&existing).Error; err != nil {
		return nil, err
	}
	oldByEndpoint := map[string][]RemoteOutboundConnection{}
	for _, row := range existing {
		plan.occupied[row.SourceKey] = struct{}{}
		endpoint := canonicalConnection(row.Canonical)
		if endpoint == nil || len(endpoint.BestOutbound) == 0 {
			raw := map[string]any{}
			if json.Unmarshal(row.Options, &raw) != nil {
				continue
			}
			raw["type"], raw["tag"] = row.Type, row.Name
			value := subcanonical.ObserveOutbound(subcanonical.FormatSingBox, raw, 0)
			endpoint = &value
		}
		if id := refreshEndpointID(*endpoint); id != "" {
			oldByEndpoint[id] = append(oldByEndpoint[id], row)
		}
	}
	wanted := map[string][]int{}
	normalized := make([]NormalizedOutbound, len(outbounds))
	for index, raw := range outbounds {
		value, err := NormalizeOutboundWithCanonical(raw, index, canonicalConnectionAt(snapshot, index))
		if err != nil {
			return nil, err
		}
		normalized[index] = value
		if endpoint := canonicalConnection(value.Canonical); endpoint != nil {
			if id := refreshEndpointID(*endpoint); id != "" {
				wanted[id] = append(wanted[id], index)
			}
		}
	}
	claim := func(index int, row RemoteOutboundConnection) {
		plan.matches[index], plan.reserved[row.Id] = row, index
	}
	for id, indices := range wanted {
		rows := oldByEndpoint[id]
		oldByName := map[string][]RemoteOutboundConnection{}
		newByName := map[string][]int{}
		oldByLocator := map[string]RemoteOutboundConnection{}
		newByLocator := map[string][]int{}
		for _, row := range rows {
			name := subcanonical.NormalizeLabel(row.Name)
			oldByName[name] = append(oldByName[name], row)
			oldByLocator[row.SourceKey] = row
		}
		for _, index := range indices {
			name := subcanonical.NormalizeLabel(normalized[index].Name)
			newByName[name] = append(newByName[name], index)
			newByLocator[normalized[index].SourceKey] = append(newByLocator[normalized[index].SourceKey], index)
		}
		for name, targets := range newByName {
			if candidates := oldByName[name]; len(candidates) == 1 && len(targets) == 1 {
				claim(targets[0], candidates[0])
			}
		}
		// Prefer a current locator over an obsolete typed-label duplicate, as
		// the existing legacy reconciliation did before EndpointIR.
		for locator, targets := range newByLocator {
			if len(targets) != 1 {
				continue
			}
			index := targets[0]
			if _, assigned := plan.matches[index]; assigned {
				continue
			}
			if row, ok := oldByLocator[locator]; ok {
				if _, used := plan.reserved[row.Id]; !used {
					claim(index, row)
				}
			}
		}
		remainingRows := []RemoteOutboundConnection{}
		remainingIndices := []int{}
		for _, row := range rows {
			if _, used := plan.reserved[row.Id]; !used {
				remainingRows = append(remainingRows, row)
			}
		}
		for _, index := range indices {
			if _, used := plan.matches[index]; !used {
				remainingIndices = append(remainingIndices, index)
			}
		}
		if len(remainingRows) == 1 && len(remainingIndices) == 1 {
			claim(remainingIndices[0], remainingRows[0])
		} else if len(remainingRows) > 0 && len(remainingIndices) > 0 {
			return nil, errRefreshIdentityAmbiguous
		}
	}
	return plan, nil
}

func refreshEndpointID(endpoint subcanonical.Connection) string {
	endpoint = subcanonical.NormalizeEndpoint(endpoint)
	if endpoint.Kind == subcanonical.KindGroup || endpoint.Projection == subcanonical.ProjectionManual || endpoint.Projection == subcanonical.ProjectionUnsupported || endpoint.Endpoint.Server == "" || endpoint.Endpoint.Port == "" {
		return ""
	}
	return endpoint.EndpointID
}

func (p *endpointRefreshPlan) unavailable(id uint, index int) bool {
	if p.claimed[id] {
		return true
	}
	owner, reserved := p.reserved[id]
	return reserved && owner != index
}
