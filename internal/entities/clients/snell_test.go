package entityclients

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
)

func snellKey(t *testing.T, client model.Client) string {
	t.Helper()
	var config struct {
		Snell struct{ Name, UserKey string }
	}
	if err := json.Unmarshal(client.Config, &config); err != nil {
		t.Fatal(err)
	}
	if config.Snell.Name != client.Name {
		t.Fatal("runtime name not synchronized")
	}
	return config.Snell.UserKey
}

func TestSnellCredentialInitializationAndPersistence(t *testing.T) {
	db := newClientDB(t)
	if err := db.Create(&model.Inbound{Id: 1, Type: "snell", Tag: "snell"}).Error; err != nil {
		t.Fatal(err)
	}
	client := model.Client{Id: 7, Name: "alice", Inbounds: json.RawMessage(`[1]`), Config: json.RawMessage(`{"trojan":{"password":"retained"}}`), Up: 101, Down: 202}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	if err := PrepareSnellCredential(db, &client); err != nil {
		t.Fatal(err)
	}
	key := snellKey(t, client)
	if len(key) != 32 || !strings.Contains(string(client.Config), "retained") {
		t.Fatal("invalid initialization or unrelated credential changed")
	}
	if err := db.Save(&client).Error; err != nil {
		t.Fatal(err)
	}
	for _, config := range []string{`{}`, `{"snell":{}}`, string(client.Config)} {
		next := client
		next.Name = "renamed"
		next.Config = json.RawMessage(config)
		if err := PrepareSnellCredential(db, &next); err != nil {
			t.Fatal(err)
		}
		if snellKey(t, next) != key || next.Id != 7 || next.Up != 101 || next.Down != 202 {
			t.Fatal("edit rotated key or identity/counters")
		}
	}
	for _, invalid := range []string{`null`, `{"userkey":""}`, `{"userkey":42}`, `{"userkey":"` + strings.Repeat("x", 256) + `"}`} {
		next := client
		next.Config = json.RawMessage(`{"snell":` + invalid + `}`)
		if err := PrepareSnellCredential(db, &next); err == nil {
			t.Fatal("invalid explicit credential accepted")
		}
	}
	list, err := GetAll(db)
	if err != nil || len(*list) != 1 || len((*list)[0].Config) != 0 {
		t.Fatal("list exposed credential config")
	}
}

func TestSnellCredentialCollisionUsesCandidateMembership(t *testing.T) {
	db := newClientDB(t)
	if err := db.Create(&[]model.Inbound{{Id: 1, Type: "snell", Tag: "one"}, {Id: 2, Type: "snell", Tag: "two"}}).Error; err != nil {
		t.Fatal(err)
	}
	one := &model.Client{Name: "one", Inbounds: json.RawMessage(`[1]`), Config: json.RawMessage(`{"snell":{"name":"one","userkey":"same"}}`)}
	two := &model.Client{Name: "two", Inbounds: json.RawMessage(`[1]`), Config: json.RawMessage(`{"snell":{"name":"two","userkey":"same"}}`)}
	if err := validateSnellUserKeys(db, []*model.Client{one, two}); err == nil {
		t.Fatal("batch collision accepted")
	}
	if err := db.Create(one).Error; err != nil {
		t.Fatal(err)
	}
	if err := validateSnellUserKeys(db, []*model.Client{two}); err == nil {
		t.Fatal("persisted collision accepted")
	}
	two.Inbounds = json.RawMessage(`[2]`)
	if err := validateSnellUserKeys(db, []*model.Client{two}); err != nil {
		t.Fatal(err)
	}
	if err := validateSnellUserKeys(db, []*model.Client{one}); err != nil {
		t.Fatal("same client edit collided with old state")
	}
}
