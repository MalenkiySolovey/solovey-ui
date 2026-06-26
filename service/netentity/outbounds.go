package netentity

import (
	"encoding/json"

	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	entityoutbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/outbounds"
	singboxapply "github.com/MalenkiySolovey/solovey-ui/internal/singbox/apply"

	"gorm.io/gorm"
)

type OutboundService struct {
	Core entityoutbounds.Core
}

func (o *OutboundService) GetAll() (*[]map[string]interface{}, error) {
	return entityoutbounds.GetAll(dbsqlite.DB())
}

func (o *OutboundService) GetAllConfig(db *gorm.DB) ([]json.RawMessage, error) {
	return entityoutbounds.GetAllConfig(db)
}

func (s *OutboundService) Save(tx *gorm.DB, act string, data json.RawMessage) error {
	_, err := s.SaveWithCoreChange(tx, act, data)
	return err
}

func (s *OutboundService) SaveWithCoreChange(tx *gorm.DB, act string, data json.RawMessage) (*singboxapply.Change, error) {
	return entityoutbounds.Save(tx, act, data)
}

func (s *OutboundService) RestartOutbounds(tx *gorm.DB, ids []uint) error {
	return entityoutbounds.Restart(tx, ids, s.Core)
}

func (s *OutboundService) RemoveOutboundsFromCore(tags []string) error {
	return entityoutbounds.RemoveFromCore(tags, s.Core)
}
