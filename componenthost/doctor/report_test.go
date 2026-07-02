package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestInspectDoesNotInstallAvailableComponentsWhenMetadataMissing(t *testing.T) {
	report := InspectWith(Options{
		Components: []registry.Component{{
			Manifest: manifest.Manifest{
				ID:             "telegram",
				Name:           "Telegram",
				Version:        "1",
				Delivery:       manifest.DeliveryInProcess,
				DefaultEnabled: true,
			},
		}},
		InstalledPath: filepath.Join(t.TempDir(), "missing.json"),
	})

	if report.MetadataPresent {
		t.Fatal("metadata should be missing")
	}
	if len(report.Rows) != 0 {
		t.Fatalf("rows = %d, want 0 for available-only components", len(report.Rows))
	}
}

func TestInspectReportsUnavailableInstalledComponent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "installed.json")
	if err := os.WriteFile(path, []byte(`{
		"version": 1,
		"binary": "full",
		"components": [
			{"id": "missing-component", "delivery": "in-process", "installed": true}
		]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	report := InspectWith(Options{InstalledPath: path})
	row := findRow(t, report, "missing-component")
	if !row.Installed || row.Available {
		t.Fatalf("row state = %#v, want installed unavailable component", row)
	}
	if !hasIssue(row.Issues, "unavailable in this binary") {
		t.Fatalf("issues = %#v, want unavailable component issue", row.Issues)
	}
	if !HasErrors(report) {
		t.Fatal("report should have an error")
	}
}

func TestInspectReportsInstalledComponentMissingPack(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "components", "installed.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{
		"version": 1,
		"components": [
			{"id": "telegram", "delivery": "in-process", "installed": true}
		]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	report := InspectWith(Options{
		Components: []registry.Component{{
			Manifest: manifest.Manifest{
				ID:       "telegram",
				Name:     "Telegram",
				Version:  "1",
				Delivery: manifest.DeliveryInProcess,
				Frontend: manifest.Frontend{
					Entries: []string{"src/views/TelegramSettings.vue"},
				},
			},
		}},
		InstalledPath: path,
	})
	row := findRow(t, report, "telegram")
	if !hasIssue(row.Issues, "pack is missing") {
		t.Fatalf("issues = %#v, want missing pack issue", row.Issues)
	}
	if !HasErrors(report) {
		t.Fatal("report should have an error")
	}
}

func TestInspectAcceptsInstalledComponentFrontendPack(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "components", "installed.json")
	packDir := filepath.Join(root, "components", "telegram")
	assetDir := filepath.Join(packDir, "frontend", "assets")
	if err := os.MkdirAll(assetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packDir, "component.json"), []byte(`{"id":"telegram"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{
		"version": 1,
		"components": [
			{"id": "telegram", "delivery": "in-process", "installed": true}
		]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	report := InspectWith(Options{
		Components: []registry.Component{{
			Manifest: manifest.Manifest{
				ID:       "telegram",
				Name:     "Telegram",
				Version:  "1",
				Delivery: manifest.DeliveryInProcess,
				Frontend: manifest.Frontend{
					Entries: []string{"src/views/TelegramSettings.vue"},
				},
			},
		}},
		InstalledPath: path,
	})
	row := findRow(t, report, "telegram")
	if hasIssue(row.Issues, "pack") || hasIssue(row.Issues, "frontend assets") {
		t.Fatalf("issues = %#v, want no pack/frontend issue", row.Issues)
	}
}

func TestInspectReportsInvalidMetadataIDEvenWhenNotInstalled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "installed.json")
	if err := os.WriteFile(path, []byte(`{
		"version": 1,
		"components": [
			{"id": "bad_component", "delivery": "in-process", "installed": false}
		]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	report := InspectWith(Options{InstalledPath: path})
	row := findRow(t, report, "bad_component")
	if !hasIssue(row.Issues, "must match") {
		t.Fatalf("issues = %#v, want invalid metadata id issue", row.Issues)
	}
	if !HasErrors(report) {
		t.Fatal("report should have an error")
	}
}

func TestInspectReportsMigrationDataForUninstalledComponent(t *testing.T) {
	db := openDoctorTestDB(t)
	if err := db.AutoMigrate(&model.ComponentMigration{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ComponentMigration{
		ComponentID: "telegram",
		Name:        "Telegram",
		Version:     "1",
		Delivery:    string(manifest.DeliveryInProcess),
		AppliedAt:   1,
	}).Error; err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "installed.json")
	if err := os.WriteFile(path, []byte(`{
		"version": 1,
		"components": [
			{"id": "telegram", "delivery": "in-process", "installed": false}
		]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	report := InspectWith(Options{
		Components: []registry.Component{{
			Manifest: manifest.Manifest{
				ID:       "telegram",
				Name:     "Telegram",
				Version:  "1",
				Delivery: manifest.DeliveryInProcess,
			},
		}},
		InstalledPath: path,
		DB:            db,
	})
	row := findRow(t, report, "telegram")
	if row.Installed {
		t.Fatalf("row state = %#v, want uninstalled component", row)
	}
	if len(row.MigrationVersions) != 1 || row.MigrationVersions[0] != "1" {
		t.Fatalf("migration versions = %#v, want [1]", row.MigrationVersions)
	}
	if !hasIssue(row.Issues, "migration data exists") {
		t.Fatalf("issues = %#v, want orphan migration issue", row.Issues)
	}
}

func TestInspectReportsInvalidEnabledSetting(t *testing.T) {
	db := openDoctorTestDB(t)
	if err := db.AutoMigrate(&model.Setting{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Setting{Key: "telegram.enabled", Value: "maybe"}).Error; err != nil {
		t.Fatal(err)
	}

	report := InspectWith(Options{
		Components: []registry.Component{{
			Manifest: manifest.Manifest{
				ID:             "telegram",
				Name:           "Telegram",
				Version:        "1",
				Delivery:       manifest.DeliveryInProcess,
				DefaultEnabled: true,
			},
		}},
		InstalledPath: filepath.Join(t.TempDir(), "missing.json"),
		DB:            db,
	})
	row := findRow(t, report, "telegram")
	if !hasIssue(row.Issues, "enabled setting is invalid") {
		t.Fatalf("issues = %#v, want invalid enabled setting issue", row.Issues)
	}
	if !HasErrors(report) {
		t.Fatal("report should have an error")
	}
}

func TestInspectReportsEnabledSettingForUnavailableComponent(t *testing.T) {
	db := openDoctorTestDB(t)
	if err := db.AutoMigrate(&model.Setting{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Setting{Key: "remote-outbound-subscriptions.enabled", Value: "true"}).Error; err != nil {
		t.Fatal(err)
	}

	report := InspectWith(Options{
		Components:    nil,
		InstalledPath: filepath.Join(t.TempDir(), "missing.json"),
		DB:            db,
	})
	row := findRow(t, report, "remote-outbound-subscriptions")
	if row.Available || row.Enabled {
		t.Fatalf("row state = %#v, want unavailable orphan enabled setting", row)
	}
	if !hasIssue(row.Issues, "enabled setting exists") {
		t.Fatalf("issues = %#v, want orphan enabled setting issue", row.Issues)
	}
}

func openDoctorTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "doctor.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func findRow(t *testing.T, report Report, id string) Row {
	t.Helper()
	for _, row := range report.Rows {
		if row.ID == id {
			return row
		}
	}
	t.Fatalf("row %q not found in %#v", id, report.Rows)
	return Row{}
}

func hasIssue(issues []Issue, fragment string) bool {
	for _, issue := range issues {
		if strings.Contains(issue.Message, fragment) {
			return true
		}
	}
	return false
}
