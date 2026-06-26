package service

import (
	"encoding/json"

	singboxapply "github.com/MalenkiySolovey/solovey-ui/internal/singbox/apply"
	"gorm.io/gorm"
)

type configSaveExecutor struct {
	service *ConfigService
}

func (e configSaveExecutor) SaveClients(tx *gorm.DB, action string, data json.RawMessage, hostname string) ([]uint, error) {
	return e.service.ClientService.Save(tx, action, data, hostname)
}

func (e configSaveExecutor) SaveTLS(tx *gorm.DB, action string, data json.RawMessage, hostname string) error {
	return e.service.TlsService.Save(tx, action, data, hostname)
}

func (e configSaveExecutor) SaveInbounds(tx *gorm.DB, action string, data json.RawMessage, initUsers string, hostname string) (*singboxapply.Change, error) {
	return e.service.InboundService.SaveWithCoreChange(tx, action, data, initUsers, hostname)
}

func (e configSaveExecutor) SaveOutbounds(tx *gorm.DB, action string, data json.RawMessage) (*singboxapply.Change, error) {
	return e.service.OutboundService.SaveWithCoreChange(tx, action, data)
}

func (e configSaveExecutor) SaveServices(tx *gorm.DB, action string, data json.RawMessage) (*singboxapply.Change, error) {
	return e.service.ServicesService.SaveWithCoreChange(tx, action, data)
}

func (e configSaveExecutor) SaveEndpoints(tx *gorm.DB, action string, data json.RawMessage) (*singboxapply.Change, error) {
	return e.service.EndpointService.SaveWithCoreChange(tx, action, data)
}

func (e configSaveExecutor) ConfigBlobChanged(tx *gorm.DB, data json.RawMessage) (bool, error) {
	return e.service.SettingService.ConfigBlobChanged(tx, data)
}

func (e configSaveExecutor) SaveBaseConfig(tx *gorm.DB, data json.RawMessage) error {
	return e.service.SettingService.SaveConfig(tx, data)
}

func (e configSaveExecutor) SaveSettings(tx *gorm.DB, data json.RawMessage) error {
	return e.service.SettingService.Save(tx, data)
}
