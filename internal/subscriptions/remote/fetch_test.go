package remote

import (
	"net/url"
	"strings"
	"testing"

	subcanonical "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/canonical"
	"github.com/MalenkiySolovey/solovey-ui/util/common"
)

func TestParseFetchedSubscriptionSupportsClashYAML(t *testing.T) {
	fetched, err := ParseFetchedSubscription(`
proxies:
  - name: Node A
    type: vless
    server: edge.example.com
    port: 443
    uuid: 11111111-1111-1111-1111-111111111111
    tls: true
    servername: sni.example.com
    network: ws
    ws-opts:
      path: /ws
proxy-groups:
  - name: Auto
    type: select
    proxies: [Node A]
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(fetched.Outbounds) != 1 {
		t.Fatalf("outbounds = %d, want visible proxy only", len(fetched.Outbounds))
	}
	if len(fetched.Snapshot.Connections) != 1 {
		t.Fatalf("snapshot connections = %d, want visible proxy only", len(fetched.Snapshot.Connections))
	}
	connection := fetched.Snapshot.Connections[0]
	if connection.Formats[0] != subcanonical.FormatClash {
		t.Fatalf("formats = %#v", connection.Formats)
	}
	if connection.Transport.Type != "ws" || connection.Transport.Path != "/ws" {
		t.Fatalf("transport = %#v", connection.Transport)
	}
	if len(fetched.Snapshot.Extras) != 1 {
		t.Fatalf("snapshot extras = %#v, want mihomo group as non-row metadata", fetched.Snapshot.Extras)
	}
	group := fetched.Snapshot.Extras[0]
	if group.Name != "Auto" {
		t.Fatalf("mihomo group extra = %#v", group)
	}
	if _, ok := group.Outbound["mihomo_group"].(map[string]any); !ok {
		t.Fatalf("mihomo group native metadata missing: %#v", group.Outbound)
	}
}

func TestParseFetchedSubscriptionKeepsMihomoLoadBalanceGroupsVisible(t *testing.T) {
	fetched, err := ParseFetchedSubscription(`
proxies:
  - name: Vless A
    type: vless
    server: a.example.com
    port: 443
    uuid: 11111111-1111-1111-1111-111111111111
  - name: Vless B
    type: vless
    server: b.example.com
    port: 443
    uuid: 22222222-2222-2222-2222-222222222222
proxy-groups:
  - name: BALANCE-VLESS
    type: load-balance
    strategy: sticky-sessions
    hidden: true
    proxies:
      - Vless A
      - Vless B
    url: https://www.gstatic.com/generate_204
    interval: 300
    timeout: 5000
    lazy: false
  - name: OTHER
    type: select
    proxies:
      - BALANCE-VLESS
      - DIRECT
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(fetched.Outbounds) != 3 {
		t.Fatalf("outbounds = %d, want two nodes plus visible load-balance group: %#v", len(fetched.Outbounds), fetched.Outbounds)
	}
	balance := fetched.Snapshot.Connections[2]
	if balance.Kind != subcanonical.KindGroup || balance.DisplayName != "BALANCE-VLESS" {
		t.Fatalf("load-balance group was not preserved as a visible connection: %#v", balance)
	}
	if balance.Protocol != "urltest" || len(balance.GroupMembers) != 2 {
		t.Fatalf("load-balance runtime shape = %#v", balance)
	}
	if len(fetched.Snapshot.Extras) != 1 || fetched.Snapshot.Extras[0].Name != "OTHER" {
		t.Fatalf("select group extras = %#v, want only OTHER as auxiliary metadata", fetched.Snapshot.Extras)
	}
	metadata, ok := balance.BestOutbound["mihomo_group"].(map[string]any)
	if !ok || metadata["type"] != "load-balance" || metadata["strategy"] != "sticky-sessions" || metadata["hidden"] != true {
		t.Fatalf("mihomo load-balance metadata = %#v", balance.BestOutbound["mihomo_group"])
	}
}

func TestParseFetchedSubscriptionMergesCanonicalOutbounds(t *testing.T) {
	fetched, err := ParseFetchedSubscription(`{
  "outbounds": [
    {
      "type": "vless",
      "tag": "Node A",
      "server": "edge.example.com",
      "server_port": 443,
      "uuid": "11111111-1111-1111-1111-111111111111"
    }
  ]
}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(fetched.Snapshot.Connections) != 1 || fetched.Snapshot.Connections[0].DisplayName != "Node A" {
		t.Fatalf("snapshot = %#v", fetched.Snapshot)
	}
	if fetched.Outbounds[0]["tag"] != "Node A" {
		t.Fatalf("outbound = %#v", fetched.Outbounds[0])
	}
}

func TestParseFetchedSubscriptionSupportsSingBoxGroups(t *testing.T) {
	fetched, err := ParseFetchedSubscription(`{
  "outbounds": [
    {
      "type": "vless",
      "tag": "Node A",
      "server": "edge.example.com",
      "server_port": 443,
      "uuid": "11111111-1111-1111-1111-111111111111"
    },
    {
      "type": "selector",
      "tag": "Manual",
      "outbounds": ["Node A"],
      "default": "Node A"
    },
    {
      "type": "urltest",
      "tag": "Auto",
      "outbounds": ["Node A"],
      "url": "http://www.gstatic.com/generate_204",
      "interval": "5m"
    },
    {"type": "direct", "tag": "direct"},
    {"type": "block", "tag": "block"}
  ]
}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(fetched.Outbounds) != 3 {
		t.Fatalf("outbounds = %d, want node + two groups", len(fetched.Outbounds))
	}
	if fetched.Outbounds[1]["type"] != "selector" || fetched.Outbounds[2]["type"] != "urltest" {
		t.Fatalf("sing-box groups were not preserved: %#v", fetched.Outbounds)
	}
	if len(fetched.Snapshot.Connections) != 3 {
		t.Fatalf("snapshot connections = %d, want 3", len(fetched.Snapshot.Connections))
	}
}

func TestParseFetchedSubscriptionSupportsXrayJSONBalancer(t *testing.T) {
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
            "users": [
              {"id": "11111111-1111-1111-1111-111111111111"}
            ]
          }
        ]
      },
      "streamSettings": {"network": "tcp", "security": "tls", "tlsSettings": {"serverName": "sni.example.com"}}
    }
  ],
  "routing": {
    "balancers": [
      {"tag": "auto", "selector": ["proxy"]}
    ]
  }
}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(fetched.Outbounds) != 2 {
		t.Fatalf("outbounds = %d, want xray group + dependency", len(fetched.Outbounds))
	}
	if len(fetched.Snapshot.Connections) != 2 {
		t.Fatalf("connections = %d, want xray group + dependency", len(fetched.Snapshot.Connections))
	}
	connection := fetched.Snapshot.Connections[0]
	if connection.Formats[0] != subcanonical.FormatXray {
		t.Fatalf("formats = %#v", connection.Formats)
	}
	if fetched.Outbounds[0]["type"] != "urltest" || fetched.Outbounds[0]["tag"] != "auto" {
		t.Fatalf("runtime group outbound = %#v", fetched.Outbounds[0])
	}
	if _, exists := fetched.Outbounds[0][subcanonical.MetadataKey]; exists {
		t.Fatalf("runtime outbound leaked metadata: %#v", fetched.Outbounds[0])
	}
	if _, exists := fetched.Outbounds[0]["xray_profile_outbounds"]; exists {
		t.Fatalf("runtime outbound leaked xray profile data: %#v", fetched.Outbounds[0])
	}
	if len(connection.Adaptations) != 1 || connection.Adaptations[0].SourceFormat != subcanonical.FormatXray || connection.Adaptations[0].SourceFeature != "routing.balancer" || connection.Adaptations[0].SourceType != "balancer" {
		t.Fatalf("group adaptation = %#v", connection.Adaptations)
	}
	balancers, _ := connection.Observations[0].Outbound["xray_profile_balancers"].([]map[string]any)
	if len(balancers) != 1 || balancers[0]["tag"] != "auto" {
		t.Fatalf("group balancers = %#v", connection.Observations[0].Outbound)
	}
}

func TestParseFetchedSubscriptionMapsXrayBalancerToRuntimeGroup(t *testing.T) {
	fetched, err := ParseFetchedSubscription(`{
  "outbounds": [
    {"tag": "proxy", "protocol": "vless", "settings": {"vnext": [{"address": "one.example.com", "port": 443, "users": [{"id": "11111111-1111-1111-1111-111111111111"}]}]}},
    {"tag": "proxy-2", "protocol": "vless", "settings": {"vnext": [{"address": "two.example.com", "port": 443, "users": [{"id": "22222222-2222-2222-2222-222222222222"}]}]}},
    {"tag": "proxy-3", "protocol": "vless", "settings": {"vnext": [{"address": "three.example.com", "port": 443, "users": [{"id": "33333333-3333-3333-3333-333333333333"}]}]}},
    {"tag": "direct", "protocol": "freedom"}
  ],
  "routing": {
    "rules": [{"balancerTag": "Balancer", "network": "tcp,udp"}],
    "balancers": [{"tag": "Balancer", "selector": ["proxy"], "fallbackTag": "direct", "strategy": {"type": "leastLoad"}}]
  }
}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(fetched.Outbounds) != 4 {
		t.Fatalf("outbounds = %d, want group + dependencies", len(fetched.Outbounds))
	}
	if len(fetched.Snapshot.Connections) != 4 {
		t.Fatalf("connections = %d, want group + dependencies", len(fetched.Snapshot.Connections))
	}
	group := fetched.Snapshot.Connections[0]
	if group.Protocol != "urltest" || len(group.GroupMembers) != 3 {
		t.Fatalf("xray balancer should be a runtime group: %#v", group)
	}
	observed := group.Observations[0].Outbound
	members, _ := observed["xray_profile_outbounds"].([]map[string]any)
	if len(members) != 3 {
		t.Fatalf("profile members = %#v", members)
	}
	balancers, _ := observed["xray_profile_balancers"].([]map[string]any)
	if len(balancers) != 1 {
		t.Fatalf("profile balancers = %#v", observed)
	}
	refs, _ := balancers[0]["members"].([]string)
	if !hasString(refs, "proxy") || !hasString(refs, "proxy-2") || !hasString(refs, "proxy-3") {
		t.Fatalf("xray balancer members = %#v", balancers[0])
	}
}

func TestParseFetchedSubscriptionUsesGroupAdaptationOptions(t *testing.T) {
	fetched, err := ParseFetchedSubscriptionWithOptions(`{
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
      }
    }
  ],
  "routing": {"balancers": [{"tag": "auto", "selector": ["proxy"]}]}
}`, FetchOptions{GroupAdaptation: "failover"})
	if err != nil {
		t.Fatal(err)
	}
	if len(fetched.Outbounds) != 2 || fetched.Outbounds[0]["type"] != "failover" {
		t.Fatalf("runtime outbound should be adapted group + dependency: %#v", fetched.Outbounds)
	}
	group := fetched.Snapshot.Connections[0]
	balancers, _ := group.Observations[0].Outbound["xray_profile_balancers"].([]map[string]any)
	if len(balancers) != 1 || balancers[0]["target_type"] != "failover" {
		t.Fatalf("adapted group balancer = %#v", group.Observations[0].Outbound)
	}
}

func TestSubscriptionFetchCandidatesPreserveOriginalResource(t *testing.T) {
	candidates, err := subscriptionFetchCandidates("https://example.com/sub/secret?token=abc&format=clash")
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 6 {
		t.Fatalf("candidates = %#v", candidates)
	}
	if candidates[0].Variant != subcanonical.FormatXray || candidates[0].UserAgent != "v2rayNG/1.10.14" {
		t.Fatalf("first candidate = %#v, want xray variant first", candidates[0])
	}
	original := candidates[len(candidates)-1]
	if original.URL != "https://example.com/sub/secret?token=abc&format=clash" || original.Variant != "original" {
		t.Fatalf("original candidate = %#v", original)
	}
	for _, candidate := range candidates {
		parsed, err := url.Parse(candidate.URL)
		if err != nil {
			t.Fatal(err)
		}
		if parsed.Scheme != "https" || parsed.Host != "example.com" || parsed.Path != "/sub/secret" {
			t.Fatalf("candidate changed resource boundary: %#v", candidate)
		}
		if parsed.Query().Get("token") != "abc" {
			t.Fatalf("candidate dropped token query: %#v", candidate)
		}
	}
}

func TestFetchSubscriptionQueriesFormatVariantsAndMerges(t *testing.T) {
	originalFetch := fetchSubscriptionData
	originalUserAgentFetch := fetchSubscriptionDataWithUserAgent
	t.Cleanup(func() {
		fetchSubscriptionData = originalFetch
		fetchSubscriptionDataWithUserAgent = originalUserAgentFetch
	})
	var requested []fetchCandidate
	fetch := func(rawURL string, userAgent string) (string, error) {
		requested = append(requested, fetchCandidate{URL: rawURL, UserAgent: userAgent})
		parsed, err := url.Parse(rawURL)
		if err != nil {
			return "", err
		}
		switch parsed.Query().Get("format") {
		case "json":
			return `{
  "outbounds": [
    {
      "type": "vless",
      "tag": "Node A",
      "server": "edge.example.com",
      "server_port": 443,
      "uuid": "11111111-1111-1111-1111-111111111111",
      "tls": {"enabled": true, "server_name": "sni.example.com"}
    }
  ]
}`, nil
		case "clash":
			return `
proxies:
  - name: Node A
    type: vless
    server: edge.example.com
    port: 443
    uuid: 11111111-1111-1111-1111-111111111111
    network: ws
    ws-opts:
      path: /ws
proxy-groups:
  - name: Auto
    type: select
    proxies: [Node A]
`, nil
		case "xray":
			return `{
  "outbounds": [
    {
      "tag": "Node A",
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
  ]
}`, nil
		default:
			return "", common.NewError("not available")
		}
	}
	fetchSubscriptionData = func(rawURL string) (string, error) {
		return fetch(rawURL, "")
	}
	fetchSubscriptionDataWithUserAgent = func(rawURL string, userAgent string) (string, error) {
		return fetch(rawURL, userAgent)
	}

	fetched, err := FetchSubscription("https://example.com/sub/secret?token=abc")
	if err != nil {
		t.Fatal(err)
	}
	if len(requested) != 6 {
		t.Fatalf("requested urls = %#v", requested)
	}
	if requested[0].UserAgent != "v2rayNG/1.10.14" ||
		requested[1].UserAgent != "v2rayNG/1.10.14" ||
		requested[2].UserAgent != "ClashMetaForAndroid/2.11.16" ||
		requested[3].UserAgent != "sing-box/1.12.0" {
		t.Fatalf("client user agents were not used: %#v", requested)
	}
	if len(fetched.Attempts) != 6 {
		t.Fatalf("attempts = %#v", fetched.Attempts)
	}
	var node subcanonical.Connection
	for _, connection := range fetched.Snapshot.Connections {
		if connection.Endpoint.Server == "edge.example.com" {
			node = connection
			break
		}
	}
	if node.DisplayName == "" {
		t.Fatalf("merged node missing: %#v", fetched.Snapshot.Connections)
	}
	if node.TLS.ServerName != "sni.example.com" {
		t.Fatalf("tls was not merged from json format: %#v", node.TLS)
	}
	if node.Transport.Type != "ws" || node.Transport.Path != "/ws" {
		t.Fatalf("transport was not merged from clash format: %#v", node.Transport)
	}
	if !hasString(node.Formats, subcanonical.FormatSingBox) || !hasString(node.Formats, subcanonical.FormatClash) || !hasString(node.Formats, subcanonical.FormatXray) {
		t.Fatalf("formats were not merged: %#v", node.Formats)
	}
}

func TestFetchSubscriptionMergesClientVariantsWithSameNamedXrayGroup(t *testing.T) {
	originalFetch := fetchSubscriptionData
	originalUserAgentFetch := fetchSubscriptionDataWithUserAgent
	t.Cleanup(func() {
		fetchSubscriptionData = originalFetch
		fetchSubscriptionDataWithUserAgent = originalUserAgentFetch
	})
	fetchSubscriptionData = func(rawURL string) (string, error) {
		return `{
  "outbounds": [
    {
      "type": "vless",
      "tag": "Auto Balancer",
      "server": "uri.example.com",
      "server_port": 443,
      "uuid": "11111111-1111-1111-1111-111111111111"
    }
  ]
}`, nil
	}
	fetchSubscriptionDataWithUserAgent = func(rawURL string, userAgent string) (string, error) {
		if userAgent != "v2rayNG/1.10.14" {
			return "", common.NewError("not available")
		}
		return `[
  {
    "remarks": "Auto Balancer",
    "outbounds": [
      {"tag": "proxy", "protocol": "vless", "settings": {"vnext": [{"address": "one.example.com", "port": 443, "users": [{"id": "22222222-2222-2222-2222-222222222222"}]}]}},
      {"tag": "proxy-2", "protocol": "vless", "settings": {"vnext": [{"address": "two.example.com", "port": 443, "users": [{"id": "33333333-3333-3333-3333-333333333333"}]}]}}
    ],
    "routing": {"balancers": [{"tag": "Balancer", "selector": ["proxy"], "strategy": {"type": "leastLoad"}}]}
  }
]`, nil
	}

	fetched, err := FetchSubscription("https://example.com/sub/secret")
	if err != nil {
		t.Fatal(err)
	}
	var profile subcanonical.Connection
	for _, connection := range fetched.Snapshot.Connections {
		if connection.DisplayName == "Auto Balancer" {
			profile = connection
			break
		}
	}
	if profile.Protocol != "urltest" || len(profile.GroupMembers) != 2 {
		t.Fatalf("same-named xray group should stay a runtime group: %#v", profile)
	}
	if !hasString(profile.Formats, subcanonical.FormatSingBox) || !hasString(profile.Formats, subcanonical.FormatXray) {
		t.Fatalf("profile did not retain both source formats: %#v", profile.Formats)
	}
	var xrayObservation subcanonical.Observation
	for _, observation := range profile.Observations {
		if observation.Format == subcanonical.FormatXray {
			xrayObservation = observation
			break
		}
	}
	if xrayObservation.Outbound == nil {
		t.Fatalf("xray profile observation missing: %#v", profile.Observations)
	}
	balancers, _ := xrayObservation.Outbound["xray_profile_balancers"].([]map[string]any)
	if len(balancers) != 1 {
		t.Fatalf("xray profile balancer data missing: %#v", xrayObservation.Outbound)
	}
	profileOutbound := subcanonical.ConnectionOutbound(profile)
	if profileOutbound["type"] != "urltest" || profileOutbound["xray_profile_outbounds"] != nil {
		t.Fatalf("runtime outbound should be clean urltest, got %#v", profileOutbound)
	}
}

func TestFetchSubscriptionSkipsDuplicateSuccessfulFormats(t *testing.T) {
	originalFetch := fetchSubscriptionData
	originalUserAgentFetch := fetchSubscriptionDataWithUserAgent
	t.Cleanup(func() {
		fetchSubscriptionData = originalFetch
		fetchSubscriptionDataWithUserAgent = originalUserAgentFetch
	})
	payload := `vless://11111111-1111-1111-1111-111111111111@edge.example.com:443?security=tls&sni=edge.example.com#Node%20A`
	fetchSubscriptionData = func(rawURL string) (string, error) {
		return payload, nil
	}
	fetchSubscriptionDataWithUserAgent = func(rawURL string, userAgent string) (string, error) {
		return payload, nil
	}

	fetched, err := FetchSubscription("https://example.com/sub/secret")
	if err != nil {
		t.Fatal(err)
	}
	if len(fetched.Snapshot.Connections) != 1 {
		t.Fatalf("duplicate URI variants created %d connections: %#v", len(fetched.Snapshot.Connections), fetched.Snapshot.Connections)
	}
	if len(fetched.Snapshot.Connections[0].Observations) != 1 {
		t.Fatalf("same-format variants should not be layered as observations: %#v", fetched.Snapshot.Connections[0].Observations)
	}
}

func TestMergeFetchedSubscriptionsDoesNotPromoteXrayProfilesOverOtherFormats(t *testing.T) {
	uriSnapshot := subcanonical.ObserveOutbounds(subcanonical.FormatURI, []map[string]any{
		{
			"type":        "vless",
			"tag":         "Profile A",
			"server":      "120",
			"server_port": 8443,
			"uuid":        "11111111-1111-1111-1111-111111111111",
		},
		{
			"type":        "vless",
			"tag":         "URI Only",
			"server":      "uri-only.example.com",
			"server_port": 8443,
			"uuid":        "22222222-2222-2222-2222-222222222222",
		},
	})
	clashSnapshot := subcanonical.ObserveOutbounds(subcanonical.FormatClash, []map[string]any{
		{
			"type":        "vless",
			"name":        "Profile A",
			"server":      "120",
			"server_port": 8443,
			"uuid":        "11111111-1111-1111-1111-111111111111",
			"udp":         true,
		},
		{
			"type":      "urltest",
			"tag":       "Обход",
			"outbounds": []string{"Profile A", "Profile B"},
			subcanonical.MetadataKey: map[string]any{
				"source_format":  subcanonical.FormatClash,
				"source_feature": "proxy-groups",
				"source_type":    "url-test",
				"target_type":    "urltest",
			},
		},
	})
	xraySnapshot := subcanonical.ObserveOutbounds(subcanonical.FormatXray, []map[string]any{
		{
			"type":              "vless",
			"tag":               "Profile A",
			"server":            "84.201.155.222",
			"server_port":       8443,
			"uuid":              "11111111-1111-1111-1111-111111111111",
			"xray_profile":      true,
			"xray_profile_type": "custom",
			subcanonical.MetadataKey: map[string]any{
				"source_format":  subcanonical.FormatXray,
				"source_feature": "custom.config",
				"source_type":    "custom",
				"target_type":    "vless",
			},
		},
		{
			"type":              "vless",
			"tag":               "Profile B",
			"server":            "84.201.155.223",
			"server_port":       8443,
			"uuid":              "33333333-3333-3333-3333-333333333333",
			"xray_profile":      true,
			"xray_profile_type": "custom",
			subcanonical.MetadataKey: map[string]any{
				"source_format":  subcanonical.FormatXray,
				"source_feature": "custom.config",
				"source_type":    "custom",
				"target_type":    "vless",
			},
		},
	})

	fetched := mergeFetchedSubscriptions(nil,
		&FetchedSubscription{Snapshot: uriSnapshot},
		&FetchedSubscription{Snapshot: clashSnapshot},
		&FetchedSubscription{Snapshot: xraySnapshot},
	)
	if len(fetched.Snapshot.Connections) != 4 || len(fetched.Outbounds) != 4 {
		t.Fatalf("top-level rows = connections:%d outbounds:%d, want neutral connection entities plus mihomo group: %#v", len(fetched.Snapshot.Connections), len(fetched.Outbounds), fetched.Snapshot.Connections)
	}
	if len(fetched.Snapshot.Extras) != 0 {
		t.Fatalf("extras = %#v, want mihomo proxy-group preserved as group connection", fetched.Snapshot.Extras)
	}
	if fetched.Snapshot.Connections[0].DisplayName != "Profile A" || fetched.Snapshot.Connections[1].DisplayName != "URI Only" {
		t.Fatalf("profiles = %#v", fetched.Snapshot.Connections)
	}
	if fetched.Snapshot.Connections[2].Kind != subcanonical.KindGroup || fetched.Snapshot.Connections[2].DisplayName != "Обход" {
		t.Fatalf("mihomo group should remain a group connection: %#v", fetched.Snapshot.Connections[2])
	}
	if !hasString(fetched.Snapshot.Connections[0].Formats, subcanonical.FormatURI) ||
		!hasString(fetched.Snapshot.Connections[0].Formats, subcanonical.FormatClash) ||
		!hasString(fetched.Snapshot.Connections[0].Formats, subcanonical.FormatXray) {
		t.Fatalf("same-name observations were not retained: %#v", fetched.Snapshot.Connections[0].Formats)
	}
	outbound := subcanonical.ConnectionOutbound(fetched.Snapshot.Connections[0])
	if outbound["server"] != "120" {
		t.Fatalf("runtime outbound should keep first successful format endpoint: %#v", outbound)
	}
}

func TestFetchSubscriptionRedactsURLSecretsFromErrors(t *testing.T) {
	originalFetch := fetchSubscriptionData
	originalUserAgentFetch := fetchSubscriptionDataWithUserAgent
	t.Cleanup(func() {
		fetchSubscriptionData = originalFetch
		fetchSubscriptionDataWithUserAgent = originalUserAgentFetch
	})
	fetchSubscriptionData = func(rawURL string) (string, error) {
		return "", common.NewError(`Get "`, rawURL, `": dial tcp token=raw-token`)
	}
	fetchSubscriptionDataWithUserAgent = func(rawURL string, userAgent string) (string, error) {
		return "", common.NewError(`Get "`, rawURL, `": dial tcp token=raw-token`)
	}

	_, err := FetchSubscription("https://example.com/sub/secret-id?token=abc&api_key=def")
	if err == nil {
		t.Fatal("expected fetch error")
	}
	message := err.Error()
	for _, forbidden := range []string{"secret-id", "token=abc", "api_key=def", "raw-token"} {
		if strings.Contains(message, forbidden) {
			t.Fatalf("fetch error leaked %q in %q", forbidden, message)
		}
	}
	if !strings.Contains(message, "https://example.com/[redacted]") {
		t.Fatalf("fetch error lost useful redacted host context: %q", message)
	}
}

func hasString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
