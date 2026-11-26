package outbounds

import (
	"encoding/json"
	"fmt"

	"github.com/MalenkiySolovey/solovey-backend/storage/model"
	singboxconfig "github.com/MalenkiySolovey/solovey-ui/internal/singbox/config"
	"github.com/MalenkiySolovey/solovey-ui/internal/zapretactivation"
	"github.com/MalenkiySolovey/solovey-ui/internal/zapretactivation/catalogstore"
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
	return catalogstore.Read(db)
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
	if err := catalogstore.LoadSelected(db, &result); err != nil {
		return result, fmt.Errorf("%w: %w", ErrZapretPacketPathUnqualified, err)
	}
	return result, nil
}
