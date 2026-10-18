package migrations

import (
	"path/filepath"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRemovalRetainsNonterminalOrIncompatibleMigrationOwner(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "removal.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	const id = "test-removal-journal"
	if code := RemovalBlocker(t.Context(), db, id); code != "" {
		t.Fatal(code)
	}
	if err := db.AutoMigrate(&model.MigrationJournal{}); err != nil {
		t.Fatal(err)
	}
	row := model.MigrationJournal{Scope: ScopeComponent, OwnerID: id, StepID: "schema:1", Checksum: "test"}
	for _, status := range []struct {
		state, compatibility string
		blocked              bool
	}{
		{"RUNNING", "COMPATIBLE", true}, {"FAILED", "COMPATIBLE", true}, {"RECOVERY_REQUIRED", "CHECKSUM_MISMATCH", true},
		{"APPLIED", "UNKNOWN", true}, {"APPLIED", "COMPATIBLE", false},
	} {
		row.State, row.CompatibilityState = status.state, status.compatibility
		if err := db.Save(&row).Error; err != nil {
			t.Fatal(err)
		}
		if blocked := RemovalBlocker(t.Context(), db, id) != ""; blocked != status.blocked {
			t.Fatalf("state=%+v blocked=%v", status, blocked)
		}
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	if code := RemovalBlocker(t.Context(), db, id); code != "component_migration_state_unavailable" {
		t.Fatalf("closed DB looked absent: %q", code)
	}
}
