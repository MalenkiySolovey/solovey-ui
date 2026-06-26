package netentity

import (
	"encoding/json"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	entitytls "github.com/MalenkiySolovey/solovey-ui/internal/entities/tls"

	"gorm.io/gorm"
)

type TlsService struct {
	InboundService
	ServicesService
}

func (s *TlsService) GetAll() ([]model.Tls, error) {
	return entitytls.GetAll(dbsqlite.DB())
}

func (s *TlsService) Save(tx *gorm.DB, action string, data json.RawMessage, hostname string) error {
	return s.applyTLSSave(tlsSaveRequest{
		tx:       tx,
		action:   action,
		data:     data,
		hostname: hostname,
	})
}

func (s *TlsService) tlsCascadeHooks() entitytls.CascadeHooks {
	if s == nil {
		return nil
	}
	return tlsCascadeHooks{service: s}
}

type tlsCascadeHooks struct {
	service *TlsService
}

func (h tlsCascadeHooks) UpdateLinksByInboundChange(tx *gorm.DB, inbounds *[]model.Inbound, hostname string, oldTag string) error {
	if h.service.InboundService.ClientHooks == nil {
		return nil
	}
	return h.service.InboundService.ClientHooks.UpdateLinksByInboundChange(tx, inbounds, hostname, oldTag)
}

func (h tlsCascadeHooks) UpdateInboundOutJSONs(tx *gorm.DB, inboundIDs []uint, hostname string) error {
	return h.service.InboundService.UpdateOutJsons(tx, inboundIDs, hostname)
}

func (h tlsCascadeHooks) RestartInbounds(tx *gorm.DB, ids []uint) error {
	return h.service.InboundService.RestartInbounds(tx, ids)
}

func (h tlsCascadeHooks) RestartServices(tx *gorm.DB, ids []uint) error {
	return h.service.ServicesService.RestartServices(tx, ids)
}
