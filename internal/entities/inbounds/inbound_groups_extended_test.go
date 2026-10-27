//go:build shtorm_extended

package entityinbounds

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
)

func TestBondRendersCanonicalMembersAndReappliesOneOwningGroup(t *testing.T) {
	db := newInboundDB(t)
	if err := db.AutoMigrate(&model.Client{}); err != nil {
		t.Fatal(err)
	}
	for _, row := range []model.Inbound{
		{Id: 1, Type: "socks", Tag: "first", Options: json.RawMessage(`{"listen":"127.0.0.1","listen_port":1080}`)},
		{Id: 2, Type: "socks", Tag: "second", Options: json.RawMessage(`{"listen":"127.0.0.1","listen_port":1081}`)},
		{Id: 3, Type: "bond", Tag: "group", Options: json.RawMessage(`{"inbounds":["second","first"]}`)},
	} {
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	client := model.Client{Id: 7, Enable: true, Inbounds: json.RawMessage(`[1,2]`), Config: json.RawMessage(`{"socks":{"username":"member-user","password":"fixture-only"}}`)}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	rendered, err := GetAllConfig(db, StoredUsers{})
	if err != nil || len(rendered) != 1 {
		t.Fatal("group was not the sole top-level serving owner", err)
	}
	var config struct {
		Tag      string `json:"tag"`
		Inbounds []struct {
			Tag   string                                `json:"tag"`
			Port  uint16                                `json:"listen_port"`
			Users []struct{ Username, Password string } `json:"users"`
		} `json:"inbounds"`
	}
	if json.Unmarshal(rendered[0], &config) != nil || config.Tag != "group" || len(config.Inbounds) != 2 || config.Inbounds[0].Tag != "second" || config.Inbounds[0].Port != 1081 || len(config.Inbounds[0].Users) != 1 || config.Inbounds[0].Users[0].Username != "member-user" {
		t.Fatal("canonical member options, order or Users state were lost")
	}
	core := &fakeInboundCore{running: true}
	if err := Restart(db, []uint{1, 2}, core, StoredUsers{}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(core.removed, []string{"group"}) || !reflect.DeepEqual(core.added, []string{"group"}) || !reflect.DeepEqual(core.closed, []string{"group", "second", "first"}) {
		t.Fatal("member change did not retire/reapply the single root and all member sessions")
	}
	if _, err := Save(SaveRequest{Tx: db, Action: "del", Data: json.RawMessage(`"first"`)}); err == nil {
		t.Fatal("referenced canonical member was deleted")
	}
	change, err := CoreChangeForSavedRow(db, 3, "group", "")
	if err != nil || !change.NeedsRestart {
		t.Fatal("structural group change bypassed complete-candidate restart", err)
	}
	if err := db.AutoMigrate(&model.InboundEndpointLease{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.InboundEndpointLease{LeaseID: "bond-member-lease", InboundID: 2, State: "ACTIVE", ExactReferenceJSON: []byte(`{}`), LeaseJSON: []byte(`{}`)}).Error; err != nil {
		t.Fatal(err)
	}
	_, err = SaveImportedCandidate(db, model.Inbound{Id: 3, Type: "bond", Tag: "group", Options: json.RawMessage(`{"inbounds":["first"]}`)}, false, "")
	if !errors.Is(err, ErrInboundEndpointLeased) {
		t.Fatal("import changed an old leased member's serving ownership", err)
	}
	if _, err := Save(SaveRequest{Tx: db, Action: "del", Data: json.RawMessage(`"group"`)}); !errors.Is(err, ErrInboundEndpointLeased) {
		t.Fatal("deletion changed a leased member's serving ownership", err)
	}
	if err := db.Model(&model.InboundEndpointLease{}).Where("lease_id = ?", "bond-member-lease").Update("state", "RELEASED").Error; err != nil {
		t.Fatal(err)
	}
	change, err = Save(SaveRequest{Tx: db, Action: "del", Data: json.RawMessage(`"group"`)})
	if err != nil || !change.NeedsRestart {
		t.Fatal("group removal did not restore members through complete-candidate restart", err)
	}
	var count int64
	if err := db.Model(&model.Inbound{}).Count(&count).Error; err != nil || count != 2 {
		t.Fatal("removing a group removed its canonical member state", err)
	}
}
