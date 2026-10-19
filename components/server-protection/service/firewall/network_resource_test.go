package firewall

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	hostsurface "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	protectionhelper "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/helper"
	protectionresources "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/resources"
)

func TestNetworkResourceLifecycleRetiresStaleGuardWithoutRebindingOtherPolicy(t *testing.T) {
	w, adapter, _, _ := newFakeCIWorkflow(t, nil)
	current := compositionEndpointPlan(t)
	w.CurrentNetworkInputs = func(context.Context, FirewallPlan) (FirewallPlan, error) { return current, nil }
	applyCompositionWorkflowPlan(t, &w, current, "guarded-baseline")
	guard4 := compositionPlanForUDP(t, current, compositionEndpoint(current, hostresources.NetworkUDP, hostresources.AddressFamilyIPv4))
	guard4Operation := applyCompositionWorkflowPlan(t, &w, guard4, "guarded-udp4")
	guard6 := compositionPlanForUDP(t, current, compositionEndpoint(current, hostresources.NetworkUDP, hostresources.AddressFamilyIPv6))
	guard6Contribution, err := contributionFromPlan(guard6)
	if err != nil {
		t.Fatal(err)
	}
	applyCompositionWorkflowPlan(t, &w, guard6, "guarded-udp6")
	resources := cloneFirewallPlan(current).Resources
	changedRef, retainedRef := resources[0].ID, resources[1].ID
	resources[0].Port = 8443
	resources[0].Capabilities.ConfigRevision = strings.Repeat("e", 64)
	for i := range resources[0].ListenIntents {
		resources[0].ListenIntents[i].Port = 8443
		resources[0].ListenIntents[i].ConfigurationRevision = resources[0].Capabilities.ConfigRevision
	}
	for i := range resources[0].Endpoints {
		resources[0].Endpoints[i].Key.Port = 8443
		resources[0].Endpoints[i].ConfigurationRevision = resources[0].Capabilities.ConfigRevision
	}
	current = networkOwnerResourcePlan(t, resources)
	before := helperOperationCount(adapter.Requests, protectionhelper.OperationNFTApply)
	_, err = w.Prepare(t.Context(), PrepareInput{Plan: current, Actor: "resource-owner", IdempotencyKey: "stale-guard", Confirmation: "PREPARE SERVER PROTECTION " + current.Revision})
	if !errors.Is(err, ErrUnsafeResource) || helperOperationCount(adapter.Requests, protectionhelper.OperationNFTApply) != before {
		t.Fatalf("old exact-socket policy silently rebound: %v", err)
	}
	// The feature owner retires its old exact-socket decision using normal
	// compensation before issuing new canonical intent. Planner never invents
	// a new UDP policy or carries a stale socket grant onto another endpoint.
	if _, err := w.Rollback(t.Context(), guard4Operation.OperationID, "ROLLBACK SERVER PROTECTION "+guard4Operation.OperationID); err != nil {
		t.Fatal(err)
	}
	applyCompositionWorkflowPlan(t, &w, current, "changed-guarded-baseline")
	newGuard := compositionPlanForUDP(t, current, compositionEndpoint(current, hostresources.NetworkUDP, hostresources.AddressFamilyIPv4))
	newGuardContribution, err := contributionFromPlan(newGuard)
	oldGuardContribution, oldErr := contributionFromPlan(guard4)
	if err != nil || oldErr != nil || newGuardContribution.ContributionID != oldGuardContribution.ContributionID || newGuardContribution.SemanticRevision == oldGuardContribution.SemanticRevision {
		t.Fatal("changed canonical endpoint did not retain semantic guard identity with a new revision")
	}
	newGuardOperation := applyCompositionWorkflowPlan(t, &w, newGuard, "changed-udp4")
	if _, err := w.Rollback(t.Context(), newGuardOperation.OperationID, "ROLLBACK SERVER PROTECTION "+newGuardOperation.OperationID); err != nil {
		t.Fatal(err)
	}
	current = networkOwnerResourcePlan(t, resources[1:])
	applyCompositionWorkflowPlan(t, &w, current, "deleted-guarded-resource")
	snapshot, err := w.Contributions.FirewallAuthority(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	values, err := contributionsFromSnapshot(snapshot)
	if err != nil || len(values) != 2 || contributionRevision(values, guard6Contribution.ContributionID) != guard6Contribution.SemanticRevision || contributionRevision(values, newGuardContribution.ContributionID) != "" {
		t.Fatal("retirement removed the independent IPv6 guard or retained the deleted binding")
	}
	composition, err := composeFirewall(values)
	if err != nil || len(composition.Plan.Endpoints) != 1 || composition.Plan.Endpoints[0].ResourceID != retainedRef || composition.Plan.Endpoints[0].UDPFlowPolicy == nil {
		t.Fatalf("remaining materialized policy changed: %v", err)
	}
	for _, endpoint := range composition.Plan.Endpoints {
		if endpoint.ResourceID == changedRef {
			t.Fatal("deleted semantic resource retained an endpoint allocation")
		}
	}
}

func TestNetworkSemanticEndpointChangeAndDeletionUseCurrentOwner(t *testing.T) {
	w, adapter, _, _ := newFakeCIWorkflow(t, nil)
	current := compositionEndpointPlan(t)
	w.CurrentNetworkInputs = func(context.Context, FirewallPlan) (FirewallPlan, error) { return current, nil }
	apply := func(key string) Result {
		t.Helper()
		prepared, err := w.Prepare(t.Context(), PrepareInput{Plan: current, Actor: "resource-owner", IdempotencyKey: key, Confirmation: "PREPARE SERVER PROTECTION " + current.Revision})
		if err != nil {
			t.Fatal(err)
		}
		result, err := w.Apply(t.Context(), ApplyInput{OperationID: prepared.Operation.OperationID, Plan: current, Resources: current.Resources, Confirmation: "APPLY SERVER PROTECTION " + prepared.Operation.OperationID})
		if err != nil || result.ActualStatus != "APPLIED" {
			t.Fatalf("%s: state=%s err=%v", key, result.State, err)
		}
		return result
	}
	initial := apply("network-initial")
	changedRef := current.Resources[0].ID
	retainedRef := current.Resources[1].ID
	retained := networkAllocationsFor(initial, retainedRef)
	retired := networkAllocationsFor(initial, changedRef)
	if len(retained) == 0 || len(retired) == 0 {
		t.Fatal("fixture did not create separately owned endpoint resources")
	}
	resources := cloneFirewallPlan(current).Resources
	resources[0].Port = 8443
	resources[0].Capabilities.ConfigRevision = strings.Repeat("e", 64)
	for i := range resources[0].ListenIntents {
		resources[0].ListenIntents[i].Port = 8443
		resources[0].ListenIntents[i].ConfigurationRevision = resources[0].Capabilities.ConfigRevision
	}
	for i := range resources[0].Endpoints {
		resources[0].Endpoints[i].Key.Port = 8443
		resources[0].Endpoints[i].ConfigurationRevision = resources[0].Capabilities.ConfigRevision
	}
	current = networkOwnerResourcePlan(t, resources)
	changed := apply("network-endpoint-changed")
	if initial.NetworkPlanRevision == changed.NetworkPlanRevision || initial.CandidateSHA256 == changed.CandidateSHA256 || len(networkAllocationsFor(changed, changedRef)) == 0 || reflect.DeepEqual(retired, networkAllocationsFor(changed, changedRef)) || !reflect.DeepEqual(retained, networkAllocationsFor(changed, retainedRef)) {
		t.Fatal("same semantic ref did not replace exactly its effective endpoint allocation")
	}
	current = networkOwnerResourcePlan(t, resources[1:])
	removed := apply("network-resource-deleted")
	if removed.NetworkPlanRevision == changed.NetworkPlanRevision || len(networkAllocationsFor(removed, changedRef)) != 0 || !reflect.DeepEqual(retained, networkAllocationsFor(removed, retainedRef)) {
		t.Fatal("resource deletion removed another semantic binding or retained the deleted one")
	}
	snapshot, err := w.Contributions.FirewallAuthority(t.Context())
	if err != nil || !snapshot.HasComposition || snapshot.Composition.ManagedPlanRevision != adapter.ManagedPlanRevision {
		t.Fatalf("live/committed resource projection diverged: %v", err)
	}
	if drift, err := w.ReconcileAuthority(t.Context()); err != nil || len(drift.NetworkDrift) != 0 || drift.NetworkEvidence == nil {
		t.Fatalf("current resource projection was not reverified: %#v %v", drift.NetworkDrift, err)
	}
}

func networkAllocationsFor(result Result, ref string) []string {
	var names []string
	for _, resource := range result.NetworkResources {
		if resource.ResourceRef == ref {
			names = append(names, resource.Allocation)
		}
	}
	return names
}

func networkOwnerResourcePlan(t *testing.T, resources []hostresources.ProtectableResource) FirewallPlan {
	t.Helper()
	now := time.Unix(1000, 0).UTC()
	var surfaces []hostsurface.HostSurfaceFactV1
	for _, resource := range resources {
		for _, endpoint := range resource.Endpoints {
			surfaces = append(surfaces, compositionOwnerSurface(resource, endpoint.Key, len(surfaces), now))
		}
	}
	graph := protectionresources.BuildSocketOwnershipGraph(protectionresources.SocketGraphInput{Resources: resources, Surfaces: surfaces, Now: now})
	plan := BuildEndpointPlan(EndpointPlanInput{Graph: graph, Resources: resources, Now: now})
	if err := Preflight(plan); err != nil {
		t.Fatalf("changed canonical resource plan: %v", err)
	}
	return plan
}
