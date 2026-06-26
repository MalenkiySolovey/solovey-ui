package local

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
)

func TestBuildInboundOutboundsSplitsMixed(t *testing.T) {
	set, err := BuildInboundOutbounds(json.RawMessage(`{}`), []*model.Inbound{{
		Type:    "mixed",
		Tag:     "mixed-in",
		Addrs:   json.RawMessage(`[]`),
		OutJson: json.RawMessage(`{"type":"mixed","tag":"mixed-node","server":"127.0.0.1","server_port":1080}`),
		Options: json.RawMessage(`{}`),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(set.Tags, []string{"mixed-node-socks", "mixed-node-http"}) {
		t.Fatalf("tags = %#v", set.Tags)
	}
	if set.Outbounds[0]["type"] != "socks" || set.Outbounds[1]["type"] != "http" {
		t.Fatalf("mixed outbounds = %#v", set.Outbounds)
	}
}

func TestBuildInboundOutboundsStripsVLESSFlowForNonTCP(t *testing.T) {
	set, err := BuildInboundOutbounds(json.RawMessage(`{
		"vless": {"uuid":"11111111-1111-4111-8111-111111111111","flow":"xtls-rprx-vision"}
	}`), []*model.Inbound{{
		Type:    "vless",
		Tag:     "vless-in",
		TlsId:   1,
		Addrs:   json.RawMessage(`[]`),
		OutJson: json.RawMessage(`{"type":"vless","tag":"vless-node","server":"example.com","server_port":443,"transport":{"type":"grpc"}}`),
		Options: json.RawMessage(`{}`),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := set.Outbounds[0]["flow"]; ok {
		t.Fatalf("flow should be stripped for non-TCP transport: %#v", set.Outbounds[0])
	}
	if set.Outbounds[0]["uuid"] != "11111111-1111-4111-8111-111111111111" {
		t.Fatalf("uuid was not merged: %#v", set.Outbounds[0])
	}
}

func TestPrependDefaultJSONOutbounds(t *testing.T) {
	set := &OutboundSet{
		Outbounds: []map[string]interface{}{{"type": "vless", "tag": "node-a"}},
		Tags:      []string{"node-a"},
	}
	PrependDefaultJSONOutbounds(set)
	if len(set.Outbounds) != 4 {
		t.Fatalf("outbound count = %d", len(set.Outbounds))
	}
	selector := set.Outbounds[0]
	if selector["type"] != "selector" || selector["tag"] != "proxy" {
		t.Fatalf("first outbound = %#v", selector)
	}
	refs, ok := selector["outbounds"].([]string)
	if !ok || !reflect.DeepEqual(refs, []string{"auto", "direct", "node-a"}) {
		t.Fatalf("selector refs = %#v", selector["outbounds"])
	}
}

func TestAppendExternalLinkOutboundsNumbersOnlyWhenMultipleLinks(t *testing.T) {
	set := &OutboundSet{}
	AppendExternalLinkOutbounds(set, []string{
		"trojan://pass@example-a.com:443#node",
		"trojan://pass@example-b.com:443#node",
	})
	if !reflect.DeepEqual(set.Tags, []string{"1.node", "2.node"}) {
		t.Fatalf("tags = %#v", set.Tags)
	}
}
