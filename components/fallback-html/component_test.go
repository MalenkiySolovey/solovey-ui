//go:build !minimal

package fallbackhtml

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/lifecycle"
	fallbackdomain "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/domain"
	fallbackservice "github.com/MalenkiySolovey/solovey-ui/components/fallback-html/service"
	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
)

func TestDropDataRemovesSchemaAndStorage(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SUI_DB_FOLDER", dir)
	_ = dbsqlite.Close()
	if err := dbsqlite.Init(configstorage.GetDBPath()); err != nil {
		t.Fatalf("init sqlite: %v", err)
	}
	t.Cleanup(func() { _ = dbsqlite.Close() })

	ctx := context.Background()
	if err := (component{}).Migrate(ctx, lifecycle.Context{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db := dbsqlite.DB()
	setComponentTestSetting(t, "webPath", "/secret-panel/")
	service := fallbackservice.New(db, fallbackservice.NewRuntime())
	site, err := service.SaveSite(fallbackservice.SiteInput{Name: "Example Portal"}, "tester")
	if err != nil {
		t.Fatalf("SaveSite: %v", err)
	}
	if _, err := service.PublishSite(site.ID, "tester"); err != nil {
		t.Fatalf("PublishSite: %v", err)
	}
	if err := db.Create(&model.InboundDraft{
		Source:      "fallback-html:self-steal",
		SourceRef:   "site/1",
		Status:      "blocked",
		InboundType: "vless",
		Tag:         "fallback-html-site-1",
		Payload:     []byte("{}"),
	}).Error; err != nil {
		t.Fatalf("create component-owned core draft: %v", err)
	}
	if err := db.Create(&model.InboundDraft{
		Source:    "other-component",
		SourceRef: "keep",
		Status:    "blocked",
		Payload:   []byte("{}"),
	}).Error; err != nil {
		t.Fatalf("create foreign core draft: %v", err)
	}
	storageRoot := filepath.Join(configstorage.GetDBFolderPath(), "fallback-html")
	if _, err := os.Stat(storageRoot); err != nil {
		t.Fatalf("storage root should exist before DropData: %v", err)
	}
	siblingRoot := filepath.Join(configstorage.GetDBFolderPath(), "not-fallback-html")
	siblingFile := filepath.Join(siblingRoot, "keep.txt")
	if err := os.MkdirAll(siblingRoot, 0o750); err != nil {
		t.Fatalf("create sibling storage: %v", err)
	}
	if err := os.WriteFile(siblingFile, []byte("keep"), 0o640); err != nil {
		t.Fatalf("write sibling storage: %v", err)
	}

	if err := (component{}).DropData(ctx, lifecycle.Context{}); err != nil {
		t.Fatalf("DropData: %v", err)
	}
	if db.Migrator().HasTable(&fallbackdomain.Site{}) {
		t.Fatalf("fallback_html_sites table still exists after DropData")
	}
	var fallbackDrafts int64
	if err := db.Model(&model.InboundDraft{}).Where("source LIKE ?", "fallback-html:%").Count(&fallbackDrafts).Error; err != nil {
		t.Fatalf("count removed core drafts: %v", err)
	}
	if fallbackDrafts != 0 {
		t.Fatalf("component-owned core drafts remain after DropData: %d", fallbackDrafts)
	}
	var foreignDrafts int64
	if err := db.Model(&model.InboundDraft{}).Where("source = ?", "other-component").Count(&foreignDrafts).Error; err != nil {
		t.Fatalf("count foreign core drafts: %v", err)
	}
	if foreignDrafts != 1 {
		t.Fatalf("DropData removed foreign core drafts: %d", foreignDrafts)
	}
	if _, err := os.Stat(storageRoot); !os.IsNotExist(err) {
		t.Fatalf("storage root after DropData = %v, want not exists", err)
	}
	if _, err := os.Stat(siblingFile); err != nil {
		t.Fatalf("DropData removed non-component storage: %v", err)
	}
}

func setComponentTestSetting(t *testing.T, key string, value string) {
	t.Helper()
	db := dbsqlite.DB()
	if err := db.Where("key = ?", key).Delete(&model.Setting{}).Error; err != nil {
		t.Fatalf("delete setting %s: %v", key, err)
	}
	if err := db.Create(&model.Setting{Key: key, Value: value}).Error; err != nil {
		t.Fatalf("create setting %s: %v", key, err)
	}
}
