//go:build !minimal

package remote

import (
	"encoding/json"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	"gorm.io/gorm"
)

func TestEndpointIRRefreshReportsRuntimeChangeWithoutRestartingForLabels(t *testing.T) {
	db := newRemoteSyncDB(t)
	sub := RemoteOutboundSubscription{Name: "Runtime projection", TagPrefix: "runtime-"}
	if err := db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	payload := func(name, server string) []map[string]any {
		return []map[string]any{{"type": "vless", "tag": name, "server": server, "server_port": 443, "uuid": "00000000-0000-4000-8000-000000000001"}}
	}
	if _, _, err := RefreshSubscriptionOutbounds(db, &sub, payload("Original", "first.example.org"), 100); err != nil {
		t.Fatal(err)
	}
	var connection RemoteOutboundConnection
	if err := db.Where("subscription_id = ?", sub.Id).First(&connection).Error; err != nil {
		t.Fatal(err)
	}
	if err := SyncConnectionToOutbound(db, &connection, true); err != nil {
		t.Fatal(err)
	}
	if err := db.Save(&connection).Error; err != nil {
		t.Fatal(err)
	}
	outboundID := *connection.OutboundId
	for index, step := range []struct {
		name, server string
		changed      bool
	}{
		{"Original", "second.example.org", true},
		{"Renamed", "second.example.org", false},
		{"Renamed", "second.example.org", false},
	} {
		_, changed, err := RefreshSubscriptionOutbounds(db, &sub, payload(step.name, step.server), int64(200+index))
		if err != nil {
			t.Fatal(err)
		}
		if changed != step.changed {
			t.Fatalf("step %d runtime change=%v, want %v", index, changed, step.changed)
		}
		var after RemoteOutboundConnection
		if err := db.First(&after, connection.Id).Error; err != nil {
			t.Fatal(err)
		}
		if after.OutboundId == nil || *after.OutboundId != outboundID {
			t.Fatal("runtime update replaced the managed binding")
		}
	}
	var outbound model.Outbound
	if err := db.First(&outbound, outboundID).Error; err != nil {
		t.Fatal(err)
	}
	var options map[string]any
	if err := json.Unmarshal(outbound.Options, &options); err != nil {
		t.Fatal(err)
	}
	if options["server"] != "second.example.org" {
		t.Fatal("canonical Outbound did not receive the runtime projection")
	}
}

func TestEndpointIRMaterializationUsesCanonicalOutboundNamespace(t *testing.T) {
	db := newRemoteSyncDB(t)
	if err := db.Create(&model.Endpoint{Type: "wireguard", Tag: "managed-collision", Options: json.RawMessage(`{}`)}).Error; err != nil {
		t.Fatal(err)
	}
	connection := RemoteOutboundConnection{Type: "vless", OutboundTag: "managed-collision", Enabled: true,
		Options: json.RawMessage(`{"server":"example.org","server_port":443,"uuid":"00000000-0000-4000-8000-000000000001"}`)}
	err := db.Transaction(func(tx *gorm.DB) error { return SyncConnectionToOutbound(tx, &connection, true) })
	if err == nil {
		t.Fatal("external materialization bypassed the canonical cross-table tag owner")
	}
	var count int64
	if err := db.Model(&model.Outbound{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("rejected materialization wrote an Outbound")
	}
	if connection.Synced || connection.OutboundId != nil {
		t.Fatal("rejected materialization claimed a successful binding")
	}
}

func TestEndpointIRMaterializationKeepsCanonicalIdentityOnRefresh(t *testing.T) {
	db := newRemoteSyncDB(t)
	connection := RemoteOutboundConnection{Type: "vless", OutboundTag: "managed-refresh", Enabled: true,
		Options: json.RawMessage(`{"server":"example.org","server_port":443,"uuid":"00000000-0000-4000-8000-000000000001"}`)}
	if err := db.Transaction(func(tx *gorm.DB) error { return SyncConnectionToOutbound(tx, &connection, true) }); err != nil {
		t.Fatal(err)
	}
	var before model.Outbound
	if err := db.First(&before, *connection.OutboundId).Error; err != nil {
		t.Fatal(err)
	}
	connection.Options = json.RawMessage(`{"server":"updated.example.org","server_port":8443,"uuid":"00000000-0000-4000-8000-000000000001"}`)
	if err := db.Transaction(func(tx *gorm.DB) error { return SyncConnectionToOutbound(tx, &connection, true) }); err != nil {
		t.Fatal(err)
	}
	var after model.Outbound
	if err := db.First(&after, *connection.OutboundId).Error; err != nil {
		t.Fatal(err)
	}
	if before.Id != after.Id || before.SortOrder != after.SortOrder || !connection.Synced {
		t.Fatal("refresh replaced the canonical identity or order")
	}
	var options map[string]any
	if err := json.Unmarshal(after.Options, &options); err != nil {
		t.Fatal(err)
	}
	if options["server"] != "updated.example.org" || options["server_port"] != float64(8443) {
		t.Fatal("refresh did not save the accepted candidate")
	}
}
