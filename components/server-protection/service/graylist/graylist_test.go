package graylist

import (
	"errors"
	"fmt"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/MalenkiySolovey/solovey-ui/components/server-protection/domain"
	protectionpolicy "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/policy"
	protectionresources "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/resources"
	protectionresponse "github.com/MalenkiySolovey/solovey-ui/components/server-protection/service/response"
)

func TestAdmissionExactScopeAttributionAndForbiddenInputs(t *testing.T) {
	now := time.Unix(1_000, 0).UTC()
	valid := signalFixture(now, 1, domain.SignalFallbackHit)
	if _, err := Admit(valid, AdmissionContext{SelectedAdapterID: "adapter:exact", ExposesOriginalSubject: true, Now: now}); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*domain.ProtectionSignalV2, *AdmissionContext){
		"native-does-not-expose-original": func(_ *domain.ProtectionSignalV2, ctx *AdmissionContext) { ctx.ExposesOriginalSubject = false },
		"stale": func(signal *domain.ProtectionSignalV2, _ *AdmissionContext) {
			signal.ExpiresAt = now.Add(-time.Second)
			signal.FinalizeID("event-stale")
		},
		"unknown": func(signal *domain.ProtectionSignalV2, _ *AdmissionContext) {
			signal.Kind = "future_kind"
			signal.KnownKind = false
			signal.FinalizeID("event-unknown")
		},
		"loopback": func(signal *domain.ProtectionSignalV2, _ *AdmissionContext) {
			signal.Subject.Value = "127.0.0.1"
			signal.FinalizeID("event-loopback")
		},
		"missing-endpoint": func(signal *domain.ProtectionSignalV2, _ *AdmissionContext) {
			signal.Scope.EndpointID = ""
			signal.FinalizeID("event-endpoint")
		},
		"packet-fingerprint": func(signal *domain.ProtectionSignalV2, _ *AdmissionContext) {
			signal.SafeMeta = map[string]string{"ja4": "bounded"}
			signal.FinalizeID("event-ja4")
		},
		"truncated-metadata": func(signal *domain.ProtectionSignalV2, _ *AdmissionContext) {
			signal.SafeMeta = map[string]string{"truncated": "true"}
			signal.FinalizeID("event-truncated")
		},
		"health-telemetry": func(_ *domain.ProtectionSignalV2, ctx *AdmissionContext) { ctx.TargetIsHealthTelemetry = true },
	} {
		t.Run(name, func(t *testing.T) {
			signal := valid
			ctx := AdmissionContext{SelectedAdapterID: "adapter:exact", ExposesOriginalSubject: true, Now: now}
			mutate(&signal, &ctx)
			if _, err := Admit(signal, ctx); err == nil {
				t.Fatal("unsafe signal was admitted")
			}
		})
	}
}

func TestGraylistLifecycleHysteresisExpiryReentryAndPrecedence(t *testing.T) {
	now := time.Unix(2_000, 0).UTC()
	policy := PolicyV2{Revision: "policy:test", EnterScore: 2, ExitScore: 1, RateScore: 4, RateConfidenceBP: 5000, MaxScore: 100, DecayInterval: time.Minute, TTL: time.Hour}
	first := AcceptedSignal{Signal: signalFixture(now, 1, domain.SignalFallbackHit), Delta: 2}
	result, err := Evaluate(EvaluateInput{Accepted: &first, Policy: policy, StrategyRevision: "strategy:test", CapabilityRevision: "capability:test", Now: now})
	if err != nil || result.State.Band != domain.GraylistBandGraylist || !result.Changed {
		t.Fatalf("observe -> graylist: %#v err=%v", result, err)
	}
	second := AcceptedSignal{Signal: signalFixture(now.Add(time.Second), 2, domain.SignalFallbackHit), Delta: 2}
	rate, err := Evaluate(EvaluateInput{Existing: &result.State, Accepted: &second, Policy: policy, StrategyRevision: "strategy:test", CapabilityRevision: "capability:test", Now: now.Add(time.Second)})
	if err != nil || rate.State.Band != domain.GraylistBandRateCandidate {
		t.Fatalf("graylist -> rate: %#v err=%v", rate, err)
	}
	cooldown, err := Evaluate(EvaluateInput{Existing: &rate.State, Policy: policy, StrategyRevision: "strategy:test", CapabilityRevision: "capability:test", Now: now.Add(5 * time.Minute)})
	if err != nil || cooldown.State.Band != domain.GraylistBandCooldown || cooldown.State.Lifecycle != domain.GraylistLifecycleCooldown {
		t.Fatalf("active -> cooldown: %#v err=%v", cooldown, err)
	}
	unchanged, err := Evaluate(EvaluateInput{Existing: &cooldown.State, Policy: policy, StrategyRevision: "strategy:test", CapabilityRevision: "capability:test", Now: now.Add(6 * time.Minute)})
	if err != nil || unchanged.Changed {
		t.Fatalf("unchanged timer evaluation wrote: %#v err=%v", unchanged, err)
	}
	expired, err := Evaluate(EvaluateInput{Existing: &cooldown.State, Policy: policy, StrategyRevision: "strategy:test", CapabilityRevision: "capability:test", Now: cooldown.State.ExpiresAt})
	if err != nil || expired.State.Lifecycle != domain.GraylistLifecycleExpired {
		t.Fatalf("cooldown -> expired: %#v err=%v", expired, err)
	}
	fresh := AcceptedSignal{Signal: signalFixture(expired.State.ExpiresAt.Add(time.Second), 3, domain.SignalFallbackHit), Delta: 2}
	reentered, err := Evaluate(EvaluateInput{Existing: &expired.State, Accepted: &fresh, Policy: policy, StrategyRevision: "strategy:test", CapabilityRevision: "capability:test", Now: fresh.Signal.ObservedAt})
	if err != nil || reentered.State.Lifecycle != domain.GraylistLifecycleActive || reentered.State.Band != domain.GraylistBandGraylist {
		t.Fatalf("fresh signal did not re-enter: %#v err=%v", reentered, err)
	}
	superseded, err := Evaluate(EvaluateInput{Existing: &reentered.State, Policy: policy, StrategyRevision: "strategy:test", CapabilityRevision: "capability:test", Allowlisted: true, Now: fresh.Signal.ObservedAt.Add(time.Second)})
	if err != nil || superseded.State.Lifecycle != domain.GraylistLifecycleSuperseded || superseded.State.SelectedResponse != domain.IntentObserve || !slices.Contains(superseded.State.ReasonCodes, "allowlist_precedence") {
		t.Fatalf("allowlist precedence: %#v err=%v", superseded, err)
	}
	if _, err := Evaluate(EvaluateInput{Existing: &superseded.State, Accepted: &fresh, Policy: policy, StrategyRevision: "strategy:test", CapabilityRevision: "capability:test", Now: fresh.Signal.ObservedAt.Add(2 * time.Second)}); err == nil {
		t.Fatal("superseded state silently reactivated")
	}
	drifted, err := Evaluate(EvaluateInput{Existing: &reentered.State, Policy: policy, StrategyRevision: "strategy:changed", CapabilityRevision: "capability:test", Now: fresh.Signal.ObservedAt.Add(2 * time.Second)})
	if err != nil || drifted.State.Lifecycle != domain.GraylistLifecycleSuperseded || !slices.Contains(drifted.State.ReasonCodes, "revision_drift") {
		t.Fatalf("strategy drift did not supersede: %#v err=%v", drifted, err)
	}
}

func TestWeakSignalAloneAndExactReplayCannotEnforce(t *testing.T) {
	now := time.Unix(3_000, 0).UTC()
	policy := PolicyV2{Revision: "policy:test", EnterScore: 1, ExitScore: 0, RateScore: 2, RateConfidenceBP: 1, MaxScore: 100, DecayInterval: time.Hour, TTL: time.Hour}
	accepted := AcceptedSignal{Signal: signalFixture(now, 1, domain.SignalShortTLSSession), Delta: 2, Weak: true}
	first, err := Evaluate(EvaluateInput{Accepted: &accepted, Policy: policy, StrategyRevision: "strategy:test", CapabilityRevision: "capability:test", Now: now})
	if err != nil || first.State.Band != domain.GraylistBandObserve || first.State.DesiredAction != domain.IntentObserve {
		t.Fatalf("weak signal enforced: %#v err=%v", first, err)
	}
	replay, err := Evaluate(EvaluateInput{Existing: &first.State, Accepted: &accepted, Policy: policy, StrategyRevision: "strategy:test", CapabilityRevision: "capability:test", Now: now})
	if err != nil || replay.Changed || replay.State.Score != first.State.Score {
		t.Fatalf("exact replay was not idempotent: %#v err=%v", replay, err)
	}
	timer, err := Evaluate(EvaluateInput{Existing: &first.State, Policy: policy, StrategyRevision: "strategy:test", CapabilityRevision: "capability:test", Now: now.Add(time.Second)})
	if err != nil || timer.State.Band != domain.GraylistBandObserve || timer.State.DesiredAction != domain.IntentObserve {
		t.Fatalf("timer forgot weak-only evidence: %#v err=%v", timer, err)
	}
}

func TestExternalOnlyAndExpiredStrongCorroborationCannotEnforce(t *testing.T) {
	now := time.Unix(3_500, 0).UTC()
	policy := PolicyV2{Revision: "policy:test", EnterScore: 1, ExitScore: 0, RateScore: 2, RateConfidenceBP: 1, MaxScore: 100, DecayInterval: time.Hour, TTL: time.Hour}
	externalSignal := signalFixture(now, 1, domain.SignalExternalReputation)
	externalSignal.Category = domain.SignalCategoryExternalReputation
	externalSignal.Source.SourceClass = "external"
	externalSignal.Source.TrustClass = "external_reputation"
	externalSignal.FinalizeID("external:one")
	external := AcceptedSignal{Signal: externalSignal, Delta: 2, EvidenceClass: domain.GraylistEvidenceExternal}
	externalOnly, err := Evaluate(EvaluateInput{Accepted: &external, Policy: policy, StrategyRevision: "strategy:test", CapabilityRevision: "capability:test", Now: now})
	if err != nil || externalOnly.State.Band != domain.GraylistBandObserve || externalOnly.State.DesiredAction != domain.IntentObserve {
		t.Fatalf("external-only evidence enforced: %#v err=%v", externalOnly, err)
	}
	timer, err := Evaluate(EvaluateInput{Existing: &externalOnly.State, Policy: policy, StrategyRevision: "strategy:test", CapabilityRevision: "capability:test", Now: now.Add(time.Second)})
	if err != nil || timer.State.Band != domain.GraylistBandObserve {
		t.Fatalf("timer enforced external-only evidence: %#v err=%v", timer, err)
	}

	strongSignal := signalFixture(now.Add(10*time.Second), 2, domain.SignalFallbackHit)
	strongSignal.ExpiresAt = now.Add(time.Minute)
	strongSignal.FinalizeID("strong:one")
	strong := AcceptedSignal{Signal: strongSignal, Delta: 2, EvidenceClass: domain.GraylistEvidenceStrongTrusted}
	corroborated, err := Evaluate(EvaluateInput{Existing: &externalOnly.State, Accepted: &strong, Policy: policy, StrategyRevision: "strategy:test", CapabilityRevision: "capability:test", Now: strongSignal.ObservedAt})
	if err != nil || corroborated.State.Band != domain.GraylistBandRateCandidate {
		t.Fatalf("fresh strong corroboration did not enable candidate: %#v err=%v", corroborated, err)
	}
	expiredStrong, err := Evaluate(EvaluateInput{Existing: &corroborated.State, Policy: policy, StrategyRevision: "strategy:test", CapabilityRevision: "capability:test", Now: now.Add(2 * time.Minute)})
	if err != nil || expiredStrong.State.Band != domain.GraylistBandObserve || expiredStrong.State.DesiredAction != domain.IntentObserve {
		t.Fatalf("expired strong corroboration remained enforcing: %#v err=%v", expiredStrong, err)
	}
}

func TestOutOfOrderSignalDoesNotChangeState(t *testing.T) {
	now := time.Unix(3_800, 0).UTC()
	policy := PolicyV2{Revision: "policy:test", EnterScore: 2, ExitScore: 1, RateScore: 4, RateConfidenceBP: 1, MaxScore: 100, DecayInterval: time.Hour, TTL: time.Hour}
	firstSignal := signalFixture(now.Add(10*time.Second), 1, domain.SignalFallbackHit)
	first := AcceptedSignal{Signal: firstSignal, Delta: 2, EvidenceClass: domain.GraylistEvidenceStrongTrusted}
	current, err := Evaluate(EvaluateInput{Accepted: &first, Policy: policy, StrategyRevision: "strategy:test", CapabilityRevision: "capability:test", Now: firstSignal.ObservedAt})
	if err != nil {
		t.Fatal(err)
	}
	olderSignal := signalFixture(now.Add(5*time.Second), 2, domain.SignalFallbackHit)
	older := AcceptedSignal{Signal: olderSignal, Delta: 2, EvidenceClass: domain.GraylistEvidenceStrongTrusted}
	before := current.State
	if _, err := Evaluate(EvaluateInput{Existing: &current.State, Accepted: &older, Policy: policy, StrategyRevision: "strategy:test", CapabilityRevision: "capability:test", Now: now.Add(11 * time.Second)}); !errors.Is(err, ErrSignalOutOfOrder) {
		t.Fatalf("older unique signal error=%v", err)
	}
	if current.State.LastSignalAt != before.LastSignalAt || current.State.ExpiresAt != before.ExpiresAt || current.State.Score != before.Score || current.State.Revision != before.Revision {
		t.Fatalf("out-of-order input mutated existing state: before=%#v after=%#v", before, current.State)
	}
}

func TestNativePipelineHonestDecoyDowngradeAndNoAppliedAction(t *testing.T) {
	now := time.Unix(4_000, 0).UTC()
	signal := signalFixture(now, 1, domain.SignalFallbackHit)
	policy := PolicyV2{Revision: signal.Provenance.PolicyRevision, EnterScore: 1, ExitScore: 0, RateScore: 2, RateConfidenceBP: 1, MaxScore: 100, DecayInterval: time.Hour, TTL: time.Hour}
	binding := testRevisionBinding()
	capability := protectionresponse.NativeFallbackCapability(now, signal.Scope.TargetResourceID, signal.Scope.EndpointID, binding, false)
	result, err := Process(PipelineInput{
		Signal: signal, Admission: AdmissionContext{SelectedAdapterID: "adapter:exact", ExposesOriginalSubject: true},
		Policy: policy, Strategy: protectionresources.StrategyNativeFallback, StrategyRevision: capability.StrategyRevision,
		CapabilityRevision: capability.CapabilityRevision, Capability: capability,
		AllowlistResult: domain.PolicyCheckV2{Result: "deny"}, RecoveryResult: domain.PolicyCheckV2{Result: "allow"},
		Guard:               protectionpolicy.ManagementGuardResult{State: protectionpolicy.ManagementGuardNotApplicable, ActionAllowed: true},
		ActionScopeRevision: strings.Repeat("a", 64), EndpointRevision: strings.Repeat("b", 64),
		ResourceRevision: strings.Repeat("c", 64), ConfigurationRevision: strings.Repeat("d", 64),
		EndpointKnown: true, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	routeDecision := result.Decision
	routeDecision.RequestedIntent = domain.IntentRouteToDecoy
	routeDecision.FinalizeID()
	resolution := protectionresponse.Resolve(protectionresponse.ResolveInput{
		Decision: routeDecision, Strategy: protectionresources.StrategyNativeFallback, Capability: capability,
		ActionScopeRevision: strings.Repeat("a", 64), EndpointRevision: strings.Repeat("b", 64),
		ResourceRevision: strings.Repeat("c", 64), ConfigurationRevision: strings.Repeat("d", 64),
		EndpointKnown: true, Guard: protectionpolicy.ManagementGuardResult{State: protectionpolicy.ManagementGuardNotApplicable, ActionAllowed: true}, Now: now,
	})
	if resolution.SelectedIntent != domain.IntentObserve || resolution.PlannedResponse != nil || resolution.ActualStatus != "NOT_APPLIED" ||
		!slices.Contains(resolution.ReasonCodes, "native_fallback_natural_only") ||
		!slices.Contains(resolution.ReasonCodes, "forced_subject_decoy_unavailable") {
		t.Fatalf("native decoy resolution was dishonest: %#v", resolution)
	}
}

func BenchmarkEvaluate1000ActiveStates(b *testing.B) {
	now := time.Unix(5_000, 0).UTC()
	policy := PolicyV2{Revision: "policy:test", EnterScore: 2, ExitScore: 1, RateScore: 4, RateConfidenceBP: 5000, MaxScore: 100, DecayInterval: time.Minute, TTL: time.Hour}
	states := make([]domain.GraylistStateV2, 1000)
	for index := range states {
		signal := signalFixture(now, index+1, domain.SignalFallbackHit)
		signal.Subject.Value = fmt.Sprintf("198.51.%d.%d", index/250, index%250+1)
		signal.FinalizeID(fmt.Sprintf("benchmark:%d", index))
		accepted := AcceptedSignal{Signal: signal, Delta: 2}
		result, err := Evaluate(EvaluateInput{Accepted: &accepted, Policy: policy, StrategyRevision: "strategy:test", CapabilityRevision: "capability:test", Now: now})
		if err != nil {
			b.Fatal(err)
		}
		states[index] = result.State
	}
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		for index := range states {
			if _, err := Evaluate(EvaluateInput{Existing: &states[index], Policy: policy, StrategyRevision: "strategy:test", CapabilityRevision: "capability:test", Now: now}); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func FuzzEvaluateBoundedSignals(f *testing.F) {
	f.Add(2, 5000, uint8(1))
	f.Add(100, 10000, uint8(2))
	f.Fuzz(func(t *testing.T, score, confidence int, selector uint8) {
		if score < 0 || score > 100 || confidence < 0 || confidence > 10000 {
			t.Skip()
		}
		now := time.Unix(6_000, 0).UTC()
		kind := domain.SignalFallbackHit
		if selector%2 == 1 {
			kind = domain.SignalShortTLSSession
		}
		signal := signalFixture(now, int(selector)+1, kind)
		signal.ConfidenceBP = confidence
		signal.FinalizeID(fmt.Sprintf("fuzz-%d", selector))
		accepted := AcceptedSignal{Signal: signal, Delta: score, Weak: kind == domain.SignalShortTLSSession}
		policy := DefaultPolicyV2()
		policy.Revision = "policy:test"
		result, err := Evaluate(EvaluateInput{Accepted: &accepted, Policy: policy, StrategyRevision: "strategy:test", CapabilityRevision: "capability:test", Now: now})
		if err == nil {
			if result.State.Score < 0 || result.State.Score > 100 || len(result.State.SignalRefs) > domain.MaxGraylistSignalRefs || result.State.ActualActionState == "APPLIED" {
				t.Fatalf("unbounded or false-applied state: %#v", result.State)
			}
		}
	})
}

func FuzzAdmissionBoundedMetadata(f *testing.F) {
	f.Add("truncated", "true")
	f.Add("method_class", "get")
	f.Add("ja4", "bounded")
	f.Fuzz(func(t *testing.T, key, value string) {
		if len(key) > 64 || len(value) > 256 {
			t.Skip()
		}
		now := time.Unix(7_000, 0).UTC()
		signal := signalFixture(now, 1, domain.SignalFallbackHit)
		signal.SafeMeta = map[string]string{key: value}
		signal.FinalizeID("fuzz-metadata")
		accepted, err := Admit(signal, AdmissionContext{SelectedAdapterID: "adapter:exact", ExposesOriginalSubject: true, Now: now})
		if err == nil {
			if accepted.Signal.SafeMeta[key] != value || forbiddenMetadata(accepted.Signal.SafeMeta) || failClosedMetadata(accepted.Signal.SafeMeta) {
				t.Fatal("unsafe or non-deterministic metadata admission")
			}
		}
	})
}

func TestEvaluationStartsNoGoroutines(t *testing.T) {
	now := time.Unix(8_000, 0).UTC()
	policy := PolicyV2{Revision: "policy:test", EnterScore: 2, ExitScore: 1, RateScore: 4, RateConfidenceBP: 5000, MaxScore: 100, DecayInterval: time.Minute, TTL: time.Hour}
	accepted := AcceptedSignal{Signal: signalFixture(now, 1, domain.SignalFallbackHit), Delta: 2}
	result, err := Evaluate(EvaluateInput{Accepted: &accepted, Policy: policy, StrategyRevision: "strategy:test", CapabilityRevision: "capability:test", Now: now})
	if err != nil {
		t.Fatal(err)
	}
	before := runtime.NumGoroutine()
	for index := 0; index < 1000; index++ {
		if _, err := Evaluate(EvaluateInput{Existing: &result.State, Policy: policy, StrategyRevision: "strategy:test", CapabilityRevision: "capability:test", Now: now}); err != nil {
			t.Fatal(err)
		}
	}
	runtime.Gosched()
	if after := runtime.NumGoroutine(); after > before {
		t.Fatalf("pure evaluation leaked goroutines: before=%d after=%d", before, after)
	}
}

func signalFixture(now time.Time, id int, kind domain.SignalKind) domain.ProtectionSignalV2 {
	category := domain.SignalCategoryConnectionMetadata
	if kind == domain.SignalSYNBurst || kind == domain.SignalConntrackPressure {
		category = domain.SignalCategoryKernelPressure
	}
	signal := domain.ProtectionSignalV2{
		Schema:   domain.ProtectionSignalSchemaV2,
		Source:   domain.SignalSourceV2{SourceID: "source:exact", Producer: "producer:exact", ProducerVersion: "v2", TrustClass: "trusted_endpoint", SourceClass: "native"},
		Category: category, Kind: string(kind), KnownKind: true,
		Subject:    domain.SignalSubjectV2{Type: "ip", Value: "192.0.2.10"},
		Scope:      domain.SignalScopeV2{Scope: domain.ScopeEndpoint, TargetResourceID: "core:inbound:one", EndpointID: "endpoint:tcp:443", Transport: "tcp"},
		ObservedAt: now, ExpiresAt: now.Add(time.Hour), ConfidenceBP: 9000,
		Provenance: domain.SignalProvenanceV2{AdapterID: "adapter:exact", SourceRevision: "source-revision-v2", PolicyRevision: "policy:test", ObservationWindowID: fmt.Sprintf("window:%d", id)},
	}
	signal.FinalizeID(fmt.Sprintf("event:%d", id))
	return signal
}

func testRevisionBinding() domain.DecisionRevisionBindingV2 {
	return domain.DecisionRevisionBindingV2{
		StrategyRevision: "strategy-revision-v2", CapabilityRevision: "capability-revision-v2",
		ActionScopeRevision: strings.Repeat("a", 64), EndpointRevision: strings.Repeat("b", 64),
		ResourceRevision: strings.Repeat("c", 64), ConfigurationRevision: strings.Repeat("d", 64),
	}
}
