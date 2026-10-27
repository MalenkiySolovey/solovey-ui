package tagrefs

import (
	"encoding/json"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/database/model"
)

func TestProjectedInboundGroupsRetainEagerCanonicalReferences(t *testing.T) {
	rows := ProjectionRows{Inbounds: []model.Inbound{
		{Id: 1, Type: "bond", Tag: "bundle", Options: json.RawMessage(`{"inbounds":["member"]}`)},
		{Id: 2, Type: "core-failover", Tag: "reconnect", Options: json.RawMessage(`{"inbounds":["other"]}`)},
	}}
	refs, err := ProjectedReferences(nil, rows, "inbounds", "member")
	if err != nil || len(refs) != 1 {
		t.Fatalf("group reference missing: %v, %v", refs, err)
	}
	if refs[0].Lazy || refs[0].Kind != "inbound group" || refs[0].Locator != `inbound "bundle" (inbounds)` {
		t.Fatal("group reference changed ownership or restart semantics")
	}
	refs, err = ProjectedReferences(nil, ProjectionRows{}, "inbounds", "member")
	if err != nil || len(refs) != 0 {
		t.Fatal("omitted historical group invented an active reference")
	}
}

func TestMalformedGroupCannotHideItsReferences(t *testing.T) {
	rows := []model.Inbound{{Type: "bond", Tag: "broken", Options: json.RawMessage(`{"inbounds":["member",null]}`)}}
	if _, err := inboundGroupReferences(rows, "member"); err == nil {
		t.Fatal("malformed group silently lost reference protection")
	}
}
