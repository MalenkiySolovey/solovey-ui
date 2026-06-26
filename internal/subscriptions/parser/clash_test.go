package parser

import (
	"testing"

	subcanonical "github.com/MalenkiySolovey/solovey-ui/internal/subscriptions/canonical"
)

func TestParseClashOutboundsMapsProxyFields(t *testing.T) {
	outbounds, err := ParseClashOutbounds(`
proxies:
  - name: Node A
    type: vless
    server: edge.example.com
    port: 443
    uuid: 11111111-1111-1111-1111-111111111111
    tls: true
    servername: sni.example.com
    client-fingerprint: chrome
    network: ws
    ws-opts:
      path: /ws
      headers:
        Host: cdn.example.com
  - name: SS
    type: ss
    server: ss.example.com
    port: 8388
    cipher: 2022-blake3-aes-128-gcm
    password: secret
proxy-groups:
  - name: Auto
    type: url-test
    proxies: [Node A, SS]
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(outbounds) != 3 {
		t.Fatalf("outbounds = %d, want 3", len(outbounds))
	}
	vless := outbounds[0]
	if vless["type"] != "vless" || vless["tag"] != "Node A" || vless["uuid"] == "" {
		t.Fatalf("vless outbound = %#v", vless)
	}
	tls, _ := vless["tls"].(map[string]interface{})
	if tls["server_name"] != "sni.example.com" {
		t.Fatalf("tls = %#v", tls)
	}
	transport, _ := vless["transport"].(map[string]interface{})
	headers, _ := transport["headers"].(map[string]interface{})
	if transport["type"] != "ws" || transport["path"] != "/ws" || headers["Host"] != "cdn.example.com" {
		t.Fatalf("transport = %#v", transport)
	}
	ss := outbounds[1]
	if ss["type"] != "shadowsocks" || ss["method"] != "2022-blake3-aes-128-gcm" || ss["password"] != "secret" {
		t.Fatalf("shadowsocks outbound = %#v", ss)
	}
	group := outbounds[2]
	if group["type"] != "urltest" || group["tag"] != "Auto" {
		t.Fatalf("proxy group = %#v", group)
	}
	refs, _ := group["outbounds"].([]string)
	if len(refs) != 2 || refs[0] != "Node A" || refs[1] != "SS" {
		t.Fatalf("group refs = %#v", group["outbounds"])
	}
	metadata, _ := group[subcanonical.MetadataKey].(map[string]interface{})
	if metadata["source_format"] != subcanonical.FormatClash || metadata["source_feature"] != "proxy-groups" || metadata["source_type"] != "url-test" || metadata["target_type"] != "urltest" {
		t.Fatalf("proxy group metadata = %#v", metadata)
	}
}

func TestParseClashOutboundsKeepsMihomoGroupTypesAsAdaptations(t *testing.T) {
	outbounds, err := ParseClashOutbounds(`
proxies:
  - name: Node A
    type: vless
    server: a.example.com
    port: 443
    uuid: 11111111-1111-1111-1111-111111111111
  - name: Node B
    type: trojan
    server: b.example.com
    port: 443
    password: secret
proxy-groups:
  - name: Failover
    type: fallback
    proxies: [Node A, Node B]
  - name: Balance
    type: load-balance
    strategy: round-robin
    proxies: [Node A, Node B]
  - name: Relay
    type: relay
    proxies: [Node A, Node B]
  - name: Smart
    type: smart
    proxies: [Node A, Node B]
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(outbounds) != 6 {
		t.Fatalf("outbounds = %d, want 2 proxies + 4 groups: %#v", len(outbounds), outbounds)
	}
	assertClashGroupAdaptation(t, outboundByTag(t, outbounds, "Failover"), "fallback", "urltest", "")
	assertClashGroupAdaptation(t, outboundByTag(t, outbounds, "Balance"), "load-balance", "urltest", "round-robin")
	assertClashGroupAdaptation(t, outboundByTag(t, outbounds, "Relay"), "relay", "selector", "")
	assertClashGroupAdaptation(t, outboundByTag(t, outbounds, "Smart"), "smart", "urltest", "")
}

func TestParseClashOutboundsDoesNotCreateDisabledTLSFromFingerprintOnly(t *testing.T) {
	outbounds, err := ParseClashOutbounds(`
proxies:
  - name: Fingerprint Only
    type: vless
    server: edge.example.com
    port: 443
    uuid: 11111111-1111-1111-1111-111111111111
    client-fingerprint: chrome
proxy-groups:
  - name: Balance
    type: load-balance
    proxies: [Fingerprint Only]
`)
	if err != nil {
		t.Fatal(err)
	}
	if tls := outboundByTag(t, outbounds, "Fingerprint Only")["tls"]; tls != nil {
		t.Fatalf("fingerprint-only proxy should not create sing-box tls block: %#v", tls)
	}
}

func TestParseClashOutboundsEnablesTLSWhenFingerprintHasTLSSignal(t *testing.T) {
	outbounds, err := ParseClashOutbounds(`
proxies:
  - name: Reality
    type: vless
    server: edge.example.com
    port: 443
    uuid: 11111111-1111-1111-1111-111111111111
    servername: sni.example.com
    client-fingerprint: chrome
    reality-opts:
      public-key: public-key
      short-id: short-id
proxy-groups:
  - name: Balance
    type: load-balance
    proxies: [Reality]
`)
	if err != nil {
		t.Fatal(err)
	}
	tls, _ := outboundByTag(t, outbounds, "Reality")["tls"].(map[string]interface{})
	if tls["enabled"] != true || tls["server_name"] != "sni.example.com" || tls["utls"] == nil || tls["reality"] == nil {
		t.Fatalf("tls with reality/fingerprint = %#v", tls)
	}
}

func TestParseClashOutboundsKeepsSelectGroupsAsAuxiliaryProfileData(t *testing.T) {
	outbounds, err := ParseClashOutbounds(`
proxies:
  - name: Node A
    type: vless
    server: a.example.com
    port: 443
    uuid: 11111111-1111-1111-1111-111111111111
proxy-groups:
  - name: Main
    type: select
    proxies: [Node A]
`)
	if err != nil {
		t.Fatal(err)
	}
	group := outboundByTag(t, outbounds, "Main")
	if group["_subscription_auxiliary"] != true {
		t.Fatalf("select group should be auxiliary profile data: %#v", group)
	}
	snapshot := subcanonical.ObserveOutbounds(subcanonical.FormatClash, outbounds)
	if len(snapshot.Connections) != 1 || snapshot.Connections[0].DisplayName != "Node A" {
		t.Fatalf("select group leaked into connections: %#v", snapshot.Connections)
	}
	if len(snapshot.Extras) != 1 || snapshot.Extras[0].Name != "Main" {
		t.Fatalf("select group was not preserved as extra metadata: %#v", snapshot.Extras)
	}
}

func TestParseClashOutboundsUsesGroupAdaptationPolicy(t *testing.T) {
	outbounds, err := ParseClashOutboundsWithOptions(`
proxies:
  - name: Node A
    type: vless
    server: a.example.com
    port: 443
    uuid: 11111111-1111-1111-1111-111111111111
proxy-groups:
  - name: Failover
    type: fallback
    proxies: [Node A]
`, ParseOptions{GroupAdaptation: GroupAdaptationFailover})
	if err != nil {
		t.Fatal(err)
	}
	group := outboundByTag(t, outbounds, "Failover")
	if group["type"] != "failover" || group["default"] != "Node A" {
		t.Fatalf("adapted fallback group = %#v", group)
	}
	assertClashGroupAdaptation(t, group, "fallback", "failover", "")
}

func outboundByTag(t *testing.T, outbounds []map[string]interface{}, tag string) map[string]interface{} {
	t.Helper()
	for _, outbound := range outbounds {
		if outbound["tag"] == tag {
			return outbound
		}
	}
	t.Fatalf("outbound %q not found in %#v", tag, outbounds)
	return nil
}

func assertClashGroupAdaptation(t *testing.T, outbound map[string]interface{}, sourceType string, targetType string, strategy string) {
	t.Helper()
	if outbound["type"] != targetType {
		t.Fatalf("%s group target type = %#v", sourceType, outbound)
	}
	metadata, _ := outbound[subcanonical.MetadataKey].(map[string]interface{})
	if metadata["source_format"] != subcanonical.FormatClash ||
		metadata["source_feature"] != "proxy-groups" ||
		metadata["source_type"] != sourceType ||
		metadata["target_type"] != targetType ||
		metadata["strategy"] != strategy {
		t.Fatalf("%s group metadata = %#v", sourceType, metadata)
	}
}
