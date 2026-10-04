//go:build !minimal

package fallbackhtml

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"

	neutral "github.com/MalenkiySolovey/solovey-ui/componenthost/fallbacktargets"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/installstate"
	"github.com/MalenkiySolovey/solovey-ui/componenthost/lifecycle"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	fallbackdomain "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/domain"
	fallbackservice "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/service"
	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	dbbackup "github.com/MalenkiySolovey/solovey-ui/database/backup"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
)

func TestRestoreRehearsalKeepsLivePublicationAndExecutionNormalizesOnce(t *testing.T) {
	for _, decision := range []string{"accept", "exact_abort"} {
		t.Run(decision, func(t *testing.T) { testRestorePublicationRebind(t, decision) })
	}
}

func testRestorePublicationRebind(t *testing.T, decision string) {
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
	oldProvider, ok := neutral.Default.ProviderV2(id)
	if !ok {
		t.Fatal("live fallback provider is not registered")
	}
	_ = hostresources.Refresh(context.Background()) // prime the old contributor generation
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
	restored, err := dbbackup.RestoreContextDetailed(context.Background(), bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if fallbackservice.DefaultRuntime.Status().Active {
		t.Fatal("private candidate retained a live publication")
	}
	private := restored.DatabaseContext(context.Background())
	provider, ok := neutral.Default.ProviderV2(id)
	if !ok {
		t.Fatal("candidate fallback provider was not rebound")
	}
	if bound := provider.(targetProvider).db; bound != dbsqlite.DB() || dbsqlite.IsMaintenanceContext(bound.Statement.Context) {
		t.Fatal("cached fallback provider lost generation or leaked private authority")
	}
	if _, providerErr := provider.InventoryV2(private, neutral.InventoryV2Request{Limit: 8}); providerErr != nil {
		t.Fatal("private candidate provider query failed")
	}
	var events int64
	if err := dbsqlite.DB().WithContext(restored.DatabaseContext(context.Background())).Model(&fallbackdomain.Event{}).Where("site_id = ? AND action = ?", site.ID, "site_restore_deactivated").Count(&events).Error; err != nil || events != 1 {
		t.Fatalf("restore normalization must execute once: events=%d err=%v", events, err)
	}
	if decision == "accept" {
		if _, _, err := dbbackup.CompletePendingRestore(context.Background()); err != nil {
			t.Fatal(err)
		}
	} else {
		if err := dbbackup.AbortPendingRestore(); err != nil {
			t.Fatal(err)
		}
		if fallbackservice.DefaultRuntime.Status() != before {
			t.Fatal("exact fallback did not rebuild its original publication")
		}
	}
	provider, ok = neutral.Default.ProviderV2(id)
	if !ok {
		t.Fatal("finished restore lost fallback provider")
	}
	if _, providerErr := provider.InventoryV2(context.Background(), neutral.InventoryV2Request{Limit: 8}); providerErr != nil {
		t.Fatal("ordinary fallback provider retained a closed or privileged handle")
	}
	if _, providerErr := oldProvider.InventoryV2(context.Background(), neutral.InventoryV2Request{Limit: 8}); providerErr == nil {
		t.Fatal("stale provider regained old database authority")
	}
	resources := hostresources.Refresh(context.Background())
	for _, failure := range resources.Errors {
		if failure.Owner == id {
			t.Fatal("finished restore retained an unavailable fallback resource contributor")
		}
	}
	var resourceCount int
	for _, resource := range resources.Resources {
		if resource.Owner == id {
			resourceCount++
		}
	}
	if (decision == "accept" && resourceCount != 0) || (decision == "exact_abort" && resourceCount == 0) {
		t.Fatalf("rebound fallback resource count=%d", resourceCount)
	}
	if err := dbsqlite.DB().WithContext(private).Raw("SELECT 1").Scan(new(int)).Error; !errors.Is(err, dbsqlite.ErrRetired) {
		t.Fatalf("finished private capability remained usable: %v", err)
	}
}
