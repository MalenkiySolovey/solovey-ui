package migrations

import (
	"errors"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func EnsureJournal(db *gorm.DB) error {
	if db == nil {
		return errors.New("component migration journal database is not initialized")
	}
	return db.AutoMigrate(&model.ComponentMigration{})
}

func RecordApplied(db *gorm.DB, item manifest.Manifest) error {
	if db == nil {
		return errors.New("component migration journal database is not initialized")
	}
	if err := item.Validate(); err != nil {
		return err
	}
	record := model.ComponentMigration{
		ComponentID: item.ID,
		Name:        item.Name,
		Version:     item.Version,
		Delivery:    string(item.Delivery),
		AppliedAt:   time.Now().Unix(),
	}
	return db.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "component_id"},
			{Name: "version"},
		},
		DoUpdates: clause.Assignments(map[string]any{
			"name":       record.Name,
			"delivery":   record.Delivery,
			"applied_at": record.AppliedAt,
		}),
	}).Create(&record).Error
}
