//go:build minimal

package supervisor

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
)

func TestSupervisorMigrateMinimalKeepsOptionalSchemaAbsent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "s-ui.db")
	if err := dbsqlite.Init(dbPath); err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dbsqlite.Close() })
	t.Setenv(installstate.InstalledFileEnv, filepath.Join(t.TempDir(), "installed.json"))

	if err := New(lifecycleHostForTest()).Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}

	for _, table := range []string{
		"remote_outbound_subscriptions",
		"remote_outbound_groups",
		"remote_outbound_group_connections",
		"remote_outbound_connections",
		"paidsub_bindings",
		"tariffs",
		"payment_orders",
	} {
		if dbsqlite.DB().Migrator().HasTable(table) {
			t.Fatalf("minimal supervisor migration must not create optional table %q", table)
		}
	}
}
