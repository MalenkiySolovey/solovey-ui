package entityclients

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newClientDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Client{}, &model.Inbound{}, &model.Tls{}); err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	return db
}

func TestSupportedActions(t *testing.T) {
	want := []string{"new", "edit", "addbulk", "editbulk", "delbulk", "del"}
	if got := SupportedActionStrings(); !reflect.DeepEqual(got, want) {
		t.Fatalf("supported client save actions = %#v, want %#v", got, want)
	}

	got := make([]string, 0, len(supportedSaveActions))
	for _, action := range supportedSaveActions {
		got = append(got, string(action))
	}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("client save actions = %#v, want %#v", got, want)
	}
}

func TestParseAction(t *testing.T) {
	action, ok := ParseAction("editbulk")
	if !ok {
		t.Fatal("expected editbulk action to be supported")
	}
	if action != ActionEditBulk {
		t.Fatalf("parsed action = %q, want %q", action, ActionEditBulk)
	}
	if _, ok := ParseAction("mystery"); ok {
		t.Fatal("unexpected support for unknown client save action")
	}
}

func TestSaveRejectsUnknownAction(t *testing.T) {
	_, err := Save(SaveRequest{Action: "mystery"})
	if err == nil {
		t.Fatal("expected unknown action to be rejected")
	}
	if err.Error() != "unknown action: mystery" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDecodeLinksTreatsEmptyAsEmptyList(t *testing.T) {
	for _, raw := range []json.RawMessage{nil, []byte(""), []byte("  "), []byte("null")} {
		got, ok := DecodeLinks(7, raw, "test")
		if !ok {
			t.Fatalf("empty links %q should decode", raw)
		}
		if len(got) != 0 {
			t.Fatalf("empty links %q decoded to %#v, want empty", raw, got)
		}
	}
}

func TestRebuildLinksNeverEmitsNull(t *testing.T) {
	keepAll := func(Link) bool { return true }

	links, ok, err := RebuildLinks(1, json.RawMessage(`{}`), json.RawMessage(`[]`), nil, "host", keepAll, "test")
	if err != nil || !ok {
		t.Fatalf("rebuild with empty inputs: ok=%v err=%v", ok, err)
	}
	if string(links) != "[]" {
		t.Fatalf("empty rebuild must marshal to [], got %q", links)
	}

	if _, ok, _ := RebuildLinks(1, json.RawMessage(`{}`), json.RawMessage(`{bad`), nil, "host", keepAll, "test"); ok {
		t.Fatal("invalid stored links must report ok=false")
	}
}

func TestSaveDeleteBulkReturnsUniqueInboundIDs(t *testing.T) {
	db := newClientDB(t)
	clients := []model.Client{
		{Name: "delete-a", Inbounds: json.RawMessage(`[1,2]`), Links: json.RawMessage(`[]`), Config: json.RawMessage(`{}`)},
		{Name: "delete-b", Inbounds: json.RawMessage(`[2,3]`), Links: json.RawMessage(`[]`), Config: json.RawMessage(`{}`)},
	}
	if err := db.Create(&clients).Error; err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal([]uint{clients[0].Id, clients[1].Id})
	if err != nil {
		t.Fatal(err)
	}

	inboundIDs, err := Save(SaveRequest{Tx: db, Action: "delbulk", Data: payload, Hostname: "example.com"})
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(inboundIDs, func(i, j int) bool { return inboundIDs[i] < inboundIDs[j] })
	if !reflect.DeepEqual(inboundIDs, []uint{1, 2, 3}) {
		t.Fatalf("inbound IDs = %#v, want [1 2 3]", inboundIDs)
	}
}

func TestPrepareSubSecretPreservesExisting(t *testing.T) {
	db := newClientDB(t)
	client := model.Client{Name: "alice", SubSecret: "keep", Inbounds: json.RawMessage(`[]`)}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	next := model.Client{Id: client.Id, Name: "alice", Inbounds: json.RawMessage(`[]`)}
	if err := PrepareSubSecret(db, &next, true); err != nil {
		t.Fatal(err)
	}
	if next.SubSecret != "keep" {
		t.Fatalf("sub secret = %q, want keep", next.SubSecret)
	}
	if next.IPLimitMode != "monitor" {
		t.Fatalf("ip limit mode = %q, want monitor", next.IPLimitMode)
	}
}
