package outbounds

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/MalenkiySolovey/solovey-backend/storage/model"
	singboxconfig "github.com/MalenkiySolovey/solovey-ui/internal/singbox/config"
	"github.com/MalenkiySolovey/solovey-ui/internal/zapretactivation"
	"gorm.io/gorm"
)

type ZapretReference struct {
	OutboundRef string `json:"outboundRef"`
	StrategyRef string `json:"strategyRef"`
	Enabled     bool   `json:"enabled"`
}

func ZapretReferences(db *gorm.DB) ([]ZapretReference, error) {
	var rows []model.Outbound
	if err := db.Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	refs := make([]ZapretReference, 0)
	for _, row := range rows {
		_, binding, err := zapretOptions(row)
		if err != nil {
			return nil, err
		}
		if binding != nil && binding.StrategyRef != "" && binding.StrategyRef != "none" {
			refs = append(refs, ZapretReference{binding.OutboundRef, binding.StrategyRef, binding.Enabled})
		}
	}
	return refs, nil
}

func ReadZapretCatalog(db *gorm.DB) (json.RawMessage, error) {
	var setting model.Setting
	if err := db.Where("key = ?", zapretactivation.CatalogKey).Limit(1).Find(&setting).Error; err != nil {
		return nil, err
	}
	if setting.Key == "" {
		return nil, nil
	}
	if len(setting.Value) > zapretactivation.MaxCatalogBytes || !json.Valid([]byte(setting.Value)) {
		return nil, errors.New("invalid stored Zapret strategy catalog")
	}
	return json.RawMessage(setting.Value), nil
}

// ZapretValidation binds the selected definitions to the same writer snapshot
// as the bindings. The broker independently verifies the sealed definitions,
// the selected Lua sources, and the installed operator's delegation.
func ZapretValidation(db *gorm.DB, requests []zapretactivation.Request) (zapretactivation.Validation, error) {
	result := zapretactivation.Validation{Bindings: requests}
	if len(requests) == 0 {
		return result, nil
	}
	var base model.Setting
	if err := db.Where("key = ?", "config").Limit(1).Find(&base).Error; err != nil {
		return result, err
	}
	tags := make(map[string]bool, len(requests))
	for _, request := range requests {
		// A proxy detour carries DNS inside its already admitted transport.
		// The DNS server's port is the packet scope only when dialing directly.
		if request.Outer.Kind == "" {
			tags[request.OutboundRef] = true
		}
	}
	requirements, err := singboxconfig.ZapretDNSRequirements([]byte(base.Value), tags)
	if err != nil {
		return result, err
	}
	result.DNS = requirements
	managed := false
	for _, request := range requests {
		managed = managed || zapretactivation.ManagedStrategyRef(request.StrategyRef)
	}
	if !managed {
		return result, nil
	}
	raw, err := ReadZapretCatalog(db)
	if err != nil {
		return result, err
	}
	var catalog struct {
		Schema       string
		Records      map[string]struct{ Definition json.RawMessage }
		LuaDrafts    map[string]json.RawMessage
		LuaRevisions map[string]json.RawMessage
	}
	if raw != nil && (json.Unmarshal(raw, &catalog) != nil || catalog.Schema != "solovey.zapret-catalog/v1" || catalog.Records == nil) {
		return result, fmt.Errorf("%w: invalid stored strategy catalog", ErrZapretPacketPathUnqualified)
	}
	for _, request := range requests {
		if !zapretactivation.ManagedStrategyRef(request.StrategyRef) {
			continue
		}
		record := catalog.Records[request.StrategyRef]
		if len(record.Definition) == 0 || len(record.Definition) > zapretactivation.MaxDefinitionBytes {
			return result, fmt.Errorf("outbound %q: Zapret strategy %q is unavailable: %w", request.OutboundRef, request.StrategyRef, ErrZapretPacketPathUnqualified)
		}
		if result.Definitions == nil {
			result.Definitions = make(map[string]json.RawMessage)
		}
		result.Definitions[request.StrategyRef] = record.Definition
		var definition struct {
			ManagedLua []struct {
				ID, Revision string
			}
		}
		if err := json.Unmarshal(record.Definition, &definition); err != nil {
			return result, fmt.Errorf("outbound %q: invalid sealed Zapret strategy: %w", request.OutboundRef, ErrZapretPacketPathUnqualified)
		}
		for _, ref := range definition.ManagedLua {
			key := zapretactivation.LuaSourceKey(ref.ID, ref.Revision)
			if ref.ID == "" || strings.Contains(ref.ID, "/") || !zapretactivation.ValidDigest(ref.Revision) {
				return result, fmt.Errorf("outbound %q: invalid managed Lua reference: %w", request.OutboundRef, ErrZapretPacketPathUnqualified)
			}
			source := catalog.LuaRevisions[key]
			if len(source) == 0 {
				source = catalog.LuaDrafts[ref.ID]
			}
			if len(source) == 0 {
				return result, fmt.Errorf("outbound %q: selected Lua revision for %q is unavailable: %w", request.OutboundRef, ref.ID, ErrZapretPacketPathUnqualified)
			}
			if result.LuaSources == nil {
				result.LuaSources = make(map[string]json.RawMessage)
			}
			result.LuaSources[key] = source
		}
	}
	return result, nil
}
