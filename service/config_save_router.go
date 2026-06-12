package service

import (
	"encoding/json"

	"github.com/MalenkiySolovey/solovey-ui/util/common"
	"gorm.io/gorm"
)

type configSaveRequest struct {
	tx        *gorm.DB
	object    string
	action    string
	data      json.RawMessage
	initUsers string
	hostname  string
}

type configSaveHandler func(*ConfigService, configSaveRequest, *configSavePlan) error

var configSaveHandlers = map[configSaveObject]configSaveHandler{
	configSaveObjectClients:   saveClientsConfigObject,
	configSaveObjectTLS:       saveTLSConfigObject,
	configSaveObjectInbounds:  saveInboundsConfigObject,
	configSaveObjectOutbounds: saveOutboundsConfigObject,
	configSaveObjectServices:  saveServicesConfigObject,
	configSaveObjectEndpoints: saveEndpointsConfigObject,
	configSaveObjectConfig:    saveBaseConfigObject,
	configSaveObjectSettings:  saveSettingsConfigObject,
}

func (s *ConfigService) applyConfigSaveMutation(tx *gorm.DB, plan *configSavePlan, obj string, act string, data json.RawMessage, initUsers string, hostname string) error {
	req := configSaveRequest{
		tx:        tx,
		object:    obj,
		action:    act,
		data:      data,
		initUsers: initUsers,
		hostname:  hostname,
	}
	return applyConfigSaveObject(s, req, plan)
}

func applyConfigSaveObject(s *ConfigService, req configSaveRequest, plan *configSavePlan) error {
	object, ok := parseConfigSaveObject(req.object)
	if !ok {
		return common.NewError("unknown object:", req.object)
	}
	handler := configSaveHandlers[object]
	return handler(s, req, plan)
}

func saveOutboundsConfigObject(s *ConfigService, req configSaveRequest, plan *configSavePlan) error {
	if err := s.OutboundService.Save(req.tx, req.action, req.data); err != nil {
		return err
	}
	plan.ApplyCascade(configSaveCascadeCoreRuntimeChanged)
	return nil
}

func saveServicesConfigObject(s *ConfigService, req configSaveRequest, plan *configSavePlan) error {
	if err := s.ServicesService.Save(req.tx, req.action, req.data); err != nil {
		return err
	}
	plan.ApplyCascade(configSaveCascadeCoreRuntimeChanged)
	return nil
}

func saveEndpointsConfigObject(s *ConfigService, req configSaveRequest, plan *configSavePlan) error {
	if err := s.EndpointService.Save(req.tx, req.action, req.data); err != nil {
		return err
	}
	plan.ApplyCascade(configSaveCascadeCoreRuntimeChanged)
	return nil
}

func saveBaseConfigObject(s *ConfigService, req configSaveRequest, plan *configSavePlan) error {
	if err := s.SettingService.SaveConfig(req.tx, req.data); err != nil {
		return err
	}
	plan.ApplyCascade(configSaveCascadeCoreRuntimeChanged)
	return nil
}

func saveSettingsConfigObject(s *ConfigService, req configSaveRequest, plan *configSavePlan) error {
	return s.SettingService.Save(req.tx, req.data)
}
