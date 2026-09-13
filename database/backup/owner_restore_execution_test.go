package backup

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	componentmanifest "github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/durableowner"
	"gorm.io/gorm"
)

func TestActualOwnerNormalizationFailureRollsBackImportedDatabase(t *testing.T) {
	const owner = "restore-execution-rejection"
	const table = "restore_execution_rows"
	var calls int
	durableowner.RegisterWithHooks(componentmanifest.Manifest{ID: owner, Name: "Restore execution rejection", Version: "1",
		Delivery: componentmanifest.DeliveryInProcess, Database: componentmanifest.Database{Tables: []string{table}}}, durableowner.Hooks{
		MigrateStaged: func(_ context.Context, db *gorm.DB) error {
			return db.Exec("CREATE TABLE IF NOT EXISTS " + table + " (id INTEGER PRIMARY KEY, value TEXT)").Error
		},
		RehearseRestore: func(_ context.Context, db *gorm.DB) error {
			calls++
			if err := db.Exec("UPDATE " + table + " SET value='normalized'").Error; err != nil {
				return err
			}
			if calls == 2 {
				return errors.New("owner execution rejection")
			}
			return nil
		},
	})
	t.Setenv("SUI_DB_FOLDER", t.TempDir())
	installed := filepath.Join(t.TempDir(), "installed.json")
	t.Setenv(installstate.InstalledFileEnv, installed)
	if err := installstate.Store(installed, installstate.Metadata{Version: 1, Components: []installstate.InstalledComponent{{ID: owner, Delivery: componentmanifest.DeliveryInProcess, Installed: true}}}); err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.Init(configstorage.GetDBPath()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dbsqlite.Close() })
	if err := dbsqlite.DB().Exec("CREATE TABLE " + table + " (id INTEGER PRIMARY KEY, value TEXT)").Error; err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Exec("INSERT INTO " + table + " VALUES(1,'snapshot')").Error; err != nil {
		t.Fatal(err)
	}
	backup, err := Export("")
	if err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Exec("UPDATE " + table + " SET value='live-after-snapshot'").Error; err != nil {
		t.Fatal(err)
	}
	_, err = RestoreContextDetailed(context.Background(), bytes.NewReader(backup))
	if err == nil || !strings.Contains(err.Error(), "owner execution rejection") || calls != 2 {
		t.Fatalf("owner rejection absent: calls=%d err=%v", calls, err)
	}
	var restored string
	if err := dbsqlite.DB().Raw("SELECT value FROM " + table + " WHERE id=1").Scan(&restored).Error; err != nil || restored != "live-after-snapshot" {
		t.Fatal("failed owner normalization did not reopen exact prior data")
	}
}
