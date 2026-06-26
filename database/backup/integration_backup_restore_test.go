package backup_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	configidentity "github.com/MalenkiySolovey/solovey-ui/config/identity"
	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	dbbackup "github.com/MalenkiySolovey/solovey-ui/database/backup"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	integrationtelegram "github.com/MalenkiySolovey/solovey-ui/internal/integrations/telegram"
	"github.com/MalenkiySolovey/solovey-ui/service"

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
	envelope, err := integrationtelegram.BuildTelegramBackupEnvelope(backup, passphrase)
	if err != nil {
		t.Fatal(err)
	}
	plaintext, err := integrationtelegram.OpenTelegramBackupEnvelope(envelope, passphrase)
	if err != nil {
		t.Fatal(err)
	}

	if err := dbsqlite.DB().Create(&model.Client{
		Enable:   true,
		Name:     "phase3-live-after-backup",
		Inbounds: []byte("[]"),
		Links:    []byte("[]"),
	}).Error; err != nil {
		t.Fatal(err)
	}
	dbbackup.SetSendSighupHook(func() error { return nil })
	t.Cleanup(func() { dbbackup.SetSendSighupHook(nil) })

	if err := dbbackup.Restore(integrationMemMultipartFile{Reader: bytes.NewReader(plaintext)}); err != nil {
		t.Fatalf("Restore returned error: %v", err)
	}
	after := integrationBackupTableCounts(t)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("backup table counts changed after restore:\nbefore=%v\nafter=%v", before, after)
	}
}

func TestIntegrationRestoreMigrationFailureRestoresFallback(t *testing.T) {
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
	if err == nil || !strings.Contains(err.Error(), "foreign key check failed") {
		t.Fatalf("expected migration foreign key failure after rename, got %v", err)
	}
	if dbsqlite.DB() == nil {
		t.Fatal("live DB handle was not restored after failed import")
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

func initBackupRestoreIntegrationDB(t *testing.T) {
	t.Helper()
	dbDir, err := os.MkdirTemp("", "s-ui-phase3-backup-*")
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
		&model.Inbound{Type: "http", Tag: "phase3-inbound", TlsId: 0, Addrs: []byte("[]"), OutJson: []byte("{}"), Options: []byte(`{"listen_port":18080}`)},
		&model.Service{Type: "derp", Tag: "phase3-service", TlsId: 0, Options: []byte("{}")},
		&model.Endpoint{Type: "wireguard", Tag: "phase3-endpoint", Options: []byte("{}"), Ext: []byte("{}")},
		&model.Tokens{Desc: "phase3-token", Token: "plain-token", UserId: 1, Enabled: true},
		&model.Stats{DateTime: 1, Resource: "user", Tag: "phase3-client", Direction: true, Traffic: 10},
		&model.ClientIP{ClientName: "phase3-client", IPHash: "phase3-hash", FirstSeen: 1, LastSeen: 2},
		&model.Client{Enable: true, Name: "phase3-client", Inbounds: []byte("[]"), Links: []byte("[]")},
		&model.Changes{DateTime: 1, Actor: "phase3", Key: "settings", Action: "set", Obj: []byte(`{"subPath":"/phase3/"}`)},
		&model.AuditEvent{DateTime: 1, Actor: "phase3", Event: "phase3_seed", Resource: "test", Severity: "info"},
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
