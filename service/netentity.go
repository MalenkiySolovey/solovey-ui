package service

import (
	"encoding/json"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	singboxapply "github.com/MalenkiySolovey/solovey-ui/internal/singbox/apply"
	remotesub "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/remote"
	netentitysvc "github.com/MalenkiySolovey/solovey-ui/service/netentity"

	"gorm.io/gorm"
)

type EndpointService struct {
	WarpService
	Runtime *Runtime
}

func (s *EndpointService) backend() netentitysvc.EndpointService {
	runtime := DefaultRuntime()
	if s != nil {
		runtime = runtimeOrDefault(s.Runtime)
	}
	return netentitysvc.EndpointService{Core: runtime.Core(), Warp: &s.WarpService}
}

func (s *EndpointService) GetAll() (*[]map[string]interface{}, error) {
	backend := s.backend()
	return backend.GetAll()
}
func (s *EndpointService) GetAllConfig(db *gorm.DB) ([]json.RawMessage, error) {
	backend := s.backend()
	return backend.GetAllConfig(db)
}
func (s *EndpointService) Save(tx *gorm.DB, action string, data json.RawMessage) error {
	backend := s.backend()
	return backend.Save(tx, action, data)
}
func (s *EndpointService) SaveWithCoreChange(tx *gorm.DB, action string, data json.RawMessage) (*singboxapply.Change, error) {
	backend := s.backend()
	return backend.SaveWithCoreChange(tx, action, data)
}
func (s *EndpointService) RestartEndpoints(tx *gorm.DB, ids []uint) error {
	backend := s.backend()
	return backend.RestartEndpoints(tx, ids)
}
func (s *EndpointService) RemoveEndpointsFromCore(tags []string) error {
	backend := s.backend()
	return backend.RemoveEndpointsFromCore(tags)
}

type OutboundService struct{ Runtime *Runtime }

func (s *OutboundService) backend() netentitysvc.OutboundService {
	runtime := DefaultRuntime()
	if s != nil {
		runtime = runtimeOrDefault(s.Runtime)
	}
	return netentitysvc.OutboundService{Core: runtime.Core()}
}
func (s *OutboundService) GetAll() (*[]map[string]interface{}, error) {
	backend := s.backend()
	return backend.GetAll()
}
func (s *OutboundService) GetAllConfig(db *gorm.DB) ([]json.RawMessage, error) {
	backend := s.backend()
	return backend.GetAllConfig(db)
}
func (s *OutboundService) Save(tx *gorm.DB, action string, data json.RawMessage) error {
	_, err := s.SaveWithCoreChange(tx, action, data)
	return err
}
func (s *OutboundService) SaveWithCoreChange(tx *gorm.DB, action string, data json.RawMessage) (*singboxapply.Change, error) {
	backend := s.backend()
	change, err := backend.SaveWithCoreChange(tx, action, data)
	if err != nil {
		return nil, err
	}
	if _, err := remotesub.ReconcileOutboundLinks(tx); err != nil {
		return nil, err
	}
	return change, nil
}
func (s *OutboundService) RestartOutbounds(tx *gorm.DB, ids []uint) error {
	backend := s.backend()
	return backend.RestartOutbounds(tx, ids)
}
func (s *OutboundService) RemoveOutboundsFromCore(tags []string) error {
	backend := s.backend()
	return backend.RemoveOutboundsFromCore(tags)
}

type ServicesService struct{ Runtime *Runtime }

func (s *ServicesService) backend() netentitysvc.ServicesService {
	runtime := DefaultRuntime()
	if s != nil {
		runtime = runtimeOrDefault(s.Runtime)
	}
	return netentitysvc.ServicesService{Core: runtime.Core()}
}
func (s *ServicesService) GetAll() (*[]map[string]interface{}, error) {
	backend := s.backend()
	return backend.GetAll()
}
func (s *ServicesService) GetAllConfig(db *gorm.DB) ([]json.RawMessage, error) {
	backend := s.backend()
	return backend.GetAllConfig(db)
}
func (s *ServicesService) Save(tx *gorm.DB, action string, data json.RawMessage) error {
	backend := s.backend()
	return backend.Save(tx, action, data)
}
func (s *ServicesService) SaveWithCoreChange(tx *gorm.DB, action string, data json.RawMessage) (*singboxapply.Change, error) {
	backend := s.backend()
	return backend.SaveWithCoreChange(tx, action, data)
}
func (s *ServicesService) RemoveServicesFromCore(tags []string) error {
	backend := s.backend()
	return backend.RemoveServicesFromCore(tags)
}
func (s *ServicesService) RestartServices(tx *gorm.DB, ids []uint) error {
	backend := s.backend()
	return backend.RestartServices(tx, ids)
}

type InboundService struct {
	ClientService
	Runtime *Runtime
}

func (s *InboundService) backend() netentitysvc.InboundService {
	runtime := DefaultRuntime()
	if s != nil {
		runtime = runtimeOrDefault(s.Runtime)
	}
	return netentitysvc.InboundService{ClientHooks: &s.ClientService, Core: runtime.Core()}
}
func (s *InboundService) Get(ids string) (*[]map[string]interface{}, error) {
	backend := s.backend()
	return backend.Get(ids)
}
func (s *InboundService) GetAll() (*[]map[string]interface{}, error) {
	backend := s.backend()
	return backend.GetAll()
}
func (s *InboundService) FromIds(ids []uint) ([]*model.Inbound, error) {
	backend := s.backend()
	return backend.FromIds(ids)
}
func (s *InboundService) Save(tx *gorm.DB, action string, data json.RawMessage, initialUserIDs, hostname string) error {
	backend := s.backend()
	return backend.Save(tx, action, data, initialUserIDs, hostname)
}
func (s *InboundService) SaveWithCoreChange(tx *gorm.DB, action string, data json.RawMessage, initialUserIDs, hostname string) (*singboxapply.Change, error) {
	backend := s.backend()
	return backend.SaveWithCoreChange(tx, action, data, initialUserIDs, hostname)
}
func (s *InboundService) UpdateOutJsons(tx *gorm.DB, inboundIDs []uint, hostname string) error {
	backend := s.backend()
	return backend.UpdateOutJsons(tx, inboundIDs, hostname)
}
func (s *InboundService) GetAllConfig(db *gorm.DB) ([]json.RawMessage, error) {
	backend := s.backend()
	return backend.GetAllConfig(db)
}
func (s *InboundService) RestartInbounds(tx *gorm.DB, ids []uint) error {
	backend := s.backend()
	return backend.RestartInbounds(tx, ids)
}
func (s *InboundService) RestartCurrentInbounds(ids []uint) error {
	backend := s.backend()
	return backend.RestartCurrentInbounds(ids)
}
func (s *InboundService) RemoveInboundsFromCore(tags []string) error {
	backend := s.backend()
	return backend.RemoveInboundsFromCore(tags)
}

type TlsService struct {
	InboundService
	ServicesService
	Runtime *Runtime
}

func (s *TlsService) backend() netentitysvc.TlsService {
	inbounds := s.InboundService.backend()
	services := s.ServicesService.backend()
	return netentitysvc.TlsService{InboundService: inbounds, ServicesService: services}
}
func (s *TlsService) GetAll() ([]model.Tls, error) {
	backend := s.backend()
	return backend.GetAll()
}
func (s *TlsService) Save(tx *gorm.DB, action string, data json.RawMessage, hostname string) error {
	backend := s.backend()
	return backend.Save(tx, action, data, hostname)
}
