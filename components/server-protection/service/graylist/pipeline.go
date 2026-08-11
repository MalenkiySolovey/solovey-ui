package graylist

import (
	"errors"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
	protectiondecision "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/decision"
	protectionpolicy "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/policy"
	protectionresources "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/resources"
	protectionresponse "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/response"
)

type PipelineInput struct {
	Signal                domain.ProtectionSignalV2
	Admission             AdmissionContext
	Existing              *domain.GraylistStateV2
	Policy                PolicyV2
	Strategy              protectionresources.Strategy
	StrategyRevision      string
	CapabilityRevision    string
	Capability            domain.StrategyActionCapabilityV2
	AllowlistResult       domain.PolicyCheckV2
	RecoveryResult        domain.PolicyCheckV2
	Allowlisted           bool
	Management            bool
	RecoveryProtected     bool
	TrustedSource         bool
	Guard                 protectionpolicy.ManagementGuardResult
	ActionScopeRevision   string
	EndpointRevision      string
	ResourceRevision      string
	ConfigurationRevision string
	BaselineApplyBlocked  bool
	EndpointKnown         bool
	Now                   time.Time
}

type PipelineResult struct {
	Accepted   AcceptedSignal
	State      domain.GraylistStateV2
	Decision   domain.ProtectionDecisionV2
	Resolution protectionresponse.Resolution
	Changed    bool
}

// Process executes the pure admission-to-response chain. Persistence remains a
// separate compare-and-swap step and no executor/native/kernel interface is reachable.
func Process(input PipelineInput) (PipelineResult, error) {
	now := input.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	input.Admission.Now = now
	input.Now = now
	accepted, err := Admit(input.Signal, input.Admission)
	if err != nil {
		return PipelineResult{}, err
	}
	return ProcessAccepted(input, accepted)
}

// ProcessAccepted continues the chain after the caller has crossed the sole
// admission boundary. Repository unit-of-work code uses it after persisting the
// admitted signal in the same transaction.
func ProcessAccepted(input PipelineInput, accepted AcceptedSignal) (PipelineResult, error) {
	now := input.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if err := input.Capability.Validate(now); err != nil {
		return PipelineResult{}, err
	}
	if input.StrategyRevision != input.Capability.StrategyRevision ||
		input.CapabilityRevision != input.Capability.CapabilityRevision ||
		input.ActionScopeRevision != input.Capability.ActionRevision ||
		input.ActionScopeRevision != input.Capability.ActionScopeRevision ||
		input.EndpointRevision != input.Capability.EndpointRevision ||
		input.ResourceRevision != input.Capability.ResourceRevision ||
		input.ConfigurationRevision != input.Capability.ConfigurationRevision ||
		input.Signal.Scope.TargetResourceID != input.Capability.ResourceID ||
		input.Signal.Scope.EndpointID != input.Capability.EndpointID ||
		string(input.Strategy) != input.Capability.Strategy {
		return PipelineResult{}, errors.New("strategy action capability binding mismatch")
	}
	evaluated, err := Evaluate(EvaluateInput{
		Existing: input.Existing, Accepted: &accepted, Policy: input.Policy,
		StrategyRevision: input.StrategyRevision, CapabilityRevision: input.CapabilityRevision,
		Allowlisted: input.Allowlisted, Management: input.Management, RecoveryProtected: input.RecoveryProtected,
		TrustedSource: input.TrustedSource, Now: now,
	})
	if err != nil {
		return PipelineResult{}, err
	}
	result := PipelineResult{Accepted: accepted, State: evaluated.State, Changed: evaluated.Changed}
	if result.State.Lifecycle == domain.GraylistLifecycleSuperseded || result.State.Lifecycle == domain.GraylistLifecycleExpired {
		return result, nil
	}
	binding := domain.DecisionRevisionBindingV2{
		StrategyRevision: input.StrategyRevision, CapabilityRevision: input.CapabilityRevision,
		ActionScopeRevision: input.ActionScopeRevision, EndpointRevision: input.EndpointRevision,
		ResourceRevision: input.ResourceRevision, ConfigurationRevision: input.ConfigurationRevision,
	}
	decision, err := protectiondecision.ResolveGraylistState(result.State, input.AllowlistResult, input.RecoveryResult, binding, now)
	if err != nil {
		return PipelineResult{}, err
	}
	resolution := protectionresponse.Resolve(protectionresponse.ResolveInput{
		Decision: decision, Strategy: input.Strategy, Capability: input.Capability,
		ActionScopeRevision: input.ActionScopeRevision, EndpointRevision: input.EndpointRevision,
		ResourceRevision: input.ResourceRevision, ConfigurationRevision: input.ConfigurationRevision,
		BaselineApplyBlocked: input.BaselineApplyBlocked, EndpointKnown: input.EndpointKnown,
		Guard: input.Guard, Now: now,
	})
	result.Decision, result.Resolution = resolution.Decision, resolution
	if result.State.SelectedResponse != resolution.SelectedIntent {
		result.State.SelectedResponse = resolution.SelectedIntent
		result.State.ReasonCodes = domain.CanonicalBoundedReasons(append(result.State.ReasonCodes, resolution.ReasonCodes...)...)
		result.State.UpdatedAt = now
		result.State.Revision++
		result.Changed = true
		if err := result.State.Validate(); err != nil {
			return PipelineResult{}, err
		}
	}
	return result, nil
}
