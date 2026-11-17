// Package schema owns the supported durable Panel schema identity and stored boundary.
package schema

import (
	"errors"
	"github.com/MalenkiySolovey/solovey-ui/backend/storage/model"
	"gorm.io/gorm"
)

const CurrentCoreVersion = "1.13"

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
