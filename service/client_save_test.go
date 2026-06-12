package service

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
)

func TestClientSaveHandlersCoverSupportedActions(t *testing.T) {
	want := []string{"new", "edit", "addbulk", "editbulk", "delbulk", "del"}
	if got := supportedClientSaveActionStrings(); !reflect.DeepEqual(got, want) {
		t.Fatalf("supported client save actions = %#v, want %#v", got, want)
	}

	got := make([]string, 0, len(clientSaveHandlers))
	for action := range clientSaveHandlers {
		got = append(got, string(action))
	}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("client save handlers = %#v, want %#v", got, want)
	}
}

func TestParseClientSaveAction(t *testing.T) {
	action, ok := parseClientSaveAction("editbulk")
	if !ok {
		t.Fatal("expected editbulk action to be supported")
	}
	if action != clientSaveActionEditBulk {
		t.Fatalf("parsed action = %q, want %q", action, clientSaveActionEditBulk)
	}
	if _, ok := parseClientSaveAction("mystery"); ok {
		t.Fatal("unexpected support for unknown client save action")
	}
}

func TestClientSaveRejectsUnknownAction(t *testing.T) {
	_, err := (&ClientService{}).applyClientSave(clientSaveRequest{action: "mystery"})
	if err == nil {
		t.Fatal("expected unknown action to be rejected")
	}
	if err.Error() != "unknown action: mystery" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClientSaveDeleteBulkReturnsUniqueInboundIDs(t *testing.T) {
	initSettingTestDB(t)
	clients := []model.Client{
		{Name: "delete-a", Inbounds: json.RawMessage(`[1,2]`), Links: json.RawMessage(`[]`), Config: json.RawMessage(`{}`)},
		{Name: "delete-b", Inbounds: json.RawMessage(`[2,3]`), Links: json.RawMessage(`[]`), Config: json.RawMessage(`{}`)},
	}
	if err := database.GetDB().Create(&clients).Error; err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal([]uint{clients[0].Id, clients[1].Id})
	if err != nil {
		t.Fatal(err)
	}

	inboundIDs, err := (&ClientService{}).Save(database.GetDB(), "delbulk", payload, "example.com")
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(inboundIDs, func(i, j int) bool { return inboundIDs[i] < inboundIDs[j] })
	if !reflect.DeepEqual(inboundIDs, []uint{1, 2, 3}) {
		t.Fatalf("inbound IDs = %#v, want [1 2 3]", inboundIDs)
	}

	var count int64
	if err := database.GetDB().Model(model.Client{}).Where("id IN ?", []uint{clients[0].Id, clients[1].Id}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("deleted clients remaining count = %d", count)
	}
}
