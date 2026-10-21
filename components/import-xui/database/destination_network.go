//go:build !minimal

package importxui

import (
	"fmt"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	entityendpoints "github.com/MalenkiySolovey/solovey-ui/internal/entities/endpoints"
	entityinbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds"
	"gorm.io/gorm"
)

func applyInbound(tx *gorm.DB, inbound *model.Inbound, strategy Strategy, report *Report, hostname string) (uint, bool, bool, error) {
	var existing model.Inbound
	err := tx.Where("tag = ?", inbound.Tag).First(&existing).Error
	if err != nil && !dbsqlite.IsNotFound(err) {
		return 0, false, false, err
	}
	if err == nil {
		report.Summary.Inbounds.Conflicts++
		if strategy == StrategySkip {
			report.warn(fmt.Sprintf("inbound %s: existing tag skipped by strategy", inbound.Tag))
			return existing.Id, false, true, nil
		}
		inbound.Id = existing.Id
	}
	saved, err := entityinbounds.SaveImportedCandidate(tx, *inbound, existing.Id != 0 && strategy == StrategyReplace, hostname)
	if err != nil {
		return 0, false, false, err
	}
	*inbound = saved
	return saved.Id, true, false, nil
}

func applyEndpoint(tx *gorm.DB, endpoint *model.Endpoint, strategy Strategy, report *Report) (bool, error) {
	var existing model.Endpoint
	err := tx.Where("tag = ?", endpoint.Tag).First(&existing).Error
	if err != nil && !dbsqlite.IsNotFound(err) {
		return false, err
	}
	if err == nil {
		if strategy == StrategySkip {
			report.warn(fmt.Sprintf("endpoint %s: existing tag skipped by strategy", endpoint.Tag))
			return false, nil
		}
		endpoint.Id = existing.Id
	}
	saved, err := entityendpoints.SaveImportedCandidate(tx, *endpoint, existing.Id != 0 && strategy == StrategyReplace)
	if err != nil {
		return false, err
	}
	*endpoint = saved
	return true, nil
}
