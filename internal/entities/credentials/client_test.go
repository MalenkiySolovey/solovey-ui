package credentials

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/secretref"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/secrets/ownerdb"
	"github.com/MalenkiySolovey/solovey-ui/util/secretbox"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func credentialDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&model.Setting{}, &model.AuditEvent{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestManagedClientCredentialProjectionScopedUseAndAudit(t *testing.T) {
	db := credentialDB(t)
	captured, err := ApplyClientField(db, 42, "mieru", "", json.RawMessage(`{"intent":"replace","value":"synthetic-mieru-password"}`))
	if err != nil || !secretbox.IsEncrypted(captured) {
		t.Fatal("credential was not sealed")
	}
	credential, _ := json.Marshal(map[string]any{"name": "alice", "password": captured})
	config, _ := json.Marshal(map[string]json.RawMessage{"mieru": credential})
	projected, err := ProjectClient(42, config)
	if err != nil || bytes.Contains(projected, []byte("synthetic-mieru-password")) || bytes.Contains(projected, []byte(captured)) {
		t.Fatal("normal read disclosed a credential or envelope")
	}
	var metadata struct {
		Mieru struct{ Password secretref.View }
	}
	if json.Unmarshal(projected, &metadata) != nil || !metadata.Mieru.Password.Present || len(metadata.Mieru.Password.RotationRevision) != 64 {
		t.Fatal("missing reference metadata")
	}
	used := false
	err = UseClient(t.Context(), db, 42, "mieru", credential, "inbound-config", func(raw []byte) error {
		used = true
		var user struct{ Name, Password string }
		if json.Unmarshal(raw, &user) != nil || user.Name != "alice" || user.Password != "synthetic-mieru-password" {
			t.Fatal("wrong owner projection")
		}
		return nil
	})
	if err != nil || !used {
		t.Fatal("owner use denied")
	}
	var events []model.AuditEvent
	if err := db.Order("id").Find(&events).Error; err != nil || len(events) != 2 {
		t.Fatal("missing synchronous use audit")
	}
	record, _ := json.Marshal(events)
	if bytes.Contains(record, []byte("synthetic-mieru-password")) || bytes.Contains(record, []byte(captured)) || events[0].Event != "owner_secret_replace" || events[1].Event != "owner_secret_use" || events[1].Resource != "users" {
		t.Fatal("audit leaked material or lost owner identity")
	}
	for _, tc := range []struct {
		id       uint
		protocol string
	}{{43, "mieru"}, {42, "trusttunnel"}, {42, "ssh"}} {
		if err := UseClient(t.Context(), db, tc.id, tc.protocol, credential, "inbound-config", func([]byte) error { t.Fatal("credential crossed its binding"); return nil }); err == nil {
			t.Fatal("wrong binding accepted")
		}
	}
	if err := UseClient(t.Context(), db, 42, "mieru", json.RawMessage(`{"name":"alice","password":"synthetic-plaintext"}`), "inbound-config", func([]byte) error { t.Fatal("plaintext bypassed SecretRef"); return nil }); !errors.Is(err, secretref.ErrUnavailable) {
		t.Fatal("plaintext backend admitted")
	}
}

func TestCredentialBackendIsPortableWithItsDatabaseMaster(t *testing.T) {
	db := credentialDB(t)
	t.Setenv("SUI_SECRETBOX_KEY", base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{31}, 32)))
	captured, err := ApplyClientField(db, 42, "trusttunnel", "", json.RawMessage(`{"intent":"replace","value":"portable-fixture-password"}`))
	if err != nil {
		t.Fatal(err)
	}
	var settings []model.Setting
	if err := db.Find(&settings).Error; err != nil || len(settings) != 1 {
		t.Fatal("new key lifecycle was created")
	}
	backupDB := credentialDB(t)
	if err := backupDB.Create(&settings).Error; err != nil {
		t.Fatal(err)
	}
	t.Setenv("SUI_SECRETBOX_KEY", base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{57}, 32)))
	credential, _ := json.Marshal(map[string]any{"name": "alice", "password": captured})
	if err := UseClient(t.Context(), backupDB, 42, "trusttunnel", credential, "restore-check", func(raw []byte) error {
		if !bytes.Contains(raw, []byte("portable-fixture-password")) {
			t.Fatal("database key did not restore the value")
		}
		return nil
	}); err != nil {
		t.Fatal("source environment key was incorrectly required")
	}
	// A different database cannot silently borrow the process-wide key/master.
	foreignDB := credentialDB(t)
	if err := UseClient(t.Context(), foreignDB, 42, "trusttunnel", credential, "restore-check", func([]byte) error { t.Fatal("foreign key fallback admitted"); return nil }); !errors.Is(err, secretref.ErrUnavailable) {
		t.Fatal("missing master accepted")
	}
	var count int64
	if err := foreignDB.Model(&model.Setting{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("read created a master")
	}
}

func TestCredentialAuditFailurePreventsUseAndReferenceMutationIsExact(t *testing.T) {
	db := credentialDB(t)
	captured, err := ApplyClientField(db, 42, "mieru", "", json.RawMessage(`{"intent":"replace","value":"synthetic-private-value"}`))
	if err != nil {
		t.Fatal(err)
	}
	ref, err := ownerdb.Reference("users", 42, "mieru.password")
	if err != nil {
		t.Fatal(err)
	}
	view := ownerdb.View(ref, captured)
	unchanged, _ := json.Marshal(map[string]any{"intent": "unchanged", "reference": view.Reference, "present": view.Present, "rotationRevision": view.RotationRevision})
	if actual, err := ApplyClientField(db, 42, "mieru", captured, unchanged); err != nil || actual != captured {
		t.Fatal("unchanged resealed")
	}
	if _, err := ApplyClientField(db, 43, "mieru", captured, unchanged); !errors.Is(err, secretref.ErrDenied) {
		t.Fatal("another client reference accepted")
	}
	if err := db.Exec("CREATE TRIGGER reject_credential_audit BEFORE INSERT ON audit_events BEGIN SELECT RAISE(ABORT,'private-audit-failure'); END").Error; err != nil {
		t.Fatal(err)
	}
	credential, _ := json.Marshal(map[string]any{"name": "alice", "password": captured})
	if err := UseClient(t.Context(), db, 42, "mieru", credential, "inbound-config", func([]byte) error { t.Fatal("failed audit reached consumer"); return nil }); !errors.Is(err, secretref.ErrUnavailable) {
		t.Fatal("failed admission was not closed")
	}
}
