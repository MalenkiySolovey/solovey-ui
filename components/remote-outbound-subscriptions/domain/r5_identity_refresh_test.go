//go:build !minimal

package remote

import (
	"bytes"
	"encoding/json"
	"errors"
	"gorm.io/gorm"
	"testing"
)

func TestEndpointIRRefreshPreservesIdentityAndBindingsAcrossLabels(t *testing.T) {
	endpoint := func(name, server string) map[string]any {
		return map[string]any{"type": "vless", "tag": name, "server": server, "server_port": 443, "uuid": "00000000-0000-4000-8000-000000000001"}
	}
	for name, payload := range map[string][]map[string]any{
		"rename and reorder":   {endpoint("Renamed B", "b.example.org"), endpoint("Renamed A", "a.example.org")},
		"swap labels":          {endpoint("Node B", "a.example.org"), endpoint("Node A", "b.example.org")},
		"reuse a former label": {endpoint("Node A", "new.example.org"), endpoint("Renamed A", "a.example.org"), endpoint("Renamed B", "b.example.org")},
	} {
		t.Run(name, func(t *testing.T) {
			db := newRemoteSyncDB(t)
			sub := RemoteOutboundSubscription{Name: "Identity source", TagPrefix: "identity-"}
			if err := db.Create(&sub).Error; err != nil {
				t.Fatal(err)
			}
			if _, _, err := RefreshSubscriptionOutbounds(db, &sub, []map[string]any{endpoint("Node A", "a.example.org"), endpoint("Node B", "b.example.org")}, 100); err != nil {
				t.Fatal(err)
			}
			var before []RemoteOutboundConnection
			if err := db.Where("subscription_id = ?", sub.Id).Order("sort_order").Find(&before).Error; err != nil {
				t.Fatal(err)
			}
			if len(before) != 2 {
				t.Fatal("fixture did not create two endpoints")
			}
			before[1].Enabled = false
			if err := db.Save(&before[1]).Error; err != nil {
				t.Fatal(err)
			}
			if err := SyncConnectionToOutbound(db, &before[0], true); err != nil {
				t.Fatal(err)
			}
			if err := db.Save(&before[0]).Error; err != nil {
				t.Fatal(err)
			}
			outboundID, outboundTag := *before[0].OutboundId, before[0].OutboundTag
			result, _, err := RefreshSubscriptionOutbounds(db, &sub, payload, 200)
			if err != nil {
				t.Fatal(err)
			}
			if result.MarkedMissing != 0 || result.Created != len(payload)-2 {
				t.Fatal("a label change recreated an existing semantic endpoint")
			}
			for index, original := range before {
				var after RemoteOutboundConnection
				if err := db.First(&after, original.Id).Error; err != nil {
					t.Fatal("refresh discarded a stable connection ID")
				}
				var beforeOptions, afterOptions map[string]any
				if err := json.Unmarshal(original.Options, &beforeOptions); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(after.Options, &afterOptions); err != nil {
					t.Fatal(err)
				}
				if beforeOptions["server"] != afterOptions["server"] {
					t.Fatal("refresh matched another endpoint through a reused display label")
				}
				if after.Enabled != original.Enabled {
					t.Fatal("a label change reset the user's enabled state")
				}
				if index == 0 && (after.OutboundId == nil || *after.OutboundId != outboundID || after.OutboundTag != outboundTag || !after.Synced) {
					t.Fatal("a label change replaced the managed Outbound binding")
				}
			}
		})
	}
}

func TestEndpointIRRefreshAmbiguityKeepsAcceptedSnapshot(t *testing.T) {
	db := newRemoteSyncDB(t)
	sub := RemoteOutboundSubscription{Name: "Ambiguous source", TagPrefix: "ambiguous-"}
	if err := db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	endpoint := func(name string) map[string]any {
		return map[string]any{"type": "vless", "tag": name, "server": "same.example.org", "server_port": 443, "uuid": "00000000-0000-4000-8000-000000000001"}
	}
	if _, _, err := RefreshSubscriptionOutbounds(db, &sub, []map[string]any{endpoint("A"), endpoint("B")}, 100); err != nil {
		t.Fatal(err)
	}
	var before, after []RemoteOutboundConnection
	if err := db.Order("id").Find(&before).Error; err != nil {
		t.Fatal(err)
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		_, _, err := RefreshSubscriptionOutbounds(tx, &sub, []map[string]any{endpoint("C"), endpoint("D")}, 200)
		return err
	})
	if !errors.Is(err, errRefreshIdentityAmbiguous) {
		t.Fatalf("ambiguous identity was guessed: %v", err)
	}
	if err := db.Order("id").Find(&after).Error; err != nil {
		t.Fatal(err)
	}
	oldJSON, _ := json.Marshal(before)
	newJSON, _ := json.Marshal(after)
	if !bytes.Equal(oldJSON, newJSON) {
		t.Fatal("ambiguous refresh changed the accepted snapshot")
	}
}
