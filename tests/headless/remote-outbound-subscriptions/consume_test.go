package subscriptionqualification

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	source "github.com/MalenkiySolovey/solovey-component-remote-outbound-subscriptions"
	remote "github.com/MalenkiySolovey/solovey-component-remote-outbound-subscriptions/domain"
	feature "github.com/MalenkiySolovey/solovey-component-remote-outbound-subscriptions/service"
)

func TestAcceptedSnapshotUsesSameFeatureWithoutHostExecution(t *testing.T) {
	raw := map[string]any{"type": "socks", "tag": "Accepted endpoint", "server": "edge.example", "server_port": 1080, "username": "fixture", "password": "fixture"}
	before, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	normalized, err := remote.NormalizeOutbound(raw, 0)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(raw)
	if !bytes.Equal(before, after) {
		t.Fatal("feature mutated caller-owned snapshot")
	}
	endpoint := remote.CanonicalConnection(normalized.Canonical)
	if endpoint == nil || endpoint.EndpointID == "" {
		t.Fatal("accepted endpoint identity absent")
	}
	raw["tag"] = "Renamed endpoint"
	renamed, err := remote.NormalizeOutbound(raw, 1)
	if err != nil {
		t.Fatal(err)
	}
	if remote.CanonicalConnection(renamed.Canonical).EndpointID != endpoint.EndpointID {
		t.Fatal("label change replaced accepted semantic endpoint")
	}
	sourceID, groupID, connectionID, outboundID := uint(41), uint(51), uint(61), uint(81)
	accepted := remote.RemoteOutboundSubscription{Id: sourceID, Enabled: true, Groups: []remote.RemoteOutboundGroup{{Id: groupID, SubscriptionId: sourceID}}, Connections: []remote.RemoteOutboundConnection{{Id: connectionID, SubscriptionId: sourceID, GroupId: groupID, Name: normalized.Name, SourceKey: normalized.SourceKey, Type: normalized.Type, Options: normalized.Options, Canonical: normalized.Canonical, OutboundId: &outboundID, OutboundTag: "accepted-node", Synced: true}}}
	encoded, err := json.Marshal(accepted)
	if err != nil {
		t.Fatal(err)
	}
	var roundtrip remote.RemoteOutboundSubscription
	if err := json.Unmarshal(encoded, &roundtrip); err != nil {
		t.Fatal(err)
	}
	c := roundtrip.Connections[0]
	if roundtrip.Id != sourceID || roundtrip.Groups[0].Id != groupID || c.Id != connectionID || c.OutboundId == nil || *c.OutboundId != outboundID || c.OutboundTag != "accepted-node" || !c.Synced {
		t.Fatal("accepted resource relationships changed")
	}
}

func TestNodeRoleHasNoDefaultFetchParserOrSchedulerAuthority(t *testing.T) {
	if _, err := remote.FetchSubscriptionWithOptionsContext(context.Background(), "https://provider.example/subscription", remote.FetchOptions{}); err == nil || !strings.Contains(err.Error(), "transport is unavailable") {
		t.Fatalf("missing authorized transport accepted: %v", err)
	}
	if _, err := remote.ParseFetchedSubscription("accepted opaque payload"); err == nil || !strings.Contains(err.Error(), "parser is unavailable") {
		t.Fatalf("missing parser accepted: %v", err)
	}
	var scheduler feature.Scheduler
	if err := scheduler.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	first := source.ManifestJSON()
	second := source.ManifestJSON()
	first[0] = '!'
	if bytes.Equal(first, second) || second[0] != '{' {
		t.Fatal("descriptor leaked mutable global source")
	}
	var manifest struct {
		ID, Delivery   string
		DefaultEnabled bool
	}
	if err := json.Unmarshal(second, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.ID != "remote-outbound-subscriptions" || manifest.Delivery != "in-process" || !manifest.DefaultEnabled {
		t.Fatal("component identity/default changed")
	}
}
