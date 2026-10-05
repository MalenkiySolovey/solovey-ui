package capabilities

import (
	"encoding/json"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
)

func TestCapabilitySemanticIdentityAndRuntimeContext(t *testing.T) {
	warp := Resolve("endpoints", "warp")
	wg := Resolve("endpoints", "wireguard")
	if !warp.Available || !wg.Available || warp.RuntimeType != wg.RuntimeType || warp.Type != "warp" {
		t.Fatalf("WARP projection: %+v %+v", warp, wg)
	}
	ep := model.Endpoint{Type: "warp", Tag: "semantic-final"}
	data, err := ep.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var runtime map[string]any
	if err := json.Unmarshal(data, &runtime); err != nil {
		t.Fatal(err)
	}
	if runtime["type"] != warp.RuntimeType || runtime["tag"] != ep.Tag || ep.Type != "warp" {
		t.Fatalf("identity/serializer mismatch: %s", data)
	}
	for _, entry := range [][2]string{{"outbounds", "wireguard"}, {"inbounds", "warp"}, {"bad-context", "warp"}} {
		if fact := Resolve(entry[0], entry[1]); !fact.Known || fact.ContextSupported || fact.Available || fact.Reason != "KNOWN_BUT_UNSUPPORTED_ENTITY_CONTEXT" {
			t.Fatalf("known type in wrong context must fail closed: %+v", fact)
		}
	}
	if fact := Resolve("endpoints", "unexpected"); fact.Known || fact.ContextSupported || fact.Available || fact.Reason != "UNKNOWN_ENTITY_TYPE" {
		t.Fatalf("unknown must stay unknown: %+v", fact)
	}
	if fact := Resolve("outbounds", "failover"); !fact.Available || fact.RuntimeType != "selector" {
		t.Fatalf("panel assembly mapping: %+v", fact)
	}
	one := Current()
	one.Facts[0].Type = "changed"
	if two := Current(); two.Schema != Schema || two.Facts[0].Type == "changed" {
		t.Fatal("snapshot mutation escaped")
	}
}
