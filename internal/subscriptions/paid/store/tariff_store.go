package store

import (
	"encoding/json"
	"fmt"

	paid "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/paid"
	"gorm.io/gorm"
)

func ValidateTariff(t *paid.Tariff) error {
	if t.Price < 0 || t.StarsAmount < 0 || t.AddDays < 0 || t.AddTrafficBytes < 0 || t.Sort < 0 {
		return fmt.Errorf("tariff fields must not be negative")
	}
	return nil
}

func ListTariffs(db *gorm.DB) ([]paid.Tariff, error) {
	var tariffs []paid.Tariff
	if err := db.Order("sort asc, id asc").Find(&tariffs).Error; err != nil {
		return nil, err
	}
	return tariffs, nil
}

func ListEnabledTariffs(db *gorm.DB) ([]paid.Tariff, error) {
	var tariffs []paid.Tariff
	if err := db.Where("enabled = ?", true).Order("sort asc, id asc").Find(&tariffs).Error; err != nil {
		return nil, err
	}
	return tariffs, nil
}

func GetTariff(db *gorm.DB, id uint) (*paid.Tariff, error) {
	var tariff paid.Tariff
	if err := db.Where("id = ?", id).First(&tariff).Error; err != nil {
		return nil, err
	}
	return &tariff, nil
}

func SaveTariff(db *gorm.DB, action string, data json.RawMessage, now int64) error {
	switch action {
	case "new":
		var tariff paid.Tariff
		if err := json.Unmarshal(data, &tariff); err != nil {
			return err
		}
		tariff.Id = 0
		tariff.CreatedAt = now
		tariff.UpdatedAt = now
		if err := ValidateTariff(&tariff); err != nil {
			return err
		}
		return db.Create(&tariff).Error
	case "edit":
		var tariff paid.Tariff
		if err := json.Unmarshal(data, &tariff); err != nil {
			return err
		}
		if tariff.Id == 0 {
			return gorm.ErrMissingWhereClause
		}
		if err := ValidateTariff(&tariff); err != nil {
			return err
		}
		tariff.UpdatedAt = now
		return db.Model(&paid.Tariff{}).Where("id = ?", tariff.Id).Updates(map[string]any{
			"name":              tariff.Name,
			"description":       tariff.Description,
			"price":             tariff.Price,
			"currency":          tariff.Currency,
			"stars_amount":      tariff.StarsAmount,
			"add_days":          tariff.AddDays,
			"add_traffic_bytes": tariff.AddTrafficBytes,
			"sort":              tariff.Sort,
			"enabled":           tariff.Enabled,
			"updated_at":        tariff.UpdatedAt,
		}).Error
	case "del":
		var id uint
		if err := json.Unmarshal(data, &id); err != nil {
			return err
		}
		return db.Where("id = ?", id).Delete(&paid.Tariff{}).Error
	case "delbulk":
		var ids []uint
		if err := json.Unmarshal(data, &ids); err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		return db.Where("id IN ?", ids).Delete(&paid.Tariff{}).Error
	default:
		return gorm.ErrInvalidData
	}
}
