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
	if err := db.AutoMigrate(&model.Client{}, &model.ClientIP{}, &model.Inbound{}, &model.Tls{}); err != nil {
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

func TestValidateStoredRejectsMissingAndDuplicateSubSecret(t *testing.T) {
	db := newClientDB(t)
	first := model.Client{Name: "first", SubSecret: "same", Inbounds: json.RawMessage(`[]`)}
	second := model.Client{Name: "second", SubSecret: "same", Inbounds: json.RawMessage(`[]`)}
	if err := db.Create(&[]model.Client{first, second}).Error; err != nil {
		t.Fatal(err)
	}
	if err := ValidateStored(db); err == nil || !strings.Contains(err.Error(), "duplicates subscription secret") {
		t.Fatalf("ValidateStored duplicate error = %v", err)
	}
	if err := db.Model(&model.Client{}).Where("name = ?", "second").Update("sub_secret", "").Error; err != nil {
		t.Fatal(err)
	}
	if err := ValidateStored(db); err == nil || !strings.Contains(err.Error(), "invalid subscription secret") {
		t.Fatalf("ValidateStored missing error = %v", err)
	}
}

func TestPrepareSubSecretIgnoresCallerControlledValues(t *testing.T) {
	db := newClientDB(t)
	newClient := model.Client{Name: "new", SubSecret: "chosen-by-caller"}
	if err := PrepareSubSecret(db, &newClient, false); err != nil {
		t.Fatal(err)
	}
	if newClient.SubSecret == "chosen-by-caller" || newClient.SubSecret == "" {
		t.Fatalf("new client retained caller-controlled secret: %q", newClient.SubSecret)
	}

	stored := model.Client{Name: "stored", SubSecret: "persisted-secret", Inbounds: json.RawMessage(`[]`)}
	if err := db.Create(&stored).Error; err != nil {
		t.Fatal(err)
	}
	edit := model.Client{Id: stored.Id, Name: stored.Name, SubSecret: "replacement"}
	if err := PrepareSubSecret(db, &edit, true); err != nil {
		t.Fatal(err)
	}
	if edit.SubSecret != "persisted-secret" {
		t.Fatalf("edit secret = %q, want persisted-secret", edit.SubSecret)
	}
}

func TestSaveRejectsDuplicateNameAndNewActionWithID(t *testing.T) {
	db := newClientDB(t)
	existing := model.Client{Name: "alice", SubSecret: "secret", Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(`[]`), Config: json.RawMessage(`{}`)}
	if err := db.Create(&existing).Error; err != nil {
		t.Fatal(err)
	}
	for _, payload := range []model.Client{
		{Name: "alice", Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(`[]`), Config: json.RawMessage(`{}`)},
		{Id: existing.Id, Name: "replacement", Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(`[]`), Config: json.RawMessage(`{}`)},
	} {
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Save(SaveRequest{Tx: db, Action: "new", Data: raw, Hostname: "example.com"}); err == nil {
			t.Fatalf("invalid new client was accepted: %#v", payload)
		}
	}
	var stored model.Client
	if err := db.First(&stored, existing.Id).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Name != "alice" {
		t.Fatalf("existing client was overwritten: %#v", stored)
	}
}

func TestSaveRejectsInvalidClientNameAndMissingBatchPersistence(t *testing.T) {
	db := newClientDB(t)
	invalid, err := json.Marshal(model.Client{
		Name: "bad\nname", Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(`[]`), Config: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Save(SaveRequest{Tx: db, Action: "new", Data: invalid}); err == nil {
		t.Fatal("client with a control character in its name was accepted")
	}
	invalidLinks, err := json.Marshal(model.Client{
		Name: "bad-links", Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(`{}`), Config: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Save(SaveRequest{Tx: db, Action: "new", Data: invalidLinks}); err == nil {
		t.Fatal("client with non-array links was accepted")
	}

	bulk, err := json.Marshal([]model.Client{{
		Name: "batch-client", Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(`[]`), Config: json.RawMessage(`{}`),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Save(SaveRequest{Tx: db, Action: "addbulk", Data: bulk}); err == nil || !strings.Contains(err.Error(), "batch persistence") {
		t.Fatalf("bulk save without persistence owner error = %v", err)
	}
}

func TestSaveMovesAndDeletesClientIPHistory(t *testing.T) {
	db := newClientDB(t)
	client := model.Client{Name: "old", SubSecret: "secret", Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(`[]`), Config: json.RawMessage(`{}`)}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.ClientIP{ClientName: "old", IPHash: "hash", FirstSeen: 1, LastSeen: 2}).Error; err != nil {
		t.Fatal(err)
	}
	client.Name = "new"
	payload, err := json.Marshal(client)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Save(SaveRequest{Tx: db, Action: "edit", Data: payload, Hostname: "example.com"}); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&model.ClientIP{}).Where("client_name = ?", "new").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("renamed history count=%d err=%v", count, err)
	}
	deletePayload, _ := json.Marshal(client.Id)
	if _, err := Save(SaveRequest{Tx: db, Action: "del", Data: deletePayload}); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.ClientIP{}).Where("client_name = ?", "new").Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("deleted history count=%d err=%v", count, err)
	}
}
