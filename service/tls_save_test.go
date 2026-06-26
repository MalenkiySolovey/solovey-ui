package service

import (
	"encoding/json"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
)

func TestTLSSaveDeleteRejectsTLSInUseByInbound(t *testing.T) {
	initSettingTestDB(t)
	tls := model.Tls{Name: "used"}
	if err := dbsqlite.DB().Create(&tls).Error; err != nil {
		t.Fatal(err)
	}
	inbound := model.Inbound{
		Type:    "trojan",
		Tag:     "uses-tls",
		TlsId:   tls.Id,
		Addrs:   json.RawMessage(`[]`),
		Options: json.RawMessage(`{}`),
	}
	if err := dbsqlite.DB().Create(&inbound).Error; err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(tls.Id)
	if err != nil {
		t.Fatal(err)
	}

	err = (&TlsService{}).Save(dbsqlite.DB(), "del", payload, "example.com")
	if err == nil {
		t.Fatal("expected TLS in use to be rejected")
	}
	if err.Error() != "tls in use" {
		t.Fatalf("unexpected error: %v", err)
	}

	var count int64
	if err := dbsqlite.DB().Model(model.Tls{}).Where("id = ?", tls.Id).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("TLS row should remain after rejected delete, count=%d", count)
	}
}

func TestTLSSaveDeleteRemovesUnusedTLS(t *testing.T) {
	initSettingTestDB(t)
	tls := model.Tls{Name: "unused"}
	if err := dbsqlite.DB().Create(&tls).Error; err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(tls.Id)
	if err != nil {
		t.Fatal(err)
	}
	if err := (&TlsService{}).Save(dbsqlite.DB(), "del", payload, "example.com"); err != nil {
		t.Fatal(err)
	}

	var count int64
	if err := dbsqlite.DB().Model(model.Tls{}).Where("id = ?", tls.Id).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("TLS row should be deleted, count=%d", count)
	}
}
