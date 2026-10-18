package datalifecycle

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestDataTransitionFailureReturnsPreviousDurableTruth(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "data.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	t.Cleanup(func() { _ = pool.Close() })
	// Journal deliberately absent, representing an actual durable write failure.
	if err := db.AutoMigrate(&model.DataLifecycleOperation{}); err != nil {
		t.Fatal(err)
	}
	previous := model.DataLifecycleOperation{OperationID: "data-operation:fixture", IdempotencyKey: "fixture", Kind: "RESTORE", OwnerID: "core", State: "ADMITTED", Revision: 1, CreatedAt: 100, UpdatedAt: 100}
	if err := db.Create(&previous).Error; err != nil {
		t.Fatal(err)
	}
	manager := &Manager{DB: func() *gorm.DB { return db }, Now: func() time.Time { return time.Unix(101, 0) }}
	result, err := manager.advance(context.Background(), previous, "RESTORING", "restore_started", "")
	if err == nil || !reflect.DeepEqual(result, previous) {
		t.Fatalf("failed write projected an uncommitted state: %+v %v", result, err)
	}
	var persisted model.DataLifecycleOperation
	if err := db.First(&persisted).Error; err != nil || !reflect.DeepEqual(persisted, previous) {
		t.Fatalf("journal failure changed durable state: %+v %v", persisted, err)
	}
}

func TestChangeRejectsUntrustedCheckpointAndMalformedReferenceMaterial(t *testing.T) {
	operation := model.DataLifecycleOperation{OperationID: "data-operation:fixture", Kind: "RESTORE", OwnerID: "core",
		State: "APPLIED", Revision: 3, ExpectedRevision: "Bearer deterministic-fake-secret",
		BackupRef: "Bearer deterministic-fake-secret", ReasonCode: "Bearer deterministic-fake-secret"}
	change := ProjectChange(operation)
	payload, err := json.Marshal(change)
	if err != nil {
		t.Fatal(err)
	}
	if !change.RecoveryRequired || change.RollbackVerified || change.CheckpointRef != "" || strings.Contains(string(payload), "deterministic-fake-secret") {
		t.Fatalf("untrusted authority escaped safe recovery projection: %s", payload)
	}
}
