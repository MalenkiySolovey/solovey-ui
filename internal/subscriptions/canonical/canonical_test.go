package canonical

import "testing"

func TestMergeSnapshotsCombinesSameConnectionAcrossFormats(t *testing.T) {
	singbox := ObserveOutbounds(FormatSingBox, []map[string]any{{
		"type":        "vless",
		"tag":         "Node A",
		"server":      "edge.example.com",
		"server_port": 443,
		"uuid":        "11111111-1111-1111-1111-111111111111",
		"tls": map[string]any{
			"enabled":     true,
			"server_name": "sni.example.com",
		},
	}})
	clash := ObserveOutbounds(FormatClash, []map[string]any{{
		"type":        "vless",
		"name":        "Node A",
		"server":      "edge.example.com",
		"server_port": 443,
		"uuid":        "11111111-1111-1111-1111-111111111111",
		"transport": map[string]any{
			"type": "ws",
			"path": "/ws",
			"headers": map[string]any{
				"Host": "cdn.example.com",
			},
		},
	}})

	merged := MergeSnapshots(singbox, clash)
	if len(merged.Connections) != 1 {
		t.Fatalf("connections = %d, want 1", len(merged.Connections))
	}
	connection := merged.Connections[0]
	if connection.TLS.ServerName != "sni.example.com" {
		t.Fatalf("tls was not preserved: %#v", connection.TLS)
	}
	if connection.Transport.Type != "ws" || connection.Transport.Path != "/ws" || connection.Transport.Host != "cdn.example.com" {
		t.Fatalf("transport was not merged: %#v", connection.Transport)
	}
	if len(connection.Observations) != 2 {
		t.Fatalf("observations = %d, want 2", len(connection.Observations))
	}
	if len(connection.Formats) != 2 {
		t.Fatalf("formats = %#v, want both source formats", connection.Formats)
	}
}

func TestNormalizeLabelIgnoresWhitespaceAndCaseOnly(t *testing.T) {
	left := NormalizeLabel(" Auto | Балансер Обход-1 ")
	right := NormalizeLabel("auto|балансеробход-1")
	if left != right {
		t.Fatalf("normalized labels = %q / %q", left, right)
	}
	withSymbols := NormalizeLabel("A-B_1/2")
	if withSymbols != "a-b_1/2" {
		t.Fatalf("normalized label should keep non-space symbols, got %q", withSymbols)
	}
}

func TestMergeSnapshotsKeepsDifferentExplicitNamesSeparate(t *testing.T) {
	first := ObserveOutbounds(FormatXray, []map[string]any{{
		"type":        "vless",
		"tag":         "Profile A",
		"server":      "edge.example.com",
		"server_port": 443,
		"uuid":        "11111111-1111-1111-1111-111111111111",
	}})
	second := ObserveOutbounds(FormatXray, []map[string]any{{
		"type":        "vless",
		"tag":         "Profile B",
		"server":      "edge.example.com",
		"server_port": 443,
		"uuid":        "11111111-1111-1111-1111-111111111111",
	}})

	merged := MergeSnapshots(first, second)
	if len(merged.Connections) != 2 {
		t.Fatalf("connections = %d, want different explicitly named profiles kept separate: %#v", len(merged.Connections), merged.Connections)
	}
}

func TestMergeSnapshotsDoesNotMergeSameFormatEvenWithSameName(t *testing.T) {
	first := ObserveOutbounds(FormatXray, []map[string]any{{
		"type":        "vless",
		"tag":         "Repeated Profile",
		"server":      "edge-a.example.com",
		"server_port": 443,
		"uuid":        "11111111-1111-1111-1111-111111111111",
	}})
	second := ObserveOutbounds(FormatXray, []map[string]any{{
		"type":        "vless",
		"tag":         "Repeated Profile",
		"server":      "edge-b.example.com",
		"server_port": 443,
		"uuid":        "11111111-1111-1111-1111-111111111111",
	}})

	merged := MergeSnapshots(first, second)
	if len(merged.Connections) != 2 {
		t.Fatalf("connections = %d, want same-format rows preserved separately: %#v", len(merged.Connections), merged.Connections)
	}
}

func TestMergeSnapshotsCombinesSameNamedConnectionObservations(t *testing.T) {
	xray := ObserveOutbounds(FormatXray, []map[string]any{{
		"type":        "vless",
		"tag":         "Shared Node",
		"server":      "xray.example.com",
		"server_port": 443,
		"uuid":        "11111111-1111-1111-1111-111111111111",
		"flow":        "xtls-rprx-vision",
	}})
	clash := ObserveOutbounds(FormatClash, []map[string]any{{
		"type":        "vless",
		"name":        "Shared Node",
		"server":      "mihomo.example.com",
		"server_port": 8443,
		"uuid":        "22222222-2222-2222-2222-222222222222",
		"udp":         true,
	}})

	merged := MergeSnapshots(xray, clash)
	if len(merged.Connections) != 1 {
		t.Fatalf("connections = %d, want same-name observations merged", len(merged.Connections))
	}
	connection := merged.Connections[0]
	if len(connection.Observations) != 2 {
		t.Fatalf("observations = %d, want both source observations", len(connection.Observations))
	}
	if len(connection.Formats) != 2 {
		t.Fatalf("formats = %#v, want both source formats", connection.Formats)
	}
	if _, ok := connection.BestOutbound["flow"]; !ok {
		t.Fatalf("xray-only field was not retained: %#v", connection.BestOutbound)
	}
	if _, ok := connection.BestOutbound["udp"]; !ok {
		t.Fatalf("clash-only field was not retained: %#v", connection.BestOutbound)
	}
}

func TestMergeSnapshotsDoesNotPromoteXrayProfileRuntimeOverSameNamedURI(t *testing.T) {
	uriLike := ObserveOutbounds(FormatURI, []map[string]any{{
		"type":        "vless",
		"tag":         "Auto Balancer",
		"server":      "120",
		"server_port": 8443,
		"uuid":        "11111111-1111-1111-1111-111111111111",
	}})
	xrayProfile := ObserveOutbounds(FormatXray, []map[string]any{{
		"type":              "vless",
		"tag":               "Auto Balancer",
		"server":            "84.201.155.222",
		"server_port":       8443,
		"uuid":              "11111111-1111-1111-1111-111111111111",
		"xray_profile":      true,
		"xray_profile_type": "balancer",
		"xray_profile_outbounds": []map[string]any{{
			"type":        "vless",
			"tag":         "proxy",
			"server":      "84.201.155.222",
			"server_port": 8443,
			"uuid":        "11111111-1111-1111-1111-111111111111",
		}},
		MetadataKey: map[string]any{
			"source_format":  FormatXray,
			"source_feature": "custom.config",
			"source_type":    "balancer",
			"target_type":    "vless",
		},
	}})

	merged := MergeSnapshots(uriLike, xrayProfile)
	if len(merged.Connections) != 1 {
		t.Fatalf("connections = %d, want one merged profile", len(merged.Connections))
	}
	connection := merged.Connections[0]
	if connection.Endpoint.Server != "120" {
		t.Fatalf("runtime endpoint = %#v, want first successful format endpoint", connection.Endpoint)
	}
	outbound := ConnectionOutbound(connection)
	if outbound["server"] != "120" {
		t.Fatalf("runtime outbound = %#v, want first successful format endpoint", outbound)
	}
	if !hasFormat(connection.Formats, FormatURI) || !hasFormat(connection.Formats, FormatXray) {
		t.Fatalf("formats = %#v, want uri and xray retained", connection.Formats)
	}
}

func TestMergeSnapshotsKeepsDifferentNamesSeparateEvenWhenParametersMatch(t *testing.T) {
	xray := ObserveOutbounds(FormatXray, []map[string]any{{
		"type":        "vless",
		"tag":         "Profile / proxy",
		"xray_tag":    "proxy",
		"server":      "edge.example.com",
		"server_port": 443,
		"uuid":        "11111111-1111-1111-1111-111111111111",
	}})
	clash := ObserveOutbounds(FormatClash, []map[string]any{{
		"type":        "vless",
		"name":        "proxy",
		"server":      "edge.example.com",
		"server_port": 443,
		"uuid":        "11111111-1111-1111-1111-111111111111",
	}})

	merged := MergeSnapshots(xray, clash)
	if len(merged.Connections) != 2 {
		t.Fatalf("connections = %d, want different normalized names kept separate: %#v", len(merged.Connections), merged.Connections)
	}
}

func TestMergeSnapshotsKeepsSameNamedGroupAsRuntimeConnection(t *testing.T) {
	uriLike := ObserveOutbounds(FormatURI, []map[string]any{{
		"type":        "vless",
		"tag":         "Auto Balancer",
		"server":      "edge.example.com",
		"server_port": 443,
		"uuid":        "11111111-1111-1111-1111-111111111111",
	}})
	xrayGroup := ObserveOutbounds(FormatXray, []map[string]any{
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
			MetadataKey: map[string]any{
				"source_format":  FormatXray,
				"source_feature": "routing.balancer",
				"source_type":    "balancer",
				"target_type":    "urltest",
			},
		},
	})

	merged := MergeSnapshots(uriLike, xrayGroup)
	if len(merged.Connections) != 3 {
		t.Fatalf("connections = %d, want 2 members + group: %#v", len(merged.Connections), merged.Connections)
	}
	var group Connection
	for _, connection := range merged.Connections {
		if len(connection.GroupMembers) > 0 {
			group = connection
			break
		}
	}
	if group.DisplayName != "Auto Balancer" || group.Protocol != "urltest" || len(group.GroupMembers) != 2 {
		t.Fatalf("group did not remain the runtime connection: %#v", group)
	}
	if group.BestOutbound["type"] != "urltest" {
		t.Fatalf("group best outbound was overwritten by single representation: %#v", group.BestOutbound)
	}
	if len(group.Observations) != 2 {
		t.Fatalf("group observations = %d, want xray group + same-name uri representation", len(group.Observations))
	}
	if len(group.Formats) != 2 {
		t.Fatalf("group formats = %#v, want xray + uri", group.Formats)
	}
}

func TestSameFormatConnectionsStaySeparateEvenWithDifferentCredentials(t *testing.T) {
	snapshot := ObserveOutbounds(FormatSingBox, []map[string]any{
		{
			"type":        "vless",
			"server":      "edge.example.com",
			"server_port": 443,
			"uuid":        "11111111-1111-1111-1111-111111111111",
		},
		{
			"type":        "vless",
			"server":      "edge.example.com",
			"server_port": 443,
			"uuid":        "22222222-2222-2222-2222-222222222222",
		},
	})
	if len(snapshot.Connections) != 2 {
		t.Fatalf("connections = %d, want 2", len(snapshot.Connections))
	}
}

func TestSnapshotOutboundsUsesCanonicalFallbackFields(t *testing.T) {
	outbounds := SnapshotOutbounds(Snapshot{Connections: []Connection{{
		DisplayName: "Node",
		Protocol:    "trojan",
		Endpoint:    Endpoint{Server: "example.com", Port: "443"},
		BestOutbound: map[string]any{
			"password": "secret",
		},
	}}})
	if len(outbounds) != 1 {
		t.Fatalf("outbounds = %d, want 1", len(outbounds))
	}
	outbound := outbounds[0]
	if outbound["type"] != "trojan" || outbound["tag"] != "Node" || outbound["server"] != "example.com" || outbound["server_port"] != "443" {
		t.Fatalf("outbound fallback fields were not applied: %#v", outbound)
	}
}

func TestAdaptationMetadataStoredOutsideRuntimeOutbound(t *testing.T) {
	snapshot := ObserveOutbounds(FormatXray, []map[string]any{{
		"type":      "urltest",
		"tag":       "auto",
		"outbounds": []string{"proxy-a"},
		MetadataKey: map[string]any{
			"source_format":  FormatXray,
			"source_feature": "routing.balancer",
			"source_type":    "balancer",
			"target_type":    "urltest",
			"strategy":       "leastPing",
		},
	}})
	if len(snapshot.Connections) != 1 {
		t.Fatalf("connections = %d, want 1", len(snapshot.Connections))
	}
	connection := snapshot.Connections[0]
	if len(connection.Adaptations) != 1 {
		t.Fatalf("adaptations = %#v, want one", connection.Adaptations)
	}
	if len(connection.GroupMembers) != 1 || connection.GroupMembers[0] != "proxy-a" {
		t.Fatalf("group members = %#v", connection.GroupMembers)
	}
	adaptation := connection.Adaptations[0]
	if adaptation.SourceFeature != "routing.balancer" || adaptation.SourceType != "balancer" || adaptation.TargetType != "urltest" || adaptation.Strategy != "leastPing" {
		t.Fatalf("adaptation = %#v", adaptation)
	}
	if _, exists := connection.BestOutbound[MetadataKey]; exists {
		t.Fatalf("best outbound leaked metadata: %#v", connection.BestOutbound)
	}
	if _, exists := connection.Observations[0].Outbound[MetadataKey]; exists {
		t.Fatalf("observation leaked metadata: %#v", connection.Observations[0].Outbound)
	}
	outbounds := SnapshotOutbounds(snapshot)
	if _, exists := outbounds[0][MetadataKey]; exists {
		t.Fatalf("snapshot outbound leaked metadata: %#v", outbounds[0])
	}
}

func TestMergeConnectionsCombinesAdaptations(t *testing.T) {
	merged := MergeConnections(
		Connection{
			Adaptations: []Adaptation{{
				SourceFormat:  FormatXray,
				SourceFeature: "routing.balancer",
				SourceType:    "balancer",
				TargetType:    "urltest",
			}},
		},
		Connection{
			Adaptations: []Adaptation{{
				SourceFormat:  FormatClash,
				SourceFeature: "proxy-groups",
				SourceType:    "select",
				TargetType:    "selector",
			}},
		},
	)
	if len(merged.Adaptations) != 2 {
		t.Fatalf("adaptations = %#v, want both source adaptations", merged.Adaptations)
	}
}

func TestMergeSnapshotsKeepsClashSelectCompositionGroupAsExtraWhenMembersAlreadyExist(t *testing.T) {
	xray := ObserveOutbounds(FormatXray, []map[string]any{
		{
			"type":        "vless",
			"tag":         "Node A",
			"server":      "a.example.com",
			"server_port": 443,
		},
		{
			"type":        "vless",
			"tag":         "Node B",
			"server":      "b.example.com",
			"server_port": 443,
		},
	})
	clash := ObserveOutbounds(FormatClash, []map[string]any{{
		"type":      "selector",
		"tag":       "Auto",
		"outbounds": []string{"Node A", "Node B"},
		MetadataKey: map[string]any{
			"source_format":  FormatClash,
			"source_feature": "proxy-groups",
			"source_type":    "select",
			"target_type":    "selector",
		},
	}})

	merged := MergeSnapshots(xray, clash)
	if len(merged.Connections) != 2 {
		t.Fatalf("connections = %d, want only existing nodes: %#v", len(merged.Connections), merged.Connections)
	}
	if len(merged.Extras) != 1 || merged.Extras[0].Name != "Auto" {
		t.Fatalf("composition group was not preserved as extra: %#v", merged.Extras)
	}
}

func TestMergeSnapshotsKeepsClashLoadBalanceGroupAsTopLevelConnection(t *testing.T) {
	nodes := ObserveOutbounds(FormatClash, []map[string]any{
		{
			"type":        "vless",
			"tag":         "Node A",
			"server":      "a.example.com",
			"server_port": 443,
		},
		{
			"type":        "vless",
			"tag":         "Node B",
			"server":      "b.example.com",
			"server_port": 443,
		},
	})
	group := ObserveOutbounds(FormatClash, []map[string]any{{
		"type":      "urltest",
		"tag":       "Balance",
		"outbounds": []string{"Node A", "Node B"},
		MetadataKey: map[string]any{
			"source_format":  FormatClash,
			"source_feature": "proxy-groups",
			"source_type":    "load-balance",
			"target_type":    "urltest",
			"strategy":       "sticky-sessions",
		},
	}})

	merged := MergeSnapshots(nodes, group)
	if len(merged.Connections) != 3 {
		t.Fatalf("connections = %d, want nodes plus load-balance group: %#v", len(merged.Connections), merged.Connections)
	}
	if len(merged.Extras) != 0 {
		t.Fatalf("extras = %#v, want load-balance as a visible group connection", merged.Extras)
	}
	connection := merged.Connections[2]
	if connection.DisplayName != "Balance" || connection.Kind != KindGroup || len(connection.GroupMembers) != 2 {
		t.Fatalf("load-balance group connection = %#v", connection)
	}
}

func hasFormat(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
