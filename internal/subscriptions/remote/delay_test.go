package remote

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
)

func TestCheckConnectionRecordsSkipsDisabledButAllowsMissingConnections(t *testing.T) {
	results := CheckConnectionRecords(context.Background(), []model.RemoteOutboundConnection{
		{Id: 1, OutboundTag: "disabled", Enabled: false},
		{
			Id:          2,
			OutboundTag: "missing",
			Enabled:     true,
			Missing:     true,
		},
	}, "https://example.com")

	if len(results) != 2 {
		t.Fatalf("results length = %d, want 2", len(results))
	}
	if !results[0].Skipped || results[0].Error != "connection is disabled" {
		t.Fatalf("disabled result = %#v", results[0])
	}
	if results[1].Skipped {
		t.Fatalf("missing result = %#v", results[1])
	}
}

func TestCheckSubscriptionOrdersBySortOrder(t *testing.T) {
	db := newRemoteSyncDB(t)
	sub := model.RemoteOutboundSubscription{Name: "Remote", Url: "https://example.com/sub"}
	if err := db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	for _, connection := range []model.RemoteOutboundConnection{
		{SubscriptionId: sub.Id, SortOrder: 20, Name: "b", SourceKey: "b", OutboundTag: "b", Enabled: false},
		{SubscriptionId: sub.Id, SortOrder: 10, Name: "a", SourceKey: "a", OutboundTag: "a", Enabled: false},
	} {
		if err := db.Create(&connection).Error; err != nil {
			t.Fatal(err)
		}
	}

	results, err := CheckSubscription(context.Background(), db, sub.Id, "https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("results length = %d, want 2", len(results))
	}
	if results[0].OutboundTag != "a" || results[1].OutboundTag != "b" {
		t.Fatalf("result order = %#v", results)
	}
}

func TestCheckOutboundsIncludesConvertedGroupMembers(t *testing.T) {
	db := newRemoteSyncDB(t)
	sub := model.RemoteOutboundSubscription{Name: "Remote", Url: "https://example.com/sub"}
	if err := db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	node := model.RemoteOutboundConnection{
		SubscriptionId: sub.Id,
		SortOrder:      10,
		Name:           "proxy-a",
		SourceKey:      "label:proxy-a",
		Type:           "vless",
		OutboundTag:    "ros-proxy-a",
		Enabled:        true,
		Options:        json.RawMessage(`{"server":"edge.example.com","server_port":443}`),
	}
	group := model.RemoteOutboundConnection{
		SubscriptionId: sub.Id,
		SortOrder:      20,
		Name:           "auto",
		SourceKey:      "label:auto",
		Type:           "urltest",
		OutboundTag:    "ros-auto",
		Enabled:        true,
		Options:        json.RawMessage(`{"outbounds":["proxy-a"],"default":"proxy-a","url":"http://www.gstatic.com/generate_204","interval":"10m","tolerance":50}`),
	}
	otherNode := model.RemoteOutboundConnection{
		SubscriptionId: sub.Id,
		SortOrder:      30,
		Name:           "proxy-b",
		SourceKey:      "label:proxy-b",
		Type:           "vless",
		OutboundTag:    "ros-proxy-b",
		Enabled:        true,
		Options:        json.RawMessage(`{"server":"unused.example.com","server_port":443}`),
	}
	otherGroup := model.RemoteOutboundConnection{
		SubscriptionId: sub.Id,
		SortOrder:      40,
		Name:           "other-auto",
		SourceKey:      "label:other-auto",
		Type:           "urltest",
		OutboundTag:    "ros-other-auto",
		Enabled:        true,
		Options:        json.RawMessage(`{"outbounds":["proxy-b"],"url":"http://www.gstatic.com/generate_204","interval":"10m","tolerance":50}`),
	}
	if err := db.Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&group).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&otherNode).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&otherGroup).Error; err != nil {
		t.Fatal(err)
	}

	outbounds, err := checkOutbounds(db, group)
	if err != nil {
		t.Fatal(err)
	}
	if len(outbounds) != 1 {
		t.Fatalf("outbounds = %d, want only converted leaf members for group delay", len(outbounds))
	}
	checkConfig, err := checkTempCoreConfig(db, group)
	if err != nil {
		t.Fatal(err)
	}
	if len(checkConfig.CheckTags) != 1 || checkConfig.CheckTags[0] != "ros-proxy-a" {
		t.Fatalf("group check tags = %#v, want only converted member tag", checkConfig.CheckTags)
	}
	for _, raw := range outbounds {
		var outbound map[string]any
		if err := json.Unmarshal(raw, &outbound); err != nil {
			t.Fatal(err)
		}
		if outbound["tag"] == "ros-auto" || outbound["tag"] == "ros-proxy-b" || outbound["tag"] == "ros-other-auto" {
			t.Fatalf("unrelated subscription outbound leaked into group delay config: %#v", outbound)
		}
	}
}
