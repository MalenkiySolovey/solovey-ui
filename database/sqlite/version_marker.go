package sqlite

import (
	"github.com/MalenkiySolovey/solovey-ui/config/versionpolicy"
	"gorm.io/gorm"
)

func bumpVersionSetting(tx *gorm.DB) error { return storageInitializer().BumpVersionSetting(tx) }
func compareVersion(left, right string) (int, bool) {
	return versionpolicy.CompareVersions(left, right)
}
