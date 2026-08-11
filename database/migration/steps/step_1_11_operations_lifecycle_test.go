package steps

import (
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestOperationsLifecycleSchemaChecksumIsReviewed(t *testing.T) {
	const reviewed = "ebc6f4aa3a1bbb9b2fca8f994efbbe2b6f0263835879a70d78737375585a9d46"
	if OperationsLifecycleChecksum != reviewed {
		t.Fatalf("1.11 schema contract changed: got %s; review it and update the pinned checksum", OperationsLifecycleChecksum)
	}
}

func TestOperationsLifecycleMigrationRepairsPartialSchemaIdempotently(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:operations-lifecycle-partial?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.UpdateOperation{}); err != nil {
		t.Fatal(err)
	}
	if err := addOperationsLifecycleSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := addOperationsLifecycleSchema(db); err != nil {
		t.Fatalf("idempotent migration failed: %v", err)
	}
	for _, table := range []string{"update_release_state_v1", "update_operations_v1", "update_journal_v1",
		"resource_pressure_state_v1", "resource_pressure_transitions_v1", "migration_journal_v1",
		"data_lifecycle_operations_v1", "data_lifecycle_journal_v1"} {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("partial migration left table %q absent", table)
		}
	}
	if !db.Migrator().HasColumn(&model.UpdateOperation{}, "binary_profile") {
		t.Fatal("partial update operation schema omitted binary_profile")
	}
	if !db.Migrator().HasColumn(&model.UpdateOperation{}, "release_id") ||
		!db.Migrator().HasColumn(&model.UpdateReleaseState{}, "release_id") {
		t.Fatal("partial update schema omitted the signed release identity")
	}
}
