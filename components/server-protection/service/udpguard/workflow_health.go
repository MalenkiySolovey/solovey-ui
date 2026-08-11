package udpguard

import (
	"context"
	"errors"
	"time"

	componenthealth "github.com/MalenkiySolovey/solovey-ui/componenthost/health"
	protectionfirewall "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/firewall"
	protectionoperations "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
)

func (c *Controller) verifyPostApplyHealth(ctx context.Context, expected UDPDirectGuardPlanV1, fence protectionfirewall.PostMutationHealthFence) (protectionfirewall.PostMutationHealthProof, error) {
	if !c.activeHealthFence(ctx, fence) {
		return protectionfirewall.PostMutationHealthProof{}, protectionfirewall.ErrHealthFailed
	}
	current, err := c.Status(ctx, true)
	if err != nil {
		return protectionfirewall.PostMutationHealthProof{}, err
	}
	for _, candidate := range current.Plans {
		if !sameHealthBinding(candidate, expected) {
			continue
		}
		proof, probeErr := c.probeFresh(ctx, candidate, fence)
		if probeErr != nil || !c.activeHealthFence(ctx, fence) {
			return protectionfirewall.PostMutationHealthProof{}, errors.Join(protectionfirewall.ErrHealthFailed, probeErr)
		}
		after, afterErr := c.Status(ctx, true)
		if afterErr != nil {
			return protectionfirewall.PostMutationHealthProof{}, afterErr
		}
		for _, afterPlan := range after.Plans {
			if sameHealthBinding(afterPlan, expected) {
				return proof, nil
			}
		}
		return protectionfirewall.PostMutationHealthProof{}, protectionfirewall.ErrHealthFailed
	}
	return protectionfirewall.PostMutationHealthProof{}, protectionfirewall.ErrHealthFailed
}

func (c *Controller) activeHealthFence(ctx context.Context, fence protectionfirewall.PostMutationHealthFence) bool {
	if c == nil || c.Repository == nil || fence.MarkerUnixNano <= 0 || fence.MutationUnixNano <= fence.MarkerUnixNano {
		return false
	}
	authority, err := c.Repository.FirewallAuthority(ctx)
	if err != nil || !authority.HasComposition || authority.Composition.State != "ACTIVE" ||
		authority.Composition.Revision != fence.CompositionRevision || authority.Composition.ManagedPlanRevision != fence.ManagedPlanRevision {
		return false
	}
	for _, contribution := range authority.Contributions {
		if contribution.ContributionID == fence.ContributionID && contribution.SemanticRevision == fence.ContributionRevision {
			return true
		}
	}
	return false
}

func (c *Controller) probeFresh(ctx context.Context, plan UDPDirectGuardPlanV1, fence protectionfirewall.PostMutationHealthFence) (protectionfirewall.PostMutationHealthProof, error) {
	if fence.MutationUnixNano <= fence.MarkerUnixNano {
		return protectionfirewall.PostMutationHealthProof{}, protectionfirewall.ErrHealthFailed
	}
	target := componenthealth.ProtocolProbeTargetV1{
		ResourceID: plan.ResourceID, EndpointID: plan.EndpointID, ProtocolClass: plan.StrategyClass,
		RuntimeRevision: plan.Claim.RuntimeGenerationRevision, CapabilityRevision: plan.CapabilityRevision,
		ConfigurationRevision: plan.Claim.ConfigurationRevision, SocketRevision: plan.Claim.ClaimRevision,
		AddressFamily: plan.Claim.AddressFamily, ConfiguredBind: plan.Claim.ConfiguredBind, Port: plan.Claim.Port,
	}
	capability := c.probes().Capability(ctx, target)
	if !capability.Available || capability.Revision != plan.HealthRevision {
		return protectionfirewall.PostMutationHealthProof{}, protectionfirewall.ErrHealthFailed
	}
	observation, err := c.probes().ProbeFresh(ctx, componenthealth.ProtocolProbeRequestV1{
		Target: target, ContributionRevision: fence.ContributionRevision, CompositionRevision: fence.CompositionRevision,
		ManagedPlanRevision: fence.ManagedPlanRevision, ProviderInstance: capability.ProviderInstance,
		MinimumGeneration: 1, NotBeforeUnixNano: fence.MutationUnixNano,
	})
	if err != nil {
		return protectionfirewall.PostMutationHealthProof{}, err
	}
	return protectionfirewall.PostMutationHealthProof{
		ProviderInstance: observation.ProviderInstance, Generation: observation.Generation,
		ObservationRevision: observation.Revision, StartedUnixNano: observation.StartedUnixNano,
		CompletedUnixNano: observation.CompletedUnixNano, ExpiresUnixNano: observation.ExpiresUnixNano,
	}, nil
}

func sameHealthBinding(current, expected UDPDirectGuardPlanV1) bool {
	return current.PlanDigest == expected.PlanDigest && current.ResourceID == expected.ResourceID && current.EndpointID == expected.EndpointID &&
		current.CapabilityRevision == expected.CapabilityRevision && current.BuildFeatureRevision == expected.BuildFeatureRevision && current.StrategyClass == expected.StrategyClass &&
		current.Claim.ClaimRevision == expected.Claim.ClaimRevision && current.Claim.ConfigurationRevision == expected.Claim.ConfigurationRevision &&
		current.Claim.RuntimeGenerationRevision == expected.Claim.RuntimeGenerationRevision && current.FirewallBaselineRevision == expected.FirewallBaselineRevision &&
		current.ManagementExclusionRevision == expected.ManagementExclusionRevision && current.HealthRevision == expected.HealthRevision &&
		current.FlowPolicy.Revision == expected.FlowPolicy.Revision
}

func healthProofState(state protectionrepository.UDPGuardStateV1Model, transition protectionrepository.FirewallContributionTransitionModel, authority protectionrepository.FirewallAuthoritySnapshot, now time.Time) (bool, bool) {
	exact := transition.OperationID == state.LatestOperationID && transition.State == "HEALTH_VERIFIED" && transition.ContributionID == state.ContributionID &&
		transition.DesiredSemanticRevision == state.ContributionRevision && transition.AfterCompositionRevision == state.CompositionRevision &&
		transition.ManagedPlanRevision == state.ManagedPlanRevision && transition.MarkerUnixNano > 0 && transition.MutationCompletedUnixNano > transition.MarkerUnixNano &&
		transition.HealthProviderInstance != "" && transition.HealthProviderInstance == state.HealthProviderInstance && transition.HealthGeneration != 0 &&
		transition.HealthGeneration == state.HealthGeneration && transition.HealthObservationRevision == state.HealthObservationRevision && validRevision(state.HealthObservationRevision) &&
		transition.HealthStartedUnixNano == state.HealthStartedUnixNano && transition.HealthCompletedUnixNano == state.HealthCompletedUnixNano &&
		transition.HealthExpiresUnixNano == state.HealthExpiresUnixNano && transition.HealthStartedUnixNano >= transition.MutationCompletedUnixNano &&
		transition.HealthCompletedUnixNano >= transition.HealthStartedUnixNano && transition.HealthExpiresUnixNano > transition.HealthCompletedUnixNano &&
		transition.HealthExpiresUnixNano-transition.HealthCompletedUnixNano <= componenthealth.MaxProtocolProbeFreshness.Nanoseconds() && authority.HasComposition &&
		authority.Composition.State == "ACTIVE" && authority.Composition.Revision == state.CompositionRevision && authority.Composition.ManagedPlanRevision == state.ManagedPlanRevision
	return exact, exact && state.HealthExpiresUnixNano > now.UTC().UnixNano()
}

func validRevision(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' && character < 'a' || character > 'f' {
			return false
		}
	}
	return true
}

func projectActualState(plan *UDPDirectGuardPlanV1, state protectionrepository.UDPGuardStateV1Model, operation protectionrepository.OperationLockModel, operationExists, contributionActive, healthProofExact, healthProofFresh bool) {
	identityMatches := state.PlanDigest == plan.PlanDigest && state.CapabilityRevision == plan.CapabilityRevision &&
		state.ClaimRevision == plan.Claim.ClaimRevision && state.PolicyRevision == plan.FlowPolicy.Revision
	activeLike := state.ActualState != string(StateNotApplied) || state.OwnsActiveContribution || state.RecoverableArtifact || contributionActive
	if state.RecoveryRequired {
		plan.ActualState, plan.RecoveryRequired = StateRecoveryRequired, true
		return
	}
	if state.OwnsActiveContribution && !contributionActive {
		plan.ActualState, plan.RecoveryRequired = StateRecoveryRequired, true
		plan.BlockCodes = append(plan.BlockCodes, "BLOCKED_MANAGED_CONTRIBUTION_DRIFT")
		return
	}
	if !identityMatches {
		if activeLike {
			plan.ActualState, plan.RecoveryRequired = StateRecoveryRequired, true
			plan.BlockCodes = append(plan.BlockCodes, CodeRevisionDrift)
		}
		return
	}
	if !operationExists || operation.Kind != protectionoperations.KindFirewall {
		if activeLike {
			plan.ActualState, plan.RecoveryRequired = StateRecoveryRequired, true
		}
		return
	}
	switch operation.State {
	case protectionoperations.StatePrepared:
		plan.ActualState = StatePrepared
	case protectionoperations.StateApplied:
		if !healthProofExact {
			plan.ActualState, plan.RecoveryRequired = StateRecoveryRequired, true
			plan.BlockCodes = append(plan.BlockCodes, "BLOCKED_HEALTH_PROOF_DRIFT")
		} else if !healthProofFresh {
			plan.ActualState = StateDegraded
			plan.BlockCodes = append(plan.BlockCodes, "BLOCKED_STALE_HEALTH")
		} else if plan.ApplyGate == ApplyGateBlocked || len(plan.BlockCodes) != 0 {
			plan.ActualState = StateDegraded
		} else {
			plan.ActualState = StateAppliedExperimental
		}
	case protectionoperations.StateRolledBack, protectionoperations.StateCancelled, protectionoperations.StateAbandoned:
		plan.ActualState = StateNotApplied
	case protectionoperations.StateRollingBack:
		plan.ActualState = StateRollingBack
	case protectionoperations.StateApplying, protectionoperations.StateHealth, protectionoperations.StateHealthFailed,
		protectionoperations.StateReconcileRequired, protectionoperations.StateLockSuspect, protectionoperations.StateRollbackFailed:
		plan.ActualState, plan.RecoveryRequired = StateRecoveryRequired, true
	case protectionoperations.StateForceUnlocked, protectionoperations.StateForgotten:
		if state.OwnsActiveContribution || state.RecoverableArtifact {
			plan.ActualState, plan.RecoveryRequired = StateRecoveryRequired, true
		} else {
			plan.ActualState = StateNotApplied
		}
	default:
		if activeLike {
			plan.ActualState, plan.RecoveryRequired = StateRecoveryRequired, true
		}
	}
}

func projectCapabilityStates(status *StatusV1) {
	for index := range status.Capabilities {
		plans := make([]UDPDirectGuardPlanV1, 0, 2)
		for _, plan := range status.Plans {
			if plan.ResourceID == status.Capabilities[index].ResourceID {
				plans = append(plans, plan)
			}
		}
		if len(plans) == 0 {
			continue
		}
		status.Capabilities[index].Observed = true
		allState := plans[0].ActualState
		allSame := true
		for _, plan := range plans {
			if plan.ActualState != allState {
				allSame = false
			}
			if plan.RecoveryRequired || plan.ActualState == StateRecoveryRequired {
				status.Capabilities[index].ActualState = StateRecoveryRequired
				status.Capabilities[index].ApplyGate = ApplyGateBlocked
				allSame = false
				break
			}
		}
		if status.Capabilities[index].ActualState == StateRecoveryRequired {
			continue
		}
		if allSame {
			status.Capabilities[index].ActualState = allState
		} else {
			status.Capabilities[index].ActualState = StateDegraded
		}
		status.Capabilities[index].ApplyGate = plans[0].ApplyGate
	}
}
