package firewall

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	hostsurface "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	protectionhelper "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/helper"
	protectionoperations "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
	protectionresources "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/resources"
)

type failingCompositionMarker struct{}

func (failingCompositionMarker) MarkMutation(string, string) error {
	return errors.New("fixture_marker_failed")
}

func TestFirewallCompositionKeepsIndependentNetworkAndFamilyContributions(t *testing.T) {
	baseline := compositionEndpointPlan(t)
	baselineContribution, err := contributionFromPlan(baseline)
	if err != nil {
		t.Fatal(err)
	}
	var udp4, udp6 EndpointPolicy
	for _, endpoint := range baseline.Endpoints {
		if endpoint.Key.Network != hostresources.NetworkUDP {
			continue
		}
		if endpoint.Key.AddressFamily == hostresources.AddressFamilyIPv4 {
			udp4 = endpoint
		} else {
			udp6 = endpoint
		}
	}
	contribution4 := compositionUDPContribution(t, baseline, udp4)
	contribution6 := compositionUDPContribution(t, baseline, udp6)
	if contribution4.ContributionID == contribution6.ContributionID || contribution4.AddressFamily == contribution6.AddressFamily {
		t.Fatalf("family identities collapsed: %#v %#v", contribution4, contribution6)
	}

	composed, err := composeFirewall([]ManagedFirewallContributionV1{contribution6, baselineContribution, contribution4})
	if err != nil {
		t.Fatal(err)
	}
	if len(composed.Bindings) != 3 || !slices.IsSortedFunc(composed.Bindings, func(a, b compositionBinding) int { return strings.Compare(a.ContributionID, b.ContributionID) }) {
		t.Fatalf("composition is not deterministic: %#v", composed.Bindings)
	}
	assertCompositionPolicies(t, composed.Plan, true, true)
	for _, endpoint := range composed.Plan.Endpoints {
		if endpoint.Key.Network == hostresources.NetworkTCP && endpoint.UDPFlowPolicy != nil {
			t.Fatal("TCP claim acquired a UDP contribution")
		}
	}

	without4, err := composeFirewall([]ManagedFirewallContributionV1{baselineContribution, contribution6})
	if err != nil {
		t.Fatal(err)
	}
	assertCompositionPolicies(t, without4.Plan, false, true)
	if without4.Revision == composed.Revision {
		t.Fatal("removing one contribution did not change composition revision")
	}
}

func TestUDPContributionKeyIsStableAcrossExactSocketRevisionDrift(t *testing.T) {
	baseline := compositionEndpointPlan(t)
	endpoint := compositionEndpoint(baseline, hostresources.NetworkUDP, hostresources.AddressFamilyIPv4)
	first, err := contributionFromPlan(compositionPlanForUDP(t, baseline, endpoint))
	if err != nil {
		t.Fatal(err)
	}
	second := first
	second.EndpointID = strings.Repeat("9", 64)
	policy := *first.UDPPolicy
	policy.EndpointID = second.EndpointID
	policy.ExactSocketRevision = strings.Repeat("8", 64)
	policy = FinalizeUDPFlowPolicy(policy)
	second.UDPPolicy = &policy
	second = finalizeContribution(second)
	if first.ContributionID != second.ContributionID {
		t.Fatalf("socket drift created a parallel authority key: %q != %q", first.ContributionID, second.ContributionID)
	}
	if first.SemanticRevision == second.SemanticRevision {
		t.Fatal("exact socket drift did not change the contribution semantic revision")
	}
	if err := validateContribution(second); err != nil {
		t.Fatalf("drifted semantic contribution is invalid: %v", err)
	}
}

func TestFirewallCompositionRollbackRebasesOntoNewUnrelatedBaseline(t *testing.T) {
	baseline := compositionEndpointPlan(t)
	oldBaseline, err := contributionFromPlan(baseline)
	if err != nil {
		t.Fatal(err)
	}
	var endpoints []EndpointPolicy
	for _, endpoint := range baseline.Endpoints {
		if endpoint.Key.Network == hostresources.NetworkUDP {
			endpoints = append(endpoints, endpoint)
		}
	}
	udpA := compositionUDPContribution(t, baseline, endpoints[0])
	udpB := compositionUDPContribution(t, baseline, endpoints[1])
	before, err := composeFirewall([]ManagedFirewallContributionV1{oldBaseline, udpA, udpB})
	if err != nil {
		t.Fatal(err)
	}

	newPlan := cloneFirewallPlan(baseline)
	newPlan.AllowTCPPorts = append(newPlan.AllowTCPPorts, 8443)
	slices.Sort(newPlan.AllowTCPPorts)
	newPlan.AllowTCPPorts = slices.Compact(newPlan.AllowTCPPorts)
	newPlan.Revision = firewallPlanRevision(newPlan)
	newBaseline, err := contributionFromPlan(newPlan)
	if err != nil {
		t.Fatal(err)
	}
	current, err := composeFirewall([]ManagedFirewallContributionV1{newBaseline, udpA, udpB})
	if err != nil {
		t.Fatal(err)
	}
	afterRollbackA, err := composeFirewall([]ManagedFirewallContributionV1{newBaseline, udpB})
	if err != nil {
		t.Fatal(err)
	}
	if before.Revision == current.Revision || current.Revision == afterRollbackA.Revision || !slices.Contains(afterRollbackA.Plan.AllowTCPPorts, 8443) {
		t.Fatalf("rollback did not preserve newer baseline: before=%s current=%s rollback=%s ports=%v", before.Revision, current.Revision, afterRollbackA.Revision, afterRollbackA.Plan.AllowTCPPorts)
	}
	if got := policyFamilies(afterRollbackA.Plan); !slices.Equal(got, []hostresources.AddressFamily{udpB.AddressFamily}) {
		t.Fatalf("rollback removed or retained wrong contribution: %v", got)
	}
	if _, err := composeFirewall([]ManagedFirewallContributionV1{newBaseline, udpB, udpB}); err == nil {
		t.Fatal("conflicting duplicate contribution was accepted")
	}
}

func TestEffectiveBaselineAuthorityRevisionDetectsDesiredBaselineDrift(t *testing.T) {
	baseline := compositionEndpointPlan(t)
	baselineContribution, err := contributionFromPlan(baseline)
	if err != nil {
		t.Fatal(err)
	}
	var udpEndpoint EndpointPolicy
	for _, endpoint := range baseline.Endpoints {
		if endpoint.Key.Network == hostresources.NetworkUDP {
			udpEndpoint = endpoint
			break
		}
	}
	udpContribution := compositionUDPContribution(t, baseline, udpEndpoint)
	snapshot := compositionAuthoritySnapshot(t, []ManagedFirewallContributionV1{baselineContribution, udpContribution})
	revision, err := EffectiveBaselineAuthorityRevision(snapshot)
	if err != nil || revision != baselineContribution.Baseline.Revision {
		t.Fatalf("explicit baseline authority revision = %q, want %q, err=%v", revision, baselineContribution.Baseline.Revision, err)
	}
	if matches, matchErr := BaselineAuthorityMatchesPlan(snapshot, baseline); matchErr != nil || !matches {
		t.Fatalf("current desired baseline did not match authority: matches=%v err=%v", matches, matchErr)
	}
	changed := compositionBaselineWithTCPPort(baseline, 8443)
	if matches, matchErr := BaselineAuthorityMatchesPlan(snapshot, changed); matchErr != nil || matches {
		t.Fatalf("an unapplied desired baseline appeared active: matches=%v err=%v", matches, matchErr)
	}

	// Rolling back the first baseline leaves the UDP contribution's agreed
	// fallback as the effective baseline authority.
	fallbackOnly := compositionAuthoritySnapshot(t, []ManagedFirewallContributionV1{udpContribution})
	revision, err = EffectiveBaselineAuthorityRevision(fallbackOnly)
	if err != nil || revision != baselineContribution.Baseline.Revision {
		t.Fatalf("fallback baseline authority revision = %q, err=%v", revision, err)
	}
	fallbackOnly.Composition.CandidateSHA256 = strings.Repeat("0", 64)
	if _, err = EffectiveBaselineAuthorityRevision(fallbackOnly); err == nil {
		t.Fatal("drifted persisted composition was accepted as baseline authority")
	}
}

func TestWorkflowComposesAndRollsBackOnlyOwnedContribution(t *testing.T) {
	workflow, _, _, _ := newFakeCIWorkflow(t, nil)
	authority := workflow.Contributions.(*memoryContributionStore)
	baseline1 := compositionEndpointPlan(t)
	baseline2 := compositionBaselineWithTCPPort(baseline1, 8443)
	baseline3 := compositionBaselineWithTCPPort(baseline1, 9443)
	base1Operation := applyCompositionWorkflowPlan(t, &workflow, baseline1, "base-1")
	_ = base1Operation
	base2Operation := applyCompositionWorkflowPlan(t, &workflow, baseline2, "base-2")

	udpEndpoints := make([]EndpointPolicy, 0, 2)
	for _, endpoint := range baseline2.Endpoints {
		if endpoint.Key.Network == hostresources.NetworkUDP {
			udpEndpoints = append(udpEndpoints, endpoint)
		}
	}
	udpAPlan := compositionPlanForUDP(t, baseline2, udpEndpoints[0])
	udpAContribution, _ := contributionFromPlan(udpAPlan)
	udpAOperation := applyCompositionWorkflowPlan(t, &workflow, udpAPlan, "udp-a")

	if _, err := workflow.Rollback(t.Context(), base2Operation.OperationID, "ROLLBACK SERVER PROTECTION "+base2Operation.OperationID); err != nil {
		t.Fatalf("rollback TCP baseline update: %v", err)
	}
	snapshot, err := authority.FirewallAuthority(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	values, err := contributionsFromSnapshot(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if contributionRevision(values, BaselineContributionID) == "" || contributionRevision(values, udpAContribution.ContributionID) != udpAContribution.SemanticRevision {
		t.Fatalf("TCP rollback lost UDP contribution: %#v", values)
	}

	udpBPlan := compositionPlanForUDP(t, baseline1, udpEndpoints[1])
	udpBContribution, _ := contributionFromPlan(udpBPlan)
	udpBOperation := applyCompositionWorkflowPlan(t, &workflow, udpBPlan, "udp-b")
	if _, err = workflow.Rollback(t.Context(), udpAOperation.OperationID, "ROLLBACK SERVER PROTECTION "+udpAOperation.OperationID); err != nil {
		t.Fatalf("rollback UDP A: %v", err)
	}
	snapshot, _ = authority.FirewallAuthority(t.Context())
	values, _ = contributionsFromSnapshot(snapshot)
	if contributionRevision(values, udpAContribution.ContributionID) != "" || contributionRevision(values, udpBContribution.ContributionID) != udpBContribution.SemanticRevision {
		t.Fatalf("UDP A rollback did not preserve UDP B: %#v", values)
	}

	applyCompositionWorkflowPlan(t, &workflow, baseline3, "base-3")
	newBaseline, _ := contributionFromPlan(baseline3)
	if _, err = workflow.Rollback(t.Context(), udpBOperation.OperationID, "ROLLBACK SERVER PROTECTION "+udpBOperation.OperationID); err != nil {
		t.Fatalf("rollback UDP B after baseline change: %v", err)
	}
	snapshot, _ = authority.FirewallAuthority(t.Context())
	values, _ = contributionsFromSnapshot(snapshot)
	if contributionRevision(values, udpBContribution.ContributionID) != "" || contributionRevision(values, BaselineContributionID) != newBaseline.SemanticRevision {
		t.Fatalf("UDP rollback overwrote newer TCP baseline: %#v", values)
	}
}

func TestInitialBaselineRollbackPreservesDependentUDPAndFinalRollbackDeletesCurrentTable(t *testing.T) {
	workflow, helper, _, _ := newFakeCIWorkflow(t, nil)
	authority := workflow.Contributions.(*memoryContributionStore)
	baseline := compositionEndpointPlan(t)
	baselineOperation := applyCompositionWorkflowPlan(t, &workflow, baseline, "initial-baseline")

	var udpEndpoint EndpointPolicy
	for _, endpoint := range baseline.Endpoints {
		if endpoint.Key.Network == hostresources.NetworkUDP {
			udpEndpoint = endpoint
			break
		}
	}
	udpPlan := compositionPlanForUDP(t, baseline, udpEndpoint)
	udpContribution, err := contributionFromPlan(udpPlan)
	if err != nil {
		t.Fatal(err)
	}
	udpOperation := applyCompositionWorkflowPlan(t, &workflow, udpPlan, "dependent-udp")

	if _, err = workflow.Rollback(t.Context(), baselineOperation.OperationID, "ROLLBACK SERVER PROTECTION "+baselineOperation.OperationID); err != nil {
		t.Fatalf("rollback initial baseline: %v", err)
	}
	snapshot, err := authority.FirewallAuthority(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	values, err := contributionsFromSnapshot(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if contributionRevision(values, BaselineContributionID) != "" || contributionRevision(values, udpContribution.ContributionID) != udpContribution.SemanticRevision || !snapshot.HasComposition || !helper.ManagedTablePresent {
		t.Fatalf("baseline rollback did not preserve the dependent UDP aggregate: values=%#v snapshot=%#v helperPresent=%v", values, snapshot, helper.ManagedTablePresent)
	}

	if _, err = workflow.Rollback(t.Context(), udpOperation.OperationID, "ROLLBACK SERVER PROTECTION "+udpOperation.OperationID); err != nil {
		t.Fatalf("rollback final UDP contribution: %v", err)
	}
	snapshot, err = authority.FirewallAuthority(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.HasComposition || len(snapshot.Contributions) != 0 || helper.ManagedTablePresent {
		t.Fatalf("empty semantic target retained stale table authority: snapshot=%#v helperPresent=%v", snapshot, helper.ManagedTablePresent)
	}
}

func TestRestartFinalizesCommittedEmptyRollbackWithoutSecondMutation(t *testing.T) {
	workflow, helper, manager, _ := newFakeCIWorkflow(t, nil)
	operation := applyCompositionWorkflowPlan(t, &workflow, compositionEndpointPlan(t), "committed-empty-base")
	rolling, err := manager.BeginRollback(t.Context(), operation.OperationID, operation.Revision)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := workflow.loadCheckpoint(operation.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	first, err := workflow.finishRollback(t.Context(), rolling, checkpoint.ArtifactRevision, true, false)
	if err != nil || helper.ManagedTablePresent {
		t.Fatalf("first empty rollback result=%#v present=%v err=%v", first, helper.ManagedTablePresent, err)
	}
	applyCalls := helperOperationCount(helper.Requests, protectionhelper.OperationNFTApply)
	rollbackCalls := helperOperationCount(helper.Requests, protectionhelper.OperationNFTRollback)
	completed, err := workflow.finishRollback(t.Context(), rolling, first.ArtifactRevision, true, true)
	if err != nil || completed.State != protectionoperations.StateRolledBack || helper.ManagedTablePresent ||
		helperOperationCount(helper.Requests, protectionhelper.OperationNFTApply) != applyCalls || helperOperationCount(helper.Requests, protectionhelper.OperationNFTRollback) != rollbackCalls {
		t.Fatalf("committed empty rollback repeated mutation: result=%#v present=%v err=%v", completed, helper.ManagedTablePresent, err)
	}
}

func TestCheckpointV1OperationRemainsExactlyRollbackableWithoutBecomingAuthority(t *testing.T) {
	workflow, helper, _, _ := newFakeCIWorkflow(t, nil)
	authority := workflow.Contributions.(*memoryContributionStore)
	baseline := compositionEndpointPlan(t)
	operation := applyCompositionWorkflowPlan(t, &workflow, baseline, "checkpoint-v1")
	checkpoint, err := workflow.loadCheckpoint(operation.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	legacy := FirewallCheckpoint{Version: 1, OperationID: checkpoint.OperationID, ArtifactRevision: checkpoint.ArtifactRevision,
		PlanRevision: checkpoint.PlanRevision, GraphRevision: checkpoint.GraphRevision, OwnerObservationRevision: checkpoint.OwnerObservationRevision,
		CandidateSHA256: checkpoint.CandidateSHA256, RollbackSHA256: checkpoint.RollbackSHA256, PreviousRevision: checkpoint.PreviousRevision, PreviousTablePresent: checkpoint.PreviousTablePresent}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err = workflow.State.WriteFirewallState(operation.OperationID, data); err != nil {
		t.Fatal(err)
	}
	authority.mu.Lock()
	authority.contributions = map[string]protectionrepository.FirewallContributionModel{}
	authority.composition = protectionrepository.FirewallCompositionModel{}
	authority.hasComposition = false
	delete(authority.transitions, operation.OperationID)
	authority.mu.Unlock()

	if _, err = workflow.Rollback(t.Context(), operation.OperationID, "ROLLBACK SERVER PROTECTION "+operation.OperationID); err != nil {
		t.Fatalf("exact legacy rollback failed: %v", err)
	}
	snapshot, err := authority.FirewallAuthority(t.Context())
	if err != nil || snapshot.HasComposition || len(snapshot.Contributions) != 0 || helper.ManagedTablePresent {
		t.Fatalf("legacy rollback fabricated authority or retained table: snapshot=%#v helperPresent=%v err=%v", snapshot, helper.ManagedTablePresent, err)
	}
}

func TestRestartRollbackReconcilesAlreadyComposedTargetWithoutDuplicateMutation(t *testing.T) {
	workflow, helper, manager, _ := newFakeCIWorkflow(t, nil)
	authority := workflow.Contributions.(*memoryContributionStore)
	baseline := compositionEndpointPlan(t)
	applyCompositionWorkflowPlan(t, &workflow, baseline, "restart-base")
	var udpEndpoints []EndpointPolicy
	for _, endpoint := range baseline.Endpoints {
		if endpoint.Key.Network == hostresources.NetworkUDP {
			udpEndpoints = append(udpEndpoints, endpoint)
		}
	}
	planA := compositionPlanForUDP(t, baseline, udpEndpoints[0])
	contributionA, _ := contributionFromPlan(planA)
	operationA := applyCompositionWorkflowPlan(t, &workflow, planA, "restart-udp-a")
	planB := compositionPlanForUDP(t, baseline, udpEndpoints[1])
	applyCompositionWorkflowPlan(t, &workflow, planB, "restart-udp-b")

	snapshot, err := authority.FirewallAuthority(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	current, err := contributionsFromSnapshot(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	target := make([]ManagedFirewallContributionV1, 0, len(current)-1)
	for _, value := range current {
		if value.ContributionID != contributionA.ContributionID {
			target = append(target, value)
		}
	}
	targetComposition, err := composeFirewall(target)
	if err != nil {
		t.Fatal(err)
	}
	rolling, err := manager.BeginRollback(t.Context(), operationA.OperationID, operationA.Revision)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := workflow.loadCheckpoint(operationA.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a crash after the rollback candidate reached the helper but
	// before semantic authority was committed.
	helper.ManagedTablePresent = true
	helper.ManagedPlanRevision = targetComposition.PlanRevision
	helper.ManagedCandidateSHA = targetComposition.CandidateSHA
	applyCalls := helperOperationCount(helper.Requests, protectionhelper.OperationNFTApply)
	firstRecovery, err := workflow.finishRollback(t.Context(), rolling, checkpoint.ArtifactRevision, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if after := helperOperationCount(helper.Requests, protectionhelper.OperationNFTApply); after != applyCalls {
		t.Fatalf("restart duplicated rollback mutation: apply calls %d -> %d", applyCalls, after)
	}
	snapshot, _ = authority.FirewallAuthority(t.Context())
	values, _ := contributionsFromSnapshot(snapshot)
	if contributionRevision(values, contributionA.ContributionID) != "" {
		t.Fatalf("reconciled rollback retained owned contribution: %#v", values)
	}
	applyAfterCommit := helperOperationCount(helper.Requests, protectionhelper.OperationNFTApply)
	rollbackAfterCommit := helperOperationCount(helper.Requests, protectionhelper.OperationNFTRollback)
	completed, err := workflow.finishRollback(t.Context(), rolling, firstRecovery.ArtifactRevision, true, true)
	if err != nil || completed.State != protectionoperations.StateRolledBack || helperOperationCount(helper.Requests, protectionhelper.OperationNFTApply) != applyAfterCommit || helperOperationCount(helper.Requests, protectionhelper.OperationNFTRollback) != rollbackAfterCommit {
		t.Fatalf("committed rollback recovery repeated mutation: result=%#v err=%v", completed, err)
	}
}

func TestRestartRecoveryRollsBackExactUncommittedForwardContribution(t *testing.T) {
	workflow, helper, manager, _ := newFakeCIWorkflow(t, nil)
	authority := workflow.Contributions.(*memoryContributionStore)
	baseline := compositionEndpointPlan(t)
	applyCompositionWorkflowPlan(t, &workflow, baseline, "forward-crash-base")
	before, err := authority.FirewallAuthority(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var udpEndpoint EndpointPolicy
	for _, endpoint := range baseline.Endpoints {
		if endpoint.Key.Network == hostresources.NetworkUDP {
			udpEndpoint = endpoint
			break
		}
	}
	udpPlan := compositionPlanForUDP(t, baseline, udpEndpoint)
	prepared, err := workflow.Prepare(t.Context(), PrepareInput{Plan: udpPlan, Actor: "composition-test", IdempotencyKey: "forward-crash-udp", Confirmation: "PREPARE SERVER PROTECTION " + udpPlan.Revision})
	if err != nil {
		t.Fatal(err)
	}
	transition, err := authority.FirewallTransition(t.Context(), prepared.Operation.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	desired, err := decodeContributionJSON(transition.DesiredJSON)
	if err != nil {
		t.Fatal(err)
	}
	current, err := contributionsFromSnapshot(before)
	if err != nil {
		t.Fatal(err)
	}
	after, err := composeFirewall(replaceContribution(current, &desired))
	if err != nil {
		t.Fatal(err)
	}
	applying, err := manager.Transition(t.Context(), prepared.Operation.OperationID, prepared.Operation.Revision, protectionoperations.StateApplying)
	if err != nil {
		t.Fatal(err)
	}
	marker := time.Now().UTC().UnixNano()
	if err = authority.MarkFirewallTransitionMutation(t.Context(), applying.OperationID, marker); err != nil {
		t.Fatal(err)
	}
	if err = authority.MarkFirewallTransitionMutationCompleted(t.Context(), applying.OperationID, marker+1); err != nil {
		t.Fatal(err)
	}
	// The helper crossed the forward mutation boundary, but semantic authority
	// is still the exact previous aggregate because the process crashed before
	// CommitFirewallAuthority.
	helper.ManagedTablePresent = true
	helper.ManagedPlanRevision = after.PlanRevision
	helper.ManagedCandidateSHA = after.CandidateSHA
	applyCalls := helperOperationCount(helper.Requests, protectionhelper.OperationNFTApply)
	result, err := workflow.rollback(t.Context(), applying, "", true)
	if err != nil || result.State != protectionoperations.StateRolledBack {
		t.Fatalf("restart rollback result=%#v err=%v", result, err)
	}
	if calls := helperOperationCount(helper.Requests, protectionhelper.OperationNFTApply); calls != applyCalls+1 {
		t.Fatalf("restart recovery apply calls = %d, want %d", calls, applyCalls+1)
	}
	restored, err := authority.FirewallAuthority(t.Context())
	if err != nil || !restored.HasComposition || restored.Composition.Revision != before.Composition.Revision || helper.ManagedPlanRevision != before.Composition.ManagedPlanRevision || helper.ManagedCandidateSHA != before.Composition.CandidateSHA256 {
		t.Fatalf("restart recovery did not restore exact semantic aggregate: before=%#v after=%#v helper=%#v err=%v", before.Composition, restored.Composition, helper, err)
	}
}

func TestHelperErrorAfterFirstAtomicApplyUsesAuthenticatedSidecarRollback(t *testing.T) {
	workflow, helper, _, _ := newFakeCIWorkflow(t, nil)
	authority := workflow.Contributions.(*memoryContributionStore)
	plan := compositionEndpointPlan(t)
	prepared, err := workflow.Prepare(t.Context(), PrepareInput{Plan: plan, Actor: "composition-test", IdempotencyKey: "first-post-apply-error", Confirmation: "PREPARE SERVER PROTECTION " + plan.Revision})
	if err != nil {
		t.Fatal(err)
	}
	helper.FailAfter[protectionhelper.OperationNFTApply] = errors.New("fixture_post_apply_observation_failed")
	rollbackCalls := helperOperationCount(helper.Requests, protectionhelper.OperationNFTRollback)
	result, err := workflow.Apply(t.Context(), ApplyInput{OperationID: prepared.Operation.OperationID, Plan: plan, Resources: plan.Resources, Confirmation: "APPLY SERVER PROTECTION " + prepared.Operation.OperationID})
	if err == nil || result.State != protectionoperations.StateRolledBack || helper.ManagedTablePresent {
		t.Fatalf("post-apply helper failure result=%#v present=%v err=%v", result, helper.ManagedTablePresent, err)
	}
	if helperOperationCount(helper.Requests, protectionhelper.OperationNFTRollback) != rollbackCalls+1 {
		t.Fatalf("authenticated sidecar rollback was not called exactly once: %#v", helper.Requests)
	}
	last := helper.Requests[len(helper.Requests)-1]
	if last.Operation != protectionhelper.OperationNFTRollback || last.NFTRollback == nil || last.NFTRollback.ExpectedSHA256 != "" || last.NFTRollback.ExpectedCurrentRevision == "" {
		t.Fatalf("first-contribution rollback was not fenced to helper-owned sidecar and exact current revision: %#v", last)
	}
	snapshot, snapshotErr := authority.FirewallAuthority(t.Context())
	transition, transitionErr := authority.FirewallTransition(t.Context(), prepared.Operation.OperationID)
	if snapshotErr != nil || transitionErr != nil || snapshot.HasComposition || len(snapshot.Contributions) != 0 || transition.State != "ROLLED_BACK" || transition.MarkerUnixNano <= 0 || transition.MutationCompletedUnixNano != 0 {
		t.Fatalf("first-contribution authority was not safely reconciled: snapshot=%#v transition=%#v snapshotErr=%v transitionErr=%v", snapshot, transition, snapshotErr, transitionErr)
	}
}

func TestHelperErrorAfterComposedAtomicApplyRestoresCurrentAuthorityWithoutCompletionFence(t *testing.T) {
	workflow, helper, _, _ := newFakeCIWorkflow(t, nil)
	authority := workflow.Contributions.(*memoryContributionStore)
	baseline := compositionEndpointPlan(t)
	applyCompositionWorkflowPlan(t, &workflow, baseline, "post-apply-error-base")
	before, err := authority.FirewallAuthority(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var udpEndpoint EndpointPolicy
	for _, endpoint := range baseline.Endpoints {
		if endpoint.Key.Network == hostresources.NetworkUDP {
			udpEndpoint = endpoint
			break
		}
	}
	udpPlan := compositionPlanForUDP(t, baseline, udpEndpoint)
	prepared, err := workflow.Prepare(t.Context(), PrepareInput{Plan: udpPlan, Actor: "composition-test", IdempotencyKey: "composed-post-apply-error", Confirmation: "PREPARE SERVER PROTECTION " + udpPlan.Revision})
	if err != nil {
		t.Fatal(err)
	}
	helper.FailAfter[protectionhelper.OperationNFTApply] = errors.New("fixture_post_apply_observation_failed")
	applyCalls := helperOperationCount(helper.Requests, protectionhelper.OperationNFTApply)
	result, err := workflow.Apply(t.Context(), ApplyInput{OperationID: prepared.Operation.OperationID, Plan: udpPlan, Resources: udpPlan.Resources, Confirmation: "APPLY SERVER PROTECTION " + prepared.Operation.OperationID})
	if err == nil || result.State != protectionoperations.StateRolledBack {
		t.Fatalf("composed post-apply helper failure result=%#v err=%v", result, err)
	}
	if helperOperationCount(helper.Requests, protectionhelper.OperationNFTApply) != applyCalls+2 {
		t.Fatalf("recomposition did not perform exactly one forward and one recovery apply: %#v", helper.Requests)
	}
	after, snapshotErr := authority.FirewallAuthority(t.Context())
	transition, transitionErr := authority.FirewallTransition(t.Context(), prepared.Operation.OperationID)
	if snapshotErr != nil || transitionErr != nil || !after.HasComposition || after.Composition.Revision != before.Composition.Revision ||
		helper.ManagedPlanRevision != before.Composition.ManagedPlanRevision || helper.ManagedCandidateSHA != before.Composition.CandidateSHA256 ||
		transition.State != "ROLLED_BACK" || transition.MarkerUnixNano <= 0 || transition.MutationCompletedUnixNano != 0 {
		t.Fatalf("composed authority was not restored after helper error: before=%#v after=%#v transition=%#v helper=%#v snapshotErr=%v transitionErr=%v", before.Composition, after.Composition, transition, helper, snapshotErr, transitionErr)
	}
}

func TestPreMutationMarkerFailureCancelsWithoutHelperMutationAndRetryIsIdempotent(t *testing.T) {
	workflow, helper, _, store := newFakeCIWorkflow(t, nil)
	baseline := compositionEndpointPlan(t)
	applyCompositionWorkflowPlan(t, &workflow, baseline, "pre-mutation-base")
	before, err := workflow.Contributions.FirewallAuthority(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var udpEndpoint EndpointPolicy
	for _, endpoint := range baseline.Endpoints {
		if endpoint.Key.Network == hostresources.NetworkUDP {
			udpEndpoint = endpoint
			break
		}
	}
	udpPlan := compositionPlanForUDP(t, baseline, udpEndpoint)
	prepared, err := workflow.Prepare(t.Context(), PrepareInput{Plan: udpPlan, Actor: "composition-test", IdempotencyKey: "pre-mutation-udp", Confirmation: "PREPARE SERVER PROTECTION " + udpPlan.Revision})
	if err != nil {
		t.Fatal(err)
	}
	workflow.Marker = failingCompositionMarker{}
	store.failState[protectionoperations.StateRolledBack] = errors.New("fixture_terminal_transition_failed")
	applyCalls := helperOperationCount(helper.Requests, protectionhelper.OperationNFTApply)
	rollbackCalls := helperOperationCount(helper.Requests, protectionhelper.OperationNFTRollback)
	result, err := workflow.Apply(t.Context(), ApplyInput{OperationID: prepared.Operation.OperationID, Plan: udpPlan, Resources: udpPlan.Resources, Confirmation: "APPLY SERVER PROTECTION " + prepared.Operation.OperationID})
	if err == nil || result.State != protectionoperations.StateRollingBack {
		t.Fatalf("marker failure result=%#v err=%v", result, err)
	}
	transition, transitionErr := workflow.Contributions.FirewallTransition(t.Context(), prepared.Operation.OperationID)
	if transitionErr != nil || transition.State != "CANCELLED" || transition.MarkerUnixNano != 0 || transition.MutationCompletedUnixNano != 0 {
		t.Fatalf("pre-mutation transition=%#v err=%v", transition, transitionErr)
	}
	if helperOperationCount(helper.Requests, protectionhelper.OperationNFTApply) != applyCalls || helperOperationCount(helper.Requests, protectionhelper.OperationNFTRollback) != rollbackCalls {
		t.Fatal("pre-mutation recovery reached a helper mutation")
	}
	after, err := workflow.Contributions.FirewallAuthority(t.Context())
	if err != nil || after.Composition.Revision != before.Composition.Revision {
		t.Fatalf("pre-mutation recovery changed semantic authority: before=%#v after=%#v err=%v", before.Composition, after.Composition, err)
	}
	delete(store.failState, protectionoperations.StateRolledBack)
	rolling, err := workflow.operation(t.Context(), prepared.Operation.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	completed, err := workflow.finishRollback(t.Context(), rolling, result.ArtifactRevision, true, true)
	if err != nil || completed.State != protectionoperations.StateRolledBack || helperOperationCount(helper.Requests, protectionhelper.OperationNFTApply) != applyCalls || helperOperationCount(helper.Requests, protectionhelper.OperationNFTRollback) != rollbackCalls {
		t.Fatalf("cancelled pre-mutation retry repeated mutation: result=%#v err=%v", completed, err)
	}
}

func TestFirstContributionMarkerFailureKeepsManagedTableAbsent(t *testing.T) {
	workflow, helper, _, _ := newFakeCIWorkflow(t, nil)
	plan := compositionEndpointPlan(t)
	prepared, err := workflow.Prepare(t.Context(), PrepareInput{Plan: plan, Actor: "composition-test", IdempotencyKey: "first-marker-failure", Confirmation: "PREPARE SERVER PROTECTION " + plan.Revision})
	if err != nil {
		t.Fatal(err)
	}
	workflow.Marker = failingCompositionMarker{}
	applyCalls := helperOperationCount(helper.Requests, protectionhelper.OperationNFTApply)
	rollbackCalls := helperOperationCount(helper.Requests, protectionhelper.OperationNFTRollback)
	result, err := workflow.Apply(t.Context(), ApplyInput{OperationID: prepared.Operation.OperationID, Plan: plan, Resources: plan.Resources, Confirmation: "APPLY SERVER PROTECTION " + prepared.Operation.OperationID})
	if err == nil || result.State != protectionoperations.StateRolledBack || helper.ManagedTablePresent ||
		helperOperationCount(helper.Requests, protectionhelper.OperationNFTApply) != applyCalls || helperOperationCount(helper.Requests, protectionhelper.OperationNFTRollback) != rollbackCalls {
		t.Fatalf("first marker failure mutated helper: result=%#v present=%v err=%v", result, helper.ManagedTablePresent, err)
	}
	authority, authorityErr := workflow.Contributions.FirewallAuthority(t.Context())
	if authorityErr != nil || authority.HasComposition || len(authority.Contributions) != 0 {
		t.Fatalf("first marker failure fabricated authority: %#v err=%v", authority, authorityErr)
	}
}

func TestRollbackBlocksWhenSameContributionHasNewerSemanticOwner(t *testing.T) {
	workflow, helper, _, _ := newFakeCIWorkflow(t, nil)
	baseline1 := compositionEndpointPlan(t)
	applyCompositionWorkflowPlan(t, &workflow, baseline1, "conflict-base-1")
	baseline2 := compositionBaselineWithTCPPort(baseline1, 8443)
	operation2 := applyCompositionWorkflowPlan(t, &workflow, baseline2, "conflict-base-2")
	baseline3 := compositionBaselineWithTCPPort(baseline1, 9443)
	applyCompositionWorkflowPlan(t, &workflow, baseline3, "conflict-base-3")
	requestsBefore := len(helper.Requests)
	if _, err := workflow.Rollback(t.Context(), operation2.OperationID, "ROLLBACK SERVER PROTECTION "+operation2.OperationID); err == nil || !strings.Contains(err.Error(), ErrContributionConflict.Error()) {
		t.Fatalf("same-contribution conflict was not fenced: %v", err)
	}
	if len(helper.Requests) != requestsBefore {
		t.Fatalf("same-contribution conflict reached helper: %d -> %d", requestsBefore, len(helper.Requests))
	}
}

func helperOperationCount(requests []protectionhelper.Request, operation protectionhelper.Operation) int {
	count := 0
	for _, request := range requests {
		if request.Operation == operation {
			count++
		}
	}
	return count
}

func applyCompositionWorkflowPlan(t *testing.T, workflow *Workflow, plan FirewallPlan, key string) protectionrepository.OperationLockModel {
	t.Helper()
	prepared, err := workflow.Prepare(t.Context(), PrepareInput{Plan: plan, Actor: "composition-test", IdempotencyKey: key, Confirmation: "PREPARE SERVER PROTECTION " + plan.Revision})
	if err != nil {
		t.Fatal(err)
	}
	input := ApplyInput{OperationID: prepared.Operation.OperationID, Plan: plan, Resources: plan.Resources, Confirmation: "APPLY SERVER PROTECTION " + prepared.Operation.OperationID}
	contribution, err := contributionFromPlan(plan)
	if err != nil {
		t.Fatal(err)
	}
	if contribution.Kind == ContributionKindUDPDirect {
		input.PostApplyHealth = func(_ context.Context, fence PostMutationHealthFence) (PostMutationHealthProof, error) {
			if fence.MutationUnixNano <= fence.MarkerUnixNano || fence.ContributionRevision != contribution.SemanticRevision || fence.CompositionRevision == "" || fence.ManagedPlanRevision == "" {
				t.Fatalf("invalid post-mutation fence: %#v", fence)
			}
			completed := time.Now().UTC().UnixNano()
			return PostMutationHealthProof{ProviderInstance: "fixture-provider", Generation: 1, ObservationRevision: strings.Repeat("e", 64), StartedUnixNano: fence.MutationUnixNano, CompletedUnixNano: completed, ExpiresUnixNano: completed + int64(30*time.Second)}, nil
		}
	}
	result, err := workflow.Apply(t.Context(), input)
	if err != nil || result.ActualStatus != "APPLIED" {
		t.Fatalf("apply %s: result=%#v err=%v", key, result, err)
	}
	operation, err := workflow.operation(t.Context(), prepared.Operation.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	return operation
}

func compositionBaselineWithTCPPort(plan FirewallPlan, port int) FirewallPlan {
	result := cloneFirewallPlan(plan)
	result.AllowTCPPorts = append(result.AllowTCPPorts, port)
	slices.Sort(result.AllowTCPPorts)
	result.AllowTCPPorts = slices.Compact(result.AllowTCPPorts)
	result.Revision = firewallPlanRevision(result)
	return result
}

func compositionPlanForUDP(t *testing.T, baseline FirewallPlan, endpoint EndpointPolicy) FirewallPlan {
	t.Helper()
	value := compositionUDPContribution(t, baseline, endpoint)
	policy := *value.UDPPolicy
	plan, err := AttachUDPFlowPolicy(baseline, endpoint.EndpointRevision, policy)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func compositionEndpoint(plan FirewallPlan, network hostresources.Network, family hostresources.AddressFamily) EndpointPolicy {
	for _, endpoint := range plan.Endpoints {
		if endpoint.Key.Network == network && endpoint.Key.AddressFamily == family {
			return endpoint
		}
	}
	return EndpointPolicy{}
}

func contributionRevision(values []ManagedFirewallContributionV1, id string) string {
	for _, value := range values {
		if value.ContributionID == id {
			return value.SemanticRevision
		}
	}
	return ""
}

func compositionAuthoritySnapshot(t *testing.T, values []ManagedFirewallContributionV1) protectionrepository.FirewallAuthoritySnapshot {
	t.Helper()
	composition, err := composeFirewall(values)
	if err != nil {
		t.Fatal(err)
	}
	compositionRow, err := compositionModel(composition)
	if err != nil {
		t.Fatal(err)
	}
	models := make([]protectionrepository.FirewallContributionModel, 0, len(values))
	for _, value := range values {
		model, modelErr := contributionModel(value)
		if modelErr != nil {
			t.Fatal(modelErr)
		}
		models = append(models, model)
	}
	return protectionrepository.FirewallAuthoritySnapshot{Contributions: models, Composition: compositionRow, HasComposition: true}
}

func compositionEndpointPlan(t *testing.T) FirewallPlan {
	t.Helper()
	now := time.Unix(1000, 0).UTC()
	configRevision, ownerRevision := strings.Repeat("c", 64), strings.Repeat("b", 64)
	expected := endpointResourceFixture().Capabilities.ExpectedListenerOwner
	resource4 := hostresources.ProtectableResource{ID: "core:inbound:composition4", Kind: "inbound", Owner: "core", Protocol: "stream", Listen: "192.0.2.8", Port: 443, Public: true, Source: "fixture", Capabilities: hostresources.ProtectableResourceCapabilities{Known: true, OwnerRevision: ownerRevision, ConfigRevision: configRevision, ExpectedListenerOwner: expected}}
	resource6 := hostresources.ProtectableResource{ID: "core:inbound:composition6", Kind: "inbound", Owner: "core", Protocol: "udp", Listen: "2001:db8::8", Port: 443, Public: true, Source: "fixture", Capabilities: hostresources.ProtectableResourceCapabilities{Known: true, OwnerRevision: ownerRevision, ConfigRevision: configRevision, ExpectedListenerOwner: expected}}
	resources := []hostresources.ProtectableResource{resource4, resource6}
	keys := [][]hostresources.PublicEndpointKey{{
		{Network: hostresources.NetworkTCP, AddressFamily: hostresources.AddressFamilyIPv4, BindAddress: "192.0.2.8", Port: 443},
		{Network: hostresources.NetworkUDP, AddressFamily: hostresources.AddressFamilyIPv4, BindAddress: "192.0.2.8", Port: 443},
	}, {
		{Network: hostresources.NetworkUDP, AddressFamily: hostresources.AddressFamilyIPv6, BindAddress: "2001:db8::8", Port: 443},
	}}
	var surfaces []hostsurface.HostSurfaceFactV1
	index := 0
	for resourceIndex := range resources {
		for _, key := range keys[resourceIndex] {
			resources[resourceIndex].ListenIntents = append(resources[resourceIndex].ListenIntents, hostresources.ConfiguredListenIntentV1{Schema: hostresources.ConfiguredListenIntentSchemaV1, Mode: hostresources.ListenIntentExact, Network: key.Network, Address: key.BindAddress, Port: key.Port, RequiredFamilies: []hostresources.AddressFamily{key.AddressFamily}, ConfigurationRevision: configRevision})
			endpoint := hostresources.PublicEndpoint{Schema: hostresources.EndpointSchemaV1, ID: "endpoint:" + string(key.Network) + ":" + string(key.AddressFamily), Key: key, Intent: hostresources.EndpointIntentPublic, Protocol: string(key.Network), ResourceID: resources[resourceIndex].ID, Owner: resources[resourceIndex].Owner, OwnerRevision: ownerRevision, ConfigurationRevision: configRevision, ObservedAt: now.Unix(), Source: "fixture", ConfidenceBP: 10000}
			resources[resourceIndex].Endpoints = append(resources[resourceIndex].Endpoints, endpoint)
			surfaces = append(surfaces, compositionOwnerSurface(resources[resourceIndex], key, index, now))
			index++
		}
	}
	graph := protectionresources.BuildSocketOwnershipGraph(protectionresources.SocketGraphInput{Resources: resources, Surfaces: surfaces, Now: now})
	plan := BuildEndpointPlan(EndpointPlanInput{Graph: graph, Resources: resources, Now: now})
	if err := Preflight(plan); err != nil {
		t.Fatalf("fixture preflight: %v reasons=%v graph=%v", err, plan.ReasonCodes, graph.ReasonCodes)
	}
	return plan
}

func compositionOwnerSurface(resource hostresources.ProtectableResource, key hostresources.PublicEndpointKey, index int, now time.Time) hostsurface.HostSurfaceFactV1 {
	expected := resource.Capabilities.ExpectedListenerOwner
	pid := 100 + index
	process := hostsurface.ProcessFact{PID: &pid, ParentPID: endpointIntPtr(1), SessionID: &pid, StartTime: "1000", ExeDigest: expected.ExecutableSHA256, Executable: expected.ExecutablePath, ExeDevice: 1, ExeInode: uint64(2 + index), UID: endpointIntPtr(0), GID: endpointIntPtr(0), ControlGroup: expected.ServiceControlGroup}
	service := hostsurface.ServiceFact{SystemdUnit: expected.SystemdUnit, MainPID: &pid, FragmentPath: expected.ServiceFragmentPath, FragmentSHA256: expected.ServiceUnitSHA256, ActiveState: "active", SubState: "running", ControlGroup: expected.ServiceControlGroup, StartMonotonicUsec: uint64(100 + index)}
	fact := hostsurface.ListenerOwnerFactV1{Schema: hostsurface.ListenerOwnerFactSchemaV1,
		Socket:  hostsurface.ListenerSocketIdentityV1{Network: hostsurface.Network(key.Network), Family: hostsurface.Family(key.AddressFamily), Bind: key.BindAddress, Port: key.Port, Inode: string(rune(1000 + index)), Cookie: uint64(2000 + index), CoverageFamilies: []hostsurface.Family{hostsurface.Family(key.AddressFamily)}},
		Process: process, Service: service,
		Application: hostsurface.ListenerApplicationIdentityV1{InstanceID: expected.InstanceID, SourceRevision: expected.SourceRevision, ArtifactRevision: expected.ArtifactRevision, DeploymentID: expected.DeploymentID, OwnerContractRevision: expected.ContractRevision, RuntimeRootBindingRevision: expected.RuntimeRootBindingRevision, ExpectedExecutableSHA256: expected.ExecutableSHA256, ServiceIdentity: expected.ServiceIdentity, ResourceID: resource.ID, ResourceOwnerRevision: resource.Capabilities.OwnerRevision, ConfigurationRevision: resource.Capabilities.ConfigRevision},
		ObservedAt:  now.Unix(), ExpiresAt: now.Add(30 * time.Second).Unix()}
	fact.Seal()
	return hostsurface.HostSurfaceFactV1{Schema: hostsurface.SchemaV1, ID: "surface:" + string(key.Network) + ":" + string(key.AddressFamily), Network: fact.Socket.Network, Family: fact.Socket.Family, Bind: key.BindAddress, Port: key.Port, Exposure: hostsurface.ExposurePublic, SocketInode: fact.Socket.Inode, SocketCookie: fact.Socket.Cookie, Process: process, Service: service, ListenerOwner: &fact, RegisteredResourceID: resource.ID, DesiredOwner: resource.Owner, OwnershipMode: hostsurface.OwnershipManaged, FirstSeen: now.Unix(), LastSeen: now.Unix(), ExpiresAt: fact.ExpiresAt, Source: "fixture", ConfidenceBP: 10000, ConfigurationRevision: resource.Capabilities.ConfigRevision, Classification: hostsurface.ClassificationManagedExact}
}

func compositionUDPContribution(t *testing.T, baseline FirewallPlan, endpoint EndpointPolicy) ManagedFirewallContributionV1 {
	t.Helper()
	revision := strings.Repeat("d", 64)
	policy := FinalizeUDPFlowPolicy(UDPFlowPolicyV1{ResourceID: endpoint.ResourceID, EndpointID: endpoint.EndpointRevision, AddressFamily: endpoint.Key.AddressFamily, Protocol: hostresources.NetworkUDP,
		ExactSocketRevision: revision, ManagementExclusionRevision: revision, TrustedExclusionRevision: revision, RateProfile: "BALANCED_V1", CardinalityProfile: "BOUNDED_4096_V1",
		ConntrackPolicy: "ADVISORY_NEW_FLOW_V1", ICMPPolicy: "PRESERVE_ICMP_AND_ICMPV6_V1", ExpectedManagedTableRevision: baseline.Revision, OperationRevision: revision, PlanRevision: revision})
	guarded, err := AttachUDPFlowPolicy(baseline, endpoint.EndpointRevision, policy)
	if err != nil {
		t.Fatalf("attach %s/%s: %v", endpoint.ResourceID, endpoint.Key.AddressFamily, err)
	}
	value, err := contributionFromPlan(guarded)
	if err != nil {
		t.Fatalf("derive %s/%s: %v endpoints=%#v", endpoint.ResourceID, endpoint.Key.AddressFamily, err, guarded.Endpoints)
	}
	return value
}

func assertCompositionPolicies(t *testing.T, plan FirewallPlan, ipv4, ipv6 bool) {
	t.Helper()
	want := map[hostresources.AddressFamily]bool{hostresources.AddressFamilyIPv4: ipv4, hostresources.AddressFamilyIPv6: ipv6}
	for _, endpoint := range plan.Endpoints {
		if endpoint.Key.Network == hostresources.NetworkUDP && (endpoint.UDPFlowPolicy != nil) != want[endpoint.Key.AddressFamily] {
			t.Fatalf("policy presence for %s = %v, want %v", endpoint.Key.AddressFamily, endpoint.UDPFlowPolicy != nil, want[endpoint.Key.AddressFamily])
		}
	}
}

func policyFamilies(plan FirewallPlan) []hostresources.AddressFamily {
	var result []hostresources.AddressFamily
	for _, endpoint := range plan.Endpoints {
		if endpoint.Key.Network == hostresources.NetworkUDP && endpoint.UDPFlowPolicy != nil {
			result = append(result, endpoint.Key.AddressFamily)
		}
	}
	slices.Sort(result)
	return result
}
