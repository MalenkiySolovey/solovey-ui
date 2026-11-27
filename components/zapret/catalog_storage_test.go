//go:build !minimal

package zapret

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-backend/settings/store"
	feature "github.com/MalenkiySolovey/solovey-component-zapret"
	"github.com/MalenkiySolovey/solovey-component-zapret/strategy"
	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/supervisor"
	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	dbbackup "github.com/MalenkiySolovey/solovey-ui/database/backup"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	"github.com/MalenkiySolovey/solovey-ui/internal/zapretactivation"
	"github.com/MalenkiySolovey/solovey-ui/internal/zapretactivation/catalogstore"
	"github.com/MalenkiySolovey/solovey-ui/service"
	"gorm.io/gorm"
)

func openZapretArchiveDB(t *testing.T) {
	t.Helper()
	t.Setenv("SUI_DB_FOLDER", t.TempDir())
	installed := filepath.Join(t.TempDir(), "installed.json")
	t.Setenv(installstate.InstalledFileEnv, installed)
	if _, err := installstate.SetInstalled(installed, []manifest.Manifest{feature.Manifest()}, "zapret", true); err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.Init(configstorage.GetDBPath()); err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dbsqlite.Close() })
	if _, err := (&service.SettingService{}).GetAllSetting(); err != nil {
		t.Fatal(err)
	}
}

func TestZapretCatalogRestoresPreMigrationArchive(t *testing.T) {
	openZapretArchiveDB(t)
	file, err := os.Open("testdata/catalog-v1.sqlite.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	zipped, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer zipped.Close()
	archive, err := io.ReadAll(zipped)
	if err != nil {
		t.Fatal(err)
	}
	dbbackup.SetSendSighupHook(func() error { return nil })
	t.Cleanup(func() { dbbackup.SetSendSighupHook(nil) })
	if err := dbbackup.Restore(zapretArchive{bytes.NewReader(archive)}); err != nil {
		t.Fatal(err)
	}
	raw, err := catalogstore.Read(dbsqlite.DB())
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := readCatalog(raw)
	if err != nil || catalog.Revision() != "2e1eac179c4023d1493d226c7820ab551dd4b21e91524d66d1701a5219ec44bb" {
		t.Fatalf("restore changed the v1 catalog revision: %v", err)
	}
	if legacy, err := catalogstore.Legacy(dbsqlite.DB()); err != nil || legacy != nil || !dbsqlite.DB().Migrator().HasTable(catalogstore.Table) {
		t.Fatalf("archive was not normalized to v2: %v", err)
	}
}

func TestZapretCatalogMigrationLargeArchiveAndSelectedActivationProjection(t *testing.T) {
	openZapretArchiveDB(t)
	const id = "ui-zapret-selected"
	const inactive = "ui-zapret-inactive"
	legacy := strategy.Catalog{Schema: strategy.CatalogSchema, Records: map[string]strategy.Record{}}
	old, err := legacy.SealDraft(strategy.Draft{ID: id, Name: "Original", Raw: new("--filter-tcp=443\n")})
	if err != nil {
		t.Fatal(err)
	}
	legacy.Records[id] = old
	legacyJSON, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertValue(dbsqlite.DB(), zapretactivation.CatalogKey, string(legacyJSON)); err != nil {
		t.Fatal(err)
	}
	if err := supervisor.New(componenthost.Deps{}).Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	initial, err := catalogstore.Read(dbsqlite.DB())
	if err != nil {
		t.Fatal(err)
	}
	current, err := readCatalog(initial)
	if err != nil || current.Revision() != legacy.Revision() {
		t.Fatalf("migration changed the catalog revision: %v", err)
	}
	if raw, err := catalogstore.Legacy(dbsqlite.DB()); err != nil || raw != nil {
		t.Fatalf("legacy setting survived the migration: %v", err)
	}
	for _, name := range []string{"unused-a", "unused-b", "unused-c"} {
		current, err = current.WithLuaDraft(name, &strategy.LuaDraft{Origin: "administrator", Source: "-- " + strings.Repeat("x", 96<<10)})
		if err != nil {
			t.Fatal(err)
		}
	}
	const luaID = "observe"
	source := strategy.LuaDraft{Origin: "administrator", Source: "function observe() return VERDICT_PASS end\n"}
	current, err = current.WithLuaDraft(luaID, &source)
	if err != nil {
		t.Fatal(err)
	}
	const selectedLua = "--new --filter-tcp=18080 --lua-desync=observe"
	text := "--filter-tcp=100-163" + strings.Repeat(" --new --filter-tcp=100-163", 40) + " " + selectedLua
	selection := []strategy.LuaReference{{ID: luaID, Revision: source.Revision()}}
	record, err := current.SealDraft(strategy.Draft{ID: id, Name: "Large selected", Raw: &text, ManagedLua: selection})
	if err != nil {
		t.Fatal(err)
	}
	current.Records[id] = record
	current.Records[inactive], err = current.SealDraft(strategy.Draft{ID: inactive, Name: "Unused strategy", Raw: new("--filter-udp=443\n")})
	if err != nil {
		t.Fatal(err)
	}
	mod := source
	mod.Source += "-- not selected\n"
	current, err = current.WithLuaDraft(luaID, &mod)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(current)
	if err != nil || len(data) <= 256<<10 {
		t.Fatalf("fixture must exceed the old catalog limit: %d bytes, %v", len(data), err)
	}
	if err := dbsqlite.DB().Transaction(func(tx *gorm.DB) error { return catalogstore.Save(tx, initial, data) }); err != nil {
		t.Fatal(err)
	}
	assertProjection := func(db *gorm.DB) {
		t.Helper()
		got := zapretactivation.Validation{Bindings: []zapretactivation.Request{{OutboundRef: "one", StrategyRef: id}, {OutboundRef: "two", StrategyRef: id}}}
		if err := catalogstore.LoadSelected(db, &got); err != nil {
			t.Fatal(err)
		}
		if len(got.Definitions) != 1 || len(got.Definitions[id]) <= 8<<10 || len(got.LuaSources) != 1 {
			t.Fatalf("wrong activation projection: %d definitions, %d sealed bytes, %d sources", len(got.Definitions), len(got.Definitions[id]), len(got.LuaSources))
		}
		var loaded strategy.LuaDraft
		if err := json.Unmarshal(got.LuaSources[zapretactivation.LuaSourceKey(luaID, source.Revision())], &loaded); err != nil || loaded != source {
			t.Fatalf("activation did not select the exact retained source: %v", err)
		}
	}
	undo := errors.New("rollback test corruption")
	if err := dbsqlite.DB().Transaction(func(tx *gorm.DB) error {
		// A runtime lookup must never decode an unrelated record's editor text.
		if err := tx.Table(catalogstore.Table).Where("kind = ? AND id = ?", "strategy", inactive).Update("payload", "invalid JSON").Error; err != nil {
			return err
		}
		assertProjection(tx)
		if _, err := catalogstore.Read(tx); err == nil {
			t.Fatal("full catalog accepted corrupt editor data")
		}
		return undo
	}); !errors.Is(err, undo) {
		t.Fatal(err)
	}
	archive, err := dbbackup.Export("")
	if err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Transaction(func(tx *gorm.DB) error { return catalogstore.Save(tx, data, initial) }); err != nil {
		t.Fatal(err)
	}
	dbbackup.SetSendSighupHook(func() error { return nil })
	t.Cleanup(func() { dbbackup.SetSendSighupHook(nil) })
	if err := dbbackup.Restore(zapretArchive{bytes.NewReader(archive)}); err != nil {
		t.Fatal(err)
	}
	restoredJSON, err := catalogstore.Read(dbsqlite.DB())
	if err != nil {
		t.Fatal(err)
	}
	restored, err := readCatalog(restoredJSON)
	if err != nil || restored.Revision() != current.Revision() || len(restored.LuaDrafts) != 4 {
		t.Fatalf("restore changed an inactive component's catalog or prior revision: %v", err)
	}
	assertProjection(dbsqlite.DB())
}

type zapretArchive struct{ *bytes.Reader }

func (zapretArchive) Close() error { return nil }
