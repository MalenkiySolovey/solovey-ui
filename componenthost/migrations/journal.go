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

func DeleteRecords(db *gorm.DB, componentID string) error {
	if db == nil {
		return errors.New("component migration journal database is not initialized")
	}
	if err := manifest.ValidateID(componentID); err != nil {
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if tx.Migrator().HasTable(&model.ComponentMigration{}) {
			if err := tx.Where("component_id = ?", componentID).Delete(&model.ComponentMigration{}).Error; err != nil {
				return err
			}
		}
		if tx.Migrator().HasTable(&model.MigrationJournal{}) {
			if err := tx.Where("scope = ? AND owner_id = ?", ScopeComponent, componentID).Delete(&model.MigrationJournal{}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
