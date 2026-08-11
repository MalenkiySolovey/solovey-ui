package udpguard

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	hostresources "github.com/MalenkiySolovey/solovey-ui/componenthost/resources"
	protectionfirewall "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/firewall"
	protectionhelper "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/helper"
	protectionoperations "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/operations"
	protectionrepository "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/repository"
)

func (c *Controller) resolvePlan(ctx context.Context, reference PlanReferenceV1, refresh bool) (UDPDirectGuardPlanV1, protectionfirewall.BaselineState, error) {
	if !safeReference(reference) {
		return UDPDirectGuardPlanV1{}, protectionfirewall.BaselineState{}, contractError(CodeMalformedInput, nil)
	}
	status, baseline, err := c.statusState(ctx, refresh)
	if err != nil {
		return UDPDirectGuardPlanV1{}, protectionfirewall.BaselineState{}, contractError(CodeMissingCapability, err)
	}
	for _, plan := range status.Plans {
		if plan.PlanID == reference.PlanID && plan.PlanDigest == reference.PlanDigest && plan.ResourceID == reference.ResourceID &&
			plan.EndpointID == reference.EndpointID && plan.CapabilityRevision == reference.CapabilityRevision &&
			plan.Claim.ClaimRevision == reference.ClaimRevision && plan.HealthRevision == reference.HealthRevision &&
			plan.FirewallBaselineRevision == reference.FirewallBaselineRevision && plan.SelectedStrategy == reference.Strategy &&
			plan.FlowPolicy.Revision == reference.PolicyRevision {
			return plan, baseline, nil
		}
	}
	return UDPDirectGuardPlanV1{}, protectionfirewall.BaselineState{}, contractError(CodeRevisionDrift, nil)
}

func safeReference(value PlanReferenceV1) bool {
	return strings.HasPrefix(value.PlanID, "udp-plan:") && digest(value.PlanDigest) && value.ResourceID != "" && value.EndpointID != "" &&
		digest(value.CapabilityRevision) && digest(value.ClaimRevision) && digest(value.HealthRevision) &&
		digest(value.FirewallBaselineRevision) && value.Strategy == "UDP_DIRECT_GUARDED" && digest(value.PolicyRevision)
}

func firstBlock(plan UDPDirectGuardPlanV1) string {
	if len(plan.BlockCodes) > 0 {
		return plan.BlockCodes[0]
	}
	return CodeMissingCapability
}

func attachPlan(baseline protectionfirewall.BaselineState, plan UDPDirectGuardPlanV1) (protectionfirewall.FirewallPlan, error) {
	for _, endpoint := range baseline.Plan.Endpoints {
		if endpoint.ResourceID == plan.ResourceID && endpoint.Key.Network == hostresources.NetworkUDP &&
			endpoint.Key.AddressFamily == plan.Claim.AddressFamily && endpoint.Key.BindAddress == plan.Claim.ConfiguredBind &&
			endpoint.Key.Port == plan.Claim.Port {
			return protectionfirewall.AttachUDPFlowPolicy(baseline.Plan, endpoint.EndpointRevision, plan.FlowPolicy)
		}
	}
	return protectionfirewall.FirewallPlan{}, errors.New("exact UDP endpoint missing")
}

func (c *Controller) applyConfigured(ctx context.Context) error {
	if c == nil || c.Firewall == nil || c.Repository == nil {
		return contractError(CodeMissingCapability, nil)
	}
	settings, _, _, err := c.Repository.LoadSettingsRevision(ctx)
	if err != nil {
		return contractError(CodeInternalFailure, err)
	}
	if !settings.FeatureFlags["enable_apply_beta"] || settings.AdvancedAcknowledgedAt == 0 {
		return contractError(CodeExperimentalAckRequired, nil)
	}
	if reason := c.firewallCapabilityReason(ctx); reason != "" {
		return contractError(CodeMissingCapability, errors.New(reason))
	}
	return nil
}

func (c *Controller) rollbackConfigured(ctx context.Context) error {
	if c == nil || c.Firewall == nil || c.Repository == nil {
		return contractError(CodeMissingCapability, nil)
	}
	if reason := c.firewallCapabilityReason(ctx); reason != "" {
		return contractError(CodeMissingCapability, errors.New(reason))
	}
	return nil
}

func (c *Controller) firewallCapabilityReason(ctx context.Context) string {
	if c == nil || c.Firewall == nil {
		return "helper_not_installed"
	}
	capabilities, err := c.Firewall.Capabilities(ctx)
	if err != nil || capabilities == nil {
		return "helper_capability_unknown"
	}
	if !capabilities.NFT.PlatformKnown {
		return "platform_capability_unknown"
	}
	if !capabilities.NFT.Linux {
		return "linux_required"
	}
	for _, operation := range []protectionhelper.Operation{
		protectionhelper.OperationNFTValidate,
		protectionhelper.OperationNFTApply,
		protectionhelper.OperationNFTRollback,
	} {
		if protectionhelper.CapabilityAvailable(capabilities, operation) {
			continue
		}
		if capabilities.NFT.Reason != "" {
			return capabilities.NFT.Reason
		}
		return "nft_capability_missing"
	}
	return ""
}

func (c *Controller) exactOperation(ctx context.Context, operationID string, revision int, expectedPlanRevision string) (protectionrepository.OperationLockModel, error) {
	if c == nil || c.Operations == nil || strings.TrimSpace(operationID) == "" || revision < 1 {
		return protectionrepository.OperationLockModel{}, contractError(CodeRevisionDrift, nil)
	}
	items, err := c.Operations.List(ctx)
	if err != nil {
		return protectionrepository.OperationLockModel{}, contractError(CodeMissingCapability, err)
	}
	for _, item := range items {
		if item.OperationID != operationID {
			continue
		}
		if item.Kind != protectionoperations.KindFirewall || item.Revision != revision || expectedPlanRevision != "" && item.PlanRevision != expectedPlanRevision {
			return protectionrepository.OperationLockModel{}, contractError(CodeRevisionDrift, nil)
		}
		return item, nil
	}
	return protectionrepository.OperationLockModel{}, contractError(CodeOperationNotFound, nil)
}

func (c *Controller) operationBound(ctx context.Context, operationID string) error {
	states, err := c.Repository.UDPGuardStates(ctx)
	if err != nil {
		return contractError(CodeInternalFailure, err)
	}
	for _, state := range states {
		if state.LatestOperationID == operationID && (state.ActualState != string(StateNotApplied) || state.RecoveryRequired || state.OwnsActiveContribution || state.RecoverableArtifact) {
			return nil
		}
	}
	return contractError(CodeRevisionDrift, nil)
}

func (c *Controller) saveState(ctx context.Context, plan UDPDirectGuardPlanV1, operationID string, operationRevision int, state ActualState, active, recoverable bool) error {
	transition, err := c.Repository.FirewallTransition(ctx, operationID)
	if err != nil {
		return err
	}
	return c.Repository.SaveUDPGuardState(ctx, protectionrepository.UDPGuardStateV1Model{
		ResourceID: plan.ResourceID, EndpointID: plan.EndpointID, AddressFamily: string(plan.Claim.AddressFamily), Schema: UDPStatusSchemaV1,
		DesiredPolicy: plan.DesiredPolicy, SelectedStrategy: plan.SelectedStrategy, ActualState: string(state),
		PlanID: plan.PlanID, PlanDigest: plan.PlanDigest, CapabilityRevision: plan.CapabilityRevision,
		ClaimRevision: plan.Claim.ClaimRevision, PolicyRevision: plan.FlowPolicy.Revision,
		ContributionID: transition.ContributionID, ContributionRevision: transition.DesiredSemanticRevision,
		CompositionRevision: transition.AfterCompositionRevision, ManagedPlanRevision: transition.ManagedPlanRevision,
		HealthProviderInstance: transition.HealthProviderInstance, HealthGeneration: transition.HealthGeneration,
		HealthObservationRevision: transition.HealthObservationRevision, HealthStartedUnixNano: transition.HealthStartedUnixNano,
		HealthCompletedUnixNano: transition.HealthCompletedUnixNano, HealthExpiresUnixNano: transition.HealthExpiresUnixNano,
		LatestOperationID: operationID, LatestOperationRevision: operationRevision,
		RecoveryRequired: state == StateRecoveryRequired, OwnsActiveContribution: active, RecoverableArtifact: recoverable,
	})
}

func projectOrphanedAuthority(status *StatusV1, states []protectionrepository.UDPGuardStateV1Model, authority protectionrepository.FirewallAuthoritySnapshot) {
	if status == nil {
		return
	}
	current := make(map[string]struct{}, len(status.Plans))
	for _, plan := range status.Plans {
		current[plan.ResourceID+"\x00"+plan.EndpointID] = struct{}{}
	}
	active := make(map[string]string, len(authority.Contributions))
	for _, contribution := range authority.Contributions {
		active[contribution.ContributionID] = contribution.SemanticRevision
	}
	for _, state := range states {
		if _, exists := current[state.ResourceID+"\x00"+state.EndpointID]; exists {
			continue
		}
		activeLike := state.ActualState != string(StateNotApplied) || state.RecoveryRequired || state.OwnsActiveContribution || state.RecoverableArtifact ||
			active[state.ContributionID] == state.ContributionRevision
		if !activeLike {
			continue
		}
		for index := range status.Plans {
			plan := &status.Plans[index]
			if plan.ResourceID != state.ResourceID || state.AddressFamily != "" && string(plan.Claim.AddressFamily) != state.AddressFamily {
				continue
			}
			plan.ActualState = StateRecoveryRequired
			plan.RecoveryRequired = true
			plan.ApplyGate = ApplyGateBlocked
			plan.BlockCodes = reasons(append(plan.BlockCodes, "RECOVERY_REQUIRED_STALE_SOCKET_BINDING"))
		}
		for index := range status.Capabilities {
			if status.Capabilities[index].ResourceID != state.ResourceID {
				continue
			}
			status.Capabilities[index].ActualState = StateRecoveryRequired
			status.Capabilities[index].ApplyGate = ApplyGateBlocked
			status.Capabilities[index].ReasonCodes = reasons(append(status.Capabilities[index].ReasonCodes, "RECOVERY_REQUIRED_STALE_SOCKET_BINDING"))
		}
	}
}

func (c *Controller) beginReceipt(ctx context.Context, action, key string, input any) (protectionrepository.UDPGuardIdempotencyV1Model, bool, error) {
	receipt, replay, err := c.Repository.BeginUDPGuardReceipt(ctx, action, strings.TrimSpace(key), hostresources.Revision(input))
	if err != nil {
		return protectionrepository.UDPGuardIdempotencyV1Model{}, false, contractError(CodeIdempotencyConflict, err)
	}
	return receipt, replay, nil
}

func decodeReceipt[T any](receipt protectionrepository.UDPGuardIdempotencyV1Model) (T, error) {
	var response T
	if err := json.Unmarshal(receipt.SemanticResponseJSON, &response); err != nil {
		return response, contractError(CodeAmbiguousResult, err)
	}
	return response, nil
}
