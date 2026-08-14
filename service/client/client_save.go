package client

import (
	"encoding/json"

	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	entityclients "github.com/MalenkiySolovey/solovey-ui/internal/entities/clients"

	"gorm.io/gorm"
)

type clientSaveRequest struct {
	tx       *gorm.DB
	action   string
	data     json.RawMessage
	hostname string
}

func (s *Service) applyClientSave(req clientSaveRequest) ([]uint, error) {
	return entityclients.Save(entityclients.SaveRequest{
		Tx:        req.tx,
		Action:    req.action,
		Data:      req.data,
		Hostname:  req.hostname,
		SaveBatch: dbsqlite.SaveInBatches,
	})
}
