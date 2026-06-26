package entityinbounds

import (
	"encoding/json"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entityorder "github.com/MalenkiySolovey/solovey-ui/internal/entities/order"
	"gorm.io/gorm"
	"os"
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
	var inbounds []*model.Inbound
	err := tx.Model(model.Inbound{}).Preload("Tls").Where("id in ?", ids).Find(&inbounds).Error
	if err != nil {
		return err
	}
	for _, inbound := range inbounds {
		err = core.RemoveInbound(inbound.Tag)
		if err != nil && err != os.ErrInvalid {
			return err
		}
		core.CloseInboundConnections(inbound.Tag)
		inboundConfig, err := inbound.MarshalJSON()
		if err != nil {
			return err
		}
		if hooks != nil {
			inboundConfig, err = hooks.AddUsers(tx, inboundConfig, inbound.Id, inbound.Type)
			if err != nil {
				return err
			}
		}
		err = core.AddInbound(inboundConfig)
		if err != nil {
			return err
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
