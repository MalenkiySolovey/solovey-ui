package capabilityview

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/core/registry/facts"
	providerfixture "github.com/MalenkiySolovey/solovey-ui/testsupport/coreprovider"
)

func diffFixture(t *testing.T, provider string, entries ...Fact) Snapshot {
	t.Helper()
	return Snapshot{Schema: Schema, ComponentProfile: "full", Provider: providerfixture.Identity(t, provider), Facts: entries}
}

func supportedFact(kind string) Fact {
	return Fact{Fact: facts.Fact{Category: "outbounds", Type: kind, Known: true, Registered: true, Compiled: true, SupportedByProduct: true},
		RuntimeType: kind, ContextSupported: true}
}

func TestCapabilityDifferencePreservesUnknownAndExplicitLoss(t *testing.T) {
	before := diffFixture(t, "shtorm-extended", supportedFact("direct"), supportedFact("native"))
	denied := supportedFact("native")
	denied.Compiled, denied.Reason = false, "PROTOCOL_NOT_COMPILED"
	after := diffFixture(t, "sing-box", supportedFact("direct"), supportedFact("http"), denied)
	beforeBytes, _ := json.Marshal(before)
	afterBytes, _ := json.Marshal(after)
	difference, err := Compare(before, after)
	if err != nil || len(difference.Changes) != 2 {
		t.Fatal("owner facts did not produce a bounded union comparison", err)
	}
	added, lost := difference.Changes[0], difference.Changes[1]
	if added.Type != "http" || added.Before != Unknown || added.After != Supported || added.BeforeReason != "CAPABILITY_OBSERVATION_UNAVAILABLE" ||
		lost.Type != "native" || lost.Before != Supported || lost.After != Unsupported || lost.AfterReason != "PROTOCOL_NOT_COMPILED" {
		t.Fatal("missing or unsupported capability was promoted or hidden")
	}
	beforeAgain, _ := json.Marshal(before)
	afterAgain, _ := json.Marshal(after)
	if string(beforeBytes) != string(beforeAgain) || string(afterBytes) != string(afterAgain) {
		t.Fatal("comparison mutated owner evidence")
	}
	reverse, err := Compare(after, before)
	if err != nil || reverse.Changes[0].Before != Supported || reverse.Changes[0].After != Unknown ||
		reverse.Changes[1].Before != Unsupported || reverse.Changes[1].After != Supported {
		t.Fatal("reverse comparison lost unknown/downgrade semantics", err)
	}
	unchanged, err := Compare(before, before)
	if err != nil || unchanged.Changes == nil || len(unchanged.Changes) != 0 {
		t.Fatal("unchanged observation invented capability changes", err)
	}
	for name, mutate := range map[string]func(*Difference){
		"source":                 func(d *Difference) { d.FromRevision = strings.Repeat("a", 64) },
		"target":                 func(d *Difference) { d.ToRevision = strings.Repeat("a", 64) },
		"missing":                func(d *Difference) { d.Changes = nil },
		"ambiguous-state":        func(d *Difference) { d.Changes[0].Before = "false" },
		"unknown-without-reason": func(d *Difference) { d.Changes[0].BeforeReason = "" },
		"duplicate":              func(d *Difference) { d.Changes = append(d.Changes, d.Changes[len(d.Changes)-1]) },
		"order":                  func(d *Difference) { d.Changes[0], d.Changes[1] = d.Changes[1], d.Changes[0] },
	} {
		t.Run(name, func(t *testing.T) {
			changed := difference
			changed.Changes = append([]Change(nil), difference.Changes...)
			mutate(&changed)
			if changed.Validate(difference.FromRevision, difference.ToRevision) == nil {
				t.Fatal("incomplete or ambiguous capability comparison was accepted")
			}
		})
	}
}

func TestCapabilityDifferenceDoesNotInferUnobservedSupport(t *testing.T) {
	before := diffFixture(t, "sing-box", supportedFact("direct"))
	after := diffFixture(t, "shtorm-extended", supportedFact("direct"))
	difference, err := Compare(before, after)
	if err != nil || len(difference.Changes) != 0 || difference.FromRevision == difference.ToRevision {
		t.Fatal("provider identity invented a semantic capability", err)
	}
	after.ComponentProfile = "core"
	if _, err := Compare(before, after); err == nil {
		t.Fatal("comparison accepted a profile transition outside this maintenance contract")
	}
}
