package response

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
	protectionpolicy "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/policy"
	protectionresources "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/resources"
)

func TestEveryStrategyIntentCellCannotFalseClaimApplied(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	strategies := []protectionresources.Strategy{
		protectionresources.StrategyNativeFallback,
		protectionresources.StrategyDirectGuarded,
		protectionresources.StrategyL4OneToOneFronting,
		protectionresources.StrategySNIPreReadFronting,
		protectionresources.StrategyHTTPTerminatingFront,
		protectionresources.StrategyUDPQUICDirectGuarded,
		protectionresources.StrategyLocalProxyGuard,
		protectionresources.StrategyInterceptionGuard,
		protectionresources.StrategyUnclassified,
	}
	intents := []domain.ResponseIntent{domain.IntentObserve, domain.IntentSoftGraylist, domain.IntentRateLimit, domain.IntentRouteToDecoy, domain.IntentTemporaryQuarantine, domain.IntentTemporaryBlock, domain.IntentManualHardBlock}
	for _, strategy := range strategies {
		for _, intent := range intents {
			t.Run(string(strategy)+"/"+string(intent), func(t *testing.T) {
				decision := resolverDecision(now, intent)
				resolution := Resolve(ResolveInput{Decision: decision, Strategy: strategy, Capability: resolverCapability(now, strategy, true, true), ActionScopeRevision: strings.Repeat("a", 64), EndpointRevision: strings.Repeat("b", 64), ResourceRevision: strings.Repeat("c", 64), ConfigurationRevision: strings.Repeat("d", 64), EndpointKnown: true, Guard: protectionpolicy.ManagementGuardResult{State: protectionpolicy.ManagementGuardNotApplicable, ActionAllowed: true}, Now: now})
				if resolution.Decision.State == domain.DecisionApplied || resolution.ActualStatus == "APPLIED" {
					t.Fatal("resolver false-claimed APPLIED")
				}
				if err := resolution.Decision.Validate(now); err != nil {
					t.Fatalf("resolved decision is invalid: %v (%#v)", err, resolution)
				}
				if resolution.SelectedIntent == domain.IntentObserve {
					if resolution.PlannedResponse != nil || resolution.Decision.CapabilityResolution.Implemented {
						t.Fatal("observe-only resolution created an actionable plan")
					}
					return
				}
				if resolution.PlannedResponse == nil || resolution.PlannedResponse.ActualState != "NOT_APPLIED" {
					t.Fatalf("supported cell did not create a strictly planned response: %#v", resolution.PlannedResponse)
				}
				if err := resolution.PlannedResponse.Validate(); err != nil {
					t.Fatalf("planned response is invalid: %v", err)
				}
			})
		}
	}
}

func TestUnsupportedIntentDowngradesAndManagementGuardWins(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	decoy := Resolve(ResolveInput{Decision: resolverDecision(now, domain.IntentRouteToDecoy), Strategy: protectionresources.StrategyDirectGuarded, Capability: resolverCapability(now, protectionresources.StrategyDirectGuarded, false, true), ActionScopeRevision: strings.Repeat("a", 64), EndpointRevision: strings.Repeat("b", 64), ResourceRevision: strings.Repeat("c", 64), ConfigurationRevision: strings.Repeat("d", 64), EndpointKnown: true, Guard: protectionpolicy.ManagementGuardResult{State: protectionpolicy.ManagementGuardNotApplicable, ActionAllowed: true}, Now: now})
	if decoy.SelectedIntent != domain.IntentSoftGraylist || !slices.Contains(decoy.ReasonCodes, "unsupported_action_downgraded") {
		t.Fatalf("unsupported decoy did not downgrade to a safer action: %#v", decoy)
	}
	guarded := Resolve(ResolveInput{Decision: resolverDecision(now, domain.IntentTemporaryBlock), Strategy: protectionresources.StrategyDirectGuarded, Capability: resolverCapability(now, protectionresources.StrategyDirectGuarded, false, true), ActionScopeRevision: strings.Repeat("a", 64), EndpointRevision: strings.Repeat("b", 64), ResourceRevision: strings.Repeat("c", 64), ConfigurationRevision: strings.Repeat("d", 64), EndpointKnown: true, Guard: protectionpolicy.ManagementGuardResult{State: protectionpolicy.ManagementGuardProtected, ActionAllowed: false, ReasonCodes: []string{"last_recovery_path_protected"}}, Now: now})
	if guarded.SelectedIntent != domain.IntentObserve || guarded.PlannedResponse != nil || !slices.Contains(guarded.ReasonCodes, "management_precedence") {
		t.Fatalf("management precedence did not force observe-only: %#v", guarded)
	}
}

func TestSNIPrereadActionResolutionIsExplicitlyDowngradeOnly(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	binding := domain.DecisionRevisionBindingV2{
		ActionScopeRevision: strings.Repeat("a", 64), StrategyRevision: "strategy-revision-v2", CapabilityRevision: "capability-revision-v2",
		EndpointRevision: strings.Repeat("b", 64), ResourceRevision: strings.Repeat("c", 64), ConfigurationRevision: strings.Repeat("d", 64),
	}
	if SNIPrereadActionMapDecisionV2 != "DOWNGRADE_ONLY" {
		t.Fatalf("unexpected SNI action-map decision %q", SNIPrereadActionMapDecisionV2)
	}
	for _, test := range []struct {
		name                  string
		sameScopeRateLimit    bool
		want                  domain.ResponseIntent
		wantPlannedNotApplied bool
	}{
		{name: "soft-graylist", sameScopeRateLimit: true, want: domain.IntentSoftGraylist, wantPlannedNotApplied: true},
		{name: "observe", sameScopeRateLimit: false, want: domain.IntentObserve},
	} {
		t.Run(test.name, func(t *testing.T) {
			capability := SNIPrereadFrontingCapability(now, "core:inbound:one", "endpoint:one", binding, test.sameScopeRateLimit)
			if capability.ForcedSameSubjectDecoyRoute || capability.NaturalInvalidTrafficFallback || capability.HardBlock {
				t.Fatalf("natural SNI topology was exposed as a forced subject action: %#v", capability)
			}
			resolution := Resolve(ResolveInput{
				Decision: resolverDecision(now, domain.IntentRouteToDecoy), Strategy: protectionresources.StrategySNIPreReadFronting,
				Capability: capability, ActionScopeRevision: binding.ActionScopeRevision, EndpointRevision: binding.EndpointRevision,
				ResourceRevision: binding.ResourceRevision, ConfigurationRevision: binding.ConfigurationRevision, EndpointKnown: true,
				Guard: protectionpolicy.ManagementGuardResult{State: protectionpolicy.ManagementGuardNotApplicable, ActionAllowed: true}, Now: now,
			})
			if resolution.SelectedIntent != test.want || resolution.ActualStatus == "APPLIED" || resolution.Decision.State == domain.DecisionApplied {
				t.Fatalf("SNI route-to-decoy was not safely downgraded: %#v", resolution)
			}
			if test.wantPlannedNotApplied {
				if resolution.PlannedResponse == nil || resolution.PlannedResponse.ActualState != "NOT_APPLIED" {
					t.Fatalf("downgraded plan was not explicitly NOT_APPLIED: %#v", resolution.PlannedResponse)
				}
			} else if resolution.PlannedResponse != nil {
				t.Fatalf("observe-only downgrade created action evidence: %#v", resolution.PlannedResponse)
			}
		})
	}
}

func TestUnknownEndpointAndCrossScopeRemainObserveOnly(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	decision := resolverDecision(now, domain.IntentTemporaryBlock)
	decision.Scope = domain.SignalScopeV2{Scope: domain.ScopeHostWide}
	decision.TargetResourceIDs = nil
	decision.ScoreSnapshot.TargetGroup = string(domain.ScopeHostWide)
	decision.FinalizeID()
	resolution := Resolve(ResolveInput{Decision: decision, Strategy: protectionresources.StrategyDirectGuarded, EndpointKnown: false, Guard: protectionpolicy.ManagementGuardResult{State: protectionpolicy.ManagementGuardNotApplicable, ActionAllowed: true}, Now: now})
	if resolution.SelectedIntent != domain.IntentObserve || resolution.PlannedResponse != nil || !slices.Contains(resolution.ReasonCodes, "scope_action_unavailable") || !slices.Contains(resolution.ReasonCodes, "endpoint_inventory_unknown") {
		t.Fatalf("cross-scope or unknown endpoint became actionable: %#v", resolution)
	}
}

func TestExpiredUnclearedOrUnrevisionedDecisionCannotPlanAction(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	for name, mutate := range map[string]func(*domain.ProtectionDecisionV2, *ResolveInput){
		"expired": func(decision *domain.ProtectionDecisionV2, _ *ResolveInput) {
			decision.ExpiresAt = now.Add(-time.Second)
			decision.CreatedAt = now.Add(-time.Hour)
			decision.ScoreSnapshot.CapturedAt = decision.CreatedAt
			decision.FinalizeID()
		},
		"allowlisted": func(decision *domain.ProtectionDecisionV2, _ *ResolveInput) {
			decision.AllowlistResult.Result = "allow"
		},
		"unknown-allowlist": func(decision *domain.ProtectionDecisionV2, _ *ResolveInput) {
			decision.AllowlistResult.Result = "unknown"
		},
		"missing-revision": func(_ *domain.ProtectionDecisionV2, input *ResolveInput) { input.EndpointRevision = "" },
		"recovery-not-evaluated": func(decision *domain.ProtectionDecisionV2, _ *ResolveInput) {
			decision.RecoveryResult.Result = "not_evaluated"
		},
		"recovery-unknown": func(decision *domain.ProtectionDecisionV2, _ *ResolveInput) {
			decision.RecoveryResult.Result = "unknown"
		},
		"recovery-stale": func(decision *domain.ProtectionDecisionV2, _ *ResolveInput) { decision.RecoveryResult.Result = "stale" },
		"recovery-ambiguous": func(decision *domain.ProtectionDecisionV2, _ *ResolveInput) {
			decision.RecoveryResult.Result = "ambiguous"
		},
	} {
		t.Run(name, func(t *testing.T) {
			decision := resolverDecision(now, domain.IntentTemporaryBlock)
			input := ResolveInput{Decision: decision, Strategy: protectionresources.StrategyDirectGuarded, Capability: resolverCapability(now, protectionresources.StrategyDirectGuarded, false, true), ActionScopeRevision: strings.Repeat("a", 64), EndpointRevision: strings.Repeat("b", 64), ResourceRevision: strings.Repeat("c", 64), ConfigurationRevision: strings.Repeat("d", 64), EndpointKnown: true, Guard: protectionpolicy.ManagementGuardResult{State: protectionpolicy.ManagementGuardNotApplicable, ActionAllowed: true}, Now: now}
			mutate(&input.Decision, &input)
			resolution := Resolve(input)
			if resolution.SelectedIntent != domain.IntentObserve || resolution.PlannedResponse != nil || resolution.ActualStatus == "APPLIED" {
				t.Fatalf("unsafe decision became actionable: %#v", resolution)
			}
			if name == "expired" && resolution.Decision.State != domain.DecisionExpired {
				t.Fatalf("expired decision was mislabeled as %s", resolution.Decision.State)
			}
		})
	}
}

func TestStrategyAndCapabilityRevisionMismatchRemainObserveOnly(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	for name, mutate := range map[string]func(*ResolveInput){
		"strategy":       func(input *ResolveInput) { input.Capability.StrategyRevision = "different-strategy-revision" },
		"capability":     func(input *ResolveInput) { input.Capability.CapabilityRevision = "different-capability-revision" },
		"endpoint-scope": func(input *ResolveInput) { input.Capability.EndpointID = "endpoint:other" },
	} {
		t.Run(name, func(t *testing.T) {
			input := ResolveInput{Decision: resolverDecision(now, domain.IntentRateLimit), Strategy: protectionresources.StrategyDirectGuarded, Capability: resolverCapability(now, protectionresources.StrategyDirectGuarded, false, true), ActionScopeRevision: strings.Repeat("a", 64), EndpointRevision: strings.Repeat("b", 64), ResourceRevision: strings.Repeat("c", 64), ConfigurationRevision: strings.Repeat("d", 64), EndpointKnown: true, Guard: protectionpolicy.ManagementGuardResult{State: protectionpolicy.ManagementGuardNotApplicable, ActionAllowed: true}, Now: now}
			mutate(&input)
			resolution := Resolve(input)
			if resolution.SelectedIntent != domain.IntentObserve || resolution.PlannedResponse != nil || !slices.Contains(resolution.ReasonCodes, "action_revision_mismatch") {
				t.Fatalf("mismatched %s capability became actionable: %#v", name, resolution)
			}
		})
	}
}

func TestStrategiesWithoutProvenExactEndpointHookRemainObserveOnly(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	for _, strategy := range []protectionresources.Strategy{protectionresources.StrategyLocalProxyGuard, protectionresources.StrategyInterceptionGuard} {
		resolution := Resolve(ResolveInput{Decision: resolverDecision(now, domain.IntentTemporaryBlock), Strategy: strategy, Capability: resolverCapability(now, strategy, false, false), ActionScopeRevision: strings.Repeat("a", 64), EndpointRevision: strings.Repeat("b", 64), ResourceRevision: strings.Repeat("c", 64), ConfigurationRevision: strings.Repeat("d", 64), EndpointKnown: true, Guard: protectionpolicy.ManagementGuardResult{State: protectionpolicy.ManagementGuardNotApplicable, ActionAllowed: true}, Now: now})
		if resolution.SelectedIntent != domain.IntentObserve || resolution.PlannedResponse != nil {
			t.Fatalf("strategy without an exact endpoint hook was treated as supported: %s %#v", strategy, resolution)
		}
	}
}

func resolverDecision(now time.Time, intent domain.ResponseIntent) domain.ProtectionDecisionV2 {
	decision := domain.ProtectionDecisionV2{Schema: domain.ProtectionDecisionSchemaV2, PolicyRevision: "endpoint-baseline-policy", StrategyRevision: "strategy-revision-v2", CapabilityRevision: "capability-revision-v2", ActionScopeRevision: strings.Repeat("a", 64), EndpointRevision: strings.Repeat("b", 64), ResourceRevision: strings.Repeat("c", 64), ConfigurationRevision: strings.Repeat("d", 64), Subject: domain.SignalSubjectV2{Type: "ip", Value: "192.0.2.10"}, Scope: domain.SignalScopeV2{Scope: domain.ScopeEndpoint, TargetResourceID: "core:inbound:one", EndpointID: "endpoint:one", Transport: "tcp"}, TargetResourceIDs: []string{"core:inbound:one"}, SignalRefs: []string{strings.Repeat("e", 64)}, SourceClasses: []string{"native"}, ScoreSnapshot: domain.ScoreSnapshotV2{Score: 100, TargetGroup: "core:inbound:one", CapturedAt: now}, ConfidenceBP: 9000, ReasonCodes: []string{}, RequestedIntent: intent, CreatedAt: now, ExpiresAt: now.Add(time.Hour), AllowlistResult: domain.PolicyCheckV2{Result: "deny"}, RecoveryResult: domain.PolicyCheckV2{Result: "allow"}, CapabilityResolution: domain.CapabilityResolutionV2{ResolvedIntent: domain.IntentObserve}, State: domain.DecisionCandidate}
	decision.FinalizeID()
	return decision
}

func resolverCapability(now time.Time, strategy protectionresources.Strategy, forced, rate bool) domain.StrategyActionCapabilityV2 {
	return domain.StrategyActionCapabilityV2{
		Schema: domain.StrategyActionCapabilitySchemaV2, Strategy: string(strategy),
		ActionRevision: strings.Repeat("a", 64), StrategyRevision: "strategy-revision-v2", CapabilityRevision: "capability-revision-v2",
		ResourceID: "core:inbound:one", EndpointID: "endpoint:one",
		ActionScopeRevision: strings.Repeat("a", 64), EndpointRevision: strings.Repeat("b", 64),
		ResourceRevision: strings.Repeat("c", 64), ConfigurationRevision: strings.Repeat("d", 64),
		NaturalInvalidTrafficFallback: strategy == protectionresources.StrategyNativeFallback,
		ForcedSameSubjectDecoyRoute:   forced, SameScopeRateLimit: rate, HardBlock: rate,
		Provenance: "resolver-test", ObservedAt: now, ExpiresAt: now.Add(time.Hour), ReasonCodes: []string{},
	}
}
