//go:build !minimal

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-backend/storage/model"
	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/supervisor"
	_ "github.com/MalenkiySolovey/solovey-ui/components/server-protection"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
	"github.com/MalenkiySolovey/solovey-ui/composition/componentmanagement"
	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	"github.com/MalenkiySolovey/solovey-ui/service/datalifecycle"
)

func TestRemovalUsesActualProtectionAuthorityInspector(t *testing.T) {
	const id = "server-protection"
	for _, state := range []string{"active firewall", "recovery required"} {
		t.Run(state, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "installed.json")
			t.Setenv(installstate.InstalledFileEnv, path)
			t.Setenv("SUI_DB_FOLDER", t.TempDir())
			if err := installstate.Store(path, installstate.Metadata{Version: 1, Profile: "full", Binary: "full", Components: []installstate.InstalledComponent{{ID: id, Delivery: manifest.DeliveryInProcess, Installed: true}}}); err != nil {
				t.Fatal(err)
			}
			_ = dbsqlite.Close()
			if err := dbsqlite.Init(configstorage.GetDBPath()); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = dbsqlite.Close() })
			db := dbsqlite.DB()
			if err := db.AutoMigrate(&model.Setting{}, &model.MigrationJournal{}); err != nil {
				t.Fatal(err)
			}
			data := datalifecycle.NewManager()
			data.Enabled = func(manifest.Manifest) (bool, error) { return false, nil }
			s := supervisor.New(componenthost.Deps{})
			inventory := resources.NewRegistry(time.Second)
			manager := componentmanagement.New(data)
			manager.Removal.Inventory, manager.Removal.Admit = inventory.Refresh, s.TryAdmitRemoval
			manager.Reconcile = func(context.Context) error { t.Error("denied removal reached reconcile"); return nil }
			if state == "active firewall" {
				if err := db.AutoMigrate(&protectionrepository.FirewallContributionModel{}); err != nil {
					t.Fatal(err)
				}
				if err := db.Create(&protectionrepository.FirewallContributionModel{ContributionID: "test-active-rule", SemanticJSON: json.RawMessage(`{}`)}).Error; err != nil {
					t.Fatal(err)
				}
			} else {
				if err := db.AutoMigrate(&protectionrepository.OperationLockModel{}); err != nil {
					t.Fatal(err)
				}
				if err := db.Create(&protectionrepository.OperationLockModel{OperationID: "test-recovery", State: "rollback_failed"}).Error; err != nil {
					t.Fatal(err)
				}
			}
			preview, err := data.PreviewRemoval(t.Context(), id)
			if err != nil || preview.ExternalAuthority != "BLOCKED" {
				t.Fatalf("actual Protection inspector missed authority: %+v %v", preview, err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			status, err := manager.Inventory()
			if err != nil || len(status.Installed) != 1 || !status.Installed[0].Removable {
				t.Fatalf("fixture did not reach Host removal admission: %+v %v", status, err)
			}
			if _, err := manager.Remove(t.Context(), id); err == nil {
				t.Fatal("Protection authority allowed metadata removal")
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("denied Protection removal changed installed metadata")
			}
		})
	}
}
