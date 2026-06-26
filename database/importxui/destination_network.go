package importxui

import (
	"fmt"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"

	"gorm.io/gorm"
)

func applyInbound(tx *gorm.DB, inbound *model.Inbound, strategy Strategy, report *Report) (uint, bool, bool, error) {
	var existing model.Inbound
	err := tx.Where("tag = ?", inbound.Tag).First(&existing).Error
	if err != nil && !dbsqlite.IsNotFound(err) {
		return 0, false, false, err
	}
	if dbsqlite.IsNotFound(err) {
		sortOrder, err := nextImportSortOrder(tx, &model.Inbound{})
		if err != nil {
			return 0, false, false, err
		}
		inbound.SortOrder = sortOrder
		if err := tx.Create(inbound).Error; err != nil {
			return 0, false, false, err
		}
		return inbound.Id, true, false, nil
	}
	report.Summary.Inbounds.Conflicts++
	switch strategy {
	case StrategySkip:
		report.warn(fmt.Sprintf("inbound %s: existing tag skipped by strategy", inbound.Tag))
		return existing.Id, false, true, nil
	case StrategyReplace:
		if err := tx.Delete(&existing).Error; err != nil {
			return 0, false, false, err
		}
		inbound.Id = 0
		inbound.SortOrder = existing.SortOrder
		if err := tx.Create(inbound).Error; err != nil {
			return 0, false, false, err
		}
		return inbound.Id, true, false, nil
	default:
		inbound.Id = existing.Id
		inbound.SortOrder = existing.SortOrder
		if err := tx.Save(inbound).Error; err != nil {
			return 0, false, false, err
		}
		return inbound.Id, true, false, nil
	}
}

func applyEndpoint(tx *gorm.DB, endpoint *model.Endpoint, strategy Strategy, report *Report) (bool, error) {
	var existing model.Endpoint
	err := tx.Where("tag = ?", endpoint.Tag).First(&existing).Error
	if err != nil && !dbsqlite.IsNotFound(err) {
		return false, err
	}
	if dbsqlite.IsNotFound(err) {
		sortOrder, err := nextImportSortOrder(tx, &model.Endpoint{})
		if err != nil {
			return false, err
		}
		endpoint.SortOrder = sortOrder
		return true, tx.Create(endpoint).Error
	}
	switch strategy {
	case StrategySkip:
		report.warn(fmt.Sprintf("endpoint %s: existing tag skipped by strategy", endpoint.Tag))
		return false, nil
	case StrategyReplace:
		if err := tx.Delete(&existing).Error; err != nil {
			return false, err
		}
		endpoint.Id = 0
		endpoint.SortOrder = existing.SortOrder
		return true, tx.Create(endpoint).Error
	default:
		endpoint.Id = existing.Id
		endpoint.SortOrder = existing.SortOrder
		return true, tx.Save(endpoint).Error
	}
}
