package service

import (
	"errors"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"gorm.io/gorm"
)

// ImportHistoricalObservations records the source's aggregate observations.
// It never adds them to local quota/lifetime counters or grants access.
func (s *StatsService) ImportHistoricalObservations(tx *gorm.DB, observations []model.Stats) error {
	if tx == nil {
		return errors.New("historical statistics persistence is unavailable")
	}
	for _, observation := range observations {
		if (observation.Resource != "client" && observation.Resource != "outbound") ||
			observation.Tag == "" || observation.DateTime <= 0 || observation.Traffic <= 0 {
			return errors.New("invalid imported historical observation")
		}
	}
	if len(observations) == 0 {
		return nil
	}
	return dbsqlite.CreateInBatches(tx, &observations)
}
