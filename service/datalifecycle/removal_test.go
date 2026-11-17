//go:build !minimal

package datalifecycle

import (
	"context"
	"errors"
	componentmanifest "github.com/MalenkiySolovey/solovey-host/componenthost/manifest"
	"github.com/MalenkiySolovey/solovey-backend/storage/model"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStaleDropDataPreviewCannotAdmitAfterOwnershipRemoval(t *testing.T) {
	owner := registerDropDataFixtureOwner()
	path := filepath.Join(t.TempDir(), "installed.json")
	t.Setenv(installstate.InstalledFileEnv, path)
	if err := installstate.Store(path, installstate.Metadata{Version: 1, Components: []installstate.InstalledComponent{{ID: owner.ID, Delivery: owner.Delivery, Installed: true}}}); err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "stale.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&model.DataLifecycleOperation{}, &model.DataLifecycleJournal{}); err != nil {
		t.Fatal(err)
	}
	m := NewManager()
	m.DB, m.Now = func() *gorm.DB { return db }, time.Now
	m.Root, m.RestoreRoot = t.TempDir(), t.TempDir()
	m.Enabled = func(componentmanifest.Manifest) (bool, error) { return false, nil }
	m.Drop = func(context.Context, string) error { t.Error("stale drop executed"); return nil }
	preview, err := m.Preview(t.Context(), owner.ID)
	if err != nil || len(preview.Blockers) != 0 {
		t.Fatalf("preview fixture: %+v %v", preview, err)
	}
	m.Admit = func(string) bool {
		_, err := installstate.SetInstalled(path, []componentmanifest.Manifest{owner}, owner.ID, false)
		if err != nil {
			t.Fatal(err)
		}
		return true
	}
	_, err = m.Execute(t.Context(), ExecuteRequest{OwnerID: owner.ID, ExpectedPreviewRevision: preview.Revision, IdempotencyKey: "stale-drop", Confirmation: "DROP_DATA_" + strings.ToUpper(strings.ReplaceAll(owner.ID, "-", "_")), BackupAcknowledged: true})
	var count int64
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("stale preview was admitted: %v", err)
	}
	if err := db.Model(&model.DataLifecycleOperation{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("stale preview created operation: %d %v", count, err)
	}
}
