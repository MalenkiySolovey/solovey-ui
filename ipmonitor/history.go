package ipmonitor

import (
	"github.com/MalenkiySolovey/solovey-ui/database/model"
)

func History(clientName string, limit int) ([]model.ClientIP, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := loadHistoryRows(clientName, limit)
	if err == nil {
		prepareHistoryRows(rows)
	}
	return rows, err
}

func Clear(clientName string) error {
	if err := clearHistory(clientName); err != nil {
		return err
	}
	invalidateCache(clientName)
	return nil
}
