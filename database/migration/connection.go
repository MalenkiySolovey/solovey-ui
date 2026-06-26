package migration

import (
	"strings"

	"gorm.io/gorm"
)

func sqliteMigrationDSN(path string) string {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return path + sep + "_busy_timeout=10000&_foreign_keys=on"
}

func checkpointWAL(db *gorm.DB) error {
	return db.Exec("PRAGMA wal_checkpoint(FULL)").Error
}
