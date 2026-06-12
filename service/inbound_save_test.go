package service

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
)

func TestInboundSaveHandlersCoverSupportedActions(t *testing.T) {
	want := []string{"new", "edit", "del"}
	if got := supportedInboundSaveActionStrings(); !reflect.DeepEqual(got, want) {
		t.Fatalf("supported inbound save actions = %#v, want %#v", got, want)
	}

	got := make([]string, 0, len(inboundSaveHandlers))
	for action := range inboundSaveHandlers {
		got = append(got, string(action))
	}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("inbound save handlers = %#v, want %#v", got, want)
	}
}

func TestParseInboundSaveAction(t *testing.T) {
	action, ok := parseInboundSaveAction("edit")
	if !ok {
		t.Fatal("expected edit action to be supported")
	}
	if action != inboundSaveActionEdit {
		t.Fatalf("parsed action = %q, want %q", action, inboundSaveActionEdit)
	}
	if _, ok := parseInboundSaveAction("mystery"); ok {
		t.Fatal("unexpected support for unknown inbound save action")
	}
}

func TestInboundSaveRejectsUnknownAction(t *testing.T) {
	err := (&InboundService{}).applyInboundSave(inboundSaveRequest{action: "mystery"})
	if err == nil {
		t.Fatal("expected unknown action to be rejected")
	}
	if err.Error() != "unknown action: mystery" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInboundSaveDeleteUpdatesClientsAndDeletesInbound(t *testing.T) {
	initSettingTestDB(t)
	inbound := model.Inbound{
		Type:    "trojan",
		Tag:     "delete-in",
		Addrs:   json.RawMessage(`[]`),
		Options: json.RawMessage(`{"listen":"0.0.0.0","listen_port":443}`),
	}
	if err := database.GetDB().Create(&inbound).Error; err != nil {
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
	if err := database.GetDB().Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(inbound.Tag)
	if err != nil {
		t.Fatal(err)
	}

	if err := (&InboundService{}).Save(database.GetDB(), "del", payload, "", "example.com"); err != nil {
		t.Fatal(err)
	}

	var inboundCount int64
	if err := database.GetDB().Model(model.Inbound{}).Where("id = ?", inbound.Id).Count(&inboundCount).Error; err != nil {
		t.Fatal(err)
	}
	if inboundCount != 0 {
		t.Fatalf("inbound should be deleted, count=%d", inboundCount)
	}

	var got model.Client
	if err := database.GetDB().Where("id = ?", client.Id).First(&got).Error; err != nil {
		t.Fatal(err)
	}
	inbounds, ok := decodeClientInbounds(got.Id, got.Inbounds, "test")
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
