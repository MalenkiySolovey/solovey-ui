package parser

import (
	"reflect"
	"testing"

	subcanonical "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/canonical"
)

func TestParseXrayOutboundsMapsBalancerToGroupWithScopedDependency(t *testing.T) {
	outbounds, err := ParseXrayOutbounds(`{
  "remarks": "Auto Balancer",
  "outbounds": [
    {
      "tag": "proxy-a",
      "protocol": "vless",
      "settings": {
        "vnext": [{
          "address": "edge.example.com",
          "port": 443,
          "users": [{"id": "11111111-1111-1111-1111-111111111111", "flow": "xtls-rprx-vision"}]
        }]
      },
      "streamSettings": {
        "network": "ws",
        "security": "reality",
        "wsSettings": {"path": "/ws", "headers": {"Host": "cdn.example.com"}},
        "realitySettings": {
          "serverName": "sni.example.com",
          "publicKey": "pub",
          "shortId": "sid",
          "fingerprint": "chrome"
        }
      }
    }
  ],
  "routing": {
    "balancers": [{
      "tag": "auto",
      "selector": ["proxy"],
      "fallbackTag": "proxy-a",
      "strategy": {"type": "leastPing"}
    }]
  }
}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(outbounds) != 2 {
		t.Fatalf("outbounds = %d, want group + dependency: %#v", len(outbounds), outbounds)
	}

	group := outbounds[0]
	if group["type"] != "urltest" || group["tag"] != "Auto Balancer" || group["xray_tag"] != "auto" {
		t.Fatalf("group = %#v", group)
	}
	refs, _ := group["outbounds"].([]string)
	if !reflect.DeepEqual(refs, []string{"Auto Balancer / proxy-a"}) {
		t.Fatalf("group refs = %#v", group["outbounds"])
	}
	if _, leaked := group["default"]; leaked {
		t.Fatalf("urltest runtime must not receive xray fallback as default: %#v", group)
	}
	if group["xray_profile"] != true || group["xray_profile_type"] != "balancer" {
		t.Fatalf("group profile metadata = %#v", group)
	}
	balancers, _ := group["xray_profile_balancers"].([]map[string]interface{})
	if len(balancers) != 1 || balancers[0]["tag"] != "auto" || balancers[0]["fallback_member"] != "Auto Balancer / proxy-a" || balancers[0]["strategy"] != "leastPing" {
		t.Fatalf("balancer metadata = %#v", balancers)
	}
	metadata, _ := group[subcanonical.MetadataKey].(map[string]interface{})
	if metadata["source_format"] != subcanonical.FormatXray || metadata["source_feature"] != "routing.balancer" || metadata["source_type"] != "balancer" || metadata["target_type"] != "urltest" {
		t.Fatalf("group metadata = %#v", metadata)
	}

	member := outbounds[1]
	if member["type"] != "vless" || member["tag"] != "Auto Balancer / proxy-a" || member["xray_tag"] != "proxy-a" || member["server"] != "edge.example.com" {
		t.Fatalf("member = %#v", member)
	}
	tls, _ := member["tls"].(map[string]interface{})
	reality, _ := tls["reality"].(map[string]interface{})
	if tls["server_name"] != "sni.example.com" || reality["public_key"] != "pub" || reality["short_id"] != "sid" {
		t.Fatalf("member tls = %#v", tls)
	}
	transport, _ := member["transport"].(map[string]interface{})
	headers, _ := transport["headers"].(map[string]interface{})
	if transport["type"] != "ws" || transport["path"] != "/ws" || headers["Host"] != "cdn.example.com" {
		t.Fatalf("member transport = %#v", transport)
	}
}

func TestParseXrayBalancerSelectorExpandsPrefixMembers(t *testing.T) {
	outbounds, err := ParseXrayOutbounds(`{
  "remarks": "Balancer Profile",
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
	if len(outbounds) != 4 {
		t.Fatalf("outbounds = %d, want group + 3 dependencies", len(outbounds))
	}
	refs, _ := outbounds[0]["outbounds"].([]string)
	if !reflect.DeepEqual(refs, []string{"Balancer Profile / proxy", "Balancer Profile / proxy-2", "Balancer Profile / proxy-3"}) {
		t.Fatalf("group refs = %#v", refs)
	}
	balancers, _ := outbounds[0]["xray_profile_balancers"].([]map[string]interface{})
	if len(balancers) != 1 || !reflect.DeepEqual(balancers[0]["members"], refs) {
		t.Fatalf("balancer members = %#v", balancers)
	}
	if balancers[0]["fallback_tag"] != "direct" {
		t.Fatalf("fallback tag should remain as xray metadata: %#v", balancers[0])
	}
}

func TestParseXrayOutboundsKeepsMultiBalancerConfigAsSingleProfile(t *testing.T) {
	outbounds, err := ParseXrayOutbounds(`{
  "remarks": "Multi Balancer",
  "outbounds": [
    {"tag": "primary", "protocol": "vless", "settings": {"vnext": [{"address": "one.example.com", "port": 443, "users": [{"id": "11111111-1111-1111-1111-111111111111"}]}]}},
    {"tag": "backup", "protocol": "vless", "settings": {"vnext": [{"address": "two.example.com", "port": 443, "users": [{"id": "22222222-2222-2222-2222-222222222222"}]}]}}
  ],
  "routing": {
    "balancers": [
      {"tag": "auto-primary", "selector": ["primary"]},
      {"tag": "auto-backup", "selector": ["backup"]}
    ]
  }
}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(outbounds) != 3 {
		t.Fatalf("outbounds = %d, want one profile + two dependencies: %#v", len(outbounds), outbounds)
	}
	profile := outbounds[0]
	if profile["tag"] != "Multi Balancer" || profile["type"] != "urltest" {
		t.Fatalf("profile = %#v", profile)
	}
	balancers, _ := profile["xray_profile_balancers"].([]map[string]interface{})
	if len(balancers) != 2 || balancers[0]["tag"] != "auto-primary" || balancers[1]["tag"] != "auto-backup" {
		t.Fatalf("balancer metadata = %#v", balancers)
	}
}

func TestParseXrayOutboundsSupportsConfigArrayWithScopedDependencies(t *testing.T) {
	outbounds, err := ParseXrayOutbounds(`[
  {
    "remarks": "Group A",
    "outbounds": [
      {"tag": "proxy", "protocol": "vless", "settings": {"vnext": [{"address": "one.example.com", "port": 443, "users": [{"id": "11111111-1111-1111-1111-111111111111"}]}]}},
      {"tag": "proxy-2", "protocol": "vless", "settings": {"vnext": [{"address": "two.example.com", "port": 443, "users": [{"id": "22222222-2222-2222-2222-222222222222"}]}]}}
    ],
    "routing": {"balancers": [{"tag": "Balancer", "selector": ["proxy"], "strategy": {"type": "leastLoad"}}]}
  },
  {
    "remarks": "Group B",
    "outbounds": [
      {"tag": "proxy", "protocol": "vless", "settings": {"vnext": [{"address": "three.example.com", "port": 443, "users": [{"id": "33333333-3333-3333-3333-333333333333"}]}]}},
      {"tag": "proxy-2", "protocol": "vless", "settings": {"vnext": [{"address": "four.example.com", "port": 443, "users": [{"id": "44444444-4444-4444-4444-444444444444"}]}]}}
    ],
    "routing": {"balancers": [{"tag": "Balancer", "selector": ["proxy"], "strategy": {"type": "leastLoad"}}]}
  }
]`)
	if err != nil {
		t.Fatal(err)
	}
	if len(outbounds) != 6 {
		t.Fatalf("outbounds = %d, want two groups + four dependencies: %#v", len(outbounds), outbounds)
	}
	if outbounds[0]["tag"] != "Group A" || outbounds[1]["tag"] != "Group B" {
		t.Fatalf("group tags = %#v / %#v", outbounds[0], outbounds[1])
	}
	firstRefs, _ := outbounds[0]["outbounds"].([]string)
	secondRefs, _ := outbounds[1]["outbounds"].([]string)
	if !reflect.DeepEqual(firstRefs, []string{"Group A / proxy", "Group A / proxy-2"}) ||
		!reflect.DeepEqual(secondRefs, []string{"Group B / proxy", "Group B / proxy-2"}) {
		t.Fatalf("scoped refs = %#v / %#v", firstRefs, secondRefs)
	}
	for _, dependency := range outbounds[2:] {
		if dependency["xray_profile_member"] != true {
			t.Fatalf("dependency is not marked as profile member: %#v", dependency)
		}
	}
}

func TestParseXrayOutboundsSupportsConcatenatedConfigs(t *testing.T) {
	outbounds, err := ParseXrayOutbounds(`{
  "remarks": "Concat A",
  "outbounds": [
    {"tag": "proxy", "protocol": "vless", "settings": {"vnext": [{"address": "one.example.com", "port": 443, "users": [{"id": "11111111-1111-1111-1111-111111111111"}]}]}}
  ],
  "routing": {"balancers": [{"tag": "Balancer", "selector": ["proxy"]}]}
}
{
  "remarks": "Concat B",
  "outbounds": [
    {"tag": "proxy", "protocol": "vless", "settings": {"vnext": [{"address": "two.example.com", "port": 443, "users": [{"id": "22222222-2222-2222-2222-222222222222"}]}]}}
  ],
  "routing": {"balancers": [{"tag": "Balancer", "selector": ["proxy"]}]}
}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(outbounds) != 4 {
		t.Fatalf("outbounds = %d, want two groups + two dependencies: %#v", len(outbounds), outbounds)
	}
	if outbounds[0]["tag"] != "Concat A" || outbounds[1]["tag"] != "Concat B" {
		t.Fatalf("concat groups = %#v", outbounds)
	}
}

func TestParseXrayOutboundsKeepsCustomConfigWithoutBalancerAsSingleProfile(t *testing.T) {
	outbounds, err := ParseXrayOutbounds(`{
  "remarks": "Custom Profile",
  "outbounds": [
    {
      "tag": "proxy-xhttp",
      "protocol": "vless",
      "settings": {"vnext": [{"address": "xhttp.example.com", "port": 443, "users": [{"id": "11111111-1111-1111-1111-111111111111"}]}]},
      "streamSettings": {
        "network": "xhttp",
        "security": "tls",
        "tlsSettings": {"serverName": "sni.example.com"},
        "xhttpSettings": {"path": "/", "host": "edge.example.com", "mode": "auto"}
      }
    }
  ]
}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(outbounds) != 1 {
		t.Fatalf("outbounds = %d, want one custom profile", len(outbounds))
	}
	if outbounds[0]["tag"] != "Custom Profile" || outbounds[0]["xray_profile_type"] != "custom" {
		t.Fatalf("custom profile = %#v", outbounds[0])
	}
	transport, _ := outbounds[0]["transport"].(map[string]interface{})
	if transport["type"] != "httpupgrade" || transport["path"] != "/" || transport["host"] != "edge.example.com" {
		t.Fatalf("transport = %#v", transport)
	}
	members, _ := outbounds[0]["xray_profile_outbounds"].([]map[string]interface{})
	metadata, _ := members[0][subcanonical.MetadataKey].(map[string]interface{})
	if metadata["source_feature"] != "streamSettings" || metadata["source_type"] != "xhttp" || metadata["target_type"] != "httpupgrade" {
		t.Fatalf("xhttp member metadata = %#v", metadata)
	}
}

func TestParseXrayOutboundsSkipsUnsupportedTransport(t *testing.T) {
	_, err := ParseXrayOutbounds(`{
  "outbounds": [
    {
      "tag": "mkcp-node",
      "protocol": "vless",
      "settings": {"vnext": [{"address": "edge.example.com", "port": 443, "users": [{"id": "11111111-1111-1111-1111-111111111111"}]}]},
      "streamSettings": {"network": "kcp"}
    }
  ]
}`)
	if err == nil {
		t.Fatal("expected unsupported transport to produce no result")
	}
}

func TestParseXrayOutboundsUsesGroupAdaptationPolicy(t *testing.T) {
	outbounds, err := ParseXrayOutboundsWithOptions(`{
  "outbounds": [
    {
      "tag": "proxy-a",
      "protocol": "vless",
      "settings": {"vnext": [{"address": "edge.example.com", "port": 443, "users": [{"id": "11111111-1111-1111-1111-111111111111"}]}]}
    }
  ],
  "routing": {"balancers": [{"tag": "auto", "selector": ["proxy"]}]}
}`, ParseOptions{GroupAdaptation: GroupAdaptationFailover})
	if err != nil {
		t.Fatal(err)
	}
	if len(outbounds) != 2 {
		t.Fatalf("outbounds = %d, want group + dependency", len(outbounds))
	}
	group := outbounds[0]
	if group["type"] != "failover" || group["default"] != "proxy-a" {
		t.Fatalf("adapted group = %#v", group)
	}
	balancers, _ := group["xray_profile_balancers"].([]map[string]interface{})
	if len(balancers) != 1 || balancers[0]["target_type"] != "failover" {
		t.Fatalf("adapted balancer metadata = %#v", balancers)
	}
}
