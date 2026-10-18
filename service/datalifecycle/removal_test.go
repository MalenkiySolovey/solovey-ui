//go:build !minimal

package datalifecycle

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	componentmanifest "github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRemovalPresenceProjectionPreservesFullDropDataPreview(t *testing.T) {
	owner := registerDropDataFixtureOwner()
	path := filepath.Join(t.TempDir(), "installed.json")
	t.Setenv(installstate.InstalledFileEnv, path)
	if err := installstate.Store(path, installstate.Metadata{Version: 1, Components: []installstate.InstalledComponent{{ID: owner.ID, Delivery: owner.Delivery, Installed: true}}}); err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "presence.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.Exec("CREATE TABLE drop_owner_fixture_rows (id INTEGER PRIMARY KEY)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO drop_owner_fixture_rows (id) VALUES (1), (2)").Error; err != nil {
		t.Fatal(err)
	}
	m := &Manager{DB: func() *gorm.DB { return db }, Now: time.Now, Enabled: func(componentmanifest.Manifest) (bool, error) { return false, nil }}
	presence, err := m.PreviewRemoval(t.Context(), owner.ID)
	if err != nil || presence.Schema != "solovey.component-removal-impact/v1" {
		t.Fatalf("presence projection: %+v %v", presence, err)
	}
	full, err := m.Preview(t.Context(), owner.ID)
	if err != nil || full.Schema != "solovey.drop-data-preview/v1" {
		t.Fatalf("drop projection: %+v %v", full, err)
	}
	find := func(p Preview) Resource {
		for _, r := range p.Resources {
			if r.ID == "table:drop_owner_fixture_rows" {
				return r
			}
		}
		return Resource{}
	}
	if find(presence).Terminal != "PRESENT" || find(presence).Rows != 0 || find(full).Rows != 2 {
		t.Fatal("presence confused absence with a zero-row count")
	}
	root := t.TempDir()
	t.Setenv("SUI_DB_FOLDER", filepath.Join(root, "db"))
	if err := os.Mkdir(filepath.Join(root, "owned"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "owned", "file"), []byte("owner data"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := m.observeFileMode("owned", owner.ID, "CONFIDENTIAL", true)
	if err != nil || r.Terminal != "PRESENT" || r.Rows != 0 {
		t.Fatalf("bounded file presence: %+v %v", r, err)
	}
	final, err := m.RemovalResources(t.Context(), owner.ID)
	if err != nil || final.ExternalAuthority != "NOT_OBSERVED" {
		t.Fatalf("trusted final projection asserted external authority: %+v %v", final, err)
	}
}

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
	m := &Manager{DB: func() *gorm.DB { return db }, Now: time.Now, Root: t.TempDir(), RestoreRoot: t.TempDir(), Enabled: func(componentmanifest.Manifest) (bool, error) { return false, nil },
		Drop: func(context.Context, string) error { t.Error("stale drop executed"); return nil }}
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
