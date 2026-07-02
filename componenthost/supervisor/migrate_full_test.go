//go:build !minimal

package supervisor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	_ "github.com/MalenkiySolovey/solovey-ui/components/paid-subscriptions"
	_ "github.com/MalenkiySolovey/solovey-ui/components/remote-outbound-subscriptions"
	_ "github.com/MalenkiySolovey/solovey-ui/components/telegram"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
)

func TestSupervisorMigratesOnlyInstalledComponents(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "s-ui.db")
	if err := dbsqlite.Init(dbPath); err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dbsqlite.Close() })

	installedPath := filepath.Join(t.TempDir(), "installed.json")
	t.Setenv(installstate.InstalledFileEnv, installedPath)
	if err := os.WriteFile(installedPath, []byte(`{
		"version": 1,
		"profile": "custom",
		"binary": "full",
		"components": [
			{"id": "remote-outbound-subscriptions", "delivery": "in-process", "installed": true}
		]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := New(lifecycleHostForTest()).Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}

	for _, table := range []string{
		"remote_outbound_subscriptions",
		"remote_outbound_groups",
		"remote_outbound_group_connections",
		"remote_outbound_connections",
	} {
		if !dbsqlite.DB().Migrator().HasTable(table) {
			t.Fatalf("installed remote component did not create table %q", table)
		}
	}
	for _, table := range []string{
		"paidsub_bindings",
		"tariffs",
		"payment_orders",
	} {
		if dbsqlite.DB().Migrator().HasTable(table) {
			t.Fatalf("uninstalled paid component created table %q", table)
		}
	}

	var records []model.ComponentMigration
	if err := dbsqlite.DB().Order("component_id").Find(&records).Error; err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].ComponentID != "remote-outbound-subscriptions" {
		t.Fatalf("component migration journal = %#v, want only remote-outbound-subscriptions", records)
	}
}
