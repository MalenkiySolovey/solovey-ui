package remote

import (
	"encoding/json"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	subcanonical "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/canonical"
)

func TestRefreshSubscriptionOutboundsCreatesConnectionsAndDefaultGroup(t *testing.T) {
	db := newRemoteSyncDB(t)
	sub := model.RemoteOutboundSubscription{Name: "Remote", TagPrefix: "ros-"}
	if err := db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}

	result, coreChanged, err := RefreshSubscriptionOutbounds(db, &sub, []map[string]interface{}{
		{"type": "vless", "tag": "Node A", "server": "a.example", "server_port": float64(443)},
		{"type": "trojan", "tag": "Node B", "server": "b.example", "server_port": float64(443), "password": "secret"},
	}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if coreChanged {
		t.Fatal("new remote-only connections should not require core restart until synced")
	}
	if result.Fetched != 2 || result.Created != 2 || result.Updated != 0 || result.MarkedMissing != 0 {
		t.Fatalf("result = %#v", result)
	}

	var connections []model.RemoteOutboundConnection
	if err := db.Order("sort_order ASC").Find(&connections).Error; err != nil {
		t.Fatal(err)
	}
	if len(connections) != 2 {
		t.Fatalf("connections = %d, want 2", len(connections))
	}
	if connections[0].OutboundTag != "ros-Node-A" || connections[1].OutboundTag != "ros-Node-B" {
		t.Fatalf("outbound tags = %q, %q", connections[0].OutboundTag, connections[1].OutboundTag)
	}
	if len(connections[0].Canonical) == 0 || len(connections[1].Canonical) == 0 {
		t.Fatalf("canonical connection snapshots should be stored: %#v", connections)
	}
	var storedSubscription model.RemoteOutboundSubscription
	if err := db.First(&storedSubscription, sub.Id).Error; err != nil {
		t.Fatal(err)
	}
	var snapshot subcanonical.Snapshot
	if err := json.Unmarshal(storedSubscription.CanonicalSnapshot, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Version != subcanonical.SnapshotVersion || len(snapshot.Connections) != 2 {
		t.Fatalf("canonical snapshot = %#v", snapshot)
	}

	var defaultGroup model.RemoteOutboundGroup
	if err := db.Where("subscription_id = ? AND name = ?", sub.Id, DefaultGroupName).First(&defaultGroup).Error; err != nil {
		t.Fatal(err)
	}
	for _, connection := range connections {
		var count int64
		if err := db.Model(&model.RemoteOutboundGroupConnection{}).
			Where("group_id = ? AND connection_id = ?", defaultGroup.Id, connection.Id).
			Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("connection %d default group links = %d, want 1", connection.Id, count)
		}
	}
}

func TestRefreshFetchedSubscriptionStoresSourceCanonicalConnections(t *testing.T) {
	db := newRemoteSyncDB(t)
	sub := model.RemoteOutboundSubscription{Name: "Remote", TagPrefix: "ros-"}
	if err := db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	fetched, err := ParseFetchedSubscription(`{
  "outbounds": [
    {
      "tag": "proxy-a",
      "protocol": "vless",
      "settings": {
        "vnext": [
          {
            "address": "edge.example.com",
            "port": 443,
            "users": [{"id": "11111111-1111-1111-1111-111111111111"}]
          }
        ]
      },
      "streamSettings": {"network": "tcp"}
    }
  ],
  "routing": {
    "balancers": [
      {"tag": "auto", "selector": ["proxy"], "strategy": {"type": "leastLoad"}}
    ]
  }
}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := RefreshFetchedSubscription(db, &sub, fetched, 100); err != nil {
		t.Fatal(err)
	}
	var storedSubscription model.RemoteOutboundSubscription
	if err := db.First(&storedSubscription, sub.Id).Error; err != nil {
		t.Fatal(err)
	}
	var collection CollectionSnapshot
	if err := json.Unmarshal(storedSubscription.CollectionSnapshot, &collection); err != nil {
		t.Fatal(err)
	}
	if len(collection.Formats) == 0 || collection.Formats[0].Format != subcanonical.FormatXray {
		t.Fatalf("collection snapshot = %#v", collection)
	}
	var connection model.RemoteOutboundConnection
	if err := db.Where("type = ?", "urltest").First(&connection).Error; err != nil {
		t.Fatal(err)
	}
	var canonicalConnection subcanonical.Connection
	if err := json.Unmarshal(connection.Canonical, &canonicalConnection); err != nil {
		t.Fatal(err)
	}
	if len(canonicalConnection.Adaptations) != 1 ||
		canonicalConnection.Adaptations[0].SourceFormat != subcanonical.FormatXray ||
		canonicalConnection.Adaptations[0].SourceFeature != "routing.balancer" ||
		canonicalConnection.Adaptations[0].Strategy != "leastLoad" {
		t.Fatalf("stored connection canonical adaptation = %#v", canonicalConnection.Adaptations)
	}
}

func TestRefreshSubscriptionOutboundsUpdatesAndMarksMissing(t *testing.T) {
	db := newRemoteSyncDB(t)
	sub := model.RemoteOutboundSubscription{Name: "Remote", TagPrefix: "ros-"}
	if err := db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	initial := []map[string]interface{}{
		{"type": "vless", "tag": "Node A", "server": "a.example", "server_port": float64(443)},
		{"type": "trojan", "tag": "Node B", "server": "b.example", "server_port": float64(443), "password": "secret"},
	}
	if _, _, err := RefreshSubscriptionOutbounds(db, &sub, initial, 100); err != nil {
		t.Fatal(err)
	}

	result, coreChanged, err := RefreshSubscriptionOutbounds(db, &sub, []map[string]interface{}{
		{"type": "vless", "tag": "Node A", "server": "a2.example", "server_port": float64(8443)},
	}, 200)
	if err != nil {
		t.Fatal(err)
	}
	if coreChanged {
		t.Fatal("unsynced missing connection should not require core restart")
	}
	if result.Fetched != 1 || result.Created != 0 || result.Updated != 1 || result.MarkedMissing != 1 {
		t.Fatalf("result = %#v", result)
	}

	var nodeA model.RemoteOutboundConnection
	if err := db.Where("source_key = ?", "label:nodea").First(&nodeA).Error; err != nil {
		t.Fatal(err)
	}
	if nodeA.Missing || nodeA.LastSeen != 200 {
		t.Fatalf("node A state = %#v", nodeA)
	}
	var options map[string]any
	if err := json.Unmarshal(nodeA.Options, &options); err != nil {
		t.Fatal(err)
	}
	if options["server"] != "a2.example" {
		t.Fatalf("node A options were not updated: %#v", options)
	}

	var nodeB model.RemoteOutboundConnection
	if err := db.Where("source_key = ?", "label:nodeb").First(&nodeB).Error; err != nil {
		t.Fatal(err)
	}
	if !nodeB.Missing {
		t.Fatalf("node B should be marked missing: %#v", nodeB)
	}
	if nodeB.MissingReason == "" || nodeB.MissingSince != 200 {
		t.Fatalf("node B missing details not recorded: %#v", nodeB)
	}
}

func TestRefreshSubscriptionOutboundsMigratesLegacyTypedLabelSourceKey(t *testing.T) {
	db := newRemoteSyncDB(t)
	sub := model.RemoteOutboundSubscription{Name: "Remote", TagPrefix: "ros-"}
	if err := db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	connection := model.RemoteOutboundConnection{
		SubscriptionId: sub.Id,
		Name:           "Node A",
		SourceKey:      "label:vless:nodea",
		Type:           "vless",
		OutboundTag:    "ros-Node-A",
		Enabled:        true,
		Options:        json.RawMessage(`{"server":"old.example","server_port":443}`),
		CreatedAt:      50,
		UpdatedAt:      50,
	}
	if err := db.Create(&connection).Error; err != nil {
		t.Fatal(err)
	}

	result, _, err := RefreshSubscriptionOutbounds(db, &sub, []map[string]interface{}{
		{"type": "vless", "tag": "Node A", "server": "new.example", "server_port": float64(8443)},
	}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if result.Created != 0 || result.Updated == 0 || result.MarkedMissing != 0 {
		t.Fatalf("refresh result = %#v, want migrated update without duplicate", result)
	}
	var connections []model.RemoteOutboundConnection
	if err := db.Where("subscription_id = ?", sub.Id).Find(&connections).Error; err != nil {
		t.Fatal(err)
	}
	if len(connections) != 1 {
		t.Fatalf("connections = %#v, want one migrated row", connections)
	}
	if connections[0].Id != connection.Id || connections[0].SourceKey != "label:nodea" || connections[0].Missing {
		t.Fatalf("migrated connection = %#v", connections[0])
	}
	var options map[string]any
	if err := json.Unmarshal(connections[0].Options, &options); err != nil {
		t.Fatal(err)
	}
	if options["server"] != "new.example" || options["server_port"] != float64(8443) {
		t.Fatalf("migrated options = %#v", options)
	}
}

func TestRefreshSubscriptionOutboundsHidesOldDuplicateAfterSourceKeyMigration(t *testing.T) {
	db := newRemoteSyncDB(t)
	sub := model.RemoteOutboundSubscription{Name: "Remote", TagPrefix: "ros-"}
	if err := db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	current := model.RemoteOutboundConnection{
		SubscriptionId: sub.Id,
		Name:           "Node A",
		SourceKey:      "label:nodea",
		Type:           "vless",
		OutboundTag:    "ros-Node-A",
		Enabled:        true,
		Options:        json.RawMessage(`{"server":"current.example","server_port":443}`),
		LastSeen:       50,
	}
	legacy := model.RemoteOutboundConnection{
		SubscriptionId: sub.Id,
		Name:           "Node A",
		SourceKey:      "label:vless:nodea",
		Type:           "vless",
		OutboundTag:    "ros-Node-A-old",
		Enabled:        true,
		Options:        json.RawMessage(`{"server":"legacy.example","server_port":443}`),
		LastSeen:       40,
	}
	if err := db.Create(&current).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}

	result, _, err := RefreshSubscriptionOutbounds(db, &sub, []map[string]interface{}{
		{"type": "vless", "tag": "Node A", "server": "fresh.example", "server_port": float64(8443)},
	}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if result.Created != 0 || result.MarkedMissing != 1 {
		t.Fatalf("refresh result = %#v, want old duplicate marked missing", result)
	}
	var stored model.RemoteOutboundSubscription
	if err := db.Preload("Connections").First(&stored, sub.Id).Error; err != nil {
		t.Fatal(err)
	}
	if len(stored.Connections) != 2 {
		t.Fatalf("stored connections = %#v, want current + hidden legacy", stored.Connections)
	}
	subscriptions := []model.RemoteOutboundSubscription{stored}
	FilterVisibleConnections(subscriptions)
	if len(subscriptions[0].Connections) != 1 || subscriptions[0].Connections[0].SourceKey != "label:nodea" {
		t.Fatalf("visible connections = %#v, want only current source key", subscriptions[0].Connections)
	}
}

func TestRefreshSubscriptionOutboundsUpdatesSortOrderFromLatestPayload(t *testing.T) {
	db := newRemoteSyncDB(t)
	sub := model.RemoteOutboundSubscription{Name: "Remote", TagPrefix: "ros-"}
	if err := db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	initial := []map[string]interface{}{
		{"type": "vless", "tag": "Node A", "server": "a.example", "server_port": float64(443)},
		{"type": "trojan", "tag": "Node B", "server": "b.example", "server_port": float64(443), "password": "secret"},
	}
	if _, _, err := RefreshSubscriptionOutbounds(db, &sub, initial, 100); err != nil {
		t.Fatal(err)
	}

	result, _, err := RefreshSubscriptionOutbounds(db, &sub, []map[string]interface{}{
		initial[1],
		initial[0],
	}, 200)
	if err != nil {
		t.Fatal(err)
	}
	if result.Created != 0 || result.Updated != 2 || result.MarkedMissing != 0 {
		t.Fatalf("result = %#v", result)
	}

	var connections []model.RemoteOutboundConnection
	if err := db.Where("subscription_id = ?", sub.Id).Order("sort_order ASC").Find(&connections).Error; err != nil {
		t.Fatal(err)
	}
	if len(connections) != 2 {
		t.Fatalf("connections = %#v", connections)
	}
	if connections[0].Name != "Node B" || connections[0].SortOrder != 1 || connections[1].Name != "Node A" || connections[1].SortOrder != 2 {
		t.Fatalf("connections order = %#v", connections)
	}
}

func TestRefreshSubscriptionOutboundsKeepsSameNameCollisionsFromCurrentPayload(t *testing.T) {
	db := newRemoteSyncDB(t)
	sub := model.RemoteOutboundSubscription{Name: "Remote", TagPrefix: "ros-"}
	if err := db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	outbounds := []map[string]interface{}{
		{
			"type":        "vless",
			"tag":         "Node A",
			"server":      "edge.example",
			"server_port": float64(443),
			"uuid":        "11111111-1111-1111-1111-111111111111",
		},
		{
			"type":        "vless",
			"tag":         "Node B",
			"server":      "edge.example",
			"server_port": float64(443),
			"uuid":        "11111111-1111-1111-1111-111111111111",
		},
	}

	result, _, err := RefreshSubscriptionOutbounds(db, &sub, outbounds, 100)
	if err != nil {
		t.Fatal(err)
	}
	if result.Created != 2 || result.Updated != 0 || result.MarkedMissing != 0 {
		t.Fatalf("result = %#v", result)
	}
	var connections []model.RemoteOutboundConnection
	if err := db.Where("subscription_id = ?", sub.Id).Order("sort_order ASC").Find(&connections).Error; err != nil {
		t.Fatal(err)
	}
	if len(connections) != 2 || connections[0].Name != "Node A" || connections[1].Name != "Node B" {
		t.Fatalf("connections = %#v, want both same-parameter payload rows", connections)
	}
}

func TestRefreshSubscriptionOutboundsTreatsDifferentNormalizedNameAsNewConnection(t *testing.T) {
	db := newRemoteSyncDB(t)
	sub := model.RemoteOutboundSubscription{Name: "Remote", TagPrefix: "ros-"}
	if err := db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	initial := []map[string]interface{}{
		{
			"type":        "vless",
			"tag":         "Node A",
			"server":      "edge.example",
			"server_port": float64(443),
			"uuid":        "11111111-1111-1111-1111-111111111111",
		},
	}
	if _, _, err := RefreshSubscriptionOutbounds(db, &sub, initial, 100); err != nil {
		t.Fatal(err)
	}

	result, _, err := RefreshSubscriptionOutbounds(db, &sub, []map[string]interface{}{
		{
			"type":        "vless",
			"tag":         "Node Renamed",
			"server":      "edge.example",
			"server_port": float64(443),
			"uuid":        "11111111-1111-1111-1111-111111111111",
		},
	}, 200)
	if err != nil {
		t.Fatal(err)
	}
	if result.Created != 1 || result.Updated != 0 || result.MarkedMissing != 1 {
		t.Fatalf("result = %#v", result)
	}
	var connections []model.RemoteOutboundConnection
	if err := db.Order("sort_order ASC").Find(&connections).Error; err != nil {
		t.Fatal(err)
	}
	if len(connections) != 2 {
		t.Fatalf("connections = %d, want old missing + new row: %#v", len(connections), connections)
	}
	if !connections[0].Missing || connections[0].SourceKey != "label:nodea" {
		t.Fatalf("old connection state = %#v", connections[0])
	}
	if connections[1].Missing || connections[1].SourceKey != "label:noderenamed" {
		t.Fatalf("new connection state = %#v", connections[1])
	}
}

func TestRefreshSubscriptionOutboundsTreatsRenamedGroupAsNewConnection(t *testing.T) {
	db := newRemoteSyncDB(t)
	sub := model.RemoteOutboundSubscription{Name: "Remote", TagPrefix: "ros-"}
	if err := db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	initial := []map[string]interface{}{
		{
			"type":        "vless",
			"tag":         "proxy-a",
			"server":      "edge.example",
			"server_port": float64(443),
			"uuid":        "11111111-1111-1111-1111-111111111111",
		},
		{
			"type":      "urltest",
			"tag":       "Auto",
			"outbounds": []string{"proxy-a"},
			"url":       "http://www.gstatic.com/generate_204",
			"interval":  "10m",
		},
	}
	if _, _, err := RefreshSubscriptionOutbounds(db, &sub, initial, 100); err != nil {
		t.Fatal(err)
	}

	result, _, err := RefreshSubscriptionOutbounds(db, &sub, []map[string]interface{}{
		initial[0],
		{
			"type":      "urltest",
			"tag":       "Auto Renamed",
			"outbounds": []string{"proxy-a"},
			"url":       "http://www.gstatic.com/generate_204",
			"interval":  "10m",
		},
	}, 200)
	if err != nil {
		t.Fatal(err)
	}
	if result.Created != 1 || result.Updated != 0 || result.MarkedMissing != 1 {
		t.Fatalf("result = %#v", result)
	}
	var connections []model.RemoteOutboundConnection
	if err := db.Order("sort_order ASC").Find(&connections).Error; err != nil {
		t.Fatal(err)
	}
	if len(connections) != 3 {
		t.Fatalf("connections = %d, want node + old missing group + new group: %#v", len(connections), connections)
	}
	oldGroup := connections[1]
	if oldGroup.Name != "Auto" || oldGroup.SourceKey != "label:auto" || !oldGroup.Missing {
		t.Fatalf("old group state = %#v", oldGroup)
	}
	newGroup := connections[2]
	if newGroup.Name != "Auto Renamed" || newGroup.SourceKey != "label:autorenamed" || newGroup.Missing {
		t.Fatalf("new group state = %#v", newGroup)
	}
}

func TestRefreshSubscriptionOutboundsKeepsOutboundEnabledGroupSynced(t *testing.T) {
	db := newRemoteSyncDB(t)
	sub := model.RemoteOutboundSubscription{Name: "Remote", TagPrefix: "ros-"}
	if err := db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	outbounds := []map[string]interface{}{
		{"type": "vless", "tag": "Node A", "server": "a.example", "server_port": float64(443)},
	}
	if _, _, err := RefreshSubscriptionOutbounds(db, &sub, outbounds, 100); err != nil {
		t.Fatal(err)
	}
	var defaultGroup model.RemoteOutboundGroup
	if err := db.Where("subscription_id = ? AND name = ?", sub.Id, DefaultGroupName).First(&defaultGroup).Error; err != nil {
		t.Fatal(err)
	}
	action, changed, err := ToggleGroupOutbounds(db, defaultGroup.Id, 150)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || action.Added != 1 || !action.OutboundOn {
		t.Fatalf("toggle result=%#v changed=%v, want one added outbound", action, changed)
	}

	result, coreChanged, err := RefreshSubscriptionOutbounds(db, &sub, outbounds, 200)
	if err != nil {
		t.Fatal(err)
	}
	if coreChanged || result.Synced != 1 {
		t.Fatalf("result=%#v coreChanged=%v, want existing sync without core change", result, coreChanged)
	}
	var connection model.RemoteOutboundConnection
	if err := db.Where("subscription_id = ?", sub.Id).First(&connection).Error; err != nil {
		t.Fatal(err)
	}
	if !connection.Synced || connection.OutboundId == nil {
		t.Fatalf("connection was not synced: %#v", connection)
	}
	var outbound model.Outbound
	if err := db.First(&outbound, *connection.OutboundId).Error; err != nil {
		t.Fatal(err)
	}
	if outbound.Tag != connection.OutboundTag {
		t.Fatalf("outbound = %#v, connection = %#v", outbound, connection)
	}
}

func TestRefreshSubscriptionOutboundsUpdatesSyncedOutboundForSameNormalizedName(t *testing.T) {
	db := newRemoteSyncDB(t)
	sub := model.RemoteOutboundSubscription{Name: "Remote", TagPrefix: "ros-"}
	if err := db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	initial := []map[string]interface{}{
		{"type": "vless", "tag": "Node A", "server": "a.example", "server_port": float64(443)},
	}
	if _, _, err := RefreshSubscriptionOutbounds(db, &sub, initial, 100); err != nil {
		t.Fatal(err)
	}
	var defaultGroup model.RemoteOutboundGroup
	if err := db.Where("subscription_id = ? AND name = ?", sub.Id, DefaultGroupName).First(&defaultGroup).Error; err != nil {
		t.Fatal(err)
	}
	if _, _, err := ToggleGroupOutbounds(db, defaultGroup.Id, 150); err != nil {
		t.Fatal(err)
	}

	result, coreChanged, err := RefreshSubscriptionOutbounds(db, &sub, []map[string]interface{}{
		{"type": "vless", "tag": "node a", "server": "a2.example", "server_port": float64(8443)},
	}, 200)
	if err != nil {
		t.Fatal(err)
	}
	if coreChanged || result.Created != 0 || result.Updated != 1 || result.Synced != 1 || result.MarkedMissing != 0 {
		t.Fatalf("refresh result=%#v coreChanged=%v", result, coreChanged)
	}
	var connection model.RemoteOutboundConnection
	if err := db.Where("source_key = ?", "label:nodea").First(&connection).Error; err != nil {
		t.Fatal(err)
	}
	if connection.Name != "node a" || !connection.Synced || connection.OutboundId == nil {
		t.Fatalf("connection was not updated and kept synced: %#v", connection)
	}
	var outbound model.Outbound
	if err := db.First(&outbound, *connection.OutboundId).Error; err != nil {
		t.Fatal(err)
	}
	var options map[string]any
	if err := json.Unmarshal(outbound.Options, &options); err != nil {
		t.Fatal(err)
	}
	if outbound.Type != "vless" || options["server"] != "a2.example" || options["server_port"] != float64(8443) {
		t.Fatalf("synced outbound was not refreshed: outbound=%#v options=%#v", outbound, options)
	}
}

func TestRefreshSubscriptionOutboundsDoesNotOverwriteManuallyChangedOutbound(t *testing.T) {
	db := newRemoteSyncDB(t)
	sub := model.RemoteOutboundSubscription{Name: "Remote", TagPrefix: "ros-"}
	if err := db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	initial := []map[string]interface{}{
		{"type": "vless", "tag": "Node A", "server": "a.example", "server_port": float64(443)},
	}
	if _, _, err := RefreshSubscriptionOutbounds(db, &sub, initial, 100); err != nil {
		t.Fatal(err)
	}
	var defaultGroup model.RemoteOutboundGroup
	if err := db.Where("subscription_id = ? AND name = ?", sub.Id, DefaultGroupName).First(&defaultGroup).Error; err != nil {
		t.Fatal(err)
	}
	if _, _, err := ToggleGroupOutbounds(db, defaultGroup.Id, 150); err != nil {
		t.Fatal(err)
	}
	var connection model.RemoteOutboundConnection
	if err := db.Where("source_key = ?", "label:nodea").First(&connection).Error; err != nil {
		t.Fatal(err)
	}
	manualOptions := json.RawMessage(`{"server":"manual.example","server_port":9443}`)
	if err := db.Model(&model.Outbound{}).Where("id = ?", *connection.OutboundId).Update("options", manualOptions).Error; err != nil {
		t.Fatal(err)
	}

	result, coreChanged, err := RefreshSubscriptionOutbounds(db, &sub, []map[string]interface{}{
		{"type": "vless", "tag": "Node A", "server": "remote.example", "server_port": float64(8443)},
	}, 200)
	if err != nil {
		t.Fatal(err)
	}
	if coreChanged || result.Synced != 0 || result.MarkedMissing != 0 {
		t.Fatalf("refresh result=%#v coreChanged=%v", result, coreChanged)
	}
	if err := db.First(&connection, connection.Id).Error; err != nil {
		t.Fatal(err)
	}
	if connection.Synced || connection.OutboundId != nil {
		t.Fatalf("manual outbound edit should detach remote sync: %#v", connection)
	}
	var outbound model.Outbound
	if err := db.Where("tag = ?", "ros-Node-A").First(&outbound).Error; err != nil {
		t.Fatal(err)
	}
	if !JSONRawEqual(outbound.Options, manualOptions) {
		t.Fatalf("manual outbound options were overwritten: %s", outbound.Options)
	}
	if err := db.First(&defaultGroup, defaultGroup.Id).Error; err != nil {
		t.Fatal(err)
	}
	if defaultGroup.OutboundEnabled {
		t.Fatalf("group should be disabled after manual detach: %#v", defaultGroup)
	}
}

func TestRefreshFetchedXrayBalancerSyncsRootURLTestWithDependencies(t *testing.T) {
	db := newRemoteSyncDB(t)
	sub := model.RemoteOutboundSubscription{Name: "Remote", TagPrefix: "ros-"}
	if err := db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	fetched, err := ParseFetchedSubscription(`{
  "outbounds": [
    {
      "tag": "proxy",
      "protocol": "vless",
      "settings": {"vnext": [{"address": "one.example.com", "port": 443, "users": [{"id": "11111111-1111-1111-1111-111111111111"}]}]},
      "streamSettings": {"network": "tcp", "security": "tls", "tlsSettings": {"serverName": "one.example.com"}}
    },
    {
      "tag": "proxy-2",
      "protocol": "vless",
      "settings": {"vnext": [{"address": "two.example.com", "port": 443, "users": [{"id": "22222222-2222-2222-2222-222222222222"}]}]},
      "streamSettings": {"network": "xhttp", "security": "tls", "tlsSettings": {"serverName": "two.example.com"}, "xhttpSettings": {"path": "/", "host": "edge.example.com", "mode": "auto"}}
    },
    {
      "tag": "proxy-3",
      "protocol": "vless",
      "settings": {"vnext": [{"address": "three.example.com", "port": 443, "users": [{"id": "33333333-3333-3333-3333-333333333333"}]}]},
      "streamSettings": {"network": "grpc", "security": "tls", "tlsSettings": {"serverName": "three.example.com"}, "grpcSettings": {"serviceName": "grpc-service"}}
    },
    {"tag": "direct", "protocol": "freedom"}
  ],
  "routing": {
    "rules": [{"type": "field", "network": "tcp,udp", "balancerTag": "Balancer"}],
    "balancers": [{"tag": "Balancer", "selector": ["proxy"], "fallbackTag": "direct", "strategy": {"type": "leastLoad"}}]
  }
}`)
	if err != nil {
		t.Fatal(err)
	}
	result, coreChanged, err := RefreshFetchedSubscription(db, &sub, fetched, 100)
	if err != nil {
		t.Fatal(err)
	}
	if coreChanged || result.Created != 4 || result.Fetched != 4 {
		t.Fatalf("refresh result=%#v coreChanged=%v", result, coreChanged)
	}

	var groupConnection model.RemoteOutboundConnection
	if err := db.Where("subscription_id = ? AND type = ?", sub.Id, "urltest").First(&groupConnection).Error; err != nil {
		t.Fatal(err)
	}
	if groupConnection.Name != "Balancer" {
		t.Fatalf("group connection = %#v", groupConnection)
	}
	var defaultGroup model.RemoteOutboundGroup
	if err := db.Where("subscription_id = ? AND name = ?", sub.Id, DefaultGroupName).First(&defaultGroup).Error; err != nil {
		t.Fatal(err)
	}
	var defaultLinks []model.RemoteOutboundGroupConnection
	if err := db.Where("group_id = ?", defaultGroup.Id).Find(&defaultLinks).Error; err != nil {
		t.Fatal(err)
	}
	if len(defaultLinks) != 1 || defaultLinks[0].ConnectionId != groupConnection.Id {
		t.Fatalf("default group links = %#v, want only balancer root", defaultLinks)
	}

	action, changed, err := ToggleGroupOutbounds(db, defaultGroup.Id, 150)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || action.Added != 1 || !action.OutboundOn {
		t.Fatalf("toggle result=%#v changed=%v", action, changed)
	}
	var outbounds []model.Outbound
	if err := db.Order("sort_order ASC").Find(&outbounds).Error; err != nil {
		t.Fatal(err)
	}
	if len(outbounds) != 4 {
		t.Fatalf("outbounds = %d, want 3 dependencies + urltest: %#v", len(outbounds), outbounds)
	}
	groupOutbound := outbounds[len(outbounds)-1]
	if groupOutbound.Type != "urltest" || groupOutbound.Tag != groupConnection.OutboundTag {
		t.Fatalf("last outbound should be balancer urltest after its dependencies, got %#v", groupOutbound)
	}
	var options map[string]any
	if err := json.Unmarshal(groupOutbound.Options, &options); err != nil {
		t.Fatal(err)
	}
	refs, _ := options["outbounds"].([]any)
	if len(refs) != 3 || refs[0] != "ros-proxy" || refs[1] != "ros-proxy-2" || refs[2] != "ros-proxy-3" {
		t.Fatalf("urltest refs = %#v options=%#v", refs, options)
	}
	var xhttpDependency model.Outbound
	if err := db.Where("tag = ?", "ros-proxy-2").First(&xhttpDependency).Error; err != nil {
		t.Fatal(err)
	}
	var dependencyOptions map[string]any
	if err := json.Unmarshal(xhttpDependency.Options, &dependencyOptions); err != nil {
		t.Fatal(err)
	}
	transport, _ := dependencyOptions["transport"].(map[string]any)
	if transport["type"] != "httpupgrade" {
		t.Fatalf("xhttp dependency transport = %#v", transport)
	}
}

func TestRefreshMissingSyncedConnectionMarksLinkedOutboundOnly(t *testing.T) {
	db := newRemoteSyncDB(t)
	sub := model.RemoteOutboundSubscription{Name: "Remote", TagPrefix: "ros-"}
	if err := db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	initial := []map[string]interface{}{
		{"type": "vless", "tag": "Node A", "server": "a.example", "server_port": float64(443)},
		{"type": "trojan", "tag": "Node B", "server": "b.example", "server_port": float64(443), "password": "secret"},
	}
	if _, _, err := RefreshSubscriptionOutbounds(db, &sub, initial, 100); err != nil {
		t.Fatal(err)
	}
	var defaultGroup model.RemoteOutboundGroup
	if err := db.Where("subscription_id = ? AND name = ?", sub.Id, DefaultGroupName).First(&defaultGroup).Error; err != nil {
		t.Fatal(err)
	}
	if _, _, err := ToggleGroupOutbounds(db, defaultGroup.Id, 150); err != nil {
		t.Fatal(err)
	}

	result, coreChanged, err := RefreshSubscriptionOutbounds(db, &sub, []map[string]interface{}{
		initial[0],
	}, 200)
	if err != nil {
		t.Fatal(err)
	}
	if !coreChanged || result.MarkedMissing != 1 {
		t.Fatalf("refresh result=%#v coreChanged=%v", result, coreChanged)
	}
	var nodeB model.RemoteOutboundConnection
	if err := db.Where("source_key = ?", "label:nodeb").First(&nodeB).Error; err != nil {
		t.Fatal(err)
	}
	if !nodeB.Missing || !nodeB.Synced || nodeB.OutboundId == nil {
		t.Fatalf("missing connection should stay linked internally: %#v", nodeB)
	}
	var outbound model.Outbound
	if err := db.First(&outbound, *nodeB.OutboundId).Error; err != nil {
		t.Fatal(err)
	}
	if !outbound.RemoteMissing || outbound.RemoteMissingSince != 200 || outbound.RemoteMissingSource != "Remote / Node B" {
		t.Fatalf("linked outbound missing metadata = %#v", outbound)
	}

	if _, _, err := RefreshSubscriptionOutbounds(db, &sub, initial, 300); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&outbound, *nodeB.OutboundId).Error; err != nil {
		t.Fatal(err)
	}
	if outbound.RemoteMissing || outbound.RemoteMissingReason != "" || outbound.RemoteMissingSource != "" {
		t.Fatalf("linked outbound missing metadata was not cleared: %#v", outbound)
	}
}

func TestDueSubscriptions(t *testing.T) {
	db := newRemoteSyncDB(t)
	fixtures := []model.RemoteOutboundSubscription{
		{Name: "due-never-updated", Enabled: true, AutoUpdate: true, UpdateInterval: 3600, LastUpdated: 0},
		{Name: "due-expired", Enabled: true, AutoUpdate: true, UpdateInterval: 3600, LastUpdated: 100},
		{Name: "fresh", Enabled: true, AutoUpdate: true, UpdateInterval: 3600, LastUpdated: 9000},
		{Name: "manual", Enabled: true, AutoUpdate: false, UpdateInterval: 3600, LastUpdated: 0},
		{Name: "disabled", Enabled: false, AutoUpdate: true, UpdateInterval: 3600, LastUpdated: 0},
	}
	if err := db.Create(&fixtures).Error; err != nil {
		t.Fatal(err)
	}
	due, err := DueSubscriptions(db, 4000)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 2 {
		t.Fatalf("due count = %d, want 2: %#v", len(due), due)
	}
	if due[0].Name != "due-never-updated" || due[1].Name != "due-expired" {
		t.Fatalf("due subscriptions = %#v", due)
	}
}
