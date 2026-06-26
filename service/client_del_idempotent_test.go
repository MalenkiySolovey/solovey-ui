package service

import (
	"encoding/json"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
)

func newTestClient(t *testing.T, name string) model.Client {
	t.Helper()
	client := model.Client{
		Enable:      true,
		Name:        name,
		Config:      json.RawMessage(`{"mixed":{"username":"` + name + `","password":"pw"}}`),
		Inbounds:    json.RawMessage(`[]`),
		Links:       json.RawMessage(`[]`),
		IPLimitMode: "monitor",
	}
	if err := dbsqlite.DB().Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	return client
}

func clientExists(t *testing.T, id uint) bool {
	t.Helper()
	var count int64
	if err := dbsqlite.DB().Model(model.Client{}).Where("id = ?", id).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	return count > 0
}

func TestConfigSaveClientsDelIsIdempotent(t *testing.T) {
	initSettingTestDB(t)

	client := newTestClient(t, "todelete")
	cs := NewConfigServiceWithRuntime(NewRuntimeWithCoreProvider(nil))
	payload, err := json.Marshal(client.Id)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := cs.Save("clients", "del", payload, "", "admin", "example.com"); err != nil {
		t.Fatalf("deleting an existing client should succeed: %v", err)
	}
	if clientExists(t, client.Id) {
		t.Fatal("client should be gone after delete")
	}

	if _, err := cs.Save("clients", "del", payload, "", "admin", "example.com"); err != nil {
		t.Fatalf("deleting an already-gone client should be a no-op success, got: %v", err)
	}
}

func TestConfigSaveClientsDelBulkSkipsMissingIds(t *testing.T) {
	initSettingTestDB(t)

	present := newTestClient(t, "present")
	cs := NewConfigServiceWithRuntime(NewRuntimeWithCoreProvider(nil))

	payload, err := json.Marshal([]uint{present.Id, 999999})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := cs.Save("clients", "delbulk", payload, "", "admin", "example.com"); err != nil {
		t.Fatalf("delbulk with a missing id should still succeed: %v", err)
	}
	if clientExists(t, present.Id) {
		t.Fatal("present client should be deleted by delbulk")
	}
}
