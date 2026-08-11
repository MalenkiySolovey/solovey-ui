package nativefallback

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/netip"
	"sort"
	"strings"
	"time"

	neutralfallback "github.com/MalenkiySolovey/solovey-ui/componenthost/fallbacktargets"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
	protectionoperations "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
	"github.com/MalenkiySolovey/solovey-ui/service/coreinboundcontrol"
)

type Workflow struct {
	Operations *protectionoperations.Manager
	Journal    NativeJournal
	Planner    Planner
	Core       CoreControl
	Providers  ProviderDirectory
	Artifacts  ArtifactWriter
	Marker     MutationMarker
	Now        func() time.Time
}

func (workflow *Workflow) Inspect(ctx context.Context, inboundDatabaseID uint) (coreinboundcontrol.CoreRuntimeIdentityV1, coreinboundcontrol.InboundFallbackSnapshotV1, error) {
	if workflow == nil || workflow.Planner.Core == nil || inboundDatabaseID == 0 {
		return coreinboundcontrol.CoreRuntimeIdentityV1{}, coreinboundcontrol.InboundFallbackSnapshotV1{}, &WorkflowError{Code: "native_workflow_unavailable"}
	}
	identity := workflow.Planner.Core.Identity(ctx)
	snapshot, err := workflow.Planner.Core.Snapshot(ctx, inboundDatabaseID)
	return identity, snapshot, err
}

func (workflow *Workflow) Preview(ctx context.Context, request PlanRequestV1) (domain.NativeFallbackPlanV1, error) {
	if workflow == nil || workflow.Planner.Core == nil || workflow.Planner.Targets == nil || workflow.Planner.Management == nil {
		return domain.NativeFallbackPlanV1{}, &WorkflowError{Code: "native_workflow_unavailable"}
	}
	return workflow.Planner.Plan(ctx, request)
}

func (workflow *Workflow) Prepare(ctx context.Context, request PrepareWorkflowRequestV1) (WorkflowResultV1, error) {
	if err := workflow.ready(); err != nil {
		return WorkflowResultV1{}, err
	}
	if err := ctx.Err(); err != nil {
		return WorkflowResultV1{}, &WorkflowError{Code: "prepare_cancelled"}
	}
	if strings.TrimSpace(request.Actor) == "" || strings.TrimSpace(request.IdempotencyKey) == "" ||
		request.Confirmation != "PREPARE NATIVE FALLBACK "+request.Plan.PlanDigest || request.Plan.Validate() != nil {
		return WorkflowResultV1{}, &WorkflowError{Code: "prepare_request_invalid"}
	}
	acquired, err := workflow.Operations.Acquire(ctx, protectionoperations.AcquireRequest{
		Kind: protectionoperations.KindNativeFallback, ResourceID: request.Plan.Resource.ResourceID,
		IdempotencyKey: request.IdempotencyKey, PlanRevision: request.Plan.PlanDigest, Actor: request.Actor,
		InitialState: protectionoperations.StatePrepared,
	})
	if err != nil {
		return WorkflowResultV1{}, err
	}
	if acquired.Joined {
		operation, loadErr := workflow.Journal.NativeFallbackOperation(ctx, acquired.Operation.OperationID)
		if loadErr != nil || operation.PlanDigest != request.Plan.PlanDigest {
			return WorkflowResultV1{}, &WorkflowError{Code: "prepare_idempotency_conflict"}
		}
		state, stateErr := workflow.Journal.NativeFallbackState(ctx, operation.ResourceID)
		return WorkflowResultV1{Operation: operation, State: state}, stateErr
	}
	lock := acquired.Operation

	currentPlan, err := workflow.Planner.Plan(ctx, request.PlanRequest)
	if err != nil || currentPlan.Validate() != nil || currentPlan.PlanDigest != request.Plan.PlanDigest || !currentPlan.Eligible ||
		!currentPlan.ExpiresAt.After(workflow.now()) || currentPlan.ApplyGate == domain.NativeApplyStable {
		_, _ = workflow.Operations.Transition(context.WithoutCancel(ctx), lock.OperationID, lock.Revision, protectionoperations.StateCancelled)
		return WorkflowResultV1{}, &WorkflowError{Code: "prepare_plan_stale_or_blocked"}
	}
	target, err := workflow.exactTargetBeforeReservation(ctx, currentPlan)
	if err != nil {
		_, _ = workflow.Operations.Transition(context.WithoutCancel(ctx), lock.OperationID, lock.Revision, protectionoperations.StateCancelled)
		return WorkflowResultV1{}, err
	}
	preview, endpoint, err := workflow.previewMaterial(ctx, currentPlan, target)
	if err != nil {
		_, _ = workflow.Operations.Transition(context.WithoutCancel(ctx), lock.OperationID, lock.Revision, protectionoperations.StateCancelled)
		return WorkflowResultV1{}, err
	}
	planJSON, _ := json.Marshal(currentPlan)
	referenceJSON, _ := json.Marshal(currentPlan.Target.Reference)
	operation, err := workflow.Journal.CreateNativeFallbackOperation(ctx, repository.NativeFallbackOperationModel{
		Schema: repository.NativeFallbackOperationSchemaV1, OperationID: lock.OperationID, ResourceID: currentPlan.Resource.ResourceID,
		InboundDatabaseID: currentPlan.Resource.InboundDatabaseID, PlanID: currentPlan.PlanID, PlanDigest: currentPlan.PlanDigest, PlanJSON: planJSON,
		RuntimeIdentityRevision: currentPlan.Runtime.IdentityRevision, CapabilityResolverRevision: currentPlan.Runtime.CapabilityResolverRevision,
		BeforeConfigurationRevision: currentPlan.CorePreview.BeforeConfigurationRevision, ExpectedAfterRevision: currentPlan.CorePreview.ExpectedAfterRevision,
		BeforeEffectiveRevision: currentPlan.Resource.EffectiveRevision, TargetReferenceJSON: referenceJSON,
		TargetRevision: currentPlan.Target.CanonicalTargetRevision, ProviderRevision: currentPlan.Target.ProviderRevision,
		EndpointRevision: currentPlan.Target.EndpointRevision, PublishRevision: currentPlan.Target.PublishRevision,
		HealthRevision: currentPlan.Target.HealthRevision, CapacityRevision: currentPlan.Target.CapacityRevision, CreatedAt: workflow.now().Unix(),
	})
	if err != nil {
		_, _ = workflow.Operations.Transition(context.WithoutCancel(ctx), lock.OperationID, lock.Revision, protectionoperations.StateCancelled)
		return WorkflowResultV1{}, err
	}
	provider, ok := workflow.Providers.ProviderV2(currentPlan.Target.Reference.ProviderID)
	if !ok {
		return workflow.cancelBeforeMutation(ctx, lock, operation, nil, "provider_absent")
	}
	reserved, err := reserveProvider(ctx, provider, neutralfallback.ReserveRequestV1{
		RequestID: operation.OperationID + "-reserve", HolderID: operation.OperationID, Purpose: neutralfallback.ReservationPurposeNativeFallback,
		ExactTargetReference: currentPlan.Target.Reference, FreshnessDurationSecs: uint32(neutralfallback.MaxPrepareReservationFreshnessV1 / time.Second),
	})
	if err != nil || reserved.State != neutralfallback.ReservationReserved || reserved.HolderID != operation.OperationID || reserved.ExactTargetReference != currentPlan.Target.Reference {
		return workflow.cancelBeforeMutation(ctx, lock, operation, nil, "provider_reserve_failed")
	}
	updatedOperation, err := workflow.Journal.AdvanceNativeFallbackOperation(ctx, repository.NativeFallbackJournalUpdate{
		OperationID: operation.OperationID, ExpectedRevision: operation.Revision, Stage: repository.NativeJournalReservation, Reservation: &reserved, Now: workflow.now(),
	})
	if err != nil {
		return workflow.cancelBeforeMutation(ctx, lock, operation, &reserved, "reservation_mirror_failed")
	}
	operation = updatedOperation
	checkpoint, err := workflow.Core.PrepareCheckpoint(ctx, coreinboundcontrol.PrepareCheckpointRequestV1{
		Preview: preview, ApprovedEndpoint: endpoint, ReplaceDefaultToo: currentPlan.CorePreview.ReplaceDefaultToo,
	})
	if err != nil || checkpoint.CheckpointID == "" || checkpoint.PreviewDigest != currentPlan.CorePreview.Digest || checkpoint.IntegrityDigest == "" {
		return workflow.cancelBeforeMutation(ctx, lock, operation, &reserved, "checkpoint_prepare_failed")
	}
	artifact, err := workflow.writePreparationArtifacts(ctx, operation, reserved, checkpoint.CheckpointID, checkpoint.IntegrityDigest)
	if err != nil {
		operation.CoreCheckpointID, operation.CoreCheckpointDigest, operation.CheckpointReleaseProof = checkpoint.CheckpointID, checkpoint.IntegrityDigest, checkpoint.UncommittedReleaseProof
		return workflow.cancelBeforeMutation(ctx, lock, operation, &reserved, "artifact_write_failed")
	}
	manifest, err := workflow.Marker.VerifyRevision(artifact.Revision, artifact.ManifestSHA256)
	if err != nil || manifest.OperationID != operation.OperationID || manifest.Revision != artifact.Revision {
		operation.CoreCheckpointID, operation.CoreCheckpointDigest, operation.CheckpointReleaseProof = checkpoint.CheckpointID, checkpoint.IntegrityDigest, checkpoint.UncommittedReleaseProof
		return workflow.cancelBeforeMutation(ctx, lock, operation, &reserved, "artifact_integrity_failed")
	}
	updatedOperation, err = workflow.Journal.AdvanceNativeFallbackOperation(ctx, repository.NativeFallbackJournalUpdate{
		OperationID: operation.OperationID, ExpectedRevision: operation.Revision, Stage: repository.NativeJournalPrepared,
		CheckpointID: checkpoint.CheckpointID, CheckpointDigest: checkpoint.IntegrityDigest, CheckpointReleaseProof: checkpoint.UncommittedReleaseProof,
		ArtifactRevision: artifact.Revision, ArtifactManifestDigest: artifact.ManifestSHA256, Now: workflow.now(),
	})
	if err != nil {
		operation.CoreCheckpointID, operation.CoreCheckpointDigest, operation.CheckpointReleaseProof = checkpoint.CheckpointID, checkpoint.IntegrityDigest, checkpoint.UncommittedReleaseProof
		return workflow.cancelBeforeMutation(ctx, lock, operation, &reserved, "prepared_state_persist_failed")
	}
	operation = updatedOperation
	state, err := workflow.Journal.NativeFallbackState(ctx, operation.ResourceID)
	return WorkflowResultV1{Operation: operation, State: state}, err
}

func (workflow *Workflow) writePreparationArtifacts(ctx context.Context, operation repository.NativeFallbackOperationModel, reservation neutralfallback.ProviderTargetReservationV1, checkpointID, checkpointDigest string) (repository.ArtifactModel, error) {
	checkpointArtifact, _ := json.Marshal(struct {
		Schema, OperationID, PlanDigest, CheckpointID, CheckpointDigest string
	}{"solovey-ui/native-fallback-checkpoint-reference/v1", operation.OperationID, operation.PlanDigest, checkpointID, checkpointDigest})
	intentArtifact, _ := json.Marshal(struct {
		Schema, OperationID, PlanDigest, ProviderReservationID, ProviderReservationRevision, CheckpointID, BeforeRevision, ExpectedAfterRevision string
	}{"solovey-ui/native-fallback-mutation-intent/v1", operation.OperationID, operation.PlanDigest, reservation.ReservationID, reservation.ReservationRevision, checkpointID, operation.BeforeConfigurationRevision, operation.ExpectedAfterRevision})
	return workflow.Artifacts.WriteRevision(ctx, operation.OperationID, "native-"+operation.OperationID, map[string][]byte{
		"canonical-plan.json": operation.PlanJSON, "checkpoint-reference.json": checkpointArtifact, "mutation-intent.json": intentArtifact,
	})
}

func (workflow *Workflow) Apply(ctx context.Context, request ApplyWorkflowRequestV1) (WorkflowResultV1, error) {
	if err := workflow.ready(); err != nil {
		return WorkflowResultV1{}, err
	}
	if strings.TrimSpace(request.Actor) == "" || !request.Confirmed || request.ExpectedState != domain.NativeActualPrepared || request.OperationRevision <= 0 ||
		!domain.ValidSHA256(request.PlanDigest) || !domain.ValidContractID(request.ProviderReservationRevision, 128) ||
		request.IdempotencyKey != "" && !domain.ValidContractID(request.IdempotencyKey, 128) {
		return WorkflowResultV1{}, &WorkflowError{Code: "apply_request_invalid"}
	}
	operation, err := workflow.Journal.NativeFallbackOperation(ctx, request.OperationID)
	if err != nil {
		return WorkflowResultV1{}, &WorkflowError{Code: "apply_operation_stale"}
	}
	lock, lockErr := workflow.Journal.OperationByID(ctx, operation.OperationID)
	if request.IdempotencyKey != "" {
		receiptLock, receiptErr := workflow.Journal.OperationByHelperRevisionPrefix(ctx, nativeActionBindingPrefix("native-apply", request.IdempotencyKey))
		if receiptErr != nil && !errors.Is(receiptErr, repository.ErrRecordNotFound) {
			return WorkflowResultV1{}, &WorkflowError{Code: "apply_idempotency_unavailable"}
		}
		if receiptErr == nil && receiptLock.OperationID != request.OperationID {
			return WorkflowResultV1{}, &WorkflowError{Code: "apply_idempotency_conflict"}
		}
		sameKey, exact := nativeActionReplay(receiptLock.HelperRevision, "native-apply", request.IdempotencyKey, request.OperationID, request.OperationRevision, request.PlanDigest, request.ProviderReservationRevision)
		if sameKey && !exact {
			return WorkflowResultV1{}, &WorkflowError{Code: "apply_idempotency_conflict"}
		}
		if exact && operation.WorkflowState == repository.NativeWorkflowApplied && receiptLock.State == protectionoperations.StateApplied {
			state, stateErr := workflow.Journal.NativeFallbackState(ctx, operation.ResourceID)
			return WorkflowResultV1{Operation: operation, State: state}, stateErr
		}
	}
	if operation.Revision != request.OperationRevision || operation.PlanDigest != request.PlanDigest ||
		operation.ProviderReservationRevision != request.ProviderReservationRevision || operation.WorkflowState != repository.NativeWorkflowPrepared {
		return WorkflowResultV1{}, &WorkflowError{Code: "apply_operation_stale"}
	}
	plan, err := decodeOperationPlan(operation)
	if err != nil {
		return WorkflowResultV1{}, err
	}
	if lockErr != nil || lock.Kind != protectionoperations.KindNativeFallback || lock.State != protectionoperations.StatePrepared {
		return WorkflowResultV1{}, &WorkflowError{Code: "apply_fence_invalid"}
	}
	provider, target, reservation, checkpoint, err := workflow.revalidatePrepared(ctx, operation, plan)
	if err != nil {
		var workflowErr *WorkflowError
		if errors.As(err, &workflowErr) && workflowErr.Code == "reservation_mirror_mismatch" {
			return workflow.persistRecoveryFailure(context.WithoutCancel(ctx), lock, operation, repository.NativeJournalReconcileRequired, "reservation_mirror_mismatch", coreinboundcontrol.CheckpointStatusV1{}, nil)
		}
		return workflow.failBeforeMutation(ctx, lock, operation, plan, "apply_preflight_failed")
	}
	manifest, err := workflow.Marker.VerifyRevision(operation.ArtifactRevision, operation.ArtifactManifestDigest)
	if err != nil || manifest.OperationID != operation.OperationID || manifest.Revision != operation.ArtifactRevision {
		return workflow.cancelBeforeMutation(ctx, lock, operation, &reservation, "prepared_artifact_invalid")
	}
	if request.IdempotencyKey == "" {
		lock, err = workflow.Operations.Transition(ctx, lock.OperationID, lock.Revision, protectionoperations.StateApplying)
	} else {
		binding := nativeActionBinding("native-apply", request.IdempotencyKey, request.OperationID, request.OperationRevision, request.PlanDigest, request.ProviderReservationRevision)
		lock, err = workflow.Operations.TransitionWithBinding(ctx, lock.OperationID, lock.Revision, protectionoperations.StateApplying, binding)
	}
	if err != nil {
		return WorkflowResultV1{}, err
	}
	updatedOperation, err := workflow.Journal.AdvanceNativeFallbackOperation(ctx, repository.NativeFallbackJournalUpdate{
		OperationID: operation.OperationID, ExpectedRevision: operation.Revision, Stage: repository.NativeJournalApplying, Now: workflow.now(),
	})
	if err != nil {
		return WorkflowResultV1{Operation: operation}, &WorkflowError{Code: "mutation_marker_persist_failed", Ambiguous: true}
	}
	operation = updatedOperation
	if err := workflow.Marker.MarkMutation(operation.OperationID, operation.ArtifactRevision); err != nil {
		return workflow.failAfterMarker(ctx, lock, operation, plan, provider, reservation, "mutation_marker_artifact_failed")
	}
	fenced, err := fenceProvider(ctx, provider, neutralfallback.ReservationMutationRequestV1{
		RequestID: operation.OperationID + "-fence", ReservationID: reservation.ReservationID, ExpectedRevision: reservation.ReservationRevision,
	})
	if err != nil || fenced.State != neutralfallback.ReservationMutationPending {
		return workflow.failAfterMarker(ctx, lock, operation, plan, provider, reservation, "provider_fence_failed")
	}
	updatedOperation, err = workflow.Journal.AdvanceNativeFallbackOperation(ctx, repository.NativeFallbackJournalUpdate{
		OperationID: operation.OperationID, ExpectedRevision: operation.Revision, Stage: repository.NativeJournalFenced, Reservation: &fenced, Now: workflow.now(),
	})
	if err != nil {
		return workflow.failAfterMarker(ctx, lock, operation, plan, provider, fenced, "provider_fence_persist_failed")
	}
	operation = updatedOperation
	endpoint := approvedEndpoint(target)
	mutation, err := workflow.Core.ApplyFallbackPatch(ctx, coreinboundcontrol.ApplyFallbackPatchRequestV1{
		CheckpointID: checkpoint.CheckpointID, ExpectedBeforeRevision: operation.BeforeConfigurationRevision, ApprovedEndpoint: endpoint,
	})
	if err != nil || mutation.AfterConfigurationRevision != operation.ExpectedAfterRevision || mutation.ExpectedEffectiveRevision == "" {
		return workflow.failAfterMarker(ctx, lock, operation, plan, provider, fenced, "core_apply_failed")
	}
	updatedOperation, err = workflow.Journal.AdvanceNativeFallbackOperation(ctx, repository.NativeFallbackJournalUpdate{
		OperationID: operation.OperationID, ExpectedRevision: operation.Revision, Stage: repository.NativeJournalHealth,
		AfterConfigurationRevision: mutation.AfterConfigurationRevision, ExpectedEffectiveRevision: mutation.ExpectedEffectiveRevision,
		ManagerGeneration: mutation.Observation.ManagerGeneration, Now: workflow.now(),
	})
	if err != nil {
		return workflow.failAfterMarker(ctx, lock, operation, plan, provider, fenced, "health_state_persist_failed")
	}
	operation = updatedOperation
	lock, err = workflow.Operations.Transition(ctx, lock.OperationID, lock.Revision, protectionoperations.StateHealth)
	if err != nil {
		return workflow.failAfterMarker(ctx, lock, operation, plan, provider, fenced, "health_fence_persist_failed")
	}
	verification, err := workflow.Core.VerifyEffective(ctx, coreinboundcontrol.VerifyEffectiveRequestV1{
		CheckpointID: operation.CoreCheckpointID, ExpectedAfterRevision: operation.ExpectedAfterRevision, ExpectedEffectiveRevision: operation.ExpectedEffectiveRevision,
	})
	if err != nil || !verification.Verified || verification.EffectiveRevision != operation.ExpectedEffectiveRevision {
		return workflow.failAfterMarker(ctx, lock, operation, plan, provider, fenced, "core_effective_verify_failed")
	}
	healthFacts, healthRevision, err := workflow.verifyHealth(ctx, operation, plan, provider, fenced, verification)
	if err != nil {
		return workflow.failAfterMarker(ctx, lock, operation, plan, provider, fenced, "target_health_failed")
	}
	active, err := activateProvider(ctx, provider, neutralfallback.ReservationMutationRequestV1{
		RequestID: operation.OperationID + "-activate", ReservationID: fenced.ReservationID, ExpectedRevision: fenced.ReservationRevision,
		FreshnessDurationSecs: uint32(neutralfallback.MaxActiveReservationFreshnessV1 / time.Second),
	})
	if err != nil || active.State != neutralfallback.ReservationActive {
		return workflow.failAfterMarker(ctx, lock, operation, plan, provider, fenced, "provider_activation_failed")
	}
	healthJSON, _ := json.Marshal(healthFacts)
	updatedOperation, err = workflow.Journal.AdvanceNativeFallbackOperation(ctx, repository.NativeFallbackJournalUpdate{
		OperationID: operation.OperationID, ExpectedRevision: operation.Revision, Stage: repository.NativeJournalApplied, Reservation: &active,
		EffectiveRevision: verification.EffectiveRevision, ManagerGeneration: verification.Observation.ManagerGeneration,
		HealthResultRevision: healthRevision, HealthFactsJSON: healthJSON, Now: workflow.now(),
	})
	if err != nil {
		return workflow.failAfterMarker(ctx, lock, operation, plan, provider, active, "applied_state_persist_failed")
	}
	operation = updatedOperation
	if _, err = workflow.Operations.Transition(ctx, lock.OperationID, lock.Revision, protectionoperations.StateApplied); err != nil {
		return WorkflowResultV1{}, &WorkflowError{Code: "applied_fence_persist_failed", Ambiguous: true}
	}
	state, err := workflow.Journal.NativeFallbackState(ctx, operation.ResourceID)
	return WorkflowResultV1{Operation: operation, State: state}, err
}

func (workflow *Workflow) ready() error {
	if workflow == nil || workflow.Operations == nil || workflow.Journal == nil || workflow.Core == nil || workflow.Providers == nil || workflow.Artifacts == nil || workflow.Marker == nil ||
		workflow.Planner.Core == nil || workflow.Planner.Targets == nil || workflow.Planner.Management == nil {
		return &WorkflowError{Code: "native_workflow_unavailable"}
	}
	return nil
}

func (workflow *Workflow) now() time.Time {
	if workflow != nil && workflow.Now != nil {
		return workflow.Now().UTC().Truncate(time.Second)
	}
	return time.Now().UTC().Truncate(time.Second)
}

func (workflow *Workflow) exactTargetBeforeReservation(ctx context.Context, plan domain.NativeFallbackPlanV1) (neutralfallback.FallbackTargetV2, error) {
	provider, ok := workflow.Providers.ProviderV2(plan.Target.Reference.ProviderID)
	if !ok {
		return neutralfallback.FallbackTargetV2{}, &WorkflowError{Code: "provider_absent"}
	}
	result, err := resolveProvider(ctx, provider, plan.Target.Reference)
	if err != nil || neutralfallback.ResolveExactV2(plan.Target.Reference, result, workflow.now()) != nil {
		return neutralfallback.FallbackTargetV2{}, &WorkflowError{Code: "target_reference_stale"}
	}
	return result, nil
}

func (workflow *Workflow) previewMaterial(ctx context.Context, plan domain.NativeFallbackPlanV1, target neutralfallback.FallbackTargetV2) (coreinboundcontrol.FallbackPatchPreviewV1, coreinboundcontrol.ApprovedEndpointV1, error) {
	variant, ok := coreVariant(plan.SelectedVariant)
	if !ok {
		return coreinboundcontrol.FallbackPatchPreviewV1{}, coreinboundcontrol.ApprovedEndpointV1{}, &WorkflowError{Code: "plan_variant_invalid"}
	}
	endpoint := approvedEndpoint(target)
	preview, err := workflow.Core.PreviewFallbackPatch(ctx, coreinboundcontrol.PreviewFallbackPatchRequestV1{
		Expected: coreinboundcontrol.FallbackPatchExpectationsV1{
			InboundDatabaseID: plan.Resource.InboundDatabaseID, ResourceID: plan.Resource.ResourceID,
			ConfigurationRevision: plan.Resource.ConfigurationRevision, RuntimeIdentityRevision: plan.Runtime.IdentityRevision,
			CapabilityResolverRevision: plan.Runtime.CapabilityResolverRevision, EndpointRevision: target.Endpoint.EndpointRevision,
		},
		Variant: variant, ApprovedEndpoint: endpoint, ReplaceDefaultToo: plan.CorePreview.ReplaceDefaultToo,
	})
	if err != nil || preview.Digest != plan.CorePreview.Digest || preview.BeforeConfigurationRevision != plan.CorePreview.BeforeConfigurationRevision ||
		preview.ExpectedAfterRevision != plan.CorePreview.ExpectedAfterRevision || !preview.ExpiresAt.After(workflow.now()) {
		return coreinboundcontrol.FallbackPatchPreviewV1{}, coreinboundcontrol.ApprovedEndpointV1{}, &WorkflowError{Code: "core_preview_stale"}
	}
	return preview, endpoint, nil
}

func coreVariant(value domain.NativeFallbackVariant) (coreinboundcontrol.FallbackPatchVariantV1, bool) {
	switch value {
	case domain.NativeFallbackVLESSRealityHandshakeTCP:
		return coreinboundcontrol.FallbackPatchVLESSRealityHandshakeTCP, true
	case domain.NativeFallbackTrojanDefaultTCP:
		return coreinboundcontrol.FallbackPatchTrojanDefaultTCP, true
	case domain.NativeFallbackTrojanALPNTCP:
		return coreinboundcontrol.FallbackPatchTrojanALPNTCP, true
	default:
		return "", false
	}
}

func digestSafe(value any) string {
	payload, _ := json.Marshal(value)
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func canonicalProtocols(values []neutralfallback.ApplicationProtocol) []neutralfallback.ApplicationProtocol {
	result := append([]neutralfallback.ApplicationProtocol(nil), values...)
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func targetStillMatchesPlan(target neutralfallback.FallbackTargetV2, plan domain.NativeFallbackPlanV1, now time.Time, reserved bool) bool {
	if target.Validate() != nil || target.Identity.ProviderID != plan.Target.Reference.ProviderID || target.Identity.TargetID != plan.Target.Reference.TargetID ||
		target.Publish.Revision != plan.Target.PublishRevision || target.Publish.ContentDigest != plan.Target.ContentDigest ||
		target.Endpoint.EndpointID != plan.Target.EndpointID || target.Endpoint.EndpointRevision != plan.Target.EndpointRevision ||
		target.ProviderRevision != plan.Target.ProviderRevision || target.Health.Revision != plan.Target.HealthRevision ||
		neutralfallback.EffectiveReadinessV2(target.Health, now) != neutralfallback.ReadinessReady || len(target.Health.ReasonCodes) != 0 ||
		string(target.Endpoint.Network) != plan.Target.Network || !target.Endpoint.Local || string(target.Endpoint.ProxyProtocol) != plan.Target.ProxyProtocol || string(target.Endpoint.CanReachManagement) != plan.Target.ManagementReachability ||
		string(target.Endpoint.TransportSecurity) != plan.Target.TransportSecurity || string(target.Endpoint.AddressFamily) != plan.Target.AddressFamily {
		return false
	}
	address, err := netip.ParseAddr(target.Endpoint.Address)
	if err != nil || !address.IsLoopback() {
		return false
	}
	binding := targetBinding(plan.Target.Reference, target, plan.Target.RequiredServerNameDigest)
	if !sameExactSet(binding.ApplicationProtocols, plan.Target.ApplicationProtocols) || !sameExactSet(binding.AcceptedServerNameDigests, plan.Target.AcceptedServerNameDigests) {
		return false
	}
	capacity := neutralfallback.EffectiveCapacityStateV2(target.Capacity, now)
	if !reserved {
		return target.Capacity.Revision == plan.Target.CapacityRevision && capacity == neutralfallback.CapacityReady && target.Capacity.ReservationSlotsUsed == plan.Target.ReservationSlotsUsed
	}
	return target.Capacity.ReservationSlotsTotal == plan.Target.ReservationSlotsTotal && target.Capacity.ReservationSlotsUsed >= plan.Target.ReservationSlotsUsed+1 &&
		(capacity == neutralfallback.CapacityReady || capacity == neutralfallback.CapacityPressured || capacity == neutralfallback.CapacityExhausted)
}

func (workflow *Workflow) currentReservedTarget(ctx context.Context, provider neutralfallback.ProviderV2, plan domain.NativeFallbackPlanV1) (neutralfallback.FallbackTargetV2, error) {
	page, err := providerInventory(ctx, provider)
	if err != nil || page.Truncated || len(page.ReasonCodes) != 0 {
		return neutralfallback.FallbackTargetV2{}, &WorkflowError{Code: "provider_inventory_unavailable"}
	}
	for _, target := range page.Targets {
		if target.Identity.ProviderID == plan.Target.Reference.ProviderID && target.Identity.TargetID == plan.Target.Reference.TargetID {
			if !targetStillMatchesPlan(target, plan, workflow.now(), true) {
				return neutralfallback.FallbackTargetV2{}, &WorkflowError{Code: "target_revision_drift"}
			}
			return target, nil
		}
	}
	return neutralfallback.FallbackTargetV2{}, &WorkflowError{Code: "target_missing"}
}

func (workflow *Workflow) revalidatePrepared(ctx context.Context, operation repository.NativeFallbackOperationModel, plan domain.NativeFallbackPlanV1) (neutralfallback.ProviderV2, neutralfallback.FallbackTargetV2, neutralfallback.ProviderTargetReservationV1, coreinboundcontrol.CheckpointStatusV1, error) {
	if !plan.Eligible || !plan.ExpiresAt.After(workflow.now()) || plan.ApplyGate == domain.NativeApplyStable {
		return nil, neutralfallback.FallbackTargetV2{}, neutralfallback.ProviderTargetReservationV1{}, coreinboundcontrol.CheckpointStatusV1{}, &WorkflowError{Code: "prepared_plan_expired"}
	}
	provider, ok := workflow.Providers.ProviderV2(plan.Target.Reference.ProviderID)
	if !ok {
		return nil, neutralfallback.FallbackTargetV2{}, neutralfallback.ProviderTargetReservationV1{}, coreinboundcontrol.CheckpointStatusV1{}, &WorkflowError{Code: "provider_absent"}
	}
	reservation, err := getProviderReservation(ctx, provider, operation.ProviderReservationID)
	if err != nil || reservation.ReservationRevision != operation.ProviderReservationRevision || reservation.State != neutralfallback.ReservationReserved ||
		reservation.HolderID != operation.OperationID || reservation.ExactTargetReference != plan.Target.Reference || !reservation.Status(workflow.now()).Fresh {
		return nil, neutralfallback.FallbackTargetV2{}, neutralfallback.ProviderTargetReservationV1{}, coreinboundcontrol.CheckpointStatusV1{}, &WorkflowError{Code: "provider_reservation_stale"}
	}
	mirror, err := workflow.Journal.ReservationMirror(ctx, operation.OperationID)
	if err != nil || mirror.ProviderReservationID != reservation.ReservationID || mirror.ProviderReservationRevision != reservation.ReservationRevision || mirror.State != string(reservation.State) {
		return nil, neutralfallback.FallbackTargetV2{}, neutralfallback.ProviderTargetReservationV1{}, coreinboundcontrol.CheckpointStatusV1{}, &WorkflowError{Code: "reservation_mirror_mismatch"}
	}
	target, err := workflow.currentReservedTarget(ctx, provider, plan)
	if err != nil {
		return nil, neutralfallback.FallbackTargetV2{}, neutralfallback.ProviderTargetReservationV1{}, coreinboundcontrol.CheckpointStatusV1{}, err
	}
	identity := workflow.Core.Identity(ctx)
	snapshot, snapshotErr := workflow.Core.Snapshot(ctx, operation.InboundDatabaseID)
	if snapshotErr != nil || identity.IdentityRevision != operation.RuntimeIdentityRevision || snapshot.RuntimeIdentityRevision != operation.RuntimeIdentityRevision ||
		snapshot.CapabilityResolverRevision != operation.CapabilityResolverRevision || snapshot.ResourceID != operation.ResourceID ||
		snapshot.ConfigurationRevision != operation.BeforeConfigurationRevision || snapshot.Effective.Revision != operation.BeforeEffectiveRevision ||
		SourceRevision(snapshot) != plan.Resource.SourceRevision || ResourceRevision(snapshot) != plan.Resource.ResourceRevision {
		return nil, neutralfallback.FallbackTargetV2{}, neutralfallback.ProviderTargetReservationV1{}, coreinboundcontrol.CheckpointStatusV1{}, &WorkflowError{Code: "core_revision_drift"}
	}
	management, err := workflow.Planner.Management.ResolveIsolation(ctx, operation.ResourceID, ManagementEndpointFactsV1{
		EndpointID: target.Endpoint.EndpointID, EndpointRevision: target.Endpoint.EndpointRevision, Network: string(target.Endpoint.Network),
		AddressFamily: string(target.Endpoint.AddressFamily), Address: target.Endpoint.Address, Port: target.Endpoint.Port,
		Local: target.Endpoint.Local, ManagementReachability: string(target.Endpoint.CanReachManagement),
	})
	if err != nil || management.State != "ISOLATED" || management.Revision != plan.ManagementIsolation.Revision || !management.ExpiresAt.After(workflow.now()) {
		return nil, neutralfallback.FallbackTargetV2{}, neutralfallback.ProviderTargetReservationV1{}, coreinboundcontrol.CheckpointStatusV1{}, &WorkflowError{Code: "management_revision_drift"}
	}
	checkpoint, err := workflow.Core.InspectCheckpoint(ctx, coreinboundcontrol.InspectCheckpointRequestV1{CheckpointID: operation.CoreCheckpointID})
	if err != nil || checkpoint.State != coreinboundcontrol.CheckpointStatePrepared || checkpoint.PreviewDigest != operation.PlanDigest && checkpoint.PreviewDigest != plan.CorePreview.Digest ||
		checkpoint.IntegrityDigest != operation.CoreCheckpointDigest || checkpoint.BeforeConfigurationRevision != operation.BeforeConfigurationRevision || checkpoint.ExpectedAfterRevision != operation.ExpectedAfterRevision {
		return nil, neutralfallback.FallbackTargetV2{}, neutralfallback.ProviderTargetReservationV1{}, coreinboundcontrol.CheckpointStatusV1{}, &WorkflowError{Code: "checkpoint_invalid"}
	}
	return provider, target, reservation, checkpoint, nil
}

func (workflow *Workflow) verifyHealth(ctx context.Context, operation repository.NativeFallbackOperationModel, plan domain.NativeFallbackPlanV1, provider neutralfallback.ProviderV2, reservation neutralfallback.ProviderTargetReservationV1, verification coreinboundcontrol.EffectiveVerificationV1) (NativeHealthFactsV1, string, error) {
	if !verification.Verified || verification.ConfigurationRevision != operation.ExpectedAfterRevision || verification.EffectiveRevision == "" ||
		verification.Observation.Tag != plan.Resource.InboundTag || verification.Observation.Type != plan.Resource.InboundType ||
		verification.Observation.OptionsDigest == "" || verification.Observation.ManagerGeneration == 0 || verification.Observation.MatchingInboundCount != 1 {
		return NativeHealthFactsV1{}, "", &WorkflowError{Code: "core_health_binding_invalid"}
	}
	target, err := workflow.currentReservedTarget(ctx, provider, plan)
	if err != nil {
		return NativeHealthFactsV1{}, "", err
	}
	currentReservation, err := getProviderReservation(ctx, provider, reservation.ReservationID)
	if err != nil || currentReservation.ReservationRevision != reservation.ReservationRevision || currentReservation.State != reservation.State || currentReservation.ExactTargetReference != plan.Target.Reference {
		return NativeHealthFactsV1{}, "", &WorkflowError{Code: "provider_health_fence_invalid"}
	}
	management, err := workflow.Planner.Management.ResolveIsolation(ctx, operation.ResourceID, ManagementEndpointFactsV1{
		EndpointID: target.Endpoint.EndpointID, EndpointRevision: target.Endpoint.EndpointRevision, Network: string(target.Endpoint.Network), AddressFamily: string(target.Endpoint.AddressFamily),
		Address: target.Endpoint.Address, Port: target.Endpoint.Port, Local: target.Endpoint.Local, ManagementReachability: string(target.Endpoint.CanReachManagement),
	})
	if err != nil || management.State != "ISOLATED" || management.Revision != plan.ManagementIsolation.Revision {
		return NativeHealthFactsV1{}, "", &WorkflowError{Code: "management_health_failed"}
	}
	now := workflow.now()
	expires := time.Unix(target.Health.ExpiresAt, 0).UTC()
	if capacityExpiry := time.Unix(target.Capacity.ExpiresAt, 0).UTC(); capacityExpiry.Before(expires) {
		expires = capacityExpiry
	}
	if management.ExpiresAt.Before(expires) {
		expires = management.ExpiresAt
	}
	facts := NativeHealthFactsV1{
		Schema: NativeHealthFactsSchemaV1, OperationID: operation.OperationID, ResourceID: operation.ResourceID,
		RuntimeIdentityRevision: operation.RuntimeIdentityRevision, InboundTag: plan.Resource.InboundTag, InboundType: plan.Resource.InboundType,
		EffectiveOptionsDigest: verification.Observation.OptionsDigest, ManagerGeneration: verification.Observation.ManagerGeneration,
		AfterConfigurationRevision: operation.ExpectedAfterRevision, EffectiveRevision: verification.EffectiveRevision,
		TargetReference: plan.Target.Reference, ProviderReservationRevision: reservation.ReservationRevision,
		ProviderHealthRevision: target.Health.Revision, ProviderCapacityRevision: target.Capacity.Revision,
		ConnectFirstByteP95MS: target.Health.ConnectFirstByteP95MS,
		TransportSecurity:     target.Endpoint.TransportSecurity, ApplicationProtocols: canonicalProtocols(target.Endpoint.ApplicationProtocols),
		RequiredServerNameDigest: plan.Target.RequiredServerNameDigest, ManagementRevision: management.Revision, ObservedAt: now, ExpiresAt: expires,
	}
	if !facts.ExpiresAt.After(now) {
		return NativeHealthFactsV1{}, "", &WorkflowError{Code: "health_observation_expired"}
	}
	return facts, digestSafe(facts), nil
}

func detachmentProof(operation repository.NativeFallbackOperationModel, currentConfiguration, currentEffective string) string {
	return digestSafe(struct {
		Schema, OperationID, ResourceID, TargetRevision, CurrentConfiguration, CurrentEffective string
	}{"solovey-ui/native-fallback-detachment-proof/v1", operation.OperationID, operation.ResourceID, operation.TargetRevision, currentConfiguration, currentEffective})
}

func (workflow *Workflow) cancelBeforeMutation(ctx context.Context, lock repository.OperationLockModel, operation repository.NativeFallbackOperationModel, reservation *neutralfallback.ProviderTargetReservationV1, reason string) (WorkflowResultV1, error) {
	recoveryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), workflowRecoveryTimeout)
	defer cancel()
	plan, planErr := decodeOperationPlan(operation)
	snapshot, snapshotErr := workflow.Core.Snapshot(recoveryCtx, operation.InboundDatabaseID)
	variant, variantOK := coreVariant(plan.SelectedVariant)
	if planErr != nil || snapshotErr != nil || !variantOK || snapshot.ResourceID != operation.ResourceID ||
		snapshot.ConfigurationRevision != operation.BeforeConfigurationRevision || snapshot.Effective.Revision != operation.BeforeEffectiveRevision ||
		CurrentSafeSubtreeDigest(snapshot, variant, plan.CorePreview.ReplaceDefaultToo) != plan.CorePreview.CurrentSafeSubtreeDigest {
		return workflow.persistRecoveryFailure(recoveryCtx, lock, operation, repository.NativeJournalReconcileRequired, "detachment_not_proven", coreinboundcontrol.CheckpointStatusV1{}, reservation)
	}
	checkpointReleased := operation.CoreCheckpointID == ""
	currentConfiguration, currentEffective := operation.BeforeConfigurationRevision, operation.BeforeEffectiveRevision
	if operation.CoreCheckpointID != "" {
		status, err := workflow.Core.InspectCheckpoint(recoveryCtx, coreinboundcontrol.InspectCheckpointRequestV1{CheckpointID: operation.CoreCheckpointID})
		if err != nil || status.State != coreinboundcontrol.CheckpointStatePrepared || status.CurrentConfigurationRevision != operation.BeforeConfigurationRevision {
			return workflow.persistRecoveryFailure(recoveryCtx, lock, operation, repository.NativeJournalReconcileRequired, "checkpoint_invalid_before_mutation", status, reservation)
		}
		currentConfiguration, currentEffective = status.CurrentConfigurationRevision, status.CurrentEffectiveRevision
	}
	var released *neutralfallback.ProviderTargetReservationV1
	if reservation != nil {
		if !reservation.Status(workflow.now()).Fresh || reservation.State != neutralfallback.ReservationReserved {
			return workflow.persistRecoveryFailure(recoveryCtx, lock, operation, repository.NativeJournalReconcileRequired, "reserved_authority_not_fresh", coreinboundcontrol.CheckpointStatusV1{}, reservation)
		}
		provider, ok := workflow.Providers.ProviderV2(reservation.ExactTargetReference.ProviderID)
		if !ok {
			return workflow.persistRecoveryFailure(recoveryCtx, lock, operation, repository.NativeJournalReconcileRequired, "provider_absent", coreinboundcontrol.CheckpointStatusV1{}, reservation)
		}
		value, err := releaseProvider(recoveryCtx, provider, neutralfallback.ReleaseReservationRequestV1{
			RequestID: operation.OperationID + "-release", ReservationID: reservation.ReservationID, ExpectedRevision: reservation.ReservationRevision,
			VerifiedDetachedRevision: detachmentProof(operation, currentConfiguration, currentEffective),
		})
		if err != nil || value.State != neutralfallback.ReservationReleased {
			return workflow.persistRecoveryFailure(recoveryCtx, lock, operation, repository.NativeJournalReconcileRequired, "provider_release_failed", coreinboundcontrol.CheckpointStatusV1{}, reservation)
		}
		released = &value
	}
	if operation.CoreCheckpointID != "" {
		_, err := workflow.Core.ReleaseCheckpoint(recoveryCtx, coreinboundcontrol.ReleaseCheckpointRequestV1{
			CheckpointID: operation.CoreCheckpointID, Kind: coreinboundcontrol.CheckpointProofApplyNeverCommitted, ProofDigest: operation.CheckpointReleaseProof,
		})
		if err != nil {
			return workflow.persistRecoveryFailure(recoveryCtx, lock, operation, repository.NativeJournalReconcileRequired, "checkpoint_release_failed", coreinboundcontrol.CheckpointStatusV1{}, released)
		}
		checkpointReleased = true
	}
	operation, err := workflow.Journal.AdvanceNativeFallbackOperation(recoveryCtx, repository.NativeFallbackJournalUpdate{
		OperationID: operation.OperationID, ExpectedRevision: operation.Revision, Stage: repository.NativeJournalCancelled,
		Reservation: released, CheckpointReleased: checkpointReleased, ReasonCodes: []domain.NativeFallbackReasonCode{domain.NativeFallbackReasonCode(reason)}, Now: workflow.now(),
	})
	if err != nil {
		return WorkflowResultV1{}, err
	}
	if _, err := workflow.Operations.Transition(recoveryCtx, lock.OperationID, lock.Revision, protectionoperations.StateCancelled); err != nil {
		return WorkflowResultV1{}, err
	}
	state, err := workflow.Journal.NativeFallbackState(recoveryCtx, operation.ResourceID)
	return WorkflowResultV1{Operation: operation, State: state}, errors.Join(&WorkflowError{Code: reason}, err)
}

func (workflow *Workflow) failBeforeMutation(ctx context.Context, lock repository.OperationLockModel, operation repository.NativeFallbackOperationModel, plan domain.NativeFallbackPlanV1, reason string) (WorkflowResultV1, error) {
	provider, ok := workflow.Providers.ProviderV2(plan.Target.Reference.ProviderID)
	if !ok || operation.ProviderReservationID == "" {
		return workflow.persistRecoveryFailure(context.WithoutCancel(ctx), lock, operation, repository.NativeJournalReconcileRequired, reason, coreinboundcontrol.CheckpointStatusV1{}, nil)
	}
	reservation, err := getProviderReservation(ctx, provider, operation.ProviderReservationID)
	if err != nil || reservation.ReservationRevision != operation.ProviderReservationRevision || reservation.State != neutralfallback.ReservationReserved ||
		reservation.HolderID != operation.OperationID || reservation.ExactTargetReference != plan.Target.Reference || !reservation.Status(workflow.now()).Fresh {
		return workflow.persistRecoveryFailure(context.WithoutCancel(ctx), lock, operation, repository.NativeJournalReconcileRequired, reason, coreinboundcontrol.CheckpointStatusV1{}, &reservation)
	}
	return workflow.cancelBeforeMutation(ctx, lock, operation, &reservation, reason)
}

func (workflow *Workflow) failAfterMarker(ctx context.Context, lock repository.OperationLockModel, operation repository.NativeFallbackOperationModel, plan domain.NativeFallbackPlanV1, provider neutralfallback.ProviderV2, reservation neutralfallback.ProviderTargetReservationV1, reason string) (WorkflowResultV1, error) {
	if ctx.Err() != nil {
		return workflow.persistRecoveryFailure(context.WithoutCancel(ctx), lock, operation, repository.NativeJournalReconcileRequired, "cancellation_after_possible_mutation", coreinboundcontrol.CheckpointStatusV1{}, &reservation)
	}
	return workflow.rollback(ctx, lock, operation, plan, provider, reservation, reason, false)
}
