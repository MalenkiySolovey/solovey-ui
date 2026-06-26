package service

import (
	"encoding/json"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
)

func TestResetClientsClampsZeroResetDays(t *testing.T) {
	initSettingTestDB(t)
	const now = int64(1_700_000_000)
	client := model.Client{
		Enable: true, Name: "zero-reset", Inbounds: json.RawMessage(`[1]`),
		Links: json.RawMessage(`[]`), Config: json.RawMessage(`{}`), AutoReset: true,
		NextReset: now - 1, ResetDays: 0, Up: 10, Down: 20,
	}
	if err := dbsqlite.DB().Create(&client).Error; err != nil {
		t.Fatal(err)
	}

	if _, err := (&ClientService{}).ResetClients(dbsqlite.DB(), now); err != nil {
		t.Fatal(err)
	}
	var got model.Client
	if err := dbsqlite.DB().First(&got, client.Id).Error; err != nil {
		t.Fatal(err)
	}
	if got.NextReset != now+86400 {
		t.Fatalf("resetDays=0 must clamp to one day: got %d want %d", got.NextReset, now+86400)
	}

	if err := dbsqlite.DB().Model(&model.Client{}).Where("id = ?", client.Id).Updates(map[string]interface{}{"up": 5, "down": 7}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := (&ClientService{}).ResetClients(dbsqlite.DB(), now+120); err != nil {
		t.Fatal(err)
	}
	var after model.Client
	if err := dbsqlite.DB().First(&after, client.Id).Error; err != nil {
		t.Fatal(err)
	}
	if after.Up != 5 || after.Down != 7 {
		t.Fatalf("client was reset again too early: up=%d down=%d", after.Up, after.Down)
	}
}
