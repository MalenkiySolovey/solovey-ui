//go:build !minimal

package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/supervisor"
	panelupdateservice "github.com/MalenkiySolovey/solovey-ui/components/panel-update-ui/service"
	_ "github.com/MalenkiySolovey/solovey-ui/components/server-protection"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	"github.com/MalenkiySolovey/solovey-ui/service/datalifecycle"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRemovalUsesActualProtectionAuthorityInspector(t *testing.T) {
	const id = "server-protection"
	for _, state := range []string{"active firewall", "recovery required"} {
		t.Run(state, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "installed.json")
			t.Setenv(installstate.InstalledFileEnv, path)
			if err := installstate.Store(path, installstate.Metadata{Version: 1, Profile: "full", Binary: "full", Components: []installstate.InstalledComponent{{ID: id, Delivery: manifest.DeliveryInProcess, Installed: true}}}); err != nil {
				t.Fatal(err)
			}
			db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "protection-removal.db")), &gorm.Config{})
			if err != nil {
				t.Fatal(err)
			}
			sqlDB, _ := db.DB()
			t.Cleanup(func() { _ = sqlDB.Close() })
			if err := db.AutoMigrate(&model.Setting{}, &model.MigrationJournal{}); err != nil {
				t.Fatal(err)
			}
			data := &datalifecycle.Manager{DB: func() *gorm.DB { return db }, Now: time.Now, Enabled: func(manifest.Manifest) (bool, error) { return false, nil }}
			s := supervisor.New(componenthost.Deps{})
			inventory := resources.NewRegistry(time.Second)
			admission := &panelupdateservice.RemovalAdmission{Preview: data.PreviewRemoval, Resources: data.RemovalResources, Inventory: inventory.Refresh, Admit: s.TryAdmitRemoval}
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
			preview, err := admission.Preview(t.Context(), id)
			if err != nil || preview.ExternalAuthority != "BLOCKED" {
				t.Fatalf("actual Protection inspector missed authority: %+v %v", preview, err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			manager := panelupdateservice.RuntimeManager{Removal: admission}
			if _, err := manager.Remove(panelupdateservice.OperationContext{}, id); err == nil {
				t.Fatal("Protection authority allowed metadata removal")
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("denied Protection removal changed installed metadata")
			}
		})
	}
}
