// Package schema owns the supported durable core schema boundary.
package schema

import (
	"errors"
	"github.com/MalenkiySolovey/solovey-ui/config/versionpolicy"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/gorm"
)

const CurrentCoreVersion = "1.12"

func LegacyProjection(db *gorm.DB) (bool, error) {
	version, err := StoredCoreVersion(db)
	if err != nil {
		return false, err
	}
	comparison, ok := versionpolicy.CompareVersions(version, CurrentCoreVersion)
	if !ok || comparison > 0 {
		return false, errors.New("core schema is outside the supported projection boundary")
	}
	return comparison < 0, nil
}

// StoredCoreVersion reports the source database's boundary. Absence denotes
// the accepted pre-versioned schema, never the running binary's schema.
func StoredCoreVersion(db *gorm.DB) (string, error) {
	if db == nil {
		return "", errors.New("core schema view is unavailable")
	}
	if !db.Migrator().HasTable(&model.Setting{}) {
		return "1.7", nil
	}
	var setting model.Setting
	if err := db.Where("key = ?", "coreSchemaVersion").Limit(1).Find(&setting).Error; err != nil {
		return "", err
	}
	version := setting.Value
	if version == "" {
		version = "1.7"
	}
	return version, nil
}
