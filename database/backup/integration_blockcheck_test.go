//go:build !minimal

package backup_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/MalenkiySolovey/solovey-backend/settings/store"
	"github.com/MalenkiySolovey/solovey-backend/storage/model"
	feature "github.com/MalenkiySolovey/solovey-component-blockcheck"
	_ "github.com/MalenkiySolovey/solovey-ui/components/blockcheck"
	dbbackup "github.com/MalenkiySolovey/solovey-ui/database/backup"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/service"
)

func TestBlockCheckBackupRestorePreservesCatalogRevisionAndUserEdits(t *testing.T) {
	initBackupRestoreIntegrationDB(t)
	if _, err := (&service.SettingService{}).GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	catalog, err := feature.DecodeCatalog(nil)
	if err != nil {
		t.Fatal(err)
	}
	edited, err := catalog.Apply(feature.CatalogChange{Action: "delete", ID: "youtube", ExpectedRevision: catalog.Revision()})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(edited)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertValue(dbsqlite.DB(), feature.CatalogSettingKey, string(payload)); err != nil {
		t.Fatal(err)
	}
	archive, err := dbbackup.Export("")
	if err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Where("key = ?", feature.CatalogSettingKey).Delete(&model.Setting{}).Error; err != nil {
		t.Fatal(err)
	}
	dbbackup.SetSendSighupHook(func() error { return nil })
	t.Cleanup(func() { dbbackup.SetSendSighupHook(nil) })
	if err := dbbackup.Restore(integrationMemMultipartFile{Reader: bytes.NewReader(archive)}); err != nil {
		t.Fatal(err)
	}
	var row model.Setting
	if err := dbsqlite.DB().Where("key = ?", feature.CatalogSettingKey).Take(&row).Error; err != nil {
		t.Fatal(err)
	}
	restored, err := feature.DecodeCatalog([]byte(row.Value))
	if err != nil || restored.Revision() != edited.Revision() {
		t.Fatalf("catalog revision did not survive restore: %v", err)
	}
	if _, exists := restored.Group("youtube"); exists {
		t.Fatal("restore resurrected a deleted default group")
	}
}
