package local

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
)

func TestPublicProjectionRetainsDuplicateCredentialsAndDefaults(t *testing.T) {
	set := &OutboundSet{}
	set.AppendMany([]map[string]any{{"type": "trojan", "tag": "proxy", "password": "a"}, {"type": "trojan", "tag": "proxy", "password": "b"}, {"type": "selector", "tag": "group", "outbounds": []string{"proxy"}, "default": "proxy"}}, []string{"proxy", "proxy", "group"})
	PrependDefaultJSONOutbounds(set)
	if len(set.Outbounds) != 6 || !reflect.DeepEqual(set.Tags, []string{"proxy-3", "proxy-2", "group"}) {
		t.Fatalf("duplicate/default names=%v count=%d", set.Tags, len(set.Outbounds))
	}
	if set.Outbounds[3]["password"] != "a" || set.Outbounds[4]["password"] != "b" {
		t.Fatal("colliding credential was discarded")
	}
	if !reflect.DeepEqual(set.Outbounds[5]["outbounds"], []string{"proxy-3"}) || set.Outbounds[5]["default"] != "proxy-3" {
		t.Fatal("default collision broke references")
	}
}

func TestPublicRemarkAndAddressTLSDoNotMutateSourceOrLeakPrivateNames(t *testing.T) {
	inbound := &model.Inbound{Type: "trojan", Tag: "inbound", Options: json.RawMessage(`{}`), OutJson: json.RawMessage(`{"type":"trojan","tag":"legacy","server":"example.com","server_port":443,"tls":{"enabled":true,"server_name":"base.example"}}`), Addrs: json.RawMessage(`[{"server":"[2001:db8::1]","server_port":443,"tls":{"server_name":"one.example"}},{"server":"two.example","server_port":443}]`)}
	config := json.RawMessage(`{"trojan":{"name":"private-auth","password":"fixture"},"_subscription":{"publicRemark":"Public"}}`)
	before := string(inbound.OutJson)
	set, err := BuildInboundOutbounds(config, []*model.Inbound{inbound})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(set.Tags, []string{"Public", "Public-2"}) {
		t.Fatal("public remark not applied")
	}
	if set.Outbounds[0]["server"] != "2001:db8::1" || set.Outbounds[1]["tls"].(map[string]any)["server_name"] != "base.example" {
		t.Fatal("address projection leaked another address's TLS override")
	}
	raw, _ := json.Marshal(set.Outbounds)
	if strings.Contains(string(raw), "private-auth") || strings.Contains(string(raw), "_subscription") {
		t.Fatal("private name or metadata object entered public output")
	}
	if string(inbound.OutJson) != before {
		t.Fatal("projection mutated stored source")
	}
	legacy, err := BuildInboundOutbounds(json.RawMessage(`{"trojan":{"password":"fixture"}}`), []*model.Inbound{inbound})
	if err != nil || !reflect.DeepEqual(legacy.Tags, []string{"1.legacy", "2.legacy"}) {
		t.Fatal("absent public metadata changed existing labels")
	}
}

func TestLinkResolverSkipsVisibleUnsupportedDeliveryDiagnostics(t *testing.T) {
	links := json.RawMessage(`[{"type":"local","uri":"","diagnostic":"unsupported format"},{"type":"local","uri":"trojan://fixture@example.com:443#node"}]`)
	if resolved := ResolveClientLinksWithFetcher(links, LinkModeAll, "", nil); len(resolved) != 1 {
		t.Fatal("empty diagnostic was emitted as a subscription URI")
	}
}
