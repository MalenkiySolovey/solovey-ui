package service

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
	"github.com/MalenkiySolovey/solovey-ui/internal/singbox/diagnostics"
)

func TestDNSStoredProjectionUsesCandidateOutboundWithoutDurableMutation(t *testing.T) {
	settings := initSettingTestDB(t)
	db := dbsqlite.DB()
	old := `{"dns":{"servers":[{"tag":"a","address":"127.0.0.1"}]},"route":{"final":"proxy"}}`
	if err := db.Create(&model.Setting{Key: "config", Value: old}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Outbound{Type: "socks", Tag: "proxy", Options: json.RawMessage(`{"server":"127.0.0.1","server_port":1080}`)}).Error; err != nil {
		t.Fatal(err)
	}
	p, err := NewSingBoxConfigBuilder(nil).BuildCandidateProjectionFromDB(db, "", true)
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]json.RawMessage
	_ = json.Unmarshal(p.Config, &root)
	var dns map[string]json.RawMessage
	_ = json.Unmarshal(root["dns"], &dns)
	if !bytes.Contains(dns["servers"], []byte(`"detour":"proxy"`)) {
		t.Fatal("selected path not frozen")
	}
	stored, err := NewSingBoxBaseConfigStore(settings).Get()
	if err != nil || stored != old {
		t.Fatal("preview mutated durable state")
	}
	if len(p.DNSCompatibility) == 0 {
		t.Fatal("diagnostics missing")
	}
	if err := NewSingBoxBaseConfigStore(settings).Set(old); err == nil {
		t.Fatal("strict save admitted legacy")
	}
}

func TestDNSManualProjectionPreservesSettingAndSharesReason(t *testing.T) {
	settings := initSettingTestDB(t)
	old := `{"dns":{"servers":[{"tag":"a","address":"local"}]}}`
	if err := dbsqlite.DB().Create(&model.Setting{Key: "config", Value: old}).Error; err != nil {
		t.Fatal(err)
	}
	p, err := NewSingBoxConfigBuilder(nil).BuildCandidateProjectionFromDB(dbsqlite.DB(), "", true)
	if err == nil || diagnostics.FirstError(p.DNSCompatibility) == nil {
		t.Fatal("manual projection admitted")
	}
	stored, _ := NewSingBoxBaseConfigStore(settings).Get()
	if stored != old {
		t.Fatal("preimage lost")
	}
	if err := NewSingBoxBaseConfigStore(settings).Set(`{"dns":{"servers":[{"type":"local","tag":"a"}]}}`); err != nil {
		t.Fatal("corrected retry failed")
	}
}

func TestDNSOtherOwnerFailureDoesNotReturnPartialUpgrade(t *testing.T) {
	initSettingTestDB(t)
	db := dbsqlite.DB()
	old := `{"dns":{"servers":[{"tag":"a","address":"127.0.0.1"}]}}`
	if err := db.Create(&model.Setting{Key: "config", Value: old}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Inbound{Type: "tun", Tag: "old-tun", Options: json.RawMessage(`{"address":["172.19.0.1/30"]}`)}).Error; err != nil {
		t.Fatal(err)
	}
	p, err := NewSingBoxConfigBuilder(nil).BuildCandidateProjectionFromDB(db, "", true)
	var candidate struct {
		DNS struct{ Servers []struct{ Address string } }
	}
	decodeErr := json.Unmarshal(p.Config, &candidate)
	if err == nil || decodeErr != nil || len(candidate.DNS.Servers) != 1 || candidate.DNS.Servers[0].Address != "127.0.0.1" {
		t.Fatal("blocked candidate exposed partial DNS upgrade")
	}
	var row model.Setting
	if err := db.Where("key = ?", "config").Take(&row).Error; err != nil || row.Value != old {
		t.Fatal("blocked candidate changed durable source")
	}
}
