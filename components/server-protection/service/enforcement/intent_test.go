package enforcement_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	hostresources "github.com/MalenkiySolovey/solovey-host/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-host/hostref"
	networkintent "github.com/MalenkiySolovey/solovey-host/network/intent"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/enforcement"
	platform "github.com/MalenkiySolovey/solovey-ui/internal/deployment"
	"github.com/MalenkiySolovey/solovey-ui/network/planner"
)

func intentFixture(t *testing.T) (enforcement.IntentInput, networkintent.ProtectionEnforcementIntent, planner.Facts) {
	t.Helper()
	action := actionFixture()
	admitted, reasons := enforcement.AdmitActions([]enforcement.EndpointActionInput{action}, action.Action.GraphRevision, actionTime, actionLimits())
	target := enforcement.Target{ResourceID: action.Action.ResourceID, ActionScopeRevision: action.Action.GraphRevision,
		EndpointRevision: action.Action.EndpointRevision, ResourceRevision: action.Action.ResourceRevision, ConfigurationRevision: action.Action.ConfigurationRevision}
	contributions, bindingReasons := enforcement.BindActions(target, admitted[action.Action.EndpointRevision])
	if len(reasons)+len(bindingReasons) != 0 {
		t.Fatalf("valid policy rejected: %v %v", reasons, bindingReasons)
	}
	input := enforcement.IntentInput{
		Revision: strings.Repeat("1", 64), InputRevision: strings.Repeat("2", 64), Mode: planner.ProtectionEndpointMode, Limits: actionLimits(),
		Endpoints: []enforcement.Endpoint{{ResourceID: target.ResourceID, Owner: "inbound", ConfigurationRevision: target.ConfigurationRevision,
			EndpointRevision: target.EndpointRevision, Key: hostresources.PublicEndpointKey{Network: hostresources.NetworkTCP,
				AddressFamily: hostresources.AddressFamilyIPv4, BindAddress: "192.0.2.1", Port: 443}, Contributions: contributions}},
		AllowTCPPorts: []int{443}, GraylistCIDRs: []string{"198.51.100.0/24"},
	}
	intent := enforcement.NetworkIntent(input)
	facts := planner.Facts{HostRef: hostref.Local(), Revision: input.InputRevision, InventoryComplete: true,
		CapabilityRevision: strings.Repeat("3", 64), Capability: platform.PlatformFact{Availability: platform.Available, Backend: "nft"},
		Resources: append([]planner.ResourceBinding(nil), intent.Binding.Resources...)}
	return input, intent, facts
}

func TestFeatureIntentIsConsumedByExistingPlannerAndCannotAuthorizeExecution(t *testing.T) {
	_, intent, facts := intentFixture(t)
	plan, err := planner.CompileProtection(intent, facts)
	if err != nil {
		t.Fatal(err)
	}
	value := plan.Enforcement()
	if value.Binding.Owner != "server-protection" || !value.Binding.HostRef.IsLocal() || value.Binding.ResourceRef != planner.ProtectionResourceRef ||
		len(value.Endpoints) != 1 || len(value.Endpoints[0].Contributions) != 1 || value.Endpoints[0].Contributions[0].Class != planner.BlockSet ||
		value.Endpoints[0].Contributions[0].TTLSeconds != 3600 || value.Endpoints[0].Contributions[0].Subject != "203.0.113.10/32" {
		t.Fatalf("feature policy changed at the Planner boundary: %+v", value)
	}
	if err := planner.CheckAccepted(planner.Accepted{}, facts, strings.Repeat("a", 64)); err == nil {
		t.Fatal("typed intent or compiled plan became an accepted execution receipt")
	}
	accepted, err := planner.Accept(plan.Plan, facts, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	current := facts
	current.CapabilityRevision = strings.Repeat("4", 64)
	if err := planner.CheckAccepted(accepted, current, accepted.CandidateDigest()); err == nil {
		t.Fatal("old accepted candidate survived a current capability generation change")
	}
}

func TestFeatureIntentCannotBypassCurrentHostResourceCapabilityOrPlannerOwnership(t *testing.T) {
	for name, test := range map[string]struct {
		mutate func(*networkintent.ProtectionEnforcementIntent, *planner.Facts)
		reason planner.Reason
	}{
		"wrong requested host": {func(i *networkintent.ProtectionEnforcementIntent, _ *planner.Facts) {
			i.Binding.HostRef = hostref.Ref{Kind: "NODE", ID: "other"}
		}, planner.InvalidIntent},
		"wrong observed host": {func(_ *networkintent.ProtectionEnforcementIntent, f *planner.Facts) { f.HostRef = hostref.Ref{} }, planner.ObservationUnknown},
		"wrong resource ref": {func(i *networkintent.ProtectionEnforcementIntent, _ *planner.Facts) {
			i.Binding.ResourceRef = "foreign:table"
		}, planner.InvalidIntent},
		"foreign endpoint": {func(i *networkintent.ProtectionEnforcementIntent, _ *planner.Facts) {
			i.Endpoints[0].Binding.ResourceRef = "core:inbound:other"
		}, planner.ResourceGone},
		"foreign endpoint owner": {func(i *networkintent.ProtectionEnforcementIntent, _ *planner.Facts) {
			i.Endpoints[0].Binding.Owner = "other"
		}, planner.ResourceGone},
		"stale resource revision": {func(_ *networkintent.ProtectionEnforcementIntent, f *planner.Facts) {
			f.Resources[0].DesiredRevision = strings.Repeat("5", 64)
		}, planner.EndpointChanged},
		"stale endpoint revision": {func(_ *networkintent.ProtectionEnforcementIntent, f *planner.Facts) {
			f.Resources[0].EndpointRevision = strings.Repeat("6", 64)
		}, planner.EndpointChanged},
		"unknown capability": {func(_ *networkintent.ProtectionEnforcementIntent, f *planner.Facts) {
			f.Capability.Availability = platform.Unknown
		}, planner.CapabilityUnavailable},
		"missing capability revision": {func(_ *networkintent.ProtectionEnforcementIntent, f *planner.Facts) { f.CapabilityRevision = "" }, planner.CapabilityUnavailable},
		"incomplete inventory":        {func(_ *networkintent.ProtectionEnforcementIntent, f *planner.Facts) { f.InventoryComplete = false }, planner.ObservationUnknown},
		"unknown action class": {func(i *networkintent.ProtectionEnforcementIntent, _ *planner.Facts) {
			i.Endpoints[0].Contributions[0].Class = "future"
		}, planner.InvalidIntent},
		"foreign table": {func(_ *networkintent.ProtectionEnforcementIntent, f *planner.Facts) {
			f.Allocations = []planner.Allocation{{Ownership: planner.Ownership{HostRef: hostref.Local(), Owner: "foreign", ResourceRef: planner.ProtectionResourceRef,
				PlanRevision: strings.Repeat("7", 64), Kind: "filter_table", Allocation: planner.ProtectionAllocation}, Proven: true}}
		}, planner.ResourceConflict},
	} {
		t.Run(name, func(t *testing.T) {
			_, intent, facts := intentFixture(t)
			test.mutate(&intent, &facts)
			_, err := planner.CompileProtection(intent, facts)
			var conflict *planner.Conflict
			if !errors.As(err, &conflict) || conflict.Reason != test.reason {
				t.Fatalf("unsafe policy reached the planner: got %v, want %s", err, test.reason)
			}
		})
	}
}

func TestTypedIntentDoesNotShareMutablePolicyOrPlannerState(t *testing.T) {
	input, intent, facts := intentFixture(t)
	plan, err := planner.CompileProtection(intent, facts)
	if err != nil {
		t.Fatal(err)
	}
	before := plan.Enforcement()
	input.Endpoints[0].Contributions[0].Subject = "203.0.113.11/32"
	input.AllowTCPPorts[0] = 22
	input.GraylistCIDRs[0] = "203.0.113.0/24"
	if intent.Endpoints[0].Contributions[0].Subject != "203.0.113.10/32" || intent.AllowTCPPorts[0] != 443 || intent.GraylistCIDRs[0] != "198.51.100.0/24" {
		t.Fatal("mutable owner data escaped into a request")
	}
	intent.Endpoints[0].Contributions[0].Class = networkintent.GraySet
	intent.Binding.Resources[0].ResourceRef = "foreign"
	if !reflect.DeepEqual(plan.Enforcement(), before) {
		t.Fatal("mutable feature request changed an already compiled network plan")
	}
	if enforcement.Materializes(domain.IntentRouteToDecoy) || enforcement.Materializes(domain.IntentObserve) {
		t.Fatal("non-firewall policy acquired a host-network primitive")
	}
}
