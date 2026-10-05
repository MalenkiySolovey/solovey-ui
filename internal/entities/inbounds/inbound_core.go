package entityinbounds

import (
	"encoding/json"
	"os"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entityorder "github.com/MalenkiySolovey/solovey-ui/internal/entities/order"
	runtimeprojection "github.com/MalenkiySolovey/solovey-ui/internal/entities/runtimeprojection"
	"gorm.io/gorm"
)

func UpdateOutJSONs(tx *gorm.DB, inboundIDs []uint, hostname string) error {
	var inbounds []model.Inbound
	err := tx.Model(model.Inbound{}).Preload("Tls").Where("id in ?", inboundIDs).Find(&inbounds).Error
	if err != nil {
		return err
	}
	for _, inbound := range inbounds {
		err = FillOutboundJSON(&inbound, hostname)
		if err != nil {
			return err
		}
		err = tx.Model(model.Inbound{}).Where("tag = ?", inbound.Tag).Update("out_json", inbound.OutJson).Error
		if err != nil {
			return err
		}
	}
	return nil
}
func GetAllConfig(db *gorm.DB, hooks UserHooks) ([]json.RawMessage, error) {
	var inboundsJSON []json.RawMessage
	var inbounds []*model.Inbound
	err := db.Model(model.Inbound{}).Preload("Tls").Order(entityorder.Clause).Find(&inbounds).Error
	if err != nil {
		return nil, err
	}
	for _, inbound := range inbounds {
		if !runtimeprojection.Included("inbounds", inbound.Type) {
			continue
		}
		inboundJSON, err := inbound.MarshalJSON()
		if err != nil {
			return nil, err
		}
		if hooks != nil {
			inboundJSON, err = hooks.AddUsers(db, inboundJSON, inbound.Id, inbound.Type)
			if err != nil {
				return nil, err
			}
		}
		inboundsJSON = append(inboundsJSON, inboundJSON)
	}
	return inboundsJSON, nil
}
func Restart(tx *gorm.DB, ids []uint, core Core, hooks UserHooks) error {
	if core == nil || !core.IsRunning() {
		return nil
	}
	if _, err := runtimeprojection.ValidateReferences(tx, nil); err != nil {
		return err
	}
	var rows []*model.Inbound
	if err := tx.Model(model.Inbound{}).Preload("Tls").Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return err
	}
	type prepared struct {
		tag     string
		configs []json.RawMessage
	}
	ready := []prepared{}
	// Complete capability/serialization preflight for the whole batch before
	// the first removal. Unavailable historical rows do not mutate live state.
	for _, row := range rows {
		if !runtimeprojection.Included("inbounds", row.Type) {
			continue
		}

		config, err := row.MarshalJSON()
		if err != nil {
			return err
		}
		if hooks != nil {
			config, err = hooks.AddUsers(tx, config, row.Id, row.Type)
			if err != nil {
				return err
			}
		}
		configs := []json.RawMessage{config}
		ready = append(ready, prepared{row.Tag, configs})
	}

	for _, row := range ready {
		if err := core.RemoveInbound(row.tag); err != nil && err != os.ErrInvalid {
			return err
		}
		core.CloseInboundConnections(row.tag)
		for _, config := range row.configs {

			if err := core.AddInbound(config); err != nil {
				return err
			}
		}
	}
	return nil
}

func RemoveFromCore(tags []string, core Core) error {
	if core == nil || !core.IsRunning() {
		return nil
	}
	for _, tag := range tags {
		if err := core.RemoveInbound(tag); err != nil && err != os.ErrInvalid {
			return err
		}
		core.CloseInboundConnections(tag)
	}
	return nil
}
