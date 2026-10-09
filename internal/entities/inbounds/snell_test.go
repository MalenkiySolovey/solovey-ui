package entityinbounds

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds/sessionidentity"
)

func TestSnellStoredUsersExportAndIdentity(t *testing.T) {
	db := newInboundDB(t)
	if err := db.AutoMigrate(&model.Client{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Inbound{Id: 1, Tag: "snell", Type: "snell"}).Error; err != nil {
		t.Fatal(err)
	}
	clients := []model.Client{
		{Id: 7, Name: "alice", Enable: true, SortOrder: 2, Inbounds: json.RawMessage(`[1]`), Config: json.RawMessage(`{"snell":{"name":"alice","userkey":"key-a"}}`)},
		{Id: 8, Name: "bob", Enable: true, SortOrder: 1, Inbounds: json.RawMessage(`[1]`), Config: json.RawMessage(`{"snell":{"name":"bob","userkey":"key-b"}}`)},
		{Id: 9, Name: "disabled", Enable: false, Inbounds: json.RawMessage(`[1]`), Config: json.RawMessage(`{"snell":{"name":"disabled","userkey":"key-c"}}`)},
	}
	if err := db.Create(&clients).Error; err != nil {
		t.Fatal(err)
	}
	users, err := (StoredUsers{}).FetchUsers(db, "snell", nil, 1)
	if err != nil || len(users) != 2 || string(users[0]) != `{"name":"bob","userkey":"key-b"}` {
		t.Fatal("users not deterministic or disabled user included")
	}
	again, err := (StoredUsers{}).FetchUsers(db, "snell", nil, 1)
	if err != nil || !reflect.DeepEqual(users, again) {
		t.Fatal("repeat changed generated users")
	}
	for _, version := range []int{5, 6} {
		config, _ := json.Marshal(map[string]any{"type": "snell", "tag": "snell", "version": version, "psk": "fixture-psk-12", "users": users})
		bindings, err := sessionidentity.Capture(db, config)
		if err != nil || len(bindings) != 2 {
			t.Fatal("actual generated credentials lost client binding")
		}
		row := model.Inbound{Tag: "snell", Type: "snell", Options: json.RawMessage(`{"version":5,"psk":"fixture-psk-12","obfs_mode":"http","listen_port":1234}`)}
		if version == 6 {
			row.Options = json.RawMessage(`{"version":6,"psk":"fixture-psk-12","mode":"unshaped","listen_port":1234}`)
		}
		if err := FillOutboundJSON(&row, "example.invalid"); err != nil {
			t.Fatal(err)
		}
		var outbound map[string]any
		if err := json.Unmarshal(row.OutJson, &outbound); err != nil {
			t.Fatal(err)
		}
		want := float64(4)
		if version == 6 {
			want = 6
		}
		if outbound["version"] != want || outbound["psk"] != "fixture-psk-12" {
			t.Fatal("wrong inbound/client version mapping")
		}
		if version == 5 && outbound["obfs_mode"] != "http" || version == 6 && outbound["mode"] != "unshaped" {
			t.Fatal("version-specific field lost")
		}
	}
}
