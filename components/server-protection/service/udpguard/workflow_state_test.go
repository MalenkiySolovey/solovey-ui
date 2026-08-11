package udpguard

import (
	"slices"
	"strings"
	"testing"
	"time"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	protectionfirewall "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/firewall"
	protectionoperations "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
)

func TestPreviewProjectionIsAlwaysNotApplied(t *testing.T) {
	plan := UDPDirectGuardPlanV1{ActualState: StateAppliedExperimental,
		LatestOperationID: "operation:historical", LatestOperationRevision: 9, RecoveryRequired: true}
	preview := previewProjection(plan)
	if preview.ActualState != StateNotApplied || preview.LatestOperationID != "" || preview.LatestOperationRevision != 0 || preview.RecoveryRequired {
		t.Fatalf("preview projected historical actual state: %#v", preview)
	}
	if plan.ActualState != StateAppliedExperimental || plan.LatestOperationID == "" {
		t.Fatal("preview projection mutated its source plan")
	}
}

func TestHealthProofStateBindsAuthorityMutationAndFreshness(t *testing.T) {
	now := time.Now().UTC()
	revision := strings.Repeat("a", 64)
	state := protectionrepository.UDPGuardStateV1Model{LatestOperationID: "operation", ContributionID: "udp:one", ContributionRevision: revision, CompositionRevision: revision, ManagedPlanRevision: revision,
		HealthProviderInstance: "provider:one", HealthGeneration: 7, HealthObservationRevision: revision, HealthStartedUnixNano: now.Add(-time.Second).UnixNano(), HealthCompletedUnixNano: now.Add(-500 * time.Millisecond).UnixNano(), HealthExpiresUnixNano: now.Add(30 * time.Second).UnixNano()}
	transition := protectionrepository.FirewallContributionTransitionModel{OperationID: state.LatestOperationID, State: "HEALTH_VERIFIED", ContributionID: state.ContributionID, DesiredSemanticRevision: state.ContributionRevision,
		AfterCompositionRevision: state.CompositionRevision, ManagedPlanRevision: state.ManagedPlanRevision, MarkerUnixNano: now.Add(-3 * time.Second).UnixNano(), MutationCompletedUnixNano: now.Add(-2 * time.Second).UnixNano(),
		HealthProviderInstance: state.HealthProviderInstance, HealthGeneration: state.HealthGeneration, HealthObservationRevision: state.HealthObservationRevision,
		HealthStartedUnixNano: state.HealthStartedUnixNano, HealthCompletedUnixNano: state.HealthCompletedUnixNano, HealthExpiresUnixNano: state.HealthExpiresUnixNano}
	authority := protectionrepository.FirewallAuthoritySnapshot{HasComposition: true, Composition: protectionrepository.FirewallCompositionModel{State: "ACTIVE", Revision: revision, ManagedPlanRevision: revision}}
	if exact, fresh := healthProofState(state, transition, authority, now); !exact || !fresh {
		t.Fatalf("valid health proof rejected: exact=%v fresh=%v", exact, fresh)
	}
	staleState := state
	staleState.HealthExpiresUnixNano = now.Add(-time.Nanosecond).UnixNano()
	staleTransition := transition
	staleTransition.HealthExpiresUnixNano = staleState.HealthExpiresUnixNano
	if exact, fresh := healthProofState(staleState, staleTransition, authority, now); !exact || fresh {
		t.Fatalf("stale health proof disposition: exact=%v fresh=%v", exact, fresh)
	}
	drifted := transition
	drifted.MutationCompletedUnixNano = state.HealthStartedUnixNano + 1
	if exact, _ := healthProofState(state, drifted, authority, now); exact {
		t.Fatal("pre-mutation observation was accepted")
	}
}

func TestActualStateFollowsJournalInsteadOfHistoricalProjection(t *testing.T) {
	revision := strings.Repeat("a", 64)
	newPlan := func() UDPDirectGuardPlanV1 {
		return UDPDirectGuardPlanV1{PlanDigest: revision, CapabilityRevision: revision,
			Claim:      UDPConfiguredSocketClaimV1{ClaimRevision: revision},
			FlowPolicy: protectionfirewall.UDPFlowPolicyV1{Revision: revision}, ApplyGate: ApplyGateExperimentalOff}
	}
	state := protectionrepository.UDPGuardStateV1Model{ActualState: string(StateAppliedExperimental), PlanDigest: revision,
		CapabilityRevision: revision, ClaimRevision: revision, PolicyRevision: revision, OwnsActiveContribution: true, RecoverableArtifact: true}
	plan := newPlan()
	projectActualState(&plan, state, protectionrepository.OperationLockModel{Kind: protectionoperations.KindFirewall, State: protectionoperations.StateRolledBack}, true, true, false, false)
	if plan.ActualState != StateNotApplied || plan.RecoveryRequired {
		t.Fatalf("rolled-back journal fabricated active state: %#v", plan)
	}
	state.ActualState = string(StatePrepared)
	state.OwnsActiveContribution = false
	plan = newPlan()
	projectActualState(&plan, state, protectionrepository.OperationLockModel{Kind: protectionoperations.KindFirewall, State: protectionoperations.StateApplied}, true, true, true, true)
	if plan.ActualState != StateAppliedExperimental {
		t.Fatalf("applied journal was hidden by stale prepared row: %#v", plan)
	}
	state.RecoveryRequired = true
	plan = newPlan()
	projectActualState(&plan, state, protectionrepository.OperationLockModel{Kind: protectionoperations.KindFirewall, State: protectionoperations.StateApplied}, true, true, true, true)
	if plan.ActualState != StateRecoveryRequired || !plan.RecoveryRequired {
		t.Fatalf("restored active-like row regained applied state: %#v", plan)
	}
	state.RecoveryRequired = false
	state.ActualState = string(StateAppliedExperimental)
	plan = newPlan()
	projectActualState(&plan, state, protectionrepository.OperationLockModel{}, false, true, false, false)
	if plan.ActualState != StateRecoveryRequired {
		t.Fatalf("historical active row without journal authority stayed applied: %#v", plan)
	}
}

func TestActualStateRequiresExactFreshPostMutationHealthProof(t *testing.T) {
	revision := strings.Repeat("a", 64)
	state := protectionrepository.UDPGuardStateV1Model{ActualState: string(StateAppliedExperimental), PlanDigest: revision, CapabilityRevision: revision, ClaimRevision: revision, PolicyRevision: revision, OwnsActiveContribution: true}
	operation := protectionrepository.OperationLockModel{Kind: protectionoperations.KindFirewall, State: protectionoperations.StateApplied}
	newPlan := func() UDPDirectGuardPlanV1 {
		return UDPDirectGuardPlanV1{PlanDigest: revision, CapabilityRevision: revision, Claim: UDPConfiguredSocketClaimV1{ClaimRevision: revision}, FlowPolicy: protectionfirewall.UDPFlowPolicyV1{Revision: revision}, ApplyGate: ApplyGateExperimentalOff}
	}
	plan := newPlan()
	projectActualState(&plan, state, operation, true, true, false, false)
	if plan.ActualState != StateRecoveryRequired {
		t.Fatalf("missing exact health proof became applied: %#v", plan)
	}
	plan = newPlan()
	projectActualState(&plan, state, operation, true, true, true, false)
	if plan.ActualState != StateDegraded || !slices.Contains(plan.BlockCodes, "BLOCKED_STALE_HEALTH") {
		t.Fatalf("expired health remained applied: %#v", plan)
	}
}

func TestOrphanedActiveSocketBindingBlocksOnlyItsAddressFamily(t *testing.T) {
	revision := strings.Repeat("a", 64)
	status := StatusV1{
		Capabilities: []CapabilityStatusV1{{ResourceID: "core:inbound:one", ActualState: StateNotApplied, ApplyGate: ApplyGateExperimentalOff}},
		Plans: []UDPDirectGuardPlanV1{
			{ResourceID: "core:inbound:one", EndpointID: "current-v4", Claim: UDPConfiguredSocketClaimV1{AddressFamily: hostresources.AddressFamilyIPv4}, ActualState: StateNotApplied, ApplyGate: ApplyGateExperimentalOff},
			{ResourceID: "core:inbound:one", EndpointID: "current-v6", Claim: UDPConfiguredSocketClaimV1{AddressFamily: hostresources.AddressFamilyIPv6}, ActualState: StateNotApplied, ApplyGate: ApplyGateExperimentalOff},
		},
	}
	states := []protectionrepository.UDPGuardStateV1Model{{ResourceID: "core:inbound:one", EndpointID: "stale-v4", AddressFamily: string(hostresources.AddressFamilyIPv4), ActualState: string(StateAppliedExperimental), ContributionID: "udp:one", ContributionRevision: revision, OwnsActiveContribution: true}}
	authority := protectionrepository.FirewallAuthoritySnapshot{Contributions: []protectionrepository.FirewallContributionModel{{ContributionID: "udp:one", SemanticRevision: revision}}}
	projectOrphanedAuthority(&status, states, authority)
	if status.Plans[0].ActualState != StateRecoveryRequired || status.Plans[0].ApplyGate != ApplyGateBlocked || !slices.Contains(status.Plans[0].BlockCodes, "RECOVERY_REQUIRED_STALE_SOCKET_BINDING") {
		t.Fatalf("orphaned IPv4 authority was not surfaced: %#v", status.Plans[0])
	}
	if status.Plans[1].ActualState != StateNotApplied || status.Plans[1].ApplyGate != ApplyGateExperimentalOff {
		t.Fatalf("independent IPv6 state was blocked: %#v", status.Plans[1])
	}
	if status.Capabilities[0].ActualState != StateRecoveryRequired || status.Capabilities[0].ApplyGate != ApplyGateBlocked {
		t.Fatalf("capability did not report partial recovery state: %#v", status.Capabilities[0])
	}
}
