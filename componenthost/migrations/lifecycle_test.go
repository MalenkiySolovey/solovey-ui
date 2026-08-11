package migrations

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestComponentMigrationAdmissionRetriesInterruptionSeriallyAndPinsChecksum(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "lifecycle.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&model.MigrationJournal{}); err != nil {
		t.Fatal(err)
	}
	step := Step{OwnerID: "fixture-owner", StepID: "component-schema:1", Checksum: strings.Repeat("a", 64), Version: "1"}
	run, err := BeginStep(db, step)
	if err != nil || !run {
		t.Fatalf("first admission run=%v err=%v", run, err)
	}
	if run, err := BeginStep(db, step); err == nil || run {
		t.Fatalf("concurrent admission run=%v err=%v", run, err)
	}
	if err := FinishStep(db, step, errors.New("fixture failure")); err != nil {
		t.Fatal(err)
	}
	run, err = BeginStep(db, step)
	if err != nil || !run {
		t.Fatalf("failed matching step was not restartable run=%v err=%v", run, err)
	}
	if err := FinishStep(db, step, nil); err != nil {
		t.Fatal(err)
	}
	run, err = BeginStep(db, step)
	if err != nil || run {
		t.Fatalf("applied step replay run=%v err=%v", run, err)
	}
	drifted := step
	drifted.Checksum = strings.Repeat("b", 64)
	if run, err := BeginStep(db, drifted); err == nil || run {
		t.Fatalf("checksum drift run=%v err=%v", run, err)
	}
	var row model.MigrationJournal
	if err := db.First(&row, "scope=? AND owner_id=? AND step_id=?", ScopeComponent, step.OwnerID, step.StepID).Error; err != nil {
		t.Fatal(err)
	}
	if row.State != "RECOVERY_REQUIRED" || row.CompatibilityState != "CHECKSUM_MISMATCH" || row.Checksum != step.Checksum {
		t.Fatalf("checksum drift posture=%#v", row)
	}
}
