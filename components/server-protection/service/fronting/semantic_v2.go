package fronting

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/componenthost/fallbacktargets"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	protectionoperations "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
)

const FrontingSemanticStatusSchemaV2 = "solovey-ui/fronting-semantic-status/v2"

type SemanticErrorV2 struct {
	Code      string
	Ambiguous bool
}

func (err *SemanticErrorV2) Error() string {
	if err == nil || err.Code == "" {
		return "fronting_semantic_failed"
	}
	return err.Code
}

type FrontingSocketClaimReferenceV2 struct {
	ResourceID    string `json:"resourceId"`
	EndpointID    string `json:"endpointId"`
	ClaimRevision string `json:"claimRevision"`
}

type SemanticResourceV2 struct {
	ResourceID                   string                                      `json:"resourceId"`
	DisplayIdentity              string                                      `json:"displayIdentity"`
	CurrentConfigurationRevision string                                      `json:"currentConfigurationRevision,omitempty"`
	Runtime                      NginxRuntimeIdentityV2                      `json:"runtime"`
	Capabilities                 []NginxStrategyCapabilityV2                 `json:"capabilities"`
	SocketClaims                 []FrontingSocketClaimV1                     `json:"socketClaims"`
	BackendReferences            []hostresources.FrontingBackendReferenceV1  `json:"backendReferences"`
	FallbackReferences           []fallbacktargets.FallbackTargetReferenceV2 `json:"fallbackReferences"`
}

// SemanticSourceV2 is a read-only resolver. Implementations return exact
// registry/runtime facts; lease acquisition and every mutation remain solely
// in Workflow.PrepareV2/ApplyV2/RollbackV2.
type SemanticSourceV2 interface {
	ResourcesV2(context.Context, time.Time) ([]SemanticResourceV2, error)
	ResolvePreviewV2(context.Context, FrontingPreviewRequestV2, SelectorSetV1, time.Time) (FrontingPlanInputV2, error)
	ResolvePrepareV2(context.Context, FrontingPrepareRequestV2, time.Time) (FrontingStrategyPlanV2, error)
}

type FrontingPreviewRequestV2 struct {
	ResourceID                           string                                      `json:"resourceId"`
	ExpectedCurrentConfigurationRevision string                                      `json:"expectedCurrentConfigurationRevision"`
	RequestedStrategy                    FrontingStrategy                            `json:"requestedStrategy"`
	SocketClaim                          FrontingSocketClaimReferenceV2              `json:"socketClaim"`
	BackendReferences                    []hostresources.FrontingBackendReferenceV1  `json:"backendReferences"`
	FallbackReferences                   []fallbacktargets.FallbackTargetReferenceV2 `json:"fallbackReferences"`
	SelectedProxyMode                    hostresources.ProxyMode                     `json:"selectedProxyMode"`
	Selectors                            []SelectorRouteInputV1                      `json:"selectors"`
	Default                              SelectorDefaultV1                           `json:"default"`
}

type FrontingPrepareRequestV2 struct {
	PlanID                       string   `json:"planId"`
	PlanDigest                   string   `json:"planDigest"`
	ResourceID                   string   `json:"resourceId"`
	RuntimeIdentityRevision      string   `json:"runtimeIdentityRevision"`
	StrategyCapabilityRevision   string   `json:"strategyCapabilityRevision"`
	SocketClaimRevision          string   `json:"socketClaimRevision"`
	SelectorSetRevision          string   `json:"selectorSetRevision"`
	TargetReferenceRevisions     []string `json:"targetReferenceRevisions"`
	IdempotencyKey               string   `json:"idempotencyKey"`
	ExperimentalRiskAcknowledged bool     `json:"experimentalRiskAcknowledged"`
	Acknowledgement              string   `json:"acknowledgement"`
}

type FrontingApplyRequestV2 struct {
	OperationID              string   `json:"operationId"`
	OperationRevision        int      `json:"operationRevision"`
	PlanDigest               string   `json:"planDigest"`
	TargetAuthorityRevisions []string `json:"targetAuthorityRevisions"`
	IdempotencyKey           string   `json:"idempotencyKey"`
	Confirmation             string   `json:"confirmation"`
}

type FrontingRollbackRequestV2 struct {
	OperationID       string `json:"operationId"`
	OperationRevision int    `json:"operationRevision"`
	IdempotencyKey    string `json:"idempotencyKey"`
	Confirmation      string `json:"confirmation"`
}

type FrontingLeaseSummaryV2 struct {
	Kind              string `json:"kind"`
	ReferenceRevision string `json:"referenceRevision"`
	AuthorityRevision string `json:"authorityRevision"`
	State             string `json:"state"`
	ExpiresAt         int64  `json:"expiresAt,omitempty"`
}

type FrontingFallbackReferenceSummaryV2 struct {
	Reference         fallbacktargets.FallbackTargetReferenceV2 `json:"reference"`
	ReferenceRevision string                                    `json:"referenceRevision"`
}

type FrontingOperationViewV2 struct {
	OperationID               string                   `json:"operationId"`
	OperationRevision         int                      `json:"operationRevision"`
	ResourceID                string                   `json:"resourceId"`
	Strategy                  FrontingStrategy         `json:"strategy"`
	WorkflowState             string                   `json:"workflowState"`
	ActualState               string                   `json:"actualState"`
	PlanDigest                string                   `json:"planDigest"`
	CandidateRevision         string                   `json:"candidateRevision,omitempty"`
	ActiveRevision            string                   `json:"activeRevision,omitempty"`
	SocketClaimRevision       string                   `json:"socketClaimRevision,omitempty"`
	BackendReferenceRevisions []string                 `json:"backendReferenceRevisions,omitempty"`
	SelectorSetRevision       string                   `json:"selectorSetRevision,omitempty"`
	MapRevision               string                   `json:"mapRevision,omitempty"`
	Leases                    []FrontingLeaseSummaryV2 `json:"leases"`
	HealthState               string                   `json:"healthState"`
	HealthObservedAt          int64                    `json:"healthObservedAt,omitempty"`
	HealthExpiresAt           int64                    `json:"healthExpiresAt,omitempty"`
	RollbackCount             int                      `json:"rollbackCount"`
	RecoveryClassification    string                   `json:"recoveryClassification,omitempty"`
	RecoveryRequired          bool                     `json:"recoveryRequired"`
	CompatibilityState        string                   `json:"compatibilityState"`
	ReasonCodes               []string                 `json:"reasonCodes"`
	SafeNextAction            string                   `json:"safeNextAction"`
}

type FrontingRecoveryStatusV2 struct {
	OperationID         string   `json:"operationId"`
	OperationRevision   int      `json:"operationRevision"`
	Classification      string   `json:"classification"`
	RecoveryRequired    bool     `json:"recoveryRequired"`
	CheckpointRetained  bool     `json:"checkpointRetained"`
	AuthoritiesRetained bool     `json:"authoritiesRetained"`
	PermittedNextAction string   `json:"permittedNextAction"`
	ReasonCodes         []string `json:"reasonCodes"`
}

type FrontingStatusV2 struct {
	Schema              string                                     `json:"schema"`
	ResourceID          string                                     `json:"resourceId"`
	DisplayIdentity     string                                     `json:"displayIdentity"`
	Runtime             NginxRuntimeIdentityV2                     `json:"runtime"`
	Capabilities        []NginxStrategyCapabilityV2                `json:"capabilities"`
	DesiredStrategy     string                                     `json:"desiredStrategy"`
	SelectedStrategy    string                                     `json:"selectedStrategy"`
	ActualState         string                                     `json:"actualState"`
	ApplyGate           string                                     `json:"applyGate"`
	SocketClaims        []FrontingSocketClaimV1                    `json:"socketClaims"`
	BackendReferences   []hostresources.FrontingBackendReferenceV1 `json:"backendReferences"`
	FallbackReferences  []FrontingFallbackReferenceSummaryV2       `json:"fallbackReferences"`
	SelectorSetRevision string                                     `json:"selectorSetRevision,omitempty"`
	ActiveMapRevision   string                                     `json:"activeMapRevision,omitempty"`
	DefaultPolicy       string                                     `json:"defaultPolicy,omitempty"`
	SelectedProxyMode   string                                     `json:"selectedProxyMode,omitempty"`
	Leases              []FrontingLeaseSummaryV2                   `json:"leases"`
	HealthState         string                                     `json:"healthState"`
	HealthObservedAt    int64                                      `json:"healthObservedAt,omitempty"`
	HealthExpiresAt     int64                                      `json:"healthExpiresAt,omitempty"`
	LatestOperation     *FrontingOperationViewV2                   `json:"latestOperation,omitempty"`
	RecoveryState       string                                     `json:"recoveryState"`
	CompatibilityState  string                                     `json:"compatibilityState"`
	Blocks              []string                                   `json:"blocks"`
	Warnings            []string                                   `json:"warnings"`
	ReasonCodes         []string                                   `json:"reasonCodes"`
	SafeNextAction      string                                     `json:"safeNextAction"`
	UpdatedAt           int64                                      `json:"updatedAt,omitempty"`
}

type FrontingStatusPageV2 struct {
	Items       []FrontingStatusV2 `json:"items"`
	GeneratedAt int64              `json:"generatedAt"`
}

type SemanticServiceV2 struct {
	Workflow   *Workflow
	Repository *protectionrepository.Repository
	Source     SemanticSourceV2
	Now        func() time.Time
}

func (s *SemanticServiceV2) Status(ctx context.Context) (FrontingStatusPageV2, error) {
	now := s.now()
	if s.Repository == nil {
		return FrontingStatusPageV2{}, semanticError("validation_unavailable", false)
	}
	states, err := s.Repository.FrontingStatesV2(ctx)
	if err != nil {
		return FrontingStatusPageV2{}, err
	}
	stateByResource := make(map[string]protectionrepository.FrontingStateV2Model, len(states))
	for _, state := range states {
		stateByResource[state.ResourceID] = state
	}
	resources := []SemanticResourceV2{}
	sourceUnavailable := s.Source == nil
	if !sourceUnavailable {
		resources, err = s.Source.ResourcesV2(ctx, now)
		sourceUnavailable = err != nil
	}
	if sourceUnavailable && len(states) == 0 {
		return FrontingStatusPageV2{}, semanticError("validation_unavailable", false)
	}
	resourceByID := make(map[string]SemanticResourceV2, len(resources))
	for _, resource := range resources {
		if resource.ResourceID != "" {
			resourceByID[resource.ResourceID] = resource
		}
	}
	for id := range stateByResource {
		if _, ok := resourceByID[id]; !ok {
			resourceByID[id] = SemanticResourceV2{ResourceID: id, DisplayIdentity: id}
		}
	}
	ids := make([]string, 0, len(resourceByID))
	for id := range resourceByID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	items := make([]FrontingStatusV2, 0, len(ids))
	for _, id := range ids {
		resource := resourceByID[id]
		view := FrontingStatusV2{
			Schema: FrontingSemanticStatusSchemaV2, ResourceID: id, DisplayIdentity: resource.DisplayIdentity,
			Runtime: resource.Runtime, Capabilities: safeCapabilitiesV2(resource.Capabilities), DesiredStrategy: "DISABLED",
			ActualState: "NOT_APPLIED", ApplyGate: string(FrontingApplyExperimentalDisabledV2),
			SocketClaims: safeSocketClaimsV2(resource.SocketClaims), BackendReferences: safeBackendReferencesV2(resource.BackendReferences),
			FallbackReferences: summarizeFallbackReferencesV2(resource.FallbackReferences), Leases: []FrontingLeaseSummaryV2{},
			HealthState: "UNKNOWN", RecoveryState: "NONE", CompatibilityState: protectionrepository.FrontingCompatibilityCurrentV2,
			Blocks: []string{}, Warnings: []string{}, ReasonCodes: []string{}, SafeNextAction: "PREVIEW",
		}
		if view.DisplayIdentity == "" {
			view.DisplayIdentity = id
		}
		if sourceUnavailable {
			view.Blocks = append(view.Blocks, "validation_unavailable")
			view.SafeNextAction = "REFRESH"
		}
		for _, capability := range view.Capabilities {
			if !capability.Actionable {
				view.ReasonCodes = append(view.ReasonCodes, capability.ReasonCodes...)
			}
		}
		if state, ok := stateByResource[id]; ok {
			validState := validPersistedFrontingStateV2(state)
			if !validState {
				view.DesiredStrategy, view.SelectedStrategy = "DISABLED_REPREVIEW_REQUIRED", ""
				view.ActualState, view.RecoveryState, view.SafeNextAction = "RECONCILE_REQUIRED", "PERSISTED_STATE_INVALID", "INSPECT_RECOVERY"
				view.Blocks = append(view.Blocks, "reconcile_required")
				view.ReasonCodes = append(view.ReasonCodes, "persisted_state_invalid")
			} else {
				applyPersistedFrontingStateV2(&view, state)
				// Runtime identity equality is necessary but not sufficient proof of
				// current active revision, listener identity, lease, and health. The
				// semantic source has no fresh aggregate verifier, so historical
				// APPLIED is always inspection/reconciliation-only.
				if view.ActualState == "APPLIED" {
					view.ActualState, view.RecoveryState, view.SafeNextAction = "RECONCILE_REQUIRED", "CURRENT_RUNTIME_UNVERIFIED", "INSPECT_RECOVERY"
					view.ReasonCodes = append(view.ReasonCodes, "reconcile_required")
				}
			}
			if validState && state.LatestOperationID != "" {
				operation, inspectErr := s.Operation(ctx, state.LatestOperationID)
				if inspectErr == nil {
					view.LatestOperation = &operation
					view.Leases = operation.Leases
				}
			}
		}
		view.Blocks = boundedSemanticStrings(view.Blocks)
		view.Warnings = boundedSemanticStrings(view.Warnings)
		view.ReasonCodes = boundedSemanticStrings(append(append([]string{}, view.ReasonCodes...), append(view.Blocks, view.Warnings...)...))
		items = append(items, view)
	}
	return FrontingStatusPageV2{Items: items, GeneratedAt: now.Unix()}, nil
}

func (s *SemanticServiceV2) Preview(ctx context.Context, request FrontingPreviewRequestV2) (FrontingStrategyPlanV2, error) {
	for _, selector := range request.Selectors {
		if len(selector.ALPN) > 0 {
			return FrontingStrategyPlanV2{}, semanticError("alpn_routing_unsupported", false)
		}
	}
	if !validPreviewRequestV2(request) {
		return FrontingStrategyPlanV2{}, semanticError("selector_invalid", false)
	}
	if s.Source == nil {
		return FrontingStrategyPlanV2{}, semanticError("validation_unavailable", false)
	}
	selectors, err := CanonicalizeSelectorSetV1(request.Selectors, request.Default)
	if err != nil {
		return FrontingStrategyPlanV2{}, semanticError("selector_invalid", false)
	}
	input, err := s.Source.ResolvePreviewV2(ctx, request, selectors, s.now())
	if err != nil {
		return FrontingStrategyPlanV2{}, normalizeSemanticErrorV2(err)
	}
	if input.DesiredStrategy != request.RequestedStrategy || input.Socket.ResourceID != request.ResourceID ||
		input.Socket.ClaimRevision != request.SocketClaim.ClaimRevision || input.Socket.EndpointID != request.SocketClaim.EndpointID ||
		input.Socket.CurrentConfigurationRevision != request.ExpectedCurrentConfigurationRevision ||
		input.Selectors.SelectorSetRevision != selectors.SelectorSetRevision || input.ProxyMode != request.SelectedProxyMode ||
		!sameBackendReferencesV2(input.BackendReferences, request.BackendReferences) || !sameFallbackReferencesV2(input.FallbackTargets, request.FallbackReferences) {
		return FrontingStrategyPlanV2{}, semanticError("plan_digest_mismatch", false)
	}
	plan, err := PlanFrontingStrategyV2(input)
	if err != nil {
		return FrontingStrategyPlanV2{}, normalizeSemanticErrorV2(err)
	}
	if plan.Strategy.Actual != FrontingActualNotAppliedV2 {
		return FrontingStrategyPlanV2{}, semanticError("validation_failed", false)
	}
	return plan, nil
}

func (s *SemanticServiceV2) Prepare(ctx context.Context, request FrontingPrepareRequestV2, actor string) (FrontingOperationViewV2, error) {
	if s.Workflow == nil || s.Repository == nil || s.Source == nil {
		return FrontingOperationViewV2{}, semanticError("validation_unavailable", false)
	}
	if !validPrepareRequestV2(request) {
		return FrontingOperationViewV2{}, semanticError("plan_digest_mismatch", false)
	}
	if !request.ExperimentalRiskAcknowledged {
		return FrontingOperationViewV2{}, semanticError("experimental_ack_required", false)
	}
	if request.Acknowledgement != "PREPARE FRONTING "+request.PlanDigest {
		return FrontingOperationViewV2{}, semanticError("confirmation_mismatch", false)
	}
	plan, err := s.Source.ResolvePrepareV2(ctx, request, s.now())
	if err != nil {
		return FrontingOperationViewV2{}, normalizeSemanticErrorV2(err)
	}
	if !prepareBindingsEqualV2(request, plan) || plan.ExpiresAt <= s.now().Unix() || len(plan.Safety.Blocks) != 0 {
		return FrontingOperationViewV2{}, semanticError("plan_digest_mismatch", false)
	}
	result, err := s.Workflow.PrepareV2(ctx, PrepareV2Input{Plan: plan, Actor: actor, IdempotencyKey: request.IdempotencyKey, Confirmation: request.Acknowledgement})
	if err != nil {
		return FrontingOperationViewV2{}, normalizeSemanticErrorV2(err)
	}
	view, err := s.Operation(ctx, result.OperationID)
	if err != nil {
		return FrontingOperationViewV2{}, err
	}
	if view.ActualState != "PREPARED" {
		return FrontingOperationViewV2{}, semanticError("ambiguous_result", true)
	}
	if err := s.project(ctx, plan, view); err != nil {
		return FrontingOperationViewV2{}, semanticError("ambiguous_result", true)
	}
	return view, nil
}

func (s *SemanticServiceV2) Apply(ctx context.Context, request FrontingApplyRequestV2) (FrontingOperationViewV2, error) {
	if request.Confirmation != "APPLY FRONTING "+request.OperationID {
		return FrontingOperationViewV2{}, semanticError("confirmation_mismatch", false)
	}
	if s.Workflow == nil || s.Repository == nil {
		return FrontingOperationViewV2{}, semanticError("validation_unavailable", false)
	}
	if !validMutationIdentityV2(request.OperationID, request.OperationRevision, request.IdempotencyKey) || !frontingHexV2(request.PlanDigest) {
		return FrontingOperationViewV2{}, semanticError("operation_revision_stale", false)
	}
	if replay, handled, err := s.replayMutation(ctx, "apply", request.IdempotencyKey, request); handled {
		return replay, err
	}
	current, checkpoint, err := s.inspectCheckpoint(ctx, request.OperationID)
	if err != nil {
		return FrontingOperationViewV2{}, err
	}
	if current.Revision != request.OperationRevision || current.State != protectionoperations.StatePrepared {
		return FrontingOperationViewV2{}, semanticError("operation_revision_stale", false)
	}
	if checkpoint.Plan.CanonicalPlanDigest != request.PlanDigest || !sameStringsV2(resultAuthorityRevisionsV2(checkpoint), request.TargetAuthorityRevisions) {
		return FrontingOperationViewV2{}, semanticError("lease_stale", false)
	}
	return s.runMutation(ctx, "apply", request.IdempotencyKey, request, func() (WorkflowResultV2, error) {
		return s.Workflow.ApplyV2(ctx, ApplyV2Input{OperationID: request.OperationID, PlanDigest: request.PlanDigest, Confirmation: request.Confirmation})
	})
}

func (s *SemanticServiceV2) Rollback(ctx context.Context, request FrontingRollbackRequestV2) (FrontingOperationViewV2, error) {
	if request.Confirmation != "ROLLBACK FRONTING "+request.OperationID {
		return FrontingOperationViewV2{}, semanticError("confirmation_mismatch", false)
	}
	if s.Workflow == nil || s.Repository == nil {
		return FrontingOperationViewV2{}, semanticError("validation_unavailable", false)
	}
	if !validMutationIdentityV2(request.OperationID, request.OperationRevision, request.IdempotencyKey) {
		return FrontingOperationViewV2{}, semanticError("operation_revision_stale", false)
	}
	if replay, handled, err := s.replayMutation(ctx, "rollback", request.IdempotencyKey, request); handled {
		return replay, err
	}
	operation, checkpoint, err := s.inspectCheckpoint(ctx, request.OperationID)
	if err != nil {
		return FrontingOperationViewV2{}, err
	}
	if operation.Revision != request.OperationRevision || !rollbackEligibleV2(operation.State) {
		return FrontingOperationViewV2{}, semanticError("operation_revision_stale", false)
	}
	return s.runMutation(ctx, "rollback", request.IdempotencyKey, request, func() (WorkflowResultV2, error) {
		return s.Workflow.RollbackV2(ctx, RollbackV2Input{OperationID: request.OperationID, PlanDigest: checkpoint.Plan.CanonicalPlanDigest, Confirmation: request.Confirmation})
	})
}

func (s *SemanticServiceV2) replayMutation(ctx context.Context, action, key string, request any) (FrontingOperationViewV2, bool, error) {
	receipt, err := s.Repository.FrontingReceiptV2(ctx, action, key)
	if errors.Is(err, protectionrepository.ErrRecordNotFound) {
		return FrontingOperationViewV2{}, false, nil
	}
	if err != nil {
		return FrontingOperationViewV2{}, true, normalizeSemanticErrorV2(err)
	}
	if receipt.RequestDigest != semanticDigestV2(request) {
		return FrontingOperationViewV2{}, true, semanticError("operation_conflict", false)
	}
	if receipt.Status != protectionrepository.FrontingReceiptComplete {
		return FrontingOperationViewV2{}, true, semanticError("ambiguous_result", true)
	}
	var replay FrontingOperationViewV2
	if json.Unmarshal(receipt.ResponseJSON, &replay) != nil || !validOperationViewV2(replay) {
		return FrontingOperationViewV2{}, true, semanticError("ambiguous_result", true)
	}
	return replay, true, nil
}

func (s *SemanticServiceV2) Operation(ctx context.Context, operationID string) (FrontingOperationViewV2, error) {
	if s.Repository == nil || operationID == "" || len(operationID) > 128 {
		return FrontingOperationViewV2{}, semanticError("operation_not_found", false)
	}
	operation, err := s.Repository.OperationByID(ctx, operationID)
	if err != nil || operation.Kind != protectionoperations.KindFronting {
		return FrontingOperationViewV2{}, semanticError("operation_not_found", false)
	}
	view := FrontingOperationViewV2{
		OperationID: operation.OperationID, OperationRevision: operation.Revision, ResourceID: operation.ResourceID,
		WorkflowState: operation.State, ActualState: semanticActualStateV2(operation.State), PlanDigest: operation.PlanRevision,
		Leases: []FrontingLeaseSummaryV2{}, HealthState: "UNKNOWN", CompatibilityState: protectionrepository.FrontingCompatibilityLegacyRepreview,
		ReasonCodes: []string{"legacy_fronting_bounded_inspection"}, SafeNextAction: safeNextActionV2(operation.State),
	}
	if s.Workflow == nil {
		return view, nil
	}
	checkpoint, loadErr := s.Workflow.loadV2(operationID)
	if loadErr != nil || checkpoint.Schema != FrontingWorkflowCheckpointSchemaV2 {
		return view, nil
	}
	view.Strategy = checkpoint.Plan.Strategy.Selected
	view.PlanDigest = checkpoint.Plan.CanonicalPlanDigest
	view.CandidateRevision = checkpoint.CandidateRevision
	view.ActiveRevision = checkpoint.ActualActiveRevision
	view.SocketClaimRevision = checkpoint.SocketClaimRevision
	view.BackendReferenceRevisions = append([]string(nil), checkpoint.BackendReferenceRevisions...)
	view.SelectorSetRevision = checkpoint.SelectorSetRevision
	view.MapRevision = checkpoint.MapRevision
	view.Leases = projectAuthoritySummariesV2(checkpoint)
	view.RollbackCount = checkpoint.RollbackAttemptCount
	view.RecoveryClassification = checkpoint.RecoveryClassification
	view.RecoveryRequired = operation.State == protectionoperations.StateRollbackFailed || operation.State == protectionoperations.StateReconcileRequired || checkpoint.RecoveryClassification != ""
	view.CompatibilityState = protectionrepository.FrontingCompatibilityCurrentV2
	view.ReasonCodes = boundedSemanticStrings(checkpoint.ReasonCodes)
	if checkpoint.Plan.Strategy.Selected == StrategySNIPreread {
		view.HealthState, view.HealthObservedAt, view.HealthExpiresAt = sniHealthStateV2(checkpoint.SNIHealth), checkpoint.SNIHealth.ObservedAt, checkpoint.SNIHealth.ExpiresAt
	} else {
		view.HealthState, view.HealthObservedAt, view.HealthExpiresAt = fixedHealthStateV2(checkpoint.Health), checkpoint.Health.ObservedAt, checkpoint.Health.ExpiresAt
	}
	return view, nil
}

func (s *SemanticServiceV2) Recovery(ctx context.Context, operationID string) (FrontingRecoveryStatusV2, error) {
	operation, err := s.Operation(ctx, operationID)
	if err != nil {
		return FrontingRecoveryStatusV2{}, err
	}
	return FrontingRecoveryStatusV2{
		OperationID: operation.OperationID, OperationRevision: operation.OperationRevision,
		Classification: operation.RecoveryClassification, RecoveryRequired: operation.RecoveryRequired,
		CheckpointRetained: operation.CandidateRevision != "", AuthoritiesRetained: authoritySummariesGuardV2(operation.Leases),
		PermittedNextAction: operation.SafeNextAction, ReasonCodes: append([]string(nil), operation.ReasonCodes...),
	}, nil
}

func (s *SemanticServiceV2) runMutation(ctx context.Context, action, key string, request any, invoke func() (WorkflowResultV2, error)) (FrontingOperationViewV2, error) {
	digest := semanticDigestV2(request)
	now := s.now().Unix()
	receipt, joined, err := s.Repository.ClaimFrontingReceiptV2(ctx, protectionrepository.FrontingIdempotencyV2Model{
		Action: action, IdempotencyKey: key, RequestDigest: digest, Status: protectionrepository.FrontingReceiptPending,
		ResponseJSON: []byte(`{}`), CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return FrontingOperationViewV2{}, normalizeSemanticErrorV2(err)
	}
	if joined {
		if receipt.RequestDigest != digest {
			return FrontingOperationViewV2{}, semanticError("operation_conflict", false)
		}
		if receipt.Status != protectionrepository.FrontingReceiptComplete || len(receipt.ResponseJSON) == 0 {
			return FrontingOperationViewV2{}, semanticError("ambiguous_result", true)
		}
		var replay FrontingOperationViewV2
		if json.Unmarshal(receipt.ResponseJSON, &replay) != nil || !validOperationViewV2(replay) {
			return FrontingOperationViewV2{}, semanticError("ambiguous_result", true)
		}
		return replay, nil
	}
	result, err := invoke()
	if err != nil {
		return FrontingOperationViewV2{}, normalizeSemanticErrorV2(err)
	}
	view, err := s.Operation(context.WithoutCancel(ctx), result.OperationID)
	if err != nil {
		return FrontingOperationViewV2{}, semanticError("ambiguous_result", true)
	}
	checkpointOperation, checkpoint, inspectErr := s.inspectCheckpoint(context.WithoutCancel(ctx), view.OperationID)
	if inspectErr != nil || checkpointOperation.OperationID == "" || s.project(context.WithoutCancel(ctx), checkpoint.Plan, view) != nil {
		return FrontingOperationViewV2{}, semanticError("ambiguous_result", true)
	}
	encoded, err := json.Marshal(view)
	if err != nil || s.Repository.CompleteFrontingReceiptV2(context.WithoutCancel(ctx), action, key, digest, view.OperationID, view.OperationRevision, encoded, s.now().Unix()) != nil {
		return FrontingOperationViewV2{}, semanticError("ambiguous_result", true)
	}
	return view, nil
}

func (s *SemanticServiceV2) inspectCheckpoint(ctx context.Context, operationID string) (protectionrepository.OperationLockModel, CheckpointV2, error) {
	operation, err := s.Repository.OperationByID(ctx, operationID)
	if err != nil || operation.Kind != protectionoperations.KindFronting || s.Workflow == nil {
		return protectionrepository.OperationLockModel{}, CheckpointV2{}, semanticError("operation_not_found", false)
	}
	checkpoint, err := s.Workflow.loadV2(operationID)
	if err != nil || checkpoint.Schema != FrontingWorkflowCheckpointSchemaV2 {
		return protectionrepository.OperationLockModel{}, CheckpointV2{}, semanticError("reconcile_required", false)
	}
	return operation, checkpoint, nil
}

func (s *SemanticServiceV2) project(ctx context.Context, plan FrontingStrategyPlanV2, operation FrontingOperationViewV2) error {
	now := s.now().Unix()
	state := protectionrepository.FrontingStateV2Model{
		ResourceID: plan.PublicSocket.ResourceID, Schema: protectionrepository.FrontingStateSchemaV2, DisplayIdentity: plan.PublicSocket.ResourceID,
		DesiredStrategy: string(plan.Strategy.Desired), SelectedStrategy: string(plan.Strategy.Selected), ActualState: operation.ActualState,
		ApplyGate: string(plan.ApplyGate), RuntimeState: string(plan.Runtime.State), RuntimeIdentityRevision: plan.Runtime.IdentityRevision,
		InstallationClass:          string(NginxInstallationUnknown),
		StrategyCapabilityRevision: plan.StrategyCapabilityRevision, DefaultPolicy: string(plan.Selectors.Default.Policy),
		SelectedProxyMode: string(plan.Targets.SelectedProxyMode), ActiveMapRevision: operation.MapRevision,
		CandidateRevision: operation.CandidateRevision, ActiveRevision: operation.ActiveRevision,
		LatestOperationID: operation.OperationID, LatestOperationRevision: operation.OperationRevision, LatestOperationState: operation.WorkflowState,
		HealthState: operation.HealthState, HealthObservedAt: operation.HealthObservedAt, HealthExpiresAt: operation.HealthExpiresAt,
		RecoveryClassification: operation.RecoveryClassification, CompatibilityState: protectionrepository.FrontingCompatibilityCurrentV2,
		SafeNextAction: operation.SafeNextAction, GuardingProviderLease: authoritySummariesGuardV2(operation.Leases),
		RecoverableArtifact:       operation.CandidateRevision != "" && recoverySignificantStateV2(operation.ActualState),
		OwnsActiveManagedRevision: operation.ActualState == "APPLIED" || operation.ActualState == "DEGRADED" || operation.ActualState == "RECONCILE_REQUIRED",
		CreatedAt:                 now, UpdatedAt: now,
	}
	state.SocketClaimJSON, _ = json.Marshal(plan.PublicSocket)
	state.BackendReferencesJSON, _ = json.Marshal(plan.Targets.BackendReferences)
	state.FallbackReferencesJSON, _ = json.Marshal(plan.Targets.FallbackReferences)
	state.SelectorSetJSON, _ = json.Marshal(plan.Selectors)
	state.LeaseMirrorsJSON, _ = json.Marshal(operation.Leases)
	state.ReasonCodesJSON, _ = json.Marshal(boundedSemanticStrings(append(plan.Safety.ReasonCodes, operation.ReasonCodes...)))
	state.BlocksJSON, _ = json.Marshal(boundedSemanticStrings(plan.Safety.Blocks))
	state.WarningsJSON, _ = json.Marshal(boundedSemanticStrings(plan.Safety.Warnings))
	return s.Repository.ProjectFrontingStateV2(ctx, state)
}

func (s *SemanticServiceV2) now() time.Time {
	if s != nil && s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func semanticError(code string, ambiguous bool) error {
	return &SemanticErrorV2{Code: code, Ambiguous: ambiguous}
}

func normalizeSemanticErrorV2(err error) error {
	if err == nil {
		return nil
	}
	var semantic *SemanticErrorV2
	if errors.As(err, &semantic) {
		return semantic
	}
	var workflow *WorkflowErrorV2
	if errors.As(err, &workflow) {
		return semanticError(workflow.Code, workflow.Ambiguous)
	}
	switch {
	case errors.Is(err, protectionrepository.ErrRecordNotFound):
		return semanticError("operation_not_found", false)
	case errors.Is(err, protectionrepository.ErrRevisionConflict), errors.Is(err, protectionrepository.ErrOperationFenced):
		return semanticError("operation_revision_stale", false)
	case errors.Is(err, protectionrepository.ErrOperationConflict):
		return semanticError("operation_conflict", false)
	default:
		return semanticError("validation_unavailable", false)
	}
}

func validPreviewRequestV2(value FrontingPreviewRequestV2) bool {
	if value.ResourceID == "" || len(value.ResourceID) > 256 || !frontingHexV2(value.ExpectedCurrentConfigurationRevision) ||
		value.SocketClaim.ResourceID != value.ResourceID || value.SocketClaim.EndpointID == "" || len(value.SocketClaim.EndpointID) > 256 ||
		!frontingHexV2(value.SocketClaim.ClaimRevision) || value.SelectedProxyMode != hostresources.ProxyModeOff && value.SelectedProxyMode != hostresources.ProxyModeOn ||
		value.RequestedStrategy != StrategyL4OneToOne && value.RequestedStrategy != StrategySNIPreread ||
		len(value.BackendReferences)+len(value.FallbackReferences) == 0 || len(value.BackendReferences)+len(value.FallbackReferences) > MaxFixedTargetsV1 {
		return false
	}
	for _, reference := range value.BackendReferences {
		if reference.Validate() != nil {
			return false
		}
	}
	for _, reference := range value.FallbackReferences {
		if reference.Validate() != nil {
			return false
		}
	}
	return true
}

func validPrepareRequestV2(value FrontingPrepareRequestV2) bool {
	return frontingHexV2(value.PlanDigest) && value.PlanID == "fronting_"+value.PlanDigest[:24] && value.ResourceID != "" && len(value.ResourceID) <= 256 &&
		frontingHexV2(value.RuntimeIdentityRevision) && frontingHexV2(value.StrategyCapabilityRevision) && frontingHexV2(value.SocketClaimRevision) &&
		(value.SelectorSetRevision == "" || frontingHexV2(value.SelectorSetRevision)) && validIdempotencyV2(value.IdempotencyKey) &&
		len(value.TargetReferenceRevisions) > 0 && len(value.TargetReferenceRevisions) <= MaxFixedTargetsV1
}

func validMutationIdentityV2(operationID string, revision int, key string) bool {
	return operationID != "" && len(operationID) <= 128 && revision > 0 && validIdempotencyV2(key)
}

func validIdempotencyV2(value string) bool {
	if value == "" || len(value) > 128 || strings.TrimSpace(value) != value {
		return false
	}
	for _, char := range value {
		if char < 0x21 || char > 0x7e {
			return false
		}
	}
	return true
}

func prepareBindingsEqualV2(request FrontingPrepareRequestV2, plan FrontingStrategyPlanV2) bool {
	return request.PlanID == plan.PlanID && request.PlanDigest == plan.CanonicalPlanDigest && request.ResourceID == plan.PublicSocket.ResourceID &&
		request.RuntimeIdentityRevision == plan.Runtime.IdentityRevision && request.StrategyCapabilityRevision == plan.StrategyCapabilityRevision &&
		request.SocketClaimRevision == plan.PublicSocket.ClaimRevision && request.SelectorSetRevision == plan.Selectors.SelectorSetRevision &&
		sameStringsV2(request.TargetReferenceRevisions, plan.Targets.ReferenceRevisions)
}

func sameStringsV2(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	a, b := append([]string(nil), left...), append([]string(nil), right...)
	sort.Strings(a)
	sort.Strings(b)
	for index := range a {
		if a[index] != b[index] {
			return false
		}
	}
	return true
}

func sameBackendReferencesV2(left, right []hostresources.FrontingBackendReferenceV1) bool {
	a, b := make([]string, len(left)), make([]string, len(right))
	for index := range left {
		a[index] = left[index].CanonicalReferenceRevision
	}
	for index := range right {
		b[index] = right[index].CanonicalReferenceRevision
	}
	return sameStringsV2(a, b)
}

func sameFallbackReferencesV2(left []FallbackPlanningTargetV2, right []fallbacktargets.FallbackTargetReferenceV2) bool {
	a, b := make([]string, len(left)), make([]string, len(right))
	for index := range left {
		a[index] = v2Revision(left[index].Reference)
	}
	for index := range right {
		b[index] = v2Revision(right[index])
	}
	return sameStringsV2(a, b)
}

func resultAuthorityRevisionsV2(checkpoint CheckpointV2) []string {
	values := make([]string, 0, len(checkpoint.EndpointLeases)+len(checkpoint.FallbackReservations))
	for _, lease := range checkpoint.EndpointLeases {
		values = append(values, lease.LeaseRevision)
	}
	for _, reservation := range checkpoint.FallbackReservations {
		values = append(values, reservation.ReservationRevision)
	}
	sort.Strings(values)
	return values
}

func projectAuthoritySummariesV2(checkpoint CheckpointV2) []FrontingLeaseSummaryV2 {
	values := make([]FrontingLeaseSummaryV2, 0, len(checkpoint.EndpointLeases)+len(checkpoint.FallbackReservations))
	for _, lease := range checkpoint.EndpointLeases {
		values = append(values, FrontingLeaseSummaryV2{Kind: "BACKEND", ReferenceRevision: lease.ExactReference.CanonicalReferenceRevision, AuthorityRevision: lease.LeaseRevision, State: string(lease.State), ExpiresAt: lease.ExpiresAt})
	}
	for _, reservation := range checkpoint.FallbackReservations {
		values = append(values, FrontingLeaseSummaryV2{Kind: "FALLBACK", ReferenceRevision: v2Revision(reservation.ExactTargetReference), AuthorityRevision: reservation.ReservationRevision, State: string(reservation.State), ExpiresAt: reservation.FreshnessExpiresAt})
	}
	sort.Slice(values, func(i, j int) bool { return values[i].ReferenceRevision < values[j].ReferenceRevision })
	return values
}

func authoritySummariesGuardV2(values []FrontingLeaseSummaryV2) bool {
	for _, value := range values {
		if value.State != "" && value.State != "RELEASED" {
			return true
		}
	}
	return false
}

func semanticActualStateV2(state string) string {
	switch state {
	case protectionoperations.StatePrepared:
		return "PREPARED"
	case protectionoperations.StateApplying:
		return "APPLYING"
	case protectionoperations.StateHealth:
		return "HEALTH"
	case protectionoperations.StateApplied:
		return "APPLIED"
	case protectionoperations.StateHealthFailed:
		return "DEGRADED"
	case protectionoperations.StateRollingBack:
		return "ROLLING_BACK"
	case protectionoperations.StateRolledBack:
		return "ROLLED_BACK"
	case protectionoperations.StateRollbackFailed:
		return "ROLLBACK_FAILED"
	case protectionoperations.StateReconcileRequired:
		return "RECONCILE_REQUIRED"
	case protectionoperations.StateCancelled:
		return "CANCELLED"
	default:
		return "NOT_APPLIED"
	}
}

func rollbackEligibleV2(state string) bool {
	return state == protectionoperations.StateApplied || state == protectionoperations.StateHealthFailed || state == protectionoperations.StateReconcileRequired
}

func safeNextActionV2(state string) string {
	switch state {
	case protectionoperations.StatePrepared:
		return "APPLY_OR_ROLLBACK"
	case protectionoperations.StateApplied, protectionoperations.StateHealthFailed:
		return "ROLLBACK"
	case protectionoperations.StateRollbackFailed, protectionoperations.StateReconcileRequired:
		return "INSPECT_RECOVERY"
	case protectionoperations.StateApplying, protectionoperations.StateHealth, protectionoperations.StateRollingBack:
		return "REFRESH"
	default:
		return "PREVIEW"
	}
}

func recoverySignificantStateV2(state string) bool {
	switch state {
	case "PREPARED", "APPLYING", "HEALTH", "APPLIED", "DEGRADED", "ROLLING_BACK", "ROLLBACK_FAILED", "RECONCILE_REQUIRED":
		return true
	default:
		return false
	}
}

func fixedHealthStateV2(value FixedL4HealthEvidenceV2) string {
	if value.Schema == "" {
		return "UNKNOWN"
	}
	if value.PublicFixtureAccepted && value.ExpectedBackendReached && value.AlternateTargetReceipts == 0 {
		return "HEALTHY"
	}
	return "FAILED"
}

func sniHealthStateV2(value SNIPrereadHealthEvidenceV2) string {
	if value.Schema == "" {
		return "UNKNOWN"
	}
	if len(value.ReasonCodes) == 0 {
		return "HEALTHY"
	}
	return "FAILED"
}

func semanticDigestV2(value any) string {
	encoded, _ := json.Marshal(value)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func boundedSemanticStrings(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || len(value) > 96 || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
		if len(result) == 32 {
			break
		}
	}
	sort.Strings(result)
	return result
}

func applyPersistedFrontingStateV2(view *FrontingStatusV2, state protectionrepository.FrontingStateV2Model) {
	view.DesiredStrategy, view.SelectedStrategy, view.ActualState, view.ApplyGate = state.DesiredStrategy, state.SelectedStrategy, state.ActualState, state.ApplyGate
	view.SelectorSetRevision, view.ActiveMapRevision, view.DefaultPolicy, view.SelectedProxyMode = selectorRevisionV2(state.SelectorSetJSON), state.ActiveMapRevision, state.DefaultPolicy, state.SelectedProxyMode
	view.HealthState, view.HealthObservedAt, view.HealthExpiresAt = state.HealthState, state.HealthObservedAt, state.HealthExpiresAt
	view.RecoveryState, view.CompatibilityState, view.SafeNextAction, view.UpdatedAt = state.RecoveryClassification, state.CompatibilityState, state.SafeNextAction, state.UpdatedAt
	_ = json.Unmarshal(state.LeaseMirrorsJSON, &view.Leases)
	_ = json.Unmarshal(state.ReasonCodesJSON, &view.ReasonCodes)
	_ = json.Unmarshal(state.BlocksJSON, &view.Blocks)
	_ = json.Unmarshal(state.WarningsJSON, &view.Warnings)
	if view.RecoveryState == "" {
		view.RecoveryState = "NONE"
	}
}

func selectorRevisionV2(data []byte) string {
	var value SelectorSetV1
	if json.Unmarshal(data, &value) == nil {
		return value.SelectorSetRevision
	}
	return ""
}

func validPersistedFrontingStateV2(state protectionrepository.FrontingStateV2Model) bool {
	if !protectionrepository.ValidFrontingStateV2Model(state) || !validStringListJSONV2(state.ReasonCodesJSON) ||
		!validStringListJSONV2(state.BlocksJSON) || !validStringListJSONV2(state.WarningsJSON) {
		return false
	}
	if state.CompatibilityState == protectionrepository.FrontingCompatibilityLegacyRepreview {
		return state.ActualState == "NOT_APPLIED" && state.DesiredStrategy == "DISABLED_REPREVIEW_REQUIRED" && !state.GuardingProviderLease && !state.OwnsActiveManagedRevision
	}
	if (state.SelectedStrategy != string(StrategyL4OneToOne) && state.SelectedStrategy != string(StrategySNIPreread)) ||
		!frontingHexV2(state.RuntimeIdentityRevision) || !frontingHexV2(state.StrategyCapabilityRevision) ||
		state.LatestOperationID == "" || len(state.LatestOperationID) > 128 || state.LatestOperationRevision <= 0 {
		return false
	}
	var socket FrontingSocketClaimV1
	if json.Unmarshal(state.SocketClaimJSON, &socket) != nil || socket.Validate(time.Unix(socket.ObservedAt, 0).UTC()) != nil {
		return false
	}
	var backends []hostresources.FrontingBackendReferenceV1
	if json.Unmarshal(state.BackendReferencesJSON, &backends) != nil || len(backends) > MaxFixedTargetsV1 {
		return false
	}
	for _, reference := range backends {
		if reference.Validate() != nil {
			return false
		}
	}
	var fallbacks []fallbacktargets.FallbackTargetReferenceV2
	if json.Unmarshal(state.FallbackReferencesJSON, &fallbacks) != nil || len(backends)+len(fallbacks) == 0 || len(backends)+len(fallbacks) > MaxFixedTargetsV1 {
		return false
	}
	for _, reference := range fallbacks {
		if reference.Validate() != nil {
			return false
		}
	}
	var selectors SelectorSetV1
	if json.Unmarshal(state.SelectorSetJSON, &selectors) != nil || selectors.Validate() != nil {
		return false
	}
	var leases []FrontingLeaseSummaryV2
	if json.Unmarshal(state.LeaseMirrorsJSON, &leases) != nil || len(leases) > MaxFixedTargetsV1 {
		return false
	}
	for _, lease := range leases {
		if !validLeaseSummaryV2(lease) {
			return false
		}
	}
	return true
}

func validStringListJSONV2(data []byte) bool {
	var values []string
	if json.Unmarshal(data, &values) != nil || len(values) > 32 {
		return false
	}
	for _, value := range values {
		if value == "" || len(value) > 96 {
			return false
		}
		for _, char := range value {
			if char < 0x20 || char > 0x7e {
				return false
			}
		}
	}
	return true
}

func validLeaseSummaryV2(value FrontingLeaseSummaryV2) bool {
	if (value.Kind != "BACKEND" && value.Kind != "FALLBACK") || !frontingHexV2(value.ReferenceRevision) || value.ExpiresAt < 0 ||
		value.Kind == "BACKEND" && !frontingHexV2(value.AuthorityRevision) || value.Kind == "FALLBACK" && !validIdempotencyV2(value.AuthorityRevision) {
		return false
	}
	switch value.State {
	case "RESERVED", "MUTATION_PENDING", "ACTIVE", "RECONCILE_REQUIRED", "RELEASED":
		return true
	default:
		return false
	}
}

func validOperationViewV2(value FrontingOperationViewV2) bool {
	if value.OperationID == "" || len(value.OperationID) > 128 || value.OperationRevision <= 0 || value.WorkflowState == "" ||
		semanticActualStateV2(value.WorkflowState) != value.ActualState || len(value.Leases) > MaxFixedTargetsV1 || len(value.ReasonCodes) > 32 {
		return false
	}
	for _, lease := range value.Leases {
		if !validLeaseSummaryV2(lease) {
			return false
		}
	}
	for _, reason := range value.ReasonCodes {
		if reason == "" || len(reason) > 96 {
			return false
		}
	}
	return true
}

func safeCapabilitiesV2(values []NginxStrategyCapabilityV2) []NginxStrategyCapabilityV2 {
	if len(values) > 4 {
		values = values[:4]
	}
	return append([]NginxStrategyCapabilityV2(nil), values...)
}
func safeSocketClaimsV2(values []FrontingSocketClaimV1) []FrontingSocketClaimV1 {
	if len(values) > 64 {
		values = values[:64]
	}
	return append([]FrontingSocketClaimV1(nil), values...)
}
func safeBackendReferencesV2(values []hostresources.FrontingBackendReferenceV1) []hostresources.FrontingBackendReferenceV1 {
	if len(values) > MaxFixedTargetsV1 {
		values = values[:MaxFixedTargetsV1]
	}
	return append([]hostresources.FrontingBackendReferenceV1(nil), values...)
}
func safeFallbackReferencesV2(values []fallbacktargets.FallbackTargetReferenceV2) []fallbacktargets.FallbackTargetReferenceV2 {
	if len(values) > MaxFixedTargetsV1 {
		values = values[:MaxFixedTargetsV1]
	}
	return append([]fallbacktargets.FallbackTargetReferenceV2(nil), values...)
}

func summarizeFallbackReferencesV2(values []fallbacktargets.FallbackTargetReferenceV2) []FrontingFallbackReferenceSummaryV2 {
	values = safeFallbackReferencesV2(values)
	result := make([]FrontingFallbackReferenceSummaryV2, 0, len(values))
	for _, value := range values {
		result = append(result, FrontingFallbackReferenceSummaryV2{Reference: value, ReferenceRevision: v2Revision(value)})
	}
	return result
}
