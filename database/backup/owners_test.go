package backup

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	componentmanifest "github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/durableowner"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestBackupOwnerModesAreClosedAndOwnerSpecific(t *testing.T) {
	for _, test := range []struct {
		owner string
		mode  string
		want  bool
	}{
		{"core", "TYPED", true},
		{"core", "LEGACY_TYPED", true},
		{"core", "OPAQUE_PRESERVED", false},
		{"component", "TYPED", true},
		{"component", "NO_DURABLE_DATA", true},
		{"component", "OPAQUE_PRESERVED", true},
		{"component", "LEGACY_TYPED", false},
		{"component", "FUTURE_MODE", false},
	} {
		if got := validBackupOwnerMode(test.owner, test.mode); got != test.want {
			t.Fatalf("validBackupOwnerMode(%q, %q)=%t, want %t", test.owner, test.mode, got, test.want)
		}
	}
}

func TestInstalledOwnerInventoryIncludesDisabledOwnerAndFailsClosedWhenUnavailable(t *testing.T) {
	const ownerID = "backup-disabled-fixture"
	const tableName = "backup_disabled_fixture_rows"
	durableowner.Register(componentmanifest.Manifest{ID: ownerID, Name: "Backup disabled fixture", Version: "1",
		Delivery: componentmanifest.DeliveryInProcess, DefaultEnabled: false,
		Database: componentmanifest.Database{Tables: []string{tableName}}})

	db, err := gorm.Open(sqlite.Open("file:backup-owner-disabled?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.Exec("CREATE TABLE " + tableName + " (id INTEGER PRIMARY KEY, value TEXT)").Error; err != nil {
		t.Fatal(err)
	}
	installedPath := filepath.Join(t.TempDir(), "installed.json")
	t.Setenv(installstate.InstalledFileEnv, installedPath)
	if err := installstate.Store(installedPath, installstate.Metadata{Version: 1, Components: []installstate.InstalledComponent{{
		ID: ownerID, Delivery: componentmanifest.DeliveryInProcess, Installed: true,
	}}}); err != nil {
		t.Fatal(err)
	}
	tables, err := installedOwnerTables(db, map[string]struct{}{})
	if err != nil || len(tables) != 1 || tables[0].owner != ownerID || tables[0].name != tableName {
		t.Fatalf("disabled installed owner tables=%#v err=%v", tables, err)
	}

	unavailablePath := filepath.Join(t.TempDir(), "installed-unavailable.json")
	t.Setenv(installstate.InstalledFileEnv, unavailablePath)
	if err := installstate.Store(unavailablePath, installstate.Metadata{Version: 1, Components: []installstate.InstalledComponent{{
		ID: "backup-unavailable-fixture", Delivery: componentmanifest.DeliveryInProcess, Installed: true,
	}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := installedOwnerTables(db, map[string]struct{}{}); err == nil ||
		!strings.Contains(err.Error(), "backup-unavailable-fixture") {
		t.Fatalf("unavailable installed owner did not fail closed: %v", err)
	}
}

func TestNonportableRuntimeAuthorityExclusionsAreExplicit(t *testing.T) {
	want := map[string]string{
		"security_sessions":                   "NONPORTABLE_RUNTIME_AUTHORITY",
		"step_up_grants":                      "NONPORTABLE_RUNTIME_AUTHORITY",
		"inbound_endpoint_leases":             "NONPORTABLE_RUNTIME_AUTHORITY",
		"failover_state":                      "NONPORTABLE_RUNTIME_STATE",
		"ssh_managed_artifact_checkpoints_v1": "NONPORTABLE_HOST_AUTHORITY",
		"ssh_reconnect_challenges_v1":         "NONPORTABLE_RUNTIME_AUTHORITY",
	}
	got := map[string]string{}
	for _, table := range backupTables() {
		if table.alwaysExclude {
			got[table.name] = table.exclusionCode
		}
	}
	if len(got) != len(want) {
		t.Fatalf("nonportable exclusion inventory=%#v, want exactly %#v", got, want)
	}
	for name, reason := range want {
		if got[name] != reason {
			t.Fatalf("nonportable exclusion %q=%q, want %q", name, got[name], reason)
		}
	}
}
