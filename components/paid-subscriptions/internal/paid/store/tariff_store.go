package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	paid "github.com/MalenkiySolovey/solovey-ui/components/paid-subscriptions/internal/paid"
	"gorm.io/gorm"
)

func ValidateTariff(t *paid.Tariff) error {
	if t.Price < 0 || t.StarsAmount < 0 || t.AddDays < 0 || t.AddTrafficBytes < 0 || t.Sort < 0 {
		return fmt.Errorf("tariff fields must not be negative")
	}
	t.Name = strings.TrimSpace(t.Name)
	if t.Name == "" || len([]rune(t.Name)) > 120 {
		return fmt.Errorf("tariff name must contain 1 to 120 characters")
	}
	if len([]rune(t.Description)) > 2000 {
		return fmt.Errorf("tariff description is too long")
	}
	t.Currency = strings.ToUpper(strings.TrimSpace(t.Currency))
	if t.Currency == "" {
		t.Currency = "RUB"
	}
	if len(t.Currency) != 3 || !asciiLetters(t.Currency) {
		return fmt.Errorf("tariff currency must be a 3-letter code")
	}
	if t.Price > 1_000_000_000_000_000 || t.StarsAmount > 1_000_000_000 || t.AddDays > 36500 || t.AddTrafficBytes > 1<<60 || t.Sort > 1_000_000 {
		return fmt.Errorf("tariff fields exceed supported limits")
	}
	return nil
}

func asciiLetters(value string) bool {
	for _, char := range value {
		if char < 'A' || char > 'Z' {
			return false
		}
	}
	return true
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
		if err := decodeTariffJSON(data, &tariff); err != nil {
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
		if err := decodeTariffJSON(data, &tariff); err != nil {
			return err
		}
		if tariff.Id == 0 {
			return gorm.ErrMissingWhereClause
		}
		if err := ValidateTariff(&tariff); err != nil {
			return err
		}
		tariff.UpdatedAt = now
		result := db.Model(&paid.Tariff{}).Where("id = ?", tariff.Id).Updates(map[string]any{
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
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		return nil
	case "del":
		var id uint
		if err := decodeTariffJSON(data, &id); err != nil {
			return err
		}
		if id == 0 {
			return gorm.ErrMissingWhereClause
		}
		return deleteTariffsWithoutOrders(db, []uint{id})
	case "delbulk":
		var ids []uint
		if err := decodeTariffJSON(data, &ids); err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		if len(ids) > 1000 {
			return fmt.Errorf("too many tariffs requested")
		}
		for _, id := range ids {
			if id == 0 {
				return fmt.Errorf("tariff id is required")
			}
		}
		return deleteTariffsWithoutOrders(db, ids)
	default:
		return gorm.ErrInvalidData
	}
}

func deleteTariffsWithoutOrders(db *gorm.DB, ids []uint) error {
	var orders int64
	if err := db.Model(&paid.PaymentOrder{}).Where("tariff_id IN ?", ids).Count(&orders).Error; err != nil {
		return err
	}
	if orders > 0 {
		return fmt.Errorf("tariffs referenced by payment history cannot be deleted")
	}
	return db.Where("id IN ?", ids).Delete(&paid.Tariff{}).Error
}

func decodeTariffJSON(data []byte, out any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("tariff payload contains multiple JSON documents")
		}
		return err
	}
	return nil
}
