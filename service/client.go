package service

import (
	"encoding/json"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	clientsvc "github.com/MalenkiySolovey/solovey-ui/service/client"

	"gorm.io/gorm"
)

// ClientService binds the client domain to the application runtime.
type ClientService struct {
	Runtime *Runtime
}

func (s *ClientService) backend() clientsvc.Service {
	runtime := DefaultRuntime()
	if s != nil {
		runtime = runtimeOrDefault(s.Runtime)
	}
	return clientsvc.New(func(value int64) { runtime.updates().Set(value) })
}

func (s *ClientService) Get(id string) (*[]model.Client, error) {
	backend := s.backend()
	return backend.Get(id)
}

func (s *ClientService) GetWithLocalLinks(id, hostname string) (*[]model.Client, error) {
	backend := s.backend()
	return backend.GetWithLocalLinks(id, hostname)
}

func (s *ClientService) GetAll() (*[]model.Client, error) {
	backend := s.backend()
	return backend.GetAll()
}

func (s *ClientService) Save(tx *gorm.DB, action string, data json.RawMessage, hostname string) ([]uint, error) {
	backend := s.backend()
	return backend.Save(tx, action, data, hostname)
}

func (s *ClientService) DepleteClients() ([]uint, error) {
	backend := s.backend()
	return backend.DepleteClients()
}

func (s *ClientService) ResetClients(tx *gorm.DB, dateTime int64) ([]uint, error) {
	backend := s.backend()
	return backend.ResetClients(tx, dateTime)
}

func (s *ClientService) RotateSubSecret(id string) (string, error) {
	backend := s.backend()
	return backend.RotateSubSecret(id)
}

func (s *ClientService) UpdateClientsOnInboundAdd(tx *gorm.DB, initialIDs string, inboundID uint, hostname string) error {
	backend := s.backend()
	return backend.UpdateClientsOnInboundAdd(tx, initialIDs, inboundID, hostname)
}

func (s *ClientService) UpdateClientsOnInboundDelete(tx *gorm.DB, id uint, tag string) error {
	backend := s.backend()
	return backend.UpdateClientsOnInboundDelete(tx, id, tag)
}

func (s *ClientService) UpdateLinksByInboundChange(tx *gorm.DB, inbounds *[]model.Inbound, hostname, oldTag string) error {
	backend := s.backend()
	return backend.UpdateLinksByInboundChange(tx, inbounds, hostname, oldTag)
}
