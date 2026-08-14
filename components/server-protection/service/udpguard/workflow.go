package udpguard

import (
	"context"
	"errors"
	"strings"
	"time"

	componenthealth "github.com/MalenkiySolovey/solovey-ui/componenthost/health"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	protectionfirewall "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/firewall"
	protectionoperations "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
)

const (
	CodeMalformedInput          = "MALFORMED_INPUT"
	CodeConfirmationRequired    = "CONFIRMATION_REQUIRED"
	CodeMissingCapability       = "BLOCKED_MISSING_CAPABILITY"
	CodeRevisionDrift           = "BLOCKED_REVISION_DRIFT"
	CodeExperimentalAckRequired = "EXPERIMENTAL_ACK_REQUIRED"
	CodeIdempotencyConflict     = "IDEMPOTENCY_CONFLICT"
	CodeAmbiguousResult         = "AMBIGUOUS_RESULT"
	CodeOperationNotFound       = "OPERATION_NOT_FOUND"
	CodeInternalFailure         = "INTERNAL_FAILURE"
)

type ContractError struct {
	Code  string
	Cause error
}

func (e *ContractError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause == nil {
		return e.Code
	}
	return e.Code + ": " + e.Cause.Error()
}

func (e *ContractError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func ErrorCode(err error) string {
	var contract *ContractError
	if errors.As(err, &contract) {
		return contract.Code
	}
	return ""
}

func contractError(code string, cause error) error {
	return &ContractError{Code: code, Cause: cause}
}

type PlanReferenceV1 struct {
	PlanID                   string `json:"planId"`
	PlanDigest               string `json:"planDigest"`
	ResourceID               string `json:"resourceId"`
	EndpointID               string `json:"endpointId"`
	CapabilityRevision       string `json:"capabilityRevision"`
	ClaimRevision            string `json:"claimRevision"`
	HealthRevision           string `json:"healthRevision"`
	FirewallBaselineRevision string `json:"firewallBaselineRevision"`
	Strategy                 string `json:"strategy"`
	PolicyRevision           string `json:"policyRevision"`
}

type PrepareRequestV1 struct {
	PlanReferenceV1
	IdempotencyKey               string `json:"idempotencyKey"`
	ExperimentalRiskAcknowledged bool   `json:"experimentalRiskAcknowledged"`
	Confirmation                 string `json:"confirmation"`
}

type ApplyRequestV1 struct {
	PlanReferenceV1
	OperationID                  string `json:"operationId"`
	OperationRevision            int    `json:"operationRevision"`
	IdempotencyKey               string `json:"idempotencyKey"`
	ExperimentalRiskAcknowledged bool   `json:"experimentalRiskAcknowledged"`
	Confirmation                 string `json:"confirmation"`
}

type RollbackRequestV1 struct {
	OperationID                  string `json:"operationId"`
	OperationRevision            int    `json:"operationRevision"`
	IdempotencyKey               string `json:"idempotencyKey"`
	ExperimentalRiskAcknowledged bool   `json:"experimentalRiskAcknowledged"`
	Confirmation                 string `json:"confirmation"`
}

type PrepareResultV1 struct {
	Operation   protectionrepository.OperationLockModel `json:"operation"`
	Joined      bool                                    `json:"joined"`
	ActualState ActualState                             `json:"actualState"`
	PlanID      string                                  `json:"planId"`
	PlanDigest  string                                  `json:"planDigest"`
	Replayed    bool                                    `json:"-"`
}

type ApplyResultV1 struct {
	Result       protectionfirewall.Result `json:"result"`
	ActualState  ActualState               `json:"actualState"`
	Experimental bool                      `json:"experimental"`
	Replayed     bool                      `json:"-"`
}

type RollbackResultV1 struct {
	Result      protectionfirewall.Result `json:"result"`
	ActualState ActualState               `json:"actualState"`
	Replayed    bool                      `json:"-"`
}

type RecoveryStatusV1 struct {
	OperationID      string `json:"operationId"`
	State            string `json:"state"`
	RecoveryRequired bool   `json:"recoveryRequired"`
	SafeNextAction   string `json:"safeNextAction"`
}

type OperationService interface {
	List(context.Context) ([]protectionrepository.OperationLockModel, error)
}

type Service interface {
	Status(context.Context, bool) (StatusV1, error)
	Preview(context.Context, PlanReferenceV1) (UDPDirectGuardPlanV1, error)
	Prepare(context.Context, string, PrepareRequestV1) (PrepareResultV1, error)
	Apply(context.Context, ApplyRequestV1) (ApplyResultV1, error)
	Rollback(context.Context, RollbackRequestV1) (RollbackResultV1, error)
	Operation(context.Context, string) (protectionrepository.OperationLockModel, error)
	Recovery(context.Context, string) (RecoveryStatusV1, error)
}

type Controller struct {
	Repository *protectionrepository.Repository
	Operations OperationService
	Firewall   *protectionfirewall.Workflow
	Baseline   *protectionfirewall.BaselineService
	Probes     *componenthealth.ProtocolProbeRegistryV1
	Transports *hostresources.InboundTransportRegistryV2
	Now        func() time.Time
}

func (c *Controller) now() time.Time {
	if c != nil && c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}

func (c *Controller) probes() *componenthealth.ProtocolProbeRegistryV1 {
	if c != nil && c.Probes != nil {
		return c.Probes
	}
	return componenthealth.DefaultProtocolProbesV1
}

func (c *Controller) transports() *hostresources.InboundTransportRegistryV2 {
	if c != nil && c.Transports != nil {
		return c.Transports
	}
	return hostresources.DefaultInboundTransportsV2
}

func (c *Controller) ready() error {
	if c == nil || c.Repository == nil || c.Baseline == nil {
		return contractError(CodeMissingCapability, errors.New("udp guard service dependencies are unavailable"))
	}
	return nil
}

func (c *Controller) Status(ctx context.Context, refresh bool) (StatusV1, error) {
	status, _, err := c.statusState(ctx, refresh)
	return status, err
}

func (c *Controller) Preview(ctx context.Context, reference PlanReferenceV1) (UDPDirectGuardPlanV1, error) {
	plan, _, err := c.resolvePlan(ctx, reference, true)
	if err != nil {
		return UDPDirectGuardPlanV1{}, err
	}
	return previewProjection(plan), nil
}

func previewProjection(plan UDPDirectGuardPlanV1) UDPDirectGuardPlanV1 {
	plan.ActualState = StateNotApplied
	plan.LatestOperationID = ""
	plan.LatestOperationRevision = 0
	plan.RecoveryRequired = false
	return plan
}

func (c *Controller) Prepare(ctx context.Context, actor string, input PrepareRequestV1) (PrepareResultV1, error) {
	if !input.ExperimentalRiskAcknowledged || strings.TrimSpace(input.IdempotencyKey) == "" || input.Confirmation != "PREPARE UDP DIRECT GUARD "+input.PlanID {
		return PrepareResultV1{}, contractError(CodeConfirmationRequired, nil)
	}
	plan, baseline, err := c.resolvePlan(ctx, input.PlanReferenceV1, true)
	if err != nil {
		return PrepareResultV1{}, err
	}
	if plan.ApplyGate == ApplyGateBlocked {
		return PrepareResultV1{}, contractError(firstBlock(plan), nil)
	}
	if err := c.applyConfigured(ctx); err != nil {
		return PrepareResultV1{}, err
	}
	candidate, err := attachPlan(baseline, plan)
	if err != nil {
		return PrepareResultV1{}, contractError(CodeRevisionDrift, err)
	}
	receipt, replay, err := c.beginReceipt(ctx, "prepare", input.IdempotencyKey, input)
	if err != nil {
		return PrepareResultV1{}, err
	}
	if replay {
		result, replayErr := decodeReceipt[PrepareResultV1](receipt)
		result.Replayed = true
		return result, replayErr
	}
	result, err := c.Firewall.Prepare(ctx, protectionfirewall.PrepareInput{
		Plan: candidate, Actor: actor, IdempotencyKey: strings.TrimSpace(input.IdempotencyKey),
		Confirmation: "PREPARE SERVER PROTECTION " + candidate.Revision,
	})
	if err != nil {
		_ = c.Repository.AmbiguousUDPGuardReceipt(ctx, receipt.ID)
		return PrepareResultV1{}, err
	}
	if err := c.saveState(ctx, plan, result.Operation.OperationID, result.Operation.Revision, StatePrepared, false, false); err != nil {
		return PrepareResultV1{}, contractError(CodeInternalFailure, err)
	}
	response := PrepareResultV1{Operation: result.Operation, Joined: result.Joined, ActualState: StatePrepared, PlanID: plan.PlanID, PlanDigest: plan.PlanDigest}
	if err := c.Repository.CompleteUDPGuardReceipt(ctx, receipt.ID, result.Operation.OperationID, result.Operation.Revision, response); err != nil {
		return PrepareResultV1{}, contractError(CodeAmbiguousResult, err)
	}
	return response, nil
}

func (c *Controller) Apply(ctx context.Context, input ApplyRequestV1) (ApplyResultV1, error) {
	if !input.ExperimentalRiskAcknowledged || input.OperationRevision < 1 || strings.TrimSpace(input.IdempotencyKey) == "" || input.Confirmation != "APPLY UDP DIRECT GUARD "+input.OperationID {
		return ApplyResultV1{}, contractError(CodeConfirmationRequired, nil)
	}
	plan, baseline, err := c.resolvePlan(ctx, input.PlanReferenceV1, true)
	if err != nil {
		return ApplyResultV1{}, err
	}
	if plan.ApplyGate == ApplyGateBlocked {
		return ApplyResultV1{}, contractError(firstBlock(plan), nil)
	}
	if err := c.applyConfigured(ctx); err != nil {
		return ApplyResultV1{}, err
	}
	candidate, err := attachPlan(baseline, plan)
	if err != nil {
		return ApplyResultV1{}, contractError(CodeRevisionDrift, err)
	}
	if _, err := c.exactOperation(ctx, input.OperationID, input.OperationRevision, ""); err != nil {
		return ApplyResultV1{}, err
	}
	transition, transitionErr := c.Repository.FirewallTransition(ctx, input.OperationID)
	requestedContributionRevision := protectionfirewall.ContributionRevision(candidate)
	if transitionErr != nil || requestedContributionRevision == "" || transition.DesiredSemanticRevision != requestedContributionRevision {
		return ApplyResultV1{}, contractError(CodeRevisionDrift, transitionErr)
	}
	receipt, replay, err := c.beginReceipt(ctx, "apply", input.IdempotencyKey, input)
	if err != nil {
		return ApplyResultV1{}, err
	}
	if replay {
		result, replayErr := decodeReceipt[ApplyResultV1](receipt)
		result.Replayed = true
		return result, replayErr
	}
	result, err := c.Firewall.Apply(ctx, protectionfirewall.ApplyInput{
		OperationID: input.OperationID, Plan: candidate, Resources: candidate.Resources,
		Confirmation: "APPLY SERVER PROTECTION " + input.OperationID,
		PostApplyHealth: func(healthCtx context.Context, fence protectionfirewall.PostMutationHealthFence) (protectionfirewall.PostMutationHealthProof, error) {
			return c.verifyPostApplyHealth(healthCtx, plan, fence)
		},
	})
	if err != nil {
		switch result.State {
		case protectionoperations.StateRolledBack, protectionoperations.StateCancelled:
			_ = c.saveState(ctx, plan, result.OperationID, result.Revision, StateNotApplied, false, false)
		case protectionoperations.StateRollbackFailed, protectionoperations.StateLockSuspect, protectionoperations.StateRollingBack:
			_ = c.saveState(ctx, plan, result.OperationID, result.Revision, StateRecoveryRequired, true, true)
		}
		_ = c.Repository.AmbiguousUDPGuardReceipt(ctx, receipt.ID)
		return ApplyResultV1{}, err
	}
	if result.State != protectionoperations.StateApplied || result.ActualStatus != "APPLIED" {
		_ = c.Repository.AmbiguousUDPGuardReceipt(ctx, receipt.ID)
		return ApplyResultV1{}, contractError(CodeAmbiguousResult, nil)
	}
	if err := c.saveState(ctx, plan, result.OperationID, result.Revision, StateAppliedExperimental, true, true); err != nil {
		return ApplyResultV1{}, contractError(CodeInternalFailure, err)
	}
	response := ApplyResultV1{Result: result, ActualState: StateAppliedExperimental, Experimental: true}
	if err := c.Repository.CompleteUDPGuardReceipt(ctx, receipt.ID, result.OperationID, result.Revision, response); err != nil {
		return ApplyResultV1{}, contractError(CodeAmbiguousResult, err)
	}
	return response, nil
}

func (c *Controller) Rollback(ctx context.Context, input RollbackRequestV1) (RollbackResultV1, error) {
	if !input.ExperimentalRiskAcknowledged || input.OperationRevision < 1 || strings.TrimSpace(input.IdempotencyKey) == "" || input.Confirmation != "ROLLBACK UDP DIRECT GUARD "+input.OperationID {
		return RollbackResultV1{}, contractError(CodeConfirmationRequired, nil)
	}
	if err := c.rollbackConfigured(ctx); err != nil {
		return RollbackResultV1{}, err
	}
	if _, err := c.exactOperation(ctx, input.OperationID, input.OperationRevision, ""); err != nil {
		return RollbackResultV1{}, err
	}
	if err := c.operationBound(ctx, input.OperationID); err != nil {
		return RollbackResultV1{}, err
	}
	receipt, replay, err := c.beginReceipt(ctx, "rollback", input.IdempotencyKey, input)
	if err != nil {
		return RollbackResultV1{}, err
	}
	if replay {
		result, replayErr := decodeReceipt[RollbackResultV1](receipt)
		result.Replayed = true
		return result, replayErr
	}
	result, err := c.Firewall.Rollback(ctx, input.OperationID, "ROLLBACK SERVER PROTECTION "+input.OperationID)
	if err != nil {
		_ = c.Repository.AmbiguousUDPGuardReceipt(ctx, receipt.ID)
		return RollbackResultV1{}, err
	}
	states, err := c.Repository.UDPGuardStates(ctx)
	if err != nil {
		return RollbackResultV1{}, contractError(CodeInternalFailure, err)
	}
	for _, state := range states {
		if state.LatestOperationID != input.OperationID {
			continue
		}
		state.ActualState = string(StateNotApplied)
		state.LatestOperationRevision = result.Revision
		state.RecoveryRequired = false
		state.OwnsActiveContribution = false
		state.RecoverableArtifact = false
		if err := c.Repository.SaveUDPGuardState(ctx, state); err != nil {
			return RollbackResultV1{}, contractError(CodeInternalFailure, err)
		}
	}
	response := RollbackResultV1{Result: result, ActualState: StateNotApplied}
	if err := c.Repository.CompleteUDPGuardReceipt(ctx, receipt.ID, result.OperationID, result.Revision, response); err != nil {
		return RollbackResultV1{}, contractError(CodeAmbiguousResult, err)
	}
	return response, nil
}

func (c *Controller) Operation(ctx context.Context, operationID string) (protectionrepository.OperationLockModel, error) {
	if c == nil || c.Operations == nil {
		return protectionrepository.OperationLockModel{}, contractError(CodeMissingCapability, nil)
	}
	items, err := c.Operations.List(ctx)
	if err != nil {
		return protectionrepository.OperationLockModel{}, contractError(CodeMissingCapability, err)
	}
	for _, item := range items {
		if item.OperationID == strings.TrimSpace(operationID) {
			return item, nil
		}
	}
	return protectionrepository.OperationLockModel{}, contractError(CodeOperationNotFound, nil)
}

func (c *Controller) Recovery(ctx context.Context, operationID string) (RecoveryStatusV1, error) {
	operation, err := c.Operation(ctx, operationID)
	if err != nil {
		return RecoveryStatusV1{}, err
	}
	return RecoveryStatusV1{
		OperationID: operation.OperationID, State: operation.State,
		RecoveryRequired: operation.State == protectionoperations.StateRollbackFailed || operation.State == protectionoperations.StateLockSuspect,
		SafeNextAction:   "INSPECT_RECOVERY_BUNDLE",
	}, nil
}

func (c *Controller) statusState(ctx context.Context, refresh bool) (StatusV1, protectionfirewall.BaselineState, error) {
	if err := c.ready(); err != nil {
		return StatusV1{}, protectionfirewall.BaselineState{}, err
	}
	now := c.now()
	facts, err := c.transports().Facts(ctx, now)
	if err != nil {
		return StatusV1{}, protectionfirewall.BaselineState{}, err
	}
	baseline, err := c.Baseline.Snapshot(ctx, refresh, nil)
	if err != nil {
		return StatusV1{}, protectionfirewall.BaselineState{}, err
	}
	authority, err := c.Repository.FirewallAuthority(ctx)
	if err != nil {
		return StatusV1{}, protectionfirewall.BaselineState{}, err
	}
	baselineAuthorityActive, err := protectionfirewall.BaselineAuthorityMatchesPlan(authority, baseline.Plan)
	if err != nil {
		return StatusV1{}, protectionfirewall.BaselineState{}, err
	}
	probeCapabilities := map[string]componenthealth.ProtocolProbeCapabilityV1{}
	for _, fact := range facts {
		for _, node := range baseline.Graph.Nodes {
			if node.ResourceID != fact.ResourceID {
				continue
			}
			for _, observed := range node.ObservedClaims {
				if observed.Key.Network != hostresources.NetworkUDP {
					continue
				}
				target := componenthealth.ProtocolProbeTargetV1{
					ResourceID: fact.ResourceID, EndpointID: observed.ID, ProtocolClass: fact.StrategyClass,
					RuntimeRevision: fact.RuntimeGenerationRevision, CapabilityRevision: fact.Revision,
					ConfigurationRevision: fact.ConfigurationRevision, SocketRevision: observed.OwnerObservationRevision,
					AddressFamily: observed.Key.AddressFamily, ConfiguredBind: observed.Key.BindAddress, Port: observed.Key.Port,
				}
				probeCapabilities[fact.ResourceID+"|"+observed.ID] = c.probes().Capability(ctx, target)
			}
		}
	}
	status := BuildStatus(PlannerInput{
		Capabilities: facts, Graph: baseline.Graph, ManagementExclusionRevision: protectionfirewall.BaselineConfigurationRevision(baseline.Management),
		TrustedExclusionRevision: hostresources.Revision(baseline.Trusted), FirewallBaselineRevision: baseline.Plan.Revision,
		FirewallAuthorityActive: baselineAuthorityActive, ProbeCapabilities: probeCapabilities, Now: now,
	})
	persisted, err := c.Repository.UDPGuardStates(ctx)
	if err != nil {
		return StatusV1{}, protectionfirewall.BaselineState{}, err
	}
	operations := map[string]protectionrepository.OperationLockModel{}
	transitions := map[string]protectionrepository.FirewallContributionTransitionModel{}
	activeContributions := map[string]string{}
	for _, contribution := range authority.Contributions {
		activeContributions[contribution.ContributionID] = contribution.SemanticRevision
	}
	transitionRows, err := c.Repository.FirewallTransitions(ctx)
	if err != nil {
		return StatusV1{}, protectionfirewall.BaselineState{}, err
	}
	for _, transition := range transitionRows {
		transitions[transition.OperationID] = transition
	}
	if c.Operations != nil {
		items, listErr := c.Operations.List(ctx)
		if listErr != nil {
			return StatusV1{}, protectionfirewall.BaselineState{}, listErr
		}
		for _, item := range items {
			operations[item.OperationID] = item
		}
	}
	for index := range status.Plans {
		for _, state := range persisted {
			if state.ResourceID != status.Plans[index].ResourceID || state.EndpointID != status.Plans[index].EndpointID {
				continue
			}
			status.Plans[index].LatestOperationID = state.LatestOperationID
			status.Plans[index].LatestOperationRevision = state.LatestOperationRevision
			operation, exists := operations[state.LatestOperationID]
			if exists {
				status.Plans[index].LatestOperationRevision = operation.Revision
			}
			proofExact, proofFresh := healthProofState(state, transitions[state.LatestOperationID], authority, now)
			projectActualState(&status.Plans[index], state, operation, exists, activeContributions[state.ContributionID] == state.ContributionRevision, proofExact, proofFresh)
		}
	}
	projectOrphanedAuthority(&status, persisted, authority)
	projectCapabilityStates(&status)
	return status, baseline, nil
}
