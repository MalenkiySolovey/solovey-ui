package resourcepressure

import (
	"math"
	"testing"
	"time"
)

func TestEvaluatorHysteresisRecoveryStalenessAndNoWriteStormRevision(t *testing.T) {
	evaluator, err := NewEvaluator([]Threshold{{ID: "disk.free_ratio", Direction: LowerIsWorse, Warning: .2, Constrained: .1, Critical: .05, Required: true}})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0)
	signal := func(at time.Time, value float64) []Signal {
		return []Signal{{ID: "disk.free_ratio", Status: ProviderSupported, Value: value, Unit: "ratio",
			ObservedAt: at.Unix(), ExpiresAt: at.Add(time.Minute).Unix()}}
	}
	if got := evaluator.Evaluate(now, signal(now, .5)); got.State != StateUnknown || got.Revision != 1 {
		t.Fatalf("one sample escaped unknown state: %#v", got)
	}
	now = now.Add(SampleInterval)
	if got := evaluator.Evaluate(now, signal(now, .5)); got.State != StateNormal || got.Revision != 2 {
		t.Fatalf("two healthy samples did not establish normal: %#v", got)
	}
	now = now.Add(SampleInterval)
	if got := evaluator.Evaluate(now, signal(now, .15)); got.State != StateNormal || got.Revision != 2 {
		t.Fatalf("single warning bypassed hysteresis: %#v", got)
	}
	now = now.Add(SampleInterval)
	if got := evaluator.Evaluate(now, signal(now, .15)); got.State != StateWarning || got.Revision != 3 {
		t.Fatalf("confirmed warning=%#v", got)
	}
	now = now.Add(SampleInterval)
	if got := evaluator.Evaluate(now, signal(now, .04)); got.State != StateCritical || got.Revision != 4 {
		t.Fatalf("critical must be immediate: %#v", got)
	}
	now = now.Add(SampleInterval)
	if got := evaluator.Evaluate(now, signal(now, .5)); got.State != StateCritical {
		t.Fatalf("single healthy sample escaped critical: %#v", got)
	}
	now = now.Add(SampleInterval)
	if got := evaluator.Evaluate(now, signal(now, .5)); got.State != StateRecovering || got.Revision != 5 {
		t.Fatalf("recovery window not entered: %#v", got)
	}
	recoveryStarted := now
	var recovered Snapshot
	for now.Sub(recoveryStarted) < RecoveryWindow {
		now = now.Add(SampleInterval)
		recovered = evaluator.Evaluate(now, signal(now, .5))
		if now.Sub(recoveryStarted) < RecoveryWindow && recovered.State != StateRecovering {
			t.Fatalf("recovery window exited early at %s: %#v", now.Sub(recoveryStarted), recovered)
		}
	}
	if recovered.State != StateNormal || recovered.Revision != 6 {
		t.Fatalf("normal was not confirmed: %#v", recovered)
	}
	expired := signal(now, .5)
	now = now.Add(DefaultFreshness + SampleInterval)
	stale := evaluator.Evaluate(now, expired)
	if stale.State != StateNormal || stale.Revision != 6 || len(stale.ReasonCodes) == 0 || stale.ReasonCodes[0] != "required_signal_unavailable:disk.free_ratio" {
		t.Fatalf("stale required provider fact is not bounded/stable: %#v", stale)
	}
	now = now.Add(SampleInterval)
	stale = evaluator.Evaluate(now, expired)
	if stale.State != StateWarning || stale.Revision != 7 {
		t.Fatalf("two stale required observations did not enter warning: %#v", stale)
	}
}

func TestPressureAdmissionPreservesSecurityStatusAndRecovery(t *testing.T) {
	for _, class := range []string{"essential", "recovery_essential", "interactive", "status", "security_critical"} {
		if decision := Decide(StateCritical, class); !decision.Allowed {
			t.Fatalf("critical pressure blocked %s: %#v", class, decision)
		}
	}
	for _, class := range []string{"heavy_mutation", "expensive", "optional", "bounded_component", "configuration"} {
		decision := Decide(StateCritical, class)
		if decision.Allowed || decision.RetryAfter <= 0 {
			t.Fatalf("critical pressure admitted %s: %#v", class, decision)
		}
	}
	if decision := Decide(StateUnknown, "heavy_mutation"); decision.Allowed {
		t.Fatal("unknown pressure admitted a heavy mutation")
	}
	if decision := Decide(StateRecovering, "heavy_mutation"); decision.Allowed || decision.RetryAfter == 0 {
		t.Fatalf("recovering pressure admitted a heavy mutation: %#v", decision)
	}
}

func TestPressureRejectsUnsafeThresholdsAndNumericSignals(t *testing.T) {
	for _, threshold := range []Threshold{
		{ID: "unsafe.ratio", Direction: HigherIsWorse, Warning: .8, Constrained: .9, Critical: 1.1},
		{ID: "unsafe.bytes", Direction: HigherIsWorse, Warning: 1, Constrained: 2, Critical: math.Inf(1)},
	} {
		if _, err := NewEvaluator([]Threshold{threshold}); err == nil {
			t.Fatalf("unsafe threshold was accepted: %#v", threshold)
		}
	}
	evaluator, err := NewEvaluator([]Threshold{{ID: "memory.ratio", Direction: HigherIsWorse,
		Warning: .8, Constrained: .9, Critical: .96, Required: true}})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0)
	snapshot := evaluator.Evaluate(now, []Signal{{ID: "memory.ratio", Status: ProviderSupported,
		Value: math.NaN(), Unit: "ratio", ObservedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix()}})
	if len(snapshot.Signals) != 1 || snapshot.Signals[0].Status != ProviderError ||
		snapshot.Signals[0].ReasonCode != "provider_numeric_value_invalid" {
		t.Fatalf("invalid numeric provider fact was not failed closed: %#v", snapshot)
	}
}

func BenchmarkPressureSampleNormalizationAndTransition(b *testing.B) {
	evaluator, err := NewEvaluator(DefaultThresholds())
	if err != nil {
		b.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0)
	signals := make([]Signal, 0, MaxSignals)
	for index := 0; index < MaxSignals; index++ {
		signals = append(signals, Signal{ID: "fixture." + benchmarkID(index), Status: ProviderSupported,
			Value: float64(index), Unit: "count", ObservedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix()})
	}
	b.ReportAllocs()
	for b.Loop() {
		evaluator.Evaluate(now, signals)
	}
}

func BenchmarkPressureAdmissionLookup(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = Decide(StateConstrained, "heavy_mutation")
	}
}

func benchmarkID(value int) string {
	const digits = "0123456789"
	if value < 10 {
		return string(digits[value])
	}
	return string([]byte{digits[value/10], digits[value%10]})
}
