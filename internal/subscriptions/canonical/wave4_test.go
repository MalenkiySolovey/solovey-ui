package canonical

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/sagernet/sing-quic/hysteria"
)

func TestNamedOutboundsPreservesAbsentObjects(t *testing.T) {
	input := []map[string]any{nil, {"tag": "node", "detour": "node"}}
	result, names := NamedOutbounds(input)
	if result[0] != nil || result[1]["tag"] != "node" || names[1] != "node" || result[1]["detour"] != "node" {
		t.Fatal("absent metadata changed valid graph names or became an invented object")
	}
}

func TestHysteriaPortsMatchPinnedRuntimeParser(t *testing.T) {
	ports, err := HysteriaPorts([]any{"1", float64(443), "8443:8445", "65535:"})
	if err != nil {
		t.Fatal(err)
	}
	expanded, err := hysteria.ParsePorts(ports)
	if err != nil || !reflect.DeepEqual(expanded, []uint16{1, 443, 8443, 8444, 8445, 65535}) {
		t.Fatalf("pinned ParsePorts mismatch: %v %v", expanded, err)
	}
	for _, value := range []any{0, 65536, 1.5, true, []any{nil}, []string{"1", ""}, "2:1", "0:1", ":443", "443-444", strings.Repeat("1,", 65)} {
		if _, err := HysteriaPorts(value); err == nil {
			t.Fatal("malformed or nonportable range accepted")
		}
	}
	for _, empty := range []any{nil, "", []string{}, []any{}} {
		if ports, err := HysteriaPorts(empty); err != nil || len(ports) != 0 {
			t.Fatal("empty range changed defaults")
		}
	}
}

func TestNamingKeepsCredentialsLiteralSuffixesAndReferences(t *testing.T) {
	source := []map[string]any{{"tag": "node", "password": "a"}, {"tag": "node", "password": "b"}, {"tag": "node-2", "password": "c"}, {"tag": "proxy", "type": "selector", "outbounds": []string{"node", "node-2"}, "default": "node", "detour": "node-2"}}
	before, _ := json.Marshal(source)
	outbounds, names := NamedOutbounds(source, "proxy", "auto", "direct")
	if !reflect.DeepEqual(names, []string{"node", "node-3", "node-2", "proxy-2"}) {
		t.Fatalf("names=%v", names)
	}
	if outbounds[0]["password"] != "a" || outbounds[1]["password"] != "b" || outbounds[2]["password"] != "c" {
		t.Fatal("colliding credentials changed or disappeared")
	}
	if !reflect.DeepEqual(outbounds[3]["outbounds"], []string{"node", "node-2"}) {
		t.Fatal("group references changed binding")
	}
	after, _ := json.Marshal(source)
	if string(before) != string(after) {
		t.Fatal("projection mutated source")
	}
	repeated, _ := NamedOutbounds(source, "proxy", "auto", "direct")
	if !reflect.DeepEqual(outbounds, repeated) {
		t.Fatal("allocation is not deterministic")
	}
}

func TestPublicRemarkMetadataValidationAndRetention(t *testing.T) {
	stored := json.RawMessage(`{"trojan":{"password":"fixture"},"_subscription":{"publicRemark":"Public 雪"}}`)
	retained, err := RetainClientPublicMetadata(json.RawMessage(`{"trojan":{"password":"changed"}}`), stored)
	if err != nil {
		t.Fatal(err)
	}
	if remark, err := ClientPublicRemark(retained); err != nil || remark != "Public 雪" {
		t.Fatal("legacy partial save lost public remark")
	}
	cleared, err := RetainClientPublicMetadata(json.RawMessage(`{"_subscription":{}}`), stored)
	if err != nil {
		t.Fatal(err)
	}
	if remark, err := ClientPublicRemark(cleared); err != nil || remark != "" {
		t.Fatal("explicit clear lost authority")
	}
	for _, raw := range []string{`{"_subscription":null}`, `{"_subscription":{"publicRemark":null}}`, `{"_subscription":{"publicRemark":17}}`, `{"_subscription":{"publicRemark":"bad\nname"}}`, `{"_subscription":{"publicRemark":"` + strings.Repeat("x", 129) + `"}}`} {
		if _, err := ClientPublicRemark(json.RawMessage(raw)); err == nil {
			t.Fatal("invalid public metadata accepted")
		}
	}
}
