package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	"github.com/MalenkiySolovey/solovey-ui/database/backup"
	"github.com/MalenkiySolovey/solovey-ui/database/migration"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	backupenvelope "github.com/MalenkiySolovey/solovey-ui/internal/backup/envelope"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/ssmcache"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func ssmCacheTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "ssm.db")), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { sqlDB.Close() })
	if err = db.AutoMigrate(&model.Service{}, &model.Tls{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func ssmCacheRow(t *testing.T, db *gorm.DB, name string) model.Service {
	t.Helper()
	options, _ := json.Marshal(map[string]any{"listen": "127.0.0.1", "listen_port": 0, "servers": map[string]string{"/main": "ssm-managed"}, "cache_path": name})
	row := model.Service{Type: "ssm-api", Tag: "ssm", Options: options}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	return row
}

func TestSSMCachePrivateInventoryRehearsalPublicationAndRetry(t *testing.T) {
	ctx := context.Background()
	t.Setenv("SUI_DB_FOLDER", t.TempDir())
	db := ssmCacheTestDB(t)
	original := filepath.Join(ssmcache.Root(), "state.json")
	row := ssmCacheRow(t, db, original)
	store, err := ssmcache.New(original)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"endpoints":{"/main":{"global_uplink":17}}}`)
	if err = store.Write(ctx, data); err != nil {
		t.Fatal(err)
	}
	files, err := exportSSMCacheFiles(ctx, db)
	if err != nil || len(files) != 2 {
		t.Fatal("cache inventory incomplete")
	}
	if err = restoreSSMCacheFiles(ctx, db, files, false); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(ssmcache.Root(), "restored")); !os.IsNotExist(err) {
		t.Fatal("rehearsal published files")
	}
	if err = db.First(&row, row.Id).Error; err != nil {
		t.Fatal(err)
	}
	first, _ := ssmcache.Path(row.Options)
	if err = restoreSSMCacheFiles(ctx, db, files, true); err != nil {
		t.Fatal(err)
	}
	restored, err := ssmcache.New(first)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := restored.Read(ctx); err != nil || !bytes.Equal(got, data) {
		t.Fatal("published service state lost")
	}
	if err = restored.Write(ctx, []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(first); !bytes.Equal(got, data) {
		t.Fatal("restore seed overwritten")
	}
	if err = restoreSSMCacheFiles(ctx, db, files, false); err != nil {
		t.Fatal(err)
	}
	if err = db.First(&row, row.Id).Error; err != nil {
		t.Fatal(err)
	}
	second, _ := ssmcache.Path(row.Options)
	if second == first {
		t.Fatal("retry reused previously mutable working state")
	}
	if err = restoreSSMCacheFiles(ctx, db, files, true); err != nil {
		t.Fatal(err)
	}
	again, err := ssmcache.New(second)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := again.Read(ctx); err != nil || !bytes.Equal(got, data) {
		t.Fatal("retry loaded old working state")
	}
	if got, _ := os.ReadFile(original); !bytes.Equal(got, data) {
		t.Fatal("live rollback cache overwritten")
	}
}

func TestSSMCacheInventoryAbsentAndCorruptFailClosed(t *testing.T) {
	ctx := context.Background()
	t.Setenv("SUI_DB_FOLDER", t.TempDir())
	db := ssmCacheTestDB(t)
	row := ssmCacheRow(t, db, filepath.Join(ssmcache.Root(), "missing.json"))
	files, err := exportSSMCacheFiles(ctx, db)
	if err != nil || len(files) != 1 {
		t.Fatal("missing cache not explicitly recorded")
	}
	if _, _, err = validateSSMCacheFiles(ctx, db, files); err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]backup.OwnerFile{nil, append(files, files[0]), append(files, backup.OwnerFile{Key: "service:999", Data: []byte("{}")}), append(files, backup.OwnerFile{Key: "service:1", Data: []byte("null")})} {
		if err = restoreSSMCacheFiles(ctx, db, bad, false); err == nil {
			t.Fatal("incomplete/corrupt inventory accepted")
		}
		var after model.Service
		if err = db.First(&after, row.Id).Error; err != nil || !bytes.Equal(after.Options, row.Options) {
			t.Fatal("rejected restore changed stored options")
		}
	}
	if err = restoreSSMCacheFiles(ctx, db, files, false); err != nil {
		t.Fatal(err)
	}
	if err = restoreSSMCacheFiles(ctx, db, files, true); err != nil {
		t.Fatal(err)
	}
	if err = db.First(&row, row.Id).Error; err != nil {
		t.Fatal(err)
	}
	name, _ := ssmcache.Path(row.Options)
	store, err := ssmcache.New(name)
	if err != nil {
		t.Fatal(err)
	}
	if data, err := store.Read(ctx); err != nil || string(data) != "{}" {
		t.Fatal("absent cache did not restore explicit default")
	}
}

func TestSSMCacheActualPrivateArchiveRehearsalAndRestore(t *testing.T) {
	// Restore intentionally suspends the process token writer during generation
	// rebinding. Retire that global test fixture state before another test runs.
	t.Cleanup(resumeTokenUseFlush)
	ctx := context.Background()
	root := t.TempDir()
	t.Setenv("SUI_DB_FOLDER", root)
	t.Setenv(backup.LogicalFileBackupEnv, backup.LogicalFileBackupRequired)
	if err := dbsqlite.Init(configstorage.GetDBPath()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dbsqlite.Close() })
	if err := migration.EnsureCurrentSchemaJournal(dbsqlite.DB(), true); err != nil {
		t.Fatal(err)
	}
	if _, err := (&SettingService{}).GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	db := dbsqlite.DB()
	inbound := model.Inbound{Type: "shadowsocks", Tag: "ssm-managed", Options: json.RawMessage(`{"listen":"127.0.0.1","listen_port":0,"method":"aes-128-gcm","managed":true}`)}
	if err := db.Create(&inbound).Error; err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(ssmcache.Root(), "state.json")
	row := ssmCacheRow(t, db, original)
	store, err := ssmcache.New(original)
	if err != nil {
		t.Fatal(err)
	}
	credential := rand.Text()
	data, _ := json.Marshal(map[string]any{"endpoints": map[string]any{"/main": map[string]any{"global_uplink": 17, "users": map[string]string{"fixture": credential}}}})
	if err = store.Write(ctx, data); err != nil {
		t.Fatal(err)
	}
	archive, err := backup.Export("")
	if err != nil {
		t.Fatal(err)
	}
	var encrypted, decrypted bytes.Buffer
	passphrase := []byte(rand.Text())
	if _, _, err = backupenvelope.SealStream(&encrypted, bytes.NewReader(archive), passphrase); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encrypted.Bytes(), []byte(credential)) {
		t.Fatal("private cache escaped encryption")
	}
	if _, _, err = backupenvelope.OpenStream(&decrypted, bytes.NewReader(encrypted.Bytes()), passphrase, backup.MaxRestoreBytes); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(archive, decrypted.Bytes()) {
		t.Fatal("private archive changed")
	}
	live := []byte(`{"endpoints":{"/main":{"global_uplink":38}}}`)
	if err = store.Write(ctx, live); err != nil {
		t.Fatal(err)
	}
	rehearsal, err := backup.Rehearse(ctx, bytes.NewReader(archive))
	if err != nil || !rehearsal.Possible {
		t.Fatalf("SSM archive rehearsal rejected: %v", err)
	}
	if _, err = os.Stat(filepath.Join(root, "ssm", "restored")); !os.IsNotExist(err) {
		t.Fatal("private rehearsal wrote live files")
	}
	backup.SetSendSighupHook(func() error { return nil })
	t.Cleanup(func() { backup.SetSendSighupHook(nil) })
	result, err := backup.RestoreContextDetailed(ctx, bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	candidate := dbsqlite.DB().WithContext(result.DatabaseContext(ctx))
	if err = candidate.First(&row, row.Id).Error; err != nil {
		t.Fatal(err)
	}
	name, _ := ssmcache.Path(row.Options)
	restored, err := ssmcache.New(name)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := restored.Read(ctx); err != nil || !bytes.Equal(got, data) {
		t.Fatal("actual private archive lost service state")
	}
	if got, _ := os.ReadFile(original); !bytes.Equal(got, live) {
		t.Fatal("actual restore changed rollback cache")
	}
	if candidate.Migrator().HasTable(backup.BackupFileTable) {
		t.Fatal("archive retained duplicate durable authority")
	}
	if err = backup.AbortPendingRestore(); err != nil {
		t.Fatal(err)
	}
}

func TestSSMCacheDoctorCorruptionIsReadOnlyAndRedacted(t *testing.T) {
	initDoctorTestDB(t)
	db := dbsqlite.DB()
	name := filepath.Join(ssmcache.Root(), "state.json")
	store, err := ssmcache.New(name)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Write(context.Background(), []byte("{}")); err != nil {
		t.Fatal(err)
	}
	marker := rand.Text()
	preimage := []byte(`{"` + marker + `"`)
	if err = os.WriteFile(name, preimage, 0600); err != nil {
		t.Fatal(err)
	}
	ssmCacheRow(t, db, name)
	if err = db.Create(&model.Inbound{Type: "shadowsocks", Tag: "ssm-managed", Options: json.RawMessage(`{"listen":"127.0.0.1","listen_port":0,"method":"aes-128-gcm","managed":true}`)}).Error; err != nil {
		t.Fatal(err)
	}
	report := (&DoctorService{}).Run("127.0.0.1")
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "SSM_CACHE_CONTENT_REJECTED") {
		t.Fatal("Doctor did not report cache rejection")
	}
	if bytes.Contains(raw, []byte(marker)) {
		t.Fatal("Doctor exposed cache input")
	}
	if got, _ := os.ReadFile(name); !bytes.Equal(got, preimage) {
		t.Fatal("Doctor rewrote corrupt preimage")
	}
}
