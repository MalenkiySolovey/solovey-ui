//go:build !minimal

package blockcheck

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	feature "github.com/MalenkiySolovey/solovey-component-blockcheck"
	"github.com/MalenkiySolovey/solovey-ui/componenthost"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/supervisor"
	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	dbbackup "github.com/MalenkiySolovey/solovey-ui/database/backup"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/internal/components/manifest"
	"github.com/MalenkiySolovey/solovey-ui/service"
)

func openBlockCheckArchiveDB(t *testing.T) {
	t.Helper()
	t.Setenv("SUI_DB_FOLDER", t.TempDir())
	installed := filepath.Join(t.TempDir(), "installed.json")
	t.Setenv(installstate.InstalledFileEnv, installed)
	if _, err := installstate.SetInstalled(installed, []manifest.Manifest{feature.Manifest()}, feature.ID, true); err != nil {
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

func TestBlockCheckRestoresPreMigrationArchive(t *testing.T) {
	openBlockCheckArchiveDB(t)
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
	if err := dbbackup.Restore(backupMultipartFile{Reader: bytes.NewReader(archive)}); err != nil {
		t.Fatal(err)
	}
	db := dbsqlite.DB()
	catalog, err := readCatalog(t.Context(), db)
	if err != nil || catalog.Revision() != "77c5e47866995be02fbdc6f0c52e130be5f182d8735f275aaf74c7df4cf1eebe" {
		t.Fatalf("restore changed the v1 catalog revision: %v", err)
	}
	if _, present := catalog.Group("youtube"); present {
		t.Fatal("restore resurrected the deleted default group")
	}
	if legacy, err := catalogSetting(db, feature.CatalogSettingKey); err != nil || legacy != "" {
		t.Fatalf("archive was not normalized to v2: %v", err)
	}
}

func TestBlockCheckBackupRestorePreservesCatalogRevisionAndUserEdits(t *testing.T) {
	openBlockCheckArchiveDB(t)
	ctx := t.Context()
	if err := supervisor.New(componenthost.Deps{}).Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	catalog, err := readCatalog(ctx, dbsqlite.DB())
	if err != nil {
		t.Fatal(err)
	}
	edited, err := changeCatalog(ctx, dbsqlite.DB(), feature.CatalogChange{Action: "delete", ID: "youtube", ExpectedRevision: catalog.Revision()})
	if err != nil {
		t.Fatal(err)
	}
	for i := range 8 {
		transfer := largeCatalogGroup(i).Export()
		edited, err = changeCatalog(ctx, dbsqlite.DB(), feature.CatalogChange{Action: "import", Transfer: &transfer, ExpectedRevision: edited.Revision()})
		if err != nil {
			t.Fatal(err)
		}
	}
	if payload, err := json.Marshal(edited); err != nil || len(payload) <= feature.MaxCatalogBytes {
		t.Fatalf("fixture does not exceed the legacy %d byte limit: %d %v", feature.MaxCatalogBytes, len(payload), err)
	}
	now := time.Now().UTC()
	if err := retainHistory(ctx, dbsqlite.DB(), feature.HistoryRun{ID: "archive-run", StartedAt: now.Add(-time.Second), FinishedAt: now, Phase: "CANCELED"}, now); err != nil {
		t.Fatal(err)
	}
	archive, err := dbbackup.Export("")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := changeCatalog(ctx, dbsqlite.DB(), feature.CatalogChange{Action: "delete", ID: edited.Groups[len(edited.Groups)-1].ID, ExpectedRevision: edited.Revision()}); err != nil {
		t.Fatal(err)
	}
	dbbackup.SetSendSighupHook(func() error { return nil })
	t.Cleanup(func() { dbbackup.SetSendSighupHook(nil) })
	if err := dbbackup.Restore(backupMultipartFile{Reader: bytes.NewReader(archive)}); err != nil {
		t.Fatal(err)
	}
	restored, err := readCatalog(ctx, dbsqlite.DB())
	if err != nil || restored.Revision() != edited.Revision() {
		t.Fatalf("catalog revision did not survive restore: %v", err)
	}
	if _, exists := restored.Group("youtube"); exists {
		t.Fatal("restore resurrected a deleted default group")
	}
	history, err := readHistory(ctx, dbsqlite.DB(), now)
	if err != nil || len(history.Runs) != 1 || history.Runs[0].ID != "archive-run" {
		t.Fatalf("catalog migration affected history: %+v %v", history, err)
	}
}

type backupMultipartFile struct{ *bytes.Reader }

func (backupMultipartFile) Close() error { return nil }
