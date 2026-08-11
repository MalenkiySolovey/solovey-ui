package localproxy

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	componenthealth "github.com/MalenkiySolovey/solovey-ui/componenthost/health"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	protectionoperations "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
)

type operationManager interface {
	Acquire(context.Context, protectionoperations.AcquireRequest) (protectionoperations.AcquireResult, error)
	Transition(context.Context, string, int, string) (protectionrepository.OperationLockModel, error)
	BeginRollback(context.Context, string, int) (protectionrepository.OperationLockModel, error)
}

type Service interface {
	Status(context.Context, bool) (StatusV1, error)
	Preview(context.Context, PlanReferenceV1) (PlanV1, error)
	Prepare(context.Context, string, PrepareRequestV1) (ResultV1, error)
	Apply(context.Context, ApplyRequestV1) (ResultV1, error)
	Disable(context.Context, DisableRequestV1) (ResultV1, error)
	Operation(context.Context, string) (protectionrepository.OperationLockModel, error)
	Recovery(context.Context, string) (RecoveryStatusV1, error)
}

type Controller struct {
	Repository *protectionrepository.Repository
	Operations operationManager
	Providers  *hostresources.LocalProxyRegistryV1
	Probes     *componenthealth.LocalProxyProbeRegistryV1
	Now        func() time.Time
}

func (c *Controller) ready() error {
	if c == nil || c.Repository == nil || c.Operations == nil || c.Providers == nil || c.Probes == nil {
		return serviceError(CodeInternalFailure)
	}
	return nil
}

func (c *Controller) currentTime() time.Time {
	if c != nil && c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}

func (c *Controller) Prepare(ctx context.Context, actor string, input PrepareRequestV1) (ResultV1, error) {
	if err := c.ready(); err != nil {
		return ResultV1{}, err
	}
	if !validateMutationToken(actor, 256) || !validateMutationToken(input.IdempotencyKey, 96) ||
		!input.Acknowledged || !safeID(input.PlanID, 96) || !digest(input.PlanDigest) {
		if !input.Acknowledged {
			return ResultV1{}, serviceError(CodeAcknowledgementRequired)
		}
		return ResultV1{}, serviceError(CodeMalformedInput)
	}
	if input.Confirmation != "PREPARE LOCAL PROXY "+input.PlanID {
		return ResultV1{}, serviceError(CodeConfirmationRequired)
	}
	requestDigest := hostresources.Revision(input)
	if result, replay, err := c.replayCompleted(ctx, "prepare", input.IdempotencyKey, requestDigest); err != nil || replay {
		return result, err
	}
	plan, err := c.Preview(ctx, PlanReferenceV1{ResourceID: input.ResourceID, EndpointID: input.EndpointID, FactRevision: input.FactRevision})
	if err != nil {
		return ResultV1{}, err
	}
	if plan.PlanID != input.PlanID || plan.PlanDigest != input.PlanDigest || plan.ApplyGate != ApplyGateExperimentalAck ||
		plan.ExactReference == nil || len(plan.BlockCodes) != 0 {
		return ResultV1{}, serviceError(CodeRevisionDrift)
	}
	receipt, replay, err := c.Repository.BeginLocalProxyReceipt(ctx, "prepare", input.IdempotencyKey, requestDigest)
	if err != nil {
		return ResultV1{}, err
	}
	if replay {
		return replayLocalProxyResult(receipt)
	}
	complete := false
	defer func() {
		if !complete {
			_ = c.Repository.AmbiguousLocalProxyReceipt(context.WithoutCancel(ctx), receipt.ID)
		}
	}()
	acquired, err := c.Operations.Acquire(ctx, protectionoperations.AcquireRequest{
		Kind: protectionoperations.KindLocalProxy, ResourceID: plan.ResourceID, Protocol: "local_proxy",
		IdempotencyKey: "local-proxy:prepare:" + input.IdempotencyKey, PlanRevision: plan.PlanDigest,
		HelperRevision: plan.FactRevision, Actor: actor,
	})
	if err != nil {
		return ResultV1{}, errors.Join(serviceError(CodeOperationConflict), err)
	}
	operation := acquired.Operation
	if acquired.Joined {
		return ResultV1{}, serviceError(CodeStateInvalid)
	}
	provider, ok := c.Providers.Provider(plan.ExactReference.ProviderID)
	if !ok {
		_, _ = c.Operations.Transition(context.WithoutCancel(ctx), operation.OperationID, operation.Revision, protectionoperations.StateCancelled)
		return ResultV1{}, serviceError(CodeProviderUnavailable)
	}
	lease, err := provider.AcquireLocalProxyGuardLease(ctx, hostresources.AcquireLocalProxyGuardLeaseRequestV1{
		RequestID: hostresources.Revision(struct{ Operation, Plan string }{operation.OperationID, plan.PlanDigest}),
		HolderID:  operation.OperationID, Purpose: hostresources.LocalProxyGuardPurposeV1,
		ExactReference: *plan.ExactReference, FreshnessSeconds: uint32(hostresources.MaxLocalProxyLeaseFreshnessV1 / time.Second),
	})
	if err != nil {
		_, _ = c.Operations.Transition(context.WithoutCancel(ctx), operation.OperationID, operation.Revision, protectionoperations.StateCancelled)
		return ResultV1{}, errors.Join(serviceError(CodeOperationConflict), err)
	}
	if err := c.persistState(ctx, plan, operation, lease, StatePrepared, "", nil, false); err != nil {
		_, _ = c.Operations.Transition(context.WithoutCancel(ctx), operation.OperationID, operation.Revision, protectionoperations.StateReconcileRequired)
		return ResultV1{}, errors.Join(serviceError(CodeRecoveryRequired), err)
	}
	result := resultFor(operation, plan, lease, StatePrepared, nil, false)
	if err := c.Repository.CompleteLocalProxyReceipt(ctx, receipt.ID, operation.OperationID, operation.Revision, result); err != nil {
		return ResultV1{}, err
	}
	complete = true
	return result, nil
}

func (c *Controller) Apply(ctx context.Context, input ApplyRequestV1) (ResultV1, error) {
	if err := c.ready(); err != nil {
		return ResultV1{}, err
	}
	if !validateMutationToken(input.OperationID, 128) || input.OperationRevision <= 0 ||
		!validateMutationToken(input.IdempotencyKey, 96) || !input.Acknowledged ||
		!safeID(input.PlanID, 96) || !digest(input.PlanDigest) || !digest(input.FactRevision) {
		if !input.Acknowledged {
			return ResultV1{}, serviceError(CodeAcknowledgementRequired)
		}
		return ResultV1{}, serviceError(CodeMalformedInput)
	}
	if input.Confirmation != "APPLY LOCAL PROXY "+input.OperationID {
		return ResultV1{}, serviceError(CodeConfirmationRequired)
	}
	requestDigest := hostresources.Revision(input)
	if result, replay, err := c.replayCompleted(ctx, "apply", input.IdempotencyKey, requestDigest); err != nil || replay {
		return result, err
	}
	operation, err := c.Repository.OperationByID(ctx, input.OperationID)
	if err != nil {
		return ResultV1{}, serviceError(CodeOperationNotFound)
	}
	if operation.Kind != protectionoperations.KindLocalProxy || operation.State != protectionoperations.StatePrepared ||
		operation.Revision != input.OperationRevision {
		return ResultV1{}, serviceError(CodeRevisionDrift)
	}
	state, err := c.Repository.LocalProxyStateByOperation(ctx, operation.OperationID)
	if err != nil {
		return ResultV1{}, serviceError(CodeStateInvalid)
	}
	plan, lease, err := c.resolvePreparedPlan(ctx, state)
	if err != nil {
		return ResultV1{}, err
	}
	if plan.PlanID != input.PlanID || plan.PlanDigest != input.PlanDigest || plan.FactRevision != input.FactRevision ||
		operation.PlanRevision != plan.PlanDigest || state.ActualState != string(StatePrepared) ||
		lease.State != hostresources.EndpointLeaseReserved {
		return ResultV1{}, serviceError(CodeRevisionDrift)
	}
	receipt, replay, err := c.Repository.BeginLocalProxyReceipt(ctx, "apply", input.IdempotencyKey, requestDigest)
	if err != nil {
		return ResultV1{}, err
	}
	if replay {
		return replayLocalProxyResult(receipt)
	}
	complete := false
	defer func() {
		if !complete {
			_ = c.Repository.AmbiguousLocalProxyReceipt(context.WithoutCancel(ctx), receipt.ID)
		}
	}()
	applying, err := c.Operations.Transition(ctx, operation.OperationID, operation.Revision, protectionoperations.StateApplying)
	if err != nil {
		return ResultV1{}, err
	}
	marker := hostresources.Revision(struct {
		Schema, Operation, Plan, Fact, Lease string
		Revision                             int
		Boundary                             int64
	}{
		"solovey-ui/local-proxy-apply-marker/v1", applying.OperationID, plan.PlanDigest,
		plan.FactRevision, lease.LeaseRevision, applying.Revision, c.currentTime().UnixNano(),
	})
	if err := c.persistState(ctx, plan, applying, lease, StateApplying, marker, nil, false); err != nil {
		_, _ = c.Operations.Transition(context.WithoutCancel(ctx), applying.OperationID, applying.Revision, protectionoperations.StateReconcileRequired)
		return ResultV1{}, errors.Join(serviceError(CodeRecoveryRequired), err)
	}
	provider, ok := c.Providers.Provider(plan.ExactReference.ProviderID)
	if !ok {
		return c.reconcileAfterAmbiguity(ctx, plan, applying, lease, marker, serviceError(CodeProviderUnavailable))
	}
	fenced, err := provider.FenceLocalProxyGuardLease(ctx, hostresources.MutateLocalProxyGuardLeaseRequestV1{
		RequestID: hostresources.Revision(struct{ Operation, Marker string }{applying.OperationID, marker}),
		LeaseID:   lease.LeaseID, ExpectedRevision: lease.LeaseRevision,
	})
	if err != nil {
		return c.reconcileAfterAmbiguity(ctx, plan, applying, lease, marker, errors.Join(serviceError(CodeLeaseDrift), err))
	}
	if err := c.persistState(ctx, plan, applying, fenced, StateApplying, marker, nil, false); err != nil {
		return c.reconcileAfterAmbiguity(ctx, plan, applying, fenced, marker, err)
	}
	healthing, err := c.Operations.Transition(ctx, applying.OperationID, applying.Revision, protectionoperations.StateHealth)
	if err != nil {
		return c.reconcileAfterAmbiguity(ctx, plan, applying, fenced, marker, err)
	}
	if err := c.persistState(ctx, plan, healthing, fenced, StateHealth, marker, nil, false); err != nil {
		return c.reconcileAfterAmbiguity(ctx, plan, healthing, fenced, marker, err)
	}
	boundary := time.Now().UTC().UnixNano()
	health, healthErr := c.probeAll(ctx, plan, healthing, fenced, marker, boundary)
	if healthErr != nil {
		result, rollbackErr := c.rollbackFailedApply(ctx, plan, healthing, fenced, marker, health)
		return result, errors.Join(serviceError(CodeHealthFailed), healthErr, rollbackErr)
	}
	if _, err := c.Providers.ResolveV1(ctx, *plan.ExactReference, c.currentTime()); err != nil {
		result, rollbackErr := c.rollbackFailedApply(ctx, plan, healthing, fenced, marker, health)
		return result, errors.Join(serviceError(CodeRuntimeDrift), err, rollbackErr)
	}
	active, err := provider.ActivateLocalProxyGuardLease(ctx, hostresources.MutateLocalProxyGuardLeaseRequestV1{
		RequestID: hostresources.Revision(struct{ Operation, Health string }{healthing.OperationID, allHealthRevision(health)}),
		LeaseID:   fenced.LeaseID, ExpectedRevision: fenced.LeaseRevision,
	})
	if err != nil {
		return c.reconcileAfterAmbiguity(ctx, plan, healthing, fenced, marker, errors.Join(serviceError(CodeLeaseDrift), err))
	}
	if err := c.persistState(ctx, plan, healthing, active, StateAppliedExperimental, marker, health, false); err != nil {
		return c.reconcileAfterAmbiguity(ctx, plan, healthing, active, marker, err)
	}
	applied, err := c.Operations.Transition(ctx, healthing.OperationID, healthing.Revision, protectionoperations.StateApplied)
	if err != nil {
		return c.reconcileAfterAmbiguity(ctx, plan, healthing, active, marker, err)
	}
	if err := c.persistState(ctx, plan, applied, active, StateAppliedExperimental, marker, health, false); err != nil {
		return c.reconcileAfterAmbiguity(ctx, plan, applied, active, marker, err)
	}
	result := resultFor(applied, plan, active, StateAppliedExperimental, health, false)
	if err := c.Repository.CompleteLocalProxyReceipt(ctx, receipt.ID, applied.OperationID, applied.Revision, result); err != nil {
		return ResultV1{}, err
	}
	complete = true
	return result, nil
}

func (c *Controller) Disable(ctx context.Context, input DisableRequestV1) (ResultV1, error) {
	if err := c.ready(); err != nil {
		return ResultV1{}, err
	}
	if !validateMutationToken(input.OperationID, 128) || input.OperationRevision <= 0 ||
		!validateMutationToken(input.IdempotencyKey, 96) {
		return ResultV1{}, serviceError(CodeMalformedInput)
	}
	disableConfirmation := "DISABLE LOCAL PROXY " + input.OperationID
	rollbackConfirmation := "ROLLBACK LOCAL PROXY " + input.OperationID
	if input.Confirmation != disableConfirmation && input.Confirmation != rollbackConfirmation {
		return ResultV1{}, serviceError(CodeConfirmationRequired)
	}
	requestDigest := hostresources.Revision(input)
	if result, replay, err := c.replayCompleted(ctx, "disable", input.IdempotencyKey, requestDigest); err != nil || replay {
		return result, err
	}
	operation, err := c.Repository.OperationByID(ctx, input.OperationID)
	if err != nil || operation.Kind != protectionoperations.KindLocalProxy || operation.State != protectionoperations.StateApplied ||
		operation.Revision != input.OperationRevision {
		return ResultV1{}, serviceError(CodeRevisionDrift)
	}
	state, err := c.Repository.LocalProxyStateByOperation(ctx, operation.OperationID)
	if err != nil {
		return ResultV1{}, serviceError(CodeStateInvalid)
	}
	plan, lease, err := c.resolveStoredPlanAndLease(ctx, state, false, false)
	if err != nil || lease.State != hostresources.EndpointLeaseActive {
		return ResultV1{}, errors.Join(serviceError(CodeLeaseDrift), err)
	}
	receipt, replay, err := c.Repository.BeginLocalProxyReceipt(ctx, "disable", input.IdempotencyKey, requestDigest)
	if err != nil {
		return ResultV1{}, err
	}
	if replay {
		return replayLocalProxyResult(receipt)
	}
	complete := false
	defer func() {
		if !complete {
			_ = c.Repository.AmbiguousLocalProxyReceipt(context.WithoutCancel(ctx), receipt.ID)
		}
	}()
	rolling, err := c.Operations.BeginRollback(ctx, operation.OperationID, operation.Revision)
	if err != nil {
		return ResultV1{}, err
	}
	if err := c.persistState(ctx, plan, rolling, lease, StateRollingBack, state.MarkerRevision, nil, false); err != nil {
		_, _ = c.Operations.Transition(context.WithoutCancel(ctx), rolling.OperationID, rolling.Revision, protectionoperations.StateReconcileRequired)
		return ResultV1{}, errors.Join(serviceError(CodeRecoveryRequired), err)
	}
	provider, ok := c.Providers.Provider(plan.ExactReference.ProviderID)
	if !ok {
		return c.reconcileAfterAmbiguity(ctx, plan, rolling, lease, state.MarkerRevision, serviceError(CodeProviderUnavailable))
	}
	released, err := provider.ReleaseLocalProxyGuardLease(ctx, hostresources.ReleaseLocalProxyGuardLeaseRequestV1{
		RequestID: hostresources.Revision(struct{ Operation, Action string }{rolling.OperationID, "disable"}),
		LeaseID:   lease.LeaseID, ExpectedRevision: lease.LeaseRevision,
	})
	if err != nil {
		return c.reconcileAfterAmbiguity(ctx, plan, rolling, lease, state.MarkerRevision, err)
	}
	if err := c.persistState(ctx, plan, rolling, released, StateNotApplied, "", nil, false); err != nil {
		return c.reconcileAfterAmbiguity(ctx, plan, rolling, released, state.MarkerRevision, err)
	}
	rolledBack, err := c.Operations.Transition(ctx, rolling.OperationID, rolling.Revision, protectionoperations.StateRolledBack)
	if err != nil {
		return ResultV1{}, err
	}
	result := resultFor(rolledBack, plan, released, StateNotApplied, nil, false)
	if err := c.Repository.CompleteLocalProxyReceipt(ctx, receipt.ID, rolledBack.OperationID, rolledBack.Revision, result); err != nil {
		return ResultV1{}, err
	}
	complete = true
	return result, nil
}

func (c *Controller) probeAll(ctx context.Context, plan PlanV1, operation protectionrepository.OperationLockModel, lease hostresources.LocalProxyGuardLeaseV1, marker string, boundary int64) ([]componenthealth.LocalProxyProbeObservationV1, error) {
	if plan.ExactReference == nil || len(plan.ExactReference.Protocols) == 0 {
		return nil, serviceError(CodeMissingHealth)
	}
	if lease.Validate() != nil || lease.ExpiresAt <= c.currentTime().Unix() {
		return nil, serviceError(CodeLeaseDrift)
	}
	result := make([]componenthealth.LocalProxyProbeObservationV1, 0, len(plan.ExactReference.Protocols))
	seen := map[hostresources.LocalProxyProtocolV1]bool{}
	for _, protocol := range plan.ExactReference.Protocols {
		target := componenthealth.LocalProxyProbeTargetV1{
			ProviderID: plan.ExactReference.ProviderID, ResourceID: plan.ResourceID, EndpointID: plan.EndpointID,
			Protocol: protocol, ConfigurationRevision: plan.ExactReference.ConfigurationRevision,
			RuntimeRevision: plan.ExactReference.EffectiveRuntimeRevision, FactRevision: plan.FactRevision,
			ListenerObservationRevision: plan.ExactReference.ListenerObservationRevision,
			AuthenticationRevision:      plan.ExactReference.AuthenticationRevision, TLSRevision: plan.ExactReference.TLSRevision,
			SystemProxyRevision: plan.ExactReference.SystemProxyRevision, LeaseID: lease.LeaseID,
			LeaseRevision: lease.LeaseRevision, LeaseState: lease.State, OperationID: operation.OperationID,
			OperationRevision: operation.Revision, PlanRevision: plan.PlanDigest, MarkerRevision: marker,
		}
		capability := c.Probes.Capability(ctx, target)
		if !capability.Available {
			return result, serviceError(CodeMissingHealth)
		}
		observation, err := c.Probes.ProbeFresh(ctx, componenthealth.LocalProxyProbeRequestV1{
			Target: target, ProviderInstance: capability.ProviderInstance, MinimumGeneration: 1,
			NotBeforeUnixNano: boundary,
		})
		if err != nil || seen[protocol] || observation.Protocol != protocol {
			return result, errors.Join(serviceError(CodeHealthFailed), err)
		}
		if plan.Fact.Authentication == hostresources.LocalProxyAuthenticationPresent &&
			(!observation.MissingAuthenticationDenied || !observation.InvalidAuthenticationDenied) {
			return result, serviceError(CodeHealthFailed)
		}
		seen[protocol] = true
		result = append(result, observation)
	}
	if len(seen) != len(plan.ExactReference.Protocols) {
		return result, serviceError(CodeHealthFailed)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Protocol < result[j].Protocol })
	return result, nil
}

func (c *Controller) rollbackFailedApply(ctx context.Context, plan PlanV1, operation protectionrepository.OperationLockModel, lease hostresources.LocalProxyGuardLeaseV1, marker string, health []componenthealth.LocalProxyProbeObservationV1) (ResultV1, error) {
	failed, err := c.Operations.Transition(context.WithoutCancel(ctx), operation.OperationID, operation.Revision, protectionoperations.StateHealthFailed)
	if err != nil {
		return c.reconcileAfterAmbiguity(ctx, plan, operation, lease, marker, err)
	}
	rolling, err := c.Operations.Transition(context.WithoutCancel(ctx), failed.OperationID, failed.Revision, protectionoperations.StateRollingBack)
	if err != nil {
		return c.reconcileAfterAmbiguity(ctx, plan, failed, lease, marker, err)
	}
	_ = c.persistState(context.WithoutCancel(ctx), plan, rolling, lease, StateRollingBack, marker, health, false)
	provider, ok := c.Providers.Provider(plan.ExactReference.ProviderID)
	if !ok {
		return c.reconcileAfterAmbiguity(ctx, plan, rolling, lease, marker, serviceError(CodeProviderUnavailable))
	}
	released, err := provider.ReleaseLocalProxyGuardLease(context.WithoutCancel(ctx), hostresources.ReleaseLocalProxyGuardLeaseRequestV1{
		RequestID: hostresources.Revision(struct{ Operation, Action string }{rolling.OperationID, "failed_apply"}),
		LeaseID:   lease.LeaseID, ExpectedRevision: lease.LeaseRevision,
	})
	if err != nil {
		return c.reconcileAfterAmbiguity(ctx, plan, rolling, lease, marker, err)
	}
	if err := c.persistState(context.WithoutCancel(ctx), plan, rolling, released, StateNotApplied, "", health, false); err != nil {
		return c.reconcileAfterAmbiguity(ctx, plan, rolling, released, marker, err)
	}
	rolled, err := c.Operations.Transition(context.WithoutCancel(ctx), rolling.OperationID, rolling.Revision, protectionoperations.StateRolledBack)
	return resultFor(rolled, plan, released, StateNotApplied, health, false), err
}

func (c *Controller) reconcileAfterAmbiguity(ctx context.Context, plan PlanV1, operation protectionrepository.OperationLockModel, lease hostresources.LocalProxyGuardLeaseV1, marker string, cause error) (ResultV1, error) {
	_ = c.persistState(context.WithoutCancel(ctx), plan, operation, lease, StateRecoveryRequired, marker, nil, true)
	_, transitionErr := c.Operations.Transition(context.WithoutCancel(ctx), operation.OperationID, operation.Revision, protectionoperations.StateReconcileRequired)
	return resultFor(operation, plan, lease, StateRecoveryRequired, nil, true),
		errors.Join(serviceError(CodeRecoveryRequired), cause, transitionErr)
}

func (c *Controller) persistState(ctx context.Context, plan PlanV1, operation protectionrepository.OperationLockModel, lease hostresources.LocalProxyGuardLeaseV1, actual ActualState, marker string, health []componenthealth.LocalProxyProbeObservationV1, recovery bool) error {
	planJSON, err := json.Marshal(plan)
	if err != nil || len(planJSON) > 128<<10 {
		return serviceError(CodeStateInvalid)
	}
	if health == nil {
		health = []componenthealth.LocalProxyProbeObservationV1{}
	}
	healthJSON, err := json.Marshal(health)
	if err != nil || len(healthJSON) > 128<<10 {
		return serviceError(CodeStateInvalid)
	}
	expires := int64(0)
	for _, observation := range health {
		if expires == 0 || observation.ExpiresUnixNano < expires {
			expires = observation.ExpiresUnixNano
		}
	}
	referenceRevision := ""
	if plan.ExactReference != nil {
		referenceRevision = plan.ExactReference.CanonicalReferenceRevision
	}
	guarding := lease.LeaseID != "" && lease.State != hostresources.EndpointLeaseReleased
	return c.Repository.SaveLocalProxyState(ctx, protectionrepository.LocalProxyStateV1Model{
		ResourceID: plan.ResourceID, EndpointID: plan.EndpointID, Schema: StateSchemaV1,
		ActualState: string(actual), ApplyGate: string(plan.ApplyGate), PlanID: plan.PlanID, PlanDigest: plan.PlanDigest,
		PlanJSON: planJSON, FactRevision: plan.FactRevision, ReferenceRevision: referenceRevision,
		LeaseID: lease.LeaseID, LeaseRevision: lease.LeaseRevision, LeaseState: string(lease.State),
		LeaseRenewedAt: lease.RenewedAt, LeaseExpiresAt: lease.ExpiresAt,
		LatestOperationID: operation.OperationID, LatestOperationRevision: operation.Revision,
		MarkerRevision: marker, HealthJSON: healthJSON, HealthRevision: allHealthRevision(health),
		HealthExpiresUnixNano: expires, GuardingProviderLease: guarding, RecoveryRequired: recovery,
	})
}

func (c *Controller) resolveStoredPlanAndLease(ctx context.Context, model protectionrepository.LocalProxyStateV1Model, requireFreshPlan, allowProviderDrift bool) (PlanV1, hostresources.LocalProxyGuardLeaseV1, error) {
	var plan PlanV1
	if json.Unmarshal(model.PlanJSON, &plan) != nil || plan.Validate(func() time.Time {
		if requireFreshPlan {
			return c.currentTime()
		}
		return time.Time{}
	}()) != nil || plan.ExactReference == nil {
		return PlanV1{}, hostresources.LocalProxyGuardLeaseV1{}, serviceError(CodeStateInvalid)
	}
	provider, ok := c.Providers.Provider(plan.ExactReference.ProviderID)
	if !ok {
		return PlanV1{}, hostresources.LocalProxyGuardLeaseV1{}, serviceError(CodeProviderUnavailable)
	}
	lease, err := provider.GetLocalProxyGuardLease(ctx, hostresources.GetLocalProxyGuardLeaseRequestV1{LeaseID: model.LeaseID})
	if err != nil {
		return PlanV1{}, hostresources.LocalProxyGuardLeaseV1{}, err
	}
	if err := validateStoredLocalProxyBinding(plan, model, lease, allowProviderDrift); err != nil {
		return PlanV1{}, hostresources.LocalProxyGuardLeaseV1{}, err
	}
	return plan, lease, nil
}

func validateStoredLocalProxyBinding(plan PlanV1, model protectionrepository.LocalProxyStateV1Model, lease hostresources.LocalProxyGuardLeaseV1, allowProviderDrift bool) error {
	if plan.ExactReference == nil || lease.Validate() != nil ||
		model.Schema != StateSchemaV1 || model.ResourceID != plan.ResourceID || model.EndpointID != plan.EndpointID ||
		model.PlanID != plan.PlanID || model.PlanDigest != plan.PlanDigest || model.FactRevision != plan.FactRevision ||
		model.ReferenceRevision != plan.ExactReference.CanonicalReferenceRevision || model.ApplyGate != string(plan.ApplyGate) ||
		model.LeaseID == "" || model.LeaseID != lease.LeaseID || model.LatestOperationID == "" ||
		lease.HolderID != model.LatestOperationID || lease.AuthorityProviderID != plan.ExactReference.ProviderID ||
		lease.ExactReference.CanonicalReferenceRevision != plan.ExactReference.CanonicalReferenceRevision {
		return serviceError(CodeStateInvalid)
	}
	if !allowProviderDrift &&
		(model.LeaseRevision != lease.LeaseRevision || model.LeaseState != string(lease.State) ||
			model.LeaseRenewedAt != lease.RenewedAt || model.LeaseExpiresAt != lease.ExpiresAt) {
		return serviceError(CodeLeaseDrift)
	}
	return nil
}

func (c *Controller) Operation(ctx context.Context, operationID string) (protectionrepository.OperationLockModel, error) {
	if err := c.ready(); err != nil {
		return protectionrepository.OperationLockModel{}, err
	}
	value, err := c.Repository.OperationByID(ctx, strings.TrimSpace(operationID))
	if err != nil || value.Kind != protectionoperations.KindLocalProxy {
		return protectionrepository.OperationLockModel{}, serviceError(CodeOperationNotFound)
	}
	return value, nil
}

func (c *Controller) Recovery(ctx context.Context, operationID string) (RecoveryStatusV1, error) {
	if err := c.ready(); err != nil {
		return RecoveryStatusV1{}, err
	}
	state, err := c.Repository.LocalProxyStateByOperation(ctx, strings.TrimSpace(operationID))
	if err != nil {
		return RecoveryStatusV1{}, serviceError(CodeOperationNotFound)
	}
	next := "NONE"
	if state.GuardingProviderLease {
		next = "DISABLE_OR_RECONCILE"
	}
	if state.RecoveryRequired {
		next = "RECONCILE_PROVIDER_AUTHORITY"
	}
	return RecoveryStatusV1{
		OperationID: operationID, ActualState: ActualState(state.ActualState),
		ProviderGuarded: state.GuardingProviderLease, RecoveryRequired: state.RecoveryRequired,
		SafeNextAction: next, ReasonCodes: func() []string {
			if state.RecoveryRequired {
				return []string{CodeRecoveryRequired}
			}
			return []string{}
		}(),
	}, nil
}

func resultFor(operation protectionrepository.OperationLockModel, plan PlanV1, lease hostresources.LocalProxyGuardLeaseV1, actual ActualState, health []componenthealth.LocalProxyProbeObservationV1, recovery bool) ResultV1 {
	return ResultV1{
		OperationID: operation.OperationID, OperationRevision: operation.Revision, OperationState: operation.State,
		PlanID: plan.PlanID, PlanDigest: plan.PlanDigest, ActualState: actual, Lease: leaseView(lease),
		Health: health, RecoveryRequired: recovery, WarningCodes: append([]string(nil), plan.WarningCodes...),
	}
}

func replayLocalProxyResult(receipt protectionrepository.LocalProxyIdempotencyV1Model) (ResultV1, error) {
	var result ResultV1
	if json.Unmarshal(receipt.SemanticResponseJSON, &result) != nil {
		return ResultV1{}, serviceError(CodeStateInvalid)
	}
	result.Replayed = true
	return result, nil
}

func (c *Controller) replayCompleted(ctx context.Context, action, key, requestDigest string) (ResultV1, bool, error) {
	receipt, replay, err := c.Repository.ReplayLocalProxyReceipt(ctx, action, key, requestDigest)
	if err != nil || !replay {
		return ResultV1{}, false, err
	}
	result, err := replayLocalProxyResult(receipt)
	return result, true, err
}

func (c *Controller) RenewActive(ctx context.Context) error {
	if err := c.ready(); err != nil {
		return err
	}
	states, err := c.Repository.LocalProxyStates(ctx)
	if err != nil {
		return err
	}
	now := c.currentTime()
	for _, state := range states {
		if state.ActualState != string(StateAppliedExperimental) || !state.GuardingProviderLease {
			continue
		}
		plan, lease, err := c.resolveStoredPlanAndLease(ctx, state, false, false)
		if err != nil {
			_ = c.markRenewalRecovery(ctx, state)
			continue
		}
		if lease.State != hostresources.EndpointLeaseActive {
			_ = c.markRenewalRecovery(ctx, state)
			continue
		}
		if lease.ExpiresAt > now.Add(5*time.Minute).Unix() {
			continue
		}
		provider, ok := c.Providers.Provider(plan.ExactReference.ProviderID)
		if !ok {
			_ = c.markRenewalRecovery(ctx, state)
			continue
		}
		renewed, err := provider.RenewLocalProxyGuardLease(ctx, hostresources.MutateLocalProxyGuardLeaseRequestV1{
			RequestID: hostresources.Revision(struct {
				Lease string
				At    int64
			}{lease.LeaseID, now.Unix()}),
			LeaseID: lease.LeaseID, ExpectedRevision: lease.LeaseRevision,
			FreshnessSeconds: uint32(hostresources.MaxLocalProxyLeaseFreshnessV1 / time.Second),
		})
		if err != nil {
			_ = c.markRenewalRecovery(ctx, state)
			continue
		}
		operation, operationErr := c.Repository.OperationByID(ctx, state.LatestOperationID)
		if operationErr != nil || c.persistState(ctx, plan, operation, renewed, StateAppliedExperimental, state.MarkerRevision, decodeHealth(state.HealthJSON), false) != nil {
			_ = c.markRenewalRecovery(ctx, state)
		}
	}
	return nil
}

func (c *Controller) markRenewalRecovery(ctx context.Context, state protectionrepository.LocalProxyStateV1Model) error {
	current, err := c.Repository.LocalProxyStateByOperation(ctx, state.LatestOperationID)
	if err != nil {
		return err
	}
	if current.ActualState != string(StateAppliedExperimental) || !current.GuardingProviderLease ||
		current.LeaseID != state.LeaseID || current.LeaseRevision != state.LeaseRevision ||
		current.LeaseState != state.LeaseState || current.LatestOperationRevision != state.LatestOperationRevision {
		// A concurrent disable, reconcile, or successful renewal advanced the
		// mirror. Never overwrite that newer authority projection with a
		// recovery decision derived from this stale scan.
		return nil
	}
	current.ActualState, current.RecoveryRequired = string(StateRecoveryRequired), true
	return c.Repository.SaveLocalProxyState(ctx, current)
}

func decodeHealth(content []byte) []componenthealth.LocalProxyProbeObservationV1 {
	var result []componenthealth.LocalProxyProbeObservationV1
	_ = json.Unmarshal(content, &result)
	return result
}

func RunLeaseRenewer(ctx context.Context, controller *Controller) {
	if controller == nil {
		return
	}
	_ = controller.RenewActive(ctx)
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = controller.RenewActive(ctx)
		}
	}
}

var _ Service = (*Controller)(nil)
