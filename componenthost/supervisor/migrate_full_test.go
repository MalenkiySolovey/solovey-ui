//go:build !minimal

package supervisor

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/lifecycle"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/registry"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
)

type migrationLifecycle struct {
	table string
}

func (l migrationLifecycle) Start(context.Context, lifecycle.Context) error {
	return nil
}

func (l migrationLifecycle) Stop(context.Context) error {
	return nil
}

func (l migrationLifecycle) Migrate(context.Context, lifecycle.Context) error {
	return dbsqlite.DB().Exec("CREATE TABLE " + l.table + " (id INTEGER PRIMARY KEY)").Error
}

func TestSupervisorMigratesOnlyInstalledComponents(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "s-ui.db")
	if err := dbsqlite.Init(dbPath); err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dbsqlite.Close() })

	installed := registry.Component{
		Manifest: manifest.Manifest{
			ID:       "test-installed-migration",
			Name:     "Test Installed Migration",
			Version:  "1",
			Delivery: manifest.DeliveryInProcess,
		},
		Lifecycle: migrationLifecycle{table: "test_installed_component_data"},
	}
	uninstalled := registry.Component{
		Manifest: manifest.Manifest{
			ID:       "test-uninstalled-migration",
			Name:     "Test Uninstalled Migration",
			Version:  "1",
			Delivery: manifest.DeliveryInProcess,
		},
		Lifecycle: migrationLifecycle{table: "test_uninstalled_component_data"},
	}

	supervisor := New(lifecycleHostForTest())
	supervisor.installedComponents = func() ([]registry.Component, error) {
		return []registry.Component{installed}, nil
	}
	if err := supervisor.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}

	if !dbsqlite.DB().Migrator().HasTable(installed.Lifecycle.(migrationLifecycle).table) {
		t.Fatalf("installed component did not create table %q", installed.Lifecycle.(migrationLifecycle).table)
	}
	if dbsqlite.DB().Migrator().HasTable(uninstalled.Lifecycle.(migrationLifecycle).table) {
		t.Fatalf("uninstalled component created table %q", uninstalled.Lifecycle.(migrationLifecycle).table)
	}

	var records []model.ComponentMigration
	if err := dbsqlite.DB().Order("component_id").Find(&records).Error; err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].ComponentID != installed.Manifest.ID {
		t.Fatalf("component migration journal = %#v, want only %s", records, installed.Manifest.ID)
	}
}
