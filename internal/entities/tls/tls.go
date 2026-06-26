package entitytls

import (
	"encoding/json"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entityorder "github.com/MalenkiySolovey/solovey-ui/internal/entities/order"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"gorm.io/gorm"
)

type SaveAction string

const (
	ActionNew  SaveAction = "new"
	ActionEdit SaveAction = "edit"
	ActionDel  SaveAction = "del"
)

var supportedSaveActions = []SaveAction{
	ActionNew,
	ActionEdit,
	ActionDel,
}

type SaveRequest struct {
	Tx       *gorm.DB
	Action   string
	Data     json.RawMessage
	Hostname string
	Hooks    CascadeHooks
}

type CascadeHooks interface {
	UpdateLinksByInboundChange(tx *gorm.DB, inbounds *[]model.Inbound, hostname string, oldTag string) error
	UpdateInboundOutJSONs(tx *gorm.DB, inboundIDs []uint, hostname string) error
	RestartInbounds(tx *gorm.DB, ids []uint) error
	RestartServices(tx *gorm.DB, ids []uint) error
}

func GetAll(db *gorm.DB) ([]model.Tls, error) {
	tlsConfigs := []model.Tls{}
	err := db.Model(model.Tls{}).Where("id > 0").Order(entityorder.Clause).Scan(&tlsConfigs).Error
	if err != nil {
		return nil, err
	}
	return tlsConfigs, nil
}

func Save(req SaveRequest) error {
	action, ok := ParseAction(req.Action)
	if !ok {
		return nil
	}
	switch action {
	case ActionNew:
		_, err := SaveConfig(req.Tx, req.Data)
		return err
	case ActionEdit:
		tls, err := SaveConfig(req.Tx, req.Data)
		if err != nil {
			return err
		}
		return ApplyEditCascade(req.Tx, tls.Id, req.Hostname, req.Hooks)
	case ActionDel:
		return Delete(req.Tx, req.Data)
	default:
		return nil
	}
}

func ParseAction(action string) (SaveAction, bool) {
	saveAction := SaveAction(action)
	for _, supported := range supportedSaveActions {
		if saveAction == supported {
			return saveAction, true
		}
	}
	return "", false
}

func SupportedActionStrings() []string {
	actions := make([]string, 0, len(supportedSaveActions))
	for _, action := range supportedSaveActions {
		actions = append(actions, string(action))
	}
	return actions
}

func SaveConfig(tx *gorm.DB, data json.RawMessage) (model.Tls, error) {
	var tls model.Tls
	if err := json.Unmarshal(data, &tls); err != nil {
		return tls, err
	}

	sortOrder, err := entityorder.ForSave(tx, &model.Tls{}, tls.Id)
	if err != nil {
		return tls, err
	}
	tls.SortOrder = sortOrder

	if err := tx.Save(&tls).Error; err != nil {
		return tls, err
	}
	return tls, nil
}

func ApplyEditCascade(tx *gorm.DB, tlsID uint, hostname string, hooks CascadeHooks) error {
	if err := RefreshInboundsUsingTLS(tx, tlsID, hostname, hooks); err != nil {
		return err
	}
	return RestartServicesUsingTLS(tx, tlsID, hooks)
}

func RefreshInboundsUsingTLS(tx *gorm.DB, tlsID uint, hostname string, hooks CascadeHooks) error {
	var inbounds []model.Inbound
	if err := tx.Model(model.Inbound{}).Preload("Tls").Where("tls_id = ?", tlsID).Find(&inbounds).Error; err != nil {
		return err
	}
	if len(inbounds) == 0 {
		return nil
	}
	if hooks == nil {
		return common.NewError("tls cascade hooks are not configured")
	}

	if err := hooks.UpdateLinksByInboundChange(tx, &inbounds, hostname, ""); err != nil {
		return err
	}
	inboundIDs := InboundIDsFromRows(inbounds)
	if err := hooks.UpdateInboundOutJSONs(tx, inboundIDs, hostname); err != nil {
		return common.NewError("unable to update out_json of inbounds: ", err.Error())
	}
	return hooks.RestartInbounds(tx, inboundIDs)
}

func RestartServicesUsingTLS(tx *gorm.DB, tlsID uint, hooks CascadeHooks) error {
	var serviceIDs []uint
	if err := tx.Model(model.Service{}).Where("tls_id = ?", tlsID).Scan(&serviceIDs).Error; err != nil {
		return err
	}
	if len(serviceIDs) == 0 {
		return nil
	}
	if hooks == nil {
		return common.NewError("tls cascade hooks are not configured")
	}
	return hooks.RestartServices(tx, serviceIDs)
}

func InboundIDsFromRows(inbounds []model.Inbound) []uint {
	inboundIDs := make([]uint, 0, len(inbounds))
	for _, inbound := range inbounds {
		inboundIDs = append(inboundIDs, inbound.Id)
	}
	return inboundIDs
}

func Delete(tx *gorm.DB, data json.RawMessage) error {
	var id uint
	if err := json.Unmarshal(data, &id); err != nil {
		return err
	}
	if err := EnsureNotInUse(tx, id); err != nil {
		return err
	}
	return tx.Where("id = ?", id).Delete(model.Tls{}).Error
}

func EnsureNotInUse(tx *gorm.DB, id uint) error {
	var inboundCount int64
	if err := tx.Model(model.Inbound{}).Where("tls_id = ?", id).Count(&inboundCount).Error; err != nil {
		return err
	}
	var serviceCount int64
	if err := tx.Model(model.Service{}).Where("tls_id = ?", id).Count(&serviceCount).Error; err != nil {
		return err
	}
	if inboundCount > 0 || serviceCount > 0 {
		return common.NewError("tls in use")
	}
	return nil
}
