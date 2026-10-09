package capabilities

import (
	"reflect"
	"testing"
)

func TestCapturedSnapshotMatchesCurrentOwnerAndFailsClosed(t *testing.T) {
	environment := Environment{InterfacesAvailable: true, Interfaces: []Interface{{Name: "fixture", Eligible: true}}, SystemBusAvailable: true, Resolve1Name: Resolve1NameUnclaimed}
	snapshot := CurrentWithEnvironment(environment)
	for _, fact := range snapshot.Facts {
		if captured := snapshot.Resolve(fact.Category, fact.Type); !reflect.DeepEqual(captured, ResolveWithEnvironment(fact.Category, fact.Type, environment)) {
			t.Fatalf("captured target disagrees for %s/%s", fact.Category, fact.Type)
		}
	}
	if snapshot.Resolve("inbounds", "warp").Reason != "KNOWN_BUT_UNSUPPORTED_ENTITY_CONTEXT" || snapshot.Resolve("endpoints", "unknown").Reason != "UNKNOWN_ENTITY_TYPE" {
		t.Fatal("captured target lost identity classification")
	}
	snapshot.Schema = "unrecognized"
	if fact := snapshot.Resolve("outbounds", "direct"); fact.Available || fact.Reason != "CAPABILITY_SNAPSHOT_UNAVAILABLE" {
		t.Fatal("unrecognized target schema authorized a type")
	}
}

func TestCapturedTargetCannotInventSchemaOrEntityContext(t *testing.T) {
	for _, identity := range []struct{ category, panelType, reason string }{
		{"inbounds", "invented", "UNKNOWN_ENTITY_TYPE"},
		{"inbounds", "warp", "KNOWN_BUT_UNSUPPORTED_ENTITY_CONTEXT"},
	} {
		fact := Resolve(identity.category, identity.panelType)
		fact.Known, fact.ContextSupported, fact.Registered, fact.Compiled, fact.Available = true, true, true, true, true
		fact.Reason = ""
		snapshot := Snapshot{Schema: Schema, ComponentProfile: "full", Facts: []Fact{fact}}
		if resolved := snapshot.Resolve(identity.category, identity.panelType); resolved.Available || resolved.Reason != identity.reason {
			t.Fatal("target snapshot invented an entity schema or context")
		}
	}
	// A legitimate target may compile an implementation absent in this process.
	fact := Resolve("inbounds", "hysteria2")
	fact.Registered, fact.Compiled, fact.Available, fact.Reason = true, true, true, ""
	snapshot := Snapshot{Schema: Schema, ComponentProfile: "full", Facts: []Fact{fact}}
	if !snapshot.Resolve("inbounds", "hysteria2").Available {
		t.Fatal("trusted target lost its supported build difference")
	}
}
