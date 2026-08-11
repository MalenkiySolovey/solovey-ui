package firewall

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	componenthealth "github.com/MalenkiySolovey/solovey-ui/componenthost/health"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
	protectionhelper "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/helper"
	protectionoperations "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
)

var (
	ErrUnknownSSH        = errors.New("SSH listener is unknown; explicit TCP port 22 allowlist entry is required")
	ErrPlanRevision      = errors.New("firewall plan revision changed")
	ErrHelperRevision    = errors.New("restricted helper capability revision changed")
	ErrHealthFailed      = errors.New("post-apply health checks failed")
	ErrRollbackHealth    = errors.New("post-rollback health checks failed")
	ErrApplyVerify       = errors.New("managed table revision verification failed")
	ErrMissingCapability = errors.New("restricted nft helper capability is unavailable")
	ErrWorkflowDisabled  = errors.New("firewall workflow is not initialized")
	ErrUnsafeResource    = errors.New("protectable resource cannot be preserved safely")
)

// Helper is intentionally the narrow restricted-helper client boundary. The
// workflow receives only the typed client and has no command runner.
type Helper interface {
	Execute(context.Context, protectionhelper.Request) (protectionhelper.Response, error)
}

type ArtifactService interface {
	WriteRevision(context.Context, string, string, map[string][]byte) (protectionrepository.ArtifactModel, error)
}

type MutationMarker interface {
	MarkMutation(string, string) error
}

type StateStore interface {
	WriteFirewallState(string, []byte) error
	ReadFirewallState(string) ([]byte, error)
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
	Contributions  FirewallContributionStore
}

type PrepareInput struct {
	Plan           FirewallPlan
	Actor          string
	IdempotencyKey string
	Confirmation   string
}

type ApplyInput struct {
	OperationID     string
	Plan            FirewallPlan
	Resources       []hostresources.ProtectableResource
	Confirmation    string
	PostApplyHealth func(context.Context, PostMutationHealthFence) (PostMutationHealthProof, error)
}

type PostMutationHealthFence struct {
	OperationID          string
	ContributionID       string
	ContributionRevision string
	CompositionRevision  string
	ManagedPlanRevision  string
	MarkerUnixNano       int64
	MutationUnixNano     int64
}

type PostMutationHealthProof struct {
	ProviderInstance    string
	Generation          uint64
	ObservationRevision string
	StartedUnixNano     int64
	CompletedUnixNano   int64
	ExpiresUnixNano     int64
}

type Result struct {
	OperationID              string                   `json:"operationId"`
	State                    string                   `json:"state"`
	Revision                 int                      `json:"revision"`
	ArtifactRevision         string                   `json:"artifactRevision,omitempty"`
	Health                   []componenthealth.Result `json:"health,omitempty"`
	RollbackAttempted        bool                     `json:"rollbackAttempted"`
	CandidateSHA256          string                   `json:"candidateSha256,omitempty"`
	RollbackSHA256           string                   `json:"rollbackSha256,omitempty"`
	PlanRevision             string                   `json:"planRevision,omitempty"`
	GraphRevision            string                   `json:"graphRevision,omitempty"`
	OwnerObservationRevision string                   `json:"ownerObservationRevision,omitempty"`
	DesiredStatus            string                   `json:"desiredStatus,omitempty"`
	SelectedStatus           string                   `json:"selectedStatus,omitempty"`
	ActualStatus             string                   `json:"actualStatus,omitempty"`
	ReasonCodes              []string                 `json:"reasonCodes,omitempty"`
}

type FirewallCheckpoint struct {
	Version                  int    `json:"version"`
	OperationID              string `json:"operationId"`
	ArtifactRevision         string `json:"artifactRevision"`
	PlanRevision             string `json:"planRevision"`
	GraphRevision            string `json:"graphRevision,omitempty"`
	OwnerObservationRevision string `json:"ownerObservationRevision,omitempty"`
	CandidateSHA256          string `json:"candidateSha256"`
	RollbackSHA256           string `json:"rollbackSha256"`
	PreviousRevision         string `json:"previousRevision,omitempty"`
	PreviousTablePresent     bool   `json:"previousTablePresent"`
	ContributionID           string `json:"contributionId,omitempty"`
	ContributionRevision     string `json:"contributionRevision,omitempty"`
	CompositionRevision      string `json:"compositionRevision,omitempty"`
}

func (w Workflow) Capabilities(ctx context.Context) (*protectionhelper.CapabilitiesResult, error) {
	if err := w.ready(); err != nil {
		return nil, err
	}
	request := protectionhelper.Request{ProtocolVersion: protectionhelper.ProtocolVersion,
		Correlation: protectionhelper.Correlation{OperationID: "capabilities", InstanceID: w.Manager.InstanceID()},
		Operation:   protectionhelper.OperationCapabilities, Capabilities: &protectionhelper.CapabilitiesRequest{}}
	response, err := w.Helper.Execute(ctx, request)
	if err != nil {
		return nil, err
	}
	if !response.OK || response.Capabilities == nil {
		return response.Capabilities, ErrMissingCapability
	}
	return response.Capabilities, nil
}

func (w Workflow) Prepare(ctx context.Context, input PrepareInput) (protectionoperations.AcquireResult, error) {
	if err := w.ready(); err != nil {
		return protectionoperations.AcquireResult{}, err
	}
	if err := Preflight(input.Plan); err != nil {
		return protectionoperations.AcquireResult{}, err
	}
	if input.Confirmation != "PREPARE SERVER PROTECTION "+input.Plan.Revision {
		return protectionoperations.AcquireResult{}, protectionoperations.ErrConfirmationRequired
	}
	contribution, err := contributionFromPlan(input.Plan)
	if err != nil {
		return protectionoperations.AcquireResult{}, err
	}
	snapshot, err := w.Contributions.FirewallAuthority(ctx)
	if err != nil {
		return protectionoperations.AcquireResult{}, err
	}
	current, err := contributionsFromSnapshot(snapshot)
	if err != nil {
		return protectionoperations.AcquireResult{}, err
	}
	var previous *ManagedFirewallContributionV1
	for index := range current {
		if current[index].ContributionID == contribution.ContributionID {
			value := current[index]
			previous = &value
			break
		}
	}
	composition, err := composeFirewall(replaceContribution(current, &contribution))
	if err != nil {
		return protectionoperations.AcquireResult{}, err
	}
	capabilities, err := w.Capabilities(ctx)
	if err != nil || !firewallCapabilitiesAvailable(capabilities, composition.Plan) {
		return protectionoperations.AcquireResult{}, errors.Join(ErrMissingCapability, err)
	}
	result, err := w.Manager.Prepare(ctx, protectionoperations.PrepareRequest{
		PlanRevision: composition.PlanRevision, Confirmation: "PREPARE SERVER PROTECTION " + composition.PlanRevision,
		Acquire: protectionoperations.AcquireRequest{
			Kind: protectionoperations.KindFirewall, ResourceID: "managed-table:inet:solovey_protection",
			Protocol: "inet", IdempotencyKey: input.IdempotencyKey, Actor: input.Actor,
			HelperRevision: capabilities.Revision,
		},
	})
	if err != nil {
		return result, err
	}
	desiredJSON := contributionJSON(contribution)
	previousJSON := json.RawMessage(`{}`)
	previousRevision := ""
	if previous != nil {
		previousJSON, previousRevision = contributionJSON(*previous), previous.SemanticRevision
	}
	beforeRevision := ""
	if snapshot.HasComposition {
		beforeRevision = snapshot.Composition.Revision
	}
	transition := protectionrepository.FirewallContributionTransitionModel{OperationID: result.Operation.OperationID,
		Schema: FirewallTransitionSchemaV1, ContributionID: contribution.ContributionID, PreviousPresent: previous != nil,
		PreviousSemanticRevision: previousRevision, PreviousJSON: previousJSON, DesiredSemanticRevision: contribution.SemanticRevision,
		DesiredJSON: desiredJSON, BeforeCompositionRevision: beforeRevision, AfterCompositionRevision: composition.Revision,
		ManagedPlanRevision: composition.PlanRevision, CandidateSHA256: composition.CandidateSHA, State: "PREPARED"}
	if result.Joined {
		existing, loadErr := w.Contributions.FirewallTransition(ctx, result.Operation.OperationID)
		if loadErr != nil || existing.ContributionID != transition.ContributionID || existing.DesiredSemanticRevision != transition.DesiredSemanticRevision || existing.AfterCompositionRevision != transition.AfterCompositionRevision {
			return protectionoperations.AcquireResult{}, errors.Join(ErrContributionConflict, loadErr)
		}
		return result, nil
	}
	if err := w.Contributions.CreateFirewallTransition(ctx, transition); err != nil {
		return protectionoperations.AcquireResult{}, err
	}
	return result, nil
}

func (w Workflow) Apply(ctx context.Context, input ApplyInput) (Result, error) {
	if err := w.ready(); err != nil {
		return Result{}, err
	}
	if input.Confirmation != "APPLY SERVER PROTECTION "+input.OperationID {
		return Result{}, protectionoperations.ErrConfirmationRequired
	}
	if err := Preflight(input.Plan); err != nil {
		return Result{}, err
	}
	requested, err := contributionFromPlan(input.Plan)
	if err != nil {
		return Result{}, err
	}
	operation, err := w.operation(ctx, input.OperationID)
	if err != nil {
		return Result{}, err
	}
	if operation.Kind != protectionoperations.KindFirewall {
		return Result{}, protectionoperations.ErrFenced
	}
	if operation.State == protectionoperations.StateApplied {
		return Result{OperationID: operation.OperationID, State: operation.State, Revision: operation.Revision, PlanRevision: operation.PlanRevision, GraphRevision: input.Plan.GraphRevision, DesiredStatus: "APPLY", SelectedStatus: "PERSISTED_APPLIED", ActualStatus: "UNKNOWN", ReasonCodes: []string{"actual_state_reverification_required"}}, nil
	}
	if operation.State != protectionoperations.StatePrepared {
		return Result{}, fmt.Errorf("firewall operation is not prepared")
	}
	transition, err := w.Contributions.FirewallTransition(ctx, operation.OperationID)
	if err != nil {
		return Result{}, err
	}
	desired, err := decodeContributionJSON(transition.DesiredJSON)
	if err != nil || desired.SemanticRevision != requested.SemanticRevision || desired.ContributionID != requested.ContributionID ||
		operation.PlanRevision != transition.ManagedPlanRevision || operation.ResourceID != "managed-table:inet:solovey_protection" {
		return Result{}, ErrPlanRevision
	}
	snapshot, err := w.Contributions.FirewallAuthority(ctx)
	if err != nil {
		return Result{}, err
	}
	current, err := contributionsFromSnapshot(snapshot)
	if err != nil {
		return Result{}, err
	}
	currentCompositionRevision := ""
	if snapshot.HasComposition {
		currentCompositionRevision = snapshot.Composition.Revision
	}
	if currentCompositionRevision != transition.BeforeCompositionRevision {
		return Result{}, ErrContributionConflict
	}
	currentContributionRevision := ""
	for _, value := range current {
		if value.ContributionID == desired.ContributionID {
			currentContributionRevision = value.SemanticRevision
		}
	}
	if currentContributionRevision != transition.PreviousSemanticRevision {
		return Result{}, ErrContributionConflict
	}
	composition, err := composeFirewall(replaceContribution(current, &desired))
	if err != nil || composition.Revision != transition.AfterCompositionRevision || composition.PlanRevision != transition.ManagedPlanRevision || composition.CandidateSHA != transition.CandidateSHA256 {
		return Result{}, errors.Join(ErrPlanRevision, err)
	}
	capabilities, err := w.Capabilities(ctx)
	if err != nil || !firewallCapabilitiesAvailable(capabilities, composition.Plan) {
		return Result{}, errors.Join(ErrMissingCapability, err)
	}
	if operation.HelperRevision == "" || operation.HelperRevision != capabilities.Revision {
		return Result{}, ErrHelperRevision
	}
	candidate := RenderManagedNFT(composition.Plan)
	candidateSHA := artifactSHA([]byte(candidate))
	artifact, err := w.Artifacts.WriteRevision(ctx, operation.OperationID, operation.OperationID, candidateFiles(composition.Plan, candidate, candidateSHA))
	if err != nil {
		return Result{}, err
	}
	paths := workflowPaths(artifact.Revision)
	validated, err := w.call(ctx, operation, protectionhelper.OperationNFTValidate, paths, composition.PlanRevision, candidateSHA, "", "", "", false)
	if err != nil {
		return Result{}, err
	}
	if validated == nil || validated.CandidateSHA256 != candidateSHA ||
		validated.PreviousTablePresent && (!validFirewallSHA(validated.PreviousRevision) || !validFirewallSHA(validated.PreviousSHA256)) ||
		!validated.PreviousTablePresent && (validated.PreviousRevision != "" || validated.PreviousSHA256 != "") {
		return Result{}, ErrApplyVerify
	}
	if snapshot.HasComposition {
		if !validated.PreviousTablePresent || validated.PreviousRevision != snapshot.Composition.ManagedPlanRevision || validated.PreviousSHA256 != snapshot.Composition.CandidateSHA256 {
			return Result{}, ErrContributionConflict
		}
	} else if validated.PreviousTablePresent {
		return Result{}, ErrContributionConflict
	}
	applying, err := w.Manager.Transition(ctx, operation.OperationID, operation.Revision, protectionoperations.StateApplying)
	if err != nil {
		return Result{}, err
	}
	result := Result{OperationID: applying.OperationID, State: applying.State, Revision: applying.Revision, ArtifactRevision: artifact.Revision, CandidateSHA256: candidateSHA, PlanRevision: composition.PlanRevision, GraphRevision: composition.Plan.GraphRevision, OwnerObservationRevision: composition.Plan.OwnerObservationRevision, DesiredStatus: "APPLY", SelectedStatus: "APPLYING", ActualStatus: "NOT_VERIFIED"}
	if err := w.Marker.MarkMutation(applying.OperationID, artifact.Revision); err != nil {
		return w.failAndRollback(ctx, applying, artifact.Revision, result, err)
	}
	markerUnixNano := time.Now().UTC().UnixNano()
	if err := w.Contributions.MarkFirewallTransitionMutation(ctx, applying.OperationID, markerUnixNano); err != nil {
		return w.failAndRollback(ctx, applying, artifact.Revision, result, err)
	}
	nftResult, err := w.call(ctx, applying, protectionhelper.OperationNFTApply, paths, composition.PlanRevision, candidateSHA, "", validated.PreviousRevision, validated.PreviousSHA256, validated.PreviousTablePresent)
	if err != nil {
		return w.failAndRollback(ctx, applying, artifact.Revision, result, err)
	}
	mutationUnixNano, clockAdvanced := unixNanoAfter(ctx, markerUnixNano)
	if !clockAdvanced {
		return w.failAndRollback(ctx, applying, artifact.Revision, result, ErrApplyVerify)
	}
	if err := w.Contributions.MarkFirewallTransitionMutationCompleted(ctx, applying.OperationID, mutationUnixNano); err != nil {
		return w.failAndRollback(ctx, applying, artifact.Revision, result, err)
	}
	// Persist the rollback identity as soon as the helper reports a durable
	// mutation. Verification below may still reject the response, but restart
	// recovery must not depend on the semantic authority having been committed.
	if nftResult != nil && validFirewallSHA(nftResult.RollbackSHA256) {
		checkpoint := FirewallCheckpoint{Version: 2, OperationID: applying.OperationID, ArtifactRevision: artifact.Revision, PlanRevision: composition.PlanRevision, GraphRevision: composition.Plan.GraphRevision, OwnerObservationRevision: composition.Plan.OwnerObservationRevision, CandidateSHA256: candidateSHA, RollbackSHA256: nftResult.RollbackSHA256, PreviousRevision: validated.PreviousRevision, PreviousTablePresent: validated.PreviousTablePresent, ContributionID: desired.ContributionID, ContributionRevision: desired.SemanticRevision, CompositionRevision: composition.Revision}
		if err := w.saveCheckpoint(checkpoint); err != nil {
			return w.failAndRollback(ctx, applying, artifact.Revision, result, err)
		}
	}
	if nftResult == nil || nftResult.AppliedRevision != composition.PlanRevision || nftResult.CandidateSHA256 != candidateSHA || !validFirewallSHA(nftResult.RollbackSHA256) ||
		nftResult.PreviousTablePresent != validated.PreviousTablePresent || nftResult.PreviousRevision != validated.PreviousRevision || nftResult.PreviousSHA256 != validated.PreviousSHA256 ||
		nftResult.PreviousTablePresent && nftResult.RollbackSHA256 != validated.PreviousSHA256 {
		return w.failAndRollback(ctx, applying, artifact.Revision, result, ErrApplyVerify)
	}
	result.RollbackSHA256 = nftResult.RollbackSHA256
	model, err := contributionModel(desired)
	if err != nil {
		return w.failAndRollback(ctx, applying, artifact.Revision, result, err)
	}
	compositionRow, err := compositionModel(composition)
	if err != nil {
		return w.failAndRollback(ctx, applying, artifact.Revision, result, err)
	}
	if err := w.Contributions.CommitFirewallAuthority(ctx, applying.OperationID, currentCompositionRevision, transition.PreviousSemanticRevision, &model, compositionRow, "APPLIED"); err != nil {
		return w.failAndRollback(ctx, applying, artifact.Revision, result, err)
	}
	result.Health = w.Health(ctx, append([]hostresources.ProtectableResource(nil), composition.Plan.Resources...))
	if healthFailedFor(composition.Plan.Resources, result.Health) {
		return w.failAndRollback(ctx, applying, artifact.Revision, result, ErrHealthFailed)
	}
	if desired.Kind == ContributionKindUDPDirect && input.PostApplyHealth == nil {
		return w.failAndRollback(ctx, applying, artifact.Revision, result, ErrHealthFailed)
	}
	if input.PostApplyHealth != nil {
		proof, healthErr := input.PostApplyHealth(ctx, PostMutationHealthFence{OperationID: applying.OperationID, ContributionID: desired.ContributionID,
			ContributionRevision: desired.SemanticRevision, CompositionRevision: composition.Revision, ManagedPlanRevision: composition.PlanRevision, MarkerUnixNano: markerUnixNano, MutationUnixNano: mutationUnixNano})
		nowUnixNano := time.Now().UTC().UnixNano()
		if healthErr != nil || proof.ProviderInstance == "" || proof.Generation == 0 || !validFirewallSHA(proof.ObservationRevision) || proof.StartedUnixNano < mutationUnixNano ||
			proof.CompletedUnixNano < proof.StartedUnixNano || proof.CompletedUnixNano > nowUnixNano || proof.ExpiresUnixNano <= nowUnixNano || proof.ExpiresUnixNano-proof.CompletedUnixNano > componenthealth.MaxProtocolProbeFreshness.Nanoseconds() {
			return w.failAndRollback(ctx, applying, artifact.Revision, result, errors.Join(ErrHealthFailed, healthErr))
		}
		if err := w.Contributions.RecordFirewallTransitionHealth(ctx, applying.OperationID, proof.ProviderInstance, proof.Generation, proof.ObservationRevision, proof.StartedUnixNano, proof.CompletedUnixNano, proof.ExpiresUnixNano); err != nil {
			return w.failAndRollback(ctx, applying, artifact.Revision, result, err)
		}
	}
	applied, err := w.Manager.Transition(ctx, applying.OperationID, applying.Revision, protectionoperations.StateApplied)
	if err != nil {
		return result, err
	}
	result.State, result.Revision = applied.State, applied.Revision
	result.SelectedStatus, result.ActualStatus = "APPLIED", "APPLIED"
	return result, nil
}

func unixNanoAfter(ctx context.Context, boundary int64) (int64, bool) {
	timer := time.NewTimer(250 * time.Millisecond)
	ticker := time.NewTicker(100 * time.Microsecond)
	defer timer.Stop()
	defer ticker.Stop()
	for {
		if now := time.Now().UTC().UnixNano(); now > boundary {
			return now, true
		}
		select {
		case <-ctx.Done():
			return 0, false
		case <-timer.C:
			return 0, false
		case <-ticker.C:
		}
	}
}

func (w Workflow) Rollback(ctx context.Context, operationID, confirmation string) (Result, error) {
	if err := w.ready(); err != nil {
		return Result{}, err
	}
	if confirmation != "ROLLBACK SERVER PROTECTION "+operationID {
		return Result{}, protectionoperations.ErrConfirmationRequired
	}
	operation, err := w.operation(ctx, operationID)
	if err != nil {
		return Result{}, err
	}
	if operation.Kind != protectionoperations.KindFirewall {
		return Result{}, protectionoperations.ErrFenced
	}
	return w.rollback(ctx, operation, operationID, false)
}

func (w Workflow) failAndRollback(ctx context.Context, applying protectionrepository.OperationLockModel, artifactRevision string, result Result, cause error) (Result, error) {
	recoveryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
	defer cancel()
	failed, transitionErr := w.Manager.Transition(recoveryCtx, applying.OperationID, applying.Revision, protectionoperations.StateHealthFailed)
	if transitionErr != nil {
		return result, errors.Join(cause, transitionErr)
	}
	rollback, rollbackErr := w.rollback(recoveryCtx, failed, artifactRevision, true)
	result.RollbackAttempted = true
	result.State, result.Revision = rollback.State, rollback.Revision
	result.SelectedStatus, result.ActualStatus = rollback.SelectedStatus, rollback.ActualStatus
	result.ReasonCodes = uniqueSorted(append(result.ReasonCodes, "apply_failed_rollback_attempted"))
	if rollbackErr != nil {
		result.ActualStatus = "UNKNOWN"
		return result, errors.Join(cause, rollbackErr)
	}
	return result, cause
}

func (w Workflow) rollback(ctx context.Context, operation protectionrepository.OperationLockModel, artifactRevision string, automatic bool) (Result, error) {
	if operation.State == protectionoperations.StateRolledBack {
		return Result{OperationID: operation.OperationID, State: operation.State, Revision: operation.Revision, ArtifactRevision: artifactRevision, RollbackAttempted: automatic, PlanRevision: operation.PlanRevision, DesiredStatus: "ROLLBACK", SelectedStatus: "ROLLED_BACK", ActualStatus: "ROLLED_BACK"}, nil
	}
	if operation.State == protectionoperations.StatePrepared {
		cancelled, err := w.Manager.Transition(ctx, operation.OperationID, operation.Revision, protectionoperations.StateCancelled)
		if err != nil {
			return Result{}, err
		}
		if err := w.Contributions.SetFirewallTransitionState(ctx, operation.OperationID, "PREPARED", "CANCELLED"); err != nil {
			return Result{}, err
		}
		return Result{OperationID: cancelled.OperationID, State: cancelled.State, Revision: cancelled.Revision, PlanRevision: cancelled.PlanRevision, DesiredStatus: "ROLLBACK", SelectedStatus: "CANCELLED_BEFORE_MUTATION", ActualStatus: "ROLLED_BACK"}, nil
	}
	if operation.State == protectionoperations.StateApplied {
		checkpoint, err := w.loadCheckpointForRollback(operation.OperationID)
		if err != nil || checkpoint.PlanRevision != operation.PlanRevision {
			return Result{}, errors.Join(errors.New("firewall rollback checkpoint is unavailable"), err)
		}
		artifactRevision = checkpoint.ArtifactRevision
		rolling, err := w.Manager.BeginRollback(ctx, operation.OperationID, operation.Revision)
		if err != nil {
			return Result{}, err
		}
		return w.finishRollback(ctx, rolling, artifactRevision, automatic, true)
	}
	if operation.State != protectionoperations.StateApplying && operation.State != protectionoperations.StateHealthFailed {
		return Result{}, fmt.Errorf("firewall operation cannot be rolled back from %s", operation.State)
	}
	rolling, err := w.Manager.Transition(ctx, operation.OperationID, operation.Revision, protectionoperations.StateRollingBack)
	if err != nil {
		return Result{}, err
	}
	return w.finishRollback(ctx, rolling, artifactRevision, automatic, true)
}

func (w Workflow) finishRollback(ctx context.Context, rolling protectionrepository.OperationLockModel, artifactRevision string, automatic, completeOperation bool) (Result, error) {
	result := Result{OperationID: rolling.OperationID, State: rolling.State, Revision: rolling.Revision, ArtifactRevision: artifactRevision, RollbackAttempted: true, PlanRevision: rolling.PlanRevision, DesiredStatus: "ROLLBACK", SelectedStatus: "ROLLING_BACK", ActualStatus: "NOT_VERIFIED"}
	transition, err := w.Contributions.FirewallTransition(ctx, rolling.OperationID)
	if errors.Is(err, protectionrepository.ErrRecordNotFound) {
		return w.finishCheckpointV1Rollback(ctx, rolling, artifactRevision, result, completeOperation)
	}
	if err != nil || transition.Schema != FirewallTransitionSchemaV1 {
		return w.rollbackFailure(ctx, rolling, result, errors.Join(ErrContributionConflict, err), completeOperation)
	}
	desired, err := decodeContributionJSON(transition.DesiredJSON)
	if err != nil {
		return w.rollbackFailure(ctx, rolling, result, err, completeOperation)
	}
	var previous *ManagedFirewallContributionV1
	if transition.PreviousPresent {
		value, decodeErr := decodeContributionJSON(transition.PreviousJSON)
		if decodeErr != nil || value.SemanticRevision != transition.PreviousSemanticRevision || value.ContributionID != desired.ContributionID {
			return w.rollbackFailure(ctx, rolling, result, errors.Join(ErrContributionConflict, decodeErr), completeOperation)
		}
		previous = &value
	}
	snapshot, err := w.Contributions.FirewallAuthority(ctx)
	if err != nil {
		return w.rollbackFailure(ctx, rolling, result, errors.Join(ErrContributionConflict, err), completeOperation)
	}
	if transition.State == "PREPARED" || transition.State == "CANCELLED" {
		return w.finishPreMutationRollback(ctx, rolling, artifactRevision, result, transition, desired, previous, snapshot, completeOperation)
	}
	if transition.State == "ROLLED_BACK" {
		return w.finishCommittedRollback(ctx, rolling, artifactRevision, result, transition, desired, previous, snapshot, completeOperation)
	}
	if !snapshot.HasComposition {
		return w.finishUncommittedRollback(ctx, rolling, artifactRevision, result, transition, desired, snapshot, completeOperation)
	}
	if snapshot.Composition.State != "ACTIVE" {
		return w.rollbackFailure(ctx, rolling, result, ErrContributionConflict, completeOperation)
	}
	current, err := contributionsFromSnapshot(snapshot)
	if err != nil {
		return w.rollbackFailure(ctx, rolling, result, err, completeOperation)
	}
	currentOwnRevision := ""
	for _, value := range current {
		if value.ContributionID == desired.ContributionID {
			currentOwnRevision = value.SemanticRevision
		}
	}
	if currentOwnRevision != desired.SemanticRevision {
		// A helper mutation can cross its boundary before the semantic commit.
		// Only that exact transition may re-assert its unchanged before-set.
		if transition.State != "MUTATING" || currentOwnRevision != transition.PreviousSemanticRevision {
			return w.rollbackFailure(ctx, rolling, result, ErrContributionConflict, completeOperation)
		}
	}
	target := make([]ManagedFirewallContributionV1, 0, len(current))
	for _, value := range current {
		if value.ContributionID != desired.ContributionID {
			target = append(target, value)
		}
	}
	if previous != nil {
		target = append(target, *previous)
	}
	expectedCompositionRevision := snapshot.Composition.Revision
	var replacementModel *protectionrepository.FirewallContributionModel
	if previous != nil {
		model, modelErr := contributionModel(*previous)
		if modelErr != nil {
			return w.rollbackFailure(ctx, rolling, result, modelErr, completeOperation)
		}
		replacementModel = &model
	}
	var compositionRow protectionrepository.FirewallCompositionModel
	if len(target) == 0 {
		// Empty is a current semantic target, not permission to replay this
		// operation's historical whole-table before-image. Validate the exact
		// current aggregate and then delete only the Solovey-owned table through
		// the existing restricted rollback primitive.
		currentComposition, composeErr := composeFirewall(current)
		if composeErr != nil || currentComposition.Revision != snapshot.Composition.Revision || currentComposition.PlanRevision != snapshot.Composition.ManagedPlanRevision || currentComposition.CandidateSHA != snapshot.Composition.CandidateSHA256 {
			return w.rollbackFailure(ctx, rolling, result, errors.Join(ErrContributionConflict, composeErr), completeOperation)
		}
		candidate := RenderManagedNFT(currentComposition.Plan)
		candidateSHA := artifactSHA([]byte(candidate))
		deleteArtifact := []byte("delete table inet solovey_protection\n")
		deleteSHA := artifactSHA(deleteArtifact)
		revision := rollbackArtifactRevision(rolling.OperationID, "empty")
		files := candidateFiles(currentComposition.Plan, candidate, candidateSHA)
		files["firewall-before.nft"] = deleteArtifact
		files["firewall-before.nft.sha256"] = []byte(deleteSHA + "\n")
		artifact, artifactErr := w.Artifacts.WriteRevision(ctx, rolling.OperationID, revision, files)
		if artifactErr != nil {
			return w.rollbackFailure(ctx, rolling, result, artifactErr, completeOperation)
		}
		result.ArtifactRevision = artifact.Revision
		paths := workflowPaths(artifact.Revision)
		validated, validateErr := w.call(ctx, rolling, protectionhelper.OperationNFTValidate, paths, currentComposition.PlanRevision, candidateSHA, "", "", "", false)
		if validateErr != nil || validated == nil || validated.CandidateSHA256 != candidateSHA {
			return w.rollbackFailure(ctx, rolling, result, errors.Join(ErrContributionConflict, validateErr), completeOperation)
		}
		if validated.PreviousTablePresent {
			if validated.PreviousRevision != snapshot.Composition.ManagedPlanRevision || validated.PreviousSHA256 != snapshot.Composition.CandidateSHA256 {
				return w.rollbackFailure(ctx, rolling, result, ErrContributionConflict, completeOperation)
			}
			nftResult, rollbackErr := w.call(ctx, rolling, protectionhelper.OperationNFTRollback, paths, snapshot.Composition.ManagedPlanRevision, "", deleteSHA, "", "", false)
			if rollbackErr != nil || nftResult == nil || nftResult.ManagedTablePresent || nftResult.RollbackSHA256 != deleteSHA {
				return w.rollbackFailure(ctx, rolling, result, errors.Join(ErrApplyVerify, rollbackErr), completeOperation)
			}
		} else if validated.PreviousRevision != "" || validated.PreviousSHA256 != "" {
			return w.rollbackFailure(ctx, rolling, result, ErrContributionConflict, completeOperation)
		}
		result.RollbackSHA256 = deleteSHA
	} else {
		composition, composeErr := composeFirewall(target)
		if composeErr != nil {
			return w.rollbackFailure(ctx, rolling, result, composeErr, completeOperation)
		}
		candidate := RenderManagedNFT(composition.Plan)
		candidateSHA := artifactSHA([]byte(candidate))
		revision := rollbackArtifactRevision(rolling.OperationID, composition.Revision[:16])
		artifact, artifactErr := w.Artifacts.WriteRevision(ctx, rolling.OperationID, revision, candidateFiles(composition.Plan, candidate, candidateSHA))
		if artifactErr != nil {
			return w.rollbackFailure(ctx, rolling, result, artifactErr, completeOperation)
		}
		result.ArtifactRevision = artifact.Revision
		paths := workflowPaths(artifact.Revision)
		validated, validateErr := w.call(ctx, rolling, protectionhelper.OperationNFTValidate, paths, composition.PlanRevision, candidateSHA, "", "", "", false)
		if validateErr != nil || validated == nil || validated.CandidateSHA256 != candidateSHA || !validated.PreviousTablePresent {
			return w.rollbackFailure(ctx, rolling, result, errors.Join(ErrContributionConflict, validateErr), completeOperation)
		}
		alreadyApplied := validated.PreviousRevision == composition.PlanRevision && validated.PreviousSHA256 == candidateSHA
		currentAuthorityApplied := validated.PreviousRevision == snapshot.Composition.ManagedPlanRevision && validated.PreviousSHA256 == snapshot.Composition.CandidateSHA256
		// A helper may cross the atomic mutation boundary and then fail its
		// post-apply observation before the workflow can record completion. The
		// durable marker plus the exact transition after-image is sufficient to
		// recompose the still-authoritative before-set; MutationCompleted remains
		// mandatory for post-mutation health, not for safe recovery.
		uncommittedAfterApplied := transition.State == "MUTATING" && transition.MarkerUnixNano > 0 &&
			currentOwnRevision == transition.PreviousSemanticRevision && validated.PreviousRevision == transition.ManagedPlanRevision && validated.PreviousSHA256 == transition.CandidateSHA256
		if !alreadyApplied && !currentAuthorityApplied && !uncommittedAfterApplied {
			return w.rollbackFailure(ctx, rolling, result, ErrContributionConflict, completeOperation)
		}
		if !alreadyApplied {
			nftResult, applyErr := w.call(ctx, rolling, protectionhelper.OperationNFTApply, paths, composition.PlanRevision, candidateSHA, "", validated.PreviousRevision, validated.PreviousSHA256, true)
			if applyErr != nil || nftResult == nil || nftResult.AppliedRevision != composition.PlanRevision || nftResult.CandidateSHA256 != candidateSHA {
				return w.rollbackFailure(ctx, rolling, result, errors.Join(ErrApplyVerify, applyErr), completeOperation)
			}
		}
		compositionRow, err = compositionModel(composition)
		if err != nil {
			return w.rollbackFailure(ctx, rolling, result, err, completeOperation)
		}
		result.CandidateSHA256, result.PlanRevision = candidateSHA, composition.PlanRevision
	}
	result.Health = w.RollbackHealth(ctx, nil)
	if healthFailed(result.Health) {
		return w.rollbackFailure(ctx, rolling, result, ErrRollbackHealth, completeOperation)
	}
	if err := w.Contributions.CommitFirewallAuthority(ctx, rolling.OperationID, expectedCompositionRevision, currentOwnRevision, replacementModel, compositionRow, "ROLLED_BACK"); err != nil {
		return w.rollbackFailure(ctx, rolling, result, err, completeOperation)
	}
	if !completeOperation {
		result.SelectedStatus, result.ActualStatus = "ROLLED_BACK", "ROLLED_BACK"
		return result, nil
	}
	rolled, err := w.Manager.Transition(ctx, rolling.OperationID, rolling.Revision, protectionoperations.StateRolledBack)
	if err != nil {
		return result, err
	}
	result.State, result.Revision = rolled.State, rolled.Revision
	result.SelectedStatus, result.ActualStatus = "ROLLED_BACK", "ROLLED_BACK"
	return result, nil
}

// finishPreMutationRollback closes the marker/DB-fence crash window. A
// PREPARED contribution transition proves the helper was never invoked by the
// workflow, but recovery still verifies the exact current managed state before
// cancelling it. CANCELLED is the idempotent retry state if the process stops
// before the operation journal becomes terminal.
func (w Workflow) finishPreMutationRollback(ctx context.Context, rolling protectionrepository.OperationLockModel, artifactRevision string, result Result, transition protectionrepository.FirewallContributionTransitionModel, desired ManagedFirewallContributionV1, previous *ManagedFirewallContributionV1, snapshot protectionrepository.FirewallAuthoritySnapshot, completeOperation bool) (Result, error) {
	if transition.MarkerUnixNano != 0 || transition.MutationCompletedUnixNano != 0 {
		return w.rollbackFailure(ctx, rolling, result, ErrContributionConflict, completeOperation)
	}
	current, err := contributionsFromSnapshot(snapshot)
	if err != nil {
		return w.rollbackFailure(ctx, rolling, result, err, completeOperation)
	}
	currentOwnRevision := ""
	for _, value := range current {
		if value.ContributionID == desired.ContributionID {
			currentOwnRevision = value.SemanticRevision
		}
	}
	expectedOwnRevision := ""
	if previous != nil {
		expectedOwnRevision = previous.SemanticRevision
	}
	if currentOwnRevision != expectedOwnRevision {
		return w.rollbackFailure(ctx, rolling, result, ErrContributionConflict, completeOperation)
	}
	validated, validateErr := w.call(ctx, rolling, protectionhelper.OperationNFTValidate, workflowPaths(artifactRevision), transition.ManagedPlanRevision, transition.CandidateSHA256, "", "", "", false)
	if validateErr != nil || validated == nil || validated.CandidateSHA256 != transition.CandidateSHA256 {
		return w.rollbackFailure(ctx, rolling, result, errors.Join(ErrContributionConflict, validateErr), completeOperation)
	}
	if snapshot.HasComposition {
		composition, composeErr := composeFirewall(current)
		if composeErr != nil || composition.Revision != snapshot.Composition.Revision || composition.PlanRevision != snapshot.Composition.ManagedPlanRevision || composition.CandidateSHA != snapshot.Composition.CandidateSHA256 ||
			!validated.PreviousTablePresent || validated.PreviousRevision != composition.PlanRevision || validated.PreviousSHA256 != composition.CandidateSHA {
			return w.rollbackFailure(ctx, rolling, result, errors.Join(ErrContributionConflict, composeErr), completeOperation)
		}
		result.CandidateSHA256, result.PlanRevision = composition.CandidateSHA, composition.PlanRevision
	} else if len(current) != 0 || validated.PreviousTablePresent || validated.PreviousRevision != "" || validated.PreviousSHA256 != "" {
		return w.rollbackFailure(ctx, rolling, result, ErrContributionConflict, completeOperation)
	}
	result.Health = w.RollbackHealth(ctx, nil)
	if healthFailed(result.Health) {
		return w.rollbackFailure(ctx, rolling, result, ErrRollbackHealth, completeOperation)
	}
	if transition.State == "PREPARED" {
		if err := w.Contributions.SetFirewallTransitionState(ctx, rolling.OperationID, "PREPARED", "CANCELLED"); err != nil {
			return w.rollbackFailure(ctx, rolling, result, err, completeOperation)
		}
	}
	if !completeOperation {
		result.SelectedStatus, result.ActualStatus = "ROLLED_BACK", "ROLLED_BACK"
		return result, nil
	}
	rolled, err := w.Manager.Transition(ctx, rolling.OperationID, rolling.Revision, protectionoperations.StateRolledBack)
	if err != nil {
		return result, err
	}
	result.State, result.Revision = rolled.State, rolled.Revision
	result.SelectedStatus, result.ActualStatus = "ROLLED_BACK", "ROLLED_BACK"
	return result, nil
}

// finishCommittedRollback closes the crash window between the semantic
// authority commit and the operation-journal terminal transition. It performs
// read-only managed-table validation and never repeats apply or rollback.
func (w Workflow) finishCommittedRollback(ctx context.Context, rolling protectionrepository.OperationLockModel, artifactRevision string, result Result, transition protectionrepository.FirewallContributionTransitionModel, desired ManagedFirewallContributionV1, previous *ManagedFirewallContributionV1, snapshot protectionrepository.FirewallAuthoritySnapshot, completeOperation bool) (Result, error) {
	current, err := contributionsFromSnapshot(snapshot)
	if err != nil {
		return w.rollbackFailure(ctx, rolling, result, err, completeOperation)
	}
	currentOwnRevision := ""
	for _, value := range current {
		if value.ContributionID == desired.ContributionID {
			currentOwnRevision = value.SemanticRevision
		}
	}
	expectedOwnRevision := ""
	if previous != nil {
		expectedOwnRevision = previous.SemanticRevision
	}
	if currentOwnRevision != expectedOwnRevision {
		return w.rollbackFailure(ctx, rolling, result, ErrContributionConflict, completeOperation)
	}
	paths := workflowPaths(artifactRevision)
	if snapshot.HasComposition {
		composition, composeErr := composeFirewall(current)
		if composeErr != nil || composition.Revision != snapshot.Composition.Revision || composition.PlanRevision != snapshot.Composition.ManagedPlanRevision || composition.CandidateSHA != snapshot.Composition.CandidateSHA256 {
			return w.rollbackFailure(ctx, rolling, result, errors.Join(ErrContributionConflict, composeErr), completeOperation)
		}
		validated, validateErr := w.call(ctx, rolling, protectionhelper.OperationNFTValidate, paths, composition.PlanRevision, composition.CandidateSHA, "", "", "", false)
		if validateErr != nil || validated == nil || validated.CandidateSHA256 != composition.CandidateSHA || !validated.PreviousTablePresent ||
			validated.PreviousRevision != composition.PlanRevision || validated.PreviousSHA256 != composition.CandidateSHA {
			return w.rollbackFailure(ctx, rolling, result, errors.Join(ErrContributionConflict, validateErr), completeOperation)
		}
		result.CandidateSHA256, result.PlanRevision = composition.CandidateSHA, composition.PlanRevision
	} else {
		if len(current) != 0 || transition.PreviousPresent || previous != nil {
			return w.rollbackFailure(ctx, rolling, result, ErrContributionConflict, completeOperation)
		}
		validated, validateErr := w.call(ctx, rolling, protectionhelper.OperationNFTValidate, paths, transition.ManagedPlanRevision, transition.CandidateSHA256, "", "", "", false)
		if validateErr != nil || validated == nil || validated.CandidateSHA256 != transition.CandidateSHA256 || validated.PreviousTablePresent || validated.PreviousRevision != "" || validated.PreviousSHA256 != "" {
			return w.rollbackFailure(ctx, rolling, result, errors.Join(ErrContributionConflict, validateErr), completeOperation)
		}
	}
	result.Health = w.RollbackHealth(ctx, nil)
	if healthFailed(result.Health) {
		return w.rollbackFailure(ctx, rolling, result, ErrRollbackHealth, completeOperation)
	}
	if !completeOperation {
		result.SelectedStatus, result.ActualStatus = "ROLLED_BACK", "ROLLED_BACK"
		return result, nil
	}
	rolled, err := w.Manager.Transition(ctx, rolling.OperationID, rolling.Revision, protectionoperations.StateRolledBack)
	if err != nil {
		return result, err
	}
	result.State, result.Revision = rolled.State, rolled.Revision
	result.SelectedStatus, result.ActualStatus = "ROLLED_BACK", "ROLLED_BACK"
	return result, nil
}

// finishCheckpointV1Rollback is the upgrade bridge for an exact version-one
// checkpoint created before contribution authority existed. It is deliberately
// rollback-only: no legacy row is inferred into active semantic authority, and
// it is disabled as soon as any current contribution/composition exists.
func (w Workflow) finishCheckpointV1Rollback(ctx context.Context, rolling protectionrepository.OperationLockModel, artifactRevision string, result Result, completeOperation bool) (Result, error) {
	snapshot, err := w.Contributions.FirewallAuthority(ctx)
	if err != nil || snapshot.HasComposition || len(snapshot.Contributions) != 0 {
		return w.rollbackFailure(ctx, rolling, result, errors.Join(ErrContributionConflict, err), completeOperation)
	}
	checkpoint, err := w.loadCheckpointForRollback(rolling.OperationID)
	if err != nil || checkpoint.Version != 1 || checkpoint.ArtifactRevision != artifactRevision || checkpoint.PlanRevision != rolling.PlanRevision {
		return w.rollbackFailure(ctx, rolling, result, errors.Join(ErrContributionConflict, err), completeOperation)
	}
	nftResult, err := w.call(ctx, rolling, protectionhelper.OperationNFTRollback, workflowPaths(artifactRevision), checkpoint.PlanRevision, "", checkpoint.RollbackSHA256, "", "", false)
	if err != nil || nftResult == nil || nftResult.RollbackSHA256 != checkpoint.RollbackSHA256 {
		return w.rollbackFailure(ctx, rolling, result, errors.Join(ErrApplyVerify, err), completeOperation)
	}
	result.RollbackSHA256 = nftResult.RollbackSHA256
	result.Health = w.RollbackHealth(ctx, nil)
	if healthFailed(result.Health) {
		return w.rollbackFailure(ctx, rolling, result, ErrRollbackHealth, completeOperation)
	}
	if !completeOperation {
		result.SelectedStatus, result.ActualStatus = "ROLLED_BACK", "ROLLED_BACK"
		return result, nil
	}
	rolled, err := w.Manager.Transition(ctx, rolling.OperationID, rolling.Revision, protectionoperations.StateRolledBack)
	if err != nil {
		return result, err
	}
	result.State, result.Revision = rolled.State, rolled.Revision
	result.SelectedStatus, result.ActualStatus = "ROLLED_BACK", "ROLLED_BACK"
	return result, nil
}

func rollbackArtifactRevision(operationID, target string) string {
	return operationID + "-rollback-" + target + "-" + strconv.FormatInt(time.Now().UTC().UnixNano(), 36)
}

// finishUncommittedRollback covers the narrow helper-mutation/authority-commit
// interval for the first managed contribution. There is no semantic aggregate
// to compose yet. Recovery therefore observes the exact transition after-image
// and lets the existing helper authenticate its operation-local rollback
// artifact and SHA sidecar. This also covers a helper error after atomic apply
// but before it returned the rollback SHA needed to create checkpoint v2.
func (w Workflow) finishUncommittedRollback(ctx context.Context, rolling protectionrepository.OperationLockModel, artifactRevision string, result Result, transition protectionrepository.FirewallContributionTransitionModel, desired ManagedFirewallContributionV1, snapshot protectionrepository.FirewallAuthoritySnapshot, completeOperation bool) (Result, error) {
	if len(snapshot.Contributions) != 0 || transition.State != "MUTATING" || transition.MarkerUnixNano <= 0 || transition.PreviousPresent || transition.BeforeCompositionRevision != "" || desired.Kind != ContributionKindBaseline {
		return w.rollbackFailure(ctx, rolling, result, ErrContributionConflict, completeOperation)
	}
	paths := workflowPaths(artifactRevision)
	validated, err := w.call(ctx, rolling, protectionhelper.OperationNFTValidate, paths, transition.ManagedPlanRevision, transition.CandidateSHA256, "", "", "", false)
	if err != nil || validated == nil || validated.CandidateSHA256 != transition.CandidateSHA256 {
		return w.rollbackFailure(ctx, rolling, result, errors.Join(ErrContributionConflict, err), completeOperation)
	}
	if validated.PreviousTablePresent {
		if validated.PreviousRevision != transition.ManagedPlanRevision || validated.PreviousSHA256 != transition.CandidateSHA256 {
			return w.rollbackFailure(ctx, rolling, result, ErrContributionConflict, completeOperation)
		}
		// ExpectedSHA256 is intentionally empty here. The privileged helper reads
		// and validates the operation-local .sha256 sidecar it durably wrote
		// before mutation, while ExpectedCurrentRevision fences the exact table
		// that this transition is allowed to remove.
		nftResult, rollbackErr := w.call(ctx, rolling, protectionhelper.OperationNFTRollback, paths, transition.ManagedPlanRevision, "", "", "", "", false)
		if rollbackErr != nil || nftResult == nil || nftResult.ManagedTablePresent || !validFirewallSHA(nftResult.RollbackSHA256) {
			return w.rollbackFailure(ctx, rolling, result, errors.Join(ErrApplyVerify, rollbackErr), completeOperation)
		}
		result.RollbackSHA256 = nftResult.RollbackSHA256
	} else if validated.PreviousRevision != "" || validated.PreviousSHA256 != "" {
		return w.rollbackFailure(ctx, rolling, result, ErrContributionConflict, completeOperation)
	}
	result.Health = w.RollbackHealth(ctx, nil)
	if healthFailed(result.Health) {
		return w.rollbackFailure(ctx, rolling, result, ErrRollbackHealth, completeOperation)
	}
	if err := w.Contributions.CommitFirewallAuthority(ctx, rolling.OperationID, "", "", nil, protectionrepository.FirewallCompositionModel{}, "ROLLED_BACK"); err != nil {
		return w.rollbackFailure(ctx, rolling, result, err, completeOperation)
	}
	if !completeOperation {
		result.SelectedStatus, result.ActualStatus = "ROLLED_BACK", "ROLLED_BACK"
		return result, nil
	}
	rolled, err := w.Manager.Transition(ctx, rolling.OperationID, rolling.Revision, protectionoperations.StateRolledBack)
	if err != nil {
		return result, err
	}
	result.State, result.Revision = rolled.State, rolled.Revision
	result.SelectedStatus, result.ActualStatus = "ROLLED_BACK", "ROLLED_BACK"
	return result, nil
}

func (w Workflow) rollbackFailure(ctx context.Context, rolling protectionrepository.OperationLockModel, result Result, cause error, completeOperation bool) (Result, error) {
	if !completeOperation {
		return result, cause
	}
	failed, transitionErr := w.markRollbackFailed(ctx, rolling)
	if transitionErr == nil {
		result.State, result.Revision = failed.State, failed.Revision
	}
	return result, errors.Join(cause, transitionErr)
}

func (w Workflow) markRollbackFailed(ctx context.Context, rolling protectionrepository.OperationLockModel) (protectionrepository.OperationLockModel, error) {
	// Publish the recovery bundle first. If that fails, keep the operation in
	// rolling_back so restart recovery can retry instead of persisting a
	// terminal rollback_failed state without recovery material.
	if err := w.Recovery.CreateBundle(ctx, rolling, protectionoperations.StateRollbackFailed); err != nil {
		return rolling, err
	}
	return w.Manager.Transition(ctx, rolling.OperationID, rolling.Revision, protectionoperations.StateRollbackFailed)
}

func (w Workflow) operation(ctx context.Context, operationID string) (protectionrepository.OperationLockModel, error) {
	items, err := w.Manager.List(ctx)
	if err != nil {
		return protectionrepository.OperationLockModel{}, err
	}
	for _, item := range items {
		if item.OperationID == operationID {
			return item, nil
		}
	}
	return protectionrepository.OperationLockModel{}, protectionrepository.ErrRecordNotFound
}

func (w Workflow) call(ctx context.Context, operation protectionrepository.OperationLockModel, action protectionhelper.Operation, paths workflowArtifactPaths, planRevision, candidateSHA, rollbackSHA, previousRevision, previousSHA string, previousPresent bool) (*protectionhelper.NFTResult, error) {
	request := protectionhelper.Request{ProtocolVersion: protectionhelper.ProtocolVersion,
		Correlation: protectionhelper.Correlation{OperationID: operation.OperationID, InstanceID: w.Manager.InstanceID(), LockRevision: operation.Revision}, Operation: action}
	switch action {
	case protectionhelper.OperationNFTValidate:
		request.NFTValidate = &protectionhelper.NFTValidateRequest{CandidatePath: paths.candidate, ExpectedRevision: planRevision, ExpectedSHA256: candidateSHA}
	case protectionhelper.OperationNFTApply:
		request.NFTApply = &protectionhelper.NFTApplyRequest{CandidatePath: paths.candidate, RollbackArtifactPath: paths.rollback, ExpectedTable: "inet solovey_protection", ExpectedRevision: planRevision, ExpectedSHA256: candidateSHA, ExpectedPreviousRevision: previousRevision, ExpectedPreviousSHA256: previousSHA, ExpectedPreviousTablePresent: previousPresent}
	case protectionhelper.OperationNFTRollback:
		request.NFTRollback = &protectionhelper.NFTRollbackRequest{RollbackArtifactPath: paths.rollback, ExpectedTable: "inet solovey_protection", ExpectedSHA256: rollbackSHA, ExpectedCurrentRevision: planRevision}
	default:
		return nil, errors.New("firewall helper operation is not allowed")
	}
	response, err := w.Helper.Execute(ctx, request)
	if err != nil {
		return nil, err
	}
	if !response.OK {
		if response.Code == protectionhelper.CodeMissingCapability {
			return nil, fmt.Errorf("%w: %s", ErrMissingCapability, response.Reason)
		}
		return nil, fmt.Errorf("helper %s failed: %s", action, response.Code)
	}
	return response.NFT, nil
}

type workflowArtifactPaths struct{ candidate, rollback string }

func workflowPaths(revision string) workflowArtifactPaths {
	return workflowArtifactPaths{candidate: "revisions/" + revision + "/candidate.nft", rollback: "revisions/" + revision + "/firewall-before.nft"}
}

func candidateFiles(plan FirewallPlan, candidate, candidateSHA string) map[string][]byte {
	mode := "legacy_managed_table"
	graphRevision := ""
	if plan.Mode == ModeCoexistenceEndpointManaged {
		mode = "coexistence_endpoint_managed"
		graphRevision = plan.GraphRevision
	}
	metadata := fmt.Sprintf("{\"family\":\"inet\",\"table\":\"solovey_protection\",\"mode\":%q,\"plan_revision\":%q,\"graph_revision\":%q}\n", mode, plan.Revision, graphRevision)
	return map[string][]byte{
		"candidate.nft":      []byte(candidate),
		"candidate.sha256":   []byte(candidateSHA + "\n"),
		"managed-table.json": []byte(metadata),
	}
}

func artifactSHA(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (w Workflow) saveCheckpoint(checkpoint FirewallCheckpoint) error {
	if err := validateFirewallCheckpoint(checkpoint); err != nil {
		return err
	}
	data, err := json.Marshal(checkpoint)
	if err != nil {
		return err
	}
	return w.State.WriteFirewallState(checkpoint.OperationID, append(data, '\n'))
}

func (w Workflow) loadCheckpoint(operationID string) (FirewallCheckpoint, error) {
	checkpoint, err := w.loadCheckpointForRollback(operationID)
	if err != nil {
		return FirewallCheckpoint{}, err
	}
	if checkpoint.Version != 2 {
		return FirewallCheckpoint{}, errors.New("firewall contribution checkpoint version is invalid")
	}
	return checkpoint, nil
}

func (w Workflow) loadCheckpointForRollback(operationID string) (FirewallCheckpoint, error) {
	data, err := w.State.ReadFirewallState(operationID)
	if err != nil {
		return FirewallCheckpoint{}, err
	}
	var checkpoint FirewallCheckpoint
	if err := json.Unmarshal(data, &checkpoint); err != nil {
		return FirewallCheckpoint{}, err
	}
	if checkpoint.OperationID != operationID {
		return FirewallCheckpoint{}, errors.New("firewall checkpoint operation mismatch")
	}
	return checkpoint, validateFirewallRollbackCheckpoint(checkpoint)
}

func validateFirewallCheckpoint(checkpoint FirewallCheckpoint) error {
	if checkpoint.Version != 2 || checkpoint.OperationID == "" || checkpoint.ArtifactRevision == "" || !validFirewallSHA(checkpoint.PlanRevision) || !validFirewallSHA(checkpoint.CandidateSHA256) || !validFirewallSHA(checkpoint.RollbackSHA256) ||
		checkpoint.ContributionID == "" || !validFirewallSHA(checkpoint.ContributionRevision) || !validFirewallSHA(checkpoint.CompositionRevision) {
		return errors.New("firewall checkpoint identity is invalid")
	}
	if checkpoint.GraphRevision != "" && !validFirewallSHA(checkpoint.GraphRevision) || checkpoint.OwnerObservationRevision != "" && !validFirewallSHA(checkpoint.OwnerObservationRevision) || checkpoint.PreviousTablePresent && !validFirewallSHA(checkpoint.PreviousRevision) || !checkpoint.PreviousTablePresent && checkpoint.PreviousRevision != "" {
		return errors.New("firewall checkpoint revision evidence is invalid")
	}
	return nil
}

func validateFirewallRollbackCheckpoint(checkpoint FirewallCheckpoint) error {
	if checkpoint.Version == 2 {
		return validateFirewallCheckpoint(checkpoint)
	}
	if checkpoint.Version != 1 || checkpoint.OperationID == "" || checkpoint.ArtifactRevision == "" || !validFirewallSHA(checkpoint.PlanRevision) || !validFirewallSHA(checkpoint.CandidateSHA256) || !validFirewallSHA(checkpoint.RollbackSHA256) ||
		checkpoint.ContributionID != "" || checkpoint.ContributionRevision != "" || checkpoint.CompositionRevision != "" {
		return errors.New("legacy firewall rollback checkpoint identity is invalid")
	}
	if checkpoint.GraphRevision != "" && !validFirewallSHA(checkpoint.GraphRevision) || checkpoint.OwnerObservationRevision != "" && !validFirewallSHA(checkpoint.OwnerObservationRevision) || checkpoint.PreviousTablePresent && !validFirewallSHA(checkpoint.PreviousRevision) || !checkpoint.PreviousTablePresent && checkpoint.PreviousRevision != "" {
		return errors.New("legacy firewall rollback checkpoint revision evidence is invalid")
	}
	return nil
}

func validFirewallSHA(value string) bool {
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

func Preflight(plan FirewallPlan) error {
	if strings.TrimSpace(plan.Revision) == "" || plan.Revision != firewallPlanRevision(plan) {
		return ErrPlanRevision
	}
	if plan.Mode == ModeCoexistenceEndpointManaged {
		return preflightEndpointPlan(plan)
	}
	if len(plan.Resources) == 0 {
		return fmt.Errorf("%w: protectable resource inventory is empty", ErrUnsafeResource)
	}
	for _, resource := range plan.Resources {
		if strings.TrimSpace(resource.ID) == "" || resource.Port < 1 || resource.Port > 65535 {
			return fmt.Errorf("%w: invalid listener identity or port", ErrUnsafeResource)
		}
		switch strings.ToLower(strings.TrimSpace(resource.Kind)) {
		case "panel_web", "subscription", "public_site", "inbound":
		default:
			return fmt.Errorf("%w: unsupported health-check resource kind", ErrUnsafeResource)
		}
		protocol, supported := classifySocketProtocol(resource.Protocol)
		if !supported {
			return fmt.Errorf("%w: unsupported listener protocol", ErrUnsafeResource)
		}
		if protocol == "udp" && !containsPort(plan.AllowUDPPorts, resource.Port) || protocol == "tcp" && !containsPort(plan.AllowTCPPorts, resource.Port) {
			return fmt.Errorf("%w: listener port is absent from the managed keep set", ErrUnsafeResource)
		}
	}
	for _, warning := range plan.Warnings {
		lower := strings.ToLower(warning)
		if strings.Contains(lower, "ssh listener is unknown") {
			return ErrUnknownSSH
		}
		if strings.Contains(lower, "allowlist item") && (strings.Contains(lower, "unsupported protocol") || strings.Contains(lower, "invalid port range")) {
			return fmt.Errorf("%w: invalid explicit keep entry", ErrUnsafeResource)
		}
	}
	if len(plan.StormLimits) > 0 {
		return fmt.Errorf("%w: nft rate primitive is unproven", ErrMissingCapability)
	}
	return nil
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
	checked := make(map[string]struct{}, len(results))
	for _, result := range results {
		if result.Status == componenthealth.StatusOK {
			checked[result.ResourceID] = struct{}{}
		}
	}
	for _, resource := range resources {
		if _, ok := checked[resource.ID]; !ok {
			return true
		}
	}
	return false
}

func firewallCapabilitiesAvailable(result *protectionhelper.CapabilitiesResult, plan FirewallPlan) bool {
	if result == nil || result.Revision == "" || !result.NFT.PlatformKnown || !result.NFT.Linux || !result.NFT.Available {
		return false
	}
	for _, operation := range []protectionhelper.Operation{protectionhelper.OperationNFTValidate, protectionhelper.OperationNFTApply, protectionhelper.OperationNFTRollback} {
		if !protectionhelper.CapabilityAvailable(result, operation) {
			return false
		}
	}
	if plan.Mode == ModeCoexistenceEndpointManaged {
		for _, endpoint := range plan.Endpoints {
			for _, contribution := range endpoint.Contributions {
				if !result.NFT.TTLSet {
					return false
				}
				if (contribution.Intent == domain.IntentSoftGraylist || contribution.Intent == domain.IntentRateLimit || contribution.Intent == domain.IntentTemporaryQuarantine) && !result.NFT.RateLimit {
					return false
				}
			}
		}
	}
	return true
}

func (w Workflow) ready() error {
	if w.Manager == nil || w.Helper == nil || w.Artifacts == nil || w.Marker == nil || w.State == nil || w.Recovery == nil || w.Health == nil || w.RollbackHealth == nil || w.Contributions == nil {
		return ErrWorkflowDisabled
	}
	return nil
}
