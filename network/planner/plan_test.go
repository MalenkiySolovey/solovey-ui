package planner

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/hostref"
	platform "github.com/MalenkiySolovey/solovey-ui/internal/deployment"
)

func fixture() (Intent, Facts) {
	resources := []ResourceBinding{
		{ResourceRef: "core:inbound:42", Owner: "inbounds", DesiredRevision: hash("inbound-config"), EndpointRevision: hash("tcp:443")},
		{ResourceRef: "panel:https", Owner: "panel", DesiredRevision: hash("panel-config"), EndpointRevision: hash("tcp:2095"), Management: true},
	}
	intent := Intent{HostRef: hostref.Local(), Owner: "server-protection", ResourceRef: "managed-table:inet:solovey_protection",
		DesiredRevision: hash("policy"), InputRevision: hash("owner-input"), Purpose: "protection_enforcement", Resources: resources}
	facts := Facts{HostRef: hostref.Local(), Revision: hash("observation"), CapabilityRevision: hash("adapter"),
		Capability:        platform.PlatformFact{Availability: platform.Available, Backend: "closed-nft-adapter", Reason: "observed"},
		InventoryComplete: true, Resources: clone(resources),
		Continuity: []Continuity{{ResourceRef: resources[1].ResourceRef, EndpointRevision: resources[1].EndpointRevision, Status: "VERIFIED"}}}
	return intent, facts
}

func hash(value string) string { return revision(value) }

func TestDriftForDurableOwnerSealRemainsScopedAndFenced(t *testing.T) {
	intent, facts := fixture()
	plan, err := Compile(intent, facts)
	if err != nil {
		t.Fatal(err)
	}
	expected := plan.Resources()[0]
	extra := Allocation{Ownership: expected, Proven: true}
	extra.Allocation += ":chain:unexpected"
	if drift := CompareResource(expected, expected.PlanRevision, true, []Allocation{extra}); len(drift) != 1 || drift[0].Kind != UnexpectedManagedResource || drift[0].Repairable {
		t.Fatalf("unexpected allocation authorized cleanup: %#v", drift)
	}
	extra.Owner = "another-owner"
	if drift := CompareResource(expected, expected.PlanRevision, true, []Allocation{extra}); len(drift) != 1 || drift[0].Kind != Missing {
		t.Fatalf("another owner's allocation was adopted: %#v", drift)
	}
	actual := Allocation{Ownership: expected, Proven: true, IdentityMismatch: true}
	if drift := CompareResource(expected, hash("old observation"), true, []Allocation{actual}); len(drift) != 1 || drift[0].Kind != Stale || drift[0].Repairable {
		t.Fatalf("late identity mismatch authorized repair: %#v", drift)
	}
	if drift := CompareResource(expected, expected.PlanRevision, true, []Allocation{actual}); len(drift) != 1 || drift[0].Kind != Mismatch {
		t.Fatalf("same-marker semantic drift was hidden: %#v", drift)
	}
}

func requireReason(t *testing.T, err error, reason Reason) {
	t.Helper()
	var problem *Conflict
	if !errors.As(err, &problem) || problem.Reason != reason {
		t.Fatalf("want typed %s, got %v", reason, err)
	}
}

func TestCompileDeterministicImmutableIdentity(t *testing.T) {
	intent, facts := fixture()
	first, err := Compile(intent, facts)
	if err != nil {
		t.Fatal(err)
	}
	intent.Resources[0], intent.Resources[1] = intent.Resources[1], intent.Resources[0]
	facts.Resources[0], facts.Resources[1] = facts.Resources[1], facts.Resources[0]
	second, err := Compile(intent, facts)
	if err != nil || first.Revision() != second.Revision() || !reflect.DeepEqual(first.Resources(), second.Resources()) {
		t.Fatalf("equivalent input order changed plan: %v", err)
	}
	intent.Resources[0].DesiredRevision = hash("mutated")
	projection := first.Intent()
	projection.Resources[0].EndpointRevision = "mutated"
	ownership := first.Resources()
	ownership[0].Owner = "foreign"
	if first.Intent().Resources[0].EndpointRevision == "mutated" || first.Resources()[0].Owner != "server-protection" {
		t.Fatal("caller mutated compiled identity")
	}
	claim := first.Resources()[0]
	if claim.HostRef != hostref.Local() || claim.PlanRevision != first.Revision() || claim.ResourceRef == "" || claim.Kind != "filter_table" || claim.Purpose == "" {
		t.Fatalf("ownership metadata is incomplete: %#v", claim)
	}
}

func TestConflictsBeforeAnyMutation(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Intent, *Facts)
		reason Reason
	}{
		{"remote host", func(i *Intent, _ *Facts) { i.HostRef.ID = "remote" }, InvalidIntent},
		{"invalid revision", func(i *Intent, _ *Facts) { i.DesiredRevision = "revision" }, InvalidIntent},
		{"unknown inventory", func(_ *Intent, f *Facts) { f.InventoryComplete = false }, ObservationUnknown},
		{"capability unavailable", func(_ *Intent, f *Facts) { f.Capability.Availability = platform.Unavailable }, CapabilityUnavailable},
		{"capability unknown", func(_ *Intent, f *Facts) { f.Capability.Availability = platform.Unknown }, CapabilityUnavailable},
		{"resource disappeared", func(_ *Intent, f *Facts) { f.Resources = f.Resources[1:] }, ResourceGone},
		{"endpoint changed", func(_ *Intent, f *Facts) { f.Resources[0].EndpointRevision = hash("tcp:8443") }, EndpointChanged},
		{"resource revision changed", func(_ *Intent, f *Facts) { f.Resources[0].DesiredRevision = hash("new-config") }, EndpointChanged},
		{"duplicate binding", func(i *Intent, _ *Facts) { i.Resources = append(i.Resources, i.Resources[0]) }, InvalidIntent},
		{"continuity unknown", func(_ *Intent, f *Facts) { f.Continuity = nil }, ManagementUnknown},
		{"continuity old endpoint", func(_ *Intent, f *Facts) { f.Continuity[0].EndpointRevision = hash("old") }, ManagementUnknown},
		{"continuity blocked", func(_ *Intent, f *Facts) { f.Continuity[0].Status = "BLOCKED" }, ManagementBlocked},
		{"non-Solovey collision", func(_ *Intent, f *Facts) {
			f.Allocations = []Allocation{{Ownership: Ownership{Allocation: ProtectionAllocation}}}
		}, ResourceConflict},
		{"concurrent owner", func(i *Intent, f *Facts) {
			f.Allocations = []Allocation{{Ownership: Ownership{HostRef: i.HostRef, Owner: "other-owner", ResourceRef: i.ResourceRef,
				PlanRevision: hash("other"), Kind: "filter_table", Allocation: ProtectionAllocation}, Proven: true}}
		}, ResourceConflict},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			intent, facts := fixture()
			test.change(&intent, &facts)
			_, err := Compile(intent, facts)
			requireReason(t, err, test.reason)
		})
	}
}

func TestExactCandidateAcceptanceAndStaleFences(t *testing.T) {
	intent, facts := fixture()
	plan, err := Compile(intent, facts)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := Accept(plan, facts, hash("candidate"))
	if err != nil || accepted.PlanRevision() != plan.Revision() || accepted.CandidateDigest() != hash("candidate") {
		t.Fatalf("candidate acceptance failed: %v", err)
	}
	if err := CheckAccepted(accepted, facts, hash("candidate")); err != nil {
		t.Fatal(err)
	}
	requireReason(t, CheckAccepted(accepted, facts, hash("other-candidate")), CandidateChanged)
	requireReason(t, CheckAccepted(Accepted{}, facts, hash("candidate")), CandidateChanged)
	for _, field := range []string{"observation", "adapter", "allocation"} {
		changed := clone(facts)
		switch field {
		case "observation":
			changed.Revision = hash("new observation")
		case "adapter":
			changed.CapabilityRevision = hash("new adapter")
		case "allocation":
			changed.Allocations = []Allocation{{Ownership: plan.Resources()[0], Proven: true}}
		}
		requireReason(t, CheckAccepted(accepted, changed, hash("candidate")), StalePlan)
	}
	changed := clone(facts)
	changed.Resources[0].EndpointRevision = hash("new endpoint")
	requireReason(t, CheckAccepted(accepted, changed, hash("candidate")), EndpointChanged)
	changed.Resources = changed.Resources[1:]
	requireReason(t, CheckAccepted(accepted, changed, hash("candidate")), ResourceGone)
	_, err = Accept(Plan{}, facts, hash("candidate"))
	requireReason(t, err, InvalidIntent)
}

func TestIntentRevisionChangesIdentity(t *testing.T) {
	intent, facts := fixture()
	plan, _ := Compile(intent, facts)
	intent.DesiredRevision = hash("next policy")
	next, err := Compile(intent, facts)
	if err != nil || next.Revision() == plan.Revision() {
		t.Fatalf("new owner revision reused plan: %v", err)
	}
	intent.Resources[0].EndpointRevision = hash("tcp:8443")
	facts.Resources[0] = intent.Resources[0]
	nextEndpoint, err := Compile(intent, facts)
	if err != nil || nextEndpoint.Revision() == next.Revision() || nextEndpoint.Intent().Resources[0].ResourceRef == "" {
		t.Fatalf("semantic endpoint change lost binding: %v", err)
	}
}

func TestBoundedDriftLateObservationAndUnownedPreservation(t *testing.T) {
	intent, facts := fixture()
	plan, _ := Compile(intent, facts)
	owned := Allocation{Ownership: plan.Resources()[0], Proven: true}
	foreign := Allocation{Ownership: Ownership{Allocation: "inet:administrator", Owner: "admin"}}
	if drift := Compare(plan, plan.Revision(), true, []Allocation{foreign, owned}); len(drift) != 0 {
		t.Fatalf("matching plan drifted: %#v", drift)
	}
	if drift := Compare(plan, strings.Repeat("a", 64), true, []Allocation{owned}); len(drift) != 1 || drift[0].Kind != Stale || drift[0].Repairable {
		t.Fatalf("late observation authorized repair: %#v", drift)
	}
	if drift := Compare(plan, plan.Revision(), true, []Allocation{foreign}); len(drift) != 1 || drift[0].Kind != Missing || !drift[0].Repairable {
		t.Fatalf("missing owned resource was not isolated: %#v", drift)
	}
	owned.PlanRevision = hash("older plan")
	if drift := Compare(plan, plan.Revision(), true, []Allocation{owned}); len(drift) != 1 || drift[0].Kind != Mismatch || !drift[0].Repairable {
		t.Fatalf("owned mismatch: %#v", drift)
	}
	owned.Proven = false
	if drift := Compare(plan, plan.Revision(), true, []Allocation{owned}); len(drift) != 1 || drift[0].Kind != Unknown || drift[0].Repairable {
		t.Fatalf("unproven allocation authorized mutation: %#v", drift)
	}
	if drift := Compare(plan, plan.Revision(), false, nil); len(drift) != 1 || drift[0].Kind != DependencyUnavailable || drift[0].Repairable {
		t.Fatalf("unavailable adapter authorized mutation: %#v", drift)
	}
}
