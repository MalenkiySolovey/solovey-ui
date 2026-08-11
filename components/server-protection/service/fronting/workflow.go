package fronting

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	componenthealth "github.com/MalenkiySolovey/solovey-ui/componenthost/health"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	protectionhelper "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/helper"
	protectionoperations "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
)

var (
	ErrWorkflowDisabled  = errors.New("fronting workflow is not initialized")
	ErrMissingCapability = errors.New("restricted nginx helper capability is unavailable")
	ErrDesiredRevision   = errors.New("fronting desired revision changed")
	ErrHelperRevision    = errors.New("restricted helper capability revision changed")
	ErrActiveRevision    = errors.New("active nginx revision changed")
	ErrValidationFailed  = errors.New("nginx candidate validation failed")
	ErrInstallFailed     = errors.New("nginx managed revision install failed")
	ErrSwitchFailed      = errors.New("nginx active revision switch failed")
	ErrReloadFailed      = errors.New("nginx reload failed")
	ErrActiveVerify      = errors.New("nginx active revision verification failed")
	ErrHealthFailed      = errors.New("fronting health failed")
	ErrRollbackReload    = errors.New("previous nginx revision reload failed")
	ErrRollbackHealth    = errors.New("previous fronting health failed")
	ErrAcknowledgement   = errors.New("fronting advanced acknowledgement is required")
)

const (
	frontingHealthTimeout    = 30 * time.Second
	checkpointSynced         = "synced"
	checkpointValidated      = "validated_previous_persisted"
	checkpointSwitchIntent   = "switch_intent"
	checkpointSwitched       = "switched"
	checkpointReloaded       = "reloaded"
	checkpointVerified       = "verified"
	checkpointHealthy        = "healthy"
	checkpointRollbackIntent = "rollback_intent"
	checkpointRestored       = "restored"
	checkpointRollbackReload = "rollback_reloaded"
	checkpointRollbackHealth = "rollback_healthy"
)

type Helper interface {
	Execute(context.Context, protectionhelper.Request) (protectionhelper.Response, error)
}

type ArtifactService interface {
	WriteRevision(context.Context, string, string, map[string][]byte) (protectionrepository.ArtifactModel, error)
}

type MutationMarker interface{ MarkMutation(string, string) error }

type StateStore interface {
	WriteFrontingState(string, []byte) error
	ReadFrontingState(string) ([]byte, error)
}

type RecoveryBundler interface {
	CreateBundle(context.Context, protectionrepository.OperationLockModel, string) error
}

type HealthCheck func(context.Context, []hostresources.ProtectableResource) []componenthealth.Result

type Workflow struct {
	Manager        *protectionoperations.Manager
	Helper         Helper
	Artifacts      ArtifactService
	Marker         MutationMarker
	State          StateStore
	Recovery       RecoveryBundler
	Health         HealthCheck
	RollbackHealth HealthCheck
	V2Plans        PlanSourceV2
	V2Leases       EndpointLeaseDirectoryV1
	V2Fallbacks    FallbackReservationDirectoryV2
	V2Artifacts    ArtifactVerifierV2
	V2Health       FixedL4HealthCheckV2
	V2SNIHealth    SNIPrereadHealthCheckV2
	Now            func() time.Time
}

type SyncInput struct {
	Preview         PreviewInput
	DesiredRevision string
	Actor           string
	IdempotencyKey  string
	Acknowledged    bool
	Confirmation    string
}

type ApplyInput struct {
	OperationID     string
	Preview         PreviewInput
	DesiredRevision string
	Acknowledged    bool
	Confirmation    string
}

type TimelineEvent struct {
	Checkpoint string `json:"checkpoint"`
	At         int64  `json:"at"`
}

type Checkpoint struct {
	Version           int                              `json:"version"`
	OperationID       string                           `json:"operationId"`
	DesiredRevision   string                           `json:"desiredRevision"`
	CandidateSHA256   string                           `json:"candidateSha256"`
	ArtifactRevision  string                           `json:"artifactRevision"`
	PreviousRevision  string                           `json:"previousRevision"`
	PreviousSHA256    string                           `json:"previousSha256"`
	PreviousListeners []protectionhelper.NginxListener `json:"previousListeners"`
	Binary            protectionhelper.BinaryIdentity  `json:"binary"`
	HelperRevision    string                           `json:"helperRevision"`
	ManagedRoot       string                           `json:"managedRoot"`
	ControlledConfig  string                           `json:"controlledConfig"`
	Listens           []ListenSpec                     `json:"listens"`
	ApprovedTargets   []string                         `json:"approvedTargets"`
	Warnings          []string                         `json:"warnings"`
	Checkpoint        string                           `json:"checkpoint"`
	Validation        string                           `json:"validation"`
	Switched          bool                             `json:"switched"`
	Reloaded          bool                             `json:"reloaded"`
	Verified          bool                             `json:"verified"`
	Restored          bool                             `json:"restored"`
	RollbackReloaded  bool                             `json:"rollbackReloaded"`
	Health            []componenthealth.Result         `json:"health,omitempty"`
	RollbackHealth    []componenthealth.Result         `json:"rollbackHealth,omitempty"`
	Timeline          []TimelineEvent                  `json:"timeline"`
}

type WorkflowResult struct {
	OperationID       string                          `json:"operationId"`
	State             string                          `json:"state"`
	Revision          int                             `json:"revision"`
	DesiredRevision   string                          `json:"desiredRevision"`
	ActiveRevision    string                          `json:"activeRevision,omitempty"`
	PreviousRevision  string                          `json:"previousRevision,omitempty"`
	CandidateSHA256   string                          `json:"candidateSha256,omitempty"`
	ArtifactRevision  string                          `json:"artifactRevision,omitempty"`
	Validation        string                          `json:"validation,omitempty"`
	Warnings          []string                        `json:"warnings"`
	Listens           []ListenSpec                    `json:"listens"`
	ApprovedTargets   []string                        `json:"approvedTargets"`
	RollbackPlan      string                          `json:"rollbackPlan"`
	RollbackAttempted bool                            `json:"rollbackAttempted"`
	RecoveryRequired  bool                            `json:"recoveryRequired"`
	Health            []componenthealth.Result        `json:"health,omitempty"`
	Timeline          []TimelineEvent                 `json:"timeline"`
	Binary            protectionhelper.BinaryIdentity `json:"-"`
}

func (w *Workflow) Capability(ctx context.Context) CapabilityReport {
	report := CapabilityReport{AdapterID: "nginx", AdapterKind: "stream", CapabilityRevision: capabilityRevision, State: StateUnknown,
		ManagedRoot: AvailabilityUnsupported, ControlledInclude: AvailabilityUnsupported, Validate: AvailabilityUnsupported, Activate: AvailabilityUnsupported, Reload: AvailabilityUnsupported,
		SNI: AvailabilitySupported, ALPN: AvailabilitySupported, ProxyProtocol: AvailabilitySupported, Warnings: []string{}, Diagnostics: []string{}}
	capabilities, err := w.capabilities(ctx)
	if err != nil || !frontingCapabilitiesAvailable(capabilities) {
		report.State, report.Reason = StateUnsupported, "restricted nginx helper capability is unavailable"
		report.Warnings = []string{"manual Sync/Apply is disabled until every typed nginx capability is verified"}
		return report
	}
	nginx := capabilities.Nginx
	report.Supported, report.State = true, StateSupported
	report.Binary = BinaryIdentity{Path: nginx.Binary.Path, TargetPath: nginx.Binary.TargetPath, Device: nginx.Binary.Device, Inode: nginx.Binary.Inode}
	report.Ownership = ConfigOwnership{ManagedRoot: nginx.ManagedRoot, ControlledInclude: nginx.ControlledConfig}
	report.ManagedRoot, report.ControlledInclude = AvailabilitySupported, AvailabilitySupported
	report.CurrentRevision, report.ActiveRevision, report.ActiveSHA256 = nginx.ActiveRevision, nginx.ActiveRevision, nginx.ActiveSHA256
	report.Validate, report.Activate, report.Reload = AvailabilitySupported, AvailabilitySupported, AvailabilitySupported
	report.Diagnostics = []string{"capability probed through the restricted helper", "exact binary and controlled managed root verified"}
	report.Warnings = []string{"manual Sync and Apply only; external system changes require separate operator authorization"}
	return report
}

func (w *Workflow) Sync(ctx context.Context, input SyncInput) (WorkflowResult, error) {
	if err := w.ready(); err != nil {
		return WorkflowResult{}, err
	}
	if !input.Acknowledged {
		return WorkflowResult{}, ErrAcknowledgement
	}
	if input.Confirmation != "SYNC FRONTING "+input.DesiredRevision {
		return WorkflowResult{}, protectionoperations.ErrConfirmationRequired
	}
	preview, err := GeneratePreview(input.Preview)
	if err != nil || preview.DesiredRevision != input.DesiredRevision {
		return WorkflowResult{}, errors.Join(ErrDesiredRevision, err)
	}
	capabilities, err := w.capabilities(ctx)
	if err != nil || !frontingCapabilitiesAvailable(capabilities) {
		return WorkflowResult{}, errors.Join(ErrMissingCapability, err)
	}
	initialNginx := capabilities.Nginx
	acquired, err := w.Manager.Acquire(ctx, protectionoperations.AcquireRequest{Kind: protectionoperations.KindFronting, ResourceID: "nginx:stream", Protocol: "tcp", IdempotencyKey: input.IdempotencyKey, PlanRevision: preview.DesiredRevision, HelperRevision: capabilities.Revision, Actor: input.Actor})
	if err != nil {
		return WorkflowResult{}, err
	}
	operation := acquired.Operation
	if acquired.Joined {
		checkpoint, loadErr := w.load(operation.OperationID)
		if loadErr != nil || checkpoint.DesiredRevision != preview.DesiredRevision {
			return WorkflowResult{}, errors.Join(ErrDesiredRevision, loadErr)
		}
		return workflowResult(operation, checkpoint, capabilities.Nginx.ActiveRevision), nil
	}
	cancelPrepared := func(cause error) (WorkflowResult, error) {
		_, _ = w.Manager.Transition(context.WithoutCancel(ctx), operation.OperationID, operation.Revision, protectionoperations.StateCancelled)
		return WorkflowResult{}, cause
	}
	// Recheck both deterministic input and helper ownership after the lock/fence.
	rechecked, err := GeneratePreview(input.Preview)
	if err != nil || rechecked.DesiredRevision != preview.DesiredRevision || rechecked.GeneratedSHA256 != preview.GeneratedSHA256 {
		return cancelPrepared(errors.Join(ErrDesiredRevision, err))
	}
	capabilities, err = w.capabilities(ctx)
	if err != nil || !frontingCapabilitiesAvailable(capabilities) {
		return cancelPrepared(errors.Join(ErrMissingCapability, err))
	}
	if capabilities.Nginx.ActiveRevision != initialNginx.ActiveRevision || capabilities.Nginx.ActiveSHA256 != initialNginx.ActiveSHA256 {
		return cancelPrepared(ErrActiveRevision)
	}
	if capabilities.Revision != operation.HelperRevision || capabilities.Nginx.Binary != initialNginx.Binary || capabilities.Nginx.ManagedRoot != initialNginx.ManagedRoot || capabilities.Nginx.ControlledConfig != initialNginx.ControlledConfig {
		return cancelPrepared(ErrHelperRevision)
	}
	artifact, err := w.Artifacts.WriteRevision(ctx, operation.OperationID, operation.OperationID, candidateFiles(preview, capabilities.Nginx))
	if err != nil {
		return cancelPrepared(err)
	}
	correlation := w.correlation(operation)
	validation, err := w.execute(ctx, protectionhelper.Request{ProtocolVersion: protectionhelper.ProtocolVersion, Correlation: correlation, Operation: protectionhelper.OperationNginxValidate, NginxValidate: &protectionhelper.NginxValidateRequest{CandidatePath: candidatePath(artifact.Revision), ExpectedRevision: preview.DesiredRevision, ExpectedSHA256: preview.GeneratedSHA256, ExpectedBinary: capabilities.Nginx.Binary}})
	if err != nil || validation.Nginx == nil || validation.Nginx.Revision != preview.DesiredRevision || validation.Nginx.SHA256 != preview.GeneratedSHA256 {
		return cancelPrepared(errors.Join(ErrValidationFailed, err))
	}
	checkpoint := Checkpoint{Version: 1, OperationID: operation.OperationID, DesiredRevision: preview.DesiredRevision, CandidateSHA256: preview.GeneratedSHA256, ArtifactRevision: artifact.Revision,
		PreviousRevision: capabilities.Nginx.ActiveRevision, PreviousSHA256: capabilities.Nginx.ActiveSHA256, PreviousListeners: append([]protectionhelper.NginxListener(nil), capabilities.Nginx.Listeners...), Binary: capabilities.Nginx.Binary, HelperRevision: capabilities.Revision, ManagedRoot: capabilities.Nginx.ManagedRoot, ControlledConfig: capabilities.Nginx.ControlledConfig,
		Listens: append([]ListenSpec(nil), preview.Listens...), ApprovedTargets: append([]string(nil), preview.ApprovedTargets...), Warnings: append([]string(nil), preview.Warnings...), Validation: "passed", Checkpoint: checkpointValidated}
	if !validManagedRevision(checkpoint.PreviousRevision, checkpoint.PreviousSHA256) || len(checkpoint.PreviousListeners) == 0 {
		return cancelPrepared(ErrActiveRevision)
	}
	// The exact rollback identity must be durable before the immutable managed
	// revision is installed. Recovery can then prove that no active switch was
	// completed even if the process stops immediately after installation.
	if err := w.save(&checkpoint, checkpointValidated); err != nil {
		return cancelPrepared(err)
	}
	installed, err := w.execute(ctx, protectionhelper.Request{ProtocolVersion: protectionhelper.ProtocolVersion, Correlation: correlation, Operation: protectionhelper.OperationNginxInstall, NginxInstall: &protectionhelper.NginxInstallRequest{CandidatePath: candidatePath(artifact.Revision), ExpectedRevision: preview.DesiredRevision, ExpectedSHA256: preview.GeneratedSHA256, Listeners: helperListeners(preview.Listens)}})
	if err != nil || installed.Nginx == nil || installed.Nginx.Revision != preview.DesiredRevision || installed.Nginx.SHA256 != preview.GeneratedSHA256 {
		return cancelPrepared(errors.Join(ErrInstallFailed, err))
	}
	if err := w.save(&checkpoint, checkpointSynced); err != nil {
		return cancelPrepared(err)
	}
	return workflowResult(operation, checkpoint, capabilities.Nginx.ActiveRevision), nil
}

func (w *Workflow) Apply(ctx context.Context, input ApplyInput) (WorkflowResult, error) {
	if err := w.ready(); err != nil {
		return WorkflowResult{}, err
	}
	if !input.Acknowledged {
		return WorkflowResult{}, ErrAcknowledgement
	}
	if input.Confirmation != "APPLY FRONTING "+input.OperationID {
		return WorkflowResult{}, protectionoperations.ErrConfirmationRequired
	}
	operation, err := w.operation(ctx, input.OperationID)
	if err != nil {
		return WorkflowResult{}, err
	}
	checkpoint, err := w.load(input.OperationID)
	if err != nil {
		return WorkflowResult{}, err
	}
	if operation.Kind != protectionoperations.KindFronting || operation.PlanRevision != input.DesiredRevision || checkpoint.DesiredRevision != input.DesiredRevision {
		return WorkflowResult{}, ErrDesiredRevision
	}
	if operation.State == protectionoperations.StateApplied {
		return workflowResult(operation, checkpoint, checkpoint.DesiredRevision), nil
	}
	if operation.State != protectionoperations.StatePrepared {
		return WorkflowResult{}, fmt.Errorf("fronting operation is not prepared")
	}
	preview, err := GeneratePreview(input.Preview)
	if err != nil || preview.DesiredRevision != checkpoint.DesiredRevision || preview.GeneratedSHA256 != checkpoint.CandidateSHA256 {
		return WorkflowResult{}, errors.Join(ErrDesiredRevision, err)
	}
	capabilities, err := w.capabilities(ctx)
	if err != nil || !frontingCapabilitiesAvailable(capabilities) {
		return WorkflowResult{}, errors.Join(ErrMissingCapability, err)
	}
	if capabilities.Nginx.ActiveRevision != checkpoint.PreviousRevision || capabilities.Nginx.ActiveSHA256 != checkpoint.PreviousSHA256 {
		return WorkflowResult{}, ErrActiveRevision
	}
	if capabilities.Revision != checkpoint.HelperRevision || capabilities.Nginx.Binary != checkpoint.Binary || capabilities.Nginx.ManagedRoot != checkpoint.ManagedRoot || capabilities.Nginx.ControlledConfig != checkpoint.ControlledConfig {
		return WorkflowResult{}, ErrHelperRevision
	}
	applying, err := w.Manager.Transition(ctx, operation.OperationID, operation.Revision, protectionoperations.StateApplying)
	if err != nil {
		return WorkflowResult{}, err
	}
	if err := w.Marker.MarkMutation(applying.OperationID, checkpoint.ArtifactRevision); err != nil {
		return w.cancelBeforeSwitch(ctx, applying, checkpoint, err)
	}
	if err := w.save(&checkpoint, checkpointSwitchIntent); err != nil {
		return w.cancelBeforeSwitch(ctx, applying, checkpoint, err)
	}
	response, err := w.execute(ctx, protectionhelper.Request{ProtocolVersion: protectionhelper.ProtocolVersion, Correlation: w.correlation(applying), Operation: protectionhelper.OperationNginxSwitch, NginxSwitch: &protectionhelper.NginxSwitchRequest{ExpectedPreviousRevision: checkpoint.PreviousRevision, TargetRevision: checkpoint.DesiredRevision, ExpectedSHA256: checkpoint.CandidateSHA256}})
	if err != nil || response.Nginx == nil || response.Nginx.Revision != checkpoint.DesiredRevision || response.Nginx.PreviousRevision != checkpoint.PreviousRevision {
		cause := errors.Join(ErrSwitchFailed, err)
		actual, actualErr := w.capabilities(context.WithoutCancel(ctx))
		if actualErr == nil && frontingCapabilitiesAvailable(actual) && actual.Nginx.Binary == checkpoint.Binary {
			if actual.Nginx.ActiveRevision == checkpoint.PreviousRevision && actual.Nginx.ActiveSHA256 == checkpoint.PreviousSHA256 {
				return w.cancelBeforeSwitch(ctx, applying, checkpoint, cause)
			}
			if actual.Nginx.ActiveRevision == checkpoint.DesiredRevision && actual.Nginx.ActiveSHA256 == checkpoint.CandidateSHA256 {
				checkpoint.Switched = true
				if saveErr := w.save(&checkpoint, checkpointSwitched); saveErr != nil {
					cause = errors.Join(cause, saveErr)
				}
				return w.failAndRollback(ctx, applying, checkpoint, cause)
			}
		}
		// Once switch intent is durable, an unprovable result is mutation-side
		// uncertainty. Never release the owner as a pre-switch cancellation.
		checkpoint.Switched = true
		if saveErr := w.save(&checkpoint, checkpointSwitched); saveErr != nil {
			cause = errors.Join(cause, saveErr)
		}
		return w.failAndRollback(ctx, applying, checkpoint, errors.Join(cause, actualErr))
	}
	checkpoint.Switched = true
	if err := w.save(&checkpoint, checkpointSwitched); err != nil {
		return w.failAndRollback(ctx, applying, checkpoint, err)
	}
	response, err = w.execute(ctx, protectionhelper.Request{ProtocolVersion: protectionhelper.ProtocolVersion, Correlation: w.correlation(applying), Operation: protectionhelper.OperationNginxReload, NginxReload: &protectionhelper.NginxReloadRequest{ExpectedRevision: checkpoint.DesiredRevision, ExpectedSHA256: checkpoint.CandidateSHA256, ExpectedBinary: checkpoint.Binary}})
	if err != nil || response.Nginx == nil || response.Nginx.MasterPID <= 0 || len(response.Nginx.WorkerPIDs) == 0 {
		return w.failAndRollback(ctx, applying, checkpoint, errors.Join(ErrReloadFailed, err))
	}
	checkpoint.Reloaded = true
	if err := w.save(&checkpoint, checkpointReloaded); err != nil {
		return w.failAndRollback(ctx, applying, checkpoint, err)
	}
	response, err = w.execute(ctx, protectionhelper.Request{ProtocolVersion: protectionhelper.ProtocolVersion, Correlation: w.correlation(applying), Operation: protectionhelper.OperationNginxVerify, NginxVerify: &protectionhelper.NginxVerifyRequest{ExpectedRevision: checkpoint.DesiredRevision, ExpectedSHA256: checkpoint.CandidateSHA256, ExpectedBinary: checkpoint.Binary, Listeners: helperListeners(checkpoint.Listens)}})
	if err != nil || response.Nginx == nil || !response.Nginx.ListenersMatched || response.Nginx.Revision != checkpoint.DesiredRevision || response.Nginx.SHA256 != checkpoint.CandidateSHA256 || response.Nginx.Binary != checkpoint.Binary || response.Nginx.MasterPID <= 0 || len(response.Nginx.WorkerPIDs) == 0 {
		return w.failAndRollback(ctx, applying, checkpoint, errors.Join(ErrActiveVerify, err))
	}
	checkpoint.Verified = true
	if err := w.save(&checkpoint, checkpointVerified); err != nil {
		return w.failAndRollback(ctx, applying, checkpoint, err)
	}
	healthing, err := w.Manager.Transition(ctx, applying.OperationID, applying.Revision, protectionoperations.StateHealth)
	if err != nil {
		return w.failAndRollback(ctx, applying, checkpoint, err)
	}
	affected := affectedResources(input.Preview)
	checkpoint.Health = boundedHealth(ctx, w.Health, affected, "fronting_health_timeout")
	if healthFailedFor(affected, checkpoint.Health) {
		return w.failAndRollback(ctx, healthing, checkpoint, ErrHealthFailed)
	}
	if err := w.save(&checkpoint, checkpointHealthy); err != nil {
		return w.failAndRollback(ctx, healthing, checkpoint, err)
	}
	applied, err := w.Manager.Transition(ctx, healthing.OperationID, healthing.Revision, protectionoperations.StateApplied)
	if err != nil {
		return workflowResult(healthing, checkpoint, checkpoint.DesiredRevision), err
	}
	return workflowResult(applied, checkpoint, checkpoint.DesiredRevision), nil
}

func (w *Workflow) Rollback(ctx context.Context, operationID, confirmation string) (WorkflowResult, error) {
	if confirmation != "ROLLBACK FRONTING "+operationID {
		return WorkflowResult{}, protectionoperations.ErrConfirmationRequired
	}
	operation, err := w.operation(ctx, operationID)
	if err != nil {
		return WorkflowResult{}, err
	}
	checkpoint, err := w.load(operationID)
	if err != nil {
		return WorkflowResult{}, err
	}
	return w.rollback(ctx, operation, checkpoint, true)
}

func (w *Workflow) Operation(ctx context.Context, operationID string) (WorkflowResult, error) {
	operation, err := w.operation(ctx, operationID)
	if err != nil {
		return WorkflowResult{}, err
	}
	checkpoint, err := w.load(operationID)
	if err != nil {
		return WorkflowResult{}, err
	}
	active := ""
	if capabilities, capabilityErr := w.capabilities(ctx); capabilityErr == nil && capabilities != nil {
		active = capabilities.Nginx.ActiveRevision
	}
	return workflowResult(operation, checkpoint, active), nil
}

func (a *NginxAdapter) Sync(ctx context.Context, input SyncInput) (WorkflowResult, error) {
	if a.Workflow == nil {
		return WorkflowResult{}, ErrWorkflowDisabled
	}
	return a.Workflow.Sync(ctx, input)
}
func (a *NginxAdapter) Apply(ctx context.Context, input ApplyInput) (WorkflowResult, error) {
	if a.Workflow == nil {
		return WorkflowResult{}, ErrWorkflowDisabled
	}
	return a.Workflow.Apply(ctx, input)
}
func (a *NginxAdapter) Rollback(ctx context.Context, operationID, confirmation string) (WorkflowResult, error) {
	if a.Workflow == nil {
		return WorkflowResult{}, ErrWorkflowDisabled
	}
	return a.Workflow.Rollback(ctx, operationID, confirmation)
}
func (a *NginxAdapter) Operation(ctx context.Context, operationID string) (WorkflowResult, error) {
	if a.Workflow == nil {
		return WorkflowResult{}, ErrWorkflowDisabled
	}
	return a.Workflow.Operation(ctx, operationID)
}

func (w *Workflow) cancelBeforeSwitch(ctx context.Context, operation protectionrepository.OperationLockModel, checkpoint Checkpoint, cause error) (WorkflowResult, error) {
	cancelled, transitionErr := w.Manager.Transition(context.WithoutCancel(ctx), operation.OperationID, operation.Revision, protectionoperations.StateCancelled)
	return workflowResult(cancelled, checkpoint, checkpoint.PreviousRevision), errors.Join(cause, transitionErr)
}

func (w *Workflow) failAndRollback(ctx context.Context, operation protectionrepository.OperationLockModel, checkpoint Checkpoint, cause error) (WorkflowResult, error) {
	recoveryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
	defer cancel()
	if operation.State == protectionoperations.StateApplying || operation.State == protectionoperations.StateHealth {
		failed, err := w.Manager.Transition(recoveryCtx, operation.OperationID, operation.Revision, protectionoperations.StateHealthFailed)
		if err != nil {
			return workflowResult(operation, checkpoint, checkpoint.DesiredRevision), errors.Join(cause, err)
		}
		operation = failed
	}
	result, rollbackErr := w.rollback(recoveryCtx, operation, checkpoint, true)
	return result, errors.Join(cause, rollbackErr)
}

func (w *Workflow) rollback(ctx context.Context, operation protectionrepository.OperationLockModel, checkpoint Checkpoint, automatic bool) (WorkflowResult, error) {
	if operation.State == protectionoperations.StateRolledBack {
		result := workflowResult(operation, checkpoint, checkpoint.PreviousRevision)
		result.RollbackAttempted = automatic
		return result, nil
	}
	if !checkpoint.Switched {
		if operation.State == protectionoperations.StatePrepared || operation.State == protectionoperations.StateApplying {
			cancelled, err := w.Manager.Transition(ctx, operation.OperationID, operation.Revision, protectionoperations.StateCancelled)
			return workflowResult(cancelled, checkpoint, checkpoint.PreviousRevision), err
		}
		return WorkflowResult{}, errors.New("fronting operation has no mutation to roll back")
	}
	var rolling protectionrepository.OperationLockModel
	var err error
	if operation.State == protectionoperations.StateApplied {
		rolling, err = w.Manager.BeginRollback(ctx, operation.OperationID, operation.Revision)
	} else if operation.State == protectionoperations.StateApplying || operation.State == protectionoperations.StateHealth || operation.State == protectionoperations.StateHealthFailed {
		rolling, err = w.Manager.Transition(ctx, operation.OperationID, operation.Revision, protectionoperations.StateRollingBack)
	} else if operation.State == protectionoperations.StateRollingBack {
		rolling = operation
	} else {
		return WorkflowResult{}, fmt.Errorf("fronting operation cannot roll back from %s", operation.State)
	}
	if err != nil {
		return WorkflowResult{}, err
	}
	if err := w.save(&checkpoint, checkpointRollbackIntent); err != nil {
		return w.rollbackFailed(ctx, rolling, checkpoint, err)
	}
	capabilities, capErr := w.capabilities(ctx)
	if capErr != nil || !frontingCapabilitiesAvailable(capabilities) {
		return w.rollbackFailed(ctx, rolling, checkpoint, errors.Join(ErrMissingCapability, capErr))
	}
	if capabilities.Nginx.ActiveRevision == checkpoint.PreviousRevision && capabilities.Nginx.ActiveSHA256 == checkpoint.PreviousSHA256 {
		checkpoint.Restored = true
	}
	if !checkpoint.Restored {
		response, restoreErr := w.execute(ctx, protectionhelper.Request{ProtocolVersion: protectionhelper.ProtocolVersion, Correlation: w.correlation(rolling), Operation: protectionhelper.OperationNginxRestore, NginxRestore: &protectionhelper.NginxRestoreRequest{ExpectedCurrentRevision: checkpoint.DesiredRevision, PreviousRevision: checkpoint.PreviousRevision, ExpectedSHA256: checkpoint.PreviousSHA256}})
		if restoreErr != nil || response.Nginx == nil || response.Nginx.Revision != checkpoint.PreviousRevision {
			return w.rollbackFailed(ctx, rolling, checkpoint, errors.Join(ErrActiveRevision, restoreErr))
		}
		checkpoint.Restored = true
		if err := w.save(&checkpoint, checkpointRestored); err != nil {
			return w.rollbackFailed(ctx, rolling, checkpoint, err)
		}
	}
	if !checkpoint.RollbackReloaded {
		response, reloadErr := w.execute(ctx, protectionhelper.Request{ProtocolVersion: protectionhelper.ProtocolVersion, Correlation: w.correlation(rolling), Operation: protectionhelper.OperationNginxReload, NginxReload: &protectionhelper.NginxReloadRequest{ExpectedRevision: checkpoint.PreviousRevision, ExpectedSHA256: checkpoint.PreviousSHA256, ExpectedBinary: checkpoint.Binary}})
		if reloadErr != nil || response.Nginx == nil || response.Nginx.MasterPID <= 0 {
			return w.rollbackFailed(ctx, rolling, checkpoint, errors.Join(ErrRollbackReload, reloadErr))
		}
		checkpoint.RollbackReloaded = true
		if err := w.save(&checkpoint, checkpointRollbackReload); err != nil {
			return w.rollbackFailed(ctx, rolling, checkpoint, err)
		}
	}
	response, verifyErr := w.execute(ctx, protectionhelper.Request{ProtocolVersion: protectionhelper.ProtocolVersion, Correlation: w.correlation(rolling), Operation: protectionhelper.OperationNginxVerify, NginxVerify: &protectionhelper.NginxVerifyRequest{ExpectedRevision: checkpoint.PreviousRevision, ExpectedSHA256: checkpoint.PreviousSHA256, ExpectedBinary: checkpoint.Binary, Listeners: checkpoint.PreviousListeners}})
	if verifyErr != nil || response.Nginx == nil || !response.Nginx.ListenersMatched || response.Nginx.Revision != checkpoint.PreviousRevision || response.Nginx.SHA256 != checkpoint.PreviousSHA256 || response.Nginx.Binary != checkpoint.Binary || response.Nginx.MasterPID <= 0 || len(response.Nginx.WorkerPIDs) == 0 {
		return w.rollbackFailed(ctx, rolling, checkpoint, errors.Join(ErrActiveVerify, verifyErr))
	}
	checkpoint.RollbackHealth = boundedHealth(ctx, w.RollbackHealth, nil, "fronting_rollback_health_timeout")
	if healthFailed(checkpoint.RollbackHealth) {
		return w.rollbackFailed(ctx, rolling, checkpoint, ErrRollbackHealth)
	}
	if err := w.save(&checkpoint, checkpointRollbackHealth); err != nil {
		return w.rollbackFailed(ctx, rolling, checkpoint, err)
	}
	rolled, err := w.Manager.Transition(ctx, rolling.OperationID, rolling.Revision, protectionoperations.StateRolledBack)
	result := workflowResult(rolled, checkpoint, checkpoint.PreviousRevision)
	result.RollbackAttempted = true
	return result, err
}

func (w *Workflow) rollbackFailed(ctx context.Context, rolling protectionrepository.OperationLockModel, checkpoint Checkpoint, cause error) (WorkflowResult, error) {
	if err := w.Recovery.CreateBundle(ctx, rolling, protectionoperations.StateRollbackFailed); err != nil {
		return workflowResult(rolling, checkpoint, checkpoint.PreviousRevision), errors.Join(cause, err)
	}
	failed, err := w.Manager.Transition(ctx, rolling.OperationID, rolling.Revision, protectionoperations.StateRollbackFailed)
	result := workflowResult(failed, checkpoint, checkpoint.PreviousRevision)
	result.RollbackAttempted = true
	result.RecoveryRequired = true
	return result, errors.Join(cause, err)
}

func (w *Workflow) capabilities(ctx context.Context) (*protectionhelper.CapabilitiesResult, error) {
	if w == nil || w.Manager == nil || w.Helper == nil {
		return nil, ErrWorkflowDisabled
	}
	response, err := w.Helper.Execute(ctx, protectionhelper.Request{ProtocolVersion: protectionhelper.ProtocolVersion, Correlation: protectionhelper.Correlation{OperationID: "capabilities", InstanceID: w.Manager.InstanceID()}, Operation: protectionhelper.OperationCapabilities, Capabilities: &protectionhelper.CapabilitiesRequest{}})
	if err != nil {
		return nil, err
	}
	if !response.OK || response.Capabilities == nil {
		return response.Capabilities, ErrMissingCapability
	}
	return response.Capabilities, nil
}

func frontingCapabilitiesAvailable(capabilities *protectionhelper.CapabilitiesResult) bool {
	if capabilities == nil || capabilities.Revision == "" || !capabilities.Nginx.PlatformKnown || !capabilities.Nginx.Linux || !capabilities.Nginx.Available || !validManagedRevision(capabilities.Nginx.ActiveRevision, capabilities.Nginx.ActiveSHA256) || len(capabilities.Nginx.Listeners) == 0 {
		return false
	}
	for _, operation := range []protectionhelper.Operation{protectionhelper.OperationNginxDetectVersion, protectionhelper.OperationNginxValidate, protectionhelper.OperationNginxInstall, protectionhelper.OperationNginxSwitch, protectionhelper.OperationNginxReload, protectionhelper.OperationNginxVerify, protectionhelper.OperationNginxRestore} {
		if !protectionhelper.CapabilityAvailable(capabilities, operation) {
			return false
		}
	}
	return true
}

func (w *Workflow) execute(ctx context.Context, request protectionhelper.Request) (protectionhelper.Response, error) {
	response, err := w.Helper.Execute(ctx, request)
	if err != nil {
		return response, err
	}
	if !response.OK {
		if response.Code == protectionhelper.CodeMissingCapability {
			return response, fmt.Errorf("%w: %s", ErrMissingCapability, response.Reason)
		}
		return response, fmt.Errorf("helper %s failed: %s", request.Operation, response.Code)
	}
	return response, nil
}
func (w *Workflow) correlation(operation protectionrepository.OperationLockModel) protectionhelper.Correlation {
	return protectionhelper.Correlation{OperationID: operation.OperationID, InstanceID: w.Manager.InstanceID(), LockRevision: operation.Revision}
}
func (w *Workflow) operation(ctx context.Context, id string) (protectionrepository.OperationLockModel, error) {
	items, err := w.Manager.List(ctx)
	if err != nil {
		return protectionrepository.OperationLockModel{}, err
	}
	for _, item := range items {
		if item.OperationID == id {
			return item, nil
		}
	}
	return protectionrepository.OperationLockModel{}, protectionrepository.ErrRecordNotFound
}
func (w *Workflow) ready() error {
	if w == nil || w.Manager == nil || w.Helper == nil || w.Artifacts == nil || w.Marker == nil || w.State == nil || w.Recovery == nil || w.Health == nil || w.RollbackHealth == nil {
		return ErrWorkflowDisabled
	}
	if w.Now == nil {
		w.Now = time.Now
	}
	return nil
}
func (w *Workflow) save(checkpoint *Checkpoint, phase string) error {
	checkpoint.Checkpoint = phase
	checkpoint.Timeline = append(checkpoint.Timeline, TimelineEvent{Checkpoint: phase, At: w.Now().Unix()})
	if len(checkpoint.Timeline) > 64 {
		checkpoint.Timeline = checkpoint.Timeline[len(checkpoint.Timeline)-64:]
	}
	data, err := json.Marshal(checkpoint)
	if err != nil {
		return err
	}
	return w.State.WriteFrontingState(checkpoint.OperationID, append(data, '\n'))
}
func (w *Workflow) load(operationID string) (Checkpoint, error) {
	data, err := w.State.ReadFrontingState(operationID)
	if err != nil {
		return Checkpoint{}, err
	}
	var checkpoint Checkpoint
	if err := json.Unmarshal(data, &checkpoint); err != nil {
		return Checkpoint{}, err
	}
	if checkpoint.Version != 1 || checkpoint.OperationID != operationID || !validManagedRevision(checkpoint.DesiredRevision, checkpoint.CandidateSHA256) || !validManagedRevision(checkpoint.PreviousRevision, checkpoint.PreviousSHA256) || checkpoint.ManagedRoot == "" || checkpoint.ControlledConfig == "" {
		return Checkpoint{}, errors.New("fronting checkpoint identity is invalid")
	}
	return checkpoint, nil
}

func candidatePath(revision string) string { return "revisions/" + revision + "/candidate.conf" }
func candidateFiles(preview Preview, nginx protectionhelper.NginxSupport) map[string][]byte {
	rollback, _ := json.Marshal(struct {
		Revision  string                           `json:"revision"`
		SHA256    string                           `json:"sha256"`
		Listeners []protectionhelper.NginxListener `json:"listeners"`
	}{nginx.ActiveRevision, nginx.ActiveSHA256, nginx.Listeners})
	return map[string][]byte{"candidate.conf": []byte(preview.GeneratedConfig), "candidate.sha256": []byte(preview.GeneratedSHA256 + "\n"), "canonical.json": []byte(preview.CanonicalInput + "\n"), "rollback.json": append(rollback, '\n')}
}
func helperListeners(listens []ListenSpec) []protectionhelper.NginxListener {
	result := make([]protectionhelper.NginxListener, 0, len(listens))
	for _, listen := range listens {
		address := listen.Address
		if address == "*" {
			address = "0.0.0.0"
		}
		result = append(result, protectionhelper.NginxListener{Address: address, Port: listen.Port})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Port != result[j].Port {
			return result[i].Port < result[j].Port
		}
		return result[i].Address < result[j].Address
	})
	return result
}
func validManagedRevision(revision, sha string) bool {
	if len(revision) < 16 || len(revision) > 128 || len(sha) != 64 {
		return false
	}
	for _, value := range revision {
		if value < '0' || value > '9' && value < 'a' || value > 'f' {
			return false
		}
	}
	for _, value := range sha {
		if value < '0' || value > '9' && value < 'a' || value > 'f' {
			return false
		}
	}
	return true
}
func workflowResult(operation protectionrepository.OperationLockModel, checkpoint Checkpoint, active string) WorkflowResult {
	return WorkflowResult{OperationID: operation.OperationID, State: operation.State, Revision: operation.Revision, DesiredRevision: checkpoint.DesiredRevision, ActiveRevision: active, PreviousRevision: checkpoint.PreviousRevision, CandidateSHA256: checkpoint.CandidateSHA256, ArtifactRevision: checkpoint.ArtifactRevision, Validation: checkpoint.Validation, Warnings: append([]string(nil), checkpoint.Warnings...), Listens: append([]ListenSpec(nil), checkpoint.Listens...), ApprovedTargets: append([]string(nil), checkpoint.ApprovedTargets...), RollbackPlan: "restore_exact_previous_revision_reload_verify_health", RecoveryRequired: operation.State == protectionoperations.StateRollbackFailed, Health: append([]componenthealth.Result(nil), checkpoint.Health...), Timeline: append([]TimelineEvent(nil), checkpoint.Timeline...), Binary: checkpoint.Binary}
}
func healthFailed(results []componenthealth.Result) bool {
	if len(results) == 0 {
		return true
	}
	for _, result := range results {
		if result.Status != componenthealth.StatusOK {
			return true
		}
	}
	return false
}
func healthFailedFor(resources []hostresources.ProtectableResource, results []componenthealth.Result) bool {
	if len(resources) == 0 || healthFailed(results) {
		return true
	}
	checked := map[string]bool{}
	for _, result := range results {
		if result.Status == componenthealth.StatusOK {
			checked[result.ResourceID] = true
		}
	}
	for _, resource := range resources {
		if !checked[resource.ID] {
			return true
		}
	}
	return false
}

func affectedResources(input PreviewInput) []hostresources.ProtectableResource {
	wanted := make(map[string]bool, len(input.Routes)+1)
	for _, route := range input.Routes {
		wanted[route.ResourceID] = true
	}
	if input.FallbackResourceID != "" {
		wanted[input.FallbackResourceID] = true
	}
	result := make([]hostresources.ProtectableResource, 0, len(wanted))
	for _, resource := range input.Resources {
		if wanted[resource.ID] {
			result = append(result, resource)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func boundedHealth(ctx context.Context, check HealthCheck, resources []hostresources.ProtectableResource, timeoutFact string) []componenthealth.Result {
	if ctx == nil {
		ctx = context.Background()
	}
	healthCtx, cancel := context.WithTimeout(ctx, frontingHealthTimeout)
	defer cancel()
	result := make(chan []componenthealth.Result, 1)
	go func() {
		defer func() {
			if recover() != nil {
				result <- degradedHealth(resources, "fronting_health_panic")
			}
		}()
		result <- check(healthCtx, resources)
	}()
	select {
	case values := <-result:
		return values
	case <-healthCtx.Done():
		return degradedHealth(resources, timeoutFact)
	}
}

func degradedHealth(resources []hostresources.ProtectableResource, fact string) []componenthealth.Result {
	if len(resources) == 0 {
		return []componenthealth.Result{{ResourceID: "previous:fronting", Status: componenthealth.StatusDegraded, Check: "fronting_health", FactCode: fact}}
	}
	values := make([]componenthealth.Result, 0, len(resources))
	for _, resource := range resources {
		values = append(values, componenthealth.Result{ResourceID: resource.ID, Status: componenthealth.StatusDegraded, Check: "fronting_health", FactCode: fact})
	}
	return values
}
