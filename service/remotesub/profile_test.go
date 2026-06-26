package remotesubservice

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
	subcanonical "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/canonical"
)

func TestCollectedProfileGroupsCharacteristicsWithSourceTags(t *testing.T) {
	xray := subcanonical.ObserveOutbounds(subcanonical.FormatXray, []map[string]any{
		{
			"type":        "vless",
			"tag":         "proxy",
			"server":      "120.0.0.1",
			"server_port": 8443,
			"uuid":        "11111111-1111-1111-1111-111111111111",
			"flow":        "xtls-rprx-vision",
			"tls": map[string]any{
				"enabled":     true,
				"server_name": "speed.example.com",
			},
		},
		{
			"type":        "vless",
			"tag":         "proxy-2",
			"server":      "120.0.0.2",
			"server_port": 8443,
			"uuid":        "22222222-2222-2222-2222-222222222222",
			"tls": map[string]any{
				"enabled":     true,
				"server_name": "second.example.com",
			},
		},
		{
			"type":      "urltest",
			"tag":       "Balancer",
			"outbounds": []string{"proxy", "proxy-2"},
			"url":       "http://www.gstatic.com/generate_204",
			subcanonical.MetadataKey: map[string]any{
				"source_format":  subcanonical.FormatXray,
				"source_feature": "routing.balancer",
				"source_type":    "balancer",
				"target_type":    "urltest",
				"strategy":       "leastLoad",
			},
		},
	})
	mihomo := subcanonical.ObserveOutbounds(subcanonical.FormatClash, []map[string]any{
		{
			"type":        "vless",
			"name":        "proxy",
			"server":      "120.0.0.1",
			"server_port": 8443,
			"uuid":        "11111111-1111-1111-1111-111111111111",
			"udp":         true,
			"tls": map[string]any{
				"enabled":     true,
				"server_name": "speed.example.com",
			},
		},
	})
	snapshot := subcanonical.MergeSnapshots(xray, mihomo)
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}

	subscription := model.RemoteOutboundSubscription{
		Name:              "Remote",
		CanonicalSnapshot: data,
	}
	profile := collectedProfile(subscription)
	if len(profile) != 1 {
		t.Fatalf("top-level profile blocks = %d, want balancer only: %#v", len(profile), profile)
	}
	group := profile[0]
	if group.Name != "Balancer" || group.Type != "balancer group" {
		t.Fatalf("group block = %#v", group)
	}
	if len(group.Connections) != 2 {
		t.Fatalf("group connections = %d, want two members: %#v", len(group.Connections), group.Connections)
	}
	first := group.Connections[0]
	assertProfileCharacteristic(t, first, "name", "proxy", []string{"x-ray", "mihomo"})
	assertProfileCharacteristic(t, first, "server", "120.0.0.1", []string{"x-ray", "mihomo"})
	assertProfileCharacteristic(t, first, "tls.server_name", "speed.example.com", []string{"x-ray", "mihomo"})
	assertProfileCharacteristic(t, first, "udp", "true", []string{"mihomo"})
	assertProfileCharacteristic(t, first, "flow", "xtls-rprx-vision", []string{"x-ray"})

	summary := collectedSummary(subscription, profile)
	for _, fragment := range []string{
		"Name: Balancer",
		"Type: balancer group [x-ray]",
		"Connection 1:",
		"Name: proxy [x-ray, mihomo]",
		"IP: 120.0.0.1 [x-ray, mihomo]",
		"SNI: speed.example.com [x-ray, mihomo]",
		"Udp: true [mihomo]",
	} {
		if !strings.Contains(summary, fragment) {
			t.Fatalf("summary missing %q:\n%s", fragment, summary)
		}
	}
}

func TestCollectedProfileMergesSameNamedSingleRepresentationIntoGroup(t *testing.T) {
	uriLike := subcanonical.ObserveOutbounds(subcanonical.FormatURI, []map[string]any{{
		"type":        "vless",
		"tag":         "Auto Balancer",
		"server":      "uri.example.com",
		"server_port": 443,
		"uuid":        "11111111-1111-1111-1111-111111111111",
	}})
	xray := subcanonical.ObserveOutbounds(subcanonical.FormatXray, []map[string]any{
		{
			"type":        "vless",
			"tag":         "Auto Balancer / proxy",
			"xray_tag":    "proxy",
			"server":      "one.example.com",
			"server_port": 443,
			"uuid":        "22222222-2222-2222-2222-222222222222",
		},
		{
			"type":        "vless",
			"tag":         "Auto Balancer / proxy-2",
			"xray_tag":    "proxy-2",
			"server":      "two.example.com",
			"server_port": 443,
			"uuid":        "33333333-3333-3333-3333-333333333333",
		},
		{
			"type":      "urltest",
			"tag":       "Auto Balancer",
			"xray_tag":  "Balancer",
			"outbounds": []string{"Auto Balancer / proxy", "Auto Balancer / proxy-2"},
			subcanonical.MetadataKey: map[string]any{
				"source_format":  subcanonical.FormatXray,
				"source_feature": "routing.balancer",
				"source_type":    "balancer",
				"target_type":    "urltest",
			},
		},
	})
	snapshot := subcanonical.MergeSnapshots(uriLike, xray)
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	subscription := model.RemoteOutboundSubscription{
		Name:              "Remote",
		CanonicalSnapshot: data,
	}

	profile := collectedProfile(subscription)
	if len(profile) != 1 {
		t.Fatalf("profile blocks = %d, want one top-level group: %#v", len(profile), profile)
	}
	group := profile[0]
	if group.Name != "Auto Balancer" || group.Type != "balancer group" {
		t.Fatalf("group = %#v", group)
	}
	assertProfileCharacteristic(t, group, "type", "urltest", []string{"x-ray"})
	assertProfileCharacteristic(t, group, "type", "vless", []string{"uri"})
	assertProfileCharacteristic(t, group, "server", "uri.example.com", []string{"uri"})
	if len(group.Connections) != 2 {
		t.Fatalf("group connections = %d, want two members: %#v", len(group.Connections), group.Connections)
	}
	summary := collectedSummary(subscription, profile)
	if strings.Count(summary, "Name: Auto Balancer\n") != 1 {
		t.Fatalf("same-named single representation leaked as a top-level block:\n%s", summary)
	}
	if !strings.Contains(summary, "Type: balancer group [x-ray, uri]") {
		t.Fatalf("group sources were not merged into summary:\n%s", summary)
	}
}

func assertProfileCharacteristic(t *testing.T, block CollectedProfileBlock, key string, value string, sources []string) {
	t.Helper()
	for _, characteristic := range block.Characteristics {
		if characteristic.Key != key {
			continue
		}
		for _, candidate := range characteristic.Values {
			if candidate.Value == value && strings.Join(candidate.Sources, ",") == strings.Join(sources, ",") {
				return
			}
		}
		t.Fatalf("characteristic %s value %q/%v not found in %#v", key, value, sources, characteristic)
	}
	t.Fatalf("characteristic %s not found in block %#v", key, block)
}
