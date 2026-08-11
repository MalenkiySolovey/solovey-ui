package fronting

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/fallbacktargets"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	protectionartifacts "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/artifacts"
	protectionhelper "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/helper"
	protectionoperations "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
)

const (
	FrontingWorkflowCheckpointSchemaV2 = "solovey-ui/fronting-workflow-checkpoint/v2"
	FixedL4HealthSchemaV2              = "solovey-ui/fixed-l4-health/v2"
	v2ProviderTimeout                  = 3 * time.Second
	v2RecoveryTimeout                  = 2 * time.Minute
)

type PlanSourceV2 interface {
	CurrentFrontingPlanInputV2(context.Context, FrontingStrategyPlanV2) (FrontingPlanInputV2, error)
}

type EndpointLeaseDirectoryV1 interface {
	EndpointLeaseProviderV1(string) (hostresources.EndpointLeaseProviderV1, bool)
	EndpointLeasesByHolderV1(context.Context, string) ([]hostresources.EndpointLeaseV1, error)
}

type ArtifactVerifierV2 interface {
	VerifyRevision(string, string) (protectionartifacts.Manifest, error)
}

type FixedL4HealthRequestV2 struct {
	OperationID              string
	OperationRevision        int
	PlanDigest               string
	CandidateRevision        string
	CandidateSHA256          string
	SocketClaimRevision      string
	BackendReferenceRevision string
	LeaseRevision            string
	ProxyMode                hostresources.ProxyMode
}

type FixedL4HealthEvidenceV2 struct {
	Schema                   string                  `json:"schema"`
	OperationID              string                  `json:"operationId"`
	OperationRevision        int                     `json:"operationRevision"`
	PlanDigest               string                  `json:"planDigest"`
	CandidateRevision        string                  `json:"candidateRevision"`
	CandidateSHA256          string                  `json:"candidateSha256"`
	SocketClaimRevision      string                  `json:"socketClaimRevision"`
	BackendReferenceRevision string                  `json:"backendReferenceRevision"`
	LeaseRevision            string                  `json:"leaseRevision"`
	ProxyMode                hostresources.ProxyMode `json:"proxyMode"`
	PublicFixtureAccepted    bool                    `json:"publicFixtureAccepted"`
	ExpectedBackendReached   bool                    `json:"expectedBackendReached"`
	BackendIdentityMarker    string                  `json:"backendIdentityMarker"`
	AlternateTargetReceipts  uint32                  `json:"alternateTargetReceipts"`
	ProxyHeaderObserved      bool                    `json:"proxyHeaderObserved"`
	LatencyMilliseconds      uint32                  `json:"latencyMilliseconds"`
	ObservedAt               int64                   `json:"observedAt"`
	ExpiresAt                int64                   `json:"expiresAt"`
	ReasonCodes              []string                `json:"reasonCodes,omitempty"`
}

type FixedL4HealthCheckV2 func(context.Context, FixedL4HealthRequestV2) (FixedL4HealthEvidenceV2, error)

type PrepareV2Input struct {
	Plan           FrontingStrategyPlanV2
	Actor          string
	IdempotencyKey string
	Confirmation   string
}

type ApplyV2Input struct {
	OperationID  string
	PlanDigest   string
	Confirmation string
}

type RollbackV2Input struct {
	OperationID  string
	PlanDigest   string
	Confirmation string
}

type WorkflowErrorV2 struct {
	Code      string
	Ambiguous bool
}

func (err *WorkflowErrorV2) Error() string {
	if err == nil || err.Code == "" {
		return "fronting_workflow_failed"
	}
	return err.Code
}

type WorkflowResultV2 struct {
	OperationID              string                           `json:"operationId"`
	OperationRevision        int                              `json:"operationRevision"`
	State                    string                           `json:"state"`
	PlanID                   string                           `json:"planId"`
	PlanDigest               string                           `json:"planDigest"`
	Strategy                 FrontingStrategy                 `json:"strategy"`
	CandidateRevision        string                           `json:"candidateRevision,omitempty"`
	CandidateSHA256          string                           `json:"candidateSha256,omitempty"`
	PreviousRevision         string                           `json:"previousRevision,omitempty"`
	LeaseID                  string                           `json:"leaseId,omitempty"`
	LeaseRevision            string                           `json:"leaseRevision,omitempty"`
	LeaseState               hostresources.EndpointLeaseState `json:"leaseState,omitempty"`
	TargetAuthorityRevisions []string                         `json:"targetAuthorityRevisions,omitempty"`
	RecoveryRequired         bool                             `json:"recoveryRequired"`
	ReasonCodes              []string                         `json:"reasonCodes,omitempty"`
}

type CheckpointV2 struct {
	Version                          int                                           `json:"version"`
	Schema                           string                                        `json:"schema"`
	OperationID                      string                                        `json:"operationId"`
	OperationRevision                int                                           `json:"operationRevision"`
	Plan                             FrontingStrategyPlanV2                        `json:"plan"`
	RuntimeIdentityRevision          string                                        `json:"runtimeIdentityRevision"`
	StrategyCapabilityRevision       string                                        `json:"strategyCapabilityRevision"`
	ActiveStrategyCapabilityRevision string                                        `json:"activeStrategyCapabilityRevision,omitempty"`
	SocketClaimRevision              string                                        `json:"socketClaimRevision"`
	BackendReferenceRevision         string                                        `json:"backendReferenceRevision"`
	BackendReferenceRevisions        []string                                      `json:"backendReferenceRevisions"`
	SelectorSetRevision              string                                        `json:"selectorSetRevision,omitempty"`
	MapRevision                      string                                        `json:"mapRevision,omitempty"`
	UpstreamIDSetRevision            string                                        `json:"upstreamIdSetRevision,omitempty"`
	SelectedProxyMode                hostresources.ProxyMode                       `json:"selectedProxyMode"`
	EngineIdentityRevision           string                                        `json:"engineIdentityRevision,omitempty"`
	HelperRevision                   string                                        `json:"helperRevision,omitempty"`
	Lease                            hostresources.EndpointLeaseV1                 `json:"lease,omitempty"`
	EndpointLeases                   []hostresources.EndpointLeaseV1               `json:"endpointLeases,omitempty"`
	FallbackReservations             []fallbacktargets.ProviderTargetReservationV1 `json:"fallbackReservations,omitempty"`
	CandidateRevision                string                                        `json:"candidateRevision,omitempty"`
	CandidateSHA256                  string                                        `json:"candidateSha256,omitempty"`
	ArtifactRevision                 string                                        `json:"artifactRevision,omitempty"`
	ArtifactManifestSHA256           string                                        `json:"artifactManifestSha256,omitempty"`
	PreviousRevision                 string                                        `json:"previousRevision,omitempty"`
	PreviousSHA256                   string                                        `json:"previousSha256,omitempty"`
	PreviousListeners                []protectionhelper.NginxListener              `json:"previousListeners,omitempty"`
	ExpectedActiveRevision           string                                        `json:"expectedActiveRevision,omitempty"`
	ActualActiveRevision             string                                        `json:"actualActiveRevision,omitempty"`
	ProcessIdentityRevision          string                                        `json:"processIdentityRevision,omitempty"`
	WorkerSetIdentityRevision        string                                        `json:"workerSetIdentityRevision,omitempty"`
	ListenerVerificationRevision     string                                        `json:"listenerVerificationRevision,omitempty"`
	Health                           FixedL4HealthEvidenceV2                       `json:"health,omitempty"`
	SNIHealth                        SNIPrereadHealthEvidenceV2                    `json:"sniHealth,omitempty"`
	MutationMarker                   bool                                          `json:"mutationMarker"`
	Switched                         bool                                          `json:"switched"`
	Reloaded                         bool                                          `json:"reloaded"`
	Verified                         bool                                          `json:"verified"`
	Restored                         bool                                          `json:"restored"`
	RollbackReloaded                 bool                                          `json:"rollbackReloaded"`
	Detached                         bool                                          `json:"detached"`
	RollbackAttemptCount             int                                           `json:"rollbackAttemptCount"`
	RecoveryClassification           string                                        `json:"recoveryClassification,omitempty"`
	FailedStage                      string                                        `json:"failedStage,omitempty"`
	ReasonCodes                      []string                                      `json:"reasonCodes,omitempty"`
	Checkpoint                       string                                        `json:"checkpoint"`
	Timeline                         []TimelineEvent                               `json:"timeline"`
}

type revalidatedPlanV2 struct {
	Plan      FrontingStrategyPlanV2
	Input     FrontingPlanInputV2
	Facts     map[string]hostresources.FrontingBackendFactV1
	Fallbacks map[string]FallbackPlanningTargetV2
}

func (w *Workflow) PrepareV2(ctx context.Context, input PrepareV2Input) (WorkflowResultV2, error) {
	if err := w.readyV2(); err != nil {
		return WorkflowResultV2{}, err
	}
	if strings.TrimSpace(input.Actor) == "" || strings.TrimSpace(input.IdempotencyKey) == "" || input.Plan.Validate() != nil ||
		input.Confirmation != "PREPARE FRONTING "+input.Plan.CanonicalPlanDigest {
		return WorkflowResultV2{}, v2Error("prepare_request_invalid", false)
	}
	if err := validateWorkflowPlanShapeV2(input.Plan, w.nowV2()); err != nil {
		return WorkflowResultV2{}, v2Error("candidate_invalid", false)
	}
	port := int(input.Plan.PublicSocket.PublicPort)
	acquired, err := w.Manager.Acquire(ctx, protectionoperations.AcquireRequest{
		Kind: protectionoperations.KindFronting, ResourceID: input.Plan.PublicSocket.ResourceID, Protocol: "tcp",
		Listen: input.Plan.PublicSocket.CanonicalBind, Port: &port, IdempotencyKey: input.IdempotencyKey,
		PlanRevision: input.Plan.CanonicalPlanDigest, Actor: input.Actor,
	})
	if err != nil {
		return WorkflowResultV2{}, err
	}
	operation := acquired.Operation
	if acquired.Joined {
		checkpoint, loadErr := w.loadV2(operation.OperationID)
		if loadErr != nil || checkpoint.Plan.CanonicalPlanDigest != input.Plan.CanonicalPlanDigest || checkpoint.Plan.PlanID != input.Plan.PlanID {
			return WorkflowResultV2{}, v2Error("plan_stale", false)
		}
		return resultV2(operation, checkpoint), nil
	}
	referenceRevisions := append([]string(nil), input.Plan.Targets.ReferenceRevisions...)
	checkpoint := CheckpointV2{
		Version: 2, Schema: FrontingWorkflowCheckpointSchemaV2, OperationID: operation.OperationID, OperationRevision: operation.Revision,
		Plan: input.Plan, RuntimeIdentityRevision: input.Plan.Runtime.IdentityRevision,
		StrategyCapabilityRevision: input.Plan.StrategyCapabilityRevision, SocketClaimRevision: input.Plan.PublicSocket.ClaimRevision,
		BackendReferenceRevisions: referenceRevisions, SelectorSetRevision: input.Plan.Selectors.SelectorSetRevision,
		SelectedProxyMode: input.Plan.Targets.SelectedProxyMode,
		ReasonCodes:       []string{}, Timeline: []TimelineEvent{},
	}
	if input.Plan.Strategy.Selected == StrategyL4OneToOne {
		checkpoint.BackendReferenceRevision = input.Plan.Targets.BackendReferences[0].CanonicalReferenceRevision
	}
	current, code := w.revalidatePlanV2(ctx, input.Plan)
	if code != "" {
		return w.cancelV2BeforeMarker(ctx, operation, checkpoint, code)
	}
	capabilities, capErr := w.capabilities(ctx)
	if capErr != nil || !frontingCapabilitiesAvailable(capabilities) {
		return w.cancelV2BeforeMarker(ctx, operation, checkpoint, "capability_stale")
	}
	checkpoint.EngineIdentityRevision, checkpoint.HelperRevision = engineIdentityRevisionV2(capabilities), capabilities.Revision
	checkpoint.PreviousRevision, checkpoint.PreviousSHA256 = capabilities.Nginx.ActiveRevision, capabilities.Nginx.ActiveSHA256
	checkpoint.PreviousListeners = append([]protectionhelper.NginxListener(nil), capabilities.Nginx.Listeners...)
	if !validManagedRevision(checkpoint.PreviousRevision, checkpoint.PreviousSHA256) || len(checkpoint.PreviousListeners) == 0 {
		return w.cancelV2BeforeMarker(ctx, operation, checkpoint, "active_revision_mismatch")
	}
	if err := w.saveV2(&checkpoint, "operation_persisted"); err != nil {
		return w.cancelV2BeforeMarker(ctx, operation, checkpoint, "artifact_integrity_failed")
	}
	if authorityCode := w.acquireTargetAuthoritiesV2(ctx, &checkpoint); authorityCode != "" {
		if authorityCode == "ambiguous_result" {
			return w.finishV2RecoveryFailure(ctx, operation, checkpoint, protectionoperations.StateReconcileRequired, authorityCode)
		}
		return w.cancelV2BeforeMarker(ctx, operation, checkpoint, authorityCode)
	}
	candidate, renderErr := renderWorkflowCandidateV2(current, checkpoint, w.nowV2())
	if renderErr != nil {
		return w.cancelV2BeforeMarker(ctx, operation, checkpoint, candidateErrorCodeV2(renderErr))
	}
	files := candidateFilesV2(candidate, checkpoint)
	artifact, artifactErr := w.Artifacts.WriteRevision(ctx, operation.OperationID, candidate.Revision, files)
	if artifactErr != nil {
		return w.cancelV2BeforeMarker(ctx, operation, checkpoint, "artifact_integrity_failed")
	}
	checkpoint.CandidateRevision, checkpoint.ExpectedActiveRevision = candidate.Revision, candidate.Revision
	checkpoint.CandidateSHA256, checkpoint.ArtifactRevision, checkpoint.ArtifactManifestSHA256 = candidate.SHA256, artifact.Revision, artifact.ManifestSHA256
	checkpoint.SelectorSetRevision, checkpoint.MapRevision, checkpoint.UpstreamIDSetRevision = candidate.SelectorSetRevision, candidate.MapRevision, candidate.UpstreamIDSetRevision
	if err := w.verifyPreparedArtifactV2(checkpoint); err != nil {
		return w.cancelV2BeforeMarker(ctx, operation, checkpoint, "artifact_integrity_failed")
	}
	if err := w.saveV2(&checkpoint, "prepared"); err != nil {
		return w.cancelV2BeforeMarker(ctx, operation, checkpoint, "artifact_integrity_failed")
	}
	return resultV2(operation, checkpoint), nil
}

func (w *Workflow) ApplyV2(ctx context.Context, input ApplyV2Input) (WorkflowResultV2, error) {
	if err := w.readyV2(); err != nil {
		return WorkflowResultV2{}, err
	}
	if input.OperationID == "" || !frontingHexV2(input.PlanDigest) || input.Confirmation != "APPLY FRONTING "+input.OperationID {
		return WorkflowResultV2{}, v2Error("apply_request_invalid", false)
	}
	operationCtx := ctx
	if ctx.Err() != nil {
		operationCtx = context.WithoutCancel(ctx)
	}
	operation, err := w.operation(operationCtx, input.OperationID)
	if err != nil {
		return WorkflowResultV2{}, v2Error("plan_stale", false)
	}
	checkpoint, err := w.loadV2(input.OperationID)
	if err != nil || checkpoint.Plan.CanonicalPlanDigest != input.PlanDigest || operation.PlanRevision != input.PlanDigest {
		return WorkflowResultV2{}, v2Error("plan_stale", false)
	}
	if operation.State == protectionoperations.StateApplied {
		if code := w.verifyHistoricalAppliedV2(ctx, operation, &checkpoint); code != "" {
			return resultV2(operation, checkpoint), v2Error(code, true)
		}
		return resultV2(operation, checkpoint), nil
	}
	if operation.State != protectionoperations.StatePrepared || checkpoint.Checkpoint != "prepared" {
		return WorkflowResultV2{}, v2Error("plan_stale", false)
	}
	if ctx.Err() != nil {
		return w.cancelV2BeforeMarker(ctx, operation, checkpoint, "ambiguous_result")
	}
	current, code := w.revalidatePlanV2(ctx, checkpoint.Plan)
	if code != "" {
		return w.cancelV2BeforeMarker(ctx, operation, checkpoint, code)
	}
	candidate, renderErr := renderWorkflowCandidateV2(current, checkpoint, w.nowV2())
	if renderErr != nil || candidate.Revision != checkpoint.CandidateRevision || candidate.SHA256 != checkpoint.CandidateSHA256 {
		return w.cancelV2BeforeMarker(ctx, operation, checkpoint, candidateErrorCodeV2(renderErr))
	}
	if err := w.verifyPreparedArtifactV2(checkpoint); err != nil {
		return w.cancelV2BeforeMarker(ctx, operation, checkpoint, "artifact_integrity_failed")
	}
	capabilities, capErr := w.capabilities(ctx)
	if capErr != nil || !frontingCapabilitiesAvailable(capabilities) || capabilities.Revision != checkpoint.HelperRevision ||
		engineIdentityRevisionV2(capabilities) != checkpoint.EngineIdentityRevision || capabilities.Nginx.ActiveRevision != checkpoint.PreviousRevision || capabilities.Nginx.ActiveSHA256 != checkpoint.PreviousSHA256 {
		return w.cancelV2BeforeMarker(ctx, operation, checkpoint, "runtime_identity_stale")
	}
	authorities, leaseCode := w.currentTargetAuthoritiesV2(ctx, checkpoint)
	if leaseCode != "" || !authoritiesInStateV2(authorities, hostresources.EndpointLeaseReserved, fallbacktargets.ReservationReserved) {
		return w.cancelV2BeforeMarker(ctx, operation, checkpoint, firstV2Code(leaseCode, "lease_stale"))
	}
	updateCheckpointAuthoritiesV2(&checkpoint, authorities)
	if ctx.Err() != nil {
		return w.cancelV2BeforeMarker(ctx, operation, checkpoint, "ambiguous_result")
	}
	applying, err := w.Manager.Transition(ctx, operation.OperationID, operation.Revision, protectionoperations.StateApplying)
	if err != nil {
		if ctx.Err() != nil {
			return w.cancelV2BeforeMarker(ctx, operation, checkpoint, "ambiguous_result")
		}
		return WorkflowResultV2{}, err
	}
	checkpoint.OperationRevision = applying.Revision
	if err := w.Marker.MarkMutation(applying.OperationID, checkpoint.ArtifactRevision); err != nil {
		return w.cancelV2BeforeMarker(ctx, applying, checkpoint, "artifact_integrity_failed")
	}
	checkpoint.MutationMarker = true
	if err := w.saveV2(&checkpoint, "mutation_intent"); err != nil {
		return w.failV2AfterMarker(ctx, applying, checkpoint, "artifact_integrity_failed")
	}
	fenced, fenceCode := fenceTargetAuthoritiesV2(ctx, authorities, applying.OperationID+"-fence", w.nowV2())
	if fenceCode != "" {
		updateCheckpointAuthoritiesV2(&checkpoint, fenced)
		if err := w.saveV2(&checkpoint, "lease_fence_partial"); err != nil {
			return w.finishV2RecoveryFailure(ctx, applying, checkpoint, protectionoperations.StateReconcileRequired, "ambiguous_result")
		}
		if ctx.Err() != nil || fenceCode == "ambiguous_result" {
			return w.finishV2RecoveryFailure(ctx, applying, checkpoint, protectionoperations.StateReconcileRequired, "ambiguous_result")
		}
		return w.failV2AfterMarker(ctx, applying, checkpoint, fenceCode)
	}
	updateCheckpointAuthoritiesV2(&checkpoint, fenced)
	if err := w.saveV2(&checkpoint, "lease_fenced"); err != nil {
		return w.failV2AfterMarker(ctx, applying, checkpoint, "lease_stale")
	}
	if code = w.validateAndInstallV2(ctx, applying, checkpoint, capabilities.Nginx.Binary); code != "" {
		if ctx.Err() != nil {
			return w.finishV2RecoveryFailure(ctx, applying, checkpoint, protectionoperations.StateReconcileRequired, "ambiguous_result")
		}
		return w.failV2AfterMarker(ctx, applying, checkpoint, code)
	}
	response, switchErr := w.execute(ctx, protectionhelper.Request{ProtocolVersion: protectionhelper.ProtocolVersion, Correlation: w.correlation(applying),
		Operation: protectionhelper.OperationNginxSwitch, NginxSwitch: &protectionhelper.NginxSwitchRequest{ExpectedPreviousRevision: checkpoint.PreviousRevision,
			TargetRevision: checkpoint.CandidateRevision, ExpectedSHA256: checkpoint.CandidateSHA256}})
	if ctx.Err() != nil {
		return w.finishV2RecoveryFailure(ctx, applying, checkpoint, protectionoperations.StateReconcileRequired, "ambiguous_result")
	}
	if switchErr != nil || response.Nginx == nil || response.Nginx.Revision != checkpoint.CandidateRevision || response.Nginx.PreviousRevision != checkpoint.PreviousRevision {
		return w.failV2AfterMarker(ctx, applying, checkpoint, "active_switch_failed")
	}
	checkpoint.Switched, checkpoint.ActualActiveRevision = true, checkpoint.CandidateRevision
	if err := w.saveV2(&checkpoint, "active_switched"); err != nil {
		return w.failV2AfterMarker(ctx, applying, checkpoint, "ambiguous_result")
	}
	response, reloadErr := w.execute(ctx, protectionhelper.Request{ProtocolVersion: protectionhelper.ProtocolVersion, Correlation: w.correlation(applying),
		Operation: protectionhelper.OperationNginxReload, NginxReload: &protectionhelper.NginxReloadRequest{ExpectedRevision: checkpoint.CandidateRevision,
			ExpectedSHA256: checkpoint.CandidateSHA256, ExpectedBinary: capabilities.Nginx.Binary}})
	if ctx.Err() != nil {
		return w.finishV2RecoveryFailure(ctx, applying, checkpoint, protectionoperations.StateReconcileRequired, "ambiguous_result")
	}
	if reloadErr != nil || response.Nginx == nil || response.Nginx.MasterPID <= 0 || len(response.Nginx.WorkerPIDs) == 0 {
		return w.failV2AfterMarker(ctx, applying, checkpoint, "reload_failed")
	}
	checkpoint.Reloaded = true
	if err := w.saveV2(&checkpoint, "reloaded"); err != nil {
		return w.failV2AfterMarker(ctx, applying, checkpoint, "ambiguous_result")
	}
	verification, verifyCode := w.verifyEngineRevisionV2(ctx, applying, checkpoint.CandidateRevision, checkpoint.CandidateSHA256, capabilities.Nginx.Binary, helperListenerV2(checkpoint.Plan.PublicSocket))
	if ctx.Err() != nil {
		return w.finishV2RecoveryFailure(ctx, applying, checkpoint, protectionoperations.StateReconcileRequired, "ambiguous_result")
	}
	if verifyCode != "" {
		return w.failV2AfterMarker(ctx, applying, checkpoint, verifyCode)
	}
	recordEngineVerificationV2(&checkpoint, verification)
	activeCapabilityRevision, activeCode := w.revalidateActiveBindingsV2(ctx, checkpoint.Plan)
	if ctx.Err() != nil {
		return w.finishV2RecoveryFailure(ctx, applying, checkpoint, protectionoperations.StateReconcileRequired, "ambiguous_result")
	}
	if activeCode != "" {
		return w.failV2AfterMarker(ctx, applying, checkpoint, activeCode)
	}
	checkpoint.ActiveStrategyCapabilityRevision = activeCapabilityRevision
	if err := w.saveV2(&checkpoint, "active_verified"); err != nil {
		return w.failV2AfterMarker(ctx, applying, checkpoint, "ambiguous_result")
	}
	healthing, err := w.Manager.Transition(ctx, applying.OperationID, applying.Revision, protectionoperations.StateHealth)
	if err != nil {
		if ctx.Err() != nil {
			return w.finishV2RecoveryFailure(ctx, applying, checkpoint, protectionoperations.StateReconcileRequired, "ambiguous_result")
		}
		return w.failV2AfterMarker(ctx, applying, checkpoint, "ambiguous_result")
	}
	checkpoint.OperationRevision = healthing.Revision
	healthCode := w.checkStrategyHealthV2(ctx, healthing, &checkpoint)
	if healthCode != "" {
		if ctx.Err() != nil {
			return w.finishV2RecoveryFailure(ctx, healthing, checkpoint, protectionoperations.StateReconcileRequired, "ambiguous_result")
		}
		return w.failV2AfterMarker(ctx, healthing, checkpoint, healthCode)
	}
	if err := w.saveV2(&checkpoint, "healthy"); err != nil {
		return w.failV2AfterMarker(ctx, healthing, checkpoint, "ambiguous_result")
	}
	activeAuthorities, activationCode := activateTargetAuthoritiesV2(ctx, fenced, healthing.OperationID+"-activate", w.nowV2())
	if activationCode != "" {
		updateCheckpointAuthoritiesV2(&checkpoint, activeAuthorities)
		if err := w.saveV2(&checkpoint, "lease_activation_partial"); err != nil {
			return w.finishV2RecoveryFailure(ctx, healthing, checkpoint, protectionoperations.StateReconcileRequired, "ambiguous_result")
		}
		if ctx.Err() != nil || activationCode == "ambiguous_result" {
			return w.finishV2RecoveryFailure(ctx, healthing, checkpoint, protectionoperations.StateReconcileRequired, "ambiguous_result")
		}
		return w.failV2AfterMarker(ctx, healthing, checkpoint, activationCode)
	}
	updateCheckpointAuthoritiesV2(&checkpoint, activeAuthorities)
	if err := w.saveV2(&checkpoint, "lease_active"); err != nil {
		return w.failV2AfterMarker(ctx, healthing, checkpoint, "ambiguous_result")
	}
	applied, err := w.Manager.Transition(ctx, healthing.OperationID, healthing.Revision, protectionoperations.StateApplied)
	if err != nil {
		if ctx.Err() != nil {
			return w.finishV2RecoveryFailure(ctx, healthing, checkpoint, protectionoperations.StateReconcileRequired, "ambiguous_result")
		}
		return w.failV2AfterMarker(ctx, healthing, checkpoint, "ambiguous_result")
	}
	checkpoint.OperationRevision = applied.Revision
	_ = w.saveV2(&checkpoint, "applied")
	return resultV2(applied, checkpoint), nil
}

func (w *Workflow) RollbackV2(ctx context.Context, input RollbackV2Input) (WorkflowResultV2, error) {
	if err := w.readyV2(); err != nil {
		return WorkflowResultV2{}, err
	}
	if input.OperationID == "" || !frontingHexV2(input.PlanDigest) || input.Confirmation != "ROLLBACK FRONTING "+input.OperationID {
		return WorkflowResultV2{}, v2Error("rollback_request_invalid", false)
	}
	operation, err := w.operation(ctx, input.OperationID)
	if err != nil {
		return WorkflowResultV2{}, v2Error("plan_stale", false)
	}
	checkpoint, err := w.loadV2(input.OperationID)
	if err != nil || checkpoint.Plan.CanonicalPlanDigest != input.PlanDigest {
		return WorkflowResultV2{}, v2Error("artifact_integrity_failed", false)
	}
	return w.rollbackV2(ctx, operation, checkpoint, false)
}

func (w *Workflow) validateAndInstallV2(ctx context.Context, operation protectionrepository.OperationLockModel, checkpoint CheckpointV2, binary protectionhelper.BinaryIdentity) string {
	validation, err := w.execute(ctx, protectionhelper.Request{ProtocolVersion: protectionhelper.ProtocolVersion, Correlation: w.correlation(operation),
		Operation: protectionhelper.OperationNginxValidate, NginxValidate: &protectionhelper.NginxValidateRequest{CandidatePath: candidatePath(checkpoint.ArtifactRevision),
			ExpectedRevision: checkpoint.CandidateRevision, ExpectedSHA256: checkpoint.CandidateSHA256, ExpectedBinary: binary}})
	if err != nil || validation.Nginx == nil || validation.Nginx.Revision != checkpoint.CandidateRevision || validation.Nginx.SHA256 != checkpoint.CandidateSHA256 {
		return "validation_failed"
	}
	installed, err := w.execute(ctx, protectionhelper.Request{ProtocolVersion: protectionhelper.ProtocolVersion, Correlation: w.correlation(operation),
		Operation: protectionhelper.OperationNginxInstall, NginxInstall: &protectionhelper.NginxInstallRequest{CandidatePath: candidatePath(checkpoint.ArtifactRevision),
			ExpectedRevision: checkpoint.CandidateRevision, ExpectedSHA256: checkpoint.CandidateSHA256, Listeners: helperListenerV2(checkpoint.Plan.PublicSocket)}})
	if err != nil || installed.Nginx == nil || installed.Nginx.Revision != checkpoint.CandidateRevision || installed.Nginx.SHA256 != checkpoint.CandidateSHA256 {
		return "active_switch_failed"
	}
	return ""
}

func (w *Workflow) verifyEngineRevisionV2(ctx context.Context, operation protectionrepository.OperationLockModel, revision, sha string, binary protectionhelper.BinaryIdentity, listeners []protectionhelper.NginxListener) (*protectionhelper.NginxResult, string) {
	response, err := w.execute(ctx, protectionhelper.Request{ProtocolVersion: protectionhelper.ProtocolVersion, Correlation: w.correlation(operation),
		Operation: protectionhelper.OperationNginxVerify, NginxVerify: &protectionhelper.NginxVerifyRequest{ExpectedRevision: revision, ExpectedSHA256: sha,
			ExpectedBinary: binary, Listeners: listeners}})
	if err != nil || response.Nginx == nil || response.Nginx.Revision != revision || response.Nginx.SHA256 != sha {
		return nil, "active_revision_mismatch"
	}
	if response.Nginx.Binary != binary || response.Nginx.MasterPID <= 0 || len(response.Nginx.WorkerPIDs) == 0 {
		return nil, "process_identity_mismatch"
	}
	if !response.Nginx.ListenersMatched {
		return nil, "listener_identity_mismatch"
	}
	return response.Nginx, ""
}

func recordEngineVerificationV2(checkpoint *CheckpointV2, verification *protectionhelper.NginxResult) {
	checkpoint.Verified, checkpoint.ActualActiveRevision = true, verification.Revision
	checkpoint.ProcessIdentityRevision = v2Revision(struct {
		Binary protectionhelper.BinaryIdentity
		Master int
	}{verification.Binary, verification.MasterPID})
	workers := append([]int(nil), verification.WorkerPIDs...)
	sort.Ints(workers)
	checkpoint.WorkerSetIdentityRevision = v2Revision(workers)
	checkpoint.ListenerVerificationRevision = v2Revision(helperListenerV2(checkpoint.Plan.PublicSocket))
}

func (w *Workflow) failV2AfterMarker(ctx context.Context, operation protectionrepository.OperationLockModel, checkpoint CheckpointV2, code string) (WorkflowResultV2, error) {
	checkpoint.FailedStage = code
	checkpoint.ReasonCodes = canonicalV2ReasonCodes(append(checkpoint.ReasonCodes, code))
	recoveryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), v2RecoveryTimeout)
	defer cancel()
	result, rollbackErr := w.rollbackV2(recoveryCtx, operation, checkpoint, true)
	if rollbackErr != nil {
		return result, rollbackErr
	}
	return result, v2Error(code, code == "ambiguous_result" || code == "reload_failed")
}

func (w *Workflow) rollbackV2(ctx context.Context, operation protectionrepository.OperationLockModel, checkpoint CheckpointV2, automatic bool) (WorkflowResultV2, error) {
	if operation.State == protectionoperations.StateRolledBack {
		return resultV2(operation, checkpoint), nil
	}
	if checkpoint.RollbackAttemptCount > 0 {
		return w.finishV2RecoveryFailure(ctx, operation, checkpoint, protectionoperations.StateReconcileRequired, "reconcile_required")
	}
	var rolling protectionrepository.OperationLockModel
	var err error
	switch operation.State {
	case protectionoperations.StateApplied:
		rolling, err = w.Manager.BeginRollback(ctx, operation.OperationID, operation.Revision)
	case protectionoperations.StatePrepared, protectionoperations.StateApplying, protectionoperations.StateHealth, protectionoperations.StateHealthFailed:
		if operation.State == protectionoperations.StatePrepared {
			applying, transitionErr := w.Manager.Transition(ctx, operation.OperationID, operation.Revision, protectionoperations.StateApplying)
			if transitionErr != nil {
				return WorkflowResultV2{}, transitionErr
			}
			operation = applying
		}
		rolling, err = w.Manager.Transition(ctx, operation.OperationID, operation.Revision, protectionoperations.StateRollingBack)
	case protectionoperations.StateRollingBack:
		rolling = operation
	default:
		return WorkflowResultV2{}, v2Error("reconcile_required", true)
	}
	if err != nil {
		return WorkflowResultV2{}, err
	}
	checkpoint.OperationRevision, checkpoint.RollbackAttemptCount = rolling.Revision, checkpoint.RollbackAttemptCount+1
	if err := w.saveV2(&checkpoint, "rollback_intent"); err != nil {
		return w.finishV2RecoveryFailure(ctx, rolling, checkpoint, protectionoperations.StateRollbackFailed, "rollback_failed")
	}
	capabilities, capErr := w.capabilities(ctx)
	if capErr != nil || !frontingCapabilitiesAvailable(capabilities) || engineIdentityRevisionV2(capabilities) != checkpoint.EngineIdentityRevision {
		return w.finishV2RecoveryFailure(ctx, rolling, checkpoint, protectionoperations.StateRollbackFailed, "runtime_identity_stale")
	}
	activePrevious := capabilities.Nginx.ActiveRevision == checkpoint.PreviousRevision && capabilities.Nginx.ActiveSHA256 == checkpoint.PreviousSHA256
	activeCandidate := capabilities.Nginx.ActiveRevision == checkpoint.CandidateRevision && capabilities.Nginx.ActiveSHA256 == checkpoint.CandidateSHA256
	if !activePrevious && !activeCandidate {
		return w.finishV2RecoveryFailure(ctx, rolling, checkpoint, protectionoperations.StateReconcileRequired, "rollback_drift")
	}
	if activeCandidate {
		if code := w.verifyRollbackSourceV2(ctx, rolling, checkpoint, capabilities.Nginx.Binary); code != "" {
			return w.finishV2RecoveryFailure(ctx, rolling, checkpoint, protectionoperations.StateReconcileRequired, code)
		}
		response, restoreErr := w.execute(ctx, protectionhelper.Request{ProtocolVersion: protectionhelper.ProtocolVersion, Correlation: w.correlation(rolling),
			Operation: protectionhelper.OperationNginxRestore, NginxRestore: &protectionhelper.NginxRestoreRequest{ExpectedCurrentRevision: checkpoint.CandidateRevision,
				PreviousRevision: checkpoint.PreviousRevision, ExpectedSHA256: checkpoint.PreviousSHA256}})
		if restoreErr != nil || response.Nginx == nil || response.Nginx.Revision != checkpoint.PreviousRevision {
			return w.finishV2RecoveryFailure(ctx, rolling, checkpoint, protectionoperations.StateRollbackFailed, "rollback_failed")
		}
		checkpoint.Restored = true
		if err := w.saveV2(&checkpoint, "previous_restored"); err != nil {
			return w.finishV2RecoveryFailure(ctx, rolling, checkpoint, protectionoperations.StateRollbackFailed, "rollback_failed")
		}
		response, reloadErr := w.execute(ctx, protectionhelper.Request{ProtocolVersion: protectionhelper.ProtocolVersion, Correlation: w.correlation(rolling),
			Operation: protectionhelper.OperationNginxReload, NginxReload: &protectionhelper.NginxReloadRequest{ExpectedRevision: checkpoint.PreviousRevision,
				ExpectedSHA256: checkpoint.PreviousSHA256, ExpectedBinary: capabilities.Nginx.Binary}})
		if reloadErr != nil || response.Nginx == nil || response.Nginx.MasterPID <= 0 || len(response.Nginx.WorkerPIDs) == 0 {
			return w.finishV2RecoveryFailure(ctx, rolling, checkpoint, protectionoperations.StateRollbackFailed, "rollback_failed")
		}
		checkpoint.RollbackReloaded = true
	}
	if _, code := w.verifyEngineRevisionV2(ctx, rolling, checkpoint.PreviousRevision, checkpoint.PreviousSHA256, capabilities.Nginx.Binary, checkpoint.PreviousListeners); code != "" {
		return w.finishV2RecoveryFailure(ctx, rolling, checkpoint, protectionoperations.StateRollbackFailed, "rollback_failed")
	}
	checkpoint.Detached, checkpoint.ActualActiveRevision = true, checkpoint.PreviousRevision
	rollbackHealth := boundedHealth(ctx, w.RollbackHealth, nil, "fronting_rollback_health_timeout")
	if healthFailed(rollbackHealth) {
		return w.finishV2RecoveryFailure(ctx, rolling, checkpoint, protectionoperations.StateRollbackFailed, "rollback_failed")
	}
	authorities, leaseCode := w.currentTargetAuthoritiesV2(ctx, checkpoint)
	if leaseCode != "" {
		return w.finishV2RecoveryFailure(ctx, rolling, checkpoint, protectionoperations.StateRollbackFailed, leaseCode)
	}
	released, releaseCode := releaseTargetAuthoritiesV2(ctx, authorities, rolling.OperationID+"-release", detachmentRevisionV2(checkpoint), w.nowV2())
	if releaseCode != "" {
		updateCheckpointAuthoritiesV2(&checkpoint, released)
		_ = w.saveV2(&checkpoint, "lease_release_partial")
		return w.finishV2RecoveryFailure(ctx, rolling, checkpoint, protectionoperations.StateRollbackFailed, releaseCode)
	}
	updateCheckpointAuthoritiesV2(&checkpoint, released)
	if err := w.saveV2(&checkpoint, "lease_released"); err != nil {
		return w.finishV2RecoveryFailure(ctx, rolling, checkpoint, protectionoperations.StateRollbackFailed, "rollback_failed")
	}
	rolled, err := w.Manager.Transition(ctx, rolling.OperationID, rolling.Revision, protectionoperations.StateRolledBack)
	checkpoint.OperationRevision = rolled.Revision
	_ = w.saveV2(&checkpoint, "rolled_back")
	return resultV2(rolled, checkpoint), err
}

func (w *Workflow) verifyRollbackSourceV2(ctx context.Context, operation protectionrepository.OperationLockModel, checkpoint CheckpointV2, binary protectionhelper.BinaryIdentity) string {
	if _, code := w.verifyEngineRevisionV2(ctx, operation, checkpoint.CandidateRevision, checkpoint.CandidateSHA256, binary, helperListenerV2(checkpoint.Plan.PublicSocket)); code == "" {
		return ""
	}
	// A failed reload may legitimately leave the exact previous listener set
	// running while the candidate symlink is current. Both states are safe
	// rollback sources; any other listener/process state is external drift.
	if _, code := w.verifyEngineRevisionV2(ctx, operation, checkpoint.CandidateRevision, checkpoint.CandidateSHA256, binary, checkpoint.PreviousListeners); code == "" {
		return ""
	}
	return "rollback_drift"
}

func (w *Workflow) finishV2RecoveryFailure(ctx context.Context, operation protectionrepository.OperationLockModel, checkpoint CheckpointV2, state, code string) (WorkflowResultV2, error) {
	checkpoint.FailedStage = code
	checkpoint.RecoveryClassification = state
	checkpoint.ReasonCodes = canonicalV2ReasonCodes(append(checkpoint.ReasonCodes, code))
	_ = w.saveV2(&checkpoint, state)
	if checkpoint.ArtifactRevision != "" && w.Recovery != nil {
		_ = w.Recovery.CreateBundle(context.WithoutCancel(ctx), operation, state)
	}
	updated, err := w.Manager.Transition(context.WithoutCancel(ctx), operation.OperationID, operation.Revision, state)
	if err != nil {
		return resultV2(operation, checkpoint), v2Error(code, true)
	}
	checkpoint.OperationRevision = updated.Revision
	_ = w.saveV2(&checkpoint, state)
	return resultV2(updated, checkpoint), v2Error(code, true)
}

func (w *Workflow) cancelV2BeforeMarker(ctx context.Context, operation protectionrepository.OperationLockModel, checkpoint CheckpointV2, code string) (WorkflowResultV2, error) {
	if checkpoint.OperationID == "" {
		checkpoint = CheckpointV2{Version: 2, Schema: FrontingWorkflowCheckpointSchemaV2, OperationID: operation.OperationID, OperationRevision: operation.Revision,
			ReasonCodes: []string{}, Timeline: []TimelineEvent{}}
	}
	checkpoint.FailedStage = code
	checkpoint.ReasonCodes = canonicalV2ReasonCodes(append(checkpoint.ReasonCodes, code))
	if len(checkpoint.EndpointLeases)+len(checkpoint.FallbackReservations) > 0 {
		authorities, authorityCode := w.currentPersistedTargetAuthoritiesV2(context.WithoutCancel(ctx), checkpoint)
		if authorityCode != "" {
			return w.finishV2RecoveryFailure(ctx, operation, checkpoint, protectionoperations.StateReconcileRequired, authorityCode)
		}
		released, releaseCode := releaseTargetAuthoritiesV2(context.WithoutCancel(ctx), authorities, operation.OperationID+"-cancel", preMutationDetachmentRevisionV2(checkpoint), w.nowV2())
		if releaseCode != "" {
			updateCheckpointAuthoritiesV2(&checkpoint, released)
			_ = w.saveV2(&checkpoint, "cancel_release_partial")
			return w.finishV2RecoveryFailure(ctx, operation, checkpoint, protectionoperations.StateReconcileRequired, releaseCode)
		}
		updateCheckpointAuthoritiesV2(&checkpoint, released)
	}
	_ = w.saveV2(&checkpoint, "cancelled_before_mutation")
	cancelled, transitionErr := w.Manager.Transition(context.WithoutCancel(ctx), operation.OperationID, operation.Revision, protectionoperations.StateCancelled)
	if transitionErr != nil {
		return resultV2(operation, checkpoint), v2Error("ambiguous_result", true)
	}
	checkpoint.OperationRevision = cancelled.Revision
	_ = w.saveV2(&checkpoint, "cancelled_before_mutation")
	return resultV2(cancelled, checkpoint), v2Error(code, false)
}

func (w *Workflow) revalidatePlanV2(ctx context.Context, plan FrontingStrategyPlanV2) (revalidatedPlanV2, string) {
	now := w.nowV2()
	if plan.Validate() != nil || plan.ExpiresAt <= now.Unix() {
		return revalidatedPlanV2{}, "plan_expired"
	}
	input, err := w.V2Plans.CurrentFrontingPlanInputV2(ctx, plan)
	if err != nil {
		return revalidatedPlanV2{}, "plan_stale"
	}
	if input.Runtime.Validate(now) != nil || input.Runtime.CanonicalRuntimeIdentityRevision != plan.Runtime.IdentityRevision {
		return revalidatedPlanV2{}, "runtime_identity_stale"
	}
	if input.Socket.Validate(now) != nil || input.Socket.ClaimRevision != plan.PublicSocket.ClaimRevision {
		return revalidatedPlanV2{}, "socket_claim_stale"
	}
	if !input.Socket.TopologyMutationEligible {
		return revalidatedPlanV2{}, "topology_mutation_blocked"
	}
	facts := make(map[string]hostresources.FrontingBackendFactV1, len(plan.Targets.BackendReferences))
	for _, reference := range plan.Targets.BackendReferences {
		found := false
		for _, fact := range input.Inventory {
			if fact.ProviderID != reference.ProviderID || fact.ResourceID != reference.ResourceID || fact.EndpointID != reference.EndpointID {
				continue
			}
			if fact.CanReachManagement != hostresources.CapabilityNo {
				return revalidatedPlanV2{}, "backend_management_forbidden"
			}
			if reference.SelectedProxyMode == hostresources.ProxyModeOn && fact.AcceptsProxyProtocol != hostresources.CapabilityYes {
				return revalidatedPlanV2{}, "proxy_protocol_mismatch"
			}
			if hostresources.ResolveExactFrontingBackendV1(reference, fact, now) != nil {
				return revalidatedPlanV2{}, "backend_reference_stale"
			}
			facts[reference.CanonicalReferenceRevision], found = fact, true
			break
		}
		if !found {
			return revalidatedPlanV2{}, "backend_reference_stale"
		}
	}
	fallbacks := make(map[string]FallbackPlanningTargetV2, len(plan.Targets.FallbackReferences))
	for _, reference := range plan.Targets.FallbackReferences {
		found := false
		for _, item := range input.FallbackTargets {
			if item.Reference != reference {
				continue
			}
			if fallbacktargets.ResolveExactV2(reference, item.Target, now) != nil {
				return revalidatedPlanV2{}, "fallback_reference_stale"
			}
			fallbacks[v2Revision(reference)], found = item, true
			break
		}
		if !found {
			return revalidatedPlanV2{}, "fallback_reference_stale"
		}
	}
	input.Now = time.Unix(plan.CreatedAt, 0).UTC()
	regenerated, err := PlanFrontingStrategyV2(input)
	if err != nil || regenerated.CanonicalPlanDigest != plan.CanonicalPlanDigest || regenerated.PlanID != plan.PlanID {
		return revalidatedPlanV2{}, "plan_stale"
	}
	return revalidatedPlanV2{Plan: plan, Input: input, Facts: facts, Fallbacks: fallbacks}, ""
}

func (w *Workflow) revalidateActiveBindingsV2(ctx context.Context, plan FrontingStrategyPlanV2) (string, string) {
	now := w.nowV2()
	current, code := w.revalidatePlanV2(ctx, plan)
	if code != "" {
		return "", code
	}
	backendProxy := CapabilitySupportedV2
	if plan.Targets.SelectedProxyMode == hostresources.ProxyModeOn {
		for _, fact := range current.Facts {
			if fact.AcceptsProxyProtocol != hostresources.CapabilityYes {
				backendProxy = backendProxyTruthV2(fact.AcceptsProxyProtocol)
			}
		}
		for _, item := range current.Fallbacks {
			if item.Target.Endpoint.ProxyProtocol != hostresources.CapabilityYes {
				backendProxy = backendProxyTruthV2(item.Target.Endpoint.ProxyProtocol)
			}
		}
	}
	capability := ResolveNginxStrategyCapabilityV2(current.Input.Runtime, plan.Strategy.Selected, plan.Targets.SelectedProxyMode, backendProxy, now)
	if !capability.Actionable || capability.Support != StrategySupportedV2 || capability.CapabilityRevision != plan.StrategyCapabilityRevision {
		return "", "capability_stale"
	}
	return capability.CapabilityRevision, ""
}

func (w *Workflow) checkHealthV2(ctx context.Context, operation protectionrepository.OperationLockModel, checkpoint CheckpointV2, lease hostresources.EndpointLeaseV1) (FixedL4HealthEvidenceV2, string) {
	request := FixedL4HealthRequestV2{OperationID: operation.OperationID, OperationRevision: operation.Revision, PlanDigest: checkpoint.Plan.CanonicalPlanDigest,
		CandidateRevision: checkpoint.CandidateRevision, CandidateSHA256: checkpoint.CandidateSHA256, SocketClaimRevision: checkpoint.SocketClaimRevision,
		BackendReferenceRevision: checkpoint.BackendReferenceRevision, LeaseRevision: lease.LeaseRevision, ProxyMode: checkpoint.SelectedProxyMode}
	evidence, err := boundedFixedL4HealthV2(ctx, w.V2Health, request)
	if err != nil {
		return FixedL4HealthEvidenceV2{}, "health_failed"
	}
	if validationErr := validateHealthEvidenceV2(request, evidence, w.nowV2()); validationErr != nil {
		if validationErr.Error() == "proxy_protocol_mismatch" {
			return FixedL4HealthEvidenceV2{}, "proxy_protocol_mismatch"
		}
		return FixedL4HealthEvidenceV2{}, "health_failed"
	}
	return evidence, ""
}

func (w *Workflow) checkStrategyHealthV2(ctx context.Context, operation protectionrepository.OperationLockModel, checkpoint *CheckpointV2) string {
	switch checkpoint.Plan.Strategy.Selected {
	case StrategyL4OneToOne:
		if len(checkpoint.EndpointLeases) != 1 {
			return "lease_stale"
		}
		health, code := w.checkHealthV2(ctx, operation, *checkpoint, checkpoint.EndpointLeases[0])
		if code == "" {
			checkpoint.Health = health
		}
		return code
	case StrategySNIPreread:
		request, err := buildSNIHealthRequestV2(operation.OperationID, operation.Revision, *checkpoint)
		if err != nil {
			return "lease_stale"
		}
		evidence, err := boundedSNIHealthV2(ctx, w.V2SNIHealth, request)
		if err != nil || validateSNIHealthEvidenceV2(request, evidence, w.nowV2()) != nil {
			return "health_failed"
		}
		checkpoint.SNIHealth = evidence
		return ""
	default:
		return "health_failed"
	}
}

func validateHealthEvidenceV2(request FixedL4HealthRequestV2, evidence FixedL4HealthEvidenceV2, now time.Time) error {
	wantProxy := request.ProxyMode == hostresources.ProxyModeOn
	if evidence.ProxyMode == request.ProxyMode && evidence.ProxyHeaderObserved != wantProxy {
		return errors.New("proxy_protocol_mismatch")
	}
	if evidence.Schema != FixedL4HealthSchemaV2 || evidence.OperationID != request.OperationID || evidence.OperationRevision != request.OperationRevision ||
		evidence.PlanDigest != request.PlanDigest || evidence.CandidateRevision != request.CandidateRevision || evidence.CandidateSHA256 != request.CandidateSHA256 ||
		evidence.SocketClaimRevision != request.SocketClaimRevision || evidence.BackendReferenceRevision != request.BackendReferenceRevision ||
		evidence.LeaseRevision != request.LeaseRevision || evidence.ProxyMode != request.ProxyMode || !evidence.PublicFixtureAccepted || !evidence.ExpectedBackendReached ||
		evidence.BackendIdentityMarker != request.BackendReferenceRevision || evidence.AlternateTargetReceipts != 0 || evidence.ProxyHeaderObserved != wantProxy ||
		evidence.ObservedAt <= 0 || evidence.ExpiresAt <= evidence.ObservedAt || evidence.ExpiresAt-evidence.ObservedAt > int64(frontingHealthTimeout/time.Second) ||
		evidence.ObservedAt > now.Unix() || evidence.ExpiresAt <= now.Unix() || evidence.LatencyMilliseconds > uint32(frontingHealthTimeout/time.Millisecond) || len(evidence.ReasonCodes) != 0 {
		return errors.New("health_failed")
	}
	return nil
}

type fixedL4HealthResultV2 struct {
	evidence FixedL4HealthEvidenceV2
	err      error
}

type endpointLeaseListResultV2 struct {
	leases []hostresources.EndpointLeaseV1
	err    error
}

type endpointLeaseCallResultV2 struct {
	lease hostresources.EndpointLeaseV1
	err   error
}

func safeLeaseListByHolderV2(ctx context.Context, directory EndpointLeaseDirectoryV1, holder string) ([]hostresources.EndpointLeaseV1, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	listCtx, cancel := context.WithTimeout(ctx, v2ProviderTimeout)
	defer cancel()
	result := make(chan endpointLeaseListResultV2, 1)
	go func() {
		defer func() {
			if recover() != nil {
				result <- endpointLeaseListResultV2{err: errors.New("lease_provider_unavailable")}
			}
		}()
		leases, err := directory.EndpointLeasesByHolderV1(listCtx, holder)
		result <- endpointLeaseListResultV2{leases: leases, err: err}
	}()
	select {
	case value := <-result:
		return value.leases, value.err
	case <-listCtx.Done():
		return nil, errors.New("lease_provider_unavailable")
	}
}

func boundedFixedL4HealthV2(ctx context.Context, check FixedL4HealthCheckV2, request FixedL4HealthRequestV2) (FixedL4HealthEvidenceV2, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	healthCtx, cancel := context.WithTimeout(ctx, frontingHealthTimeout)
	defer cancel()
	result := make(chan fixedL4HealthResultV2, 1)
	go func() {
		defer func() {
			if recover() != nil {
				result <- fixedL4HealthResultV2{err: errors.New("health_failed")}
			}
		}()
		evidence, err := check(healthCtx, request)
		result <- fixedL4HealthResultV2{evidence: evidence, err: err}
	}()
	select {
	case value := <-result:
		return value.evidence, value.err
	case <-healthCtx.Done():
		return FixedL4HealthEvidenceV2{}, errors.New("health_failed")
	}
}

func (w *Workflow) verifyHistoricalAppliedV2(ctx context.Context, operation protectionrepository.OperationLockModel, checkpoint *CheckpointV2) string {
	authorities, code := w.currentTargetAuthoritiesV2(ctx, *checkpoint)
	if code != "" || !authoritiesInStateV2(authorities, hostresources.EndpointLeaseActive, fallbacktargets.ReservationActive) {
		return firstV2Code(code, "lease_lost")
	}
	updateCheckpointAuthoritiesV2(checkpoint, authorities)
	capabilities, err := w.capabilities(ctx)
	if err != nil || !frontingCapabilitiesAvailable(capabilities) ||
		engineIdentityRevisionV2(capabilities) != checkpoint.EngineIdentityRevision || capabilities.Nginx.ActiveRevision != checkpoint.CandidateRevision ||
		capabilities.Nginx.ActiveSHA256 != checkpoint.CandidateSHA256 {
		return "active_revision_mismatch"
	}
	if !checkpoint.Verified || checkpoint.ActualActiveRevision != checkpoint.CandidateRevision ||
		!frontingHexV2(checkpoint.ProcessIdentityRevision) || !frontingHexV2(checkpoint.WorkerSetIdentityRevision) ||
		!frontingHexV2(checkpoint.ListenerVerificationRevision) {
		return "active_revision_mismatch"
	}
	if capabilities.Nginx.MasterPID <= 0 || checkpoint.ProcessIdentityRevision != v2Revision(struct {
		Binary protectionhelper.BinaryIdentity
		Master int
	}{capabilities.Nginx.Binary, capabilities.Nginx.MasterPID}) {
		return "process_identity_mismatch"
	}
	expectedListeners := helperListenerV2(checkpoint.Plan.PublicSocket)
	if checkpoint.ListenerVerificationRevision != v2Revision(expectedListeners) || len(capabilities.Nginx.Listeners) != len(expectedListeners) {
		return "listener_identity_mismatch"
	}
	for index := range expectedListeners {
		if capabilities.Nginx.Listeners[index] != expectedListeners[index] {
			return "listener_identity_mismatch"
		}
	}
	activeCapability, activeCode := w.revalidateActiveBindingsV2(ctx, checkpoint.Plan)
	if activeCode != "" {
		return activeCode
	}
	checkpoint.ActiveStrategyCapabilityRevision = activeCapability
	if healthCode := w.checkStrategyHealthV2(ctx, operation, checkpoint); healthCode != "" {
		return healthCode
	}
	_ = w.saveV2(checkpoint, "applied_reverified")
	return ""
}

func (w *Workflow) verifyPreparedArtifactV2(checkpoint CheckpointV2) error {
	if checkpoint.ArtifactRevision == "" || checkpoint.ArtifactManifestSHA256 == "" || checkpoint.ArtifactRevision != checkpoint.CandidateRevision {
		return errors.New("artifact_integrity_failed")
	}
	manifest, err := w.V2Artifacts.VerifyRevision(checkpoint.ArtifactRevision, checkpoint.ArtifactManifestSHA256)
	if err != nil || manifest.OperationID != checkpoint.OperationID || manifest.Revision != checkpoint.ArtifactRevision {
		return errors.New("artifact_integrity_failed")
	}
	if len(manifest.Files) != 5 {
		return errors.New("artifact_integrity_failed")
	}
	plan, _ := json.Marshal(checkpoint.Plan)
	rollback, _ := json.Marshal(struct {
		Revision  string                           `json:"revision"`
		SHA256    string                           `json:"sha256"`
		Listeners []protectionhelper.NginxListener `json:"listeners"`
	}{checkpoint.PreviousRevision, checkpoint.PreviousSHA256, checkpoint.PreviousListeners})
	wanted := map[string]string{
		"candidate.conf":         checkpoint.CandidateSHA256,
		"candidate.sha256":       fixedL4DigestV2([]byte(checkpoint.CandidateSHA256 + "\n")),
		"canonical-plan.json":    fixedL4DigestV2(append(plan, '\n')),
		"candidate-binding.json": "",
		"rollback.json":          fixedL4DigestV2(append(rollback, '\n')),
	}
	for _, file := range manifest.Files {
		expected, ok := wanted[file.Path]
		if !ok || file.Bytes <= 0 || file.Bytes > MaxFutureCandidateBytesV1 || expected != "" && file.SHA256 != expected {
			return errors.New("artifact_integrity_failed")
		}
		delete(wanted, file.Path)
	}
	if len(wanted) != 0 {
		return errors.New("artifact_integrity_failed")
	}
	return nil
}

func (w *Workflow) saveV2(checkpoint *CheckpointV2, phase string) error {
	if checkpoint.Plan.Strategy.Selected == StrategyL4OneToOne && checkpoint.Lease.LeaseID != "" &&
		(len(checkpoint.EndpointLeases) == 0 || len(checkpoint.EndpointLeases) == 1 && checkpoint.EndpointLeases[0].LeaseRevision != checkpoint.Lease.LeaseRevision) {
		checkpoint.EndpointLeases = []hostresources.EndpointLeaseV1{checkpoint.Lease}
	}
	canonicalizeCheckpointAuthoritiesV2(checkpoint)
	checkpoint.Checkpoint = phase
	checkpoint.ReasonCodes = canonicalV2ReasonCodes(checkpoint.ReasonCodes)
	checkpoint.Timeline = append(checkpoint.Timeline, TimelineEvent{Checkpoint: phase, At: w.nowV2().Unix()})
	if len(checkpoint.Timeline) > 64 {
		checkpoint.Timeline = checkpoint.Timeline[len(checkpoint.Timeline)-64:]
	}
	data, err := json.Marshal(checkpoint)
	if err != nil {
		return errors.New("artifact_integrity_failed")
	}
	return w.State.WriteFrontingState(checkpoint.OperationID, append(data, '\n'))
}

func (w *Workflow) loadV2(operationID string) (CheckpointV2, error) {
	data, err := w.State.ReadFrontingState(operationID)
	if err != nil {
		return CheckpointV2{}, err
	}
	var checkpoint CheckpointV2
	if json.Unmarshal(data, &checkpoint) != nil {
		return CheckpointV2{}, errors.New("artifact_integrity_failed")
	}
	if len(checkpoint.EndpointLeases) == 0 && checkpoint.Lease.LeaseID != "" {
		checkpoint.EndpointLeases = []hostresources.EndpointLeaseV1{checkpoint.Lease}
	}
	if len(checkpoint.BackendReferenceRevisions) == 0 {
		checkpoint.BackendReferenceRevisions = append([]string(nil), checkpoint.Plan.Targets.ReferenceRevisions...)
	}
	canonicalizeCheckpointAuthoritiesV2(&checkpoint)
	if checkpoint.Version != 2 || checkpoint.Schema != FrontingWorkflowCheckpointSchemaV2 ||
		checkpoint.OperationID != operationID || checkpoint.Plan.Validate() != nil ||
		validateWorkflowPlanShapeV2(checkpoint.Plan, time.Unix(checkpoint.Plan.CreatedAt, 0).UTC()) != nil || checkpoint.Plan.CanonicalPlanDigest == "" ||
		checkpoint.RuntimeIdentityRevision != checkpoint.Plan.Runtime.IdentityRevision || checkpoint.StrategyCapabilityRevision != checkpoint.Plan.StrategyCapabilityRevision ||
		checkpoint.SocketClaimRevision != checkpoint.Plan.PublicSocket.ClaimRevision || !equalStringsV2(checkpoint.BackendReferenceRevisions, checkpoint.Plan.Targets.ReferenceRevisions) ||
		checkpoint.SelectedProxyMode != checkpoint.Plan.Targets.SelectedProxyMode || len(checkpoint.ReasonCodes) > 32 || len(checkpoint.Timeline) > 64 {
		return CheckpointV2{}, errors.New("artifact_integrity_failed")
	}
	if checkpoint.Plan.Strategy.Selected == StrategyL4OneToOne && checkpoint.BackendReferenceRevision != checkpoint.Plan.Targets.BackendReferences[0].CanonicalReferenceRevision {
		return CheckpointV2{}, errors.New("artifact_integrity_failed")
	}
	if _, err := authorityRevisionsV2(checkpoint); err != nil && len(checkpoint.EndpointLeases)+len(checkpoint.FallbackReservations) != 0 {
		return CheckpointV2{}, errors.New("lease_stale")
	}
	for _, lease := range checkpoint.EndpointLeases {
		if lease.HolderID != operationID {
			return CheckpointV2{}, errors.New("lease_stale")
		}
	}
	for _, reservation := range checkpoint.FallbackReservations {
		if reservation.HolderID != operationID || reservation.Purpose != fallbacktargets.ReservationPurposeFronting {
			return CheckpointV2{}, errors.New("lease_stale")
		}
	}
	if checkpoint.CandidateRevision != "" && (!frontingHexV2(checkpoint.CandidateRevision) || !frontingHexV2(checkpoint.CandidateSHA256) || checkpoint.ExpectedActiveRevision != checkpoint.CandidateRevision) {
		return CheckpointV2{}, errors.New("artifact_integrity_failed")
	}
	if checkpoint.Plan.Strategy.Selected == StrategySNIPreread && checkpoint.CandidateRevision != "" &&
		(checkpoint.SelectorSetRevision != checkpoint.Plan.Selectors.SelectorSetRevision || !frontingHexV2(checkpoint.MapRevision) || !frontingHexV2(checkpoint.UpstreamIDSetRevision)) {
		return CheckpointV2{}, errors.New("artifact_integrity_failed")
	}
	return checkpoint, nil
}

func (w *Workflow) readyV2() error {
	if err := w.ready(); err != nil || w.V2Plans == nil || w.V2Leases == nil || w.V2Artifacts == nil || w.V2Health == nil {
		return v2Error("fronting_workflow_unavailable", false)
	}
	return nil
}

func (w *Workflow) nowV2() time.Time {
	if w != nil && w.Now != nil {
		return w.Now().UTC().Truncate(time.Second)
	}
	return time.Now().UTC().Truncate(time.Second)
}

func resultV2(operation protectionrepository.OperationLockModel, checkpoint CheckpointV2) WorkflowResultV2 {
	authorities, _ := authorityRevisionsV2(checkpoint)
	authorityRevisions := make([]string, 0, len(authorities))
	for _, revision := range authorities {
		authorityRevisions = append(authorityRevisions, revision)
	}
	sort.Strings(authorityRevisions)
	return WorkflowResultV2{OperationID: operation.OperationID, OperationRevision: operation.Revision, State: operation.State,
		PlanID: checkpoint.Plan.PlanID, PlanDigest: checkpoint.Plan.CanonicalPlanDigest, Strategy: checkpoint.Plan.Strategy.Selected,
		CandidateRevision: checkpoint.CandidateRevision, CandidateSHA256: checkpoint.CandidateSHA256, PreviousRevision: checkpoint.PreviousRevision,
		LeaseID: checkpoint.Lease.LeaseID, LeaseRevision: checkpoint.Lease.LeaseRevision, LeaseState: checkpoint.Lease.State,
		TargetAuthorityRevisions: authorityRevisions,
		RecoveryRequired:         operation.State == protectionoperations.StateRollbackFailed || operation.State == protectionoperations.StateReconcileRequired,
		ReasonCodes:              append([]string(nil), checkpoint.ReasonCodes...)}
}

func candidateFilesV2(candidate renderedCandidateV2, checkpoint CheckpointV2) map[string][]byte {
	plan, _ := json.Marshal(checkpoint.Plan)
	rollback, _ := json.Marshal(struct {
		Revision  string                           `json:"revision"`
		SHA256    string                           `json:"sha256"`
		Listeners []protectionhelper.NginxListener `json:"listeners"`
	}{checkpoint.PreviousRevision, checkpoint.PreviousSHA256, checkpoint.PreviousListeners})
	return map[string][]byte{"candidate.conf": candidate.Bytes, "candidate.sha256": []byte(candidate.SHA256 + "\n"),
		"canonical-plan.json": append(plan, '\n'), "candidate-binding.json": append(candidate.CanonicalInput, '\n'), "rollback.json": append(rollback, '\n')}
}

func helperListenerV2(claim FrontingSocketClaimV1) []protectionhelper.NginxListener {
	return []protectionhelper.NginxListener{{Address: claim.CanonicalBind, Port: int(claim.PublicPort)}}
}

func engineIdentityRevisionV2(capabilities *protectionhelper.CapabilitiesResult) string {
	if capabilities == nil {
		return ""
	}
	return v2Revision(struct {
		PlatformKnown    bool
		Linux            bool
		Available        bool
		Binary           protectionhelper.BinaryIdentity
		ManagedRoot      string
		ControlledConfig string
	}{capabilities.Nginx.PlatformKnown, capabilities.Nginx.Linux, capabilities.Nginx.Available,
		capabilities.Nginx.Binary, capabilities.Nginx.ManagedRoot, capabilities.Nginx.ControlledConfig})
}

func preMutationDetachmentRevisionV2(checkpoint CheckpointV2) string {
	return v2Revision(struct {
		Operation string
		Previous  string
		Candidate string
		Marker    bool
	}{checkpoint.OperationID, checkpoint.PreviousRevision, checkpoint.CandidateRevision, false})
}

func detachmentRevisionV2(checkpoint CheckpointV2) string {
	return v2Revision(struct {
		Operation   string
		Previous    string
		PreviousSHA string
		Candidate   string
		Detached    bool
	}{checkpoint.OperationID, checkpoint.PreviousRevision, checkpoint.PreviousSHA256, checkpoint.CandidateRevision, checkpoint.Detached})
}

func canonicalV2ReasonCodes(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, min(len(values), 32))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if safeRuntimeTokenV2(value, 64) == "" {
			value = "reconcile_required"
		}
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
		if len(result) == 32 {
			break
		}
	}
	sort.Strings(result)
	return result
}

func v2Error(code string, ambiguous bool) error {
	return &WorkflowErrorV2{Code: code, Ambiguous: ambiguous}
}

func candidateErrorCodeV2(err error) string {
	if err != nil {
		switch err.Error() {
		case "candidate_too_large", "proxy_protocol_mismatch":
			return err.Error()
		}
	}
	return "candidate_invalid"
}

func firstV2Code(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func fenceLeaseV2(ctx context.Context, provider hostresources.EndpointLeaseProviderV1, current hostresources.EndpointLeaseV1, requestID string, now time.Time) (hostresources.EndpointLeaseV1, string) {
	request := hostresources.MutateEndpointLeaseRequestV1{RequestID: requestID, LeaseID: current.LeaseID, ExpectedRevision: current.LeaseRevision}
	if request.Validate(false) != nil {
		return hostresources.EndpointLeaseV1{}, "lease_stale"
	}
	next, err := safeLeaseCallV2(ctx, func(callCtx context.Context) (hostresources.EndpointLeaseV1, error) {
		return provider.FenceEndpointLease(callCtx, request)
	})
	if err != nil {
		return hostresources.EndpointLeaseV1{}, "ambiguous_result"
	}
	cas := hostresources.EndpointLeaseCASV1{RequestID: request.RequestID, LeaseID: request.LeaseID, ExpectedRevision: request.ExpectedRevision}
	if hostresources.ValidateEndpointLeaseTransitionV1(current, next, cas, hostresources.EndpointLeaseFence, now) != nil {
		return hostresources.EndpointLeaseV1{}, "lease_stale"
	}
	return next, ""
}

func activateLeaseV2(ctx context.Context, provider hostresources.EndpointLeaseProviderV1, current hostresources.EndpointLeaseV1, requestID string, now time.Time) (hostresources.EndpointLeaseV1, string) {
	request := hostresources.MutateEndpointLeaseRequestV1{RequestID: requestID, LeaseID: current.LeaseID, ExpectedRevision: current.LeaseRevision,
		FreshnessSeconds: uint32(hostresources.MaxEndpointLeaseFreshnessV1 / time.Second)}
	if request.Validate(true) != nil {
		return hostresources.EndpointLeaseV1{}, "lease_stale"
	}
	next, err := safeLeaseCallV2(ctx, func(callCtx context.Context) (hostresources.EndpointLeaseV1, error) {
		return provider.ActivateEndpointLease(callCtx, request)
	})
	if err != nil {
		return hostresources.EndpointLeaseV1{}, "ambiguous_result"
	}
	cas := hostresources.EndpointLeaseCASV1{RequestID: request.RequestID, LeaseID: request.LeaseID, ExpectedRevision: request.ExpectedRevision}
	if hostresources.ValidateEndpointLeaseTransitionV1(current, next, cas, hostresources.EndpointLeaseActivate, now) != nil {
		return hostresources.EndpointLeaseV1{}, "lease_stale"
	}
	return next, ""
}

func releaseLeaseV2(ctx context.Context, provider hostresources.EndpointLeaseProviderV1, current hostresources.EndpointLeaseV1, requestID, detachmentRevision string, now time.Time) (hostresources.EndpointLeaseV1, string) {
	request := hostresources.ReleaseEndpointLeaseRequestV1{RequestID: requestID, LeaseID: current.LeaseID, ExpectedRevision: current.LeaseRevision, DetachmentRevision: detachmentRevision}
	if request.Validate() != nil {
		return hostresources.EndpointLeaseV1{}, "lease_stale"
	}
	next, err := safeLeaseCallV2(ctx, func(callCtx context.Context) (hostresources.EndpointLeaseV1, error) {
		return provider.ReleaseEndpointLease(callCtx, request)
	})
	if err != nil {
		return hostresources.EndpointLeaseV1{}, "ambiguous_result"
	}
	cas := hostresources.EndpointLeaseCASV1{RequestID: request.RequestID, LeaseID: request.LeaseID, ExpectedRevision: request.ExpectedRevision}
	if hostresources.ValidateEndpointLeaseTransitionV1(current, next, cas, hostresources.EndpointLeaseRelease, now) != nil {
		return hostresources.EndpointLeaseV1{}, "lease_lost"
	}
	return next, ""
}

func getLeaseV2(ctx context.Context, provider hostresources.EndpointLeaseProviderV1, leaseID string) (hostresources.EndpointLeaseV1, string) {
	request := hostresources.GetEndpointLeaseRequestV1{LeaseID: leaseID}
	if request.Validate() != nil {
		return hostresources.EndpointLeaseV1{}, "lease_lost"
	}
	lease, err := safeLeaseCallV2(ctx, func(callCtx context.Context) (hostresources.EndpointLeaseV1, error) {
		return provider.GetEndpointLease(callCtx, request)
	})
	if err != nil || lease.Validate() != nil || lease.LeaseID != leaseID {
		return hostresources.EndpointLeaseV1{}, "lease_lost"
	}
	return lease, ""
}

func safeLeaseCallV2(ctx context.Context, call func(context.Context) (hostresources.EndpointLeaseV1, error)) (lease hostresources.EndpointLeaseV1, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	callCtx, cancel := context.WithTimeout(ctx, v2ProviderTimeout)
	defer cancel()
	result := make(chan endpointLeaseCallResultV2, 1)
	go func() {
		defer func() {
			if recover() != nil {
				result <- endpointLeaseCallResultV2{err: errors.New("lease_provider_unavailable")}
			}
		}()
		value, callErr := call(callCtx)
		result <- endpointLeaseCallResultV2{lease: value, err: callErr}
	}()
	select {
	case value := <-result:
		if value.err != nil || callCtx.Err() != nil {
			if errors.Is(value.err, hostresources.ErrEndpointLeaseConflictV1) {
				return hostresources.EndpointLeaseV1{}, value.err
			}
			return hostresources.EndpointLeaseV1{}, errors.New("lease_provider_unavailable")
		}
		return value.lease, nil
	case <-callCtx.Done():
		return hostresources.EndpointLeaseV1{}, errors.New("lease_provider_unavailable")
	}
}

var _ = fmt.Sprintf
