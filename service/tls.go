package service

import (
	"encoding/json"

	"github.com/MalenkiySolovey/solovey-ui/database"
	"github.com/MalenkiySolovey/solovey-ui/database/model"

	"gorm.io/gorm"
)

type TlsService struct {
	InboundService
	ServicesService
	Runtime *Runtime
}

func (s *TlsService) GetAll() ([]model.Tls, error) {
	db := database.GetDB()
	tlsConfig := []model.Tls{}
	err := db.Model(model.Tls{}).Where("id > 0").Order(sortOrderClause).Scan(&tlsConfig).Error
	if err != nil {
		return nil, err
	}

	return tlsConfig, nil
}

func (s *TlsService) Save(tx *gorm.DB, action string, data json.RawMessage, hostname string) error {
	return s.applyTLSSave(tlsSaveRequest{
		tx:       tx,
		action:   action,
		data:     data,
		hostname: hostname,
	})
}
