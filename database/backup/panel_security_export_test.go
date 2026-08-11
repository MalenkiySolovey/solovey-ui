package backup

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestPanelSecurityBackupPreservesEncryptedMFAAndOmitsBearerState(t *testing.T) {
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "s-ui.db")
	t.Setenv("SUI_DB_FOLDER", dbDir)
	if err := dbsqlite.Init(dbPath); err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() {
		closeMainDB(t)
		cleanupBackupSidecars(dbPath)
	})

	var admin model.User
	if err := dbsqlite.DB().Where("username = ?", "admin").First(&admin).Error; err != nil {
		t.Fatal(err)
	}
	factor := model.AdminMFAFactor{
		UserID:                  admin.Id,
		State:                   "active",
		ActiveSecretCiphertext:  "secretbox:v1:ciphertext-only",
		LastAcceptedCounter:     123,
		RecoveryGeneration:      7,
		RecoveryAcknowledged:    true,
		PendingSecretCiphertext: "",
		UpdatedAt:               1000,
	}
	if err := dbsqlite.DB().Create(&factor).Error; err != nil {
		t.Fatal(err)
	}
	recovery := model.AdminRecoveryCode{
		UserID: admin.Id, Generation: 7, Verifier: strings.Repeat("a", 64), CreatedAt: 1000,
	}
	if err := dbsqlite.DB().Create(&recovery).Error; err != nil {
		t.Fatal(err)
	}
	session := model.SecuritySession{
		SessionID: "server-session-id", Ref: "opaque-ref", UserID: admin.Id,
		UsernameSnapshot: admin.Username, State: "active", AuthState: "authenticated",
		Assurance: "mfa", LifetimePosture: "bounded_v1",
		SessionGenerationRevision: strings.Repeat("b", 64),
		CredentialGeneration:      1, MFAGeneration: 1,
		CreatedAt: 1000, AuthenticatedAt: 1000, LastSeenAt: 1000,
		IdleExpiresAt: 2000, AbsoluteExpiresAt: 3000,
		ClientProvenance: "direct", ClientPrefix: "198.51.100.0/24",
		UserAgentHash: strings.Repeat("c", 64), DeviceLabel: "browser",
	}
	if err := dbsqlite.DB().Create(&session).Error; err != nil {
		t.Fatal(err)
	}
	grant := model.StepUpGrant{
		Digest: strings.Repeat("d", 64), Revision: strings.Repeat("e", 64),
		UserID: admin.Id, SessionRef: session.Ref,
		SessionGenerationRevision: strings.Repeat("b", 64),
		CredentialGeneration:      1, MFAGeneration: 1,
		OperationKind: "mfa.disable", TargetDigest: strings.Repeat("f", 64),
		Assurance: "mfa", CreatedAt: 1000, ExpiresAt: 1300,
	}
	if err := dbsqlite.DB().Create(&grant).Error; err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Exec(`
CREATE TABLE IF NOT EXISTS sessions (
	id TEXT PRIMARY KEY,
	data BLOB NOT NULL,
	expires_at INTEGER NOT NULL DEFAULT 0
)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Exec("INSERT INTO sessions(id, data, expires_at) VALUES(?,?,?)", "server-session-id", []byte("bearer-material"), 3000).Error; err != nil {
		t.Fatal(err)
	}

	backupPath, cleanup, err := PrepareExport("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	backupDB, err := gorm.Open(sqlite.Open(backupPath), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sqlDB, sqlErr := backupDB.DB(); sqlErr == nil {
			_ = sqlDB.Close()
		}
	})

	var copiedFactor model.AdminMFAFactor
	if err := backupDB.Where("user_id = ?", admin.Id).First(&copiedFactor).Error; err != nil {
		t.Fatal(err)
	}
	if copiedFactor.ActiveSecretCiphertext != factor.ActiveSecretCiphertext ||
		copiedFactor.ActiveSecretCiphertext == "" {
		t.Fatalf("encrypted MFA state was not preserved: %#v", copiedFactor)
	}
	var recoveryCount int64
	if err := backupDB.Model(&model.AdminRecoveryCode{}).
		Where("user_id = ? AND verifier = ?", admin.Id, recovery.Verifier).
		Count(&recoveryCount).Error; err != nil {
		t.Fatal(err)
	}
	if recoveryCount != 1 {
		t.Fatalf("recovery verifier rows=%d, want 1", recoveryCount)
	}
	for _, table := range []string{"sessions", "security_sessions", "step_up_grants"} {
		if backupDB.Migrator().HasTable(table) {
			t.Fatalf("bearer-state table %q must not be present in backup", table)
		}
	}
}
