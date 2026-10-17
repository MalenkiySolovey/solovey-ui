// Package schema owns the supported durable core schema boundary.
package schema

import (
	"errors"
	"github.com/MalenkiySolovey/solovey-ui/config/versionpolicy"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/gorm"
)

const CurrentCoreVersion = "1.13"

// LegacyProjection selects the pre-1.12 sing-box stored-state projection.
// Later additive Client migrations do not turn accepted 1.12 runtime data
// back into legacy input. CurrentCoreVersion remains the future-schema fence.
func LegacyProjection(db *gorm.DB) (bool, error) {
	version, err := StoredCoreVersion(db)
	if err != nil {
		return false, err
	}
	comparison, ok := versionpolicy.CompareVersions(version, CurrentCoreVersion)
	if !ok || comparison > 0 {
		return false, errors.New("core schema is outside the supported projection boundary")
	}
	projectionComparison, _ := versionpolicy.CompareVersions(version, "1.12")
	return projectionComparison < 0, nil
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
