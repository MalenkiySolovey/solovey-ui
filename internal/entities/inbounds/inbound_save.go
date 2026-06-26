package entityinbounds

import (
	"encoding/json"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	entityorder "github.com/MalenkiySolovey/solovey-ui/internal/entities/order"
	singboxapply "github.com/MalenkiySolovey/solovey-ui/internal/singbox/apply"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/tagrefs"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"gorm.io/gorm"
)

func Save(req SaveRequest) (*singboxapply.Change, error) {
	action, ok := ParseAction(req.Action)
	if !ok {
		return nil, common.NewErrorf("unknown action: %s", req.Action)
	}
	switch action {
	case ActionNew:
		return saveNew(req)
	case ActionEdit:
		return saveEdited(req)
	case ActionDel:
		return saveDeleted(req)
	default:
		return nil, common.NewErrorf("unknown action: %s", req.Action)
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
func saveNew(req SaveRequest) (*singboxapply.Change, error) {
	inbound, err := SaveConfig(req.Tx, req.Data, req.Hostname)
	if err != nil {
		return nil, err
	}
	if req.ClientHooks != nil {
		if err := req.ClientHooks.UpdateClientsOnInboundAdd(req.Tx, req.InitUserIDs, inbound.Id, req.Hostname); err != nil {
			return nil, err
		}
	}
	return CoreChangeForSavedRow(req.Tx, inbound.Id, inbound.Tag, "")
}
func saveEdited(req SaveRequest) (*singboxapply.Change, error) {
	inbound, err := DecodeForSave(req.Tx, req.Data)
	if err != nil {
		return nil, err
	}
	oldTag, err := TagByID(req.Tx, inbound.Id)
	if err != nil {
		return nil, err
	}
	if oldTag != "" && oldTag != inbound.Tag {
		refs, err := tagrefs.Inbound(req.Tx, oldTag)
		if err != nil {
			return nil, err
		}
		if len(refs) > 0 {
			return nil, tagrefs.FormatError("inbound", oldTag, refs)
		}
	}
	if err := FillAndSave(req.Tx, &inbound, req.Hostname); err != nil {
		return nil, err
	}
	if req.ClientHooks != nil {
		if err := req.ClientHooks.UpdateLinksByInboundChange(req.Tx, &[]model.Inbound{inbound}, req.Hostname, oldTag); err != nil {
			return nil, err
		}
	}
	return CoreChangeForSavedRow(req.Tx, inbound.Id, inbound.Tag, oldTag)
}
func SaveConfig(tx *gorm.DB, data json.RawMessage, hostname string) (model.Inbound, error) {
	inbound, err := DecodeForSave(tx, data)
	if err != nil {
		return inbound, err
	}
	if err := FillAndSave(tx, &inbound, hostname); err != nil {
		return inbound, err
	}
	return inbound, nil
}
func DecodeForSave(tx *gorm.DB, data json.RawMessage) (model.Inbound, error) {
	var inbound model.Inbound
	if err := inbound.UnmarshalJSON(data); err != nil {
		return inbound, err
	}
	if inbound.TlsId > 0 {
		if err := tx.Model(model.Tls{}).Where("id = ?", inbound.TlsId).Find(&inbound.Tls).Error; err != nil {
			return inbound, err
		}
	}
	return inbound, nil
}
func FillAndSave(tx *gorm.DB, inbound *model.Inbound, hostname string) error {
	if err := FillOutboundJSON(inbound, hostname); err != nil {
		return err
	}
	sortOrder, err := entityorder.ForSave(tx, &model.Inbound{}, inbound.Id)
	if err != nil {
		return err
	}
	inbound.SortOrder = sortOrder
	return tx.Save(inbound).Error
}
func TagByID(tx *gorm.DB, id uint) (string, error) {
	var tag string
	err := tx.Model(model.Inbound{}).Select("tag").Where("id = ?", id).Find(&tag).Error
	return tag, err
}
func saveDeleted(req SaveRequest) (*singboxapply.Change, error) {
	var tag string
	if err := json.Unmarshal(req.Data, &tag); err != nil {
		return nil, err
	}
	refs, err := tagrefs.Inbound(req.Tx, tag)
	if err != nil {
		return nil, err
	}
	if len(refs) > 0 {
		return nil, tagrefs.FormatError("inbound", tag, refs)
	}
	id, err := IDByTag(req.Tx, tag)
	if err != nil {
		return nil, err
	}
	if req.ClientHooks != nil {
		if err := req.ClientHooks.UpdateClientsOnInboundDelete(req.Tx, id, tag); err != nil {
			return nil, err
		}
	}
	if err := req.Tx.Where("tag = ?", tag).Delete(model.Inbound{}).Error; err != nil {
		return nil, err
	}
	return &singboxapply.Change{RemoveTags: []string{tag}}, nil
}
func IDByTag(tx *gorm.DB, tag string) (uint, error) {
	var id uint
	err := tx.Model(model.Inbound{}).Select("id").Where("tag = ?", tag).Scan(&id).Error
	return id, err
}
func CoreChangeForSavedRow(tx *gorm.DB, id uint, tag string, oldTag string) (*singboxapply.Change, error) {
	change := &singboxapply.Change{ReloadIDs: []uint{id}}
	if oldTag != "" && oldTag != tag {
		change.RemoveTags = []string{oldTag}
	}
	serviceIDs, err := tagrefs.SSMCascadeServiceIDs(tx, tag)
	if err != nil {
		return nil, err
	}
	change.CascadeServiceIDs = serviceIDs
	return change, nil
}
