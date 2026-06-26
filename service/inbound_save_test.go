package service

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	entityclients "github.com/MalenkiySolovey/solovey-ui/internal/entities/clients"
)

func TestInboundSaveDeleteUpdatesClientsAndDeletesInbound(t *testing.T) {
	initSettingTestDB(t)
	inbound := model.Inbound{
		Type:    "trojan",
		Tag:     "delete-in",
		Addrs:   json.RawMessage(`[]`),
		Options: json.RawMessage(`{"listen":"0.0.0.0","listen_port":443}`),
	}
	if err := dbsqlite.DB().Create(&inbound).Error; err != nil {
		t.Fatal(err)
	}
	client := model.Client{
		Name:     "delete-client",
		Inbounds: json.RawMessage(fmt.Sprintf("[%d]", inbound.Id)),
		Config:   json.RawMessage(`{}`),
		Links: json.RawMessage(`[
			{"remark":"delete-in","type":"local","uri":"drop-local"},
			{"remark":"delete-in","type":"external","uri":"drop-external"},
			{"remark":"keep-in","type":"local","uri":"keep-local"}
		]`),
	}
	if err := dbsqlite.DB().Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(inbound.Tag)
	if err != nil {
		t.Fatal(err)
	}

	if err := (&InboundService{}).Save(dbsqlite.DB(), "del", payload, "", "example.com"); err != nil {
		t.Fatal(err)
	}

	var inboundCount int64
	if err := dbsqlite.DB().Model(model.Inbound{}).Where("id = ?", inbound.Id).Count(&inboundCount).Error; err != nil {
		t.Fatal(err)
	}
	if inboundCount != 0 {
		t.Fatalf("inbound should be deleted, count=%d", inboundCount)
	}

	var got model.Client
	if err := dbsqlite.DB().Where("id = ?", client.Id).First(&got).Error; err != nil {
		t.Fatal(err)
	}
	inbounds, ok := entityclients.DecodeInbounds(got.Id, got.Inbounds, "test")
	if !ok {
		t.Fatal("updated client inbounds should decode")
	}
	if len(inbounds) != 0 {
		t.Fatalf("client inbounds = %#v, want empty", inbounds)
	}
	assertKept(t, linkURIs(t, got.Links),
		[]string{"keep-local"},
		[]string{"drop-local", "drop-external"})
}
