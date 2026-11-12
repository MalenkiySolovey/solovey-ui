package backup

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/durable"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	componentmanifest "github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	"github.com/MalenkiySolovey/solovey-ui/internal/ops/durableowner"
	"gorm.io/gorm"
)

type restoreLifecycle struct{}

func (restoreLifecycle) Migrate(ctx context.Context, port durable.Migration) error {
	_, err := port.MigrateAndValidate(ctx)
	return err
}
func (restoreLifecycle) RehearseRestore(ctx context.Context, port durable.Restore) error {
	_, err := port.NormalizeRestore(ctx)
	return err
}
func (restoreLifecycle) InspectDropAuthority(ctx context.Context, port durable.Inspection, now time.Time) (durable.AuthorityStatus, error) {
	return port.InspectDropAuthority(ctx, now)
}
func (restoreLifecycle) DropData(ctx context.Context, port durable.DataDrop) error {
	_, err := port.DropOwnedData(ctx)
	return err
}

func TestTypedOwnerFailurePreservesExactLiveState(t *testing.T) {
	for _, stage := range []string{"preparation", "post_open"} {
		t.Run(stage, func(t *testing.T) {
			owner := "typed-restore-" + strings.ReplaceAll(stage, "_", "-")
			table := "typed_restore_" + stage
			item := componentmanifest.Manifest{ID: owner, Name: owner, Version: "1", Delivery: componentmanifest.DeliveryInProcess,
				Database: componentmanifest.Database{Tables: []string{table}}}
			calls := 0
			binding, err := durableowner.Bind(item, restoreLifecycle{}, durableowner.BackendAdapter{
				Migrate: func(ctx context.Context, db *gorm.DB) error {
					return db.WithContext(ctx).Exec("CREATE TABLE IF NOT EXISTS " + table + " (id INTEGER PRIMARY KEY, value TEXT)").Error
				},
				Restore: func(ctx context.Context, db *gorm.DB) error {
					calls++
					if err := db.WithContext(ctx).Exec("UPDATE " + table + " SET value='candidate-normalized'").Error; err != nil {
						return err
					}
					if stage == "preparation" || calls == 2 {
						return errors.New("typed owner restore rejected")
					}
					return nil
				},
				Inspect: func(context.Context, *gorm.DB, time.Time) durable.AuthorityStatus {
					return durable.AuthorityStatus{State: "VERIFIED_SAFE"}
				},
				Drop: func(context.Context, *gorm.DB) error { return errors.New("restore cannot authorize drop") },
			})
			if err != nil {
				t.Fatal(err)
			}
			durableowner.RegisterWithHooks(item, durableowner.Hooks{Typed: binding})
			dir := t.TempDir()
			t.Setenv("SUI_DB_FOLDER", dir)
			t.Setenv(installstate.InstalledFileEnv, filepath.Join(dir, "installed.json"))
			if err := installstate.Store(installstate.DefaultPath(), installstate.Metadata{Version: 1, Components: []installstate.InstalledComponent{{ID: owner, Delivery: componentmanifest.DeliveryInProcess, Installed: true}}}); err != nil {
				t.Fatal(err)
			}
			installed, err := os.ReadFile(installstate.DefaultPath())
			if err != nil {
				t.Fatal(err)
			}
			if err := dbsqlite.Init(configstorage.GetDBPath()); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = dbsqlite.Close() })
			if err := binding.Migrate(t.Context()); err != nil {
				t.Fatal(err)
			}
			if err := dbsqlite.DB().Exec("INSERT INTO " + table + " VALUES (1, 'backup')").Error; err != nil {
				t.Fatal(err)
			}
			data, err := Export("")
			if err != nil {
				t.Fatal(err)
			}
			if err := dbsqlite.DB().Exec("UPDATE " + table + " SET value='exact-live'").Error; err != nil {
				t.Fatal(err)
			}
			if _, err := RestoreContextDetailed(t.Context(), bytes.NewReader(data)); err == nil || !strings.Contains(err.Error(), "typed owner") && stage == "post_open" {
				t.Fatalf("restore accepted injected owner failure: %v", err)
			}
			expectedCalls := 1
			if stage == "post_open" {
				expectedCalls = 2
			}
			if calls != expectedCalls {
				t.Fatalf("owner hook calls=%d, want=%d", calls, expectedCalls)
			}
			var value string
			if err := dbsqlite.DB().Raw("SELECT value FROM " + table + " WHERE id=1").Scan(&value).Error; err != nil || value != "exact-live" {
				t.Fatalf("owner failure changed live data: %q %v", value, err)
			}
			after, err := os.ReadFile(installstate.DefaultPath())
			if err != nil || !bytes.Equal(installed, after) {
				t.Fatal("restore failure changed installed inventory", err)
			}
		})
	}
}
