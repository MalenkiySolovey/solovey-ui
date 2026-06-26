package netentity

import (
	"encoding/json"

	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	entityservices "github.com/MalenkiySolovey/solovey-ui/internal/entities/services"
	singboxapply "github.com/MalenkiySolovey/solovey-ui/internal/singbox/apply"

	"gorm.io/gorm"
)

type ServicesService struct {
	Core entityservices.Core
}

func (s *ServicesService) GetAll() (*[]map[string]interface{}, error) {
	return entityservices.GetAll(dbsqlite.DB())
}

func (s *ServicesService) GetAllConfig(db *gorm.DB) ([]json.RawMessage, error) {
	return entityservices.GetAllConfig(db)
}

func (s *ServicesService) Save(tx *gorm.DB, act string, data json.RawMessage) error {
	_, err := s.SaveWithCoreChange(tx, act, data)
	return err
}

func (s *ServicesService) SaveWithCoreChange(tx *gorm.DB, act string, data json.RawMessage) (*singboxapply.Change, error) {
	return entityservices.Save(tx, act, data)
}

func (s *ServicesService) RemoveServicesFromCore(tags []string) error {
	return entityservices.RemoveFromCore(tags, s.Core)
}

func (s *ServicesService) RestartServices(tx *gorm.DB, ids []uint) error {
	return entityservices.Restart(tx, ids, s.Core)
}
