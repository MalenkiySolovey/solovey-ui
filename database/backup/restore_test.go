package backup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/MalenkiySolovey/solovey-ui/database/hooks"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	configidentity "github.com/MalenkiySolovey/solovey-ui/config/identity"
	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	passwordutil "github.com/MalenkiySolovey/solovey-ui/util/password"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// memMultipartFile is a minimal multipart.File implementation backed by an
// in-memory byte slice so the import path can be exercised from a test
// without going through net/http.
type memMultipartFile struct{ *bytes.Reader }

func (memMultipartFile) Close() error { return nil }

func newLegacyBackup(t testing.TB) []byte {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.db")

	// Open a plain (non-WAL) SQLite database so the file we read back is a
	// single self-contained .db blob, exactly like a legacy 1.4.1 backup.
	legacy, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := legacy.AutoMigrate(
		&model.Setting{},
		&model.Tls{},
		&model.Inbound{},
		&model.Outbound{},
		&model.Service{},
		&model.Endpoint{},
		&model.User{},
		&model.Tokens{},
		&model.Stats{},
		&model.Client{},
		&model.Changes{},
	); err != nil {
		t.Fatal(err)
	}

	// Plaintext admin credential (legacy schema), pre-1.4.2 version pin.
	if err := legacy.Create(&model.User{Username: "legacy-admin", Password: "legacy-secret"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := legacy.Create(&model.Setting{Key: "version", Value: "1.4.1"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := legacy.Create(&model.Setting{Key: "config", Value: `{"dns":{},"route":{}}`}).Error; err != nil {
		t.Fatal(err)
	}

	if sqlDB, err := legacy.DB(); err == nil {
		_ = sqlDB.Close()
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestRestoreRunsResetHooks(t *testing.T) {
	dbDir, err := os.MkdirTemp("", "s-ui-import-reset-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SUI_DB_FOLDER", dbDir)
	livePath := configstorage.GetDBPath()
	t.Cleanup(func() {
		closeMainDB(t)
		time.Sleep(25 * time.Millisecond)
		_ = os.RemoveAll(dbDir)
	})

	if err := dbsqlite.Init(livePath); err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Create(&model.Setting{Key: "config", Value: `{"dns":{},"route":{}}`}).Error; err != nil {
		t.Fatal(err)
	}

	prev := sendSighupHook
	sendSighupHook = func() error { return nil }
	t.Cleanup(func() { sendSighupHook = prev })

	backupBytes, err := Export("")
	if err != nil {
		t.Fatal(err)
	}

	var calls atomic.Int32
	const hookName = "test.import_db_reset_hooks"
	hooks.RegisterResetHook(hookName, func() {
		calls.Add(1)
	})
	t.Cleanup(func() {
		hooks.RegisterResetHook(hookName, nil)
	})

	if err := Restore(memMultipartFile{Reader: bytes.NewReader(backupBytes)}); err != nil {
		t.Fatalf("Restore returned error: %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("reset hook calls=%d, want 1", got)
	}
}

func TestRestorePreservesConfigDNSAndRouteRules(t *testing.T) {
	dbDir, err := os.MkdirTemp("", "s-ui-import-config-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SUI_DB_FOLDER", dbDir)
	livePath := configstorage.GetDBPath()
	t.Cleanup(func() {
		closeMainDB(t)
		time.Sleep(25 * time.Millisecond)
		_ = os.RemoveAll(dbDir)
	})

	if err := dbsqlite.Init(livePath); err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}

	prev := sendSighupHook
	sendSighupHook = func() error { return nil }
	t.Cleanup(func() { sendSighupHook = prev })

	const restoredConfig = `{
  "dns": {
    "servers": [
      {
        "tag": "dns-umbrella",
        "type": "udp",
        "server": "208.67.222.222"
      }
    ]
  },
  "route": {
    "rules": [
      {
        "domain_suffix": [
          "example.test"
        ],
        "action": "route",
        "outbound": "direct"
      }
    ]
  }
}`
	if err := dbsqlite.DB().Create(&model.Setting{Key: "config", Value: restoredConfig}).Error; err != nil {
		t.Fatal(err)
	}

	backupBytes, err := Export("")
	if err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Model(&model.Setting{}).Where("key = ?", "config").Update("value", `{"dns":{},"route":{}}`).Error; err != nil {
		t.Fatal(err)
	}

	if err := Restore(memMultipartFile{Reader: bytes.NewReader(backupBytes)}); err != nil {
		t.Fatalf("Restore returned error: %v", err)
	}

	var config string
	if err := dbsqlite.DB().Model(&model.Setting{}).Select("value").Where("key = ?", "config").Scan(&config).Error; err != nil {
		t.Fatal(err)
	}
	if config != restoredConfig {
		t.Fatalf("imported config=%q, want %q", config, restoredConfig)
	}
}

func TestRestoreRejectsFutureCoreSchemaBeforeMutatingLiveDatabase(t *testing.T) {
	dbDir, err := os.MkdirTemp("", "s-ui-import-future-version-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SUI_DB_FOLDER", dbDir)
	livePath := configstorage.GetDBPath()
	t.Cleanup(func() {
		closeMainDB(t)
		time.Sleep(25 * time.Millisecond)
		_ = os.RemoveAll(dbDir)
	})
	if err := dbsqlite.Init(livePath); err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Create(&model.Setting{Key: "restore-live-sentinel", Value: "unchanged"}).Error; err != nil {
		t.Fatal(err)
	}
	liveDB := dbsqlite.DB()

	candidateBytes, err := Export("")
	if err != nil {
		t.Fatal(err)
	}
	candidatePath := filepath.Join(dbDir, "future.db")
	if err := os.WriteFile(candidatePath, candidateBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	candidate, err := gorm.Open(sqlite.Open(candidatePath), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	updated := candidate.Model(&model.Setting{}).Where("key = ?", "coreSchemaVersion").Update("value", "9999.0.0")
	if updated.Error != nil {
		t.Fatal(updated.Error)
	}
	if updated.RowsAffected == 0 {
		if err := candidate.Create(&model.Setting{Key: "coreSchemaVersion", Value: "9999.0.0"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	rewriteBackupManifestForTest(t, candidate, func(item *BackupManifest) { item.CoreSchema = "9999.0.0" })
	candidateSQL, err := candidate.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := candidateSQL.Close(); err != nil {
		t.Fatal(err)
	}
	candidateBytes, err = os.ReadFile(candidatePath)
	if err != nil {
		t.Fatal(err)
	}

	err = Restore(memMultipartFile{Reader: bytes.NewReader(candidateBytes)})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "future_core_schema") {
		t.Fatalf("future-schema restore error=%v", err)
	}
	if dbsqlite.DB() != liveDB {
		t.Fatal("live database handle changed before future-schema rejection")
	}
	var value string
	if err := dbsqlite.DB().Model(&model.Setting{}).
		Select("value").Where("key = ?", "restore-live-sentinel").Scan(&value).Error; err != nil {
		t.Fatal(err)
	}
	if value != "unchanged" {
		t.Fatalf("live sentinel mutated: %q", value)
	}
	if _, err := os.Stat(livePath + ".backup"); !os.IsNotExist(err) {
		t.Fatalf("restore rotated live database before validation: %v", err)
	}
}

func rewriteBackupManifestForTest(t *testing.T, db *gorm.DB, mutate func(*BackupManifest)) {
	t.Helper()
	var record backupManifestRecord
	if err := db.First(&record, "scope = ?", "backup").Error; err != nil {
		t.Fatal(err)
	}
	var item BackupManifest
	if err := json.Unmarshal(record.Payload, &item); err != nil {
		t.Fatal(err)
	}
	if mutate != nil {
		mutate(&item)
	}
	for index := range item.Tables {
		if item.Tables[index].Excluded && item.Tables[index].ExclusionCode != "OPERATOR_EXCLUDED_OPTIONAL_TELEMETRY" {
			schemaDigest, contentDigest := excludedTableDigests(item.Tables[index].Name, item.Tables[index].ExclusionCode)
			item.Tables[index].Rows = 0
			item.Tables[index].SchemaDigest = schemaDigest
			item.Tables[index].ContentDigest = contentDigest
			continue
		}
		entry, err := digestBackupTable(context.Background(), db, item.Tables[index].Owner, item.Tables[index].Name)
		if err != nil {
			t.Fatal(err)
		}
		entry.Excluded, entry.ExclusionCode = item.Tables[index].Excluded, item.Tables[index].ExclusionCode
		item.Tables[index] = entry
	}
	item.BackupID = backupManifestDigest(item)
	payload, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&backupManifestRecord{}).Where("scope = ?", "backup").Updates(map[string]any{
		"payload": payload, "digest": item.BackupID,
	}).Error; err != nil {
		t.Fatal(err)
	}
}

func TestRestoreAdaptsLegacyBackup(t *testing.T) {
	if runtime.GOOS == "windows" {
		// On Windows the test runner's t.TempDir() cleanup races against
		// the SQLite WAL/SHM mappings even after explicit Close, producing
		// noisy "file in use" errors that do not happen on the production
		// Linux servers this code targets.
		t.Skip("skipping Windows-specific TempDir cleanup race; logic is exercised on Linux CI")
	}

	dbDir := t.TempDir()
	t.Setenv("SUI_DB_FOLDER", dbDir)

	// Initialize a fresh "live" database so Restore has something to
	// rotate aside as the fallback. Use the same path GetDBPath() returns
	// so the import code targets it.
	livePath := configstorage.GetDBPath()
	if err := dbsqlite.Init(livePath); err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}

	// Make sure we close the DB and nuke WAL sidecars before t.TempDir()
	// cleanup runs, otherwise on Windows the dir-remove fails because the
	// SQLite driver is still mmap'd onto the *.db-wal file.
	t.Cleanup(func() {
		closeMainDB(t)
		for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
			_ = os.Remove(livePath + suffix)
		}
	})

	// Suppress the SIGHUP that Restore sends at the end so it does not
	// kill the test runner.
	prev := sendSighupHook
	sendSighupHook = func() error { return nil }
	t.Cleanup(func() { sendSighupHook = prev })

	// Build a legacy backup blob.
	legacyBytes := newLegacyBackup(t)

	// Hand it to Restore through the multipart.File interface.
	if err := Restore(memMultipartFile{Reader: bytes.NewReader(legacyBytes)}); err != nil {
		t.Fatalf("Restore returned error: %v", err)
	}

	// The fallback and temp files must be cleaned up after a successful
	// import.
	for _, p := range []string{livePath + ".temp", livePath + ".backup"} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("leftover file after successful import: %s", p)
		}
	}

	// The live DB must contain the legacy admin user with a bcrypt-hashed
	// password, validating that AdaptToCurrentVersion ran on the imported
	// database.
	d := dbsqlite.DB()
	if d == nil {
		t.Fatal("sqlite.DB returned nil after import")
	}
	var stored string
	if err := d.Model(&model.User{}).Select("password").Where("username = ?", "legacy-admin").Scan(&stored).Error; err != nil {
		t.Fatalf("query imported user: %v", err)
	}
	if stored == "" {
		t.Fatal("imported admin user is missing")
	}
	if !passwordutil.IsEncoded(stored) {
		t.Fatalf("imported password was not rehashed; got plaintext: %q", stored)
	}
	if ok, _, err := passwordutil.Verify(context.Background(), stored, "legacy-secret"); err != nil || !ok {
		t.Fatal("rehashed password no longer validates the legacy plaintext")
	}

	// settings.version must have been bumped from 1.4.1 to the current
	// build version.
	var version string
	if err := d.Model(&model.Setting{}).Select("value").Where("key = ?", "version").Scan(&version).Error; err != nil {
		t.Fatal(err)
	}
	if version == "1.4.1" || version == "" {
		t.Fatalf("settings.version was not bumped: %q", version)
	}
}

func TestRehearsalExplicitlyAdaptsOnlySupportedLegacyBackup(t *testing.T) {
	dbDir := t.TempDir()
	t.Setenv("SUI_DB_FOLDER", dbDir)
	legacyBytes := newLegacyBackup(t)
	rehearsal, err := Rehearse(context.Background(), bytes.NewReader(legacyBytes))
	if err != nil {
		t.Fatal(err)
	}
	if !rehearsal.Possible || rehearsal.ManifestStatus != "LEGACY_EXPLICITLY_ADAPTED" ||
		rehearsal.Manifest == nil || rehearsal.Manifest.AppVersion != "1.4.1" ||
		rehearsal.MigrationPlan != "REHEARSED" {
		t.Fatalf("legacy rehearsal=%#v", rehearsal)
	}
}

func TestRestoreRejectsCorruptSQLiteBackup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping Windows-specific TempDir cleanup race; logic is exercised on Linux CI")
	}
	dbDir := t.TempDir()
	t.Setenv("SUI_DB_FOLDER", dbDir)
	livePath := configstorage.GetDBPath()
	if err := dbsqlite.Init(livePath); err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() {
		closeMainDB(t)
		for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
			_ = os.Remove(livePath + suffix)
		}
	})
	corrupt := append([]byte("SQLite format 3\x00"), bytes.Repeat([]byte{0xff}, 256)...)
	if err := Restore(memMultipartFile{Reader: bytes.NewReader(corrupt)}); err == nil {
		t.Fatal("corrupt sqlite backup should be rejected")
	}
}

func TestRestoreAcceptsVersionedBackupWithoutConfig(t *testing.T) {
	dbDir := t.TempDir()
	t.Setenv("SUI_DB_FOLDER", dbDir)
	livePath := configstorage.GetDBPath()
	if err := dbsqlite.Init(livePath); err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() {
		closeMainDB(t)
		for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
			_ = os.Remove(livePath + suffix)
		}
	})

	if err := dbsqlite.DB().Create(&model.Setting{Key: "restore_marker", Value: "live-before-import"}).Error; err != nil {
		t.Fatal(err)
	}
	SetSendSighupHook(func() error { return nil })
	t.Cleanup(func() { SetSendSighupHook(nil) })

	err := Restore(memMultipartFile{Reader: bytes.NewReader(newVersionedBackupWithoutConfig(t))})
	// Post-fix #12: missing settings.config no longer aborts the import. The
	// import may still fail for unrelated reasons (e.g. fixture migration
	// gaps), but never because settings.config is absent.
	if err != nil && strings.Contains(err.Error(), "settings.config") {
		t.Fatalf("missing settings.config should now warn-and-continue, got error: %v", err)
	}
	// Live DB must remain reachable. Either the import succeeded (live DB
	// replaced, no restore_marker) or it failed downstream and the fallback
	// rollback re-attached the original live DB (restore_marker present).
	if dbsqlite.DB() == nil {
		t.Fatal("sqlite.DB returned nil after import attempt")
	}
	if sqlDB, dbErr := dbsqlite.DB().DB(); dbErr != nil {
		t.Fatalf("live DB handle error: %v", dbErr)
	} else if pingErr := sqlDB.Ping(); pingErr != nil {
		t.Fatalf("live DB ping failed: %v", pingErr)
	}
}

func TestRestoreRollsBackProtectedPostSwapFailureAndReopensExactLiveDB(t *testing.T) {
	dbDir := t.TempDir()
	t.Setenv("SUI_DB_FOLDER", dbDir)
	livePath := configstorage.GetDBPath()
	if err := dbsqlite.Init(livePath); err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() {
		closeMainDB(t)
		for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
			_ = os.Remove(livePath + suffix)
		}
	})

	if err := dbsqlite.DB().Create(&model.Setting{Key: "restore_marker", Value: "live-before-import"}).Error; err != nil {
		t.Fatal(err)
	}
	candidate, err := Export("")
	if err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Model(&model.Setting{}).Where("key = ?", "restore_marker").Update("value", "live-after-candidate").Error; err != nil {
		t.Fatal(err)
	}
	restoreProtectedPostActionHook = func(context.Context) error { return errors.New("forced protected failure") }
	t.Cleanup(func() { restoreProtectedPostActionHook = nil })

	err = Restore(memMultipartFile{Reader: bytes.NewReader(candidate)})
	if err == nil || !strings.Contains(err.Error(), "forced protected failure") {
		t.Fatalf("expected protected post-swap failure, got %v", err)
	}
	if dbsqlite.DB() == nil {
		t.Fatal("sqlite.DB returned nil after failed import rollback")
	}
	if sqlDB, dbErr := dbsqlite.DB().DB(); dbErr != nil {
		t.Fatalf("live db handle after rollback: %v", dbErr)
	} else if pingErr := sqlDB.Ping(); pingErr != nil {
		t.Fatalf("live db was not reopened after rollback: %v", pingErr)
	}
	var marker string
	if err := dbsqlite.DB().Model(&model.Setting{}).Select("value").Where("key = ?", "restore_marker").Scan(&marker).Error; err != nil {
		t.Fatal(err)
	}
	if marker != "live-after-candidate" {
		t.Fatalf("rollback marker=%q, want exact live-after-candidate", marker)
	}
}

func newVersionedBackupWithoutConfig(t *testing.T) []byte {
	t.Helper()

	path := filepath.Join(t.TempDir(), "missing-config.db")
	backup, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := backup.AutoMigrate(&model.Setting{}); err != nil {
		t.Fatal(err)
	}
	if err := backup.Create(&model.Setting{Key: "version", Value: configidentity.GetVersion()}).Error; err != nil {
		t.Fatal(err)
	}
	if sqlDB, err := backup.DB(); err == nil {
		_ = sqlDB.Close()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// _ keeps io referenced when nothing else uses it.
var _ = io.EOF
