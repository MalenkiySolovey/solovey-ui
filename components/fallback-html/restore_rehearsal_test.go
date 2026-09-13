//go:build !minimal

package fallbackhtml

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/lifecycle"
	fallbackdomain "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/domain"
	fallbackservice "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/service"
	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	dbbackup "github.com/MalenkiySolovey/solovey-ui/database/backup"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
)

func TestRestoreRehearsalKeepsLivePublicationAndExecutionNormalizesOnce(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SUI_DB_FOLDER", dir)
	installed := filepath.Join(dir, "installed.json")
	t.Setenv(installstate.InstalledFileEnv, installed)
	if err := installstate.Store(installed, installstate.Metadata{Version: 1, Components: []installstate.InstalledComponent{{ID: id, Delivery: "in-process", Installed: true}}}); err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.Init(configstorage.GetDBPath()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dbsqlite.Close() })
	previousRuntime := fallbackservice.DefaultRuntime
	fallbackservice.DefaultRuntime = fallbackservice.NewRuntime()
	t.Cleanup(func() {
		_ = (component{}).Stop(context.Background())
		fallbackservice.DefaultRuntime = previousRuntime
	})
	if err := (component{}).Migrate(context.Background(), lifecycle.Context{}); err != nil {
		t.Fatal(err)
	}
	setComponentTestSetting(t, "webPath", "/private-panel/")
	if err := (component{}).Start(context.Background(), lifecycle.Context{}); err != nil {
		t.Fatal(err)
	}
	service := fallbackservice.New(dbsqlite.DB(), fallbackservice.DefaultRuntime)
	site, err := service.SaveSite(fallbackservice.SiteInput{Name: "Live publication"}, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.PublishSite(site.ID, "fixture"); err != nil {
		t.Fatal(err)
	}
	before := fallbackservice.DefaultRuntime.Status()
	if !before.Active {
		t.Fatal("fixture publication is not active")
	}
	data, err := dbbackup.Export("")
	if err != nil {
		t.Fatal(err)
	}
	rehearsal, err := dbbackup.Rehearse(context.Background(), bytes.NewReader(data))
	if err != nil || !rehearsal.Possible {
		t.Fatalf("rehearsal failed: %v", err)
	}
	if fallbackservice.DefaultRuntime.Status() != before {
		t.Fatal("disposable restore rehearsal changed the live publication runtime")
	}
	var active int64
	if err := dbsqlite.DB().Model(&fallbackdomain.Publish{}).Where("active = ?", true).Count(&active).Error; err != nil || active != 1 {
		t.Fatal("rehearsal changed live publication data")
	}
	dbbackup.SetSendSighupHook(func() error { return nil })
	t.Cleanup(func() { dbbackup.SetSendSighupHook(nil) })
	if _, err := dbbackup.RestoreContextDetailed(context.Background(), bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	if fallbackservice.DefaultRuntime.Status().Active {
		t.Fatal("accepted restore retained a live publication")
	}
	var events int64
	if err := dbsqlite.DB().Model(&fallbackdomain.Event{}).Where("site_id = ? AND action = ?", site.ID, "site_restore_deactivated").Count(&events).Error; err != nil || events != 1 {
		t.Fatalf("restore normalization must execute once: events=%d err=%v", events, err)
	}
}
