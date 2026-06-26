package remote

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	subcanonical "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/canonical"
	subconversion "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/conversion"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newRemoteSyncDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.RemoteOutboundSubscription{},
		&model.RemoteOutboundGroup{},
		&model.RemoteOutboundGroupConnection{},
		&model.RemoteOutboundConnection{},
		&model.Outbound{},
		&model.Endpoint{},
		&model.Service{},
		&model.Setting{},
	); err != nil {
		if strings.Contains(err.Error(), "go-sqlite3 requires cgo") {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	return db
}

func TestConnectionOutboundConfig(t *testing.T) {
	config, err := ConnectionOutboundConfig(model.RemoteOutboundConnection{
		Type:        "vless",
		OutboundTag: "ros-node",
		Options:     json.RawMessage(`{"server":"example.com","server_port":443}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(config, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["type"] != "vless" || decoded["tag"] != "ros-node" {
		t.Fatalf("decoded outbound = %#v", decoded)
	}
}

func TestFilterVisibleConnectionsHidesTechnicalMembers(t *testing.T) {
	canonicalMember, err := json.Marshal(subcanonical.Connection{
		Role:        subcanonical.RoleMember,
		DisplayName: "proxy",
		Protocol:    "vless",
	})
	if err != nil {
		t.Fatal(err)
	}
	subscriptions := []model.RemoteOutboundSubscription{{
		Connections: []model.RemoteOutboundConnection{
			{
				Id:        1,
				Name:      "auto",
				Type:      "urltest",
				Options:   json.RawMessage(`{"outbounds":["renamed-proxy"]}`),
				LastSeen:  100,
				Canonical: json.RawMessage(`{"kind":"group","role":"top"}`),
			},
			{
				Id:            2,
				Name:          "proxy",
				Type:          "vless",
				Missing:       true,
				MissingReason: "old state",
				MissingSince:  90,
				Canonical:     canonicalMember,
			},
		},
	}}

	FilterVisibleConnections(subscriptions)

	if len(subscriptions[0].Connections) != 1 {
		t.Fatalf("visible connections = %#v, want only top-level group", subscriptions[0].Connections)
	}
	visible := subscriptions[0].Connections[0]
	if visible.Name != "auto" || visible.Missing {
		t.Fatalf("visible group presentation state = %#v", visible)
	}
}

func TestFilterVisibleConnectionsHidesMissingTopLevelConnections(t *testing.T) {
	subscriptions := []model.RemoteOutboundSubscription{{
		Connections: []model.RemoteOutboundConnection{
			{
				Id:       1,
				Name:     "old node",
				Type:     "vless",
				Missing:  true,
				LastSeen: 100,
			},
			{
				Id:       2,
				Name:     "current node",
				Type:     "vless",
				LastSeen: 200,
			},
		},
	}}

	FilterVisibleConnections(subscriptions)

	if len(subscriptions[0].Connections) != 1 || subscriptions[0].Connections[0].Name != "current node" {
		t.Fatalf("visible connections = %#v, want only current node", subscriptions[0].Connections)
	}
}

func TestFilterVisibleConnectionsKeepsReferencedTopLevelConnections(t *testing.T) {
	topLevelSingle, err := json.Marshal(subcanonical.Connection{
		Role:        subcanonical.RoleTopLevel,
		DisplayName: "proxy",
		Protocol:    "vless",
	})
	if err != nil {
		t.Fatal(err)
	}
	subscriptions := []model.RemoteOutboundSubscription{{
		Connections: []model.RemoteOutboundConnection{
			{
				Id:        1,
				Name:      "auto",
				Type:      "urltest",
				Options:   json.RawMessage(`{"outbounds":["proxy"]}`),
				Canonical: json.RawMessage(`{"kind":"group","role":"top"}`),
			},
			{
				Id:        2,
				Name:      "proxy",
				Type:      "vless",
				Canonical: topLevelSingle,
			},
		},
	}}

	FilterVisibleConnections(subscriptions)

	if len(subscriptions[0].Connections) != 2 {
		t.Fatalf("visible connections = %#v, want group and top-level referenced single", subscriptions[0].Connections)
	}
}

func TestConnectionMatchesMemberRefNormalizesLabelWhitespace(t *testing.T) {
	connection := model.RemoteOutboundConnection{
		Name:        "Auto | Балансер Обход-1 / proxy",
		SourceKey:   "label:auto|балансеробход-1/proxy",
		OutboundTag: "ros-Auto-Балансер-Обход-1-proxy",
	}
	if !connectionMatchesMemberRef(connection, "auto|балансеробход-1/proxy") {
		t.Fatalf("member ref should match normalized connection name")
	}
	if connectionMatchesMemberRef(connection, "auto|балансеробход-2/proxy") {
		t.Fatalf("different normalized member ref should not match")
	}
}

func TestConnectionOutboundConfigRewritesGroupRefsWithTagMap(t *testing.T) {
	config, err := connectionOutboundConfig(model.RemoteOutboundConnection{
		Type:        "urltest",
		OutboundTag: "ros-auto",
		Options:     json.RawMessage(`{"outbounds":["proxy-a"],"default":"proxy-a"}`),
	}, map[string]string{"proxy-a": "ros-proxy-a"})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(config, &decoded); err != nil {
		t.Fatal(err)
	}
	refs, _ := decoded["outbounds"].([]any)
	if len(refs) != 1 || refs[0] != "ros-proxy-a" || decoded["default"] != "ros-proxy-a" {
		t.Fatalf("decoded group = %#v", decoded)
	}
}

func TestConnectionOutboundConfigDropsFingerprintOnlyDisabledTLS(t *testing.T) {
	config, err := ConnectionOutboundConfig(model.RemoteOutboundConnection{
		Type:        "vless",
		OutboundTag: "ros-node",
		Options:     json.RawMessage(`{"server":"edge.example.com","server_port":443,"tls":{"utls":{"enabled":true,"fingerprint":"chrome"}}}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(config, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["tls"] != nil {
		t.Fatalf("fingerprint-only stored tls should be removed before runtime use: %#v", decoded["tls"])
	}
}

func TestConnectionOutboundConfigEnablesTLSForStoredReality(t *testing.T) {
	config, err := ConnectionOutboundConfig(model.RemoteOutboundConnection{
		Type:        "vless",
		OutboundTag: "ros-node",
		Options:     json.RawMessage(`{"server":"edge.example.com","server_port":443,"tls":{"server_name":"sni.example.com","reality":{"enabled":true,"public_key":"key"},"utls":{"enabled":true,"fingerprint":"chrome"}}}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(config, &decoded); err != nil {
		t.Fatal(err)
	}
	tls, _ := decoded["tls"].(map[string]any)
	if tls["enabled"] != true || tls["utls"] == nil || tls["reality"] == nil {
		t.Fatalf("stored reality tls should be explicitly enabled: %#v", tls)
	}
}

func TestApplyClientConversionUsesTargetPolicy(t *testing.T) {
	connection := model.RemoteOutboundConnection{
		Type:      "urltest",
		Canonical: json.RawMessage(`{"adaptations":[{"sourceFormat":"xray-json","sourceFeature":"routing.balancer","sourceType":"balancer","targetType":"urltest","strategy":"leastLoad"}]}`),
	}
	policy := subconversion.DefaultPolicy()
	policy.Client.SingBox[subconversion.FeatureXrayBalancer] = subconversion.ModeSelector

	converted := applyClientConversion(connection, map[string]interface{}{
		"type":      "urltest",
		"tag":       "auto",
		"outbounds": []interface{}{"proxy-a"},
		"url":       "http://www.gstatic.com/generate_204",
		"interval":  "10m",
		"tolerance": 50,
	}, ClientConversionOptions{Target: subconversion.TargetSingBox, Policy: policy})
	if converted["type"] != "selector" || converted["url"] != nil {
		t.Fatalf("sing-box conversion = %#v", converted)
	}

	original := applyClientConversion(connection, map[string]interface{}{
		"type":      "urltest",
		"tag":       "auto",
		"outbounds": []interface{}{"proxy-a"},
		"url":       "http://www.gstatic.com/generate_204",
	}, ClientConversionOptions{Target: subconversion.TargetXray, Policy: subconversion.DefaultPolicy()})
	if original["type"] != "urltest" || original[subcanonical.MetadataKey] == nil {
		t.Fatalf("xray original conversion = %#v", original)
	}
}

func TestApplyClientConversionUsesXrayTargetBalancerMode(t *testing.T) {
	connection := model.RemoteOutboundConnection{
		Type:      "urltest",
		Canonical: json.RawMessage(`{"adaptations":[{"sourceFormat":"clash-yaml","sourceFeature":"proxy-groups","sourceType":"fallback","targetType":"urltest"}]}`),
	}
	policy := subconversion.DefaultPolicy()
	converted := applyClientConversion(connection, map[string]interface{}{
		"type":      "urltest",
		"tag":       "fallback",
		"outbounds": []interface{}{"proxy-a", "proxy-b"},
		"url":       "http://www.gstatic.com/generate_204",
		"interval":  "10m",
	}, ClientConversionOptions{Target: subconversion.TargetXray, Policy: policy})
	if converted["type"] != "selector" || converted["url"] != nil {
		t.Fatalf("xray client conversion = %#v", converted)
	}
	metadata, ok := converted[subcanonical.MetadataKey].([]subcanonical.Adaptation)
	if !ok || len(metadata) < 2 || metadata[len(metadata)-1].TargetType != subconversion.ModeXrayBalancer {
		t.Fatalf("xray conversion metadata = %#v", converted[subcanonical.MetadataKey])
	}
}

func TestApplyClientConversionUsesMihomoTargetGroupMode(t *testing.T) {
	connection := model.RemoteOutboundConnection{
		Type:      "urltest",
		Canonical: json.RawMessage(`{"adaptations":[{"sourceFormat":"xray-json","sourceFeature":"routing.balancer","sourceType":"balancer","targetType":"urltest","strategy":"random"}]}`),
	}
	policy := subconversion.DefaultPolicy()
	policy.Client.Mihomo[subconversion.FeatureXrayBalancer] = subconversion.ModeMihomoLoadBalance
	converted := applyClientConversion(connection, map[string]interface{}{
		"type":      "urltest",
		"tag":       "auto",
		"outbounds": []interface{}{"proxy-a", "proxy-b"},
		"url":       "http://www.gstatic.com/generate_204",
		"interval":  "10m",
	}, ClientConversionOptions{Target: subconversion.TargetMihomo, Policy: policy})
	if converted["type"] != "urltest" || converted[subcanonical.MetadataKey] == nil {
		t.Fatalf("mihomo client conversion = %#v", converted)
	}
	metadata, ok := converted[subcanonical.MetadataKey].([]subcanonical.Adaptation)
	if !ok || len(metadata) < 2 || metadata[len(metadata)-1].TargetType != subconversion.ModeMihomoLoadBalance {
		t.Fatalf("mihomo conversion metadata = %#v", converted[subcanonical.MetadataKey])
	}
}

func TestApplyClientConversionRestoresMihomoNativeGroupMetadata(t *testing.T) {
	connection := model.RemoteOutboundConnection{
		Type: "urltest",
		Canonical: json.RawMessage(`{
			"bestOutbound":{
				"type":"urltest",
				"tag":"Auto",
				"mihomo_group":{
					"type":"url-test",
					"url":"https://cp.cloudflare.com/generate_204",
					"lazy":true,
					"timeout":3000
				}
			},
			"adaptations":[{
				"sourceFormat":"clash-yaml",
				"sourceFeature":"proxy-groups",
				"sourceType":"url-test",
				"targetType":"urltest"
			}]
		}`),
	}
	converted := applyClientConversion(connection, map[string]interface{}{
		"type":      "urltest",
		"tag":       "Auto",
		"outbounds": []interface{}{"proxy-a"},
	}, ClientConversionOptions{Target: subconversion.TargetMihomo, Policy: subconversion.DefaultPolicy()})
	metadata, ok := converted["mihomo_group"].(map[string]interface{})
	if !ok || metadata["url"] != "https://cp.cloudflare.com/generate_204" || metadata["lazy"] != true || metadata["timeout"] != float64(3000) {
		t.Fatalf("mihomo native metadata = %#v", converted["mihomo_group"])
	}
}

func TestOutboundsForClientLinksAddsMihomoSnapshotExtrasOnlyForMihomo(t *testing.T) {
	db := newRemoteSyncDB(t)
	snapshot := subcanonical.Snapshot{
		Version: subcanonical.SnapshotVersion,
		Extras: []subcanonical.Observation{{
			Format: subcanonical.FormatClash,
			Name:   "Auto",
			Outbound: map[string]any{
				"type":      "selector",
				"tag":       "Auto",
				"outbounds": []any{"Node A"},
				subcanonical.MetadataKey: map[string]any{
					"source_format":  subcanonical.FormatClash,
					"source_feature": "proxy-groups",
					"source_type":    "select",
					"target_type":    "selector",
				},
			},
		}},
	}
	snapshotData, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	subscription := model.RemoteOutboundSubscription{
		Name:              "Remote",
		Enabled:           true,
		CanonicalSnapshot: snapshotData,
	}
	if err := db.Create(&subscription).Error; err != nil {
		t.Fatal(err)
	}
	group := model.RemoteOutboundGroup{
		SubscriptionId: subscription.Id,
		Name:           "Default",
		Enabled:        true,
	}
	if err := db.Create(&group).Error; err != nil {
		t.Fatal(err)
	}
	connection := model.RemoteOutboundConnection{
		SubscriptionId: subscription.Id,
		GroupId:        group.Id,
		Name:           "Node A",
		SourceKey:      "label:nodea",
		Type:           "vless",
		OutboundTag:    "ros-node-a",
		Enabled:        true,
		Options:        json.RawMessage(`{"server":"edge.example.com","server_port":443}`),
	}
	if err := db.Create(&connection).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.RemoteOutboundGroupConnection{GroupId: group.Id, ConnectionId: connection.Id}).Error; err != nil {
		t.Fatal(err)
	}
	rawLinks := json.RawMessage(`[{"type":"remoteGroup","groupId":` + fmt.Sprint(group.Id) + `}]`)
	mihomoOutbounds, _, err := OutboundsForClientLinksWithOptions(db, rawLinks, ClientConversionOptions{
		Target: subconversion.TargetMihomo,
		Policy: subconversion.DefaultPolicy(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(mihomoOutbounds) != 2 {
		t.Fatalf("mihomo outbounds = %#v, want proxy + extra group", mihomoOutbounds)
	}
	refs := stringList(mihomoOutbounds[1]["outbounds"])
	if mihomoOutbounds[1]["tag"] != "Auto" || len(refs) != 1 || refs[0] != "ros-node-a" {
		t.Fatalf("extra group was not rewritten for mihomo client: %#v", mihomoOutbounds[1])
	}
	singBoxOutbounds, _, err := OutboundsForClientLinksWithOptions(db, rawLinks, ClientConversionOptions{
		Target: subconversion.TargetSingBox,
		Policy: subconversion.DefaultPolicy(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(singBoxOutbounds) != 1 {
		t.Fatalf("sing-box outbounds = %#v, want only selected proxy", singBoxOutbounds)
	}
}

func TestOutboundsForClientLinksSupportsRemoteSubscriptionAll(t *testing.T) {
	db := newRemoteSyncDB(t)
	subscription := model.RemoteOutboundSubscription{
		Name:    "Remote",
		Enabled: true,
	}
	if err := db.Create(&subscription).Error; err != nil {
		t.Fatal(err)
	}
	group := model.RemoteOutboundGroup{
		SubscriptionId: subscription.Id,
		Name:           "Hidden from All",
		Enabled:        false,
	}
	if err := db.Create(&group).Error; err != nil {
		t.Fatal(err)
	}
	connections := []model.RemoteOutboundConnection{
		{
			SubscriptionId: subscription.Id,
			GroupId:        group.Id,
			Name:           "Node A",
			SourceKey:      "node-a",
			Type:           "vless",
			OutboundTag:    "ros-node-a",
			Enabled:        true,
			SortOrder:      1,
			Options:        json.RawMessage(`{"server":"a.example.com","server_port":443}`),
		},
		{
			SubscriptionId: subscription.Id,
			Name:           "Node B",
			SourceKey:      "node-b",
			Type:           "trojan",
			OutboundTag:    "ros-node-b",
			Enabled:        true,
			SortOrder:      2,
			Options:        json.RawMessage(`{"server":"b.example.com","server_port":443,"password":"secret"}`),
		},
		{
			SubscriptionId: subscription.Id,
			Name:           "Disabled",
			SourceKey:      "disabled",
			Type:           "vless",
			OutboundTag:    "ros-disabled",
			Enabled:        false,
			SortOrder:      3,
			Options:        json.RawMessage(`{"server":"disabled.example.com","server_port":443}`),
		},
	}
	if err := db.Create(&connections).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.RemoteOutboundGroupConnection{GroupId: group.Id, ConnectionId: connections[0].Id}).Error; err != nil {
		t.Fatal(err)
	}

	rawLinks := json.RawMessage(`[{"type":"remoteSubscription","subscriptionId":` + fmt.Sprint(subscription.Id) + `}]`)
	outbounds, tags, err := OutboundsForClientLinks(db, rawLinks)
	if err != nil {
		t.Fatal(err)
	}
	if len(outbounds) != 2 || strings.Join(tags, ",") != "ros-node-a,ros-node-b" {
		t.Fatalf("remote subscription all outbounds=%#v tags=%v", outbounds, tags)
	}
}

func TestSyncConnectionToOutboundCreatesOutbound(t *testing.T) {
	db := newRemoteSyncDB(t)
	connection := model.RemoteOutboundConnection{
		Name:        "node",
		SourceKey:   "node",
		Type:        "vless",
		OutboundTag: "ros-node",
		Enabled:     true,
		Options:     json.RawMessage(`{"server":"example.com","server_port":443}`),
	}
	if err := db.Create(&connection).Error; err != nil {
		t.Fatal(err)
	}
	if err := SyncConnectionToOutbound(db, &connection, true); err != nil {
		t.Fatal(err)
	}
	if !connection.Synced || connection.OutboundId == nil {
		t.Fatalf("connection was not linked: %#v", connection)
	}
	var outbound model.Outbound
	if err := db.First(&outbound, *connection.OutboundId).Error; err != nil {
		t.Fatal(err)
	}
	if outbound.Tag != connection.OutboundTag || outbound.Type != connection.Type {
		t.Fatalf("outbound = %#v", outbound)
	}
}

func TestSyncConnectionToOutboundRewritesGroupRefs(t *testing.T) {
	db := newRemoteSyncDB(t)
	sub := model.RemoteOutboundSubscription{Name: "Remote"}
	if err := db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	node := model.RemoteOutboundConnection{
		SubscriptionId: sub.Id,
		Name:           "proxy-a",
		SourceKey:      "label:proxy-a",
		Type:           "vless",
		OutboundTag:    "ros-proxy-a",
		Enabled:        true,
		Options:        json.RawMessage(`{"server":"example.com","server_port":443}`),
	}
	group := model.RemoteOutboundConnection{
		SubscriptionId: sub.Id,
		Name:           "auto",
		SourceKey:      "label:auto",
		Type:           "urltest",
		OutboundTag:    "ros-auto",
		Enabled:        true,
		Options:        json.RawMessage(`{"outbounds":["proxy-a"],"default":"proxy-a","url":"http://www.gstatic.com/generate_204","interval":"10m","tolerance":50}`),
	}
	if err := db.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&group).Error; err != nil {
		t.Fatal(err)
	}
	if err := SyncConnectionToOutbound(db, &group, true); err != nil {
		t.Fatal(err)
	}
	var outbound model.Outbound
	if err := db.First(&outbound, *group.OutboundId).Error; err != nil {
		t.Fatal(err)
	}
	var options map[string]any
	if err := json.Unmarshal(outbound.Options, &options); err != nil {
		t.Fatal(err)
	}
	refs, _ := options["outbounds"].([]any)
	if len(refs) != 1 || refs[0] != "ros-proxy-a" || options["default"] != "ros-proxy-a" {
		t.Fatalf("rewritten group options = %#v", options)
	}
	var storedNode model.RemoteOutboundConnection
	if err := db.First(&storedNode, node.Id).Error; err != nil {
		t.Fatal(err)
	}
	if !storedNode.Synced || storedNode.OutboundId == nil {
		t.Fatalf("group dependency was not synced: %#v", storedNode)
	}
}

func TestUnsyncConnectionFromOutboundBlocksReferencedSelector(t *testing.T) {
	db := newRemoteSyncDB(t)
	outbound := model.Outbound{Tag: "ros-node", Type: "vless", Options: json.RawMessage(`{"server":"example.com","server_port":443}`)}
	if err := db.Create(&outbound).Error; err != nil {
		t.Fatal(err)
	}
	selector := model.Outbound{Tag: "selector", Type: "selector", Options: json.RawMessage(`{"outbounds":["ros-node"],"default":"ros-node"}`)}
	if err := db.Create(&selector).Error; err != nil {
		t.Fatal(err)
	}
	connection := model.RemoteOutboundConnection{
		Name:        "node",
		SourceKey:   "node",
		Type:        outbound.Type,
		OutboundTag: outbound.Tag,
		Synced:      true,
		OutboundId:  &outbound.Id,
		Options:     outbound.Options,
	}
	if err := db.Create(&connection).Error; err != nil {
		t.Fatal(err)
	}
	err := UnsyncConnectionFromOutbound(db, &connection)
	if err == nil || !strings.Contains(err.Error(), "still referenced") {
		t.Fatalf("referenced unsync should be blocked, got %v", err)
	}
}

func TestDeleteSyncedOutboundsForSubscriptionUsesReferenceGuard(t *testing.T) {
	db := newRemoteSyncDB(t)
	sub := model.RemoteOutboundSubscription{Name: "Remote"}
	if err := db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	outbound := model.Outbound{Tag: "ros-node", Type: "vless", Options: json.RawMessage(`{"server":"example.com","server_port":443}`)}
	if err := db.Create(&outbound).Error; err != nil {
		t.Fatal(err)
	}
	selector := model.Outbound{Tag: "selector", Type: "selector", Options: json.RawMessage(`{"outbounds":["ros-node"]}`)}
	if err := db.Create(&selector).Error; err != nil {
		t.Fatal(err)
	}
	connection := model.RemoteOutboundConnection{
		SubscriptionId: sub.Id,
		Name:           "node",
		SourceKey:      "node",
		Type:           outbound.Type,
		OutboundTag:    outbound.Tag,
		Synced:         true,
		OutboundId:     &outbound.Id,
		Options:        outbound.Options,
	}
	if err := db.Create(&connection).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := DeleteSyncedOutboundsForSubscription(db, sub.Id); err == nil || !strings.Contains(err.Error(), "still referenced") {
		t.Fatalf("referenced subscription delete should be blocked, got %v", err)
	}
}

func TestDeleteSyncedOutboundsForSubscriptionDeletesLinkedOutbound(t *testing.T) {
	db := newRemoteSyncDB(t)
	sub := model.RemoteOutboundSubscription{Name: "Remote"}
	if err := db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	outbound := model.Outbound{Tag: "ros-node", Type: "vless", Options: json.RawMessage(`{"server":"example.com","server_port":443}`)}
	if err := db.Create(&outbound).Error; err != nil {
		t.Fatal(err)
	}
	connection := model.RemoteOutboundConnection{
		SubscriptionId: sub.Id,
		Name:           "node",
		SourceKey:      "node",
		Type:           outbound.Type,
		OutboundTag:    outbound.Tag,
		Synced:         true,
		OutboundId:     &outbound.Id,
		Options:        outbound.Options,
	}
	if err := db.Create(&connection).Error; err != nil {
		t.Fatal(err)
	}
	changed, err := DeleteSyncedOutboundsForSubscription(db, sub.Id)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected linked outbound delete to report changed")
	}
	var count int64
	if err := db.Model(&model.Outbound{}).Where("id = ?", outbound.Id).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("linked outbound was not deleted, count=%d", count)
	}
}

func TestReconcileOutboundLinksClearsMissingOutbound(t *testing.T) {
	db := newRemoteSyncDB(t)
	missingID := uint(999)
	connection := model.RemoteOutboundConnection{
		Name:        "node",
		SourceKey:   "node",
		Type:        "vless",
		OutboundTag: "ros-node",
		Synced:      true,
		OutboundId:  &missingID,
	}
	if err := db.Create(&connection).Error; err != nil {
		t.Fatal(err)
	}
	changed, err := ReconcileOutboundLinks(db)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("missing outbound should be reported as changed")
	}
	var stored model.RemoteOutboundConnection
	if err := db.First(&stored, connection.Id).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Synced || stored.OutboundId != nil {
		t.Fatalf("missing outbound link was not cleared: %#v", stored)
	}
}

func TestUniqueOutboundTagAvoidsExistingConnectionTag(t *testing.T) {
	db := newRemoteSyncDB(t)
	sub := model.RemoteOutboundSubscription{Name: "Remote", TagPrefix: "ros-"}
	if err := db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	existing := model.RemoteOutboundConnection{
		SubscriptionId: sub.Id,
		Name:           "Node",
		SourceKey:      "node",
		OutboundTag:    "ros-Node",
	}
	if err := db.Create(&existing).Error; err != nil {
		t.Fatal(err)
	}
	tag, err := UniqueOutboundTag(db, sub, "Node", 0)
	if err != nil {
		t.Fatal(err)
	}
	if tag != "ros-Node-2" {
		t.Fatalf("tag = %q, want ros-Node-2", tag)
	}
}
