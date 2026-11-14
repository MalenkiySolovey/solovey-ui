package backendnetwork

import (
	"encoding/json"
	"errors"
	"testing"

	hostresources "github.com/MalenkiySolovey/solovey-host/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-host/hostref"
	networkintent "github.com/MalenkiySolovey/solovey-host/network/intent"
	contract "github.com/MalenkiySolovey/solovey-host/network/planning"
)

func typedIntentFixture() (networkintent.ProtectionEnforcementIntent, contract.Facts) {
	resource := networkintent.ResourceBinding{ResourceRef: "core:inbound:42", Owner: "inbounds", DesiredRevision: hostresources.Revision("configuration"), EndpointRevision: hostresources.Revision("tcp:443")}
	binding := networkintent.Intent{HostRef: hostref.Local(), Owner: "server-protection", ResourceRef: contract.ProtectionResourceRef, DesiredRevision: hostresources.Revision("policy"), InputRevision: hostresources.Revision("input"), Purpose: "protection_enforcement", Resources: []networkintent.ResourceBinding{resource}}
	intent := networkintent.ProtectionEnforcementIntent{Binding: binding, ArtifactRevision: binding.DesiredRevision, Mode: contract.ProtectionEndpointMode,
		Limits:    networkintent.ProtectionLimits{MaxElements: 4096, DefaultTTLSeconds: 14400, MaxTTLSeconds: 86400},
		Rates:     networkintent.ProtectionRates{Gray: networkintent.PacketLimit{Rate: 5, Burst: 10}, Rate: networkintent.PacketLimit{Rate: 20, Burst: 40}, Quarantine: networkintent.PacketLimit{Rate: 2, Burst: 4}},
		Endpoints: []networkintent.ProtectionEndpoint{{Binding: resource, Key: hostresources.PublicEndpointKey{Network: hostresources.NetworkTCP, AddressFamily: hostresources.AddressFamilyIPv4, BindAddress: "0.0.0.0", Port: 443}}}}
	facts := contract.Facts{HostRef: hostref.Local(), Revision: hostresources.Revision("observation"), CapabilityRevision: hostresources.Revision("adapter"), Capability: contract.CapabilityFact{Availability: contract.CapabilityAvailable, Backend: "closed-nft-adapter", Reason: "observed"}, InventoryComplete: true, Resources: []networkintent.ResourceBinding{resource}}
	return intent, facts
}

func requireConflict(t *testing.T, err error, reason contract.Reason) {
	t.Helper()
	var conflict *contract.Conflict
	if !errors.As(err, &conflict) || conflict.Reason != reason {
		t.Fatalf("want %s, got %v", reason, err)
	}
}

func TestOnlyActualPlannerHandlesCanBeAcceptedAndRevalidated(t *testing.T) {
	intent, facts := typedIntentFixture()
	owner := Planner{}
	plan, err := owner.CompileProtection(intent, facts)
	if err != nil {
		t.Fatal(err)
	}
	digest := hostresources.Revision("candidate")
	// Identical getters do not recreate the private immutable owner plan.
	_, err = owner.Accept(struct{ contract.ProtectionPlan }{plan}, facts, digest)
	requireConflict(t, err, contract.InvalidIntent)
	accepted, err := owner.Accept(plan, facts, digest)
	if err != nil {
		t.Fatal(err)
	}
	if accepted.PlanRevision() != plan.Revision() || accepted.CandidateDigest() != digest {
		t.Fatal("accepted candidate lost identity")
	}
	if err := owner.CheckAccepted(accepted, facts, digest); err != nil {
		t.Fatal(err)
	}
	requireConflict(t, owner.CheckAccepted(struct{ contract.Accepted }{accepted}, facts, digest), contract.CandidateChanged)
	requireConflict(t, owner.CheckAccepted(accepted, facts, hostresources.Revision("different candidate")), contract.CandidateChanged)
	resources := plan.Resources()
	resources[0].Owner = "foreign"
	if plan.Resources()[0].Owner != "server-protection" {
		t.Fatal("read view mutated owner plan")
	}
}

func TestActualPlannerRejectsUnknownCapabilityAndForeignAllocation(t *testing.T) {
	for _, condition := range []string{"unknown", "foreign"} {
		t.Run(condition, func(t *testing.T) {
			intent, facts := typedIntentFixture()
			reason := contract.CapabilityUnavailable
			if condition == "unknown" {
				facts.Capability.Availability = contract.CapabilityUnknown
			} else {
				reason = contract.ResourceConflict
				facts.Allocations = []contract.Allocation{{Ownership: contract.Ownership{HostRef: hostref.Local(), Owner: "foreign", ResourceRef: contract.ProtectionResourceRef, PlanRevision: hostresources.Revision("foreign"), Kind: "filter_table", Purpose: "other", Allocation: contract.ProtectionAllocation}, Proven: true}}
			}
			_, err := (Planner{}).CompileProtection(intent, facts)
			requireConflict(t, err, reason)
		})
	}
}

func TestOwnerFactProjectionPreservesEverySerializedInput(t *testing.T) {
	_, facts := typedIntentFixture()
	facts.Allocations = []contract.Allocation{{Ownership: contract.Ownership{HostRef: hostref.Local(), Owner: "owner", ResourceRef: "resource", PlanRevision: hostresources.Revision("plan"), Kind: "filter_table", Purpose: "purpose", Allocation: contract.ProtectionAllocation}, Proven: true, IdentityMismatch: true}}
	facts.Continuity = []contract.Continuity{{ResourceRef: "resource", EndpointRevision: hostresources.Revision("endpoint"), Status: "BLOCKED"}}
	before, err := json.Marshal(facts)
	if err != nil {
		t.Fatal(err)
	}
	after, err := json.Marshal(ownerFacts(facts))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("owner fact projection changed: %s != %s", before, after)
	}
}
