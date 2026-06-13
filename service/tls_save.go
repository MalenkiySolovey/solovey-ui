package service

import (
	"encoding/json"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/util/common"

	"gorm.io/gorm"
)

type tlsSaveAction string

const (
	tlsSaveActionNew  tlsSaveAction = "new"
	tlsSaveActionEdit tlsSaveAction = "edit"
	tlsSaveActionDel  tlsSaveAction = "del"
)

var supportedTLSSaveActions = []tlsSaveAction{
	tlsSaveActionNew,
	tlsSaveActionEdit,
	tlsSaveActionDel,
}

type tlsSaveRequest struct {
	tx       *gorm.DB
	action   string
	data     json.RawMessage
	hostname string
}

type tlsSaveHandler func(*TlsService, tlsSaveRequest) error

var tlsSaveHandlers = map[tlsSaveAction]tlsSaveHandler{
	tlsSaveActionNew:  saveNewTLS,
	tlsSaveActionEdit: saveEditedTLS,
	tlsSaveActionDel:  saveDeletedTLS,
}

func (s *TlsService) applyTLSSave(req tlsSaveRequest) error {
	action, ok := parseTLSSaveAction(req.action)
	if !ok {
		return nil
	}
	return tlsSaveHandlers[action](s, req)
}

func parseTLSSaveAction(action string) (tlsSaveAction, bool) {
	saveAction := tlsSaveAction(action)
	for _, supported := range supportedTLSSaveActions {
		if saveAction == supported {
			return saveAction, true
		}
	}
	return "", false
}

func supportedTLSSaveActionStrings() []string {
	actions := make([]string, 0, len(supportedTLSSaveActions))
	for _, action := range supportedTLSSaveActions {
		actions = append(actions, string(action))
	}
	return actions
}

func saveNewTLS(s *TlsService, req tlsSaveRequest) error {
	_, err := saveTLSConfig(req.tx, req.data)
	return err
}

func saveEditedTLS(s *TlsService, req tlsSaveRequest) error {
	tls, err := saveTLSConfig(req.tx, req.data)
	if err != nil {
		return err
	}
	return s.applyTLSEditCascade(req.tx, tls.Id, req.hostname)
}

func saveTLSConfig(tx *gorm.DB, data json.RawMessage) (model.Tls, error) {
	var tls model.Tls
	if err := json.Unmarshal(data, &tls); err != nil {
		return tls, err
	}
	sortOrder, err := sortOrderForSave(tx, &model.Tls{}, tls.Id)
	if err != nil {
		return tls, err
	}
	tls.SortOrder = sortOrder
	if err := tx.Save(&tls).Error; err != nil {
		return tls, err
	}
	return tls, nil
}

func (s *TlsService) applyTLSEditCascade(tx *gorm.DB, tlsID uint, hostname string) error {
	if err := s.refreshInboundsUsingTLS(tx, tlsID, hostname); err != nil {
		return err
	}
	return s.restartServicesUsingTLS(tx, tlsID)
}

func (s *TlsService) refreshInboundsUsingTLS(tx *gorm.DB, tlsID uint, hostname string) error {
	var inbounds []model.Inbound
	if err := tx.Model(model.Inbound{}).Preload("Tls").Where("tls_id = ?", tlsID).Find(&inbounds).Error; err != nil {
		return err
	}
	if len(inbounds) == 0 {
		return nil
	}
	if err := s.ClientService.UpdateLinksByInboundChange(tx, &inbounds, hostname, ""); err != nil {
		return err
	}
	inboundIDs := inboundIDsFromRows(inbounds)
	if err := s.InboundService.UpdateOutJsons(tx, inboundIDs, hostname); err != nil {
		return common.NewError("unable to update out_json of inbounds: ", err.Error())
	}
	return s.InboundService.RestartInbounds(tx, inboundIDs)
}

func (s *TlsService) restartServicesUsingTLS(tx *gorm.DB, tlsID uint) error {
	var serviceIDs []uint
	if err := tx.Model(model.Service{}).Where("tls_id = ?", tlsID).Scan(&serviceIDs).Error; err != nil {
		return err
	}
	if len(serviceIDs) == 0 {
		return nil
	}
	return s.ServicesService.RestartServices(tx, serviceIDs)
}

func inboundIDsFromRows(inbounds []model.Inbound) []uint {
	inboundIDs := make([]uint, 0, len(inbounds))
	for _, inbound := range inbounds {
		inboundIDs = append(inboundIDs, inbound.Id)
	}
	return inboundIDs
}

func saveDeletedTLS(s *TlsService, req tlsSaveRequest) error {
	var id uint
	if err := json.Unmarshal(req.data, &id); err != nil {
		return err
	}
	if err := ensureTLSNotInUse(req.tx, id); err != nil {
		return err
	}
	return req.tx.Where("id = ?", id).Delete(model.Tls{}).Error
}

func ensureTLSNotInUse(tx *gorm.DB, id uint) error {
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
