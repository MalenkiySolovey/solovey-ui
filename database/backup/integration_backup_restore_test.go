package backup_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	configidentity "github.com/MalenkiySolovey/solovey-ui/config/identity"
	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	dbbackup "github.com/MalenkiySolovey/solovey-ui/database/backup"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	backupenvelope "github.com/MalenkiySolovey/solovey-ui/internal/backup/envelope"
	deploymentdomain "github.com/MalenkiySolovey/solovey-ui/internal/deployment"
	domain "github.com/MalenkiySolovey/solovey-ui/internal/sshmanagement"
	"github.com/MalenkiySolovey/solovey-ui/service"
	deploymentservice "github.com/MalenkiySolovey/solovey-ui/service/deployment"
	sshservice "github.com/MalenkiySolovey/solovey-ui/service/sshmanagement"

	gormsqlite "gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type integrationMemMultipartFile struct{ *bytes.Reader }

func (integrationMemMultipartFile) Close() error { return nil }

func TestIntegrationBackupEnvelopeRestorePreservesBackupTableCounts(t *testing.T) {
	initBackupRestoreIntegrationDB(t)
	if _, err := (&service.SettingService{}).GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	seedBackupRestoreTables(t)
	before := integrationBackupTableCounts(t)

	backup, err := dbbackup.Export("")
	if err != nil {
		t.Fatal(err)
	}
	passphrase := []byte("correct horse battery staple")
	envelope, err := backupenvelope.Build(backup, passphrase)
	if err != nil {
		t.Fatal(err)
	}
	plaintext, err := backupenvelope.Open(envelope, passphrase)
	if err != nil {
		t.Fatal(err)
	}

	if err := dbsqlite.DB().Create(&model.Client{
		Enable:    true,
		Name:      "integration-live-after-backup",
		SubSecret: "integration-live-after-backup-secret",
		Inbounds:  []byte("[]"),
		Links:     []byte("[]"),
	}).Error; err != nil {
		t.Fatal(err)
	}
	dbbackup.SetSendSighupHook(func() error { return nil })
	t.Cleanup(func() { dbbackup.SetSendSighupHook(nil) })

	if err := dbbackup.Restore(integrationMemMultipartFile{Reader: bytes.NewReader(plaintext)}); err != nil {
		t.Fatalf("Restore returned error: %v", err)
	}
	after := integrationBackupTableCounts(t)
	expected := maps.Clone(before)
	// Restore preserves the backed-up rows and completes both new audit records
	// before returning, while the imported candidate still owns the database.
	expected["audit_events"] += 2
	var restoreAuditCount int64
	if err := dbsqlite.DB().Model(&model.AuditEvent{}).Where("event = ?", "db_restore_post_actions").Count(&restoreAuditCount).Error; err != nil || restoreAuditCount != 1 {
		t.Fatalf("restore audit count=%d, want 1: %v", restoreAuditCount, err)
	}
	var rotationAuditCount int64
	if err := dbsqlite.DB().Model(&model.AuditEvent{}).Where("event = ?", "ws_tokens_invalidated").Count(&rotationAuditCount).Error; err != nil || rotationAuditCount != 1 {
		t.Fatalf("rotation audit count=%d, want 1: %v", rotationAuditCount, err)
	}
	if !reflect.DeepEqual(expected, after) {
		t.Fatalf("backup table counts changed after restore:\nbefore=%v\nexpected=%v\nafter=%v", before, expected, after)
	}
}

func TestRetentionSSHRetentionBoundsBackupAndRestoreGeneration(t *testing.T) {
	initBackupRestoreIntegrationDB(t)
	if _, err := (&service.SettingService{}).GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	for index := 0; index < sshservice.HistoryRetentionPolicy().TerminalOperations+40; index++ {
		operationID := fmt.Sprintf("ssh-operation:retention-backup-%03d", index)
		candidate := model.SSHManagementCandidate{OperationID: operationID, Scope: "global", IdempotencyKey: "idem:" + operationID,
			EndpointID: "management:ssh:retention", State: string(domain.StateCommitted), Revision: 2,
			PolicyJSON: []byte(`{"schema":"retention"}`), PreservationJSON: []byte(`{"schema":"retention"}`), CandidateDigest: domain.Revision(operationID),
			BindingDigest: domain.Revision("binding:" + operationID), PostureRevision: domain.Revision("posture"), EndpointRevision: domain.Revision("endpoint"),
			RecoveryRevision: domain.Revision("recovery"), ProviderRevision: domain.Revision("provider"), BinaryRevision: domain.Revision("binary"),
			ServiceRevision: domain.Revision("service"), ConfigurationRevision: domain.Revision("configuration"), BrokerStageReleased: true,
			ReasonCodesJSON: []byte(`[]`), CreatedAt: now.Add(-time.Duration(index) * time.Second).Unix(), UpdatedAt: now.Add(-time.Duration(index) * time.Second).Unix()}
		if err := dbsqlite.DB().Create(&candidate).Error; err != nil {
			t.Fatal(err)
		}
		journal := model.SSHManagementJournal{OperationID: operationID, Sequence: 2, State: candidate.State, Event: "candidate_committed",
			Revision: domain.Revision(operationID + ":journal"), CreatedAt: candidate.UpdatedAt}
		if err := dbsqlite.DB().Create(&journal).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := sshservice.Shared().Repository.PruneHistory(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	policy := sshservice.HistoryRetentionPolicy()
	var liveCount int64
	if err := dbsqlite.DB().Model(&model.SSHManagementCandidate{}).Count(&liveCount).Error; err != nil || liveCount != int64(policy.TerminalOperations) {
		t.Fatalf("live retained candidates=%d err=%v", liveCount, err)
	}
	backupBytes, err := dbbackup.Export("")
	if err != nil {
		t.Fatal(err)
	}
	if len(backupBytes) > policy.TerminalBytes+(4<<20) {
		t.Fatalf("bounded SSH backup bytes=%d terminal horizon=%d", len(backupBytes), policy.TerminalBytes)
	}
	backupPath := filepath.Join(t.TempDir(), "retention-bounded-backup.db")
	if err := os.WriteFile(backupPath, backupBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	backupDB, err := gorm.Open(gormsqlite.Open(backupPath), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	var backupCount int64
	if err := backupDB.Model(&model.SSHManagementCandidate{}).Count(&backupCount).Error; err != nil || backupCount != int64(policy.TerminalOperations) {
		t.Fatalf("backup retained candidates=%d err=%v", backupCount, err)
	}
	if sqlDB, err := backupDB.DB(); err != nil {
		t.Fatal(err)
	} else if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	dbbackup.SetSendSighupHook(func() error { return nil })
	t.Cleanup(func() { dbbackup.SetSendSighupHook(nil) })
	if err := dbbackup.Restore(integrationMemMultipartFile{Reader: bytes.NewReader(backupBytes)}); err != nil {
		t.Fatal(err)
	}
	var restoredCount, restoredUntrusted int64
	if err := dbsqlite.DB().Model(&model.SSHManagementCandidate{}).Count(&restoredCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Model(&model.SSHManagementCandidate{}).Where("restored_untrusted = ?", true).Count(&restoredUntrusted).Error; err != nil {
		t.Fatal(err)
	}
	if restoredCount != int64(policy.TerminalOperations) || restoredUntrusted != restoredCount {
		t.Fatalf("restored retained candidates=%d untrusted=%d", restoredCount, restoredUntrusted)
	}
}

func TestIntegrationRestoreInvalidCandidateLeavesLiveUntouched(t *testing.T) {
	initBackupRestoreIntegrationDB(t)
	if _, err := (&service.SettingService{}).GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Create(&model.Setting{Key: "restore_marker", Value: "live-before-import"}).Error; err != nil {
		t.Fatal(err)
	}
	dbbackup.SetSendSighupHook(func() error { return nil })
	t.Cleanup(func() { dbbackup.SetSendSighupHook(nil) })

	err := dbbackup.Restore(integrationMemMultipartFile{Reader: bytes.NewReader(newIntegrationForeignKeyBrokenBackup(t))})
	if err == nil || !strings.Contains(err.Error(), "backup_manifest_invalid") {
		t.Fatalf("expected strict manifest rehearsal rejection, got %v", err)
	}
	if dbsqlite.DB() == nil {
		t.Fatal("live DB handle was lost after rehearsal rejection")
	}
	if sqlDB, dbErr := dbsqlite.DB().DB(); dbErr != nil {
		t.Fatalf("live DB handle error after rollback: %v", dbErr)
	} else if pingErr := sqlDB.Ping(); pingErr != nil {
		t.Fatalf("live DB ping failed after rollback: %v", pingErr)
	}
	var marker string
	if err := dbsqlite.DB().Model(&model.Setting{}).Select("value").Where("key = ?", "restore_marker").Scan(&marker).Error; err != nil {
		t.Fatal(err)
	}
	if marker != "live-before-import" {
		t.Fatalf("fallback marker=%q, want live-before-import", marker)
	}
}

func TestIntegrationRestoreRevokesSSHHostLocalAuthorityAcrossRestart(t *testing.T) {
	initBackupRestoreIntegrationDB(t)
	if _, err := (&service.SettingService{}).GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	repository := sshservice.Shared().Repository
	posture := domain.SSHPostureV1{
		Schema: domain.PostureSchemaV1, ObservedAt: now.Unix(), ExpiresAt: now.Add(5 * time.Minute).Unix(),
		SemanticRevision: strings.Repeat("a", 64), BinaryRevision: strings.Repeat("b", 64),
		ServiceRevision: strings.Repeat("c", 64), ConfigurationRevision: strings.Repeat("d", 64),
	}
	if err := repository.SavePosture(context.Background(), posture, now); err != nil {
		t.Fatal(err)
	}
	for index, kind := range []hostresources.ManagementServiceKind{
		hostresources.ManagementPanel, hostresources.ManagementSSH, hostresources.ManagementSubscriptionAdmin, hostresources.ManagementOtherAdmin,
	} {
		path := integrationRecoveryPath(fmt.Sprintf("restore-kind-%d", index), kind, now)
		if err := repository.UpsertRecoveryEvidence(context.Background(), path, now); err != nil {
			t.Fatal(err)
		}
	}
	backup, err := dbbackup.Export("")
	if err != nil {
		t.Fatal(err)
	}
	dbbackup.SetSendSighupHook(func() error { return nil })
	t.Cleanup(func() { dbbackup.SetSendSighupHook(nil) })
	if err := dbbackup.Restore(integrationMemMultipartFile{Reader: bytes.NewReader(backup)}); err != nil {
		t.Fatalf("Restore returned error: %v", err)
	}
	assertSSHHostLocalAuthorityRevoked(t, repository, now)

	livePath := configstorage.GetDBPath()
	if err := dbsqlite.Close(); err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.Init(livePath); err != nil {
		t.Fatal(err)
	}
	assertSSHHostLocalAuthorityRevoked(t, repository, now)

	locallyObserved := posture
	locallyObserved.ObservedAt = now.Add(time.Minute).Unix()
	locallyObserved.ExpiresAt = now.Add(6 * time.Minute).Unix()
	if err := repository.SavePosture(context.Background(), locallyObserved, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	locallyVerified := integrationRecoveryPath("restore-kind-0", hostresources.ManagementPanel, now.Add(time.Minute))
	locallyVerified.Revision = 3
	if err := repository.UpsertRecoveryEvidence(context.Background(), locallyVerified, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if rows, err := repository.RecoveryRows(context.Background(), now.Add(time.Minute)); err != nil || len(rows) != 1 || rows[0].ID != locallyVerified.ID {
		t.Fatalf("local re-verification did not restore exactly one live row: rows=%#v err=%v", rows, err)
	}
	if got, err := repository.LatestPosture(context.Background()); err != nil || got.ObservedAt != locallyObserved.ObservedAt {
		t.Fatalf("local posture re-observation unavailable: posture=%#v err=%v", got, err)
	}
}

func TestCheckpointIntegrationRestoreKeepsTerminalDeploymentHistoryInertAcrossRestart(t *testing.T) {
	initBackupRestoreIntegrationDB(t)
	if _, err := (&service.SettingService{}).GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	rows := []model.DeploymentOperation{
		integrationDeploymentRow(now, "committed", deploymentdomain.StateCommitted),
		integrationDeploymentRow(now.Add(time.Second), "rolled-back", deploymentdomain.StateRolledBack),
		integrationDeploymentRow(now.Add(2*time.Second), "active", deploymentdomain.StateApplying),
	}
	if err := dbsqlite.DB().Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	backup, err := dbbackup.Export("")
	if err != nil {
		t.Fatal(err)
	}
	dbbackup.SetSendSighupHook(func() error { return nil })
	t.Cleanup(func() { dbbackup.SetSendSighupHook(nil) })
	if err := dbbackup.Restore(integrationMemMultipartFile{Reader: bytes.NewReader(backup)}); err != nil {
		t.Fatalf("Restore returned error: %v", err)
	}
	assertRestoredDeploymentAuthority(t, rows)

	livePath := configstorage.GetDBPath()
	if err := dbsqlite.Close(); err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.Init(livePath); err != nil {
		t.Fatal(err)
	}
	assertRestoredDeploymentAuthority(t, rows)
}

func integrationDeploymentRow(now time.Time, suffix string, state deploymentdomain.OperationState) model.DeploymentOperation {
	digest := deploymentdomain.Revision("integration-deployment-" + suffix)
	operation := deploymentdomain.Operation{Schema: deploymentdomain.SchemaV1,
		OperationID: "deployment-operation:integration-" + suffix, IdempotencyKey: "deployment-idem-integration-" + suffix,
		State: state, FromProfile: deploymentdomain.NativeLegacyRoot, TargetProfile: deploymentdomain.NativeHardened,
		ExpectedPosture: digest, ExpectedManagement: digest, CheckpointRef: digest, BrokerReceipt: digest,
		Revision: 3, CreatedAt: now.Unix(), UpdatedAt: now.Unix()}
	operation.BindingRevision = deploymentdomain.OperationBinding(operation)
	return model.DeploymentOperation{OperationID: operation.OperationID, IdempotencyKey: operation.IdempotencyKey,
		State: string(operation.State), FromProfile: string(operation.FromProfile), TargetProfile: string(operation.TargetProfile),
		ExpectedPosture: operation.ExpectedPosture, ExpectedManagement: operation.ExpectedManagement,
		CheckpointRef: operation.CheckpointRef, BrokerReceipt: operation.BrokerReceipt, Revision: operation.Revision,
		CreatedAt: operation.CreatedAt, UpdatedAt: operation.UpdatedAt, ReasonsJSON: []byte(`[]`), BindingRevision: operation.BindingRevision}
}

func assertRestoredDeploymentAuthority(t *testing.T, original []model.DeploymentOperation) {
	t.Helper()
	var rows []model.DeploymentOperation
	if err := dbsqlite.DB().Where("operation_id IN ?", []string{original[0].OperationID, original[1].OperationID, original[2].OperationID}).
		Order("operation_id asc").Find(&rows).Error; err != nil || len(rows) != 3 {
		t.Fatalf("restored deployment rows=%d err=%v", len(rows), err)
	}
	byID := make(map[string]model.DeploymentOperation, len(rows))
	for _, row := range rows {
		byID[row.OperationID] = row
	}
	for _, terminal := range original[:2] {
		row := byID[terminal.OperationID]
		if row.RestoredUntrusted || row.CheckpointRef != "" || row.BrokerReceipt != "" || !row.CheckpointReleased || row.State != terminal.State {
			t.Fatalf("restored terminal deployment history retained live authority: %#v", row)
		}
	}
	active := byID[original[2].OperationID]
	if active.State != string(deploymentdomain.StateManualRecoveryRequired) || !active.RestoredUntrusted || active.CheckpointRef != "" ||
		active.BrokerReceipt != "" || !active.CheckpointReleased {
		t.Fatalf("restored unresolved deployment authority did not fail closed: %#v", active)
	}
	recovery, err := deploymentservice.Shared().Repository.Recovery(context.Background())
	if err != nil || recovery.OperationID != active.OperationID || recovery.State != deploymentdomain.StateManualRecoveryRequired || !recovery.RestoredUntrusted {
		t.Fatalf("restored live recovery projection=%#v err=%v", recovery, err)
	}
}

func assertSSHHostLocalAuthorityRevoked(t *testing.T, repository sshservice.Repository, now time.Time) {
	t.Helper()
	var postureCount int64
	if err := dbsqlite.DB().Model(&model.SSHPostureSnapshot{}).Count(&postureCount).Error; err != nil || postureCount != 0 {
		t.Fatalf("restored posture remained authoritative: count=%d err=%v", postureCount, err)
	}
	if rows, err := repository.RecoveryRows(context.Background(), now); err != nil || len(rows) != 0 {
		t.Fatalf("restored recovery remained live: rows=%d err=%v", len(rows), err)
	}
	var rows []model.SSHRecoveryEvidence
	if err := dbsqlite.DB().Order("id asc").Find(&rows).Error; err != nil || len(rows) != 4 {
		t.Fatalf("restored descriptive evidence rows=%d err=%v", len(rows), err)
	}
	for _, row := range rows {
		var reasons []string
		if row.VerificationState != "invalidated" || row.Revision != 2 || json.Unmarshal(row.ReasonCodesJSON, &reasons) != nil ||
			len(reasons) != 1 || reasons[0] != string(domain.ReasonRestoredStateUntrusted) {
			t.Fatalf("restored evidence retained authority: row=%#v reasons=%v", row, reasons)
		}
	}
}

func integrationRecoveryPath(id string, kind hostresources.ManagementServiceKind, now time.Time) hostresources.RecoveryPathV1 {
	return hostresources.RecoveryPathV1{
		Schema: hostresources.RecoveryPathSchemaV1, ID: "recovery:" + id, Kind: string(kind), EndpointID: "management:" + id,
		PrincipalID: "principal:" + id, VerificationMethod: "provider_console", EvidenceProvider: "integration-provider",
		TargetOperation: "ssh-operation:integration", VerifiedAt: now.Unix(), ExpiresAt: now.Add(10 * time.Minute).Unix(),
		IndependenceClass: "provider_control_plane", VerificationState: "verified", OperationBound: true, Revision: 1,
		SourceRevision: strings.Repeat("e", 64), ConfigurationRevision: strings.Repeat("f", 64), ProducerRevision: strings.Repeat("1", 64),
	}
}

func initBackupRestoreIntegrationDB(t *testing.T) {
	t.Helper()
	dbDir, err := os.MkdirTemp("", "s-ui-integration-backup-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SUI_DB_FOLDER", dbDir)
	livePath := configstorage.GetDBPath()
	closeBackupRestoreIntegrationDB()
	if err := dbsqlite.Init(livePath); err != nil {
		_ = os.RemoveAll(dbDir)
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() {
		closeBackupRestoreIntegrationDB()
		for _, suffix := range []string{"", "-wal", "-shm", "-journal", ".temp", ".backup"} {
			_ = os.Remove(livePath + suffix)
		}
		time.Sleep(25 * time.Millisecond)
		_ = os.RemoveAll(dbDir)
	})
}

func closeBackupRestoreIntegrationDB() {
	if db := dbsqlite.DB(); db != nil {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	}
}

func seedBackupRestoreTables(t *testing.T) {
	t.Helper()
	db := dbsqlite.DB()
	rows := []any{
		&model.Inbound{Type: "http", Tag: "integration-inbound", TlsId: 0, Addrs: []byte("[]"), OutJson: []byte("{}"), Options: []byte(`{"listen_port":18080}`)},
		&model.Service{Type: "derp", Tag: "integration-service", TlsId: 0, Options: []byte("{}")},
		&model.Endpoint{Type: "wireguard", Tag: "integration-endpoint", Options: []byte("{}"), Ext: []byte("{}")},
		&model.Tokens{Desc: "integration-token", Token: "plain-token", UserId: 1, Enabled: true},
		&model.Stats{DateTime: 1, Resource: "user", Tag: "integration-client", Direction: true, Traffic: 10},
		&model.ClientIP{ClientName: "integration-client", IPHash: "integration-hash", FirstSeen: 1, LastSeen: 2},
		&model.Client{Enable: true, Name: "integration-client", SubSecret: "integration-client-secret", Inbounds: []byte("[]"), Links: []byte("[]")},
		&model.Changes{DateTime: 1, Actor: "integration", Key: "settings", Action: "set", Obj: []byte(`{"subPath":"/integration/"}`)},
		&model.AuditEvent{DateTime: 1, Actor: "integration", Event: "integration_seed", Resource: "test", Severity: "info"},
	}
	for _, row := range rows {
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("seed %T: %v", row, err)
		}
	}
}

func integrationBackupTableCounts(t *testing.T) map[string]int64 {
	t.Helper()
	counts := map[string]int64{}
	for _, table := range integrationBackupTableNames() {
		var count int64
		if err := dbsqlite.DB().Table(table).Count(&count).Error; err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		counts[table] = count
	}
	return counts
}

func integrationBackupTableNames() []string {
	return []string{
		"settings",
		"tls",
		"inbounds",
		"outbounds",
		"services",
		"endpoints",
		"users",
		"tokens",
		"stats",
		"client_ips",
		"clients",
		"changes",
		"audit_events",
	}
}

func newIntegrationForeignKeyBrokenBackup(t *testing.T) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "broken-fk.db")
	broken, err := gorm.Open(gormsqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := broken.AutoMigrate(&model.Setting{}, &model.Tls{}, &model.Inbound{}); err != nil {
		t.Fatal(err)
	}
	if err := broken.Create(&model.Setting{Key: "version", Value: configidentity.GetVersion()}).Error; err != nil {
		t.Fatal(err)
	}
	if err := broken.Create(&model.Setting{Key: "config", Value: `{"dns":{},"route":{}}`}).Error; err != nil {
		t.Fatal(err)
	}
	if err := broken.Exec("PRAGMA foreign_keys = OFF").Error; err != nil {
		t.Fatal(err)
	}
	if err := broken.Exec(`
INSERT INTO inbounds(type, tag, tls_id, addrs, out_json, options)
VALUES(?, ?, ?, ?, ?, ?)
`, "http", fmt.Sprintf("broken-fk-%d", time.Now().UnixNano()), 99, []byte("[]"), []byte("{}"), []byte("{}")).Error; err != nil {
		t.Fatal(err)
	}
	if sqlDB, err := broken.DB(); err == nil {
		_ = sqlDB.Close()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
