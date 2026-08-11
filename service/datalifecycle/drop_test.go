//go:build !minimal

package datalifecycle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/lifecycle"
	componentregistry "github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	componentmanifest "github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/durableowner"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDropDataPreviewIsReadOnlyAndExecutionIsManifestExact(t *testing.T) {
	owner := registerDropDataFixtureOwner()
	installedPath := filepath.Join(t.TempDir(), "installed.json")
	t.Setenv(installstate.InstalledFileEnv, installedPath)
	if err := installstate.Store(installedPath, installstate.Metadata{Version: 1, Components: []installstate.InstalledComponent{{
		ID: owner.ID, Delivery: componentmanifest.DeliveryInProcess, Installed: true,
	}}}); err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(sqlite.Open("file:drop-data-exact?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&model.Setting{}, &model.ComponentMigration{}, &model.MigrationJournal{},
		&model.DataLifecycleOperation{}, &model.DataLifecycleJournal{}, &model.UpdateOperation{}, &model.DeploymentOperation{}); err != nil {
		t.Fatal(err)
	}
	for _, table := range owner.Database.Tables {
		if err := db.Exec("CREATE TABLE " + table + " (id INTEGER PRIMARY KEY, value TEXT)").Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec("INSERT INTO "+table+"(value) VALUES (?)", "owned").Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Exec("CREATE INDEX drop_owner_fixture_test_index ON " + owner.Database.Tables[0] + "(value)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Setting{Key: owner.Database.Settings[0], Value: "false"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Setting{Key: owner.Database.Secrets[0], Value: "encrypted:test"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ComponentMigration{ComponentID: owner.ID, Version: "1", Name: owner.Name,
		Delivery: string(owner.Delivery), AppliedAt: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.MigrationJournal{Scope: "component", OwnerID: owner.ID, StepID: "component-schema:1",
		Checksum: owner.Database.MigrationChecksum, State: "APPLIED", CompatibilityState: "COMPATIBLE", UpdatedAt: 1}).Error; err != nil {
		t.Fatal(err)
	}
	manager := &Manager{DB: func() *gorm.DB { return db }, Now: func() time.Time { return time.Unix(1000, 0).UTC() },
		Root: t.TempDir(), Enabled: func(componentmanifest.Manifest) (bool, error) { return false, nil },
		Admit: func(string) bool { return true }, Backup: func(context.Context, model.DataLifecycleOperation) (string, error) {
			return dataLifecycleTestDigest("drop-backup"), nil
		}}
	manager.Drop = func(ctx context.Context, ownerID string) error {
		item, available := durableowner.Lookup(ownerID)
		if !available {
			return fmt.Errorf("owner unavailable")
		}
		for _, table := range item.Database.Tables {
			if err := db.WithContext(ctx).Migrator().DropTable(table); err != nil {
				return err
			}
		}
		keys := append(append([]string(nil), item.Database.Settings...), item.Database.Secrets...)
		if err := db.WithContext(ctx).Where("key IN ?", keys).Delete(&model.Setting{}).Error; err != nil {
			return err
		}
		if err := db.WithContext(ctx).Where("component_id = ?", ownerID).Delete(&model.ComponentMigration{}).Error; err != nil {
			return err
		}
		return db.WithContext(ctx).Where("scope = ? AND owner_id = ?", "component", ownerID).Delete(&model.MigrationJournal{}).Error
	}
	var changesBefore, changesAfter int64
	if err := db.Raw("SELECT total_changes()").Scan(&changesBefore).Error; err != nil {
		t.Fatal(err)
	}
	preview, err := manager.Preview(context.Background(), owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Raw("SELECT total_changes()").Scan(&changesAfter).Error; err != nil {
		t.Fatal(err)
	}
	if changesAfter != changesBefore {
		t.Fatalf("Drop Data preview wrote to SQLite: before=%d after=%d", changesBefore, changesAfter)
	}
	if len(preview.Blockers) != 0 || preview.Enabled || !preview.Installed || !preview.Available || preview.ExternalAuthority != "NOT_REQUIRED" {
		t.Fatalf("unexpected preview posture: %#v", preview)
	}
	kinds := map[string]bool{}
	for _, resource := range preview.Resources {
		kinds[resource.Kind] = true
	}
	if !kinds["sqlite_table"] || !kinds["sqlite_index"] || !kinds["setting"] || !kinds["secret"] || !kinds["migration_record"] {
		t.Fatalf("preview omitted declared resource classes: %#v", kinds)
	}
	operation, err := manager.Execute(context.Background(), ExecuteRequest{OwnerID: owner.ID,
		ExpectedPreviewRevision: preview.Revision, IdempotencyKey: "drop-fixture-1",
		Confirmation: "DROP_DATA_DROP_OWNER_FIXTURE", BackupAcknowledged: true})
	if err != nil {
		t.Fatal(err)
	}
	if operation.State != "APPLIED" || operation.BackupRef != dataLifecycleTestDigest("drop-backup") {
		t.Fatalf("unexpected Drop Data result: %#v", operation)
	}
	for _, table := range owner.Database.Tables {
		if db.Migrator().HasTable(table) {
			t.Fatalf("declared table %q survived Drop Data", table)
		}
	}
}

type dropDataFixtureLifecycle struct{ lifecycle.Noop }

func (dropDataFixtureLifecycle) DropData(context.Context, lifecycle.Context) error { return nil }

func registerDropDataFixtureOwner() componentmanifest.Manifest {
	const id = "drop-owner-fixture"
	if item, available := durableowner.Lookup(id); available {
		return item
	}
	item := componentmanifest.Manifest{
		ID: id, Name: "Drop owner fixture", Version: "1", Delivery: componentmanifest.DeliveryInProcess,
		Database: componentmanifest.Database{
			Tables:   []string{"drop_owner_fixture_rows", "drop_owner_fixture_events"},
			Settings: []string{"dropOwnerFixtureEnabled"}, Secrets: []string{"dropOwnerFixtureSecret"},
		},
	}.Normalized()
	componentregistry.Register(componentregistry.Component{Manifest: item, Lifecycle: dropDataFixtureLifecycle{}})
	return item
}

func dataLifecycleTestDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
