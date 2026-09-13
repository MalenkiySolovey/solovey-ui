package firewall

import (
	"context"
	"errors"
	"time"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	helper "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/helper"
	operations "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
	repository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
)

type RuntimeLifecycle interface {
	Generation(context.Context) (string, error)
	RequiresBaseFirewall() bool
}

type RuntimeStore interface {
	UpdateFirewallRuntime(context.Context, string, int, string, repository.FirewallRuntimeBinding, repository.FirewallRuntimeBinding) error
}

func runtimeDecision(reason string) operations.ReconcileDecision {
	return operations.ReconcileDecision{State: operations.StateReconcileRequired, Reason: reason}
}

// reconcileRuntime consumes only committed authority. The caller holds the
// existing operation manager gate; this method never invokes public Apply.
func (w Workflow) reconcileRuntime(ctx context.Context, operation repository.OperationLockModel, observation AuthorityObservation) (operations.ReconcileDecision, bool, error) {
	snapshot, err := w.Contributions.FirewallAuthority(ctx)
	if err != nil {
		return runtimeDecision("firewall_authority_unavailable"), true, err
	}
	if !snapshot.HasComposition {
		return operations.ReconcileDecision{}, false, nil
	}
	binding := snapshot.Composition.Runtime
	if binding.Schema == "" {
		return operations.ReconcileDecision{}, false, nil
	}
	if binding.Schema != repository.FirewallRuntimeSchema || !validFirewallSHA(binding.VerifiedBoot) || w.RuntimeStore == nil {
		return runtimeDecision("firewall_restore_authority_unavailable"), true, nil
	}
	if snapshot.Composition.AppliedOperationID != operation.OperationID {
		// Another contribution's operation cannot retire or restore the aggregate.
		if observation.State == FirewallLiveMatching {
			return operations.ReconcileDecision{State: operations.StateApplied, Reason: "firewall_live_authority_reverified"}, true, nil
		}
		return runtimeDecision("firewall_restore_aggregate_owner_pending"), true, nil
	}
	if observation.State != FirewallLiveAbsent && observation.State != FirewallLiveMatching {
		return runtimeDecision("firewall_restore_foreign_runtime"), true, nil
	}
	values, err := contributionsFromModels(snapshot.Contributions)
	composition, composeErr := composeFirewall(values)
	transition, transitionErr := w.Contributions.FirewallTransition(ctx, operation.OperationID)
	if err != nil || composeErr != nil || transitionErr != nil || transition.Schema != FirewallTransitionSchemaV2 || transition.State != "HEALTH_VERIFIED" || snapshot.Composition.State != "ACTIVE" ||
		!matchesCommittedComposition(composition, snapshot.Composition) ||
		transition.AfterCompositionRevision != composition.Revision || transition.ManagedPlanRevision != composition.PlanRevision || transition.CandidateSHA256 != composition.CandidateSHA || transition.CandidateSemanticSHA256 != composition.CandidateSemanticSHA || transition.CandidateTimedMembershipSHA256 != composition.CandidateTimedMembershipSHA || operation.PlanRevision != composition.PlanRevision || operation.ResourceID != "managed-table:inet:solovey_protection" {
		return runtimeDecision("firewall_restore_integrity_failed"), true, nil
	}
	if err := w.validateRuntimeContributionOwners(ctx, snapshot); err != nil {
		return runtimeDecision("firewall_restore_contribution_owner_invalid"), true, nil
	}
	write := func(next repository.FirewallRuntimeBinding) error {
		if err := w.RuntimeStore.UpdateFirewallRuntime(ctx, operation.OperationID, operation.Revision, composition.Revision, binding, next); err != nil {
			return err
		}
		binding = next
		return nil
	}
	// A bounded attempt's expiry is a durable firewall fact. Terminalizing an
	// absent, never-mutated attempt does not require the capabilities needed
	// to execute a forward restore. Keep that decision before those gates.
	if zeroMutationExpiredRuntime(binding, w.now()) && observation.State == FirewallLiveAbsent {
		next := binding
		next.State, next.Reason = "RESTORE_FAILED", "boot_restore_deadline_expired"
		if err := write(next); err != nil {
			return runtimeDecision(next.Reason), true, err
		}
		return runtimeDecision(next.Reason), true, nil
	}
	if retainedRuntimeFailure(binding) {
		return runtimeDecision(binding.Reason), true, nil
	}
	if w.Runtime == nil || w.CurrentRuntimeManagement == nil {
		return runtimeDecision("firewall_restore_authority_unavailable"), true, nil
	}
	boot, err := w.Runtime.Generation(ctx)
	if err != nil || !validFirewallSHA(boot) {
		return runtimeDecision("firewall_boot_generation_unavailable"), true, nil
	}
	if binding.State == "HEALTH_VERIFIED" && binding.VerifiedBoot == boot {
		return operations.ReconcileDecision{}, false, nil
	}
	if binding.State == "HEALTH_VERIFIED" {
		if observation.State != FirewallLiveAbsent {
			return runtimeDecision("firewall_new_boot_runtime_not_absent"), true, nil
		}
		next := binding
		next.State, next.AttemptBoot, next.Reason = "RESTORE_REQUIRED", boot, "boot_runtime_loss_detected"
		next.StartedAt, next.Deadline = w.now().Unix(), w.now().Add(5*time.Minute).Unix()
		next.MutationAt, next.HealthAt, next.HealthRevision = 0, 0, ""
		next.ArtifactRevision, next.CleanupAttempted = "", false
		next.ArtifactSHA256, next.ArtifactMembershipSHA256 = "", ""
		if err := write(next); err != nil {
			return runtimeDecision("firewall_restore_fence_failed"), true, err
		}
	}
	if binding.AttemptBoot != boot {
		return runtimeDecision("firewall_restore_interrupted_boot"), true, nil
	}
	if binding.StartedAt <= 0 || binding.StartedAt > w.now().Unix() || binding.Deadline <= binding.StartedAt || binding.Deadline-binding.StartedAt > 300 {
		return runtimeDecision("firewall_restore_clock_or_deadline_invalid"), true, nil
	}
	if binding.State == "RESTORE_FAILED" {
		if binding.CleanupAttempted && observation.State != FirewallLiveAbsent {
			return runtimeDecision(binding.Reason), true, nil
		}
		return w.failRuntimeRestore(ctx, operation, composition, binding, binding.Reason)
	}
	if binding.State != "RESTORE_REQUIRED" && binding.State != "RESTORING_RUNTIME" {
		return runtimeDecision("firewall_restore_state_invalid"), true, nil
	}
	if binding.State == "RESTORING_RUNTIME" && operation.State != operations.StateRestoringRuntime {
		operation, err = w.Manager.Transition(ctx, operation.OperationID, operation.Revision, operations.StateRestoringRuntime)
		if err != nil {
			return runtimeDecision("firewall_restore_operation_fenced"), true, err
		}
	}
	if binding.State == "RESTORING_RUNTIME" && observation.State == FirewallLiveAbsent {
		// The durable attempt can have crossed the privileged boundary. Never
		// repeat forward mutation, even if it now appears absent. Retain the
		// committed fact so interruption cannot silently erase desired policy.
		next := binding
		next.State, next.Reason = "RESTORE_FAILED", "boot_restore_interrupted_absent"
		if err := write(next); err != nil {
			return runtimeDecision(next.Reason), true, err
		}
		return runtimeDecision(next.Reason), true, nil
	}
	if binding.Deadline <= w.now().Unix() && observation.State == FirewallLiveAbsent {
		if binding.MutationAt == 0 {
			return runtimeDecision("firewall_restore_state_invalid"), true, nil
		}
		decision, retireErr := w.retireRuntime(ctx, operation, composition.Revision)
		return decision, true, retireErr
	}
	current, currentErr := w.CurrentRuntimeManagement(ctx)
	capabilities, capabilityErr := w.Capabilities(ctx)
	if currentErr != nil || capabilityErr != nil || !firewallCapabilitiesAvailable(capabilities, composition.Plan) || w.Runtime.RequiresBaseFirewall() && !capabilities.NFT.BaseFirewallPresent ||
		validateRuntimeManagement(composition.Plan, current, w.now()) != nil || healthFailedFor(current.Resources, w.Health(ctx, current.Resources)) {
		if binding.State == "RESTORING_RUNTIME" {
			return w.failRuntimeRestore(ctx, operation, composition, binding, "boot_restore_management_failed")
		}
		next := binding
		next.Reason = "boot_restore_waiting_for_prerequisites"
		if err := write(next); err != nil {
			return runtimeDecision(next.Reason), true, err
		}
		return runtimeDecision(next.Reason), true, nil
	}
	if operation.State != operations.StateRestoringRuntime {
		operation, err = w.Manager.Transition(ctx, operation.OperationID, operation.Revision, operations.StateRestoringRuntime)
		if err != nil {
			return runtimeDecision("firewall_restore_operation_fenced"), true, err
		}
	}
	capturedAt := w.now()
	candidate := renderEndpointManagedNFTAt(composition.Plan, true, capturedAt)
	runtimeSHA := artifactSHA([]byte(candidate))
	runtimeSemantic, semanticErr := helper.ManagedSemanticSHA256([]byte(candidate))
	runtimeMembership, membershipErr := helper.ManagedTimedMembershipSHA256([]byte(candidate))
	if semanticErr != nil || membershipErr != nil || runtimeSemantic != composition.CandidateSemanticSHA {
		return w.failRuntimeRestore(ctx, operation, composition, binding, "boot_restore_projection_invalid")
	}
	if binding.State == "RESTORE_REQUIRED" {
		revision := rollbackArtifactRevision(operation.OperationID, hostresources.Revision(struct {
			Boot     string
			Revision int
		}{boot, operation.Revision})[:24])
		artifact, err := w.Artifacts.WriteRevision(ctx, operation.OperationID, revision, candidateFiles(composition.Plan, candidate, runtimeSHA))
		if err != nil {
			return runtimeDecision("boot_restore_artifact_failed"), true, err
		}
		paths := workflowPaths(artifact.Revision)
		paths.capturedAt = capturedAt.UnixNano()
		validated, err := w.call(ctx, operation, helper.OperationNFTValidate, paths, composition.PlanRevision, runtimeSHA, "", "", false, composition.CandidateSemanticSHA, runtimeMembership)
		if err != nil || validated == nil || validated.PreviousTablePresent || validated.CandidateSHA256 != runtimeSHA || validated.SemanticSHA256 != composition.CandidateSemanticSHA || validated.TimedMembershipSHA256 != runtimeMembership {
			return w.failRuntimeRestore(ctx, operation, composition, binding, "boot_restore_validation_failed")
		}
		// Re-observe current management immediately before consuming the attempt.
		current, err = w.CurrentRuntimeManagement(ctx)
		if err != nil || validateRuntimeManagement(composition.Plan, current, w.now()) != nil {
			return runtimeDecision("boot_restore_waiting_for_prerequisites"), true, nil
		}
		next := binding
		next.State, next.Reason, next.MutationAt = "RESTORING_RUNTIME", "boot_restore_started", time.Now().UTC().UnixNano()
		next.ArtifactRevision = artifact.Revision
		next.ArtifactSHA256, next.ArtifactMembershipSHA256 = runtimeSHA, runtimeMembership
		if err := write(next); err != nil {
			return runtimeDecision("boot_restore_fence_failed"), true, err
		}
		if err := w.Marker.MarkMutation(operation.OperationID, artifact.Revision); err != nil {
			return w.failRuntimeRestore(ctx, operation, composition, binding, "boot_restore_marker_failed")
		}
		result, err := w.call(ctx, operation, helper.OperationNFTApply, paths, composition.PlanRevision, runtimeSHA, "", "", false, composition.CandidateSemanticSHA, "", runtimeMembership, "")
		if err != nil || result == nil || result.AppliedRevision != composition.PlanRevision || result.CandidateSHA256 != runtimeSHA || result.SemanticSHA256 != composition.CandidateSemanticSHA || result.TimedMembershipSHA256 != runtimeMembership || !validFirewallSHA(result.RollbackSHA256) {
			return w.failRuntimeRestore(ctx, operation, composition, binding, "boot_restore_apply_failed")
		}
		checkpoint := FirewallCheckpoint{Version: 2, OperationID: operation.OperationID, ArtifactRevision: artifact.Revision, PlanRevision: composition.PlanRevision, CandidateSHA256: runtimeSHA, CandidateSemanticSHA256: composition.CandidateSemanticSHA, CandidateTimedMembershipSHA256: runtimeMembership, RollbackSHA256: result.RollbackSHA256, ContributionID: transition.ContributionID, ContributionRevision: transition.DesiredSemanticRevision, CompositionRevision: composition.Revision}
		if err := w.saveCheckpoint(checkpoint); err != nil {
			return w.failRuntimeRestore(ctx, operation, composition, binding, "boot_restore_checkpoint_failed")
		}
	}
	current, err = w.CurrentRuntimeManagement(ctx)
	if err != nil || validateRuntimeManagement(composition.Plan, current, w.now()) != nil {
		return w.failRuntimeRestore(ctx, operation, composition, binding, "boot_restore_management_failed")
	}
	health := w.Health(ctx, current.Resources)
	if healthFailedFor(current.Resources, health) {
		return w.failRuntimeRestore(ctx, operation, composition, binding, "boot_restore_health_failed")
	}
	live, err := w.ReconcileAuthority(ctx)
	if err != nil || live.State != FirewallLiveMatching {
		return w.failRuntimeRestore(ctx, operation, composition, binding, "boot_restore_verification_failed")
	}
	// Reconstruct the operation-local deletion checkpoint even after an
	// interrupted post-mutation write. Rollback semantics stay contribution-owned.
	deleteSHA := artifactSHA([]byte("delete table inet solovey_protection\n"))
	checkpoint := FirewallCheckpoint{Version: 2, OperationID: operation.OperationID, ArtifactRevision: binding.ArtifactRevision, PlanRevision: composition.PlanRevision, CandidateSHA256: binding.ArtifactSHA256, CandidateSemanticSHA256: composition.CandidateSemanticSHA, CandidateTimedMembershipSHA256: binding.ArtifactMembershipSHA256, RollbackSHA256: deleteSHA, ContributionID: transition.ContributionID, ContributionRevision: transition.DesiredSemanticRevision, CompositionRevision: composition.Revision}
	if err := w.saveCheckpoint(checkpoint); err != nil {
		return w.failRuntimeRestore(ctx, operation, composition, binding, "boot_restore_checkpoint_failed")
	}
	next := binding
	next.State, next.VerifiedBoot, next.Reason = "HEALTH_VERIFIED", boot, "boot_restore_complete"
	next.HealthAt, next.HealthRevision = time.Now().UTC().UnixNano(), hostresources.Revision(health)
	if err := write(next); err != nil {
		return runtimeDecision("boot_restore_health_commit_failed"), true, err
	}
	return operations.ReconcileDecision{State: operations.StateApplied, Reason: "boot_restore_complete"}, true, nil
}

func zeroMutationExpiredRuntime(binding repository.FirewallRuntimeBinding, now time.Time) bool {
	return binding.Schema == repository.FirewallRuntimeSchema && binding.State == "RESTORE_REQUIRED" &&
		(binding.Reason == "boot_runtime_loss_detected" || binding.Reason == "boot_restore_waiting_for_prerequisites") &&
		validFirewallSHA(binding.VerifiedBoot) && validFirewallSHA(binding.AttemptBoot) && binding.VerifiedBoot != binding.AttemptBoot &&
		binding.StartedAt > 0 && binding.Deadline > binding.StartedAt && binding.Deadline-binding.StartedAt <= 300 && binding.Deadline <= now.Unix() &&
		binding.MutationAt == 0 && binding.HealthAt == 0 && binding.HealthRevision == "" && !binding.CleanupAttempted &&
		binding.ArtifactRevision == "" && binding.ArtifactSHA256 == "" && binding.ArtifactMembershipSHA256 == ""
}

func retainedRuntimeFailure(binding repository.FirewallRuntimeBinding) bool {
	return binding.Schema == repository.FirewallRuntimeSchema && binding.State == "RESTORE_FAILED" &&
		(binding.MutationAt == 0 && binding.Reason == "boot_restore_deadline_expired" || binding.Reason == "boot_restore_interrupted_absent")
}

// Every live contribution keeps its own original operation and rollback
// before-image. An aggregate seal cannot hide a broken contributor's authority.
func (w Workflow) validateRuntimeContributionOwners(ctx context.Context, snapshot repository.FirewallAuthoritySnapshot) error {
	items, err := w.Manager.List(ctx)
	if err != nil {
		return err
	}
	owners := make(map[string]repository.OperationLockModel, len(items))
	for _, item := range items {
		owners[item.OperationID] = item
	}
	for _, model := range snapshot.Contributions {
		owner, ok := owners[model.AppliedOperationID]
		if !ok || owner.Kind != operations.KindFirewall || (owner.State != operations.StateApplied && owner.State != operations.StateReconcileRequired && owner.State != operations.StateRestoringRuntime) {
			return ErrContributionConflict
		}
		transition, err := w.Contributions.FirewallTransition(ctx, owner.OperationID)
		if err != nil || transition.Schema != FirewallTransitionSchemaV2 || transition.State != "HEALTH_VERIFIED" || transition.ContributionID != model.ContributionID || transition.DesiredSemanticRevision != model.SemanticRevision || owner.PlanRevision != transition.ManagedPlanRevision ||
			transition.MarkerUnixNano <= 0 || transition.MutationCompletedUnixNano <= transition.MarkerUnixNano || transition.HealthStartedUnixNano < transition.MutationCompletedUnixNano || transition.HealthCompletedUnixNano < transition.HealthStartedUnixNano || transition.HealthProviderInstance == "" || transition.HealthGeneration == 0 || !validFirewallSHA(transition.HealthObservationRevision) {
			return ErrContributionConflict
		}
		desired, err := decodeContributionJSON(transition.DesiredJSON)
		if err != nil || desired.ContributionID != model.ContributionID || desired.SemanticRevision != model.SemanticRevision {
			return ErrContributionConflict
		}
		if transition.PreviousPresent {
			previous, err := decodeContributionJSON(transition.PreviousJSON)
			if err != nil || previous.ContributionID != model.ContributionID || previous.SemanticRevision != transition.PreviousSemanticRevision {
				return ErrContributionConflict
			}
		}
	}
	return nil
}

func (w Workflow) retireRuntime(ctx context.Context, operation repository.OperationLockModel, composition string) (operations.ReconcileDecision, error) {
	if err := w.Contributions.RetireFirewallAuthorityAfterRuntimeLoss(ctx, operation.OperationID, operation.Revision, composition); err != nil {
		return runtimeDecision("firewall_retirement_fenced"), err
	}
	return operations.ReconcileDecision{State: operations.StateForgotten, Reason: "firewall_runtime_authority_retired"}, nil
}

// Failure recovery uses the same exact managed-only rollback primitive. An
// unknown/foreign table is never deleted. The durable failure fence stops all
// subsequent forward attempts, including periodic reconciliation.
func (w Workflow) failRuntimeRestore(ctx context.Context, operation repository.OperationLockModel, composition FirewallCompositionV2, binding repository.FirewallRuntimeBinding, reason string) (operations.ReconcileDecision, bool, error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
	defer cancel()
	failed := binding
	failed.State, failed.Reason = "RESTORE_FAILED", reason
	if err := w.RuntimeStore.UpdateFirewallRuntime(ctx, operation.OperationID, operation.Revision, composition.Revision, binding, failed); err != nil {
		return runtimeDecision(reason), true, err
	}
	live, observeErr := w.ReconcileAuthority(ctx)
	if observeErr == nil && live.State == FirewallLiveAbsent {
		decision, err := w.retireRuntime(ctx, operation, composition.Revision)
		decision.Reason = reason
		return decision, true, err
	}
	if observeErr == nil && live.State == FirewallLiveMatching && !binding.CleanupAttempted {
		if operation.State != operations.StateRestoringRuntime {
			var err error
			operation, err = w.Manager.Transition(ctx, operation.OperationID, operation.Revision, operations.StateRestoringRuntime)
			if err != nil {
				return runtimeDecision(reason), true, err
			}
		}
		cleanup := failed
		cleanup.CleanupAttempted = true
		if err := w.RuntimeStore.UpdateFirewallRuntime(ctx, operation.OperationID, operation.Revision, composition.Revision, failed, cleanup); err != nil {
			return runtimeDecision(reason), true, err
		}
		deletion := []byte("delete table inet solovey_protection\n")
		digest := artifactSHA(deletion)
		files := candidateFiles(composition.Plan, RenderManagedNFT(composition.Plan), composition.CandidateSHA)
		files["firewall-before.nft"], files["firewall-before.nft.sha256"] = deletion, []byte(digest+"\n")
		artifact, err := w.Artifacts.WriteRevision(ctx, operation.OperationID, rollbackArtifactRevision(operation.OperationID, "boot-failed"), files)
		if err == nil {
			var result *helper.NFTResult
			result, err = w.call(ctx, operation, helper.OperationNFTRollback, workflowPaths(artifact.Revision), composition.PlanRevision, "", digest, "", false, composition.CandidateSemanticSHA, live.CurrentTimedMembershipSHA)
			if err == nil && result != nil && !result.ManagedTablePresent && result.RollbackSHA256 == digest {
				live, err = w.ReconcileAuthority(ctx)
				if err == nil && live.State == FirewallLiveAbsent && !healthFailed(w.RollbackHealth(ctx, nil)) {
					decision, retireErr := w.retireRuntime(ctx, operation, composition.Revision)
					decision.Reason = reason
					return decision, true, retireErr
				}
			}
		}
		observeErr = err
	}
	bundleErr := w.Recovery.CreateBundle(ctx, operation, operations.StateReconcileRequired)
	return runtimeDecision(reason), true, errors.Join(observeErr, bundleErr)
}
