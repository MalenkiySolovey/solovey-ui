package service

import (
	"encoding/json"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/util"
	"github.com/MalenkiySolovey/solovey-ui/util/common"

	"gorm.io/gorm"
)

type inboundSaveAction string

const (
	inboundSaveActionNew  inboundSaveAction = "new"
	inboundSaveActionEdit inboundSaveAction = "edit"
	inboundSaveActionDel  inboundSaveAction = "del"
)

var supportedInboundSaveActions = []inboundSaveAction{
	inboundSaveActionNew,
	inboundSaveActionEdit,
	inboundSaveActionDel,
}

type inboundSaveRequest struct {
	tx          *gorm.DB
	action      string
	data        json.RawMessage
	initUserIDs string
	hostname    string
}

type inboundSaveHandler func(*InboundService, inboundSaveRequest) error

var inboundSaveHandlers = map[inboundSaveAction]inboundSaveHandler{
	inboundSaveActionNew:  saveNewInbound,
	inboundSaveActionEdit: saveEditedInbound,
	inboundSaveActionDel:  saveDeletedInbound,
}

func (s *InboundService) applyInboundSave(req inboundSaveRequest) error {
	action, ok := parseInboundSaveAction(req.action)
	if !ok {
		return common.NewErrorf("unknown action: %s", req.action)
	}
	return inboundSaveHandlers[action](s, req)
}

func parseInboundSaveAction(action string) (inboundSaveAction, bool) {
	saveAction := inboundSaveAction(action)
	for _, supported := range supportedInboundSaveActions {
		if saveAction == supported {
			return saveAction, true
		}
	}
	return "", false
}

func supportedInboundSaveActionStrings() []string {
	actions := make([]string, 0, len(supportedInboundSaveActions))
	for _, action := range supportedInboundSaveActions {
		actions = append(actions, string(action))
	}
	return actions
}

func saveNewInbound(s *InboundService, req inboundSaveRequest) error {
	inbound, err := saveInboundConfig(req.tx, req.data, req.hostname)
	if err != nil {
		return err
	}
	return s.ClientService.UpdateClientsOnInboundAdd(req.tx, req.initUserIDs, inbound.Id, req.hostname)
}

func saveEditedInbound(s *InboundService, req inboundSaveRequest) error {
	inbound, err := decodeInboundForSave(req.tx, req.data)
	if err != nil {
		return err
	}
	oldTag, err := inboundTagByID(req.tx, inbound.Id)
	if err != nil {
		return err
	}
	if err := fillAndSaveInbound(req.tx, &inbound, req.hostname); err != nil {
		return err
	}
	return s.ClientService.UpdateLinksByInboundChange(req.tx, &[]model.Inbound{inbound}, req.hostname, oldTag)
}

func saveInboundConfig(tx *gorm.DB, data json.RawMessage, hostname string) (model.Inbound, error) {
	inbound, err := decodeInboundForSave(tx, data)
	if err != nil {
		return inbound, err
	}
	if err := fillAndSaveInbound(tx, &inbound, hostname); err != nil {
		return inbound, err
	}
	return inbound, nil
}

func decodeInboundForSave(tx *gorm.DB, data json.RawMessage) (model.Inbound, error) {
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

func fillAndSaveInbound(tx *gorm.DB, inbound *model.Inbound, hostname string) error {
	if err := util.FillOutJson(inbound, hostname); err != nil {
		return err
	}
	sortOrder, err := sortOrderForSave(tx, &model.Inbound{}, inbound.Id)
	if err != nil {
		return err
	}
	inbound.SortOrder = sortOrder
	return tx.Save(inbound).Error
}

func inboundTagByID(tx *gorm.DB, id uint) (string, error) {
	var tag string
	err := tx.Model(model.Inbound{}).Select("tag").Where("id = ?", id).Find(&tag).Error
	return tag, err
}

func saveDeletedInbound(s *InboundService, req inboundSaveRequest) error {
	var tag string
	if err := json.Unmarshal(req.data, &tag); err != nil {
		return err
	}
	id, err := inboundIDByTag(req.tx, tag)
	if err != nil {
		return err
	}
	if err := s.ClientService.UpdateClientsOnInboundDelete(req.tx, id, tag); err != nil {
		return err
	}
	return req.tx.Where("tag = ?", tag).Delete(model.Inbound{}).Error
}

func inboundIDByTag(tx *gorm.DB, tag string) (uint, error) {
	var id uint
	err := tx.Model(model.Inbound{}).Select("id").Where("tag = ?", tag).Scan(&id).Error
	return id, err
}
