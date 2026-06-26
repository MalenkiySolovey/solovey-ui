package sqlite

import (
	configidentity "github.com/MalenkiySolovey/solovey-ui/config/identity"
	"github.com/MalenkiySolovey/solovey-ui/config/versionpolicy"
	"github.com/MalenkiySolovey/solovey-ui/database/model"

	"gorm.io/gorm"
)

// bumpVersionSetting writes the current build version into the `settings`
// table so the migration runner does not re-run already-applied migrations on
// the next start.
func bumpVersionSetting(tx *gorm.DB) error {
	current := configidentity.GetVersion()
	if current == "" {
		return nil
	}
	var existing model.Setting
	err := tx.Model(model.Setting{}).Where("key = ?", "version").First(&existing).Error
	if IsNotFound(err) {
		return tx.Create(&model.Setting{Key: "version", Value: current}).Error
	}
	if err != nil {
		return err
	}
	cmp, ok := compareVersion(existing.Value, current)
	if ok && cmp >= 0 {
		return nil
	}
	if existing.Value == current {
		return nil
	}
	return tx.Model(model.Setting{}).Where("key = ?", "version").Update("value", current).Error
}

func compareVersion(left string, right string) (int, bool) {
	return versionpolicy.CompareVersions(left, right)
}
