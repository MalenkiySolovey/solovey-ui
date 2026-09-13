package firewall

import (
	"context"
	"errors"

	protectionoperations "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
)

// RuntimeLossReconciler owns restart classification for an already APPLIED
// managed-table generation. Sealed authority in a proven new boot enters the
// workflow's bounded runtime continuation. Same-boot loss and unsealed legacy
// authority retain safe retirement. Ambiguous state remains reconcile-required.
type RuntimeLossReconciler struct {
	Workflow *Workflow
}

// NeedsReconcile leaves retained terminal authority inert. The observation has
// its own freshness owner; observing it does not create a new operation fence.
// A changed fact that permits continuation is rechecked after the manager CAS.
func (r RuntimeLossReconciler) NeedsReconcile(ctx context.Context, operation protectionrepository.OperationLockModel) (bool, error) {
	if r.Workflow == nil || operation.Kind != protectionoperations.KindFirewall {
		return true, nil
	}
	snapshot, err := r.Workflow.Contributions.FirewallAuthority(ctx)
	if err != nil {
		return false, &protectionoperations.RuntimeRecoveryError{Step: "firewall_authority_inspection", Cause: err}
	}
	if !snapshot.HasComposition {
		return true, nil
	}
	binding := snapshot.Composition.Runtime
	if snapshot.Composition.AppliedOperationID != operation.OperationID || binding.Schema != protectionrepository.FirewallRuntimeSchema {
		return true, nil
	}
	if operation.State == protectionoperations.StateReconcileRequired && retainedRuntimeFailure(binding) {
		// These failure classes never authorize automatic continuation or
		// retirement, even if a later observation changes. Only the operator
		// lifecycle may resolve them after proving current safety.
		return false, nil
	}
	if operation.State == protectionoperations.StateReconcileRequired && binding.State == "RESTORE_FAILED" {
		// The existing failure-cleanup path still requires this attempt's
		// current generation. An unavailable prerequisite cannot justify a
		// new durable claim on every tick. A later valid generation is read
		// again here and permits the normal owner to continue under its fence.
		if r.Workflow.Runtime == nil || r.Workflow.RuntimeStore == nil || r.Workflow.CurrentRuntimeManagement == nil {
			return false, nil
		}
		boot, err := r.Workflow.Runtime.Generation(ctx)
		if err != nil || !validFirewallSHA(boot) || binding.AttemptBoot != boot ||
			binding.StartedAt <= 0 || binding.StartedAt > r.Workflow.now().Unix() || binding.Deadline <= binding.StartedAt || binding.Deadline-binding.StartedAt > 300 {
			return false, nil
		}
		observation, err := r.Workflow.ReconcileAuthority(ctx)
		if err != nil {
			if observation.Persisted {
				return false, nil
			}
			return false, &protectionoperations.RuntimeRecoveryError{Step: "firewall_live_authority_inspection", Cause: err}
		}
		// Failed cleanup cannot be retried against an unchanged live table.
		// Absence is a new fact that can permit the existing retirement path;
		// a matching table permits at most the first bounded cleanup attempt.
		return observation.State == FirewallLiveAbsent || observation.State == FirewallLiveMatching && !binding.CleanupAttempted, nil
	}
	if operation.State == protectionoperations.StateApplied && binding.State == "HEALTH_VERIFIED" && r.Workflow.Runtime != nil {
		boot, err := r.Workflow.Runtime.Generation(ctx)
		if err == nil && boot == binding.VerifiedBoot {
			observation, err := r.Workflow.ReconcileAuthority(ctx)
			if err == nil && observation.State == FirewallLiveMatching {
				return false, nil
			}
		}
	}
	return true, nil
}

func (r RuntimeLossReconciler) Reconcile(ctx context.Context, operation protectionrepository.OperationLockModel) (protectionoperations.ReconcileDecision, error) {
	if r.Workflow == nil || operation.Kind != protectionoperations.KindFirewall ||
		(operation.State != protectionoperations.StateApplied && operation.State != protectionoperations.StateReconcileRequired && operation.State != protectionoperations.StateRestoringRuntime) {
		return protectionoperations.ReconcileDecision{}, errors.New("firewall runtime-loss reconciliation fence is invalid")
	}
	observation, err := r.Workflow.ReconcileAuthority(ctx)
	if err != nil {
		if observation.Persisted {
			return protectionoperations.ReconcileDecision{State: protectionoperations.StateReconcileRequired, Reason: "firewall_live_authority_unavailable"}, nil
		}
		return protectionoperations.ReconcileDecision{}, &protectionoperations.RuntimeRecoveryError{Step: "firewall_live_authority_observation", Cause: err}
	}
	if decision, handled, restoreErr := r.Workflow.reconcileRuntime(ctx, operation, observation); handled {
		if restoreErr != nil {
			return decision, &protectionoperations.RuntimeRecoveryError{Step: decision.Reason, Cause: restoreErr}
		}
		return decision, restoreErr
	}
	switch observation.State {
	case FirewallLiveMatching:
		return protectionoperations.ReconcileDecision{State: protectionoperations.StateApplied, Reason: "firewall_live_authority_reverified"}, nil
	case FirewallLiveAbsent:
		snapshot, loadErr := r.Workflow.Contributions.FirewallAuthority(ctx)
		if loadErr != nil {
			return protectionoperations.ReconcileDecision{}, loadErr
		}
		expectedComposition := ""
		if snapshot.HasComposition {
			expectedComposition = snapshot.Composition.Revision
		}
		if retireErr := r.Workflow.Contributions.RetireFirewallAuthorityAfterRuntimeLoss(ctx, operation.OperationID, operation.Revision, expectedComposition); retireErr != nil {
			return protectionoperations.ReconcileDecision{}, retireErr
		}
		return protectionoperations.ReconcileDecision{State: protectionoperations.StateForgotten, Reason: "firewall_runtime_authority_retired"}, nil
	default:
		return protectionoperations.ReconcileDecision{State: protectionoperations.StateReconcileRequired, Reason: "firewall_live_authority_not_matching"}, nil
	}
}
