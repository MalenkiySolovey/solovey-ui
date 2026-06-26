package entitytls

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

func newTLSDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Tls{}, &model.Inbound{}, &model.Service{}); err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	return db
}

func TestSupportedActions(t *testing.T) {
	want := []string{"new", "edit", "del"}
	if got := SupportedActionStrings(); !reflect.DeepEqual(got, want) {
		t.Fatalf("supported TLS save actions = %#v, want %#v", got, want)
	}

	got := make([]string, 0, len(supportedSaveActions))
	for _, action := range supportedSaveActions {
		got = append(got, string(action))
	}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("TLS save actions = %#v, want %#v", got, want)
	}
}

func TestParseAction(t *testing.T) {
	action, ok := ParseAction("edit")
	if !ok {
		t.Fatal("expected edit action to be supported")
	}
	if action != ActionEdit {
		t.Fatalf("parsed action = %q, want %q", action, ActionEdit)
	}
	if _, ok := ParseAction("mystery"); ok {
		t.Fatal("unexpected support for unknown TLS save action")
	}
}

func TestSaveKeepsUnknownActionNoopCompatibility(t *testing.T) {
	if err := Save(SaveRequest{Action: "mystery"}); err != nil {
		t.Fatalf("unknown TLS action should stay a no-op for compatibility, got %v", err)
	}
}

func TestSaveConfigKeepsExistingSortOrder(t *testing.T) {
	db := newTLSDB(t)
	tls := model.Tls{Name: "existing", SortOrder: 7}
	if err := db.Create(&tls).Error; err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(model.Tls{Id: tls.Id, Name: "renamed"})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := SaveConfig(db, payload)
	if err != nil {
		t.Fatal(err)
	}
	if saved.SortOrder != 7 {
		t.Fatalf("sort order = %d, want 7", saved.SortOrder)
	}
}

func TestDeleteRejectsTLSInUseByInbound(t *testing.T) {
	db := newTLSDB(t)
	tls := model.Tls{Name: "used"}
	if err := db.Create(&tls).Error; err != nil {
		t.Fatal(err)
	}
	inbound := model.Inbound{
		Type:    "trojan",
		Tag:     "uses-tls",
		TlsId:   tls.Id,
		Addrs:   json.RawMessage(`[]`),
		Options: json.RawMessage(`{}`),
	}
	if err := db.Create(&inbound).Error; err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(tls.Id)
	if err != nil {
		t.Fatal(err)
	}

	err = Delete(db, payload)
	if err == nil {
		t.Fatal("expected TLS in use to be rejected")
	}
	if err.Error() != "tls in use" {
		t.Fatalf("unexpected error: %v", err)
	}

	var count int64
	if err := db.Model(model.Tls{}).Where("id = ?", tls.Id).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("TLS row should remain after rejected delete, count=%d", count)
	}
}

func TestDeleteRemovesUnusedTLS(t *testing.T) {
	db := newTLSDB(t)
	tls := model.Tls{Name: "unused"}
	if err := db.Create(&tls).Error; err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(tls.Id)
	if err != nil {
		t.Fatal(err)
	}
	if err := Delete(db, payload); err != nil {
		t.Fatal(err)
	}

	var count int64
	if err := db.Model(model.Tls{}).Where("id = ?", tls.Id).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("TLS row should be deleted, count=%d", count)
	}
}

func TestInboundIDsFromRows(t *testing.T) {
	got := InboundIDsFromRows([]model.Inbound{{Id: 3}, {Id: 7}})
	want := []uint{3, 7}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("inbound IDs = %#v, want %#v", got, want)
	}
}
