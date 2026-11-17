//go:build !minimal

package datalifecycle

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	componentmanifest "github.com/MalenkiySolovey/solovey-host/componenthost/manifest"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRemovalPresenceProjectionPreservesFullDropDataPreview(t *testing.T) {
	owner := registerDropDataFixtureOwner()
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
	m := &Manager{Owners: &fixtureOwnerCatalog{manifest: owner, installed: true}, DB: func() *gorm.DB { return db }, Now: time.Now, Enabled: func(componentmanifest.Manifest) (bool, error) { return false, nil }}
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
	m.DatabaseRoot = func() string { return filepath.Join(root, "db") }
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
