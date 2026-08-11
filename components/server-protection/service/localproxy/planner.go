package localproxy

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	hostsurface "github.com/MalenkiySolovey/solovey-ui/componenthost/hostsurface"
	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
)

func (c *Controller) Status(ctx context.Context, refresh bool) (StatusV1, error) {
	if err := c.ready(); err != nil {
		return StatusV1{}, err
	}
	if refresh {
		hostsurface.Default.Reconcile(ctx)
	}
	now := c.currentTime()
	facts, err := c.Providers.FactsV1(ctx, now)
	if err != nil {
		return StatusV1{}, err
	}
	states, err := c.Repository.LocalProxyStates(ctx)
	if err != nil {
		return StatusV1{}, err
	}
	byEndpoint := make(map[string]protectionrepository.LocalProxyStateV1Model, len(states))
	for _, state := range states {
		byEndpoint[state.ResourceID+"\x00"+state.EndpointID] = state
	}
	plans := make([]PlanV1, 0, len(facts))
	for _, fact := range facts {
		plans = append(plans, planForFact(fact, byEndpoint[fact.ResourceID+"\x00"+fact.EndpointID], now))
	}
	stateViews := make([]StateViewV1, 0, len(states))
	for _, state := range states {
		stateViews = append(stateViews, stateView(state))
	}
	return StatusV1{
		Schema: StatusSchemaV1, GeneratedAt: now.Unix(), Facts: facts, Plans: plans, States: stateViews,
		Experimental: true, DefaultApplyEnabled: false,
	}, nil
}

func stateView(value protectionrepository.LocalProxyStateV1Model) StateViewV1 {
	return StateViewV1{
		ResourceID: value.ResourceID, EndpointID: value.EndpointID, ActualState: ActualState(value.ActualState),
		ApplyGate: ApplyGate(value.ApplyGate), PlanID: value.PlanID, PlanDigest: value.PlanDigest,
		FactRevision: value.FactRevision,
		Lease: LeaseViewV1{LeaseID: value.LeaseID, Revision: value.LeaseRevision,
			State: hostresources.EndpointLeaseState(value.LeaseState), RenewedAt: value.LeaseRenewedAt, ExpiresAt: value.LeaseExpiresAt},
		LatestOperationID: value.LatestOperationID, LatestOperationRevision: value.LatestOperationRevision,
		MarkerRevision: value.MarkerRevision, Health: decodeHealth(value.HealthJSON),
		HealthRevision: value.HealthRevision, HealthExpiresUnixNano: value.HealthExpiresUnixNano,
		ProviderGuarded: value.GuardingProviderLease, RecoveryRequired: value.RecoveryRequired,
		UpdatedAt: value.UpdatedAt,
	}
}

func (c *Controller) Preview(ctx context.Context, reference PlanReferenceV1) (PlanV1, error) {
	if err := c.ready(); err != nil {
		return PlanV1{}, err
	}
	if !safeID(reference.ResourceID, 256) || !safeID(reference.EndpointID, 128) || !digest(reference.FactRevision) {
		return PlanV1{}, serviceError(CodeMalformedInput)
	}
	now := c.currentTime()
	facts, err := c.Providers.FactsV1(ctx, now)
	if err != nil {
		return PlanV1{}, err
	}
	states, err := c.Repository.LocalProxyStates(ctx)
	if err != nil {
		return PlanV1{}, err
	}
	var state protectionrepository.LocalProxyStateV1Model
	for _, candidate := range states {
		if candidate.ResourceID == reference.ResourceID && candidate.EndpointID == reference.EndpointID {
			state = candidate
			break
		}
	}
	for _, fact := range facts {
		if fact.ResourceID == reference.ResourceID && fact.EndpointID == reference.EndpointID {
			if fact.FactRevision != reference.FactRevision {
				return PlanV1{}, serviceError(CodeRevisionDrift)
			}
			return planForFact(fact, state, now), nil
		}
	}
	return PlanV1{}, serviceError(CodeFactMissing)
}

func planForFact(fact hostresources.LocalProxyFactV1, state protectionrepository.LocalProxyStateV1Model, now time.Time) PlanV1 {
	plan := PlanV1{
		CreatedAt: now.Unix(), ExpiresAt: now.Add(MaxPlanAgeV1).Unix(), ResourceID: fact.ResourceID,
		EndpointID: fact.EndpointID, FactRevision: fact.FactRevision, Fact: fact,
		ActualState: StateNotApplied, ApplyGate: ApplyGateBlocked,
	}
	switch {
	case fact.Exposure == hostresources.LocalProxyExposurePublic || fact.Exposure == hostresources.LocalProxyExposureWildcard ||
		fact.Exposure == hostresources.LocalProxyExposureUnspecified || fact.Exposure == hostresources.LocalProxyExposureUnknown:
		plan.BlockCodes = append(plan.BlockCodes, CodeNotShipped)
	case fact.Ownership == hostresources.LocalProxyExternalManaged:
		plan.ActualState = StateExternalManaged
		plan.BlockCodes = append(plan.BlockCodes, CodeExternalManaged)
	case fact.Ownership != hostresources.LocalProxyProviderManaged:
		plan.BlockCodes = append(plan.BlockCodes, CodeOwnerUnproven)
	}
	if fact.ListenerState != hostresources.LocalProxyListenerObservedExact {
		plan.BlockCodes = append(plan.BlockCodes, CodeListenerUnproven)
	}
	if !fact.RuntimeReady {
		plan.BlockCodes = append(plan.BlockCodes, CodeRuntimeDrift)
	}
	if !fact.HealthCapabilityReady {
		plan.BlockCodes = append(plan.BlockCodes, CodeMissingHealth)
	}
	if !fact.CapacityReady {
		plan.BlockCodes = append(plan.BlockCodes, CodeCapacityUnavailable)
	}
	if fact.ManagementCollision != hostresources.CapabilityNo {
		plan.BlockCodes = append(plan.BlockCodes, CodeManagementCollision)
	}
	if fact.RecoveryPathCollision != hostresources.CapabilityNo {
		plan.BlockCodes = append(plan.BlockCodes, CodeRecoveryCollision)
	}
	switch fact.Authentication {
	case hostresources.LocalProxyAuthenticationUnknown:
		plan.BlockCodes = append(plan.BlockCodes, CodeAuthenticationUnknown)
	case hostresources.LocalProxyAuthenticationAbsent:
		if fact.Exposure == hostresources.LocalProxyExposurePrivate {
			plan.BlockCodes = append(plan.BlockCodes, CodePrivateAuthenticationRequired)
		} else if fact.Exposure == hostresources.LocalProxyExposureLoopback {
			plan.WarningCodes = append(plan.WarningCodes, "LOCAL_ONLY_NO_AUTH_RISK")
		}
	case hostresources.LocalProxyAuthenticationPresent:
		if fact.Exposure == hostresources.LocalProxyExposurePrivate {
			plan.WarningCodes = append(plan.WarningCodes, "PRIVATE_NETWORK_PROXY_EXPOSURE")
		}
	}
	if fact.TLS == hostresources.LocalProxyTLSUnknown || fact.SystemProxy == hostresources.LocalProxySystemProxyUnknown {
		plan.BlockCodes = append(plan.BlockCodes, CodeRuntimeShapeUnknown)
	}
	if fact.SystemProxy == hostresources.LocalProxySystemProxyEnabled {
		plan.BlockCodes = append(plan.BlockCodes, CodeNotShipped)
	}
	if fact.DependentUDPAssociation {
		plan.WarningCodes = append(plan.WarningCodes, "SOCKS_UDP_ASSOCIATION_DIAGNOSTICS_ONLY")
	}
	if strings.EqualFold(fact.InboundType, "mixed") {
		plan.WarningCodes = append(plan.WarningCodes, "MIXED_ALL_PROTOCOLS_ATOMIC")
	}
	plan.BlockCodes = append(plan.BlockCodes, fact.ReasonCodes...)
	if len(codes(plan.BlockCodes)) == 0 {
		reference, err := hostresources.ReferenceLocalProxyV1(fact, now)
		if err == nil {
			plan.ExactReference = &reference
			plan.ApplyGate = ApplyGateExperimentalAck
		} else {
			plan.BlockCodes = append(plan.BlockCodes, CodeFactNotActionable)
		}
	}
	if state.ResourceID != "" {
		if state.FactRevision != fact.FactRevision && (state.GuardingProviderLease || state.RecoveryRequired) {
			plan.ActualState = StateRecoveryRequired
			plan.BlockCodes = append(plan.BlockCodes, CodeRevisionDrift)
		} else if validActualState(ActualState(state.ActualState)) {
			plan.ActualState = ActualState(state.ActualState)
		}
	}
	if plan.ApplyGate == ApplyGateBlocked && plan.ActualState == StateNotApplied {
		plan.ActualState = StateBlocked
	}
	return finalizePlan(plan)
}

func (c *Controller) resolvePreparedPlan(ctx context.Context, model protectionrepository.LocalProxyStateV1Model) (PlanV1, hostresources.LocalProxyGuardLeaseV1, error) {
	var plan PlanV1
	if len(model.PlanJSON) == 0 || jsonUnmarshal(model.PlanJSON, &plan) != nil || plan.Validate(c.currentTime()) != nil ||
		plan.PlanID != model.PlanID || plan.PlanDigest != model.PlanDigest || plan.ExactReference == nil {
		return PlanV1{}, hostresources.LocalProxyGuardLeaseV1{}, serviceError(CodeStateInvalid)
	}
	fact, err := c.Providers.ResolveV1(ctx, *plan.ExactReference, c.currentTime())
	if err != nil || fact.FactRevision != model.FactRevision {
		return PlanV1{}, hostresources.LocalProxyGuardLeaseV1{}, serviceError(CodeRevisionDrift)
	}
	provider, ok := c.Providers.Provider(plan.ExactReference.ProviderID)
	if !ok {
		return PlanV1{}, hostresources.LocalProxyGuardLeaseV1{}, serviceError(CodeProviderUnavailable)
	}
	lease, err := provider.GetLocalProxyGuardLease(ctx, hostresources.GetLocalProxyGuardLeaseRequestV1{LeaseID: model.LeaseID})
	if err != nil {
		return PlanV1{}, hostresources.LocalProxyGuardLeaseV1{}, serviceError(CodeLeaseDrift)
	}
	if err := validateStoredLocalProxyBinding(plan, model, lease, false); err != nil {
		return PlanV1{}, hostresources.LocalProxyGuardLeaseV1{}, serviceError(CodeLeaseDrift)
	}
	return plan, lease, nil
}

func jsonUnmarshal(content []byte, value any) error {
	return json.Unmarshal(content, value)
}
