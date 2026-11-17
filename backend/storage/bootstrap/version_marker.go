package bootstrap

import (
	"errors"

	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/backend/storage/sqlite"

	"gorm.io/gorm"
)

// BumpVersionSetting writes the current build version into the `settings`
// table so the migration runner does not re-run already-applied migrations on
// the next start.
func (i Initializer) BumpVersionSetting(tx *gorm.DB) error {
	if i.CompareVersions == nil {
		return errors.New("storage version policy is unavailable")
	}
	current := i.ApplicationVersion
	if current == "" {
		return nil
	}
	var existing model.Setting
	err := tx.Model(model.Setting{}).Where("key = ?", "version").First(&existing).Error
	if dbsqlite.IsNotFound(err) {
		return tx.Create(&model.Setting{Key: "version", Value: current}).Error
	}
	if err != nil {
		return err
	}
	cmp, ok := i.CompareVersions(existing.Value, current)
	if ok && cmp >= 0 {
		return nil
	}
	if existing.Value == current {
		return nil
	}
	return tx.Model(model.Setting{}).Where("key = ?", "version").Update("value", current).Error
}
