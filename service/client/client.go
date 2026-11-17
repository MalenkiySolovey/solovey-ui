// Package client owns client persistence, membership, links, and lifecycle operations.
package client

import (
	"encoding/json"

	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/backend/storage/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/backend/users"
	entityclients "github.com/MalenkiySolovey/solovey-ui/internal/entities/clients"
	logger "github.com/MalenkiySolovey/solovey-ui/logger"

	"gorm.io/gorm"
)

type Service struct {
	touchLastUpdate func(int64)
}

func New(setLastUpdate func(int64)) Service {
	return Service{touchLastUpdate: setLastUpdate}
}

func clientDatabase() *gorm.DB {
	return dbsqlite.DB()
}

func (s *Service) Get(id string) (*[]model.Client, error) {
	return entityclients.Get(clientDatabase(), id)
}

func (s *Service) GetWithLocalLinks(id string, hostname string) (*[]model.Client, error) {
	return entityclients.GetWithLocalLinks(clientDatabase(), id, hostname)
}

func (s *Service) GetAll() (*[]model.Client, error) {
	return entityclients.GetAll(clientDatabase())
}

func (s *Service) Save(tx *gorm.DB, act string, data json.RawMessage, hostname string) ([]uint, error) {
	return s.applyClientSave(clientSaveRequest{
		tx:       tx,
		action:   act,
		data:     data,
		hostname: hostname,
	})
}

func (s *Service) lifecycle() users.Lifecycle {
	var touch func(int64)
	if s != nil {
		touch = s.touchLastUpdate
	}
	return users.Lifecycle{TouchLastUpdate: touch, Error: logger.Error, Debug: logger.Debug, Warningf: logger.Warningf}
}
func (s *Service) DepleteClients() ([]uint, error) { return s.lifecycle().DepleteClients() }
func (s *Service) ResetClients(tx *gorm.DB, stamp int64) ([]uint, error) {
	return s.lifecycle().ResetClients(tx, stamp)
}
func (s *Service) ResetTraffic(id string) (model.Client, error) {
	return s.lifecycle().ResetTraffic(id)
}
