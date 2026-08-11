package backup

import (
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
)

func TestSSHManagementBackupIncludesSafeMetadataAndExcludesRecoverySecrets(t *testing.T) {
	dbPath := initBackupContributionDB(t)
	db := dbsqlite.DB()
	digest := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	candidate := model.SSHManagementCandidate{OperationID: "ssh-operation:test", Scope: "global", IdempotencyKey: "idem-backup", State: "COMMITTED", Revision: 8,
		PolicyJSON: []byte(`{"schema":"solovey-ui/ssh-managed-policy/v1","permitRootLogin":"UNCHANGED"}`), PreservationJSON: []byte(`{"safe":true}`),
		CandidateDigest: digest, BindingDigest: digest, PostureRevision: digest, EndpointRevision: digest, RecoveryRevision: digest,
		ProviderRevision: digest, BinaryRevision: digest, ServiceRevision: digest, ConfigurationRevision: digest,
		ReasonCodesJSON: []byte(`[]`), ReconciledAt: 100, CreatedAt: 1, UpdatedAt: 100}
	if err := db.Create(&candidate).Error; err != nil {
		t.Fatal(err)
	}
	checkpoint := model.SSHManagedArtifactCheckpoint{OperationID: candidate.OperationID, PriorPresent: true, PriorContent: []byte("sensitive exact prior bytes"),
		PriorOwner: "root", PriorGroup: "root", PriorModeClass: "owner_read_write", PriorDigest: digest, StagedArtifactDigest: digest, CreatedAt: 1}
	if err := db.Create(&checkpoint).Error; err != nil {
		t.Fatal(err)
	}
	challenge := model.SSHReconnectChallenge{OperationID: candidate.OperationID, CandidateDigest: digest, MarkerDigest: digest,
		EndpointID: "management:ssh:test", PrincipalID: "principal:test", AuthenticationClass: "publickey", ServiceRevision: digest,
		BinaryRevision: digest, ConfigurationRevision: digest, VerifierDigest: digest, IssuedAt: 1, ExpiresAt: 2, Revision: 1}
	if err := db.Create(&challenge).Error; err != nil {
		t.Fatal(err)
	}

	backupDB := openContributionBackup(t, dbPath)
	var count int64
	if err := backupDB.Model(&model.SSHManagementCandidate{}).Where("operation_id = ?", candidate.OperationID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("safe candidate count=%d err=%v", count, err)
	}
	if backupDB.Migrator().HasTable(&model.SSHManagedArtifactCheckpoint{}) || backupDB.Migrator().HasTable(&model.SSHReconnectChallenge{}) {
		t.Fatal("artifact checkpoint or reconnect challenge leaked into backup")
	}
}
