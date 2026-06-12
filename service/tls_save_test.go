package service

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database"
	"github.com/MalenkiySolovey/solovey-ui/database/model"
)

func TestTLSSaveHandlersCoverSupportedActions(t *testing.T) {
	want := []string{"new", "edit", "del"}
	if got := supportedTLSSaveActionStrings(); !reflect.DeepEqual(got, want) {
		t.Fatalf("supported TLS save actions = %#v, want %#v", got, want)
	}

	got := make([]string, 0, len(tlsSaveHandlers))
	for action := range tlsSaveHandlers {
		got = append(got, string(action))
	}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("TLS save handlers = %#v, want %#v", got, want)
	}
}

func TestParseTLSSaveAction(t *testing.T) {
	action, ok := parseTLSSaveAction("edit")
	if !ok {
		t.Fatal("expected edit action to be supported")
	}
	if action != tlsSaveActionEdit {
		t.Fatalf("parsed action = %q, want %q", action, tlsSaveActionEdit)
	}
	if _, ok := parseTLSSaveAction("mystery"); ok {
		t.Fatal("unexpected support for unknown TLS save action")
	}
}

func TestTLSSaveKeepsUnknownActionNoopCompatibility(t *testing.T) {
	if err := (&TlsService{}).applyTLSSave(tlsSaveRequest{action: "mystery"}); err != nil {
		t.Fatalf("unknown TLS action should stay a no-op for compatibility, got %v", err)
	}
}

func TestTLSSaveDeleteRejectsTLSInUseByInbound(t *testing.T) {
	initSettingTestDB(t)
	tls := model.Tls{Name: "used"}
	if err := database.GetDB().Create(&tls).Error; err != nil {
		t.Fatal(err)
	}
	inbound := model.Inbound{
		Type:    "trojan",
		Tag:     "uses-tls",
		TlsId:   tls.Id,
		Addrs:   json.RawMessage(`[]`),
		Options: json.RawMessage(`{}`),
	}
	if err := database.GetDB().Create(&inbound).Error; err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(tls.Id)
	if err != nil {
		t.Fatal(err)
	}

	err = (&TlsService{}).Save(database.GetDB(), "del", payload, "example.com")
	if err == nil {
		t.Fatal("expected TLS in use to be rejected")
	}
	if err.Error() != "tls in use" {
		t.Fatalf("unexpected error: %v", err)
	}

	var count int64
	if err := database.GetDB().Model(model.Tls{}).Where("id = ?", tls.Id).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("TLS row should remain after rejected delete, count=%d", count)
	}
}

func TestTLSSaveDeleteRemovesUnusedTLS(t *testing.T) {
	initSettingTestDB(t)
	tls := model.Tls{Name: "unused"}
	if err := database.GetDB().Create(&tls).Error; err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(tls.Id)
	if err != nil {
		t.Fatal(err)
	}
	if err := (&TlsService{}).Save(database.GetDB(), "del", payload, "example.com"); err != nil {
		t.Fatal(err)
	}

	var count int64
	if err := database.GetDB().Model(model.Tls{}).Where("id = ?", tls.Id).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("TLS row should be deleted, count=%d", count)
	}
}
