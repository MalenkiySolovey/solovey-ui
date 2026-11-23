package outbounds

import (
	"encoding/json"
	"errors"
	"fmt"

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
// as the bindings. The broker verifies each sealed definition independently;
// neither editor text nor environment/engine authority crosses this port.
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
		tags[request.OutboundRef] = true
	}
	requirements, err := singboxconfig.ZapretDNSRequirements([]byte(base.Value), tags)
	if err != nil {
		return result, err
	}
	result.DNS = requirements
	raw, err := ReadZapretCatalog(db)
	if err != nil {
		return result, err
	}
	var catalog struct {
		Schema  string
		Records map[string]struct{ Definition json.RawMessage }
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
	}
	return result, nil
}
