// Package schema owns the supported durable core schema boundary.
package schema

import (
	"errors"
	storageschema "github.com/MalenkiySolovey/solovey-ui/backend/storage/schema"
	"github.com/MalenkiySolovey/solovey-ui/config/versionpolicy"
	"gorm.io/gorm"
)

const CurrentCoreVersion = storageschema.CurrentCoreVersion

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

// StoredCoreVersion uses the canonical durable schema reader.
func StoredCoreVersion(db *gorm.DB) (string, error) { return storageschema.StoredCoreVersion(db) }
