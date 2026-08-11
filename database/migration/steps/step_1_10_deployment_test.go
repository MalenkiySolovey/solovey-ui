package steps

import (
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestDeploymentMigrationIsIdempotentAndFencesOneUnresolvedOperation(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := addDeploymentProfileSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := addDeploymentProfileSchema(db); err != nil {
		t.Fatalf("idempotent rerun: %v", err)
	}
	for _, table := range []any{&model.DeploymentState{}, &model.DeploymentOperation{}, &model.DeploymentJournal{}, &model.DeploymentDoctorSnapshot{}} {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("migration omitted %T", table)
		}
	}
	digest := strings.Repeat("a", 64)
	row := model.DeploymentOperation{OperationID: "deployment-operation:one", IdempotencyKey: "deployment-idem-one", State: "APPLYING",
		FromProfile: "native-legacy-root", TargetProfile: "native-hardened", ExpectedPosture: digest, ExpectedManagement: digest,
		Revision: 3, CreatedAt: 1, UpdatedAt: 1, ReasonsJSON: []byte(`[]`), BindingRevision: digest}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	row.OperationID, row.IdempotencyKey = "deployment-operation:two", "deployment-idem-two"
	if err := db.Create(&row).Error; err == nil {
		t.Fatal("second unresolved deployment operation bypassed the database fence")
	}
	if err := db.Model(&model.DeploymentOperation{}).Where("operation_id = ?", "deployment-operation:one").Update("state", "MANUAL_RECOVERY_REQUIRED").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&row).Error; err == nil {
		t.Fatal("manual recovery authority did not retain the database fence")
	}
	if err := db.Model(&model.DeploymentOperation{}).Where("operation_id = ?", "deployment-operation:one").Update("state", "ROLLED_BACK").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("terminal reconciled operation did not release the fence: %v", err)
	}
}
