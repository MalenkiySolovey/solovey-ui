package backup

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	validation "github.com/MalenkiySolovey/solovey-ui/internal/singbox/validation"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRestoreRuleCompatibilityPreservesBackup(t *testing.T) {
	for _, name := range []string{"route-and", "dns-or", "route-conflicting", "dns-action-only"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "internal", "singbox", "validation", "testdata", "nested_rules_v2026.3.3", name+".json"))
		if err != nil {
			t.Fatal(err)
		}
		probe := openVersionedBackupProbe(t, name+".db")
		if err := probe.Create(&model.Setting{Key: "version", Value: "2026.3.3"}).Error; err != nil {
			t.Fatal(err)
		}
		if err := probe.Create(&model.Setting{Key: "config", Value: string(data)}).Error; err != nil {
			t.Fatal(err)
		}
		_, expected := validation.PrepareRuleUpgrade(data)
		for attempt := 0; attempt < 2; attempt++ {
			err := validateVersionedBackupConfig(probe)
			if (err != nil) != (expected != nil) {
				t.Fatal("restore diverges from upgrade owner", err)
			}
			if err != nil {
				var actual, want *validation.RuleConditionError
				if !errors.As(err, &actual) || !errors.As(expected, &want) || actual.Finding.Path != want.Finding.Path || actual.Finding.Code != want.Finding.Code {
					t.Fatal("restore lost stable diagnostic")
				}
			}
			var row model.Setting
			if err := probe.Where("key = ?", "config").First(&row).Error; err != nil || row.Value != string(data) {
				t.Fatal("backup lost pre-image", err)
			}
		}
	}
}

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
