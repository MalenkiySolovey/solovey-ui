package service

import (
	"github.com/MalenkiySolovey/solovey-backend/storage/model"
	"gorm.io/gorm"
)

// ImportHistoricalObservations records observations without granting access or changing quotas.
func (s *StatsService) ImportHistoricalObservations(tx *gorm.DB, rows []model.Stats) error {
	return s.backend().ImportHistoricalObservations(tx, rows)
}
