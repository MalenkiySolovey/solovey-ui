package steps

import (
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestSSHManagementMigrationIsIdempotentAndAllowsOnlyOneActiveCandidate(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	digest := strings.Repeat("a", 64)
	if err := addSSHManagementRecoverySchema(db); err != nil {
		t.Fatal(err)
	}
	if err := addSSHManagementRecoverySchema(db); err != nil {
		t.Fatalf("idempotent rerun: %v", err)
	}
	for _, table := range []any{&model.SSHPostureSnapshot{}, &model.SSHManagementCandidate{}, &model.SSHManagedArtifactCheckpoint{}, &model.SSHReconnectChallenge{}, &model.SSHRecoveryEvidence{}, &model.SSHManagementJournal{}} {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("migration omitted %T", table)
		}
	}
	row := model.SSHManagementCandidate{OperationID: "one", Scope: "global", IdempotencyKey: "idem-one", State: "DRAFT", Revision: 1,
		PolicyJSON: []byte(`{}`), PreservationJSON: []byte(`{}`), CandidateDigest: digest, BindingDigest: digest,
		PostureRevision: digest, EndpointRevision: digest, RecoveryRevision: digest, ProviderRevision: digest,
		BinaryRevision: digest, ServiceRevision: digest, ConfigurationRevision: digest, ReasonCodesJSON: []byte(`[]`), CreatedAt: 1, UpdatedAt: 1}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	row.OperationID, row.IdempotencyKey = "two", "idem-two"
	if err := db.Create(&row).Error; err == nil {
		t.Fatal("second active global SSH candidate bypassed the unique fence")
	}
}
