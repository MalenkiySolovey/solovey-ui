package backup_test

import (
	"bytes"
	"encoding/json"
	"testing"

	dbbackup "github.com/MalenkiySolovey/solovey-ui/database/backup"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	backupenvelope "github.com/MalenkiySolovey/solovey-ui/internal/backup/envelope"
	entityclients "github.com/MalenkiySolovey/solovey-ui/internal/entities/clients"
	"github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/canonical"
	"github.com/MalenkiySolovey/solovey-ui/service"
)

func TestIntegrationPublicRemarkPrivateBackupRestore(t *testing.T) {
	initBackupRestoreIntegrationDB(t)
	if _, err := (&service.SettingService{}).GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	client := model.Client{Name: "private-auth", Desc: "private-description", Enable: true, SubSecret: "fixture-subscription-secret", Config: json.RawMessage(`{"trojan":{"password":"fixture"},"_subscription":{"publicRemark":"Public 雪"}}`), Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(`[]`)}
	if err := dbsqlite.DB().Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	backup, err := dbbackup.Export("")
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := backupenvelope.Build(backup, []byte("fixture-private-envelope"))
	if err != nil {
		t.Fatal(err)
	}
	restored, err := backupenvelope.Open(encrypted, []byte("fixture-private-envelope"))
	if err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Model(&model.Client{}).Where("id = ?", client.Id).Update("config", json.RawMessage(`{}`)).Error; err != nil {
		t.Fatal(err)
	}
	dbbackup.SetSendSighupHook(func() error { return nil })
	t.Cleanup(func() { dbbackup.SetSendSighupHook(nil) })
	if err := dbbackup.Restore(integrationMemMultipartFile{Reader: bytes.NewReader(restored)}); err != nil {
		t.Fatal(err)
	}
	var actual model.Client
	if err := dbsqlite.DB().First(&actual, client.Id).Error; err != nil {
		t.Fatal(err)
	}
	if actual.Name != client.Name || actual.Desc != client.Desc || actual.SubSecret != client.SubSecret {
		t.Fatal("private backup changed client identity")
	}
	if remark, err := canonical.ClientPublicRemark(actual.Config); err != nil || remark != "Public 雪" {
		t.Fatal("private backup lost public metadata")
	}
	if err := entityclients.ValidateStored(dbsqlite.DB()); err != nil {
		t.Fatal(err)
	}
}
