// Package netentity composes network entity persistence with live core updates.
package netentity

import (
	"encoding/json"

	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	entityendpoints "github.com/MalenkiySolovey/solovey-ui/internal/entities/endpoints"
	singboxapply "github.com/MalenkiySolovey/solovey-ui/internal/singbox/apply"

	"gorm.io/gorm"
)

type EndpointService struct {
	Core entityendpoints.Core
	Warp entityendpoints.WarpHooks
}

func (o *EndpointService) GetAll() (*[]map[string]interface{}, error) {
	return entityendpoints.GetAll(dbsqlite.DB())
}

func (o *EndpointService) GetAllConfig(db *gorm.DB) ([]json.RawMessage, error) {
	return entityendpoints.GetAllConfig(db)
}

func (s *EndpointService) Save(tx *gorm.DB, act string, data json.RawMessage) error {
	_, err := s.SaveWithCoreChange(tx, act, data)
	return err
}

func (s *EndpointService) SaveWithCoreChange(tx *gorm.DB, act string, data json.RawMessage) (*singboxapply.Change, error) {
	return entityendpoints.Save(tx, act, data, s.Warp)
}

func (s *EndpointService) RestartEndpoints(tx *gorm.DB, ids []uint) error {
	return entityendpoints.Restart(tx, ids, s.Core)
}

func (s *EndpointService) RemoveEndpointsFromCore(tags []string) error {
	return entityendpoints.RemoveFromCore(tags, s.Core)
}
