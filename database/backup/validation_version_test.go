package backup

import (
	"path/filepath"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openVersionedBackupProbe(t *testing.T, name string) *gorm.DB {
	t.Helper()
	probe, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), name)), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := probe.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := probe.AutoMigrate(&model.Setting{}); err != nil {
		t.Fatal(err)
	}
	return probe
}

func TestValidateVersionedBackupConfigSoftensMissingConfig(t *testing.T) {
	probe := openVersionedBackupProbe(t, "versioned-no-config.db")
	if err := probe.Create(&model.Setting{Key: "version", Value: "1.5.5-beta3"}).Error; err != nil {
		t.Fatal(err)
	}

	if err := validateVersionedBackupConfig(probe); err != nil {
		t.Fatalf("missing settings.config should now be a warning, not an error; got %v", err)
	}
}

func TestValidateVersionedBackupConfigUntouchedWhenConfigPresent(t *testing.T) {
	probe := openVersionedBackupProbe(t, "versioned-with-config.db")
	if err := probe.Create(&model.Setting{Key: "version", Value: "1.5.5-beta3"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := probe.Create(&model.Setting{Key: "config", Value: `{"dns":{},"route":{}}`}).Error; err != nil {
		t.Fatal(err)
	}

	if err := validateVersionedBackupConfig(probe); err != nil {
		t.Fatalf("happy path should still pass; got %v", err)
	}
}

func TestValidateVersionedBackupConfigIgnoresUnversioned(t *testing.T) {
	probe := openVersionedBackupProbe(t, "unversioned.db")

	if err := validateVersionedBackupConfig(probe); err != nil {
		t.Fatalf("unversioned backup should always pass; got %v", err)
	}
}
