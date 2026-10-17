package entityclients

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
)

func TestClientSaveOwnsManualReasonAndRejectsJSONQuotaAuthority(t *testing.T) {
	db := newClientDB(t)
	data := json.RawMessage(`{"name":"manual-intent","enable":false,"inbounds":[],"links":[],"config":{},"accessDisableReason":"QUOTA_EXHAUSTED","access_disable_reason":"QUOTA_EXHAUSTED"}`)
	if _, err := Save(SaveRequest{Tx: db, Action: "new", Data: data}); err != nil {
		t.Fatal(err)
	}
	var row model.Client
	if err := db.Where("name = ?", "manual-intent").First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.Enable || row.AccessDisableReason != model.ClientDisabledManual {
		t.Fatalf("forged reason accepted: %#v", row)
	}
	encoded, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("MANUAL_DISABLED")) || bytes.Contains(encoded, []byte("accessDisableReason")) || bytes.Contains(encoded, []byte("access_disable_reason")) {
		t.Fatal("private access authority leaked into the Client wire contract")
	}
	row.Enable = true
	encoded, err = json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Save(SaveRequest{Tx: db, Action: "edit", Data: encoded}); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&row, row.Id).Error; err != nil {
		t.Fatal(err)
	}
	if !row.Enable || row.AccessDisableReason != "" {
		t.Fatal("explicit administrative enable did not clear manual intent")
	}
}
