package durableowner

import (
	"context"
	"testing"

	componentmanifest "github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRunStagedRestoreUsesDisabledOwnerHooksAndPostconditions(t *testing.T) {
	item := componentmanifest.Manifest{ID: "staged-owner", Name: "Staged Owner", Version: "1",
		Delivery: componentmanifest.DeliveryInProcess, Database: componentmanifest.Database{Tables: []string{"staged_owner_rows"},
			Settings: []string{"stagedOwnerSetting"}}}.Normalized()
	Register(item)
	RegisterHooks(item.ID, Hooks{MigrateStaged: func(ctx context.Context, db *gorm.DB) error {
		return db.WithContext(ctx).Exec("CREATE TABLE IF NOT EXISTS staged_owner_rows (id INTEGER PRIMARY KEY)").Error
	}})
	db, err := gorm.Open(sqlite.Open("file:staged-owner?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)").Error; err != nil {
		t.Fatal(err)
	}
	if err := RunStagedRestore(context.Background(), item.ID, db); err != nil {
		t.Fatal(err)
	}
	if !db.Migrator().HasTable("staged_owner_rows") {
		t.Fatal("staged owner migration did not run")
	}
}

func TestRunStagedRestoreFailsClosedWithoutMigrationOrPortableFileArchive(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:staged-owner-negative?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	missingHook := componentmanifest.Manifest{ID: "missing-staged-hook", Name: "Missing Hook", Version: "1",
		Delivery: componentmanifest.DeliveryInProcess, Database: componentmanifest.Database{Tables: []string{"missing_rows"}}}.Normalized()
	Register(missingHook)
	RegisterHooks(missingHook.ID, Hooks{})
	if err := RunStagedRestore(context.Background(), missingHook.ID, db); err == nil {
		t.Fatal("table owner without a staged migration hook was accepted")
	}
	portable := componentmanifest.Manifest{ID: "portable-file-owner", Name: "Portable File", Version: "1",
		Delivery: componentmanifest.DeliveryInProcess, Database: componentmanifest.Database{Files: []componentmanifest.DurableFileResource{{
			Path: "portable/data", BackupClass: componentmanifest.FileBackupOpaque,
			Redaction: componentmanifest.RedactionSensitive, Portability: componentmanifest.PortabilityPortable}}}}.Normalized()
	Register(portable)
	RegisterHooks(portable.ID, Hooks{})
	if err := RunStagedRestore(context.Background(), portable.ID, db); err == nil {
		t.Fatal("portable owner file without a staged archive was accepted")
	}
}
