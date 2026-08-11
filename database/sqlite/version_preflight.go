package sqlite

import (
	"fmt"
	"os"
	"strings"

	configidentity "github.com/MalenkiySolovey/solovey-ui/config/identity"
	"github.com/MalenkiySolovey/solovey-ui/config/versionpolicy"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

type UnsupportedVersionError struct {
	Label     string
	Actual    string
	Supported string
	Malformed bool
}

func (e *UnsupportedVersionError) Error() string {
	if e.Malformed {
		return fmt.Sprintf("%s version %q is not semver-compatible", e.Label, e.Actual)
	}
	return fmt.Sprintf("%s version %q is newer than supported %q", e.Label, e.Actual, e.Supported)
}

func preflightSupportedVersion(dbPath string) error {
	dataPath := sqliteDataPath(dbPath)
	if dataPath == "" || strings.Contains(dataPath, ":memory:") {
		return nil
	}
	if _, err := os.Stat(dataPath); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	separator := "?"
	if strings.Contains(dbPath, "?") {
		separator = "&"
	}
	readOnly, err := gorm.Open(
		sqlite.Open(dbPath+separator+"mode=ro&_query_only=1&_foreign_keys=on"),
		&gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)},
	)
	if err != nil {
		return fmt.Errorf("open database version preflight: %w", err)
	}
	sqlDB, err := readOnly.DB()
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	if !readOnly.Migrator().HasTable("settings") {
		return nil
	}
	for _, check := range []struct {
		key       string
		label     string
		supported string
	}{
		{key: "version", label: "database", supported: configidentity.GetVersion()},
		{key: "coreSchemaVersion", label: "core schema", supported: "1.11"},
	} {
		var actual string
		if err := readOnly.Raw("SELECT value FROM settings WHERE key = ? LIMIT 1", check.key).Scan(&actual).Error; err != nil {
			return fmt.Errorf("read %s version preflight: %w", check.label, err)
		}
		if actual == "" {
			continue
		}
		cmp, ok := versionpolicy.CompareVersions(actual, check.supported)
		if !ok {
			return &UnsupportedVersionError{Label: check.label, Actual: actual, Supported: check.supported, Malformed: true}
		}
		if cmp > 0 {
			return &UnsupportedVersionError{Label: check.label, Actual: actual, Supported: check.supported}
		}
	}
	return nil
}

// ValidateSupportedVersionFile performs the same read-only compatibility gate
// for a staged restore before the live database is closed or renamed.
func ValidateSupportedVersionFile(dbPath string) error {
	return preflightSupportedVersion(dbPath)
}
