package service

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	entityclients "github.com/MalenkiySolovey/solovey-ui/internal/entities/clients"
)

func TestClientInboundMembershipFindsClientsAndNames(t *testing.T) {
	initSettingTestDB(t)

	inA := model.Inbound{Type: "vmess", Tag: "in-a", Options: json.RawMessage(`{}`)}
	inB := model.Inbound{Type: "vmess", Tag: "in-b", Options: json.RawMessage(`{}`)}
	if err := dbsqlite.DB().Create(&inA).Error; err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Create(&inB).Error; err != nil {
		t.Fatal(err)
	}

	alice := model.Client{Name: "alice", Inbounds: json.RawMessage(fmt.Sprintf("[%d,%d]", inA.Id, inB.Id))}
	bob := model.Client{Name: "bob", Inbounds: json.RawMessage(fmt.Sprintf("[%d]", inB.Id))}
	cara := model.Client{Name: "cara", Inbounds: json.RawMessage(`[]`)}
	if err := dbsqlite.DB().Create(&alice).Error; err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Create(&bob).Error; err != nil {
		t.Fatal(err)
	}
	if err := dbsqlite.DB().Create(&cara).Error; err != nil {
		t.Fatal(err)
	}

	clients, err := entityclients.ByInbound(dbsqlite.DB(), inB.Id)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, client := range clients {
		names = append(names, client.Name)
	}
	if !reflect.DeepEqual(names, []string{"alice", "bob"}) {
		t.Fatalf("clientsByInbound names = %v, want alice and bob", names)
	}

	usersByInbound, err := entityclients.NamesByInboundIDs(dbsqlite.DB(), []uint{inA.Id, inB.Id, 999})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(usersByInbound[inA.Id], []string{"alice"}) {
		t.Fatalf("names for inA = %v, want alice", usersByInbound[inA.Id])
	}
	if !reflect.DeepEqual(usersByInbound[inB.Id], []string{"alice", "bob"}) {
		t.Fatalf("names for inB = %v, want alice and bob", usersByInbound[inB.Id])
	}
	if usersByInbound[999] == nil || len(usersByInbound[999]) != 0 {
		t.Fatalf("unknown inbound should be present with an empty user list, got %v", usersByInbound[999])
	}
}

func TestClientInboundMembershipMutatesJSON(t *testing.T) {
	added, ok, err := entityclients.AppendInbound(7, json.RawMessage(`[1]`), 2, "test add")
	if err != nil || !ok {
		t.Fatalf("appendClientInbound ok=%v err=%v", ok, err)
	}
	assertJSONUintSlice(t, added, []uint{1, 2})

	removed, ok, err := entityclients.RemoveInbound(7, added, 1, "test remove")
	if err != nil || !ok {
		t.Fatalf("removeClientInbound ok=%v err=%v", ok, err)
	}
	assertJSONUintSlice(t, removed, []uint{2})

	if _, ok, err := entityclients.AppendInbound(7, json.RawMessage(`{bad`), 2, "test add"); err != nil || ok {
		t.Fatalf("invalid inbound JSON should skip without error, ok=%v err=%v", ok, err)
	}
}

func assertJSONUintSlice(t *testing.T, raw json.RawMessage, want []uint) {
	t.Helper()

	var got []uint
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal %q: %v", raw, err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decoded %q = %v, want %v", raw, got, want)
	}
}
